// chat_slash_refusal_vocabulary_test.go — the slash-VOCABULARY agreement test.
//
// THE DEFECT THIS FILE GUARDS (epic 01a0dd87): `gopi chat`'s unknown-command
// refusal printed a vocabulary that disagreed with the carrier's own help page.
// `printf '/frobnicate\n/quit\n' | gopi chat --once` said
//
//	unknown command "frobnicate" — available: code-search, log, memory, …
//
// — registry words WITHOUT the leading slash, and without /quit /exit /clear
// /tools /help, every one of which the same REPL answers (they mutate
// chatSession, so the shared table cannot see them). The operator's next line
// could therefore be a word the refusal had called unavailable, or a word the
// refusal showed in a spelling the parser does NOT accept (bare `frobnicate`
// without the slash).
//
// THE CONTRACT: refuser and help page describe ONE surface. Both derive from
// chatReplCommandNames (registry + REPL-native words + the table's reset word),
// rendered by the one renderer slashWordList; this file asserts the two token
// sets are EQUAL as sets, so adding a command to the table, or a native word to
// the switch, cannot leave the refusal behind.
package main

import (
	"sort"
	"strings"
	"testing"
)

// chatRefusalTokenSet extracts the slash vocabulary out of a refusal message the
// way an operator reads it: every token rendered after "available:" — the token
// set the refusal CLAIMS the carrier accepts. The tokens must be slash-prefixed
// (a bare word here is the defect itself, and it fails below).
func chatRefusalTokenSet(t *testing.T, msg string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(msg, "available:")
	if !ok {
		t.Fatalf("the refusal carries no available-command list:\n%s", msg)
	}
	// The list ends where the help pointer begins.
	if list, _, ok := strings.Cut(rest, ";"); ok {
		rest = list
	}
	raw := strings.Split(rest, ",")
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if !strings.HasPrefix(tok, completionSlashPrefix) {
			t.Errorf("the refusal lists %q WITHOUT a leading slash — the surface only "+
				"accepts slash-prefixed words (isSlashCommand), so a bare word names a "+
				"spelling nothing resolves:\n%s", tok, msg)
		}
		out = append(out, tok)
	}
	sort.Strings(out)
	return out
}

// TestChatRefusalVocabularyEqualsItsHelpPage is the epic's acceptance assertion
// in-process: the REPL refusal's slash-word token set == the REPL help page's
// slash-command set, and both contain every accepted word with its slash.
func TestChatRefusalVocabularyEqualsItsHelpPage(t *testing.T) {
	// The HELP PAGE's own set: /help's table rows (slash-prefixed), the REPL
	// banner's rows, and the "<<word>> (also /<alias>)" spellings — i.e. the
	// words a reader of /help learns the carrier accepts.
	help, err := chatHelpText("/help")
	if err != nil {
		t.Fatalf("chatHelpText(/help): %v", err)
	}
	helpWords := slashWordsInHelpText(help)
	if len(helpWords) == 0 {
		t.Fatalf("the help page names no slash commands — extraction is broken:\n%s", help)
	}

	refusal := chatUnknownCommandError("frobnicate")
	refusalTokens := chatRefusalTokenSet(t, refusal.Error())

	want := slashTokensForWords(helpWords)
	if strings.Join(refusalTokens, " ") != strings.Join(want, " ") {
		t.Errorf("the refusal's vocabulary disagrees with the carrier's help page:\n"+
			"  refusal: %v\n  /help:   %v", refusalTokens, want)
	}

	// The words the epic names explicitly: the refusal must list the REPL's
	// native spellings AND the registry words, each with its slash.
	for _, word := range []string{
		quitCommandName, exitCommandName, webClearAliasName, webClearCommandName,
		toolsCommandName, helpCommandName,
	} {
		if !strings.Contains(refusal.Error(), completionSlashPrefix+word) {
			t.Errorf("the refusal does not list /%s:\n%s", word, refusal.Error())
		}
	}
	for _, word := range slashCommandNames() {
		if !strings.Contains(refusal.Error(), completionSlashPrefix+word) {
			t.Errorf("the refusal does not list the registry word /%s:\n%s", word, refusal.Error())
		}
	}
}

