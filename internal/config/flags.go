package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// The two option spellings this parser knows. A token starting with flagPrefix
// is a long option; a token starting with shortFlagPrefix (and not flagPrefix)
// is a SHORT option, and no short option is accepted here — the whole CLI
// surface is spelled `--name` (see `gopi -h`). One constant per spelling, so the
// classifier and the refusals below cannot drift apart (Rules 1b/13).
const (
	flagPrefix      = "--"
	shortFlagPrefix = "-"
)

// unknownSingleDashOptionFmt is the refusal text for a short/single-dash token
// on the bare-run argv. It names the token, says why a dash-word is not a
// prompt, and points at the two deliberate ways to say it LITERALLY — the
// arguments are (token, token). Refusing loudly is the point (Rule 15): the
// silent alternative is what turned `-z` into a task prompt.
const unknownSingleDashOptionFmt = "unknown option %q: gopi options are spelled `--name` (see `gopi -h`), " +
	"and a single-dash token is never taken as prompt text — pass the prompt as a positional, " +
	"or `--prompt %s` to say it literally"

// unknownFlagFmt is the refusal for a `--name` that is in NO gopi table at all:
// a typo, or a flag belonging to another command. It is deliberately the ONE
// spelling of that message — reachable from both the parse loop and applyFlag's
// default arm, so a catalog row that acquired a name but no applyFlag case
// cannot fall back to a second, differently worded "unknown flag" (Rule 15).
const unknownFlagFmt = unknownFlagPrefix + "%s"

// unknownFlagPrefix opens the ONE "no such flag" refusal. It is also the
// discriminator the claim↔reality gate reads: a refusal that STARTS with it is
// claiming the name is unknown, which is FALSE for a catalog row — such a row
// may lack a flag, but its name is real and its refusal says why.
const unknownFlagPrefix = "unknown flag --"

