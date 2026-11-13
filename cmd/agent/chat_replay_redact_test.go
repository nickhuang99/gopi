package main

// chat_replay_redact_test.go — the chat route's redaction pass, driven through
// the REAL driver the REPL uses (startChatReplayBeats): the shapes, the mask, the
// log format and the log's name are internal/replay's ONE definition, and what
// this file pins is that the route APPLIES it, at the seam, to the text both
// consumers get.
//
// The pass has no separate unit here on purpose: a test that called a masker
// directly would pass while the route still handed the surface its raw bytes —
// which is exactly the state tools/replay/verify.py's fourth self-check reported
// ("no redaction log at <scratch>/redactions.tsv"). So every case below drives
// the driver.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nick/gopi/internal/replay"
	"github.com/nick/gopi/internal/uiclient"
)

// The identifying runs these cases use, as literals: a test that read them from
// the pass would assert nothing about the pass.
const (
	redactTestUUID  = "4ca8cd29-019c-4dda-a000-6dafd782bf91"
	redactTestHome  = "/home/nick/.gopi/sessions"
	redactTestSplit = 18 // the rune index the stream cases split the uuid at
)

// newRedactDriver builds the run's beat driver over a fresh scratch and arms a
// screen dump for it, so the context under test is the one a filmed run has.
func newRedactDriver(t *testing.T) (*chatReplayBeatDriver, *chatInput, string) {
	t.Helper()
	scratch := t.TempDir()
	dumper, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: filepath.Join(scratch, uiclient.ChatDumpFileName),
		Cols: chatBeatIdealCols(),
		Rows: chatBeatIdealRows(),
		Mode: uiclient.DumpModeANSI,
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}
	input := &chatInput{dumper: dumper}
	f := chatBeatFlag{scratch: scratch, cols: chatBeatIdealCols(), rows: chatBeatIdealRows(), maxBeats: chatBeatDefaultMaxBeats, ackTimeoutMS: chatBeatDefaultAckTimeoutMS}
	d, err := startChatReplayBeats(f, input)
	if err != nil {
		t.Fatalf("start the beat driver: %v", err)
	}
	if d == nil {
		t.Fatal("no driver for a scratch — the pass would not exist on this run at all")
	}
	t.Cleanup(func() { _ = d.close() }) //stylegate:ignore cleanup close of the redaction fixture's own writer; the redacted output was read before
	return d, input, scratch
}

// TestChatReplayMaskTextWithholdsARun pins the complete-text entry point: the
// text the surface is handed is the text the ledger records, and both are masked.
// The typed prompt and the tool-activity row go through this path, which is where
// a path or an id actually lives (a tool row is composed from the call's
// arguments).
func TestChatReplayMaskTextWithholdsARun(t *testing.T) {
	d, _, _ := newRedactDriver(t)
	in := "read " + redactTestHome + "/x and " + redactTestUUID + "\n"
	out := d.maskText(in)
	if strings.Contains(out, redactTestHome) || strings.Contains(out, redactTestUUID) {
		t.Fatalf("the mask let a run through: %q", out)
	}
	if utf8.RuneCountInString(out) != utf8.RuneCountInString(in) {
		t.Fatalf("the substitution is not length-preserving: %d in, %d out", utf8.RuneCountInString(in), utf8.RuneCountInString(out))
	}
	// The ledger's row is cut from the SAME text, so the film and the ledger
	// cannot describe one event differently.
	row := replay.LedgerRow(1, chatBeatKindToolCall, 0, 0, out)
	if strings.Contains(row, redactTestHome) || strings.Contains(row, redactTestUUID) {
		t.Fatalf("the ledger row carries a run the mask withheld: %q", row)
	}
}

// TestChatReplayAnswerStreamMasksAcrossTheSplit is the straddle case, driven
// through the streaming seam: an identifier split across two answer deltas
// matches NO shape in either half, so a pass that judged each delta alone would
// draw the head and let the tail complete it. What the surface draws is what the
// seams released, so the assertion is on the RELEASED text.
func TestChatReplayAnswerStreamMasksAcrossTheSplit(t *testing.T) {
	d, _, _ := newRedactDriver(t)
	first, second := redactTestUUID[:redactTestSplit], redactTestUUID[redactTestSplit:]
	if replay.IdentShapes.MatchString(first) || replay.IdentShapes.MatchString(second) {
		t.Fatalf("the split is not the one this test is about (%q / %q)", first, second)
	}
	var drawn strings.Builder
	for _, chunk := range d.maskAnswerStream("the session is " + first) {
		drawn.WriteString(chunk)
	}
	for _, chunk := range d.maskAnswerStream(second + " and it ended") {
		drawn.WriteString(chunk)
	}
	if err := d.flushAnswerHold(func(chunk string) error {
		drawn.WriteString(chunk)
		return nil
	}); err != nil {
		t.Fatalf("flush the held tail: %v", err)
	}
	got := drawn.String()
	if !strings.Contains(got, "the session is") || !strings.Contains(got, "and it ended") {
		t.Fatalf("the stream lost text: %q", got)
	}
	if strings.Contains(got, redactTestUUID) || replay.IdentShapes.MatchString(got) {
		t.Fatalf("an identifier that straddled two deltas reached the surface: %q", got)
	}
	if utf8.RuneCountInString(got) != utf8.RuneCountInString("the session is "+redactTestUUID+" and it ended") {
		t.Fatalf("the released stream is not the length of what arrived: %q", got)
	}
}

