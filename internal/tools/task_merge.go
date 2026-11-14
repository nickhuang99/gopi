package tools

// TaskMerge (epic "gopi task merge", 2026-09-01, the operator's design correction:
// "why is it not a gopi cli interface?"): the merge back-pressure that used
// to live as inline bash loops in the post-receive hook (v1-v4 incident
// chain 16c391a/b9aabac/d63a6f5/c80f870) is now ONE token-free Go command.
//
// The flow per branch:
//
//	already merged?   git merge-base --is-ancestor <branch> master →
//	                  branch -d (git's own -d gate refuses unmerged refs)
//	hp-unique-*?      ARCHIVE-EXEMPT — the loser-archive ledger is never
//	                  touched (same exemption as the finish-cleanup chain)
//	clean merge?      git merge <branch> → ort auto-commits → branch -d
//	conflict?         git merge --abort → exit non-zero → enqueue ONE
//	                  merge-conflict task (LLM domain: reading conflict
//	                  hunks is a semantic judgment — architecture red line
//	                  ④, task_adoption.go) and leave master untouched
//
// git is the arbiter: the Go layer only invokes git, reads its exit code,
// and routes the outcome. No diff parsing, no hunk reasoning.

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/tasksource"
)

// --- TaskMerge tunables (Rule 1: every literal in one place) ---

const (
	// taskMergeGitTimeout bounds each git invocation — a hung repo must
	// degrade to an error outcome, never wedge the caller (serve tick).
	taskMergeGitTimeout = 10 * time.Second
	// taskMergeDetachedHeadRef is git's spelling of "no branch" in a ref list:
	// `for-each-ref refs/heads` never yields it, but the named-branch spelling
	// admits it as a positional, so both sweeps skip it by the same word.
	taskMergeDetachedHeadRef = "HEAD"

	// taskMergeCwdRepo is the repo an invocation operates in when the caller
	// named none: git's own "here". Named (Rule 1) because the no-argument
	// spelling's repo RESOLUTION and the per-repo engine both spell it.
	taskMergeCwdRepo = "."

	// The repos-table sweep's failure vocabulary (the no-argument spelling).
	// Each is a DIFFERENT answer to "merge everything" and each names the
	// remedy, because the defect they replace was one silent sentence for all
	// three: a run that swept nothing and reported success (Rule 15).
	//
	//	taskMergeSweepTableUnreadableFmt  the table exists but cannot be read
	//	                                  (args: key, the resolver's error)
	//	taskMergeSweepTableEmptyFmt       the table is empty — nothing configured
	//	                                  (args: key, settings-path env, --repo)
	//	taskMergeSweepRepoUndeclaredFmt   a --repo the table does not declare
	//	                                  (args: the path, key, env, table size)
	//	taskMergeSweepRepoFailedFmt       one repo's sweep returned a hard error
	//	                                  (args: repo, that error)
	taskMergeSweepTableUnreadableFmt = "task merge: the settings.json %q table cannot be read: %w"
	taskMergeSweepTableEmptyFmt      = "task merge: the settings.json %q table names no repo, so a full sweep would examine nothing — declare a repo in it (or point %s at a settings file that does), or name the branches to merge; `gopi task merge %s PATH ...` sweeps one declared repo"
	taskMergeSweepRepoUndeclaredFmt  = "task merge: repo %q is not in the settings.json %q table (%d configured) — a full sweep covers the table; add the repo to it (%s names the file), or name the branches to merge"
	taskMergeSweepRepoFailedFmt      = "task merge: repo %q: %w"
	// taskMergeSweepCandidatesFmt  discovery for one repo failed (args: the
	//                              reason it could not be read)
	taskMergeSweepCandidatesFmt = "merge candidate discovery: %s"

	// taskMergeSweepRepoFlag is the `--repo` spelling the sweep's refusals
	// point a reader at: the same word internal/runcmd.FlagRepo gives every
	// command line, spelled here because the refusal text lives in this package
	// and a bare literal beside four named knobs is what Rule 1 forbids.
	taskMergeSweepRepoFlag = runcmd.FlagRepo

	// taskMergeSummaryVerb stamps the conflict task summary — the dedup
	// key shared with adoptionDispatchConflict's spelling.
	taskMergeSummaryVerb = "resolve merge conflict: %s into master"

	// taskMergeTagMergeConflict routes the conflict row to the priority
	// tier (taskTagPriority); it is THE epic-mandated tag.
	taskMergeTagMergeConflict = taskTagMergeConflict

	// taskMergeTagAutoDispatch marks machine-enqueued rows (the hook face).
	taskMergeTagAutoDispatch = "auto-dispatch"

	// taskMergeSourceAuto is the ledger source value for machine-enqueued
	// rows. The VALUE now comes from internal/tasksource (Rule 14) — the merge
	// path's own role, `merger`, which the retired "auto" conflated with the
	// harness's startup registration.
	taskMergeSourceAuto = tasksource.Merger

	// taskMergePayloadBranchPrefix is the TIP-OPEN head of the conflict
	// briefing line — "branch <name> (tip ". The ⑤ same-branch-once dedup
	// (adoptionConflictTaskLive) derives its payload needle from THIS const
	// while the writers render the full line from it, so the needle can
	// never drift from what a dispatched briefing actually carries.
	taskMergePayloadBranchPrefix = "branch %s (tip "

	// taskMergePayloadBranchLine is the FULL conflict briefing line the
	// writers render (prefix + sha + closing paren): the composition keeps
	// reader and writer on one definition (Rule 14).
	taskMergePayloadBranchLine = taskMergePayloadBranchPrefix + "%s)"

	// Branch-conflict outcome vocabulary — the (action, detail) pair every
	// writer in this file reports and `gopi task merge` prints.
	mergeConflictActionDispatched = "dispatched"
	mergeConflictActionDuplicate  = "duplicate"
	// mergeConflictActionConcluded is the outcome a SECOND dispatch for the
	// same branch reports when the branch is already named by a TERMINAL
	// conflict row: the decision was taken, the resolver's deliverable is on
	// master via the ordinary path, and this writer declines to re-mint.
	// Distinct from mergeConflictActionDuplicate on purpose — one is
	// "the work is queued", the other "the work is judged" — because the
	// operator's next action differs (wait vs look at the branch).
	mergeConflictActionConcluded = "concluded"
	mergeConflictActionError     = "error"
	// mergeConflictActionConflict is the REPORT-ONLY outcome (no --submit):
	// the conflict was measured and nothing was enqueued.
	mergeConflictActionConflict = "conflict"
	// mergeConflictActionReworkOwned is the outcome for a branch already owned
	// by a LIVE branch-rework row: the rework lane holds the branch, so the
	// merge-time conflict lane defers instead of minting a second, competing
	// job for the same work (F5 of devdoc/merge-resolve-effort-audit.md).
	mergeConflictActionReworkOwned = "rework-owned"

	// TaskMergeActionHeadOffIntegration is the outcome for a branch this face
	// REFUSED because the repo's checkout is not on the integration branch —
	// nothing was decided about the branch, and every branch of the run gets
	// this same verdict until the checkout is put right. Named (Rule 1b/13):
	// the CLI report, the counts switch and the tests all key on this word.
	TaskMergeActionHeadOffIntegration = "head-off-integration"

	// taskMergeHeadOffIntegrationFmt is the refusal SENTENCE: the branch the
	// checkout was found on, the integration branch this face would have landed
	// into, what was and was not done, and the exact command that puts it right.
	// It is a FORMAT rather than one fixed sentence because every name in it is a
	// per-repo fact, and it is declared once (Rule 1b) because the CLI report and
	// the tests both read this spelling.
	//
	// The three claims it makes are the refusals' whole contract, and each is
	// measured before it is printed: nothing was merged (no `git merge` ran),
	// nothing was deleted (the branch is at its old tip) and nothing was built.
	// The remedy is printed, NEVER performed — relocating an operator's working
	// tree out from under them is outside this command's contract, and a branch
	// move that fails (uncommitted work) would leave the run half-done.
	taskMergeHeadOffIntegrationFmt = "checkout is on %s, not the integration branch %s — a merge here would land on %s; " +
		"left untouched (nothing merged, deleted or built). Remedy: git -C %s checkout %s"

	// taskMergeCheckoutHeadUnreadableFmt is the arm where the question could not
	// be ANSWERED at all (git failed, or the directory is not a repo). It is a
	// deliberate third state, never folded into the refusal above: "the checkout
	// is on another branch" and "I could not read the checkout" are different
	// facts, and only one of them is fixed by checking the integration branch
	// out (Rule 15 — a display that renders both alike is a silent failure
	// wearing a label).
	taskMergeCheckoutHeadUnreadableFmt = "checkout branch unreadable — cannot tell whether a merge here would land on the integration branch, " +
		"so nothing is merged: %w"
)

// The branch-rework lane's two live states, spelled once (Rule 1b): they are
// what mergeConflictActionReworkOwned's detail line reports.
const (
	resolveOutcomeReworkPending    = "rework-queued"
	resolveOutcomeReworkInProgress = "rework-in-progress"
)

