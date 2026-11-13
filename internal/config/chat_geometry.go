package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// chat_geometry — the chat surface's IDEAL GEOMETRY (one knob, three channels,
// one resolution).
//
// WHY IT EXISTS. The chat surface draws three pieces of chrome that a narrower
// terminal cannot carry: the startup screen's boxed mark with its side panel of
// tips (chat_banner.go), the reserved layout's editing line + status row
// (lineedit's region), and the info/status bar's column list. They were tuned at
// 117x26 — the geometry every live measurement in this repo was taken at — but
// that number lived in TWO places as a bare literal (the replay/beat flag
// defaults), so a normal chat on a wider terminal had no way to say "draw this
// screen at the size it was designed for", and the two paths could drift into
// two different ideas of the designed size.
//
// So this is deliberately a KNOB, not a framework feature: the geometry the
// surface BUILDS ITSELF for is now a declared value, and the paths that draw the
// surface (a normal `gopi chat`, the replay/beat filmer) resolve it through the
// ONE function below (Rule 2/14).
//
// CHANNELS, highest first — the same three the rest of internal/config uses:
//
//	rank 1  CLI flag `--chat-ideal-geometry COLSxROWS`  (hoisted into the env
//	                                                     BEFORE the subcommand
//	                                                     chain, so `gopi chat`
//	                                                     and the replay path
//	                                                     both see it)
//	rank 2  env  GOPI_CHAT_IDEAL_GEOMETRY=COLSxROWS
//	rank 3  settings.json "chat_ideal_geometry": "COLSxROWS"
//	rank 4  the built-in default (chatIdealGeometryDefault)
//
// The flag and the env channel are ONE predicate at runtime (the flag is
// published into the env by BootstrapEnvFromArgs — and by the chat verb's own
// walk — because `gopi chat` returns from main() before the agent-path config
// block). That is not a lost precedence rung: the flag writes the variable
// UNCONDITIONALLY, so a flag beats an operator's export while an unrelated
// launch leaves the export alone.
//
// Rule 15 discipline: a value that is not a `COLSxROWS` pair is REPORTED on
// stderr and the NEXT channel decides — never silently read as unset, because
// "the operator asked for a geometry I did not understand" and "nobody asked"
// must not look alike.

// ChatIdealGeometrySettingsKey is the settings.json key of the ideal-geometry
// knob (whose value is a COLSxROWS pair, e.g. the built-in default below).
// Exported so the catalog row, the help rows and the report name the key they
// read instead of re-typing it.
const ChatIdealGeometrySettingsKey = "chat_ideal_geometry"

// ChatIdealGeometryEnv is the env override of the same knob
// (GOPI_CHAT_IDEAL_GEOMETRY=117x26) — and the ONE representation the CLI flag is
// published as, so exactly one reader below sees all three channels (Rule 2).
const ChatIdealGeometryEnv = "GOPI_CHAT_IDEAL_GEOMETRY"

// ChatIdealGeometryFlagName is the CLI spelling (`--chat-ideal-geometry 117x26`).
// It is the bare name (no dashes) because the catalog, the arity table, the help
// rows and the hoist all spell the prefix once (flagPrefix) — the same shape
// OutputLanguageFlagName has.
const ChatIdealGeometryFlagName = "chat-ideal-geometry"

// chatIdealGeometryDefault is the built-in ideal geometry — the ONE place the
// designed size is written down (Rule 13/14). It is a string because the knob's
// value IS a string on every channel, so the default needs no second
// representation to stay equal to what an operator would type.
//
// The numbers are not chosen here: 117x26 is the geometry every measurement of
// this surface was taken at (the operator's own terminal, the pty every dump test
// allocates, the film's --cols/--rows). Before this constant existed the same
// pair was typed in three places.
const chatIdealGeometryDefault = "117x26"

// ChatIdealGeometryDefault is the built-in default as a STRING — the same one
// value, exported for the surfaces that must NAME it (the help page's row, the
// catalog entry, the degradation hint) without typing a second copy of the
// size. It is the constant above, not a parallel definition (Rule 13).
const ChatIdealGeometryDefault = chatIdealGeometryDefault

