package tools

// Stale-worktree-registration sweeper — ranked guard #5 of the 2026-09-13
// /dev/shm assessment, and the one the assessment found MISSING.
//
// THE MEASURED PROBLEM. `git worktree list` for the main repo answered with 19
// linked worktrees, 1 015 MB, inside a tmpfs whose size nobody chose; MemFree
// was 265 MB. Every one of those registrations was created by a task that has
// since finished. The existing reclaim path (task_reclaim.go) only handles rows
// whose CLAIMED HOLDER IS DEAD and whose status is TERMINAL — a completed task
// whose holder is long gone, or a task whose session simply ended without the
// finish-cleanup step, leaves a registration that nothing ever revisits. The
// suite's own tmpfs sweep (internal/scratch) cannot see a name that is not a
// task-scratch name, which is why 19 worktrees survived it.
//
// WHAT THIS SWEEPS: the GIT REGISTRATIONS of a repo, matched to the task ledger,
// and reclaimed only when every one of five independent conditions holds:
//
//	① the registration is a linked worktree inside a DECLARED root
//	   (worktreeroots policy — the main checkout and anything outside a declared
//	   root are never touched, they are only REPORTED);
//	② NO LIVE PROCESS HOLDS IT (cwd, fd, exe or a mapped file — the holder pass,
//	   which is what makes deleting a live suite's fixture survivable);
//	③ NO TASK ROW OWNS IT (no non-terminal row's deliverable names it as its
//	   worktree, and no non-terminal row mentions its branch);
//	④ IT IS CLEAN (`git status --porcelain` empty — uncommitted work is never
//	   destroyed; a dirty worktree is REPORTED instead, which is precisely the
//	   deliverable a merged-but-uncommitted branch needs). A dirty registration
//	   that NO ledger row owns is additionally ESCALATED to the operator once it
//	   passes the age floor: it is the one shape every other arm on the host is
//	   blind to, and it is still never removed (see worktreeEscalateOrphan).
//	⑤ IT IS OLDER THAN THE AGE THRESHOLD, measured on the git-internal backlink
//	   file (`<gitdir>/commondir`) — not on the directory mtime, which any later
//	   access refreshes and which therefore cannot answer "when was this made".
//
// ONE LIST AND ONE PRUNE PER REPO (stated as a hard rule because the
// alternative is actively wrong): registrations are enumerated per REPO and
// pruned per REPO, never in a per-row loop. `git worktree prune` is
// repo-global — it drops every registration whose directory is gone — so a
// per-row `prune` attributes an unrelated registration's removal to whatever row
// happened to run the prune. And the enumeration is done ONCE per repo, because
// each listing is a git process: 19 registrations × N repos of per-row listing
// is the cost that makes a sweeper too slow to run.
//
// RECLAIM = `git worktree remove` (never --force: git re-checks dirty/locked
// state itself, so a guard this file missed still cannot destroy work) followed
// by ONE `git worktree prune` per repo. The directory is NOT deleted directly:
// git owns the registration, and a `remove` that git refuses is the signal the
// sweeper wanted.
//
// BACKGROUND OR FOREGROUND: `gopi task sweep-worktrees` is the operator verb
// (dry-run by DEFAULT — an operator reads the plan before an unattended run
// takes anything away), and the same core is what a resident daemon can call.
// Neither path can be destructive without the operator asking for it, because
// the dry-run default is a property of the options the caller must override.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/removalaudit"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// worktreeSweepMinAgeEnvName is the age threshold (hours) below which a
	// registration is never reclaimed. Read in exactly ONE place
	// (worktreeSweepOptionsFrom), Rule 2 — and DECLARED once, in the leaf
	// package both this one and the config catalog already import
	// (config.WorktreeSweepMinAgeEnvName, Rules 1b/13/14): the name is
	// operator-visible (the family help advertises it, `gopi --help-env`
	// documents it, CONFIG.md tabulates it), so its single definition belongs
	// where every one of those readers can reach it. Re-typing the literal here
	// would put the help page and the catalog one typo apart.
	worktreeSweepMinAgeEnvName = config.WorktreeSweepMinAgeEnvName
	// worktreeSweepMaxEnvName caps how many registrations one run reclaims (the
	// blast-radius throttle: an unattended sweep that suddenly finds a hundred
	// stale registrations takes a bounded bite). Read in the same ONE place,
	// declared in the same leaf package.
	worktreeSweepMaxEnvName = config.WorktreeSweepMaxEnvName

	// DefaultWorktreeSweepMinAge is the default age threshold. MEASURED basis:
	// the assessment's stale registrations came from tasks finished days
	// earlier, while a LIVE sibling session's worktree is typically hours old;
	// 24 h separates the two with room to spare, and the holder pass is what
	// protects anything younger that nobody would have expected.
	DefaultWorktreeSweepMinAge = 24 * time.Hour
	// DefaultWorktreeSweepMax caps one run's reclamation.
	DefaultWorktreeSweepMax = 50

	// worktreeSweepEndedGrace is how recently a NON-TERMINAL row must carry
	// ended_at before the sweep will treat that row as over despite its
	// status. A live status on a row whose life was already declared over is
	// self-contradictory, but there is one benign way to produce it: a run
	// that submits a CHILD row mid-task while its own row is closed out as
	// the submission goes terminal (measured on this ledger: the gap between a
	// row's ended_at and its child's created_at is under two minutes), and a
	// child that inherits the same deliverable.branch would then collide with
	// the parent's own registration. The grace keeps that window safe while
	// still releasing a registration whose owner has been stamped over for
	// far longer than any live run can stay silent.
	worktreeSweepEndedGrace = time.Hour

	// worktreeSweepGitTimeout bounds ONE git process of the sweep.
	worktreeSweepGitTimeout = 60 * time.Second
	// worktreeSweepInitLockFloor is how long git's "initializing" lock must
	// have been sitting before the sweep treats it as ABANDONED. The window is
	// load-bearing, not cosmetic: git writes that lock at the START of
	// `worktree add` and removes it when the checkout completes, so a lock
	// younger than this belongs to a creator that is RUNNING RIGHT NOW — and
	// clearing it would let a concurrent `worktree prune` pull the
	// registration out from under the live creator. `worktree add` on this
	// host completes in well under a minute; a floor far above it (an hour)
	// keeps a merely SLOW creator safe while still collecting the leak within
	// an hour of the SIGKILL that made it (the sweeper's own age default is
	// 24h, so the rescue still lands an order of magnitude sooner than a
	// reclaim).
	worktreeSweepInitLockFloor = time.Hour
	// worktreeSweepHolderScanMax bounds how many pids the holder pass probes.

	// worktreePorcelainHeadToken / worktreePorcelainDetachedToken are git's own
	// porcelain keys for the HEAD line and the detached marker.
	worktreePorcelainHeadToken     = "HEAD"
	worktreePorcelainDetachedToken = "detached"
	// Backlink file names inside a linked worktree's git dir. `commondir` is the
	// file git writes for the main repo's git dir, so its mtime records when the
	// registration was CREATED — the age signal that a directory access cannot
	// forge.
	worktreeSweepCommondirName = "commondir"
	// worktreeSweepGitdirName is the worktree-private git dir itself; the
	// fallback age signal when commondir is absent (a hand-made registration).
	worktreeSweepGitdirName = ".git"
	// worktreeSweepGitdirFileName / worktreeSweepAdminPathName /
	// worktreeSweepLockFileName are the three names of git's OWN admin area for
	// a linked worktree — the ones the dead-registration repair reads, then
	// clears:
	//
	//	<repo>/.git/worktrees/<id>/gitdir   the backlink to the worktree's .git
	//	                                    file — the identity of the entry
	//	<repo>/.git/worktrees/<id>/locked   git's do-not-touch flag, left behind
	//	                                    as "initializing" by a SIGKILLed
	//	                                    `worktree add`
	//
	// These are the names git itself documents for the unlock repair, spelled
	// once (Rule 1b: they are compared and written, and a typo would silently
	// disable the repair).
	worktreeSweepGitdirFileName = "gitdir"
	worktreeSweepAdminPathName  = ".git/worktrees"
	worktreeSweepLockFileName   = "locked"
	// worktreeSweepHeadName is the registration's own HEAD file — the age
	// signal's fallback when the `gitdir` backlink is absent (a hand-made
	// registration). Both are written once at registration and survive every
	// routine git verb, unlike the git dir's own mtime.
	worktreeSweepHeadName = "HEAD"
	// worktreeSweepUnlockVerb is git's own unlock subcommand — the repair git's
	// refusal message names ("use 'remove -f -f' to override or unlock first").
	worktreeSweepUnlockVerb = "unlock"
	// worktreeSweepBranchVerb is git's branch verb (the ref axis of the reap).
	worktreeSweepBranchVerb = "branch"
	// worktreeSweepInitLockReason is the lock CONTENT git writes while
	// `worktree add` is still running. It is read (never written) in
	// production and written by the leak fixture in tests; it is a named
	// constant because the fixture must reproduce the leak's own spelling.
	worktreeSweepInitLockReason = "initializing"

	// porcelain output markers (git worktree list --porcelain). They are DERIVED
	// from the shared finishCleanup* constants rather than re-spelled: the
	// package's own literal gate (TestFinishCleanupWorktreeVerbLiteralGate)
	// exists precisely because a second spelling of the git verb drifts, so
	// these aliases reference it (Rule 13).
	worktreePorcelainWorktreePrefix = finishCleanupWorktreeVerb + " "
	worktreePorcelainBranchPrefix   = finishCleanupPorcelainBranchPrefix
	worktreePorcelainHeadPrefix     = worktreePorcelainHeadToken + " "
	worktreePorcelainDetached       = worktreePorcelainDetachedToken
	// worktreePorcelainPrunable is DERIVED from the shared marker rather than
	// re-spelled: the merger's candidate filter (repoSweepStaleWorktree) and
	// this sweeper must agree on the token, or one of them reads a leftover
	// registration as a live checkout.
	worktreePorcelainPrunable = finishCleanupPorcelainPrunableMark
	// worktreePorcelainLocked / worktreePorcelainBare are git's own porcelain
	// annotations; the unlock/lock-file names live with the other admin-area
	// constants above.
	worktreePorcelainLocked = "locked"
	worktreePorcelainBare   = "bare"
)

// worktreeDisposition is the closed verdict set for one registration. It is a
// named type with constants (never a bare "keep"/"reclaim" literal at a
// comparison site — Rule 1b) because every branch below discriminates on it.
type worktreeDisposition string

const (
	// worktreeReclaim: every guard passed — the registration is removed (or, in
	// a dry run, would be).
	worktreeReclaim worktreeDisposition = "reclaim"
	// worktreeKeep: a guard said no, and the reason is recorded. There is no
	// "unknown" disposition: an unanswerable question resolves to keep.
	worktreeKeep worktreeDisposition = "keep"
	// worktreeReport: the registration is NOT this sweeper's to remove, but it
	// still consumes disk — the operator is told (Rule 15: name what you can
	// see instead of doing nothing).
	worktreeReport worktreeDisposition = "report"
	// worktreeReap: the registration is DEAD — its directory is gone, so the
	// only thing left to collect is GIT BOOKKEEPING (the .git/worktrees entry)
	// and the stale "initializing" lock a SIGKILLed `worktree add` leaves
	// behind. Distinct from worktreeReclaim because the evidence bar differs: a
	// reap destroys NO working tree and NO branch, so it needs no age or
	// dirtiness verdict — but it is still refused while a live process holds
	// the path (a registration can look dir-less to us and real to its owner
	// in another mount namespace).
	worktreeReap worktreeDisposition = "reap"
	// worktreeEscalateOrphan: the registration is DIRTY and NO ledger row
	// owns it — the one shape that was unreachable by EVERY sweep on the host
	// (the producer pass needs a row, the dead-holder arm needs a terminal row,
	// and the reclaim's guard ② refuses a dirty tree), so the directory — and
	// the merge gate it pins (safe-merge [c]) — could sit for good. It is
	// ESCALATED, never removed: uncommitted work is a human's call, and this
	// sweeper's removal verb has no --force by design. Distinct from
	// worktreeReport because the operator decision is different: a dirty tree
	// whose owner ROW exists is somebody's unfinished delivery, while a dirty
	// tree NOBODY owns is residue whose author will never come back for it.
	worktreeEscalateOrphan worktreeDisposition = "escalate"
	// worktreeReapBranch: the registration is DIRTY — the tree is gone but
	// uncommitted work was destroyed with it, so its branch is pure residue on
	// top of the dead registration. Removed only when the branch is PROVABLY
	// its own (it IS the registration's HEAD) and git itself agrees the branch
	// is safe to delete (`branch -d`, never `-D`).
	worktreeReapBranch worktreeDisposition = "reap-branch"
)

// worktreeSweepRule names the policy ARM that decided a registration. The
// values are the dry run's contract: the operator reads a plan, and a plan that
// does not say WHICH rule produced a verdict is a plan they cannot act on
// (Rule 15). They are constants because the same spelling is asserted by the
// sweep's own tests and printed in its report (Rule 1b).
type worktreeSweepRule string

const (
	// worktreeRuleMainCheckout: the first registration is the repository.
	worktreeRuleMainCheckout worktreeSweepRule = "main-checkout"
	// worktreeRuleBare: a bare registration carries no working tree.
	worktreeRuleBare worktreeSweepRule = "bare-registration"
	// worktreeRuleDeadRegistration: the worktree directory is gone (or git
	// calls the entry prunable) — git bookkeeping is all that is left.
	worktreeRuleDeadRegistration worktreeSweepRule = "dead-registration"
	// worktreeRuleGitLock: git itself flagged the entry locked.
	worktreeRuleGitLock worktreeSweepRule = "git-lock"
	// worktreeRuleRootScope: the declared-root test — the arm that REPORTS an
	// out-of-policy registration (and the one the memory-base root extends).
	worktreeRuleRootScope worktreeSweepRule = "declared-root-scope"
	// worktreeRuleLiveHolder: a live process holds the path.
	worktreeRuleLiveHolder worktreeSweepRule = "live-holder"
	// worktreeRuleLedgerOwnership: the ledger says a live task still holds
	// ground here.
	worktreeRuleLedgerOwnership worktreeSweepRule = "ledger-ownership"
	// worktreeRuleSessionLive: no row's deliverable names this registration, but
	// a LIVE row still has a run standing (its recorded runner, or a process in
	// its session directory) — the concurrent-session arm. Weaker than a
	// deliverable tie on purpose, and reported under its own rule so an
	// operator can see WHICH evidence kept the registration.
	worktreeRuleSessionLive worktreeSweepRule = "live-session"
	// worktreeRuleOrphan: the registration is DIRTY and NO ledger row owns it —
	// the arm that ESCALATES it (worktreeEscalateOrphan) rather than reporting
	// it. Its own rule on purpose: a dry run that files an escalation under
	// "dirty-worktree" is indistinguishable from the ordinary report of a dirty
	// tree whose owner is still on the host, which is exactly the fact an
	// operator has to be able to read off the plan (Rule 15).
	worktreeRuleOrphan worktreeSweepRule = "orphan-unowned"
	// worktreeRuleDirty: uncommitted work — reported, never destroyed.
	worktreeRuleDirty worktreeSweepRule = "dirty-worktree"
	// worktreeRuleTooYoung: inside the age threshold.
	worktreeRuleTooYoung worktreeSweepRule = "age-threshold"
	// worktreeRuleUnmeasurable: a fact the decision needs could not be measured.
	worktreeRuleUnmeasurable worktreeSweepRule = "unmeasurable"
	// worktreeRuleRunCap: the run's own reclamation cap was reached.
	worktreeRuleRunCap worktreeSweepRule = "run-cap"
	// worktreeRuleNoLedger: the task ledger could not be read (fail closed).
	worktreeRuleNoLedger worktreeSweepRule = "ledger-unavailable"
	// worktreeRuleOwnerEnded: the owning row's life is over, so the age floor
	// does not shield the registration.
	worktreeRuleOwnerEnded worktreeSweepRule = "owner-life-ended"
	// worktreeRuleSelfCheckout: the registration belongs to the repository THIS
	// PROCESS stands in — removing it would delete the ground under itself, so
	// it is refused here and left to the producer pass that can PROVE
	// ownership (see worktreeSweepHostObservations.SelfRepoGuestCheckout).
	worktreeRuleSelfCheckout worktreeSweepRule = "self-checkout"
	// worktreeRuleUnclaimed: nothing owns it, nobody holds it, it is clean and
	// old — the plain reclaim.
	worktreeRuleUnclaimed worktreeSweepRule = "unclaimed-reclaim"
	// worktreeRuleChatInert: a CHAT session's ground that is INERT — no ledger
	// row owns it (a chat is not a row: internal/tools/chat_ground.go), its tree
	// is clean, it is not the checkout this process stands in, no live process
	// holds it, and its session is provably over (the chats row is closed, or a
	// run of it is still standing in the ground). It replaces the AGE FLOOR on
	// the registration axis and the "dead registration" rung on the branch axis
	// — see worktreeSweepChatInert.
	worktreeRuleChatInert worktreeSweepRule = "chat-ground-inert"
	// worktreeRuleChatSessionActive: a CHAT session's ground whose session is
	// still going — the ground of a live conversation is not residue, however
	// clean and old the registration is (the chat carrier may still hand it
	// back). Kept by its own rule so the operator can tell a live chat's ground
	// from an inert one.
	worktreeRuleChatSessionActive worktreeSweepRule = "chat-session-active"
	// worktreeRuleChatCarriesWork: a CHAT ground whose session IS over, but whose
	// branch carries commits the integration branch does not have. The commits'
	// only other home is that ref, so the directory is not freed while it is 0
	// — the whole point of the 0-ahead condition in inertness, and the reason
	// this arm is separate from "the session is still going": an operator
	// reading the finding must be able to tell "somebody might still be working"
	// from "the work is committed and unpushed".
	worktreeRuleChatCarriesWork worktreeSweepRule = "chat-ground-carries-work"
	// worktreeRuleGroundChecklist: the judge said reclaim, and the
	// deterministic checklist (reclaim_ground_checklist.go) refused — the
	// registration may not be freed for a reason the judge never asks about
	// (the ground is this process's own checkout, or the branch shares no
	// ancestor with the integration branch). Stamped on the finding so the
	// plan NAMES the arm that refused it instead of reading as the judge's
	// own keep.
	worktreeRuleGroundChecklist worktreeSweepRule = "ground-checklist"
)

