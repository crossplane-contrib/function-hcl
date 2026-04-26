package composition

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
)

// validResourceHCL is a minimal valid HCL resource block for use in dynamic test fixtures.
const validResourceHCL = `resource cmap {
  body = {
    apiVersion = "v1"
    kind       = "ConfigMap"
    data       = { foo = "bar" }
  }
}
`

// --- Package tests ---

func TestPackage_NonExistentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := Package(dir, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does-not-exist")
}

func TestPackage_FileNotDirectory(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.hcl")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = Package(f.Name(), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a directory")
}

func TestPackage_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Empty(t, archive.Files)
}

func TestPackage_NonHCLFilesAreExcluded(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.hcl"), []byte(validResourceHCL), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("some text"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: value"), 0o644))

	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1, "only .hcl files should be packaged")
}

func TestPackage_HCLSubdirectoryIsExcluded(t *testing.T) {
	// A subdirectory that happens to match the *.hcl glob (unusual but possible) must be skipped.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.hcl"), []byte(validResourceHCL), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub.hcl"), 0o755))

	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1, "directory matching *.hcl glob must not be included")
}

func TestPackage_MultipleHCLFiles(t *testing.T) {
	dir := filepath.Join("testdata", "multi-hcl")
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 2)
}

func TestPackage_ArchiveFileNamesAreRelativeToProcessedDir(t *testing.T) {
	dir := filepath.Join("testdata", "dir-only")
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1)

	assert.Equal(t, "main.hcl", archive.Files[0].Name)
}

func TestPackage_ArchiveFileContentsMatchDisk(t *testing.T) {
	dir := filepath.Join("testdata", "dir-only")
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1)

	expected, err := os.ReadFile(filepath.Join(dir, "main.hcl"))
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(archive.Files[0].Data))
}

func TestPackage_WithLibs_ArchiveContainsBothHCLAndLibFiles(t *testing.T) {
	dir := filepath.Join("testdata", "with-libs")
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 2)

	names := make([]string, len(archive.Files))
	for i, f := range archive.Files {
		names[i] = f.Name
	}
	sort.Strings(names)

	assert.Equal(t, filepath.Join("lib", "bar.hcl"), names[0])
	assert.Equal(t, "main.hcl", names[1])
}

func TestPackage_WithLibs_LibFilesAppendedAfterHCLFiles(t *testing.T) {
	// Library files are appended after the glob'd HCL files.
	dir := filepath.Join("testdata", "with-libs")
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 2)

	assert.True(t, strings.HasSuffix(archive.Files[0].Name, "main.hcl"), "first file should be the glob'd main.hcl")
	assert.True(t, strings.HasSuffix(archive.Files[1].Name, "bar.hcl"), "second file should be the library bar.hcl")
}

func TestPackage_MissingLibraryFile(t *testing.T) {
	dir := filepath.Join("testdata", "missing-lib")
	_, err := Package(dir, false)
	require.Error(t, err)
}

func TestPackage_LibraryFileIsDirectory(t *testing.T) {
	dir := filepath.Join("testdata", "dir-as-lib")
	_, err := Package(dir, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be a directory")
}

func TestPackage_InvalidCompositionYAML(t *testing.T) {
	dir := filepath.Join("testdata", "invalid-yaml-config")
	_, err := Package(dir, false)
	require.Error(t, err)
}

func TestPackage_CompositionYAMLIsADirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a directory named composition.yaml instead of a file.
	require.NoError(t, os.Mkdir(filepath.Join(dir, ConfigFile), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.hcl"), []byte(validResourceHCL), 0o644))

	_, err := Package(dir, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is a directory")
}

func TestPackage_SkipAnalysis_WithInvalidHCL(t *testing.T) {
	// With skipAnalysis=true, packaging succeeds even if HCL is invalid.
	dir := filepath.Join("testdata", "invalid-hcl")
	b, err := Package(dir, true)
	require.NoError(t, err)

	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1)
}

func TestPackage_WithAnalysis_InvalidHCL(t *testing.T) {
	dir := filepath.Join("testdata", "invalid-hcl")
	_, err := Package(dir, false)
	require.Error(t, err)
	require.Equal(t, "analysis failed", err.Error())
}

func TestPackage_AbsoluteLibraryPath(t *testing.T) {
	// Library files specified with absolute paths should be resolved as-is.
	libDir := t.TempDir()
	libFile := filepath.Join(libDir, "mylib.hcl")
	libContent := `function mylib {
  description = "absolute path library"
  arg x {}
  body = x
}
`
	require.NoError(t, os.WriteFile(libFile, []byte(libContent), 0o644))

	compDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "main.hcl"), []byte(validResourceHCL), 0o644))

	configContent := fmt.Sprintf("libraryFiles:\n  - %s\n", libFile)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, ConfigFile), []byte(configContent), 0o644))

	_, err := Package(compDir, true) // skip analysis; lib function isn't used
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is an absolute path, not allowed")
}

