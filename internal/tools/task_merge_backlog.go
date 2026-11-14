package tools

// Merge-backlog audit (foresee-review design, devdoc/foresee-review-branch-
// backlog.md §2): inventory unmerged local branches of a repo and — with
// Submit — enqueue one "merge <branch> into master" pending row per unmerged
// branch, deduped by branch+tip so re-audits never spam the ledger.
//
// The audit is infrastructure (a git query), NOT an LLM tool (Rule 3) — the
// serve tick and the `gopi task audit-merges` CLI share this engine.

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Merge-backlog audit tunables (Rule 1: every literal in one place).
const (
	// mergeBacklogGitTimeout bounds each git invocation — a hung repo must
	// not stall the serve pass.
	mergeBacklogGitTimeout = 10 * time.Second

	// mergeBacklogSummaryVerb is the summary prefix the audit enqueues;
	// the dedup live-check keys on "merge <branch> into master".
	mergeBacklogSummaryVerb = "merge %s into master"

	// mergeBacklogTagPrefix is the branch-prefix → tag map (§2.3 模块分区):
	// fix/* rows also carry merge-fix, feat/* rows merge-feat.
	mergeBacklogTagPrefix = "merge-"

	// mergeBacklogTipLine is the payload line carrying the tip commit
	// ("branch tip: <sha>") — the terminal-row dedup compares against it,
	// and the D2 terminal guard falls back to it as merge proof when the
	// branch ref is already gone (task_merge_backlog_terminal_guard.go).
	mergeBacklogTipLine = "branch tip: %s"

	// mergeBacklogMergeTarget is the integration branch every merge-backlog
	// ancestry question is asked against — the same base the audit's
	// discovery uses (`git rev-list --count master..<branch>`). Named once
	// (Rule 1/14): it is DEFINED as the package's integration-branch
	// constant rather than re-spelling "master", so the discovery, the
	// dedup and the D2 terminal guard answer the SAME question — a guard
	// judging a different base than the row is about would refuse
	// legitimate closes.
	mergeBacklogMergeTarget = masterBranchName

	// archiveTagRefPrefix (single source in tasks.go) scopes the
	// archive-reachability filter to the loser-archive ledger
	// (refs/tags/archive/* — the same namespace the safe-merge guard checks).
)

// archiveTipReachable reports whether tip contains the commit an archive
// tag points at (git merge-base --is-ancestor <tag> <tip>, exit 0 = yes).
// The loser-archive convention: a decision "archive <branch>" tags the
// branch's tip and deletes the branch; the tag keeps the tip alive, so the
// branch (re-created or remote-mirrored) still reads unmerged — but its
// merge decision is already recorded and re-submitting rows for it is the
// phantom-row amplification the hp/master closure (859db09) traced.
func archiveTipReachable(repo, tag, tip string) bool {
	_, err := mergeBacklogGit(repo, "merge-base", "--is-ancestor", tag, tip)
	return err == nil
}

// archiveTagAncestry filters out branches whose tip is reachable from any
// refs/tags/archive/* tag (already-archived losers). Degrades to a no-op
// when the tag listing fails or the namespace is empty (the scan must not
// go blind on a git hiccup).
func archiveTagAncestry(repo string, branches []MergeBacklogBranch) []MergeBacklogBranch {
	tagsRaw, err := mergeBacklogGit(repo, "for-each-ref", "--format=%(refname:short)", archiveTagRefPrefix)
	if err != nil || strings.TrimSpace(tagsRaw) == "" {
		return branches // no archive ledger / listing failed → filter off
	}
	out := branches[:0:0]
	for _, b := range branches {
		archived := false
		for _, tag := range strings.Fields(tagsRaw) {
			if archiveTipReachable(repo, tag, b.LastCommit) {
				archived = true
				break
			}
		}
		if !archived {
			out = append(out, b)
		}
	}
	return out
}

// MergeBacklogBranch is one discovered branch and its merge posture.
type MergeBacklogBranch struct {
	// Name is the branch name (refs/heads short form, e.g. feat/x).
	Name string
	// AheadCount is `git rev-list --count master..<branch>` — commits the
	// branch would add to master. 0 = fully merged, nothing to do.
	AheadCount int
	// LastCommit is the short tip sha (payload provenance for dedup).
	LastCommit string
	// LastDate is the committer date, short form (YYYY-MM-DD).
	LastDate string
}

// MergeBacklogOptions carries one audit invocation. The zero value audits
// the cwd's repo against the default ledger in report-only mode.
type MergeBacklogOptions struct {
	// Repo is the git repo to audit ("" = cwd).
	Repo string
	// DBPath overrides the ledger ("" = memoryDBPath()); CLI --db.
	DBPath string
	// Submit true = enqueue pending rows for unmerged, unduped branches;
	// false = report only.
	Submit bool
	// Discover replaces the `git for-each-ref` discovery entirely (test
	// seam — same injection pattern as HarvestOptions.Fetch). nil = the
	// real git discovery.
	Discover func(repo string) ([]MergeBacklogBranch, error)
}

