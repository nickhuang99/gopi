package tools

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestBoundedTailWriterKeepsTheTailAndTheBound pins the two properties the
// writer exists for, on the SMALLEST sample that can show them (Rule 10.4):
// the retained window never exceeds its limit, and what it retains is the TAIL
// of the stream — the bytes buildOutput actually keeps.
func TestBoundedTailWriterKeepsTheTailAndTheBound(t *testing.T) {
	const limit = 64
	w := newBoundedTailWriter(limit)

	// Write in chunks that do NOT divide the limit, so the shift path (not the
	// replace-outright path) is the one exercised.
	var want []byte
	for i := 0; i < 200; i++ {
		chunk := []byte(strings.Repeat(string(rune('a'+i%26)), 7))
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
		want = append(want, chunk...)
	}

	if got := len(w.Bytes()); got > limit {
		t.Fatalf("retained %d bytes, want at most the limit %d — the bound is the whole point", got, limit)
	}
	if got, wantTail := w.String(), string(want[len(want)-limit:]); got != wantTail {
		t.Fatalf("retained window is not the stream's tail:\n got %q\nwant %q", got, wantTail)
	}
	if !w.Truncated() {
		t.Fatal("dropped bytes must be observable (Rule 15) — Truncated() said nothing was lost")
	}

	// A single chunk LARGER than the window takes the other path and must obey
	// the same bound: this is the shape an unbounded pipe delivers.
	big := newBoundedTailWriter(limit)
	huge := []byte(strings.Repeat("z", limit*10))
	if _, err := big.Write(huge); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := len(big.Bytes()); got > limit {
		t.Fatalf("one oversized chunk retained %d bytes, want at most %d", got, limit)
	}
	if got, want := big.String(), string(huge[len(huge)-limit:]); got != want {
		t.Fatalf("oversized chunk did not retain the tail:\n got %q\nwant %q", got, want)
	}

	// A limit of zero must never yield a writer that silently returns nothing.
	zero := newBoundedTailWriter(0)
	if _, err := zero.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if zero.String() != "x" {
		t.Fatalf("limit<=0 retained %q, want the last byte — an empty result would read as 'the command printed nothing'", zero.String())
	}
}

// bashUnboundedOutputHeapCeilingBytes is the ceiling this guard puts on the
// heap growth of an unbounded-output bash call. MEASURED basis (2026-10-13,
// this host): with the unbounded bytes.Buffer the same call grew the heap by
// GIGABYTES inside its 2 s budget (the aborting run reported a 2 GiB block
// requested while 2.0 GiB was already in use, one growslice after another),
// while the bounded window holds at most 2 × pipeRetainedTailSize (~208 KiB).
// 64 MiB is three orders of magnitude above the bounded value and two orders
// below the unbounded one, so it cannot fire on the fixed code and cannot miss
// the defect.
const bashUnboundedOutputHeapCeilingBytes = 64 << 20

// TestBashUnboundedOutputIsBoundedNotFatal is the REGRESSION GUARD for the
// package-level abort (task 01a1315950dd7c51b5a546b78494cf88).
//
// The defect it pins: the two read goroutines in bashInDirCtx accumulated a
// command's whole output in an unbounded bytes.Buffer, so a command that emits
// faster than the run can stop it grew the heap until the runtime aborted the
// test BINARY —
//
//	runtime: out of memory: cannot allocate 2147483648-byte block (…)
//	bytes.(*Buffer).Write  internal/tools/bash.go:602
//
// — which carries no failing test with it and therefore reaches the budget
// harness as "failing tests: 0 / PACKAGE FAILED". `yes` is the smallest command
// that produces the shape (infinite output, continuous, never idle) and the
// suite already runs it (TestBashIdleTimeout_InfiniteOutputCommand) — but that
// test asserts only the ATTRIBUTION of the kill, so the memory behaviour it
// provoked was never asserted anywhere. This test asserts the behaviour.
//
// It measures the package's own heap around the call rather than the process's
// RSS: RSS is shared with every concurrent test in the binary and would make
// the guard flaky, while runtime.MemStats is this binary's own accounting.
func TestBashUnboundedOutputIsBoundedNotFatal(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	// `yes` with a 2s absolute budget and a generous idle net: the idle net must
	// NOT be the cause of death (output flows continuously), so the absolute
	// budget is what ends it — the same shape the suite already exercises.
	output, err := bash("yes", 2*time.Second, 10*time.Second)
	if err == nil {
		t.Fatal("expected the absolute budget to end `yes`, got nil error")
	}
	if !strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("expected the absolute-budget wording, got: %v", err)
	}

	runtime.ReadMemStats(&after)
	// after.TotalAlloc - before.TotalAlloc counts every byte ALLOCATED (freed or
	// not), which is exactly the quantity the defect drove: with the unbounded
	// buffer it is the command's whole output. HeapAlloc would miss the churn.
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > bashUnboundedOutputHeapCeilingBytes {
		t.Fatalf("`yes` allocated %d bytes of output retention in 2s, above the %d-byte ceiling — "+
			"the stream accumulators are unbounded again, and an unbounded one aborts the whole test binary",
			allocated, bashUnboundedOutputHeapCeilingBytes)
	}

	// The result must still be the TAIL of what the command printed — the
	// bounded window must not turn a real output into a truncated-looking one,
	// and the exit= line must survive.
	if !strings.Contains(output, "exit=-1") {
		t.Fatalf("expected the exit=-1 line in the result, got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
}