// FlagOverrides parses a flat list of `--name value` / `--name=value` args
// (the same calling convention gopi uses everywhere — see the subcommands'
// memoryFlagSet) into a Config of explicitly-set overrides. Only flags listed
// in EnvVars (plus --settings) are accepted; anything else is an unknown-flag
// error so a typo cannot silently be ignored. Parsing never touches the
// process (no os.Exit, no env mutation) and is unit-testable.
//
// Positionals (non-flag arguments) are collected flag-value-aware: a value
// consumed by a preceding flag (--model X, --prompt "text", …) is never
// reported as a positional, so the caller cannot mistake a flag value for an
// input file.
func FlagOverrides(args []string) (*Config, error) {
	scalar, flagEnv, envFlag := flagArity()
	// The rows with no flag of their own, by env-name spelling: a lookup here is
	// what turns "unknown flag" into a refusal that names the real reason (the
	// knob exists; it has no CLI channel).
	flagless := flaglessEnvNames()

	cfg := &Config{SetEnvVars: map[string]bool{}}
	// The RUN FLAG the positionals follow, recorded in the same walk that
	// collects them: the positional-verb refusal names the flag the operator
	// actually misplaced (`--db`, not "a run flag"), and the two walks must
	// agree on which options carry a value or the name would be a guess
	// (Rules 1b/14 — see scanPrefixingRunFlag, which reads the SAME arity
	// table this parse does).
	cfg.PositionalVerbFlag = scanPrefixingRunFlag(args)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, flagPrefix) {
			// A SINGLE-dash token is an OPTION we do not know, never prompt
			// text (the `gopi -z` defect, 2026-09-21). It used to fall through
			// to the positional branch, so `-z` silently BECAME the run's
			// prompt: the ledger got a junk `[auto] -z` row whose run_cmd
			// truthfully recorded `gopi -z`, and an operator reading it asked
			// whether the task-run command had turned into `gopi -z`. The
			// pre-subcommand scanners already refuse an unknown `--bogus`
			// loudly (Rule 15, devdoc/worker-unknown-flag.md); the bare-run
			// argv was the one surface that degraded silently.
			if strings.HasPrefix(a, shortFlagPrefix) {
				return nil, fmt.Errorf(unknownSingleDashOptionFmt, a, a)
			}
			cfg.Positionals = append(cfg.Positionals, a)
			continue // positional (input file / prompt text) — not ours to reject here
		}
		name := strings.TrimPrefix(a, flagPrefix)
		var value string
		if eq := strings.Index(name, "="); eq >= 0 {
			name, value = name[:eq], name[eq+1:]
		}
		// Accept the full env var name as a flag too (e.g. --gopi-timeout-sec
		// ≡ --timeout) — the inverse of the CLIFlag→env map above. The help
		// text advertises this escape hatch, so the parser must honour it; the
		// env-named spelling routes to the SAME applyFlag case as the short
		// flag, so parsing and precedence are identical by construction.
		//
		// The escape hatch covers the FLAG-BEARING catalog rows only, and the
		// help says so. For a row with no flag of its own the name is still a
		// real knob — so it is refused by NAME, with the reason and the
		// channels that do work, never as a bare "unknown flag" (Rule 15).
		lookup := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
		if canonical, ok := envFlag[lookup]; ok {
			name = canonical
		} else if e, ok := flagless[lookup]; ok {
			return nil, flaglessEnvRefusal(name, e)
		}
		needsValue, ok := scalar[name]
		if !ok {
			return nil, fmt.Errorf(unknownFlagFmt, name)
		}
		if needsValue && value == "" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, fmt.Errorf("flag --%s requires a value", name)
			}
			value = args[i+1]
			i++
		} else if !needsValue && value == "" && i+1 < len(args) {
			// Boolean switch: --flag means true, but --flag off|false|0|no also
			// works (consume a following bare bool token if present).
			if _, ok := normalizedBoolValue(args[i+1]); ok {
				value = args[i+1]
				i++
			}
		}
		if err := applyFlag(cfg, name, value); err != nil {
			return nil, err
		}
		if env, ok := flagEnv[name]; ok {
			cfg.SetEnvVars[env] = true
			// --model must override BOTH ANTHROPIC_MODEL and DEEPSEEK_MODEL:
			// ResolveModel checks ANTHROPIC_MODEL first, and a shell-exported
			// ANTHROPIC_MODEL would otherwise silently beat the flag (the
			// env-key map only holds the LAST catalog entry per flag). Same
			// for --api-key / --base-url (Anthropic_* checked first).
			switch name {
			case "model":
				cfg.SetEnvVars["ANTHROPIC_MODEL"] = true
				cfg.SetEnvVars["DEEPSEEK_MODEL"] = true
			case "api-key":
				cfg.SetEnvVars["ANTHROPIC_AUTH_TOKEN"] = true
				cfg.SetEnvVars["DEEPSEEK_API_KEY"] = true
			case "base-url":
				cfg.SetEnvVars["ANTHROPIC_BASE_URL"] = true
				cfg.SetEnvVars["DEEPSEEK_BASE_URL"] = true
			}
		}
	}
	return cfg, nil
}

// flagArity builds the three tables the flag parser works from, out of the
// EnvVars catalog so the accepted flag surface lives in ONE place (Rule 14):
//
//	scalar   — CLI flag → does it REQUIRE a value (a boolean knob may take one
//	           as `--flag off`, a scalar knob must),
//	flagEnv  — CLI flag → canonical env key,
//	envFlag  — env var name (lowercased, dashes) → CLI flag — the inverse.
//
// Two callers share it because they must agree on ARITY, not merely on the
// flag set: FlagOverrides consumes a scalar flag's value token, and
// scanPrefixingRunFlag must skip the same token — reading one table twice is
// what keeps `--db version` (a value token, not a verb) out of the refusal.
func flagArity() (scalar map[string]bool, flagEnv map[string]string, envFlag map[string]string) {
	scalar = map[string]bool{}
	flagEnv = map[string]string{}
	envFlag = map[string]string{}
	for _, e := range EnvVars {
		if e.CLIFlag == "" {
			continue
		}
		scalar[e.CLIFlag] = !isBoolKey(e.Key)
		flagEnv[e.CLIFlag] = e.Env
		envFlag[envNameAsFlag(e.Env)] = e.CLIFlag
	}
	scalar["settings"] = true // --settings PATH
	scalar["home"] = true     // --home PATH (gopi data root; GOPI_HOME)
	scalar["prompt"] = true   // --prompt TEXT (task instruction; positionals = files)
	return scalar, flagEnv, envFlag
}

