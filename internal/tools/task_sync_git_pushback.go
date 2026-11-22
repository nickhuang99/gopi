package tools

// Shared push-back core for the (merged-branch flood) bugfix
// (epic: the default git push-back pushed EVERY non-master local branch to
// the peer, resurrecting branches the server had adopted-into-master and
// then deleted — the 2026-09-02 14:39 stale-branch flood on peer1).
//
// The server's adoption chain integrates a deliverable branch into server
// master and then DELETES the branch ref (task_adoption.go ③a). A stale
// client that still lists that branch and re-pushes it re-creates the
// deleted ref from a tip that is now an ANCESTOR of server master. The fix:
// filter the push list to branches whose tip is NOT an ancestor of the
// server's current master. Only truly-unmerged work (e.g. the just-finished
// task's deliverable branch) crosses the wire; already-integrated branches
// are skipped, so a server-side deletion stays deleted. A no-branch flush
// (nothing unmerged) pushes nothing — requirement (d).
//
// Both blanket push sites route through this core:
//   - gitPushbackDefault      (harvest's git push-back phase)
//   - GitSyncPushLocalBranches (serve-tick client pass / `gopi task push`)
//
// Per-branch NAME COLLISION on the server (both sides independently created
// the same branch name — rare, task sets differ) makes the push non-fast-
// forward. That case is asked EXPLICITLY, before the push: the lane reads the
// peer's own ref of that name into the gitPushcheck mirror namespace and
// classifies it (absent → push; contained in ours → a fast-forward, pushed
// and said so; neither contained in the other → REFUSED BY NAME with both
// tips and the remedy). A collision is the one outcome that needs a human
// decision, so it is never left to be inferred from a push failure — and it
// is never worked around: the core does not force and does not fork (the
// rename-retry was removed on 2026-09-17, see gitPushbackPushOne).
//
// The per-task branch push USED to be the finish-cleanup chain's job (retired
// whole in 58b5618b; the chain's entry point, TaskFinishCleanupSessionID, no
// longer exists in production — task 01a18f76). These periodic passes handle the
// residue that outlived completion — hence the merged-ancestor gate, not a
// "current task" notion that a serve tick does not have.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// Push-back filtering tunables (Rule 1 — every literal in one place).
const (
	// gitPushbackCheckNamespace names the reserved remote-tracking namespace
	// this lane mirrors into: the peer's master at <ns>/master, and (the
	// peer-ref gate) one peer branch at <ns>/peer/<branch>. Reserving our own
	// namespace keeps every comparison object-local and off the client's
	// ordinary remote-tracking set. The VALUE has one definition, in
	// internal/config (Rule 14): the read-only ref-lag check in this package
	// names the same namespace to build its mirror-ref list, and two spellings
	// of one ref would let the measurement read a namespace the lane never
	// writes.
	gitPushbackCheckNamespace = config.DeliveryPushbackMirrorNamespace

	// gitPushbackCheckRootRef is the namespace PREFIX (trailing slash) that every
	// mirror ref sits under and that the pass's prune enumerates — refs/heads
	// and refs/remotes alike are spelled through the constants in this block,
	// never inline (Rule 1b).
	gitPushbackCheckRootRef = "refs/remotes/" + gitPushbackCheckNamespace + "/"

	// gitPushbackCheckRef is the local ref a pass fetches the SERVER's
	// master into before per-branch ancestor checks.
	gitPushbackCheckRef = gitPushbackCheckRootRef + "master"

	// gitPushbackFetchTimeout caps each git invocation in the push-back
	// core (server-master fetch + per-branch merge-base + push). A busy
	// peer must fail fast, never hang the harvest/serve pass.
	gitPushbackFetchTimeout = 60 * time.Second

	// pushbackFfableFactPhrase / pushbackNotFfableFactPhrase are the two
	// spellings of the ONE fact this lane reports about a pushed branch's
	// alignment with the peer's trunk: whether the branch's tip CONTAINS the
	// peer's master mirror tip. The note fmts below interpolate them (Rule 13
	// — the phrase exists once, and a reader that greps the report and the
	// constant cannot drift).
	pushbackFfableFactPhrase    = "contains the peer's " + masterBranchName
	pushbackNotFfableFactPhrase = "does not contain the peer's " + masterBranchName

	// pushbackNotFfableFactNoteFmt is the FACT — not a refusal — appended to
	// the Pushed entry of a branch whose tip does NOT contain the peer's
	// master. See gitPushbackPlan below: the ff question is reported, never
	// refused. Args: peer master tip.
	pushbackNotFfableFactNoteFmt = pushbackNotFfableFactPhrase + " at %s — the peer's own landing call decides it: " +
		"the landing operation is `merge --ff-only`, so an unaligned branch parks there until it is aligned"

	// pushbackFfableFactNoteFmt is the same fact's positive side, appended to
	// a Pushed entry only when the entry would otherwise be bare AND the
	// branch is in the not-fast-forwardable set of the same flush — see
	// pushbackPushedEntry: an ordinary aligned delivery keeps the bare name
	// (existing readers rely on that shape, and the machine-readable half
	// carries the fact). Args: peer master tip.
	pushbackFfableFactNoteFmt = pushbackFfableFactPhrase + " at %s — the peer's `merge --ff-only` can land it as delivered"

	// gitPushbackPeerRefMirrorFmt is the local mirror ref a pass mirrors the
	// PEER's own ref of one branch into, so that branch's ancestry question is
	// answered object-locally (the same trick gitPushbackCheckRef plays for the
	// peer's master, in its own namespace so a branch can never overwrite the
	// master mirror). %s = the branch name.
	gitPushbackPeerRefMirrorFmt = gitPushbackCheckRootRef + "peer/%s"

	// gitPushbackPeerRefHeadsFmt is the PEER-side spelling of that ref — what
	// `ls-remote`/`fetch` are asked for, and what the refusal line names so an
	// operator can look at it on the peer. %s = the branch name.
	gitPushbackPeerRefHeadsFmt = "refs/heads/%s"

	// gitPushbackShortShaLen is the abbreviation length the collision line
	// reports tips at: short enough to read on one line, long enough to
	// `git show` from either side (Rule 1 — no inline literal).
	gitPushbackShortShaLen = 7

	// pushbackPeerRefCollisionReasonFmt is the report line for a NAME
	// COLLISION: the peer already holds refs/heads/<name> and neither tip
	// contains the other, so the push cannot be a fast-forward. It is its own
	// line — distinct from every transport, permission or fetch failure —
	// because it is the one outcome that needs a HUMAN DECISION (two people, or
	// two hosts, used one name for different work), and a report that renders
	// it as an anonymous "peer rejected the push" hides the decision.
	//
	// It states the branch, BOTH tips (short shas, so either side can be
	// inspected) and the remedy, and it states what did NOT happen (nothing was
	// pushed, the peer's ref is untouched) because this lane refuses rather
	// than resolving: forcing would rewrite the peer's history and forking
	// mints a second branch the peer cannot land either (measured 2026-09-17:
	// one topic arrived as five branches on one tip, devdoc/
	// rework-ff-convergence.md). Why a rename is not the remedy either: the
	// two histories belong to different work, so only a human can say which
	// name keeps which work.
	//
	// Args: branch, branch, peer tip, our tip.
	pushbackPeerRefCollisionReasonFmt = "%s: NAME COLLISION — the peer's refs/heads/%s is at %s while ours is at %s and neither contains the other, so this push cannot be a fast-forward; nothing was pushed and the peer's ref is untouched. This lane never forces (that would rewrite the peer's history) and never forks into a new name (the peer cannot land that either): reconcile the two tips deliberately — merge the peer's tip into yours and re-deliver — or, if the two histories are unrelated, deliver this work under a different branch name"

	// pushbackPeerRefFastForwardNoteFmt is the report entry for a branch whose
	// push UPDATED — rather than created — the peer's ref of that name: the
	// peer's tip was contained in ours, so git fast-forwarded it from the
	// peer's tip to ours. The entry still starts with the branch name (a
	// Pushed entry is keyed by the branch reaching the peer under its own name
	// — existing readers and tests rely on that); the note is appended.
	// Args: branch, peer tip, our tip.
	pushbackPeerRefFastForwardNoteFmt = "%s (fast-forwarded the peer's ref of this name: %s → %s)"

	// pushbackPeerRefUnmeasuredNoteFmt is appended to a branch's report entry
	// when the peer-ref probe could NOT answer (the peer is unreachable, or the
	// reported tip does not resolve locally). The branch is NOT refused on it:
	// an unmeasured premise is not a finding (Rule 15), so the push is
	// attempted and git's own answer is reported — with this note, so a later
	// reader knows the peer-ref question was never actually answered. %s = the
	// measurement that failed.
	pushbackPeerRefUnmeasuredNoteFmt = "peer-ref probe UNMEASURED (%s) — no collision was proven or ruled out"

	// pushbackPushRefusedReasonFmt is the report line for a branch that PASSED
	// the containment gate and had NO colliding peer ref, yet whose push the
	// peer still rejected — i.e. a transport, authentication or permission
	// failure, or a refusal this lane could not name. It is deliberately
	// anonymous about the CAUSE (git's own one-line stderr is on the harvest
	// error line) because a named cause here would be a guess: the peer-ref
	// probe above already answered the only git-domain question there was.
	pushbackPushRefusedReasonFmt = "%s: the peer rejected the push — this lane does not fork or force; reconcile with the peer and re-deliver"

	// pushbackPushRefusedDiagFmt appends git's OWN sentence to a refusal. The
	// lane's verdict answers "what does this lane decide"; only git answers
	// "why did the wire refuse", and the two are not interchangeable: a
	// non-fast-forward, a permission denial and a push that never left this
	// machine all render as the same three words above (Rule 15 — an
	// unmeasured cause reported as the lane's finding).
	pushbackPushRefusedDiagFmt = "git said: %s"

	// pushbackRemotePushURLKeyFmt is git's own config key for the push half of
	// a remote (`remote.<name>.pushurl`) — the key whose value is a URL git
	// reads IN PLACE OF the remote's name, and therefore the key that can make
	// a push fail against a target that is not a repository at all.
	pushbackRemotePushURLKeyFmt = "remote.%s.pushurl"

	// pushbackBlockedPushURLLineFmt is the REFUSAL'S PLAIN FINDING for a push
	// that failed because the target is not a repository and the branch's own
	// remote carries a pushurl that is not one either. It names git's
	// diagnostic, the config key, its value and the command that clears it —
	// the four things a reader needs to turn the refusal into an action.
	//
	// MEASURED 2026-10-08 on HP: `remote.peer1.pushurl = DISABLED-no-push`
	// (installed 2026-10-03 by the hermes-agent session as a guardrail) makes
	// `git push peer1 master:master` fail with git's own
	// `fatal: 'DISABLED-no-push' does not appear to be a git repository`, and
	// the operator-facing report said only "the peer rejected the push". The
	// same measurement shows the guardrail does NOT govern this lane: gopi
	// composes `<alias>:<path>` (`GitSyncServerTarget()`, the same shape
	// `finishCleanupServerTarget()` renders), which git hands to ssh directly,
	// so the block is a false guardrail AND an unreadable refusal in one.
	pushbackBlockedPushURLLineFmt = "the target %q is blocked by its own git config: %s = %q — git is not reading that remote for this push (this lane passes a direct target, which remote.<name>.pushurl does not govern), so pushing is refused before anything leaves this machine; clear the block with `git config --unset %s` and re-deliver"

	// pushbackAliasNotRemoteLineFmt is the REFUSAL'S PLAIN FINDING for the
	// other half of git's "not a repository" sentence — the one the pushurl
	// arm above CANNOT see. A target's ALIAS half (`<alias>:<path>`, the
	// shape finishCleanupServerTarget/ GitSyncServerTarget render) names an
	// ssh host, and NOTHING requires that host to also be a configured git
	// REMOTE in this repo. When it is not, git's diagnostic is the same
	// sentence it prints for a blocked pushurl — `fatal: '<alias>' does not
	// appear to be a git repository` — but the cause is a NAME MISMATCH
	// between the declared topology (settings.json sync_peers) and this
	// repo's remotes, not a config block. Before this line the operator got
	// git's sentence and no way to see WHICH of the two worlds they were in.
	//
	// MEASURED 2026-10-12 on sager: `sync_peers: ["peer1"]` while
	// `git remote -v` lists only `origin` — the report said only "the peer
	// rejected the push — git said: fatal: 'peer1' does not appear to be a
	// git repository", naming neither the declared peer, nor the remotes
	// that DO exist here, nor that a push is allowed to fail purely because
	// the two names disagree.
	//
	// The arity is 4 because the finding is a COMPARISON: the target, the
	// alias git could not resolve, the declared topology's key, and the
	// remotes this repo actually has (rendered by
	// gitPushbackRemoteNameList — "none" when the repo has no remote at all).
	pushbackAliasNotRemoteLineFmt = "the target %q does not resolve on this machine: its host half %q is not a configured git remote here, so git has nothing to hand the push to — the declared topology (%s) and this repo's git remotes are two different NAME SPACES and they disagree; this repo's remotes are: %s. Fix the naming on whichever side is wrong (add a remote named %q, or declare the peer under the name this repo already has) — nothing was pushed and no ref was moved"

	// pushbackRemoteNameVerb is git's verb that LISTS the repo's configured
	// remotes — the measurement the finding above reports, and the reason
	// the diagnosis is a comparison instead of a restatement of the failure.
	pushbackRemoteNameVerb = "remote"

	// pushbackRemoteNameNone is what the listing renders when the repo has no
	// configured remote at all: the same finding, one step emptier, and it
	// never reads as a blank (Rule 15 — a measurement that found nothing must
	// be distinguishable from one that never ran).
	pushbackRemoteNameNone = "none (this repo has no configured remote)"

	// pushbackRemoteNameListSep is the separator the listing joins remote
	// names with. A comma keeps a name list readable inside the sentence and
	// cannot be confused with the ':' of a target.
	pushbackRemoteNameListSep = ", "

	// pushbackTestFenceMessage is the message the in-process test fence records
	// when this pass would have pushed a deliverable out of test space. It is
	// the CALLER's own sentence: the refusal line the shared fence renders
	// already names the condition, the blocked target and the opt-out, and a
	// second wording here would be a second answer to the same question.
	//
	// The message is NOT run through a fmt verb: it is the fence's text
	// verbatim. It rides the seam's report exactly where an ordinary refusal
	// rides — failing the whole flush — so a reader (the operator, or the pass
	// that harvests it) sees one shape for "this lane refused to deliver".
	pushbackTestFenceMessage = "test fence: %s"

	// pushbackUnrelatedHistoryReasonFmt is the report line for a branch that
	// shares NO commit with the peer's trunk — an ORPHAN history (a scratch
	// repository `git init`-ed elsewhere and pushed in). Measured 2026-09-21:
	// `feat/sync-conflict-completed` arrived with root 39fd968a against
	// master's e9538e78 and its own two-file `init`.
	//
	// It is a SEPARATE line from the alignment FACT (pushbackNotFfableFactNoteFmt)
	// this lane now renders, because the remedy that fact would imply is
	// unperformable here: `git merge master` into a history with no
	// master is not a resolution, it is git's own
	// `fatal: refusing to merge unrelated histories` (exit 128) — the refusal
	// the peer's merger then hit by hand, and the state the merge-backlog
	// generator re-filed four times (rows 01a0c201, 01a0c2e1, 01a0c2e7,
	// 01a0c55c). The remedy named here is the only one that terminates: the ref
	// did not come from this repository's history, so it is discarded (the
	// branch, its commits and this repo's refs are otherwise untouched — a
	// refusal may not delete), and anything genuinely wanted is re-created on
	// top of master and re-applied.
	pushbackUnrelatedHistoryReasonFmt = "%s: shares no common ancestor with %s (git merge-base finds no shared commit) — push refused; this ref was never cut from %s's history and no merge can land it: archive it with the project's one archive writer (`gopi task archive %s --reason " + ArchiveReasonUnrelatedHistory + "`), which records the decision and reclaims the ref through its own audited arm — and if the change is wanted, re-create the branch on %s and re-apply the work there"
)

