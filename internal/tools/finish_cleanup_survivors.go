// Code moved OUT of the retired finish-cleanup mechanism (Rule 43).
//
// Every symbol below was DEFINED in internal/tools/task_finish_cleanup*.go and
// is still read by live code OUTSIDE that mechanism — the git and /proc
// primitives other packages drive, the main-checkout restore, the deliverable
// keys the ledger readers share, and the report vocabulary the close-out trail
// uses. They moved here in the SAME commit that deleted the mechanism, so the
// tree was never red in between (Rule 43: move first, delete in the same
// commit). Each one is named by `go build` once the mechanism is gone; none of
// them was moved speculatively.

package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/daemoncwd"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/pushfence"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// finishCleanupEnv is the kill switch (default ON, autoTaskRegisterEnv
	// convention): "0"/"off"/"no"/"false"/"disabled" turns the chain off.
	finishCleanupEnv = "GOPI_FINISH_CLEANUP"

	// holderOrphanMarker is the phrase that marks a holder reparented to init.
	// It is a named constant because a test asserts on it and an operator reads
	// it: one spelling, matched and printed from the same source (Rule 13).
	holderOrphanMarker = "ORPHAN"

	// NOTE (2026-10-25): this file used to declare
	// `finishCleanupPeerRepo = "~/work/GoPi"` — the THIRD copy of the peer's
	// checkout path (Rules 1/14) and the one the close-out push dialled. The
	// path half now comes from the ONE resolver (peer_repo_path.go), which
	// parses the host's DECLARED delivery point and announces its named
	// fallback; the alias half is still resolved against the delivering
	// checkout's own remotes (peerRemoteTargetFor).

	// finishCleanupTargetDotDir is the last-resort repo the delivery-target
	// resolution asks about when this process's own repo root cannot be read:
	// git resolves "." as the current checkout, which is the same answer the
	// unreadable resolution was trying to give (Rule 1: the spelling is named
	// once, never typed at a call site).
	finishCleanupTargetDotDir = "."

	// finishCleanupGitTimeout caps every git invocation in the chain; a
	// push to a busy peer fails fast, never hangs the close-out.
	finishCleanupGitTimeout = 60 * time.Second

	// finishCleanupDBTimeout caps the ledger read (terminal-state check).
	finishCleanupDBTimeout = 10 * time.Second

	// finishCleanupChainTimeout caps the chain's ACTION steps — the git
	// operations and, since the delivery-point sync, the merge + declared
	// build + vet that stand between the run and its release.
	//
	// It is a SEPARATE budget from finishCleanupDBTimeout on purpose: one
	// bounds a database round-trip, the other drives git and a compiler over
	// the whole repository, and sharing the ledger's 10s made the release fail
	// on any real repo — measured 2026-09-17 on a clone of ~/work/GoPi: the
	// sync's merge succeeded, its build succeeded, and `git worktree remove`
	// then returned "context deadline exceeded" (the ledger context had
	// expired while the compiler ran), refusing the close-out of a run whose
	// delivery was already verified. A budget that expires before the work it
	// bounds is a failure mode, not a timeout.
	finishCleanupChainTimeout = 30 * time.Minute

	// finishCleanupExemptBranches are the never-clean branches (master is
	// also skipped; it never carries a session deliverable anyway).
	finishCleanupExemptBranches = "hp-unique-maxtokens-work"
)

// exemption env override (single place, Rule 2): comma list added to the
// built-in exempt set. Empty = built-ins only.
const finishCleanupExemptEnv = "GOPI_FINISH_CLEANUP_EXEMPT"

const (
	// procRootName is the procfs mount point: the live-CWD scan's entry
	// (Linux; a host without it degrades to "cannot scan" = never blocks).
	// It is an ALIAS of internal/procname's value (Rules 13/14: the name has
	// ONE definition — in the leaf package every process-fact consumer can
	// import — and this side references it instead of re-typing the literal).
	procRootName = procname.ProcRootDefault

	// procCwdName is the per-process CWD symlink under procRootName —
	// Readlink(<procRootName>/<pid>/<procCwdName>) yields the dir the
	// process is working in. Alias of procname.CwdName.
	procCwdName = procname.CwdName

	// procCommName is the per-process command-name file (best-effort: only
	// decorates the skip reason with the holder's name). Alias of
	// procname.CommName.
	procCommName = procname.CommName

	// procCommUnknown decorates the skip reason when the process name file
	// is unreadable (still a live holder — never a reason to skip the guard).
	procCommUnknown = "?"
	// finishCleanupDaemonHolderMarker names the ONE exemption to the "a live
	// holder is a live BOND" reading: a DETACHED GOPI DAEMON standing in the
	// worktree. Guard ③ still refuses (the directory is genuinely held — the
	// refusal direction is unchanged), but the reason NAMES the holder as
	// unrelated to any task: no row owns it, it has no bounded lifetime, so
	// the refusal is not evidence of work in progress and will not clear by
	// itself. Reporting the distinction is the recovery half of epic
	// daemon-cwd-escape; the producer half (every daemon now starts in a
	// MEASURED process root) is what stops new ones being created.
	//
	// This is a NAMED exception, never a veto bypass (PROJECT.md Rule 15b /
	// Rule 25): the guard's predicate is untouched and the directory still
	// survives.
	// finishCleanupTestHolderMarker / finishCleanupTestHolderFmt name the
	// TEST-ARTIFACT holder class in the refusal reason (epic
	// verify-no-verdict-keeps-merge). It names the AGE it waits on, so the
	// refusal is checkable (`ps -o lstart= -p <pid>`) rather than a promise.
	//
	// THE CRITERION (why age, and why here): a holder of this class is a
	// leftover of a Go test run BY CONSTRUCTION — it stands in the scratch TEST
	// tree, which no production process may occupy (SuiteTempRoot/MkdirTemp are
	// the suite's own sandbox) — and its lifetime is UNBOUNDED. MEASURED
	// (2026-09-14): `gopi-merger merger` daemons from an earlier commit's
	// cmd/agent suite were still alive ~20 min after that suite ended, cwd
	// inside a task worktree, GOPI_HOME under the suite scratch root. Waiting
	// them out is therefore not an option, while "any live cwd vetoes" keeps the
	// worktree un-recyclable forever. Age breaks the tie: below the floor the
	// holder is an in-flight test (refuse, and say so), at or past it a leftover
	// (reclaim).
	finishCleanupTestHolderMarker = "holder is a leftover of a TEST run"
	finishCleanupTestHolderFmt    = finishCleanupTestHolderMarker +
		": its cwd is inside the scratch test tree (%s) — no task owns it and it will never exit on its own, so it cannot be waited out; below the %s floor it is treated as a test still running and this worktree is kept (remove that process explicitly to keep it longer)"
	// finishCleanupTestHolderMinAge is the floor described above.
	finishCleanupTestHolderMinAge   = 10 * time.Minute
	finishCleanupDaemonHolderMarker = "unrelated immortal holder"
	// finishCleanupDaemonHolderFmt is the sentence appended to the skip reason
	// when the holder is a detached daemon: it names WHAT the holder is, why
	// waiting cannot clear it, and the one lever that does (the daemon's cwd
	// override for a future start — the guard itself is never bypassed). It is
	// built FROM the marker constant, so the phrase an operator greps and the
	// phrase the report emits can never be two spellings.
	finishCleanupDaemonHolderFmt = finishCleanupDaemonHolderMarker + ": holder is a detached gopi daemon (%s overrides where a future daemon starts; this one must be stopped explicitly — it has no bounded lifetime, so this refusal will not clear on its own)"

	// procNumericNameChars is the pid-directory test: a /proc entry whose
	// name is ENTIRELY these characters is a per-process directory ("self",
	// "sys", "net", … are kernel faces, never process CWD holders).
	procNumericNameChars = "0123456789"

	// finishCleanupGitDirFilePrefix is the pointer a LINKED worktree keeps
	// in its .git FILE ("gitdir: <main>/.git/worktrees/<name>"): the live
	// git process's real CWD while it holds that worktree's index/admin dir.
	finishCleanupGitDirFilePrefix = "gitdir:"

	// finishCleanupInUseMarker is the phrase every "do not remove" refusal
	// carries — one string for the skip reason AND the step Detail, so an
	// operator (or a test) matches the condition without knowing the wording
	// of the holder line around it.
	finishCleanupInUseMarker = "worktree in use"

	// finishCleanupDetachedHead is what `rev-parse --abbrev-ref HEAD` prints
	// for a detached HEAD — no branch is checked out, so no branch axis
	// exists to compare (Rule 1: one spelling for the sentinel).
	finishCleanupDetachedHead = "HEAD"

	// finishCleanupStepWorktree is the worktree step's report name (shared
	// by both removal sites: server recycle and client chain step ③).
	finishCleanupStepWorktree = "worktree"

	// finishCleanupStaleRegistrationStep names the report step that records
	// git's OWN pruning of a registration it holds for a directory that is
	// gone. It is a step of its own rather than a Detail on the worktree step
	// because it is NOT a removal — nothing on disk is entered, touched or
	// deleted, only a registration whose directory is missing — and because a
	// reader must be able to tell "the ground was collected" from "the registry
	// was tidied so the ground COULD be collected" (Rule 15).
	finishCleanupStaleRegistrationStep = "stale-registration"

	// finishCleanupStaleRegistrationNote / …NoneNote are the two readings the
	// step renders. The verb and the flag are NOT re-spelled here: they are the
	// ones already owned beside this group (finishCleanupWorktreePruneVerb /
	// finishCleanupWorktreePruneFlag, referenced by the reclaim's repair leg),
	// so the close-out and the reclaim cannot drift about what `prune -v` is
	// (Rule 14).
	finishCleanupStaleRegistrationNote = "pruned the registration git still held for the missing directory " +
		"(git worktree prune -v) — nothing on disk was touched"

	// …NoneNote is printed rather than "" on purpose: `prune -v` prints one line
	// per registration it drops and NOTHING when it holds none, so an empty
	// reading is the EVIDENCE that the branch delete below — if it refuses — has
	// some other cause than a vanished-directory registry entry.
	finishCleanupStaleRegistrationNoneNote = "no stale registration to prune (git worktree prune -v printed nothing) " +
		"— the branch delete below, if it refuses, is NOT a vanished-directory registry entry"

	// finishCleanupPlanStaleRegistrationDetail is the PLAN-side counterpart, and
	// it is not decoration: the plan face and the apply face are required to
	// describe the SAME chain step name by step name
	// (TestFinishCleanupPlanAndApplyDescribeTheSameChainInTheGoneGroundShape,
	// the regression for task 01a13017). A step that runs on the apply path and
	// appears nowhere in the plan is a chain the dry run did not describe — so
	// the prune is PREDICTED here, carrying finishCleanupPlanMarker exactly as
	// every other predicted step does (finishCleanupPlanMarker is the prefix
	// that makes a reader — and isPlanStep — read a line as a prediction, never
	// as a claim that something ran).
	finishCleanupPlanStaleRegistrationDetail = finishCleanupPlanMarker + "run: git " +
		finishCleanupWorktreeVerb + " " + finishCleanupWorktreePruneVerb + " " + finishCleanupWorktreePruneFlag +
		" — drop the registrations git still holds for MISSING directories " +
		"(nothing on disk is touched, and a live worktree cannot be affected)"

	// merge-note: both sides' const groups are kept — every value below is
	// referenced in the merged tree, so dropping either side breaks a caller.
	// HEAD: the three re-exported refusal phrases the command package's skip
	// filter matches on (FinishCleanupOwnerMismatchMarkerName /
	// FinishCleanupOwnerStaleMarkerName / FinishCleanupInUseMarkerName — used
	// by cli_tasks.go) plus finishCleanupOwnerStaleMarker (the stale-holder
	// refusal). master: finishCleanupPushVerb / finishCleanupBranchVerb (the
	// client chain's steps ②/④ and their report Step names).
	// The three refusal phrases the serve sweep's skip filter matches on are
	// RE-EXPORTED here as aliases (Rule 14: one definition, no mirrored
	// literal — the command package cannot see the unexported constants, so
	// it must not re-type their values). The values live on the unexported
	// names below.
	FinishCleanupOwnerMismatchMarkerName = finishCleanupOwnerMismatchMarker
	FinishCleanupOwnerStaleMarkerName    = finishCleanupOwnerStaleMarker
	FinishCleanupInUseMarkerName         = finishCleanupInUseMarker

	// finishCleanupOwnerStaleMarker is the phrase every STALE-HOLDER refusal
	// carries (the finishCleanupOwnerMismatchMarker convention): the worktree
	// is bound to a NON-LIVE row (terminal/cancelled) whose claim stamp is
	// resolvable on THIS host and whose pid is GONE — the holder no longer
	// exists, the binding is stale, and the path is exactly the stranded
	// case (the cleaner's own row carries the same path). The refusal is
	// deliberately LOUD: cleanup still refuses (a stale holder is a fact an
	// operator should see, not a licence to remove another row's work), but
	// it is never silent — the sweep reports it every pass.
	finishCleanupOwnerStaleMarker = "worktree holder not live"

	// finishCleanupPushVerb / finishCleanupBranchVerb are the two git verbs
	// the client chain's steps ②/④ run — and the report Step names for those
	// steps (the step IS the verb here). Spelled once (Rule 1b): the bare
	// "push"/"branch" strings appeared at every append site before.
	// finishCleanupBranchCheckStep (task_finish_premerge.go) is the same shape
	// for the branch's own test run, which sits between them.
	finishCleanupPushVerb = "push"

	// finishCleanupPushTargetStep is the step name for the DELIVERY TARGET'S
	// OWN RESOLUTION (which arm was taken, and why) — reported beside the push
	// it qualifies, never instead of it.
	finishCleanupPushTargetStep = "push-target"

	// finishCleanupTargetInjectedNoteFmt states that the target actually pushed
	// was SUPPLIED BY THE CALLER (the fixture seam) rather than resolved from
	// the declared topology, and carries the topology's own answer for
	// comparison (Rule 15: a step reports what it measured, and an injected
	// value is named as injected rather than passed off as resolved).
	finishCleanupTargetInjectedNoteFmt = "the target pushed here was INJECTED by the caller, not resolved from the declared topology (which resolves to %q for this repo) — the push below therefore reports the caller's target, and this line states what the topology says"
	finishCleanupBranchVerb            = "branch"

	// finishCleanupInProcessTestMarker / finishCleanupTestFenceBase /
	// finishCleanupTestFenceMarker are ALIASES of internal/pushfence's exported
	// constants — the fence itself now lives there (Rule 14/43), and these names
	// survive so an operator-facing contract and every pinned reading of it keep
	// spelling the same values. Nothing in this package re-implements the
	// predicate or the sentence.
	finishCleanupInProcessTestMarker = pushfence.OptOutEnv
	finishCleanupTestFenceBase       = pushfence.Base
	finishCleanupTestFenceMarker     = pushfence.Marker

	// finishCleanupOwnerMismatchMarker is the phrase every OWNERSHIP refusal
	// carries (the finishCleanupInUseMarker convention): the row's
	// deliverable named a different directory than the one the cleanup arm
	// resolved, so the removal is refused — one string for an operator (or a
	// test) to match without knowing the paths around it.
	finishCleanupOwnerMismatchMarker = "worktree ownership mismatch"

	DeliverableWorktreeKey = "worktree"

	// DeliverableBranchKey / DeliverableRepoKey are the SAME single-source rule
	// for the deliverable's other two keys (Rule 1b: a bare `"branch"` inlined at
	// a SQL filter or a map index is one typo away from mismatching the value
	// every writer stores). The SQL scans that ask "which rows claim a branch?"
	// and the tests that count the keys in a blob BOTH reference these, so the
	// ledger query and the blob contract cannot drift apart.
	DeliverableBranchKey = "branch"
	DeliverableRepoKey   = "repo"

	// merge-note: both sides kept. master's three live-owner/branch refusal
	// markers are referenced by the merged tree's step ④ machinery
	// (finishCleanupBranchLiveOwnerMarker / finishCleanupBranchNotDeliveredMarker
	// in finishCleanupBranchForceDeleteReason + the live-owner refusals,
	// finishCleanupLiveOwnerMarker in the recycle guard); HEAD's
	// DeliverableWorktreeKey is referenced by the ledger SCAN in the command
	// package (SQL filter) and pinned against its json tag by
	// TestDeliverableWorktreeKeyMatchesTheTag. HEAD's own comment above is
	// explicit that the two "worktree" spellings are deliberately SEPARATE
	// constants (JSON field contract vs git argv verb), so master's
	// finishCleanupWorktreeVerb (declared below) does not supersede it.
	// finishCleanupLiveOwnerMarker is the phrase the LIVE-owner refusal
	// carries (the finishCleanupInUseMarker convention): the worktree (or the
	// branch ref about to be deleted) belongs to a row a session still owes
	// work through — one string for an operator (or a test) to match without
	// knowing the row/holder wording around it.
	finishCleanupLiveOwnerMarker = "live ledger row references the worktree"

	// finishCleanupBranchLiveOwnerMarker is the phrase the branch half of the
	// live-owner refusal carries (step ④): the ref the chain is about to
	// delete is one a LIVE row still works through, so the deletion is
	// refused — same marker convention as finishCleanupInUseMarker.
	finishCleanupBranchLiveOwnerMarker = "live ledger row references the branch"

	// finishCleanupDeadLocalHolderMarker is the phrase the DEAD-LOCAL-HOLDER
	// release carries (2026-09-12 orphan epic): a row whose STATUS is still
	// live (pending/claimed/in_progress) named the worktree, but its claim
	// stamp resolves on THIS host to a pid that is provably DEAD — no runner
	// exists any more, so the row holds nothing and the directory is free.
	// The phrase is shared by the reclaim arm (task_reclaim.go) and the
	// ledger-liveness gate so ONE marker names the disposition on every
	// front door (Rule 14); the pair is written together with the directory
	// it frees.
	finishCleanupDeadLocalHolderMarker = "dead local holder"

	// finishCleanupLiveOwnerRowMarker is the phrase the LIVE-OWNER-row
	// release carries: the directory is named by a row whose status is live
	// AND whose holder cannot be proven dead (a live pid, or a foreign host's
	// stamp this host cannot resolve). It is the surviving half of the
	// original veto — the release above exists only for the provably-dead
	// case — and it is reported loudly, because a live-status row whose
	// heartbeat is long dead is exactly the state an operator must see
	// (the other arm may still free the directory, but the row stays).
	finishCleanupLiveOwnerRowMarker = "live ledger row still holds the directory"

	// finishCleanupBranchNotDeliveredMarker is the phrase the "this sweep did
	// not deliver the branch" refusal carries: `-D` force-deletes a ref git
	// itself considers UNMERGED, and it is only safe when THIS run's push
	// landed (the tip is then server-held). Without that delivery the sweep
	// has no standing to force anything (fail-soft: report, keep the ref).
	finishCleanupBranchNotDeliveredMarker = "branch not delivered by this sweep"
	// finishCleanupNoGroundStepDetail is the ③ step record for a chain with NO
	// worktree at all (rep.Worktree == ""): the opless shape a Rule-6a executor
	// row leaves behind. It exists so a reader can tell "the sweep removed the
	// directory" from "there was never a directory in this chain" — the two were
	// the same absent ③ step, which is exactly how the wt-less shape lost ④ too.
	finishCleanupNoGroundStepDetail = "no worktree to remove: this row recorded none and none could be resolved (nothing for this sweep to collect) — the branch step below decides on its own standing"

	// finishCleanupBranchAbsentNote is the ④ step record for a row whose local
	// ref is ALREADY GONE — the ordinary second pass over a terminal row, whose
	// first pass delivered and deleted the ref. It is a step and not an audit
	// record on purpose: no deletion happens here, and the removal audit must
	// not carry a line for one (defect D1, task 01a12b80 — the repeated
	// `-d`/`-D` pair was two records for a mutation git refused twice).
	finishCleanupBranchAbsentNote = "no local branch to delete: the ref is already gone (an earlier pass delivered and deleted it, or no ref was ever created) — nothing for step ④ to do"
	// finishCleanupBranchTipUnreadableNote heads the refusal of a pass that
	// could not READ the ref because its directory is gone. It is NOT the
	// sentence above: "the ref is already gone" is a claim about a BRANCH, and
	// what this pass measured is a DIRECTORY that cannot be entered (Rule 15;
	// measured 2026-10-17 — the two were the same sentence, and a live ref was
	// reported as already deleted).
	finishCleanupBranchTipUnreadableNote = "could not read the branch here: the directory it would be read in is not there — an unreadable probe is not an absent ref (this is NOT a claim that the branch is gone)"
	// finishCleanupBranchStillHereNote is the measurement that replaces the
	// claim: asked of the MAIN repo, the ref RESOLVED, and the tip follows it.
	// Its second reader is finishCleanupReportSettled's owed marker, so a
	// surviving ref can never be counted as `settled` (Rules 1b/13/14).
	finishCleanupBranchStillHereNote = "the local branch IS still here and this pass did not delete it — tip "

	// procStatName is the per-process stat file — its 4th field is the
	// parent pid, the link the caller-ancestry walk follows. Alias of
	// procname.StatName.
	procStatName = procname.StatName

	// finishCleanupWorktreeVerb / finishCleanupWorktreeListVerb /
	// finishCleanupWorktreeRemoveVerb / finishCleanupWorktreeListFlag are the
	// git verb, actions and flag this file uses for worktrees. Rule 1: each
	// value lives in ONE place — the bare "worktree" appeared 8x here (both
	// porcelain parsers + both removal sites), so a rename or a verb typo had
	// 8 chances to diverge.
	finishCleanupWorktreeVerb       = "worktree"
	finishCleanupWorktreeListVerb   = "list"
	finishCleanupWorktreeRemoveVerb = "remove"
	finishCleanupWorktreeListFlag   = "--porcelain"

	// finishCleanupWorktreeAddVerb / finishCleanupWorktreeDetachFlag /
	// finishCleanupWorktreeForceFlag are the worktree-mutating git args the
	// package's own seams need (the verify child's private ground,
	// taskVerifyGround in tasks.go). They live here with the verb they modify
	// so "worktree" has ONE home (Rules 1b/13): the verify seam references
	// these, it never re-spells the git argv.
	finishCleanupWorktreeAddVerb    = "add"
	finishCleanupWorktreeDetachFlag = "--detach"
	finishCleanupWorktreeForceFlag  = "--force"

	// finishCleanupPlanMarker is the prefix of EVERY step detail a PLAN-ONLY
	// run (FinishCleanupPlan) records in place of the action it would have
	// taken. It is the one spelling that makes a plan observable as such: a
	// step carrying it was NOT performed, and a reader (operator, test) can
	// tell a dry run's report from an executed chain's without knowing which
	// mode produced it (Rule 1b/15 — an unmarked no-op would read as success).
	finishCleanupPlanMarker = "would "

	// finishCleanupPlanRemoveDetail is the worktree step's plan detail — the
	// removal the chain would have run, rendered as its argv (the same
	// `git worktree remove` shape the executed step runs).
	finishCleanupPlanRemoveDetail = finishCleanupPlanMarker + "run: git " +
		finishCleanupWorktreeVerb + " " + finishCleanupWorktreeRemoveVerb

	// finishCleanupPlanForceFlag / finishCleanupPlanDeleteFlag are the extra
	// argv tokens the plan renders for the two destructive halves git gates
	// (the client arm's --force removal, and the branch delete's own flag) —
	// spelled by reference so a plan can never advertise an argv the executor
	// does not run (Rules 1b/13). The branch token is the FORCE half because
	// that is what ④ runs (see its comment).
	finishCleanupPlanForceFlag  = "--force"
	finishCleanupPlanDeleteFlag = finishCleanupBranchForceDeleteFlag

	// finishCleanupPlanWouldRun / finishCleanupPlanNoReport are the plan
	// renderer's two non-skip verdicts: the row's guards all pass and the
	// chain would act (the step lines carry the what), or the chain returned
	// no report at all (a defensive branch — the caller has nothing to show
	// and must not read as "clean").
	finishCleanupPlanWouldRun = "would run"
	finishCleanupPlanNoReport = "no report returned by the chain"

	// finishCleanupPlanGoneGroundDetail is the GONE-GROUND shape's own
	// prediction: a row whose recorded worktree directory is not on disk has
	// its delivery half still pending, and the APPLY face runs the branch's own
	// checks over it. The revision is named by the branch's REF: a ref names a
	// commit whether or not any directory holds a checkout of it.
	//
	// WHAT IT NO LONGER SAYS (2026-10-12): this sentence used to promise the
	// published-anchor step of the trial-merge lane — the lane that fetched the
	// peer's master, merged it into the delivered branch and rebuilt the merged
	// tree. That lane is gone (the header of task_finish_premerge.go carries the
	// measurement), and a plan that kept promising it would predict an act the
	// execution never performs — worse than no prediction, because an operator
	// reads "would run" as "this is what happens".
	finishCleanupPlanGoneGroundDetail = finishCleanupPlanMarker + "run: " + finishCleanupBranchCheckStep +
		" — the packages this branch's own change touches are tested on the delivered revision (rev " +
		refsHeadsPrefix // + <branch> + …
	finishCleanupPlanGoneGroundDetailTail = "), before any hand-off"

	// finishCleanupPlanGoneGroundCaveat is the plan's HONEST SECOND HALF, and
	// the reason the sentence above says "run" rather than "verifies": what the
	// executed route will FIND is not knowable from here — an abstention if no
	// directory can be produced for the delivered revision, a refusal if the
	// branch's own tests are red at it, a pass if they are green — and a plan
	// that implied an outcome would promise what it cannot know. The verdict is
	// the lane's own, never a forecast of it.
	finishCleanupPlanGoneGroundCaveat = " — NOT predicted here (the execution decides it): whether a directory holding " +
		"the delivered revision can be produced at all (an abstention, recorded), whether a branch that is BEHIND the " +
		"integration branch is delivered anyway, and which verdict the branch's own test run returns — that verdict is the " +
		"lane's own, never a forecast of it"

	// finishCleanupWorktreePruneVerb / finishCleanupWorktreePruneFlag are
	// `git worktree prune -v`: git's OWN removal of the registrations it
	// holds for directories that are gone. Safe by construction (never
	// touches a directory, only a registration whose directory is missing),
	// which is why the reclaim's repair leg can run it without the three
	// removal guards. The flag is what makes the repair OBSERVABLE: prune
	// prints one line per registration it dropped and NOTHING when it had
	// none, which is the difference between "the sweep repaired something"
	// and "the sweep re-examined the same already-clean row every pass" —
	// the convergence property the caller relies on.
	finishCleanupWorktreePruneVerb = "prune"
	finishCleanupWorktreePruneFlag = "-v"

	// finishCleanupBranchDeleteFlag / finishCleanupBranchForceDeleteFlag are
	// `git branch`'s two deletion halves: the safe half refuses an unmerged
	// branch, the force half overrides exactly that refusal.
	//
	// WHICH HALF THE DELIVERY CHAIN'S STEP ④ USES (measured 2026-10-14, task
	// 01a131bb): the FORCE half, unconditionally. The safe half's question ("is
	// this branch merged into HEAD / its upstream?") is not ④'s question, and
	// where ④ stands makes it answer NO by construction: ② has just pushed the
	// ref to the DELIVERY POINT (the peer's checkout), so the tip is present on
	// the far side while this repo's HEAD still does not contain it — the
	// `-d`-then-`-D` shape paid for one guaranteed refusal per close-out. What
	// stands in the safe half's place is the chain's own pair of guards, neither
	// weakened here: axis 2 (finishCleanupBranchForceDeleteReason — ② must have
	// landed THIS ref) and the orphan guard (branchDeleteOrphanRefusalAt — the
	// tip must survive in another ref, the delivery point included). The ④ body
	// carries the full argument.
	//
	// The safe half is NOT a dead constant: its readers are the plan renderer
	// before the 2026-10-14 change and, today, the three arms that really do ask
	// git's merged-only question — worktree_sweep.go's worktreeSweepDeleteBranch
	// (a dead registration's branch, an inert chat ground's branch) and
	// task_merged_cleanup.go's orphan lane (a branch re-verified as master's
	// ancestor). Rule 13: a constant is a definition WITH readers, and these are
	// its readers.
	finishCleanupBranchDeleteFlag      = "-d"
	finishCleanupBranchForceDeleteFlag = "-D"

	// finishCleanupPorcelainWorktreePrefix / finishCleanupPorcelainBranchPrefix
	// are the `git worktree list --porcelain` line prefixes the two parsers
	// key on (derived from the verb above — never re-typed).
	finishCleanupPorcelainWorktreePrefix = finishCleanupWorktreeVerb + " "
	finishCleanupPorcelainBranchPrefix   = "branch "

	// finishCleanupPorcelainPrunableMark is git's `prunable` annotation on a
	// `git worktree list --porcelain` entry: git itself reports the entry's
	// directory as gone, so the registration belongs to `git worktree prune`,
	// never to a removal (`worktree remove --force` refuses exactly this
	// entry: exit 128, "is not a working tree"). It lives here, beside the
	// prefixes above, so every reader of that registry keys on ONE spelling of
	// the marker (Rule 14 — a drifted marker silently parses nothing).
	//
	// The `detached` annotation is NOT defined here: it already has its owner
	// (worktreePorcelainDetachedToken), and a second spelling of a value that
	// exists is a mirror, not a source of truth (Rule 14). This constant has
	// readers in two files (worktree_sweep.go's parser, and the merger's
	// candidate filter through it) — a constant with no reader is the shape
	// this note exists to prevent.
	finishCleanupPorcelainPrunableMark = "prunable"

	// procStatParentField is the parent-pid field's index in the fields
	// that FOLLOW the "name" column of procStatName (the name is parenthesised
	// and may itself contain spaces/parens, so it is stripped first). Derived
	// from procname's field NUMBER and the field that precedes the addressable
	// list rather than typed as a bare index (Rule 1a/13): the two can never
	// disagree.
	procStatParentField = procname.StatFieldParent - procname.StatFieldState

	// finishCleanupProcChainMaxHops bounds the caller-ancestry walk: a
	// pid chain cannot be deeper than this, and the bound also makes a
	// procfs cycle (or a re-parenting race) terminate.
	finishCleanupProcChainMaxHops = 64
)

// finishCleanupPIDDigits is the pid-directory alphabet as an indexable set
// (Rule 1: the membership test reads the same constant, never a literal).
var finishCleanupPIDDigits = func() [256]bool {
	var set [256]bool
	for i := 0; i < len(procNumericNameChars); i++ {
		set[procNumericNameChars[i]] = true
	}
	return set
}()

// FinishCleanupStep is one executed step of the close-out chain (the report
// is the audit trail — which step ran, what git said, where it stopped).
type FinishCleanupStep struct {
	Step   string `json:"step"`   // push | worktree | branch
	Target string `json:"target"` // branch / worktree path the step acted on
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"` // git output line / skip reason
}

// FinishCleanupReport is the chain outcome for one finished session row.
//
// PlanOnly records that this report came from FinishCleanupPlan: every action
// step is a PREDICTION carrying the finishCleanupPlanMarker prefix, nothing on
// disk and nothing in the ledger was touched. It rides the report (rather than
// only the caller's memory) because the CLI prints the report and an operator
// reading a "would remove …" line must be able to see that no run happened —
// the refusal/skip half needs no marker (a skip acts on nothing either way).
type FinishCleanupReport struct {
	TaskID     string `json:"task_id"`
	Status     string `json:"status,omitempty"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	PlanOnly   bool   `json:"plan_only,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
	// MainRepoCheckSkipped is the forward-reading line for the MAIN-repo
	// checklist (clean + HEAD on the integration branch) on the paths where
	// that check was NOT applied — a plan run, or a shape where the repo
	// argument already IS the run's own ground (there the chain's existing
	// dirty reading covers it). Rule 15: "I did not ask" is stated, never
	// rendered as a pass.
	MainRepoCheckSkipped string              `json:"main_repo_check_skipped,omitempty"`
	Steps                []FinishCleanupStep `json:"steps,omitempty"`

	// mainRepoChecked records that the main-repo checklist has already been
	// asked for this close-out. The check is wired at TWO places — the entry
	// (so a shape that returns early still cannot skip it) and each chain body
	// (the seam the tests drive) — and both calls share this one report pointer,
	// so the second is a no-op and the two git readings happen ONCE per
	// close-out (Rule 14: one reading, not two).
	mainRepoChecked bool
}

// finishCleanupPlanGoneGroundStep renders the gone-ground shape's
// published-anchor prediction for one branch. It is a function of the branch
// (the constants carry the wording), and it names the ONE step name the chain
// records (Rules 13/14: a plan and a run may not name the same step two ways).
func finishCleanupPlanGoneGroundStep(branch string) string {
	return finishCleanupPlanGoneGroundDetail + branch + finishCleanupPlanGoneGroundDetailTail
}

// taskFinishCleanupOn reports whether the close-out chain is enabled
// (default ON; explicit OFF values disable — the autoTaskRegister convention,
// read from the ONE vocabulary definition rather than re-spelled here).
func taskFinishCleanupOn() bool {
	return !config.IsFalseValue(os.Getenv(finishCleanupEnv))
}

// finishCleanupExemptBranchesResolved merges the built-in exemptions with
// the env-added ones (read in exactly one place, Rule 2). Matching is
// case-insensitive exact-branch-name.
func finishCleanupExemptBranchesResolved() map[string]bool {
	exempt := map[string]bool{"master": true, "main": true}
	for _, name := range strings.Split(finishCleanupExemptBranches, ",") {
		if n := strings.TrimSpace(name); n != "" {
			exempt[strings.ToLower(n)] = true
		}
	}
	if raw := strings.TrimSpace(os.Getenv(finishCleanupExemptEnv)); raw != "" {
		for _, name := range strings.Split(raw, ",") {
			if n := strings.TrimSpace(name); n != "" {
				exempt[strings.ToLower(n)] = true
			}
		}
	}
	return exempt
}

// finishCleanupBranchExempt reports whether a branch must never be cleaned
// (integration branch / archive ledger / env exemptions).
//
// §2.5 CRITERION REPAIR (epic archive-ref-reclaim, 2026-10-01). This predicate
// used to test `strings.HasPrefix(branch, archiveBranchPrefix)` — the NAME
// `archive/…`, which is the graveyard namespace a branch is RENAMED INTO, not
// the marker an archive decision leaves. The system's actual decided-exclusion
// mark is the TAG `refs/tags/archive/<branch>` (ArchiveTagReasoned), and a
// decided branch KEEPS its own name, so the prefix test never matched one: it
// exempted only an ephemeral `archive/…` chore branch, while every genuinely
// archived branch passed straight through the exemption that exists to name it.
// Measured on this host: `git for-each-ref refs/heads` lists no `archive/*`
// branch at all, and `refs/tags/archive/*` holds every decided loser.
//
// The test is therefore asked about the MARKER, and asked through the ONE
// predicate every other merge/cleanup path already shares
// (repoSweepArchiveExempt → the tag probe), so this arm cannot drift from the
// merger's exemption again (Rule 13: one definition, every reader references
// it). The graveyard NAME stays a valid exemption above it: a branch somebody
// renamed into archive/… is out of every path by its name, and that arm needs
// no git database to decide.
//
// repo is the git database the branch belongs to. "" keeps the old,
// conservative reading for every probe-free caller: no branch is exempted for a
// marker that could not be read.
func finishCleanupBranchExempt(repo, branch string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return true
	}
	if strings.HasPrefix(branch, archiveBranchPrefix) {
		return true
	}
	if repo != "" && repoSweepArchiveExempt(repo, branch) {
		return true
	}
	return finishCleanupExemptBranchesResolved()[strings.ToLower(branch)]
}

// finishCleanupGroundNotRecordedReason is the ONE spelling of the refusal for a
// row that names NO branch and whose own run directory does not either. It is a
// refusal rather than a fallback: the chain used to read the DELIVERABLE REPO's
// checked-out branch (the shared main checkout, on master) and call that
// "exempt", which reads as a legitimate no-op while saying nothing about this
// row's ground at all (Rule 15 — the measured 2026-10-12 shape, rows submitted
// from a task/* linked worktree).
func finishCleanupGroundNotRecordedReason() string {
	return "no deliverable branch recorded for this row and no branch readable in its own run directory (" +
		deliverableBranchKeyPhrase() + ") — inspection needed: the chain does not read the deliverable repo's HEAD, " +
		"because that is the SHARED main checkout and not this row's ground"
}

// deliverableBranchKeyPhrase names the missing key as an operator would see it
// (Rule 1: the key is spelled by its constant, never typed inline twice).
func deliverableBranchKeyPhrase() string {
	return "deliverable." + DeliverableBranchKey
}

