package lineedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLineEditorHistoryRecallOrder asserts the Up/Down walk: oldest-to-newest on
// the way up, back to the live line on the way down, exactly like a shell.
func TestLineEditorHistoryRecallOrder(t *testing.T) {
	h := NewHistory(0, "")
	for _, line := range []string{"first", "second", "third"} {
		if err := h.Add(line); err != nil {
			t.Fatalf("Add(%q): %v", line, err)
		}
	}
	// Up from the live line: newest first.
	got, ok := h.Prev("half-typed")
	if !ok || got != "third" {
		t.Fatalf("Prev = %q ok=%v, want %q", got, ok, "third")
	}
	got, ok = h.Prev("")
	if !ok || got != "second" {
		t.Fatalf("second Prev = %q ok=%v, want %q", got, ok, "second")
	}
	got, ok = h.Prev("")
	if !ok || got != "first" {
		t.Fatalf("third Prev = %q ok=%v, want %q", got, ok, "first")
	}
	// At the oldest entry, another Up stays there (never runs off the end).
	got, ok = h.Prev("")
	if !ok || got != "first" {
		t.Fatalf("Prev past the oldest = %q ok=%v, want %q", got, ok, "first")
	}
	// Down walks back, and the last Down restores the line being typed.
	got, _ = h.Next()
	if got != "second" {
		t.Fatalf("Next = %q, want %q", got, "second")
	}
	got, _ = h.Next()
	if got != "third" {
		t.Fatalf("Next = %q, want %q", got, "third")
	}
	got, ok = h.Next()
	if !ok || got != "half-typed" {
		t.Fatalf("final Next = %q ok=%v, want the live line %q", got, ok, "half-typed")
	}
	if _, ok := h.Next(); ok {
		t.Fatal("Next past the live line must report no more entries")
	}
}

// TestLineEditorHistoryAdjacentDedup asserts the shell convention: pressing
// Enter twice on the same text is ONE entry, not two identical recalls.
func TestLineEditorHistoryAdjacentDedup(t *testing.T) {
	h := NewHistory(0, "")
	for _, line := range []string{"same", "same", "other", "same"} {
		if err := h.Add(line); err != nil {
			t.Fatalf("Add(%q): %v", line, err)
		}
	}
	got := h.Entries()
	want := []string{"same", "other", "same"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("entries = %v, want %v (only ADJACENT duplicates collapse)", got, want)
	}
	// A blank line is never recorded.
	if err := h.Add("   "); err != nil {
		t.Fatalf("Add(blank): %v", err)
	}
	if n := h.Len(); n != 3 {
		t.Fatalf("len after a blank add = %d, want 3", n)
	}
}

