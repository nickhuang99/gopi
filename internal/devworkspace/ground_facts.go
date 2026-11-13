package devworkspace

// ground_facts.go — the ONE renderer of the "Ground facts" summary sentence.
//
// WHY IT EXISTS (measured 2026-10-27, row 01a17a48). A summary line inserted
// into a run's context said, in ONE sentence:
//
//	Ground facts: /home/nick/workspace/01a17a24-… is a LINKED WORKTREE (.git is
//	a FILE), on branch master, tree clean, HEAD=46a448d, diverged vs
//	origin/master: 1 ahead / 6 behind. No rebase in progress (no
//	.git/rebase-merge|apply).
//
// Two clauses of it cannot both be true at once. Measured in that very
// directory, at that very moment:
//
//	$ test -d .git && echo DIRECTORY || echo FILE      → DIRECTORY   (a CLONE)
//	$ git rev-parse --git-dir --git-common-dir         → .git / .git (a CLONE)
//	$ git worktree list                                → itself only (a CLONE)
//
// and the source of the sentence is on disk: the same run's own `test -d .git`
// printed `.git is FILE = linked worktree`, because an `ls -d .git/rebase-merge`
// EARLIER IN THE SAME `&&` CHAIN had exited 2 and short-circuited the chain
// straight into the `||` arm. The shape clause was the error branch of a failed
// probe — a sentence whose truth depended on a measurement that had not been
// taken.
//
// WHY A RENDERER AND NOT A PATCHED SENTENCE: Rule 6c gives a CLONE and a LINKED
// worktree OPPOSITE delivery instructions (the clone channel's loop publishes a
// clone's commit; a linked worktree's commit is already in the shared repo). A
// run that believes the wrong one reports itself finished without publishing —
// the exact defect shape. So the sentence must be RENDERED FROM the measured
// shape, by ONE code path, and this is that path:
//
//   - the SHAPE half comes from `GroundModelOf` — the one discriminator
//     (Rules 13/14), never from a second opinion;
//   - the REBASE half comes from `GroundRebaseProbe`, which asks git where its
//     own rebase markers live (`git rev-parse --git-path`), so it is correct for
//     BOTH shapes — and reports UNDETERMINED rather than "no rebase" when it
//     cannot tell (Rule 15: an unmeasured factor is never the reassuring
//     verdict);
//   - a shape that cannot be decided is SAID to be undecided, and the delivery
//     consequence is left explicitly unanswered rather than guessed.

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The labels of the rendered line: exported because the regression tests assert
// the sentence from this package and a mirrored literal is the drift Rules
// 1b/13 forbid.
const (
	// GroundFactsLabel is the line's leading marker.
	GroundFactsLabel = "Ground facts:"
	// GroundFactsCloneShape / GroundFactsLinkedShape / GroundFactsUnknownShape are
	// the THREE shape clauses. There is deliberately no fourth "maybe": each is
	// reached only from a measured model, and the unknown clause names what was
	// measured instead of picking a side.
	GroundFactsCloneShape = "a CLONE (a standalone repository of its own: .git is a DIRECTORY, and " +
		"`git rev-parse --git-common-dir` prints `.git`) — its commits are published by the clone channel's " +
		"DELIVERY LOOP, so nothing is on the server until that loop lands it"
	GroundFactsLinkedShape = "a LINKED WORKTREE (a guest checkout: .git is a FILE naming a gitdir under the " +
		"main checkout) — it shares the main repo's refs and object store, so a commit here is already on the " +
		"shared branch and no push is needed for it"
	GroundFactsUnknownShape = "of UNDETERMINED shape (%s) — whether its commits are already shared with the " +
		"task's repo therefore has NO answer here; measure it before reporting delivery " +
		"(`test -d .git`: a DIRECTORY is a clone, whose commit the channel must publish; a FILE is a linked " +
		"worktree, whose commit is already shared), and do not assume either answer"
)

// GroundFactsLine renders the summary sentence for dir: which ground it is
// (from the ONE discriminator), the branch it stands on, and its rebase state
// (from git's own answer about where that state lives).
//
// It is a PURE READ, and it never invents a fact: every clause is either a value
// this package measured or an explicit statement that the value was not
// measured. That is the whole contract — the sentence it replaces asserted a
// shape from a probe that had failed.
func GroundFactsLine(dir string) string {
	st := ResolveGroundState(dir)
	return GroundFactsLineForState(st)
}

