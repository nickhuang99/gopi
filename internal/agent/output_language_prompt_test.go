package agent

// output_language_prompt_test.go — the reply-language directive at the HEAD of
// BOTH system prompt paths, and the byte-identity contract when it is unset.
//
// Why this file exists separately from internal/config's own tests: the config
// package owns the RESOLUTION (which channel decides, what a usable value is,
// what gets reported). What it cannot see is the EMISSION half — that the two
// prompt paths actually carry the directive, that they carry it at the head,
// and that with nothing configured they return exactly the string they returned
// before the knob existed. That half lives here, against the real surfaces
// (assembleSystemPrompt and chatSystemPromptServed), not against a helper.
//
// The byte-identity check compares against a FROZEN copy of the pre-knob chat
// prompt text and against the registry's own join: a test that compared
// assembleSystemPrompt() to itself would pass no matter what the knob did.
//
// Rule 17: every env flip is t.Setenv (test-scoped).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFileString is the fixture writer (one place, so the settings-file shape
// cannot differ between this file's cases).
func writeFileString(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// freezePreKnobChatPrompt is the chat system prompt's PERSONA BODY — exactly
// as master carried it, up to and including the sentence this task replaced.
// It is frozen here on purpose: the byte-identity contract is a promise about
// the PREVIOUS behaviour, so the body must not be re-derived from the code
// under test, or renaming a constant would keep the test green while the prompt
// changed.
//
// IT WAS AMENDED ONCE, DELIBERATELY (task 01a0e167; epic: "the chat surface must
// be told its OWN context"). The pre-task third sentence — "Do not mention
// tools, task lists, or system internals unless asked" — is the sentence that
// MADE the defect: it forbade the surface from saying what this same task now
// TELLS it (where it runs, how to read its own screen). Keeping the freeze at
// the old text would have pinned the defect as the contract, so the freeze
// moved with the ruling, and the amendment is recorded here rather than
// silently absorbed. The body is still frozen — a future renaming cannot slip
// past it.
const freezePreKnobChatPrompt = "You are GoPi in chat mode, a helpful conversational assistant. " +
	"Reply directly to the user's message in a natural, human-readable way. " +
	"Honesty over fluency: never claim to have seen, run or checked something you have not."

// chatBodyOf returns the persona body a served chat prompt carries — the served
// prompt with the runtime chat-context block (and any reply-language directive)
// removed, so a placement assertion can be made about the BODY without the
// test re-typing it.
func chatBodyOf(served string) string {
	const marker = promptSectionSeparator + chatContextHead
	if i := strings.Index(served, marker); i >= 0 {
		return served[:i]
	}
	return served
}

// settingsWithOutputLanguage writes a settings.json fixture carrying (or not)
// the knob and points GOPI_SETTINGS at it, clearing the env channel.
func settingsWithOutputLanguage(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := writeFileString(p, content); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", p)
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "")
}

// TestChatPromptByteIdenticalWhenUnset is half the compatibility contract: with
// no reply-language channel configured, the chat path's PERSONA BODY is the
// EXACT pre-knob string, byte for byte, with the runtime chat-context block
// appended after it (never interleaved, never substituted into it).
func TestChatPromptByteIdenticalWhenUnset(t *testing.T) {
	settingsWithOutputLanguage(t, `{"model":"m"}`)
	got := chatBodyOf(chatSystemPromptServed(nil))
	if got != freezePreKnobChatPrompt {
		t.Fatalf("chatSystemPromptServed(nil) body with the knob unset is NOT byte-identical to master:\n got=%q\nwant=%q", got, freezePreKnobChatPrompt)
	}
	if len(got) != len(freezePreKnobChatPrompt) {
		t.Fatalf("length differs: %d vs %d", len(got), len(freezePreKnobChatPrompt))
	}
}

