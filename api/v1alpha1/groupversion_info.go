// Package v1alpha1 contains the ConfigBlend API (docs/05-recipe-and-crd.md
// §5.4): a lean binding resource that references a Recipe by name and a
// target ConfigMap — it carries no configuration content itself.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion   = schema.GroupVersion{Group: "configblender.io", Version: "v1alpha1"}
	SchemeBuilder  = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme    = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&ConfigBlend{}, &ConfigBlendList{})
}