// flaglessRefusalStem is the sentence inside the refusal that names the reason;
// one constant, so the gate asserting "the refusal names why" reads the same
// text the operator reads (Rule 13).
const flaglessRefusalStem = "a real GoPi knob with NO CLI flag of its own"

// flaglessEnvRefusalFmt is the refusal for the env-named spelling of a catalog
// row that has NO CLI flag of its own — the rows `gopi --help-env` renders with
// an em dash in the flag column (measured 2026-09-27: every one of the 44 such
// rows answered `gopi: unknown flag`). The name is a REAL knob, correctly
// spelled, and simply has no flag channel, so the generic "unknown flag" would
// be false in its first word and useless in its second: it sends the operator
// hunting a typo that is not there. The arguments are (spelling as typed, env
// var, the reach phrase), the last built by flaglessEnvRefusal so the
// settings-key half is named only when one exists.
const flaglessEnvRefusalFmt = "--%s names %s — " + flaglessRefusalStem +
	" (the flag column of `gopi --help-env` shows an em dash for it). Reach it through %s. " +
	"The env-name-as-flag spelling covers the flag-bearing rows only."

// AgentFlagToken reports whether a token is a flag the TOP-LEVEL parser accepts
// (`--tmpdir`, `--film-terminal`, …) and, when it is, the catalog row's ENV name
// so a subcommand can point the caller at the channel that DOES work.
//
// WHY IT EXISTS. A knob may be reachable at the top level (`gopi --film-terminal
// x chat …`) and ALSO advertised by a subcommand's own page as a post-verb flag
// (`gopi chat --film-terminal x`). A subcommand that reads only its own list
// answers such a word with "unknown flag" — a sentence whose first word is false
// (the name IS a gopi flag) and whose second sends the operator hunting a typo
// that does not exist. The flagless rows already have that treatment
// (flaglessEnvRefusal); this is the same fact for a row that DOES have a flag,
// reached from a verb that does not parse it.
//
// It reads the SAME arity table the parser builds (Rule 13/14), so the answer
// cannot drift from what the top-level parser accepts.
//
// THE SECOND RETURN IS THE VALUE SLOT, and it exists because a hint that told
// every caller to write `--flag VALUE` was WRONG for the boolean rows: measured
// 2026-10-27, `gopi chat --memory-prefetch` answered "it belongs BEFORE the
// subcommand: `gopi --memory-prefetch VALUE chat …`", and typing that gives
// `VALUE` as the flag's value. The hint has to know which shape it is naming,
// so the arity table's own answer rides out with it instead of being guessed by
// the caller.
func AgentFlagToken(token string) (env string, takesValue, ok bool) {
	if !strings.HasPrefix(token, flagPrefix) {
		return "", false, false
	}
	name := strings.TrimPrefix(token, flagPrefix)
	if name == "" || strings.HasPrefix(name, shortFlagPrefix) {
		return "", false, false
	}
	arity, flagEnv, _ := flagArity()
	scalar, known := arity[name]
	if !known {
		// A boolean knob's row is in flagEnv but NOT in arity as a scalar… except
		// the table stores `false` for it, so `known` is what decides membership.
		// A name in neither map is not a top-level flag at all.
		if _, inEnv := flagEnv[name]; !inEnv {
			return "", false, false
		}
	}
	// A flag with no catalog row (`--settings`, `--home`, `--prompt`) is still a
	// real top-level flag; it simply has no env channel, and the empty string is
	// how that is reported.
	return flagEnv[name], scalar, true
}

