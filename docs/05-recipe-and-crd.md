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

**Decision**: a layer never embeds a raw repo URL (and so never any credentials that might be baked into one). Instead, `internal/gitsourcedb` stores a small registry of named Git sources — `GitSource{Name, Repo}`, no credentials, sharing the same bbolt file as the Recipe database (`internal/recipesource.Open`, one `*bbolt.DB`, two buckets) — and a layer's `LayerSource.SourceRef` references one by name. The webui's layer builder only ever offers a `<select>` of registered names; the Sources admin screen (`webui/src/components/SourcesAdmin.vue`) is where a source's repo URL is entered, once, separately from any Recipe.

**Repository authentication (implemented, per-source)**: `gitsource.AuthResolver` (a `repoURL -> transport.AuthMethod` function) is built by `internal/gitauth.FromSources(lookupName)`: for a fetch's repo URL, it asks `lookupName` (backed by `gitsourcedb.Store.LookupByRepo`, always current) which registered source owns it, then resolves *that source's own* credentials from environment variables suffixed by its sanitized name — `GIT_TOKEN_<NAME>`, `GIT_USERNAME_<NAME>`, `GIT_SSH_KEY_<NAME>` / `GIT_SSH_KEY_FILE_<NAME>`, `GIT_SSH_USER_<NAME>`, `GIT_SSH_KEY_PASSPHRASE_<NAME>` (same names `internal/gitauth.FromEnv` already recognizes, just suffixed; e.g. a source named `internal-configs` reads `GIT_TOKEN_INTERNAL_CONFIGS`). Falls back to the single global credential (`FromEnv`'s behavior) when a source has no per-source override, or when a repo URL belongs to no registered source — same fallback used before this registry existed. No credential is ever stored in the database, the API, or the webui: only a source's name and repo URL are, and both are already effectively public (the registry is world-readable, matching Recipes).

**API and CLI**: `GET /v1/sources` (public), `PUT /v1/sources/{name}` and `DELETE /v1/sources/{name}` (write-token gated, same token as Recipe writes) — see 5.3. Unlike Recipes, sources carry no version history: they're operational config, not audited/rolled-back content.

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
- `PUT /v1/sources/{name}` (body: `{"repo": "..."}`) and `DELETE /v1/sources/{name}` — register/replace or remove a Git source (5.2bis); `configblender source put (--db <path> | --central-url <url>) --name <name> --repo <url>` and `configblender source delete (--db <path> | --central-url <url>) --name <name>`.

**What this changes, and what it doesn't**: only the Recipe's **structure** (which layers, in what order, `mergePolicy`) becomes editable over the network. Layer **content** (static files, Starlark scripts) remains exclusively Git-sourced and therefore reviewable in a PR (5.2) — that part of the GitOps guarantee hasn't moved. What changed is who can decide *which* layers, in what order, without going through a code review.

**Authentication**: both write endpoints require `Authorization: Bearer <token>`, compared in constant time against `CONFIGBLENDER_WRITE_TOKEN` (an environment variable of the central service — same philosophy as `internal/gitauth`, fed from a mounted K8s Secret). An empty token completely disables the write endpoints (403), rather than opening them by default. Reads stay unauthenticated. Accepted limitation: a single shared secret, no distinct users or audit of who wrote what (version history knows *when*, not *who*) — see [07-open-questions.md](07-open-questions.md).

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
