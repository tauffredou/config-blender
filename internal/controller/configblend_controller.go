// Package controller implements the ConfigBlend reconciliation loop
// (docs/04-kubernetes.md §4.3): resolve the bound Recipe and keep the
// target ConfigMap in sync with it, on the same trigger model as External
// Secrets Operator (create/update of the resource, refreshInterval expiry,
// drift on the target).
package controller

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configblenderv1alpha1 "configblender/api/v1alpha1"
	"configblender/resolve"
)

// configMapKey is the data key holding the resolved config tree
// (docs/04-kubernetes.md §4.4, output 1 — YAML).
const configMapKey = "config.yaml"

const readyCondition = "Ready"

// DefaultRefreshInterval is used when a ConfigBlend does not set
// spec.refreshInterval (docs/05-recipe-and-crd.md §5.4).
const DefaultRefreshInterval = time.Hour

// RecipeResolver resolves a named Recipe into a config tree. It decouples
// the reconciler — a lightweight, per-cluster controller
// (docs/04-kubernetes.md §4.2) — from how that resolution actually happens:
// a local internal/recipesource.Store (simple single-process deployments)
// or a network client to the central service (internal/centralclient, the
// ESO+Vault-shaped multi-cluster deployment).
type RecipeResolver interface {
	Resolve(name string) (*resolve.Result, error)
}

// ConfigBlendReconciler reconciles a ConfigBlend object.
type ConfigBlendReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Recipes RecipeResolver
}

// +kubebuilder:rbac:groups=configblender.io,resources=configblends,verbs=get;list;watch
// +kubebuilder:rbac:groups=configblender.io,resources=configblends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

func (r *ConfigBlendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cb configblenderv1alpha1.ConfigBlend
	if err := r.Get(ctx, req.NamespacedName, &cb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	interval := refreshInterval(&cb)

	result, resolveErr := r.Recipes.Resolve(cb.Spec.Recipe)
	if resolveErr != nil {
		r.setCondition(&cb, metav1.ConditionFalse, "ResolveFailed", resolveErr.Error())
		if err := r.Status().Update(ctx, &cb); err != nil {
			return ctrl.Result{}, err
		}
		// Requeue at the normal cadence rather than the exponential
		// backoff a returned error would trigger: a transient Git/DB
		// failure should be retried at the same pace as any other
		// refresh, not hammered (docs/04-kubernetes.md §4.3, ESO-style
		// polling).
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	data, err := yaml.Marshal(result.Config)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("marshaling resolved config: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cb.Spec.Target.ConfigMapName,
			Namespace: cb.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[configMapKey] = string(data)
		return controllerutil.SetControllerReference(&cb, cm, r.Scheme)
	})
	if err != nil {
		r.setCondition(&cb, metav1.ConditionFalse, "ConfigMapSyncFailed", err.Error())
		if statusErr := r.Status().Update(ctx, &cb); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	now := metav1.Now()
	cb.Status.LastResolvedTime = &now
	cb.Status.ObservedGeneration = cb.Generation
	r.setCondition(&cb, metav1.ConditionTrue, "Resolved", "target ConfigMap reflects the current Recipe resolution")
	if err := r.Status().Update(ctx, &cb); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *ConfigBlendReconciler) setCondition(cb *configblenderv1alpha1.ConfigBlend, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cb.Status.Conditions, metav1.Condition{
		Type:               readyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cb.Generation,
	})
}

func refreshInterval(cb *configblenderv1alpha1.ConfigBlend) time.Duration {
	if d := cb.Spec.RefreshInterval.Duration; d > 0 {
		return d
	}
	return DefaultRefreshInterval
}

func (r *ConfigBlendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configblenderv1alpha1.ConfigBlend{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