// gitPushbackFlush is one client→server flush's outcome.
type gitPushbackFlush struct {
	Pushed  []string // branches that reached the server under their own name
	Skipped []string // branches skipped: already integrated into server master
	Failed  []string // branches refused or failed, each carrying its reason
	// FastForwarded names the branches (bare branch name, no note) whose push
	// UPDATED the peer's own ref of that name down-to-ours. Their pretty
	// report entry in Pushed carries the from→to note
	// (pushbackPeerRefFastForwardNoteFmt); this list is the machine-readable
	// half, so a caller can tell "the peer already owned this name" from
	// "this name was new at the peer" without parsing prose. The rename-retry
	// is NOT this: a fork mints a new name, a fast-forward moves the peer's
	// existing ref forward along our history.
	FastForwarded []string
	// FastForwardable names the branches that were PUSHED and whose tip
	// CONTAINS the peer's master mirror — i.e. the peer's own landing
	// operation (`git merge --ff-only <branch>`, repo_sweep.go) can take them
	// as delivered. NotFastForwardable is its complement among the pushed
	// branches: pushed, and the peer's `merge --ff-only` cannot land them
	// until they are aligned. The two lists are the machine-readable half of
	// the alignment FACT (the Pushed entry carries the prose for a reader);
	// a branch appears in exactly one of them, and a branch in neither was
	// not pushed by this flush.
	//
	// They are NOT the same list as FastForwarded above, and the difference is
	// measured: a branch the peer has never seen is pushed AND is
	// fast-forwardable (the peer's merger can take it), while FastForwarded
	// answers the other question — "did the push update an EXISTING peer ref".
	FastForwardable    []string
	NotFastForwardable []string
}

