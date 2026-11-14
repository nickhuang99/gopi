package config

// screenstream.go — the SCREEN-STREAM switching surface: ONE parser for the
// terminal-agnostic channel `GOPI_SCREEN_STREAM`, ONE statement of the
// precedence between that channel and this run's argv, and ONE place that says
// what a surface does when nobody said anything.
//
// WHY THIS IS NOT A CHAT CONCERN ANY MORE (epic screen-stream, 2026-11-08): the
// recording used to be switched from the chat flag namespace only
// (`GOPI_CHAT_SGR_STREAM` / `--sgr-stream`), which made it unreachable for a
// worker run — a run that paints no screen of its own but whose progress text a
// reader still wants as bytes. The name that decides it therefore had to leave
// `cmd/agent`, and the precedence had to stop being re-derived at each call site
// (Rule 13: a value shared by two surfaces has one definition; Rule 2: exactly
// ONE place reads the environment).
//
// THE THREE STATES ARE NOT TWO. "off" and "on, path unknown" are different
// answers, and a parser that collapsed them into an empty string would make
// `GOPI_SCREEN_STREAM=0` indistinguishable from `GOPI_SCREEN_STREAM=` — that
// is, the one spelling an operator uses to DECLINE an artifact would read as
// "nobody said anything" and the surface default would silently win.

import (
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// The states one channel's answer can take. They are named constants rather
// than bare words because three packages compare against them (Rule 1b).
const (
	// ScreenStreamStateUnset is "this channel said nothing".
	ScreenStreamStateUnset = "unset"
	// ScreenStreamStateOn asks for the artifact at the path the surface resolves
	// (the run's own session directory, named after the session).
	ScreenStreamStateOn = "on"
	// ScreenStreamStateOff DECLINES the artifact. No file is created, and the
	// surface announces the decision rather than staying silent (Rule 15).
	ScreenStreamStateOff = "off"
	// ScreenStreamStatePath carries an explicit destination.
	ScreenStreamStatePath = "path"
)

// The channels that can decide the answer, in precedence order. `Origin` travels
// with every resolved choice so a caller can print WHERE its decision came from
// instead of guessing (the chat's startup notice and its closing report both say
// it, and the worker's report line does too).
const (
	// ScreenStreamOriginArgv is this run's command line — the highest channel.
	ScreenStreamOriginArgv = "argv"
	// ScreenStreamOriginEnv is GOPI_SCREEN_STREAM (or its pre-rename alias).
	ScreenStreamOriginEnv = "env"
	// ScreenStreamOriginDefault is the surface's own answer when neither of the
	// two above said anything.
	ScreenStreamOriginDefault = "default"
)

// The PER-SURFACE defaults. They live here, not at the two call sites, because
// the difference between them is a fact about the surfaces rather than about
// either caller: a chat paints a screen (so the bytes exist to record, and the
// operator's ruling 2026-10-31 — 数据的不可再生性高于代码完整性 — arms it by
// default), while a worker paints nothing of its own (so an armed worker would
// manufacture the empty artifact this epic exists to forbid unless it has
// something to say).
const (
	// ScreenStreamDefaultChat arms the recording for `gopi chat`.
	ScreenStreamDefaultChat = true
	// ScreenStreamDefaultWorker declines it for `gopi worker`: the round's
	// progress text is not a screen, so recording it is an OPT-IN answer to a
	// question nobody asked.
	ScreenStreamDefaultWorker = false
)

// ScreenStreamChoice is ONE channel's answer, and — once ScreenStreamResolve has
// run — the decided answer with the channel it came from.
type ScreenStreamChoice struct {
	// State is one of the four ScreenStreamState* values.
	State string
	// Path is the explicit destination, non-empty only for
	// ScreenStreamStatePath.
	Path string
	// Origin names the channel that decided (ScreenStreamOrigin*); empty on a
	// raw per-channel reading, which has no precedence yet.
	Origin string
	// Source names the SPELLING that carried the answer — the flag word, or the
	// environment variable's name (so a caller can say "read from
	// GOPI_CHAT_SGR_STREAM, the pre-rename alias"). Empty when nothing was said.
	Source string
}

// Armed reports whether this choice asks for an artifact. It is the ONE
// predicate the recorders read, so "off" has a single definition (Rule 13).
func (c ScreenStreamChoice) Armed() bool {
	return c.State == ScreenStreamStateOn || c.State == ScreenStreamStatePath
}

// ScreenStreamFromEnv reads the environment channel, and it is the ONE reader of
// both spellings (Rule 2): the current name first, the pre-rename one only when
// the current is unset — so an operator who exported the new name for one run
// overrides a stale profile entry rather than fighting it, while a shell profile
// written before the rename keeps working. `Source` names which one answered, so
// the caller can print it instead of silently tolerating it (Rule 15).
//
// VALUES: unset/empty → Unset; any IsFalseValue (`0`/`false`/`off`/`no`/
// `disabled`) → Off; any EnvBool truthy (`1`/`true`/`yes`/`on`) → On; anything
// else IS the path.
func ScreenStreamFromEnv() ScreenStreamChoice {
	for _, name := range [...]string{cliverbs.ScreenStreamEnv, cliverbs.LegacyScreenStreamEnv} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return ScreenStreamChoiceOf(name, v)
		}
	}
	return ScreenStreamChoice{State: ScreenStreamStateUnset}
}

