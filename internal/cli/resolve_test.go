package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"configblender/internal/cli"
	"configblender/internal/gitsourcedb"
	"configblender/internal/recipesource"
	"configblender/recipe"
	"configblender/resolve"
)

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

func TestResolveRecipe(t *testing.T) {
	repoDir := newTestRepo(t, map[string]string{
		"base.yaml": "port: 8080\nenv: base\n",
		"env.yaml":  "env: dev\n",
	})

	dbPath := filepath.Join(t.TempDir(), "recipes.db")
	store, err := recipesource.Open(dbPath)
	if err != nil {
		t.Fatalf("recipesource.Open: %v", err)
	}
	ctx := context.Background()
	if err := store.PutSource(ctx, "repo", &gitsourcedb.GitSource{Repo: repoDir}); err != nil {
		t.Fatalf("PutSource: %v", err)
	}
	err = store.Put(ctx, "my-app-recipe", &recipe.Spec{
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "base.yaml", Ref: "master"}},
			{Name: "env", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "env.yaml", Ref: "master"}},
		},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	store.Close()

	res, err := cli.ResolveRecipe(dbPath, "", "my-app-recipe")
	if err != nil {
		t.Fatalf("ResolveRecipe: %v", err)
	}
	if res.Config["port"] != 8080 {
		t.Errorf("Config[port] = %v, want 8080", res.Config["port"])
	}
	if got, ok := res.Explain["env"].(resolve.Provenance); !ok || got.Layer != "env" {
		t.Errorf("Explain[env] = %v, want a Provenance with layer %q", res.Explain["env"], "env")
	}
}

func TestResolveRecipe_UnknownRecipe(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recipes.db")
	store, err := recipesource.Open(dbPath)
	if err != nil {
		t.Fatalf("recipesource.Open: %v", err)
	}
	store.Close()

	if _, err := cli.ResolveRecipe(dbPath, "", "does-not-exist"); err == nil {
		t.Fatal("ResolveRecipe: expected an error for an unknown recipe, got nil")
	}
}

func TestLookupPath(t *testing.T) {
	tree := map[string]any{
		"env": "dev",
		"server": map[string]any{
			"middlewares": "base",
		},
	}

	cases := []struct {
		path   string
		want   any
		wantOK bool
	}{
		{"env", "dev", true},
		{"server.middlewares", "base", true},
		{"server.missing", nil, false},
		{"missing", nil, false},
		{"env.tooDeep", nil, false},
	}

	for _, c := range cases {
		got, ok := cli.LookupPath(tree, c.path)
		if ok != c.wantOK || got != c.want {
			t.Errorf("LookupPath(%q) = (%v, %v), want (%v, %v)", c.path, got, ok, c.want, c.wantOK)
		}
	}
}
