package main

// cli_session_token_total_label_test.go — the CLI face of the token-label
// correction (task 01a1954e, 2026-11-01). The tools-layer measurement is
// internal/tools/chat_token_total_vs_peak_test.go; this file proves the two
// operator-facing surfaces carry the same fact, because a label that exists
// only in the tools layer is a label nobody reads.
//
// THE DEFECT THIS CORRECTS. `session show` and `session list` printed the
// ledger's token columns under the word "peak", on the reasoning that the fold
// is `MAX(col, ?)` and therefore a high-water mark. The premise was false: the
// value folded is the CostTracker's CUMULATIVE count (one `+=` per API request,
// internal/deepseek/claude_client.go:303), so MAX returns the LAST element of a
// growing series — the session's running total. Measured on session
// d6120d04-98fa-454d-a2f0-51ea7c661956: three requests of 36455 / 36576 /
// 36674 gave a stored 109705, which is their SUM, not the 36674 peak.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// The fixture's two turns, in the LIVE path's shape (CUMULATIVE tracker totals,
// which is what the three call sites hand the recorder). After request 1 the
// tracker reads 100; after request 2 it reads 300.
//
// THE COLUMN MUST HOLD 300, AND THAT NUMBER IS THE TOTAL, NOT A PEAK. The
// per-request sides behind it are 100 and 200 — they SUM to 300 while the
// largest single one is 200 — so a row reading 300 is the running total and a
// row reading 200 would be a peak. The names below are the readings the CLI
// passes, not the quantities being distinguished (cliTokenTotalPeakSide is);
// naming a cumulative reading "peak" is exactly the mistake this file corrects.
const (
	cliTokenTotalFirstTurn  = 100 // cumulative after request 1
	cliTokenTotalSecondTurn = 300 // cumulative after request 2 == the row's value
	// cliTokenTotalPeakSide is the largest SINGLE request's prompt side
	// (request 2 alone: 300 − 100). If the ledger ever held this instead of the
	// cumulative total, the label would be wrong again.
	cliTokenTotalPeakSide = cliTokenTotalSecondTurn - cliTokenTotalFirstTurn
)

// TestSessionShowLabelsTheTokenLineAsATotal drives the REAL argv (`session show
// <id>`) and asserts on the bytes a reader sees: the key column says
// `token total`, the value carries the `total tok in=` clause, and the note
// names the fold that keeps the row from shrinking.
func TestSessionShowLabelsTheTokenLineAsATotal(t *testing.T) {
	sessionChatEnv(t)
	const id = "cli-total-label-11111111"
	sessionSeedRow(t, id, writeCLISessionJSONL(t, id))
	if err := tools.ChatRecordTurn(id, cliTokenTotalFirstTurn, 20, 90); err != nil {
		t.Fatalf("record turn 1: %v", err)
	}
	if err := tools.ChatRecordTurn(id, cliTokenTotalSecondTurn, 50, 280); err != nil {
		t.Fatalf("record turn 2: %v", err)
	}

	out := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{id}) })

	if !strings.Contains(out, sessionTotalTokensKey) {
		t.Errorf("`session show` has no %q line — a reader scanning the key column cannot tell "+
			"the number from a per-request figure:\n%s", sessionTotalTokensKey, out)
	}
	// THE KEY AND THE VALUE MUST BE SEPARATED BY A CELL, and this arm exists
	// because the shipped page violated it while every substring assertion above
	// still passed (2026-11-01). The key column was the literal width 12, the new
	// key `total tokens` is exactly 12 cells, so `%-*s%s` left no gap and the
	// page read `total tokenstotal tok in=109705` — which CONTAINS both
	// fragments, so a "does it say total" check is blind to it. Asserting the
	// joined form is what makes the defect visible; asserting the fragments is
	// not (Rule 31: a surface nobody ran is a surface nobody has seen).
	joined := sessionTotalTokensKey + fmt.Sprintf(tools.ChatTokenCountsFormat, cliTokenTotalSecondTurn, 50, 280)
	if strings.Contains(out, joined) {
		t.Errorf("the token key runs INTO its value with no separating cell:\n%s", out)
	}
	want := sessionTotalTokensKey + " " + fmt.Sprintf(tools.ChatTokenCountsFormat, cliTokenTotalSecondTurn, 50, 280)
	if !strings.Contains(out, want) {
		t.Errorf("`session show` does not print %q — the key column must leave at least one "+
			"cell before the value:\n%s", want, out)
	}
	if !strings.Contains(out, "total tok in=") {
		t.Errorf("`session show` does not label the counts as the session's total:\n%s", out)
	}
	if strings.Contains(out, "peak tok in=") || strings.Contains(out, "peak tokens") {
		t.Errorf("`session show` still carries the withdrawn `peak` wording:\n%s", out)
	}
	// The note is what a label alone cannot buy: WHY the row cannot shrink, and
	// where the per-request figures live.
	for _, line := range tools.ChatTokenBasisNote {
		if !strings.Contains(out, line) {
			t.Errorf("`session show` does not carry the total note line %q:\n%s", line, out)
		}
	}
}

