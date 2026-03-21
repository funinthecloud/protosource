package opaquedata

import (
	"fmt"

	"github.com/funinthecloud/protosource"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
	"google.golang.org/protobuf/proto"
)

type AutoPKSK interface {
	proto.Message
	DynamoPK() string
	DynamoSK() string
	DynamoGSI1PK() string
	DynamoGSI1SK() string
	DynamoGSI2PK() string
	DynamoGSI2SK() string
	DynamoGSI3PK() string
	DynamoGSI3SK() string
	DynamoGSI4PK() string
	DynamoGSI4SK() string
	DynamoGSI5PK() string
	DynamoGSI5SK() string
	DynamoGSI6PK() string
	DynamoGSI6SK() string
	DynamoGSI7PK() string
	DynamoGSI7SK() string
	DynamoGSI8PK() string
	DynamoGSI8SK() string
	DynamoGSI9PK() string
	DynamoGSI9SK() string
	DynamoGSI10PK() string
	DynamoGSI10SK() string
	DynamoGSI11PK() string
	DynamoGSI11SK() string
	DynamoGSI12PK() string
	DynamoGSI12SK() string
	DynamoGSI13PK() string
	DynamoGSI13SK() string
	DynamoGSI14PK() string
	DynamoGSI14SK() string
	DynamoGSI15PK() string
	DynamoGSI15SK() string
	DynamoGSI16PK() string
	DynamoGSI16SK() string
	DynamoGSI17PK() string
	DynamoGSI17SK() string
	DynamoGSI18PK() string
	DynamoGSI18SK() string
	DynamoGSI19PK() string
	DynamoGSI19SK() string
	DynamoGSI20PK() string
	DynamoGSI20SK() string
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
		Pk:      msg.DynamoPK(),
		Sk:      msg.DynamoSK(),
		Body:    body,
		Ttl:     GetTTL(o.ttl),
		Gsi1Pk:  msg.DynamoGSI1PK(),
		Gsi1Sk:  msg.DynamoGSI1SK(),
		Gsi2Pk:  msg.DynamoGSI2PK(),
		Gsi2Sk:  msg.DynamoGSI2SK(),
		Gsi3Pk:  msg.DynamoGSI3PK(),
		Gsi3Sk:  msg.DynamoGSI3SK(),
		Gsi4Pk:  msg.DynamoGSI4PK(),
		Gsi4Sk:  msg.DynamoGSI4SK(),
		Gsi5Pk:  msg.DynamoGSI5PK(),
		Gsi5Sk:  msg.DynamoGSI5SK(),
		Gsi6Pk:  msg.DynamoGSI6PK(),
		Gsi6Sk:  msg.DynamoGSI6SK(),
		Gsi7Pk:  msg.DynamoGSI7PK(),
		Gsi7Sk:  msg.DynamoGSI7SK(),
		Gsi8Pk:  msg.DynamoGSI8PK(),
		Gsi8Sk:  msg.DynamoGSI8SK(),
		Gsi9Pk:  msg.DynamoGSI9PK(),
		Gsi9Sk:  msg.DynamoGSI9SK(),
		Gsi10Pk: msg.DynamoGSI10PK(),
		Gsi10Sk: msg.DynamoGSI10SK(),
		Gsi11Pk: msg.DynamoGSI11PK(),
		Gsi11Sk: msg.DynamoGSI11SK(),
		Gsi12Pk: msg.DynamoGSI12PK(),
		Gsi12Sk: msg.DynamoGSI12SK(),
		Gsi13Pk: msg.DynamoGSI13PK(),
		Gsi13Sk: msg.DynamoGSI13SK(),
		Gsi14Pk: msg.DynamoGSI14PK(),
		Gsi14Sk: msg.DynamoGSI14SK(),
		Gsi15Pk: msg.DynamoGSI15PK(),
		Gsi15Sk: msg.DynamoGSI15SK(),
		Gsi16Pk: msg.DynamoGSI16PK(),
		Gsi16Sk: msg.DynamoGSI16SK(),
		Gsi17Pk: msg.DynamoGSI17PK(),
		Gsi17Sk: msg.DynamoGSI17SK(),
		Gsi18Pk: msg.DynamoGSI18PK(),
		Gsi18Sk: msg.DynamoGSI18SK(),
		Gsi19Pk: msg.DynamoGSI19PK(),
		Gsi19Sk: msg.DynamoGSI19SK(),
		Gsi20Pk: msg.DynamoGSI20PK(),
		Gsi20Sk: msg.DynamoGSI20SK(),
	}

	return od, nil
}

func NewOpaqueKeyFromProto(msg AutoPKSK) *opaquedatav1.OpaqueData {
	return &opaquedatav1.OpaqueData{
		Pk: msg.DynamoPK(),
		Sk: msg.DynamoSK(),
	}
}

func ReHydrate(od *opaquedatav1.OpaqueData, target Hydrater) error {
	body, err := protosource.MaybeDecompress(od.GetBody())
	if err != nil {
		return fmt.Errorf("opaquedata: decompress: %w", err)
	}
	return target.Hydrate(body)
}
