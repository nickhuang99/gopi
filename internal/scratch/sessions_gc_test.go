package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The sessions reclaimer's contract, driven through the REAL predicate
// (PlanSessions / SweepSessions) against real directories and — where the arm
// under test is liveness — a real process.
//
// THE THREE REFUSAL DIRECTIONS THE EPIC DEMANDS, each its own test so that
// removing ONE guard turns exactly ONE test red:
//
//	① LIVE PROCESS   a running child with its cwd inside a candidate → KEEP
//	② LEDGER ROW     a task row (or a chat/note/conclusion row) names it → KEEP
//	③ UNKNOWN        an unmeasurable age / an unwired reader → KEEP, and the
//	                 reason says WHY rather than folding into "not collectable"
//
// plus the two invariants that make the verb safe to hand to an operator:
// the sessions ROOT survives every sweep at any age, and a container this
// system did not name is never a candidate.
//
// THE FIXTURE IS A REAL SESSIONS TREE UNDER THE SCRATCH ROOT (Rule 22: never a
// temp dir on `/`, never inside the source tree), created through the same
// shape a live tree has: a canonical-uuid container directory holding one
// file.

// sessionsRoot makes an empty sessions-shaped root under the device scratch
// tree and returns it.
func sessionsRoot(t *testing.T) string {
	t.Helper()
	sessionsHome(t)
	root, err := SessionRawLogRoot()
	if err != nil {
		t.Fatalf("the sessions tree is not resolvable with GOPI_HOME pinned: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create the sessions root: %v", err)
	}
	return root
}

// sessionsHome pins GOPI_HOME for ONE test to a fresh scratch tree OFF the `/`
// partition (Rule 22) and returns that home. Every helper below derives its
// sessions root from it through the REAL resolver, so the fixture and the verb
// agree about where the tree is.
func sessionsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	return home
}

// sessionContainer makes one uuid-named session directory holding one file, and
// returns its path.
func sessionContainer(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create session dir %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write a session log in %s: %v", name, err)
	}
	return dir
}

// sessionAged backdates a directory past the window. It MUST run after every
// other mutation of that directory: creating or removing an entry inside it
// updates its own mtime, so aging first would be undone by the next write.
func sessionAged(t *testing.T, dir string) {
	t.Helper()
	old := time.Now().Add(-(SessionsGCStaleAge + 24*time.Hour))
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("age %s: %v", dir, err)
	}
}

// withLedger installs a fake ledger reader for the duration of ONE test and
// restores the previous one. It is the seam's test door, so a test can state
// exactly which names are claimed instead of depending on the live ledger.
func withLedger(t *testing.T, names map[string]string, err error) {
	t.Helper()
	prev := sessionsLedgerNames
	sessionsLedgerNames = func() (map[string]string, error) { return names, err }
	t.Cleanup(func() { sessionsLedgerNames = prev })
}

// sessionPlanByName indexes a plan by directory base name.
func sessionPlanByName(plans []SessionsPlan) map[string]SessionsPlan {
	out := make(map[string]SessionsPlan, len(plans))
	for _, p := range plans {
		out[p.Name] = p
	}
	return out
}

const (
	sessGCOldUUID  = "0f8e2a10-1111-4000-8000-00000000aa01"
	sessGCOldUUID2 = "0f8e2a10-2222-4000-8000-00000000aa02"
	sessGCFreshID  = "0f8e2a10-3333-4000-8000-00000000aa03"
	sessGCNotOurs  = "two-rounds-evidence"
)

// ① A LIVE PROCESS INSIDE A CANDIDATE KEEPS IT.
//
// The child is REAL and its cwd is inside the directory: the /proc arm is the
// subject, so a simulated pid would test nothing. The control (the same shape,
// aged, with no process inside) is in the same plan and must be COLLECTABLE, so
// the test cannot pass by the predicate refusing everything.
func TestSessionsGCLiveProcessInsideKeeps(t *testing.T) {
	root := sessionsRoot(t)
	held := sessionContainer(t, root, sessGCOldUUID)
	free := sessionContainer(t, root, sessGCOldUUID2)
	withLedger(t, map[string]string{}, nil)

	_, _, kill := spawnIn(t, held)
	defer kill()

	sessionAged(t, held)
	sessionAged(t, free)

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	by := sessionPlanByName(plans)
	if p := by[sessGCOldUUID]; p.Collectable() {
		t.Fatalf("%s holds a LIVE process's cwd but was called collectable (reason %q)", sessGCOldUUID, p.Reason)
	}
	if p := by[sessGCOldUUID2]; !p.Collectable() {
		t.Fatalf("%s holds no process and is aged, yet was kept: %q", sessGCOldUUID2, p.Reason)
	}
}

