// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package modules provides facilities to track the parsed state of modules
// and provider completion and hover contexts, among other things.
package modules

import (
	"context"
	"io/fs"
	"log"

	"github.com/crossplane-contrib/function-hcl/language-server/internal/document"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/eventbus"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/features/modules/store"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/target"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/schema"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/resource"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/utils/logging"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/utils/queue"
	"github.com/hashicorp/hcl/v2"
)

// dependencies

// ReadOnlyFS is a read-only filesystem needed for module processing.
type ReadOnlyFS interface {
	fs.FS                                       // extends a base FS
	ReadDir(name string) ([]fs.DirEntry, error) // allows listing directory contents.
	ReadFile(name string) ([]byte, error)       // convenience method to read a file.
	Stat(name string) (fs.FileInfo, error)      // provides file information
}

// DocStore provides minimal information about the state of the document store
// for efficient module processing.
type DocStore interface {
	// HasOpenDocuments returns true if the supplied directory has documents open.
	HasOpenDocuments(dirHandle document.DirHandle) bool
	// IsDocumentOpen returns true if the supplied document is currently open.
	IsDocumentOpen(dh document.Handle) bool
}

// DynamicSchemas provides schemas on demand for an API version, kind tuple.
// It can also list all such known tuples.
type DynamicSchemas interface {
	Keys() []resource.Key                                   // return all known keys
	Schema(apiVersion, kind string) *schema.AttributeSchema // return schema for the supplied key
}

// DynamicSchemaProvider provides dynamic schema information for a module rooted at the
// supplied path.
type DynamicSchemaProvider func(modPath string) DynamicSchemas

// Modules groups everything related to modules.
// Its internal state keeps track of all modules in the workspace.
type Modules struct {
	eventbus *eventbus.EventBus
	queue    *queue.Queue
	docStore DocStore
	fs       ReadOnlyFS
	provider DynamicSchemaProvider
	store    *store.Store
	logger   *log.Logger
}

// Config is the feature configuration.
type Config struct {
	EventBus *eventbus.EventBus    // event bus to listen on
	DocStore DocStore              // document store
	FS       ReadOnlyFS            // filesystem that can provide unsaved document changes
	Provider DynamicSchemaProvider // provider to get dynamic schemas for a module, for autocomplete
}

// New returns a new Modules instance.
func New(c Config) (*Modules, error) {
	return &Modules{
		eventbus: c.EventBus,
		queue:    queue.New(1),
		docStore: c.DocStore,
		fs:       c.FS,
		provider: c.Provider,
		logger:   logging.LoggerFor(logging.ModuleModules),
		store:    store.New(),
	}, nil
}

// Start starts background activities. It terminates when the supplied context is closed.
func (m *Modules) Start(ctx context.Context) {
	m.start(ctx)
}

// PathContext returns the context for the supplied path.
func (m *Modules) PathContext(p lang.Path) (decoder.Context, error) {
	return m.pathContext(p)
}

// PathCompletionContext returns the completion/ hover context for the supplied path, for a given position in a
// specific file. This takes into account the variables that are visible from that position.
func (m *Modules) PathCompletionContext(p lang.Path, filename string, pos hcl.Pos) (decoder.CompletionContext, error) {
	return m.pathCompletionContext(p, filename, pos)
}

// ReferenceMap returns the map of document references from declaration to references and vice-versa.
func (m *Modules) ReferenceMap(p lang.Path) (*target.ReferenceMap, error) {
	return m.referenceMap(p)
}

// WaitUntilProcessed waits until all jobs currently queued for the supplied directory complete.
func (m *Modules) WaitUntilProcessed(dir string) {
	m.queue.WaitForKey(queue.Key(dir))
}
