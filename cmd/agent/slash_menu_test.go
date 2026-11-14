package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// slash_menu_test.go — what the `/`-menu OFFERS, and what pressing Enter on it
// does to the screen the operator then looks at.
//
// The three operator reports this file pins (task 01a0d508, attachments
// tmp/ui_exit.txt + tmp/ui_cleanup.txt):
//
//  1. "/quit (session)" beside "/exit (session)" read as TWO commands and the
//     operator asked what the difference was. There is none: quit and exit are
//     one behaviour with two spellings, exactly like /clear and /new — and the
//     menu has to say so ON the row.
//  2. walking the menu with Up/Down and pressing Enter did nothing: the raw "/"
//     was submitted and the highlighted candidate was discarded (the editor half
//     of that contract is internal/lineedit's
//     TestChatCompletion_EnterTakesTheSelectedCandidate; this file pins it at the
//     REPL's own seam, over the PRODUCTION completer).
//  3. on /quit the reserved layout was left on screen — the answer band's rows,
//     the editing row and the status row stayed painted and the shell's own next
//     prompt landed inside them (attachment tmp/ui_cleanup.txt). That contract is
//     closeout_test.go's; this file owns 1 and 2.
//
// The unit of truth is the RENDERED CELL GRID (Rule 10.2): the bytes the editor
// actually wrote, replayed into a vt100 grid — never the layout's own arithmetic
// and never "the callback fired".

// slashHarness builds the chat input layer over the PRODUCTION slash completer —
// the same one newChatInputWithCompleter installs — so a menu assertion is about
// the menu the operator is really handed.
func slashHarness(t *testing.T) *completionHarness {
	t.Helper()
	h := newCompletionHarness(t, &completionStub{})
	h.in.editor.SetCompleter(NewSlashCompleter())
	return h
}

// menuCandidateList asks the production completer the way the editor does — for
// the bare slash prefix, at the menu's own candidate limit.
func menuCandidateList(t *testing.T, prefix string) []lineedit.Candidate {
	t.Helper()
	cands := NewSlashCompleter()(prefix, len([]rune(prefix)), lineedit.CompletionCandidateLimit())
	if len(cands) == 0 {
		t.Fatalf("the slash completer offered nothing for %q — the menu is empty", prefix)
	}
	return cands
}

// TestSlashMenuPresentsOneRowPerBehaviourWithItsAlias is defect 1's contract.
//
// For every spelling that shares its behaviour with another (/quit–/exit,
// /clear–/new) the menu must say so ON THE ROW: "alias of <the other spelling>".
// The operator read two rows that looked like two commands and asked what the
// difference was (attachment tmp/ui_exit.txt); there is none, and the row is
// where that has to be visible. The contract is SYMMETRIC, because the pair is:
// whoever carries the row points at the other.
func TestSlashMenuPresentsOneRowPerBehaviourWithItsAlias(t *testing.T) {
	cands := menuCandidateList(t, completionSlashPrefix)
	byText := map[string]lineedit.Candidate{}
	for _, c := range cands {
		byText[c.Text] = c
	}
	for canonical, alias := range chatSessionAliases {
		c, ok := byText[completionSlashPrefix+canonical]
		if !ok {
			t.Errorf("the menu offers no %s%s row, want one per session command\n--- menu ---\n%s",
				completionSlashPrefix, canonical, menuDump(cands))
			continue
		}
		want := chatSessionAliasNote(alias)
		if c.Hint != want {
			t.Errorf("%s%s hint = %q, want %q — the row must say the two spellings are ONE command\n--- menu ---\n%s",
				completionSlashPrefix, canonical, c.Hint, want, menuDump(cands))
		}
	}
	// BOTH spellings stay reachable — a word the menu stopped offering is a
	// command the user can only reach by typing it in full — so the contract is
	// not "one row" but SYMMETRY: if a pair appears as two rows, each row must
	// name the other. Two rows that look like two commands is the defect; two
	// rows that each say "alias of the other" is the answer to the operator's
	// question, and it is what keeps either spelling selectable.
	for canonical, alias := range chatSessionAliases {
		a, ok := byText[completionSlashPrefix+alias]
		if !ok {
			continue
		}
		if want := chatSessionAliasNote(canonical); a.Hint != want {
			t.Errorf("%s%s hint = %q, want %q — a row offered beside its twin must name the twin\n--- menu ---\n%s",
				completionSlashPrefix, alias, a.Hint, want, menuDump(cands))
		}
	}
}

