//go:build integration

package ddb

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestCRUDWithDynamoDBLocal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	endpoint := startDynamoDBLocal(t, ctx)
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, _ ...any) (aws.Endpoint, error) {
				if service == dynamodb.ServiceID {
					return aws.Endpoint{
						URL:               endpoint,
						SigningRegion:     region,
						HostnameImmutable: true,
					}, nil
				}
				return aws.Endpoint{}, &aws.EndpointNotFoundError{}
			},
		)),
	)
	if err != nil {
		t.Fatalf("load aws config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)
	table := fmt.Sprintf("dynac_users_%d", time.Now().UnixNano())
	createUserTable(t, ctx, client, table)

	queries := &Queries{Client: client, Table: table}
	createdAt := time.Unix(1_700_000_000, 0).UTC()
	user1 := User{
		PK:        "ORG#1",
		SK:        "USER#1",
		UserName:  "alice",
		Email:     "alice@example.com",
		CreatedAt: createdAt,
	}
	user2 := User{
		PK:        "ORG#1",
		SK:        "USER#2",
		UserName:  "bob",
		Email:     "bob@example.com",
		CreatedAt: createdAt.Add(time.Minute),
	}

	if err := queries.CreateUser(ctx, user1); err != nil {
		t.Fatalf("create user1: %v", err)
	}
	if err := queries.CreateUser(ctx, user2); err != nil {
		t.Fatalf("create user2: %v", err)
	}

	got, err := queries.GetUser(ctx, user1.PK, user1.SK)
	if err != nil {
		t.Fatalf("get user1: %v", err)
	}
	if got.Email != user1.Email {
		t.Fatalf("get user1 email mismatch: got %q", got.Email)
	}

	if err := queries.UpdateUser(ctx, user1.PK, user1.SK, "alice+new@example.com"); err != nil {
		t.Fatalf("update user1: %v", err)
	}
	updated, err := queries.GetUser(ctx, user1.PK, user1.SK)
	if err != nil {
		t.Fatalf("get updated user1: %v", err)
	}
	if updated.Email != "alice+new@example.com" {
		t.Fatalf("updated email mismatch: got %q", updated.Email)
	}

	list, err := queries.ListUsersByOrg(ctx, "ORG#1", "USER#")
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list users count mismatch: got %d", len(list))
	}

	if err := queries.DeleteUser(ctx, user1.PK, user1.SK); err != nil {
		t.Fatalf("delete user1: %v", err)
	}
	_, err = queries.GetUser(ctx, user1.PK, user1.SK)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func startDynamoDBLocal(t *testing.T, ctx context.Context) string {
	t.Helper()
	if !dockerSocketAvailable() {
		t.Skip("docker socket not available or not accessible")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("docker not available: %v", r)
		}
	}()
	req := testcontainers.ContainerRequest{
		Image:        dynamoDBLocalImage(),
		ExposedPorts: []string{"8000/tcp"},
		Cmd:          []string{"-jar", "DynamoDBLocal.jar", "-sharedDb", "-inMemory"},
		WaitingFor:   wait.ForListeningPort("8000/tcp"),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start dynamodb local container: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8000")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func dockerSocketAvailable() bool {
	candidates := []string{
		"/var/run/docker.sock",
		filepath.Join(os.Getenv("HOME"), ".docker", "run", "docker.sock"),
	}
	if host := strings.TrimPrefix(os.Getenv("DOCKER_HOST"), "unix://"); host != os.Getenv("DOCKER_HOST") {
		candidates = append([]string{host}, candidates...)
	}
	for _, path := range candidates {
		if path == "" {
			continue
		}
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err != nil {
			continue
		}
		_ = conn.Close()
		return true
	}
	return false
}

func dynamoDBLocalImage() string {
	if image := os.Getenv("DYNAMODB_LOCAL_IMAGE"); image != "" {
		return image
	}
	return "amazon/dynamodb-local:2.5.0"
}

func createUserTable(t *testing.T, ctx context.Context, client *dynamodb.Client, table string) {
	t.Helper()
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: &table,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	waiter := dynamodb.NewTableExistsWaiter(client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: &table}, time.Minute); err != nil {
		t.Fatalf("wait for table: %v", err)
	}
}
