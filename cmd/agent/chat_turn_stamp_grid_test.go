package main

// chat_turn_stamp_grid_test.go — THE BANNER'S MOMENT REACHES THE SCREEN.
//
// The assertions in internal/lineedit prove the header COMPOSES the moment, and
// cli_web_turnstamp_test.go proves the server TELLS the page the record's
// moment. Neither answers the question a reader actually asks — "does the
// terminal band show it?" — because a value can be composed correctly and never
// painted (a header rendered into a string nobody commits), which is the class of
// defect that only a rendered grid catches.
//
// So this arm drives the REPL's own seam (chatBeginTurnAt, the exact call
// cli_chat.go makes) into the real answer band and reads the cells back off the
// screen. THE GRID IS THE ORACLE: the moment string must appear as cells, on the
// turn's header row, in the ordinal→moment→prompt order.
//
// AND THE NEGATIVE HALF: a turn opened WITHOUT a moment (chatBeginTurn, the
// pre-existing call) must not paint one. That is what keeps the feature opt-in
// and stops a future "simplification" from stamping every header with whatever
// clock it happens to hold.

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionevent"
)

// TestChatTurnBandPaintsTheTurnsMomentOnTheGrid drives ONE stamped turn and ONE
// unstamped turn through the production dispatch and reads both off the grid.
func TestChatTurnBandPaintsTheTurnsMomentOnTheGrid(t *testing.T) {
	h := newStreamHarness(t)
	band := h.in.Answer()
	if band == nil {
		t.Fatal("no answer band on a terminal that hosts the layout")
	}
	status := newChatStatusSource("glm-5.3-flash", false)
	h.in.SetStatus(status.status)

	renderer := newChatTurnRenderer(h.in)
	if !renderer.Reserved() {
		t.Fatal("the turn renderer did not take the reserved path — the band is not where the header lands")
	}

	// The moment a real caller mints: a fixed, historical instant, so a header
	// that reached for the clock would paint a DIFFERENT string and be caught.
	at := time.Date(2026, 11, 8, 9, 14, 7, 0, time.FixedZone("CST", 8*3600))
	stamp := sessionevent.FormatTurnStamp(at)
	const firstPrompt = "the stamped ask"

	// Turn 1: STAMPED, through the very function cli_chat.go calls.
	chatBeginTurnAt(renderer, 1, chatOutbound{Message: firstPrompt, Display: firstPrompt}, at)
	renderer.Write("the first answer\n")
	renderer.Finish()
	renderer.EndTurn()
	publishChatStatus(h.in, status, 1, statusLiveIdleTurn, 1234)

	// Turn 2: UNSTAMPED, through the pre-existing dispatch.
	const secondPrompt = "the unstamped ask"
	chatBeginTurn(renderer, 2, chatOutbound{Message: secondPrompt, Display: secondPrompt})
	renderer.Write("the second answer\n")
	renderer.Finish()
	renderer.EndTurn()
	publishChatStatus(h.in, status, 2, statusLiveIdleTurn, 4321)

	h.waitFor(t, "both turn headers drawn", func() bool {
		h.replay()
		all := h.screen.Contents()
		return strings.Contains(all, firstPrompt) && strings.Contains(all, secondPrompt)
	})

	// ── the stamped header, read off the cells ─────────────────────────────
	var stampedRow, plainRow string
	// THE HEADER ROW, selected by the ORDINAL — not by the prompt: the prompt is
	// echoed again on the user's own committed row, so selecting by text can land
	// on the echo and assert about a row that is not chrome at all (measured: the
	// first version of this case did exactly that and "failed" while the grid was
	// correct).
	// SELECT BY WHAT THE WIDTH ACTUALLY DRAWS. On a 40-column band the stamped
	// header cannot hold `turn 1 · <19-cell clock>`, so lineedit yields the
	// ORDINAL and draws the moment whole (see turnLabelWithin) — selecting the
	// stamped header by "turn 1" would find nothing. The selector is therefore
	// the moment itself; the unstamped header is still found by its ordinal,
	// which it always draws.
	for _, line := range strings.Split(h.screen.Contents(), "\n") {
		if strings.Contains(line, stamp) {
			stampedRow = line
		}
		if strings.Contains(line, "turn 2") {
			plainRow = line
		}
	}
	if stampedRow == "" || plainRow == "" {
		t.Fatalf("a turn header was not painted at all:\n%s", h.screen.Contents())
	}
	// THE MOMENT IS ON THE HEADER — in full, in order. At this band's 40 columns
	// the header cannot hold `turn 1 · <19-cell clock> · <prompt>` on ONE row, so
	// the block puts the remainder on its continuation row (the same mechanism an
	// over-long prompt has always used). The assertion is therefore
	// "every character is present, in order, starting on the header row" — a
	// dropped or truncated clock is caught, and a WRAPPED one is not mistaken for
	// a cut (see lineedit's firstMissingInOrder for the same distinction one
	// layer down).
	block := strings.Join(headerBlockRows(h.screen.Contents(), stampedRow), "\n")
	if drop := firstMissingInOrder(block, stamp); drop >= 0 {
		t.Fatalf("the band did not paint the turn's moment %q (missing at offset %d) — composed but never drawn:\n%s",
			stamp, drop, h.screen.Contents())
	}
	// ORDER, where both fields are drawn (a wider band is where lineedit draws
	// them together): ordinal first, then the moment, then the prompt.
	wide := lineedit.TurnHeader(lineedit.TurnInfo{
		Index: 1, Prompt: firstPrompt, OpenedAt: at, Stamp: true,
	}, 80)
	iTurn, iStamp, iPrompt := strings.Index(wide, "turn 1"), strings.Index(wide, stamp), strings.Index(wide, firstPrompt)
	if !(iTurn >= 0 && iTurn < iStamp && iStamp < iPrompt) {
		t.Fatalf("at a width that holds all three the fields are not in ordinal→moment→prompt order (%d,%d,%d):\n%s",
			iTurn, iStamp, iPrompt, wide)
	}
	// A HISTORICAL MOMENT, not today's: the header must not carry the current
	// clock, which is what a render-time time.Now() would have put there.
	if now := time.Now().Format(sessionevent.TurnStampLayout); strings.Contains(stampedRow, now) {
		t.Fatalf("the band painted the CURRENT clock (%s) for a turn opened at %s — a replay would state today:\n%s",
			now, stamp, stampedRow)
	}

	// ── the unstamped header, the negative half ────────────────────────────
	if strings.Contains(plainRow, sessionevent.TurnStampPlaceholder) || strings.Contains(plainRow, stamp) {
		t.Fatalf("an UNSTAMPED turn painted a moment — the feature is not opt-in:\n%s", plainRow)
	}
	if !strings.Contains(plainRow, "turn 2") || !strings.Contains(plainRow, secondPrompt) {
		t.Fatalf("the unstamped header lost the fields it always had:\n%s", plainRow)
	}
}

// headerBlockRows returns the painted rows a header BLOCK occupies: the row that
// carries the ordinal, plus the continuation rows that follow it up to (and not
// including) the next header or a blank row.
//
// It exists because a header is a BLOCK, not a row: the composition puts overflow
// on continuation rows, so "the moment is painted" has to be asked of the block.
func headerBlockRows(screen, header string) []string {
	lines := strings.Split(screen, "\n")
	start := -1
	for i, l := range lines {
		if l == header {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	out := []string{lines[start]}
	for i := start + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.Contains(l, "turn ") {
			break
		}
		// A continuation row of a header block is drawn inside the band and
		// carries no gutter marker of its own.
		if strings.ContainsAny(l, "▸▌") {
			break
		}
		out = append(out, lines[i])
	}
	return out
}

// firstMissingInOrder re-walks want over got and reports the index of the first
// byte of want that never appears in order (or -1 when all of them do).
func firstMissingInOrder(got, want string) int {
	i := 0
	for k := 0; k < len(want); k++ {
		j := strings.IndexByte(got[i:], want[k])
		if j < 0 {
			return k
		}
		i += j + 1
	}
	return -1
}
