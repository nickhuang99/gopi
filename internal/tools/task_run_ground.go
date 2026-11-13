package tools

// task_run_ground.go — THE CHILD'S GROUND IS MADE BEFORE THE CHILD EXISTS
// (epic worker-creates-child-ground, 2026-09-22; supersedes 01a0c637).
//
// THE DEFECT THIS CLOSES, stated exactly. The spawn seam always set the child's
// working directory (`cmd.Dir = childDir`, cmd/agent/cli_tasks.go) — the
// earlier diagnosis "os/exec has a Dir field, use it" was wrong. What was wrong
// is that childDir RESOLVED TO THE MAIN CHECKOUT: a repo-aware row whose ground
// had not been created yet has nothing to resolve to (deliverable.worktree
// empty, deliverable.repo = the main checkout), so the spawn was CORRECTLY
// configured to start in the wrong place. The wrong place is legal, so nothing
// complained, and the reclaim guard that scans /proc/*/cwd was HONESTLY BLIND:
// the live child was standing in the main checkout, not in the worktree whose
// removal was being considered. Measured 2026-09-22: three `task run` children
// all started in the main checkout, and branch fix/jieba-token-absorbs-search-kw
// had its worktree freed while its row was in_progress and its pid alive.
//
// THE RESOLUTION (the project owner's): the WORKER creates the ground, before
// the spawn, deterministically. Three fixed steps — create the worktree, check
// the branch out on it, spawn the child with cmd.Dir = that worktree. The name
// need not be pretty ("worktree/branch 名字起得不贴切也没有办法"); determinism
// beats a pretty name, and since 2026-10-07 the pretty name is not available
// at all: the branch IS the row's uuid, because that is what makes
// task ↔ branch ↔ worktree cross-comparable (see taskGroundAnnouncementFmt).
//
// WHY THE PRODUCER AND NOT THE GUARD: the ground used to be minted by the LLM
// mid-run (PROJECT.md Rule 6a tells the run to create its own worktree), which is
// precisely why it could not be known at spawn time. Making the worker the
// creator removes the whole class — the LLM no longer CAN start in the wrong
// place, so there is nothing to enforce by prose and nothing for the guard to
// be blind to.
//
// WHAT THIS FILE OWNS: the resolve-or-create half (the ground's PATH, the
// BRANCH, and the `git worktree add` that materialises it) and the ONE refusal
// text for a ground that cannot be made. The spawn seam owns the placement (it
// is the funnel every face shares) and the main-checkout assertion.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/worktreeroots"
)

// The ground's naming and its git verbs, each spelled once (Rule 1b/14).
const (
	// TaskGroundDirPrefix is the leading half of a ground directory's name:
	// <prefix><task-id stem> (see taskGroundIDStem). Derived from the ROW,
	// never guessed (the briefing's item 2) — two runs of the same row always
	// resolve the same path, so a second run REUSES the ground instead of
	// minting a sibling.
	TaskGroundDirPrefix = "gopi-"
	// TaskGroundBranchPrefix is the branch name minted for a row whose
	// deliverable names no branch: "task/" + the same row-derived stem. A
	// name that is merely serviceable is acceptable — what matters is that it
	// is DERIVED (item 2) and that it is not EXEMPT from deliverable
	// attribution (master/main/archive-* are: taskBranchExempt).
	TaskGroundBranchPrefix = "task/"
	// TaskGroundDirEnv / TaskGroundBranchEnv are the GROUND ANNOUNCEMENT pair
	// (epic ground-announcement, 2026-09-25): the two names the spawn seam
	// exports so the executor child is TOLD the ground it was spawned into,
	// instead of inferring one.
	//
	// WHY AN ANNOUNCEMENT IS THE FIX, and not a different ground: the worker
	// already creates the ground and spawns the child in it (TaskRunGroundFor,
	// cmd.Dir), and the child DOES work there — but nothing told the child so.
	// PROJECT.md Rule 6a instructs every run to create "a worktree for YOUR
	// BRANCH" (~/workspace/gopi-<descriptive> on fix/<descriptive>), and the
	// ground's branch is task/<uuid>, which matches nothing in that literal
	// test — so every run complied with Rule 6a and left an ORPHANED SPARE
	// worktree behind: never entered, never held, and named by no row, so no
	// reclaim path collects it (measured 2026-09-23: one task → two worktrees,
	// both refs carrying the very same commits). This pair is the missing half
	// of the mechanism; the GOPI.md clause is the other half.
	//
	// The names carry THIS task's ground, never a launcher's preference: the
	// child reads them to answer "which ground is mine?", so an inherited pair
	// would be a foreign claim about a directory this run never entered. The
	// reader (TaskGroundAnnounced) is what keeps that honest — see there.
	TaskGroundDirEnv    = "GOPI_RUN_GROUND"
	TaskGroundBranchEnv = "GOPI_RUN_GROUND_BRANCH"
	// taskGroundWorktreeVerb is git's worktree porcelain verb, taken from the
	// value this package already defines for the worktree commands it runs
	// (finishCleanupWorktreeVerb — Rule 13: the literal has ONE home).
	taskGroundWorktreeListVerb   = finishCleanupWorktreeListVerb
	taskGroundWorktreeListFlag   = finishCleanupWorktreeListFlag
	taskGroundWorktreeAddVerb    = "add"
	taskGroundWorktreeRemoveVerb = finishCleanupWorktreeRemoveVerb
	// taskGroundHeadArg is git's "the commit these refs resolve to" revision
	// argument: the ground's branch is cut from the integration branch the
	// task's repo currently has checked out, resolved by NAME at creation.
	taskGroundHeadArg = "HEAD"
	// taskGroundBaselineNoteSeparator joins the creation clause and the
	// baseline clause in one Detail line. It is a constant (Rule 1b) because
	// the degradation TEST asserts on the joined shape: a second spelling of
	// the separator would let the note land in a log line the test cannot see.
	taskGroundBaselineNoteSeparator = " — "
)

// The ground-name SHAPE, in one place (Rule 1b/13/14).
//
// WHY THE SHAPE IS A RULE AND NOT A STYLE (measured 2026-10-07, this epic).
// Three families of name coexist in ~/.git/worktrees on this host:
//
//	`gopi-01a115487cba717091f5430964f8e087` — the row's uuid, dash-stripped
//	                                          (the DERIVED shape; the only one
//	                                          this code is allowed to mint);
//	`gopi-masterchk` / `gopi-crelease`      — a human's own control worktree,
//	                                          named by hand, no row behind it;
//	`gopi-01a113e467767eb9a4c33cb1df1cdfbb` — a DERIVED name that merely LOOKS
//	                                          like a short id: it shares its
//	                                          first 8 hex characters with a
//	                                          different, real row
//	                                          (01a113e4-6730), which is how it
//	                                          was mistaken for "a name built
//	                                          from an 8-char prefix".
//
// The third shape is why the rule has to be spelled by LENGTH and not by
// "looks like a uuid": an 8-char prefix and a 32-char stem are both hex, and a
// name built from a prefix would be indistinguishable from a correct one by eye
// alone. So the assertion is exact — a derived stem is the row's uuid with its
// dashes removed, which is UUIDLen-shorter-dashes, always.
const (
	// taskGroundUUIDStemLen is the length of a uuid-derived stem: the row's
	// id in canonical 8-4-4-4-12 form (fullid.UUIDLen = 36), minus the four
	// dashes the stem drops. Anything shorter is a PREFIX (an id cut short),
	// anything longer is not an id at all.
	taskGroundUUIDStemLen = fullid.UUIDLen - 4

	// TaskGroundRole* classify a ground NAME by where it came from. The
	// classification is by shape, never by a lookup, because a control
	// worktree has no row to look up — that absence IS its definition.
	//
	// TaskGroundRoleUUIDDerived — `gopi-<32 hex>`: minted by this file from a
	// row's id. The only role gopi itself creates.
	TaskGroundRoleUUIDDerived = "uuid-derived"
	// TaskGroundRoleHumanControl — `gopi-<anything else>`: a worktree a person
	// made FOR THEMSELVES (`gopi-crelease`, `gopi-masterchk`). It is LEGAL —
	// nothing in gopi may claim it, free it, or attribute it to a row — but it
	// is also not a deliverable and never gains a row by being read.
	TaskGroundRoleHumanControl = "human-control"
	// TaskGroundRoleForeign — a directory that is not in the ground family at
	// all (no TaskGroundDirPrefix, or the bare prefix): not ours to judge.
	TaskGroundRoleForeign = "foreign"
)

// taskGroundStemIsUUIDDerived reports whether stem is a row's uuid with its
// dashes removed — the ONE shape this package mints (taskGroundIDStem).
//
// It is deliberately not a "contains hex characters" test: the whole point of
// the measurement above is that a SHORT hex stem (a prefix) is the failure this
// predicate exists to catch, so the LENGTH is the assertion.
func taskGroundStemIsUUIDDerived(stem string) bool {
	return len(stem) == taskGroundUUIDStemLen && hexStemOnly(stem)
}

// hexStemOnly reports whether every byte of s is a lowercase hex digit.
func hexStemOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

