// terminal_close_test.go — the STOP PATH of a terminal driving a real read loop
// (Rule 5: the bug that produced this file ships with the test that catches it).
//
// THE DEFECT, measured 2026-10-02. A terminal was closed while an editor's read
// loop was parked in ReadEvent. Close() closed the stop channel and the two
// notification pipes, and the loop did NOT end: poll(2) reports POLLNVAL for a
// closed descriptor, and a loop that inspects only POLLIN/POLLHUP finds nothing,
// re-polls, and spins there for ever. The observable was a cmd/agent pty case
// whose test PASSED while the package FAILED — the suite's close-out sweep
// reported two parked goroutines, `startWinch.func1` (never joined) and the read
// loop still inside ReadEvent.
//
// THE TWO HALVES THIS FILE PINS, each on its own case so a regression names
// which half broke:
//
//  1. A PARKED READ ENDS. Close() must end a ReadEvent that is ALREADY blocked,
//     not merely install a flag a later read would notice. The case parks a real
//     read on a real pty, closes the terminal from another goroutine, and fails
//     if the read has not returned within the bound.
//  2. THE WATCHER IS JOINED. The SIGWINCH watcher must be gone when Close
//     returns — a stop that only closes the channel leaves it runnable, which is
//     exactly what a close-out sweep samples as work the run never ended.

package lineedit

import (
	"os"
	"testing"
	"time"
)

const (
	// closeStopBound bounds the wait for a parked read to observe the stop. It
	// is a synchronization bound on the wake the stop raises, never a duration
	// the assertion measures: the stop is stated before the descriptors are torn
	// down and the parked poll is woken through the seam it already watches, so
	// the healthy path returns within one scheduling quantum.
	closeStopBound = 2 * time.Second
	// closeStopStep is how often that bound re-asks, and
	// closeWatcherSettle is the same bound for the watcher's own goroutine: the
	// JOIN inside Close is what makes this a formality rather than a race, so a
	// wait here can only turn a real miss into a pass.
	closeStopStep = time.Millisecond
	// closeReadLoopEventCap bounds how many NON-terminal events a looping reader
	// may see before the case calls it a spin. The healthy path needs one (the
	// wake the stop raises) and the pre-fix path produced unbounded POLLNVAL
	// rounds, so the cap is deliberately generous: it separates "ended" from
	// "spinning" without asserting a count.
	closeReadLoopEventCap = 64
	closeWatcherSettle    = 2 * time.Second
)

// closeTestPTY opens a fresh pty pair for these cases, skipping the host that
// cannot provide one (the same shape and the same refusal as the package's other
// pty cases).
func closeTestPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	return openInputPendingPTY(t)
}