// finishCleanupResolveBranch resolves the branch this close-out is about, from
// the ROW's own evidence and nothing else. Three sources, in order:
//
//	① the row's recorded deliverable.branch (the backfilled record — the
//	   ordinary shape);
//	② the row's OWN GROUND read off disk (finishCleanupRowGroundBranch below):
//	   a run that worked in a linked worktree has its HEAD there, so the
//	   branch is READ, not guessed;
//	③ nothing: SKIP with a NAMED refusal (finishCleanupGroundNotRecordedReason)
//	   naming the missing key — never the deliverable repo's HEAD.
//
// WHY ③ IS NOT A FALLBACK ANY MORE (the defect this resolver closes, measured
// 2026-10-12). The old code read `git -C <deliverable.repo> rev-parse
// --abbrev-ref HEAD`. `deliverable.repo` is normalized to the repo's DURABLE
// main checkout (DurableRepoForSubmit — a linked worktree cannot outlive its
// run), so that command answers whatever the SHARED checkout has parked on its
// HEAD: master, in the ordinary shape. The exempt arm then reported
// "branch master is exempt" for a row whose ground was a task/* worktree that
// nobody had read — a silent pass for a row that was never looked at
// (Rule 15). The exemption itself is untouched: an EXEMPT branch the row
// actually RECORDS (① / ②) still skips with the same sentence, which is the
// legitimate read of a row that genuinely names master.
//
// "" with a non-nil error leaves the refusal on the report; ("", nil) can only
// come back for a shape that has already recorded its own skip.
func finishCleanupResolveBranch(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable) (string, error) {
	if recorded := strings.TrimSpace(d.Branch); recorded != "" {
		return finishCleanupAcceptBranch(ctx, rep, d, recorded)
	}
	if own := finishCleanupRowGroundBranch(ctx, rep.TaskID, d); own != "" {
		return finishCleanupAcceptBranch(ctx, rep, d, own)
	}
	rep.Skipped = true
	rep.SkipReason = finishCleanupGroundNotRecordedReason()
	return "", nil
}

// finishCleanupRowGroundBranch reads the branch out of the row's OWN ground —
// never out of the deliverable repo, which is the SHARED main checkout. Three
// places are tried, each only when it already exists (a close-out READS; it
// creates nothing), and the first live one answers:
//
//	① the row's recorded worktree (deliverable.worktree);
//	② the row's EXECUTOR SESSION directory (the child's own cwd, resolved by
//	   the ONE reader the completion-time backfill uses);
//	③ the row's DETERMINISTIC ground directory (taskGroundDirFor — the derived
//	   name a worker creates this row's ground under), accepted only when it is
//	   a REGISTERED linked worktree of the row's repo (taskGroundBranchAt), so a
//	   foreign directory that happens to sit at that path is never read as this
//	   row's ground.
//
// "" when none is readable: the caller reports the named refusal.
func finishCleanupRowGroundBranch(ctx context.Context, taskID string, d taskFinishDeliverable) string {
	candidates := []string{strings.TrimSpace(d.Worktree)}
	if sess := taskWorkdirFromSession(strings.TrimSpace(taskID)); sess != "" {
		candidates = append(candidates, sess)
	}
	for _, dir := range candidates {
		if liveDir(dir) == "" {
			continue
		}
		if br := finishCleanupWorktreeBranch(ctx, dir); br != "" {
			return br
		}
	}
	repo := strings.TrimSpace(d.Repo)
	if repo == "" {
		return ""
	}
	ground := taskGroundDirFor(repo, taskID)
	if liveDir(ground) == "" || SameDir(ground, taskGroundMainCheckout(repo)) {
		return ""
	}
	br, err := taskGroundBranchAt(repo, ground)
	if err != nil || br == finishCleanupDetachedHead {
		return ""
	}
	return br
}

// finishCleanupAcceptBranch applies the exempt arm to a branch the ROW itself
// named (recorded deliverable or its own run directory) and returns it on the
// report. The exempt sentence keeps its ONE spelling, so a row that truly
// names an integration branch still skips identically.
func finishCleanupAcceptBranch(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable, branch string) (string, error) {
	if finishCleanupBranchExempt(d.Repo, branch) {
		rep.Skipped = true
		rep.SkipReason = "branch " + branch + " is exempt"
		return "", nil
	}
	return branch, nil
}

// finishCleanupGit runs one git command in dir with the chain timeout and
// returns (first meaningful output line, error). No shell — the arguments
// are the contract (exec.Command injection-safety convention).
func finishCleanupGit(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, finishCleanupGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	line := finishCleanupDiagnosticLine(out, err)
	if err != nil {
		return line, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, line)
	}
	return line, nil
}

// finishCleanupNoOutput is the sentinel that stands for "git printed nothing":
// for a SUCCESSFUL probe that means clean (empty porcelain, an empty tag
// list), which is why the callers test for it. It must never be the reported
// CAUSE of a FAILED probe — see finishCleanupDiagnosticLine.
const finishCleanupNoOutput = "(no output)"

// finishCleanupNoLinkedWorktreeMarker is the tail of the skip reason for a row
// with NOTHING to recycle — the branch has no linked worktree (the run worked
// on the main checkout, or an earlier pass already reclaimed the directory).
// Its own constant because finishCleanupReportSettled reads it to classify the
// skip as settled rather than as a refusal (Rules 1b/13).
const finishCleanupNoLinkedWorktreeMarker = " to recycle (main checkout / already gone)"

// finishCleanupNoServerTargetBase is the recognisable head of the "no push
// target resolved" skip (the line the client chain and the git-sync pass both
// print). A named constant because the reason is COMPOSED below and matched by
// tests — never a bare literal at two sites (Rules 1b/13).
const finishCleanupNoServerTargetBase = "no sync_peers resolved — no server to push to"

// finishCleanupNoPeerDeclaredNote says what an EMPTY target means when NO peer
// is declared (the single-node shape, "sync_peers": []): the hand-off does not
// merely fail here, it does not EXIST on this host — nothing ever pushes a
// deliverable branch to a server. Without this sentence the skip reads as a
// transient/retryable condition, which is exactly how a never-delivered branch
// stayed invisible (Rules 1b/13: one spelling, read by the reason renderer and
// the tests). The key name comes from config.SyncPeersSettingsKey — the
// settings key is never re-typed (Rule 1b: a keyword the rest of the system
// writes/compares from that constant).
const finishCleanupNoPeerDeclaredNote = "single-node: " + config.SyncPeersSettingsKey +
	" empty — this branch is NOT handed off to any server"

// finishCleanupMalformedPeerNote names the OTHER empty-target cause: a peer IS
// declared, but its alias cannot be composed into an ssh/git target (the
// peer-alias guard, internal/tools/peer_alias.go), so the empty target is a
// settings slip the operator can fix — distinct from the single-node
// deployment fact above. Same discipline as that note: one spelling, shared.
const finishCleanupMalformedPeerNote = "a sync peer IS declared but its alias is unusable (option-shaped/malformed ssh alias) — fix " + config.SyncPeersSettingsKey

// finishCleanupReasonSeparator joins a refusal's cause to an appended
// explanation ("no push target resolved — <why>"), so the composed reasons
// read as one sentence and every appended note is introduced the same way.
const finishCleanupReasonSeparator = " — "

// finishCleanupChdirMarker / finishCleanupCantChangeToMarker are the two
// spellings of "this directory is not there": exec reports its own chdir
// failure, git reports a failed `-C`/cwd. Read as CONSTANTS by
// finishCleanupMissingDirReason rather than typed at the test (Rule 1b).
const (
	finishCleanupChdirMarker        = "chdir "
	finishCleanupCantChangeToMarker = "cannot change to"
)

// finishCleanupProbeClean reads an EMPTY-PROBE probe: it reports whether git
// answered "nothing" (the clean porcelain / no-such-tag case), and "" when it
// answered anything else — in which case the answer IS the dirt (or the tag).
//
// It replaces the substring test `!strings.Contains(dirty, "no output")` that
// used to guard the dirty gate. That test made the sentinel a MATCH PATTERN
// over git's output: a real untracked file whose name carries "no output"
// (legal on disk) would have read as "clean" and let the chain pull the
// worktree out from under a live bond — the guard defeated by a filename.
// Derived from the sentinel constant (Rule 13 — the literal lives once).
func finishCleanupProbeClean(probe string) (clean bool, dirt string) {
	probe = strings.TrimSpace(probe)
	if probe == finishCleanupNoOutput {
		return true, ""
	}
	if probe == "" {
		// An empty string is the same finding as the sentinel: git said
		// nothing. Kept explicit so the two spellings cannot drift apart.
		return true, ""
	}
	return false, probe
}

// finishCleanupWorktreeDirtyReason is the ONE reading of the three arms' dirty
// gate (Rule 14): it returns "" when the tree is clean, else the NAMED refusal
// — and it splits the two states `git status --porcelain` conflates, exactly as
// the reclaim's guard ② does (reclaimDirtyReason, the sibling definition).
//
//	only untracked entries — the tree's tracked content equals its commit, so
//	  there is no uncommitted CHANGE to protect; what pins the directory is
//	  residue a run failed to clean up. A gopi run must leave its worktree
//	  without untracked files, so this state is reported as a hygiene debt
//	  rather than hidden inside the word "dirty" (this epic's case 1: 242 live
//	  refusals, ~58 of them this state, all rendered identically).
//	tracked modifications — real, committable work: left for a human.
//
// The comparison is against git's own plumbing output; the arm still reports
// the raw porcelain body in rep.SkipReason's caller when it needs the detail
// (nothing here is swallowed, Rule 15).
func finishCleanupWorktreeDirtyReason(rep *FinishCleanupReport, porcelain string) string {
	trimmed := strings.TrimSpace(porcelain)
	// The clean probe is compared EXACTLY, never as a substring of git's
	// output: a real untracked file whose name carries the sentinel (legal on
	// disk) must read as DIRT, or a filename defeats the live-bond guard — the
	// same repair finishCleanupProbeClean makes, derived from the same single
	// constant (Rules 1b/13).
	if trimmed == "" || trimmed == finishCleanupNoOutput {
		return ""
	}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, reclaimUntrackedProbePrefix) {
			return finishCleanupDirtyTrackedPrefix + trimmed
		}
	}
	if rep != nil {
		rep.Steps = append(rep.Steps, FinishCleanupStep{
			Step: finishCleanupStepWorktree, Target: rep.Worktree, Detail: finishCleanupUntrackedResidueNote})
	}
	return finishCleanupDirtyUntrackedPrefix + trimmed
}

// finishCleanupDirtyTrackedPrefix / finishCleanupDirtyUntrackedPrefix begin the
// two dirty refusals (one spelling each — the sweep and any test match on the
// same string, Rule 14).
const (
	finishCleanupDirtyTrackedPrefix   = "worktree dirty (live bond protected) — tracked changes: "
	finishCleanupDirtyUntrackedPrefix = "worktree dirty (live bond protected) — carries ONLY UNTRACKED files (build output / scratch residue), a run's own hygiene, not uncommitted work: "
	// finishCleanupUntrackedResidueNote is the durable step record for the
	// untracked-only state: the skip reason is one line in a pass log, while
	// the step survives in the report an operator reads afterwards.
	finishCleanupUntrackedResidueNote = "untracked-only residue in the worktree of a finished run — the run must clean up after itself (removal deliberately skips: git's own safety, no uncommitted change exists)"
)

// The REMOVAL-REFUSED skip reasons (epic: client-push-rebase, 2026-09-19).
//
// The three states below are the ones a *removal* may refuse on: a dirty
// worktree, a directory that is not this row's (gate ①), and a directory that
// is GONE. None of them is an obstacle to the DELIVERY — the push moves a ref
// and transfer nothing from the working tree — so the chain records the
// refusal and goes on to deliver. Before this epic each of the three returned
// a report that said `Skipped` at the HEAD of the chain, so a run that left
// one untracked scratch file behind never handed its branch over at all
// (devdoc/hp-push-diagnosis-20260916.md §1, finding A/A′), and a close-out is
// a SINGLE SHOT per row: nothing re-runs it.
//
// Each marker is the skip reason's PREFIX — the refusal is followed by the
// named state (filenames for dirt, the two paths for an ownership mismatch,
// git's own diagnostic for a gone directory), so an operator reads WHICH
// condition refused without opening the ledger. Rule 1b: matched by name in
// FinishCleanupSkipIsReportable and in tests, so each is spelled once.
const (
	// finishCleanupBranchAbsentDeliveryNote is the step record for a row whose
	// branch ref is not here: nothing to hand over (an earlier pass delivered
	// and removed it, or the work never had a ref). Recorded as a SUCCESSFUL
	// no-op — the push half is done — so a settled row is not reported as a
	// failed one (measured: a second close-out pass on a delivered row used to
	// print `error: src refspec <branch> does not match any`).
	finishCleanupBranchAbsentDeliveryNote = "no local branch to push (already delivered, or no ref was ever created) — nothing to hand over"
	// finishCleanupDeliveryClaimedPrefix is the ONE spelling of what these
	// refusal reasons say about the DELIVERY, and it says only what the chain
	// can know: the delivery is NOT established by the condition refusing the
	// removal — the chain's own push step is what establishes it, later in the
	// same chain.
	//
	// WHY THE SPELLING CHANGED (measured 2026-10-13, row
	// 01a130c6d8c87a78bae7a546c660523f's epic). The three markers below used to
	// read "… (delivery already attempted)". That is a claim about a DELIVERY
	// drawn from a fact about the DIRECTORY, and on a real two-node world it is
	// false in exactly the shape this arm exists for: the row's trail read
	// `[completed align-unverified premerge-unverified finish-cleanup-removal-refused]`
	// — NO `delivery-verified` event anywhere — while the reason still asserted a
	// delivery outcome. An operator reading it concludes the branch is on the
	// peer and stops looking, which is the failure the surrounding comments were
	// written to end. The word "already" went with it: it named the disposition
	// of another pass this chain cannot see, where the chain knows only its OWN
	// behaviour (it attempts the delivery after this refusal — or, when its own
	// push was refused, says so with finishCleanupDeliveryRefusedNote).
	// The spelling is deliberately SHORT, because this clause sits inside the
	// gone-directory marker that `gopi task list` clips at 60 runes
	// (taskListCloseoutReasonLimit): a scanner greps the list line for it, so a
	// long clause would be cut before an operator could see it — and the whole
	// point of the change is that the DELIVERY state is readable, not a claim
	// taken on faith (Rule 1b: one spelling, both readers).
	finishCleanupDeliveryClaimedPrefix = "delivery not established"
	// finishCleanupDeliveryRefusedNote is the fallback sentence for a reason
	// whose report carries NO evidence either way — the delivery could not be
	// established. A statement about this report, never about the directory
	// (Rule 15); where THIS run's push step refused, the composer states that in
	// its own words (finishCleanupGoneWorktreeDeliveryPushRefusedNote).
	finishCleanupDeliveryRefusedNote      = "this close-out did not attempt the delivery (its push step is absent or was refused)"
	finishCleanupDirtyTrackedSkipMarker   = "removal refused (" + finishCleanupDeliveryClaimedPrefix + "): " + finishCleanupDirtyTrackedPrefix
	finishCleanupDirtyUntrackedSkipMarker = "removal refused (" + finishCleanupDeliveryClaimedPrefix + "): " + finishCleanupDirtyUntrackedPrefix
	// finishCleanupGoneWorktreeDirAbsentHead is the CLIP-SURVIVING head of the
	// same reason and the DIRECTORY fact the arm knows — its own dedicated
	// constant (Rules 1b/14), so no reader slices the marker by index. The list
	// line, the row's trail and the full report all name this one spelling.
	finishCleanupGoneWorktreeDirAbsentHead = "the recorded worktree directory is not there"
	// finishCleanupGoneWorktreeSkipMarker is the gone-directory REMOVAL reason's
	// prefix — extracted because two readers match on it (the settled
	// classifier and the tests), the Rule 14 discipline.
	//
	// ORDER: the DIRECTORY fact comes FIRST and the delivery clause after it,
	// because `gopi task list` clips a row's reason at 60 runes
	// (taskListCloseoutReasonLimit) and this arm's whole reason is longer than
	// that — so what survives the clip is the fact the arm MEASURED, and the
	// delivery state (stated in full by the chain's own report and by the row's
	// trail) follows it. The "removal skipped:" prefix the marker used to carry
	// is gone with it: the skip is already said by the row's own verdict token
	// and by the word `skipped` on the CLI line, so prefixing it again only cost
	// the clip budget the directory fact needs.
	finishCleanupGoneWorktreeSkipMarker = finishCleanupGoneWorktreeDirAbsentHead +
		" — nothing to remove (" + finishCleanupDeliveryClaimedPrefix + "; "
	// THE OWED SENTENCES — three shapes, each read off the report's OWN push
	// step, never predicted and never inferred from the directory (Rule 15).
	// The gone-worktree marker above states only the DIRECTORY fact; these say
	// what is known about the DELIVERY, and which sentence applies is decided by
	// finishCleanupRemovalReasonWithDelivery from the report's own steps:
	//
	//	· absent push step → the half has not run in this chain;
	//	· refused push step → this close-out RAN it and it did not land;
	//	· succeeded push step → the delivery IS established, by this chain's own
	//	  act (the sentence names the evidence);
	//	· a branch that was ALREADY absent → also evidence, but of another shape:
	//	  somebody's earlier pass (or no ref at all), which is not the same claim.
	//
	// Before this change the reason carried "… (delivery already attempted)" in
	// all of them, from the directory's absence alone; measured on a real
	// two-node world, a row in this state read
	// `[completed align-unverified premerge-unverified finish-cleanup-removal-refused]`
	// with no delivery event anywhere while the reason asserted a delivery
	// outcome (row 01a130c6d8c87a78bae7a546c660523f's epic).
	finishCleanupGoneWorktreeDeliveryOwedNote = "the local branch IS still here — the delivery half has not run for this row, and the absent directory is not evidence that it did"
	// finishCleanupGoneWorktreeDeliveryPushRefusedNote is the sentence for the
	// report whose own push step did NOT succeed: the case is not "not run",
	// this close-out RAN the delivery and it was refused. Read off that step
	// (finishCleanupPushStepRefused), so the reason states the condition THIS
	// run measured.
	finishCleanupGoneWorktreeDeliveryPushRefusedNote = "the local branch IS still here and this close-out's own push step was refused — the branch has not crossed, and the absent directory is not evidence that it did"
	// finishCleanupGoneWorktreeDeliveredNote: the report's OWN push step
	// succeeded, so the delivery IS established — by the act this chain
	// performed, which is the only thing that can establish it (Rule 41: the
	// measurement is named, and it is named HERE and nowhere else).
	finishCleanupGoneWorktreeDeliveredNote = "the delivery IS established, by this close-out's own push step — the revision it carried was handed to the peer"
	// finishCleanupGoneWorktreeRefAbsentNote is the OTHER evidence shape: the
	// local ref was already absent when this chain looked, so nothing was left
	// to hand over. It witnesses that the branch did not NEED delivering — an
	// earlier pass delivered it and ④ reaped the ref, or no ref was ever
	// created — which is not the same statement as "this run delivered it", and
	// saying "delivered" here would put this chain's name on another's act.
	finishCleanupGoneWorktreeRefAbsentNote = "no delivery was owed by this pass (the local branch was already absent) — nothing was left to hand over"
	// finishCleanupDeliveryOwedPrefix heads the reason of a row that still owes
	// the delivery: the directory fact first, the delivery state after it.
	finishCleanupDeliveryOwedPrefix = "delivery owed: "
	// finishCleanupDeliveryNotEstablishedPrefix heads the reason of a row whose
	// delivery could not be established at all (no evidence in either
	// direction) — the honest head where the old wording asserted the opposite.
	finishCleanupDeliveryNotEstablishedPrefix = "delivery not established: "
	// finishCleanupGoneWorktreeDeliveredNote is the third shape, and the one the
	// old wording claimed unconditionally: the report's OWN push step succeeded,
	// so the delivery IS established — by the act this chain performed, which is
	// the only thing that can establish it (Rule 41: the measurement is named).
	// finishCleanupDeliveryOwedSkipMarker prefixes the skip reason of a row the
	// chain refused at its delivery half — not at a removal guard — so the
	// settled classifier can tell "nothing left to do" from "the one act this
	// chain exists for was never performed" without parsing the removal
	// wording. Matched by finishCleanupReportSettled only: it states the
	// delivery, never an action (Rules 1b/13).
	finishCleanupDeliveryOwedSkipMarker = "delivery owed: "
	// finishCleanupGoneWorktreeSkipFmt appends git's own diagnostic, which
	// names the directory that failed — not necessarily the row's recorded one
	// (the chain may be cleaning a directory resolved by fallback).
	finishCleanupGoneWorktreeSkipFmt = finishCleanupGoneWorktreeSkipMarker + "%s)"
	// finishCleanupOwnerMismatchSkipMarker prefixes gate ①'s refusal. The
	// ownership gate protects the DIRECTORY (a directory this row does not own
	// is another row's ground — never entered, probed or removed); it has no
	// business stopping the push — and it says nothing about the delivery, for
	// the same reason the two markers above do not.
	finishCleanupOwnerMismatchSkipMarker = "removal refused (no probe of a directory this row does not own; " + finishCleanupDeliveryClaimedPrefix + "): "
)

// finishCleanupDirtySkipReason renders the dirty refusal as a SKIP REASON.
// The prefix constants above begin the *reclaim* arm's reason (they keep their
// exact spelling — the sweep and cmd/agent match on them); this renderer
// prepends the delivery-first context and appends the same git porcelain body,
// so nothing about the condition is hidden (Rule 15).
func finishCleanupDirtySkipReason(reason string) string {
	switch {
	case strings.HasPrefix(reason, finishCleanupDirtyTrackedPrefix):
		return finishCleanupDirtyTrackedSkipMarker + strings.TrimPrefix(reason, finishCleanupDirtyTrackedPrefix)
	case strings.HasPrefix(reason, finishCleanupDirtyUntrackedPrefix):
		return finishCleanupDirtyUntrackedSkipMarker + strings.TrimPrefix(reason, finishCleanupDirtyUntrackedPrefix)
	}
	return reason
}

// finishCleanupGoneWorktreeSkipReason renders the gone directory as a SKIP
// REASON, carrying git's own diagnostic (the chdir refusal IS the evidence of
// absence — see finishCleanupGoneWorktreeNoPeersNote for why that word
// matters to an operator).
func finishCleanupGoneWorktreeSkipReason(diagnostic string) string {
	return fmt.Sprintf(finishCleanupGoneWorktreeSkipFmt, "git status: "+diagnostic+finishCleanupGoneWorktreeNoPeersNote(diagnostic))
}

// finishCleanupRemovalReasonWithDelivery composes a REMOVAL reason with the
// delivery half stated as the report measured it — the ONE composer every arm
// reads (Rule 14: the plan arm, the apply arm and the not-reached arm call this,
// so no two of them can describe the delivery differently).
//
// WHY THE REASON CARRIES IT: the removal reason is the one string every reader
// of this state already has (the plan's verdict, the settle classifier, the
// operator's pass line), and its own head is a statement about the DIRECTORY
// ("the recorded worktree directory is not there"). An operator who reads only
// that concludes the delivery is behind them — the conclusion the old wording
// ("delivery already attempted") handed them for every shape, from the
// directory's absence alone. So each shape says what THIS run measured, and no
// shape says more:
//
//	push step refused        → the row still owes the delivery AND this run
//	                           attempted it and it did not land;
//	delivery evidenced       → the sentence naming the evidence — which of the
//	                           two evidence shapes it is decides the wording
//	                           (this chain's own push / an earlier pass's);
//	owed                     → the delivery half has not run in this chain;
//	none of the above        → "delivery not established", with the reason.
func finishCleanupRemovalReasonWithDelivery(rep *FinishCleanupReport, removeWhy string, owed bool) string {
	switch {
	case removeWhy == "":
		return ""
	case finishCleanupPushStepRefused(rep):
		return finishCleanupDeliveryOwedPrefix + removeWhy + finishCleanupReasonSeparator +
			finishCleanupGoneWorktreeDeliveryPushRefusedNote
	case finishCleanupDeliveryEvidenced(rep):
		return removeWhy + finishCleanupReasonSeparator + finishCleanupDeliveryEvidenceNote(rep)
	case owed:
		return finishCleanupDeliveryOwedPrefix + removeWhy + finishCleanupReasonSeparator +
			finishCleanupGoneWorktreeDeliveryOwedNote
	}
	return finishCleanupDeliveryNotEstablishedPrefix + removeWhy + finishCleanupReasonSeparator +
		finishCleanupDeliveryRefusedNote
}

// finishCleanupDeliveryEvidenceNote names WHICH evidence the report shows — the
// two evidence shapes are not interchangeable, because one of them is this
// close-out's own act and the other is a statement about a ref that was ALREADY
// absent when this chain looked (Rule 15: an arm names the condition it knows,
// and "we delivered it" is not "somebody did").
func finishCleanupDeliveryEvidenceNote(rep *FinishCleanupReport) string {
	if finishCleanupPushStepExecuted(rep) {
		return finishCleanupGoneWorktreeDeliveredNote
	}
	return finishCleanupGoneWorktreeRefAbsentNote
}

// finishCleanupPushStepExecuted reports whether this report's own push step RAN
// and SUCCEEDED — the strongest delivery evidence the chain can have, because it
// is the act itself rather than a reading of a ref.
func finishCleanupPushStepExecuted(rep *FinishCleanupReport) bool {
	if rep == nil {
		return false
	}
	for _, s := range rep.Steps {
		if s.Step != finishCleanupPushVerb || !s.OK {
			continue
		}
		if strings.Contains(s.Detail, finishCleanupBranchAbsentDeliveryNote) ||
			strings.HasPrefix(strings.TrimSpace(s.Detail), finishCleanupPlanMarker) {
			continue
		}
		return true
	}
	return false
}

// finishCleanupPushStepRefused reports whether this report RAN its push half and
// the push did not succeed — the `!OK` push step whose detail is neither the
// branch-absent no-op nor a plan's prediction. It is the second input the
// delivery sentences are read from (Rule 15: a shape that is measured is named,
// so the owed sentence never describes a delivery this run already attempted).
func finishCleanupPushStepRefused(rep *FinishCleanupReport) bool {
	if rep == nil {
		return false
	}
	for _, s := range rep.Steps {
		if s.Step != finishCleanupPushVerb || s.OK {
			continue
		}
		if strings.Contains(s.Detail, finishCleanupBranchAbsentDeliveryNote) ||
			strings.HasPrefix(strings.TrimSpace(s.Detail), finishCleanupPlanMarker) {
			continue
		}
		return true
	}
	return false
}

// finishCleanupGoneWorktreeDeliveryOwed reports whether a row's delivery half
// still owes an act, in the gone-directory refusal state: the branch is HERE
// and this report carries no evidence that the delivery happened.
//
// The delivery evidence is the report's OWN executed push step
// (finishCleanupDeliveryEvidenced) — the same source of standing the rest of
// the chain reads — never the absent directory, and never a prediction: a plan
// appends its push as a marked prediction, so a plan run (which runs nothing)
// always reads "owed", and an apply run reads it from what it actually did.
// The gone-directory condition is required because that is the state whose
// marker makes the false claim; a dirty worktree's refusal says nothing about
// delivery and keeps its current reading (reach unchanged).
func finishCleanupGoneWorktreeDeliveryOwed(rep *FinishCleanupReport, removeWhy string, branchPresent bool) bool {
	if !branchPresent || removeWhy == "" || !finishCleanupWorktreeDirAbsent(removeWhy) {
		return false
	}
	return !finishCleanupDeliveryEvidenced(rep)
}

// finishCleanupDeliveryEvidenced reports whether this report shows the delivery
// having HAPPENED. Two shapes count, and both are statements the CHAIN made
// about a ref, never an inference from a directory:
//
//	· an executed push step (OK, no plan marker) — the ref was handed over;
//	· the recorded no-op for a branch that is not here
//	  (finishCleanupBranchAbsentDeliveryNote): nothing was left to hand over,
//	  because an earlier pass delivered it and ④ reaped the ref, or the work
//	  never had one. That note is written exactly when the local ref is ABSENT,
//	  so it is measurement too — and reading it as evidence is what keeps the
//	  second pass over a delivered row SETTLED (the 2026-09-16 disposition: a
//	  clean host must not read as a stuck sweep).
//
// A PREDICTION never counts (the marker test is the same one
// finishCleanupReportRemovedWorktree uses): "would run: git push …" is a
// statement about a plan, and reading it as delivery is the defect this epic
// closes one level down.
func finishCleanupDeliveryEvidenced(rep *FinishCleanupReport) bool {
	if rep == nil {
		return false
	}
	for _, s := range rep.Steps {
		if s.Step != finishCleanupPushVerb {
			continue
		}
		if strings.Contains(s.Detail, finishCleanupBranchAbsentDeliveryNote) {
			return true
		}
		if !s.OK {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(s.Detail), finishCleanupPlanMarker) {
			continue
		}
		return true
	}
	return false
}

// finishCleanupBranchExists reports whether the repo holds a ref for branch.
// A git failure reads as PRESENT (the conservative side: the push then runs and
// reports its own refusal, rather than this probe suppressing a delivery it
// could not judge — Rule 15).
//
// THE DOCUMENTED CONTRACT AND THE BODY DISAGREED UNTIL 2026-10-17, and the
// disagreement was a measured defect, not a comment slip: the body read
// `return err == nil`, i.e. a probe that could not run answered "the branch does
// not exist". The chain reaches this probe with a `branchDir` that may itself be
// GONE (the live shape: `deliverable.repo` points AT the worktree, and that
// directory has been removed), where `git rev-parse --verify` cannot even chdir
// — so every such row reported `no local branch … the ref is already absent`
// while `git branch` still listed the ref, and step ④'s own tip reading (the
// same failure) turned the deletion into a no-op. A failure is now what the
// sentence above always said it was: NOT a measurement of absence.
func finishCleanupBranchExists(ctx context.Context, repoDir, branch string) bool {
	_, err := finishCleanupGit(ctx, repoDir, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeVerifyFlag, finishCleanupPreMergeQuietFlag, refsHeadsPrefix+branch)
	return err == nil || finishCleanupProbeCouldNotRun(ctx, repoDir)
}

// finishCleanupProbeCouldNotRun reports whether a ref probe failed because the
// DIRECTORY could not be read at all, rather than because the ref is absent.
// The two are distinguished by the ONE reading every other arm of this chain
// already uses for the same state — git's own chdir diagnostic
// (finishCleanupMissingDirReason) — never by a second `stat` of the path
// (Rule 14: one reader, so "gone" cannot mean two things in one chain).
func finishCleanupProbeCouldNotRun(ctx context.Context, repoDir string) bool {
	if strings.TrimSpace(repoDir) == "" {
		return true
	}
	// PARSED porcelain: stdout only.
	out, err := finishCleanupGitParsedStdout(ctx, repoDir, finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err == nil {
		return false // the directory IS readable: the probe's failure was about the ref
	}
	return finishCleanupMissingDirReason(out) || finishCleanupMissingDirReason(err.Error())
}

// finishCleanupBranchTip resolves branch's tip in repoDir, and returns "" when
// the ref does not resolve. The second return value says WHICH of the two
// answers this is — a MEASURED absence, or a probe that could not run — because
// a caller that deletes refs may not treat "I could not look" as "there is
// nothing there" (Rule 15; measured: a gone `branchDir` produced exactly that,
// and the ref survived the close-out that reported it already gone).
func finishCleanupBranchTip(ctx context.Context, repoDir, branch string) string {
	out, err := finishCleanupGit(ctx, repoDir, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeVerifyFlag, refsHeadsPrefix+branch)
	if err != nil {
		return ""
	}
	tip := strings.TrimSpace(out)
	if tip == finishCleanupNoOutput {
		return ""
	}
	return tip
}

// finishCleanupBranchTipUnreadableReason renders the unreadable-probe refusal
// with the directory it names, so an operator can go and look (Rules 1b/14:
// one renderer, one spelling, and the note itself is never re-typed).
func finishCleanupBranchTipUnreadableReason(dir string) string {
	return finishCleanupBranchTipUnreadableNote + " (" + dir + ")"
}

// finishCleanupRemovalReason decides whether the REMOVAL half of the chain may
// run, and returns the reason it may not ("" = clear). Three conditions, one
// question — "may this directory be removed?" — and deliberately NOT "may the
// delivery proceed?": dirt, a directory this row does not own, and a directory
// that is not there each keep the directory and the ref exactly as they were,
// while the branch is still owed to the peer (epic client-push-rebase).
//
// It calls BOTH of its collaborators rather than re-implementing their
// readings: finishCleanupOwnerDeclaredReason (gate ①) and
// finishCleanupWorktreeDirtyReason (the two dirty states, and the untracked
// note it records as a step).
func finishCleanupRemovalReason(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable, wt, linked string) string {
	if why := finishCleanupOwnerDeclaredReason(d.Worktree, linked); why != "" {
		// A directory this row does not own is never entered — not even to
		// read its porcelain (a probe of foreign ground is exactly what gate ①
		// exists to refuse), and never removed.
		return finishCleanupOwnerMismatchSkipMarker + why
	}
	// PARSED porcelain: stdout only — this reading is what refuses a removal.
	dirty, err := finishCleanupGitParsedStdout(ctx, repoDirForStatus(d.Repo, wt), finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		// The directory is GONE (git cannot change into it) or unreadable.
		// BEFORE THIS EPIC this state aborted the whole chain, which is how
		// a row whose directory was removed by hand reported "never handed
		// off" instead of "the directory is gone"
		// (devdoc/hp-push-diagnosis-20260916.md A′). It is now a
		// REMOVAL-side fact, and the delivery proceeds — with ONE
		// deliberate narrowing: a gone directory is also the branch-check
		// lane's missing GROUND, so the chain pushes with the branch's own
		// tests UNMEASURED (that is the ABSTAIN path
		// finishCleanupBranchCheckOffBranchMark reports).
		// The narrowing this epic does NOT take is the one that gave up on
		// the row: the delivery is still attempted.
		//
		// THE DIAGNOSTIC COMES FROM THE ERROR, not from the parsed stdout: this
		// renderer's contract is to carry git's own chdir sentence ("fatal:
		// cannot change to '<path>': No such file or directory"), and under the
		// parsed reader that sentence lives in the ERROR (the whole point of
		// the split — epic git-parsed-stdout-only). Reading stdout here would
		// hand an always-empty string to finishCleanupMissingDirReason, whose
		// marker pair is what recognises the GONE state at all (measured
		// 2026-10-27: it made a gone worktree read as "delivery not established"
		// with no diagnostic).
		return finishCleanupGoneWorktreeSkipReason(err.Error())
	}
	// Two dirty states, two NAMES (the split the reclaim's guard ② makes,
	// reclaimDirtyReason): the refusal is unchanged — the directory is never
	// force-removed over uncommitted work — it simply stops the REMOVAL only,
	// never the push (epic client-closeout-delivery's judgement, restored here
	// after 085811f8 backed its tree out).
	return finishCleanupDirtySkipReason(finishCleanupWorktreeDirtyReason(rep, dirty))
}

// finishCleanupDeliveryPremise reports why the delivery must NOT be attempted
// ("" = attempt it). It is deliberately DIRECTIONAL: it only ever fires when
// the premise the delivery lane depends on is provably ABSENT, so no delivery
// that would otherwise have happened is suppressed (epic client-push-rebase).
//
// THE PREMISE: the branch-check lane must have a directory checked out ON the
// delivered branch to run its tests in (finishCleanupBranchCheckVerify →
// finishCleanupBranchCheckGroundOnBranch). With no such ground the lane ABSTAINS
// — a recorded abstention, never a silent no-op — and the chain delivers a
// branch whose own tests it could not measure.
//
// The accepted blind spot, stated rather than hidden: when the registered
// worktree is not this row's ground (gate ①) the CHAIN can still push, without
// having looked at that directory. The pairing gate above already refuses to
// probe or remove it, so the delivery is left to the binding defect's own root
// cause (a re-attached worktree).
func finishCleanupDeliveryPremise(ctx context.Context, d taskFinishDeliverable, branch, wt, linked string) string {
	why := finishCleanupOwnerDeclaredReason(d.Worktree, linked)
	if why == "" {
		return "" // ownership agrees: the pre-merge gate's ground is usable
	}
	// Ownership disagrees. Does the branch-check lane still have a ground? It
	// resolves one exactly like the chain's push block does.
	checkGround := wt
	if checkGround == "" && finishCleanupOwningWorktree(ctx, d.Repo) != "" {
		checkGround = d.Repo
	}
	if checkGround != "" && finishCleanupBranchCheckGroundOnBranch(ctx, checkGround, branch) {
		return "" // it does: the ordering is honoured, the delivery proceeds
	}
	return finishCleanupOwnerMismatchSkipMarker + why
}

// finishCleanupRemovalNotReachedNote is the sentence appended when the chain
// stops before the removal: the delivered branch is on the peer and the local
// directory is STILL THERE, which is the state an operator has to act on. It
// exists because rep.Skipped is not read on that path (a report carrying a
// push step reads as progress), so silence there would be a Rule 15 blind spot.
const finishCleanupRemovalNotReachedNote = " — the worktree was NOT collected (the chain stopped before the removal)"

// finishCleanupRemovalRefusedEvent is the audit-trail event name a close-out
// writes when its DELIVERY LANE refuses.
//
// THE LANE, NOT ONE OF ITS TWO OUTCOMES. The lane is the delivery half of the
// close-out — "push the branch to the peer, then collect the local ground" —
// and a refusal in it has exactly two shapes, which the DETAIL distinguishes so
// a reader does not have to infer: the delivery was WITHHELD before any step
// ran (the chain stopped at the main-repo gate; the branch is NOT on the peer —
// the reason carries finishCleanupDeliveryWithheldMarker), or the delivery RAN
// and the local removal was declined afterwards (the branch IS on the peer).
// One event name for both lets a single reader answer "did this row's close-out
// refuse?" without a list of names that can silently fall out of date; the
// distinguished state rides the durable sentence, and the durable sentence is
// the record.
//
// The name kept its historical spelling (measured 2026-10-12: this host's
// ledger holds 2063 rows under it, every one written by the removal arm) so no
// existing trail row becomes unreachable by name; the fix is the SECOND writer
// — the main-repo gate — not a rename.
//
// It exists because
// devdoc/requirements/merge-and-branches.md B2 names the gap: the pre-merge
// refusal was recorded on the row's event trail and the dirty skip was NOT,
// "so the one condition that strands a delivery is the one that leaves no trace
// beyond a stderr line (Rule 15's spirit, violated in the arm that needed it
// most)". The reordering makes this event the durable half of the refusal.
const finishCleanupRemovalRefusedEvent = "finish-cleanup-removal-refused"

// finishCleanupPushCarriesTipMarker heads the step detail's tip clause: the
// revision this sweep handed to the peer, recorded at the push so
// "which commit did the delivery carry?" is answerable from the row's own
// report instead of a hand investigation (epic 01a12da7; the same question
// the branch audit record answers one step later, with the tip captured
// before that ref goes away).
const finishCleanupPushCarriesTipMarker = " — carried tip: "

// finishCleanupPushCarriesNoTipMarker is the recognisable head of the "the
// push had nothing to carry" refusal, matched by tests and by an operator's
// grep over a refusal trail (Rules 1b/13: one spelling, one definition site).
const finishCleanupPushCarriesNoTipMarker = "push would carry no revision"

// finishCleanupPushTipMovedMarker is the recognisable head of the refusal
// raised when the ref moved ACROSS the push: the revision this sweep resolved
// and aligned is not the revision the ref held when the push returned. Matched
// by tests and by an operator's grep over a refusal trail (Rules 1b/13).
const finishCleanupPushTipMovedMarker = "the branch moved across the push"

// finishCleanupPushCarriesTheRefTip answers the ONE question this seam can
// answer about "what did the push carry": is the revision the ref holds once
// the push returned the SAME revision the sweep resolved immediately before
// it? "" = yes (a delivery this sweep can NAME), else the NAMED refusal.
//
// WHICH QUESTION THIS IS, AND WHY NOT ANOTHER. A push of `<branch>:<branch>`
// carries the LOCAL REF's tip — git resolves the src ref inside the push,
// which is why no local read taken before the transfer can by itself declare
// what left. The strongest statement a local observer can make is therefore
// the comparison below: the ref before the push and the ref after it hold the
// same revision, so the revision this sweep aligned and verified is the one
// the ref held ACROSS the transfer. A ref that moved inside that window is
// refused BY NAME (both hashes, never a guess about which one the peer got),
// because settling is exactly the claim 01a12ba1's trail made about a revision
// the peer did not have.
//
// Fail-closed: an ref that resolves to nothing after the push is not evidence
// that a delivery happened.
func finishCleanupPushCarriesTheRefTip(target, branch, verified, carried string) string {
	verified = strings.TrimSpace(verified)
	carried = strings.TrimSpace(carried)
	if carried == "" {
		return finishCleanupPushCarriesNoTipMarker + " (" + branch + " → " + target + "): no revision resolves at " +
			"refs/heads/" + branch + " once the push returned — a delivery that cannot name its revision is not one " +
			"this chain may record as landed (the branch, its commits and its ground are kept)"
	}
	if carried == verified {
		return ""
	}
	return finishCleanupPushTipMovedMarker + " (" + branch + " → " + target + "): the ref held " +
		branchDeleteShortHash(verified) + " when this sweep resolved it and " + branchDeleteShortHash(carried) +
		" when the push returned — git resolves the src ref inside the push, so the peer may hold EITHER revision " +
		"and this sweep will not record a delivery it cannot name. The branch, its commits and its ground are kept"
}

// finishCleanupPushCarriesTipReason records the delivery refusal on the row's
// trail — the SAME recorder (and the SAME event name) every other refusal of
// this chain uses, so one query on `task_events` finds all of them (Rule 35:
// no new state file, no new sweep, no second mechanism).
func finishCleanupPushCarriesTipReason(taskID, why string) {
	finishCleanupRemovalRefusedReason(taskID, why)
}

// finishCleanupRemovalRefusedReason records the refusal on the row's trail.
// Best-effort in the same shape as the pre-merge record: a ledger that cannot
// be opened logs and returns, never failing the delivery that already happened.
func finishCleanupRemovalRefusedReason(taskID, detail string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || strings.TrimSpace(detail) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("finish-cleanup: removal-refusal record for %s not written (ledger unavailable): %v", taskID, err)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, finishCleanupRemovalRefusedEvent, detail)
}

