package main

// cli_verb_aliases.go — the SECOND half of the refusal vocabulary: the known
// SYNONYMS of a real command.
//
// WHY IT EXISTS (task 01a0f451…, 2026-09-30). cli_undispatched_tool_words.go
// refuses a word that names a real command under a DIFFERENT SPELLING of the
// same word (the tool wire name `code_search` for the dash-cased
// `code-search`; the dash twin of an underscore command). That covers
// spelling, not MEANING: `gopi buildinfo` is nobody's misspelling — it is a
// natural way to say `gopi version`, and the dispatch chain does not know it,
// so the word fell through to the bare-prompt path and bought a whole agent
// session. Measured 2026-09-30: three consecutive `gopi buildinfo` invocations
// became three `reason=task failed` ledger tombstones with zero output — the
// operator asked for a version string and paid for three model runs.
//
// WHERE IT COMES FROM (Rule 13/14 — one definition, referenced). A spellable
// variant can be DERIVED from its canonical word (dashTwin). A SYNONYM cannot:
// no rule turns `version` into `buildinfo`. So a synonym is its OWN fact and it
// gets its own authoritative table HERE, one row per (alias, canonical) pair —
// and the REFUSAL VOCABULARY is read out of that table rather than typed beside
// it. Adding a synonym is one row here; nothing else in the refusal machinery
// changes, and every row is mechanically checked: the alias is refused on the
// real binary, its canonical word really dispatches, and the alias itself does
// not (cli_verb_aliases_test.go — the red-then-green gate).
//
// WHAT IS *NOT* IN IT: nothing that merely "sounds like a command". The
// negative control is the whole point (a refusal is a statement about THIS
// BINARY's own surface, never a guess about what the operator meant), so
// `gopi hi` and `gopi frobnicate9` must keep reaching the run path — asserted
// by a test that probes the real binary, not by a comment.
//
// WHERE IT RUNS: the same two places as the spelling vocabulary, because it is
// the same lookup — the dispatch chain's argv[0] predicate
// (isUndispatchedToolWordSubcommand) and internal/config's positional-verb
// guard, which catches the word left over AFTER a run flag
// (`gopi --timeout 3 buildinfo`), a position the dispatch chain never reads.
// Both refuse before any cost: no session directory, no ledger row, no model
// request.

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// verbAlias is ONE retired synonym of a command: the word somebody types, the
// command line that actually does the work, and why the two are the same
// request (or, for a word with NO live home, why it is refused at all).
//
// TWO FIELDS, TWO KINDS OF ROW — and the vocabulary is now mostly the second.
//
//	(a) HOMED — `canonical` is set: the word is a synonym of a command this
//	    binary dispatches, and the refusal sends the reader there.
//	(b) HOMELESS — `canonical` is EMPTY and `reason` carries the whole
//	    explanation: the capability is gone (retired with no successor), so
//	    there is no address to print. The refusal must still fire — the word
//	    must never reach the run path — but a hint pointing at nothing would be
//	    a dead end (Rule 15), so the message says so instead.
//
// THE ADDRESS IS A COMMAND LINE, NOT A WORD. `canonical` holds the FULL
// spelling the reader should type — `version` for the shipped `buildinfo` row,
// `session log` / `tool capture` for the retired flat verbs — and the refusal
// renders it VERBATIM after the program word. That is what makes a two-level
// home expressible in this table at all (the operator's ruling, 2026-10-05:
// "对" to extending the rendering logic to multi-level commands): a field that
// could only hold one word would have forced either a second table or a
// refusal that prints `gopi session log` as two independent words — read by an
// operator as `gopi session` plus a stray argument.
//
// So the two spellings of one row are the SAME string by construction: the
// address is `gopiProgramWord + " " + canonical`, derived once, and no site
// types it twice.
type verbAlias struct {
	alias string // the retired word a user is likely to type
	// canonical is the FULL command line (without the program word) that
	// dispatches the work — one word (`version`) or several (`session log`,
	// `tool web search`). EMPTY when the word has no live home at all.
	canonical string
	// retired marks a row whose word USED TO dispatch as a top-level command
	// and no longer does (the verb-retirement epic, 2026-10-05). It is a fact
	// about history that nothing in the tree can derive — a synonym and a
	// retired verb can look identical from here (both are words this binary
	// does not dispatch) — so it is carried explicitly, and the refusal's
	// wording is the ONE place it is read: a retired verb's message says the
	// door MOVED, a synonym's says the word was never a door.
	retired bool
	reason  string // why this word means that command, or why it is gone
}

