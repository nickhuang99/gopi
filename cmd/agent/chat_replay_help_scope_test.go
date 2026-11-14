package main

// chat_replay_help_scope_test.go — the SCOPED HELP contract for the replay/film
// family (`gopi chat --replay -h`).
//
// WHY THIS FILE EXISTS. The page change is trivial to make and trivial to
// un-make: a later edit can drop a family word from the scoped page, re-inline an
// interactive-only flag onto it, or turn the scope decision into a constant and
// nothing in the suite notices. So the contract is pinned in BOTH currencies:
//
//   - ON THE CONSTANTS (fast, exhaustive over the family's vocabulary): every word
//     the family's parser accepts is on the scoped page, no interactive-only word
//     is, the main page NAMES the scoped page, and the scoped page is COMPOSED
//     from the same named parts the main page joins (Rule 13: one definition,
//     two renderings — a second copy of a flag's prose would pass every
//     containment check below while being exactly the drift this epic removed).
//   - ON THE REAL BINARY (Rule 31): the argv is what a user types. The scope
//     decision is proven to read ARGV — not a constant — by driving four real
//     command lines and comparing the pages they print. A resolver that answered
//     from a constant would make `--replay -h` and `--tools -h` print the same
//     page, and this file's real-binary arm is what turns that red.
//
// Rule 30: the probe's scratch home is t.TempDir() and no artifact is written
// into the worktree.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// chatReplayScopeInteractiveOnly are the words that must NEVER reach the scoped
// page: they configure an interactive session, and a reader on the replay/film
// page has no session to configure. They are the parser's own constants
// (Rule 1/13) — a renamed flag follows the compiler rather than leaving a stale
// spelling here.
func chatReplayScopeInteractiveOnly() []string {
	return []string{
		chatFlagTools,
		chatFlagNoEditor,
		chatFlagNoColor,
		chatFlagVerbose,
		chatFlagResume,
		chatPolicyFlag,
		chatDumpFlag,
		chatNoDumpFlag,
		chatAmbiguousWidthFlag,
		// NOT in this list, by name: chatGeometryFlag. The ideal geometry is a
		// SHARED word — it is the canvas a film is drawn at as much as it is an
		// interactive run's width — so it legitimately appears on both pages; a
		// list that refused it here would be a rule about the page's prose rather
		// than about its scope.
	}
}

// TestChatScopedPagesFitTheWrapBudget measures the two COMPOSED pages against the
// same wrap column every help constant is held to. The package's own sweep
// (help_width_test.go) walks CONSTANTS, and both of these are vars — the main
// page's index is padded by a call, the scoped page joins other vars — so their
// width is pinned HERE rather than left to a sweep that cannot see them (Rule 15:
// an exemption that is not measured is an exemption that drifts).
func TestChatScopedPagesFitTheWrapBudget(t *testing.T) {
	for _, page := range []struct {
		name string
		text string
	}{
		// chatUsage is deliberately NOT here: it is a var the package sweep cannot
		// reach and whose long lines PRE-DATE this change (measured: the synopsis
		// row and ten prose rows run past the column). Fixing them is its own task,
		// not a side effect of adding a scope — what this file must not do is make
		// the situation worse, which is why the two pages IT introduces are
		// measured, line for line.
		{"chatReplayGroupUsage", chatReplayGroupUsage},
		{"chatReplayHelp", chatReplayHelp},
		{"chatReplayHelpPointsHere", chatReplayHelpPointsHere},
	} {
		for i, line := range strings.Split(page.text, "\n") {
			if w := config.DisplayWidth(line); w > config.HelpWrapCol {
				t.Errorf("%s line %d is %d display cols (> %d): %q", page.name, i+1, w, config.HelpWrapCol, line)
			}
		}
	}
}

