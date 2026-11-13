package main

// cli_completion.go — shell tab-completion support for the gopi CLI (Epic:
// linux tab-completion for gopi commands and subcommands).
//
// The gopi CLI is a hand-rolled dispatcher (isXxxSubcommand predicate chain
// in main.go), NOT a cobra/urfave app — so there is no built-in completion
// generator to enable. This file adds the standard generator the ecosystem
// uses (git / docker / kubectl / gh all ship one):
//
//	gopi config completion bash|zsh|fish
//
// prints a shell script to STDOUT that registers completion for the `gopi`
// command in the caller's shell. Sourcing the output makes:
//
//	gopi <TAB>          → first-level commands (memory, task, web, log, …)
//	gopi memory <TAB>   → second-level subcommands (search, store, get, …)
//	gopi task <TAB>     → the ledger sub-verbs (list, show, submit, run, …)
//
// The completion CANDIDATE SETS are declared here as the single source of
// truth (Rule 2), derived from the same subcommand tables the dispatch code
// uses (memorySubcommandNames, the task verb dispatch, …). The generated
// scripts embed the candidate lists verbatim, so:
//   - they work WITHOUT a gopi round-trip (no __complete back-call needed —
//     static sets, unlike cobra's dynamic directive protocol), and
//   - they cannot drift from the CLI surface as long as the tables here are
//     kept in sync with the dispatchers (guarded by cli_completion_test.go,
//     which locks every set against the actual dispatch cases).
//
// Registration per shell (user-level — no /etc/..., no root needed):
//
//	bash:  source <(gopi config completion bash)
//	       (or: gopi config completion bash > ~/.local/share/bash-completion/completions/gopi)
//	zsh:   gopi config completion zsh > ~/.zfunc/_gopi  (add ~/.zfunc to $fpath)
//	fish:  gopi config completion fish > ~/.config/fish/completions/gopi.fish

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/taskverbs"
)

// completionUsage is the -h text for `gopi config completion` (the "every command
// answers its own -h" hard standard).
const completionUsage = `gopi config completion — generate shell tab-completion scripts for the gopi CLI.

Usage:
  gopi config completion bash
      Print a bash completion script to stdout. It makes gopi <TAB> offer
      the first-level commands and gopi <verb> <TAB> the subcommands
      (memory search|store|get|…, web search|fetch, task list|show|submit|…,
      checkpoint list|export|resume). Enable by sourcing the output once per shell:
        source <(gopi config completion bash)
      or persist it (auto-loaded by the bash-completion package):
        gopi config completion bash > ~/.local/share/bash-completion/completions/gopi

  gopi config completion zsh
      Print a zsh completion function. Enable:
        mkdir -p ~/.zfunc && gopi config completion zsh > ~/.zfunc/_gopi
        echo 'fpath=(~/.zfunc $fpath); autoload -Uz compinit; compinit' >> ~/.zshrc

  gopi config completion fish
      Print a fish completion file. Enable:
        gopi config completion fish > ~/.config/fish/completions/gopi.fish

No /etc/ registration is needed: every shell's completion system loads
user-level completions from the directories above (bash-completion's
BASH_COMPLETION_USER_DIR / XDG, zsh $fpath, fish ~/.config/fish/completions).
`

// ---------------------------------------------------------------------------
// Candidate sets — the SINGLE SOURCE OF TRUTH for every completion the
// generated scripts offer. Each *_names list mirrors the corresponding
// dispatch surface in the cli_*.go files; cli_completion_test.go locks each
// set against the real dispatch cases so a new verb/subcommand added to a
// dispatcher WITHOUT updating the table here fails the test.
// ---------------------------------------------------------------------------

