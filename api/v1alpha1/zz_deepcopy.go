package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Hand-written in place of controller-gen's usual zz_generated.deepcopy.go
// (no codegen tool wired into the build yet — the struct set is small
// enough that this stays easy to keep correct by hand).

func (in *ConfigBlendTarget) DeepCopy() *ConfigBlendTarget {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (in *ConfigBlendSpec) DeepCopyInto(out *ConfigBlendSpec) {
	*out = *in
	out.Target = in.Target
}

func (in *ConfigBlendSpec) DeepCopy() *ConfigBlendSpec {
	if in == nil {
		return nil
	}
	out := new(ConfigBlendSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConfigBlendStatus) DeepCopyInto(out *ConfigBlendStatus) {
	*out = *in
	if in.LastResolvedTime != nil {
		out.LastResolvedTime = in.LastResolvedTime.DeepCopy()
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

func (in *ConfigBlendStatus) DeepCopy() *ConfigBlendStatus {
	if in == nil {
		return nil
	}
	out := new(ConfigBlendStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *ConfigBlend) DeepCopyInto(out *ConfigBlend) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ConfigBlend) DeepCopy() *ConfigBlend {
	if in == nil {
		return nil
	}
	out := new(ConfigBlend)
	in.DeepCopyInto(out)
	return out
}

func (in *ConfigBlend) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *ConfigBlendList) DeepCopyInto(out *ConfigBlendList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ConfigBlend, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ConfigBlendList) DeepCopy() *ConfigBlendList {
	if in == nil {
		return nil
	}
	out := new(ConfigBlendList)
	in.DeepCopyInto(out)
	return out
}

func (in *ConfigBlendList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
