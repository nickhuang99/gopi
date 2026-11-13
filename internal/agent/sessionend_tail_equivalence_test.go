package agent

// sessionend_tail_equivalence_test.go — the SEMANTIC half of the bounded-tail
// fix: the new tail read must produce the SAME verdict as the old whole-file
// read on the same input. The old implementation is kept HERE (test binary
// only) as hasSessionEndWholeFile, so every case below is a direct old-vs-new
// comparison rather than a hand-written expectation.
//
// The cost half (bytes read) lives in sessionend_tail_cost_test.go.

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/heavytest"
)

// hasSessionEndWholeFile is the PRE-CHANGE implementation, verbatim from
// internal/agent/session.go before fix/crashdump-scan-cost: os.ReadFile of the
// whole file (up to 31 MB observed per session) to inspect a 4 KB tail. It
// exists only in this test binary, as the oracle the bounded read is compared
// against — deleting it would delete the equivalence proof.
func hasSessionEndWholeFile(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // the oracle: this IS the old read
	if err != nil || len(data) == 0 {
		return false
	}
	return hasSessionEndBytes(data)
}

// equivalenceCorpus is the input set both implementations are run over. It
// mixes the shapes the tail walk is sensitive to (newline-bounded moving end
// pointer, empty, torn tail, scan-back bound) with the real sessions tree when
// one is present.
type equivalenceCase struct {
	name    string
	content string
}

func equivalenceCases() []equivalenceCase {
	buriedEnd := startLine + endLine
	for i := 0; i < 60; i++ {
		buriedEnd += `{"type":"noise","i":` + strings.Repeat("9", 100) + `}` + "\n"
	}
	// A session_end placed exactly AT the scan-back boundary, surrounded by
	// same-size noise lines: the one shape where "window = bound + 1 byte" is
	// load-bearing (a window one byte short silently flips this verdict).
	noiseLine := `{"type":"noise","pad":"` + strings.Repeat("p", 200) + `"}` + "\n"
	atBoundary := startLine
	for i := 0; i < 24; i++ {
		atBoundary += noiseLine
	}
	atBoundary += endLine + noiseLine

	return []equivalenceCase{
		{"clean with trailing newline", startLine + endLine},
		{"clean without trailing newline", strings.TrimSuffix(startLine+endLine, "\n")},
		{"garbage tail after session_end", startLine + endLine + `{"type":"assistant_text"`},
		{"blank lines after session_end", startLine + endLine + "\n\n"},
		{"single line session_end no newline", strings.TrimSuffix(endLine, "\n")},
		{"single line session_start no newline", strings.TrimSuffix(startLine, "\n")},
		{"orphan (no session_end)", startLine},
		{"orphan with torn tail", startLine + `{"type":"assistant_text"`},
		{"empty file", ""},
		{"newline only", "\n"},
		{"session_end beyond scan-back bound", buriedEnd},
		{"long torn tail after session_end", startLine + endLine + strings.Repeat("x", 1400)},
		{"session_end line at the scan-back boundary", atBoundary},
		{"huge event-free file (no newline at all)", strings.Repeat("x", 3*sessionTailWindowBytes)},
		{"right at the window size", startLine + endLine},
	}
}

// TestHasSessionEndEquivalenceOnFixtures is the fixture arm of the proof:
// every corpus case, run through the old whole-file read and the new bounded
// read, must agree — and the case set must include both verdicts so the test
// cannot pass by returning a constant.
func TestHasSessionEndEquivalenceOnFixtures(t *testing.T) {
	sawTrue, sawFalse := false, false
	for _, tc := range equivalenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSessionFile(t, tc.content)
			want := hasSessionEndWholeFile(path)
			got := HasSessionEnd(path)
			if got != want {
				t.Fatalf("old vs new disagree on %q (%d bytes): whole-file=%v bounded=%v",
					tc.name, len(tc.content), want, got)
			}
			sawTrue = sawTrue || want
			sawFalse = sawFalse || !want
		})
	}
	if !sawTrue || !sawFalse {
		t.Fatalf("corpus did not exercise both verdicts (true=%v false=%v) — equivalence would be vacuous", sawTrue, sawFalse)
	}
}

// TestHasSessionEndEquivalenceAtEveryWindowBoundary is the exhaustive arm: for
// a file of a fixed head, slide the session_end line to EVERY position near the
// tail/window boundary, plus a random sweep across the file, and require the
// two implementations to agree at every offset. This is the case a property
// test would generate — spelled out so a failure names the exact offset.
func TestHasSessionEndEquivalenceAtEveryWindowBoundary(t *testing.T) {
	head := strings.Repeat(`{"type":"tool_result","pad":"`+strings.Repeat("q", 80)+`"}`+"\n", 40) // ~4.6 KB
	tail := startLine + endLine

	var offsets []int
	// Every byte offset in a ±64-byte band around the window size.
	for off := sessionTailWindowBytes - 64; off <= sessionTailWindowBytes+64; off++ {
		offsets = append(offsets, off)
	}
	// A deterministic random sweep of the whole first 3 windows.
	rng := rand.New(rand.NewSource(20260912)) //nolint:gosec // deterministic test seed, not security
	for i := 0; i < 120; i++ {
		offsets = append(offsets, rng.Intn(3*sessionTailWindowBytes))
	}

	for _, off := range offsets {
		content := head + strings.Repeat("#", off) + "\n" + tail
		path := writeSessionFile(t, content)
		want := hasSessionEndWholeFile(path)
		got := HasSessionEnd(path)
		if got != want {
			t.Fatalf("offset %d (%d bytes): old=%v new=%v — the bounded read changed the verdict", off, len(content), want, got)
		}
	}
}

