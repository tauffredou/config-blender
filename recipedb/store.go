// Package recipedb is configblender's own database for Recipes
// (CONCEPTION.md section 9): the structure of a Recipe (which layers, in
// what order, with what merge policy) is managed here rather than in a K8s
// object, while the layers' content stays in Git (recipe.Spec.Materialize).
//
// Embedded storage (bbolt) was chosen over an external database for the
// MVP: a Recipe is accessed by name with no relational queries, and an
// embedded store needs no separate service to operate — the same
// "lightest fit for the job" reasoning already applied to Starlark for
// dynamic layers (CONCEPTION.md section 5.3).
//
// Every Put is versioned, Vault KV-v2-style (docs/05-recipe-and-crd.md
// §5.3): a write never overwrites history, it appends a new version, and
// Rollback restores an old one by writing it again as the newest version —
// there is no destructive edit. Unlike Vault there is nothing to mask in
// values here (Recipes carry no secrets, docs/04-kubernetes.md §4.1), so
// this is purely an audit/rollback mechanism, not an access-control one.
package recipedb

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.etcd.io/bbolt"

	"configblender/recipe"
)

var ErrNotFound = errors.New("recipedb: recipe not found")

var (
	recipesBucket  = []byte("recipes")
	versionsBucket = []byte("recipe_versions")
)

// VersionInfo is one entry in a Recipe's history, without the Spec body —
// what ListVersions returns to let a caller pick a version before fetching
// it in full via GetVersion.
type VersionInfo struct {
	Version   int
	UpdatedAt time.Time
}

type versionedSpec struct {
	Spec      recipe.Spec
	UpdatedAt time.Time
}

// Store persists recipe.Spec values, keyed by name, with full version
// history per name.
type Store struct {
	db *bbolt.DB
}

// Open opens (creating if needed) a bbolt-backed store at path.
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("recipedb: opening %s: %w", path, err)
	}
	store, err := NewStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// NewStore wraps an already-open bbolt.DB, creating this store's buckets
// if needed. Used by internal/recipesource to share a single bbolt file
// (and a single open *bbolt.DB) with gitsourcedb.Store rather than
// managing two separate database files — prefer Open for standalone use.
func NewStore(db *bbolt.DB) (*Store, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(recipesBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(versionsBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("recipedb: initializing buckets: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func versionKey(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func versionFromKey(k []byte) int {
	return int(binary.BigEndian.Uint64(k))
}

// Put creates or replaces the Recipe spec named spec.Name, recording it as
// a new version (the latest) rather than overwriting history.
func (s *Store) Put(spec *recipe.Spec) error {
	if spec.Name == "" {
		return errors.New("recipedb: recipe name is required")
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("recipedb: encoding recipe %q: %w", spec.Name, err)
	}

	return s.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.Bucket(recipesBucket).Put([]byte(spec.Name), data); err != nil {
			return err
		}

		nameVersions, err := tx.Bucket(versionsBucket).CreateBucketIfNotExists([]byte(spec.Name))
		if err != nil {
			return err
		}
		next := 1
		if lastKey, _ := nameVersions.Cursor().Last(); lastKey != nil {
			next = versionFromKey(lastKey) + 1
		}
		vdata, err := json.Marshal(versionedSpec{Spec: *spec, UpdatedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		return nameVersions.Put(versionKey(next), vdata)
	})
}

// Get returns the latest Recipe spec named name, or ErrNotFound.
func (s *Store) Get(name string) (*recipe.Spec, error) {
	var spec recipe.Spec
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(recipesBucket).Get([]byte(name))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &spec)
	})
	if err != nil {
		return nil, err
	}
	return &spec, nil
}

// GetVersion returns a specific historical version of the Recipe spec
// named name, or ErrNotFound if name or that version does not exist.
func (s *Store) GetVersion(name string, version int) (*recipe.Spec, error) {
	var vs versionedSpec
	err := s.db.View(func(tx *bbolt.Tx) error {
		nameVersions := tx.Bucket(versionsBucket).Bucket([]byte(name))
		if nameVersions == nil {
			return ErrNotFound
		}
		data := nameVersions.Get(versionKey(version))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &vs)
	})
	if err != nil {
		return nil, err
	}
	return &vs.Spec, nil
}

// ListVersions returns every version of the Recipe named name, oldest
// first, or ErrNotFound if name does not exist.
func (s *Store) ListVersions(name string) ([]VersionInfo, error) {
	var out []VersionInfo
	err := s.db.View(func(tx *bbolt.Tx) error {
		nameVersions := tx.Bucket(versionsBucket).Bucket([]byte(name))
		if nameVersions == nil {
			return ErrNotFound
		}
		return nameVersions.ForEach(func(k, v []byte) error {
			var vs versionedSpec
			if err := json.Unmarshal(v, &vs); err != nil {
				return err
			}
			out = append(out, VersionInfo{Version: versionFromKey(k), UpdatedAt: vs.UpdatedAt})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Rollback restores an old version of a Recipe by writing its content
// again as a new version (Vault-style: history is never rewritten, a
// rollback is itself a new, auditable version).
func (s *Store) Rollback(name string, version int) error {
	spec, err := s.GetVersion(name, version)
	if err != nil {
		return fmt.Errorf("recipedb: rolling back %q to version %d: %w", name, version, err)
	}
	return s.Put(spec)
}

// Delete removes the Recipe spec named name and its version history.
// Deleting a name that does not exist is not an error, matching bbolt's
// own Delete semantics.
func (s *Store) Delete(name string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.Bucket(recipesBucket).Delete([]byte(name)); err != nil {
			return err
		}
		err := tx.Bucket(versionsBucket).DeleteBucket([]byte(name))
		if err != nil && !errors.Is(err, bbolt.ErrBucketNotFound) {
			return err
		}
		return nil
	})
}

// List returns the names of every stored Recipe.
func (s *Store) List() ([]string, error) {
	var names []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(recipesBucket).ForEach(func(k, _ []byte) error {
			names = append(names, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}