// ChatIdealGeometryDimSep is the separator between the two dimensions. It is a
// name, not an inline literal, because the parser, the refusal text and the help
// row must all mean the same character (Rule 1b).
const ChatIdealGeometryDimSep = "x"

// The bounds. They are named (Rule 1) because each is a decision with a failure
// mode behind it: a zero/negative value is not a geometry at all, and an
// oversized one would ask the layout for a canvas no terminal has and no
// measurement covers.
const (
	chatIdealGeometryMinDim  = 1
	chatIdealGeometryMaxCols = 1000
	chatIdealGeometryMaxRows = 500
)

// The THREE source labels a resolution reports: which channel decided. They are
// the channel spellings themselves (one definition each, Rules 1b/13/14), not a
// second vocabulary a reader would have to map back.
const (
	// ChatIdealGeometrySourceDefault is reported when nobody declared a value and
	// the built-in default decided.
	ChatIdealGeometrySourceDefault = "default"
)

// The refusal texts. They are constants for the usual reason: the parser's
// message and the doc's claim must not be two spellings of one rule (Rule 13).
const (
	chatIdealGeometryShapeClaim  = "a COLS" + ChatIdealGeometryDimSep + "ROWS pair of positive integers"
	chatIdealGeometryRefusalFmt  = "is not %s"
	chatIdealGeometryConsequence = "the next channel decides"
	chatIdealGeometryDefaultHint = "the built-in default (" + chatIdealGeometryDefault + ") decides"
)

// ChatIdealState is the RESOLVED ideal geometry plus the channel that decided
// it. The source half exists for the same reason the geometry exists: "the screen
// is 117 wide because you asked for it" and "because nobody asked" are two
// different facts about one number, and a caller that reports the geometry must
// be able to say which.
type ChatIdealState struct {
	Cols   int
	Rows   int
	Source string
}

// ChatIdealGeometry resolves the ideal geometry through the ONE resolution
// below (Rule 2). Every caller — the normal chat surface, the replay/beat
// filmer — calls THIS, so the two paths cannot hold two ideas of the designed
// size.
func ChatIdealGeometry() ChatIdealState {
	return resolveChatIdealGeometry()
}

// ChatIdealCols / ChatIdealRows are the geometry as plain accessors, for the
// callers that only need one number (a flag default, a comparison).
func ChatIdealCols() int { return ChatIdealGeometry().Cols }

// ChatIdealRows reports the ideal row count.
func ChatIdealRows() int { return ChatIdealGeometry().Rows }

// PublishChatIdealGeometryFlag writes the flag's value into the knob's ONE env
// representation.
//
// It exists so the flag has exactly ONE publisher (Rule 13/14): both callers —
// BootstrapEnvFromArgs (the pre-subcommand hoist, which is what makes
// `gopi --chat-ideal-geometry 100x30 chat` reach chat) and the chat verb's own
// argv walk (the spelling an operator types after the verb) — write the SAME
// variable, and neither re-types its name.
func PublishChatIdealGeometryFlag(value string) {
	os.Setenv(ChatIdealGeometryEnv, value)
}

// resolveChatIdealGeometry is the ONE resolution (Rule 2): it reads every
// channel and answers WHICH one decided, reporting a declaration it could not
// understand on stderr and letting the next channel decide (Rule 15).
func resolveChatIdealGeometry() ChatIdealState {
	if raw := strings.TrimSpace(os.Getenv(ChatIdealGeometryEnv)); raw != "" {
		if cols, rows, ok := parseChatIdealGeometry(raw); ok {
			return ChatIdealState{Cols: cols, Rows: rows, Source: ChatIdealGeometryEnv}
		}
		fmt.Fprintf(os.Stderr, "config: %s=%q %s — %s; %s\n",
			ChatIdealGeometryEnv, raw,
			fmt.Sprintf(chatIdealGeometryRefusalFmt, chatIdealGeometryShapeClaim),
			chatIdealGeometryConsequence, chatIdealGeometryDefaultHint)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// The settings file is the declaration of record: if it cannot be read,
		// say so rather than reporting "nobody asked" (Rule 15).
		fmt.Fprintf(os.Stderr, "config: %s unreadable (%v) — %s resolves to the built-in default\n",
			ResolveSettingsPath(), err, ChatIdealGeometrySettingsKey)
		return chatIdealGeometryDefaultValue()
	}
	if cfg == nil {
		return chatIdealGeometryDefaultValue()
	}
	raw := strings.TrimSpace(cfg.ChatIdealGeometry)
	if raw == "" {
		return chatIdealGeometryDefaultValue()
	}
	if cols, rows, ok := parseChatIdealGeometry(raw); ok {
		return ChatIdealState{Cols: cols, Rows: rows, Source: ChatIdealGeometrySettingsKey}
	}
	fmt.Fprintf(os.Stderr, "config: %s=%q %s — the built-in default (%s) decides\n",
		ChatIdealGeometrySettingsKey, raw,
		fmt.Sprintf(chatIdealGeometryRefusalFmt, chatIdealGeometryShapeClaim),
		chatIdealGeometryDefault)
	return chatIdealGeometryDefaultValue()
}