// TestChatReplayHelpCarriesTheWholeFamily is the CONTAINMENT half: the family is
// on its own page, whole.
func TestChatReplayHelpCarriesTheWholeFamily(t *testing.T) {
	for _, flag := range chatReplayHelpFlags() {
		if !strings.Contains(chatReplayHelp, flag) {
			t.Errorf("%s is part of the replay/film family but the scoped page never names it — `gopi chat %s -h` would document a family with a hole in it",
				flag, chatFlagReplay)
		}
	}
	// The parser's own beat vocabulary must agree with the family list: a word the
	// parser accepts and this page's list forgot is invisible on the page that
	// exists to document it.
	for _, flag := range chatBeatOwnedWords() {
		if !containsString(chatReplayHelpFlags(), flag) {
			t.Errorf("%s is parsed by this verb (chatBeatOwnedWords) but is not in chatReplayHelpFlags — the family and its page disagree", flag)
		}
	}
}

// TestChatReplayHelpExcludesInteractiveOnlyFlags is the OTHER half, and it is the
// one that makes the page a SCOPE rather than a second wallet: the interactive
// session's own words are absent.
func TestChatReplayHelpExcludesInteractiveOnlyFlags(t *testing.T) {
	for _, flag := range chatReplayScopeInteractiveOnly() {
		if strings.Contains(chatReplayHelp, flag) {
			t.Errorf("%s configures an INTERACTIVE session and appears on the replay/film page — the page a reader reached by asking about `%s` must not hand them the session's flags",
				flag, chatFlagReplay)
		}
	}
}

// TestChatReplayHelpIsComposedNotCopied pins the anti-drift property the epic
// requires: the scoped page reuses the named parts, so no flag's prose exists
// twice in this package (Rule 13).
func TestChatReplayHelpIsComposedNotCopied(t *testing.T) {
	for _, part := range []struct {
		name string
		text string
	}{
		{"chatReplayUsage", chatReplayUsage},
		{"chatFilmUsage", chatFilmUsage},
		{"chatBeatUsageSection", chatBeatUsageSection},
		{"chatReplayExamples", chatReplayExamples},
	} {
		if !strings.Contains(chatReplayHelp, part.text) {
			t.Errorf("the scoped page does not join %s — it has become a SECOND copy of the family's prose instead of a rendering of it", part.name)
		}
	}
}

// TestChatMainPageNamesTheScopedPage: a reader who types `gopi chat -h` must be
// led to the page that documents the family, by a command line they can paste.
// The pointer is built from the constants, so this also pins that it is not typed
// by hand.
func TestChatMainPageNamesTheScopedPage(t *testing.T) {
	if !strings.Contains(chatUsage, chatReplayHelpPointsHere) {
		t.Fatalf("the main page never names %s — the family's own page is unreachable from the page a reader starts on", chatReplayHelpPointsHere)
	}
	if !strings.Contains(chatUsage, chatReplayGroupUsage) {
		t.Error("the main page does not carry the family's index block — the words would leave the page a reader starts on")
	}
	// Every family word still appears ONCE on the main page (the index names each),
	// so the surface census and a reader both keep meeting them (C14).
	for _, flag := range chatReplayHelpFlags() {
		if !strings.Contains(chatUsage, flag) {
			t.Errorf("%s left the main page entirely — the index is what keeps the family findable from `gopi chat -h`", flag)
		}
	}
}

