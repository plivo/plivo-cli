//go:build internal

// Gated behind `internal`: the boundary test names the internal-only
// domain suffixes it guards against. Only Plivo engineers build with
// `-tags internal`, and only they can introduce internal-host URL
// literals. Public contributors don't have those hosts to leak.

package api

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestNoDirectCXServiceURLs gates the public-build URL surface: any URL
// literal whose host ends in one of Plivo's internal-infrastructure
// domain suffixes must appear in allowedCXHosts. Stops the public binary
// from accidentally hard-coding hosts that should be resolved at runtime
// via env/config.
//
// URLs to api.plivo.com / lookup.plivo.com / third-party hosts are
// unconstrained — this isn't a general allowlist, just a guard on the
// internal-infrastructure hosts.
//
// Files behind `//go:build internal` are skipped: the internal binary
// reaches dev/staging hosts that don't ship in the public release.
func TestNoDirectCXServiceURLs(t *testing.T) {
	// Plivo CLI's customer-facing edge is hodor.plivo.com. No public-build
	// code should embed any *.contacto.com / *.contactodev.com literal —
	// those are internal-brand hosts. The allowlist is intentionally
	// empty; any addition needs review.
	allowedCXHosts := map[string]bool{}
	// Internal-infrastructure domain suffixes. Hosts suffix-matched here
	// are gated by allowedCXHosts. Dev-variant suffixes must never appear
	// in public code (internal-only files are skipped; public files must
	// not reference them).
	cxDomainSuffixes := []string{
		".contacto.com",
		".contactodev.com",
	}
	isCXHost := func(host string) bool {
		for _, suf := range cxDomainSuffixes {
			if strings.HasSuffix(host, suf) {
				return true
			}
		}
		return false
	}

	moduleRoot := findModuleRoot(t)
	fset := token.NewFileSet()
	// Plausible https:// URL match inside a Go string literal. We stop at
	// whitespace, quotes, backslashes, and closing brackets so the URL
	// doesn't bleed into surrounding code. url.Parse cleans up the rest.
	urlRE := regexp.MustCompile(`https?://[^\s"'\\)\]}` + "`" + `]+`)

	var offenders []string

	walkErr := filepath.WalkDir(moduleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			// Skip vendored / hidden / node deps; everything else is in scope.
			if base == "vendor" || base == "node_modules" || (strings.HasPrefix(base, ".") && base != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if requiresInternalBuildTag(path) {
			return nil
		}

		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, qerr := strconv.Unquote(lit.Value)
			if qerr != nil {
				return true
			}
			for _, m := range urlRE.FindAllString(s, -1) {
				u, uerr := url.Parse(m)
				if uerr != nil {
					continue
				}
				host := u.Hostname()
				if !isCXHost(host) {
					continue // non-CX host, unconstrained
				}
				if allowedCXHosts[host] {
					continue // allowed auth-server edge
				}
				pos := fset.Position(lit.Pos())
				rel, _ := filepath.Rel(moduleRoot, pos.Filename)
				offenders = append(offenders, fmt.Sprintf("%s:%d → %s (host %q not in allowlist)", rel, pos.Line, m, host))
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	if len(offenders) > 0 {
		t.Errorf("direct internal-host URL(s) found (must go via the runtime-resolved auth-server edge — see api.Client.BuddyURL):\n  %s\n\n"+
			"If this is a new auth-server edge, add it to allowedCXHosts in %s.",
			strings.Join(offenders, "\n  "),
			"internal/api/url_boundary_test.go")
	}
}

// findModuleRoot walks up from this test file until it finds the go.mod.
// We need the module root so the walk picks up every package, not just
// internal/api.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", file)
		}
		dir = parent
	}
}

// requiresInternalBuildTag reports whether the file's build constraint
// requires `internal` to compile (i.e. it's absent from the public build).
// For tags we don't know about (GOOS, GOARCH, ...) we assume they're set,
// so a constraint like "linux && internal" reports true but plain "linux"
// reports false.
func requiresInternalBuildTag(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	publicEval := func(tag string) bool { return tag != "internal" }
	for i := 0; i < 30 && sc.Scan(); i++ {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			return false
		}
		if !constraint.IsGoBuild(line) && !constraint.IsPlusBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			continue
		}
		// Would this file compile in the public build (no `internal` tag)?
		if !expr.Eval(publicEval) {
			return true
		}
	}
	_ = sc.Err() // scan errors here just mean we couldn't read the build header; treat as public
	return false
}
