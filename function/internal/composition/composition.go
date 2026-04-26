package composition

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/crossplane-contrib/function-hcl/function/internal/evaluator"
	"github.com/ghodss/yaml"
	"github.com/hashicorp/hcl/v2"
	"github.com/pkg/errors"
	"golang.org/x/tools/txtar"
)

func doAnalyze(files []evaluator.File) error {
	logger := log.New(os.Stderr, "", 0)
	e, err := evaluator.New(evaluator.Options{})
	if err != nil {
		return err
	}
	diags := e.Analyze(files...)
	for _, diag := range diags {
		sev := "ERROR:"
		if diag.Severity == hcl.DiagWarning {
			sev = "WARN :"
		}
		logger.Println("\t", sev, diag.Error())
	}
	if diags.HasErrors() {
		return fmt.Errorf("analysis failed")
	}
	return nil
}

type loader struct {
	fs FS
}

func newLoader(fs FS) *loader {
	return &loader{fs: fs}
}

func (l *loader) load(dir string) (*Config, []string, error) {
	dir, err := l.checkDir(dir)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := l.loadConfig(dir)
	if err != nil {
		return nil, nil, err
	}
	fsFiles, err := l.fileList(dir, cfg)
	if err != nil {
		return nil, nil, err
	}
	return cfg, fsFiles, nil
}

func (l *loader) loadArchive(dir string) (*txtar.Archive, []evaluator.File, error) {
	_, fsFiles, err := l.load(dir)
	if err != nil {
		return nil, nil, err
	}
	var archive txtar.Archive
	var files []evaluator.File
	for _, file := range fsFiles {
		fileToRead := file
		if !filepath.IsAbs(file) {
			fileToRead = filepath.Join(dir, file)
		}
		// since the file list has file relative to the directory loaded
		// we need to make it relative to the working directory instead.
		contents, err := l.fs.ReadFile(fileToRead)
		if err != nil {
			return nil, nil, err
		}
		archive.Files = append(archive.Files, txtar.File{
			Name: file,
			Data: contents,
		})
		files = append(files, evaluator.File{
			Name:    file,
			Content: string(contents),
		})
	}
	return &archive, files, nil
}

func (l *loader) checkDir(dir string) (string, error) {
	st, err := l.fs.Stat(dir)
	if err != nil {
		return "", errors.Wrapf(err, "stat %s", dir)
	}
	if !st.IsDir() {
		return "", errors.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

func (l *loader) loadConfig(dir string) (*Config, error) {
	var cfg Config
	file := filepath.Join(dir, ConfigFile)
	st, err := l.fs.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, err
	}
	if st.IsDir() {
		return nil, errors.Errorf("%s is a directory", file)
	}
	b, err := l.fs.ReadFile(file)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(b, &cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "unmarshal contents of %s", file)
	}
	return &cfg, nil
}

func (l *loader) fileList(dir string, cfg *Config) ([]string, error) {
	var err error
	var files []string
	allFiles, err := l.fs.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range allFiles {
		if filepath.Ext(entry.Name()) != ".hcl" {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		s, err := l.fs.Stat(file)
		if err != nil {
			return nil, errors.Wrapf(err, "stat %s", file)
		}
		if s.IsDir() {
			continue
		}
		files = append(files, file)
	}

	for _, file := range cfg.LibraryFiles {
		if !filepath.IsAbs(file) {
			file = filepath.Clean(filepath.Join(dir, file))
		}
		s, err := l.fs.Stat(file)
		if err != nil {
			return nil, errors.Wrapf(err, "stat %s", file)
		}
		if s.IsDir() {
			return nil, errors.Errorf("library file %s cannot be a directory", file)
		}
		files = append(files, file)
	}

	var outFiles []string
	for _, file := range files {
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return nil, err
		}
		outFiles = append(outFiles, rel)
	}
	return outFiles, nil
}