// resolveConflictRowIdentity answers what this branch's `resolve merge
// conflict: <branch>` rows already say — the ONE reader both live writers
// consult before minting a row (F6 of devdoc/merge-resolve-effort-audit.md).
//
// TWO ROW SHAPES ARE THE SAME FACT. The lane-A writer stamps the branch into
// the SUMMARY (taskMergeSummaryVerb); the lane-B writer stamps the same summary
// text but a payload that also carries taskMergePayloadBranchLine. Both are the
// branch's resolve-row identity, so both are counted here — a branch named as
// CONCLUDED by either is concluded for both. The summary operand is bound as a
// parameter (never interpolated); the payload channel stays TAG-anchored
// (taskTagTokenSQLTest) so it matches only genuine merge-conflict rows, and its
// needle is derived from the shared prefix (Rule 13: needle and writer cannot
// drift). "No" is likewise stated in both channels: a row whose summary matches
// is not required to carry the payload line.
//
// THE DEFECT IT CLOSES. Dedup used to ask only "is a row LIVE?" (pending /
// claimed / in_progress). A row that reached a terminal status therefore
// stopped blocking, so the next tick minted a fresh one for the same branch:
// measured 8 branches carried >1 row and feat/merger-always-rebuilds took 7
// rows in 3 hours, each one hand-authoring ~93 lines of conflict resolution
// (the audit's F7) before being closed — the same work queued, executed and
// closed again.
//
// THE RULE. A branch has at most ONE resolve row in flight and at most ONE
// concluded decision: live → duplicate (wait), terminal → concluded (the work
// is judged, do not re-mint). The re-mint that is LEGITIMATE — a branch whose
// resolve attempt FAILED, and whose author has since pushed more work — is not
// silenced by this gate alone: it reaches the ledger through the branch-rework
// lane (resolveRowReworkSummaryPrefix, deduped on a live rework row), which is
// the lane that owns "the resolver could not land it" (F5's lane-ownership
// note, and the take-over prompt's §3.6).
//
// `live` and `terminal` are mutually exclusive by construction (the two
// status sets partition the CHECK-constrained set), so callers read the
// pair as one tri-state.
func resolveConflictRowIdentity(ctx context.Context, branch string) (live, terminal bool, err error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false, false, err
	}
	defer db.Close()
	branchNeedle := "%" + fmt.Sprintf(taskMergePayloadBranchPrefix, branch) + "%"
	var liveN, terminalN int
	err = db.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE status IN (`+taskLiveStatusSQLList()+`)),
       COUNT(*) FILTER (WHERE status IN (`+taskTerminalStatusSQLList()+`))
FROM tasks WHERE summary = ?
   OR (`+taskTagTokenSQLTest(taskTagMergeConflict)+` AND payload LIKE ?)`,
		fmt.Sprintf(taskMergeSummaryVerb, branch), branchNeedle).Scan(&liveN, &terminalN)
	if err != nil {
		return false, false, err
	}
	return liveN > 0, terminalN > 0, nil
}

// resolveConflictRowIdentityLive is the live-only projection of
// resolveConflictRowIdentity: "is a resolver already holding this branch?".
// Callers that only need the in-flight half (the token-scoped dedup table)
// read it through here so the tri-state has exactly one implementation.
func resolveConflictRowIdentityLive(ctx context.Context, branch string) (bool, error) {
	live, _, err := resolveConflictRowIdentity(ctx, branch)
	return live, err
}

// reworkRowLiveState reports whether a LIVE branch-rework row already owns
// this branch, and names the state it is in ("" when there is none).
//
// It is the SECOND half of F6's identity rule, and the mechanism behind F5's
// lane-ownership deference: the summary `resolveRowReworkSummaryPrefix+branch`
// is the branch's identity in the rework lane (resolveRowHasOpenRework and the
// two merge-time refusals read the same one), so a live row here means the
// branch's merge-time difficulty is already being worked by its author's lane.
// A writer that minted a conflict row anyway would queue the SAME branch for
// two resolvers, which is the competing-jobs defect F5 measured.
func reworkRowLiveState(ctx context.Context, branch string) (string, bool, error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	var status string
	err = db.QueryRowContext(ctx, `
SELECT status FROM tasks
WHERE summary = ? AND status IN (`+taskLiveStatusSQLList()+`)
ORDER BY id LIMIT 1`, resolveRowReworkSummaryPrefix+branch).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if status == taskStatusPending {
		return resolveOutcomeReworkPending, true, nil
	}
	return resolveOutcomeReworkInProgress, true, nil
}

// TaskMergeOptions configures one invocation. The zero value merges the
// cwd's repo into master and routes conflicts to the ledger.
type TaskMergeOptions struct {
	// Repo is the git repo to operate in ("" = cwd).
	Repo string
	// MasterBranch is the integration branch ("" = "master").
	MasterBranch string
	// Submit routes conflicts into the ledger when true (the hook and
	// --all face); false = report the conflict, exit non-zero, no writes.
	Submit bool
	// Source stamps dispatched conflict rows ("" = taskMergeSourceAuto,
	// the merge path's own role — internal/tasksource.Merger).
	Source string
	// DBPath overrides the ledger ("" = memoryDBPath()).
	DBPath string
}

// TaskMergeBranchOutcome is one branch's result line (the report rows).
type TaskMergeBranchOutcome struct {
	// Branch is the short branch name.
	Branch string
	// Action is what happened: merged | deleted | conflict | dispatched |
	// duplicate | exempt | error | skipped.
	Action string
	// Detail is the git one-liner / skip reason (human diagnosis).
	Detail string
	// PreMergeTip is master's tip immediately BEFORE this branch's merge
	// attempt ("" when the probe failed / the branch was never merged). The
	// post-merge verify gate records it as the tip a failed verdict must
	// restore, so the record precedes the landing in the same call that
	// performs it (epic merger-verify-gate: the tip is recorded BEFORE
	// merging, never re-derived after).
	PreMergeTip string
	// BuildErr carries the BRANCH's own build diagnostics when the merge-time
	// build gate refused it (the real compiler text, already capped) — the
	// refusal's payload is rendered from it, so the row a reworker claims and
	// the sweep report tell the same story (Rule 13: one measurement, one
	// spelling).
	BuildErr string
	// BuildCmd is the build command the gate ran (the repo's declared contract,
	// or the Go default) — named in the refusal brief so the fix is verified
	// with the SAME command that refused it.
	BuildCmd string
	// BuildNotified is true when the build refusal also FILED the
	// branch-rework notification row this tick (false when a live row already
	// existed — the shared dedup — or the submit failed). The branch is
	// archived either way; only the count differs.
	BuildNotified bool
}

// TaskMergeCounts is the invocation's tally.
type TaskMergeCounts struct {
	// Merged counts clean merges (git auto-committed via ort).
	Merged int
	// Deleted counts already-merged branches pruned with branch -d.
	Deleted int
	// Dispatched counts NEW conflict rows enqueued.
	Dispatched int
	// Duplicated counts conflicts whose task was already live (dedup).
	Duplicated int
	// Concluded counts conflicts whose branch is already named by a TERMINAL
	// resolve row — the decision was taken, so no second row is minted (F6,
	// Rule 5's F6 section in tasks/01a0b1b3-report.md). It is reported apart
	// from Duplicated because the two say different things about the branch.
	Concluded int
	// ReworkOwned counts conflicts this lane DEFERRED because the branch-rework
	// lane already holds the branch (F5 lane ownership, rule 2): the branch's
	// merge-time difficulty is already queued as its author's job.
	ReworkOwned int
	// Exempt counts hp-unique-* archive-ledger skips.
	Exempt int
	// Conflicted counts conflicts NOT dispatched (report-only face).
	Conflicted int
	// ContractRefused counts branches the merge-time CLI contract gate
	// refused: the branch newly wires a main.go dispatch predicate its
	// cli_contracts_test.go does not track, so landing it would put a red
	// test on master (bd3d34a5 voice_input). Master is untouched by
	// construction — the gate runs before `git merge` — and the branch is
	// left intact for the rework lane.
	ContractRefused int
	// BuildRefused counts branches the merge-time BUILD gate refused: the
	// branch does not compile while the merge target does, so landing it
	// would stop master building (2026-09-13, 82b05c4f's ten undefined
	// symbols — merged by 82b3b6f3 with no git conflict at all). Master is
	// untouched by construction and the branch is ARCHIVED in the same tick
	// (it is broken, not merely in conflict: no resolver and no rework
	// round-trip is spent on it).
	//
	// The merge-time MANIFEST gate's refusals share this counter and this
	// archive lane: both are refusals of a branch that is unusable AS IT STANDS
	// (one does not compile, the other would write a duplicated manifest key),
	// both archive the branch with the compile-failure-family reason, and both
	// route to the branch-rework lane — so one counter tells the operator
	// exactly what a nonzero means: "N branches were refused for being broken
	// in a way no merge resolution fixes". The gate that refused each one is
	// named per branch in the outcome detail, never folded into the number.
	BuildRefused int
	// HeadOffIntegration counts branches REFUSED because the repo's checkout is
	// not on the integration branch (see TaskMergeActionHeadOffIntegration).
	// Nothing was merged, deleted or built for them and the integration branch
	// did not move — the counter exists so the run's own summary line tells the
	// operator that the refusal happened, instead of leaving a zero-merge run to
	// be read as "there was nothing to do".
	HeadOffIntegration int
	// Failed counts per-branch errors (git/gitless).
	Failed int
	// RepoTallies is the repos-table sweep's PER-REPO report, in sweep order:
	// one entry per repo examined, carrying that repo's own counters and the
	// branches it landed. It exists because the recursive-counts contract above
	// is a UNION, and the close loop that follows a landing (the declared
	// rebuild, Rule 12) must run in EVERY repo that landed work — a union total
	// cannot name those repos, and a caller that guessed one (`opts.Repo", or
	// the cwd) would rebuild a repo the sweep never touched. It is empty for the
	// two single-repo spellings, which need no per-repo breakdown.
	RepoTallies []TaskMergeRepoTally
	// Repos counts the repos this invocation swept. It is 0 for the spellings
	// that decide ONE repo (named branches, --all) and 1-or-more for the
	// repos-table sweep — the counter that lets an operator tell "swept N repos,
	// nothing needed merging" from "examined no repo at all". Both used to read
	// as merged=0 with nothing said, which is the silent success the sweep's
	// loud arms replace (Rule 15).
	Repos int
	// Outcomes is the per-branch report (input order preserved).
	Outcomes []TaskMergeBranchOutcome
}

// TaskMergeRepoTally is ONE repo's slice of a repos-table sweep: the counters
// this repo produced and the branches it landed, so the caller's close loop can
// act per repo instead of on a union total.
type TaskMergeRepoTally struct {
	// Repo is the configured repo path, exactly as the settings table spells it.
	Repo string
	// Merged / Deleted are this repo's own counts (a subset of the union).
	Merged  int
	Deleted int
	// Landed names the branches this repo's sweep merged or pruned.
	Landed []string
}

