package tools

// chat_shell_tokens.go — the EXPORTED spelling of the chat surface's ONE shell
// tokenizer.
//
// WHY A NEW FILE RATHER THAN AN EDIT TO chat_write_guard.go. The tokenizer's
// body lives there, beside the guard that first needed it, and that file must
// not change: the rule layer (internal/chatpolicy) is being built in parallel
// and the guard's behaviour has to stay byte-for-byte what it is until the
// wiring item lands. An exported delegate in its own file gives the rule layer
// (and any other consumer) the ONE tokenizer without touching a line the guard
// depends on — and without a second implementation, which is the defect this
// delegate exists to prevent: two tokenizers eventually disagree about where
// one command ends, and a boundary that tokenises differently from the engine
// judging it has a hole in it.
//
// internal/chatpolicy deliberately holds NO tokenizer of its own: its Decide
// takes the []string this function returns. The single definition stays here,
// where it already was.

// ChatShellTokens splits a chat command line into words: on shell whitespace
// and on the UNQUOTED clause separators (chatGitClauseSeparators), emitting
// chatClauseBreakWord for each separator it consumed. Quoting is tracked by
// STATE, so an apostrophe inside double quotes is literal text.
//
// The words it returns are the input internal/chatpolicy's rule table matches
// against, and they are also what chatShellClauses splits when a decision has
// to be taken per clause — one tokenizer, one sentinel, one splitter.
func ChatShellTokens(command string) []string {
	return chatShellTokens(command)
}
