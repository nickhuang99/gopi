package main

// terminal_capability_wiring_test.go — the guard that keeps the CONSOLE on the
// one entry point that asks the terminal, and states why a source assertion is
// the right instrument here.
//
// WHY THIS IS A SOURCE GATE AND NOT A BEHAVIOURAL TEST. The console's sequence
// cannot be reached from a test without a real terminal AND a real chat run:
// `newChatInputWithPalette` opens the descriptor it is given, and cmd/agent's
// pty fixtures do exercise that — but a behavioural test there would assert
// "the palette is 256" and would go on passing if the code kept its own private
// probe, because the private probe would answer correctly too. The property under
// guard is not "the answer is right" but "there is exactly ONE answer": a second,
// private copy of the probe in this file is the defect (it is what let
// `gopi-replay` classify a whole film without asking), and a duplicate is only
// visible by reading the file. The precedent is TestChatHelpSwitchRoutesThrough
// TheArgument (cli_chat_test.go): a re-inlined call is caught by reading the
// source, because every behavioural test stays green while it is re-inlined.
//
// WHAT IT PINS, and each half is a different regression:
//
//  1. the wiring CALLS lineedit.CapabilityForOpenTerminal — the one place that
//     knows "a terminal is open, so ask it";
//  2. the wiring does NOT spell the probe itself (no term.ProbeCapability(), no
//     .WithMeasurement(...)) — the shape that was there before this task and
//     that `gopi-replay` copied;
//  3. the wiring still reads no capability from the environment at the point of
//     decision OTHER than through lineedit — so a future edit cannot quietly
//     return to an env-only answer for the editor's table.

import (
	"os"
	"strings"
	"testing"
)

// capabilityWiringSource is the file whose call site is under guard: the chat
// console's terminal wiring (cmd/agent/lineedit_wire.go). Its two constructors
// are the only paths that install the editor, and the editor's palette is what
// this guard is about.
const capabilityWiringSource = "lineedit_wire.go"

// capabilityAskingEntryPoint is the ONE function that asks an OPEN terminal to
// classify itself. A caller that has a tty must go through it; the name is
// spelled here so the guard fails loudly if it is renamed rather than silently
// matching nothing.
const capabilityAskingEntryPoint = "lineedit.CapabilityForOpenTerminal"

// capabilityPrivateProbeShapes are the spellings a private copy of the asking
// sequence would use. Each one is a defect ON THIS SIDE of the package boundary:
// the probe, or the fold of its result, performed by the caller instead of by
// lineedit.
var capabilityPrivateProbeShapes = []string{
	".ProbeCapability()",
	".WithMeasurement(",
}

// TestChatCapabilityWiringAsksThroughTheOneEntryPoint is the chat side's half of
// the parity contract (internal/replay/capability_parity_test.go is the other:
// it asserts the two surfaces AGREE, and this file asserts the console's half
// reaches the agreement by the same door the replay uses).
func TestChatCapabilityWiringAsksThroughTheOneEntryPoint(t *testing.T) {
	src, err := os.ReadFile(capabilityWiringSource)
	if err != nil {
		t.Fatalf("read %s: %v", capabilityWiringSource, err)
	}
	body := string(src)

	// ── 1. the call is there ────────────────────────────────────────────────
	if !strings.Contains(body, capabilityAskingEntryPoint) {
		t.Errorf("%s does not call %s — the console has a terminal open and must ask it through "+
			"the one entry point that does so (a private probe here is what the replay copied, "+
			"and the two surfaces then disagree about the same hardware)", capabilityWiringSource, capabilityAskingEntryPoint)
	}

	// ── 2. the private copy is gone ─────────────────────────────────────────
	for _, shape := range capabilityPrivateProbeShapes {
		if strings.Contains(body, shape) {
			t.Errorf("%s contains %q — the asking sequence (probe, and the fold of its result) "+
				"belongs to lineedit's ONE entry point, %s; a second copy here drifts from it",
				capabilityWiringSource, shape, capabilityAskingEntryPoint)
		}
	}

	// ── 3. the declaration is resolved ONCE, from lineedit ──────────────────
	//
	// The console resolves the environment's half (CapabilityForEnv) and hands it
	// IN, which is the shape the entry point takes: the declaration is decided
	// once per run (a second resolution would print the palette downgrade notice
	// twice, into the terminal the operator is reading). The guard asserts the
	// resolver is lineedit's, so the environment is not read by hand here.
	if !strings.Contains(body, "lineedit.CapabilityForEnv(") {
		t.Errorf("%s no longer resolves the declared capability through lineedit.CapabilityForEnv — "+
			"the environment's half must come from lineedit's single resolver, not from a local read "+
			"of TERM/COLORTERM", capabilityWiringSource)
	}
}