// taskGroundNameRefusalDoorFmt is the DOOR half of the sentence, and it is its
// own constant because the door was MEASURED to be narrower than the sentence
// claimed (2026-10-13, task 01a131f5). The advertised verb short-circuits on
// the REPO axis whenever the argument resolves to the repo the row already
// stores — and a linked work tree OF THAT REPO always does — so the operator
// pasted `wt3`, read "already names … — nothing to repair", and `deliverable.
// worktree` stayed unset: the exact outcome the refusal promised was reachable.
// The verb now performs the named repair (naming a live registered linked work
// tree of the row's own repo stamps the ground), so the sentence names the door
// AND the one qualification that still routes it: which path to paste.
const taskGroundNameRefusalDoorFmt = "; resubmit the row under a uuid id, or name the ground explicitly with %s (%s)"

// taskGroundNameRefusalDoorNote is that qualification, spelled once beside the
// door it qualifies (Rule 1b/13).
const taskGroundNameRefusalDoorNote = "run it from a linked work tree of the row's repo — the directory itself is the ground"

// taskGroundNameRefusal is the Rule-15 REFUSAL for a row whose id cannot name a
// ground: the id is not a full uuid, so any ground derived from it would either
// be a PREFIX-shaped name (indistinguishable from a hand-made one, and shared
// with every other row that shares that prefix) or a human-style slug.
//
// It states the cause, the consequence and the door (Rule 15: a refusal an
// operator cannot act on is indistinguishable from silence).
func taskGroundNameRefusal(taskID, repo string) error {
	return fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo,
		fmt.Sprintf("the task id is not a full uuid, so no ground name can be derived from it "+
			"(a ground directory is %s + the row's uuid with its dashes removed, %d hex characters — "+
			"id %q would name the ground %q, a prefix/slug shape that other rows and hand-made "+
			"control worktrees also carry)%s",
			TaskGroundDirPrefix, taskGroundUUIDStemLen, taskID,
			TaskGroundDirPrefix+taskGroundIDStem(taskID),
			fmt.Sprintf(taskGroundNameRefusalDoorFmt, TaskRepoRepairVerb, taskGroundNameRefusalDoorNote)))
}

// TaskGroundRole classifies a worktree DIRECTORY NAME by its provenance — the
// three families measured above, decided by shape alone.
//
// It answers "may gopi touch this?" and nothing else; it is NOT a row lookup
// (a human's control worktree has no row by construction, so a lookup could
// only ever report a false negative). Callers that need an OWNER must read the
// ledger; callers that need "is this even ours to consider" ask here.
func TaskGroundRole(name string) string {
	trimmed := strings.TrimSpace(name)
	if !strings.HasPrefix(trimmed, TaskGroundDirPrefix) {
		return TaskGroundRoleForeign
	}
	stem := strings.TrimPrefix(trimmed, TaskGroundDirPrefix)
	if stem == "" {
		return TaskGroundRoleForeign
	}
	if taskGroundStemIsUUIDDerived(stem) {
		return TaskGroundRoleUUIDDerived
	}
	return TaskGroundRoleHumanControl
}

// TaskGroundResolution is what a row's ground resolved to — the ONE answer
// shared by the spawn seam's placement decision and its main-checkout
// assertion, so the two cannot disagree about where the child stands.
type TaskGroundResolution struct {
	// Dir is the directory the executor child must be spawned in. Empty when
	// the row is not repo-aware (every row submitted without --repo), which
	// is the documented backward-compatible shape: such a row keeps the
	// launcher's directory and the caller REPORTS that (Rule 15).
	Dir string
	// Branch is the branch checked out in Dir when the caller knows it —
	// the row's recorded deliverable.branch, or the branch minted for it.
	Branch string
	// Repo is the row's deliverable.repo this was resolved against ("" when
	// the row names none).
	Repo string
	// MainCheckout is the repository's MAIN worktree — the one directory the
	// child must never call its ground. "" when it could not be resolved
	// (then the assertion has nothing to prove and says so).
	MainCheckout string
	// Created reports that THIS call made the ground (worktree + branch);
	// false means an existing ground was REUSED. Reported by name, never
	// silent: "created" and "reused" are different operator facts.
	Created bool
	// Detail is the one-line human record of what happened, for the seam's
	// log (Rule 15 — the decision is never invisible).
	Detail string
}

// TaskRunGroundFor resolves the ground an executor for taskID must be spawned// in, CREATING it (linked worktree + branch) when the row is repo-aware and no
// ground exists yet. It returns an error ONLY for the shapes that must REFUSE
// the run (the briefing's item 3): a repo-aware row whose ground cannot be
// made. A row that merely names no repo is not an error — it resolves to ""
// and the caller keeps the pre-existing launch directory.
//
// THE ORDER THAT HOLDS, and why it is this order:
//
//  1. A ground the row ALREADY RECORDS wins, if it still exists and is
//     usable. This is what makes the resolution additive: a run that created
//     its own worktree (Rule 6a) and was recorded by one of the existing
//     seams keeps exactly that directory, and a resumed run returns to the
//     ground its predecessor left behind rather than minting a second one.
//  2. The DETERMINISTIC ground (taskGroundDirFor) is considered next. It
//     exists already ⇒ reuse (a previous attempt of the same row). It does
//     not ⇒ create it.
//  3. Only if creation is impossible does this REFUSE. There is deliberately
//     NO fallback to the main checkout: a silent fallback is exactly what made
//     the defect invisible for a day.
//
// The return value is never the main checkout for a repo-aware row (item 4):
// the only path that could produce it is a row that itself RECORDS the main
// checkout as its worktree, and that is refused loudly rather than used.
func TaskRunGroundFor(taskID string) (TaskGroundResolution, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return TaskGroundResolution{}, fmt.Errorf("task ground: task_id must not be empty")
	}
	repo, branch, worktree := taskRowDeliverable(taskID)
	repo = strings.TrimSpace(repo)
	if repo == "" {
		// Not repo-aware: the pre-existing behaviour, and the caller reports
		// the fallback. Never an error (rows are submitted without --repo all
		// the time, and they run in the launcher's directory by design).
		return TaskGroundResolution{}, nil
	}
	// A ground name is DERIVED from the row's id, so a row that needs one
	// DERIVED cannot have a row id that is not a full uuid: refuse LOUDLY, by
	// name, before anything is created (Rule 15 — a silent slug-shaped ground
	// is exactly the ambiguity this epic removes).
	//
	// THE GATE SITS AT THE DERIVATION and nowhere earlier, which is what keeps
	// it from over-reaching. A row that RECORDS its own worktree (#1 below)
	// never mints a name, so its id is none of this rule's business — and a
	// legacy ledger id (`t-1788387314`) is a fact about history, not a name
	// this code would derive. Only the two DERIVING paths (#2, #3) depend on
	// the id being a full uuid, and those are exactly the paths guarded here.
	if !fullid.IsUUID(taskID) && liveDir(strings.TrimSpace(worktree)) == "" {
		return TaskGroundResolution{}, taskGroundNameRefusal(taskID, repo)
	}
	main := taskGroundMainCheckout(repo)
	if main == "" {
		// The row names a repo that git cannot answer for. Refuse loudly:
		// this is the "repo is configured but the ground cannot be created"
		// shape (item 3), and starting the child in the main checkout would
		// be the silent fallback this epic removes.
		return TaskGroundResolution{}, fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo,
			"the repo is not a git work tree (or git cannot be read), so its main checkout cannot be told from a task ground")
	}
	// (1) An already recorded ground wins — including its BRANCH, which is
	// what makes the reuse additive rather than a re-mint.
	if wt := liveDir(strings.TrimSpace(worktree)); wt != "" {
		if SameDir(wt, main) {
			// A row that records the MAIN CHECKOUT as its worktree is the
			// exact shape this epic forbids: its child would start in the
			// shared master checkout (item 4).
			return TaskGroundResolution{}, fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo,
				fmt.Sprintf("the row records the repo's MAIN checkout %s as its worktree — a repo-aware row must work in "+
					"a workspace of its own (a clone ground, or a linked worktree), never in the shared checkout", main))
		}
		return TaskGroundResolution{
			Dir: wt, Branch: strings.TrimSpace(branch), Repo: repo, MainCheckout: main,
			Detail: fmt.Sprintf("reused the recorded ground %s (branch %s)", wt, groundBranchOrDash(branch)),
		}, nil
	}
	// (2) The deterministic ground: derived from the row, so a second run of
	// the same row lands on the same directory instead of a sibling. The name
	// is the row's own (uuid-first); a name another row already holds is a
	// COLLISION and falls back to a fresh uuid-named branch (see
	// taskGroundBranchUnowned).
	dir := taskGroundDirFor(repo, taskID)
	branch = taskGroundBranchFor(taskID, branch, repo)
	if liveDir(dir) != "" {
		// The directory exists but the row does not record it — either an
		// earlier attempt of this row whose stamp was lost, or a directory git
		// does not know. Reuse it only when it is a registered ground of THIS
		// repo on the right branch; otherwise refuse (item 3 — a path taken by
		// a foreign checkout is a refusal, never an overwrite).
		got, err := taskGroundBranchAt(repo, dir)
		if err != nil || got == "" {
			return TaskGroundResolution{}, fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo,
				fmt.Sprintf("the ground path %s is taken by a directory that is not a registered linked worktree of this repo", dir))
		}
		if got != branch {
			return TaskGroundResolution{}, fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo,
				fmt.Sprintf("the ground path %s is taken by a worktree on branch %s, not the row's branch %s", dir, got, branch))
		}
		stampTaskGround(taskID, dir, branch)
		return TaskGroundResolution{
			Dir: dir, Branch: branch, Repo: repo, MainCheckout: main,
			Detail: fmt.Sprintf("reused the derived ground %s (branch %s)", dir, branch),
		}, nil
	}
	// (3) Create it. Nothing is written to the ledger before this succeeds, so
	// a refusal leaks no half-stamped row.
	//
	// THE BASE COMMIT (epic worktree-base-align, 2026-09-26): a new ground is
	// cut from the PEER's integration-branch tip when this host declares a peer
	// and can reach it, so the run starts aligned instead of N commits behind.
	// When the peer is declared but unreachable the run STILL STARTS (a down
	// peer must not stop local work) and the degradation is stated in the
	// resolution's Detail — see taskGroundPeerBaseline.
	base, baseNote := taskGroundPeerBaseline(repo)
	if err := taskGroundCreateFrom(repo, dir, branch, base); err != nil {
		return TaskGroundResolution{}, fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo, err.Error())
	}
	stampTaskGround(taskID, dir, branch)
	detail := fmt.Sprintf("CREATED the ground %s on branch %s", dir, branch)
	if baseNote != "" {
		detail += taskGroundBaselineNoteSeparator + baseNote
	}
	return TaskGroundResolution{
		Dir: dir, Branch: branch, Repo: repo, MainCheckout: main, Created: true,
		Detail: detail,
	}, nil
}

