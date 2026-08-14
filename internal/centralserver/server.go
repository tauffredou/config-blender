// Package centralserver exposes internal/recipesource over HTTP — the
// "Vault" side of the ESO+Vault-shaped split (docs/04-kubernetes.md §4.2):
// it owns the Recipe database, the Git fetch, and the Starlark resolution;
// per-cluster controllers only ever call Resolve over the network.
//
// This package is transport only: decode request, call the service layer
// (internal/recipesource), encode response. Business rules (e.g. which name
// wins between a URL path and a request body) and error semantics
// (recipesource.ErrNotFound) live in the service layer — this package maps
// that one error to a 404 and everything else to a 500, once, in
// writeError, rather than repeating that decision per handler.
package centralserver

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"configblender/internal/centralapi"
	"configblender/internal/gitsourcedb"
	"configblender/internal/recipesource"
	"configblender/internal/userdb"
	"configblender/recipe"
	"configblender/recipedb"
	"configblender/resolve"
)

// Store is what the server needs from the underlying Recipe store —
// satisfied by *internal/recipesource.Store.
type Store interface {
	Resolve(ctx context.Context, name string) (*resolve.Result, error)
	Get(ctx context.Context, name string) (*recipe.Spec, error)
	GetVersion(ctx context.Context, name string, version int) (*recipe.Spec, error)
	ListVersions(ctx context.Context, name string) ([]recipedb.VersionInfo, error)
	List(ctx context.Context) ([]string, error)
	Put(ctx context.Context, name string, spec *recipe.Spec) error
	Rollback(ctx context.Context, name string, version int) error
	ListSources(ctx context.Context) ([]gitsourcedb.GitSource, error)
	PutSource(ctx context.Context, name string, src *gitsourcedb.GitSource) error
	DeleteSource(ctx context.Context, name string) error
	TestSourceConnection(ctx context.Context, repo string, auth *gitsourcedb.Credentials) error
	CreateUser(ctx context.Context, username, password string, role userdb.Role) error
	CreateServiceAccount(ctx context.Context, username string, role userdb.Role) (string, error)
	RotateServiceAccountKey(ctx context.Context, username string) (string, error)
	ListUsers(ctx context.Context) ([]userdb.User, error)
	SetUserRole(ctx context.Context, username string, role userdb.Role) error
	SetUserPassword(ctx context.Context, username, password string) error
	DeleteUser(ctx context.Context, username string) error
	VerifyUser(ctx context.Context, username, password string) (userdb.Role, error)
	VerifyAPIKey(ctx context.Context, apiKey string) (string, userdb.Role, error)
	UserRole(ctx context.Context, username string) (userdb.Role, error)
	SessionSecret(ctx context.Context) ([]byte, error)
	Ping() error
}

// Server exposes Store over HTTP, plus the embedded UI (ui.go). Reads are
// unauthenticated; writes are role-gated (internal/userdb.Role) behind an
// identity presented either as a session cookie obtained from
// POST centralapi.LoginPath (a human account, the webui) or
// `Authorization: Bearer <api-key>` (a service account,
// docs/05-recipe-and-crd.md §5.3ter — this reopens the earlier "GitOps
// only, no API push" decision, deliberately, in exchange for the UI's
// edit/rollback flow: only the Recipe *structure* is affected, layer
// *content* stays Git-sourced and PR-reviewable regardless). There is no
// shared break-glass credential: every identity is a named account, human
// or service, in internal/userdb.
type Server struct {
	store   Store
	session *sessionSigner
	log     *slog.Logger
	metrics *metrics
}

// Option configures a Server built by New.
type Option func(*Server)

// WithLogger overrides the default (slog.Default()) logger used for access
// logging and for unexpected (500-mapped) errors.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.log = l }
}