// finishCleanupNoteRemovalNotReached appends that sentence to the removal
// reason so a caller that reports only the FAILING step still learns the
// directory survived. Called on every early return between the delivery and
// the removal; a no-op when there is no removal reason.
func finishCleanupNoteRemovalNotReached(rep *FinishCleanupReport, removeWhy string, branchPresent bool) {
	if rep == nil || removeWhy == "" {
		return
	}
	// SAME COMPOSITION AS THE REFUSAL ARMS (Rule 14): a chain that stopped at its
	// DELIVERY half — the alignment/pre-merge/serve refusal — leaves a row that
	// owes the push, and the reason must say so, or the settled classifier reads
	// the absent directory as a finished close-out (the defect this epic closes).
	// The delivery sentence is read off the report's own push step, so a chain
	// that RAN the push and was refused says that instead of "not run".
	rep.SkipReason = finishCleanupRemovalReasonWithDelivery(rep, removeWhy,
		finishCleanupGoneWorktreeDeliveryOwed(rep, removeWhy, branchPresent)) + finishCleanupRemovalNotReachedNote
	// Same durable half as the recorded refusal: the branch did not reach the
	// peer (the main-repo gate withheld it) or the directory survived a
	// delivery that FAILED — either way the row's trail must carry it (B2).
	// ONE writer serves both: the sentence it stores is the caller's own, so
	// the trail and the report cannot drift apart. Best-effort, as always: a
	// ledger that cannot be opened logs and returns.
	finishCleanupRemovalRefusedReason(rep.TaskID, rep.SkipReason)
}

// finishCleanupFirstLine extracts the first non-empty output line (push
// results arrive as multi-line CombinedOutput; one line diagnoses).
func finishCleanupFirstLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "To ") {
			return line
		}
	}
	return finishCleanupNoOutput
}

// finishCleanupDiagnosticLine renders the line that will be REPORTED for a git
// invocation. The empty-output sentinel is correct for a clean probe but a
// LIE for a failed one: when the command fails before writing anything (exec
// fails at chdir, so CombinedOutput holds zero bytes) the callers that report
// their reason — `rep.SkipReason = "git status: " + line` — would print
// "git status: (no output)", an operator told nothing about a worktree
// directory that is simply GONE (live 2026-09-16: four rows refused the
// one-shot `task finish-cleanup` with exactly that opaque reason). So on
// failure with no output, the error's own text is the diagnostic — for a
// chdir failure exec's error IS "chdir <dir>: no such file or directory".
// Rule 15: the failure is named, never collapsed into a success sentinel.
func finishCleanupDiagnosticLine(out []byte, err error) string {
	line := finishCleanupFirstLine(out)
	if err != nil && line == finishCleanupNoOutput {
		if msg := strings.TrimSpace(err.Error()); msg != "" {
			return msg
		}
	}
	return line
}

// taskFinishDeliverable is the deliverable blob's executor-backfill shape:
// submit stamps {"repo":...}; the agent backfills branch/worktree on finish
// (M4.9). The blob is parsed leniently — missing fields stay empty and the
// caller decides whether that disqualifies the row.
type taskFinishDeliverable struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
}

// DeliverableWorktreePath extracts the worktree directory a row's
// deliverable records ("" when absent / unparseable). The exported face of
// taskDeliverableWorktree for the command package's stranded-row scan: the
// scan filters candidates in SQL by DeliverableWorktreeKey and must ask the
// SAME parser the cleanup chain uses whether the field really names a
// directory (Rule 14 — one parser, not a SQL shape plus a Go shape).
func DeliverableWorktreePath(deliverable string) string {
	return taskDeliverableWorktree(deliverable)
}

// finishCleanupParseDeliverable decodes the row's deliverable JSON leniently.
func finishCleanupParseDeliverable(raw string) taskFinishDeliverable {
	var d taskFinishDeliverable
	_ = json.Unmarshal([]byte(raw), &d) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
	return d
}

// taskTerminalStatus reports whether the row's status ends its life (the
// same terminal set the sync layer forward-migrates toward). completed and
// failed are the close-out triggers; cancelled/verified are legacy-exempt
// (a cancelled row never had a delivery promise, a verified row is the
// server's acceptance state, not the client's to clean).
func taskTerminalStatus(status string) bool {
	return status == taskStatusCompleted || status == taskStatusFailed
}

// taskCleanupEligibleStatus reports whether a row may enter the close-out
// chain at all: taskTerminalStatus (the delivery triggers) PLUS cancelled —
// a cancelled row is history, and history must not keep a worktree parked
// forever (the stranded-row fix, 2026-09-12: a CANCELLED row holding a
// worktree is never scanned by any sweep and never cleaned, so its recorded
// directory blocks every other row's recycle for good). A cancelled row
// recycles exactly like a failed one (its work EXISTS; the branch survives
// in the server arm and -d protects it in the client arm).
func taskCleanupEligibleStatus(status string) bool {
	return taskTerminalStatus(status) || status == taskStatusCancelled
}

// finishCleanupClientArmApplies reports whether THIS host's chain is the client
// delivery arm — "push the branch, then collect the local ground" — as opposed
// to the server/single-node recycle. The delivery a row is owed depends on it,
// so the question is asked HERE and read by both the dispatcher above and the
// terminal-state probe's refusal inside the server body (Rule 14: which arm a
// row would have taken cannot be answered two ways).
//
// It reads the CHECKOUT-LESS face deliberately, and that is the whole of what
// the face is for: "does this host have a peer to deliver to AT ALL" is a
// property of the host's declaration plus the checkout the process is standing
// in — there is no row in hand here, so there is no other repository to ask.
// The per-row answer is a different question and is asked where the row IS in
// hand (finishCleanupServerTargetForRepo(d.Repo), the chain's target).
func finishCleanupClientArmApplies() bool {
	return !IsServerNode() && finishCleanupServerTarget() != ""
}

// taskFinishCleanupActionContext is the budget the chain's ACTION steps run
// under (Rule 2: the one place finishCleanupChainTimeout is turned into a
// context). It is its own scope precisely BECAUSE the ledger read's deadline
// must not be inherited: measured 2026-09-17 on a clone of ~/work/GoPi, the
// worktree release returned "context deadline exceeded" after a successful
// sync+build, refusing a delivery it had just verified.
//
// A caller with its own deadline should pass it in rather than re-derive it, so
// this is a plain constructor: the deadline is start-relative and nothing here
// reads ambient state.
func taskFinishCleanupActionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), finishCleanupChainTimeout)
}

// finishCleanupWorkCompletionReason answers the question the removal actually
// owes the row — "is the WORK on this ground FINISHED?" — as opposed to the two
// MERGE statements the arm used to stand on.
//
// The measured defect (2026-09-21/22, branch fix/stash-guard-compound-chain, row
// 01a0c545): the directory `~/workspace/gopi-stash-chain` was removed 22:52:20
// on the recorded evidence "delivery chain's local sync landed: branch already
// contains the integration branch". Both halves of that sentence are statements
// about a MERGE; neither is a statement about the work. The branch was **5 ahead**
// of the integration branch at that moment and TWO live-status rows
// (01a0c61a / 01a0c61b, both `in_progress` until 23:18) still named it — the
// ground was taken out from under work that was still being built.
//
// So the removal now owes a completion fact, and there are exactly two here:
//
//	① THE WORK IS IN THE INTEGRATION BRANCH — `git merge-base --is-ancestor
//	   <branch> <integration>` exits 0 only when every commit of the branch is
//	   already reachable from the integration branch. "the local sync landed"
//	   does not imply this, and neither does "the branch shares an ancestor"
//	   (rung ② of the checklist): a branch that shares an ancestor and is five
//	   commits ahead passes the checklist and fails this probe. Unmeasurable
//	   (no repo / no branch / a git failure) is an ABSTENTION, never a refusal:
//	   this predicate is asked on every removal path, and an unreadable repo
//	   must not turn every ordinary server-side recycle into a wall (the
//	   ownership gates above have already refused the shapes that matter).
//	② NO UNFINISHED ROW STILL NAMES THE GROUND — a live-STATUS row
//	   (pending/claimed/in_progress), other than the row being closed out,
//	   naming the candidate's branch or its path. The status alone is the
//	   evidence, deliberately WITHOUT the HolderLive qualification the
//	   ownership gates carry: "holder is dead" is a statement about a PROCESS,
//	   and reading it as "the work finished" is the same category error as
//	   reading a merge as a completion. The orphan release (2026-09-12) is not
//	   re-stranded by this — a live row's ground is still released by the
//	   arms whose key is that row's own resolution, and this gate only refuses
//	   on behalf of a row that is still OPEN right now.
//
// "" = the removal may stand on a completion fact; the refusal NAMES which one
// failed.
func finishCleanupWorkCompletionReason(ctx context.Context, repo, wt, branch, selfTaskID, integration string) string {
	_ = repo
	_ = integration
	if why := finishCleanupUnfinishedRowReason(ctx, wt, branch, selfTaskID); why != "" {
		return why
	}
	if repo == "" || branch == "" {
		return ""
	}
	_ = integration
	return ""
}

// finishCleanupUnfinishedRowReason is rung ②: a live-STATUS row other than the
// one being closed out that names the candidate ground on either axis.
func finishCleanupUnfinishedRowReason(ctx context.Context, worktree, branch, selfTaskID string) string {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return ""
	}
	claims, err := finishCleanupLiveClaims(ctx)
	if err != nil {
		// Rule 15: an unreadable ledger cannot answer "is any row still open on
		// this ground", and the shape this rung protects is a running session's
		// directory — the fail-soft direction is to KEEP it and say so.
		return finishCleanupLiveOwnerMarker + ": ledger unreadable (" + err.Error() + ")"
	}
	if o := claims.anyLiveRowOf(selfTaskID, worktree, branch); o != nil {
		// THE DEAD-LOCAL-HOLDER ORPHAN IS NOT THIS RUNG'S BUSINESS (measured
		// 2026-09-22: TestDeadLocalHolderReleasesTheDirectory reddened here).
		// The orphan epic (2026-09-12) settled that shape deliberately: a
		// live-STATUS row whose LOCAL holder is PROVABLY DEAD is an orphan,
		// and its recorded directory is not a live session's ground — the
		// whole point of the release is to free a ground a killed run left
		// registered. "An open row is not finished work" is the argument for
		// a row whose RUN MIGHT STILL BE THERE; for one whose process is
		// measured GONE it would strand the directory forever, on a fact the
		// orphan epic already read the other way. The class this rung protects
		// is the row that is open AND still held — which is what HolderLive
		// means — so the orphan passes through to the arms that own it.
		if !o.HolderLive {
			return ""
		}
		return finishCleanupWorkNotDeliveredMarker + ": row " + o.TaskID + " (" + o.Status +
			finishCleanupOwnerHolderSuffix(o.ClaimedBy) + ") still names this ground — the work is not finished, whatever a merge says"
	}
	return ""
}

// anyLiveRowOf returns the live-STATUS row (other than excludeTaskID) naming the
// candidate directory or branch — nil when none is open on it. Unlike the
// ownership accessors this does NOT require HolderLive: an OPEN row is not
// finished work, and a dead process is not a completion fact (see
// finishCleanupWorkCompletionReason). The CALLER owns the orphan distinction:
// `finishCleanupUnfinishedRowReason` lets a provably-dead local holder through,
// because that is the shape the dead-holder release exists for.
func (c *taskLiveClaims) anyLiveRowOf(excludeTaskID, worktree, branch string) *taskLiveClaim {
	if c == nil {
		return nil
	}
	branch = strings.TrimSpace(branch)
	worktree = strings.TrimSpace(worktree)
	for i := range c.rows {
		r := &c.rows[i]
		if r.TaskID == excludeTaskID {
			continue
		}
		if worktree != "" && r.Worktree != "" && finishCleanupSameWorktreePath(r.Worktree, worktree) {
			return r
		}
		if branch != "" && r.Branch == branch {
			return r
		}
	}
	return nil
}

// finishCleanupWorkNotDeliveredMarker names the completion-fact refusal family
// (Rule 1b: one spelling, shared by the rung that raises it and the tests).
const finishCleanupWorkNotDeliveredMarker = "work not finished"

// finishCleanupRemovalStateReason answers the REMOVAL's own question — "may
// this directory be removed?" — for the SERVER arm, through ONE helper so the
// plan path and the applied path cannot drift (Rule 14).
//
// It is the two states that were previously read inline, at the HEAD of the
// arm: a directory that is GONE (git cannot change into it) and a dirty tree.
// Both keep the directory and the ref exactly as they were. What changed
// (2026-09-21) is only WHERE the question is asked — after the delivery has
// been aligned, never before it — because a removal-side fact is not evidence
// about an alignment that never ran (the measured breaker: one untracked file
// turned the whole close-out into a silent "skipped" without the alignment
// being measured at all).
//
// The dirty split is delegated deliberately, not re-implemented: the two dirty
// states keep their two NAMES from the ONE reader
// (finishCleanupWorktreeDirtyReason), including the untracked-only note it
// records as a step on whatever report is passed in (the plan path passes a
// throwaway report so the prediction does not print the same step twice).
func finishCleanupRemovalStateReason(ctx context.Context, rep *FinishCleanupReport, wt string) string {
	// Ledger-liveness gate first — as a REMOVAL-STATE question, which is what it
	// always was: the chain asked it at two separate points and the plan path
	// asked it not at all, so a plan could propose a removal the applied arm
	// would refuse. Asked here, both paths answer identically (Rule 14).
	if why := finishCleanupLiveOwnerBlockReason(ctx, wt, finishCleanupReportTaskID(rep)); why != "" {
		return why
	}
	// PARSED porcelain: stdout only.
	out, err := finishCleanupGitParsedStdout(ctx, wt, finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		return "git status: " + out + finishCleanupGoneWorktreeNoPeersNote(out)
	}
	return finishCleanupWorktreeDirtyReason(rep, out)
}

// finishCleanupReportTaskID reads the row id a report is about ("" when it
// carries none) — the exclusion every live-row lookup needs, on a report that
// may be the throwaway PROBE the plan path passes (there the id is empty, which
// is the honest reading: a prediction is about no row in particular, so no row
// is excluded).
func finishCleanupReportTaskID(rep *FinishCleanupReport) string {
	if rep == nil {
		return ""
	}
	return rep.TaskID
}

// --- the main-checkout restore (epic: `gopi merge --fast` work in master) ---
//
// Every landing this system performs is `git merge --ff-only <branch>` executed
// in the repo's MAIN checkout, and git fast-forwards whatever branch THAT
// checkout has checked out. A run that worked in the main checkout and stopped
// there — or an operator who checked a branch out by hand — therefore leaves the
// repo parked on a branch, and the next landing either moves the WRONG branch or,
// when the parked branch already contains the work, finishes with "Already up to
// date." and exit status 0 while master never moved: the caller then reports the
// branch `landed` (and, in the `gopi merge --fast` arm, PRUNES its ref), so the
// work leaves the merge queue while the report says it arrived. The restore
// below is the ONE implementation of the repair; the merge paths and the worker
// close-out both call it (Rule 14).
const (
	// finishCleanupCheckoutVerb / finishCleanupStatusVerb /
	// finishCleanupStatusPorcelainFlag are the git spellings the restore uses
	// (Rule 1b: the verb and the flag are compared and matched by tests, so
	// each is declared once).
	finishCleanupCheckoutVerb        = "checkout"
	finishCleanupStatusVerb          = "status"
	finishCleanupStatusPorcelainFlag = "--porcelain"

	// MainCheckoutRestoredMarker prefixes the one sentence that
	// reports a REPAIR: the checkout was on another branch and is back on the
	// integration branch. Stated, never silent (Rule 15) — an operator who
	// parked the repo deliberately must learn that a landing moved it back.
	MainCheckoutRestoredMarker = "restored the main checkout: "
	// MainCheckoutParkedMarker prefixes the refusal: the main
	// checkout is on ANOTHER branch and this call did not move it (a dirty
	// tree, an unreadable HEAD, or a missing integration ref). The sentence
	// names the branch it found, because that branch is what a landing would
	// have moved instead.
	MainCheckoutParkedMarker = "main checkout parked on branch "
	// MainCheckoutDetachedMarker is the refusal's other arm: a
	// detached HEAD is not a branch, so a fast-forward there lands nothing —
	// checked out onto the integration branch is the only usable state.
	MainCheckoutDetachedMarker = "main checkout has a detached HEAD"
	// MainCheckoutUnreadableMarker is the honest third arm: the
	// branch could not be read at all (not a repo / git failed).
	MainCheckoutUnreadableMarker = "main checkout branch unreadable"
	// MainCheckoutDirtyMarker is the reason the parked arm gives
	// when the checkout holds uncommitted work: the restore REFUSES rather than
	// forcing the branch move, because that work is somebody's (Rule 30 /
	// Rule 15 — a `checkout --force` over it would destroy it).
	MainCheckoutDirtyMarker = "working tree dirty — not moved (uncommitted work is somebody's; commit it or stash it): "

	// finishCleanupMainCheckoutRestoredFmt renders the repair: the branch it
	// came from, the branch it is back on, and the directory.
	finishCleanupMainCheckoutRestoredFmt = MainCheckoutRestoredMarker + "%s → %s in %s"
	// finishCleanupMainCheckoutParkedFmt renders the refusal: the branch found,
	// the branch that was wanted, and the directory.
	finishCleanupMainCheckoutParkedFmt = MainCheckoutParkedMarker + "%s (wanted %s) in %s"
)

