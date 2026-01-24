package query

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeQueryFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "q.partiql")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestValidateQueries(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
		options ValidateOptions
	}{
		{
			name: "valid one",
			content: `-- name: GetUser :one
SELECT * FROM "App"
WHERE pk = ? AND sk = ?`,
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "valid many limit",
			content: `-- name: ListUsersByOrg :many
-- @limit 10
SELECT * FROM "App"
WHERE pk = ? AND begins_with(sk, ?)`,
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "missing limit",
			content: `-- name: ListUsers :many
SELECT * FROM "App"
WHERE pk = ?`,
			wantErr: ":many requires LIMIT/@limit or @nolimit",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "projection without index",
			content: `-- name: ListUsers :many
-- @projection all
-- @limit 10
SELECT * FROM "App"
WHERE pk = ?`,
			wantErr: "@projection requires @index",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "index left contiguous",
			content: `-- name: ListMatches :many
-- @index GSI pk(tournament_id) sk(round, match_id)
-- @projection all
-- @limit 10
SELECT * FROM "App"
WHERE tournament_id = ? AND match_id = ?`,
			wantErr: "left-contiguous",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "begins_with not last",
			content: `-- name: ListMatches :many
-- @index GSI pk(tournament_id) sk(round, match_id, phase)
-- @projection all
-- @limit 10
SELECT * FROM "App"
WHERE tournament_id = ? AND round = ? AND begins_with(match_id, ?) AND phase = ?`,
			wantErr: "must be last SK condition",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "update exec with returning",
			content: `-- name: UpdateUser :exec
UPDATE "App"
SET user_name = ?
WHERE pk = ? AND sk = ?
RETURNING ALL NEW *`,
			wantErr: "cannot RETURNING",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "update one requires all returning",
			content: `-- name: UpdateUser :one
UPDATE "App"
SET user_name = ?
WHERE pk = ? AND sk = ?
RETURNING UPDATED NEW *`,
			wantErr: "requires RETURNING ALL OLD/NEW",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "delete one requires returning",
			content: `-- name: DeleteUser :one
DELETE FROM "App"
WHERE pk = ? AND sk = ?`,
			wantErr: "requires RETURNING",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "limit conflict",
			content: `-- name: ListUsers :many
-- @limit 10
SELECT * FROM "App"
WHERE pk = ?
LIMIT 5`,
			wantErr: "either LIMIT or @limit",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
		{
			name: "one requires full key",
			content: `-- name: GetUser :one
SELECT * FROM "App"
WHERE pk = ?`,
			wantErr: "requires full key equality",
			options: ValidateOptions{Table: "App", PK: "pk", SK: "sk"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeQueryFile(t, tc.content)
			qs, err := LoadFile(path)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(qs) != 1 {
				t.Fatalf("expected 1 query, got %d", len(qs))
			}
			err = qs[0].Validate(tc.options)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
