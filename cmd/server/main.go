// Command server runs configblender's central service
// (docs/04-kubernetes.md §4.2): it owns the Recipe database, the Git
// fetch, and the Starlark resolution, exposed over HTTP to per-cluster
// controllers (cmd/manager) — the "Vault" side of the ESO+Vault-shaped
// split.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"configblender/internal/centralserver"
	"configblender/internal/recipesource"
	"configblender/internal/userdb"
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
		log.Warn("CONFIGBLENDER_WRITE_TOKEN is not set — the break-glass admin bearer/login is disabled; write access is entirely through per-user accounts (docs/07-open-questions.md)")
	}

	store, err := recipesource.Open(*dbPath, recipesource.WithLogger(log))
	if err != nil {
		log.Error("unable to open recipe database", "path", *dbPath, "error", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := bootstrapAdmin(store, log); err != nil {
		log.Error("unable to bootstrap the initial admin user", "error", err)
		os.Exit(1)
	}

	log.Info("configblender central service listening", "addr", *addr, "recipe-db", *dbPath)
	if err := http.ListenAndServe(*addr, centralserver.New(store, writeToken, centralserver.WithLogger(log)).Handler()); err != nil {
		log.Error("server exited with an error", "error", err)
		os.Exit(1)
	}
}

// adminUserStore is what bootstrapAdmin needs from the Recipe store —
// satisfied by *recipesource.Store.
type adminUserStore interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, username, password string, role userdb.Role) error
}

// bootstrapAdmin creates the first admin account when the user store is
// empty — otherwise there would be no way to reach the (admin-only)
// POST /v1/users endpoint at all on a brand-new deployment. Username
// defaults to "admin"; the password comes from CONFIGBLENDER_ADMIN_PASSWORD,
// or is generated and logged once if that's unset. A no-op once at least
// one user exists, so it never overwrites an operator's own accounts on a
// later restart.
func bootstrapAdmin(store adminUserStore, log *slog.Logger) error {
	ctx := context.Background()
	count, err := store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	username := os.Getenv("CONFIGBLENDER_ADMIN_USER")
	if username == "" {
		username = "admin"
	}
	password := os.Getenv("CONFIGBLENDER_ADMIN_PASSWORD")
	generated := password == ""
	if generated {
		password, err = userdb.RandomPassword()
		if err != nil {
			return err
		}
	}

	if err := store.CreateUser(ctx, username, password, userdb.RoleAdmin); err != nil {
		return err
	}
	if generated {
		log.Warn("no users existed — created an initial admin account with a generated password; log in once and change it, or set CONFIGBLENDER_ADMIN_PASSWORD to control it explicitly next time", "username", username, "password", password)
	} else {
		log.Info("no users existed — created an initial admin account from CONFIGBLENDER_ADMIN_USER/CONFIGBLENDER_ADMIN_PASSWORD", "username", username)
	}
	return nil
}