// TestSessionShowJSONNamesTheQuantity is the same fact on the machine-readable
// face. The wire KEY keeps its spelling (renaming input_tokens breaks readers,
// and silently re-pointing a key's MEANING is the breakage Rule 15 forbids), so
// the payload carries a separate field naming the QUANTITY — and that field is
// what a script reads instead of guessing.
func TestSessionShowJSONNamesTheQuantity(t *testing.T) {
	sessionChatEnv(t)
	const id = "cli-total-json-22222222"
	sessionSeedRow(t, id, writeCLISessionJSONL(t, id))
	if err := tools.ChatRecordTurn(id, cliTokenTotalSecondTurn, 50, 280); err != nil {
		t.Fatalf("record turn: %v", err)
	}

	out := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{id, sessionJSONFlag}) })
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("the JSON face printed no object:\n%s", out)
	}
	var payload struct {
		InputTokens  int    `json:"input_tokens"`
		TokensBasis  string `json:"tokens_basis"`
		TokensReport bool   `json:"tokens_reported"`
	}
	if err := json.Unmarshal([]byte(out[start:]), &payload); err != nil {
		t.Fatalf("the JSON face is not one parseable object (%v):\n%s", err, out)
	}
	if payload.TokensBasis != tools.ChatTokenBasisWord {
		t.Errorf("tokens_basis = %q, want %q — the wire does not say which quantity "+
			"input_tokens holds:\n%s", payload.TokensBasis, tools.ChatTokenBasisWord, out)
	}
	if payload.TokensBasis == "per-request-peak" {
		t.Errorf("tokens_basis still carries the withdrawn value %q:\n%s",
			"per-request-peak", out)
	}
	if !payload.TokensReport {
		t.Errorf("tokens_reported = false on a row with recorded counts:\n%s", out)
	}
	if payload.InputTokens != cliTokenTotalSecondTurn {
		t.Errorf("input_tokens = %d, want the cumulative total %d:\n%s",
			payload.InputTokens, cliTokenTotalSecondTurn, out)
	}
	if payload.InputTokens == cliTokenTotalPeakSide {
		t.Errorf("input_tokens = %d equals the largest SINGLE request, not the total — this is "+
			"the quantity the withdrawn `peak` label claimed:\n%s", payload.InputTokens, out)
	}
}

