package recipedb

import (
	"errors"
	"path/filepath"
	"testing"

	"configblender/recipe"
)

func testSpec(name string) *recipe.Spec {
	return &recipe.Spec{
		Name: name,
		Layers: []recipe.LayerSpec{
			{
				Name: "base",
				Type: recipe.LayerStatic,
				Source: recipe.LayerSource{
					SourceRef: "internal-configs",
					Path:      "base.yaml",
					Ref:       "main",
				},
			},
		},
		MergePolicy: []recipe.MergeRule{
			{Path: "featureFlags", Strategy: recipe.StrategyUnion},
		},
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "recipes.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_PutGet(t *testing.T) {
	s := openTestStore(t)
	spec := testSpec("my-app-recipe")

	if err := s.Put(spec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get("my-app-recipe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != spec.Name || len(got.Layers) != 1 || got.Layers[0].Source.SourceRef != spec.Layers[0].Source.SourceRef {
		t.Errorf("Get = %+v, want a round-trip of %+v", got, spec)
	}
	if len(got.MergePolicy) != 1 || got.MergePolicy[0].Strategy != recipe.StrategyUnion {
		t.Errorf("Get MergePolicy = %+v, want the union rule preserved", got.MergePolicy)
	}
}

func TestStore_GetMissing(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Get("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
}

func TestStore_PutReplaces(t *testing.T) {
	s := openTestStore(t)
	spec := testSpec("my-app-recipe")
	if err := s.Put(spec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	spec.Layers = append(spec.Layers, recipe.LayerSpec{Name: "env", Type: recipe.LayerStatic})
	if err := s.Put(spec); err != nil {
		t.Fatalf("Put (replace): %v", err)
	}

	got, err := s.Get("my-app-recipe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Layers) != 2 {
		t.Errorf("Get after replace: len(Layers) = %d, want 2", len(got.Layers))
	}
}

func TestStore_DeleteAndList(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(testSpec("a")); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := s.Put(testSpec("b")); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	names, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("List = %v, want 2 names", names)
	}

	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	names, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 1 || names[0] != "b" {
		t.Errorf("List after delete = %v, want [b]", names)
	}
}

func TestStore_VersionHistory(t *testing.T) {
	s := openTestStore(t)
	spec := testSpec("my-app-recipe")

	if err := s.Put(spec); err != nil {
		t.Fatalf("Put (v1): %v", err)
	}
	spec.Layers = append(spec.Layers, recipe.LayerSpec{Name: "env", Type: recipe.LayerStatic})
	if err := s.Put(spec); err != nil {
		t.Fatalf("Put (v2): %v", err)
	}

	versions, err := s.ListVersions("my-app-recipe")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("ListVersions = %+v, want versions [1, 2]", versions)
	}

	v1, err := s.GetVersion("my-app-recipe", 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if len(v1.Layers) != 1 {
		t.Errorf("GetVersion(1).Layers = %v, want the original single layer", v1.Layers)
	}

	v2, err := s.GetVersion("my-app-recipe", 2)
	if err != nil {
		t.Fatalf("GetVersion(2): %v", err)
	}
	if len(v2.Layers) != 2 {
		t.Errorf("GetVersion(2).Layers = %v, want 2 layers", v2.Layers)
	}
}

func TestStore_GetVersion_Missing(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(testSpec("my-app-recipe")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := s.GetVersion("my-app-recipe", 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVersion(unknown version) error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetVersion("does-not-exist", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVersion(unknown recipe) error = %v, want ErrNotFound", err)
	}
}

func TestStore_Rollback(t *testing.T) {
	s := openTestStore(t)
	spec := testSpec("my-app-recipe")
	if err := s.Put(spec); err != nil {
		t.Fatalf("Put (v1): %v", err)
	}

	spec.Layers = append(spec.Layers, recipe.LayerSpec{Name: "env", Type: recipe.LayerStatic})
	if err := s.Put(spec); err != nil {
		t.Fatalf("Put (v2): %v", err)
	}

	if err := s.Rollback("my-app-recipe", 1); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Rollback must not rewrite history: it appends a new (third) version
	// carrying v1's content, rather than mutating v1 or v2 in place.
	got, err := s.Get("my-app-recipe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Layers) != 1 {
		t.Errorf("Get after rollback: Layers = %v, want 1 (v1's content)", got.Layers)
	}

	versions, err := s.ListVersions("my-app-recipe")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 3 || versions[2].Version != 3 {
		t.Fatalf("ListVersions after rollback = %+v, want 3 versions, the last being 3", versions)
	}
}

func TestStore_DeleteRemovesVersionHistory(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(testSpec("my-app-recipe")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete("my-app-recipe"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.ListVersions("my-app-recipe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListVersions after delete: error = %v, want ErrNotFound", err)
	}

	// Deleting a name with no history at all must not error either.
	if err := s.Delete("never-existed"); err != nil {
		t.Errorf("Delete(never-existed): %v, want nil", err)
	}
}
