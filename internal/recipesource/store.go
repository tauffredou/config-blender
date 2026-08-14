// Package recipesource is configblender's service layer: it combines
// recipedb (Recipe structure), gitsourcedb (the registry of preconfigured
// Git sources) and gitsource (layer content) into business operations —
// Resolve, Put, Rollback, ... — used by both the CLI (internal/cli) and the
// central HTTP service (internal/centralserver), kept as a long-lived store
// here rather than opening/closing the database on every call
// (docs/05-recipe-and-crd.md §5.2). Recipes and Git sources share one bbolt
// file (recipedb.NewStore/gitsourcedb.NewStore over one *bbolt.DB,
// different buckets) rather than needing two database files to operate.
//
// This package owns business rules (e.g. a Recipe's name is whatever the
// caller addressed it by, not whatever a stored spec's body claims) and
// error semantics (ErrNotFound) so that transport-layer callers —
// internal/centralserver's HTTP handlers, internal/cli's commands — stay
// thin adapters that never need to know recipedb exists.
package recipesource

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"

	"go.etcd.io/bbolt"

	"configblender/gitsource"
	"configblender/internal/gitauth"
	"configblender/internal/gitsourcedb"
	"configblender/internal/userdb"
	"configblender/recipe"
	"configblender/recipedb"
	"configblender/resolve"
)

// ErrNotFound is returned (wrapped) when a named Recipe does not exist.
// Re-exported from recipedb so callers depend on this package's error
// vocabulary rather than reaching past it into the storage layer —
// errors.Is still works since it's the same sentinel underneath.
var ErrNotFound = recipedb.ErrNotFound

// metaBucket holds small, single-value operational state that doesn't
// belong to any one domain store — currently just the session-signing
// secret (SessionSecret).
var metaBucket = []byte("meta")

var sessionSecretKey = []byte("session-secret")

type Store struct {
	rawDB   *bbolt.DB
	db      *recipedb.Store
	sources *gitsourcedb.Store
	users   *userdb.Store
	fetcher *gitsource.Fetcher
	log     *slog.Logger
}

// Option configures a Store built by Open.
type Option func(*Store)

// WithLogger overrides the default (slog.Default()) logger used for
// business events: resolve failures, writes, rollbacks.
func WithLogger(l *slog.Logger) Option {
	return func(s *Store) { s.log = l }
}

// Open opens the bbolt file at dbPath — shared by the Recipe database and
// the Git-source registry — and prepares a Git fetcher whose credentials
// are resolved per registered source, always from the registry itself
// (internal/gitauth.FromSources, docs/04-kubernetes.md §4.1) — never from
// the environment.
func Open(dbPath string, opts ...Option) (*Store, error) {
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
	users, err := userdb.NewStore(rawDB)
	if err != nil {
		rawDB.Close()
		return nil, err
	}
	if err := rawDB.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(metaBucket)
		return err
	}); err != nil {
		rawDB.Close()
		return nil, fmt.Errorf("recipesource: initializing meta bucket: %w", err)
	}
	auth := gitauth.FromSources(sources.LookupByRepo)

	s := &Store{
		rawDB:   rawDB,
		db:      db,
		sources: sources,
		users:   users,
		fetcher: gitsource.NewFetcher(auth),
		log:     slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.rawDB.Close()
}

// Ping reports whether the underlying database is reachable — cheap enough
// to call from an HTTP readiness probe.
func (s *Store) Ping() error {
	return s.rawDB.View(func(tx *bbolt.Tx) error { return nil })
}

// SessionSecret returns the random key internal/centralserver signs
// session cookies with, generating and persisting one on first call if
// none exists yet. Persisted (rather than generated fresh per process) so
// restarting the central service doesn't log out every browser session;
// unlike the write token, this secret is never operator-supplied, so a
// session stays unforgeable even when CONFIGBLENDER_WRITE_TOKEN is unset.
func (s *Store) SessionSecret(ctx context.Context) ([]byte, error) {
	var secret []byte
	err := s.rawDB.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(metaBucket)
		if v := b.Get(sessionSecretKey); v != nil {
			secret = append([]byte(nil), v...)
			return nil
		}
		generated := make([]byte, 32)
		if _, err := rand.Read(generated); err != nil {
			return fmt.Errorf("generating session secret: %w", err)
		}
		if err := b.Put(sessionSecretKey, generated); err != nil {
			return err
		}
		secret = generated
		return nil
	})
	return secret, err
}

