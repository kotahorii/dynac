package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestRunGenerateRequiresModel(t *testing.T) {
	var buf bytes.Buffer
	code := runGenerate([]string{"--queries", t.TempDir()}, &buf)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(buf.String(), "--model is required for generate") {
		t.Fatalf("missing error message: %s", buf.String())
	}
}

func TestRunGenerateWritesOutput(t *testing.T) {
	tmp := t.TempDir()
	queryPath := filepath.Join(tmp, "q.partiql")
	modelPath := filepath.Join(tmp, "models.go")
	outDir := filepath.Join(tmp, "out")
	writeFile(t, queryPath, `-- name: GetUser :one
SELECT * FROM "App"
WHERE pk = ? AND sk = ?
`)
	writeFile(t, modelPath, `package ddb

type User struct {
	PK string `+"`"+`dynamodbav:"pk"`+"`"+`
	SK string `+"`"+`dynamodbav:"sk"`+"`"+`
}
`)
	var buf bytes.Buffer
	code := runGenerate([]string{
		"--queries", tmp,
		"--model", modelPath,
		"--pkg", outDir,
	}, &buf)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", code, buf.String())
	}
	outPath := filepath.Join(outDir, "queries_gen.go")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "GetUser") {
		t.Fatalf("generated file missing GetUser")
	}
}

func TestRunVetValidQuery(t *testing.T) {
	tmp := t.TempDir()
	queryPath := filepath.Join(tmp, "q.partiql")
	writeFile(t, queryPath, `-- name: GetUser :one
SELECT * FROM "App"
WHERE pk = ? AND sk = ?
`)
	var buf bytes.Buffer
	code := runVet([]string{"--queries", tmp}, &buf)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", code, buf.String())
	}
}
