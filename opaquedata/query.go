package opaquedata

import (
	"context"
	"fmt"
	"strconv"
	"time"

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
			od, err := itemToOpaqueData(item)
			if err != nil {
				return nil, fmt.Errorf("opaquedata: convert item: %w", err)
			}
			results = append(results, od)
		}

		if resp.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = resp.LastEvaluatedKey
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results, nil
}

func itemToOpaqueData(item map[string]types.AttributeValue) (*opaquedatav1.OpaqueData, error) {
	od := &opaquedatav1.OpaqueData{}

	if v, ok := item["pk"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			od.Pk = s.Value
		}
	}
	if v, ok := item["sk"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			od.Sk = s.Value
		}
	}
	if v, ok := item["body"]; ok {
		if b, ok := v.(*types.AttributeValueMemberB); ok {
			od.Body = b.Value
		}
	}
	if v, ok := item["ttl"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			val, err := strconv.ParseInt(n.Value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse ttl: %w", err)
			}
			od.Ttl = val
		}
	}
	if v, ok := item["version"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			val, err := strconv.ParseInt(n.Value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse version: %w", err)
			}
			od.Version = val
		}
	}

	setGSIField := func(attr string, target *string) {
		if v, ok := item[attr]; ok {
			if s, ok := v.(*types.AttributeValueMemberS); ok {
				*target = s.Value
			}
		}
	}

	setGSIField("gsi1pk", &od.Gsi1Pk)
	setGSIField("gsi1sk", &od.Gsi1Sk)
	setGSIField("gsi2pk", &od.Gsi2Pk)
	setGSIField("gsi2sk", &od.Gsi2Sk)
	setGSIField("gsi3pk", &od.Gsi3Pk)
	setGSIField("gsi3sk", &od.Gsi3Sk)
	setGSIField("gsi4pk", &od.Gsi4Pk)
	setGSIField("gsi4sk", &od.Gsi4Sk)
	setGSIField("gsi5pk", &od.Gsi5Pk)
	setGSIField("gsi5sk", &od.Gsi5Sk)
	setGSIField("gsi6pk", &od.Gsi6Pk)
	setGSIField("gsi6sk", &od.Gsi6Sk)
	setGSIField("gsi7pk", &od.Gsi7Pk)
	setGSIField("gsi7sk", &od.Gsi7Sk)
	setGSIField("gsi8pk", &od.Gsi8Pk)
	setGSIField("gsi8sk", &od.Gsi8Sk)
	setGSIField("gsi9pk", &od.Gsi9Pk)
	setGSIField("gsi9sk", &od.Gsi9Sk)
	setGSIField("gsi10pk", &od.Gsi10Pk)
	setGSIField("gsi10sk", &od.Gsi10Sk)
	setGSIField("gsi11pk", &od.Gsi11Pk)
	setGSIField("gsi11sk", &od.Gsi11Sk)
	setGSIField("gsi12pk", &od.Gsi12Pk)
	setGSIField("gsi12sk", &od.Gsi12Sk)
	setGSIField("gsi13pk", &od.Gsi13Pk)
	setGSIField("gsi13sk", &od.Gsi13Sk)
	setGSIField("gsi14pk", &od.Gsi14Pk)
	setGSIField("gsi14sk", &od.Gsi14Sk)
	setGSIField("gsi15pk", &od.Gsi15Pk)
	setGSIField("gsi15sk", &od.Gsi15Sk)
	setGSIField("gsi16pk", &od.Gsi16Pk)
	setGSIField("gsi16sk", &od.Gsi16Sk)
	setGSIField("gsi17pk", &od.Gsi17Pk)
	setGSIField("gsi17sk", &od.Gsi17Sk)
	setGSIField("gsi18pk", &od.Gsi18Pk)
	setGSIField("gsi18sk", &od.Gsi18Sk)
	setGSIField("gsi19pk", &od.Gsi19Pk)
	setGSIField("gsi19sk", &od.Gsi19Sk)
	setGSIField("gsi20pk", &od.Gsi20Pk)
	setGSIField("gsi20sk", &od.Gsi20Sk)

	return od, nil
}

// GSIIndexName returns the DynamoDB index name for the given GSI number (1-20).
func GSIIndexName(n int) string {
	return fmt.Sprintf("gsi%dpk-gsi%dsk-index", n, n)
}
