package dynamo

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
)

// DynamoDBer is the minimal DynamoDB interface needed by the dynamo adapter.
// It is satisfied by *dynamodb.Client.
type DynamoDBer interface {
	Querier
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// Querier is the subset of DynamoDB operations needed for queries.
type Querier interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// Store implements opaquedata.OpaqueStore backed by DynamoDB.
type Store struct {
	client       DynamoDBer
	tableName    string
	tenantPrefix string
}

// Option configures a Store.
type Option func(*Store)

// WithTenantPrefix prepends "prefix#" to all PKs and GSI PKs on Put.
func WithTenantPrefix(prefix string) Option {
	return func(s *Store) { s.tenantPrefix = prefix }
}

// New creates a new DynamoDB-backed OpaqueStore.
func New(client DynamoDBer, tableName string, opts ...Option) *Store {
	s := &Store{client: client, tableName: tableName}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) Put(ctx context.Context, od *opaquedatav1.OpaqueData) error {
	if s.tenantPrefix != "" {
		PrefixPKs(od, s.tenantPrefix)
	}
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      GetItem(od),
	})
	if err != nil {
		return fmt.Errorf("dynamo.Store.Put: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, pk, sk string) (*opaquedatav1.OpaqueData, error) {
	if s.tenantPrefix != "" {
		pk = s.tenantPrefix + "#" + pk
	}
	resp, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.tableName,
		Key:       getKey(pk, sk),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamo.Store.Get: %w", err)
	}
	if resp.Item == nil {
		return nil, opaquedata.ErrNotFound
	}
	var od opaquedatav1.OpaqueData
	if err := attributevalue.UnmarshalMap(resp.Item, &od); err != nil {
		return nil, fmt.Errorf("dynamo.Store.Get: unmarshal: %w", err)
	}
	return &od, nil
}

func (s *Store) Delete(ctx context.Context, pk, sk string) error {
	if s.tenantPrefix != "" {
		pk = s.tenantPrefix + "#" + pk
	}
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.tableName,
		Key:       getKey(pk, sk),
	})
	if err != nil {
		return fmt.Errorf("dynamo.Store.Delete: %w", err)
	}
	return nil
}

func (s *Store) Query(ctx context.Context, pkAttr, pkValue, skAttr string, sort *opaquedata.SortCondition, opts ...opaquedata.QueryOption) ([]*opaquedatav1.OpaqueData, error) {
	qo := opaquedata.ApplyQueryOptions(opts)

	if qo.GSIIndex < 0 || qo.GSIIndex > 20 {
		return nil, fmt.Errorf("opaquedata: GSI index %d out of range [0,20]", qo.GSIIndex)
	}

	if s.tenantPrefix != "" {
		pkValue = s.tenantPrefix + "#" + pkValue
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
		case opaquedata.Equal:
			keyCondition += " AND #sk = :sk"
		case opaquedata.Lt:
			keyCondition += " AND #sk < :sk"
		case opaquedata.Le:
			keyCondition += " AND #sk <= :sk"
		case opaquedata.Gt:
			keyCondition += " AND #sk > :sk"
		case opaquedata.Ge:
			keyCondition += " AND #sk >= :sk"
		case opaquedata.Between:
			exprValues[":sk2"] = &types.AttributeValueMemberS{Value: sort.Value2}
			keyCondition += " AND #sk BETWEEN :sk AND :sk2"
		case opaquedata.BeginsWith:
			keyCondition += " AND begins_with(#sk, :sk)"
		default:
			return nil, fmt.Errorf("opaquedata: unknown sort operator %d", sort.Operator)
		}
	}

	filterExpr := "attribute_not_exists(#ttl) OR #ttl = :zero OR #ttl > :now"

	input := &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		KeyConditionExpression:    &keyCondition,
		FilterExpression:          &filterExpr,
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
	}

	if qo.GSIIndex > 0 {
		indexName := GSIIndexName(qo.GSIIndex)
		input.IndexName = &indexName
	}

	var results []*opaquedatav1.OpaqueData
	for {
		resp, err := s.client.Query(ctx, input)
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

// getKey builds a DynamoDB key from pk and sk strings.
func getKey(pk, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: pk},
		"sk": &types.AttributeValueMemberS{Value: sk},
	}
}

type gsiPair struct {
	pkAttr, skAttr string
	pkVal, skVal   string
}

func gsiPairs(od *opaquedatav1.OpaqueData) []gsiPair {
	return []gsiPair{
		{"gsi1pk", "gsi1sk", od.GetGsi1Pk(), od.GetGsi1Sk()},
		{"gsi2pk", "gsi2sk", od.GetGsi2Pk(), od.GetGsi2Sk()},
		{"gsi3pk", "gsi3sk", od.GetGsi3Pk(), od.GetGsi3Sk()},
		{"gsi4pk", "gsi4sk", od.GetGsi4Pk(), od.GetGsi4Sk()},
		{"gsi5pk", "gsi5sk", od.GetGsi5Pk(), od.GetGsi5Sk()},
		{"gsi6pk", "gsi6sk", od.GetGsi6Pk(), od.GetGsi6Sk()},
		{"gsi7pk", "gsi7sk", od.GetGsi7Pk(), od.GetGsi7Sk()},
		{"gsi8pk", "gsi8sk", od.GetGsi8Pk(), od.GetGsi8Sk()},
		{"gsi9pk", "gsi9sk", od.GetGsi9Pk(), od.GetGsi9Sk()},
		{"gsi10pk", "gsi10sk", od.GetGsi10Pk(), od.GetGsi10Sk()},
		{"gsi11pk", "gsi11sk", od.GetGsi11Pk(), od.GetGsi11Sk()},
		{"gsi12pk", "gsi12sk", od.GetGsi12Pk(), od.GetGsi12Sk()},
		{"gsi13pk", "gsi13sk", od.GetGsi13Pk(), od.GetGsi13Sk()},
		{"gsi14pk", "gsi14sk", od.GetGsi14Pk(), od.GetGsi14Sk()},
		{"gsi15pk", "gsi15sk", od.GetGsi15Pk(), od.GetGsi15Sk()},
		{"gsi16pk", "gsi16sk", od.GetGsi16Pk(), od.GetGsi16Sk()},
		{"gsi17pk", "gsi17sk", od.GetGsi17Pk(), od.GetGsi17Sk()},
		{"gsi18pk", "gsi18sk", od.GetGsi18Pk(), od.GetGsi18Sk()},
		{"gsi19pk", "gsi19sk", od.GetGsi19Pk(), od.GetGsi19Sk()},
		{"gsi20pk", "gsi20sk", od.GetGsi20Pk(), od.GetGsi20Sk()},
	}
}

