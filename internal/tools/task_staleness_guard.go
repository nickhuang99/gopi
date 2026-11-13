package tools

// Dispatch-time STALENESS GUARD for the merge-verify rework lane
// (epic merge-verify-rework-staleness, 2026-09-14).
//
// THE DEFECT (measured, row 01a09e9b): a rework briefing is FROZEN at filing
// time (TaskSubmit writes the payload; the executor later reads it verbatim as
// its briefing) and the merger's rework lane can then be re-landed by a
// sibling row while the rework row sits pending. Measured case: the row's
// deliverable named `fix/tasklist-inprogress-scan` (a typo — the real ref was
// `fix/task-list-inprogress-scan`), the branch ref had ALREADY been deleted by
// the merged-cleanup sweep, and the fix itself had been re-landed on master as
// fc9b06d4 "adoption: merge fix/task-list-inprogress-scan (0618a2cd)" — 40
// minutes BEFORE the row was claimed. Nothing at claim time looked; a whole
// session could be spent reworking work already shipped.
//
// THE GUARD is a claim-time JUDGEMENT, not a new tool (Rule 3) and not a
// daemon: the dispatchers (worker round / serve dispatch / `task run next`)
// all reach the queue through TaskNextFiltered, so the judge runs there —
// BEFORE the CAS claim — and a row whose premise is already false is CLOSED as
// a no-op that points at the landing commit instead of being dispatched.
//
// WHAT IS CHECKED (the premise of a merge-verify rework briefing is exactly
// two claims, so the judgement is two git facts):
//
//	1. `git rev-parse --verify --quiet refs/heads/<branch>` in the row's repo
//	   — does the branch the briefing tells the executor to check out even
//	   exist? (exit 128 = fatal → NOT FOUND.)
//	2. `git merge-base --is-ancestor <branch> <master>` — if the branch tip is
//	   already an ancestor of the integration branch, the fix is ON master and
//	   there is nothing to rework.
//
//	3. the LANDING arm: when the ref is gone (the normal end state — the
//	   merger's merged-cleanup pass deletes a merged ref with `git branch -d`,
//	   which only succeeds for an already-merged tip), the guard proves the
//	   landing from master's OWN history, through EVERY form this project's
//	   writers produce (mergeVerifyStaleLandingToken): git's merge subjects
//	   `Merge branch '<b>'` / `Merge remote-tracking branch '<b>'`, the
//	   conflict-resolution subject `resolve merge <b> into master`, and this
//	   lane's resolver `adoption: merge <b> (<sha>)`.
//
//	   ① proved ⇒ the row is closed as LANDED (verdict landed, status
//	      COMPLETED): the work shipped, and recording it as anything else is
//	      what defect D8 measured. See the D8 note below.
//	   ② not proved by a FULL-HISTORY search over master (never a window: the
//	      absence arm's soundness is the search's, not a record count's) ⇒
//	      the refused premise stands (verdict branch-gone-unproved), cancelled
//	      with the reworded reason.
//	   ③ the search could not run, or a matching record did not parse ⇒
//	      unmeasured, NEVER closed.
//
// DEFECT D8 (measured, rows 01a0a59a / 01a0a9e0 — fixed 2026-09-17): the
// landing arm used to recognise ONLY the resolver's `adoption: merge` subject.
// A branch landed by the ORDINARY merger (`git merge <branch>` → git's own
// "Merge branch '<b>'" subject; `resolve merge <b> into master` when a worker
// resolved it by hand) leaves no adoption subject at all, so the guard found no
// witness, called "ref gone" a broken briefing, and CANCELLED a successfully
// merged row as a failure. Both cited rows are on master —
// `51edd8d9 Merge branch 'feat/web-lan-access'` and `15060a5c resolve merge
// feat/merger-status-log into master` — and both were closed as
// "branch-not-found". The whole guard run to date had cancelled exactly those
// two rows, so 100% of its cancellations were false positives. The fix: the
// landing proof reads master's history through every landing form, a proof
// closes the row as LANDED (completed), and only a genuine, untruncated
// absence keeps a refusal — reworded to say exactly that, and settled by a
// full-history search (a windowed absence reading was unsound on this repo:
// master carries 3714 commits against the 2000-record window, so the refusal
// arm would have been unreachable in production).
//
// NOT-THIS-ARM (deliberate): a genuinely UNLANDED branch (ref present, tip not
// an ancestor of master) is legitimate rework and is ALWAYS dispatched — the
// guard never blocks real work; it only refuses premises that are already
// false at claim time.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// mergeVerifyReworkBranchKey is the deliverable JSON key the rework lane's
	// rows carry their branch under — the same key every deliverable writer
	// emits (verify_gate.go's two stamps, task_deliverable_branch.go's
	// backfill), so the guard reads what the row really carries.
	mergeVerifyReworkBranchKey = "branch"

	// mergeVerifyGitTimeoutSec bounds ONE staleness probe. A guard that can
	// hang the dispatcher is worse than the stale row it hunts, so every git
	// call is deadline-bounded and a timeout is recorded as an unusable
	// measurement (Rule 15: never a silent "clean").
	mergeVerifyGitTimeoutSec = 10

	// git's two NEGATIVE exit statuses, distinguished because they mean
	// different things:
	//   mergeVerifyGitExitNo    = 1   git answered "no" (rev-parse --quiet:
	//                                 the ref is absent; is-ancestor: not an
	//                                 ancestor) — a REAL measurement.
	//   mergeVerifyGitExitFatal = 128 git could not answer at all ("fatal:
	//                                 Needed a single revision", a bad ref
	//                                 spelling) — never read as absence.
	mergeVerifyGitExitNo    = 1
	mergeVerifyGitExitFatal = 128

	// mergeVerifyStaleEvidenceTail caps the git stderr tail recorded as
	// evidence (a full fatal line is ~120 bytes; the cap keeps the row's
	// reason column readable).
	mergeVerifyStaleEvidenceTail = 200

	// mergeVerifyStaleSweepCap bounds how many stale rows ONE claim attempt
	// closes before moving on to a runnable row (a corrupt ledger full of
	// stale rows must not turn a claim into an unbounded loop).
	mergeVerifyStaleSweepCap = 8

	// mergeVerifyStaleLogWindow bounds the commit window the guard reads off
	// the integration branch when it looks for the re-landing adoption commit
	// (the merger re-lands within minutes of the revert; the window only has
	// to cover the landing that motivated the row).
	mergeVerifyStaleLogWindow = 40

	// mergeVerifyLogFormat is the git log wire format the landing scan parses:
	// record = %H (sha) %x1f (field sep) %B (raw body, first line is the
	// subject) %x1e (record sep). A format literal (Rule 1b): named once here,
	// never retyped at a call site.
	mergeVerifyLogFormat = "--format=%H" + mergeVerifyLogSubjectFieldFmt + "%B" + mergeVerifyLogRecordFmt

	// mergeVerifyStaleTipHint is the short-sha width used when the guard names
	// a commit in the no-op text (the same width `gopi task list` renders).
	mergeVerifyStaleTipHintLen = 8

	// mergeVerifyStaleDeclinedMarker marks a run whose premise the guard
	// REFUSED to judge (no branch recorded, no repo resolvable, git absent or
	// timed out). It is deliberately NOT a stale verdict: an unmeasurable
	// premise leaves the judgement open and the row claimable — the guard must
	// never close work it could not measure.
	mergeVerifyStaleDeclinedMarker = "premise-unmeasured"

	// mergeVerifyGitMergeSubjectQuote is git's own quoting of a branch name in
	// a merge commit's subject ("Merge branch '<b>'"). The quote pair is the
	// delimiter the LANDING parser reads the branch token between, defined
	// once (Rule 1b) and shared by the builder and the parser below.
	mergeVerifyGitMergeSubjectQuote = "'"

	// mergeVerifyGitMergeSubjectPrefix is the subject git writes for a merge
	// commit ("Merge branch '…'", "Merge remote-tracking branch '…'"): the
	// prefix that precedes the quote pair the landing parser reads.
	mergeVerifyGitMergeSubjectPrefix = "Merge branch "

	// mergeVerifyGitMergeSubjectRemote is the REMOTE spelling of the same
	// subject ("Merge remote-tracking branch 'origin/x'"), a distinct landing
	// form the parser must recognise: the local spelling's prefix is a strict
	// prefix of this one, so the two can never be confused, and neither may be
	// matched as the other (a remote ref is not the row's local branch).
	mergeVerifyGitMergeSubjectRemote = "Merge remote-tracking branch "

	// mergeVerifyResolveMergePrefix/Suffix delimit the conflict-resolution
	// landing subject ("resolve merge <b> into master") — the third landing
	// form, matched at BOTH ends so "resolve merge conflict: … into master" (a
	// ledger SUMMARY, never a commit subject) cannot be read as a landing.
	mergeVerifyResolveMergePrefix = "resolve merge "
	mergeVerifyResolveMergeSuffix = " into master"

	// mergeVerifyMergeBacklogPrefix brackets the merge-backlog EXECUTOR's own
	// merge subject. Its branch is interpolated from mergeExecMergeMsg — the
	// very constant task_merge_executor.go commits with — so the writer and the
	// witness cannot drift apart (Rule 14: one source of truth for the fact).
	mergeVerifyMergeBacklogPrefix = "merge-backlog: merge "

	// mergeVerifyLocalRefPrefix is git's canonical LOCAL-branch ref prefix. The
	// merge-backlog executor names its merge target as a full ref
	// (`refs/heads/<b>`, the form it hands `git merge`), so a landing token is
	// normalized out of that spelling before it is compared with the row's
	// branch — the compare itself stays an EQUALITY (only the ref SPELLING is
	// reduced, never a fuzzy match).
	mergeVerifyLocalRefPrefix = "refs/heads/"

	// mergeVerifyRefSep is the ref-path separator the normalization trims from
	// both ends of a token: a trailing separator (or a doubled one) is a
	// spelling of the same branch, not a different one.
	mergeVerifyRefSep = "/"

	// mergeVerifyLogSubjectFieldFmt/RecordFmt are the git-log FORMAT escapes
	// (--format=%x1f / %x1e) that ASK git for the two separators, and
	// mergeVerifyLogSubjectFieldSep/RecordSep are the BYTES that actually
	// arrive. Both pairs are named (Rule 1b) and both are needed: the format
	// argument is a git-side spelling, the parser reads the wire — conflating
	// them silently yields a one-record stream that parses as no records.
	mergeVerifyLogSubjectFieldFmt = "%x1f"
	mergeVerifyLogRecordFmt       = "%x1e"
	mergeVerifyLogSubjectFieldSep = "\x1f"
	mergeVerifyLogRecordSep       = "\x1e"

	// mergeVerifyGitLogWindow is the commit window the POSITIVE landing scan
	// reads off the integration branch, in RECORDS (git's `-n` counts the
	// records a pass returns). It bounds the RICH-EVIDENCE pass only — a
	// positive hit proves a landing wherever it is found — and is deliberately
	// NOT the absence reading: absence is settled by a full-history search
	// (mergeVerifyGitLogGrepArgs), because a window says nothing about the
	// commits it did not reach (Rule 1: one place to find and change).
	mergeVerifyGitLogWindow = 2000

	// mergeVerifyGitGrepMatchCap bounds the records the FULL-HISTORY landing
	// search prints. The search answers a hit/no-hit question (git logs
	// newest-first, so the first records are the newest landings), so the cap
	// is a bound on the evidence, never a narrowing of the question: absence is
	// read from an EMPTY output, which a cap cannot fake.
	mergeVerifyGitGrepMatchCap = 50

	// mergeVerifyGitLogWindowTestNarrow is the window a test narrows to when it
	// must place a seeded landing OUTSIDE it (see mergeVerifyLogWindowForTest).
	mergeVerifyGitLogWindowTestNarrow = 2

	// mergeVerifyGrepLinePrefix is the regex anchor that keeps the full-history
	// landing search on a message LINE START (git applies --grep per line of
	// the message). Without it, a commit whose BODY merely mentions a merge
	// subject would be read as a landing — the same prose-vs-fact confusion the
	// parser refuses (Rule 15: a resemblance is not a witness).
	mergeVerifyGrepLinePrefix = "^"

	// mergeVerifyLandingParenOpen is the " (" separator an adoption subject
	// carries between the branch and its sha ("adoption: merge <b> (<sha>)") —
	// named once and shared by the parser and the full-history pattern builder
	// (Rule 1b/13: the two must never drift apart).
	mergeVerifyLandingParenOpen = " ("

	// mergeVerifyGitGrepRegexFlag puts git's --grep into EXTENDED regex mode,
	// which is what regexp.QuoteMeta's escaping assumes: in git's default
	// (BRE) mode a quoted "(" becomes a GROUP OPEN, so the adoption pattern
	// fails to compile and the whole search is ERR 128 — measured as
	// `fatal: command line, '^adoption: merge … \(': Unmatched ( or \(`.
	mergeVerifyGitGrepRegexFlag = "-E"
)

