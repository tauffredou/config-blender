// Mirrors the Go wire types (internal/centralapi, recipe.Spec) exposed by
// the central service — docs/05-recipe-and-crd.md §5.3.

// GitSource is a *resolved* pointer to a layer's content — what the
// Explain tree's provenance tooltip shows (ExplainNode.vue). Distinct from
// SourceConfig below: this always has a concrete repo URL, path and ref,
// regardless of whether the Recipe's layer referenced a registered source
// by name or (only possible server-side, never via the builder) embedded
// one inline.
export interface GitSource {
  repo: string
  path: string
  ref: string
}

// Credentials authenticates a SourceConfig's repo (docs/04-kubernetes.md
// §4.1, docs/05-recipe-and-crd.md §5.2bis): the Recipe DB is the only
// place Git credentials live, so this is the one place a form ever
// collects them. Write-only: a GET/list response never populates this on
// a SourceConfig, even for a source that has credentials stored — only
// PUT and the test-connection call send it.
export interface Credentials {
  username?: string
  password?: string
  sshKey?: string
  sshUser?: string
  sshKeyPassphrase?: string
}

// SourceConfig is a preconfigured Git source (docs/07-open-questions.md):
// a name, repo URL, and (write-only) credentials, registered once via the
// Sources admin screen or `configblender source put`, then referenced from
// a layer by name — never a raw URL typed into the layer builder.
export interface SourceConfig {
  name: string
  repo: string
  auth?: Credentials
}

export type LayerType = "static" | "dynamic"

// LayerSourceRef is how a LayerSpec locates its content: by reference to a
// registered SourceConfig (sourceRef), plus the path/ref within it —
// mirrors recipe.LayerSource.
export interface LayerSourceRef {
  sourceRef: string
  path: string
  ref: string
}

export interface LayerSpec {
  name: string
  type: LayerType
  source: LayerSourceRef
}

export type MergeStrategy = "replace" | "union" | "append"

export interface MergeRule {
  path: string
  strategy: MergeStrategy
}

export interface RecipeSpec {
  name: string
  layers: LayerSpec[]
  mergePolicy?: MergeRule[]
}

export interface VersionEntry {
  version: number
  updatedAt: string
}

export interface ResolveResponse {
  config: Record<string, unknown>
  explain: Record<string, unknown>
}

// Role mirrors the Go role enum (internal/userdb): three flat values, no
// hierarchy except that the backend implicitly grants "admin" every check
// that any other role satisfies — every allowed-role list in this UI must
// include "admin" itself, mirroring the backend's route table.
export type Role = "admin" | "source-manager" | "contributor"

export interface User {
  username: string
  role: Role
  createdAt: string
}
