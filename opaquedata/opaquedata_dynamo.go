package opaquedata

import (
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
)

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

func GetKey(od *opaquedatav1.OpaqueData) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: od.GetPk()},
		"sk": &types.AttributeValueMemberS{Value: od.GetSk()},
	}
}

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

func GetValue(od *opaquedatav1.OpaqueData) map[string]types.AttributeValue {
	item := GetItem(od)
	delete(item, "pk")
	delete(item, "sk")
	return item
}

// PrefixPKs prepends prefix+"#" to the primary PK and all non-empty GSI PKs.
// This is used by multi-tenant stores to isolate key spaces.
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