// TaskRunGroundRefusal renders the ONE operator-facing refusal for a ground that
// cannot be resolved or created (Rule 1b: the message shape lives here, not at
// the call site). The child is NOT spawned on this path, and the text is what
// the row records.
//
// It carries the REMEDY as well as the cause (epic 01a0d55b): a refusal that
// only names the dead directory tells the operator what is wrong and leaves
// them to invent the door. The remedy is the row's own repair surface
// (TaskRepoRepairVerb), which is what makes the four live rows fixable without
// hand-editing the ledger.
func TaskRunGroundRefusal(taskID string, err error) string {
	return fmt.Sprintf("gopi task: %s: %v — refusing to start the run in the repo's main checkout; %s",
		taskID, err, taskGroundRefusalRemedyFmt)
}

// TaskRepoRepairVerb is the ONE spelling of the command that re-points a row's
// deliverable.repo at a live git work tree (Rule 1b/13: the refusal text and
// the usage page name the same door from this constant, never from a literal).
const TaskRepoRepairVerb = "gopi task update <task_id> --repo <path-to-live-work-tree>"

// ChildInMainCheckout is the SPAWN SEAM'S ASSERTION (the briefing's item 4): a
// repo-aware row must never be executed from the repository's main checkout.
// It reports the refusal text and true when the directory the child is about to
// start in IS the main checkout or lies INSIDE it.
//
// WHY AN ASSERTION AND NOT ONLY THE RESOLUTION: the resolution above already
// refuses to RETURN the main checkout, so this looks redundant — and it is
// exactly the redundancy the defect calls for. The failure mode was not a wrong
// value; it was a value that was LEGAL and wrong (the main checkout IS a usable
// directory, so `cmd.Dir = <a legal directory>` succeeded and nothing
// complained). An assertion at the seam is what turns "this directory is
// legal" into "this directory is wrong FOR THIS ROW" — and it is the seam, not
// the resolver, that owns the child's actual placement.
//
// A repo-less row (Repo == "") has nothing to assert: it runs in the
// launcher's directory by design, and that is documented, not a defect. An
// unresolvable main checkout (MainCheckout == "") also asserts nothing — the
// caller already refused on that path — but says so rather than passing
// silently.
func (g TaskGroundResolution) ChildInMainCheckout(childDir string) (string, bool) {
	if strings.TrimSpace(g.Repo) == "" || strings.TrimSpace(g.MainCheckout) == "" {
		return "", false
	}
	dir := liveDir(strings.TrimSpace(childDir))
	if dir == "" {
		return "", false
	}
	if !SameDir(dir, g.MainCheckout) && !pathWithin(dir, g.MainCheckout) {
		return "", false
	}
	return fmt.Sprintf(taskGroundMainCheckoutRefusalFmt, g.Repo, g.MainCheckout, dir), true
}

// taskGroundMainCheckoutRefusalFmt is the ONE spelling of the main-checkout
// refusal: the repo, the shared checkout, and the directory that was about to
// be used.
const taskGroundMainCheckoutRefusalFmt = "refusing to start: the child would be spawned in %s — the MAIN checkout of repo %s (%s) — while the row names that repo; a repo-aware row must work in a workspace of its own (a clone ground, or a linked worktree), never in the shared checkout"

// taskGroundUnresolvableFmt is the ONE shape of a ground refusal's cause: the
// row, the repo, and the measured reason. The spawn seam wraps it in
// TaskRunGroundRefusal, which adds the consequence (the run does not start).
const taskGroundUnresolvableFmt = "cannot resolve the ground for task %s in repo %s: %s"

// taskGroundRefusalRemedyFmt is the OPERATOR'S REMEDY, appended by
// TaskRunGroundRefusal to every ground refusal.
//
// WHY IT IS PART OF THE REFUSAL (epic 01a0d55b): the refusal is correct and
// stays — but the four rows that hit it live (01a0d528-*, whose
// deliverable.repo was their parent run's deleted ground) left the operator
// with a dead end: the text named the row and the repo, never the door. The
// remedy is a real one, on an existing surface: the row's own deliverable is
// re-pointable at a repo that is a git work tree (`gopi task update <id>
// --repo <path>` — the same single blob writer, which resolves a linked
// worktree to the durable main checkout). The refusal names it so the reader
// does not have to derive it.
const taskGroundRefusalRemedyFmt = "remedy: re-point the row at a live git work tree (" +
	TaskRepoRepairVerb + ") — a linked worktree is resolved to its repository's main checkout"

// stampTaskGround records the ground on the row through the SAME single blob
// writer every other deliverable stamp uses (Rules 13/14: one blob writer, one
// ownership rule, one CAS). A stamp that cannot be written is REPORTED by that
// writer and the run continues: the ground itself was already measured correct
// (taskGroundCreate refuses loudly before this point), so a ledger hiccup must
// not stop a run whose directory is fine.
//
// TWO AXES, ONE WRITER each, and the second is why this is not a one-liner:
// the worktree/branch pair is stamped by the claimer writer, but when that
// writer declines the WORKTREE axis for a row that records no ground (its
// ownership rule cannot attribute a directory nobody declared), the BRANCH axis
// must still be recorded — a run that carries a branch and no worktree is a
// complete deliverable ground (epic 01a0b182), and leaving the row with neither
// would make the very run that created the ground unattributable at completion.
func stampTaskGround(taskID, dir, branch string) {
	repo := TaskDeliverableRepoFor(taskID)
	// THE RUN-GROUND ANCHOR (epic worktree-reclaim-two-gaps, Gap B): this is the
	// spawn seam's own moment of TAKING a ground (created or reused), and it is
	// the second producer of the anchor — the detection seam writes the other
	// one. Both call the same recorder, which keeps one line per directory.
	TaskGroundAnchorRecord(taskID, dir, branch)
	TaskWorktreeStampClaimerFor(taskID, repo, dir)
	if _, recorded, _ := taskRowDeliverable(taskID); strings.TrimSpace(recorded) != "" {
		return // the branch axis is on the row; nothing left to record
	}
	if strings.TrimSpace(branch) == "" {
		return
	}
	// The CLAIM moment, the existing spelling (Rule 1b/13): this stamp runs while
	// the row is still in flight, before any child exists — which is exactly what
	// that moment means (the creation moment belongs to the executor's own bash
	// round, internal/agent's detection seam).
	if _, err := taskBackfillDeliverableWorktreeAt(taskID, "", branch, &repo, taskWorktreeStampMomentClaim); err != nil {
		// Reported by the writer's own logs; the run keeps its ground.
		return
	}
}

// taskGroundIDStem is the row-derived name stem shared by the ground directory
// and the minted branch: the task id's characters, minus the ones a path or a
// ref cannot carry. It is DERIVED, never guessed (item 2).
//
// WHY THE WHOLE ID AND NOT A PREFIX. A short prefix is prettier and was the
// first shape, but it is not deterministic in the way this epic needs: two
// DIFFERENT rows whose ids share a prefix resolve to the SAME ground directory
// and, because the minted branch is derived the same way, to the SAME branch —
// so the second row would silently find a ground that belongs to the first and
// reuse it (measured: two tests in one binary sharing the default worktree root,
// one reusing the other's directory as "its" ground). Refusing on a branch
// mismatch does not close this — the branches match. The whole id closes it by
// construction: row ids are unique, so ground paths are unique, and a shared
// ground can only ever be a re-resolution of the same row.
func taskGroundIDStem(taskID string) string {
	id := taskIDNormalize(taskID)
	if id == "" {
		id = strings.TrimSpace(taskID)
	}
	stem := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return -1
	}, id)
	if stem == "" {
		// An id with no usable character at all: fall back to the sanitised raw
		// form rather than naming the ground after nothing (an empty stem would
		// make every such row share "gopi-").
		stem = strings.Trim(strings.Map(func(r rune) rune {
			if r == '-' || r == '.' || r == '_' {
				return r
			}
			return '-'
		}, strings.TrimSpace(taskID)), "-.")
	}
	return stem
}

