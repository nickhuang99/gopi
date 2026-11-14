package tools

// Git sync channel (epic 2026-09-01: the serve-tick git pass — M4.13
// t-1788093118 delivered only the delta protocol + harvest; the git
// half of the serve tick had NO owner). Two asymmetric passes, one
// shared engine, both fail-soft per step:
//
//	HP (client) pass — syncGitClientPass:
//	  stale sweep → push local feat/* back (bug038 idempotent push,
//	  the harvest gitPushbackDefault logic reused) → fast-forward the
//	  local INTEGRATION BRANCH to the peer's tip (--ff-only; a diverged
//	  branch is skipped, never merged here) → db sync → notify.
//	peerbox (server) pass — syncGitServerPass:
//	  stale sweep → fetch the client's feat/* → clean branches merge
//	  + branch delete (the adoption chain's ③a path reused; conflicts
//	  stay the adoption chain's ③b dispatch → a Bond session) →
//	  db sync → notify.
//
// Trigger: the serve tick (`runTaskSyncTick`) and the manual `gopi task push`
// CLI (M4.18, the same atom). There is NO operator knob for the cadence: the
// two sync-interval variables this comment used to name were advertised by the
// catalog and `gopi --help-env` while nothing read them, and they were removed
// on 2026-11-05 (CONFIG.md carries the removal note; the anti-re-advertisement
// gate is internal/config's). Order: git first, db second
// (deliverable{branch} must exist before rows migrate).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// gitSync constants — Rule 1: every literal in one place.
const (
	// gitSyncTimeout caps every single git invocation (a wedged repo must
	// not stall the serve pass; same contract as adoptionGitTimeout).
	gitSyncTimeout = 60 * time.Second

	// gitSyncMaxBranches caps the per-pass push/merge list (the runaway
	// ledger cannot turn one tick into a fork bomb —
	// harvestGitMaxBranches contract).
	gitSyncMaxBranches = 20

	// NOTE (2026-10-25): the peer's repo path is NOT a per-leg literal here any
	// more. This file used to carry `gitSyncPeerRepo = "~/work/GoPi"`, the
	// harvest lane carried `harvestPeerRepo` with the same value and the
	// close-out chain carried `finishCleanupPeerRepo` — three definitions of one
	// shared value (Rule 14) that had all drifted away from the host's DECLARED
	// delivery point. Every leg now asks peer_repo_path.go, which parses the
	// declaration and announces its one named fallback.

	// gitSyncStepPullBranchName is step ③'s report name. The VALUE is the one
	// the pass has always printed (a human reads it in the serve log and
	// existing analyses key on it), while the STEP's contract is what changed
	// on 2026-10-12: it now fast-forwards the local INTEGRATION BRANCH, not
	// "whatever branch the calling worktree happens to have checked out" —
	// see its call site.
	gitSyncStepPullBranchName = "pull-master"

	// gitSyncMergeVerb / gitSyncMergeFFOnlyFlag spell the operation step ③
	// performs inside the branch's HOLDER worktree: a fast-forward merge of the
	// already-fetched peer tip, which moves the ref, that worktree's index and
	// its working tree together. The asymmetry holds — the client still NEVER
	// creates a merge commit.
	gitSyncMergeVerb       = "merge"
	gitSyncMergeFFOnlyFlag = "--ff-only"

	// gitSyncPullSkippedPrefix / gitSyncPullSkippedClose bracket the reason a
	// step that could not do its job reports, and
	// gitSyncPullUpToDateDetail / gitSyncFfDetailPrefix are the two success
	// details of the ref-delta route. Named so the report's wording has ONE
	// home rather than being spelled at the call site (Rule 1b).
	gitSyncPullSkippedPrefix = "skipped ("
	gitSyncPullSkippedClose  = ")"
	// gitSyncPullUpToDateDetail is the step's no-op sentence. The step renders
	// its OWN words rather than echoing git's human prose, which was localized
	// (the previous assertion had to accept a Chinese spelling as well as an
	// English one): one spelling, no locale, no dependence on git's phrasing.
	gitSyncPullUpToDateDetail = "up to date"
	gitSyncFfDetailPrefix     = "fast-forwarded to "

	// gitSyncHolderUnreadableFmt is the refusal step ③ reports when the survey
	// of the repository's worktrees cannot be read: whether a bare ref write
	// would desync somebody's checkout is then UNKNOWN, and an unjudged write
	// into a namespace every worktree shares is refused rather than permitted
	// (Rule 15 — the same predicate the shared-integration-ref guard applies to
	// the shell's own commands).
	gitSyncHolderUnreadableFmt = "cannot determine which worktree holds %s: %v"

	// gitSyncHeldByOtherFmt / gitSyncRefUnresolvedFmt are step ③'s other two
	// refusals: the branch is held by a worktree this pass must not write
	// through, and the ref did not resolve after a fetch git called successful.
	gitSyncHeldByOtherFmt   = "%s is held by the worktree %s, which is neither this checkout nor a throwaway ground — the merge belongs to whoever holds the branch; nothing was written"
	gitSyncRefUnresolvedFmt = "%s did not resolve after the fetch reported success"

	// gitSyncAbbrevRefFlag / gitSyncHeadRef / gitSyncDetachedHead are the HEAD
	// survey step ③ resolves the holder with: the same question, the same flag
	// and the same two sentinels the shared-integration-ref guard and the
	// close-out chain use (gitProbeOutput(dir, "rev-parse", "--abbrev-ref",
	// "HEAD") vs finishCleanupDetachedHead), referenced rather than re-spelled
	// (Rules 13/14).
	gitSyncAbbrevRefFlag  = finishCleanupPreMergeAbbrevRefFlag
	gitSyncHeadRef        = finishCleanupPreMergeHeadArg
	gitSyncPullFFOnlyFlag = "--ff-only"

	// gitSyncFetchNoTagsFlag / gitSyncFetchPruneVerb are the two flags of the
	// client arm's fetch-prune step (see its call site): --prune is the
	// deletion contract's other half, --no-tags keeps a peer's tag set out of
	// this repo (refs/heads only is the contract between the nodes).
	gitSyncFetchNoTagsFlag = "--no-tags"
	gitSyncFetchPruneVerb  = "--prune"

	// gitSyncPruneDetailSuffix is the report suffix the client arm renders on
	// its fetch-prune step, and gitSyncPruneUnknownDetail is what it renders
	// when the ref state could not be READ — the operator reads how many peer
	// refs this pass reaped rather than having to diff `git branch -a` by
	// hand, and an unreadable survey must never print a made-up 0 (Rule 15a).
	gitSyncPruneDetailSuffix  = " pruned"
	gitSyncPruneUnknownDetail = "unmeasurable"

	// --- the STALENESS ALARM (task 01a13440, the cadence/face half) ---------
	//
	// The step reports its own outcome; these three constants make the ONE
	// fact the step cannot read — "how far behind is this host, really?" —
	// a line of its own, because a per-step verdict is not a LEADING
	// INDICATOR. Measured 2026-10-13 on host-a: `refs/heads/master` was
	// 141 commits behind the peer and a purely-ancestor of it, while every
	// step of every pass had reported success — the freeze was invisible in
	// the log for exactly as long as it lasted.
	//
	// The NUMBER is counted between the two refs themselves (the peer is only
	// ever contacted by the fetch legs, never by this measurement), so the
	// alarm can be printed on a pass that did not reach the network.

	// gitSyncLagRef is the ref the lag is asked ABOUT: the peer's integration
	// branch as this repository last mirrored it (a reserved non-branch ref no
	// worktree can hold, so reading it can never disturb a checkout). It is
	// deliberately NOT the peer's remote-tracking namespace: the mirror is the
	// ref the delivery lane's ancestry gate already decides on (gitPushbackIsMerged),
	// so the alarm and the gate cannot disagree about what "the peer's master"
	// means. Its NAME comes from the ONE constant that defines it (Rule 1/13).
	gitSyncLagRef = gitPushbackCheckRef

	// gitSyncLagUnknown is what the alarm prints when the count could not be
	// taken (no mirror ref yet, an unreadable repo, a git that answered
	// nothing). A made-up 0 would read as "aligned", i.e. as the all-clear
	// (Rule 15a: the required semantic — "behind by N" — is not the zero
	// value).
	gitSyncLagUnknown = "unmeasurable"

	// gitSyncLagAligned / gitSyncLagBehindFormat are the alarm's two texts, and
	// the split is the point: a host AT the peer's tip says so in one word,
	// while a host with the integration ref behind names the NUMBER, the ref it
	// was measured against, and the ONE command that closes the gap. The
	// closing advice is the pass this very alarm rides (`gopi task push`) —
	// never a cron line, never a new process (Rule 35).
	//
	// gitSyncLagBehindFormat is TRUE ONLY FOR A PURELY-BEHIND PAIR (see
	// gitSyncLagDivergedFormat): it promises a fast-forward, so it is printed
	// exactly where a fast-forward exists. Its closing half is
	// gitSyncLagPassPrescription — a constant of its own so the test that
	// asserts the promise is ABSENT from the diverged sentence names the same
	// bytes this sentence carries (Rule 13: one definition, referenced).
	gitSyncLagAligned      = "at the peer's tip"
	gitSyncLagBehindFormat = "integration ref %s is %s commit(s) behind %s — " + gitSyncLagPassPrescription

	// gitSyncLagPassPrescription is the ONE command that actually closes a
	// purely-behind gap: this pass, whose client arm fast-forwards the
	// integration ref onto the peer's mirrored tip. It is the prescription the
	// alarm prints when — AND ONLY WHEN — that fast-forward exists; the
	// diverged sentence must not carry it (the pass refuses there by design).
	gitSyncLagPassPrescription = "`gopi task push` (this pass) advances it"

	// gitSyncLagDivergedFormat is the alarm's THIRD text, and the reason this
	// classification exists at all. Measured 2026-10-13 (task 01a13440's own
	// run, filed as this task): on a DIVERGED pair — the local integration ref
	// carries commits the peer's mirrored tip does not have — the two texts
	// above are BOTH wrong. gitSyncLagBehindFormat prescribes `gopi task push`
	// (this pass), which is exactly the pass whose client arm REFUSES to
	// rebase/merge by design (gitSyncFFIntegrationBranch: `merge --ff-only` ⇒
	// "Not possible to fast-forward"), so the alarm sent the operator back to a
	// command that can never succeed while the ref stayed frozen and
	// `pull:skipped` repeated forever; and dropping to silence would hide a
	// freeze that is real. So the third text states the MEASURED shape (both
	// counts, named) and a next step that EXISTS.
	//
	// It names, in this order: the local ref, the behind count, the mirror it
	// was measured against, the LOCAL-ONLY count, the local ref again (the
	// checkout whose tips must be reconciled), and the executable next step —
	// git's OWN merge verb against the mirrored peer tip
	// (gitSyncLagManualMergeCommand), the operation the client arm declines to
	// run on the operator's behalf. No new gopi verb, no new tool, no new
	// process (Rule 35): the remedy is a command the operator already has.
	//
	// Args: localRef, behind, mirrorRef, local-only count, localRef.
	gitSyncLagDivergedFormat = "integration ref %s is %s commit(s) behind %s and carries %s commit(s) the peer does not have — the two refs have DIVERGED, so no fast-forward exists and this pass cannot advance it: reconcile the tips in the checkout that holds %s with `" + gitSyncLagManualMergeCommand + "`, after which this pass reports the ref aligned"

	// gitSyncLagDifferentRepoFormat is the alarm's FOURTH text, and the one that
	// had to exist before any of the other three could be trusted (measured
	// 2026-10-25). The three texts above all presuppose that the two refs being
	// compared belong to ONE repository. When they do not — the measured case: a
	// peer path that named a different, unrelated repository, so the mirror held
	// 12025 commits with disjoint roots against a local 359 — the divergence
	// sentence states a true count and prescribes `git merge <mirror>`, i.e. the
	// graft that Project Rule 38 forbids a prescription to make things worse
	// with. The measurement that separates the two shapes is git's own
	// `merge-base --all <mirror> <localRef>`: empty output + exit 1 = no shared
	// commit at all.
	//
	// So this text names NO git command. There is no git command to name: the
	// defect is a mis-declared path, and the remedy is a declaration. Args: the
	// local ref, the mirror ref (peerRepoIdentityUnrelatedDetail carries the
	// whole statement and its measured basis).
	gitSyncLagDifferentRepoFormat = "integration ref %s: %s"

	// gitSyncLagManualMergeCommand is the executable next step the diverged
	// sentence names: git's own merge verb, aimed at the mirrored peer tip —
	// the same ref the count above is measured against and the same ref
	// gitSyncFFIntegrationBranch merges when a holder exists. It is a CONSTANT
	// so the sentence and any test that asserts it cannot drift (Rule 1b), and
	// so the remedy stays an existing verb rather than a new one (Rule 35).
	gitSyncLagManualMergeCommand = "git merge " + gitSyncLagRef

	// gitSyncLagUnknownFmt is the ONE sentence for every unmeasurable answer
	// (no mirror ref yet, an unreadable repo, a git that answered nothing, a
	// behind count whose divergence could not be classified). It is a constant
	// because the three arms that produce it must be indistinguishable from one
	// another to a reader: an unanswered question is not a defect (Rule 15a),
	// and a second spelling would make the same non-answer read as two facts.
	// Args: the local ref.
	gitSyncLagUnknownFmt = "integration ref %s: " + gitSyncLagUnknown
	// gitSyncLagAlignedValue is the WIRE value of the aligned case ("0"), as
	// opposed to gitSyncLagAligned, which is the human phrase. They are two
	// constants on purpose (Rule 1b): one is compared against and one is
	// printed, so a reworded sentence can never silently change the alarm's
	// verdict or the summary line's decision to omit the field.
	gitSyncLagAlignedValue = "0"

	// gitSyncLagSummaryField is the field name the pass's ONE summary line
	// carries the lag under. A constant because the alarm's test asserts the
	// line is ABSENT for an aligned host (Rule 1b): a reworded field would
	// otherwise turn that assertion into a check that reads a different line.
	gitSyncLagSummaryField = "integration_lag"

	// gitSyncLagAheadSummaryField is the summary line's SECOND number — the
	// LOCAL-ONLY count, which is what makes the line say which of the two
	// shapes the host is in. Without it `integration_lag=3` is the same log
	// line for a purely-behind host (one pass away from aligned) and for a
	// diverged one (which that pass can never close) — the very ambiguity the
	// diverged sentence exists to end, and the number an operator needs to
	// reproduce the measurement by hand. It is carried only when it is
	// non-zero, so an aligned checkout and a purely-behind one print exactly
	// the line they printed before (gitSyncLagAlignedValue's rule, one field
	// over).
	gitSyncLagAheadSummaryField = "integration_ahead"

	// gitSyncLagAlarmFormat is the WHOLE warning line. It carries the word
	// WARNING because it prints to stderr on a defect that is otherwise
	// indistinguishable from health: an operator greps for one word, and
	// nothing else in this pass's output carries it. The sentence half comes
	// from the report's own step (gitSyncStaleAlarm), never from a second
	// rendering of the same numbers (Rule 14).
	gitSyncLagAlarmFormat = "[%s] [git-%s] WARNING: stale integration ref — %s\n"

	// gitSyncStepStaleWarning is the report's step name for the lag, so the
	// number and the failure ride the SAME passthrough every other step uses
	// (the CLI faces print one stderr line per failed step) — the alarm needs
	// no new channel (Rule 35). Its Detail IS the human sentence, so there is
	// exactly one place that wording lives.
	gitSyncStepStaleWarning = "integration-ref-stale"
)

