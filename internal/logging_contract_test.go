package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLayersUseInjectedLoggerInsteadOfPackageLevelLogging(t *testing.T) {
	forbiddenSlogCalls := map[string]struct{}{
		"Debug": {}, "DebugContext": {},
		"Default": {}, "Enabled": {},
		"Error": {}, "ErrorContext": {},
		"Info": {}, "InfoContext": {},
		"Log": {}, "LogAttrs": {},
		"New": {}, "NewJSONHandler": {}, "NewTextHandler": {},
		"SetDefault": {}, "SetLogLoggerLevel": {},
		"Warn": {}, "WarnContext": {},
		"With": {}, "WithGroup": {},
	}

	for _, root := range []string{"domain", "infra", "application"} {
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

			files := token.NewFileSet()
			parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			slogAliases := make(map[string]struct{})
			for _, imported := range parsed.Imports {
				importPath, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if importPath == "log" {
					t.Errorf("%s imports standard log; inject *slog.Logger instead", path)
				}
				if importPath != "log/slog" {
					continue
				}
				alias := "slog"
				if imported.Name != nil {
					alias = imported.Name.Name
				}
				if alias == "." {
					t.Errorf("%s dot-imports log/slog; inject *slog.Logger instead", path)
					continue
				}
				if alias != "_" {
					slogAliases[alias] = struct{}{}
				}
			}
			if len(slogAliases) == 0 {
				return nil
			}

			parsed, err = parser.ParseFile(files, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				if _, ok := slogAliases[identifier.Name]; !ok {
					return true
				}
				if _, forbidden := forbiddenSlogCalls[selector.Sel.Name]; forbidden {
					position := files.Position(call.Pos())
					t.Errorf("%s:%d calls package-level slog.%s; use the injected logger instance", path, position.Line, selector.Sel.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s logging contract: %v", root, err)
		}
	}
}