// GroundFactsLineForState renders the sentence from an ALREADY RESOLVED state —
// the seam the regression tests drive both ground shapes through without
// materialising a second directory, and the shape a caller that already holds a
// GroundState uses so the two faces cannot disagree (Rule 14).
func GroundFactsLineForState(st GroundState) string {
	dir := st.Dir
	if strings.TrimSpace(dir) == "" {
		dir = "(no directory)"
	}
	var b strings.Builder
	b.WriteString(GroundFactsLabel)
	b.WriteString(" ")
	b.WriteString(dir)
	b.WriteString(" is ")
	b.WriteString(groundFactsShapeClause(st))
	b.WriteString(", on branch ")
	b.WriteString(groundAxisOrNone(st.Branch))
	b.WriteString(", ")
	b.WriteString(groundFactsRebaseClause(st.Rebase))
	b.WriteString(".")
	return b.String()
}

// groundFactsShapeClause is the SHAPE half, and it is a total function of the
// measured model: a clone, a linked worktree, or an explicit undetermined
// clause naming what was looked at. A "broken-link" (a `.git` FILE whose gitdir
// is gone) is reported as its own case rather than being rounded to either
// side, because its delivery consequence is a third thing: the registration is
// gone, so there may be no shared repo left to be "already on".
func groundFactsShapeClause(st GroundState) string {
	switch st.Model {
	case GroundModelClone:
		return GroundFactsCloneShape
	case GroundModelLinkedWorktree:
		return GroundFactsLinkedShape
	case GroundModelBrokenLink:
		return fmt.Sprintf(GroundFactsUnknownShape,
			"a .git FILE whose gitdir does not resolve — the linked-worktree registration is GONE, so the "+
				"shared repo this directory used to belong to cannot be named; "+st.Detail)
	case GroundModelMissing:
		return fmt.Sprintf(GroundFactsUnknownShape, "no .git entry in this directory; "+st.Detail)
	default:
		return fmt.Sprintf(GroundFactsUnknownShape, "model "+string(st.Model))
	}
}

// groundFactsRebaseClause is the REBASE half. `RebaseStateNone` renders a claim
// backed by the paths git resolved; `RebaseStateInProgress` renders git's
// finding; `RebaseStateUndetermined` renders the question as UNANSWERED, which
// is the clause the broken sentence got wrong by asserting "no rebase" from a
// probe that could not have found one either way.
func groundFactsRebaseClause(p RebaseProbe) string {
	switch p.State {
	case RebaseStateNone:
		return "no rebase in progress (" + p.Detail + ")"
	case RebaseStateInProgress:
		return "A REBASE IS IN PROGRESS — " + p.Detail
	case RebaseStateUndetermined:
		return "rebase state UNDETERMINED (" + p.Detail + ")"
	default:
		return "rebase state UNDETERMINED (the probe returned no state at all)"
	}
}

// GroundFactsDeliveryClause renders the ONE delivery consequence Rule 6c draws
// from the measured shape: who publishes this ground's commits. "" when the
// shape is undetermined — a caller may say the shape is unknown, but it may not
// be handed an instruction that presumes one (Rule 15: unknown is never a
// default).
func (s GroundState) GroundFactsDeliveryClause() string {
	switch s.Model {
	case GroundModelClone:
		return "publishing is the clone channel's job (its DELIVERY LOOP runs once per run from the END hook): " +
			"commit on this ground's branch and leave the tree clean — do NOT push, and do not invent a ref"
	case GroundModelLinkedWorktree:
		return "no push is needed: committing here already updates the branch in the shared repo"
	default:
		return ""
	}
}

// RebaseDirName keeps the resolved merge-marker path's base name in one place
// for callers that only want to NAME the directory in a message; it returns ""
// when the probe did not resolve it.
func (p RebaseProbe) RebaseDirName() string {
	if strings.TrimSpace(p.MergePath) == "" {
		return ""
	}
	return filepath.Base(p.MergePath)
}
