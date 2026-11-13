package tools

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// homeEnvName is the environment variable os.UserHomeDir resolves "~" through —
// the seam the tilde-expansion tests override with a fixture home (Rule 1b:
// the literal lives here once, never inline).
const homeEnvName = "HOME"

func TestRead_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello, gopi\nline two\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("Read(%q, 0, 0) unexpected error: %v", path, err)
	}
	if got != content {
		t.Fatalf("Read(%q, 0, 0) = %q, want %q", path, got, content)
	}
}

func TestRead_RelativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relative.txt")
	content := "relative path test\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// Change to the temp dir so a relative path works.
	origDir, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	//stylegate:ignore restores the cwd the fixture changed (the test's own temp dir)
	defer func() { _ = os.Chdir(origDir) }()

	got, err := Read("relative.txt", 0, 0)
	if err != nil {
		t.Fatalf("Read(%q, 0, 0) unexpected error: %v", "relative.txt", err)
	}
	if got != content {
		t.Fatalf("Read(%q, 0, 0) = %q, want %q", "relative.txt", got, content)
	}
}

func TestRead_NonexistentFile(t *testing.T) {
	_, err := Read("/tmp/gopi-nonexistent-file-9f8a7b6c.txt", 0, 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "no such file") &&
		!strings.Contains(err.Error(), "cannot find") {
		t.Logf("error message (should mention file not found): %v", err)
	}
}

func TestRead_Directory(t *testing.T) {
	dir := t.TempDir()
	_, err := Read(dir, 0, 0)
	if err == nil {
		t.Fatal("expected error for directory, got nil")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("error should mention 'directory', got: %v", err)
	}
}

func TestRead_EmptyPath(t *testing.T) {
	_, err := Read("", 0, 0)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error should mention 'empty', got: %v", err)
	}
}

// TestRead_TildeExpansion proves "~/x" expands to the HOME directory without
// ever writing into the developer's REAL home. expandHome resolves the tilde
// through os.UserHomeDir, which honours $HOME, so a fixture home is installed
// with t.Setenv and the file lives inside it — the real home is only READ
// (snapshotted before/after) to assert nothing new appeared there.
//
// Regression guard (audit 1.3, test-code-discipline): this test used to write
// ~/gopi-read-test-<randomSuffix()>.txt into the real $HOME, safe only because
// of `defer os.Remove` — any path that skips the defer (os.Exit, a panic in
// another test of the same binary, SIGKILL) left a stray file in the one tree
// on this host that is not scratch; randomSuffix() was not random either
// (filepath.Base(os.TempDir()) — a constant per run).
func TestRead_TildeExpansion(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home dir: %v", err)
	}
	before := homeEntryNames(t, realHome)

	// Install a fixture $HOME: expandHome resolves "~" via os.UserHomeDir, so
	// "~/…" resolves inside this t.TempDir() (auto-removed, never the real home).
	fixtureHome := t.TempDir()
	t.Setenv(homeEnvName, fixtureHome)

	fileName := "gopi-read-test.txt"
	path := filepath.Join(fixtureHome, fileName)
	content := "tilde expansion test\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create temp file in the fixture home: %v", err)
	}
	if err := os.Chmod(fixtureHome, 0o700); err != nil {
		t.Fatalf("tighten fixture home mode: %v", err)
	}

	got, err := Read("~/"+fileName, 0, 0)
	if err != nil {
		t.Fatalf("Read(%q, 0, 0) unexpected error: %v", "~/"+fileName, err)
	}
	if got != content {
		t.Fatalf("Read(%q, 0, 0) = %q, want %q", "~/"+fileName, got, content)
	}

	// The point of the fix: the real home gained no entry.
	if after := homeEntryNames(t, realHome); !slices.Equal(before, after) {
		t.Errorf("test must not touch the real home %s:\n before %v\n after  %v", realHome, before, after)
	}
}

// homeEntryNames lists the sorted entry names of dir (the before/after probe
// for "a test must not write into the real home").
func homeEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list home dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// TestHomeEntryNamesProbe_DetectsNewEntry proves the before/after probe used by
// TestRead_TildeExpansion is not vacuous: a listing taken before and after an
// added entry must differ. Without this, the "the real home gained no entry"
// assertion would silently pass even if homeEntryNames returned a constant.
// (The probe itself is exercised on a t.TempDir(), never the real home.)
func TestHomeEntryNamesProbe_DetectsNewEntry(t *testing.T) {
	dir := t.TempDir()
	before := homeEntryNames(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "appeared.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write probe fixture: %v", err)
	}
	after := homeEntryNames(t, dir)
	if slices.Equal(before, after) {
		t.Fatalf("probe missed a new entry: before %v, after %v", before, after)
	}
	if !slices.Contains(after, "appeared.txt") {
		t.Errorf("probe listing missing the new entry: %v", after)
	}
}

