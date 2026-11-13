package tools

// task_submit_clone_ground.go — THE CLONE-GROUND ARM of the durable-repo
// resolution (epic 01a160a2; measured rows 01a1604a, 01a15b96, 01a16053).
//
// THE DEFECT, measured three times before this file existed. In the CLONE
// channel a worker's ground is not a linked worktree: it is a plain
// `git clone` at <workspace_root>/<task-id> (internal/devworkspace.Open). Its
// `.git` is a DIRECTORY, so it is a repository of its own, and
// `git rev-parse --git-common-dir` answers the clone ITSELF — the linked-worktree
// arm (DurableRepoForSubmit → mergeExecMainRepo) therefore returned the clone
// path verbatim, correctly concluding "this is already a main checkout".
//
// So every row submitted from inside a clone-channel run was born naming the
// directory the run's own end deletes:
//
//	$ gopi task show 01a1604a-63d9-…
//	deliverable.repo: /home/nick/workspace/01a16033-c069-72fe-8b4e-d15d706f41ac
//	reason: clone channel: repo "…/01a16033-c069-…" is not usable: the directory
//	        no longer exists on disk — refusing to push a workspace that cannot
//	        be built …   runs: none (no run log in the directory)
//
// The three measured rows were all CHILDREN of a run in that workspace, and all
// three were correctly refused at spawn — the row is not "broken", it is
// stranded: the directory it names will never exist again.
//
// THE ANSWER, and why it is not a guess. The checkout knows where it came from:
// `git remote get-url origin` is a fact about the directory (the same one
// internal/devworkspace/trace.go already asks when it needs a workspace's
// provenance, and its SourceRepoDisplayPath is the ONE renderer of "the local
// checkout path an origin URL names" — Rule 13/14, no second implementation).
// A clone ground is recognised by the ONE ground-model resolver
// (devworkspace.ResolveGroundState: `.git` a DIRECTORY = the CLONE model) and by
// the ONE workspace-root reader (config.WorkspaceRootConfigured + a
// worktreeroots.Within boundary), exactly as the dead-workspace repair does.
//
// THE BOUNDARY IS DELIBERATE (three limits, each a decision):
//   - a path OUTSIDE the workspace root is not touched — no stat, no git call,
//     no behaviour change for an ordinary repository or an ordinary clone;
//   - a path DEEPER THAN the channel's own shape is not touched either, even
//     under the root. `being under the workspace root` was measured to be too
//     WIDE a test (task 01a16218): the workspace root is also the ordinary
//     place a checkout, a test scratch home or an operator's scratch repo
//     lives, so a clone-model repository nested below it was refused as "this
//     run's workspace" although no run ever created it. The channel's shape is
//     ONE level — `devworkspace.Dir` (the ONE producer) joins the root with the
//     task id and nothing else — so the predicate is the DIRECT-CHILD test in
//     cloneWorkspaceDirShape below, never a bare containment check;
//   - a LINKED-WORKTREE ground under the root is not this arm's business: its
//     durable answer is the main checkout it is a guest of, which
//     mergeExecMainRepo already returns;
//   - when the origin cannot be turned into a LIVE git work tree, the arm
//     REFUSES with a sentence naming the path, the root and the reason — never
//     a fallback to the doomed path (that is the defect) and never a guessed
//     repository (Rule 12's discipline applied to a path).
//
// It writes nothing: the answer is a string, and the ledger is not edited.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// TaskDurableRepoCloneGroundNoteFmt is the ONE spelling of "the repo you
	// named is this run's clone workspace; the row now names the repository it
	// was cloned from" (Rule 1b). It names the workspace, the root that makes it
	// reclaimable, and the substituted repository — the three facts a reader
	// needs to trust the substitution.
	TaskDurableRepoCloneGroundNoteFmt = "task_submit: repo %q is the clone channel's workspace for one run " +
		"(under the workspace root %q) — stamping the source repository %s it was cloned from, " +
		"so the row outlives this run"

	// TaskDurableRepoCloneGroundRefusalFmt is the refusal for a clone workspace
	// whose source cannot be named. It is a REFUSAL rather than a fallback: the
	// only other value available is the path that is about to be deleted, which
	// is the defect this file exists to remove.
	TaskDurableRepoCloneGroundRefusalFmt = "no durable repository for %s: it is the clone channel's workspace " +
		"for one run (under the workspace root %q), so it cannot be the row's deliverable.repo — %s " +
		"(re-point the row at a live git work tree: gopi task update <task_id> --repo <path-to-live-work-tree>)"
)

