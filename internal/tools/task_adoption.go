package tools

// Branch adoption chain (branch-adoption-chain epic, 2026-08-31): the last
// piece of the two-developer merge loop. When a client pushes a completed
// task's branch back to the server repo, the server's serve tick picks it
// up, TRIES the merge, and either lands it (clean → commit + branch -d,
// zero tokens) or dispatches a self-contained conflict-resolution task the
// client's agent picks up in its OWN session (P2.5 t-1788063202 precedent:
// 8 conflicts solved in one self-contained briefing). Architecture red
// line ④: the Go code only DISCOVERS / TRIES / DISPATCHES / ADOPTS — every
// semantic judgment (reading conflict hunks, choosing sides) happens in
// the executor agent's session, never inside this process.

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/gitcommit"
	"github.com/nickhuang99/gopi/internal/removalaudit"
)

// Adoption tunables (Rule 1: every literal in one place).
const (
	// adoptionGitTimeout caps every single git invocation (a wedged repo
	// must not stall the serve pass; the pass runs the whole chain
	// synchronously before the claim loop).
	adoptionGitTimeout = 30 * time.Second

	// adoptionSummaryPrefix stamps auto-adopted merge commits so humans can
	// tell them apart from hand merges in git log.
	adoptionSummaryPrefix = "adoption: merge "

	// taskTagMergeConflict routes the dispatched conflict task to the
	// highest priority tier via taskTagPriority — a blocked adoption chain
	// is queue-critical (epic ⑤). Declared here so tests and callers share
	// the spelling; the priority MAPPING stays the single taskTagPriority.
	//
	// This is the package's SINGLE literal for the merge-conflict tag value.
	// Same-package consumers that reference the value alias this const
	// (taskMergeTagMergeConflict in task_merge.go, resolveRowTagMergeConflict
	// in repo_resolve_row.go) so a rename can never desync the resolve
	// exclusion gate (resolveRowIsMergerOwned) from the row creators.
	taskTagMergeConflict = "merge-conflict"
)

// taskTagTokenSQLTest is THE definition of "this row's tags carry <tag> as a
// WHOLE token" for every SQL reader of the comma-separated tags column
// (Rule 14: one shared fragment, no per-query reinvention; Rule 1b: the
// comma-anchored idiom is never re-typed inline). It renders a boolean SQL
// expression; the caller appends the `AS <alias>` it scans into.
//
// The bug this closes (⑤ same-branch-once false positive): a raw
// `tags LIKE '%merge-conflict%'` substring match also matches a MID-TOKEN
// tag (merge-conflictz, xmerge-conflict), so the guard reported a live
// conflict row for a branch that had none and ③b skipped dispatching the
// conflict task the branch actually needed. Comma-padding tests membership
// instead of containment.
//
// GLOB, never LIKE: SQLite's LIKE is case-INSENSITIVE, so a `%,tag,%` LIKE
// would match a differently-cased tag that the Go token readers
// (resolveRowIsMergerOwned, isTaskResolved) reject by exact comparison —
// the mismatch is the same class of defect one case-fold over. GLOB is
// case-sensitive, matching Go, and `*` is its wildcard here.
//
// Closes over the caller's existing string arguments without adding an
// argument list, so the LIKE→GLOB conversion at each site stays one line.
func taskTagTokenSQLTest(tag string) string {
	return "(','||COALESCE(tags,'')||',') GLOB '*," + tag + ",*'"
}

// AdoptionOptions configures one serve-tick adoption pass.
type AdoptionOptions struct {
	// Repo is the git repo to sweep ("" = the process cwd — the normal
	// server-side serve lives inside the repo checkout).
	Repo string
	// MasterBranch is the integration branch commits merge INTO
	// ("" = "master", the repo's convention).
	MasterBranch string
	// Submit wires ③b: when true a conflicted branch dispatches a
	// merge-conflict task into the ledger; false = probe-only report.
	Submit bool
	// Source is the provenance stamped on dispatched rows
	// ("" = taskMergeSourceAuto, the merge path's own role — see
	// internal/tasksource; tests may override).
	Source string

	// TryMerge is the test seam for ② the probe merge. nil = real
	// `git merge --no-commit --no-ff` (same injection pattern as
	// HarvestOptions.Fetch). It receives the repo path and branch name.
	TryMerge func(repo, branch string) error
}

