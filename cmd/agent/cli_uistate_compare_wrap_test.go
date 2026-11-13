package main

// cli_uistate_compare_wrap_test.go — the reported false paint loss, pinned at the
// verb it was measured from.
//
// THE REPORT (task 01a0e4f5, real 64x34 pty, kept dump
// ~/.gopi/sessions/01a0e49e-6730-755a-a6ed-11dc0dfa5d96):
//
//	$ gopi ui_state --compare "<a 102-rune line the grid painted verbatim>" --target chat
//	#gopi-ui-state-compare ... outcome=disagreed match=false
//	  mismatch grid_row=-1 in_grid=false in_transcript=true damage=text_wrapped
//	  ... prefix=36 reason="the line is on the grid WRAPPED ..."        EXIT=3
//
// TWO FACTS CONTRADICTED EACH OTHER INSIDE THAT ONE RECORD. The reason said the text
// ARRIVED (wrapped) and the status said the paint lost it; and `prefix=36` was the
// FIRST GRID ROW'S own text length (the row carries `▌ ` plus 34 runes of a 36-rune
// line), not a prefix of the 102-rune line — the grid carried all 102, reassembled by
// the join from rows 10..12. The SAME dump's whole-answer audit (`--all`) counted that
// line `rejoined=1`, so one artifact had two contradicting faces.
//
// WHAT IS ASSERTED HERE, through the VERB (never by re-deriving a verdict in the test):
//
//  1. a line the grid painted verbatim across a mid-word wrap comes back from
//     `--compare` as an ARRIVAL — exit 0, outcome=agreed, match=true — and its
//     `prefix` is measured in the LINE'S OWN rune space (the whole line);
//  2. the two faces agree on that line: `--all` on the SAME fixture reports no lost
//     text and counts the line rejoined;
//  3. the RED direction: a line the grid really did not paint still exits 3 with a
//     loss class — the fix must not become a blanket excuse.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// wrapLinePrefix is CJK text wide enough to push the line past the band's width, and
// wrapLineToken is a single unbroken ASCII word longer than what is left of the row —
// so the band's wrap lands INSIDE the word, which is the shape the report measured
// (a wrap the ROW-EXACT rule cannot see, because the row the line opens on carries
// only its head).
const (
	wrapLinePrefix = "自观测闭环：一条比窗口更宽的答案行必须逐字符还原，包括超长词："
	wrapLineToken  = "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"
)

// wrapLine is the line the fixture paints and the transcript carries: wider than the
// band in CELLS, and wider than the band in RUNES too, so the wrap cannot be mistaken
// for the wide-rune case.
func wrapLine() string { return wrapLinePrefix + wrapLineToken }

// wrapCompareFixture paints ONE line through the PRODUCTION band on a REAL pty (the
// same fixture the whole-answer cases use: production editor, production sink, the
// geometry asserted against the pty's own answer) and returns the session path the
// verb's derived authority resolves to, plus the dump path.
func wrapCompareFixture(t *testing.T) (session, dump string) {
	t.Helper()
	dump, session = auditFixture(t, []string{wrapLine()}, []string{wrapLine()})
	return session, dump
}

// wrapCompareAssertFixturePremise is the fixture's own premise, read back through the
// reader the verb uses: the line is WIDER than the grid, no single grid row carries it
// (so the row-exact rule cannot match it and the join is what decides), and the wrap
// really did land MID-WORD — one row ends inside the token and the next row continues
// it. A fixture that stopped exercising the wrap would otherwise go green silently.
func wrapCompareAssertFixturePremise(t *testing.T, dump string) {
	t.Helper()
	cand, err := uiclient.ReadDump(dump)
	if err != nil {
		t.Fatalf("the fixture dump does not read back: %v", err)
	}
	line := wrapLine()
	if lineedit.DisplayWidth(line) <= cand.Dump.Cols {
		t.Fatalf("the fixture line is %d cells at a %d-cell grid: it cannot wrap, so the case proves nothing",
			lineedit.DisplayWidth(line), cand.Dump.Cols)
	}
	for _, row := range cand.Dump.Lines {
		if strings.Contains(row, line) {
			t.Fatalf("a single grid row carries the whole line, so the fixture does not exercise the wrap: %q", row)
		}
	}
	// The MID-WORD evidence: some row ends with a prefix of the token that is shorter
	// than the token, and the row after it continues with the token's remainder.
	split := false
	for i := 0; i+1 < len(cand.Dump.Lines); i++ {
		tail := strings.TrimRight(cand.Dump.Lines[i], " ")
		next := strings.TrimRight(cand.Dump.Lines[i+1], " ")
		for cut := 1; cut < len(wrapLineToken); cut++ {
			if strings.HasSuffix(tail, wrapLineToken[:cut]) && strings.HasPrefix(next, wrapLineToken[cut:]) {
				split = true
			}
		}
	}
	if !split {
		t.Fatalf("the wrap did not land inside the token: the case is about a MID-WORD wrap and this grid's wrap is elsewhere:\n%s",
			strings.Join(cand.Dump.Lines, "\n"))
	}
}