// New builds a Server.
func New(store Store, opts ...Option) *Server {
	s := &Server{store: store, log: slog.Default(), metrics: newMetrics()}
	for _, opt := range opts {
		opt(s)
	}
	secret, err := store.SessionSecret(context.Background())
	if err != nil {
		// Sessions still work within this process — the signer just gets
		// a random in-memory key — but won't survive a restart. Worth a
		// startup log, not worth failing to start over.
		s.log.Error("could not load persisted session secret, sessions will not survive a restart", "error", err)
		secret = make([]byte, 32)
		_, _ = cryptorand.Read(secret)
	}
	s.session = newSessionSigner(secret)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+centralapi.ResolvePath, s.handleResolve)
	mux.HandleFunc("PUT "+centralapi.RecipesPath+"/{name}", s.requireRole(userdb.RoleAdmin, userdb.RoleContributor)(s.handlePutRecipe))
	mux.HandleFunc("POST "+centralapi.RecipesPath+"/{name}/rollback", s.requireRole(userdb.RoleAdmin, userdb.RoleContributor)(s.handleRollback))
	mux.HandleFunc("GET "+centralapi.RecipesPath+"/{name}", s.handleGetRecipe)
	mux.HandleFunc("GET "+centralapi.RecipesPath+"/{name}/versions/{version}", s.handleGetRecipeVersion)
	mux.HandleFunc("GET "+centralapi.RecipesPath+"/{name}/versions", s.handleListVersions)
	mux.HandleFunc("GET "+centralapi.RecipesPath, s.handleListRecipes)
	mux.HandleFunc("PUT "+centralapi.SourcesPath+"/{name}", s.requireRole(userdb.RoleAdmin, userdb.RoleSourceManager)(s.handlePutSource))
	mux.HandleFunc("DELETE "+centralapi.SourcesPath+"/{name}", s.requireRole(userdb.RoleAdmin, userdb.RoleSourceManager)(s.handleDeleteSource))
	mux.HandleFunc("GET "+centralapi.SourcesPath, s.handleListSources)
	mux.HandleFunc("POST "+centralapi.TestConnectionPath, s.requireRole(userdb.RoleAdmin, userdb.RoleSourceManager)(s.handleTestConnection))
	mux.HandleFunc("GET "+centralapi.UsersPath, s.requireRole(userdb.RoleAdmin)(s.handleListUsers))
	mux.HandleFunc("POST "+centralapi.UsersPath, s.requireRole(userdb.RoleAdmin)(s.handleCreateUser))
	mux.HandleFunc("PUT "+centralapi.UsersPath+"/{username}", s.requireRole(userdb.RoleAdmin)(s.handleUpdateUser))
	mux.HandleFunc("DELETE "+centralapi.UsersPath+"/{username}", s.requireRole(userdb.RoleAdmin)(s.handleDeleteUser))
	mux.HandleFunc("POST "+centralapi.ServiceAccountsPath, s.requireRole(userdb.RoleAdmin)(s.handleCreateServiceAccount))
	mux.HandleFunc("POST "+centralapi.ServiceAccountsPath+"/{username}/rotate", s.requireRole(userdb.RoleAdmin)(s.handleRotateServiceAccountKey))
	mux.HandleFunc("POST "+centralapi.LoginPath, s.handleLogin)
	mux.HandleFunc("POST "+centralapi.LogoutPath, s.handleLogout)
	mux.HandleFunc("GET "+centralapi.SessionPath, s.handleSession)
	s.mountUI(mux)

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /metrics", s.metrics.handler())

	return s.withMiddleware(mux)
}

// identity is the caller a request authenticated as: either a registered
// user (Subject = their username, Role from internal/userdb, looked up
// fresh on every request rather than trusted from the session cookie —
// see authenticate) or the break-glass write token (Subject = "token",
// Role = RoleAdmin always).
type identity struct {
	subject string
	role    userdb.Role
}

// authenticate resolves the caller's identity from a session cookie (a
// human account) or a bearer token (a service account's API key,
// internal/userdb.VerifyAPIKey — the Vault-token idiom for machine callers,
// which authenticate per request rather than logging in for a session
// cookie the way a human/browser does), in that order; the zero identity
// and false if neither is present or valid.
func (s *Server) authenticate(r *http.Request) (identity, bool) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if subject, ok := s.session.valid(cookie.Value); ok {
			// Re-check against live data, not a role baked into the
			// cookie at login time: a role change or account deletion
			// takes effect on the very next request instead of waiting
			// out the session's multi-day TTL.
			if role, err := s.store.UserRole(r.Context(), subject); err == nil {
				return identity{subject: subject, role: role}, true
			}
		}
	}
	if bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); bearer != "" {
		if username, role, err := s.store.VerifyAPIKey(r.Context(), bearer); err == nil {
			return identity{subject: username, role: role}, true
		}
	}
	return identity{}, false
}

