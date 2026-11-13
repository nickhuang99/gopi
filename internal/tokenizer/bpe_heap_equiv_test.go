package tokenizer

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// bpe_heap_equiv_test.go — the rewrite's proof of EQUIVALENCE.
//
// mergePiece was changed from "rescan every adjacent pair after every merge" to
// a heap. The requirement on that change is not "it is faster", it is "it
// produces the SAME ids, in the same order, for every input". This file proves
// it by keeping the old algorithm here, as an oracle, and comparing the two on
// inputs that exercise every path: ASCII runs, punctuation, CJK (stage 2 hands
// a CJK run over as ONE piece), digits, whitespace runs, emoji, and randomly
// generated text over the byte alphabet.
//
// The oracle is deliberately the SLOW, obviously-correct form: it is only ever
// run on small inputs, so its quadratic cost is irrelevant here, and its
// simplicity is the whole point.

// mergePieceRescan is the pre-rewrite algorithm, verbatim in intent: after every
// merge, look at every adjacent pair again and take the lowest rank, leftmost
// on a tie.
func (t *Tokenizer) mergePieceRescan(ids []uint32, mapped []byte) []uint32 {
	rs := decodeMapped(mapped)
	if len(rs) == 0 {
		return ids
	}
	starts := make([]int, len(rs)+1)
	for k := range starts {
		starts[k] = k
	}
	syms := len(rs)
	for syms > 1 {
		bestRank := uint32(0)
		bestK := -1
		for k := 0; k+1 < syms; k++ {
			rank, ok := t.merged[pairKey(rs, starts, k)]
			if !ok {
				continue
			}
			if bestRank == 0 || rank < bestRank {
				bestRank = rank
				bestK = k
			}
		}
		if bestK < 0 {
			break
		}
		starts = append(starts[:bestK+1], starts[bestK+2:]...)
		syms--
	}
	for k := 0; k < syms; k++ {
		ids = append(ids, t.idForSymbol(string(rs[starts[k]:starts[k+1]])))
	}
	return ids
}

// pairKey builds the lookup key of the two adjacent symbols at k: the first
// symbol's runes followed by the second's.
func pairKey(rs []rune, starts []int, k int) string {
	var b []byte
	for p := starts[k]; p < starts[k+1]; p++ {
		b = appendRune(b, rs[p])
	}
	for p := starts[k+1]; p < starts[k+2]; p++ {
		b = appendRune(b, rs[p])
	}
	return string(b)
}

// bpeEquivInputs is the corpus both implementations are run on.
func bpeEquivInputs() map[string]string {
	in := map[string]string{
		"empty":              "",
		"one byte":           "a",
		"two bytes":          "ab",
		"short word":         "hello",
		"english sentence":   "The quick brown fox jumps over the lazy dog.",
		"whitespace run":     "a     b\n\n\nc\t\t d",
		"cjk run":            "这是一个中文测试句子，用来验证分词器的合并顺序。",
		"cjk no punctuation": strings.Repeat("中文测试", 20),
		"digits":             "1234567890 20250101 1234567.89",
		"punctuation":        `!@#$%^&*()_+-=[]{}|;:'",.<>/?\`,
		"emoji":              "😀🎉👍🏽 mixed with text",
		"code":               "func (t *Tokenizer) Count(text string) int { return len(t.Encode(text)) }",
		"json blob":          `{"a":1,"b":[true,false,null],"c":{"d":"e"}}`,
		"base64-ish":         strings.Repeat("QUJDREVGR0hJSktMTU5PUA==", 8),
		"mixed":              "订单号 20250101 金额 1234567.89 😀🎉\nThe quick brown fox.",
		"long ascii run":     strings.Repeat("abcdefghij", 40),
		"long digit run":     strings.Repeat("1234567890", 40),
		"long punct run":     strings.Repeat("!@#$%^&*", 40),
		"newlines":           strings.Repeat("\n", 50),
		"spaces":             strings.Repeat(" ", 50),
		"tabs":               strings.Repeat("\t", 50),
		"repeated word":      strings.Repeat("token ", 60),
		"hyphens":            strings.Repeat("a-b-c-", 30),
		"utf8 wide":          strings.Repeat("中文字", 30),
		"accented":           "naïve café résumé über",
	}
	// Random strings over a mixed alphabet, fixed seed so a failure replays.
	rng := rand.New(rand.NewSource(20261107))
	alphabet := []rune("abcXYZ .,!?\n\t0123456789中文本語あア😀-_/\\\"'{}[]")
	for i := 0; i < 200; i++ {
		n := 1 + rng.Intn(60)
		var b strings.Builder
		for j := 0; j < n; j++ {
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		in[fmt.Sprintf("random %03d", i)] = b.String()
	}
	return in
}

// TestMergePieceHeapMatchesRescan is the acceptance gate on the rewrite.
func TestMergePieceHeapMatchesRescan(t *testing.T) {
	tok, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, text := range bpeEquivInputs() {
		if name == "" {
			continue
		}
		name, text := name, text
		t.Run(name, func(t *testing.T) {
			var mapped []byte
			for i := 0; i < len(text); i++ {
				mapped = appendRune(mapped, tok.byteRunes[text[i]])
			}
			want := tok.mergePieceRescan(nil, mapped)
			got := tok.mergePiece(nil, mapped)
			if len(got) != len(want) {
				t.Fatalf("%q: heap gave %d ids, rescan gave %d", text, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%q: id %d differs: heap %d, rescan %d", text, i, got[i], want[i])
				}
			}
		})
	}
}

// TestMergePieceHeapHandlesEmptyEdge pins the degenerate inputs directly: the
// heap path must not index into an empty symbol slice.
func TestMergePieceHeapHandlesEmptyEdge(t *testing.T) {
	tok, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for _, mapped := range [][]byte{nil, {}, []byte("a")} {
		got := tok.mergePiece(nil, mapped)
		want := tok.mergePieceRescan(nil, mapped)
		if len(got) != len(want) {
			t.Fatalf("mapped %q: heap %d ids, rescan %d", mapped, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("mapped %q: id %d differs", mapped, i)
			}
		}
	}
}
