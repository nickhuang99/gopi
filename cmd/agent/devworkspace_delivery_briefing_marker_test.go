package main

// devworkspace_delivery_briefing_marker_test.go — THE PRODUCER SIDE of "a
// delivery briefing must not claim the conversation's title".
//
// THE DEFECT (measured 2026-10-30, `gopi session log <id> --turns` on session
// 6a0ac1c6-4e6c-4155-8f5b-91d53a441663):
//
//	   TURN     ROUNDS  PROMPT
//	      1         76  交付没有落地：3 轮交付尝试全部被挡下，这一轮不再尝试交付。 工作区被保留，…
//
// ONE turn, and it is the CLONE CHANNEL's closing briefing — the framework's own
// sentence about a delivery that did not land, printed as the person's ask. The
// producer is this package: the delivery loop hands that text to the child agent
// on STDIN (cloneRoundCmd's cmd.Stdin), and a `gopi < stdin` child records its
// prompt from the HUMAN face because there is no other provenance a stdin input
// can honestly claim. Text was therefore the ONLY thing that could distinguish
// the row, and the text carried no marker at all.
//
// So the three briefings now open with
// sessionevent.HarnessCloneChannelBriefingMarker, applied through ONE wrapper
// (deliveryBriefingMarked) — and the property this file pins is not "the string
// is there" but the two PREDICATE answers every reader uses:
//
//	IsHarnessInjected             = true   (it is the framework talking)
//	IsTurnOpeningRow(text, human) = false  (it opens no turn of the person's)
//
// THE BRIEFINGS COME STRAIGHT FROM THE PRODUCTION RENDERERS, never from a
// test-side wrapper: a table assembled by the test would keep passing after the
// production code stopped marking anything, which is the "green over a dead
// path" shape this repository has already paid for.
//
// THE RED PROOF has two halves, and both are recorded in the report: the flag
// `-marker-off` (below) reconstructs the pre-fix TEXT from the production
// renderers and turns these arms red in one command, and the edit-level proof
// (removing the wrapper from deliveryRetryBriefing / NothingToDeliver /
// Exhausted) was run against the real tree and is quoted there.
//
// THE OPERATOR ROW IS THE CONTROL, in the same table: a marker wide enough to
// swallow everything would pass the first arm and be worth nothing.

import (
	"flag"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
)

// deliveryBriefingMarkerOff reconstructs the PRE-FIX briefing text from the
// production renderers for the RED PROOF run:
//
//	go test ./cmd/agent -run TestDeliveryBriefingsOpenAsHarnessRows -marker-off
//
// It strips the marker the production code now leads with, i.e. it hands the
// predicates exactly the bytes session 6a0ac1c6 recorded. A test that only read
// the marked text would prove the constant exists; this proves the constant is
// what flips the two predicates.
var deliveryBriefingMarkerOff = flag.Bool("marker-off",
	false, "RED PROOF ONLY: strip the harness marker from the rendered delivery briefings (the pre-fix text) so the arms below turn red")

// preFixBriefing returns the rendered briefing as the pre-fix tree would have
// produced it when the red-proof flag is set, and unchanged otherwise.
func preFixBriefing(rendered string) string {
	if !*deliveryBriefingMarkerOff {
		return rendered
	}
	return strings.TrimPrefix(rendered, sessionevent.HarnessCloneChannelBriefingMarker)
}

// deliveryBriefingCases renders the four delivery exits through their REAL
// renderers. The three briefings are the whole surface this row touches: retry
// (both blocker shapes it splits on), nothing-to-deliver, and exhausted (both
// shapes).
func deliveryBriefingCases() []struct {
	name     string
	briefing string
} {
	const dir = "/home/nick/workspace/01a188c0-34e9-72ec-b3d1-89e39619470b"
	pull, empty := pullConflictBlocker(), emptyBehindBlocker()
	return []struct {
		name     string
		briefing string
	}{
		{"deliveryRetryBriefing/refused", preFixBriefing(deliveryRetryBriefing(pull, dir, 1))},
		{"deliveryRetryBriefing/empty", preFixBriefing(deliveryRetryBriefing(empty, dir, 2))},
		{"deliveryNothingToDeliverBriefing", preFixBriefing(deliveryNothingToDeliverBriefing(empty, dir, devworkspace.MaxDeliveryRounds))},
		{"deliveryExhaustedBriefing/refused", preFixBriefing(deliveryExhaustedBriefing(pull, dir, devworkspace.MaxDeliveryRounds))},
		{"deliveryExhaustedBriefing/empty", preFixBriefing(deliveryExhaustedBriefing(empty, dir, devworkspace.MaxDeliveryRounds))},
	}
}