// worktreeSweepSelfCheckoutPrefix opens the reason of every self-checkout
// refusal (one spelling, so the console line, the operator channel and the
// tests cannot drift apart — Rules 13/14).
const worktreeSweepSelfCheckoutPrefix = "this process stands in this registration's own repository"

// worktreeSweepNoRootLabel is what a finding's Root field says when the arm that
// decided it reports no root at all — the arms decided BEFORE any root is
// resolvable (the main checkout, a bare or dead registration, a git lock) answer
// a question about git or the host, not about policy, and inventing a root for
// them would be a fabricated finding (Rule 15). It is NOT a worktreeSweepRule:
// it names no arm, it is the word the renderer uses where a role would be.
const worktreeSweepNoRootLabel = "no-root"

// worktreeSweepOptions is the resolved policy for one run.
type worktreeSweepOptions struct {
	// DryRun: plan only. The DEFAULT for the operator verb.
	DryRun bool
	// MinAge is the age threshold (condition ⑤).
	MinAge time.Duration
	// Max caps one run's reclamations.
	Max int
	// Now is the clock seam (tests pin it; production passes time.Now()).
	Now time.Time
}

// WorktreeSweepFlags carries the operator verb's raw argv values into the
// resolver, so the CLI stays free of policy and the knobs have ONE reader.
type WorktreeSweepFlags struct {
	// DryRun: plan only (the DEFAULT; a bare run reports). It WINS over Apply:
	// a caller that passes both gets a plan, never a removal (the sibling
	// verbs' contract — an invocation carrying both spellings is read as "show
	// me first", and the CLI refuses that argv outright so nobody has to rely
	// on the precedence).
	DryRun bool
	// Apply: the explicit opt-in to removal (destructive only with DryRun
	// clear).
	Apply bool
	// MinAgeH / Max: the raw flag values ("" = not given).
	MinAgeH string
	Max     string
}

// WorktreeSweepOptionsFromFlags resolves the operator verb's options: the flags
// win, else the environment, else the measured defaults. The dry-run default is
// enforced HERE (a caller must pass Apply to get a destructive run), and the
// age/max environment readers live in exactly one place.
func WorktreeSweepOptionsFromFlags(flags WorktreeSweepFlags) worktreeSweepOptions {
	// DryRun is honored from EITHER spelling — a flags struct carrying both is
	// its caller refusing to pick, and planning is the safe reading. Apply is
	// the only destructive opt-in, and it is what the operator verb feeds.
	opts := worktreeSweepOptions{DryRun: !flags.Apply || flags.DryRun, Now: time.Now()}
	if hours, err := strconv.Atoi(strings.TrimSpace(flags.MinAgeH)); err == nil && hours > 0 {
		opts.MinAge = time.Duration(hours) * time.Hour
	}
	if n, err := strconv.Atoi(strings.TrimSpace(flags.Max)); err == nil && n > 0 {
		opts.Max = n
	}
	return worktreeSweepOptionsFrom(opts.DryRun, opts.MinAge, opts.Max)
}

// WorktreeSweepRun executes the sweep (the exported face the CLI and any
// resident tick share).
func WorktreeSweepRun(ctx context.Context, opts worktreeSweepOptions) worktreeSweepResult {
	return worktreeSweepRun(ctx, opts)
}

// WorktreeSweepJSON renders a result for the operator.
func WorktreeSweepJSON(res worktreeSweepResult) string { return worktreeSweepJSON(res) }

// WorktreeReapDisposition / WorktreeReapBranchDisposition export the two
// dispositions that mean "git bookkeeping was dropped" — the resident tick
// (cmd/agent) filters the findings on them to log what it reaped, and a bare
// "reap" literal at that call site is exactly the drift Rule 1b forbids.
const (
	WorktreeReapDisposition       = worktreeReap
	WorktreeReapBranchDisposition = worktreeReapBranch
	// WorktreeOrphanEscalateDisposition is the escalation the resident tick
	// reports to the operator: a DIRTY registration NO ledger row owns (see
	// worktreeEscalateOrphan). Exported for the same reason the two above are —
	// the tick filters findings on it, and a bare "escalate" literal there is
	// the drift Rule 1b forbids.
	WorktreeOrphanEscalateDisposition = worktreeEscalateOrphan
)

// worktreeOrphanEscalationMarker is the ONE spelling every orphan escalation
// carries: the reason sentence leads with it, the same value leads the alarm
// summary, and a test greps for it — so the console line, the operator channel
// and the assertion cannot drift apart (Rule 14).
const worktreeOrphanEscalationMarker = "ORPHAN WORKTREE"

// worktreeSweepOptionsFrom resolves the options: the caller's dry-run/age/max
// arguments win when set, else the environment, else the measured defaults. It
// is the ONE reader of both knobs (Rule 2).
func worktreeSweepOptionsFrom(dryRun bool, minAge time.Duration, max int) worktreeSweepOptions {
	opts := worktreeSweepOptions{DryRun: dryRun, MinAge: minAge, Max: max, Now: time.Now()}
	if opts.MinAge <= 0 {
		opts.MinAge = DefaultWorktreeSweepMinAge
		if raw := strings.TrimSpace(os.Getenv(worktreeSweepMinAgeEnvName)); raw != "" {
			if hours, err := strconv.Atoi(raw); err == nil && hours > 0 {
				opts.MinAge = time.Duration(hours) * time.Hour
			} else {
				fmt.Fprintf(os.Stderr, "sweep-worktrees: %s=%q is not a positive hour count — using the default %v\n", worktreeSweepMinAgeEnvName, raw, opts.MinAge)
			}
		}
	}
	if opts.Max <= 0 {
		opts.Max = DefaultWorktreeSweepMax
		if raw := strings.TrimSpace(os.Getenv(worktreeSweepMaxEnvName)); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				opts.Max = n
			} else {
				fmt.Fprintf(os.Stderr, "sweep-worktrees: %s=%q is not a positive count — using the default %d\n", worktreeSweepMaxEnvName, raw, opts.Max)
			}
		}
	}
	return opts
}

// WorktreeSweepGitObserver observes one git invocation of the sweep. It exists
// so the "ONE list and ONE prune per repo" rule can be ASSERTED (a test counts
// the invocations) rather than merely documented — the failure mode it guards
// is a per-row prune that misattributes one registration's removal to another
// row's judgement. nil in production: the hook costs nothing when unset.
type WorktreeSweepGitObserver interface {
	Observe(args []string)
}

// worktreeSweepGitObserverHook is the process-wide observer (a test installs
// one; production leaves it nil).
var worktreeSweepGitObserverHook WorktreeSweepGitObserver

// InstallWorktreeSweepObserver installs a git-invocation observer and returns
// the previous one (nil when none was installed), so a caller can restore it.
//
// It is exported because the sweep's REACHABILITY must be provable from
// outside this package: the defect it closes had a wiring test that called the
// sweep directly (green) while no resident tick ever invoked it (the actual
// bug). An observer lets a caller assert "the production entry point made the
// sweep RUN" — evidence of reachability, not of existence — which is the only
// assertion that catches that class of defect.
func InstallWorktreeSweepObserver(obs WorktreeSweepGitObserver) WorktreeSweepGitObserver {
	prev := worktreeSweepGitObserverHook
	worktreeSweepGitObserverHook = obs
	return prev
}

// worktreeSweepObserve reports one git invocation to the observer, if any.
func worktreeSweepObserve(args ...string) {
	if worktreeSweepGitObserverHook != nil {
		worktreeSweepGitObserverHook.Observe(args)
	}
}

// worktreeRegistration is one `git worktree list --porcelain` entry.
type worktreeRegistration struct {
	Repo   string
	Path   string
	Head   string
	Branch string
	// Detached / Locked / Prunable mirror git's own annotations: a detached
	// registration has no branch to match against the ledger (so it can only be
	// reclaimed on the age + holder + clean evidence), a locked one is git's own
	// "do not touch" flag, and a prunable one is already gone.
	Detached bool
	Locked   bool
	Prunable bool
	Bare     bool
	// Main is true for the FIRST entry — always the main checkout, which is
	// never a target (removing it would delete the repository).
	Main bool
}

// worktreeSweepFinding is one verdict, rendered to the operator and returned to
// the caller. Findings are the deliverable of a dry run, so every one carries
// its reason.
type worktreeSweepFinding struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Branch      string `json:"branch,omitempty"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
	AgeHours    int    `json:"age_hours"`
	// Rule names the POLICY ARM that decided this registration, and Root names
	// the root it was judged against. Both are the dry run's deliverable: a plan
	// that says "report" without saying WHICH rule refused it (and which
	// declared root would bring it into policy) is a plan the operator cannot
	// act on (Rule 15). They are set at the one point where the verdict is
	// formed (worktreeSweepTagFinding), so they cannot drift from the
	// disposition they describe.
	Rule string `json:"rule,omitempty"`
	Root string `json:"root,omitempty"`
}

// worktreeSweepResult is the whole run.
type worktreeSweepResult struct {
	Findings    []worktreeSweepFinding `json:"findings"`
	Repos       []string               `json:"repos"`
	DryRun      bool                   `json:"dry_run"`
	Reclaimed   int                    `json:"reclaimed"`
	WouldReclai int                    `json:"would_reclaim"`
	// Reaped counts DEAD registrations whose git bookkeeping was dropped (the
	// directory was already gone — no working tree was destroyed). They are
	// counted apart from Reclaimed for the operator's sake: the two mean
	// different things ("a checkout was recycled" vs "git had forgotten
	// something that was already gone"), and a system that cannot tell them
	// apart cannot tell a recycle from a repair.
	Reaped int `json:"reaped"`
	// WouldReap is the dry-run counterpart of Reaped.
	WouldReap int `json:"would_reap"`
	// BranchesReaped counts branches deleted ALONGSIDE a dead registration —
	// always through `git branch -d`, so git's own merge check authorises every
	// one of them (Rule 15: this number is the only evidence the sweep ever
	// removes a ref, and it must be reported, never inferred).
	BranchesReaped int `json:"branches_reaped"`
	// Escalated counts registrations handed to a HUMAN this run — the dirty,
	// ownerless leftovers nothing else on the host can reach
	// (worktreeEscalateOrphan). They are counted apart from every removal
	// counter on purpose: this number must never be read as a recycle, and a
	// system that cannot tell "an operator was asked" from "a directory was
	// taken" cannot be audited (Rule 15).
	Escalated int `json:"escalated"`
	// Pruned is what `git worktree prune -v` REPORTED removing this run, one
	// line per registration. The prune is repo-global (git offers no scoped
	// form — measured), so a line here may name a registration the sweep never
	// judged: relaying them is what keeps the run's side effects fully
	// accounted for (Rule 15) instead of a silent repo-wide mutation.
	Pruned []string `json:"pruned,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// tasksRowForSweep is the ledger evidence about one task.
type tasksRowForSweep struct {
	TaskID      string
	Status      string
	Worktree    string
	Branch      string
	Repo        string
	Deliverable string
	// EndedAt is the row's ended_at stamp (0 = never ended). It is the
	// ledger's own declaration that the row's life is over, and it is what
	// lets the sweep tell an ownership record apart from a row that still
	// holds ground (see worktreeSweepRowHoldsGround).
	EndedAt float64
	// HolderProvenDead records that this host can PROVE the row's recorded
	// runner is gone (finishCleanupDeadLocalHolder — the same classifier the
	// finish-cleanup arms consult, so the two can never drift, Rule 14).
	//
	// It is spelled as the PROOF rather than as "HolderLive" on purpose: the
	// zero value of this struct must stay the FAIL-CLOSED direction, so any
	// evidence set that does not carry a holder verdict (a caller that builds
	// rows by hand, a future loader that forgets the column) protects the
	// registration instead of freeing it. "Unknowable" is never "dead".
	HolderProvenDead bool
	// ClaimedBy is the row's claim stamp (`host:pid`), kept beside
	// HolderProvenDead because the two are the SAME evidence read at two
	// strengths: HolderProvenDead is the derived verdict, the stamp is the
	// value an operator (or the branch-attribution journal) can re-probe. A
	// consumer that needs to say WHY a row reads dead must not have to re-query
	// the ledger for a column this loader already read (Rule 15: a verdict
	// nobody can re-derive is not an explanation).
	ClaimedBy string
	// Ident is the row's recorded RUNNER identity (pid + start ticks + cmd) —
	// the cross-session-readable triple that survives the ptrace_scope=1 hole
	// a /proc cwd probe falls into (incident 2026-09-14). It is carried, not
	// re-derived: one capture, one verdict, both consumers (Rule 14).
	Ident procIdentity
}

// worktreeSweepSelfOwnedRepo answers the repo THIS PROCESS stands in — the
// main checkout of the repository the process's own working directory belongs
// to ("" when the cwd is in no repository). It is the identity half of the
// self-checkout rung, and it is deliberately resolved from the PROCESS's own
// cwd rather than from the run's repo list: the repo list is exactly what
// enrols the live repository in the first place (worktreeSweepRepos), so
// answering the question from it would make the guard say yes to every repo it
// was asked about.
//
// The result is memoized per CWD, and the KEY IS THE MEASURED CWD, never the
// process alone: a process's repo is a property of where it is STANDING, and
// this one DOES move. The "the pass chdir'ing mid-run is not a shape this code
// has" assumption this memo used to make was FALSE, and the falsification was
// measured (2026-09-25, `gopi session resume`): the resume producer chdirs INTO the
// row's deliverable worktree to work there (cli_resume.go resumeChdirToWorktree)
// and its close-out then hands the cwd BACK (worktreerelease) — the designed
// order, precisely so the directory it worked in becomes removable. A memo that
// outlived the chdir kept answering "this process stands in the worktree" from
// a process that had already left it, so the release arm refused to free the
// very directory the release had just made safe:
//
//	[gopi] resume: working in deliverable.worktree <wt>
//	[ownref] cwd="/" memo="<wt>" candidate="<wt>"   ← stale identity refuses
//	[gopi] finish-cleanup: skipped: own-ground refusal …
//
// The identity of "where I stand" is therefore resolved ONCE PER CWD and
// re-resolved the moment the cwd changes (one `git rev-parse` pair per distinct
// directory a process visits — the same cost shape as before for a process that
// does not move, which is nearly all of them). The gate itself is NOT weakened:
// a process that really IS standing in the candidate directory still resolves
// that directory and still refuses.
func worktreeSweepSelfOwnedRepo() string {
	return worktreeSweepSelfPosition().Repo
}

