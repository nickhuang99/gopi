package tools

// task_submit_dead_test_name_warning_test.go — THE EXECUTED ARM for the whole
// `[no tests to run]` family, plus the write-face warning's own two arms.
//
// WHY AN EXECUTED ARM EXISTS AT ALL (the epic's stated gap, verbatim: "两个探测器
// 缺「执行臂」——没有任何测试证明「源码零命中」与「go test 打印 no tests to run 且
// exit 0」是同一件事"). Every test in the two detector files drives the SOURCE
// SCAN: it proves a name is or is not declared in a `*_test.go` file. NONE of
// them proves that the toolchain agrees — that a name the scan calls dead really
// produces `[no tests to run]` and exit 0. Without that leg, a scan with an
// off-by-one (say, one that missed a `func Test…` spelling) would report a live
// test as dead and no test in this repository would notice.
//
// LEG ① IS THAT PROOF, and it is measured on the real toolchain, in this
// repository, against a real package: the same selector is run twice, once with a
// name the package implements and once with a name nothing anywhere implements,
// and the two outputs are compared:
//
//	live name  → a `=== RUN` line, exit 0
//	dead name  → NO `=== RUN` line, `[no tests to run]` in the output, exit 0
//
// The second half of leg ① is the cross-check the epic asks for: for that SAME
// dead name, the detectors must report ZERO reach (a `ZERO REACH` record from
// each of the two clauses), and the write-face warning must name it. So one
// string is judged by both instruments and they agree.
//
// LEG ② is the positive control on the write face: a briefing whose selector is
// LIVE yields NO warning. A warning that fired on every submit would be noise, and
// the sibling warnings' own arms are built the same way.
//
// LEG ③ drives the REAL surface (Rule 31): a TaskSubmit of a briefing carrying a
// dead selector, asserting the warning is in the confirmation the submitter reads
// AND that the row still queues (a warning, never a refusal — a briefing may
// legitimately name a test the child is asked to WRITE).
//
// LEG ④ states the silence contract: an unresolvable package pattern and an empty
// repo produce NO warning. "Cannot measure" must never be reported as a finding
// (Rule 15).
//
// Run this file:  go test ./internal/tools/ -run 'TestSubmitDeadTestName' -count=1

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// deadNameGoBin is the toolchain this arm executes. Named because the whole
	// point of the leg is that it runs a REAL `go test` (Rule 1).
	deadNameGoBin = "go"
	// deadNameTestVerb / deadNameCountFlag / deadNameRunFlag are that command's
	// own spellings, used as-is.
	deadNameTestVerb  = "test"
	deadNameCountFlag = "-count=1"
	// deadNameVerboseFlag is what makes the runner PRINT its `=== RUN` lines at
	// all — without it a live selector and a dead one both print one `ok` line,
	// which is the whole reason this defect is invisible (measured while writing
	// this arm: the first draft's LIVE control printed only `ok … 0.638s`).
	deadNameVerboseFlag = "-v"
	deadNameRunFlag     = "-run"
	// deadNamePackage is the package both runs select: tiny, dependency-free,
	// and the one scripts/test-budget.sh's own gate already uses, so its cost is
	// already on the merge path.
	deadNamePackage = "./internal/smokegate/"
	// deadNameLiveTest is a test that EXISTS there (`grep -n '^func Test'`).
	deadNameLiveTest = "TestCommandIsNotTheWholeTree"
	// deadNameDeadTest is the name this arm proves nothing implements. It is
	// spelled exactly like a real name on purpose: that is the live typo shape
	// the PA reported four instances of in one night.
	deadNameDeadTest = "TestBriefingRunAltNoSuchTestEverExists"
	// deadNameProseDeadTest is a SECOND dead name used only in the prose
	// population, so the two arms of the write face are visibly distinct.
	deadNameProseDeadTest = "TestBriefingDeadNameInProseNoSuchTestEverExists"
	// deadNameRunMarker is what a `go test` run PRINTS when it selected a test.
	deadNameRunMarker = "=== RUN"
	// deadNameVacuumMarker is what it prints when it selected NOTHING — the
	// suffix that sits at the end of the `ok` line, four milliseconds from a
	// real pass.
	deadNameVacuumMarker = "[no tests to run]"
	// deadNameZeroReach is the detectors' own verdict word for a dead
	// alternative/name; referenced rather than re-typed.
	deadNameZeroReach = "ZERO REACH"
	// deadNameSubmitSummary / deadNamePayloadPrefix frame the briefing leg ③
	// files. The payload carries the acceptance command verbatim.
	deadNameSubmitSummary = "prove the write face names a dead selector"
	deadNamePayloadHead   = "## Acceptance\n\ngo test " + deadNamePackage + " " + deadNameRunFlag + " '" + deadNameDeadTest + "' -count=1\n"
)

