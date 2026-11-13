package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
)

// `gopi session log <session-id> <round>` + `--rounds`: the session round view.
//
// The two facts these tests exist to pin:
//  1. the <round> positional is the SHARED grammar (gopilog.ParseHTTPSelector)
//     — no second grammar in cmd/agent;
//  2. a session round number is NOT the raw log's ordinal: the raw log also
//     numbers sends the session JSONL never recorded, so the pairing is
//     DERIVED (by usage tokens) and absorbs the extra rounds.
//
// The fixture (tests/fixtures/log-stats-raw) is the real-log case that proves
// (2): its raw log holds 5 rounds whose output_tokens are 97/195/247/237/218,
// exactly the completion_tokens of JSONL iterations 0..4 — a +1 offset — while
// the session has 9 request events.

// logRoundFixture copies the shared real-log fixture into a fresh GOPI_HOME and
// returns the session id plus the sandboxed session file path.
func logRoundFixture(t *testing.T) (string, string) {
	t.Helper()
	id := statsFixtureSandbox(t)
	home := os.Getenv("GOPI_HOME")
	if home == "" {
		t.Fatalf("statsFixtureSandbox did not pin GOPI_HOME")
	}
	path := filepath.Join(home, ".gopi", "sessions", id+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture session not in the sandbox: %v", err)
	}
	return id, path
}

// wantSessionRoundIndex is the INDEPENDENT statement of the round-numbering
// contract: 1 = the session's first round, 0 and -1 = the last, -2 = the
// 2nd-from-last, and an out-of-range selector names no round (in BOTH
// directions). Written from the documented rule, not by calling the code under
// test — a self-referential expectation stays green under mutation.
func wantSessionRoundIndex(n, round int) (int, bool) {
	if n <= 0 {
		return 0, false
	}
	if round > n || round < -n {
		return 0, false
	}
	switch {
	case round > 0:
		return round - 1, true
	case round == 0:
		return n - 1, true
	default:
		return n + round, true
	}
}

// TestSessionRoundIndexNumberingContract drives the resolver over the whole
// grammar, both directions of the range check included.
//
// The ROUND COUNT the selector resolves against is the CANONICAL one — the
// session's `request` rows (SessionRounds) — so the fixture states that count
// (RequestEvents) rather than a content-round slice: the two are different
// numbers, and a resolver reading the wrong one is exactly what this guards.
func TestSessionRoundIndexNumberingContract(t *testing.T) {
	for _, n := range []int{1, 2, 5} {
		s := &gopilog.Session{RequestEvents: n, Iterations: make([]gopilog.Iteration, n)}
		for round := -n - 2; round <= n+2; round++ {
			sel := gopilog.HTTPRoundSelector{Detail: true, Round: round}
			ord, ok := gopilog.SessionCanonicalOrdinal(s, sel)
			wantIdx, wantOK := wantSessionRoundIndex(n, round)
			wantOrd := wantIdx + 1
			if ok != wantOK || (ok && ord != wantOrd) {
				t.Errorf("n=%d round=%d: got (%d,%v), want (%d,%v)", n, round, ord, ok, wantOrd, wantOK)
			}
		}
	}
	// A session that never reached a round has no round to name, whatever the
	// selector says — including the "last round" forms, which would otherwise
	// index an empty list.
	for _, round := range []int{1, 0, -1, -2} {
		if ord, ok := gopilog.SessionCanonicalOrdinal(&gopilog.Session{}, gopilog.HTTPRoundSelector{Detail: true, Round: round}); ok {
			t.Errorf("empty session, round=%d: got (%d,true), want no round", round, ord)
		}
	}
}

