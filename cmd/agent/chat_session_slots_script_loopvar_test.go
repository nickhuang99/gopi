package main

// chat_session_slots_script_loopvar_test.go — the RED-to-GREEN proof for the
// renamable-loop-variable arm of the notify.sh pin.
//
// WHAT IT PINS. `TestNotifyScriptAgreesWithTheRegistryConstants` used to require
// the literal `${SLOT_PREFIX}${i}${SLOT_SUFFIX}` — a spelling of the script's
// own counter, not a property of the script. Commit 9e8446fdd lifted the
// delivery loop into `deliver_all` and renamed that counter `i` -> `_da_i`; the
// script still built the name correctly from the declared parts, but the pin
// asserted a literal that no correct version of the script carried, so the
// package went red on pristine master. The three cases below are the two
// directions of the fix: the LITERAL must not be pinned, the DERIVATION must
// still be.
//
// The fixtures are text, not files: the arms run the two readers over script
// bodies spelled here, so the proof travels with the test and never depends on
// the shipped script's identifier of the day.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture body: the shape the script has after 9e8446fdd, with the counter
// name as the parameter. It is NOT the shipped script — only its loop shape.
func notifyScriptFixtureBody(loopVar string) string {
	return strings.Join([]string{
		"# a fixture that walked a loop in a previous life: ${SLOT_PREFIX}hello${SLOT_SUFFIX}",
		"readonly SLOT_LIMIT=10",
		`readonly SLOT_PREFIX="chat_session_id_"`,
		`readonly SLOT_SUFFIX=".txt"`,
		"deliver_all() {",
		"\t_da_message=\"$1\"",
		"\t" + loopVar + "=0",
		"\twhile [ \"$" + loopVar + "\" -lt \"$SLOT_LIMIT\" ]; do",
		"\t\tslot=\"${PROFILE}/${SLOT_PREFIX}${" + loopVar + "}${SLOT_SUFFIX}\"",
		"\t\t" + loopVar + "=$((" + loopVar + " + 1))",
		"\tdone",
		"}",
	}, "\n")
}

// TestNotifyScriptSlotLoopVarReadsTheScriptsOwnCounter — the reader finds the
// counter the script actually walks with, whatever it is called. A pin that
// hard-coded `i` fails this on the `_da_i` body; that is the defect it repairs.
func TestNotifyScriptSlotLoopVarReadsTheScriptsOwnCounter(t *testing.T) {
	for _, want := range []string{"i", "_da_i", "slot_index"} {
		body := notifyScriptFixtureBody(want)
		got, found := notifyScriptSlotLoopVar(body)
		if !found {
			t.Fatalf("no slot walk found in a body whose loop counter is %q", want)
		}
		if got != want {
			t.Fatalf("notifyScriptSlotLoopVar = %q, want %q (the script's own counter, not a pinned spelling)", got, want)
		}
		if assigned := notifyScriptAssignedSlotName(body, got); assigned == "" {
			t.Fatalf("the %q body BUILDS the slot name from the declared parts, but the pin found no assignment", want)
		}
		// THE RED PROOF, MEASURED IN THE SAME RUN: the pre-fix predicate was
		// `strings.Contains(body, "${SLOT_PREFIX}${i}${SLOT_SUFFIX}")`. On the
		// renamed counter it is false while the script has built the name
		// correctly — which is exactly the pristine-master failure this arm
		// repairs, and it is asserted rather than described.
		oldPredicate := strings.Contains(body, "${SLOT_PREFIX}${i}${SLOT_SUFFIX}")
		if oldPredicate != (want == "i") {
			t.Fatalf("the pre-fix predicate on the %q body = %v, want %v — a pin that is not spelling-blind misses a correct script",
				want, oldPredicate, want == "i")
		}
		// THE PIN'S OWN VERDICT, driven over the same bodies — this is what makes
		// this file the arm FOR THE PIN and not a parallel opinion: the predicate
		// the pin consults accepts all three bodies, while the pre-fix literal
		// accepts only the `i` one. A pin reverted to that literal therefore FAILS
		// HERE, on a renamed body, and not only on the shipped script.
		gotVar, built := notifyScriptBuildsSlotName(body)
		if !built || gotVar != want {
			t.Fatalf("the pin's own verdict on the %q body = (%q, %v) — a pin that reads the script's counter accepts it", want, gotVar, built)
		}
	}
}

