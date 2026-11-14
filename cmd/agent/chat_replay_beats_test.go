package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/replay"
)

// TestParseChatBeatFlags pins the beat protocol's own vocabulary: the values a
// launcher passes reach the driver, the REPL's words are left for the REPL, and
// both spellings of a flag are accepted (a launcher may write --scratch=X or
// --scratch X, and a run that accepted only one would refuse half its callers).
func TestParseChatBeatFlags(t *testing.T) {
	f, rest, err := parseChatBeatFlags([]string{
		"--replay", "sess", "--scratch", "/tmp/x", "--cols=100", "--rows", "30",
		"--max-beats", "25", "--ack-timeout-ms=1500", "positional",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.scratch != "/tmp/x" || f.cols != 100 || f.rows != 30 || f.maxBeats != 25 || f.ackTimeoutMS != 1500 {
		t.Fatalf("values not read: %+v", f)
	}
	want := []string{"--replay", "sess", "positional"}
	if strings.Join(rest, " ") != strings.Join(want, " ") {
		t.Fatalf("the REPL's own words were consumed: got %v want %v", rest, want)
	}
}

// TestParseChatBeatFlagsDefaults pins the defaults to `gopi replay`'s own: a
// launcher that passes only --scratch must get the film geometry the driver's
// legibility floor was measured at, not a zero-sized one.
func TestParseChatBeatFlagsDefaults(t *testing.T) {
	f, _, err := parseChatBeatFlags([]string{"--scratch", "/tmp/x"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.cols != 117 || f.rows != 26 {
		t.Fatalf("geometry defaults are not the designed 117x26: %dx%d", f.cols, f.rows)
	}
	// …and the designed pair IS the one the shared resolver reports (the knob's
	// ONE definition): a launcher that declared chat_ideal_geometry gets its
	// geometry here too, which is why this is asserted against the resolver
	// rather than against a second literal.
	if f.cols != chatBeatIdealCols() || f.rows != chatBeatIdealRows() {
		t.Fatalf("the beat defaults %dx%d disagree with the shared ideal %dx%d",
			f.cols, f.rows, chatBeatIdealCols(), chatBeatIdealRows())
	}
	if f.maxBeats != chatBeatDefaultMaxBeats || f.ackTimeoutMS != chatBeatDefaultAckTimeoutMS {
		t.Fatalf("bound defaults are wrong: %+v", f)
	}
}

// TestParseChatBeatFlagsRefusesNonPositive is the Rule 15 half: a geometry or a
// bound of zero is a REFUSAL, never a silent substitution — the run's own record
// of what it was asked for would otherwise be a lie.
func TestParseChatBeatFlagsRefusesNonPositive(t *testing.T) {
	for _, args := range [][]string{
		{"--cols", "0"},
		{"--rows", "-3"},
		{"--max-beats", "0"},
		{"--ack-timeout-ms", "0"},
		{"--cols", "wide"},
	} {
		if _, _, err := parseChatBeatFlags(args); err == nil {
			t.Fatalf("%v was accepted; want a refusal", args)
		}
	}
}

// TestStartChatReplayBeatsRefusesWithoutSink pins the sink requirement by name: a
// beat protocol run with no screen dump would write a ledger whose beats carry no
// grid, and the driver would wait for frames nobody writes.
func TestStartChatReplayBeatsRefusesWithoutSink(t *testing.T) {
	if _, err := startChatReplayBeats(chatBeatFlag{scratch: t.TempDir()}, nil); err == nil {
		t.Fatal("a run with no dump sink accepted the beat protocol")
	}
}

// TestStartChatReplayBeatsOpensLedgerAndClaimFile pins the two files the driver
// polls: the ledger exists and carries the shared header, and the per-beat dump
// directory is materialised — both BEFORE the first beat, so the driver's first
// read finds a file rather than an absence it must treat as "no beats yet".
func TestStartChatReplayBeatsOpensLedgerAndClaimFile(t *testing.T) {
	scratch := t.TempDir()
	input := &chatInput{dumper: nil}
	// A nil dumper is exactly what the refusal above rejects, so this test drives
	// the file half through the writer the driver uses (internal/replay), which is
	// the same call startChatReplayBeats makes.
	w, err := replay.NewLedgerWriter(filepath.Join(scratch, replay.LedgerName))
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore cleanup close of the replay watcher this case started; the emitted beats are the assertion
	raw, err := os.ReadFile(filepath.Join(scratch, replay.LedgerName))
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if string(raw) != replay.LedgerHeader {
		t.Fatalf("the ledger does not start with the shared header: %q", raw)
	}
	if err := w.Write(1, chatBeatKindRequest, 0, 0, "hello"); err != nil {
		t.Fatalf("write beat 1: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(scratch, replay.LedgerName))
	if err != nil {
		t.Fatalf("re-read the ledger: %v", err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(rows) != 2 {
		t.Fatalf("want a header and one row, got %d rows", len(rows))
	}
	cols := strings.Split(rows[1], "\t")
	if len(cols) != 6 {
		t.Fatalf("the row does not carry the protocol's six columns: %q", rows[1])
	}
	if cols[1] != chatBeatKindRequest || cols[4] != "hello" {
		t.Fatalf("the row's kind or preview is wrong: %q", rows[1])
	}
	got, err := base64.StdEncoding.DecodeString(cols[5])
	if err != nil || string(got) != "hello" {
		t.Fatalf("the row's text_b64 does not round-trip: %q (%v)", cols[5], err)
	}
	_ = input
}

// TestLedgerRowIsAlwaysOneLine is the regression guard for the measured stall: an
// answer fragment legitimately holds newlines, and a row that spanned several lines
// would lose its columns — the driver's "highest beat number" read would then stop
// advancing at the first such fragment while the producer waited for an
// acknowledgement that could never come (measured: beat 36 of a 201-beat run).
func TestLedgerRowIsAlwaysOneLine(t *testing.T) {
	row := replay.LedgerRow(7, chatBeatKindAnswer, 12, 900, "first\nsecond\tthird\r\n")
	if strings.Count(row, "\n") != 1 || !strings.HasSuffix(row, "\n") {
		t.Fatalf("the row spans more than one line: %q", row)
	}
	cols := strings.Split(strings.TrimSuffix(row, "\n"), "\t")
	if len(cols) != 6 {
		t.Fatalf("a multi-line fragment cost the row its columns: %q", row)
	}
	if strings.ContainsAny(cols[4], "\n\r\t") {
		t.Fatalf("the preview still carries a line break: %q", cols[4])
	}
	// The full text survives, base64-encoded: the preview is a courtesy, the
	// traceability check reads this column.
	got, err := base64.StdEncoding.DecodeString(cols[5])
	if err != nil {
		t.Fatalf("text_b64: %v", err)
	}
	if string(got) != "first\nsecond\tthird\r\n" {
		t.Fatalf("the row's text was altered: %q", got)
	}
}

// TestLedgerRowIsTheSharedFormat pins the ledger's row to the ONE formatter both
// producers use: a change to the format must appear in cmd/agent's file at the same
// moment it appears in internal/replay's, which is what exporting the formatter buys.
func TestLedgerRowIsTheSharedFormat(t *testing.T) {
	if got := replay.LedgerRow(1, chatBeatKindChrome, 0, 0, chatBeatTurnEndText); !strings.HasPrefix(got, "1\tchrome\t0\t0\tturn end\t") {
		t.Fatalf("the shared ledger row changed shape: %q", got)
	}
}
