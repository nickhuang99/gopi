package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// THE CLOSE-OUT MUST NOT REPORT ON A DIRECTORY THE FACE ALREADY LEFT.
//
// THE DEFECT (measured 2026-10-15, real binary, the operator's own capture of a
// live `gopi worker --resume --branch <row>`): every SUCCESSFUL run ended with
// this pair of identical lines —
//
//	gopi worker --resume: close-out for /: resume: --worktree / is not a work
//	tree: git rev-parse --show-toplevel in /: exit status 128: fatal: 不是 git
//	仓库（或者任何父目录）：.git
//
// — and they were printed TWICE per run. Neither line is about a real failure:
// the run had just finished closing the row out.
//
// THE MECHANISM. runTaskWorkerResumeCleanup re-parents this process's CWD to the
// filesystem root BEFORE it does anything else, and that ordering is a producer
// obligation rather than style (it must not hold a ground its own next step is
// allowed to remove — internal/worktreerelease). The face's own-ground report
// was then made from `pwdOrEmpty()` — read AFTER the release — so the resolver
// was handed "/" as "the directory this face occupies", asked git about it, and
// git (correctly) refused. The call site was also duplicated, once at the end of
// runTaskWorkerResumeCleanup and once in runTaskWorkerResume, which is why the
// false pair appeared twice.
//
// WHAT THIS TEST PINS, in the operator's own terms — the merged capture of one
// real run, which is what the operator reads:
//
//	① the released root is NEVER named as this face's ground ("close-out for /:"),
//	② no "is not a work tree" refusal appears at all in a SUCCESSFUL run,
//	③ the correct sentence appears EXACTLY ONCE, naming the directory the face
//	   was launched from — the dedup half of the fix,
//	④ and the run still exits 0: a close-out refusal is not the run's verdict.
func TestWorkerResumeOwnGroundReportNeverNamesTheReleasedRoot(t *testing.T) {
	taskID, repo, _, branch, _, gopiHome, dbPath := workerResumeE2E(t)

	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "the deliverable is finished"})
	defer mock.Close()
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	// The launch directory IS this face's own ground for the whole run: the
	// fixture's repo is where the operator stands, and it is the directory the
	// own-ground report must name (it names no row, which is its own correct
	// answer — the point here is WHICH directory is reported on).
	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch},
		"", env, repo)
	merged := stdout + stderr
	if code != 0 {
		t.Fatalf("worker --resume must exit 0 (a close-out report is not the run's verdict): code=%d\n%s", code, merged)
	}
	t.Logf("one run of `%s %s %s %s`: released-root reports=%d, non-work-tree refusals=%d, own-ground reports=%d",
		CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, branch,
		strings.Count(merged, workerResumeReleasedRootMarker),
		strings.Count(merged, workerResumeNotAWorkTreeMarker),
		strings.Count(merged, workerResumeOwnGroundLine(repo)))

	// ① THE RELEASED ROOT IS NEVER THE SUBJECT. This is the defect verbatim: the
	// path in the sentence was "/", and "/" is not a fact about this face's
	// ground — it is the artifact of the release that ran one line earlier.
	if n := strings.Count(merged, workerResumeReleasedRootMarker); n != 0 {
		t.Errorf("the released root must never be named as this face's ground, got %d occurrence(s) of %q\n--- merged capture ---\n%s",
			n, workerResumeReleasedRootMarker, merged)
	}
	// …and the same property in the note's own frame (epic 01a17e23): the
	// sentence now ENDS with the directory, so a resolved root would show up as
	// the line's last token — never as a substring test, because every real
	// path starts with "/" and `HasPrefix` would fire on all of them. Compared
	// on whole lines.
	if n := workerResumeOwnGroundRootLines(merged); n != 0 {
		t.Errorf("the released root must never be the own-ground note's subject, got %d line(s)\n--- merged capture ---\n%s",
			n, merged)
	}
	// ② NO NON-WORK-TREE REFUSAL in a run that succeeded. The marker is the
	// resolver's own sentence, and the ONLY way it reaches this capture is the
	// defect (nothing else asks git about a directory in this mode but the
	// selectors, which resolved the row and the ground long before).
	if n := strings.Count(merged, workerResumeNotAWorkTreeMarker); n != 0 {
		t.Errorf("a successful run must carry no %q refusal, got %d\n--- merged capture ---\n%s",
			workerResumeNotAWorkTreeMarker, n, merged)
	}
	// ③ THE CORRECT SENTENCE, EXACTLY ONCE — the launch directory, and the
	// dedup half: pre-fix this same reading was taken and printed twice.
	if n := strings.Count(merged, workerResumeOwnGroundLine(repo)); n != 1 {
		t.Errorf("this face's own ground must be reported exactly once, naming %s — got %d\n--- merged capture ---\n%s",
			repo, n, merged)
	}
	// The row the run was about is still the one it reports closing out — the
	// fix must not have swapped which row the close-out is about.
	if !strings.Contains(merged, taskID) {
		t.Errorf("the capture must still name the resumed row %s:\n%s", taskID, merged)
	}
}

// workerResumeReleasedRootMarker is the false sentence's distinguishing prefix:
// the report rendered the released root as the subject of "close-out for …".
// Spelled as the literal it is (not assembled from ProcessRootPath) so editing
// the sentence at its call site cannot silently satisfy the assertion.
const workerResumeReleasedRootMarker = "close-out for /:"

// workerResumeNotAWorkTreeMarker is the resolver's own refusal text (the same
// marker internal/tools' own test pins), read here as a literal for the same
// reason.
const workerResumeNotAWorkTreeMarker = "is not a work tree"

// workerResumeOwnGroundLine renders the sentence this face emits for its own
// working directory: "<prog>: own ground (normal): <answer> — <dir>". It is
// built from the face's own program-name constant and the directory asked
// about, so the assertion cannot drift into counting some other line that
// happens to share the prefix.
//
// THE FRAME CHANGED (epic 01a17e23, 2026-10-28) and these two assertions did
// not: the note used to open with the close-out's own frame ("close-out for
// <dir>: …"), which made a healthy run's line read as that close-out's failure
// cause. The properties pinned here — exactly once, naming the LAUNCH directory;
// never the released root — are unchanged, and they are still measured on a real
// run's merged capture.
func workerResumeOwnGroundLine(dir string) string {
	return strings.TrimSuffix(fmt.Sprintf(workerResumeOwnGroundNormalFormat,
		workerResumeProgName, workerResumeOwnGroundNoRowSummary, dir), "\n")
}

// The two tokens the own-ground note is built from, spelled here because the
// test reads the emitted line (Rule 1: a value the assertions compare is named,
// not typed twice).
const (
	// workerResumeOwnGroundSeparator is the em-dash separator between the note's
	// summary and the directory it is about.
	workerResumeOwnGroundSeparator = " — "
	// rootPathSpelling is the filesystem root: the directory the release pass
	// re-parents this process to, and the subject the 2026-10-15 defect
	// reported on.
	rootPathSpelling = "/"
)

// workerResumeOwnGroundRootLines counts the capture's lines whose SUBJECT is the
// filesystem root. The note ends with the directory it is about, so the root can
// only appear as a line's final token — a substring test would match every
// absolute path, and a whole-line test would miss a note carrying any other
// summary. Hence: one line per line, ending in the note's own separator plus "/".
func workerResumeOwnGroundRootLines(capture string) int {
	root := workerResumeOwnGroundSeparator + rootPathSpelling
	n := 0
	for _, line := range strings.Split(capture, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.TrimSpace(line), workerResumeProgName+": ") && strings.HasSuffix(line, root) {
			n++
		}
	}
	return n
}
