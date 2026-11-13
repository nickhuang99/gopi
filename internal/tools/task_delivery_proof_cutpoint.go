package tools

// task_delivery_proof_cutpoint.go — THE PRODUCED-NOTHING GATE the delivery
// proof asks BEFORE any containment arm, and the two measurements it needs.
//
// WHY IT EXISTS (measured 2026-10-28). Every containment arm asks "does the
// destination carry what the run produced?". A ground whose HEAD is the very
// commit its cut-time snapshot names produced NOTHING, and the ancestry arm then
// answers "contained" for a trivial reason: the tip is an ancestor of ITSELF.
//
// The red proof is the sentence the arm prints with the gate short-circuited
// (this commit's companion test), with the gate's own condition turned off:
//
//	row dpg-empty-run: master (the ground's HEAD) is delivered —
//	7b868a540c06 is an ancestor of 7b868a540c06def2ab87f44e5d766eb7eed5d55a
//
// "7b868a540c06 is an ancestor of 7b868a540c06" is a statement about the tip's
// relationship to itself, not a fact about any delivery — and `delivered` is the
// signal that RELEASES the ground.
//
// A CHAT IS THE CASE THAT FORCES IT, and it is not hypothetical: Rule 37 gives a
// `gopi chat` session a ground of its own and nothing commits into it, so a
// chat's ground sits exactly on its cut point for the whole session. A worker
// run reaches the same shape by failing before it produces a commit.
//
// The honest answer is the third one: `unknown` — it says the run produced
// nothing, refuses nothing (so the row still closes) and still records the fact
// on the trail (Rule 15: silence is not an answer).

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func deliveryProofTipDir(repo, ground string) string {
	if g := strings.TrimSpace(ground); g != "" {
		if _, err := os.Stat(g); err == nil {
			return g
		}
	}
	return strings.TrimSpace(repo)
}

// tipIsTheCutPoint reports whether the tip a run claims is the very commit its
// ground was cut from — i.e. the run produced NO commit of its own.
//
// ONLY THE GROUND CAN ANSWER IT, and that is why this is not the "is the tip an
// ancestor of the integration branch" question: the ground holds both objects,
// so the ancestry test is meaningful. Asked of the live integration tip instead,
// the same test would be answered by the tip being an ancestor of ITSELF — a
// tautology that would call every delivered row, and every empty run, a
// delivery.
//
// THE CUT POINT IS READ FROM THE BRANCH'S OWN REFLOG, NOT FROM
// `refs/remotes/origin/master` — and that is a measured correction (row
// 01a1810a, 2026-10-28). The clone channel delivers with
// `git push origin HEAD:<integration branch>` (devworkspace.publishGround's ONE
// refspec), and a successful push ADVANCES the clone's own
// `refs/remotes/origin/master` to the pushed tip. So at delivery-proof time the
// old instrument read `origin/master == tip` for every run that had just
// delivered and answered "this run produced NO commit of its own" — a FALSE
// NEGATIVE on a delivered row (the proof hid a landing instead of proving it).
// Reproduced end to end in a scratch clone:
//
//	push#1      HEAD=a091808  origin/master=a091808
//	before #2   HEAD=dc3809a  origin/master=a091808
//	AFTER  #2   HEAD=dc3809a  origin/master=dc3809a   ← the push moved the witness
//
// The branch reflog's OLDEST entry is the creation position and a later commit
// or push never moves it (same scratch clone: `reflog show --format=%H master |
// tail -1` stayed at the cut commit while `origin/master` followed the push).
// That instrument already exists in this package — GroundCutPoint reads exactly
// it off the branch's reflog for the briefing — so this is one reader, not a
// second one (Rules 8/13).
//
// False means either "the run is AHEAD of its cut point" (it produced
// something), "the cut point could not be read" (no ground, no reflog — the
// question was never answered), or "no ground at all". All three fall through to
// the arms below, which is the correct behaviour for each: an unread cut point
// must never manufacture a "produced nothing" verdict, because that verdict is
// what the reader sees INSTEAD of a delivery.
// GroundProducedNothingRead is THE answer of the produced-nothing question about
// a GROUND, with its evidence — three facts kept apart because two callers need
// different halves of them and folding any two together is how one fact starts
// wearing two opposite sentences (Rule 15).
type GroundProducedNothingRead struct {
	// Measured is false when the question could not be asked at all (no ground,
	// no reflog, an unreadable HEAD). An abstention is NEVER a claim in either
	// direction, so a caller that ACTS on this must require Measured.
	Measured bool
	// Nothing is true when the ground's HEAD is the very commit its branch was
	// cut at — the run produced no commit of its own.
	Nothing bool
	// Pending is true when the ground's worktree carries uncommitted changes.
	// It matters because "no commit yet" and "nothing that COULD be committed"
	// are different states: a dirty ground still holds work a delivery round can
	// turn into a commit, so a caller that would skip the round must also see
	// this false.
	Pending bool
	// Evidence names what was read, so a reviewer can re-take it.
	Evidence string
}

