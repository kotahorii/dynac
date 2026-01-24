package ddb

import (
	"errors"
	"testing"

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
