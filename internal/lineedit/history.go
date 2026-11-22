package lineedit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// History bounds and on-disk shape. Every value is named (Rule 1) because each
// one is a policy decision a future reader will want to find in one place: how
// much recall we keep, how large a single recalled line may be, and what the
// persistence file looks like.
const (
	// DefaultHistoryMaxEntries is the recall bound: the most recent N
	// submitted lines survive, in both the in-memory ring and the file. The
	// file is rewritten from the ring, so the bound applies to the disk too —
	// an unbounded history file is a slow leak in the user's home directory.
	DefaultHistoryMaxEntries = 500
	// historyMaxEntryBytes caps one stored entry. A pasted megabyte is not
	// something a user recalls with Up, and an unbounded entry would let one
	// paste blow past the file bound.
	historyMaxEntryBytes = 8 * 1024
	// historyFileMode is the file's permission bits: recall is private.
	historyFileMode = 0o600
	// historyTempPattern is the atomic-rename staging name: the file is
	// written to a sibling temp file and renamed over the target, so a crash
	// mid-write can never truncate the existing history (rename within one
	// directory is atomic).
	historyTempPattern = ".tmp-*"
	// historyEscNewline / historyEscBackslash encode an entry that contains a
	// newline (multiline input is a first-class case) onto ONE physical line,
	// and restore it on load. The backslash must be escaped first, otherwise a
	// literal `\n` in the text would decode as a newline.
	historyEscNewline   = `\n`
	historyEscBackslash = `\\`
	// historyMaxLineBytes bounds one physical line of the history file's
	// scanner: a corrupt/oversized line is skipped rather than allocating
	// without limit. It is the entry cap plus the escape headroom.
	historyMaxLineBytes = historyMaxEntryBytes * 2
)

// History is a bounded, most-recent-last recall ring with atomic persistence.
//
// The ring is the single source of truth: Load reads the file into it, Add
// appends and rewrites the file from the ring (so the on-disk copy can never
// drift from what Up/Down will return). Recall navigation mirrors a shell:
// cursor -1 is the line being edited, and Prev/Next walk away from it.
type History struct {
	mu         sync.Mutex
	entries    []string
	cursor     int    // -1 = the live (unsubmitted) line
	pending    string // the live line, restored when navigation returns past the newest entry
	maxEntries int
	path       string
}

// NewHistory builds an in-memory-only ring (empty path) with the given bound;
// a bound <= 0 selects DefaultHistoryMaxEntries.
func NewHistory(maxEntries int, path string) *History {
	if maxEntries <= 0 {
		maxEntries = DefaultHistoryMaxEntries
	}
	return &History{cursor: historyCursorLive, maxEntries: maxEntries, path: path}
}

// historyCursorLive is the cursor position meaning "the line being typed",
// i.e. the position after the newest entry.
const historyCursorLive = -1

// LoadHistory reads a history file into a bounded ring. A missing file is an
// empty history with no error (the first run of a fresh home); a corrupt line
// is skipped, never fatal — a damaged recall file must not stop the CLI.
// An empty path returns an empty, non-persisting ring.
func LoadHistory(path string, maxEntries int) *History {
	h := NewHistory(maxEntries, path)
	if path == "" {
		return h
	}
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer func() {
		// deliberately ignored: the history is read-only here, and a close
		// error on a file we never write through cannot lose entries.
		_ = f.Close()
	}()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, historyMaxLineBytes), historyMaxLineBytes)
	for sc.Scan() {
		line := decodeHistoryLine(sc.Text())
		if line == "" {
			continue
		}
		h.entries = append(h.entries, line)
	}
	h.trim()
	return h
}

// trim enforces the entry bound, keeping the newest entries.
func (h *History) trim() {
	if len(h.entries) > h.maxEntries {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.maxEntries:]...)
	}
}

// Entries returns a copy of the ring, oldest first.
func (h *History) Entries() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.entries...)
}

// Len returns the number of recalled entries.
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