// requireRole gates a handler behind the caller authenticating as one of
// roles — 401 if unauthenticated, 403 if authenticated as a role that
// isn't allowed.
func (s *Server) requireRole(roles ...userdb.Role) func(http.HandlerFunc) http.HandlerFunc {
	allowed := make(map[userdb.Role]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, ok := s.authenticate(r)
			if !ok {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			if !allowed[id.role] {
				http.Error(w, fmt.Sprintf("role %q is not permitted to perform this action", id.role), http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
}

// handleLogin authenticates a registered human account (Username+Password,
// checked against internal/userdb) and on success sets a session cookie so
// the webui doesn't need to attach `Authorization: Bearer` to every write
// request itself — the browser sends the cookie automatically on
// same-origin requests. A service account never logs in here — it
// authenticates with its API key directly, per request (see authenticate).
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req centralapi.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "missing credentials: provide username and password", http.StatusBadRequest)
		return
	}
	role, err := s.store.VerifyUser(r.Context(), req.Username, req.Password)
	if err != nil {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	subject := req.Username

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.session.issueFor(subject),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		// Strict, not Lax: this cookie only ever needs to accompany
		// same-origin requests the webui itself makes (it's served by this
		// same process) — there is no legitimate cross-site navigation
		// that should carry it, so the stricter setting costs nothing and
		// closes off CSRF via the cookie entirely.
		SameSite: http.SameSiteStrictMode,
	})
	respondSession(w, subject, role, true)
}

// handleLogout clears the session cookie. The break-glass write token
// itself isn't revocable short of redeploying with a new one
// (docs/07-open-questions.md); this only ends the browser's session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleSession reports the request's current identity (cookie or bearer
// token), so the webui can render logged-in/logged-out state — and which
// role it holds, to show/hide admin-only UI — on load, without attempting
// a write and inferring auth state from its result.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authenticate(r)
	respondSession(w, id.subject, id.role, ok)
}

func respondSession(w http.ResponseWriter, subject string, role userdb.Role, authenticated bool) {
	w.Header().Set("Content-Type", "application/json")
	resp := centralapi.SessionResponse{Authenticated: authenticated}
	if authenticated {
		resp.Username, resp.Role = subject, string(role)
	}
	json.NewEncoder(w).Encode(resp)
}

// writeError maps a service-layer error to an HTTP response: ErrNotFound
// becomes 404 with the error's own (safe, user-facing) message; anything
// else is logged server-side with full detail and returned to the client as
// a generic 500, so internal error strings (DB paths, Git remote errors)
// never leak over the wire.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, recipesource.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "request_id", requestID(r.Context()), "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("recipe")
	if name == "" {
		http.Error(w, "missing recipe query parameter", http.StatusBadRequest)
		return
	}

	result, err := s.store.Resolve(r.Context(), name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ResolveResponse{
		Config:  result.Config,
		Explain: result.Explain,
	})
}

