// Package store tracks derived information for modules. A module is equivalent to a directory on the filesystem.
package store

import (
	"sync"

	"github.com/crossplane-contrib/function-hcl/function/api"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/target"
	"github.com/hashicorp/hcl/v2"
)

type XRD = api.XRD

func copyMap[T any](in map[string]T) map[string]T {
	ret := map[string]T{}
	for k, v := range in {
		ret[k] = v
	}
	return ret
}

// Content contains information being tracked for a module.
type Content struct {
	Path    string                     // the directory of the module.
	Files   map[string]*hcl.File       // parsed files by unqualified filename.
	Diags   map[string]hcl.Diagnostics // diags by unqualified filename.
	Targets *target.Targets            // computed reference targets.
	RefMap  *target.ReferenceMap       // computed reference map.
	XRD     *XRD                       // XRD information, if present in the module metadata file.
}

// toModule adapts a content record to module format.
func (c *Content) toModule() *module {
	return &module{
		path:    c.Path,
		files:   copyMap(c.Files),
		diags:   copyMap(c.Diags),
		refMap:  c.RefMap,
		targets: c.Targets,
		xrd:     c.XRD,
	}
}

// module represents the information we need to track for every module that is processed.
type module struct {
	path    string
	files   map[string]*hcl.File
	diags   map[string]hcl.Diagnostics
	targets *target.Targets
	refMap  *target.ReferenceMap
	xrd     *XRD
}

// Content returns the current contents of the module as a read only copy.
func (m *module) Content() *Content {
	return &Content{
		Path:    m.path,
		Files:   copyMap(m.files),
		Diags:   copyMap(m.diags),
		Targets: m.targets,
		RefMap:  m.refMap,
		XRD:     m.xrd,
	}
}

// Store tracks module state for multiple directories.
type Store struct {
	l       sync.RWMutex
	modules map[string]*module
}

// New creates a new store.
func New() *Store {
	return &Store{
		modules: map[string]*module{},
	}
}

// ListDirs returns all known module directories.
func (s *Store) ListDirs() []string {
	s.l.RLock()
	defer s.l.RUnlock()
	var ret []string
	for k := range s.modules {
		ret = append(ret, k)
	}
	return ret
}

// Exists returns true if the module store is currently tracking the supplied directory.
func (s *Store) Exists(dir string) bool {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.modules[dir] != nil
}

// Remove removes stored information for the supplied directory if such information exists.
func (s *Store) Remove(dir string) {
	s.l.Lock()
	defer s.l.Unlock()
	delete(s.modules, dir)
}

// Get returns module information for the supplied directory or nil if no such information exists.
func (s *Store) Get(dir string) *Content {
	s.l.RLock()
	defer s.l.RUnlock()
	m := s.modules[dir]
	if m == nil {
		return nil
	}
	return m.Content()
}

// Put adds or updates information stored for the supplied module.
func (s *Store) Put(content *Content) {
	s.l.Lock()
	defer s.l.Unlock()
	s.modules[content.Path] = content.toModule()
}
