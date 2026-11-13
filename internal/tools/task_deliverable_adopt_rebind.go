package tools

// task_deliverable_adopt_rebind.go — the ADOPT arm of `deliverable.worktree`.
//
// WHY THIS IS ITS OWN WRITER AND NOT A FLAG ON THE BACKFILL (measured
// 2026-10-29): every other writer of the worktree axis is a DETECTION seam — it
// watches a run and guesses where the run works, and the 2026-09-12 foreign-owner
// hijack is why it must NEVER overwrite a value that is already there
// (`taskBackfillDeliverableWorktreeAt`'s never-rebind guard). This writer is not a
// guess: the runner ANNOUNCED the ground (`GOPI_RUN_GROUND`, published by the
// spawn seam), the run ADOPTED that announcement (it chdir'd into it — measured
// by `os.Chdir` succeeding, never inferred), and the row's own ground stamp went
// there through the one shared writer. What is left is the case where the row
// ALREADY named a different directory — a row spawned into a ground that was
// opened for a different id, the delivery-retry round's shape — and then the row
// and the run disagree by construction. The delivery leg's anchor gate reads that
// disagreement as a reason to REFUSE (correctly: it publishes one tree at a
// time), so a run that adopted a ground and did not say so is a run that can
// never deliver.
//
// THE SAFETY IS IN THE PREDICATE, not in the caller's good intentions: the
// adopted directory must be a work tree whose HEAD lives in ITSELF (measured by
// git, the same question `devworkspace`'s head-directory gate asks) and must
// differ from what the row records. Everything unmeasured is a REFUSAL that
// names why (Rule 15) — this writer never falls back to writing anyway.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TaskDeliverableWorktreeAdoptMoment is the moment spelling this writer records
// in its line. It is separate from the claim/creation/repair spellings because
// the line is a CLAIM about when the byte was written, and attributing an ADOPTED
// ground to "creation" (or to an operator's repair) would be the same
// measured-attribution defect the claim/creation split was introduced for.
const TaskDeliverableWorktreeAdoptMoment = "adopt"

// The two refusal shapes of the adopt arm, each naming its own fact (Rule 15: an
// undecided case must never share a sentence with a decided one).
const (
	// TaskAdoptRebindNotAGroundFmt — the adopted directory is not a work tree of
	// its own, so the row must not be pointed at it: a recorded ground the sweeper
	// cannot measure is a ground nothing can reclaim and nothing can deliver from.
	// `%s` order: dir, the measured head directory, the probe command.
	TaskAdoptRebindNotAGroundFmt = "task %s: refusing to record the adopted ground %s — its HEAD lives in %s, " +
		"not in itself (measured with `git %s`), so it is not a work tree this row can own"
	// TaskAdoptRebindUnmeasurableFmt — the question could not be asked at all.
	TaskAdoptRebindUnmeasurableFmt = "task %s: refusing to record the adopted ground %s — cannot measure where its " +
		"HEAD lives (`git %s` failed: %w)"
)

// TaskAdoptRebindLineFmt is the ONE spelling of the success line: the row, the
// directory it recorded before, the one it now records, and WHAT made the old
// value wrong (the run adopted an announced ground). An operator reading this
// must be able to tell a deliberate rebind from a hijack, and a rebind nobody
// can read is indistinguishable from it.
const TaskAdoptRebindLineFmt = "task %s: deliverable.worktree %q -> %q at %s — this run ADOPTED the ground the " +
	"runner announced (it chdir'd into it and the row's ground stamp landed there), so the record now names the " +
	"directory the run actually works in"

// git adopt-arm spellings, named once (Rule 1b/13) so the command a reader
// reproduces is the command this file builds.
const (
	adoptRebindShowTopLevelVerb = "rev-parse"
	adoptRebindShowTopLevelFlag = "--show-toplevel"
	// adoptRebindExitNotAWorkTree is git's own status for "this directory is under
	// no work tree at all" — a real ANSWER, not a failure: it means the adopted
	// directory is a plain directory, which is exactly the refusal this writer
	// must make.
	adoptRebindExitNotAWorkTree = 128
)

