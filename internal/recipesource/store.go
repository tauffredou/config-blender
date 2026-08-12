// Package recipesource combines recipedb (Recipe structure), gitsourcedb
// (the registry of preconfigured Git sources) and gitsource (layer content)
// into a single Resolve(name) call — the same chain the CLI (internal/cli)
// and the K8s controller (internal/controller) both need, kept as a
// long-lived store here rather than opening/closing the database on every
// call (docs/05-recipe-and-crd.md §5.2). Recipes and Git sources share one
// bbolt file (recipedb.NewStore/gitsourcedb.NewStore over one *bbolt.DB,
// different buckets) rather than needing two database files to operate.
package recipesource

import (
	"fmt"

	"go.etcd.io/bbolt"

	"configblender/gitsource"
	"configblender/internal/gitauth"
	"configblender/internal/gitsourcedb"
	"configblender/recipe"
	"configblender/recipedb"
	"configblender/resolve"
)

type Store struct {
	rawDB   *bbolt.DB
	db      *recipedb.Store
	sources *gitsourcedb.Store
	fetcher *gitsource.Fetcher
}

// Open opens the bbolt file at dbPath — shared by the Recipe database and
// the Git-source registry — and prepares a Git fetcher whose credentials
// are resolved per registered source (internal/gitauth.FromSources): a
// source with no per-source credentials in the environment falls back to
// the single global credential, same as before this registry existed.
func Open(dbPath string) (*Store, error) {
	rawDB, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("recipesource: opening %s: %w", dbPath, err)
	}

	db, err := recipedb.NewStore(rawDB)
	if err != nil {
		rawDB.Close()
		return nil, err
	}
	sources, err := gitsourcedb.NewStore(rawDB)
	if err != nil {
		rawDB.Close()
		return nil, err
	}
	auth, err := gitauth.FromSources(sources.LookupByRepo)
	if err != nil {
		rawDB.Close()
		return nil, err
	}
	return &Store{rawDB: rawDB, db: db, sources: sources, fetcher: gitsource.NewFetcher(auth)}, nil
}

func (s *Store) Close() error {
	return s.rawDB.Close()
}

// Put creates or replaces the Recipe spec named spec.Name, recording it as
// a new version (recipedb). Local only, by design — GitOps is the only
// write path for v1 (docs/05-recipe-and-crd.md §5.3): internal/cli calls
// this directly against a local database; internal/centralserver never
// exposes it over the network.
func (s *Store) Put(spec *recipe.Spec) error {
	return s.db.Put(spec)
}

// Get returns the latest stored Spec for name, or recipedb.ErrNotFound.
func (s *Store) Get(name string) (*recipe.Spec, error) {
	return s.db.Get(name)
}

// GetVersion returns a specific historical version of the Spec named
// name (docs/05-recipe-and-crd.md §5.3 — Vault-KV-v2-style history).
func (s *Store) GetVersion(name string, version int) (*recipe.Spec, error) {
	return s.db.GetVersion(name, version)
}

// ListVersions returns every version of the Recipe named name.
func (s *Store) ListVersions(name string) ([]recipedb.VersionInfo, error) {
	return s.db.ListVersions(name)
}

// Rollback restores an old version of a Recipe as a new version. Local
// only, same as Put — it is a write.
func (s *Store) Rollback(name string, version int) error {
	return s.db.Rollback(name, version)
}

// List returns the names of every stored Recipe.
func (s *Store) List() ([]string, error) {
	return s.db.List()
}

// Delete removes the Recipe spec named name and its version history.
// Local only, same as Put.
func (s *Store) Delete(name string) error {
	return s.db.Delete(name)
}

// PutSource registers or replaces the Git source named src.Name
// (internal/gitsourcedb) — no credentials involved, just its identity
// (name, repo URL); internal/gitauth.FromSources resolves that source's
// credentials from the environment at fetch time.
func (s *Store) PutSource(src *gitsourcedb.GitSource) error {
	return s.sources.Put(src)
}

// GetSource returns the registered Git source named name.
func (s *Store) GetSource(name string) (*gitsourcedb.GitSource, error) {
	return s.sources.Get(name)
}

// ListSources returns every registered Git source.
func (s *Store) ListSources() ([]gitsourcedb.GitSource, error) {
	return s.sources.List()
}

// DeleteSource removes the registered Git source named name.
func (s *Store) DeleteSource(name string) error {
	return s.sources.Delete(name)
}

// Resolve loads the named Recipe, materializes its layers from Git, and
// resolves it (docs/05-recipe-and-crd.md §5.2/§5.3).
func (s *Store) Resolve(name string) (*resolve.Result, error) {
	spec, err := s.db.Get(name)
	if err != nil {
		return nil, fmt.Errorf("loading recipe %q: %w", name, err)
	}

	r, err := spec.Materialize(s.fetcher, s.sources)
	if err != nil {
		return nil, fmt.Errorf("materializing recipe %q: %w", name, err)
	}

	return resolve.Resolve(r, resolve.DefaultOptions())
}