// worktreeSweepSelfPosition is the ONE reader of "where does this process
// stand?" — the memoized self-identity, keyed on the cwd it was measured from
// (see worktreeSweepSelfOwnedRepo for why the key is the cwd and not the
// process). Callers that need more than the repo root — the common git dir, the
// guest bit — read them from this ONE struct, so the three facts can never
// describe different moments.
func worktreeSweepSelfPosition() worktreeSweepSelfIdentity {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	worktreeSweepSelfMu.Lock()
	defer worktreeSweepSelfMu.Unlock()
	if cwd != "" && cwd == worktreeSweepSelfCwd {
		return worktreeSweepSelf
	}
	id := worktreeSweepResolveSelf(cwd)
	worktreeSweepSelf = id
	// The cwd is recorded even when resolution failed (unreadable cwd, a
	// directory in no repository): the FAILED answer belongs to this cwd too,
	// and re-running git for every call from the same place would be cost with
	// no new information. A cwd that could not be read is never cached ("" is
	// not a location), so that case retries — the fail-safe direction is to
	// measure again, never to keep an older answer.
	worktreeSweepSelfCwd = cwd
	return id
}

// worktreeSweepResolveSelf measures the identity from one cwd, with no cache in
// the way — the cheap pure-ish half, so the memo's own bookkeeping and the
// measurement can be read apart.
func worktreeSweepResolveSelf(cwd string) worktreeSweepSelfIdentity {
	if cwd == "" {
		return worktreeSweepSelfIdentity{}
	}
	root, rerr := repoRootOf(cwd)
	if rerr != nil {
		return worktreeSweepSelfIdentity{}
	}
	id := worktreeSweepSelfIdentity{Repo: scratch.CanonicalPath(root)}
	// The REPOSITORY IDENTITY of that cwd, in the same pass so the two facts can
	// never describe different moments: a repository's identity is its COMMON
	// git dir (`rev-parse --git-common-dir`), which a linked worktree SHARES
	// with its main checkout. Comparing top-level paths instead would say "not
	// my repo" precisely when the process stands in a linked worktree of it —
	// the measured case this rung exists for.
	common, cerr := worktreeSweepCommonGitDir(root)
	if cerr != nil {
		return id
	}
	id.Common = scratch.CanonicalPath(common)
	// The GUEST half: the process stands in a LINKED worktree rather than in
	// the repository's main checkout — i.e. its top level is not the dir
	// holding that common git dir's `.git`.
	id.Guest = id.Repo != scratch.CanonicalPath(worktreeSweepMainCheckoutOf(common))
	return id
}

// worktreeSweepMainCheckoutOf returns the directory whose `.git` IS the given
// common git dir — the repository's main checkout, for a common dir laid out the
// ordinary way (<checkout>/.git). A bare/mirror common dir (no such parent
// shape) returns "", which makes the guest comparison say "guest" — the
// conservative direction: a process in an unrecognisable layout must not be the
// one holding eviction authority over its peers.
func worktreeSweepMainCheckoutOf(common string) string {
	if common == "" || filepath.Base(common) != worktreeSweepGitdirName {
		return ""
	}
	return filepath.Dir(common)
}

// worktreeSweepSelfIdentity is the memoized answer to "where does this process
// stand, and what is that place?": the repo root, the repo's COMMON git dir
// (its identity), and whether the process is a GUEST there (standing in a linked
// worktree rather than in the main checkout). The three travel TOGETHER because
// they are measured together and a reader that mixed one call's repo with
// another call's common dir would be comparing facts from two different places.
type worktreeSweepSelfIdentity struct {
	Repo   string
	Common string
	Guest  bool
}

// worktreeSweepSelfMu / worktreeSweepSelf / worktreeSweepSelfCwd are the memo
// and its KEY (see worktreeSweepSelfPosition): one measure per distinct cwd.
var (
	worktreeSweepSelfMu  sync.Mutex
	worktreeSweepSelf    worktreeSweepSelfIdentity
	worktreeSweepSelfCwd string
)

// worktreeSweepSelfGuestCheckout reports whether a registration of repo must be
// withheld from this pass because the process is a GUEST in that very repo —
// standing in a linked worktree of it, not in its main checkout. Both halves are
// required: the repo must be the process's own (an unrelated repo is swept
// normally) AND the process must be a guest (a resident keeps its mandate over
// its own repo — see the observation's doc).
//
// "The process's own repo" is decided on the COMMON git dir (see
// worktreeSweepSelfOwnedRepo): a linked worktree and its main checkout are one
// repository, so a guest standing in either must recognise the other. An
// unresolvable self-identity (a cwd in no repository, an unreadable common dir)
// refuses nothing: the guard may only withhold ground it can positively
// identify, and inventing an identity would refuse work on unrelated repos. An
// unreadable common dir for the CANDIDATE repo also refuses nothing — the
// enumeration already failed for it, and this rung adds no new failure mode.
func worktreeSweepSelfGuestCheckout(repo string) bool {
	self := worktreeSweepSelfPosition()
	if self.Common == "" || repo == "" || !self.Guest {
		return false
	}
	common, err := worktreeSweepCommonGitDir(repo)
	if err != nil {
		return false
	}
	return scratch.CanonicalPath(common) == self.Common
}

// worktreeSweepHostObservations carries the facts that only the live host can
// answer for a registration, so the decision stage stays pure.
type worktreeSweepHostObservations struct {
	// CreatedAt is when the registration was created (zero = not measurable —
	// which KEEPS the registration rather than reclaiming it).
	CreatedAt time.Time
	// DirtyReason is "" when the worktree is clean, else the "could not be
	// read" evidence — the path was gone or `git status` failed there.
	DirtyReason string
	// DirGone records, as an INDEPENDENT measurement (not merely the failure of
	// the git probe above), that the registration's worktree directory does not
	// exist. It is the reap's whole evidence: a registration whose tree is gone
	// holds no work, only git bookkeeping — and that bookkeeping is exactly
	// what survives forever (prune SKIPS a locked entry, and nothing else ever
	// revisits it; measured: two entries from 2026-09-13 23:40 still present
	// with `locked: initializing` after their owners were SIGKILLed).
	DirGone bool
	// SelfRepoGuestCheckout records that the registration belongs to the
	// repository THIS PROCESS stands in AND that the process is a GUEST there —
	// it stands in a LINKED worktree of that repository rather than in its main
	// checkout. Both halves are needed, and the second is what makes the rung
	// safe (see worktreeSweepSelfIdentity.Guest).
	//
	// WHY THIS RUNG EXISTS (measured 2026-09-21). The sweep's repo set is the
	// settings `repos` table PLUS the repo the process's CWD belongs to
	// (worktreeSweepRepos). A process may be sandboxed in every OTHER respect —
	// GOPI_HOME, GOPI_MEMORY_DB, a settings file whose repos table is empty —
	// while its WORKING DIRECTORY is the repository under verification, because
	// that is what "verify this checkout" means; and its ledger is a PER-TASK
	// row, so every other task's claims are structurally invisible to it. With
	// the actor blind by construction, every remaining rung can pass on a peer
	// task's registration: clean (nothing to report), no live holder, past the
	// age floor, inside a declared root. Observed in this session: while
	// `go test ./cmd/agent/` ran from a linked worktree, the sibling linked
	// worktree /home/nick/workspace/probe-run — clean, unowned in the sandbox
	// ledger that run pointed at, registered three days earlier — was removed
	// mid-run (t+147s): the directory went and the branch stayed.
	//
	// WHY GUEST, NOT MERELY "SAME REPO": the resident sweep stands in the MAIN
	// checkout and its mandate over that repo's registrations is the whole
	// point of the pass (it is the entity that reaps its own repo's residue,
	// including dead registrations nobody else can collect). That mandate is
	// removed only for the process whose position makes a peer look like an
	// anonymous occupant. Two measured consequences of the narrower criterion:
	// a guest still reclaims freely in OTHER repos (every test fixture, every
	// unrelated repo a verification host stands in), and a resident still
	// reclaims its own repo's unowned registrations exactly as before.
	SelfRepoGuestCheckout bool
}

// worktreeSweepTagFinding stamps a finding with the policy arm that decided it
// and the root it was judged against, returning the finding. It is applied once
// per finding at the single point where the verdict is formed (the judge's
// return), so the two labels can never describe a different verdict than the
// disposition beside them.
func worktreeSweepTagFinding(f worktreeSweepFinding, rule worktreeSweepRule, root worktreeroots.Root) worktreeSweepFinding {
	f.Rule = string(rule)
	f.Root = worktreeSweepRootLabel(root)
	if f.Root == "" {
		// A finding that reports NO root says exactly that, in the one spelling
		// the renderers share, instead of leaving the field empty: an empty
		// string is indistinguishable from a field nobody set, which is the
		// defect this stamping exists to close.
		f.Root = worktreeSweepNoRootLabel
	}
	return f
}

// worktreeSweepRootLabel renders one root the way BOTH the finding's Root field
// and the roots named inside a reason are rendered: role (when the root has
// one), path, and the source that says where the root came from. One renderer,
// so a refusal's list of consulted roots cannot be spelled differently from the
// root the finding reports (Rule 14). A root with no path renders as "" — the
// caller decides what that means.
func worktreeSweepRootLabel(root worktreeroots.Root) string {
	if root.Path == "" {
		return ""
	}
	if root.Role == "" {
		return root.Path + " [" + root.Source + "]"
	}
	return string(root.Role) + " " + root.Path + " [" + root.Source + "]"
}

// worktreeSweepConsultedRoot returns the root an OUT-OF-POLICY refusal REPORTS.
//
// PermitsAny hands back the FIRST root that permitted the path and NOTHING at
// all when none did — so the refusal would name no root precisely when the
// operator most needs to see what was consulted (the reason beside it promises
// the memory-base remedy). The reported root is therefore the memory base the
// SCRATCH HOME resolves onto, derived by the SAME measurement the sweep's own
// implicit root uses (memoryBaseRoot, worktree_sweep_memory_base.go), so the
// label can never describe a different filesystem than the remedy names.
//
// It carries the path and the source of that measurement and NO role, because a
// role is what AUTHORISES an operation and worktreeroots.Permits refuses a
// roleless root — so labelling a refusal can never widen what the sweep may
// remove. On a host with no memory base it stays empty: a fabricated root would
// be worse than the silence it replaces (Rule 15).
func worktreeSweepConsultedRoot(permitted worktreeroots.Root) worktreeroots.Root {
	if permitted.Path != "" {
		return permitted
	}
	base, ok := memoryBaseRoot()
	if !ok {
		return worktreeroots.Root{}
	}
	return worktreeroots.Root{Path: memoryBaseDeclaredRoot(base).Path, Source: memoryBaseSourcePrefix}
}

// worktreeSweepOutOfPolicyReason states the declared-root refusal WITH the roots
// that were consulted named (Rule 15's positive half — the same way the reclaim
// arm names its root) and the remedy this arm promises spelled out. A refusal
// that says "outside policy" without saying which policy was read is a plan the
// operator cannot act on; the consulted list IS that policy.
func worktreeSweepOutOfPolicyReason(roots []worktreeroots.Root, consulted worktreeroots.Root) string {
	var b strings.Builder
	b.WriteString("outside every declared root (consulted: ")
	if len(roots) == 0 {
		b.WriteString("no root at all")
	} else {
		for i, root := range roots {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(worktreeSweepRootLabel(root))
		}
	}
	b.WriteString(") — not swept; declare the repo's worktree_root")
	if consulted.Path == "" {
		b.WriteString(" to bring it into policy (no memory-base root is available here: ")
		b.WriteString(memoryBaseRootSkipped(scratch.CanonicalPath(scratch.HomeDir())))
		b.WriteString(")")
		return b.String()
	}
	b.WriteString(", or make the scratch home resolve onto the ")
	b.WriteString(string(worktreeroots.RoleMemoryBase))
	b.WriteString(" root ")
	b.WriteString(worktreeSweepRootLabel(consulted))
	b.WriteString(", to bring it into policy")
	return b.String()
}

