package main

// A NORMAL DIAGNOSTIC MUST NOT READ AS A FAILURE CAUSE (epic 01a17e23,
// measured 2026-10-28).
//
// WHAT WENT WRONG, in three sessions' words. `gopi worker --resume` closes a row
// out and then notes which row the directory IT stands in belongs to. On healthy
// runs that directory names no row, and the note used to print inside the
// close-out's own frame:
//
//	gopi worker --resume: close-out for /path/to/launch-dir: no ledger row names this directory
//
// — on the line AFTER the result. Rows 01a17da0, 01a17bf6 and 01a17dd7 each
// faced a failed count (`close-out report=0`) with that sentence two lines away
// and concluded the ledger lookup had missed the directory. None of them had: the
// count was failing for an unrelated reason, and the sentence was describing the
// ordinary design of this mode (the face is NOT standing in the task's ground —
// that is the whole point of the mode).
//
// WHAT THE EPIC ASKS FOR is not silence (Rule 15: the fact still gets printed,
// once) but a sentence a reader cannot mistake for a cause. Hence three
// properties, and this file pins all three:
//
//	① a HEALTHY run's capture carries no spelling a reader can take as the
//	   close-out's failure cause;
//	② the own-ground fact is still stated — ONCE, naming the directory, and
//	   saying in its own words that it is the normal condition;
//	③ a REAL failure states its own cause in the capture, where it happens;
//	④ the close-out fact is stated too, in ITS OWN normal frame — added when the
//	   finish-cleanup chain was retired (2026-10-31): with no chain to ask there
//	   is no report on any run, so "no report" is the ordinary condition and may
//	   not wear the failure frame. That is the red this file now guards.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// workerResumeCauseLookingSpellings are the words a healthy capture must not
// carry about the own-ground note. Each was read as "this is why the count
// failed" by at least one of the three sessions, and each is spelled here as a
// literal on purpose: this file pins the operator-facing text, so an edit at the
// call site must turn it red rather than be followed silently.
//
//	v1 `no ledger row names this directory`  the arm's old wording, sitting
//
//	                                          right after the close-out result
//	v2 `close-out for`                        the frame it borrowed — nothing a
//
//	                                          healthy run prints may reuse it
//	v3 `FAILED`                               the failure marker; reserved for the
//
//	                                          arm that really did fail
//	v4 `no cause`                             the failure sentence's own token
//
// A note that carries any of these is a note a reader can misread, which is the
// defect this epic removes.
var workerResumeCauseLookingSpellings = []string{
	"no ledger row names this directory",
	workerResumeProgName + ": close-out for",
	"FAILED",
	"no cause",
	// v5 `produced NO report`  the FAILURE frame's verdict; with the chain
	//                          retired no run can honestly produce a report, so
	//                          a healthy capture must not say this either
	"produced NO report",
}

// TestWorkerResumeHealthyCaptureCarriesNoCauseLookingSpelling is property ① —
// and it is measured on a REAL run's merged capture, because that is what the
// three sessions read.
func TestWorkerResumeHealthyCaptureCarriesNoCauseLookingSpelling(t *testing.T) {
	taskID, repo, _, branch, _, gopiHome, dbPath := workerResumeE2E(t)

	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "deliverable done"})
	defer mock.Close()
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch},
		"", env, repo)
	merged := stdout + stderr
	if code != 0 {
		t.Fatalf("a close-out note is not the run's verdict: code=%d\n--- merged capture ---\n%s", code, merged)
	}

	// ① NO CAUSE-LOOKING SPELLING in a healthy capture.
	for _, bad := range workerResumeCauseLookingSpellings {
		if n := strings.Count(merged, bad); n != 0 {
			t.Errorf("a HEALTHY run must not print %q (%d occurrence(s)) — that is the spelling three "+
				"sessions read as the close-out's failure cause\n--- merged capture ---\n%s", bad, n, merged)
		}
	}

	// ② THE FACT IS STILL STATED, exactly once, naming the directory, and it
	// says in its own words that it is normal (Rule 15: the report is not
	// deleted, it is made unmistakable).
	note := workerResumeOwnGroundNote(repo)
	if n := strings.Count(merged, note); n != 1 {
		t.Errorf("the own-ground note must be stated exactly once, naming %s as %q — got %d\n--- merged capture ---\n%s",
			repo, note, n, merged)
	}
	if !strings.Contains(merged, workerResumeOwnGroundNormalWord) {
		t.Errorf("the note must call itself the NORMAL condition (word %q), so a reader does not go looking "+
			"for a failure behind it\n--- merged capture ---\n%s", workerResumeOwnGroundNormalWord, merged)
	}

	// ③ THE CLOSE-OUT FACT IS STATED TOO, once, in its OWN normal frame — and
	// this half is what the retirement of the chain made necessary (measured
	// 2026-10-31): the sentence that used to say `close-out for … produced NO
	// report` was a failure frame over an ordinary condition. The fact may not be
	// deleted (Rule 15), it may not be dressed as a failure (that is ①), so it is
	// stated where it happens, naming the row and the retired mechanism.
	retiredLine := workerResumeCloseoutRetiredLine(taskID)
	if n := strings.Count(merged, retiredLine); n != 1 {
		t.Errorf("the close-out note must be stated exactly once, as %q — got %d\n--- merged capture ---\n%s",
			retiredLine, n, merged)
	}
	if !strings.Contains(merged, workerResumeCloseoutRetiredWord) {
		t.Errorf("the close-out note must call itself the %q mechanism, so a reader stops looking for a report "+
			"nothing produces\n--- merged capture ---\n%s", workerResumeCloseoutRetiredWord, merged)
	}
	if !strings.Contains(merged, workerResumeCloseoutRetiredReason) {
		t.Errorf("the close-out note must state WHY there is no report (the retired chain), not leave the reader "+
			"to invent a cause\n--- merged capture ---\n%s", merged)
	}

	// …and the run still names the row it closed out: the fix must not have
	// moved the close-out's own subject.
	if !strings.Contains(merged, taskID) {
		t.Errorf("the capture must still name the resumed row %s\n--- merged capture ---\n%s", taskID, merged)
	}
	t.Logf("healthy `%s %s %s %s`: cause-looking spellings=%d, own-ground note=%d, close-out note=%d",
		CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch,
		len(workerResumeCauseLookingSpellings), strings.Count(merged, note), strings.Count(merged, retiredLine))
	// The CAPTURE itself is logged, not only the counts: this is the artifact
	// the epic is about (what an operator reads), and a passing run that prints
	// only numbers leaves the evidence in a variable nobody can quote.
	t.Logf("--- merged capture (stdout+stderr) of `%s %s %s %s` ---\n%s",
		CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch, merged)
}

