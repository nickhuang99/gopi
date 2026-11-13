// Deliverable selector → the SESSION a row is running: "I am standing in a task
// worktree / I have the branch name — which session is that?".
//
// WHY IT EXISTS. `gopi session log`, `gopi session raw` and `gopi session stats` each address a session
// by <session-id> (or --last / --all / --file), and the id is exactly what the
// operator standing in a task worktree does NOT hold: they hold `git branch
// --show-current` or `pwd` — the same two addresses `gopi task show --branch`
// and `gopi worker --resume --branch` already accept (PROJECT.md Rule 31's
// motivating shape: the surface nobody can type is the surface nobody uses).
// This file adds the one step those commands do not need — the ROW → the
// session that row is running — so the three session verbs can accept the SAME
// address as a drop-in REPLACEMENT for the positional <session-id>.
//
// WHY THE SELECTION RULE IS NOT HERE. It is ResolveTaskIDByDeliverable's
// (task_deliverable_selector.go → the live-first policy the merge fence uses). A
// second tie-break in this file would let `gopi session log --branch X` and `gopi task
// show --branch X` name DIFFERENT rows for one address — the drift Rules 13/14
// forbid.
//
// WHY THE LOG LOOKUP IS NOT HERE EITHER — and it is not a preference, it is a
// measured constraint. Resolving a row's recorded session id to the LOG ON DISK
// is gopilog's own job (gopilog.FindSession is the one owner of which layouts a
// session log lives in, and of what an operand means), and the three consumers
// of this brief (`gopi session log <id>`, `gopi session raw <id>`, `gopi session stats <id>`) hand the
// id straight BACK to exactly that resolver. Doing the path resolution here
// would be a SECOND answer to "where is this session's log?" — and it cannot be
// the gopilog one: internal/gopilog imports internal/agent, which imports
// internal/tools, so a tools → gopilog edge closes an import cycle
// (go build: "imports github.com/nick/gopi/internal/gopilog from
// task_deliverable_session.go → … → import cycle not allowed"). A hand-joined
// layout probe here would also be the staler answer: internal/tools' own
// resolution (taskFindSessionJSONLForRow) reads session heads for layouts
// gopilog resolves with one stat in the normal case (measured 2026-09-25:
// 3.2s → a stat over a 76k-entry tree).
//
// So the division is: THIS file resolves the row and returns the id the row
// records; the verb substitutes that id for its <session-id> positional and the
// existing dispatch resolves it — one resolution path, so "the id resolved but
// its log was not found" cannot happen as a disagreement, only as the ordinary
// "session not found" the verb already prints for a hand-typed id.

package tools

import (
	"fmt"
	"strings"
)

// DeliverableSessionSelector is the selector spellings this resolution accepts —
// the SAME type the row lookup takes (branch or worktree, exactly one), so there
// is no second spelling to keep in step.
type DeliverableSessionSelector = DeliverableSelector

// DeliverableSessionBrief is the answer: the resolved row plus the session it is
// running. SessionID is the OPERAND the caller substitutes for its <session-id>
// positional — the same string the operator would otherwise have had to copy out
// of `gopi task show`, so the resolution is a SUBSTITUTION into the existing
// dispatch and never a second grammar.
type DeliverableSessionBrief struct {
	// TaskID is the resolved row (the live owner of the selector).
	TaskID string
	// SessionID is the session the row currently records — the form every
	// `<session-id>` surface already accepts.
	SessionID string
	// Owners / Readable / RowsWithBranch come straight from the row resolution,
	// so the caller renders the plurality note from the SAME data the choice was
	// made from (Rule 13: one definition, one render source).
	Owners         []TaskForBranchOwner
	Readable       bool
	RowsWithBranch int
}

// ResolveSessionByDeliverable resolves a deliverable selector (--branch NAME or
// --worktree PATH) to the session the ONE row behind it is running.
//
// The refusals are named and distinct because their repairs differ:
//
//   - both selectors, or neither: a usage error raised by the row resolver;
//   - no ledger row names it: the row resolver's own sentinel
//     (ErrDeliverableNoOwner), propagated unchanged so a caller can tell it from
//     a read failure;
//   - the row records NO session: nothing has run for it yet (a pending row),
//     which is a different answer from a run whose session has been recorded.
//
// It is a PURE READ: no claim, no status move, no ledger write, no git command.
func ResolveSessionByDeliverable(sel DeliverableSelector, dbPath string) (DeliverableSessionBrief, error) {
	var out DeliverableSessionBrief
	res, err := ResolveTaskIDByDeliverable(sel, dbPath)
	if err != nil {
		return out, err
	}
	out.TaskID, out.Owners = res.TaskID, res.Owners
	out.Readable, out.RowsWithBranch = res.Readable, res.RowsWithBranch
	sessionID := strings.TrimSpace(taskSessionIDForRow(res.TaskID))
	if sessionID == "" {
		return out, fmt.Errorf("task %s (%s %q) records no session — nothing has run for it yet, so there is no session log to read",
			res.TaskID, deliverableSelectorKindOf(sel), deliverableSelectorValueOf(sel))
	}
	out.SessionID = sessionID
	return out, nil
}

// deliverableSelectorKindOf / deliverableSelectorValueOf name WHICH spelling a
// selector used and its value — one definition, so a message can never name the
// branch when the operator asked about a worktree.
func deliverableSelectorKindOf(sel DeliverableSelector) string {
	if strings.TrimSpace(sel.Worktree) != "" {
		return deliverableSelectorWorktreeKind
	}
	return deliverableSelectorBranchKind
}

func deliverableSelectorValueOf(sel DeliverableSelector) string {
	if strings.TrimSpace(sel.Worktree) != "" {
		return strings.TrimSpace(sel.Worktree)
	}
	return strings.TrimSpace(sel.Branch)
}
