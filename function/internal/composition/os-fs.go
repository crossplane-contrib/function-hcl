package composition

import (
	"io/fs"
	"os"
)

type osFs struct{}

func (o osFs) Open(name string) (fs.File, error)          { return os.Open(name) }
func (o osFs) Stat(name string) (fs.FileInfo, error)      { return os.Stat(name) }
func (o osFs) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
func (o osFs) ReadFile(name string) ([]byte, error)       { return os.ReadFile(name) }