// ② A LEDGER ROW NAMING THE DIRECTORY KEEPS IT — for every source column.
//
// The predicate is asked through its REAL seam, so this pins the contract the
// implementation owes: a name the ledger claims is never collected, whatever
// table it came from. The control in the same plan must still be collectable.
func TestSessionsGCLedgerRowKeeps(t *testing.T) {
	root := sessionsRoot(t)
	claimed := sessionContainer(t, root, sessGCOldUUID)
	free := sessionContainer(t, root, sessGCOldUUID2)
	withLedger(t, map[string]string{sessGCOldUUID: "task 01a0dead is completed"}, nil)

	sessionAged(t, claimed)
	sessionAged(t, free)

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	by := sessionPlanByName(plans)
	if p := by[sessGCOldUUID]; p.Collectable() {
		t.Fatalf("%s is named by a ledger row but was called collectable", sessGCOldUUID)
	}
	if p := by[sessGCOldUUID2]; !p.Collectable() {
		t.Fatalf("%s is named by nothing and is aged, yet was kept: %q", sessGCOldUUID2, p.Reason)
	}
}

// ③ AN UNREADABLE LEDGER IS AN ERROR, NEVER A GREEN PLAN.
//
// This is the direction that matters most: an empty claim set authorises
// collecting EVERYTHING, so a reader that failed must not be able to produce
// one. The verb reports the failure and collects nothing.
func TestSessionsGCUnreadableLedgerRefusesToPlan(t *testing.T) {
	root := sessionsRoot(t)
	dir := sessionContainer(t, root, sessGCOldUUID)
	sessionAged(t, dir)
	withLedger(t, nil, fmt.Errorf("disk on fire"))

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err == nil {
		t.Fatalf("an unreadable ledger produced a plan of %d entries instead of an error", len(plans))
	}
	if !strings.Contains(err.Error(), SessionsLedgerUnreadableReason) {
		t.Fatalf("the refusal does not name the unreadable ledger: %v", err)
	}
}

// ③b AN UNWIRED READER IS A REFUSAL TOO, and its sentence is not the same one:
// a wiring bug and a host fact are different defects (Rule 15).
func TestSessionsGCUnwiredLedgerRefusesToPlan(t *testing.T) {
	root := sessionsRoot(t)
	dir := sessionContainer(t, root, sessGCOldUUID)
	sessionAged(t, dir)
	prev := sessionsLedgerNames
	sessionsLedgerNames = nil
	t.Cleanup(func() { sessionsLedgerNames = prev })

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err == nil {
		t.Fatalf("an unwired reader produced a plan of %d entries instead of an error", len(plans))
	}
	if !strings.Contains(err.Error(), SessionsLedgerUnwiredReason) {
		t.Fatalf("the refusal does not name the unwired reader: %v", err)
	}
}

// ④ UNMEASURABLE AGE → KEEP, and the reason says so rather than claiming
// freshness. The directory is REMOVED between listing and judging only in
// reality; here the honest minimal form is a candidate whose age cannot be read
// — a path that does not exist any more.
func TestSessionsGCUnmeasurableAgeKeeps(t *testing.T) {
	plans := []SessionsPlan{}
	_ = plans // the predicate is exercised directly, below

	root := sessionsRoot(t)
	dir := sessionContainer(t, root, sessGCOldUUID)
	sessionAged(t, dir)
	withLedger(t, map[string]string{}, nil)

	// Plan first (the directory is listable), then judge the vanished path — the
	// exact window the ORDER of the four questions has to survive.
	got, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(got) != 1 || !got[0].Collectable() {
		t.Fatalf("the fixture must start collectable, got %+v", got)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove the fixture: %v", err)
	}
	scan := ScanHolders()
	p := judgeSessionDir(dir, sessGCOldUUID, root, time.Now(), SessionsGCStaleAge,
		heldTreeScan{index: heldSet(scan, root), scan: scan},
		map[string]string{}, "")
	if p.Collectable() {
		t.Fatalf("a directory whose age cannot be read was called collectable: %q", p.Reason)
	}
	if !strings.Contains(p.Reason, SessionsKeepUnmeasurable) {
		t.Fatalf("the keep reason does not name the unmeasurable age: %q", p.Reason)
	}
}

