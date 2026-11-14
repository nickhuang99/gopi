package main

// cli_tool.go — the `tool` GROUP verb: gopi's local tool commands, collected
// under ONE noun (command-hierarchy epic, slice 1/5).
//
// WHY A THIRD LEVEL OF NAMING EXISTS AT ALL. The top-level verb set is grouped
// by CONCEPT DIMENSION (task=planning, session=execution record, memory=
// knowledge, chat=human interaction, tool=local tools) or by HIGH-FREQUENCY
// CLOSED LOOP (worker, merge) — the operator's criterion, recorded in
// ~/.gopi/sessions/aa0d772f-…/artifacts/verb-final-table-v2.md and in
// internal/config/help.go's commandIndex. `act`, `capture`, `proxy`, `shot`
// and friends are neither: each is a one-shot local action with no loop, and
// they were crowding the top level. This file is where they are looked up by
// noun instead.
//
// WHAT THIS FILE IS — AND, MORE IMPORTANTLY, WHAT IT IS NOT.
//
//	It is a PURE NAME→IMPLEMENTATION MAP. The words below each have ONE
//	implementation (their own predicate and run function, still in the tree
//	untouched) and `gopi tool <word> …` hands that same argv tail to it: one
//	implementation, one help page, one exit status. The alternative (a second
//	dispatch body, or a `tool`-flavoured copy of each usage page) would be a
//	second source of truth that drifts the day either side changes — and it is
//	exactly what the operator ruled out: "无非就是把一个命令映射到一个实现" (we
//	add and delete no functionality; this whole change lives in the command
//	line and the help menu).
//
//	WHERE THE SECOND SPELLING WENT (epic verb-retirement, 2026-10-05). These
//	words USED to be reachable at top level as well — the group was additive,
//	"two spellings, one implementation". The operator ruled that out:
//	不要再残留别名，否则后患无穷 — no leftover aliases; the harm outlives the
//	convenience — and 一次性的做完. So the bare word was removed from the
//	dispatch chain and is now REFUSED by name with this group's spelling as its
//	address (cli_verb_aliases.go). This group is therefore no longer "an extra
//	door"; it is the ONLY door, which is what the page below says in its first
//	paragraph.
//
//	It is NOT a gate, a policy, or a new tool (Rule 3: the tool count does not
//	move). It adds no dependency (Rule 35) and spawns nothing: the process the
//	group hands to is the SAME binary, already running.
//
// WHY THE WORDS COME FROM CONSTANTS AND NEVER FROM BARE LITERALS (Rules 1/13/
// 14): a command word is a decision value — it is compared against argv, it is
// printed in help and completion, and internal/config's positional-verb guard
// refuses it after a run flag. Every word in the tables below is therefore
// referenced from the constant that already owns it (the cliVerbs block in
// cli_verbs.go, internal/toolnames for the tool-named spellings, the sandbox
// leaf's own verb constants, and webCompletionNames for the `web` group's two
// action words). A rename then lands in one place and the compiler points at
// every site that must follow.
//
// THE REFUSAL IS THE POINT, NOT A COURTESY. The dispatch chain reads the verb
// from the FIRST argument, and an unrecognized word is not refused there — it
// falls through to the bare-prompt path and buys a REAL agent run (a session
// directory, an `[auto] <word>` ledger row, tokens). An unknown word under this
// group is therefore refused BY NAME, with exit 1, before anything else can
// happen (the shape cli_undispatched_tool_words.go established for `gopi
// code_search`): `gopi tool nosuchword` must cost zero tokens, not one run.

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// toolUsageTitle is the page's FIRST LINE, and it is a contract: the contract
// suite (cli_contracts_test.go) pins it as the `gopi tool -h` fingerprint, so a
// rename of the page fails the live-binary smoke instead of passing silently.
//
// IT SPEAKS THE FAMILY'S FORM (`GoPi <x> subcommands — …`), which `session`,
// `task`, `checkpoint`, `memory` and the family's other pages all state
// (`cmd/agent/cli_session.go:191`, `cli_tasks.go:360`, `cli_checkpoint.go:105`).
// A page that introduced itself differently made a reader wonder whether the
// difference MEANT something; it did not — the group's wording was written
// before the family had a convention. The dimension carried no function, so it
// moved.
const toolUsageTitle = "GoPi tool subcommands — gopi's local tool commands, collected under one noun"