// AdoptionCounts is the per-pass tally the serve log prints.
type AdoptionCounts struct {
	Scanned  int      // remote branches examined
	Adopted  []string // ③a clean → merged + branch deleted
	Dispatch []string // ③b conflict → merge-conflict task dispatched
	Duped    int      // conflict task already live for the branch (⑤)
	// Concluded names the branches skipped because a TERMINAL resolve row
	// already judged them (F6): the decision is on the record, so no second
	// resolution is queued. Named, never folded into Duped — "the work is
	// judged" and "the work is queued" are different facts about a branch.
	Concluded []string
	Skipped   int // not linked to a completed task (or already merged)
	// RefKept names the branches whose ref deletion git REFUSED even though the
	// adoption LANDED (the branch is checked out in a linked worktree: measured
	// `git branch -d <b>` → exit 1 "Cannot delete branch '<b>' checked out at
	// <path>"). It is deliberately NOT Failed: master filed this expected,
	// benign outcome through the same channel as a real error, so every serve
	// pass logged `failed=1` for a healthy landing (Rule 15b — a channel that
	// cries wolf is a channel nobody reads). The ref is deleted later, by the
	// merged-cleanup pass that owns the worktree.
	RefKept []string
	// AlreadyIntegrated names the candidates whose tip master ALREADY
	// contains: the probe merged nothing ("Already up to date."), so no
	// adoption commit was owed and the ref prune is the only remaining work.
	// Each entry carries the prune command that clears it, and the leftover
	// ref is PRUNED in the pass (falling back to this list only when git
	// refuses — a branch checked out in a linked worktree, in which case the
	// caller renders it through RefKept).
	AlreadyIntegrated []string
	Failed            []string // per-branch errors
}

// adoptionRefKeptMarker is how a landed-but-ref-kept adoption is reported (one
// spelling for both report faces — Rule 1b/13).
const adoptionRefKeptMarker = "landed; branch ref kept (checked out in a linked worktree — merged-cleanup deletes it once the worktree is dead)"

// adoptionBranch is one candidate discovered in ①.
type adoptionBranch struct {
	Name string // short branch name as pushed (feat/x)
	Tip  string // short sha — goes into the dispatch briefing
	// Remote marks a refs/remotes/origin/* arrival (branch -r -d on ③a).
	Remote bool
}

// adoptionGit runs one git command in repo with the shared timeout. The
// returned error carries git's own cause: stderr when git used it, else the
// STDOUT text — `git commit` with nothing staged explains itself on stdout
// ("nothing added to commit but untracked files present"), so a stderr-only
// error renders as a trailing ": " with no cause at all (Rule 15b).
func adoptionGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), adoptionGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	// THE LOCALE IS PINNED HERE — this runner is the adoption lane's ONE place
	// (Rule 2/14). Its callers classify git's answer: ③b treats a failed
	// `merge --abort` as "the scene could not be put back", and git's
	// "no merge to abort" sentence is TRANSLATED. Measured on this host
	// 2026-10-30 (git 2.43.0, zh_CN.utf8 installed):
	//
	//	$ LC_ALL=C            git merge --abort
	//	fatal: There is no merge to abort (MERGE_HEAD missing).
	//	$ LC_ALL=zh_CN.UTF-8  git merge --abort
	//	致命错误：没有要终止的合并（MERGE_HEAD 丢失）。
	//
	// A branch whose PROBE merge failed pre-flight (`refusing to merge unrelated
	// histories`) leaves no merge state, so the mandatory abort answers with the
	// sentence above and the branch is reported `failed: abort: …` — under zh_CN
	// carrying text C never prints, which is the same input producing a
	// different record. Root cause and the sharing rule: git_child_locale.go.
	cmd.Env = ChildLocaleEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := adoptionGitTail(stderr.String())
		if detail == "" {
			detail = adoptionGitTail(string(out))
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "),
			err, detail)
	}
	return string(out), nil
}

