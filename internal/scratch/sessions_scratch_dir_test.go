// sessions_scratch_dir_test.go — bounding the `session-scratch-dir` family:
// what the sweep must COLLECT, what it must never TOUCH, and the red-proof that
// the sweep's OWN hook — not the test's copy of it — is the thing that decides.
//
// WHY TWO-SIDED AND NOT ONE-SIDED. "An old directory went away" is satisfied by
// a predicate that is simply too blunt: it cannot tell the sweep that bounds the
// family from the sweep that eats a live run's record. So ONE fixture container
// tree holds the collect shape beside every measured KEEP shape — a container the
// ledger describes (`metadata.json`), a container holding a resumable `*.jsonl`,
// a `.holder`-registered directory, a young directory, a young container, a
// container holding nothing but that one directory (a run in flight), a plain
// file beside it, and a symlink — and the test asserts the EXACT set that came
// back.
//
// The ages come from the tree's OWN window (`SessionScratchDirStaleAge` is the
// bound the hook's caller applies through the sweep's override), never from a
// literal, and the sweep is driven with a window far SHORTER than every fixture
// age — so nothing here can pass because of an age the fixture did not intend
// (Rule 1: no magic numbers, not even in a test).
package scratch

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// scratchDirFixtureOrphan is a container the ledger has FORGOTTEN: a
	// uuid-shaped directory holding no record of any kind.
	scratchDirFixtureOrphan = "01a0f40b-0000-7000-a000-000000000000"
	// scratchDirFixtureLedger is a container the ledger still describes through
	// `metadata.json`.
	scratchDirFixtureLedger = "01a0f4f9-baf5-7f13-a707-d05a68ea12e4"
	// scratchDirFixtureRecord is a per-run record's name (a `*.jsonl` the ledger
	// can still resume).
	scratchDirFixtureRecord = "2247d577-8044-43c5-bcc1-512642883822.jsonl"
	// The fixture DIRECTORY names: the run-chosen shapes the census measured
	// (evidence / evid / artifacts / bin). No name rule could tell them apart,
	// which is the whole finding.
	scratchDirFixtureDeadName  = "evidence"
	scratchDirFixtureKeptName  = "evid"
	scratchDirFixtureHeldName  = "artifacts"
	scratchDirFixtureBinName   = "bin"
	scratchDirFixturePayloadNm = "payload.bin"
	// scratchDirFixtureAge is how old every COLLECT-shaped directory and
	// container is made: comfortably past the arm's own window
	// (`SessionScratchDirStaleAge`), because that window — not the caller's — is
	// the one this arm judges by (scratchDirWindow).
	scratchDirFixtureAge = SessionScratchDirStaleAge + 48*time.Hour
	// scratchDirFixtureWindow is the override the sweep is driven with. It is
	// the arm's own declared window, named EXPLICITLY rather than inherited from
	// the row's StaleAge: the row's `session-raw` window is the *.raw.log FILE
	// arm's 14 days, and a test that drove the sweep with that value would be
	// testing the floor instead of the decision (the floor has its own test).
	scratchDirFixtureWindow = SessionScratchDirStaleAge
	// scratchDirFixturePayload is the byte size of the file inside each fixture
	// directory, so the reclaimed total is a measured number.
	scratchDirFixturePayload = 4096
	// scratchDirFixturePlainName is a plain, non-candidate file dropped into the
	// containers whose DIRECTORIES are the subject. The `session-raw` row's
	// container arm removes a container that holds no non-candidate child
	// (`keptFiles == 0`), which is its own existing rule and not this family's
	// decision — without this file the fixture's containers would be collected
	// beside the directories under test, and the assertions would be about two
	// rules at once.
	scratchDirFixturePlainName = "notes.txt"
)

