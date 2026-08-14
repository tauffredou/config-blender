// Package userdb stores configblender's own accounts (docs/07-open-questions.md
// — "UI authentication beyond a single shared token"): a username, a Role,
// and one of two credential kinds — a bcrypt password hash for a human
// account (Kind == KindHuman), or a SHA-256'd API key for a service account
// (Kind == KindService, docs/05-recipe-and-crd.md §5.3ter) meant for
// machine callers (CI, scripts) that authenticate with `Authorization:
// Bearer <api-key>` on every request rather than logging in for a session
// cookie. Both kinds live in the same store/bucket, keyed by username, and
// share the same Role vocabulary. This is deliberately separate from any
// application config/secrets a Recipe resolves — it exists only to gate who
// may call which write endpoint on the central service.
//
// A user's password or API key is never returned by Get/List, mirroring the
// write-only pattern gitsourcedb uses for Git credentials — only
// CreateServiceAccount and RotateServiceAccountKey ever return a plaintext
// API key, and only once, at the moment it's generated.
package userdb

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound is returned when a named user does not exist.
var ErrNotFound = errors.New("userdb: user not found")

// ErrAlreadyExists is returned by Create when the username is taken.
var ErrAlreadyExists = errors.New("userdb: user already exists")

// ErrInvalidCredentials is returned by Verify for either an unknown
// username or a wrong password — the two are never distinguished, so a
// failed login can't be used to enumerate valid usernames.
var ErrInvalidCredentials = errors.New("userdb: invalid username or password")

// ErrInvalidInput is wrapped by Create/SetRole/SetPassword's validation
// failures (bad username shape, empty password, unknown role) so callers
// can distinguish a client-input error (400) from an unexpected storage
// error (500) via errors.Is, without string-matching messages.
var ErrInvalidInput = errors.New("userdb: invalid input")

var usersBucket = []byte("users")

// apiKeysBucket indexes a service account's API key by its SHA-256 hash
// (the key itself is never stored) -> username, so VerifyAPIKey is an O(1)
// lookup rather than a full scan of usersBucket on every authenticated
// request.
var apiKeysBucket = []byte("api_keys")

// Role controls which write endpoints an account may call
// (internal/centralserver's requireRole) — the one RBAC model shared by
// every auth method (human userpass login, service-account API key, the
// break-glass write token), the way a HashiCorp Vault token or AppRole role
// carries the same policy vocabulary regardless of how it was obtained:
// there is no separate permission system for machine callers. There is no
// hierarchy between roles beyond RoleAdmin implicitly satisfying every
// check.
type Role string

const (
	// RoleAdmin manages users, Git sources, and Recipes — every write
	// endpoint.
	RoleAdmin Role = "admin"
	// RoleSourceManager manages the Git source registry (add/edit/delete,
	// test connections) but not Recipes or other users.
	RoleSourceManager Role = "source-manager"
	// RoleContributor writes Recipes (put/rollback) but not Git sources or
	// other users.
	RoleContributor Role = "contributor"
	// RoleRead grants no write endpoint at all — the floor of the RBAC
	// model, for a caller (typically a service account) that should only
	// ever resolve/read. Reads are already unauthenticated
	// (docs/05-recipe-and-crd.md §5.3bis), so this role's role today is
	// mostly to give such a caller an identity distinct from "anonymous"
	// (it shows up as itself, not as "token", in access logs) without
	// implicitly granting it any write — and to have a role ready for a
	// future where reads themselves become gated (docs/07-open-questions.md).
	RoleRead Role = "read"
)

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleSourceManager, RoleContributor, RoleRead:
		return true
	}
	return false
}

// Kind distinguishes a human account (username/password, logs in for a
// session cookie) from a service account (no password, authenticates via a
// long-lived API key presented as `Authorization: Bearer <api-key>` on
// every request — the Vault-token idiom for machine callers, as opposed to
// Vault's userpass method for humans). Both kinds are looked up the same
// way and carry the same Role.
type Kind string

const (
	KindHuman   Kind = "human"
	KindService Kind = "service"
)

