package gitsource

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newTestRepo creates a local git repository with a single commit on
// "main" containing path -> content, so tests exercise the Fetcher against
// a real (if local) repository with no network access.
func newTestRepo(t *testing.T, path, content string) string {
	t.Helper()
	dir := t.TempDir()

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := wt.Add(path); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	return dir
}

func TestFetcher_Content(t *testing.T) {
	repoDir := newTestRepo(t, "layers/env-dev.yaml", "env: dev\n")

	f := NewFetcher(nil)
	got, err := f.Content(context.Background(), Source{Repo: repoDir, Path: "layers/env-dev.yaml", Ref: "master"})
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if got != "env: dev\n" {
		t.Errorf("Content = %q, want %q", got, "env: dev\n")
	}
}

func TestFetcher_MissingRefFails(t *testing.T) {
	repoDir := newTestRepo(t, "layers/base.yaml", "port: 8080\n")

	f := NewFetcher(nil)
	if _, err := f.Content(context.Background(), Source{Repo: repoDir, Path: "layers/base.yaml"}); err == nil {
		t.Fatal("Content: expected an error for a missing ref, got nil")
	}
}

func TestFetcher_UpdatesOnSubsequentFetch(t *testing.T) {
	repoDir := newTestRepo(t, "layers/base.yaml", "port: 8080\n")

	f := NewFetcher(nil)
	first, err := f.Content(context.Background(), Source{Repo: repoDir, Path: "layers/base.yaml", Ref: "master"})
	if err != nil {
		t.Fatalf("Content (first): %v", err)
	}
	if first != "port: 8080\n" {
		t.Fatalf("Content (first) = %q, want %q", first, "port: 8080\n")
	}

	// Simulate a new commit landing on the same repo, as a periodic
	// refreshInterval fetch (section 5.5) would observe.
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "layers/base.yaml"), []byte("port: 9090\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := wt.Add("layers/base.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := wt.Commit("update", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	second, err := f.Content(context.Background(), Source{Repo: repoDir, Path: "layers/base.yaml", Ref: "master"})
	if err != nil {
		t.Fatalf("Content (second): %v", err)
	}
	if second != "port: 9090\n" {
		t.Errorf("Content (second) = %q, want %q (fetch should have picked up the new commit)", second, "port: 9090\n")
	}
}

func TestTestConnection_Reachable(t *testing.T) {
	repoDir := newTestRepo(t, "layers/base.yaml", "port: 8080\n")

	if err := TestConnection(context.Background(), repoDir, nil); err != nil {
		t.Errorf("TestConnection: %v, want nil (repo is reachable)", err)
	}
}

func TestTestConnection_Unreachable(t *testing.T) {
	if err := TestConnection(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), nil); err == nil {
		t.Fatal("TestConnection: expected an error for an unreachable repo, got nil")
	}
}
