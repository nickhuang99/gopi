package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// Job management helpers backing the merged job tool (job family merge;
// A2, competition #4, 2026-08-21): JobStart / JobOutput / JobKill let the
// agent run long-lived commands detached from the bash tool's timeout
// machinery, dispatched by the job tool's action parameter (start | output |
// kill).
//
// Why detached: the bash tool isolates every command in its own process group
// and SIGKILLs the WHOLE group on timeout (bug011 fix — otherwise a background
// daemon inheriting the pipe write-ends deadlocks the caller). A `nohup ... &`
// launched through bash is a group member, so a build that outlives the
// 120s timeout takes the background work down with it — gopi's competition #4
// build was killed exactly this way (+1 retry). JobStart therefore runs the
// command in its OWN SESSION (setsid): it is no longer in any bash tool
// process group, so no bash timeout can reach it.
//
// Discipline (mirrored in the system prompt): start the job, continue other
// work, and collect the output ONCE via action=output when the result is
// needed — never busy-poll, never sleep.

const (
	// jobOutputCap caps the output returned by JobOutput (bytes). Jobs can
	// produce unbounded output (builds, servers); the model receives the tail
	// plus the job's stdout/stderr file paths for the full log.
	jobOutputCap = 100 * 1024
	// jobOutputTailHint is appended when the capped output was truncated.
	jobOutputTailHint = "\n[output truncated at %d bytes — see the stdout file for the full log]"
	// jobKillWait bounds how long JobKill waits for the killed process to be
	// reaped before returning. SIGKILL is immediate; the wait exists so the
	// caller never sees a stale "running" state after a successful kill.
	jobKillWait = 5 * time.Second
)

// JobStatus is the lifecycle state of a background job.
type JobStatus string

const (
	// JobRunning: the process is alive.
	JobRunning JobStatus = "running"
	// JobExited: the process finished on its own (exit code recorded).
	JobExited JobStatus = "exited"
	// JobKilled: the process was terminated by the job kill action (or died
	// by signal).
	JobKilled JobStatus = "killed"
)

// Job holds the state of one background job. Output is streamed to disk so
// the caller never holds the process's pipes (a held pipe write-end would
// deadlock the bash tool — bug011).
type Job struct {
	ID         string
	Command    string
	StartTime  time.Time
	Status     JobStatus
	ExitCode   int // meaningful when Status is JobExited or JobKilled
	PID        int
	StdoutPath string
	StderrPath string
	// Marker is this job's own lineage record, stamped into the child's
	// environment at spawn (jobDescendantMarkerEnv). It is what lets JobKill
	// reach a descendant that left the job's process group (a `setsid` inside
	// the job's own command — scripts/test-budget.sh does exactly that) and
	// prove ownership across reparenting, without a pattern match on a cmdline
	// a stranger could also carry (Rule 42).
	Marker string
	proc   *os.Process
	done   chan struct{} // closed by the reaper when the process is reaped
}

// JobNotice is a completion notification produced by the reaper when a job
// reaches a terminal state on its own (P0-3, dsh onJobDone —
// packages/jobs/tool-jobs/src/index.ts:279-300: the harness injects a user
// notice into the conversation so the model NEVER busy-polls a job).
// Killed jobs produce no notice: the kill action is synchronous and the model
// already knows the outcome.
type JobNotice struct {
	JobID    string
	Command  string
	Status   JobStatus
	ExitCode int
	// External carries an INBOUND message when this notice was not produced by
	// a job at all: `gopi notify --session <id> --message <text>` merges its
	// delivery into this SAME slice (session_message.go), so an external
	// message and a job completion leave through ONE take point
	// (TakeJobNotices) and are injected at the same round boundary. Exactly one
	// of the two shapes is ever populated — a notice with External set has no
	// job behind it, and JobID is empty.
	External *ExternalMessage
}

// jobManager is the process-global job registry (single instance — Rule 2).
type jobManager struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	notices []JobNotice // pending completion notices (drained by the agent loop)
	next    atomic.Int64
}

func newJobManager() *jobManager {
	return &jobManager{jobs: make(map[string]*Job)}
}

// theJobs is the singleton registry backing JobStart/JobOutput/JobKill.
var theJobs = newJobManager()

