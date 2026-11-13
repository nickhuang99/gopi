package tools

// task_ground_session_reading.go — the RENDERING half of the shared ground
// reading (the reading itself lives with the claim seams in
// task_deliverable_worktree.go; see taskGroundSessionReadingFor).
//
// ONE sentence, TWO faces: the claim-time candidate's INFO line (gap A) and the
// read-only advisory show line (gap B) describe the same facts — repo /
// worktree / branch, plus the honest why — so they render through the ONE
// layout and the ONE set of axis constants below. Two spellings of one sentence
// is the drift Rules 1b/13 forbid, and a reader who greps `branch=` must find
// both faces.

import (
	"os"
	"strings"
)

// The axis keys and the absence marker of the shared sentence, named once
// (Rule 1b): both faces print the same three axes, so a reader who greps
// `branch=` finds both.
const (
	taskGroundKeyRepo     = "repo"
	taskGroundKeyWorktree = "worktree"
	taskGroundKeyBranch   = "branch"
	// taskGroundKeyAbsent is what an axis that resolved to nothing renders —
	// the explicit absence, never a blank.
	taskGroundKeyAbsent = "(none)"
)

// taskGroundResolveList is the CLAIM-TIME worktree resolution: the directory a
// branch is checked out in, found from the repo's REGISTERED linked worktrees —
// listed once, so the caller's candidate set and this reading describe the same
// registration. Read-only; an unreadable listing resolves nothing (the reading
// keeps its branch axis, and the caller's fail-closed guards decide).
func taskGroundResolveList(repo, branch string) (string, bool) {
	branch = strings.TrimSpace(branch)
	if repo == "" || branch == "" {
		return "", false
	}
	linked, err := taskRepoLinkedWorktrees(repo)
	if err != nil {
		return "", false
	}
	for _, e := range linked {
		if e.branch == branch {
			return e.path, true
		}
	}
	return "", false
}

// taskPathExists reports whether path is a directory that exists RIGHT NOW —
// the live-verification half of a recorded ground (os.Stat only; a symlink to a
// directory counts, because that is where the run stands).
func taskPathExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// taskGroundText renders one reading through the ONE layout both faces use:
// `repo=… worktree=… branch=… — <why>`. An axis that resolved to nothing renders
// the explicit absence marker, never a blank.
func taskGroundText(r taskGroundSessionReading) string {
	return taskGroundKeyRepo + "=" + taskGroundAxisValue(r.repo) + " " +
		taskGroundKeyWorktree + "=" + taskGroundAxisMarked(r.worktree, r.worktreeOK) + " " +
		taskGroundKeyBranch + "=" + taskGroundAxisMarked(r.branch, r.branchOK) + " — " + r.why
}

// taskGroundAxisMarked renders one axis with its LIVE-VERIFICATION marker: the
// whole point of the reading is that it was resolved just now, so whether the
// value still exists is part of the answer, not a detail. An axis that resolved
// to nothing carries no marker (absence needs no qualifier).
func taskGroundAxisMarked(v string, verified bool) string {
	if strings.TrimSpace(v) == "" {
		return taskGroundKeyAbsent
	}
	if verified {
		return strings.TrimSpace(v) + " " + TaskShowGroundExistsMarker
	}
	return strings.TrimSpace(v) + " " + TaskShowGroundStaleMarker
}

// taskGroundAxisValue renders one axis, or the explicit absence marker.
func taskGroundAxisValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return taskGroundKeyAbsent
	}
	return strings.TrimSpace(v)
}
