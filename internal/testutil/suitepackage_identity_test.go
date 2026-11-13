package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

// The regression guard for the lock's IDENTITY (suitepackage_identity.go): two
// PACKAGES that share one suite LABEL must never take the same lock, and two runs of
// ONE package must.
//
// The defect it pins, measured 2026-11-08 on this repository: `go test -run '^$'
// ./...` failed with
//
//	FATAL: suite "stylegate": another run of this package is in flight.
//	FAIL	github.com/nick/gopi/internal/stylegate/cmd/stylegate
//
// — no second RUN existed; internal/stylegate and internal/stylegate/cmd/stylegate
// both declare the label "stylegate", the key WAS the label, and `go test` starts
// packages in parallel, so the second refused itself with the exit code that fails
// the whole invocation.
func TestSuitePackageLockIsKeyedOnThePackageNotTheLabel(t *testing.T) {
	const label = "stylegate"
	outer := "github.com/nick/gopi/internal/stylegate"
	inner := "github.com/nick/gopi/internal/stylegate/cmd/stylegate"

	gotOuter, gotInner := suitePackageLockKeyFor(outer, label), suitePackageLockKeyFor(inner, label)
	if gotOuter == gotInner {
		t.Fatalf("two different packages that share the label %q were keyed on ONE identity (%q) — the parallel "+
			"`go test` of ./... then makes one refuse the other with exit %d, which is the defect this guards",
			label, gotOuter, SuiteBusyExitCode)
	}
	// The same package twice must be the SAME identity, or the exclusion stops
	// excluding anything at all.
	if again := suitePackageLockKeyFor(outer, label); again != gotOuter {
		t.Fatalf("the same package was keyed on two different identities: %q then %q", gotOuter, again)
	}
	// The label must not decide the address: a package keeps its identity when the
	// human-facing label changes (a label is a diagnosis, never an address).
	if other := suitePackageLockKeyFor(outer, "a-completely-different-label"); other != gotOuter {
		t.Fatalf("the lock identity moved with the LABEL (%q → %q); the package path must decide it", gotOuter, other)
	}
}

// The two ADDRESSES are what the kernel actually decides on, so the assertion is made
// on the directory a take would mkdir, not only on the key string: internal/scratch
// reduces a name to one path element, and that reduction maps "/" to "_" — which is
// why the key carries a digest instead of relying on the raw path.
func TestSuitePackageLockAddressesDifferForPackagesSharingALabel(t *testing.T) {
	home := t.TempDir()
	const label = "stylegate"
	a := scratchDirFor(t, home, "github.com/nick/gopi/internal/stylegate", label)
	b := scratchDirFor(t, home, "github.com/nick/gopi/internal/stylegate/cmd/stylegate", label)
	if a == b {
		t.Fatalf("the lock ADDRESS is the same for two packages sharing label %q: %s", label, a)
	}
	// The reduction's own trap, asserted directly: these two paths differ only in
	// where a separator sits, so a key built from the raw path would collide.
	u := suitePackageLockKeyFor("a/b", label)
	v := suitePackageLockKeyFor("a_b", label)
	if u == v {
		t.Fatalf(`paths differing only by "/" vs "_" were keyed identically (%q) — internal/scratch's element `+
			`reduction maps them onto ONE lock directory`, u)
	}
}

// TestNoTwoPackagesShareTheIdentityTheLockUses walks the REAL tree the way the suite
// entry points declare themselves and asserts the property the mechanism needs:
// wherever two packages name the same suite LABEL, their keys still differ.
//
// It is the arm that would have caught the defect where it was introduced: the label
// duplication is not a fixture, it is in the tree.
func TestNoTwoPackagesShareTheIdentityTheLockUses(t *testing.T) {
	root := repoRootForLockIdentity(t)
	byLabel := map[string]map[string][]string{} // label → key → packages
	scanned, labelled := 0, 0
	for _, pkg := range testPackagesDeclaringASuiteLabel(t, root) {
		scanned++
		labelled++
		key := suitePackageLockKeyFor(pkg.importPath, pkg.label)
		if byLabel[pkg.label] == nil {
			byLabel[pkg.label] = map[string][]string{}
		}
		byLabel[pkg.label][key] = append(byLabel[pkg.label][key], pkg.importPath)
	}
	if scanned < lockIdentityScanFloor {
		t.Fatalf("the lock-identity scan read only %d declaring package(s) (floor %d) — it is not looking where "+
			"the suites live, and a scan that found nothing cannot pass", scanned, lockIdentityScanFloor)
	}
	shared := 0
	for label, keys := range byLabel {
		for key, pkgs := range keys {
			if len(pkgs) < 2 {
				continue
			}
			shared++
			t.Errorf("label %q puts %d packages on ONE lock identity (%q): %s — a parallel `go test ./...` "+
				"refuses all but the first with exit %d",
				label, len(pkgs), key, strings.Join(pkgs, ", "), SuiteBusyExitCode)
		}
	}
	t.Logf("lock identity: %d declaring package(s) over %d distinct label(s), %d shared identity group(s)",
		scanned, len(byLabel), shared)
	if labelled == 0 {
		t.Fatalf("no suite label was read at all — the scan's shape no longer matches the tree")
	}
}