// The landing-subject BUILDERS: one function per form the project's writers
// produce, derived from the SAME constants the parser matches (Rule 14 — a test
// or a writer that re-types a subject shape would be a second source of truth
// for the fact the guard is deciding on).
func mergeVerifyGitMergeSubject(branch string) string {
	return mergeVerifyGitMergeSubjectPrefix + mergeVerifyGitMergeSubjectQuote + branch + mergeVerifyGitMergeSubjectQuote
}

func mergeVerifyGitRemoteMergeSubject(branch string) string {
	return mergeVerifyGitMergeSubjectRemote + mergeVerifyGitMergeSubjectQuote + branch + mergeVerifyGitMergeSubjectQuote
}

func mergeVerifyResolveMergeSubject(branch string) string {
	return mergeVerifyResolveMergePrefix + branch + mergeVerifyResolveMergeSuffix
}

// mergeVerifyMergeBacklogSubject is the merge-backlog EXECUTOR's merge subject
// for branch, derived from the executor's OWN message constant: the witness and
// the writer share one spelling (Rule 14).
func mergeVerifyMergeBacklogSubject(branch string) string {
	return fmt.Sprintf(mergeExecMergeMsg, branch)
}

// mergeVerifyMergeBacklogSubjects is the executor form's GREP candidate set: its
// commit names the branch as the full ref it merges (`refs/heads/<b>`), while a
// hand-written subject may name the bare branch — and --grep matches the
// SUBJECT TEXT, so the SEARCH must carry both spellings even though the PARSER
// reduces either one to the same branch. The bare spelling is skipped when the
// branch already carries it (no duplicate pattern).
func mergeVerifyMergeBacklogSubjects(branch string) []string {
	bare := mergeVerifyLandingTokenNormalize(branch)
	full := mergeVerifyLocalRefPrefix + bare
	out := []string{mergeVerifyMergeBacklogSubject(bare)}
	if full != bare {
		out = append(out, mergeVerifyMergeBacklogSubject(full))
	}
	return out
}

// mergeVerifyLandingTokenNormalize reduces a landing token's REF SPELLING to
// the branch name a row carries: the executor's full-ref target loses its
// refs/heads/ prefix, and either end may carry a stray separator (the
// executor's own spelling or a hand-written merge). Only the SPELLING is
// reduced — the caller still compares the result by EQUALITY (and the
// separator-insensitive arm still compares name keys), so a normalization can
// never turn a different branch into a match (Rule 15: a resemblance is not a
// witness).
func mergeVerifyLandingTokenNormalize(tok string) string {
	tok = strings.TrimSpace(tok)
	tok = strings.TrimPrefix(tok, mergeVerifyLocalRefPrefix)
	tok = strings.Trim(tok, mergeVerifyRefSep)
	return strings.TrimSpace(tok)
}

func mergeVerifyAdoptionSubject(branch, sha string) string {
	return adoptionSummaryPrefix + branch + " (" + sha + ")"
}

// mergeVerifyAdoptionSubjectPrefix is the same subject WITHOUT its sha tail —
// the shape the full-history search can match before the sha is known.
func mergeVerifyAdoptionSubjectPrefix(branch string) string {
	return adoptionSummaryPrefix + branch + mergeVerifyLandingParenOpen
}