// Add records a submitted line and persists the ring. Adjacent duplicates are
// collapsed (pressing Enter twice on the same text is not two entries — the
// shell convention), blank lines are never recorded, and an entry longer than
// historyMaxEntryBytes is dropped rather than truncated: a truncated recalled
// prompt is worse than no recall.
func (h *History) Add(line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	if len(line) > historyMaxEntryBytes {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := len(h.entries); n > 0 && h.entries[n-1] == line {
		h.resetLocked()
		return nil
	}
	h.entries = append(h.entries, line)
	h.trim()
	h.resetLocked()
	return h.saveLocked()
}

// resetLocked returns navigation to the live line.
func (h *History) resetLocked() {
	h.cursor = historyCursorLive
	h.pending = ""
}

// Reset ends the current navigation (called when a fresh edit begins).
func (h *History) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resetLocked()
}

// Prev walks to the older entry, remembering current (the live line) so Next
// can return to it. It reports false when there is nothing older.
func (h *History) Prev(current string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.entries) == 0 {
		return "", false
	}
	if h.cursor == historyCursorLive {
		h.pending = current
		h.cursor = len(h.entries)
	}
	if h.cursor <= 0 {
		return h.entries[0], true
	}
	h.cursor--
	return h.entries[h.cursor], true
}

// Next walks toward the live line. It reports false when the live line is
// already current and there is nothing newer to recall.
func (h *History) Next() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cursor == historyCursorLive {
		return "", false
	}
	if h.cursor < len(h.entries)-1 {
		h.cursor++
		return h.entries[h.cursor], true
	}
	// Past the newest entry: back to the line the user was typing.
	live := h.pending
	h.resetLocked()
	return live, true
}

// saveLocked rewrites the history file atomically from the ring. A ring with
// no path is memory-only (tests, or a home we could not resolve).
func (h *History) saveLocked() error {
	if h.path == "" {
		return nil
	}
	dir := filepath.Dir(h.path)
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, historyFileMode|os.ModePerm); err != nil {
		return fmt.Errorf("lineedit: create history dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(h.path)+historyTempPattern)
	if err != nil {
		return fmt.Errorf("lineedit: stage history file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// deliberately ignored: a failed cleanup of an already-renamed temp is
		// not an error — the rename is what makes the write durable.
		_ = os.Remove(tmpName)
	}()
	w := bufio.NewWriter(tmp)
	for _, e := range h.entries {
		if _, err := w.WriteString(encodeHistoryLine(e) + "\n"); err != nil {
			// deliberately ignored: close-on-error keeps the REAL cause (the
			// write failure) as the returned error, not a secondary close.
			_ = tmp.Close()
			return fmt.Errorf("lineedit: write history file: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		// deliberately ignored: same reason — the flush failure is the cause.
		_ = tmp.Close()
		return fmt.Errorf("lineedit: flush history file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		// deliberately ignored: same reason — the sync failure is the cause.
		_ = tmp.Close()
		return fmt.Errorf("lineedit: sync history file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("lineedit: close history file: %w", err)
	}
	if err := os.Rename(tmpName, h.path); err != nil {
		return fmt.Errorf("lineedit: install history file %s: %w", h.path, err)
	}
	return nil
}

// encodeHistoryLine flattens one entry onto a physical line: a backslash first
// (so a stored escape cannot be re-decoded), then a newline.
func encodeHistoryLine(entry string) string {
	e := strings.ReplaceAll(entry, `\`, historyEscBackslash)
	return strings.ReplaceAll(e, "\n", historyEscNewline)
}

// decodeHistoryLine restores an entry flattened by encodeHistoryLine.
func decodeHistoryLine(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != '\\' || i+1 >= len(line) {
			b.WriteByte(line[i])
			continue
		}
		switch line[i+1] {
		case 'n':
			b.WriteByte('\n')
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		default:
			b.WriteByte(line[i])
		}
	}
	return b.String()
}