// TestUIStateCompare_MidWordWrapIsNotPaintLoss is the reported defect, through the
// verb: the line the grid painted verbatim across a mid-word wrap is an ARRIVAL, its
// prefix is the line's own length, and the same fixture's whole-answer audit agrees.
//
// RED BEFORE THE FIX: `--compare` never asked the join instrument, so this line fell
// through to the loss classes and printed `outcome=disagreed ... prefix=<first row>`
// with exit 3 (measured: 36 of 102 runes on the report's own kept dump), while `--all`
// counted the same line rejoined — one artifact, two contradicting faces.
func TestUIStateCompare_MidWordWrapIsNotPaintLoss(t *testing.T) {
	session, dump := wrapCompareFixture(t)
	wrapCompareAssertFixturePremise(t, dump)

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, wrapLine(),
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != uiStateExitAgreed {
		t.Fatalf("exit = %d, want %d: the grid painted this line verbatim across a mid-word wrap\noutput:\n%s",
			code, uiStateExitAgreed, out)
	}
	for _, want := range []string{
		"outcome=" + uiStateAuditWordAgreed,
		"match=true",
		string(uiclient.CompareMatch),
		// THE PREFIX IS IN THE LINE'S OWN RUNE SPACE — the whole line, not the first
		// row's own text length.
		"prefix=" + strconv.Itoa(len([]rune(wrapLine()))),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the verdict does not carry %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, string(uiclient.DamagePaintLoss)) || strings.Contains(out, string(uiclient.DamageWrapArtifactOrLoss)) {
		t.Errorf("the verdict carries a loss class for a line the grid painted whole:\n%s", out)
	}

	// THE TWO FACES AGREE, on the same artifact: the whole-answer audit counts this
	// line as arrived (rejoined) and loses no text. The defect was precisely that the
	// ledger said arrived while the one-line face said lost, so asserting both here is
	// what makes this case about the CONTRADICTION rather than about one face.
	auditOut, auditCode := uiStateRun(t, auditArgs(session))
	if auditCode != uiStateExitAgreed {
		t.Fatalf("the whole-answer face exits %d on the same fixture\noutput:\n%s", auditCode, auditOut)
	}
	for _, want := range []string{
		"outcome=" + uiStateAuditWordAgreed,
		"rejoined=1",
		uiStateFieldLostText + "=0",
		"disagreed=0",
	} {
		if !strings.Contains(auditOut, want) {
			t.Errorf("the ledger does not carry %q:\n%s", want, auditOut)
		}
	}
}

// TestUIStateCompare_LostLineStillExitsThree is the ANTI-WIDENING control, and the
// reason the fix asks the join instead of widening containment: a line the grid never
// painted still comes back a mismatch, exit 3. Without this the case above could be
// satisfied by a compare that stopped reporting losses at all.
func TestUIStateCompare_LostLineStillExitsThree(t *testing.T) {
	session, _ := wrapCompareFixture(t)
	// The SAME fixture, a line that is NOT on it, and that is wider than the grid: the
	// honest class is "a wrap explains its absence as well as a loss does", and the
	// exit code is the disagreement code. The join is what must refuse to claim it.
	lost := wrapLinePrefix + strings.Repeat("zyxwvu", 8)
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, lost,
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit = %d, want %d: this line is on no row and no join reassembles it\noutput:\n%s",
			code, uiStateExitCompareMismatch, out)
	}
	if !strings.Contains(out, "outcome="+uiStateAuditWordDisagreed) {
		t.Fatalf("the verdict is not a disagreement:\n%s", out)
	}
	if strings.Contains(out, "match=true") {
		t.Fatalf("a line the grid does not carry was reported as a match:\n%s", out)
	}
}
