package main

// task_session_cwd.go — the TASK SESSION WORKING DIRECTORY seam: the
// processes that OWN a task (the `gopi task run` wrapper and the worker's
// round) move their working directory INTO the task's own session directory
// as soon as the task id exists, and hand their identity to their own
// descendants through the environment.
//
// THE PRINCIPLE (epic 2026-09-12, operator ruling): a task's logs, its raw
// JSONL, its report, its attachments and the stderr of the gopi process
// serving it all belong to ONE directory — <gopi-dir>/sessions/<task-id>/ —
// so a misbehaving or cancelled task is debuggable and cleanable as a UNIT,
// and so nothing a task produces can be mistaken for another task's output.
// The corollary the epic spells out: the process serving the task should
// STAND in that directory, which is what makes a relative path (a tool
// output, a generated artifact, a stray `tee out.txt`) land in the task's
// own assets instead of wherever a launcher happened to start.
//
// WHERE THE ID COMES FROM, AND WHY THIS IS A ONCE-PER-PROCESS MOVE:
//
//	worker round  → the id exists at CLAIM time (workerRoundClaiming ②)
//	task run      → the id exists at CLAIM time (TaskClaimExplicit /
//	                       TaskNextFiltered / the worker's claimed row)
//
// Both stand in the SAME place as the run they own, and both are going to
// DIE with it: every exit path out of runTaskRunOnce ends the process
// (os.Exit after the terminal writeback). A permanent chdir is therefore
// correct for them — and it is deliberately NOT done by the executor CHILD,
// which runs the agent loop and must keep the operator's working context:
//
//   - the loop loads the project's GOPI.md from cwd (internal/agent NewAgent;
//     it now also walks UP from cwd to the enclosing project's boundary, see
//     internal/agent/projectctx.go — starting the child in the project's own
//     directory is still what makes the nearest file the project's own);
//   - code_search/keyword_search index "." (cwd);
//   - task_submit stamps deliverable.repo from cwd (the child submitting a
//     sibling task must stamp the PROJECT, not a log directory);
//   - bash's dangerous-command whitelist resolves relative targets against
//     cwd — relative `rm -rf build` inside a session dir means the session
//     dir, which is NOT the tree the guard is protecting.
//
// So the child INHERITS the start workdir: os/exec documentation and
// implementation both make an empty Cmd.Dir inherit the calling process's cwd
// at Start time, and the spawn site passes the workdir EXPLICITLY as well
// (TaskWorkdirEnv + cmd.Dir), so the contract survives either mechanism
// changing and is visible to every later reader.
//
// CONSEQUENCE, ADMITTED IN THE REPORT: inside a task container the WRAPPER's
// own git shell-outs (`git branch --no-merged`, `git rev-parse` at the
// writeback push leg) now run against whatever repo encloses the session
// directory (~/.gopi) rather than the executor's project. The child remains
// the git authority (it creates the worktree, commits, and is exported
// GOPI_TASK_WORKDIR for exactly that); a descendant that CHANGES cwd attaches
// itself deliberately (os/exec Cmd.Dir), which is the documented way to run
// in another directory.

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/daemoncwd"
	"github.com/nickhuang99/gopi/internal/tools"
)

// taskRunWorkdir is the directory the CURRENT task-serving process started
// in, captured once by taskWorkdirCapture and handed to the executor child
// (TaskWorkdirEnv + Cmd.Dir). taskWorkdirOverride, when set, is a directory
// this process DELIBERATELY moved to and means to stay in (the resume path
// chdirs into the row's worktree): descendants inherit THAT, never the
// original launch cwd, and the round-end restore returns here rather than
// undoing the move. Single-threaded per process — the same package-level
// latch reasoning as workerWriteback.
var (
	taskRunWorkdir      string
	taskWorkdirOverride string
	// taskWorkdirRoundOrigin is WHERE THE CURRENT ROUND ENTERED FROM — the
	// directory taskWorkdirRestore returns to. It is deliberately NOT the
	// process-wide taskRunWorkdir latch: that latch is captured ONCE (it is
	// the executor's handoff, and the first capture is the only truthful one),
	// whereas the restore target must be PER ROUND. A long-lived process
	// serves many rounds, and a stale restore target is a live hazard — the
	// measured failure: a process that restores into a directory captured by
	// an EARLIER round, in a test binary an earlier TEST's t.TempDir(), ends
	// up standing in a deleted directory and every later operation in that
	// process fails with "getwd: no such file or directory".
	taskWorkdirRoundOrigin string
	taskWorkdirRoundActive bool
)

