//go:build tools
// +build tools

package tools

// Third party stuff
//go:generate go install github.com/bufbuild/buf/cmd/buf
//go:generate go install github.com/google/wire/cmd/wire
//go:generate go install google.golang.org/protobuf/cmd/protoc-gen-go

import (
	_ "github.com/bufbuild/buf/cmd/buf"
	_ "github.com/google/wire/cmd/wire"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