// The alias WORDS (Rule 1: a word that carries meaning is a named constant,
// never a bare literal at a decision site). Where the word already HAS a home
// constant — a cliVerbs word, a toolnames wire name, a taskverbs verb — the
// table below references THAT constant and no new one is minted here; the
// block below is only for the words this file is the first to name.
const (
	// verbAliasBuildInfo is the `gopi version` synonym an operator reaches for
	// when the question is "what is this binary?" — `buildinfo` is the word
	// the Go toolchain itself uses for the metadata `gopi version` prints, and
	// it is the word that cost three agent runs on 2026-09-30.
	verbAliasBuildInfo = "buildinfo"
	// verbAliasMerge is the `gopi merge` word, retired by the merge-removal
	// epic (item 2). It is NOT a cliVerbs constant any more: the constant went
	// with the dispatch, and this row is what keeps the word refused BY NAME
	// instead of falling through to the bare-prompt path (the measured
	// 2026-09-30 failure mode: a command the operator meant becomes a model
	// call and a ledger row).
	verbAliasMerge = "merge"
)

// toolHome is the address of a verb that moved under the `tool` group
// (`tool capture`): the group word plus the leaf word, assembled in ONE place
// because nineteen rows share it (Rule 13/14 — the address is derived, never
// typed per row).
func toolHome(word string) string { return CliVerbTool + " " + word }

// groupHome is the same idea for the other families (`session log`,
// `task archive`, `memory brew`, `config setup`): the group word plus the
// second level that carries the verb.
func groupHome(group, sub string) string { return group + " " + sub }

