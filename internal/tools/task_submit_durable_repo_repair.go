package tools

// task_submit_durable_repo_repair.go — THE DEAD-WORKSPACE REPAIR (epic
// 01a15629, measured live: rows 01a154de / 01a1510e / 01a1513b).
//
// THE SHAPE. A split/reframe child can be born naming the directory its PARENT
// ran in — a clone-channel WORKSPACE at <workspace_root>/<parent-id> — instead
// of the repository that workspace was cloned from. The workspace is a plain
// CLONE: its own repository, declared nowhere in the settings `repos` table, and
// RECLAIMED when the parent's push lands. So the child names a directory that
// (a) is not a git work tree any more and (b) has nothing left to read its
// origin from. The clone channel then refused it — correctly — but said
// "declares no build command in settings repos", which was the WRONG reason:
// the directory was dead, not undeclared.
//
// WHAT THIS FILE DOES, and it is deliberately small: for a repo path that lies
// UNDER THE WORKSPACE ROOT and is no longer a usable git work tree, it recovers
// the LIVE repository from the one place that recorded it — the ledger row that
// stamped that directory as its own deliverable.worktree — and hands back that
// repository's main checkout. Every outcome is RETURNED as one line, so the
// caller prints it (Rule 15: a repair nobody can read in the log is a silent
// one).
//
// THE FIVE LIMITS, each of them a decision rather than an omission:
//   - a path OUTSIDE the workspace root is not touched at all — not even a
//     stat is consulted (the same boundary internal/devworkspace/trace.go uses,
//     through the same ONE reader, config.WorkspaceRootConfigured);
//   - a path that IS a live git work tree is IDENTITY: the same string comes
//     back and no git process is spawned (a normal repo's submit is unchanged);
//   - nothing is GUESSED: when no row recorded the path, or the row's repo is
//     not itself verifiable, the input comes back untouched and the note says
//     so;
//   - the workspace root is never re-derived here (Rule 1/13/14 — one reader);
//   - it writes nothing. The repair is a value the caller stamps; the ledger is
//     not edited from here.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// TaskRepairDeadWorkspaceFmt is the ONE spelling of "the row's repo was
	// repaired" (Rule 1b). It names the dead path, WHICH ROW recorded it, and
	// the live repository the row now names — the three facts an operator needs
	// to trust the substitution without re-running the submit.
	TaskRepairDeadWorkspaceFmt = "task_submit: repo %q no longer exists under the workspace root %q — repaired to %s, the repository row %s recorded for that workspace (%s)"

	// TaskRepairDeadWorkspaceMissFmt is the honest MISS: the path is dead, it
	// lies under the workspace root, and no live repository could be recovered
	// from it. The row keeps the path it was given and the caller decides
	// (the clone channel will refuse it, now naming the real reason).
	TaskRepairDeadWorkspaceMissFmt = "task_submit: repo %q no longer exists under the workspace root %q and no live repository could be recovered (%s) — the row keeps the path it was given"

	// TaskRepairDeadWorkspaceSourceLabel names WHERE the recovered repository
	// came from. It is a fact (a ledger row), not an inference from a path
	// spelling — the workspace directory itself is gone and cannot be asked.
	TaskRepairDeadWorkspaceSourceLabel = "its deliverable.worktree"

	// taskRepairLedgerTimeout bounds the one ledger query (Rule 1: no bare
	// number).
	taskRepairLedgerTimeout = 5 * time.Second
)

// RepairDurableRepoForSubmit answers the DURABLE repository a submit must stamp
// when the caller's repo is a WORKSPACE PATH THAT DIED. It returns the repo to
// use and ONE line describing what happened ("" = nothing to report: the path is
// outside the workspace root, or it is a live git work tree, i.e. the normal
// case, which therefore costs no git process).
//
// It is idempotent: the returned path is a main checkout, so a second call sees
// a live work tree and answers identity with no note.
func RepairDurableRepoForSubmit(repo string) (string, string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return repo, ""
	}
	root, _, err := config.WorkspaceRootConfigured()
	if err != nil || root == "" {
		// An unreadable workspace root is NOT a licence to guess: the repair is
		// confined to the channel's own checkouts, and without the root there is
		// no way to tell one from an operator's repository.
		return repo, ""
	}
	if !worktreeroots.Within(repo, root) {
		return repo, ""
	}
	// INSIDE the workspace root. A directory that still IS a git work tree is
	// left alone — it may be a live run's workspace, and identity is the answer
	// every caller expects.
	if taskRepairPathExists(repo) {
		if err := taskRepoVerifiable(repo); err == nil {
			return repo, ""
		}
	}
	source, rowID, err := taskWorkspaceRowRepo(repo)
	if err != nil {
		return repo, fmt.Sprintf(TaskRepairDeadWorkspaceMissFmt, repo, root, err.Error())
	}
	if source == "" {
		return repo, fmt.Sprintf(TaskRepairDeadWorkspaceMissFmt, repo, root,
			"no ledger row records it as its "+DeliverableWorktreeKey)
	}
	if verr := taskRepoVerifiable(source); verr != nil {
		return repo, fmt.Sprintf(TaskRepairDeadWorkspaceMissFmt, repo, root,
			fmt.Sprintf("row %s names %s, which is not a live git work tree either: %v", rowID, source, verr))
	}
	return source, fmt.Sprintf(TaskRepairDeadWorkspaceFmt, repo, root, source, rowID,
		TaskRepairDeadWorkspaceSourceLabel)
}

// taskWorkspaceRowRepo finds the ledger row whose deliverable.worktree IS dir and
// returns the REPO that row names (its deliverable.repo) — the repository the
// workspace was a working copy of. Returns ("", "", nil) when no row recorded the
// directory: the absence is an answer, not an error.
func taskWorkspaceRowRepo(dir string) (repo, rowID string, err error) {
	want := filepath.Clean(dir)
	ctx, cancel := context.WithTimeout(context.Background(), taskRepairLedgerTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", "", fmt.Errorf("opening the task ledger: %w", err)
	}
	defer db.Close() //nolint:errcheck // read-only handle; a close error carries no decision
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, deliverable FROM tasks WHERE deliverable LIKE ?`,
		"%"+DeliverableWorktreeKey+"%")
	if err != nil {
		return "", "", fmt.Errorf("reading deliverable.worktree rows: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only handle
	for rows.Next() {
		var id, raw string
		if serr := rows.Scan(&id, &raw); serr != nil {
			return "", "", fmt.Errorf("scanning a deliverable row: %w", serr)
		}
		d := taskDeliverableParse(raw)
		if strings.TrimSpace(d.Worktree) == "" {
			continue
		}
		if filepath.Clean(d.Worktree) != want {
			continue
		}
		if strings.TrimSpace(d.Repo) == "" {
			continue
		}
		return d.Repo, id, nil
	}
	if rerr := rows.Err(); rerr != nil {
		return "", "", fmt.Errorf("reading deliverable.worktree rows: %w", rerr)
	}
	return "", "", nil
}

// dirExists reports whether p is a directory on disk. It is a plain stat: the
// decision this file makes is "is there anything there at all", and the git
// question is asked separately (by the ONE verifier, taskRepoVerifiable).
func taskRepairPathExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
