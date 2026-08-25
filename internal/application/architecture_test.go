package application_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestApplicationDoesNotImportInfrastructure protects dependency inversion:
// Application Usecases may consume Domain ports but never concrete Infra
// adapters, Ent, Redis, or store clients.
func TestApplicationDoesNotImportInfrastructure(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve application package directory")
	}
	root := filepath.Dir(filename)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(importPath, "glex/internal/infra") {
				position := fileSet.Position(spec.Pos())
				t.Errorf("%s:%d: Application imports Infrastructure package %q", filepath.Base(path), position.Line, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Application packages: %v", err)
	}
}