// TestDeliveryBriefingsOpenAsHarnessRows is the epic's producer-side acceptance
// arm: every delivery-round briefing is a HARNESS row to both predicates, and a
// real operator line is neither.
func TestDeliveryBriefingsOpenAsHarnessRows(t *testing.T) {
	if *deliveryBriefingMarkerOff {
		t.Logf("RED PROOF RUN: the markdown marker is stripped from the rendered briefings — this is the pre-fix text")
	}
	for _, c := range deliveryBriefingCases() {
		if !strings.HasPrefix(c.briefing, sessionevent.HarnessCloneChannelBriefingMarker) {
			t.Errorf("%s does not OPEN with %q — the child's row will be read as the operator's own ask:\n%s",
				c.name, sessionevent.HarnessCloneChannelBriefingMarker, firstLineOf(c.briefing))
		}
		if !sessionevent.IsHarnessInjected(c.briefing) {
			t.Errorf("%s: IsHarnessInjected = false — every session reader will render this framework sentence as the person's line:\n%s",
				c.name, firstLineOf(c.briefing))
		}
		// THE FACE IS HUMAN AND THAT IS THE POINT: the child agent records its
		// stdin prompt from the human face, so the marker's second job is to make
		// IsTurnOpeningRow say "no turn" DESPITE that face.
		if sessionevent.IsTurnOpeningRow(c.briefing, inputsource.Human) {
			t.Errorf("%s: IsTurnOpeningRow(text, human) = true — this briefing opens a HUMAN turn, which is exactly how it became session 6a0ac1c6's only turn:\n%s",
				c.name, firstLineOf(c.briefing))
		}
		// The empty-source default (a row written before the format recorded
		// faces) must answer the same — those are precisely the measured rows.
		if sessionevent.IsTurnOpeningRow(c.briefing, "") {
			t.Errorf("%s: IsTurnOpeningRow(text, \"\") = true — the empty-source default must not let the briefing open a turn",
				c.name)
		}
	}

	// THE CONTROL. An operator row must stay an operator row: a marker that
	// swallowed everything would satisfy the arms above and destroy the feature.
	for _, ask := range []string{
		"你自己去执行去看看",
		"1：false 2:开吧。3.这个是真的示例，你可以先试验一下。",
		"[gopi message boundary] the operator's own spilled payload",
	} {
		if sessionevent.IsHarnessInjected(ask) {
			t.Errorf("the operator row %q is classified as a harness injection — the marker is too wide", ask)
		}
		if !sessionevent.IsTurnOpeningRow(ask, inputsource.Human) {
			t.Errorf("the operator row %q does NOT open a turn — the marker swallowed the person's own input", ask)
		}
	}
}

// TestDeliveryBriefingsKeepTheirFactsUnderTheMarker is the second half: the
// marker is 25 runes spent BEFORE the decisive lines in a 200-rune ledger
// summary, so this arm re-runs the budget assertions over the marked renderings
// — the facts the three earlier budget tests pin must still be inside the row a
// reader sees.
//
// It asserts the STORED row (folding + truncation reproduced by storedSummary),
// never the briefing: a briefing can look right while the row cannot carry it.
func TestDeliveryBriefingsKeepTheirFactsUnderTheMarker(t *testing.T) {
	if *deliveryBriefingMarkerOff {
		t.Skip("red-proof run: this arm measures the SHIPPED text, not the stripped one")
	}
	// THE SAME WORKSPACE THE BLOCKER WAS COMPOSED FOR. emptyBehindBlocker's
	// Words open with that path, and deliveryEmptyDecisiveBlock strips it only
	// when the two agree — passing a different directory would leave the path in
	// the row and spend 56 runes of the ledger budget on it (which is precisely
	// the pre-01a18801 defect, reproduced by accident).
	const dir = emptyBehindBlockerDir
	cases := []struct {
		name   string
		blk    *devworkspace.Blocker
		render func(*devworkspace.Blocker, string, int) string
		wants  []string
	}{
		{
			name: "retry/empty carries the measurement",
			blk:  emptyBehindBlocker(), render: deliveryRetryBriefing,
			wants: []string{"carries no commit to deliver", "rev-list --count", "answered 0 against origin/master", "nothing to push", "origin/master"},
		},
		{
			name: "retry/refused carries the CONFLICT and the file",
			blk:  pullConflictBlocker(), render: deliveryRetryBriefing,
			wants: []string{"CONFLICT", "task_holder_resume.go", string(devworkspace.BlockerKindPull)},
		},
	}
	for _, c := range cases {
		stored := storedSummary(t, c.render(c.blk, dir, 1))
		if !strings.HasPrefix(stored, agent.AutoTaskSummaryPrefix+deliveryBriefingMarkerMarkdownLeadership()) {
			t.Errorf("%s: the stored row does not open with the marker: %q", c.name, stored)
		}
		for _, want := range c.wants {
			if !strings.Contains(stored, want) {
				t.Errorf("%s: the ledger row must still carry %q:\nSTORED: %s", c.name, want, stored)
			}
		}
	}
}

// deliveryBriefingMarkerMarkdownLeadership is what the STORED row must open
// with: the ledger prefixes `[auto] ` to the folded briefing, so the marker is
// the first thing after that prefix (Rule 14: the spelling is taken from the
// constant, never re-typed here).
func deliveryBriefingMarkerMarkdownLeadership() string {
	return sessionevent.HarnessCloneChannelBriefingMarker
}

// firstLineOf keeps a failure readable: the briefings are pages long and the
// first line is what the ledger and the turn table print.
func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