// scratchDirFixtureRoot builds a sessions tree under a fresh temp directory and
// points the gopi data root at it, so the session-raw row's resolver finds it.
// It returns the root and the row itself — the REAL row from DeclaredTrees(),
// which is the whole point of driving the test through the tree rather than
// through the hook.
func scratchDirFixtureRoot(t *testing.T) (string, DeclaredTree) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root, err := SessionRawLogRoot()
	if err != nil {
		t.Fatalf("the sessions tree is not resolvable with GOPI_HOME pinned: %v", err)
	}
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	tree, ok := treeByName(TreeNameSessionRaw)
	if !ok {
		t.Fatalf("the %s tree must be declared — it is the arm this family is bound through", TreeNameSessionRaw)
	}
	return root, tree
}

// agePath backdates ONE path's mtime by age.
func agePath(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}

// seedScratchDir seeds ONE directory with a payload file inside it and returns
// both paths, so the caller can age them LAST.
//
// WHY AGING COMES LAST, and not inside this helper: creating a child moves the
// PARENT's mtime, so a container backdated before its directory was seeded reads
// as freshly touched — which is exactly the state the predicate protects (a run
// in flight). The fixture therefore builds everything first and ages it
// afterwards; the failure that caught this is recorded in the report.
func seedScratchDir(t *testing.T, path string) (string, string) {
	t.Helper()
	if err := os.MkdirAll(path, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	payload := filepath.Join(path, scratchDirFixturePayloadNm)
	if err := os.WriteFile(payload, make([]byte, scratchDirFixturePayload), 0o600); err != nil {
		t.Fatalf("write %s: %v", payload, err)
	}
	return path, payload
}

// seedContainer seeds a container directory with the given child names, plus one
// plain non-candidate FILE.
//
// WHY THE PLAIN FILE: the `session-raw` row's container arm removes a container
// that has no non-candidate child left (`keptFiles == 0`, an EXISTING rule of
// that arm). These fixtures are about the container arm's DIRECTORY hook, and one
// rule per test is the point: with a plain file inside, the containers stay, and
// an assertion about which directories went is an assertion about this family
// rather than about two rules at once.
func seedContainer(t *testing.T, path string, children ...string) string {
	t.Helper()
	if err := os.MkdirAll(path, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	for _, child := range children {
		if err := os.MkdirAll(filepath.Join(path, child), DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Join(path, child), err)
		}
	}
	writeContainerFile(t, path, scratchDirFixturePlainName)
	return path
}

// writeContainerFile drops one file inside a container — the record shapes that
// decide whether the ledger still describes it.
func writeContainerFile(t *testing.T, container, name string) {
	t.Helper()
	if err := os.MkdirAll(container, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", container, err)
	}
	if err := os.WriteFile(filepath.Join(container, name), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", filepath.Join(container, name), err)
	}
}

// TestSessionsScratchDirSweepIsTwoSided is the contract: the collect shapes, and
// every KEEP shape the epic names beside them, decided by the REAL `session-raw`
// tree row. Everything is built first and AGED LAST (an mtime moves when a child
// is created), so each container carries the age its case intends.
func TestSessionsScratchDirSweepIsTwoSided(t *testing.T) {
	root, tree := scratchDirFixtureRoot(t)

	// --- COLLECT: a container the ledger has forgotten, holding a directory
	// past the window.
	orphan := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan))
	dead, deadPayload := seedScratchDir(t, filepath.Join(orphan, scratchDirFixtureDeadName))

	// --- KEEP: a container the ledger describes. Its directory is past the
	// window, so ONLY the container's lifecycle can be what saves it. The
	// container also carries an `attachments/` archive — the sibling family
	// whose bundle may be a deleted branch's ONLY surviving copy — so this case
	// pins the two together: a ledger-carrying container keeps its working
	// directories AND its archive.
	ledger := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger))
	writeContainerFile(t, ledger, sessionlayout.MetadataFileName)
	if err := os.MkdirAll(filepath.Join(ledger, sessionlayout.AttachmentsDirName), DirPerm); err != nil {
		t.Fatalf("mkdir attachments: %v", err)
	}
	keptByMetadata, keptByMetadataPayload := seedScratchDir(t, filepath.Join(ledger, scratchDirFixtureDeadName))

	// --- KEEP: a container holding a resumable record (a `*.jsonl`).
	recordContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-record"), scratchDirFixtureKeptName)
	writeContainerFile(t, recordContainer, scratchDirFixtureRecord)
	keptByRecord, keptByRecordPayload := seedScratchDir(t, filepath.Join(recordContainer, scratchDirFixtureKeptName))

	// --- KEEP: a `.holder`-registered directory inside a forgotten container
	// (the permission-free liveness channel).
	holderContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-holder"), scratchDirFixtureHeldName)
	held, heldPayload := seedScratchDir(t, filepath.Join(holderContainer, scratchDirFixtureHeldName))
	RegisterHolder(held)

	// --- KEEP: a FRESH directory inside a forgotten container (the window).
	youngContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-young"), scratchDirFixtureKeptName)
	freshDir, _ := seedScratchDir(t, filepath.Join(youngContainer, scratchDirFixtureKeptName))

	// --- COLLECT, with the registry's OTHER families left alone: a container
	// that holds NO record of any kind, an `attachments/` archive, and a
	// `bin/` directory, all past the window. The arm's rule is about the
	// container's ledger, so its `bin/` is a candidate — and the archive beside
	// it is NOT, because the registry answers for it (`attachments-archive`,
	// KEEP: a deleted branch's only surviving copy is an attached bundle). The
	// hook asks the registry's own classifier, so this case is where a second
	// answer to "whose family is this directory?" would break loudly.
	plainContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-plain"), scratchDirFixtureBinName)
	archive := filepath.Join(plainContainer, sessionlayout.AttachmentsDirName)
	if err := os.MkdirAll(archive, DirPerm); err != nil {
		t.Fatalf("mkdir attachments: %v", err)
	}
	deadBesideArchive, deadBesideArchivePayload := seedScratchDir(t, filepath.Join(plainContainer, scratchDirFixtureBinName))

	// --- KEEP: a probe's whole CHECKOUT (holds `.git`) inside a forgotten
	// container, and a materialised sandbox PROFILE (holds `.gopi`) — the
	// registry's two `legacy` container families. Neither is this arm's to
	// decide: a checkout's contents are a full repository tree and a sandbox has
	// its own reclaimer, so the hook must route both away by asking the registry
	// rather than by pattern-matching the two names itself.
	checkoutContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-checkout"))
	checkout := seedContainer(t, filepath.Join(checkoutContainer, "smoke-clone"), ".git")
	profileContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-profile"))
	profile := seedContainer(t, filepath.Join(profileContainer, "capdemo"))
	if err := os.MkdirAll(filepath.Join(profile, ".gopi"), DirPerm); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}

	// --- KEEP: a container holding NOTHING but this one directory, and TOUCHED
	// RECENTLY — the shape a run in flight has (the container is minted before
	// the record is written). The window is what protects it, and this case is
	// exactly the one the fixture's first version got wrong.
	inFlight := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-inflight"), scratchDirFixtureDeadName)
	inFlightDir, _ := seedScratchDir(t, filepath.Join(inFlight, scratchDirFixtureDeadName))

	// --- KEEP: a SYMLINK inside a forgotten container (removing a link removes
	// its target with it).
	linkContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-link"))
	linkTarget := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger+"-linktarget"))
	link := filepath.Join(linkContainer, "link")
	if err := os.Symlink(linkTarget, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}

	// --- AGE LAST. The collect containers and their directories go past the
	// window; the in-flight container stays inside it.
	for _, p := range []string{orphan, dead, deadPayload, ledger, keptByMetadata, keptByMetadataPayload,
		recordContainer, keptByRecord, keptByRecordPayload, holderContainer, held, heldPayload,
		plainContainer, archive, deadBesideArchive, deadBesideArchivePayload,
		checkoutContainer, checkout, profileContainer, profile, linkContainer, linkTarget} {
		agePath(t, p, scratchDirFixtureAge)
	}

	// The PLAN and the SWEEP are one predicate with two faces: what the plan
	// promises must be exactly what the sweep takes.
	wantPlan := map[string]bool{dead: true, deadBesideArchive: true}
	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != len(wantPlan) {
		t.Fatalf("plan=%+v, want exactly %d candidates (%v) — the plan face and the sweep face must not be two answers",
			plan, len(wantPlan), wantPlan)
	}
	for _, e := range plan {
		if !wantPlan[e.Path] {
			t.Fatalf("plan includes %s, which no fixture says is collectable: %+v", e.Path, plan)
		}
		if e.Size < scratchDirFixturePayload {
			t.Errorf("planned size=%d for %s, want at least the %d-byte payload: a plan that measured 0 would make the report a lie",
				e.Size, e.Path, scratchDirFixturePayload)
		}
	}

	// The sweep decides through the hook — proved by counting its calls, so a
	// "two-sided" result that came from some other code path is visible here.
	calls := 0
	restore := wrapScratchDirHook(func(inner scratchDirHookFunc, tree DeclaredTree, path, container string, now time.Time, age time.Duration, held heldTreeScan) []TreeEntry {
		calls++
		return inner(tree, path, container, now, age, held)
	})
	removed, bytes, err := SweepDeclaredTree(tree)
	restore()
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if calls == 0 {
		t.Errorf("the sweep never called the container arm's directory hook — this test's subject was not executed")
	}
	if removed != 2 {
		t.Errorf("removed=%d, want 2 (the forgotten container's aged directory, and the no-record container's bin beside its archive)", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytes=%d, want the measured size of the reclaimed directories (the epic requires bytes on the log line)", bytes)
	}
	for _, gone := range []string{dead, deadBesideArchive} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("the collect shape %s survived (err=%v)", gone, err)
		}
	}
	kept := map[string]string{
		"a container the ledger describes (metadata.json)":            keptByMetadata,
		"a container holding a resumable record (*.jsonl)":            keptByRecord,
		"a .holder-registered directory":                              held,
		"a fresh directory":                                           freshDir,
		"the only directory of a container touched inside the window": inFlightDir,
		"a symlink": link,
		"a probe's checkout (holds .git — the foreign-checkout family)": checkout,
		"a materialised sandbox profile (holds .gopi)":                  profile,
	}
	for why, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("KEPT shape collected: %s — %s (%v)", path, why, err)
		}
	}
	if _, err := os.Stat(linkTarget); err != nil {
		t.Errorf("the symlink's TARGET %s was removed — a link must never be a candidate: %v", linkTarget, err)
	}
	if _, err := os.Stat(filepath.Join(plainContainer, sessionlayout.AttachmentsDirName)); err != nil {
		t.Errorf("the attachments archive in %s was removed — the registry answers for that directory (attachments-archive, KEEP), and this arm must ask it rather than decide for it: %v", plainContainer, err)
	}
}