// mergeVerifyLogWindow is the number of records the windowed landing scan reads
// off the integration branch (the POSITIVE arm's bound). One place resolves the
// value (Rule 2); a test that must place a landing OUTSIDE the window narrows it
// through mergeVerifyLogWindowForTest.
func mergeVerifyLogWindow() int {
	if mergeVerifyLogWindowForTest > 0 {
		return mergeVerifyLogWindowForTest
	}
	return mergeVerifyGitLogWindow
}

// mergeVerifyLogWindowForTest narrows the window in tests only (0 = the
// production constant). It exists because the ABSENCE arm's soundness is a
// property of the FULL-HISTORY search: the only way to prove the guard still
// finds a landing the window did not reach is to shrink the window below a
// seeded landing, and the real repo makes that unaffordable (a fixture would
// need >2000 commits).
var mergeVerifyLogWindowForTest int

// The guard's verdict vocabulary (Rule 1b/14: one spelling per fact, exported
// so the tests and the CLI assert on the same strings the guard writes).
const (
	// MergeVerifyStaleVerdictAlreadyLanded: the branch tip IS an ancestor of
	// the integration branch — the fix is on master, the rework is a no-op.
	MergeVerifyStaleVerdictAlreadyLanded = "already-landed"

	// MergeVerifyStaleVerdictBranchNotFound: the ref does not resolve — the
	// briefing orders the executor to check out a ref that does not exist (the
	// measured typo case: `fix/tasklist-inprogress-scan` for the real
	// `fix/task-list-inprogress-scan`). A HARD claim-time refusal.
	//
	// Why an unresolvable ref can never be legitimate rework HERE: the lane's
	// OWN unwind path preserves the work first — MergeVerifyPreserveBranch
	// re-creates the branch ref at the merged tip BEFORE the revert — so a
	// genuine rework artifact always resolves. An unresolvable ref is therefore
	// either a briefing typo or a branch the merged-cleanup sweep already
	// deleted (which `git branch -d` only does for an already-merged tip ⇒ the
	// "already-landed" arm above, seen through the ref probe instead). Both are
	// no-ops, and the adoption-attestation scan below says WHICH one it is.
	MergeVerifyStaleVerdictBranchNotFound = "branch-not-found"

	// MergeVerifyStaleVerdictLanded: the ref is gone AND master's own history
	// carries this branch's LANDING commit (one of the merger's own forms —
	// see mergeVerifyStaleLandingToken). This is the case D8 measured: the
	// merger DELETES a branch precisely when its merge succeeded, so "ref
	// gone" is the normal post-merge state, and a guard that reads it as a
	// broken brief records a SUCCESS as a refusal. The row is closed as the
	// LANDED disposition, never as a loss.
	MergeVerifyStaleVerdictLanded = "landed"

	// MergeVerifyStaleVerdictBranchGoneUnproved: the ref is gone and NO
	// landing is provable by a FULL-HISTORY search over master. That qualifier
	// is load-bearing: absence is a property of a search that walked the whole
	// history, never of a record window — a search that could not run, or a
	// matching record that did not parse, is not evidence of absence and stays
	// MergeVerifyStaleVerdictNotJudged (never closed).
	//
	// The refusal is the CORRECT reading of exactly two states, both of which
	// leave nothing to fix: a branch name the briefing misspelled (the ref was
	// never created) and a ref whose commit is unreachable from master. They
	// are indistinguishable from a scan alone, which is why the text says so
	// instead of asserting the typo.
	MergeVerifyStaleVerdictBranchGoneUnproved = "branch-gone-unproved"

	// MergeVerifyStaleVerdictPremiseOpen: the premise MEASURED true — the
	// branch resolves and its tip is NOT on the integration branch, so the
	// forward rework is real and owed. This verdict is the guard's "dispatch
	// it" signal; the guard must NEVER cancel a row that carries it (a
	// genuinely unlanded revert keeps its worker).
	MergeVerifyStaleVerdictPremiseOpen = "premise-open"

	// MergeVerifyStaleVerdictNotJudged: the guard could not measure the
	// premise (no branch/repo recorded, git missing, probe timed out). The row
	// stays claimable — an unmeasurable premise is never closed as stale.
	MergeVerifyStaleVerdictNotJudged = "not-judged"

	// MergeVerifyStaleNoOpMarker is the durable grep marker of a no-op close;
	// it heads the row's summary so `task list` shows WHY the row ended.
	MergeVerifyStaleNoOpMarker = "[merge-verify staleness guard]"

	// MergeVerifyStaleNoOpReasonPrefix heads the row's `reason` column — the
	// operator-visible one-liner naming the verdict and the measured facts.
	MergeVerifyStaleNoOpReasonPrefix = "merge-verify staleness guard — no forward rework: "

	// MergeVerifyStaleLandedReasonPrefix heads the reason column of a row the
	// guard closed because the work is PROVABLY on the integration branch
	// (verdict landed). It is a distinct spelling from the no-op prefix on
	// purpose: an operator grepping the guard's trail must be able to tell
	// "this row was already delivered" from "this row's premise evaporated".
	MergeVerifyStaleLandedReasonPrefix = "merge-verify staleness guard — work had LANDED: "

	// MergeVerifyStaleEvent is the notify event fired when the guard closes a
	// stale row (same side-channel as build-failed / merge-reverted /
	// merge-verify, so an operator subscribed to GOPI_TASK_NOTIFY_EVENTS sees
	// the refusal without reading logs).
	MergeVerifyStaleEvent = "merge-verify-stale-rework"

	// MergeVerifyStaleLandedEvent is the notify event for the LANDED
	// disposition (see MergeVerifyStaleVerdictLanded). Distinct from the
	// refusal event because the two are opposite facts about the same row
	// shape, and an operator who subscribed to be told about losses must not
	// have to parse a sentence to find out there was none.
	MergeVerifyStaleLandedEvent = "merge-verify-stale-landed"

	// mergeVerifyStaleGuardHolder is the claimed_by stamp the guard writes when
	// it advances a landed row through claim → start → completed. It names the
	// GUARD as the closer (never the operator's pid): a reader of the claim
	// trail must be able to see that no worker ran this row.
	mergeVerifyStaleGuardHolder = "merge-verify-staleness-guard"
)

// MergeVerifyStaleVerdict is the dispatch-time judgement of ONE rework row's
// premise — the measurement, its evidence, and the verdict built from them.
type MergeVerifyStaleVerdict struct {
	TaskID       string // the judged row
	Repo         string // the row's deliverable repo ("" = not recorded)
	Branch       string // the row's deliverable branch ("" = not recorded)
	Master       string // the integration branch probed (never empty when judged)
	Tip          string // the branch tip when the ref resolved
	Landed       string // the adoption commit sha attesting a re-landing, when one exists
	LandedAt     string // that commit's subject (audit copy)
	LandedBranch string // the branch token that landing names (a spelling divergence from Branch)
	Evidence     string // the commands run + their measured results, in order
	Skip         bool   // true = no branch/repo to judge (the row is not lane work)
	Measured     bool   // true = the judgement rests on real git measurements
	Stale        bool   // true = the premise is ALREADY false ⇒ close as a no-op
	Verdict      string // one of the MergeVerifyStaleVerdict* constants
}

// mergeVerifyStaleTipHint shortens a sha for the human-facing text (a 40-char
// hex dump in a task summary is noise; the reason column still carries the full
// sha as evidence).
func mergeVerifyStaleTipHint(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= mergeVerifyStaleTipHintLen {
		return sha
	}
	return sha[:mergeVerifyStaleTipHintLen]
}