// taskGroundDirFor is the deterministic ground directory of a repo-aware row:
// <that repo's worktree_root>/gopi-<task id stem>.
//
// The root comes from the ONE resolver every other consumer uses
// (config.ResolveRepoWorktreeRoot → the settings `repos[repo].worktree_root`
// entry, else the worktreeroots default) — the briefing's hard boundary: no new
// config knob, the existing declaration only.
func taskGroundDirFor(repo, taskID string) string {
	root, err := config.ResolveRepoWorktreeRoot(repo)
	if err != nil {
		// A broken settings file must not decide where a worktree goes: fall
		// back to the documented default root, which is what an undeclared
		// repo gets anyway (the resolver's own contract).
		r, rerr := worktreeroots.DefaultRoot()
		if rerr != nil {
			return filepath.Join(os.TempDir(), TaskGroundDirPrefix+taskGroundIDStem(taskID))
		}
		root = worktreeroots.Root{Path: r}
	}
	return filepath.Join(root.Path, TaskGroundDirPrefix+taskGroundIDStem(taskID))
}

// taskGroundBranchFor is the branch a NEW ground carries: the row's DERIVED
// name, TaskGroundBranchPrefix + the row's own stem — i.e. the task id itself,
// so task ↔ branch ↔ worktree stay cross-comparable from the names alone.
//
// A RECORDED branch is honoured only when it IS that derived name (a re-claim
// of this row's own ground) or is EXEMPT (master/main/archive-*, which is no
// deliverable at all and is replaced). A DESCRIPTIVE recorded name — fix/…,
// feat/…, task/crelease (the rename Rule 6a used to bless and no longer does)
// — is NOT inherited: inheriting it is what let a run's ref drift away from
// its row, so no ledger lookup could read the row off the branch.
//
// (A run RESUMED onto an existing ground keeps that ground's branch verbatim —
// that is the step-1 reuse path in TaskRunGroundFor, which runs before this
// function; resume is the one case the operator excepted.)
func taskGroundBranchFor(taskID, recorded, repo string) string {
	// The recorded name is deliberately NOT the input: whether a row records a
	// descriptive name or its own uuid, a MINTED ground carries the row's
	// derived name (or the fallback a taken name forces). See the doc above.
	_ = recorded
	return taskGroundBranchUnowned(taskID, repo, taskGroundBranchDerived(taskID))
}

// taskGroundBranchDerived is the row's own uuid-derived ground branch name —
// the ONE spelling, so the minting path, the existing-directory probe and the
// tests agree on what "this row's name" is.
func taskGroundBranchDerived(taskID string) string {
	return TaskGroundBranchPrefix + taskGroundIDStem(taskID)
}

// TaskGroundBranchName is taskGroundBranchDerived for callers OUTSIDE this
// package — the clone channel is one (internal/devworkspace reads the name from
// its Config, so it carries no dependency on this package; the two callers in
// cmd/agent and internal/agent fill it in through THIS function, never by
// re-spelling the prefix).
//
// WHY THE CLONE CHANNEL NEEDS IT AT ALL (the defect this closes, measured
// 2026-10-27): `git clone` checks out the SOURCE's branch — `<repo>`'s
// integration branch, i.e. `master` — so the workspace stood on the INTEGRATION
// branch and every ledger stamp derived from it (`TaskRunGroundForClone` reads
// `rev-parse --abbrev-ref HEAD`) recorded `deliverable.branch: master`. Two
// consequences, both measured: the branch→row lookup answered "361
// owners of branch master" (a name shared by every row of the channel, so the
// ref maps back to no row), and the run's commits sat on the CLONE's own
// `refs/heads/master` — a ref no server holds until the channel pushes it,
// under the very same shared name. The row's address, by contrast, is
// `task/<id>`: unique per row, resolvable by `gopi task show --branch`, and
// exactly what the WORKTREE channel already mints for every ground
// (taskGroundBranchFor above), so both channels answer "which ref carries this
// row's work?" with one spelling.
func TaskGroundBranchName(taskID string) string {
	if taskIDNormalize(taskID) == "" && strings.TrimSpace(taskID) == "" {
		return ""
	}
	return taskGroundBranchDerived(taskID)
}

// taskGroundBranchUnowned returns branch when a new ground may TAKE that ref
// name. The ref does not exist ⇒ take it (the ordinary case). The ref exists
// and the LEDGER says it is THIS row's ⇒ take it too: that is a re-claim of the
// row's own deliverable (its previous ground directory is gone but its ref
// survived), and re-attaching to your own branch inherits nothing foreign.
//
// Every OTHER shape is the operator's conflict case — "if conflict, i.e. name
// exists, then fallback to create new uuid": another row owns the ref, the
// ledger cannot be read to tell, or (no row names it at all) the ref carries
// content nobody can vouch for. In all three, the run mints a FRESH uuid-named
// branch rather than attaching to it, because `git worktree add <dir>
// <existing-branch>` would start this run on a base that is not its own and
// finish-cleanup's push-back would then fast-forward that ref with this run's
// commits. The cost of the fallback is a longer name; the cost of guessing is
// somebody else's deliverable.
//
// The verdict is the claim seam's own ownership predicate (taskBranchRowOwner —
// a ledger scan, no repo state), so the two seams cannot disagree about who
// holds a ref.
func taskGroundBranchUnowned(taskID, repo, branch string) string {
	if strings.TrimSpace(repo) == "" || !taskGroundBranchExists(repo, branch) {
		return branch // no repo to ask, or the name is free: take the row's name
	}
	fresh := TaskGroundBranchPrefix + taskGroundFallbackStem()
	owner, err := taskBranchRowOwner(taskID, repo, branch)
	switch {
	case err != nil:
		taskNotifyLog(taskGroundBranchUnprovenFmt, taskID, branch, err, fresh)
	case owner == taskID:
		return branch // this row's own ref — a re-claim, not a conflict
	case owner == "":
		taskNotifyLog(taskGroundBranchOrphanFmt, taskID, branch, fresh)
	default:
		taskNotifyLog(taskGroundBranchTakenFmt, taskID, branch, owner, fresh)
	}
	return fresh
}

// taskGroundFallbackStem mints the stem of a COLLISION fallback branch: a fresh
// uuid v7, run through the same sanitiser the row stem uses, so the fallback
// carries the same path- and ref-safe shape without being any row's id.
//
// The generation error is REPORTED, never swallowed (Rule 15): uuid v7 fails
// only when the kernel's randomness/clock is unreadable, and the caller still
// needs a NAME no existing ref can already hold, so the fallback is the pid
// plus the wall clock — not derived from any row, which is the property that
// matters here.
func taskGroundFallbackStem() string {
	id, err := uuid.NewV7()
	if err == nil {
		return taskGroundIDStem(id.String())
	}
	taskNotifyLog(taskGroundFallbackUuidFailedFmt, err)
	return fmt.Sprintf("%d%09d", os.Getpid(), time.Now().UnixNano())
}

// The collision fallback's two lines (Rule 1b: one spelling, read by the
// producer and whoever greps the log for why a branch is not a task id).
const (
	taskGroundBranchOrphanFmt       = "task %s: branch %s exists but no ledger row names it — minting the fresh uuid-named ground branch %s instead (a nameless ref's content cannot be vouched for)"
	taskGroundBranchTakenFmt        = "task %s: branch %s is already named by row %s — minting the fresh uuid-named ground branch %s instead (a run never attaches to another row's ref)"
	taskGroundBranchUnprovenFmt     = "task %s: cannot prove branch %s is unowned (%v) — minting the fresh uuid-named ground branch %s instead"
	taskGroundFallbackUuidFailedFmt = "task ground: uuid v7 generation failed (%v) — naming the collision fallback from pid+nanotime"
)

// groundBranchOrDash renders a branch for a human line ("" reads as "none").
func groundBranchOrDash(branch string) string {
	if b := strings.TrimSpace(branch); b != "" {
		return b
	}
	return "none"
}

// GroundBranchLabel renders a ground's branch for an operator record, with the
// project's ONE spelling for "the row named none" (Rule 1b/13/14: the producer
// and its records agree on the label because they read the same constant's
// function, never a re-typed literal).
func GroundBranchLabel(branch string) string { return groundBranchOrDash(branch) }