// ScreenStreamChoiceOf classifies ONE raw value from the named spelling. It is
// exported so a test (and a future surface with its own channel after the same
// contract) drives the classification without touching the process environment.
//
// The ORIGIN comes from the spelling that carries the answer, and it is decided
// here rather than at the call site for one reason: `Source` and `Origin` are two
// readings of the same fact (which channel spoke), so deriving one from the other
// keeps them from disagreeing. A flag word (`--screen-stream`) is argv; the two
// environment names ARE the environment channel.
func ScreenStreamChoiceOf(source, raw string) ScreenStreamChoice {
	origin := ScreenStreamOriginArgv
	if source == cliverbs.ScreenStreamEnv || source == cliverbs.LegacyScreenStreamEnv {
		origin = ScreenStreamOriginEnv
	}
	v := strings.TrimSpace(raw)
	switch {
	case v == "":
		return ScreenStreamChoice{State: ScreenStreamStateUnset, Source: source}
	case IsFalseValue(v):
		return ScreenStreamChoice{State: ScreenStreamStateOff, Origin: origin, Source: source}
	case IsTrueValue(v):
		return ScreenStreamChoice{State: ScreenStreamStateOn, Origin: origin, Source: source}
	}
	return ScreenStreamChoice{State: ScreenStreamStatePath, Path: v, Origin: origin, Source: source}
}

// ScreenStreamFromFlag is the argv channel: `--screen-stream[=PATH]` or
// `--no-screen-stream`. The two words are the two DIRECTIONS of one switch, and
// which one wins when both appear is the caller's walk (last word wins), because
// only the walk sees the order.
func ScreenStreamFromFlag(word, path string, off bool) ScreenStreamChoice {
	if off {
		return ScreenStreamChoice{State: ScreenStreamStateOff, Origin: ScreenStreamOriginArgv, Source: word}
	}
	if path == "" {
		return ScreenStreamChoice{State: ScreenStreamStateOn, Origin: ScreenStreamOriginArgv, Source: word}
	}
	return ScreenStreamChoice{State: ScreenStreamStatePath, Path: path, Origin: ScreenStreamOriginArgv, Source: word}
}

// ScreenStreamResolve is THE precedence statement, in code, for every surface:
//
//	argv  --screen-stream[=PATH] / --no-screen-stream
//	  └─ wins outright. It is this run's own words, typed one second ago.
//	env   GOPI_SCREEN_STREAM  (GOPI_CHAT_SGR_STREAM only when the current name is unset)
//	  └─ wins over the surface default, and over the alias.
//	default  the SURFACE's own answer (ScreenStreamDefaultChat / ...Worker)
//
// It is deliberately a pure function of its three arguments: the two channels
// arrive already-read, so a test can drive the whole matrix without a process
// environment, and neither caller can invent a fourth rung (a settings key would
// be added HERE, in this function, and every surface would inherit it — CONFIG.md
// §1 lists a settings key below the environment, and there is none today).
func ScreenStreamResolve(flag, env ScreenStreamChoice, surfaceDefault bool) ScreenStreamChoice {
	if flag.State != "" && flag.State != ScreenStreamStateUnset {
		return flag
	}
	if env.State != "" && env.State != ScreenStreamStateUnset {
		return env
	}
	out := ScreenStreamChoice{Origin: ScreenStreamOriginDefault}
	if surfaceDefault {
		out.State = ScreenStreamStateOn
		return out
	}
	out.State = ScreenStreamStateOff
	return out
}
