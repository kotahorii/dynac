# CLAUDE.md

This document provides guidance for AI assistants working with the dynac codebase.

## Project Overview

**dynac** is a type-safe DynamoDB query code generator for Go. It parses constrained PartiQL-like queries from `.partiql` files and generates strongly typed methods using the AWS SDK v2.

The validator is intentionally strict to prevent accidental table scans and unsafe queries.

## Technology Stack

- **Language**: Go 1.25+
- **AWS SDK**: aws-sdk-go-v2
- **Testing**: Go testing, Testcontainers for integration tests
- **Linting**: golangci-lint v2.8.0

## Project Structure

```
dynac/
├── cmd/dynac/              # CLI entry point
│   ├── main.go             # CLI commands: generate, vet
│   └── main_test.go        # CLI tests
├── internal/
│   ├── ddb/                # Runtime helpers for generated code
│   │   ├── runtime.go      # Error types, Queries struct, helpers
│   │   └── runtime_test.go
│   ├── gen/                # Code generator
│   │   ├── gen.go          # Generator struct and code generation
│   │   ├── gen_test.go
│   │   ├── runtime_template.go  # Embedded runtime template
│   │   ├── runtime_template_test.go
│   │   └── runtime.go.tmpl # Template for runtime_gen.go
│   ├── model/              # Go struct parser for model types
│   │   └── model.go        # Parses `dynamodbav` tags
│   ├── partiql/            # PartiQL parser
│   │   └── partiql.go      # Lexer and parser for supported SQL
│   └── query/              # Query loading and validation
│       ├── query.go        # Query struct, validation rules
│       └── query_test.go
├── examples/sample-app/    # Working demo
│   ├── main.go
│   ├── queries/user.partiql
│   └── internal/ddb/
│       ├── models.go
│       ├── queries_gen.go
│       └── runtime_gen.go
├── .github/workflows/ci.yml  # CI: lint, test, integration
├── .golangci.yml            # Linter configuration
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── CHANGELOG.md
```

## Key Packages

### `cmd/dynac`
CLI entry point with two commands:
- `generate` - Parses queries and generates type-safe Go code
- `vet` - Validates queries without generating code

### `internal/partiql`
Custom PartiQL parser that supports a subset of SQL:
- `SELECT * FROM "Table" WHERE ...`
- `INSERT INTO "Table" VALUE ?`
- `UPDATE "Table" SET ... WHERE ...`
- `DELETE FROM "Table" WHERE ...`

Supported conditions: `=`, `begins_with()`, `BETWEEN`
Only `AND` is allowed; `OR`, `NOT`, `IN` are rejected.

### `internal/query`
Handles query file discovery, parsing, and validation:
- Discovers `.partiql` files
- Parses name annotations (`:one`, `:many`, `:exec`)
- Parses annotations (`@limit`, `@nolimit`, `@index`, `@projection`, `@model`)
- Validates DynamoDB-safe query shapes

### `internal/model`
Parses Go struct definitions to extract field types:
- Reads `dynamodbav` struct tags
- Maps attribute names to Go types
- Handles imports for external types

