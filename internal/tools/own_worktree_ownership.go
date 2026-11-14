package tools

// own_worktree_ownership.go — WHOSE registration is it? (the 2026-10-27 defect:
// an unowned directory was reported on an unrelated row's account).
//
// THE MEASURED DEFECT. The ownership-independent half of the close-out release
// (ReclaimSelfOwnRegisteredWorktrees) enumerates every repo the ledger names and
// then EVERY registration of each — and each registration was judged only by
// "may this be deleted?" (main/bare/prunable/locked, age, live holder, the dirty
// rung, the checklist). Nothing asked "is this THIS row's ground?". So a dirty
// registration no row had ever created was printed under whatever row happened to
// be closing out at that moment:
//
//	task 01a17dcd-c59c-7344-a99a-83133d4416c7: own-worktree-release: own registered worktree kept: /home/user/workspace/cptest-verify — worktree is DIRTY (uncommitted work) — left for a human
//
// The directory was a chat's hand-made verification checkout, the seven files in
// it belonged to a DIFFERENT task, and `01a17dcd`'s own commit was complete and
// correct. The FACT ("a directory is dirty, a human should look") was true and
// must keep being reported (Rule 15); the SUBJECT (`task <id>:`, i.e. this row's
// state) was false. Two halves, both required:
//
//	(a) a registration that is not this row's ground is reported under a NEUTRAL
//	    marker and never carries a row id — see reclaimSelfOwnUnownedMarker;
//	(b) "own" becomes a real question, answered by ONE resolver (GroundOwnership)
//	    whose shape half is devworkspace.GroundModelOf — the same call PROJECT.md
//	    Rule 6c makes the single answer for "what kind of ground is this", never a
//	    second copy of the `.git`-is-a-directory test.
//
// WHY THE MODEL MATTERS AND NOT JUST THE PATHS. The two ground models own their
// commits in OPPOSITE ways (Rule 6c): a LINKED worktree's commit is already on
// its branch in the shared repository, while a CLONE's commit is owed by the
// clone channel's delivery loop. Collapsing them into one ownership test is how a
// second, disagreeing answer gets invented — so the resolver switches on the
// measured model and says which model it decided by.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/devworkspace"
)

// ownGroundRow is ONE ledger row as the ownership question sees it: the row is
// the SUBJECT (it is this process's own closing row), and its deliverable names
// the ground it owes a release for.
type ownGroundRow struct {
	// TaskID is the row's id — the token a `task <id>:` report line carries.
	TaskID string
	// Worktree is the row's recorded `deliverable.worktree` ("" when the row
	// names none: a submit-time placeholder, or a clone-channel row).
	Worktree string
	// Branch is the row's recorded `deliverable.branch` ("" when none).
	Branch string
}

// ownGroundVerdict is the resolver's answer about one registration.
type ownGroundVerdict struct {
	// Own reports whether the registration IS the ground of one of the rows in
	// question — the only condition under which a report may say `own` and be
	// printed under that row's id.
	Own bool
	// TaskID is the row it is the ground of ("" unless Own).
	TaskID string
	// Evidence is the ONE sentence saying which test decided it, so a non-own
	// line an operator reads carries its reason rather than a verdict with
	// nothing behind it.
	Evidence string
}

// The evidence phrases, one spelling each, so the sentence an operator reads and
// the sentence a test asserts on can never drift apart (Rule 1b).
const (
	// ownGroundRecordedWorktreePhrase — the registration is the directory the
	// row's own deliverable records (path identity; the model is not needed,
	// which is why this test holds for a clone-channel row too).
	ownGroundRecordedWorktreePhrase = "it is the directory row %s records as its ground (deliverable.worktree)"
	// ownGroundNamedBranchPhrase — the registration carries the branch the row
	// records, on a LINKED worktree (the model whose commit is already on that
	// branch in the shared repository).
	ownGroundNamedBranchPhrase = "it carries row %s's recorded branch %s, and it is a LINKED worktree (model %s)"
	// ownGroundDerivedBranchPhrase — the registration carries the branch DERIVED
	// from the row's id (TaskGroundBranchName), on a LINKED worktree: the clone
	// channel's ground, whose deliverable may name no branch because git cloned
	// the source's integration branch (see TaskGroundBranchName's doc).
	ownGroundDerivedBranchPhrase = "it carries the branch derived from row %s (%s), and it is a LINKED worktree (model %s)"
	// ownGroundNoRowPhrase — no row in question names this registration.
	ownGroundNoRowPhrase = "no closing row of this process records this directory or its branch"
	// ownGroundCloneModelPhrase — a CLONE carrying the branch that looks like a
	// row's. It is NOT owned by the branch test, and the sentence says why: a
	// clone's commit is owed by the clone channel, so a clone is only ever this
	// row's ground when the row records the DIRECTORY.
	ownGroundCloneModelPhrase = "it carries a row's branch but it is a CLONE (model %s), whose commit the clone channel owns — only a recorded deliverable.worktree can claim it"
	// ownGroundUnreadableModelPhrase — the registration's shape could not be read
	// as a ground at all, so the branch test declines rather than guessing.
	ownGroundUnreadableModelPhrase = "its ground model reads %q, so its branch cannot claim it"
	// ownGroundRowsUnreadablePhrase is the report line's clause when the SUBJECT
	// set could not be read at all: every registration is then unowned by
	// construction, and a reader must be able to tell that apart from "the ledger
	// says no row of mine is here" (Rule 15 — the two are different facts).
	ownGroundRowsUnreadablePhrase = "the ledger of this process's own rows is unreadable, so no registration can be shown to be a ground of mine"
)

