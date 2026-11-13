package tools

// chat_write_guard_readonly.go — the CLEANUP FACES a chat may run, and the
// authorization channel its destructive half is gated on.
//
// WHAT USED TO LIVE HERE, AND WHY IT IS GONE. This file was the READ-ONLY
// SUBCOMMAND TABLE: a per-verb map of the git words that only query
// (`worktree list`, `branch --list`, `stash show`), built because the old guard
// refused a verb by NAME and therefore refused pure queries too. That whole
// apparatus — the table, the verdict, the rendered spelling the prompt taught —
// was deleted with the verb list it compensated for: the bash boundary now asks
// the OPERATOR's rule table (internal/tools/chat_policy_gate.go) instead of
// classifying git's grammar, so a read is admitted by a rule that says so and not
// by a second table that must be kept in step with git.
//
// WHAT REMAINS is the cleanup faces and their authorization channel — the part
// that is ABOUT the operator's turn rather than about a command word. There is no
// read-only table here: a read is admitted because a rule says `allow` (or because
// the posture does), and no second table has to be kept in step with git's own
// grammar.

import (
	"strings"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/taskverbs"
)

// --- the cleanup faces -----------------------------------------------------
//
// The TWO repository faces that already reclaim a finished run's ground
// (Rule 3/35: no new tool, no new script — the console drives what the hooks and
// the merger already run). They are named here, in a leaf both readers can
// import, because the chat prompt must teach them, the guard must recognise them,
// and `internal/agent` cannot import `cmd/agent` (where the verbs' parsers live).
// Each word is taken from the constant that OWNS it (Rule 1/13).

// chatCleanupFace is ONE cleanup face: the invocation a chat may run, and the
// flag that makes it destructive.
type chatCleanupFace struct {
	// invocation is the command words (`gopi task finish-cleanup`).
	invocation string
	// applyFlag is the destructive opt-in of that face (its absence is a plan).
	applyFlag string
	// planFlag is the explicit spelling of the read-only default.
	planFlag string
}

// ChatTaskApplyFlag / ChatTaskDryRunFlag are the two switches the cleanup faces
// share. They are the values the guard compares a command line against, so they
// live here once (Rule 1/13): cmd/agent's own flag walk reads the same two names
// through its bare-flag table, which is why the dashed spelling is defined HERE
// and nowhere else.
const (
	ChatTaskApplyFlag  = "--apply"
	ChatTaskDryRunFlag = "--dry-run"
)

// chatCleanupFaces is the ONE list of the cleanup faces a chat may run under the
// operator's authorisation (Rule 13/14).
func chatCleanupFaces() []chatCleanupFace {
	return []chatCleanupFace{
		{
			invocation: runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.SweepWorktrees,
			applyFlag:  ChatTaskApplyFlag,
			planFlag:   ChatTaskDryRunFlag,
		},
	}
}

// ChatCleanupFaceInvocations renders the cleanup faces as the words a reader
// pastes, for the prompt and the refusals (Rule 13/14: one list, two readers).
func ChatCleanupFaceInvocations() []string {
	faces := chatCleanupFaces()
	out := make([]string, 0, len(faces))
	for _, f := range faces {
		out = append(out, f.invocation)
	}
	return out
}

// chatCleanupFacesRendered renders those faces as a backticked list.
func chatCleanupFacesRendered() string {
	faces := ChatCleanupFaceInvocations()
	quoted := make([]string, 0, len(faces))
	for _, f := range faces {
		quoted = append(quoted, "`"+f+"`")
	}
	return strings.Join(quoted, ", ")
}