// chatIdealGeometryDefaultValue is the last channel: the built-in default,
// parsed through the SAME parser every other channel goes through, so a typo in
// the constant cannot produce a geometry no channel could express.
func chatIdealGeometryDefaultValue() ChatIdealState {
	cols, rows, ok := parseChatIdealGeometry(chatIdealGeometryDefault)
	if !ok {
		// A default that does not parse is a programming error with no sensible
		// fallback: report it and answer the last legal pair rather than a zero
		// geometry a caller would divide by.
		fmt.Fprintf(os.Stderr, "config: built-in %s default %q %s — using %d%c%d\n",
			ChatIdealGeometrySettingsKey, chatIdealGeometryDefault,
			fmt.Sprintf(chatIdealGeometryRefusalFmt, chatIdealGeometryShapeClaim),
			chatIdealGeometryMinDim, ChatIdealGeometryDimSep[0], chatIdealGeometryMinDim)
		cols, rows = chatIdealGeometryMinDim, chatIdealGeometryMinDim
	}
	return ChatIdealState{Cols: cols, Rows: rows, Source: ChatIdealGeometrySourceDefault}
}

// parseChatIdealGeometry parses "COLSxROWS" — the knob's value shape on every
// channel. It is the ONE validator, so the CLI, the env var, the settings key
// and the built-in default cannot drift into four opinions of what a geometry is
// (Rule 2/14).
func parseChatIdealGeometry(raw string) (cols, rows int, ok bool) {
	parts := strings.Split(raw, ChatIdealGeometryDimSep)
	if len(parts) != 2 {
		return 0, 0, false
	}
	cols, cerr := strconv.Atoi(strings.TrimSpace(parts[0]))
	rows, rerr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if cerr != nil || rerr != nil {
		return 0, 0, false
	}
	if cols < chatIdealGeometryMinDim || cols > chatIdealGeometryMaxCols {
		return 0, 0, false
	}
	if rows < chatIdealGeometryMinDim || rows > chatIdealGeometryMaxRows {
		return 0, 0, false
	}
	return cols, rows, true
}

// ChatIdealGeometryRowSummary is the one-line summary the help pages render for
// this knob. It is a constant (Rule 13) so the top-level row, the chat page's
// section and the gate that checks them name the same text and the same three
// channels.
const ChatIdealGeometryRowSummary = "the chat surface's ideal geometry as " +
	"COLS" + ChatIdealGeometryDimSep + "ROWS (default " + chatIdealGeometryDefault + ", " +
	"the size its chrome is drawn for), via " + ChatIdealGeometryEnv + " or " +
	ChatIdealGeometrySettingsKey + "; " + chatIdealGeometryEffectClaim

// chatIdealGeometryEffectClaim states what the resolved geometry DOES, in the
// same words on the help page and in the degradation notice (Rule 13).
//
// It is deliberately about the WIDTH. Row count is never imposed on a terminal:
// the status row is anchored to the screen's last row, so a canvas that claimed
// fewer rows than the terminal has would park the status line in the middle of the
// screen — the "composer is not at the bottom" defect the layout's own comments
// record. So a terminal wider than the ideal draws the canvas at the ideal width
// (the remainder is left blank) and a terminal NARROWER than the ideal keeps its
// own width, with the loss announced.
const chatIdealGeometryEffectClaim = "a terminal at least this wide draws the chat canvas at exactly this width, " +
	"and a narrower one keeps its own width and says what it lost"
