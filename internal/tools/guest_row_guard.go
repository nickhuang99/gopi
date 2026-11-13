// A GUEST process may not release a task row's worktree of the repository it
// only stands in — the ROW-reconciliation half of the worktree-eviction defect
// (epic 01a0c2b5; the SWEEP half is worktreeRuleSelfCheckout, 832794fe).
//
// THE MEASURED DEFECT (2026-09-21, reproduced in this change's own evidence).
// The arms in this package decide over a row whose deliverable names a repo and
// a worktree, and each arm was guarded about the REGISTRATION (dirty? held?
// row-owned? too young?) and about the ACTOR being the row's CLAIMANT — but
// never about WHERE the actor stands. So a process merely standing in a LINKED
// worktree of repo R (a "GUEST") reconciled R's task rows exactly like the
// process that owns R: rows are ledger-keyed, and a per-task ledger
// (GOPI_MEMORY_DB) cannot observe another task's claims, so a PEER's row read
// as an unowned/abandoned one and its checkout was freed — including the
// checkout the guest was itself standing in. Measured in the wild on the sweep
// fix's own closing gate run: the worktree it ran from
// (/home/nick/workspace/gopi-ownrel) was gone afterwards ("getwd: no such file
// or directory") while a planted decoy survived, which is why the two halves
// are separate fixes.
//
// WHY THE GUARD LIVES HERE AND NOT AT THE CALL SITES. The rows are reconciled
// by helpers that already TAKE the repo as a parameter, and cmd/agent reaches
// them from several faces (the worker round's own-release tail, the wrapper's
// close-out, `gopi task finish-cleanup`). CORRECTED 2026-10-30 (task 01a18ef7):
// this list used to begin with "the resident lifecycle tick", which has no
// production caller and was deleted. A
// guard at those call sites was written once and REVERTED: it took cmd/agent
// from green to 8 failures, because at a call site the repo under judgement is
// not in hand — the helpers hold it, their callers do not. Asking the question
// where the repo IS known (this file, reached from reclaimDisposition and from
// reclaimSelfOwnRepos) gives every face ONE implementation (Rules 13/14).
//
// THE CRITERION is the sweep's own self-checkout question, RE-USED rather than
// re-derived: a repository is its COMMON git dir (`git rev-parse
// --git-common-dir`), which the main checkout and every linked worktree share,
// so it is the identity a guest and its owner both resolve to. The process is a
// GUEST of repo R iff
//
//	R's common git dir == this process's repository's common git dir, AND
//	this process's own top level is NOT the directory holding that git dir.
//
// Both halves are required, and each answers the opposite failure:
//
//	half 1 (identity) — without it every row of every repo would be refused,
//	  because a process standing in an unrelated repository — or in none — has
//	  no identity to compare and must not withhold ground it cannot name;
//	half 2 (residency) — with identity alone, the process that OWNS R would
//	  lose its mandate over R (it is, by definition, inside R's common dir).
//	  Residency is why a run standing in R's MAIN checkout keeps the whole
//	  mandate, INCLUDING reconciling its OWN row — the producer leg that
//	  releases the worktree a finished run developed in, and the leg a worker
//	  keeps using after its round hands its cwd back to the filesystem root.
//
// WHAT IS REFUSED, AND WHAT IS NOT (the narrowness is the whole safety of this
// rung). A refusal is emitted ONLY on a POSITIVE guest identification, of one
// of two shapes:
//
//	① the process's repository IS the row's repository and the process is not
//	   resident in that repository's main checkout;
//	② the process stands in a LINKED worktree whose repository identity could
//	   not be read at all — it is demonstrably a guest of SOME repository, so
//	   it may not act with a resident's authority over any of them.
//
// An actor standing outside any repository at all (a worker that has handed its
// cwd back to "/", a one-shot process run from a temp directory) keeps the full
// mandate: it is a guest of nothing. An unreadable identity is therefore never
// rendered as "unowned" — it is rendered as what it is, and the refusal
// sentences below quote the actor's own checkout and the repository's main
// checkout, so a withheld directory is never a silent one (Rule 15).
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nick/gopi/internal/scratch"
)