// taskMergeGit runs one git command in repo ("" = cwd) and returns its
// trimmed stdout. A non-zero exit surfaces git's stderr (root-cause first)
// — the stderr text rides the error so isGitNoMergeToAbort can match
// "There is no merge to abort".
func taskMergeGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskMergeGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if repo != "" {
		cmd.Dir = repo
	}
	// THE LOCALE IS PINNED HERE, and this runner is its ONE place in this lane
	// (Rule 2/14). A caller above interprets git's own sentence:
	// isGitNoMergeToAbort matches `no merge to abort`, which git translates.
	// Measured on this host 2026-10-30 (git 2.43.0, zh_CN.utf8 installed):
	//
	//	$ LC_ALL=C            git merge --abort
	//	fatal: There is no merge to abort (MERGE_HEAD missing).
	//	$ LC_ALL=zh_CN.UTF-8  git merge --abort
	//	致命错误：没有要终止的合并（MERGE_HEAD 丢失）。
	//
	// Without the pin the second text matched nothing, so a PRE-FLIGHT failure
	// (`fatal: refusing to merge unrelated histories`) was reported as a
	// conflict scene to abort — `gopi task merge <orphan-branch>` against the
	// real binary said "merge --abort: … 没有要终止的合并" under zh_CN and
	// "no conflict state to abort" under C for the SAME input. Root cause and
	// the sharing rule: git_child_locale.go.
	cmd.Env = ChildLocaleEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// THE REPO IS NAMED (Rule 15). git's own fatal message says what went
		// wrong but never WHERE: measured on the real binary (this task's
		// rework of the archived branch task/01a115b6…, whose intent this
		// restores), `gopi task merge --all --repo <non-checkout>` printed
		// `git for-each-ref …: fatal: not a git repository` and named no path,
		// so a caller reading the line could not tell which repository the
		// engine had been pointed at — the one fact the reader needs, since the
		// repo comes from a flag. `-C %s` is also how the command actually
		// resolves the directory (cmd.Dir above), so the message spells the
		// invocation the reader would have to re-run. The repo-less arm is kept
		// unchanged: with no flag there is no path to name, and the caller's own
		// cwd is what git read.
		if repo != "" {
			return "", fmt.Errorf("git -C %s %s: %w: %s", repo, strings.Join(args, " "),
				err, strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "),
			err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// taskMergeExemptPrefix marks archive-ledger branches (hp-unique-*) — the
// loser-archive ledger is NEVER merged away or deleted (the finish-cleanup
// exemption, task_finish_cleanup.go).
func taskMergeExempt(branch string) bool {
	return strings.HasPrefix(branch, "hp-unique-")
}

// taskMergeGraveyardExempt reports whether branch is a DECIDED LOSER by NAME:
// a branch whose name sits in the archive/ graveyard namespace (PROJECT.md Rule
// 9), the namespace the merger's own archive step writes. It is the ONE
// predicate behind the graveyard half of the manual face's refusal.
//
// Every other path already honours that namespace: repoSweepClassify's
// archive-tagged bucket, taskMergeExecutor's mergeExecBlacklistPrefixes
// ("loser/," + archiveBranchPrefix), task_deliverable_branch's archived-name
// test and task_finish_cleanup's branch skip. The manual face did not, which
// is the hole this predicate closes.
//
// Why a NAME test and not the archive TAG alone: the sweep removes a decided
// loser from the candidate set with a refs/tags/archive/<branch> TAG, but the
// graveyard is equally a NAME (repo_resolve_row archives by name), and a
// hand-typed `gopi task merge <ref>` arrives as a positional argument that
// reaches neither discovery filter. Measured on master 2026-09-19 (recovery
// audit for archive/feat/conflict-branch-rename, abb4a062, epic
// conflict-branch-rename): an archive-TAGGED branch named by hand merged into
// master, and an archive/-NAMED branch merged in a `gopi task merge --all`
// sweep — both landed, both deleted.
func taskMergeGraveyardExempt(branch string) bool {
	return strings.HasPrefix(branch, archiveBranchPrefix)
}

// isGitExit1 reports whether err is git's exit-1 (the is-ancestor "no"
// answer — a RESULT, not a failure; merge-base exits 1 when the commit is
// not an ancestor). 128 = unknown revision → a real error.
func isGitExit1(err error) bool {
	return err != nil && strings.Contains(err.Error(), "exit status 1")
}

// isGitNoMergeToAbort reports git's "There is no merge to abort (MERGE_HEAD
// missing)" — the merge never started, so the failure is a pre-flight
// error (unknown revision, wedge), NOT a conflict scene to dispatch.
func isGitNoMergeToAbort(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no merge to abort")
}

// TaskMerge merges the named branches into the integration branch — and, when
// the caller names NO branch, it sweeps the settings.json "repos" table.
//
// THE THREE SPELLINGS, and the decision each one makes. They used to be three
// different stories about one verb: the help page promised the repos-table
// sweep, this comment claimed "empty branches without All is a usage error",
// and the engine returned success having decided nothing at all. A no-argument
// run that reports success while examining no repo is Rule 15's shape (silent
// failure wearing a green exit code), which is why the empty spelling now has a
// LOUD arm of its own.
//
//	branches...   decide ONLY the named branches, inside opts.Repo ("" = cwd)
//	(empty)+All   sweep every local branch of opts.Repo except the integration
//	              branch (hp-unique-* archive-ledger branches stay exempt)
//	(empty)       THE FULL SWEEP: resolve every repo in the settings.json
//	              "repos" table (config.ResolveMergerRepos — the ONE reader of
//	              that table, Rules 13/14) and run the per-repo BRANCH sweep of
//	              the spelling above on each. The counters are the UNION over
//	              repos, Outcomes keeps each repo's rows in sweep order, and
//	              counts.Repos says how many repos were examined — the number
//	              that lets a reader tell "swept 3 repos, nothing to merge" from
//	              "examined no repo at all".
//
// WHICH BRANCHES THE FULL SWEEP DECIDES, and why the restriction is load-bearing.
// The repos-table sweep decides the MERGER PASS's branch CANDIDATE SET, not
// "every local branch". It takes that set from MergerSweepPreview
// (repo_sweep.go), which runs the SAME classifier the sweep itself runs
// (repoSweepClassify, shared by construction — Rules 13/14), so the manual face
// and the merger pass can never disagree about which branch is a candidate. A
// branch is a candidate only when ALL of these hold:
//
//	· its tip is not already integrated into the repo's integration branch;
//	· it is not checked out in a linked worktree (an occupied branch is ACTIVE
//	  work — the checkout guard is load-bearing); and
//	· it is not an archive-ledger / graveyard ref (hp-unique-*, archive/*).
//
// The per-branch TRIAGE below that set stays this engine's (task_mergeOne: the
// merge-time gates, the conflict dispatch, the counters).
//
// The restriction is not a shortcut. A hand-rolled "every local branch except
// the integration branch" list is the sweep epic merger-ff-park-file RETIRED:
// it decides branches nothing prepared, and it steps around the two guards that
// make a landing safe — the checkout-occupancy guard (an occupied branch is
// active work) and the resumable sweep's in-flight/dirty-tree checks. Taking
// the CANDIDATE SET from the sweep and the TRIAGE from this engine keeps one
// owner for each half, which is also what makes the manual face and the merger
// pass agree by construction rather than by review.
//
// An EMPTY or UNREADABLE repos table is an ERROR, never a success: "nothing is
// configured" and "nothing needed merging" are different answers to "merge
// everything", and only one of them is a fact about the branches.
//
// opts.Repo is the sweep TARGET when the caller names one: the no-argument
// spelling then sweeps THAT repo and only that repo (a manual single-repo sweep)
// instead of quietly deciding nothing. A --repo the table does not declare is
// REFUSED by name (the remedy is either to add it to the table or to name the
// branches), because a no-argument run against an unconfigured directory would
// otherwise report a success that covers no configured repo.
//
// Exit-code semantics live in the CLI face; this engine returns the counts plus
// the first hard error.
func TaskMerge(opts TaskMergeOptions, branches []string, all bool) (*TaskMergeCounts, error) {
	master := opts.MasterBranch
	if master == "" {
		master = IntegrationBranchName
	}
	if !all && len(branches) == 0 {
		return taskMergeReposTableSweep(opts, master)
	}
	repo := opts.Repo
	if repo == "" {
		repo = taskMergeCwdRepo
	}
	return taskMergeNamed(opts, repo, master, branches, all)
}

// taskMergeNamed is the per-repo engine: decide the named branches inside ONE
// repo, or — with all=true — every local branch of it except the integration
// branch. It is the single implementation both spellings above run, so an
// --all sweep and the repos-table sweep cannot drift apart (Rules 13/14).
func taskMergeNamed(opts TaskMergeOptions, repo, master string, branches []string, all bool) (*TaskMergeCounts, error) {
	counts := &TaskMergeCounts{}
	if all {
		names, err := taskMergeGit(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
		if err != nil {
			return counts, err
		}
		for _, name := range strings.Fields(names) {
			if name == master || name == taskMergeDetachedHeadRef || taskMergeExempt(name) {
				continue
			}
			branches = append(branches, name)
		}
	}
	if len(branches) == 0 {
		return counts, nil
	}
	for _, branch := range branches {
		outcome := taskMergeOne(opts, repo, master, branch)
		counts.Outcomes = append(counts.Outcomes, outcome)
		switch outcome.Action {
		case mergeActionMerged:
			counts.Merged++
		case mergeActionDeleted:
			counts.Deleted++
		case mergeConflictActionDispatched:
			counts.Dispatched++
		case mergeConflictActionDuplicate:
			counts.Duplicated++
		case mergeConflictActionConcluded:
			counts.Concluded++
		case mergeConflictActionReworkOwned:
			counts.ReworkOwned++
		case mergeConflictActionConflict:
			counts.Conflicted++
		case "exempt":
			counts.Exempt++
		case mergeContractGateActionRefused:
			counts.ContractRefused++
		case mergeBuildGateActionRefused:
			counts.BuildRefused++
		case TaskMergeActionHeadOffIntegration:
			// The checkout guard's tally. It is deliberately NOT folded into
			// Failed: nothing failed here — the branches are intact and the
			// integration branch is untouched, so an operator reading the
			// counters must not go looking for a broken branch. It is a refusal
			// of the same family as the gate refusals above (the run declined to
			// act, and said why), which is why it lands beside them.
			counts.HeadOffIntegration++
		case repoSweepActionRefGone:
			// The named branch's ref does not exist: the operator's request
			// cannot be honoured, so it is a per-branch FAILURE (exit 2) — the
			// ledger record of any live row still claiming it is already on that
			// row's trail (repo_sweep_ref_guard.go).
			counts.Failed++
		default:
			counts.Failed++
		}
	}
	return counts, nil
}

// taskMergeReposTableSweep is the no-branch, no---all arm: resolve the configured
// repos and sweep each one.
//
// WHO REACHES IT (task 01a1ac9e, 2026-11-06): the ENGINE arms it exactly as
// before — the merger tick's own pass, and `gopi task merge --repo <PATH>` from
// the CLI, name it. What changed is the CLI's BARE spelling: `gopi task merge`
// with no branch, no --all and no --repo is now refused at the parse seam
// (cmd/agent/cli_tasks.go, taskBareScopeRefusal) with exit 1 rather than taking
// the merge lock and sweeping every configured repo — measured on this host, the
// bare run had decided nothing and printed nothing in five seconds, twice,
// against the operator's LIVE working checkout. The arm below is unchanged and
// still the truth the help page describes; only the door that ran it unasked is
// closed.
//
// LOUD, never a no-op (Rule 15): an unreadable table, an EMPTY table, and a
// --repo the table does not declare are each a returned error naming the reason
// and the remedy. The caller's cwd is deliberately NOT a fallback target: the
// engine cannot tell a misconfigured profile from a deliberately narrow one, and
// a sweep that quietly examined nothing while exiting 0 is exactly the defect
// this arm replaces.
func taskMergeReposTableSweep(opts TaskMergeOptions, master string) (*TaskMergeCounts, error) {
	targets, err := taskMergeSweepTargets(opts.Repo)
	if err != nil {
		return &TaskMergeCounts{}, err
	}
	counts := &TaskMergeCounts{}
	for _, repo := range targets {
		// The CANDIDATE SET is the sweep's, never a hand-rolled branch list:
		// MergerRepoSweep owns discovery (and the occupancy / graveyard /
		// already-integrated guards that make it safe), so the manual face and
		// the merger pass decide the same branches by construction.
		discovered, err := taskMergeSweepCandidates(repo, master)
		if err != nil {
			// A repo error is REPORTED and the sweep CONTINUES: one broken repo
			// in the table must not abandon the rest. It is still never
			// swallowed — the first hard error is returned WITH the counts.
			return counts, fmt.Errorf(taskMergeSweepRepoFailedFmt, repo, err)
		}
		perRepo, perErr := taskMergeNamed(opts, repo, master, discovered, false)
		if perErr != nil {
			return counts, fmt.Errorf(taskMergeSweepRepoFailedFmt, repo, perErr)
		}
		taskMergeAddCounts(counts, perRepo)
		counts.RepoTallies = append(counts.RepoTallies, TaskMergeRepoTally{
			Repo:    repo,
			Merged:  perRepo.Merged,
			Deleted: perRepo.Deleted,
			Landed:  MergedBranchNames(perRepo.Outcomes),
		})
		counts.Repos++
	}
	return counts, nil
}

// taskMergeSweepCandidates is the repos-table sweep's BRANCH RESOLUTION for one
// repo: the branches the merge candidate set offers, from the ONE discovery
// owner (MergerSweepPreview shares repoSweepClassify with the real sweep, so the
// list a manual sweep decides is exactly the list the merger pass would).
//
// A repo the discovery cannot read is an ERROR naming the repo — the sweep must
// not report "examined, nothing to merge" about a repo it could not open.
func taskMergeSweepCandidates(repo, master string) ([]string, error) {
	preview := MergerSweepPreview(repo)
	if preview.Err != "" {
		return nil, fmt.Errorf(taskMergeSweepCandidatesFmt, preview.Err)
	}
	branches := make([]string, 0, len(preview.Candidates))
	for _, branch := range preview.Candidates {
		// The per-branch triage below belongs to the engine (gates, conflict
		// dispatch), so the integration branch and the archive ledger are
		// filtered here as they are in the --all spelling — ONE predicate
		// (taskMergeExempt), never a second copy of the rule (Rule 1: the
		// detached-HEAD spelling is git's own word, named once).
		if branch == master || branch == taskMergeDetachedHeadRef || taskMergeExempt(branch) {
			continue
		}
		branches = append(branches, branch)
	}
	// NO CANDIDATE is a legitimate answer, not an error: a repo whose branches
	// are all integrated, archived or held by a checkout has nothing for a sweep
	// to decide, and the counters say so (repos=1, merged=0). The distinction
	// this epic needs is "examined a repo, found nothing" versus "examined no
	// repo at all" — and only the SECOND is a failure (the table arms above).
	return branches, nil
}

// taskMergeSweepTargets resolves the repos the no-argument spelling sweeps.
//
// requested is opts.Repo: the caller's --repo override, "" when there is none.
// With no override the sweep list IS the settings.json "repos" table, through
// config.ResolveMergerRepos — the ONE reader of that table (Rules 13/14), so the
// manual face and the merger tick sweep the same set by construction.
//
// With an override the sweep is NARROWED to that one repo, and the narrowing is
// checked against the same table: an undeclared --repo is refused by name rather
// than swept, because "sweep everything" against a directory the table does not
// list would mean "sweep nothing" — and returning it as a success is the very
// answer this arm exists to stop giving.
func taskMergeSweepTargets(requested string) ([]string, error) {
	repos, err := config.ResolveMergerRepos()
	if err != nil {
		return nil, fmt.Errorf(taskMergeSweepTableUnreadableFmt, config.ReposSettingsKey, err)
	}
	if strings.TrimSpace(requested) == "" {
		if len(repos) == 0 {
			return nil, fmt.Errorf(taskMergeSweepTableEmptyFmt, config.ReposSettingsKey,
				config.SettingsPathEnv, taskMergeSweepRepoFlag)
		}
		return repos, nil
	}
	want := filepath.Clean(requested)
	for _, repo := range repos {
		if filepath.Clean(repo) == want {
			return []string{repo}, nil
		}
	}
	return nil, fmt.Errorf(taskMergeSweepRepoUndeclaredFmt, requested, config.ReposSettingsKey,
		len(repos), config.SettingsPathEnv)
}

// taskMergeAddCounts folds one repo's tally into the sweep's union.
func taskMergeAddCounts(into, from *TaskMergeCounts) {
	into.Merged += from.Merged
	into.Deleted += from.Deleted
	into.Dispatched += from.Dispatched
	into.Duplicated += from.Duplicated
	into.Concluded += from.Concluded
	into.ReworkOwned += from.ReworkOwned
	into.Exempt += from.Exempt
	into.Conflicted += from.Conflicted
	into.ContractRefused += from.ContractRefused
	into.BuildRefused += from.BuildRefused
	into.HeadOffIntegration += from.HeadOffIntegration
	into.Failed += from.Failed
	into.Outcomes = append(into.Outcomes, from.Outcomes...)
}

// taskMergeOne runs the per-branch decision tree (one outcome per call).
func taskMergeOne(opts TaskMergeOptions, repo, master, branch string) TaskMergeBranchOutcome {
	fail := func(err error) TaskMergeBranchOutcome {
		return TaskMergeBranchOutcome{Branch: branch, Action: "error", Detail: err.Error()}
	}
	// ⓪-A THE CHECKOUT GUARD — FIRST, before every other arm, because every arm
	// below ends in a `git merge <branch>` executed in THIS repo, and git merges
	// into whatever branch the checkout holds. With the checkout parked on
	// another branch that merge lands the WRONG REF, and the run still reports
	// the branch as merged and prunes its ref: the work leaves the queue while
	// the integration branch never received it.
	//
	// It is an ADDITIONAL refusal, not a replacement for anything: the graveyard,
	// ref-presence, archive-tag, contract, build and union arms below all still
	// run, unchanged, for a repo whose checkout IS on the integration branch.
	//
	// READ-ONLY AND NEVER A REPAIR. `rev-parse --abbrev-ref HEAD` answers from
	// the ref that points at the current commit — it neither inspects nor touches
	// the working tree — so this gate cannot disturb a live checkout, and it
	// deliberately does NOT check the branch out for the operator (the restart
	// face's counterpart in cmd/agent has its own restore arm; this engine's
	// contract is "refuse, name the remedy, stop").
	//
	// The expected branch is `IntegrationBranchForRepo(repo)`, the ONE resolver
	// the whole package shares (Rule 13/14) — never a literal here, because the
	// two faces must never disagree about which branch a landing means. The
	// master parameter passed in by the caller is the same value for every
	// ordinary caller; the resolver is what makes a repo whose integration branch
	// is `main` behave correctly too.
	expected := IntegrationBranchForRepo(repo)
	head, headErr := taskMergeGit(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if headErr != nil {
		return fail(fmt.Errorf(taskMergeCheckoutHeadUnreadableFmt, headErr))
	}
	if head != expected {
		// A DETACHED HEAD lands in this same arm and needs no separate sentence:
		// `rev-parse --abbrev-ref HEAD` renders it literally as "HEAD", which is
		// neither the found branch nor a branch at all — the "would land on HEAD"
		// reading is exactly as true as it is useless, and the remedy it prints is
		// the correct one either way.
		return TaskMergeBranchOutcome{Branch: branch, Action: TaskMergeActionHeadOffIntegration,
			Detail: fmt.Sprintf(taskMergeHeadOffIntegrationFmt, head, expected, head, repo, expected)}
	}
	if taskMergeExempt(branch) {
		return TaskMergeBranchOutcome{Branch: branch, Action: "exempt",
			Detail: "archive ledger (hp-unique-*) — never merged or deleted"}
	}

	// ⓪c the GRAVEYARD arm — a branch NAMED into the archive/ namespace is a
	// decided loser and is refused BEFORE any discovery filter could have
	// dropped it. The sweep's tag-based exemption covers its own candidate
	// set; this arm is what covers a branch named by hand (positional arg) or
	// reached by the --all loop's pre-filter. Master-safe by construction: the
	// refusal runs before every merge-time gate, so nothing touches master.
	if taskMergeGraveyardExempt(branch) {
		return TaskMergeBranchOutcome{Branch: branch, Action: "exempt",
			Detail: "archive graveyard (" + archiveBranchPrefix + "*) — never merged or deleted"}
	}

	// ⓪ the candidate's REF is re-verified FIRST — the SAME guard the merger's
	// sweep runs (repo_sweep_ref_guard.go), because this face discovers its
	// candidates the same way and shares the same race: `--all` lists refs and
	// then acts on them one at a time, and a positional name may simply be
	// wrong. Without this, a branch whose ref is already gone reaches the
	// merge-time gates, which can only answer "Not a valid object name" — the
	// 2026-09-16 ghost-ref noise this task removes — and a live row still
	// claiming it goes unrecorded. A gone ref is an ERROR here (the operator
	// named a branch that does not exist) while the sweep's tick SKIPS it (a
	// snapshot raced a concurrent round): the same evidence, the two faces'
	// honest verdicts.
	present, probeErr := repoSweepRefPresentFn(repo, branch)
	if probeErr != nil {
		return fail(probeErr)
	}
	if !present {
		evidence, recorded := repoSweepRefGoneEvidence(repo, branch)
		action := "error"
		if len(recorded) > 0 {
			action = repoSweepActionRefGone
		}
		return TaskMergeBranchOutcome{Branch: branch, Action: action,
			Detail: "no such branch ref (git " + repoSweepRefProbeVerb + " " + repoSweepRefProbeVerifyFlag +
				" " + refsHeadsPrefix + branch + repoSweepRefProbeCommitSuffix + " says so) — skipped before the merge-time gates; " + evidence}
	}

	// ⓪c the ARCHIVE-TAG arm — the same exemption the merge CANDIDATE set has
	// honoured since the merger's earliest ticks (repoSweepArchiveExempt →
	// repoSweepClassify's archive-tagged bucket), applied to the branch the
	// operator named. The sweep drops a decided loser through the tag; a
	// positional `gopi task merge <ref>` reaches no discovery filter at all, so
	// the tag has to be read here too. Reuses the ONE predicate (Rule 13), so
	// the two faces cannot disagree about who is archived. Master-safe by the
	// same construction as the arms below: nothing has touched master yet.
	if repoSweepArchiveExempt(repo, branch) {
		return TaskMergeBranchOutcome{Branch: branch, Action: "exempt",
			Detail: "archive tag " + archiveBranchPrefix + branch + " — decided loser, never merged or deleted"}
	}

	// ⓪ the MERGE-TIME CLI contract gate — asked BEFORE `git merge` runs, so a
	// refusal cannot have touched master (nothing to abort, nothing to
	// restore: the worktree is not even checked, `git show` reads objects
	// only). 2026-09-12 voice_input (bd3d34a5) landed on master with a
	// dispatch predicate its cli_contracts_test.go did not track; the drift
	// guard exists but fires only in `go test`, which the merger never runs.
	// The branch is left INTACT — the fix is a code edit, not a merge
	// resolution — so the refusal routes to the rework lane (repo_sweep
	// repoSweepRouteContractRefusals), never to a conflict resolver.
	if gate := mergeContractGate(repo, master, branch); gate.Refused {
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeContractGateActionRefused, Detail: gate.Detail}
	}

	// The target's own corpus question USED TO BE ASKED HERE and is gone with the
	// corpus: see repo_sweep.go's ⓪a note (the same lane, the same deletion).

	// ⓪b the MERGE-TIME BUILD gate — the sibling machine check, asked before
	// `git merge` for the same reason: a refusal must be master-safe by
	// construction. 2026-09-13: 82b3b6f3 merged 82b05c4f although
	// cmd/agent/merger_self_restart.go referenced ten never-defined symbols —
	// there was NO git conflict, so the merge exited 0, the merger reported
	// success, and master stopped compiling. Builds the branch and the target
	// in detached scratch worktrees and refuses only a BRANCH-INTRODUCED
	// failure (a temporarily-red master must not freeze the queue).
	//
	// Since 2026-10-12 the gate runs TWO legs on a Go module: the repo's
	// declared build AND the test-tree compile (`go test -run '^$' -count=1
	// ./...`). The second leg exists because the declared build is
	// `CGO_ENABLED=0 go build …`, which reads no `_test.go` file — measured,
	// a branch that dropped `tools.NotifyEventLedgerDivergence` from
	// internal/tools was GREEN here while `go vet ./cmd/agent/` reported
	// `undefined:` in a redproof test file, so only ⓪c's union question and
	// the after-the-fact vet gate stood between that branch and master.
	// merge_build_gate.go's mergeBuildGateLegs holds the whole argument.
	if gate := mergeBuildGate(repo, master, branch); gate.Refused {
		// ARCHIVE + NOTIFY happen HERE, at the refusal's own site — the one
		// place that knows the branch is broken. Every face that refuses
		// (the merger's sweep tick and a manual `gopi task merge`) therefore
		// leaves the same state behind: the branch tagged archive/<branch>
		// (repoSweepArchiveBranch — a tag, never `branch -d`, so the patch is
		// preserved for mining and `git tag -d` re-arms it) and ONE
		// notification row on the shared branch-rework summary (deduped per
		// branch: a second refusal of the same branch adds no second row).
		// TaskMergeOptions.Submit governs CONFLICT-ROW dispatch, not this
		// lane: the merger's self-resolve face runs with Submit=false and
		// still lands branches, so a refused branch is archived in every face.
		//
		// Order matters: the TAG first. It is what removes the branch from the
		// next tick's candidate set (repoSweepArchiveExempt →
		// repoSweepUnprotectedBranches), so an archived branch can never be
		// refused twice in a row. When the tag cannot be created the branch
		// stays a candidate and is retried on the next tick — and no
		// notification is filed, because a brief that points at an archive
		// path that does not exist is a lie.
		return taskMergeRefuseUncompilable(repo, master, branch, mergeBuildGateName, gate.Detail, gate.BranchErr, gate.BuildCmd)
	}

	// ⓪c the MERGE-TIME UNION COMPILE gate — the third machine check, asked
	// before `git merge` for the same master-safety reason, and the only one
	// that measures the MERGE RESULT rather than the branch. 2026-09-16: two
	// branches (db1fcef6, 362278e2) each added a `funcBody` helper to
	// cmd/agent's package; each built green ALONE, their union was
	// "funcBody redeclared in this block", and EVERY test in cmd/agent became
	// unrunnable on master. The ⓪b BUILD gate cannot see it by construction —
	// `go build` never compiles `_test.go` files — so this lane compiles the
	// union tree (built from git objects in an alternate index, extracted to
	// scratch) with `go test -run '^$' -count=1 ./...` + `go vet ./...`, and
	// refuses only when the branch's OWN test build PASSES (measured, not
	// assumed) — a branch that fails alone is the ⓪b lane's case.
	if gate := mergeUnionGate(repo, master, branch); gate.Refused {
		return taskMergeRefuseUncompilable(repo, master, branch, mergeUnionGateName, gate.Detail, gate.BranchErr, gate.BuildCmd)
	}

	// ⓪d the MERGE-TIME MANIFEST gate — the FOURTH machine check, asked before
	// `git merge` for the same master-safety reason. 2026-09-26 (master
	// 79807932): devdoc/DOC-INDEX.tsv carried the SAME row key at line 10 and
	// line 762 because two branches each added a row for one doc at different
	// positions and git's three-way merge UNIONED them (+1/-0 against BOTH
	// parents — no conflict for a resolver to see). The ⓪c lane cannot catch
	// it: its own union builder (`read-tree -m`) leaves this shape unmerged and
	// abstains, so this gate predicts the union with `git merge-file` instead.
	// The manifest is read from the SIDES' git objects, never from a worktree,
	// and a refusal names the key with BOTH line numbers in the standards
	// gate's own shape.
	if gate := manifestUnionGate(repo, master, branch); gate.Refused {
		return taskMergeRefuseManifest(repo, master, branch, gate.Manifest, gate.Detail)
	}

	// ⓪e the MERGE-TIME PUBLISHED-ANCHOR gate — the FIFTH machine check, asked
	// before `git merge` for the same master-safety reason. 2026-10-13: a branch
	// commit moved `cmd/agent/cli_log.go`'s routing predicate 602 → 611 and
	// regenerated COMMANDS.md against a PRE-FINAL state of that same commit; the
	// landing that carried it (merge c52dad2d2, 00:57:04) is RED at its own
	// revision (`COMMANDS.md` names `cli_log.go:607` while the source has the
	// declaration at 611, and 607 is a BLANK line). The measurement existed and
	// ran on every landing (`RebuildAfterMergeMergeset` → mergeset_gendoc.go,
	// whose union DID contain the file and whose edge DID name
	// internal/standards) but it is REPORT-ONLY by contract (Rule 12), and the
	// arm that used to refuse at the delivery point (task_finish_gendoc.go) was
	// deleted 73 minutes before the landing. So the drift crossed a seam that
	// both halves had measured: this lane asks the same edge about the MERGE
	// RESULT — the tree nobody has checked out — and REFUSES a union whose
	// document lags its anchor sources, master untouched, before anything lands.
	// A red TARGET and a branch that fails alone both ABSTAIN (measured at the
	// time: the integration ref this lane was written against was itself red on
	// the generated-document tests, since deleted with internal/docclass in
	// 5a35f61), so the lane cannot freeze the queue for a failure no
	// candidate caused.
	if gate := mergeAnchorGate(repo, master, branch); gate.Refused {
		return taskMergeRefuseAnchor(repo, master, branch, gate.Detail, gate.Findings, gate.Mandate)
	}

	// ① already merged? merge-base --is-ancestor exits 0 when the branch
	// tip is IN master — prune the ref (branch -d, git's own double lock).
	// exit-1 = not an ancestor (fall through to the merge), any other
	// failure (128 = unknown revision, wedged repo) = a real error.
	if _, err := taskMergeGit(repo, "merge-base", "--is-ancestor", branch, master); err != nil {
		if !isGitExit1(err) {
			// A repo still carrying an unmerged index (a prior conflict
			// resolution in flight) wedges merge-base too — surface THAT,
			// not a bare 128.
			if state, sErr := taskMergeGit(repo, "ls-files", "-u"); sErr == nil && strings.TrimSpace(state) != "" {
				return TaskMergeBranchOutcome{Branch: branch, Action: "error",
					Detail: "repo has unmerged index entries — resolve/abort the in-flight merge first"}
			}
			return fail(err)
		}
	} else {
		// The audit record goes BEFORE the delete (package removalaudit): the
		// evidence here is the ancestry probe that just succeeded.
		removalaudit.BranchDelete(removalaudit.ArmTaskMergeBranch, repo, branch, "",
			"tip is an ancestor of "+master)
		// lock ② — the same question asked at the ref as it stands NOW (epic
		// 01a12f68): the ancestry probe above is a READ taken a moment ago, and
		// `git branch -d` is not a substitute for it — git consults the branch's
		// UPSTREAM first, and a branch whose upstream is a scratch mirror is
		// deleted with exit 0 (measured). The branch is KEPT, never errored (its
		// contract: keep it, produce a merge row).
		if why := branchDeleteNonDurableTipRefusal(repo, branch, master); why != "" {
			return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionDeleted,
				Detail: "already merged into " + master + " — branch kept: " + why}
		}
		if _, err := taskMergeGit(repo, "branch", "-d", branch); err != nil {
			return fail(err)
		}
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionDeleted,
			Detail: "already merged into " + master + " — branch -d"}
	}

	// ② record master's tip BEFORE the landing: the post-merge verify gate's
	// restore point. Never re-derived after the merge — the pre-merge tip is
	// the only state a failed verdict may return to.
	preTip, _ := taskMergeGit(repo, "rev-parse", "HEAD") //stylegate:ignore diagnostics-grade probe; an empty tip only disables the gate's revert for this branch

	// ③ try the real merge (ort). Clean → git auto-commits.
	_, mergeErr := taskMergeGit(repo, "merge", branch)
	if mergeErr == nil {
		// ④a landed: prune the branch (-d: git refuses if not ancestor).
		// Audit record BEFORE the delete; the evidence is the landing itself.
		removalaudit.BranchDelete(removalaudit.ArmTaskMergeBranch, repo, branch, "",
			"the merge just landed this branch in "+master)
		if why := branchDeleteNonDurableTipRefusal(repo, branch, master); why != "" {
			return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionMerged, PreMergeTip: preTip,
				Detail: "merged, but the ref was kept: " + why}
		}
		if _, err := taskMergeGit(repo, "branch", "-d", branch); err != nil {
			return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionMerged, PreMergeTip: preTip,
				Detail: "merged, but branch -d failed: " + err.Error()}
		}
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionMerged, PreMergeTip: preTip, Detail: "clean merge, branch deleted"}
	}

	// ③b conflict (or wedged tree): abort — the tree may hold partial
	// index state either way, abort MUST run before anything else. A
	// failed merge that left NO merge state (e.g. "not something we can
	// merge" pre-flight failure) aborts with "There is no merge to
	// abort" — report the original failure instead of dispatching.
	if _, abortErr := taskMergeGit(repo, "merge", "--abort"); abortErr != nil {
		if !isGitNoMergeToAbort(abortErr) {
			return fail(fmt.Errorf("merge --abort: %w", abortErr))
		}
		// mergeErr IS the chain's cause: the caller's diagnosis (and any
		// errors.Is/As on git's failure) must be able to reach it. This line
		// carried %v with an inline "deliberately outside the chain" escape;
		// that escape was a FALSE positive — a2bf8ef4 had already fixed this
		// site to %w, and the master-canonical merge resolution silently took
		// the %v side back, after which the escape silenced the regression
		// instead of reverting it (found 2026-09-18, fix/standards-gate-red;
		// internal/stylegate's errorf-verb ceiling was red over the same site).
		return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %w", branch, mergeErr))
	}
	if !opts.Submit {
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeConflictActionConflict,
			Detail: "merge conflict — report-only (no --submit): nothing enqueued"}
	}
	action, detail := taskMergeDispatchConflict(opts, repo, master, branch)
	return TaskMergeBranchOutcome{Branch: branch, Action: action, Detail: detail}
}