// gitSyncPruneDetail renders the fetch-prune step's detail from the ref count
// SURVEYED around the fetch — never from git's own output.
//
// MEASURED 2026-09-26 on HP: `git fetch --prune` writes its "[deleted]" lines
// to STDERR, while gitSyncRun captures stdout. Counting the marker therefore
// reported "0 pruned" on the very pass that reaped 182 refs — a false reading
// in the operator's log for the one number this step exists to report. The
// count is derived from the repository's own ref state (the two surveys the
// caller takes around the fetch), which cannot disagree with what happened.
func gitSyncPruneDetail(before, after int) string {
	if before < 0 || after < 0 {
		return gitSyncPruneUnknownDetail
	}
	removed := before - after
	if removed < 0 {
		removed = 0
	}
	return strconv.Itoa(removed) + gitSyncPruneDetailSuffix
}

// gitSyncPruneSource picks WHICH spelling of the peer the prune fetches.
//
// MEASURED 2026-09-26 on HP, and it is the whole reason the ghosts survived:
// the sync target is spelled "<alias>:<path>" (sync_peers declares a git
// REMOTE's name, e.g. "peerbox", and the path is the resolver's
// (peer_repo_path.go)). Fetching that
// full target is an scp-style URL, and a URL fetch updates NO remote-tracking
// ref — it writes FETCH_HEAD and returns. So `git fetch <alias>:<path>` cannot
// prune refs/remotes/<alias>/*: the pass reported "0 pruned" while the repo
// held 184 ghost refs, and the same repo pruned 182 of them the moment the
// fetch named the remote (`git fetch --prune --no-tags peerbox`) instead of its
// URL. The refspec lives on the REMOTE, so the remote's name is what the
// prune must be given; the URL form stays as the fallback for a target that
// names no configured remote (a bare path, a one-off host).
func gitSyncPruneSource(repo, server string) string {
	alias, _, ok := strings.Cut(server, sshPeerAliasTargetSeparator)
	if !ok {
		return server
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return server
	}
	if _, err := gitSyncRun(repo, "remote", "get-url", alias); err != nil {
		return server // not a configured remote: the URL form is all there is
	}
	return alias
}

