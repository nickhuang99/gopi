package tools

// Run-ground anchor tests (epic worktree-reclaim-two-gaps, Gap B, 2026-09-23).
//
// Two directions, and both are the brief's acceptance:
//
//	PRODUCER  the anchor is OBSERVED in the sink at ground time — for a TASK
//	          run (through the real grounding entry) and for the BARE-PROMPT
//	          face (a row minted by the harness hook, no wrapper env), which is
//	          the run that had NO anchor at all before this change.
//	SWEEPER   a FINISHED rowless run's ground is collectable and is collected,
//	          while a LIVE run's ground is kept — a dry-run read-out showing
//	          both, and an apply showing the finished one actually released and
//	          the live one untouched.
import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// askeProbeMarker identifies the re-exec'd asker-probe child (see
// TestGroundLivenessDoesNotCountTheAskerAsAHolder).
const askeProbeMarker = "GOPI_RUN_GROUND_ASKER_PROBE"

// anchorSink points the audit sink (and therefore the anchors) at a file this
// test owns, so the read-back is the test's own and never the operator's log.
func anchorSink(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), removalaudit.FileName)
	t.Setenv(removalaudit.EnvSink, path)
	return path
}

// anchorsIn reads the anchors back from a sink and fails on a read error that
// is not "no file yet".
func anchorsIn(t *testing.T, path string) []RunGroundAnchor {
	t.Helper()
	got, err := RunGroundAnchors(path)
	if err != nil {
		t.Fatalf("RunGroundAnchors(%s): %v", path, err)
	}
	return got
}

// TestGroundAnchorWrittenAtGroundTimeForTaskRun is the producer half for a TASK
// run: the real grounding entry (TaskRunGroundFor — the same one the executor
// spawn seam calls) must leave an anchor naming the directory it took, with the
// taker's own identity.
func TestGroundAnchorWrittenAtGroundTimeForTaskRun(t *testing.T) {
	sink := anchorSink(t)
	id, repo, root := groundFixture(t, "01a11500-0000-7000-8000-000000000006", "anchored-repo")

	res, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("TaskRunGroundFor(%s): %v", id, err)
	}
	if res.Dir == "" {
		t.Fatalf("the ground did not resolve to a directory: %+v", res)
	}
	if !strings.HasPrefix(res.Dir, root) {
		t.Fatalf("the ground landed outside the pinned root: %s (root %s)", res.Dir, root)
	}

	anchors := anchorsIn(t, sink)
	if len(anchors) == 0 {
		t.Fatalf("no run-ground anchor was written for %s (sink %s)", res.Dir, sink)
	}
	a := anchors[len(anchors)-1]
	if !sameWorkingPath(a.Worktree, res.Dir) {
		t.Errorf("the anchor names %q, want the ground %q", a.Worktree, res.Dir)
	}
	if a.TaskID != id {
		t.Errorf("the anchor names row %q, want %q", a.TaskID, id)
	}
	if a.Repo != repo && !sameWorkingPath(a.Repo, repo) {
		t.Errorf("the anchor names repo %q, want %q", a.Repo, repo)
	}
	// The identity is the TAKER's, read live: these must be real values, and the
	// incarnation is what makes a recycled pid unable to impersonate the run.
	if a.PID != strconv.Itoa(os.Getpid()) {
		t.Errorf("the anchor's pid is %q, want this process %d", a.PID, os.Getpid())
	}
	if a.StartTicks == "" || a.StartTicks == runGroundAnchorUnrecorded {
		t.Errorf("the anchor records no incarnation (start-ticks=%q) — a recycled pid could impersonate it", a.StartTicks)
	}
	if a.Session == "" || a.Session == runGroundAnchorUnrecorded {
		t.Errorf("the anchor records no session (session=%q)", a.Session)
	}
	if a.At.IsZero() {
		t.Errorf("the anchor carries no parseable timestamp: %+v", a)
	}
	// One line per GROUND: the seam fires per candidate probe, so a second
	// resolution of the same row must not append a second anchor for one
	// directory.
	if _, err := TaskRunGroundFor(id); err != nil {
		t.Fatalf("second TaskRunGroundFor: %v", err)
	}
	if again := anchorsIn(t, sink); len(again) != len(anchors) {
		t.Errorf("a re-resolution appended another anchor for one ground: %d → %d", len(anchors), len(again))
	}
}

