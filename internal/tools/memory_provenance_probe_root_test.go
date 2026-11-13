package tools

// Regression suite for the LATENT PROBE ROOTS
// (internal/tools/memory_provenance_latent.go).
//
// The defect this file pins (found by the 2026-09-27 statistics audit): the
// four reporting probe roots were compiled-in absolute paths
// (/home/nick/work/GoPi + three hermes trees), so "does this harvested path
// resolve?" was really asking "is this machine the operator's box?". The roots
// feed REPORTING only — no classification decision reads the filesystem, a
// property the file's own comment states and these tests preserve.
//
// The contract under test:
//
//	① the probe root is DERIVED (this process's own repository) when nothing is
//	   declared — a path that exists in this checkout resolves;
//	② the operator can DECLARE extra trees (path-separator joined), which is
//	   how the imported hermes_* rows' artifacts stay resolvable on a box that
//	   has them and are honestly unresolvable on one that does not;
//	③ a path that exists nowhere is unresolved — the probe never reports a
//	   resolution it did not make.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLatentProbeRootDerivesFromThisRepository pins rung ①: with no declaration,
// a repo-relative path that really exists here must resolve. The old literal
// passed this test on exactly one machine.
func TestLatentProbeRootDerivesFromThisRepository(t *testing.T) {
	t.Setenv(latentColumnProbeRootEnv, "")
	t.Setenv(latentRepoRootsEnv, "")
	if got := latentProbeRoot(); got == "" {
		t.Fatal("probe root is empty with nothing declared — rung ① must derive it from this process's repository")
	}
	if !latentPathResolves("go.mod") {
		t.Fatalf("go.mod does not resolve under the derived probe root %q — derivation is broken", latentProbeRoot())
	}
	if latentPathResolves("no/such/artifact-" + "0123456789abcdef.go") {
		t.Fatalf("an invented path resolved under %q — the probe must never report a resolution it did not make", latentProbeRoot())
	}
}

// TestLatentProbeRootsHonourDeclaredTrees pins rung ②: a declared tree is
// searched, and a declared tree that does not exist simply contributes nothing
// (no error, no fabricated hit).
func TestLatentProbeRootsHonourDeclaredTrees(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "probe")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "artifact.go"), []byte("package probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(latentColumnProbeRootEnv, root)
	t.Setenv(latentRepoRootsEnv, "")

	if !latentPathResolves("probe/artifact.go") {
		t.Fatalf("declared probe root %q did not resolve probe/artifact.go", root)
	}
	if latentPathResolves("probe/absent.go") {
		t.Fatal("a file that exists nowhere must not resolve")
	}

	// The EXTRA-roots channel is what a deployment uses for rows whose
	// artifacts live in a tree this process is not standing in: with the probe
	// root pointed elsewhere, the extra root is the only place the file can be
	// found.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "elsewhere.go"), []byte("package elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(latentRepoRootsEnv, other)
	if !latentPathResolves("elsewhere.go") {
		t.Fatalf("declared extra root %q was not searched (roots=%v)", other, latentRepoRoots())
	}
	// An empty entry in the list is skipped rather than resolving to the
	// process's cwd — a bare separator must not widen the search root.
	t.Setenv(latentRepoRootsEnv, string(os.PathListSeparator)+other+string(os.PathListSeparator))
	if roots := latentRepoRoots(); len(roots) != 2 {
		t.Fatalf("blank list entries must be dropped: got %d roots %v", len(roots), roots)
	}
	if !latentPathResolves("elsewhere.go") {
		t.Fatal("a padded list must still resolve the declared tree")
	}
}

// TestLatentProbeRootNeverAnOperatorLiteral is the anti-regression half: the
// probe root must not be the operator's checkout spelling again. It is checked
// on the SOURCE text rather than on a value, because the failure mode is a
// literal being reinstated — which a value check on the operator's own box
// cannot distinguish from a correct derivation.
func TestLatentProbeRootNeverAnOperatorLiteral(t *testing.T) {
	body, err := os.ReadFile("memory_provenance_latent.go")
	if err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue // prose recording the incident is evidence, not a defect
		}
		if home != "" && strings.Contains(trimmed, home) {
			t.Fatalf("memory_provenance_latent.go:%d carries a home-directory literal in CODE: %s — the probe roots are derived + declared, never compiled in", i+1, trimmed)
		}
	}
}
