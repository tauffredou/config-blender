// Package centralapi defines the wire format between a per-cluster
// controller and the central service (docs/04-kubernetes.md §4.2, the
// ESO+Vault-shaped split): shared by internal/centralserver and
// internal/centralclient so neither depends on the other's internals.
package centralapi

import "time"

// ResolvePath is the endpoint a controller calls to resolve a named
// Recipe; the name is passed as the "recipe" query parameter.
const ResolvePath = "/v1/resolve"

// RecipesPath is the base path for consulting Recipes, including their
// version history (docs/05-recipe-and-crd.md §5.3 — "configuring
// configblender", Vault-KV-v2-style versioning):
//   - GET RecipesPath/{name}                   — fetch the latest version
//   - GET RecipesPath/{name}/versions           — list version history (ListVersionsResponse)
//   - GET RecipesPath/{name}/versions/{version} — fetch a specific version
//   - GET RecipesPath                           — list names (ListRecipesResponse)
//
// Read-only for v1: creating or changing a Recipe is GitOps only (applied
// locally to the store, e.g. `configblender put --db`), not a network
// write call — see docs/07-open-questions.md for how that GitOps sync is
// expected to work eventually.
const RecipesPath = "/v1/recipes"

// ResolveResponse is the JSON body of a successful resolve call — the same
// two outputs a local resolve.Result carries (docs/04-kubernetes.md §4.4).
type ResolveResponse struct {
	Config  map[string]any `json:"config"`
	Explain map[string]any `json:"explain"`
}

// ListRecipesResponse is the JSON body of a successful list call.
type ListRecipesResponse struct {
	Names []string `json:"names"`
}

// VersionEntry is one entry in a Recipe's version history, without the
// Spec body (mirrors recipedb.VersionInfo).
type VersionEntry struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListVersionsResponse is the JSON body of a successful version-history call.
type ListVersionsResponse struct {
	Versions []VersionEntry `json:"versions"`
}

// RollbackRequest is the JSON body of POST RecipesPath/{name}/rollback.
type RollbackRequest struct {
	Version int `json:"version"`
}

// SourcesPath is the base path for managing the Git source registry
// (docs/07-open-questions.md — the preconfigured-source registry a Recipe
// layer references by name instead of embedding a raw repo URL):
//   - GET SourcesPath        — list registered sources (ListSourcesResponse)
//   - PUT SourcesPath/{name} — register or replace a source (write-token gated)
//   - DELETE SourcesPath/{name} — remove a source (write-token gated)
//
// Unlike RecipesPath, there is no version history here — a source is
// operational config (which repos configblender may read from), not
// audited/rolled-back content.
const SourcesPath = "/v1/sources"

// GitSource is the wire form of a registered Git source: just its name and
// repo URL, never credentials — internal/gitauth resolves those from the
// environment at fetch time, so there is nothing secret to carry over the
// API.
type GitSource struct {
	Name string `json:"name"`
	Repo string `json:"repo"`
}

// ListSourcesResponse is the JSON body of a successful GET SourcesPath call.
type ListSourcesResponse struct {
	Sources []GitSource `json:"sources"`
}
