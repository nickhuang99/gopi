package main

// session_log_turns_test.go — THE THIRD SURFACE, PINNED TO THE SAME NUMBER.
//
// THE DEFECT (epic, measured 2026-10-27 on session
// a60787b0-9081-451f-bd5a-9e396cfc048b): `gopi session show <id>` and
// `gopi session list --mode chat` print `turns 16` — the LEDGER's
// `chats.turn_count` — while `gopi session log <id>` printed NO turn count at
// all and its two tables led with `human turns 8`. Two screens, one word, two
// different numbers, and the log surface (the one a reader opens to see the
// session) was the one that never showed the total.
//
// WHY IT COULD NOT: the count is a column in the chat index, and
// internal/gopilog cannot import internal/tools (the cycle runs
// tools → gopilog → agent → tools; the restriction is documented in
// internal/tools/chat_turns.go). The renderer therefore DECLARES the reader and
// the binary INJECTS it (gopilog.InstallLedgerTurnCount, published by
// installLedgerTurnCounter at both entry points).
//
// WHAT THIS FILE PINS, on the smallest fixture that can see the defect — a chats
// row whose recorded turn_count is 18 beside a log holding 11 operator rows:
//
//	(1) the log's COST block prints `turns: 18` — the ledger's number, under the
//	    bare word — and `human turns: 11` as the named subset;
//	(2) that number is the SAME as the one `session show` and `session list`
//	    print, compared as NUMBERS rather than as strings, because three surfaces
//	    agreeing by accident is exactly what the epic is about;
//	(3) with no reader installed the log says the count was NOT MEASURED, with
//	    the reason — and never prints the subset under the bare word (Rule 15).

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// sessionClauseRe builds the pattern for one `<word><padding><number>` clause.
// TWO PAGES SPELL THE SAME COUNTER and they pad differently — the log writes
// `human turns: 11` and the show page writes its padded key column
// (`human turns  11`) — so a substring search pins neither page; the pattern is
// the ONE spelling both are tested with (Rule 14).
func sessionClauseRe(unit string, n int) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(unit) + `:?\s+` + regexp.QuoteMeta(fmt.Sprint(n)) + `\b`)
}

// turnClauseRe pulls the number out of a `<word> <number>` clause, ON A WORD
// BOUNDARY — "human turns 11" contains "turns 11", so a substring search would
// read the SUBSET as the total (the trap the sibling file's stripQualified
// comment records).
func turnCountFrom(t *testing.T, label, text string) int {
	t.Helper()
	// Drop the qualified clause (and its number) before looking for the bare
	// one: removing the WORDS alone would leave `11` behind and the bare clause
	// would then match a number the qualified line owned.
	bare := regexp.MustCompile(regexp.QuoteMeta(sessionevent.CounterUnitHumanTurn)+`:?\s+\d+\b`).ReplaceAllString(text, "")
	re := regexp.MustCompile(regexp.QuoteMeta(sessionevent.CounterUnitTurn) + `[: ]+(\d+)`)
	m := re.FindStringSubmatch(bare)
	if m == nil {
		t.Fatalf("%s prints no `%s <number>` clause:\n%s", label, sessionevent.CounterUnitTurn, text)
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
		t.Fatalf("%s: cannot read %q as a number: %v", label, m[1], err)
	}
	return n
}