// TestSlashMenuOffersTheSharedTableAndEverySessionWord is the menu's COVERAGE
// contract, and the guard that keeps the alias work from shrinking the menu: a
// word the menu no longer offers is a command the operator can only reach by
// typing it in full.
func TestSlashMenuOffersTheSharedTableAndEverySessionWord(t *testing.T) {
	cands := menuCandidateList(t, completionSlashPrefix)
	seen := map[string]bool{}
	for _, c := range cands {
		seen[c.Text] = true
	}
	for _, name := range slashCommandNames() {
		if !seen[completionSlashPrefix+name] {
			t.Errorf("the menu does not offer %s%s (a shared-table command)\n--- menu ---\n%s",
				completionSlashPrefix, name, menuDump(cands))
		}
	}
	for _, name := range chatSessionWords {
		if !seen[completionSlashPrefix+name] {
			t.Errorf("the menu does not offer %s%s (a REPL-native session command)\n--- menu ---\n%s",
				completionSlashPrefix, name, menuDump(cands))
		}
	}
}

// TestSlashMenuRowsRenderTheAliasNote is the RENDER half of defect 1: the note
// reaches the CELL GRID, not merely the candidate struct. It drives the real
// editor over the production completer and reads the row the operator sees.
func TestSlashMenuRowsRenderTheAliasNote(t *testing.T) {
	h := slashHarness(t)
	h.typeKeys(completionSlashPrefix)
	h.waitFor(t, "the slash menu to be drawn", func() bool {
		h.replay()
		return h.rowContaining(completionSlashPrefix+quitCommandName) >= 0
	})

	aliasRow := h.rowContaining(completionSlashPrefix + quitCommandName)
	want := chatSessionAliasNote(exitCommandName)
	got := h.screen.Line(aliasRow)
	if !strings.Contains(got, want) {
		t.Fatalf("the %s%s row = %q, want it to carry %q (one command, its other spelling named)\n--- screen ---\n%s",
			completionSlashPrefix, quitCommandName, got, want, h.screen.Contents())
	}
	// No row of its OWN offers /exit: every row that mentions it mentions it as
	// the alias note INSIDE another command's row, never as its own label. The
	// check reads the row's FIRST field (its label), not its text — the alias
	// note legitimately contains "/exit".
	for row := 0; row < h.screen.Rows; row++ {
		if firstWordOnRow(h.screen.Line(row)) == completionSlashPrefix+exitCommandName {
			t.Fatalf("the menu draws a SEPARATE %s%s row at screen row %d (the %s row is %d), want one row that names the alias\n--- screen ---\n%s",
				completionSlashPrefix, exitCommandName, row, quitCommandName, aliasRow, h.screen.Contents())
		}
	}
}

// TestSlashMenuEnterTakesTheChoiceIntoTheInputSlot is defect 2 end to end at the
// REPL's seam, over the production completer: "/" opens the menu, Down walks it,
// and ENTER — not Tab — takes the highlighted candidate into the input slot.
//
// Enter must not submit in the same keystroke: the operator's report is that the
// choice never reached the slot, and an Enter that both chose and sent would take
// the decision away from the line the user is looking at. The second Enter is
// what sends, and the test asserts BOTH halves — that is what makes this a
// contract about a real editing session rather than about one key press.
func TestSlashMenuEnterTakesTheChoiceIntoTheInputSlot(t *testing.T) {
	h := slashHarness(t)
	h.typeKeys(completionSlashPrefix)
	h.waitFor(t, "the slash menu to be drawn", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.menuRow(0)), completionSlashPrefix)
	})

	// Down: the SECOND row becomes the selection, and the test reads the text it
	// actually offers — an assertion on a fixed candidate would agree with a menu
	// whose order moved.
	h.typeKeys("\x1b[B")
	h.waitFor(t, "the selection marker on the second menu row", func() bool {
		h.replay()
		return strings.HasPrefix(h.screen.Line(h.menuRow(1)), menuMarkerOnScreen)
	})
	chosen := firstWordOnRow(h.screen.Line(h.menuRow(1)))
	if chosen == "" {
		t.Fatalf("second menu row = %q, want a candidate\n--- screen ---\n%s", h.screen.Line(h.menuRow(1)), h.screen.Contents())
	}

	h.typeKeys("\r")
	h.waitFor(t, "the accepted candidate on the editing line", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.lineRow()), chosen)
	})
	if got := h.screen.Line(h.lineRow()); !strings.Contains(got, chosen) {
		t.Fatalf("editing row after Enter = %q, want the SELECTED candidate %q in the input slot\n--- screen ---\n%s",
			got, chosen, h.screen.Contents())
	}
	if line, ok := h.submitted(); ok {
		t.Fatalf("Enter submitted %q while taking the menu's selection, want the choice in the input slot FIRST", line)
	}

	h.typeKeys("\r")
	select {
	case res := <-h.done:
		if res.line != chosen {
			t.Fatalf("submitted line = %q, want the accepted candidate %q", res.line, chosen)
		}
	case <-timeoutAfter(completionTimeout):
		t.Fatalf("the second Enter never submitted the accepted line\n--- screen ---\n%s", h.screen.Contents())
	}
}