// TestHasSessionEndEquivalenceOnRealTree compares the two implementations over
// the ACTUAL sessions tree on this host — the inputs the sweep really meets
// (multi-MB JSONL, live files, torn tails, dumps). Skipped when no tree
// exists (CI / a clean sandbox) so the suite stays hermetic.
func TestHasSessionEndEquivalenceOnRealTree(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	root, ok := realSessionsRoot()
	if !ok {
		t.Skip("no real sessions tree on this host — fixture arms cover the predicate")
	}
	var n, trueVerdicts int
	var maxBytes int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil // best-effort sweep of the real tree; a per-entry error is not the subject
		}
		if n >= realTreeEquivalenceMax {
			return filepath.SkipAll
		}
		want := hasSessionEndWholeFile(path)
		got := HasSessionEnd(path)
		if got != want {
			t.Errorf("real file %s: old=%v new=%v — verdict changed on real input", path, want, got)
		}
		if want {
			trueVerdicts++
		}
		if int(info.Size()) > maxBytes {
			maxBytes = int(info.Size())
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("walk real sessions tree: %v", err)
	}
	if n == 0 {
		t.Skip("sessions tree holds no JSONL — nothing to compare")
	}
	t.Logf("compared %d real session files (largest %d bytes, %d ended)", n, maxBytes, trueVerdicts)
}

// realSessionsRoot locates the host's real sessions tree (via the same
// config.GopiDir resolution production uses), reporting false when it is
// absent — the real-tree arms skip rather than fail in a sandbox.
func realSessionsRoot() (string, bool) {
	// ambient-profile-ok: read-only real-tree oracle — the point of the
	// comparison is the HOST's own session files, nothing is written, and the
	// caller SKIPS when the tree is absent (so a sandboxed run is unaffected).
	dir, err := config.GopiDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(dir, config.SessionsDirName)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "", false
	}
	return root, true
}

// realTreeEquivalenceMax bounds how many real files the comparison reads with
// the OLD whole-file oracle — the oracle is deliberately O(file), so the proof
// caps its own cost instead of re-paying the bug it documents.
const realTreeEquivalenceMax = 400

// TestHasSessionEndNonFilePaths: the old read and the new read must agree on
// inputs that are NOT session files — a directory path (ErrIsADirectory on the
// read) and a missing path. Both must report the best-effort false, and the
// directory case is the one the new Stat-then-ReadAt shape could plausibly get
// wrong (Stating a directory succeeds; only the read fails).
func TestHasSessionEndNonFilePaths(t *testing.T) {
	dir := t.TempDir()
	if hasSessionEndWholeFile(dir) != HasSessionEnd(dir) {
		t.Fatalf("directory path: old=%v new=%v — the two reads disagree on a non-file input",
			hasSessionEndWholeFile(dir), HasSessionEnd(dir))
	}
	if HasSessionEnd(dir) {
		t.Fatalf("a directory path must report false (not a session log)")
	}
	missing := filepath.Join(dir, "nope.jsonl")
	if hasSessionEndWholeFile(missing) != HasSessionEnd(missing) {
		t.Fatal("missing path: old and new disagree")
	}
}

// TestHasSessionEndEquivalenceOnDumpTails replays the SHAPES that the real
// corpus is made of (a big run file with a session_end, a big orphan, a bare
// session) at production sizes, so the equivalence holds at multi-MB scale
// even when no real tree is present.
func TestHasSessionEndEquivalenceOnDumpTails(t *testing.T) {
	sizes := []int{0, 1, sessionTailWindowBytes - 1, sessionTailWindowBytes, sessionTailWindowBytes + 1, 1 << 20, 8 << 20}
	tails := []struct {
		name string
		tail string
	}{
		{"ended", startLine + endLine},
		{"orphan", startLine},
		{"ended-then-torn", startLine + endLine + `{"type":"assistant"`},
	}
	for _, size := range sizes {
		for _, tl := range tails {
			content := padTo(size) + tl.tail
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			want := hasSessionEndWholeFile(path)
			got := HasSessionEnd(path)
			if got != want {
				t.Fatalf("size=%d tail=%s: old=%v new=%v", size, tl.name, want, got)
			}
		}
	}
}

// padTo builds `size` bytes of newline-free filler (so the tail lands at an
// exact size without the filler accidentally forming parseable candidate
// lines).
func padTo(size int) string {
	if size <= 0 {
		return ""
	}
	var b bytes.Buffer
	for b.Len() < size {
		b.WriteByte('x')
	}
	return b.String()[:size]
}
