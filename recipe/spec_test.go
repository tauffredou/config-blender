package recipe_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"configblender/gitsource"
	"configblender/recipe"
	"configblender/resolve"
)

// newTestRepo creates a local, single-commit git repository containing the
// given files, so Materialize can be tested without network access.
func newTestRepo(t *testing.T, files map[string]string) string {
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
	for path, content := range files {
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
	}
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir
}

// mapResolver is a trivial recipe.SourceResolver for tests, so recipe's
// own tests don't need to depend on internal/gitsourcedb.
type mapResolver map[string]string

func (m mapResolver) ResolveSource(name string) (string, error) {
	url, ok := m[name]
	if !ok {
		return "", fmt.Errorf("no source registered as %q", name)
	}
	return url, nil
}

// End-to-end: a Spec stored with Git-referenced layers materializes into a
// Recipe whose resolution matches resolving the same layers inline
// (CONCEPTION.md section 9 — the Recipe database only holds structure, Git
// holds content).
func TestSpec_MaterializeThenResolve(t *testing.T) {
	repoDir := newTestRepo(t, map[string]string{
		"base.yaml": "port: 8080\nenv: base\n",
		"env.yaml":  "env: dev\n",
	})

	spec := &recipe.Spec{
		Name: "my-app-recipe",
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "base.yaml", Ref: "master"}},
			{Name: "env", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "env.yaml", Ref: "master"}},
		},
	}

	r, err := spec.Materialize(context.Background(), gitsource.NewFetcher(nil), mapResolver{"repo": repoDir})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if r.Name != spec.Name {
		t.Errorf("Materialize: Name = %q, want %q", r.Name, spec.Name)
	}

	res, err := resolve.Resolve(r, resolve.DefaultOptions())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Config["port"] != 8080 {
		t.Errorf("port = %v, want 8080", res.Config["port"])
	}
	if res.Config["env"] != "dev" {
		t.Errorf("env = %v, want dev", res.Config["env"])
	}
	got, ok := res.Explain["env"].(resolve.Provenance)
	if !ok || got.Layer != "env" {
		t.Errorf("explain[env] = %v, want a Provenance with layer %q", res.Explain["env"], "env")
	}
	if got.Source == nil || got.Source.Repo != repoDir || got.Source.Path != "env.yaml" {
		t.Errorf("explain[env].Source = %v, want the Git source of the env layer (repo=%q, path=%q)", got.Source, repoDir, "env.yaml")
	}
}

func TestSpec_MaterializeMissingFileFails(t *testing.T) {
	repoDir := newTestRepo(t, map[string]string{"base.yaml": "port: 8080\n"})

	spec := &recipe.Spec{
		Name: "broken",
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "missing.yaml", Ref: "master"}},
		},
	}

	if _, err := spec.Materialize(context.Background(), gitsource.NewFetcher(nil), mapResolver{"repo": repoDir}); err == nil {
		t.Fatal("Materialize: expected an error for a missing file, got nil")
	}
}

func TestSpec_MaterializeUnknownSourceRefFails(t *testing.T) {
	spec := &recipe.Spec{
		Name: "broken",
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "does-not-exist", Path: "base.yaml", Ref: "master"}},
		},
	}

	if _, err := spec.Materialize(context.Background(), gitsource.NewFetcher(nil), mapResolver{}); err == nil {
		t.Fatal("Materialize: expected an error for an unregistered SourceRef, got nil")
	}
}