// topLevelCommandNames is the `gopi <TAB>` candidate set: every verb the
// dispatch chain in main() recognizes as a subcommand. Sorted for stable
// output (shell completion lists are conventionally sorted).
//
// THE SET IS THE DISPATCH SURFACE, AND NOTHING ELSE (epic verb-retirement,
// 2026-10-05). Every retired word was deleted from it together with its
// dispatch entry: completion is the strongest teaching surface gopi has — it
// shows the operator a word at the exact moment they are deciding what to
// type — so an entry the dispatcher now REFUSES would actively teach an error
// rather than merely be stale. That is why this list may not carry a word
// "for compatibility"; the refusal table (cli_verb_aliases.go) is where a
// retired spelling is kept, and its message carries the address.
//
// The entries are the VERB WORDS the dispatch chain reads (CliVerb* constants
// from cli_verbs.go), so a rename propagates here by construction. No entry is
// a tool WIRE name any more: since the retirement the local tools are reached
// through the group (`gopi tool <command>`), and their wire spellings — which
// keep the underscore by rule 1 (`devdoc/cli-naming-convention.md`) — are
// enumerated by `gopi tool -h` / toolCompletionNames instead.
var topLevelCommandNames = []string{
	CliVerbChat,
	CliVerbConfig,
	CliVerbMemory,
	CliVerbMerger,
	CliVerbNotify,
	CliVerbSession,      // the session family verb (list/show/log/stats/raw/resume/checkpoint)
	CliVerbScreen,       // the screen family verb: the BYTES of a recorded screen (cli_screen.go)
	CliVerbScreenReplay, // the same reader addressed AT THE TOP LEVEL (cli_screen_replay_verb.go)
	CliVerbTask,
	CliVerbTool, // the tool group (cli_tool.go): the local tools under one noun
	// ui / cli are DISPATCHED and UNPUBLISHED, so they belong in the dispatch
	// vocabulary this list mirrors — the suppression is expressed by
	// completionSuppressedVerbs below, NOT by their absence here (union
	// resolution, rework 01a10e11…, 2026-10-06). Their absence would collapse
	// two distinct facts into one shape: "this verb does not exist" and "this
	// verb exists and is withheld" would both read as a missing entry, and
	// TestHelpHiddenVerbsAreDispatchedNotGone — written to keep a hidden verb
	// from decaying into a removed one — asserts exactly that they exist here
	// and are dispatched. The branch this merged from had retired the two words
	// outright and dropped these entries with them; master's later ruling
	// (cc8289b0, 04:50, task 01a10dba…: 暂不暴露) keeps the commands, so the
	// entries come back and the suppression list does the withholding.
	CliVerbUI,
	CliVerbCLI,
	CliVerbUIState,
	CliVerbVersion,
	CliVerbWeb,
	CliVerbWorker,
}