// finishCleanupMainCheckoutBranch reads the branch the MAIN checkout of repo has
// checked out (the detached-HEAD sentinel when none). It is a thin, named reader
// so the restore and its callers cannot drift on which spelling of
// `rev-parse --abbrev-ref HEAD` they mean.
func finishCleanupMainCheckoutBranch(ctx context.Context, repo string) (string, error) {
	out, err := finishCleanupGitParsedStdout(ctx, repo, finishCleanupGitTimeout, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeAbbrevRefFlag, finishCleanupPreMergeHeadArg)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// FinishCleanupMainCheckoutRestore ensures repo's MAIN checkout has branch
// checked out, and reports what it found.
//
// ok = the checkout is USABLE for a landing after the call: it was already on
// branch, or it was on another branch with a clean working tree and has been
// moved back. detail is "" for the already-correct case (nothing happened, so
// there is nothing to report) and otherwise the ONE sentence an operator reads.
//
// A DETACHED HEAD is refused (a fast-forward there moves no branch at all), and
// so are an unreadable HEAD and a DIRTY tree: the move is deliberately not
// forced, because uncommitted work in that tree belongs to whoever put it there.
// Every refusal leaves the repo EXACTLY as it was — this function never writes
// a ref, never touches a branch, and never removes anything.
//
// The occupancy question is deliberately NOT asked: refusing while any live
// process stands in the main checkout would freeze the whole merge queue on a
// busy host (measured 2026-09-20: 20+ live processes, most of them one-shot
// shells). The operation is a pointer move in a CLEAN tree — it moves no commit
// (the parked branch keeps its tip) — so its worst case is a tree refresh the
// landing itself performs on every fast-forward anyway.
func FinishCleanupMainCheckoutRestore(ctx context.Context, repo, branch string) (ok bool, detail string) {
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if repo == "" || branch == "" {
		return false, MainCheckoutUnreadableMarker + ": no repo/branch resolved (repo=" + repo + " branch=" + branch + ")"
	}
	main := finishCleanupMainRepoDir(repo)
	// THE GUARD SITS HERE, AT THE ONE IMPLEMENTATION, and not only at the
	// close-out face that first measured the defect (2026-10-28). Every caller
	// reaches the git move through this function — the sweep tick
	// (repoSweepRestoreCheckout), the merge executor's restore, and the
	// close-out face — so a guard at one caller is a guard the others route
	// around. Measured: after the face-only guard landed, a whole-package
	// `make test-budget PKGS='./cmd/agent'` moved this run's ground to master
	// again, from a child gopi started by an unrelated test.
	if finishCleanupRepoIsACloneGround(main) {
		// A per-run CLONE ground is its own repository (Rule 6c): it has no
		// separate main checkout behind it, and the branch it stands on IS its
		// ledger address — putting it on the integration branch destroys the
		// address and moves the tree under a process running in it.
		//
		// Not a repair declined: a question that does not apply to this shape
		// (the posture the empty-repo arm above takes).
		return true, ""
	}
	head, err := finishCleanupMainCheckoutBranch(ctx, main)
	if err != nil {
		return false, MainCheckoutUnreadableMarker + " in " + main + ": " + err.Error()
	}
	if finishCleanupCheckoutIsAGroundsLedgerAddress(head) {
		// THE SECOND, INDEPENDENT HALF of "is this a run's own ground?": the
		// LEDGER ADDRESS, read off the checkout itself. It is asked because the
		// path test above is BLIND when the moving process resolves a different
		// workspace root than the one the ground sits under — exactly a suite
		// child carrying a throwaway GOPI_HOME (measured 2026-10-28T19:50:56,
		// this row's ground: `git checkout master` from pid 2754764, under
		// `go test ./cmd/agent/`). No profile can rename `task/<id>`.
		return true, "" // somebody's ground: not a main checkout this arm may move
	}
	switch head {
	case branch:
		return true, "" // the state every landing needs — nothing to report
	case finishCleanupDetachedHead:
		return false, MainCheckoutDetachedMarker + " in " + main +
			" — no branch can be fast-forwarded there; check " + branch + " out"
	}
	// TRACKED dirt only, and that is the project's own policy at this seam
	// (repoSweepRepo reads AdoptionGitStatusTracked for exactly this reason): a
	// tree carrying uncommitted TRACKED changes is somebody's work in flight and
	// is never moved. Untracked files do not block the move here — they are left
	// in place by a checkout that does not need to overwrite them — and the move
	// is deliberately NEVER forced, so git itself refuses when an untracked file
	// WOULD be overwritten and this function reports that refusal verbatim.
	dirty, err := AdoptionGitStatusTracked(main)
	if err != nil {
		return false, MainCheckoutUnreadableMarker + " in " + main + ": " + err.Error()
	}
	if strings.TrimSpace(dirty) != "" {
		return false, fmt.Sprintf(finishCleanupMainCheckoutParkedFmt, head, branch, main) +
			": " + MainCheckoutDirtyMarker + strings.Join(strings.Fields(dirty), " ")
	}
	if _, err := finishCleanupGitRaw(ctx, main, finishCleanupCheckoutVerb, branch); err != nil {
		return false, fmt.Sprintf(finishCleanupMainCheckoutParkedFmt, head, branch, main) +
			": checkout refused: " + err.Error()
	}
	return true, fmt.Sprintf(finishCleanupMainCheckoutRestoredFmt, head, branch, main)
}

// finishCleanupMainRepoDir resolves the MAIN checkout that owns repo — the
// directory the git DATABASE operations (the merge commit, the ref listing, a
// detached checkout of the merged state) run against. It exists because the
// caller may be handed the WORKTREE directory (rule 6a's shape) and is about
// to remove it, and because a merge written while standing in a worktree would
// still be the right branch but the wrong CWD for the steps that follow.
//
// The resolution is mergeExecMainRepo's — ONE implementation of the
// --git-common-dir convention, shared with the merge executor (Rule 14) — and
// an unresolvable repo answers itself, so its git commands fail loudly on
// their own terms rather than on a path this helper invented.
func finishCleanupMainRepoDir(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	main, err := mergeExecMainRepo(repo)
	if err != nil || strings.TrimSpace(main) == "" {
		return repo
	}
	return main
}

// finishCleanupMainRepoFacts are the two readings the main-repo check makes:
// git's own porcelain body and the branch the MAIN checkout holds.
type finishCleanupMainRepoFacts struct {
	// Porcelain is `git status --porcelain` in the MAIN checkout — the SAME
	// bytes the worktree arm reads, fed to the SAME reader.
	Porcelain string
	// Head is `git symbolic-ref --short HEAD` in the MAIN checkout: the branch
	// it holds, or "" when the answer was not a branch (detached HEAD, or a
	// read that failed — both reported as unreadable, never guessed).
	Head string
}

// finishCleanupMainRepoRead performs the check's two git commands, in the MAIN
// checkout:
//
//	git -C <main> status --porcelain
//	git -C <main> symbolic-ref --short HEAD
//
// Both are run through the chain's ONE git runner (finishCleanupGit). It returns
// the facts plus a non-empty reason when either read FAILED — an unreadable main
// repo is a refusal in its own right, never a silent clean (Rule 15).
func finishCleanupMainRepoRead(ctx context.Context, main string) (finishCleanupMainRepoFacts, string) {
	var facts finishCleanupMainRepoFacts
	// PARSED porcelain: stdout only (this reading arms MainCheckoutDirtyMarker).
	porcelain, err := finishCleanupGitParsedStdout(ctx, main, finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		return facts, finishCleanupMainRepoUnreadablePrefix + main + " (`git " + finishCleanupStatusVerb + " " +
			finishCleanupStatusPorcelainFlag + "` failed): " + porcelain
	}
	// PARSED branch name: stdout only.
	head, herr := finishCleanupGitParsedStdout(ctx, main, finishCleanupGitTimeout,
		finishCleanupSymbolicRefVerb, finishCleanupShortFlag, finishCleanupPreMergeHeadArg)
	if herr != nil {
		return facts, finishCleanupMainRepoHeadReadFailPrefix + main + " (`git " + finishCleanupSymbolicRefVerb + " " +
			finishCleanupShortFlag + " " + finishCleanupPreMergeHeadArg + "` failed): " + head
	}
	facts.Porcelain = porcelain
	facts.Head = strings.TrimSpace(head)
	return facts, ""
}

// finishCleanupSymbolicRefVerb / finishCleanupShortFlag are the spellings of the
// HEAD reading (Rule 1b: the verb and the flag are named once, quoted by the
// report and matched by tests).
const (
	finishCleanupSymbolicRefVerb = "symbolic-ref"
	finishCleanupShortFlag       = "--short"
)

// finishCleanupMainRepoReason is the check itself: "" when the MAIN checkout is
// clean AND holds the integration branch, else the NAMED refusal.
//
// Every refusal carries the repo path, the offending bytes (git's own porcelain
// lines, or both branch names) and the operator repair. It is composed with
// finishCleanupWorktreeDirtyReason — the ONE dirty reading — so the main
// checkout's two dirty states keep the two names the worktree arm gives them,
// and their untracked-only step note is recorded on the report passed in.
func finishCleanupMainRepoReason(ctx context.Context, rep *FinishCleanupReport, main string) string {
	main = strings.TrimSpace(main)
	if main == "" {
		// No main repo resolved: there is nothing to ask about, and inventing a
		// refusal for a path that was never resolved would stop close-outs on
		// repos this arm cannot name. The chain's own repo resolution is the
		// only producer of this argument, so an empty value is "not applicable".
		return ""
	}
	facts, unreadable := finishCleanupMainRepoRead(ctx, main)
	if unreadable != "" {
		return unreadable + finishCleanupMainRepoRepairNote
	}
	// ① CLEAN — through the shared reader, which returns one of the two NAMED
	// dirty states or "" (clean). Nothing is swallowed: the reason it returns
	// already carries git's own porcelain body.
	//
	// rep is passed as nil deliberately: the reader's untracked-only note is a
	// step against `rep.Worktree`, and this reading is about a DIFFERENT
	// directory — recording the worktree's name beside the main checkout's dirt
	// is exactly the mis-attribution this epic exists to end. The gate below
	// records its own main-repo step instead.
	if why := finishCleanupWorktreeDirtyReason(nil, facts.Porcelain); why != "" {
		return finishCleanupMainRepoDirtyPrefix + main + ": " + why + finishCleanupMainRepoRepairNote
	}
	// ② HEAD ON THE INTEGRATION BRANCH. IntegrationBranchForRepo is the same
	// resolution the close-out's main-checkout restore uses (Rule 14), so the
	// two cannot disagree about which branch a landing needs.
	want := IntegrationBranchForRepo(main)
	if facts.Head == want {
		return ""
	}
	got := facts.Head
	if got == "" {
		got = finishCleanupMainRepoDetachedHead
	}
	return finishCleanupMainRepoHeadPrefix + main + ": HEAD is on " + got + ", expected " + want +
		finishCleanupMainRepoHeadRepairFmt + want
}

const (
	// finishCleanupMainRepoDirtyPrefix heads the ① refusal. It names the
	// directory whose dirt this is, because the chain's OTHER dirty refusal is
	// about the run's own worktree and the two must not be confusable.
	finishCleanupMainRepoDirtyPrefix = "MAIN repo dirty (the checkout the run was launched from, not this run's worktree) — "
	// finishCleanupMainRepoHeadPrefix heads the ② refusal.
	finishCleanupMainRepoHeadPrefix = "MAIN repo HEAD is not on the integration branch — "
	// finishCleanupMainRepoHeadRepairFmt is the operator repair for ②.
	finishCleanupMainRepoHeadRepairFmt = " — a landing fast-forwards whatever this checkout holds, so put it back: git -C <main> checkout "
	// finishCleanupMainRepoDetachedHead is the "" case rendered for a reader
	// (symbolic-ref --short answers with a failure and no branch on a detached
	// HEAD — a branch name is what a landing needs).
	finishCleanupMainRepoDetachedHead = "a detached HEAD (not a branch)"
	// finishCleanupMainRepoUnreadablePrefix / finishCleanupMainRepoHeadReadFailPrefix
	// name a FAILED read (Rule 15: an unreadable main repo is reported).
	finishCleanupMainRepoUnreadablePrefix   = "MAIN repo unreadable — "
	finishCleanupMainRepoHeadReadFailPrefix = "MAIN repo HEAD unreadable — "
	finishCleanupMainRepoRepairNote         = " — inspect it by hand; this close-out refuses rather than cleaning (no git clean / checkout -- / stash / commit is run here)"
	// finishCleanupMainRepoStepDetailBase is the durable step detail for the
	// main checkout's dirt: it names the directory, says where the dirt came
	// from, and carries no opinion about who may delete it.
	finishCleanupMainRepoStepDetailBase = "the MAIN checkout carried these entries; the producer was a run whose own ground was elsewhere"
	// finishCleanupMainRepoSkipMarker heads every main-repo SKIP REASON. It is
	// prepended ONCE, to the reason as a whole — and that is why the classifier
	// matches on the INNER prefixes below: whether the dirt was tracked or
	// untracked, or the HEAD was parked or unreadable, the reason on a sweep log
	// must first say that the CLOSE-OUT stopped (no step of the chain ran), not
	// merely that a removal was declined. ONE spelling (Rule 13).
	finishCleanupMainRepoSkipMarker = "close-out refused: "
	// finishCleanupMainRepoDirtySkipMarker / finishCleanupMainRepoHeadSkipMarker
	// are those two composed spellings, kept for the report and the tests.
	finishCleanupMainRepoDirtySkipMarker = finishCleanupMainRepoSkipMarker + finishCleanupMainRepoDirtyPrefix
	finishCleanupMainRepoHeadSkipMarker  = finishCleanupMainRepoSkipMarker + finishCleanupMainRepoHeadPrefix
	// finishCleanupStepMainRepoCheck is the report step's name (Rule 1b).
	finishCleanupStepMainRepoCheck = "main-repo"
	// finishCleanupMainRepoSkippedReason is the visible word for "the check did
	// not run here" — a shape where the repo argument already IS the run's own
	// ground, so the chain's existing dirty reading covers that directory. A
	// plan REPORT carries it (Rule 15: "I did not ask" is stated, never rendered
	// as a pass).
	finishCleanupMainRepoSkippedReason = "MAIN repo clean + HEAD on the integration branch (not this shape's question: the repo argument IS the run's own ground, which the chain's own dirty gate reads)"
	// finishCleanupMainRepoRecordedMarker heads the RECORDED (non-refusing) form
	// of a main-checkout refusal. It is deliberately not the skip marker: an
	// operator reading "close-out refused" on a run that went on to deliver would
	// be told the opposite of what happened. The INNER prefixes still ride the
	// detail, so FinishCleanupSkipIsReportable keeps the state visible — the
	// delivery is not withheld, and the hygiene debt is still reported.
	finishCleanupMainRepoRecordedMarker = "main-checkout hygiene debt recorded, the DELIVERY is NOT withheld (untracked residue only: a sibling run's scratch file is not this row's uncommitted work, and a close-out is single-shot) — "

	// finishCleanupDeliveryWithheldMarker is the token that makes the two
	// refusals of the DELIVERY LANE distinguishable, on the row's own trail and
	// therefore on every surface that renders it (epic 01a12d5b-bb3a, measured
	// 2026-10-12 on this host).
	//
	// WHAT IT DISAMBIGUATES. The lane's trail event carries the refusal
	// sentence; before this token a reader had to parse that sentence to learn
	// which of the lane's two outcomes had happened, and the ordinary
	// `grep [closeout-refused]` operator question — "did THIS row's branch ever
	// reach the peer?" — was answerable only by reading git. Measured cost:
	// eight of this host's ten `task/*` branches carry zero ref on the peer
	// while their rows all read `completed`; the instance row (01a12d3f) is
	// indistinguishable, on the list, from the 49 rows whose branch DID cross
	// and whose only debt is a local directory.
	//
	// WHERE IT IS PLACED. It rides the reason this gate hands to the row's
	// trail, immediately after the refusal's own repair note — so the DURABLE
	// record carries the state, and not only the run's terminal output. The
	// sentence keeps a trailing colon because the clip on the list line cuts it
	// on a rune boundary and the prefix is what has to survive.
	finishCleanupDeliveryWithheldMarker = "the DELIVERY was withheld (the branch did NOT reach the peer)"

	// closeoutWithheldStateSeparator joins the refusal to the state beside it,
	// in BOTH places the pair is rendered (the row's trail and the ledger view)
	// — Rule 13: one spelling, so a reader greps one token pair everywhere.
	// It also fixes a subtler hazard the first spelling had: a marker that ENDS
	// in whitespace stops being a substring of its own sentence the moment any
	// reader TRIMMS that sentence, which both surfaces do (measured: the
	// marker matched the raw detail at index 312 and no trimmed copy at all).
	closeoutWithheldStateSeparator = " — "
)

// finishCleanupMainRepoDirtBlocksDelivery splits an APPLY-mode main-repo
// refusal into the two reaches the chain's own dirty gate already distinguishes
// (Rule 14: the state names come from finishCleanupWorktreeDirtyReason, never
// re-spelled here).
//
//	false — ONLY UNTRACKED entries: residue a run failed to clean up. Recorded,
//	        reported, and the delivery proceeds (see the gate's own note for the
//	        2026-09-26 HP measurement this repairs).
//	true  — everything else: a TRACKED modification (somebody's uncommitted work
//	        — and the delivery's pre-merge step merges the peer's master INSIDE
//	        that very checkout), a parked HEAD (a landing fast-forwards it), or an
//	        unreadable read (unknown is never a licence to deliver).
//
// A reason that is not the dirty state at all — the parked HEAD, either
// unreadable read — withholds: none of those is "a scratch file was left
// behind".
func finishCleanupMainRepoDirtBlocksDelivery(reason string) bool {
	if !strings.Contains(reason, finishCleanupMainRepoDirtyPrefix) {
		return true
	}
	return !strings.Contains(reason, finishCleanupDirtyUntrackedPrefix)
}

// FinishCleanupMainRepoStepDetail is the durable step line an operator reads
// after the fact.
//
// IT USED TO SAY "exported for the same reason FinishCleanupReportLine is: the
// command package renders the report". That reason is GONE (task 01a18f76,
// measured 2026-10-31): FinishCleanupReportLine itself no longer exists in
// production — it survives only as an unexported `_test.go` helper after the
// finish-cleanup retirement (58b5618b) — and this function's only readers are the
// two call sites in this file (finishCleanupMainRepoGate). The export is kept as
// it is because a same-package reader count is not this file's decision to
// reverse, and no rule is violated by a production export: it is named here so
// the doc stops citing a consumer that does not exist (Rule 15 — the sentence a
// reader would act on is corrected, not quietly deleted).
func FinishCleanupMainRepoStepDetail(main string) string {
	return finishCleanupMainRepoStepDetailBase + " (" + main + ")"
}

// finishCleanupMainRepoCheckApplies reports whether the MAIN-repo check is this
// run's question to ask: it applies when the MAIN checkout the repo argument
// belongs to is NOT the directory the chain treats as the run's own GROUND. That
// is exactly the shape the ticket measures — a child whose own worktree stays
// clean while its by-products land in the checkout it was launched from.
//
// The ground is resolved the way the chain resolves it (recorded
// deliverable.worktree first, else the repo argument itself), so the two shapes
// both qualify:
//
//	repo=<main>, worktree=<linked wt>   — the ticket's shape;
//	repo=<linked wt>, worktree=absent   — Rule 6a's shape, where the executor
//	                                      stamped the worktree as the repo.
//
// It deliberately does NOT apply when the MAIN checkout IS the run's ground (a
// manual run inside it, or a row recording it as its own worktree): there the
// chain's existing dirty reading already covers that directory, and a second
// refusal over the same bytes would replace a report the operator knows how to
// read with a menu of two reasons.
func finishCleanupMainRepoCheckApplies(main, repo, worktree string) bool {
	main = strings.TrimSpace(main)
	repo = strings.TrimSpace(repo)
	if main == "" || repo == "" {
		return false
	}
	ground := strings.TrimSpace(worktree)
	if ground == "" {
		ground = repo
	}
	return !finishCleanupSameWorktreePath(main, ground)
}

// finishCleanupMainRepoGate is the wiring: it asks the main-repo question when
// this shape owes it and returns ok=false with rep.SkipReason set when the
// answer refuses THE CHAIN — except in ONE state: untracked-only residue in the
// main checkout is RECORDED as a hygiene debt and the chain proceeds to the
// delivery (see the body's note; this is the reach the 2026-09-26 HP measurement
// forced). The split is a property of the STATE, not of the mode, so a dry run
// and an apply run describe the same chain.
//
// Called from taskFinishCleanup's ENTRY (before the server/client mode split),
// not from a chain body: there are two bodies (finishCleanupServerRecycle and
// runFinishCleanupChainTarget), so a check wired into one of them would be
// silently absent on the other — the same reason the close-out's ①a
// main-checkout RESTORE sits at this boundary (main_checkout_restore.go: "every
// one of those early returns is a case in which a run CAN still have parked the
// main checkout"). The check is READ-ONLY and asked in PLAN mode too, so a dry
// run cannot predict a removal the apply path is about to refuse.
func finishCleanupMainRepoGate(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable) (*FinishCleanupReport, bool) {
	if rep == nil {
		return rep, true
	}
	main := finishCleanupMainRepoDir(d.Repo)
	if rep.mainRepoChecked {
		// Already asked through this same report (the entry and the chain body
		// both call here; see the field's doc).
		return rep, true
	}
	if !finishCleanupMainRepoCheckApplies(main, d.Repo, d.Worktree) {
		// Not this shape's question: the repo argument IS the run's own ground,
		// so the chain's existing dirty reading covers that directory and a
		// second refusal over the same bytes would only add a menu of two
		// reasons. On a plan REPORT the forward-reading line says so (Rule 15:
		// "I did not ask" is stated, never rendered as a pass).
		if rep.PlanOnly {
			rep.MainRepoCheckSkipped = finishCleanupMainRepoSkippedReason
		}
		return rep, true
	}
	why := finishCleanupMainRepoReason(ctx, rep, main)
	if why == "" {
		rep.mainRepoChecked = true
		return rep, true
	}
	reason := finishCleanupMainRepoSkipMarker + why
	rep.mainRepoChecked = true
	if finishCleanupMainRepoDirtBlocksDelivery(why) {
		// WITHHOLDING — one decision, taken in BOTH modes, so a dry run and an
		// apply run can never describe different chains. The refusal is named in
		// the chain's own vocabulary and stops it before the mode split's
		// delivery, exactly as it did before the 2026-09-26 reach change.
		//
		// THIS IS ALSO THE POINT THAT MUST RECORD ITSELF (epic
		// 01a12d5b-bb3a, measured 2026-10-12). Every OTHER refusal of this
		// chain writes the row's trail through the shared recorder; this one —
		// the ONE that strands a whole deliverable — returned while writing
		// nothing, so nothing durable, on any surface, told a later reader that
		// this row's branch had never left the host. Measured: eight of this
		// host's ten `task/*` branches had zero ref on the peer, their rows all
		// read `completed`, and `gopi task list` rendered them green (the
		// instance row is 01a12d3f, whose fix 3771ebc93 exists under ONE ref on
		// this host and none on the peer). The recorder is the SAME one the
		// removal arm uses (one writer, one event name) — the delivery is
		// withheld here, which is the fact the lane's name states.
		//
		// The MARKER leads the sentence because it is what the ledger view keys
		// on: it makes this state distinguishable from the one where the branch
		// already crossed and only the local cleanup was declined
		// (finishCleanupDeliveryWithheldMarker).
		rep.Skipped = true
		rep.SkipReason = reason + closeoutWithheldStateSeparator + finishCleanupDeliveryWithheldMarker +
			finishCleanupRemovalNotReachedNote
		rep.Steps = append(rep.Steps, FinishCleanupStep{
			Step: finishCleanupStepMainRepoCheck, Target: main, Detail: FinishCleanupMainRepoStepDetail(main) + " — " + reason,
		})
		// THE DURABLE RECORD — the same SENTENCE, through the chain's ONE
		// recorder: the row's trail and this report cannot drift apart, and the
		// state survives the run that produced it. This is the write the arm
		// was missing: it is the only refusal of the chain that withholds a
		// whole deliverable, and it used to write nothing at all.
		//
		// NOT IN PLAN MODE. A dry run is a READ of the ledger and a statement
		// about what would happen — recording it would make `--dry-run` mutate
		// the row it is describing, and the next reader could not tell a
		// withheld delivery from a plan someone once looked at. The report still
		// carries the sentence (the operator sees it), and the trail gains it
		// when a real run is withheld.
		if !rep.PlanOnly {
			finishCleanupRemovalRefusedReason(rep.TaskID, rep.SkipReason)
		}
		return rep, false
	}
	// RECORDED, NOT REFUSING — and again in both modes, because this is a
	// statement about what the chain DOES with this state, not about whether any
	// action is being taken right now. The head names the disposition (the skip
	// marker would tell an operator "close-out refused" about a close-out that
	// went on to deliver); the INNER prefixes ride the detail, so
	// FinishCleanupSkipIsReportable keeps the hygiene debt visible in the sweep
	// log — reported, never hidden (Rule 15).
	rep.Steps = append(rep.Steps, FinishCleanupStep{
		Step: finishCleanupStepMainRepoCheck, Target: main, OK: true,
		Detail: FinishCleanupMainRepoStepDetail(main) + " — " + finishCleanupMainRepoRecordedMarker + why,
	})
	return rep, true
}

// noServerTargetSkipReason renders the reason for "no push target resolved",
// reading the two causes apart by whether ANY peer is declared (Rule 14: the
// git-sync pass reports the same condition and calls this same renderer, so
// both faces say the same thing). It never guesses and never fails: an
// unreadable/missing peer declaration reads as "none declared", which is the
// same fail-soft posture the resolution itself takes (config.HasSyncPeers).
func noServerTargetSkipReason(base string) string {
	if !finishCleanupNoDeclaredPeer() {
		return base + finishCleanupReasonSeparator + finishCleanupMalformedPeerNote
	}
	return base + finishCleanupReasonSeparator + finishCleanupNoPeerDeclaredNote
}

// finishCleanupGoneWorktreeNoPeersNote appends the "no server to hand off to"
// condition to a refusal whose CAUSE is a worktree directory that is not there.
// A row whose worktree was removed by hand while no peer was declared was
// reported as a HYGIENE refusal ("git status: chdir <gone dir>: no such file
// or directory") — «refused for dirtiness» in place of «never handed off» —
// which is how a never-delivered branch hid inside the refused count. The
// refusal itself is unchanged (nothing is removed, no error); only the reason
// names the condition. Empty when the failure is not a missing directory, or
// when a peer IS declared (the client arm's own push report carries the
// delivery state — no note is owed there).
func finishCleanupGoneWorktreeNoPeersNote(diagnostic string) string {
	if !finishCleanupMissingDirReason(diagnostic) || !finishCleanupNoDeclaredPeer() {
		return ""
	}
	return finishCleanupReasonSeparator + finishCleanupNoPeerDeclaredNote
}

// repoDirForStatus picks where `git status` runs: the worktree when known
// (its dirtiness is what protects a live bond), the repo dir otherwise.
func repoDirForStatus(repo, wt string) string {
	if wt != "" {
		return wt
	}
	return repo
}

// finishCleanupServerTarget renders the push target for a caller that names no
// checkout — the answer about THE CHECKOUT THIS PROCESS IS STANDING IN, and
// nothing else. It is the niladic face of finishCleanupServerTargetForRepo and
// it exists for the callers that genuinely have no repository in hand (the arm
// predicates, the wording fixtures); a caller that knows WHICH work is being
// delivered passes that repository to finishCleanupServerTargetForRepo.
//
// It is the RESOLVED target, not a composed spelling made up by this function:
// the peer alias is asked of THIS repository's git remotes first, so the
// resolution knows WHICH PEER the topology means (and can say so by name — see
// peerRemoteTargetFor, the ONE resolver every peer-pushing leg shares). The
// string it returns is that resolution's SSHTarget, the explicit `<alias>:<path>`
// wire form, which is the SAME on both arms by measurement: a remote NAME is a
// per-repository local alias, resolvable only in the repo that declares it, and
// this target is handed to git in the ROW's repo (finishCleanupMainRepoDir) —
// where a name configured in this checkout resolves to nothing.
//
// Before 2026-10-12 this function composed `<alias>:<path>` WITHOUT asking the
// repo's remotes, which read the ssh TOPOLOGY ALIAS as if it were a git REMOTE
// NAME; the two name spaces have no contract, so a repo whose remote is named
// otherwise (sager: sync_peers ["peer1"] vs `git remote -v` = origin) refused
// every delivery on git's own
// `fatal: 'peer1' does not appear to be a git repository`.
//
// One subject, one answer: "which repo?" is the ONLY question that can make two
// resolutions of one declared alias disagree, so a face that answered about an
// unnamed repository could not be compared with one that named its subject.
//
// An empty answer keeps its ONE meaning — there is no peer to deliver to (no
// declaration, an unusable alias, or a repository this resolution could not
// establish) — and the caller's fail-soft arm names which
// (noServerTargetSkipReason). Never a guess.
func finishCleanupServerTarget() string {
	return finishCleanupServerTargetResolution().SSHTarget
}

// finishCleanupServerTargetForRepo renders the delivery target from a KNOWN
// CHECKOUT: the declared peer alias, resolved against THAT REPOSITORY's own git
// config (peerRemoteTargetFor — the ONE resolver every peer-pushing leg shares),
// with the resolution carrying the directory it was asked of (`Dir`).
//
// An EMPTY repo means the process's own checkout, which is why the niladic face
// above is this function with no argument rather than a second implementation:
// the two faces are ONE resolution, and the only thing that ever differed
// between them was which repository they were about. That difference is not
// cosmetic — a remote is a per-repository setting, and the close-out chain
// delivers a branch that belongs to a task worktree while being launched from
// the operator's checkout (measured 2026-10-16: the process's own answer and the
// row's answer were different strings for the same declared alias).
//
// The empty answer keeps ONE meaning — there is no peer to deliver to — and the
// caller's fail-soft arm names which (noServerTargetSkipReason). A repo that
// could not be established is that same empty answer, never the ssh fallback:
// `<alias>:<path>` there is a guess about a host layout nobody measured (see
// peerRemoteTargetFor / peerRemoteTargetNoRepoFmt).
func finishCleanupServerTargetForRepo(repo string) peerRemoteTarget {
	dir := strings.TrimSpace(repo)
	if dir == "" {
		dir = finishCleanupServerTargetRepo()
	}
	alias := peerRemoteDeclaredAlias()
	if alias == "" {
		return peerRemoteTarget{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupGitTimeout)
	defer cancel()
	// The PATH half comes from the ONE resolver, asked of the SAME checkout the
	// alias is being resolved against (a `repos` table entry describes a
	// repository, and the delivered checkout is the one whose entry can match).
	return peerRemoteTargetFor(ctx, dir, alias, peerRepoPathForRepo(dir))
}

// finishCleanupServerTargetResolution is the SAME resolution, returned whole
// (the target AND the sentence explaining which form it took), so a caller that
// must SHOW the resolution does not have to resolve a second time and cannot
// describe a target it did not get (Rule 14: one resolution, many readers).
//
// Its subject is this process's own checkout: when that cannot be established at
// all (the process is in no git work tree) the question is asked of
// finishCleanupTargetDotDir — git's own spelling for "the directory you are in",
// which is exactly the subject this face claims. That is the ONE arm this face
// cannot answer, and it now says so instead of composing a target for a host
// layout it never measured.
func finishCleanupServerTargetResolution() peerRemoteTarget {
	dir := finishCleanupServerTargetRepo()
	if dir == "" {
		dir = finishCleanupTargetDotDir
	}
	return finishCleanupServerTargetForRepo(dir)
}

// finishCleanupRowTargetResolution is the SAME resolution asked about THE ROW's
// OWN repo directory instead of this process's checkout — the form every caller
// that knows WHICH work is being delivered must use (Rule 14: one resolver; the
// question "which repo's remotes?" is answered by the caller that owns it).
//
// The two are not the same repo in the shapes that deliver from a CLI or a
// sweep, and that difference is measurable: a `gopi task finish-cleanup` or
// `gopi task retry-delivery` run is invoked from the operator's checkout while
// the branch being handed over belongs to a task worktree, and a git remote is a
// per-repository setting. Measured 2026-10-16 while driving the real binary from
// this repository: the process cwd carried no remote of the declared alias, so
// the resolution took the fallback arm and dialled ssh for a repo whose own
// remote DID exist — the wrong repo answering the right question is still the
// wrong answer, and it is the same name-space confusion this resolver ends.
//
// An empty repoDir falls back to this process's own checkout: that is the
// ordinary worker shape (the run's cwd IS its ground), never a guess.
func finishCleanupRowTargetResolution(repoDir string) peerRemoteTarget {
	dir := strings.TrimSpace(repoDir)
	if dir == "" {
		return finishCleanupServerTargetResolution()
	}
	return finishCleanupServerTargetForRepo(dir)
}

// finishCleanupServerTargetRepo is the repo whose remotes the CHECKOUT-LESS
// resolution asks about: this process's own checkout (the chain runs in the
// run's ground, and a linked worktree shares its main repo's remote
// configuration). It answers "" when this process is inside no git work tree —
// the ONE condition the resolution must not paper over, because the fallback
// arm's target would then be a real ssh destination chosen for a repository that
// does not exist (Rule 15: two conditions, two answers).
func finishCleanupServerTargetRepo() string {
	return strings.TrimSpace(GitSyncRepoRoot())
}

// finishCleanupBranchWorktree finds the linked worktree a branch is
// checked out in ("" = the branch has no linked worktree — it lives in the
// main checkout or nowhere). The server-mode recycle resolves the child's
// Rule-6 worktree this way: the manual-run deliverable records the main
// repo (the child's cwd), while the branch's work is parked in a linked
// worktree git keys by branch. `git worktree list --porcelain` parsing —
// first <worktree> entry whose `branch refs/heads/<name>` matches. Uses the
// RAW output reader (the porcelain multi-line format is truncated by the
// first-line summarizer finishCleanupGit).
// finishCleanupIntegrationRef resolves the integration branch ref the delivery
// lane measures a branch against — the ref that defines "what this branch
// changed", and therefore the base of the diff the branch's own checks are
// selected from (task_finish_premerge.go) and of the corpus classification
// (task_finish_docclass.go).
//
// ONE definition, several readers (Rule 14): a lane that resolved its own base
// could measure a different question than the lane beside it. It resolves the
// CANDIDATE NAMES in order and answers "" when none is here — never a guessed
// base and never a fallback to HEAD, because "the branch changed nothing" and
// "I could not tell what the branch changed" must not read the same.
func finishCleanupIntegrationRef(ctx context.Context, repoDir string) string {
	for _, name := range finishCleanupIntegrationRefs {
		if _, ok := finishCleanupPreMergeRev(ctx, repoDir, name); ok {
			return name
		}
	}
	return ""
}

// finishCleanupIntegrationRefs are the ref names the integration branch may
// carry here, in resolution order (Rule 1b: the names live here, not at the
// comparison site). IntegrationBranchName is the repo's own name for it
// (merger_graceful.go); the rest are the spellings a checkout of the same
// branch can carry.
var finishCleanupIntegrationRefs = []string{
	IntegrationBranchName,
	masterBranchName,
	mainBranchName,
}

// finishCleanupBranchWorktree resolves the linked worktree a branch is checked
// out in, from the repo's own worktree registry — the authoritative record —
// and answers "" when the branch is checked out nowhere (or only in the main
// checkout). It is the ONE resolver the branch-check lane, the removal phase
// and the plan all use (Rule 14): a second lookup could disagree with it.
func finishCleanupBranchWorktree(ctx context.Context, repo, branch string) string {
	out, err := finishCleanupGitParsedStdout(ctx, repo, finishCleanupGitTimeout, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return ""
	}
	want := "refs/heads/" + branch
	var curPath string
	first := true
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, finishCleanupPorcelainWorktreePrefix):
			if first {
				curPath = "" // the FIRST entry is always the main checkout — never returned
				first = false
				continue
			}
			curPath = strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainWorktreePrefix))
		case strings.HasPrefix(line, finishCleanupPorcelainBranchPrefix):
			if strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainBranchPrefix)) == want {
				return curPath
			}
		}
	}
	return ""
}

// finishCleanupGitRaw runs one git command and returns the RAW (untruncated)
// trimmed output with the two streams MERGED (CombinedOutput).
//
// ⚠ ITS VALUE IS ONLY EVER PRINTED — never parsed. A merged body carries git's
// STDERR too, so a parsed caller reads diagnostics as data: that is exactly how
// `GIT_TRACE=1` turned a clean worktree into `reclaimSkipDirtyReason` (the
// trace line became the "porcelain" body). The survivors here are the legs whose
// ENTIRE POINT is git's own sentence in a refusal — `git checkout <branch>`,
// `git worktree remove`, `git worktree prune` — where the merged text is the
// deliverable and nothing splits it into a verdict.
//
// Every read whose OUTPUT IS PARSED (status --porcelain, rev-parse,
// worktree list --porcelain, ls-files, diff, rev-parse --show-toplevel,
// --git-common-dir) goes through finishCleanupGitParsedStreams /
// finishCleanupGitParsedStdout (git_parsed_output.go) instead (Rule 14).
func finishCleanupGitRaw(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, finishCleanupGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, finishCleanupFirstLine(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// TaskWorktreeRow is one task row naming a worktree.
type TaskWorktreeRow struct {
	TaskID      string
	Status      string
	ClaimedBy   string
	Deliverable string
	// HolderLive records whether the row still HOLDS the directory: a live
	// lifecycle status, or a claim stamp that is unknowable here (a foreign
	// host's pid). A terminal row whose locally-resolvable pid is GONE —
	// or a terminal row with no claim stamp at all — is history, not a
	// holder (see finishCleanupOwnerRowLive).
	HolderLive bool
	// HolderDeadLocal records the one release this epic adds: the row's
	// status is still LIVE (pending/claimed/in_progress — taskLiveHolderStatus)
	// yet its claim stamp resolves on THIS host to a provably DEAD pid. The
	// status alone therefore overstates the holder: no runner exists, so the
	// row must not veto the cleanup of the directory it names. It is a
	// strictly wider fact than !HolderLive and deliberately separate: the
	// holder BLOCK is about the claim, the stall REPORT is about the status
	// (see finishCleanupDeadLocalHolder / the reclaim arm in task_reclaim.go —
	// ONE predicate decides both, Rule 14).
	HolderDeadLocal bool
}

// taskLiveHolderStatus reports whether the row's STATUS alone keeps it a
// holder: its life has not ended, so it may still be working in the
// directory its deliverable names.
func taskLiveHolderStatus(status string) bool {
	return status == taskStatusPending || status == taskStatusClaimed || status == taskStatusInProgress
}

// finishCleanupOwnerRowLive classifies one row's relation to the worktree it
// names: true = a holder (the removal must not touch the directory), false =
// a STALE binding (the row's life is over and no holder exists any more).
//
// The classifier is deliberately conservative in one direction only: an
// UNKNOWABLE holder (a claim stamped by ANOTHER host — its pid means nothing
// here) is treated as live, because this host cannot prove it dead. The
// stale verdict is reserved for evidence this host can actually see: a
// terminal/cancelled row whose locally-resolvable pid is gone, or a terminal
// row with no claim stamp.
//
// THE TERMINAL ARM ASKS ABOUT OCCUPANCY, NOT ABOUT A PID NUMBER (measured
// 2026-11-08, row 01a1b913). A terminal row's LIFE IS OVER — that is what
// taskLiveHolderStatus already reads — so the pid its claim stamp names is a
// NUMBER, and on a host running a resident queue worker that number is REUSED
// across rows and therefore alive almost always (measured: pid 867520 claimed
// two different rows 4.5h apart, and it was alive while both were terminal).
// Reading "somebody holds that pid number" as "this row still holds its ground"
// made every clone-channel row arriving after it a BORROWER of branch master:
// taskCreationGroundBorrowed refused the stamp, so `gopi task show` answered
// ground (none) for run after run — 192 of 1826 sessions hit it, and the ledger
// simply stopped recording which directory a run worked in. No commit was lost;
// the FACT was.
//
// So the terminal arm now keeps the veto only on the two facts this host can
// actually PROVE about a finished row:
//
//	(i)  the row's own recorded runner identity still resolves to a live
//	     process (finishCleanupProcessHolderLive — the ONE identity ladder);
//	(ii) a live process is STANDING IN the directory the row names
//	     (finishCleanupWorktreeInUse — the ONE occupancy scan, the same one the
//	     removal arms refuse on), asked only when the claim pid IS alive, so the
//	     scan never runs for the ordinary case of a long-dead stamp.
//
// An empty directory (the sweep already collected it) with a live, unrelated
// claim pid is therefore NO holder — while every unmeasurable factor still
// answers HOLD (a foreign claim, a row naming no directory at all, a /proc this
// process cannot read). Nothing the removal arms protect is weakened: the
// directory a live process stands in keeps its veto through THIS predicate and,
// independently, through the removal arms' own occupancy guard.
//
// The live-STATUS arm asks TWO independent questions and keeps the veto when
// EITHER says the row still holds the directory (finishCleanupHolderLive, the
// ONE definition of that verdict — Rule 14):
//
//	(a) the claim stamp resolves on this host to a LIVE pid — the cheap proven
//	    case that freed nothing real;
//	(b) a process identity the LEDGER recorded (proc_pid + proc_start_ticks +
//	    proc_cmd, the runner's own stamp) still resolves to a live process
//	    whose start time and command line match what was recorded.
//
// (b) exists because of the 2026-09-14 incident (task 01a09d67): (a) alone
// judged liveness by a pid whose CWD could not be read — under
// kernel.yama.ptrace_scope=1 a NON-DESCENDANT process's /proc/<pid>/cwd is
// EACCES even for the same uid, measured 185 of ~360 entries on this host —
// so every liveness probe built on that channel failed OPEN, and a live
// session's worktree was deleted mid-run twice. (b) touches only files the
// kernel serves cross-session (stat, cmdline), so an unresolvable claim pid or
// host can no longer turn "alive" into "dead".
func finishCleanupOwnerRowLive(status, claimedBy string, rec procIdentity, worktree string) bool {
	if !taskLiveHolderStatus(status) {
		// THE ROW'S LIFE IS OVER. Two facts this host can PROVE keep the veto;
		// everything else is history (see the decision note above).
		if finishCleanupProcessHolderLive(rec) {
			return true // (i) the row's own recorded runner is still running it
		}
		host, pid, ok := claimHolder(claimedBy)
		if !ok {
			return host != "" // foreign claim: unknowable here — never call it dead
		}
		if !pidAlive(pid) {
			return false // the claim pid is measurably gone: nothing holds the ground
		}
		return finishCleanupTerminalRowOccupies(worktree) // (ii) somebody stands IN it
	}
	if !finishCleanupDeadLocalHolder(status, claimedBy, rec) {
		return true // the claim pid resolves here and is alive
	}
	// The status is live and the local CLAIM pid is gone — which is NOT "the
	// holder is gone" (2026-09-14 incident, task 01a09d67): the claim stamp
	// names whichever process took the row, while the runner itself recorded
	// its own identity on the row. Ask that record before releasing the veto.
	return finishCleanupProcessHolderLive(rec)
}

// finishCleanupTerminalRowOccupies answers the SECOND question a finished row
// must still pass to keep a ground: is a live process standing in the directory
// the row names?
//
// It is the terminal arm's whole use of occupancy, and it delegates to
// finishCleanupWorktreeInUse — the ONE cwd scan the removal arms refuse on
// (Rule 14: a second /proc walk would drift from the guard it exists to agree
// with, and drift here is a deleted directory). It is asked ONLY when the claim
// stamp's pid is alive, so a host with thousands of terminal rows pays for the
// scan on the handful whose pid number happens to be in use.
//
// EVERY UNMEASURABLE ANSWER IS A HOLD, with ONE deliberate exception named so
// it is not mistaken for the rule: a row naming no directory has nothing this
// host can occupy — the branch axis alone (a row that records `branch` and no
// worktree) is a NAME, and a name this host cannot point at is not evidence, so
// it HOLDS, exactly as before this change. The exception is the scan itself:
// finishCleanupWorktreeInUse is fail-soft by construction, so an UNREADABLE
// /proc returns "" and this reads "not occupied" — a release on a missing
// measurement. That direction is safe HERE and only here, because the removal
// arms keep their own occupancy guard independently (the same predicate, asked
// before anything is deleted), so a missed read can free a ledger CLAIM without
// ever deleting a directory a process stands in.
func finishCleanupTerminalRowOccupies(worktree string) bool {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return true // no directory to stand in: the name alone is not measurable
	}
	if _, why := finishCleanupWorktreeInUse(worktree); why != "" {
		return true // a live process stands in it — the ground is held
	}
	return false
}

// procIdentity is the runner identity a task ROW recorded about itself (see
// the tasks ledger's proc_pid / proc_start_ticks / proc_cmd columns). It is
// the row's own statement "this process is running me", and it is what
// finishCleanupProcessHolderLive compares against the live process table.
type procIdentity struct {
	Pid        int
	StartTicks int64
	Cmd        string
}

// finishCleanupProcessHolderLive answers "the row's own recorded runner is
// still alive?" for one row. Returns true (HOLD the veto) in every case the
// host cannot positively prove the runner is gone.
//
// It delegates the identity comparison to procname.RunnerLive — the SINGLE
// implementation of "is this still the same live process?" for a recorded
// pid + starttime + cmdline (Rule 14: a second ladder drifts, and this drift is
// fail-open: memory #2632). That ladder, its doc comment and the neighbouring
// checkout-occupancy question now live there; this function keeps only its own
// CONTRACT, which is a READING RULE rather than a comparison:
//
//	true  = hold the veto, because the runner is live OR because the host could
//	        not measure it at all (Rule 15a: an unmeasurable factor is not a
//	        death proof — the pre-consolidation body returned false for a
//	        process whose /proc/<pid>/stat was unreadable while the directory
//	        was present, which let a sweep free a live run's worktree)
//	false = a MEASURED not-live: the process directory is gone, the pid was
//	        recycled by a different incarnation, or the pid names a zombie.
//
// The recorded command line is deliberately NOT passed as a factor: a runner's
// cmdline is a HINT, because the recorded spelling is whatever the writer
// captured (the raw /proc cmdline, trailing separator and truncation included)
// while the runner may have re-exec'd in place — a wrapper `sh -c …` becoming
// its target under the same pid — which turned a LIVE runner into a death proof
// on 2026-09-17 and made the sweep delete its worktree. The incarnation ticks
// are the strong factor; procname.RunnerLive's doc states the rule.
func finishCleanupProcessHolderLive(rec procIdentity) bool {
	if rec.Pid <= 0 || (rec.StartTicks <= 0 && rec.Cmd == "") {
		// No runner identity was recorded at all: there is nothing to compare,
		// and the callers fall back to their own claim-pid evidence exactly as
		// they did before the identity columns existed.
		return false
	}
	live, why := procname.RunnerLive(identityProcRoot,
		identityLiveness(rec.Pid, rec.StartTicks, rec.StartTicks > 0, rec.Cmd))
	if why != "" {
		return true // unmeasurable: the host cannot prove the runner is gone
	}
	return live
}

// finishCleanupDeadLocalHolder reports whether a row's STATUS still looks
// live while this host can PROVE its holder is gone: the status is a live one
// (taskLiveHolderStatus — the single live-status definition, shared with the
// reclaim arm in task_reclaim.go) and the claim stamp resolves on this host
// (claimHolder's ok=true — a FOREIGN host's pid is unknowable here and is
// therefore never called dead) to a pid that is not alive (pidAlive — the
// zombie-aware probe, so a SIGKILLed-but-unreaped wrapper counts as dead).
//
// It is the WHOLE disposition for one row shape, so both consumers ask the
// same question of the same inputs:
//
//	holder block (finishCleanupOwnerRowLive) — such a row no longer vetoes
//	the cleanup of the directory it names, which is what un-stranded the
//	orphan: before this, a dead wrapper's row vetoed its OWN directory's
//	cleanup forever, while the reclaim scan (keyed on TERMINAL rows) never
//	looked at a live-status row either — so the directory and its branch
//	(gate [c]) stayed parked until a human closed the row out.
//	release report / reclaim arm — the freed directory is NAMED with this
//	disposition (finishCleanupDeadLocalHolderMarker), never freed silently.
//
// The evidence is deliberately the pid layer ALONE, not the stale-heartbeat
// chain (zombieVerdict/claimStampFreshness in taskzombie.go): a stale claim
// stamp proves neither life nor death by design, and this predicate must
// never free a directory on "probably". A pid this host can see as gone is
// the one death proof that carries no false positive — and the remaining
// guards (a clean worktree, no process with a cwd inside, the branch match)
// still hold in full, so a runner that is alive anywhere in the directory's
// process tree still vetoes through the cwd scan.
func finishCleanupDeadLocalHolder(status, claimedBy string, rec procIdentity) bool {
	if !taskLiveHolderStatus(status) {
		return false
	}
	_, pid, known := claimHolder(claimedBy)
	if !known {
		return false // no stamp / foreign host: unknowable here — never dead
	}
	if pidAlive(pid) {
		return false
	}
	// The claim pid is gone. Before calling the row unheld, ask the row's OWN
	// record of its runner (2026-09-14 incident, task 01a09d67): a live runner
	// the claim pid does not name must not be reported as a dead local holder,
	// or every consumer of this disposition — the reclaim arm above all —
	// frees a live session's directory and prints a release note for it.
	if rec.Pid <= 0 || (rec.StartTicks <= 0 && rec.Cmd == "") {
		// The row recorded NOTHING about a runner: the claim-pid death proof is
		// then the whole evidence, exactly as it was before the identity columns
		// existed (a row stamped by an older build must keep its disposition).
		return true
	}
	// (the identity was recorded — see the predicate call below)
	// An identity WAS recorded. The disposition is a RELEASE — it names the freed
	// directory in a release note — so it needs a POSITIVE death measurement and
	// never rests on a factor the kernel did not answer (Rule 15a): the ONE
	// predicate's Gone/Recycled are measured deaths, and Unmeasurable leaves the
	// row holding its ground.
	outcome, why := procname.RunnerIdentity(identityProcRoot,
		identityLiveness(rec.Pid, rec.StartTicks, rec.StartTicks > 0, rec.Cmd))
	return why == "" && (outcome == procname.OutcomeGone || outcome == procname.OutcomeRecycled)
}

// finishCleanupOwnerRowGate decides whether the row may remove worktree:
// gate ① (declared-vs-resolved) then gate ② (no other row's worktree).
// Declared = the row's own deliverable.worktree ("" = the row carries no
// worktree claim — the backfill-gap shape). TaskID = the row being cleaned
// (excluded from the foreign-owner scan). Returns "" when the removal is
// allowed, else the skip reason (both paths / holder named).
//
// Callers whose removal runs at the very END of their step sequence (the
// chain's step ③) use the two halves directly — the declared half BEFORE
// touching the directory, the foreign half after the branch is safely
// pushed (see runFinishCleanupChainTarget): a refused removal must never
// cost the delivery.
func finishCleanupOwnerRowGate(taskID, declared, worktree string) string {
	if why := finishCleanupOwnerDeclaredReason(declared, worktree); why != "" {
		return why
	}
	owners, err := finishCleanupWorktreeOwnerRows(taskID)
	if err != nil {
		return "" // ledger unreadable — best-effort, never block on it (Rule 15: the arm reports its own steps)
	}
	block, _ := finishCleanupForeignOwnerReason(owners, worktree)
	// A live-STATUS row whose local holder is provably dead is NOT a holder
	// (finishCleanupOwnerRowLive), so it never reaches this refusal: the
	// directory a dead wrapper's orphan names is free — that is the whole
	// point of the orphan epic. The release is REPORTED by the arm that frees
	// the directory (the reclaim line / the live-owner gate's release note),
	// never silently swallowed here (Rule 15).
	return block
}

// finishCleanupOwnerDeclaredReason is gate ①: the resolved worktree must BE
// the directory the row's own deliverable claims (path identity,
// canonicalized — a symlinked / trailing-slash spelling of the SAME
// directory stays a match; a subdirectory does NOT: git removes a worktree
// by its ROOT path). "" = allowed (no declaration to bind against, or a
// match).
func finishCleanupOwnerDeclaredReason(declared, worktree string) string {
	worktree = strings.TrimSpace(worktree)
	declared = strings.TrimSpace(declared)
	if worktree == "" || declared == "" || finishCleanupSameWorktreePath(declared, worktree) {
		return ""
	}
	return finishCleanupOwnerMismatchMarker + ": deliverable names " + declared +
		" but cleanup resolved " + worktree
}

// finishCleanupForeignOwnerReason is gate ② over a preloaded owner list. It
// returns TWO strings, because the gate both decides and reports:
//
//	block — non-empty when ANOTHER row still HOLDS the target: the removal
//	        must not happen, and the string names the owner (row id, status,
//	        and — when the claim resolves on this host — whether its holder
//	        pid is alive).
//	note  — non-empty when the only other rows naming the target are STALE
//	        bindings (their life is over and their holder is provably gone):
//	        the removal MAY proceed, but the observation is reported so an
//	        operator still sees the two-row claim on the directory.
//
// Used by every removal path (the finish-cleanup arms AND the merged-branch
// sweep), so ownership is enforced by ONE implementation.
//
// Only a LIVE holder blocks (2026-09-12 stranded-worktree fix). A row whose
// life is over and whose holder is provably gone is HISTORY, not an owner:
// blocking on it is what stranded directories forever — the sweep scanned a
// completed row, found a cancelled/failed sibling naming the same worktree,
// refused ("owned by row X (cancelled)") and moved on; the sibling's own
// turn never re-ran the cleanup, so not one of the two ever released the
// directory. A stale holder is still REPORTED — loudly, with the
// finishCleanupOwnerStaleMarker — because an operator should see a
// directory two rows claim (an accidental re-attach), it just must not
// block the delivered row's recycle.
func finishCleanupForeignOwnerReason(owners []TaskWorktreeRow, worktree string) (block, note string) {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return "", ""
	}
	var live, stale *TaskWorktreeRow
	for i := range owners {
		o := owners[i]
		if !finishCleanupSameWorktreePath(o.Deliverable, worktree) {
			continue
		}
		if o.HolderLive && live == nil {
			row := o
			live = &row
			continue
		}
		if stale == nil {
			row := o
			stale = &row
		}
	}
	if live != nil {
		return finishCleanupOwnerMismatchMarker + ": worktree " + worktree +
			" is owned by row " + live.TaskID + " (" + live.Status + finishCleanupOwnerHolderSuffix(live.ClaimedBy) + ")", ""
	}
	if stale != nil {
		return "", finishCleanupOwnerStaleMarker + ": worktree " + worktree +
			" was bound to row " + stale.TaskID + " (" + stale.Status + finishCleanupOwnerHolderSuffix(stale.ClaimedBy) +
			") whose holder is gone — the binding is stale"
	}
	return "", ""
}

