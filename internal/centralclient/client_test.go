package centralclient_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"configblender/internal/centralclient"
	"configblender/internal/centralserver"
	"configblender/internal/gitsourcedb"
	"configblender/internal/recipesource"
	"configblender/recipe"
)

func newTestRepo(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, "base.yaml"), []byte("port: 8080\nenv: dev\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := wt.Add("base.yaml"); err != nil {
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

const testWriteToken = "test-token"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	repoDir := newTestRepo(t)

	dbPath := filepath.Join(t.TempDir(), "recipes.db")
	store, err := recipesource.Open(dbPath)
	if err != nil {
		t.Fatalf("recipesource.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	if err := store.PutSource(ctx, "repo", &gitsourcedb.GitSource{Repo: repoDir}); err != nil {
		t.Fatalf("PutSource: %v", err)
	}
	spec := &recipe.Spec{
		Name: "my-app-recipe",
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "base.yaml", Ref: "master"}},
		},
	}
	if err := store.Put(ctx, spec.Name, spec); err != nil {
		t.Fatalf("Put (v1): %v", err)
	}
	// A second version (layers untouched) so version-history tests have
	// something to list beyond a single entry.
	spec.MergePolicy = []recipe.MergeRule{{Path: "items", Strategy: recipe.StrategyUnion}}
	if err := store.Put(ctx, spec.Name, spec); err != nil {
		t.Fatalf("Put (v2): %v", err)
	}

	srv := httptest.NewServer(centralserver.New(store, testWriteToken).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_Resolve(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	result, err := client.Resolve(context.Background(), "my-app-recipe")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Config["port"] != float64(8080) {
		t.Errorf("Config[port] = %v (%T), want 8080 (JSON numbers decode as float64)", result.Config["port"], result.Config["port"])
	}
	// A network-resolved Explain leaf decodes from JSON as a plain
	// map[string]any (docs/07-open-questions.md), not a resolve.Provenance
	// struct — the "layer"/"source" keys are still there, just untyped.
	env, ok := result.Explain["env"].(map[string]any)
	if !ok {
		t.Fatalf("Explain[env] = %v (%T), want a map", result.Explain["env"], result.Explain["env"])
	}
	if env["layer"] != "base" {
		t.Errorf("Explain[env][layer] = %v, want %q", env["layer"], "base")
	}
	source, ok := env["source"].(map[string]any)
	if !ok {
		t.Fatalf("Explain[env][source] = %v (%T), want a map (redirection to the Git source)", env["source"], env["source"])
	}
	if source["path"] != "base.yaml" {
		t.Errorf("Explain[env][source][path] = %v, want %q", source["path"], "base.yaml")
	}
}

func TestClient_Resolve_UnknownRecipe(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	if _, err := client.Resolve(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("Resolve: expected an error for an unknown recipe, got nil")
	}
}

func TestClient_Get(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	spec, err := client.Get(context.Background(), "my-app-recipe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if spec.Name != "my-app-recipe" || len(spec.Layers) != 1 || spec.Layers[0].Name != "base" {
		t.Errorf("Get = %+v, want the stored my-app-recipe spec", spec)
	}
}

func TestClient_Get_UnknownRecipe(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	if _, err := client.Get(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("Get: expected an error for an unknown recipe, got nil")
	}
}

func TestClient_List(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	names, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 1 || names[0] != "my-app-recipe" {
		t.Errorf("List = %v, want [my-app-recipe]", names)
	}
}

func TestClient_ListVersions(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	versions, err := client.ListVersions(context.Background(), "my-app-recipe")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("ListVersions = %+v, want versions [1, 2]", versions)
	}
	if versions[0].UpdatedAt.IsZero() {
		t.Error("ListVersions[0].UpdatedAt is zero, want a timestamp")
	}
}