// completionSuppressedVerbs are the verbs that DISPATCH but are deliberately NOT
// PUBLISHED on a discovery surface — neither the top-level help index
// (internal/config/help.go's commandIndex) nor the `gopi <TAB>` candidate list
// this file renders. The list has TWO KINDS OF ROW, and the distinction decides
// what the verb answers when it is typed:
//
//	(a) LIVE AND UNPUBLISHED — `gopi ui` / `gopi cli`. Code and entry point are
//	    kept and the command runs; only its publicity was withdrawn. Operator
//	    ruling, 2026-10-06 (task 01a10dba…): 暂不暴露.
//	(b) RETIRED AND UNPUBLISHED — `gopi merger`. The verb refuses at every
//	    spelling (2026-09-19, devdoc/merger-retired.md) and names the surviving
//	    one-shot face (itself removed since, epic merge-removal item 2); the
//	    WORD keeps routing so an old script
//	    gets the explanation instead of "unknown command".
//
// WHY (b) IS A SUPPRESSION AND NOT A DELETION. The word must stay in
// topLevelCommandNames (that list is the DISPATCH vocabulary — see its own
// comment), and it must stay in the refusal vocabulary. What it must NOT do is
// sit in the top-level command index teaching a reader a command that answers
// `-h` with a refusal: the index's preamble promises "each accepts -h for its
// full help", and that row was the one row on the page which could not honour
// it. Task 01a113a8… (2026-10-06) is the fix; the operator's report is the
// requirement in one line — 「merger 既然都不存在来为什么还要提一遍？这不是
// 混淆吗？」
//
// They are held HERE, beside the candidate table, because the two surfaces must
// be suppressed TOGETHER: a completion list offering a verb the help does not
// explain is the drift TestTopLevelHelpCoversEveryDispatchedSubcommand's reverse
// direction exists to catch ("completion follows the help"). A suppressed verb
// is therefore subtracted from the published set by ONE function
// (topLevelCompletionNames) rather than by editing every renderer, and
// cmd/agent/cli_help_hidden_verbs_test.go asserts the two surfaces agree in both
// directions — so re-adding either half alone is red, not silent. What a
// suppressed verb IS (live or retired) is asserted by that same file, per group,
// against the built binary.
//
// MERGE NOTE (rework 01a10e11…, 2026-10-06): this list arrived with the merge's
// other side, and the UNION KEEPS IT — together with the dispatch entries it
// names. The earlier note here asserted the opposite (that the tree carried no
// `ui` / `cli` dispatch entry and that the membership was a KNOWN GAP); that
// claim was WRONG, and this is the correction. `isUISubcommand` / `isTermSubcommand`
// ARE in this tree's cmd/agent/main.go pre-run chain (they arrive from master
// and from the branch's own cli_ui.go / cli_term.go), so both verbs dispatch,
// both answer `-h`, and this list is the ONE place that withholds them from the
// published surfaces — which is master's ruling (`cc8289b0`, 04:50, task
// 01a10dba…: 暂不暴露, the code stays and nothing advertises it) and the LATER
// of the two positions this merge had to reconcile. The branch's own commit had
// retired the two words outright; that reading is superseded here, and
// cmd/agent/cli_verb_aliases.go carries the same resolution on the refusal side.
var completionSuppressedVerbs = []string{CliVerbUI, CliVerbCLI, CliVerbMerger}

// topLevelCompletionNames is what `gopi <TAB>` actually OFFERS: the dispatch
// vocabulary (topLevelCommandNames) minus every suppressed verb. The renderers
// read this, never topLevelCommandNames directly, so the publish decision lives
// in one place. The order is the table's own (the renderers keep it); only the
// membership changes.
func topLevelCompletionNames() []string {
	out := make([]string, 0, len(topLevelCommandNames))
	for _, v := range topLevelCommandNames {
		if !completionSuppressesVerb(v) {
			out = append(out, v)
		}
	}
	return out
}

// completionSuppressesVerb reports whether a verb is withheld from the published
// surfaces. It is the ONE membership question the suppression asks; the set is
// never scanned by a caller (Rule 13).
func completionSuppressesVerb(verb string) bool {
	for _, v := range completionSuppressedVerbs {
		if v == verb {
			return true
		}
	}
	return false
}

// completionShellNames is the `gopi config completion <TAB>` candidate set: the
// shells this generator can emit scripts for.
var completionShellNames = []string{"bash", "zsh", "fish"}

// memoryCompletionNames is the `gopi memory <TAB>` candidate set. It IS the
// canonical group list (cli_memory.go's memorySubcommandNames — the same slice
// the dispatcher's switch and the unknown-subcommand error enumerate), so a
// sub-verb added to the group surface reaches tab completion BY CONSTRUCTION.
// The previous hand-typed copy of the list drifted the moment `memory backup`
// was wired into cli_memory.go: completion offered 9 of the group's 10
// sub-verbs and the lock test below went red. One definition, referenced
// (Rules 13/14) — never mirrored.
var memoryCompletionNames = memorySubcommandNames