// TestChatHelpScopeReadsArgv is the RED PROOF that the decision is a function of
// the command line: each spelling the parser accepts resolves to the scoped page,
// and the interactive command lines do NOT. If chatHelpScopeFor ever answered from
// a constant, the "main" cases below would go red.
func TestChatHelpScopeReadsArgv(t *testing.T) {
	cases := []struct {
		argv []string
		want chatHelpScope
		why  string
	}{
		{[]string{HelpFlag}, chatHelpScopeMain, "a bare query asks about the ordinary session"},
		{[]string{chatFlagTools, HelpFlag}, chatHelpScopeMain, "--tools is a SESSION flag, so it names no family scope"},
		{[]string{chatFlagReplay, HelpFlag}, chatHelpScopeReplay, "--replay beside the query names the family scope"},
		{[]string{chatFlagReplay + chatFlagValueSep + "abc", HelpFlag}, chatHelpScopeReplay, "the joined spelling is accepted by the parser and must scope the same way"},
		{[]string{chatFlagReplay, "abc", HelpFlag}, chatHelpScopeReplay, "the spaced spelling: the value does not change the scope"},
		{[]string{chatFlagFilm, HelpFlag}, chatHelpScopeReplay, "--film is the family's second half"},
		{[]string{chatFlagScratch, "/tmp/x", HelpFlag}, chatHelpScopeReplay, "a beat-protocol word names the same scope"},
		{[]string{chatFlagAckTimeoutMS + chatFlagValueSep + "1000", HelpFlag}, chatHelpScopeReplay, "…including the joined spelling of a beat word"},
	}
	for _, c := range cases {
		if got := chatHelpScopeFor(c.argv); got != c.want {
			t.Errorf("chatHelpScopeFor(%v) = %v, want %v — %s", c.argv, got, c.want, c.why)
		}
		if got := chatHelpPage(c.want); c.want == chatHelpScopeReplay && got != chatReplayHelp {
			t.Errorf("the replay scope resolved to the SESSION page — the scope would be a decision nothing renders")
		}
	}
}

// TestChatScopedHelpOnTheRealBinary is the Rule 31 half: four REAL command lines,
// through the REAL binary, with an isolated home. It asserts what the operator's
// shell sees — the exit status, the bytes on stderr, and which page arrived on
// stdout.
func TestChatScopedHelpOnTheRealBinary(t *testing.T) {
	p := g5NewProbe(t)

	run := func(args ...string) g5Result {
		t.Helper()
		res := p.run(t, "", args...)
		if res.exit != 0 {
			t.Fatalf("`gopi %s` exited %d, want 0 — a usage query documents a command line, it never fails one:\n%s",
				strings.Join(args, " "), res.exit, res.out)
		}
		return res
	}

	scoped := run(CliVerbChat, chatFlagReplay, HelpFlag)
	if !strings.Contains(scoped.out, chatReplayHelp) {
		t.Errorf("`gopi chat %s -h` did not print the replay/film page:\n%s", chatFlagReplay, scoped.out)
	}
	for _, flag := range chatReplayScopeInteractiveOnly() {
		if strings.Contains(scoped.out, flag) {
			t.Errorf("`gopi chat %s -h` printed %s — an interactive-only flag on the family's own page", chatFlagReplay, flag)
		}
	}

	film := run(CliVerbChat, chatFlagFilm, HelpFlag)
	if film.out != scoped.out {
		t.Errorf("`gopi chat %s -h` and `gopi chat %s -h` print different pages — the family is ONE scope:\n--- replay page tail\n%s\n--- film page tail\n%s",
			chatFlagReplay, chatFlagFilm, scoped.out, film.out)
	}

	main := run(CliVerbChat, HelpFlag)
	if main.out == scoped.out {
		t.Fatal("`gopi chat -h` printed the SCOPED page — the ordinary session page is unreachable")
	}
	if !strings.Contains(main.out, chatReplayHelpPointsHere) {
		t.Errorf("`gopi chat -h` does not name %s — the family's page has no route from the main page", chatReplayHelpPointsHere)
	}

	tools := run(CliVerbChat, chatFlagTools, HelpFlag)
	if tools.out != main.out {
		t.Errorf("`gopi chat %s -h` differs from `gopi chat -h` — an interactive flag must resolve to the SESSION page", chatFlagTools)
	}

	// The page bytes are what a census gate measures; print the pair so a reviewer
	// reads the same numbers this run did (Rule 38: the figure names its producer).
	t.Logf("main=%d bytes, replay=%d bytes, film=%d bytes, tools=%d bytes",
		len(main.out), len(scoped.out), len(film.out), len(tools.out))
}