// mergeVerifyStaleGit runs ONE bounded git command in repo and reports its
// trimmed stdout, its exit status and a stderr tail. It never fails the
// caller: every failure mode (missing git, timeout, non-zero exit) comes back
// as data so the caller can RECORD it (Rule 15 — a swallowed git error must
// never read as a clean measurement).
func mergeVerifyStaleGit(repo string, args ...string) (stdout string, exit int, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(mergeVerifyGitTimeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	detail = strings.TrimSpace(stderr.String())
	if len(detail) > mergeVerifyStaleEvidenceTail {
		detail = detail[:mergeVerifyStaleEvidenceTail]
	}
	if err == nil {
		return strings.TrimSpace(string(out)), 0, detail
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return strings.TrimSpace(string(out)), ee.ExitCode(), detail
	}
	// Not an exit error: git is missing, or the deadline fired. -1 is the
	// guard's "no exit status" sentinel — the caller records the cause text.
	if detail == "" {
		detail = err.Error()
	}
	return strings.TrimSpace(string(out)), -1, detail
}

// mergeVerifyStaleResolveRef reports whether `refs/heads/<branch>` resolves in
// repo, and its 40-char tip when it does. A git that cannot run at all reports
// measured=false (the premise is unmeasured, never "missing").
func mergeVerifyStaleResolveRef(repo, branch string) (tip string, measured bool, evidence string) {
	out, exit, detail := mergeVerifyStaleGit(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	evidence = fmt.Sprintf("git rev-parse --verify --quiet refs/heads/%s ⇒ exit=%d %s", branch, exit, detail)
	switch {
	case exit == 0:
		// rev-parse prints exactly one line for a single ref; take the first
		// field so a trailing newline/whitespace can never reach a git argv.
		return strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]), true, evidence
	case exit == mergeVerifyGitExitNo:
		// `--quiet` turns "no such ref" into exit 1: a real measurement of
		// ABSENCE (git's documented --quiet contract for rev-parse --verify).
		return "", true, evidence
	case exit == mergeVerifyGitExitFatal:
		// The fatal arm: git rejected the SPELLING (e.g. a branch name it
		// cannot parse) — it did not answer whether the ref exists, so the
		// premise is unmeasured, never "missing".
		return "", false, evidence
	default:
		// Any other status (git absent, timeout, a wedged repo): the guard
		// must not read it as absence. Recorded, and the row stays claimable.
		return "", false, evidence
	}
}

// mergeVerifyStaleIsAncestor answers `git merge-base --is-ancestor <tip>
// <master>`: exit 0 = yes, exit 1 = no, anything else = unmeasured. The
// exit-1 case is git's documented "no" (task_merge.go's isGitExit1 precedent).
func mergeVerifyStaleIsAncestor(repo, tip, master string) (isAncestor bool, measured bool, evidence string) {
	_, exit, detail := mergeVerifyStaleGit(repo, "merge-base", "--is-ancestor", tip, master)
	evidence = fmt.Sprintf("git merge-base --is-ancestor %s %s ⇒ exit=%d %s", mergeVerifyStaleTipHint(tip), master, exit, detail)
	switch exit {
	case 0:
		return true, true, evidence
	case mergeVerifyGitExitNo:
		return false, true, evidence
	}
	return false, false, evidence
}

// MergeVerifyStaleNameKey normalizes a branch name for the SEPARATOR-INSENSITIVE
// match: lowercase, with "-" and "_" folded away. The measured case (row
// 01a09e9b) is exactly this class of divergence - the briefing's deliverable
// named "fix/tasklist-inprogress-scan" while the ref that really landed was
// "fix/task-list-inprogress-scan" (one dropped hyphen), so an EXACT-name scan
// for the row's spelling finds nothing and the operator gets no pointer to the
// commit that already shipped the fix.
//
// The key is deliberately conservative: only separators are folded, so two
// genuinely different branches can only collide when they differ by separators
// alone. It is used to attach EVIDENCE to a row that is already refused (the
// ref did not resolve) - never to turn a claimable row into a cancelled one -
// so a false attribution cannot block real work.
func MergeVerifyStaleNameKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	return name
}

// mergeVerifyStaleAdoptionCommit scans the integration branch's own history for
// a LANDING of this row's branch, through EVERY form the project's writers
// produce:
//
//	Merge branch '<b>'                        (git's own merge commit — the
//	                                           ordinary merger: task_merge.go's
//	                                           `git merge <b>`, repo_sweep.go's
//	                                           `--ff-only`, the executor's
//	                                           --no-commit + commit)
//	Merge remote-tracking branch '<b>'        (the remote-tracking spelling of
//	                                           the same git subject)
//	resolve merge <b> into master             (the conflict-resolution landing,
//	                                           user/worker authored: 68 on this
//	                                           repo's master at the fix's
//	                                           baseline)
//	adoption: merge <b> (<sha>)               (this lane's resolver)
//
// It returns the full sha, the subject and the BRANCH TOKEN the landing names
// ("" = none). The token matters: when it differs from the row's branch, the
// landing is the row's work recorded under another ref spelling - the measured
// typo case, made visible instead of silently unfound.
//
// This is the ONLY witness available once the merged-cleanup sweep has deleted
// the branch ref: a git fact about master, not a lease on a prose briefing.
//
// ONE pass over the newest window records, all commits — the merge forms and
// the resolve/adoption forms are read from the same stream (a `--merges`-only
// pass would miss the single-parent landings, and `-n` counts RECORDS, so the
// two passes would cover different spans anyway).
//
// The ABSENCE arm needs a FULL-HISTORY search: a positive hit proves a landing
// wherever it is found, but an empty WINDOWED scan proves nothing about the
// commits the window did not reach. The measured repo makes that concrete —
// master carries 3714 commits against a 2000-record window, so a windowed
// absence reading would be unsound on the very history this guard runs against
// (and the refusal arm would be unreachable in production, i.e. untested code
// on the path that decides whether to close a row).
//
// So the scan is two arm-appropriate readings:
//
//	POSITIVE (rich evidence): one windowed pass parsed for every landing form,
//	  which also yields the SEPARATOR-INSENSITIVE match (the measured typo case).
//	ABSENCE (sound): one `git log --grep` pass over the WHOLE of master, whose
//	  patterns are the same named landing forms anchored at a message-line
//	  start. git walks all of master to answer it, so an EMPTY output is a proof
//	  of absence rather than a statement about a window, and a failure to run is
//	  `not-judged` — never a refusal.
func mergeVerifyStaleAdoptionCommit(repo, branch, master string) (sha, subject, matchedBranch string, measured bool, evidence string) {
	window := mergeVerifyLogWindow()
	out, exit, detail := mergeVerifyStaleGit(repo, "log", mergeVerifyLogFormat,
		"-n", strconv.Itoa(window), master)
	evidence = fmt.Sprintf("git log %s (n=%d, %s) -> exit=%d %s",
		master, window, mergeVerifyLogFormat, exit, detail)
	if exit != 0 {
		return "", "", "", false, evidence
	}
	if hitSha, hitSubj, hitToken, ok := mergeVerifyStaleLandingIn(out, branch); ok {
		return hitSha, hitSubj, hitToken, true,
			evidence + fmt.Sprintf(" | landing proved on %s: %s %q", master, hitSha, hitSubj)
	}
	// No landing in the window: settle ABSENCE against the whole history.
	hit, hitExit, hitDetail := mergeVerifyStaleGit(repo,
		mergeVerifyGitLogGrepArgs(branch, master, mergeVerifyLogFormat)...)
	evidence += fmt.Sprintf(" | git log --grep (all history, %d pattern(s)) -> exit=%d %s",
		mergeVerifyLandingPatternCount(), hitExit, hitDetail)
	if hitExit != 0 {
		// A pattern git could not evaluate (a branch name that is not a valid
		// regex once quoted) is UNMEASURED: the row stays claimable.
		return "", "", "", false, evidence + " | landing search did not run — absence NOT proved"
	}
	if hitSha, hitSubj, hitToken, ok := mergeVerifyStaleLandingIn(hit, branch); ok {
		return hitSha, hitSubj, hitToken, true,
			evidence + fmt.Sprintf(" | landing proved on %s: %s %q (outside the newest %d records)",
				master, hitSha, hitSubj, window)
	}
	if strings.TrimSpace(hit) != "" {
		// A record matched a landing pattern but did not parse as one: a shape
		// the guard does not understand is UNMEASURED (Rule 15 — an unreadable
		// answer is not the same fact as a negative one).
		return "", "", "", false, evidence + " | a matching record did not parse as a landing — absence NOT proved"
	}
	return "", "", "", true, evidence + fmt.Sprintf(
		" | no landing form anywhere on %s (the search walked the whole history)", master)
}