// TaskGroundAnnouncementClause is the ONE spelling of the line that ANNOUNCES
// the ground to the executor child — the briefing half of the announcement
// (epic ground-announcement; the env pair TaskGroundDirEnv/TaskGroundBranchEnv
// is the other half, and BOTH are emitted at the same spawn seam).
//
// WHY IT IS A BRIEFING LINE AT ALL, given the env pair: the env pair is only
// readable by a process that chooses to read it, while the briefing is the
// child's FIRST user message — it reaches the model before its first tool
// round, which is the only moment that matters here (the spare worktree is
// created IN that first round, by Rule 6a's instruction). A fact the model
// cannot see is a fact it cannot act on; measured 2026-09-25, `grep -c -i
// ground GOPI.md` was 2 and neither hit was about the mechanism.
//
// The shape is deliberately imperatively phrased (Rule 11: routing is what
// makes an instruction reach the model): it names the DIRECTORY, the BRANCH,
// and the three things the run must NOT do (create a second worktree, work in
// the repo's main checkout, RENAME the branch).
//
// WHY THE RENAME IS NOW FORBIDDEN (2026-10-07; this REVERSES an earlier
// blessing). The blessing was added so a run would not mint a SECOND worktree
// just to get a pretty name, and on that axis it worked. It paid with the one
// property the whole ground mechanism rests on: the branch and the directory
// are DERIVED from the task id (taskGroundIDStem), which is exactly what lets
// an operator — or the merger, or `gopi task show --branch` — cross-compare
// task ↔ branch ↔ worktree from the names alone. A rename severs that
// derivation and leaves a ref whose row can no longer be read off it; measured
// on this repo's own refs/heads: `task/crelease`, a name no ledger row can be
// looked up from (`gopi task show crelease` → no such row). The operator's
// ruling is that the uuid IS the name, and the human handle already exists and
// is better — the row's SUMMARY, which `gopi task show` / `task for-branch`
// reach from the uuid. So the pretty name is bought back by READING the row,
// never by mutating a ref; a run that wants to be descriptive records it in
// its report.
//
// An empty dir (a row that names no repo) renders "" — the caller emits
// nothing, because a clause naming no directory is worse than no clause
// (Rule 15: a record that cannot name its subject is not a record).
func TaskGroundAnnouncementClause(dir, branch string) string {
	d := strings.TrimSpace(dir)
	if d == "" {
		return ""
	}
	b := strings.TrimSpace(branch)
	if b == "" {
		b = groundBranchOrDash(b) // "none" — the pair is always announced TOGETHER
	}
	return fmt.Sprintf(taskGroundAnnouncementFmt, d, taskGroundShapeSentence(d, b))
}

// taskGroundShapeSentence is the ONE place the announcement's claim about the
// ground's SHAPE is decided — and it decides from a MEASUREMENT, not from which
// channel is asking (Rules 13/14: one definition, referenced everywhere).
//
// It is the fix for the measured 2026-10-27 defect: this row's own briefing said
// "it is a linked worktree of your task's repo" while the ground was a CLONE
// (`.git` a DIRECTORY, `git rev-parse --git-common-dir` printing `.git`), and
// the neighbouring publishing clause in the SAME briefing said "This ground is a
// CLONE". The two sentences disagreed because the shape sentence was a typed
// literal rendered unconditionally, for every ground of every channel.
//
// The three arms are the three shapes `devworkspace.GroundModelOf` can answer,
// and each carries the DELIVERY consequence of its own shape: a clone's commit
// is owed by the channel (NOT already on the server), a linked worktree's is
// already in the shared repo (NO push needed), and an unmeasurable ground claims
// NEITHER — a run told "no push needed" without a measurement is the same defect
// wearing different words.
func taskGroundShapeSentence(dir, branch string) string {
	switch model := devworkspace.GroundModelOf(dir); model {
	case devworkspace.GroundModelClone:
		return fmt.Sprintf(taskGroundShapeClone, branch)
	case devworkspace.GroundModelLinkedWorktree:
		return fmt.Sprintf(taskGroundShapeLinkedWorktree, branch)
	default:
		// GroundModelMissing / GroundModelBrokenLink: the shape could not be
		// established, so neither delivery claim may be made. The reason is the
		// constant's own spelling, never re-typed here.
		return fmt.Sprintf(taskGroundShapeUnknown, string(model), branch)
	}
}

// GroundShapeSentence is the exported reader of the sentence above — the claim
// alone, without the announcement around it, so a caller (and the regression
// test) asserts WHICH claim a measured shape makes instead of re-implementing
// the decision beside it (Rule 31: exercise the surface, do not shadow it).
func GroundShapeSentence(dir, branch string) string {
	return taskGroundShapeSentence(dir, groundBranchOrDash(branch))
}

// ── the EVIDENCE DESTINATION clause: the fence needs a destination ───────────
//
// THE MEASURED CONSEQUENCE OF A FENCE WITH NOWHERE TO PUT THINGS (2026-09-26,
// row 01a0dbe4-ac93-74f6-8afe-e740669c9532, HP main checkout): the run's row
// carried `deliverable.repo` = the shared checkout, its briefing rendered
// "Repo: /home/nick/work/GoPi", and the announcement above told it that its
// worktree was the ONLY tree it writes. Rule 22 forbids the `/` partition for
// temp data, `.gitignore` entries and reapers were ruled out, and no evidence
// directory existed anywhere in the run's text — so the run INVENTED
// `<repo>/scratch/` and left ~161 untracked files in the operator's checkout.
// The fence was obeyed in spirit (the run did not touch tracked content) and
// had nowhere else to go; a rule with no legal alternative is a rule that gets
// routed around.
//
// THE DESTINATION IS ALREADY RESOLVED, so this clause invents nothing: it is the
// run's OWN session-artifacts directory — the directory `taskreport.PathEnv`
// (`GOPI_TASK_REPORT_PATH`) already points into, because the report lives there
// and `SealTaskReport` creates it. That makes it legal by construction rather
// than by assertion: it is off every git work tree (so no `git status`, no
// `git add` and no merge path sees it), the harness already holds the handle to
// it, and it is per-TASK (nothing a sibling run writes can collide, and nothing
// in it can be confused with a deliverable).

// TaskGroundEvidenceClause renders the destination half of the announcement for
// one row: the run's allowed evidence directory, derived from the SAME resolver
// the report contract uses (TaskReportPath → taskSessionDir), so the briefing and
// the exported report path can never name different directories.
//
// It takes the ROW id (the session directory is keyed by the runner's normative
// task id — the ground path is a directory, not that key) and returns "" when
// the direction cannot be resolved — a clause naming no directory is worse than
// no clause (Rule 15), and the announcement above still stands without it.
func TaskGroundEvidenceClause(taskID, ground string) string {
	row := taskIDNormalize(taskID)
	if row == "" {
		return ""
	}
	reportPath, err := TaskReportPath(row)
	if err != nil {
		taskNotifyLog("ground announcement: evidence destination for %s unresolved (the announcement renders without it): %v", row, err)
		return ""
	}
	return fmt.Sprintf(taskGroundEvidenceFmt, filepath.Dir(reportPath), taskreport.PathEnv, ground)
}

// taskGroundEvidenceFmt is the evidence clause's ONLY spelling (Rules 1/13/14:
// the producer and the regression test read the same text). The three
// substitutions are the evidence directory (resolved, never spelled), the env
// var the run ALREADY has for it (the second handle on the same directory —
// `taskreport.PathEnv` is exported into the child's environment beside the
// report it names), and the run's own tree (the one place a destination looks
// legal and is not).
//
// The two consequences that make the choice non-arbitrary are stated because
// they are what a run needs in order to believe it: every checkout stays clean,
// and nothing there is a deliverable.
const taskGroundEvidenceFmt = "\n[Where your evidence goes — the legal destination]\n" +
	"Anything a run must write that is NOT part of the branch — pixel captures, screenshots, dumps, logs, scrape output, throwaway measurements — goes in YOUR run's evidence directory:\n%s\n" +
	"It is outside every git work tree (so no `git status`, no `git add` and no merge path ever sees it), it already exists, and it is yours alone: nothing in it can collide with a sibling run, and nothing in it is a deliverable. Your environment names the same directory: %s lives there, and its directory IS this one.\n" +
	"Do NOT invent a destination inside a checkout — in particular NOT `%s/scratch/`, not a temp directory under it, and not anything else a git repository would report as yours. A run that invents one leaves untracked residue in a tree it does not own, and it is recorded as such."

// TaskGroundEvidenceLabel is the clause's leading marker, spelled once so a
// reader (and a test, in the tools package and in cmd/agent alike) can find it in
// a briefing without re-typing the body — the TaskGroundAnnouncementLabel
// precedent, which is also why it is exported: the announcement text is asserted
// from two packages and a mirrored literal is the drift Rules 1b/13 forbid.
const TaskGroundEvidenceLabel = "[Where your evidence goes — the legal destination]"