// TestParseLogArgsRoundSelectorUsesSharedGrammar: the positional <round> must
// come out of the shared parser unchanged (no positive-only rule, no local
// reject of the from-the-end spellings).
func TestParseLogArgsRoundSelectorUsesSharedGrammar(t *testing.T) {
	for _, arg := range []string{"1", "3", "0", "-1", "-2", "last", "  0  "} {
		opts, err := parseLogArgs([]string{"sess", arg})
		if err != nil {
			t.Errorf("parseLogArgs(sess %q) rejected a documented selector: %v", arg, err)
			continue
		}
		want, err := gopilog.ParseHTTPSelector(arg)
		if err != nil {
			t.Fatalf("gopilog.ParseHTTPSelector(%q): %v", arg, err)
		}
		if opts.roundSel != want {
			t.Errorf("parseLogArgs(sess %q).roundSel = %+v, want the shared parser's %+v", arg, opts.roundSel, want)
		}
		if opts.sessionID != "sess" {
			t.Errorf("parseLogArgs(sess %q).sessionID = %q, want %q", arg, opts.sessionID, "sess")
		}
	}
}

// TestParseLogArgsRoundPositionalSlotShifts: with a deliverable selector the
// selector occupies the session-id slot, so the FIRST positional is the round
// (the same shift `gopi session raw` applies).
func TestParseLogArgsRoundPositionalSlotShifts(t *testing.T) {
	opts, err := parseLogArgs([]string{"--" + sessionBranchFlag, "feat/x", "3"})
	if err != nil {
		t.Fatalf("parseLogArgs(--branch feat/x 3): %v", err)
	}
	if opts.roundSel != (gopilog.HTTPRoundSelector{Detail: true, Round: 3}) {
		t.Errorf("roundSel = %+v, want round 3", opts.roundSel)
	}
	if opts.sessionID != "" {
		t.Errorf("sessionID = %q, want \"\" (the selector names the session)", opts.sessionID)
	}
}

// TestParseLogArgsRefusesNonRoundPositional: a third positional is still a
// usage error, and a first-positional-with-selector that is not a round is
// refused BY NAME (never silently treated as a session id).
func TestParseLogArgsRefusesNonRoundPositional(t *testing.T) {
	if _, err := parseLogArgs([]string{"s1", "2", "3"}); err == nil || !strings.Contains(err.Error(), "unexpected extra argument") {
		t.Errorf("three positionals: want an extra-argument error, got %v", err)
	}
	// WITH a deliverable selector the session is already named, so a non-round
	// positional is read as the SAME input spelled the other way — the verb's
	// pre-existing one-input refusal, which names the selector flag and the
	// conflict. (A round-looking token in that slot IS the round; see the test
	// above.)
	_, err := parseLogArgs([]string{"--" + sessionBranchFlag, "feat/x", "sess-1"})
	if err == nil {
		t.Fatalf("selector + a second input was accepted")
	}
	if !strings.Contains(err.Error(), "--"+sessionBranchFlag) || !strings.Contains(err.Error(), "SAME input") {
		t.Errorf("selector + a second input: want the one-input refusal naming --%s and the conflict, got %v", sessionBranchFlag, err)
	}
}

// TestParseLogArgsRefusesListingKeywordAsRound: `all` names no single round —
// it is refused with the pointer to the listing, never read as "the whole
// session" or as round 0. The positional <round> numbers ROUNDS (the Nth API
// call = one HTTP request/response — the operator's ruling, 2026-10-12), so the
// refusal says "round" and points at the ROUND table (--rounds) — the same unit
// and the same corpus the positional itself numbers.
func TestParseLogArgsRefusesListingKeywordAsRound(t *testing.T) {
	_, err := parseLogArgs([]string{"sess", gopilog.RoundKeywordAll})
	if err == nil {
		t.Fatalf("<round>=%q was accepted", gopilog.RoundKeywordAll)
	}
	if !strings.Contains(err.Error(), "must name one round") {
		t.Errorf("refusal does not say a round must be named: %v", err)
	}
	if !strings.Contains(err.Error(), "--"+logFlagRounds) {
		t.Errorf("refusal does not point at --%s, the table that lists the very rounds the positional numbers: %v", logFlagRounds, err)
	}
	// The pointer must NOT be the TURN table: turns are a DIFFERENT quantity
	// (operator rows), so a reader sent there to find "the rounds this session
	// has" would get the neighbour's numbering and a wrong answer.
	if strings.Contains(err.Error(), "--"+logFlagTurns) {
		t.Errorf("refusal points at --%s, the TURN table — a different quantity from the rounds the positional numbers: %v", logFlagTurns, err)
	}
}