// finishCleanupOwnerHolderSuffix renders the ownership holder's liveness
// when the row's claim stamp resolves on THIS host (pid alive / pid gone);
// foreign or unstamped rows add nothing (claimHolder's contract: a foreign
// claim's pid is unknowable here — never reported as dead).
func finishCleanupOwnerHolderSuffix(claimedBy string) string {
	host, pid, ok := claimHolder(claimedBy)
	if !ok {
		if host != "" {
			return ", claimed by " + host
		}
		return ""
	}
	if pidAlive(pid) {
		return fmt.Sprintf(", claimed by %s pid %d (alive)", host, pid)
	}
	return fmt.Sprintf(", claimed-by pid %d gone", pid)
}

// finishCleanupWorktreeBranch reports the branch checked out in worktree
// ("" for a detached HEAD, an unreadable worktree, or a git failure — a
// caller treats "unknown" as "no branch axis to compare", never as a
// match; Rule 15: the git error is folded into the empty answer here
// because the gate must never refuse on an unreadable probe alone).
func finishCleanupWorktreeBranch(ctx context.Context, worktree string) string {
	// PARSED branch name: stdout only (a stderr trace line would be read as the
	// branch a directory checks out — the exact refusal the self-delivery arm
	// printed under GIT_TRACE=1).
	out, err := finishCleanupGitParsedStdout(ctx, worktree, finishCleanupGitTimeout,
		finishCleanupPreMergeRevParseVerb, finishCleanupPreMergeAbbrevRefFlag, finishCleanupPreMergeHeadArg)
	if err != nil {
		return ""
	}
	out = strings.TrimSpace(out)
	if out == "" || out == finishCleanupDetachedHead {
		return ""
	}
	return out
}

// finishCleanupSameWorktreePath reports whether two worktree paths name the
// SAME directory: cleaned, symlink-resolved, separator-trimmed. Empty on
// either side = no match (never a vacuous positive — Rule 15).
func finishCleanupSameWorktreePath(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return finishCleanupCanonicalWorktreePath(a) == finishCleanupCanonicalWorktreePath(b)
}

// finishCleanupCanonicalWorktreePath canonicalizes a worktree path for
// identity comparison: absolute (against the process cwd when relative),
// symlinks resolved (EvalSymlinks — /tmp vs /private/tmp style spellings),
// falling back to filepath.Clean when the path does not exist any more.
func finishCleanupCanonicalWorktreePath(p string) string {
	p = strings.TrimSpace(p)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}

// finishCleanupWorktreeOwnerRows loads every task row whose deliverable
// names a worktree, EXCLUDING taskID (the row being cleaned). Best-effort:
// a DB error returns it — the caller then skips the ownership scan rather
// than blocking the cleanup (the ledger is the source of truth; when it is
// unreadable, the declared-vs-resolved gate is all there is to bind).
func finishCleanupWorktreeOwnerRows(taskID string) ([]TaskWorktreeRow, error) {
	taskID = taskIDNormalize(taskID)
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// A cheap SQL pre-filter (LIKE) keeps the scan off rows that cannot
	// carry a worktree; the JSON decode below stays the authority. The scan
	// is deliberately COMPLETE over that subset — every row naming a
	// worktree is classified by finishCleanupOwnerRowLive, so ONE predicate
	// (not a SQL shape plus a Go shape) decides who still holds a directory
	// (Rule 14: one source of truth).
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, claimed_by, deliverable, COALESCE(`+tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks WHERE deliverable LIKE ?`,
		"%"+finishCleanupWorktreeVerb+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskWorktreeRow
	for rows.Next() {
		var id, status, claimedBy, deliverable string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&id, &status, &claimedBy, &deliverable, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		if strings.TrimSpace(taskID) != "" && id == taskID {
			continue // the row being cleaned is never its own foreign owner
		}
		wt := taskDeliverableWorktree(deliverable)
		if wt == "" {
			continue
		}
		rec := procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}
		out = append(out, TaskWorktreeRow{
			TaskID: id, Status: status, ClaimedBy: claimedBy, Deliverable: wt,
			HolderLive:      finishCleanupOwnerRowLive(status, claimedBy, rec, wt),
			HolderDeadLocal: finishCleanupDeadLocalHolder(status, claimedBy, rec),
		})
	}
	return out, rows.Err()
}

// finishCleanupOwningWorktree finds the linked worktree containing dir
// (empty string = dir is a main checkout / not a worktree). Best-effort:
// `git worktree list --porcelain` parsing, first <worktree> line that is a
// path-prefix of dir (or equals it). The FIRST porcelain entry is always the
// main checkout and is never a cleanup target — skipping it explicitly is
// what keeps a dir INSIDE the main checkout (repo/sub) from resolving to the
// main checkout itself (git would refuse that removal anyway: "is a main
// working tree").
func finishCleanupOwningWorktree(ctx context.Context, dir string) string {
	out, err := finishCleanupGitParsedStdout(ctx, dir, finishCleanupGitTimeout, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	// The FIRST porcelain entry is always the main checkout — never a
	// cleanup target. Skipping it explicitly (not just a p == abs test) is
	// what keeps a dir INSIDE the main checkout (repo/sub) from resolving
	// to the main checkout itself.
	first := true
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, finishCleanupPorcelainWorktreePrefix) {
			continue
		}
		if first {
			first = false
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainWorktreePrefix))
		if p == "" {
			continue
		}
		if abs == p || strings.HasPrefix(abs, p+string(filepath.Separator)) {
			return p
		}
	}
	return ""
}

// finishCleanupProcParent reads one process's parent pid from
// <procRootName>/<pid>/<procStatName>. (0, false) = unreadable (gone /
// not visible) — the caller then ends its walk. The command name in that file
// is parenthesised and may contain spaces and parentheses, so every field
// after the LAST ')' is parsed — by the ONE parse in internal/procname
// (Rules 13/14), which is what sets both the parent column and the
// count-from-the-last-')' trap.
func finishCleanupProcParent(pid string) (int, bool) {
	raw, ok := procname.StatFieldString(filepath.Join(procRootName, pid, procStatName), procname.StatFieldParent)
	if !ok {
		return 0, false
	}
	ppid, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// finishCleanupSelfChain is the caller's own process tree, upward: this
// process and every ancestor pid. The liveness guard EXCLUDES them, and the
// reason is a production fact, not a convenience: the close-out runs INSIDE
// the very run that occupied the worktree — a manual `gopi task run` (and
// the worker pass) executes with its own CWD inside that linked worktree
// (cmd/agent/cli_tasks_recycle_e2e_test.go drives the real binary exactly
// that way), so counting the caller as a "live holder" would make the
// recycle refuse its own legitimate cleanup and leave EVERY manual run's
// worktree behind. The incident the guard exists for is a DIFFERENT process
// (a second live session, or a sweep acting for another row) — that stays
// caught, because it is nobody's ancestor here.
//
// The walk is bounded by finishCleanupProcChainMaxHops so a procfs cycle
// (or the pid-1 re-parenting race) always terminates.
func finishCleanupSelfChain() map[int]bool {
	chain := map[int]bool{}
	pid := os.Getpid()
	for hops := 0; hops < finishCleanupProcChainMaxHops; hops++ {
		if pid <= 0 || chain[pid] {
			break
		}
		chain[pid] = true
		parent, ok := finishCleanupProcParent(strconv.Itoa(pid))
		if !ok {
			break
		}
		pid = parent
	}
	return chain
}

const (
	// finishCleanupFamilyMaxHops bounds the upward walk that asks "does this
	// process descend from one of MY own ancestors?" — the ancestry half of
	// finishCleanupAskFamily's criterion. A /proc parent chain is finite, but a
	// bound keeps the walk from being walked for a hostile/odd table, and a
	// walk that runs out of hops answers NO (which keeps the veto: Rule 15a).
	finishCleanupFamilyMaxHops = 8

	// finishCleanupPipeTargetPrefix is what /proc/<pid>/fd's readlink yields for a
	// descriptor on an anonymous pipe ("pipe:[<inode>]", inode included) — the
	// shape that names the pipeline two processes are the two ends of.
	finishCleanupPipeTargetPrefix = "pipe:["

	// finishCleanupFdInfoFlagsKey prefixes the fdinfo line carrying the
	// descriptor's open(2) flags ("flags:\t0100000"); finishCleanupFdAccessMask
	// is the low two bits of that word — the access mode (O_RDONLY 0 / O_WRONLY 1
	// / O_RDWR 2), which is what distinguishes a pipe's two ends (Rule 1).
	finishCleanupFdInfoFlagsKey = "flags:"
	finishCleanupFdAccessMask   = 3
	// finishCleanupPipeAccessRead / …Write are the two access modes a pipeline
	// connects: one stage writes the pipe, the next reads it.
	finishCleanupPipeAccessRead  = 0
	finishCleanupPipeAccessWrite = 1

	// finishCleanupPipeFdScanMax bounds how many descriptors the pipe reading
	// looks at per process: far above any real pipeline, and the bound is what
	// keeps it from walking a table with tens of thousands of fds (Rule 27: a
	// bounded loop carries its own cap).
	finishCleanupPipeFdScanMax = 256
)

// finishCleanupAskIdentity is THIS process's own /proc identity, read ONCE at
// package init: the pid, its start ticks (the kernel's INCARNATION of the pid
// slot) and its process group id. They are read once because the question they
// answer — "was that process forked as a partner of the pipeline that started
// ME?" — is about a fork, and a fork time does not change afterwards.
type finishCleanupAskIdentity struct {
	pid   int
	start int64
	pgid  int
}

// finishCleanupAsk is the record above, initialised at package init so every
// reader shares ONE reading (Rules 13/14: a second walk of the asker's own
// /proc entry is a second answer to "who am I").
var finishCleanupAsk = finishCleanupReadAskIdentity()

// finishCleanupReadAskIdentity reads this process's own identity. An unreadable
// half is left at its ZERO value, which is exactly what makes
// finishCleanupAskFamily answer NO for every candidate — an unmeasurable asker
// keeps the full veto rather than silently widening the exclusion (Rule 15a:
// unknown is never a permission).
func finishCleanupReadAskIdentity() finishCleanupAskIdentity {
	ask := finishCleanupAskIdentity{pid: os.Getpid()}
	ask.start, _ = procStartTicks(ask.pid) //stylegate:ignore 0 = "my fork time is unknown", which DISABLES the family widening (finishCleanupAskFamily returns false) — the refusal direction
	if raw, ok := procname.StatFieldString(procname.StatStatPath(procRootName, strconv.Itoa(ask.pid)), procname.StatFieldPgrp); ok {
		if n, err := strconv.Atoi(raw); err == nil {
			ask.pgid = n
		}
	}
	return ask
}

// finishCleanupAskFamily reports whether pid stands on the ASKER'S OWN SIDE of
// the pipeline that produced this reading — the caller's own diagnostic
// plumbing, which is not a third-party holder and must not be counted as one.
//
// THE DEFECT THIS CLOSES (measured 2026-10-27, row 01a17d23; the same shape was
// already recorded as a reading trap on 2026-10-25). `cd <ground> && gopi task
// status --worktree . | grep ground_now` reported
//
//	ground_now:  IN USE by live process pid 1409561 (grep) argv="grep ^ground_now:" ppid=1409558 — cwd <ground>
//
// — the grep the operator typed as part of the very command asking the
// question. It is not a holder: its cwd is the ground only because the shell
// forked every pipeline stage from the same directory, and it exits with the
// command. finishCleanupSelfChain answers "my ancestors", which excludes the
// SHELL but not the shell's OTHER CHILDREN, so the pipeline's own sink was read
// as a competing live run.
//
// THE CRITERION. The first rung is a PROCESS-GROUP match (a launch family), and
// then the candidate must satisfy ONE of two independent "same fork wave"
// measurements — where "one fork wave" means the asker and the candidate were
// both forked by the caller's own command:
//
//	(a)  SAME PROCESS GROUP as the asker. A pipeline's stages inherit the group
//	     of the shell that launched them; a launcher that isolates a child
//	     (`Setpgid`, a daemon, `systemd-run`) puts it in its OWN group, and a
//	     third party's run therefore normally fails here — measured on this
//	     repo's own spawn seams (cmd/agent/cli_tasks.go sets Setpgid for the
//	     executor child). A different group is answered NO, full stop.
//
//	(a2) ONE OF THE ASKER'S OWN PIPES. The asker's file descriptors include the
//	     pipe the shell built for the pipeline, and the OTHER END of that pipe
//	     belongs to the partner — so "this process holds an fd on the same pipe
//	     inode as one of mine" is the kernel stating the two are two ends of one
//	     command. This rung is LOAD-BEARING, not a refinement: measured while
//	     developing this fix, a shell forks the pipeline's stages in the same
//	     /proc clock tick, so starttime ordering cannot tell the partner from a
//	     slightly older same-group process (a sh-pipe ambiguity that appeared in
//	     the partner-first arm). A pipe inode is exact where a tick is not.
//
//	(b)  SHARED ANCESTRY — walking UP from the candidate, one of the asker's
//	     ANCESTORS turns up within finishCleanupFamilyMaxHops: the two were
//	     forked by the same shell. The ASKER ITSELF is deliberately NOT accepted:
//	     meeting it means the candidate is one of the asker's DESCENDANTS, which
//	     the criterion keeps refusing (the asker's own executor child standing in
//	     the ground is a legitimate holder — epic worktree-not-released,
//	     2026-09-24); whose pin is TestWorktreeInUseKeepsTheAskersOwnChild.
//
//	(c)  FORKED AT OR AFTER the asker — asked of the ancestry rung only. A
//	     candidate that PREDATES this process cannot be part of the pipeline
//	     that started it: it is a live run, a daemon or a leftover that HAPPENS
//	     to share the group (`gopi task status` typed in a shell that also
//	     launched `gopi worker` for that very ground). It keeps its veto — the
//	     direction a wrong answer must take;
//	     TestWorktreeInUseKeepsAnOlderSameGroupProcess pins it.
//
//	(d)  THE ASKER'S IDENTITY IS MEASURABLE (pgid > 1, start ticks > 0) and the
//	     candidate's is too. Every unreadable field answers NO, so an
//	     unmeasurable asker keeps the FULL veto (Rule 15a: unknown is never a
//	     permission).
//
// WHAT IT DOES NOT TOUCH: the verdict for a genuine third party, and every
// OTHER exclusion (the orphan reading, the test-artifact age floor, the daemon
// note) — those are downstream of this predicate and unchanged. The shared
// predicate is the ONLY place this exclusion lives, so the five production arms
// that ask it (finish-cleanup, reclaim, merged-cleanup, status, sweep) all
// inherit the fix at once (Rules 13/14).
func finishCleanupAskFamily(pid int) bool {
	ask := finishCleanupAsk
	if ask.pid <= 0 || ask.pgid <= 1 || ask.start <= 0 {
		return false // my own identity is unmeasurable — no widening, full veto
	}
	if pid <= 1 || pid == ask.pid {
		return false
	}
	raw, ok := procname.StatFieldString(procname.StatStatPath(procRootName, strconv.Itoa(pid)), procname.StatFieldPgrp)
	if !ok {
		return false
	}
	pgid, err := strconv.Atoi(raw)
	if err != nil || pgid != ask.pgid {
		return false // (a) a different process group is somebody else's launch
	}
	descendant, sharesAncestor := finishCleanupAskRelation(pid, ask.pid)
	if descendant {
		return false // my own child is a legitimate holder, in every arm
	}
	if finishCleanupSharesAskerPipe(pid, ask.pid) {
		return true // (a2) the other end of a pipe this asker holds
	}
	start, ok := procStartTicks(pid)
	if !ok {
		return false // (c) unmeasurable fork time: not my pipeline
	}
	if !finishCleanupForkedAfter(start, pid, ask) {
		return false // (c) AT OR BEFORE me: not my pipeline
	}
	return sharesAncestor // (b) forked by the same launcher
}

// finishCleanupSameTickPidDelta bounds the pid distance that still reads as
// "one fork wave" when /proc's tick counter cannot separate two forks.
//
// WHY THE TICK ALONE IS NOT ENOUGH (measured 2026-10-27, row 01a17d97; the
// defect is a DATA-LOSS arm, not a cosmetic one). /proc/<pid>/stat's starttime
// is counted in CLOCK TICKS (10 ms here), while a shell forks a pipeline's
// stages microseconds apart — so the asker and its partner routinely share a
// tick, and "start < ask.start" answers "not after me" for a process that WAS
// forked after. Measured on this tree: `TestWorkerResumeEmitsTheCloseoutReportOnce`
// passed on the first run of a test binary and failed on every later one, and the
// instrumented reading was
//
//	asker=2062530 start=101516640 | holder=2062517 start=101516639  (REFUSE)
//	asker=2062518 start=101516639 | holder=2062517 start=101516639  (SPARED — wrong)
//
// The holder there is the test's own `sleep` stand-in, forked BEFORE the gopi
// worker that asks; sharing the tick let it through, the close-out chain then
// removed a worktree the liveness guard exists to protect, and the report the
// test counts was never emitted.
//
// THE TIE-BREAK. The kernel allocates pids monotonically within a tick, so a
// strictly greater pid in the SAME tick is the later fork. The bound is what
// keeps this from becoming a widening: a gap larger than
// finishCleanupSameTickPidDelta is a wrapped pid_max or an unrelated process,
// and neither is credited.
const finishCleanupSameTickPidDelta = 1024

// finishCleanupForkedAfter reports whether pid was forked STRICTLY AFTER the
// asker — the (c) rung of finishCleanupAskFamily. A process forked at or before
// the asker cannot be part of the pipeline the asker started, however many
// other rungs it satisfies (Rule 15a: an ordering this reading cannot measure
// keeps the veto).
func finishCleanupForkedAfter(start int64, pid int, ask finishCleanupAskIdentity) bool {
	if start > ask.start {
		return true
	}
	if start < ask.start {
		return false
	}
	// SAME TICK: only the pid allocation order can separate the two forks.
	if pid <= ask.pid {
		return false
	}
	return pid-ask.pid <= finishCleanupSameTickPidDelta
}

// finishCleanupAskRelation answers the two ancestry facts about pid in ONE
// upward walk — whether pid descends from the asker ITSELF, and whether it
// descends from one of the asker's ANCESTORS (i.e. was forked by the same
// launcher). Meeting the asker means "my child"; running out of hops or of
// /proc means "not measured", which answers false for both (Rule 15a).
func finishCleanupAskRelation(pid, asker int) (descendantOfAsker, sharesAnAncestor bool) {
	// THE REPARENT TARGETS ARE NEVER ANCESTRY EVIDENCE (measured 2026-10-27 while
	// red-proving this fix: TestSharedSessionHolderProbe went red because a
	// fixture holder whose launcher had exited was handed to this host's reaper —
	// `systemd --user`, pid 54349 here — which is ALSO one of the asker's own
	// ancestors, so a walk that climbed into it "proved" a shared ancestor for a
	// process the asker never forked). The kernel rewrites ppid on reparenting,
	// so anything at or above a reparent target says nothing about who forked
	// whom: the walk STOPS there and answers NO (Rule 15a). The same set
	// finishCleanupSelfTree excludes, read through the same call.
	reparentTargets, _ := finishCleanupOrphanReparentTargets() //stylegate:ignore an unmeasurable set degrades to the pre-widening exclusion (the walk simply climbs further), which keeps the veto — the refusing direction
	ancestors := finishCleanupSelfChain()
	delete(ancestors, asker) // the asker's own pid means "descendant", handled below
	for target := range reparentTargets {
		delete(ancestors, target)
	}
	at := pid
	for hops := 0; hops < finishCleanupFamilyMaxHops; hops++ {
		parent, ok := finishCleanupProcParent(strconv.Itoa(at))
		if !ok || parent <= 1 || reparentTargets[parent] {
			return false, false
		}
		if parent == asker {
			return true, false
		}
		if ancestors[parent] {
			return false, true
		}
		at = parent
	}
	return false, false
}

// finishCleanupSharesAskerPipe reports whether pid and the asker hold OPPOSITE
// ENDS of one pipe — the kernel's statement that the two are the two stages of
// ONE command (see rung (a2)).
//
// DIRECTION IS THE LOAD-BEARING HALF, measured while developing this fix: a
// fixture that forked the two halves from one shell had BOTH of them pointing
// their stdout at the same inherited pipe, and an "inode match" rung then
// spared a process that merely shared the harness's output — refusing nothing.
// Two ends of a pipeline are written by one side and read by the other, so the
// shared inode must be seen with OPPOSITE access modes; two writable
// descriptors on one inode are a COMMON ANCESTOR's output, not a connection
// between the two processes.
func finishCleanupSharesAskerPipe(pid, asker int) bool {
	mine := finishCleanupPipeEndsOf(asker)
	if len(mine) == 0 {
		return false // the asker holds no pipe — no statement to make
	}
	for inode, mode := range finishCleanupPipeEndsOf(pid) {
		other, ok := mine[inode]
		if !ok {
			continue
		}
		// A read end facing a write end (in either order) is one pipeline.
		if (mode == finishCleanupPipeAccessRead && other == finishCleanupPipeAccessWrite) ||
			(mode == finishCleanupPipeAccessWrite && other == finishCleanupPipeAccessRead) {
			return true
		}
	}
	return false
}

// finishCleanupPipeEndsOf reads ONE process's pipe descriptors as
// inode → access mode, from /proc/<pid>/fd (the "pipe:[<inode>]" link) and
// /proc/<pid>/fdinfo/<fd> (the "flags:" octal word whose low two bits are the
// access mode). The scan is bounded by finishCleanupPipeFdScanMax so a process
// with thousands of descriptors cannot make this walk expensive, and an
// unreadable entry is skipped — "no statement", never "shared".
func finishCleanupPipeEndsOf(pid int) map[string]int {
	out := map[string]int{}
	fdDir := filepath.Join(procRootName, strconv.Itoa(pid), procFdDirName)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return out
	}
	for i, entry := range entries {
		if i >= finishCleanupPipeFdScanMax {
			break
		}
		target, lerr := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if lerr != nil || !strings.HasPrefix(target, finishCleanupPipeTargetPrefix) {
			continue
		}
		mode, ok := finishCleanupFdAccessMode(pid, entry.Name())
		if !ok {
			continue
		}
		out[target] = mode
	}
	return out
}

// finishCleanupFdAccessMode reads one descriptor's access mode from its fdinfo
// flags word: the low two bits are O_RDONLY (0) / O_WRONLY (1) / O_RDWR (2).
// An unreadable or unparsable entry answers false (the caller then makes no
// statement about that descriptor).
func finishCleanupFdAccessMode(pid int, fd string) (int, bool) {
	raw, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), procFdInfoDirName, fd))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, finishCleanupFdInfoFlagsKey) {
			continue
		}
		flags, perr := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, finishCleanupFdInfoFlagsKey)), 8, 64)
		if perr != nil {
			return 0, false
		}
		return int(flags & finishCleanupFdAccessMask), true
	}
	return 0, false
}

// finishCleanupSelfTree answers "which pids are NOT third parties to this
// asker?" — the set the ladder's session census must skip when it decides
// whether a live run still stands behind a holder.
//
// It is finishCleanupSelfChain widened DOWNWARD: every ancestor of the asker
// (the launcher chain that started it — not a third party either) PLUS the pids
// this asker itself ACCOUNTS for, which is the asker and the transitive
// descendants of the asker. The two halves are deliberately different sets: the
// ancestors are EXCLUDED but seed nothing, because an ancestor is a shell /
// sshd / session leader whose other children are this host's SIBLING runs.
//
// WHY THE DOWNWARD HALF EXISTS (epic worktree-not-released, measured
// 2026-09-24 on this host): the release passes run as a DESCENDANT of the run
// whose ground they release. `gopi task run`'s close-out happens inside the
// wrapper process, and the worker's pass is a deferred call inside the round,
// while the round's EXECUTOR CHILD was spawned with cmd.Dir = the worktree
// (cmd/agent/cli_tasks.go) — so the process standing in the registration under
// judgement is the asker's own child. Nothing named that child, so the ladder's
// last rung counted the asker's own executor as a third-party holder of the
// session and kept a directory only its own tree was standing in. Measured:
// worker 492455 → executor child 492481, both in session 17262, cwd inside the
// very worktree the round had finished.
//
// WHY THE ANCESTORS ARE EXCLUDED BUT NEVER EXPANDED FROM (measured 2026-09-25,
// the first attempt at this fix): that version expanded the closure from every
// pid already in the set, ancestors included. Every process's ancestry ends at
// init, so the seed set contained pid 1 and the closure reached the WHOLE HOST —
// 65 pids on the measured box, unrelated `gopi worker` rounds and a sandboxed
// `gopi ui` among them. The set fed the census, so over-excluding made the rung
// answer "nothing is live" always: not a narrower veto but no veto at all, and
// a bystander's registration would have been torn down. Regression guard:
// TestFinishCleanupSelfTreeSparesThirdParties.
//
// PID 1 IS NEVER A MEMBER, and that exclusion is LOAD-BEARING: the kernel
// reparents every orphan to one of THIS host's reparent targets, so an
// init-rooted expansion would put the WHOLE orphan class inside "my own tree"
// and release directories a bystander still holds. Every MEASURED target is
// skipped for the same reason (finishCleanupOrphanReparentTargets — the namespace
// init plus any systemd user instance, which on this host is pid 3100 rather
// than pid 1).
//
// An unmeasurable /proc yields the upward chain alone: the caller then keeps
// exactly the behaviour it had before this widening (Rule 15a — unknown is
// never a permission).
func finishCleanupSelfTree() map[int]bool {
	// The pids an orphan can be handed to on this host (namespace init + any
	// systemd user instance), read ONCE for both the exclusion and the closure
	// below. An unmeasurable set degrades to the UPWARD CHAIN ALONE — exactly the
	// behaviour that preceded the widening, which is the safe direction (Rule 15a:
	// unknown is never a permission). The error is deliberately NOT propagated:
	// this function's contract is a SET, and an empty set IS the pre-widening
	// reading; the DECIDING call sites (the orphan ladder and the orphan scan)
	// measure the set themselves and report the unmeasurable case in their own
	// right, so no failure is swallowed anywhere it could release a directory.
	reparentTargets, _ := finishCleanupOrphanReparentTargets() //stylegate:ignore an unmeasurable set degrades to the pre-widening exclusion, and the deciding call sites measure it themselves
	notThirdParty := map[int]bool{}                            // what the caller excludes
	mine := map[int]bool{}                                     // what this asker ACCOUNTS for (expansion seeds)
	self := os.Getpid()
	for pid := range finishCleanupSelfChain() {
		if pid <= 1 || reparentTargets[pid] {
			continue
		}
		notThirdParty[pid] = true
	}
	notThirdParty[self] = true
	mine[self] = true
	pids, err := finishCleanupPIDDirs()
	if err != nil {
		return notThirdParty
	}
	// Transitive closure seeded by WHAT THE ASKER ACCOUNTS FOR — the asker and
	// its own descendants — never by its ancestors.
	//
	// WHY (measured 2026-09-25): the first version expanded from every pid
	// already in the set, ancestors included. An ancestor is a shell / sshd /
	// session leader whose children are this host's OTHER runs, so that
	// expansion reached the whole host — 65 pids on the measured box, unrelated
	// `gopi worker` rounds and a sandboxed `gopi ui` among them — and a census
	// that excludes the host never vetoes anything. The ancestors stay
	// EXCLUDED (a launcher chain is not a third party either: a worker must not
	// veto its own launcher's session), but nothing is attributed to them: an
	// unrelated round is a third party and keeps its veto.
	//
	// Transitive closure, bounded by the pid count: each round can only add, and
	// the loop stops as soon as a round adds nothing.
	for added := true; added; {
		added = false
		for _, p := range pids {
			n := finishCleanupAtoi(p)
			if n <= 0 || n == 1 || notThirdParty[n] || reparentTargets[n] {
				continue
			}
			parent, ok := finishCleanupProcParent(p)
			if !ok || !mine[parent] {
				continue
			}
			notThirdParty[n] = true
			mine[n] = true
			added = true
		}
	}
	return notThirdParty
}

// finishCleanupProcCensus reads the live process table ONCE into parent and
// session maps (the two ancestry/session questions every walk below asks).
//
// It exists so every classification in this file reads the SAME measurement
// through the SAME /proc reader
// (Rules 13/14): a second walker is a second answer to "who is alive", and the
// two would drift exactly where the answer decides whether a process is killed.
//
// A pid whose stat is unreadable is simply absent from both maps — the process
// died between the listing and the read, or it is not ours to inspect. Both mean
// "not evidence", which is the safe direction for a KEEP (the callers refuse to
// act on a process they cannot place).
func finishCleanupProcCensus() (parents map[int]int, sessions map[int]int) {
	parents = map[int]int{}
	sessions = map[int]int{}
	pids, err := finishCleanupPIDDirs()
	if err != nil {
		return parents, sessions
	}
	for _, p := range pids {
		n := finishCleanupAtoi(p)
		if n <= 0 {
			continue
		}
		parent, pOK := procname.StatFieldString(procname.StatStatPath(procRootName, p), procname.StatFieldParent)
		session, sOK := procname.StatFieldString(procname.StatStatPath(procRootName, p), procname.StatFieldSession)
		if !pOK || !sOK {
			continue
		}
		ppid, pErr := strconv.Atoi(parent)
		sid, sErr := strconv.Atoi(session)
		if pErr != nil || sErr != nil {
			continue
		}
		parents[n] = ppid
		sessions[n] = sid
	}
	return parents, sessions
}

// finishCleanupProcName reports whether a /proc directory entry names a
// process (an all-digits pid) — the scan's filter.
func finishCleanupProcName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ { // len > 0: pid name, never empty
		if !finishCleanupPIDDigits[name[i]] {
			return false
		}
	}
	return true
}

// finishCleanupPIDDirs lists the live per-process directories under /proc
// (numeric names only). A missing/unreadable procfs returns an error — the
// caller degrades to "cannot scan" (fail-soft, never blocks cleanup).
func finishCleanupPIDDirs() ([]string, error) {
	entries, err := os.ReadDir(procRootName)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// A name is a pid only when it is non-empty AND all-digits —
		// finishCleanupProcName's loop would accept "" vacuously, so the
		// emptiness test stays explicit here (Rule 15: no silent misread).
		if n := e.Name(); n != "" && finishCleanupProcName(n) {
			names = append(names, n)
		}
	}
	return names, nil
}

// finishCleanupProcCWD reads one process's CWD via <procRootName>/<pid>/
// <procCwdName>. ("", false) = unreadable (gone / other user) — the caller
// skips it: a process we cannot see is a process we cannot protect.
func finishCleanupProcCWD(pid string) (string, bool) {
	target, err := os.Readlink(filepath.Join(procRootName, pid, procCwdName))
	if err != nil || target == "" {
		return "", false
	}
	return target, true
}

// finishCleanupProcCommand names the CWD holder for the skip reason
// (best-effort: procCommUnknown when unreadable).
//
// IT IS A LABEL, NOT A CRITERION — read this before citing it as one. Row
// 01a17dab recorded that two `gopi chat` SIGINT deaths were caused by
// "finishCleanupProcCommand matching on comm == gopi". The first half of that
// attribution was measured and true (a comm-shaped scan WAS signalled:
// internal/testutil/glm_night_watch_test.go, fixed in 6db5a24); the second half
// is not. This function reads /proc/<pid>/comm, trims it and returns it. It
// decides nothing, grants no ownership and is read by NO signal path: the
// process-signal primitives (procsignal.go) compare INCARNATIONS, never names,
// and contain no call to it (TestCloseoutSignalPathNeverReadsComm fails the
// build if one appears there).
//
// WHAT A SIGNAL IS ADDRESSED BY (epic 01a17a65): the kernel's own statements —
// sid == pid (VETO), the explicit registry (pid + kernel start ticks), a
// registered group verified by its leader's INCARNATION, the ancestor chain, and
// the inherited environment record as AUXILIARY only. Every one of those is
// compared as an incarnation, never as a name: a name cannot tell a live process
// from a recycled number, and the number is what a signal is addressed to.
//
// The callers of THIS function are all readers of a human-readable line:
// run_ground_anchor.go's hold census (via finishCleanupProcRunsGopi, which
// unions comm with the kernel's image link), and the holder refusals below. A
// name is exactly what an operator needs to decide whose process is blocking a
// worktree; it is never what the kernel needs to receive a signal.
//
// finishCleanupHolderWho names a blocking holder for the operator — the whole
// point of the refusal, because the operator has to decide WHOSE process it is
// before touching it.
//
// WHY cmdline + PPID, not comm (measured, 2026-09-26). comm is a 15-character
// kernel field and it is the field the operator was reading — but for a leaking
// TEST FIXTURE it is actively misleading: two re-exec'd test binaries pinning a
// task worktree both read as `signalrec.test`, and a gopi-shaped child reads as
// `gopi`. The operator then goes looking for a `gopi` process, fails to find the
// real culprit, and the worktree stays pinned for 11 hours (the measured
// incident: `python3 …/blackhole.py` pid 1503983). The argv line names the
// artefact that actually leaked, and the PPID separates the two cases that need
// different decisions: PPID 1 is an ORPHAN (nothing will ever reap it), a live
// PPID means a session still owns it.
//
// It degrades honestly: a process whose argv is unreadable (permissions, a
// raced exit) falls back to comm with no PPID claim, never to a blank name.
func finishCleanupHolderWho(pid, cwd string) string {
	who := fmt.Sprintf("pid %s (%s)", pid, finishCleanupProcCommand(pid))
	argv, ok := procname.Cmdline(procRootName, pid)
	if !ok || len(argv) == 0 {
		return who
	}
	who = fmt.Sprintf("pid %s (%s) argv=%q", pid, finishCleanupProcCommand(pid), strings.Join(argv, " "))
	return who + finishCleanupHolderParentSuffix(pid)
}

// finishCleanupHolderParentSuffix renders the holder's parent, and says outright
// when the parent is one of THIS host's orphan reparent targets — the orphan
// shape this epic exists to remove, and the one fact that turns "wait it out"
// into "nothing will ever clean this up".
//
// The test is the MECHANISM (the set), never the literal 1: on this host the
// kernel hands orphans to `systemd --user` (measured: pid 3100), so a holder
// that was reparented there and one reparented to init(1) are the same fact and
// must render the same sentence. Saying "reparented to init" when the target was
// the user manager is what made this line unusable for the fixtures that read it.
func finishCleanupHolderParentSuffix(pid string) string {
	ppid, ok := finishCleanupProcParent(pid)
	if !ok || ppid <= 0 {
		return ""
	}
	targets, err := finishCleanupOrphanReparentTargets()
	if err == nil && targets[ppid] {
		return fmt.Sprintf(" ppid=%d (ORPHAN — reparented to this host's reaper %d, nothing will reap it)", ppid, ppid)
	}
	return fmt.Sprintf(" ppid=%d", ppid)
}

func finishCleanupProcCommand(pid string) string {
	b, err := os.ReadFile(filepath.Join(procRootName, pid, procCommName))
	if err != nil {
		return procCommUnknown
	}
	if name := strings.TrimSpace(string(b)); name != "" {
		return name
	}
	return procCommUnknown
}

// finishCleanupPathUnder reports whether path is dir itself or nested
// inside it (the CWD-INSIDE-the-worktree test, separator-aware so
// "wt-sibling" never matches worktree "wt").
func finishCleanupPathUnder(path, dir string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	dir = filepath.Clean(strings.TrimSpace(dir))
	if path == "" || dir == "" || dir == string(filepath.Separator) {
		return false
	}
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// finishCleanupWorktreeGitDir resolves a worktree's PRIVATE git dir: the
// .git DIR itself (rare, submodule-ish shape) or the "gitdir: <path>"
// pointer a linked worktree keeps in its .git FILE. "" = not resolvable
// (main checkout shape / unreadable) — the guard then covers only the
// worktree directory itself.
func finishCleanupWorktreeGitDir(wt string) string {
	p := filepath.Join(wt, ".git")
	info, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return p
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, finishCleanupGitDirFilePrefix) {
			continue
		}
		g := strings.TrimSpace(strings.TrimPrefix(line, finishCleanupGitDirFilePrefix))
		if g == "" {
			return ""
		}
		if !filepath.IsAbs(g) {
			g = filepath.Join(filepath.Dir(p), g)
		}
		g = filepath.Clean(g)
		if info, err := os.Stat(g); err == nil && info.IsDir() {
			return g
		}
		return ""
	}
	return ""
}

