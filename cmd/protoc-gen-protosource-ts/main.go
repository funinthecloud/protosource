package main

import (
	"fmt"
	"os"

	pgs "github.com/lyft/protoc-gen-star/v2"
)

// version is set via ldflags at build time (e.g. by goreleaser).
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("protoc-gen-protosource-ts %s\n", version)
			os.Exit(0)
		}
	}

	if stdin := os.Getenv("STDIN"); len(stdin) != 0 {
		stdinFile, err := os.Open(stdin)
		if err != nil {
			panic(err)
		}
		defer stdinFile.Close()
		os.Stdin = stdinFile
	}
	pgs.Init(
		pgs.DebugEnv("DEBUG"),
	).RegisterModule(
		Protosourceify(),
	).Render() // No GoFmt post-processor — output is TypeScript
}