// TestTerminalCloseEndsAParkedRead is the case the defect is named for: a read
// blocked in poll(2) on the pty must END when the terminal is closed, rather than
// spinning on the descriptors Close just closed.
//
// The read is parked on a pty with NOTHING written — the shape a chat run's idle
// prompt has — so there is no input event that could end it for the wrong reason.
func TestTerminalCloseEndsAParkedRead(t *testing.T) {
	master, slave := closeTestPTY(t)
	defer func() { _ = master.Close() }() //stylegate:ignore the harness's own pty teardown

	term, err := Open(slave, os.Stderr)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}

	// THE READER'S OWN LOOP, which is the shape the defect lived in: the editor
	// calls ReadEvent again after every event, so "the read returned once" says
	// nothing — a wake from Close IS an event. What must hold is that the loop
	// REACHES the terminal-is-gone result, so this case repeats the read exactly
	// as the editor does and requires it to end there.
	type outcome struct {
		events int
		eof    bool
	}
	done := make(chan outcome, 1)
	go func() {
		buf := make([]byte, readBufBytes)
		events := 0
		for {
			ev, err := term.ReadEvent(buf, pollTimeoutBlock)
			if err != nil {
				done <- outcome{events: events}
				return
			}
			events++
			if ev.EOF {
				done <- outcome{events: events, eof: true}
				return
			}
			if events > closeReadLoopEventCap {
				done <- outcome{events: events}
				return
			}
		}
	}()

	// Let the read really park before the stop: a stop raised before the read
	// begins would be observed at entry and would prove nothing about a PARKED
	// read (the arm this file exists for).
	time.Sleep(closeStopStep) //stylegate:allow-sleep bounded settle so the read enters poll(2) before the stop is raised

	if err := term.Close(); err != nil {
		t.Fatalf("close the terminal: %v", err)
	}

	select {
	case got := <-done:
		if !got.eof {
			t.Fatalf("a reader looping on ReadEvent never reached the terminal-is-gone result within %v of Terminal.Close (%d events, none of them EOF) — the stop path does not reach a blocked read: poll reports POLLNVAL on the descriptors Close just closed, and a loop that inspects only POLLIN/POLLHUP spins there for ever", closeStopBound, got.events)
		}
	case <-time.After(closeStopBound):
		t.Fatalf("a reader looping on ReadEvent did not end within %v of Terminal.Close — it is still parked, or spinning on the closed descriptors (the pre-fix spin: each poll returns POLLNVAL and the loop re-polls)", closeStopBound)
	}
}

// TestTerminalCloseJoinsTheWinchWatcher pins the second half: the SIGWINCH
// watcher must be GONE when Close returns, not merely asked to stop.
//
// The measurement is the watcher's own exit channel, read through the same
// signal the JOIN uses, so this case does not depend on goroutine-count sampling
// (which another test in the process can move at any moment).
func TestTerminalCloseJoinsTheWinchWatcher(t *testing.T) {
	master, slave := closeTestPTY(t)
	defer func() { _ = master.Close() }() //stylegate:ignore the harness's own pty teardown

	term, err := Open(slave, os.Stderr)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	watcherDone := term.winchDone
	if watcherDone == nil {
		t.Fatal("the terminal has no watcher-exit signal, so Close cannot JOIN the watcher it stops")
	}
	if err := term.Close(); err != nil {
		t.Fatalf("close the terminal: %v", err)
	}

	select {
	case <-watcherDone:
	case <-time.After(closeWatcherSettle):
		t.Fatal("the SIGWINCH watcher is still running after Terminal.Close returned — the stop closed the channel but did not JOIN, so a close-out sweep sampling this process reports it as work the run never ended")
	}
}

// TestTerminalCloseEndsAReadThatStartsAfterwards is the entry arm: a read begun
// AFTER Close must not park at all. Without it, a caller that closes a terminal
// and then reads would wait on descriptors that can never deliver — the read
// would end only when the pty peer happened to hang up.
func TestTerminalCloseEndsAReadThatStartsAfterwards(t *testing.T) {
	master, slave := closeTestPTY(t)
	defer func() { _ = master.Close() }() //stylegate:ignore the harness's own pty teardown

	term, err := Open(slave, os.Stderr)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("close the terminal: %v", err)
	}
	// The stop is observable by ASKING (never by assuming).
	if !term.Stopped() {
		t.Fatal("Terminal.Stopped() reports false after Close — the stop path is not stated")
	}

	got := make(chan Event, 1)
	go func() {
		buf := make([]byte, readBufBytes)
		ev, _ := term.ReadEvent(buf, pollTimeoutBlock) //stylegate:ignore reads the terminal event the close case is about; a read error on its own does not fail the verdict
		got <- ev
	}()
	select {
	case ev := <-got:
		if !ev.EOF {
			t.Fatalf("a read on a closed terminal reported %+v, want the terminal-is-gone (EOF) event", ev)
		}
	case <-time.After(closeStopBound):
		t.Fatalf("a read STARTED after Terminal.Close did not return within %v — the stop is not observed at entry", closeStopBound)
	}
}