// menuMarkerOnScreen is the marker a rendered menu row starts with when it is
// the CURRENT selection. It names internal/lineedit's own marker vocabulary at
// the assertion site rather than trusting a copy of it.
const menuMarkerOnScreen = ">"

// timeoutAfter is time.After, named so the wait's channel is built the same way
// everywhere in this file.
func timeoutAfter(d time.Duration) <-chan time.Time { return time.After(d) }

// firstWordOnRow strips a menu row's selection marker and returns its candidate
// text (the row is "<marker> <candidate> (<hint>)").
func firstWordOnRow(row string) string {
	fields := strings.Fields(row)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

// menuDump renders a candidate list for a failure message.
func menuDump(cands []lineedit.Candidate) string {
	var b strings.Builder
	for _, c := range cands {
		fmt.Fprintf(&b, "  %-14s (%s)\n", c.Text, c.Hint)
	}
	return b.String()
}

// slashSingleCandidateWord is the command word this file's third contract is
// written over. The menu is PREFIX-matched, so the contract needs a word that is
// a prefix of exactly ONE command on the production table — and "/qu" is no
// longer one: `/quit` (a REPL-native session command) shares it with `/queue`,
// which ff76758c added. "/qui" is the shortest prefix the shipped vocabulary
// still leaves single-candidate, and menuCandidateList asserts that below: a
// later table change that widens THIS one fails loudly instead of silently
// turning the test into a two-candidate case (which is exactly how "/q" broke).
const slashSingleCandidateWord = "qui"

// slashSingleCandidatePrefix is the line the operator types for the contract
// above, built from the ONE slash constant — never re-typed (Rule 1b/13).
const slashSingleCandidatePrefix = completionSlashPrefix + slashSingleCandidateWord

// slashTwoCandidateWord is the OTHER half of the same seam, and it is the prefix
// this fixture used to occupy: "/q" reaches TWO commands now (/quit and /queue),
// so the ordinary two-candidate menu is pinned on it (below) rather than left as
// the gap the one-candidate fixture stopped covering.
const slashTwoCandidateWord = "q"

const slashTwoCandidatePrefix = completionSlashPrefix + slashTwoCandidateWord

// TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove is defect 3, over the REAL
// completer and the REAL editor: the operator typed a slash prefix that matched
// ONE command, pressed Arrow-Down, and the screen did not change at all — so the
// shortcut read as broken.
//
// The contract: "/q" opens a ONE-row menu whose sole candidate is already
// selected, Down (then Up) leaves it selected, and the menu STATES the reason on
// a row of its own — "nothing to move to" — so the press is never a silent no-op.
// The screen is the unit of truth (Rule 10.2): the bytes are replayed into a
// vt100 grid and every assertion reads cells.
func TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove(t *testing.T) {
	cands := menuCandidateList(t, slashSingleCandidatePrefix)
	if len(cands) != 1 {
		t.Fatalf("the production completer offers %d candidates for %q (want exactly 1, the one-item case this contract is about)\n--- menu ---\n%s",
			len(cands), slashSingleCandidatePrefix, menuDump(cands))
	}
	// The sole candidate must be a word the REPL's OWN dispatcher vocabulary
	// accepts — the shared table's words plus the REPL-native session words the
	// turn loop switches on (chatReplCommandNames: the same list the REPL's
	// refusal and its /help page are built from, Rules 13/14). The candidate here
	// is `/quit`, a SESSION word the shared registry does not own, so a
	// table-only check would be both wrong and vacuous; membership in the
	// carrier's vocabulary is what makes the offer executable, and it fails the
	// moment the word loses its dispatcher.
	accepted := chatReplCommandNames()
	for _, c := range cands {
		if !containsString(accepted, strings.TrimPrefix(c.Text, completionSlashPrefix)) {
			t.Fatalf("the menu offers %q, which the REPL's own vocabulary (chatReplCommandNames) does not accept — the menu would offer a command its carrier refuses", c.Text)
		}
	}
	note := lineedit.CompletionMenuNote()

	h := slashHarness(t)
	h.typeKeys(slashSingleCandidatePrefix)
	h.waitFor(t, "the one-candidate menu to be drawn", func() bool {
		h.replay()
		return h.rowContaining(cands[0].Text) >= 0
	})
	solo := h.rowContaining(cands[0].Text)
	if got := h.screen.Line(solo); !strings.HasPrefix(got, menuMarkerOnScreen) {
		t.Fatalf("the sole candidate row = %q, want the marker %q on it\n--- screen ---\n%s",
			got, menuMarkerOnScreen, h.screen.Contents())
	}
	// The row count cmd/agent reads off the menu must include the note row, or
	// the layout reserves fewer rows than are painted and the status row below
	// the menu is the casualty.
	if got, want := h.in.editor.MenuState().Rows(), len(cands)+lineedit.CompletionNoteRow(); got != want {
		t.Fatalf("Menu.Rows() = %d for a %d-candidate menu, want %d (candidates + note row)\n--- screen ---\n%s",
			got, len(cands), want, h.screen.Contents())
	}
	if noteRow := h.rowContaining(note); noteRow < 0 {
		t.Fatalf("the one-candidate menu does not state the reason on screen; want a row carrying %q\n--- screen ---\n%s",
			note, h.screen.Contents())
	}

	// DOWN then UP, each measured against the screen: the selection stays on the
	// sole row and the reason stays visible.
	for _, key := range []string{"\x1b[B", "\x1b[A"} {
		beforeWrites := h.out.count()
		h.typeKeys(key)
		h.waitFor(t, "the arrow press to be handled", func() bool { return h.out.count() > beforeWrites })
		waitForQuietWritesSlash(h)
		if got := h.screen.Line(h.rowContaining(cands[0].Text)); !strings.HasPrefix(got, menuMarkerOnScreen) {
			t.Fatalf("after the arrow press the sole candidate row = %q, want the marker %q still on it\n--- screen ---\n%s",
				got, menuMarkerOnScreen, h.screen.Contents())
		}
		if h.rowContaining(note) < 0 {
			t.Fatalf("after the arrow press the menu no longer states %q\n--- screen ---\n%s", note, h.screen.Contents())
		}
	}

	// The one-candidate row is not an offer: it carries NO selection marker and
	// the sole OFFER is the candidate above it.
	if noteRow := h.rowContaining(note); strings.HasPrefix(h.screen.Line(noteRow), menuMarkerOnScreen) {
		t.Fatalf("the note row = %q starts with the selection marker %q — the reason must not read as a second candidate",
			h.screen.Line(noteRow), menuMarkerOnScreen)
	}
	// And the sole candidate is still the one Tab/Enter takes.
	h.typeKeys("\r")
	h.waitFor(t, "the sole candidate accepted into the input slot", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.lineRow()), cands[0].Text)
	})

	// The submit ends the editing session: an editor left parked in its read loop
	// fails the suite's goroutine sweep, and the accepted line is the one worth
	// checking on the way out (the menu must not have swallowed the submit key).
	h.typeKeys("\r")
	select {
	case res := <-h.done:
		if res.line != cands[0].Text {
			t.Fatalf("submitted line = %q, want the accepted candidate %q", res.line, cands[0].Text)
		}
	case <-timeoutAfter(completionTimeout):
		t.Fatalf("the second Enter never submitted the accepted line\n--- screen ---\n%s", h.screen.Contents())
	}
}