const (
	// taskGroundAnnouncementFmt is the announcement's ONLY spelling for a LINKED
	// WORKTREE ground (Rules 1/13/14: one definition, referenced by the producer
	// and the regression test alike, so a test that asserts the clause cannot
	// drift from the text the child actually receives).
	//
	// THE SHAPE SENTENCE IS MEASURED, NOT DECLARED (2026-10-27). This template
	// used to be rendered for EVERY ground, and it told the reader "it is a
	// linked worktree of your task's repo" — which is FALSE for a clone-channel
	// ground, and was measured false on this very epic's own run: a clone
	// (`.git` a DIRECTORY, `git rev-parse --git-common-dir` printing `.git`)
	// received that sentence while its neighbouring clause said "This ground is
	// a CLONE standing on branch master". Two sentences, one briefing, two
	// answers. A sentence whose truth depends on the ground's SHAPE is rendered
	// FROM the measured shape (devworkspace.GroundModelOf — the ONE
	// discriminator), never from the channel that happens to be asking.
	taskGroundAnnouncementFmt = TaskGroundAnnouncementLabel + "\n" +
		"THIS RUN ALREADY HAS ITS WORKTREE. You are standing in it: %s\n" +
		"%s" +
		"Work here. Do NOT create a second worktree, do NOT work in the repo's main checkout, and do NOT rename the branch.\n" +
		"The branch name IS the ledger address — the task id itself, so task ↔ branch ↔ worktree stay cross-comparable from the names alone (`gopi task show --branch <branch>` reads this row off it). A descriptive name belongs in your report, never in the ref.\n" +
		"The runner created this ground BEFORE your first round and recorded it as this row's deliverable, so a second worktree is litter: nothing names it, no reclaim collects it."
	// TaskGroundAnnouncementLabel is the announcement's leading marker, spelled
	// once so the regression test can find the clause in a briefing without
	// re-typing the body (the taskGroundLineLabel precedent in cmd/agent).
	//
	// SHAPE-NEUTRAL BY DESIGN: it says "ground", not "worktree", because the
	// label is the first thing the model reads and a clone-channel ground is not
	// a worktree. The shape sentence that follows it is the measured one.
	TaskGroundAnnouncementLabel = "[Your ground — ALREADY CREATED for you]"
	// taskGroundShapeClone is the shape sentence for a measured CLONE ground:
	// it says what the directory IS, and — the half that matters at close-out —
	// who publishes the commit (this bullet is Rule 6c's clone half, in the
	// briefing, so a run is not left to infer it).
	taskGroundShapeClone = "it is a CLONE — a standalone repository of its own (.git is a DIRECTORY, `git rev-parse --git-common-dir` prints `.git`), checked out on branch %s. " +
		"It is registered in no worktree list and NOBODY ELSE'S repo holds its commits: the clone channel's own DELIVERY LOOP publishes them (clean → pull --rebase → declared build → push of that branch), once per run, from the run's END hook. Your commit is NOT already on the server — do not treat it as delivered, and do not hand-roll the push either (the bash tool refuses a run-chosen destination; internal/tools/git_publish_target_guard.go).\n"
	// taskGroundShapeLinkedWorktree is the shape sentence for a MEASURED linked
	// worktree ground: the commit really is already in the shared repository, so
	// the "no push needed" claim is TRUE here — and only here, which is why it is
	// a separate template instead of a conditional clause inside one sentence.
	taskGroundShapeLinkedWorktree = "it is a LINKED WORKTREE of your task's repo (.git is a FILE naming a gitdir under the main checkout), checked out on branch %s. " +
		"A linked worktree shares the main repo's object store and refs, so committing here already updates your branch in the shared repo: your commit IS already on the branch, and NO push is needed for it.\n"
	// taskGroundShapeUnknown is the shape sentence for a ground whose kind could
	// NOT be measured (the entry is missing, or unreadable). It claims neither
	// answer: a run told "no push needed" without a measurement is the defect
	// this clause exists to remove, and so is a run told the channel owns a
	// commit nobody has established is unshared.
	taskGroundShapeUnknown = "its ground SHAPE COULD NOT BE MEASURED (%s), checked out on branch %s. " +
		"Whether your commit is already shared with the task's repo therefore has NO answer here: measure it before you report delivery (`test -d .git` — a DIRECTORY is a clone, whose commit the channel must publish; a FILE is a linked worktree, whose commit is already in the shared repo). Do not assume either answer.\n"
)

// TaskGroundAnnounced reads the ground THIS process was handed by the runner
// (TaskGroundDirEnv / TaskGroundBranchEnv) — the executor child's reader of the
// pair its spawn seam exported.
//
// It returns ("", "") when the pair is ABSENT, which is the truthful answer for
// every process the seam did not spawn: an operator's own shell, a nested gopi
// the run itself started, a repo-less row. It deliberately does NOT fall back to
// a path derived from the process cwd: an announced ground is a fact the RUNNER
// stated, and manufacturing one here would let a plain session believe it owns a
// worktree nobody made for it (the same confusion, from the other side).
//
// Rule 21: this is the reader the env-name constants above are declared for —
// the names are exported because the spawn seam writes them and the executor
// reads them, each through this one pair of constants.
func TaskGroundAnnounced() (dir, branch string) {
	return strings.TrimSpace(os.Getenv(TaskGroundDirEnv)),
		strings.TrimSpace(os.Getenv(TaskGroundBranchEnv))
}

// TaskGroundBranchPublish is the ONE cross-check between the two spellings of a
// row's ground branch, and it is what BOTH announcement writers call before they
// publish TaskGroundBranchEnv.
//
// WHY IT EXISTS (the defect this closes). `task/<dashless stem>` had two
// producers that nobody compared:
//
//	DERIVED     TaskGroundBranchName(taskID) — what the claim records as
//	            `deliverable.branch`, and what the clone channel asks
//	            devworkspace to check out;
//	ANNOUNCED   the value published as GOPI_RUN_GROUND_BRANCH and read back by
//	            TaskGroundAnnounced — what the publish-target guard, the
//	            delivery-proof arm and the briefing use to decide "which branch
//	            is this run's?".
//
// A reader that picks the wrong one does not fail: the guard's
// `gitPublishGroundBranch` falls back to a measured HEAD, a mismatch there is a
// SKIP, and a skip reads exactly like a pass. So the disagreement has to be
// named by the producer, at the moment of publication.
//
// THE INPUTS, and what they mean:
//
//	taskID      the row this ground belongs to ("" ⇒ no ledger address at all)
//	standingOn  the branch the ground ACTUALLY stands on, as measured in the
//	            ground (one of the callers' `rev-parse --abbrev-ref HEAD`, or
//	            the value the channel just checked out). "" means the ground
//	            could not name its branch — a measurement that did not happen,
//	            which is reported as such rather than guessed.
//
// THE OUTPUT is the branch to PUBLISH — always a real value, never "" for a
// row that has an address (an empty announcement is the very silent skip this
// function removes):
//
//	standingOn == the derived name   → derived (the ordinary case)
//	standingOn == ""                 → derived, WITH a diagnostic (Rule 15)
//	standingOn is "task/" + a 32-hex stem that names NO ledger row — the
//	    taken-name FALLBACK taskGroundBranchUnowned mints when another row
//	    already holds the name
//	                                 → standingOn: the ONE legitimate divergence,
//	                                   named in the diagnostic and NOT an alarm
//	anything else (an integration branch, a descriptive name, another row's
//	    task/<id>, a dash-carrying stem) → derived, WITH a loud diagnostic
//
// The second return value is that diagnostic, "" when the two agree. Callers
// publish the first and log the second; neither is allowed to drop it.
// TaskGroundDetachedHeadBranch is what `git rev-parse --abbrev-ref HEAD` answers
// for a repository whose HEAD names no branch. It is EXPORTED (Rule 14) because
// two packages read it — this one and the clone channel that measures a ground's
// HEAD before calling TaskGroundBranchPublish — and an unexported constant there
// would be a bare literal in disguise.
const TaskGroundDetachedHeadBranch = "HEAD"

func TaskGroundBranchPublish(taskID, standingOn string) (branch, divergence string) {
	derived := TaskGroundBranchName(taskID)
	standing := strings.TrimSpace(standingOn)
	row := strings.TrimSpace(taskID)
	if derived == "" {
		// No ledger address to derive from. Publishing the bare row id (or an
		// empty string) would announce a ref no row owns; the measurement, if
		// there was one, is still the only honest answer.
		return standing, fmt.Sprintf(taskGroundPublishNoAddressFmt, row, groundBranchOrDash(standing))
	}
	if standing == derived {
		return derived, ""
	}
	// THE INTEGRATION BRANCH IS NOW THE AGREEMENT, NOT THE DIVERGENCE (the
	// operator's ruling, 2026-11-05). A clone ground is a workspace of this
	// run's own, so it stands on `master` from the moment it is cloned and
	// delivers by pushing that same branch; the row is addressed by its
	// WORKSPACE PATH (`deliverable.worktree`), not by a ref, so there is no
	// address for a ground branch to resolve. Publishing the derivation here
	// would announce a ref the ground never stood on — the exact lie this
	// cross-check exists to prevent — and the publish-target guard would then
	// refuse the run's own `git push origin master`.
	//
	// The legacy test arm that read this as a divergence encoded the pre-fix
	// shape (`task/<id>` minted per ground); it was updated with this ruling,
	// not deleted.
	if standing == masterBranchName {
		return standing, ""
	}
	if standing == "" {
		return derived, fmt.Sprintf(taskGroundPublishUnmeasuredFmt, row, derived)
	}
	fallback, ferr := taskGroundBranchIsFallback(standing, derived)
	if ferr != nil {
		return derived, fmt.Sprintf(taskGroundPublishLedgerErrFmt, row, standing, derived, ferr)
	}
	if fallback {
		return standing, fmt.Sprintf(taskGroundPublishFallbackFmt, row, standing, derived)
	}
	return derived, fmt.Sprintf(taskGroundPublishDivergedFmt, row, standing, derived)
}