// TestRead_TildeAlone locks bare-"~" semantics: it expands to the home
// directory, whose read is refused as a directory. As in
// TestRead_TildeExpansion the home is a fixture, so the real $HOME is only
// read (never entered by the tool).
func TestRead_TildeAlone(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv(homeEnvName, fixtureHome)

	_, err := Read("~", 0, 0)
	// "~" alone expands to home dir, which is a directory → should error.
	if err == nil {
		t.Fatal("expected error when reading home directory via ~, got nil")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("error should mention 'directory', got: %v", err)
	}
}

func TestRead_WithOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	// 10 lines: "line 1", "line 2", ..., "line 10"
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// offset=3 → skip first 3 lines, return lines 4-10.
	got, err := Read(path, 3, 0)
	if err != nil {
		t.Fatalf("Read(%q, 3, 0) unexpected error: %v", path, err)
	}
	want := strings.Join(lines[3:], "\n") // lines[3] = "line 4"
	if got != want {
		t.Fatalf("Read(%q, 3, 0) = %q, want %q", path, got, want)
	}
}

func TestRead_WithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// limit=5 → return first 5 lines.
	got, err := Read(path, 0, 5)
	if err != nil {
		t.Fatalf("Read(%q, 0, 5) unexpected error: %v", path, err)
	}
	want := strings.Join(lines[:5], "\n")
	if got != want {
		t.Fatalf("Read(%q, 0, 5) = %q, want %q", path, got, want)
	}
}

func TestRead_WithOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// offset=2, limit=3 → skip first 2 lines, return next 3 (lines 3-5).
	got, err := Read(path, 2, 3)
	if err != nil {
		t.Fatalf("Read(%q, 2, 3) unexpected error: %v", path, err)
	}
	want := strings.Join(lines[2:5], "\n") // lines[2:5] = "line 3", "line 4", "line 5"
	if got != want {
		t.Fatalf("Read(%q, 2, 3) = %q, want %q", path, got, want)
	}
}

func TestRead_BackwardsCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// offset=0, limit=0 → all lines (existing behavior).
	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("Read(%q, 0, 0) unexpected error: %v", path, err)
	}
	if got != content {
		t.Fatalf("Read(%q, 0, 0) = %q, want %q", path, got, content)
	}
}

// TestRead_FullReadTruncates verifies the full-read context guard: a file
// larger than maxFullReadBytes is returned truncated at a line boundary with
// a hint, and the untruncated content stays reachable via offset/limit.
func TestRead_FullReadTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.go")

	// 60KB of lines: above maxFullReadBytes (48KB), below the 100KB block.
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "line %04d: %s\n", i, strings.Repeat("x", 30))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= maxFullReadBytes {
		t.Fatalf("test file too small: %d bytes", info.Size())
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("Read full: %v", err)
	}
	if len(got) > maxFullReadBytes+512 {
		t.Fatalf("full read not truncated: %d bytes > %d", len(got), maxFullReadBytes)
	}
	if !strings.Contains(got, "[read truncated:") {
		t.Errorf("expected truncation hint, got tail: %q", got[len(got)-120:])
	}
	// Truncated at a line boundary: the hint is preceded by the line-ending
	// newline plus the hint's own leading blank line.
	idx := strings.Index(got, "[read truncated:")
	if idx < 2 || got[idx-2] != '\n' || got[idx-1] != '\n' {
		t.Errorf("truncation did not land on a line boundary (hint at %d)", idx)
	}
	// The full content remains reachable via paging (R3-3: a single limit
	// read is now byte-bounded, so the tail is fetched by the footer's
	// offset instead of one oversized window).
	m := regexp.MustCompile(`showing 1-(\d+) — continue with offset=\d+`)
	sub := m.FindStringSubmatch(got)
	if sub == nil {
		t.Fatalf("footer missing shown count: %q", got[len(got)-120:])
	}
	shown, err := strconv.Atoi(sub[1])
	if err != nil {
		t.Fatal(err)
	}
	rest, err := Read(path, shown, 0)
	if err != nil {
		t.Fatalf("Read with offset=%d: %v", shown, err)
	}
	if !strings.Contains(rest, "line 1999:") {
		t.Errorf("paged read missing the file tail (truncation lost content): %q", rest[len(rest)-80:])
	}
}