// Put creates or replaces the Recipe spec addressed by name, recording it as
// a new version (recipedb). name is authoritative over spec.Name — a caller
// addresses a Recipe by name (a URL path segment, a CLI argument) and that
// address wins over whatever a decoded request body or file happens to
// claim. Local only, by design — GitOps is the only write path for v1
// (docs/05-recipe-and-crd.md §5.3): internal/cli calls this directly
// against a local database; internal/centralserver never exposes it over
// the network.
func (s *Store) Put(ctx context.Context, name string, spec *recipe.Spec) error {
	spec.Name = name
	if err := s.db.Put(spec); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "recipe put", "recipe", name)
	return nil
}

// Get returns the latest stored Spec for name, or ErrNotFound.
func (s *Store) Get(ctx context.Context, name string) (*recipe.Spec, error) {
	return s.db.Get(name)
}

// GetVersion returns a specific historical version of the Spec named
// name (docs/05-recipe-and-crd.md §5.3 — Vault-KV-v2-style history).
func (s *Store) GetVersion(ctx context.Context, name string, version int) (*recipe.Spec, error) {
	return s.db.GetVersion(name, version)
}

// ListVersions returns every version of the Recipe named name.
func (s *Store) ListVersions(ctx context.Context, name string) ([]recipedb.VersionInfo, error) {
	return s.db.ListVersions(name)
}

// Rollback restores an old version of a Recipe as a new version. Local
// only, same as Put — it is a write.
func (s *Store) Rollback(ctx context.Context, name string, version int) error {
	if err := s.db.Rollback(name, version); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "recipe rolled back", "recipe", name, "version", version)
	return nil
}

// List returns the names of every stored Recipe.
func (s *Store) List(ctx context.Context) ([]string, error) {
	return s.db.List()
}

// Delete removes the Recipe spec named name and its version history.
// Local only, same as Put.
func (s *Store) Delete(ctx context.Context, name string) error {
	if err := s.db.Delete(name); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "recipe deleted", "recipe", name)
	return nil
}

// PutSource registers or replaces the Git source addressed by name
// (internal/gitsourcedb), including its credentials if src.Auth is set —
// internal/gitauth.FromSources always resolves a fetch's credentials from
// here (docs/04-kubernetes.md §4.1). name is authoritative over src.Name,
// same rule and same reason as Put.
func (s *Store) PutSource(ctx context.Context, name string, src *gitsourcedb.GitSource) error {
	src.Name = name
	if err := s.sources.Put(src); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "git source put", "source", name)
	return nil
}

// GetSource returns the registered Git source named name.
func (s *Store) GetSource(ctx context.Context, name string) (*gitsourcedb.GitSource, error) {
	return s.sources.Get(name)
}

// ListSources returns every registered Git source.
func (s *Store) ListSources(ctx context.Context) ([]gitsourcedb.GitSource, error) {
	return s.sources.List()
}

// DeleteSource removes the registered Git source named name.
func (s *Store) DeleteSource(ctx context.Context, name string) error {
	if err := s.sources.Delete(name); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "git source deleted", "source", name)
	return nil
}

// TestSourceConnection checks that repo is reachable with auth (which may
// be nil, for unauthenticated access) — used by the webui's Sources admin
// screen to let an operator verify a repo URL and credentials, including
// ones not yet saved, before committing them with PutSource. It never
// touches the database: this is a pure connectivity/credential check.
func (s *Store) TestSourceConnection(ctx context.Context, repo string, auth *gitsourcedb.Credentials) error {
	method, err := gitauth.AuthMethod(auth)
	if err != nil {
		return err
	}
	if err := gitsource.TestConnection(ctx, repo, method); err != nil {
		s.log.WarnContext(ctx, "git source connection test failed", "repo", repo, "error", err)
		return err
	}
	return nil
}

