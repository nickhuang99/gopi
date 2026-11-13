package tools

// merger_graceful.go — what is LEFT of the merger's service vocabulary after the
// merger became a CRONJOB (epic daemon-machinery-removal, 2026-09-17). Two
// things, and both have a live reader:
//
//	① the merger's DATA DIRECTORY (<gopi data root>/merger): it holds the
//	   merger's log and the rebuild-pending marks the merge path drains, and it
//	   is the flock fallback location — none of which is about being resident;
//	② MASTER SAFETY (MergerForcedSafety + its report): the inspection-and-repair
//	   a successor runs before its tick, which is what makes a one-tick process
//	   safe after its predecessor was hard-killed.
//
// HISTORY — what this file used to hold and why each part is gone. Under the
// stateless redesign (epic merger-ff-park-file, 2026-09-16) the per-instance
// STATE FILE, the durable STOP REQUEST file, the retired FORCE request file and
// every path helper for them were deleted: each recorded something about an
// IN-FLIGHT pass, and a stateless tick has nothing in flight to record. The
// RESIDENT-FORM SERVICES went the same way with the cron form (2026-09-17):
//
//   - the STOP SIGNAL VOCABULARY (MergerSignalName / MergerSignalNames /
//     mergerStopSignals) — "exit at the tick boundary" describes a process that
//     has a next boundary to exit at. A one-tick process IS its boundary; it
//     ends by returning, and nothing external signals it to stop.
//   - the SUCCESSOR HANDOVER WAIT (MergerHandoverWaitDefault) — the window a
//     RESTARTING daemon waited for the lock its predecessor held. Nothing
//     restarts a merger now: the next TICK is a new process, and it either takes
//     a free flock or is refused with a message naming the holder.
//   - the MergerPidAlive DELEGATE — it existed so the stop path could ask "is the
//     lock holder still there" without taking the lock. Its subject is the
//     worker's control surface now (the shared PidAlive, service_control.go),
//     and a merger-thin delegate read by nobody is a second spelling of one
//     predicate (Rule 13).
//
// The lock (serve.lock, flock-based) is the one durable fact the merger still
// has, and it is MEASURED — never asserted from a record.

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// MergerStateDirName is the directory (under the gopi data root) holding
	// the merger's service files (its daemon log). One directory, so an
	// operator has ONE place to look and a sandboxed merger (GOPI_HOME) never
	// writes into the real profile.
	MergerStateDirName = "merger"

	// mergerStateDirMode is the mode of that directory (Rule 1: named, one
	// place).
	mergerStateDirMode = ControlDirMode

	// DELETED: MergerHandoverWaitDefault — the successor-restart handover window.
	// Nothing waits for a predecessor's lock any more; the next tick acquires a
	// free flock or is refused, and it does so synchronously (epic
	// daemon-machinery-removal, 2026-09-17). internal/tools/verify_gate.go's
	// ordering note was updated with it.
)

// MergerGracefulDir resolves <gopi data root>/merger, creating it (the
// writer's half; the mechanism is shared — see service_control.go).
func MergerGracefulDir() (string, error) {
	return ControlDir(MergerStateDirName)
}

// MergerGracefulDirReadOnly resolves <gopi data root>/merger WITHOUT creating
// it — the reader's half (a status probe must not have the side effect of
// creating directories; the same read-only discipline merger_status.go keeps
// for the lock).
func MergerGracefulDirReadOnly() (string, error) {
	return ControlDirReadOnly(MergerStateDirName)
}

// --- master safety at start-up ------------------------------------------

// IntegrationBranchName is the branch a merger sweep integrates into — the
// cross-package source of truth (Rule 14). The safety check in cmd/agent takes
// the branch it must inspect from HERE rather than mirroring the literal: a
// mirrored copy is drift waiting to happen, and drift here means the safety
// check inspects a different branch than the sweep lands on.
const IntegrationBranchName = "master"