// finishCleanupWorktreeInUse is the worktree-removal LIVENESS guard: it
// reports whether any LIVE process currently works inside wt ("", "" =
// unused → removal may proceed). Two shapes of "in use" are covered —
// nested separators handled, never a sibling-prefix false positive
// (finishCleanupPathUnder):
//
//	(a) CWD inside the worktree DIRECTORY — the gopi session / shell /
//	    `go test` whose working directory IS the worktree (observed
//	    2026-09-10: row 01a088ef, branch feat/reframe-verdict —
//	    `git worktree remove` tore the directory out from under a running
//	    session, surfacing as bogus "package-load" test failures).
//	(b) CWD inside the worktree's PRIVATE git dir — the shape a long-lived
//	    `git` process takes (git chdir's into .git/worktrees/<name> to hold
//	    that worktree's index); removing the worktree deletes that admin
//	    dir too, so its holder counts as in use.
//
// Fail-soft by construction (Rule 15: report, never swallow): the callers
// log the returned reason as the step's Detail/SkipReason. An UNSCANNABLE
// procfs (non-Linux host, restricted /proc) is deliberately NOT a block —
// the guard closes a race, it must not become a hard dependency (Rule 7
// zero-dep intent); the empty result then means "no live holder observed"
// and the caller proceeds on the pre-existing dirty/exempt gates.
// finishCleanupProcAge is the process-age observation the arm branches on. It
// is a package var so the two states the reclaim rule turns on can be pinned
// without waiting ten minutes or owning a broken /proc (the same seam pattern
// as runFinishCleanupChainForTest); production wires it to the /proc read.
// Every OTHER piece of evidence in the decision (cwd, argv[0], the process's
// existence) is read live — only the age is injectable.
var finishCleanupProcAge = finishCleanupProcessAge

// finishCleanupProcessAge reports how long pid has been alive, from the same
// /proc pair the task-probe uses (starttime ticks against /proc/uptime) — an
// unreadable either half reports false, and the caller refuses (an absent
// measurement is never a permission).
func finishCleanupProcessAge(pid int) (time.Duration, bool) {
	ticks, ok := procStartTicks(pid)
	if !ok || ticks <= 0 {
		return 0, false
	}
	b, err := os.ReadFile(filepath.Join(procRootName, procUptimeName))
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, false
	}
	up, perr := strconv.ParseFloat(fields[0], 64)
	if perr != nil {
		return 0, false
	}
	startSec := float64(ticks) / procClockTicksPerSec
	age := time.Duration((up - startSec) * float64(time.Second))
	if age < 0 {
		return 0, false // clock skew between the two reads — not a measurement
	}
	return age, true
}

func finishCleanupWorktreeInUse(wt string) (pidMsg, reason string) {
	wt = strings.TrimSpace(wt)
	if wt == "" {
		return "", ""
	}
	abs, err := filepath.Abs(wt)
	if err != nil {
		abs = filepath.Clean(wt)
	}
	targets := []string{abs}
	// A CWD recorded through a symlinked path (/tmp → /private/tmp style)
	// never string-matches the resolved worktree — compare both shapes.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
		targets = append(targets, resolved)
	}
	// The worktree's private git dir participates when it IS a dir (a
	// file-shaped .git = the linked-worktree pointer, resolved to the admin
	// dir git actually chdir's into).
	if gitDir := finishCleanupWorktreeGitDir(abs); gitDir != "" {
		targets = append(targets, gitDir)
	}
	pids, err := finishCleanupPIDDirs()
	if err != nil {
		return "", "" // cannot scan (non-Linux / restricted procfs) — fail-soft
	}
	// The caller's OWN process tree is not a "live holder" — the close-out
	// runs inside the run that occupied the worktree (see
	// finishCleanupSelfChain). A worktree whose only occupant is the caller
	// IS recyclable: that is the pre-guard behaviour, preserved.
	self := finishCleanupSelfChain()
	for _, pid := range pids {
		cwd, ok := finishCleanupProcCWD(pid)
		if !ok {
			continue // gone / not visible to us
		}
		n, convErr := strconv.Atoi(pid)
		if convErr != nil || !pidAlive(n) {
			continue // a directory without a live process (zombie / raced exit)
		}
		if self[n] {
			continue // this caller's own run — not a competing holder
		}
		inTarget := false
		for _, dir := range targets {
			if finishCleanupPathUnder(cwd, dir) {
				inTarget = true
				break
			}
		}
		if !inTarget {
			continue
		}
		// THE CALLER'S OWN SIDE OF THE PIPELINE (finishCleanupAskFamily): the
		// shell that forked this command forked every stage of it from the same
		// directory, so `… | grep` / `| tee` partners stand in the ground too.
		// They are the caller's diagnostic plumbing, not a competing run — see
		// the criterion's own comment for the four measured facts it requires,
		// and for what it keeps refusing. Asked HERE, only about a process
		// already measured as standing in the target, so the extra /proc reads
		// never run for the whole process table.
		if finishCleanupAskFamily(n) {
			continue
		}
		who := finishCleanupHolderWho(pid, cwd)
		why := fmt.Sprintf("%s by live process %s: cwd %s", finishCleanupInUseMarker, who, cwd)
		// The RECOVERY-side distinction (epic daemon-cwd-escape): a
		// detached gopi daemon is an unrelated immortal holder — no row
		// owns it and it will never exit on its own, so this refusal
		// will NOT clear by waiting the way a task's own session does.
		// The guard still refuses (predicate unchanged); the reason now
		// says which KIND of holder the operator is waiting on.
		if n, convErr := strconv.Atoi(pid); convErr == nil && daemoncwd.IsDaemonProcess(n) {
			if argv0, ok := daemoncwd.ProcessArgv0(n); ok {
				who = fmt.Sprintf("pid %s (%s)", pid, argv0)
				why = fmt.Sprintf("%s by live process %s: cwd %s", finishCleanupInUseMarker, who, cwd)
			}
			why += "; " + fmt.Sprintf(finishCleanupDaemonHolderFmt, daemoncwd.EnvName)
		}
		// TEST-ARTIFACT HOLDER (epic verify-no-verdict-keeps-merge,
		// 2026-09-14): a process standing in the scratch TEST tree is
		// nobody's legitimate holder — it is a leftover of a Go test
		// run (measured: `gopi-merger merger` daemons from an earlier
		// commit's cmd/agent suite, cwd inside a task worktree,
		// GOPI_HOME under the suite scratch root), and it is IMMORTAL,
		// so waiting it out is not an option. The arm therefore
		// converges on AGE: below the floor the holder is taken for a
		// test still in flight (refuse, and say which class of holder
		// this is), at or past it for a leftover (reclaim). A single
		// stray read still cannot delete anything a live test is using,
		// and an UNMEASURABLE age refuses: a missing measurement is
		// never a reclaim.
		if n, convErr := strconv.Atoi(pid); convErr == nil && daemoncwd.IsTestScratchHolder(n) {
			if age, ok := finishCleanupProcAge(n); ok && age >= finishCleanupTestHolderMinAge {
				continue // a leftover of a finished run — this read reclaims
			}
			why += "; " + finishCleanupTestHolderFmt
			why = fmt.Sprintf(why, cwd, finishCleanupTestHolderMinAge)
			return who, why
		}
		return who, why
	}
	return "", ""
}

// finishCleanupTestSpaceRefusal is THE question every push boundary in this
// repository asks before it hands a target to git: "is this a test process
// about to push into something that is NOT its own temp space?" It returns the
// refusal line to record, or "" when the push may run.
//
// The count of `pushfence.Refusal(` call sites IS the coverage of the class:
//   - the close-out chain's deliverable push (this file);
//   - the push-back lane (task_sync_git_pushback.go);
//   - the CLONE CHANNEL's `git push origin HEAD:<branch>` (internal/devworkspace);
//   - the report-back push (cmd/agent, writebackOutcome step 3).
//
// A new `git push` that does not ask it is a boundary the class does not cover,
// which is exactly the defect that produced the measured residue
// (`feat/exists`: an orphan root of a `t <t@t>` fixture, reflog `push`).
func finishCleanupTestSpaceRefusal(target string) string {
	return pushfence.Refusal(target)
}

// finishCleanupPruneStaleWorktrees is the close-out's ONE spelling of `git
// worktree prune -v` (Rule 14: the reclaim arm's repair leg runs the same verb
// through finishCleanupGitRaw, and both callers name the verb and the flag from
// the constants that live beside this file's group — never a literal).
//
// IT IS SAFE FOR THE REASON THE REMOVAL GUARDS EXIST, not despite them. `git
// worktree prune` takes no path, and git prunes a registration only when the
// directory it names is MISSING — so a directory that still exists (any live
// worktree, any ground holding uncommitted work, any foreign row's binding)
// cannot be affected by it. That is why this runs on the leg where the removal
// arm has ALREADY read the directory as gone, and why it needs none of the
// three guards: there is no directory here for them to protect.
//
// The RETURNED STRING is git's own statement ("Removing worktrees/<name>: …"),
// empty when it held no stale registration. Both readings are the caller's to
// render — this function never decides what silence means (Rule 15).
func finishCleanupPruneStaleWorktrees(ctx context.Context, repo string) (string, error) {
	out, err := finishCleanupGitRaw(ctx, repo,
		finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb, finishCleanupWorktreePruneFlag)
	if err != nil {
		return "", fmt.Errorf("git %s %s %s in %s: %w",
			finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb, finishCleanupWorktreePruneFlag, repo, err)
	}
	return out, nil
}

// finishCleanupBranchScratchOnlyNote is the SUCCESS line's honesty half: after
// a branch has been deleted, it reports the case this epic measured — every
// other ref still holding the tip is the pushback lane's per-pass mirror, refs
// that lane DELETES when its pass ends — and hands the operator the ONE command
// that makes the tip durable again.
//
// "" on the ordinary shape (a durable holder keeps the tip, or nothing holds it
// and the orphan guard above has already spoken). The reading is taken from the
// tip captured BEFORE the delete: asking afterwards is asking a question whose
// subject no longer exists.
//
// IT AMENDS NOTHING THAT WAS DECIDED. The step stays OK:true (the ref really was
// deleted, after the guards above allowed it) and no gate is re-asked. What the
// operator loses by omission otherwise is the knowledge that the commit they can
// still see is one fetch away from unreachable — the epic's case ④.
func finishCleanupBranchScratchOnlyNote(ctx context.Context, repo, tip, branch string) string {
	holders := branchDeleteScratchOnlyHolders(ctx, repo, tip, branch)
	if len(holders) == 0 {
		return ""
	}
	return branchDeleteMirrorOnlyNote(branch, holders)
}

// finishCleanupStepSucceeded reports whether the report carries a successful
// step named step (OK:true) — the standing a later step derives from an
// earlier one (step ④ force-deletes a ref only on step ②'s delivery).
func finishCleanupStepSucceeded(rep *FinishCleanupReport, step string) bool {
	if rep == nil {
		return false
	}
	for _, s := range rep.Steps {
		if s.Step == step && s.OK {
			return true
		}
	}
	return false
}

// finishCleanupBranchLiveOwnerReason is the branch half of the ledger-
// liveness gate: "" = no LIVE row (pending/claimed/in_progress) records this
// branch, else the refusal naming the owner. An unreadable ledger REFUSES —
// this exists so a running session's ref is never deleted, so "unknown" must
// keep it (fail-soft: report AND keep, never report AND destroy).
func finishCleanupBranchLiveOwnerReason(ctx context.Context, selfTaskID, branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return finishCleanupBranchLiveOwnerMarker + ": no branch resolved"
	}
	claims, err := finishCleanupLiveClaims(ctx)
	if err != nil {
		return finishCleanupBranchLiveOwnerMarker + ": ledger unreadable (" + err.Error() + ")"
	}
	if o := claims.ownerOfBranch(selfTaskID, branch); o != nil {
		return finishCleanupBranchLiveOwnerMarker + ": row " + o.TaskID + " (" + o.Status +
			finishCleanupOwnerHolderSuffix(o.ClaimedBy) + ")"
	}
	return ""
}

// finishCleanupBranchForceDeleteReason is the delivery half of the branch-
// deletion guard: "" = the sweep's own push landed (④'s delete is then sound —
// the ref's tip is server-held), else the refusal reason. The force flag
// overrides git's "not fully merged" verdict, which is exactly the verdict
// the sweep must not bypass for a ref it did not deliver.
func finishCleanupBranchForceDeleteReason(delivered bool) string {
	if delivered {
		return ""
	}
	return finishCleanupBranchNotDeliveredMarker + ": the push did not land — no force-delete of an undelivered branch"
}

// finishCleanupLiveClaims opens the tasks ledger and loads the live side of
// it — the ONE reader behind every ledger-liveness gate (the worktree gate
// and the branch gate can therefore never disagree on what "a live owner"
// means).
func finishCleanupLiveClaims(ctx context.Context) (*taskLiveClaims, error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return taskLoadLiveClaims(ctx, db)
}

// taskLiveClaim is one LIVE row's ground, on BOTH axes a removal path can
// observe: the worktree path the row recorded, and the branch it works on.
type taskLiveClaim struct {
	TaskID    string
	Status    string
	ClaimedBy string
	Worktree  string // deliverable.worktree ("" while the row has not recorded one yet)
	Branch    string // deliverable.branch ("" when absent)
	// HolderLive is the row's HOLDER verdict (finishCleanupOwnerRowLive): a
	// live-status row whose local claim pid is provably dead is NOT a holder
	// (orphan epic, 2026-09-12), so its ground is not a live session's ground
	// and must not veto a cleanup. Every other live row is a holder here —
	// including a row with no claim stamp at all (unknowable ≠ dead).
	HolderLive bool
}

// taskLiveClaims is the live side of the ledger, loaded ONCE per sweep and
// consulted by every removal path before it touches a worktree directory or
// deletes a branch ref. One loader and one definition of "live"
// (isLiveTaskStatus) for the finish-cleanup arms AND the merged-branch
// sweep, so the two can never drift on what a live row is (Rule 14).
type taskLiveClaims struct {
	rows []taskLiveClaim
}

// taskLoadLiveClaims reads every LIVE task row (pending / claimed /
// in_progress). An unreadable ledger returns the error — the caller decides
// what that means (Rule 15: reported, never swallowed).
func taskLoadLiveClaims(ctx context.Context, db *sql.DB) (*taskLiveClaims, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, claimed_by, deliverable, COALESCE(`+tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks WHERE status IN (`+taskLiveStatusSQLList()+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := &taskLiveClaims{}
	for rows.Next() {
		var id, status, claimedBy, deliverable string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&id, &status, &claimedBy, &deliverable, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		if !isLiveTaskStatus(status) {
			continue
		}
		claims.rows = append(claims.rows, taskLiveClaim{
			TaskID:    id,
			Status:    status,
			ClaimedBy: claimedBy,
			Worktree:  taskDeliverableWorktree(deliverable),
			Branch:    taskDeliverableBranchOrEmpty(deliverable),
			// A row whose status is live while its local holder is provably
			// dead holds nothing (orphan epic): it stays in the loader — the
			// sweep reports it as the dead-local disposition — but it must
			// not veto a directory's cleanup.
			HolderLive: finishCleanupOwnerRowLive(status, claimedBy,
				procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd},
				taskDeliverableWorktree(deliverable)),
		})
	}
	return claims, rows.Err()
}

// taskDeliverableBranchOrEmpty reads a deliverable blob's branch ("") —
// the live-claims loader's own reader, so a malformed blob costs one claim
// axis, never the whole scan.
func taskDeliverableBranchOrEmpty(raw string) string {
	b, err := taskDeliverableBranch(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(b)
}

// ownerOfWorktree returns the LIVE row claiming worktree (canonical path
// comparison) other than excludeTaskID — nil when none does. A live-STATUS
// row whose local holder is provably dead holds nothing (HolderLive,
// orphan epic) and is therefore not an owner: such a row is exactly the
// orphan whose veto stranded its own directory.
func (c *taskLiveClaims) ownerOfWorktree(excludeTaskID, worktree string) *taskLiveClaim {
	if c == nil || strings.TrimSpace(worktree) == "" {
		return nil
	}
	for i := range c.rows {
		if c.rows[i].TaskID == excludeTaskID || c.rows[i].Worktree == "" || !c.rows[i].HolderLive {
			continue
		}
		if finishCleanupSameWorktreePath(c.rows[i].Worktree, worktree) {
			return &c.rows[i]
		}
	}
	return nil
}

// ownerOfBranch returns the LIVE row working ON branch other than
// excludeTaskID — nil when none does. This is the axis that catches the
// shape the worktree path axis cannot: a row that is already working in a
// worktree but has NOT recorded deliverable.worktree yet (the backfill
// lands on the first git-touching round, so a freshly claimed row is
// exactly this shape). The candidate's checked-out branch is observable
// from git whatever the ledger says about paths.
//
// HolderLive is required here too (orphan epic): a live-STATUS orphan whose
// local holder is provably dead must not veto through the branch axis
// either — it was the branch axis that kept gate [c]'s sibling (step ④'s
// branch -d refusal) blocking the dead wrapper's own deliverable.
func (c *taskLiveClaims) ownerOfBranch(excludeTaskID, branch string) *taskLiveClaim {
	branch = strings.TrimSpace(branch)
	if c == nil || branch == "" {
		return nil
	}
	for i := range c.rows {
		if c.rows[i].TaskID == excludeTaskID || c.rows[i].Branch == "" || !c.rows[i].HolderLive {
			continue
		}
		if c.rows[i].Branch == branch {
			return &c.rows[i]
		}
	}
	return nil
}

// liveOwnerSkipReason renders the refusal naming the owning live row, its
// status and (on this host) its holder's liveness.
func liveOwnerSkipReason(o *taskLiveClaim) string {
	return finishCleanupLiveOwnerMarker + ": row " + o.TaskID + " (" + o.Status +
		finishCleanupOwnerHolderSuffix(o.ClaimedBy) + ")"
}

// finishCleanupLiveOwnerGate is the ledger-liveness gate for a candidate
// directory (worktree, already resolved by the caller): "" = no LIVE row
// owns it (the removal may proceed), else the skip reason naming the owner.
// Both axes are asked — the recorded worktree path (a row that already
// recorded its ground) and the branch checked out in it (a row that has not
// recorded one yet — the backfill lands on the row's first git-touching
// round, so a freshly claimed row is exactly this shape).
//
// A ledger that cannot be read reports a reason TOO: this gate exists so a
// live session's ground is never destroyed, so an unknown ledger must SKIP
// rather than guess safe — fail-soft here means report AND keep, never
// report AND destroy.
func finishCleanupLiveOwnerGate(ctx context.Context, worktree, selfTaskID string) string {
	if why := finishCleanupLiveOwnerBlockReason(ctx, worktree, selfTaskID); why != "" {
		return why
	}
	// The surviving half of the veto is a REPORT here (orphan epic): a row
	// whose status is live but whose local holder is provably DEAD no longer
	// refuses (the accessors above skip it), yet the operator must still SEE
	// the orphan — it is the state that used to strand a directory forever,
	// and a human close-out is the only thing that ends the row.
	branch := finishCleanupWorktreeBranch(ctx, worktree)
	if o := finishCleanupDeadLocalHolderRow(ctx, selfTaskID, worktree, branch); o != nil {
		taskNotifyLog("finish cleanup %s: %s — row %s (%s%s) still listed; its directory is not a live ground",
			worktree, finishCleanupDeadLocalHolderMarker, o.TaskID, o.Status, finishCleanupOwnerHolderSuffix(o.ClaimedBy))
	}
	return ""
}

// finishCleanupLiveOwnerBlockReason is the BLOCKING half of the gate on its
// own — the reason a removal must not happen, with no reporting side effect.
// It exists so a caller that must ask the question WITHOUT emitting the orphan
// note can ask the identical question: the removal's own state reader
// (finishCleanupRemovalStateReason) asks it, and both the plan path and the
// applied path go through that reader, so a directory whose ground a live row
// still owns is refused identically whether the run is a prediction or a
// removal (Rule 14: one verdict, one implementation).
func finishCleanupLiveOwnerBlockReason(ctx context.Context, worktree, selfTaskID string) string {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return ""
	}
	// The branch axis is the branch CHECKED OUT IN THE CANDIDATE directory —
	// not the cleaned row's own branch: the row that must be protected is
	// the one working there, and its checked-out branch is exactly what a
	// live row's deliverable.branch names (for a fresh worktree the two
	// agree; for a recycled path it is the live row's branch, not the
	// terminal row's). A detached HEAD has no branch axis — the worktree
	// axis is then the only one, which is why both are asked.
	branch := finishCleanupWorktreeBranch(ctx, worktree)
	claims, err := finishCleanupLiveClaims(ctx)
	if err != nil {
		return finishCleanupLiveOwnerMarker + ": ledger unreadable (" + err.Error() + ")"
	}
	if o := claims.ownerOfWorktree(selfTaskID, worktree); o != nil {
		return liveOwnerSkipReason(o)
	}
	if o := claims.ownerOfBranch(selfTaskID, branch); o != nil {
		return liveOwnerSkipReason(o)
	}
	return ""
}

// finishCleanupDeadLocalHolderRow is the reporting half's lookup — the
// orphaned live-status row naming the candidate ground on either axis.
func finishCleanupDeadLocalHolderRow(ctx context.Context, selfTaskID, worktree, branch string) *taskLiveClaim {
	claims, err := finishCleanupLiveClaims(ctx)
	if err != nil {
		return nil
	}
	return claims.deadLocalHolderExcluding(selfTaskID, worktree, branch)
}

// deadLocalHolderExcluding returns the live-STATUS row whose holder is
// provably dead on this host and which names the candidate directory on
// EITHER axis (the recorded path or the checked-out branch) — nil when none
// does. It is the reporting counterpart of the two owner accessors: those
// skip such a row because it holds nothing, this one finds it because the
// sweep must name it (finishCleanupDeadLocalHolderMarker).
func (c *taskLiveClaims) deadLocalHolderExcluding(excludeTaskID, worktree, branch string) *taskLiveClaim {
	if c == nil {
		return nil
	}
	branch = strings.TrimSpace(branch)
	worktree = strings.TrimSpace(worktree)
	for i := range c.rows {
		r := &c.rows[i]
		if r.TaskID == excludeTaskID || r.HolderLive {
			continue
		}
		if worktree != "" && r.Worktree != "" && finishCleanupSameWorktreePath(r.Worktree, worktree) {
			return r
		}
		if branch != "" && r.Branch == branch {
			return r
		}
	}
	return nil
}

