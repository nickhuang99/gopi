package config

// output_language_value_vocabulary_test.go — the guard for the VALUE VOCABULARY
// of the reply-language knob (epic 01a0e049).
//
// THE GAP (measured 2026-09-27). The knob had become discoverable on `gopi -h`
// — the row existed and named all three channels — but the VALUE was spelled
// `TAG` and left undefined: no shape, no worked examples, and no statement of
// what is NOT a value. So the operator's question ("is `chinese` correct? is it
// an enum? a locale?") was answerable from no help page, and `gopi -h` gave
// neither an example nor a theme for a knob whose entire grammar is one regex.
//
// TEACHING THE VOCABULARY IN THE REFUSAL MESSAGE IS NOT A SUBSTITUTE. The error
// path already taught it ("want e.g. zh, zh-CN, en-GB") — but a user learns it
// by being refused first, and a question asked BEFORE the first attempt gets no
// report at all. The page is the only surface that can answer it in advance.
//
// WHAT IS PINNED, and why none of it is vacuous:
//
//  1. the page states the value class, the worked examples and the negative
//     claim, each read from the knob's OWN constants (Rules 1b/13/14) — a
//     reworded constant moves the page and this assertion together;
//  2. the page cannot ADVERTISE a vocabulary the parser disagrees with: every
//     example it teaches must be accepted by the real predicate, and every
//     spelling it says is NOT a value must really be refused. The spellings are
//     EXTRACTED from the constant, never re-typed here, so the check measures
//     the page rather than a copy of it;
//  3. the refusal message and the page teach the SAME class and examples — the
//     two surfaces drifted apart once (the page taught nothing) and this is the
//     tripwire against a repeat.
//
// RED PROOF (Rule 10.4, minimal sample): dropping the vocabulary half from
// OutputLanguageRowSummary — back to the pre-epic `"language gopi ANSWERS in (" +
// OutputLanguageEnv + …` — turns TestHelpPageStatesTheValueVocabulary red,
// naming the missing pieces. Renaming OutputLanguageExampleTags to a value the
// regex refuses turns TestHelpVocabularyMatchesTheParser red. A guard that
// passes on the un-fixed tree would prove nothing.

import (
	"regexp"
	"strings"
	"testing"
)

// backquotedTokenRe extracts the `…`-quoted spellings out of a claim text, so
// the agreement test below reads the spellings the PAGE teaches instead of a
// hand-copied list (Rule 1b).
var backquotedTokenRe = regexp.MustCompile("`([^`]+)`")

// flatUsage is the rendered page with every run of whitespace collapsed: the
// renderer wraps prose by width, so a sentence must be matched across its line
// breaks and never against the raw text.
func flatUsage() string { return strings.Join(strings.Fields(FormatUsage()), " ") }

// TestHelpPageStatesTheValueVocabulary is the epic's acceptance: the page a
// user reads first must answer "what do I write after --output-language?" —
// including that the answer is a language TAG and not the language's name.
func TestHelpPageStatesTheValueVocabulary(t *testing.T) {
	flat := flatUsage()
	for _, want := range []string{
		OutputLanguageValueQualifier, // BCP-47-ish — the standard the values come from
		OutputLanguageValueShape,     // language tag — the class, named
		OutputLanguageExampleTags,    // zh, zh-CN, en-GB — worked examples
		OutputLanguageNotAValueClaim, // NOT a name / NOT a locale — the negative half
		OutputLanguageRowSummary,     // the whole row, flattened
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("`gopi -h` does not state the value vocabulary: missing %q.\n"+
				"The page shows the placeholder TAG and never says what a TAG is, so the reader "+
				"cannot tell a language tag from a language name or a locale (epic 01a0e049).",
				want)
		}
	}
}

