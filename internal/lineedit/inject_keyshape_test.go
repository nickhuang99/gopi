package lineedit

// inject_keyshape_test.go — the refusal contract for a value that is a SPELLING
// of a key rather than the key's own BYTES.
//
// THE DEFECT THIS PINS (measured, task 01a10e8f). InjectKey is name-first: a
// value that IS a table entry becomes that key's wire bytes, and *anything else*
// is injected as its own bytes. That rule is right for a rune — "/" opens the
// completion menu and no name produces it — and wrong for a value that carries
// the vocabulary's OWN name form. Asked for `ctrl-u`, the seam queued the six
// letters c-t-r-l---u as ONE read unit, the editor's decoder read them as
// printable runes, and they landed IN THE OPERATOR'S EDITING LINE while the tool
// answered "injected=ok". The caller sees a success, the human's text is edited,
// and nothing anywhere says the key did not arrive (Rule 15: a silent downgrade
// is not a success). `\u0015` — how a caller spells the byte 0x15 — takes the
// same path: six literal characters into the line.
//
// The refusal names the legal set (the same discipline the tool's action enum
// follows), and the probes below are every shape of that class.

import (
	"strings"
	"testing"
)

// injectKeyShapeProbes are the key-NAME-SHAPED values: each is the vocabulary's
// own spelling of a key (a family member that is not in the table, a table name
// under a different case), or an escape spelling of one byte.
const injectShapeCeiling = 64

var injectKeyShapeProbes = []string{
	"ctrl-u", // the decoder maps 0x15 to KeyKillToStart — a real key, unnamed here
	"ctrl-a", // KeyHome
	"ctrl-b", // KeyLeft
	"ctrl-e", // KeyEnd
	"ctrl-f", // KeyRight
	"ctrl-k", // KeyKillToEnd
	"ctrl-w", // KeyKillWordBack
	"ctrl-y", // KeyYank
	"ctrl-l", // KeyClearScreen
	"ctrl-p", // KeyScrollUp
	"ctrl-n", // KeyScrollDown
	"ctrl-d", // KeyEOF
	"ctrl-q", // mapped to nothing, but STILL the family's spelling, never text
	`\u0015`,
	`\u0003`,
	`\x15`,
	`\027`,
	`\n`,
	`\t`,
	`\e`,
	"Down",   // a table name in the WRONG CASE: the same typo class, the same harm
	"Enter",  // as above
	"Ctrl-C", // the table name ctrl-c, spelled in Title Case
	"CTRL-U", // the family, upper-cased
}

