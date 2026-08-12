// Package cli holds the logic behind configblender's CLI subcommands,
// factored out of cmd/configblender/main.go so it can be tested without
// spawning a subprocess.
package cli

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"configblender/internal/centralclient"
	"configblender/internal/gitsourcedb"
	"configblender/internal/recipesource"
	"configblender/recipe"
	"configblender/recipedb"
	"configblender/resolve"
)

// RecipeStore is what `resolve`/`explain`/`list`/`get`/`history` need to
// consult Recipes — satisfied by a local internal/recipesource.Store or a
// remote internal/centralclient.Client (docs/05-recipe-and-crd.md §5.3:
// consultation works either way; creating/changing a Recipe is GitOps
// only for v1, so no equivalent write interface exists here — `put` and
// `rollback` go straight to internal/recipedb, local only).
type RecipeStore interface {
	Resolve(name string) (*resolve.Result, error)
	Get(name string) (*recipe.Spec, error)
	GetVersion(name string, version int) (*recipe.Spec, error)
	ListVersions(name string) ([]recipedb.VersionInfo, error)
	List() ([]string, error)
}

// openStore opens exactly one of a local Recipe database (dbPath) or a
// central service client (centralURL), mirroring cmd/manager's
// --recipe-db/--central-url split.
func openStore(dbPath, centralURL string) (RecipeStore, func() error, error) {
	if (dbPath == "") == (centralURL == "") {
		return nil, nil, fmt.Errorf("exactly one of --db or --central-url must be set")
	}

	if centralURL != "" {
		return centralclient.New(centralURL, nil), func() error { return nil }, nil
	}

	store, err := recipesource.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return store, store.Close, nil
}

// ResolveRecipe resolves recipeName from a local database or the central
// service — the same path a controller's reconcile loop follows to produce
// a ConfigMap (docs/04-kubernetes.md), used here for `configblender
// resolve`/`explain` (docs/05-recipe-and-crd.md §5.3, consulted directly
// rather than as a second ConfigMap).
func ResolveRecipe(dbPath, centralURL, recipeName string) (*resolve.Result, error) {
	store, closeStore, err := openStore(dbPath, centralURL)
	if err != nil {
		return nil, err
	}
	defer closeStore()

	return store.Resolve(recipeName)
}

// ListRecipeNames returns every Recipe name known to a local database or
// the central service.
func ListRecipeNames(dbPath, centralURL string) ([]string, error) {
	store, closeStore, err := openStore(dbPath, centralURL)
	if err != nil {
		return nil, err
	}
	defer closeStore()

	return store.List()
}

// GetRecipeSpec returns the declarative Spec of recipeName — the latest
// version, or a specific one if version > 0 (docs/05-recipe-and-crd.md
// §5.3 — Vault-KV-v2-style history). Pairs with PutRecipe for an edit
// workflow: get, edit the YAML, put.
func GetRecipeSpec(dbPath, centralURL, recipeName string, version int) (*recipe.Spec, error) {
	store, closeStore, err := openStore(dbPath, centralURL)
	if err != nil {
		return nil, err
	}
	defer closeStore()

	if version > 0 {
		return store.GetVersion(recipeName, version)
	}
	return store.Get(recipeName)
}

// ListRecipeVersions returns the version history of recipeName.
func ListRecipeVersions(dbPath, centralURL, recipeName string) ([]recipedb.VersionInfo, error) {
	store, closeStore, err := openStore(dbPath, centralURL)
	if err != nil {
		return nil, err
	}
	defer closeStore()

	return store.ListVersions(recipeName)
}

// SourceStore is what `source list`/`source put`/`source delete` need —
// satisfied by a local internal/recipesource.Store (sharing the same
// bbolt file as Recipes) or a remote internal/centralclient.Client.
type SourceStore interface {
	ListSources() ([]gitsourcedb.GitSource, error)
	PutSource(src *gitsourcedb.GitSource) error
	DeleteSource(name string) error
}

// openSourceStore opens exactly one of a local Recipe database (dbPath —
// the Git-source registry lives in the same file) or a central service
// client (centralURL).
func openSourceStore(dbPath, centralURL string) (SourceStore, func() error, error) {
	if (dbPath == "") == (centralURL == "") {
		return nil, nil, fmt.Errorf("exactly one of --db or --central-url must be set")
	}

	if centralURL != "" {
		return centralclient.New(centralURL, nil), func() error { return nil }, nil
	}

	store, err := recipesource.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return store, store.Close, nil
}

// ListSources returns every registered Git source from a local database or
// the central service.
func ListSources(dbPath, centralURL string) ([]gitsourcedb.GitSource, error) {
	store, closeStore, err := openSourceStore(dbPath, centralURL)
	if err != nil {
		return nil, err
	}
	defer closeStore()

	return store.ListSources()
}

// PutSource registers or replaces a Git source in a local database or the
// central service.
func PutSource(dbPath, centralURL string, src *gitsourcedb.GitSource) error {
	store, closeStore, err := openSourceStore(dbPath, centralURL)
	if err != nil {
		return err
	}
	defer closeStore()

	return store.PutSource(src)
}

// DeleteSource removes a Git source from a local database or the central
// service.
func DeleteSource(dbPath, centralURL, name string) error {
	store, closeStore, err := openSourceStore(dbPath, centralURL)
	if err != nil {
		return err
	}
	defer closeStore()

	return store.DeleteSource(name)
}

// LookupPath descends a dot-separated key path (e.g. "server.middlewares")
// into tree, as used by `configblender explain --key`. It returns
// (nil, false) if the path does not exist.
func LookupPath(tree map[string]any, path string) (any, bool) {
	var cur any = tree
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[part]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// PutRecipe reads a recipe.Spec from the YAML file at specPath and stores
// it under dbPath. Local only, deliberately: for v1 the only way to
// create or change a Recipe is GitOps (docs/05-recipe-and-crd.md §5.3) —
// applying a Spec file to the store directly, not a network write call.
func PutRecipe(dbPath, specPath string) error {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var spec recipe.Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return err
	}

	store, err := recipedb.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	return store.Put(&spec)
}

// RollbackRecipe restores an old version of recipeName as the new latest
// version (recipedb.Store.Rollback — history is never rewritten). Local
// only, same reasoning as PutRecipe: it is a write.
func RollbackRecipe(dbPath, recipeName string, version int) error {
	store, err := recipedb.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	return store.Rollback(recipeName, version)
}