// TestRead_PageByteBudget verifies R3-3: an offset/limit page is bounded by
// maxPageReadBytes as well as the line count — a window of long lines is
// truncated at a line boundary with a footer instead of returning 86-90KB
// (the R2 A/B measurement under the old line-only cap).
func TestRead_PageByteBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "longlines.txt")

	// 80 lines of 1.5KB ≈ 120KB: a limit=80 window would be ~120KB under the
	// line-only cap; the byte budget cuts it near 50KB.
	var sb strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&sb, "L%03d:%s\n", i, strings.Repeat("y", 1500))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 0, 80)
	if err != nil {
		t.Fatalf("Read page: %v", err)
	}
	if len(got) > maxPageReadBytes+512 {
		t.Fatalf("page exceeded the byte budget: %d bytes", len(got))
	}
	m := regexp.MustCompile(`\[read truncated: file has (\d+) lines total, showing 1-(\d+) — continue with offset=\d+`)
	sub := m.FindStringSubmatch(got)
	if sub == nil {
		t.Fatalf("byte-budget page missing footer: tail=%q", got[len(got)-160:])
	}
	if sub[1] != "80" {
		t.Errorf("footer total = %s, want 80", sub[1])
	}
	// Truncated at a line boundary: every line in the page is complete (no
	// mid-line split) — lines join with '\n' and the last one has no trailing
	// newline, so each piece must match the full L%03d shape.
	pageEnd := strings.Index(got, "\n\n[read truncated:")
	if pageEnd < 0 {
		t.Fatalf("byte-budget page missing the footer")
	}
	for i, piece := range strings.Split(got[:pageEnd], "\n") {
		if !regexp.MustCompile(`^L\d{3}:y+$`).MatchString(piece) {
			t.Fatalf("page line %d split mid-line: %q", i, piece[:min(40, len(piece))])
		}
	}
	// The footer's offset resumes on the exact next line, with no loss.
	shown, err := strconv.Atoi(sub[2])
	if err != nil {
		t.Fatal(err)
	}
	next, err := Read(path, shown, 3)
	if err != nil {
		t.Fatalf("Read(offset=%d): %v", shown, err)
	}
	wantNext := fmt.Sprintf("L%03d:", shown)
	if !strings.HasPrefix(next, wantNext) {
		t.Errorf("continuation starts %q, want %q", firstLine(next), wantNext)
	}
}

// TestRead_FullReadFooter verifies the R3-1 footer: a truncated full read
// names the total line count and the next offset, and continuing with that
// offset resumes exactly after the last shown line (the R2 A/B gap — the old
// hint had neither, so the model could not detect EOF or continue without
// guessing).
func TestRead_FullReadFooter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.go")
	// 2100 lines of 42 bytes ≈ 88KB: above maxFullReadBytes (48KB) but below
	// the 100KB single-line-trap guard, so the truncation footer fires.
	const total = 2100
	var sb strings.Builder
	for i := 0; i < total; i++ {
		fmt.Fprintf(&sb, "line %04d: %s\n", i, strings.Repeat("x", 30))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("Read full: %v", err)
	}
	m := regexp.MustCompile(`\[read truncated: file has (\d+) lines total, showing 1-(\d+) — continue with offset=(\d+) \(offset=N skips the first N lines\)\]`)
	sub := m.FindStringSubmatch(got)
	if sub == nil {
		t.Fatalf("footer missing total lines/next offset: tail=%q", got[len(got)-160:])
	}
	if sub[1] != strconv.Itoa(total) {
		t.Errorf("footer total = %s, want %d", sub[1], total)
	}
	if sub[3] != sub[2] {
		t.Errorf("footer next offset %s != shown lines %s", sub[3], sub[2])
	}
	shown, err := strconv.Atoi(sub[2])
	if err != nil || shown <= 0 || shown >= total {
		t.Fatalf("footer shown count %q out of range", sub[2])
	}

	// Continuing at the footer's offset resumes with the line right after the
	// truncated content — no guesswork, no skipped line.
	rest, err := Read(path, shown, 0)
	if err != nil {
		t.Fatalf("Read(offset=%d): %v", shown, err)
	}
	wantNext := fmt.Sprintf("line %04d:", shown)
	if !strings.HasPrefix(rest, wantNext) {
		t.Errorf("continuation starts %q, want %q", firstLine(rest), wantNext)
	}
}

// TestRead_LargeFileStreamsNotRejected verifies R3-2: a full read of a file
// above the old 100KB hard block now returns the first 48KB at a line
// boundary with a footer (total lines + next offset) instead of rejecting —
// the R2 A/B measured the rejection's cost as two wasted rounds (bash wc -l
// + a failed read). Only the single-line trap keeps a hard error.
func TestRead_LargeFileStreamsNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.go")

	// ~142KB of normal lines: above hugeLineGuardBytes, average line ~42B.
	const total = 3400
	var sb strings.Builder
	for i := 0; i < total; i++ {
		fmt.Fprintf(&sb, "line %04d: %s\n", i, strings.Repeat("x", 30))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= hugeLineGuardBytes {
		t.Fatalf("test file too small to exercise the streaming path: %d bytes", info.Size())
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("full read of >100KB file must stream, not reject: %v", err)
	}
	if len(got) > maxFullReadBytes+512 {
		t.Fatalf("streamed read not bounded: %d bytes", len(got))
	}
	// Footer must carry the total line count and the next offset.
	m := regexp.MustCompile(`\[read truncated: file has (\d+) lines total, showing 1-(\d+) — continue with offset=\d+ \(offset=N skips the first N lines\)\]`)
	sub := m.FindStringSubmatch(got)
	if sub == nil {
		t.Fatalf("streamed read missing footer: tail=%q", got[len(got)-160:])
	}
	if sub[1] != strconv.Itoa(total) {
		t.Errorf("footer total = %s, want %d", sub[1], total)
	}
	// The tail of the file stays reachable via the footer's offsets — paging
	// with the next offset from each footer until the last line shows up.
	shown, err := strconv.Atoi(sub[2])
	if err != nil {
		t.Fatal(err)
	}
	// Keep paging: each page starts at the previous footer's next offset;
	// the tail shows up once the final (untruncated) page is reached.
	for guard := 0; guard < 10; guard++ {
		page, err := Read(path, shown, 0)
		if err != nil {
			t.Fatalf("Read(offset=%d): %v", shown, err)
		}
		if strings.Contains(page, "line 3399:") {
			return
		}
		m2 := regexp.MustCompile(`showing \d+-(\d+) — continue with offset=(\d+)`)
		s2 := m2.FindStringSubmatch(page)
		if s2 == nil {
			t.Fatalf("continuation page missing footer: tail=%q", page[len(page)-120:])
		}
		next, err := strconv.Atoi(s2[2])
		if err != nil {
			t.Fatal(err)
		}
		if next == shown {
			t.Fatalf("footer offset did not advance (stuck at %d)", shown)
		}
		shown = next
	}
	t.Fatalf("file tail unreachable after 10 pages")
}

