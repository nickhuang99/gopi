package tools

// Dead-holder RESUME decision tests (epic dead-holder-resume, 2026-09-16).
//
// What is proven HERE is the DECISION — the part that must be deterministic
// and that no LLM may influence: given a row's recorded process triple and the
// live process table, is this worktree's holder alive (leave it alone), gone
// (resume the run), or unknowable (do nothing)? Every branch is exercised with
// REAL processes (/proc is the evidence; a fabricated pid would prove nothing),
// and the two dangerous directions get their own tests:
//
//   - a LIVE holder must never be judged dead (that would double-run a run in
//     flight — the P0 the singleton guard exists for);
//   - an UNREADABLE probe must never be judged dead (Rule 15; the fail-open
//     precedent deleted a live session's worktree).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// probeOfLiveProcess records the identity of a real, living process the way
// the ledger does — the triple read from /proc by the SAME capture the writers
// use (taskCaptureProcIdentity), so the fixture cannot drift from production.
func probeOfLiveProcess(t *testing.T, pid int) procIdentity {
	t.Helper()
	ident, ok := taskCaptureProcIdentity(pid)
	if !ok {
		t.Fatalf("cannot capture the identity of live pid %d", pid)
	}
	return procIdentity{Pid: ident.Pid, StartTicks: ident.Start, Cmd: ident.Cmd}
}

// TestHolderProbeJudgesLiveProcessLive: a recorded triple that matches a real
// living process is LIVE — the branch stays protected and nothing is resumed.
func TestHolderProbeJudgesLiveProcessLive(t *testing.T) {
	mock := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	defer mock.Reap()
	rec := probeOfLiveProcess(t, mock.Pid())

	var resumed bool
	oc, judged := resumeDeadHolderRow("", deadHolderResumeRow{
		TaskID: "t-live", SessionID: "s-live", Status: taskStatusInProgress,
		ClaimedBy: claimHolderOfPid(mock.Pid()), Ident: rec,
	}, t.TempDir())
	if judged {
		resumed = oc.Resumed
	}
	if judged || resumed {
		t.Fatalf("a LIVE recorded holder must be judged live (no outcome, no resume); got judged=%v outcome=%+v", judged, oc)
	}
	if got := (holderProbe{ident: rec}).resolve(); got != holderProbeLive {
		t.Fatalf("verdict for a live process = %v, want live", got)
	}
}

// TestHolderProbeJudgesRecycledPidDead is the MUTATION-STYLE red-proof anchor:
// the SAME pid with a DIFFERENT start tick (the shape the OS produces when it
// recycles the number) must be DEAD. Break the start-tick comparison in the
// probe and this test fails — which is what makes the comparison load-bearing
// rather than decorative.
func TestHolderProbeJudgesRecycledPidDead(t *testing.T) {
	mock := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	defer mock.Reap()
	rec := probeOfLiveProcess(t, mock.Pid())
	rec.StartTicks++ // a different incarnation of the same number

	if got := (holderProbe{ident: rec}).resolve(); got != holderProbeDead {
		t.Fatalf("verdict for a recycled pid = %v, want dead (a live process under a recycled number is NOT the recorded holder)", got)
	}
}

// TestHolderProbeJudgesVanishedPidDead: /proc/<pid> is gone — the positive
// death proof this whole arm rests on.
func TestHolderProbeJudgesVanishedPidDead(t *testing.T) {
	dead := testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	rec := procIdentity{Pid: dead, StartTicks: 12345, Cmd: "sleep 300"}
	if got := (holderProbe{ident: rec}).resolve(); got != holderProbeDead {
		t.Fatalf("verdict for a vanished pid = %v, want dead", got)
	}
}

// TestHolderProbeNoIdentityWithoutADeadClaimPidIsUnreadable is the RULE-15
// test at the decision layer: a row that names a holder textually but recorded
// no triple, where this host CANNOT prove the claim pid is gone, must be
// UNREADABLE — never dead. An unreadable answer must not authorise a resume
// (that resume would race a holder this host simply cannot see).
func TestHolderProbeNoIdentityWithoutADeadClaimPidIsUnreadable(t *testing.T) {
	// A pid that no process can own on this host (above the kernel's pid_max
	// ceiling is not portable, so use a claim stamp that does not parse as a
	// LOCAL pid at all — a foreign host's stamp is the real-world shape).
	ident := procIdentity{Pid: 0, StartTicks: 0, Cmd: ""}
	if got := (holderProbe{ident: ident}).resolve(); got != holderProbeUnreadable {
		t.Fatalf("verdict for a holder with no identity and no provably-dead claim pid = %v, want unreadable", got)
	}
	// And the resume must NOT fire on that verdict.
	oc, judged := resumeDeadHolderRow("", deadHolderResumeRow{
		TaskID: "t-unreadable", Status: taskStatusInProgress, ClaimedBy: "somewhere-else:4242",
	}, t.TempDir())
	if !judged {
		t.Fatalf("an unreadable holder must still be REPORTED (an operator needs to see why nothing happened)")
	}
	if oc.Resumed {
		t.Fatalf("an unreadable verdict must never resume: %+v", oc)
	}
	if oc.Verdict != holderProbeUnreadable.String() {
		t.Fatalf("verdict = %q, want %q", oc.Verdict, holderProbeUnreadable)
	}
}

