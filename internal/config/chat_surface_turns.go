package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// chat_surface_turns — the chat_surface tool's SELF-DRIVEN TURN BUDGET (one
// knob, three channels, one resolution).
//
// WHY IT EXISTS. The bound that caps `chat_surface action=type` per period was a
// package constant (8) with no channel of its own, so an operator running a long
// autonomous verification had no way to raise it short of a rebuild — and the
// only lever the model had (max_turns) can TIGHTEN a period, never lengthen one
// (devdoc/runaway-loop-20260923.md is the incident that made a bound a
// precondition for shipping a self-driving seam). The bound itself stays a bound:
// this knob moves the CEILING, and the operator's own submitted line stays the
// only thing that resets a period.
//
// CHANNELS, highest first — the same three the rest of internal/config uses:
//
//	rank 1  CLI flag `--chat-surface-turns N`  (hoisted into the env before the
//	                                            subcommand chain, so `gopi chat`
//	                                            sees it)
//	rank 2  env  GOPI_CHAT_SURFACE_TURNS=N
//	rank 3  settings.json "chat_surface_turns": N
//	rank 4  the built-in default (ChatSurfaceTurnsDefault)
//
// The flag is published into the env by BootstrapEnvFromArgs
// (PublishChatSurfaceTurnsFlag) because `gopi chat` returns from main() at the
// subcommand chain and never reaches the agent-path config block: a flag parsed
// only there would be silently dropped by the very verb it exists for (the
// --chat-ideal-geometry precedent).
//
// Rule 15 discipline: a value that is not a positive integer, or one that falls
// outside [ChatSurfaceTurnsFloor, the built-in default ceiling channel's own
// clamp], is REPORTED on stderr and the NEXT channel decides — never silently
// read as unset, because "the operator asked for a bound I did not understand"
// and "nobody asked" must not look alike.

// ChatSurfaceTurnsSettingsKey is the settings.json key of the knob.
const ChatSurfaceTurnsSettingsKey = "chat_surface_turns"

// ChatSurfaceTurnsEnv is the env override — and the ONE representation the CLI
// flag is published as, so exactly one reader below sees all three channels
// (Rule 2).
const ChatSurfaceTurnsEnv = "GOPI_CHAT_SURFACE_TURNS"

// ChatSurfaceTurnsFlagName is the CLI spelling (`--chat-surface-turns 30`),
// bare (no dashes) for the reason every other knob's name is: the catalog, the
// arity table, the help rows and the hoist all spell the prefix once.
const ChatSurfaceTurnsFlagName = "chat-surface-turns"

// ChatSurfaceTurnsDefault is the built-in bound — the ONE place the default
// number is written down (Rule 13/14). It is exported because the agent package
// declares ITS default as an alias of this value rather than a second copy, and
// because the tool description, the help rows and CONFIG.md must name the same
// number.
//
// 20 rather than 8: 8 turns is not enough for the long autonomous verification
// the self-driven seam exists for, and the guarantee that makes the seam safe is
// NOT the number — it is that the model can never renew itself and every
// self-driven turn is announced on the operator's console.
const ChatSurfaceTurnsDefault = 20

// ChatSurfaceTurnsFloor is the smallest bound any channel may install. It is
// the same floor the tool's max_turns argument is clamped up to: "bound it"
// cannot be answered with a bound that fires before the first self-driven turn.
const ChatSurfaceTurnsFloor = 2

// ChatSurfaceTurnsCeiling is the largest bound any channel may install. A bound
// beyond it is not a stronger guarantee, it is an unbounded loop wearing a
// number: an operator (or a settings file copied between machines) asking for
// 100000 would make the guard unmeasurable.
const ChatSurfaceTurnsCeiling = 1000

// The source labels a resolution reports: which channel decided. They are the
// channel spellings themselves where they exist (one definition each, Rules
// 1b/13/14), not a second vocabulary a reader would have to map back.
const (
	// ChatSurfaceTurnsSourceDefault is reported when nobody declared a value.
	ChatSurfaceTurnsSourceDefault = "default"
)

// The refusal texts. They are constants for the usual reason: the parser's
// message and the doc's claim must not be two spellings of one rule (Rule 13).
const (
	chatSurfaceTurnsShapeClaim  = "a whole number of turns"
	chatSurfaceTurnsRangeFmt    = "outside the accepted range %d..%d"
	chatSurfaceTurnsRefusalFmt  = "is not %s"
	chatSurfaceTurnsConsequence = "the next channel decides"
	chatSurfaceTurnsDefaultHint = "the built-in default decides"
)

// ChatSurfaceTurnsState is the RESOLVED bound plus the channel that decided it.
// The source half exists because "the bound is 30 because you asked for it" and
// "because nobody asked" are two different facts about one number, and a caller
// that reports the bound must be able to say which.
type ChatSurfaceTurnsState struct {
	Turns  int
	Source string
}

// ChatSurfaceTurns resolves the self-driven turn bound through the ONE
// resolution below (Rule 2). internal/agent's ceiling reader calls THIS, so the
// tool's refusal, its clamping, its console notices and its description can
// never disagree about the number.
func ChatSurfaceTurns() ChatSurfaceTurnsState {
	return resolveChatSurfaceTurns()
}

// ChatSurfaceTurnCount is the resolved bound as a plain number, for the callers
// that only need the value.
func ChatSurfaceTurnCount() int { return ChatSurfaceTurns().Turns }

