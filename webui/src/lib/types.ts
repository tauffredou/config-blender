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

// SourceConfig is a preconfigured Git source (docs/07-open-questions.md):
// just a name and repo URL, registered once via the Sources admin screen
// or `configblender source put`, then referenced from a layer by name —
// never a raw URL typed into the layer builder. No credentials travel
// over this type; internal/gitauth resolves those from the environment.
export interface SourceConfig {
  name: string
  repo: string
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