const (
	// scratchDirFloorFixtureAge is the age of a directory that the FILE arm's
	// window (the row's own StaleAge, 14 days) would collect and this arm's floor
	// (SessionScratchDirStaleAge) must NOT: 15 days is one day past the file
	// window and half a month inside the directory window.
	scratchDirFloorFixtureAge = 15 * 24 * time.Hour
	// scratchDirFloorPastAge is the age of a directory BOTH windows agree on:
	// past the floor, so it is a candidate under the row's own default window.
	scratchDirFloorPastAge = SessionScratchDirStaleAge + 24*time.Hour
)

// TestSessionsScratchDirWindowFloorIsTheArmsOwn is the regression guard for the
// window itself, and it is RED against the shape this arm first shipped.
//
// THE DEFECT IT PINS (measured 2026-10-01, caught by re-reading the constant
// against its readers): containerEntries hands its hook the same `age` it judges
// the row's own candidates by, which for `session-raw` is the *.raw.log FILE
// arm's 14 days. The first version used that value directly, so
// SessionScratchDirStaleAge — the 30-day window the constant, the census doc and
// the registry row all describe — was read by NOTHING, and a 15-day-old evidence
// directory inside a forgotten container was collectable: a deletion the
// documented decision does not authorise, in the direction that costs evidence.
//
// The floor is asserted from BOTH sides, and through the REAL sweep as well as
// the hook, because a floor the hook applies and the plan does not would be two
// answers again.
func TestSessionsScratchDirWindowFloorIsTheArmsOwn(t *testing.T) {
	root, tree := scratchDirFixtureRoot(t)

	// One forgotten container with two directories: one 15 days old (the file
	// arm's window collects it, this arm's floor does not) and one past the
	// floor.
	orphan := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan))
	between, _ := seedScratchDir(t, filepath.Join(orphan, "evidence"))
	past, _ := seedScratchDir(t, filepath.Join(orphan, "artifacts"))
	agePath(t, orphan, scratchDirFloorPastAge)
	agePath(t, between, scratchDirFloorFixtureAge)
	agePath(t, past, scratchDirFloorPastAge)

	if got := scratchDirWindow(time.Hour); got != SessionScratchDirStaleAge {
		t.Errorf("scratchDirWindow(1h) = %s, want the arm's own %s: a caller's short window must not shorten the window this arm deletes by",
			got, SessionScratchDirStaleAge)
	}
	if long := SessionScratchDirStaleAge + time.Hour; scratchDirWindow(long) != long {
		t.Errorf("scratchDirWindow(%s) = %s: an operator asking for a LONGER window must be honoured (a sweep face may always be more conservative)",
			long, scratchDirWindow(long))
	}

	// The row's OWN window (no override): the plan is what `gopi tool scratch-gc
	// trees` promises, and the 15-day directory must not be in it.
	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != past {
		t.Fatalf("plan=%+v with the %s row's own window, want exactly %s (the directory past the arm's floor, and NOT the %s-old one)",
			plan, tree.Name, past, scratchDirFloorFixtureAge)
	}

	// And the decision the hook itself returns, for the shape the defect was:
	// the 15-day directory is refused even when the caller hands down a window
	// that would take it.
	const callerWindow = time.Hour
	if cand := containerScratchDirEntries(tree, between, orphan, time.Now(), callerWindow, heldTreeScan{index: heldSet(HolderScan{}, root)}); len(cand) != 0 {
		t.Errorf("containerScratchDirEntries collected %s at %s old under a %s caller window: the arm's own floor (%s) did not decide — every directory the ledger has forgotten between the file window and the floor would be deleted by a sweep the documented decision forbids",
			between, scratchDirFloorFixtureAge, callerWindow, SessionScratchDirStaleAge)
	}
	if cand := containerScratchDirEntries(tree, past, orphan, time.Now(), callerWindow, heldTreeScan{index: heldSet(HolderScan{}, root)}); len(cand) != 1 {
		t.Errorf("containerScratchDirEntries refused %s at %s old (past the floor %s): the arm would never collect anything",
			past, scratchDirFloorPastAge, SessionScratchDirStaleAge)
	}
}

