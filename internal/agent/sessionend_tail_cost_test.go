package agent

// sessionend_tail_cost_test.go — the COST half of the bounded-tail fix, pinned
// in BYTES READ (never wall-clock: a timing assertion is a flaky assertion on
// a shared host).
//
// The seam is openSessionTailReader: the test substitutes a wrapper that totals
// every byte ReadAt returns, so "the tail walk reads O(tail), not O(file)" is
// asserted as a NUMBER on a large fixture.
//
// RED-PROOF RECIPE (what this file catches): revert HasSessionEnd to
// os.ReadFile and every bound below fails on the fixture size — the old read
// pulls len(file) bytes through no seam at all (bytesRead() == 0, the first
// guard fires) and, once the seam is threaded through a whole-file read, the
// bound fires. Either way the regression shows up here, not in production.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// countingTailReader redirects the tail-read seam onto a counting file and
// returns a reader of the running byte total plus a reset.
type countingFile struct {
	*os.File
	total *atomic.Int64
}

func (c *countingFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.File.ReadAt(p, off)
	c.total.Add(int64(n))
	return n, err
}

func countingTailReader(t *testing.T) func() int64 {
	t.Helper()
	var total atomic.Int64
	prev := openSessionTailReader
	openSessionTailReader = func(path string) (sessionTailReader, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &countingFile{File: f, total: &total}, nil
	}
	t.Cleanup(func() { openSessionTailReader = prev })
	return total.Load
}

// bigSessionJSONL writes a session file of roughly padBytes of event padding
// followed by tail verbatim.
func bigSessionJSONL(t *testing.T, padBytes int, tail string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(startLine)
	pad := `{"type":"tool_result","content":"` + strings.Repeat("p", 900) + `"}` + "\n"
	for b.Len() < padBytes {
		b.WriteString(pad)
	}
	b.WriteString(tail)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write big session file: %v", err)
	}
	return path
}

// TestHasSessionEndReadsTailOnly_EndedSession is the core proof: on an 8 MB
// session (the observed production shape — up to 31 MB), one verdict reads at
// most the bounded window. The old whole-file read pulls 8 MB.
func TestHasSessionEndReadsTailOnly_EndedSession(t *testing.T) {
	const padBytes = 8 << 20
	path := bigSessionJSONL(t, padBytes, startLine+endLine)
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < padBytes {
		t.Fatalf("fixture too small: %v %v (want ≥ %d bytes)", fi, err, padBytes)
	}

	bytesRead := countingTailReader(t)
	if !HasSessionEnd(path) {
		t.Fatal("cleanly-ended 8 MB session reported as an orphan")
	}
	got := bytesRead()
	if got == 0 {
		t.Fatal("the tail seam was never used — HasSessionEnd is not reading through sessionTailRead")
	}
	if got > int64(sessionTailWindowBytes) {
		t.Fatalf("read %d bytes of a %d-byte file: the read is not bounded to sessionTailWindowBytes=%d",
			got, fi.Size(), sessionTailWindowBytes)
	}
}

// TestHasSessionEndReadsTailOnly_OrphanSession: the same bound for the FALSE
// verdict — the orphan is the case an unbounded read hurts most (the walk
// looks back and finds nothing, so it may legitimately travel the full
// scan-back bound).
func TestHasSessionEndReadsTailOnly_OrphanSession(t *testing.T) {
	path := bigSessionJSONL(t, 4<<20, `{"type":"tool_result","content":"last"}`+"\n")

	bytesRead := countingTailReader(t)
	if HasSessionEnd(path) {
		t.Fatal("orphan session (no session_end) reported as ended")
	}
	if got := bytesRead(); got > int64(sessionTailWindowBytes) {
		t.Fatalf("orphan probe read %d bytes of a 4 MB file — must stay within %d", got, sessionTailWindowBytes)
	}
}

// TestHasSessionEndReadIsProportionalToTailNotFile sweeps the fixture size and
// asserts the read is FLAT: a 64× larger file must not read a larger tail.
// This is the property the whole fix exists for — O(tail), not O(file).
func TestHasSessionEndReadIsProportionalToTailNotFile(t *testing.T) {
	sizes := []int{1 << 10, 1 << 16, 1 << 20, 16 << 20}
	for _, size := range sizes {
		path := bigSessionJSONL(t, size, startLine+endLine)
		bytesRead := countingTailReader(t)
		if !HasSessionEnd(path) {
			t.Fatalf("size %d: cleanly-ended session reported as orphan", size)
		}
		got := bytesRead()
		if got > int64(sessionTailWindowBytes) {
			t.Fatalf("size %d: read %d bytes (bound %d) — the read scales with the file", size, got, sessionTailWindowBytes)
		}
	}
}

