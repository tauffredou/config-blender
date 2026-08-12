package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configblenderv1alpha1 "configblender/api/v1alpha1"
	"configblender/internal/controller"
	"configblender/internal/gitsourcedb"
	"configblender/internal/recipesource"
	"configblender/recipe"
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

func newTestStore(t *testing.T, recipeName, repoDir string) *recipesource.Store {
	t.Helper()
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
	err = store.Put(ctx, recipeName, &recipe.Spec{
		Layers: []recipe.LayerSpec{
			{Name: "base", Type: recipe.LayerStatic, Source: recipe.LayerSource{SourceRef: "repo", Path: "base.yaml", Ref: "master"}},
		},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	return store
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme: %v", err)
	}
	if err := configblenderv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("configblenderv1alpha1.AddToScheme: %v", err)
	}
	return scheme
}

func TestReconcile_CreatesConfigMap(t *testing.T) {
	repoDir := newTestRepo(t, map[string]string{"base.yaml": "port: 8080\nenv: dev\n"})
	store := newTestStore(t, "my-app-recipe", repoDir)
	scheme := newScheme(t)

	cb := &configblenderv1alpha1.ConfigBlend{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app-config", Namespace: "my-app"},
		Spec: configblenderv1alpha1.ConfigBlendSpec{
			Recipe: "my-app-recipe",
			Target: configblenderv1alpha1.ConfigBlendTarget{ConfigMapName: "my-app-config"},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&configblenderv1alpha1.ConfigBlend{}).
		WithObjects(cb).
		Build()

	r := &controller.ConfigBlendReconciler{Client: c, Scheme: scheme, Recipes: store}

	res, err := r.Reconcile(context.Background(), reconcileRequest(cb))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter != controller.DefaultRefreshInterval {
		t.Errorf("RequeueAfter = %v, want default %v", res.RequeueAfter, controller.DefaultRefreshInterval)
	}

	var cm corev1.ConfigMap
	if err := c.Get(context.Background(), types.NamespacedName{Name: "my-app-config", Namespace: "my-app"}, &cm); err != nil {
		t.Fatalf("getting ConfigMap: %v", err)
	}
	if got := cm.Data["config.yaml"]; got == "" {
		t.Fatal("ConfigMap data[config.yaml] is empty")
	} else {
		t.Logf("config.yaml:\n%s", got)
	}
	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Name != "my-app-config" {
		t.Errorf("OwnerReferences = %v, want a controller ref to the ConfigBlend", cm.OwnerReferences)
	}

	var updated configblenderv1alpha1.ConfigBlend
	if err := c.Get(context.Background(), reconcileRequest(cb).NamespacedName, &updated); err != nil {
		t.Fatalf("getting ConfigBlend: %v", err)
	}
	if len(updated.Status.Conditions) != 1 || updated.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("Status.Conditions = %+v, want a single Ready=True condition", updated.Status.Conditions)
	}
}

func TestReconcile_UpdatesConfigMapOnRecipeChange(t *testing.T) {
	repoDir := newTestRepo(t, map[string]string{"base.yaml": "port: 8080\n"})
	store := newTestStore(t, "my-app-recipe", repoDir)
	scheme := newScheme(t)

	cb := &configblenderv1alpha1.ConfigBlend{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app-config", Namespace: "my-app"},
		Spec: configblenderv1alpha1.ConfigBlendSpec{
			Recipe: "my-app-recipe",
			Target: configblenderv1alpha1.ConfigBlendTarget{ConfigMapName: "my-app-config"},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&configblenderv1alpha1.ConfigBlend{}).
		WithObjects(cb).
		Build()
	r := &controller.ConfigBlendReconciler{Client: c, Scheme: scheme, Recipes: store}

	if _, err := r.Reconcile(context.Background(), reconcileRequest(cb)); err != nil {
		t.Fatalf("Reconcile (first): %v", err)
	}

	// Simulate a new commit landing on the layer's repo, as a refreshInterval
	// tick (docs/04-kubernetes.md §4.3) would observe on the next reconcile.
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, _ := repo.Worktree()
	if err := os.WriteFile(filepath.Join(repoDir, "base.yaml"), []byte("port: 9090\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	wt.Add("base.yaml")
	wt.Commit("update", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})

	if _, err := r.Reconcile(context.Background(), reconcileRequest(cb)); err != nil {
		t.Fatalf("Reconcile (second): %v", err)
	}

	var cm corev1.ConfigMap
	if err := c.Get(context.Background(), types.NamespacedName{Name: "my-app-config", Namespace: "my-app"}, &cm); err != nil {
		t.Fatalf("getting ConfigMap: %v", err)
	}
	if got := cm.Data["config.yaml"]; !containsLine(got, "port: 9090") {
		t.Errorf("config.yaml after update = %q, want it to contain %q", got, "port: 9090")
	}
}

func TestReconcile_ResolveFailureRequeuesWithoutError(t *testing.T) {
	repoDir := newTestRepo(t, map[string]string{"base.yaml": "port: 8080\n"})
	// Recipe name in the CR does not match anything stored — a common
	// misconfiguration this scenario exercises deliberately.
	store := newTestStore(t, "some-other-recipe", repoDir)
	scheme := newScheme(t)

	cb := &configblenderv1alpha1.ConfigBlend{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app-config", Namespace: "my-app"},
		Spec: configblenderv1alpha1.ConfigBlendSpec{
			Recipe: "does-not-exist",
			Target: configblenderv1alpha1.ConfigBlendTarget{ConfigMapName: "my-app-config"},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&configblenderv1alpha1.ConfigBlend{}).
		WithObjects(cb).
		Build()
	r := &controller.ConfigBlendReconciler{Client: c, Scheme: scheme, Recipes: store}

	res, err := r.Reconcile(context.Background(), reconcileRequest(cb))
	if err != nil {
		t.Fatalf("Reconcile: expected no error (requeue instead), got %v", err)
	}
	if res.RequeueAfter != controller.DefaultRefreshInterval {
		t.Errorf("RequeueAfter = %v, want default %v", res.RequeueAfter, controller.DefaultRefreshInterval)
	}

	var cm corev1.ConfigMap
	err = c.Get(context.Background(), types.NamespacedName{Name: "my-app-config", Namespace: "my-app"}, &cm)
	if !isNotFound(err) {
		t.Errorf("ConfigMap should not have been created on resolve failure, got err=%v", err)
	}

	var updated configblenderv1alpha1.ConfigBlend
	if err := c.Get(context.Background(), reconcileRequest(cb).NamespacedName, &updated); err != nil {
		t.Fatalf("getting ConfigBlend: %v", err)
	}
	if len(updated.Status.Conditions) != 1 || updated.Status.Conditions[0].Status != metav1.ConditionFalse {
		t.Errorf("Status.Conditions = %+v, want a single Ready=False condition", updated.Status.Conditions)
	}
}

func reconcileRequest(cb *configblenderv1alpha1.ConfigBlend) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: cb.Name, Namespace: cb.Namespace}}
}

func containsLine(s, substr string) bool {
	return len(s) > 0 && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func isNotFound(err error) bool {
	return err != nil && client.IgnoreNotFound(err) == nil
}