// gitPushbackGitRun executes one git call in dir with the shared on-call
// timeout. It runs `git -C dir` (the -C convention shared with gitSyncRun)
// so the push-back core works uniformly from a repo directory argument.
// Returns (first meaningful output line, error).
//
// It is a DELEGATING ADAPTER over gitPeerRun (git_peer_fetch.go) — the ONE
// exec of the peer-fetch path (Rules 13/14: a second copy of the runner left
// standing is a defect, not a convenience). The on-call timeout stays HERE
// because it is this lane's named cap (gitPushbackFetchTimeout) and because
// it is the only difference from gitSyncRun's cap: 60s each, one value
// (gitPeerFetchTimeout).
func gitPushbackGitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitPushbackFetchTimeout)
	defer cancel()
	return gitPeerRun(cctx, dir, nil, args...)
}

// gitPushbackLoadBranches lists NON-integration local branches for a push
// (master/main excluded — asymmetry: the client never pushes master).
func gitPushbackLoadBranches(ctx context.Context, repoDir string) ([]string, error) {
	out, err := gitPushbackGitRun(ctx, repoDir, "for-each-ref",
		"--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "master" || line == "main" {
			continue
		}
		if len(names) >= gitSyncMaxBranches {
			break
		}
		names = append(names, line)
	}
	return names, nil
}

// gitPushbackFetchMaster pulls the server's integration-branch tip into a
// fixed local ref so every per-branch ancestor check is local (one ssh /
// filesystem round-trip per whole pass, not per branch). An unreachable
// server here means the subsequent pushes would fail too, so a fetch error
// is surfaced to the caller (which fails-soft).
//
// It DELEGATES to the ONE peer fetch (SyncPeerFetch, git_peer_fetch.go): the
// refspec, the runner and the "the mirror must resolve" check are shared with
// the close-out pre-merge gate and the worker's ground baseline, so the three
// cannot drift into three different answers about the peer's tip. Kept as a
// named adapter (rather than replaced at its ~2 call sites) so this lane's
// callers and their assertions are untouched by the consolidation.
func gitPushbackFetchMaster(ctx context.Context, repoDir, target string) error {
	_, err := SyncPeerFetch(ctx, repoDir, target)
	return err
}

// gitPushbackIsMerged reports whether `branch`'s tip is already an ancestor
// of the server master mirror (i.e. the server adopted + deleted it). A true
// return means the push WOULD resurrect a deleted ref → the plan skips it.
func gitPushbackIsMerged(ctx context.Context, repoDir, branch string) bool {
	// merge-base exit codes: 0 = ancestor (merged) ; 1 = not ancestor ;
	// other (no object) 128. After the master fetch the server tip object
	// is present, so only 0/1 are reachable — treat non-zero as not-merged.
	_, err := gitPushbackGitRun(ctx, repoDir, "merge-base", "--is-ancestor",
		branch, gitPushbackCheckRef)
	return err == nil
}

// gitPushbackPush tries ONE push and returns git's OWN diagnostic on failure
// ("" on success). The diagnostic is a return value rather than a log line,
// deliberately: gitPeerRun captures stderr into the error, and the caller is
// the only place that may decide whether that sentence is worth quoting to a
// human (Rule 15 — the failure is never swallowed HERE, only carried).
func gitPushbackPush(ctx context.Context, repoDir, target, name string) (string, bool) {
	// THE TEST FENCE, AT THE PUSH ITSELF. gitPushbackPlan asks it once per pass
	// and skips the whole flush, but the plan is NOT the only caller: the
	// delivery LOOP (task_delivery_loop.go) and memory_sync_task.go reach this
	// function directly, so a fence that lived only in the plan left those legs
	// uncovered — the same shape as the residue (a boundary that could push while
	// every OTHER boundary was fenced). Asking it here covers every caller by
	// construction, and the plan's own ask keeps its distinct job (skip the pass
	// and record WHY, rather than fail per branch).
	if why := finishCleanupTestSpaceRefusal(target); why != "" {
		return why, false
	}
	msg, err := gitPushbackGitRun(ctx, repoDir, "push", target, fmt.Sprintf("%s:%s", name, name))
	if err == nil {
		return "", true
	}
	return strings.TrimSpace(msg), false
}