const (
	// guestRowMainUnresolved / guestRowActorOutsideRepo are the evidence
	// strings a refusal quotes when the corresponding half could not be named.
	// Both say "unidentified", never "unowned".
	guestRowMainUnresolved   = "(repository main checkout unidentifiable)"
	guestRowActorOutsideRepo = "(this process stands in no repository)"

	// guestRowRefusalFmt / guestRowOwnRefusalFmt are the ONE spelling of each
	// refusal sentence (Rule 13). Each names the ARM and the two facts that
	// decided it: the checkout the process stands in, and the main checkout of
	// the repository it was asked to reconcile.
	guestRowRefusalFmt    = "guest-row-reconcile refusal (arm guestRowReleaseRefusal): this process stands in the linked worktree %s, not in the main checkout %s of that repository — a guest does not release or reconcile another row's worktree of %s (path B of the worktree-eviction defect)"
	guestRowOwnRefusalFmt = "guest-row-reconcile refusal (arm guestRowOwnReleaseRefusal): this process stands in the linked worktree %s, not in the main checkout %s of that repository — it does not release the worktree of its own row in %s; the process that owns the repository (its main checkout, or a resident) does"
	// guestRowUnresolvedGuestFmt is the arm-② sentence: the actor is a guest of
	// some repository it cannot name, so it releases nothing.
	guestRowUnresolvedGuestFmt = "guest-row-reconcile refusal (arm guestRowUnresolvedGuestRefusal): this process stands in the linked worktree %s but that repository's identity is unreadable — an actor that cannot name the repository it stands in does not release worktrees of %s"

	// guestRowOwnCheckoutRefusalFmt is the arm-③ sentence: the directory under
	// judgement IS the checkout the actor stands in, so the removal would delete
	// the ground under the running process. One spelling (Rule 13); it names the
	// arm, the checkout, and the mechanism (git deletes the directory the process
	// is standing in, and the loss stays invisible until some later step asks for
	// its own cwd).
	guestRowOwnCheckoutRefusalFmt = "own-ground refusal (arm guestRowOwnCheckoutRefusal): the worktree %s is the checkout THIS process is standing in — removing it would delete the ground under the running process, and the loss stays invisible until some later step asks for its own cwd (getcwd: no such file or directory); the directory is released by the arm that does not stand in it"

	// guestRowGitdirDirName is the git dir's own name (`<checkout>/.git`): the
	// main checkout is the directory whose child of this name IS the common git
	// dir, which is what the residency half compares.
	guestRowGitdirDirName = ".git"
	// guestRowCommonDirMarker is the sub-directory a linked worktree's git dir
	// lives under inside the common dir (`<common>/worktrees/<name>`) — the
	// parse's proof that an unreadable identity still belongs to a GUEST.
	guestRowCommonDirMarker = "worktrees"
)

// guestRowGuardOffEnv is the RED-PROOF switch: "off" makes both refusal
// predicates answer "" — i.e. it reproduces the PRE-FIX behaviour (no guard at
// all) from the fixed source, which is how the guard's absence can be measured
// on a tree where the guard is wired. It is read in exactly ONE place, below.
const guestRowGuardOffEnv = "GOPI_TEST_GUEST_ROW_GUARD"

// guestRowGuardDisabled reports whether the red-proof switch is set. The
// production reading is the default (guard ON): ONLY the literal "off" disables
// it, so an empty or unknown value can never switch a guard off by accident
// (Rule 15's direction — a broken knob must not read as "no guard").
func guestRowGuardDisabled() bool {
	return strings.TrimSpace(os.Getenv(guestRowGuardOffEnv)) == "off"
}

