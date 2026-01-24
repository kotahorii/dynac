package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kotahorii/dynac/internal/model"
	"github.com/kotahorii/dynac/internal/query"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestGenerate(t *testing.T) {
	modelPath := writeTempFile(t, "models.go", `package ddb

import t "time"

type User struct {
	PK        string `+"`"+`dynamodbav:"pk"`+"`"+`
	SK        string `+"`"+`dynamodbav:"sk"`+"`"+`
	UserName  string `+"`"+`dynamodbav:"user_name"`+"`"+`
	CreatedAt t.Time `+"`"+`dynamodbav:"created_at"`+"`"+`
}`)

	queryPath := writeTempFile(t, "queries.partiql", `-- name: GetUser :one
SELECT * FROM "App"
WHERE pk = ? AND sk = ?

-- name: ListUsersByOrg :many
-- @limit 10
SELECT * FROM "App"
WHERE pk = ? AND begins_with(sk, ?)

-- name: CreateUser :exec
INSERT INTO "App" VALUE ?

-- name: UpdateUser :exec
UPDATE "App"
SET created_at = ?
WHERE pk = ? AND sk = ?`)

	models, err := model.Parse([]string{modelPath})
	if err != nil {
		t.Fatalf("parse models: %v", err)
	}
	qs, err := query.LoadFile(queryPath)
	if err != nil {
		t.Fatalf("load queries: %v", err)
	}

	g := Generator{
		PkgName: "ddb",
		Table:   "App",
		PK:      "pk",
		SK:      "sk",
		Models:  models,
	}
	out, err := g.Generate(qs)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	code := string(out)
	checks := []string{
		"func (q *Queries) GetUser",
		"begins_with(#n1, :v1)",
		"attribute_not_exists(#pk)",
		"newCreatedAt t.Time",
		"t \"time\"",
		"\"#n0\": \"pk\"",
	}
	for _, want := range checks {
		if !strings.Contains(code, want) {
			t.Fatalf("generated code missing %q", want)
		}
	}
}

func TestBuildKeyParamsSanitizesNames(t *testing.T) {
	modelPath := writeTempFile(t, "models.go", `package ddb

type Item struct {
	Type  string `+"`"+`dynamodbav:"type"`+"`"+`
	First string `+"`"+`dynamodbav:"1st"`+"`"+`
}`)

	models, err := model.Parse([]string{modelPath})
	if err != nil {
		t.Fatalf("parse models: %v", err)
	}

	g := Generator{Models: models}
	params, err := g.buildKeyParams("Item", []string{"type"}, []string{"1st"})
	if err != nil {
		t.Fatalf("build params: %v", err)
	}
	if got := params[0].Name; got != "vType" {
		t.Fatalf("param name for type = %q, want %q", got, "vType")
	}
	if got := params[1].Name; got != "v1st" {
		t.Fatalf("param name for 1st = %q, want %q", got, "v1st")
	}
}

func TestBuildKeyParamsHandlesNonASCII(t *testing.T) {
	modelPath := writeTempFile(t, "models.go", `package ddb

type Item struct {
	Name string `+"`"+`dynamodbav:"名前"`+"`"+`
}`)

	models, err := model.Parse([]string{modelPath})
	if err != nil {
		t.Fatalf("parse models: %v", err)
	}

	g := Generator{Models: models}
	params, err := g.buildKeyParams("Item", []string{"名前"}, nil)
	if err != nil {
		t.Fatalf("build params: %v", err)
	}
	if got := params[0].Name; got != "v" {
		t.Fatalf("param name for non-ascii = %q, want %q", got, "v")
	}
}