// TestSessionLogPrintsTheLedgerTurnCount is THE epic's third-surface arm: the
// show page and the index row report ONE number under ONE word, and the
// person-only subset keeps its own word.
//
// THE LOG IS NO LONGER ONE OF THOSE SURFACES (operator's ruling, 2026-11-04).
// This test used to read the count out of `gopi session log`, whose tail
// carried a CALCULATED `COST:` block; that block moved to the record-backed
// view (`gopi session show`), so the counter is asserted on the two surfaces
// that still print it. The claim it pins is unchanged — one number, one word,
// the LEDGER's — and internal/gopilog/cost_test.go now pins the other half
// (the log carries the pointer, not the numbers).
func TestSessionLogPrintsTheLedgerTurnCount(t *testing.T) {
	if sessionListTurnsFixtureRecorded == sessionListTurnsFixtureOperator {
		t.Fatalf("the fixture's two counts are equal (%d) — it cannot see the defect this test exists for",
			sessionListTurnsFixtureRecorded)
	}
	if !gopilog.LedgerTurnCountInstalled() {
		t.Fatal("no ledger turn reader is installed in this process — the suite's TestMain must call installLedgerTurnCounter(), otherwise the log renders the UNMEASURED branch and this test proves nothing")
	}
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, sessionListTurnsFixtureOperator)
	seedTurnsFixtureRow(t, sessionListTurnsFixtureID, path)

	// (1) THE LOG — `gopi session log <full-id>`, the default text view.
	// runLogSubcommand IS the verb's dispatch (the same function the CLI's
	// `session log` arm calls); it exits non-zero on its own failure paths, so a
	// fixture that could not be read fails this test rather than passing it.
	// It is RUN not for a count but for the ABSENCE: the counters moved to the
	// show page, and a log that grew them back is the defect returning.
	logOut := captureStdout(t, func() { runLogSubcommand([]string{sessionListTurnsFixtureID}) })
	if strings.Contains(logOut, gopilog.CostBlockHeader) {
		t.Errorf("the log still renders the calculated %q block — those counters belong on the record-backed page:\n%s",
			gopilog.CostBlockHeader, logOut)
	}

	// (2) THE TWO SURFACES THAT PRINT THE COUNT, through their own run functions.
	showOut := captureStdout(t, func() { runSessionShow([]string{sessionListTurnsFixtureID}) })
	listOut := captureStdout(t, func() {
		if code := runSessionList([]string{sessionModeFlag, tools.ChatModeCLI}); code != 0 {
			t.Fatalf("runSessionList = %d, want 0", code)
		}
	})
	listRow := ""
	for _, line := range strings.Split(listOut, "\n") {
		if strings.Contains(line, sessionListTurnsFixtureID) {
			listRow = line
			break
		}
	}
	if listRow == "" {
		t.Fatalf("the seeded session is missing from the index:\n%s", listOut)
	}

	// THE CLAIM (epic 2026-11-04): one number under the BARE word on the surfaces
	// that print it, and it is the LOG'S OPENED COUNT (the 11 operator rows) —
	// because `--turn N` and every replay band are POSITIONS, and only a position
	// count can carry one. The ledger's 18 is still published, under its own word,
	// on every surface that prints the pair; it must never sit under the bare word.
	//
	// WHY IT MOVED FROM THE LEDGER'S NUMBER. The ledger advances when a turn
	// COMPLETES, the log records a seam when one OPENS — different gates. Measured
	// on session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: `session list` printed
	// `turns 29` while `session replay --turns` printed 30 rows and the replay
	// screen's last band read `──── turn 30`.
	//
	// TWO SURFACES, NOT THREE (delivery rebase, 2026-11-04): this row's other half
	// shipped alongside master's `3ed4186f`, which moved the CALCULATED counters
	// off the log's tail onto the record-backed page. The log now prints the
	// POINTER (asserted as an ABSENCE in (1) above), so the count is read from the
	// two surfaces that still carry it — the epic's claim is unchanged, the number
	// simply has one fewer place to be printed, not one fewer place to be pinned.
	showTurns := turnCountFrom(t, "the show page", showOut)
	listTurns := turnCountFrom(t, "the index row", listRow)
	for _, c := range []struct {
		surface string
		got     int
	}{{"session show", showTurns}, {"session list", listTurns}} {
		if c.got != sessionListTurnsFixtureOperator {
			t.Errorf("%s reports %d turns under the bare word, want the log's opened count %d — the surfaces must "+
				"agree on ONE number, and it must be the axis `--turn N` addresses",
				c.surface, c.got, sessionListTurnsFixtureOperator)
		}
	}

	// THE LEDGER'S COMPLETED COUNT SURVIVES UNDER ITS OWN WORD on the per-session
	// face — nothing was deleted, only named. (The INDEX row keeps one number: the
	// 2026-10-27 ruling that a one-line index must not carry two turn counts still
	// holds, and the completed count rides the surface a reader opens to ask about
	// THIS session. The log is no longer one of those surfaces: master's
	// `3ed4186f` moved the counters off it, so only the show page is checked
	// here.)
	{
		want := sessionClauseRe(sessionevent.CounterUnitTurnCompleted, sessionListTurnsFixtureRecorded)
		if !want.MatchString(showOut) {
			t.Errorf("the show page does not report the ledger's completed count %d under %q — the gap must be "+
				"accounted in numbers:\n%s", sessionListTurnsFixtureRecorded,
				sessionevent.CounterUnitTurnCompleted, showOut)
		}
	}

	// THE SUBSET SURVIVES UNDER ITS OWN WORD (Rule 14 / Rule 15: a real fact is
	// named, not deleted) — on the page that carries the counters. The show page
	// pads its key column rather than using the log's `word: value` shape, so the
	// clause is matched with the padding allowed
	// (sessionClauseRe is the ONE spelling of that pattern for both pages).
	wantSubset := sessionClauseRe(sessionevent.CounterUnitHumanTurn, sessionListTurnsFixtureOperator)
	if !wantSubset.MatchString(showOut) {
		t.Errorf("the show page does not report the person-only subset %d under %q:\n%s",
			sessionListTurnsFixtureOperator, sessionevent.CounterUnitHumanTurn, showOut)
	}

	// THE SUBSTITUTION IS GONE: no surface may print the LEDGER's completed count
	// (18) under the BARE turn word. That is the defect this row exists for — the
	// bare word is the axis `--turn N` addresses, and the ledger's number rides its
	// own word beside it (asserted above).
	//
	// IT IS NOT THE SUBSET THAT IS CHECKED HERE ANY MORE (delivery rebase,
	// 2026-11-04): the epic moved the bare word from the ledger's count to the
	// log's opened count, and on this fixture the opened count and the person-only
	// subset are the same number (11) — by design, because what the fixture must
	// keep apart is 18 from 11. A "subset under the bare word" arm would therefore
	// fire on the CORRECT output. The forbidden pairing is ledger-under-bare.
	//
	// THE SHOW PAGE IS THE SURFACE THAT CARRIES THE PAIR: master's `3ed4186f` moved
	// the counters off the log, so this is where the two words print side by side.
	bare := wantSubset.ReplaceAllString(showOut, "")
	if sessionClauseRe(sessionevent.CounterUnitTurn, sessionListTurnsFixtureRecorded).MatchString(bare) {
		t.Errorf("the show page prints the LEDGER's completed count %d under the bare turn word — that is the "+
			"substitution this row must never make:\n%s", sessionListTurnsFixtureRecorded, showOut)
	}
}