// GroundOwnership answers "is this registration the ground of one of these rows?"
//
// IT IS THE ONE RESOLVER (Rules 13/14): the production pass and its tests ask
// here, and the shape half is devworkspace.GroundModelOf — never a second
// `os.Stat(<dir>/.git)` written next door.
//
// The two tests, in this order, and why that order:
//
//	① THE RECORDED DIRECTORY — the row's own deliverable names this path. This is
//	  the strongest evidence there is (the ledger itself says "my ground is
//	  here"), and it holds for BOTH models: a clone-channel row records its clone.
//	② THE RECORDED BRANCH — and only on a LINKED worktree. A registration whose
//	  branch is the row's own ground branch (recorded, or DERIVED from the row id
//	  by TaskGroundBranchName) is that row's ground even when the deliverable
//	  records another directory: that is exactly the launch-directory shape
//	  reclaim_own_ground.go documents (the worker creates its own ground and the
//	  row records that one, while the run was launched in a linked worktree of the
//	  same row's branch). On a CLONE the same branch is NOT ownership: every clone
//	  of the channel stands on its row's branch by construction, and its commit is
//	  owed by the clone channel, so a clone is claimed by ① alone.
//
// A row that records no branch and no matching directory claims nothing — and
// `main`/`master`/`archive/*` never claim anything through the branch axis
// (taskBranchExempt: those refs are shared by every row, so a name that maps back
// to no row must not be read as one).
func GroundOwnership(reg worktreeRegistration, rows []ownGroundRow) ownGroundVerdict {
	// ① the recorded directory — model-independent, so it is asked first.
	for _, row := range rows {
		if strings.TrimSpace(row.Worktree) == "" {
			continue
		}
		if sameWorkingPath(row.Worktree, reg.Path) {
			return ownGroundVerdict{Own: true, TaskID: row.TaskID,
				Evidence: fmt.Sprintf(ownGroundRecordedWorktreePhrase, row.TaskID)}
		}
	}
	// ② the branch, on a LINKED worktree only. The model is MEASURED through the
	// one discriminator (Rule 6c) — the same call the publish guard, the delivery
	// proof and the clone-residue arm make.
	branch := strings.TrimSpace(reg.Branch)
	if branch == "" || taskBranchExempt(branch) {
		return ownGroundVerdict{Evidence: ownGroundNoRowPhrase}
	}
	model := devworkspace.GroundModelOf(reg.Path)
	for _, row := range rows {
		recorded := strings.TrimSpace(row.Branch)
		derived := TaskGroundBranchName(row.TaskID)
		matchesRecorded := recorded != "" && recorded == branch
		matchesDerived := derived != "" && derived == branch
		if !matchesRecorded && !matchesDerived {
			continue
		}
		switch model {
		case devworkspace.GroundModelLinkedWorktree:
			phrase := ownGroundNamedBranchPhrase
			if matchesDerived && !matchesRecorded {
				phrase = ownGroundDerivedBranchPhrase
			}
			return ownGroundVerdict{Own: true, TaskID: row.TaskID,
				Evidence: fmt.Sprintf(phrase, row.TaskID, branch, string(model))}
		case devworkspace.GroundModelClone:
			return ownGroundVerdict{Evidence: fmt.Sprintf(ownGroundCloneModelPhrase, string(model))}
		default:
			return ownGroundVerdict{Evidence: fmt.Sprintf(ownGroundUnreadableModelPhrase, string(model))}
		}
	}
	return ownGroundVerdict{Evidence: ownGroundNoRowPhrase}
}

// ownGroundRowsFrom converts the ledger rows the close-out already loaded into
// the ownership view — ONE conversion, so the pass and its tests ask the resolver
// about the same fields they judge on.
func ownGroundRowsFrom(rows []reclaimDeadHolderRow) []ownGroundRow {
	out := make([]ownGroundRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, ownGroundRow{
			TaskID:   r.TaskID,
			Worktree: r.Worktree,
			Branch:   r.Branch,
		})
	}
	return out
}

// reclaimSelfOwnLine renders ONE report line for a registration, with the
// subject the verdict earned:
//
//	own      → the line PLUS the ownership sentence: the subject IS one of the
//	           closing rows, the sentence names it (`this close-out's row <id>`)
//	           and carries the evidence that decided it, so the claim is stated
//	           rather than left to the caller's prefix.
//	not own  → ReclaimSelfOwnUnownedLead + the line: the FACT is kept verbatim
//	           (Rule 15 — a dirty directory is still reported, with its reason),
//	           and the claim of ownership is not made — no row id anywhere.
//
// The refusal sentence is untouched in both branches, so nothing an operator
// greps today stops matching; the ownership sentence is APPENDED, so a line an
// operator matches on `<marker>: <path>` still matches.
func reclaimSelfOwnLine(v ownGroundVerdict, line string) string {
	if v.Own {
		return line + fmt.Sprintf(ReclaimSelfOwnOwnSubjectFmt, v.TaskID, v.Evidence)
	}
	return ReclaimSelfOwnUnownedLead + line
}

// ReclaimSelfOwnIsOwnLine reports whether a line this pass produced is about a
// registration that IS one of the closing rows' grounds — the ONE question the
// two producer seams (cmd/agent's wrapper tail and the worker round) must ask
// before they prefix a line with `task <id>:` (Rules 13/14; the alternative, a
// second spelling of the lead string in cmd/agent, is exactly the drift the
// 2026-10-27 defect came from).
func ReclaimSelfOwnIsOwnLine(line string) bool {
	return !strings.HasPrefix(line, ReclaimSelfOwnUnownedLead)
}
