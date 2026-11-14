// worker_round_retry_orphan.go — the retry ladder's detached resume is waited
// on by the round that spawned it.
//
// THE MEASUREMENT THIS FILE ANSWERS (task 01a15bd1, measured 2026-10-21). A
// foreground `gopi worker <row>` ran one round; the round's executor child
// exited 1; the worker's own account of the round was complete —
//
//	worker: run 01a15bb9-b231-747b-ae57-7737658dc9e2-r1 failed (continuing):
//	        gopi task run: task … FAILED (child: exit status 1)
//
// — and 8m27s later a process still stood in that row's worktree:
//
//	PID     PPID  PGID    SID     ELAPSED STAT NLWP
//	3380552 54349 3380552 3380552 08:27   Ssl  37
//	→ ppid = 54349 = /lib/systemd/systemd --user, sid == pgid == pid, cgroup =
//	  …/vte-spawn-….scope (Rule 24: still in the login session's scope, i.e.
//	  setsid'd but NOT detached)
//
// The two readings agree once the process is NAMED: it was launched as
// `gopi --session 01a15bb9-… --task 01a15bb9-…`, and the worker's log carries
// the line that started it one second before the round's failure line
// (`task 01a15bb9-…: abnormal child exit (attempt 1 of 3) — resuming the same
// session: … (pid 3380552, …)`). The survivor was therefore the RETRY LADDER's
// resume: taskRunAbnormalChildRetry → tools.SpawnTaskResume, which launches the
// recovery run with SysProcAttr{Setsid: true} on purpose and never waits for it
// (internal/tools/task_holder_resume.go: "the merger does not wait for it —
// that is the point of the whole epic").
//
// ROOT CAUSE (① vs ② in the epic's own vocabulary). This is NOT ① — cmd.Wait()
// waits on the right process: the executor child it waits on is the direct
// child it spawned (cli_tasks.go, cmd := exec.Command(gopiExecutable()) with
// Setpgid, then runErr := cmd.Wait()), and its exit status 1 is exactly what
// the wrapper reports. It is ② and a third shape the epic's two options did not
// name: the failure path runs the retry arm, the retry arm SPAWNS A NEW ROUND
// with Setsid and returns without waiting for it, and the round therefore ends
// while a process it started is still running. NOTHING ELSE REAPS IT EITHER:
// the close-out reap that used to judge such a process was removed on
// 2026-11-05 (it signalled processes it did not create, and its judgement was
// derived from an inherited environment record), so a spawn that leads its own
// session and carries no lineage record of the run is now nobody's to collect.
// That removal is exactly why the WAIT below matters more than it did: the only
// party that can end that process is the one that started it (the inherited
// environment still names nothing about us: GOPI_TASK_RUN_WRAPPER_PID is stamped
// into the executor child's env, never into this process's own).
//
// WHAT THIS FILE DOES. The round that handed off a resume WAITS for it, bounded
// by a deadline, reporting as it waits:
//
//   - workerRound notes the pid at the one seam that learns it
//     (workerRoundNoteRetryResume, called from taskRunAbnormalChildRetry);
//   - the round's own tail waits for it (workerRoundAwaitRetryResume), so the
//     round does not declare itself finished while its respawn runs — the
//     contradiction the epic reported is closed at its source;
//   - the wait is bounded (workerRoundRetryWaitDefault, env-overridable) and
//     every exit from it — exited, deadline, operator escalation — is REPORTED
//     with which fact was observed (Rule 15: "it finished" and "I stopped
//     waiting" are different statements).
//
// WHAT IS DELIBERATELY UNCHANGED. The resume is still Setsid'd (a worker killed
// outright must still leave the recovery running — that is what makes recovery
// survive its launcher), still the SAME single spawn implementation
// (tools.SpawnTaskResume, Rules 13/14), and still takes the ledger handover. A
// round that WAITS is the opposite of a round that OWNS and kills.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// workerRoundRetryWaitDefault bounds how long a round waits for the resume
	// its ladder handed off. It is a DEADLINE, not a poll: a resume runs a whole
	// agent round (minutes), and cutting one off mid-task would destroy the very
	// work the ladder exists to preserve — so the default is generous, and the
	// wait prints progress instead of looking wedged.
	workerRoundRetryWaitDefault = 60 * time.Minute
	// workerRoundRetryWaitEnv overrides that deadline in SECONDS (Rule 2: the
	// constant is the default and this is the one place the env is read). A
	// value of 0 or less means "no deadline".
	workerRoundRetryWaitEnv = "GOPI_WORKER_ROUND_RETRY_WAIT_SEC"
	// workerRoundRetryPoll is the cadence of the liveness re-read.
	workerRoundRetryPoll = time.Second
	// workerRoundRetryNoteInterval is how often a long wait reports itself.
	workerRoundRetryNoteInterval = 30 * time.Second
)

// The /proc entries this file reads (Rule 1b: the spellings live here).
const (
	procCgroupFileName = "cgroup"
)