// contractGateRefuseFmt renders the self-contained branch-rework briefing for
// a branch the merge-time CLI contract gate refused. The refusal is NOT a
// conflict: the branch merges fine and merely fails a contract check, so a
// conflict resolver would spend a 15-minute child on the wrong problem. The
// briefing names the EXACT drift the gate measured and the fix (edit the
// contract suite in THIS branch), and it warns against the two wrong
// reactions (merge master in / delete the branch).
const contractGateRefuseFmt = `You are the merger's BRANCH-REWORK agent for repo %q. Branch %q is REFUSED at merge time by the CLI contract gate (master %q is untouched and the branch is intact).

WHY the merger refuses it (measured by the gate, token-free):
%s

WHAT THIS MEANS: the branch merges cleanly — there is no conflict to resolve. What it fails is the project's CLI contract: cmd/agent/main.go dispatches ` + "`gopi <verb>`" + ` through a chain of isXxxSubcommand predicates, and cmd/agent/cli_contracts_test.go holds that chain's mirror (cliContractDispatchPredicates) plus the usage-inventory cases. A new predicate without its mirror entry turns TestCLIContractInventoryMatchesDispatch RED on master the moment this branch lands (the 2026-09-12 voice_input incident, bd3d34a5).

TASK:
1. Check out branch %q in repo %q (do NOT merge master into it and do NOT delete it — both would hide or destroy the deliverable).
2. Add the missing entry to cliContractDispatchPredicates in cmd/agent/cli_contracts_test.go, IN THE SAME ORDER as the new predicate appears in main.go's dispatch chain, and add the matching cliContractInventory case (argv + args + usage fingerprint) for the new subcommand. If the gate reported the opposite direction (a predicate main.go no longer wires), remove the stale mirror entry and its inventory case instead.
3. Verify from a checkout of the branch with the project's own commands: ` + "`go build ./...`" + ` and ` + "`go test ./cmd/agent/ -run TestCLIContract -count=1`" + ` must both pass.

When the contract suite tracks the branch's dispatch chain and those commands pass, you are done — the merger will land the branch on the next tick.`