// modulePathOfThisRepo is the module path the scan anchors on. It is a literal here
// rather than read from go.mod: the file that FINDS go.mod cannot take its search key
// from the file it has not found yet.
const modulePathOfThisRepo = "module github.com/nick/gopi"

// lockIdentityScanFloor is how many declaring packages the scan must reach for its
// green to mean anything (a scan that read nothing must not pass — Rule 15).
const lockIdentityScanFloor = 20

// scratchDirFor asks the PRODUCTION address function for the directory a take would
// create for one package identity.
func scratchDirFor(t *testing.T, home, importPath, label string) string {
	t.Helper()
	return scratch.SuitePackageLockDir(home, suitePackageLockKeyFor(importPath, label))
}

// declaringPackage is one package that routes its TestMain through the suite entry
// point, with the label it passes.
type declaringPackage struct {
	dir        string
	importPath string
	label      string
}

// testPackagesDeclaringASuiteLabel reads every *_test.go under root and returns the
// packages whose TestMain names a suite label through the shared entry points.
//
// The label is taken from the call itself (RunSuiteMain(m, "…") /
// PinSuiteForTestMain("…")), because that literal IS the declaration the lock used to
// be keyed on; the import path comes from the package's directory under the module
// root, which is the identity the go tool builds.
func testPackagesDeclaringASuiteLabel(t *testing.T, root string) []declaringPackage {
	t.Helper()
	modulePath := readModulePath(t, root)
	seen := map[string]bool{}
	var out []declaringPackage
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		label := suiteLabelIn(string(body))
		if label == "" {
			return nil
		}
		dir := filepath.Dir(path)
		if seen[dir] {
			return nil
		}
		seen[dir] = true
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil {
			t.Fatalf("relativise %s: %v", dir, relErr)
		}
		out = append(out, declaringPackage{
			dir:        dir,
			importPath: modulePath + "/" + filepath.ToSlash(rel),
			label:      label,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// suiteLabelIn extracts the label from the first suite-entry-point call in src.
func suiteLabelIn(src string) string {
	for _, marker := range []string{"RunSuiteMain(m, ", "PinSuiteForTestMain("} {
		idx := strings.Index(src, marker)
		if idx < 0 {
			continue
		}
		rest := src[idx+len(marker):]
		open := strings.Index(rest, `"`)
		if open < 0 {
			continue
		}
		rest = rest[open+1:]
		end := strings.Index(rest, `"`)
		if end < 0 {
			continue
		}
		return rest[:end]
	}
	return ""
}

// readModulePath reads the module path from go.mod at root.
func readModulePath(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("go.mod at %s states no module path", root)
	return ""
}

// repoRootForLockIdentity is the module root this package's tests run in.
//
// It starts from the DIRECTORY OF THIS SOURCE FILE, not from os.Getwd(): internal/testutil's
// own entry point chdirs a suite into its scratch tree (measured here: getwd returned "/"),
// so the working directory is a suite-chosen location and carrying no relationship to the
// tree the scan must read.
func repoRootForLockIdentity(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate this test's own source file, so the module root cannot be found")
	}
	dir := filepath.Dir(file)
	for {
		if raw, readErr := os.ReadFile(filepath.Join(dir, "go.mod")); readErr == nil &&
			strings.Contains(string(raw), modulePathOfThisRepo) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod declaring %s above %s", modulePathOfThisRepo, filepath.Dir(file))
		}
		dir = parent
	}
}
