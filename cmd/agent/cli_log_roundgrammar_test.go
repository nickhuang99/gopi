package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
)

// ---------------------------------------------------------------------------
// Round-grammar contract: cmd/agent must NOT own a second `all|last|N` round
// grammar. gopilog.ParseHTTPSelector is the single source of truth (the same
// grammar `gopi session raw <round>` / `gopi session stats --rounds` name); the pointer line
// only chooses which new command a PARSED selection maps onto.
//
// Origin: commit e23d506 moved `gopi session log --http/--raw` out but left cmd with
// its own roundArg grammar and gopilog's parser with no production caller —
// two grammars, nobody comparing them. These tests are the comparison.
// ---------------------------------------------------------------------------

// roundGrammarCases is the shared matrix: the documented grammar is
// "" / "all" → whole-round listing, "last" → the last round's detail, a
// decimal integer N → round N (0/negative included, resolved downstream by
// the "detail defaults to last" rule), anything else → a parse error.
var roundGrammarCases = []struct {
	arg     string
	wantSel gopilog.HTTPRoundSelector
	wantOK  bool
}{
	{"", gopilog.HTTPRoundSelector{All: true}, true},
	{"   ", gopilog.HTTPRoundSelector{All: true}, true}, // trimmed like the parser
	{"all", gopilog.HTTPRoundSelector{All: true}, true},
	{"last", gopilog.HTTPRoundSelector{Detail: true, Round: gopilog.LastRound}, true},
	{"1", gopilog.HTTPRoundSelector{Detail: true, Round: 1}, true},
	{"7", gopilog.HTTPRoundSelector{Detail: true, Round: 7}, true},
	{"+7", gopilog.HTTPRoundSelector{Detail: true, Round: 7}, true},
	{"0", gopilog.HTTPRoundSelector{Detail: true, Round: 0}, true},
	{"-3", gopilog.HTTPRoundSelector{Detail: true, Round: -3}, true},
	{"All", gopilog.HTTPRoundSelector{}, false},  // case matters, exactly like the parser
	{"LAST", gopilog.HTTPRoundSelector{}, false}, //
	{"0x10", gopilog.HTTPRoundSelector{}, false}, // no hex
	{"3x", gopilog.HTTPRoundSelector{}, false},   // trailing junk
	{"third", gopilog.HTTPRoundSelector{}, false},
}

// TestRoundGrammarIsParsedByGopilog pins the cmd-side mapping to the matrix
// AND re-checks the parser against the same rows, so a change to the parser's
// grammar fails here too (one table, both sides).
func TestRoundGrammarIsParsedByGopilog(t *testing.T) {
	for _, c := range roundGrammarCases {
		sel, ok := roundSelectionFor(c.arg)
		if ok != c.wantOK {
			t.Errorf("roundSelectionFor(%q): ok = %v, want %v", c.arg, ok, c.wantOK)
			continue
		}
		if ok && sel != c.wantSel {
			t.Errorf("roundSelectionFor(%q): sel = %+v, want %+v", c.arg, sel, c.wantSel)
		}

		psel, perr := gopilog.ParseHTTPSelector(c.arg)
		if (perr == nil) != c.wantOK {
			t.Errorf("ParseHTTPSelector(%q): err = %v — the parser and the cmd matrix disagree", c.arg, perr)
			continue
		}
		if perr == nil && psel != c.wantSel {
			t.Errorf("ParseHTTPSelector(%q): sel = %+v, want %+v", c.arg, psel, c.wantSel)
		}
	}
}

// TestRoundSelectionIsNotTautological: roundSelectionFor must DELEGATE, not
// re-parse — a value the parser rejects must never yield a selection, and a
// value it accepts must come through unchanged.
func TestRoundSelectionIsNotTautological(t *testing.T) {
	for _, arg := range []string{"nope", "0x10", "1.5", "--2", "٣"} {
		if sel, ok := roundSelectionFor(arg); ok {
			t.Errorf("roundSelectionFor(%q) = %+v, %v — the parser rejects this; delegation is broken", arg, sel, ok)
		}
	}
	sel, ok := roundSelectionFor("4")
	if !ok || sel != (gopilog.HTTPRoundSelector{Detail: true, Round: 4}) {
		t.Errorf("roundSelectionFor(\"4\") = %+v, %v", sel, ok)
	}
}

// wantRoundArg is the INDEPENDENT statement of the pointer grammar roundArg
// must implement: every selector the shared grammar accepts as naming ONE round
// is kept as written (integer N, including 0 and negatives, and the literal
// keyword "last"), while the bare form and the listing keyword name no round
// and become "".
func wantRoundArg(httpArg string) string {
	arg := strings.TrimSpace(httpArg)
	if arg == "" || arg == gopilog.RoundKeywordAll {
		return ""
	}
	if arg == gopilog.RoundKeywordLast {
		return arg
	}
	if _, err := strconv.Atoi(arg); err != nil {
		return ""
	}
	return arg
}

// TestRoundArgGrammarTable pins the pointer's observable behavior across the
// refactor: roundArg now ASKS the shared parser, and every selector that names
// a round — "last" and the negative from-the-end forms included — is kept, so
// the pointer names the exact equivalent invocation.
func TestRoundArgGrammarTable(t *testing.T) {
	for _, arg := range []string{"", "   ", "all", "last", "1", "3", "07", "+3", "0", "-1", "-2", "0x10", "3x", "third", "All", "٣"} {
		if got, want := roundArg(arg), wantRoundArg(arg); got != want {
			t.Errorf("roundArg(%q) = %q, want %q", arg, got, want)
		}
	}
}

// TestLogMovedPointerForMalformedRound: a --http value the shared grammar
// rejects still yields ONE pointer line naming the round-table equivalent —
// never a crash, never a grammatically wrong new command.
func TestLogMovedPointerForMalformedRound(t *testing.T) {
	for _, arg := range []string{"third", "0x10", "1.5"} {
		got := logMovedPointer(&logOptions{http: true, httpArg: arg})
		if got == "" {
			t.Fatalf("--http %q: empty pointer", arg)
		}
		if strings.Contains(got, arg) {
			t.Errorf("--http %q: pointer names the unusable round %q: %s", arg, arg, got)
		}
		if want := "gopi session stats <id> --rounds"; !strings.Contains(got, want) {
			t.Errorf("--http %q: pointer %q does not name %q", arg, got, want)
		}
	}
}