// TaskGroundProducedNothing asks the produced-nothing question about a GROUND,
// by itself, and returns the reading WITH its evidence. It is the ONE exported
// face of `tipIsTheCutPoint` (Rule 14: one criterion, one reader), added
// because a SECOND seam needs the same answer — the clone channel's delivery
// loop must not spend rounds on a ground that can never carry a commit
// (`cmd/agent/devworkspace_channel.go`), and it may not grow an opinion of its
// own about reflogs.
//
// The cut point comes from the branch's own reflog (GroundCutPoint), never from
// `refs/remotes/origin/master`: the channel's delivery push ADVANCES that ref,
// so the tracking ref answers "produced nothing" for runs that just delivered.
func TaskGroundProducedNothing(ground, branch string) GroundProducedNothingRead {
	ground = strings.TrimSpace(ground)
	branch = strings.TrimSpace(branch)
	if ground == "" || branch == "" {
		return GroundProducedNothingRead{Evidence: "not attempted: the ground directory or the branch is not named"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	tip := deliveryProofGitRead(ctx, ground, "rev-parse", "HEAD")
	if tip == "" {
		return GroundProducedNothingRead{Evidence: fmt.Sprintf(
			"git -C %s rev-parse HEAD printed nothing, so the ground's own commit was never read", ground)}
	}
	read := GroundProducedNothingRead{
		Measured: true,
		Pending:  deliveryProofGitRead(ctx, ground, gitStatusVerb, gitStatusPorcelainFlag) != "",
	}
	cut := GroundCutPoint(ground, branch)
	if !cut.Measured() {
		return GroundProducedNothingRead{Evidence: fmt.Sprintf(
			"the cut point of branch %s could not be read here (%s), so this is an abstention, not an answer",
			branch, cut.Note)}
	}
	cutSHA := strings.TrimSpace(cut.SHA)
	read.Nothing = tip == cutSHA || tipIsTheCutPoint(ctx, ground, branch, tip)
	verdict := "is ahead of"
	if read.Nothing {
		verdict = "IS"
	}
	read.Evidence = fmt.Sprintf("git -C %s rev-parse HEAD = %s, cut point of branch %s = %s, so HEAD %s the cut point (%s); worktree pending changes: %t",
		ground, branchDeleteShortHash(tip), branch, branchDeleteShortHash(cutSHA), verdict, cut.Evidence, read.Pending)
	return read
}

// gitStatusVerb / gitStatusPorcelainFlag are git's own spellings for the ONE
// question "is anything uncommitted here", declared rather than typed (Rule 1b).
const (
	gitStatusVerb          = "status"
	gitStatusPorcelainFlag = "--porcelain"
)

func tipIsTheCutPoint(ctx context.Context, ground, branch, tip string) bool {
	ground = strings.TrimSpace(ground)
	branch = strings.TrimSpace(branch)
	tip = strings.TrimSpace(tip)
	if ground == "" || branch == "" || tip == "" {
		return false
	}
	cut := GroundCutPoint(ground, branch)
	if !cut.Measured() {
		return false
	}
	cutSHA := strings.TrimSpace(cut.SHA)
	if cutSHA == tip {
		return true
	}
	_, err := branchDeleteGit(ctx, ground, "merge-base", "--is-ancestor", tip, cutSHA)
	return err == nil
}