// PublishChatSurfaceTurnsFlag writes the flag's value into the knob's ONE env
// representation (Rule 2/13/14 — one spelling, one writer). It exists so the
// pre-verb hoist (BootstrapEnvFromArgs) and the chat verb's own argv walk write
// the SAME variable and neither re-types its name.
func PublishChatSurfaceTurnsFlag(value string) {
	os.Setenv(ChatSurfaceTurnsEnv, value)
}

// resolveChatSurfaceTurns is the ONE resolution (Rule 2): every channel, and
// WHICH one decided; a declaration it could not understand is reported on stderr
// and the next channel decides (Rule 15).
func resolveChatSurfaceTurns() ChatSurfaceTurnsState {
	if raw := strings.TrimSpace(os.Getenv(ChatSurfaceTurnsEnv)); raw != "" {
		if n, ok := ParseChatSurfaceTurns(raw); ok {
			return ChatSurfaceTurnsState{Turns: n, Source: ChatSurfaceTurnsEnv}
		}
		reportChatSurfaceTurnsRefusal(ChatSurfaceTurnsEnv, raw, chatSurfaceTurnsDefaultHint)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// The settings file is the declaration of record: if it cannot be read,
		// say so rather than reporting "nobody asked" (Rule 15).
		fmt.Fprintf(os.Stderr, "config: %s unreadable (%v) — %s resolves to the built-in default\n",
			ResolveSettingsPath(), err, ChatSurfaceTurnsSettingsKey)
		return chatSurfaceTurnsDefaultValue()
	}
	if cfg == nil {
		return chatSurfaceTurnsDefaultValue()
	}
	raw := strings.TrimSpace(cfg.ChatSurfaceTurns)
	if raw == "" {
		return chatSurfaceTurnsDefaultValue()
	}
	if n, ok := ParseChatSurfaceTurns(raw); ok {
		return ChatSurfaceTurnsState{Turns: n, Source: ChatSurfaceTurnsSettingsKey}
	}
	reportChatSurfaceTurnsRefusal(ChatSurfaceTurnsSettingsKey, raw, chatSurfaceTurnsDefaultHint)
	return chatSurfaceTurnsDefaultValue()
}

// reportChatSurfaceTurnsRefusal is the ONE refusal sentence every channel
// prints, so an operator reading stderr learns the shape, the range and which
// channel decides instead (Rule 15).
func reportChatSurfaceTurnsRefusal(channel, raw, fallback string) {
	fmt.Fprintf(os.Stderr, "config: %s=%q %s — %s; %s\n",
		channel, raw,
		fmt.Sprintf(chatSurfaceTurnsRefusalFmt, chatSurfaceTurnsShapeClaim),
		chatSurfaceTurnsConsequence, fallback)
}

// chatSurfaceTurnsDefaultValue is the last channel: the built-in default.
func chatSurfaceTurnsDefaultValue() ChatSurfaceTurnsState {
	return ChatSurfaceTurnsState{Turns: ChatSurfaceTurnsDefault, Source: ChatSurfaceTurnsSourceDefault}
}

// ParseChatSurfaceTurns parses the knob's value shape on every channel: a whole
// number inside [ChatSurfaceTurnsFloor, ChatSurfaceTurnsCeiling]. It is the ONE
// validator, so the CLI, the env var, the settings key and the built-in default
// cannot drift into four opinions of what a bound is (Rule 2/14).
//
// It is exported because the agent package clamps a caller's max_turns through
// the SAME range: a second copy of the bounds would let the tool accept a
// max_turns no channel could have installed.
func ParseChatSurfaceTurns(raw string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	if n < ChatSurfaceTurnsFloor || n > ChatSurfaceTurnsCeiling {
		return 0, false
	}
	return n, true
}

// ChatSurfaceTurnsRowSummary is the one-line summary the help pages render for
// this knob (Rule 13), so the top-level row, the chat page's section and the
// gate that checks them name the same text and the same three channels.
//
// It is a VAR, not a const, for one reason: the three numbers in it are rendered
// FROM the named constants (strconv), so raising the default moves the sentence
// with it. A const string would have to re-type 20/2/1000 by hand — the exact
// mirroring Rule 1/13 forbids.
// It is ONE line on purpose (measured): the top-level index renders this text in
// a column, and a four-line sentence here pushed `gopi -h` past its own line
// budget — the page's rule is "an index, detail on `gopi <cmd> -h`"
// (cmd/agent/usage_test.go:topLevelUsageMaxLines). The full claim lives in
// chatSurfaceTurnsEffectClaim, which the chat page's section and the tool
// description carry.
var ChatSurfaceTurnsRowSummary = "self-driven turns the chat_surface tool may add per PERIOD " +
	"(default " + strconv.Itoa(ChatSurfaceTurnsDefault) + ", accepted " +
	strconv.Itoa(ChatSurfaceTurnsFloor) + ".." + strconv.Itoa(ChatSurfaceTurnsCeiling) + "); " +
	ChatSurfaceTurnsEnv + " or " + ChatSurfaceTurnsSettingsKey

// chatSurfaceTurnsEffectClaim states what the resolved bound DOES, in the same
// words the operator's help page and the tool description use (Rule 13).
const chatSurfaceTurnsEffectClaim = "it caps how many turns the model may type for itself in one period " +
	"(a period is the stretch between two lines a HUMAN wrote) and is the ceiling a max_turns argument is clamped to"
