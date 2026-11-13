package testutil

// Bounded fixture subprocesses: the ONE way a suite runs a `gopi` child whose
// output it captures into a buffer.
//
// ROOT CAUSE THIS FILE EXISTS FOR (measured 2026-09-26): a fixture child ran
// with `cmd.CombinedOutput()` and NO deadline. The child was a fresh-DB
// `gopi memory_store` — SQLite first open + schema init + WAL close, whose
// fsyncs go to whatever filesystem the test's `t.TempDir()` sits on. On this
// host that was the scratch home's block device (ext4, the same device two
// live `gopi worker` daemons and a `gopi merge --every 5m` were writing), and
// the child's D-state thread was caught in `jbd2_log_wait_commit` — the ext4
// journal commit wait — i.e. an fsync that had not returned. The parent
// goroutine then sat in `os/exec.Waitid` at the fixture's own call site, and
// the suite's timeout dump pointed at the fixture, not at the sync loop it
// looked like it was testing. Nothing in the suite could end that wait: with
// no deadline there is no arm that reaps a child that never returns.
//
// So the budget is not a tuning knob, it is the missing arm:
//
//	fixtureCommandBudget bounds ONE fixture child, end to end;
//	fixtureCommandWaitDelay bounds how long the os/exec layer keeps waiting for
//	the child's OWN children to close the inherited output pipe after the
//	child is gone (a grandchild holding a copied pipe descriptor must not
//	block the parent either).
//
// A child killed at the budget is NOT a silent pass: the returned error names
// the budget and carries the output captured so far, and every call site in
// this tree turns it into a test failure (the caller's `t.Fatalf` with %v/%s).
// Rule 15: the timeout is reported, never swallowed, and never converted into
// a "the fixture simply worked" verdict.
//
// Rule 27 RESPECT: this file only BOUNDS a wait. It generates no load, and it
// raises no budget to make a loaded host look idle.

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// fixtureCommandBudget is the wall-clock ceiling for ONE fixture child of a
	// suite. It is far above any healthy child (a `gopi memory_store` measures
	// 16-24 ms on a memory filesystem and sub-second on the device, even while
	// the scratch device is loaded) and far below the suite's own budget, so it
	// converts the lock-up into a failure that names the child instead of a
	// timeout dump that names the parent.
	fixtureCommandBudget = 90 * time.Second
	// fixtureCommandWaitDelay is how long the os/exec layer keeps waiting for
	// the child's descendants to close the inherited output pipe after the
	// child itself has exited or been killed (exec.Cmd.WaitDelay).
	fixtureCommandWaitDelay = 5 * time.Second
)

// FixtureCommand runs a fixture subprocess to completion, capturing stdout and
// stderr into one buffer, under the one budget (zero dependency: os/exec and
// context, nothing else).
//
// The returned error is the child's own exit error, or a budget error naming
// the budget and the kill. Callers must FAIL on a non-nil error; a caller that
// wants the output regardless (a probe) passes it through its own assertion.
func FixtureCommand(bin string, args ...string) ([]byte, error) {
	return runFixtureCommand(bin, nil, args, fixtureCommandBudget)
}

// FixtureCommandEnv is FixtureCommand for a fixture child that needs a specific
// environment (a sync leg's peer map, a simulated node identity). A nil env
// inherits this process's own, exactly like exec.Cmd.
func FixtureCommandEnv(bin string, env []string, args ...string) ([]byte, error) {
	return runFixtureCommand(bin, env, args, fixtureCommandBudget)
}

// runFixtureCommand is the budget-explicit form: FixtureCommand passes the one
// constant, and the regression test passes a small budget so the kill arm is
// provable without spending the real one (Rule 1/2: the value has one home,
// the seam is what makes the red proof affordable).
func runFixtureCommand(bin string, env []string, args []string, budget time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.WaitDelay = fixtureCommandWaitDelay
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := buf.Bytes()
	if ctx.Err() != nil {
		return out, fmt.Errorf("fixture command timed out: %s %s exceeded its %s budget and was killed "+
			"(a fresh-DB child on a loaded, device-backed scratch root is the known shape — see "+
			"internal/testutil/runfixture.go): %w", bin, strings.Join(args, " "), budget, err)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
	}
	return out, nil
}
