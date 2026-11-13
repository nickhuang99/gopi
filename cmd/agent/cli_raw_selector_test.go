package main

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
)

// The <round> positional of `gopi session raw` is the SHARED round grammar: it goes
// through gopilog.ParseHTTPSelector and resolves through gopilog.HTTPDetailIndex
// — the same parser/resolver every other surface naming a round uses. These
// tests drive the CLI's own parse path (parseRawArgs) and the CLI's own
// resolver (rawRoundPosition over the real fixture's parsed rounds), so "the
// shared grammar is used" is a checked fact, not a comment.
//
// Origin: the archived branch feat/raw-round-grammar (tag
// archive/feat/raw-round-grammar, tip d9a5ea41) — re-derived on master, where
// `gopi session raw` still parsed <round> itself with a positive-only rule, making 100%
// of the documented grammar unreachable from the CLI.

// rawSelectorFixture parses the real fixture raw log's rounds — the same input
// `gopi session raw <session> --rounds` prints.
func rawSelectorFixture(t *testing.T) []gopilog.HTTPRound {
	t.Helper()
	id := statsFixtureSandbox(t)
	rounds, err := gopilog.ParseHTTPRounds(resolveRawForSession(id))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) < 3 {
		t.Fatalf("fixture has %d rounds, want >= 3 for the from-the-end cases", len(rounds))
	}
	return rounds
}

// TestRawRoundSelectorUsesSharedGrammar: parseRawArgs must accept exactly the
// gopilog selector grammar — last / 0 / negative / positive — and each accepted
// argument must land in opts.sel with the values gopilog.ParseHTTPSelector
// produced (no second, divergent rule in the CLI).
func TestRawRoundSelectorUsesSharedGrammar(t *testing.T) {
	for _, arg := range []string{"1", "3", "0", "-1", "-2", "last", "  0  "} {
		opts, err := parseRawArgs([]string{"sess", arg})
		if err != nil {
			t.Errorf("parseRawArgs(sess %q) rejected a documented selector: %v", arg, err)
			continue
		}
		want, err := gopilog.ParseHTTPSelector(arg)
		if err != nil {
			t.Fatalf("gopilog.ParseHTTPSelector(%q): %v", arg, err)
		}
		if opts.sel != want {
			t.Errorf("parseRawArgs(sess %q).sel = %+v, want the shared parser's %+v", arg, opts.sel, want)
		}
		if !opts.sel.Detail {
			t.Errorf("parseRawArgs(sess %q).sel = %+v, want Detail", arg, opts.sel)
		}
	}
}

// TestRawRoundSelectorRefusesListingKeyword: "all" selects a LISTING, not a
// round — as the <round> positional it must be an error, never a silent round 0.
func TestRawRoundSelectorRefusesListingKeyword(t *testing.T) {
	for _, arg := range []string{gopilog.RoundKeywordAll, " all "} {
		if _, err := parseRawArgs([]string{"sess", arg}); err == nil {
			t.Errorf("parseRawArgs(sess %q) accepted a listing selector as a round", arg)
		} else if !strings.Contains(err.Error(), "<round> must name one round") {
			t.Errorf("parseRawArgs(sess %q) error = %q, want the <round>-names-one-round diagnostic", arg, err)
		}
	}
	// "ALL" is not the keyword (the grammar is case-sensitive); it is a bad
	// round, reported by the SHARED parser's message — still an error, never
	// a silent round.
	if _, err := parseRawArgs([]string{"sess", "ALL"}); err == nil ||
		!strings.Contains(err.Error(), "--http takes all|last|N") {
		t.Errorf("parseRawArgs(sess ALL) = %v, want the shared grammar's error", err)
	}
}

// TestRawRoundSelectorAcceptsRoundAfterFile: the --file form takes the round as
// its FIRST positional, through the same grammar.
func TestRawRoundSelectorAcceptsRoundAfterFile(t *testing.T) {
	opts, err := parseRawArgs([]string{"--file", "/x.log", "last"})
	if err != nil {
		t.Fatalf("--file + last: %v", err)
	}
	if opts.sel.Round != gopilog.LastRound || !opts.sel.Detail {
		t.Errorf("--file + last = %+v, want the last round", opts.sel)
	}
	// A word that is not a selector next to --file is the both-inputs mistake.
	if _, err := parseRawArgs([]string{"--file", "/x.log", "sess-abc"}); err == nil ||
		!strings.Contains(err.Error(), "EXCLUSIVE") {
		t.Errorf("--file + a session id must report EXCLUSIVE, got %v", err)
	}
}

// TestRawRoundPositionResolvesFromTheEnd is the acceptance core: against the
// real fixture's round list, `last` and `0` name the LAST round and `-2` names
// the 2nd-from-last.
func TestRawRoundPositionResolvesFromTheEnd(t *testing.T) {
	rounds := rawSelectorFixture(t)
	last := len(rounds) - 1

	cases := []struct {
		arg  string
		want int
	}{
		{gopilog.RoundKeywordLast, last},
		{"0", last},
		{"-1", last},
		{"-2", last - 1},
		{"-3", last - 2},
		{"1", 0},
		{"3", 2},
	}
	for _, c := range cases {
		sel, err := gopilog.ParseHTTPSelector(c.arg)
		if err != nil {
			t.Fatalf("ParseHTTPSelector(%q): %v", c.arg, err)
		}
		idx, ok := rawRoundPosition(rounds, sel)
		if !ok {
			t.Errorf("raw <round> %q resolved to nothing, want index %d", c.arg, c.want)
			continue
		}
		if idx != c.want {
			t.Errorf("raw <round> %q → index %d, want %d (of %d rounds)", c.arg, idx, c.want, len(rounds))
		}
		if rounds[idx].Number != rounds[c.want].Number {
			t.Errorf("raw <round> %q → round #%d, want #%d", c.arg, rounds[idx].Number, rounds[c.want].Number)
		}
	}
}

