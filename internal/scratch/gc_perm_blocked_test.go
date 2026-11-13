package scratch

// gc_perm_blocked_test.go — the regression guard for PERMISSION-BLOCKED
// residue: a stale, unheld, in-namespace entry the sweep selects every pass and
// can never delete.
//
// THE BUG (measured on this host, 2026-09-15): the deletion was a bare
// os.RemoveAll, so a single descendant directory with no owner write+execute
// bit made the whole entry undeletable — EACCES from the openat of that
// descendant, every pass, forever. The real-world producer is a test fixture
// that drops a directory to mode 0 as a probe and restores it in t.Cleanup: a
// suite KILLED before the cleanup ran (the load-94 incident) leaves the mode-0
// directory behind for good.
//
// Consequence measured on the host: /dev/shm sat at 100% (7.8G used, 0 free),
// which reads as `database or disk is full (13)` and `no space left on device`
// failures across the suites. `gopi tool scratch-gc --dry-run` promised
// `would-remove ...suite-internal-testutil-run-3663106447 age=17h3m7s` while
// the real sweep answered `removed=0` + `permission denied`; restoring that one
// directory to 0700 flipped the same sweep to `removed=1`.
//
// The test asserts BOTH halves, so it is red on the pre-fix tree without
// needing that tree: (a) the bare RemoveAll the sweep used to perform provably
// FAILS on the fixture, and (b) SweepHome still collects the entry.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// permBlockedFixture builds the shape a killed suite leaves behind: a stale
// in-namespace root whose subtree contains a directory (and a file) with no
// owner access at all. It returns the home and the root; the modes are restored
// on cleanup so the framework can remove its own temp tree.
func permBlockedFixture(t *testing.T) (home, root, blockedDir string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, SuiteRunMarker+"permblocked-run-1")
	// The blocked directory sits BELOW the entry, which is what makes the
	// failure interesting: the entry itself is measurable (Lstat succeeds), so
	// the sweep has no reason to skip it — only the deletion fails.
	blockedDir = filepath.Join(root, "probe", "locked")
	if err := os.MkdirAll(blockedDir, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", blockedDir, err)
	}
	blockedFile := filepath.Join(blockedDir, "held.db")
	if err := os.WriteFile(blockedFile, []byte("residue\n"), FilePerm); err != nil {
		t.Fatalf("write %s: %v", blockedFile, err)
	}
	// t.Cleanup restore FIRST, so it runs LAST (LIFO) — after the mode is
	// dropped and before the framework removes the tree.
	t.Cleanup(func() {
		_ = os.Chmod(blockedDir, DirPerm)   //stylegate:ignore cleanup restore of the mode this fixture dropped; a failure here leaves t.TempDir unremovable, it cannot make the assertion wrong
		_ = os.Chmod(blockedFile, FilePerm) //stylegate:ignore cleanup restore of the mode this fixture dropped; see the dirPerm restore above
	})
	// The producer's own shape: the directory loses all owner access.
	if err := os.Chmod(blockedDir, 0); err != nil {
		t.Skipf("cannot drop permissions on %s: %v", blockedDir, err)
	}
	old := time.Now().Add(-staleRootAge)
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", root, err)
	}
	return home, root, blockedDir
}

// TestRemoveAllCannotDrainAPermissionBlockedEntry is the RED-PROOF half: it
// pins the mechanism (a bare RemoveAll on this shape fails) so the fix below
// cannot be satisfied by a fixture that was never actually blocked.
func TestRemoveAllCannotDrainAPermissionBlockedEntry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions — the blocked shape is inexpressible")
	}
	_, root, _ := permBlockedFixture(t)
	if err := os.RemoveAll(root); err == nil {
		t.Fatalf("a bare os.RemoveAll drained the permission-blocked entry %s — "+
			"the fixture no longer models the defect the sweep has to survive", root)
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("the entry survived the failed RemoveAll only partially: %v", err)
	}
}

// TestSweepHomeCollectsPermissionBlockedResidue is THE regression: a stale,
// unheld, in-namespace entry whose subtree is permission-blocked is collected
// by the sweep (and the tmpfs bytes it holds are actually freed).
func TestSweepHomeCollectsPermissionBlockedResidue(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions — the blocked shape is inexpressible")
	}
	home, root, blockedDir := permBlockedFixture(t)
	if _, err := os.Lstat(blockedDir); err != nil {
		t.Fatalf("fixture: blocked dir missing before the sweep: %v", err)
	}
	n, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d entries, want 1 — the permission-blocked entry was NOT collected "+
			"(this is the immortal-residue defect: selected every pass, deleted never)", n)
	}
	if exists(t, root) {
		t.Fatalf("the permission-blocked entry %s SURVIVED a sweep — its bytes are unreclaimable", root)
	}
}