// mergeVerifyGitLogGrepArgs builds the full-history landing search: one
// `git log` over master with a --grep pattern per landing form, each quoting
// the ROW's branch and anchored at a message-LINE START (mergeVerifyGrepLine
// Prefix). The patterns are REs (the anchor needs one), so the branch is
// regexp-quoted: a name carrying a metacharacter must not become a pattern that
// matches something else (git then reports a pattern it cannot evaluate, which
// the caller reads as unmeasured — never as absence).
func mergeVerifyGitLogGrepArgs(branch, master, format string) []string {
	args := []string{"log", format, "-n", strconv.Itoa(mergeVerifyGitGrepMatchCap),
		mergeVerifyGitGrepRegexFlag}
	for _, subject := range mergeVerifyLandingSubjectCandidates(branch) {
		args = append(args, "--grep",
			mergeVerifyGrepLinePrefix+regexp.QuoteMeta(subject))
	}
	return append(args, master)
}

// mergeVerifyLandingSubjectCandidates is the SET of subject shapes a landing for
// branch can carry — every form the project's writers produce, built from the
// same constants the parser matches (Rule 14: one source of truth for the fact).
// The count is what the evidence line reports as the search's coverage.
func mergeVerifyLandingSubjectCandidates(branch string) []string {
	return append([]string{
		mergeVerifyGitMergeSubject(branch),
		mergeVerifyGitRemoteMergeSubject(branch),
		mergeVerifyResolveMergeSubject(branch),
		mergeVerifyAdoptionSubjectPrefix(branch),
	}, mergeVerifyMergeBacklogSubjects(branch)...)
}

// mergeVerifyLandingPatternCount is the number of landing forms the full-history
// search covers — reported as evidence so a reader sees the search's breadth.
func mergeVerifyLandingPatternCount() int { return len(mergeVerifyLandingSubjectCandidates("")) }

// mergeVerifyStaleLandingIn finds the landing for branch in one `git log`
// record stream (see mergeVerifyLogFormat), preferring an EXACT branch-token
// match over the separator-insensitive one. git logs newest-first, so the first
// hit of each kind is the latest landing.
func mergeVerifyStaleLandingIn(out, branch string) (sha, subject, matchedBranch string, ok bool) {
	wantKey := MergeVerifyStaleNameKey(branch)
	var exactSha, exactSubj, exactBranch string
	var fuzzySha, fuzzySubj, fuzzyBranch string
	for _, rec := range strings.Split(out, mergeVerifyLogRecordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		fields := strings.SplitN(rec, mergeVerifyLogSubjectFieldSep, 2)
		if len(fields) != 2 {
			continue
		}
		curSha := strings.TrimSpace(fields[0])
		subj := fields[1]
		if i := strings.IndexByte(subj, '\n'); i >= 0 {
			subj = subj[:i]
		}
		subj = strings.TrimSpace(subj)
		token, isLanding := mergeVerifyStaleLandingToken(subj)
		if !isLanding || curSha == "" {
			continue
		}
		// One normalization for BOTH arms, at the single comparison site: the
		// landing forms spell the same branch differently (a bare name, the
		// executor's full refs/heads/ ref), and an unreduced spelling would
		// silently miss the row's branch. The comparison itself stays an
		// EQUALITY (and the fuzzy arm a name-key equality) — only the spelling
		// is reduced, so a DIFFERENT branch can never be matched.
		token = mergeVerifyLandingTokenNormalize(token)
		if token == branch {
			if exactSha == "" {
				exactSha, exactSubj, exactBranch = curSha, subj, token
			}
			continue
		}
		// The separator-insensitive arm, newest-first: git logs newest first,
		// so the FIRST fuzzy hit is the latest landing. Both arms compare the
		// token with the row's branch out of the SAME ref spelling (the landing
		// forms name a branch differently: a bare name, a full refs/heads/ ref)
		// — the equality below is what keeps the normalization honest.
		if exactSha == "" && fuzzySha == "" && wantKey != "" && MergeVerifyStaleNameKey(mergeVerifyLandingTokenNormalize(token)) == wantKey {
			fuzzySha, fuzzySubj, fuzzyBranch = curSha, subj, mergeVerifyLandingTokenNormalize(token)
		}
	}
	switch {
	case exactSha != "":
		return exactSha, exactSubj, exactBranch, true
	case fuzzySha != "":
		return fuzzySha, fuzzySubj, fuzzyBranch, true
	}
	return "", "", "", false
}

// mergeVerifyStaleLandingToken extracts the branch a landing subject names, for
// every landing form the project writes. Each form is matched on its OWN named
// constant (Rule 1b/13) — never a retyped literal:
//
//	"Merge branch '<b>'"                 -> git's local merge subject
//	"Merge remote-tracking branch '<b>'" -> git's remote-tracking merge subject
//	"resolve merge <b> into master"      -> the hand/worker conflict resolution
//	"adoption: merge <b> (<sha>)"        -> this lane's resolver
//	"merge-backlog: merge <b>"           -> the merge-backlog executor
//
// A subject that matches no form is not a landing witness (niladic: nothing is
// inferred from a sentence that merely resembles one).
func mergeVerifyStaleLandingToken(subject string) (branch string, ok bool) {
	switch {
	case strings.HasPrefix(subject, mergeVerifyGitMergeSubjectRemote):
		return mergeVerifyQuotedToken(subject, mergeVerifyGitMergeSubjectRemote)
	case strings.HasPrefix(subject, mergeVerifyGitMergeSubjectPrefix):
		return mergeVerifyQuotedToken(subject, mergeVerifyGitMergeSubjectPrefix)
	case mergeVerifyResolveMergeToken(subject) != "":
		return mergeVerifyResolveMergeToken(subject), true
	case strings.HasPrefix(subject, mergeVerifyMergeBacklogPrefix):
		// The merge-backlog executor's own subject. Its token is the FULL ref
		// it merges (`refs/heads/<b>`, mergeExecMergeMsg's branch argument), so
		// it is reduced to the branch name where tokens are COMPARED
		// (mergeVerifyStaleLandingIn) — one place, so no caller sees a ref
		// spelling it would have to remember to reduce.
		tok := strings.TrimSpace(strings.TrimPrefix(subject, mergeVerifyMergeBacklogPrefix))
		if tok == "" || strings.ContainsAny(tok, " \t") {
			return "", false
		}
		return tok, true
	}
	return mergeVerifyStaleAdoptionToken(subject)
}

// mergeVerifyQuotedToken reads the branch between the quote pair that follows
// prefix in a git merge subject ("Merge branch 'x'" -> "x"). The subject must
// END at the closing quote: git may append a message body's first line to the
// subject via %s-like formatting, and a token taken from the middle of prose
// would be a guess. An empty or unterminated name is not a witness.
func mergeVerifyQuotedToken(subject, prefix string) (branch string, ok bool) {
	rest := strings.TrimPrefix(subject, prefix)
	if !strings.HasPrefix(rest, mergeVerifyGitMergeSubjectQuote) {
		return "", false
	}
	rest = strings.TrimPrefix(rest, mergeVerifyGitMergeSubjectQuote)
	end := strings.Index(rest, mergeVerifyGitMergeSubjectQuote)
	if end <= 0 {
		return "", false
	}
	branch = strings.TrimSpace(rest[:end])
	// A TAIL after the closing quote is accepted, deliberately: master carries
	// subjects like "Merge branch 'fix/a' (a0bc3e11) into fix/other" — a real
	// merge of the branch, annotated by its author — and the commit is
	// reachable from master like any other, so the branch's work IS on master.
	// The token is what the guard decides on; the tail is commentary.
	if branch == "" || strings.ContainsAny(branch, " \t") {
		return "", false
	}
	return branch, true
}

// mergeVerifyResolveMergeToken reads the conflict-resolution landing form
// "resolve merge <b> into master" -> "<b>". The subject is USER/AUTHORED prose,
// so the match is anchored at BOTH ends: the prefix `resolve merge ` AND the
// suffix ` into master` must both hold, which keeps "resolve merge conflict: …
// into master" (a ledger summary, not a commit) and any other sentence out.
func mergeVerifyResolveMergeToken(subject string) string {
	prefix, suffix := mergeVerifyResolveMergePrefix, mergeVerifyResolveMergeSuffix
	if !strings.HasPrefix(subject, prefix) || !strings.HasSuffix(subject, suffix) {
		return ""
	}
	branch := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(subject, prefix), suffix))
	if branch == "" || strings.ContainsAny(branch, " \t") {
		return ""
	}
	return branch
}