// gitPushbackPushOne pushes ONE local branch to the target, under its OWN
// name, and reports whether it reached the peer. It keeps the two-value shape
// the plan's gate reads; gitPushbackPush carries the diagnostic for callers
// that report the refusal.
//
// The rename-retry is GONE, deliberately: a rejected same-name push used to be
// retried as `<name>-<HHMMSS>`, which does not resolve the divergence — it
// MINTS a new branch on the peer, the peer's merger parks the fork for exactly
// the same reason it would have parked the original, and each retry files
// another rework row. Measured 2026-09-17 (memory #1937, devdoc/
// rework-ff-convergence.md): one topic arrived at the peer as five branches on
// ONE tip, and the parked-branch ledger held 61 pending rework rows.
//
// A peer ref that has moved apart is a finding, not a name collision to dodge:
// the caller reports it (Rule 15) and the work is delivered by merging the
// peer's master into the branch and pushing again — history is never rewritten
// here (deviations: a branch already published is merged, not rebased).
func gitPushbackPushOne(ctx context.Context, repoDir, target, name string) bool {
	_, ok := gitPushbackPush(ctx, repoDir, target, name)
	return ok
}

// --- WHY a push was refused (Rule 15: name the cause, not the transport) ---

// pbUnresolvableRepoPhrase is git's own sentence for a push target that is not
// a repository. It is git's wording, not ours, and it is quoted from the real
// failure: `fatal: 'DISABLED-no-push' does not appear to be a git repository`
// (measured on HP 2026-10-08).

const pbUnresolvableRepoPhrase = "does not appear to be a git repository"

