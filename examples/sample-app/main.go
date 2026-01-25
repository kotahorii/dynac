package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/kotahorii/dynac/examples/sample-app/internal/ddb"
)

func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	client := dynamodb.NewFromConfig(cfg)
	table := os.Getenv("DYNAC_TABLE")
	if table == "" {
		table = "App"
	}
	q := &ddb.Queries{Client: client, Table: table}

	orgID := "acme"
	userID := "user-1"
	pk := "ORG#" + orgID
	sk := "USER#" + userID

	user := ddb.User{
		PK:        pk,
		SK:        sk,
		UserName:  "Alice",
		Email:     "alice@example.com",
		CreatedAt: time.Now().UTC(),
	}

	if err := q.CreateUser(ctx, user); err != nil {
		if errors.Is(err, ddb.ErrConflict) {
			log.Printf("user already exists: %s/%s", pk, sk)
		} else {
			log.Fatalf("create user: %v", err)
		}
	}

	got, err := q.GetUser(ctx, pk, sk)
	if err != nil {
		log.Fatalf("get user: %v", err)
	}
	log.Printf("get: %s (%s)", got.UserName, got.Email)

	users, err := q.ListUsersByOrg(ctx, pk, "USER#")
	if err != nil {
		log.Fatalf("list users: %v", err)
	}
	log.Printf("list: %d users", len(users))

	if err := q.UpdateUser(ctx, pk, sk, "alice+updated@example.com"); err != nil {
		if errors.Is(err, ddb.ErrNotFound) {
			log.Printf("update: not found")
		} else {
			log.Fatalf("update user: %v", err)
		}
	}

	if err := q.DeleteUser(ctx, pk, sk); err != nil {
		if errors.Is(err, ddb.ErrNotFound) {
			log.Printf("delete: not found")
		} else {
			log.Fatalf("delete user: %v", err)
		}
	}
}