// TestTaskPromptByteIdenticalWhenUnset is the other half: the TASK path's
// assembled prompt is a plain join of the registry's non-empty sections, with
// nothing prepended. It is asserted against that join rather than against a
// frozen 4KB string, because the registry already has a dense expectation
// battery (prompt_test.go) and what this test must prove is the knobs's
// contribution: NOTHING.
func TestTaskPromptByteIdenticalWhenUnset(t *testing.T) {
	settingsWithOutputLanguage(t, `{"model":"m"}`)
	want := unprefixedAssembledPrompt()
	got := assembleSystemPrompt()
	if got != want {
		t.Fatalf("assembleSystemPrompt() with the knob unset differs from the registry join — the knob added something:\n got[:120]=%q\nwant[:120]=%q", head(got), head(want))
	}
	if strings.HasPrefix(got, outputLanguageDirectiveMarker) {
		t.Fatal("an unset knob emitted the output-language directive")
	}
}

// outputLanguageDirectiveMarker is the head of every directive the knob emits.
// internal/config owns the full sentence; this file needs only the PREFIX, and
// it is declared once here (Rule 1) so the assertions below cannot drift from
// each other. It is also self-pinning: TestTaskPromptCarriesDirectiveAtHead
// asserts a configured prompt STARTS with this prefix, so a change to the
// config side's wording fails that test instead of silently un-checking every
// absence assertion in this file.
const outputLanguageDirectiveMarker = "[OUTPUT LANGUAGE"

// taskPromptBody is the task path's prompt body WITHOUT the output-language
// directive, obtained by driving the REAL composition path with the knob
// cleared. Deriving it this way (rather than re-typing the header sentence)
// is what makes the PREPEND assertion below meaningful: the expectation is the
// text the production path itself produces when it has nothing to prepend, so
// TestTaskPromptCarriesDirectiveAtHead cannot pass against a stale copy of the
// header — it fails the moment the body changes without the directive changing.
func taskPromptBody(t *testing.T) string {
	t.Helper()
	settingsWithOutputLanguage(t, `{}`)
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "")
	full := assembleSystemPrompt()
	if strings.HasPrefix(full, outputLanguageDirectiveMarker) {
		t.Fatalf("precondition: the knob must be silent for the body fixture, got %q", head(full))
	}
	return full
}