// TestGroundAnchorWrittenForBarePromptRun is the producer half for the BARE
// PROMPT face — the run that had no anchor at all. That run carries a row but
// no wrapper env (the harness minted the row itself), and it takes its ground
// by an ordinary git command, so the seam is the creation-time detector, not
// the executor's spawn path.
//
// The detection here is deliberately the REAL command reader: the anchor must
// be written from what the command line says the run did, with no env help.
func TestGroundAnchorWrittenForBarePromptRun(t *testing.T) {
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := groundTestRepo(t, "bare-prompt-repo")
	const row = "01a0anchor-bare1"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "bare prompt row", Payload: "work", Repo: repo, TaskID: row,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	wt := filepath.Join(root, "gopi-bare-prompt")
	cmdline := "git -C " + repo + " worktree add " + wt + " -b task/" + row
	// The wrapper env is DELIBERATELY absent: this is the face that has no
	// GOPI_TASK_WRAPPER_ID. The caller supplies the row it knows it runs under —
	// exactly what the harness's auto-registration hook holds.
	// env-leak-ok: a bare t.Setenv cannot express ABSENCE, and this face needs
	// the variable unset; SnapshotEnv restores PRESENCE (it captures with
	// os.LookupEnv, so an originally-unset variable comes back unset).
	defer testutil.SnapshotEnv(t, TaskWrapperEnv)()
	if err := os.Unsetenv(TaskWrapperEnv); err != nil { // env-leak-ok: a bare t.Setenv cannot express ABSENCE; SnapshotEnv restores presence above
		t.Fatalf("unset %s: %v", TaskWrapperEnv, err)
	}

	// Perform the creation the command line describes, then drive the detector
	// over that command — the seam the bash/job round calls.
	if out, err := groundTestGit(repo, "worktree", "add", wt, "-b", "task/"+row); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	TaskDeliverableWorktreeBashForRow(row, "", cmdline)

	anchors := anchorsIn(t, sink)
	found := false
	for _, a := range anchors {
		if sameWorkingPath(a.Worktree, wt) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the bare-prompt run's ground %s was not anchored; anchors=%+v", wt, anchors)
	}
}