// guestRowVerdict is the answer to "where does THIS process stand with respect
// to a repository", resolved once per process per repo (see
// guestRowRepoVerdict). It carries the EVIDENCE, not just the boolean, because
// every refusal must name what it compared (Rule 15).
type guestRowVerdict struct {
	// isGuest is true only on a positive guest identification (arm ① or ②).
	isGuest bool
	// unresolvedGuest is true for arm ② — a guest whose repository identity
	// could not be read.
	unresolvedGuest bool
	// checkout is the top level THIS process stands in (empty when it stands in
	// no repository).
	checkout string
	// main is the main checkout of the repository the verdict is about (empty
	// when the common dir names none).
	main string
}

// guestRowRepoVerdict answers the criterion for one repo, memoized for the
// process. Every refusal sentence below is built on it, so the identity is
// resolved in exactly ONE place per repo.
//
// THE KEY IS (repo, THE ACTOR'S CWD), not the repo alone. The verdict folds in
// WHERE THE ACTOR STOOD when it was computed, so a process that MOVES would
// otherwise be judged by a position it has left — the measured 2026-09-25
// `gopi session resume` shape, where the close-out hands its cwd back to the root before
// running the cleanup and a repo-keyed cache still reported the worktree it had
// just finished in. Keying on the cwd keeps the memo (one git call pair per
// distinct pair) without letting it describe a moment that has passed.
func guestRowRepoVerdict(repo string) guestRowVerdict {
	repo = strings.TrimSpace(repo)
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "" // unreadable: never a cache key, so the answer is re-measured
	}
	guestRowVerdictMu.Lock()
	defer guestRowVerdictMu.Unlock()
	if guestRowVerdicts == nil {
		guestRowVerdicts = map[guestRowVerdictKey]guestRowVerdict{}
	}
	k := guestRowVerdictKey{Repo: repo, Cwd: cwd}
	if v, ok := guestRowVerdicts[k]; ok {
		return v
	}
	v := guestRowRepoVerdictUncached(repo)
	guestRowVerdicts[k] = v
	return v
}

// guestRowVerdictKey is the memo's key: which repo was asked about, and where
// the actor stood when it was asked (see guestRowRepoVerdict).
type guestRowVerdictKey struct {
	Repo string
	Cwd  string
}

// guestRowVerdicts / guestRowVerdictMu memoize guestRowRepoVerdict. The cache
// is process-wide — a repository's identity does not change under a pass, and
// neither does the actor's position between two questions asked from one place —
// and guarded, because the arms are reachable from more than one tick.
var (
	guestRowVerdictMu sync.Mutex
	guestRowVerdicts  map[guestRowVerdictKey]guestRowVerdict
)

// guestRowRepoVerdictUncached resolves the criterion, re-using the sweep's
// memoized self-identity (worktreeSweepSelfPosition — the repo root, its common
// git dir and the guest bit, measured together, per CWD) and its
// common-git-dir reader: one process has ONE position, and two resolutions of it
// could disagree.
func guestRowRepoVerdictUncached(repo string) guestRowVerdict {
	if strings.TrimSpace(repo) == "" {
		// No repo named: there is nothing to compare the actor against, so the
		// arms' own "row carries no worktree/repo" refusal is what should fire —
		// never this one (running git in whatever cwd the process happens to
		// have would invent an identity for a repo the row never named).
		return guestRowVerdict{}
	}
	self := worktreeSweepSelfPosition()
	selfCheckout := self.Repo
	if selfCheckout == "" {
		// The actor stands in no repository: a guest of nothing keeps the
		// mandate (see the file doc).
		return guestRowVerdict{checkout: guestRowActorOutsideRepo}
	}
	if self.Common == "" {
		// The actor IS inside a repository root whose identity could not be
		// read. Arm ② applies only when that root is a linked worktree's; a
		// broken main checkout is not a guest of anything.
		if inLinkedWorktreeRoot(selfCheckout) {
			return guestRowVerdict{isGuest: true, unresolvedGuest: true, checkout: selfCheckout}
		}
		return guestRowVerdict{checkout: selfCheckout}
	}
	common, err := worktreeSweepCommonGitDir(repo)
	if err != nil {
		// The CANDIDATE repo cannot be named, so the actor cannot be proven a
		// guest of it: no refusal here — the row's own guards (and the
		// registration probe) still stand between this and a removal.
		return guestRowVerdict{checkout: selfCheckout}
	}
	canonical := scratch.CanonicalPath(common)
	if canonical != self.Common || !self.Guest {
		// A DIFFERENT repository (a guest keeps its mandate over every repo it
		// does not stand in), or the same repository with the process RESIDENT
		// in its main checkout (the full mandate, its own row included).
		return guestRowVerdict{checkout: selfCheckout, main: worktreeSweepMainCheckoutOf(canonical)}
	}
	return guestRowVerdict{
		isGuest:  true,
		checkout: selfCheckout,
		main:     worktreeSweepMainCheckoutOf(canonical),
	}
}

