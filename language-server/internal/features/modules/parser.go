// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package modules

import (
	"path/filepath"
	"strings"

	"github.com/crossplane-contrib/function-hcl/language-server/internal/features/modules/store"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/target"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/schema"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/utils/perf"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func isModuleFilename(name string) bool {
	return strings.HasSuffix(name, ".hcl")
}

func (m *Modules) loadAndParseModule(modPath string, sourceFiles []string) (map[string]*hcl.File, map[string]hcl.Diagnostics, error) {
	fs := m.fs
	parser := hclparse.NewParser()
	files := map[string]*hcl.File{}
	diags := map[string]hcl.Diagnostics{}

	for _, name := range sourceFiles {
		src, err := fs.ReadFile(filepath.Join(modPath, name))
		if err != nil {
			m.logger.Printf("error reading file: %v", err)
			// If a file isn't accessible, continue with reading the
			// remaining module files
			continue
		}
		f, pDiags := parser.ParseHCL(src, name)
		diags[name] = pDiags
		if f != nil {
			files[name] = f
		}
	}
	return files, diags, nil
}

func (m *Modules) parseModuleFile(filePath string) (*hcl.File, hcl.Diagnostics, error) {
	fs := m.fs
	parser := hclparse.NewParser()
	src, err := fs.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	f, pDiags := parser.ParseHCL(src, filepath.Base(filePath))
	return f, pDiags, nil
}

type derivedData struct {
	targets *target.Targets
	refMap  *target.ReferenceMap
}

func (m *Modules) deriveData(modPath string, files map[string]*hcl.File, xrd *store.XRD) derivedData {
	defer perf.Measure("derivedData")()
	lookup := m.provider(modPath)
	var compositeSchema *schema.AttributeSchema
	if xrd != nil {
		compositeSchema = lookup.Schema(xrd.APIVersion, xrd.Kind)
	}
	targets := target.BuildTargets(files, m.provider(modPath), compositeSchema)
	refMap := target.BuildReferenceMap(files, targets)
	return derivedData{targets: targets, refMap: refMap}
}