// gitStatusTrackedOnlyFlag restricts `git status --porcelain` to TRACKED
// changes (Rule 1b: the flag is a meaningful literal, named once here).
const gitStatusTrackedOnlyFlag = "--untracked-files=no"

// AdoptionGitStatus is the STRICT dirty-tree check the serve face's adoption
// chain uses (keeps `git status --porcelain` spelling in one place). It is
// untracked-INCLUSIVE on purpose: the probe merge / ③a commit path must not
// entangle with untracked in-flight work either. Callers that need the
// tracked-only view use AdoptionGitStatusTracked (below) — the two contracts
// are deliberately separate, never a silent redefinition of this one.
func AdoptionGitStatus(repo string) (string, error) {
	return adoptionGit(repo, "status", "--porcelain")
}

// AdoptionGitStatusTracked is the TRACKED-ONLY dirty-tree check the
// post-merge rebuild guard uses (epic 2026-09-12 — the Rule-12 close loop
// was skipped FOREVER in a busy task field: the repo permanently holds
// untracked artifacts, so the untracked-inclusive predicate never came back
// empty and merge → cleanup → REBUILD never ran).
//
// The rebuild question is only whether the WORKING TREE's tracked state can
// represent the merge that just landed; untracked scratch (briefings/, logs,
// another agent's new file) does not change any committed source file, so it
// must not defer the build. Genuine tracked dirt still does — this is a
// SPLIT predicate, not a deletion of the guard.
func AdoptionGitStatusTracked(repo string) (string, error) {
	return adoptionGit(repo, "status", "--porcelain", gitStatusTrackedOnlyFlag)
}

// adoptionDefaultTryMerge is the real ② probe: merge without committing.
// An error means CONFLICT (or a wedged tree) — callers abort; nil means
// the merge staged clean and ③a commits it.
func adoptionDefaultTryMerge(repo, branch string) error {
	_, err := adoptionGit(repo, "merge", "--no-commit", "--no-ff", branch)
	return err
}

// adoptionMergeHeadRef is git's "a merge is in progress" sentinel. Its
// PRESENCE after a probe merge — not the probe's exit status — is the ground
// truth for "there is a merge to commit": for a branch already CONTAINED in
// master git prints "Already up to date." and returns 0 while staging
// NOTHING, so the exit status alone reports a green probe with no work.
const adoptionMergeHeadRef = "MERGE_HEAD"

// adoptionErrDetailBudget caps the git text carried into a report entry
// (Rule 1: the budget is a named limit, never an inline number). The serve
// line is line-oriented and an operator reads it at a glance.
const adoptionErrDetailBudget = 200

// adoptionErrDetail renders a failed git call for the report as ONE bounded
// line (adoptionGit already put git's own cause in the error; this only keeps
// the serve line readable).
func adoptionErrDetail(err error) string {
	detail := strings.TrimSpace(err.Error())
	if len(detail) > adoptionErrDetailBudget {
		detail = detail[:adoptionErrDetailBudget] + "…"
	}
	return detail
}

// adoptionMergeStaged reports whether the probe left a merge to commit
// (MERGE_HEAD present). git writes it during `--no-commit --no-ff` and drops
// it on `merge --abort`, so its presence is the one signal that separates
// "the branch staged a real merge" from "the branch was already in master".
func adoptionMergeStaged(repo string) bool {
	_, err := adoptionGit(repo, "rev-parse", "-q", "--verify", adoptionMergeHeadRef+"^{commit}")
	return err == nil
}