// TestParseLogArgsRoundCombinations: a round is one API call, so the modes that
// describe a whole session are refused beside it — each by its own name.
func TestParseLogArgsRoundCombinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"round + --chat", []string{"sess", "3", "--chat"}, "--chat"},
		{"round + --all", []string{"sess", "3", "--all"}, "--all"},
		{"round + --rounds", []string{"sess", "3", "--rounds"}, "--rounds"},
		{"round + --turns", []string{"sess", "3", "--turns"}, "--turns"},
		{"round + --last", []string{"sess", "3", "--last"}, "--last"},
		{"--rounds + --all", []string{"sess", "--rounds", "--all"}, "--all"},
		{"--turns + --all", []string{"sess", "--turns", "--all"}, "--all"},
		{"--rounds + --turns", []string{"sess", "--rounds", "--turns"}, "--turns"},
	} {
		_, err := parseLogArgs(tc.args)
		if err == nil {
			t.Errorf("%s: accepted, want a refusal naming %s", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal %q does not name %s", tc.name, err, tc.want)
		}
	}
}

// TestParseLogArgsRoundNestsWithViewFlags: the round is a SCOPE, so the
// per-content flags (--thinking/--response/--memory/--filter) still apply —
// that is the combination the feature exists for.
func TestParseLogArgsRoundNestsWithViewFlags(t *testing.T) {
	opts, err := parseLogArgs([]string{"sess", "3", "--thinking", "--filter", "bash"})
	if err != nil {
		t.Fatalf("round + --thinking + --filter: %v", err)
	}
	if !opts.thinking || len(opts.filterNames) != 1 || opts.filterNames[0] != "bash" {
		t.Errorf("view flags lost beside a round: %+v", opts)
	}
	opts, err = parseLogArgs([]string{"sess", "3", "--request"})
	if err != nil || !opts.request {
		t.Errorf("round + --request: opts=%+v err=%v", opts, err)
	}
	opts, err = parseLogArgs([]string{"sess", "--rounds"})
	if err != nil || !opts.rounds {
		t.Errorf("--rounds: opts=%+v err=%v", opts, err)
	}
}

// TestParseLogArgsEmptyStillParses (regression pin): a bare `gopi session log` and a
// lone session id must keep parsing to zero round selection — the round
// positional must never make an argument optional-mandatory.
func TestParseLogArgsEmptyStillParses(t *testing.T) {
	for _, args := range [][]string{nil, {"sess"}} {
		opts, err := parseLogArgs(args)
		if err != nil {
			t.Fatalf("parseLogArgs(%v): %v", args, err)
		}
		if opts.roundSel.Detail || opts.roundSel.All {
			t.Errorf("parseLogArgs(%v): roundSel = %+v, want no round named", args, opts.roundSel)
		}
	}
}

