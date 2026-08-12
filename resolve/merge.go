package resolve

import (
	"fmt"

	"configblender/recipe"
)

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// mergeValue merges patch into (base, baseExplain) at path according to r's
// declarative merge policy (CONCEPTION.md section 3): maps are always
// merged recursively, scalars are always overridden, and lists follow the
// strategy declared for path (replace by default). It returns the merged
// value and its provenance projection, mirroring the same shape
// (docs/02-resolution-model.md §2.2/§2.3) with every touched leaf
// attributed to prov.
func mergeValue(base, baseExplain, patch any, prov Provenance, path string, r *recipe.Recipe) (any, any) {
	switch p := patch.(type) {
	case map[string]any:
		b, _ := base.(map[string]any)
		be, _ := baseExplain.(map[string]any)
		outVal := make(map[string]any, len(b)+len(p))
		outExp := make(map[string]any, len(b)+len(p))
		for k, v := range b {
			outVal[k] = v
			outExp[k] = be[k]
		}
		for k, pv := range p {
			childPath := joinPath(path, k)
			mv, me := mergeValue(outVal[k], outExp[k], pv, prov, childPath, r)
			outVal[k] = mv
			outExp[k] = me
		}
		return outVal, outExp

	case []any:
		switch r.MergeStrategyFor(path) {
		case recipe.StrategyUnion:
			return unionLists(asList(base), p), prov
		case recipe.StrategyAppend:
			out := append([]any{}, asList(base)...)
			return append(out, p...), prov
		default: // StrategyReplace
			return p, prov
		}

	default:
		// Scalar, or nil: the later layer always wins (section 3).
		return p, prov
	}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// unionLists concatenates a and b, dropping duplicates by their string
// representation (sufficient for the scalar-heavy lists this targets —
// feature flags, allowed origins, plugin names).
func unionLists(a, b []any) []any {
	out := make([]any, 0, len(a)+len(b))
	seen := make(map[string]bool, len(a)+len(b))
	add := func(v any) {
		key := fmt.Sprintf("%v", v)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, v)
	}
	for _, v := range a {
		add(v)
	}
	for _, v := range b {
		add(v)
	}
	return out
}