// buildGateRefuseFmt renders the self-contained branch-rework briefing for a
// branch the merge-time BUILD gate refused. It is the BUILD gate's sibling of
// contractGateRefuseFmt and differs where the two REFUSALS differ: the contract
// gate leaves a fixable branch intact (edit it in place), while this one
// ARCHIVES a broken branch — so the brief must send the reworker to a NEW
// branch off master and tell it not to edit the archived predecessor.
//
// Its six ELEMENTS are the operator's own list (2026-09-13: "the row must
// carry (1) the branch, (2) the real build error, (3) where the patch now
// lives — the archive tag, since the branch is gone from the queue, (4) a
// warning not to fix the predecessor in place, (5) the instruction to build a
// new branch, (6) how to revive the archived one if that is genuinely what
// they want"), because a reworker claiming this row has NO other context: the
// branch is not a merge candidate any more and the compiler's message is the
// only evidence of what broke.
const buildGateRefuseFmt = `You are the merger's BRANCH-REWORK agent for repo %q. Branch %q was refused at MERGE TIME because it DOES NOT COMPILE, and the merger ARCHIVED it in the same tick. Master (%q) is untouched and its own build was verified green, so the breakage is the branch's.

ELEMENT 2 — THE MEASURED BUILD ERROR (the compiler's own text, %s):
%s

THE OTHER FIVE ELEMENTS, in the order they matter:
1. WHERE THE PATCH IS: the branch ref %q still exists, and the merger marked it a DECIDED LOSER with the tag %s (git rev-parse %s). An archived branch is exempt from every merge path, so nothing will land it while that tag is there — the tag is where the deliverable lives now.
2. DO NOT EDIT THE PREDECESSOR IN PLACE: do not check out %q and fix it there, and do not push a "make it compile" commit onto it. Reviving an archived ref behind the archive decision is how a losing branch silently re-enters the merge queue. Read its diff if you need it — then build the fix as its own deliverable.
3. START A NEW BRANCH OFF CURRENT master in repo %q: create a fresh branch, carry over %q's INTENT (its diff is the specification, NOT its tip — the tip is the code that does not compile), and make that code build.
4. VERIFY WITH THE COMMAND THAT REFUSED IT: from a checkout of your new branch, run %s and make it exit 0. That is the exact command the merger runs before every landing.
5. DONE MEANS: your new branch builds (%s exits 0) and merges cleanly into master — then the merger lands it on the next tick.
6. REVIVAL (only if you deliberately want to re-arm THIS archived branch instead): %s removes the archive tag and puts %q back in the merge queue — it will be refused again unless it also builds, which is why element 3 is the path that works.`