// worktreeSweepJudge is the decision for ONE registration — the whole guard
// ladder, in order, each rung returning its own reason. It performs no I/O.
func worktreeSweepJudge(reg worktreeRegistration, rows []tasksRowForSweep, holders scratch.HolderScan, roots []worktreeroots.Root, opts worktreeSweepOptions, ho worktreeSweepHostObservations) worktreeSweepFinding {
	f := worktreeSweepFinding{Repo: reg.Repo, Path: reg.Path, Branch: reg.Branch, Disposition: string(worktreeKeep)}
	// judgedRoot is the root this registration was judged against; every return
	// below reports it ("" until a root has been resolved — the arms before the
	// root test answer a question about git or the host, not about policy).
	judgedRoot := worktreeroots.Root{}
	// keep/reclaim/reap/reapBranch stamp the finding with the ARM that decided
	// it, so a dry run can NAME the rule instead of printing a bare disposition
	// (the deliverable's "name the rule that would claim it").
	keep := func(rule worktreeSweepRule, reason string) worktreeSweepFinding {
		f.Reason = reason
		return worktreeSweepTagFinding(f, rule, judgedRoot)
	}
	reclaim := func(rule worktreeSweepRule, reason string) worktreeSweepFinding {
		f.Disposition = string(worktreeReclaim)
		f.Reason = reason
		return worktreeSweepTagFinding(f, rule, judgedRoot)
	}
	reap := func(reason string) worktreeSweepFinding {
		f.Disposition = string(worktreeReap)
		f.Reason = reason
		return worktreeSweepTagFinding(f, worktreeRuleDeadRegistration, judgedRoot)
	}
	reapBranch := func(reason string) worktreeSweepFinding {
		f.Disposition = string(worktreeReapBranch)
		f.Reason = reason
		return worktreeSweepTagFinding(f, worktreeRuleDeadRegistration, judgedRoot)
	}

	// ① the main checkout is never a target, and neither is anything git itself
	//    flags (locked = an explicit do-not-touch; prunable/bare = not a
	//    directory this sweeper owns).
	if reg.Main {
		return keep(worktreeRuleMainCheckout, "the main checkout (removing it would delete the repository)")
	}
	if reg.Bare {
		return keep(worktreeRuleBare, "a bare registration")
	}
	// ①D the DEAD registration — the leak this sweeper could not see. A
	//    registration whose worktree directory is GONE holds no work: what is
	//    left is git bookkeeping, and bookkeeping is precisely what nothing
	//    collects. It needs no age verdict (a missing directory cannot be a
	//    fresh checkout), but it is still refused while a live process holds the
	//    path.
	//
	//    WHY THIS ARM MUST COME BEFORE THE LOCKED ARM: `git worktree prune`
	//    SKIPS locked entries (measured: a locked dead registration survived
	//    `prune -v` untouched, twice on this host), so the previous "already
	//    prunable — the repo prune collects it" arm made exactly the one leaking
	//    shape permanently uncollectable. `remove` explicitly refuses a locked
	//    locked tree and tells the caller to unlock first; the reap does that.
	if reg.Prunable || ho.DirGone {
		if why := scratch.HolderVeto(reg.Path, holders); why != "" {
			return keep(worktreeRuleLiveHolder, "its directory is gone but "+why)
		}
		// A LIVE CREATOR'S lock is not stale (see worktreeSweepInitLockFloor):
		// the dir-gone observation can precede the checkout, so the lock's own
		// age is what distinguishes "the creator died" from "the creator is
		// still working". An unmeasurable age keeps it — the conservative half
		// of the same rule.
		if reg.Locked {
			lockAge, aerr := worktreeSweepLockAge(reg.Repo, reg.Path)
			if aerr != nil {
				return keep(worktreeRuleUnmeasurable, "its directory is gone and its lock could not be dated ("+aerr.Error()+") — keeping")
			}
			if lockAge < worktreeSweepInitLockFloor {
				return keep(worktreeRuleTooYoung, "its directory is gone but its lock is only "+lockAge.Round(time.Second).String()+
					" old — a `worktree add` may still be running (floor "+worktreeSweepInitLockFloor.String()+")")
			}
		}
		if reg.Branch != "" && !reg.Detached {
			for _, row := range rows {
				if evidence := worktreeSweepRowHoldsRegistration(row, reg, opts); evidence != "" {
					return keep(worktreeRuleLedgerOwnership, "registration is dead, but branch "+reg.Branch+" belongs to live task "+row.TaskID+" ("+row.Status+"): "+evidence)
				}
			}
			if id, evidence := worktreeSweepLiveSessionRow(rows, reg); id != "" {
				return keep(worktreeRuleSessionLive, "registration is dead, but live task "+id+" still has a run standing ("+evidence+
					") — every live run records a ground of its own and none of them is this registration, so its branch is not anybody's live ground")
			}
			// The branch is the registration's OWN (git forbids checking one
			// branch out in two worktrees) and git still refuses to delete a
			// branch that is checked out elsewhere or that carries unmerged
			// commits — so the removal is attempted with `branch -d` and a
			// refusal is a REPORTED keep, never a force (the difference between
			// "the branch is ready to recycle" and "the branch may be someone's
			// work" is exactly what `-d` decides, and it is git's decision to
			// make).
			return reapBranch("registration is dead (its directory is gone) — the branch goes with it if git agrees (`branch -d`)")
		}
		return reap("registration is dead (its directory is gone; no branch to consider)")
	}
	if reg.Locked {
		return keep(worktreeRuleGitLock, "locked by git (an explicit do-not-touch)")
	}

	// ① he declared-root test: a registration outside every declared root is not
	//    this sweeper's to remove, but it IS disk the operator should see.
	root, permitted := worktreeroots.PermitsAny(roots, reg.Path)
	if !permitted {
		// NOTHING permitted this path, so PermitsAny had no root to hand back
		// and the finding would have named nothing at all. This arm's own reason
		// promises the memory-base remedy, so it reports the root that remedy
		// names (see worktreeSweepConsultedRoot) and lists every root it
		// consulted — a refusal nobody can act on is not a refusal, it is noise
		// (Rule 15).
		root = worktreeSweepConsultedRoot(root)
	}
	judgedRoot = root
	if !permitted {
		f.Disposition = string(worktreeReport)
		f.Reason = worktreeSweepOutOfPolicyReason(roots, root)
		return worktreeSweepTagFinding(f, worktreeRuleRootScope, root)
	}

	// ①C the CHAT GROUND — the registration is a chat's ground and its own
	//    session's evidence is the axis this pass judges it on, BEFORE the two
	//    PROCESS-GLOBAL rungs below. That position is the whole content of this
	//    rung, and it is what the diagnosis document measured:
	//
	//      - rung ② (self-checkout) and rung ③'s session arm are about THE
	//        ACTOR and about SOMEBODY ELSE. They answer "is this mine to free?"
	//        and "may a stranger free it?" — neither can ever answer "is the
	//        ground's OWN session over?", which is the fact that decides this
	//        registration. A chat ground is owned by NO row (chat_ground.go:
	//        "A chat is not a row"), so on a host whose ledger carries any live
	//        row at all (this one always does — the resident worker's own row),
	//        the session arm kept every chat ground forever, whatever its own
	//        evidence said;
	//      - the session's OWN evidence is strictly stronger for this
	//        registration than a stranger's liveness: a live process in the
	//        ground is checked by the predicate itself, and a session that is
	//        closed (or whose every trace has been quiet past the grace) cannot
	//        be resurrected by a row belonging to a DIFFERENT task.
	//
	//    What it does NOT do is suppress rung ②. The self-checkout veto is
	//    about the ground THIS PROCESS stands in, it is a fact about the actor
	//    no later evidence can make safe, and a chat runs itself inside its own
	//    ground: exactly the shape where the two meet. So the veto RUNS HERE
	//    FIRST, in its own words, and the chat axis is reached only by a
	//    registration that is not the asker's own — which is also why the
	//    chat ground of the run reading this line is kept.
	if ho.SelfRepoGuestCheckout {
		return keep(worktreeRuleSelfCheckout, worktreeSweepSelfCheckoutPrefix+
			" — freeing it would delete the ground under this process; the registration belongs to the process that created it (the producer-side release), which proves ownership from the ledger's claim stamp rather than from the directory's looks")
	}
	// ② a live holder vetoes the reclaim unconditionally (and before any
	//    ledger/age reasoning, because a live process is the strongest evidence
	//    the registration is in use right now).
	if why := scratch.HolderVeto(reg.Path, holders); why != "" {
		return keep(worktreeRuleLiveHolder, why)
	}

	// ② a live holder vetoes the reclaim unconditionally (and before any
	//    ledger/age reasoning, because a live process is the strongest evidence
	//    the registration is in use right now).
	// ③ the ledger: a NON-TERMINAL row that still HOLDS GROUND owns the
	//    registration. "Holds ground" is the load-bearing half of this rung and
	//    it is deliberately NOT "the row exists" — a ledger row is an
	//    OWNERSHIP RECORD, and an ownership record outlives the process that
	//    wrote it. Rows are therefore judged on evidence that the task is
	//    still executing (worktreeSweepRowHoldsGround): its recorded runner
	//    still standing in the registration's own directory, or a live-status
	//    row whose life has not been declared over. Terminal rows never
	//    protect. The ownership clause is the deliverable tie shared with the
	//    dead-registration rung. The SESSION clause beside it is the
	//    concurrent-session half, and it asks a STRICTER question on purpose: a
	//    live-status row with a demonstrably STANDING run (positive evidence —
	//    a verified live runner identity, or a process in the row's own session
	//    directory). The ledger records ONE runner per row, so the recorded tie
	//    goes quiet the moment that runner exits while a SECOND session of the
	for _, row := range rows {
		if evidence := worktreeSweepRowHoldsRegistration(row, reg, opts); evidence != "" {
			return keep(worktreeRuleLedgerOwnership, "owned by live task "+row.TaskID+" ("+row.Status+"): "+evidence)
		}
	}
	// ③O the ORPHAN ESCALATION — rung ④'s own subject, moved to a human when
	//    NOBODY owns it. A dirty registration whose ledger row exists is a
	//    delivery in progress: the owner's own close-out (or the reclaim arms)
	//    will meet guard ② when the work is committed, and the reason names
	//    that. A dirty registration with NO row anywhere in the ledger is the
	//    one shape that is unreachable by EVERY arm on the host — measured live
	//    (`worktree-sweep-memory-base-policy`: 6 modified + 1 untracked, no row
	//    names it or its branch) — because the producer pass needs a row keyed
	//    on this host's own claim, the dead-holder arm needs a TERMINAL row, and
	//    the age sweep's guard ② refuses dirt. Such a registration is never
	//    removed here (uncommitted work + no --force is the whole design), but
	//    it is no longer silent: past the age floor it ESCALATES to the operator
	//    with the branch named, so the merge gate it pins becomes an action
	//    item instead of an invisible leak. The age floor is the same one the
	//    clean arm uses — nothing about a dirty tree makes it younger.
	//
	//    ORDER: after the ownership rungs (a live owner must keep its own dirty
	//    tree in the words the ledger speaks) and before the plain dirty report.
	//    An UNMEASURABLE age keeps the plain report: "nobody owns this and I
	//    cannot say how long it has been unowned" is not an escalation.
	if ho.DirtyReason != "" && !worktreeSweepOwnedByAnyRow(rows, reg) {
		if !ho.CreatedAt.IsZero() && opts.Now.Sub(ho.CreatedAt) >= opts.MinAge {
			f.Disposition = string(worktreeEscalateOrphan)
			f.AgeHours = int(opts.Now.Sub(ho.CreatedAt).Hours())
			f.Reason = worktreeSweepOrphanReason(reg, ho.DirtyReason, f.AgeHours)
			return worktreeSweepTagFinding(f, worktreeRuleOrphan, judgedRoot)
		}
		if !ho.CreatedAt.IsZero() {
			f.AgeHours = int(opts.Now.Sub(ho.CreatedAt).Hours())
		}
		f.Disposition = string(worktreeReport)
		f.Reason = worktreeSweepDirtyReportReason(reg, ho.DirtyReason)
		return worktreeSweepTagFinding(f, worktreeRuleDirty, judgedRoot)
	}

	// ④ uncommitted work is never destroyed — it is the deliverable a
	//    merged-but-uncommitted branch needs, so it is REPORTED.
	//
	//    THE CHAT GROUND'S OWN VERSION of that sentence says WHY the whole arm
	//    steps back here (epic chat-ground-reclaim). A chat that wrote files
	//    wrote them INTO its ground (cmd/agent/chat_ground.go chdirs there
	//    before the first turn), so an uncommitted change in one is that
	//    session's deliverable — the exact shape the chat ground was invented
	//    for, and the one shape no arm may destroy. The REASON is still the
	//    ordinary dirty report (the decision is the same), and this is not a
	//    new rung: it names the session so the operator knows there is no row to
	//    find and no branch to resume (Rule 15 — a leak the reader can act on).
	if ho.DirtyReason != "" {
		f.Disposition = string(worktreeReport)
		f.Reason = worktreeSweepDirtyReportReason(reg, ho.DirtyReason)
		return worktreeSweepTagFinding(f, worktreeRuleDirty, judgedRoot)
	}

	// ④C the CHAT GROUND — asked AFTER the dirty guard and BEFORE the two
	//    PROCESS-GLOBAL rungs left in this ladder (the session arm below, and
	//    the age floor at ⑤). BOTH halves of that position are load-bearing:
	//
	//      - AFTER ④, because the dirty guard is about the GROUND's CONTENT
	//        and no reclaim arm may run before it. A chat ground is where a
	//        chat does its work (cmd/agent/chat_ground.go chdirs the session
	//        there), so an uncommitted change in one is that session's
	//        DELIVERABLE — measured while building this arm: with the chat
	//        rung placed before ④, a dirty ground was handed to
	//        `git worktree remove` and only git's own refusal stopped it
	//        ("contains modified or untracked files"). Relying on a tool's
	//        refusal for a rule this cheap is exactly what the ladder's order
	//        exists to avoid, and the escalation for a dirty ownerless ground
	//        is ③O's, which has already run above;
	//      - BEFORE the session arm and ⑤, because both are about SOMEBODY
	//        ELSE — a stranger's live row, or a clock — while this rung answers
	//        the registration's OWN question. A chat ground is owned by NO row
	//        by design (chat_ground.go: "A chat is not a row"), so on a host
	//        whose ledger carries any live row at all (this one always does —
	//        the resident worker's own row) the session arm kept every chat
	//        ground forever, whatever its own evidence said. That was one of
	//        the two process-global reasons the diagnosis document measured.
	//
	//    The self-checkout veto is NOT suppressed by this rung: it sits at ①B,
	//    above, and a chat runs itself inside its own ground — exactly the
	//    shape where the two meet — so the ground of the run reading this line
	//    is kept by that veto, in its own words.
	if stem := worktreeSweepChatSessionStem(reg); stem != "" {
		// A CHAT CARRIER STANDS IN ITS OWN GROUND for the whole session, so a
		// live process whose cwd is inside the registration IS that session —
		// and this check is what keeps a chat ground that has no ledger row to
		// speak for it. Asked BEFORE the index, so a running conversation is
		// never even described as inert. A chat
		// carrier stands in its own ground for the whole session
		// (cmd/agent/chat_ground.go chdirs into it before the first turn), so a
		// live process whose cwd is inside the registration IS that session —
		// and this check is what keeps a chat ground that has no ledger row to
		// speak for it. It is asked BEFORE the index so a running conversation
		// is never even described as inert.
		if why := scratch.HolderVeto(reg.Path, holders); why != "" {
			return keep(worktreeRuleChatSessionActive,
				"it is a chat session's ground ("+reg.Branch+") and "+worktreeSweepChatHolderPhrase(why))
		}
		inert, evidence := worktreeSweepChatInert(reg, holders, opts, ho.CreatedAt)
		if !inert {
			return keep(worktreeRuleChatSessionActive, "it is a chat session's ground ("+reg.Branch+") and its session is not provably over: "+evidence)
		}
		return reclaim(worktreeRuleChatInert, "chat ground "+stem+" is inert — "+evidence)
	}

	// ⑤ the age threshold, measured on the creation record. An UNMEASURABLE age
	//    keeps the registration: "I could not tell how old this is" is not a
	//    licence to remove it. The threshold's PURPOSE is to leave alone a
	//    registration that some task just created, and it is measured on the
	//    registration alone — so a same-day orphan (a worktree whose creating
	//    run is long over but whose registration record carries today's date)
	//    reported age=0h and could never be reclaimed that day, no matter how
	//    conclusively its owner was gone. When the ledger says the owning row's
	//    life IS over, that purpose is already satisfied by direct evidence,
	//    and the floor must not override it (see worktreeSweepOwnerEnded).
	if ho.CreatedAt.IsZero() {
		return keep(worktreeRuleUnmeasurable, "registration age could not be measured (missing commondir/backlink) — keeping")
	}
	age := opts.Now.Sub(ho.CreatedAt)
	f.AgeHours = int(age.Hours())
	ended, endedRow := worktreeSweepOwnerEnded(rows, reg, opts)
	if age < opts.MinAge && !ended {
		return keep(worktreeRuleTooYoung, "younger than the threshold ("+age.Round(time.Minute).String()+" < "+opts.MinAge.String()+")")
	}

	// ⑤+ the CONCURRENT-SESSION arm — the 2026-09-17 incident's guard (row
	//    01a0af50): a row's own branch is this registration's branch while its
	//    recorded ground is a DIFFERENT directory, and a second session of that
	//    row is demonstrably standing in the registration. The ledger records ONE
	//    runner per row, so the deliverable tie (rung ③, which asks whether the
	//    row HOLDS ground) went quiet the moment that runner exited, and the
	//    registration was removed under a live run — the branch then became
	//    reachable from no ref.
	//
	//    ITS POSITION IS THE FIX (task 01a130ae, the 2026-10-12 residue). The arm
	//    used to sit BETWEEN rung ③ and the dirty guard, where it returned the
	//    first live row with a standing run and kept ANY registration on that
	//    evidence — so a one-shot detached worktree carrying uncommitted mutation
	//    material was never escalated, never reported and never reclaimed, and
	//    the sentence the operator read claimed the registration WAS that row's
	//    ground while the row's ground was another directory. A stranger's
	//    liveness is not a fact about this registration and may not outrank the
	//    facts that are (its branch, its dirt, its age, the ledger's ties), so the
	//    arm now runs where its evidential strength belongs — at the END, after
	//    every question about the registration itself has been asked — and it
	//    keeps only what its own evidence names: see
	//    worktreeSweepLiveSessionRow (the branch tie).
	if reg.Branch != "" {
		if id, evidence := worktreeSweepLiveSessionRow(rows, reg); id != "" {
			return keep(worktreeRuleSessionLive, "no live task's deliverable names this registration, but live task "+id+
				" still has a run standing ("+evidence+") — branch "+reg.Branch+" is that row's branch, so this may be its second session's ground")
		}
	}

	// Ownership for the report: a registration whose task row is TERMINAL is
	// reclaimed with the terminal task named; one with NO row at all (a session
	// that ended before the ledger knew, or a fixture's own worktree) is
	// reclaimed too — with the evidence, not a shrug.
	for _, row := range rows {
		if row.terminal() && (sameWorkingPath(row.Worktree, reg.Path) || row.Branch == reg.Branch) {
			return reclaim(worktreeRuleOwnerEnded, "task "+row.TaskID+" is "+row.Status+", no live holder, clean, age "+age.Round(time.Minute).String())
		}
	}
	// The owning row's life is over even though its status never went
	// terminal: name that row and say why the age floor did not apply, so the
	// reason carries the evidence that licensed the reclaim (Rule 15).
	if endedRow != "" {
		return reclaim(worktreeRuleOwnerEnded, "task "+endedRow+" recorded its run as ended, no live holder, clean, registration age "+
			age.Round(time.Minute).String()+" (a finished run's registration — the age floor does not shield it)")
	}
	return reclaim(worktreeRuleUnclaimed, "no task row owns it, no live holder, clean, age "+age.Round(time.Minute).String()+" (root "+string(root.Role)+": "+root.Source+")")
}

// terminal reports whether a task row's status is one of the terminal states —
// the same closed set the reclaim path uses, referenced rather than re-spelled
// (Rule 13).
func (r tasksRowForSweep) terminal() bool {
	switch r.Status {
	case taskStatusCompleted, taskStatusFailed, taskStatusCancelled, taskStatusVerified, taskStatusSuperseded:
		return true
	}
	return false
}

