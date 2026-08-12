// Package resolve implements configblender's resolution engine: merging a
// Recipe's ordered layers (CONCEPTION.md section 5.3) into a config tree
// while tracking per-key provenance (section 4), independent of the K8s
// controller, the Recipe database, and Git fetching (section 9) — those
// are wired around this engine, not through it.
package resolve

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"configblender/gitsource"
	"configblender/recipe"
)

// Result is the resolution's two outputs (CONCEPTION.md section 5.6):
// the config tree consumed by the application, and its provenance
// projection consumed by an operator via explain.
type Result struct {
	Config  map[string]any
	Explain map[string]any
}

// Options controls dynamic-layer execution.
type Options struct {
	Starlark StarlarkOptions
}

func DefaultOptions() Options {
	return Options{Starlark: DefaultStarlarkOptions()}
}

// Resolve merges r's layers in order (CONCEPTION.md section 5.3: order is
// semantically significant) into a single config tree, recording which
// layer produced each leaf.
func Resolve(r *recipe.Recipe, opts Options) (*Result, error) {
	var config any = map[string]any{}
	var explain any = map[string]any{}

	for _, layer := range r.Layers {
		var patch map[string]any

		switch layer.Type {
		case recipe.LayerStatic:
			if err := yaml.Unmarshal([]byte(layer.Content), &patch); err != nil {
				return nil, fmt.Errorf("layer %q: parsing static content: %w", layer.Name, err)
			}
			if patch == nil {
				patch = map[string]any{}
			}

		case recipe.LayerDynamic:
			accumulated, _ := config.(map[string]any)
			m, err := runDynamicLayer(accumulated, layer.Content, layer.Name, opts.Starlark)
			if err != nil {
				return nil, err
			}
			patch = m

		default:
			return nil, fmt.Errorf("layer %q: unknown layer type %q", layer.Name, layer.Type)
		}

		prov := Provenance{Layer: layer.Name}
		if layer.Source != (gitsource.Source{}) {
			src := layer.Source
			prov.Source = &src
		}

		config, explain = mergeValue(config, explain, patch, prov, "", r)
	}

	cfg, _ := config.(map[string]any)
	exp, _ := explain.(map[string]any)
	return &Result{Config: cfg, Explain: exp}, nil
}