// TestRunGroundSweepCollectsFinishedRowlessRunAndKeepsLiveWork is the sweeper
// half: both directions in one fixture, so the READ-OUT shows the pair and the
// APPLY proves the finished ground is released while the live one survives.
func TestRunGroundSweepCollectsFinishedRowlessRunAndKeepsLiveWork(t *testing.T) {
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := groundTestRepo(t, "ground-sweep-repo")

	// The FINISHED, ROWLESS run: a ground taken by a process that has since
	// exited. Its row names nothing (a bare-prompt run whose row never recorded
	// the directory) — so the ledger is silent about it, which is precisely the
	// shape no other arm can attribute.
	finished := filepath.Join(root, "gopi-finished-rowless")
	if out, err := groundTestGit(repo, "worktree", "add", finished, "-b", "task/01a0finished"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	// A pid that is dead by MEASUREMENT (started, reaped, confirmed gone), with
	// an incarnation that belongs to no live process.
	dead := deadPidOnThisHost(t)
	writeAnchorRecordForTest(t, sink, repo, finished, "task/01a0finished", "", strconv.Itoa(dead), "999999999", strconv.Itoa(dead))
	// Age the ground past the floor by budging its registration record — the
	// same record the sweep reads its age from.
	if err := os.Chtimes(filepath.Join(finished, ".git"), time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour)); err != nil {
		t.Logf("(the registration record could not be aged: %v — the anchor's own timestamp decides)", err)
	}
	anchorAgeForTest(t, sink, finished, 48*time.Hour)

	// The LIVE run: a real process standing in its own ground right now.
	live := filepath.Join(root, "gopi-live-run")
	if out, err := groundTestGit(repo, "worktree", "add", live, "-b", "task/01alive"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	writeAnchorRecordForTest(t, sink, repo, live, "task/01alive", "", strconv.Itoa(os.Getpid()), testStartTicks(t, os.Getpid()), testSessionKey(t, os.Getpid()))
	anchorAgeForTest(t, sink, live, 48*time.Hour)

	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	t.Logf("read-out:\n  %s", strings.Join(RunGroundSweepReport(collectable, kept), "\n  "))

	if !groundIn(collectable, finished) {
		t.Fatalf("the finished rowless run's ground must be collectable; collectable=%v kept=%v", groundPaths(collectable), groundPaths(kept))
	}
	if groundIn(collectable, live) {
		t.Fatalf("the LIVE run's ground must never be collectable; collectable=%v", groundPaths(collectable))
	}
	if !groundIn(kept, live) {
		t.Fatalf("the live ground must appear among the KEPT, with its reason; kept=%v", groundPaths(kept))
	}
	for _, v := range kept {
		// The sentence names the LAUNCH CONTEXT, not "the run" — a distinction
		// the second gate (defect 01a1151f) made load-bearing: the recorded pid is
		// the claim-time worker, so a keep-reason claiming the RUN is still here
		// would be a false statement on an operator's screen whenever the worker
		// has simply moved on to another row.
		if sameWorkingPath(v.Anchor.Worktree, live) && !strings.Contains(v.Reason, "is still alive") {
			t.Errorf("the live ground's keep-reason must say the launch context is still alive: %q", v.Reason)
		}
	}

	// APPLY: the finished ground is released, the live one survives.
	removed, skipped, cerr := RunGroundCollect(collectable)
	if cerr != nil {
		t.Fatalf("RunGroundCollect: %v", cerr)
	}
	t.Logf("apply: removed=%v skipped=%v", removed, skipped)
	if !containsPath(removed, finished) {
		t.Fatalf("the finished ground was not removed: removed=%v skipped=%v", removed, skipped)
	}
	if _, err := os.Stat(finished); !os.IsNotExist(err) {
		t.Errorf("the finished ground's directory must be gone after apply, stat err=%v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("the LIVE ground must survive the apply: %v", err)
	}
	// The BRANCH is never touched by this arm — it is the merger's input.
	if out, err := groundTestGit(repo, "rev-parse", "--verify", "--quiet", "task/01a0finished"); err != nil {
		t.Errorf("the branch must survive the ground's release: %v\n%s", err, out)
	}
}

// TestRunGroundSweepKeepsAGroundWhoseBranchSharesNoAncestor is the arm-level
// REFUSAL: the anchor arm now asks the shared deterministic checklist
// (reclaim_ground_checklist.go), so a ground whose registration stands on an
// ORPHAN-ROOT branch must survive the sweep's apply — and it must survive with
// its registration and its branch, because removal is never the response to a
// failed check.
//
// Why this arm and not the sweep arm's own test: this is a SWEEPER whose ladder
// above proves only that the TAKER's run is gone, which says nothing about (a)
// whether this process is standing in the ground and (b) whether the branch is
// the unrelated-history shape `cmd/agent/merge_run.go` refuses to treat as an
// ordinary deliverable. The arm asked neither question until 2026-09-26; this
// test drives the second one through the REAL collect path (RunGroundCollectableGrounds
// → RunGroundCollect), on a real linked worktree whose branch genuinely reports
// `git merge-base master <branch>` exit 1 (measured: the orphan checkout keeps
// the registration valid and the merge-base exit is exactly 1).
func TestRunGroundSweepKeepsAGroundWhoseBranchSharesNoAncestor(t *testing.T) {
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := groundTestRepo(t, "ground-orphan-repo")

	// A ground registered DETACHED first, then switched to an orphan root inside
	// the worktree — the only way a linked worktree comes to stand on a branch
	// with no common ancestor (`git worktree add -b X` can only name a branch
	// that exists, and an orphan branch has no commit yet).
	orphan := filepath.Join(root, "gopi-orphan-branch")
	if out, err := groundTestGit(repo, "worktree", "add", "--detach", orphan); err != nil {
		t.Fatalf("worktree add --detach: %v\n%s", err, out)
	}
	if out, err := groundTestGit(orphan, "checkout", "--orphan", "fix/orphan-root"); err != nil {
		t.Fatalf("checkout --orphan: %v\n%s", err, out)
	}
	if out, err := groundTestGit(orphan, "rm", "-rfq", "."); err != nil {
		t.Fatalf("rm -rfq: %v\n%s", err, out)
	}
	if out, err := groundTestGit(orphan, "commit", "-q", "--allow-empty", "-m", "orphan root"); err != nil {
		t.Fatalf("commit the orphan root: %v\n%s", err, out)
	}
	// The fixture must BE the shape the rung measures, or the test proves
	// nothing: git's own exit 1 is the positive finding.
	if out, err := groundTestGit(repo, "merge-base", IntegrationBranchName, "fix/orphan-root"); err == nil {
		t.Fatalf("the fixture's branch must share NO ancestor with %s, but merge-base succeeded:\n%s",
			IntegrationBranchName, out)
	}

	// A FINISHED taker: the run that anchored this ground has exited, so the
	// liveness ladder above is satisfied and the ONLY thing between this ground
	// and removal is the checklist.
	dead := deadPidOnThisHost(t)
	writeAnchorRecordForTest(t, sink, repo, orphan, "fix/orphan-root", "", strconv.Itoa(dead), "999999999", strconv.Itoa(dead))
	anchorAgeForTest(t, sink, orphan, 48*time.Hour)

	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	t.Logf("read-out:\n  %s", strings.Join(RunGroundSweepReport(collectable, kept), "\n  "))
	if !groundIn(collectable, orphan) {
		t.Fatalf("the fixture is not exercising the arm: the ground must reach the APPLY step; collectable=%v kept=%v",
			groundPaths(collectable), groundPaths(kept))
	}

	removed, skipped, cerr := RunGroundCollect(collectable)
	if cerr != nil {
		t.Fatalf("RunGroundCollect: %v", cerr)
	}
	t.Logf("apply: removed=%v skipped=%v", removed, skipped)

	if containsPath(removed, orphan) {
		t.Fatalf("the arm FREED the ground of an unrelated-history branch: removed=%v", removed)
	}
	var why string
	for _, s := range skipped {
		if strings.Contains(s, orphan) {
			why = s
		}
	}
	if !strings.Contains(why, reclaimGroundLineageRefusalFmt) {
		t.Errorf("the skip must NAME the failed check (%q); skipped=%v", reclaimGroundLineageRefusalFmt, skipped)
	}
	// A refusal leaves the directory, the registration and the branch EXACTLY as
	// they were — the arm removes a registration, and it removed none.
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("a refused ground's directory must survive: %v", err)
	}
	if out, err := groundTestGit(repo, "worktree", "list"); err != nil || !strings.Contains(out, orphan) {
		t.Errorf("a refused ground's registration must survive; worktree list=%q err=%v", out, err)
	}
	if out, err := groundTestGit(repo, "rev-parse", "--verify", "--quiet", "fix/orphan-root"); err != nil {
		t.Errorf("the branch must survive a refused removal: %v\n%s", err, out)
	}
}

// TestGroundLivenessDoesNotCountTheAskerAsAHolder is the RED PROOF for the
// defect the Rule 31 run MEASURED (2026-09-23): the sweep is `gopi`, it asks
// "does the taker's launch session still hold a live gopi?" about the session it
// is ITSELF standing in (the operator's shell), the census found the asker,
// called it a live holder, and kept a provably finished ground forever — the
// exact symptom (nothing is ever collected) the sweep exists to remove.
//
// The fixture is the real shape, not a model of it: the test RE-EXECS itself
// through a symlink named `gopi` (so the asker's kernel `comm` is exactly
// procname.NameGopi, as the real sweep's is), in the same session, and the child
// process then asks the production ladder about an anchor whose taker is DEAD.
// Pre-fix the verdict is LIVE (the child counts itself); with the fix it is the
// "no live gopi process" rung.
func TestGroundLivenessDoesNotCountTheAskerAsAHolder(t *testing.T) {
	if _, err := os.Stat(procRootName); err != nil {
		t.Skipf("no procfs at %s — the census cannot be measured here", procRootName)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, procname.NameGopi)
	if err := os.Symlink(os.Args[0], link); err != nil {
		t.Skipf("cannot plant the gopi-named asker: %v", err)
	}
	cmd := exec.Command(link, "-test.run=TestGroundLivenessAskerProbeChild", "-test.v")
	cmd.Env = append(os.Environ(), askeProbeMarker+"=1")
	// Its OWN session (setsid): the probe then judges a session whose only gopi
	// is itself, which is the case the defect is about — a questioner answering
	// its own question. Without this the child would sit in the operator's live
	// session, where a live gopi genuinely exists and the keep is correct.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.CombinedOutput()
	t.Logf("asker probe (comm=gopi, pid child):\n%s", strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("the asker probe failed — a gopi process standing in the session must not keep a finished ground: %v", err)
	}
}

// TestGroundLivenessAskerProbeChild is the re-exec'd half: it runs as `gopi`, in
// its caller's session, and asks the production ladder about a DEAD taker.
func TestGroundLivenessAskerProbeChild(t *testing.T) {
	if os.Getenv(askeProbeMarker) == "" {
		t.Skip("asker probe — only runs as the re-exec'd child")
	}
	sid, ok := procname.ProcSessionKey(procRootName, strconv.Itoa(os.Getpid()))
	if !ok {
		t.Skip("cannot read this process's session key")
	}
	if finishCleanupProcCommand(strconv.Itoa(os.Getpid())) != procname.NameGopi {
		t.Skipf("the probe must run as %s, got %s", procname.NameGopi, finishCleanupProcCommand(strconv.Itoa(os.Getpid())))
	}
	dead := deadPidOnThisHost(t)
	anchor := RunGroundAnchor{
		Worktree:   filepath.Join(os.TempDir(), "gopi-probe-ground-does-not-exist"),
		PID:        strconv.Itoa(dead),
		StartTicks: "999999999",
		Session:    strconv.Itoa(sid),
	}
	verdict := RunGroundAnchorLiveness(anchor)
	t.Logf("liveness verdict: live=%v reason=%q", verdict.Live, verdict.Reason)
	if verdict.Live {
		t.Fatalf("the ASKER'S OWN gopi process kept a finished ground — a questioner that answers itself is not evidence: %s", verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, "holds no live gopi process") {
		t.Errorf("the verdict must name the session census as the reason: %q", verdict.Reason)
	}
}

// TestGroundLivenessExcludesOnlyTheRecordingProcess pins the exclusion's SCOPE:
// the exclusion is anchored on the pid the ANCHOR recorded (the process that
// took the ground), never on "any gopi", so a live bystander gopi launched from
// the same shell still vetoes. A weaker rule would release a ground a real run
// is standing in.
func TestGroundLivenessExcludesOnlyTheRecordingProcess(t *testing.T) {
	// Simulate the sweep standing in session 4242 with the finished taker gone:
	// the census — with the asker removed by the caller — is empty, so the
	// verdict must be the "no live gopi" rung, not the unmeasurable one.
	cands := []finishCleanupSessionCand{}
	if finishCleanupSessionCensusHoldsGopi(cands, 4242) {
		t.Fatalf("with the asker excluded the census must be empty")
	}
	// And with a SECOND live gopi in that session the keep stands.
	cands = append(cands, finishCleanupSessionCand{Sid: 4242, Alive: true})
	if !finishCleanupSessionCensusHoldsGopi(cands, 4242) {
		t.Errorf("a second live gopi in the session must keep the ground")
	}
}

// TestRunGroundLivenessIsUnmeasurableWhenTheAnchorCarriesNoIdentity pins the
// fail-safe direction the epic requires (Rule 15a / memory #2633: unknown is
// never dead): an anchor that measured nothing can never license a removal.
func TestRunGroundLivenessIsUnmeasurableWhenTheAnchorCarriesNoIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    RunGroundAnchor
	}{
		{"no pid", RunGroundAnchor{Worktree: "/x", Session: "123"}},
		{"no session and a dead incarnation", RunGroundAnchor{Worktree: "/x", PID: "999999999", StartTicks: "1"}},
		{"unparseable pid", RunGroundAnchor{Worktree: "/x", PID: "not-a-pid", Session: "123"}},
	} {
		v := RunGroundAnchorLiveness(tc.a)
		if !v.Live {
			t.Errorf("%s: must read as LIVE (unmeasurable is never dead), got collectable: %s", tc.name, v.Reason)
		}
		if v.Reason == "" {
			t.Errorf("%s: every verdict must carry its reason", tc.name)
		}
	}
}