// TestWorkerResumeCloseoutFailureNamesItsCauseOnTheCapture is the failure half
// of the epic, and it has TWO halves because only one of them is reachable from
// a whole run:
//
//	① THE REACHABLE FAILURE, end to end. `gopi worker --resume` validates its
//	   ground (the row) BEFORE it spawns anything, so a ledger it cannot read
//	   fails at the SELECTOR and never reaches the close-out. What that capture
//	   must still do is name its cause — the selector's own sentence, which
//	   reads `gopi worker: resume: ledger lookup for …: … unable to open
//	   database file` — rather than leaving the reader to the nearest line.
//	   (Same shape the three sessions faced: a run that failed, and a healthy-
//	   looking note beside it. The note is now unmistakable, which is ②'s job.)
//	② THE CLOSE-OUT ARM ITSELF, asserted on its own sentence. `TaskFinishCleanup
//	   -SessionID` is documented to return `(nil, err)` on every fault, so the
//	   arm's reachable cause in practice is the ERROR line beside it; the
//	   `rep == nil` half is the one shape that needs the chain to answer
//	   neither a report nor an error, which no fixture can force without
//	   failing the selector first. So its sentence is pinned at its own
//	   renderer — the fact is delivered, and the report says exactly how much
//	   of it was executed (Rule 31's honest-gap clause, not a silent claim).
func TestWorkerResumeCloseoutFailureNamesItsCauseOnTheCapture(t *testing.T) {
	_, repo, _, branch, _, gopiHome, _ := workerResumeE2E(t)

	// A DIRECTORY where a database file should be: sqlite cannot open it, so the
	// chain cannot read the row. Nobody's logic is mocked here — the real
	// binary meets a real unreadable ledger path.
	unopenable := t.TempDir()

	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "deliverable done"})
	defer mock.Close()
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+unopenable, config.SkipClaimBroadcastEnv+"=1")

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch},
		"", env, repo)
	merged := stdout + stderr
	if code == 0 {
		t.Fatalf("an unreadable ledger must fail the run: code=%d\n--- merged capture ---\n%s", code, merged)
	}

	// ① THE FAILURE NAMES ITS CAUSE IN THE CAPTURE — the ledger, by path, and
	// the fact that it could not be opened. A capture that only says "failed"
	// would repeat the defect from the other direction (Rule 15).
	for _, want := range []string{unopenable, workerResumeLedgerUnopenableMarker} {
		if !strings.Contains(merged, want) {
			t.Errorf("the failure capture must name its cause (%q absent):\n--- merged capture ---\n%s", want, merged)
		}
	}
	// …and the cause line is the SELECTOR's own, in its own frame — the
	// top-level worker name, not this face's resume name — so a reader cannot
	// mistake the own-ground note for it.
	if !strings.Contains(merged, workerProgTopLevel+": "+workerResumeSelectorCauseFrame) {
		t.Errorf("the cause must come from the selector's own frame (%q), so a reader cannot mistake the "+
			"own-ground note for it:\n--- merged capture ---\n%s",
			workerProgTopLevel+": "+workerResumeSelectorCauseFrame, merged)
	}

	t.Logf("failure `%s %s %s %s` on an unopenable ledger: exit=%d, cause named=%v",
		CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch, code,
		strings.Contains(merged, workerResumeLedgerUnopenableMarker))

	// ② THE TWO FRAMES, rendered side by side and read. This is the epic's
	// acceptance in one assertion: a healthy sentence and a failed sentence must
	// be distinguishable by their FIRST words, not by a reader's care.
	//
	// The retirement of the chain (58b5618b) is why the healthy half exists at
	// all: there is no report to produce on ANY run, so "no report" is the
	// ordinary condition and gets the ordinary frame. The failure frame stays
	// exactly where it is true — a reading that failed.
	healthy := workerResumeCloseoutRetiredLine("t-some-row")
	// The FAILED side is rendered from the frame that is LIVE (`close-out for …
	// FAILED:`, reachable when a reading fails) — not a symbol invented to make
	// this assertion pass. That is the frame a healthy line must not borrow.
	failed := fmt.Sprintf(workerResumeOwnGroundFailedFormat, workerResumeProgName, "t-some-row", workerResumeLedgerUnopenableMarker)
	if healthy == failed {
		t.Fatalf("the healthy and failed sentences must differ, both were %q", healthy)
	}
	for _, want := range []string{
		"t-some-row",
		workerResumeProgName + ": " + workerResumeCloseoutRetiredLabel + " " + workerResumeCloseoutRetiredWord + ":",
		workerResumeCloseoutRetiredReason,
	} {
		if !strings.Contains(healthy, want) {
			t.Errorf("the healthy close-out line must carry %q, got %q", want, healthy)
		}
	}
	// …and neither may borrow the other's decisive token. This is the property
	// the defect violated in both directions: the borrowed frame (a healthy run
	// printing `close-out for …`) and the invented cause (`no cause`).
	for _, bad := range []string{workerResumeCloseoutFailureFrame, workerResumeCloseoutFailureMissingMarker} {
		if strings.Contains(healthy, bad) {
			t.Errorf("the HEALTHY line must not carry the failure frame %q, got %q", bad, healthy)
		}
	}
	for _, bad := range []string{workerResumeCloseoutRetiredWord, workerResumeCloseoutRetiredReason} {
		if strings.Contains(failed, bad) {
			t.Errorf("the FAILED line must not carry the healthy note's wording %q, got %q", bad, failed)
		}
	}
	// …nor may the healthy line reuse the own-ground note's label: they are two
	// different facts about two different things (Rule 15's one-fact rule).
	if strings.Contains(healthy, workerResumeOwnGroundNormalLabel) {
		t.Errorf("the close-out line must not borrow the own-ground note's label (%q), got %q",
			workerResumeOwnGroundNormalLabel, healthy)
	}
}

