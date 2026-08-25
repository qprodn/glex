package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// importViolation scans every non-test Go file under root (relative to the
// repository's internal/ directory) and reports imports matching any
// forbidden prefix.
func importViolation(t *testing.T, root string, forbiddenPrefixes []string, skipTestFiles bool) {
	t.Helper()
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
		if !strings.HasSuffix(path, ".go") || (skipTestFiles && strings.HasSuffix(path, "_test.go")) {
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
		importViolation(t, root, forbiddenPrefixes, true)
	}
}

// TestDomainPackagesAreIslands pins ADR 0016's package-level boundaries: a
// domain package never imports another domain package. Modules interact only
// through ports, snapshots, and the crossdomain layer.
func TestDomainPackagesAreIslands(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(osDir(), "domain"))
	if err != nil {
		t.Fatalf("list domain packages: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		siblings := make([]string, 0, len(entries))
		for _, other := range entries {
			if other.IsDir() && other.Name() != entry.Name() {
				siblings = append(siblings, "glex/internal/domain/"+other.Name())
			}
		}
		importViolation(t, filepath.Join("domain", entry.Name()), siblings, true)
	}
}

// TestModuleSplitBoundaries pins the iam/authn split of ADR 0016: the authn
// application module is reachable from iam only through the crossdomain
// contracts and the Principal context, and authn itself never reaches back
// into iam.
func TestModuleSplitBoundaries(t *testing.T) {
	importViolation(t, filepath.Join("application", "authn"), []string{
		"glex/internal/domain/iam",
		"glex/internal/application/iam",
		"glex/internal/infra/iam",
	}, true)
	importViolation(t, filepath.Join("application", "iam"), []string{
		"glex/internal/application/authn",
		"glex/internal/infra/authn",
	}, true)
}

// osDir returns this file's directory (internal/).
func osDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "internal"
	}
	return filepath.Dir(filename)
}