// TestSessionRoundRawOrdinalsAbsorbsExtraRounds: the pairing must survive the
// raw log's extra rounds wherever they sit. Three session rounds (output tokens
// 10/20/30) are paired against a raw log that carries EXTRA rounds at the front
// (a meta call, tokens 5) and in the middle (a retry, tokens 7) — a positional
// or fixed-offset pairing would name the wrong round for every round.
func TestSessionRoundRawOrdinalsAbsorbsExtraRounds(t *testing.T) {
	raw := func(n, out int) gopilog.HTTPRound {
		return gopilog.HTTPRound{Number: n, Response: gopilog.RoundResponse{OutputTokens: out}}
	}
	rounds := []gopilog.HTTPRound{raw(1, 5), raw(2, 10), raw(3, 7), raw(4, 20), raw(5, 30)}
	s := &gopilog.Session{Iterations: []gopilog.Iteration{
		{CompletionTokens: 10}, {CompletionTokens: 20}, {CompletionTokens: 30},
	}}
	ords := gopilog.SessionRoundRawOrdinals(s, rounds)
	want := []int{2, 4, 5} // raw ordinals carrying 10 / 20 / 30
	for i, w := range want {
		if ords[i].Raw != w || !ords[i].Exact {
			t.Errorf("round %d: paired raw %d (exact=%v, index=%d), want raw %d confirmed by tokens",
				i+1, ords[i].Raw, ords[i].Exact, ords[i].Index, w)
		}
	}
}

// TestSessionRoundRawOrdinalsNoUsageFallsBackPositionally: with no usage in the
// session log there is nothing to confirm a pair, so the pairing is positional
// and says so (Exact=false) rather than claiming a match.
func TestSessionRoundRawOrdinalsNoUsageFallsBackPositionally(t *testing.T) {
	rounds := []gopilog.HTTPRound{{Number: 1}, {Number: 2}, {Number: 3}}
	s := &gopilog.Session{Iterations: []gopilog.Iteration{{}, {}}}
	ords := gopilog.SessionRoundRawOrdinals(s, rounds)
	for i, o := range ords {
		if o.Raw != i+1 || o.Exact {
			t.Errorf("round %d: got raw %d exact=%v, want raw %d positional", i+1, o.Raw, o.Exact, i+1)
		}
	}
}

// TestGopiLogRoundEndToEnd drives the real CLI path on the real-log fixture:
// renderSessionRound for round 1 (the session's FIRST round, which the fixture's
// raw log holds as raw round 1 — a +1-offset session) and printSessionRounds for
// the whole table, where the round past the raw log's end must be DASHED, not
// silently pointed at another round.
func TestGopiLogRoundEndToEnd(t *testing.T) {
	id, path := logRoundFixture(t)
	s, err := gopilog.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	n := gopilog.SessionRounds(s)
	if n < 3 {
		t.Fatalf("fixture session has %d rounds, want >= 3", n)
	}

	out := captureStdoutStream(t, func() {
		renderSessionRound(path, &logOptions{roundSel: gopilog.HTTPRoundSelector{Detail: true, Round: 1}})
	})
	if !strings.Contains(out, "ROUND 1 of "+strconv.Itoa(n)) {
		t.Errorf("round 1 header missing: %q", roundHeadLines(out, 3))
	}
	if !strings.Contains(out, id) {
		t.Errorf("round 1 does not name the session %s", id)
	}
	if !strings.Contains(out, "TOOL CALLS") && !strings.Contains(out, "TEXT:") {
		t.Errorf("round 1 rendered no content:\n%s", roundHeadLines(out, 12))
	}

	// The last round, named the from-the-end way, must be the same round as the
	// Nth — the two spellings cannot mean different rounds.
	lastByNumber := captureStdoutStream(t, func() {
		renderSessionRound(path, &logOptions{roundSel: gopilog.HTTPRoundSelector{Detail: true, Round: n}})
	})
	lastByName := captureStdoutStream(t, func() {
		renderSessionRound(path, &logOptions{roundSel: gopilog.HTTPRoundSelector{Detail: true, Round: gopilog.LastRound}})
	})
	if lastByNumber != lastByName {
		t.Errorf("round %d and round %s rendered different output", n, gopilog.RoundKeywordLast)
	}

	table := captureStdoutStream(t, func() { printSessionRounds(path) })
	if !strings.Contains(table, gopilog.SessionRoundColumnWord()) || !strings.Contains(table, "RAW") {
		t.Fatalf("--rounds table missing its columns:\n%s", table)
	}
	// The fixture's raw log holds 5 rounds and the session has more: the rounds
	// past the raw log's end carry "-" in the raw ordinal column (no fabricated
	// ordinal), and the table still lists a row per session round.
	if !strings.Contains(table, strconv.Itoa(n)+" ") && !strings.Contains(table, " "+strconv.Itoa(n)+" ") {
		t.Errorf("table has no row for the last round %d:\n%s", n, table)
	}
	if !strings.Contains(table, "-") {
		t.Errorf("table dashed no raw column despite rounds past the raw log's end:\n%s", table)
	}
}

