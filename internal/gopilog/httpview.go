package gopilog

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/rawlogfmt"
)

// The `all|last|N` selector grammar's keyword spellings and its grammar text
// (PROJECT.md Rule 1b/13/14: one definition, referenced — never retyped, never
// mirrored). Every consumer that must recognize the whole-round / last-round
// spellings compares against these — the parser, its error text, `gopi session raw`'s
// refusal of the listing keyword, and the pointer test in cmd/agent — so a
// rename cannot leave one surface accepting an obsolete word.
const (
	// RoundKeywordAll selects the whole-round listing (the selector's zero
	// value behaves the same, so the bare flag needs no argument).
	RoundKeywordAll = "all"
	// RoundKeywordLast selects the LAST round by name (ParseHTTPSelector maps
	// it to LastRound).
	RoundKeywordLast = "last"
	// LastRound is the selector's "the last round" round value — what
	// RoundKeywordLast maps to, and what an empty round selector resolves to.
	// Round values count from the end when zero or negative: 0 = last,
	// -1 = last, -2 = 2nd-from-last (see HTTPDetailIndex).
	LastRound = -1
)

// httpSelectorGrammar is the grammar as ParseHTTPSelector's error message names
// it, RENDERED from the keywords themselves so the text cannot misdocument what
// the parser is about to accept. The text once listed the `last` keyword TWICE
// (the Rule 13 literal gate in httpview_selector_grammar_test.go bans that
// spelling from every production file, so it is not retyped here) and
// contradicted the parser outright: 0 and negatives are legal round selectors.
// cmd/agent's pointer path had re-implemented the grammar a second time
// (removed by commit 4d9b7797).
const httpSelectorGrammar = RoundKeywordAll + "|" + RoundKeywordLast + "|N"

// This file keeps the round-selector GRAMMAR and the rendering helpers that
// other surfaces share. The per-round digest view that used to live here
// (`gopi session log --http`'s listing/detail/verbatim-body formatters) was orphaned
// when that view moved out of the log surface (commit e23d506: `gopi session log <id>
// --http …` now only prints a pointer line, cmd/agent/cli_log.go
// logMovedPointer). The migrated commands render their OWN output —
// gopilog.FormatStatsRounds (`gopi session stats <id> --rounds`, per-round numbers) and
// gopilog.RawRoundIndex / RawRoundRequest / RawRoundResponseSpan (`gopi session raw
// <id> [round] [request|response]`, bytes) — so the digest formatters
// (FormatHTTPRounds / FormatHTTPRoundRequestBody / FormatHTTPRoundResponseBody,
// formatHTTPRoundList, formatHTTPRoundDetail, writeRequestDetail,
// writeResponseDetail, describeHTTPMessage, httpRequestShape, httpRoundTools,
// shortToolID, HTTPDetailIndex, HTTPBodyOptions and their display caps) were
// deleted outright; the per-message digest has no successor by design.
//
// What stayed, and why:
//   - ParseHTTPSelector / HTTPRoundSelector / RoundKeywordAll /
//     RoundKeywordLast / LastRound / HTTPDetailIndex: the ONE owner of the
//     `all|last|N` round grammar. ParseHTTPSelector is used in PRODUCTION by
//     cmd/agent's moved-pointer path (cli_log.go roundSelectionFor/roundArg,
//     rendering logMovedPointer's pointer text) AND by `gopi session raw`'s <round>
//     positional (cli_raw.go parseRawArgs) — one grammar, two surfaces — and
//     HTTPDetailIndex is the one resolver that turns a selector into a round
//     index, shared by both.
//   - HTTPDetailIndex had been deleted with the digest view although it is a
//     RESOLVER, not a renderer. Without it each surface resolved by hand
//     (`Round-1`; every round <= 0 sent to the last one), so the documented
//     `-N` = Nth-from-last semantics existed nowhere in the tree.
//   - humanBytes (rawview.go, stats.go), orDash (stats.go) and
//     readRawLogLineSpan (rawview.go): shared rendering/reading helpers with
//     live production consumers.

// HTTPRoundSelector parses the --http value into a round selection.
// "" or "all" → the whole-round summary listing; a positive integer N → the
// detail of round N; "last" / 0 → the last round's detail; a negative -N →
// the Nth-from-last round's detail.
type HTTPRoundSelector struct {
	All    bool // summary listing of every round
	Detail bool // detail of one round (chosen by Round)
	Round  int  // round selector when Detail: >0 = 1-based round, 0/negative = from the end (HTTPDetailIndex)
}

// ParseHTTPSelector parses the raw round-selection argument: the `all|last|N`
// grammar. It is NOT (any longer) the parser of `gopi session log --http` — that view
// moved out in e23d506; cmd/agent uses it for the moved-pointer text, and
// `gopi session raw`'s <round> positional goes through the SAME parser, so the CLI
// help, the parser, the error text and both surfaces cannot drift into three
// spellings.
func ParseHTTPSelector(arg string) (HTTPRoundSelector, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" || arg == RoundKeywordAll {
		return HTTPRoundSelector{All: true}, nil
	}
	if arg == RoundKeywordLast {
		return HTTPRoundSelector{Detail: true, Round: LastRound}, nil
	}
	n, err := strconv.Atoi(arg)
	if err != nil {
		return HTTPRoundSelector{}, fmt.Errorf("--http takes %s (a round number), got %q", httpSelectorGrammar, arg)
	}
	return HTTPRoundSelector{Detail: true, Round: n}, nil
}

// HTTPDetailIndex resolves a round selector against a parsed round list and
// returns the index to print (0-based). It counts from the END for a selector
// that is zero or negative: 0 and -1 are the last round, -2 the 2nd-from-last,
// and a selector past the first round clamps to it — the same forgiving
// fallback an over-large positive selector gets. A zero/negative selector
// resolves against the list length HERE; a caller that needs to REJECT an
// out-of-range selector must range-check it itself (cmd/agent's rawRoundPosition).
func HTTPDetailIndex(rounds []HTTPRound, round int) int {
	return selectIndex(len(rounds), round)
}

// selectIndex is the ONE place the from-the-end counting rule and its clamp
// live, expressed over a list LENGTH so it serves every list a round selector
// can address (raw rounds, session iterations) instead of only []HTTPRound
// (Rule 1b/13: one definition, no mirrored copy of the arithmetic).
//
// A selector that is zero or negative counts from the end: 0 and -1 are the
// last entry, -2 the 2nd-from-last; an out-of-range selector clamps to the
// nearest end. A caller that needs to REJECT an out-of-range selector must
// range-check it itself (cmd/agent's rawRoundPosition, gopilog.SessionRoundIndex).
func selectIndex(n, round int) int {
	if n == 0 {
		return 0
	}
	idx := round - 1
	if round <= 0 {
		idx = n + round
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// humanBytes renders a byte count compactly (e.g. "45.6 KB").
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// readRawLogLineSpan returns the round's response-side lines verbatim from the
// raw log, spanning [first, last] (both 1-based inclusive) as the logger wrote
// them (markers and payload lines, byte-exact modulo the trailing per-line
// newline stripped by the scanner, then re-added). A best-effort copy of the
// round's payload region: parse already validated the file once.
func readRawLogLineSpan(path string, first, last int) ([]string, error) {
	if last < first {
		return nil, fmt.Errorf("empty line span %d..%d", first, last)
	}
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		return nil, err
	}
	sc, err := rawlogfmt.OpenLineScanner(path, format)
	if err != nil {
		return nil, err
	}
	var lines []string
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo > last {
			break
		}
		if lineNo < first {
			continue
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
