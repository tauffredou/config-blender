// Command server runs configblender's central service
// (docs/04-kubernetes.md §4.2): it owns the Recipe database, the Git
// fetch, and the Starlark resolution, exposed over HTTP to per-cluster
// controllers (cmd/manager) — the "Vault" side of the ESO+Vault-shaped
// split.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"configblender/internal/centralserver"
	"configblender/internal/recipesource"
)

func main() {
	dbPath := flag.String("recipe-db", "/data/recipes.db", "path to the Recipe database — also holds the Git source registry (docs/05-recipe-and-crd.md §5.2)")
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	// Read from the environment, not a flag, so it doesn't show up in
	// process listings (ps) — same reasoning as internal/gitauth, and
	// naturally fed by a mounted K8s Secret in a real deployment.
	writeToken := os.Getenv("CONFIGBLENDER_WRITE_TOKEN")
	if writeToken == "" {
		log.Println("warning: CONFIGBLENDER_WRITE_TOKEN is not set — write endpoints (put/rollback) are disabled")
	}

	store, err := recipesource.Open(*dbPath)
	if err != nil {
		log.Fatalf("unable to open recipe database %q: %v", *dbPath, err)
	}
	defer store.Close()

	log.Printf("configblender central service listening on %s (recipe-db=%s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, centralserver.New(store, writeToken).Handler()); err != nil {
		log.Fatalf("server exited with an error: %v", err)
	}
}
