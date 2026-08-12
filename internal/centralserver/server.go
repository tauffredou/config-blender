// Package centralserver exposes internal/recipesource over HTTP — the
// "Vault" side of the ESO+Vault-shaped split (docs/04-kubernetes.md §4.2):
// it owns the Recipe database, the Git fetch, and the Starlark resolution;
// per-cluster controllers only ever call Resolve over the network.
package centralserver

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"configblender/internal/centralapi"
	"configblender/internal/gitsourcedb"
	"configblender/recipe"
	"configblender/recipedb"
	"configblender/resolve"
)

// Store is what the server needs from the underlying Recipe store —
// satisfied by *internal/recipesource.Store.
type Store interface {
	Resolve(name string) (*resolve.Result, error)
	Get(name string) (*recipe.Spec, error)
	GetVersion(name string, version int) (*recipe.Spec, error)
	ListVersions(name string) ([]recipedb.VersionInfo, error)
	List() ([]string, error)
	Put(spec *recipe.Spec) error
	Rollback(name string, version int) error
	ListSources() ([]gitsourcedb.GitSource, error)
	PutSource(src *gitsourcedb.GitSource) error
	DeleteSource(name string) error
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
}

// New builds a Server. writeToken gates the write endpoints (PUT/rollback)
// via `Authorization: Bearer <writeToken>`; an empty writeToken disables
// writes entirely (they 403) rather than defaulting to open.
func New(store Store, writeToken string) *Server {
	return &Server{store: store, writeToken: writeToken}
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
	return mux
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

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("recipe")
	if name == "" {
		http.Error(w, "missing recipe query parameter", http.StatusBadRequest)
		return
	}

	result, err := s.store.Resolve(name)
	if err != nil {
		if errors.Is(err, recipedb.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	spec, err := s.store.Get(name)
	if err != nil {
		if errors.Is(err, recipedb.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	spec, err := s.store.GetVersion(name, version)
	if err != nil {
		if errors.Is(err, recipedb.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	versions, err := s.store.ListVersions(name)
	if err != nil {
		if errors.Is(err, recipedb.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	spec.Name = name // path is authoritative, so the body doesn't need to repeat it correctly

	if err := s.store.Put(&spec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	if err := s.store.Rollback(name, req.Version); err != nil {
		if errors.Is(err, recipedb.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListRecipes(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	sources, err := s.store.ListSources()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	src.Name = name // path is authoritative, so the body doesn't need to repeat it correctly

	if err := s.store.PutSource(&gitsourcedb.GitSource{Name: src.Name, Repo: src.Repo}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if err := s.store.DeleteSource(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
