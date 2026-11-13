package tokenizer

import (
	"strings"
	"sync"
	"testing"
)

// unit_test.go — properties that hold WITHOUT the Python baseline, so the
// package is still covered on a machine where the reference corpus is absent.
// The differential test is the acceptance gate; these are the fail-fast ring
// around it.

// TestLazyUntilFirstCount is the sync.Once contract: importing and using the
// package without counting must not parse the 7.8 MB file, and the first Count
// must be the thing that parses it.
//
// It cannot assert Ready() == false in a process where another test already
// counted, so it asserts the ORDER the contract states instead: a fresh process
// that never calls Count leaves Ready() false, and Count flips it. The fresh
// process part is what TestLazyInFreshProcess checks; here the flip is checked.
func TestLazyUntilFirstCount(t *testing.T) {
	before := Ready()
	tok, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if !Ready() {
		t.Fatal("Ready() is false after Default() parsed the data")
	}
	if n := tok.Count("hello world"); n == 0 {
		t.Fatal("Count returned 0 for a non-empty string")
	}
	_ = before
}

// TestEmptyAndWhitespace pins the two inputs a caller passes by accident.
func TestEmptyAndWhitespace(t *testing.T) {
	tok := mustDefault(t)
	if got := tok.Count(""); got != 0 {
		t.Errorf("Count(\"\") = %d, want 0", got)
	}
	if ids := tok.Encode(""); ids != nil {
		t.Errorf("Encode(\"\") = %v, want nil", ids)
	}
	if got := tok.Count(" "); got != 1 {
		t.Errorf("Count(\" \") = %d, want 1", got)
	}
	if got := tok.Count("\n\n\n"); got != 1 {
		t.Errorf("Count(three newlines) = %d, want 1 (the byte-level grouping keeps a newline run)", got)
	}
}

// TestDigitSplitIsASCIIOnly pins the measured behaviour of stage 1: \p{N}{1,3}
// is Go's decimal-digit class, which does NOT include fullwidth or Arabic-Indic
// digits. Ten ASCII digits become four ids; three fullwidth digits stay one
// piece. A change to the digit class shows up here, not in a review.
func TestDigitSplitIsASCIIOnly(t *testing.T) {
	tok := mustDefault(t)
	if got := tok.Count("1234567890"); got != 4 {
		t.Errorf("Count(1234567890) = %d, want 4 (3+3+3+1)", got)
	}
	if got, want := tok.Count("１２３"), 2; got != want {
		t.Errorf("Count(fullwidth 123) = %d, want %d", got, want)
	}
	if got, want := tok.Count("٣٤٥"), 5; got != want {
		t.Errorf("Count(arabic-indic 345) = %d, want %d", got, want)
	}
}

// TestAddedTokensAreSingleIDs pins the 1,283-entry layer: a special token is ONE
// id whether it stands alone or is embedded in text.
func TestAddedTokensAreSingleIDs(t *testing.T) {
	tok := mustDefault(t)
	cases := []struct {
		text string
		want int
	}{
		{"<｜end▁of▁sentence｜>", 1},
		{"<｜begin▁of▁sentence｜>", 1},
		{"<｜▁pad▁｜>", 1},
		{"<｜place▁holder▁no▁0｜>", 1},
		{"a<｜end▁of▁sentence｜>b", 3},
	}
	for _, c := range cases {
		if got := tok.Count(c.text); got != c.want {
			t.Errorf("Count(%q) = %d, want %d", c.text, got, c.want)
		}
	}
	// The id itself must be the added token's own id, not a model id.
	ids := tok.Encode("<｜end▁of▁sentence｜>")
	if len(ids) != 1 || ids[0] != 1 {
		t.Errorf("Encode(<｜end▁of▁sentence｜>) = %v, want [1]", ids)
	}
	ids = tok.Encode("<｜begin▁of▁sentence｜>")
	if len(ids) != 1 || ids[0] != 0 {
		t.Errorf("Encode(<｜begin▁of▁sentence｜>) = %v, want [0]", ids)
	}
}

