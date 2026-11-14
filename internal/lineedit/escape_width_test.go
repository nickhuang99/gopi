package lineedit

// escape_width_test.go — the ONE escape-aware measure/split policy, at the unit
// level.
//
// The end-to-end guard lives in cmd/agent/chat_flow_wrap_test.go (asserted on the
// rendered grid); these pin the policy the grid tests depend on, in the smallest
// shapes that separate the cases: a sequence costs zero cells, is never split,
// and never leaks its own bytes into the text.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// TestDisplayWidth_ControlSequenceIsZeroCells is the measurement the band's
// re-cut got wrong: before the fix, displayWidth counted the introducer's bytes
// as cells, so a row of N cells carrying a 4-byte marker measured N+4 and was
// sized four cells short.
func TestDisplayWidth_ControlSequenceIsZeroCells(t *testing.T) {
	dim := sgr.DimSeq()
	cases := []struct {
		name string
		text string
		want int
	}{
		{"plain ascii", "abcd", 4},
		{"marker wraps plain text", dim + "abcd" + sgr.ResetSeq(), 4},
		{"marker in the middle", "ab" + dim + "cd", 4},
		{"sequence only", dim, 0},
		{"cjk counts double", "日本語", 6},
		{"cjk after a marker", dim + "日本語", 6},
		{"two-byte escape", "a" + string(sgr.Esc) + "Mb", 2},
	}
	for _, c := range cases {
		if got := displayWidth(c.text); got != c.want {
			t.Errorf("displayWidth(%q) = %d cells, want %d", c.text, got, c.want)
		}
	}
}

// TestCutCols_NeverSplitsAControlSequence places the cut column INSIDE a long
// sequence and asserts the sequence rides whole with the head — never halved,
// and never counted against the width. This is the unit shape of the measured
// tear (`\x1b[31m` cut after `\x1b[` printed `31m` as text).
func TestCutCols_NeverSplitsAControlSequence(t *testing.T) {
	seq := sgr.TruecolorFgSeq(18, 52, 86) // a long sequence: `ESC[38;2;18;52;86m`
	if len(seq) <= 4 {
		t.Fatalf("the probe sequence is only %d bytes — too short to place a cut inside it", len(seq))
	}
	// Width 2: the two 'a' cells fill the row exactly, so the cut falls at the
	// sequence's own end — the column that used to land inside it (with the
	// sequence's bytes counted as cells, the cut came at byte 4, i.e. after
	// `ESC[`, printing the parameters as text).
	head, tail := cutCols(strings.Repeat("a", 2)+seq+"TAIL", 2)
	if !strings.HasSuffix(head, seq) {
		t.Fatalf("head = %q, want it to end with the whole sequence %q — the sequence was split", head, seq)
	}
	if strings.Contains(tail, seqParams(seq)) {
		t.Fatalf("tail = %q carries the sequence's own parameters — a cut landed inside it", tail)
	}
	if got := displayWidth(head); got != 2 {
		t.Fatalf("head measures %d cells, want 2 (the sequence costs none)", got)
	}
	if !strings.HasPrefix(tail, "TAIL") {
		t.Fatalf("tail = %q, want it to start with the text that followed the sequence — cells were lost or reordered", tail)
	}
}

// TestWrapCols_SequenceStaysOnItsRow is the same rule for the wrapper: a
// sequence written before the break rides the row it was written on, and the
// wrapped text is the original text reassembled (nothing lost, nothing added).
func TestWrapCols_SequenceStaysOnItsRow(t *testing.T) {
	dim := sgr.DimSeq()
	text := dim + strings.Repeat("x", 7)
	rows := wrapCols(text, 3)
	if len(rows) != 3 {
		t.Fatalf("wrapCols(%q, 3) produced %d rows, want 3: %q", text, len(rows), rows)
	}
	if !strings.HasPrefix(rows[0], dim) {
		t.Fatalf("row 0 = %q, want it to open with the marker %q", rows[0], dim)
	}
	if got := strings.Join(rows, ""); got != text {
		t.Fatalf("the wrapped rows reassemble to %q, want the original %q", got, text)
	}
	for i, r := range rows {
		if w := displayWidth(r); w > 3 {
			t.Fatalf("row %d is %d cells, wider than the width 3: %q", i, w, r)
		}
	}
}

// TestTruncateCols_SequenceCostsNoWidth: a sequence kept whole must not consume
// the cell budget, or the text after it is silently dropped.
func TestTruncateCols_SequenceCostsNoWidth(t *testing.T) {
	dim := sgr.DimSeq()
	got := truncateCols(dim+"abcdef", 3)
	if want := dim + "abc"; got != want {
		t.Fatalf("truncateCols = %q, want %q", got, want)
	}
}

// TestEscapeSeqLen_TruncatedSequenceIsKeptWhole: a sequence the text ends inside
// reports its length to the end, so an atomic caller carries it rather than
// splitting it — the property cutCols' "never split" rule rests on.
func TestEscapeSeqLen_TruncatedSequenceIsKeptWhole(t *testing.T) {
	whole := sgr.TruecolorFgSeq(18, 52, 86)
	for i := 1; i < len(whole); i++ {
		prefix := whole[:i]
		if got := sgr.EscapeSeqLen(prefix); got != i {
			t.Fatalf("EscapeSeqLen(%q) = %d, want %d (the whole remainder)", prefix, got, i)
		}
	}
	if got := sgr.EscapeSeqLen(whole); got != len(whole) {
		t.Fatalf("EscapeSeqLen(complete %q) = %d, want %d", whole, got, len(whole))
	}
	if got := sgr.EscapeSeqLen("plain"); got != 0 {
		t.Fatalf("EscapeSeqLen(%q) = %d, want 0", "plain", got)
	}
}

// seqParams strips the introducer and final byte off a sequence, leaving the
// parameters string a torn cut would print as text.
func seqParams(seq string) string {
	if len(seq) < 3 {
		return seq
	}
	return seq[2 : len(seq)-1]
}

// TestWrapToCols_ZeroWidthIsTheIdentity pins the HISTORIC path's bytes: with no
// reserved layout the seam reads a band width of 0 and hands the row here, so
// this call IS the non-reserved path's only wrap — it must return the row
// unchanged, as ONE element. That is what keep the piped / --no-editor output
// byte-identical (Rule 30: a run's non-reserved output must not move).
func TestWrapToCols_ZeroWidthIsTheIdentity(t *testing.T) {
	row := "hello " + sgr.DimSeq() + "world" + sgr.ResetSeq() + " — 日本語"
	got := WrapToCols(row, 0)
	if len(got) != 1 || got[0] != row {
		t.Fatalf("WrapToCols(%q, 0) = %q, want the row unchanged as ONE element", row, got)
	}
	if neg := WrapToCols(row, -1); len(neg) != 1 || neg[0] != row {
		t.Fatalf("WrapToCols(%q, -1) = %q, want the row unchanged as ONE element", row, neg)
	}
}