// TestSlashMenuTwoCandidatesWrapsAndOffersNoReason pins the OTHER side of the
// same seam, on the prefix the one-candidate fixture used to occupy: "/q"
// reached ONE command when that fixture was written (/quit, the operator's own
// report) and reaches TWO now — ff76758c added `/queue` — so the one-candidate
// contract moved to "/qu" and THIS case keeps "/q" under test as the ordinary
// two-candidate menu.
//
// The contract: two candidates, the first selected, Arrow-Down MOVES the marker
// to the second (the no-op the one-candidate case has to explain never happens
// here), and the one-candidate reason row is ABSENT — the note exists only at
// n == 1, so a menu that painted it always would fail here.
func TestSlashMenuTwoCandidatesWrapsAndOffersNoReason(t *testing.T) {
	cands := menuCandidateList(t, slashTwoCandidatePrefix)
	if len(cands) != 2 {
		t.Fatalf("the production completer offers %d candidates for %q, want exactly 2 — this is the prefix the one-candidate fixture abandoned, and /queue is what widened it\n--- menu ---\n%s",
			len(cands), slashTwoCandidatePrefix, menuDump(cands))
	}
	h := slashHarness(t)
	h.typeKeys(slashTwoCandidatePrefix)
	h.waitFor(t, "the two-candidate menu to be drawn", func() bool {
		h.replay()
		return h.rowContaining(cands[0].Text) >= 0 && h.rowContaining(cands[1].Text) >= 0
	})
	// No reason row is spent at two candidates, so the menu's own row count is
	// the candidate count (the one-candidate case adds lineedit.CompletionNoteRow()).
	if got, want := h.in.editor.MenuState().Rows(), len(cands); got != want {
		t.Fatalf("Menu.Rows() = %d for a %d-candidate menu, want %d (the reason row belongs to the one-candidate menu)\n--- screen ---\n%s",
			got, len(cands), want, h.screen.Contents())
	}
	if row := h.rowContaining(lineedit.CompletionMenuNote()); row >= 0 {
		t.Fatalf("the two-candidate menu states %q on row %d — the reason is the one-candidate case's, not a permanent banner\n--- screen ---\n%s",
			lineedit.CompletionMenuNote(), row, h.screen.Contents())
	}
	first, second := h.rowContaining(cands[0].Text), h.rowContaining(cands[1].Text)
	if got := h.screen.Line(first); !strings.HasPrefix(got, menuMarkerOnScreen) {
		t.Fatalf("row %d = %q, want the selection marker %q on the first candidate\n--- screen ---\n%s",
			first, got, menuMarkerOnScreen, h.screen.Contents())
	}
	// DOWN moves the marker onto the second candidate — the arrow is not a
	// no-op here — and the first row loses it: exactly one row is selected.
	beforeWrites := h.out.count()
	h.typeKeys("\x1b[B")
	h.waitFor(t, "the arrow press to be handled", func() bool { return h.out.count() > beforeWrites })
	waitForQuietWritesSlash(h)
	if got := h.screen.Line(second); !strings.HasPrefix(got, menuMarkerOnScreen) {
		t.Fatalf("after Down, row %d = %q, want the marker %q moved onto the second candidate\n--- screen ---\n%s",
			second, got, menuMarkerOnScreen, h.screen.Contents())
	}
	if got := h.screen.Line(first); strings.HasPrefix(got, menuMarkerOnScreen) {
		t.Fatalf("after Down the first candidate row is STILL marked = %q — exactly one row carries the selection\n--- screen ---\n%s",
			got, h.screen.Contents())
	}
	// Enter takes the selection into the input slot and the second Enter sends
	// it — the same editing session the one-candidate case ends with.
	h.typeKeys("\r")
	h.waitFor(t, "the selected candidate accepted into the input slot", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.lineRow()), cands[1].Text)
	})
	h.typeKeys("\r")
	select {
	case res := <-h.done:
		if res.line != cands[1].Text {
			t.Fatalf("submitted line = %q, want the accepted candidate %q", res.line, cands[1].Text)
		}
	case <-timeoutAfter(completionTimeout):
		t.Fatalf("the second Enter never submitted the accepted line\n--- screen ---\n%s", h.screen.Contents())
	}
}

// waitForQuietWritesSlash is slashHarness's own settle wait: the completion
// worker publishes from its own goroutine, so a write count is only meaningful
// once the stream has been quiet for completionPollStep.
func waitForQuietWritesSlash(h *completionHarness) {
	deadline := time.Now().Add(completionTimeout)
	last := time.Now()
	writes := h.out.count()
	for time.Since(last) < completionPollStep {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(completionPollStep) //stylegate:allow-sleep waiting out the completion worker's own publish repaint, bounded by completionTimeout
		if n := h.out.count(); n != writes {
			writes, last = n, time.Now()
		}
	}
	h.replay()
}
