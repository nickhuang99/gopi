// Worktree → task lookup: the ONE answer to "which ledger row owns this
// worktree?" — the SIBLING of the branch lookup (task_for_branch.go), sharing
// its scan, its owner shape and its resolution policy.
//
// WHY IT EXISTS. `gopi task show` addressed a row by its task_id only, while
// the operator standing in a git worktree holds the BRANCH name (`git branch
// --show-current`) or the worktree path (`pwd`) — never the row id. The branch
// half of that gap already had a verb (the retired `for-branch`); the WORKTREE
// half had nothing, because the deliverable blob records it too
// ({"repo":…,"branch":…,"worktree":…}) and every row created through the
// dispatch path carries it.
//
// WHY NOT A SECOND SCAN. The rows are the same rows and the fields are the
// same fields, so the read is the SHARED taskDeliverableScanRows and the
// ordering is the SHARED orderTaskForBranchOwners — the only thing that differs
// is the matcher (branch equality vs worktree equality). A second loader
// walking the same table with its own SQL would be free to disagree with the
// branch lookup about what an owner IS (Rules 13/14).
//
// WHAT IT DELIBERATELY DOES NOT DO. It is READ-ONLY, exactly like its sibling:
// no claim, no status move, no ledger write, no notify event, no git command.
// It never picks a "winner" among the owners — at the ROW level the answer is
// plural — and it never re-derives the deliverable blob's shape.
//
// A ROW IS A CANDIDATE ONLY IF IT NAMES A BRANCH. The shared scan skips the
// rows whose deliverable carries a worktree but no branch (they are not
// owners of a branch, and the branch lookup's unit is exactly that). Worktree
// matching therefore sees every row the branch lookup sees — no wider, no
// narrower — and the two answers cannot describe different universes.

package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// TaskForWorktreeQuery is the parsed, testable input of one worktree lookup:
// the worktree path asked about plus the optional repo narrowing. It exists so
// the resolution policy can be unit-tested with no ledger and no process (the
// same split TaskForBranchQuery uses for its verb).
type TaskForWorktreeQuery struct {
	// Worktree is the worktree DIRECTORY asked about.
	Worktree string
	// Repo, when non-empty, narrows the answer to owners whose deliverable
	// names that repo — the same worktree path cannot exist twice, but the
	// narrowing is carried for symmetry with the branch lookup and because a
	// caller who knows its repo must be able to say so.
	Repo string
}

// taskWorktreePathKey is the comparison key of a worktree path: absolute,
// cleaned, and free of a trailing separator, so `~/workspace/wt`,
// `/home/user/workspace/wt/` and `/home/user/workspace/./wt` are ONE worktree
// rather than three.
//
// The path is NOT symlink-resolved: the ledger records what the dispatch path
// recorded, and resolving here would make the comparison depend on the state
// of the filesystem at query time (a worktree removed and recreated elsewhere
// would keep matching a row it no longer belongs to). An empty or unclearable
// path returns "" — the caller refuses that query rather than matching every
// relative-looking row (Rule 15: a blank selector names no row).
func taskWorktreePathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// TaskForWorktree resolves a worktree path to every ledger row that names it.
//
// It is a PURE READ of the ledger (the shared scan runs one SELECT inside one
// read transaction; nothing here writes). An unreadable ledger returns
// Readable=false — never an error a caller could confuse with an empty answer,
// and never a partial owner list.
func TaskForWorktree(q TaskForWorktreeQuery, dbPath string) (TaskForBranchLookup, error) {
	out := TaskForBranchLookup{
		Branch:          strings.TrimSpace(q.Worktree),
		Worktree:        strings.TrimSpace(q.Worktree),
		Repo:            strings.TrimSpace(q.Repo),
		LedgerRowsTotal: taskForBranchRowsTotalUnknown,
	}
	want := taskWorktreePathKey(q.Worktree)
	if want == "" {
		return out, fmt.Errorf("task: worktree must not be empty — there is nothing to look up")
	}
	ctx := context.Background()
	path := memoryDBPath()
	if strings.TrimSpace(dbPath) != "" {
		path = dbPath
	}
	// The ledger this answer is about travels with it, exactly as the branch
	// sibling's does (Rule 15: the no-owner claim is scoped to ONE database).
	out.LedgerPath = path
	// The MACHINE travels with the ledger path for the same reason (Rule 15: the
	// denial is scoped to ONE host's ONE ledger), through the SAME reader the
	// branch sibling uses (Rule 14).
	out.Host = taskForBranchHostname()
	// The SAME read guard the branch sibling consults, for the same reason: a
	// pure read must not build the ledger it reads. An absent ledger holds no
	// rows, so it names no owner — and its size is 0 BY MEASUREMENT.
	if ledgerMissingForRead(path) {
		out.Readable = true
		out.LedgerRowsTotal = 0
		return out, nil
	}
	db, err := openTasksDB(ctx, path)
	if err != nil {
		// An unopenable ledger is NOT "this worktree has no owner": report the
		// read failure as a read failure (Rule 15).
		return out, fmt.Errorf("task: open ledger %s: %w", path, err)
	}
	defer db.Close()

	scan, err := taskDeliverableScanRows(ctx, db, func(branch, worktree, repo string, grounds []taskDeliverableGround) (bool, string) {
		if taskWorktreePathKey(worktree) == want {
			return out.Repo == "" || strings.TrimSpace(repo) == out.Repo, ""
		}
		// The SAME second axis, for the worktree sibling (epic 01a0dff1): the
		// directory a run created is a ground of this row exactly as the row's
		// own directory is, and the answer says which axis matched.
		for _, g := range grounds {
			if taskWorktreePathKey(g.Worktree) != want {
				continue
			}
			if out.Repo != "" && !sameWorkingPath(strings.TrimSpace(g.Repo), out.Repo) {
				continue
			}
			return true, taskDeliverableGroundProvenance(worktree, branch, g.Worktree)
		}
		return false, ""
	})
	if err != nil {
		return out, fmt.Errorf("task: scan ledger for worktree %q: %w", out.Worktree, err)
	}
	out.Readable = scan.Readable
	out.RowsWithBranch = scan.RowsWithBranch
	out.Owners = orderTaskForBranchOwners(scan.Owners)

	// The ledger's own size — every row, not only the deliverable-carrying ones
	// — so the renderer can tell a FRESH PROFILE from a populated ledger that
	// does not name this path. The same count function as the branch sibling
	// (Rule 14), on the handle already open.
	if n, countErr := taskForBranchCountRows(ctx, db); countErr != nil {
		out.LedgerRowsTotal = taskForBranchRowsTotalUnknown
	} else {
		out.LedgerRowsTotal = n
	}

	// The same repair as the branch sibling (epic 01a0e074): a worktree whose
	// row predates the plural-grounds producer records nothing, so the LIVE
	// registration plus the session that created it are read before "no owner"
	// is answered.
	if len(out.Owners) == 0 && out.Readable {
		q := taskGroundReconcileQuery{Worktree: out.Worktree, Repo: out.Repo}
		ans := taskGroundReconcile(ctx, db, q)
		taskGroundApplyReconcile(&out, q, ans)
	}
	return out, nil
}
