package api

import (
	"github.com/crossplane-contrib/function-hcl/function/internal/composition"
	"github.com/crossplane-contrib/function-hcl/function/internal/evaluator"
	"github.com/crossplane-contrib/function-hcl/function/internal/format"
	"github.com/hashicorp/hcl/v2"
)

// ConfigFile is the well-named file that contains XRD metadata and library file paths.
const ConfigFile = composition.ConfigFile

// FormatHCL formats the supplied code.
func FormatHCL(code string) string {
	return format.Source(code, format.Options{StandardizeObjectLiterals: true})
}

// File is a named syntax tree.
type File = evaluator.RawFile

// Analyze analyzes the supplied files for correctness.
func Analyze(files ...File) hcl.Diagnostics {
	e, _ := evaluator.New(evaluator.Options{})
	return e.AnalyzeHCLFiles(files...)
}

// FS is a minimal filesystem implementation that the caller can implement.
type FS = composition.FS

// XRD provides the XRD information if available as metadata.
type XRD = composition.XRD

// LoadModule loads metadata and HCL files from the supplied directory and returns the
// results. File paths are relative to the directory that was processed.
func LoadModule(fs FS, dir string, ignoreMetadataErrors bool) (*XRD, []string, error) {
	cfg, files, err := composition.Load(fs, dir, ignoreMetadataErrors)
	if err != nil {
		return nil, nil, err
	}
	var xrd *composition.XRD
	if cfg != nil {
		xrd = &cfg.XRD
	}
	if xrd != nil && (xrd.APIVersion == "" || xrd.Kind == "") {
		xrd = nil
	}
	return xrd, files, nil
}