// gitSyncPeerRemoteNamespace names this repo's remote-tracking namespace for
// the peer at the other end of `server` ("<alias>:<path>"). The alias is the
// half that decides a remote's refspec, so the namespace is derived from it
// and never from the peer's configured name on disk (the two agree today, but
// only the alias is the contract the caller passed in). An alias-free target
// (a bare path) has no remote-tracking namespace → empty, and the count is
// reported as unmeasurable rather than as a fabricated 0.
func gitSyncPeerRemoteNamespace(server string) string {
	alias, _, ok := strings.Cut(server, sshPeerAliasTargetSeparator)
	if !ok || strings.TrimSpace(alias) == "" {
		return ""
	}
	return "refs/remotes/" + strings.TrimSpace(alias) + "/"
}

// gitSyncPeerRefCount counts this repo's mirror of the peer's refs (the
// remote-tracking namespace of the named peer). An unreadable count returns
// -1, which gitSyncPruneDetail treats as "not measurable" rather than as 0 —
// a 0 that meant "unknown" would report a nonexistent prune as done (Rule
// 15a: the required semantic is not the zero value).
func gitSyncPeerRefCount(repo, server string) int {
	ns := gitSyncPeerRemoteNamespace(server)
	if ns == "" {
		return -1
	}
	out, err := gitSyncRun(repo, "for-each-ref", "--format=%(refname)", ns)
	if err != nil {
		return -1
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// GitPassStep is one fail-soft step's outcome (the finish-cleanup
// FinishCleanupStep shape: the caller decides what a failure means).
type GitPassStep struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// GitPassReport is one pass's full outcome — every step that ran, its
// verdict, and the branch/ref counts. A pass NEVER aborts: each step
// degrades independently (denyCurrentBranch refusing a master push is a
// NORMAL skip, not an error — the server's master is checked out).
type GitPassReport struct {
	Role           string   `json:"role"`                  // client | server
	Skipped        bool     `json:"skipped,omitempty"`     // gate refused the whole pass
	SkipReason     string   `json:"skip_reason,omitempty"` // why (no peers / dirty tree / disabled)
	BranchesPushed []string `json:"branches_pushed,omitempty"`
	PushFailed     []string `json:"push_failed,omitempty"`
	// BranchesFastForwardable / BranchesNotFastForwardable split BranchesPushed
	// by the ONE alignment fact this pass measures: whether the branch's tip
	// contains the peer's master, i.e. whether the peer's own landing operation
	// (`git merge --ff-only <branch>`) can take it as delivered. They are the
	// machine-readable half of a fact, never a verdict — the pass pushes both
	// kinds (see gitPushbackPlan's alignment note) and the peer's merger decides.
	// A pushed branch appears in exactly one of the two.
	BranchesFastForwardable    []string `json:"branches_fast_forwardable,omitempty"`
	BranchesNotFastForwardable []string `json:"branches_not_fast_forwardable,omitempty"`
	PullFF                     string   `json:"pull_ff,omitempty"`    // client: master fast-forwarded to X / up-to-date
	Fetched                    []string `json:"fetched,omitempty"`    // server: remote refs updated
	Adopted                    []string `json:"adopted,omitempty"`    // server: clean branches merged + deleted
	Dispatched                 []string `json:"dispatched,omitempty"` // server: conflict tasks dispatched (Bond)
	Duped                      int      `json:"duped,omitempty"`      // server: conflict task already live
	// IntegrationLag is how far behind the peer's tip this host's integration
	// ref was left by this pass — a STRING on purpose, because "unmeasurable"
	// is a distinct answer from "0" and the zero value of an int spells them
	// the same (Rule 15a). "" = the pass did not measure it (a skipped pass).
	// The step of the same name carries the human sentence; the CLI faces
	// render this value as the staleness alarm.
	IntegrationLag string `json:"integration_lag,omitempty"`
	// IntegrationAhead is the OTHER half of the same measurement: how many
	// commits the LOCAL integration ref carries that the peer's mirrored tip
	// does not. It is a string for IntegrationLag's reason (an unmeasurable
	// count must not spell as a 0), and it is the number that decides which of
	// the alarm's three sentences is TRUE: 0 with a lag ⇒ purely behind (the
	// pass's fast-forward closes it), non-zero ⇒ diverged (no fast-forward
	// exists, so the pass's prescription would be a no-op). It rides the pass's
	// summary line, omitted at its zero, so a purely-behind host prints exactly
	// the line it printed before this field existed.
	IntegrationAhead string        `json:"integration_ahead,omitempty"`
	Steps            []GitPassStep `json:"steps,omitempty"`
}

// gitPassSteps renders the report as one human log line (the serve face).
func (r *GitPassReport) Line() string {
	if r == nil {
		return "(nil pass)"
	}
	if r.Skipped {
		return fmt.Sprintf("skipped: %s", r.SkipReason)
	}
	var parts []string
	if len(r.BranchesPushed) > 0 {
		parts = append(parts, fmt.Sprintf("pushed=[%s]", strings.Join(r.BranchesPushed, " ")))
	}
	if len(r.PushFailed) > 0 {
		parts = append(parts, fmt.Sprintf("push_failed=[%s]", strings.Join(r.PushFailed, " | ")))
	}
	if len(r.BranchesFastForwardable) > 0 {
		parts = append(parts, fmt.Sprintf("ffable=[%s]", strings.Join(r.BranchesFastForwardable, " ")))
	}
	if len(r.BranchesNotFastForwardable) > 0 {
		// A FACT, stated on the pass line: these branches ARE delivered, and
		// the peer's `merge --ff-only` cannot take them until they are aligned
		// (the pass still pushes them — see gitPushbackPlan).
		parts = append(parts, fmt.Sprintf("not_ffable=[%s]", strings.Join(r.BranchesNotFastForwardable, " ")))
	}
	if r.PullFF != "" {
		parts = append(parts, "pull:"+r.PullFF)
	}
	if len(r.Fetched) > 0 {
		parts = append(parts, fmt.Sprintf("fetched=[%s]", strings.Join(r.Fetched, " ")))
	}
	// ⓘ THE LAG IS ON THE SUMMARY LINE (task 01a13440). The step list below
	// need not be read by an operator grepping a log for the freeze: the number
	// rides the pass's own one-line summary, which every face already prints
	// (Rule 14 — one rendering, many readers). "aligned" says nothing, so the
	// field is omitted when it is at the peer's tip.
	if r.IntegrationLag != "" && r.IntegrationLag != gitSyncLagAlignedValue {
		parts = append(parts, fmt.Sprintf("%s=%s", gitSyncLagSummaryField, r.IntegrationLag))
	}
	// The divergence half of the same measurement, on the same line: a
	// non-zero local-only count means the number above cannot be closed by this
	// pass. Omitted at its zero for the reason the lag field is omitted when
	// aligned (an aligned and a purely-behind host keep their exact line).
	if r.IntegrationAhead != "" && r.IntegrationAhead != gitSyncLagAlignedValue {
		parts = append(parts, fmt.Sprintf("%s=%s", gitSyncLagAheadSummaryField, r.IntegrationAhead))
	}
	if len(r.Adopted) > 0 {
		parts = append(parts, fmt.Sprintf("adopted=[%s]", strings.Join(r.Adopted, " ")))
	}
	if len(r.Dispatched) > 0 {
		parts = append(parts, fmt.Sprintf("dispatched=[%s]", strings.Join(r.Dispatched, " ")))
	}
	if r.Duped > 0 {
		parts = append(parts, fmt.Sprintf("duped=%d", r.Duped))
	}
	if len(parts) == 0 {
		return "no-op"
	}
	return strings.Join(parts, " ")
}

// step records one step's outcome into the report (fail-soft: the pass
// keeps running, the verdict travels).
func (r *GitPassReport) step(name string, err error, okDetail string) {
	st := GitPassStep{Step: name, OK: err == nil}
	if err != nil {
		st.Detail = err.Error()
	} else {
		st.Detail = okDetail
	}
	r.Steps = append(r.Steps, st)
}

// gitSyncRun executes one git command in dir with the shared timeout.
//
// It is a DELEGATING ADAPTER: the exec itself lives in gitPeerRun
// (git_peer_fetch.go), the ONE `git -C <dir>` runner of the peer-fetch path
// — three wrappers for one job was the defect that file closes (Rules 13/14).
// The timeout is applied HERE because it is THIS lane's named cap
// (gitSyncTimeout); the runner deliberately bounds nothing.
func gitSyncRun(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitSyncTimeout)
	defer cancel()
	return gitPeerRun(ctx, dir, nil, args...)
}

// GitSyncRepoRoot resolves this process's git repo root (the pass runs
// inside the gopi checkout on both sides). Exported for the CLI face and
// tests; empty on error (caller gates).
func GitSyncRepoRoot() string {
	out, err := gitSyncRun(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// --- the client (HP) pass ---

// GitSyncPushLocalBranches pushes to the server ONLY the local branches the
// server has not yet integrated (its tip is an ancestor of server master →
// SKIPPED, so the push never resurrects a deleted ref — the merge-branch-flood
// bugfix, see gitPushbackPlan). Every other unmerged branch IS pushed: whether
// it can fast-forward onto the server's master is a FACT this lane reports
// (see the plan's alignment note), never a refusal — the server lands a branch
// with `git merge --ff-only` and owns that call. bug038 idempotency holds (an
// already-sent ref prints up-to-date, exit 0 — free re-ticks). Exported for the
// client pass and the CLI; single-branch failure is reported, not fatal.
func GitSyncPushLocalBranches(repo, server string) (pushed, failed []string) {
	flush := gitSyncPushLocalPlan(repo, server)
	return flush.Pushed, flush.Failed
}

// gitSyncPushLocalPlan is the ONE entry into the push-back core for this
// package's callers: it owns the target validation (the ssh-alias guard) and
// returns the WHOLE flush, so a caller that reports the alignment halves
// (GitSyncClientPass) and one that only needs pushed/failed
// (GitSyncPushLocalBranches) cannot drift into two different validations or
// two different plans (Rule 14).
func gitSyncPushLocalPlan(repo, server string) gitPushbackFlush {
	server = strings.TrimSpace(server)
	if server == "" {
		return gitPushbackFlush{Failed: []string{"no server target"}}
	}
	// ssh-shaped target ("<alias>:<path>"): the ALIAS half must not be
	// option-shaped — git hands the host to ssh as an argv element, so a
	// "-oProxyCommand=…" alias would inject an ssh option through git too.
	// A local filesystem path (fixtures, no ':') is not an ssh argv
	// element and is left untouched.
	if strings.Contains(server, sshPeerAliasTargetSeparator) {
		if _, err := ValidateSSHGitTarget(server); err != nil {
			return gitPushbackFlush{Failed: []string{err.Error()}}
		}
	}
	return gitPushbackPlan(context.Background(), repo, server)
}

// gitSyncErrLine extracts the diagnostic tail of a failed git call (the
// harvestGitErrLine contract: one line is enough to diagnose).
func gitSyncErrLine(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		return msg[i+2:]
	}
	return msg
}

// GitSyncClientPass runs the HP-side git pass: push local feat/* back to
// the server, then pull master (--ff-only). Steps are independent; any
// failure is recorded and the pass continues. server == "" skips the pass
// (no peers = no server to sync with).
func GitSyncClientPass(repo, server string) *GitPassReport {
	rep := &GitPassReport{Role: "client"}
	if strings.TrimSpace(server) == "" {
		// An empty server is two conditions, named apart (Rule 14 — the
		// finish-cleanup chain reports the same condition through the same
		// renderer): no peer declared at all (single-node: nothing is handed
		// off to any server) vs a declared-but-unusable alias (a settings slip).
		rep.Skipped, rep.SkipReason = true, noServerTargetSkipReason(finishCleanupNoServerTargetBase)
		return rep
	}
	if repo == "" {
		rep.Skipped, rep.SkipReason = true, "no git repo (not inside a checkout)"
		return rep
	}
	// An option-shaped / malformed ssh target is refused up front (skip +
	// reason) instead of being handed to git: the alias half travels to ssh
	// as an argv element (peer-alias guard, internal/tools/peer_alias.go).
	if strings.Contains(server, sshPeerAliasTargetSeparator) {
		if _, err := ValidateSSHGitTarget(server); err != nil {
			rep.Skipped, rep.SkipReason = true, err.Error()
			return rep
		}
	}
	// ① push-back: local feat/* → server refs (deliverable{branch} lands
	// BEFORE any db row migrates — git first, db second).
	flush := gitSyncPushLocalPlan(repo, server)
	rep.BranchesPushed, rep.PushFailed = flush.Pushed, flush.Failed
	rep.BranchesFastForwardable, rep.BranchesNotFastForwardable = flush.FastForwardable, flush.NotFastForwardable
	if len(flush.Failed) > 0 {
		rep.step("push", fmt.Errorf("%d branch(es) failed: %s", len(flush.Failed), strings.Join(flush.Failed, "; ")),
			fmt.Sprintf("%d pushed", len(flush.Pushed)))
	} else {
		rep.step("push", nil, fmt.Sprintf("%d pushed (idempotent)", len(flush.Pushed)))
	}
	// ② fetch --prune <peer>: the peer's namespace is mirrored here
	// (refs/remotes/<peer>/*), and the peer DELETES a branch once it has
	// landed it — its adoption chain does `branch -r -d`. This step is what
	// makes that deletion cross the wire; without it the mirror never goes
	// away.
	//
	// MEASURED 2026-09-26 on HP: `git ls-remote --heads peerbox` = 3 refs
	// (master + 2 live task branches) while `git branch -a` printed 187 — 184
	// local ghost refs, the oldest from 2026-09-16, i.e. every branch the peer
	// had ever adopted and reaped. That is the operator-visible symptom "my
	// finished branches are still in the repo": the LOCAL ref is reaped
	// correctly on both sides (the close-out's step ④ here, `branch -r -d`
	// there), and the one ref with no deleter was this mirror.
	//
	// A SEPARATE step, deliberately NOT folded into the pull below. The pull is
	// refused whenever the two masters have diverged (`--ff-only`), and HP was
	// measured in exactly that state on 2026-09-26 (`peerbox/master...master` =
	// 5 behind / 1 ahead). Riding the prune on the pull would therefore stop
	// pruning precisely when the checkout is out of step — the state a
	// long-lived client host is in most of the time. The prune is a fact about
	// the PEER's ref list, not about this checkout's history, so it takes the
	// evidence it needs (one fetch) and does not depend on the merge that
	// follows.
	pruneBefore := gitSyncPeerRefCount(repo, server)
	if _, err := gitSyncRun(repo, "fetch", gitSyncFetchPruneVerb, gitSyncFetchNoTagsFlag,
		gitSyncPruneSource(repo, server)); err != nil {
		rep.step("fetch-prune", err, "")
	} else {
		rep.step("fetch-prune", nil, gitSyncPruneDetail(pruneBefore, gitSyncPeerRefCount(repo, server)))
	}
	// ③ fast-forward the INTEGRATION BRANCH to the peer's tip. The client
	// never merges (asymmetry): a diverged branch is reported and skipped,
	// never resolved here.
	//
	// ── WHY THIS IS NOT A `git pull --ff-only` ANY MORE (measured 2026-10-12) ──
	//
	// The step used to run `git pull --ff-only` with NO refspec, under a runner
	// that only sets cwd=repo root. A pull with no refspec merges the CALLER's
	// current branch, and the pass's repo root is reached from every kind of
	// checkout. In a worker's linked worktree (branch task/<id>, no upstream)
	// the fetch leg still updated refs/remotes/<peer>/master while the merge leg
	// could do nothing, and `git pull` exits 1 with "There is no tracking
	// information for the current branch" — the step reported a FAILURE and
	// refs/heads/master never moved. Measured on the real host: 0 moves of
	// refs/heads/master against 4 of the peer's remote ref, and a local
	// integration ref frozen 70 commits behind the peer. It could also do the
	// WRONG thing: any staged caller-side commit on master is fast-forwarded by
	// the pull before its own delivery finishes (the shared-integration-ref
	// incident's precondition).
	//
	// The three facts the replacement rests on, all measured (this run's own
	// fixture, evidence in the session directory):
	//
	//	- `git fetch <peer> refs/heads/<b>:refs/heads/<b>` is REFUSED by git
	//	  with exit 128 when ANOTHER worktree holds <b> ("refusing to fetch into
	//	  branch 'refs/heads/master' checked out at '…'") — the shared ref can
	//	  never be advanced by a write that bypasses the holder's index;
	//	- `git merge --ff-only <ref>` run INSIDE the holder moves ref + index +
	//	  working tree together and leaves the holder's status clean;
	//	- with NO worktree holding <b>, git ACCEPTS the plain (unforced) refspec
	//	  fetch and creates the ref (exit 0).
	//
	// So the step resolves WHO holds the branch and takes the route that fits:
	// the holder gets the merge, nobody-holds-it gets the create, and everything
	// else (an unreadable worktree survey, a refused fetch, a merge git would
	// not fast-forward) is REPORTED rather than silently rewritten or silently
	// declared a success.
	//
	// The merge target is the durable mirror ref this repository already
	// maintains of the peer's tip (gitPushbackCheckRef, also what the push-back
	// lane's and the pre-merge gate's `SyncPeerFetch` write) rather than a sha —
	// measured: `merge --ff-only <sha>` fails with "not something we can merge"
	// unless that sha happens to be present locally, so a clean route must name
	// a REF.
	//
	// denyCurrentBranch can never hit us here (we move refs/heads in a non-bare
	// repo, never push to it).
	ff, skipped := gitSyncFFIntegrationBranch(repo, server)
	if skipped != "" {
		rep.step(gitSyncStepPullBranchName, errors.New(skipped), "")
		rep.PullFF = gitSyncPullSkippedPrefix + skipped + gitSyncPullSkippedClose
	} else {
		rep.step(gitSyncStepPullBranchName, nil, ff)
		rep.PullFF = ff
	}

	// ④ THE STALENESS ALARM. Steps ①–③ report what each STEP did; none of them
	// reports the fact the step exists for — "is this host's integration ref
	// where the peer's is?". Measured 2026-10-13 (task 01a13440): on this host
	// the local ref was 141 commits behind and a purely-ancestor of the peer's
	// tip while EVERY step of every pass printed success, so the freeze was
	// invisible for as long as it lasted — and a report that says "a gate is
	// red on master" then cannot be re-checked against any revision.
	//
	// So the pass measures the distance between the two refs it already holds
	// (no network, no second fetch: the mirror ref IS the peer's tip as this
	// repo last saw it) and records it as a step AND a value. A lag of at
	// least one commit is a FAILED step — the pass's own summary line then
	// carries it, and `rep.IntegrationLag` names the number for any caller
	// that wants to route it. Zero is the ordinary aligned case and reports as
	// such; an unmeasurable lag (no mirror yet, an unreadable repo) is
	// reported as unmeasurable and is NOT a failure — an unanswered question
	// is not a defect (Rule 15a), it is a question nobody asked yet.
	//
	// THE MEASUREMENT IS TWO NUMBERS, because the alarm's prescription depends
	// on BOTH (task 01a134ca): `lag` is how far behind, `ahead` is how much of
	// this host's own work the peer lacks. ahead=0 means the pass's own
	// fast-forward closes the gap, and the sentence says so; ahead>0 means the
	// refs DIVERGED and that fast-forward can never happen, so the sentence
	// names an executable next step instead — a prescription the pass cannot
	// honour is worse than none (it trains the reader to ignore the word
	// WARNING). Both numbers ride the summary line, so the log distinguishes
	// the two shapes without a step name.
	lag, ahead, lagDetail := gitSyncIntegrationLag(repo)
	rep.IntegrationLag, rep.IntegrationAhead = lag, ahead
	switch lag {
	case gitSyncLagUnknown, gitSyncLagAlignedValue:
		// Nothing to warn about: an unanswered question is not a defect
		// (Rule 15a), and the aligned value is the ordinary case.
		rep.step(gitSyncStepStaleWarning, nil, lagDetail)
	default:
		// At least one commit behind: the step FAILS. That is what makes the
		// staleness ride the pass's own summary line (the CLI faces print a
		// line per failed step to stderr) instead of existing only as a number.
		// The sentence (lagDetail) is the classification's: it promises a
		// fast-forward only for a purely-behind pair, and names the operator's
		// own merge for a diverged one.
		rep.step(gitSyncStepStaleWarning, errors.New(lagDetail), "")
	}
	return rep
}

// gitSyncServerLag records the alarm's verdict on the SERVER arm, which cannot
// take the client's measurement: the ref the client compares against
// (gitSyncLagRef) is the client's DURABLE mirror of the server's tip, while the
// same-named ref on a server is whatever the push-back lane last fetched into a
// pass-private reservation — a different fact wearing the same name.
//
// It reports `unmeasurable` with that reason rather than a number, and it is a
// NON-failing step: the server's master advances by its OWN merges, so the
// client's staleness question has no server-side analogue, and printing a
// fabricated lag (or a cheerful "aligned") would be the exact silent-success
// shape this epic exists to end (Rule 15).
func gitSyncServerLag() (lag, detail string) {
	return gitSyncLagUnknown, fmt.Sprintf("integration ref %s: %s on a server — the lag is a client-side fact and this arm must not invent it",
		refsHeadsPrefix+IntegrationBranchName, gitSyncLagUnknown)
}

// gitSyncIntegrationLag answers "how far behind the peer's tip is this
// repository's integration ref, and has the pair DIVERGED?", as strings the
// report can print verbatim:
//
//	lag "0",        ahead "0"       - at the peer's tip (or ahead of it: this
//	                                  host has work the peer has not taken yet,
//	                                  which is not a staleness)
//	lag "<n>",      ahead "0"       - n behind, PURELY BEHIND: the pass's own
//	                                  fast-forward closes it
//	lag "<n>",      ahead "<m>"     - n behind AND m commits the peer does not
//	                                  have: the pair DIVERGED, so no
//	                                  fast-forward exists and this pass can
//	                                  never close the gap
//	unmeasurable,   unmeasurable    - a count could not be taken
//
// Both operands are LOCAL refs, so the measurement never touches the network
// and cannot fail the pass; the peer's half is the reserved mirror ref the
// delivery lane already maintains (`gitPushbackCheckRef`), which the same pass
// refreshed one step earlier. The counts are taken with the ONE relative-refspec
// spelling this package uses (`git rev-list --count <from>..<to>`, the same
// verb internal/tools/integration_claim_target.go reads), but through
// repoSweepGit rather than through the client pass's own runner: this accessory
// leg must produce NOTHING on stdout, and the client pass's runner wraps git's
// output into its own error text (the measurement is not the lane's, and it must
// not inherit its output contract).
//
// WHY THE SECOND COUNT IS TAKEN (the defect that returns this classification to
// close; measured 2026-10-13 in task 01a13440's own run and filed as task
// 01a134ca). The behind count ALONE cannot tell the two shapes apart, and the
// alarm printed ONE prescription - `gopi task push` (this pass) - for both. On a
// diverged pair that prescription names the very pass whose client arm refuses
// to rebase/merge BY DESIGN (gitSyncFFIntegrationBranch merges `--ff-only`, so
// git answers "Not possible to fast-forward"), which sent the operator back to a
// command that can never succeed while the ref stayed frozen and `pull:skipped`
// repeated on every tick. The classification is exactly one more
// `rev-list --count`, in the opposite direction, over the same two refs.
//
// The second count is read ONLY where the first says there is a gap: a host AT
// the peer's tip cannot be diverged with a non-zero lag, so on the aligned path
// the extra git child would be paid on every tick for an answer that cannot
// differ - the zero reported there is entailed by the measured lag, not a guess.
// (The loop is over the LOCAL integration ref's side of the pair; whatever the
// ref is CALLED (master, main), the count is taken between the two refs the
// delivery lane already names, never from a branch name typed here.)
//
// An unreadable count is ONE answer for the WHOLE measurement (never a measured
// half plus an invented half): callers render a single sentence from `lag`, and
// a half-taken measurement has no classification to state (Rule 15a). Leaving
// the behind count visible with an unmeasurable divergence half would print the
// fast-forward promise for a pair nobody checked - the exact defect this
// classification exists to close.
func gitSyncIntegrationLag(repo string) (lag, ahead, detail string) {
	return gitSyncIntegrationLagAgainst(repo, gitSyncLagRef)
}

// gitSyncIntegrationLagAgainst is the SAME measurement, asked against ONE named
// mirror ref instead of the delivery lane's own reservation (gitSyncLagRef).
//
// WHY THE PARAMETER EXISTS (the read-only carrier's half of task 01a1837c): the
// question "is this host's integration ref behind the PEER's?" is answerable
// from more than one mirror on a client host — the ordinary remote-tracking ref
// the operator's fetches refresh (`refs/remotes/origin/<branch>`) and the
// push-back lane's private reservation — and the answer differs between them
// when one of the two has been frozen. A second copy of this function per
// mirror would be a second measurement (Rule 14); the caller picks the refs and
// the classification stays HERE, so every reader gets the same verdicts for the
// same pair of commits.
func gitSyncIntegrationLagAgainst(repo, mirrorRef string) (lag, ahead, detail string) {
	branch := IntegrationBranchForRepo(repo)
	localRef := refsHeadsPrefix + branch
	raw, err := repoSweepGit(repo, integrationClaimRevListVerb, integrationClaimCountFlag,
		localRef+integrationClaimRelativeRefspecSeparator+mirrorRef)
	if err != nil {
		return gitSyncLagUnknown, gitSyncLagUnknown, fmt.Sprintf(gitSyncLagUnknownFmt, localRef)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return gitSyncLagUnknown, gitSyncLagUnknown, fmt.Sprintf(gitSyncLagUnknownFmt, localRef)
	}
	if n <= 0 {
		return gitSyncLagAlignedValue, gitSyncLagAlignedValue,
			fmt.Sprintf("integration ref %s %s", localRef, gitSyncLagAligned)
	}
	behind := strconv.Itoa(n)
	// THE IDENTITY QUESTION COMES FIRST, ahead of every count-based verdict: a
	// mirror that belongs to another repository makes "behind" and "ahead"
	// meaningless numbers, and the DIVERGED prescription a dangerous one. An
	// unmeasured answer (git could not be asked) does NOT shorten this path —
	// it falls through to the counts, whose own unmeasurable arm reports the
	// single `unmeasurable` verdict (Rule 15: never invent a finding).
	//
	// THE ERROR IS READ, not discarded: peerRepoIdentityShared answers
	// (false, err) when git could not be asked, and a bare `!_shared` would
	// turn "I do not know" into the measured verdict "different repository" —
	// the exact inversion Rule 15 names. Only `err == nil && !shared` is the
	// measurement; any error keeps the counts' own path.
	shared, identityErr := peerRepoIdentityShared(context.Background(), repo, mirrorRef, localRef)
	if identityErr == nil && !shared {
		return behind, gitSyncLagAlignedValue,
			fmt.Sprintf(gitSyncLagDifferentRepoFormat, localRef,
				peerRepoIdentityUnrelatedDetail("", mirrorRef, localRef))
	}
	localOnly, err := gitSyncIntegrationAheadCount(repo, mirrorRef, localRef)
	if err != nil {
		// The divergence question could not be answered, so the measurement has
		// NO classification: reporting the behind count alone would print the
		// fast-forward promise for a pair nobody checked (the exact defect this
		// classification exists to close), and the error is not swallowed — it
		// becomes the measurement's own unmeasurable verdict (Rule 15).
		return gitSyncLagUnknown, gitSyncLagUnknown, fmt.Sprintf(gitSyncLagUnknownFmt, localRef)
	}
	if localOnly == gitSyncLagAlignedValue {
		return behind, gitSyncLagAlignedValue, fmt.Sprintf(gitSyncLagBehindFormat, localRef, behind, mirrorRef)
	}
	return behind, localOnly,
		fmt.Sprintf(gitSyncLagDivergedFormat, localRef, behind, mirrorRef, localOnly, localRef)
}

// gitSyncIntegrationAheadCount counts the commits the LOCAL integration ref
// carries that the peer's mirrored tip does not — the direction that turns a
// behind count into a divergence verdict. It answers as a STRING for the
// caller's own contract (a count that could not be taken is never spelled 0),
// and an unreadable answer is an ERROR the caller turns into the measurement's
// single `unmeasurable` verdict rather than a fabricated zero.
func gitSyncIntegrationAheadCount(repo, mirrorRef, localRef string) (string, error) {
	out, err := repoSweepGit(repo, integrationClaimRevListVerb, integrationClaimCountFlag,
		mirrorRef+integrationClaimRelativeRefspecSeparator+localRef)
	if err != nil {
		return "", fmt.Errorf("count the commits %s carries that %s does not: %w", localRef, mirrorRef, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return "", fmt.Errorf("count the commits %s carries that %s does not returned %q: %w",
			localRef, mirrorRef, strings.TrimSpace(out), err)
	}
	return strconv.Itoa(n), nil
}

// GitSyncStaleAlarm renders the ONE warning line the CLI faces print when a
// pass leaves the integration ref behind the peer's tip, and reports whether
// there was anything to warn about. It is EXPORTED because two faces must warn
// with the same words (Rule 14): the serve-tick sweep and the manual
// `gopi task push` — an operator who runs the verb by hand must not have to
// read the log of a daemon to learn that the verb did not close the gap.
//
// The sentence comes FROM THE STEP (`gitSyncStepStaleWarning`'s Detail), never
// from a second rendering of the same numbers: the step is what the pass printed
// per-failure already, so the alarm and the step cannot drift into two spellings
// of one fact.
//
// A `gitSyncLagUnknown` lag is NOT a warning: the pass has already reported it
// as an unmeasurable step, and a warning about a number nobody could take would
// train the reader to ignore the word.
func (r *GitPassReport) GitSyncStaleAlarm(role string) (string, bool) {
	if r == nil || r.IntegrationLag == "" {
		return "", false
	}
	if r.IntegrationLag == gitSyncLagAlignedValue || r.IntegrationLag == gitSyncLagUnknown {
		return "", false
	}
	return fmt.Sprintf(gitSyncLagAlarmFormat, role, r.Role, r.StepDetail(gitSyncStepStaleWarning)), true
}

// StepDetail returns the recorded detail of a named step ("" when the pass
// never ran it). It exists so a caller can render a step's own sentence without
// re-deriving the numbers behind it — the same "one writer, many readers"
// discipline the step list itself follows.
func (r *GitPassReport) StepDetail(name string) string {
	if r == nil {
		return ""
	}
	for _, st := range r.Steps {
		if st.Step == name {
			return st.Detail
		}
	}
	return ""
}

// gitSyncFFIntegrationBranch fast-forwards this repository's local
// integration-branch ref to the peer's tip, and answers:
//
//	(detail, "")   — the step succeeded; detail is what the report prints;
//	("", reason)   — the step could NOT do its job, and no ref, index or
//	                 working tree was touched (the reason is reported as a
//	                 failure, never as a success — Rule 15).
//
// The ALGORITHM, and why each branch of it is the shape it is (all measured on
// git 2.43.0; the measurement fixture and its raw output live in this task's
// session directory):
//
//	① resolve the integration branch from the repo (IntegrationBranchForRepo:
//	   master, else main) and its local ref;
//	② survey `git worktree list --porcelain` to find WHO holds that ref, and
//	   refuse the whole step when that survey cannot be read (an unjudged write
//	   into a namespace every worktree shares is never permitted — Rule 15);
//	③ nobody holds it → fetch the peer tip straight onto the ref (git allows
//	   this — measured, exit 0), which CREATES the ref on a host that has never
//	   had the integration branch locally;
//	④ a worktree holds it → merge the peer tip THERE (the caller's own checkout,
//	   or the sibling worktree that holds the branch). The ref, that worktree's
//	   index and its working tree move together, which is exactly what a bare
//	   ref write must never do here.
//
// A holder that cannot be written through is NOT a special case: the branch ref
// is one ref of one repository, so whichever worktree holds it is the one that
// gets the merge, and this pass never needs to reason about which KIND of
// worktree that is.
//
// Every failure is a REASON, and the pass reports it: the step's success
// sentence is only ever printed from a measured, successful git operation.
func gitSyncFFIntegrationBranch(repo, server string) (detail, skipped string) {
	branch := IntegrationBranchForRepo(repo)
	localRef := refsHeadsPrefix + branch
	worktrees, err := gitSyncWorktreePaths(repo)
	if err != nil {
		// The holder cannot be resolved, so an unguarded write is UNKNOWN work:
		// refuse it (Rule 15). This is the same predicate, on the same evidence,
		// that guardSharedIntegrationRef applies to the shell's own commands.
		return "", fmt.Sprintf(gitSyncHolderUnreadableFmt, localRef, err)
	}
	before, err := gitSyncRun(repo, "rev-parse", gitPeerVerifyFlag, gitPeerQuietFlag, localRef+gitPeerCommitSuffix)
	haveLocal := err == nil && strings.TrimSpace(before) != ""
	before = strings.TrimSpace(before)
	switch holder := gitSyncBranchHolderPath(worktrees, branch); {
	case holder == "":
		// NOBODY holds the branch: git accepts the plain (unforced) refspec
		// fetch and CREATES the ref (measured, exit 0). This is the route for a
		// host whose main checkout sits on some other branch, and for a
		// repository that has never had the integration branch locally.
		if _, err := gitSyncRun(repo, gitPeerFetchVerb, gitSyncFetchNoTagsFlag, server, localRef+refGuardRefSep+localRef); err != nil {
			return "", gitSyncErrLine(err)
		}
		after := gitSyncRefTip(repo, localRef)
		if after == "" {
			return "", fmt.Sprintf(gitSyncRefUnresolvedFmt, localRef)
		}
		if haveLocal && after == before {
			return gitSyncPullUpToDateDetail, ""
		}
		return gitSyncFfDetailPrefix + after, ""
	default:
		// A WORKTREE HOLDS IT, and the merge runs THERE — in the caller's own
		// checkout, or in the sibling worktree that holds the branch. This is
		// the same operation a pull performs, aimed at the branch the pass is
		// about instead of at whatever the caller happened to check out, and it
		// moves the ref, that worktree's index and its working tree TOGETHER —
		// which is precisely what a bare ref write must never be allowed to do
		// here (the shared-integration-ref incident).
		//
		// The target is the durable mirror ref this repository maintains of the
		// peer's tip (gitPushbackCheckRef): a RESERVED, non-branch ref that no
		// worktree can hold, so mirroring the peer can never desync a checkout,
		// and it stays a REF — measured: `merge --ff-only <sha>` fails with "not
		// something we can merge" unless that sha happens to be present locally,
		// so a sha is not a usable target.
		if err := gitSyncFetchPeerMirror(holder, server); err != nil {
			return "", gitSyncErrLine(err)
		}
		if _, err := gitSyncRun(holder, gitSyncMergeVerb, gitSyncMergeFFOnlyFlag, gitPushbackCheckRef); err != nil {
			return "", gitSyncErrLine(err)
		}
		return gitSyncFfDetailFromMerge(repo, localRef, before), ""
	}
}

// gitSyncFfDetailFromMerge renders the step's success detail after a merge that
// ran in the branch's holder worktree: the ref's NEW tip, read back from the
// repository (never from git's human prose — the same discipline
// gitSyncPruneDetail applies to the prune count).
//
// A merge that did not move the ref is the ordinary "nothing new" case and is
// reported as up-to-date; a ref that moved is reported by value, so the log
// answers "which commit did this pass take?" without a second command.
func gitSyncFfDetailFromMerge(repo, localRef, before string) string {
	after := gitSyncRefTip(repo, localRef)
	if after == "" {
		return gitSyncPullUpToDateDetail
	}
	if before != "" && after == before {
		return gitSyncPullUpToDateDetail
	}
	return gitSyncFfDetailPrefix + after
}

// gitSyncRefTip reads a local ref's commit id, or "" when the ref does not
// resolve (a missing-ref probe is an ordinary answer here, never an error).
func gitSyncRefTip(repo, ref string) string {
	out, err := gitSyncRun(repo, gitPeerRevParseVerb, gitPeerVerifyFlag, gitPeerQuietFlag, ref+gitPeerCommitSuffix)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// gitSyncFetchPeerMirror fetches the peer's integration branch into the durable
// mirror ref (gitPushbackCheckRef) that the merge target is read from. It is a
// FORCED refspec onto a reserved NON-branch ref, which no worktree can hold, so
// this write can never desync anybody's checkout — and the mirroring is the
// same one SyncPeerFetch performs, through the same shared refspec builder
// (Rule 14).
func gitSyncFetchPeerMirror(repo, server string) error {
	_, err := gitSyncRun(repo, gitPeerFetchVerb, gitPeerFetchForceFlag, gitSyncFetchNoTagsFlag,
		server, gitPeerMirrorRefspec())
	return err
}

// gitSyncWorktreePaths lists every worktree of the repository AS OF NOW, through
// the ONE vocabulary this package keeps for that survey (finishCleanupWorktree*)
// — the same command refGuardWorktreePaths issues, parsed here because gitSyncRun
// (git's own stderr/wrapping convention) is this lane's runner and a probe's is
// not. Read fresh on every pass: the merger and the close-out chain add, remove
// and reclaim worktrees continuously, and a cached list would judge a stale world.
func gitSyncWorktreePaths(repo string) ([]string, error) {
	out, err := gitSyncRun(repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), finishCleanupPorcelainWorktreePrefix)
		if !ok {
			continue
		}
		if p := strings.TrimSpace(rest); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// gitSyncWorktreeHolds reports whether the worktree at dir has `branch` checked
// out. Unreadable answers are FALSE and are caught by the caller's separate
// holder test, which is the one allowed to fail closed.
func gitSyncWorktreeHolds(dir, branch string) bool {
	out, err := gitSyncRun(dir, "rev-parse", gitSyncAbbrevRefFlag, gitSyncHeadRef)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == branch
}

// gitSyncBranchHolderPath returns the path of the worktree holding `branch`, or
// "" when none does.
func gitSyncBranchHolderPath(worktrees []string, branch string) string {
	for _, wt := range worktrees {
		if _, err := os.Stat(wt); err != nil {
			continue
		}
		if gitSyncWorktreeHolds(wt, branch) {
			return wt
		}
	}
	return ""
}

// --- the server (peerbox) pass ---

// GitSyncServerPass runs the peerbox-side git pass: fetch the client's
// branches, then hand the clean-merge/delete decision to the adoption
// chain (③a lands here; ③b conflicts dispatch a Bond task — the
// red line ④ contract: Go discovers/tries/dispatches, the agent judges).
// repo == "" = no checkout → skip (fail-soft).
func GitSyncServerPass(repo string) *GitPassReport {
	rep := &GitPassReport{Role: "server"}
	if repo == "" {
		rep.Skipped, rep.SkipReason = true, "no git repo (not inside a checkout)"
		return rep
	}
	// ① fetch: every declared sync peer is a branch source (a local-path
	// remote lands directly in refs/heads; an ssh remote produces
	// refs/remotes/<peer>/* — AdoptionSweep sweeps BOTH channels).
	peers, perr := config.ResolveSyncPeers(nil)
	if perr != nil || len(peers) == 0 {
		rep.Skipped, rep.SkipReason = true, "no sync peers declared (nothing to fetch)"
		return rep
	}
	for _, p := range peers {
		out, err := gitSyncRun(repo, "fetch", "--prune", p)
		if err != nil {
			rep.step("fetch "+p, err, "")
			continue
		}
		rep.Fetched = append(rep.Fetched, p)
		rep.step("fetch "+p, nil, strings.TrimSpace(out))
	}
	// ② clean merge + branch delete: the adoption chain's exact path —
	// completed-task gate, probe merge, ③a commit + branch -d / -r -d,
	// ③b conflict dispatch (Bond). Submit=true: the serve face auto-
	// dispatches (the chain's whole point).
	counts, err := AdoptionSweep(nil, AdoptionOptions{Repo: repo, Submit: true})
	rep.Adopted, rep.Dispatched, rep.Duped = counts.Adopted, counts.Dispatch, counts.Duped
	if err != nil {
		rep.step("adopt", err, "")
	} else {
		rep.step("adopt", nil, fmt.Sprintf("scanned=%d adopted=%d dispatched=%d duped=%d skipped=%d",
			counts.Scanned, len(counts.Adopted), len(counts.Dispatch), counts.Duped, counts.Skipped))
	}
	// ③ the lag, asked where the question HAS an answer on this arm. The step
	// carries the reason as its Detail and NEVER fails (see gitSyncServerLag):
	// a client-side fact this arm must not invent.
	lag, lagDetail := gitSyncServerLag()
	rep.IntegrationLag = lag
	rep.step(gitSyncStepStaleWarning, nil, lagDetail)
	return rep
}

// GitSyncServerTarget renders the ssh push target the CLIENT uses: the
// first sync peer whose name is NOT this host (the peer list may contain
// this node's own alias — pushing to yourself is a permission error, the
// live HP incident of 2026-09-01: peers=[hp,peerbox], target must be
// peerbox). No such peer = no server (the caller skips, fail-soft).
func GitSyncServerTarget() string {
	peers, err := config.ResolveSyncPeers(nil)
	if err != nil {
		return ""
	}
	// GOPI_SYNC_NODE overrides the hostname (the syncSelfTag rule — a
	// same-host two-node simulation needs distinct node identities).
	self := os.Getenv("GOPI_SYNC_NODE")
	if self == "" {
		self, _ = os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	}
	for _, p := range peers {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if self != "" && strings.EqualFold(p, self) {
			continue // my own name — not a server to push to
		}
		// The peer alias becomes the ssh half of the push target: refuse an
		// option-shaped / malformed alias here so the caller fails-soft
		// (empty target = no server) instead of handing the injection to git.
		if _, err := ValidateSSHPeerAlias(p); err != nil {
			continue
		}
		return fmt.Sprintf("%s:%s", p, peerRepoPath())
	}
	return ""
}