// adoptionAlreadyIntegratedMarker is the report entry's WART marker for a
// discovered candidate whose tip is ALREADY CONTAINED in the integration
// branch: the probe merges nothing, so there is nothing to commit and the ref
// prune is the only remaining work. It is a HONEST, actionable line — the
// defect it replaces printed `failed=[<branch>: commit: git commit --no-verify
// -m adoption: merge <branch> (<tip>): exit status 1: ]` — an EMPTY detail
// whose cause git wrote to stdout, which adoptionGit discarded — which reads
// as a broken adoption of a branch that is in fact fully landed (measured
// 2026-09-14: fix/ledger-fixture-leak b9ae5cbc and
// fix/worker-ownership-pointer-master-red 67445327, both ancestors of master,
// both re-printed as failures on every serve pass).
const adoptionAlreadyIntegratedMarker = "already integrated in "

// adoptionAlreadyIntegratedLineFmt renders the whole report entry for that
// case, converging with the remedy in the SAME line (an operator must be able
// to clear the leftover without rediscovering git). One spelling of the marker
// for the line and for the tests reading it.
const adoptionAlreadyIntegratedLineFmt = "%s: " + adoptionAlreadyIntegratedMarker + "%s — nothing to merge; prune the leftover ref with: %s"

// adoptionPruneCommand renders the exact command that clears a leftover ref of
// an already-integrated branch — the project's ONE archive writer, because a raw
// branch deletion would leave no audit record (Rule 9 step 2).
//
// The LOCAL form goes through `gopi task archive`: the writer records the WHY
// beside the decision and reclaims the head ref with its own audited arm. The
// REMOTE form is a remote-tracking ref, which no archive writer owns: the ref is
// a local CACHE of the peer's namespace, so the command that clears it is the
// fetch that prunes it — the same instruction the ledger's own sync faces give.
func adoptionPruneCommand(branch string, remote bool) string {
	if remote {
		return "git fetch --prune"
	}
	return "gopi task archive " + branch + " --reason " + ArchiveReasonSuperseded
}