// deadNameModuleRoot locates the module root this test binary was compiled from —
// a package pattern is resolved against the working directory, and this test's
// directory is a package, not the module (the measured trap the sibling probe in
// internal/testutil records).
func deadNameModuleRoot(t *testing.T) string {
	t.Helper()
	return moduleRootOfTest(t)
}

// deadNameRunGoTest executes ONE real `go test` and returns its combined output
// and exit code.
//
// The exit code is read from the *exec.Cmd's own error, never through a shell
// pipe: `cmd | grep` reports grep's status, and this whole epic is about exit
// codes that lie about what ran (PROJECT.md §4.10).
func deadNameRunGoTest(t *testing.T, root, pattern string) (out string, code int) {
	t.Helper()
	if _, err := exec.LookPath(deadNameGoBin); err != nil {
		t.Skipf("%s not installed: %v", deadNameGoBin, err)
	}
	cmd := exec.Command(deadNameGoBin, deadNameTestVerb, deadNameCountFlag, deadNameVerboseFlag, deadNameRunFlag, pattern, deadNamePackage)
	cmd.Dir = root
	raw, err := cmd.CombinedOutput()
	if err == nil {
		return string(raw), 0
	}
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) {
		t.Fatalf("execute `%s %s …`: %v", deadNameGoBin, deadNameTestVerb, err)
	}
	return string(raw), exitErr.ExitCode()
}

// TestSubmitDeadTestNameTheScanAndTheToolchainAgree is LEG ① and the epic's
// central deliverable: the source scan's verdict and the toolchain's behaviour
// are the SAME verdict, proven by running both on one string.
func TestSubmitDeadTestNameTheScanAndTheToolchainAgree(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := deadNameModuleRoot(t)

	// --- the toolchain, on a LIVE name -------------------------------------
	liveOut, liveCode := deadNameRunGoTest(t, root, "^"+deadNameLiveTest+"$")
	if liveCode != 0 {
		t.Fatalf("the LIVE selector exited %d, want 0 — this control must pass or the comparison below "+
			"proves nothing\n%s", liveCode, liveOut)
	}
	if !strings.Contains(liveOut, deadNameRunMarker) {
		t.Fatalf("the LIVE selector printed no %q line — nothing ran, so the control is vacuous\n%s",
			deadNameRunMarker, liveOut)
	}
	if strings.Contains(liveOut, deadNameVacuumMarker) {
		t.Fatalf("the LIVE selector printed %q — this package must really execute a test\n%s",
			deadNameVacuumMarker, liveOut)
	}

	// --- the toolchain, on a DEAD name (the defect, reproduced) -------------
	deadOut, deadCode := deadNameRunGoTest(t, root, "^"+deadNameDeadTest+"$")
	if deadCode != 0 {
		t.Fatalf("a DEAD selector exited %d, want 0 — if Go ever fails a zero-match filter, this family of "+
			"defects stops existing and this test must be rewritten rather than kept\n%s", deadCode, deadOut)
	}
	if strings.Contains(deadOut, deadNameRunMarker) {
		t.Fatalf("a DEAD selector printed %q — the name is not dead, so the premise of the whole epic is "+
			"wrong for %q\n%s", deadNameRunMarker, deadNameDeadTest, deadOut)
	}
	if !strings.Contains(deadOut, deadNameVacuumMarker) {
		t.Fatalf("a DEAD selector printed no %q — the only signal that separates a vacuum from a pass is "+
			"missing, and every consumer of the exit status would read it as green\n%s",
			deadNameVacuumMarker, deadOut)
	}

	// --- THE CROSS-CHECK: the SAME dead name, as the dispatchers see it -----
	briefing := deadNamePayloadHead
	_, runAltRecord := TaskBriefingRunAlternativesRefresh(root, briefing)
	if !deadNameRecordNames(runAltRecord, deadNameZeroReach, deadNameDeadTest) {
		t.Errorf("the `-run` clause must report %q for %q — the toolchain just proved it selects nothing, "+
			"and the scan must agree with that\nrecord: %v", deadNameZeroReach, deadNameDeadTest, runAltRecord)
	}
	// The prose clause reads PROSE only (a `go test … -run` line is its
	// sibling's population by contract), so the prose arm is driven with a line
	// that names no instrument.
	proseBriefing := "必测 `" + deadNameProseDeadTest + "` 必须转绿。\n"
	_, proseRecord := TaskBriefingProseTestNamesRefresh(root, proseBriefing)
	if !deadNameRecordNames(proseRecord, deadNameZeroReach, deadNameProseDeadTest) {
		t.Errorf("the prose clause must report %q for %q\nrecord: %v",
			deadNameZeroReach, deadNameProseDeadTest, proseRecord)
	}

	// --- and the WRITE FACE names the same string ---------------------------
	w := TaskSubmitDeadTestNameWarning(root, deadNameSubmitSummary, briefing)
	if w == "" {
		t.Fatalf("the write face must warn about %q — the toolchain proved it selects nothing\nbriefing:\n%s",
			deadNameDeadTest, briefing)
	}
	if !strings.Contains(w, deadNameDeadTest) {
		t.Errorf("the write-face warning must NAME the dead selector %q (a warning without its measurement "+
			"is a rumour):\n%s", deadNameDeadTest, w)
	}
	if !strings.Contains(w, "still queues") {
		t.Errorf("the write face must say the row still queues — a refusal is the one thing this seam must "+
			"never be:\n%s", w)
	}
}