// cloneWorkspaceDirShape reports whether dir has the ONE shape the clone
// channel gives its per-run workspace: a DIRECT child of the workspace root.
//
// WHY A SHAPE AND NOT A CONTAINMENT TEST (measured, task 01a16218, master
// ebbecc7). The arm used to accept every path `worktreeroots.Within(dir, root)`
// — anything at or below the root. The root is not a private namespace: by
// default it IS `<home>/workspace` (worktreeroots.DefaultRoot), which is where
// checkouts, an operator's scratch repositories and — on this host — the test
// suite's own scratch home (`/home/nick/workspace/gopi-test-scratch`) live. A
// plain `git init` fixture two levels down was therefore announced as "the
// clone channel's workspace for one run (under the workspace root …)" and
// refused, though no run had ever touched it:
//
//	$ go test ./cmd/agent -run TestTaskVerbCloneCarriesARepo
//	--- FAIL: TestTaskVerbCloneCarriesARepo
//	    gopi task clone … --repo /home/nick/workspace/gopi-test-scratch/queue/queue-device-cli-clone-repo-3615867/override-main
//	    exit 1 (want 0)
//	    gopi task clone: task clone: --repo: no durable repository for /home/nick/workspace/gopi-test-scratch/queue/queue-device-cli-clone-repo-3615867/override-main:
//	    it is the clone channel's workspace for one run (under the workspace root
//	    "/home/nick/workspace"), so it cannot be the row's deliverable.repo — the
//	    checkout declares no readable origin remote: … No such remote 'origin'
//
// The channel produces exactly one depth: `devworkspace.Dir(root, taskID)` is
// `filepath.Join(root, taskID)` — the root, ONE task-id component, nothing
// else (the opener, the reclaimer and the ledger stamp all go through it, so it
// is the ONE definition of the shape). Anything deeper cannot be a workspace
// this channel made, and the arm must stay out of it: the substitution it
// performs ("stamp the source repository instead") and its refusal are answers
// to a question only a real workspace asks.
//
// The DIRECT-CHILD test is deliberately not "contains a task-id-looking name":
// an id-shape predicate would re-decide the naming rule here (Rule 1's second
// copy) and would still mis-fire on an operator's own `<root>/<uuid>` checkout.
// The depth is the channel's own invariant; the model check below supplies the
// remaining discrimination.
func cloneWorkspaceDirShape(dir, root string) bool {
	dir, root = filepath.Clean(strings.TrimSpace(dir)), filepath.Clean(strings.TrimSpace(root))
	if dir == "" || dir == "." || root == "" || root == "." || root == string(filepath.Separator) {
		return false
	}
	// The root ITSELF is never a workspace: the channel opens a subdirectory
	// inside it and reuses an existing one by task id.
	if !worktreeroots.Within(dir, root) || dir == root {
		return false
	}
	return filepath.Dir(dir) == root
}

// cloneGroundSourceRepo answers the durable repository for a repo path that IS
// the clone channel's per-run workspace.
//
// applies=false means "this arm has nothing to say about dir": the path is
// outside the workspace root, or it is not a CLONE-model ground (a linked
// worktree, or a directory that is not a ground at all) — the caller's existing
// resolution keeps the question and its behaviour is unchanged.
//
// applies=true with err != nil is a REFUSAL: dir is a clone ground and its
// source could not be named as a live repository.
func cloneGroundSourceRepo(dir string) (repo, note string, applies bool, err error) {
	root, _, rerr := config.WorkspaceRootConfigured()
	if rerr != nil || strings.TrimSpace(root) == "" {
		// An unreadable root is not a licence to guess which checkouts are this
		// channel's (the same boundary the dead-workspace repair draws).
		return "", "", false, nil
	}
	if !cloneWorkspaceDirShape(dir, root) {
		return "", "", false, nil
	}
	// THE MODEL IS ASKED, NOT INFERRED FROM THE PATH SPELLING: the ONE ground
	// resolver decides whether dir is a clone (its own repository) — a linked
	// worktree under the same root must keep its own answer.
	st := devworkspace.ResolveGroundState(dir)
	if !st.Alive || st.Model != devworkspace.GroundModelClone {
		return "", "", false, nil
	}
	out, gerr := mergeExecGit(dir, "remote", "get-url", "origin")
	if gerr != nil {
		return "", "", true, fmt.Errorf(TaskDurableRepoCloneGroundRefusalFmt, dir, root,
			"the checkout declares no readable origin remote: "+strings.TrimSpace(gerr.Error()))
	}
	src, serr := devworkspace.SourceRepoDisplayPath(out)
	if serr != nil {
		return "", "", true, fmt.Errorf(TaskDurableRepoCloneGroundRefusalFmt, dir, root, serr.Error())
	}
	if verr := taskRepoVerifiable(src); verr != nil {
		return "", "", true, fmt.Errorf(TaskDurableRepoCloneGroundRefusalFmt, dir, root,
			fmt.Sprintf("its origin %s is not a live git work tree either: %v", src, verr))
	}
	return src, fmt.Sprintf(TaskDurableRepoCloneGroundNoteFmt, dir, root, src), true, nil
}
