// Package composition provides loading, analysis and archive creation of a function-hcl
// composition module. It also processes a composition.yaml metadata file that is optionally
// present in the composition directory.
package composition

import (
	"io/fs"

	"golang.org/x/tools/txtar"
)

const ConfigFile = "composition.yaml"

type XRD struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// FS is the minimal filesystem interface needed to load files for a module.
type FS interface {
	Stat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	ReadFile(name string) ([]byte, error)
}

// Config represents the configuration for the composition in terms of library file requirements
// and XRD type information.
type Config struct {
	Version      string   `json:"version"`
	XRD          XRD      `json:"xrd"`
	LibraryFiles []string `json:"libraryFiles"`
}

// Load returns composition information and a list of files to process from a specific directory.
// File paths in the list are relative to the directory that was loaded.
func Load(fs FS, dir string) (*Config, []string, error) {
	l := newLoader(fs)
	return l.load(dir)
}

// Package combines all HCL files and any additional library files and returns a byte array
// that contains the entire package in txtar format.
func Package(dir string, skipAnalysis bool) ([]byte, error) {
	l := newLoader(osFs{})
	archive, files, err := l.loadArchive(dir)
	if err != nil {
		return nil, err
	}
	if !skipAnalysis {
		if err = doAnalyze(files); err != nil {
			return nil, err
		}
	}
	return txtar.Format(archive), nil
}

// Analyze analyzes all HCL files and any additional library files and returns an error on a failed analysis.
func Analyze(dir string) error {
	l := newLoader(osFs{})
	_, files, err := l.loadArchive(dir)
	if err != nil {
		return err
	}
	if err = doAnalyze(files); err != nil {
		return err
	}
	return nil
}