// inLinkedWorktreeRoot reports whether a checkout's OWN git link names a linked
// worktree — the parse that keeps arm ② honest when `git rev-parse` could not
// answer: a linked worktree's `.git` is a FILE pointing at
// `<common>/worktrees/<name>`, while a main checkout's is a DIRECTORY.
func inLinkedWorktreeRoot(checkout string) bool {
	raw, err := os.ReadFile(filepath.Join(checkout, guestRowGitdirDirName))
	if err != nil {
		return false
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), finishCleanupGitDirFilePrefix))
	if gitdir == "" {
		return false
	}
	return filepath.Base(filepath.Dir(gitdir)) == guestRowCommonDirMarker
}

// guestRowOwnCheckoutRefusal answers the PRODUCER arm's THIRD question — may
// this process release a worktree that IS the checkout it is standing in? It
// returns the NAMED refusal sentence when it may not, and "" when it may.
//
// The criterion is the file's own (`guestRowRepoVerdict.selfCheckout`, the ONE
// resolution of the actor's position), but it is a DIFFERENT question from
// either sibling above, and it needs neither the candidate repo nor the ledger:
//
//	guestRowReleaseRefusal    takes the receiver's ROW, so it asks whether the
//	                          ROW is mine — a foreign row is never released.
//	guestRowOwnRefusal        takes the RECEIVER's repo, so it asks whether the
//	                          PAIR (actor, receiver) is mine.
//	this one                  takes NOTHING: the receiver is the actor's own
//	                          ground, and the ground is the one thing a process
//	                          must never free out from under itself.
//
// WHY IT IS NOT REDUNDANT WITH `guestRowOwnRefusal`, measured (breach 2026-09-21,
// `~/workspace/gopi-jieba-searchkw`, row 01a0c574, holder pid 1191072 alive,
// branch 14 ahead): the PRODUCER arm resolves its candidate rows from the LEDGER
// (`reclaimSelfRows`, keyed on the claim stamp), so one of them may name the very
// directory this process is STANDING IN — and `guestRowOwnRefusal` answers "" for
// exactly that pair, because the repo the row names is the repo the actor is a
// guest of. A row's deliverable records exactly ONE worktree, and a run that
// marks and stamps its ground late (the reported breach: the stamp named the
// directory the run was standing in) puts the actor's OWN ground in the candidate
// set. `git worktree remove` deletes it under the running process.
//
// The refusal NEVER fires for a checkout the actor merely stands in,
// deliberately — the `SelfRepoGuestCheckout` fact alone is not a reason to
// withhold a ROW's release (the sibling arm's whole point is that a process
// resident in the main checkout runs the full mandate, and widening THAT gate
// is what the guest guard's own negative tests forbid). The weaker "resident in
// this repo" fact is wrong in the same direction for the same reason: a worker
// round deliberately runs resident in the main checkout, and its released
// directories are the rows' own linked worktrees — not the main checkout. The
// only act this arm withholds is deleting the directory the process is standing
// in.
//
// This is NOT a liveness re-implementation (Rules 13/14): it reads no /proc,
// compares no pid, and calls none of the liveness ladders — the identity it
// compares is the actor's OWN checkout, resolved by the shared memoized reader
// (`worktreeSweepSelfOwnedRepo`, one resolution per process).
//
// Guest-status independent ON PURPOSE: a guest is refused by its own arm
// (`guestRowOwnRefusal`) before this one can fire, and a process resident in a
// DIFFERENT repo's main checkout is no guest at all — so the actor's own ground
// is unrepresentable by either of them and needs its own rung.
func guestRowOwnCheckoutRefusal(worktree string) string {
	if guestRowGuardDisabled() {
		return ""
	}
	worktree = scratch.CanonicalPath(worktree)
	if worktree == "" {
		return ""
	}
	// The actor's position comes from the SHARED one-resolution reader
	// (worktreeSweepSelfPosition, memoized per CWD — the same source the
	// guest rungs above read through guestRowRepoVerdict). It is NOT asked
	// through guestRowRepoVerdict(""): that entry point takes the CANDIDATE repo
	// and answers an empty verdict when it has none, which would make this rung a
	// silent no-op — measured while wiring it, and the reason the reader is named
	// explicitly here.
	//
	// IT IS READ LIVE, never from the guestRowVerdicts cache: that cache is keyed
	// on the CANDIDATE repo and holds a verdict about where the actor stood when
	// the repo was first asked about. A producer that CHANGES its position — the
	// resume close-out, which hands its cwd back before running the cleanup
	// (measured 2026-09-25) — would otherwise be judged by where it stood while
	// it was still working, which is exactly the directory it is now safe to
	// free. The position reader re-measures on every cwd change, so this line is
	// the answer for NOW.
	checkout := scratch.CanonicalPath(worktreeSweepSelfPosition().Repo)
	if checkout == "" {
		return ""
	}
	if worktree != checkout {
		return ""
	}
	return fmt.Sprintf(guestRowOwnCheckoutRefusalFmt, worktree)
}