// TestChatRefusalNamesTheRefusedWordAndKeepsTheHelpPointer pins the rest of the
// message shape, so a rewrite of the vocabulary cannot quietly drop the word
// that was refused (which the chatOnce face reports on stderr, Rule 15) or the
// pointer to the authority the list was derived from.
func TestChatRefusalNamesTheRefusedWordAndKeepsTheHelpPointer(t *testing.T) {
	msg := chatUnknownCommandError("frobnicate").Error()
	if !strings.Contains(msg, `"/frobnicate"`) {
		t.Errorf("the refusal does not name the refused word as a slash token:\n%s", msg)
	}
	if !strings.Contains(msg, completionSlashPrefix+helpCommandName) {
		t.Errorf("the refusal does not point at /%s:\n%s", helpCommandName, msg)
	}
}

// TestChatReplVocabularyCoversBothSources is the derivation guard: the REPL's
// vocabulary is the union of the shared table's accepted set and the REPL-native
// words — never one of them alone (the defect was the table-only list).
func TestChatReplVocabularyCoversBothSources(t *testing.T) {
	have := map[string]bool{}
	for _, w := range chatReplCommandNames() {
		have[w] = true
	}
	for _, w := range sharedCarrierCommandNames() {
		if !have[w] {
			t.Errorf("the REPL vocabulary drops the shared-table word /%s", w)
		}
	}
	for _, w := range chatSessionWords {
		if !have[w] {
			t.Errorf("the REPL vocabulary drops the REPL-native word /%s", w)
		}
	}
	// The reset word is accepted in BOTH spellings by the REPL switch, and the
	// second spelling is the shared table's canonical name.
	if !have[webClearCommandName] || !have[webClearAliasName] {
		t.Errorf("the REPL vocabulary must carry BOTH reset spellings (/%s and /%s): %v",
			webClearCommandName, webClearAliasName, chatReplCommandNames())
	}
	// Sorted and duplicate-free: the refusal reads as one list, and a word
	// repeated is a rendering defect, not harmless.
	words := chatReplCommandNames()
	for i := 1; i < len(words); i++ {
		if words[i-1] >= words[i] {
			t.Fatalf("the vocabulary is not a sorted, duplicate-free list at %d: %v", i, words)
		}
	}
}

// TestHelpPageAnswersTheResetWordLackingItsOwnRow is the defect the E2E run
// found: the help page listed /quit /exit /clear /tools /help, but a word it
// printed elsewhere in a table row — the shared table's reset word /new — was
// answered "unknown command" by chatHelpText. Teaching the help page the word
// (chatReplSlashVocabulary) is what keeps the two vocabularies EQUAL rather than
// "equal except for the words the help page forgot".
func TestHelpPageAnswersTheResetWordLackingItsOwnRow(t *testing.T) {
	usage, err := chatHelpText("/help " + webClearCommandName)
	if err != nil {
		t.Fatalf("the help page calls /%s unknown, but its own table row prints it: %v",
			webClearCommandName, err)
	}
	if !strings.Contains(usage, completionSlashPrefix+webClearCommandName) {
		t.Errorf("/help %s answered with a block that does not document the word:\n%s",
			webClearCommandName, usage)
	}
	if !strings.Contains(usage, completionSlashPrefix+webClearAliasName) {
		t.Errorf("/help %s answered without naming its twin spelling /%s:\n%s",
			webClearCommandName, webClearAliasName, usage)
	}
}

// slashWordsInHelpText extracts the words a reader of the help page learns: the
// slash-prefixed word that STARTS a line (a table row: "  /task list|show …"),
// and the alias spelled inside a row ("(also /exit)").
func slashWordsInHelpText(help string) []string {
	seen := map[string]bool{}
	add := func(word string) {
		if word != "" {
			seen[word] = true
		}
	}
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, completionSlashPrefix); ok {
			add(leadingWord(rest))
		}
		// "(also /exit)" — the second spelling of a row, which is an accepted
		// word and must therefore appear in the refusal too.
		if _, rest, ok := strings.Cut(line, "(also"); ok {
			if _, after, ok := strings.Cut(rest, completionSlashPrefix); ok {
				add(leadingWord(after))
			}
		}
	}
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// leadingWord returns the word a string starts with (letters/digits/hyphen),
// stopping at the first separator — the word a "/word …" token names.
func leadingWord(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			continue
		default:
			return s[:i]
		}
	}
	return s
}

// slashTokensForWords renders words the one way a carrier may show them.
func slashTokensForWords(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, completionSlashPrefix+w)
	}
	sort.Strings(out)
	return out
}
