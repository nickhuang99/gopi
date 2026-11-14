package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// chat_paste_wiring_test.go — the two halves of the SPILL that only exist at the
// chat layer: WHERE the store's directory comes from (the run's own session
// directory, taken from the session logger — one owner of that path), and the
// fact that a long paste really leaves the input line as a reference while the
// bytes reach that file. The editor's own cost and byte-exactness are asserted
// in internal/lineedit; what is asserted here is the WIRING.

// chatPasteSpillBody is the paste used by the wiring test: EXACTLY one full read
// (lineedit.PasteThreshold bytes) — the shape a terminal delivers, and the largest
// single read the editor's read buffer can hold (a longer block arrives as
// several reads, each spilled on its own; that is what the editor's own cost test
// exercises).
var chatPasteSpillBody = []byte(strings.Repeat("pasted-", lineedit.PasteThreshold/len("pasted-")))[:lineedit.PasteThreshold]

// TestChatPasteStoreIsTheRunsOwnSessionDirectory pins the ONE owner of the
// spill's location: the store's directory is `<session dir>/paste`, where the
// session dir is the one the SESSION LOGGER already opened (its JSONL path's
// directory) — not a second derivation of the sessions root that could disagree
// with the log's.
func TestChatPasteStoreIsTheRunsOwnSessionDirectory(t *testing.T) {
	sl, err := agent.NewSessionLogger(chatPasteTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { _ = sl.Close() }) //stylegate:ignore cleanup close of the paste fixture's session logger, after its JSONL records were consumed

	store := chatPasteStore(sl)
	if store == nil {
		t.Fatal("no store was resolved for a live session logger — a long paste would be typed into the buffer")
	}
	want := filepath.Join(sessionlayout.RecordContainer(sl.Path()), lineedit.PasteDirName)
	if store.Dir() != want {
		t.Fatalf("the paste store's directory is %q, want %q (the run's own session directory, beside its session log)",
			store.Dir(), want)
	}
	// Nothing is created before the first spill: resolving the store is not a
	// side effect on the filesystem.
	if _, err := os.Stat(store.Dir()); err == nil {
		t.Fatalf("%s exists before any paste was spilled", store.Dir())
	}
}

// chatPasteTestModel is the model string the session logger is stamped with in
// this file (the value is irrelevant to the assertion; the logger's own default
// is not spelled here).
const chatPasteTestModel = "deepseek-v4-pro"

// TestChatPasteStoreIsNilWithoutASessionLog is the honest-degradation arm: a run
// whose session logging failed gets NO store, so the editor keeps the historic
// behaviour (the paste is typed) instead of filing it under a directory nothing
// owns.
func TestChatPasteStoreIsNilWithoutASessionLog(t *testing.T) {
	if store := chatPasteStore(nil); store != nil {
		t.Fatalf("a nil session logger produced a store at %q", store.Dir())
	}
}

// TestChatInputSpillsALongPasteToTheSessionDirectory drives the chat layer end to
// end: a full read arrives (the shape a terminal delivers a paste in), and the
// line the REPL receives carries the REFERENCE — not the pasted bytes — while the
// file it names holds them exactly.
func TestChatInputSpillsALongPasteToTheSessionDirectory(t *testing.T) {
	dir := t.TempDir()
	store := lineedit.NewPasteStore(dir)

	steps := []scriptStep{
		// One full read: a paste, by the editor's own threshold.
		{data: chatPasteSpillBody},
		{timeout: true, advance: lineedit.PasteEnterSuppressWindow * 2},
		// The user presses Enter to send the reference.
		{data: []byte("\r")},
	}
	in, _ := newTestInput(t, steps, 80)
	in.SetPasteStore(store)

	lines := readLines(t, in)
	if len(lines) != 1 {
		t.Fatalf("a %d-byte paste produced %d input lines (%q), want exactly 1", len(chatPasteSpillBody), len(lines), lines)
	}
	line := lines[0]
	if strings.Contains(line, string(chatPasteSpillBody[:32])) {
		t.Fatalf("the submitted line carries the pasted bytes — the paste was typed into the buffer instead of being "+
			"spilled: %q", line)
	}
	path := pastePathInLine(t, line)
	if !strings.HasPrefix(path, store.Dir()) {
		t.Fatalf("the reference names %q, outside the store's directory %q", path, store.Dir())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the reference names an unreadable file: %v", err)
	}
	if string(got) != string(chatPasteSpillBody) {
		t.Fatalf("spilled %d bytes, pasted %d — the file must hold the paste verbatim", len(got), len(chatPasteSpillBody))
	}
}

// TestChatInputCookedPathIgnoresThePasteStore is the untouched arm: the cooked
// (non-TTY) path has no editor, so installing a store on it is a no-op and the
// piped bytes are delivered exactly as they always were.
func TestChatInputCookedPathIgnoresThePasteStore(t *testing.T) {
	in, closeIn := newChatInputCooked(strings.NewReader(string(chatPasteSpillBody)+"\n"), os.Stdout)
	defer closeIn()
	// No editor exists on this path; installing must not panic and must not
	// change what ReadLine returns.
	in.SetPasteStore(lineedit.NewPasteStore(t.TempDir()))
	lines := readLines(t, in)
	if len(lines) != 1 || strings.TrimRight(lines[0], "\n") != string(chatPasteSpillBody) {
		t.Fatalf("the cooked path returned %d line(s) (%d bytes, first %q), want one carrying the pasted bytes verbatim",
			len(lines), len(lines[0]), lines[0][:min(len(lines[0]), 64)])
	}
}

// pastePathInLine extracts the absolute path a paste reference names, reading the
// line the way the user (and the model) would.
// pastePathInLine returns the absolute path of the reference a submitted input
// line carries, read through the PRODUCTION parser (lineedit.PasteRefInfoFor).
//
// It is deliberately not a slice of the line: cutting `": " .. "]"` is the
// format's grammar restated in the test, and it stopped naming a file the moment
// the reference grew its `source=` / `size=` fields — the slice then took the
// whole body as the path. One reader for one spelling.
func pastePathInLine(t *testing.T, line string) string {
	t.Helper()
	info, ok := lineedit.PasteRefInfoFor(line)
	if !ok {
		t.Fatalf("the line carries no reference the production reader can parse: %q", line)
	}
	return info.Path
}