// workerResumeOwnGroundNote renders the whole healthy line for a launch
// directory that names no row — the ordinary shape, and the one the three
// sessions misread. Built from the production constants so the assertion cannot
// drift into counting a lookalike: a change to either half turns this test red.
func workerResumeOwnGroundNote(dir string) string {
	return fmt.Sprintf(workerResumeOwnGroundNormalFormat, workerResumeProgName, workerResumeOwnGroundNoRowSummary, dir)
}

// workerResumeCloseoutFailureFrame is the distinguishing prefix of the FAILED
// line: the close-out's own frame, reserved for a seam that really failed.
const workerResumeCloseoutFailureFrame = workerResumeProgName + ": " + workerResumeCloseoutRetiredLabel + " for "

// workerResumeCloseoutFailureMissingMarker is the failure line's verdict — that
// the seam produced no report — which is a defect when it is a real seam and an
// ordinary condition when the mechanism is retired. The two sentences must never
// carry each other's token.
const workerResumeCloseoutFailureMissingMarker = "produced NO report:"

// workerResumeLedgerUnopenableMarker is sqlite's own wording for a ledger path
// it could not open, as the selector renders it. Spelled as a literal on purpose,
// like the sentences above: it is the token an operator greps, so a change in
// the layer that words it must turn this test red rather than be followed
// silently.
const workerResumeLedgerUnopenableMarker = "unable to open database file"

// workerResumeSelectorCauseFrame is the frame the SELECTOR's ledger fault carries
// (internal/tools/task_resume_ground.go, "resume: ledger lookup for --<flag> …").
// The test compares against it so "the failure says what failed" is measured on
// the reader's text, not on an assertion's prose.
const workerResumeSelectorCauseFrame = "resume: ledger lookup for"
