// Package gitsourcedb stores the registry of preconfigured Git sources
// (docs/05-recipe-and-crd.md §5.2, docs/07-open-questions.md): a named
// {name, repo URL} pair that a Recipe layer references instead of
// embedding a raw repo URL directly. No credentials are stored here —
// internal/gitauth resolves those live, per source, from the environment
// (FromSources) — so this store only ever holds non-secret identity.
//
// Unlike recipedb, sources carry no version history: they're operational
// config (which repos configblender is allowed to read from), not
// something an operator audits or rolls back.
package gitsourcedb

import (
	"encoding/json"
	"errors"
	"fmt"

	"go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("gitsourcedb: source not found")

var sourcesBucket = []byte("gitsources")

// GitSource is one registered Git repository, referenced by name from a
// recipe.LayerSpec's LayerSource.SourceRef.
type GitSource struct {
	Name string `json:"name" yaml:"name"`
	Repo string `json:"repo" yaml:"repo"`
}

// Store persists GitSource values, keyed by name.
type Store struct {
	db *bbolt.DB
}

// Open opens (creating if needed) a bbolt-backed store at path.
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("gitsourcedb: opening %s: %w", path, err)
	}
	store, err := NewStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// NewStore wraps an already-open bbolt.DB, creating this store's bucket if
// needed. Used by internal/recipesource to share a single bbolt file (and
// a single open *bbolt.DB) with recipedb.Store rather than managing two
// separate database files — prefer Open for standalone use.
func NewStore(db *bbolt.DB) (*Store, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(sourcesBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("gitsourcedb: initializing buckets: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Put creates or replaces the GitSource named src.Name.
func (s *Store) Put(src *GitSource) error {
	if src.Name == "" {
		return errors.New("gitsourcedb: source name is required")
	}
	if src.Repo == "" {
		return errors.New("gitsourcedb: source repo is required")
	}
	data, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("gitsourcedb: encoding source %q: %w", src.Name, err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(sourcesBucket).Put([]byte(src.Name), data)
	})
}

// Get returns the GitSource named name, or ErrNotFound.
func (s *Store) Get(name string) (*GitSource, error) {
	var src GitSource
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(sourcesBucket).Get([]byte(name))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &src)
	})
	if err != nil {
		return nil, err
	}
	return &src, nil
}

// Delete removes the GitSource named name. Deleting a name that does not
// exist is not an error, matching bbolt's own Delete semantics.
func (s *Store) Delete(name string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(sourcesBucket).Delete([]byte(name))
	})
}

// List returns every registered GitSource, ordered by name.
func (s *Store) List() ([]GitSource, error) {
	var out []GitSource
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(sourcesBucket).ForEach(func(_, v []byte) error {
			var src GitSource
			if err := json.Unmarshal(v, &src); err != nil {
				return err
			}
			out = append(out, src)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveSource returns the repo URL registered under name — satisfies
// recipe.SourceResolver directly, so *Store can be passed straight to
// recipe.Spec.Materialize.
func (s *Store) ResolveSource(name string) (string, error) {
	src, err := s.Get(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf("gitsourcedb: no git source registered as %q", name)
		}
		return "", err
	}
	return src.Repo, nil
}

// LookupByRepo reverse-looks-up a repo URL to the name of the source that
// registered it, used by internal/gitauth.FromSources to find which
// source's credentials apply to a given fetch. Bucket keys are names, not
// URLs, so this is a linear scan — fine at the size (a handful of
// registered repos) and cadence (one lookup per Git fetch) this runs at.
func (s *Store) LookupByRepo(repoURL string) (name string, ok bool) {
	sources, err := s.List()
	if err != nil {
		return "", false
	}
	for _, src := range sources {
		if src.Repo == repoURL {
			return src.Name, true
		}
	}
	return "", false
}