// TestSessionsScratchDirSweepHookIsTheOneThatDecides is the RED-PROOF the epic
// asks for: it takes a directory the REAL predicate protects (an aged directory
// inside a container the ledger still describes) and shows that the REAL sweep —
// SweepDeclaredTree on the REAL `session-raw` row, the same call
// `gopi tool scratch-gc trees` makes — takes it the moment the ONE hook is neutered,
// and only then. A proof that edited anything else would be a proof about a
// different code path.
func TestSessionsScratchDirSweepHookIsTheOneThatDecides(t *testing.T) {
	root, tree := scratchDirFixtureRoot(t)
	ledger := seedContainer(t, filepath.Join(root, scratchDirFixtureLedger))
	writeContainerFile(t, ledger, sessionlayout.MetadataFileName)
	protected, protectedPayload := seedScratchDir(t, filepath.Join(ledger, scratchDirFixtureDeadName))
	orphan := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan))
	dead, deadPayload := seedScratchDir(t, filepath.Join(orphan, scratchDirFixtureDeadName))
	for _, p := range []string{ledger, protected, protectedPayload, orphan, dead, deadPayload} {
		agePath(t, p, scratchDirFixtureAge)
	}

	// CONTROL, with the real hook in place: the protected directory is not a
	// candidate, the collect shape is.
	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != dead {
		t.Fatalf("control plan=%+v, want only %s: the ledger-carrying container's directory must be protected before the neutering", plan, dead)
	}

	restore := neuterScratchDirHook()
	removed, bytes, err := SweepDeclaredTree(tree)
	restore()
	if err != nil {
		t.Fatalf("sweep with the hook neutered: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed=%d with the ONE hook neutered, want 2 (both directories) — the sweep did not decide through containerScratchDirEntries, so this proof is about some other code path", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytes=%d: the neutered sweep reported no reclaimed bytes", bytes)
	}
	if _, err := os.Stat(protected); !os.IsNotExist(err) {
		t.Errorf("the protected directory %s survived a neutered hook (err=%v) — the hook is not what decides", protected, err)
	}
	if _, err := os.Lstat(dead); !os.IsNotExist(err) {
		t.Errorf("the collect shape %s survived the neutered sweep (err=%v)", dead, err)
	}
}

