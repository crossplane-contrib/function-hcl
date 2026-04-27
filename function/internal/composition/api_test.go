package composition

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
)

func TestPackageLib(t *testing.T) {
	dir := filepath.Join("testdata", "with-libs")
	cfg, files, err := Load(osFs{}, dir, false)
	require.NoError(t, err)
	assert.Equal(t, "example.com/v1", cfg.XRD.APIVersion)
	assert.Equal(t, "FooBar", cfg.XRD.Kind)
	require.Equal(t, 1, len(cfg.LibraryFiles))
	require.Equal(t, 2, len(files))
	assert.Contains(t, files, "main.hcl")
	assert.Contains(t, files, "lib/bar.hcl")

	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 2)
	err = Analyze(dir)
	require.NoError(t, err)
}

func TestPackageNoLib(t *testing.T) {
	dir := filepath.Join("testdata", "dir-only")
	b, err := Package(dir, false)
	require.NoError(t, err)
	archive := txtar.Parse(b)
	require.Len(t, archive.Files, 1)
	err = Analyze(dir)
	require.NoError(t, err)
}

func TestLoadMetadataErrorsBadYAML(t *testing.T) {
	dir := filepath.Join("testdata", "metadata-errors")
	cfg, files, err := Load(osFs{}, dir, true)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "", cfg.XRD.APIVersion)
	require.Equal(t, "", cfg.XRD.Kind)
	require.Equal(t, 1, len(files))
}

func TestLoadMetadataErrorsBadLibPaths(t *testing.T) {
	dir := filepath.Join("testdata", "bad-lib-paths")
	cfg, files, err := Load(osFs{}, dir, true)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "example.com/v1", cfg.XRD.APIVersion)
	require.Equal(t, "FooBar", cfg.XRD.Kind)
	require.Equal(t, 2, len(files))
}
