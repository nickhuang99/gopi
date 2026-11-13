// Package cjkseg is gopi's ONE Chinese word segmenter: a thin, deterministic
// wrapper around the pure-Go `github.com/go-ego/gse` engine, loaded from its
// EMBEDDED dictionary so no data file ever has to exist beside the binary.
//
// Why it is a package of its own (Rule 14): the segmenter is used by the
// MEMORY WRITER (the jieba_token column) and by the QUERY SIDE (the segmented
// recall route), and those two must produce the SAME granularity or the index
// and the query speak past each other. One function, one dictionary, one
// process-wide instance is the only shape in which that cannot drift.
//
// What it replaces: a 2-char CJK sliding-window "segmentation" that emitted
// every overlapping bigram of a contiguous CJK run. That is a stand-in for a
// segmenter, not a segmenter — it has no dictionary, so it cannot know that
// 网络测试环境 is 网络 + 测试环境 rather than 网络/络测/测试/试环/环境, and it
// cannot peel a Latin word off a glued run (这些commit is two words). The
// column is named jieba_token and promised a real segmenter; this package is
// what makes that promise true (devdoc/jieba-token-real-segmentation.md).
//
// CONTRACT for callers: Words() never returns an empty string, never returns
// a token carrying whitespace, is deterministic for a given input, and is
// order-preserving (gse returns the input's words in input order). The
// caller joins with its own separator; the separator is deliberately NOT
// defined here (see internal/tools' m1JiebaTextSep for the index-side join
// rule).
package cjkseg

import (
	"strings"
	"sync"

	"github.com/go-ego/gse"
)

const (
	// embeddedDict names the gse dictionary compiled into the binary. `zh_s`
	// is the small Simplified-Chinese dictionary (gse also embeds `zh`),
	// chosen for the binary-size delta measured in the task report; the name
	// is a constant (Rule 1b) because it is a semantic value, not a literal.
	embeddedDict = "zh_s"

	// maxWords caps the token list one call may return. gse's DAG path is
	// linear in input length, but the token list is written into a single
	// SQLite column whose FTS5 index is rebuilt on every write: an
	// unbounded token list is an unbounded index write. The cap is the
	// declared-safe degradation (the input's head wins) — a text long
	// enough to reach it is a document, not a memory row (Rule 1a).
	maxWords = 4096

	// hmmEnabled is gse's HMM switch: unknown words (names, coinages, new
	// terms a dictionary cannot carry) are recovered by Viterbi over the
	// HMM when the DAG finds no dictionary path. It is ON because the
	// alternative silently splits a new term into single runes, which is
	// exactly the recall loss this package exists to remove.
	hmmEnabled = true
)

// segmenterOnce guards the one process-wide load. gse's Segmenter carries a
// ~10 MB dictionary in memory, so it is loaded once and shared: the memory
// engine may be called from concurrent goroutines (a store while a search
// runs), and Segmenter.Cut is documented read-only after loading.
var (
	segmenterOnce sync.Once
	segmenter     gse.Segmenter
	segmentErr    error
)

// load performs the one-time dictionary load. split out so a test can drive
// the failure path without poisoning the package-level Once.
func load() {
	var seg gse.Segmenter
	if err := seg.LoadDictEmbed(embeddedDict); err != nil {
		segmentErr = err
		return
	}
	segmenter = seg
}

// Words segments s into its words. It returns only non-empty, whitespace-free
// tokens, in input order. A string with no segmentable content returns nil.
//
// The dictionary load happens on the first call. Its failure is returned
// rather than swallowed (Rule 15): a segmenter that cannot load would
// otherwise degrade silently into "no tokens", i.e. an index column that
// looks written and matches nothing.
func Words(s string) ([]string, error) {
	segmenterOnce.Do(load)
	if segmentErr != nil {
		return nil, segmentErr
	}
	if s == "" {
		return nil, nil
	}
	cut := segmenter.Cut(s, hmmEnabled)
	out := make([]string, 0, len(cut))
	for _, w := range cut {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		// gse keeps whitespace inside a token when the input has a run of it
		// ("逐步   缩小" yielded an empty-string token in the measured probe);
		// an embedded space would make the joined column two unicode61 tokens
		// while the query side sees one — split instead, deterministically.
		for _, part := range strings.Fields(w) {
			out = append(out, part)
			if len(out) >= maxWords {
				return out, nil
			}
		}
	}
	return out, nil
}

// DictName reports the embedded dictionary this binary was built with. It
// exists so a report or a test can name what produced a token list instead of
// describing it from memory (Rule 38: a figure names its generator).
func DictName() string { return embeddedDict }