// verbAliases is the authoritative refusal table — the ONE definition of
// "these words do not dispatch here, and this is what the reader should type
// instead" (Rule 13/14). The refusal vocabulary is DERIVED from it:
// undispatchedToolWordRefusalReason walks it, and verbAliasWords projects its
// spellings for the tests and for internal/config's positional-verb guard, so
// the words exist in exactly one place.
//
// A ROW IS A PROMISE, and every row is driven on the real binary by
// cli_verb_aliases_test.go: the word is refused with exit 1, the address it
// prints really RUNS, and the word itself is not a command (a word that IS
// dispatched would make the command unreachable). What a row may NOT be is a
// guess about intent: `build` is deliberately absent even though `gopi build`
// reads like a build query, because a word this binary never dispatched is
// indistinguishable from ordinary prompt text — and the negative control
// (`gopi frobnicate9` still runs) is what keeps the refusal honest.
//
// WHY THE TABLE GREW FROM ONE ROW TO THIRTY (epic verb-retirement, 2026-10-05).
// The command-hierarchy work first GROUPED the local tools under `tool` and the
// session readers under `session` while keeping every old spelling dispatched —
// "one implementation, two spellings". The operator ruled that arrangement out:
// 不要再残留别名，否则后患无穷 (no leftover aliases; the harm outlives the
// convenience), and 一次性的做完 (do it all at once). So each word below was
// REMOVED from the dispatch chain and given a row here instead: the capability
// is untouched (its predicate, its run function and its help page are all
// still there — only the ENTRY POINT moved), and the old spelling now refuses
// at zero cost instead of minting a session.
//
// THE HARD CONSTRAINT THIS TABLE EXISTS FOR: a verb removed from the dispatch
// chain and NOT listed here falls through to the bare-prompt path — one real
// model call, a minted session and an `[auto] <word>` ledger row for what the
// operator meant as a command. Measured 2026-09-30: three `gopi buildinfo`
// invocations became three failed tasks. That is why the two halves (摘除 the
// dispatch entry, 接入 this table) ship in ONE change, word by word.
//
// RETIRED means the word used to dispatch at top level; for those rows the
// refusal says so and points at the family that absorbed the verb. The
// address is a WHOLE COMMAND LINE (`session log`, `tool capture`,
// `tool web search`) — see the verbAlias doc for why that matters.
var verbAliases = []verbAlias{
	// ---- class (c): a word that never dispatched, but means a command -------
	{
		alias:     verbAliasBuildInfo,
		canonical: CliVerbVersion,
		reason:    "`buildinfo` IS the Go toolchain's own word for the build metadata `gopi version` prints — the request is the version command, spelled the way the toolchain names it",
	},

	// ---- A group → `session` (the session family absorbed the readers) ------
	{
		alias:     CliVerbLog,
		canonical: groupHome(CliVerbSession, CliVerbLog),
		retired:   true,
		reason:    "the JSONL session log reader; `gopi session log` is the same command with the family's other readers beside it in `gopi session -h`",
	},
	{
		alias:     CliVerbStats,
		canonical: groupHome(CliVerbSession, CliVerbStats),
		retired:   true,
		reason:    "session numbers (exchanges, tokens, cost); `gopi session stats` is the same command under the session family",
	},
	{
		alias:     CliVerbRaw,
		canonical: groupHome(CliVerbSession, CliVerbRaw),
		retired:   true,
		reason:    "the verbatim raw HTTP log; `gopi session raw` is the same command under the session family",
	},
	{
		alias:     CliVerbResume,
		canonical: groupHome(CliVerbSession, CliVerbResume),
		retired:   true,
		reason:    "continue an interrupted session; `gopi session resume` is the same command under the session family",
	},
	{
		alias:     CliVerbCheckpoint,
		canonical: groupHome(CliVerbSession, CliVerbCheckpoint),
		retired:   true,
		reason:    "the raw-log snapshot registry; `gopi session checkpoint list|export` is the same command under the session family",
	},

	// ---- B group → `task` ---------------------------------------------------
	{
		alias:     CliVerbArchive,
		canonical: groupHome(CliVerbTask, taskverbs.Archive),
		retired:   true,
		reason:    "archive a branch WITH a recorded reason; `gopi task archive` is the same command (and the same --check/--reclaim/--verify-commit modes) under the task ledger",
	},

	// ---- C group → `memory` -------------------------------------------------
	{
		// The git-PR patch channel is DELETED (2026-11-05, the cross-node sync
		// removal). The row stays so a script, a note or a shell history that
		// types the word hears where its one surviving FUNCTION went, instead
		// of falling through to the bare-prompt path and buying a real agent
		// run (see the note above this table: an unrecognized word is not
		// refused anywhere else, it is SPENT). The twin-row audit was the only
		// memsync action with a local, non-cross-node meaning, and it lives on
		// as the ledger verb — so the pointer names that, by name.
		alias: CliVerbMemsync,
		// groupHome, NOT taskDedupScanProg: the canonical field is a
		// SUBCOMMAND path that programAddress prefixes with the program word,
		// and taskDedupScanProg already carries `gopi ` in front — so the
		// refusal rendered "use `gopi gopi task dedup-scan`" (measured on the
		// real binary 2026-11-06, `gopi memsync --export`). The address a
		// reader copies has to be runnable, which is this file's own rule.
		canonical: groupHome(CliVerbTask, taskverbs.DedupScan),
		retired:   true,
		reason:    "the git-PR patch channel (export/import a JSON patch) is DELETED — it moved rows between nodes, and this binary no longer runs a cross-node sync. The one local audit it carried is a ledger verb now: `gopi task dedup-scan`",
	},
	{
		alias:     CliVerbBrew,
		canonical: groupHome(CliVerbMemory, CliVerbBrew),
		retired:   true,
		reason:    "distill reusable lessons from session logs; `gopi memory brew` is the same command under the memory family",
	},

	// ---- D group → `config` -------------------------------------------------
	{
		alias:     CliVerbSetup,
		canonical: groupHome(CliVerbConfig, CliVerbSetup),
		retired:   true,
		reason:    "provision a host to a run-ready state; `gopi config setup` is the same command, and provisioning is a configuration act",
	},
	{
		alias:     CliVerbCompletion,
		canonical: groupHome(CliVerbConfig, CliVerbCompletion),
		retired:   true,
		reason:    "emit a shell tab-completion script; `gopi config completion` is the same command — it writes a shell artifact, which is configuration",
	},

	// ---- F group: `ui` / `cli` are NOT rows here (union resolution) ----------
	//
	// This branch's own commit retired them as HOMELESS (no successor, the door
	// gone) and they were the only rows with an EMPTY address. The union with
	// master settles the other way, and the LATER ruling wins: master's
	// `cc8289b0` (2026-10-06 04:50, task 01a10dba…) had already ruled that `ui`
	// and `cli` are 暂不暴露 — "the code stays, the commands still run, and
	// nothing advertises them" — because the regression it was fixing is REAL
	// and this branch's commit predates it: `gopi ui` and `gopi cli` ARE
	// dispatched predicates in master's `cmd/agent/main.go`
	// (isUISubcommand / isTermSubcommand, cli_ui.go / cli_term.go), wired into
	// the pre-run chain, with contract tests driving them on the binary. So the
	// two words are a PUBLISHING question on master, not a retirement, and a
	// row here would contradict a live dispatch (the guard's own rule: a word
	// that is BOTH dispatched and refused makes the command unreachable).
	//
	// A row here is therefore NOT the union — it would WITHDRAW the operator's
	// kept commands and delete their `-h` pages with them. Kept on this side
	// instead, as the record of the outcome: master's mechanism owns it now
	// (help.go's absent rows + cli_completion.go's completionSuppressedVerbs),
	// and cmd/agent/cli_help_hidden_verbs_test.go pins the suppression in BOTH
	// directions against the built binary (Rule 31).

	// ---- E + I groups → `tool` (the ONE entry point for a local tool) -------
	//
	// THE OPERATOR'S RULING, which this block implements literally: "如果全退役，
	// `gopi tool capture` 就不能跑了，只能 `gopi tool capture`： 就是这个意思!" — and, on
	// the `tool -h` line claiming these words are also top-level verbs, "胡说！
	// 他们不是" (nonsense; they are not). So the bare word refuses and the group
	// spelling is the command.
	{
		alias:     CliVerbCapture,
		canonical: toolHome(CliVerbCapture),
		retired:   true,
		reason:    "the live X11 desktop capture (screen / window / rectangle); `gopi tool capture` is the command",
	},
	{
		alias:     toolnames.ToolScreenCapture,
		canonical: toolHome(toolnames.ToolScreenCapture),
		retired:   true,
		reason:    "the tool's own underscore spelling (whole screen); `gopi tool screen_capture` is the command",
	},
	{
		alias:     CliVerbAct,
		canonical: toolHome(CliVerbAct),
		retired:   true,
		reason:    "drive the live X11 desktop (move, click, type); `gopi tool act` is the command",
	},
	{
		alias:     CliVerbAnnotate,
		canonical: toolHome(CliVerbAnnotate),
		retired:   true,
		reason:    "draw marks onto an existing PNG; `gopi tool annotate` is the command",
	},
	{
		alias:     CliVerbGif,
		canonical: toolHome(CliVerbGif),
		retired:   true,
		reason:    "turn a PNG frame sequence into one animated GIF; `gopi tool gif` is the command",
	},
	{
		alias:     CliVerbShot,
		canonical: toolHome(CliVerbShot),
		retired:   true,
		reason:    "screenshot a URL (headless Chromium) or the local chat UI; `gopi tool shot` is the command",
	},
	{
		alias:     CliVerbProxy,
		canonical: toolHome(CliVerbProxy),
		retired:   true,
		reason:    "the local protocol proxy (an Anthropic-speaking client, an OpenAI peer); `gopi tool proxy` is the command",
	},
	{
		alias:     toolnames.ToolVisionAnalyze,
		canonical: toolHome(toolnames.ToolVisionAnalyze),
		retired:   true,
		reason:    "image understanding / OCR through the vision model; `gopi tool vision_analyze` is the command",
	},
	{
		alias:     toolnames.ToolVoiceInput,
		canonical: toolHome(toolnames.ToolVoiceInput),
		retired:   true,
		reason:    "speech to text (doubao cloud by default, `--engine local` for Fun-ASR-Nano on this machine); `gopi tool voice_input` is the command",
	},
	{
		alias:     toolnames.ToolText2img,
		canonical: toolHome(toolnames.ToolText2img),
		retired:   true,
		reason:    "render a text file to PNG with the pure-Go TrueType renderer; `gopi tool text2img` is the command",
	},
	{
		alias:     CliVerbCodeSearch,
		canonical: toolHome(CliVerbCodeSearch),
		retired:   true,
		reason:    "where a symbol is DEFINED (ctags); `gopi tool code-search` is the command",
	},
	{
		alias:     toolnames.ToolKeywordSearch,
		canonical: toolHome(toolnames.ToolKeywordSearch),
		retired:   true,
		reason:    "full-text search over the repo; `gopi tool keyword_search` is the command (the tool's own underscore spelling, the documented exception)",
	},
	{
		alias:     CliVerbLedgerWindow,
		canonical: toolHome(CliVerbLedgerWindow),
		retired:   true,
		reason:    "two wall-clock bounds in, their epoch seconds out; `gopi tool ledger-window` is the command",
	},
	{
		alias:     CliVerbScratchGC,
		canonical: toolHome(CliVerbScratchGC),
		retired:   true,
		reason:    "the one-shot scratch sweep the host watchdog executes; `gopi tool scratch-gc` is the command",
	},
	{
		alias:     CliVerbText2imgVerify,
		canonical: toolHome(CliVerbText2imgVerify),
		retired:   true,
		reason:    "render the `知` sample at N px (pixel-level IoU check); `gopi tool text2img-verify` is the command",
	},
	{
		alias:     CliVerbSandbox,
		canonical: toolHome(CliVerbSandbox),
		retired:   true,
		reason:    "the ONE standard agent sandbox (create/run/show); `gopi tool sandbox` is the command",
	},
	{
		// The bare word the group serves and the top level does not (task
		// 01a19be7…, 2026-11-02). `gopi tool tokens` is its one door, and until
		// this row existed the bare word was in NO vocabulary — measured in an
		// isolated GOPI_HOME: `gopi tokens` minted a session directory and a
		// live `[auto] tokens` ledger row, i.e. it bought a real agent run from
		// a word the operator meant as a command. It is the same class as
		// `sandbox` / `scratch-gc` / `ledger-window` above and `retired` is the
		// right flag for exactly the same reason: the word was never a synonym,
		// it names the command that moved under the group.
		alias:     CliVerbTokens,
		canonical: toolHome(CliVerbTokens),
		retired:   true,
		reason:    "count a request's prompt tokens and compare with the server's own number; `gopi tool tokens` is the command",
	},

	// ---- G group: `merge` — RETIRED WITH NO SUCCESSOR (epic merge-removal) ---
	//
	// A HOMED ROW (phase 2 of the deletion, 2026-10-31): item 3/6 removed the
	// one-shot implementation, so the word no longer dispatches — but the
	// CAPABILITY still does, under the surviving family spelling. The row
	// therefore carries that address, DERIVED from the family word and the
	// verb's own leaf constant (Rule 13/14), exactly like the other nineteen
	// group rows. Measured: `gopi task merge -h` exits 0 on this tree.
	//
	// Why not leave it homeless: a homeless row prints NO address, and the
	// shipped gate (cli_spelling_pair_test.go arm 3) refuses a refusal whose
	// hint leads nowhere — measured RED on origin/master itself before this
	// row was given its home (evidence:
	// merge-row-homeless.txt in task 01a18d61's session dir).
	//
	// WHY A REFUSAL AT ALL, rather than just letting the word fall through: a
	// word that leaves the dispatch chain and is not refused by name is
	// indistinguishable from prompt text, so `gopi merge` would mint a session,
	// make one model call and write an `[auto] merge` ledger row for what the
	// operator meant as a command — the measured 2026-09-30 incident (three
	// `gopi buildinfo` invocations became three failed tasks) that this table
	// exists to prevent. The message is the retirement's ONLY discoverable home
	// now that the help row is gone.
	{
		alias:     verbAliasMerge,
		canonical: groupHome(CliVerbTask, taskverbs.Merge),
		retired:   true,
		reason:    "the standalone merge verb was removed from the command surface — a merge pass is the family spelling `gopi task merge`",
	},
}