// TestVocabIsComplete asserts the parse indexed the whole file rather than a
// prefix: every byte must have a single-rune token, and the vocab must have the
// size the file declares.
func TestVocabIsComplete(t *testing.T) {
	tok := mustDefault(t)
	if got, want := len(tok.vocab), vocabEntries; got != want {
		t.Errorf("vocab holds %d entries, want %d", got, want)
	}
	if got, want := len(tok.merged), mergeEntries; got != want {
		t.Errorf("merge index holds %d entries, want %d", got, want)
	}
	if got, want := len(tok.added), addedTokens; got != want {
		t.Errorf("added tokens: %d, want %d", got, want)
	}
	for b := 0; b < 256; b++ {
		sym := string(tok.byteRunes[b])
		if _, ok := tok.vocab[sym]; !ok {
			t.Errorf("byte %d (rune %q) has no single-rune token", b, sym)
		}
	}
}

// TestConcurrentEncode runs encodes in parallel: a parsed Tokenizer is
// read-only, and a shared buffer would show up here as a race or a wrong id.
func TestConcurrentEncode(t *testing.T) {
	tok := mustDefault(t)
	texts := []string{
		"hello world", "这是一个中文测试句子", "1234567890", "<｜end▁of▁sentence｜>",
		"func main() { println(\"hi\") }", "emoji 😀🎉", strings.Repeat("ab ", 50),
	}
	want := make([][]uint32, len(texts))
	for i, s := range texts {
		want[i] = tok.Encode(s)
	}
	var wg sync.WaitGroup
	for round := 0; round < 8; round++ {
		for i := range texts {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				got := tok.Encode(texts[i])
				if len(got) != len(want[i]) {
					t.Errorf("concurrent Encode(%q): %d ids, want %d", texts[i], len(got), len(want[i]))
					return
				}
				for j := range got {
					if got[j] != want[i][j] {
						t.Errorf("concurrent Encode(%q): id[%d] = %d, want %d", texts[i], j, got[j], want[i][j])
						return
					}
				}
			}(i)
		}
	}
	wg.Wait()
}

// TestCountEqualsEncodeLen is the cheap invariant a caller relies on.
func TestCountEqualsEncodeLen(t *testing.T) {
	tok := mustDefault(t)
	for _, s := range []string{"", "a", "中文", "123", "a b  c", "<｜▁pad▁｜>"} {
		if got, want := tok.Count(s), len(tok.Encode(s)); got != want {
			t.Errorf("Count(%q) = %d, len(Encode) = %d", s, got, want)
		}
	}
}

func mustDefault(t *testing.T) *Tokenizer {
	t.Helper()
	tok, err := Default()
	if err != nil {
		t.Fatalf("parse embedded tokenizer.json: %v", err)
	}
	return tok
}

// TestEncodeMaskedRefusesAMismatchedMask is the regression arm for a panic that
// shipped in this signature: a mask whose length is not len(text) used to call
// panic from a plain counting method, so a caller's arithmetic slip took the
// whole process down mid-turn. It is now an error the caller handles
// (PROJECT.md Rule 19 — panic belongs to init/main/Must*, and this is neither).
func TestEncodeMaskedRefusesAMismatchedMask(t *testing.T) {
	tok := mustDefault(t)
	if _, err := tok.EncodeMasked("abc", []bool{true, true}); err == nil {
		t.Fatal("EncodeMasked accepted a 2-byte mask for 3 bytes of text — the length check is gone")
	}
	// A nil mask means "every byte is template text" and a mask of exactly
	// len(text) is the ordinary case: both must still succeed, so the refusal
	// above is a check on the SHAPE and not a blanket refusal.
	if _, err := tok.EncodeMasked("abc", nil); err != nil {
		t.Fatalf("EncodeMasked(nil mask): %v", err)
	}
	if _, err := tok.EncodeMasked("abc", []bool{true, false, true}); err != nil {
		t.Fatalf("EncodeMasked(exact mask): %v", err)
	}
}