func TestClient_GetVersion(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	v1, err := client.GetVersion(context.Background(), "my-app-recipe", 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if len(v1.MergePolicy) != 0 {
		t.Errorf("GetVersion(1).MergePolicy = %v, want empty (v1 predates it)", v1.MergePolicy)
	}

	v2, err := client.GetVersion(context.Background(), "my-app-recipe", 2)
	if err != nil {
		t.Fatalf("GetVersion(2): %v", err)
	}
	if len(v2.MergePolicy) != 1 || v2.MergePolicy[0].Strategy != recipe.StrategyUnion {
		t.Errorf("GetVersion(2).MergePolicy = %v, want the union rule added in v2", v2.MergePolicy)
	}
}

func TestClient_GetVersion_Unknown(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	if _, err := client.GetVersion(context.Background(), "my-app-recipe", 99); err == nil {
		t.Fatal("GetVersion: expected an error for an unknown version, got nil")
	}
}

func TestClient_Put(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil).WithToken(testWriteToken)

	spec := &recipe.Spec{
		Name: "my-app-recipe",
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "base.yaml", Ref: "main"}},
		},
	}
	if err := client.Put(context.Background(), spec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	versions, err := client.ListVersions(context.Background(), "my-app-recipe")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("ListVersions = %+v, want 3 versions after Put (2 seeded + 1 new)", versions)
	}
}

func TestClient_Put_WithoutToken(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil) // no WithToken

	spec := &recipe.Spec{Name: "my-app-recipe"}
	if err := client.Put(context.Background(), spec); err == nil {
		t.Fatal("Put: expected an error without a token, got nil")
	}
}

func TestClient_Put_WrongToken(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil).WithToken("wrong-token")

	spec := &recipe.Spec{Name: "my-app-recipe"}
	if err := client.Put(context.Background(), spec); err == nil {
		t.Fatal("Put: expected an error with the wrong token, got nil")
	}
}

func TestClient_Rollback(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil).WithToken(testWriteToken)

	if err := client.Rollback(context.Background(), "my-app-recipe", 1); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	latest, err := client.Get(context.Background(), "my-app-recipe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(latest.MergePolicy) != 0 {
		t.Errorf("Get after rollback to v1 = %+v, want empty MergePolicy (v1 predates it)", latest.MergePolicy)
	}

	versions, err := client.ListVersions(context.Background(), "my-app-recipe")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("ListVersions = %+v, want 3 versions after rollback (history is never rewritten)", versions)
	}
}

func TestClient_Rollback_WithoutToken(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	if err := client.Rollback(context.Background(), "my-app-recipe", 1); err == nil {
		t.Fatal("Rollback: expected an error without a token, got nil")
	}
}

func TestClient_ListSources(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	sources, err := client.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "repo" {
		t.Errorf("ListSources = %+v, want the one source seeded by newTestServer", sources)
	}
}

func TestClient_PutSource(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil).WithToken(testWriteToken)

	if err := client.PutSource(context.Background(), "second", &gitsourcedb.GitSource{Repo: "https://example.invalid/second.git"}); err != nil {
		t.Fatalf("PutSource: %v", err)
	}

	sources, err := client.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("ListSources = %+v, want 2 sources after PutSource", sources)
	}
}

func TestClient_PutSource_WithoutToken(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	if err := client.PutSource(context.Background(), "second", &gitsourcedb.GitSource{Repo: "https://example.invalid/second.git"}); err == nil {
		t.Fatal("PutSource: expected an error without a token, got nil")
	}
}

func TestClient_DeleteSource(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil).WithToken(testWriteToken)

	if err := client.DeleteSource(context.Background(), "repo"); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}

	sources, err := client.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("ListSources after DeleteSource = %+v, want empty", sources)
	}
}

func TestClient_DeleteSource_WithoutToken(t *testing.T) {
	srv := newTestServer(t)
	client := centralclient.New(srv.URL, nil)

	if err := client.DeleteSource(context.Background(), "repo"); err == nil {
		t.Fatal("DeleteSource: expected an error without a token, got nil")
	}
}
