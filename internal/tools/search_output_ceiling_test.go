package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the regression guard for the reading ceiling the ripgrep wrapper
// now has (searchOutputByteCeiling, search.go). Before it, the wrapper read the
// backend's ENTIRE stdout into memory with cmd.CombinedOutput() and parsed it
// afterwards, so peak memory was O(sum of matching line lengths) with no upper
// bound at all — measured on this host: 38.9 MB of output for query "the" over
// one session file, and a run killed at 11.4 GB RSS for query "tool_result".
//
// The four facts pinned here, in order of how expensive they were to learn:
//  1. readBackendStdout stops at the ceiling and says so, having read no more
//     than the ceiling plus one line (the bound is a CONSTANT, not a data size);
//  2. the real ripgrep leg stops, KILLS the child and returns the partial hits
//     with a SearchCap that names the cut — through the same code path
//     production uses (a small ceiling is passed instead of the 64 MiB one,
//     because a test cannot write 64 MiB of fixture and a bound nobody can test
//     is a bound nobody can trust);
//  3. an ordinary query is byte-identical to what it was: Stopped is false and
//     the renderer's output equals FormatTextHits verbatim;
//  4. a truncated result is NEVER rendered as a complete one — a truncated read
//     that kept zero hits must not print "No matches." (Rule 15).

// TestReadBackendStdoutStopsAtTheCeiling drives the reading loop directly with a
// synthetic stream, so the assertion is about the LOOP and not about rg's
// behaviour on this host.
func TestReadBackendStdoutStopsAtTheCeiling(t *testing.T) {
	const lineBytes = 100
	const ceiling = 1000 // ten lines
	var b strings.Builder
	for i := 0; i < 500; i++ { // 50 KB of output against a 1 KB ceiling
		fmt.Fprintf(&b, "f.go:%d:1:%s\n", i+1, strings.Repeat("x", lineBytes-14))
	}

	hits, matched, read, stopped, _, _, err := readBackendStdout(strings.NewReader(b.String()), 0, ceiling)
	if err != nil {
		t.Fatalf("readBackendStdout: %v", err)
	}
	if !stopped {
		t.Fatalf("stopped=false with %d bytes of output against a %d-byte ceiling", b.Len(), ceiling)
	}
	// The bound is what matters: the reader must not have consumed the stream.
	// One line of slack is inherent (a line is measured once it has been read).
	if read > ceiling+lineBytes {
		t.Errorf("read %d bytes against a %d-byte ceiling (+%d line) — the bound is not a constant",
			read, ceiling, lineBytes)
	}
	if matched == 0 || len(hits) == 0 {
		t.Fatalf("a truncated read must still return the matches it saw: matched=%d hits=%d", matched, len(hits))
	}
	if len(hits) >= 500 {
		t.Fatalf("truncated read returned all %d matches — it kept reading past the ceiling", len(hits))
	}

	// The same loop, unlimited, must see everything — proving the stop above came
	// from the ceiling and not from the stream ending early.
	_, matchedAll, readAll, stoppedAll, _, _, err := readBackendStdout(strings.NewReader(b.String()), 0, 0)
	if err != nil {
		t.Fatalf("readBackendStdout (unlimited): %v", err)
	}
	if stoppedAll {
		t.Error("stopped=true with byteCeiling=0 — 0 means unlimited")
	}
	if matchedAll != 500 {
		t.Errorf("unlimited read matched %d, want 500", matchedAll)
	}
	if readAll != int64(b.Len()) {
		t.Errorf("unlimited read read %d bytes, want %d", readAll, b.Len())
	}
}

// TestReadBackendStdoutCountsBeyondTheResultCap pins that the ceiling change did
// NOT alter what `dropped` means: matches past scope.MaxResult are still COUNTED
// (the renderer prints "plus N more matches", R3-8) while only MaxResult of them
// are HELD.
func TestReadBackendStdoutCountsBeyondTheResultCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "f.go:%d:1:needle\n", i+1)
	}
	hits, matched, _, stopped, _, _, err := readBackendStdout(strings.NewReader(b.String()), 5, 0)
	if err != nil {
		t.Fatalf("readBackendStdout: %v", err)
	}
	if stopped {
		t.Fatal("stopped=true with no byte ceiling")
	}
	if len(hits) != 5 {
		t.Errorf("hits = %d, want 5 (the result cap)", len(hits))
	}
	if matched != 30 {
		t.Errorf("matched = %d, want 30 — matches past the cap must still be counted", matched)
	}
}