// flaglessEnvNames maps the env-name spelling of every catalog row that has NO
// CLI flag (`gopi --help-env` prints an em dash there) to its catalog entry.
// Derived from the same EnvVars table the parser and the help render from, so
// the set the parser refuses with a reason and the set the help marks with an em
// dash cannot drift apart — the claim↔reality test asserts the three-way
// equality (Rules 13/14).
func flaglessEnvNames() map[string]envKey {
	m := map[string]envKey{}
	for _, e := range EnvVars {
		if e.CLIFlag == "" {
			m[envNameAsFlag(e.Env)] = e
		}
	}
	return m
}

// flaglessEnvRefusal builds the "no flag of its own" error for one flagless
// catalog row. The reach phrase names the channels that DO work, and names the
// settings key only when the row actually has one: the settings-file path sits
// in the catalog with the path-locator key, which is not an editable key —
// advertising it as one would be the same defect one layer down.
func flaglessEnvRefusal(typed string, e envKey) error {
	reach := fmt.Sprintf("its environment variable %s", e.Env)
	switch e.Key {
	case "", settingsPathKey:
		if e.Key == settingsPathKey {
			reach += " (it is the settings file path itself — see --settings)"
		}
	default:
		reach += fmt.Sprintf(" or its settings.json key %q", e.Key)
	}
	return fmt.Errorf(flaglessEnvRefusalFmt, typed, e.Env, reach)
}

// scanPrefixingRunFlag returns the option token rendered as the operator
// spelled it (`--db`, `--gopi-timeout-sec`) that the positionals FOLLOW — or
// "" when no option precedes them. The run path's refusal needs it: a message
// that names the misplaced flag and the order that works ("`--db P version` →
// put the verb first") is actionable, while "you passed a flag" is not, and
// the spelling must be the one typed — the name is taken from the very table
// the parser binds flags through, never re-derived from a second list.
//
// The walk mirrors FlagOverrides' consumption rules exactly (same arity table
// from flagArity, same `--name=value` split, same short-option arm), because
// it must return the same positional boundary the parser computes: a scalar
// option's VALUE is not a positional, so `gopi --db version` puts the verb at
// argv[3], not argv[2]. O(n), a few branches, once per run.
func scanPrefixingRunFlag(args []string) string {
	scalar, _, envFlag := flagArity()
	lastOption := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, flagPrefix) {
			// The first non-option token: every positional starts here, so no
			// LATER option can precede them — the walk is over.
			return lastOption
		}
		name := strings.TrimPrefix(a, flagPrefix)
		hasInlineValue := false
		if eq := strings.Index(name, "="); eq >= 0 {
			name, hasInlineValue = name[:eq], true
		}
		if canonical, ok := envFlag[strings.ToLower(strings.ReplaceAll(name, "_", "-"))]; ok {
			name = canonical
		}
		lastOption = flagPrefix + name
		if hasInlineValue {
			continue // `--name=value`: the value is inside the token
		}
		switch needsValue, known := scalar[name]; {
		case !known:
			// An unknown option — FlagOverrides refuses the line outright a
			// moment later (Rule 15), so nothing is consumed here: guessing its
			// arity could swallow the verb and hand back a name for a flag that
			// was never accepted.
		case needsValue:
			i++ // the option's own value token — not a positional, not the verb
		default:
			// A boolean knob may carry an explicit value token
			// (`--raw-log off`); consume it exactly as the parser does.
			if i+1 < len(args) {
				if _, ok := normalizedBoolValue(args[i+1]); ok {
					i++
				}
			}
		}
	}
	return lastOption
}

// envNameAsFlag renders an env var name the way help advertises it as a
// flag: GOPI_TIMEOUT_SEC → "gopi-timeout-sec" (lowercase, underscores →
// dashes). One function so the help text and the parser agree on spelling.
func envNameAsFlag(env string) string {
	return strings.ToLower(strings.ReplaceAll(env, "_", "-"))
}