// finishCleanupMissingDirReason reports whether a skip reason is git's refusal
// to ENTER a directory that is not there. git says "cannot change to '<dir>'
// …" and exec says "chdir <dir>: …"; both name the directory, which is why
// the caller also checks the named path (a reason mentioning an unrelated
// directory must never read as settled).
func finishCleanupMissingDirReason(reason string) bool {
	for _, marker := range []string{finishCleanupChdirMarker, finishCleanupCantChangeToMarker} {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

// finishCleanupWorktreeDirAbsent reports whether a RECORDED worktree directory
// is not on disk — the state in which the removal half of the close-out has
// nothing to protect, and therefore nothing to refuse. It is derived from the
// one reading that already names this state (finishCleanupMissingDirReason, the
// marker pair git's own chdir refusal carries) rather than from a second `stat`
// of the same path (Rule 14: one reader, so the two can never disagree about
// what "gone" means).
//
// ONLY the reason that describes this row's own directory reaches here: the
// removal reason is computed against the chain's own ground, and a gone
// directory is exactly what finishCleanupGoneWorktreeSkipReason renders.
func finishCleanupWorktreeDirAbsent(reason string) bool {
	return finishCleanupMissingDirReason(reason)
}

// FinishCleanupSkipVerdict heads the skip line; finishCleanupSkipStepsLabel
// heads the tail FinishCleanupSkipStepTail adds, and the two are spelled as
// constants because the line is read by an operator AND by the regression test.
const (
	FinishCleanupSkipVerdict    = "skipped: "
	finishCleanupSkipStepsLabel = " — steps that DID run in this chain=["
	// finishCleanupSkipSpacer separates a task id from the verdict token on the
	// one-line report. It exists so the TOKEN ITSELF has one spelling (Rule 14):
	// the CLI face joins `id + spacer + verdict`, the agent face joins
	// `verdict + reason` and re-types nothing.
	finishCleanupSkipSpacer = " "
)

// FinishCleanupSkipStepTail renders the steps that ALREADY SUCCEEDED in this
// chain, for the line that reports a SKIP.
//
// THE MEASURED DEFECT IT CLOSES (2026-10-12, task 01a12dc4). A skip reason is
// the state of ONE step, and the line printed it as the state of the WHOLE
// chain. Measured on this host, three times: `01a12e98-c1cd-7042-a64a-a6726187ce16
// skipped: removal skipped (delivery already attempted) …` — printed by the
// very run that had just PUSHED that branch to the peer (the peer ref appeared
// at b2f6a4b6 in the same second, and the local ref the chain had deleted was
// gone). The operator reading the line is told the close-out skipped, while the
// delivery it exists for actually landed; the same line is what the console's
// `task list` close-out signal shows.
//
// It reports ONLY successes on the EXECUTION arm and ONLY predictions on the
// PLAN arm, each under its own label: the reason already quotes the step that
// refused (its Detail), so repeating it would print one fact twice; and a
// prediction printed under "steps that DID run" would be a claim about a run
// that has not happened (Rule 15).
func FinishCleanupSkipStepTail(rep *FinishCleanupReport) string {
	// A PLAN renders nothing: on the dry-run arm a step carrying no OK flag is
	// a PREDICTION, and the plan already prints its predictions under the
	// `would run: ` marker of its own verdict line — a tail here would either
	// repeat them or (read under "steps that DID run") claim a run that never
	// happened (Rule 15).
	if rep == nil || rep.PlanOnly {
		return ""
	}
	ok := make([]string, 0, len(rep.Steps))
	for _, s := range rep.Steps {
		if s.OK {
			ok = append(ok, s.Step)
		}
	}
	if len(ok) == 0 {
		return ""
	}
	return finishCleanupSkipStepsLabel + strings.Join(ok, ",") + "]"
}

// finishCleanupUnreadableWorktreeMarker is the head of the chain's OWN
// terminal-state probe refusal — `git rev-parse --verify HEAD` inside the
// recorded worktree. When the directory is not there the exec failure carries
// git's chdir diagnostic (finishCleanupChdirMarker), so this reason names the
// vanished directory too — but it is composed by the PROBE, never by the
// removal-reason composer (finishCleanupGoneWorktreeSkipReason), which is why
// it names no delivery state and reached the settled classifier's fall-through.
// Its own constant because the classifier READS it (Rules 1b/13).
const finishCleanupUnreadableWorktreeMarker = "worktree unreadable"

// finishCleanupNoDeclaredPeer reports whether this host declares NO sync peer
// at all — the single-node deployment, where a deliverable branch is never
// handed off to any server. It is the ONE reading of that fact inside this
// package: the reason composer (finishCleanupGoneWorktreeNoPeersNote), the
// empty-target composer (noServerTargetSkipReason) and the settled classifier
// all ask HERE, so which host has a delivery channel cannot be answered two
// ways (Rule 14: one criterion, one reader).
func finishCleanupNoDeclaredPeer() bool {
	return !config.HasSyncPeers()
}

const (
	// finishCleanupProcEnvironReadLimit bounds the raw environment read. /proc
	// reports no size up front, so the block is capped rather than sized; far
	// above any real block, and a truncated tail can only hide a marker this
	// package would otherwise have found — which KEEPS the veto (fail-safe).
	finishCleanupProcEnvironReadLimit = 1 << 16

	// finishCleanupProcEnvironSep is the NUL that separates entries in the raw
	// environment block.
	finishCleanupProcEnvironSep = "\x00"

	// finishCleanupOrphanHolderMarker is the phrase the orphan-inheritor
	// RELEASE carries — the sibling of finishCleanupInUseMarker, rendered
	// beside it so every existing consumer of the "worktree in use"
	// convention still matches.
	finishCleanupOrphanHolderMarker = "orphan cwd inheritor"

	// finishCleanupOrphanDescendantMarker is the phrase the DESCENDANT release
	// carries (see finishCleanupReleasedOrphanAncestor). It is a second marker,
	// never a reuse of the one above: the two releases rest on different
	// evidence — a dead launcher RECORD the holder carries itself, versus an
	// ANCESTOR this same sweep released — and a reader must be able to tell
	// which one decided.
	finishCleanupOrphanDescendantMarker = "orphan cwd inheritor descendant"

	// finishCleanupOrphanAncestorWalkMax bounds the ancestry walk of
	// finishCleanupReleasedOrphanAncestor. The released set is small (the
	// sweep's own verdicts), so a few hops reach it; the bound exists so a
	// pathological or recycled ancestry can never turn one verdict into an
	// unbounded /proc read.
	finishCleanupOrphanAncestorWalkMax = 32

	// finishCleanupProcEnvironName is the /proc entry the launcher record is
	// read from. Alias of the identity package's constant (Rules 13/14: one
	// definition for a value several readers share).
	finishCleanupProcEnvironName = procname.EnvironName
	// finishCleanupHolderEnvGround is the run-ground ANNOUNCEMENT a worker's
	// child inherits (GOPI_RUN_GROUND — "which ground is mine?"). Here it is
	// the SECOND reading of the same fact: a leaked child carries, in its
	// exec-time environment, the directory the run that spawned it WORKED in.
	// The value is a path this package may only COMPARE (see
	// finishCleanupSameDirectory) — it is a claim made by the process being
	// classified, never a permit on its own.
	finishCleanupHolderEnvGround = TaskGroundDirEnv
)

// The environment markers are referenced from their ONE definition (Rules
// 13/14), never re-spelled: the executor wrapper pid column lives in this
// package, the agent-chain anchor in internal/config.
const (
	// finishCleanupHolderEnvAnchor is the agent-chain anchor marker. Its VALUE
	// is the exporting gopi process's pid, which is what makes "is there a live
	// gopi process above this one?" answerable without walking a /proc tree
	// (internal/config/agentguard.go documents the three states).
	finishCleanupHolderEnvAnchor = config.AgentSessionAncestorEnv
	// finishCleanupHolderEnvWrapperPID is the executor wrapper's pid, stamped
	// at spawn on a `gopi task run` child and inherited by everything below it.
	finishCleanupHolderEnvWrapperPID = TaskRunWrapperPIDEnv
)

// finishCleanupOrphanReparentTargets returns every pid the kernel hands an
// orphan to on this host (namespace init + any systemd user instance), read from
// the ONE definition of that set (procname.OrphanReparentTargets, Rules 13/14).
// A non-nil error is the "cannot measure" state every caller turns into a HOLD:
// a host whose targets cannot be read classifies NOTHING, because comparing an
// orphan's ppid against an empty set would refuse every genuine orphan — the
// same defect in the opposite direction (Rule 15).
func finishCleanupOrphanReparentTargets() (map[int]bool, error) {
	return procname.OrphanReparentTargets(procRootName)
}

// finishCleanupProcEnviron reads one process's exec-time environment block,
// bounded. ("", false) = unreadable: the process is gone, or it is not ours to
// inspect (a foreign user's /proc/<pid>/environ is EACCES). The caller treats
// both as "no record" → KEEP the veto.
//
// It is a VAR for the same reason finishCleanupProcAge is one: the unreadable
// state cannot be produced on a host whose /proc works, so the seam exists to
// pin it — every other fact in the decision (the pids, the ancestry, the
// session) stays a real live read.
var finishCleanupProcEnviron = func(pid string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(procRootName, pid, finishCleanupProcEnvironName))
	if err != nil {
		return "", false
	}
	if len(b) > finishCleanupProcEnvironReadLimit {
		b = b[:finishCleanupProcEnvironReadLimit]
	}
	return string(b), true
}

// finishCleanupAtoi parses a /proc directory name (finishCleanupProcName has
// already accepted it as all-digits); an unparseable value reads as 0, which
// every caller treats as "not a process" rather than as a guess.
func finishCleanupAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// finishCleanupProcEnvValue extracts the FIRST entry named `name` from a raw
// /proc/<pid>/environ block. ("", false) = the entry is absent (a different
// thing from present-and-empty, which no caller here reads as a record).
//
// One parse for every marker (Rules 13/14): the block is NUL-separated
// `NAME=VALUE`, and the reading is delegated to the FIRST-MATCH rule the
// package documents once — finishCleanupProcEnvValueAll — so "which entry wins"
// is decided in one place (a reader that picked the LAST match would read a
// different value for the very same process).
func finishCleanupProcEnvValue(raw, name string) (string, bool) {
	values := finishCleanupProcEnvValueAll(raw, name)
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// finishCleanupProcEnvValueAll extracts EVERY entry named `name`, in block
// order. It exists for the records whose value is a PATH: a block can carry the
// same name more than once (the holder's own exec-time value, plus every value
// an intermediate launcher exported before it), and "is one of these the
// directory I am classifying?" is a different question from "what is the
// first" — a caller asking the first question must SEE the block, not its first
// match.
func finishCleanupProcEnvValueAll(raw, name string) []string {
	out := []string{}
	for _, entry := range strings.Split(raw, finishCleanupProcEnvironSep) {
		if !strings.HasPrefix(entry, name+"=") {
			continue
		}
		out = append(out, strings.TrimPrefix(entry, name+"="))
	}
	return out
}

// finishCleanupHolderEnvLive reads ONE pid-valued marker from a process's
// exec-time environment and reports whether the process it names is still
// alive. All three states are kept apart, because they answer different
// questions:
//
//	present=true   the process carries the marker at all. A process carrying
//	               NEITHER marker is not classified by this package: nothing
//	               here knows its launcher, so it keeps the veto (the
//	               operator's own shell is that case, and it must keep it).
//	alive=true     the recorded launcher is still running — a LIVE run, so the
//	               veto stays (the positive control).
//	alive=false    the recorded launcher is gone. ONLY now does the caller ask
//	               the session question.
//
// An UNPARSEABLE value is never a death proof (Rule 15a): it reports
// present=true, alive=true, so the caller keeps the veto.
func finishCleanupHolderEnvLive(raw, name string) (present, alive bool) {
	value, ok := finishCleanupProcEnvValue(raw, name)
	if !ok {
		return false, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || pid <= 0 {
		return true, true // recorded but unreadable — not a death proof
	}
	return true, pidAlive(pid)
}

// finishCleanupHolderAnchorLive reads the agent-chain ANCHOR marker, whose
// value is special: internal/config/agentguard.go documents that it carries the
// EXPORTER's pid, so that pid answers "which gopi process launched this" even
// after the exporter has died. The two shapes are read differently from a plain
// pid marker:
//
//	the value PARSES as a pid  → its liveness decides (the same reading the
//	                             wrapper-pid marker gets);
//	the value does NOT parse   → the exporting session is merely NAMED (gopi
//	                             exports opaque session handles on the non-pid
//	                             paths), and there is no pid to test. "An agent
//	                             is above me" is then NOT a death proof, so this
//	                             reports alive=true and the caller keeps the veto.
func finishCleanupHolderAnchorLive(raw string) (present, alive bool) {
	value, ok := finishCleanupProcEnvValue(raw, finishCleanupHolderEnvAnchor)
	if !ok {
		return false, false
	}
	anchor, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || anchor <= 0 {
		return true, true // named, not pid-anchored — never a release
	}
	return true, pidAlive(anchor)
}

// finishCleanupSessionHasLiveGopiExceptTree is finishCleanupSessionHasLiveGopi
// for the ORPHAN-HOLDER question, with the two pids that are not evidence
// removed from the census:
//
//	subject   the cwd-holder itself. It is the process being classified; a
//	          session census that counts it finds "a live gopi in this
//	          session" for EVERY holder, so the rung could only ever answer
//	          TRUE and the release was unreachable (measured 2026-09-23: the
//	          leak below was refused by its own presence).
//	asker     the process asking, and its whole process tree (ancestors ∪
//	          descendants — finishCleanupSelfTree). Same defect the run-ground
//	          sweep already fixed (run_ground_anchor.go:241): the sweep runs
//	          inside SOMEBODY's session, so a census that counted the asker
//	          answered "there is a live gopi here" about any session it was
//	          launched from — and left every leaked child's session held
//	          forever.
//
// What is NOT excluded is the point: a third-party gopi process in the session
// still vetoes, and so does the holder when it LEADS its session (a detached
// daemon, PROJECT.md Rule 24 — a live service wearing the released run's session).
func finishCleanupSessionHasLiveGopiExceptTree(sid, subject int) bool {
	cands, err := finishCleanupSessionGopiCensus(sid, subject, finishCleanupSelfTree())
	if err != nil {
		return true // cannot measure the census — keep the veto
	}
	return finishCleanupSessionCensusHoldsGopi(cands, sid)
}

// finishCleanupSessionHasLiveGopiInExcept is the ONE census body with an optional
// ATTRIBUTION filter: nil dirs is the historical unattributed question (every
// live gopi in the session, minus the asker); a non-empty dirs asks the narrower
// and more honest question "is a live gopi standing in THIS ground?"
// (finishCleanupSessionCensusHoldsGopiIn).
//
// One body, two strengths — never a second /proc walker (Rules 13/14). The
// unmeasurable directions are unchanged: a census that cannot be taken, or one
// with a session key that is not a key, answers TRUE (KEEP), because this
// predicate is only ever asked in order to hold something.
func finishCleanupSessionHasLiveGopiInExcept(sid int, except int, dirs []string) bool {
	cands, err := finishCleanupSessionGopiCensus(sid, except, nil)
	if err != nil {
		return true // cannot measure the census — keep the veto
	}
	if len(dirs) > 0 {
		return finishCleanupSessionCensusHoldsGopiIn(cands, sid, dirs)
	}
	return finishCleanupSessionCensusHoldsGopi(cands, sid)
}

// finishCleanupSessionGopiCensus MEASURES the session census: every live gopi
// process naming session sid, minus `except` (when > 0, the asker) and minus
// every pid in `exclude` (nil = exclude nothing). A non-nil error is the
// "cannot measure" state the callers turn into a KEEP.
//
// The measurement and the decision are separated on purpose (Rules 13/14: one
// reader of /proc for "which live gopi processes name this session"): the
// decision then has one implementation, and a test can stage a census without
// planting a live process.
func finishCleanupSessionGopiCensus(sid, except int, exclude map[int]bool) ([]finishCleanupSessionCand, error) {
	if sid <= 0 {
		return nil, fmt.Errorf("finish-cleanup: session %d is not a usable session key", sid)
	}
	pids, err := finishCleanupPIDDirs()
	if err != nil {
		return nil, err
	}
	cands := make([]finishCleanupSessionCand, 0, len(pids))
	for _, p := range pids {
		n := finishCleanupAtoi(p)
		if n <= 0 || n == except {
			continue
		}
		if exclude[n] {
			continue
		}
		// THE IDENTITY IS THE PROCESS'S, not its name field. comm equality alone
		// is the false-absence family (bug 01a131fc, measured 2026-10-13): a
		// perfectly good gopi binary can be built or renamed (bin/gopi-fix,
		// bin/gopi-test), and a census that only compared comm would drop it.
		// finishCleanupProcRunsGopi is that ONE predicate (image ∪ argv ∪ comm,
		// Rules 13/14), and it errs toward COUNTING a process.
		if !finishCleanupProcRunsGopi(p) {
			continue
		}
		// The session LEADER is included on purpose: a live gopi process that
		// leads the session is still a live gopi process in it (and a held
		// session must never be released on a technicality).
		got, ok := procname.ProcSessionKey(procRootName, p)
		if !ok {
			continue
		}
		// The candidate's OWN evidence, kept so a decision can ask WHERE it works
		// (finishCleanupSessionCensusHoldsGopiIn) instead of re-reading /proc for
		// the same process a second time (Rules 13/14). Both fields degrade to ""
		// on an unreadable entry — a missing measurement, which the attributed
		// decision reads as "not attributable", never as its opposite.
		cwd, _ := finishCleanupProcCWD(p)
		argv, _ := procname.Cmdline(procRootName, p)
		cands = append(cands, finishCleanupSessionCand{
			Pid:    n,
			Sid:    got,
			Alive:  pidAlive(n),
			Leader: n == sid,
			CWD:    cwd,
			Cmd:    strings.Join(argv, " "),
		})
	}
	return cands, nil
}

// finishCleanupSessionCand is one already-measured member of the census: which
// session it names, whether it is still alive, and whether it leads the session
// under question. Splitting the MEASUREMENT (procfs) from the DECISION keeps the
// decision testable without a live process to plant.
type finishCleanupSessionCand struct {
	Pid    int
	Sid    int
	Alive  bool
	Leader bool
	// CWD and Cmd are the candidate's OWN measurements, taken once by the census
	// so an ATTRIBUTED decision (finishCleanupSessionCensusHoldsGopiIn) never has
	// to re-read /proc for a process it already classified. "" = the channel was
	// unreadable, which is a missing measurement and never a match.
	CWD string
	Cmd string
}

// finishCleanupSessionCensusHoldsGopiIn is finishCleanupSessionCensusHoldsGopi
// restricted to the candidates that can be ATTRIBUTED TO ONE GROUND: the live
// gopi process must be standing IN that directory (its cwd, its recorded
// command line, or its image path naming the ground).
//
// WHY AN "IN THIS GROUND" QUESTION EXISTS AT ALL (measured 2026-10-27, bug
// 01a17d00). The unattributed form asks "does the taker's launch SESSION still
// hold a live gopi process?" — a question about a SHELL SESSION, not about the
// ground. The launch session of every run started from an operator's terminal is
// that terminal's session, which this host keeps stocked with gopi processes
// that have nothing to do with any particular ground (measured: session 823212
// held two `gopi worker` rounds while the ground's own taker, pid 2345865, was
// long dead). So the rung answered LIVE for a provably finished ground and kept
// it forever — row 01a17a5f's ground, whose only remaining blocker after the
// fix was the undelivered-commits rung (the honest one).
//
// The attributed form asks the question the rung MEANT to ask: is some gopi
// process standing in THIS directory right now? A worker working this ground has
// its cwd inside it; a bystander in the operator's shell does not.
//
// DIRECTION, unchanged (Rule 15a): every unmeasurable candidate answers "not
// attributable" — it does not RELEASE anything, because this predicate is only
// ever consulted to decide whether a KEEP is warranted. The rungs that can
// actually free a ground (ledger life-ended, undelivered commits, age floor,
// occupancy) all run after it, and the occupancy guard
// (finishCleanupWorktreeInUse) is the independent veto that still refuses a
// directory any live process is standing in, whoever its image is.
func finishCleanupSessionCensusHoldsGopiIn(cands []finishCleanupSessionCand, sid int, dirs []string) bool {
	for _, c := range cands {
		if !c.Alive {
			continue
		}
		if c.Sid != sid && !c.Leader {
			continue
		}
		if finishCleanupCandInDirs(c, dirs) {
			return true
		}
	}
	return false
}

// finishCleanupCandInDirs asks whether one census candidate is attributable to
// one of dirs, through the three channels that can name a ground:
//
//	① cwd     — where it works RIGHT NOW (the strongest: a worker stands in the
//	            ground it is building).
//	② cmdline — the ground's path appears in its argv (measured shape: the
//	            channel spells the directory it clones/enters).
//	③ image   — its resolved /proc/<pid>/exe link names the directory (a build
//	            started from inside the ground).
//
// "" in any channel is a MISSING measurement and matches nothing (Rule 15a): an
// unreadable candidate cannot become evidence of a live holder.
func finishCleanupCandInDirs(c finishCleanupSessionCand, dirs []string) bool {
	if c.CWD != "" && finishCleanupCwdInAnyDir(c.CWD, dirs) {
		return true
	}
	if c.Cmd != "" {
		for _, dir := range dirs {
			if strings.Contains(c.Cmd, dir) {
				return true
			}
		}
	}
	pid := strconv.Itoa(c.Pid)
	if pid != "0" && procRootName != "" {
		if target, err := os.Readlink(filepath.Join(procRootName, pid, procname.ExeName)); err == nil && target != "" {
			if finishCleanupCwdInAnyDir(target, dirs) {
				return true
			}
		}
	}
	return false
}

// finishCleanupSessionCensusHoldsGopi is the decision: does session sid hold a
// live gopi process other than the ones the caller excluded? A candidate counts
// only when it names this session AND is alive; an ALIVE session LEADER counts
// even on a session-key mismatch (the leader is in its own session by
// construction, and a technicality must never release a held session).
//
// An empty census is the ordinary "nothing holds it" answer, so it returns
// false — the caller has already established that the SESSION itself is alive
// (an id that answers kill(sid, 0) is a real, currently-used session).
func finishCleanupSessionCensusHoldsGopi(cands []finishCleanupSessionCand, sid int) bool {
	for _, c := range cands {
		if !c.Alive {
			continue
		}
		if c.Sid == sid || c.Leader {
			return true
		}
	}
	return false
}

// finishCleanupOrphanCwdInheritorReason reports whether EVERY live process
// whose cwd is inside wt is an ORPHANED cwd-inheritor that belongs to no live
// run — the condition under which a refusal must NOT be treated as "the
// directory is held".
//
// It is a second pass, deliberately: the refusal path is the one that has a
// holder to classify, and a separate walk keeps the primary guard's loop (and
// its measured semantics) untouched. Callers invoke it ONLY on refusal.
//
// Returns ("", "") when even ONE holder is a genuine one — the veto then stands
// with the primary guard's own sentence.
func finishCleanupOrphanCwdInheritorReason(wt string) (pidMsg, reason string) {
	wt = strings.TrimSpace(wt)
	if wt == "" {
		return "", ""
	}
	abs, err := filepath.Abs(wt)
	if err != nil {
		abs = filepath.Clean(wt)
	}
	// The SAME target set the primary guard judges against (Rule 14: one
	// definition of "inside the worktree" — rebuilt from the same helper, never
	// a second path comparison).
	targets := []string{abs}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
		targets = append(targets, resolved)
	}
	if gitDir := finishCleanupWorktreeGitDir(abs); gitDir != "" {
		targets = append(targets, gitDir)
	}
	reparentTargets, err := finishCleanupOrphanReparentTargets()
	if err != nil {
		return "", "" // cannot measure the reparent targets — the primary guard already decided
	}
	pids, err := finishCleanupPIDDirs()
	if err != nil {
		return "", "" // cannot scan — the primary guard already decided
	}
	self := finishCleanupSelfChain()
	// RELEASED-ANCESTOR PASS FIRST, and it is what makes the rest honest. The
	// acceptance below (held==0) is all-or-nothing over the worktree's holders,
	// and the leaked-tree shape defeats it: the ladder releases the parent
	// orphan, then stops on a child the parent spawned (the child is not
	// reparented to init, so rung 2 refuses it, and it carries no launcher record
	// of its own, so rung 3 refuses it too) — one unclassifiable leftover pins a
	// directory whose whole remaining occupancy is the wreckage of a run that is
	// already gone. Measured 2026-09-25, row 01a0d5d2's ground: parent
	// `bash … while …; sleep 10` inheritor=true, its live `sleep` child
	// inheritor=false ⇒ the classifier answered "" (refusal) on a worktree that
	// is clean, owned by no row, inside a declared root, and held by nothing but
	// a dead run's leftovers.
	//
	// The pass reads each holder ONCE and remembers who was released, so the
	// pedigree is the sweep's own finding (never a second liveness reading that
	// could disagree with it).
	type orphanHolderVerdict struct {
		pid       int
		cwd       string
		pidMsg    string
		reason    string
		inheritor bool
	}
	var verdicts []orphanHolderVerdict
	released := map[int]bool{}
	for _, pid := range pids {
		cwd, ok := finishCleanupProcCWD(pid)
		if !ok {
			continue
		}
		n := finishCleanupAtoi(pid)
		if n <= 0 || !pidAlive(n) || self[n] {
			continue
		}
		if !finishCleanupCwdInAnyDir(cwd, targets) {
			continue
		}
		raw, _ := finishCleanupProcEnviron(pid)
		who, why, inheritor := finishCleanupClassifyOrphanHolder(pid, cwd, raw, reparentTargets)
		if pidMsg == "" {
			pidMsg, reason = who, why
		}
		if inheritor {
			released[n] = true
		}
		verdicts = append(verdicts, orphanHolderVerdict{pid: n, cwd: cwd, pidMsg: who, reason: why, inheritor: inheritor})
	}
	free, held := 0, 0
	for _, v := range verdicts {
		if v.inheritor {
			free++
			continue
		}
		ancestor := finishCleanupReleasedOrphanAncestor(strconv.Itoa(v.pid), released)
		if ancestor <= 0 {
			held++
			continue
		}
		// The release rests on the pedigree, and the session census is asked
		// anyway: a descendant that forked into a session of its own (a
		// self-daemonising leftover) is exactly the case whose ancestor's census
		// says nothing about it, so the veto is re-asked on the SAME channel the
		// ladder uses (Rules 13/14) rather than assumed to hold.
		sid, sessionOK := procname.ProcSessionKey(procRootName, strconv.Itoa(v.pid))
		if !sessionOK || finishCleanupSessionHasLiveGopiExceptTree(sid, v.pid) {
			held++
			continue
		}
		// The release is the one this verdict already qualifies for: an ancestor
		// of this very sweep was classified as a released orphan, so this holder
		// is the leftover of a launcher that is gone. It is NOT a second,
		// weaker release — every rung of the ladder still ran, and it is their
		// refusal that this evidence answers.
		if pidMsg == "" {
			pidMsg = v.pidMsg
		}
		if reason == "" {
			reason = finishCleanupOrphanDescendantReason(ancestor, sid, v.cwd)
		}
		free++
	}
	if held > 0 || free == 0 {
		// A genuine holder is present (or there was nothing to release after
		// all): the refusal stands and the caller renders the primary guard's
		// sentence.
		return "", ""
	}
	return pidMsg, reason
}

// finishCleanupClassifyOrphanHolder decides ONE cwd-holder: is it an orphaned
// cwd-inheritor (true), or a holder the veto must keep (false)? It returns the
// holder's name and, for the release direction, the rendered reason.
//
// The ladder, in order, each rung a kernel-owned (or single-writer) fact:
//
//  1. unreadable ancestry                          → HOLD.
//  2. not reparented to the live init              → HOLD. A launcher that is
//     still alive has not released its child; this is the POSITIVE CONTROL.
//  3. carries no gopi launcher record              → HOLD. The process was not
//     launched by a gopi run: the operator's own shell inside the worktree is
//     that case, and it keeps refusing.
//  4. a recorded launcher that is STILL ALIVE      → HOLD.
//  5. the run-ground record names a directory that is NOT this one → HOLD.
//     The holder announces GOPI_RUN_GROUND ("which ground is mine?"): a process
//     that took a DIFFERENT ground is a run standing somewhere else, so its
//     presence here is not evidence that THIS directory is free. Any spelling
//     that names this directory counts as a match — the comparison is
//     structural, because A PROCESS EDITING FILES IN THE WORKTREE HAS NO OTHER
//     RECORD OF WHAT IT IS DOING (its cwd and its argv are unchanged whatever
//     it is up to), so this rung is the only claim available to a working
//     process and it is read as one: a holder that makes a DIFFERENT claim, or
//     makes none, keeps a veto this rung would otherwise have to keep by hand.
//  6. a live gopi process in the same session      → HOLD (a detached run
//     still owns its session). The census excludes exactly two pids that are
//     not evidence — the holder itself (a gopi process standing in its own
//     session would answer its own question: measured 2026-09-24, the leak
//     below was refused by its own presence) and the ASKER'S OWN TREE (the
//     sweep's worker/agent children wear the sweep's session, and they are not
//     a foreign holder: measured 2026-09-24, session 23345 held both the leak
//     and the asking worker chain). Nothing else is excluded: a third-party
//     gopi process keeps the veto.
//  7. otherwise                                    → the directory was
//     INHERITED and nobody is left → RELEASE.
//
// THE RECORD THAT MATCHES LAST, and the two it does not match — the residual,
// stated rather than hidden:
//
//	(a) a leaked run-child whose cwd is inside ANOTHER run's worktree and whose
//	    GOPI_RUN_GROUND names a THIRD directory keeps its veto forever (rung 5),
//	    because no process records "I am finished" (GOPI_TASK_RUN_OUTCOME is
//	    stamped at spawn, before the run works — measured, cli_tasks.go:2217);
//	(b) a leaked child that inherited NO ground record and whose session was
//	    left with no live gopi process is released (rungs 3 and 6 do not stop
//	    it), which is the intent for a bare leftover but is a class we cannot
//	    separate from a live `gopi web` in the same shape;
//	(c) a leaked child whose process LEADS its session is released too, and
//	    that is deliberate: the epic's own measured leftover class
//	    (briefings/brief-worktree-reclaim-two-gaps.md, 2026-09-23) is
//	    setsid-orphaned Xvfb/servers pinned in worktrees, so a "session leader
//	    holds the veto" rung would re-break the defect Gap A fixed. The
//	    daemon/non-daemon separation therefore rests on rung 5 and on the
//	    session census, never on leadership alone.
//
// The remedy for (a)/(b) belongs to the RUN, not to this classifier: reap the
// run's own children at close-out, so no bystander is left to classify. That
// half is named in the report as not done here.
func finishCleanupClassifyOrphanHolder(pid, cwd, raw string, reparentTargets map[int]bool) (who, why string, inheritor bool) {
	who = "pid " + pid + " (" + finishCleanupProcCommand(pid) + ")"
	ppid, ok := finishCleanupProcParent(pid)
	if !ok {
		return who, "", false // unreadable ancestry — never classified
	}
	// RUNG 2, and it is a test of MECHANISM, not of a value: the holder's parent
	// must be a process the kernel hands orphans TO on this host. "== init(1)"
	// was that test spelled as one host's answer — on a systemd user session the
	// target is `systemd --user` (measured: pid 3100 here), so the literal form
	// refused every genuine orphan and made this whole family of fixtures SKIP.
	// An EMPTY set is the unmeasurable case and holds the veto (never a release).
	if len(reparentTargets) == 0 || !reparentTargets[ppid] {
		return who, "", false // launcher alive (or targets unmeasurable): HOLD
	}
	wrapperPresent, wrapperAlive := finishCleanupHolderEnvLive(raw, finishCleanupHolderEnvWrapperPID)
	anchorPresent, anchorAlive := finishCleanupHolderAnchorLive(raw)
	if !wrapperPresent && !anchorPresent {
		return who, "", false // not a gopi-launched process: HOLD
	}
	if wrapperAlive || anchorAlive {
		return who, "", false // its launcher is still running: HOLD
	}
	sid, sessionOK := procname.ProcSessionKey(procRootName, pid)
	if !sessionOK {
		return who, "", false // unmeasurable session: HOLD
	}
	// NOTE: leadership is deliberately NOT a rung here. The epic's measured
	// leftover class (setsid-orphaned Xvfb/servers pinned in worktrees,
	// 2026-09-23) is exactly a session LEADER, so a "leader holds the veto"
	// rung would re-break the defect this file was written for; the daemon
	// separation rests on the ground rung and on the session census instead.
	if ground, recorded := finishCleanupProcEnvValue(raw, finishCleanupHolderEnvGround); recorded {
		if !finishCleanupGroundMatchesHolder(ground, cwd, raw) {
			return who, "", false // it announces a DIFFERENT ground: a live run elsewhere: HOLD
		}
	}
	if finishCleanupSessionHasLiveGopiExceptTree(sid, finishCleanupAtoi(pid)) {
		return who, "", false // a third-party live gopi shares the session: HOLD
	}
	record := finishCleanupHolderEnvAnchor
	if wrapperPresent {
		record = finishCleanupHolderEnvWrapperPID
	}
	return who, finishCleanupOrphanHolderReason(who, record, sid, cwd), true
}

// finishCleanupGroundMatchesHolder asks whether the process's OWN run-ground
// announcement names the directory it is standing in. `first` is the block's
// first GOPI_RUN_GROUND value, `raw` the whole block: the record can appear more
// than once (see finishCleanupProcEnvValue), and the holder's own exec-time
// value is only sometimes the first — a launcher that exported the pair for its
// child puts the CHILD's value last. ANY spelling naming this directory is
// therefore a match (and none naming it is a HOLD).
//
// A return of false is never a release on its own: it is one rung of the ladder,
// and the rungs after it (the session census) still have to agree.
func finishCleanupGroundMatchesHolder(first, cwd, raw string) bool {
	if finishCleanupSameDirectory(first, cwd) {
		return true
	}
	for _, ground := range finishCleanupProcEnvValueAll(raw, finishCleanupHolderEnvGround) {
		if finishCleanupSameDirectory(ground, cwd) {
			return true
		}
	}
	return false
}

// finishCleanupSameDirectory reports whether two directory spellings name the
// SAME directory. It is structural (rule 14: one implementation of "the same
// place"), never a string comparison of two independent spellings:
//
//	symlinks are resolved (EvalSymlinks) so a recorded /tmp/... and a read
//	         /private/tmp/... agree, and
//	the identity is COMPARED AS A CLEANED PATH, never canonicalised beyond
//	         that: an unresolvable component would otherwise read as a
//	         mismatch and turn a genuinely recycled directory into a veto.
//
// An empty side is never a match: no record, or an unreadable cwd, is not a
// claim about a directory.
func finishCleanupSameDirectory(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return finishCleanupResolveDir(a) == finishCleanupResolveDir(b)
}

// finishCleanupResolveDir returns the comparison form of one directory path:
// symlinks resolved when that is possible, cleaned always. An unreadable /
// nonexistent path keeps its cleaned spelling (the component that does not
// exist cannot be resolved, and the caller compares the rest).
func finishCleanupResolveDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(dir)
}

// finishCleanupOrphanHolderReason renders the ONE release sentence. The
// finishCleanupInUseMarker phrase leads it on purpose: the release is a
// refinement of the guard's own reading of a directory it WOULD have called "in
// use", and every existing consumer of that convention keeps matching.
func finishCleanupOrphanHolderReason(who, record string, sid int, cwd string) string {
	return finishCleanupInUseMarker + ": released " + finishCleanupOrphanHolderMarker + " " + who +
		" (launcher record " + record + " is gone and session " + strconv.Itoa(sid) +
		" holds no live gopi process): cwd " + cwd +
		" was only INHERITED before the launcher died"
}

// finishCleanupCwdInAnyDir asks whether a process's cwd lies in one of the
// directories the caller judges against. One implementation of "inside" (Rules
// 13/14): the identical finishCleanupPathUnder comparison the per-holder loop
// already used, lifted so the resting and the new rung cannot drift apart.
func finishCleanupCwdInAnyDir(cwd string, dirs []string) bool {
	for _, dir := range dirs {
		if finishCleanupPathUnder(cwd, dir) {
			return true
		}
	}
	return false
}

// finishCleanupReleasedOrphanAncestor reports the pid of a RELEASED orphan
// cwd-inheritor standing above pid in its ancestry, or 0 when there is none.
//
// WHY (measured 2026-09-25, epic worktree-not-removed). A leaked run child is
// often a TREE, not one process, and the directory-level verdict is
// all-or-nothing over its cwd-holders: ONE leftover the ladder cannot classify
// by itself pins the worktree forever. The measured specimen — the finished row
// 01a0d5d2's ground was held by `bash -c … while pgrep …; sleep 10` (correctly
// released by the rung above: its recorded wrapper pid was long dead) TOGETHER
// WITH its live child. The child's launcher is that orphan itself, so it is not
// reparented to init and carries no record of the ladder reads: the ladder
// stopped on it, refused, and the pass left the directory pinned. Measured
// against the live tree with the real classifier, holder by holder: the parent
// reads inheritor=true, the child inheritor=false.
//
// ANCESTRY IS THE EVIDENCE, and only ancestry: the question is "was this
// process started by a run that is already gone?", which is exactly what an
// ancestor already classified as a released orphan answers. It is read from
// /proc (kernel-owned), never from argv or from a self-declared environment
// value.
//
// The walk is bounded and cycle-safe: an unreadable parent ends it (an
// unmeasurable ancestry is never a release — the rule the ladder's own first
// rung keeps), and init is never an ancestor in the sense meant here: it is
// where reparenting PUTS a process, not who launched it.
func finishCleanupReleasedOrphanAncestor(pid string, released map[int]bool) int {
	if len(released) == 0 {
		return 0
	}
	chain := map[string]bool{pid: true}
	for step := 0; step < finishCleanupOrphanAncestorWalkMax; step++ {
		ppid, ok := finishCleanupProcParent(pid)
		if !ok || ppid <= 0 {
			return 0
		}
		if released[ppid] {
			return ppid
		}
		key := strconv.Itoa(ppid)
		if chain[key] {
			return 0 // a cycle cannot be walked further, and must not be
		}
		chain[key] = true
		pid = key
	}
	return 0
}

// finishCleanupOrphanDescendantReason renders the release sentence for a holder
// the ladder could not classify alone whose ANCESTOR this same sweep released.
// It leads with the same finishCleanupInUseMarker phrase as its sibling so every
// existing consumer of the "worktree in use" convention keeps matching, and it
// NAMES the ancestor — the evidence — rather than asserting that no holder
// exists.
func finishCleanupOrphanDescendantReason(ancestor, sid int, cwd string) string {
	return finishCleanupInUseMarker + ": released " + finishCleanupOrphanDescendantMarker +
		" (spawned by pid " + strconv.Itoa(ancestor) +
		", which this same sweep released as an " + finishCleanupOrphanHolderMarker +
		", and session " + strconv.Itoa(sid) +
		" holds no live gopi process): cwd " + cwd +
		" was only INHERITED before the launcher died"
}

// finishCleanupRunIdentityMarker is the phrase the refusal carries — the ONE
// spelling a test, the removalaudit evidence and the report's reader match
// (Rule 1b/13: never a re-typed literal).
const finishCleanupRunIdentityMarker = "ground still stood on by a live run"

// finishCleanupRunIdentityEvidenceFmt renders the GROUND's own record inside the
// refusal: the task the ground was taken for, the taker's pid and the launch
// session, and the ladder's reason — so the operator can see which run is
// standing there without reading the audit sink by hand.
const finishCleanupRunIdentityEvidenceFmt = " (ground taken for task %s: pid %s, session %s — %s)"

// finishCleanupRunIdentityNoMoveNote is the refusal's consequence sentence: the
// ARM that asked was stopped before its act, so nothing was removed and the ref
// this step owns was not moved.
//
// THE WORDING IS MEASURED, NOT RHETORICAL (the SL3-2 pushback scope check,
// 2026-09-26). The sentence used to read "nothing was removed and NO REF was
// moved" — and the Rule 31 run of the real chain shows why that overclaims: the
// guard is asked at ③, which runs AFTER the delivery half, and the delivery
// half's own alignment legitimately moves the DELIVERED branch (its trial merge
// / its doc-class commit) and the pushback core mirrors the peer's master into
// its own scratch namespace (`refs/remotes/gitPushcheck/…`,
// gitPushbackCheckRootRef). So by the time the guard speaks, a ref HAS moved. A
// refusal line that said otherwise would be a false statement on an operator's
// screen — Rule 15's silent failure, inverted. What is true, and is what this
// says, is the part an operator acts on: the ground was not removed and the ref
// THIS step owns was not moved.
const finishCleanupRunIdentityNoMoveNote = "nothing was removed and this step moved no ref"

// finishCleanupRunIdentityRefusal is the guard itself: "" = the ground's own
// record does not prove a run is standing on it (the arm proceeds), else the
// NAMED refusal.
//
// It is asked by every arm's removal path and by the branch step, and it reads
// the candidate directory's anchor — the record the TAKER wrote. A directory
// with no anchor answers "" (nothing measured is not a refusal: an anchor is
// written at acquisition by the seams that take a ground, and a directory no
// seam ever took has no taker to protect).
func finishCleanupRunIdentityRefusal(worktree, branch string) string {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return ""
	}
	a, ok := finishCleanupGroundAnchorFor(worktree)
	if !ok {
		return ""
	}
	// THE SELF-LINEAGE EXCLUSION, and it is load-bearing (measured on this very
	// run, 2026-09-26): the anchor's taker is the run's WRAPPER, which is an
	// ANCESTOR of the process executing the close-out — so without this exclusion
	// every legitimate close-out would refuse its OWN ground and no worktree in
	// the fleet would ever be freed. The taker pid is therefore compared against
	// the caller's own chain (finishCleanupSelfChain — the ONE reading of "who is
	// not a third party here", the same exclusion finishCleanupWorktreeInUse and
	// the orphan ladder use; Rule 14).
	//
	// The exclusion is the UPWARD chain ONLY, deliberately: the downward closure
	// (finishCleanupSelfTree) is the widening this tree measured as HARMFUL — a
	// live runner that is a DESCENDANT of the asker must keep its veto, so a
	// descendant taker stays a foreign run here.
	if taker := finishCleanupAtoi(strings.TrimSpace(a.PID)); taker > 0 {
		if self := finishCleanupSelfChain(); self[taker] {
			return ""
		}
	}
	live := RunGroundAnchorLiveness(a)
	if !live.Live {
		return ""
	}
	task := strings.TrimSpace(a.TaskID)
	if task == "" {
		task = "(unnamed)"
	}
	return finishCleanupRunIdentityMarker + ": worktree " + worktree +
		" belongs to a live run — not freeing it" +
		"\n" + finishCleanupRunIdentityRefusalLine(a, live, branch)
}

// finishCleanupRunIdentityRefusalLine renders the two lines an operator needs:
// the evidence (who is standing there, in the record's own words) and the
// consequence (nothing was removed and no ref was moved, plus the door).
func finishCleanupRunIdentityRefusalLine(a RunGroundAnchor, live RunGroundLiveness, branch string) string {
	var b strings.Builder
	b.WriteString("  evidence: the ground's own anchor record says its taker is still standing")
	b.WriteString(fmt.Sprintf(finishCleanupRunIdentityEvidenceFmt, a.TaskID, a.PID, a.Session, live.Reason))
	b.WriteString("\n  " + finishCleanupRunIdentityNoMoveNote)
	if br := strings.TrimSpace(branch); br != "" {
		b.WriteString(" (branch " + br + " is untouched)")
	}
	b.WriteString("; the owning run's own close-out is what frees this ground")
	return b.String()
}

// finishCleanupGroundAnchorFor reads the anchor for one directory out of the
// SAME sink and with the SAME path comparison the rest of the package uses
// (Rules 13/14: taskGroundAnchorSinkReader is the one reader, sameWorkingPath
// the one directory identity).
//
// The sink is resolved through the package's own resolver, so the env override
// the audit package honours applies here too — a test that points the sink at a
// fixture gets this reader pointed at the same file, with no second spelling.
func finishCleanupGroundAnchorFor(dir string) (RunGroundAnchor, bool) {
	path, err := TaskGroundAnchorPath()
	if err != nil {
		return RunGroundAnchor{}, false
	}
	anchors, err := RunGroundAnchors(path)
	if err != nil {
		return RunGroundAnchor{}, false
	}
	// NEWEST LAST in file order, and the first anchor for a directory is the one
	// that names its taker (TaskGroundAnchorRecord's dedup) — so the FIRST match
	// is the record, and a later one could only be a re-take.
	for _, a := range anchors {
		if sameWorkingPath(a.Worktree, dir) {
			return a, true
		}
	}
	return RunGroundAnchor{}, false
}

// The rescue's vocabulary: one spelling per value (Rules 1b/13/14).
const (
	// finishCleanupSalvageRefPrefix is the ref namespace a rescue lives in.
	// It is deliberately NOT under refs/heads or refs/tags: no branch walk, no
	// tag, no push refspec and no merge path reads refs/gopi/, so a rescue is
	// addressable and invisible to every delivery mechanism at once.
	finishCleanupSalvageRefPrefix = "refs/gopi/residue/"

	// finishCleanupSalvageIndexEnv is git's own variable for "use THIS index
	// file" — the whole mechanism by which property 2 (no mutation) is
	// satisfied.
	finishCleanupSalvageIndexEnv = "GIT_INDEX_FILE"

	// finishCleanupSalvageDirName is the rescue's container INSIDE the run's
	// session directory (<gopi-dir>/sessions/<taskID>/salvage/). Not a new
	// root: the session directory is the one location a run's own artifacts
	// already own, so the rescue inherits its lifecycle instead of inventing
	// a second one (Rule 35).
	finishCleanupSalvageDirName = "salvage"

	// finishCleanupSalvageIndexName is the throwaway index file's name inside
	// that container.
	finishCleanupSalvageIndexName = "index"

	// finishCleanupSalvageBundleName is the tar bundle's name. One name per
	// row, like the ref: a second close-out replaces it (property 3).
	finishCleanupSalvageBundleName = "residue.tar"

	// finishCleanupSalvageStoreName is the PERSISTENT repository a CLONE
	// ground's rescue is mirrored into, inside that same container
	// (<session>/salvage/store.git). It is a bare repository — nothing is
	// checked out, nothing ticks, no second working tree exists (Rule 35: no
	// resident artifact; it is the session directory's own content, reclaimed
	// with it).
	//
	// WHY IT IS NEEDED AT ALL, and it is the defect this constant answers: a
	// rescue ref written with `git -C <ground> update-ref` lives in the
	// GROUND's object database. For a LINKED worktree that is the shared repo,
	// so `git worktree remove` leaves it alone — but a CLONE ground is a
	// repository of its own (`.git` is a DIRECTORY), and its removal
	// (`devworkspace.Reclaim` → `os.RemoveAll`) takes the ref AND the objects
	// with it. The rescue was then an artifact that could not outlive the
	// deletion it licensed: "retrievable" was true only while the directory the
	// rescue existed to release was still there.
	finishCleanupSalvageStoreName = "store.git"

	// finishCleanupSalvageInitVerb / BareFlag / FetchVerb / MirroredFmt are the
	// git spellings the publication leg runs (Rule 1b: one spelling per value,
	// and the tests address them by name).
	finishCleanupSalvageInitVerb  = "init"
	finishCleanupSalvageBareFlag  = "--bare"
	finishCleanupSalvageFetchVerb = "fetch"

	// finishCleanupSalvageMirroredFmt is the half of the record that only
	// exists for a CLONE ground: the retrieval handle of the copy that
	// SURVIVES the removal.
	finishCleanupSalvageMirroredFmt = "; MIRRORED into the persistent repository %s (the ground is a CLONE: removing it takes its own refs and objects) — retrieve with `git -C %s ls-tree -r %s`"

	// finishCleanupSalvageIdentityName / Email are the identity a rescue commit
	// is authored/committed with. The rescue must not depend on the run's git
	// config existing in the rescued worktree (a fixture, a fresh clone, a
	// stripped config all lack it) — the identity is passed per invocation.
	finishCleanupSalvageIdentityName  = "gopi"
	finishCleanupSalvageIdentityEmail = "gopi@gopi"

	// finishCleanupSalvageMessageFmt is the rescue commit's subject: the row
	// it belongs to and the branch that was being delivered. A reader who finds
	// the ref with `git log refs/gopi/residue/...` learns both without the ledger.
	finishCleanupSalvageMessageFmt = "salvage: residue of run %s (delivering %s)"

	// finishCleanupIgnoredPrefix is git's own porcelain marker for an ignored
	// entry (`--ignored=matching`). An entry under it is NOT in the rescue's
	// tree object — `git add -A` honours .gitignore — which is precisely the
	// gap the tar bundle exists to close.
	finishCleanupIgnoredPrefix = "!! "

	// finishCleanupSalvageIgnoredCap bounds how many ignored entries the bundle
	// carries. Past the cap the entries are still NAMED in the record (property
	// 1: the loss is never silent) — the bound exists so a worktree holding a
	// dependency tree cannot turn one close-out into an unbounded copy (Rule 1:
	// the bound is named, not implied).
	finishCleanupSalvageIgnoredCap = 64

	// finishCleanupSalvageReadTreeVerb / IndexVerb / WriteTreeVerb /
	// CommitTreeVerb / UpdateRefVerb are the git spellings the rescue runs.
	// Each is declared once because the rescue's report and its tests address
	// them by name.
	finishCleanupSalvageReadTreeVerb   = "read-tree"
	finishCleanupSalvageAddVerb        = "add"
	finishCleanupSalvageWriteTreeVerb  = "write-tree"
	finishCleanupSalvageCommitTreeVerb = "commit-tree"
	finishCleanupSalvageUpdateRefVerb  = "update-ref"
	finishCleanupSalvageParentFlag     = "-p"
	finishCleanupSalvageMessageFlag    = "-m"
	finishCleanupSalvageAllFlag        = "-A"

	// finishCleanupSalvageStatusIgnored is git's ignored-entry listing flag —
	// `git status --porcelain --ignored=matching` is the ONE reading both the
	// classification (core porcelain) and the ignored class come from, so the
	// rescue and the refusal arms can never disagree about what is in the tree.
	finishCleanupSalvageStatusIgnored = "--ignored=matching"
)

// finishCleanupSalvageResult is what a rescue FOUND and MADE: the record the
// close-out writes to the row's trail (property 1). Every field is evidence —
// a rescue that produced nothing (a clean tree, or a failure) leaves Refs and
// Bundle empty and the caller keeps the refusal.
type finishCleanupSalvageResult struct {
	// Ref is the full ref name the rescue commit was written to ("" = none).
	Ref string
	// Commit is the rescue commit's object id.
	Commit string
	// Entries is the rescue commit's tree listing, one path per line — the
	// answer to "what was rescued" without running any command.
	Entries []string
	// Ignored names the entries git ignores (present in the worktree, absent
	// from the rescue commit's tree) — carried explicitly so the reader knows
	// which class needed the bundle.
	Ignored []string
	// Bundle / BundleBytes / BundleSHA256 are the tar bundle and its integrity
	// evidence ("" / 0 / "" when there were no ignored entries to carry).
	Bundle     string
	BundleSize int64
	BundleSHA  string
	// Note is the one-sentence record; "" when nothing was rescued.
	Note string
	// MirroredStore is the PERSISTENT bare repository the rescue was copied
	// into ("" = the rescue lives only where it was written, which is
	// sufficient for a LINKED worktree and NOT sufficient for a clone ground —
	// see finishCleanupSalvageMirror).
	MirroredStore string
	// MirroredRef is the ref name inside MirroredStore (the same spelling as
	// Ref: one rescue, one address, wherever it lives).
	MirroredRef string
}

// finishCleanupSalvageRef is the ONE spelling of a row's rescue ref (property
// 3). taskID is sanitized the same way the ledger's own ids are: a git ref may
// not carry a space, a `~`, a `^`, a `:` or a `..`, and a row id from the
// ledger is a hex-shaped token — anything else is replaced rather than passed
// through, so a hostile id cannot address a ref outside this namespace.
func finishCleanupSalvageRef(taskID, worktree string) string {
	// The WORKTREE TARGET is part of the identity on purpose: one row CAN have
	// two grounds (the ledger records one, a run may create a second), and one
	// ref for two directories would make the second rescue overwrite the
	// first's evidence. A FNV-1a hash of the absolute path keeps the ref
	// readable AND collision-free without inventing a counter.
	target := finishCleanupSalvageToken(taskID)
	wt := worktree
	if abs, err := filepath.Abs(wt); err == nil {
		wt = abs
	}
	sum := sha256.Sum256([]byte(wt))
	return finishCleanupSalvageRefPrefix + target + "-" + hex.EncodeToString(sum[:])[:finishCleanupSalvageTargetLen]
}

