package scratch

// Tests for the memory-BASE residue sweep (base_residue.go). Two layers:
//
//   - the NAMESPACE table: the predicate is the whole safety argument of a
//     sweep over a world-writable shared directory, so the accepted and the
//     refused shapes are pinned name by name, before any filesystem question;
//   - the LIVE fixtures: a stale + unheld entry IS collected, a fresh one and a
//     LIVE-HELD one are UNTOUCHED, and a foreign name is never a candidate —
//     run against a real base (the holder rule is a property of /proc and the
//     filesystem, not of a function's return value).
//
// A fixture base is a plain directory (t.TempDir()) rather than a tmpfs: the
// sweep's own device gate (SharesRootFS) is a property of the REAL base and is
// exercised by its own test below, not by every fixture.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
)

// baseResidueStaleAge is comfortably past the sweep's threshold (the same
// spelling the home sweep's fixtures use).
const baseResidueStaleAge = DefaultStaleAge + time.Hour

// TestIsBaseResidueName_NamespaceTable pins the namespace rule: every ACCEPTED
// name is one a command in this repository (or a documented operator hand-point
// at the memory base) produces, and every REFUSED name is either foreign or a
// name a human chose. The refused half is the point — a prefix test alone would
// delete a neighbouring user's "gopi-…" directory.
func TestIsBaseResidueName_NamespaceTable(t *testing.T) {
	cases := []struct {
		name string
		want bool
		why  string
	}{
		// ── accepted: the measured residue's shapes ─────────────────────────
		{"gopi-verify-01a0a387", true, "a session's ad-hoc verify dir, task-id suffix (the family in the epic)"},
		{"probe-550e8400-e29b-41d4-a716-446655440000", true, "an operator's own mktemp -d under the base (canonical UUID shape)"},
		{"gopi-acc-4211", true, "an account dir: family + digit token (measured spelling)"},
		{"gopi-privhint-4211", true, "measured shm residue"},
		{"gopi-rule11-shot-4211", true, "multi-token chain (the mid token is accepted once ONE token is generated)"},
		{"gopi-verify-Xk3", true, "os.MkdirTemp's alphabet (digit in the token)"},
		{"gopi-sg-probe_2", true, "os.MkdirTemp's alphabet (underscore in the token)"},
		{"gocache-01a0a392", true, "a Go build cache an agent pointed at the base; task-id suffix"},
		{"gocache-k7fq", true, "base36/random build-cache suffix"},
		{"gocache-9f3a2c1b7e4d6a8f9c0b1d2e3f4a5b6c", true, "a 32-char random cache key"},
		{"gocache-550e8400-e29b-41d4-a716-446655440000", true, "a uuid-shaped cache key (canonical uuid + a non-hex token)"},
		{"probe-8f2a", true, "a writability/device probe directory"},
		{"gopi-acc-4211-1", true, "the de-dup spelling appendPathToken produces"},

		// ── refused: outside the namespace, or a shape no command here writes ─
		//
		// Two refusals are being pinned at once:
		//
		//   (i) NAMESPACE — a name that does not START with a family (a foreign
		//       program, a human's own directory, a name that merely mentions
		//       the family) is never a candidate, at any age;
		//   (ii) SHAPE — a plain word under a family ("gocache-root",
		//       "probe-cfg", "gopi-notes") is a name a HUMAN chose: this
		//       repository's writers always append a digit, an underscore, a
		//       base36 key or a task id, so a word-only suffix is refused.
		//
		// The word-only cases are the regression guard for the defect this
		// predicate was corrected for: the first version accepted ANY token
		// equal to the sweep's OWN PID, so on a host where the sweep ran from
		// pid 4211 all four names below read as generated. A namespace rule
		// whose answer depends on the caller's pid is no rule at all.
		{"postgres", false, "a foreign program's directory"},
		{"gocache-root", false, "a human's directory: no generated token (was accepted by the pid arm in the first version)"},
		{"probe-cfg", false, "a human's directory: a word token"},
		{"gopi-notes", false, "a human's directory: a word token under the gopi- family"},
		{"systemd-private-xyz", false, "a foreign program's directory"},
		{"gocache", false, "the bare family: no suffix, never a shape this system writes"},
		{"probe", false, "the bare family"},
		{"gopi-", false, "the bare family with a dash and nothing else"},
		{"gopi--x", false, "an empty token: nothing this system writes"},
		{"00-task", false, "a numbered foreign name"},
		{"my-gopi-verify-01a0a387", false, "the family is not the FIRST token — a human's directory"},
		{"gofi-gopi-acc-1", false, "the family is not the first token"},
		{"notes-01a0a387", false, "no family prefix at all"},
		{"postgres-01a0a387", false, "a foreign program with a generated-looking suffix"},
		{"tmp.gopi-verify-01a0a387", false, "the family is not the first token (a dotted prefix)"},
	}
	for _, tc := range cases {
		if got := IsBaseResidueName(tc.name); got != tc.want {
			t.Errorf("IsBaseResidueName(%q) = %v, want %v — %s", tc.name, got, tc.want, tc.why)
		}
	}
}