// worktreeSweepRowHoldsGround answers the ONE question rungs ①D and ③ both ask:
// does this live-status row still hold real ground, or is it merely an
// ownership RECORD the ledger kept after its run was over?
//
// A ledger row is a record, and a record outlives the process that wrote it —
// so "a non-terminal row exists" is not by itself evidence that anyone is
// still in the directory. Two independent bits of evidence count, and a row
// carrying neither is not an owner:
//
//	(a) the row's recorded runner is not PROVABLY gone (HolderProvenDead — the
//	    same classifier the finish-cleanup arms already use). A row whose own
//	    runner record is dead is not executing, whatever else the row says;
//	    "no readable identity" is not that proof and keeps the row an owner;
//	(b) the row's life has not been declared over — a live status with no
//	    ended_at stamp, or an ended_at still inside worktreeSweepEndedGrace
//	    (the enqueue window).
//
// Both consumers of ownership (the path arm and the branch arm) call THIS, so
// a row cannot protect ground through one rung's evidence and not the other's.
func worktreeSweepRowHoldsGround(row tasksRowForSweep, opts worktreeSweepOptions) bool {
	if !taskLiveHolderStatus(row.Status) {
		return false
	}
	return !row.HolderProvenDead && !worktreeSweepRowLifeEnded(row, opts)
}

// worktreeSweepRowSessionLive returns the NAMED evidence that a live row's run
// is still breathing ("" = none), for the session arm. The question itself lives
// in worktreeSweepSessionLiveEvidence (one implementation, shared with the
// reclaim arm's Guard ⓪).
func worktreeSweepRowSessionLive(row tasksRowForSweep) string {
	return worktreeSweepSessionLiveEvidence(row.Status, row.Ident, row.TaskID)
}

// worktreeSweepSessionLiveEvidenceFor is worktreeSweepSessionLiveEvidence asked
// with an explicit set of pids that are NOT THIRD PARTIES to the asker
// (finishCleanupSelfTree — the asker, its ancestry and its live descendants).
// Every bit of evidence this predicate could name about such a pid is the asker
// answering its own question, so the pid is skipped.
//
// WHY THE PRODUCER ARM NEEDS THE EXCLUSION AND THE SWEEP DOES NOT (row
// 01a0d58e): the ledger-keyed own-release arm selects candidates by
// `claimed_by = MY claim stamp`, so the row's recorded runner IS the process
// asking the question and its pid is alive by construction — the ledger's own
// writer stamps the executing process (taskUpdateStampedFrom), which means the
// unexcluded predicate refused EVERY legitimate release of a run's own ground.
// The sweep asks the same question about rows it did NOT run, where nothing is a
// self-match; it therefore passes nil and its evidence is unchanged. One
// predicate, one reading of "third party" (Rules 13/14) — never a second
// implementation of either.
func worktreeSweepSessionLiveEvidenceFor(status string, ident procIdentity, taskID string, selfPid int) string {
	if !taskLiveHolderStatus(status) {
		return ""
	}
	// ① the recorded runner — SKIPPED when it is the ASKER ITSELF. Only this
	// bit may be skipped: the recorded runner is the one identity the asker's own
	// ledger write put there (taskUpdateStampedFrom), so the asker asking about it
	// is answering itself.
	//
	// The skip must fall THROUGH to ②: the concurrent-session window (a foreign
	// process still working the row while the recorded runner is quiet) is exactly
	// the case this arm still has to catch, and a bare early return here would
	// have reopened it the moment the asker's own runner was excluded.
	if finishCleanupProcessHolderLive(ident) && !(selfPid > 0 && ident.Pid == selfPid) {
		return "its recorded runner (pid " + strconv.Itoa(ident.Pid) + ") is still running"
	}
	// ② the session-directory scan, asked UNEXCLUDED — deliberately (measured
	// 2026-09-25, this row). Excluding the asker here too broke the 2026-09-17
	// concurrent-session guard: its regression fixture places the asking process
	// INSIDE the row's session directory (live_row_session_veto_test.go,
	// sessionVetoChdir), so "the asker stands in the session dir" IS that guard's
	// evidence — five veto tests went red the moment this bit honoured an
	// exclusion. A process standing in a row's session directory is a run
	// standing, whoever it is.
	if dir := worktreeSweepSessionDir(taskID); dir != "" {
		if processInsideSessionDir(dir) {
			return "a live process works in its session directory " + dir
		}
	}
	return ""
}

// worktreeSweepSessionLiveEvidence is the ONE implementation of the
// cross-session liveness question — "does this row have a run standing right
// now?" — shared by the sweep's session arm and the reclaim arm's Guard ⓪, so
// the two removal paths can never drift apart (Rule 14). It NAMES the evidence
// ("" = nothing here proves a run is alive).
//
// Two independent bits, either one sufficing, both readable ACROSS sessions:
//
//	① the row's recorded runner is not proven dead. The verdict is the shared
//	   classifier's (pid + start ticks + cmd — the cross-session triple of the
//	   2026-09-14 fix: a foreign session's /proc cwd is EACCES under
//	   ptrace_scope=1, so the pid identity is what survives, and "unknown" is
//	   never "dead").
//	② a live process's working directory IS the row's own session container
//	   (`<gopi dir>/sessions/<task id>` — measured: every worker that adopts a
//	   row stands there). The directory NAME is a weak identity on its own, so
//	   it counts only beside the row's own record: an id whose session dir does
//	   not exist yields nothing.
//
// Evidence ② is what closes the concurrent-session window (the 2026-09-17
// incident, row 01a0af50: one row, two sessions — the ledger records ONE runner,
// so the recorded-runner bit goes quiet the moment that runner exits).
//
// FAIL-CLOSED and one-directional: the empty answer means only "nothing here
// proves a session is running", never "the row is dead".
//
// The ASKER's own process tree is not excluded here, and that is not an
// oversight: this is the SWEEP's question about rows it did not run (see
// worktreeSweepSessionLiveEvidenceFor for the arm whose candidates are its own
// rows and where the exclusion is load-bearing).
func worktreeSweepSessionLiveEvidence(status string, ident procIdentity, taskID string) string {
	if !taskLiveHolderStatus(status) {
		return ""
	}
	// ① POSITIVE liveness proof only. An unrecorded / unreadable runner
	// identity is NOT evidence of life here — this predicate's answer becomes a
	// refusal sentence ("this run is still standing"), and a sentence must
	// rest on something the host saw. The conservative "cannot prove it dead"
	// direction is a different question, asked by the ownership rungs'
	// RowHoldsGround over the same evidence (Rule 14: one evidence set, two
	// strengths — the STRONG one for the words, the WEAK one for the veto).
	if finishCleanupProcessHolderLive(ident) {
		return "its recorded runner (pid " + strconv.Itoa(ident.Pid) + ") is still running"
	}
	if dir := worktreeSweepSessionDir(taskID); dir != "" && processInsideSessionDir(dir) {
		return "a live process works in its session directory " + dir
	}
	return ""
}

// worktreeSweepSessionDir returns the row's session container path when the
// tree actually has one ("" otherwise) — the record half of evidence ② above.
func worktreeSweepSessionDir(taskID string) string {
	if taskIDNormalize(taskID) == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(gd, taskSessionsRootName, taskID)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// worktreeSweepLiveSessionRow returns the id (and the named evidence) of a LIVE
// row that still has a run standing AND whose own BRANCH is this registration's —
// the concurrent-session arm, and the WEAKEST of the arms.
//
// THE BRANCH TIE IS THE ARM'S WHOLE CONTENT, and it is the fix for the 2026-10-12
// residue (task 01a130ae). As written, the arm answered "SOMEBODY ELSE is
// running": it returned the FIRST live row with a standing run and kept ANY
// registration on that evidence, in a sentence that claimed the registration was
// that row's own ground ("its own session's ground") while the row's recorded
// ground was a different directory. Four one-shot detached worktrees carrying
// uncommitted mutation material therefore sat in /home/nick/workspace with NO arm
// reaching them (no row names them), while the sweep said an unrelated task's
// live session owned them.
//
// The tie is the SAME one the ownership rungs use — a row's branch against this
// registration's branch — because that is the only fact a stranger's liveness can
// be about THIS registration through: git forbids one branch in two worktrees, so
// a row whose own branch IS this registration's branch and whose deliverable
// points elsewhere is exactly the incident's concurrent-session shape, and any
// OTHER standing row says nothing about this directory.
//
// A branchless (detached) registration has no such name and therefore no
// concurrent-session reading at all — it falls through to the age floor and the
// unowned/terminal reclaim, which is where the residue needed to arrive.
func worktreeSweepLiveSessionRow(rows []tasksRowForSweep, reg worktreeRegistration) (string, string) {
	if reg.Branch == "" {
		return "", "" // a branchless registration has no NAME to tie a run to
	}
	for _, row := range rows {
		if row.Branch != reg.Branch {
			continue
		}
		if evidence := worktreeSweepRowSessionLive(row); evidence != "" {
			return row.TaskID, evidence
		}
	}
	return "", ""
}

// processInsideSessionDir answers whether ANY live process's working directory
// is dir or a directory beneath it — the ONE /proc scan behind the sweep's
// session arm and the reclaim arm's Guard ⓪, so the two removal paths share a
// single liveness probe (a second, drifting probe is how a live worktree gets
// deleted).
//
// It reads ONLY `cwd`: measured on this host, a foreign same-uid process exposes
// `cmdline`, `stat` and a `cwd` readlink while `fd`, `fdinfo`, `maps` and
// `environ` are EACCES under kernel.yama.ptrace_scope=1 — precisely the
// processes this question is about. One bounded scan, one question.
func processInsideSessionDir(dir string) bool {
	dir = filepath.Clean(dir)
	if dir == "" || dir == "." {
		return false
	}
	entries, err := os.ReadDir(scratch.ProcRootDir)
	if err != nil {
		return false // no /proc: no session evidence from this channel
	}
	for _, entry := range entries {
		pid := entry.Name()
		if !entry.IsDir() || !scratch.IsPID(pid) {
			continue
		}
		cwd, err := os.Readlink(filepath.Join(scratch.ProcRootDir, pid, scratch.ProcCwdName))
		if err != nil {
			continue // not observable: cannot veto (the scratch holder policy)
		}
		if cwd != dir && !strings.HasPrefix(cwd, dir+string(os.PathSeparator)) {
			continue
		}
		return true
	}
	return false
}

// worktreeSweepSessionDir is declared beside worktreeSweepSessionLiveEvidence
// (one definition; the arms below reference it).

// worktreeSweepRowHoldsRegistration is the ONE ownership verdict every
// registration rung asks, and it returns the NAMED evidence instead of a bare
// bool so the refusal can quote what it saw (Rule 15: a verdict nobody can
// re-derive is not an explanation). "" = the row does not hold this
// registration.
//
// The arms, in order:
//
//	record-path      the row's deliverable.worktree IS this registration;
//	record-branch    the row's branch is this registration's branch and the row
//	                 names no OTHER directory (the collision guard — a row
//	                 working elsewhere that merely reuses a branch name proves
//	                 nothing about this directory);
//	session-branch   the row's branch is this registration's branch and one of
//	                 the row's sessions is demonstrably RUNNING (evidence ②
//	                 above). This is the concurrent-session arm: the ledger
//	                 records ONE runner per row, so the recorded-runner arm goes
//	                 quiet the moment that runner exits — while a second session
//	                 of the same row may still be building in the very worktree
//	                 its branch names.
//
// The last arm deliberately does NOT accept the path arm's mere path equality:
// a path comparison cannot see a concurrent session either, so the tie there
// stays the ledger's own fields, plus the live-session evidence that the ledger
// alone cannot provide.
func worktreeSweepRowHoldsRegistration(row tasksRowForSweep, reg worktreeRegistration, opts worktreeSweepOptions) string {
	// The ownership test itself stays the shared one (rungs ①D/③ must agree, and
	// this predicate is the ledger half of both), so the SESSION clauses below
	// only ever ADD evidence to a row that already holds ground.
	if !worktreeSweepRowHoldsGround(row, opts) {
		return ""
	}
	if sameWorkingPath(row.Worktree, reg.Path) {
		return "its deliverable names this directory"
	}
	if reg.Branch == "" || row.Branch != reg.Branch || worktreeSweepRowNamesOtherDir(row, reg.Path) {
		return ""
	}
	base := "branch " + reg.Branch + " is its deliverable's branch"
	if live := worktreeSweepRowSessionLive(row); live != "" {
		return base + " and " + live
	}
	return base
}

// worktreeSweepRowLifeEnded reports whether the LEDGER itself declares the
// row's life over. Two shapes carry that declaration, and both are the
// ledger's own statement rather than an inference:
//
//	(a) a TERMINAL status — the transition that closed the row out;
//	(b) a live status beside an ended_at older than worktreeSweepEndedGrace.
//	    A live status on a row whose life was already stamped over is
//	    self-contradictory, exactly the record-not-owner shape this epic is
//	    about — the parent/child enqueue window is the one benign way to
//	    produce it, which is what the grace covers (see
//	    worktreeSweepEndedGrace; a row inside the window is still live).
//
// MEASURED on the live ledger (2026-09-15): 671 rows carry a deliverable, 665
// of them terminal ones carry a populated ended_at — so "the ledger says this
// run is over" is a real, populated signal, not a speculative one.
func worktreeSweepRowLifeEnded(row tasksRowForSweep, opts worktreeSweepOptions) bool {
	if row.terminal() {
		return true
	}
	if row.EndedAt <= 0 {
		return false
	}
	return opts.Now.Sub(time.Unix(int64(row.EndedAt), 0)) >= worktreeSweepEndedGrace
}

// worktreeSweepNamesOtherDir reports whether a row's deliverable records a
// worktree that is NOT this registration — i.e. the row is working somewhere
// else, so its branch name matching this registration is a COLLISION and not
// ownership of this directory. "" (the submit-time {"repo":...} placeholder,
// or a row whose tree was never backfilled) is not another directory: it is
// the absence of evidence, which must not be read as "somewhere else".
func worktreeSweepRowNamesOtherDir(row tasksRowForSweep, regPath string) bool {
	return strings.TrimSpace(row.Worktree) != "" && !sameWorkingPath(row.Worktree, regPath)
}

// worktreeSweepOwnedByAnyRow reports whether ANY row of the ledger speaks for
// this registration — on either ownership axis (a recorded worktree that IS
// this path, or a matching branch when the row records no other directory) and
// in ANY status, terminal included. It is the ESCALATION's predicate, and it is
// deliberately broader than worktreeSweepRowHoldsGround (liveness): the
// question here is not "is anyone still working here" but "does anybody's
// record even remember creating this", which is what separates a delivery in
// progress from residue nobody will ever come back for.
func worktreeSweepOwnedByAnyRow(rows []tasksRowForSweep, reg worktreeRegistration) bool {
	for _, row := range rows {
		if sameWorkingPath(row.Worktree, reg.Path) {
			return true
		}
		if reg.Branch != "" && row.Branch == reg.Branch && !worktreeSweepRowNamesOtherDir(row, reg.Path) {
			return true
		}
	}
	return false
}

// worktreeSweepOrphanBranch renders the naming clause of the escalation
// sentence: the branch when the registration has one, and the abbreviated
// commit its HEAD points at when it does not.
//
// WHY THE COMMIT AND NOT "branch detached" (task 01a130ae, 2026-10-13). The
// clause used to read "(branch detached, age 21h)" for every detached
// registration — a sentence that is TRUE OF EVERY DETACHED REGISTRATION and
// therefore says nothing about this one; the four residue directories each
// produced it. The head commit is the registration's one identifying handle
// (`git worktree list --porcelain` prints it on the HEAD line), and the
// escalation exists precisely so an operator can ACT on an unowned tree — act
// on it with what, if the reason will not say which tree it is? The
// abbreviation goes through the package's own short-hash helper
// (branchDeleteShortHash), so a reader cannot meet two widths for one hash
// (Rule 14).
func worktreeSweepOrphanBranch(reg worktreeRegistration) string {
	if reg.Branch != "" {
		return "branch " + reg.Branch
	}
	if reg.Head != "" {
		return "detached HEAD at commit " + branchDeleteShortHash(reg.Head)
	}
	// Neither a branch nor a readable HEAD: the registration's own shape is all
	// there is to name, and saying so beats printing an empty clause.
	return "detached HEAD at an unreadable commit"
}

// worktreeSweepOrphanReason renders the escalation sentence. It always LEADS
// with worktreeOrphanEscalationMarker (one spelling for the console line, the
// operator channel and the test's assertion — Rule 14), names the branch so an
// operator can act without re-deriving anything, and states the two things the
// reader must know: nothing was removed, and NO --force was used.
func worktreeSweepOrphanReason(reg worktreeRegistration, dirty string, ageHours int) string {
	branch := worktreeSweepOrphanBranch(reg)
	// A CHAT GROUND's escalation names the session, because the ordinary
	// remedy this sentence offers ("the producer pass needs a row keyed on this
	// host's own claim") does not apply to it: a chat writes no row BY DESIGN
	// (chat_ground.go), so an operator reading the console must be told which
	// conversation the dirt belongs to and that its session is over — the two
	// facts that turn "some orphan" into "session X's uncommitted deliverable".
	// The word "uncommitted only" is deliberate: an uncommitted change is the
	// ONLY shape that reaches this sentence from a chat ground (a committed one
	// is kept by the carries-work arm before it).
	chatClause := ""
	if stem := worktreeSweepChatSessionStem(reg); stem != "" {
		chatClause = worktreeSweepChatOrphanClause + stem + ")"
	}
	return fmt.Sprintf(
		"%s: %s (%s, age %dh) is DIRTY and NO ledger row owns it%s — the producer pass needs a row keyed on this host's own claim, the dead-holder arm needs a terminal row, and the age arm refuses dirt, so nothing on this host could ever reach it. NOT removed (uncommitted work is kept, and this sweeper never uses --force); a human must commit it, copy it elsewhere, or remove the worktree by hand. Dirt: %s",
		worktreeOrphanEscalationMarker, reg.Path, branch, ageHours, chatClause, dirty)
}

// worktreeOrphanNotifyState is the once-per-(repo,path)-per-process memo behind
// worktreeSweepNotifyOrphan (the stale detector's own re-arm contract: an
// escalation repeated every tick is noise, not a signal).
var (
	worktreeOrphanNotifyMu sync.Mutex
	worktreeOrphanNotified = map[string]bool{}
)

// worktreeSweepNotifyOrphan escalates one orphan registration on the operator's
// notify channel. The console line already carries the full reason; this is the
// alarm that leaves the log — Rule 15: a condition that no automated arm can
// act on must at least reach a human, once.
func worktreeSweepNotifyOrphan(reg worktreeRegistration, ageHours int) {
	key := reg.Repo + "\x00" + reg.Path
	worktreeOrphanNotifyMu.Lock()
	seen := worktreeOrphanNotified[key]
	worktreeOrphanNotified[key] = true
	worktreeOrphanNotifyMu.Unlock()
	if seen {
		return
	}
	NotifySyntheticEvent(worktreeOrphanEscalationEvent, "", "",
		fmt.Sprintf("%s: %s branch=%s age=%dh dirty, no ledger row — kept (never --force); a human must decide",
			worktreeOrphanEscalationMarker, reg.Path, reg.Branch, ageHours))
}

// worktreeOrphanEscalationEvent is the synthetic notify event one orphan
// escalation fires (the mergedCleanupReapedEvent convention — a named constant
// so an operator subscribes to the same spelling the code emits).
const worktreeOrphanEscalationEvent = "worktree-orphan"

// worktreeSweepOwnerEnded reports whether the ledger's own evidence says the
// registration's owning task life is over, and names the row that carries the
// evidence. It is what lets rung ⑤ stop letting a same-day registration age
// hide a finished run: when the owner is provably ended, the age floor has
// nothing left to protect.
//
// A row can only speak for a registration it is TIED to: its recorded worktree
// must BE this registration, or — when it records no other directory — its
// branch must be this one. A row that names a DIFFERENT directory is working
// elsewhere; with terminal rows now counting as ended, a mere branch-name
// match would let ANY finished task elsewhere license the bypass, which is the
// very collision this epic is about. The tie is therefore the same predicate
// rung ③ uses, so the two seams cannot disagree about who owns what.
func worktreeSweepOwnerEnded(rows []tasksRowForSweep, reg worktreeRegistration, opts worktreeSweepOptions) (bool, string) {
	for _, row := range rows {
		if !worktreeSweepRowLifeEnded(row, opts) {
			continue
		}
		if sameWorkingPath(row.Worktree, reg.Path) ||
			(reg.Branch != "" && row.Branch == reg.Branch && !worktreeSweepRowNamesOtherDir(row, reg.Path)) {
			return true, row.TaskID
		}
	}
	return false, ""
}

// sameWorkingPath compares two filesystem spellings of a worktree. Both sides are
// compared literally AND resolved, because the ledger records what the agent
// typed (`~/workspace/...`) while `git worktree list` reports an absolute,
// resolved path — comparing one spelling would make every ownership match fail
// and every live worktree look unowned.
func sameWorkingPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	for _, x := range worktreeSpellings(a) {
		for _, y := range worktreeSpellings(b) {
			if x == y {
				return true
			}
		}
	}
	return false
}

