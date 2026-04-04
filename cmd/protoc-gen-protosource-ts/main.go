package main

import (
	"os"

	pgs "github.com/lyft/protoc-gen-star/v2"
)

func main() {
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