// gitPushbackRemotePushURL reads `remote.<name>.pushurl` from dir's config. It
// answers ("", false) when the key is UNSET, when the name is not a configured
// remote at all (a path or an URL target), or when git could not be asked —
// three different situations that all mean "no pushurl is riding this push".
// The key itself is git's, so it must be READ rather than parsed out of an
// ancestor remote's config block.
func gitPushbackRemotePushURL(ctx context.Context, dir, name string) (string, bool) {
	if name == "" || strings.Contains(name, sshPeerAliasTargetSeparator) {
		return "", false // a target with a path half is not a remote NAME
	}
	out, err := gitPushbackGitRun(ctx, dir, "config", "--get",
		fmt.Sprintf(pushbackRemotePushURLKeyFmt, name))
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// gitPushbackGitRemoteNames lists this repo's configured git remotes — the
// measurement behind the name-mismatch finding (pushbackAliasNotRemoteLineFmt).
// It answers the ROW of names, never a yes/no: the caller compares the alias
// against them and prints them all, because "the name you used is not here"
// is only actionable beside "here is what IS here" (the operator then knows
// which side of the topology to correct).
//
// A git failure (unreadable repo, remote-less tree) and an EMPTY listing both
// return nil — the renderer turns that into pushbackRemoteNameNone, so a
// measurement that found nothing never reads as a blank (Rule 15).
func gitPushbackGitRemoteNames(ctx context.Context, dir string) []string {
	out, err := gitPushbackGitRun(ctx, dir, pushbackRemoteNameVerb)
	if err != nil {
		return nil
	}
	names := []string{}
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// gitPushbackRemoteNameList renders gitPushbackGitRemoteNames for a sentence.
// The "none" spelling is a named constant, not a literal, because the finding
// that carries it is matched by tests and read by operators (Rule 1b).
func gitPushbackRemoteNameList(ctx context.Context, dir string) string {
	names := gitPushbackGitRemoteNames(ctx, dir)
	if len(names) == 0 {
		return pushbackRemoteNameNone
	}
	return strings.Join(names, pushbackRemoteNameListSep)
}

// gitPushbackTargetAliasNotRemote reports whether a push target's ALIAS half
// fails to resolve as a configured git remote in dir — the one measurably
// distinct cause of git's "{name} does not appear to be a git repository"
// that the pushurl arm cannot see.
//
// Only an "<alias>:<path>" target is asked: an alias-free target (a bare path
// or URL) names no remote to be missing, and gitPushbackRemotePushURL refuses
// the same shape for the same reason. A target whose alias the repo already
// configures answers false, so the branch's own pushurl diagnosis keeps the
// sentence to itself and the two arms never both fire.
func gitPushbackTargetAliasNotRemote(ctx context.Context, dir, target string) (string, bool) {
	alias, _, ok := strings.Cut(strings.TrimSpace(target), sshPeerAliasTargetSeparator)
	if !ok {
		return "", false
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", false
	}
	for _, name := range gitPushbackGitRemoteNames(ctx, dir) {
		if name == alias {
			return "", false
		}
	}
	return alias, true
}

// gitPushbackAliasNotRemoteLine renders the name-mismatch finding. It shares
// the ONE separator and the ONE comparison source with gitPushbackRefusal, so
// the two push-failure paths (this lane's flush and the finish-cleanup chain,
// which reads the same helper) cannot describe the same condition two ways
// (Rule 14).
//
// The declared-topology key is cited by its own constant
// (config.SyncPeersSettingsKey), never spelled here: it is the name the
// operator must go and look at.
func gitPushbackAliasNotRemoteLine(ctx context.Context, dir, target, alias string) string {
	return fmt.Sprintf(pushbackAliasNotRemoteLineFmt,
		target, alias, config.SyncPeersSettingsKey,
		gitPushbackRemoteNameList(ctx, dir), alias)
}

// gitPushbackTargetFailureDetail decorates ANY target-git failure with the
// measurable finding behind git's "{name} does not appear to be a git
// repository" sentence. It is the ONE entry point for that decoration, so the
// failure paths that can each carry it — the peer-master FETCH (which runs
// before any push and is therefore the first thing a mis-named peer breaks),
// the push itself in this lane, and the finish-cleanup chain's own fetch and
// push — describe the condition identically (Rule 14: one definition, many
// call sites; Rule 15: the cause is named wherever the failure surfaces).
//
// A detail that does not carry git's sentence is returned UNCHANGED: this
// function annotates a measured cause, it never rewrites a failure, and it
// never claims a name mismatch a probe did not find.
func gitPushbackTargetFailureDetail(ctx context.Context, dir, target, detail string) string {
	// THE NAME MISMATCH IS MEASURED, NEVER INFERRED FROM GIT'S WORDING, and
	// that is a correction paid for in the red proof: the same unresolvable
	// peer arrives with DIFFERENT sentences depending on the verb that failed
	// — `git fetch <alias>:<path>` over ssh says "ssh: Could not resolve
	// hostname … fatal: Could not read from remote repository", while a push
	// (or a local-path target) says git's "{name} does not appear to be a git
	// repository". Gating this arm on either phrase would have covered one
	// path and left the other silent, which is exactly the half-diagnosis this
	// change exists to end. The one thing both shapes share is the MEASUREMENT:
	// the alias half of the target is not among this repo's configured
	// remotes. So the probe is the gate, and the sentence is carried along
	// unchanged beneath the finding.
	//
	// THE RESOLUTION ADDS THE OTHER HALF (2026-10-12). Since the target is now
	// RESOLVED against this repo's remotes (peerRemoteTargetFor), a target that
	// is itself a remote NAME already carries the finding: git resolved the
	// remote and the failure is something else — the remote's URL, a pushurl, a
	// permission. Only an `<alias>:<path>` target — the FALLBACK arm, taken
	// precisely because this repo configures no remote of that name — can be
	// missing a name, and the two arms below are about exactly those two
	// worlds.
	if alias, missing := gitPushbackTargetAliasNotRemote(ctx, dir, target); missing {
		return detail + finishCleanupReasonSeparator +
			gitPushbackAliasNotRemoteLine(ctx, dir, target, alias)
	}
	// NOT a name mismatch — but the target may still BE a configured remote
	// whose own pushurl refuses the push. That arm keeps the sentence git
	// actually printed as its gate, because the pushurl key only rides the
	// "not a repository" failure (the pre-existing behaviour, untouched).
	if !strings.Contains(detail, pbUnresolvableRepoPhrase) {
		return detail
	}
	if url, ok := gitPushbackRemotePushURL(ctx, dir, target); ok {
		return detail + finishCleanupReasonSeparator + fmt.Sprintf(pushbackBlockedPushURLLineFmt,
			target, fmt.Sprintf(pushbackRemotePushURLKeyFmt, target), url, target)
	}
	return detail
}

// gitPushbackRefusal renders ONE refused branch: the lane's own verdict, git's
// own sentence, and — when the cause is measurable — the plain finding that
// turns the refusal into an action.
//
// The blocked-pushurl arm exists because the sentence "the peer rejected the
// push" reads identically for three different worlds, two of which this lane
// can measure and one of which it cannot. MEASURED 2026-10-08 on HP: the
// operator-facing report of a push refused by
// `remote.peer1.pushurl = DISABLED-no-push` was that sentence, with no mention
// of the key, its value or the command that clears it.
func gitPushbackRefusal(ctx context.Context, repoDir, target, name, gitMsg string) string {
	base := fmt.Sprintf(pushbackPushRefusedReasonFmt, name)
	if strings.TrimSpace(gitMsg) == "" {
		return base
	}
	refusal := base + finishCleanupReasonSeparator + fmt.Sprintf(pushbackPushRefusedDiagFmt, gitMsg)
	return gitPushbackTargetFailureDetail(ctx, repoDir, target, refusal)
}

// gitPushbackFastForwardsOntoMaster reports whether pushing `branch` could be
// a FAST-FORWARD of the peer's master mirror — i.e. whether the branch already
// CONTAINS the mirror tip. This is the peer merger's own landing predicate
// (repo_sweep.go: `git merge --ff-only <branch>`), asked in the same polarity:
// `merge-base --is-ancestor <mirror> <branch>` exits 0 exactly when that
// landing would succeed.
//
// The polarity is the whole point (memory #1900: the INVERTED form,
// `--is-ancestor <branch> <trunk>`, exits 0 precisely when the branch is NOT
// ff-able, so a caller who writes it backwards ships the loop). Direction
// here: the mirror is the FIRST argument, the branch the second.
//
// A git error reads as FALSE — the conservative side: refuse the push, never
// claim an ff-ability that was not proven (the same convention
// gitPushbackIsMerged uses in the opposite direction).
func gitPushbackFastForwardsOntoMaster(ctx context.Context, repoDir, branch string) bool {
	_, err := gitPushbackGitRun(ctx, repoDir, "merge-base", "--is-ancestor",
		gitPushbackCheckRef, branch)
	return err == nil
}

// gitPushbackSharesHistoryWithMaster reports whether `branch` and the peer
// master mirror have at least ONE commit in common — git's own ancestry
// question, asked with `git merge-base --all`: empty output + exit 1 means
// NO common ancestor (an unrelated history), a printed base means they share
// history.
//
// It is a THIRD question, prior to the two `merge-base --is-ancestor` probes
// above, and neither of them can answer it. "already integrated" and
// "fast-forwardable" are both relations that a NON-descendant branch can also
// satisfy, but an ORPHAN fails both in a way that reads as an ordinary
// non-delivery: without this probe the branch is reported as merely not
// fast-forwardable, whose remedy ("merge master into it") cannot be performed
// on a history that has no master to merge — and the peer's merger then hits
// `fatal: refusing to merge unrelated histories` (exit 128) by hand. The
// measured incident (2026-09-21, `feat/sync-conflict-completed`) is that loop,
// and it is why this question is asked separately, not folded into the ff
// gate it superficially resembles.
//
// Empty output is also an exit-1 non-answer for a ref that does not resolve
// (exit 128) or a wedged repo (timeout). Measured: `merge-base --all` answers
// 0 = a shared commit was printed, 1 = no common ancestor, 128 = "fatal: Not
// a valid object name". So the emptiness test is corroborated by the EXIT
// status: only git's documented exit 1 (task_staleness_guard.go's
// mergeVerifyGitExitNo — the same "git said no" constant
// mergeVerifyStaleIsAncestor reads) counts as a measured "no shared history".
// An error is NOT evidence of an unrelated history and must not refuse the
// push on it (Rule 15: an unmeasured premise is reported as unmeasured, never
// as a finding) — such a branch continues into the ff gate, which decides it
// on its own terms.
func gitPushbackSharesHistoryWithMaster(ctx context.Context, repoDir, branch string) bool {
	out, err := gitPushbackGitRun(ctx, repoDir, "merge-base", "--all",
		gitPushbackCheckRef, branch)
	if err != nil && !gitPushbackExitIsNo(err) {
		return true // unmeasured — never read as "unrelated"
	}
	return strings.TrimSpace(out) != ""
}

// gitPushbackExitIsNo reports whether a failed gitPushbackGitRun was git's own
// exit status 1 — git's documented "no" (mergeVerifyStaleIsAncestor's
// mergeVerifyGitExitNo convention: `rev-parse --verify --quiet` and
// `merge-base --is-ancestor` both answer 1 for a plain negative, while 128
// means the invocation itself failed). gitPushbackGitRun wraps the exit in an
// *exec.ExitError and prefixes the git argv, so the status is read off the
// wrapped error rather than the message text.
func gitPushbackExitIsNo(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == mergeVerifyGitExitNo
}

// --- the peer-ref question (NAME COLLISION) ---

// gitPushbackFetchPeerRef mirrors the peer's refs/heads/<branch> into this
// pass's throwaway gitPushcheck ref for that branch — the `+` refspec makes it
// a mirror, so the peer's tip is overwritten by whatever the peer advertises
// now (never a stale local understanding).
//
// The fetch is what makes the ancestry question answerable at all: `ls-remote`
// reports a sha, but an ancestry test needs the COMMIT. Fetching the one ref
// brings exactly those objects, and the gitPushcheck namespace keeps them out
// of the client's ordinary remote-tracking set.
func gitPushbackFetchPeerRef(ctx context.Context, repoDir, target, branch string) error {
	_, err := gitPushbackGitRun(ctx, repoDir, "fetch", "--force", "--no-tags", target,
		"+"+fmt.Sprintf(gitPushbackPeerRefHeadsFmt, branch)+":"+fmt.Sprintf(gitPushbackPeerRefMirrorFmt, branch))
	return err
}

// gitPushbackPeerRefTip reads the peer's refs/heads/<branch> with `ls-remote`.
// The probe is deliberately explicit — "does the peer own this name, and where
// is it" is the COLLISION question, and inferring it from a git push's own
// refusal would leave a name collision indistinguishable from a transport or
// permission failure (the defect this classification closes).
//
// Returns (short sha, measured). measured == false means the question was NOT
// answered: `ls-remote` failed (unreachable peer, timeout) or it printed no
// refs/heads/<branch> line at all. A ref that is genuinely ABSENT answers the
// question too ("the peer does not own this name") — and the caller
// distinguishes the two by the bool, never by an empty string, because a
// name-collision verdict may not be built on an unmeasured premise (Rule 15).
//
// Each branch costs its own ls-remote round-trip, unlike the single
// master fetch: that is deliberate. The named ref is learned from a remote's
// advertisement, which is exactly what a push negotiates against — using the
// pass's master mirror instead would answer about a ref that was already
// stale when the pass began.
func gitPushbackPeerRefTip(ctx context.Context, repoDir, target, branch string) (string, bool) {
	out, err := gitPushbackGitRun(ctx, repoDir, "ls-remote", target,
		fmt.Sprintf(gitPushbackPeerRefHeadsFmt, branch))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] != fmt.Sprintf(gitPushbackPeerRefHeadsFmt, branch) {
			continue
		}
		// Abbreviated: the sha is a REPORT value (tip provenance on the
		// collision line), so it is abbreviated at the single point where it is
		// read from git — never re-abbreviated per renderer.
		return shortSha(fields[0]), true
	}
	return "", true // measured: the peer does not own refs/heads/<branch>
}

