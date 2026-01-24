package ddb

import (
	"errors"
	"fmt"

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
	// Referenced by generated code.
	_ = (*Queries).marshal
	_ = (*Queries).unmarshal
	_ = (*Queries).marshalValue
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