### `internal/gen`
Code generator that produces:
- `queries_gen.go` - Generated query methods
- `runtime_gen.go` - Runtime helpers (only if `runtime.go` doesn't exist)

### `internal/ddb`
Runtime support library with:
- Error types: `ErrNotFound`, `ErrConflict`, `ErrInvalid`, `ErrThrottled`, `ErrNotFoundTable`
- `Queries` struct with DynamoDB client and table name
- Marshal/unmarshal helpers
- Batch write with chunking and retry

## Development Workflow

### Running the CLI

```sh
# Run without installing
go run ./cmd/dynac generate --model ./internal/ddb

# Or install and run
go install ./cmd/dynac
dynac generate --model ./internal/ddb
```

### Testing

```sh
# Run all unit tests
go test ./...

# Run integration tests (requires Docker)
go test ./examples/sample-app/internal/ddb -tags=integration -v -count=1

# Format check
gofmt -l ./cmd ./internal
```

### Linting

```sh
# Using golangci-lint (CI uses v2.8.0)
golangci-lint run --timeout=5m
```

### Code Modernization

```sh
make modernize
```

## Query File Format

Queries are defined in `.partiql` files with this format:

```sql
-- name: QueryName :one|:many|:exec
-- @annotation value
SQL_STATEMENT
```

### Query Kinds
- `:one` - Returns single item (uses GetItem or Query with Limit=2)
- `:many` - Returns multiple items (uses Query with paginator)
- `:exec` - No return value (for INSERT/UPDATE/DELETE)

### Annotations
- `@limit N` - Maximum items for `:many`
- `@nolimit` - Allow unbounded `:many` (explicit opt-in)
- `@index Name pk(a,b) sk(c,d)` - Use GSI/LSI
- `@projection all` - Required with `@index`
- `@model TypeName` - Override model type inference

### Validation Rules
- `:many` requires `LIMIT`, `@limit`, or `@nolimit`
- `:one` requires full key equality
- Only key attributes in WHERE
- PK must use `=`
- SK can use `=`, `begins_with()`, `BETWEEN`
- SK conditions must be left-contiguous

## Code Generation Patterns

### Model Name Inference
Query names follow conventions:
- `GetUser` -> model `User`
- `ListUsersByOrg` -> model `User`
- `CreateUser` -> model `User`
- Prefixes: `Get`, `List`, `Create`, `Update`, `Delete`, `Put`, `BatchPut`, `BatchDelete`
- Suffixes: `By...`, `For...` are stripped before singularization

### Generated Method Signatures

```go
// :one SELECT
func (q *Queries) GetUser(ctx context.Context, pk string, sk string) (User, error)

// :many SELECT
func (q *Queries) ListUsers(ctx context.Context, pk string, skPrefix string) ([]User, error)

// :exec INSERT
func (q *Queries) CreateUser(ctx context.Context, user User) error

// :exec UPDATE
func (q *Queries) UpdateUser(ctx context.Context, pk string, sk string, newEmail string) error

// :exec DELETE
func (q *Queries) DeleteUser(ctx context.Context, pk string, sk string) error
```

### DynamoDB API Mapping
- `:one` SELECT without `@index` -> `GetItem`
- `:one` SELECT with `@index` -> `Query` with `Limit=2`
- `:many` SELECT -> `Query` with paginator
- INSERT -> `PutItem`
- INSERT with `Create` prefix -> `PutItem` with condition (`attribute_not_exists`)
- UPDATE -> `UpdateItem` with condition (`attribute_exists`)
- DELETE -> `DeleteItem` with condition (`attribute_exists`)

## Coding Conventions

### Error Handling
- Use sentinel errors from `internal/ddb/runtime.go`
- Wrap errors with `fmt.Errorf("%w", err)` for chaining
- Use `normalizeError()` to convert AWS errors to sentinel errors

### Code Style
- Run `gofmt` before committing
- Follow golangci-lint rules in `.golangci.yml`
- Enabled linters: errcheck, errorlint, errname, govet, ineffassign, staticcheck, unused, copyloopvar

### Testing
- Unit tests in `*_test.go` files alongside source
- Integration tests use build tag: `//go:build integration`
- Integration tests use Testcontainers with DynamoDB Local

### Comments
- Generated code header: `// Code generated by dynac. DO NOT EDIT.`
- Query methods include comment: `// QueryName :kind`

## CLI Flags

```
dynac generate [flags]
dynac vet [flags]

Flags:
  --table   DynamoDB table name (optional, for validation)
  --pkg     Output package path (default: internal/ddb)
  --model   Model file or directory (repeatable, required for generate)
  --pk      Partition key name (default: pk)
  --sk      Sort key name (default: sk; empty for single-key tables)
  --queries Query root directory (default: queries)
```

## Common Tasks

### Adding a New Query Type
1. Update `internal/partiql/partiql.go` to parse the statement
2. Add validation in `internal/query/query.go`
3. Add code generation in `internal/gen/gen.go`
4. Add tests in corresponding `*_test.go` files

### Adding a New Annotation
1. Update `applyAnnotation()` in `internal/query/query.go`
2. Add validation logic if needed
3. Update code generation in `internal/gen/gen.go` to use the annotation

### Updating Runtime Helpers
1. Modify `internal/ddb/runtime.go`
2. Update `internal/gen/runtime.go.tmpl` if needed
3. Regenerate `internal/gen/runtime_template.go` from the template

## Known Limitations

- Only `SELECT *` projections (no column selection)
- No filters on non-key attributes
- No `OR`, `NOT`, `IN` operators
- No batch read APIs (BatchGetItem)
- SK conditions must be left-contiguous for composite keys

## Error Types Reference

| Error | Description | When Returned |
|-------|-------------|---------------|
| `ErrNotFound` | Item not found | GetItem returns nil, conditional check fails on Update/Delete |
| `ErrConflict` | Item already exists | Create (PutItem with condition) fails |
| `ErrInvalid` | Marshal/unmarshal failure | Invalid data types |
| `ErrThrottled` | Rate limit exceeded | ProvisionedThroughputExceededException |
| `ErrNotFoundTable` | Table doesn't exist | ResourceNotFoundException |
| `ErrUnprocessed` | Batch write failed | Unprocessed items after max retries |
