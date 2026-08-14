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
// single shared token"): the browser proves its identity once via POST
// LoginPath, either as a registered user (Username+Password, checked
// against internal/userdb, role-gated per UsersPath below) or as the
// break-glass CONFIGBLENDER_WRITE_TOKEN (Token, always treated as
// RoleAdmin) — either issues the same kind of session cookie. API/CLI
// callers (internal/centralclient) are unaffected: the `Authorization:
// Bearer <writeToken>` header still works exactly as before and is always
// treated as RoleAdmin too; the session cookie is an additional way for
// the webui to authenticate without attaching that header to every write
// request.
//   - POST LoginPath   — {username,password} or {token} -> sets a session cookie, or 401
//   - POST LogoutPath  — clears the session cookie
//   - GET  SessionPath — reflects the request's cookie/bearer identity
const (
	LoginPath   = "/v1/login"
	LogoutPath  = "/v1/logout"
	SessionPath = "/v1/session"
)

// LoginRequest is the JSON body of POST LoginPath — exactly one of
// (Username+Password) or Token is expected to be set.
type LoginRequest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

// SessionResponse is the JSON body of a successful GET SessionPath call,
// and of a successful POST LoginPath. Username and Role are empty when
// Authenticated is false, or when the session was established via the
// break-glass write token rather than a registered user (Username is then
// "token", Role is "admin").
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	Role          string `json:"role,omitempty"`
}

// UsersPath is the base path for managing configblender's own accounts —
// human and service alike (docs/07-open-questions.md — roles gate write
// endpoints: RoleAdmin/RoleSourceManager/RoleContributor/RoleRead in
// internal/userdb, the one RBAC model shared by both account kinds). Every
// endpoint here is RoleAdmin-only.
//   - GET    UsersPath              — list every account, human and service (ListUsersResponse), never including credentials
//   - POST   UsersPath              — create a human account (CreateUserRequest)
//   - PUT    UsersPath/{username}   — change role and/or password (UpdateUserRequest); Password is rejected for a service account
//   - DELETE UsersPath/{username}   — remove an account, either kind
//
// ServiceAccountsPath below is the parallel surface for the other account
// kind — creation and key rotation, since a service account has no
// password to set via UpdateUserRequest.
const UsersPath = "/v1/users"

// User is the wire form of a registered account — never includes a
// password, password hash, or API key/hash.
type User struct {
	Username  string    `json:"username"`
	Kind      string    `json:"kind"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListUsersResponse is the JSON body of a successful GET UsersPath call.
type ListUsersResponse struct {
	Users []User `json:"users"`
}

// CreateUserRequest is the JSON body of POST UsersPath — always creates a
// human (password) account; see CreateServiceAccountRequest for the other
// kind.
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateUserRequest is the JSON body of PUT UsersPath/{username}. Role and
// Password are each optional — set whichever should change; an empty
// Password leaves the account's password unchanged. Password is invalid
// (400) for a service account — rotate its key via ServiceAccountsPath
// instead.
type UpdateUserRequest struct {
	Role     string `json:"role,omitempty"`
	Password string `json:"password,omitempty"`
}

// ServiceAccountsPath manages machine accounts that authenticate with
// `Authorization: Bearer <api-key>` on every request instead of a
// username/password session — the Vault-token idiom for CI/scripts, gated
// by the same Role vocabulary as human accounts (RoleAdmin-only to manage,
// same as UsersPath):
//   - POST ServiceAccountsPath/{username}/rotate — replace the account's API key, returning the new one once
//   - POST ServiceAccountsPath                   — create a service account (CreateServiceAccountRequest), returning its API key once
//
// A service account is listed, role-changed, and deleted through UsersPath
// like any other account — this path only covers what's specific to having
// an API key instead of a password.
const ServiceAccountsPath = "/v1/service-accounts"

// CreateServiceAccountRequest is the JSON body of POST ServiceAccountsPath.
type CreateServiceAccountRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ServiceAccountKeyResponse is the JSON body of a successful POST
// ServiceAccountsPath or POST ServiceAccountsPath/{username}/rotate call.
// APIKey is shown here once — it is never retrievable again, only rotated.
type ServiceAccountKeyResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	APIKey   string `json:"apiKey"`
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