// TestSubmitDeadTestNameALiveBriefingIsSilent is LEG ②: the positive control.
// Without it, a warning that fired on everything would pass leg ①.
func TestSubmitDeadTestNameALiveBriefingIsSilent(t *testing.T) {
	root := deadNameModuleRoot(t)
	live := "## Acceptance\n\ngo test " + deadNamePackage + " -run '" + deadNameLiveTest + "' -count=1\n"
	if w := TaskSubmitDeadTestNameWarning(root, "a correct briefing", live); w != "" {
		t.Errorf("a briefing whose selector is LIVE must yield NO warning (got %q)\nbriefing:\n%s", w, live)
	}
	// A briefing with no selector at all is the ordinary submit and must also be
	// silent — this is why the warning cannot degrade every row it touches.
	if w := TaskSubmitDeadTestNameWarning(root, "an ordinary row", "Fix the parser and run the suite.\n"); w != "" {
		t.Errorf("an ordinary briefing naming no selector must yield NO warning, got %q", w)
	}
}

// TestSubmitDeadTestNameTheWarningIsInTheConfirmation is LEG ③: the surface the
// submitter actually reads (Rule 31), driven end to end through the engine.
func TestSubmitDeadTestNameTheWarningIsInTheConfirmation(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	t.Setenv(worktreeroots.WorktreeRootEnvName, t.TempDir())
	repo := deadNameModuleRoot(t)

	const rowID = "t-submit-dead-selector"
	out, id, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: deadNameSubmitSummary,
		Payload: deadNamePayloadHead,
		Repo:    repo,
		TaskID:  rowID,
	})
	if err != nil {
		t.Fatalf("TaskSubmitReturningID: %v", err)
	}
	if id != rowID {
		t.Fatalf("row id: got %q, want %q", id, rowID)
	}
	if !strings.HasPrefix(out, "Task queued: ") {
		t.Fatalf("the confirmation must still open with the queued line (the warning is ADDITIVE):\n%s", out)
	}
	if !strings.Contains(out, deadNameDeadTest) {
		t.Fatalf("the confirmation the submitter reads must name the dead selector %q — this is the write "+
			"face, and a warning the submitter never sees is the defect it repairs:\n%s", deadNameDeadTest, out)
	}
	// THE ROW QUEUES (never a refusal).
	if got := taskRowStatus(t, rowID); got != taskStatusPending {
		t.Errorf("the warning must never refuse the row: status=%q, want %q", got, taskStatusPending)
	}
}

// TestSubmitDeadTestNameUnmeasurableIsNotAFinding is LEG ④: every arm that could
// not take the measurement stays SILENT (Rule 15 — an unmeasurable submit is not
// an accused one).
func TestSubmitDeadTestNameUnmeasurableIsNotAFinding(t *testing.T) {
	briefing := deadNamePayloadHead
	if w := TaskSubmitDeadTestNameWarning("", deadNameSubmitSummary, briefing); w != "" {
		t.Errorf("a submit with no repo cannot be measured and must stay silent, got %q", w)
	}
	// A package pattern this ground cannot resolve: the pattern must be STATED
	// as unmeasured by the dispatch clause and warned about by nobody here.
	outside := "## Acceptance\n\ngo test ./internal/definitely-not-a-package/ -run '" + deadNameDeadTest + "'\n"
	if w := TaskSubmitDeadTestNameWarning(deadNameModuleRoot(t), "unresolvable package", outside); w != "" {
		t.Errorf("an UNRESOLVABLE package pattern is a measurement that could not be taken, not a dead "+
			"selector; the write face must stay silent, got %q", w)
	}
}

// deadNameRecordNames reports whether any record line carries EVERY one of the
// given fragments — the shape both detector tests use, restated here because the
// record is a []string of rendered lines rather than structured data.
func deadNameRecordNames(record []string, wants ...string) bool {
	for _, line := range record {
		all := true
		for _, w := range wants {
			if !strings.Contains(line, w) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// asExitError is a one-line errors.As, kept beside its only caller so the two
// arms share it (Rule 14's spirit, one definition).
func asExitError(err error, target **exec.ExitError) bool {
	for err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			*target = ee
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