// TestRead_HugeLimitsCannotBypassByteBudget verifies R3-4: the R2 A/B found
// the old 100KB guard bypassable — read(offset=0, limit=100000) on a 760KB
// file injected 742KB into the context in one call. The byte budget now
// applies to every page regardless of offset/limit values.
func TestRead_HugeLimitsCannotBypassByteBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")

	// ~600KB of normal lines (avg ~42B/line — under the single-line trap).
	var sb strings.Builder
	for i := 0; i < 15000; i++ {
		fmt.Fprintf(&sb, "line %05d: %s\n", i, strings.Repeat("x", 30))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100*1024 {
		t.Fatalf("test file too small: %d bytes", info.Size())
	}

	// A huge limit must not bypass the byte budget — one page, one footer.
	got, err := Read(path, 0, 1000000)
	if err != nil {
		t.Fatalf("Read(limit=1000000): %v", err)
	}
	if len(got) > maxPageReadBytes+512 {
		t.Fatalf("huge-limit read bypassed the byte budget: %d bytes", len(got))
	}
	if !strings.Contains(got, "[read truncated:") {
		t.Errorf("huge-limit page missing the truncation footer")
	}

	// A huge offset with a huge limit is likewise bounded (and may be empty —
	// skipping past EOF is legitimate, never an unbounded read).
	got2, err := Read(path, 1000000, 1000000)
	if err != nil {
		t.Fatalf("Read(offset=1000000, limit=1000000): %v", err)
	}
	if len(got2) > maxPageReadBytes+512 {
		t.Fatalf("huge offset+limit read bypassed the byte budget: %d bytes", len(got2))
	}
}

// TestRead_SingleLineTrapRejected verifies the one remaining hard rejection:
// a >100KB file whose lines average > maxBytesPerLine is unreadable in any
// window, so read refuses with the bash hint (the R2 A/B "single-line trap").
func TestRead_SingleLineTrapRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trap.go")
	// 130KB written as 6 lines of ~21KB — average way above 10KB/line.
	var sb strings.Builder
	for i := 0; i < 6; i++ {
		sb.WriteString(strings.Repeat("x", 21*1024) + "\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// Both the full read and partial reads hit the trap.
	for _, tc := range []struct{ offset, limit int }{{0, 0}, {0, 2}} {
		_, err := Read(path, tc.offset, tc.limit)
		if err == nil {
			t.Fatalf("Read(offset=%d, limit=%d) must reject a single-line trap file", tc.offset, tc.limit)
		}
		if !strings.Contains(err.Error(), "bash(head/cut)") {
			t.Errorf("trap error should point at bash(head/cut), got: %v", err)
		}
	}
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestRead_BinaryFileRejected verifies that a binary file (NUL byte within
// the 8KB sample window) is rejected with a clear hint instead of returning
// mojibake into the context.
func TestRead_BinaryFileRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	// Small binary: a PNG-ish header with a NUL byte in the first bytes.
	content := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to create binary file: %v", err)
	}

	_, err := Read(path, 0, 0)
	if err == nil {
		t.Fatal("expected error for binary file, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "binary file") {
		t.Errorf("error should mention 'binary file', got: %v", msg)
	}
	// The hint must teach the model the inspection path (bash file/xxd), not
	// how to read the file.
	if !strings.Contains(msg, "bash('file") || !strings.Contains(msg, "xxd") {
		t.Errorf("error should suggest bash('file')/bash('xxd'), got: %v", msg)
	}
}

// TestRead_BinaryFileRejectedWithLimit verifies binary detection also applies
// to offset/limit reads (the sample check runs before line scanning).
func TestRead_BinaryFileRejectedWithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	content := append([]byte("text line\n"), 0x00, 0x01, 0x02)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to create binary file: %v", err)
	}

	_, err := Read(path, 0, 5)
	if err == nil {
		t.Fatal("expected error for binary file with limit, got nil")
	}
	if !strings.Contains(err.Error(), "binary file") {
		t.Errorf("error should mention 'binary file', got: %v", err)
	}
}

