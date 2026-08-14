# 5. Recipe, database and CRD

## 5.1 Terminology: Recipe

The name given to the ordered stack of layers (static and dynamic) and its associated `mergePolicy` ([02-resolution-model.md §2.1](02-resolution-model.md)) — what configblender executes to produce a resolved configuration tree. Implemented by `recipe.Recipe` (materialized form, ready to resolve) and `recipe.Spec` (stored declarative form, see 5.2).

## 5.2 Recipe stored in a database

**Decision**: a Recipe **is not carried by the reconciled CRD**. It's part of configblender's own configuration — **configblender manages its Recipes in its own database**, rather than in a K8s object (CRD or ConfigMap). Consequence: configblender is no longer a simple stateless controller reading the K8s API (like ESO) — it's a **stateful service**, with persistent storage to operate (durability, backup), and a way to write/edit a Recipe (CLI, see 5.3) since `kubectl apply` can no longer serve as an entry point for that.

**MVP implementation chosen: embedded storage (bbolt)**, rather than an external service (PostgreSQL...) — a Recipe is accessed by name, with no relational query needed, so an embedded key/value store is enough and adds no service to operate. Same reasoning ("the lightest thing that fits the need") as for the choice of Starlark ([02-resolution-model.md §2.6](02-resolution-model.md)). Package `recipedb`: `Put`/`Get`/`Delete`/`List`, JSON serialization of `recipe.Spec`.

**Editing and history, modeled on Vault (KV v2)**: every `Put` creates a **new version** rather than overwriting the previous one (`Put`/`GetVersion`/`ListVersions`/`Rollback`) — a Recipe's full history stays browsable, and `Rollback` restores an old version by writing a **new** version with its content (history is never rewritten, even for a rollback). Deliberate difference from Vault: values are **not masked** — a Recipe carries no secret ([04-kubernetes.md §4.1](04-kubernetes.md)), so this mechanism is a pure audit/rollback tool, not an access control.

**Web UI (built): `webui/` (Vue 3 + shadcn-vue + Tailwind), served by the central service itself.** The built assets (`npm run build`, output in `internal/centralserver/ui/`) are embedded in the binary (`//go:embed`, `internal/centralserver/ui.go`) — no separate frontend service to deploy, same "single component to operate" reasoning as for bbolt/Starlark. Features: Recipe list, a layer builder (add/reorder/remove layers, each pointing at a preconfigured Git source by name — 5.2bis — with a JSON-advanced fallback for `mergePolicy` and anything else), version history, line-by-line diff between a past version and the latest, rollback with confirmation, a Resolve/Explain tab that calls `/v1/resolve` to preview the result as a collapsible tree colored per source layer — including provenance with redirection to each value's Git source (2.3) — and a Sources admin screen to register/remove Git sources.

**Relationship with GitOps ([03-security.md](03-security.md)) — clarified**: what lives in the database is the Recipe's **structure** (which layers, in what order, `mergePolicy`) — not the layers' **content**. Content (static YAML/JSON files, Starlark scripts) **stays versioned in a Git repository**, so it stays reviewable in a PR. Each layer entry in a Recipe references its content via a `LayerSource` (`recipe.LayerSpec.Source`): a **reference to a preconfigured Git source by name** (`sourceRef`) plus a path and ref within it, rather than an inline repo URL — see 5.2bis.

**Architectural consequence**: configblender must itself **fetch layer content from Git** at resolution time (`gitsource.Fetcher`: in-memory clone, `Fetch` on every call, following the same polling logic as `refreshInterval` — [04-kubernetes.md §4.3](04-kubernetes.md)). The database therefore only stores the Recipe's structure/metadata, not the layer content itself (no duplicated source of truth between Git and the database). `recipe.Spec.Materialize(fetcher, sources)` performs this step — resolving each layer's `SourceRef` to a repo URL via `sources` (a `recipe.SourceResolver`) before fetching — and produces a `recipe.Recipe` ready for `resolve.Resolve`.

Implementation point worth noting: `Fetch` updates the remote-tracking refs (`refs/remotes/origin/*`) but not the local branch frozen at the initial clone (standard git behavior) — `gitsource` therefore resolves the remote ref first, falling back to the raw ref for tags/SHA/HEAD.

## 5.2bis Preconfigured Git sources

