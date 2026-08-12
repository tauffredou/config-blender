# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

configblender is a configuration management tool for applications deployed across multiple Kubernetes
environments: it resolves an ordered stack of layers (static YAML/JSON + sandboxed Starlark) into a
config tree with per-key provenance, stores that stack (a "Recipe") versioned like Vault KV v2, and
ships it to clusters via a lightweight per-cluster controller (ESO/External-Secrets-style). Full design
is in [CONCEPTION.md](CONCEPTION.md) and [docs/](docs/) — package-level doc comments across the
Go code point back to specific sections (e.g. `CONCEPTION.md section 5.3`, `docs/02-resolution-model.md
§2.3`); read the cited section when a comment references one, don't guess at the design from the code alone.

## Commands

Build orchestration is [Task](https://taskfile.dev) (`Taskfile.yml`), not Make — `task --list-all` (or
just `task`) lists everything.

- `task check` — build + vet + test; run before committing.
- `task test` — Go test suite (`go test ./...`). For a single package/test:
  `go test ./resolve/... -run TestName` (webui must be built first, see gotcha below).
- `task vet` — `go vet ./...`.
- `task build` — builds `manager`, `server`, `configblender` CLI binaries into `bin/`.
- `task dev:server` — runs the central service locally against a scratch Recipe DB at
  `/tmp/configblender-dev`, writes enabled, token `devtoken`, on `:8091`.
- `task webui:dev` — Vite dev server with hot reload against a running `dev:server`.
- `task webui:build` — builds the Vue UI and embeds it into `internal/centralserver/ui`.
- `task docker:build` / `task docker:manager` / `task docker:server` — build container images.
- `task kind:load` — build images and load them into the local kind cluster (`configblender-mvp`).
- `task crd:install` — apply the `ConfigBlend` CRD to the current kubeconfig context.
- `task clean` — removes `bin/`, `webui/dist`, `webui/node_modules`, and the embedded
  `internal/centralserver/ui` — leaves the repo **unbuildable** until `task webui:build` runs again.

### Critical gotcha: the webui embed

`internal/centralserver/ui.go` does `//go:embed ui`. `cmd/server`, and anything importing
`internal/centralserver` (including its tests), will not compile until `webui/` has been built at
least once. Every Go task in the Taskfile depends on `webui:build` for exactly this reason — if you
run raw `go build`/`go test`/`go vet` yourself instead of via `task`, build the webui first or you'll
hit a confusing compile failure unrelated to your change.

### Webui-only commands

Run from `webui/`: `npm run dev` (Vite), `npm run build` (`vue-tsc -b && vite build`), `npm run preview`.
Stack: Vue 3 + shadcn-vue + Tailwind v4 + vue-tsc.

## Architecture

Data flow: Git repos → central service (Recipe DB + resolution engine) → per-cluster controller (HTTP) →
ConfigMap → pod. Secrets (Vault) are a stated non-goal for now — not connected.

- **`recipe`** — declarative types (`Recipe`, `Layer`, `MergeRule`) plus the `Spec`/`LayerSpec` →
  `Materialize` path that fetches layer content from Git. No I/O in the core types themselves.
- **`resolve`** — the resolution engine: merges a Recipe's ordered layers into a config tree while
  tracking per-key provenance. Layer order is semantically significant. Two layer types: static
  (YAML/JSON, unmarshaled directly) and dynamic (Starlark, executed sandboxed against the config
  accumulated so far by earlier layers — see `starlark_layer.go`). Deliberately independent of the K8s
  controller, Recipe DB, and Git fetching; those are wired around it, not through it.
- **`gitsource`** — fetches layer content from Git with auth (HTTP token, SSH); `internal/gitauth`
  builds credentials from the Git-source registry's stored `Auth` (`internal/gitsourcedb`) — the Recipe
  DB is the only source of Git credentials, no environment fallback — shared between the CLI and the
  controller.
- **`recipedb`** — Recipe storage (bbolt), versioned Vault-KV-v2-style (`Put`/`GetVersion`/
  `ListVersions`/`Rollback`).
- **`internal/recipesource`** — persistent Recipe+Git store used by both the CLI and the central service.
- **`api/v1alpha1`** — the `ConfigBlend` CRD Go types (mirrored in `deploy/crd/configblend.yaml`).
- **`internal/controller`** + **`cmd/manager`** — the per-cluster controller: reconciles `ConfigBlend` →
  `ConfigMap` (ESO-style), resolving via a `RecipeResolver` that can be local or remote (network to the
  central service). Tested against both a fake client and a real kind cluster.
- **`internal/centralapi`** / **`internal/centralserver`** / **`internal/centralclient`** + `cmd/server`
  — the central service: Recipe DB + Git + Starlark resolution behind an HTTP API. Public reads,
  token-authenticated writes (`CONFIGBLENDER_WRITE_TOKEN`). Embeds the built webui.
- **`internal/cli`** + **`cmd/configblender`** — CLI (`put`/`rollback`/`list`/`get`/`history`/`resolve`/
  `explain`), usable against a local DB (`--db`) or the central service (`--central-url`).
- **`webui/`** — Vue 3 + shadcn-vue UI (Vault-like): Recipe list, editor, history/diff, rollback,
  Resolve/Explain. Built by `task webui:build`, embedded into `cmd/server`.

Everything in the table above is implemented and tested per [CONCEPTION.md](CONCEPTION.md)'s
implementation-status table, including a real kind-cluster round trip and a real-browser UI pass —
check that table before assuming something is a stub.
