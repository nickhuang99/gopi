package tools

// task_submit_ground_name.go — THE SUBMIT MOMENT REFUSES WHAT THE DISPATCH
// MOMENT MUST REFUSE (Rule 15; epic 01a12b5c).
//
// THE DEFECT, measured: `gopi task submit --task-id t-foo` was ACCEPTED, wrote
// the row and printed "Task queued: t-foo …". The row can never run. At
// dispatch TaskRunGroundFor takes the DERIVE-OR-CREATE path (the row is
// repo-aware and records no worktree) and refuses, correctly, because a ground
// directory is `gopi-` + the row's uuid with its dashes removed and no such
// name can be derived from `t-foo`. So a worker burns a claim, the run dies,
// and the ledger collects `failed-needs-reissue` litter — for an argument the
// operator could have been told about at the moment he typed it.
//
// WHERE THE PREDICATE LIVES, and why it is these two facts and no others. The
// dispatch gate refuses when ALL THREE hold:
//
//	① the row id is not a full uuid (fullid.IsUUID — the ONE definition of
//	   "this id can name a uuid-derived ground", Rule 14),
//	② the row needs a DERIVED ground (repo-aware: deliverable.repo non-empty),
//	③ and one cannot be REUSED either (no live recorded deliverable.worktree).
//
// The submit face can see ① and ② — the id it was handed and the repo it just
// resolved — and it CANNOT see ③, because the row does not exist yet and a
// fresh row records no worktree. So the submit predicate is the sound
// under-approximation of the gate: it fires only where the gate would
// certainly fire, and it never claims more than it knows (a row that the gate
// could still rescue by reuse is not refused early — which is what keeps this
// additive; the dispatch gate keeps its exact behaviour as the backstop).
//
// WHY IT SITS IN THE ENGINE (Rule 13/14, one definition). The refusal text is
// taskGroundNameRefusal's — the SAME sentence the operator would have read at
// dispatch time, naming the cause, the consequence and the door. A second
// wording for the same condition would be a second definition, and the two
// would drift.
//
// WHY THE CALLER DECLARES ITSELF, AND WHY THE ENGINE DOES NOT GUESS. The check
// is asked only when TaskSubmitOptions.RefuseUngroundableID is set — the
// `gopi task submit` CLI face sets it; library callers leave it false.
//
// That is the MEASURED boundary, not a preference. The unconditional form
// (test the gate's condition here: non-uuid id AND repo-aware) was written
// first and a full budget run over `./internal/tools ./cmd/agent` came back
// with 410 failing tests, twice independently: a fixture submits a slug id
// (`t-tie-zzz`, `t-attrib-live-runner`, `t-start-stamp`) against an EXPLICIT
// repo — the main checkout — and never resolves a ground at all. The condition
// was right; the boundary was not, because repo-aware is the normal shape for a
// fixture that cares about deliverables and `t-*` is the normal shape for its
// id. The engine cannot measure "a human typed this id"; the caller can. So the
// caller says so, and the CONDITION and the SENTENCE stay single-definition —
// the flag governs only WHEN they are asked (Rule 13/14 preserved).
//
// The predicate itself is still exactly the dispatch gate's, so the two cannot
// drift: TaskSubmitGroundNameDenied is fullid.IsUUID + "the row is repo-aware",
// and the text comes from taskGroundNameRefusal. A row that already exists with
// a non-uuid id keeps its current verdict at the gate — this is an additive
// early refusal, never a moved gate.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/fullid"
)

// TaskSubmitGroundNameDenied reports whether a submit of this row id must be
// REFUSED because the row could never name a ground: the id cannot become a
// derived ground name AND the row is repo-aware, so dispatch will take the
// derive-or-create path and refuse there.
//
// It is the submit-side reading of the dispatch gate's condition (see the file
// header). The two NEGATIVES are the load-bearing half: a full-uuid id, and a
// row that resolves no repo (not repo-aware — nothing is derived, and dispatch
// starts the child in the launcher's directory by design).
func TaskSubmitGroundNameDenied(taskID, repo string) bool {
	id := taskIDNormalize(taskID)
	if id == "" || fullid.IsUUID(id) {
		return false
	}
	return strings.TrimSpace(repo) != ""
}

// TaskSubmitRefusalFace is the name the submit-time refusals carry — the face
// the operator was standing on when he was refused, so the sentence says WHICH
// moment produced it (the same condition reported at dispatch reads as that
// gate's own). One definition, used by the refusal and asserted by the CLI test
// (Rule 1b/13).
const TaskSubmitRefusalFace = "task_submit"

// TaskSubmitGroundNameRefusal returns the refusal a submit must be aborted with
// when the row id it was handed can never name a ground, and nil when the submit
// may proceed.
//
// The message is taskGroundNameRefusal's, wrapped with the caller's own
// context ("task_submit:"), so the operator reads at submit time the exact
// sentence the dispatch would have produced later — same cause, same
// consequence, same door.
func TaskSubmitGroundNameRefusal(taskID, repo string) error {
	if !TaskSubmitGroundNameDenied(taskID, repo) {
		return nil
	}
	return fmt.Errorf(TaskSubmitRefusalFace+": %w", taskGroundNameRefusal(taskIDNormalize(taskID), repo))
}