func TestPackage_RelativeLibraryPath(t *testing.T) {
	// Library files specified with relative paths are resolved relative to the composition dir.
	compDir := t.TempDir()
	libDir := filepath.Join(compDir, "libs")
	require.NoError(t, os.Mkdir(libDir, 0o755))

	libContent := `function helper {
  description = "relative path library"
  arg v {}
  body = v
}
`
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "helper.hcl"), []byte(libContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "main.hcl"), []byte(validResourceHCL), 0o644))

	configContent := "version: \"1.0\"\nlibraryFiles:\n  - libs/helper.hcl\n"
	require.NoError(t, os.WriteFile(filepath.Join(compDir, ConfigFile), []byte(configContent), 0o644))

	b, err := Package(compDir, true)
	require.NoError(t, err)

	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 2)
}

// --- Analyze tests ---

func TestAnalyze_NonExistentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	err := Analyze(dir)
	require.Error(t, err)
}

func TestAnalyze_FileNotDirectory(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.hcl")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	err = Analyze(f.Name())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a directory")
}

func TestAnalyze_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	err := Analyze(dir)
	require.NoError(t, err)
}

func TestAnalyze_InvalidHCL(t *testing.T) {
	dir := filepath.Join("testdata", "invalid-hcl")
	err := Analyze(dir)
	require.Error(t, err)
	require.Equal(t, "analysis failed", err.Error())
}

func TestAnalyze_MissingLibraryFile(t *testing.T) {
	dir := filepath.Join("testdata", "missing-lib")
	err := Analyze(dir)
	require.Error(t, err)
}

func TestAnalyze_LibraryFileIsDirectory(t *testing.T) {
	dir := filepath.Join("testdata", "dir-as-lib")
	err := Analyze(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be a directory")
}

func TestAnalyze_InvalidCompositionYAML(t *testing.T) {
	dir := filepath.Join("testdata", "invalid-yaml-config")
	err := Analyze(dir)
	require.Error(t, err)
}

func TestAnalyze_ValidSingleFile(t *testing.T) {
	dir := filepath.Join("testdata", "dir-only")
	err := Analyze(dir)
	require.NoError(t, err)
}

func TestAnalyze_ValidWithLibs(t *testing.T) {
	dir := filepath.Join("testdata", "with-libs")
	err := Analyze(dir)
	require.NoError(t, err)
}

func TestAnalyze_ValidMultipleFiles(t *testing.T) {
	dir := filepath.Join("testdata", "multi-hcl")
	err := Analyze(dir)
	require.NoError(t, err)
}

// --- loadConfig tests (exercised via Package/Analyze) ---

func TestPackage_NoCompositionYAML_UsesEmptyConfig(t *testing.T) {
	// When composition.yaml is absent, an empty Config is used (no library files).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.hcl"), []byte(validResourceHCL), 0o644))

	b, err := Package(dir, false)
	require.NoError(t, err)

	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1, "only the single HCL file should be packaged")
}

func TestPackage_ConfigXRDFields(t *testing.T) {
	// XRD fields in composition.yaml are parsed but don't affect packaging output.
	compDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "main.hcl"), []byte(validResourceHCL), 0o644))

	configContent := `version: "1.0"
xrd:
  apiVersion: example.io/v1
  kind: XMyResource
`
	require.NoError(t, os.WriteFile(filepath.Join(compDir, ConfigFile), []byte(configContent), 0o644))

	b, err := Package(compDir, false)
	require.NoError(t, err)

	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1)
}
