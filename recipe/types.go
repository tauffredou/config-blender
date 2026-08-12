// Package recipe defines the declarative structure of a configblender Recipe:
// an ordered stack of configuration layers plus the merge policy applied
// when resolving them (see CONCEPTION.md, sections 3, 5.3 and 9).
package recipe

import "configblender/gitsource"

// LayerType distinguishes a static layer (YAML/JSON content) from a
// dynamic layer (a Starlark script evaluated against the config
// accumulated by previous layers).
type LayerType string

const (
	LayerStatic  LayerType = "static"
	LayerDynamic LayerType = "dynamic"
)

// Layer is one entry in a Recipe's ordered layer stack.
type Layer struct {
	// Name identifies the layer; it is also the provenance label reported
	// in the explain output (docs/02-resolution-model.md §2.3).
	Name string
	Type LayerType
	// Content is the layer's source text: YAML/JSON for a static layer,
	// Starlark source for a dynamic layer. In the target architecture this
	// content is fetched from Git (CONCEPTION.md section 9); for the MVP
	// engine it is passed in directly so the resolver has no I/O dependency.
	Content string
	// Source is where Content came from, when it came from Git
	// (Spec.Materialize sets this) — carried into the resolver's
	// provenance so explain can point at the actual source, not just the
	// layer name (docs/02-resolution-model.md §2.3). Zero value if unset.
	Source gitsource.Source
}

// MergeStrategy is the declarative merge behavior for a list-valued key
// (CONCEPTION.md section 3). It has no effect on scalar or map values,
// which are always overridden resp. recursively merged.
type MergeStrategy string

const (
	// StrategyReplace: the later layer's list fully replaces the earlier one.
	// This is the implicit default for any path not listed in the MergePolicy.
	StrategyReplace MergeStrategy = "replace"
	// StrategyUnion: lists are concatenated and deduplicated.
	StrategyUnion MergeStrategy = "union"
	// StrategyAppend: lists are concatenated, duplicates preserved, order kept.
	StrategyAppend MergeStrategy = "append"
)

// MergeRule declares the merge strategy for one key path.
type MergeRule struct {
	// Path is a dot-separated key path, e.g. "featureFlags" or "server.middlewares".
	Path     string        `yaml:"path" json:"path"`
	Strategy MergeStrategy `yaml:"strategy" json:"strategy"`
}

// Recipe is the ordered layer stack plus its merge policy — the unit
// configblender resolves into a config tree (CONCEPTION.md section 9).
type Recipe struct {
	Name        string
	Layers      []Layer
	MergePolicy []MergeRule
}

// MergeStrategyFor returns the declared strategy for path, defaulting to
// StrategyReplace when the path has no explicit rule (CONCEPTION.md section 3).
func (r *Recipe) MergeStrategyFor(path string) MergeStrategy {
	for _, rule := range r.MergePolicy {
		if rule.Path == path {
			return rule.Strategy
		}
	}
	return StrategyReplace
}