// checkVerbAliasRows validates a row against the two facts no other table can
// supply: an address, when present, must be a command line this binary really
// RUNS (so the refusal's hint is never a dead end — Rule 15, and Rule 31's
// "run it, do not read it"), and an EMPTY address is only legal for a word
// whose capability is GONE (the two withdrawn chat surfaces) — a retired word
// WITH a home that forgot to name it would refuse while sending the reader
// nowhere.
//
// IT LIVES IN THE PRODUCTION FILE, not in the test, on purpose: the test calls
// it (so a bad row fails the suite), and the table's own readers can call it
// too. What it may NOT do is decide anything at run time — it is a statement
// about the table, not about an invocation.
//
// dispatches is the caller's oracle: "does this address really run?" The test
// answers it by EXECUTING the binary; a caller with no binary to run should not
// be calling this at all.
func checkVerbAliasRows(dispatches func(address string) bool) error {
	for _, a := range verbAliases {
		if a.alias == "" || a.reason == "" {
			return fmt.Errorf("verb alias row %+v is incomplete — an empty field would make its gate vacuous", a)
		}
		address := programAddress(a.canonical)
		if address == "" {
			if !a.retired {
				return fmt.Errorf("alias %q carries no address and is not marked retired — a word with a command behind it must send the reader there", a.alias)
			}
			continue
		}
		if !dispatches(address) {
			return fmt.Errorf("alias %q prints the address %q, which this binary does not run — the hint leads nowhere", a.alias, address)
		}
	}
	return nil
}