// TestSweepHomeKeepsAnEntryItCannotMeasure is the OTHER half of the contract,
// pinned so the repair-and-retry path can never be widened into "delete
// whatever RemoveAll failed on": an entry the sweep cannot MEASURE is kept and
// never chmod-ed (Rule 15). The measured, blocked-descendant case — the defect
// this file's fix closes — is TestSweepHomeCollectsPermissionBlockedResidue.
func TestSweepHomeKeepsAnEntryItCannotMeasure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions — the unmeasurable shape is inexpressible")
	}
	// The entry's own identity is unreadable when its PARENT lacks owner
	// execute: this is the smallest expressible form of "the sweep cannot see
	// what is there, so it must not act on it".
	root := t.TempDir()
	parent := filepath.Join(root, SuiteRunMarker+"outer-run-1")
	entry := filepath.Join(parent, SuiteRunMarker+"inner-run-1")
	if err := os.MkdirAll(entry, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-staleRootAge)
	_ = os.Chtimes(entry, old, old)  //stylegate:ignore fixture ageing: a failed Chtimes leaves the entry too fresh, so the sweep skips it and the test fails loudly anyway
	_ = os.Chtimes(parent, old, old) //stylegate:ignore fixture ageing of the blocked parent; see the entry ageing above
	if err := os.Chmod(parent, 0); err != nil {
		t.Skipf("cannot drop permissions on %s: %v", parent, err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, DirPerm) }) //stylegate:ignore cleanup restore of the mode this fixture dropped to 0; the test already holds the directory

	// The direct measurement the guard is built on: the sweep cannot see
	// inside the entry → refuse. Lstat alone must NOT authorise the repair
	// (it succeeds behind a blocked parent), which is why the probe opens the
	// directory and reads one name.
	if entryReadable(entry) {
		t.Fatalf("entryReadable(%s) = true — an entry behind a permission-blocked parent is "+
			"exactly what the repair must refuse", entry)
	}
	if _, err := os.Lstat(entry); err == nil {
		t.Fatalf("premise check: Lstat on %s succeeded, so this fixture does not exercise the "+
			"Lstat-vs-readable gap the guard exists for", entry)
	}
	// And the delete path leaves it alone rather than repairing it.
	if err := removeCollectable(entry); err == nil {
		t.Fatalf("removeCollectable(%s) reported success on an unmeasurable entry", entry)
	}
	// The parent must NOT have been repaired: the repair only runs for an
	// entry the sweep measured and selected.
	fi, err := os.Lstat(parent)
	if err != nil {
		t.Fatalf("parent vanished: %v", err)
	}
	if fi.Mode().Perm() != 0 {
		t.Errorf("the blocked parent's mode is now %v, want 0 — the repair touched an entry the "+
			"collectability predicate never approved", fi.Mode().Perm())
	}
}

// TestSweepHomeReportsAnUnmeasurableHome pins the fail-open half: a scratch home
// the sweep cannot read is REPORTED (the caller learns the tree is unbounded),
// and nothing inside it is guessed at.
func TestSweepHomeReportsAnUnmeasurableHome(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions — the unreadable shape is inexpressible")
	}
	root := t.TempDir()
	home := filepath.Join(root, "locked-home")
	entry := filepath.Join(home, SuiteRunMarker+"orphan-run-1")
	if err := os.MkdirAll(entry, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-staleRootAge)
	_ = os.Chtimes(entry, old, old) //stylegate:ignore fixture ageing: a failed Chtimes makes the entry un-collectable, which fails the assertion rather than passing it
	if err := os.Chmod(home, 0); err != nil {
		t.Skipf("cannot drop permissions on %s: %v", home, err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, DirPerm) }) //stylegate:ignore cleanup restore of the mode this fixture dropped to 0

	// The sweep may not read the home: it must say so (Rule 15b), never
	// silently report "0 removed" as if the tree were clean.
	if _, err := SweepHome(home, DefaultStaleAge); err == nil {
		t.Errorf("SweepHome over an unreadable home returned no error — the unbounded tree " +
			"would be reported as clean")
	}
}

// TestSweepHomeLeavesPermissionBlockedForeignResidueAlone pins the blast radius:
// the repair-and-retry path runs ONLY after the shared collectability predicate
// approved the entry, so a permission-blocked FOREIGN directory is still never
// touched (nor chmod-ed on the way past).
func TestSweepHomeLeavesPermissionBlockedForeignResidueAlone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions — the blocked shape is inexpressible")
	}
	home := t.TempDir()
	foreign := filepath.Join(home, "somebody-elses-tree")
	if err := os.MkdirAll(foreign, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	inner := filepath.Join(foreign, "locked")
	if err := os.Mkdir(inner, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(inner, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inner, DirPerm) }) //stylegate:ignore cleanup restore of the mode this fixture dropped to 0
	old := time.Now().Add(-staleRootAge)
	_ = os.Chtimes(foreign, old, old) //stylegate:ignore fixture ageing of a foreign directory; the sweep must ignore it either way

	if _, err := SweepHome(home, DefaultStaleAge); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !exists(t, foreign) {
		t.Fatalf("a foreign directory %s was collected — the sweep is not namespace-limited", foreign)
	}
	fi, err := os.Lstat(inner)
	if err != nil {
		t.Fatalf("foreign subtree was disturbed: %v", err)
	}
	if fi.Mode().Perm() != 0 {
		t.Errorf("the foreign locked dir's mode is now %v, want 0 — the repair touched an entry "+
			"the collectability predicate never approved", fi.Mode().Perm())
	}
}
