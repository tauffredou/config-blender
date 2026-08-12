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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"configblender/internal/centralapi"
	"configblender/internal/gitsourcedb"
	"configblender/internal/recipesource"
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
	Ping() error
}

// Server exposes Store over HTTP, plus the embedded UI (ui.go). Reads are
// unauthenticated; writes (PUT recipe, rollback) require a bearer token
// (docs/05-recipe-and-crd.md §5.3, docs/07-open-questions.md — this
// reopens the earlier "GitOps only, no API push" decision, deliberately,
// in exchange for the UI's edit/rollback flow: only the Recipe *structure*
// is affected, layer *content* stays Git-sourced and PR-reviewable
// regardless).
type Server struct {
	store      Store
	writeToken string
	log        *slog.Logger
	metrics    *metrics
}

// Option configures a Server built by New.
type Option func(*Server)

// WithLogger overrides the default (slog.Default()) logger used for access
// logging and for unexpected (500-mapped) errors.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.log = l }
}

// New builds a Server. writeToken gates the write endpoints (PUT/rollback)
// via `Authorization: Bearer <writeToken>`; an empty writeToken disables
// writes entirely (they 403) rather than defaulting to open.
func New(store Store, writeToken string, opts ...Option) *Server {
	s := &Server{store: store, writeToken: writeToken, log: slog.Default(), metrics: newMetrics()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+centralapi.ResolvePath, s.handleResolve)
	mux.HandleFunc("PUT "+centralapi.RecipesPath+"/{name}", s.requireToken(s.handlePutRecipe))
	mux.HandleFunc("POST "+centralapi.RecipesPath+"/{name}/rollback", s.requireToken(s.handleRollback))
	mux.HandleFunc("GET "+centralapi.RecipesPath+"/{name}", s.handleGetRecipe)
	mux.HandleFunc("GET "+centralapi.RecipesPath+"/{name}/versions/{version}", s.handleGetRecipeVersion)
	mux.HandleFunc("GET "+centralapi.RecipesPath+"/{name}/versions", s.handleListVersions)
	mux.HandleFunc("GET "+centralapi.RecipesPath, s.handleListRecipes)
	mux.HandleFunc("PUT "+centralapi.SourcesPath+"/{name}", s.requireToken(s.handlePutSource))
	mux.HandleFunc("DELETE "+centralapi.SourcesPath+"/{name}", s.requireToken(s.handleDeleteSource))
	mux.HandleFunc("GET "+centralapi.SourcesPath, s.handleListSources)
	s.mountUI(mux)

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /metrics", s.metrics.handler())

	return s.withMiddleware(mux)
}

// requireToken gates a write handler behind the configured bearer token.
// Constant-time comparison avoids leaking the token through response-time
// side channels.
func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.writeToken == "" {
			http.Error(w, "write API disabled: no token configured", http.StatusForbidden)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.writeToken)) != 1 {
			http.Error(w, "invalid or missing bearer token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
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

	if err := s.store.PutSource(r.Context(), name, &gitsourcedb.GitSource{Name: src.Name, Repo: src.Repo}); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if err := s.store.DeleteSource(r.Context(), name); err != nil {
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
