// Package userdb stores configblender's own user accounts
// (docs/07-open-questions.md — "UI authentication beyond a single shared
// token"): a username, a bcrypt password hash, and a Role. This is
// deliberately separate from any application config/secrets a Recipe
// resolves — it exists only to gate who may call which write endpoint on
// the central service.
//
// A user's password is never returned by Get/List, mirroring the
// write-only pattern gitsourcedb uses for Git credentials.
package userdb

import (
	"crypto/rand"
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

// Role controls which write endpoints a user may call
// (internal/centralserver's requireRole). There is no hierarchy between
// roles beyond RoleAdmin implicitly satisfying every check.
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
)

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleSourceManager, RoleContributor:
		return true
	}
	return false
}

// usernamePattern keeps usernames out of the session cookie's payload
// separator ("|", ".") and out of URL path segments needing escaping —
// internal/centralserver's session cookie format depends on "|" never
// appearing in a subject.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// User is one account. PasswordHash is bcrypt, never the plaintext
// password.
type User struct {
	Username     string    `json:"username"`
	PasswordHash []byte    `json:"passwordHash,omitempty"`
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
		_, err := tx.CreateBucketIfNotExists(usersBucket)
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
	u := User{Username: username, PasswordHash: hash, Role: role, CreatedAt: time.Now().UTC()}
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
	return &u, nil
}

// Verify checks username/password and returns the user's Role on success.
// Fails with ErrInvalidCredentials for both an unknown username and a
// wrong password, deliberately not distinguishing the two.
func (s *Store) Verify(username, password string) (Role, error) {
	u, err := s.get(username)
	if err != nil {
		return "", ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(password)) != nil {
		return "", ErrInvalidCredentials
	}
	return u.Role, nil
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

// SetRole updates an existing user's role. Returns ErrNotFound if username
// is not registered.
func (s *Store) SetRole(username string, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("userdb: invalid role %q: %w", role, ErrInvalidInput)
	}
	return s.update(username, func(u *User) { u.Role = role })
}

// SetPassword updates an existing user's password. Returns ErrNotFound if
// username is not registered.
func (s *Store) SetPassword(username, password string) error {
	if password == "" {
		return fmt.Errorf("userdb: password is required: %w", ErrInvalidInput)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("userdb: hashing password: %w", err)
	}
	return s.update(username, func(u *User) { u.PasswordHash = hash })
}

func (s *Store) update(username string, mutate func(*User)) error {
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
		mutate(&u)
		encoded, err := json.Marshal(u)
		if err != nil {
			return err
		}
		return b.Put([]byte(username), encoded)
	})
}

// Delete removes the user named username. Deleting a name that does not
// exist is not an error, matching bbolt's own Delete semantics.
func (s *Store) Delete(username string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(usersBucket).Delete([]byte(username))
	})
}

// List returns every registered user, ordered by username. PasswordHash is
// always cleared — this is the read path, and a hash (even a bcrypt one)
// has no business leaving the server.
func (s *Store) List() ([]User, error) {
	var out []User
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(usersBucket).ForEach(func(_, v []byte) error {
			var u User
			if err := json.Unmarshal(v, &u); err != nil {
				return err
			}
			u.PasswordHash = nil
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
