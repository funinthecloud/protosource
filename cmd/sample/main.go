package main

import (
	"context"

	"github.com/davecgh/go-spew/spew"
	"github.com/funinthecloud/protosource"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
)

func main() {
	foo := GetMeARepo()

	const SampleId = "56286b71-1c41-4300-86d7-29e4a94f0d2c"

	const SmallBody = `0123456789`
	const BigBody = `0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789
0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789
0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789
0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789`

	create := &samplev1.Create{
		Id:    SampleId,
		Actor: "SeanConnery",
		Body:  "LOOK AT MY BELLY",
	}

	if _, err := foo.Apply(context.TODO(), create); err != nil {
		panic(err)
	}

	for i := 1; i < 100; i++ {
		u := &samplev1.Update{
			Id:    SampleId,
			Actor: "HelgaFeld",
			Body:  SmallBody,
		}
		if i%2 == 0 {
			u.Body = BigBody
		}
		if _, err := foo.Apply(context.TODO(), u); err != nil {
			panic(err)
		}
	}

	bar, err := foo.Load(context.TODO(), SampleId)
	if err != nil {
		panic(err)
	}
	spew.Dump(bar)

	baz, err := foo.History(context.TODO(), SampleId)
	if err != nil {
		panic(err)
	}
	spew.Dump(baz)

}

func GetMeARepo() *protosource.Repository {

	store := memorystore.New(memorystore.WithSnapshotInterval(samplev1.SnapshotEveryNEvents))
	serializer := protobinaryserializer.NewSerializer()
	return samplev1.NewRepository(protosource.WithStore(store), protosource.WithSerializer(serializer))

}