// mergeHeadName is git's marker file for an UNFINISHED merge: while it exists
// the index and the working tree hold a half-applied merge, and `git status`
// reports "You have unmerged paths". Its presence is the machine-decidable
// answer to "did a hard stop land inside a landing?" — no guessing from logs.
const mergeHeadName = "MERGE_HEAD"

// MergerRepoSafety is one configured repo's safety picture: what an
// interrupted landing looks like, and what the successor had to clean.
type MergerRepoSafety struct {
	Repo            string
	Tip             string // HEAD (the integration branch's tip)
	UnfinishedMerge bool   // MERGE_HEAD present at inspection time
	Dirty           string // `git status --porcelain` (tracked+untracked), "" = clean
	Cleaned         string // what the cleanup did ("" = nothing was needed)
	Err             string
}

// Report renders the repo's safety line for the start-up report.
func (s MergerRepoSafety) Report() string {
	parts := []string{"tip " + shortTip(s.Tip)}
	switch {
	case s.Err != "":
		parts = append(parts, "INSPECTION FAILED: "+s.Err)
	case s.Cleaned != "":
		parts = append(parts, "interrupted landing rolled back: "+s.Cleaned+" (master restored buildable)")
	case s.UnfinishedMerge:
		parts = append(parts, "UNFINISHED MERGE LEFT IN PLACE — a human must check this repo")
	case s.Dirty != "":
		parts = append(parts, "working tree dirty: "+strings.Join(strings.Fields(s.Dirty), " "))
	default:
		parts = append(parts, "no unfinished merge, tree clean")
	}
	return strings.Join(parts, ", ")
}

// shortTip renders a commit id's first 12 characters (the same rendering the
// merger's other reports use).
func shortTip(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	if sha == "" {
		return "(unknown)"
	}
	return sha
}

// MergerForcedSafety inspects repo and, when clean is set, repairs the ONE
// state a hard stop can leave behind: an unfinished merge.
//
// WHY the repair is safe: the merger only ever MERGES (it edits nothing —
// conflicts are PARKED for an ordinary worker, never resolved in-process, and
// nothing else writes master). So uncommitted state in the merger's own working
// tree is by construction the aborted-merge artifact, and the branch it was
// merging is untouched (its ref is where it was). Restoring the tree to HEAD
// with `reset --hard` therefore discards NOTHING anyone authored — it returns
// the repo to the buildable tip the tick started from.
//
// ITS CALLER IS THE SUCCESSOR, not the dying process: see the start-up safety
// sweep in cmd/agent. A process that is alive, holds the lock and is not being
// written to by the dead pass is strictly the better repairer.
func MergerForcedSafety(repo, master string, clean bool) MergerRepoSafety {
	s := MergerRepoSafety{Repo: repo}
	s.Tip = MergeVerifyHead(repo)
	if s.Tip == "" {
		s.Err = "git rev-parse HEAD failed (not a git repo?)"
		return s
	}
	if st, err := AdoptionGitStatus(repo); err == nil {
		s.Dirty = strings.TrimSpace(st)
	}
	mergeHead := filepath.Join(repo, ".git", mergeHeadName)
	if _, err := os.Stat(mergeHead); err != nil {
		if st, gerr := mergeRevertGit(repo, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); gerr == nil && strings.TrimSpace(st) != "" {
			// A worktree's .git is a FILE pointing at the real git dir, so the
			// stat above cannot see MERGE_HEAD there — ask git itself.
			s.UnfinishedMerge = true
		}
	} else {
		s.UnfinishedMerge = true
	}
	if !clean || !s.UnfinishedMerge {
		return s
	}
	if _, err := mergeRevertGit(repo, "reset", "--hard", "HEAD"); err != nil {
		s.Err = "cleanup failed: " + err.Error()
		return s
	}
	s.Cleaned = "git reset --hard HEAD (unfinished merge aborted; branch refs untouched)"
	s.UnfinishedMerge = false
	if st, err := AdoptionGitStatus(repo); err == nil {
		s.Dirty = strings.TrimSpace(st)
	}
	return s
}
