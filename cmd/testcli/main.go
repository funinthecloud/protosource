package main

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"text/tabwriter"
	"time"

	"github.com/funinthecloud/protosource"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/boltdbstore"
)

const usage = `Usage: testcli <command> [args]

Commands:
  create  <id> <body>   Create a new test aggregate
  update  <id> <body>   Update an existing aggregate
  lock    <id>          Lock the aggregate
  unlock  <id>          Unlock the aggregate
  load    <id>          Load and display the aggregate
  history <id>          Show event history

Actor is derived automatically from the current user and hostname.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	store, err := boltdbstore.New("./data/testcli", "test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	serializer := protobinaryserializer.NewSerializer()
	repo := testv1.NewRepository(store, serializer)

	actor := detectActor()
	ctx := context.Background()
	subcmd := os.Args[1]

	switch subcmd {
	case "create":
		if len(os.Args) != 4 {
			fatal("usage: testcli create <id> <body>")
		}
		cmd := &testv1.Create{Id: os.Args[2], Actor: actor, Body: os.Args[3]}
		applyAndPrint(ctx, repo, cmd)

	case "update":
		if len(os.Args) != 4 {
			fatal("usage: testcli update <id> <body>")
		}
		cmd := &testv1.Update{Id: os.Args[2], Actor: actor, Body: os.Args[3]}
		applyAndPrint(ctx, repo, cmd)

	case "lock":
		if len(os.Args) != 3 {
			fatal("usage: testcli lock <id>")
		}
		cmd := &testv1.Lock{Id: os.Args[2], Actor: actor}
		applyAndPrint(ctx, repo, cmd)

	case "unlock":
		if len(os.Args) != 3 {
			fatal("usage: testcli unlock <id>")
		}
		cmd := &testv1.Unlock{Id: os.Args[2], Actor: actor}
		applyAndPrint(ctx, repo, cmd)

	case "load":
		if len(os.Args) != 3 {
			fatal("usage: testcli load <id>")
		}
		agg, err := repo.Load(ctx, os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printAggregate(agg)

	case "history":
		if len(os.Args) != 3 {
			fatal("usage: testcli history <id>")
		}
		hist, err := repo.History(ctx, os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("History: %d record(s)\n\n", len(hist.GetRecords()))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "VERSION\tEVENT\tACTOR\tAT\tBYTES")
		fmt.Fprintln(w, "-------\t-----\t-----\t--\t-----")
		for _, rec := range hist.GetRecords() {
			event, err := serializer.UnmarshalEvent(rec)
			if err != nil {
				fmt.Fprintf(w, "%d\t(error)\t\t\t%d\n", rec.GetVersion(), len(rec.GetData()))
				continue
			}
			name := string(event.ProtoReflect().Descriptor().Name())
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\n",
				event.GetVersion(), name, event.GetActor(),
				formatMicros(event.GetAt()), len(rec.GetData()))
		}
		w.Flush()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s\n", subcmd, usage)
		os.Exit(1)
	}
}

func applyAndPrint(ctx context.Context, repo *protosource.Repository, cmd protosource.Commander) {
	if _, err := repo.Apply(ctx, cmd); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	agg, err := repo.Load(ctx, cmd.GetId())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading after apply: %v\n", err)
		os.Exit(1)
	}
	printAggregate(agg)
}

func printAggregate(agg protosource.Aggregate) {
	t := agg.(*testv1.Test)
	fmt.Printf("ID:        %s\n", t.GetId())
	fmt.Printf("Version:   %d\n", t.GetVersion())
	fmt.Printf("State:     %s\n", t.GetState())
	fmt.Printf("Body:      %s\n", t.GetBody())
	fmt.Printf("CreatedAt: %s\n", formatMicros(t.GetCreateAt()))
	fmt.Printf("CreatedBy: %s\n", t.GetCreateBy())
	fmt.Printf("ModifyAt:  %s\n", formatMicros(t.GetModifyAt()))
	fmt.Printf("ModifyBy:  %s\n", t.GetModifyBy())
}

func formatMicros(us int64) string {
	if us == 0 {
		return "(none)"
	}
	return time.UnixMicro(us).Format(time.RFC3339)
}

func detectActor() string {
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	hostname := "unknown"
	if h, err := os.Hostname(); err == nil {
		hostname = h
	}
	return username + "@" + hostname
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