// --- fixture helpers -------------------------------------------------------

// writeAnchorRecordForTest appends one anchor record to the sink, so a test can
// stage an identity it cannot produce by running (a dead taker) without
// weakening the reader it is testing.
func writeAnchorRecordForTest(t *testing.T, sink, repo, worktree, branch, taskID, pid, ticks, sid string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", os.Getenv("GOPI_MEMORY_DB"))
	t.Setenv(removalaudit.EnvSink, sink)
	removalaudit.WorktreeGround(removalaudit.ArmTaskGroundAnchor, repo, worktree, branch, taskID, pid, ticks, sid, "staged anchor")
}

// anchorAgeForTest rewrites the anchor's timestamp for one directory, so the age
// floor can be exercised without waiting out a real one. It rewrites the record
// the reader will parse — the reader itself is untouched.
func anchorAgeForTest(t *testing.T, sink, worktree string, age time.Duration) {
	t.Helper()
	raw, err := os.ReadFile(sink)
	if err != nil {
		t.Fatalf("read sink: %v", err)
	}
	want := "\"target\":\"" + worktree + "\""
	when := time.Now().Add(-age).UTC().Format(time.RFC3339)
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if !strings.Contains(line, want) {
			continue
		}
		lines[i] = replaceJSONField(line, "ts", when)
	}
	if err := os.WriteFile(sink, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write sink: %v", err)
	}
}