// toolExitUsage is the status of a usage error (a missing word, an unknown
// word). It is the POSIX usage class, the same 1 the other verbs use for
// "you typed it wrong" — and it is deliberately NOT 0: a word this group does
// not know must never be mistakable for a run that succeeded.
const toolExitUsage = 1

// toolRefusalPageClause is the sentence's own name for the address it prints —
// the group's page of the command the reader asked for. It is a named constant
// (Rule 1: a string that carries meaning is not typed inline) because the
// refusal's format and the tests that READ the address back out of the message
// must agree on the same text: a test carrying its own copy of this clause
// would keep passing while the message it claims to parse had changed.
const toolRefusalPageClause = "for the command's own page"

// toolGroupOwnPageWord is the address fragment every word the spelling table
// does NOT claim is sent to: this group's own page. It is a WHOLE command line
// minus the program word (the refusal's format supplies `gopi `), and it is the
// only fragment such a word can be given — see toolRefusalPageWord.
var toolGroupOwnPageWord = CliVerbTool + " " + HelpFlag

// toolRefusalFmt is the unknown-word refusal. It names the word, the whole
// vocabulary this group accepts (so the reader does not have to guess), and the
// page of the command the reader meant. Args: (word, vocabulary, page).
//
// THE PAGE ADDRESS IS RESOLVED, NOT ECHOED (spelling-table epic, 2026-10-07).
// The last slot used to be the raw word, so a TOOL WIRE name was corrected into
// a second refusal — measured on the built binary: `gopi tool code_search -h`
// said "or `gopi code_search` for the command's own page", and
// `gopi code_search` exits 1 with "this one is spelled gopi tool code-search".
// A refusal whose hint lands on another refusal is a dead end (Rule 15).
//
// AND IT MUST BE A PAGE, NOT A PROMPT — the half the table cannot see.
// toolRefusalPageWord answers with the live door of the spelling that word
// belongs to (`tool code-search`, `ui_state`); a word NO row claims has no page
// of its own at all, and `gopi <word>` is not one: measured on the built binary
// 2026-10-07 from this very review, `gopi tool nosuchword -h` printed
// "or `gopi nosuchword` for the command's own page" and running it EXITED 0 AND
// MINTED A SESSION — the bare-prompt path, one real agent run, recommended by a
// typo. Such a word is therefore sent to this group's own page, which opens and
// costs nothing.
const toolRefusalFmt = "gopi %s: unknown command %q — this group knows: %s\n" +
	"Run `gopi %s -h` for what each one does (or `gopi %s` " + toolRefusalPageClause + ").\n"

// toolLeafCommand is one DIRECT command of the group: a word that IS a command
// (`gopi tool <word> [args…]`), with no second level of its own.
type toolLeafCommand struct {
	word string // the word after `gopi tool` — ALWAYS a constant (Rules 1/13/14)
	form string // the argument shape, as advertised (help text, not a decision)
	desc string
	// internal marks a command whose output is a contract for this repo's own
	// gates and scripts rather than a stable surface for a person. The operator
	// kept them exposed on purpose — "一个功能你不暴露，那么你就只能内部写
	// unittest" (a capability with no door can only be tested by unit tests) —
	// so they are listed, and the page carries a warning block that says so.
	internal bool
}

