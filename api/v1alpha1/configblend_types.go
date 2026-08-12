package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigBlendSpec binds a named Recipe (docs/05-recipe-and-crd.md §5.1,
// stored in configblender's own database — not here) to a target
// ConfigMap. It carries no layer content: the CRD is a binding, the Recipe
// is the source (docs/05-recipe-and-crd.md §5.4).
type ConfigBlendSpec struct {
	// Recipe is the name of the Recipe to resolve, as stored by
	// configblender (docs/05-recipe-and-crd.md §5.2).
	Recipe string `json:"recipe"`

	// RefreshInterval controls how often the Recipe is re-resolved
	// (docs/04-kubernetes.md §4.3 — polling, ESO-style). Defaults to 1h
	// when unset.
	// +optional
	RefreshInterval metav1.Duration `json:"refreshInterval,omitempty"`

	// Target names the ConfigMap configblender produces and keeps in
	// sync (docs/04-kubernetes.md §4.4).
	Target ConfigBlendTarget `json:"target"`
}

type ConfigBlendTarget struct {
	// ConfigMapName is the name of the ConfigMap to create/update in the
	// same namespace as the ConfigBlend resource.
	ConfigMapName string `json:"configMapName"`
}

type ConfigBlendStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastResolvedTime is when the Recipe was last resolved successfully.
	// +optional
	LastResolvedTime *metav1.Time `json:"lastResolvedTime,omitempty"`

	// Conditions follows the standard Kubernetes condition convention;
	// a "Ready" condition reports whether the target ConfigMap reflects
	// the current resolution.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Recipe",type=string,JSONPath=`.spec.recipe`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.configMapName`
type ConfigBlend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigBlendSpec   `json:"spec,omitempty"`
	Status ConfigBlendStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ConfigBlendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigBlend `json:"items"`
}