// neuterScratchDirHook replaces the container arm's ONE directory hook with a
// blanket verdict, and returns the restore function. The neutered form is the
// OLD behaviour of the arm plus the age test — i.e. exactly what the sweep did
// before this change for an aged directory — so the red proof shows the hook,
// and not the age window, is what protects the ledger-carrying container.
func neuterScratchDirHook() func() {
	prev := containerScratchDirEntries
	containerScratchDirEntries = func(tree DeclaredTree, path, _ string, now time.Time, age time.Duration, held heldTreeScan) []TreeEntry {
		if heldIn(path, held.index) || !unheldRegistered(path) {
			return nil
		}
		stale, err := tree.entryStale(path, now, age)
		if err != nil || !stale {
			return nil
		}
		return []TreeEntry{measuredEntry(tree, path, true, now)}
	}
	return func() { containerScratchDirEntries = prev }
}

// scratchDirHookFunc is the container arm's directory hook's signature.
type scratchDirHookFunc = func(DeclaredTree, string, string, time.Time, time.Duration, heldTreeScan) []TreeEntry

// wrapScratchDirHook installs an OBSERVING wrapper around the hook and returns
// its restore function. The observer is handed the wrapped function itself, so it
// must call `inner` — calling containerScratchDirEntries from inside it would call
// the wrapper again and overflow the stack (measured: it did, on the first run of
// this test).
func wrapScratchDirHook(observe func(inner scratchDirHookFunc, tree DeclaredTree, path, container string, now time.Time, age time.Duration, held heldTreeScan) []TreeEntry) func() {
	prev := containerScratchDirEntries
	containerScratchDirEntries = func(tree DeclaredTree, path, container string, now time.Time, age time.Duration, held heldTreeScan) []TreeEntry {
		return observe(prev, tree, path, container, now, age, held)
	}
	return func() { containerScratchDirEntries = prev }
}