**Decision**: a layer never embeds a raw repo URL. Instead, `internal/gitsourcedb` stores a small registry of named Git sources — `GitSource{Name, Repo, Auth}`, sharing the same bbolt file as the Recipe database (`internal/recipesource.Open`, one `*bbolt.DB`, two buckets) — and a layer's `LayerSource.SourceRef` references one by name. The webui's layer builder only ever offers a `<select>` of registered names; the Sources admin screen (`webui/src/components/SourcesAdmin.vue`) is where a source's repo URL and credentials are entered, once, separately from any Recipe.

**Repository authentication (implemented, per-source, DB-backed)**: `gitsource.AuthResolver` (a `repoURL -> (transport.AuthMethod, error)` function) is built by `internal/gitauth.FromSources(lookup)`: for a fetch's repo URL, it asks `lookup` (backed by `gitsourcedb.Store.LookupByRepo`, always current) which registered `GitSource` owns it, then converts that source's own stored `Auth` (`internal/gitauth.AuthMethod`) — HTTP basic auth from `Username`+`Password` (`Password` doubling as a personal access token), or SSH from `SSHKey`/`SSHUser`/`SSHKeyPassphrase`. **The Recipe DB is the single source of truth for Git credentials — there is no environment-variable fallback and no global credential** (this reopens `docs/04-kubernetes.md §4.1`'s "configblender stores no secret material" framing, deliberately, in exchange for the webui's Sources admin screen being able to configure *and test* a source's credentials in one place, docs/07-open-questions.md). A repo URL with no registered source, or a registered source with no stored `Auth`, fetches unauthenticated.

**Write-only over the API and webui**: `PUT /v1/sources/{name}` accepts `Auth`, but `GET /v1/sources` never includes it in the response (`internal/centralserver`'s handlers construct that DTO without the field) — a stored credential is never echoed back, mirroring how an account's password hash or a service account's API key is handled (5.3ter). `POST /v1/sources/test` (`internal/centralapi.TestConnectionPath`) checks a repo/credential pair — saved or not — via `gitsource.TestConnection` (a cheap `git ls-remote`-equivalent, no clone), used by both the webui's "Test connection" button and `configblender source test`.

**API and CLI**: `GET /v1/sources` (public, credential-free), `PUT /v1/sources/{name}`, `DELETE /v1/sources/{name}`, and `POST /v1/sources/test` (all role-gated the same way as every other write — `admin` or `source-manager`, see 5.3ter) — see 5.3. `configblender source put`/`source test` accept credentials via `--username`/`--password`/`--password-stdin` (HTTPS) or `--ssh-key-file`/`--ssh-user`/`--ssh-key-passphrase` (SSH) — `--password-stdin` avoids putting a secret in shell history or `ps`. Unlike Recipes, sources carry no version history: they're operational config, not audited/rolled-back content.

## 5.3 CLI and API interface — read and write

**Read: local or via the central service**, either way, with exactly the same output in both cases — `cmd/configblender` accepts `--db <path>` (local database) or `--central-url <url>` (central service over HTTP, [04-kubernetes.md §4.2](04-kubernetes.md)), mutually exclusive:

- `configblender list (--db <path> | --central-url <url>)` — lists known Recipe names.
- `configblender get (--db <path> | --central-url <url>) --recipe <name> [--version <n>]` — prints the declarative Spec as YAML (the latest version, or a specific one) — chains naturally with `put` for an edit flow (get, edit the YAML, put).
- `configblender history (--db <path> | --central-url <url>) --recipe <name>` — lists the version history (number + timestamp).
- `configblender resolve (--db <path> | --central-url <url>) --recipe <name>` — resolves the named Recipe and prints the final tree as YAML (output 1 from [04-kubernetes.md §4.4](04-kubernetes.md), produced here outside of the ConfigMap managed by the controller).
- `configblender explain (--db <path> | --central-url <url>) --recipe <name> [--key <dotted.path>] [--annotate]` — prints the full explain, or a single key's provenance; `--annotate` folds Config and Explain into one config-shaped tree with each value commented by its source layer instead of a separate provenance tree ([02-resolution-model.md §2.3](02-resolution-model.md)).
- `configblender source list (--db <path> | --central-url <url>)` — lists registered Git sources (5.2bis).

On the central-service side, these same reads are exposed over HTTP (`internal/centralserver`): `GET /v1/resolve?recipe=<name>`, `GET /v1/recipes` (list), `GET /v1/recipes/{name}` (latest version), `GET /v1/recipes/{name}/versions` (history), `GET /v1/recipes/{name}/versions/{n}` (a specific version), `GET /v1/sources` (list registered Git sources, 5.2bis) — no authentication.

### 5.3bis Authenticated network write (revision of the initial decision)

**The "GitOps only, no push API" decision was revised** to allow editing/rollback from the web UI (5.2) and the remote CLI:

- `PUT /v1/recipes/{name}` (body: `recipe.Spec` JSON) — creates a new version.
- `POST /v1/recipes/{name}/rollback` (body: `{"version": n}`) — restores an old version as a new version.
- `configblender put (--db <path> | --central-url <url>)` and `configblender rollback (--db <path> | --central-url <url>)` — same subcommands as before, now also usable in network mode.
- `PUT /v1/sources/{name}` (body: `{"repo": "...", "auth": {...}}`) and `DELETE /v1/sources/{name}` — register/replace or remove a Git source, credentials included (5.2bis); `configblender source put (--db <path> | --central-url <url>) --name <name> --repo <url> [credential flags]` and `configblender source delete (--db <path> | --central-url <url>) --name <name>`.
- `POST /v1/sources/test` (body: `{"repo": "...", "auth": {...}}`) — checks a repo/credential pair without saving it (5.2bis).

**What this changes, and what it doesn't**: only the Recipe's **structure** (which layers, in what order, `mergePolicy`) becomes editable over the network. Layer **content** (static files, Starlark scripts) remains exclusively Git-sourced and therefore reviewable in a PR (5.2) — that part of the GitOps guarantee hasn't moved. What changed is who can decide *which* layers, in what order, without going through a code review.

**Authentication**: every write endpoint requires an identity, presented one of two ways: `Authorization: Bearer <api-key>` (a registered service account's key — API/CLI clients, `internal/centralclient`, `configblender`, and any machine caller — see 5.3ter), or a session cookie (the webui) obtained from `POST /v1/login` with `{"username": "...", "password": "..."}` (a registered human account — see 5.3ter). There is no shared break-glass credential — every identity is a named account, human or service, in `internal/userdb`. `internal/centralserver/session.go` issues an HMAC-signed cookie carrying `subject|expiresAt` — the signing key is a random secret persisted once via `internal/recipesource.Store.SessionSecret`, never operator-supplied, so sessions stay unforgeable and survive a restart. A registered user's role is looked up fresh from `internal/userdb` on every request rather than trusted from the cookie, so a role change or account deletion by an admin takes effect on the account's very next request instead of waiting out the cookie's 7-day TTL; a service account's API key is checked the same way, fresh, on every request — it never gets a cookie at all, since a machine caller has no session to keep (see 5.3ter). `GET /v1/session` reports `{"authenticated": bool, "username"?: string, "role"?: string}` so the webui can render real login state (and which role to show/hide admin-only UI for) instead of inferring it from a write's success/failure. Reads stay unauthenticated.

### 5.3ter Accounts and roles

**Resolves** the "no distinct users, no audit of who wrote what" limitation carried over from 5.3bis (partially — see below) — via a new `internal/userdb` package: a bbolt-backed store of accounts, sharing the same underlying file as the rest of the Recipe DB (`internal/recipesource.Store` wraps it, same pattern as `gitsourcedb`). One RBAC model — a flat `Role`, no hierarchy beyond `RoleAdmin` implicitly satisfying every check — shared by every auth method rather than a separate permission system per method, the way a HashiCorp Vault token or AppRole role carries the same policy vocabulary regardless of how it was obtained:

- `admin` — every write endpoint, including account management.
- `source-manager` — the Git source registry (`PUT`/`DELETE /v1/sources/{name}`, `POST /v1/sources/test`).
- `contributor` — Recipes (`PUT /v1/recipes/{name}`, `POST /v1/recipes/{name}/rollback`).
- `read` — no write endpoint at all. Reads are already unauthenticated (5.3bis above), so today this role's only effect is giving its holder an identity distinct from anonymous in access logs; it's the natural default for a service account minted just to call `/v1/resolve`, and it's ready for a future where reads themselves become gated.

Two account kinds share this same store, keyed by username, and the same role vocabulary — `internal/userdb.Kind`:

- **human** (`{username, bcrypt password hash, role, createdAt}`) — logs in via `POST /v1/login` with `{username, password}` for a session cookie, same as before.
- **service** (`{username, SHA-256 API-key hash, role, createdAt}`) — no password; `internal/userdb.CreateServiceAccount` generates a random `cbk_...` API key and returns it once (never recoverable, only rotatable via `internal/userdb.RotateServiceAccountKey` / `POST /v1/service-accounts/{username}/rotate`), hashed before storage and indexed by hash for O(1) lookup (`VerifyAPIKey`). Authenticates by presenting that key as `Authorization: Bearer <api-key>` on every request — no login step, no cookie, no session to expire — the Vault-token idiom for a machine caller (CI, a script, a controller) as opposed to Vault's userpass method for a human. Meant for exactly that: giving CI/automation a scoped, named identity (typically `read`, but any role) rather than one shared credential everyone (and every pipeline) uses as `admin` — the shared write token this feature replaced.

`GET /v1/users`, `POST /v1/users` (human only — service accounts are created via `POST /v1/service-accounts` instead, since there's no password to collect), `PUT /v1/users/{username}`, `DELETE /v1/users/{username}` (`internal/centralapi.UsersPath`) are `admin`-only and cover both kinds; a credential (password or API key) is always write-only — `List`/`Get` never return a hash, and an API key is shown exactly once, at creation or rotation. `PUT` rejects a password change on a service account (400 — it has none to change; rotate its key instead). An admin cannot delete the account they're currently authenticated as (self-deletion guard, so a lone admin can't lock themselves out). A failed login (password or API key) collapses "unknown account" and "wrong credential" into one generic error, so it can't be used to enumerate valid usernames.

**Bootstrap**: `cmd/server` creates one initial human admin account at startup if the account store is completely empty — from `CONFIGBLENDER_ADMIN_USER` (default `admin`) / `CONFIGBLENDER_ADMIN_PASSWORD` (a random password is generated and logged once if unset) — otherwise there'd be no way to reach the admin-only `POST /v1/users` endpoint at all on a brand-new deployment. A no-op on every later restart once at least one account exists.

**What this doesn't resolve yet**: writes still aren't attributed in version history itself (`recipedb.VersionInfo` records *when*, not *who* — the acting username is known to the HTTP layer at request time via the session or API key, but not yet threaded into the stored version metadata); a service account's key has no TTL/expiry, only manual rotation; see [07-open-questions.md](07-open-questions.md).

## 5.4 CRD proposal (v1alpha1)

Consequence of 5.2: the reconciled CRD becomes a **binding** resource, which references a Recipe by name and declares the target + refresh cadence — with no inline configuration content. This echoes, without formally settling it, the source/binding split à la `SecretStore`/`ExternalSecret` (see [07-open-questions.md](07-open-questions.md)): the Recipe plays the role of the source, this CRD the role of the binding.

```mermaid
flowchart LR
    subgraph central["Central service — Recipe database (5.2)"]
        recipe["Recipe « my-app-recipe »<br/>(layers + mergePolicy)"]
    end

    subgraph cluster["Target cluster"]
        crd["ConfigBlend « my-app-config »<br/>(binding: recipe, refreshInterval, target)"]
        cm[("ConfigMap « my-app-config »")]
        crd -->|references by name| recipe
        crd -->|produces| cm
    end
```

A single Recipe can in principle be referenced by several `ConfigBlend` resources (in the same cluster or across different clusters), each with its own target ConfigMap name and its own `refreshInterval` — useful for applying the same base configuration to several deployments without duplicating the Recipe. See [07-open-questions.md](07-open-questions.md) for what remains to be settled on this point.

```yaml
apiVersion: configblender.io/v1alpha1
kind: ConfigBlend
metadata:
  name: my-app-config
  namespace: my-app
spec:
  recipe: my-app-recipe      # referenced by name, no inline content
  refreshInterval: 1h        # 04-kubernetes.md §4.3 — ESO-style polling
  target:
    configMapName: my-app-config   # 04-kubernetes.md §4.3/§4.4 — single output
```

For reference, a Recipe's conceptual structure (ordered layers + centralized `mergePolicy`, [02-resolution-model.md §2.1](02-resolution-model.md)) is still the one sketched by `recipe.Spec` (5.1) — only where it's defined (database, not the CRD) changes, not its shape.

**Points still to be settled**: see [07-open-questions.md](07-open-questions.md) — Recipe-definition mechanism (resolved as: database, 5.2), exact `mergePolicy` syntax (nested paths/wildcards), whether a Recipe can be referenced by several binding CRDs.