// webCompletionNames is the `gopi web <TAB>` candidate set (the web tool
// group; the bare `gopi web` / server flags stay out — they are the server
// face, not subcommands).
var webCompletionNames = []string{"search", "fetch"}

// taskCompletionNames is the `gopi task <TAB>` candidate set: the family
// surface the tasksUsage help (cli_tasks.go) documents — tab follows the
// HELP/advertised surface, not the dispatch superset. 'serve' is therefore
// NOT offered: it is the deprecated alias of 'merger' (serve/worker-split);
// dispatch still accepts the alias for scripts, but the completion surface
// offers only what the family help documents (guarded by
// TestTaskCompletionNamesMatchFamilyHelp). cli_completion_test.go locks the
// set against both the dispatch cases and the help lines by scanning
// cli_tasks.go's source.
//
// The set IS taskverbs.FamilyVerbs (one definition, referenced — Rule 13/14):
var taskCompletionNames = taskverbs.FamilyVerbs

// checkpointCompletionNames is the `gopi session checkpoint <TAB>` candidate set — the
// checkpoint group's second level. It carries list|export ONLY: `resume` was
// REMOVED from this group (epic verb-restructure, 2026-10-05 — one continuation
// keeps one spelling, `gopi session resume`), and a completion set that offered
// a word the dispatcher refuses would teach the operator to type an error.
var checkpointCompletionNames = []string{"list", "export"}

// subcommandSets maps each group verb to its second-level candidate set. A
// verb NOT listed here has no subcommand level (its argument is a file /
// session id / free text — the shell's default file completion applies).
var subcommandSets = map[string][]string{
	CliVerbConfig:  configSubcommandNames,
	CliVerbMemory:  memoryCompletionNames,
	"web":          webCompletionNames,
	CliVerbTask:    taskCompletionNames,
	CliVerbSession: sessionGroupCompletionNames,
	CliVerbTool:    toolCompletionNames,
}

// completionShellKind is the emitted script flavor.
type completionShellKind string

const (
	completionBash completionShellKind = "bash"
	completionZsh  completionShellKind = "zsh"
	completionFish completionShellKind = "fish"
)

// isCompletionSubcommand reports whether argv begins with the `completion`
// verb (or the internal `__complete` back-call used by cobra-style dynamic
// completion — reserved for future use; the static scripts do not call it).
// Checked FIRST in main()'s dispatch chain so `gopi config completion bash` never
// falls through to the agent loop.
func isCompletionSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case CliVerbCompletion:
		runCompletionSubcommand(args[1:])
		return true
	}
	return false
}

// runCompletionSubcommand dispatches `gopi config completion <shell>`.
func runCompletionSubcommand(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, completionUsage)
		os.Exit(1)
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(completionUsage)
		return
	case "bash":
		fmt.Print(renderBashCompletion())
		return
	case "zsh":
		fmt.Print(renderZshCompletion())
		return
	case "fish":
		fmt.Print(renderFishCompletion())
		return
	default:
		fmt.Fprintf(os.Stderr, "gopi config completion: unknown shell %q — want %s\n\n%s",
			args[0], strings.Join(completionShellNames, "|"), completionUsage)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Script renderers — one per shell. Each embeds the candidate sets as static
// data (no gopi back-call at completion time).
// ---------------------------------------------------------------------------

