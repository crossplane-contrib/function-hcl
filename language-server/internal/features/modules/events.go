package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/crossplane-contrib/function-hcl/function/api"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/document"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/eventbus"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/features/modules/store"
	lsp "github.com/crossplane-contrib/function-hcl/language-server/internal/langserver/protocol"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/utils/perf"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/utils/queue"
	"github.com/hashicorp/hcl/v2"
)

func (m *Modules) start(ctx context.Context) {
	m.queue.Start(ctx)
	open := m.eventbus.SubscribeToOpenEvents("feature.modules")
	edit := m.eventbus.SubscribeToEditEvents("feature.modules")
	changeWatch := m.eventbus.SubscribeToChangeWatchEvents("feature.modules")
	go func() {
		for {
			var err error
			select {
			case event := <-open:
				err = m.onOpen(event.Doc.Dir.Path(), event.Doc.Filename)
			case event := <-edit:
				err = m.onEdit(event.Doc.Dir.Path(), event.Doc.Filename)
			case event := <-changeWatch:
				err = m.onChangeWatch(event.ChangeType, event.RawPath, event.IsDir)
			case <-ctx.Done():
				m.logger.Print("stopped modules feature")
				return
			}
			if err != nil {
				m.logger.Printf("modules: process event: %q", err)
			}
		}
	}()
}

// ProcessOpenForTesting processes an open event in a completely synchronous fashion
// for use by unit tests in other packages. It assumes that the dir has not been opened.
func (m *Modules) ProcessOpenForTesting(dir string) error {
	if m.store.Exists(dir) { // new doc opened for known dir, nothing to do
		return fmt.Errorf("module %q already exists", dir)
	}
	return m.fullParse(dir)()
}

func (m *Modules) onOpen(dir, filename string) (err error) {
	if m.store.Exists(dir) {
		// Module exists - add the new file to it
		content := m.store.Get(dir)
		if content == nil {
			return nil
		}
		filePath := filepath.Join(dir, filename)
		m.queue.Enqueue(queue.Key(dir), func() error {
			return m.incrementalParse(content, filePath)
		})
		return nil
	}
	m.queue.Enqueue(queue.Key(dir), m.fullParse(dir))
	return nil
}

func (m *Modules) onEdit(dir, filename string) (err error) {
	content := m.store.Get(dir)
	if content == nil {
		return fmt.Errorf("module %q not found when processing edit event", dir)
	}
	filePath := filepath.Join(dir, filename)
	m.queue.Enqueue(queue.Key(dir), func() error {
		return m.incrementalParse(content, filePath)
	})
	return nil
}

func (m *Modules) onChangeWatch(changeType lsp.FileChangeType, rawPath string, isDir bool) error {
	if changeType == lsp.Deleted {
		path := rawPath
		// we don't know whether file or dir is being deleted
		// 1st we just blindly try to look it up as a directory
		hasModuleRecord := m.store.Exists(path)
		if !hasModuleRecord {
			// otherwise try the parent dir
			path = filepath.Dir(path)
			hasModuleRecord = m.store.Exists(path)
		}
		// nothing to do if not found in our store
		if !hasModuleRecord {
			return nil
		}
		// if the path no longer exists, nuke it internally
		_, err := os.Stat(path)
		if err != nil && os.IsNotExist(err) {
			m.queue.Dequeue(queue.Key(path))
			m.store.Remove(path)
		}
		return nil
	}

	dir := rawPath
	filename := ""
	if !isDir {
		dir = filepath.Dir(rawPath)
		filename = filepath.Base(rawPath)
	}

	// if a file that is being edited is changed, we do not need to reprocess the module.
	if filename != "" {
		h := document.Handle{
			Dir:      document.DirHandleFromPath(dir),
			Filename: filename,
		}
		if m.docStore.IsDocumentOpen(h) {
			return nil
		}
	}

	x, err := os.Stat(dir)
	if err != nil || !x.IsDir() {
		m.logger.Printf("error checking existence (%q), or not a directory: %s", dir, err)
		return err
	}

	// If the parent directory exists, we just need to
	// check if the there are open documents for the path and that the
	// path is a module path. If so, we need to reparse the module.
	hasOpenDocs := m.docStore.HasOpenDocuments(document.DirHandleFromPath(dir))
	if !hasOpenDocs {
		return nil
	}
	m.queue.Enqueue(queue.Key(dir), m.fullParse(dir))
	return nil
}

func (m *Modules) analyze(content *store.Content, dir string) {
	m.queue.Enqueue(queue.Key(dir+":analysis"), func() error {
		var files []api.File
		for name, v := range content.Files {
			files = append(files, api.File{Name: name, File: v})
		}
		diags := api.Analyze(files...)
		diagsByFile := map[string]hcl.Diagnostics{}
		for _, d := range diags {
			var rng *hcl.Range
			if d.Context != nil {
				rng = d.Context
			} else {
				rng = d.Subject
			}
			if rng == nil {
				continue
			}
			diagsByFile[rng.Filename] = append(diagsByFile[rng.Filename], d)
		}
		for filename, fileDiags := range diagsByFile {
			m.publishDiagnostics(dir, filename, fileDiags)
		}
		return nil
	})
}

func (m *Modules) incrementalParse(content *store.Content, filePath string) error {
	file, diags, err := m.parseModuleFile(filePath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(filePath)
	filename := filepath.Base(filePath)
	content.Files[filename] = file
	content.Diags[filename] = diags
	dd := m.deriveData(dir, content.Files, content.XRD)
	content.Targets = dd.targets
	content.RefMap = dd.refMap
	m.store.Put(content)
	m.publishDiagnostics(dir, filename, diags)
	m.analyze(content, dir)
	return nil
}

func (m *Modules) fullParse(dir string) func() error {
	return func() error {
		defer perf.Measure("fullParse")()
		xrd, sourceFiles, err := api.LoadModule(m.fs, dir, true)
		if err != nil {
			return err
		}
		files, diags, err := m.loadAndParseModule(dir, sourceFiles)
		if err != nil {
			return err
		}
		dd := m.deriveData(dir, files, xrd)
		content := store.Content{
			Path:    dir,
			Files:   files,
			Diags:   diags,
			Targets: dd.targets,
			RefMap:  dd.refMap,
			XRD:     xrd,
		}
		m.store.Put(&content)
		for filename, fileDiags := range diags {
			m.publishDiagnostics(dir, filename, fileDiags)
		}
		m.analyze(&content, dir)
		return nil
	}
}

func (m *Modules) publishDiagnostics(dir, filename string, diags hcl.Diagnostics) {
	m.eventbus.PublishDiagnosticsEvent(eventbus.DiagnosticsEvent{
		Doc: document.Handle{
			Dir:      document.DirHandleFromPath(dir),
			Filename: filename,
		},
		Diags: diags,
	})
}
