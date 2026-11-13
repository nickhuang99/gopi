// The `--explain` face of `gopi tool scratch-gc`, driven through the REAL verb against
// a sandbox scratch home, so nothing here can touch the developer's real tree.
//
// The three things this face owes an operator:
//
//  1. every candidate it looked at carries a line saying whether it may be
//     collected and WHY (so a zero removal count stops being unreadable);
//  2. the line names the RECORDED OWNER the decision was measured against, so a
//     `collectable` verdict is checkable evidence rather than a claim;
//  3. the writer-owned verdict works WITHOUT the age clock: a FRESH root whose
//     recorded owner is gone is collectable in the same run, and a FRESH root
//     whose recorded owner is live (but standing somewhere else) is not.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/scratch"
)

// writeOwnerRegistration writes a v2 owner record into dir, exactly as a scratch
// writer does (pid + kernel start ticks + the command line).
func writeOwnerRegistration(t *testing.T, dir string, pid int, start uint64, cmdText string) {
	t.Helper()
	body := fmt.Sprintf("%s=%d\n%s=%d\n%s=%s\n",
		scratch.HolderKeyPID, pid, scratch.HolderKeyStart, start, scratch.HolderKeyCmd, cmdText)
	if err := os.WriteFile(filepath.Join(dir, scratch.HolderFileName), []byte(body), scratch.FilePerm); err != nil {
		t.Fatalf("write owner record in %s: %v", dir, err)
	}
}

// goneProcessIdentity returns the identity of a process that has ALREADY exited: a
// real child is started, its pid + start ticks captured, and then it is killed and
// reaped, so the record names a pid the kernel has released.
func goneProcessIdentity(t *testing.T) (pid int, start uint64, cmdText string) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	// The fixture's subject is its PID's identity, never its cwd, and it must not
	// outlive this binary (a SIGKILLed test runs no cleanup at all).
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a fixture process (%v) — the gone-owner case is untestable here", err)
	}
	pid = cmd.Process.Pid
	st, err := scratch.ProcStartTime(pid)
	if err != nil {
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		_ = cmd.Wait()         //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
		t.Skipf("cannot read /proc/%d/stat (%v)", pid, err)
	}
	argv, _ := procname.Cmdline(scratch.ProcRootDir, strconv.Itoa(pid))
	cmdText = strings.Join(argv, " ")
	if err := cmd.Process.Kill(); err != nil {
		_ = err //stylegate:ignore the error is already reported by the caller's checked call; this site only reaps or logs the leftover// best effort: Wait still reaps it
	}
	if err := cmd.Wait(); err != nil {
		_ = err //stylegate:ignore the error is already reported by the caller's checked call; this site only reaps or logs the leftover// a killed child exits non-zero; the point is that it is GONE
	}
	if _, err := scratch.ProcStartTime(pid); err == nil {
		t.Skipf("pid %d is still in /proc after Wait — cannot build a gone-owner fixture here", pid)
	}
	return pid, st, cmdText
}

// TestScratchGCCLI_ExplainReportsWhyKept: a home with one fresh root and one aged
// root produces a verdict line for EACH, naming the decision and its outcome —
// the fresh one kept (no recorded owner, so only the clock bounds it) and the aged
// one collectable by the age rule.
func TestScratchGCCLI_ExplainReportsWhyKept(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	fresh := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"explain-run-fresh", 0)
	aged := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"explain-run-aged", 7*time.Hour)

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCExplainFlag, scratchGCDryRunFlag}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	freshLine := verdictLineFor(t, out, fresh)
	if !strings.Contains(freshLine, "verdict="+scratchGCKeepVerdict) {
		t.Errorf("a fresh unregistered root must be a KEEP with a reason, got: %q", freshLine)
	}
	if !strings.Contains(freshLine, "outcome="+string(scratch.EntryOutcomeUnregistered)) {
		t.Errorf("the line must name the OUTCOME (no writer registered it), got: %q", freshLine)
	}
	agedLine := verdictLineFor(t, out, aged)
	if !strings.Contains(agedLine, "verdict="+scratchGCCollectableVerdict) {
		t.Errorf("a 7h-old unregistered root must be collectable by the age rule, got: %q", agedLine)
	}
}

