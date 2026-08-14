package userdb

import (
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "users.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("opening bbolt db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestServiceAccount_CreateAndVerifyAPIKey(t *testing.T) {
	s := newTestStore(t)

	apiKey, err := s.CreateServiceAccount("ci-bot", RoleRead)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if apiKey == "" {
		t.Fatal("expected a non-empty API key")
	}

	username, role, err := s.VerifyAPIKey(apiKey)
	if err != nil {
		t.Fatalf("VerifyAPIKey: %v", err)
	}
	if username != "ci-bot" || role != RoleRead {
		t.Fatalf("got (%q, %q), want (\"ci-bot\", %q)", username, role, RoleRead)
	}
}

func TestServiceAccount_VerifyAPIKey_WrongOrUnknownKey_Fails(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateServiceAccount("ci-bot", RoleRead); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	for _, key := range []string{"cbk_wrong", "", "not-a-real-key"} {
		if _, _, err := s.VerifyAPIKey(key); err != ErrInvalidCredentials {
			t.Errorf("VerifyAPIKey(%q): got %v, want ErrInvalidCredentials", key, err)
		}
	}
}

func TestServiceAccount_RotateKey_InvalidatesOldKey(t *testing.T) {
	s := newTestStore(t)
	oldKey, err := s.CreateServiceAccount("ci-bot", RoleContributor)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	newKey, err := s.RotateServiceAccountKey("ci-bot")
	if err != nil {
		t.Fatalf("RotateServiceAccountKey: %v", err)
	}
	if newKey == oldKey {
		t.Fatal("rotated key should differ from the original")
	}

	if _, _, err := s.VerifyAPIKey(oldKey); err != ErrInvalidCredentials {
		t.Errorf("old key still verifies: %v", err)
	}
	username, role, err := s.VerifyAPIKey(newKey)
	if err != nil || username != "ci-bot" || role != RoleContributor {
		t.Errorf("new key verify: got (%q, %q, %v)", username, role, err)
	}
}

func TestServiceAccount_RotateKey_OnHumanAccount_Fails(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create("alice", "hunter2", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.RotateServiceAccountKey("alice"); err == nil {
		t.Fatal("expected an error rotating a human account's (nonexistent) API key")
	}
}

func TestServiceAccount_PasswordLoginFails(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateServiceAccount("ci-bot", RoleRead); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if _, err := s.Verify("ci-bot", ""); err != ErrInvalidCredentials {
		t.Errorf("Verify on a service account: got %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceAccount_SetPassword_Fails(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateServiceAccount("ci-bot", RoleRead); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if err := s.SetPassword("ci-bot", "whatever"); err == nil {
		t.Fatal("expected SetPassword to fail on a service account")
	}
}

func TestServiceAccount_Delete_RevokesKeyImmediately(t *testing.T) {
	s := newTestStore(t)
	apiKey, err := s.CreateServiceAccount("ci-bot", RoleRead)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if err := s.Delete("ci-bot"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := s.VerifyAPIKey(apiKey); err != ErrInvalidCredentials {
		t.Errorf("deleted account's key still verifies: %v", err)
	}
}

func TestList_IncludesKindAndNeverLeaksCredentials(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create("alice", "hunter2", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.CreateServiceAccount("ci-bot", RoleRead); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	users, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := make(map[string]User, len(users))
	for _, u := range users {
		if u.PasswordHash != nil || u.APIKeyHash != nil {
			t.Errorf("List leaked a credential hash for %q", u.Username)
		}
		byName[u.Username] = u
	}
	if byName["alice"].Kind != KindHuman {
		t.Errorf("alice.Kind = %q, want %q", byName["alice"].Kind, KindHuman)
	}
	if byName["ci-bot"].Kind != KindService {
		t.Errorf("ci-bot.Kind = %q, want %q", byName["ci-bot"].Kind, KindService)
	}
}

func TestRoleRead_Valid(t *testing.T) {
	if !RoleRead.Valid() {
		t.Fatal("RoleRead should be a valid role")
	}
}