// AdoptionSweep runs the three-stage chain over the repo's remote branches.
// Errors are per-branch (collected into Counts.Failed) — the sweep never
// aborts the serve pass.
func AdoptionSweep(ctx context.Context, opts AdoptionOptions) (AdoptionCounts, error) {
	if ctx == nil {
		// nil ctx = nil interface, not a zero deadline — database/sql
		// dereferences it and segfaults (CLI face passes nil).
		ctx = context.Background()
	}
	counts := AdoptionCounts{}
	repo := opts.Repo
	if repo == "" {
		var err error
		repo, err = filepath.Abs(".")
		if err != nil {
			return counts, err
		}
	}
	master := opts.MasterBranch
	if master == "" {
		master = "master"
	}
	tryMerge := opts.TryMerge
	if tryMerge == nil {
		tryMerge = adoptionDefaultTryMerge
	}

	// ① discovery: push-back branches — BOTH arrival channels. A client
	// `git push origin feat/x` into a local-path remote lands directly in
	// refs/heads (no remote-tracking ref is created); the harvest/gopeer
	// channel produces refs/remotes/origin/*. Sweep both, excluding the
	// integration branch itself and the currently checked-out branch
	// (merging INTO the checked-out master is fine; a branch checked out
	// elsewhere in a worktree is refused by git's own ref protection).
	out, err := adoptionGit(repo, "for-each-ref",
		"--format=%(refname) %(objectname:short)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return counts, err
	}
	var candidates []adoptionBranch
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		ref, tip := fields[0], fields[1]
		name := ""
		remote := false
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			name = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/remotes/origin/"):
			name = strings.TrimPrefix(ref, "refs/remotes/origin/")
			remote = true
		default:
			continue // non-origin remotes are not the push-back channel
		}
		if name == "" || name == master || name == "HEAD" || seen[name] {
			continue
		}
		seen[name] = true
		candidates = append(candidates, adoptionBranch{Name: name, Tip: tip, Remote: remote})
	}
	counts.Scanned = len(candidates)

	for _, br := range candidates {
		// ① gate: the branch must be paired with a COMPLETED ledger task
		// (deliverable.branch backfilled by the executor at completion).
		completed, err := adoptionTaskCompleted(ctx, br.Name)
		if err != nil {
			counts.Failed = append(counts.Failed, br.Name+": "+err.Error())
			continue
		}
		if !completed {
			counts.Skipped++
			continue
		}
		// ⑤ idempotency gate FIRST (before ② mutates the worktree): a
		// branch whose conflict task is already dispatched must not be
		// re-probed or re-dispatched, whatever the pass cadence.
		//
		// The gate is the branch's WHOLE resolve-row identity (F6 of
		// devdoc/merge-resolve-effort-audit.md): a LIVE row is a duplicate,
		// and a TERMINAL one is a CONCLUDED decision that must not be
		// re-minted. Asking only "is it live?" is what let one branch collect
		// seven rows — each one a full hand-authored resolution — because every
		// tick after the previous row closed saw a free slot.
		live, concluded, err := resolveConflictRowIdentity(ctx, br.Name)
		if err != nil {
			counts.Failed = append(counts.Failed, br.Name+": "+err.Error())
			continue
		}
		if live {
			counts.Duped++
			continue
		}
		if concluded {
			// The branch's resolve decision is already on the record. Say so
			// once per pass (never silently) and mint nothing — the operator
			// reads a named branch, not a counter that stops moving.
			counts.Concluded = append(counts.Concluded, br.Name+
				": a terminal resolve row already judged this branch — not re-dispatched")
			continue
		}
		// ② probe merge (worktree must be clean — serve owns it).
		if err := tryMerge(repo, br.Name); err != nil {
			// ③b conflict: save the scene, then dispatch. Abort MUST run
			// even when the probe failed for non-conflict reasons — the
			// tree may hold partial index state either way.
			if _, aerr := adoptionGit(repo, "merge", "--abort"); aerr != nil {
				counts.Failed = append(counts.Failed, br.Name+": abort: "+aerr.Error())
				continue
			}
			if !opts.Submit {
				counts.Skipped++
				continue
			}
			if err := adoptionDispatchConflict(ctx, opts, repo, br); err != nil {
				counts.Failed = append(counts.Failed, br.Name+": dispatch: "+err.Error())
				continue
			}
			counts.Dispatch = append(counts.Dispatch, br.Name)
			continue
		}
		// ②b the probe returned 0 — but "the merge went well" is not "a merge
		// was staged". For a branch already CONTAINED in master git prints
		// "Already up to date." and exits 0 while staging NOTHING, so ③a's
		// commit has nothing to commit and fails with a message git writes to
		// STDOUT ("nothing added to commit but untracked files present"), which
		// adoptionGit discards — the operator saw a bare `exit status 1: ` tail
		// and read a healthy landing as a broken adoption. MERGE_HEAD is the
		// ground truth: present = commit the merge, absent = already integrated.
		// Reported as a WART (prune the ref), never as `failed=`, exactly like
		// the landed-but-ref-kept case above: the branch's work IS in master,
		// so the queue is not blocked and the branch is not owed a merge.
		if !adoptionMergeStaged(repo) {
			// ORPHAN guard (2026-09-17 incident, task 01a0af50): this arm
			// force-deletes a ref whose merge staged nothing because git
			// considered it already integrated — but "no merge was staged" is
			// also what a WEDGED or mis-probed adoption looks like, and a
			// deleted ref is unrecoverable. A branch whose tip no other ref
			// reaches is reported (the ref is KEPT) instead of pruned.
			if why := branchDeleteOrphanRefusal(repo, br.Name); why != "" {
				counts.RefKept = append(counts.RefKept, br.Name+" ("+why+") ["+adoptionRefKeptMarker+"]")
				counts.AlreadyIntegrated = append(counts.AlreadyIntegrated, fmt.Sprintf(
					adoptionAlreadyIntegratedLineFmt, br.Name, master, adoptionPruneCommand(br.Name, br.Remote)))
				continue
			}
			removalaudit.BranchDelete(removalaudit.ArmAdoptionBranchForce, repo, br.Name, "",
				"the probe proved the branch's work is already in master (no MERGE_HEAD staged)")
			if _, derr := adoptionGit(repo, "branch", "-D", br.Name); derr != nil {
				counts.RefKept = append(counts.RefKept, br.Name+" ("+adoptionGitTail(derr.Error())+") ["+adoptionRefKeptMarker+"]")
			}
			counts.AlreadyIntegrated = append(counts.AlreadyIntegrated, fmt.Sprintf(
				adoptionAlreadyIntegratedLineFmt, br.Name, master, adoptionPruneCommand(br.Name, br.Remote)))
			continue
		}
		// ③a clean: commit + delete the branch. refs/heads arrivals are
		// deleted with branch -d; remote-tracking ones need -r.
		if err := adoptionCommitMerge(ctx, repo, adoptionSummaryPrefix+br.Name+" ("+br.Tip+")"); err != nil {
			counts.Failed = append(counts.Failed, br.Name+": commit: "+adoptionErrDetail(err))
			continue
		}
		delArgs := []string{"branch"}
		if br.Remote {
			delArgs = append(delArgs, "-r")
		}
		delArgs = append(delArgs, "-d", br.Name)
		removalaudit.BranchDelete(removalaudit.ArmAdoptionBranchClean, repo, br.Name, "",
			"the adoption merge of this ref was just committed")
		if _, err := adoptionGit(repo, delArgs...); err != nil {
			// The merge landed; losing only the ref deletion is a
			// reportable WART, not a failed adoption — the MergedCleanupSweep
			// serve pass (bugfix 2026-09-02) removes the blocking worktree
			// and deletes the branch once it is provably dead.
			//
			// It goes to RefKept, NEVER to Failed: measuring master with a
			// worktree-held branch showed the merge landing correctly while the
			// pass reported `failed=[… prune ref: Cannot delete branch '…'
			// checked out at '…']` — a healthy adoption wearing the operator's
			// error channel (Rule 15b).
			counts.RefKept = append(counts.RefKept, br.Name+" ("+adoptionGitTail(err.Error())+") ["+adoptionRefKeptMarker+"]")
		}
		counts.Adopted = append(counts.Adopted, br.Name)
	}
	return counts, nil
}

