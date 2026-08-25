package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The crossdomain anti-corruption layer (ADR 0017) has three structural
// invariants this contract pins down:
//
//  1. Domain packages never import crossdomain: cross-module calls happen in
//     Application Usecases, not inside a provider's or consumer's domain.
//  2. Outside a provider's impl/ adapter, a crossdomain package never imports
//     the provider's domain: contracts expose only model/ snapshots, so
//     consumers cannot reach provider domain types at all.
//  3. Application consumes crossdomain contracts only, never their impl/
//     adapters: impl is wired exclusively by the cmd composition root.
func TestCrossdomainAntiCorruptionBoundaries(t *testing.T) {
	rules := []struct {
		name      string
		root      string
		forbidden func(importPath, filePath string) bool
	}{
		{
			name: "domain must not import crossdomain",
			root: "domain",
			forbidden: func(importPath, _ string) bool {
				return importPath == "glex/internal/crossdomain" || strings.HasPrefix(importPath, "glex/internal/crossdomain/")
			},
		},
		{
			name: "crossdomain outside impl must not import a domain",
			root: "crossdomain",
			forbidden: func(importPath, filePath string) bool {
				if !strings.HasPrefix(importPath, "glex/internal/domain") {
					return false
				}
				return !strings.Contains(filePath, string(filepath.Separator)+"impl"+string(filepath.Separator))
			},
		},
		{
			name: "application must not import a crossdomain impl adapter",
			root: "application",
			forbidden: func(importPath, _ string) bool {
				if !strings.HasPrefix(importPath, "glex/internal/crossdomain/") {
					return false
				}
				rest := strings.TrimPrefix(importPath, "glex/internal/crossdomain/")
				return rest == "impl" || strings.HasPrefix(rest, "impl/") || strings.HasSuffix(rest, "/impl") || strings.Contains(rest, "/impl/")
			},
		},
	}

	for _, rule := range rules {
		err := filepath.WalkDir(rule.root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			files := token.NewFileSet()
			parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range parsed.Imports {
				importPath, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if rule.forbidden(importPath, path) {
					t.Errorf("%s: %s imports %s", rule.name, path, importPath)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", rule.root, err)
		}
	}
}