// worktreeSpellings lists the spellings a worktree path may carry: the tilde
// expanded, cleaned form and its symlink-resolved form.
func worktreeSpellings(path string) []string {
	expanded, err := worktreeroots.ExpandTilde(strings.TrimSpace(path))
	if err != nil || expanded == "" {
		return nil
	}
	clean := filepath.Clean(expanded)
	out := []string{clean}
	if resolved, rerr := filepath.EvalSymlinks(clean); rerr == nil && resolved != "" && resolved != clean {
		out = append(out, resolved)
	}
	return out
}

// worktreeSweepRegistrations lists ONE repo's worktrees (one git process — the
// "one list per repo" rule) in git's order, so the FIRST entry can be marked as
// the main checkout.
func worktreeSweepRegistrations(repo string) ([]worktreeRegistration, error) {
	worktreeSweepObserve(finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	// PARSED porcelain (the ONE parser below reads it): stdout only.
	out, err := finishCleanupGitParsedStdout(ctx, repo, worktreeSweepGitTimeout,
		finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return nil, fmt.Errorf("git %s %s in %s: %w",
			finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, repo, err)
	}
	var regs []worktreeRegistration
	var cur *worktreeRegistration
	flush := func() {
		if cur == nil || cur.Path == "" {
			return
		}
		cur.Repo = repo
		if len(regs) == 0 {
			cur.Main = true
		}
		regs = append(regs, *cur)
		cur = nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, worktreePorcelainWorktreePrefix):
			flush()
			cur = &worktreeRegistration{Path: strings.TrimSpace(strings.TrimPrefix(line, worktreePorcelainWorktreePrefix))}
		case cur == nil:
			// A key line before any "worktree" line is not a shape git emits.
			continue
		case strings.HasPrefix(line, worktreePorcelainHeadPrefix):
			cur.Head = strings.TrimSpace(strings.TrimPrefix(line, worktreePorcelainHeadPrefix))
		case strings.HasPrefix(line, worktreePorcelainBranchPrefix):
			ref := strings.TrimSpace(strings.TrimPrefix(line, worktreePorcelainBranchPrefix))
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case strings.TrimSpace(line) == worktreePorcelainDetached:
			cur.Detached = true
		case strings.HasPrefix(strings.TrimSpace(line), worktreePorcelainLocked):
			cur.Locked = true
		case strings.HasPrefix(strings.TrimSpace(line), worktreePorcelainPrunable):
			cur.Prunable = true
		case strings.TrimSpace(line) == worktreePorcelainBare:
			cur.Bare = true
		case strings.TrimSpace(line) == "":
			flush()
		}
	}
	flush()
	return regs, nil
}

// firstNonEmptyLine renders a command's first non-blank output line for an
// error report.
func firstNonEmptyLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
	}
	return finishCleanupNoOutput
}

// worktreeSweepCreatedAt answers WHEN a registration was created, from the
// registration's OWN admin area — the git-private backlink file `gitdir` and the
// worktree-private git dir that holds it. It is the only age signal a later
// ACCESS cannot forge (see worktreeSweepCommondirName).
//
// WHY NOT `commondir` (corrected 2026-09-16, epic worktree-reap-reachable): for
// a worktree of the MAIN repository — every worktree this host registers —
// `commondir` holds the MAIN repo's git dir, so its mtime is the REPOSITORY's
// own age, not the worktree's. Trusting it made a worktree created minutes ago
// read as days old. That was harmless while the sweep was unreachable and
// became a live hazard the moment it was reachable: the age floor is what keeps
// the resident tick from collecting a registration somebody just made. The
// newest of the registration's own records is used instead — it can never be
// older than the creation, and the failure direction is therefore the SAFE one
// (a bumped record makes the worktree look younger, so the sweep keeps it).
func worktreeSweepCreatedAt(worktree string) (time.Time, error) {
	gitFile := filepath.Join(worktree, worktreeSweepGitdirName)
	raw, err := os.ReadFile(gitFile)
	if err != nil {
		return time.Time{}, fmt.Errorf("read %s: %w", gitFile, err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), finishCleanupGitDirFilePrefix))
	if gitdir == "" {
		return time.Time{}, fmt.Errorf("%s carries no gitdir backlink", gitFile)
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	var newest time.Time
	// FIRST HIT WINS, in this fixed order — deliberately NOT "newest of them
	// all": the git dir itself is bumped by any `git status` (index writes go
	// through a lock-and-rename into it), so taking the newest would make a
	// registration look freshly created every time the sweeper looked at it.
	// `gitdir` and `HEAD` are written once, at registration, and survive every
	// routine verb (measured: `worktree list`, `status`, `add`, `prune`).
	for _, name := range []string{worktreeSweepGitdirFileName, worktreeSweepHeadName, ""} {
		info, serr := os.Stat(filepath.Join(gitdir, name))
		if serr != nil {
			continue
		}
		newest = info.ModTime()
		break
	}
	if newest.IsZero() {
		return time.Time{}, fmt.Errorf("no creation record under %s", gitdir)
	}
	return newest, nil
}

// worktreeSweepDirtyReason answers condition ④ with git's own porcelain status
// (untracked files included — the finish-cleanup convention).
func worktreeSweepDirtyReason(worktree string) string {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	// PARSED porcelain: stdout only. A merged stream would let any stderr line
	// (a `GIT_TRACE=1` trace, a git warning) read as DIRT and report a clean
	// worktree as soiled — the defect this reader was moved off CombinedOutput for.
	out, err := finishCleanupGitParsedStdout(ctx, worktree, worktreeSweepGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		// Unrunnable status is NOT "clean": an unverifiable worktree keeps its
		// files (returning a reason makes the caller REPORT it) — the same
		// fail-closed direction as the pattern guard's cwd rule.
		return "git status failed: " + err.Error()
	}
	text := out
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > worktreeSweepDirtyPreviewLines {
		lines = append(lines[:worktreeSweepDirtyPreviewLines], "...")
	}
	return strings.Join(lines, "; ")
}

// worktreeSweepDirtyReportReason renders the one sentence every dirty
// registration gets, with the CHAT GROUND's own clause appended when the
// registration is a chat's (epic chat-ground-reclaim).
//
// WHY IT IS A FUNCTION AND NOT AN INLINE APPEND: TWO rungs report dirt — ③O's
// fall-through (dirty, unowned, but not yet past the age floor) and ④ (the
// plain report) — and the first of them RETURNS before the second is reached.
// Measured against the real verb: a dirty chat ground whose registration was
// minutes old took ③O's fall-through and lost the clause, which is precisely
// the operator the clause exists for (a chat's dirt has no ledger row to look
// up). One renderer, both rungs, so the two cannot drift.
func worktreeSweepDirtyReportReason(reg worktreeRegistration, dirty string) string {
	reason := "dirty (uncommitted work) — left in place: " + dirty
	stem := worktreeSweepChatSessionStem(reg)
	if stem == "" {
		return reason
	}
	// The chat's clause states the two facts a ledger lookup cannot supply:
	// WHICH session the dirt belongs to, and that NO row will ever own it (a
	// chat writes none by design — chat_ground.go). It is the difference
	// between an operator hunting for a row that does not exist and one who
	// knows to commit the file or copy it out.
	return reason + worktreeSweepChatDirtyReasonSuffix + stem +
		") — this is the session's deliverable, and no ledger row will ever own it"
}

// worktreeSweepDirtyPreviewLines bounds how much porcelain dirt one finding
// quotes (a 5 000-file worktree must not produce a 5 000-line report).
const worktreeSweepDirtyPreviewLines = 3

// worktreeSweepTaskRows reads the ledger evidence: every task row whose
// deliverable names a worktree, whatever its status (the caller discriminates —
// terminal rows are evidence FOR reclaiming, non-terminal rows are evidence
// AGAINST).
func worktreeSweepTaskRows(ctx context.Context) ([]tasksRowForSweep, error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// NO SQL LIMIT — and that is the fix, not a style choice (incident
	// 2026-09-14 08:25, task 01a09d67): this loader used to read the oldest
	// reclaimScanMax (200) worktree rows. On the incident host 286 rows named a
	// worktree, so the NEWEST 86 — precisely the live ones, updated minutes
	// earlier — were never loaded, and the live owner (row 01a09d07, position
	// 275 by updated_at ASC) was invisible to the ownership veto in
	// worktreeSweepJudge rung ③: the sweep saw an unowned-looking, clean, aged
	// registration and reclaimed the directory out from under a running
	// session. Recency and liveness correlate, so an age-ASC LIMIT drops
	// exactly the rows that protect live ground — the one truncation this
	// evidence set must never have. The scan is exhaustive now; the bound below
	// is a runaway bound only, and hitting it REFUSES the pass (Rule 15: no
	// silent partial universe).
	// The evidence set is "every row that COULD own a registration", so it
	// must carry BOTH ownership axes the rungs below judge on: a recorded
	// worktree (the path arm) and a recorded branch (the branch arm).
	// MEASURED (2026-09-15, probe repo + probe ledger): with the worktree
	// needle alone, a live row that named ONLY a branch never reached rung ③
	// — the arm cannot protect what the loader never loaded — while the branch
	// arm's own tests, which construct rows directly, kept passing. Rather
	// than add a second needle that must be kept in step with the blob's
	// spelling, the scan takes every row carrying a deliverable at all and
	// lets the parsers below decide: no needle can then truncate an axis out
	// of the evidence (Rule 15 — the miss was silent).
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(deliverable,''), COALESCE(ended_at,0),
		        COALESCE(claimed_by,''), COALESCE(`+tasksProcPidColumnName+`,0),
		        COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks
		 WHERE COALESCE(deliverable,'') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tasksRowForSweep
	for rows.Next() {
		var id, status, deliverable, claimedBy, procCmd string
		var endedAt float64
		var procPid, procStart int64
		if err := rows.Scan(&id, &status, &deliverable, &endedAt, &claimedBy, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		parsed := finishCleanupParseDeliverable(deliverable)
		ident := procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}
		out = append(out, tasksRowForSweep{
			TaskID:      id,
			Status:      status,
			Worktree:    taskDeliverableWorktree(deliverable),
			Branch:      taskDeliverableBranchOrEmpty(deliverable),
			Repo:        parsed.Repo,
			Deliverable: deliverable,
			EndedAt:     endedAt,
			ClaimedBy:   claimedBy,
			Ident:       ident,
			// The holder verdict is NOT re-derived here: it is the shared
			// classifier the finish-cleanup arms consult (Rule 14 — one
			// definition of "this row's runner is gone"), fed the row's own
			// recorded process identity.
			HolderProvenDead: finishCleanupDeadLocalHolder(status, claimedBy, ident),
		})
	}
	return out, rows.Err()
}

// worktreeSweepRepos returns the repos to sweep: the settings `repos` table's
// keys (Rule 12's sweep list) plus the repo the process stands in, deduped and
// capped. A settings file that cannot be read is REPORTED and the run degrades
// to the current repo — never to no repos at all (an unreadable settings file
// must not silently disable the sweep).
func worktreeSweepRepos() ([]string, []string) {
	var repos []string
	var errs []string
	// Dedupe on the git COMMON dir, not on the path: a linked worktree and its
	// main checkout are the SAME repository (the git dir is shared), so listing
	// both would enumerate — and judge — one repo's registrations twice, and a
	// double judgement is a double removal attempt. The common dir is the
	// identity of a repository.
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		key := clean
		if common, cerr := worktreeSweepCommonGitDir(clean); cerr == nil && common != "" {
			key = common
		}
		if seen[key] || len(repos) >= reclaimScanMax {
			return
		}
		seen[key] = true
		repos = append(repos, clean)
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil {
		errs = append(errs, fmt.Sprintf("settings: %v (sweeping only the current repo)", err))
	} else {
		for repo := range cfg.Repos {
			add(repo)
		}
	}
	if cwd, cerr := os.Getwd(); cerr == nil {
		if root, rerr := repoRootOf(cwd); rerr == nil {
			add(root)
		}
	}
	return repos, errs
}