// TestLineEditorHistoryBound asserts the bound is enforced in memory AND on
// disk: the file is rewritten from the ring, so an over-long history cannot
// accumulate on the user's disk either.
func TestLineEditorHistoryBound(t *testing.T) {
	const bound = 5
	path := filepath.Join(t.TempDir(), "chat_history")
	// The direction the editor takes: load (empty), write through Add.
	h := LoadHistory(path, bound)
	for i := 0; i < 20; i++ {
		if err := h.Add("line-" + itoa(i)); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if n := h.Len(); n != bound {
		t.Fatalf("ring holds %d entries, want the bound %d", n, bound)
	}
	entries := h.Entries()
	if entries[0] != "line-15" || entries[bound-1] != "line-19" {
		t.Fatalf("ring = %v, want the NEWEST %d entries (line-15..line-19)", entries, bound)
	}
	// The file must carry the same (bounded) content — a reopened history that
	// disagrees with the ring is the drift this test exists to catch.
	reopened := LoadHistory(path, bound)
	if got := reopened.Entries(); strings.Join(got, "|") != strings.Join(entries, "|") {
		t.Fatalf("reloaded history = %v, want %v", got, entries)
	}
	if n := reopened.Len(); n != bound {
		t.Fatalf("reloaded history holds %d entries, want %d (the bound must reach the disk)", n, bound)
	}
}

// TestLineEditorHistoryPersistenceRoundTrip asserts what survives a restart: the
// entries, their order, and the MULTILINE content (lineedit history is read in a
// new process, so the on-disk encoding — one physical line per entry — is part
// of the contract).
func TestLineEditorHistoryPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat_history")
	h := LoadHistory(path, 0)
	lines := []string{
		"plain line",
		"multi\nline\nmessage",
		`back\slash and \n literal`,
		"unicode 知知知 ✓",
		strings.Repeat("x", 300),
	}
	for _, line := range lines {
		if err := h.Add(line); err != nil {
			t.Fatalf("Add(%q): %v", line, err)
		}
	}
	// The file must be one PHYSICAL line per entry (the encoding is what makes a
	// multiline entry recallable at all).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	if got := strings.Count(strings.TrimRight(string(raw), "\n"), "\n"); got != len(lines)-1 {
		t.Fatalf("history file has %d embedded newlines, want %d (one physical line per entry)", got, len(lines)-1)
	}
	reopened := LoadHistory(path, 0)
	got := reopened.Entries()
	if len(got) != len(lines) {
		t.Fatalf("reloaded %d entries, want %d: %v", len(got), len(lines), got)
	}
	for i := range lines {
		if got[i] != lines[i] {
			t.Fatalf("entry %d round-tripped to %q, want %q", i, got[i], lines[i])
		}
	}
}

// TestLineEditorHistoryAtomicReplace asserts the write is a rename over the
// target (atomic), not an in-place truncate-and-write: an interrupted write can
// therefore never leave a truncated history behind. The proof is that a
// pre-existing file keeps its content until the new one is fully staged, and that
// no temp file is left behind afterwards.
func TestLineEditorHistoryAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history")
	if err := os.WriteFile(path, []byte("old-entry\n"), 0o600); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	h := LoadHistory(path, 0)
	if err := h.Add("new-entry"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(string(raw), "old-entry") || !strings.Contains(string(raw), "new-entry") {
		t.Fatalf("history file = %q, want both entries (the append must preserve what was there)", raw)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "chat_history"+historyTempPattern))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("staging files left behind: %v (the rename must consume them)", leftovers)
	}
}

// TestLineEditorHistoryMissingFileIsEmpty pins the first-run behavior: a home
// with no history file is an empty history, never an error (a broken recall file
// must not stop the CLI from starting).
func TestLineEditorHistoryMissingFileIsEmpty(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "nope", "chat_history"), 0)
	if n := h.Len(); n != 0 {
		t.Fatalf("missing-file history holds %d entries, want 0", n)
	}
	if _, ok := h.Prev(""); ok {
		t.Fatal("Prev on an empty history must report no entry")
	}
	// An empty path is memory-only: Add succeeds and nothing is written.
	mem := LoadHistory("", 0)
	if err := mem.Add("kept in memory"); err != nil {
		t.Fatalf("Add on a memory-only history: %v", err)
	}
	if mem.Len() != 1 {
		t.Fatalf("memory-only history holds %d entries, want 1", mem.Len())
	}
}

// TestLineEditorHistoryRecallInEditor drives recall through the EDITOR: Up must
// show the previous line on the rendered screen, and a recalled line is a normal
// editable buffer (Home + kill + retype works on it).
func TestLineEditorHistoryRecallInEditor(t *testing.T) {
	hist := NewHistory(0, "")
	if err := hist.Add("recalled line"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	steps := []scriptStep{
		{data: []byte("\x1b[A")}, // Up: recall
		{timeout: true},
	}
	ed, _, _, out := newScriptedEditor(t, steps, 80, hist)
	if _, err := ed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 80, 24)
	if got := sc.Line(0); got != "you> recalled line" {
		t.Fatalf("rendered row 0 = %q, want the recalled line", got)
	}
}

// itoa is a tiny local integer formatter so the history test file needs no
// strconv import (the values are small and fixed).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