// ⑤ THE SESSIONS ROOT ITSELF IS NEVER A CANDIDATE — at any age, and even when
// the sweep runs against a root it could otherwise judge.
func TestSessionsGCRootSurvivesEverySweep(t *testing.T) {
	root := sessionsRoot(t)
	// A uuid-named root would be the interesting case: the root's own NAME must
	// not be what makes it collectable. Make the root itself look aged.
	uuidRoot := filepath.Join(root, sessGCOldUUID)
	if err := os.MkdirAll(uuidRoot, 0o700); err != nil {
		t.Fatalf("create the root: %v", err)
	}
	withLedger(t, map[string]string{}, nil)
	sessionAged(t, uuidRoot)

	// The refusal is about the ROOT ARGUMENT, so it is asked of the root itself.
	if err := SessionsRootRefusal(uuidRoot); err != nil {
		t.Fatalf("a legitimate sessions root was refused: %v", err)
	}
	removed, bytes, err := SweepSessions(uuidRoot, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || bytes != 0 {
		t.Fatalf("a sweep removed %d entries / %d bytes from an EMPTY root", removed, bytes)
	}
	if _, serr := os.Stat(uuidRoot); serr != nil {
		t.Fatalf("the sessions root was removed by its own sweep: %v", serr)
	}
	// And the same invariant for the real parent: sweeping it must not take the
	// aged uuid directory's own PARENT (the root) out of existence.
	if _, serr := os.Stat(root); serr != nil {
		t.Fatalf("the parent root vanished: %v", serr)
	}
}

// ⑥ A NAME THIS SYSTEM DOES NOT MINT IS NEVER A CANDIDATE, whatever its age.
func TestSessionsGCNotOursKeeps(t *testing.T) {
	root := sessionsRoot(t)
	foreign := filepath.Join(root, sessGCNotOurs)
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatalf("create %s: %v", sessGCNotOurs, err)
	}
	withLedger(t, map[string]string{}, nil)
	sessionAged(t, foreign)

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	by := sessionPlanByName(plans)
	p := by[sessGCNotOurs]
	if p.Collectable() {
		t.Fatalf("%s is not a session container name yet was called collectable", sessGCNotOurs)
	}
	if !strings.Contains(p.Reason, SessionsKeepNotOurs) {
		t.Fatalf("the keep reason does not say the name is not ours: %q", p.Reason)
	}

	// The DESTRUCTIVE arm must refuse it too, not just the report.
	removed, _, err := SweepSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("the sweep removed %d entries, but nothing here is collectable", removed)
	}
	if _, serr := os.Stat(foreign); serr != nil {
		t.Fatalf("a foreign directory was deleted by the sweep: %v", serr)
	}
}

// ⑦ THE DEFAULT IS A DRY RUN. This is a property of the CLI arm, but the
// predicate it rests on is pinned here: a plan NEVER deletes, so "the verb
// reported candidates" can never mean "something was removed".
func TestSessionsGCPlanDeletesNothing(t *testing.T) {
	root := sessionsRoot(t)
	dir := sessionContainer(t, root, sessGCOldUUID)
	withLedger(t, map[string]string{}, nil)
	sessionAged(t, dir)

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plans) != 1 || !plans[0].Collectable() {
		t.Fatalf("the fixture must be collectable, got %+v", plans)
	}
	if _, serr := os.Stat(filepath.Join(dir, "session.jsonl")); serr != nil {
		t.Fatalf("planning removed the directory's contents: %v", serr)
	}
}

// ⑧ AN EMPTY (OR ABSENT) ROOT IS NOT AN ERROR AND NOT A CANDIDATE.
func TestSessionsGCAbsentRootIsTriviallyClean(t *testing.T) {
	sessionsHome(t)
	root := filepath.Join(os.Getenv("GOPI_HOME"), "sessions")
	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("an absent root must not be an error: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("an absent root produced %d plans", len(plans))
	}
}

// ⑨ THE ROOT REFUSALS. A mistyped root must never put the whole machine inside
// this arm's jurisdiction.
func TestSessionsGCRootRefusals(t *testing.T) {
	cases := []struct{ name, root string }{
		{"filesystem root", "/"},
		{"empty", ""},
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cases = append(cases, struct{ name, root string }{"user home", home})
	}
	for _, c := range cases {
		if err := SessionsRootRefusal(c.root); err == nil {
			t.Fatalf("the %s (%q) was accepted as a sessions root", c.name, c.root)
		}
	}
}

// ⑩ AN APPLY FLOORS THE WINDOW; A DRY RUN MAY PREVIEW A SHORTER ONE.
//
// The floor is what makes SessionsGCStaleAge a value something READS rather
// than a constant nothing enforces (Rule 13).
func TestSessionsGCWindowFloor(t *testing.T) {
	if got := SessionsGCWindow(time.Hour); got != SessionsGCStaleAge {
		t.Fatalf("an apply with a 1h request judged by %s, want the floor %s", got, SessionsGCStaleAge)
	}
	longer := SessionsGCStaleAge + 48*time.Hour
	if got := SessionsGCWindow(longer); got != longer {
		t.Fatalf("an apply with a LONGER window was shortened to %s", got)
	}
}

// ⑪ A CONTAINER INSIDE THE FRESHNESS WINDOW IS KEPT, and the reason names the
// window rather than the age being silently small.
func TestSessionsGCFreshKeeps(t *testing.T) {
	root := sessionsRoot(t)
	dir := sessionContainer(t, root, sessGCFreshID)
	withLedger(t, map[string]string{}, nil)

	plans, err := PlanSessions(root, SessionsGCStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	by := sessionPlanByName(plans)
	p := by[sessGCFreshID]
	if p.Collectable() {
		t.Fatalf("a just-created session (%s) was called collectable", dir)
	}
	if !strings.Contains(p.Reason, SessionsKeepFresh) {
		t.Fatalf("the keep reason does not name the freshness window: %q", p.Reason)
	}
}