// adoptionCommitMerge creates the adoption merge commit — the ONE seam in this
// package that CREATES a commit rather than running another git verb.
//
// It is a named function rather than an inline call for two reasons that are
// about proof, not style: the site's semantics (which flags, which message,
// which timeout) are readable in one place, and a test can drive THIS function
// against a real repository and read the commit back — an inline call inside
// the sweep's loop can only be reached through a full adoption fixture.
//
// The sweep merges OTHER rows' branches, so it has no ledger row of its own to
// name: the commit carries the host line and no `task:` line, which is the
// honest description of who produced it.
func adoptionCommitMerge(ctx context.Context, repo, subject string) error {
	_, err := gitcommit.Commit(ctx, repo,
		gitcommit.Message{Subject: subject},
		gitcommit.Options{NoVerify: true, Timeout: adoptionGitTimeout})
	return err
}

// adoptionGitTail renders git's message on ONE line so a report entry cannot
// smuggle newlines into the serve log (the status line format is line-oriented).
func adoptionGitTail(msg string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(msg, ":", " -")), " ")
}

// adoptionTaskCompleted reports whether the ledger holds a COMPLETED task
// whose deliverable branch is name (① gate 2: completed 台账配对). The
// deliverable JSON carries "branch" only after an executor backfills it
// (M4.9); a bare {"repo":...} submit row (no branch yet) never matches.
func adoptionTaskCompleted(ctx context.Context, name string) (bool, error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT deliverable FROM tasks WHERE status='completed'`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		branch, err := taskDeliverableBranch(raw.String)
		if err != nil || branch == "" {
			continue // malformed JSON or branch-less row — not a match
		}
		if branch == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

// adoptionDispatchConflict enqueues the ③b task. The briefing is
// SELF-CONTAINED (P2.5 t-1788063202 precedent): the executor agent gets the
// conflict file list, both-side diff summaries, the branch intent (ledger
// summary+deliverable) and the acceptance bar — it resolves the conflict in
// its OWN session, commits, pushes back; the next tick adopts via ③a.
//
// LANE OWNERSHIP (F5 of devdoc/merge-resolve-effort-audit.md). Three routes
// answer "this branch conflicts at merge time": this one (a fresh adoption
// worker, embeds the hunks), the merge-time lane (taskMergeDispatchConflict,
// reached by a deliberate `gopi task merge --submit`) and the take-over
// prompt's §3.6 case ④ (hand the diff to the branch's AUTHOR as a rework task).
// They coexisted with no rule choosing between them. The rule, stated once
// here and implemented by the dedup readers:
//
//	rule 1 — ONE resolver per branch. Whichever route reaches the branch first
//	         owns it: a later route reads the resolve-row identity
//	         (resolveConflictRowIdentity) and defers — a live row is a
//	         duplicate, a terminal row is a concluded decision.
//	rule 2 — the AUTHOR's lane holds the branch. A live branch-rework row
//	         (resolveRowReworkSummaryPrefix+branch) means the branch's
//	         merge-time difficulty is already queued as its author's job, so
//	         the merge-time conflict lane reports rework-owned and defers
//	         (taskMergeDispatchConflict). §3.6 case ④ files that row.
//	rule 3 — this lane is the DEFAULT for a branch nobody owns, and it needs no
//	         special test: reaching a conflict here has already passed the
//	         ① gate (a COMPLETED ledger row names the branch) and both dedup
//	         rules above.
//	rule 4 — the route is worth arbitrating because it is real work, not a
//	         formality: a resolve commit authors a median 93 hand-written lines
//	         (35 of 71 above 100, max 1594 — the audit's F7 census). That is why
//	         a duplicate row is a duplicate review, not a duplicate click.
func adoptionDispatchConflict(ctx context.Context, opts AdoptionOptions, repo string, br adoptionBranch) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Conflict resolution: "+taskMergePayloadBranchLine+" does not merge cleanly into master in repo %s.\n\n",
		br.Name, br.Tip, repo)
	if files, err := adoptionGit(repo, "diff", "--name-only", "master..."+br.Name); err == nil && strings.TrimSpace(files) != "" {
		fmt.Fprintf(&b, "Branch-vs-master changed files:\n%s\n", strings.TrimSpace(files))
	}
	if stat, err := adoptionGit(repo, "diff", "--stat", "master..."+br.Name); err == nil && strings.TrimSpace(stat) != "" {
		fmt.Fprintf(&b, "\nDiff summary (git diff master...%s --stat):\n%s\n", br.Name, strings.TrimSpace(stat))
	}
	if err := adoptionMergeConflictBriefing(repo, br, &b); err != nil {
		return err
	}
	if intent, err := adoptionBranchIntent(ctx, br.Name); err == nil && intent != "" {
		fmt.Fprintf(&b, "\nBranch intent (ledger):\n%s\n", intent)
	}
	b.WriteString(`
Acceptance: the merge into master completes, the project builds and the
test suite passes. Work in your own session/branch, push back when green —
the server adopts the branch automatically on a later serve tick.`)

	source := opts.Source
	if source == "" {
		// The conflict adoption lane is the MERGER's machine enqueue.
		source = tasksource.Merger
	}
	_, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resolve merge conflict: " + br.Name + " into master",
		Payload: b.String(),
		Tags:    taskTagMergeConflict + ",auto-dispatch",
		Source:  source,
		Repo:    repo,
	})
	return err
}

// adoptionBranchIntent pulls the originating task's intent lines (summary +
// deliverable) for the briefing — the executor knows WHY the branch exists.
func adoptionBranchIntent(ctx context.Context, name string) (string, error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT summary, deliverable FROM tasks WHERE status='completed'`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var summary, raw sql.NullString
		if err := rows.Scan(&summary, &raw); err != nil {
			return "", err
		}
		branch, err := taskDeliverableBranch(raw.String)
		if err != nil || branch != name {
			continue
		}
		return fmt.Sprintf("task %q deliverable %s", strings.TrimSpace(summary.String), strings.TrimSpace(raw.String)), nil
	}
	return "", rows.Err()
}