// shortSha abbreviates a full sha for a report line (a resolved sha shorter
// than gitPushbackShortShaLen is returned unchanged).
func shortSha(sha string) string {
	if len(sha) <= gitPushbackShortShaLen {
		return sha
	}
	return sha[:gitPushbackShortShaLen]
}

// gitPushbackLocalSha resolves a local ref to its full sha ("" when it does
// not resolve — the caller has already listed it, so this is a repo error).
func gitPushbackLocalSha(ctx context.Context, repoDir, ref string) string {
	out, err := gitPushbackGitRun(ctx, repoDir, "rev-parse", "--verify", "--quiet",
		ref+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// gitPushbackPeerRefVerdict is the three-way answer to the peer-ref question.
type gitPushbackPeerRefVerdict int

const (
	// gitPushbackPeerRefAbsent: the peer does not own refs/heads/<branch> —
	// the push CREATES it (the ordinary case).
	gitPushbackPeerRefAbsent gitPushbackPeerRefVerdict = iota
	// gitPushbackPeerRefFastForward: the peer's ref is an ANCESTOR of ours —
	// the push updates it by fast-forward; the peer's commits (if any) are
	// preserved, so this is not a collision.
	gitPushbackPeerRefFastForward
	// gitPushbackPeerRefCollision: the peer's ref DIVERSED — neither ancestry
	// direction holds (a divergent same-name branch, or an unrelated history
	// pushed under this name). A push can only be refused; a human decides.
	gitPushbackPeerRefCollision
	// gitPushbackPeerRefUnmeasured: the probe could not answer (the peer was
	// unreachable, or the ref did not resolve locally). NEVER a refusal — the
	// push proceeds and git's own answer is reported (Rule 15).
	gitPushbackPeerRefUnmeasured
)

// String names the verdict for test failure output (gate constants read as
// integers otherwise).
func (v gitPushbackPeerRefVerdict) String() string {
	switch v {
	case gitPushbackPeerRefAbsent:
		return "absent"
	case gitPushbackPeerRefFastForward:
		return "fast-forward"
	case gitPushbackPeerRefCollision:
		return "collision"
	case gitPushbackPeerRefUnmeasured:
		return "unmeasured"
	}
	return "unknown"
}

// gitPushbackClassifyPeerRef answers the peer-ref question for ONE branch:
// given the sha the peer advertises for refs/heads/<branch> ("" = the peer does
// not own the name), it returns the verdict plus the abbreviated sha the
// measurement was made against (the mirror's tip — the value the peer actually
// advertised, which the report prints).
//
// The classification is ancestry, measured with `merge-base --is-ancestor` in
// BOTH directions (the polarity rules the two probes it shares with the
// master gates: argument order decides, and the ANCESTOR is the first
// argument):
//
//	peer ancestor of ours    → fast-forward (our ref contains the peer's, so
//	                           the push only moves the peer's ref forward)
//	ours ancestor of peer    → collision (the peer has already moved past us;
//	                           nothing of ours is missing there, but the push
//	                           still cannot be a fast-forward, and "someone
//	                           else owns this name" is the human decision)
//	neither                  → collision (diverged / unrelated histories)
//
// A git ERROR anywhere leaves the verdict UNMEASURED rather than guessed: both
// `merge-base` calls answer 0/1 for a plain negative, so any other status means
// the question itself failed (the ref did not arrive, the repo is wedged) — and
// an unmeasured premise is never evidence of a collision (Rule 15). The
// caller's mirror is what is measured, not the name: the answer is about the
// tip the peer advertises now.
func gitPushbackClassifyPeerRef(ctx context.Context, repoDir, target, branch, peerTip string) (gitPushbackPeerRefVerdict, string) {
	if strings.TrimSpace(peerTip) == "" {
		// The measured ABSENCE case, answered here rather than by the caller so
		// the three-way answer is produced in one place.
		return gitPushbackPeerRefAbsent, ""
	}
	mirror := fmt.Sprintf(gitPushbackPeerRefMirrorFmt, branch)
	if err := gitPushbackFetchPeerRef(ctx, repoDir, target, branch); err != nil {
		return gitPushbackPeerRefUnmeasured, peerTip
	}
	mirrorTip := strings.TrimSpace(gitPushbackLocalSha(ctx, repoDir, mirror))
	if mirrorTip == "" {
		return gitPushbackPeerRefUnmeasured, peerTip
	}
	if strings.TrimSpace(gitPushbackLocalSha(ctx, repoDir, branch)) == "" {
		return gitPushbackPeerRefUnmeasured, shortSha(mirrorTip)
	}
	peerIsAncestor, err := gitPushbackIsAncestorOf(ctx, repoDir, mirror, branch)
	if err != nil {
		return gitPushbackPeerRefUnmeasured, mirrorTip
	}
	if peerIsAncestor {
		return gitPushbackPeerRefFastForward, shortSha(mirrorTip)
	}
	oursIsAncestor, err := gitPushbackIsAncestorOf(ctx, repoDir, branch, mirror)
	if err != nil {
		return gitPushbackPeerRefUnmeasured, shortSha(mirrorTip)
	}
	if oursIsAncestor {
		return gitPushbackPeerRefCollision, shortSha(mirrorTip)
	}
	return gitPushbackPeerRefCollision, shortSha(mirrorTip)
}

// gitPushbackIsAncestorOf answers `merge-base --is-ancestor <ancestor>
// <descendant>` as a three-state measurement: (true, nil) = git's documented
// exit 0, (false, nil) = git's documented exit 1 ("no"), and a non-nil error
// for every other status — an ERROR, never a "no" (Rule 15). Sharing the
// exit-code discipline with gitPushbackExitIsNo keeps one definition of "git
// said no" in this lane.
func gitPushbackIsAncestorOf(ctx context.Context, repoDir, ancestor, descendant string) (bool, error) {
	_, err := gitPushbackGitRun(ctx, repoDir, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if gitPushbackExitIsNo(err) {
		return false, nil
	}
	return false, err
}

// gitPushbackPruneMirrors drops every ref of this lane's mirror namespace.
//
// The namespace is one PASS's scratch — the peer's master and one peer branch
// per candidate — so a pass prunes it on the way out (deferred, so the error
// paths prune too) and no mirror survives to answer a later pass's question
// with an older tip.
//
// It ENUMERATES rather than deleting the namespace as a prefix: git refuses a
// ref name carrying a trailing slash (`refusing to update ref with bad name`
// — measured), so there is no single-ref deletion of a namespace. The guard on
// the enumerated names is what keeps this honest: a name outside
// gitPushbackCheckRootRef is never touched, whatever for-each-ref printed. A
// failed enumeration or deletion is ignored on purpose (Rule 15's other half):
// this is best-effort hygiene, it can neither deliver nor refuse a branch, and
// the next pass's forced fetch overwrites any mirror left behind.
func gitPushbackPruneMirrors(ctx context.Context, repoDir string) {
	out, err := gitPushbackGitRun(ctx, repoDir, "for-each-ref",
		"--format=%(refname)", gitPushbackCheckRootRef)
	if err != nil {
		return
	}
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, gitPushbackCheckRootRef) {
			continue // never a name outside this lane's own namespace
		}
		// removal-audit-ok: this deletes the lane's OWN scratch namespace
		// (gitPushbackCheckRootRef = refs/remotes/<lane>/…) in the CLIENT repo —
		// never a refs/heads branch, never a delivery fact, and the next pass's
		// forced fetch overwrites anything left behind. It is classified as a
		// branch-delete site because it deletes a ref (the census matches the
		// plumbing verb, 2026-10-12); it carries no record of its own because a
		// mirror pruned every 10 minutes is not a removal an operator accounts
		// for, and recording it would bury the real deletions in this log.
		// (The marker is repeated on the call line: the census reads the escape
		// from the site line or the contiguous comment block above it, and this
		// statement's own trailing comment supersedes the block.)
		_, _ = gitPushbackGitRun(ctx, repoDir, "update-ref", "--no-deref", "-d", ref) //stylegate:ignore the error is deliberately discarded (best-effort prune that can neither deliver nor refuse a branch) — removal-audit-ok: this lane's own mirror namespace, never refs/heads; reason in full in the block above
	}
}

// gitPushbackPlan runs the push-back for one client repo against one server
// target. It loads the local non-integration branches, fetches the server
// master once (fail-soft: an unreachable check peers out the whole flush —
// pushing blind would resurrect), skips branches already in server master, and
// pushes the rest — but ONLY those that fast-forward onto that master AND on
// which this branch's own name is not DIVERGENT at the peer.
//
// The ff gate is this lane's delivery contract. The peer lands with
// `git merge --ff-only` and nothing else, so a branch behind the peer's master
// cannot be delivered by pushing it: it is refused by NAME with the remedy on
// the line, and the branch, its commits and the peer's refs are left exactly
// as they were.
//
// The three-way container/ancestry gates above decide a branch's fate from
// measurements only: a git answer this lane could not read is never turned
// into a refusal or a skip (Rule 15) — the branch falls through to the next
// gate and finally to git's own push answer, annotated as unmeasured.
func gitPushbackPlan(ctx context.Context, repoDir, target string) gitPushbackFlush {
	var flush gitPushbackFlush
	// THE TEST FENCE, asked where this lane's boundary actually is: the
	// production ENTRY does not receive its target (it renders it from the
	// declared peers, GitSyncClientPass -> GitSyncServerTarget), so a fence on
	// the caller's argument would be a fence on a value no production call
	// passes. Here the target is the rendered one, in every arm: `go test`
	// in-process and a re-exec'd child alike (the predicate's marker), and a
	// fixture pushing to its OWN bare repo under the test temp root is inside
	// test space and unaffected — the path property, never an alias list.
	//
	// Measured 2026-09-27: a `t <t@t>` fixture branch reached the operator's
	// refs/heads through a test path, by push (reflog: push). This is the lane
	// whose whole job is `git push`; the close-out chain has had the same
	// fence since 2026-09-22, and THAT fence is reused here (one predicate, one
	// sentence — no second implementation, Rule 14).
	if why := finishCleanupTestSpaceRefusal(target); why != "" {
		flush.Failed = append(flush.Failed, fmt.Sprintf(pushbackTestFenceMessage, why))
		return flush
	}
	branches, err := gitPushbackLoadBranches(ctx, repoDir)
	if err != nil {
		flush.Failed = append(flush.Failed, "list: "+err.Error())
		return flush
	}
	if err := gitPushbackFetchMaster(ctx, repoDir, target); err != nil {
		// Cannot prove merge-ness AND cannot reach the server to push:
		// blind push re-introduces resurrection. Refuse the flush (report).
		//
		// The decoration is owed HERE more than anywhere: this fetch is the
		// FIRST target-git call a pass makes, so a peer name this machine
		// cannot resolve breaks the pass before any push runs — the shape
		// measured on sager 2026-10-12, where the entire report was git's
		// bare sentence with no mention of the declared topology or of the
		// remotes that DO exist there.
		flush.Failed = append(flush.Failed,
			gitPushbackTargetFailureDetail(ctx, repoDir, target, "server-master fetch: "+err.Error()))
		return flush
	}
	defer gitPushbackPruneMirrors(ctx, repoDir) // this pass's own scratch namespace: <ns>/master and <ns>/peer/<branch>
	for _, name := range branches {
		if gitPushbackIsMerged(ctx, repoDir, name) {
			flush.Skipped = append(flush.Skipped, name)
			continue
		}
		if !gitPushbackSharesHistoryWithMaster(ctx, repoDir, name) {
			// ANCESTRY GATE, ahead of the ff gate: the branch shares no commit
			// with the peer's trunk, so the ordinary refusal line would name a
			// remedy that cannot be performed (see the fmt's doc). Reported by
			// name; never pushed, never deleted, never forked.
			flush.Failed = append(flush.Failed, fmt.Sprintf(pushbackUnrelatedHistoryReasonFmt,
				name, masterBranchName, masterBranchName, name, masterBranchName))
			continue
		}
		// ALIGNMENT IS A FACT HERE, NOT A GATE. The branch does not contain
		// the peer's master, so the peer's own landing operation
		// (`git merge --ff-only <branch>`) cannot take it as delivered — but
		// WHETHER TO HAND THE WORK OVER IS NOT THIS LANE'S CALL, and it used
		// to be: a "no" here REFUSED the push and printed the remedy "merge
		// master into it". Measured consequence (epic 01a12a54…): a
		// deliverable cut from a base the peer has since moved past stays
		// LOCAL and is reported as a failure of THIS lane, which is the wrong
		// owner of that decision — the peer reads the branch, its merger
		// applies its own rule, and a branch it cannot land yet is a fact it
		// is entitled to see rather than one we hide. The fact is therefore
		// stated on the Pushed entry and in the machine-readable halves
		// (FastForwardable / NotFastForwardable); the push proceeds.
		//
		// What this does NOT relax: the ancestry gate above still refuses
		// (a history with no common commit can never be landed by anyone) and
		// the peer-ref gate below still refuses a NAME COLLISION (that one
		// needs a human decision). Those two are the lane's refusals; a
		// merely unaligned branch is not.
		ffable := gitPushbackFastForwardsOntoMaster(ctx, repoDir, name)
		notFfPeerTip := ""
		if !ffable {
			// The peer tip the branch is behind ("" when it could not be
			// read — the note then states the fact without a tip rather than
			// printing an empty one).
			notFfPeerTip = shortSha(gitPushbackLocalSha(ctx, repoDir, gitPushbackCheckRef))
		}

		// PEER-REF GATE: the peer may still own refs/heads/<name> at a
		// divergent tip (two sides used one name). Ask it explicitly — the
		// push's own refusal cannot tell a collision from a transport failure,
		// which is the defect this gate closes. It is asked on EVERY pushed
		// branch, aligned or not (it answers a question about the peer's ref
		// namespace, not about our alignment).
		unmeasured := ""
		verdict, peerTip := gitPushbackPeerRefUnmeasured, ""
		advertised, measured := gitPushbackPeerRefTip(ctx, repoDir, target, name)
		if !measured {
			unmeasured = "the peer's " + fmt.Sprintf(gitPushbackPeerRefHeadsFmt, name) +
				" could not be read (ls-remote failed — unreachable peer?)"
		} else {
			verdict, peerTip = gitPushbackClassifyPeerRef(ctx, repoDir, target, name, advertised)
			switch verdict {
			case gitPushbackPeerRefCollision:
				// REFUSED BY NAME, both tips and the remedy on the line. The
				// only outcome in this lane that needs a human decision, and
				// the only one it may NOT resolve itself (no force, no fork).
				flush.Failed = append(flush.Failed, fmt.Sprintf(pushbackPeerRefCollisionReasonFmt,
					name, name, peerTip, shortSha(gitPushbackLocalSha(ctx, repoDir, name))))
				continue
			case gitPushbackPeerRefFastForward:
				// Not a collision: our ref contains the peer's, so the push can
				// only move the peer's ref forward along our own history.
			case gitPushbackPeerRefUnmeasured:
				unmeasured = "the ancestry of the peer's " + fmt.Sprintf(gitPushbackPeerRefHeadsFmt, name) +
					" could not be measured (fetch or merge-base failed)"
			case gitPushbackPeerRefAbsent:
				// The ordinary case: the peer does not own this name, so the
				// push CREATES it. Nothing to report beyond the branch name.
			}
		}
		if _, ok := gitPushbackPush(ctx, repoDir, target, name); !ok {
			// ONE MORE ROUND, NOT ONE MORE ANSWER (epic 01a17033: the delivery
			// loop). The push was refused — and the lane already knows the
			// four different causes that sentence can hide. Round 1 of the
			// loop re-fetches the peer's master and classifies: a stale base
			// is ALIGNED and pushed again (the ordinary two-host case, which
			// used to be reported as a failure), a name collision / an orphan
			// history / a content conflict stops with its own sentence, and an
			// unreachable peer says so instead of "push failed".
			//
			// The loop is BOUNDED (pushbackDeliveryRounds, Rule 1) and every
			// round re-fetches; it is not a retry of this same call. Whatever
			// it measured is what this branch reports — verbatim (Rule 15).
			delivery := deliverBranchToPeer(ctx, repoDir, target, name, nil)
			if delivery.Delivered {
				flush.Pushed = append(flush.Pushed, name)
				flush.FastForwarded = append(flush.FastForwarded, name)
				flush.FastForwardable = append(flush.FastForwardable, name)
				continue
			}
			if last := delivery.LastAttempt(); last != nil && last.Aligned {
				// Aligned but not delivered: the branch now CONTAINS the peer's
				// master, which is a fact about the peer's `merge --ff-only`
				// even though the push still did not land.
				flush.NotFastForwardable = append(flush.NotFastForwardable, name)
			}
			flush.Failed = append(flush.Failed, delivery.Reason)
			continue
		}
		entry := pushbackPushedEntry(ctx, repoDir, name, peerTip, unmeasured)
		if ffable {
			flush.FastForwardable = append(flush.FastForwardable, name)
		} else {
			// The machine-readable half of the fact, and the prose for a human
			// reader: the branch crossed, and it does not contain the peer's
			// master.
			flush.NotFastForwardable = append(flush.NotFastForwardable, name)
			entry = pushbackFactNote(entry, pushbackNotFfableFactNoteFmt, notFfPeerTip)
		}
		flush.Pushed = append(flush.Pushed, entry)
		if peerTip != "" && unmeasured == "" {
			flush.FastForwarded = append(flush.FastForwarded, name)
		}
	}
	// THE SYMMETRIC HALF, rendered once the whole flush is known. An aligned
	// branch's entry stays the bare branch name when EVERY pushed branch in
	// the flush is aligned — the ordinary case, and the shape existing readers
	// and tests rely on — and gains its positive fact line as soon as one
	// pushed sibling is unaligned. That is what makes one report answer, for
	// every branch it delivered, whether the peer's `merge --ff-only` can land
	// it as delivered, without writing a note over the ordinary flush.
	if len(flush.NotFastForwardable) > 0 {
		tip := shortSha(gitPushbackLocalSha(ctx, repoDir, gitPushbackCheckRef))
		for i, entry := range flush.Pushed {
			name := pushbackEntryBranch(entry)
			if name == "" || pushbackNameIn(flush.NotFastForwardable, name) {
				continue
			}
			flush.Pushed[i] = pushbackFactNote(entry, pushbackFfableFactNoteFmt, tip)
		}
	}
	return flush
}

// pushbackFactNote appends one alignment fact to a Pushed entry. A tip that
// could not be read adds NO note: the fact's value is the peer tip it is
// measured against, and an unmeasured premise stated as a finding is exactly
// what Rule 15 forbids (the branch is still pushed and still listed in its
// machine-readable half).
func pushbackFactNote(entry, noteFmt, tip string) string {
	if tip == "" {
		return entry
	}
	return fmt.Sprintf("%s (%s)", entry, fmt.Sprintf(noteFmt, tip))
}

// pushbackEntryBranch reads the branch name off a Pushed entry (the name is
// the entry's head, before any appended note). A git ref name cannot contain a
// space, so " (" is an unambiguous separator.
func pushbackEntryBranch(entry string) string {
	if i := strings.Index(entry, " ("); i >= 0 {
		return entry[:i]
	}
	return entry
}

// pushbackNameIn reports whether name is in list.
func pushbackNameIn(list []string, name string) bool {
	for _, s := range list {
		if s == name {
			return true
		}
	}
	return false
}

// pushbackPushedEntry renders one Pushed entry: the branch name the peer
// received under its own name, with the peer-ref finding appended when there
// is one — the fast-forward note (the peer's own ref of that name moved
// forward) or the unmeasured note (the peer-ref question was never answered).
// A plain branch name is the ordinary case: the peer did not own the name, so
// the push created it.
func pushbackPushedEntry(ctx context.Context, repoDir, name, peerTip, unmeasured string) string {
	if unmeasured != "" {
		return fmt.Sprintf("%s (%s)", name, fmt.Sprintf(pushbackPeerRefUnmeasuredNoteFmt, unmeasured))
	}
	if peerTip == "" {
		return name
	}
	return fmt.Sprintf(pushbackPeerRefFastForwardNoteFmt, name, peerTip,
		shortSha(gitPushbackLocalSha(ctx, repoDir, name)))
}