// JobStart runs command in its own session (setsid) and returns immediately.
// stdout/stderr stream to temp files (paths returned in the result so the
// model can find the full log); stdin is /dev/null so the job can never sit
// on the harness's input. The command is NOT subject to any bash timeout —
// the model ends it with JobKill.
func JobStart(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("empty command")
	}

	stdout, err := scratch.CreateTemp(scratch.RuntimeClassJob, "gopi-job-*.out")
	if err != nil {
		return "", fmt.Errorf("job start: cannot create stdout file: %w", err)
	}
	stderr, err := scratch.CreateTemp(scratch.RuntimeClassJob, "gopi-job-*.err")
	if err != nil {
		stdout.Close() //stylegate:ignore stdio pipe teardown after the child process exited
		os.Remove(stdout.Name())
		return "", fmt.Errorf("job start: cannot create stderr file: %w", err)
	}

	cmd := exec.Command("bash", "-c", scratch.ChildLimitPreamble()+command)
	// The job's own id is minted BEFORE the spawn: the lineage marker below is
	// derived from it, so the id may not be created after the child exists.
	jobID := fmt.Sprintf("job-%d", theJobs.next.Add(1))
	// Wrapper-child identity is stripped (childenv.go): a background job that
	// re-launches gopi must auto-register its OWN row, never write telemetry
	// under the task run's wrapper row (reframe-telemetry-doublecount).
	env, removed := stripWrapperIdentity(os.Environ())
	if len(removed) > 0 {
		taskNotifyLog("job start: stripped inherited wrapper-child identity %v from the child env", removed)
	}
	// The job's OWN lineage record (job_kill_reap.go): one nonce, inherited by
	// every descendant across setsid/setpgid/reparenting. It replaces any value
	// an outer gopi stamped, because a job is not its launcher's descendant for
	// the purposes of a kill — the marker names THIS job and nothing else.
	marker := jobKillMarkerFor(jobID)
	env = append(stripEnvName(env, jobDescendantMarkerEnv), jobDescendantMarkerEnv+"="+marker)
	cmd.Env = env
	// Own session (new session id, own process group): immune to the bash
	// tool's group-kill on timeout. The job is the session leader, so JobKill
	// can signal the whole group (children included) with a negative pid.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil // /dev/null

	if err := cmd.Start(); err != nil {
		stdout.Close() //stylegate:ignore stdio pipe teardown after the child process exited
		stderr.Close() //stylegate:ignore stdio pipe teardown after the child process exited
		os.Remove(stdout.Name())
		os.Remove(stderr.Name())
		return "", fmt.Errorf("job start: command start failed: %w", err)
	}

	job := &Job{
		ID:         jobID,
		Command:    command,
		StartTime:  time.Now(),
		Status:     JobRunning,
		ExitCode:   -1,
		PID:        cmd.Process.Pid,
		StdoutPath: stdout.Name(),
		StderrPath: stderr.Name(),
		Marker:     marker,
		proc:       cmd.Process,
		done:       make(chan struct{}),
	}

	theJobs.mu.Lock()
	theJobs.jobs[job.ID] = job
	theJobs.mu.Unlock()

	// Reaper: wait for the process, record its final state, close the files.
	// Runs under the manager mutex so JobOutput/JobKill never see a torn
	// status transition.
	go func() {
		err := cmd.Wait()
		theJobs.mu.Lock()
		if err == nil {
			job.ExitCode = 0
			job.Status = JobExited
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			job.ExitCode = exitErr.ExitCode()
			// Signal death (e.g. a JobKill) reads as "killed", any other
			// non-zero exit as "exited" with its code.
			if st, ok := exitErr.Sys().(syscall.WaitStatus); ok && st.Signaled() {
				job.Status = JobKilled
			} else {
				job.Status = JobExited
			}
		} else {
			job.ExitCode = -1
			job.Status = JobKilled
		}
		// P0-3: completion notice for naturally-exited jobs only (the kill
		// action is synchronous — the caller already knows the outcome).
		if job.Status == JobExited {
			theJobs.notices = append(theJobs.notices, JobNotice{
				JobID:    job.ID,
				Command:  job.Command,
				Status:   job.Status,
				ExitCode: job.ExitCode,
			})
			// The EDGE of the notice primitive (job_notice_wake.go): the append
			// above raises the LEVEL (the inbox is now non-empty), and this
			// breaks the park of any reader blocked at an idle prompt so it can
			// see that level without the operator typing anything. It carries no
			// payload — the notice itself is read from the level, and the TAKE
			// stays at the turn's round boundary (the one take point).
			NotifyJobNoticeWaiters()
		}
		stdout.Close() //stylegate:ignore stdio pipe teardown after the child process exited
		stderr.Close() //stylegate:ignore stdio pipe teardown after the child process exited
		close(job.done)
		theJobs.mu.Unlock()
	}()

	return fmt.Sprintf("job started: %s\ncommand: %s\nstdout: %s\nstderr: %s",
		job.ID, command, job.StdoutPath, job.StderrPath), nil
}

