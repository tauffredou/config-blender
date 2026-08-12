// Command server runs configblender's central service
// (docs/04-kubernetes.md §4.2): it owns the Recipe database, the Git
// fetch, and the Starlark resolution, exposed over HTTP to per-cluster
// controllers (cmd/manager) — the "Vault" side of the ESO+Vault-shaped
// split.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"configblender/internal/centralserver"
	"configblender/internal/recipesource"
)

func main() {
	dbPath := flag.String("recipe-db", "/data/recipes.db", "path to the Recipe database — also holds the Git source registry (docs/05-recipe-and-crd.md §5.2)")
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	// Read from the environment, not a flag, so it doesn't show up in
	// process listings (ps) — same reasoning as internal/gitauth, and
	// naturally fed by a mounted K8s Secret in a real deployment.
	writeToken := os.Getenv("CONFIGBLENDER_WRITE_TOKEN")
	if writeToken == "" {
		log.Warn("CONFIGBLENDER_WRITE_TOKEN is not set — write endpoints (put/rollback) are disabled")
	}

	store, err := recipesource.Open(*dbPath, recipesource.WithLogger(log))
	if err != nil {
		log.Error("unable to open recipe database", "path", *dbPath, "error", err)
		os.Exit(1)
	}
	defer store.Close()

	log.Info("configblender central service listening", "addr", *addr, "recipe-db", *dbPath)
	if err := http.ListenAndServe(*addr, centralserver.New(store, writeToken, centralserver.WithLogger(log)).Handler()); err != nil {
		log.Error("server exited with an error", "error", err)
		os.Exit(1)
	}
}
