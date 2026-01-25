# dynac

Type-safe DynamoDB query code generator for Go.

Write constrained PartiQL-like queries in `.partiql` files and dynac generates strongly typed methods that use the AWS SDK v2. The validator is intentionally strict to prevent accidental scans and unsafe queries.

## Status

Early-stage / design in progress. Expect breaking changes. The roadmap is in `docs/plan/plan.md` (Japanese).

## Requirements

- Go 1.25+
- AWS SDK v2 (required by generated code; pulled via Go modules)

## Install

Go (from this repository root):

```sh
go install ./cmd/dynac
```

Go (from module path):

```sh
go install github.com/kotahorii/dynac/cmd/dynac@latest
```

Homebrew (tap):

```sh
brew install kotahorii/dynac/dynac
```

Or:

```sh
brew tap kotahorii/dynac
brew install dynac
```

Ensure your `GOBIN` (or `$(go env GOPATH)/bin`) is on your `PATH` if you use `go install`.

## Quick Start

1. Define your models with `dynamodbav` tags:

```go
// internal/ddb/models.go
package ddb

type User struct {
	PK       string `dynamodbav:"pk"`
	SK       string `dynamodbav:"sk"`
	UserName string `dynamodbav:"user_name"`
}
```

2. Write queries in `.partiql` files under `queries/` (or pass `--queries`):

```sql
-- queries/user.partiql
-- name: GetUser :one
SELECT * FROM "App"
WHERE pk = ? AND sk = ?

-- name: ListUsersByOrg :many
-- @limit 100
SELECT * FROM "App"
WHERE pk = ? AND begins_with(sk, ?)

-- name: UpdateUserName :exec
UPDATE "App"
SET user_name = ?
WHERE pk = ? AND sk = ?
```

3. Generate code (defaults to `internal/ddb/queries_gen.go`):

```sh
dynac generate --model ./internal/ddb
# or, without installing:
go run github.com/kotahorii/dynac/cmd/dynac@latest generate --model ./internal/ddb
```

4. Use the generated API:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	// Replace with your own module path.
	// Note: Go's internal packages can only be imported from within your module.
	"example.com/yourapp/internal/ddb"
)

func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	client := dynamodb.NewFromConfig(cfg)
	q := &ddb.Queries{Client: client, Table: "App"}

	user, err := q.GetUser(ctx, "USER#1", "PROFILE")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(user.UserName)
}
```

Note: This example assumes AWS credentials are configured and a DynamoDB table named "App" exists with `pk`/`sk` keys.

## Sample App

A runnable demo that exercises Get/List/Put/Update/Delete lives under `examples/sample-app`.
See `examples/sample-app/README.md` for setup and run instructions.

5. Validate queries without generating code:

```sh
dynac vet
# or, without installing:
go run github.com/kotahorii/dynac/cmd/dynac@latest vet
```

## CLI

```
dynac generate [flags]
dynac vet [flags]

Flags:
  --table   DynamoDB table name
  --pkg     output package path (default internal/ddb)
  --model   model file or directory (repeatable, generate only)
  --pk      partition key name (default pk)
  --sk      sort key name (default sk; set empty for single-key)
  --queries query root (default queries)
```

Notes:

- `generate` requires `--model`.
- Queries are discovered by scanning `--queries` (default `queries/`) for `.partiql` files.
- If you have no `queries/` directory, create it or pass `--queries` to point elsewhere.
- `generate` writes `queries_gen.go` and `runtime_gen.go` into `--pkg`.
  If a `runtime.go` already exists in that package, `runtime_gen.go` is not written.

## Query Format

Each query is defined by a name annotation followed by a single statement:

```
-- name: QueryName :one|:many|:exec
<SQL>
```

Supported statements (current):

- `SELECT * FROM "Table" WHERE ... [LIMIT n]`
- `INSERT INTO "Table" VALUE ?`
- `UPDATE "Table" SET a = ? [, b = ? ...] WHERE ... [RETURNING ...]`
- `DELETE FROM "Table" WHERE ... [RETURNING ...]`

Placeholders must be `?` (positional). Only `AND` is allowed in `WHERE` clauses.

## SELECT Validation Rules (Safety-First)

The validator enforces DynamoDB-safe query shapes:

- Only `SELECT *` is supported.
- `:many` requires `LIMIT`, `@limit`, or `@nolimit`.
- `:one` requires full key equality.
- Only key attributes may appear in `WHERE`.
- Partition key conditions must be `=`.
- Sort key conditions must be left-contiguous and can be:
  - `=`
  - `begins_with(sk, ?)`
  - `sk BETWEEN ? AND ?`
- `OR`, `NOT`, and `IN` are rejected.

These rules are enforced by both `dynac vet` and `dynac generate`.

## Annotations

- `@limit N` - maximum number of items returned by `:many`.
- `@nolimit` - allow unbounded `:many` (explicit opt-in).
- `@index Name pk(a,b,...) sk(c,d,...)` - use a GSI/LSI.
- `@projection all` - required when `@index` is used.
- `@model TypeName` - override model type inference.

`@index` notes:

- Supports up to 4 PK attributes and 4 SK attributes.
- SK conditions must be left-contiguous (no gaps).
- Optional type hints are accepted (`pk(user_id:S)`) and used to bind index key values as S/N/B.

## Code Generation Behavior

- `:one` SELECT
  - Uses `GetItem` if there is no `@index`.
  - Otherwise uses `Query` with `Limit=2` and returns `ErrInvalid` if more than one item is returned.
- `:many` SELECT
  - Uses `Query` with a paginator, returning all pages.
  - `LIMIT` / `@limit` caps the total number of results.
- `INSERT` -> `PutItem`
  - Queries prefixed with `Create` add a conditional expression (`attribute_not_exists(pk)`) and return `ErrConflict` on conflict.
- `UPDATE` -> `UpdateItem`
  - Adds `attribute_exists(pk)` to prevent blind writes.
  - `:one` requires `RETURNING ALL OLD|NEW` and returns the item.
- `DELETE` -> `DeleteItem`
  - Adds `attribute_exists(pk)` to prevent blind deletes.
  - `:one` requires `RETURNING ALL OLD` and returns the item.

Generated code depends on runtime helpers and errors defined in `internal/ddb/runtime.go`:
`ErrNotFound`, `ErrConflict`, `ErrInvalid`, `ErrThrottled`, `ErrNotFoundTable`, plus helpers such as
`Queries`, `marshal`, `unmarshal`, `marshalValue`, `ptrBool`, `normalizeError`, `invalidErr`,
and `isConditionalFailed`. If you change `--pkg`, copy `internal/ddb/runtime.go` into that package
or provide equivalent definitions.

## Model Mapping

- dynac reads Go structs from `--model` paths and matches attributes via `dynamodbav` tags.
- Every attribute referenced in a query must exist on the selected model type.
- If you omit `@model`, dynac infers the model name from the query name (e.g. `GetUser` -> `User`, `ListUsersByOrg` -> `User`).

## Development

- `go test ./...`
- `go vet ./...`
- `gofmt -w ./cmd ./internal`
- `go run ./cmd/dynac`

## Limitations (Current)

- No projections other than `SELECT *`.
- No filters on non-key attributes.
- No `OR`, `NOT`, `IN`, or comparison operators other than `=` and `BETWEEN` (plus `begins_with` on the sort key).
- No batch APIs yet (BatchGet/BatchWrite/ExecuteStatement).

---

If you'd like, I can also add a worked example under `docs/` or expand the README with a generated-code snippet.
