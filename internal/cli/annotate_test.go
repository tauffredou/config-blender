package cli_test

import (
	"testing"

	"configblender/gitsource"
	"configblender/internal/cli"
	"configblender/resolve"
)

func TestFormatAnnotated(t *testing.T) {
	cfg := map[string]any{
		"port": 8080,
		"env":  "dev",
		"server": map[string]any{
			"middlewares": []any{"auth", "ratelimit"},
		},
	}
	explain := map[string]any{
		"port": resolve.Provenance{Layer: "base", Source: &gitsource.Source{Repo: "git@example.com/repo.git", Path: "base.yaml", Ref: "main"}},
		"env":  resolve.Provenance{Layer: "env-override"},
		"server": map[string]any{
			"middlewares": resolve.Provenance{Layer: "computed"},
		},
	}

	got := cli.FormatAnnotated(cfg, explain)
	want := `env: dev  # env-override
port: 8080  # base (git@example.com/repo.git@main:base.yaml)
server:
  middlewares:  # computed
    - auth
    - ratelimit`

	if got != want {
		t.Errorf("FormatAnnotated() =\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatAnnotated_JSONDecodedProvenance(t *testing.T) {
	// A central-service response decodes Explain into map[string]any all the
	// way down (docs/02-resolution-model.md §2.3), not resolve.Provenance —
	// FormatAnnotated must handle both shapes identically.
	cfg := map[string]any{"port": float64(8080)}
	explain := map[string]any{
		"port": map[string]any{
			"layer":  "base",
			"source": map[string]any{"repo": "repo.git", "path": "base.yaml", "ref": "main"},
		},
	}

	got := cli.FormatAnnotated(cfg, explain)
	want := "port: 8080  # base (repo.git@main:base.yaml)"

	if got != want {
		t.Errorf("FormatAnnotated() = %q, want %q", got, want)
	}
}

func TestFormatAnnotated_Leaf(t *testing.T) {
	// The shape LookupPath returns for a single scalar key (`--key --annotate`).
	got := cli.FormatAnnotated("dev", resolve.Provenance{Layer: "env"})
	want := "dev  # env"
	if got != want {
		t.Errorf("FormatAnnotated() = %q, want %q", got, want)
	}
}