// JobOutput returns a job's status and (capped) output. Safe to call any
// number of times. The output files are kept after the job exits so the
// result can be fetched once the model actually needs it.
func JobOutput(jobID string) (string, error) {
	job, err := lookupJob(jobID)
	if err != nil {
		return "", err
	}

	theJobs.mu.Lock()
	status := job.Status
	exitCode := job.ExitCode
	theJobs.mu.Unlock()

	out, err := os.ReadFile(job.StdoutPath)
	if err != nil {
		return "", fmt.Errorf("job %s: cannot read stdout: %w", jobID, err)
	}
	errOut, err := os.ReadFile(job.StderrPath)
	if err != nil {
		return "", fmt.Errorf("job %s: cannot read stderr: %w", jobID, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "job %s status=%s", jobID, status)
	if status == JobExited || status == JobKilled {
		fmt.Fprintf(&b, " code=%d", exitCode)
	}
	fmt.Fprintf(&b, " elapsed=%.1fs\n", time.Since(job.StartTime).Seconds())

	body := string(out)
	if len(errOut) > 0 {
		body += "\n[stderr]\n" + string(errOut)
	}
	if len(body) > jobOutputCap {
		body = body[len(body)-jobOutputCap:]
		body += fmt.Sprintf(jobOutputTailHint, jobOutputCap)
	}
	b.WriteString(body)
	return b.String(), nil
}

// stripEnvName returns env with every entry named `name` removed — the "replace
// a marker this process inherited" half of JobStart's spawn (Rule 15: a second
// entry beside the dropped one would make the reading order decide ownership).
func stripEnvName(env []string, name string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key == name {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// JobKill terminates a background job by killing its whole process group
// (the job is a session leader, so a negative pid reaches every child).
// Idempotent: killing an already-finished job reports its final state.
func JobKill(jobID string) (string, error) {
	job, err := lookupJob(jobID)
	if err != nil {
		return "", err
	}

	theJobs.mu.Lock()
	status := job.Status
	theJobs.mu.Unlock()
	if status != JobRunning {
		return fmt.Sprintf("job %s already %s (code=%d) — nothing to kill",
			jobID, status, job.ExitCode), nil
	}

	if job.proc != nil {
		// Negative pid = the process group (Setsid made the job its leader) — and the
		// group id is RE-VERIFIED before delivery, because Setsid made it the leader of
		// a number the kernel may hand to a stranger after the job is reaped
		// (kill_group_guard.go).
		_, _ = signalProcessGroup(job.proc.Pid, job.proc.Pid, syscall.SIGKILL) //stylegate:ignore the job is already being abandoned and its reaper runs on the next lines; ESRCH (already gone) is the expected answer, and the caller never sees a stale "running" state because of that reaper
	}
	// THE GROUP SIGNAL IS ONE RUNG, NOT THE TREE (job_kill_reap.go). A job whose
	// own command runs `setsid …` (scripts/test-budget.sh launches `go test`
	// that way) puts its real work in ANOTHER session and group, where a group
	// signal from here cannot reach it — and the killed parent reparents that
	// work to systemd --user, so nothing on the host ever collects it. The sweep
	// below selects by the marker JobStart stamped into the child (inherited
	// across setsid/reparenting — provable ownership, Rule 42) and then READS
	// BACK, so "killed" is only claimed for what was measured dead.
	reap := jobKillDescendants(job.Marker)
	// Wait for the reaper so the caller never sees a stale "running" state
	// after a successful kill. Best-effort: an unkillable process (D state)
	// still reports "killed" — the signal was sent.
	select {
	case <-job.done:
	case <-time.After(jobKillWait):
	}
	result := fmt.Sprintf("job %s killed (pid %d)", jobID, job.PID)
	switch {
	case len(reap.Survivors) > 0:
		// THE CLAIM IS WITHHELD HERE, deliberately: something this job provably
		// owns is still running, so the honest sentence names it instead of
		// reporting a clean kill (Rule 15 — the defect this epic exists for is
		// exactly a "killed" that no read-back backed).
		result = fmt.Sprintf("job %s: KILLED pid %d, but SURVIVORS REMAIN — pids %v still carry the job's own marker (%s=%s) after SIGKILL; the job's tree is NOT gone",
			jobID, job.PID, reap.Survivors, jobDescendantMarkerEnv, job.Marker) +
			jobKillUndeliveredClause(reap)
	default:
		if line := jobKillReapLine(jobID, job.Marker, reap); line != "" {
			result += "\n" + line
		}
	}
	return result, nil
}

// lookupJob returns the registered job or an error naming the id.
func lookupJob(jobID string) (*Job, error) {
	theJobs.mu.Lock()
	defer theJobs.mu.Unlock()
	job, ok := theJobs.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("unknown job id %q — the job start action returns the id", jobID)
	}
	return job, nil
}

// TakeJobNotices drains the pending job-completion notices. The agent loop
// calls this once per iteration and injects each notice as a user message —
// the model never busy-polls a job (P0-3, dsh onJobDone).
//
// It is the ONE take point for BOTH kinds of notice (a job completion and an
// external message merged into the same queue, session_message.go). Which of
// them still needs an injection is the CALLER's decision, made against the
// conversation it is about to send — `Agent.noticesToInject`.
func TakeJobNotices() []JobNotice {
	theJobs.mu.Lock()
	defer theJobs.mu.Unlock()
	n := theJobs.notices
	theJobs.notices = nil
	return n
}

// ResetJobs clears the registry (test aid — never call while jobs run).
func ResetJobs() {
	theJobs.mu.Lock()
	defer theJobs.mu.Unlock()
	theJobs.jobs = make(map[string]*Job)
	theJobs.notices = nil
}