// mergeVerifyStaleAdoptionToken extracts the branch a landing subject names:
// "adoption: merge <branch> (<sha...>)" -> "<branch>". The two subject forms the
// resolver writes both parse: the plain "(<sha>)" form and the "<sha>, ref
// kept" form (adoptionRefKeptMarker). A subject without the prefix, without the
// " (" separator, or without a closing paren is not a landing witness.
func mergeVerifyStaleAdoptionToken(subject string) (branch string, ok bool) {
	if !strings.HasPrefix(subject, adoptionSummaryPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(subject, adoptionSummaryPrefix)
	if !strings.HasSuffix(rest, ")") {
		return "", false
	}
	i := strings.LastIndex(rest, mergeVerifyLandingParenOpen)
	if i <= 0 {
		return "", false
	}
	branch = strings.TrimSpace(rest[:i])
	if branch == "" {
		return "", false
	}
	return branch, true
}

// mergeVerifyStaleRowBranch reads a row's recorded (repo, branch) deliverable
// pair. An unreadable/absent stamp is a DECLINED judgement, never a stale one.
func mergeVerifyStaleRowBranch(t *Task) (repo, branch string) {
	if t == nil || strings.TrimSpace(t.Deliverable) == "" {
		return "", ""
	}
	var d map[string]string
	if err := json.Unmarshal([]byte(t.Deliverable), &d); err != nil {
		// The stamp is not ours (a legacy/foreign deliverable shape): the
		// premise cannot be read, which is a DECLINED judgement — never a
		// stale one (Rule 15: unreadable is not the same fact as false).
		taskNotifyLog("merge-verify staleness guard: row %s deliverable is not a readable stamp: %v", t.TaskID, err)
		return "", ""
	}
	return strings.TrimSpace(d["repo"]), strings.TrimSpace(d[mergeVerifyReworkBranchKey])
}

// TaskIsMergeVerifyReworkRow reports whether a row belongs to the merge-verify
// REWORK lane: the gate files these rows with tags `merge-verify,branch-rework`
// AND the deterministic summary prefix, so lane membership is judged on what the
// gate itself wrote (MergeVerifyFileRework).
//
// Why the prefix test as well as the tags: the rework lane is exactly "a
// FORWARD rework of a branch whose merge was reverted", and the tag pair is the
// other half of the same stamp (mergeVerifyReworkSummaryPrefix + o.Branch,
// verified by the gate's own tests). Requiring both keeps the guard off any
// future row that carries the tag pair for another purpose, and it is a purely
// NEGATIVE check — a row the guard does not recognise is left claimable, so a
// stricter predicate can never block a legitimate rework.
func TaskIsMergeVerifyReworkRow(t *Task) bool {
	if t == nil {
		return false
	}
	tags := strings.Split(t.Tags, ",")
	if !tagListHas(tags, MergeVerifyEvent) || !tagListHas(tags, mergeVerifyReworkTag) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(t.Summary), mergeVerifyReworkSummaryPrefix)
}

// tagListHas is the lane-membership test both gate predicates share (the tags
// column is a comma list; an exact element match, never a substring — a tag
// named `branch-rework-retry` must not read as `branch-rework`).
func tagListHas(tags []string, want string) bool {
	for _, tg := range tags {
		if strings.TrimSpace(tg) == want {
			return true
		}
	}
	return false
}

// MergeVerifyStaleReworkJudge judges ONE row: it reads the row's deliverable
// (repo + branch), resolves the branch ref, probes ancestry against the
// integration branch and scans for the adoption landing. Read-only and
// parallel-safe — the caller decides what to do with the verdict.
func MergeVerifyStaleReworkJudge(taskID string) (MergeVerifyStaleVerdict, error) {
	taskID = taskIDNormalize(taskID)
	v := MergeVerifyStaleVerdict{TaskID: taskID, Verdict: MergeVerifyStaleVerdictNotJudged}
	if taskID == "" {
		return v, fmt.Errorf("merge-verify staleness guard: empty task id")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return v, fmt.Errorf("merge-verify staleness guard: open ledger: %w", err)
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return v, fmt.Errorf("merge-verify staleness guard: read row %s: %w", taskID, err)
	}
	if t == nil {
		return v, fmt.Errorf("merge-verify staleness guard: no row %s", taskID)
	}
	return mergeVerifyStaleJudgeRow(t), nil
}

// mergeVerifyStaleJudgeRow is the judgement body (no I/O of its own beyond the
// bounded git probes), shared by the read-only entry and the claim pump.
func mergeVerifyStaleJudgeRow(t *Task) MergeVerifyStaleVerdict {
	v := MergeVerifyStaleVerdict{Verdict: MergeVerifyStaleVerdictNotJudged}
	if t == nil {
		return v
	}
	v.TaskID = t.TaskID
	repo, branch := mergeVerifyStaleRowBranch(t)
	v.Repo, v.Branch = repo, branch
	if repo == "" || branch == "" {
		v.Skip = true // not lane work: nothing recorded to judge
		v.Evidence = fmt.Sprintf("row %s records repo=%q branch=%q — not a promise that can go stale",
			t.TaskID, repo, branch)
		return v
	}
	var ev []string
	addEv := func(parts ...string) {
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				ev = append(ev, p)
			}
		}
	}
	// The ref the probe asks is RESOLVED, not assumed: on a client whose local
	// integration branch trails the peer (measured 2026-10-12, task 01a12d30 —
	// `refs/heads/master` was 62 commits behind the delivery lane's mirror), the
	// two refs answer "is this tip landed?" differently, and asking the stale one
	// makes this guard judge a LANDED branch as not-landed.
	// IntegrationClaimTargetRef resolves which ref carries the delivery's own
	// state: the mirror when it is strictly ahead, else the local branch (the
	// pre-fix answer). The choice is MEASURED in this repo, never assumed from a
	// name, and the measurement rides the evidence so a reader sees which ref the
	// verdict is about.
	master, claimLookahead := integrationClaimTargetRefAndLookahead(repo)
	if claimLookahead > 0 {
		addEv(fmt.Sprintf("claim ref %s (the local %s trails it by %d commit(s))",
			master, integrationClaimLocalRef, claimLookahead))
	}
	v.Master = master

	tip, refMeasured, refEv := mergeVerifyStaleResolveRef(repo, branch)
	addEv(refEv)
	v.Tip = tip
	switch {
	case !refMeasured:
		// git could not run in this repo: unmeasured, row stays claimable.
		v.Evidence = strings.Join(ev, " | ")
		return v
	case tip != "":
		ancestor, ancMeasured, ancEv := mergeVerifyStaleIsAncestor(repo, tip, master)
		addEv(ancEv)
		if ancMeasured && ancestor {
			// The fix is ON the integration branch: the premise is already
			// false. (The branch ref surviving here is the ref-kept case:
			// merged-cleanup deletes it once its worktree is gone.)
			v.Measured, v.Stale = true, true
			v.Verdict = MergeVerifyStaleVerdictAlreadyLanded
		} else if ancMeasured {
			// The tip is NOT on master: the rework is genuinely owed. The
			// adoption scan below is EVIDENCE only — a landing that was unwound
			// looks exactly like this, and the briefing's premise holds.
			v.Measured = true
			v.Verdict = MergeVerifyStaleVerdictPremiseOpen
			sha, subj, token, _, _ := mergeVerifyStaleAdoptionCommit(repo, branch, master)
			if sha != "" {
				v.Landed, v.LandedAt, v.LandedBranch = sha, subj, token
				addEv(fmt.Sprintf("earlier landing on %s: %s %q (branch %q; now unwound — the premise stands)",
					master, mergeVerifyStaleTipHint(sha), subj, token))
			}
		}
		v.Evidence = strings.Join(ev, " | ")
		return v
	}

	// The ref is gone. That state ALONE proves nothing: the merger DELETES a
	// branch precisely when its merge succeeded (`git branch -d` only ever
	// succeeds for an already-merged tip), so "ref gone" is the NORMAL
	// post-merge state. The measured case (rows 01a0a59a / 01a0a9e0) is
	// exactly this reading taken as a refusal: both branches were merged on
	// master — `51edd8d9 Merge branch 'feat/web-lan-access'`,
	// `15060a5c resolve merge feat/merger-status-log into master` — and the
	// guard closed both rows as "branch-not-found" anyway, recording a
	// SUCCESS as a failure.
	//
	// So the judgement moves to master's OWN history: does a landing for this
	// branch exist there, in ANY of the forms the project's writers produce?
	// Proved ⇒ LANDED (the work shipped; the row is closed as such, never as
	// a loss). Not proved within an UNTRUNCATED window ⇒ the ref really names
	// nothing (a briefing typo / a branch that never existed) and the refusal
	// stands, reworded. Not proved within a TRUNCATED window ⇒ unmeasured,
	// never a refusal.
	sha, subj, token, landingMeasured, landingEv := mergeVerifyStaleAdoptionCommit(repo, branch, master)
	addEv(landingEv)
	if !landingMeasured {
		v.Evidence = strings.Join(ev, " | ")
		return v
	}
	v.Measured, v.Stale = true, true
	if sha == "" {
		v.Verdict = MergeVerifyStaleVerdictBranchGoneUnproved
		v.Evidence = strings.Join(ev, " | ")
		return v
	}
	v.Landed, v.LandedAt, v.LandedBranch = sha, subj, token
	if token != branch {
		addEv(fmt.Sprintf("the landing subject names %q while the row's branch is %q (a separator divergence; the SAME work)", token, branch))
	}
	addEv(fmt.Sprintf("landing proved on %s: %s %q", master, sha, subj))
	v.Verdict = MergeVerifyStaleVerdictLanded
	v.Evidence = strings.Join(ev, " | ")
	return v
}

