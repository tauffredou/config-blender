package resolve

import "configblender/gitsource"

// Provenance is an explain leaf (docs/02-resolution-model.md §2.3): which
// layer produced a config value, and — when that layer's content came from
// Git — exactly where, so an operator can jump straight to the source
// instead of only knowing "which layer".
type Provenance struct {
	Layer  string            `yaml:"layer" json:"layer"`
	Source *gitsource.Source `yaml:"source,omitempty" json:"source,omitempty"`
}