// TestHolderProbeCwdResolvesMultiRowAmbiguity is the SECONDARY-evidence test:
// the triple matches a live process, but that process stands in a DIFFERENT
// worktree — the multi-row ambiguity (two rows, one process, LIKE-matched
// worktree paths). The recorded holder for THIS worktree is not that process,
// so the branch is not held by it.
func TestHolderProbeCwdResolvesMultiRowAmbiguity(t *testing.T) {
	// A real process standing in a directory we control.
	dir := t.TempDir()
	// The cwd IS this fixture's subject, so it passes Dir explicitly; the
	// spawn still goes through the helper so the child carries Pdeathsig and
	// cannot outlive this test binary (measured leak: task 01a179d7).
	childMP := testutil.StartMockProcessCmd(t, func() *exec.Cmd {
		c := exec.Command("sh", "-c", "sleep 300")
		c.Dir = dir
		return c
	}())
	child := childMP.Cmd()
	rec := probeOfLiveProcess(t, child.Process.Pid)

	// Positive direction: the process IS standing in this worktree → live.
	if got := (holderProbe{ident: rec, worktree: dir}).resolve(); got != holderProbeLive {
		t.Fatalf("a process standing IN the worktree must be live, got %v", got)
	}
	// The ambiguity: the SAME live identity, judged against ANOTHER install's
	// copy of the path (the second row) → the cwd cross-check must NOT claim
	// occupancy of that other tree.
	other := t.TempDir()
	if got := (holderProbe{ident: rec, worktree: other}).resolve(); got == holderProbeDead {
		t.Fatalf("the cwd cross-check must never turn a live process into a death proof (got %v)", got)
	}
}

// TestDeadHolderResumeCapStopsWithANamedReason: at the attempt cap the arm must
// STOP and record a reason — never restart the row forever (the anti-loop
// requirement).
func TestDeadHolderResumeCapStopsWithANamedReason(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "cap fixture", Payload: "cap", TaskID: "t-cap", Source: "human",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dead := testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	if err := TaskStampHolder("t-cap", claimHolderOfPid(dead), dead); err == nil {
		t.Fatalf("stamping a dead pid must fail (no readable identity) — the fixture needs the pid to be gone")
	}
	if _, err := TaskUpdateReason("t-cap", taskStatusInProgress, "", ""); err != nil {
		t.Fatalf("advance to in_progress: %v", err)
	}
	for i := 1; i <= deadHolderResumeMaxAttempts; i++ {
		if err := noteDeadHolderResumeAttempt("t-cap", i); err != nil {
			t.Fatalf("note attempt %d: %v", i, err)
		}
	}
	wts := t.TempDir()
	wtJSON := `{"repo":"` + t.TempDir() + `","` + DeliverableWorktreeKey + `":"` + wts + `","branch":"feat/cap"}`
	oc, judged := resumeDeadHolderRow("", deadHolderResumeRow{
		TaskID: "t-cap", Status: taskStatusInProgress, Deliverable: wtJSON,
		ClaimedBy: claimHolderOfPid(dead),
		Ident:     procIdentity{Pid: dead, StartTicks: 4242, Cmd: "sleep 300"},
		Attempts:  deadHolderResumeMaxAttempts,
	}, wts)
	if !judged {
		t.Fatalf("a capped row must still be REPORTED")
	}
	if oc.Resumed {
		t.Fatalf("the arm must NOT resume past the cap: %+v", oc)
	}
	if !strings.Contains(oc.Reason, "cap") {
		t.Fatalf("the cap outcome must name the cap, got %q", oc.Reason)
	}
	// The row is LEFT FAILED with that reason (an operator decision, not an
	// infinite restart loop).
	st, err := TaskShow("t-cap")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(st, taskStatusFailed) {
		t.Fatalf("the capped row must be failed with the reason, got:\n%s", st)
	}
	if !strings.Contains(st, "resume attempt cap") {
		t.Fatalf("the row's reason must carry the cap text, got:\n%s", st)
	}
}

