package api

import (
	"github.com/crossplane-contrib/function-hcl/internal/evaluator"
	"github.com/crossplane-contrib/function-hcl/internal/format"
	"github.com/hashicorp/hcl/v2"
)

// FormatHCL formats the supplied code.
func FormatHCL(code string) string {
	return format.Source(code, format.Options{StandardizeObjectLiterals: true})
}

type File = evaluator.File

// Analyze analyzes the supplied files for correctness.
func Analyze(files ...File) hcl.Diagnostics {
	e, _ := evaluator.New(evaluator.Options{})
	return e.Analyze(files...)
}