// MergeVerifyStaleNoOpText is the message a refused/closed stale row carries:
// the verdict, the measured facts, and the landing commit the operator can look
// at. One builder for every writer (Rule 14) — the summary marker, the reason
// column and the notify line are all this text.
func MergeVerifyStaleNoOpText(v MergeVerifyStaleVerdict) string {
	if !v.Stale {
		return ""
	}
	switch v.Verdict {
	case MergeVerifyStaleVerdictAlreadyLanded:
		return fmt.Sprintf(
			"no-op — the fix is ALREADY on %s: branch %q tip %s is an ancestor of %s. The forward rework is not owed (the briefing was frozen before the re-landing). Nothing to check out, nothing to fix.",
			v.Master, v.Branch, mergeVerifyStaleTipHint(v.Tip), v.Master)
	case MergeVerifyStaleVerdictLanded:
		return fmt.Sprintf(
			"LANDED — branch %q does not resolve in %q because the merger DELETED it after a successful merge (that is what the close-out chain's branch delete means), and %s carries its landing commit %s (%s)%s. The work SHIPPED: the forward rework is not owed, nothing is to be checked out or re-implemented.%s",
			v.Branch, v.Repo, v.Master, v.Landed, v.LandedAt,
			mergeVerifyStaleLandingDivergence(v), mergeVerifyStaleLandedNextStep(v))
	default: // MergeVerifyStaleVerdictBranchGoneUnproved
		return fmt.Sprintf(
			"refusal — %s and %s: no %q, no %q, no %q and no %q commit exists. The scan walked the WHOLE of %s (a --grep pass over every commit, not a window), so a branch ref that was never created and a branch whose landing cannot be found are indistinguishable from here, and nothing is checked out: re-file with the REAL branch name, or close this row.",
			mergeVerifyStaleLandingForms(v), MergeVerifyStaleBranchGoneReason,
			mergeVerifyGitMergeSubject(v.Branch), mergeVerifyResolveMergeSubject(v.Branch),
			mergeVerifyAdoptionSubject(v.Branch, mergeVerifyStaleShaPlaceholder), mergeVerifyGitRemoteMergeSubject(v.Branch),
			v.Master)
	}
}

// mergeVerifyStaleShaPlaceholder stands in for the landing sha inside the
// refusal text's "no <form> commit" listing — the reader needs the SUBJECT
// SHAPE, not a sha that does not exist. Named so the text has no bare literal.
const mergeVerifyStaleShaPlaceholder = "<sha>"

// MergeVerifyStaleBranchGoneReason is the reworded GENUINE-LOSS clause (D8): it
// states the measured fact (the ref is gone AND no landing commit is provable)
// instead of asserting a typo, and it names the two states that are
// indistinguishable from here — a ref never created, and a ref whose commit is
// unreachable from master. Exported so the regression test asserts the same
// spelling the guard writes (Rule 14), never a retyped copy.
const MergeVerifyStaleBranchGoneReason = "the branch ref is GONE and NO landing commit is provable on the integration branch"

// mergeVerifyStaleLandingForms names, in prose, the class of commits the scan
// looked for — the refusal's opening clause.
func mergeVerifyStaleLandingForms(v MergeVerifyStaleVerdict) string {
	return fmt.Sprintf("the branch ref %q is GONE from %q", v.Branch, v.Repo)
}

// mergeVerifyStaleLandedNextStep is the LANDED arm's closing counsel: whether
// the landed state still needs a verdict is the caller's business, and the one
// thing a reworker must not do is re-implement what shipped.
func mergeVerifyStaleLandedNextStep(v MergeVerifyStaleVerdict) string {
	return fmt.Sprintf(" Verify the landed state on %s if a verdict is still wanted; do NOT re-implement the branch.", v.Master)
}

// mergeVerifyStaleLandingDivergence renders the "the row's spelling differs
// from the landing's" note — empty when the landing names the row's branch
// verbatim. The measured case is a dropped hyphen (row 01a09e9b), so this note
// is what tells the operator WHICH ref the landed fix actually belongs to.
func mergeVerifyStaleLandingDivergence(v MergeVerifyStaleVerdict) string {
	if v.LandedBranch == "" || v.LandedBranch == v.Branch {
		return ""
	}
	return fmt.Sprintf(" (the landing names the ref %q — the row's %q differs only by separators)",
		v.LandedBranch, v.Branch)
}

// MergeVerifyStaleNoOpMarkedSummary is the row's summary after a no-op close:
// the marker first (so `task list` and a ledger grep find every refusal
// together), then the original headline — the row keeps its identity.
func MergeVerifyStaleNoOpMarkedSummary(originalSummary string) string {
	return MergeVerifyStaleNoOpMarker + " " + strings.TrimSpace(originalSummary)
}

// mergeVerifyStaleCloseRow closes ONE stale row: a refusal is cancelled, a
// LANDED row is completed (see mergeVerifyStaleCloseLandedRow) — the two
// dispositions are the two opposite facts the guard can measure, and the ledger
// must not spell them the same way. The close carries the verdict text as its
// event detail, the reason column names the verdict, and the operator is
// notified. A failed close is LOGGED with the evidence and the row stays
// claimable — a guard that silently swallowed the failure would re-dispatch the
// stale row while claiming to have stopped it (Rule 15b).
func mergeVerifyStaleCloseRow(t *Task, v MergeVerifyStaleVerdict) (bool, string) {
	if v.Verdict == MergeVerifyStaleVerdictLanded {
		return mergeVerifyStaleCloseLandedRow(t, v)
	}
	// The REASON column is the operator-visible surface (`task list` renders it)
	// and the row's only mutable summary face (bug056: the summary itself is
	// immutable), so the marker rides here; the full no-op text + the raw
	// evidence ride the append-only event trail the same transition writes.
	text := MergeVerifyStaleNoOpText(v) + " | evidence: " + v.Evidence
	reason := MergeVerifyStaleNoOpMarker + " " + MergeVerifyStaleNoOpReasonPrefix + v.Verdict + " (" + v.Evidence + ")"
	if _, err := taskUpdate(t.TaskID, taskStatusCancelled, text, reason); err != nil {
		taskNotifyLog("merge-verify staleness guard: close %s as a no-op FAILED (row stays claimable): %v",
			t.TaskID, err)
		return false, ""
	}
	MergeVerifyNotify(MergeVerifyStaleEvent, v.Repo, v.Branch, reason)
	taskNotifyLog("merge-verify staleness guard: %s refused — verdict=%s evidence=%s",
		t.TaskID, v.Verdict, v.Evidence)
	return true, reason
}

