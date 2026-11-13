package tools

// chat_token_total_vs_peak_test.go — THE LEDGER'S TOKEN COLUMNS ARE A RUNNING
// TOTAL, AND THIS FILE IS THE MEASUREMENT THAT SETTLED IT (task 01a1954e,
// 2026-11-01).
//
// WHAT THE PREVIOUS FILE (chat_peak_vs_sum_test.go, 9f00fd60c) CLAIMED, AND WHY
// IT WAS WRONG. It read the fold — `input_tokens = MAX(input_tokens, ?)` in
// ChatRecordTurnOutcome — and concluded the columns hold a peak, because MAX
// looks like a high-water mark. That is reading SEMANTICS OFF THE SQL'S SHAPE,
// which Rule 41 forbids, and here the shape is misleading: the value being
// folded is not a per-request size, it is the CostTracker's CUMULATIVE count.
// `CostTracker.AddClaudeUsage` (internal/deepseek/claude_client.go:303) does
// `ct.TotalPromptTokens += u.PromptSideTokens()` once per API request, and the
// recorder is handed `ag.Cost.TotalPromptTokens` whole (cmd/agent/cli_chat.go
// :4584, term.go:417, cli_web.go:1605). MAX over a monotonically increasing
// series returns its LAST element — the running total. The MAX's only job is
// idempotence: a replayed or out-of-order write must not shrink the row.
//
// THE MEASUREMENT. Session d6120d04-98fa-454d-a2f0-51ea7c661956 stored
// input_tokens 109705; its three wire requests carry prompt sides 36455 /
// 36576 / 36674:
//
//	sum 109705   max 36674   ledger 109705
//
// and session 34bb9adc-0618-4e05-b849-6bd15757dec6 stored 8086768 against the
// same 8086768 sum and a per-request max of 102312. The ledger IS the sum.
//
// WHY THE FIXTURE BELOW IS SHAPED THE WAY IT IS. Because MAX(cumulative) ==
// SUM(increments) by construction, a fixture cannot separate "total" from
// "peak" using the ledger alone — both folds land on the same number. What
// separates them is the PER-REQUEST peak, and that is what this test asserts
// against: with per-request sides of 100 and 200 the sum is 300 while the peak
// is 200, and the stored column must read 300.

import (
	"strings"
	"testing"
)

// The fixture's three quantities, named so the assertions read as the claim
// rather than as arithmetic (Rule 1: no bare literals carrying meaning).
const (
	// chatTokenTotalFixtureFirstRequest is the first API request's prompt side.
	chatTokenTotalFixtureFirstRequest = 100
	// chatTokenTotalFixtureSecondRequest is the second API request's prompt
	// side. The session's running total after it is the two added together.
	chatTokenTotalFixtureSecondRequest = 200
	// chatTokenTotalFixturePeak is the LARGER SINGLE request.
	chatTokenTotalFixturePeak = chatTokenTotalFixtureSecondRequest
)

// chatTokenTotalFixtureSum is what the ledger must hold: the CUMULATIVE total,
// which equals the per-request sum. Derived, never restated (Rule 13).
const chatTokenTotalFixtureSum = chatTokenTotalFixtureFirstRequest + chatTokenTotalFixtureSecondRequest

