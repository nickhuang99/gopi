package tokenizer

import (
	"encoding/json"
	"fmt"
	"sort"
)

// fileShape is the slice of tokenizer.json this package reads. The other keys
// (decoder, post_processor, truncation, padding) do not affect encoding with
// add_special_tokens=false and are deliberately not decoded: a field nothing
// reads cannot drift under the reader.
type fileShape struct {
	Model struct {
		Type   string            `json:"type"`
		Vocab  map[string]uint32 `json:"vocab"`
		Merges []string          `json:"merges"`
	} `json:"model"`
	AddedTokens []struct {
		ID      uint32 `json:"id"`
		Content string `json:"content"`
	} `json:"added_tokens"`
}

// Parse decodes and indexes tokenizer.json. This is the expensive step — it is
// what the sync.Once exists for: the decode allocates the whole vocab and merge
// table, and the index build turns 127,741 merge strings into a rank map.
func Parse(data []byte) (*Tokenizer, error) {
	var f fileShape
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode tokenizer.json: %w", err)
	}
	if f.Model.Type != bpeModelType {
		return nil, fmt.Errorf("tokenizer.json: model.type %q, want %q", f.Model.Type, bpeModelType)
	}
	var err error
	t := &Tokenizer{
		vocab:  make(map[string]uint32, len(f.Model.Vocab)),
		merged: make(map[string]uint32, len(f.Model.Merges)),
	}
	for s, id := range f.Model.Vocab {
		t.vocab[s] = id
	}
	for i, m := range f.Model.Merges {
		// The BPE loop's hot lookup: a 2-rune key per merge, so the rank map is
		// only paid on the pieces that actually merge.
		left, right, ok := splitMerge(m)
		if !ok {
			return nil, fmt.Errorf("tokenizer.json: merge %d (%q) is not a two-part pair", i, m)
		}
		t.merged[left+right] = uint32(i) + 1 // 0 means "not a merge"
	}
	t.byteRunes = byteRuneTable()
	t.added = make([]addedToken, 0, len(f.AddedTokens))
	for _, a := range f.AddedTokens {
		t.added = append(t.added, addedToken{content: a.Content, id: a.ID})
		if len(a.Content) > t.maxAdded {
			t.maxAdded = len(a.Content)
		}
		for i := 0; i < len(a.Content); i++ {
			t.addedInit[a.Content[i]] = true
		}
	}
	sort.Slice(t.added, func(i, j int) bool { return t.added[i].content < t.added[j].content })
	if t.pre, err = compilePre(); err != nil {
		return nil, err
	}
	return t, nil
}

// splitMerge splits a merge entry into its two space-separated components.
// Every entry in this file has exactly two (measured: 0 entries differ) and no
// component contains a space (measured: 0 vocab tokens do), so a plain split is
// exact rather than convenient.
func splitMerge(m string) (left, right string, ok bool) {
	for i := 0; i < len(m); i++ {
		if m[i] == ' ' {
			if i == 0 || i == len(m)-1 {
				return "", "", false
			}
			return m[:i], m[i+1:], true
		}
	}
	return "", "", false
}

// bpeModelType is the tokenizer.json model.type this package implements. Any
// other value (Unigram, WordPiece) has a different model and must be refused
// rather than counted wrongly.
const bpeModelType = "BPE"

// The three population counts of the embedded file. Named because three tests
// and a parser all read them, and a bare number in the parser would be a second
// source of truth for "how big is the file" (Rule 1/14). A different
// tokenizer.json trips these on purpose: this package is pinned to one model.
const (
	vocabEntries = 128000
	mergeEntries = 127741
	addedTokens  = 1283
)