// toolLeafCommands is the DIRECT half of the page, in the order it renders.
// Every word is a dispatched top-level verb; see toolSubcommandPredicates for
// the implementation each one forwards to.
var toolLeafCommands = []toolLeafCommand{
	{
		word: CliVerbCodeSearch,
		form: "<symbol>",
		desc: "Where a symbol is DEFINED (ctags index); the code_search tool.",
	},
	{
		word: toolnames.ToolKeywordSearch,
		form: "<query>",
		desc: "Full-text search over the repo (ripgrep); the keyword_search tool.",
	},
	{
		word: toolnames.ToolVoiceInput,
		form: "<audio> [--engine doubao|local] [--server|--client]",
		desc: "Speech to text (doubao cloud by default, `--engine local` on-machine); `--server` serves HTTP and `--client` records a mic.",
	},
	{
		word: toolnames.ToolScreenCapture,
		form: "[--display D] [--out PATH]",
		desc: "Capture the LIVE X11 desktop to a PNG (the whole-screen spelling).",
	},
	{
		word: CliVerbCapture,
		form: "--screen | --window | --area WxH[+X+Y]",
		desc: "The same capture engine, mode-aware (screen / window / rectangle).",
	},
	{
		word: CliVerbAct,
		form: "<move|click|type|key|…> [args…]",
		desc: "Drive the LIVE X11 desktop and return JSON evidence of what happened.",
	},
	{
		word: CliVerbAnnotate,
		form: "--in PNG --out PNG <mark>…",
		desc: "Draw marks (rect, point, arrow, label) onto an EXISTING PNG.",
	},
	{
		word: CliVerbGif,
		form: "--frames DIR --out GIF",
		desc: "Turn a PNG frame sequence into ONE animated GIF, each frame captioned.",
	},
	{
		word: CliVerbShot,
		form: "--url URL | --self",
		desc: "Screenshot a URL (headless Chromium) or the local chat UI.",
	},
	{
		word: CliVerbProxy,
		form: "--upstream openai|anthropic …",
		desc: "Local protocol proxy: an Anthropic-speaking client, an OpenAI peer.",
	},
	{
		word: toolnames.ToolVisionAnalyze,
		form: "<image> [--prompt TEXT]",
		desc: "Image understanding / OCR through the vision model (LLM-backed).",
	},
	{
		word: CliVerbText2imgVerify,
		form: "[size-px]",
		desc: "Render the `知` glyph at N px and write z_<N>px.png — the pixel-level (IoU) render check.",
	},
	{
		word: CliVerbTokens,
		form: "--file <path> | --session <id> | …",
		desc: "Count tokens: a text file, a session's verification rows, or a request against its server number.",
	},
	{
		word:     CliVerbLedgerWindow,
		form:     "<from> [to]",
		desc:     "Two wall-clock bounds in, their epoch seconds out — for shell scripts.",
		internal: true,
	},
	{
		word:     CliVerbScratchGC,
		form:     "[--dry-run] [--explain] [trees|sandboxes|deps|…]",
		desc:     "The one-shot scratch sweep the host watchdog executes between ticks.",
		internal: true,
	},
}

// toolGroupCommand is one GROUPED command: a word that keeps its own second
// level (`gopi tool <word> <action> …`). It is a group because the command
// itself has more than one action — the epic's hierarchy rule: top level is a
// dimension, the second word an action, and a third level is allowed exactly
// when that action has actions of its own.
type toolGroupCommand struct {
	word  string
	forms []string // the words the second level accepts, as advertised
	desc  string
}

// toolGroupCommands is the GROUPED half of the page. `sandbox` and `text2img`
// are themselves dispatched top-level verbs whose own argv already carries the
// second word; `web` is the group whose TOOL face (search|fetch) lives here,
// while its bare form stays the browser chat carrier the top-level help
// describes (devdoc/cli-naming-convention.md; artifacts/verb-final-table-v2.md
// §三, where the operator moved the hidden `web search`/`web fetch` entry points
// under `tool`).
var toolGroupCommands = []toolGroupCommand{
	{
		word:  CliVerbSandbox,
		forms: []string{SandboxCreateVerb, SandboxRunVerb, SandboxShowVerb},
		desc:  "Create, run inside, or describe a private gopi tool sandbox profile.",
	},
	{
		word:  toolnames.ToolText2img,
		forms: []string{toolText2imgRenderForm},
		desc:  "Render a text file to PNG(s) with the pure-Go TrueType renderer.",
	}, {
		word:  CliVerbWeb,
		forms: webCompletionNames,
		desc:  "The web TOOLS from the shell (the bare word is the chat UI carrier).",
	},
}

// toolText2imgRenderForm is the text2img leaf's own invocation form, rendered
// where a second-level action word would go: text2img takes the INPUT PATH as
// its first argument (it has no `render` action word — see the report's honest
// note), so the table must show the real shape rather than invent a word.
const toolText2imgRenderForm = "<input.txt> [output.png] [flags]"