// The two merge-time compile gates are ONE lane — the refusal, the archive, the
// notification row and the six-element rework brief are identical, because the
// fix a reworker owes is the same (code that must compile) — but TWO
// measurements. An operator reading the merger log must be able to tell WHICH
// measurement refused the branch: its own build (⓪b) or the MERGE RESULT's
// (⓪c). Rule 1b: the spelling lives in exactly one place.
const (
	mergeBuildGateName = "BUILD gate"
	mergeUnionGateName = "UNION COMPILE gate"
)

// taskMergeRefuseUncompilable is the shared refusal arm of the two merge-time
// compile gates. ARCHIVE + NOTIFY happen HERE, at the refusal's own site — the
// one place that knows the branch is broken — so every face that refuses (the
// merger's sweep tick and a manual `gopi task merge`) leaves the same state
// behind: the branch tagged archive/<branch> (repoSweepArchiveBranch — a tag,
// never `branch -d`, so the patch is preserved for mining and `git tag -d`
// re-arms it) and ONE notification row on the shared branch-rework summary
// (deduped per branch: a second refusal of the same branch adds no second row).
// TaskMergeOptions.Submit governs CONFLICT-ROW dispatch, not this lane: the
// merger's self-resolve face runs with Submit=false and still lands branches, so
// a refused branch is archived in every face.
//
// Order matters: the TAG first. It is what removes the branch from the next
// tick's candidate set (repoSweepArchiveExempt → repoSweepUnprotectedBranches),
// so an archived branch can never be refused twice in a row. When the tag cannot
// be created the branch stays a candidate and is retried on the next tick — and
// no notification is filed, because a brief that points at an archive path that
// does not exist is a lie.
func taskMergeRefuseUncompilable(repo, master, branch, gateName, detail, buildErr, buildCmd string) TaskMergeBranchOutcome {
	if aerr := repoSweepArchiveBranch(repo, branch, ArchiveReasonCompileFailure); aerr != nil {
		taskNotifyLog("merger: branch %s refused by the %s but ARCHIVE FAILED: %v (branch stays a candidate)", branch, gateName, aerr)
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeBuildGateActionRefused,
			Detail:   detail + " (archive failed: " + aerr.Error() + ")",
			BuildErr: buildErr, BuildCmd: buildCmd}
	}
	action, submitDetail := buildGateSubmitRework(repo, master, branch, buildCmd, buildErr)
	notified := action == "rework-submitted"
	taskNotifyLog("merger: branch %s refused by the %s — archived as %s, notification row: %s",
		branch, gateName, archiveBranchPrefix+branch, action)
	return TaskMergeBranchOutcome{Branch: branch, Action: mergeBuildGateActionRefused,
		Detail:   detail + " — archived as " + archiveBranchPrefix + branch + ", notification row: " + action + " (" + submitDetail + ")",
		BuildErr: buildErr, BuildCmd: buildCmd, BuildNotified: notified}
}