// taskWorkdirLineFmt is the ONE rendering of the round's workdir record — the
// line that proves WHERE the process stands and is therefore also the line the
// operator (and the e2e) reads to know the move happened (Rule 1: the message
// shape lives in the const, never at the call sites). taskWorkdirMovePrefix is
// the same line's leading half, up to (and including) the arrow: a reader
// matching the move for a KNOWN task id — whose start directory it cannot know
// — matches this prefix + the directory, never the whole line.
const (
	taskWorkdirLineFmt    = "gopi task: workdir → %s (started in %s)"
	taskWorkdirMovePrefix = "gopi task: workdir → "
)

// taskWorkdirResetTest clears the process-wide workdir latches. Production
// never needs it (main captures once and the process serves one task), but
// several scenarios in ONE test binary must each start from a clean latch —
// t.Chdir moves the cwd, the latches are process state.
func taskWorkdirResetTest() {
	taskRunWorkdir = ""
	taskWorkdirOverride = ""
	taskWorkdirRoundOrigin = ""
	taskWorkdirRoundActive = false
}

// taskWorkdirCapture records, once per process, the directory this process
// was started in. It must be called while the process is still standing
// there (main's first statement): after the first taskWorkdirEnter the cwd
// belongs to a task's session directory, and a later capture would read back
// the container instead of the origin.
func taskWorkdirCapture() string {
	if taskRunWorkdir == "" {
		if wd, err := os.Getwd(); err == nil {
			taskRunWorkdir = wd
		}
	}
	return taskRunWorkdir
}

// taskWorkdirEnter performs the epic's move: it captures the start workdir,
// then stands the process in taskID's session directory. The returned string
// is the report line for the operator (one line per enter, so a log shows
// where the process went — Rule 15: the move is never silent).
//
// An error means the container could not be entered; the caller REFUSES the
// round (see taskWorkdirChdirRefuse) — running with the output outside the
// task's own directory is the defect, not an acceptable fallback.
func taskWorkdirEnter(taskID string) (string, error) {
	start := taskWorkdirCapture()
	if dir := liveWorkdir(taskWorkdirOverride); dir != "" {
		start = dir
	}
	// THE CLOSE-OUT'S LAUNCH GROUND, published here because this is the ONE
	// place the round's origin is decided and still readable (tools.SelfGroundLaunchDir;
	// the reader is the producer close-out, internal/tools/reclaim_own_ground.go).
	// A producer's own passes free the ground of the rows it closed out, and the
	// directory the run was LAUNCHED in is part of that ground when it is a linked
	// worktree of one of those rows' repos: the worker creates its own ground for
	// the executor child, so no ledger row names the directory the operator
	// launched from, and the measured contract (master f4451b9d) is a
	// `gopi worker <task-id>` started in a task's worktree leaving it registered
	// and on disk.
	//
	// BOTH CANDIDATES, in this order, and neither is privileged: `start` is where
	// THIS process stood, `TaskWorkdirEnv` is where its LAUNCHER stood — measured
	// on the fixture's run, that handoff is inherited from whatever session
	// launched the suite (an agent's own worktree, in this repository), so a
	// handoff that simply won would crown a foreign directory. Each candidate is
	// admitted on its own merits by the reader's conditions (a linked worktree of
	// a closing row's repo), which is where the safety actually lives.
	tools.SetSelfGroundLaunchDir(start, tools.TaskStartWorkdir())
	// Record the round's origin BEFORE moving (an already-active round keeps
	// its original: the worker enters, then runTaskRunOnce enters again for
	// the same row, and the second call must not mistake the container for
	// the origin and strand the process inside it after the restore).
	if !taskWorkdirRoundActive {
		if override := liveWorkdir(taskWorkdirOverride); override != "" {
			taskWorkdirRoundOrigin = override
		} else if wd, err := os.Getwd(); err == nil {
			taskWorkdirRoundOrigin = wd
		}
	}
	dir, err := taskWorkdirChdir(taskID)
	if err != nil {
		return "", err
	}
	taskWorkdirRoundActive = true
	return fmt.Sprintf(taskWorkdirLineFmt, dir, start), nil
}