// expandFileRefValue is the flags.go wrapper around ExpandFileRef (same
// package — exists so applyFlag's call sites read uniformly and so a future
// cross-package move keeps one name).
func expandFileRefValue(value string) (string, error) {
	return ExpandFileRef(value)
}

// applyFlag writes one parsed flag value into cfg.
func applyFlag(cfg *Config, flag, value string) error {
	switch flag {
	case "settings":
		cfg.SettingsPath = value
	case "home":
		cfg.GopiHomePath = value
	case "prompt":
		// '@path' file reference: multi-line prompts with special characters
		// are painful to quote on the command line — allow --prompt @file.
		expanded, err := expandFileRefValue(value)
		if err != nil {
			return err
		}
		cfg.Prompt = expanded
	case "api-key":
		cfg.APIKey = value
	case "base-url":
		cfg.BaseURL = value
	case "model":
		cfg.Model = value
	case "vision-model":
		cfg.VisionModel = value
	case "vision-base-url":
		cfg.VisionBaseURL = value
	case OutputLanguageFlagName:
		// The reply-language knob (--output-language zh-CN). The value is
		// carried on the Config AND published into GOPI_OUTPUT_LANGUAGE before
		// the subcommand chain (BootstrapEnvFromArgs), which is what makes the
		// flag reach `gopi chat` and a task run: neither goes through the
		// agent-path config block's ApplyEnv. It is a plain string flag — no
		// validation here, because the ONE validator lives beside the
		// resolution (config.isOutputLanguageTag), so the CLI, the env var and
		// the settings key cannot drift into three opinions of what a language
		// tag is.
		cfg.OutputLanguage = value
	case AmbiguousWidthFlagName:
		// How an East Asian Width AMBIGUOUS rune is measured (--ambiguous-width
		// wide|narrow). A plain string flag — no validation here, because the ONE
		// reader (ResolveAmbiguousWidthMode) owns both the accepted vocabulary and
		// the refusal for anything else, so the CLI, the env var and the settings
		// key cannot drift into three opinions of what a width is (Rules 2/14/15).
		cfg.AmbiguousWidth = value
	case ChatIdealGeometryFlagName:
		// The chat surface's ideal geometry (--chat-ideal-geometry 117x26). A
		// plain string flag — no validation here, because the ONE validator
		// lives beside the resolution (parseChatIdealGeometry), so the CLI, the
		// env var, the settings key and the built-in default cannot drift into
		// four opinions of what a geometry is (Rules 2/14).
		cfg.ChatIdealGeometry = value
	case ChatSurfaceTurnsFlagName:
		// The chat_surface tool's self-driven turn budget
		// (--chat-surface-turns 30). A plain string flag — no validation here,
		// because the ONE validator lives beside the resolution
		// (config.ParseChatSurfaceTurns), so the CLI, the env var, the settings
		// key and the built-in default cannot drift into four opinions of what
		// a bound is (Rules 2/14). An unusable value is REPORTED on stderr and
		// the next channel decides (Rule 15) — never a silent default.
		cfg.ChatSurfaceTurns = value
	case "reasoning-effort":
		cfg.ReasoningEffort = value
	case "raw-log":
		cfg.RawLog = value
	case "raw-log-dir":
		cfg.RawLogDir = value
	case "websearch-scripts-dir":
		cfg.WebSearchScripts = value
	case "db":
		cfg.MemoryDB = value
	case TmpDirFlagName:
		// The temp workspace: the flag's value is carried in the Config so the
		// publisher can rank it above every env channel (ResolveTmpDirConfigured
		// — the one place the precedence is spelled). An explicitly empty value
		// therefore means "no flag"; the default then applies.
		cfg.TmpDir = value
	case SandboxFlagName:
		// The sandbox root: carried on the Config for the same reason as
		// tmpdir — ResolveSandboxRoot ranks the flag above the env and the
		// settings key, and that ranking is spelled in ONE place. An
		// explicitly empty value means "no flag"; the default then applies.
		cfg.SandboxDir = value
	case ScratchGCStaleMinFlagName:
		// The sweep's STALE WINDOW, kept on the Config exactly like tmpdir (and
		// for the same reason): the value's publication needs to rank the flag
		// ABOVE the operator's exported environment variable, which only the
		// publisher (PublishScratchGCStaleMin) can decide — so the flag parser
		// records the value and does NOT touch the environment (a second writer
		// here would be a second source of truth, Rule 2/13).
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n <= 0 {
			// A typo must fail loudly (Rule 15): "collect nothing" is expressed
			// by the OFF switch (--scratch-gc=false / the cadence env =0), never
			// by a threshold that can never be exceeded.
			return fmt.Errorf("flag --%s requires a positive integer number of minutes, got %q",
				ScratchGCStaleMinFlagName, value)
		}
		cfg.ScratchGCStaleMin = n
	case "thinking-budget":
		return setInt(&cfg.ThinkingBudget, flag, value)
	case "max-tokens":
		return setInt(&cfg.MaxTokens, flag, value)
	case "context-window":
		return setInt(&cfg.ContextWindow, flag, value)
	case "tool-timeout":
		return setInt(&cfg.ToolTimeoutSec, flag, value)
	case "bash-timeout":
		return setInt(&cfg.BashTimeoutSec, flag, value)
	case "bash-idle":
		return setInt(&cfg.BashIdleSec, flag, value)
	case "timeout":
		n, err := atoi(flag, value)
		if err != nil {
			return err
		}
		cfg.TimeoutSec = n
	case "thinking":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		if b {
			mode := ThinkingEnabled
			cfg.Thinking = &mode
		} else {
			mode := ThinkingDisabled
			cfg.Thinking = &mode
		}
	case "hide-thinking":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.HideThinking = b
	case "keep-thinking":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.KeepThinking = b
	case "debug":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.Debug = b
	case "allow-dangerous":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.AllowDangerous = b
	case "no-enforce-memory":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.NoEnforceMemory = b
	case "no-enforce-memory-consult":
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.NoEnforceMemoryConsult = b
	case MemoryPrefetchFlagName:
		// The typing-time prefetch switch (--memory-prefetch). Like every other
		// boolean knob the parsed value lands on the Config; unlike a
		// settings-only knob its ENV representation is written by the hoist in
		// BootstrapEnvFromArgs, because `gopi --memory-prefetch chat` must reach a
		// verb that returns from main() before the agent-path config block (the
		// --output-language precedent).
		b, err := atob(flag, value)
		if err != nil {
			return err
		}
		cfg.MemoryPrefetch = &b
	case "scratch-gc": // The flag's VALUE is a cadence in minutes ("--scratch-gc 30"), but
		// the knob has exactly ONE representation: the env the cadence reader
		// consumes (scratch.GCIntervalEnvName). A numeric 0 or a boolean off
		// spelling disables the resident sweep. Storing a number in
		// cfg.ScratchGC would create a second source of truth for the same
		// knob (Rule 2/13).
		cfg.ScratchGC = nil
		cfg.SetEnvVars[scratch.GCIntervalEnvName] = true
		if b, berr := atob(flag, value); berr == nil && !b {
			os.Setenv(scratch.GCIntervalEnvName, "0")
			break
		}
		os.Setenv(scratch.GCIntervalEnvName, value)
	case FilmTerminalFlagName:
		// The filmed terminal's NAME — the third channel of the knob whose other
		// two are GOPI_FILM_TERMINAL and the settings key film_terminal. It is
		// stored as given and resolved by ONE function (shot.ResolveFilmTerminal),
		// which validates it against the registry and refuses an unknown name with
		// the usable list; validating here would put a second, weaker opinion of
		// what a terminal is called in the parser (Rule 13/14).
		cfg.FilmTerminal = value
	default:
		// A catalog row whose CLIFlag exists but whose applyFlag case does not:
		// the name came from the accepted-flag table (so the caller's error must
		// not read "unknown"), and the miss is a wiring gap. Same wording as the
		// parse loop's refusal so there is exactly ONE text for "no such flag".
		return fmt.Errorf(unknownFlagFmt, flag)
	}
	return nil
}

