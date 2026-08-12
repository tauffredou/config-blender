// Command configblender is the MVP CLI: `put` creates/updates a Recipe
// locally (GitOps only for v1 — docs/05-recipe-and-crd.md §5.3), `list`,
// `resolve` and `explain` consult Recipes either locally or via the
// central service (docs/02-resolution-model.md §2.3).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"configblender/internal/cli"
	"configblender/internal/gitsourcedb"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "put":
		err = runPut(os.Args[2:])
	case "rollback":
		err = runRollback(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	case "get":
		err = runGet(os.Args[2:])
	case "history":
		err = runHistory(os.Args[2:])
	case "resolve":
		err = runResolve(os.Args[2:])
	case "explain":
		err = runExplain(os.Args[2:])
	case "source":
		err = runSource(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "configblender:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  configblender put --db <path> --file <recipe.yaml>
  configblender rollback --db <path> --recipe <name> --version <n>
  configblender list (--db <path> | --central-url <url>)
  configblender get (--db <path> | --central-url <url>) --recipe <name> [--version <n>]
  configblender history (--db <path> | --central-url <url>) --recipe <name>
  configblender resolve (--db <path> | --central-url <url>) --recipe <name>
  configblender explain (--db <path> | --central-url <url>) --recipe <name> [--key <dot.path>] [--annotate]
  configblender source put (--db <path> | --central-url <url>) --name <name> --repo <url> [credential flags]
  configblender source list (--db <path> | --central-url <url>)
  configblender source delete (--db <path> | --central-url <url>) --name <name>
  configblender source test (--db <path> | --central-url <url>) --repo <url> [credential flags]

put and rollback are local only: for v1, creating or changing a Recipe is GitOps, not a network call.
Every put is versioned (Vault-KV-v2-style); use history/get --version to inspect and rollback to revert.

A layer's source is a reference to a preconfigured Git source (see "source" above), not a raw repo
URL — register a source once, then point layers at it by name. A source's credentials, if the repo is
private, are stored in the Recipe DB itself (--username/--password/--password-stdin for HTTPS,
--ssh-key-file/--ssh-user/--ssh-key-passphrase for SSH — see "source put -h"); "source test" exercises
the same flags without saving anything, to verify a repo/credential pair first.`)
}

func runPut(args []string) error {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to the Recipe database")
	specPath := fs.String("file", "", "path to a Recipe spec YAML file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *specPath == "" {
		return fmt.Errorf("--db and --file are required")
	}
	return cli.PutRecipe(*dbPath, *specPath)
}

func runRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to the Recipe database")
	recipeName := fs.String("recipe", "", "name of the Recipe to roll back")
	version := fs.Int("version", 0, "version to restore as the new latest version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *recipeName == "" || *version <= 0 {
		return fmt.Errorf("--db, --recipe and a positive --version are required")
	}
	return cli.RollbackRecipe(*dbPath, *recipeName, *version)
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	if err := fs.Parse(args); err != nil {
		return err
	}

	names, err := cli.ListRecipeNames(*dbPath, *centralURL)
	if err != nil {
		return err
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}

func runGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	recipeName := fs.String("recipe", "", "name of the Recipe to fetch")
	version := fs.Int("version", 0, "specific version to fetch (default: latest)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *recipeName == "" {
		return fmt.Errorf("--recipe is required")
	}

	spec, err := cli.GetRecipeSpec(*dbPath, *centralURL, *recipeName, *version)
	if err != nil {
		return err
	}
	return printYAML(spec)
}

func runHistory(args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	recipeName := fs.String("recipe", "", "name of the Recipe")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *recipeName == "" {
		return fmt.Errorf("--recipe is required")
	}

	versions, err := cli.ListRecipeVersions(*dbPath, *centralURL, *recipeName)
	if err != nil {
		return err
	}
	for _, v := range versions {
		fmt.Printf("%d\t%s\n", v.Version, v.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	return nil
}

func runResolve(args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	recipeName := fs.String("recipe", "", "name of the Recipe to resolve")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *recipeName == "" {
		return fmt.Errorf("--recipe is required")
	}

	res, err := cli.ResolveRecipe(*dbPath, *centralURL, *recipeName)
	if err != nil {
		return err
	}
	return printYAML(res.Config)
}

func runExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	recipeName := fs.String("recipe", "", "name of the Recipe to resolve")
	key := fs.String("key", "", "dot-separated key path to look up, e.g. server.middlewares (default: the whole tree)")
	annotate := fs.Bool("annotate", false, "print the final config with each value annotated by the layer (and Git source) that produced it, instead of the raw provenance tree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *recipeName == "" {
		return fmt.Errorf("--recipe is required")
	}

	res, err := cli.ResolveRecipe(*dbPath, *centralURL, *recipeName)
	if err != nil {
		return err
	}

	if *annotate {
		var cfg, exp any = res.Config, res.Explain
		if *key != "" {
			var ok bool
			cfg, ok = cli.LookupPath(res.Config, *key)
			if !ok {
				return fmt.Errorf("key %q not found in the resolved config", *key)
			}
			exp, _ = cli.LookupPath(res.Explain, *key)
		}
		fmt.Println(cli.FormatAnnotated(cfg, exp))
		return nil
	}

	if *key == "" {
		return printYAML(res.Explain)
	}

	source, ok := cli.LookupPath(res.Explain, *key)
	if !ok {
		return fmt.Errorf("key %q not found in the resolved config", *key)
	}
	return printYAML(source)
}

func runSource(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: configblender source (put|list|delete|test) ...")
	}
	switch args[0] {
	case "put":
		return runSourcePut(args[1:])
	case "list":
		return runSourceList(args[1:])
	case "delete":
		return runSourceDelete(args[1:])
	case "test":
		return runSourceTest(args[1:])
	default:
		return fmt.Errorf("unknown source subcommand %q (want put, list, delete, or test)", args[0])
	}
}

// credentialFlags are the source-credential flags shared by `source put`
// (which stores them — docs/04-kubernetes.md §4.1: the Recipe DB is the
// only place Git credentials live now, no environment-variable fallback)
// and `source test` (which only exercises them, never stores them).
type credentialFlags struct {
	username         *string
	password         *string
	passwordStdin    *bool
	sshKeyFile       *string
	sshUser          *string
	sshKeyPassphrase *string
}

func addCredentialFlags(fs *flag.FlagSet) *credentialFlags {
	return &credentialFlags{
		username:         fs.String("username", "", "HTTP basic auth username, for a private repo (with --password or --password-stdin)"),
		password:         fs.String("password", "", "HTTP basic auth password or access token (with --username) — prefer --password-stdin to keep it out of shell history and process listings"),
		passwordStdin:    fs.Bool("password-stdin", false, "read the password/token from stdin instead of --password"),
		sshKeyFile:       fs.String("ssh-key-file", "", "path to a PEM-encoded SSH private key, for a private repo over SSH"),
		sshUser:          fs.String("ssh-user", "", `SSH username (default "git")`),
		sshKeyPassphrase: fs.String("ssh-key-passphrase", "", "passphrase for --ssh-key-file, if it's encrypted"),
	}
}

// credentials resolves the parsed flags into a Credentials value, or nil if
// none were set (unauthenticated). Must be called after fs.Parse.
func (f *credentialFlags) credentials() (*gitsourcedb.Credentials, error) {
	if *f.passwordStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading password from stdin: %w", err)
		}
		*f.password = strings.TrimSpace(string(data))
	}

	switch {
	case *f.username != "" || *f.password != "":
		return &gitsourcedb.Credentials{Username: *f.username, Password: *f.password}, nil
	case *f.sshKeyFile != "":
		key, err := os.ReadFile(*f.sshKeyFile)
		if err != nil {
			return nil, fmt.Errorf("reading --ssh-key-file: %w", err)
		}
		return &gitsourcedb.Credentials{SSHKey: string(key), SSHUser: *f.sshUser, SSHKeyPassphrase: *f.sshKeyPassphrase}, nil
	}
	return nil, nil
}

func runSourcePut(args []string) error {
	fs := flag.NewFlagSet("source put", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	name := fs.String("name", "", "name layers will reference this source by (LayerSpec.source.sourceRef)")
	repo := fs.String("repo", "", "repository URL")
	credFlags := addCredentialFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *repo == "" {
		return fmt.Errorf("--name and --repo are required")
	}

	auth, err := credFlags.credentials()
	if err != nil {
		return err
	}
	return cli.PutSource(*dbPath, *centralURL, &gitsourcedb.GitSource{Name: *name, Repo: *repo, Auth: auth})
}

func runSourceTest(args []string) error {
	fs := flag.NewFlagSet("source test", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	repo := fs.String("repo", "", "repository URL to test")
	credFlags := addCredentialFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return fmt.Errorf("--repo is required")
	}

	auth, err := credFlags.credentials()
	if err != nil {
		return err
	}
	if err := cli.TestSourceConnection(*dbPath, *centralURL, *repo, auth); err != nil {
		return err
	}
	fmt.Println("ok: connection succeeded")
	return nil
}

func runSourceList(args []string) error {
	fs := flag.NewFlagSet("source list", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sources, err := cli.ListSources(*dbPath, *centralURL)
	if err != nil {
		return err
	}
	for _, src := range sources {
		fmt.Printf("%s\t%s\n", src.Name, src.Repo)
	}
	return nil
}

func runSourceDelete(args []string) error {
	fs := flag.NewFlagSet("source delete", flag.ExitOnError)
	dbPath := fs.String("db", "", "path to a local Recipe database — mutually exclusive with --central-url")
	centralURL := fs.String("central-url", "", "base URL of the central service — mutually exclusive with --db")
	name := fs.String("name", "", "name of the Git source to delete")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	return cli.DeleteSource(*dbPath, *centralURL, *name)
}

func printYAML(v any) error {
	out, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}
