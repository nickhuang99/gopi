package main

// merger_work_lock.go — the single-instance flock that guards "who may write to
// these repos", shared by every face that mutates git.
//
// WHAT THIS FILE IS NOW (epic merger-delete-dead-impl, 2026-09-19). It used to be
// the CALLER-SIDE rule of the resident merger's work lock. That daemon is
// retired and deleted; the LOCK is not — every live face that lands refs, deletes
// branches or reclaims worktrees still takes it, and the admission/inheritance
// machinery the retired take-over built around it is what makes those live faces
// work under a holder. So the surviving half was RE-HOMED HERE rather than
// deleted with the daemon:
//
//	mergerWorkLock / mergerWorkChildMarked — the rule itself (unchanged).
//	mergerLockHeldByAncestor / inheritedLockHandleForExemptFace —
//	    the ADMISSION rule at the one acquisition funnel (acquireServeLock).
//	mergerLockOwnerEnv / mergerTakeoverArgv0 — the marker and the process name
//	    the admission messages use.
//
// THE DEFECT THE LOCK CLOSES (epic merger-always-flock, 2026-09-16; measured on
// this host). With an external process holding serve.lock:
//
//	gopi merger --once            → exit 1, "another merge process is already running"
//	gopi task merge --all --repo R → exit 0, "merge feat/x merged clean merge,
//	                                 branch deleted"   ← master ADVANCED
//
// i.e. the git post-receive hook's one-line trigger
// (scripts/git-hooks/post-receive) merged a branch into master while the
// single-instance holder was live. The flock guarded the merger LOOP but not the
// merge WORK. Refusing (exit 1, naming the holder) is the correct semantics for
// these faces: they are script/hook triggers, and the branches they skip stay
// exactly where they were for the next pass to land.
//
// THE NON-OBVIOUS HALF, kept from the take-over's design because it is a
// correctness property of the LOCK and not of that launcher: a process the holder
// is WAITING ON must not be refused its own ancestor's lock, or the lock becomes a
// deadlock. That is mergerLockHeldByAncestor, checked at the ONE acquisition
// funnel so every face is covered by one rule rather than by a rule repeated per
// face (Rules 2/13).

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/tools"
)

// The program names these faces print in their diagnostics (Rule 1: each
// spelling is declared once, so one grep matches every line the face emits).
const (
	// mergeFaceProg is `gopi task merge` — the post-receive hook's trigger and
	// the manual merge round.
	mergeFaceProg = "gopi task merge"
	// adoptFaceProg is `gopi task adopt` — the branch-adoption sweep (its
	// ③a half COMMITS the probe-merged adoptions onto master).
	adoptFaceProg = "gopi task adopt"
	// mergedCleanupFaceProg is `gopi task merged-cleanup` — branch + worktree
	// reaping.
	mergedCleanupFaceProg = "gopi task merged-cleanup"
	// mergeExecuteFaceProg is `gopi task merge-execute <task-id>` — the
	// merge-backlog executor's single-row landing.
	mergeExecuteFaceProg = "gopi task merge-execute"
	// sweepWorktreesFaceProg is `gopi task sweep-worktrees` — the worktree
	// reclaim sweep (git registrations).
	sweepWorktreesFaceProg = "gopi task sweep-worktrees"
)

// mergerTakeoverArgv0 is the process NAME the admission messages quote. It used
// to be the retired take-over's argv[0] marker; the name is kept because the
// messages it appears in (a lock that could not be restamped, a lock file
// replaced underneath its holder) are about THIS process's own lock handling
// wherever they are printed, and renaming the noun would not change what the
// sentence tells the operator (Rule 1: the spelling is declared once).
const mergerTakeoverArgv0 = "gopi"

// mergerWorkLock takes the single-instance flock for a foreground face that
// mutates git repos and returns the release function (defer it). A refusal is
// FATAL and names the live holder: two writers in the same repos is the
// double-merge this lock exists to prevent, and a face that continued anyway
// would be the defect reported above.
//
// The face's OWN children are exempt (mergerWorkLockInherited): their parent
// already holds the lock — the flock lives on the open file description, which a
// spawned child shares unless it is fenced with O_CLOEXEC — so re-acquiring would
// refuse the child and deadlock the parent that is waiting on it.
func mergerWorkLock(prog string) func() {
	if mergerWorkLockInherited() {
		return func() {}
	}
	lock, err := acquireServeLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
		os.Exit(1)
	}
	return func() {
		//stylegate:ignore release of the work lock when the face exits; process exit closes the descriptor too
		_ = lock.Close()
	}
}