// TestLedgerTokenColumnsAreARunningTotal is the red proof for the correction.
//
// It drives the REAL writer (ChatRecordTurnOutcome through ChatRecordTurn) with
// the SAME shape the live CLI passes — cumulative tracker totals — and then
// reads the row back and RENDERS it, so the assertion lands on bytes a reader
// sees rather than on a constant compared with itself.
func TestLedgerTokenColumnsAreARunningTotal(t *testing.T) {
	chatTestEnv(t)
	const id = "chat-total-vs-peak-1111"
	logPath := writeChatSessionJSONL(t, id, "how many tokens is this")
	if _, err := ChatEnsureRow(ChatSession{SessionID: id, Mode: ChatModeCLI, Model: "deepseek-flash", FirstMsg: "total probe"}); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// THE LIVE PATH'S SHAPE: after request 1 the tracker holds 100; after
	// request 2 it holds 300. Those are the numbers the CLI hands the recorder.
	if err := ChatRecordTurn(id, chatTokenTotalFixtureFirstRequest, 20, 90); err != nil {
		t.Fatalf("record turn 1: %v", err)
	}
	if err := ChatRecordTurn(id, chatTokenTotalFixtureSum, 50, 280); err != nil {
		t.Fatalf("record turn 2: %v", err)
	}

	// THE PREMISE, asserted first: the two quantities this defect confused must
	// actually differ here, or every later line would hold vacuously.
	if chatTokenTotalFixturePeak == chatTokenTotalFixtureSum {
		t.Fatalf("fixture cannot separate a peak (%d) from a total (%d)",
			chatTokenTotalFixturePeak, chatTokenTotalFixtureSum)
	}

	// MONOTONICITY IS NOT WEAKENED, and this arm is why the fold stays MAX: a
	// replayed or out-of-order write carrying a SMALLER cumulative total must
	// not shrink the row.
	if err := ChatRecordTurn(id, 50, 5, 5); err != nil {
		t.Fatalf("record replayed turn: %v", err)
	}

	row, err := ChatShow(id)
	if err != nil {
		t.Fatalf("ChatShow(%s): %v", id, err)
	}
	if !row.TokensReported {
		t.Fatalf("ChatShow(%s).TokensReported = false on a row with real counts", id)
	}

	// THE CLAIM: the stored number is the SUM, not the largest single request.
	if row.InputTokens != chatTokenTotalFixtureSum {
		t.Fatalf("stored input_tokens = %d, want the running TOTAL %d — a peak would be %d",
			row.InputTokens, chatTokenTotalFixtureSum, chatTokenTotalFixturePeak)
	}
	if row.InputTokens == chatTokenTotalFixturePeak {
		t.Fatalf("stored input_tokens = %d equals the PER-REQUEST PEAK: this is the "+
			"quantity the withdrawn label claimed", row.InputTokens)
	}

	// …AND THE RENDER SAYS SO. A correct column under a wrong word is the
	// defect this task removes, so the assertion is on the rendered row.
	out, err := ChatList(ChatListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	line := sessionRowLine(t, out, id)
	if strings.Contains(line, "peak tok in=") {
		t.Errorf("the index row still calls the ledger's total a PEAK:\n%s", line)
	}
	if !strings.Contains(line, "total tok in=") {
		t.Errorf("the index row does not label the counts as the session's total:\n%s", line)
	}
	if !strings.Contains(line, logPath) && logPath != "" {
		// Not an assertion about the row — only that the fixture log this test
		// derived its numbers from is the one the env resolved.
		t.Logf("fixture log: %s", logPath)
	}
}

// TestTotalLabelSaysWhatThePeakWordGotWrong is the second half of the
// correction, and the half a label alone cannot buy: `total` says what the
// number IS, while the note has to stop a reader from re-filing the same
// defect. Two facts do that — that the fold makes the LAST write win (so the
// row cannot shrink), and where the per-request figures actually live.
//
// IT MUST NOT POINT AT A DERIVED SUMMARY, and TWO were measured wrong on
// 2026-11-01 — which is why the omission is deliberate and this test pins it.
// `session stats` and `session log`'s COST block both add up the session log's
// own rows, and on the chat paths those rows carry the CUMULATIVE reading
// (internal/agent/chat_ops.go:843), while the task loop writes the per-request
// DELTA (internal/agent/loop.go:5105). Measured on d6120d04: `session log`
// printed `Prompt: 219,191` for a session whose total is 109,705; on e1e22e9f
// `session stats` printed 250,670,748,726 against a true 266,909,263.
// `session raw` is the surface that carries the wire frames, so that is what the
// note names. The defect in the two summarisers is filed as its own row.
func TestTotalLabelSaysWhatThePeakWordGotWrong(t *testing.T) {
	note := ChatTokenBasisNoteText()
	if strings.Contains(note, "PEAK per-request") {
		t.Errorf("ChatTokenBasisNote still describes the columns as per-request peaks:\n%s", note)
	}
	if !strings.Contains(note, "RUNNING TOTALS") {
		t.Errorf("ChatTokenBasisNote does not say the columns are running totals:\n%s", note)
	}
	if !strings.Contains(note, "MAX") {
		t.Errorf("ChatTokenBasisNote does not name the fold, which is what a reader needs to "+
			"understand why the row cannot shrink:\n%s", note)
	}
	// WHERE THE PER-REQUEST FIGURES LIVE, asserted against the surface that
	// ACTUALLY has them. This assertion first named `session log`, and running
	// that verb on d6120d04 showed it prints a SUMMARY COST block — `Prompt:
	// 219,191` for a session whose total is 109,705, because it adds up rows that
	// carry CUMULATIVE readings on the chat paths. `session raw` is the one that
	// prints the wire frames (`"input_tokens":36455`), so the note must send a
	// reader there (2026-11-01).
	if !strings.Contains(note, "RAW WIRE LOG") {
		t.Errorf("ChatTokenBasisNote does not say where the per-request figures live "+
			"(it must name the raw wire log, not a derived summary):\n%s", note)
	}
	if strings.Contains(note, "session log") {
		t.Errorf("ChatTokenBasisNote points at the session log, whose totals are a sum over "+
			"CUMULATIVE rows (219,191 against a true 109,705):\n%s", note)
	}
	// The page keeps the AUTHOR's line breaks: a line that runs on gets wrapped
	// by the terminal and the explanation stops looking like one.
	if len(ChatTokenBasisNote) < 2 {
		t.Errorf("ChatTokenBasisNote is %d line(s): it is rendered in the show page's indented "+
			"slot, so a single long run wraps wherever the reader's terminal decides",
			len(ChatTokenBasisNote))
	}
	for i, ln := range ChatTokenBasisNote {
		if len(ln) > chatTokenNoteLineWidth {
			t.Errorf("ChatTokenBasisNote line %d is %d cells wide, over the %d-cell budget the "+
				"show page's indented slot holds:\n%s", i, len(ln), chatTokenNoteLineWidth, ln)
		}
	}
}

// chatTokenNoteLineWidth is the widest a note line may be: the show page indents
// it by sessionShowKeyWidth's cells under an 80-column terminal, with room to
// spare so a narrow window does not re-wrap what the author already broke.
const chatTokenNoteLineWidth = 100

// TestTokensBasisWordNamesTheQuantity pins the machine-readable spelling, so
// the JSON face and the human label cannot describe one column two ways. It is
// a VALUE a consumer keys on, which is why the correction from
// "per-request-peak" to "session-total" is asserted here rather than left to a
// changelog: a wire value that silently changes meaning is the breakage Rule 15
// forbids, and one that silently stays WRONG is worse.
func TestTokensBasisWordNamesTheQuantity(t *testing.T) {
	if ChatTokenBasisWord != "session-total" {
		t.Errorf("ChatTokenBasisWord = %q, want %q — the ledger holds the session's running "+
			"total; the previous value %q named the fold's shape and was false",
			ChatTokenBasisWord, "session-total", "per-request-peak")
	}
}
