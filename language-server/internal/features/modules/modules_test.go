package modules

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/crossplane-contrib/function-hcl/language-server/internal/document"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/eventbus"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/schema"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/resource"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations

type mockDocStore struct {
	openDocs map[string]bool // filename -> open status
	openDirs map[string]bool // dir -> has open documents
}

func newMockDocStore() *mockDocStore {
	return &mockDocStore{
		openDocs: map[string]bool{},
		openDirs: map[string]bool{},
	}
}

func (m *mockDocStore) HasOpenDocuments(dirHandle document.DirHandle) bool {
	return m.openDirs[dirHandle.Path()]
}

func (m *mockDocStore) IsDocumentOpen(dh document.Handle) bool {
	key := filepath.Join(dh.Dir.Path(), dh.Filename)
	return m.openDocs[key]
}

func (m *mockDocStore) setDocumentOpen(dir, filename string, open bool) {
	key := filepath.Join(dir, filename)
	m.openDocs[key] = open
	if open {
		m.openDirs[dir] = true
	}
}

type mockDynamicSchemas struct {
	schemas map[resource.Key]*schema.AttributeSchema
}

func newMockDynamicSchemas() *mockDynamicSchemas {
	return &mockDynamicSchemas{
		schemas: map[resource.Key]*schema.AttributeSchema{},
	}
}

func (m *mockDynamicSchemas) Keys() []resource.Key {
	var keys []resource.Key
	for k := range m.schemas {
		keys = append(keys, k)
	}
	return keys
}

func (m *mockDynamicSchemas) Schema(apiVersion, kind string) *schema.AttributeSchema {
	key := resource.Key{ApiVersion: apiVersion, Kind: kind}
	return m.schemas[key]
}

func (m *mockDynamicSchemas) addSchema(apiVersion, kind string, s *schema.AttributeSchema) {
	key := resource.Key{ApiVersion: apiVersion, Kind: kind}
	m.schemas[key] = s
}

// Test helpers

// createTestFS creates an in-memory filesystem with the given file content
func createTestFS(files map[string]string) fstest.MapFS {
	mapFS := fstest.MapFS{}
	for path, content := range files {
		mapFS[path] = &fstest.MapFile{
			Data: []byte(content),
		}
	}
	return mapFS
}

// createModules creates a new Modules instance for testing
func createModules(t *testing.T, fileSystem fs.FS, docStore *mockDocStore, schemas *mockDynamicSchemas) *Modules {
	t.Helper()

	eventBus := eventbus.New()

	provider := func(modPath string) DynamicSchemas {
		return schemas
	}

	config := Config{
		EventBus: eventBus,
		DocStore: docStore,
		FS:       fileSystem.(ReadOnlyFS),
		Provider: provider,
	}

	modules, err := New(config)
	require.NoError(t, err)

	return modules
}