// taskGroundBranchIsFallback reports whether branch is the COLLISION FALLBACK
// for derived — the ONE legitimate divergence — rather than another row's
// address or a descriptive name.
//
// THE SHAPE IS NOT THE DISCRIMINATOR, and saying so is the point. A sanitised
// task id and a freshly-minted uuid stem are BOTH 32 hex characters, so a shape
// test would accept `task/<some other row>` as a fallback: the exact red-arm
// case this epic names. What separates them is the LEDGER — a fallback stem
// names NO row at all (it is minted as a name no existing ref can hold), so it
// is re-dashed into canonical uuid form and asked.
//
// An UNREADABLE ledger is not an acceptance either: the caller reports the
// divergence with the error in the sentence (Rule 15), because "I could not
// tell" must never read as "this is the fallback".
func taskGroundBranchIsFallback(branch, derived string) (bool, error) {
	if !strings.HasPrefix(branch, TaskGroundBranchPrefix) || !strings.HasPrefix(derived, TaskGroundBranchPrefix) {
		return false, nil
	}
	// The hex predicate is the PACKAGE's existing one (worktree_sweep_chat.go),
	// not a second spelling of "is this a uuid stem" (Rule 13).
	stem := strings.TrimPrefix(branch, TaskGroundBranchPrefix)
	if len(stem) != taskGroundFallbackStemLen {
		return false, nil
	}
	for _, r := range stem {
		if !isHexRune(r) {
			return false, nil
		}
	}
	exists, err := taskRowExists(taskGroundRedashStem(stem))
	if err != nil {
		return false, err
	}
	// A row names this ref ⇒ it is that row's ADDRESS, not a fallback.
	return !exists, nil
}

