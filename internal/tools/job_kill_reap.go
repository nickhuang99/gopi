// job_kill_reap.go — a killed job's DETACHED descendants are killed too, and the
// claim "killed" is only made after a read-back proves nobody survived.
//
// THE DEFECT THIS CLOSES (measured). JobStart spawns `bash -c …` with Setsid, so
// the job is a session leader and JobKill can address its whole process group
// with one negative pid. That is exactly one rung, and it stops at the first
// setsid inside the job's own command:
//
//	job start "bash scripts/test-budget.sh …"
//	  └─ bash                     <- the job (session leader, its own group)
//	      └─ setsid bash -c "go test …"   <- test-budget.sh's own launcher
//	          └─ go test …        <- a DIFFERENT session and a different group
//
// The group signal reaches the job and everything that stayed in its group; the
// `go test` tree is in neither, so it kept running inside the run ground — and
// because a killed parent reparents its children to systemd --user, nothing on
// the host was going to collect it. JobKill printed "job %s killed (pid %d)",
// which was true about the pid it named and false about the work.
//
// WHAT OWNERSHIP IS HERE. The job stamps ONE value into its child's environment
// at spawn (jobDescendantMarkerEnv = a per-job nonce): the marker is INHERITED,
// so every descendant carries it across `setsid`, across `setpgid`, and across
// reparenting — the kernel preserves the exec-time environment block while
// getppid() stops saying anything. A process carrying THIS job's nonce is
// provably this job's descendant, which is the precondition Rule 42 puts on a
// signal: the caller created it and holds the record that says so.
//
// NOT a cwd test, NOT a cmdline pattern, NOT `pkill -f`: the epic's positive
// control is a live unrelated process, and every one of those rungs would reach
// it. The marker is unforgeable by accident (it carries the job id and a
// nanosecond stamp) and cannot travel to a stranger.
//
// AND THE READ-BACK IS THE POINT (Rule 15). "The signal was delivered" is not
// "the process is gone" (measured: kill(2) only QUEUES; a target is woken on
// another CPU). So after delivery this file waits a BOUNDED settle and then
// re-measures the marker: survivors are named in JobKill's own result instead of
// being papered over with "killed".
package tools

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"syscall"
	"time"
)

const (
	// jobDescendantMarkerEnv is the environment name JobStart stamps into a
	// job's child. It is the job's OWN lineage record — the one fact that
	// survives setsid, setpgid and reparenting — and it is per-job (see
	// jobKillMarkerFor), so two jobs can never reach each other's processes.
	jobDescendantMarkerEnv = "GOPI_JOB_DESCENDANT"
	// jobKillSettleStep is the interval the read-back sleeps between marker
	// censuses while waiting for the tree to disappear.
	jobKillSettleStep = 25 * time.Millisecond
	// jobKillSettlePolls bounds the read-back (jobKillSettleStep × this = the
	// window). It is a SETTLE, not a timeout a caller waits on beyond the kill's
	// own budget: SIGKILL is uncatchable, so the only thing being waited for is
	// the kernel's bookkeeping, and a survivor that outlives the window is
	// REPORTED rather than waited for (Rule 15).
	jobKillSettlePolls = 20
)

// jobKillMarkerFor mints the per-job nonce stamped into a job's child.
//
// The job id alone is not enough: ids restart at job-1 in every process, so a
// surviving process from an earlier gopi would answer to the same marker. The
// nanosecond stamp makes the value unique to THIS spawn, which is what lets the
// read-back distinguish "my descendant" from "a stranger that happens to carry
// the same job id".
func jobKillMarkerFor(jobID string) string {
	return fmt.Sprintf("%s-%d", jobID, time.Now().UnixNano())
}