// TestScratchGCCLI_ExplainPairsWithDryRun: `--explain --dry-run` is the VETTING
// face spelled out — the verdicts and the plan, and nothing deleted. It is the
// SAME face `--explain` alone now reaches (scratchGCExplainImpliesDryRun), kept as
// an advertised form because a reader who wants to be explicit about the read-only
// face should not have to know the implication.
func TestScratchGCCLI_ExplainPairsWithDryRun(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	aged := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"explain-run-kept", 7*time.Hour)

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCExplainFlag, scratchGCDryRunFlag}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(aged); err != nil {
		t.Fatalf("the vetting face (--explain --dry-run) DELETED %s: %v", aged, err)
	}
	// Both faces of the report are present: the plan's promise and the verdicts.
	if !strings.Contains(out, "dry-run=true") || !strings.Contains(out, "would-remove "+aged) {
		t.Errorf("the plan half must still be printed, got: %q", out)
	}
	if line := verdictLineFor(t, out, aged); !strings.Contains(line, "verdict="+scratchGCCollectableVerdict) {
		t.Errorf("the verdict half must call the aged root collectable, got: %q", line)
	}

	// And the real sweep then does remove it — the report was a promise the sweep
	// keeps (one predicate, two faces).
	out2, stderr2, code2 := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code2 != 0 {
		t.Fatalf("sweep exit=%d stderr=%s", code2, stderr2)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("the sweep did not honour the verdict it reported (%v); out=%q", err, out2)
	}
}

// TestScratchGCCLI_OwnerGoneIsCollectedWhileFresh — the epic's acceptance through
// the real verb: a FRESH root whose recorded owner is GONE is removed in one manual
// run, with NO clock involved (its mtime is now), and the second run is idempotent
// (removes 0).
//
// TWO invocations, and the split is the promise this file's header makes: the
// VERDICT face (`--explain` ALONE) must judge that root collectable and leave it on
// disk, and the plain sweep must then do exactly what the verdict said. Before task
// 01a1957a this test spelled only `--explain` for both halves — which is the defect
// itself, restated as a test: the flag that describes a deletion performed it.
//
// The verdict line reports the owner evidence; the sweep's own collection line on
// stderr names the root, its recorded incarnation and the outcome that authorised
// the delete.
func TestScratchGCCLI_OwnerGoneIsCollectedWhileFresh(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	fossil := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"owner-gone-run", 0) // FRESH on purpose
	pid, start, cmdText := goneProcessIdentity(t)
	writeOwnerRegistration(t, fossil, pid, start, cmdText)

	// The JUDGEMENT first: `--explain` alone says collectable and touches nothing.
	judgeOut, judgeErr, judgeCode := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCExplainFlag}, "", scratchGCEnv(home, scratchHome), "")
	if judgeCode != 0 {
		t.Fatalf("judgement run exit=%d stderr=%s", judgeCode, judgeErr)
	}
	if _, err := os.Stat(fossil); err != nil {
		t.Fatalf("`%s %s` DELETED the fresh root it was asked to judge (err=%v); out=%q stderr=%q",
			CliVerbScratchGC, scratchGCExplainFlag, err, judgeOut, judgeErr)
	}
	if !strings.Contains(judgeOut, "dry-run=true") {
		t.Errorf("the judgement face must report itself as a dry run, got: %q", judgeOut)
	}
	judged := verdictLineFor(t, judgeOut, fossil)
	if !strings.Contains(judged, "verdict="+scratchGCCollectableVerdict) {
		t.Errorf("a fresh root with a GONE recorded owner must be judged collectable, got: %q", judged)
	}

	// Then the SWEEP (no flag — the deletion needs no explanation): it keeps the
	// promise the verdict just made.
	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("sweep exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(fossil); !os.IsNotExist(err) {
		t.Fatalf("a fresh root with a GONE recorded owner survived the sweep (err=%v); out=%q stderr=%q", err, out, stderr)
	}
	if !strings.Contains(out, "removed=1") {
		t.Errorf("the sweep must report removed=1, got: %q", out)
	}
	if !strings.Contains(stderr, "outcome="+string(procname.OutcomeGone)) {
		t.Errorf("the collection line must name the outcome that authorised the delete (gone), got stderr: %q", stderr)
	}

	// Third run: idempotent — nothing left to collect.
	out2, stderr2, code2 := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code2 != 0 {
		t.Fatalf("second run exit=%d stderr=%s", code2, stderr2)
	}
	if !strings.Contains(out2, "removed=0") {
		t.Errorf("the second run must be idempotent (removed=0), got: %q", out2)
	}
}

