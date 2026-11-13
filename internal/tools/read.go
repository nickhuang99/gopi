// Package tools implements the tool functions (read, write, edit, bash) used by
// the GoPi coding agent's ReAct loop.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

// maxFullReadBytes caps a full read (offset=0, limit=0): files larger than
// this are returned truncated at a line boundary with a footer naming the
// total line count and the next offset (R3-1). Root cause (gopi-race R5-R6
// post-mortem): a full read of a 74KB source file injected ~18K tokens into
// the context in one tool result, ballooning the next request's prefill
// (12K → 62K prompt tokens) and pushing rounds from ~10s to 160-600s of API
// time.
const maxFullReadBytes = 48 * 1024

// maxPageReadBytes caps ANY single read window — partial (offset/limit) or
// full — at ~50KB, truncated at a line boundary with a footer when crossed.
// R3-3/R3-4: partial reads used to be line-count-only, so a window of 2000
// long lines returned 86-90KB (R2 A/B measurement), and the old >100KB full
// guard was bypassable with read(offset=0, limit=100000) — one call injected
// 742KB into the context. The budget applies to every page regardless of
// offset/limit values, so it cannot be bypassed.
const maxPageReadBytes = 50 * 1024

// hugeLineGuardBytes is the file size above which the single-line trap guard
// applies: when a file this large averages more than maxBytesPerLine per
// line, even a windowed read cannot return a usable page (a 48KB window may
// land mid-line), so read rejects it with a bash(head/cut) hint. The old
// hard rejection of ALL >100KB full reads is gone (R3-2) — only this trap
// keeps a hard error.
const hugeLineGuardBytes = 100 * 1024

// maxBytesPerLine is the average-line-size threshold for the single-line
// trap guard (see hugeLineGuardBytes).
const maxBytesPerLine = 10 * 1024

// maxPageLineBytes is the scanner token cap for readPage and countLines: a
// single line larger than this would trip bufio.Scanner's default 64KB token
// limit and surface the bare "token too long" error to the model (R3-9).
// 128KB deliberately exceeds both that default and the 100KB
// single-line-trap threshold, so ANY line of a file that reaches readPage
// (≤100KB files escape the trap guard) scans — including a giant first line
// that offset/limit must pass over to reach the normal lines after it (the
// service-fragment footer's "or offset/limit" promise). A line beyond the cap
// is mapped onto the clean single-line bash hint instead of the raw bufio
// error. The page budget (maxPageReadBytes) still governs what is SERVED —
// this cap only governs what can be scanned.
const maxPageLineBytes = 128 * 1024

// scannerInitBytes is the initial buffer for the file scanners. Kept below
// maxPageLineBytes (bufio.Scanner.Buffer requires len(buf) <= max).
const scannerInitBytes = 64 * 1024

// maxBinarySampleBytes is the sample size for binary detection: the first 8KB
// of a file are enough to catch NUL bytes or invalid UTF-8 in any real binary
// (headers, embedded data), while keeping the check cheap on multi-MB files.
const maxBinarySampleBytes = 8 * 1024

// utf8ContinuationByte is the pattern byte used to pad an incomplete
// trailing rune when proving it is a sliced rune (isIncompleteRunePrefix):
// 0x80 is a valid UTF-8 continuation byte for every multi-byte form, so a
// padded sequence decodes when — and only when — the tail was a genuine
// truncated rune.
const utf8ContinuationByte = 0x80

// symlinkHintFormat is the hint line appended to a read of a symlink path:
// the read follows the link (reading a symlink = reading its target is the
// correct behavior), and the line tells the model the link exists and where
// it points — so an edit/write on the path can account for the link (the
// 2026-08-28 settings.json incident: write/edit renamed over the link node,
// corrupting the link while the target kept its old content). Rendered with
// English brackets in the existing footer style, placed after the content
// and before any truncation footer.
const symlinkHintFormat = "\n[symlink: %s -> %s]\n"