// workerRoundRetryWaitBeginNote / Done / Deadline / Progress / Escalated are the
// wait's own operator-facing lines. Each one states WHICH fact it observed, so a
// reader of the log can never confuse a resume that finished with one that was
// abandoned (Rule 15).
const (
	workerRoundRetryWaitBeginNote     = "worker: the round's retry resume (pid %d) is RUNNING — waiting up to %s for it before this round ends (%s=0 waits without a deadline)\n"
	workerRoundRetryWaitDoneNote      = "worker: the round's retry resume (pid %d) exited — the round now ends; nothing it started is left running\n"
	workerRoundRetryWaitDeadlineNote  = "worker: the round's retry resume (pid %d) is STILL RUNNING after %s — this round is leaving it behind (it is a detached recovery run, not a leftover of the round's executor)\n"
	workerRoundRetryWaitProgressNote  = "worker: still waiting for the round's retry resume (pid %d)\n"
	workerRoundRetryWaitEscalatedNote = "worker: stop escalated — the wait for the round's retry resume (pid %d) is abandoned; that run is left to its own close-out\n"
	workerRoundRetryUnmeasurableNote  = "worker: the round's retry resume (pid %d) could not be measured — not waiting on a pid this process cannot read (Rule 24: an unknown is never a fact)\n"
)

// workerRoundRetryResume is the pid of the resume the current round's retry
// ladder handed off (0 = none). It is process-wide because the seam that learns
// the pid (taskRunAbnormalChildRetry, deep inside the wrapper) and the seam that
// waits for it (workerRound's tail) are different functions — the same shape,
// and the same reason, as passChildRegistry.
//
// It is cleared at the start of every round, so a finished round's pid can never
// be waited on again (a recycled number is somebody else's process — the trap
// Rule 24 names).
var workerRoundRetryResume int

// workerRoundNoteRetryResume records the detached resume this round spawned.
// The ladder is the only writer.
func workerRoundNoteRetryResume(pid int) {
	if pid > 0 && pid != os.Getpid() {
		workerRoundRetryResume = pid
	}
}

// workerRoundResetRetryResume forgets any previous round's pid. Called at the
// round's start so one round can never inherit another's.
func workerRoundResetRetryResume() { workerRoundRetryResume = 0 }

// workerRoundRetryResumePID is the reader of the round's handed-off pid (0 when
// this round handed none off).
func workerRoundRetryResumePID() int { return workerRoundRetryResume }

// workerRoundAwaitRetryResume blocks until the round's handed-off resume exits,
// the deadline expires, or the operator escalates. It returns without waiting
// when no round handed one off (the common case).
func workerRoundAwaitRetryResume() {
	pid := workerRoundRetryResume
	if pid <= 0 {
		return
	}
	if !processRunning(pid) {
		return
	}
	fmt.Fprintf(os.Stderr, workerRoundRetryWaitBeginNote, pid,
		workerRoundRetryWaitBudget().String(), workerRoundRetryWaitEnv)
	deadline := time.Now().Add(workerRoundRetryWaitBudget())
	nextNote := time.Now().Add(workerRoundRetryNoteInterval)
	for {
		if !processRunning(pid) {
			fmt.Fprintf(os.Stderr, workerRoundRetryWaitDoneNote, pid)
			return
		}
		if workerStopEscalated() {
			fmt.Fprintf(os.Stderr, workerRoundRetryWaitEscalatedNote, pid)
			return
		}
		if workerRoundRetryWaitBudget() > 0 && !time.Now().Before(deadline) {
			fmt.Fprintf(os.Stderr, workerRoundRetryWaitDeadlineNote, pid, workerRoundRetryWaitBudget().String())
			return
		}
		if !time.Now().Before(nextNote) {
			fmt.Fprintf(os.Stderr, workerRoundRetryWaitProgressNote, pid)
			nextNote = time.Now().Add(workerRoundRetryNoteInterval)
		}
		time.Sleep(workerRoundRetryPoll) //stylegate:allow-sleep bounded settle wait for the handed-off resume
	}
}

// workerRoundRetryWaitBudget resolves the wait deadline (Rule 2: the constant is
// the default, and this ONE function reads the environment).
func workerRoundRetryWaitBudget() time.Duration {
	raw := strings.TrimSpace(os.Getenv(workerRoundRetryWaitEnv))
	if raw == "" {
		return workerRoundRetryWaitDefault
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		return 0 // spelled "no deadline"
	}
	return time.Duration(sec) * time.Second
}

// processRunning asks the kernel whether pid exists and is not a zombie.
//
// It is deliberately NOT `kill(pid, 0)`: a terminated-but-unreaped process
// answers that probe while being no process anybody has left running — the same
// defect that once made a dead wrapper read as a live holder
// (internal/tools/taskzombie.go). The field NUMBERING is not re-derived here:
// procname.StatField owns the count-from-the-last-')' rule.
func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	path := procname.StatStatPath(procRoot, strconv.Itoa(pid))
	state, ok := procname.StatFieldString(path, procname.StatFieldState)
	if !ok {
		return false
	}
	return state != procZombieStateName
}

// processCgroupOf renders pid's cgroup membership — the only detachment
// evidence Rule 24 accepts (never PPID, never `sid == pid`). "" when unreadable.
func processCgroupOf(pid int) string {
	if pid <= 0 {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), procCgroupFileName))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if fields := strings.SplitN(line, ":", 3); len(fields) == 3 {
			return strings.TrimSpace(fields[2])
		}
	}
	return ""
}

// procZombieStateName is /proc/<pid>/stat's state letter for a terminated,
// not-yet-reaped process. internal/tools carries the same letter for its own
// liveness rule; this package cannot import it (it is unexported there), so the
// letter is named HERE rather than typed inline at the comparison (Rule 1b).
const procZombieStateName = "Z"