func (s *Server) handleGetRecipe(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	spec, err := s.store.Get(r.Context(), name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (s *Server) handleGetRecipeVersion(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		http.Error(w, "invalid version: "+err.Error(), http.StatusBadRequest)
		return
	}

	spec, err := s.store.GetVersion(r.Context(), name, version)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	versions, err := s.store.ListVersions(r.Context(), name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	entries := make([]centralapi.VersionEntry, len(versions))
	for i, v := range versions {
		entries[i] = centralapi.VersionEntry{Version: v.Version, UpdatedAt: v.UpdatedAt}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ListVersionsResponse{Versions: entries})
}

func (s *Server) handlePutRecipe(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var spec recipe.Spec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.Put(r.Context(), name, &spec); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req centralapi.RollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.Rollback(r.Context(), name, req.Version); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListRecipes(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.List(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if names == nil {
		// recipedb.Store.List returns a nil slice for an empty store,
		// which encoding/json renders as `null` rather than `[]` — fine
		// for Go callers, but a footgun for the UI (webui/), which does
		// data.names.length without a null check. An empty JSON array is
		// the correct empty-collection representation on the wire.
		names = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ListRecipesResponse{Names: names})
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListSources(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	out := make([]centralapi.GitSource, len(sources))
	for i, src := range sources {
		out[i] = centralapi.GitSource{Name: src.Name, Repo: src.Repo}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ListSourcesResponse{Sources: out})
}

func (s *Server) handlePutSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var src centralapi.GitSource
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.PutSource(r.Context(), name, &gitsourcedb.GitSource{Repo: src.Repo, Auth: toStoredCredentials(src.Auth)}); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestConnection checks a repo URL and (optional) credentials —
// saved or not — the same check PutSource's stored credentials will be
// exercised against on the next resolve. A failed connection is a normal
// outcome (bad token, unreachable host, ...), not a server error: it comes
// back as 200 with TestConnectionResponse.Ok = false, not a 4xx/5xx.
func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req centralapi.TestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Repo == "" {
		http.Error(w, "missing repo", http.StatusBadRequest)
		return
	}

	resp := centralapi.TestConnectionResponse{Ok: true}
	if err := s.store.TestSourceConnection(r.Context(), req.Repo, toStoredCredentials(req.Auth)); err != nil {
		resp.Ok = false
		resp.Error = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// toStoredCredentials converts the wire form of a source's credentials to
// the storage-layer type gitsourcedb persists — kept as separate types
// (internal/centralapi.Credentials vs gitsourcedb.Credentials) so the API's
// wire format doesn't couple directly to the storage layer's.
func toStoredCredentials(c *centralapi.Credentials) *gitsourcedb.Credentials {
	if c == nil {
		return nil
	}
	return &gitsourcedb.Credentials{
		Username:         c.Username,
		Password:         c.Password,
		SSHKey:           c.SSHKey,
		SSHUser:          c.SSHUser,
		SSHKeyPassphrase: c.SSHKeyPassphrase,
	}
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if err := s.store.DeleteSource(r.Context(), name); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	out := make([]centralapi.User, len(users))
	for i, u := range users {
		out[i] = centralapi.User{Username: u.Username, Kind: string(u.Kind), Role: string(u.Role), CreatedAt: u.CreatedAt}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ListUsersResponse{Users: out})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req centralapi.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	role := userdb.Role(req.Role)
	if !role.Valid() {
		http.Error(w, fmt.Sprintf("invalid role %q", req.Role), http.StatusBadRequest)
		return
	}

	if err := s.store.CreateUser(r.Context(), req.Username, req.Password, role); err != nil {
		if errors.Is(err, userdb.ErrAlreadyExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, userdb.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	var req centralapi.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Role == "" && req.Password == "" {
		http.Error(w, "nothing to update: set role and/or password", http.StatusBadRequest)
		return
	}

	if req.Role != "" {
		role := userdb.Role(req.Role)
		if !role.Valid() {
			http.Error(w, fmt.Sprintf("invalid role %q", req.Role), http.StatusBadRequest)
			return
		}
		if err := s.store.SetUserRole(r.Context(), username, role); err != nil {
			if errors.Is(err, userdb.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			s.writeError(w, r, err)
			return
		}
	}
	if req.Password != "" {
		if err := s.store.SetUserPassword(r.Context(), username, req.Password); err != nil {
			if errors.Is(err, userdb.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if errors.Is(err, userdb.ErrInvalidInput) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.writeError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateServiceAccount registers a machine account and returns its
// API key once — the account never has a password, so unlike
// handleCreateUser there's nothing for the caller to supply beyond a
// username and role.
func (s *Server) handleCreateServiceAccount(w http.ResponseWriter, r *http.Request) {
	var req centralapi.CreateServiceAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decoding request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	role := userdb.Role(req.Role)
	if !role.Valid() {
		http.Error(w, fmt.Sprintf("invalid role %q", req.Role), http.StatusBadRequest)
		return
	}

	apiKey, err := s.store.CreateServiceAccount(r.Context(), req.Username, role)
	if err != nil {
		if errors.Is(err, userdb.ErrAlreadyExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, userdb.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ServiceAccountKeyResponse{Username: req.Username, Role: string(role), APIKey: apiKey})
}

// handleRotateServiceAccountKey replaces a service account's API key and
// returns the new one once, immediately invalidating the old one.
func (s *Server) handleRotateServiceAccountKey(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	apiKey, err := s.store.RotateServiceAccountKey(r.Context(), username)
	if err != nil {
		if errors.Is(err, userdb.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, userdb.ErrInvalidInput) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeError(w, r, err)
		return
	}

	role, err := s.store.UserRole(r.Context(), username)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(centralapi.ServiceAccountKeyResponse{Username: username, Role: string(role), APIKey: apiKey})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	if id, ok := s.authenticate(r); ok && id.subject == username {
		http.Error(w, "cannot delete your own account while logged in as it", http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteUser(r.Context(), username); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHealthz reports process liveness — no dependency checks, just "the
// process is up and handling requests".
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReadyz reports whether the server can actually serve traffic: the
// Recipe database is reachable.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(); err != nil {
		http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