// toolGroupSecondDoorWord is the ONE word this group forwards to that this
// binary ALSO dispatches at top level — the single declared exception to the
// one-door rule stated at the head of this file, and the only word for which
// `TestToolGroupWordsAreTheOnlyDoor` does not demand a top-level refusal.
//
// A second door is otherwise a defect: it makes one of the two spellings
// unreachable (a refusal shadows the command, or the command shadows the
// refusal). Here it is neither, and the measurements are the reason to keep it
// rather than a reason to hide it:
//
//	the two spellings reach the SAME predicate — `toolSubcommandPredicates[
//	"web"]` IS isWebSubcommand, which is what main.go's chain calls, so
//	`gopi web -h` and `gopi tool web -h` print ONE page (byte-identical,
//	verified by TestToolGroupDeclaredSecondDoorIsPrintedAndPinned);
//	the group spelling has a SECOND LEVEL the top-level word does not
//	advertise on its own page (`web search` / `web fetch`), which is why the
//	group entry exists at all;
//	and the top-level word is not a spelling of those tools — it is the
//	CARRIER of the chat UI (the browser server), a different thing that
//	happens to share the noun. Retiring the carrier would move a live server
//	behind a group word, which is a live-surface decision, not a rename.
//
// So the exception is DECLARED (this constant), PRINTED (the group's page
// carries toolGroupSecondDoorReason), and PINNED (the test above + the
// forwarder's own probe) — never a silent asymmetry a reader has to
// reverse-engineer from two contradicting leads.
const toolGroupSecondDoorWord = CliVerbWeb

// toolGroupSecondDoorReason is that exception's reason, in ONE string: the page
// prints it, the tests assert it, and neither side types a second copy of it
// (Rule 13/14). It is written for the reader of `gopi tool -h`, who asked why
// this one row behaves differently from every other row on the page.
const toolGroupSecondDoorReason = "`gopi " + CliVerbWeb + "` is the browser chat UI's own carrier AND this group's door to the web TOOLS " +
	"(`" + webGroupSubcmdSearch + "`/`" + webGroupSubcmdFetch + "`). Both spellings reach the SAME predicate (`isWebSubcommand`), so neither can shadow the " +
	"other and no spelling of these tools is ever spent as a prompt; the grouping is what puts the two tool faces on this page, and the top-level word " +
	"is what starts the server. The one-door rule at the head of this file is stated for a word with ONE meaning; `" + CliVerbWeb + "` has two, and the " +
	"page you are reading is the reason it is written down instead of hidden (devdoc/cli-naming-convention.md)."

// toolGroupWordIsADeclaredSecondDoor reports whether a group word is the
// declared exception above. Both the group's gates and its page read THIS — a
// second exception added to the table cannot slip past a test that asks the
// question in one place (Rule 13/14).
func toolGroupWordIsADeclaredSecondDoor(word string) bool { return word == toolGroupSecondDoorWord }

// toolGroupSecondDoorNote is the paragraph the group's page carries about the
// exception, derived from the constant that owns it. It returns "" when no
// group row is the declared second door, so a table with the exception removed
// prints a page with no stale paragraph about a word that is no longer special.
func toolGroupSecondDoorNote() string {
	for _, g := range toolGroupCommands {
		if toolGroupWordIsADeclaredSecondDoor(g.word) {
			return "ONE DECLARED EXCEPTION to the one-door rule above: " + toolGroupSecondDoorReason + "\n"
		}
	}
	return ""
}

// toolSubcommandPredicates maps every word this group accepts — direct and
// grouped alike — to the predicate that OWNS that command's implementation.
// The group hands the SAME argv tail to the SAME predicate the top-level chain
// calls: `gopi tool sandbox create -h` and `gopi tool sandbox create -h` reach one
// function, which is why they print one page and exit with one status.
//
// A word that is absent from this map is refused by name (never spent as a
// prompt, never silently ignored).
var toolSubcommandPredicates = map[string]func([]string) bool{
	CliVerbCodeSearch:           isCodeSearchSubcommand,
	toolnames.ToolKeywordSearch: isKeywordSearchSubcommand,
	toolnames.ToolScreenCapture: isScreenCaptureSubcommand,
	CliVerbCapture:              isCaptureSubcommand,
	CliVerbAct:                  isActSubcommand,
	CliVerbAnnotate:             isAnnotateSubcommand,
	CliVerbGif:                  isGifSubcommand,
	CliVerbShot:                 isShotSubcommand,
	CliVerbProxy:                isProxySubcommand,
	toolnames.ToolVisionAnalyze: isVisionSubcommand,
	CliVerbText2imgVerify:       isText2imgVerifySubcommand,
	CliVerbLedgerWindow:         isLedgerWindowSubcommand,
	CliVerbTokens:               isTokensSubcommand,
	CliVerbScratchGC:            isScratchGCSubcommand,
	CliVerbSandbox:              isSandboxSubcommand,
	toolnames.ToolText2img:      isText2imgSubcommand,
	toolnames.ToolVoiceInput:    isVoiceSubcommand,
	CliVerbWeb:                  isWebSubcommand,
}