// verbAliasWords lists the alias spellings, in table order. Callers that need
// the vocabulary as a SET build a map from this once — there is no second copy
// of it to drift (Rules 13/14).
func verbAliasWords() []string {
	out := make([]string, 0, len(verbAliases))
	for _, a := range verbAliases {
		out = append(out, a.alias)
	}
	return out
}

// verbAliasCanonical returns the canonical command line an alias word stands
// for, or "" when the word is not a known alias. It is the READ side of the
// same table verbAliasRefusalReason walks — a caller that must answer "does
// this binary still KNOW the word?" asks here rather than keeping a second
// list (Rules 13/14). The classification gate over devdoc/gopi-verb-census.md
// is such a caller: a word that left the dispatch chain but is refused BY NAME
// is still part of this binary's surface, and its census row is the record of
// the decision rather than a row that invented a verb.
//
// MERGE NOTE (rework 01a10e11…, 2026-10-06): this function arrived with the
// other side of the merge (master's verb-retirement phase 1) while the census
// classification gate that CALLS it arrived with THIS side; both are kept.
// The body is the union of the two behaviours — it returns the row's canonical
// address whatever shape that is (the phase-1 rows carried a two-level
// canonical; the rows this side added carry one too, and the homeless rows
// carry ""), which is exactly what the three callers ask for.
func verbAliasCanonical(word string) string {
	for _, a := range verbAliases {
		if a.alias == word {
			return a.canonical
		}
	}
	return ""
}

