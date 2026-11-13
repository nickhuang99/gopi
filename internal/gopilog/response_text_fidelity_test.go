package gopilog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The count relationship `gopi session log <id> --response --text` must satisfy, pinned
// so the "the replay loses text" reading (bug task 01a0ddcd-2b52-771f-8fea-2c3dc42b5b56)
// can never be re-argued as an assumption again.
//
// The bug report compared two DIFFERENT UNITS — the output's BYTES (`| wc -c`
// gave 4727) against the transcript's RUNES (python len() gave 3831) — and read
// the gap as truncation. For CJK one rune is 3 UTF-8 bytes, so the byte count is
// legitimately LARGER than the rune count; "4727 bytes vs 3831 chars" is that
// unit mismatch, and the arithmetic closes exactly:
//
//	Σ utf8bytes(first 3 blocks) = 4724  +3 wrapper newlines = 4727  ← wc -c
//	Σ runes   (first 3 blocks) = 3831                              ← python len
//
// So the pin checks BOTH units, each summed independently from the JSONL:
//
//	runes(FormatResponse(s, true)) == Σ runes(block_i) + N
//	bytes(FormatResponse(s, true)) == Σ utf8bytes(block_i) + N
//
// where N = one wrapper "\n" per block — the ONLY thing FormatResponse adds
// (formatter.go:345-347). A regression that drops a block, drops a line, or caps
// a block breaks the equality.

const (
	// responseTextWrapperPerBlock is the one and only wrapper FormatResponse
	// adds per assistant text block in textMode: a single "\n" after the text.
	responseTextWrapperPerBlock = 1
	// responseTextMidClauseRunes is a head fragment short enough to stand for
	// the bug report's "cut mid-clause" shape in the red-proof.
	responseTextMidClauseRunes = 20
	// responseTextOverCapRunes makes the long fixture block exceed
	// formatTextMaxLen by a healthy margin, so a rune cap is detectable.
	responseTextOverCapRunes = 25
)

// responseTextFidelityBlocks is the fixture's assistant text: CJK (3-byte
// runes, so runes and bytes diverge — the whole point), ASCII, a multi-line
// block, and one block LONGER than formatTextMaxLen so a rune cap of the kind
// FormatSession applies (formatter.go:108) cannot hide here.
var responseTextFidelityBlocks = []string{
	"第一段：中文内容按 rune 计数与按 byte 计数不同，这正是 4727 与 3831 的来源。",
	"Second block, ASCII only — here runes and bytes are equal.",
	"第三段第一行\n第三段第二行\n第三段第三行",
	strings.Repeat("长", formatTextMaxLen+responseTextOverCapRunes),
}

// TestResponseTextCountRelationshipMatchesTranscript pins the char/byte
// arithmetic of `--response --text` against the JSONL text blocks.
func TestResponseTextCountRelationshipMatchesTranscript(t *testing.T) {
	path, blocks := responseTextFidelityFixture(t, responseTextFidelityBlocks)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// The reader must not have DROPPED a block on the way in — the transcript
	// holds N text blocks, so the parsed session must hold N.
	gotBlocks := 0
	for _, iter := range s.Iterations {
		gotBlocks += len(iter.TextBlocks)
	}
	if gotBlocks != len(blocks) {
		t.Fatalf("parsed text blocks = %d, want %d (a reader that drops a block is the reported defect)",
			gotBlocks, len(blocks))
	}

	out := FormatResponse(s, true)
	wrapper := len(blocks) * responseTextWrapperPerBlock

	wantRunes := 0
	wantBytes := 0
	for _, b := range blocks {
		wantRunes += utf8.RuneCountInString(b)
		wantBytes += len(b)
	}

	gotRunes := utf8.RuneCountInString(out)
	gotBytes := len(out)

	if gotRunes != wantRunes+wrapper {
		t.Errorf("--response --text runes = %d, want %d (= %d transcript runes + %d wrapper newlines)\n"+
			"a shortfall here is REAL lost text — report which block it falls in",
			gotRunes, wantRunes+wrapper, wantRunes, wrapper)
	}
	if gotBytes != wantBytes+wrapper {
		t.Errorf("--response --text bytes = %d, want %d (= %d transcript bytes + %d wrapper newlines)",
			gotBytes, wantBytes+wrapper, wantBytes, wrapper)
	}

	// The two units must genuinely diverge on this fixture, or the test would
	// pass for the wrong reason (ASCII-only would make runes == bytes and hide
	// exactly the mix-up that produced the bug report).
	if wantBytes == wantRunes {
		t.Fatalf("fixture is degenerate: runes == bytes (%d) — it cannot distinguish the two units", wantRunes)
	}
}

// TestResponseTextBytesExceedRunesForCJK is the one-line falsification of the
// original claim, stated as an executable fact: a CJK session's replay BYTE
// count is legitimately LARGER than its transcript RUNE count, so "bytes > chars"
// is not evidence of anything.
func TestResponseTextBytesExceedRunesForCJK(t *testing.T) {
	path, blocks := responseTextFidelityFixture(t, []string{responseTextFidelityBlocks[0]})

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	out := FormatResponse(s, true)

	if len(out) <= utf8.RuneCountInString(strings.Join(blocks, "")) {
		t.Errorf("bytes=%d runes=%d — expected bytes > runes on CJK input (the unit mix-up the bug report made)",
			len(out), utf8.RuneCountInString(out))
	}
}

