package main

import (
	"go/format"
	"strings"

	pgs "github.com/lyft/protoc-gen-star/v2"
)

// GoFmtProcessor formats generated Go files using go/format.Source.
// This replaces pgsgo.GoFmt() to avoid importing the pgsgo package
// solely for formatting.
type GoFmtProcessor struct{}

func (GoFmtProcessor) Match(a pgs.Artifact) bool {
	var n string
	switch a := a.(type) {
	case pgs.GeneratorFile:
		n = a.Name
	case pgs.GeneratorTemplateFile:
		n = a.Name
	case pgs.CustomFile:
		n = a.Name
	case pgs.CustomTemplateFile:
		n = a.Name
	default:
		return false
	}
	return strings.HasSuffix(n, ".go")
}

func (GoFmtProcessor) Process(in []byte) ([]byte, error) {
	return format.Source(in)
}