// TestRead_InvalidUTF8Rejected verifies that a file whose sampled prefix is
// not valid UTF-8 (but has no NUL byte) is also rejected as binary.
func TestRead_InvalidUTF8Rejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "latin1.txt")
	// 0xFF is not a valid UTF-8 leading byte; no NUL present.
	content := []byte{0x4c, 0x61, 0x74, 0x69, 0x6e, 0xff, 0x2d, 0x31}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to create invalid-UTF-8 file: %v", err)
	}

	_, err := Read(path, 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid-UTF-8 file, got nil")
	}
	if !strings.Contains(err.Error(), "binary file") {
		t.Errorf("error should mention 'binary file', got: %v", err)
	}
}

// Regression fixture for the false "binary file（检测到无效 UTF-8）" rejection:
// the REAL artifact devdoc/deepseek-v41-flash-pricing-table-repair-20260911.md
// (14365 bytes) carries these bytes at offset midRuneFixtureOffset. Its sample
// window cut at maxBinarySampleBytes lands INSIDE the 3-byte rune 工 (e5 b7 a5),
// so the raw sample is not valid UTF-8 although the file is 100% valid UTF-8.
const (
	// midRuneFixtureOffset is the real artifact's offset of the sequence below.
	midRuneFixtureOffset = 8188
	// realArtifactPath is the repo-relative report that exposed the bug (the
	// test skips when it is not checked out).
	realArtifactPath = "../../devdoc/deepseek-v41-flash-pricing-table-repair-20260911.md"
)

// cjkRuneRepeatCount yields a sample longer than maxBinarySampleBytes composed
// purely of 3-byte runes, so the window cut is guaranteed mid-rune
// (maxBinarySampleBytes%3 == 2).
const cjkRuneRepeatCount = 3000

// realMidRuneSampleBytes is the real artifact's byte sequence at
// midRuneFixtureOffset, kept as BYTES so a window sliced at
// maxBinarySampleBytes is reproduced byte-identically (a string literal would
// not show where the cut lands).
var realMidRuneSampleBytes = []byte{
	0xe6, 0x89, 0x8b, // 手
	0xe5, 0xb7, 0xa5, // 工 — the sample window cuts INSIDE this rune
	0xe5, 0x8f, 0x8d, // 反
	0xe6, // lead byte of the following rune
}

