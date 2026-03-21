package opaquedata

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
)

type SortOperator int

const (
	Equal      SortOperator = iota
	Lt                      // <
	Le                      // <=
	Gt                      // >
	Ge                      // >=
	Between                 // BETWEEN value AND value2
	BeginsWith              // begins_with
)

type SortCondition struct {
	Operator SortOperator
	Value    string
	Value2   string // only used by Between
}

type Querier interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// DynamoDBer extends Querier with the full set of single-item operations
// needed by generated opaquedata clients. It is satisfied by *dynamodb.Client.
type DynamoDBer interface {
	Querier
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

type QueryOption func(*queryOptions)

type queryOptions struct {
	gsiIndex int // 0 = main table, 1-20 = GSI index
}

func WithGSIIndex(n int) QueryOption {
	return func(o *queryOptions) { o.gsiIndex = n }
}

func QueryPKSK(ctx context.Context, client Querier, table, pkAttr, pkValue string, skAttr string, sort *SortCondition, opts ...QueryOption) ([]*opaquedatav1.OpaqueData, error) {
	qo := &queryOptions{}
	for _, fn := range opts {
		fn(qo)
	}

	if qo.gsiIndex < 0 || qo.gsiIndex > 20 {
		return nil, fmt.Errorf("opaquedata: GSI index %d out of range [0,20]", qo.gsiIndex)
	}

	exprNames := map[string]string{
		"#pk":  pkAttr,
		"#ttl": "ttl",
	}
	exprValues := map[string]types.AttributeValue{
		":pk":   &types.AttributeValueMemberS{Value: pkValue},
		":zero": &types.AttributeValueMemberN{Value: "0"},
		":now":  &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
	}

	keyCondition := "#pk = :pk"

	if sort != nil {
		exprNames["#sk"] = skAttr
		exprValues[":sk"] = &types.AttributeValueMemberS{Value: sort.Value}

		switch sort.Operator {
		case Equal:
			keyCondition += " AND #sk = :sk"
		case Lt:
			keyCondition += " AND #sk < :sk"
		case Le:
			keyCondition += " AND #sk <= :sk"
		case Gt:
			keyCondition += " AND #sk > :sk"
		case Ge:
			keyCondition += " AND #sk >= :sk"
		case Between:
			exprValues[":sk2"] = &types.AttributeValueMemberS{Value: sort.Value2}
			keyCondition += " AND #sk BETWEEN :sk AND :sk2"
		case BeginsWith:
			keyCondition += " AND begins_with(#sk, :sk)"
		default:
			return nil, fmt.Errorf("opaquedata: unknown sort operator %d", sort.Operator)
		}
	}

	filterExpr := "attribute_not_exists(#ttl) OR #ttl = :zero OR #ttl > :now"

	input := &dynamodb.QueryInput{
		TableName:                 &table,
		KeyConditionExpression:    &keyCondition,
		FilterExpression:          &filterExpr,
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
	}

	if qo.gsiIndex > 0 {
		indexName := fmt.Sprintf("gsi%dpk-gsi%dsk-index", qo.gsiIndex, qo.gsiIndex)
		input.IndexName = &indexName
	}

	var results []*opaquedatav1.OpaqueData
	for {
		resp, err := client.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("opaquedata: query: %w", err)
		}

		for _, item := range resp.Items {
			var od opaquedatav1.OpaqueData
			if err := attributevalue.UnmarshalMap(item, &od); err != nil {
				return nil, fmt.Errorf("opaquedata: unmarshal item: %w", err)
			}
			results = append(results, &od)
		}

		if resp.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = resp.LastEvaluatedKey
	}

	return results, nil
}

// GSIIndexName returns the DynamoDB index name for the given GSI number (1-20).
func GSIIndexName(n int) string {
	return fmt.Sprintf("gsi%dpk-gsi%dsk-index", n, n)
}