// TestDeadHolderResumeAttemptTagIsAppendOnlyAndCounted: the counter is read
// back from the row's tags, and re-recording the same ordinal is idempotent
// (so a retried pass cannot inflate the count past the cap by accident).
func TestDeadHolderResumeAttemptTagIsAppendOnlyAndCounted(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "tags fixture", Payload: "tags", TaskID: "t-tags", Source: "human",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := noteDeadHolderResumeAttempt("t-tags", 1); err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := noteDeadHolderResumeAttempt("t-tags", 1); err != nil {
		t.Fatalf("note again: %v", err)
	}
	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, r := range rows {
		if r.TaskID == "t-tags" && r.Attempts != 1 {
			t.Fatalf("re-recording ordinal 1 must be idempotent, got Attempts=%d", r.Attempts)
		}
	}
}

// TestDeadHolderResumeRowsScopeSkipsTerminalRows: the candidate set is bounded
// to statuses that can carry a live holder — a FINISHED row's worktree is
// reclaim's business, not recovery's (resuming it would restart completed work).
func TestDeadHolderResumeRowsScopeSkipsTerminalRows(t *testing.T) {
	testutil.NewTestDBPath(t)
	wt := t.TempDir()
	repo := t.TempDir()
	for _, c := range []struct{ id, status string }{
		{"t-scope-terminal", taskStatusCancelled},
		{"t-scope-live", taskStatusInProgress},
	} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "scope " + c.id, Payload: "scope", TaskID: c.id, Source: "human",
		}); err != nil {
			t.Fatalf("seed %s: %v", c.id, err)
		}
		// The deliverable is written directly (the shared fixture helper):
		// TaskSubmit stamps the REPO from the process cwd, and this fixture
		// needs a worktree path of its own.
		seedDeliverable(t, c.id, repo, "feat/scope", wt)
		if _, err := TaskUpdateReason(c.id, c.status, "", ""); err != nil {
			t.Fatalf("status %s: %v", c.id, err)
		}
	}
	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	var sawLive, sawTerminal bool
	for _, r := range rows {
		switch r.TaskID {
		case "t-scope-live":
			sawLive = true
		case "t-scope-terminal":
			sawTerminal = true
		}
	}
	if !sawLive {
		t.Errorf("a live-status worktree row must be a candidate")
	}
	if sawTerminal {
		t.Errorf("a TERMINAL row must not be a resume candidate (its run is over; the reclaim arm owns its directory)")
	}
}

// TestSameWorktreePathToleratesSymlinkedAndNestedShapes pins the path
// comparison the cwd cross-check rests on: an exact path, a nested subdirectory
// and a symlinked shape all count as standing IN the tree, while a sibling
// directory does not (a false positive here would report a stranger as the
// holder and suppress a legitimate resume).
func TestSameWorktreePathToleratesSymlinkedAndNestedShapes(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(filepath.Join(wt, "pkg", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(wt, link); err != nil {
		t.Skipf("cannot create the symlink fixture: %v", err)
	}
	sibling := filepath.Join(base, "sibling")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, cwd, tree string
		want            bool
	}{
		{"exact", wt, wt, true},
		{"nested", filepath.Join(wt, "pkg", "deep"), wt, true},
		{"symlinked", wt, link, true},
		{"symlinked-reverse", link, wt, true},
		{"sibling", sibling, wt, false},
		{"prefix-collision", wt + "-other", wt, false},
		{"empty", "", wt, false},
	}
	for _, c := range cases {
		if got := sameWorktreePath(c.cwd, c.tree); got != c.want {
			t.Errorf("%s: sameWorktreePath(%q, %q) = %v, want %v", c.name, c.cwd, c.tree, got, c.want)
		}
	}
}

// TestDeadHolderResumeOutcomeLineCarriesTheEvidence pins the ONE renderer: an
// operator reading the merger log must see WHICH session continues, how many
// attempts were spent, and — when nothing happened — WHY.
func TestDeadHolderResumeOutcomeLineCarriesTheEvidence(t *testing.T) {
	yes := deadHolderResumeOutcome{
		TaskID: "t-1", Worktree: "/w/t-1", SessionID: "sess-9", Verdict: "dead",
		Resumed: true, Attempts: 1, Action: "gopi session resume sess-9 --task t-1", LogPath: "/l/x.log",
	}
	line := yes.Line()
	for _, want := range []string{"t-1", "sess-9", "1/" + strconv.Itoa(deadHolderResumeMaxAttempts), "gopi session resume", "/l/x.log"} {
		if !strings.Contains(line, want) {
			t.Errorf("resume line must carry %q:\n%s", want, line)
		}
	}
	no := deadHolderResumeOutcome{
		TaskID: "t-2", Worktree: "/w/t-2", Verdict: "unreadable", Attempts: 0,
		Reason: "holder liveness unreadable on this host",
	}
	line = no.Line()
	for _, want := range []string{"t-2", "/w/t-2", "not resumed", "unreadable", "0/" + strconv.Itoa(deadHolderResumeMaxAttempts)} {
		if !strings.Contains(line, want) {
			t.Errorf("non-resume line must carry %q:\n%s", want, line)
		}
	}
}