// verbAliasRetiredRefusal is the message for a retired TOP-LEVEL verb — a word
// that really dispatched here until the verb-retirement epic (2026-10-05) moved
// its door into a family. It leads with the retirement (so the reader learns
// their muscle memory is stale, not that they mistyped), prints the address the
// row carries VERBATIM, and keeps the ONE way to mean the word literally.
//
// The address is a whole COMMAND LINE, never a word: the rows that need this
// message have homes that are two or three words long (`gopi session log`,
// `gopi tool web search`), and rendering those into a "%s" that expects one
// word is how `gopi session log` came to read as `gopi session` plus a stray
// argument — the operator's objection, answered by the format itself.
// Args: (alias, address, address, alias).
const verbAliasRetiredRefusal = "gopi: `%s` is RETIRED — use `%s` (`%s -h` for its help). " +
	"To send the word as a prompt instead, say so explicitly: `--prompt %s`."

// verbAliasHomelessRefusal is the message for the rows with NO live home — the
// two visible commands this binary retired outright (`gopi ui`, `gopi cli`),
// whose capability was withdrawn rather than moved. There is no address to
// print, so it is not rendered: a message that printed a bare `gopi ` would
// point at nothing, and a hint that leads nowhere is worse than no hint
// (Rule 15 — the same reason the spelling table's rows all carry a resolvable
// address). What remains is the honest route a refusal must always leave open:
// say the word is a prompt.
// Args: (alias, reason, alias).
const verbAliasHomelessRefusal = "gopi: `%s` is RETIRED with no replacement — %s. " +
	"To send the word as a prompt instead, say so explicitly: `--prompt %s`."

