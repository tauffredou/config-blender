package cli

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"configblender/gitsource"
	"configblender/resolve"
)

// FormatAnnotated renders cfg (a resolve.Result's Config, or a subtree of
// it reached via LookupPath) as an indented, YAML-like tree in which every
// leaf is followed by a comment naming the layer — and, when available,
// the exact Git source — that produced it (docs/02-resolution-model.md
// §2.3's "annotated view": the same (Config, Explain) pair as the raw
// explain dump, folded into one human-readable tree instead of two). explain
// must be the matching subtree of the same Result's Explain.
func FormatAnnotated(cfg, explain any) string {
	var b strings.Builder
	writeAnnotated(&b, "", cfg, explain, 0)
	return strings.TrimRight(b.String(), "\n")
}

func writeAnnotated(b *strings.Builder, key string, cfg, explain any, indent int) {
	pad := strings.Repeat("  ", indent)

	switch v := cfg.(type) {
	case map[string]any:
		exp, _ := explain.(map[string]any)
		if key != "" {
			fmt.Fprintf(b, "%s%s:\n", pad, key)
			indent++
			pad = strings.Repeat("  ", indent)
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeAnnotated(b, k, v[k], exp[k], indent)
		}

	case []any:
		header := pad + key + ":"
		fmt.Fprintln(b, withProvenanceComment(header, explain))
		for _, item := range v {
			fmt.Fprintf(b, "%s  - %s\n", pad, formatScalar(item))
		}

	default:
		var line string
		if key == "" {
			line = pad + formatScalar(cfg)
		} else {
			line = fmt.Sprintf("%s%s: %s", pad, key, formatScalar(cfg))
		}
		fmt.Fprintln(b, withProvenanceComment(line, explain))
	}
}

// withProvenanceComment appends a "# layer: ... (repo@ref:path)" comment to
// line when explain resolves to a Provenance, handling both the concrete
// resolve.Provenance a local resolution produces and the map[string]any a
// central-service response decodes into (docs/02-resolution-model.md §2.3:
// "only the internal Go representation differs").
func withProvenanceComment(line string, explain any) string {
	layer, source, ok := asProvenance(explain)
	if !ok {
		return line
	}
	if source != nil {
		return fmt.Sprintf("%s  # %s (%s@%s:%s)", line, layer, source.Repo, source.Ref, source.Path)
	}
	return fmt.Sprintf("%s  # %s", line, layer)
}

func asProvenance(v any) (layer string, source *gitsource.Source, ok bool) {
	switch p := v.(type) {
	case resolve.Provenance:
		return p.Layer, p.Source, true
	case map[string]any:
		layer, ok = p["layer"].(string)
		if !ok {
			return "", nil, false
		}
		if sm, hasSource := p["source"].(map[string]any); hasSource {
			source = &gitsource.Source{
				Repo: fmt.Sprint(sm["repo"]),
				Path: fmt.Sprint(sm["path"]),
				Ref:  fmt.Sprint(sm["ref"]),
			}
		}
		return layer, source, true
	default:
		return "", nil, false
	}
}

func formatScalar(v any) string {
	out, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimSuffix(string(out), "\n")
}