// TestRawRoundPositionRejectsOutOfRange: a selector past EITHER end names no
// round, in both directions.
func TestRawRoundPositionRejectsOutOfRange(t *testing.T) {
	rounds := rawSelectorFixture(t)
	for _, arg := range []string{"99", "-99"} {
		sel, err := gopilog.ParseHTTPSelector(arg)
		if err != nil {
			t.Fatalf("ParseHTTPSelector(%q): %v", arg, err)
		}
		if _, ok := rawRoundPosition(rounds, sel); ok {
			t.Errorf("raw <round> %q resolved against %d rounds, want no round", arg, len(rounds))
		}
	}
	if _, ok := rawRoundPosition(nil, gopilog.HTTPRoundSelector{Detail: true, Round: gopilog.LastRound}); ok {
		t.Error("an empty round list must resolve to no round")
	}
}

// TestRawRoundSelectorParseMatchesGoPiLog: the moved-pointer text for
// `gopi session log <id> --http <selector>` must now name the equivalent `gopi session raw`
// invocation — proof the two surfaces really share one grammar (before this,
// "last" and the negatives were normalized away and raw could not accept them
// at all).
func TestRawRoundSelectorParseMatchesGoPiLog(t *testing.T) {
	cases := []struct {
		httpArg string
		want    string
	}{
		{"3", "gopi session log <id> --http 3 moved out of gopi session log — use: gopi session raw <id> 3"},
		{gopilog.RoundKeywordLast, "gopi session log <id> --http last moved out of gopi session log — use: gopi session raw <id> last"},
		{"0", "gopi session log <id> --http 0 moved out of gopi session log — use: gopi session raw <id> 0"},
		{"-2", "gopi session log <id> --http -2 moved out of gopi session log — use: gopi session raw <id> -2"},
		{gopilog.RoundKeywordAll, "gopi session log <id> --http moved out of gopi session log — use: gopi session stats <id> --rounds"},
		{"", "gopi session log <id> --http moved out of gopi session log — use: gopi session stats <id> --rounds"},
	}
	for _, c := range cases {
		got := logMovedPointer(&logOptions{http: true, httpArg: c.httpArg})
		if got != c.want {
			t.Errorf("--http %q → %q\nwant %q", c.httpArg, got, c.want)
		}
	}
}

// TestRawRoundSelectorEndToEnd drives the whole CLI path — parseRawArgs then
// printRawRound — for the acceptance rounds, and compares the bytes with the
// round the shared index names: `last`/`0` print the LAST round's span and
// `-2` the 2nd-from-last. stdout purity holds (the fixture is a single log).
func TestRawRoundSelectorEndToEnd(t *testing.T) {
	id := statsFixtureSandbox(t)
	rawLog := resolveRawForSession(id)
	rounds, err := gopilog.ParseHTTPRounds(rawLog)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) < 2 {
		t.Fatalf("fixture has %d rounds, want >= 2", len(rounds))
	}
	for _, c := range []struct {
		arg  string
		want int // expected index
	}{
		{gopilog.RoundKeywordLast, len(rounds) - 1},
		{"0", len(rounds) - 1},
		{"-2", len(rounds) - 2},
	} {
		opts, err := parseRawArgs([]string{id, c.arg})
		if err != nil {
			t.Fatalf("parseRawArgs(%q): %v", c.arg, err)
		}
		if opts.sel.Round != mustSelectorRound(t, c.arg) {
			t.Fatalf("%q parsed to %+v, want the shared parser's round", c.arg, opts.sel)
		}
		got := captureStdoutStream(t, func() { printRawRound(rawLog, opts) })
		want := statsFixtureRawTranscript(t, rounds[c.want].LineStart, rounds[c.want].LineEnd)
		if got != want {
			t.Errorf("raw <round> %q did not print the expected round's span (%d..%d)\n got: %.80q\nwant: %.80q",
				c.arg, rounds[c.want].LineStart, rounds[c.want].LineEnd, got, want)
		}
	}
	// Distinct rounds, so the three selectors above cannot all be satisfied
	// by one accidental span.
	if rounds[len(rounds)-1].LineStart == rounds[len(rounds)-2].LineStart {
		t.Fatal("fixture's last two rounds share a start line — the test would prove nothing")
	}
}

// mustSelectorRound returns the Round the shared parser assigns to arg.
func mustSelectorRound(t *testing.T, arg string) int {
	t.Helper()
	sel, err := gopilog.ParseHTTPSelector(arg)
	if err != nil {
		t.Fatalf("ParseHTTPSelector(%q): %v", arg, err)
	}
	return sel.Round
}

// statsFixtureRawTranscript joins the fixture raw log's lines [first, last]
// with newlines, each line terminated — the exact byte stream printRawSpan
// emits. Lines are split the way the raw-log scanner sees them: the single
// trailing "" produced by the file's final newline is not a line, but the
// blank line before it (a round's SSE separator) IS.
func statsFixtureRawTranscript(t *testing.T, first, last int) string {
	t.Helper()
	b, err := os.ReadFile(statsFixtureRawPath(t))
	if err != nil {
		t.Fatalf("read fixture raw log: %v", err)
	}
	lines := strings.Split(string(b), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // the final newline's empty token, not a line
	}
	var sb strings.Builder
	for n := first; n <= last && n <= len(lines); n++ {
		sb.WriteString(lines[n-1])
		sb.WriteString("\n")
	}
	return sb.String()
}