// TestDetectOrphanSessionsReadVolumeIsTailBounded is the whole-sweep proof, at
// the seam the startup path actually uses: over a tree of large session files
// the TOTAL bytes read must stay within window × files — never the 405 MB the
// old code pulled from the reference tree. It also pins that the sweep still
// classifies correctly (one orphan among the ended files).
func TestDetectOrphanSessionsReadVolumeIsTailBounded(t *testing.T) {
	const (
		files     = 12
		fileBytes = 2 << 20
	)
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	// The tree must be QUIET for the live-window before a missing session_end
	// counts as a death (orphanLiveWindow, the sibling task's mtime guard) —
	// otherwise the fixture files are all "live" and the sweep finds nothing to
	// classify. Zeroing the window keeps this test about BYTES READ.
	t.Setenv(orphanLiveWindowSecEnv, "1")
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < files; i++ {
		content := ""
		if i == 0 {
			// The stamp must be the session's FIRST line: the classifier reads
			// only the head window for it, so it cannot sit behind the padding.
			content += deadStampStartLine("s-1")
		}
		content += padFixtureJSONL(fileBytes)
		if i == 0 {
			content += `{"type":"tool_result","content":"last"}` + "\n" // killed mid-run: no session_end
		} else {
			content += startLine + endLine
		}
		name := filepath.Join(sessionsDir, "s"+string(rune('a'+i))+".jsonl")
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		// Backdate the mtime: the live-window guard (sibling task) skips files
		// touched within orphanLiveWindow, and this test is about the READ
		// volume of the files the sweep does classify.
		old := time.Now().Add(-24 * time.Hour)
		if err := os.Chtimes(name, old, old); err != nil {
			t.Fatal(err)
		}
	}

	bytesRead := countingTailReader(t)
	orphans := DetectOrphanSessions()
	total := bytesRead()

	if len(orphans) != 1 {
		t.Fatalf("orphans = %v, want exactly 1 (classification changed)", orphans)
	}
	ceiling := int64(files * sessionTailWindowBytes)
	if total > ceiling {
		t.Fatalf("sweep read %d bytes over %d files of %d bytes each (ceiling %d = files × window) — the tree is still read whole",
			total, files, fileBytes, ceiling)
	}
	t.Logf("sweep over %d files × %d bytes = %d bytes of JSONL read %d bytes (%.4f%% of the tree)",
		files, fileBytes, files*fileBytes, total, 100*float64(total)/float64(files*fileBytes))
}

// padFixtureJSONL builds a valid-JSONL pad of roughly n bytes.
func padFixtureJSONL(n int) string {
	var b strings.Builder
	pad := `{"type":"tool_result","content":"` + strings.Repeat("p", 900) + `"}` + "\n"
	for b.Len() < n {
		b.WriteString(pad)
	}
	return b.String()
}

// TestHasSessionEndAllocChurnIsBounded pins the cost in the unit a startup
// pays: allocation churn. Before the fix each call allocated the whole file
// (4 MB), so 200 calls churned ~800 MB; the bound below is ~1% of that, which
// a pooled 4 KB window clears and a whole-file read cannot.
func TestHasSessionEndAllocChurnIsBounded(t *testing.T) {
	const (
		padBytes  = 4 << 20
		calls     = 200
		allocCeil = 4 << 20
	)
	path := bigSessionJSONL(t, padBytes, startLine+endLine)
	_ = HasSessionEnd(path) // warm the pool: the first call may allocate the window

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < calls; i++ {
		if !HasSessionEnd(path) {
			t.Fatalf("call %d: cleanly-ended session reported as orphan", i)
		}
	}
	runtime.ReadMemStats(&after)
	if churn := after.TotalAlloc - before.TotalAlloc; churn > allocCeil {
		t.Fatalf("%d tail probes over a %d MB file churned %d bytes (ceiling %d) — the read is not bounded to the window",
			calls, padBytes>>20, churn, allocCeil)
	}
}

// TestHasSessionEndWindowKeepsLineBoundary guards the invariant the window
// depends on: when the window cuts a line in half, the leftover partial line
// must stay its own (unparseable) candidate — never spliced onto the
// session_end line behind it.
func TestHasSessionEndWindowKeepsLineBoundary(t *testing.T) {
	path := bigSessionJSONL(t, 64<<10, startLine+endLine)
	if !HasSessionEnd(path) {
		t.Fatal("a window cutting mid-line must still find the session_end behind it")
	}
}

// TestHasSessionEndGarbageOnlyWindow pins the documented behaviour for a tail
// larger than the window with no session_end in it: the window holds only
// garbage, so the verdict is orphan — the same verdict the pre-change bounded
// walk reached at the same bound (a false NEGATIVE, never a false "this
// session ended").
func TestHasSessionEndGarbageOnlyWindow(t *testing.T) {
	path := writeSessionFile(t, startLine+endLine+strings.Repeat("x", 3*sessionTailWindowBytes))
	if HasSessionEnd(path) {
		t.Fatal("a garbage tail beyond the scan-back bound must report orphan (the window has no session_end)")
	}
}
