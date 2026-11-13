package tools

// The CLOSE-OUT keepalive: a wrapper that is still working on a row it claimed
// must say so, for exactly as long as it is working.
//
// THE DEFECT THIS CLOSES (measured 2026-10-27, row
// 01a17a1b-496f-7179-9567-d544fa5922f9). Every heartbeat write in the codebase
// lives in internal/agent and belongs to the run's ENGINE — the registration
// stamp, the per-iteration touch, the per-tool-batch touch, and
// startRunKeepalive's 2-minute ticker, all bound to the engine's own context.
// The wrapper (`gopi task run`, cmd/agent/cli_tasks.go) is a DIFFERENT process
// and it keeps working after the engine exits: the clone channel's delivery leg
// (clean → pull --rebase → build → push) and finish-cleanup's branch check run
// THERE, on the row the wrapper claimed, while the row is still in_progress.
// Nothing stamps that row during that window, so a liveness predicate over the
// heartbeat alone reads a row with a live wrapper and reasonably concludes
// "zombie" — which is why the read-out resolves the recorded PROCESS IDENTITY
// (pid + start ticks) rather than trusting the frozen stamp
// (taskLivenessInstantSQL, the query-side half of that predicate, is deleted
// 2026-11-05 with the stale sweep).
//
// The measured shape, from that run's own ledger row and log:
//
//	last_toolcall_at = 06:15:26   (the engine's last stamp)
//	ended_at         = 07:01:10   (the wrapper's writeback)
//	span             = 45.7 min   of in_progress work with no heartbeat writer
//
// and the engine's own session JSONL has no record after 06:15:43, so the freeze
// is not "the keepalive ticked late" — the process that would have ticked was
// gone. The scheduler observed 38m52s of it and reported the row as stale.
//
// The keepalive is the smallest honest fix (Rule 35): it adds no process, no
// file, no timer outside gopi — the wrapper already exists and already holds
// the claim. It reuses the ONE write path (TaskHeartbeatConn.touch), so the
// holder guard, the terminal-row no-op and the column's throttle semantics are
// inherited rather than re-implemented (Rule 14).

import (
	"context"
	"fmt"
	"os"
	"time"
)

// TaskRowKeepaliveInterval is how often a wrapper stamps the row it holds while
// it works on it. It is deliberately the same cadence the engine's own
// keepalive uses (internal/agent's runKeepaliveInterval, 2 minutes): both are
// "a live holder saying so", and the stale grace is 30 minutes, so either one
// leaves nine ticks of headroom inside one grace window.
const TaskRowKeepaliveInterval = 2 * time.Minute

// taskRowKeepaliveInterval is the ticker's actual cadence as a REBINDABLE seam:
// it defaults to the named constant above (Rule 1/13 — the constant stays the
// single definition of the production value) and exists only so a test can
// drive a real second tick without waiting two minutes. Production never
// rebinds it — the same pattern the engine's own keepalive uses
// (internal/agent's runKeepaliveTick).
var taskRowKeepaliveInterval = TaskRowKeepaliveInterval

// StartTaskRowKeepalive stamps taskID's last_toolcall_at immediately and then
// once per TaskRowKeepaliveInterval until ctx is cancelled. The returned stop
// function is idempotent and safe to defer; it cancels the goroutine and waits
// for it to finish, so a caller's close-out cannot race a write still in
// flight.
//
// It is best-effort in the same direction as every other heartbeat write: a
// failed stamp is reported on stderr with the row and the reason (Rule 15) and
// never fails the run — a heartbeat is a liveness CLAIM, not a deliverable.
func StartTaskRowKeepalive(ctx context.Context, taskID string) (stop func()) {
	if ctx == nil || taskID == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		stamp := func() {
			if err := stampTaskRowLiveness(ctx, taskID); err != nil {
				fmt.Fprintf(os.Stderr, "[gopi] close-out heartbeat for %s: %v\n", taskID, err)
			}
		}
		stamp()
		ticker := time.NewTicker(taskRowKeepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stamp()
			}
		}
	}()
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
	}
}

// stampTaskRowLiveness performs one close-out stamp and REPORTS why it failed.
//
// It exists because OpenTaskHeartbeatConn collapses its open error into a nil
// handle, which is exactly the "silent failure" shape Rule 15 refuses: a
// keepalive whose ledger never opened would look identical to one that simply
// had nothing to say. The agent-facing handle keeps its signature (nine
// callers, one of them the engine's hot path); this wrapper is where a close-out
// gets the reason.
func stampTaskRowLiveness(ctx context.Context, taskID string) error {
	conn := OpenTaskHeartbeatConn(ctx)
	if conn == nil {
		return fmt.Errorf("the tasks ledger could not be opened, so the row was not stamped")
	}
	defer conn.Close()
	// An empty session id is correct here: the write addresses the row by
	// task_id (touch tries that column first), and the wrapper has no session
	// of its own — the engine's session is the CHILD's.
	return conn.ExecTouchLastToolcall(taskID, "", float64(time.Now().Unix()))
}