// replaceJSONField replaces one top-level string field's value in a record line.
func replaceJSONField(line, field, value string) string {
	key := "\"" + field + "\":\""
	i := strings.Index(line, key)
	if i < 0 {
		return line
	}
	rest := line[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return line
	}
	return line[:i+len(key)] + value + rest[j:]
}

// testStartTicks / testSessionKey read a live process's identity for a staged
// anchor that must read as LIVE.
func testStartTicks(t *testing.T, pid int) string {
	t.Helper()
	ticks, ok := procname.StartTicks(procRootName, strconv.Itoa(pid))
	if !ok {
		t.Skipf("cannot read start-ticks for pid %d", pid)
	}
	return ticks
}

func testSessionKey(t *testing.T, pid int) string {
	t.Helper()
	sid, ok := procname.ProcSessionKey(procRootName, strconv.Itoa(pid))
	if !ok {
		t.Skipf("cannot read the session for pid %d", pid)
	}
	return strconv.Itoa(sid)
}

func groundIn(list []RunGroundLiveness, dir string) bool {
	for _, v := range list {
		if sameWorkingPath(v.Anchor.Worktree, dir) {
			return true
		}
	}
	return false
}

func groundPaths(list []RunGroundLiveness) []string {
	var out []string
	for _, v := range list {
		out = append(out, v.Anchor.Worktree+" ("+v.Reason+")")
	}
	return out
}