// TestRead_SampleCutMidRuneNotRejected is the regression guard for the false
// binary rejection: an incomplete TRAILING rune (an artifact of where the 8KB
// window was cut) must not mark a valid-UTF-8 file as binary — while an
// interior invalid byte, a bare 0xFF at the cut, and any NUL stay rejected.
func TestRead_SampleCutMidRuneNotRejected(t *testing.T) {
	// (a1) systematic CJK case: every 3-byte-rune file whose length crosses the
	// window leaves the sample ending mid-rune.
	t.Run("cjk_repeat_cut_mid_rune", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cjk.md")
		content := strings.Repeat("中", cjkRuneRepeatCount) + "\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		// Precondition: the sample really is cut mid-rune (else the test
		// proves nothing).
		sample := []byte(content)[:maxBinarySampleBytes]
		if utf8.Valid(sample) {
			t.Fatalf("fixture invalid: sample at %d bytes ends on a rune boundary", maxBinarySampleBytes)
		}

		got, err := Read(path, 0, 0)
		if err != nil {
			t.Fatalf("valid-UTF-8 file cut mid-rune must not be rejected, got: %v", err)
		}
		if got != content {
			t.Fatalf("Read returned %d bytes, want %d", len(got), len(content))
		}
	})

	// (a2) the real artifact's byte sequence, byte-identically.
	t.Run("real_artifact_bytes_cut_mid_rune", func(t *testing.T) {
		if midRuneFixtureOffset >= maxBinarySampleBytes ||
			midRuneFixtureOffset+len(realMidRuneSampleBytes) <= maxBinarySampleBytes {
			t.Fatalf("fixture invalid: offset %d does not straddle maxBinarySampleBytes=%d",
				midRuneFixtureOffset, maxBinarySampleBytes)
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "pricing.md")
		content := make([]byte, 0, midRuneFixtureOffset+len(realMidRuneSampleBytes))
		content = append(content, bytes.Repeat([]byte("a"), midRuneFixtureOffset)...)
		content = append(content, realMidRuneSampleBytes...)
		// Close the rune whose lead byte ends the fixture (0xe6 0x8a 0x80 = 报)
		// so the reproduced file is valid UTF-8 throughout.
		content = append(content, 0x8a, 0x80)
		content = append(content, []byte("tail\n")...)
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		sample := content[:maxBinarySampleBytes]
		if utf8.Valid(sample) {
			t.Fatalf("fixture invalid: the real artifact's sample bytes should cut mid-rune")
		}
		if !utf8.Valid(content) {
			t.Fatalf("fixture invalid: the reproduced content must be valid UTF-8 throughout")
		}

		if _, err := Read(path, 0, 0); err != nil {
			t.Fatalf("real-artifact byte sequence must not be rejected, got: %v", err)
		}
	})

	// (a3) the real report file itself, when checked out (skipped elsewhere).
	t.Run("real_report_file_reads", func(t *testing.T) {
		full, err := os.ReadFile(realArtifactPath)
		if err != nil {
			t.Skipf("real artifact not checked out: %v", err)
		}
		if !utf8.Valid(full) {
			t.Fatalf("fixture invalid: %s must be valid UTF-8", realArtifactPath)
		}
		got, err := Read(realArtifactPath, 0, 0)
		if err != nil {
			t.Fatalf("real valid-UTF-8 report must read, got: %v", err)
		}
		if got != string(full) {
			t.Fatalf("Read(%s) = %d bytes, want %d (byte-identical)", realArtifactPath, len(got), len(full))
		}
	})

	// (b) an INTERIOR invalid byte is still rejected — the trim only excuses
	// the final, provably incomplete rune.
	t.Run("interior_invalid_byte_rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "interior.txt")
		content := []byte(strings.Repeat("中", cjkRuneRepeatCount))
		content[maxBinarySampleBytes/2] = 0xff // middle of the sample window
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if _, err := Read(path, 0, 0); err == nil {
			t.Fatal("interior 0xFF must still be rejected as binary")
		}
	})

	// (b2) a bare 0xFF / a lone continuation byte AT the cut is not an
	// incomplete rune — repairing it would defeat detection, so it stays
	// rejected.
	t.Run("bare_invalid_byte_at_cut_rejected", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			tail byte
		}{
			{"bare_0xff", 0xff},
			{"lone_continuation_byte", 0x80},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "cut.txt")
				content := append(bytes.Repeat([]byte("a"), maxBinarySampleBytes-1), tc.tail)
				if err := os.WriteFile(path, content, 0644); err != nil {
					t.Fatalf("failed to create file: %v", err)
				}
				if _, err := Read(path, 0, 0); err == nil {
					t.Fatalf("trailing 0x%02x must still be rejected as binary", tc.tail)
				}
			})
		}
	})

	// (c) the NUL rule is unchanged: a NUL anywhere in the sample — including
	// the very last sampled byte — is still binary.
	t.Run("nul_rejected", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			index int
		}{
			{"nul_interior", maxBinarySampleBytes / 2},
			{"nul_last_sampled_byte", maxBinarySampleBytes - 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "nul.txt")
				content := []byte(strings.Repeat("中", cjkRuneRepeatCount))
				content[tc.index] = 0x00
				if err := os.WriteFile(path, content, 0644); err != nil {
					t.Fatalf("failed to create file: %v", err)
				}
				_, err := Read(path, 0, 0)
				if err == nil {
					t.Fatal("NUL byte must still be rejected as binary")
				}
				if !strings.Contains(err.Error(), "NUL") {
					t.Errorf("NUL rejection should name the NUL rule, got: %v", err)
				}
			})
		}
	})
}

// TestTrimIncompleteTrailingRune pins the helper's contract directly: it trims
// exactly one sliced rune and leaves every other byte sequence untouched.
func TestTrimIncompleteTrailingRune(t *testing.T) {
	tests := []struct {
		name  string
		in    []byte
		want  []byte
		valid bool // whether the RESULT is valid UTF-8 (what checkBinary uses)
	}{
		{"ascii untouched", []byte("hello"), []byte("hello"), true},
		{"complete rune untouched", []byte("中"), []byte("中"), true},
		{"truncated 3-byte rune trimmed", []byte{0xe4, 0xb8}, []byte{}, true},
		{"truncated 2-byte rune trimmed", []byte{0xc3}, []byte{}, true},
		{"truncated rune after text trimmed", append([]byte("ab"), 0xe4, 0xb8), []byte("ab"), true},
		{"bare 0xff kept", []byte{0x4c, 0xff}, []byte{0x4c, 0xff}, false},
		{"lone continuation byte kept", []byte{0x4c, 0x80}, []byte{0x4c, 0x80}, false},
		{"interior invalid byte kept", []byte{0x4c, 0xff, 0x61, 0xe4, 0xb8}, []byte{0x4c, 0xff, 0x61, 0xe4, 0xb8}, false},
		{"empty sample", []byte{}, []byte{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trimIncompleteTrailingRune(tc.in)
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("trimIncompleteTrailingRune(% x) = % x, want % x", tc.in, got, tc.want)
			}
			if utf8.Valid(got) != tc.valid {
				t.Fatalf("utf8.Valid(trim(% x)) = %v, want %v", tc.in, utf8.Valid(got), tc.valid)
			}
		})
	}
}

// TestRead_NormalTextStillPasses guards the negative case: the binary check
// must not reject ordinary text (incl. UTF-8 multibyte content).
func TestRead_NormalTextStillPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "text.go")
	content := "package main\n\n// 中文注释 with ümläut\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create text file: %v", err)
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("Read(%q, 0, 0) unexpected error: %v", path, err)
	}
	if got != content {
		t.Fatalf("Read(%q, 0, 0) = %q, want %q", path, got, content)
	}
}