// MergeBacklogCounts is the audit's tally (mirror of the harvest counts).
type MergeBacklogCounts struct {
	// Scanned counts the local branches examined (master/main excluded).
	Scanned int
	// Submitted counts NEW pending rows created.
	Submitted int
	// Duped counts branches skipped because a ledger row already covers
	// branch+tip (live or terminal-same-tip).
	Duped int
	// Skipped counts ahead=0 branches (already merged, nothing to do).
	Skipped int
	// Failed lists per-branch errors (discovery/DB failures), one line each.
	Failed []string
	// Branches carries the per-branch report rows for CLI rendering
	// (all discovered branches, including skipped/failed-to-submit ones).
	Branches []MergeBacklogBranch
}

// mergeBacklogDefaultBranches are the integration targets excluded from the
// audit (the branch IS the thing merging INTO them). Rule 13: the two names
// come from the package's single definitions, never re-spelled here.
var mergeBacklogDefaultBranches = map[string]bool{
	masterBranchName: true,
	mainBranchName:   true,
}

// AuditMergeBacklog runs one branch-backlog audit (§2). With Submit it
// enqueues one pending "merge <branch> into master" row per unmerged,
// not-yet-covered branch; the branch+tip dedup (§2.2) makes re-runs
// idempotent. Git/discovery failure degrades to an error — the serve pass
// logs and continues (degrade-don't-die).
func AuditMergeBacklog(opts MergeBacklogOptions) (MergeBacklogCounts, error) {
	counts := MergeBacklogCounts{}
	discover := opts.Discover
	if discover == nil {
		discover = discoverMergeBacklogBranches
	}
	branches, err := discover(opts.Repo)
	if err != nil {
		return counts, fmt.Errorf("merge-backlog discovery: %w", err)
	}
	// Archive-reachability filter: a branch tip reachable from any
	// refs/tags/archive/* tag is an already-archived loser — its decision
	// is recorded, no merge row is owed (the same fix the branch-scan side
	// carries; without it the audit re-submits "merge <loser>" forever).
	branches = archiveTagAncestry(opts.Repo, branches)
	counts.Scanned = len(branches)
	counts.Branches = branches

	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		return counts, err
	}
	defer db.Close()

	for _, b := range branches {
		if b.AheadCount == 0 {
			counts.Skipped++ // fully merged; tombstone rows stay terminal
			continue
		}
		status, err := mergeBacklogDedupCheck(context.Background(), db, b.Name, b.LastCommit)
		if err != nil {
			counts.Failed = append(counts.Failed, fmt.Sprintf("%s: dedup: %v", b.Name, err))
			continue
		}
		if status != "" { // live or terminal-same-tip → duped
			counts.Duped++
			continue
		}
		if !opts.Submit {
			continue // report-only: the branch is pending-eligible
		}
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: fmt.Sprintf(mergeBacklogSummaryVerb, b.Name),
			Payload: mergeBacklogPayload(b),
			Tags:    mergeBacklogTags(b.Name),
			Source:  tasksource.Merger,
			Repo:    auditRepoPath(opts.Repo),
		}); err != nil {
			counts.Failed = append(counts.Failed, fmt.Sprintf("%s: submit: %v", b.Name, err))
			continue
		}
		counts.Submitted++
	}
	return counts, nil
}

// mergeBacklogPayload renders the execution briefing (§2.3): the dedup
// facts (branch/tip/ahead/date) + the instruction that keeps merge
// decision-making human-verifiable (review diff → safe-merge.sh → merge;
// conflicts are reported, never forced).
func mergeBacklogPayload(b MergeBacklogBranch) string {
	return fmt.Sprintf("branch: %s\ntip: %s\nahead: %d\nlast_commit_date: %s\n%s"+
		"Task: review the branch diff vs master, run scripts/safe-merge.sh %s, then merge. "+
		"On conflict: report, do not force.",
		b.Name, b.LastCommit, b.AheadCount, b.LastDate, MergeBacklogPayloadTipLine(b.LastCommit), b.Name)
}