func containsPath(list []string, dir string) bool {
	for _, p := range list {
		if sameWorkingPath(p, dir) {
			return true
		}
	}
	return false
}

// --- the SECOND GATE (defect 01a1151f, measured 2026-10-07) -----------------

// TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow is the
// RED->GREEN proof for the defect this file's doc calls the second gate.
//
// THE SHAPE, and it is the real one (measured on this host): the pid recorded in
// the anchor is a long-lived `gopi worker` LOOP — pid 3577762, started 07:53:02,
// claimed the 08:45 row 01a113ac, the 12:23 row 01a11494 and the 14:18 row
// 01a114f3. So "the recorded incarnation is alive" is a fact about the WORKER and
// never about the ground: every ground it claimed stayed LIVE for as long as it
// lived, long after those runs had ended. Pre-fix the verdict for such a ground
// was LIVE with the reason "the run that took this ground (pid …) is still here"
// — a sentence the measurement does not support, and the reason the row-terminal
// grounds 01a113ac / 01a11494 were still kept by `gopi task sweep-worktrees
// --dry-run` on 2026-10-07.
//
// The fixture is the whole ladder through the PRODUCTION entry: one live pid (the
// test process itself — the worker's stand-in, alive and claiming row after row)
// anchoring TWO grounds, one whose row is terminal (failed) and one whose row is
// still in_progress. The second ground is the POSITIVE CONTROL: it must keep its
// veto, or the fix would have torn the lock out instead of widening it.
func TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow(t *testing.T) {
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := groundTestRepo(t, "ground-second-gate-repo")

	me := os.Getpid()
	pid := strconv.Itoa(me)
	ticks := testStartTicks(t, me)
	sid := testSessionKey(t, me)

	// The finished run: its row reached a terminal status hours ago, while the
	// worker that claimed it is still alive (this process).
	finished := filepath.Join(root, "gopi-second-gate-finished")
	if out, err := groundTestGit(repo, "worktree", "add", finished, "-b", "task/01a0sgfinished"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	const finishedRow = "01a0sg-finished"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "second gate: finished", Payload: "x", Repo: repo, TaskID: finishedRow}); err != nil {
		t.Fatalf("TaskSubmit(finished): %v", err)
	}
	// The row is driven through the REAL state machine (pending → claimed →
	// failed), never by editing the status column: the ladder reads a status the
	// ledger actually produces.
	if _, err := TaskUpdate(finishedRow, TaskStatusClaimed); err != nil {
		t.Fatalf("TaskUpdate(finished, claimed): %v", err)
	}
	if _, err := TaskUpdate(finishedRow, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskUpdate(finished, in_progress): %v", err)
	}
	if _, err := TaskUpdate(finishedRow, TaskStatusFailed); err != nil {
		t.Fatalf("TaskUpdate(finished, failed): %v", err)
	}
	writeAnchorRecordForTest(t, sink, repo, finished, "task/01a0sgfinished", finishedRow, pid, ticks, sid)
	anchorAgeForTest(t, sink, finished, 48*time.Hour)

	// The live run: a DIFFERENT row, claimed by the SAME still-alive worker, and
	// still in_progress — this ground must survive.
	live := filepath.Join(root, "gopi-second-gate-live")
	if out, err := groundTestGit(repo, "worktree", "add", live, "-b", "task/01a0sglive"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	const liveRow = "01a0sg-live"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "second gate: live", Payload: "x", Repo: repo, TaskID: liveRow}); err != nil {
		t.Fatalf("TaskSubmit(live): %v", err)
	}
	if _, err := TaskUpdate(liveRow, TaskStatusClaimed); err != nil {
		t.Fatalf("TaskUpdate(live, claimed): %v", err)
	}
	if _, err := TaskUpdate(liveRow, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskUpdate(live, in_progress): %v", err)
	}
	writeAnchorRecordForTest(t, sink, repo, live, "task/01a0sglive", liveRow, pid, ticks, sid)
	anchorAgeForTest(t, sink, live, 48*time.Hour)

	// THE LADDER, one anchor at a time: the same launch context, two different
	// ledger facts, two different verdicts.
	overVerdict := RunGroundAnchorLiveness(RunGroundAnchor{
		Repo: repo, Worktree: finished, Branch: "task/01a0sgfinished", TaskID: finishedRow, PID: pid, StartTicks: ticks, Session: sid,
	})
	t.Logf("terminal-row ground: live=%v reason=%q", overVerdict.Live, overVerdict.Reason)
	if overVerdict.Live {
		t.Fatalf("the ledger says this run is over (row %s is %s) while only the CLAIM WORKER is alive — the ground must be collectable: %s",
			finishedRow, TaskStatusFailed, overVerdict.Reason)
	}
	if !strings.Contains(overVerdict.Reason, TaskStatusFailed) {
		t.Errorf("the reason must name the ledger fact that decided it (status %s, row id): %q", TaskStatusFailed, overVerdict.Reason)
	}

	liveVerdict := RunGroundAnchorLiveness(RunGroundAnchor{
		Repo: repo, Worktree: live, Branch: "task/01a0sglive", TaskID: liveRow, PID: pid, StartTicks: ticks, Session: sid,
	})
	t.Logf("in_progress-row ground: live=%v reason=%q", liveVerdict.Live, liveVerdict.Reason)
	if !liveVerdict.Live {
		t.Fatalf("POSITIVE CONTROL: a row still in_progress must keep its ground whatever the launch rung says: %s", liveVerdict.Reason)
	}

	// And the SCAN, through the production partitioner: same two verdicts, and
	// the finished ground reaches the removal arm.
	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	t.Logf("read-out:\n  %s", strings.Join(RunGroundSweepReport(collectable, kept), "\n  "))
	if !groundIn(collectable, finished) {
		t.Fatalf("the finished ground must be collectable; collectable=%v kept=%v", groundPaths(collectable), groundPaths(kept))
	}
	if groundIn(collectable, live) {
		t.Fatalf("the in_progress row's ground must never be collectable; collectable=%v", groundPaths(collectable))
	}
	if !groundIn(kept, live) {
		t.Fatalf("the in_progress row's ground must appear among the KEPT; kept=%v", groundPaths(kept))
	}
}

