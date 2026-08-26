package internal_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// importViolation scans every Go file under root (relative to the repository's
// internal/ directory) and reports imports matching any forbidden prefix.
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
			"glex/internal/crossdomain",
			"glex/internal/infra",
			"glex/internal/server",
		},
		"application": {
			"glex/internal/infra",
			"glex/internal/server",
		},
		"crossdomain": {
			"glex/api/",
			"glex/internal/application",
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
// domain package never imports another domain package, and (per the layer
// rules above) nothing new at all — modules interact only through ports,
// snapshots, and the crossdomain layer.
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

// TestModuleSplitBoundaries pins the module split of ADR 0016 in both the
// application and the infra layer: the user, access, and authn modules reach
// each other only through the crossdomain contracts and the Principal
// context, never through another module's domain, application, infra, or
// (for infra adapters) crossdomain packages.
//
// One sanctioned exception at import granularity: the application modules may
// import domain/authn for the Principal identity carrier (ADR 0016 —
// authenticated identity crosses the seam as domain/authn.Principal; GetInfo
// and GetRouters read the snapshot, adding no cross-module port).
func TestModuleSplitBoundaries(t *testing.T) {
	for _, module := range []string{"user", "access", "authn"} {
		var appForbidden, infraForbidden []string
		for _, other := range []string{"user", "access", "authn"} {
			if other == module {
				continue
			}
			appForbidden = append(appForbidden,
				fmt.Sprintf("glex/internal/domain/%s", other),
				fmt.Sprintf("glex/internal/application/%s", other),
				fmt.Sprintf("glex/internal/infra/%s", other))
			// Infra adapters never make cross-module calls: another module's
			// crossdomain contracts are consumed by Application Usecases.
			infraForbidden = append(infraForbidden,
				fmt.Sprintf("glex/internal/domain/%s", other),
				fmt.Sprintf("glex/internal/application/%s", other),
				fmt.Sprintf("glex/internal/infra/%s", other),
				fmt.Sprintf("glex/internal/crossdomain/%s", other))
		}
		if module != "authn" {
			// domain/authn stays importable by the user and access
			// application modules for the Principal carrier only.
			appForbidden = slices.DeleteFunc(appForbidden, func(p string) bool {
				return p == "glex/internal/domain/authn"
			})
		}
		importViolation(t, filepath.Join("application", module), appForbidden, true)
		// The infra adapters' tests are module-clean, so they are pinned too.
		importViolation(t, filepath.Join("infra", module), infraForbidden, false)
	}
}

// osDir returns this file's directory (internal/).
func osDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "internal"
	}
	return filepath.Dir(filename)
}
