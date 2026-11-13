package main

// Regression tests for epic failed-row-reason-facts: the failure writeback must
// record what the CHILD said, not just that it exited non-zero.
//
// They are written as one red-then-green pair against the pre-fix tree: before
// the ring existed, the wrapper's failure digest was
// "gopi run failed: <err> — task run failed (abnormal child exit) — run: …"
// with no child text anywhere in it.

import (
	"strings"
	"testing"
)

// stderrTailFixtureText is the child's own line, verbatim: the excerpt the
// digest must quote has to be findable in the reason a reader sees.
const stderrTailFixtureText = "Error: cannot open project context: permission denied"

// clauseHeadSlack is the rune allowance the digest's fixed head costs beyond the
// excerpt itself ("gopi run failed: exit status 1" and the separators), used by
// the two budget assertions below (Rule 1: a threshold is a named constant).
const clauseHeadSlack = 8

// digestHeadBudget is how much of the composed digest the derived reason keeps:
// the excerpt cap plus the head allowance above.
const digestHeadBudget = taskChildStderrDigestMaxRunes + clauseHeadSlack + 120

// TestStderrTailKeepsTheEndOfTheStream pins the RING property: a child may
// write far more than the bound, and what survives is the END (the diagnosis),
// never the beginning (the banner).
func TestStderrTailKeepsTheEndOfTheStream(t *testing.T) {
	ring := newTaskChildStderrTail(64)
	if _, err := ring.Write([]byte(strings.Repeat("x", 10_000))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ring.Write([]byte(stderrTailFixtureText)); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := ring.tail()
	if len(got) > 64 {
		t.Fatalf("ring kept %d bytes, want at most 64", len(got))
	}
	if !strings.HasSuffix(got, stderrTailFixtureText) {
		t.Fatalf("ring must keep the TAIL of the stream, got %q", got)
	}
}

// TestStderrTailAcceptsWritesAfterTheBound spins the ring past its bound in many
// small writes — the shape os/exec actually produces — and asserts the writer
// never refuses a byte: a diagnostic must not be able to break the child's
// stream (Rule 15: the record is additive).
func TestStderrTailAcceptsWritesAfterTheBound(t *testing.T) {
	ring := newTaskChildStderrTail(32)
	chunk := []byte("0123456789")
	for i := 0; i < 100; i++ {
		n, err := ring.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("write %d: n=%d err=%v — an additive record must never fail the stream", i, n, err)
		}
	}
	if got := ring.tail(); len(got) != 32 {
		t.Fatalf("ring length = %d, want the bound 32", len(got))
	}
}

// TestStderrClauseIsEmptyForASilentChild is the anti-fabrication arm: a child
// that wrote nothing produces NO clause at all — not "child stderr: \"\"", not
// "unknown". The epic's mandate is that a reason carries observable facts; a
// placeholder for an absent fact is the defect it exists to remove.
func TestStderrClauseIsEmptyForASilentChild(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\n\t \r\n"} {
		ring := newTaskChildStderrTail(taskChildStderrTailMaxBytes)
		if _, err := ring.Write([]byte(raw)); err != nil {
			t.Fatalf("write %q: %v", raw, err)
		}
		if got := taskChildStderrDigestClause(ring); got != "" {
			t.Fatalf("whitespace-only tail %q produced clause %q, want empty", raw, got)
		}
	}
	// A nil ring is the "seam never ran" state, and it too is silence.
	if got := taskChildStderrDigestClause(nil); got != "" {
		t.Fatalf("nil ring produced clause %q, want empty", got)
	}
}

// TestStderrClauseQuotesTheChildsLastLine is the digest-shape test: the clause
// is one line (a ledger reason is one line), it quotes the child's own words,
// and it is bounded so the exit status stays inside the reason that
// internal/tools derives from the digest's head.
func TestStderrClauseQuotesTheChildsLastLine(t *testing.T) {
	ring := newTaskChildStderrTail(taskChildStderrTailMaxBytes)
	body := "Error: " + strings.Repeat("boom ", 400) + stderrTailFixtureText
	if _, err := ring.Write([]byte(body)); err != nil {
		t.Fatalf("write: %v", err)
	}
	clause := taskChildStderrDigestClause(ring)
	if clause == "" {
		t.Fatal("a child that printed must yield a clause")
	}
	if strings.ContainsAny(clause, "\n\r") {
		t.Fatalf("clause must be one line, got %q", clause)
	}
	if !strings.Contains(clause, stderrTailFixtureText) {
		t.Fatalf("clause must quote the child's LAST line, got %q", clause)
	}
	if !strings.Contains(clause, taskChildStderrExcerptEllipsis) {
		t.Fatalf("a clipped tail must be marked with %q, got %q", taskChildStderrExcerptEllipsis, clause)
	}
	// The excerpt bound is what keeps the exit code inside the derived reason:
	// head = "gopi run failed: exit status 1" + this clause.
	if n := len([]rune(strings.TrimSpace(clause))); n > taskChildStderrDigestMaxRunes+len(taskChildStderrDigestFmt)+clauseHeadSlack {
		t.Fatalf("clause is %d runes — it would push the exit status out of the derived reason", n)
	}
}

// TestFailureDigestPutsStderrBeforeTheAbnormalChildMarker pins the ORDER the
// failure arm composes (cli_tasks.go): the excerpt rides directly behind the
// exit status, so the reason column — which keeps only the digest's HEAD — still
// contains it. A digest that appended it after the run stats would lose it to
// the 300-rune cap, silently, and the row would read exactly like the pre-fix
// one.
func TestFailureDigestPutsStderrBeforeTheAbnormalChildMarker(t *testing.T) {
	ring := newTaskChildStderrTail(taskChildStderrTailMaxBytes)
	if _, err := ring.Write([]byte(stderrTailFixtureText)); err != nil {
		t.Fatalf("write: %v", err)
	}
	digest := "gopi run failed: exit status 1" + taskChildStderrDigestClause(ring) +
		taskAbnormalChildMarker + " — run: elapsed=1s"
	exitAt := strings.Index(digest, "exit status 1")
	stderrAt := strings.Index(digest, stderrTailFixtureText)
	markerAt := strings.Index(digest, taskAbnormalChildMarker)
	if exitAt < 0 || stderrAt < 0 || markerAt < 0 {
		t.Fatalf("digest lost a required fact: %q", digest)
	}
	if !(exitAt < stderrAt && stderrAt < markerAt) {
		t.Fatalf("digest order must be exit status → child stderr → abnormal-exit marker, got %q", digest)
	}
	if len([]rune(digest)) > digestHeadBudget {
		t.Fatalf("digest head is %d runes — the excerpt must fit inside the derived reason", len([]rune(digest)))
	}
}
