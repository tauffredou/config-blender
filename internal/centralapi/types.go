// Package centralapi defines the wire format between a per-cluster
// controller and the central service (docs/04-kubernetes.md §4.2, the
// ESO+Vault-shaped split): shared by internal/centralserver and
// internal/centralclient so neither depends on the other's internals.
package centralapi

import "time"

// ResolvePath is the endpoint a controller calls to resolve a named
// Recipe; the name is passed as the "recipe" query parameter.
const ResolvePath = "/v1/resolve"

// LoginPath, LogoutPath and SessionPath implement session-based auth for
// the webui (docs/07-open-questions.md — "UI authentication beyond a
// single shared token"): still one shared secret
// (CONFIGBLENDER_WRITE_TOKEN), but the browser proves it once via POST
// LoginPath rather than attaching it to every write request. API/CLI
// callers (internal/centralclient) are unaffected — the `Authorization:
// Bearer` header still works exactly as before; the session cookie is a
// second, additional way to satisfy the same write-token gate.
//   - POST LoginPath   — {token} -> sets a session cookie, or 401
//   - POST LogoutPath  — clears the session cookie
//   - GET  SessionPath — {authenticated: bool}, reflects the request's cookie
const (
	LoginPath   = "/v1/login"
	LogoutPath  = "/v1/logout"
	SessionPath = "/v1/session"
)

// LoginRequest is the JSON body of POST LoginPath.
type LoginRequest struct {
	Token string `json:"token"`
}

// SessionResponse is the JSON body of a successful GET SessionPath call.
type SessionResponse struct {
	Authenticated bool `json:"authenticated"`
}

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
//   - GET SourcesPath                — list registered sources (ListSourcesResponse), never including Auth
//   - PUT SourcesPath/{name}         — register or replace a source, Auth included (write-token gated)
//   - DELETE SourcesPath/{name}      — remove a source (write-token gated)
//   - POST SourcesPath/test          — test connectivity to a repo/credential pair, saved or not (write-token gated)
//
// Unlike RecipesPath, there is no version history here — a source is
// operational config (which repos configblender may read from, and with
// what credentials), not audited/rolled-back content.
const SourcesPath = "/v1/sources"

// TestConnectionPath is the endpoint used by the webui's Sources admin
// screen to verify a repo URL and credentials — including ones not yet
// saved — before committing them with PUT SourcesPath/{name}.
const TestConnectionPath = SourcesPath + "/test"

// Credentials is the wire form of a Git source's credentials
// (docs/04-kubernetes.md §4.1) — write-only: PUT SourcesPath/{name} and
// POST TestConnectionPath accept it, but GET/List never populate it in
// their response (internal/centralserver's handlers construct those DTOs
// without this field), so a stored credential is never echoed back over
// the API or shown in the webui. Mirrors gitsourcedb.Credentials field for
// field; kept as a separate type so the wire format doesn't couple
// directly to the storage layer's.
type Credentials struct {
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	SSHKey           string `json:"sshKey,omitempty"`
	SSHUser          string `json:"sshUser,omitempty"`
	SSHKeyPassphrase string `json:"sshKeyPassphrase,omitempty"`
}

// GitSource is the wire form of a registered Git source. Auth is
// write-only — see Credentials.
type GitSource struct {
	Name string       `json:"name"`
	Repo string       `json:"repo"`
	Auth *Credentials `json:"auth,omitempty"`
}

// ListSourcesResponse is the JSON body of a successful GET SourcesPath call.
type ListSourcesResponse struct {
	Sources []GitSource `json:"sources"`
}

// TestConnectionRequest is the JSON body of POST TestConnectionPath: a
// repo URL and (optional) credentials to try, independent of whether
// they're saved as a registered source yet.
type TestConnectionRequest struct {
	Repo string       `json:"repo"`
	Auth *Credentials `json:"auth,omitempty"`
}

// TestConnectionResponse is the JSON body of a completed POST
// TestConnectionPath call. Ok is false with Error set when the connection
// attempt failed — this is a normal, expected outcome (e.g. a wrong
// token), not an HTTP-level error, so the endpoint still returns 200.
type TestConnectionResponse struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
