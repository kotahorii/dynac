package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
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
	for part := range strings.SplitSeq(v, ",") {
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
		os.Exit(runGenerate(os.Args[2:], os.Stderr))
	case "vet":
		os.Exit(runVet(os.Args[2:], os.Stderr))
	case "-h", "--help", "help":
		usage()
	default:
		writef(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func writeLine(w io.Writer, line string) {
	_, _ = fmt.Fprintln(w, line)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func usage() {
	writeLine(os.Stderr, "dynac - DynamoDB query code generator")
	writeLine(os.Stderr, "")
	writeLine(os.Stderr, "Usage:")
	writeLine(os.Stderr, "  dynac generate [flags]")
	writeLine(os.Stderr, "  dynac vet [flags]")
	writeLine(os.Stderr, "")
	writeLine(os.Stderr, "Flags:")
	writeLine(os.Stderr, "  --table   DynamoDB table name")
	writeLine(os.Stderr, "  --pkg     output package path (default internal/ddb)")
	writeLine(os.Stderr, "  --model   model file or directory (repeatable, generate only)")
	writeLine(os.Stderr, "  --pk      partition key name (default pk)")
	writeLine(os.Stderr, "  --sk      sort key name (default sk; set empty for single-key)")
	writeLine(os.Stderr, "  --queries query root (default queries)")
}

func runGenerate(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	table := fs.String("table", "", "table name")
	pkgPath := fs.String("pkg", "internal/ddb", "output package path")
	pk := fs.String("pk", "pk", "partition key name")
	sk := fs.String("sk", "sk", "sort key name")
	queriesRoot := fs.String("queries", "queries", "query root")
	var modelPaths stringSlice
	fs.Var(&modelPaths, "model", "model file or directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(modelPaths) == 0 {
		writeLine(stderr, "--model is required for generate")
		return 2
	}
	queries, err := loadQueries(*queriesRoot)
	if err != nil {
		writeLine(stderr, err.Error())
		return 1
	}
	models, err := model.Parse(modelPaths)
	if err != nil {
		writef(stderr, "load models: %v\n", err)
		return 1
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
		writef(stderr, "generate: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(*pkgPath, 0o755); err != nil {
		writef(stderr, "mkdir: %v\n", err)
		return 1
	}
	outPath := filepath.Join(*pkgPath, "queries_gen.go")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		writef(stderr, "write: %v\n", err)
		return 1
	}
	runtimeOut, err := gen.Runtime()
	if err != nil {
		writef(stderr, "generate runtime: %v\n", err)
		return 1
	}
	runtimePath := filepath.Join(*pkgPath, "runtime.go")
	if _, err := os.Stat(runtimePath); err != nil {
		runtimeGenPath := filepath.Join(*pkgPath, "runtime_gen.go")
		if err := os.WriteFile(runtimeGenPath, runtimeOut, 0o644); err != nil {
			writef(stderr, "write runtime: %v\n", err)
			return 1
		}
	}
	return 0
}

func runVet(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("vet", flag.ContinueOnError)
	fs.SetOutput(stderr)
	table := fs.String("table", "", "table name")
	pk := fs.String("pk", "pk", "partition key name")
	sk := fs.String("sk", "sk", "sort key name")
	queriesRoot := fs.String("queries", "queries", "query root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	queries, err := loadQueries(*queriesRoot)
	if err != nil {
		writeLine(stderr, err.Error())
		return 1
	}
	for _, q := range queries {
		if err := q.Validate(query.ValidateOptions{Table: *table, PK: *pk, SK: *sk}); err != nil {
			writef(stderr, "vet: %v\n", err)
			return 1
		}
	}
	return 0
}

func loadQueries(root string) ([]query.Query, error) {
	files, err := query.DiscoverQueryFiles(root)
	if err != nil {
		return nil, fmt.Errorf("discover queries: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .partiql files under %s", root)
	}
	queries, err := query.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("load queries: %w", err)
	}
	return queries, nil
}