// Read reads the content of the file at path and returns it as a string.
// It supports absolute paths, relative paths (resolved against the current
// working directory), and ~/ paths (expanded to the user's home directory).
// Path must not be empty and must not point to a directory.
//
// offset and limit control partial reads for large files:
//   - offset=0, limit=0 → read the entire file (backwards compatible)
//   - offset>0 → skip the first offset lines (1-indexed line counting)
//   - limit>0 → read at most limit lines
//
// Read is protected by a timeout (default 30s, GOPI_TOOL_TIMEOUT_SEC).
// On timeout the operation returns an error; the underlying goroutine may
// continue but the caller is unblocked.
func Read(path string, offset int, limit int) (string, error) {
	return readWithTimeout(path, offset, limit, FileToolTimeout())
}

// readWithTimeout wraps the file read in a goroutine+channel with the given timeout.
func readWithTimeout(path string, offset int, limit int, timeout time.Duration) (string, error) {
	return withTimeout(timeout,
		fmt.Errorf("read: %s: timed out after %s", path, timeout),
		func(context.Context) (string, error) { return readImpl(path, offset, limit) })
}

// readImpl contains the actual file-read logic (unchanged, extracted from Read).
func readImpl(path string, offset int, limit int) (string, error) {
	if path == "" {
		return "", fmt.Errorf("read: path must not be empty")
	}

	// Expand ~ or ~/ to the user's home directory.
	var err error
	path, err = expandHome(path)
	if err != nil {
		return "", fmt.Errorf("read: failed to get home directory: %w", err)
	}

	// Resolve to an absolute path for consistent error messages.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("read: failed to resolve path: %w", err)
	}

	// Stat to check existence and whether it is a directory.
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read: %s: is a directory", absPath)
	}

	// Symlink detection via os.Lstat (never follows the link): when the path
	// is itself a symlink the read still follows it (reading a symlink =
	// reading its target), but the returned content is annotated with the
	// link target so the model knows the path is a link before editing or
	// writing it — a plain rename onto the link path would corrupt the link
	// and leave the target unchanged (the 2026-08-28 settings.json incident).
	var symlinkHint string
	if linkInfo, err := os.Lstat(absPath); err == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(absPath); err == nil {
			symlinkHint = fmt.Sprintf(symlinkHintFormat, absPath, target)
		}
	}

	// Single-line trap guard: a file whose lines average more than
	// maxBytesPerLine is unreadable in any window (even a 48KB page can land
	// mid-line), so read rejects it with a bash hint. This is the ONLY hard
	// rejection left — the old block of all >100KB full reads is replaced by
	// streaming the first maxFullReadBytes with a footer (R3-2; the R2 A/B
	// measured the block's cost at two wasted rounds: bash wc -l + a failed
	// read).
	var totalLines = -1 // -1 = not counted yet (counted only when needed)
	if info.Size() > hugeLineGuardBytes {
		totalLines = countLines(absPath)
		avgBytes := 0
		if totalLines > 0 {
			avgBytes = int(info.Size()) / totalLines
		}
		if avgBytes > maxBytesPerLine {
			return "", fmt.Errorf(
				"read: %s is too large (%d bytes, %d lines, ~%d bytes/line). "+
					"Even one line is huge — use bash(head/cut) instead of read().",
				absPath, info.Size(), totalLines, avgBytes,
			)
		}
	}

	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	defer f.Close()

	// Binary detection: sample the first 8KB for NUL bytes or invalid UTF-8.
	// Reading a binary file returns mojibake that pollutes the context, so it
	// is rejected with a hint to inspect the file via bash instead. The sample
	// read is rewound so the line-by-line path below starts at the beginning.
	sample := make([]byte, maxBinarySampleBytes)
	n, err := f.Read(sample)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read: %w", err)
	}
	if err := checkBinary(absPath, sample[:n]); err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	// offset=0 and limit=0 → read the whole file, streaming only the first
	// maxFullReadBytes (R3-2: the old path read the entire file into memory
	// and hard-rejected everything above 100KB; streaming reads a single
	// window, so multi-MB files cost nothing extra and never block).
	if offset == 0 && limit == 0 {
		return readFull(f, absPath, totalLines, symlinkHint)
	}

	// Line-by-line reading with offset and limit, bounded by maxPageReadBytes.
	return readPage(f, absPath, offset, limit, totalLines, symlinkHint)
}