// jobMarkerPids returns every live pid whose exec-time environment carries this
// job's marker, sorted (the census iterates a map, and two read-backs of the
// same fact must not print two orders).
//
// An unreadable environment block is skipped, never guessed at: a pid this job
// cannot prove it owns is left alone (Rule 42, and every unknown is a KEEP).
func jobMarkerPids(marker string) (pids []int, measurable bool) {
	if marker == "" {
		return nil, true
	}
	parents, _ := finishCleanupProcCensus()
	if len(parents) == 0 {
		// The table could not be read. That is a MEASUREMENT FAILURE, not "no
		// descendants", and the caller must not print the two the same way
		// (Rule 15).
		return nil, false
	}
	me := OwnPid()
	out := make([]int, 0, 4)
	for pid := range parents {
		if pid <= procSignalInitPid || pid == me {
			continue
		}
		raw, ok := finishCleanupProcEnviron(strconv.Itoa(pid))
		if !ok {
			continue
		}
		if v, found := finishCleanupProcEnvValue(raw, jobDescendantMarkerEnv); found && v == marker {
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out, true
}

// jobKillReapResult is what one marker sweep did, so JobKill can say it in the
// result instead of claiming a clean kill it did not measure.
type jobKillReapResult struct {
	// Signalled are the pids the kernel accepted the kill for.
	Signalled []int
	// Recycled are the pids whose incarnation changed between the census and the
	// signal — the number moved, so nothing was sent (Rule 42).
	Recycled int
	// Survivors are the pids STILL carrying the marker when the settle window
	// expired. Non-empty here is the only case in which JobKill must not say
	// "killed" without also naming them.
	Survivors []int
	// Undelivered names every pid whose signal was neither delivered nor
	// refused-by-incarnation and whose failure was not the expected ESRCH:
	// "nothing was sent" is a fact the caller's sentence carries (Rule 15), and
	// the pid is in neither Signalled nor Survivors because neither is true.
	Undelivered []string
	// Unmeasurable is true when the process table itself could not be read: a
	// different fact from "no descendants", and never reported as the same one.
	Unmeasurable bool
}

// jobKillDescendants kills every process carrying this job's marker and then
// READS BACK for a bounded window, returning what it delivered and what it could
// still see.
//
// The signal is SIGKILL for the same reason the job's group signal is: the job is
// already being abandoned, and a descendant that ignores SIGTERM is exactly what
// a close-out cannot afford to wait out. Delivery goes through the package's ONE
// incarnation-guarded sender (SignalIfSameIncarnation), so a pid recycled
// between the census and the signal is refused by name rather than signalled, and
// the provenance intent is filed before delivery.
func jobKillDescendants(marker string) jobKillReapResult {
	res := jobKillReapResult{}
	if marker == "" {
		return res
	}
	targets, measurable := jobMarkerPids(marker)
	res.Unmeasurable = !measurable
	for _, pid := range targets {
		ticks := ProcessIncarnationOf(pid)
		outcome, sigErr := SignalIfSameIncarnation(pid, ticks, syscall.SIGKILL, nil, syscall.Kill)
		switch {
		case outcome == SignalDelivered:
			// Only a DELIVERED outcome is a kill this job may claim — the
			// per-pid arm's error and its outcome are the same fact, so the
			// error is read here rather than dropped (Rule 15).
			res.Signalled = append(res.Signalled, pid)
		case outcome == SignalSkippedIncarnationChanged && sigErr == nil:
			// A refusal with NOTHING behind it: the incarnation check answered
			// for itself, and "nothing was sent, correctly" is the whole fact.
			res.Recycled++
		case outcome == SignalSkippedIncarnationChanged:
			// A refusal that CAME WITH an error: the number moved, so nothing
			// was addressed (Rule 42 — a refusal is the right answer), but the
			// error is the reason and it is reported rather than asserted away.
			res.Recycled++
			res.Undelivered = append(res.Undelivered,
				fmt.Sprintf("pid %d: %s (%v)", pid, outcome, sigErr))
		case sigErr != nil && !errors.Is(sigErr, syscall.ESRCH):
			// NOT a recycled number and NOT the process exiting between the
			// census and the signal (ESRCH is the outcome this reap WANTS, and
			// reporting it as a failure would bury the real ones). Anything
			// else is a delivery nobody explained, and it goes into the
			// result's own sentence instead of being swallowed: the caller is
			// about to print "killed" and must be able to say why a pid was
			// not part of it.
			res.Undelivered = append(res.Undelivered,
				fmt.Sprintf("pid %d: %s (%v)", pid, outcome, sigErr))
		}
	}
	// THE READ-BACK: a delivered signal is not a death (kill(2) only queues), so
	// the set of survivors is MEASURED — repeatedly, for a bounded window — and
	// never inferred from the absence of an error.
	deadline := time.Now().Add(jobKillSettleStep * jobKillSettlePolls)
	for {
		survivors, ok := jobMarkerPids(marker)
		res.Unmeasurable = res.Unmeasurable || !ok
		res.Survivors = survivors
		if len(survivors) == 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(jobKillSettleStep)
	}
	return res
}

// jobKillReapLine renders one sweep as the sentence JobKill's result carries.
// "" = nothing to say (no descendant ever carried the marker).
func jobKillReapLine(jobID, marker string, res jobKillReapResult) string {
	if res.Unmeasurable && len(res.Survivors) == 0 {
		return fmt.Sprintf("job %s: the process table could not be read, so the read-back proves NOTHING about the job's detached descendants (%s=%s) — do not read this as \"no survivors\"",
			jobID, jobDescendantMarkerEnv, marker)
	}
	if len(res.Survivors) > 0 {
		return fmt.Sprintf("job %s: SURVIVORS REMAIN after SIGKILL — pids %v still carry the job's own marker (%s=%s); the group signal and the per-pid sweep did not stop them",
			jobID, res.Survivors, jobDescendantMarkerEnv, marker) + jobKillUndeliveredClause(res)
	}
	if len(res.Signalled) == 0 && res.Recycled == 0 && len(res.Undelivered) == 0 {
		return ""
	}
	if len(res.Signalled) == 0 && res.Recycled == 0 {
		return fmt.Sprintf("job %s: no detached descendant of this job was signalled — %v (nothing may be claimed dead)",
			jobID, res.Undelivered)
	}
	if len(res.Signalled) == 0 {
		// NOTHING was delivered and at least one pid was refused: rendering
		// "killed 0 detached descendant(s)" would claim a kill of zero, the same
		// shape of false claim the SURVIVORS headline exists to withhold.
		return fmt.Sprintf("job %s: no detached descendant of this job was signalled — %d pid(s) refused because their incarnation changed (the number names another process now — Rule 42)",
			jobID, res.Recycled) + jobKillUndeliveredClause(res)
	}
	line := fmt.Sprintf("job %s: killed %d detached descendant(s) by the job's own marker %s=%s — pids %v, read back and gone",
		jobID, len(res.Signalled), jobDescendantMarkerEnv, marker, res.Signalled)
	if res.Recycled > 0 {
		line += fmt.Sprintf("; %d pid(s) not signalled because their incarnation changed (the number names another process now — Rule 42)", res.Recycled)
	}
	return line + jobKillUndeliveredClause(res)
}

// jobKillUndeliveredClause renders the undelivered pids as a clause that
// APPENDS to whatever the sweep can honestly claim — including the SURVIVORS
// headline, which REPLACES the "killed" sentence and used to drop them
// (measured: Survivors=[9] plus one undelivered pid rendered a sentence naming
// neither the pid nor its error, so a delivery that failed reached the operator
// as nothing at all — the very defect this rule exists for).
// "" = every delivery was either accepted or legitimately refused.
func jobKillUndeliveredClause(res jobKillReapResult) string {
	if len(res.Undelivered) == 0 {
		return ""
	}
	return fmt.Sprintf("; %d pid(s) NOT delivered and not covered by the answers above: %v",
		len(res.Undelivered), res.Undelivered)
}