// TestSessionShowTotalNoteIsAbsentWhenUnmeasured keeps the disclosure HONEST in
// the other direction: an unmeasured row already prints the unknown word as its
// value, so stacking the fold explanation on top would assert arithmetic over
// counts nothing measured. A disclosure that fires on every row stops meaning
// anything (Rule 15's sibling: silence and speech are both decisions).
func TestSessionShowTotalNoteIsAbsentWhenUnmeasured(t *testing.T) {
	sessionChatEnv(t)
	const id = "cli-total-unmeasured-33333333"
	sessionSeedRow(t, id, writeCLISessionJSONL(t, id))
	if err := tools.ChatRecordTurn(id, 0, 0, 0); err != nil {
		t.Fatalf("record silent turn: %v", err)
	}
	out := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{id}) })
	if !strings.Contains(out, tools.ChatTokensUnknownWord) {
		t.Errorf("the unmeasured row no longer says %q:\n%s", tools.ChatTokensUnknownWord, out)
	}
	for _, line := range tools.ChatTokenBasisNote {
		if strings.Contains(out, line) {
			t.Errorf("the unmeasured row carries the total note (there are no counts to fold):\n%s", out)
		}
	}
}

// TestSessionShowKeysFitTheirColumn is the CHECK the key-width comment claims,
// and it exists because the claim was written before the check did (2026-11-01:
// the comment said "now CHECKED rather than remembered" while nothing checked
// it — the same species of unfounded assertion this task exists to remove).
//
// THE CONSTRAINT. The page renders key/value lines with `%-*s%s` at
// sessionShowKeyWidth, so a key as wide as the column leaves ZERO separating
// cells and the value welds to its label. That is not hypothetical: the token
// key shipped as `total tokens` (12 cells of a 12-cell column) and the page read
//
//	total tokenstotal tok in=109705 out=262 cache=109705
//
// THE FIX IS A BUDGET, NOT A WIDER COLUMN. The page also carries lines whose
// padding is hardcoded at 12 cells (`ended`, `started`, `workdir`), so widening
// the column moves the `%-*s` half and shears the other half — measured: with
// the width at 13, TestSessionShowReportsTheLastSegmentOfAResumedSession and
// TestSessionShowSaysWhenTheLogWasNotScanned both went red on their hardcoded
// 12-cell literals. So every key must FIT; a new key that cannot is renamed.
//
// The list below is the page's key vocabulary, maintained by hand: a key is a
// package-level string constant and Go offers no reflection over those. **It is
// therefore NOT enforced that this list is complete** — a new key added to the
// page without a row here escapes the budget silently, and this comment says so
// rather than claiming a guard that does not exist (the failure mode this task
// is about). Two things keep the gap small: every key lives in ONE const block
// (`sessionRuntime*Key`), so a reviewer adding one is looking at its neighbours;
// and TestSessionShowLabelsTheTokenLineAsATotal asserts the separation on the
// one key that has actually collided.
func TestSessionShowKeysFitTheirColumn(t *testing.T) {
	keys := []string{
		sessionTotalTokensKey,
		sessionevent.CounterUnitTurn,
		sessionevent.CounterUnitHumanTurn,
		sessionRuntimeKindKey,
		sessionRuntimeModeKey,
		sessionRuntimeStateKey,
		sessionRuntimeModelKey,
		sessionRuntimeStartedKey,
		sessionRuntimeLastWriteKey,
		sessionRuntimeWorkdirKey,
		sessionRuntimeBranchKey,
		sessionRuntimeRepoKey,
		sessionRuntimeLogKey,
		sessionRuntimePIDKey,
		sessionRuntimeAliveKey,
		sessionRuntimeTaskKey,
		sessionRuntimeRunKey,
		sessionRuntimeRawKey,
		sessionRuntimeRecordKey,
		sessionRuntimeSegmentsKey,
		sessionEndedFirstKey,
	}
	for _, k := range keys {
		if len(k) > sessionShowKeyMaxKey {
			t.Errorf("key %q is %d cells; the column is %d and every key must fit in %d so ONE "+
				"separating cell survives (rename the key — widening the column shears the "+
				"page's hardcoded lines)",
				k, len(k), sessionShowKeyWidth, sessionShowKeyMaxKey)
		}
	}
}