// TestChatReplayRedactionLogIsVerifyFormat is the positive control the film's
// checker reads: <scratch>/redactions.tsv exists after the run, carries the
// shared header and holds one `#n runes matched` row per substitution — the row
// whose second column is what makes the length-preserving claim checkable from
// the file alone.
func TestChatReplayRedactionLogIsVerifyFormat(t *testing.T) {
	d, _, scratch := newRedactDriver(t)
	_ = d.maskText("see " + redactTestUUID)
	_ = d.maskText("see " + redactTestHome)
	if err := d.closeRedaction(); err != nil {
		t.Fatalf("flush the log: %v", err)
	}
	path := filepath.Join(scratch, replay.RedactionLogName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no redaction log at %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("the log is header-only: %q", raw)
	}
	if lines[0] != strings.TrimSuffix(replay.RedactionLogHeader, "\n") {
		t.Fatalf("the log's header is not the shared one: %q", lines[0])
	}
	cols := strings.Split(lines[1], "\t")
	if len(cols) != 3 || cols[0] != "1" || cols[2] != redactTestUUID {
		t.Fatalf("row 1 is not `#n runes matched`: %q", lines[1])
	}
	if cols[1] != "36" {
		t.Fatalf("the row's rune count is not the run's own: %q", lines[1])
	}
	if d.red.Masks() < 2 {
		t.Fatalf("the log holds %d substitution(s); both calls should have been recorded", d.red.Masks())
	}
}

// newIdleDumpSink arms a screen dump inside dir without painting anything: the
// refusal under test is about the REDACTION log's path, and a driver that refused
// earlier for want of a sink would report a different cause.
func newIdleDumpSink(t *testing.T, dir string) *uiclient.ScreenDumper {
	t.Helper()
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: filepath.Join(dir, "screen.dump"),
		Cols: chatBeatIdealCols(),
		Rows: chatBeatIdealRows(),
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}
	return d
}

// TestChatReplayRedactionRefusesAnUnwritableScratch is Rule 15's half on the
// route: a run that cannot record what it withheld must not draw the identifier
// instead. The refusal happens at START time, before a single keystroke is typed
// into the surface.
func TestChatReplayRedactionRefusesAnUnwritableScratch(t *testing.T) {
	// A file where the log must go: os.Create then fails with ENOTDIR.
	scratch := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scratch, replay.RedactionLogName), 0o755); err != nil {
		t.Fatalf("prepare the obstruction: %v", err)
	}
	input := &chatInput{dumper: newIdleDumpSink(t, scratch)}
	_, err := startChatReplayBeats(chatBeatFlag{scratch: scratch, cols: chatBeatIdealCols(), rows: chatBeatIdealRows(), maxBeats: 1, ackTimeoutMS: 1}, input)
	if err == nil {
		t.Fatal("a run that cannot open its redaction log accepted the beat protocol — it would draw the identifier instead")
	}
	if !strings.Contains(err.Error(), replay.RedactionLogName) {
		t.Fatalf("the refusal does not name the log: %v", err)
	}
}

// TestChatReplayLineLayerRowsGoThroughThePass pins the OTHER producer of
// identifying runs: the layer composes some of its own rows at draw time (the
// `chat_surface` arming notice names this run's published identity, the /help
// panel names the session directory), and those land in the dumped grids the
// film's checker greps. The pass is armed on the LAYER, not only on the replay's
// seams, and the arming is a function so the layer stays ignorant of the protocol.
func TestChatReplayLineLayerRowsGoThroughThePass(t *testing.T) {
	_, input, _ := newRedactDriver(t)
	if input.redact == nil {
		t.Fatal("the driver did not arm the line layer: a notice it composes itself would carry a run into the band")
	}
	var sink strings.Builder
	announced := &strings.Builder{}
	input.errOut = &sink
	input.announce = announced
	input.WriteNotice("this run's surface identity is published to its children \u2192 id=" + redactTestUUID + "\n")
	got := sink.String()
	if strings.Contains(got, redactTestUUID) {
		t.Fatalf("WriteNotice drew an identifying run the pass withheld: %q", got)
	}
	if announced.String() != got {
		t.Fatalf("the announcement seam and the drawn row disagree (%q vs %q) — one of them skipped the pass", announced.String(), got)
	}
	block := "session  " + redactTestUUID + "\n"
	input.WriteLines(block)
	if strings.Contains(sink.String(), redactTestUUID) {
		t.Fatalf("WriteLines kept an identifying run the pass withheld: %q", sink.String())
	}
}

// TestChatReplayPassIsAbsentForAPlainRun pins the other direction: a run with no
// beat protocol has no pass, and its bytes are UNCHANGED. A masking layer that
// rewrote every plain console's output would be a behaviour change nobody asked
// for.
func TestChatReplayPassIsAbsentForAPlainRun(t *testing.T) {
	var sink strings.Builder
	plain := &chatInput{errOut: &sink}
	plain.WriteNotice("plain run: " + redactTestUUID + "\n")
	if !strings.Contains(sink.String(), redactTestUUID) {
		t.Fatalf("a run with no beat protocol was masked: %q", sink.String())
	}
	// And the driver's own entry points are nil-safe, which is what lets every
	// seam call them unconditionally.
	var nilDriver *chatReplayBeatDriver
	if got := nilDriver.maskText("x " + redactTestUUID); got != "x "+redactTestUUID {
		t.Fatalf("a nil driver changed the text: %q", got)
	}
	if chunks := nilDriver.maskAnswerStream("hello"); len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("a nil driver withheld the stream: %q", chunks)
	}
}