// taskGroundRedashStem puts the canonical 8-4-4-4-12 dashes back into a stem
// taskGroundIDStem stripped them from, so the value can be asked of the ledger
// as a task id. A stem of any other length is returned unchanged (the caller has
// already refused it).
func taskGroundRedashStem(stem string) string {
	if len(stem) != taskGroundFallbackStemLen {
		return stem
	}
	var b strings.Builder
	for i, r := range stem {
		switch i {
		case taskGroundRedashGroup1, taskGroundRedashGroup2, taskGroundRedashGroup3, taskGroundRedashGroup4:
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// The publish-time diagnostics (Rule 1b: one spelling, read by the producer and
// by whoever greps the log for why a ground branch is not the row's name).
const (
	taskGroundPublishNoAddressFmt  = "task ground: row %q has no derivable ledger address — publishing the measured branch %s unchanged rather than an empty announcement"
	taskGroundPublishUnmeasuredFmt = "task ground: row %s published its DERIVED branch %s because the ground named no branch of its own — the announcement is the derivation, not a measurement of what this run stands on"
	taskGroundPublishFallbackFmt   = "task ground: row %s stands on %s, not its derived %s — the taken-name FALLBACK (taskGroundBranchUnowned mints a fresh uuid when another row holds the name); publishing the measured branch"
	taskGroundPublishDivergedFmt   = "task ground: row %s DIVERGED — the ground stands on %q but this row's ledger address is %q; publishing the ADDRESS (a reader given the standing branch would resolve no row). This is not the taken-name fallback"
	taskGroundPublishLedgerErrFmt  = "task ground: row %s stands on %s, not its derived %s, and the LEDGER could not be asked whether that stem names a row (%v) — publishing the ADDRESS"
	taskGroundFallbackStemLen      = 32
)

// The dash positions of a canonical uuid, as OFFSETS INTO THE DASHLESS STEM at
// which a separator belongs (8-4-4-4-12). Named because taskGroundRedashStem
// walks them (Rule 1) and the value is a property of the id format, not of any
// one call site.
const (
	taskGroundRedashGroup1 = 8
	taskGroundRedashGroup2 = 12
	taskGroundRedashGroup3 = 16
	taskGroundRedashGroup4 = 20
)

// taskGroundCreate materialises the ground: a LINKED worktree of repo at dir,
// checked out on branch. The branch is created when it does not exist yet and
// otherwise attached to the existing ref (a second attempt of the same row
// finds its branch already there), so the operation is idempotent by shape
// rather than by special-casing the second call.
//
// Cut from the repo's currently checked-out HEAD: the worktree follows the
// repo, and a repo on its integration branch (the ordinary shape) therefore
// gives the task a ground branched from the integration branch. `-C repo` keeps
// the command anchored on the repo regardless of the caller's cwd.
//
// This is the LOCAL-BASELINE spelling of the operation (taskGroundCreateFrom
// with an empty base), kept because it is the documented shape of the
// non-peer path and of the fresh-path fixture.
func taskGroundCreate(repo, dir, branch string) error {
	return taskGroundCreateFrom(repo, dir, branch, "")
}

// taskGroundCreateFrom is the ONE `git worktree add -b <branch>` of the ground
// (epic worktree-base-align, 2026-09-26): identical to taskGroundCreate in
// every respect except the COMMIT the new branch starts at —
//
//	base == ""    → HEAD, the repo's currently checked-out commit (the
//	                pre-epic behaviour, and what a host with no declared
//	                peer or an unreachable one falls back to),
//	base == <sha> → that commit (the peer's integration-branch tip as
//	                SyncPeerFetch just read it).
//
// The path, the branch name, the fresh-path guard and the idempotency probe
// are untouched: only the base commit moves (Rule 6a — a "descriptive" rename
// of anything here would break the task ↔ branch ↔ worktree addressability).
//
// WHY THE BASE MATTERS (the measured defect this closes). A ground cut from a
// stale local integration branch starts the run N commits behind the peer, and
// every one of those N commits is a potential conflict the run pays for twice:
// once resolving it, and once again in the merge machinery downstream. Cutting
// from the peer's own tip removes the lag at its source, BEFORE the run has
// written anything — the cheapest place there is to fix it.
//
// It writes nothing to the main checkout: the base is a COMMIT-ish, resolved
// from a remote-tracking/mirror ref this repo already holds (Rule 29).
func taskGroundCreateFrom(repo, dir, branch, base string) error {
	// FRESH PATH FIRST (worktree_fresh_path.go): git reuses an existing EMPTY
	// directory silently, and this ground's path is DERIVED from the row — so a
	// leftover empty directory at exactly this path would silently become this
	// row's ground, shared with whatever put it there. The caller has already
	// established that `dir` holds no USABLE ground (TaskRunGroundFor step 2
	// probes it and refuses a foreign checkout); this is the strictly stronger
	// claim that nothing at all occupies the path before we create it.
	if err := worktreeFreshPath(dir); err != nil {
		return err
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = taskGroundHeadArg
	}
	args := []string{finishCleanupWorktreeVerb, taskGroundWorktreeAddVerb, dir}
	if !taskGroundBranchExists(repo, branch) {
		args = append(args, "-b", branch, base)
	} else {
		args = append(args, branch)
	}
	out, err := taskGroundGit(repo, args...)
	if err != nil {
		return fmt.Errorf("git worktree add %s -b %s in %s: %w: %s", dir, branch, repo, err, strings.TrimSpace(out))
	}
	return nil
}

// taskGroundBranchExists reports whether branch already exists in repo — the
// idempotency probe of taskGroundCreate. A probe that cannot answer (no such
// branch) is false, which is the creating direction: `git worktree add -b`
// fails loudly on a real branch collision instead of attaching to a ref the
// caller did not expect.
func taskGroundBranchExists(repo, branch string) bool {
	out, err := taskGroundGit(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil && strings.TrimSpace(out) != ""
}

// taskGroundBranchAt reports the branch a directory registered as a worktree of
// repo currently carries ("" when dir is not a registered worktree of repo, or
// is detached). It reads the SAME porcelain listing the rest of this package
// parses (taskWorktreeOwningDir) rather than trusting the directory's .git
// file: a directory that merely LOOKS like a worktree is not one.
func taskGroundBranchAt(repo, dir string) (string, error) {
	out, err := taskGroundGit(repo, finishCleanupWorktreeVerb, taskGroundWorktreeListVerb, taskGroundWorktreeListFlag)
	if err != nil {
		return "", fmt.Errorf("git worktree list in %s: %w: %s", repo, err, strings.TrimSpace(out))
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	_, entries := taskParseWorktreeListPorcelain(out)
	for _, e := range entries {
		got, aerr := filepath.Abs(e.path)
		if aerr != nil {
			continue
		}
		if got == want {
			if e.branch == "" || e.branch == e.path {
				return "", nil // detached: names no branch
			}
			return e.branch, nil
		}
	}
	return "", nil
}

// taskGroundMainCheckout returns repo's MAIN worktree — the shared checkout the
// executor child must never call its ground (item 4). The porcelain listing
// marks it explicitly (`bare`/`detached` aside, the first record is the main
// one and `bare` marks a bare repo); this reads the marker rather than
// assuming the order, and falls back to repo itself, which IS the main checkout
// for every non-bare repository.
//
// "" means "cannot be told" — the caller must then REFUSE (it cannot prove the
// child is not standing in the shared checkout), never guess.
func taskGroundMainCheckout(repo string) string {
	out, err := taskGroundGit(repo, finishCleanupWorktreeVerb, taskGroundWorktreeListVerb, taskGroundWorktreeListFlag)
	if err != nil {
		return ""
	}
	main, _ := taskParseWorktreeListPorcelain(out)
	if strings.TrimSpace(main) == "" {
		return ""
	}
	return main
}

// taskGroundGitTimeout caps one ground git subprocess. Named (Rule 1): a worktree
// add writes a checkout, so it is slower than a probe but must never hang a
// spawn — a wedged repository is a refusal, not a stall.
const taskGroundGitTimeout = 120 * time.Second

// taskGroundGit runs one git command anchored on repo and returns its combined
// output. It is the ONE place this file shells out (Rule 14), so the anchoring,
// the environment hygiene and the bounded wait cannot drift between the probe
// and the creation.
//
// The exec itself is gitPeerRun (git_peer_fetch.go): one `git -C <dir>` runner
// for the whole peer channel, so this path and the fetch path cannot diverge on
// the argv prefix, the stderr convention or the error text. What stays HERE is
// this call site's own contract — its named timeout, and the stripped git
// environment (a ground probe must not inherit GIT_DIR=… and probe someone
// else's repository).
func taskGroundGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskGroundGitTimeout)
	defer cancel()
	return gitPeerRun(ctx, repo, withoutGitDirEnv(os.Environ()), args...)
}

// TaskRunGroundForClone resolves the ground of a row that will be executed in a
// CLONE WORKSPACE (the clone channel, internal/devworkspace) instead of a
// linked `git worktree`.
//
// WHY IT IS A SEPARATE ENTRY POINT AND NOT A BRANCH INSIDE TaskRunGroundFor:
// that function's whole contract is about LINKED worktrees — it derives a
// `gopi-<stem>` path under a worktree root, refuses a main checkout, and
// re-bases on a peer's integration branch. A clone workspace is none of those
// things: the directory is chosen by the channel (workspace_root/<task-id>),
// it is a repository of its OWN (its own .git), and there is no main-checkout
// relation to assert. Folding the two into one function would mean a reader
// could no longer tell which rules apply to which channel, and the worktree
// channel's behaviour would stop being the thing that is unchanged.
//
// WHAT IS SHARED, AND WHY THAT IS THE POINT: the STAMP. dir is recorded as the
// row's deliverable.worktree through stampTaskGround — the same single blob
// writer every other ground stamp uses — so a cloned row names its ground
// exactly like a worktree row does and the ledger never carries a row with no
// ground (Rules 13/14).
//
// MainCheckout is deliberately "" (there is no main checkout this row must be
// kept out of: the workspace IS the row's own clone, and the assertion built on
// that field is about the shared checkout of the row's repo, which a clone
// never is).
func TaskRunGroundForClone(taskID, dir string) (TaskGroundResolution, error) {
	taskID = taskIDNormalize(taskID)
	dir = strings.TrimSpace(dir)
	if taskID == "" {
		return TaskGroundResolution{}, fmt.Errorf("task ground: task_id must not be empty")
	}
	if dir == "" {
		return TaskGroundResolution{}, fmt.Errorf("task ground: the clone workspace path for task %s is empty", taskID)
	}
	repo, _, _ := taskRowDeliverable(taskID)
	repo = strings.TrimSpace(repo)
	// THE BRANCH IS READ FROM THE GROUND, and the ORDER of this read against the
	// repo check is the defect this function used to carry (measured 2026-10-27).
	//
	// It used to return EARLY when the row named no repo, and the ONLY writer of
	// a run-ground anchor — TaskGroundAnchorRecord — sits below that point. A row
	// with no deliverable.repo (the `source=auto` auto-registration shape: a
	// bare-prompt run whose row IS the run) therefore opened a full clone at
	// <workspace_root>/<task-id> and left it with NO ANCHOR — and the anchor is
	// the sole input to the sweep's run-ground population
	// (RunGroundCollectableGrounds ← RunGroundAnchors). The directory was not
	// "kept"; it was INVISIBLE: no reclaim arm could name it, so it stayed on
	// disk with nothing anywhere recording that it existed (measured:
	// /home/nick/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c — 233 MB, clean,
	// on master, its row `completed` — absent from
	// `gopi task sweep-worktrees --dry-run` in its entirety).
	//
	// The branch is the OTHER half of the same anchor and it never depended on
	// the row: the ground is a repository of its own, and `git -C <dir>` answers
	// for the branch standing in it. That is why reading it FIRST costs nothing
	// and why an anchor can be written for a row that names nothing.
	branch, branchErr := repoSweepGit(dir, "rev-parse", "--abbrev-ref", taskGroundHeadArg)
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == taskGroundHeadArg {
		branch = masterBranchName
	}
	if branchErr != nil && repo != "" {
		// A repo-aware row's ground that cannot name its branch is a broken
		// ground, and the caller must hear it (unchanged behaviour).
		return TaskGroundResolution{}, fmt.Errorf(taskGroundUnresolvableFmt, taskID, repo,
			fmt.Sprintf("the clone workspace %s does not answer for its current branch: %v", dir, branchErr))
	}
	if branchErr != nil {
		// A REPO-LESS row gets no such refusal, and that is not a swallowed
		// error (Rule 15): the run itself is fine — it stands in a directory it
		// owns — and what cannot be measured is one FIELD of the anchor. The
		// anchor is still written, carrying no branch, which the reader treats as
		// "there is no lineage to judge" (reclaimGroundChecklistVerdict rung ②)
		// and which keeps the undelivered-commits rung unmeasurable — i.e. the
		// ground is KEPT, the conservative direction. The fact is stated in the
		// Detail the channel logs, so an operator can see it.
		branch = ""
	}
	// THE ANCHOR, FIRST — before any repo check, because the anchor is what makes
	// the ground exist as far as the sweeper is concerned, and a clone workspace
	// has one whether or not the row names a repo. This is the SAME recorder the
	// worktree path uses (Rule 14: one writer, one shape).
	TaskGroundAnchorRecord(taskID, dir, branch)
	if repo == "" {
		// THE STAMP IS STILL A NO-OP, and deliberately so: the deliverable blob
		// pairs a worktree WITH a repo (`taskBackfillDeliverableWorktreeAt`
		// returns early on an empty repo — `{"worktree":...}` alone is not a
		// shape the ledger has). Writing one here would be a value no reader can
		// pair, i.e. false evidence (Rule 15). The ground's record is the
		// anchor above; the row keeps naming no ground, exactly as it declares.
		return TaskGroundResolution{
			Dir: dir, Branch: branch,
			Detail: fmt.Sprintf("the row names no repo, so no ground is stamped — the clone workspace %s is ANCHORED instead (branch %q), which is what puts it in the run-ground population", dir, branch),
		}, nil
	}
	// ONE GROUND, ONE OWNING ROW — the clone channel's arm of the rule the
	// worktree creation seam and the claim seam already apply (see
	// taskCreationGroundBorrowed). Measured absent 2026-11-01 (rows
	// 01a19648-4fbe → 01a196bf-8cbb): this writer recorded ANOTHER row's live
	// workspace and branch onto the run that merely stood in it, and the run
	// then failed its delivery leg on a dirty tree it had never touched.
	//
	// A refusal is REPORTED and returns the ground it resolved WITHOUT stamping
	// it: the run keeps working where it is (the directory is real and the run
	// is already in it), and the ROW keeps whatever ground it had. Guessing the
	// other way round is what put two owners on one directory.
	if why := taskCreationGroundBorrowed(taskID, repo, dir, branch); why != "" {
		taskNotifyLog(taskWorktreeBorrowedGroundLineFmt, taskID, why)
		return TaskGroundResolution{
			Dir: dir, Branch: branch, Repo: repo,
			Detail: fmt.Sprintf("the clone workspace %s is NOT recorded as this row's ground — %s", dir, why),
		}, nil
	}
	// THE WRITER IS THE ONE THE WORKTREE CLAIMER ALSO USES, but the WORKTREE
	// ARGUMENT IS THIS DIRECTORY, not a name derived from the row: the claimer
	// derives `gopi-<stem>` from the repo, and on a clone that names a directory
	// the run never creates (measured live 2026-10-18: the row ended up pointing
	// at /home/nick/workspace/gopi-<stem>, which does not exist, while the real
	// workspace was reclaimed). Passing dir explicitly records the truth.
	if _, err := taskBackfillDeliverableWorktreeAt(taskID, dir, branch, &repo, taskWorktreeStampMomentClaim); err != nil {
		return TaskGroundResolution{}, fmt.Errorf("task ground: recording the clone workspace %s on row %s: %w", dir, taskID, err)
	}
	return TaskGroundResolution{
		Dir: dir, Branch: branch, Repo: repo,
		Detail: fmt.Sprintf("REUSED the clone workspace %s on branch %s", dir, branch),
	}, nil
}