func setInt(dst *int, flag, value string) error {
	n, err := atoi(flag, value)
	if err != nil {
		return err
	}
	*dst = n
	return nil
}

func atoi(flag, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid value %q for --%s: want an integer", value, flag)
	}
	return n, nil
}

func atof(flag, value string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q for --%s: want a number", value, flag)
	}
	return f, nil
}

func atob(flag, value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	b, ok := normalizedBoolValue(value)
	if !ok {
		return false, fmt.Errorf("invalid value %q for --%s: want true/false/1/0/on/off", value, flag)
	}
	return b, nil
}

// CatalogFlagArity answers one question about a SINGLE flag name for a face
// that is not the run path: is this a flag the TOP-LEVEL parser accepts, and
// if so does it require a value?
//
// WHY A PREDICATE AND NOT A LIST. A sibling face (`gopi --session <id>
// "<prompt>"`, cmd/agent/cli_resume_session_flag.go) needs to let the
// configuration knobs through — the same knobs the bare-prompt run path accepts,
// because the two faces are the same shape — while still refusing a stray,
// non-catalog option by name (Rule 15). The ONLY correct source for that
// question is the arity table FlagOverrides itself parses with, built from the
// EnvVars catalog (Rule 13/14: one source, referenced — never re-typed). A list
// written in cmd/agent would be a second answer that drifts the moment a knob is
// added, and it would be wrong in a way nobody sees: the new knob would work on
// the run path and be refused by the resume face.
//
// The token spelled `--name` or `--name=value` is accepted; the NAME is what is
// looked up (the env-name-as-flag spelling is normalised exactly as the parser
// normalises it, so `--gopi-timeout-sec` answers like `--timeout`). A
// single-dash token, an empty name and an unknown name all answer false — the
// caller names the offending word back.
func CatalogFlagArity(token string) (needsValue bool, known bool) {
	if !strings.HasPrefix(token, flagPrefix) {
		return false, false
	}
	name := strings.TrimPrefix(token, flagPrefix)
	if name == "" || strings.HasPrefix(name, shortFlagPrefix) {
		return false, false
	}
	if eq := strings.Index(name, "="); eq >= 0 {
		name = name[:eq]
	}
	scalar, _, envFlag := flagArity()
	if canonical, ok := envFlag[strings.ToLower(strings.ReplaceAll(name, "_", "-"))]; ok {
		name = canonical
	}
	needsValue, ok := scalar[name]
	return needsValue, ok
}

// IsCatalogBoolValue reports whether a bare token is a boolean SPELLING the
// flag parser accepts for a boolean knob ("off"/"false"/"0"/"no" and their
// true-side twins). It is the SAME normalizedBoolValue the parser itself peeks
// with, exported so a face that CONSUMES a catalog flag picks the same operand
// the parser would — a second spelling table here would consume "false" on one
// face and leave it in the prompt on the other.
func IsCatalogBoolValue(token string) bool {
	_, ok := normalizedBoolValue(token)
	return ok
}

// CatalogFlagValueTaken reports whether a value-taking flag spelled inline as
// `--name=value` already carries its operand. It exists so a consumer that
// CONSUMES a catalog flag (rather than merely rejecting it) splits the token the
// way FlagOverrides does — `--name VALUE` and `--name=VALUE` are the same flag
// with the same value, and a consumer that only understood the first form would
// silently read the second as a flag with no value.
func CatalogFlagValueTaken(token string) (value string, inline bool) {
	name := strings.TrimPrefix(token, flagPrefix)
	if eq := strings.Index(name, "="); eq >= 0 {
		return name[eq+1:], true
	}
	return "", false
}
