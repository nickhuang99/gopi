package main

// chat_thinking.go — the `/thinking [on|off]` command: flip the LIVE reasoning
// display of the session in front of you.
//
// WHY IT EXISTS. `Agent.HideThinking` is already a knob with one name and three
// published channels (`--hide-thinking`, `GOPI_HIDE_THINKING`, settings
// `hide_thinking`), and all three are read ONCE, when the agent is built. So a
// run started with the reasoning shown stays that way until the process ends,
// and a run started with it hidden shows nothing until the process ends. The one
// question an operator asks in the middle of a session — "show me what it is
// actually thinking right now" — had no answer short of quitting and re-launching
// with a flag.
//
// WHAT IT REUSES, AND WHAT IT DELIBERATELY DOES NOT CREATE (Rule 14). The switch
// is `Agent.HideThinking` and nothing else: no second field, no per-turn copy, no
// shadow boolean in the REPL. `Agent.HideThinking` is read at each emit point
// (loop.go's streaming branch and its non-streaming fallback), so a write here is
// in force for the NEXT delta — there is nothing to re-build and no cache to
// invalidate. That also means the STARTUP state is respected for free: the
// command prints whatever the process was launched with, and only changes it when
// the operator asks.
//
// WHY THE STATE IS PER-SESSION AND NEVER WRITTEN BACK (operator's rule, and a
// design choice rather than an omission). This is a VIEW decision about the next
// minute of one conversation, not a configuration edit; writing settings.json
// from a chat would make every future run inherit an accident of this one, and
// `gopi config show` would report it as a document value nobody chose. The
// confirmation line says "this session only" so the reader is never in doubt
// about what will happen after a restart.
//
// WHY THE WORD IS BOTH A TABLE ENTRY AND A REPL ARM. The table (`commands.go`)
// is what makes the word discoverable — `/help` lists it, the completion menu
// offers it, and a refusal names it — but its `Run` signature
// (`func(args, CommandContext)`) cannot see a live `*agent.Agent`; the switch it
// flips lives on one. So the table entry documents the command and REFUSES on a
// carrier that holds no agent (Rule 15: it names that reason instead of printing a
// state it did not read), and `gopi chat` answers the word in its own switch arm,
// exactly as `/config` does (chat_config.go's arm, cli_chat.go). One vocabulary,
// one behaviour, two doors that already exist.
//
// THE WIRE IS UNTOUCHED. `keep_thinking` / `GOPI_KEEP_THINKING` is the OTHER
// axis — whether assistant reasoning is trimmed from the NEXT request — and this
// command never reads or writes it. `/thinking off` hides the live display only;
// what goes out on the wire, and what is kept of it, is decided by those fields.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
)

// The command's vocabulary (Rules 1/13/14): every word the switch, the usage page
// and the shared table compare against or print is declared once.
const (
	// chatThinkingCommandName is the slash word that owns this file.
	chatThinkingCommandName = "thinking"

	// chatThinkingOnWord / chatThinkingOffWord are the two arguments. They are
	// this command's OWN grammar, not the environment's boolean vocabulary: `on`
	// and `off` are what a person types at a prompt, while `1`/`0`/`true`/`false`
	// are what a knob's document carries. Accepting both would be a second grammar
	// to document for no gain.
	chatThinkingOnWord  = "on"
	chatThinkingOffWord = "off"

	// The two state words the command REPORTS. They describe the DISPLAY, never
	// the model's thinking mode (which `--thinking`/`GOPI_THINKING` owns): hidden
	// reasoning is still requested and still spent, it is only not printed.
	chatThinkingStateShown  = "shown"
	chatThinkingStateHidden = "hidden"
)

// The notes the command writes. Each names the state it just set AND the scope it
// set it in, because the scope is the part a reader would otherwise guess wrong:
// a toggle that persisted would be indistinguishable from one that does not, and
// the restart is where that difference shows up (Rule 15).
const (
	chatThinkingOffNote  = "[gopi] reasoning display: " + chatThinkingStateHidden + " — this session only; the startup config is unchanged.\n"
	chatThinkingOnNote   = "[gopi] reasoning display: " + chatThinkingStateShown + " — this session only; the startup config is unchanged.\n"
	chatThinkingQueryFmt = "[gopi] reasoning display: %s — `/thinking on` shows it, `/thinking off` hides it (this session only).\n"

	// chatThinkingNoAgentRefusal is the table entry's answer on a carrier that
	// holds no live agent. Naming the carrier that CAN answer is the point: the
	// word is in /help on both carriers, so a silent no-op here would be read as a
	// switch that does not work.
	chatThinkingNoAgentRefusal = "`/%s` flips the live reasoning display of a running chat, and this carrier holds no agent to flip — use it inside `gopi chat`"

	// chatThinkingBadArgLeading is the visible refusal for an argument that is
	// neither word nor absent. The accepted spellings are appended from the same
	// constants the switch compares against, so a rename cannot leave the refusal
	// advertising a spelling the code dropped.
	chatThinkingBadArgLeading = "`/%s` takes no argument, `%s` or `%s` — %q is not one of them"
)

