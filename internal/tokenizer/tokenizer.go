// Package tokenizer counts DeepSeek-V4.1 BPE tokens locally — no network round
// trip, no third-party dependency.
//
// The data is the model's own tokenizer.json (6,367,257 B,
// sha256 c90dfa01249db1be4245780a052ede752e1361c612ac6d08e2bdada7d599476b),
// embedded with go:embed so the binary carries it. Nothing is parsed at package
// initialisation: the embed holds the bytes, the parse happens inside a
// sync.Once on the first Count. A process that never counts tokens pays one
// pointer for the image and no heap at all.
//
// # This file is the V4.1 tokenizer, not V3
//
// The BPE vocabulary, the merges, the pre-tokenizer, the normalizer and the
// decoder are IDENTICAL between the two generations (measured key-by-key
// against the V4.1 model repo's tokenizer.json: 128,000 vocab entries, 127,741
// merges, byte-for-byte the same). The only difference is `added_tokens`:
// V3 ships 818, V4.1 ships 1,283. The 465 new entries are the V4.1 template's
// own markers — `｜DSML｜` (id 128825), `<｜System｜>`, `<｜latest_reminder｜>`,
// the task tokens — and the ones that moved (`<think>` 128798→128821,
// `</think>` 128799→128822).
//
// That is why counting a rendered V4.1 prompt with the V3 data produced a
// wrong answer that LOOKED nearly right: `<｜User｜>`, `<｜Assistant｜>` and the
// BOS/EOS markers kept their ids and stayed one token, while `｜DSML｜` — the
// marker every V4.1 tool call is built from — fell through to ordinary BPE and
// was charged four tokens where the endpoint charges one.
//
// The pipeline reproduces
//
//	tokenizers.Tokenizer.from_file("tokenizer.json").encode(text, add_special_tokens=False)
//
// stage for stage (see pre.go for the chain and bpe.go for the merge):
//
//  1. added tokens (1,283) are cut out of the raw text first, leftmost-longest;
//  2. three Split stages run in order over the remaining pieces;
//  3. ByteLevel maps each byte to the vocabulary alphabet (use_regex is FALSE in
//     this file, so it maps and does not re-split);
//  4. greedy lowest-rank BPE over each mapped piece;
//  5. ids from the vocab.
package tokenizer

