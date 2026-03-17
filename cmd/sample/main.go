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
			Body:  "Burp",
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

}

func GetMeARepo() *protosource.Repository {

	serializer := protobinaryserializer.NewSerializer()
	store := memorystore.New(memorystore.WithSnapshotInterval(10))
	return protosource.New(&samplev1.Sample{}, protosource.WithSerializer(serializer), protosource.WithStore(store))

}