// TestGroundLivenessKeepsWhenTheLedgerCannotBeAsked pins the FAIL-SAFE direction
// of the same rung (Rule 15a — unknown is never dead): an anchor whose row is
// ABSENT from the ledger, and an anchor that names no row at all, both keep the
// ground when the launch context is alive. Without this the second gate would
// release exactly the grounds nobody can account for — a bare-prompt run, a
// foreign ledger, an anchor written by an older build.
func TestGroundLivenessKeepsWhenTheLedgerCannotBeAsked(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	me := os.Getpid()
	base := RunGroundAnchor{
		Worktree:   filepath.Join(os.TempDir(), "gopi-second-gate-keeps"),
		PID:        strconv.Itoa(me),
		StartTicks: testStartTicks(t, me),
		Session:    testSessionKey(t, me),
	}
	for _, tc := range []struct {
		name string
		id   string
	}{
		{"a row the ledger does not hold", "01a0sg-absent"},
		{"an anchor that names no row", ""},
	} {
		a := base
		a.TaskID = tc.id
		v := RunGroundAnchorLiveness(a)
		t.Logf("%s: live=%v reason=%q", tc.name, v.Live, v.Reason)
		if !v.Live {
			t.Errorf("%s must KEEP the ground (unmeasurable is never dead): %s", tc.name, v.Reason)
		}
	}
}