// readPage reads one window of a file for an offset/limit (partial) read.
// The window is bounded by BOTH the requested line count and
// maxPageReadBytes: when the byte budget is crossed the page is truncated at
// a line boundary and closed with a footer (R3-3 — the R2 A/B measured
// single windows of 86-90KB under the old line-only cap). A page whose first
// line alone exceeds the budget is no longer a hard error (gopi/claude/codex
// consensus, absorbed from dsh/codex): the line is served as a rune-boundary
// fragment within the budget, closed with the huge-line footer — the model
// gets a usable prefix plus the bash(head/cut/xxd) hint.
func readPage(f *os.File, path string, offset, limit, totalLines int, symlinkHint string) (string, error) {
	var buf bytes.Buffer
	scanner := bufio.NewScanner(f)
	// R3 patch: a giant line must not surface bufio's raw "token too long"
	// error. maxPageLineBytes exceeds the default 64KB cap, so lines that
	// once tripped it now scan (and are judged by the page budget); anything
	// beyond it degrades to the shown lines plus a footer below.
	scanner.Buffer(make([]byte, 0, scannerInitBytes), maxPageLineBytes)
	lineNum, shown := 0, 0
	budgetHit := false
	var hugeLine []byte // the line that alone crossed the page budget (served as a fragment)
	for scanner.Scan() {
		lineNum++
		if offset > 0 && lineNum <= offset {
			continue
		}
		if limit > 0 && shown >= limit {
			break
		}
		line := scanner.Bytes()
		start := buf.Len()
		if start > 0 {
			buf.WriteByte('\n')
		}
		buf.Write(line)
		shown++
		if buf.Len() > maxPageReadBytes {
			buf.Truncate(start) // line boundary — drop the line that crossed the budget
			shown--
			budgetHit = true
			if len(line) > maxPageReadBytes {
				hugeLine = line // the line alone exceeds the budget
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		if err != bufio.ErrTooLong {
			return "", fmt.Errorf("read: %w", err)
		}
		// ErrTooLong graceful degradation (dsh): a line beyond the scanner
		// cap stopped the scan; the partial token is not retrievable
		// (scanner.Bytes is nil after an error), so the page returns the
		// lines shown SO FAR plus the huge-line footer — never bufio's raw
		// "token too long", never a bare refusal.
		if totalLines < 0 {
			totalLines = countLines(path)
		}
		// Symlink hint goes before the footer (content → hint → footer).
		return buf.String() + symlinkHint + hugeLineFooter(totalLines, maxPageLineBytes), nil
	}
	if hugeLine != nil {
		// Service fragment: the huge line cut at a rune boundary within the
		// remaining page budget, closed with the huge-line footer. The old
		// behavior was a hard "one line is larger than the page limit"
		// error.
		room := maxPageReadBytes - buf.Len()
		if room > 0 {
			snip := hugeLine
			if len(snip) > room {
				snip = snip[:room]
			}
			snip = []byte(runeSafePrefix(snip))
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.Write(snip)
		}
		if totalLines < 0 {
			totalLines = countLines(path)
		}
		return buf.String() + symlinkHint + hugeLineFooter(totalLines, maxPageReadBytes), nil
	}
	if budgetHit {
		if totalLines < 0 {
			totalLines = countLines(path)
		}
		return buf.String() + symlinkHint + readFooter(totalLines, offset+1, offset+shown), nil
	}
	return buf.String() + symlinkHint, nil
}

// readFull streams the head of a file for a full read (offset=0, limit=0).
// Files up to maxFullReadBytes are returned whole; larger files return the
// first maxFullReadBytes at a line boundary with a footer naming the total
// line count and the next offset. Only maxFullReadBytes+1 bytes are read, so
// streaming a 100MB+ file costs nothing extra. A window with no line break at
// all (a single huge line) returns a service fragment — the first
// maxFullReadBytes cut at a rune boundary plus a footer telling the model the
// file is one giant line and to use bash(head/cut/xxd) or offset/limit to page
// it — instead of erroring (R3 patch: previously this fell back to the old
// bare "no line break" error). symlinkHint ("" for non-symlink paths) is
// inserted after the content and before any truncation footer.
func readFull(f *os.File, path string, totalLines int, symlinkHint string) (string, error) {
	buf := make([]byte, maxFullReadBytes+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read: %w", err)
	}
	if n <= maxFullReadBytes {
		return string(buf[:n]) + symlinkHint, nil // whole file fit in one window
	}
	cut := maxFullReadBytes
	if i := bytes.LastIndexByte(buf[:maxFullReadBytes], '\n'); i >= 0 {
		cut = i // line boundary — never split a line mid-way
		if totalLines < 0 {
			totalLines = countLines(path)
		}
		// The newline at cut terminates the last shown line, so the shown count
		// is the number of newlines in buf[:cut] plus that final line.
		shown := bytes.Count(buf[:cut], []byte{'\n'}) + 1
		return string(buf[:cut]) + symlinkHint + readFooter(totalLines, 1, shown), nil
	}
	// Single huge line: serve the first maxFullReadBytes at a rune boundary,
	// with a fragment footer that points the model at bash/xxd or offset/limit
	// (the file's first "line" is the whole >48KB run, so page-offset reads
	// via read() cannot isolate it either).
	if totalLines < 0 {
		totalLines = countLines(path)
	}
	frag := runeSafePrefix(buf[:cut])
	return frag + symlinkHint + hugeLineFooter(totalLines, maxFullReadBytes), nil
}

// runeSafePrefix returns frag truncated to a valid UTF-8 string: if the byte
// at the cut lands mid-rune (or the cut slices off a partial multi-byte rune
// whose lead byte lands as the final byte), back up to the previous complete
// rune so the returned fragment is never mojibake. Valid UTF-8 guarantees a
// boundary within UTFMax bytes of the cut, so the fragment stays near the cap.
func runeSafePrefix(frag []byte) string {
	if utf8.Valid(frag) {
		return string(frag)
	}
	// Not valid at the cut — rewind up to UTFMax bytes to a valid prefix.
	for i := len(frag) - 1; i > len(frag)-utf8.UTFMax; i-- {
		if i <= 0 {
			break
		}
		if utf8.Valid(frag[:i]) {
			return string(frag[:i])
		}
	}
	// Defensive fallback (shouldn't be reachable for valid UTF-8): full prefix.
	return string(frag)
}

// hugeLineFooter closes a single-huge-line service fragment: it names the total
// line count and points the model at bash/xxd or offset/limit paging, because
// read() cannot isolate one oversized line inside a normal offset/limit
// window. windowBytes is the read window the line exceeded: the full-read
// budget (maxFullReadBytes) for readFull fragments, the page budget
// (maxPageReadBytes) or scanner cap (maxPageLineBytes) for readPage ones.
func hugeLineFooter(totalLines, windowBytes int) string {
	return fmt.Sprintf(
		"\n\n[single-line huge file（单行巨行）: %d line(s) total — one line exceeds the %d-byte read window; use bash(head/cut/xxd) or offset/limit to page it]",
		totalLines, windowBytes)
}

// readFooter closes a truncated read with the facts the model needs to
// continue without guessing (R3-1, mirroring dsh's read footer): the total
// line count, the range shown, and the next offset. gopi's offset semantics
// ("offset=N skips the first N lines") differ from dsh's ("start at line N")
// — the hint restates the semantics because a model trained on the dsh
// convention silently skips one line otherwise (R2 A/B footgun; the read
// tool schema carries the same note).
func readFooter(totalLines, from, to int) string {
	return fmt.Sprintf(
		"\n\n[read truncated: file has %d lines total, showing %d-%d — continue with offset=%d (offset=N skips the first N lines)]",
		totalLines, from, to, to)
}

// countLines counts lines in a file for the safety guard.
func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Same scanner cap as readPage: a line the page reader can scan must also
	// be counted, or the trap guard and the service-fragment footer misjudge
	// a huge-line file's line count (R3-9).
	scanner.Buffer(make([]byte, 0, scannerInitBytes), maxPageLineBytes)
	n := 0
	for scanner.Scan() {
		n++
	}
	return n
}

// checkBinary rejects sampled content that looks binary: a NUL byte or
// invalid UTF-8 in the first 8KB marks a file as non-text. The error message
// mirrors the "too large" hint style: it states the problem and points the
// model at bash('file') / bash('xxd') as the right inspection tools.
//
// The 8KB sample boundary is a byte offset, not a rune boundary, so a
// text file whose byte maxBinarySampleBytes lands mid-rune (systematically:
// CJK 3-byte runes, ~1/3 of such files) ends the sample with an incomplete
// rune. That trailing fragment says nothing about the file — it is an
// artifact of where the window was cut — so it is trimmed before
// utf8.Valid (trimIncompleteTrailingRune). An INTERIOR invalid byte (or
// NUL) is still rejected: only the final, provably incomplete rune is
// excused, and the NUL rule is checked on the raw sample, unchanged.
func checkBinary(path string, sample []byte) error {
	if bytes.IndexByte(sample, 0) >= 0 {
		return fmt.Errorf(
			"read: %s is a binary file（检测到 NUL 字节）— 用 bash('file %s') 或 bash('xxd %s | head') 检查，不要 read",
			path, path, path)
	}
	if !utf8.Valid(trimIncompleteTrailingRune(sample)) {
		return fmt.Errorf(
			"read: %s is a binary file（检测到无效 UTF-8）— 用 bash('file %s') 或 bash('xxd %s | head') 检查，不要 read",
			path, path, path)
	}
	return nil
}

// trimIncompleteTrailingRune drops a trailing incomplete UTF-8 rune from a
// byte-window sample and returns the rest. A window cut at an arbitrary byte
// offset can slice a multi-byte rune in half; that fragment is an artifact of
// where the sample ended, not evidence of binary content, so it must not fail
// utf8.Valid. Interior invalid bytes survive untouched — the trim only ever
// removes bytes that form a proper prefix of ONE multi-byte rune (at most
// utf8.UTFMax-1 of them), so any malformed byte before the cut still makes the
// returned sample invalid and the file is rejected as before.
//
// A bytes.ToValidUTF8-style repair is deliberately NOT used: rewriting or
// dropping an INTERIOR invalid byte would defeat detection entirely.
func trimIncompleteTrailingRune(sample []byte) []byte {
	// Fast path: the sample already ends on a rune boundary — every pure-ASCII
	// file and the ~2/3 of CJK windows whose cut lands on a boundary.
	if utf8.Valid(sample) {
		return sample
	}
	// Only the final rune can have been sliced by the window, so candidates
	// shorter than the sample are the last utf8.UTFMax-1 bytes at most. The
	// first candidate (longest prefix) that is valid UTF-8 AND whose dropped
	// tail is an incomplete rune is the answer; once a valid prefix is found
	// with a non-rune tail, no longer trim can be excusable (the extra byte
	// was already part of a valid rune), so the loop can stop.
	for back := 1; back < utf8.UTFMax && back <= len(sample); back++ {
		prefix := sample[:len(sample)-back]
		if !utf8.Valid(prefix) {
			continue
		}
		if isIncompleteRunePrefix(sample[len(sample)-back:]) {
			return prefix
		}
		return sample
	}
	return sample
}

// isIncompleteRunePrefix reports whether tail is the leading bytes of exactly
// one multi-byte rune cut short — the ONLY trailing shape
// trimIncompleteTrailingRune may excuse. It pads tail with UTF-8 continuation
// bytes: if the completed sequence decodes to a valid rune STRICTLY LONGER
// than tail, the tail was a truncated rune. Otherwise (bare 0xFF, NUL, lone
// continuation byte, or a complete rune) it is not a sliced rune and the
// caller must keep rejecting the sample.
func isIncompleteRunePrefix(tail []byte) bool {
	if len(tail) == 0 || len(tail) >= utf8.UTFMax {
		return false
	}
	padded := make([]byte, utf8.UTFMax)
	copy(padded, tail)
	for i := len(tail); i < utf8.UTFMax; i++ {
		padded[i] = utf8ContinuationByte
	}
	r, size := utf8.DecodeRune(padded)
	return r != utf8.RuneError && size > len(tail)
}