// renderBashCompletion emits the bash script. Design notes:
//   - COMP_CWORD 1 → first-level commands; COMP_CWORD 2 → the group's
//     subcommands when the verb is a group; anything deeper → the default
//     (file) completion, since gopi's leaf arguments are files/session ids.
//   - The function is registered with `complete -F` and `-o default` so a
//     non-matching position falls back to bash's filename completion.
//   - The candidate lists are quoted word lists — a command name with a
//     hyphen (code-search) survives compgen -W word-splitting.
func renderBashCompletion() string {
	var b strings.Builder
	b.WriteString(`# bash completion for gopi — generated by ` + "`gopi config completion bash`" + `.
# Source once per shell (or drop into bash-completion's completions dir).

_gopi() {
    local cur
    cur="${COMP_WORDS[COMP_CWORD]}"
    case "${COMP_CWORD}" in
        1)
            COMPREPLY=( $(compgen -W "` + strings.Join(topLevelCompletionNames(), " ") + `" -- "$cur") )
            ;;
        2)
            case "${COMP_WORDS[1]}" in
`)
	// One case arm per group verb with a subcommand level.
	groupVerbs := make([]string, 0, len(subcommandSets))
	for v := range subcommandSets {
		groupVerbs = append(groupVerbs, v)
	}
	sort.Strings(groupVerbs)
	for _, v := range groupVerbs {
		fmt.Fprintf(&b, "                %s)\n                    COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n                    ;;\n",
			v, strings.Join(subcommandSets[v], " "))
	}
	b.WriteString(`            esac
            ;;
    esac
}
complete -o default -F _gopi gopi
`)
	return b.String()
}

// renderZshCompletion emits the zsh completion function. Uses the autoload
// convention: the file IS the function body (named _gopi via its install
// path), with the `#compdef gopi` header so compinit wires it to the command.
// Install: mkdir -p ~/.zfunc && gopi config completion zsh > ~/.zfunc/_gopi, with
// ~/.zfunc in $fpath before compinit. Offers first-level commands at
// position 1 and group subcommands at position 2.
func renderZshCompletion() string {
	var b strings.Builder
	b.WriteString(`#compdef gopi
# zsh completion for gopi — generated by ` + "`gopi config completion zsh`" + `.
# This file is the completion function body (autoload convention): install as
# ~/.zfunc/_gopi with ~/.zfunc in $fpath before compinit.

# first level: commands
local -a commands
commands=(
`)
	for _, c := range topLevelCompletionNames() {
		fmt.Fprintf(&b, "    '%s'\n", c)
	}
	b.WriteString(`)

if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
fi

# second level: group subcommands
case "${words[2]}" in
`)
	groupVerbs := make([]string, 0, len(subcommandSets))
	for v := range subcommandSets {
		groupVerbs = append(groupVerbs, v)
	}
	sort.Strings(groupVerbs)
	for _, v := range groupVerbs {
		fmt.Fprintf(&b, "    %s)\n        local -a subs\n        subs=(\n", v)
		for _, s := range subcommandSets[v] {
			fmt.Fprintf(&b, "            '%s'\n", s)
		}
		b.WriteString("        )\n")
		fmt.Fprintf(&b, "        if (( CURRENT == 3 )); then\n            _describe 'subcommand' subs\n        fi\n        ;;\n")
	}
	b.WriteString(`esac
`)
	return b.String()
}

// renderFishCompletion emits the fish completion file. fish's completion
// grammar is declarative: each `complete -c gopi` line registers one
// candidate group, with __fish_use_subcommand / __fish_seen_subcommand_from
// conditions scoping the levels.
func renderFishCompletion() string {
	var b strings.Builder
	b.WriteString(`# fish completion for gopi — generated by ` + "`gopi config completion fish`" + `.
# Install: gopi config completion fish > ~/.config/fish/completions/gopi.fish

# first level: commands
complete -c gopi -n '__fish_use_subcommand' -a '` + strings.Join(topLevelCompletionNames(), " ") + `'
`)
	groupVerbs := make([]string, 0, len(subcommandSets))
	for v := range subcommandSets {
		groupVerbs = append(groupVerbs, v)
	}
	sort.Strings(groupVerbs)
	for _, v := range groupVerbs {
		fmt.Fprintf(&b, "# second level: %s subcommands\n", v)
		fmt.Fprintf(&b, "complete -c gopi -n '__fish_seen_subcommand_from %s' -a '%s'\n",
			v, strings.Join(subcommandSets[v], " "))
	}
	return b.String()
}