// TestBaseResiduePath_KindGate pins the KIND half of "ours": the entry must be a
// directory or a REGULAR FILE this system created. A symlink is judged as a
// symlink (never followed) and refused; a socket/fifo/device is refused too.
//
// THE FILE ARM IS A FIX, NOT A WIDENING (measured 2026-10-01): a session that
// built the binary with `-o /dev/shm/gopi-…` left a 28 MB regular file in the
// base under a name this system's own generator wrote, and the directory-only
// kind gate kept it out of every sweep forever while IsBaseResidueName said yes.
func TestBaseResiduePath_KindGate(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "gopi-verify-01a0a387")
	if err := os.Mkdir(dir, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !IsBaseResiduePath(dir, "gopi-verify-01a0a387") {
		t.Errorf("a real directory in our namespace must be a candidate")
	}
	file := filepath.Join(base, "gopi-verify-01a0a388")
	if err := os.WriteFile(file, []byte("x"), FilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !IsBaseResiduePath(file, "gopi-verify-01a0a388") {
		t.Errorf("a regular file in our namespace must be a candidate (the compiler-output residue)")
	}
	link := filepath.Join(base, "gopi-verify-01a0a389")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if IsBaseResiduePath(link, "gopi-verify-01a0a389") {
		t.Errorf("a symlink must never be a candidate — its target is not ours to delete")
	}
	fifo := filepath.Join(base, "gopi-verify-01a0a390")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	if IsBaseResiduePath(fifo, "gopi-verify-01a0a390") {
		t.Errorf("a fifo must never be a candidate — this system leaves no fifo in the base")
	}
	if IsBaseResiduePath(filepath.Join(base, "postgres"), "postgres") {
		t.Errorf("a missing foreign path must never be a candidate")
	}
}

// TestBaseResidueSeam_NameLayerAgreesWithCandidateLayer is the CROSS-SEAM
// regression guard the epic asks for: for ONE name, the NAMESPACE answer
// (IsBaseResidueName) and the CANDIDATE answer (the sweep's own predicate) must
// agree about the entry that name carries.
//
// WHY IT EXISTS: the defect this file fixes was exactly a disagreement between
// the two halves — IsBaseResidueName said "ours" about `gopi-red2`, the kind gate
// said "not a directory", and NOTHING reported the contradiction: the sweep
// printed removed=0 and --explain printed one line. Neither half was individually
// wrong; the seam between them was.
//
// THE PROPERTY, stated so it cannot be satisfied by weakening a case: for every
// name in this system's namespace, an entry carrying that name which is STALE and
// UNHELD must be collectable — as a directory AND as a regular file (the file arm
// is what was missing). A symlink is the ONE documented exception, and it is
// asserted in the opposite direction (never collectable, whatever its name).
//
// The fixtures copy their timestamps rather than resting on the clock (the
// directory can be created long before the sweep runs, and a fresh one would be
// kept for the RIGHT reason — which would make this test pass while measuring
// nothing).
func TestBaseResidueSeam_NameLayerAgreesWithCandidateLayer(t *testing.T) {
	// Every family, so the seam is pinned per FAMILY rather than for one sample.
	names := []string{
		"gopi-red2",            // the measured residue: a binary, no underscore, no long key
		"gopi-after-01a0e8c7",  // a task-id tail
		"gocache-01a0a392",     // the build-cache family
		"probe-01a0a393",       // the probe family
		"gopi-verify-01a0a387", // a plain ad-hoc entry
		"gopi-sg-probe_2",      // os.MkdirTemp's underscore alphabet
	}
	// The SUITE-RUN family (suite-<pkg>-run-<rand>) is deliberately NOT in this
	// list: inside a suite its AUTOMATIC arm stands down by design
	// (SetAutoBaseResidue, called by the suite pin), so IsBaseResidueName answers
	// false for it here and the seam would be pinned against the stand-down
	// rather than against the kind rule. That family's own seam — including its
	// FILE arm — is pinned by base_suite_runs_test.go, which re-arms the automatic
	// pass for its own body.
	for _, name := range names {
		if !IsBaseResidueName(name) {
			t.Fatalf("fixture name %q is not in the namespace — the seam test pins nothing", name)
		}

		base := t.TempDir()
		now := time.Now()
		// noHolders is the ONE batched pass every real caller takes (LiveHolders),
		// in its "the scan ran and found nobody" state — the only state in which a
		// stale entry may be collected.
		var noHolders HolderScan

		// DIRECTORY: collectable.
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		reAge(t, dir, baseResidueStaleAge)
		dirVerdict := BaseResidueVerdictFor(dir, name, now, DefaultStaleAge, noHolders, "")
		if !dirVerdict.Collectable {
			t.Errorf("%s: a stale unheld DIRECTORY must be collectable — got kept (%s)", name, dirVerdict.Reason)
		}

		// REGULAR FILE: the arm that was missing. Collectable, and the verdict
		// must be a COLLECTABLE one — not a keep sentence that happened to read
		// well.
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("rmdir %s: %v", dir, err)
		}
		file := filepath.Join(base, name)
		if err := os.WriteFile(file, []byte("residue"), FilePerm); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		reAge(t, file, baseResidueStaleAge)
		fileVerdict := BaseResidueVerdictFor(file, name, now, DefaultStaleAge, noHolders, "")
		if !fileVerdict.Collectable {
			t.Errorf("%s: a stale unheld regular FILE must be collectable (the seam defect) — got kept (%s)",
				name, fileVerdict.Reason)
		}
		if !strings.HasPrefix(fileVerdict.Reason, EntryCollectablePrefix) {
			t.Errorf("%s: a collectable verdict must carry %q, got %q", name, EntryCollectablePrefix, fileVerdict.Reason)
		}

		// The DELETING face must agree with the verdict: the plan names it, the
		// verdict explains it, and the sweep removes it.
		if plan, err := BaseResiduePlan(base, DefaultStaleAge); err != nil {
			t.Fatalf("plan %s: %v", base, err)
		} else if len(plan) != 1 || plan[0].Path != file {
			t.Errorf("%s: the plan must name exactly the file candidate, got %+v", name, plan)
		}
		removed, err := SweepBase(base, DefaultStaleAge)
		if err != nil {
			t.Fatalf("sweep %s: %v", base, err)
		}
		if removed != 1 {
			t.Errorf("%s: sweep removed %d, want 1 (the stale file must go)", name, removed)
		}
		if exists(t, file) {
			t.Errorf("%s: the stale file survived the sweep", name)
		}
		if exists(t, dir) {
			t.Fatalf("%s: the directory fixture survived its own removal", name)
		}

		// The ONE exception, asserted in the opposite direction: a symlink is
		// never a candidate however the name reads, because its target is not
		// this sweep's to remove.
		link := filepath.Join(base, name)
		if err := os.Symlink(t.TempDir(), link); err != nil {
			t.Fatalf("symlink %s: %v", link, err)
		}
		linkVerdict := BaseResidueVerdictFor(link, name, now, DefaultStaleAge, noHolders, "")
		if linkVerdict.Collectable {
			t.Errorf("%s: a SYMLINK must never be collectable", name)
		}
		if linkVerdict.Reason != BaseResidueKeepSymlink {
			t.Errorf("%s: a refused symlink must say so, got %q", name, linkVerdict.Reason)
		}
	}
}