// toolUsage is the group's `-h` page. It is built from the tables above (never
// hand-typed twice) and split into the two blocks the epic asks for — the
// direct commands, which can be typed as-is, and the grouped ones, which need a
// second word — plus the warning block for the internal entries.
//
// TWO DIMENSIONS OF IT ARE THE FAMILY'S, and they were adopted because they
// carry no function: the TITLE FORM above, and the BLANK LINE before `Usage:`
// that every family page has (`sessionUsage`, `tasksUsage`, `checkpointUsage`
// all put one there). The page's COLUMN-ALIGNED command block is deliberately
// KEPT, and that asymmetry is the reason to state it: the family's pages print
// SYNOPSES — one command per line, wrapped onto indented continuation lines,
// because each synopsis is a sentence an operator types. This page prints an
// INDEX of 16 direct commands and 3 groups whose whole purpose is that a reader
// scans DOWN one column of descriptions; collapsing it to the family's
// indented-continuation shape would make the last description of a long entry
// land 60+ columns in, which is exactly what the widening `%-*s` below exists
// to prevent. So the LAYOUT here is a function-bearing decision and stays; the
// title and the blank line carried nothing and moved.
var toolUsage = toolUsageTitle + `

Usage:
  gopi tool -h                     this page
  gopi tool <command> [args…]      run a DIRECT command (first block)
  gopi tool <group> <action> [args…]   run a GROUPED command (second block)

Every command below is reached ONLY as ` + "`gopi tool <command>`" + `: the local tools
have ONE door, and the tool's own page is one hop further (` + "`gopi tool <command> -h`" + `).
The bare word (` + "`gopi tool capture`" + `, ` + "`gopi tool shot`" + `, …) is RETIRED: it refuses with the
spelling to use instead, so a typo there never becomes a model run.

Direct commands — gopi tool <command> IS the command:
` + renderToolLeafRows() + `
Grouped commands — the command keeps its own second level:
` + renderToolGroupRows() + `
internal — gates and scripts, not a stable surface for a person:
  ` + strings.Join(toolInternalWords(), ", ") + `
  These work, and they are exposed on purpose (a capability with no door can
  only be tested by unit tests). Their output is a contract for this repo's own
  gates, so a script may depend on it — a human should not.

Each entry's flags and examples are on its own page (gopi tool <command> -h);
why the page is this shape, and the background it moved off the page:
devdoc/help-page-layering-small-pages.md.
` + toolGroupSecondDoorNote()

// renderToolLeafRows renders the DIRECT block: word, argument shape, one-line
// description, aligned on the block's own widest entry.
func renderToolLeafRows() string {
	var b strings.Builder
	width := 0
	for _, c := range toolLeafCommands {
		if w := len(c.word) + 1 + len(c.form); w > width {
			width = w
		}
	}
	for _, c := range toolLeafCommands {
		name := c.word + " " + c.form
		mark := ""
		if c.internal {
			mark = " (internal)"
		}
		fmt.Fprintf(&b, "  %-*s  %s%s\n", width, name, c.desc, mark)
	}
	return b.String()
}

// renderToolGroupRows renders the GROUPED block: word, the second-level words,
// one-line description.
func renderToolGroupRows() string {
	forms := make([]string, len(toolGroupCommands))
	width := 0
	for i, g := range toolGroupCommands {
		forms[i] = toolGroupForm(g)
		if w := len(forms[i]); w > width {
			width = w
		}
	}
	var b strings.Builder
	for i, g := range toolGroupCommands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, forms[i], g.desc)
	}
	return b.String()
}

