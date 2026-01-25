package ddb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestNormalizeError(t *testing.T) {
	if normalizeError(nil) != nil {
		t.Fatalf("expected nil")
	}
	if !errors.Is(normalizeError(&types.ResourceNotFoundException{}), ErrNotFoundTable) {
		t.Fatalf("expected ErrNotFoundTable")
	}
	if !errors.Is(normalizeError(&types.ProvisionedThroughputExceededException{}), ErrThrottled) {
		t.Fatalf("expected ErrThrottled from throughput error")
	}
	if !errors.Is(normalizeError(&types.ThrottlingException{}), ErrThrottled) {
		t.Fatalf("expected ErrThrottled from throttling error")
	}
	other := errors.New("other")
	if !errors.Is(normalizeError(other), other) {
		t.Fatalf("expected original error")
	}
}

func TestInvalidErr(t *testing.T) {
	if invalidErr(nil) != nil {
		t.Fatalf("expected nil")
	}
	err := invalidErr(errors.New("bad"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid wrapping")
	}
}

func TestIsConditionalFailed(t *testing.T) {
	if !isConditionalFailed(&types.ConditionalCheckFailedException{}) {
		t.Fatalf("expected conditional check failed")
	}
	if isConditionalFailed(errors.New("other")) {
		t.Fatalf("did not expect conditional check failed")
	}
}

func TestMarshalKeyIgnoresNonKeyFields(t *testing.T) {
	type Item struct {
		PK  string     `dynamodbav:"pk"`
		Bad func() int `dynamodbav:"bad"`
	}
	q := &Queries{}
	out, err := q.marshalKey(Item{PK: "id", Bad: func() int { return 1 }}, "pk")
	if err != nil {
		t.Fatalf("marshalKey: %v", err)
	}
	if out["pk"] == nil {
		t.Fatalf("expected pk in result")
	}
	if _, ok := out["bad"]; ok {
		t.Fatalf("did not expect non-key field in result")
	}
}

func TestMarshalValueAs(t *testing.T) {
	q := &Queries{}
	if av, err := q.marshalValueAs("id", "S"); err != nil {
		t.Fatalf("marshalValueAs string: %v", err)
	} else if _, ok := av.(*types.AttributeValueMemberS); !ok {
		t.Fatalf("expected string attribute value")
	}
	if av, err := q.marshalValueAs(123, "N"); err != nil {
		t.Fatalf("marshalValueAs number: %v", err)
	} else if _, ok := av.(*types.AttributeValueMemberN); !ok {
		t.Fatalf("expected number attribute value")
	}
	if av, err := q.marshalValueAs([]byte("x"), "B"); err != nil {
		t.Fatalf("marshalValueAs binary: %v", err)
	} else if _, ok := av.(*types.AttributeValueMemberB); !ok {
		t.Fatalf("expected binary attribute value")
	}
	if _, err := q.marshalValueAs(123, "S"); err == nil {
		t.Fatalf("expected type mismatch error")
	}
	if _, err := q.marshalValueAs("id", "X"); err == nil {
		t.Fatalf("expected invalid hint error")
	}
}

func useBatchConfig(t *testing.T, chunk, retries int) {
	t.Helper()
	origChunk := batchWriteChunk
	origRetries := batchWriteMaxRetries
	origDelay := batchWriteBaseDelay
	origSleep := batchWriteSleep
	batchWriteChunk = chunk
	batchWriteMaxRetries = retries
	batchWriteBaseDelay = 0
	batchWriteSleep = func(time.Duration) {}
	t.Cleanup(func() {
		batchWriteChunk = origChunk
		batchWriteMaxRetries = origRetries
		batchWriteBaseDelay = origDelay
		batchWriteSleep = origSleep
	})
}

type fakeBatchWriteClient struct {
	table  string
	reqLen []int
	outs   []map[string][]types.WriteRequest
	errs   []error
}

func (f *fakeBatchWriteClient) BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	_ = ctx
	_ = optFns
	if params != nil {
		f.reqLen = append(f.reqLen, len(params.RequestItems[f.table]))
	} else {
		f.reqLen = append(f.reqLen, 0)
	}
	idx := len(f.reqLen) - 1
	out := &dynamodb.BatchWriteItemOutput{}
	if idx < len(f.outs) {
		out.UnprocessedItems = f.outs[idx]
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return out, err
}

func TestBatchWriteChunks(t *testing.T) {
	useBatchConfig(t, 25, 1)

	reqs := make([]types.WriteRequest, 30)
	client := &fakeBatchWriteClient{table: "App"}
	if err := batchWrite(context.Background(), client, "App", reqs); err != nil {
		t.Fatalf("batchWrite: %v", err)
	}
	if len(client.reqLen) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(client.reqLen))
	}
	if client.reqLen[0] != 25 || client.reqLen[1] != 5 {
		t.Fatalf("unexpected chunk sizes: %v", client.reqLen)
	}
}

func TestBatchWriteRetriesUnprocessed(t *testing.T) {
	useBatchConfig(t, 25, 2)

	req := types.WriteRequest{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: "v"},
	}}}
	client := &fakeBatchWriteClient{
		table: "App",
		outs: []map[string][]types.WriteRequest{
			{"App": {req}},
			{"App": {}},
		},
	}
	if err := batchWrite(context.Background(), client, "App", []types.WriteRequest{req}); err != nil {
		t.Fatalf("batchWrite: %v", err)
	}
	if len(client.reqLen) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(client.reqLen))
	}
	if client.reqLen[0] != 1 || client.reqLen[1] != 1 {
		t.Fatalf("unexpected retry sizes: %v", client.reqLen)
	}
}

func TestBatchWriteErrUnprocessed(t *testing.T) {
	useBatchConfig(t, 25, 1)

	req := types.WriteRequest{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: "v"},
	}}}
	client := &fakeBatchWriteClient{
		table: "App",
		outs: []map[string][]types.WriteRequest{
			{"App": {req}},
			{"App": {req}},
		},
	}
	err := batchWrite(context.Background(), client, "App", []types.WriteRequest{req})
	if !errors.Is(err, ErrUnprocessed) {
		t.Fatalf("expected ErrUnprocessed, got %v", err)
	}
	if len(client.reqLen) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(client.reqLen))
	}
}
