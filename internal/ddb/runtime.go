package ddb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
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

func (q *Queries) marshalKey(v any, keys ...string) (map[string]types.AttributeValue, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("no key attributes")
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("nil value")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct")
	}
	rt := rv.Type()
	raw := make(map[string]any, len(keys))
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("dynamodbav")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		if _, ok := keySet[name]; !ok {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanInterface() {
			continue
		}
		raw[name] = fv.Interface()
	}
	for _, key := range keys {
		if _, ok := raw[key]; !ok {
			return nil, fmt.Errorf("missing key %s", key)
		}
	}
	out := make(map[string]types.AttributeValue, len(raw))
	for name, val := range raw {
		av, err := q.marshalValue(val)
		if err != nil {
			return nil, err
		}
		out[name] = av
	}
	return out, nil
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
	_ = (*Queries).marshalKey
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