import (
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrMaskLengthMismatch is returned by EncodeMasked when the caller's mask is
// not exactly len(text) entries long: the mask is built beside the string it
// masks, so a mismatch means the CALLER rendered one and measured the other.
// It is an error and not a panic because no invariant of this package is
// violated — the input is simply wrong (PROJECT.md Rule 19).
var ErrMaskLengthMismatch = errors.New("tokenizer: EncodeMasked mask length does not match text")

// rawData is the embedded tokenizer.json — the only copy of the data.
//
//go:embed tokenizer.json
var rawData []byte

// DataBytes is the size of the embedded tokenizer.json, pinned so a truncated
// or swapped copy fails a test instead of producing plausible ids. The report
// cites this number: PROJECT.md Rule 39's 256 KiB tracked-blob ceiling is
// decided on the operator's side for this one file.
const DataBytes = 6367257

// Tokenizer is a parsed tokenizer. The zero value is not usable; take the one
// from Default or Parse. A parsed Tokenizer is read-only and safe for
// concurrent use.
type Tokenizer struct {
	vocab     map[string]uint32
	merged    map[string]uint32 // two-rune pair → 1-based rank, 0 = "not a merge"
	byteRunes [256]rune
	added     []addedToken // sorted by content
	addedInit [256]bool    // a byte that can never start an added token
	maxAdded  int
	pre       pre
}

// The three special tokens the coverage check looks for by name. They are named
// constants rather than inline literals (Rule 1) and are the file's own strings.
const (
	addedBOS = "<｜begin▁of▁sentence｜>"
	addedEOS = "<｜end▁of▁sentence｜>"
	addedPAD = "<｜▁pad▁｜>"
)

// addedToken is one entry of the file's added_tokens array.
type addedToken struct {
	content string
	id      uint32
}

var (
	loadOnce sync.Once
	defaultT *Tokenizer
	loadErr  error
)

// Default returns the embedded tokenizer, parsing it on the first call. A parse
// failure is returned on every call rather than cached as a partial value.
func Default() (*Tokenizer, error) {
	loadOnce.Do(func() {
		defaultT, loadErr = Parse(rawData)
	})
	return defaultT, loadErr
}

// Ready reports whether the embedded data has been parsed yet. It exists for the
// laziness test and for a caller that wants to log the boundary; it does NOT
// trigger the parse.
func Ready() bool { return defaultT != nil }

// Count returns the number of tokens text encodes to, special tokens disabled —
// len(tokenizer.encode(text, add_special_tokens=False).ids) in Python.
func (t *Tokenizer) Count(text string) int { return len(t.Encode(text)) }

// CountDefault is Count on the embedded tokenizer, with the parse error surfaced.
// A caller that only needs a number (a context-size readout) wants this one.
func CountDefault(text string) (int, error) {
	t, err := Default()
	if err != nil {
		return -1, err
	}
	return t.Count(text), nil
}

// Encode returns the token ids for text, in order.
func (t *Tokenizer) Encode(text string) []uint32 {
	if text == "" {
		return nil
	}
	var ids []uint32
	var pieces []string
	start, i := 0, 0
	for i < len(text) {
		// Added tokens are rare: the common path is a byte test and an
		// increment, and only a byte that can start one pays for a search.
		if !t.addedInit[text[i]] {
			i++
			continue
		}
		id, n := t.matchAdded(text[i:])
		if n == 0 {
			i++
			continue
		}
		if i > start {
			pieces = t.pre.splitPre(text[start:i], pieces)
			ids = t.encodePieces(ids, pieces)
		}
		ids = append(ids, id)
		i += n
		start = i
	}
	if start < len(text) {
		pieces = t.pre.splitPre(text[start:], pieces)
		ids = t.encodePieces(ids, pieces)
	}
	return ids
}

// EncodeMasked returns the token ids for text in which added-token matching is
// allowed ONLY at positions the caller marks as template text.
//
// This is the endpoint's own rule, measured: a marker the TEMPLATE emits
// (`｜DSML｜`, `<｜User｜>`, …) is one added token, while the same spelling
// interpolated from MESSAGE CONTENT is ordinary BPE (`｜DSML｜` as a parameter
// value costs 4). The prompt is still tokenized as ONE string — pre-tokenizer
// and BPE runs cross the content/template boundary — so a boundary merge the
// split-at-every-segment model would lose is preserved.
//
// mask must be exactly len(text) bytes long; nil means every byte is template
// text. A mask of any other length is a CALLER bug and is returned as the error
// ErrMaskLengthMismatch, never panicked on: the count a caller builds from it is
// a budget, a process that dies mid-turn loses that turn, and a caller that has
// to string-match the message cannot tell this refusal from any other (PROJECT.md
// Rule 19 — panic belongs to init/main/Must*; Rule 15 — no silent failure).
//
// REBASE NOTE (delivery round 2/3, ground 01a1a674): origin/master's fa77f68f
// landed this same fix independently (a sibling row). The BEHAVIOUR is master's
// — its error text and its no-panic contract. What is kept from f601f40a is the
// named sentinel alone, because master's `fmt.Errorf` with no %w is a message a
// caller can only string-match; the sibling's regression arms in unit_test.go and
// the sentinel's arms in masked_test.go both now assert it.
func (t *Tokenizer) EncodeMasked(text string, mask []bool) ([]uint32, error) {
	if text == "" {
		return nil, nil
	}
	if mask != nil && len(mask) != len(text) {
		return nil, fmt.Errorf("%w: text %d bytes, mask %d entries", ErrMaskLengthMismatch, len(text), len(mask))
	}
	var ids []uint32
	var pieces []string
	start, i := 0, 0
	for i < len(text) {
		if mask != nil && !mask[i] {
			i++
			continue
		}
		if !t.addedInit[text[i]] {
			i++
			continue
		}
		id, n := t.matchAdded(text[i:])
		if n == 0 {
			i++
			continue
		}
		if i > start {
			pieces = t.pre.splitPre(text[start:i], pieces)
			ids = t.encodePieces(ids, pieces)
		}
		ids = append(ids, id)
		i += n
		start = i
	}
	if start < len(text) {
		pieces = t.pre.splitPre(text[start:], pieces)
		ids = t.encodePieces(ids, pieces)
	}
	return ids, nil
}

// encodePieces encodes each pre-tokenizer piece.
func (t *Tokenizer) encodePieces(ids []uint32, pieces []string) []uint32 {
	var mapped []byte
	for _, p := range pieces {
		mapped = mapped[:0]
		for i := 0; i < len(p); i++ {
			mapped = appendRune(mapped, t.byteRunes[p[i]])
		}
		ids = t.mergePiece(ids, mapped)
	}
	return ids
}

// matchAdded returns the LONGEST added token that prefixes s, with its id and
// byte length; (0, 0) when none does.
//
// The 1,283 contents are pairwise prefix-free (measured: 0 prefix pairs), so the
// longest rule never decides anything in THIS file — it is here because it is
// the rule. added is sorted, so each candidate is one binary search.
func (t *Tokenizer) matchAdded(s string) (uint32, int) {
	if len(s) > t.maxAdded {
		s = s[:t.maxAdded]
	}
	for k := len(s); k > 0; k-- {
		prefix := s[:k]
		idx := sort.Search(len(t.added), func(j int) bool { return t.added[j].content >= prefix })
		if idx < len(t.added) && t.added[idx].content == prefix {
			return t.added[idx].id, k
		}
	}
	return 0, 0
}