// finishCleanupSalvageTargetLen is how many hex characters of the worktree
// digest the ref carries (Rule 1: a number with a meaning has a name).
const finishCleanupSalvageTargetLen = 12

// finishCleanupSalvageRefPrefixLiteral guards the namespace at the ONE place it
// is spelled: see finishCleanupSalvageToken.
//
// finishCleanupSalvageToken reduces an arbitrary string to characters a git ref
// component may carry: ASCII alphanumerics, `-`, `_`, `.`; everything else
// becomes `_`. `..` and a leading `.` are refused by git, so they are collapsed
// too — the function's job is to make the ref VALID, never to preserve the id's
// spelling.
func finishCleanupSalvageToken(s string) string {
	var b strings.Builder
	prevDot := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
			prevDot = false
		case r == '.':
			if prevDot {
				continue // `..` is not a legal ref component
			}
			b.WriteRune(r)
			prevDot = true
		default:
			b.WriteRune('_')
			prevDot = false
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return finishCleanupSalvageUnnamedTarget
	}
	return out
}

// finishCleanupSalvageUnnamedTarget is the token a row whose id reduces to
// nothing uses — a ref must have a component, and inventing one silently is
// what this constant exists to prevent.
const finishCleanupSalvageUnnamedTarget = "row"

// finishCleanupSalvageGit runs git in dir with an explicit environment — the
// ONE runner of this file (finishCleanupGitRaw cannot carry the throwaway
// index, and a second copy of its timeout/error shape would drift from it).
// It wraps its error with %w (Rule 16) and returns git's own diagnostic as the
// first line, exactly like finishCleanupGitRaw.
func finishCleanupSalvageGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, finishCleanupGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, finishCleanupFirstLine(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// finishCleanupSalvageBase returns the commit the rescue parents onto: the
// rescued worktree's HEAD. A worktree with no HEAD (an unborn branch) has no
// history to parent onto, and inventing a root commit would make the rescue
// look like an unrelated lineage — the caller then records that it could not
// salvage rather than producing a misleading ref.
func finishCleanupSalvageBase(ctx context.Context, wt string) (string, error) {
	return finishCleanupSalvageGit(ctx, wt, nil, finishCleanupPreMergeRevParseVerb, finishCleanupHeadRef)
}

// finishCleanupHeadRef is the spelling of HEAD the rescue resolves (Rule 1b).
const finishCleanupHeadRef = "HEAD"

// finishCleanupSalvageResidue performs the rescue (properties 1–3). It returns
// an empty result and a nil error for a CLEAN worktree: there is nothing to
// rescue, and that is not a failure.
//
// Every failure is returned, never swallowed: the caller keeps the refusal when
// the rescue did not produce a ref, so a rescue that could not run can never
// license a removal (Rule 15 + the whole point of the arm).
func finishCleanupSalvageResidue(ctx context.Context, wt, taskID, branch string) (finishCleanupSalvageResult, error) {
	var out finishCleanupSalvageResult
	wt = strings.TrimSpace(wt)
	if wt == "" {
		return out, nil
	}
	// PARSED body (split into porcelain vs ignored): stdout only — a stderr line
	// would be classified as an entry and rescued as if it were a file.
	raw, err := finishCleanupGitParsedStdout(ctx, wt, finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag, finishCleanupSalvageStatusIgnored)
	if err != nil {
		return out, fmt.Errorf("salvage: read the ground %s: %w", wt, err)
	}
	porcelain, ignored := finishCleanupSalvageSplit(raw)
	if strings.TrimSpace(porcelain) == "" && len(ignored) == 0 {
		return out, nil // clean: nothing to rescue
	}
	if strings.TrimSpace(porcelain) == "" {
		// IGNORED-ONLY: there is no index-visible change, so there is no tree
		// to write. The bundle alone is the rescue — and if it cannot be
		// written the caller refuses (an ignored file may still be a secret or
		// an irreplaceable artifact; git's own definition of "ignored" is a
		// convenience, not a claim of regenerability).
		if err := finishCleanupSalvageBundle(ctx, wt, taskID, ignored, &out); err != nil {
			return out, err
		}
		if out.Bundle != "" {
			out.Note = finishCleanupSalvageNote(taskID, wt, 0, out)
		}
		return out, nil
	}
	base, err := finishCleanupSalvageBase(ctx, wt)
	if err != nil {
		return out, fmt.Errorf("salvage: the ground %s has no HEAD to parent the rescue onto: %w", wt, err)
	}
	dir, err := finishCleanupSalvageDir(taskID)
	if err != nil {
		return out, err
	}
	index := filepath.Join(dir, finishCleanupSalvageIndexName)
	if err := os.Remove(index); err != nil && !os.IsNotExist(err) {
		return out, fmt.Errorf("salvage: clear the throwaway index %s: %w", index, err)
	}
	env := []string{finishCleanupSalvageIndexEnv + "=" + index}
	// THE THROWAWAY INDEX (property 2): read HEAD into it, stage the WHOLE
	// worktree into it (honouring .gitignore, like any `git add -A`), and write
	// the tree from it. The run's own index and working tree are never touched —
	// `git add` here writes <session>/salvage/index and nothing else.
	if _, err := finishCleanupSalvageGit(ctx, wt, env, finishCleanupSalvageReadTreeVerb, base); err != nil {
		return out, fmt.Errorf("salvage: seed the throwaway index: %w", err)
	}
	if _, err := finishCleanupSalvageGit(ctx, wt, env, finishCleanupSalvageAddVerb, finishCleanupSalvageAllFlag); err != nil {
		return out, fmt.Errorf("salvage: stage the ground into the throwaway index: %w", err)
	}
	tree, err := finishCleanupSalvageGit(ctx, wt, env, finishCleanupSalvageWriteTreeVerb)
	if err != nil {
		return out, fmt.Errorf("salvage: write the rescue tree: %w", err)
	}
	msg := fmt.Sprintf(finishCleanupSalvageMessageFmt, taskID, branch)
	commit, err := finishCleanupSalvageGit(ctx, wt, env, "-c", "user.name="+finishCleanupSalvageIdentityName,
		"-c", "user.email="+finishCleanupSalvageIdentityEmail,
		finishCleanupSalvageCommitTreeVerb, tree, finishCleanupSalvageParentFlag, base, finishCleanupSalvageMessageFlag, msg)
	if err != nil {
		return out, fmt.Errorf("salvage: write the rescue commit: %w", err)
	}
	ref := finishCleanupSalvageRef(taskID, wt)
	if _, err := finishCleanupSalvageGit(ctx, wt, nil, finishCleanupSalvageUpdateRefVerb, ref, commit); err != nil {
		return out, fmt.Errorf("salvage: publish %s: %w", ref, err)
	}
	out.Ref = ref
	out.Commit = commit
	// THE PUBLICATION LEG THAT SURVIVES THE REMOVAL (the defect this answers).
	// A rescue ref written into the GROUND is only as durable as the ground,
	// and a CLONE ground is deleted WHOLE. Mirroring first — and failing the
	// rescue if the mirror cannot be verified — is what makes `cleared` mean
	// "retrievable after this directory is gone" instead of "retrievable
	// while it is still standing here".
	store, merr := finishCleanupSalvageMirror(ctx, wt, taskID, ref)
	if merr != nil {
		return out, merr
	}
	out.MirroredStore = store
	out.MirroredRef = ref
	// THE LISTING IS READ FROM WHERE IT WILL STILL BE (Rule 15: the record is
	// evidence of the copy that survives, not of the directory about to be
	// deleted).
	listingDir := wt
	if store != "" {
		listingDir = store
	}
	if listing, lerr := finishCleanupSalvageGit(ctx, listingDir, nil, finishCleanupLsTreeVerb, finishCleanupLsTreeNameOnlyFlag, finishCleanupLsTreeRecurseFlag, ref); lerr == nil {
		out.Entries = finishCleanupSalvageLines(listing)
	}
	out.Ignored = ignored
	if err := finishCleanupSalvageBundle(ctx, wt, taskID, ignored, &out); err != nil {
		return out, err
	}
	out.Note = finishCleanupSalvageNote(taskID, wt, len(out.Entries), out)
	return out, nil
}

// finishCleanupLsTreeVerb / NameOnlyFlag / RecurseFlag are git's tree-listing
// spellings (Rule 1b).
const (
	finishCleanupLsTreeVerb          = "ls-tree"
	finishCleanupLsTreeNameOnlyFlag  = "--name-only"
	finishCleanupLsTreeRecurseFlag   = "-r"
	finishCleanupSalvageBundleTarExt = ".tar"

	// finishCleanupSalvageCatFileVerb / TypeFlag are git's object-type reading,
	// and finishCleanupSalvageCommitType is the answer that proves the mirrored
	// object really landed: the SAME reading the regression test asserts with,
	// so the mechanism and its proof cannot disagree about what "published"
	// means.
	finishCleanupSalvageCatFileVerb = "cat-file"
	finishCleanupSalvageTypeFlag    = "-t"
	finishCleanupSalvageCommitType  = "commit"
)

// finishCleanupSalvageMirror copies a rescue ref — WITH its objects — into the
// persistent bare repository inside the run's session directory, and VERIFIES
// the copy landed before returning. It returns "" (and no error) when the ground
// is not a CLONE: a linked worktree's rescue ref already lives in a repository
// that outlives the registration (`git worktree remove` deletes the directory,
// the branch refs and objects stay), so mirroring it would be a second copy of
// something nothing can lose.
//
// WHY THE VERIFICATION IS PART OF THE FUNCTION AND NOT THE CALLER'S JOB. The
// caller (`finishCleanupSalvageGround`) answers "may this directory be deleted?"
// from `cleared`, and `cleared` must mean "the residue is retrievable AFTER the
// deletion". A fetch that returned exit 0 but landed nothing (an unreachable
// store, a refspec the store refused, an object the transport dropped) would
// otherwise license the removal of the only copy — the exact data loss the whole
// rescue arm exists to prevent. So the mirrored ref is re-read from the STORE and
// its object type demanded to be a commit; anything else is an error, and the
// caller keeps the ground.
func finishCleanupSalvageMirror(ctx context.Context, wt, taskID, ref string) (string, error) {
	if !deliveryProofIsCloneGround(wt) {
		return "", nil
	}
	dir, err := finishCleanupSalvageDir(taskID)
	if err != nil {
		return "", err
	}
	store := filepath.Join(dir, finishCleanupSalvageStoreName)
	// `git init --bare <path>` is IDEMPOTENT for an existing repository, so a
	// second close-out of the same row reuses the one store instead of minting a
	// second copy of its history (the same property the ref itself has).
	if _, err := finishCleanupSalvageGit(ctx, dir, nil, finishCleanupSalvageInitVerb, finishCleanupSalvageBareFlag, store); err != nil {
		return "", fmt.Errorf("salvage: materialise the persistent repository %s: %w", store, err)
	}
	// THE FETCH RUNS IN THE STORE and reads the GROUND — the direction is the
	// whole correctness of this leg: `git fetch <repo> <refspec>` resolves the
	// refspec against <repo> and writes into the CWD's repository, so running it
	// the other way round asks the STORE for a ref only the ground has (measured:
	// `fatal: couldn't find remote ref refs/gopi/residue/…`). `+` forces the
	// refspec, so a rescue that replaced the same ref replaces it here too
	// (property 3: one ref per (row, worktree), deterministic and idempotent).
	refspec := fmt.Sprintf("+%s:%s", ref, ref)
	if _, err := finishCleanupSalvageGit(ctx, store, nil, finishCleanupSalvageFetchVerb, wt, refspec); err != nil {
		return "", fmt.Errorf("salvage: mirror %s into the persistent repository %s: %w", ref, store, err)
	}
	typ, err := finishCleanupSalvageGit(ctx, store, nil, finishCleanupSalvageCatFileVerb, finishCleanupSalvageTypeFlag, ref)
	if err != nil {
		return "", fmt.Errorf("salvage: read the mirrored %s back from %s: %w", ref, store, err)
	}
	if strings.TrimSpace(typ) != finishCleanupSalvageCommitType {
		return "", fmt.Errorf("salvage: the mirrored %s in %s answers %q, not a %s — the copy did not land, so the ground is kept",
			ref, store, strings.TrimSpace(typ), finishCleanupSalvageCommitType)
	}
	return store, nil
}

// finishCleanupSalvageSplit separates the classification porcelain from the
// IGNORED entries of one `git status --porcelain --ignored=matching` reading.
// The ignored lines are STRIPPED from the classification on purpose: the rescue
// commit cannot carry them, and a classifier that read them as ordinary residue
// would report a directory as dirty for content nothing can move.
func finishCleanupSalvageSplit(raw string) (porcelain string, ignored []string) {
	var keep []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, finishCleanupIgnoredPrefix) {
			ignored = append(ignored, strings.TrimSpace(strings.TrimPrefix(line, finishCleanupIgnoredPrefix)))
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n"), ignored
}

// finishCleanupSalvageLines splits a git listing into non-empty lines.
func finishCleanupSalvageLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// salvagePlural picks the suffix for a count — one spelling for every sentence
// this file writes (Rule 1b). Named distinctly from the ledger guard's own
// `plural` (task_merge_backlog_terminal_guard.go), which renders commit counts.
func salvagePlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// finishCleanupSalvageDir materialises the rescue's container inside the run's
// OWN session directory (Rule 35: no new root — the container rides the
// directory a run's artifacts already own, and inherits its reclamation).
func finishCleanupSalvageDir(taskID string) (string, error) {
	sd, err := taskSessionDir(taskID)
	if err != nil {
		return "", fmt.Errorf("salvage: resolve the run's session directory for %s: %w", taskID, err)
	}
	dir := filepath.Join(sd, finishCleanupSalvageDirName)
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, 0o700); err != nil {
		return "", fmt.Errorf("salvage: create %s: %w", dir, err)
	}
	return dir, nil
}

// finishCleanupSalvageBundle writes the bundle carrying the git-IGNORED entries
// (the class the rescue commit cannot hold), with its size and sha256 recorded
// (property 1). No ignored entries ⇒ no bundle, and that is not a failure. Past
// the cap the entries are NAMED in the note and the bundle is skipped, so the
// record still says exactly what was left.
//
// The tar mechanics are NOT re-implemented here: the dead-holder salvage arm
// (dirty_holder_salvage.go) already carries the ONE gzip+tar writer and the ONE
// size/sha256 evidence recorder this repo has, and a second copy would drift
// from it (Rule 8 — 复用不重做; Rule 14 — one definition per shared value). What
// this file adds in front of it is the boundary check the writer does not make:
// an ignored entry whose RESOLVED path leaves the ground is filtered out, and
// the removal (which this bundle justifies) is told so.
func finishCleanupSalvageBundle(ctx context.Context, wt, taskID string, ignored []string, out *finishCleanupSalvageResult) error {
	if len(ignored) == 0 {
		return nil
	}
	out.Ignored = ignored
	if len(ignored) > finishCleanupSalvageIgnoredCap {
		// Over the cap: the NAMES are the record (the caller's note carries
		// them); the bundle is not attempted, and the caller is told so.
		out.Note = finishCleanupSalvageOverflowNote(len(ignored), ignored)
		return nil
	}
	safe, escaped := finishCleanupSalvageContained(wt, ignored)
	if len(escaped) > 0 {
		// An entry that resolves outside the ground is NOT bundled and NOT
		// silently listed as carried: the rescue refuses to copy a path a run
		// could have planted (the difference between "rescue the ground" and
		// "exfiltrate the host"). Naming them is what keeps the refusal honest
		// (Rule 15) — and the caller keeps the ground until a human looks.
		return fmt.Errorf("salvage: %d ignored entr%s resolve outside the ground %s and are not bundled: %s",
			len(escaped), salvagePlural(len(escaped), "y", "ies"), wt, strings.Join(escaped, reclaimResidueFileSeparator))
	}
	dir, err := finishCleanupSalvageDir(taskID)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, finishCleanupSalvageBundleName)
	if _, err := dirtyHolderUntrackedArchive(wt, safe, dest); err != nil {
		os.Remove(dest) // a partial bundle is not evidence of anything
		return fmt.Errorf("salvage: bundle the ignored entries of %s: %w", wt, err)
	}
	// Size and sha256 of the ARTIFACT THAT SHIPPED, recorded through the ONE
	// recorder (captureAppend) so the rescue and the dead-holder salvage quote
	// their evidence in the same shape.
	var res dirtyHolderCaptureResult
	if err := captureAppend(&res, dest, finishCleanupSalvageBundleName); err != nil {
		os.Remove(dest)
		return err
	}
	out.Bundle = dest
	out.BundleSize = res.sizes[0]
	out.BundleSHA = res.sums[0]
	_ = ctx // the archive walk is bounded by the entry cap, not by a deadline
	return nil
}

// finishCleanupSalvageContained splits the ignored entries into those whose
// RESOLVED path is inside the ground and those that are not. A path that cannot
// be resolved (it vanished between the listing and the walk) stays with the
// contained set — the archive writer reports it by leaving it out, which is the
// honest outcome for a file nobody can read any more.
func finishCleanupSalvageContained(wt string, entries []string) (safe, escaped []string) {
	root, err := filepath.EvalSymlinks(wt)
	if err != nil {
		return entries, nil // cannot resolve the ground: nothing to compare against
	}
	for _, rel := range entries {
		resolved, rerr := filepath.EvalSymlinks(filepath.Join(wt, rel))
		if rerr != nil {
			safe = append(safe, rel)
			continue
		}
		if resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			safe = append(safe, rel)
			continue
		}
		escaped = append(escaped, rel)
	}
	return safe, escaped
}

// finishCleanupSalvageNote is the ONE sentence the rescue leaves on the row's
// trail (property 1). It names the row, the ground, the ref, the count, and
// the bundle with its size and digest — everything a later reader needs to
// retrieve the residue WITHOUT the commit having ever been on a branch.
func finishCleanupSalvageNote(taskID, wt string, entries int, out finishCleanupSalvageResult) string {
	parts := []string{
		fmt.Sprintf("run %s's ground %s %s: %d entr%s in %s (%s)",
			taskID, wt, finishCleanupSalvageRescuedMarker, entries, salvagePlural(entries, "y", "ies"), out.Ref, out.Commit),
		"entries: " + strings.Join(finishCleanupSalvageCapped(out.Entries), reclaimResidueFileSeparator),
	}
	if len(out.Ignored) > 0 {
		parts = append(parts, fmt.Sprintf("%d git-ignored entr%s (not in the rescue commit) — %s", len(out.Ignored), salvagePlural(len(out.Ignored), "y", "ies"), strings.Join(out.Ignored, reclaimResidueFileSeparator)))
	}
	if out.Bundle != "" {
		parts = append(parts, fmt.Sprintf("bundle %s (%d bytes, %s %s)", out.Bundle, out.BundleSize, salvageSHA256Label, out.BundleSHA))
	}
	if out.MirroredStore != "" {
		parts = append(parts, fmt.Sprintf(finishCleanupSalvageMirroredFmt, out.MirroredStore, out.MirroredStore, out.MirroredRef))
	}
	parts = append(parts, "retrieve with `git ls-tree -r "+out.Ref+"` / `git archive "+out.Ref+"| tar -t` — nothing was deleted before this record existed")
	return strings.Join(parts, "; ")
}

// finishCleanupSalvageOverflowNote is the record when the ignored class
// exceeded the bundle's cap: the NAMES are carried (property 1) even though the
// bytes are not.
func finishCleanupSalvageOverflowNote(n int, ignored []string) string {
	return fmt.Sprintf("the ground carries %d git-ignored entries, above the %d-entry bundle cap — the entries are NAMED here rather than copied: %s",
		n, finishCleanupSalvageIgnoredCap, strings.Join(ignored, reclaimResidueFileSeparator))
}

// finishCleanupDirtyReasonIsResidue reports whether a removal reason names the
// UNTRACKED-residue dirty state — the ONE shape the rescue may clear.
//
// IT IS DELIBERATELY NOT "both dirty shapes", and the asymmetry is the point
// (Rule 26: the repair is narrow). A TRACKED change is an uncommitted
// DELIVERABLE: the run has not delivered, its `completed` writeback is refused
// (reclaimRunTrackedDirtRefusal), the row stays OPEN, and a ground belonging to
// an unfinished run is not this arm's to collect — removing it would delete the
// tree a still-live obligation is written in. The UNTRACKED shape is the other
// fact: the run finished and its own scratch is still lying in the tree (Rule
// 30), which is hygiene, not an obligation. Only that one can be cleared by a
// rescue.
func finishCleanupDirtyReasonIsResidue(removeWhy string) bool {
	return strings.Contains(removeWhy, finishCleanupDirtyUntrackedPrefix)
}

// finishCleanupSalvageGround is the ONE call the close-out chain makes (Rule
// 14). It rescues the ground of a dirty refusal and answers whether the refusal
// may be DROPPED:
//
//   - cleared == true  → the residue is now retrievable (a rescue ref carrying
//     every index-visible entry, and/or a bundle carrying the ignored ones), so
//     the removal arm may proceed. The note is already recorded on the row's
//     trail by this call.
//   - cleared == false → nothing was rescued (a failed step, or a class the
//     rescue deliberately does not carry), and the caller keeps the refusal
//     EXACTLY as before: this arm never trades a refusal for an empty promise.
//
// The error arm is REPORTED, never swallowed (Rule 15): the caller renders it
// as the step's detail, and the refusal stands.
func finishCleanupSalvageGround(ctx context.Context, wt, taskID, branch string) (note string, cleared bool, err error) {
	res, serr := finishCleanupSalvageResidue(ctx, wt, taskID, branch)
	if serr != nil {
		finishCleanupSalvageRecord(taskID, serr.Error())
		return "", false, serr
	}
	if res.Note == "" && res.Ref == "" && res.Bundle == "" {
		// The tree went clean between the refusal's own reading and this call
		// (a run finishing its own cleanup concurrently). Nothing to rescue —
		// and nothing to refuse either: the reason was a measurement of a
		// state that no longer holds. Stated, because a reader must be able to
		// tell "rescued" from "nothing left to rescue".
		return finishCleanupSalvageVanishedNote, true, nil
	}
	if res.Ref == "" && res.Bundle == "" {
		// The ignored class exceeded the bundle cap: the entries are NAMED,
		// the bytes are not carried — so the refusal STANDS (an ignored file
		// git cannot move is still single-copy data this arm will not delete in
		// one motion). The note is recorded so an operator can act on it.
		finishCleanupSalvageRecord(taskID, res.Note)
		return res.Note, false, nil
	}
	finishCleanupSalvageRecord(taskID, res.Note)
	return res.Note, true, nil
}

// finishCleanupSalvageVanishedNote states the third state of the rescue: the
// ground is no longer dirty, so there is no residue to carry and no reason to
// refuse.
const finishCleanupSalvageVanishedNote = "the ground went clean between the removal reason's reading and the rescue — nothing was left to carry (the removal may proceed)"

// finishCleanupSalvageRescuedMarker / salvageSHA256Label are the record's
// shared vocabulary (Rule 1b: the sentence, the event and the tests meet on one
// spelling instead of re-typing a phrase).
const (
	finishCleanupSalvageRescuedMarker = "was RESCUED before this close-out (retrievable, nothing deleted)"

	// finishCleanupSalvageEntryCap bounds how many rescued entry names the
	// record spells out: a worktree can carry thousands of untracked files and
	// the record is read by a human. Past the cap the COUNT still stands and the
	// ref is the complete listing (`git ls-tree -r <ref>`) — the truncation is
	// itself stated, so a reader is never misled about the total (Rule 1: the
	// bound is named; Rule 15: the omission is named too).
	finishCleanupSalvageEntryCap = 40

	// salvageSHA256Label is how the bundle's digest is labelled in the record —
	// the retrieval handle an operator greps for (Rule 1b).
	salvageSHA256Label = "sha256"
)

// finishCleanupSalvageCapped renders the rescue's entry list for the record,
// naming the truncation when it applies (Rule 15: an abbreviated list must say
// it is abbreviated).
func finishCleanupSalvageCapped(entries []string) []string {
	if len(entries) <= finishCleanupSalvageEntryCap {
		return entries
	}
	out := append([]string{}, entries[:finishCleanupSalvageEntryCap]...)
	return append(out, fmt.Sprintf("… and %d more (the ref lists them all)", len(entries)-finishCleanupSalvageEntryCap))
}

// finishCleanupSalvageFailedNote appends the salvage's own failure to the
// removal reason, so the refusal a reader sees names BOTH the dirt and why the
// rescue that would have cleared it did not run (Rule 15).
const finishCleanupSalvageFailedNote = "; the rescue of that residue FAILED, so the ground is kept: "

// finishCleanupSalvageStep is the salvage's step name in a close-out report
// (Rule 1b — the report and its tests address it by this spelling).
const finishCleanupSalvageStep = "salvage"

// finishCleanupSalvageEvent is the ledger event value carrying a rescue's
// record on the row's trail. Its own event (distinct from the refusal's) so an
// operator can tell "the run was told" from "the residue is recoverable, here".
const finishCleanupSalvageEvent = reclaimRunResidueUncleanEvent + "-salvaged"

// finishCleanupSalvageRecord files a rescue record on the row's trail. Same
// best-effort contract as every other close-out record: the stderr note is the
// carrier when the ledger is unreachable (Rule 15).
func finishCleanupSalvageRecord(taskID, detail string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || strings.TrimSpace(detail) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("finish-cleanup: salvage record for %s not written (ledger unavailable): %v", taskID, err)
		fmt.Fprintln(os.Stderr, "[task] "+detail)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, finishCleanupSalvageEvent, detail)
	taskNotifyLog("finish-cleanup: %s", detail)
}

const (
	// finishCleanupGroundReclaimedMarker heads the skip reason composed when the
	// directory is gone AND a recorded arm freed it. It is the ONE spelling of
	// the recycled terminal state (Rules 1b/13): the settled classifier reads it
	// and the regression test asserts it, so neither can drift from the prose an
	// operator sees. It must never carry the word "unreadable" — that word is
	// what sent the operator looking for a directory that was deleted on
	// purpose.
	finishCleanupGroundReclaimedMarker = "ground already reclaimed"

	// finishCleanupGroundReclaimedStep is the report step that records the
	// finding. It is a step of its own (a SUCCESSFUL one, OK=true) rather than a
	// clause inside the reason: the one-line report prints the successful steps
	// in its `steps that DID run` tail, so the reader sees that this chain
	// ACTUALLY resolved the absence rather than skipped over it.
	finishCleanupGroundReclaimedStep = "ground-reclaimed"

	// finishCleanupGroundReclaimedArmLabel names the arm that freed the ground,
	// finishCleanupGroundReclaimedAtLabel its stamp and
	// finishCleanupGroundReclaimedTipLabel the tip the record preserved. One
	// constant each, because the reason is read by an operator AND asserted by
	// the regression test.
	finishCleanupGroundReclaimedArmLabel = " arm="
	finishCleanupGroundReclaimedAtLabel  = " at "
	finishCleanupGroundReclaimedTipLabel = " tip="

	// finishCleanupGroundReclaimedNoTipLabel stands in the tip slot when the
	// record carries none (the pre-2026-10-29 records the sink still holds).
	// Named rather than an empty string so the reason never renders
	// `tip=` followed by nothing, which reads as a truncation.
	finishCleanupGroundReclaimedNoTipLabel = "(not recorded)"

	// finishCleanupGroundReclaimSinkUnreadableLog is the operator-facing line
	// printed when the sink cannot be read (Rule 15). It says which file and
	// why, and states the consequence — the alarm is kept, not the silence.
	finishCleanupGroundReclaimSinkUnreadableLog = "finish-cleanup: the removal-audit sink %s could not be read " +
		"(%v) — the vanished worktree %s cannot be accounted for, so its refusal stands as reported"
)

// finishCleanupGroundDirExists reports whether the recorded worktree directory
// is still on disk. It is a STAT and not a git answer on purpose: the question
// asked here is about a DIRECTORY ("was this ground removed?"), and it must be
// answerable without entering the directory the question is about.
func finishCleanupGroundDirExists(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, err := os.Stat(dir)
	return err == nil
}

// finishCleanupOwnRemovalArm reports whether an arm name belongs to THIS chain —
// the close-out itself. Such a record is NOT a reclaim reading, and the
// distinction is the whole reason the set exists:
//
//	· a THIRD PARTY's record answers "an arm finished with this ground before
//	  this close-out ever looked" — the writer that runs FIRST, and the state
//	  this file exists to report as INFO;
//	· the CLOSE-OUT's own record answers "this chain removed that directory in
//	  an EARLIER pass" — a fact the chain already reports through its own
//	  removal/settled wording, with the delivery state attached. Reading it as a
//	  third-party reclaim would relabel every idempotent second pass as somebody
//	  else's act, and would drop the delivery sentence that shape carries.
//
// One predicate, stated once (Rules 1b/13): the reader below filters on it and a
// future arm that frees a ground joins the set HERE or is read as a third party
// — the choice cannot be made silently at the call site.
func finishCleanupOwnRemovalArm(arm removalaudit.Arm) bool {
	switch arm {
	case removalaudit.ArmFinishCleanupServer,
		removalaudit.ArmFinishCleanupForce,
		removalaudit.ArmFinishCleanupBranch,
		removalaudit.ArmFinishCleanupBranchForce:
		return true
	}
	return false
}

// finishCleanupGroundReclaimRecord asks the removal-audit sink whether THIS
// row's ground was already freed by a recorded arm, and answers the newest such
// record.
//
// The identity it matches on is the PROPERTY the reader must be sure about —
// the DIRECTORY — compared canonically (finishCleanupSameWorktreePath, the same
// comparator the ownership gate uses, so a trailing slash or a symlinked
// workspace root cannot split one ground into two). A record that names a
// ledger row must name THIS row: a recycled path (the sweep's own reclaim of a
// ground an earlier row abandoned) must never be read as this row's accounting.
// A record with no task id is accepted on the path alone — the path is per-run
// unique, and the arms that free a ground do not all know a row id.
//
// The second return value is "a record was found"; the error is the sink's own
// read failure, which the caller reports and treats as NO RECORD (Rule 15: the
// alarm is kept, the reason is printed).
func finishCleanupGroundReclaimRecord(taskID, dir string) (removalaudit.Record, bool, error) {
	taskID = taskIDNormalize(taskID)
	if strings.TrimSpace(dir) == "" {
		return removalaudit.Record{}, false, nil
	}
	path, why := removalaudit.SinkPath()
	if path == "" {
		return removalaudit.Record{}, false, fmt.Errorf("no removal-audit sink to read: %s", why)
	}
	recs, err := removalaudit.Records(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No sink yet = no gopi arm has ever removed anything on this host.
			// That is an EMPTY HISTORY, not a failure: the absence of a record
			// is the answer, and it is the loud one.
			return removalaudit.Record{}, false, nil
		}
		return removalaudit.Record{}, false, err
	}
	// NEWEST FIRST: a row can have its ground freed more than once (a re-cut
	// ground after a failed run), and the record that explains THIS absence is
	// the last one — the one whose removal the row's absence followed.
	for i := len(recs) - 1; i >= 0; i-- {
		rec := recs[i]
		if rec.Kind != removalaudit.KindWorktreeRemoval {
			continue
		}
		if finishCleanupOwnRemovalArm(removalaudit.Arm(rec.Arm)) {
			// THIS chain's own earlier pass (see finishCleanupOwnRemovalArm): not a
			// third-party reclaim, and its wording is the removal/settled path's.
			continue
		}
		if !finishCleanupSameWorktreePath(rec.Target, dir) {
			continue
		}
		if recTask := strings.TrimSpace(rec.TaskID); recTask != "" && taskID != "" &&
			taskIDNormalize(recTask) != taskIDNormalize(taskID) {
			continue
		}
		return rec, true, nil
	}
	return removalaudit.Record{}, false, nil
}

// finishCleanupGroundReclaimSinkLabel names the sink the record question was
// asked of, for the operator line printed when it cannot be read. It resolves
// through the ONE reader (removalaudit.SinkPath) so the line names the file
// actually in force, never a re-derived path.
func finishCleanupGroundReclaimSinkLabel() string {
	path, why := removalaudit.SinkPath()
	if path != "" {
		return path
	}
	if why != "" {
		return "(none: " + why + ")"
	}
	return "(none resolved)"
}

// finishCleanupGroundReclaimedSkipReason composes the INFO-level skip reason
// for a ground a recorded arm already freed.
//
// It carries the DIRECTORY too (the same discipline the settled classifier's
// other arms follow): a reason that names the row's own path can be matched
// against the report's Worktree, and one that does not would read as a general
// statement about the sweep rather than about this row.
func finishCleanupGroundReclaimedSkipReason(rec removalaudit.Record, dir string) string {
	tip := strings.TrimSpace(rec.Sha)
	if tip == "" {
		tip = finishCleanupGroundReclaimedNoTipLabel
	}
	return finishCleanupGroundReclaimedMarker +
		finishCleanupGroundReclaimedArmLabel + strings.TrimSpace(rec.Arm) +
		finishCleanupGroundReclaimedAtLabel + strings.TrimSpace(rec.TS) +
		finishCleanupGroundReclaimedTipLabel + tip +
		": " + strings.TrimSpace(dir)
}

// finishCleanupGroundReclaimedDetail is the Detail of the successful step the
// reclaimed branch records — the same facts as the reason, rendered for a
// reader scanning the step list.
func finishCleanupGroundReclaimedDetail(rec removalaudit.Record) string {
	detail := "freed by " + strings.TrimSpace(rec.Arm) +
		finishCleanupGroundReclaimedAtLabel + strings.TrimSpace(rec.TS)
	if ev := strings.TrimSpace(rec.Evidence); ev != "" {
		detail += " — " + ev
	}
	return detail
}

// finishCleanupReclaimedGone asks the ONE question a vanished ground raises —
// "was it FREED, or is it LOST?" — and, when it was freed, answers the report
// outright.
//
// THE TWO READINGS, in this order and for this reason:
//
//	① THE DIRECTORY ON DISK (finishCleanupGroundDirExists). A directory that is
//	  still there has nothing to explain — the chain goes on to ask its own
//	  questions and this predicate answers false without touching the sink. That
//	  keeps the sink read OFF the ordinary path (the overwhelming majority of
//	  close-outs) and makes this arm's cost exactly one stat.
//	② THE RECORD (finishCleanupGroundReclaimRecord), asked only for a ground
//	  that is NOT there. A record naming this directory (and this row, when both
//	  sides know one) means an arm freed it on evidence it judged sufficient, and
//	  the report says so as a SUCCESSFUL step with no alarm in its prose.
//
// NO RECORD, NO CONCLUSION: false, and the caller keeps composing exactly the
// refusal it composed before this file existed. An unreadable sink is the same
// answer (the alarm stands) with the failure REPORTED — the operator learns the
// question could not be asked, which is the difference between "nobody recorded
// it" and "we cannot tell" (Rule 15, and the reason the log line exists).
//
// THE PLAN AND THE APPLIED ARM GET THE SAME VERDICT, deliberately: a recycled
// ground is a terminal fact on either face, and a plan that predicted a removal
// the record already accounts for would be the same false alarm one step
// earlier. The step this arm records carries no finishCleanupPlanMarker, because
// there is nothing left to run — the prediction it replaces was a removal that
// had already happened.
func finishCleanupReclaimedGone(ctx context.Context, rep *FinishCleanupReport, wt string) (*FinishCleanupReport, bool) {
	if rep == nil || strings.TrimSpace(wt) == "" || finishCleanupGroundDirExists(wt) {
		return rep, false
	}
	// The probe is still what makes a gone ground a FACT rather than a stat's
	// opinion: a directory that is a file, a path git refuses for a reason of its
	// own, or a mount that vanished between the two calls is NOT a reclaim. Asked
	// BEFORE the record so the two conditions cannot be conflated.
	if _, err := finishCleanupGit(ctx, wt, "rev-parse", "--verify", "HEAD"); err == nil {
		return rep, false
	}
	rec, found, rerr := finishCleanupGroundReclaimRecord(rep.TaskID, wt)
	if !found {
		if rerr != nil {
			taskNotifyLog(finishCleanupGroundReclaimSinkUnreadableLog,
				finishCleanupGroundReclaimSinkLabel(), rerr, wt)
		}
		return rep, false
	}
	rep.Skipped = true
	rep.SkipReason = finishCleanupGroundReclaimedSkipReason(rec, wt)
	rep.Steps = append(rep.Steps, FinishCleanupStep{
		Step: finishCleanupGroundReclaimedStep, Target: wt, OK: true,
		Detail: finishCleanupGroundReclaimedDetail(rec),
	})
	return rep, true
}

// OpenTasksDBForSweep opens the ledger for a sweep pass.
//
// It lived inside the retired close-out mechanism's file, but it is not part of
// it: three LIVE tests open the ledger through this name (worker_drain_unit,
// worker_stop_accept, cli_tasks_dead_holder_accept) and the sweep that still
// ticks reads rows through the same door. Rule 43 — a symbol with readers
// outside the mechanism moves out BEFORE the mechanism goes, in the same commit.
func OpenTasksDBForSweep(ctx context.Context) (*sql.DB, error) {
	return openTasksDB(ctx, memoryDBPath())
}