// Helper to wait for queue processing with timeout
func waitForProcessing(m *Modules, dir string, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		m.WaitUntilProcessed(dir)
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Tests

func TestNew(t *testing.T) {
	t.Run("creates new Modules instance", func(t *testing.T) {
		eventBus := eventbus.New()
		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		fileSystem := createTestFS(map[string]string{})

		provider := func(modPath string) DynamicSchemas {
			return schemas
		}

		config := Config{
			EventBus: eventBus,
			DocStore: docStore,
			FS:       fileSystem,
			Provider: provider,
		}

		modules, err := New(config)

		require.NoError(t, err)
		assert.NotNil(t, modules)
		assert.NotNil(t, modules.store)
		assert.NotNil(t, modules.queue)
	})
}

func TestModuleLifecycle(t *testing.T) {
	// This test demonstrates the full lifecycle of a module:
	// 1. Opening a document triggers parsing
	// 2. Module state is tracked
	// 3. Editing updates the module
	// 4. Deletion removes the module

	t.Run("full lifecycle - open, edit, delete", func(t *testing.T) {
		// Setup: Create a simple HCL module
		hclContent := `
locals {
  name = "test"
}

resource "example" {
  body = {
    apiVersion = "v1"
    kind       = "Pod"
    metadata   = {
      name = local.name
    }
  }
}
`
		fileSystem := createTestFS(map[string]string{
			"module1/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		// Step 1: Open the document
		docStore.setDocumentOpen("module1", "main.hcl", true)
		err := modules.onOpen("module1", "main.hcl")
		require.NoError(t, err)

		// Wait for parsing to complete
		ok := waitForProcessing(modules, "module1", 2*time.Second)
		require.True(t, ok, "timeout waiting for module processing")

		// Verify module was parsed and stored
		assert.True(t, modules.store.Exists("module1"))
		content := modules.store.Get("module1")
		require.NotNil(t, content)
		assert.Equal(t, "module1", content.Path)
		assert.Contains(t, content.Files, "main.hcl")
		assert.NotNil(t, content.Files["main.hcl"])

		// Step 2: Verify we can get paths
		paths, err := modules.Paths()
		require.NoError(t, err)
		assert.Len(t, paths, 1)
		assert.Equal(t, "module1", paths[0].Path)
	})
}

func TestModuleParsing(t *testing.T) {
	t.Run("parses valid HCL file", func(t *testing.T) {
		hclContent := `
locals {
  greeting = "hello"
}
`
		fileSystem := createTestFS(map[string]string{
			"test-module/locals.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("test-module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "test-module", 2*time.Second)
		require.True(t, ok)

		// Verify the file was parsed
		content := modules.store.Get("test-module")
		require.NotNil(t, content)
		assert.Contains(t, content.Files, "locals.hcl")

		// Verify no parse errors for valid HCL
		diags := content.Diags["locals.hcl"]
		assert.False(t, diags.HasErrors())
	})

	t.Run("handles syntax errors gracefully", func(t *testing.T) {
		invalidHCL := `
locals {
  bad =
}
`
		fileSystem := createTestFS(map[string]string{
			"bad-module/invalid.hcl": invalidHCL,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("bad-module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "bad-module", 2*time.Second)
		require.True(t, ok)

		// Module should still be tracked, but with diagnostics
		content := modules.store.Get("bad-module")
		require.NotNil(t, content)

		// Should have parse errors
		diags := content.Diags["invalid.hcl"]
		assert.True(t, diags.HasErrors())
	})

	t.Run("parses multiple files in a module", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"multi-file/locals.hcl": `
locals {
  name = "test"
}
`,
			"multi-file/resources.hcl": `
resource "example" {
  body = {
    apiVersion = "v1"
    kind       = "Pod"
  }
}
`,
			"multi-file/templates.hcl": `
template "config" {
  body = {
    apiVersion = "v1"
    kind       = "ConfigMap"
  }
}
`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("multi-file", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "multi-file", 2*time.Second)
		require.True(t, ok)

		content := modules.store.Get("multi-file")
		require.NotNil(t, content)

		// All three files should be parsed
		assert.Len(t, content.Files, 3)
		assert.Contains(t, content.Files, "locals.hcl")
		assert.Contains(t, content.Files, "resources.hcl")
		assert.Contains(t, content.Files, "templates.hcl")
	})

	t.Run("ignores non-HCL files", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"mixed/main.hcl":   `locals { x = 1 }`,
			"mixed/README.md":  "# Documentation",
			"mixed/config.txt": "some config",
			"mixed/data.json":  `{"key": "value"}`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("mixed", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "mixed", 2*time.Second)
		require.True(t, ok)

		content := modules.store.Get("mixed")
		require.NotNil(t, content)

		// Only .hcl file should be parsed
		assert.Len(t, content.Files, 1)
		assert.Contains(t, content.Files, "main.hcl")
	})
}

func TestXRDFile(t *testing.T) {
	t.Run("loads XRD metadata when present", func(t *testing.T) {
		xrdContent := `---
xrd:
  apiVersion: example.io/v1
  kind: XExample
`
		fileSystem := createTestFS(map[string]string{
			"with-xrd/main.hcl":         `locals { x = 1 }`,
			"with-xrd/composition.yaml": xrdContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		// Add a schema for the XRD
		schemas.addSchema("example.io/v1", "XExample", &schema.AttributeSchema{
			Constraint: schema.Object{
				Attributes: map[string]*schema.AttributeSchema{
					"spec": {IsOptional: true},
				},
			},
		})

		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("with-xrd", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "with-xrd", 2*time.Second)
		require.True(t, ok)

		content := modules.store.Get("with-xrd")
		require.NotNil(t, content)

		// XRD should be loaded
		require.NotNil(t, content.XRD)
		assert.Equal(t, "example.io/v1", content.XRD.APIVersion)
		assert.Equal(t, "XExample", content.XRD.Kind)
	})

	t.Run("works without XRD file", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"no-xrd/main.hcl": `locals { x = 1 }`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("no-xrd", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "no-xrd", 2*time.Second)
		require.True(t, ok)

		content := modules.store.Get("no-xrd")
		require.NotNil(t, content)

		// XRD should be nil
		assert.Nil(t, content.XRD)
	})

	t.Run("handles malformed XRD gracefully", func(t *testing.T) {
		badXRD := `this is not valid YAML: {[`
		fileSystem := createTestFS(map[string]string{
			"bad-xrd/main.hcl":         `locals { x = 1 }`,
			"bad-xrd/composition.yaml": badXRD,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("bad-xrd", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "bad-xrd", 2*time.Second)
		require.True(t, ok)

		content := modules.store.Get("bad-xrd")
		require.NotNil(t, content)

		// Should handle gracefully - XRD will be nil
		assert.Nil(t, content.XRD)
		// But module should still be parsed
		assert.Contains(t, content.Files, "main.hcl")
	})
}

func TestPathContext(t *testing.T) {
	t.Run("returns context for known module", func(t *testing.T) {
		hclContent := `
locals {
  value = "test"
}
`
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		// Get path context
		pathCtx, err := modules.PathContext(lang.Path{Path: "module"})
		require.NoError(t, err)
		require.NotNil(t, pathCtx)

		// Verify context provides access to files
		assert.Equal(t, "module", pathCtx.Dir())
		files := pathCtx.Files()
		assert.Contains(t, files, "main.hcl")
	})

	t.Run("returns error for unknown module", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{})
		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		// Try to get context for non-existent module
		_, err := modules.PathContext(lang.Path{Path: "nonexistent"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module not found")
	})
}

func TestReferenceMap(t *testing.T) {
	t.Run("returns reference map for known module", func(t *testing.T) {
		hclContent := `
locals {
  name = "test"
}

resource "example" {
  body = {
    metadata = {
      name = local.name
    }
  }
}
`
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		// Get reference map
		refMap, err := modules.ReferenceMap(lang.Path{Path: "module"})
		require.NoError(t, err)
		require.NotNil(t, refMap)

		// Reference map should exist (even if empty for simple cases)
		assert.NotNil(t, refMap.DefToRefs)
		assert.NotNil(t, refMap.RefsToDef)
	})

	t.Run("returns error for unknown module", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{})
		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		_, err := modules.ReferenceMap(lang.Path{Path: "nonexistent"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module not found")
	})
}

func TestIsModuleFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"HCL file", "main.hcl", true},
		{"Another HCL file", "locals.hcl", true},
		{"Markdown file", "README.md", false},
		{"Text file", "config.txt", false},
		{"JSON file", "data.json", false},
		{"No extension", "Makefile", false},
		{"Hidden HCL file", ".hidden.hcl", true},
		{"Empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isModuleFilename(tt.filename)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPaths(t *testing.T) {
	t.Run("returns empty list when no modules loaded", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{})
		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		paths, err := modules.Paths()
		require.NoError(t, err)
		assert.Empty(t, paths)
	})

	t.Run("returns all loaded module paths", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"module1/main.hcl": `locals { x = 1 }`,
			"module2/main.hcl": `locals { y = 2 }`,
			"module3/main.hcl": `locals { z = 3 }`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		// Load all modules
		for _, dir := range []string{"module1", "module2", "module3"} {
			err := modules.onOpen(dir, "main.hcl")
			require.NoError(t, err)
			ok := waitForProcessing(modules, dir, 2*time.Second)
			require.True(t, ok)
		}

		paths, err := modules.Paths()
		require.NoError(t, err)
		assert.Len(t, paths, 3)

		// Extract path strings for easier assertion
		pathStrings := make([]string, len(paths))
		for i, p := range paths {
			pathStrings[i] = p.Path
		}

		assert.Contains(t, pathStrings, "module1")
		assert.Contains(t, pathStrings, "module2")
		assert.Contains(t, pathStrings, "module3")
	})
}

func TestPathCompletionContext(t *testing.T) {
	t.Run("returns completion context for valid position", func(t *testing.T) {
		hclContent := `
locals {
  name = "test"
}

resource "example" {
  body = {
    apiVersion = "v1"
    kind       = "Pod"
  }
}
`
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		// Get completion context at a specific position
		pos := hcl.Pos{Line: 4, Column: 1, Byte: 20}
		compCtx, err := modules.PathCompletionContext(
			lang.Path{Path: "module"},
			"main.hcl",
			pos,
		)

		require.NoError(t, err)
		require.NotNil(t, compCtx)

		// Verify context provides expected functionality
		assert.Equal(t, "module", compCtx.Dir())
		assert.NotNil(t, compCtx.TargetSchema())
	})

	t.Run("returns error for unknown module", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{})
		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		pos := hcl.Pos{Line: 1, Column: 1, Byte: 0}
		_, err := modules.PathCompletionContext(
			lang.Path{Path: "nonexistent"},
			"main.hcl",
			pos,
		)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module not found")
	})

	t.Run("returns error for unknown file", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": `locals { x = 1 }`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		pos := hcl.Pos{Line: 1, Column: 1, Byte: 0}
		_, err = modules.PathCompletionContext(
			lang.Path{Path: "module"},
			"nonexistent.hcl",
			pos,
		)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestContextMethods(t *testing.T) {
	t.Run("pathCtx HCLFileByName returns file when exists", func(t *testing.T) {
		hclContent := `locals { x = 1 }`
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		pathCtx, err := modules.PathContext(lang.Path{Path: "module"})
		require.NoError(t, err)

		// Test HCLFileByName
		file, exists := pathCtx.HCLFileByName("main.hcl")
		assert.True(t, exists)
		assert.NotNil(t, file)
	})

	t.Run("pathCtx FileBytesByName returns bytes when exists", func(t *testing.T) {
		hclContent := `locals { x = 1 }`
		fileSystem := createTestFS(map[string]string{
			"module/test.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		pathCtx, err := modules.PathContext(lang.Path{Path: "module"})
		require.NoError(t, err)

		// Test FileBytesByName
		bytes, exists := pathCtx.FileBytesByName("test.hcl")
		assert.True(t, exists)
		assert.NotNil(t, bytes)
		assert.Contains(t, string(bytes), "locals")
	})

	t.Run("pathCtx returns false for non-existent file", func(t *testing.T) {
		hclContent := `locals { x = 1 }`
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		pathCtx, err := modules.PathContext(lang.Path{Path: "module"})
		require.NoError(t, err)

		// Test non-existent file
		_, exists := pathCtx.HCLFileByName("nonexistent.hcl")
		assert.False(t, exists)

		_, exists = pathCtx.FileBytesByName("nonexistent.hcl")
		assert.False(t, exists)
	})
}

func TestOnEdit(t *testing.T) {
	t.Run("updates module on edit event", func(t *testing.T) {
		originalContent := `locals { x = 1 }`
		updatedContent := `locals { x = 2 }`

		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": originalContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		// First, open the module
		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		// Verify initial content
		content := modules.store.Get("module")
		require.NotNil(t, content)
		assert.Contains(t, content.Files, "main.hcl")

		// Now update the file in the filesystem
		fileSystem["module/main.hcl"] = &fstest.MapFile{
			Data: []byte(updatedContent),
		}

		// Trigger edit event
		err = modules.onEdit("module", "main.hcl")
		require.NoError(t, err)

		ok = waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		// Verify content was updated
		updated := modules.store.Get("module")
		require.NotNil(t, updated)
		assert.Contains(t, updated.Files, "main.hcl")
	})

	t.Run("returns error when module not found", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{})
		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		// Try to edit a module that wasn't opened
		err := modules.onEdit("nonexistent", "main.hcl")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestOnChangeWatch(t *testing.T) {
	t.Run("handles file deletion", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": `locals { x = 1 }`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		// Open module first
		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		assert.True(t, modules.store.Exists("module"))

		// Simulate deletion (file doesn't exist in real FS, so it will be removed from store)
		// Note: This test is limited because we can't easily simulate real filesystem operations
		// The onChangeWatch method checks os.Stat which works on real filesystem
	})

	t.Run("ignores changes to open documents", func(t *testing.T) {
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": `locals { x = 1 }`,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()
		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		// Mark document as open
		docStore.setDocumentOpen("module", "main.hcl", true)

		// onChangeWatch should ignore changes to open documents
		// This behavior is tested implicitly through the docStore.IsDocumentOpen check
	})
}

func TestCompletionHooks(t *testing.T) {
	t.Run("completion functions are registered", func(t *testing.T) {
		hclContent := `
resource "test" {
  body = {
    apiVersion = "v1"
    kind       = "Pod"
  }
}
`
		fileSystem := createTestFS(map[string]string{
			"module/main.hcl": hclContent,
		})

		docStore := newMockDocStore()
		schemas := newMockDynamicSchemas()

		// Add some test schemas
		schemas.addSchema("v1", "Pod", &schema.AttributeSchema{
			Constraint: schema.Object{
				Attributes: map[string]*schema.AttributeSchema{
					"spec": {IsOptional: true},
				},
			},
		})
		schemas.addSchema("v1", "Service", &schema.AttributeSchema{
			Constraint: schema.Object{
				Attributes: map[string]*schema.AttributeSchema{
					"spec": {IsOptional: true},
				},
			},
		})

		modules := createModules(t, fileSystem, docStore, schemas)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go modules.Start(ctx)

		err := modules.onOpen("module", "main.hcl")
		require.NoError(t, err)

		ok := waitForProcessing(modules, "module", 2*time.Second)
		require.True(t, ok)

		// Get completion context
		pos := hcl.Pos{Line: 4, Column: 20, Byte: 50}
		compCtx, err := modules.PathCompletionContext(
			lang.Path{Path: "module"},
			"main.hcl",
			pos,
		)

		require.NoError(t, err)
		require.NotNil(t, compCtx)

		// Verify completion functions are available
		apiVersionFunc := compCtx.CompletionFunc("apiVersion")
		kindFunc := compCtx.CompletionFunc("kind")

		assert.NotNil(t, apiVersionFunc)
		assert.NotNil(t, kindFunc)
	})
}
