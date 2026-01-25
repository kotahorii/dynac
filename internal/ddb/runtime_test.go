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