// usernamePattern keeps usernames out of the session cookie's payload
// separator ("|", ".") and out of URL path segments needing escaping —
// internal/centralserver's session cookie format depends on "|" never
// appearing in a subject.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// User is one account, human or service. PasswordHash (bcrypt, human
// accounts) and APIKeyHash (SHA-256, service accounts) are mutually
// exclusive in practice, never the plaintext credential.
type User struct {
	Username     string    `json:"username"`
	Kind         Kind      `json:"kind"`
	PasswordHash []byte    `json:"passwordHash,omitempty"`
	APIKeyHash   []byte    `json:"apiKeyHash,omitempty"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Store persists User values, keyed by username.
type Store struct {
	db *bbolt.DB
}

// Open opens (creating if needed) a bbolt-backed store at path.
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("userdb: opening %s: %w", path, err)
	}
	store, err := NewStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// NewStore wraps an already-open bbolt.DB, creating this store's bucket if
// needed — used by internal/recipesource to share one bbolt file with
// recipedb.Store and gitsourcedb.Store rather than a separate database
// file.
func NewStore(db *bbolt.DB) (*Store, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(usersBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(apiKeysBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("userdb: initializing buckets: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Create adds a new user with a bcrypt-hashed password. Returns
// ErrAlreadyExists if username is already registered.
func (s *Store) Create(username, password string, role Role) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("userdb: invalid username %q: must match %s: %w", username, usernamePattern.String(), ErrInvalidInput)
	}
	if password == "" {
		return fmt.Errorf("userdb: password is required: %w", ErrInvalidInput)
	}
	if !role.Valid() {
		return fmt.Errorf("userdb: invalid role %q: %w", role, ErrInvalidInput)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("userdb: hashing password: %w", err)
	}
	u := User{Username: username, Kind: KindHuman, PasswordHash: hash, Role: role, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(u)
	if err != nil {
		return fmt.Errorf("userdb: encoding user %q: %w", username, err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		if b.Get([]byte(username)) != nil {
			return ErrAlreadyExists
		}
		return b.Put([]byte(username), data)
	})
}

// CreateServiceAccount registers a new machine account with the given role
// and returns its plaintext API key — generated here, hashed (SHA-256)
// before storage, and never recoverable again: same one-time-reveal
// contract as RotateServiceAccountKey. Returns ErrAlreadyExists if username
// is already registered (human or service).
func (s *Store) CreateServiceAccount(username string, role Role) (string, error) {
	if !usernamePattern.MatchString(username) {
		return "", fmt.Errorf("userdb: invalid username %q: must match %s: %w", username, usernamePattern.String(), ErrInvalidInput)
	}
	if !role.Valid() {
		return "", fmt.Errorf("userdb: invalid role %q: %w", role, ErrInvalidInput)
	}
	key, hash, err := newAPIKey()
	if err != nil {
		return "", fmt.Errorf("userdb: generating API key: %w", err)
	}
	u := User{Username: username, Kind: KindService, APIKeyHash: hash, Role: role, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(u)
	if err != nil {
		return "", fmt.Errorf("userdb: encoding user %q: %w", username, err)
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		if b.Get([]byte(username)) != nil {
			return ErrAlreadyExists
		}
		if err := b.Put([]byte(username), data); err != nil {
			return err
		}
		return tx.Bucket(apiKeysBucket).Put(hash, []byte(username))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// RotateServiceAccountKey replaces username's API key with a freshly
// generated one, immediately invalidating the old one, and returns the new
// plaintext key. Returns ErrNotFound if username is not registered, or
// ErrInvalidInput if it's a human account rather than a service account.
func (s *Store) RotateServiceAccountKey(username string) (string, error) {
	key, hash, err := newAPIKey()
	if err != nil {
		return "", fmt.Errorf("userdb: generating API key: %w", err)
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		data := b.Get([]byte(username))
		if data == nil {
			return ErrNotFound
		}
		var u User
		if err := json.Unmarshal(data, &u); err != nil {
			return err
		}
		if u.Kind != KindService {
			return fmt.Errorf("userdb: %q is not a service account: %w", username, ErrInvalidInput)
		}
		oldHash := u.APIKeyHash
		u.APIKeyHash = hash
		encoded, err := json.Marshal(u)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(username), encoded); err != nil {
			return err
		}
		if oldHash != nil {
			if err := tx.Bucket(apiKeysBucket).Delete(oldHash); err != nil {
				return err
			}
		}
		return tx.Bucket(apiKeysBucket).Put(hash, []byte(username))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// newAPIKey generates a fresh API key and returns both the plaintext key
// (shown to the caller once) and the SHA-256 hash that gets persisted — an
// API key is high-entropy by construction (unlike a human password), so a
// fast, unsalted hash is the right tool here: it's what lets VerifyAPIKey
// look a presented key up in O(1) via apiKeysBucket instead of needing to
// bcrypt-compare against every service account on every request.
func newAPIKey() (key string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	key = "cbk_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(key))
	return key, sum[:], nil
}

func (s *Store) get(username string) (*User, error) {
	var u User
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(usersBucket).Get([]byte(username))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &u)
	})
	if err != nil {
		return nil, err
	}
	normalizeKind(&u)
	return &u, nil
}

// normalizeKind defaults Kind to KindHuman for accounts persisted before
// this field existed — every account created before Kind was introduced is
// a password account, so an empty Kind on read is unambiguously human.
func normalizeKind(u *User) {
	if u.Kind == "" {
		u.Kind = KindHuman
	}
}

// Verify checks username/password and returns the user's Role on success.
// Fails with ErrInvalidCredentials for both an unknown username and a
// wrong password, deliberately not distinguishing the two — and for a
// service account, which has no password to check against (PasswordHash is
// always empty for one, so the bcrypt compare below fails harmlessly, but
// the Kind check makes that not-a-coincidence).
func (s *Store) Verify(username, password string) (Role, error) {
	u, err := s.get(username)
	if err != nil || u.Kind != KindHuman {
		return "", ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(password)) != nil {
		return "", ErrInvalidCredentials
	}
	return u.Role, nil
}

// VerifyAPIKey checks a presented API key and returns the owning service
// account's username and Role on success. Fails with ErrInvalidCredentials
// for an unknown or revoked key, deliberately not distinguishing the two —
// the same anti-enumeration posture as Verify.
func (s *Store) VerifyAPIKey(apiKey string) (string, Role, error) {
	if apiKey == "" {
		return "", "", ErrInvalidCredentials
	}
	sum := sha256.Sum256([]byte(apiKey))
	var username string
	err := s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket(apiKeysBucket).Get(sum[:])
		if v == nil {
			return ErrInvalidCredentials
		}
		username = string(v)
		return nil
	})
	if err != nil {
		return "", "", ErrInvalidCredentials
	}
	u, err := s.get(username)
	if err != nil || u.Kind != KindService {
		return "", "", ErrInvalidCredentials
	}
	return u.Username, u.Role, nil
}

// Role returns the current role for username — used to re-check a
// session's identity against live data on every request, rather than
// trusting a role embedded in the session cookie itself, so a role change
// or account deletion takes effect immediately instead of waiting out the
// session TTL.
func (s *Store) Role(username string) (Role, error) {
	u, err := s.get(username)
	if err != nil {
		return "", err
	}
	return u.Role, nil
}

// SetRole updates an existing account's role (human or service). Returns
// ErrNotFound if username is not registered.
func (s *Store) SetRole(username string, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("userdb: invalid role %q: %w", role, ErrInvalidInput)
	}
	return s.update(username, func(u *User) error { u.Role = role; return nil })
}

// SetPassword updates an existing human account's password. Returns
// ErrNotFound if username is not registered, or ErrInvalidInput if it's a
// service account — those authenticate by API key, not password, so
// changing their (nonexistent) password is a client error, not a no-op.
func (s *Store) SetPassword(username, password string) error {
	if password == "" {
		return fmt.Errorf("userdb: password is required: %w", ErrInvalidInput)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("userdb: hashing password: %w", err)
	}
	return s.update(username, func(u *User) error {
		if u.Kind != KindHuman {
			return fmt.Errorf("userdb: %q is a service account and has no password: %w", username, ErrInvalidInput)
		}
		u.PasswordHash = hash
		return nil
	})
}

func (s *Store) update(username string, mutate func(*User) error) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		data := b.Get([]byte(username))
		if data == nil {
			return ErrNotFound
		}
		var u User
		if err := json.Unmarshal(data, &u); err != nil {
			return err
		}
		normalizeKind(&u)
		if err := mutate(&u); err != nil {
			return err
		}
		encoded, err := json.Marshal(u)
		if err != nil {
			return err
		}
		return b.Put([]byte(username), encoded)
	})
}

// Delete removes the account named username, including its API-key index
// entry if it's a service account. Deleting a name that does not exist is
// not an error, matching bbolt's own Delete semantics.
func (s *Store) Delete(username string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		if data := b.Get([]byte(username)); data != nil {
			var u User
			if err := json.Unmarshal(data, &u); err == nil && u.APIKeyHash != nil {
				if err := tx.Bucket(apiKeysBucket).Delete(u.APIKeyHash); err != nil {
					return err
				}
			}
		}
		return b.Delete([]byte(username))
	})
}

// List returns every registered account (human and service), ordered by
// username. PasswordHash and APIKeyHash are always cleared — this is the
// read path, and a hash (even a bcrypt or SHA-256 one) has no business
// leaving the server.
func (s *Store) List() ([]User, error) {
	var out []User
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(usersBucket).ForEach(func(_, v []byte) error {
			var u User
			if err := json.Unmarshal(v, &u); err != nil {
				return err
			}
			normalizeKind(&u)
			u.PasswordHash = nil
			u.APIKeyHash = nil
			out = append(out, u)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Count returns the number of registered users — used at startup to decide
// whether to bootstrap an initial admin account.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.View(func(tx *bbolt.Tx) error {
		n = tx.Bucket(usersBucket).Stats().KeyN
		return nil
	})
	return n, err
}

// RandomPassword returns a random password suitable for a generated
// bootstrap account — used by cmd/server when CONFIGBLENDER_ADMIN_PASSWORD
// is not set.
func RandomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(out), nil
}
