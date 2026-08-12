package gitsourcedb_test

import (
	"errors"
	"path/filepath"
	"testing"

	"configblender/internal/gitsourcedb"
)

func openTestStore(t *testing.T) *gitsourcedb.Store {
	t.Helper()
	s, err := gitsourcedb.Open(filepath.Join(t.TempDir(), "sources.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_PutGet(t *testing.T) {
	s := openTestStore(t)

	src := &gitsourcedb.GitSource{Name: "internal-configs", Repo: "https://example.invalid/config.git"}
	if err := s.Put(src); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get("internal-configs")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Repo != src.Repo {
		t.Errorf("Get: Repo = %q, want %q", got.Repo, src.Repo)
	}
}

func TestStore_PutGet_RoundTripsCredentials(t *testing.T) {
	s := openTestStore(t)

	src := &gitsourcedb.GitSource{
		Name: "internal-configs",
		Repo: "https://example.invalid/config.git",
		Auth: &gitsourcedb.Credentials{Username: "x-access-token", Password: "s3cr3t"},
	}
	if err := s.Put(src); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get("internal-configs")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Auth == nil || got.Auth.Username != "x-access-token" || got.Auth.Password != "s3cr3t" {
		t.Errorf("Get: Auth = %+v, want the stored credential", got.Auth)
	}
}

func TestStore_PutGet_NoCredentialsStoresNilAuth(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put(&gitsourcedb.GitSource{Name: "public-repo", Repo: "https://example.invalid/public.git"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get("public-repo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Auth != nil {
		t.Errorf("Get: Auth = %+v, want nil (no credentials stored)", got.Auth)
	}
}

func TestStore_Put_RequiresNameAndRepo(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put(&gitsourcedb.GitSource{Repo: "https://example.invalid/x.git"}); err == nil {
		t.Error("Put: expected an error for a missing name, got nil")
	}
	if err := s.Put(&gitsourcedb.GitSource{Name: "x"}); err == nil {
		t.Error("Put: expected an error for a missing repo, got nil")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.Get("does-not-exist"); !errors.Is(err, gitsourcedb.ErrNotFound) {
		t.Errorf("Get: err = %v, want ErrNotFound", err)
	}
}

func TestStore_List(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put(&gitsourcedb.GitSource{Name: "b", Repo: "https://example.invalid/b.git"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(&gitsourcedb.GitSource{Name: "a", Repo: "https://example.invalid/a.git"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List: len = %d, want 2", len(got))
	}
}

func TestStore_Delete(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put(&gitsourcedb.GitSource{Name: "a", Repo: "https://example.invalid/a.git"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("a"); !errors.Is(err, gitsourcedb.ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}

	// Deleting a name that doesn't exist is not an error.
	if err := s.Delete("does-not-exist"); err != nil {
		t.Errorf("Delete: unexpected error for a non-existent name: %v", err)
	}
}

func TestStore_ResolveSource(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put(&gitsourcedb.GitSource{Name: "a", Repo: "https://example.invalid/a.git"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	repo, err := s.ResolveSource("a")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if repo != "https://example.invalid/a.git" {
		t.Errorf("ResolveSource: %q, want %q", repo, "https://example.invalid/a.git")
	}

	if _, err := s.ResolveSource("missing"); err == nil {
		t.Error("ResolveSource: expected an error for an unregistered name, got nil")
	}
}

func TestStore_LookupByRepo(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put(&gitsourcedb.GitSource{Name: "a", Repo: "https://example.invalid/a.git"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	src, ok := s.LookupByRepo("https://example.invalid/a.git")
	if !ok || src.Name != "a" {
		t.Errorf("LookupByRepo: got (%+v, %v), want (name=%q, true)", src, ok, "a")
	}

	if _, ok := s.LookupByRepo("https://example.invalid/unknown.git"); ok {
		t.Error("LookupByRepo: expected ok=false for an unregistered URL")
	}
}