// chatThinkingUsage is `/help thinking` — the ONE definition, shared by the shared
// table's entry, `/thinking help` and the REPL's arm.
//
// IT IS A PLAIN RAW STRING ON PURPOSE, like every other usage block in this
// package (chatConfigUsage, chatQueueUsage): the wrap-budget sweep
// (help_width_test.go) only measures a help constant whose value is a string
// LITERAL, so composing this one from constants would take it silently out of the
// 88-column gate. The word it spells is kept honest by the test instead
// (TestThinkingIsInTheSharedTableAndHelp asserts this block OPENS with the
// command word and both argument words taken from their constants), so a rename
// cannot leave the page documenting a spelling the switch no longer answers.
const chatThinkingUsage = `Usage: /thinking [on|off]

Show or hide the model's LIVE reasoning display for the rest of this session.
Bare /thinking prints the current state and changes nothing.

  on    print the reasoning stream as the model produces it
  off   stop printing it (the model still thinks; only the display stops)

It flips Agent.HideThinking — the SAME switch --hide-thinking,
GOPI_HIDE_THINKING and settings hide_thinking set at start-up — so the state
this command prints IS the state the run started in, and a toggle takes effect
on the next reasoning fragment, not in the next session.

The change lasts until this session ends and is never written back: settings is
configuration, this is a view of the next minute's output. The wire is untouched
— keep_thinking decides what reasoning is carried to the model, a different
question from what is printed here.`

// chatThinkingArg returns everything the line carried after the word itself:
// `/thinking off` → "off", a bare `/thinking` → "". The word is stripped ONCE
// here rather than at the call site, so the switch and the usage answer cannot
// disagree about where the argument starts (the /config shape).
func chatThinkingArg(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), chatSlashTrigger+chatThinkingCommandName))
}

// chatThinkingHandleLine answers `/thinking [on|off]` on the REPL: read the
// argument, flip `Agent.HideThinking` when one was given, and print the state the
// operator is now in.
//
// IT REPORTS THE STATE IT MEASURED, not the state it intended: the confirmation
// line is composed from `ag.HideThinking` AFTER the write, so a switch that did
// not take (a nil agent, a future refactor that moved the field) shows up as a
// line that contradicts the request rather than as a silent success.
func chatThinkingHandleLine(input *chatInput, ag *agent.Agent, line string) {
	arg := chatThinkingArg(line)
	if ag == nil {
		// A carrier with no agent has no switch to read; say so rather than print a
		// state nobody measured.
		fmt.Fprintf(input.errWriter(), chatThinkingNoAgentRefusal+"\n", chatThinkingCommandName)
		return
	}
	if commandHelpRequested([]string{strings.ToLower(arg)}) {
		// HELP: the same page the table's entry answers, from the ONE constant —
		// so `/thinking help` and `/help thinking` cannot describe two commands.
		// The word set is the shared helper's, not a second list (Rules 13/14).
		input.WriteNotice(chatThinkingUsage + "\n")
		return
	}
	switch strings.ToLower(arg) {
	case "":
		// QUERY: no argument, no change. The state is read from the live field, so
		// "the startup state" and "what this prints" cannot drift apart.
		input.WriteNotice(fmt.Sprintf(chatThinkingQueryFmt, chatThinkingStateWord(ag.HideThinking)))
		return
	case chatThinkingOnWord:
		ag.HideThinking = false
		input.WriteNotice(chatThinkingOnNote)
		return
	case chatThinkingOffWord:
		ag.HideThinking = true
		input.WriteNotice(chatThinkingOffNote)
		return
	}
	// REFUSED BY NAME (Rule 15): the accepted spellings come from the same
	// constants the switch compares against.
	fmt.Fprintf(input.errWriter(), chatThinkingBadArgLeading+"\n",
		chatThinkingCommandName, chatThinkingOnWord, chatThinkingOffWord, arg)
}

// chatThinkingStateWord renders a `HideThinking` value as the word the console
// uses. It is the ONE mapping, so the query line and any later reader of the same
// field cannot describe one state with two words.
func chatThinkingStateWord(hideThinking bool) string {
	if hideThinking {
		return chatThinkingStateHidden
	}
	return chatThinkingStateShown
}

// slashThinkingCommand is the shared table's `/thinking`: it answers the help
// request (the hard-help standard every table command carries) and REFUSES the
// switch on a carrier that holds no live agent, naming the carrier that does.
//
// It is the discoverability half of the command, not a second implementation: the
// switch itself needs an `*agent.Agent`, which `Run(args, CommandContext)` does
// not carry, so the table cannot flip it without inventing a second place for the
// live field to live (Rule 35: no new seam for one word).
func slashThinkingCommand(args []string, _ CommandContext) (string, error) {
	if commandHelpRequested(args) {
		return chatThinkingUsage, nil
	}
	return "", fmt.Errorf(chatThinkingNoAgentRefusal, chatThinkingCommandName)
}