// worktreeSweepCommonGitDir resolves a repo's SHARED git dir — the identity of
// the repository across its main checkout and every linked worktree
// (`git rev-parse --git-common-dir`; for a linked worktree it is the main
// repo's .git, so two spellings of one repository compare equal).
func worktreeSweepCommonGitDir(repo string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	// PARSED path: stdout only (the stderr half lives in the error).
	out, err := finishCleanupGitParsedStdout(ctx, repo, worktreeSweepGitTimeout,
		gitRevParseVerb, gitParsedCommonDirFlag)
	if err != nil {
		return "", fmt.Errorf("git %s %s in %s: %w", gitRevParseVerb, gitParsedCommonDirFlag, repo, err)
	}
	dir := out
	if dir == "" {
		return "", fmt.Errorf("empty --git-common-dir for %s", repo)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, dir)
	}
	if resolved, rerr := filepath.EvalSymlinks(dir); rerr == nil {
		dir = resolved
	}
	return filepath.Clean(dir), nil
}

// RepoRootAt returns the top level of the repo a directory is inside — the
// exported face of repoRootOf, for callers outside this package (the
// `gopi task archive` verb resolves WHICH repo it is writing a tag into; writing
// the decision into the wrong repo would be the worst outcome of a gate whose
// whole purpose is legibility).
func RepoRootAt(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	return repoRootOf(dir)
}

// repoRootOf returns the top level of the repo a directory is inside.
func repoRootOf(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	// PARSED path: stdout only (see worktreeSweepCommonGitDir).
	out, err := finishCleanupGitParsedStdout(ctx, dir, worktreeSweepGitTimeout,
		gitRevParseVerb, gitParsedShowTopLevelFlag)
	if err != nil {
		return "", fmt.Errorf("git %s %s in %s: %w", gitRevParseVerb, gitParsedShowTopLevelFlag, dir, err)
	}
	return out, nil
}

// worktreeSweepRoots returns the declared roots consulted for one repo: its
// declared worktree root, the scratch home, the MEASURED memory base that home
// resolves onto, the temp fixture root, and the repo's own tree. A registration
// inside the repo tree itself is a nested worktree, which is legitimate and
// must not be reported as out-of-policy.
func worktreeSweepRoots(repo string) []worktreeroots.Root {
	var roots []worktreeroots.Root
	if root, err := config.ResolveRepoWorktreeRoot(repo); err == nil {
		roots = append(roots, root)
	} else {
		fmt.Fprintf(os.Stderr, "sweep-worktrees: %v — using the default worktree root\n", err)
		if fallback, ferr := worktreeroots.Resolve(""); ferr == nil {
			roots = append(roots, fallback)
		}
	}
	roots = append(roots, worktreeroots.Root{
		Path:   scratch.CanonicalPath(scratch.HomeDir()),
		Role:   worktreeroots.RoleScratch,
		Source: worktreeroots.SourceScratchHome,
	})
	// The MEMORY BASE the scratch home itself resolves onto. The scratch home
	// is one DIRECTORY inside it, so registrations created directly in the base
	// (the measured gopi-verify-* family, which is what filled /dev/shm on
	// 2026-09-15) were inside no declared root and could never be reclaimed.
	// The root is DERIVED by measurement, never spelled: on a host whose
	// scratch home sits on a block device this adds nothing at all. See
	// worktree_sweep_memory_base.go.
	if base, ok := memoryBaseRoot(); ok {
		roots = append(roots, memoryBaseDeclaredRoot(base))
	}
	if temp := scratch.CanonicalPath(os.TempDir()); temp != "" {
		roots = append(roots, worktreeroots.Root{Path: temp, Role: worktreeroots.RoleFixture, Source: worktreeroots.SourceTempRoot})
	}
	if repo != "" {
		// The repo's own tree (its main checkout) — a worktree nested INSIDE it
		// is a legitimate shape and must not be reported as out-of-policy.
		roots = append(roots, worktreeroots.Root{Path: filepath.Clean(repo), Role: worktreeroots.RoleSession, Source: worktreeroots.SourceSessionCwd})
	}
	return roots
}

// worktreeSweepDeps is the host evidence the sweep runs on. Production builds
// it from settings + the ledger + /proc; the tests inject it, which is what
// makes the ONE-LIST-ONE-PRUNE-PER-REPO rule assertable without touching a real
// repo.
type worktreeSweepDeps struct {
	Repos   []string
	Rows    []tasksRowForSweep
	Holders scratch.HolderScan
	// Roots, when non-nil, IS the declared-root list every registration is
	// judged against, for every repo in Repos. nil = resolve from the host per
	// repo (worktreeSweepRoots), which is the PRODUCTION path: the roots are
	// host evidence exactly like the ledger rows and the holder list injected
	// beside it, so a fixture may declare its own throwaway area instead of
	// hoping the ambient list reaches it.
	//
	// MEASURED NEED (2026-09-28, master's own run of
	// TestWorktreeSweepRefusesEvictionOfAPeerCheckout). The fixture's repo lives
	// under t.TempDir(), but the child helper PROCESS runs this package's
	// TestMain, which pins TMPDIR to its OWN per-run suite root — a SIBLING of
	// the parent's. So in the child the fixture's registration lay outside every
	// host-resolved root (declared worktree root, scratch home, the CHILD's temp
	// root, the repo tree) and rung ① reported `declared-root-scope — not swept`
	// for a registration the test required to be reclaimed. The failure was
	// structural, not a policy defect: the fixture asked a policy question about
	// a path the policy could not see.
	Roots []worktreeroots.Root
	// LedgerRead records that the ledger WAS consulted (a successful query, even
	// one returning no row). A reclaim requires it: "no owning row" is only
	// evidence when the ledger was actually read (the fail-closed rule).
	LedgerRead bool
	// RowsErr records that the ledger could not be read ("" = no error).
	RowsErr string
}

// worktreeSweepCollect gathers the host evidence.
func worktreeSweepCollect(ctx context.Context) (worktreeSweepDeps, []string) {
	deps := worktreeSweepDeps{}
	var errs []string
	deps.Repos, errs = worktreeSweepRepos()
	rows, err := worktreeSweepTaskRows(ctx)
	if err != nil {
		// The ledger is the ownership evidence: without it NOTHING may be
		// reclaimed (an unowned-looking registration may be a live task's).
		errs = append(errs, fmt.Sprintf("task ledger: %v (no registration will be reclaimed)", err))
	} else {
		deps.Rows = rows
		deps.LedgerRead = true
	}
	deps.Holders = scratch.ScanHolders()
	return deps, errs
}

// worktreeSweepRun executes the sweep: per repo ONE registration listing, per
// registration ONE judgement, then ONE prune per repo that actually removed
// something. DryRun is honoured by never invoking a removal.
func worktreeSweepRun(ctx context.Context, opts worktreeSweepOptions) worktreeSweepResult {
	deps, errs := worktreeSweepCollect(ctx)
	return worktreeSweepRunWith(ctx, opts, deps, errs)
}

// worktreeSweepRunWith is the sweep over injected evidence (see
// worktreeSweepDeps).
func worktreeSweepRunWith(ctx context.Context, opts worktreeSweepOptions, deps worktreeSweepDeps, errs []string) worktreeSweepResult {
	res := worktreeSweepResult{DryRun: opts.DryRun, Findings: []worktreeSweepFinding{}}
	res.Repos = deps.Repos
	res.Errors = append(res.Errors, errs...)

	rows := deps.Rows
	holders := deps.Holders

	for _, repo := range deps.Repos {
		regs, lerr := worktreeSweepRegistrations(repo)
		if lerr != nil {
			res.Errors = append(res.Errors, lerr.Error())
			continue
		}
		roots := deps.Roots
		if roots == nil {
			roots = worktreeSweepRoots(repo)
		}
		// guestSelf: is this process a GUEST in this repo (standing in a linked
		// worktree of it, not in its main checkout)? The guard's input, resolved
		// once per repo here — never re-derived per registration (Rule 2).
		guestSelf := worktreeSweepSelfGuestCheckout(repo)
		// actedInRepo: did THIS run take a registration out of this repo (by
		// reclaiming a checkout or by reaping dead bookkeeping)? The closing
		// prune is gated on it — a repo this run did not touch is left exactly
		// as it was, so the sweep never mutates a repo it found nothing to do
		// in (the per-repo, never per-row, contract the verb's test asserts).
		actedInRepo := false
		// Branch deletions are DEFERRED until after the repo's prune: git
		// refuses `branch -d` for a branch its bookkeeping still associates with
		// a worktree, and that association is only dropped by the prune (or by
		// `worktree remove -f -f`). Measured: deleting the ref straight after
		// clearing the lock fails with "Cannot delete branch '…' checked out
		// at '…'" — the exact registration we had just repaired.
		var pendingBranchReaps []worktreePendingBranchReap
		for _, reg := range regs {
			ho := worktreeSweepHostObservations{SelfRepoGuestCheckout: guestSelf}
			if !reg.Main {
				if created, aerr := worktreeSweepCreatedAt(reg.Path); aerr == nil {
					ho.CreatedAt = created
				}
				// The DIR-GONE measurement is separate from the dirtiness
				// probe: `git status` cannot run in a missing directory, so a
				// missing tree would otherwise masquerade as "could not read"
				// and a dead registration would never be recognised as dead.
				// Measured separately, its meaning is unambiguous (Rule 15: no
				// silent guessing).
				if _, serr := os.Stat(reg.Path); os.IsNotExist(serr) {
					ho.DirGone = true
				} else {
					ho.DirtyReason = worktreeSweepDirtyReason(reg.Path)
				}
			}
			f := worktreeSweepJudge(reg, rows, holders, roots, opts, ho)
			if f.Disposition == string(worktreeReclaim) && !deps.LedgerRead {
				// Fail closed without ledger evidence (see above).
				f.Disposition = string(worktreeKeep)
				f.Reason = "task ledger unavailable — ownership unverifiable, keeping"
				f.Rule = string(worktreeRuleNoLedger)
			}
			switch f.Disposition {
			case string(worktreeReclaim):
				// THE CHAT GROUND's own two additions to an ORDINARY reclaim
				// (epic chat-ground-reclaim). Both are asked of the ground's OWN
				// evidence; neither touches what the ordinary reclaim already
				// does with the registration.
				chatGround := f.Rule == string(worktreeRuleChatInert)
				// THE THIRD CONDITION OF INERTNESS: 0 AHEAD of the integration
				// branch. A chat ground's commits live on its own ref and
				// nowhere else (a chat is not a row — no merge path will ever
				// look at `chat/<id>`), so a ground holding commits the
				// integration branch does not have is a DELIVERABLE, not
				// residue: freeing the directory would leave those commits
				// reachable only by the ref this same arm is about to offer to
				// `branch -d`. The measurement is git's own (`merge-base
				// --is-ancestor`, the merger's own spent-branch test), asked
				// BEFORE anything is removed.
				if chatGround && !worktreeSweepBranchIsSpent(repo, reg.Branch) {
					// The judge's reason ALREADY opens with "chat ground <stem> is
					// inert — <evidence>", so the runner keeps it verbatim and
					// appends only the fact the judge could not know (the ref's
					// lineage). Re-prefixing produced a doubly-nested sentence on
					// the real verb ("chat ground X is inert (chat ground X is
					// inert — ...)") — measured, and fixed here.
					f.Disposition = string(worktreeKeep)
					f.Reason = f.Reason + worktreeSweepChatCarriesWorkReason
					f.Rule = string(worktreeRuleChatCarriesWork)
					break
				}
				// THE BRANCH AXIS for a registration that is NOT dead, which
				// until this arm did not exist: the ONLY branch rung was the
				// DEAD registration's (a directory already gone), so an inert
				// chat ground's `chat/<id>` branch survived every arm on the
				// host forever — 0 ahead of the integration branch, blocking no
				// merge, owned by nobody (measured 2026-09-28, diagnosis §3).
				// The ref is deleted AFTER the registration is gone (git refuses
				// `branch -d` while its bookkeeping still associates the branch
				// with a worktree — see pendingBranchReaps), through `-d` and
				// never `-D`: an unmerged or elsewhere-checked-out ref is
				// refused BY GIT, and its refusal is reported on the finding.
				chatBranch := chatGround && !opts.DryRun && reg.Branch != ""
				if res.Reclaimed >= opts.Max {
					f.Disposition = string(worktreeKeep)
					f.Reason = fmt.Sprintf("run cap reached (%d) — will be considered next run", opts.Max)
					f.Rule = string(worktreeRuleRunCap)
				} else if !opts.DryRun {
					// THE DETERMINISTIC CHECKLIST (reclaim_ground_checklist.go),
					// asked on the RECLAIM branch so the report stays truthful:
					// `worktreeSweepJudge` answers the REGISTRATION's and the
					// ROW's questions and never the ground's or the branch's.
					// A refusal is rendered exactly as the judge's own keep is —
					// same disposition, same channel — so the sweep can never
					// claim it acted on a directory it did not touch. The
					// finding is stamped and the switch LEFT; the one append
					// below this switch is the only place a finding joins the
					// report (appending here too printed the refusal twice on
					// the real verb — measured, Rule 31).
					if why := reclaimGroundChecklistVerdict(repo, reg.Path, reg.Branch); why != "" {
						f.Disposition = string(worktreeKeep)
						f.Reason = why
						f.Rule = string(worktreeRuleGroundChecklist)
						break
					}
					// The audit record goes BEFORE the removal (package
					// removalaudit): the deciding evidence is this finding's own
					// reason, which names the policy arm that judged it.
					removalaudit.WorktreeRemove(removalaudit.ArmSweepReclaim, repo, reg.Path,
						reg.Branch, "", f.Reason+"; checklist "+reclaimGoalChecklistSummary)
					worktreeSweepObserve(finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, reg.Path)
					if rerr := reclaimGitRemove(repo, reg.Path); rerr != nil {
						res.Errors = append(res.Errors, rerr.Error())
						// Rule 23 / Rule 15b: a FAILED reclaim is recorded, never
						// swallowed and never silently upgraded to a removal by
						// other means. The verb deliberately does NOT reach for
						// `remove --force` or for a direct directory delete here:
						// the branch that produced this refusal was a refusal of
						// git's own safety check, and a sweeper that bulldozes
						// through it cannot be told apart from one that ate
						// somebody's work. Nowhere else in this file does a
						// failure path become a removal.
						f.Disposition = string(worktreeKeep)
						f.Reason = "removal refused by git (kept, never forced): " + rerr.Error()
					} else {
						res.Reclaimed++
						actedInRepo = true
						if chatBranch {
							pendingBranchReaps = append(pendingBranchReaps, worktreePendingBranchReap{
								worktree:   reg.Path,
								branch:     reg.Branch,
								findingAt:  len(res.Findings),
								chatGround: true,
							})
						}
					}
				} else {
					res.WouldReclai++
				}
			case string(worktreeEscalateOrphan):
				// Escalation is a REPORT, not an action: nothing is removed,
				// no git verb runs, and the operator channel carries the alarm
				// (Rule 15 — the one shape no sweep can act on must still reach
				// a human). Counted once per registration, throttled per
				// (repo, path) per process by the notifier.
				res.Escalated++
				worktreeSweepNotifyOrphan(reg, f.AgeHours)
			case string(worktreeReap), string(worktreeReapBranch):
				if !opts.DryRun {
					if rerr := worktreeSweepReapRegistration(repo, reg.Path); rerr != nil {
						res.Errors = append(res.Errors, rerr.Error())
						f.Disposition = string(worktreeKeep)
						f.Reason = "the dead registration could not be dropped: " + rerr.Error()
						break
					}
					res.Reaped++
					actedInRepo = true
					if f.Disposition == string(worktreeReapBranch) && reg.Branch != "" {
						pendingBranchReaps = append(pendingBranchReaps, worktreePendingBranchReap{
							worktree:  reg.Path,
							branch:    reg.Branch,
							findingAt: len(res.Findings),
						})
					}
				} else {
					res.WouldReap++
				}
			}
			res.Findings = append(res.Findings, f)
		}
		// ONE prune per repo THIS RUN ACTED IN — never per row, and never in a
		// repo it found nothing to do in. The prune is what drops git's last
		// association between a ref and a vanished worktree (which is why the
		// deferred branch reap waits for it), and it is repo-global because git
		// offers no scoped form (measured). Its removals are relayed into the
		// report (see worktreeSweepPrune). A DRY RUN never prunes: the default
		// posture is side-effect free.
		if !opts.DryRun && actedInRepo {
			worktreeSweepPrune(repo, &res)
		}
		// Only NOW are the dead registrations' branches free: the prune above is
		// what drops git's association between the ref and the vanished
		// worktree. Each deletion still goes through `branch -d`, so an unmerged
		// ref survives and the finding says why.
		for _, pending := range pendingBranchReaps {
			if pending.findingAt >= len(res.Findings) {
				continue
			}
			// The ARM names itself in the audit record: a dead registration's
			// branch and an inert chat ground's branch are two different
			// decisions, and the record is what a later reader greps.
			var berr error
			if pending.chatGround {
				berr = worktreeSweepReapChatBranch(repo, pending.worktree, pending.branch)
			} else {
				berr = worktreeSweepReapBranch(repo, pending.worktree, pending.branch)
			}
			if berr != nil {
				res.Findings[pending.findingAt].Reason += " — branch " + pending.branch + " kept: " + berr.Error()
				continue
			}
			res.BranchesReaped++
			res.Findings[pending.findingAt].Reason += " — branch " + pending.branch + " deleted (git agreed it was safe)"
		}
	}
	return res
}