// TaskDeliverableWorktreeAdopt records an ADOPTED ground on the row, replacing a
// previously recorded one.
//
// It returns (changed, error): changed=false with a nil error means there was
// nothing to do (the row already names this directory, the row is gone, the blob
// is not one this writer can pair). An error means the write was REFUSED or
// failed, and the caller reports it — a ground the run works in but the ledger
// does not name is a delivery that will be refused later with a much less
// readable sentence.
func TaskDeliverableWorktreeAdopt(taskID, adopted, branch string) (bool, error) {
	taskID = taskIDNormalize(taskID)
	adopted = strings.TrimSpace(adopted)
	branch = strings.TrimSpace(branch)
	if taskID == "" || adopted == "" {
		return false, nil
	}
	// THE GROUND MUST BE A WORK TREE WHOSE HEAD LIVES IN ITSELF. This is the same
	// question devworkspace's head-directory gate asks before publishing, asked
	// HERE because recording a ground and publishing from it must agree about
	// what a ground is (Rule 14: one definition, two readers).
	head, err := adoptRebindHeadDir(adopted)
	if err != nil {
		return false, fmt.Errorf(TaskAdoptRebindUnmeasurableFmt, taskID, adopted,
			adoptRebindShowTopLevelVerb, err)
	}
	if head == "" {
		return false, fmt.Errorf(TaskAdoptRebindNotAGroundFmt, taskID, adopted, "no work tree",
			adoptRebindShowTopLevelVerb+" "+adoptRebindShowTopLevelFlag)
	}
	if head != adoptRebindIdentity(adopted) {
		return false, fmt.Errorf(TaskAdoptRebindNotAGroundFmt, taskID, adopted, head,
			adoptRebindShowTopLevelVerb+" "+adoptRebindShowTopLevelFlag)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false, err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return false, err
	}
	if t == nil {
		return false, nil // row gone — nothing to record
	}
	raw := strings.TrimSpace(t.Deliverable)
	if raw == "" {
		return false, nil // no deliverable blob — nothing to merge
	}
	parsed := taskDeliverableParse(raw)
	if parsed.Repo == "" {
		return false, nil // repo-less blob — the worktree axis has nothing to pair
	}
	previous := parsed.Worktree
	if previous != "" && sameWorkingPath(previous, adopted) {
		return false, nil // already the adopted ground — no write, no line
	}
	if branch == "" {
		branch = parsed.Branch
	}
	merged := taskDeliverableRebuild(taskDeliverable{
		Repo: parsed.Repo, Branch: branch, Worktree: adopted,
		Grounds: parsed.Grounds, Attachments: parsed.Attachments,
	})
	if merged == raw {
		return false, nil
	}
	// THE CAS IS ON THE RAW BLOB: a concurrent writer that moved the deliverable
	// between the read and this write wins, and this writer then records nothing
	// rather than clobbering an axis it never read (the family's one write shape).
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ? AND deliverable = ?`,
		merged, float64(time.Now().Unix()), taskID, raw)
	if err != nil {
		return false, err
	}
	if n, nerr := res.RowsAffected(); nerr == nil && n > 0 {
		// The old value is PRINTED as the row recorded it (empty quoted as ""),
		// because "it changed from somewhere else" is the whole evidence that
		// this was a rebind and not a first stamp.
		taskWorktreeStampLog(TaskAdoptRebindLineFmt, taskID, previous, adopted,
			TaskDeliverableWorktreeAdoptMoment)
		return true, nil
	}
	return false, nil // the CAS lost (a concurrent writer moved the blob) — no write
}

// adoptRebindHeadDir answers which directory this directory's HEAD lives in,
// resolved through symlinks so two spellings of one directory are not a
// disagreement. An empty answer means "git found no work tree here", which is a
// MEASURED answer; a non-nil error means the question could not be asked.
func adoptRebindHeadDir(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, adoptRebindShowTopLevelVerb, adoptRebindShowTopLevelFlag)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == adoptRebindExitNotAWorkTree {
			return "", nil // measured: no work tree here
		}
		return "", err
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return "", nil
	}
	return adoptRebindIdentity(top), nil
}

// adoptRebindIdentity is the path identity comparison this file's predicate uses
// (absolute + symlink-resolved). It is NOT a second copy of devworkspace's
// `deliveryDirIdentity`: that one is unexported in another package and the
// import direction is tools ← nothing in devworkspace, so this package cannot
// reach it. Both implement ONE rule, stated here: two spellings of a directory
// are the same directory, and a path that cannot be resolved is compared as
// spelled (dropping the comparison would turn a real difference into a pass).
func adoptRebindIdentity(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil && resolved != "" {
		return resolved
	}
	return filepath.Clean(abs)
}
