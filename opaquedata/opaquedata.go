package opaquedata

import (
	"context"
	"fmt"

	"github.com/funinthecloud/protosource"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	"google.golang.org/protobuf/proto"
)

type AutoPKSK interface {
	proto.Message
	PK() string
	SK() string
	GSI1PK() string
	GSI1SK() string
	GSI2PK() string
	GSI2SK() string
	GSI3PK() string
	GSI3SK() string
	GSI4PK() string
	GSI4SK() string
	GSI5PK() string
	GSI5SK() string
	GSI6PK() string
	GSI6SK() string
	GSI7PK() string
	GSI7SK() string
	GSI8PK() string
	GSI8SK() string
	GSI9PK() string
	GSI9SK() string
	GSI10PK() string
	GSI10SK() string
	GSI11PK() string
	GSI11SK() string
	GSI12PK() string
	GSI12SK() string
	GSI13PK() string
	GSI13SK() string
	GSI14PK() string
	GSI14SK() string
	GSI15PK() string
	GSI15SK() string
	GSI16PK() string
	GSI16SK() string
	GSI17PK() string
	GSI17SK() string
	GSI18PK() string
	GSI18SK() string
	GSI19PK() string
	GSI19SK() string
	GSI20PK() string
	GSI20SK() string
}

// OpaqueStore is the interface that store adapters implement to persist and
// retrieve OpaqueData projections. Each adapter maps OpaqueData fields to
// its native storage format (DynamoDB items, Cosmos containers, etc.).
type OpaqueStore interface {
	Put(ctx context.Context, od *opaquedatav1.OpaqueData) error
	Get(ctx context.Context, pk, sk string) (*opaquedatav1.OpaqueData, error)
	Delete(ctx context.Context, pk, sk string) error
	Query(ctx context.Context, pkAttr, pkValue, skAttr string, sort *SortCondition, opts ...QueryOption) ([]*opaquedatav1.OpaqueData, error)
}

type Hydrater interface {
	proto.Message
	Hydrate(body []byte) error
}

func NewOpaqueDataFromProto(msg AutoPKSK, opts ...Option) (*opaquedatav1.OpaqueData, error) {
	o := buildOptions(opts)

	body, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("opaquedata: marshal: %w", err)
	}

	body, err = protosource.MaybeCompress(body, o.compressThreshold)
	if err != nil {
		return nil, fmt.Errorf("opaquedata: compress: %w", err)
	}

	od := &opaquedatav1.OpaqueData{
		Pk:      msg.PK(),
		Sk:      msg.SK(),
		Body:    body,
		T:       GetTTL(o.ttl),
		Gsi1Pk:  msg.GSI1PK(),
		Gsi1Sk:  msg.GSI1SK(),
		Gsi2Pk:  msg.GSI2PK(),
		Gsi2Sk:  msg.GSI2SK(),
		Gsi3Pk:  msg.GSI3PK(),
		Gsi3Sk:  msg.GSI3SK(),
		Gsi4Pk:  msg.GSI4PK(),
		Gsi4Sk:  msg.GSI4SK(),
		Gsi5Pk:  msg.GSI5PK(),
		Gsi5Sk:  msg.GSI5SK(),
		Gsi6Pk:  msg.GSI6PK(),
		Gsi6Sk:  msg.GSI6SK(),
		Gsi7Pk:  msg.GSI7PK(),
		Gsi7Sk:  msg.GSI7SK(),
		Gsi8Pk:  msg.GSI8PK(),
		Gsi8Sk:  msg.GSI8SK(),
		Gsi9Pk:  msg.GSI9PK(),
		Gsi9Sk:  msg.GSI9SK(),
		Gsi10Pk: msg.GSI10PK(),
		Gsi10Sk: msg.GSI10SK(),
		Gsi11Pk: msg.GSI11PK(),
		Gsi11Sk: msg.GSI11SK(),
		Gsi12Pk: msg.GSI12PK(),
		Gsi12Sk: msg.GSI12SK(),
		Gsi13Pk: msg.GSI13PK(),
		Gsi13Sk: msg.GSI13SK(),
		Gsi14Pk: msg.GSI14PK(),
		Gsi14Sk: msg.GSI14SK(),
		Gsi15Pk: msg.GSI15PK(),
		Gsi15Sk: msg.GSI15SK(),
		Gsi16Pk: msg.GSI16PK(),
		Gsi16Sk: msg.GSI16SK(),
		Gsi17Pk: msg.GSI17PK(),
		Gsi17Sk: msg.GSI17SK(),
		Gsi18Pk: msg.GSI18PK(),
		Gsi18Sk: msg.GSI18SK(),
		Gsi19Pk: msg.GSI19PK(),
		Gsi19Sk: msg.GSI19SK(),
		Gsi20Pk: msg.GSI20PK(),
		Gsi20Sk: msg.GSI20SK(),
	}

	return od, nil
}

func NewOpaqueKeyFromProto(msg AutoPKSK) *opaquedatav1.OpaqueData {
	return &opaquedatav1.OpaqueData{
		Pk: msg.PK(),
		Sk: msg.SK(),
	}
}

func ReHydrate(od *opaquedatav1.OpaqueData, target Hydrater) error {
	body, err := protosource.MaybeDecompress(od.GetBody())
	if err != nil {
		return fmt.Errorf("opaquedata: decompress: %w", err)
	}
	return target.Hydrate(body)
}