// homelessRefusalWord reports whether word names a row whose capability was
// WITHDRAWN rather than moved — the two views of the chat engine retired whole
// (`gopi ui`, `gopi cli`). It exists so a second reader (the guard's vocabulary
// contract) can ask the same question the refusal message answers, instead of
// re-deriving "no home" from a third place and drifting.
func homelessRefusalWord(word string) bool {
	for _, a := range verbAliases {
		if a.alias == word {
			return a.refusalAddress() == ""
		}
	}
	return false
}

// refusalAddress is the program spelling a row's refusal sends the reader to:
// `programAddress(canonical)` for a homed row, and "" for a homeless one (a
// withdrawn capability has nothing to point at). Both tables ask THIS method,
// so "no home" has one representation and a caller cannot render a bare
// `gopi ` by forgetting a case.
func (a verbAlias) refusalAddress() string { return programAddress(a.canonical) }

// verbAliasRefusalReason returns the pointed refusal for a retired word, or ""
// when the word is not in either half of the vocabulary. It is the table walk
// the ONE refusal lookup (undispatchedToolWordRefusalReason) performs for the
// alias half of the vocabulary, so a word refused here is refused in BOTH
// shapes (argv[0] and after a run flag) by construction rather than by two
// matching edits.
//
// THREE SHAPES, ONE LOOKUP — the row decides which:
//
//	(a) RETIRED WITH A HOME (`log`, `capture`, the web flat verbs): the word was
//	    a top-level command and is now reached through its family, so the
//	    message leads with `retired — use \`gopi session log\``, prints that
//	    address VERBATIM (it may be two or three words), and keeps the ONE way
//	    to mean the word literally.
//	(b) HOMELESS (`ui`, `cli`): no successor exists, so BOTH address slots are
//	    empty and the message must not render a bare `gopi ` — it names the
//	    retirement and leaves the one honest route (`--prompt`), which is what
//	    keeps the refusal from being a dead end (Rule 15).
//	(c) SYNONYM (`buildinfo` → `version`): the word never dispatched; the
//	    shipped message is kept byte-identical (its shape is pinned by the
//	    live-binary tests and by the operator's "keep the existing row's shape"
//	    ruling).
func verbAliasRefusalReason(word string) string {
	for _, a := range verbAliases {
		if a.alias != word {
			continue
		}
		address := programAddress(a.canonical)
		switch {
		case a.retired && address == "":
			return fmt.Sprintf(verbAliasHomelessRefusal, a.alias, a.reason, a.alias)
		case a.retired:
			return fmt.Sprintf(verbAliasRetiredRefusal, a.alias, address, address, a.alias)
		default:
			return fmt.Sprintf(undispatchedToolWordRefusal,
				a.alias, address, address, a.alias, config.PromptEnvVar)
		}
	}
	return ""
}
