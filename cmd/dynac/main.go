package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kotahorii/dynac/internal/gen"
	"github.com/kotahorii/dynac/internal/model"
	"github.com/kotahorii/dynac/internal/query"
)

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSlice) Set(v string) error {
	if v == "" {
		return errors.New("empty value")
	}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		*s = append(*s, part)
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "generate":
		runGenerate(os.Args[2:])
	case "vet":
		runVet(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "dynac - DynamoDB query code generator")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  dynac generate [flags]")
	fmt.Fprintln(os.Stderr, "  dynac vet [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --table   DynamoDB table name")
	fmt.Fprintln(os.Stderr, "  --pkg     output package path (default internal/ddb)")
	fmt.Fprintln(os.Stderr, "  --model   model file or directory (repeatable, generate only)")
	fmt.Fprintln(os.Stderr, "  --pk      partition key name (default pk)")
	fmt.Fprintln(os.Stderr, "  --sk      sort key name (default sk; set empty for single-key)")
	fmt.Fprintln(os.Stderr, "  --queries query root (default queries)")
}

func runGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	table := fs.String("table", "", "table name")
	pkgPath := fs.String("pkg", "internal/ddb", "output package path")
	pk := fs.String("pk", "pk", "partition key name")
	sk := fs.String("sk", "sk", "sort key name")
	queriesRoot := fs.String("queries", "queries", "query root")
	var modelPaths stringSlice
	fs.Var(&modelPaths, "model", "model file or directory")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if len(modelPaths) == 0 {
		fmt.Fprintln(os.Stderr, "--model is required for generate")
		os.Exit(2)
	}
	files, err := query.DiscoverQueryFiles(*queriesRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover queries: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no .partiql files under %s\n", *queriesRoot)
		os.Exit(1)
	}
	queries, err := query.LoadFiles(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load queries: %v\n", err)
		os.Exit(1)
	}
	models, err := model.Parse(modelPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load models: %v\n", err)
		os.Exit(1)
	}
	pkgName := filepath.Base(filepath.Clean(*pkgPath))
	gen := gen.Generator{
		PkgPath: *pkgPath,
		PkgName: pkgName,
		Table:   *table,
		PK:      *pk,
		SK:      *sk,
		Models:  models,
	}
	out, err := gen.Generate(queries)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*pkgPath, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	outPath := filepath.Join(*pkgPath, "queries_gen.go")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
}

func runVet(args []string) {
	fs := flag.NewFlagSet("vet", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	table := fs.String("table", "", "table name")
	pk := fs.String("pk", "pk", "partition key name")
	sk := fs.String("sk", "sk", "sort key name")
	queriesRoot := fs.String("queries", "queries", "query root")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	files, err := query.DiscoverQueryFiles(*queriesRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover queries: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no .partiql files under %s\n", *queriesRoot)
		os.Exit(1)
	}
	queries, err := query.LoadFiles(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load queries: %v\n", err)
		os.Exit(1)
	}
	for _, q := range queries {
		if err := q.Validate(query.ValidateOptions{Table: *table, PK: *pk, SK: *sk}); err != nil {
			fmt.Fprintf(os.Stderr, "vet: %v\n", err)
			os.Exit(1)
		}
	}
}