// TestKeywordSearchTruncatesALargeResultAndSaysSo runs the REAL ripgrep leg over
// a fixture that produces far more output than the ceiling it is given, and
// asserts the three things a caller needs: partial hits came back, SearchCap says
// the read was cut, and the notice is rendered.
func TestKeywordSearchTruncatesALargeResultAndSaysSo(t *testing.T) {
	if !rgAvailable() {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	// 4000 matching lines of ~60 bytes ≈ 240 KB of rg output.
	var b strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&b, "ceiling-needle line %04d %s\n", i, strings.Repeat("y", 30))
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := SearchScope{RootPath: dir, MaxResult: 0}

	const testCeiling = 4096 // a few KB stand in for the production 64 MiB
	hits, _, searchCap, err := keywordSearchViaRipgrepCapped(context.Background(), scope, "ceiling-needle", testCeiling)
	if err != nil {
		t.Fatalf("keywordSearchViaRipgrepCapped: %v", err)
	}
	if !searchCap.Stopped {
		t.Fatalf("SearchCap.Stopped=false over ~240 KB of output against a %d-byte ceiling", testCeiling)
	}
	if searchCap.MaxBytes != testCeiling {
		t.Errorf("SearchCap.MaxBytes = %d, want %d", searchCap.MaxBytes, testCeiling)
	}
	if searchCap.Bytes > testCeiling+4096 {
		t.Errorf("SearchCap.Bytes = %d — more than the ceiling plus one line", searchCap.Bytes)
	}
	if len(hits) == 0 {
		t.Fatal("a truncated read returned NO hits — the partial answer is the point")
	}
	if len(hits) >= 4000 {
		t.Fatalf("truncated read returned all %d matches — nothing was cut", len(hits))
	}
	if searchCap.Kept != len(hits) {
		t.Errorf("SearchCap.Kept = %d, want %d (the hits the caller holds)", searchCap.Kept, len(hits))
	}

	// The rendered result must carry the truncation — never a bare list that
	// reads like a complete answer (Rule 15).
	rendered := FormatTextHitsWithCap(hits, 0, searchCap)
	if !strings.Contains(rendered, "truncated") {
		t.Errorf("rendered result of a truncated search carries no truncation notice:\n%s", rendered)
	}
	if !strings.Contains(rendered, "narrow the query") {
		t.Errorf("the notice does not tell the caller what to do next:\n%s", rendered)
	}
}

// TestKeywordSearchOrdinaryQueryIsUnchanged is the other half of the contract: a
// query that does NOT hit the ceiling behaves exactly as before. The comparison
// is byte-level against the pre-existing renderer, which is what "normal queries
// are unchanged" has to mean.
func TestKeywordSearchOrdinaryQueryIsUnchanged(t *testing.T) {
	if !rgAvailable() {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	src := "package demo\n\nfunc Alpha() {}\nfunc Beta() { Alpha() }\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := SearchScope{RootPath: dir, MaxResult: 50}

	hits, dropped, searchCap, err := KeywordSearch(scope, "Alpha")
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}
	if searchCap.Stopped {
		t.Fatalf("an ordinary 2-match query reported truncation: %+v", searchCap)
	}
	if searchCap.Bytes == 0 {
		t.Error("SearchCap.Bytes = 0 — the byte count is reported even when nothing was cut")
	}
	if len(hits) != 2 || dropped != 0 {
		t.Fatalf("hits=%d dropped=%d, want 2 and 0", len(hits), dropped)
	}
	// Byte-identical to the pre-existing renderer: that IS "unchanged".
	if got, want := FormatTextHitsWithCap(hits, dropped, searchCap), FormatTextHits(hits, dropped); got != want {
		t.Errorf("an untruncated result rendered differently:\n got: %q\nwant: %q", got, want)
	}
}

// TestTruncatedEmptyResultIsNotNoMatches is the case the notice exists for: a
// read cut short before its first parsable line kept not a single hit, and
// "No matches." would be a LIE (the query never reached the end of the data).
func TestTruncatedEmptyResultIsNotNoMatches(t *testing.T) {
	cut := SearchCap{Bytes: 64 << 20, MaxBytes: 64 << 20, Stopped: true}
	got := FormatTextHitsWithCap(nil, 0, cut)
	if strings.Contains(got, "No matches.") {
		t.Errorf("a truncated read with zero hits claims \"No matches.\":\n%s", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("no truncation notice on an empty truncated result:\n%s", got)
	}

	sym := FormatCodeSymbolHitsWithCap("SomeSymbol", nil, 0, cut)
	if strings.Contains(sym, `No symbol "SomeSymbol" found`) {
		t.Errorf("a truncated code_search read claims the symbol does not exist:\n%s", sym)
	}
	if !strings.Contains(sym, "truncated") {
		t.Errorf("no truncation notice on an empty truncated code_search result:\n%s", sym)
	}
}

// TestFormatSearchCapIsSilentWhenNothingWasCut — the notice must not appear in
// the ordinary case, or every search result grows a line that means nothing.
func TestFormatSearchCapIsSilentWhenNothingWasCut(t *testing.T) {
	if got := FormatSearchCap(SearchCap{Bytes: 1024}); got != "" {
		t.Errorf("FormatSearchCap(not stopped) = %q, want empty", got)
	}
	if got := FormatSearchCap(SearchCap{}); got != "" {
		t.Errorf("FormatSearchCap(zero) = %q, want empty", got)
	}
}
