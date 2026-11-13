package main

// RP-1's ANCHOR STRENGTH, machine-checked (Rule 5: every fix ships a minimal
// repro test; Rule 5).
//
// WHAT THIS FILE ADDS. TestRunPathPlainPromptIsVerbatim pins the anchor at the
// REQUEST BOUNDARY against the real binary — the strongest instrument there is,
// but it can only observe the wires today's code happens to produce, one request
// per surface. It cannot answer "would this anchor still catch the defect if the
// defect came back in the OTHER wire shape", and that is the question the
// inherited report raised about the assertion: it was relaxed from an equality
// to a prefix, and a relaxation with no red proof is a weakened gate nobody can
// see.
//
// THE DEFECT FORM RP-1 REPAIRED is bytes IN FRONT of the prompt: the first user
// message read `[EPIC GOAL]\n<prompt>\n\nBased on this Epic, …[EPIC REFRAME] …`
// instead of beginning with the prompt (devdoc/end-user-audit-slice8-runpath.md;
// fix ee8aff278). The shipped wire instead APPENDS a turn stamp AFTER the prompt
// (internal/agent/clock.go — one stamp per assembled request, the run's own
// every-round time signal), so the message is exactly `<prompt><stamp>`.
//
// THE TWO STRENGTHS ON THE TWO WIRE SHAPES — a 2x2, because one row alone cannot
// show that prefix is the STRONGEST available anchor rather than a weaker one:
//
//	                         prepended frame (defect)   appended stamp (shipped)
//	equality (the old test)   RED                        RED    <- false alarm on a correct product
//	prefix (the anchor now)   RED                        GREEN  <- the only cell that is both
//	contains (rejected)      GREEN                         -     <- the RP-1 defect passes a substring test
//
// The two RED cells are driven here as measurements, not as prose: the fixture
// renders both wires from PRODUCTION renderers (agent.TurnStamp.Render for the
// stamp, config.FrameMarker* for the frame vocabulary — Rule 13/14, never a
// retyped spelling) and asserts what each anchor does to each of them. The third
// check reads the boundary test's own source, so an anchor quietly weakened back
// to a substring spelling fails by name rather than passing in silence
// (Rule 15: a check that found nothing must be distinguishable from one that
// never ran).

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/config"
)

const (
	// rp1AnchorTickLabel is a wire spelling of the stamp's tick — the sub-minute
	// form internal/agent/clock.go's TickLabel mints. Only its SHAPE matters:
	// every anchor here is asserted against the stamp's position, never against
	// the tick's value.
	rp1AnchorTickLabel = "[t+0s]"
	// rp1AnchorSourcePath is the boundary test whose anchor this file guards,
	// read as SOURCE so a weakened spelling is caught before it can pass.
	rp1AnchorSourcePath = "cli_runpath_boundary_rp1_test.go"
	// rp1AnchorPositive is the anchor expression the boundary test must carry:
	// the prompt must BEGIN the message.
	rp1AnchorPositive = "strings.HasPrefix(got, rp1PromptToken)"
	// rp1AnchorWeakened is the spelling that must NOT appear. A prompt wrapped in
	// an operating frame satisfies a substring test — that IS the RP-1 defect,
	// and it is one edit away from the line above.
	rp1AnchorWeakened = "!strings.Contains(got, rp1PromptToken)"
)

// rp1AnchorShippedWire is the message the run path really sends for a plain
// prompt: the caller's text, then the turn stamp the harness appends to it. The
// stamp comes from its production renderer, so this fixture reads the code's own
// answer instead of a copy of it (the defect class 6c06db641 repaired).
func rp1AnchorShippedWire() string {
	return rp1PromptToken + agent.TurnStamp{Tick: rp1AnchorTickLabel, First: true}.Render()
}

