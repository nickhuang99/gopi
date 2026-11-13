package main

// cli_session_replay_turncalls_test.go — THE HEADER'S `calls=` AND THE BODY'S
// GLYPH ROWS ARE ONE NUMBER, PINNED AT THE VERB.
//
// THE DEFECT THIS FILE PINS (measured 2026-10-26, session 80c07a7d, task
// 01a1788f): `gopi session replay <id> --turn 11` printed
// "turn 11/11  (10 request(s), 8 tool call(s))  calls=8" over a body an operator
// counting `⚙` scored ZERO — while `gopi chat --replay` of the SAME turn showed
// all eight calls with their `↳` returns. A view that prints a count it does not
// show is worse than no count: the number is the reader's only reason to believe
// the turn was rendered in full.
//
// internal/replayplay's turnbody_test.go pins the LAY-OUT (its rows carry every
// call). This file pins the SURFACE the operator runs: the header line and the
// body it heads come out of ONE invocation of the real binary, so the two halves
// cannot drift apart without a case here failing — which is exactly what the
// lay-out test alone could not catch (it never read the header).
//
// THE FIXTURE is the smallest one that can tell a TURN from its ROUNDS: ONE
// operator row answered with three exchanges, each carrying its own tool call and
// its own result. So `calls=3` over three rounds — a body that printed only one
// round's call, or that printed the calls of no round at all, fails here, and a
// header that counted rounds instead of calls is off by construction.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// turnHeaderCallsRe reads the `calls=N` field out of a turn's header line. It is
// the number the epic asserts against, read from where the verb prints it rather
// than re-derived by the test.
var turnHeaderCallsRe = regexp.MustCompile(`calls=(\d+)`)

// TestSessionReplayTurnHeaderCallsEqualsItsGlyphRows is the PAIRED assertion:
// `calls=` N ⇒ N rows opening with the tool glyph AND N rows opening with the
// result glyph, all measured on the REAL sub-verb's output.
func TestSessionReplayTurnHeaderCallsEqualsItsGlyphRows(t *testing.T) {
	home, id := replayTurnRoundFixture(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "1")
	if err != nil {
		t.Fatalf("--turn 1 failed: %v\n%s", err, out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatalf("--turn 1 printed nothing")
	}
	// THE HEADER IS THE FIRST LINE and states the count; read it, do not assume.
	m := turnHeaderCallsRe.FindStringSubmatch(lines[0])
	if m == nil {
		t.Fatalf("the turn's header carries no calls= field — a reader has no number to check the body against:\n%s", out)
	}
	declared := turnCallsField(t, m[1], out)
	if declared != replayTurnRoundRounds {
		t.Fatalf("the header declares calls=%d, want the %d calls the fixture's one turn ran — the fixture and the code disagree, not the reader:\n%s",
			declared, replayTurnRoundRounds, out)
	}
	// THE BODY is everything after that header, counted at the GLYPHS the rest of
	// the system draws a call and its return under (internal/lineedit's own
	// constants — never a literal typed here, Rule 14).
	calls := countRowsOpeningWith(lines[1:], lineedit.ToolGlyph)
	results := countRowsOpeningWith(lines[1:], strings.TrimSpace(lineedit.NoteContinuation))
	if calls != declared {
		t.Fatalf("the header declares calls=%d but the body carries %d row(s) opening with %q — the two halves of ONE view must be one number:\n%s",
			declared, calls, lineedit.ToolGlyph, out)
	}
	if results != declared {
		t.Fatalf("the header declares calls=%d but the body carries %d row(s) opening with %q — every call's return belongs with it:\n%s",
			declared, results, lineedit.NoteContinuation, out)
	}
	// AND THE RETURNS ARE THE RECORDED ONES, not empty glyph rows: the count above
	// would pass on three `↳` rows with nothing under them, which is the same
	// defect wearing the right number.
	for _, want := range []string{"result one", "result two", "result three"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the turn's body does not carry the recorded result %q — a result row count is not a result:\n%s", want, out)
		}
	}
	// The user surface's own vocabulary: no round number leaks onto `--turn`.
	if regexp.MustCompile(`(^|[^a-z])round`).MatchString(out) {
		t.Fatalf("`--turn N` mentions a round — this is the turn address space:\n%s", out)
	}
}

// TestSessionReplayTurnCallsEqualTheSumOfItsRounds is the CROSS-SURFACE half of
// the same fact: the turn's `calls=` must equal the sum of `calls=` over the
// rounds that turn spans, read from `--round N` — the internal address space.
//
// It is the assertion that would have caught the measured defect from either
// side: a turn mapped onto the WRONG rounds (dump frames instead of HTTP rounds)
// prints a header count its body cannot show, and its per-round sum disagrees with
// it — while a turn mapped correctly agrees on both.
func TestSessionReplayTurnCallsEqualTheSumOfItsRounds(t *testing.T) {
	home, id := replayTurnRoundFixture(t)
	turnOut, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "1")
	if err != nil {
		t.Fatalf("--turn 1 failed: %v\n%s", err, turnOut)
	}
	m := turnHeaderCallsRe.FindStringSubmatch(strings.Split(turnOut, "\n")[0])
	if m == nil {
		t.Fatalf("the turn's header carries no calls= field:\n%s", turnOut)
	}
	declared := turnCallsField(t, m[1], turnOut)
	sum := 0
	for r := 1; r <= replayTurnRoundRounds; r++ {
		roundOut, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", strconv.Itoa(r))
		if err != nil {
			t.Fatalf("--round %d failed: %v\n%s", r, err, roundOut)
		}
		// The round's own header is its SECOND line: the first states the unit
		// boundary (which turn this round belongs to).
		var roundCalls int
		for _, line := range strings.Split(roundOut, "\n") {
			if rm := turnHeaderCallsRe.FindStringSubmatch(line); rm != nil {
				roundCalls = turnCallsField(t, rm[1], roundOut)
				break
			}
		}
		sum += roundCalls
	}
	if sum != declared {
		t.Fatalf("the turn declares calls=%d but the %d round(s) it spans sum to %d — the turn's own count and the rounds it is made of must agree:\n%s",
			declared, replayTurnRoundRounds, sum, turnOut)
	}
}

// countRowsOpeningWith counts the lines that OPEN with prefix — the way an
// operator's `grep -c '⚙'` counts, and the way the two halves of a turn's view
// are compared.
func countRowsOpeningWith(lines []string, prefix string) int {
	n := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

// turnCallsField parses a calls= field's digits, failing loudly rather than
// reading a malformed number as zero (Rule 15: "not printed" and "was 0" must
// never read the same).
func turnCallsField(t *testing.T, digits, out string) int {
	t.Helper()
	n, err := strconv.Atoi(digits)
	if err != nil {
		t.Fatalf("calls=%q is not a number:\n%s", digits, out)
	}
	return n
}
