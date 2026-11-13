package main

// cli_tasks_exec_stderr_tail.go — WHAT THE FAILED CHILD SAID, kept.
//
// THE GAP THIS CLOSES (epic failed-row-reason-facts, measured 2026-10-26 on the
// live ledger). The failed-reason derivation (internal/tools/tasks.go
// taskFailedReason, epic failed-reason-attribution 2026-10-07) already turns a
// failed write's digest into the row's reason column, so a failed row no longer
// closes with an EMPTY reason or with the fact-free literal "task failed". Re-
// measured on this host: 0 of 186 failed rows from the last 30 days carry an
// empty reason; all 43 empty-reason rows are 30–90 days old, i.e. historical.
//
// What the digest DOES carry today, verified on a real row
// (01a175d6-a055-751d-b083-fff5329cfa16):
//
//	failed: gopi run failed: exit status 1 — task run failed (abnormal child
//	exit) — run: elapsed=20m10s — task-report: /home/nick/.gopi/sessions/… md
//	(5740 bytes, missing sections: ## Files) — gate-claims-unobserved: …
//
// exit status ✓ · elapsed ✓ · report path ✓ · **the child's own stderr ✗**.
//
// The wrapper waits on a child whose stdout AND stderr it has REDIRECTED into
// the run's <session-dir>/output.log (taskChildStreamWriters), so at the
// instant the exit is observed the wrapper holds the child's dying words and
// records none of them: the operator gets "exit status 1" and must go hunting
// in a file whose path they are only told if the worker-writeback branch
// happens to have stamped it. The single most diagnostic artifact of a failed
// run — what the program said as it failed — is dropped on the floor by the
// one process that saw it (Rule 15: the failure is not silent, but it is
// mute).
//
// WHAT THIS FILE DOES. It keeps the LAST bytes the child wrote to stderr in a
// bounded ring (memory-bounded — a run may emit megabytes; the ledger column
// is one line), and renders them into the failure digest. Because the reason
// column is DERIVED from the digest's head (taskFailedReason: first line, drop
// the stats run, cap at 300 runes), the clause is placed immediately after the
// exit status, so the excerpt lands INSIDE the reason a reader sees in
// `task list` — not at the tail where the cap would cut it off.
//
// WHAT IT DOES NOT DO. It never invents, rewrites or summarises: the bytes are
// the child's own, quoted. An empty tail renders NOTHING (no empty-quote
// ornament, no "unknown" — a claim about a stream that said nothing is exactly
// the fact-free reason this epic exists to remove), so a child that failed
// silently keeps a digest without the clause rather than gaining a fabricated
// one.

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
)

const (
	// taskChildStderrTailMaxBytes bounds the CAPTURE — a ring, so a child that
	// writes a megabyte of progress still costs this many bytes of wrapper
	// memory. It is deliberately larger than the quoted excerpt: the ring is
	// also the only place the tail exists, so it should hold enough to be
	// worth quoting at all.
	taskChildStderrTailMaxBytes = 4096

	// taskChildStderrDigestMaxRunes bounds the EXCERPT that rides the digest,
	// and it is sized against taskReasonDigestMaxRunes (300, internal/tools):
	// the reason keeps the digest's head, so this excerpt must leave room for
	// "gopi run failed: exit status N", the abnormal-exit marker and the
	// capture's own label — otherwise the excerpt pushes the exit code out of
	// the reason instead of riding beside it.
	taskChildStderrDigestMaxRunes = 120

	// taskChildStderrDigestFmt renders the clause. %q is not decoration: the
	// tail is program text that may hold quotes, tabs and (after normalisation)
	// no newlines, and %q makes every one of those unambiguous on a single
	// ledger line while marking the extent of the quote. Leading " — " is the
	// separator the run digest already uses between its runs
	// (taskReasonSegmentSeparator's spelling, mirrored here because the
	// agent layer renders the digest and the tools layer only reads it).
	taskChildStderrDigestFmt = " — child stderr: %q"

	// taskChildStderrExcerptEllipsis marks a tail CUT to the excerpt cap, so a
	// reader can tell "the child said exactly this" from "this is the end of
	// what it said" — the distinction taskReasonDigestEllipsis makes one layer
	// up, for the same reason.
	taskChildStderrExcerptEllipsis = "…"
)