// rp1AnchorDefectWire is the pre-fix wire RP-1 repaired: the agent-harness frame
// in FRONT of the prompt, with the frame's own markers taken from
// config.FrameMarker* — the same constants the help page and the boundary test's
// frame scan read — so the fixture cannot drift from the frame vocabulary while
// still claiming to reproduce it.
func rp1AnchorDefectWire() string {
	return config.FrameMarkerEpic + "\n" + rp1PromptToken + "\n\n" + config.FrameMarkerReframe
}

// TestRP1AnchorPrefixAcceptsTheShippedWire pins the anchor's GREEN cell: on the
// wire the run path really produces, the prompt BEGINS the message while the old
// equality anchor does NOT hold — which is why equality was a false alarm
// against a correct product rather than a defect report.
func TestRP1AnchorPrefixAcceptsTheShippedWire(t *testing.T) {
	msg := rp1AnchorShippedWire()
	if !strings.HasPrefix(msg, rp1PromptToken) {
		t.Errorf("the prefix anchor REJECTS the shipped wire — the run path no longer begins the message with the caller's text:\n got: %q", msg)
	}
	if msg == rp1PromptToken {
		t.Errorf("the shipped wire is the bare prompt with nothing appended, so this fixture measures no wire the run produces (the stamp rendered empty): %q", msg)
	}
	t.Logf("measured on the shipped wire: equality=%v HasPrefix=%v (the appended stamp is the run's published time signal, not a defect)",
		msg == rp1PromptToken, strings.HasPrefix(msg, rp1PromptToken))
}

// TestRP1AnchorPrefixRejectsTheDefectWire pins the anchor's RED cell — the cell
// that makes the relaxation safe to have made. On the pre-fix wire every
// substring test passes and the prefix test fails, which is exactly the red the
// boundary test is supposed to raise when the frame comes back in front of a
// prompt.
func TestRP1AnchorPrefixRejectsTheDefectWire(t *testing.T) {
	msg := rp1AnchorDefectWire()
	if strings.HasPrefix(msg, rp1PromptToken) {
		t.Fatalf("the prefix anchor ACCEPTED a message that begins with the frame — a prompt wrapped in operating instructions is the RP-1 defect and must fail by name:\n got: %q", msg)
	}
	if !strings.Contains(msg, rp1PromptToken) {
		t.Fatalf("the defect wire does not carry the prompt at all, so this fixture measures nothing:\n got: %q", msg)
	}
	t.Logf("measured on the pre-fix wire: Contains=%v HasPrefix=%v — the substring spelling the anchor was NOT relaxed to would pass here",
		strings.Contains(msg, rp1PromptToken), strings.HasPrefix(msg, rp1PromptToken))
}

// TestRP1BoundaryAnchorIsNotWeakened reads the boundary test's own source: the
// prefix anchor must still be there, and the substring spelling must not be. It
// is the anti-regression half of the fixture above — the fixture proves the
// anchor's strength is SUFFICIENT, this proves it is still the anchor in use.
func TestRP1BoundaryAnchorIsNotWeakened(t *testing.T) {
	src, err := os.ReadFile(rp1AnchorSourcePath)
	if err != nil {
		t.Fatalf("read %s: %v — an anchor that cannot be read is not a checked one", rp1AnchorSourcePath, err)
	}
	text := string(src)
	if !strings.Contains(text, rp1AnchorPositive) {
		t.Errorf("%s no longer carries the prefix anchor %q — the RP-1 assertion was relaxed or renamed, and the strength measured above is no longer what runs:\n file: %s",
			rp1AnchorSourcePath, rp1AnchorPositive, rp1AnchorSourcePath)
	}
	if strings.Contains(text, rp1AnchorWeakened) {
		t.Errorf("%s carries the substring anchor %q — a prompt wrapped in an operating frame passes it, which is the RP-1 defect itself:\n file: %s",
			rp1AnchorSourcePath, rp1AnchorWeakened, rp1AnchorSourcePath)
	}
}