// taskWorkdirRestore puts a LONG-LIVED process (the worker loop) back where
// the round it owned STARTED. A process that exits with its round does not
// need this; a worker serving many rounds does — otherwise round 2's
// pre-claim output and every later relative write would land in round 1's
// task directory. A restore failure is reported, never fatal (the next round
// chdirs again anyway).
//
// The target is the ROUND's origin, never a process-wide latch (see
// taskWorkdirRoundOrigin): the restore is an undo of THIS round's move, so a
// process that did not move is not touched at all, and a process whose
// earlier round is long gone is not dragged back into that round's directory.
func taskWorkdirRestore() {
	if !taskWorkdirRoundActive {
		return // nothing moved: a process that never entered is never chdir'd
	}
	origin := taskWorkdirRoundOrigin
	taskWorkdirRoundOrigin = ""
	taskWorkdirRoundActive = false
	// The origin must still EXIST before the process moves back into it — a
	// restore into a directory that has since been removed is the worst
	// outcome available here: os.Chdir SUCCEEDS on a deleted directory (the
	// kernel keeps it alive for the process), the process is then standing
	// somewhere that no longer has a name, and EVERY later operation in that
	// process fails with "getwd: no such file or directory" — a long-lived
	// worker's next round, and, in a test binary, every following test. That
	// was the measured failure (a round's origin removed by its owner before
	// the restore). A missing origin is reported and the process stays where
	// it is, which is always a usable directory.
	if origin == "" || liveWorkdir(origin) == "" {
		if origin != "" {
			fmt.Fprintf(os.Stderr, "gopi task: workdir origin %s is gone — staying in the current directory\n", origin)
		}
		return
	}
	if wd, err := os.Getwd(); err == nil && wd == origin {
		return
	}
	if err := os.Chdir(origin); err != nil {
		fmt.Fprintf(os.Stderr, "gopi task: restore workdir %s: %v\n", origin, err)
	}
}