// guestRowReleaseRefusal answers the ROW arms' question — may THIS process free
// (or repair) the worktree of a row whose deliverable names repo? It returns a
// NAMED refusal sentence when it may not, and "" when it may.
//
// The question is asked per row rather than once per pass on purpose: a pass's
// rows may name several repositories, and the verdict is about the PAIR
// (actor, repo) — a guest keeps freeing rows of every repo it does not stand in.
func guestRowReleaseRefusal(repo string) string {
	if guestRowGuardDisabled() {
		return ""
	}
	v := guestRowRepoVerdict(repo)
	switch {
	case v.unresolvedGuest:
		return fmt.Sprintf(guestRowUnresolvedGuestFmt, checkoutText(v.checkout), repo)
	case v.isGuest:
		return fmt.Sprintf(guestRowRefusalFmt, checkoutText(v.checkout), mainCheckoutText(v.main), repo)
	default:
		return ""
	}
}

// guestRowOwnRefusal answers the PRODUCER arm's question (the rows THIS process
// claims): may it release its own row's worktree in repo? A run owns its row's
// worktree only where it also owns the repository it stands in — a GUEST
// standing in a linked worktree of R is not the process R's rows belong to, and
// releasing from there is exactly how the measured case lost the checkout it
// was running from.
func guestRowOwnRefusal(repo string) string {
	if guestRowGuardDisabled() {
		return ""
	}
	v := guestRowRepoVerdict(repo)
	switch {
	case v.unresolvedGuest:
		return fmt.Sprintf(guestRowUnresolvedGuestFmt, checkoutText(v.checkout), repo)
	case v.isGuest:
		return fmt.Sprintf(guestRowOwnRefusalFmt, checkoutText(v.checkout), mainCheckoutText(v.main), repo)
	default:
		return ""
	}
}

// checkoutText renders the actor's top level for a refusal sentence.
func checkoutText(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return guestRowActorOutsideRepo
	}
	return dir
}

// mainCheckoutText renders the repository's main checkout for a refusal
// sentence.
func mainCheckoutText(main string) string {
	if strings.TrimSpace(main) == "" {
		return guestRowMainUnresolved
	}
	return main
}