// mergeVerifyStaleCloseLandedRow closes a row whose work is PROVABLY on the
// integration branch: the row is COMPLETED (the ordinary success disposition —
// the deliverable exists), never cancelled, so a reader of the ledger cannot
// mistake a landed fix for an abandoned one.
//
// The path is pending → claimed → in_progress → completed via the SAME exported
// transitions every other closer uses (TaskClaimExplicit / TaskStart /
// TaskUpdateWithResult). That is not ceremony: `pending → completed` is not on
// taskAllowedTransitions, and the intermediate states are what leave the claim
// trail (claimed_by=mergeVerifyStaleGuardHolder, in_progress_at) that records
// WHO closed the row — with no worker ever having run it.
//
// Failure is handled per step, never swallowed (Rule 15b): a step that cannot
// run is logged with the measured landing evidence and the row is left in
// whatever legal state it reached — a LANDED row must never be cancelled as a
// loss just because the guard's own write path was interrupted.
func mergeVerifyStaleCloseLandedRow(t *Task, v MergeVerifyStaleVerdict) (bool, string) {
	text := MergeVerifyStaleNoOpText(v) + " | evidence: " + v.Evidence
	reason := MergeVerifyStaleNoOpMarker + " " + MergeVerifyStaleLandedReasonPrefix + v.Verdict + " (" + v.Evidence + ")"
	stepErr := func(step string, err error) {
		taskNotifyLog("merge-verify staleness guard: %s proved LANDED (%s %s) but the %s step failed (row left as-is): %v",
			t.TaskID, mergeVerifyStaleTipHint(v.Landed), v.LandedAt, step, err)
	}
	if err := TaskClaimExplicit(t.TaskID, mergeVerifyStaleGuardHolder); err != nil {
		stepErr("claim", err)
		return false, ""
	}
	// NO `in_progress` STEP (epic 01a1a87b). This closer exec's nothing: the run
	// it is closing is dead. Writing `in_progress` here asserted a process that
	// does not exist, which is why the row's `claimed` state now goes straight
	// to the terminal verdict (taskAllowedTransitions' claimed→terminal edge).
	if err := TaskStampLockPhase(t.TaskID, TaskLockPhaseBoth); err != nil {
		// The lock record is bookkeeping, not the verdict: a failure is reported
		// on the trail and the close continues (the terminal write below is the
		// operator-facing outcome, and losing it to a stamp failure would be the
		// worse trade).
		taskNotifyLog("merge-verify staleness guard: lock-phase stamp for %s failed (close continues): %v", t.TaskID, err)
	}
	// The terminal write goes through taskUpdate (the shared mutation core)
	// rather than the harness-only TaskUpdateWithResult, because the REASON
	// column is this close's whole operator-facing content: a blank reason
	// resolves to the status default and would erase the landing evidence
	// (Rule 15 — a close whose reason does not say WHY is a silent close).
	if _, err := taskUpdate(t.TaskID, taskStatusCompleted, text, reason); err != nil {
		stepErr("completed", err)
		return false, ""
	}
	MergeVerifyNotify(MergeVerifyStaleLandedEvent, v.Repo, v.Branch, reason)
	taskNotifyLog("merge-verify staleness guard: %s closed LANDED — landing=%s %s evidence=%s",
		t.TaskID, v.Landed, v.LandedAt, v.Evidence)
	return true, reason
}

// mergeVerifySweepStaleRework is the CLAIM PUMP'S half: it judges one candidate
// row and, when the premise is already false, CLOSES it as a no-op. It reports
// whether the row was consumed by the guard (so the pump skips it) and the
// refusal line the caller renders.
//
// Why close inside the pump rather than "return the row and let the caller
// close it": the pump runs inside TaskNextFiltered — a single process that owns
// the moment — and the row must not be left pending-as-refused, which every
// other dispatcher would re-scan forever. The close is a legal transition from
// both pending and claimed (taskAllowedTransitions), so a concurrent claimer
// that won the race a moment earlier ends in the same terminal state.
func mergeVerifySweepStaleRework(t *Task) (bool, string) {
	if !TaskIsMergeVerifyReworkRow(t) {
		return false, ""
	}
	v := mergeVerifyStaleJudgeRow(t)
	if !v.Stale {
		// Unmeasured or genuinely-unlanded: the premise is NOT false, so the
		// row is dispatched — but never silently (a guard indistinguishable
		// from a missing guard is how the measured defect stayed invisible).
		// Both faces: the ledger log line and the dispatcher's stderr.
		if v.Measured {
			fmt.Fprintf(os.Stderr, "merge-verify staleness guard: %s dispatched — %s (%s)\n",
				t.TaskID, v.Verdict, v.Evidence)
		}
		if v.Evidence != "" {
			taskNotifyLog("merge-verify staleness guard: %s premise %s — %s", t.TaskID, v.Verdict, v.Evidence)
		}
		return false, ""
	}
	return mergeVerifyStaleCloseRow(t, v)
}

// MergeVerifyStaleClaimNote renders the guard's claim-time line for ONE row —
// the read-only entry an operator (or PA) uses to ask "what would the guard
// say about this row?" before a round. Same judgement the claim path runs, so
// the answer and the enforcement cannot drift.
func MergeVerifyStaleClaimNote(taskID string) string {
	v, err := MergeVerifyStaleReworkJudge(taskID)
	if err != nil {
		return ""
	}
	if v.Skip {
		return ""
	}
	if !v.Measured {
		return fmt.Sprintf("%s %s: premise %s (%s)", MergeVerifyStaleNoOpMarker, v.TaskID,
			MergeVerifyStaleVerdictNotJudged, v.Evidence)
	}
	return fmt.Sprintf("%s %s: premise %s (%s)", MergeVerifyStaleNoOpMarker, v.TaskID,
		v.Verdict, v.Evidence)
}

// mergeVerifyStaleSweepPending closes the stale merge-verify rework rows that
// are sitting in the pending queue, returning the refusal lines for the claim
// that triggered the sweep. It is the entry the dispatcher calls at claim time
// (worker round / serve dispatch / `task run next` all funnel through
// TaskNextFiltered, which calls THIS before its CAS).
//
// It takes the CALLER'S already-open ledger handle (db). That is deliberate and
// load-bearing: opening a second handle on the same sqlite file adds a
// WAL shm-unmap/close cycle per claim, and that close is a known long-tail
// stall for the pure-Go modernc driver (measured in this repo:
// TestTaskNextOrdersByPriority timing out inside `TaskSubmit` →
// `sqlite3_close_v2` → `unixShmUnmap`). Sharing the claim's own handle keeps
// the guard free of ANY extra open/close churn — the sweep is then only its
// queries plus bounded git probes.
//
// The sweep is bounded (mergeVerifyStaleSweepCap) and read-then-close by row
// id: a row that leaves pending between the read and the close is left alone
// (the close is a legal transition from either state, so no row can be lost).
func mergeVerifyStaleSweepPending(db *sql.DB) []string {
	if db == nil {
		return nil
	}
	ctx := context.Background()
	// Lane prefilter (SQL narrowing hint only — TaskIsMergeVerifyReworkRow is
	// the authority): the gate writes every rework row with both tags, so the
	// scan touches the lane's rows and nothing else.
	rows, err := db.QueryContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks `+
			`WHERE status = ? AND tags LIKE ? ORDER BY priority ASC, created_at ASC, id ASC LIMIT ?`,
		taskStatusPending, "%"+MergeVerifyEvent+"%", mergeVerifyStaleSweepCap)
	if err != nil {
		taskNotifyLog("merge-verify staleness sweep: query: %v", err)
		return nil
	}
	var candidates []*Task
	for rows.Next() {
		c, err := scanTask(rows)
		if err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			taskNotifyLog("merge-verify staleness sweep: scan: %v", err)
			return nil
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		taskNotifyLog("merge-verify staleness sweep: rows: %v", err)
		return nil
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover

	var closed []string
	for _, c := range candidates {
		ok, reason := mergeVerifySweepStaleRework(c)
		if ok {
			closed = append(closed, reason)
		}
	}
	return closed
}

// MergeVerifyStaleReworkNoOpSummary is the audit line the sweep returns to its
// caller (the CLI prints it; a test asserts on it). Kept here so the sweep's
// output shape has ONE definition.
func MergeVerifyStaleReworkNoOpSummary(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return fmt.Sprintf("%s closed %d stale rework row(s) at claim time: %s",
		MergeVerifyStaleNoOpMarker, len(reasons), strings.Join(reasons, " | "))
}