// MergeBacklogPayloadTipLine renders the payload's tip line
// ("branch tip: <sha>") — the ONE authoring helper of that line, exported for
// the cmd/agent branch-scan face. The line is load-bearing on the READ side:
// mergeBacklogDedupCheck compares it against a branch's current tip, and the
// merge-backlog terminal guard falls back to it when the branch ref is already
// deleted (the merger's order is merge → `git branch -d` → write completed, so
// a success-terminal row never has a live ref). A caller that typed the line
// itself would be a SECOND definition of a format two parsers depend on —
// hence a helper beside the mergeBacklogTipLine constant it renders (Rule
// 13/14). The returned text ends with a newline: the tip line is prepended to
// a payload authored as an ordered set of newline-terminated lines.
func MergeBacklogPayloadTipLine(tip string) string {
	return fmt.Sprintf(mergeBacklogTipLine, tip) + "\n"
}

// mergeBacklogTags builds the row's tag set: merge-backlog + the branch-
// prefix partition tag (fix/* → merge-fix, feat/* → merge-feat, §2.3).
func mergeBacklogTags(branch string) string {
	tag := ""
	if slash := strings.Index(branch, "/"); slash > 0 {
		prefix := branch[:slash]
		if prefix == "fix" || prefix == "feat" {
			tag = mergeBacklogTagPrefix + prefix
		}
	}
	if tag == "" {
		return mergeBacklogTag
	}
	return mergeBacklogTag + "," + tag
}

// auditRepoPath resolves the audited repo for TaskSubmit's Repo stamp
// ("" = cwd — the deliverable.repo verification chain anchors there).
func auditRepoPath(repo string) string {
	if repo != "" {
		return repo
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// mergeBacklogDedupCheck implements §2.2 — the load-bearing anti-spam rule.
// Returns "" when the branch may be submitted, or a non-empty reason when
// the branch is already covered:
//   - any live row (pending/claimed/in_progress) with the branch's summary
//     → "live";
//   - a terminal row whose payload stamps the SAME branch tip
//     ("branch tip: <sha>") → "terminal";
//   - a terminal row with a DIFFERENT tip → "" (the branch gained commits
//     after the last merge-task closed — re-audit is legitimate).
func mergeBacklogDedupCheck(ctx context.Context, db *sql.DB, branch, tip string) (string, error) {
	summary := fmt.Sprintf(mergeBacklogSummaryVerb, branch)
	rows, err := db.QueryContext(ctx,
		`SELECT status, payload FROM tasks WHERE summary = ? ORDER BY id DESC`, summary)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var status, payload string
		if err := rows.Scan(&status, &payload); err != nil {
			return "", err
		}
		if mergeBacklogLiveStatus(status) {
			return "live", nil
		}
		want := fmt.Sprintf(mergeBacklogTipLine, tip)
		if strings.Contains(payload, want) {
			return "terminal", nil
		}
		// terminal with another tip → keep scanning (older rows for the
		// same branch may carry THIS tip; only a same-tip hit dedupes).
	}
	return "", rows.Err()
}

// mergeBacklogLiveStatus reports whether s is a queue-live state (the row
// is or may soon be worked — a re-submit would double-queue the merge).
func mergeBacklogLiveStatus(s string) bool {
	switch s {
	case taskStatusPending, taskStatusClaimed, taskStatusInProgress:
		return true
	}
	return false
}

// discoverMergeBacklogBranches is the real §2.1 discovery: local branches
// (refs/heads), master/main excluded, per-branch ahead count and tip. The
// git calls are sequential and local-only (sub-100ms on the audit repo).
func discoverMergeBacklogBranches(repo string) ([]MergeBacklogBranch, error) {
	names, err := mergeBacklogGit(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("for-each-ref: %w", err)
	}
	var out []MergeBacklogBranch
	for _, name := range strings.Fields(names) {
		if mergeBacklogDefaultBranches[name] {
			continue
		}
		ahead, err := mergeBacklogGit(repo, "rev-list", "--count", mergeBacklogMergeTarget+".."+name)
		if err != nil {
			return nil, fmt.Errorf("rev-list %s: %w", name, err)
		}
		n := 0
		for _, f := range strings.Fields(ahead) {
			fmt.Sscanf(f, "%d", &n)
		}
		tip, err := mergeBacklogGit(repo, "log", "-1", "--format=%h %cs", name)
		if err != nil {
			return nil, fmt.Errorf("log -1 %s: %w", name, err)
		}
		fields := strings.Fields(tip)
		sha, date := "", ""
		if len(fields) > 0 {
			sha = fields[0]
		}
		if len(fields) > 1 {
			date = fields[1]
		}
		out = append(out, MergeBacklogBranch{
			Name:       name,
			AheadCount: n,
			LastCommit: sha,
			LastDate:   date,
		})
	}
	return out, nil
}

// mergeBacklogGit runs one git command in repo ("" = cwd) and returns its
// trimmed stdout. A non-zero exit surfaces git's stderr (root-cause first).
func mergeBacklogGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeBacklogGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if repo != "" {
		cmd.Dir = repo
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