// TestScratchGCCLI_LiveOwnerElsewhereKeepsFreshRoot — the other half of the
// contract, through the real verb: the registration protects a root even though
// (a) it is FRESH and (b) its owner is NOT standing in it (the /proc cwd scan
// cannot see this process, whose cwd is the test's own directory).
func TestScratchGCCLI_LiveOwnerElsewhereKeepsFreshRoot(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	live := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"owner-live-run", 0)
	// This test process registers ITSELF: alive for the whole test, cwd elsewhere.
	start, err := scratch.ProcStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("read own start ticks: %v", err)
	}
	writeOwnerRegistration(t, live, os.Getpid(), start, "the-test-process")

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCExplainFlag}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	line := verdictLineFor(t, out, live)
	if !strings.Contains(line, "verdict="+scratchGCKeepVerdict) || !strings.Contains(line, "outcome="+string(procname.OutcomeLive)) {
		t.Errorf("the verdict must be keep/outcome=live, got: %q", line)
	}
	// The SURVIVAL claim is asserted against the SWEEP, not against the report:
	// `--explain` alone deletes nothing on any arm now, so a judgement-only run
	// would let a broken predicate pass. The registration must hold when the
	// deletion actually runs.
	sweepOut, sweepErr, sweepCode := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if sweepCode != 0 {
		t.Fatalf("sweep exit=%d stderr=%s", sweepCode, sweepErr)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("a root whose RECORDED OWNER IS LIVE was collected by the sweep: %v (out=%q stderr=%q)", err, sweepOut, sweepErr)
	}
	if !strings.Contains(sweepOut, "removed=0") {
		t.Errorf("the sweep must remove nothing while the recorded owner is live, got: %q", sweepOut)
	}
}

// TestScratchGCCLI_RecycledPIDIsCollected — the pid is ALIVE (it is this test
// process) but the recorded incarnation is not its, so the kernel handed the number
// on and the owner is gone: the fresh root is collectable — judged so by the verdict
// face and then actually removed by the sweep, which is the only invocation that
// deletes (scratchGCExplainImpliesDryRun).
func TestScratchGCCLI_RecycledPIDIsCollected(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	recycled := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"owner-recycled-run", 0)
	start, err := scratch.ProcStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("read own start ticks: %v", err)
	}
	writeOwnerRegistration(t, recycled, os.Getpid(), start+1, "an-earlier-life-of-this-pid")

	judgeOut, judgeErr, judgeCode := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCExplainFlag}, "", scratchGCEnv(home, scratchHome), "")
	if judgeCode != 0 {
		t.Fatalf("judgement run exit=%d stderr=%s", judgeCode, judgeErr)
	}
	if _, err := os.Stat(recycled); err != nil {
		t.Fatalf("the judgement face deleted the recycled-pid root it was judging: %v (out=%q)", err, judgeOut)
	}
	if line := verdictLineFor(t, judgeOut, recycled); !strings.Contains(line, "verdict="+scratchGCCollectableVerdict) {
		t.Errorf("a fresh root at a RECYCLED pid must be judged collectable, got: %q", line)
	}

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("sweep exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(recycled); !os.IsNotExist(err) {
		t.Fatalf("a fresh root at a RECYCLED pid survived the sweep (err=%v); out=%q stderr=%q", err, out, stderr)
	}
	if !strings.Contains(stderr, "outcome="+string(procname.OutcomeRecycled)) {
		t.Errorf("the collection line must name outcome=recycled, got stderr: %q", stderr)
	}
}

// TestScratchGCCLI_ExplainWithConsentStillDeletes — the ONE exception, through the
// real verb: `profile --explain --yes` spells a consent word, so the verdict report
// prints and the confirmed reclaim RUNS. This is the arm proving the implication is
// not a blanket "explain means never delete": an operator who typed the family's
// own "go ahead" word asked for the pass, and silently turning that into a report
// would be the same class of lie in the other direction.
func TestScratchGCCLI_ExplainWithConsentStillDeletes(t *testing.T) {
	f := seedProfileArmFixture(t)
	env := profileArmEnv(f.home, f.profile)

	out, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCExplainFlag, scratchGCStubsYesFlag}, "", env, f.home)
	if code != 0 {
		t.Fatalf("exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, out, stderr)
	}
	if !strings.Contains(out, "removed=1") {
		t.Errorf("`profile --explain --yes` must still reclaim (the consent word is not nullified by --explain), got:\n%s", out)
	}
	if _, err := os.Lstat(f.aged); !os.IsNotExist(err) {
		t.Errorf("the aged unheld residue child survived an explicit `--yes` sweep: err=%v", err)
	}
	if _, err := os.Lstat(f.declared); err != nil {
		t.Errorf("the declared child was removed by the consent form: %v", err)
	}
}

// verdictLineFor finds the one verdict line naming path (the report is the only
// place these lines are printed).
func verdictLineFor(t *testing.T, out, path string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, scratchGCVerdictLabel+" "+path+" ") {
			return line
		}
	}
	t.Fatalf("no %s line for %s in the report:\n%s", scratchGCVerdictLabel, path, out)
	return ""
}