// toolGroupForm renders one grouped command's second level. A group whose
// entries are all single ACTION WORDS renders as the `<a|b|c>` alternation the
// group's own argv accepts; a group whose entry is itself an argument shape
// (text2img takes the input FILE as its first argument — it has no `render`
// action word) renders that shape verbatim, so the page shows the command the
// binary really runs rather than a word nobody dispatches.
func toolGroupForm(g toolGroupCommand) string {
	for _, f := range g.forms {
		if strings.ContainsAny(f, " <>[]|") {
			return g.word + " " + strings.Join(g.forms, " | ")
		}
	}
	return g.word + " <" + strings.Join(g.forms, "|") + ">"
}

// toolInternalWords names the internal entries of the DIRECT block, for the
// warning block — derived from the table, never a second list.
func toolInternalWords() []string {
	out := make([]string, 0, len(toolLeafCommands))
	for _, c := range toolLeafCommands {
		if c.internal {
			out = append(out, c.word)
		}
	}
	return out
}

// toolSubcommandWords is the group's whole vocabulary, sorted: the refusal and
// the completion candidate set both read it, so an entry added to either table
// reaches both surfaces by construction (Rule 13/14 — one definition).
func toolSubcommandWords() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(toolSubcommandPredicates))
	for w := range toolSubcommandPredicates {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

// toolCompletionNames is the `gopi tool <TAB>` candidate set — the SAME
// vocabulary the dispatcher accepts (it IS toolSubcommandWords), so tab
// completion can never offer a word this group would refuse.
var toolCompletionNames = toolSubcommandWords()

// isToolSubcommand reports whether argv begins with the tool group verb. When
// it does it runs the group and returns true (the caller must not treat the
// remaining args as an input file).
func isToolSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbTool {
		return false
	}
	runToolSubcommand(args[1:])
	return true
}

// runToolSubcommand dispatches the group's second level. It returns nothing:
// the dispatch chain expects the verb to handle its own exit.
//
// The forwarding shape is the whole trick, and it is deliberately literal: the
// tail is handed to the leaf predicate with the leaf's OWN word put back in
// front, so the leaf sees exactly the argv it sees when invoked at top level
// (`gopi tool sandbox create -h` ⇒ isSandboxSubcommand({"sandbox","create","-h"})).
// Nothing is re-parsed here, so nothing can diverge from the top-level path.
func runToolSubcommand(args []string) {
	if len(args) == 0 {
		// A bare `gopi tool` is a usage error, not a run: print the page and
		// exit non-zero so a script can tell "you typed it wrong".
		fmt.Fprint(os.Stderr, toolUsage)
		os.Exit(toolExitUsage)
	}
	word := args[0]
	switch word {
	case HelpFlag, HelpFlagLong, HelpWord:
		fmt.Print(toolUsage)
		return
	}
	pred, ok := toolSubcommandPredicates[word]
	if !ok {
		// UNKNOWN WORD — refused BY NAME, before the run path can see it. This
		// is the difference between a typo and an agent run (see the file head).
		//
		// THE PAGE ADDRESS IS RESOLVED, NOT ECHOED (spelling-table epic,
		// 2026-10-07). The last slot used to be the raw word, so a TOOL WIRE
		// name was corrected into a second refusal — measured on the built
		// binary: `gopi tool code_search -h` said "or `gopi code_search` for the
		// command's own page", and `gopi code_search` exits 1 with "this one is
		// spelled gopi tool code-search". A refusal whose hint lands on another
		// refusal is a dead end (Rule 15). toolRefusalPageWord answers with the
		// live door of the spelling that word belongs to (`tool code-search`,
		// `ui_state`), and for a word NO row claims — where no page of its own
		// exists, and `gopi <word>` would be the bare-prompt path rather than a
		// page — it falls back to this group's own page (measured 2026-10-07:
		// the echoed spelling exited 0 and minted a session).
		fmt.Fprintf(os.Stderr, toolRefusalFmt, CliVerbTool, word,
			strings.Join(toolSubcommandWords(), "|"), CliVerbTool, toolRefusalPageWord(word))
		os.Exit(toolExitUsage)
	}
	// A BARE address — `gopi tool <word>` with NOTHING after it — is only a
	// usage error for a word whose own command line needs more input: a GROUPED
	// word (`sandbox`, `web`, `text2img`) with no action, whose page is one hop
	// further. The two shapes must not be conflated (bug task 01a113ac…,
	// measured 2026-10-07):
	//
	//	`gopi tool proxy` is COMPLETE — `proxy` is a leaf whose arguments are
	//	all FLAGS, so "no tail" is a valid command line and the flag parser owns
	//	the refusal (`--upstream is required`, exit 1). Refusing it here made the
	//	address the group PRINTS (`gopi tool proxy`, in its own usage block)
	//	unrunnable.
	//	`gopi tool web` is the CARRIER — its bare form is a real command of its
	//	own (`gopi web` serves the chat UI), so it is handed the tail as-is.
	//	`gopi tool capture` (a leaf with REQUIRED arguments) and `gopi tool
	//	sandbox` (a grouped word) both need input, and both get the usage error.
	//
	// WHICH SHAPE A WORD IS comes from the group's OWN tables (Rule 13/14),
	// never a typed list: toolGroupCommands is the grouped half of the page, and
	// a word that is not in it is a leaf — the shape the page itself renders.
	if len(args) == 1 {
		if toolWordIsABareCarrier(word) {
			pred([]string{word})
			return
		}
		if !toolWordNeedsASecondWord(word) {
			// A LEAF: hand the leaf's own argv (its word, no tail) to its
			// predicate and let it decide — it either runs (a leaf with no
			// required input) or refuses by name with its own message.
			pred([]string{word})
			return
		}
		// A GROUPED word with no action: print the group's page and exit
		// non-zero, exactly like a missing word, so a script can never read
		// "I forgot the action" as a successful run.
		//
		// THE PAGE ADDRESS IS RESOLVED HERE TOO, and that is not a second
		// mechanism: this arm has no spelling row either — a grouped word IS in
		// the group's vocabulary, so the echo would name the very word that just
		// failed (measured 2026-10-07: `gopi tool sandbox` offered
		// "or `gopi sandbox` for the command's own page", and `gopi sandbox`
		// exits 1 with the same refusal, one refusal pointing at itself, forever
		// — the dead end the arm above resolves away).
		fmt.Fprintf(os.Stderr, toolRefusalFmt, CliVerbTool, word,
			strings.Join(toolSubcommandWords(), "|"), CliVerbTool, toolGroupOwnPageWord)
		os.Exit(toolExitUsage)
	}
	// The leaf predicate consumes the rest of argv and handles its own exit.
	pred(append([]string{word}, args[1:]...))
}