// CreateUser registers a new human account with the given role
// (internal/userdb.Role) — admin-only over the API
// (internal/centralserver's requireRole).
func (s *Store) CreateUser(ctx context.Context, username, password string, role userdb.Role) error {
	if err := s.users.Create(username, password, role); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "user created", "username", username, "role", role)
	return nil
}

// CreateServiceAccount registers a new machine account and returns its
// plaintext API key — shown once, same one-time-reveal contract as
// RotateServiceAccountKey. Admin-only over the API.
func (s *Store) CreateServiceAccount(ctx context.Context, username string, role userdb.Role) (string, error) {
	key, err := s.users.CreateServiceAccount(username, role)
	if err != nil {
		return "", err
	}
	s.log.InfoContext(ctx, "service account created", "username", username, "role", role)
	return key, nil
}

// RotateServiceAccountKey replaces username's API key, immediately
// invalidating the old one, and returns the new plaintext key.
func (s *Store) RotateServiceAccountKey(ctx context.Context, username string) (string, error) {
	key, err := s.users.RotateServiceAccountKey(username)
	if err != nil {
		return "", err
	}
	s.log.InfoContext(ctx, "service account key rotated", "username", username)
	return key, nil
}

// ListUsers returns every registered account, human and service (without
// password/API-key hashes).
func (s *Store) ListUsers(ctx context.Context) ([]userdb.User, error) {
	return s.users.List()
}

// SetUserRole changes username's role.
func (s *Store) SetUserRole(ctx context.Context, username string, role userdb.Role) error {
	if err := s.users.SetRole(username, role); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "user role changed", "username", username, "role", role)
	return nil
}

// SetUserPassword changes username's password.
func (s *Store) SetUserPassword(ctx context.Context, username, password string) error {
	if err := s.users.SetPassword(username, password); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "user password changed", "username", username)
	return nil
}

// DeleteUser removes the account named username.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	if err := s.users.Delete(username); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "user deleted", "username", username)
	return nil
}

// VerifyUser checks a login attempt, returning the account's role on
// success.
func (s *Store) VerifyUser(ctx context.Context, username, password string) (userdb.Role, error) {
	return s.users.Verify(username, password)
}

// VerifyAPIKey checks a service account's API key, returning its username
// and role on success — the per-request bearer-token auth path for machine
// callers, as opposed to VerifyUser's session-oriented login.
func (s *Store) VerifyAPIKey(ctx context.Context, apiKey string) (string, userdb.Role, error) {
	return s.users.VerifyAPIKey(apiKey)
}

// UserRole returns username's current role — used on every authenticated
// request (not just at login) so a role change or account deletion takes
// effect immediately rather than waiting out the session's TTL.
func (s *Store) UserRole(ctx context.Context, username string) (userdb.Role, error) {
	return s.users.Role(username)
}

// CountUsers returns the number of registered accounts — cmd/server uses
// this at startup to decide whether to bootstrap an initial admin.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	return s.users.Count()
}

// Resolve loads the named Recipe, materializes its layers from Git, and
// resolves it (docs/05-recipe-and-crd.md §5.2/§5.3). ctx bounds the Git
// fetch, the only network I/O in this path.
func (s *Store) Resolve(ctx context.Context, name string) (*resolve.Result, error) {
	spec, err := s.db.Get(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("loading recipe %q: %w", name, err)
	}

	r, err := spec.Materialize(ctx, s.fetcher, s.sources)
	if err != nil {
		s.log.ErrorContext(ctx, "recipe resolve failed", "recipe", name, "error", err)
		return nil, fmt.Errorf("materializing recipe %q: %w", name, err)
	}

	result, err := resolve.Resolve(r, resolve.DefaultOptions())
	if err != nil {
		s.log.ErrorContext(ctx, "recipe resolve failed", "recipe", name, "error", err)
		return nil, err
	}
	return result, nil
}