// TestRead_NULAfterSampleWindow verifies the documented sampling boundary:
// a NUL byte beyond the first 8KB is not caught (per spec the 8KB sample is
// the detection window). This locks in the sampling semantics.
func TestRead_NULAfterSampleWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edge.txt")
	// 8KB of text, then a NUL — outside the sample window.
	content := strings.Repeat("a", maxBinarySampleBytes) + "\x00tail"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	if _, err := Read(path, 0, 0); err != nil {
		t.Fatalf("NUL beyond the 8KB sample window should not be rejected, got: %v", err)
	}
}

// TestRead_SingleHugeLineFullReturnsFragment verifies the R3 read patch: a full
// read of a 48-100KB file that is a single giant line (no line break inside the
// first maxFullReadBytes) returns a SERVICE FRAGMENT — the first
// maxFullReadBytes cut at a rune boundary plus a footer that names the line
// count and points at bash/xxd — instead of falling back to the old bare error.
func TestRead_SingleHugeLineFullReturnsFragment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "singleline.go")
	// 60KB single line: between maxFullReadBytes (48KB) and hugeLineGuardBytes
	// (100KB), so the avg-based trap does not fire and readFull runs.
	content := strings.Repeat("a", 60*1024)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= maxFullReadBytes || info.Size() >= hugeLineGuardBytes {
		t.Fatalf("test file must be 48-100KB, got %d bytes", info.Size())
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("full read of a single huge line must serve a fragment, not error: %v", err)
	}
	if len(got) > maxFullReadBytes+512 {
		t.Fatalf("service fragment not bounded: %d bytes", len(got))
	}
	// Footer must be present and name the single-line nature.
	if !strings.Contains(got, "single-line huge file") {
		t.Errorf("fragment missing the single-line footer, tail=%q", got[len(got)-160:])
	}
	if !strings.Contains(got, "bash(head/cut/xxd)") {
		t.Errorf("fragment footer must point at bash(head/cut/xxd), tail=%q", got[len(got)-160:])
	}
	if !strings.Contains(got, "offset/limit") {
		t.Errorf("fragment footer must point at offset/limit paging, tail=%q", got[len(got)-160:])
	}
	// The fragment body must be non-empty content.
	if i := strings.Index(got, "\n\n[single-line"); i < 0 {
		t.Errorf("fragment body or footer missing")
	}
}

// TestRead_SingleHugeLineRuneBoundary verifies the fragment is cut at a UTF-8
// rune boundary even when the cut lands mid-multibyte-rune — no mojibake tail.
func TestRead_SingleHugeLineRuneBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "singleline_utf8.txt")
	// A single huge line of multi-byte 'é' runs preceded by one ASCII byte so
	// every 'é' starts at an ODD byte offset: maxFullReadBytes (49152, even)
	// therefore lands mid-rune, exercising the runeSafePrefix backup.
	// A single huge line of 3-byte '€' runes preceded by a 2-byte ASCII prefix.
	// Rune starts land at offsets ≡ 2 (mod 3): the 8KB binary-check sample ends
	// exactly on a rune boundary (8192 ≡ 2 mod 3 → valid), but the 48KB cut at
	// 49152 (≡ 0 mod 3) lands mid-rune — exercising the runeSafePrefix backup.
	content := "xy" + strings.Repeat("€", 27000) // 2 + 81000 = 81002 bytes
	if len(content) <= maxFullReadBytes || len(content) >= hugeLineGuardBytes {
		t.Fatalf("test file must be 48-100KB, got %d bytes", len(content))
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("Read single-line utf-8: %v", err)
	}
	body := got
	if i := strings.Index(got, "\n\n[single-line"); i >= 0 {
		body = got[:i]
	}
	if !utf8.ValidString(body) {
		t.Fatal("service fragment body is not valid UTF-8 (cut mid-rune)")
	}
}

// TestRead_HugeLinePageServesFragment verifies the readPage service fragment
// (gopi/claude/codex consensus, absorbed from dsh/codex): a page whose first
// line alone exceeds the page budget is served as a rune-boundary cut within
// the budget plus the huge-line footer — never the old hard
// "one line is larger than the page limit" error and never bufio's raw
// "token too long".
func TestRead_HugeLinePageServesFragment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hugepage.go")
	// 60KB single line + a trailing newline so readImpl passes the avg guard
	// (file is <100KB, so the guard never fires anyway) and readPage runs.
	content := strings.Repeat("b", 60*1024) + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 0, 2)
	if err != nil {
		t.Fatalf("a page starting on a huge line must serve a fragment, not error: %v", err)
	}
	if len(got) > maxPageReadBytes+512 {
		t.Fatalf("service fragment not bounded by the page budget: %d bytes", len(got))
	}
	if !strings.Contains(got, "single-line huge file") || !strings.Contains(got, "bash(head/cut/xxd)") {
		t.Errorf("fragment missing the huge-line footer: tail=%q", got[len(got)-160:])
	}
	body := got
	if i := strings.Index(got, "\n\n[single-line"); i >= 0 {
		body = got[:i]
	}
	if !utf8.ValidString(body) {
		t.Fatal("fragment body is not valid UTF-8 (cut mid-rune)")
	}
	if body != strings.Repeat("b", maxPageReadBytes) {
		t.Errorf("fragment must fill the page window with the line head, got %d bytes", len(body))
	}
}

