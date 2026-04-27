package completion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/crossplane-contrib/function-hcl/language-server/internal/document"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/eventbus"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/features/modules"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/filesystem"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder"
	ourschema "github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/schema"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/resource"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/resource/loader"
	"github.com/ghodss/yaml"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"
)

const testFileName = "test.hcl"

// scaffold implements the requirements for completion by actually
// setting up the packages that provide these interfaces.
// This is probably slower than anything we could otherwise do but
// avoids having to mock the whole world. Of course, this assumes
// that the other modules have been appropriately tested and don't
// have bugs. Martin Fowler would be proud. Not!
type scaffold struct {
	bc            *byteCompute
	mods          *modules.Modules // embedding automatically implements decoder.Context
	dir           string
	fileUnderTest string
}

type fakeDocStore struct{}

func (f fakeDocStore) Get(handle document.Handle) (*document.Document, error) {
	return nil, document.NotFound(handle.FullURI())
}

func (f fakeDocStore) List(handle document.DirHandle) []*document.Document {
	return nil
}

func (f fakeDocStore) HasOpenDocuments(dirHandle document.DirHandle) bool {
	return false
}

func (f fakeDocStore) IsDocumentOpen(dh document.Handle) bool {
	return false
}

type simpleSchemaProvider struct {
	schemas *resource.Schemas
}

func (s *simpleSchemaProvider) get(_ string) modules.DynamicSchemas {
	return s.schemas
}

var (
	_ filesystem.DocumentStore = &fakeDocStore{}
	_ modules.DocStore         = &fakeDocStore{}
)

func schemasFromDir(t *testing.T, dir string) *resource.Schemas {
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	var list []*resource.Schemas
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, file.Name())
		l := loader.NewFile(path)
		sch, err := l.Load()
		require.NoError(t, err)
		list = append(list, sch)
	}
	return resource.Compose(list...)
}

var (
	stdSchemas *resource.Schemas
	l          sync.Mutex
)

func getSchemas(t *testing.T) *resource.Schemas {
	l.Lock()
	defer l.Unlock()
	if stdSchemas != nil {
		return stdSchemas
	}
	stdSchemas = schemasFromDir(t, filepath.Join("testdata", "schemas"))
	return stdSchemas
}

type byteCompute struct {
	eolChars int
	text     []string
}

func (b *byteCompute) fixBytePos(t *testing.T, pos hcl.Pos) hcl.Pos {
	if pos.Line < 1 {
		require.Fail(t, "line position %d must be at least 1", pos.Line)
	}
	if pos.Line > len(b.text)+1 {
		require.Fail(t, "bad line pos", "line position %d too large (have %d lines)", pos.Line, len(b.text))
	}
	offset := 0
	for i := 0; i < pos.Line-1; i++ {
		offset += len(b.text[i]) + b.eolChars
	}
	offset += pos.Column - 1
	pos.Byte = offset
	return pos
}

func newByteCompute(t *testing.T, file string) *byteCompute {
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	lines := strings.Split(string(b), "\n") // TODO: windoze where it is \r\n
	return &byteCompute{
		text:     lines,
		eolChars: 1,
	}
}

type xrd struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

func newTextScaffold(t *testing.T, text string, xrd *xrd) *scaffold {
	text = strings.TrimPrefix(text, "\n")
	tmpDir, err := os.MkdirTemp("", "completion*")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})
	err = os.WriteFile(filepath.Join(tmpDir, testFileName), []byte(text), 0o644)
	require.NoError(t, err)
	if xrd != nil {
		b, err := yaml.Marshal(map[string]any{"xrd": xrd})
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), b, 0o644)
		require.NoError(t, err)
	}
	return newScaffold(t, tmpDir, testFileName)
}

func newScaffold(t *testing.T, hclDir string, fileUnderTest string) *scaffold {
	ds := fakeDocStore{}
	fs := filesystem.New(ds)
	ss := &simpleSchemaProvider{schemas: getSchemas(t)}
	mods, err := modules.New(modules.Config{
		EventBus: eventbus.New(),
		DocStore: ds,
		FS:       fs,
		Provider: ss.get,
	})
	require.NoError(t, err)
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	mods.Start(ctx)

	dir, err := filepath.Abs(hclDir)
	require.NoError(t, err)

	bc := newByteCompute(t, filepath.Join(dir, fileUnderTest))

	err = mods.ProcessOpenForTesting(dir)
	require.NoError(t, err)
	return &scaffold{
		bc:            bc,
		mods:          mods,
		dir:           dir,
		fileUnderTest: fileUnderTest,
	}
}

func (s *scaffold) completionContext(t *testing.T, pos hcl.Pos) (decoder.CompletionContext, hcl.Pos) {
	p := lang.Path{
		Path:       s.dir,
		LanguageID: ourschema.LanguageHCL,
	}
	pos = s.bc.fixBytePos(t, pos)
	cc, err := s.mods.PathCompletionContext(p, s.fileUnderTest, pos)
	require.NoError(t, err)
	return cc, pos
}

func getCandidateLabels(candidates []lang.Candidate) []string {
	labels := make([]string, len(candidates))
	for i, c := range candidates {
		labels[i] = c.Label
	}
	return labels
}
