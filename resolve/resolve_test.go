package resolve

import (
	"testing"

	"configblender/recipe"
)

// S1 — Résolution hiérarchique de base (CONCEPTION.md scénario S1).
func TestResolve_HierarchicalBase(t *testing.T) {
	r := &recipe.Recipe{
		Layers: []recipe.Layer{
			{Name: "base", Type: recipe.LayerStatic, Content: "port: 8080\nenv: base\n"},
			{Name: "dev", Type: recipe.LayerStatic, Content: "env: dev\n"},
		},
	}

	res, err := Resolve(r, DefaultOptions())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := res.Config["port"]; got != 8080 {
		t.Errorf("port = %v (%T), want 8080 (inherited from base)", got, got)
	}
	if got := res.Config["env"]; got != "dev" {
		t.Errorf("env = %v, want %q (overridden by dev)", got, "dev")
	}
}

// S2 — Merge déclaratif d'une liste (CONCEPTION.md scénario S2): la même
// paire de couches produit un résultat différent selon la stratégie
// déclarée pour la clé, sans toucher au moteur.
func TestResolve_DeclarativeListMerge(t *testing.T) {
	layers := []recipe.Layer{
		{Name: "base", Type: recipe.LayerStatic, Content: "items: [apple, banana, cherries]\n"},
		{Name: "override", Type: recipe.LayerStatic, Content: "items: [banana, dango]\n"},
	}

	cases := []struct {
		strategy recipe.MergeStrategy
		want     []any
	}{
		{recipe.StrategyReplace, []any{"banana", "dango"}},
		{recipe.StrategyUnion, []any{"apple", "banana", "cherries", "dango"}},
		{recipe.StrategyAppend, []any{"apple", "banana", "cherries", "banana", "dango"}},
	}

	for _, c := range cases {
		t.Run(string(c.strategy), func(t *testing.T) {
			r := &recipe.Recipe{
				Layers:      layers,
				MergePolicy: []recipe.MergeRule{{Path: "items", Strategy: c.strategy}},
			}
			res, err := Resolve(r, DefaultOptions())
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			got, _ := res.Config["items"].([]any)
			if !equalAny(got, c.want) {
				t.Errorf("items = %v, want %v", got, c.want)
			}
		})
	}
}

// S3 — Couche dynamique dépendante de l'état accumulé (CONCEPTION.md
// scénario S3, reprend l'exemple env/foo de la section 5.3).
func TestResolve_DynamicLayerReadsAccumulatedState(t *testing.T) {
	r := &recipe.Recipe{
		Layers: []recipe.Layer{
			{Name: "base", Type: recipe.LayerStatic, Content: "env: dev\n"},
			{
				Name: "computed",
				Type: recipe.LayerDynamic,
				Content: `
res = {}
if env in ["dev", "staging"]:
    res["foo"] = "bar"
`,
			},
		},
	}

	res, err := Resolve(r, DefaultOptions())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := res.Config["env"]; got != "dev" {
		t.Errorf("env = %v, want dev", got)
	}
	if got := res.Config["foo"]; got != "bar" {
		t.Errorf("foo = %v, want bar", got)
	}
}

// S4 — Provenance d'une valeur (CONCEPTION.md scénario S4): l'explain
// indique la couche source exacte, y compris pour une couche dynamique.
func TestResolve_Explain(t *testing.T) {
	r := &recipe.Recipe{
		Layers: []recipe.Layer{
			{Name: "base", Type: recipe.LayerStatic, Content: "port: 8080\nenv: base\n"},
			{Name: "dev", Type: recipe.LayerStatic, Content: "env: dev\n"},
			{
				Name:    "computed",
				Type:    recipe.LayerDynamic,
				Content: "res = {'foo': 'bar'}\n",
			},
		},
	}

	res, err := Resolve(r, DefaultOptions())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := map[string]any{
		"port": "base",
		"env":  "dev",
		"foo":  "computed",
	}
	for key, wantLayer := range want {
		got, ok := res.Explain[key].(Provenance)
		if !ok {
			t.Errorf("explain[%q] = %v (%T), want a Provenance", key, res.Explain[key], res.Explain[key])
			continue
		}
		if got.Layer != wantLayer {
			t.Errorf("explain[%q].Layer = %v, want %q", key, got.Layer, wantLayer)
		}
	}
}

// Une couche dynamique qui dépasse la limite de pas d'exécution doit être
// interrompue plutôt que de bloquer la résolution indéfiniment
// (CONCEPTION.md section 5.4 — protection contre l'épuisement de
// ressources).
func TestResolve_DynamicLayerStepLimit(t *testing.T) {
	r := &recipe.Recipe{
		Layers: []recipe.Layer{
			{
				Name: "runaway",
				Type: recipe.LayerDynamic,
				Content: `
res = {}
for i in range(100000000):
    res["x"] = i
`,
			},
		},
	}

	opts := DefaultOptions()
	opts.Starlark.MaxSteps = 1000

	if _, err := Resolve(r, opts); err == nil {
		t.Fatal("Resolve: expected an error from the step limit, got nil")
	}
}

func equalAny(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