// injectLiteralProbes are values that must NOT be refused: the rarer text an
// operator (or a model) may legitimately want typed into its own line. The
// predicate has to be conservative — a refusal that fires on ordinary content
// takes the seam away from the callers it exists for.
var injectLiteralProbes = []string{
	"q",
	"/",
	"\u77e5", // a CJK rune, passed as itself
	"-",
	"x",
	"ab",
	// A literal LONGER THAN ONE KEY'S BOUND. It is here on purpose: the "one
	// injected key is one read unit" refusal (injectReadBytes) must stay
	// reachable THROUGH InjectKey, so the shape refusal cannot become a blanket
	// refusal of everything past the table.
	"you> ",
	`\`,      // a lone backslash: no escape body
	`\q`,     // a backslash and a letter that is NOT an escape
	`\Users`, // a Windows-ish path fragment: body is not a valid escape
	`\u`,     // the prefix alone, with no digits
	`\x`,     // as above
	"ctrl",   // the prefix word alone
	"./x",    // a path-ish run of printable bytes
	"know> ", // the prompt of a real chat line, to be typed back
}

// assertRefusalNamesTheLegalSet is the Rule 15 half: the refusal must say what
// IS accepted, not merely that this was not.
func assertRefusalNamesTheLegalSet(t *testing.T, probe, why string) {
	t.Helper()
	if why == "" {
		t.Fatalf("InjectKey(%q) was refused with no sentence — a refusal nobody can act on (Rule 15)", probe)
	}
	for _, name := range InjectableKeyNames() {
		if !strings.Contains(why, name) {
			t.Errorf("the refusal for %q does not name the legal key %q — the caller cannot tell what to pass instead\nsentence: %s", probe, name, why)
		}
	}
	if !strings.Contains(why, "byte") {
		t.Errorf("the refusal for %q does not mention the BYTE route (the other way to inject what a name cannot express)\nsentence: %s", probe, why)
	}
}

// TestInject_KeyNameShapedValuesAreRefusedNotTyped is THE regression guard for
// the silent downgrade. RED BEFORE THE FIX: every probe above was accepted as
// its own bytes, so the first assertion failed on the first probe and the
// operator's-line assertion (below) failed with "ctrl-u" visible in the buffer.
//
// RED PROOF (Rule 5): delete the `keyNameSpellingShape` branch from
// InjectKeyBounded and this case fails on line one of the probe loop with
// `InjectKey("ctrl-u") accepted = true` — the spelling becomes text again.
func TestInject_KeyNameShapedValuesAreRefusedNotTyped(t *testing.T) {
	h := newInjectHarness(t, &stubCompleter{})

	for _, probe := range injectKeyShapeProbes {
		before := h.inj.Used()
		res := h.inj.InjectKey(probe)
		if res.Accepted {
			t.Errorf("InjectKey(%q) accepted = true: a key NAME/spelling was queued as its LITERAL BYTES, which types its letters into the operator's editing line (Rule 15)", probe)
			continue
		}
		assertRefusalNamesTheLegalSet(t, probe, res.Why)
		if res.Used != before {
			t.Errorf("InjectKey(%q) refused but charged the ceiling (%d -> %d): a refusal that spends the budget is a second silent cost", probe, before, res.Used)
		}
	}

	// THE OPERATOR'S LINE: the positive half. Nothing may have reached the
	// buffer — the property is about the HUMAN's text, not about a return value.
	for _, probe := range injectKeyShapeProbes {
		if strings.Contains(h.ed.Value().Text(), probe) {
			t.Fatalf("the refused spelling %q is IN the operator's editing line (%q) — the refusal did not stop the write\n--- screen ---\n%s",
				probe, h.ed.Value().Text(), h.rows())
		}
	}
	if got := h.ed.Value().Text(); got != "" {
		t.Fatalf("the editing line holds %q after %d refusals, want it empty", got, len(injectKeyShapeProbes))
	}
}

// TestInject_OrdinaryLiteralValuesAreStillInjected is the OTHER half of the
// predicate: a conservative refusal, never an eager one. Every probe here is a
// legitimate literal — an ordinary rune, a slash the completion menu wants, a
// path fragment, a lone backslash — and a value LONGER THAN ONE KEY'S BOUND, so
// the pre-existing byte bound stays reachable through InjectKey as well.
//
// The property is NOT "accepted": these all share one session ceiling, and the
// longer ones spend it. What must hold is that each reaches the seam's OTHER
// landing places — the queue (Accepted) or one of the two refusals that are not
// the shape refusal. A probe rejected for its SHAPE would mean the predicate
// fired on ordinary content, which is the failure this case exists to catch.
func TestInject_OrdinaryLiteralValuesAreStillInjected(t *testing.T) {
	h := newInjectHarness(t, &stubCompleter{})
	for _, probe := range injectLiteralProbes {
		res := h.inj.InjectKey(probe)
		if res.Accepted {
			continue
		}
		if strings.Contains(res.Why, "not a key's bytes") {
			t.Errorf("InjectKey(%q) refused by the SHAPE predicate (%s) — ordinary literal content must never be refused for being a key spelling", probe, res.Why)
			continue
		}
		// Reaching the ceiling is not this case's business; reaching it in
		// position 7 of 17 without a refusal would be (a ceiling below the
		// probe count would make this assert nothing).
		if strings.Contains(res.Why, "ceiling is reached") {
			continue
		}
		t.Errorf("InjectKey(%q) refused for an unexpected reason: %s", probe, res.Why)
	}
}

// TestInject_OverLongLiteralIsStillRefusedForItsLength pins the byte bound's
// reachability through the name-first door: a shape refusal that swallowed every
// long value would make "one injected key is one read unit" unreachable from
// InjectKey, and the class of regressions that bound exists for would go
// unnoticed.
func TestInject_OverLongLiteralIsStillRefusedForItsLength(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectShapeCeiling)
	res := inj.InjectKey(strings.Repeat("x", injectReadBytes+1))
	if res.Accepted {
		t.Fatalf("a %d-byte literal was accepted through InjectKey, want the one-key bound to refuse it", injectReadBytes+1)
	}
	if !strings.Contains(res.Why, "one key") {
		t.Fatalf("the refusal must be the byte bound's own sentence (one key is one read unit); got %q", res.Why)
	}
}

// TestInject_SpaceAndNewlineAreBytesNotPadding is the second defect the shape
// work uncovered, and it is a REGRESSION GUARD for the ordering inside
// InjectKeyBounded (measured on the pre-fix seam, verbatim):
//
//	InjectKey(" ")  -> no key: pass a key name or the raw bytes of one key
//	InjectKey("\n") -> no key: pass a key name or the raw bytes of one key
//
// Both are ONE BYTE of real input — the most ordinary two bytes in a line — and
// the trim ran BEFORE the lookup, so both became the empty string and the seam
// denied the very thing its own sentence asks for. The lookup is therefore tried
// on the RAW value first and the trim is applied only to the NAME arm.
func TestInject_SpaceAndNewlineAreBytesNotPadding(t *testing.T) {
	for _, probe := range []string{" ", "\n"} {
		h := newInjectHarness(t, &stubCompleter{})
		h.inj.ResetKeys(injectShapeCeiling)
		res := h.inj.InjectKey(probe)
		if !res.Accepted {
			t.Errorf("InjectKey(%q) refused (%s) — a space and a newline are one byte of ordinary input, not padding to be trimmed away", probe, res.Why)
			continue
		}
		if got := h.ed.Value().Text(); got != probe {
			t.Errorf("InjectKey(%q) accepted but the editing line holds %q", probe, got)
		}
	}
	// And the NAME arm still forgives the model's own padding: the trim exists so
	// that a word wrapped in spaces still reaches its key.
	h := newInjectHarness(t, &stubCompleter{})
	h.inj.ResetKeys(injectShapeCeiling)
	if res := h.inj.InjectKey(" up "); !res.Accepted {
		t.Errorf("InjectKey(\" up \") refused (%s) — the name arm must still forgive surrounding padding", res.Why)
	}
}

// TestInject_TheNamedCtrlKeyIsStillResolved is the shadowing guard, stated as a
// property rather than as prose: ctrl-c IS a table entry, so the family refusal
// must not swallow it. If a later author adds a member to the table, the probe
// list in the shape case goes RED (that value now resolves instead of refusing),
// and this case is what tells them which side of the line it belongs on.
//
// The property it rests on is asserted over the EDITOR'S OWN decoder rather than
// over a list: every control byte the decoder maps to a key must, under the
// family's spelling, be refused rather than typed — which is exactly what a
// value that resolves to a real key silently becoming TEXT violates.
func TestInject_TheNamedCtrlKeyIsStillResolved(t *testing.T) {
	h := newInjectHarness(t, &stubCompleter{})
	if res := h.inj.InjectKey(KeyNameCtrlC); !res.Accepted {
		t.Fatalf("InjectKey(%s) refused (%s): the ONE control key the vocabulary names must still resolve to its byte", KeyNameCtrlC, res.Why)
	}

	// THE SET IS READ FROM decodeControl, never enumerated here: a byte the
	// decoder gives a key to is a key, and the vocabulary's name form for it
	// must not become text.
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	probe := NewInjector(src, injectShapeCeiling)
	var covered int
	for b := 1; b <= 26; b++ {
		if decodeControl(byte(b)).Code == KeyNone {
			continue
		}
		spelling := injectKeyNameFormPrefix + string(rune('a'+b-1))
		if spelling == KeyNameCtrlC {
			continue
		}
		covered++
		if res := probe.InjectKey(spelling); res.Accepted {
			t.Errorf("the spelling %q (control byte %#x, which the decoder maps to a key) was accepted as literal text", spelling, b)
		}
	}
	if covered == 0 {
		t.Fatalf("the decoder maps no control byte other than ctrl-c to a key: the property above is vacuous")
	}
}
