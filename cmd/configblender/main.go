// Command configblender is the MVP CLI: `put` creates/updates a Recipe
// locally (GitOps only for v1 — docs/05-recipe-and-crd.md §5.3), `list`,
// `resolve` and `explain` consult Recipes either locally or via the
// central service (docs/02-resolution-model.md §2.3).
package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"configblender/internal/cli"
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

put and rollback are local only: for v1, creating or changing a Recipe is GitOps, not a network call.
Every put is versioned (Vault-KV-v2-style); use history/get --version to inspect and rollback to revert.`)
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

func printYAML(v any) error {
	out, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}
