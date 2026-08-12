package recipe

import (
	"context"
	"fmt"

	"configblender/gitsource"
)

// LayerSource locates one layer's content by reference to a preconfigured
// Git source (internal/gitsourcedb, docs/07-open-questions.md) rather than
// an inline repo URL — so a layer never embeds a repo URL (and never any
// credentials that might be baked into one) directly. Path/Ref stay
// per-layer: different layers of the same repo can point at different
// refs.
type LayerSource struct {
	SourceRef string `yaml:"sourceRef" json:"sourceRef"`
	Path      string `yaml:"path" json:"path"`
	Ref       string `yaml:"ref" json:"ref"`
}

// SourceResolver resolves a registered Git source's name (LayerSource.SourceRef)
// to its repository URL. Defined here rather than importing
// internal/gitsourcedb's concrete store, so recipe stays independent of how
// or where sources are stored (internal/gitsourcedb.Store satisfies this
// interface directly).
type SourceResolver interface {
	ResolveSource(name string) (repoURL string, err error)
}

// LayerSpec is the declarative, stored form of a layer: it references its
// content in Git (CONCEPTION.md section 9) rather than embedding it, so the
// content itself stays versioned and review-able independently of the
// Recipe's structure.
type LayerSpec struct {
	Name   string      `yaml:"name" json:"name"`
	Type   LayerType   `yaml:"type" json:"type"`
	Source LayerSource `yaml:"source" json:"source"`
}

// Spec is the declarative, stored form of a Recipe (CONCEPTION.md section
// 9): what configblender keeps in its Recipe database. Resolving it into a
// config tree first requires Materialize to fetch each layer's content.
// The yaml tags define the file format read by `configblender put`
// (docs/05-recipe-and-crd.md §5.3).
type Spec struct {
	Name        string      `yaml:"name" json:"name"`
	Layers      []LayerSpec `yaml:"layers" json:"layers"`
	MergePolicy []MergeRule `yaml:"mergePolicy,omitempty" json:"mergePolicy,omitempty"`
}

// Materialize resolves every layer's LayerSource.SourceRef to a repo URL
// via sources, fetches its content from Git via f, and returns the
// ready-to-resolve Recipe (CONCEPTION.md section 5.3/9). It is the only
// place Git I/O happens; resolve.Resolve itself has no knowledge of Git.
func (s *Spec) Materialize(ctx context.Context, f *gitsource.Fetcher, sources SourceResolver) (*Recipe, error) {
	layers := make([]Layer, len(s.Layers))
	for i, ls := range s.Layers {
		repoURL, err := sources.ResolveSource(ls.Source.SourceRef)
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", ls.Name, err)
		}
		src := gitsource.Source{Repo: repoURL, Path: ls.Source.Path, Ref: ls.Source.Ref}

		content, err := f.Content(ctx, src)
		if err != nil {
			return nil, err
		}
		layers[i] = Layer{Name: ls.Name, Type: ls.Type, Content: content, Source: src}
	}
	return &Recipe{
		Name:        s.Name,
		Layers:      layers,
		MergePolicy: s.MergePolicy,
	}, nil
}