// taskExecutorStderrTail is the CURRENT run's child-stderr ring, armed at the
// ONE spawn seam (cli_tasks.go) beside taskExecutorOutputLogPath and read by
// the failure arm after cmd.Wait(). Single-threaded per process — one run per
// `task run` / worker round, the same package-level latch reasoning as
// workerWriteback. nil before the seam runs and in any path that never spawned
// a child, which every reader must treat as "nothing captured" rather than as
// an error (tail() is nil-safe).
var taskExecutorStderrTail *taskChildStderrTail

// taskChildStderrTail is a bounded ring over the child's stderr stream.
//
// CONCURRENCY: os/exec writes a non-*os.File stderr through its own copying
// goroutine, and runTaskRunOnce can observe the exit while that goroutine's
// last chunk is still landing — so the ring is mutex-guarded, and the failure
// arm reads it only AFTER cmd.Wait() returns (Wait drains the copy goroutines
// before returning, which is what makes the tail complete rather than racy).
type taskChildStderrTail struct {
	mu  sync.Mutex
	max int
	buf []byte
}

// newTaskChildStderrTail returns a ring holding at most maxBytes of the tail.
// A non-positive bound yields a ring that accepts and discards: the write must
// still succeed (the child's stream is load-bearing and an io.Writer that
// refuses bytes fails the run), it simply keeps nothing.
func newTaskChildStderrTail(maxBytes int) *taskChildStderrTail {
	return &taskChildStderrTail{max: maxBytes}
}

// Write implements io.Writer. It always reports a full write and never returns
// an error: a diagnostic ring must never be able to break the running child
// (Rule 15's other half — the record is additive, the stream is the primary
// destination).
func (t *taskChildStderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.max <= 0 || len(p) == 0 {
		return len(p), nil
	}
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		// Drop from the FRONT, keeping the most recent bytes: a ring, not a
		// truncation — the child's last lines are the ones that explain an
		// exit, and a head-truncating buffer would keep the banner and drop
		// the error.
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

// tail returns the captured bytes as they stand. Callers read it after the
// child has been waited on.
func (t *taskChildStderrTail) tail() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// taskChildStderrDigestClause renders the failure digest's stderr clause, or
// the empty string when the child wrote nothing worth quoting — the only two
// outcomes (a clause, or no clause; never a placeholder, never an invented
// sentence describing an empty stream).
func taskChildStderrDigestClause(tail *taskChildStderrTail) string {
	excerpt := taskChildStderrExcerpt(tail.tail())
	if excerpt == "" {
		return ""
	}
	return fmt.Sprintf(taskChildStderrDigestFmt, excerpt)
}

// taskChildStderrExcerpt flattens the raw tail into the one-line, capped
// excerpt the digest quotes.
//
// FLATTENING, NOT FILTERING: the ledger's reason is one line (the derivation
// reads only the digest's first line, and `task list` prints one row per
// task), so every run of whitespace — the newlines of a program's output, the
// padding of a progress meter — becomes a single space. Nothing else is
// rewritten: control characters stay in the bytes and are made visible by %q,
// because a reader diagnosing a crash may care that the child emitted a token
// the terminal would swallow.
//
// The cap is applied to the TAIL's end (the last N runes), not its head: the
// last thing a program said before exiting is the diagnosis; the first thing
// is usually a banner.
func taskChildStderrExcerpt(raw string) string {
	flat := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, raw)
	flat = strings.TrimSpace(strings.Join(strings.Fields(flat), " "))
	if flat == "" {
		return ""
	}
	runes := []rune(flat)
	if len(runes) > taskChildStderrDigestMaxRunes {
		flat = taskChildStderrExcerptEllipsis +
			string(runes[len(runes)-taskChildStderrDigestMaxRunes:])
	}
	return flat
}