// TestScratchDirFamilyIsBoundThroughTheContainerArm pins the wiring the registry
// row depends on: the family is `bound:` to the tree the sweep sweeps, that tree
// is a member of DeclaredTrees() (so `gopi tool scratch-gc trees` walks it), and the
// container arm is where its directories are decided.
func TestScratchDirFamilyIsBoundThroughTheContainerArm(t *testing.T) {
	fam, ok := sessionlayout.FamilyByName(sessionlayout.FamilySessionScratchDir)
	if !ok {
		t.Fatal("the session-scratch-dir family is not registered — the census would report its entries as unclassified")
	}
	if fam.Decision != sessionlayout.DecisionBound {
		t.Fatalf("family %s carries decision %q, want bound: the family was left unbounded while the sweep collects it",
			fam.Name, fam.Decision)
	}
	if fam.Tree != TreeNameSessionRaw {
		t.Errorf("family %s is bound to tree %q, want %q — the arm that decides its directories is the session-raw one",
			fam.Name, fam.Tree, TreeNameSessionRaw)
	}
	// The tree the registry names is a REAL row of the swept set (Rules 13/14:
	// the registry's constant and this package's constant are one value).
	if fam.Tree != TreeNameSessionRaw {
		t.Errorf("the registry spells the tree %q while this package names it %q", fam.Tree, TreeNameSessionRaw)
	}
	tree, ok := treeByName(fam.Tree)
	if !ok {
		t.Fatalf("the registry binds the family to tree %q, which DeclaredTrees() does not hold — a bound naming a tree that does not exist bounds nothing", fam.Tree)
	}
	if tree.RootStaleAge == 0 {
		t.Errorf("the %s row carries no RootStaleAge: the container-arm hook is invoked from containerEntries, which only runs for a TreeFileSuffix row", tree.Name)
	}
	// The rows the census documents still name the trees they claim.
	if tree.Name != TreeNameSessionRaw {
		t.Errorf("treeByName(%q) returned %q", fam.Tree, tree.Name)
	}
}

