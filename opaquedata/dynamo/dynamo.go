package dynamo

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/funinthecloud/protosource/aws/dynamoclient"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
)

// Store implements opaquedata.OpaqueStore backed by DynamoDB.
type Store struct {
	client    dynamoclient.Client
	tableName string
}

// New creates a new DynamoDB-backed OpaqueStore.
func New(client dynamoclient.Client, tableName string) *Store {
	return &Store{client: client, tableName: tableName}
}

func (s *Store) Put(ctx context.Context, od *opaquedatav1.OpaqueData) error {
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

	exprNames := map[string]string{
		"#pk":  pkAttr,
		"#t": "t",
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

	filterExpr := "attribute_not_exists(#t) OR #t = :zero OR #t > :now"

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
	if ttl := od.GetT(); ttl != 0 {
		item["t"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(ttl, 10)}
	}
	if v := od.GetVersion(); v != 0 {
		item["version"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(v, 10)}
	}
	for _, g := range gsiPairs(od) {
		if g.pkVal != "" && g.pkVal != "NA" {
			item[g.pkAttr] = &types.AttributeValueMemberS{Value: g.pkVal}
			// Always write SK when PK is present so DynamoDB projects the item into the GSI.
			// Coerce empty SK to "NA" since DynamoDB rejects empty string key attributes.
			skVal := g.skVal
			if skVal == "" {
				skVal = "NA"
			}
			item[g.skAttr] = &types.AttributeValueMemberS{Value: skVal}
		} else if g.skVal != "" && g.skVal != "NA" {
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