// TestHelpVocabularyMatchesTheParser is the anti-drift half, and the reason the
// page can be trusted: a page is evidence only if what it teaches is what the
// code does. Both directions are checked — the values it advertises are
// ACCEPTED, and the shapes it says are illegal ARE refused.
func TestHelpVocabularyMatchesTheParser(t *testing.T) {
	// (a) Every example the page teaches as legal must pass the real predicate.
	examples := strings.Split(OutputLanguageExampleTags, ", ")
	if len(examples) < 2 {
		t.Fatalf("the examples constant lists %d value(s) — a vocabulary taught with fewer than two "+
			"worked examples does not show the SHAPE (a bare tag and a subtagged one)", len(examples))
	}
	for _, tag := range examples {
		if !isOutputLanguageTag(tag) {
			t.Errorf("`gopi -h` teaches %q as a legal value but the parser REFUSES it — the page "+
				"advertises a vocabulary the code does not implement", tag)
		}
	}

	// (b) Every spelling the page says is NOT a value must really be refused.
	// The spellings are extracted from the claim, so this cannot silently drift
	// into checking a list the test itself invented.
	toks := backquotedTokenRe.FindAllStringSubmatch(OutputLanguageNotAValueClaim, -1)
	if len(toks) == 0 {
		t.Fatalf("the negative claim names no spelling — the reflection in (b) would scan nothing, " +
			"so the page could deny a value it actually accepts")
	}
	for _, m := range toks {
		if isOutputLanguageTag(m[1]) {
			t.Errorf("`gopi -h` tells the reader %q is NOT a value, but the parser ACCEPTS it — the "+
				"page lies about the vocabulary", m[1])
		}
	}

	// (c) At least one runnable example must pair the flag with a taught value,
	// so the reader can copy a working invocation instead of assembling one.
	var paired bool
	for _, tag := range examples {
		if strings.Contains(flatUsage(), flagPrefix+OutputLanguageFlagName+" "+tag) {
			paired = true
			break
		}
	}
	if !paired {
		t.Errorf("`gopi -h` names %s%s but never shows it with one of the taught values (%s) — "+
			"an example is the half of a vocabulary that makes it copy-pasteable",
			flagPrefix, OutputLanguageFlagName, OutputLanguageExampleTags)
	}
}

// TestRefusalAndPageTeachTheSameVocabulary closes the loop the epic opened: the
// error a user gets and the page they read must teach the same class with the
// same examples. Before this change the page taught nothing while the message
// taught `zh, zh-CN, en-GB` — two surfaces, two answers to one question.
func TestRefusalAndPageTeachTheSameVocabulary(t *testing.T) {
	writeOutputLanguageSettings(t, `{}`)
	t.Setenv(OutputLanguageEnv, "chinese")
	var got string
	stderr := captureStderr(t, func() { got = OutputLanguage() })

	if got != "" {
		t.Fatalf("`chinese` is not a language tag and must resolve to unset: got %q", got)
	}
	for _, want := range []string{
		OutputLanguageValueShape,  // the class, named the same way on both surfaces
		OutputLanguageExampleTags, // the same worked examples
		`"chinese"`,               // and the value actually refused, quoted back
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal message does not carry %q — the error and the page must teach the "+
				"same vocabulary, got stderr=%q", want, stderr)
		}
	}
}

// TestOperatorQuestionIsAnswerableFromThePage pins the exact question that
// opened this epic, as a behaviour rather than a wording: the page must let a
// reader conclude BOTH that `chinese` is not accepted AND that `zh-CN` is.
func TestOperatorQuestionIsAnswerableFromThePage(t *testing.T) {
	flat := flatUsage()
	if !strings.Contains(flat, "chinese") {
		t.Errorf("`gopi -h` never mentions the language NAME a reader would try first (`chinese`), " +
			"so the page cannot tell them it is the wrong spelling — the question the epic asked")
	}
	if !strings.Contains(flat, "zh-CN") {
		t.Errorf("`gopi -h` never shows a subtagged example (`zh-CN`) — a bare `zh` alone does not " +
			"reveal that region subtags are legal")
	}
}