// TestSessionLogStatesAnUnmeasuredTurnCount is the other arm (Rule 15): with no
// ledger reader published the SURFACE THAT PRINTS THE COUNT says the total was
// not measured and names the reason, and it never borrows the bare word for the
// subset.
//
// THE SURFACE IS THE SHOW PAGE (operator's ruling, 2026-11-04): the counters
// moved off `session log`, so the absence must be stated where the number now
// lives. Retargeted rather than deleted — deleting it would drop the only arm
// that proves an unmeasured count is STATED and not silently rendered as 0.
func TestSessionLogStatesAnUnmeasuredTurnCount(t *testing.T) {
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, sessionListTurnsFixtureOperator)
	seedTurnsFixtureRow(t, sessionListTurnsFixtureID, path)

	gopilog.InstallLedgerTurnCount(nil)
	t.Cleanup(installLedgerTurnCounter)

	showOut := captureStdout(t, func() { runSessionShow([]string{sessionListTurnsFixtureID}) })

	// An unmeasured count is a WORD (sessionShowUnmeasured), never a fabricated
	// 0 beside a real number: the reader must be able to tell "nobody looked"
	// from "the session ran zero turns".
	if !strings.Contains(showOut, sessionShowUnmeasured) {
		t.Errorf("with no ledger reader the show page must state the count was not measured (%q):\n%s",
			sessionShowUnmeasured, showOut)
	}
	// The word-boundary trap, again: "human turns: 11" CONTAINS "turns: 11", so
	// the qualified clause is removed before the bare one is looked for.
	qualified := sessionevent.CounterUnitHumanTurn + ": " + fmt.Sprint(sessionListTurnsFixtureOperator)
	if strings.Contains(strings.ReplaceAll(showOut, qualified, ""), sessionevent.CounterUnitTurn+": "+fmt.Sprint(sessionListTurnsFixtureOperator)) {
		t.Errorf("the unmeasured page printed the SUBSET under the bare turn word instead of stating the absence:\n%s", showOut)
	}
}