// TestBaseResidueVerdicts_ExplainsEveryInNamespaceEntry pins the --explain face
// itself: every in-namespace entry of the base gets a line with a reason, the
// refused kinds carry the symlink/kind sentence, a foreign name is NOT reported
// (it was never a candidate — reporting neighbours would bury the answer), and a
// directory held by a live process reports the holder veto.
func TestBaseResidueVerdicts_ExplainsEveryInNamespaceEntry(t *testing.T) {
	base := t.TempDir()

	// Ours, stale, a FILE: collectable (the residue the epic measured).
	binary := filepath.Join(base, "gopi-red2")
	if err := os.WriteFile(binary, []byte("ELF"), FilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	reAge(t, binary, baseResidueStaleAge)

	// Ours, stale, a DIRECTORY held by this very process: kept for the holder.
	held := filepath.Join(base, "gopi-held-01a0a394")
	if err := os.Mkdir(held, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reAge(t, held, baseResidueStaleAge)
	open, err := os.Open(held)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = open.Close() }() //stylegate:ignore test fixture handle; the verdict is read while it is open

	// Ours, FRESH: kept, and the reason must name the clock.
	fresh := filepath.Join(base, "gopi-fresh-01a0a395")
	if err := os.WriteFile(fresh, []byte("x"), FilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A foreign neighbour: never a candidate, so never a verdict line.
	foreign := filepath.Join(base, "postgres")
	if err := os.Mkdir(foreign, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reAge(t, foreign, baseResidueStaleAge)

	verdicts, err := BaseResidueVerdicts(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("verdicts: %v", err)
	}
	byPath := map[string]EntryVerdict{}
	for _, v := range verdicts {
		byPath[v.Path] = v
		if v.Reason == "" {
			t.Errorf("%s: a verdict with no reason is the Rule 15 defect this face exists to close", v.Path)
		}
	}
	if _, ok := byPath[foreign]; ok {
		t.Errorf("a foreign name must not be reported as a candidate")
	}
	if v, ok := byPath[binary]; !ok || !v.Collectable {
		t.Errorf("the stale binary must be reported collectable, got %+v", v)
	}
	if v, ok := byPath[held]; !ok || v.Collectable {
		t.Errorf("the live-held directory must be reported KEPT, got %+v", v)
	} else if !strings.Contains(v.Reason, EntryKeepLiveHolder) {
		t.Errorf("the held entry's reason must name the holder veto, got %q", v.Reason)
	}
	if v, ok := byPath[fresh]; !ok || v.Collectable {
		t.Errorf("the fresh entry must be reported KEPT, got %+v", v)
	} else if !strings.Contains(v.Reason, EntryKeepFresh) {
		t.Errorf("the fresh entry's reason must name the clock, got %q", v.Reason)
	}
	// The reason strings are the CLI's vocabulary: the sentence a report quotes
	// must be the sentence the sweep acted on, and one of them must be the
	// collectable marker so a reader can tell the two halves apart.
	if !strings.HasPrefix(byPath[binary].Reason, EntryCollectablePrefix) {
		t.Errorf("the collectable verdict must carry %q, got %q", EntryCollectablePrefix, byPath[binary].Reason)
	}
}

// baseFixture builds a base with one ad-hoc directory of the given name and
// age, returning the base and the directory path.
func baseFixture(t *testing.T, name string, age time.Duration) (string, string) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	reAge(t, dir, age)
	return base, dir
}

// TestBaseSweepFixture_StaleUnheldIsCollected — fixture 1 of 3 (the epic's
// acceptance (b)): an age-stale, unheld entry in this system's own namespace IS
// collected.
func TestBaseSweepFixture_StaleUnheldIsCollected(t *testing.T) {
	base, dir := baseFixture(t, "gopi-verify-01a0a387", baseResidueStaleAge)

	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if exists(t, dir) {
		t.Errorf("%s survived the sweep, want removed", dir)
	}
	if !exists(t, base) {
		t.Errorf("the base itself must never be removed")
	}
}

// TestBaseSweepFixture_FreshEntryIsUntouched — fixture 2a of 3 (acceptance (a)):
// a FRESH entry is kept, because a session may still be writing it.
func TestBaseSweepFixture_FreshEntryIsUntouched(t *testing.T) {
	base, dir := baseFixture(t, "gocache-01a0a392", time.Minute)

	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (fresh)", removed)
	}
	if !exists(t, dir) {
		t.Errorf("%s was removed although it is fresh", dir)
	}
}

// TestBaseSweepFixture_LiveHeldIsUntouched — fixture 2b of 3 (acceptance (a),
// the HARD RULE): an old entry this process HOLDS (an open directory descriptor
// inside it) is kept. The .holder registration is the second channel of the same
// rule and is exercised by the home sweep's own fixtures.
func TestBaseSweepFixture_LiveHeldIsUntouched(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	base, dir := baseFixture(t, "gopi-acc-01a0a393", baseResidueStaleAge)
	held, err := os.Open(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	defer func() { _ = held.Close() }() //stylegate:ignore test fixture handle; the sweep's verdict is taken while it is open, and the test fails before this defer if it is wrong

	if _, err := SweepBase(base, DefaultStaleAge); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !exists(t, dir) {
		t.Errorf("%s was removed although a live process holds it — the hard rule", dir)
	}
}

// TestBaseSweepFixture_ForeignNameIsNeverACandidate — fixture 3 of 3
// (acceptance (c)): a foreign/other-program name is never a candidate, however
// old and however unheld.
func TestBaseSweepFixture_ForeignNameIsNeverACandidate(t *testing.T) {
	base := t.TempDir()
	foreign := []string{"postgres", "systemd-private-abc", "gocache-root", "myapp-data", "tmp.gopi-verify-01a0a387"}
	for _, name := range foreign {
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		reAge(t, dir, baseResidueStaleAge)
	}

	prev := SetAutoBaseResidue(false)
	defer SetAutoBaseResidue(prev)
	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (nothing in the base is ours)", removed)
	}
	for _, name := range foreign {
		if !exists(t, filepath.Join(base, name)) {
			t.Errorf("foreign entry %s was removed — the namespace rule failed", name)
		}
	}
}

// TestBaseSweepFixture_MixedBaseCollectsOnlyOurs — the whole rule in one base:
// ours+stale goes, ours+fresh stays, ours+held stays, foreign+stale stays.
func TestBaseSweepFixture_MixedBaseCollectsOnlyOurs(t *testing.T) {
	base := t.TempDir()
	stale := filepath.Join(base, "gopi-verify-01a0a394")
	fresh := filepath.Join(base, "gocache-01a0a395")
	heldPath := filepath.Join(base, "gopi-acc-01a0a396")
	foreign := filepath.Join(base, "postgres")
	for _, dir := range []string{stale, fresh, heldPath, foreign} {
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, dir := range []string{stale, heldPath, foreign} {
		reAge(t, dir, baseResidueStaleAge)
	}
	reAge(t, fresh, time.Minute)
	held, err := os.Open(heldPath)
	if err != nil {
		t.Fatalf("open %s: %v", heldPath, err)
	}
	defer func() { _ = held.Close() }() //stylegate:ignore test fixture handle; see the holder fixture's note

	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want exactly 1 (the stale unheld in-namespace entry)", removed)
	}
	if exists(t, stale) {
		t.Errorf("the stale unheld entry survived")
	}
	for _, keep := range []string{fresh, heldPath, foreign} {
		if !exists(t, keep) {
			t.Errorf("%s was removed", keep)
		}
	}
}

// TestBaseResiduePlan_NamesExactlyTheSyntheticStaleEntry is the epic's dry-run
// acceptance: against a fixture base the plan names the synthetic stale entry
// and NEVER a foreign one — and reports nothing else.
func TestBaseResiduePlan_NamesExactlyTheSyntheticStaleEntry(t *testing.T) {
	base := t.TempDir()
	synthetic := filepath.Join(base, "gopi-verify-01a0a397")
	fresh := filepath.Join(base, "gocache-01a0a398")
	foreign := filepath.Join(base, "postgres")
	for _, dir := range []string{synthetic, fresh, foreign} {
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	reAge(t, synthetic, baseResidueStaleAge)
	reAge(t, foreign, baseResidueStaleAge)
	reAge(t, fresh, time.Minute)

	plan, err := BaseResiduePlan(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != synthetic {
		t.Fatalf("plan = %+v, want exactly [%s]", plan, synthetic)
	}
	if plan[0].Age < baseResidueStaleAge {
		t.Errorf("planned age = %s, want at least %s", plan[0].Age, baseResidueStaleAge)
	}
	// The plan is a READ: nothing was deleted, and nothing new appeared.
	for _, keep := range []string{synthetic, fresh, foreign} {
		if !exists(t, keep) {
			t.Fatalf("the plan deleted %s — a plan must not delete", keep)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read base: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("base holds %d entries, want 3 — the plan created something", len(entries))
	}
}

// TestBaseResiduePlan_MissingBaseIsEmptyAndNotCreated: the read-only face must
// never bring a base into existence (SweepBase's contract, shared).
func TestBaseResiduePlan_MissingBaseIsEmptyAndNotCreated(t *testing.T) {
	base := filepath.Join(t.TempDir(), "no-such-base")
	plan, err := BaseResiduePlan(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("plan of a missing base: %v", err)
	}
	if len(plan) != 0 {
		t.Fatalf("plan = %+v, want empty", plan)
	}
	if exists(t, base) {
		t.Errorf("the plan created %s", base)
	}
	// The RESOLVED base (the "" argument resolves through ResolveBase) is
	// pointed at a fixture so the arm is exercised end-to-end without touching
	// the developer's real memory base.
	fixture := t.TempDir()
	t.Setenv(BaseEnvName, fixture)
	missingUnderFixture := filepath.Join(fixture, "no-such-base")
	removed, err := SweepBase(missingUnderFixture, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep of a missing base: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d from a missing base, want 0", removed)
	}
	if exists(t, missingUnderFixture) {
		t.Errorf("the sweep created %s", missingUnderFixture)
	}
	if exists(t, base) {
		t.Errorf("the sweep created the missing base %s", base)
	}
	// "" resolves through ResolveBase (ONE reader), which now names the
	// fixture: the resolved base holds no residue, so the arm is exercised
	// end-to-end without touching the developer's real memory base.
	if n, err := SweepBase("", DefaultStaleAge); err != nil || n != 0 {
		t.Errorf("SweepBase(\"\") = (%d, %v), want (0, nil) for an empty resolved base", n, err)
	}
}

// TestSweepBase_RefusesABaseOnTheRootFilesystem — Rule 22 made mechanical: the
// refusal is by MEASURED device id, so it holds for a base spelled "/tmp" and
// for any other directory on the boot partition, and the entry inside it is
// untouched.
func TestSweepBase_RefusesABaseOnTheRootFilesystem(t *testing.T) {
	sharesRoot, err := SharesRootFS(t.TempDir())
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if !sharesRoot {
		t.Skip("t.TempDir() is not on the / filesystem on this host — the refusal fixture cannot be built")
	}
	base, dir := baseFixture(t, "gopi-verify-01a0a399", baseResidueStaleAge)
	if _, err := SweepBase(base, DefaultStaleAge); err == nil {
		t.Fatalf("SweepBase(%s) succeeded, want the on-/ refusal", base)
	}
	if !exists(t, dir) {
		t.Errorf("%s was removed although the base is on the / filesystem", dir)
	}
}

// TestSweepBase_AutoResidueArmIsOnOnlyWhenArmed pins the seam itself: a suite
// pin (internal/testutil.PinSuiteForTestMain) stands the automatic arm down for
// the WHOLE run — that is the point, since the suite runs on the developer's
// host — while an explicitly configured base (GOPI_SCRATCH_BASE) is swept
// whichever way the seam is set.
func TestSweepBase_AutoResidueArmIsOnOnlyWhenArmed(t *testing.T) {
	// The suite pin has run (TestMain), so the automatic arm must read OFF. The
	// package-level default stays true (production never calls the setter), so
	// this assertion also proves the setter is what suites rely on rather than a
	// changed zero value.
	prev := SetAutoBaseResidue(true)
	if prev {
		t.Fatalf("the automatic base arm is ON inside a suite — the developer's real memory base is exposed to go test")
	}
	SetAutoBaseResidue(prev)
	fixture := t.TempDir()
	explicit := filepath.Join(fixture, "gopi-verify-01a0a401")
	if err := os.Mkdir(explicit, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reAge(t, explicit, baseResidueStaleAge)
	// A configured base is swept even with the automatic arm stood down.
	t.Setenv(BaseEnvName, fixture)
	standDown := SetAutoBaseResidue(false)
	if removed, err := SweepBase("", DefaultStaleAge); err != nil || removed != 1 {
		t.Fatalf("SweepBase with an explicit base = (%d, %v), want (1, nil)", removed, err)
	}
	SetAutoBaseResidue(standDown)
}

// TestSweepBase_RefusesTheScratchHome: when the base IS this process's scratch
// home, the base arm stands down — the home sweep owns that tree (one collector
// per tree, and a base arm would RemoveAll an entry the home sweep protects).
func TestSweepBase_RefusesTheScratchHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeDirEnvName, home)
	// "gocache-01a0a400" would be a candidate in a base, but not here.
	dir := filepath.Join(home, "gocache-01a0a400")
	if err := os.Mkdir(dir, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reAge(t, dir, baseResidueStaleAge)

	removed, err := SweepBase(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep of the scratch home: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0 — the home sweep owns the scratch home", removed)
	}
	if !exists(t, dir) {
		t.Errorf("%s was removed by the base arm", dir)
	}
}

// TestBaseResiduePrefixes_AreTheDocumentedFamilies pins the family list itself:
// each name is a prefix this repository's own commands produce, and the list is
// what IsBaseResidueName walks (a family added without a justification fails
// here first).
func TestBaseResiduePrefixes_AreTheDocumentedFamilies(t *testing.T) {
	want := []string{BaseResiduePrefixGopi, BaseResiduePrefixGocache, BaseResiduePrefixProbe}
	if len(baseResiduePrefixes) != len(want) {
		t.Fatalf("baseResiduePrefixes = %v, want %v", baseResiduePrefixes, want)
	}
	for i, prefix := range want {
		if baseResiduePrefixes[i] != prefix {
			t.Errorf("family %d = %q, want %q", i, baseResiduePrefixes[i], prefix)
		}
	}
}

// TestResolveBase_KnobIsReadOnce: the base is HOST configuration and has ONE
// reader; an explicit value wins, and the default is the shared-memory path.
func TestResolveBase_KnobIsReadOnce(t *testing.T) {
	base := t.TempDir()
	t.Setenv(BaseEnvName, base)
	if got := ResolveBase(); got != CanonicalPath(base) {
		t.Errorf("ResolveBase() = %q, want %q", got, CanonicalPath(base))
	}
	t.Setenv(BaseEnvName, "")
	if got := ResolveBase(); got != CanonicalPath(DefaultBasePath) {
		t.Errorf("ResolveBase() = %q, want the default %q", got, CanonicalPath(DefaultBasePath))
	}
}

// ---------------------------------------------------------------------------
// The SUITE-RUN arm: "suite-<pkg>-run-<N>" roots created DIRECTLY in the memory
// base (the family the 2026-09-28 measurement found with no collector at all).
// ---------------------------------------------------------------------------

// TestSuiteRunResidueName_NamespaceTable pins the arm's shape rule name by name.
//
// THE ACCEPTED HALF is the family measured in the base on this host
// (2026-09-28): 146 `suite-<pkg>-run-<N>` roots, 5.9G of a 7.8G tmpfs, created
// by internal/testutil's own pin via os.MkdirTemp(home, "suite-<suite>-run-").
//
// THE REFUSED HALF is the safety argument, and every entry in it is a name a
// person could type: the arm must not widen to "any name containing suite-" (a
// neighbour's directory) nor to "any name ending in a small number" (a human's
// counter). Both refusals are red-proofed: each case below FAILS against the
// predicate the epic's defect was reported with (a bare SuiteRunMarker prefix
// test, which accepts every name in the refused block).
func TestSuiteRunResidueName_NamespaceTable(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	cases := []struct {
		name string
		want bool
		why  string
	}{
		// ── accepted: the measured residue, verbatim from this host's base ──
		{"suite-agent-run-3113361481", true, "measured: /dev/shm/suite-agent-run-3113361481"},
		{"suite-cmd-agent-run-1103740161", true, "measured: the cmd/agent suite root (57M)"},
		{"suite-internal-tools-run-3286604888", true, "a suite name with a dash inside it"},
		{"suite-unnamed-run-1234567890", true, "the pin's unknown-suite label"},
		{"suite-tools-run-1a2b3c4d5e6f708192a3b4c5d6e7f809", true, "a fixture naming its root after a task id (32 hex chars)"},
		{"suite-x-run-550e8400-e29b-41d4-a716-446655440000", true, "a uuid tail (an operator's own mktemp)"},

		// ── refused: a human's names, or a foreign tree ─────────────────────
		{"suite-notes", false, "no run token: a directory a person made"},
		{"suite-notes-run-1", false, "a ONE-digit tail is a counter a human wrote, not os.MkdirTemp's suffix"},
		{"suite-notes-run-latest", false, "a word tail"},
		{"suite-notes-run-final", false, "a word tail"},
		{"my-suite-runner-2", false, "\"run\" is not its own token — the infix trap a bare marker test falls into"},
		{"suite--run-1234", false, "an empty suite name"},
		{"suite-", false, "the bare marker"},
		{"suite-run-1234", false, "no suite name at all"},
		{"suite-notes-run-1234567890.tar", false, "a dot is not in this system's name alphabet"},
		{"postgres", false, "a foreign program's directory"},
		{"suite", false, "the bare word"},
		{"notes-suite-run-1234567890", false, "the marker is not the first token"},
		{"gopi-suite-tmp-ab12cd", false, "the LEGACY family is collected by internal/testutil's arm — one entry, one collector"},
	}
	for _, tc := range cases {
		if got := IsBaseResidueName(tc.name); got != tc.want {
			t.Errorf("IsBaseResidueName(%q) = %v, want %v — %s", tc.name, got, tc.want, tc.why)
		}
	}
}

// TestSuiteRunResidueName_RedProof is the red-first half: the predicate the
// defect was REPORTED with — "does the name start with the suite marker" —
// accepts every refused name above, which is exactly why a suite root in the
// base was in nobody's namespace (the sweep's other families are dash-prefixed
// and matched none of them). This is the guard's own red proof: if the arm is
// ever reduced to a prefix test, this test goes red.
func TestSuiteRunResidueName_RedProof(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	// The two weaker predicates a reader or a refactor reaches for first. BOTH
	// accept names this sweep must never delete, which is why the arm needs the
	// shape rule: a prefix test on the marker, and a "contains the marker"
	// test (the spelling the HOME sweep's own marker rule uses — legitimate
	// there, because it walks a tree this system owns, and fatal here).
	prefixOnly := func(name string) bool { return strings.HasPrefix(name, SuiteRunMarker) }
	infixOnly := func(name string) bool { return strings.Contains(name, SuiteRunMarker) }
	for _, name := range []string{"suite-notes", "suite-notes-run-1", "suite--run-1234", "suite-notes-run-final"} {
		if !prefixOnly(name) {
			t.Fatalf("the red-proof premise is wrong: %q does not start with %q", name, SuiteRunMarker)
		}
		if IsBaseResidueName(name) {
			t.Errorf("IsBaseResidueName(%q) = true — the arm has been widened to a marker test, which is the "+
				"defect: the base is 1777 and shared, so a name this sweep did not create is not a name it may delete", name)
		}
	}
	if !infixOnly("my-suite-runner-2") {
		t.Fatalf("the red-proof premise is wrong: my-suite-runner-2 does not contain %q", SuiteRunMarker)
	}
	if IsBaseResidueName("my-suite-runner-2") {
		t.Errorf("IsBaseResidueName(\"my-suite-runner-2\") = true — a name that merely CONTAINS the marker is a " +
			"foreign tree, and the arm must not reach it")
	}
}

// TestBaseSweepFixture_SuiteRunRootIsCollected is the epic's acceptance (b) at
// the sweep level: a stale, unheld `suite-<pkg>-run-<N>` root placed directly in
// the base IS collected by the ONE base sweep the CLI verb and the resident tick
// both call.
func TestBaseSweepFixture_SuiteRunRootIsCollected(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	base, dir := baseFixture(t, "suite-cmd-agent-run-3113361481", baseResidueStaleAge)

	plan, err := BaseResiduePlan(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != dir {
		t.Fatalf("plan = %+v, want exactly %s — a --dry-run must promise what the sweep does", plan, dir)
	}
	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if exists(t, dir) {
		t.Errorf("%s survived the sweep, want removed", dir)
	}
	if !exists(t, base) {
		t.Errorf("the base itself must never be removed")
	}
}

// TestBaseSweepFixture_SuiteRunRootFreshOrHeldIsUntouched is the other half of
// the hard rule for the new family: age and liveness are unchanged by adding a
// namespace — a FRESH suite root (a suite is running right now) and a root held
// by a live process are both kept.
func TestBaseSweepFixture_SuiteRunRootFreshOrHeldIsUntouched(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	base := t.TempDir()
	fresh := filepath.Join(base, "suite-cmd-agent-run-1234567890")
	heldPath := filepath.Join(base, "suite-agent-run-9876543210")
	for _, dir := range []string{fresh, heldPath} {
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	reAge(t, heldPath, baseResidueStaleAge) // old, but held below

	held, err := os.Open(heldPath)
	if err != nil {
		t.Fatalf("open %s: %v", heldPath, err)
	}
	defer func() { _ = held.Close() }() //stylegate:ignore test fixture handle; the sweep's verdict is taken while it is open

	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (one fresh, one held)", removed)
	}
	for _, dir := range []string{fresh, heldPath} {
		if !exists(t, dir) {
			t.Errorf("%s was removed — age and the holder rule must apply unchanged to the new family", dir)
		}
	}
}

// armAutoBaseForFixture re-enables the AUTOMATIC base arm for the duration of
// one test and returns the restore. The suite's TestMain pin stands the arm down
// (SetAutoBaseResidue(false)) so a `go test` run can never sweep the developer's
// real memory base; a test whose SUBJECT is the base namespace must therefore
// arm it explicitly, against its own fixture base (Rule 6: the fixture is the
// only base it touches).
func armAutoBaseForFixture(t *testing.T) func() {
	t.Helper()
	prev := SetAutoBaseResidue(true)
	return func() { SetAutoBaseResidue(prev) }
}
