package config

// film_terminal.go — the FILMED-TERMINAL knob's NAME, in exactly one place.
//
// WHY IT IS HERE AND NOT IN internal/shot. The value is a CONFIGURATION channel
// (flag / env / settings key), and the ONE resolver that reads it lives in
// internal/shot (shot.ResolveFilmTerminal), because a terminal's launch shape and
// its registry are that package's business. The names themselves, though, are
// shared: the catalog row in settings.go, the flag parser in flags.go and the
// resolver in internal/shot must all mean the SAME three strings, and
// internal/shot imports internal/config (never the other way round) — so this is
// the one package every consumer can import, which is what Rule 14 asks for. The
// alternative was two definitions of "GOPI_FILM_TERMINAL", and a knob whose env
// name is spelled twice is a knob that quietly stops working in one of its two
// halves.
//
// The VALUE SET is deliberately NOT here: which terminals exist is the registry's
// fact (internal/shot.FilmTerminalSpecByName), and duplicating that list in the
// config package would give a name two sources of truth — the defect this
// arrangement exists to avoid.

import "os"

// PublishFilmTerminalFlag writes the knob's ONE env representation from the CLI
// channel, exactly as PublishChatIdealGeometryFlag does for its own knob: the
// top-level hoist (BootstrapEnvFromArgs) is the writer for the PRE-verb spelling
// `gopi --film-terminal NAME chat …`, and the chat verb's own parser is the
// writer for the POST-verb spelling — both through THIS function, so the flag has
// one spelling and one writer (Rule 13/14) and neither can invent a second env
// name. A published value is not validated here: the registry's resolver is the
// one authority on what a terminal is called (shot.ResolveFilmTerminal).
func PublishFilmTerminalFlag(value string) { os.Setenv(FilmTerminalEnv, value) }

const (
	// FilmTerminalEnv is the knob's env channel (GOPI_FILM_TERMINAL).
	FilmTerminalEnv = "GOPI_FILM_TERMINAL"
	// FilmTerminalSettingsKey is the knob's settings.json key ("film_terminal").
	FilmTerminalSettingsKey = "film_terminal"
	// FilmTerminalFlagName is the CLI spelling WITHOUT dashes ("film-terminal"),
	// the bare form every other catalog row uses; the parser composes the
	// accepted `--film-terminal NAME` / `--film-terminal=NAME` spellings from it.
	FilmTerminalFlagName = "film-terminal"
	// FilmTerminalFlag is the FULL spelling ("--film-terminal"), composed here
	// from the same dash prefix the rest of the CLI uses so a consumer does not
	// write a second one (Rule 1b/13).
	FilmTerminalFlag = flagPrefix + FilmTerminalFlagName
)