// TestNotifyScriptSlotNameMustBeBuiltInAnAssignment — the OTHER direction: a
// mention is not a build. The pre-fix pin was satisfied by the three parts
// appearing anywhere in the text, so a script whose only occurrence sits in a
// COMMENT (or is baked into a message string) passed while it built the name
// from something else entirely — a pin that pins nothing.
func TestNotifyScriptSlotNameMustBeBuiltInAnAssignment(t *testing.T) {
	commentOnly := strings.Join([]string{
		"# ${SLOT_PREFIX}${i}${SLOT_SUFFIX} is what the registry writes",
		"readonly SLOT_LIMIT=10",
		`readonly SLOT_PREFIX="chat_session_id_"`,
		`readonly SLOT_SUFFIX=".txt"`,
		"deliver_all() {",
		"\ti=0",
		"\twhile [ \"$i\" -lt \"$SLOT_LIMIT\" ]; do",
		"\t\tslot=\"${PROFILE}/hardcoded-${i}.txt\"",
		"\t\ti=$((i + 1))",
		"\tdone",
		"}",
	}, "\n")
	if got := notifyScriptAssignedSlotName(commentOnly, "i"); got != "" {
		t.Fatalf("a build found where the parts appear only in a comment: %q", got)
	}
	// THE HOLE, MEASURED IN THE SAME RUN. The pre-fix predicate was
	// `strings.Contains(text, "${SLOT_PREFIX}${i}${SLOT_SUFFIX}")` over the WHOLE
	// file, so this comment-only body SATISFIES it: the old pin would have passed
	// a script that never built the name from the declared parts — a pin that
	// pinned nothing. Measuring both directions in one run (false on a renamed
	// correct body above, true here) is what makes this arm a REPAIR rather than
	// a relaxation; loosening alone would just remove the guard.
	if !strings.Contains(commentOnly, "${SLOT_PREFIX}${i}${SLOT_SUFFIX}") {
		t.Fatal("the pre-fix predicate is NOT satisfied by a comment-only mention — the hole measured here is not the one this repair closes")
	}

	// The declared parts mentioned in a lookalike assignment must not count
	// either: the shape has to carry the counter, not the literal `i` of the
	// comment above someone's loop.
	otherVar := notifyScriptFixtureBody("_da_i")
	otherVar = strings.Replace(otherVar, `slot="${PROFILE}/${SLOT_PREFIX}${_da_i}${SLOT_SUFFIX}"`,
		`slot="${PROFILE}/chat_session_id_${i}.txt"`+"\n\t\t# ${SLOT_PREFIX}${i}${SLOT_SUFFIX}", 1)
	if got := notifyScriptAssignedSlotName(otherVar, "_da_i"); got != "" {
		t.Fatalf("a build found where the name is spelled with a foreign counter: %q", got)
	}
}

// TestNotifyScriptSlotNameRecognisesTheShippedScript — the anti-vacuity arm: the
// shipped script is read as text (never through a checkout or a fixture) and the
// two readers must both answer non-empty on it. Deleting the loop, renaming the
// declared parts without building the name from them, or moving the build off an
// assignment line all turn this red — so the two readers cannot pass by finding
// nothing anywhere.
func TestNotifyScriptSlotNameRecognisesTheShippedScript(t *testing.T) {
	body, err := os.ReadFile(filepath.Clean(notifyScriptRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", notifyScriptRelPath, err)
	}
	loopVar, found := notifyScriptSlotLoopVar(string(body))
	if !found {
		t.Fatalf("the shipped script has no slot walk: no `IDENT=0` followed by a `while [ \"$IDENT\" -lt %s ]` test", notifyScriptSlotLimitBound)
	}
	assigned := notifyScriptAssignedSlotName(string(body), loopVar)
	if assigned == "" {
		t.Fatalf("the shipped script does not build the slot name from %s%s%s on an assignment line",
			notifyScriptSlotPrefixExpansion, loopVar, notifyScriptSlotSuffixExpansion)
	}
	for _, part := range []string{notifyScriptSlotPrefixExpansion, loopVar, notifyScriptSlotSuffixExpansion} {
		if !strings.Contains(assigned, part) {
			t.Fatalf("the assignment that builds the slot name is missing %s: %q", part, assigned)
		}
	}
}