// GetKey returns the DynamoDB key attributes for an OpaqueData item.
func GetKey(od *opaquedatav1.OpaqueData) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: od.GetPk()},
		"sk": &types.AttributeValueMemberS{Value: od.GetSk()},
	}
}

// GetItem returns the full DynamoDB item for an OpaqueData record.
func GetItem(od *opaquedatav1.OpaqueData) map[string]types.AttributeValue {
	item := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: od.GetPk()},
		"sk": &types.AttributeValueMemberS{Value: od.GetSk()},
	}
	if body := od.GetBody(); len(body) > 0 {
		item["body"] = &types.AttributeValueMemberB{Value: body}
	}
	if ttl := od.GetTtl(); ttl != 0 {
		item["ttl"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(ttl, 10)}
	}
	if v := od.GetVersion(); v != 0 {
		item["version"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(v, 10)}
	}
	for _, g := range gsiPairs(od) {
		if g.pkVal != "" && g.pkVal != "NA" {
			item[g.pkAttr] = &types.AttributeValueMemberS{Value: g.pkVal}
		}
		if g.skVal != "" && g.skVal != "NA" {
			item[g.skAttr] = &types.AttributeValueMemberS{Value: g.skVal}
		}
	}
	return item
}

// GetItems returns a subset of the DynamoDB item attributes by name.
func GetItems(od *opaquedatav1.OpaqueData, names ...string) (map[string]types.AttributeValue, error) {
	full := GetItem(od)
	result := make(map[string]types.AttributeValue, len(names))
	for _, name := range names {
		v, ok := full[name]
		if !ok {
			return nil, fmt.Errorf("opaquedata: attribute %q not found or empty", name)
		}
		result[name] = v
	}
	return result, nil
}

// GetExpressionValues returns DynamoDB expression attribute values (colon-prefixed).
func GetExpressionValues(od *opaquedatav1.OpaqueData, names ...string) (map[string]types.AttributeValue, error) {
	full := GetItem(od)
	result := make(map[string]types.AttributeValue, len(names))
	for _, name := range names {
		v, ok := full[name]
		if !ok {
			return nil, fmt.Errorf("opaquedata: attribute %q not found or empty", name)
		}
		result[":"+name] = v
	}
	return result, nil
}

// GetValue returns all DynamoDB item attributes except pk and sk.
func GetValue(od *opaquedatav1.OpaqueData) map[string]types.AttributeValue {
	item := GetItem(od)
	delete(item, "pk")
	delete(item, "sk")
	return item
}

// PrefixPKs prepends prefix+"#" to the primary PK and all non-empty, non-sentinel
// GSI PKs. Values that are empty or "NA" (unused GSI slots) are left untouched.
// This is used by multi-tenant stores to isolate key spaces.
func PrefixPKs(od *opaquedatav1.OpaqueData, prefix string) {
	pfx := func(v string) string {
		if v == "" || v == "NA" {
			return v
		}
		return prefix + "#" + v
	}
	od.Pk = prefix + "#" + od.Pk
	od.Gsi1Pk = pfx(od.Gsi1Pk)
	od.Gsi2Pk = pfx(od.Gsi2Pk)
	od.Gsi3Pk = pfx(od.Gsi3Pk)
	od.Gsi4Pk = pfx(od.Gsi4Pk)
	od.Gsi5Pk = pfx(od.Gsi5Pk)
	od.Gsi6Pk = pfx(od.Gsi6Pk)
	od.Gsi7Pk = pfx(od.Gsi7Pk)
	od.Gsi8Pk = pfx(od.Gsi8Pk)
	od.Gsi9Pk = pfx(od.Gsi9Pk)
	od.Gsi10Pk = pfx(od.Gsi10Pk)
	od.Gsi11Pk = pfx(od.Gsi11Pk)
	od.Gsi12Pk = pfx(od.Gsi12Pk)
	od.Gsi13Pk = pfx(od.Gsi13Pk)
	od.Gsi14Pk = pfx(od.Gsi14Pk)
	od.Gsi15Pk = pfx(od.Gsi15Pk)
	od.Gsi16Pk = pfx(od.Gsi16Pk)
	od.Gsi17Pk = pfx(od.Gsi17Pk)
	od.Gsi18Pk = pfx(od.Gsi18Pk)
	od.Gsi19Pk = pfx(od.Gsi19Pk)
	od.Gsi20Pk = pfx(od.Gsi20Pk)
}