// mergerWorkLockInherited reports whether THIS process is a child of a lock
// holder — i.e. it must NOT take the work lock its parent already holds (see
// mergerWorkLock).
func mergerWorkLockInherited() bool {
	return mergerWorkChildMarked() || mergerLockHeldByAncestor()
}

// mergerWorkChildMarked reports the role marker a lock holder stamps on a child it
// spawns WHILE HOLDING the work lock. Both markers belong to gopi processes that
// reach the same CLI code as any other invocation and are spawned by a holder
// that then WAITS for them:
//
//   - the conflict RESOLVER (epic merger-single-owner-architecture, retired with
//     the resolve machinery — the merger now LEAVES a conflicting branch alone and
//     REPORTS it instead of resolving it), tagged tools.MergeResolveRoleEnv;
//   - the merge-VERIFY child (epic merger-verify-gate, retired the same way),
//     tagged mergeVerifyChildRoleEnv.
//
// The SPAWNS are gone; the exemption STAYS, because it is inheritance bookkeeping
// rather than a property of those two children specifically: a still-running
// older holder (or a child that outlived its parent) is exactly such a child, and
// a process must never be refused the lock its ancestor already holds — a missing
// exemption does not merely refuse a child, it refuses the child the HOLDER IS
// WAITING ON, which turns a lock into a deadlock.
func mergerWorkChildMarked() bool {
	return os.Getenv(tools.MergeResolveRoleEnv) != "" || os.Getenv(mergeVerifyChildRoleEnv) != ""
}

// --- the ADMISSION rule (re-homed from the retired take-over's lock file) ---

const (
	// mergerLockOwnerEnv is the marker a lock holder stamps into a child's
	// environment (ALIAS of tools.TakeoverLockOwnerEnv — one definition, two
	// packages, Rule 14). It says "an ancestor of mine launched me holding the
	// singleton"; it is NEVER sufficient on its own (mergerLockHeldByAncestor):
	// a variable anyone can type must not be able to bypass the guard.
	mergerLockOwnerEnv = tools.TakeoverLockOwnerEnv

	// mergerLockOwnerStaleFmt reports the one inheritance shape that must not be
	// trusted: the marker says an ancestor holds the lock, but NO live ancestor
	// holds it. The caller then takes the ordinary acquisition path, which
	// refuses if somebody else holds it.
	mergerLockOwnerStaleFmt = "%s: %s is set but no ANCESTOR of this process holds the single-instance lock — ignoring the marker (an inherited marker is evidence, never permission)\n"
	// mergerLockReplacedFmt reports the state the orphan probe refuses a fresh
	// acquisition for, met from the INHERITANCE side: a live ancestor holds a
	// flock on an inode that is no longer what the lock path names, so the guard
	// is detached from its name and the exemption must not be granted.
	mergerLockReplacedFmt = "%s: %s is set and a live ancestor holds a lock, but NOT on the file %s names (it was replaced underneath) — the guard is detached; this face proceeds as an ordinary unguarded caller\n"
)

// inheritedLockHandleForExemptFace returns the descriptor an ADMITTED descendant
// face works with when its ancestor holds the lock (mergerLockHeldByAncestor).
//
// IT IS A READ-ONLY, NON-LOCKING HANDLE, AND THAT IS THE WHOLE DESIGN — stated at
// length because the alternative reading ("this looks like a lock we hold") is
// exactly the misunderstanding that would turn a convenience into a defect:
//
//   - It is opened O_RDONLY, so NOTHING can write to the file. That matters
//     because the file's content is the holder's pid STAMP: a face that
//     re-truncated it would relabel a live merge round with its own pid — the
//     corpse-as-holder misreport this surface has been fixed for twice.
//   - It takes NO flock, so it guards NOTHING and releases NOTHING. Every caller
//     of acquireServeLock defers `Close()` (or hands the handle to a release
//     closure), and closing a descriptor that never held the lock is a no-op on
//     the guard — the ancestor's flock lives on the ANCESTOR's open file
//     description and is untouched by anything this process does.
//   - Its callers use it for exactly two things while exempt: `Name()` (the
//     diagnostics path) and the identity check
//     (ensureServeLockIdentity/sameFileAsPath stats it against the path and finds
//     them the same file). Both are reads.
//
// WHY THE EXEMPT FACE CANNOT SIMPLY BE HANDED THE REAL FD: the biggest such face
// is started by the holder through the ordinary tool surface (a shell command),
// not by Go code that could attach ExtraFiles. There is no construction to add a
// descriptor to, so the exemption must be an ADMISSION decision rather than a
// hand-off. The daemon seam CAN pass its fd explicitly (workerDaemonInheritLock)
// precisely because its children are spawned by Go code it controls; that
// difference is why two models exist — see merger_lock_exempt.go.
func inheritedLockHandleForExemptFace() (*os.File, error) {
	path, err := serveLockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s read-only for the inherited-lock record: %w", path, err)
	}
	return f, nil
}