// unprefixedAssembledPrompt reproduces the registry join WITHOUT the
// output-language step. It is written out here deliberately (not by calling the
// production helper) so the test does not verify the new code with the new code.
// It honours Render exactly as the production join does — a section whose text
// is resolved at assembly time (the header, whose reply-language half reads the
// knob) is otherwise invisible to this expectation, and the byte-identity case
// would fail for a reason that has nothing to do with the knob.
func unprefixedAssembledPrompt() string {
	var parts []string
	for _, s := range promptSections {
		text := s.Text
		if s.Render != nil {
			text = s.Render()
		}
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

func head(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

// tail is head's counterpart: the LAST 120 bytes, so a failure message can show
// where a body ended up (the PREPEND assertions compare the prompt's tail to the
// body, and printing only the head would hide exactly what is being checked).
func tail(s string) string {
	if len(s) > 120 {
		return s[len(s)-120:]
	}
	return s
}

// TestTaskPromptCarriesDirectiveAtHead drives the task path with the knob set
// through the ENV channel and pins the placement: the directive is the FIRST
// thing in the prompt, before the English header that would otherwise set the
// reply's language by inertia.
func TestTaskPromptCarriesDirectiveAtHead(t *testing.T) {
	// The body is captured with the knob cleared (the real composition path,
	// nothing prepended), then the SAME path is driven with the knob set: the
	// configured prompt must be the body with the directive IN FRONT of it.
	body := taskPromptBody(t)
	settingsWithOutputLanguage(t, `{}`)
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "zh-CN")
	got := assembleSystemPrompt()
	if !strings.HasPrefix(got, outputLanguageDirectiveMarker) {
		t.Fatalf("the directive must be at the HEAD of the task prompt, got %q", head(got))
	}
	if !strings.Contains(got, "zh-CN") {
		t.Fatalf("the directive must name the configured language, got %q", head(got))
	}
	// PREPEND, never replace: the whole body is still there, at the head's
	// side of the prompt, byte for byte (not a re-rendered "equivalent").
	if !strings.Contains(got, body) {
		t.Fatalf("the body must survive the directive intact; body head=%q prompt head=%q", head(body), head(got))
	}
	if !strings.HasSuffix(got, body) {
		t.Fatalf("the body must come AFTER the directive — got head=%q tail=%q", head(got), tail(got))
	}
	// The separator is the registry's own blank line, so the body is intact.
	if !strings.Contains(got, promptSectionSeparator+body) {
		t.Fatalf("the directive must be joined to the body with one blank line, got %q", head(got))
	}
}

// TestChatPromptCarriesDirectiveAtHead is the surface the operator actually
// asked about (`gopi chat`), asserted on the same helper the request path uses.
func TestChatPromptCarriesDirectiveAtHead(t *testing.T) {
	settingsWithOutputLanguage(t, `{"output_language":"zh-CN"}`)
	got := chatSystemPromptServed(nil)
	if !strings.HasPrefix(got, outputLanguageDirectiveMarker) {
		t.Fatalf("the chat prompt must open with the directive, got %q", head(got))
	}
	// Placement, spelled as a structural claim rather than a substring hunt: the
	// prompt is <directive> <blank line> <body> …, i.e. the directive is the head
	// and the persona body starts at the very next block, nothing between them.
	first, rest, ok := strings.Cut(got, promptSectionSeparator)
	if !ok {
		t.Fatalf("the chat prompt has no section separator at all: %q", got)
	}
	if !strings.HasPrefix(first, outputLanguageDirectiveMarker) {
		t.Fatalf("the FIRST block must be the directive, got %q", head(first))
	}
	if !strings.HasPrefix(rest, freezePreKnobChatPrompt) {
		t.Fatalf("the persona body must start immediately after the directive, got %q", head(rest))
	}
	if strings.Count(got, freezePreKnobChatPrompt) != 1 {
		t.Fatalf("the body must appear exactly once, got %d occurrences", strings.Count(got, freezePreKnobChatPrompt))
	}
	if !strings.Contains(got, chatContextHead) {
		t.Fatalf("the chat-context block must survive the directive: %q", head(got))
	}
}

// TestCompactionPromptCarriesNoDirective is the SCOPE ruling, asserted rather
// than claimed: compaction's system prompt asks the model to SUMMARISE into the
// context, which is not a reply to the user. A "reply in Chinese" instruction
// there would change what gets written into the context — outside the operator's
// ruling and a real behavioural change.
func TestCompactionPromptCarriesNoDirective(t *testing.T) {
	settingsWithOutputLanguage(t, `{}`)
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "zh-CN")
	if strings.Contains(compactionSystemPrompt(), outputLanguageDirectiveMarker) {
		t.Fatal("the compaction prompt carries the output-language directive — it is out of scope (it is not a reply to the user)")
	}
}

// TestOutputLanguageUnsetAfterSetIsSilentAgain pins the reversible shape: an
// operator who clears the knob gets the silent prompt back, with no residue
// (no stray separator, no empty directive line).
func TestOutputLanguageUnsetAfterSetIsSilentAgain(t *testing.T) {
	settingsWithOutputLanguage(t, `{}`)
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "zh-CN")
	set := chatSystemPromptServed(nil)
	if !strings.HasPrefix(set, outputLanguageDirectiveMarker) {
		t.Fatalf("precondition: the knob must be in effect, got %q", head(set))
	}
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "")
	cleared := chatBodyOf(chatSystemPromptServed(nil))
	if cleared != freezePreKnobChatPrompt {
		t.Fatalf("clearing the knob must restore the byte-identical body:\n got=%q\nwant=%q", cleared, freezePreKnobChatPrompt)
	}
}