// dirEntryFor returns the os.DirEntry a directory listing yields for path, so a
// test can drive containerEntries — whose argument is an entry, not a path —
// with the REAL entry the walk would hand it.
func dirEntryFor(t *testing.T, path string) os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Dir(path), err)
	}
	base := filepath.Base(path)
	for _, e := range entries {
		if e.Name() == base {
			return e
		}
	}
	t.Fatalf("%s is not a child of %s", path, filepath.Dir(path))
	return nil
}

// TestTruncatedHolderScanKeepsTheScratchDirArm is the FAIL-CLOSED half of this
// arm's liveness veto: when the ONE /proc pass that decided the arm did not
// finish, the arm keeps what it could not check.
//
// THE DEFECT IT PINS (measured 2026-10-14). The declared-tree walk handed its
// hook an INDEXED scan (heldSet) and nothing else: the index can say "this
// spelling is held", but it cannot say whether the pass that produced it looked
// at the WHOLE process table. So a scan that stopped at its bound — the tail of
// the table unvisited — read exactly like a scan that found nobody, and the arm
// deleted on it. Every other arm of this package refuses on that flag
// (unheldBatch, profileResidueReason, judgeSandbox, worktreeStubHeld); this one
// never asked, because the fact was not in the value it was handed.
//
// THE TWO SIDES, in one fixture, so neither half can pass vacuously: the SAME
// aged directory inside the SAME forgotten container is a candidate under a
// COMPLETE pass and is kept under a TRUNCATED one. The container arm and the
// shared per-entry predicate are asserted separately, because both are delete
// paths and a fix in one is not a fix in the other.
func TestTruncatedHolderScanKeepsTheScratchDirArm(t *testing.T) {
	root, tree := scratchDirFixtureRoot(t)
	orphan := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan))
	dead, _ := seedScratchDir(t, filepath.Join(orphan, scratchDirFixtureDeadName))
	agePath(t, orphan, scratchDirFixtureAge)
	agePath(t, dead, scratchDirFixtureAge)
	now := time.Now()

	complete := heldTreeScan{index: heldSet(HolderScan{}, root), scan: HolderScan{}}
	truncated := heldTreeScan{index: heldSet(HolderScan{Truncated: true}, root), scan: HolderScan{Truncated: true}}

	// ① THE CONTROL, first: under a pass that FINISHED and saw nobody, this
	// directory is a candidate. Without this the second half proves nothing.
	if cand := containerScratchDirEntries(tree, dead, orphan, now, scratchDirFixtureWindow, complete); len(cand) != 1 {
		t.Fatalf("the control failed: an aged directory in a forgotten container is not a candidate under a COMPLETE scan (%d candidates) — the truncated half below would pass vacuously", len(cand))
	}

	// ② The hook: the SAME directory, judged by a pass that stopped early.
	if cand := containerScratchDirEntries(tree, dead, orphan, now, scratchDirFixtureWindow, truncated); len(cand) != 0 {
		t.Fatalf("a TRUNCATED holder pass produced the candidate %v — 'not scanned' was read as 'not held': the tail of the process table may hold this very directory", cand)
	}

	// ③ The container arm, which is the other delete path over the same fixture
	// (it plans the container's own candidates, and the container once empty).
	if got := containerEntries(tree, orphan, dirEntryFor(t, orphan), now, scratchDirFixtureWindow, truncated); len(got) != 0 {
		t.Fatalf("containerEntries planned %v under a TRUNCATED holder pass: the arm's children were judged on a scan that never finished", got)
	}

	// ④ The shared per-entry predicate the generic walk applies (the other tree
	// kinds): same rule, same answer.
	if declaredEntryCollectable(tree, dead, now, scratchDirFixtureWindow, truncated) {
		t.Fatalf("declaredEntryCollectable authorised %s under a TRUNCATED holder pass — the fail-closed clause is missing on the generic walk", dead)
	}
	if !declaredEntryCollectable(tree, dead, now, scratchDirFixtureWindow, complete) {
		t.Fatalf("%s is not collectable under a COMPLETE scan either: the fixture is not the shape this test claims (control failed)", dead)
	}

	// ⑤ THE UNMEASURABLE CONTAINER: a container this walk cannot LIST (mode 000)
	// is kept, and the keep is REPORTED. The predicate half is the pre-existing
	// Rule-15 rule; what this leg pins is that the walk answers it at all — and
	// that the answer is observable, because a container the sweep silently
	// dropped was indistinguishable from one that is not this tree's shape.
	if os.Geteuid() != 0 { // root ignores the mode bits, so the fixture would not be unreadable
		unreadable := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan+"-unreadable"))
		agePath(t, unreadable, scratchDirFixtureAge)
		if err := os.Chmod(unreadable, 0); err != nil {
			t.Fatalf("chmod 000 %s: %v", unreadable, err)
		}
		t.Cleanup(func() { _ = os.Chmod(unreadable, DirPerm) }) //stylegate:ignore restore of the mode this test set, so t.TempDir can clean up
		// The keep is OBSERVED on stderr, not only asserted as a verdict: a
		// silent keep is the Rule-15 half this change adds, and a test that only
		// read the return value would pass with the report line deleted.
		r, w, perr := os.Pipe()
		if perr != nil {
			t.Fatalf("pipe: %v", perr)
		}
		prevErr := os.Stderr
		os.Stderr = w
		got := containerEntries(tree, unreadable, dirEntryFor(t, unreadable), now, scratchDirFixtureWindow, complete)
		os.Stderr = prevErr
		if cerr := w.Close(); cerr != nil {
			t.Fatalf("close pipe: %v", cerr)
		}
		said, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		if len(got) != 0 {
			t.Fatalf("containerEntries planned %v from a container it could not list: an unreadable container is unmeasured, never emptied", got)
		}
		if !strings.Contains(string(said), EntryKeepUnmeasurable) || !strings.Contains(string(said), unreadable) {
			t.Fatalf("the unmeasurable container was kept SILENTLY: stderr=%q, want the keep and the path (%s)", string(said), EntryKeepUnmeasurable)
		}
		if _, err := os.Stat(unreadable); err != nil {
			t.Fatalf("the unreadable container was removed: %v", err)
		}
	}
}