// manifestUnionGateRefuseFmt renders the self-contained branch-rework briefing
// for a branch the merge-time MANIFEST gate refused. It is the third member of
// the family (contractGateRefuseFmt, buildGateRefuseFmt) and its shape follows
// the BUILD gate's — the branch is ARCHIVED here too, so the reworker is sent to
// a NEW branch off master rather than told to edit the archived predecessor.
//
// It carries the gate's own evidence block verbatim: the duplicate's key and
// BOTH line numbers in the standards gate's spelling, which is the one thing a
// reworker cannot reconstruct from the branch alone (the union is a THIRD file
// that exists in neither side's checkout).
const manifestUnionGateRefuseFmt = `You are the merger's BRANCH-REWORK agent for repo %q. Branch %q was refused at MERGE TIME because the MERGE RESULT would write a DUPLICATED row key in %s, and the merger ARCHIVED it in the same tick. Master (%q) is untouched.

ELEMENT 2 — THE MEASURED DUPLICATE (the standards gate's own shape; %s carries the key twice):
%s

WHY A MERGE PRODUCES THIS, and why neither side is "wrong" on its own: each side added its own row for the same key at a DIFFERENT position in the table, so the two hunks do not overlap — git UNIONS them instead of raising a conflict, and the merged table carries the key twice. Read the evidence lines above before editing: they name the two lines in the union's text, and the note beside them quotes each side's row.

THE FIX (a table edit, not a code fix):
1. Work on a NEW branch off master — do NOT edit the archived branch %q in place. The archive tag refs/tags/%s%s is the decision, and ` + "`git tag -d %s`" + ` revives the OLD ref if that is genuinely what you want.
2. Keep exactly ONE row for the duplicated key: the copy that descends from the original write (its position and text are the ones the corpus has been reading), and delete the other. When the note says both sides carry identical text, no content can tell the copies apart — decide on the position the row has held, and say in the commit message which copy you kept and why.
3. Re-run the repo's own gates before pushing: ` + "`go build ./...`" + ` and ` + "`go test ./internal/tools/ -run 'TestManifestUnionGate' -count=1`" + ` must both pass — the union gate is the one that reads the table you are editing (the corpus coverage/banner gates this step used to name were deleted with the document pipeline; the union gate is what remains and it is the one that refused this branch).

When those pass, the merger will land the new branch on the next tick.
`

// manifestUnionGateSubmitRework enqueues the branch-rework fix task for a branch
// the MANIFEST gate refused, deduping through the SAME deterministic summary
// every merge-time refusal lane uses (resolveRowReworkSummaryPrefix + branch) —
// one branch, ONE rework row, however it was refused. Returns (action, detail)
// in taskMergeDispatchConflict's vocabulary.
func manifestUnionGateSubmitRework(repo, master, branch, manifest, detail string) (string, string) {
	summary := resolveRowReworkSummaryPrefix + branch
	blocked, why, err := reworkSummaryBlocked(summary)
	if err != nil {
		return "error", err.Error()
	}
	if blocked {
		return "duplicate", fmt.Sprintf(reworkGateDuplicateDetailFormat, branch, why)
	}
	tag := archiveBranchPrefix + branch
	payload := fmt.Sprintf(manifestUnionGateRefuseFmt,
		repo, branch, manifest, master, manifest, detail,
		branch, archiveBranchPrefix, branch, tag)
	_, reworkID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    resolveRowReworkTags,
		Source:  taskMergeSourceAuto,
		Repo:    repo,
	})
	if err != nil {
		return "error", "submit: " + err.Error()
	}
	if derr := resolveRowStampDeliverableBranch(repo, branch); derr != nil {
		taskNotifyLog("merger: manifest-refusal deliverable.branch stamp skipped: %v", derr)
	}
	// Rule 33's supersession edge (see rework_reparent_children.go): the row
	// this branch's work was falsified under must stop satisfying its children.
	reworkReparentFromBranch(repo, branch, reworkID, reworkLaneName)
	return "rework-submitted", "duplicate manifest row key → branch-rework fix task (branch archived as " + tag + ", master untouched)"
}

// taskMergeRefuseManifest is the MANIFEST gate's refusal arm: ARCHIVE + NOTIFY at
// the refusal's own site, so both faces that refuse (the merger's sweep tick and
// a manual `gopi task merge`) leave identical state behind.
//
// The BUILD gate's helper is deliberately NOT reused: that one mints a row whose
// payload is the COMPILER's own text plus the command to re-run — a manifest
// duplicate is a TABLE edit with no compiler text at all, and a brief carrying
// two empty build fields would be the "a brief that points at nothing is a lie"
// defect the build gate's own archive ordering exists to avoid.
func taskMergeRefuseManifest(repo, master, branch, manifest, detail string) TaskMergeBranchOutcome {
	if aerr := repoSweepArchiveBranch(repo, branch, ArchiveReasonManifestDuplicate); aerr != nil {
		taskNotifyLog("merger: branch %s refused by the %s but ARCHIVE FAILED: %v (branch stays a candidate)",
			branch, ManifestUnionGateName, aerr)
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeBuildGateActionRefused,
			Detail: detail + " (archive failed: " + aerr.Error() + ")"}
	}
	action, submitDetail := manifestUnionGateSubmitRework(repo, master, branch, manifest, detail)
	taskNotifyLog("merger: branch %s refused by the %s — archived as %s, notification row: %s",
		branch, ManifestUnionGateName, archiveBranchPrefix+branch, action)
	return TaskMergeBranchOutcome{Branch: branch, Action: mergeBuildGateActionRefused,
		Detail:        detail + " — archived as " + archiveBranchPrefix + branch + ", notification row: " + action + " (" + submitDetail + ")",
		BuildNotified: action == "rework-submitted"}
}

// buildGateReviveCmdFmt renders the revival command element (6) in git's own
// spelling — `git tag -d <archive tag>` — so the briefing never paraphrases the
// one command that re-arms a branch (Rule 1: the archive namespace has ONE
// spelling, archiveBranchPrefix).
const buildGateReviveCmdFmt = "git tag -d %s"

// buildGateArchiveTagFmt renders the archived ref element (1).
const buildGateArchiveTagFmt = "refs/tags/%s"

// buildGateSubmitRework enqueues the branch-rework fix task for a branch the
// BUILD gate refused, deduping through the SAME deterministic summary the
// resolve-failure and contract-refusal lanes use
// (resolveRowReworkSummaryPrefix + branch) — one branch, ONE rework row,
// however it was refused — and stamping the branch into the fresh row's
// deliverable through the shared helper. Returns (action, detail) in
// taskMergeDispatchConflict's vocabulary.
func buildGateSubmitRework(repo, master, branch, buildCmd, buildErr string) (string, string) {
	summary := resolveRowReworkSummaryPrefix + branch
	blocked, why, err := reworkSummaryBlocked(summary)
	if err != nil {
		return "error", err.Error()
	}
	if blocked {
		return "duplicate", fmt.Sprintf(reworkGateDuplicateDetailFormat, branch, why)
	}
	tag := archiveBranchPrefix + branch
	payload := fmt.Sprintf(buildGateRefuseFmt,
		repo, branch, master,
		buildCmd, buildErr,
		branch, fmt.Sprintf(buildGateArchiveTagFmt, tag), fmt.Sprintf(buildGateArchiveTagFmt, tag),
		branch,
		repo, branch,
		buildCmd,
		buildCmd,
		fmt.Sprintf(buildGateReviveCmdFmt, tag), branch)
	_, reworkID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    resolveRowReworkTags,
		Source:  taskMergeSourceAuto,
		Repo:    repo,
	})
	if err != nil {
		return "error", "submit: " + err.Error()
	}
	if derr := resolveRowStampDeliverableBranch(repo, branch); derr != nil {
		taskNotifyLog("merger: build-refusal deliverable.branch stamp skipped: %v", derr)
	}
	// Rule 33's supersession edge (see rework_reparent_children.go): the row
	// this branch's work was falsified under must stop satisfying its children.
	reworkReparentFromBranch(repo, branch, reworkID, reworkLaneName)
	return "rework-submitted", "build failure → branch-rework fix task (branch archived as " + tag + ", master untouched)"
}