// mergerLockHeldByAncestor reports whether THIS process is a designated descendant
// of the current single-instance lock holder — the ADMISSION RULE for a face the
// holder runs while it waits (`gopi merger --once`, whose close loop carries the
// rebuild contract of Rule 12; `gopi task merge`).
//
// WHY THESE FACES CANNOT SIMPLY BE REFUSED: they inherit no lock fd — the holder
// fences its children — and the holder is WAITING on them. A refusal would
// therefore not be a safety property but a self-deadlock: the holder would block
// forever on a child it refused. (The daemon seam reasons the same way about its
// own children: workerDaemonInheritLock passes the descriptor explicitly rather
// than let the child re-acquire. See merger_lock_exempt.go for why that explicit
// model is not available here and what is used instead.)
//
// TWO INDEPENDENT FACTS ARE REQUIRED, and neither is sufficient alone:
//
//   - the MARKER (mergerLockOwnerEnv) says a holder launched this process
//     lineage. It travels through the environment, so it is evidence about
//     PROVENANCE only — any caller can set it by hand;
//   - the PROOF is read from the kernel: some ancestor of this process (walked
//     through each live process's own /proc/<pid>/stat, never guessed) is holding
//     the lock file's flock right now (tools.LockEvidences — the same reader the
//     refusal path uses, Rule 2). An INDEPENDENT process can never satisfy it,
//     because its ancestors hold nothing.
//
// A THIRD FACT IS REQUIRED THAT IS NOT OBVIOUS, and it exists because of the STATE
// the exact same guard tracks elsewhere: the lock path may have been REPLACED
// under an unlink (the 2026-09-12 hazard LockProbe and ensureServeLockIdentity
// both handle). tools.LockEvidences can then report the live ancestor through the
// fdinfo scan — which joins on the INODE the descriptor locks, not on the path —
// so evidence of a holder is NOT evidence that the file the PATH names is the file
// being held. A holder in that state has a guard that is detached from its name:
// the very condition the orphan probe refuses a NEW acquisition for. This function
// therefore also requires that the ancestor's lock is on the CURRENT file the path
// names, and announces the case it refuses, so the operator sees "the guard is
// down, this face sweeps unguarded" rather than an unexplained refusal or a silent
// wrong admission.
//
// A marker WITHOUT the proof is reported and IGNORED (Rule 15): the caller then
// takes the ordinary acquisition path, which refuses if the lock is held. An
// inheritance claim is never permission by itself.
func mergerLockHeldByAncestor() bool {
	if os.Getenv(mergerLockOwnerEnv) == "" {
		return false
	}
	path, err := serveLockPath()
	if err != nil {
		return false
	}
	held, ok := tools.LockEvidences(path)
	if !ok || len(held) == 0 {
		fmt.Fprintf(os.Stderr, mergerLockOwnerStaleFmt, mergerProgTopLevel, mergerLockOwnerEnv)
		return false
	}
	// THE IDENTITY HALF (see the third fact above): the lock the ancestor holds
	// must be the file the PATH currently names. sameFileAsPath is the existing
	// reader for exactly this question (Rule 2), so no second implementation of
	// the unlink hazard is introduced here.
	current, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, mergerLockOwnerStaleFmt, mergerProgTopLevel, mergerLockOwnerEnv)
		return false
	}
	defer current.Close() //stylegate:ignore read-only probe descriptor used for one identity check
	if ok, statErr := sameFileAsPath(current, path); statErr != nil || !ok {
		fmt.Fprintf(os.Stderr, mergerLockReplacedFmt, mergerProgTopLevel, mergerLockOwnerEnv, path)
		return false
	}

	holders := make(map[int]bool, len(held))
	for _, e := range held {
		holders[e.PID] = true
	}
	for _, pid := range tools.SelfAncestorPIDs() {
		if holders[pid] {
			return true
		}
	}
	fmt.Fprintf(os.Stderr, mergerLockOwnerStaleFmt, mergerProgTopLevel, mergerLockOwnerEnv)
	return false
}

// mergerLockBaseName is the FILE NAME of the single-instance lock, named once so
// no diagnostic has to derive it (Rule 1b). It is the value serveLockFilename
// carries; the alias exists so this file's diagnostics do not reach across into
// the CLI's constant block for a string they merely print.
const mergerLockBaseName = serveLockFilename

// takeoverLockName is the name a lock descriptor is given in diagnostics, so a
// message names the FILE rather than a bare fd number. Built from the same
// constant the lock path is built from, so the two ends cannot drift.
var takeoverLockName = filepath.Base(mergerLockBaseName)