// worktreePendingBranchReap is one deferred ref removal: a dead registration's
// own branch, waiting for the repo's prune to drop git's association with the
// vanished worktree (otherwise `branch -d` refuses the deletion it would
// otherwise allow). findingAt is the index of the finding to amend with the
// outcome — the operator's report and the code's belief must not diverge.
type worktreePendingBranchReap struct {
	worktree  string
	branch    string
	findingAt int
	// chatGround selects the arm that records and performs the deletion: an
	// inert chat ground's branch is a different decision from a dead
	// registration's, and the audit record must say which one it was (see
	// worktreeSweepReapChatBranch).
	chatGround bool
}

// worktreeSweepPrune drops a repo's dead registrations once, recording a
// failure instead of swallowing it (Rule 15b).
//
// It is repo-GLOBAL by necessity, not by choice: `git worktree prune` accepts
// no path argument at all (measured, git 2.34.1 — `git worktree prune -v <path>`
// answers with the usage block and exit 129), so a prune cannot be scoped to
// the registrations this run judged. What makes that safe is git's own
// condition, also measured: a registration whose worktree directory is PRESENT
// is never pruned (git re-resolves the `gitdir` backlink first), and a LOCKED
// one is skipped — so the prune collects exactly the dir-gone, unlocked
// registrations that are the leak this sweeper exists for.
//
// The one honest cost is that a bare prune may ALSO collect a dead registration
// this run never judged. That is why the sweep runs it with `-v` and relays the
// lines: "Removing worktrees/<id>: gitdir file points to non-existent location"
// names every registration the prune touched, so the operator's report is a
// complete account of what changed (Rule 15: no unreported side effects) —
// instead of the silent repo-wide mutation a bare prune used to be.
func worktreeSweepPrune(repo string, res *worktreeSweepResult) {
	worktreeSweepObserve(finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb,
		finishCleanupWorktreePruneFlag)
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb,
		finishCleanupWorktreePruneFlag)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("git worktree prune in %s: %v: %s", repo, err, firstNonEmptyLine(out)))
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			res.Pruned = append(res.Pruned, l)
		}
	}
}

// worktreeDropStaleLock is the SHARED repair for the lock git leaves behind on a
// registration it was killed in the middle of creating (measured: a SIGKILLed
// `git worktree add` leaves `.git/worktrees/<id>/locked` = "initializing", and
// `git worktree prune` then SKIPS that entry forever while
// `git worktree remove --force` refuses it).
//
// It returns nil when the registration carries no lock (nothing to repair).
// Two documented steps, in git's own order:
//
//	① `unlock <path>` — git's named repair; it needs the directory to still
//	   exist, so a failure here is expected once the checkout is gone and is
//	   returned to the caller to REPORT (Rule 15b), never discarded;
//	② clearing the lock file inside the registration's admin dir — the same
//	   operation `git worktree unlock` performs, available when ① ran out of
//	   directory to work with.
//
// Callers: the sweeper (a dead registration it found) and merge_build_gate.go
// (the checkout that produced the shape — the gate must not leave behind the
// thing it created).
func worktreeDropStaleLock(repo, worktree string) error {
	unlockErr := worktreeSweepUnlockOptional(repo, worktree)
	if !worktreeSweepRegistrationLocked(repo, worktree) {
		return nil
	}
	if lerr := worktreeSweepClearLockFile(repo, worktree); lerr != nil {
		// BOTH causes are wrapped. This site used to render unlockErr with %v
		// under a `// no-wrap:` reason ("the unlock error is deliberately
		// OUTSIDE the chain"), and stylegate's errorf-verb honours no inline
		// escape, so the escape bought nothing there and the ceiling carried
		// the exception instead. The reason was also not true of the code: the
		// unlock failure is the FIRST half of a two-step repair, and a caller
		// asking `errors.Is(err, <the unlock cause>)` was told "no" for a cause
		// that was right there in the message. unlockErr is assigned only from
		// a call (never a nil-able variable), so %w cannot render
		// `%!w(<nil>)` here; Go 1.20+ wraps both verbs, so errors.Is/As reach
		// either cause. The measurement that used to justify the escape
		// (stylegate errorf-verb == ceiling 1) is now 0.
		return fmt.Errorf("unlock did not hold (%w) and the lock file could not be cleared: %w", unlockErr, lerr)
	}
	return nil
}

// worktreeSweepLockAge reports how long a registration's lock file has existed
// — the measurement that separates an ABANDONED "initializing" lock (a SIGKILLed
// `worktree add`, which is the leak) from a LIVE one (a creator mid-checkout,
// which must never be touched). It reports an error when there is no lock to
// date or the state cannot be read; the caller then KEEPS the registration.
func worktreeSweepLockAge(repo, worktree string) (time.Duration, error) {
	dir, err := worktreeSweepAdminDir(repo, worktree)
	if err != nil {
		return 0, err
	}
	fi, serr := os.Stat(filepath.Join(dir, worktreeSweepLockFileName))
	if serr != nil {
		return 0, serr
	}
	return time.Since(fi.ModTime()), nil
}

// worktreeSweepReapRegistration drops the git BOOKKEEPING of a registration
// whose worktree directory is already gone. It is the repair the leak needed:
//
//	prune          SKIPS a locked registration (measured — the 2026-09-13
//	               entries survived it untouched),
//	remove         REFUSES a locked registration and points at unlock,
//	unlock <path>  FAILS once the directory is gone ("not a working tree") —
//	               only when the directory still exists does it work,
//	remove -f -f   IS the documented override ("use 'remove -f -f' to override
//	               or unlock first" — git's own message) and works in both
//	               states,
//	lock-clear+prune  also works, by hand-editing git's admin area.
//
// This function uses the unlock-then-clear path (worktreeDropStaleLock) and
// never the force-removal one: it removes no directory (there is none) and no
// ref (that is the caller's separate, `-d`-guarded decision).
func worktreeSweepReapRegistration(repo, worktree string) error {
	return worktreeDropStaleLock(repo, worktree)
}

// worktreeSweepUnlockOptional runs `git worktree unlock <path>` and returns its
// error to the caller's REPORT (never a bare discard — Rule 15b): the expected
// failure when the directory is gone is evidence, and the reap's own
// post-condition check below decides whether anything is still to do.
func worktreeSweepUnlockOptional(repo, worktree string) error {
	worktreeSweepObserve(finishCleanupWorktreeVerb, worktreeSweepUnlockVerb, worktree)
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", finishCleanupWorktreeVerb, worktreeSweepUnlockVerb, worktree)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("worktree unlock %s: %w: %s", worktree, err, firstNonEmptyLine(out))
	}
	return nil
}

// worktreeSweepRegistrationLocked reports whether the registration still
// carries a lock, measured on git's own admin area for the path — never on the
// unlock command's exit status (a command that failed for an unrelated reason
// must not be read as "still locked"). An unmeasurable state counts as LOCKED:
// the caller then clears the lock file, which is safe in both cases.
func worktreeSweepRegistrationLocked(repo, worktree string) bool {
	dir, err := worktreeSweepAdminDir(repo, worktree)
	if err != nil {
		return true
	}
	_, serr := os.Stat(filepath.Join(dir, worktreeSweepLockFileName))
	return serr == nil || !os.IsNotExist(serr)
}

// worktreeSweepClearLockFile removes the lock file inside the registration's
// admin dir — git's own "unlock", spelled as a file removal (the same operation
// `git worktree unlock` performs).
func worktreeSweepClearLockFile(repo, worktree string) error {
	dir, err := worktreeSweepAdminDir(repo, worktree)
	if err != nil {
		return err
	}
	lock := filepath.Join(dir, worktreeSweepLockFileName)
	if rerr := os.Remove(lock); rerr != nil && !os.IsNotExist(rerr) {
		return fmt.Errorf("remove lock file %s: %w", lock, rerr)
	}
	return nil
}

// worktreeSweepAdminDir resolves the <repo>/.git/worktrees/<id> admin directory
// of a registration. The id is git's own: the basename of the admin path that
// the worktree's `.git` backlink pointed at — read from the registration's own
// `gitdir` file when the worktree directory is gone, because then there is no
// backlink left to read.
//
// The resolution is deliberately conservative: a gitdir pointing anywhere
// outside the repo's own admin area is REFUSED (the caller reports it), so a
// hand-crafted registration can never aim this repair at a foreign path
// (Rule 15: an unanswerable question is a keep, not a guess).
func worktreeSweepAdminDir(repo, worktree string) (string, error) {
	adminRoot := filepath.Join(repo, worktreeSweepAdminPathName)
	entries, err := os.ReadDir(adminRoot)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", adminRoot, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(adminRoot, entry.Name())
		if worktreeSweepAdminDirMatches(dir, worktree) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no registration under %s names %s", adminRoot, worktree)
}

// worktreeSweepAdminDirMatches reports whether one .git/worktrees/<id> entry
// belongs to the given worktree path: its `gitdir` file records the path of the
// worktree's own `.git` file, which sits INSIDE the worktree — so a containment
// test on the parent is what identifies the entry, for both a live directory and
// a vanished one.
func worktreeSweepAdminDirMatches(dir, worktree string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, worktreeSweepGitdirFileName))
	if err != nil {
		return false
	}
	gitdir := strings.TrimSpace(string(raw))
	if gitdir == "" || !filepath.IsAbs(gitdir) {
		return false
	}
	return sameWorkingPath(filepath.Dir(gitdir), worktree)
}

// worktreeSweepReapBranch deletes the branch of a DEAD registration, through
// `git branch -d` — git's own check that the branch is merged and checked out
// nowhere. The ref is never force-deleted (Rule 15: an unmerged branch is
// someone's work, and a deleted ref is the one thing a sweeper cannot restore);
// a refusal is returned as text so the caller reports it on the finding.
func worktreeSweepReapBranch(repo, worktree, branch string) error {
	return worktreeSweepDeleteBranch(repo, worktree, branch, removalaudit.ArmSweepDeadRegistrationBranch,
		"the branch's registration "+worktree+" was dead and its git bookkeeping was reaped")
}

// worktreeSweepReapChatBranch is the SAME removal for the other rung that has a
// branch to recycle: an INERT CHAT GROUND whose registration was just reclaimed.
// It exists as its own entry point for ONE reason — the audit record must name
// the arm that decided, and `removalaudit`'s own design says so ("a record that
// cannot distinguish the sweeper from the finish-cleanup chain is the
// incident's own blind spot"): a reader grepping `arm=` must be able to tell
// "the registration's directory was already gone" from "the session was over".
// The deletion itself is the shared body, so `-d`, never `-D`, and the main
// checkout as the working directory hold for both.
func worktreeSweepReapChatBranch(repo, worktree, branch string) error {
	return worktreeSweepDeleteBranch(repo, worktree, branch, removalaudit.ArmSweepChatGroundBranch,
		"the chat ground's registration "+worktree+" was reclaimed as inert (no ledger row, clean, 0 ahead, session over)")
}

// worktreeSweepDeleteBranch is the ONE `git branch -d` this package performs
// (Rules 13/14: one body, two arms naming themselves in the record).
func worktreeSweepDeleteBranch(repo, worktree, branch string, arm removalaudit.Arm, evidence string) error {
	if branch == "" {
		return nil
	}
	// Audit record FIRST, before any of this function's bookkeeping and before
	// the ref deletion itself: the deciding evidence is the arm's own verdict.
	//
	// THE TIP GOES IN TOO (measured 2026-10-29). This site wrote the record with
	// NO sha, and it is one of the two arms still doing so: on this host's sink
	// `merged-cleanup-*` and `task-merge-branch` records carry a sha in ZERO of
	// their cases, against 247 of 2601 for `finish-cleanup-branch` (which passes
	// BranchDeleteAt). A ref that is deleted without a recorded sha leaves its
	// branch's tip knowable only from a directory somebody may already have
	// removed — the same one-witness problem the ground-deletion half of this
	// change repairs. The tip is captured HERE because the delete below destroys
	// the only other place it could be read.
	removalaudit.BranchDeleteAt(arm, repo, branch, "", branchDeleteTipOf(context.Background(), repo, branch), "", evidence)
	worktreeSweepObserve(finishCleanupBranchVerb, finishCleanupBranchDeleteFlag, branch)
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	// The deletion runs in the MAIN checkout — the branch's own registration was
	// just dropped, so the ref is free; `-d` still refuses a branch that carried
	// commits master does not have.
	cmd := exec.CommandContext(ctx, "git", finishCleanupBranchVerb, finishCleanupBranchDeleteFlag, branch) // branch-delete-ok: this rung's guard IS git's own -d (the comment above: "`-d` still refuses a branch that carried commits master does not have") — the sweeper never force-deletes, so the non-durable-tip lock is not the mechanism here and adding a second refusal would be the same question asked twice
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, firstNonEmptyLine(out))
	}
	return nil
}

// worktreeSweepBranchIsSpent reports whether a branch carries NOTHING the
// integration branch does not already have — `git merge-base --is-ancestor
// <branch> <integration>` exit 0. It is the SAME measurement the merger's own
// spent-branch arm makes (cmd/agent/merge_run.go), reused rather than
// re-derived, and it is deliberately conservative in the direction that
// matters: an unanswerable question (a missing integration branch, a non-repo,
// a vanished ref) answers FALSE, so the ref is left alone and `git branch -d`
// never even runs.
//
// It does not replace `-d`: the deletion still goes through git's own checks
// (checked out elsewhere? unmerged? the ref is refused by git). This predicate
// only decides whether the arm OFFERS the ref at all, so a branch with its own
// commits is not handed to a deletion path that would report a refusal for
// every ordinary ground.
func worktreeSweepBranchIsSpent(repo, branch string) bool {
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(branch) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", reclaimMergeBaseVerb, worktreeSweepIsAncestorFlag, branch, IntegrationBranchName)
	cmd.Dir = repo
	return cmd.Run() == nil
}

// worktreeSweepIsAncestorFlag is git's one-way projection of `merge-base`
// ("is the first revision an ancestor of the second?") — the spelling the
// spent-branch arm uses, named once (Rule 1b).
const worktreeSweepIsAncestorFlag = "--is-ancestor"

// worktreeSweepChatOrphanClause is appended to the orphan escalation when the
// orphan is a CHAT GROUND's — see worktreeSweepOrphanReason. It states the two
// facts the ordinary remedy cannot supply (there is no row by design, and the
// session is over) so the operator knows the dirt is a finished conversation's
// deliverable rather than a live run's.
const worktreeSweepChatOrphanClause = " (chat ground: session "

// worktreeSweepChatDirtyReasonSuffix is appended to the ordinary dirty report
// when the dirty registration is a CHAT GROUND's — see guard ④ in
// worktreeSweepJudge. One spelling (Rule 13), so the judge and the tests read
// the same sentence.
const worktreeSweepChatDirtyReasonSuffix = " (chat ground: session "

// worktreeSweepChatCarriesWorkReason is the sentence the runner renders when a
// chat ground's session is over but its branch is not spent — the keep that
// protects commits whose only home is that ref (Rule 15: the refusal names the
// reason a reader can act on, and the action is `git push` / a task).
const worktreeSweepChatCarriesWorkReason = " — but its branch carries commits the integration branch does not have, and that ref is their only home: push or file a task, then it is collectable"

// worktreeSweepJSON renders the result for the operator verb.
func worktreeSweepJSON(res worktreeSweepResult) string {
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}", err.Error())
	}
	return string(raw)
}
