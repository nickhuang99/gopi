package main

// THE RUN-SCOPE LOCK ON THE RESUME FACE (epic run-lock-per-run-dir, 2026-11-05).
//
// THE HOLE THIS CLOSES. internal/runlock + run_lock_seam.go cover the two arms
// that carry a TASK ROW — `gopi task run <id>` (an explicit target) and a
// worker round. The RESUME face carries no row at all: `gopi session resume
// <target>` and `gopi --session <id>` (one implementation, cli_resume.go
// resumeSession) load a checkpoint and then CONTINUE THE SESSION THAT EXISTS —
// appending to the same <session>.jsonl and the same <session>.raw.log inside
// the same run directory a worker may be executing at that very moment. Two
// processes then write one directory with nothing arbitrating, which is exactly
// the corruption the lock exists to make impossible.
//
// WHY THE RUN DIRECTORY AND NOT A ROW. The lock's scope is ONE RUN DIRECTORY,
// not one ledger row (internal/runlock's package comment: sessions/<id>/ is the
// address, and two different rows execute concurrently). A resume often has no
// row to name — a bare `gopi "<prompt>"`, a chat, a session uuid — so the
// directory is also the ONLY address this face can offer. Taking the lock on it
// means the resume and the worker round stand on the same inode, which is what
// makes them exclude each other; a row-keyed second lock would miss every
// row-less session and would be a second definition of "this run is executing".
//
// THE ADDRESS IS THE CHECKPOINT'S OWN DIRECTORY. resumeIdentityFor (cli_resume.go)
// is the ONE place that says which session record a resume continues, and its
// answer is either a record INSIDE the checkpoint's directory or no record at
// all. This seam reads the same fact from the same input: the run directory is
// filepath.Dir(rawLogPath), and the lock is taken ON it through
// taskRunTakeLockDir — the same body the run funnel uses, so the hand-off, the
// announcement and the refusal text have ONE implementation (Rules 13/14).
//
// The lock is NOT taken by target string through taskRunTakeLock: that
// derivation goes through tools.TaskSessionDir, which is GopiDir-based, and a
// relocated sessions root (config.SessionsRootEnv — `gopi chat --replay`) makes
// it name a directory the checkpoint does not live in. runlock.Lock MkdirAll's
// what it is given, so the wrong address would create and lock an empty
// directory beside the run: a lock that excludes nobody. The checkpoint's own
// parent is the address that cannot drift from the files being written.
//
// WHERE IT SITS: AFTER the ground chdir, BEFORE the checkpoint load, the agent
// construction, the ledger tag and the run itself. That position is the
// requirement, not a preference:
//
//   - after the ground chdir, because the ground is the row's own workspace and
//     a resume refused for a lock must not have moved into it first;
//   - BEFORE LoadCheckpointWithSource, newAgentResuming and ag.Resume, because
//     those are the calls that CREATE session files (the fresh record of a
//     rebuilt identity, the run's own response log beside the checkpoint, the
//     JSONL append, the raw-log append). A refusal that happened after them
//     would be a refusal that already wrote.
//
// WHAT IT IS NOT: no second lock file (it is runlock.LockFileName, taken
// through runlock.Lock), no ledger transition, no CAS change, no new state, no
// resident process and no sweep (Rule 35 — the kernel is the cleaner). The lock
// dies with the process, which is what makes `kill -9` on a resume holder
// immediately resumable with no sleep and no stale-file cleanup.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/config"
)

// resumeUnlockedDirNotice is printed when NO run directory can be named for
// this checkpoint, so no lock is taken. It is a NOTICE and not a refusal on
// purpose, and it is never silent (Rule 15).
//
// Two shapes reach it, and the text names which: (a) the sessions root cannot
// be resolved or the comparison fails — an UNVERIFIABLE address; (b) the
// checkpoint's directory is not a run directory of this profile at all (a raw
// log copied out of a tree, a fixture's log, an export — the shape
// resumeIdentityFor documents). On shape (b) the resume does not continue
// anybody's session record anyway: it mints a fresh id and writes its record
// under the sessions root, so there is no existing run directory for a second
// writer to collide in. The honest answer there is "I locked nothing, and here
// is why" rather than an invented lock on a directory nothing else uses.
//
// The two `%s` operands are the face's progName and the checkpoint's directory:
// naming the directory is what lets an operator judge the claim instead of
// taking it on trust.
const resumeUnlockedDirNotice = "%s: run lock: NOT taken — %q is not a run directory of this profile that can be named (an unverifiable sessions root, or a copied/exported raw log). Continuing without a run-directory lock; see run_lock_resume_seam.go for why this shape has no directory to guard."

