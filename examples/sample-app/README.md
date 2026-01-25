# Sample App

Minimal demo that exercises Get/List/Put/Update/Delete against a DynamoDB table
using generated dynac code.

## Prerequisites

- AWS credentials configured
- A DynamoDB table named `App` with `pk` (HASH) and `sk` (RANGE) string keys

Create the table (once):

```sh
aws dynamodb create-table \
  --table-name App \
  --attribute-definitions AttributeName=pk,AttributeType=S AttributeName=sk,AttributeType=S \
  --key-schema AttributeName=pk,KeyType=HASH AttributeName=sk,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST
```

## Generate code

From the repo root:

```sh
go run ./cmd/dynac generate \
  --table App \
  --queries ./examples/sample-app/queries \
  --model ./examples/sample-app/internal/ddb \
  --pkg ./examples/sample-app/internal/ddb
```

This writes `queries_gen.go` and `runtime_gen.go` into `examples/sample-app/internal/ddb`.

## Run

```sh
DYNAC_TABLE=App go run ./examples/sample-app
```

The program will:

- Create a user (PutItem with a conditional check)
- Get the user
- List users by org (Query + begins_with)
- Update the user email
- Delete the user
