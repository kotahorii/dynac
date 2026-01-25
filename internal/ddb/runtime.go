package ddb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	ErrNotFound      = errors.New("ddb: not found")
	ErrConflict      = errors.New("ddb: conflict")
	ErrInvalid       = errors.New("ddb: invalid")
	ErrUnprocessed   = errors.New("ddb: unprocessed items")
	ErrThrottled     = errors.New("ddb: throttled")
	ErrNotFoundTable = errors.New("ddb: table not found")
)

type Queries struct {
	Client *dynamodb.Client
	Table  string
}

func (q *Queries) marshal(v any) (map[string]types.AttributeValue, error) {
	return attributevalue.MarshalMap(v)
}

func (q *Queries) unmarshal(m map[string]types.AttributeValue, out any) error {
	return attributevalue.UnmarshalMap(m, out)
}

func (q *Queries) marshalValue(v any) (types.AttributeValue, error) {
	return attributevalue.Marshal(v)
}

func ptrBool(v bool) *bool { return &v }

var (
	batchWriteChunk      = 25
	batchWriteMaxRetries = 5
	batchWriteBaseDelay  = 50 * time.Millisecond
	batchWriteSleep      = time.Sleep
)

type batchWriteClient interface {
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

func (q *Queries) batchWrite(ctx context.Context, reqs []types.WriteRequest) error {
	return batchWrite(ctx, q.Client, q.Table, reqs)
}

func batchWrite(ctx context.Context, client batchWriteClient, table string, reqs []types.WriteRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	send := func(items []types.WriteRequest) ([]types.WriteRequest, error) {
		out, err := client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				table: items,
			},
		})
		if err != nil {
			return nil, normalizeError(err)
		}
		return out.UnprocessedItems[table], nil
	}
	for i := 0; i < len(reqs); i += batchWriteChunk {
		end := min(i+batchWriteChunk, len(reqs))
		unprocessed, err := send(reqs[i:end])
		if err != nil {
			return err
		}
		for retry := 0; len(unprocessed) > 0; retry++ {
			if retry >= batchWriteMaxRetries {
				return ErrUnprocessed
			}
			batchWriteSleep(batchWriteBaseDelay * time.Duration(1<<retry))
			unprocessed, err = send(unprocessed)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

var (
	// Referenced by generated code.
	_ = (*Queries).marshal
	_ = (*Queries).unmarshal
	_ = (*Queries).marshalValue
	_ = (*Queries).batchWrite
	_ = ptrBool
)

func isConditionalFailed(err error) bool {
	var ccfe *types.ConditionalCheckFailedException
	return errors.As(err, &ccfe)
}

func isNotFoundTable(err error) bool {
	var rnfe *types.ResourceNotFoundException
	return errors.As(err, &rnfe)
}

func isThrottled(err error) bool {
	var pte *types.ProvisionedThroughputExceededException
	if errors.As(err, &pte) {
		return true
	}
	var te *types.ThrottlingException
	return errors.As(err, &te)
}

func invalidErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	if isNotFoundTable(err) {
		return ErrNotFoundTable
	}
	if isThrottled(err) {
		return ErrThrottled
	}
	return err
}
