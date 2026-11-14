package main

// task_session_cwd_freshness_scope_test.go — the regression guard for the
// freshness guard.
//
// MEASURED DEFECT (master 2026-09-16, the 12:59:33 `make test` stamp):
// TestBareRunKeepsAllItsArtifactsInOneSessionDir failed with
//
//	/home/user/work/GoPi/bin/gopi (mtime 2026-09-16T12:59:01+08:00) is OLDER
//	than /home/user/work/GoPi/internal/testutil/legacy_residue.go (mtime
//	2026-09-16T12:59:35+08:00) — this test drives the BUILT binary, so it would
//	verify code that is not in that binary. Rebuild it … and re-run.
//
// and the very next run SKIPPED the same test ("gopi binary not found") — so
// the epic's one cmd/agent red never executed its layout walk at all. The
// binary was NOT stale: `go list -deps ./cmd/agent/` links ZERO package under
// internal/testutil, and internal/testutil/legacy_residue.go is a production
// .go file inside a TEST-ONLY package, which the walk's _test.go filter cannot
// see. The failure came from the guard measuring the wrong tree, i.e. a stale
// SCOPE, not a stale binary.
//
// These tests pin both halves of the fix: a test-only tree must NOT be able to
// make a fresh binary look stale, and a genuinely newer production file must
// still make it look stale (the guard is what makes every e2e green
// falsifiable — it may not be weakened into silence).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// freshnessBinRel is the built artifact's path inside a fixture tree, named
// from the resolver's own constants (Rule 1/14) so the fixture and the
// production walk agree on what they are talking about.
const freshnessBinRel = testutil.BinDirName + "/" + testutil.BinFileName

// freshnessFixture lays out the smallest tree the walk actually reads. Each
// mtime is an offset from now (negative = older), so a caller reproduces a
// precise geometry:
//
//	<root>/cmd/agent/main.go                     production
//	<root>/internal/agent/probe.go               production
//	<root>/internal/testutil/legacy_residue.go   TEST-ONLY
//	<root>/bin/gopi                              the built artifact
func freshnessFixture(t *testing.T, cmdAge, internalAge, testOnlyAge, binAge time.Duration) string {
	t.Helper()
	root := t.TempDir()
	now := time.Now()

	write := func(rel string, mtime time.Time) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", rel, err)
		}
	}

	write("cmd/agent/main.go", now.Add(cmdAge))
	write("internal/agent/probe.go", now.Add(internalAge))
	write(filepath.Join("internal", "testutil", "legacy_residue.go"), now.Add(testOnlyAge))
	write(freshnessBinRel, now.Add(binAge))

	return root
}

// TestFreshnessWalkIgnoresTestOnlyTrees reproduces the incident's geometry: the
// TEST-ONLY file is the newest source in the tree, the binary sits between it
// and the production sources. Before the fix the walk returned the test-only
// file, so a just-built binary was declared stale and the e2e refused to run;
// after the fix the newest source the walk reports must not be newer than the
// binary.
func TestFreshnessWalkIgnoresTestOnlyTrees(t *testing.T) {
	// test-only -1s (the newest thing in the tree, as at 12:59:35), bin -2s
	// (the build at 12:59:01), production -4s/-6s.
	root := freshnessFixture(t, -4*time.Second, -6*time.Second, -1*time.Second, -2*time.Second)

	newest, newestPath, err := cwdProbeNewestProductionSource(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	binInfo, err := os.Stat(filepath.Join(root, freshnessBinRel))
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}
	if newest.After(binInfo.ModTime()) {
		t.Fatalf("the walk reports %s (mtime %s) as newer than the binary (mtime %s) — a TEST-ONLY tree made a fresh binary look stale, which is exactly the 2026-09-16 false alarm (TestBareRunKeepsAllItsArtifactsInOneSessionDir: %s is OLDER than …/internal/testutil/legacy_residue.go)",
			newestPath, newest.Format(time.RFC3339), binInfo.ModTime().Format(time.RFC3339), filepath.Join(root, freshnessBinRel))
	}
	rel, relErr := filepath.Rel(root, newestPath)
	if relErr != nil {
		t.Fatalf("rel %s: %v", newestPath, relErr)
	}
	if rel != filepath.Join("cmd", "agent", "main.go") {
		t.Errorf("newest production source = %q; want cmd/agent/main.go (the newest file the binary actually compiles)", rel)
	}
}

// TestFreshnessWalkStillSeesANewerProductionFile is the other half: the guard
// keeps firing when a file the binary DOES compile is newer — the fix narrowed
// the scope, it did not disarm the verdict.
func TestFreshnessWalkStillSeesANewerProductionFile(t *testing.T) {
	// production -1s (edited after the build), bin -4s, test-only -5s.
	root := freshnessFixture(t, -1*time.Second, -3*time.Second, -5*time.Second, -4*time.Second)

	newest, newestPath, err := cwdProbeNewestProductionSource(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	binInfo, err := os.Stat(filepath.Join(root, freshnessBinRel))
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}
	if filepath.Base(newestPath) != "main.go" {
		t.Errorf("newest production source = %q; want cmd/agent/main.go (the newest file the binary compiles)", newestPath)
	}
	if !newest.After(binInfo.ModTime()) {
		t.Errorf("newest production source (%s) is NOT after the binary (%s) — the guard would miss a genuinely stale binary", newest.Format(time.RFC3339), binInfo.ModTime().Format(time.RFC3339))
	}
}

// TestFreshnessScopeSkipsWholeTestOnlyTrees pins the predicate itself, both
// directions, so a future edit cannot quietly turn the directory skip into a
// no-op (which would restore the false alarm) or widen it to a parent
// directory (which would hide a genuinely stale binary).
func TestFreshnessScopeSkipsWholeTestOnlyTrees(t *testing.T) {
	root := "/repo"
	if len(cwdProbeBinaryScopeSkippedDir) == 0 {
		t.Fatal("cwdProbeBinaryScopeSkippedDir is empty — the false alarm this test guards would return")
	}
	for _, rel := range cwdProbeBinaryScopeSkippedDir {
		if !cwdProbeBinaryScopeSkipped(root, filepath.Join(root, rel)) {
			t.Errorf("cwdProbeBinaryScopeSkipped(%q) = false; want true — the test-only tree must be skipped", rel)
		}
		// A parent of an excluded tree is NOT excluded: skipping it would hide
		// production packages that live beside it.
		parent := filepath.Dir(rel)
		if parent != "." && cwdProbeBinaryScopeSkipped(root, filepath.Join(root, parent)) {
			t.Errorf("cwdProbeBinaryScopeSkipped(%q) = true; want false — only the test-only tree itself may be skipped", parent)
		}
	}
	// A production tree is never skipped.
	if cwdProbeBinaryScopeSkipped(root, filepath.Join(root, "internal", "agent")) {
		t.Error("cwdProbeBinaryScopeSkipped(internal/agent) = true; want false — production sources must be walked")
	}
}