// TestRead_PageHugeLineMidFileFragment verifies the readPage service fragment
// when the huge line arrives AFTER normal lines: the page shows the earlier
// lines, then a rune-boundary cut of the huge line within the remaining page
// budget, closed with the huge-line footer.
func TestRead_PageHugeLineMidFileFragment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "midhuge.txt")
	var sb strings.Builder
	sb.WriteString("head line 1\nhead line 2\n")
	sb.WriteString(strings.Repeat("h", 60*1024) + "\n")
	sb.WriteString("tail line\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 0, 5)
	if err != nil {
		t.Fatalf("page with a mid-file huge line must serve a fragment, not error: %v", err)
	}
	if len(got) > maxPageReadBytes+512 {
		t.Fatalf("service fragment not bounded by the page budget: %d bytes", len(got))
	}
	if !strings.Contains(got, "head line 1") || !strings.Contains(got, "head line 2") {
		t.Errorf("earlier lines must be shown, got: %q", got[:120])
	}
	if !strings.Contains(got, "single-line huge file") || !strings.Contains(got, "bash(head/cut/xxd)") {
		t.Errorf("fragment missing the huge-line footer: tail=%q", got[len(got)-160:])
	}
	if i := strings.Index(got, "\n\n[single-line"); i >= 0 {
		if !utf8.ValidString(got[:i]) {
			t.Fatal("fragment body is not valid UTF-8 (cut mid-rune)")
		}
	}
}

// TestRead_PageErrTooLongDegradesToPartialContent verifies the ErrTooLong
// graceful degradation (dsh): when a line exceeds the maxPageLineBytes
// scanner cap, the page returns the lines scanned SO FAR plus the huge-line
// footer — never bufio's raw "token too long" and never a bare refusal. The
// file's line average stays below maxBytesPerLine so the readImpl trap guard
// does not fire and readPage runs.
func TestRead_PageErrTooLongDegradesToPartialContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "toolong.txt")
	var sb strings.Builder
	// 20 small lines keep the counted average under the trap threshold even
	// though countLines stops at the giant line (its own cap); the giant
	// line then must lie within the readPage window to be scanned.
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "small line %d\n", i)
	}
	sb.WriteString(strings.Repeat("g", 130*1024))
	sb.WriteString("\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&sb, "tail %d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 0, 25)
	if err != nil {
		t.Fatalf("ErrTooLong must degrade to partial content, not error: %v", err)
	}
	if !strings.Contains(got, "small line 0") {
		t.Errorf("lines scanned before the giant line must be returned, got head: %q", got[:100])
	}
	if !strings.Contains(got, "single-line huge file") || !strings.Contains(got, "bash(head/cut/xxd)") {
		t.Errorf("degraded page missing the huge-line footer: tail=%q", got[len(got)-160:])
	}
	if strings.Contains(got, "token too long") {
		t.Fatal("degraded page leaked bufio's raw 'token too long' error")
	}
}

// TestRead_HugeLineFragmentMidRune verifies the fragment cut lands on a true
// UTF-8 rune boundary when a multi-byte rune straddles the 48KB window end —
// a naive byte cut would leave mojibake. (The é-pair test above lands the cut
// exactly between runes; this one forces the rewind path.)
func TestRead_HugeLineFragmentMidRune(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "midrune.txt")
	// 界 is 3 bytes; it straddles the maxFullReadBytes cut.
	content := strings.Repeat("a", maxFullReadBytes-2) + "界" + strings.Repeat("a", 10*1024)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 0, 0)
	if err != nil {
		t.Fatalf("full read must serve a fragment, not error: %v", err)
	}
	body := got
	if i := strings.Index(got, "\n\n[single-line"); i >= 0 {
		body = got[:i]
	}
	if !utf8.ValidString(body) {
		t.Fatal("fragment body must be valid UTF-8 (cut at a rune boundary)")
	}
	if len(body) != maxFullReadBytes-2 {
		t.Errorf("fragment must stop at the rune start (48KB-2), got %d bytes", len(body))
	}
}

// TestRead_HugeFirstLineSkippableViaOffset verifies the 128KB scanner cap
// (R3-9): a file whose FIRST line is a giant run can still be read past via
// offset=1 — the scanner must pass over the huge line to reach the normal
// lines after it. This is what the fragment footer's "or offset/limit" hint
// promises.
func TestRead_HugeFirstLineSkippableViaOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hugefirst.go")
	var sb strings.Builder
	sb.WriteString(strings.Repeat("a", 60*1024) + "\n")
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&sb, "tail line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path, 1, 3)
	if err != nil {
		t.Fatalf("offset=1 must skip the huge first line: %v", err)
	}
	if !strings.Contains(got, "tail line 0") || !strings.Contains(got, "tail line 2") {
		t.Errorf("expected the tail lines after the huge first line, got: %q", got[:120])
	}
}