// resumeCheckpointDirUnderSessionsRoot reports the checkpoint's run directory —
// its OWN directory — but only when that directory is a DIRECT ENTRY of this
// profile's sessions root.
//
// The comparison is the SAME one resumeCheckpointNamesARunDir uses
// (cli_resume_record_gap.go): the resume family answers "is this a run
// directory of this profile" in one place, and this file must not answer it a
// second time with a slightly different test. The posture is that predicate's
// too — an absent or unreadable root means NOTHING can be claimed, and an
// unverifiable address is never the one to refuse.
func resumeCheckpointDirUnderSessionsRoot(rawLogPath string) (string, bool) {
	root, err := config.SessionsRoot()
	if err != nil || root == "" {
		return "", false
	}
	abs, aerr := filepath.Abs(filepath.Dir(rawLogPath))
	if aerr != nil {
		return "", false
	}
	rootAbs, rerr := filepath.Abs(root)
	if rerr != nil {
		return "", false
	}
	if filepath.Dir(abs) != rootAbs {
		return "", false
	}
	return abs, true
}

// resumeTakeRunLock takes the run-scope kernel lock on the run directory whose
// session the resume at rawLogPath is about to CONTINUE, and returns the
// release the caller must defer — plus whether the lock was TAKEN AT ALL.
//
// THE SECOND RETURN IS THE LOAD-BEARING ONE FOR THE HOLDER GATE (epic
// resume-gate-is-the-lock, 2026-11-05). The caller's question is "may I
// override a LIVE holder stamp?", and the honest answer depends on whether the
// kernel actually arbitrated this directory: a HELD lock is the kernel saying
// "nobody is executing this run directory", so a live pid named by the ledger
// is alive but not running it (the 5h18m zero-progress shape). When NO lock
// could be taken (the notice path below — a checkpoint outside the sessions
// root), the kernel said nothing, so nothing may be overridden: the caller
// keeps the refusal. A seam that answered only "released / error" would leave
// the caller unable to tell those two states apart, and the safe answer and the
// unsafe one would look identical (Rule 15).
//
// A REFUSAL IS FATAL for the resume and names both facts an operator needs: the
// lock path (so it can be stat'd) and the holder stamp (pid + start ticks, so
// the process can be found). That text comes from runlock.HeldError through
// taskRunTakeLockDir — the body the run funnel uses — and this function adds
// only the face's own progName and the verb, so the two faces cannot describe
// the same collision in two different ways.
//
// The release is never nil on a nil error, so `defer rel()` is safe on every
// path the caller returns from afterwards — including the ordinary error paths
// (no checkpoint, a refusal from the gates).
func resumeTakeRunLock(progName, rawLogPath string) (func(), bool, error) {
	noop := func() {}
	dir, ok := resumeCheckpointDirUnderSessionsRoot(rawLogPath)
	if !ok {
		// NOT LOCKABLE, AND SAID SO. Which case this is, and why neither is a
		// refusal, is on the notice's own comment. The `false` is what tells the
		// caller nothing arbitrated this directory, so a LIVE holder stamp keeps
		// its refusal (see this function's own comment).
		fmt.Fprintf(os.Stderr, resumeUnlockedDirNotice+"\n", progName, filepath.Dir(rawLogPath))
		return noop, false, nil
	}
	rel, err := taskRunTakeLockDir(dir, progName, func(inner error) error {
		return fmt.Errorf("%s: refusing to resume %s: %w", progName, filepath.Base(rawLogPath), inner)
	})
	if err != nil {
		return noop, false, err
	}
	return rel, true, nil
}