// TestResponseTextPreservesEveryBlockWhole is the red-proof: the SAME predicate
// the tests above use, applied to the truncation shapes the bug report described
// (a block cut mid-sentence, a rune cap, a dropped block), must FAIL — and the
// mixed-unit comparison the report actually made must be shown to be the false
// alarm itself. Without this a test that always passes proves nothing.
func TestResponseTextPreservesEveryBlockWhole(t *testing.T) {
	path, blocks := responseTextFidelityFixture(t, responseTextFidelityBlocks)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	want := FormatResponse(s, true)

	// Shape 1: the reported "cut mid-clause" — the last block loses its tail.
	truncated := strings.Replace(want, responseTextFidelityBlocks[3],
		responseTextFidelityBlocks[3][:responseTextMidClauseRunes], 1)
	if !responseTextCountBroken(truncated, blocks) {
		t.Error("tail truncation went UNDETECTED — the fidelity pin has no teeth")
	}

	// Shape 2: a rune cap of the kind FormatSession applies (formatter.go:108).
	capped := strings.Replace(want, responseTextFidelityBlocks[3],
		truncateRunes(responseTextFidelityBlocks[3], formatTextMaxLen), 1)
	if !responseTextCountBroken(capped, blocks) {
		t.Error("rune cap went UNDETECTED — the fidelity pin has no teeth")
	}

	// Shape 3: a whole block dropped.
	dropped := strings.Replace(want, responseTextFidelityBlocks[3]+"\n", "", 1)
	if !responseTextCountBroken(dropped, blocks) {
		t.Error("dropped block went UNDETECTED — the fidelity pin has no teeth")
	}

	// The intact output must NOT be flagged (the predicate is not trivially true).
	if responseTextCountBroken(want, blocks) {
		t.Error("intact --response --text output was flagged as lossy")
	}

	// Shape 4: THE COMPARISON THE BUG REPORT ACTUALLY MADE — the output's BYTES
	// (| wc -c) against the transcript's RUNES (python len of the block text).
	// Applied to INTACT output it must report a mismatch, because that mixed-unit
	// comparison IS the false alarm: 4727 bytes counted against 3831 runes read
	// as a shortfall that was never there.
	if !responseTextMixedUnitBroken(want, blocks) {
		t.Error("the bug report's bytes-vs-runes comparison was NOT detected as broken — " +
			"without this the unit mix-up could be re-reported as a live defect")
	}
}

// responseTextCountBroken reports whether out fails the pinned count
// relationship against blocks — the shared predicate of the tests above.
func responseTextCountBroken(out string, blocks []string) bool {
	want := len(blocks) * responseTextWrapperPerBlock
	for _, b := range blocks {
		want += utf8.RuneCountInString(b)
	}
	return utf8.RuneCountInString(out) != want
}

// responseTextMixedUnitBroken reports whether the MIXED-UNIT comparison the bug
// report made — the output's BYTES against the transcript's RUNES — disagrees.
// It returns true when the two disagree, i.e. when the mixed-unit comparison
// would (wrongly) report loss. On CJK input it is true for INTACT output, which
// is precisely the false alarm: bytes > runes is a property of UTF-8, not a
// defect.
func responseTextMixedUnitBroken(out string, blocks []string) bool {
	want := 0
	for _, b := range blocks {
		want += utf8.RuneCountInString(b)
	}
	return len(out) != want
}

// responseTextFidelityFixture writes a session JSONL whose assistant messages
// carry blocks, and returns the file path plus the blocks VERBATIM as the
// transcript holds them (the expectation is computed from the raw text, never
// from what the reader produced — otherwise the test would be tautological).
func responseTextFidelityFixture(t *testing.T, blocks []string) (string, []string) {
	t.Helper()

	lines := []string{
		`{"model":"deepseek-flash","sessionId":"response-text-fidelity","timestamp":"2026-09-26T20:00:00.000000000+08:00","type":"session_start","workdir":"/home/nick/work/GoPi"}`,
	}
	for i, b := range blocks {
		raw, err := json.Marshal(map[string]any{
			"message": map[string]any{
				"content": []map[string]any{{"text": b, "type": "text"}},
				"role":    "assistant",
			},
			"timestamp": fmt.Sprintf("2026-09-26T20:00:%02d.000000000+08:00", i+1),
			"type":      "assistant",
		})
		if err != nil {
			t.Fatalf("marshal assistant block %d: %v", i, err)
		}
		lines = append(lines, string(raw))
		// A request event flushes the open iteration, so the blocks land in
		// distinct iterations and the multi-iteration path is exercised too.
		lines = append(lines, fmt.Sprintf(
			`{"cache_hit":1,"cache_miss":1,"completion_tokens":1,"iteration":%d,"prompt_tokens":1,"timestamp":"2026-09-26T20:01:%02d.000000000+08:00","type":"request"}`,
			i, i+1))
	}
	lines = append(lines,
		`{"elapsed_sec":1.0,"exit_code":0,"total_iterations":1,"total_tokens":1,"timestamp":"2026-09-26T20:02:00.000000000+08:00","type":"session_end"}`)

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path, blocks
}