// reworkSummaryLive reports whether a LIVE task row (pending/claimed/
// in_progress) already carries summary — the ONE dedup probe every submission
// through the branch-rework lane shares. The contract gate's refusal and the
// build gate's refusal MUST NOT mint two rows for the same branch: the summary
// is the branch's identity in that lane (resolveRowHasOpenRework reads the same
// one), so one probe keeps the two gates from racing each other into a
// duplicate.
// reworkSummaryLive reports whether the branch-rework lane already owns
// summary — through a LIVE row OR through one HANDED OFF to a peer. It is the
// boolean projection of the lane's ONE dedup entry (reworkSummaryBlocked) for
// callers that need only the answer; a caller that RENDERS a reason to an
// operator takes the two-value form, so its line names the fact that stopped it
// while this one stays the single implementation (Rule 14).
//
// The contract gate's refusal and the build gate's refusal MUST NOT mint two
// rows for the same branch: the summary is the branch's identity in that lane
// (resolveRowHasOpenRework reads the same one), so one probe keeps the gates
// from racing each other into a duplicate.
func reworkSummaryLive(summary string) (bool, error) {
	blocked, _, err := reworkSummaryBlocked(summary)
	return blocked, err
}

// contractGateSubmitRework enqueues the branch-rework fix task for a branch
// the CLI contract gate refused, deduping through the SAME deterministic
// summary the resolve-failure rework lane uses (resolveRowReworkSummaryPrefix
// + branch) — so one branch has ONE rework row however it was refused, and
// resolveRowHasOpenRework holds the gate until the reworker closes it.
// Returns (action, detail) in taskMergeDispatchConflict's vocabulary.
func contractGateSubmitRework(repo, master, branch, driftDetail string) (string, string) {
	summary := resolveRowReworkSummaryPrefix + branch
	blocked, why, err := reworkSummaryBlocked(summary)
	if err != nil {
		return "error", err.Error()
	}
	if blocked {
		return "duplicate", fmt.Sprintf(reworkGateDuplicateDetailFormat, branch, why)
	}
	payload := fmt.Sprintf(contractGateRefuseFmt, repo, branch, master, driftDetail, branch, repo)
	_, reworkID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    resolveRowReworkTags,
		Source:  taskMergeSourceAuto,
		Repo:    repo,
	})
	if err != nil {
		return "error", "submit: " + err.Error()
	}
	if derr := resolveRowStampDeliverableBranch(repo, branch); derr != nil {
		taskNotifyLog("merger: contract-refusal deliverable.branch stamp skipped: %v", derr)
	}
	// Rule 33's supersession edge: the branch's ORIGINAL row was falsified by
	// this refusal, so every row depending on it is re-pointed at the rework
	// row — otherwise the original row's release status starts them on the
	// premise this refusal just disproved (ONE shared implementation).
	reworkReparentFromBranch(repo, branch, reworkID, reworkLaneName)
	return "rework-submitted", "CLI contract drift → branch-rework fix task (branch " + branch + " preserved, master untouched)"
}

// repoSweepRouteBuildRefusals tallies the merge-time BUILD gate's refusals for
// the sweep report — the sibling of repoSweepRouteContractRefusals, with one
// deliberate difference: it WRITES NOTHING. The archive and the notification
// row are performed at the refusal's own site (taskMergeOne), so both the
// merger's tick and a manual `gopi task merge` leave identical state behind and
// no second failure mechanism exists to disagree with the first. This pass
// therefore only reports: how many branches the gate refused, how many of them
// are now archived (the tag is the ground truth, re-read here rather than
// assumed), and how many filed a notification row.
func repoSweepRouteBuildRefusals(repo string, outcomes []TaskMergeBranchOutcome, res *RepoSweepResult) {
	for _, o := range outcomes {
		if o.Action != mergeBuildGateActionRefused {
			continue
		}
		res.BuildRefused++
		// The tag is the fact; a refusal whose archive failed stays a candidate
		// for the next tick and is reported as such (never counted as archived).
		if repoSweepArchiveExempt(repo, o.Branch) {
			res.BuildArchived++
		} else {
			taskNotifyLog("merger: branch %s was refused by the BUILD gate but is NOT archived — it stays a merge candidate and will be refused again", o.Branch)
		}
		if o.BuildNotified {
			res.ReworkSubmitted++
		}
		taskNotifyLog("merger: BUILD gate refused %s (%s)", o.Branch, o.Detail)
	}
}

// repoSweepRouteContractRefusals is the post-merge routing pass for the
// merge-time CLI contract gate. TaskMerge REFUSES drift branches before any
// git merge runs and merges everything else; the refusals are surfaced here,
// right after the merge pass, so the SAME tick that refused a branch also
// routes it into the existing failure lane. At most ONE refusal is dispatched
// per tick (the requirement #3 discipline the conflict lane follows); the
// rework row then holds the gate (resolveRowHasOpenRework) so no second row is
// minted while a reworker owns the branch.
//
// Deduped and fail-soft: a duplicate (a row already live, including the same
// refusal on an earlier tick) and a submit error both degrade to a
// "duplicate"/"error" outcome with a log line — the branch stays a candidate
// and master is untouched either way.
func repoSweepRouteContractRefusals(repo string, outcomes []TaskMergeBranchOutcome, res *RepoSweepResult) {
	var refused []string
	for _, o := range outcomes {
		if o.Action == mergeContractGateActionRefused {
			refused = append(refused, o.Branch)
		}
	}
	if len(refused) == 0 {
		return
	}
	sort.Strings(refused)
	branch := refused[0]
	detail := ""
	for _, o := range outcomes {
		if o.Branch == branch && o.Action == mergeContractGateActionRefused {
			detail = o.Detail
			break
		}
	}
	action, dispatchDetail := contractGateSubmitRework(repo, "master", branch, detail)
	res.Outcomes = append(res.Outcomes, TaskMergeBranchOutcome{Branch: branch, Action: action, Detail: dispatchDetail})
	res.ContractRefused++
	if action == "rework-submitted" {
		res.ReworkSubmitted++
	}
	taskNotifyLog("merger: branch %s refused by the CLI merge-time contract gate (%s)", branch, action)
}

// taskMergeDispatchConflict enqueues ONE conflict-resolution task for the
// branch (dedup: a LIVE row with the same summary is a duplicate, and a
// TERMINAL one is a concluded decision — F6; same-branch-once, ⑤). Returns
// (action, detail): dispatched | duplicate | concluded | error.
func taskMergeDispatchConflict(opts TaskMergeOptions, repo, master, branch string) (string, string) {
	tip, err := taskMergeGit(repo, "log", "-1", "--format=%h", branch)
	if err != nil {
		return mergeConflictActionError, "resolve tip: " + err.Error()
	}
	ctx := context.Background()
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		return mergeConflictActionError, "open ledger: " + err.Error()
	}
	defer db.Close()
	summary := fmt.Sprintf(taskMergeSummaryVerb, branch)
	live, terminal, err := resolveConflictRowIdentity(ctx, branch)
	if err != nil {
		return mergeConflictActionError, "dedup query: " + err.Error()
	}
	if terminal {
		// F6: this branch is already NAMED by a terminal resolve row — the
		// decision was taken and executed; a fresh row would queue the same
		// resolution again (the 7-rows-in-3-hours case the audit measured).
		// Mint nothing. A branch whose attempt FAILED travels the branch-rework
		// lane instead (contractGateSubmitRework / the build gate), which owns
		// "the resolver could not land it".
		return mergeConflictActionConcluded, "concluded conflict task already judged " + branch
	}
	if live {
		return mergeConflictActionDuplicate, "live conflict task already queued for " + branch
	}
	// F5 lane ownership, rule 2: if the branch-rework lane is ALREADY holding
	// this branch (a live rework row), the resolution job is the author's and
	// this lane defers — minting here would hand one branch to two resolvers.
	// Checked only when the resolve summary is free, so the deferral never
	// masks a duplicate.
	state, owned, oerr := reworkRowLiveState(ctx, branch)
	if oerr != nil {
		return mergeConflictActionError, "rework dedup query: " + oerr.Error()
	}
	if owned {
		return mergeConflictActionReworkOwned,
			state + ": branch-rework lane owns " + branch + " — merge-time conflict lane defers"
	}
	payload := taskMergeConflictPayload(repo, master, branch, tip)
	source := opts.Source
	if source == "" {
		source = taskMergeSourceAuto
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    taskMergeTagMergeConflict + "," + taskMergeTagAutoDispatch,
		Source:  source,
		Repo:    repo,
	}); err != nil {
		return mergeConflictActionError, "submit: " + err.Error()
	}
	return mergeConflictActionDispatched, fmt.Sprintf("conflict → %s task (tip %s)", taskTagMergeConflict, tip)
}

// taskMergeConflictPayload renders the self-contained briefing (the P2.5
// precedent: the executor resolves the conflict in its OWN session — the
// payload carries the conflict scene, never an in-process hunk judgment).
func taskMergeConflictPayload(repo, master, branch, tip string) string {
	var b strings.Builder
	fmt.Fprintf(&b, taskMergePayloadBranchLine+" does not merge cleanly into %s in repo %s.\n\n",
		branch, tip, master, repo)
	if files, err := taskMergeGit(repo, "diff", "--name-only", master+"..."+branch); err == nil && strings.TrimSpace(files) != "" {
		fmt.Fprintf(&b, "Branch-vs-master changed files:\n%s\n", strings.TrimSpace(files))
	}
	if stat, err := taskMergeGit(repo, "diff", "--stat", master+"..."+branch); err == nil && strings.TrimSpace(stat) != "" {
		fmt.Fprintf(&b, "\nDiff summary (git diff %s...%s --stat):\n%s\n", master, branch, strings.TrimSpace(stat))
	}
	b.WriteString(`
Task: merge this branch into ` + master + `, resolving conflicts in your
own session (read the conflict hunks, choose sides, verify the result).
Acceptance: the merge completes, the project builds, the test suite
passes. Do not force-push; commit the resolution normally.`)
	return b.String()
}