// toolWordNeedsASecondWord reports whether the word's own command line requires
// another word after it — i.e. it is one of the GROUPED entries on the group's
// page (toolGroupCommands), whose second level (`sandbox create|run|show`,
// `web search|fetch`, `text2img <input.txt>`) is where the command actually is.
// Every other registered word is a LEAF: its arguments are flags, so a bare
// address is a complete command line.
func toolWordNeedsASecondWord(word string) bool {
	for _, g := range toolGroupCommands {
		if g.word == word {
			return true
		}
	}
	return false
}

// toolWordIsABareCarrier reports whether `gopi tool <word>` — the word ALONE,
// no arguments — is a command in its own right rather than a usage error.
//
// It is a NAME test against the group's own vocabulary, and that is deliberate
// (Rule 13/14): the alternative — asking the word's own predicate — cannot be
// done, because several of those predicates HAVE SIDE EFFECTS and exit the
// process (`isWebSubcommand` starts the chat-UI server when handed
// `["web"]`; measured 2026-10-07, a test that called it in-process took the
// whole test binary down on the missing model credential). The name list is
// the group's OWN (toolSubcommandPredicates), never a second hand-typed copy,
// so the set that can be dispatched and the set that may be bare cannot drift:
//
//	word must be a REGISTERED group entry (else runToolSubcommand already
//	refused it by name), AND its keyword must be a CARRIER_KEYWORD — the
//	one-word form `gopi <word>` that this binary really serves as a command.
//
// Today `web` is the only carrier (`gopi web` is the chat-UI server, its own
// door in main()'s chain). A second carrier added later belongs in this list
// WITH its own evidence, and the test that pins reachability is
// TestToolGroupForwardsToTheSameImplementation, which runs `gopi <word> -h`
// on the real binary and requires a page.
var toolBareCarrierWords = map[string]bool{
	CliVerbWeb: true,
}

func toolWordIsABareCarrier(word string) bool {
	if _, registered := toolSubcommandPredicates[word]; !registered {
		return false
	}
	return toolBareCarrierWords[word]
}