// liveWorkdir returns dir when it NAMES an existing directory, else "" — the
// one predicate behind both "may this be a restore origin?" and "is this the
// workdir a child should start in?". A path that is empty, missing, or a FILE
// is not a working directory, and treating it as one is exactly how a process
// ends up standing somewhere it cannot read back (Rule 15: the caller reports
// the skip, never a silent chdir into nothing).
func liveWorkdir(dir string) string {
	if dir == "" {
		return ""
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// taskWorkdirChdir moves the calling process into taskID's session directory,
// creating it if needed, and returns a report line. It is idempotent: a
// process already standing there is not moved again.
//
// The root is tools.SessionsRoot — the ONE resolution of the sessions tree
// (Rule 2/14), shared with the executor output-log path the spawn seam
// resolves, so the cwd and the output log of a round can never land in
// different trees.
//
// Rule 15 (no silent failure): the returned error is the caller's decision
// point, never a warning to swallow — workerRoundRun REVERTS the claim and does
// not run; the standalone `task run` wrapper exits non-zero. A task whose
// container cannot be entered is a task whose output nobody can find, which
// is the defect this epic removes.
func taskWorkdirChdir(taskID string) (string, error) {
	return tools.ChdirTaskSessionDir(taskID)
}

// taskWorkdirChdirRefuse renders the ONE operator-facing refusal for a task
// container that cannot be entered (Rule 1: the message shape lives here, not
// at the call sites).
func taskWorkdirChdirRefuse(taskID string, err error) string {
	return fmt.Sprintf("gopi task: cannot enter the session directory of %s: %v — refusing to run with its output outside its own task directory", taskID, err)
}

// taskExecutorWorkdir returns the directory the EXECUTOR child must run in:
// the workdir captured before this process moved (in-process latch), else the
// value a launcher EXPORTED (TaskWorkdirEnv — the child re-exported scenario),
// else — for a caller that spawned us without either (an operator's shell, a
// test) — the current working directory, which at spawn time IS the caller's.
//
// This is the child's working CONTEXT handoff: the wrapper must chdir into
// the task's session dir, and the child must NOT inherit that move.
// --- executor-cwd resolution (daemon-context-isolation epic, 2026-09-13) ---

// taskExecutorWorkdir returns the directory the executor child would inherit
// under the PRE-EPIC rule: the workdir THIS process was started in, preceded
// by a deliberate move (taskWorkdirOverride) and by the exported handoff
// (TaskWorkdirEnv) — the handoff wins because the child of a child must see
// the ORIGIN, not the container its parent moved into. It remains the
// fallback input of taskExecutorCwd (a row with no deliverable.repo) and the
// wrapper's own git anchor when no task repo was resolved.
func taskExecutorWorkdir() string {
	if taskRunWorkdir != "" {
		return taskRunWorkdir
	}
	if wd := tools.TaskStartWorkdir(); wd != "" {
		return wd
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// The resolution's constants, its env-hygiene seam and the strip predicate all
// live in task_child_env.go (Rule 1: one place per decision and its message
// shape — the spawn seam in cli_tasks.go reads them from there).

// taskRunRepoDir is the directory THIS run resolved as the executor's
// project (taskExecutorCwd's answer). It is the push leg's repo authority:
// the wrapper itself has chdir'd into the task's session directory, so an
// unanchored `git` there names whatever repo encloses the profile — the
// resolved task repo is the truthful anchor. Single-threaded per process
// (one run per `task run`/worker round), so a package latch is the whole
// state machine.
var taskRunRepoDir string

// taskExecutorCwd resolves the directory the executor CHILD must stand in,
// plus the source that decided it (taskCwdSourceRepo / taskCwdSourceLaunch).
//
// THE EPIC'S MOVE: a resident daemon (worker, merger) was launched from SOME
// directory — the operator's shell, ~/bin, another project — and every task it
// claims used to inherit THAT directory as the child's cwd. The child loads
// the project rules from cwd (internal/agent resolves the project context
// file by walking up from the working directory, so the NEAREST enclosing
// project answers), indexes "." for code_search/keyword_search, and resolves
// relative guard targets against it, so a task belonging to project A ran
// under project B's rules: the wrong rules file, the wrong search root, the
// wrong build contract. The child's cwd is therefore resolved from the TASK
// (its deliverable.repo, or the deliverable.worktree when the executor has
// already created one — tools.TaskRepoFor), never from the spawner.
//
// launchWorkdir is the pre-epic answer (taskExecutorWorkdir). It is returned
// ONLY when the row carries no usable repo — the documented backward-compatible
// fallback for every row submitted without --repo (no deliverable.repo stamp)
// — and the caller reports that fallback by name (Rule 15).
func taskExecutorCwd(taskID, launchWorkdir string) (string, string) {
	if repo := tools.TaskRepoForLaunch(taskID, launchWorkdir); repo != "" {
		if tools.SameDir(launchWorkdir, repo) {
			// The spawner already STANDS in the task's ground — the common
			// shape for a worker started inside the workspace/worktree it
			// serves. Returning it unchanged satisfies both jobs of the cwd
			// at once: the child's project rules/search roots, and the
			// branch its deliverable backfill reads (a linked worktree's
			// branch lives at the worktree, not at the row's repo, whose
			// checked-out branch is exempt).
			return repo, taskCwdSourceTaskGround
		}
		return repo, taskCwdSourceRepo
	}
	return launchWorkdir, taskCwdSourceLaunch
}

// taskCwdSourceFor names WHICH step decided the child's directory (Rule 15): the
// ground when one resolved, else the pre-epic resolution's own source. It is the
// ONE place the two decisions are reconciled, so the record can never name a
// directory and a source that disagree.
func taskCwdSourceFor(ground tools.TaskGroundResolution, resolved string) string {
	if ground.Dir != "" {
		return taskCwdSourceGround
	}
	return resolved
}

// executorChildDir is the directory the executor child is spawned in: the
// wrapper's resolved cwd, ESCAPED to the measured process root when that cwd is
// a FOREIGN linked worktree (epic daemon-cwd-escape).
//
// The resolution gives the child the TASK's project — but when the row carries
// no deliverable repo that answer is the LAUNCH workdir, and a resident daemon's
// launch workdir is wherever the daemon was started. If that is a task's linked
// worktree (a merger spawned from an agent session's own task worktree — the
// live incident), the child would stand in a FOREIGN worktree for the whole
// executor run: a foreign holder inside guard ③'s window, blocking that
// worktree's recycle for nothing. The child's cwd is kept when it is neutral or
// the task's own ground, and escapes to the MEASURED process root otherwise —
// reported by name (Rule 15).
//
// ownGround comes from TaskRepoForLaunch, NOT TaskRepoFor: a spawner that
// already stands in the row's linked worktree makes that worktree the task's own
// ground, and the bare row repo would read it as foreign (measured: the
// `task run` recycle e2e and the executor-cwd walks all launch from inside the
// worktree they serve).
//
// NOTE the shape, because it is the bug this function was extracted for:
// EscapeFromWorktree's first return is meaningful ONLY with ok=true ("keep what
// you passed in" is spelled "" — the package's contract). Assigning it
// unconditionally put every executor child in the session dir instead of the
// row's repo (measured: five cmd/agent e2e walks), so the RESOLVED directory is
// the default and only an actual escape overwrites it.
func executorChildDir(execWorkdir, taskID, launchWorkdir string) string {
	if execWorkdir == "" {
		return execWorkdir
	}
	root, escapedFrom, escaped := daemoncwd.EscapeFromWorktree(execWorkdir, tools.TaskRepoForLaunch(taskID, launchWorkdir))
	if !escaped {
		return execWorkdir
	}
	daemoncwd.Report(taskProgName, root, escapedFrom)
	return root
}
