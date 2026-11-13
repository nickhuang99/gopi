package tools

// task_resume_ground_refusal.go — the STRUCTURED half of a resume ground
// refusal, so a caller can render "recorded path / model verdict / detail /
// repair" instead of parsing a sentence.
//
// WHY IT EXISTS (measured, `gopi session resume <task-id>` on a CLONE-channel
// row): the session face resolved the row's ground with the worktree-era
// reading (`TaskDeliverableWorktreeFor` + `os.Stat` + `os.Chdir`) and, when the
// directory was not there, printed ONE warning line and ran the whole session in
// the CALLER's cwd — from the main checkout that is a session whose commits
// land in the shared tree, with no owner, no branch and no rollback point
// (PROJECT.md Rule 37). The refusal text existed already (`gopi worker --resume`
// had it, through TaskResumeGroundForTask); what the session face lacked was a
// way to TELL the two kinds of answer apart and to render each field:
//
//	no-ground    the row records neither a worktree nor a branch: nothing was
//	             ever stamped, so there is no ground to be wrong about
//	ground-gone  the row RECORDS a ground (path and/or branch) and that ground
//	             is not usable now: swept, removed by hand, or never registered
//
// The two are different facts with different repairs, and rendering them the
// same way is the misattribution this file exists to prevent (Rule 15). The
// LIVENESS verdict itself is NOT decided here: it is `devworkspace.ResolveGroundState`'s
// (the ONE judge, Rules 13/14) and this type only carries its answer forward.

import (
	"errors"
	"fmt"

	"github.com/nick/gopi/internal/config"
)

// TaskResumeGroundRefusalKind names WHICH refusal a resume ground resolution
// produced. Two values, because the two have different repairs (Rule 1b: the
// renderers and the tests agree by sharing these spellings).
type TaskResumeGroundRefusalKind string

const (
	// TaskResumeGroundRefusalNoGround: the row exists and records NO ground.
	TaskResumeGroundRefusalNoGround TaskResumeGroundRefusalKind = "no-ground"
	// TaskResumeGroundRefusalGone: the row records a ground and the ground is
	// not usable (the one judge said so; its own Detail rides along).
	TaskResumeGroundRefusalGone TaskResumeGroundRefusalKind = "ground-gone"
)

// taskResumeGroundModelUnread is the model field's spelling when no directory
// was read at all (the branch axis records no path, so no model exists to
// report). It reuses the package's absence marker rather than minting a second
// one.
func taskResumeGroundModelUnread() string { return taskGroundKeyAbsent }

// TaskResumeGroundRefusal is the typed refusal of a resume-ground resolution.
//
// It EMBEDS its operator sentence (Error()) so every existing caller that only
// prints `err` is unchanged, and adds the fields a renderer needs to answer the
// operator's four questions without re-reading the ledger or git.
type TaskResumeGroundRefusal struct {
	// TaskID is the row the resolution was asked about.
	TaskID string
	// Kind is which of the two facts this refusal is (see the Kind values).
	Kind TaskResumeGroundRefusalKind
	// Path is the ground the ROW RECORDED ("" when the row recorded none).
	// It is the row's own value — not a resolved directory — because the
	// operator has to see what was written, including a stale spelling.
	Path string
	// Branch and Repo are the row's other two deliverable axes, rendered with
	// the package's absence marker rather than as blank fields.
	Branch string
	Repo   string
	// Model is the ONE judge's verdict on Path (devworkspace.GroundModel
	// spelling), or the absence marker when no directory was read.
	Model string
	// Detail is the judge's own evidence line — what was checked and found.
	Detail string
	// Repair is the command an operator can run to fix the state, if the state
	// is repairable; "" when the repair is a judgement (a row with no ground
	// has nothing to repair, only a decision to make).
	Repair string
	// Message is the operator sentence (Error()'s text).
	Message string
}

// Error makes the typed refusal usable anywhere an error is (`gopi worker
// --resume` prints it exactly as before this type existed).
func (r *TaskResumeGroundRefusal) Error() string {
	if r == nil {
		return ""
	}
	return r.Message
}

// TaskResumeGroundRefusalOf extracts the typed refusal from an error chain, so
// a caller renders the fields without a second resolution (and without matching
// on words). ok is false for every other error — an unreadable ledger, an
// unknown row — which are NOT ground refusals and must not be dressed as one
// (Rule 15).
func TaskResumeGroundRefusalOf(err error) (*TaskResumeGroundRefusal, bool) {
	var r *TaskResumeGroundRefusal
	if errors.As(err, &r) {
		return r, r != nil
	}
	return nil, false
}

// taskResumeGroundGoneRepair is the repair an operator can actually run for a
// ground that is gone: the ledger's view of what the row records (the path,
// the branch, the status), git's own list of what the repo registers, and the
// explicit escape hatch. Named (Rule 1b) because the refusal and its test agree
// on it.
const taskResumeGroundGoneRepairFmt = "gopi task show %s · git -C %s worktree list --porcelain · " +
	"to resume anyway in the CALLER's cwd (not recommended): %s=1 gopi session resume %s"

// TaskResumeGroundGoneRefusal builds the `ground-gone` refusal for a row whose
// recorded ground the ONE judge called unusable.
func TaskResumeGroundGoneRefusal(taskID, path, branch, repo, model, detail string) *TaskResumeGroundRefusal {
	repoText := taskResumeGroundAxisText(repo)
	return &TaskResumeGroundRefusal{
		TaskID:  taskID,
		Kind:    TaskResumeGroundRefusalGone,
		Path:    path,
		Branch:  taskResumeGroundAxisText(branch),
		Repo:    repoText,
		Model:   model,
		Detail:  detail,
		Repair:  fmt.Sprintf(taskResumeGroundGoneRepairFmt, taskID, repoText, config.ResumeAllowMissingGroundEnv, taskID),
		Message: fmt.Sprintf(taskResumeGroundGoneMessageFmt, taskID, path, model, detail),
	}
}

// TaskResumeGroundNoGroundRefusal builds the `no-ground` refusal: the row
// carries no deliverable ground at all. Nothing is "gone" here, so the message
// does not claim a loss, and the repair is the operator's own decision (name a
// ground explicitly, or resume in the cwd they chose).
func TaskResumeGroundNoGroundRefusal(taskID, branch, repo string) *TaskResumeGroundRefusal {
	return &TaskResumeGroundRefusal{
		TaskID:  taskID,
		Kind:    TaskResumeGroundRefusalNoGround,
		Branch:  taskResumeGroundAxisText(branch),
		Repo:    taskResumeGroundAxisText(repo),
		Model:   taskResumeGroundModelUnread(),
		Detail:  "the row's deliverable records neither worktree nor branch",
		Message: fmt.Sprintf(taskResumeGroundNoGroundMessageFmt, taskID),
	}
}

// The two operator sentences, kept byte-compatible with the wording the
// `gopi worker --resume` face already shipped (a refusal that changes its
// words for no reason sends a reader who greps for the old one nowhere).
const (
	taskResumeGroundNoGroundMessageFmt = "resume: row %s names no ground — its deliverable records neither " +
		"worktree nor branch, so there is nothing to resume where the work sits"
	taskResumeGroundGoneMessageFmt = "resume: row %s records worktree %s, which is NOT a live ground — " +
		"the ground is gone (model %s: %s); this mode never creates one"
)
