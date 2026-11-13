package tools

// THE WIP-SNAPSHOT POINTER — a successor row is told where its predecessor's
// uncommitted work is.
//
// THE DEFECT THIS CLOSES (same epic as task_wip_snapshot.go). The snapshot leg
// preserves an interrupted run's uncommitted work in that run's own session
// directory. Preserving it is only half the job: the successor row (`<id>-cN`,
// minted by `gopi task clone`) starts from a FRESH ground with a clean tree, so
// the 1097 salvaged lines are invisible to it unless someone tells it they
// exist. In the measured incident that someone was a human, awake, who copied
// the files and described them in the successor by hand.
//
// So the successor seam asks the disk a direct question — "did this row leave a
// WIP deposit?" — and, when it did, appends the PATH to the successor's payload.
// The relayer reads it out of its own briefing. No human in the loop, no file
// hand-over.
//
// WHY THE PAYLOAD AND NOT A NEW COLUMN OR A NEW DELIVERABLE KEY: the payload IS
// the briefing (the successor's own text, copied verbatim from the source and
// extended by the copy's provenance header). A pointer written anywhere else
// would have to be fetched by a reader who does not know to look; a pointer in
// the briefing is read by the one agent that must act on it, at the one moment
// it reads anything at all. It adds no schema, no key and no dependency
// (Rule 35).
//
// WHAT IT DOES NOT DO: it never copies the snapshot into the successor's
// directory (the bytes stay in ONE place, and a second copy is a second thing to
// lose track of), and it never restores anything into the successor's worktree
// — restoring is the relayer's deliberate act, and an automatic `git apply` onto
// a fresh ground would be exactly the kind of tree mutation Rule 30 forbids this
// epic from making.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TaskSuccessorWIPMarker is the phrase the successor's briefing carries when a WIP
// deposit from the source run was found. One string for the writer and for a
// test to match without re-parsing the sentence (Rule 14).
const TaskSuccessorWIPMarker = "--- uncommitted-work snapshot from the interrupted run ---"

// TaskSuccessorNoWIPMarker is the phrase the mint's LINE carries when the source
// run left no deposit. Stated rather than silent (Rule 15): an operator asking
// "was anything salvaged from that failure?" reads the answer here instead of
// concluding from an absence that the mechanism did not run.
const TaskSuccessorNoWIPMarker = "carries no WIP snapshot (the source run left no uncommitted work on disk)"

// TaskSuccessorWIPPathMarker prefixes the path in the mint's returned LINE, the
// same way the repo and attachment axes render theirs.
const TaskSuccessorWIPPathMarker = "wip="

// TaskSuccessorWIPBriefing renders the block appended to the successor's payload.
// It names the directory, what is inside it, and the exact restore recipe —
// a briefing that says "there is a snapshot somewhere" would still need a human
// to explain it.
func TaskSuccessorWIPBriefing(path, sourceTaskID string) string {
	var b strings.Builder
	b.WriteString(TaskSuccessorWIPMarker + "\n")
	b.WriteString("The run that failed (" + sourceTaskID + ") left UNCOMMITTED work in its own\n")
	b.WriteString("worktree. It was snapshotted before the row was closed, and it is still on\n")
	b.WriteString("disk at:\n\n")
	b.WriteString("    " + path + "\n\n")
	b.WriteString("Read `" + taskWIPHandoffFile + "` in that directory FIRST — it states what was\n")
	b.WriteString("measured and which entries could NOT be carried. Then, from the root of the\n")
	b.WriteString("ground you are working in:\n\n")
	b.WriteString("    git apply --binary --3way " + filepath.Join(path, taskWIPTrackedPatchFile) + "\n")
	b.WriteString("    cp -r " + filepath.Join(path, taskWIPUntrackedDirName) + "/. .\n")
	b.WriteString("    git status --porcelain   # compare against " + filepath.Join(path, taskWIPStatusFile) + "\n\n")
	b.WriteString("Restore only what still applies: your ground may already carry part of that\n")
	b.WriteString("work as a commit, in which case `git apply` refuses the hunks it has and the\n")
	b.WriteString("remainder is the real gap. Reporting what you recovered — and what you\n")
	b.WriteString("could not — is part of the delivery.\n")
	return b.String()
}

// taskSuccessorWIPDeposit answers where the source run's WIP deposit is, or ""
// when there is none. It reads the DISK, not the ledger: the deposit's existence
// is the fact, and a ledger event that says a snapshot happened while the
// directory has since been removed must not put a dead path in a successor's
// briefing.
func taskSuccessorWIPDeposit(taskID string) string {
	dir, err := taskSessionDir(taskID)
	if err != nil || dir == "" {
		return ""
	}
	deposit := filepath.Join(dir, taskWIPDirName)
	fi, err := os.Stat(deposit)
	if err != nil || !fi.IsDir() {
		return ""
	}
	// A directory that exists but holds no hand-over note is not a usable
	// deposit (a partially written one from a crash mid-copy): a pointer at it
	// would send the successor to a directory it cannot interpret.
	if _, err := os.Stat(filepath.Join(deposit, taskWIPHandoffFile)); err != nil {
		return ""
	}
	return deposit
}

// taskSuccessorWIPAxis is the successor seam: it returns the text to APPEND to
// the successor's payload and the line fragment for the caller's confirmation.
//
// The two are returned together so a caller cannot render the line without
// carrying the briefing (or the reverse) — the measured defect in this family is
// always one side of a promise landing and the other not.
func taskSuccessorWIPAxis(taskID string) (briefing, line string) {
	path := taskSuccessorWIPDeposit(taskID)
	if path == "" {
		return "", " — " + TaskSuccessorNoWIPMarker
	}
	return "\n\n" + TaskSuccessorWIPBriefing(path, taskID),
		fmt.Sprintf(" — %s%s", TaskSuccessorWIPPathMarker, path)
}
