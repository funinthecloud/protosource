package main

import (
	"os"

	pgs "github.com/lyft/protoc-gen-star/v2"
	pgsgo "github.com/lyft/protoc-gen-star/v2/lang/go"
)

func main() {
	if stdin := os.Getenv("STDIN"); len(stdin) != 0 {
		stdinFile, err := os.Open(stdin)
		if err != nil {
			panic(err)
		}
		os.Stdin = stdinFile
	}
	pgs.Init(
		pgs.DebugEnv("DEBUG"),
	).RegisterModule(
		Protosourceify(),
	).RegisterPostProcessor(
		pgsgo.GoFmt(),
	).Render()
}