// TestGopiLogRoundByteViewIsRawBytes: `gopi session log <id> <round> request` must print
// the round's request body BYTES — the same bytes `gopi session raw` prints for the raw
// ordinal the pairing resolved — so both verbs answer "what went on the wire"
// identically for the same round.
func TestGopiLogRoundByteViewIsRawBytes(t *testing.T) {
	_, path := logRoundFixture(t)
	rawLog := resolveRawLogOrExit(&gopilog.Session{ID: "fixture"}, path)
	rounds, err := gopilog.ParseHTTPRounds(rawLog)
	if err != nil || len(rounds) == 0 {
		t.Fatalf("ParseHTTPRounds(%s): %v (%d rounds)", rawLog, err, len(rounds))
	}
	wantBody, ok := gopilog.RawRoundRequest(&rounds[0])
	if !ok {
		t.Fatalf("fixture raw round 1 logged no request body")
	}
	// printSessionRoundBytes takes the CANONICAL round ordinal (1-based), not a
	// 0-based index into the reader's content rounds: the two stopped being the
	// same number when "round" is pinned to the `request` row (a round that
	// carries no content still has an ordinal, and the content index skips it).
	got := captureStdoutStream(t, func() {
		printSessionRoundBytes(
			&gopilog.Session{ID: "fixture", RequestEvents: 1, Iterations: []gopilog.Iteration{{CompletionTokens: 97}}},
			path, 1, &logOptions{request: true})
	})
	if strings.TrimRight(got, "\n") != strings.TrimRight(wantBody, "\n") {
		t.Errorf("round 1 --request differs from the raw body:\n got: %.120q\nwant: %.120q", got, wantBody)
	}
}

// roundHeadLines is a test-side diagnostic helper: the first n lines of s.
func roundHeadLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// TestParseLogArgsRoundLastFormStillAPair (the trap this change had to avoid):
// `last` is BOTH a round selector and an existing flag, so a parse must never
// silently turn `--last` into a round or vice versa. `--last` with no value is
// the cross-session selector (roundSel stays unset); `last` as a positional is
// the round selector (lastN stays 0).
func TestParseLogArgsRoundLastFormStillAPair(t *testing.T) {
	opts, err := parseLogArgs([]string{"--last"})
	if err != nil {
		t.Fatalf("parseLogArgs(--last): %v", err)
	}
	if opts.lastN <= 0 {
		t.Errorf("--last did not set the cross-session selector: %+v", opts)
	}
	if opts.roundSel.Detail {
		t.Errorf("--last was read as a round selector: %+v", opts.roundSel)
	}
	opts, err = parseLogArgs([]string{"sess", gopilog.RoundKeywordLast})
	if err != nil {
		t.Fatalf("parseLogArgs(sess last): %v", err)
	}
	if opts.roundSel != (gopilog.HTTPRoundSelector{Detail: true, Round: gopilog.LastRound}) {
		t.Errorf("positional %q = %+v, want the shared last-round selector", gopilog.RoundKeywordLast, opts.roundSel)
	}
	if opts.lastN != 0 {
		t.Errorf("the positional %q set --last's count to %d", gopilog.RoundKeywordLast, opts.lastN)
	}
}
