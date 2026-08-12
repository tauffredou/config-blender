// Command manager runs configblender's per-cluster controller
// (docs/04-kubernetes.md §4.2): it reconciles ConfigBlend resources in the
// cluster it runs in. Recipe resolution comes either from a local
// Recipe database (--recipe-db, simple single-process deployments) or from
// the central service over HTTP (--central-url, the ESO+Vault-shaped
// multi-cluster deployment) — exactly one of the two must be set.
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	configblenderv1alpha1 "configblender/api/v1alpha1"
	"configblender/internal/centralclient"
	"configblender/internal/controller"
	"configblender/internal/recipesource"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(configblenderv1alpha1.AddToScheme(scheme))
}

func main() {
	dbPath := flag.String("recipe-db", "", "path to a local Recipe database — also holds the Git source registry (docs/05-recipe-and-crd.md §5.2) — mutually exclusive with --central-url")
	centralURL := flag.String("central-url", "", "base URL of the central service (docs/04-kubernetes.md §4.2) — mutually exclusive with --recipe-db")
	flag.Parse()

	// One slog backend for the whole process — bridged into logr so
	// controller-runtime's reconciler logging (the idiomatic choice for a
	// controller-runtime-based controller) and this package's own logging
	// emit the same structured shape as cmd/server.
	slogHandler := slog.NewJSONHandler(os.Stderr, nil)
	slog.SetDefault(slog.New(slogHandler))
	ctrl.SetLogger(logr.FromSlogHandler(slogHandler))
	log := ctrl.Log.WithName("configblender")

	if (*dbPath == "") == (*centralURL == "") {
		log.Error(nil, "exactly one of --recipe-db or --central-url must be set")
		os.Exit(1)
	}

	var recipes controller.RecipeResolver
	if *centralURL != "" {
		recipes = centralclient.New(*centralURL, nil)
		log.Info("resolving recipes via the central service", "central-url", *centralURL)
	} else {
		store, err := recipesource.Open(*dbPath, recipesource.WithLogger(slog.Default()))
		if err != nil {
			log.Error(err, "unable to open recipe database", "path", *dbPath)
			os.Exit(1)
		}
		defer store.Close()
		recipes = store
		log.Info("resolving recipes via a local database", "recipe-db", *dbPath)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// Metrics.BindAddress defaults to :8080 even when left unset; the
		// health probe address has no such default, so it's set explicitly.
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		log.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up healthz check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up readyz check")
		os.Exit(1)
	}

	reconciler := &controller.ConfigBlendReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Recipes: recipes,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to set up ConfigBlend controller")
		os.Exit(1)
	}

	log.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager exited with an error")
		os.Exit(1)
	}
}
