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

func TestLayerImportsFollowDDDDependencyDirection(t *testing.T) {
	rules := map[string][]string{
		"domain": {
			"glex/api/",
			"glex/internal/application",
			"glex/internal/infra",
			"glex/internal/server",
		},
		"application": {
			"glex/internal/infra",
			"glex/internal/server",
		},
		"infra": {
			"glex/api/",
			"glex/internal/application",
			"glex/internal/server",
		},
		"server": {
			"glex/internal/infra",
		},
	}

	for root, forbiddenPrefixes := range rules {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path == filepath.Join("infra", "ent") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range parsed.Imports {
				importPath, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				for _, prefix := range forbiddenPrefixes {
					if importPath == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(importPath, prefix) {
						t.Errorf("%s imports forbidden layer %q", path, importPath)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s layering contract: %v", root, err)
		}
	}
}
