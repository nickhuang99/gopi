package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// PromptEnvVar is the environment channel that carries an explicit prompt
// (`--prompt` is published into it by ApplyEnv; the settings.json "prompt"
// key feeds the same var). Named here once (Rule 1b) because THREE surfaces
// must agree on the spelling: the publisher (env.go's setStr), the settings
// catalog row (settings.go), and every reader — cmd/agent/main.go's readInput
// and the run path's verb guard, where an explicit prompt makes a same-word
// positional an input FILE rather than a mistyped command.
const PromptEnvVar = "GOPI_PROMPT"

// promptSource records WHICH channel supplied the prompt in force for an
// invocation. The value alone is not enough to answer the guard's question:
// "the prompt is the string version" and "the prompt is X, and it got here
// because a probe exported it for somebody else" are the same string and
// opposite answers (task 01a0de86, 2026-09-26).
type promptSource int

const (
	// promptSourceNone — no prompt channel supplied anything.
	promptSourceNone promptSource = iota
	// promptSourceFlag — the command line said so (`--prompt X`, `--prompt @f`).
	promptSourceFlag
	// promptSourceSettings — the profile's settings.json "prompt" key said so.
	promptSourceSettings
	// promptSourceEnv — an environment variable carried it. It is the LAST
	// resort because it is the only channel whose provenance is unknowable at
	// read time: an inherited GOPI_PROMPT says "a prompt applied to some
	// invocation", never "a prompt applies to THIS one".
	promptSourceEnv
)

// LoadMerged resolves the configuration IN FORCE for this invocation, the
// same way the run path resolves it: flags > env > settings.json > defaults.
//
// It exists for a caller that needs the prompt a run will ACTUALLY use, with
// its PROVENANCE, rather than one channel of it — the run path's verb guard is
// that caller (see promptInForce). It is the read-only twin of the resolution
// cmd/agent/main.go performs before readInput (same three layers, same
// precedence, same settings-file loader), so a caller reaching it EARLY —
// before those steps have run — gets the answer the run path would.
//
// It is also the only place that resolution can be done compatibly: the guard
// used to ask `os.Getenv(PromptEnvVar) != ""`, i.e. "does ANY channel mention a
// prompt", and a lower-precedence channel a probe left behind answered a
// question about a DIFFERENT invocation.
//
// The ENV layer is read through the settings catalog's own env mapping
// (EnvVars), never re-spelled here; the FLAG layer is field-wise because
// ApplyEnv(base, true) cannot serve this call — it always writes GOPI_HOME into
// the process environment (setStr, not gated by SetEnvVars) and a resolution
// that only READS has no business exporting anything. Only the knobs the
// guard's answer depends on are taken from each layer: the prompt, plus the two
// path knobs that steer the settings read.
//
// Degradation is deliberate and cannot mask a run-path error: a missing
// settings file yields that layer's defaults (FromSettingsFile), an unreadable
// or malformed one is ignored here — the guard must still be able to REFUSE —
// while the run path re-resolves through its own steps and still fails LOUDLY
// on a malformed settings file. An unparseable FLAG line is the one error
// propagated: FlagOverrides' refusal is the same one the run path will print,
// and acting on a half-parsed argv is exactly the guess this guard exists to
// avoid.
func LoadMerged(args []string) (*Config, error) {
	flags, err := FlagOverrides(args)
	if err != nil {
		return nil, err
	}
	// The two path knobs follow main.go's own resolution, INCLUDING its
	// ordering: `--home PATH` redirects the settings read, and by the time the
	// guard is consulted main.go has already exported the flag value into
	// GopiDirEnv (the same constant), so this environment read IS the data root
	// in force — the flag, or a profile a caller exported (a sandbox, a
	// task-run child). Nothing here is ever written back to the environment:
	// cmd/agent owns that write.
	gopiHome := flags.GopiHomePath
	if gopiHome == "" {
		gopiHome = os.Getenv(GopiDirEnv)
	}
	settingsPath := flags.SettingsPath
	if settingsPath == "" {
		settingsPath = os.Getenv(SettingsEnv)
	}
	if settingsPath == "" {
		settingsPath = DefaultSettingsPath()
	}
	base, _ := FromSettingsFile(settingsPath) //stylegate:ignore the guard falls back to flag-only resolution when the file is unreadable, which is the conservative arm it exists to provide
	if base == nil {
		base = &Config{SettingsPath: settingsPath}
	}
	// Layers, lowest precedence first — each one FILLS an empty prompt and
	// records where the value came from. The settings layer is already in base
	// (FromSettingsFile decodes the "prompt" key into the same field the run
	// path reads); refuse to overwrite it from a weaker channel.
	if base.Prompt != "" {
		base.promptFrom = promptSourceSettings
	}
	for _, e := range EnvVars {
		switch e.Env {
		case PromptEnvVar:
			// The ONE env-channel knob this resolution consumes, read through
			// envChannelValue so the constant itself is what reaches the read
			// (Rule 21: an env-name constant with no reader is a documented
			// knob nothing consumes). A flag or a settings value already in
			// place is a HIGHER channel and keeps the field.
			if base.Prompt == "" {
				if v := envChannelValue(PromptEnvVar); v != "" {
					base.Prompt, base.promptFrom = v, promptSourceEnv
				}
			}
		case SettingsEnv, GopiDirEnv:
			// Consumed above (they steer the read); a value here would be the
			// caller's own profile, not a knob for this config.
		}
	}
	if gopiHome != "" {
		base.GopiHomePath = gopiHome
	}
	if flags.SetEnvVars != nil && flags.SetEnvVars[PromptEnvVar] {
		base.Prompt, base.promptFrom = flags.Prompt, promptSourceFlag
	}
	if flags.SetEnvVars != nil && flags.SetEnvVars[SettingsEnv] {
		base.SettingsPath = flags.SettingsPath
	}
	// The positionals and the flag they follow come from the FLAG layer alone
	// and are copied verbatim (they are json:"-" and settings never carry
	// them): they are the argument slice the guard classifies, computed once by
	// FlagOverrides and flag-value-aware there (`--model version` puts no
	// positional on this list).
	base.Positionals = flags.Positionals
	base.PositionalVerbFlag = flags.PositionalVerbFlag
	return base, nil
}

// envChannelValue reads ONE environment variable by NAME — the funnel every
// env-channel read in this resolution goes through, so the NAME it is handed is
// the exported constant (PromptEnvVar / SettingsEnv / GopiDirEnv) rather than a
// re-typed spelling (Rule 1b/13).
//
// It exists because the read has to be expressible as an environment call
// against the CONSTANT: the standard gate for Rule 21 ("every env-name constant
// must have a REAL reader") tracks the identifier inside an environment call,
// and a value reached through a helper that only forwards a struct field would
// read as unread — an env-name constant with no consumer is how a documented
// knob becomes a lie.
func envChannelValue(name string) string {
	return os.Getenv(name)
}

// promptInForce reports whether a prompt governs THIS invocation for the
// config at hand — the disarmer predicate, and the ONE place the question is
// answered so no caller can answer half of it.
//
// A prompt is in force when the command line stated one (`--prompt X`,
// `--prompt=X`, `--prompt @file` — FlagOverrides expands the file ref, so a
// multi-line task file arrives here as the prompt text) or when the profile's
// settings.json carries the "prompt" key. Both are decisions about the
// invocation being examined.
//
// An ambient GOPI_PROMPT does NOT disarm, and that is the fix, not an
// oversight (task 01a0de86). Its provenance is unknowable at read time: the
// variable survives the process that set it, so a probe spelling
// `--gopi-prompt 1 version` — the documented env-name-as-flag equivalence of
// `--prompt 1` — leaves GOPI_PROMPT=1 behind for every LATER invocation, and
// "does the environment mention a prompt" then answers yes for a run that was
// never given one. The operator who really means the word literally has the
// spelling the refusal message hands them (`--prompt version`), which is
// unambiguous and does not depend on what some earlier process exported.
func promptInForce(cfg *Config) bool {
	if cfg == nil || cfg.Prompt == "" {
		return false
	}
	return cfg.promptFrom != promptSourceEnv
}

// cli_verb_guard.go — the RUN PATH's verb guard: a leftover positional that
// names a dispatched top-level gopi verb is REFUSED instead of being sent to
// the model as a prompt.
//
// WHY IT LIVES HERE (and not in cmd/agent): the classification must run on
// the SAME argument slice that decides what the prompt IS — FlagOverrides'
// flag-aware positionals. A positional is only visible after option values
// have been consumed (`--timeout 3 version` leaves the positional "version",
// while `--model deepseek-v4-flash` must never turn its VALUE into a
// candidate). cmd/agent cannot be imported from here (package main, import
// cycle), so the verb vocabulary cannot be CALLED from this side either —
// hence a self-contained, FLAG-AWARE copy of the words with an exhaustive
// test in cmd/agent (cli_verb_guard_contract_test.go) that fails the moment
// the two vocabularies disagree. Contrast the CLI's own vocabulary, which is
// the real single source of truth (cmd/agent/cli_verbs.go's CliVerbXxx
// constants, enumerated by the dispatch chain in cmd/agent/main.go and
// mirrored into topLevelCommandNames for completion).
//
// WHERE IT IS USED: cmd/agent/main.go's run path, immediately BEFORE
// readInput — the one moment a leftover positional is about to become the
// task prompt of a real agent run (a session dir, a live ledger row, tokens).
// The bug this closes (2026-09-26, task 01a0dd02-d0a2): `gopi --timeout 3
// version` started a real run and wrote an `[auto] version` ledger row,
// because the dispatch chain reads the verb from the FIRST argument only,
// while the run path accepted any leftover positional as the prompt.

// PositionalVerbs is the set of top-level command words this guard refuses in
// a leftover positional. It holds two classes, both of which mean "the operator
// almost certainly meant `gopi <word> …` and got the argument order wrong":
//
//	(a) the DISPATCHED verb words — the vocabulary `gopi <TAB>` offers
//	    (topLevelCommandNames, sorted) plus the dispatched verbs that set
//	    deliberately omits (completion/raw/stats — see cli_completion_test.go);
//	(b) the KNOWN SPURIOUS SPELLINGS of a real command — the tool wire name
//	    where the command is dash-cased, and the dash twin where the command
//	    keeps the tool's underscore spelling. `code_search` is the code_search
//	    TOOL's wire name while the command is the dash-cased `code-search`
//	    (naming rule 2), so typing the wire name reached the run path with no
//	    refusal at all (measured 2026-09-26, EUAUDIT slice 4 TOOLS-1);
//	(c) the KNOWN SYNONYMS of a real command — a word that MEANS a command
//	    without being a respelling of it. `buildinfo` is the operator's word
//	    for `gopi version` (the Go toolchain's own name for that metadata), and
//	    the dispatch chain does not know it: three `gopi buildinfo` invocations
//	    on 2026-09-30 each bought a whole agent run and produced nothing. The
//	    authoritative table is cmd/agent/cli_verb_aliases.go (one row per
//	    alias), and this list carries the same words because a word refused at
//	    argv[0] must be refused after a run flag too.
//
// Any OTHER word is legitimate prompt text and stays one: `gopi frobnicate …`
// must keep running, because the refusal is a statement about THIS BINARY's own
// surface (a command exists, spelled differently, or under a synonym) and never
// a guess about what the operator meant.
//
// cmd/agent owns the (b) and (c) tables (cli_undispatched_tool_words.go's
// spelling table, cli_verb_aliases.go's synonym table — which refuse the
// argv[0] shape of the same words) and pins this vocabulary against them in
// both directions — adding a word on one side only fails the build.
//
// Kept alphabetical so the mirror test can compare both directions by value.
var PositionalVerbs = []string{
	"act",
	"annotate",
	"archive",
	"brew",
	verbAliasBuildInfo, // (c) the `gopi version` synonym — see cli_verb_guard.go's class (c)
	"capture",
	"chat",
	"checkpoint",
	"cli",
	"code-search",
	toolnames.ToolCodeSearch,
	"completion",
	"config",
	"gif",
	toolnames.ToolKeywordSearch, // tool-derived word, rule 1 spelling (never a literal)
	// (b) the DASH TWIN of that same word: the command keeps the tool's
	// underscore spelling, so `keyword-search` names a real command without
	// being it. It was missing on BOTH sides until the `tool` group's
	// acceptance run spent `gopi keyword-search Foo` as a prompt and minted a
	// session (task 01a109bc…, 2026-10-05); the argv[0] half is the row this
	// one mirrors in cli_undispatched_tool_words.go.
	"keyword-search",
	"ledger-window",
	// (b) the UNDERSCORE twin of the dash-cased command above. A command whose
	// CLI face is dash-cased (`ledger-window`) has an underscore spelling that
	// names no command, and that spelling was in NO vocabulary until this row:
	// measured on the built binary (epic 01a1acc8…, 2026-11-06), argv verbatim
	// in an isolated GOPI_HOME —
	//   `gopi --timeout 3 ledger_window`  → "Error creating agent: …" (run path)
	// while the dash spelling was refused by name. Derived from the same
	// constant as the dash row, never typed (Rules 1/13/14).
	positionalVerbUnderscoreTwin(cliverbs.LedgerWindow),
	"log",
	"memory",
	"memsync",
	"merge",
	"merger",
	// notify is dispatched (CliVerbNotify) but was missing from this list, so
	// `gopi --timeout 3 notify events` still reached the run path as a prompt.
	// Caught by TestPositionalVerbGuardVocabularyMatchesDispatch — the
	// vocabulary contract's whole job is to fail when one side is edited and
	// the other is not.
	"notify",
	"proxy",
	"raw",
	"resume",
	"sandbox",
	// screen is the GROUP verb for a RECORDED SCREEN's bytes
	// (cmd/agent/cli_screen.go). Its neighbour `screen-capture` below is the
	// RETIRED bare tool spelling, a different word.
	"screen",
	"screen-capture",
	// screen-replay is the DISPATCHED top-level verb for a RECORDED SCREEN's
	// bytes (cmd/agent/cli_screen_replay_verb.go) — its own word, next to the
	// `screen` group that answers one level down. Taken from the leaf constant
	// the CLI aliases, never typed (Rules 1/13/14).
	cliverbs.ScreenReplayVerb,
	// ...and its underscore twin, which this vocabulary refuses by name. Every
	// dash-cased command word carries both rows: without the twin row the word
	// falls through to the run path as ordinary prompt text (measured 2026-11-06
	// on `gopi --timeout 3 ledger_window`).
	positionalVerbUnderscoreTwin(cliverbs.ScreenReplayVerb),
	toolnames.ToolScreenCapture,
	"scratch-gc",
	positionalVerbUnderscoreTwin(cliverbs.ScratchGC),
	"session",
	"setup",
	"shot",
	"stats",
	"task",
	"text2img",
	"text2img-verify",
	positionalVerbUnderscoreTwin(cliverbs.Text2imgVerify),
	// tokens is a GROUP-ONLY word (task 01a19be7…, 2026-11-02): `gopi tool
	// tokens` is its one door, and until this row existed `gopi tokens` was in
	// NO vocabulary — so the bare word fell through to the bare-prompt path and
	// bought a real agent run (measured in an isolated GOPI_HOME: a session
	// directory and an `[auto] tokens` ledger row, from a word the operator
	// meant as a command). It is the same class as its neighbours `sandbox` /
	// `scratch-gc` / `ledger-window`, which all carry a row here and one in
	// cmd/agent's alias table; `tokens` was the one that did not.
	"tokens",
	// tool is the GROUP verb for the local tools (cli_tool.go in cmd/agent): a
	// word it does not know is refused BY NAME there, but the group word itself
	// must also be refused in the positional shape — `gopi --timeout 3 tool
	// code-search x` would otherwise spend the word as a prompt, which is the
	// exact accident this vocabulary exists to prevent for every other verb.
	"tool",
	"ui",
	"ui-state",
	"ui_state",
	"version",
	"vision-analyze",
	"vision_analyze",
	"voice-input",
	"voice_input",
	"web",
	// The dash twins of the retired web tool words. `web_search`/`web_fetch`
	// (class (b) below) are refused at argv[0] by their own arm; their
	// dash-cased twins were in NO vocabulary at all, so `gopi --timeout 3
	// web-search` reached the run path as a prompt (task 01a10b4b…, 2026-10-05,
	// measured in an isolated GOPI_HOME: the agent-creation error on the pre-fix
	// binary). Both rows are DERIVED from the tool-name constants through
	// positionalVerbDashTwin — never typed (Rule 1/13).
	positionalVerbDashTwin(toolnames.ToolWebFetch),
	positionalVerbDashTwin(toolnames.ToolWebSearch),
	"web_fetch",
	"web_search",
	"worker",
}

// AliasAddresses exposes the table above for the ONE reader that must agree
// with it: cmd/agent's vocabulary contract, which fails the build when the
// address this package corrects a word to differs from the one the argv[0]
// refusal prints. It returns the table itself (read-only by convention) rather
// than a copy — the caller compares, it does not mutate.
func AliasAddresses() map[string]string { return aliasAddresses }

// positionalVerbDashTwin returns the dash-cased normalisation of an underscore
// command or tool word for the vocabulary above (`web_search` → `web-search`).
// It is DERIVED rather than typed for the same reason cmd/agent's dashTwin is:
// a literal here would be a mirror of a constant this package already imports,
// and the two would drift (Rule 1/13/14). The package cannot share cmd/agent's
// helper — cmd/agent is package main and unimportable — so the drift guard is
// the contract test in cmd/agent that compares the two vocabularies on the real
// binary.
func positionalVerbDashTwin(word string) string {
	return strings.ReplaceAll(word, "_", "-")
}

// positionalVerbUnderscoreTwin is positionalVerbDashTwin's INVERSE, and it is
// the row this vocabulary was missing for every DASH-cased command word
// (`ledger-window` → `ledger_window`). The dash-cased word carries a row here
// and one in cmd/agent's spelling table; its underscore spelling carried
// neither, so it fell through to the run path as ordinary prompt text —
// measured on the built binary (epic 01a1acc8…, 2026-11-06):
// `gopi --timeout 3 ledger_window` printed "Error creating agent: …" instead of
// the positional refusal `gopi --timeout 3 code_search` prints. DERIVED from
// the same constant as its dash sibling for Rules 1/13/14 reasons: a typed
// twin drifts the moment the command is renamed.
func positionalVerbUnderscoreTwin(word string) string {
	return strings.ReplaceAll(word, "-", "_")
}

// positionalVerbSet is the membership index of PositionalVerbs, built once so
// the per-run check is O(1) rather than a scan per positional.
var positionalVerbSet = func() map[string]bool {
	m := make(map[string]bool, len(PositionalVerbs))
	for _, v := range PositionalVerbs {
		m[v] = true
	}
	return m
}()

// positionalVerbRefusalFmt is the refusal text for the shape in which the
// operator named NO flag — the word is simply not in verb position. It names
// the token, says why it is not prompt text, and gives BOTH legal spellings:
// the command as the FIRST argument (a verb anywhere after a run flag is not
// dispatched), and `--prompt <word>` for the operator who really does want the
// word to be the prompt.
//
// THE CORRECTED LINE NAMES THE ADDRESS, NOT THE TOKEN, and for a SYNONYM
// the two differ: `gopi --timeout 3 buildinfo` must be corrected to
// `gopi version`, because correcting it to `gopi buildinfo` would send the
// operator into the OTHER refusal (the argv[0] one) — a dead end pointing at
// itself (found by running the pair, not by reading the code). For a plain verb
// the two are the same word, so the ordinary message is unchanged.
// Args: (token, address, token, address).
const positionalVerbRefusalFmt = "%q is a gopi command, not prompt text — gopi reads the verb from the FIRST argument, " +
	"so it must precede every run flag: `gopi %s`. To send the word as a prompt instead, say so explicitly: `--prompt %s`. " +
	"(For this command's own help: `gopi %s -h`.)"

// positionalVerbHomelessRefusalFmt is the refusal for a token whose command was
// WITHDRAWN rather than moved (`ui`, `cli` — the two retired chat views). There
// is no corrected line, so none is printed: the message says the command is
// gone and leaves the two routes that exist (this word is prompt text only if
// you say `--prompt`, and the interactive surface lives in `gopi chat`). A
// message that spelled `gopi ui` here would send the operator straight back
// into the argv[0] refusal for the same word — a loop, not a fix.
// Args: (token, token).
const positionalVerbHomelessRefusalFmt = "%q is a gopi command whose command line was WITHDRAWN — it was retired with no replacement, " +
	"and gopi reads the verb from the FIRST argument. There is no spelling of it to put first: the terminal and text-mode chat views are gone " +
	"(`gopi chat` is the interactive console). To send the word as a prompt instead, say so explicitly: `--prompt %s`."

// positionalVerbHomelessFlagRefusalFmt is the same refusal when the token
// followed a run flag — it names the flag (the datum that turns "wrong order"
// into a fix everywhere else) and then says plainly that no order fixes THIS
// word, because the command does not exist any more.
// Args: (token, flag, flag, value, token).
const positionalVerbHomelessFlagRefusalFmt = "%q is a gopi command whose command line was WITHDRAWN — it is a POSITIONAL here, following the run flag %s, " +
	"but no position fixes it: the command was retired with no replacement (`gopi %s %s` cannot work — the verb is gone, not misplaced). " +
	"To send the word as a prompt instead, say so explicitly: `--prompt %s`."

// positionalVerbFlagRefusalFmt is the same refusal for the shape the operator
// actually hits — the positional word FOLLOWS a run flag (`gopi --db P
// version`). It leads with the misplaced flag (the one datum that turns "wrong
// order" into a fix), then shows the line that works, derived from that flag
// and its value: a reader must not have to work out which flag to move.
// Args: (token, flag, address, flag, value, token, address).
const positionalVerbFlagRefusalFmt = "%q is a gopi command, not prompt text — it is a POSITIONAL here, following the run flag %s, " +
	"and gopi reads the verb from the FIRST argument. Put the command first: `gopi %s` (the run flag is not the problem: `gopi %s %s` is correct). " +
	"To send the word as a prompt instead, say so explicitly: `--prompt %s`. (For this command's own help: `gopi %s -h`.)"

// PositionalCommandVerbError is the refusal for one verb token — the error the
// run path prints before exiting without starting a run. When the verb was
// passed as a positional AFTER a run flag, the message names that flag and the
// corrected line (see PositionalCommandVerbErrorFor).
func PositionalCommandVerbError(verb string) error {
	addr := positionalVerbAddress(verb)
	if addr == "" {
		return fmt.Errorf(positionalVerbHomelessRefusalFmt, verb, verb)
	}
	return fmt.Errorf(positionalVerbRefusalFmt, verb, addr, verb, addr)
}

// PositionalCommandVerbErrorFor names the misplaced RUN FLAG. It is the
// message the ordinary defect produces: the operator wrote
//
//	gopi --db /tmp/x.db version
//
// so the refusal must not read as a statement about the flag's VALUE (the
// `--db <path>` pair is perfectly legal) but about the word's POSITION —
// naming the flag it followed and spelling the corrected line in the same
// tokens, so the fix is a copy-paste rather than a re-reading of the usage.
func PositionalCommandVerbErrorFor(verb, flag string) error {
	addr := positionalVerbAddress(verb)
	if addr == "" {
		return fmt.Errorf(positionalVerbHomelessFlagRefusalFmt, verb, flag, flag, flagValuePlaceholder, verb)
	}
	return fmt.Errorf(positionalVerbFlagRefusalFmt, verb, flag, addr, flag, flagValuePlaceholder, verb, addr)
}

// positionalVerbAddress is the word the corrected line must spell for a refused
// token: the token ITSELF when it is a command (move it to the front), and the
// command it MEANS when it is a synonym (`buildinfo` → `version`) — for a
// synonym, "put the verb first" is not a fix at all, because that word is not a
// verb anywhere. See aliasAddresses.
//
// Reading the address from a table rather than deriving it is deliberate: the
// synonym → command mapping is a fact, and its authoritative definition is
// cmd/agent/cli_verb_aliases.go (package main, which cannot be imported here —
// Rule 14's leaf-package reason). This table is the guard's half of it, and
// cmd/agent/cli_verb_guard_test.go compares the two in both directions on the
// real binary (a synonym that is refused at argv[0] must be refused here too,
// and must be corrected to the SAME command).
func positionalVerbAddress(verb string) string {
	if canonical, ok := aliasAddresses[verb]; ok {
		return canonical // may be "" — a withdrawn command (see aliasAddresses)
	}
	return verb
}

// aliasAddresses maps a KNOWN SYNONYM to the command it means, for the ONE
// purpose the corrected line in the refusal serves: `gopi --timeout 3 buildinfo`
// must be corrected to `gopi version`, because correcting it to `gopi buildinfo`
// would send the operator into the OTHER refusal — a dead end pointing at
// itself (found by running the pair, not by reading the code).
//
// IT IS ALSO THE GUARD'S HALF OF THE RETIRED-VERB VOCABULARY (epic
// verb-retirement, 2026-10-05). The two halves are (a) MEMBERSHIP — which words
// the guard refuses in a leftover positional, which is PositionalVerbs and
// nothing else — and (b) the ADDRESS a refused word is corrected to, which is
// this map. A retired verb is a member for exactly the same reason it lives in
// cmd/agent's refusal table: `gopi --timeout 3 capture` must not be swallowed
// as a prompt either (that position is the one the dispatch chain never reads).
//
// WHY A LITERAL MIRROR IS THE RIGHT SHAPE HERE (and not a Rule-13 violation).
// **The address is a WHOLE COMMAND LINE now** (`tool capture`, `session log`,
// `tool web search`) and the group words that begin one — `tool`, `session`,
// `config`, `memory` — are declared in cmd/agent (package main, unimportable)
// and in no leaf package. Importing them would mean minting a new leaf package
// for four words or moving the cmd/agent constants; what it would NOT change is
// that this map must be checked against cmd/agent anyway, because the reason the
// guard exists is a statement about THAT binary's surface. The drift guard is
// therefore the gate (cmd/agent/cli_verb_guard_test.go compares the two halves
// in BOTH directions on the real binary — the words AND the addresses), and the
// two directions are what keep a literal here from being free-floating. Where an
// address begins with a word a leaf package already owns it is REFERENCED
// (cliverbs.Version, taskverbs.Archive), never re-typed.
//
// A word in this map but not in PositionalVerbs is never reached; the drift
// guard fails on that too.
var aliasAddresses = map[string]string{
	verbAliasBuildInfo: cliverbs.Version,

	// A group → `session`: the family that absorbed the four readers.
	"log":        "session log",
	"stats":      "session stats",
	"raw":        "session raw",
	"resume":     "session resume",
	"checkpoint": "session checkpoint",

	// B group → `task` (the verb word itself is the family leaf's constant).
	"archive": "task " + taskverbs.Archive,
	// `merge` (epic merge-removal, item 3/6): the ONE-SHOT verb was deleted,
	// the CAPABILITY survives under the family spelling — so the guard must
	// correct a leftover `gopi --timeout 3 merge` to the address that runs,
	// which is the same sentence the argv[0] refusal prints.
	"merge": "task " + taskverbs.Merge,
	// C group → `task`. `memsync` is corrected to `task dedup-scan`, because
	// BOTH memsync spellings are deleted (2026-11-05): `memory memsync` was
	// deleted with the cross-node family, so an address naming it would send the
	// reader to a verb that can only refuse. The one local capability the word
	// carried survives as the ledger verb, and that is what the cmd/agent alias
	// row's own refusal prints (cli_verb_aliases.go) — the two shells of the
	// refusal render ONE address (Rule 14).
	"memsync": "task " + taskverbs.DedupScan,
	"brew":    "memory brew",

	// D group → `config`.
	"setup":      "config setup",
	"completion": "config completion",

	// E + I groups → `tool`: the ONE door for a local tool (and the address the
	// bare word's own refusal prints, so the two argv shapes agree by
	// construction rather than by two edits).
	"capture":         "tool capture",
	"screen_capture":  "tool screen_capture",
	"act":             "tool act",
	"annotate":        "tool annotate",
	"gif":             "tool gif",
	"shot":            "tool shot",
	"proxy":           "tool proxy",
	"vision_analyze":  "tool vision_analyze",
	"voice_input":     "tool voice_input",
	"text2img":        "tool text2img",
	"code-search":     "tool code-search",
	"keyword_search":  "tool keyword_search",
	"ledger-window":   cliverbs.Tool + " " + cliverbs.LedgerWindow,
	"scratch-gc":      cliverbs.Tool + " " + cliverbs.ScratchGC,
	"text2img-verify": cliverbs.Tool + " " + cliverbs.Text2imgVerify,
	// The UNDERSCORE twins of those three dash-cased commands (epic 01a1acc8…,
	// 2026-11-06): a refused spelling must carry the address that RUNS, and for
	// this direction that address is the DASH spelling under `tool` — not the
	// underscore word, which is the one being refused. Derived from the same
	// constants as the rows above so the corrected line can never point at a
	// spelling this binary does not dispatch.
	positionalVerbUnderscoreTwin(cliverbs.LedgerWindow):   cliverbs.Tool + " " + cliverbs.LedgerWindow,
	positionalVerbUnderscoreTwin(cliverbs.ScratchGC):      cliverbs.Tool + " " + cliverbs.ScratchGC,
	positionalVerbUnderscoreTwin(cliverbs.Text2imgVerify): cliverbs.Tool + " " + cliverbs.Text2imgVerify,
	// A TOP-LEVEL dash-cased verb's twin corrects to the flat word, not to a
	// `tool` child: `screen_replay` → `screen-replay` is the address that runs.
	positionalVerbUnderscoreTwin(cliverbs.ScreenReplayVerb): cliverbs.ScreenReplayVerb,
	"sandbox": "tool sandbox",
	"tokens":  "tool tokens",

	// G group → the SPELLING twins (class (b)) whose corrected line used to be
	// the flat word itself — which is REFUSED now (epic verb-retirement,
	// 2026-10-05). Measured by running the pair: `gopi --timeout 3 code_search`
	// said "put the command first: `gopi code_search`", and `gopi code_search`
	// answers "this one is spelled `gopi tool code-search`" — a refusal
	// correcting a word into another refusal. Both halves of each pair now name
	// the surviving command line, spelled exactly as the row above spells the
	// tool words (one table, one spelling), and cmd/agent's vocabulary contract
	// compares the two halves word by word.
	toolnames.ToolCodeSearch:                            cliverbs.Tool + " " + cliverbs.CodeSearch,
	positionalVerbDashTwin(toolnames.ToolKeywordSearch): cliverbs.Tool + " " + toolnames.ToolKeywordSearch,
	positionalVerbDashTwin(toolnames.ToolVisionAnalyze): cliverbs.Tool + " " + toolnames.ToolVisionAnalyze,
	positionalVerbDashTwin(toolnames.ToolScreenCapture): cliverbs.Tool + " " + toolnames.ToolScreenCapture,
	positionalVerbDashTwin(toolnames.ToolVoiceInput):    cliverbs.Tool + " " + toolnames.ToolVoiceInput,
	// The dash twins of the retired web words: their live home is the web
	// SUB-group, whose spelling is `tool web <word>` — not `tool <word>`.
	positionalVerbDashTwin(toolnames.ToolWebSearch): cliverbs.Tool + " " + cliverbs.Web + " " + cliverbs.WebSearch,
	positionalVerbDashTwin(toolnames.ToolWebFetch):  cliverbs.Tool + " " + cliverbs.Web + " " + cliverbs.WebFetch,
	// ui-state → ui_state is a SPELLING correction with a live door behind it
	// (the reader surface is still dispatched), so its address is the word it
	// corrects to.
	positionalVerbDashTwin(cliverbs.UIState): cliverbs.UIState,

	// The F group's `ui` / `cli` rows are GONE (union resolution, rework
	// 01a10e11…, 2026-10-06). On this branch they carried an EMPTY address,
	// because its commit retired both words with no successor; the union with
	// master settles the other way. Master's `cc8289b0` (04:50, task
	// 01a10dba…) had already ruled them 暂不暴露 — the code stays, the commands
	// still run, nothing advertises them — and master DOES dispatch them
	// (isUISubcommand / isTermSubcommand in cmd/agent/main.go). An address row
	// for a dispatched, live verb is not a positional CORRECTION at all; it is
	// the positional guard telling an operator who wrote `gopi --timeout 3 ui`
	// to put a perfectly good command first, which is exactly the shape
	// cmd/agent/cli_verb_guard_test.go's vocabulary contract compares against
	// the argv[0] refusal table and now finds empty. The mechanism that
	// implements the ruling is master's own: help.go's deliberately absent
	// index rows plus cli_completion.go's completionSuppressedVerbs, pinned in
	// both directions by cmd/agent/cli_help_hidden_verbs_test.go.
	//
	// The EMPTY address remains a legal value — positionalVerbAddress still
	// branches on it — it simply no longer has a row: the branch's own
	// empty-address refusal was reachable only while the word was withheld from
	// the dispatch chain too.
}

// PositionalCommandVerbRefusal is the ONE call the run path makes: it returns
// the refusal for a positional verb, choosing the message that fits the argv —
// the flag-aware one when the positionals followed a run flag, the plain one
// otherwise. Keeping the choice here (not at the call site) is what stops the
// run path from having to know which shape produced the token.
func PositionalCommandVerbRefusal(cfg *Config, verb string) error {
	if cfg == nil || cfg.PositionalVerbFlag == "" {
		return PositionalCommandVerbError(verb)
	}
	return PositionalCommandVerbErrorFor(verb, cfg.PositionalVerbFlag)
}

// flagValuePlaceholder stands in for a misplaced flag's VALUE inside the
// refusal's example line: the guard holds the flag's NAME, never its
// argument, and printing the real argv value would echo a path or a key back
// into a terminal. A placeholder keeps the example copy-pasteable by shape.
const flagValuePlaceholder = "<VALUE>"

// PositionalCommandVerb reports whether this run's leftover positionals would
// hand a DISPATCHED top-level verb to the model as the prompt, and returns
// the offending verb — the caller refuses the run and names it.
//
// It returns "" (run) for the deliberate cases:
//
//   - an EXPLICIT prompt channel (--prompt / --prompt= / --prompt @file, the
//     GOPI_PROMPT env var, or the settings.json "prompt" key) — the operator
//     said what the prompt is, so a same-word positional is an input FILE, not
//     a mistyped verb. `gopi --prompt version` therefore still runs a run.
//   - no positional at all (stdin/usage path — nothing to misread), or a
//     positional that is not a dispatched verb word.
//
// cfg.Positionals is already flag-value-aware (FlagOverrides never reports a
// flag's value there), so `gopi --model version "prompt"` is not a false hit.
// Position-zero matching is deliberately NOT restricted to cfg.Positionals[0]:
// the words are command tokens AND legitimate prompt text, and a file-path
// positional sent in alongside the bare word (`gopi --timeout 600 version
// task.txt`) is exactly the shape the operator meant as a command — the
// refusal tells them the one way to say it as a prompt.
func PositionalCommandVerb(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	// An explicit prompt channel wins: the words below are then input FILES.
	if promptInForce(cfg) {
		return ""
	}
	for _, p := range cfg.Positionals {
		if positionalVerbSet[p] {
			return p
		}
	}
	return ""
}

// verbAliasBuildInfo is the `gopi version` SYNONYM (class (c) above), spelled
// here as its own constant because this package must refuse it in a leftover
// positional and canNOT import cmd/agent, where the authoritative synonym table
// lives (cli_verb_aliases.go, package main — Rule 14's leaf-package rule, and
// the reason cmd/agent's cli_verb_guard_test.go compares the two vocabularies
// in BOTH directions on the real binary). It is an alias of cmd/agent's
// constant in every sense except the import direction: the drift guard is the
// test, not a shared symbol.
const verbAliasBuildInfo = "buildinfo"

// cli_verb_hints.go — the SECOND refusal surface: a word that this binary does
// not dispatch AND does not know as a spelling or a synonym of a verb.
//
// WHY IT EXISTS (task 01a0f72b, 2026-10-01). The guard above refuses a word it
// RECOGNISES (`version`, `code_search`, `buildinfo`). Anything else falls
// through to the bare-prompt path, where an UNKNOWN word is a perfectly legal
// prompt — and that is correct for `gopi frobnicate` and wrong for
// `gopi revision`, which is not a prompt at all: it is a QUERY about this
// repository that the operator expected to be answered, spelled as if the
// command existed. Measured on master (`GOPI_HOME=<sandbox> ./bin/gopi revision
// </dev/null`): no refusal, the interactive console starts, and the run
// REGISTERS AN `[auto] revision` LEDGER ROW — the operator pays for a whole
// agent session and gets nothing. 123 such `[auto]` rows were counted in the
// 2026-10-01 failed-row audit (`revision`, `status`, `branch list`,
// `settings show` ×5, `scratch dir`), plus the same shapes in `[auto]` probes
// (`buildinfo`, `branch list`, `scratch dir`).
//
// THE LINE THIS FILE WALKS. A refusal is a statement about THIS BINARY's own
// surface (a command exists under another spelling) and never a guess about
// what the operator meant — the rule the spelling and synonym tables already
// state. So the second surface may not refuse "a word that looks like a
// command"; it refuses a word that is a KNOWN NEAR-MISS of a real command: the
// `-h` query form (`gopi <verb> -h` is legal, `gopi revision -h` is the same
// request with a word that does not dispatch), an alias, or the second word of
// a two-word query whose FIRST word is one of those near-misses (`gopi branch
// list`, `gopi settings show`, `gopi scratch dir`).
//
// WHAT IS DELIBERATELY *NOT* HERE: `gopi frobnicate` / `gopi hi` must keep
// reaching the run path. A word that is neither a verb nor a near-miss of one
// is ordinary prompt text, and the negative control that pins it is a test on
// the real binary, not a comment.

// commandHintRefusalFmt is the refusal for an unknown-positional invocation.
// It names the word, says what this binary would have done with it (a run —
// there is no command of that name), and gives the ONE spelling that means the
// word literally. The `-h` clause is rendered only when the invocation really
// was a help query (see commandHintRefusal): a reader must not be told to run
// `-h` on a word that is not a command.
// Args: (word, word, -h clause, routes clause).
const commandHintRefusalFmt = "gopi: %q is not a gopi command — this binary would have started a RUN with it as the prompt " +
	"(and registered an [auto] task row). To send the word as a prompt, say so explicitly: `--prompt %s`.%s%s"

// THE ROUTES CLAUSE exists for ONE ROW, measured (2026-10-06). The word `replay`
// names a capability this binary reaches through TWO DIFFERENT doors that answer
// two different questions, and the capability's own help page does not say so:
//
//	gopi session replay <id>   PLAYS the recording back — "what was on the
//	                           screen"; the recording is the MATERIAL.
//	gopi chat --replay <id>    RE-RUNS the recorded conversation on a live
//	                           console — "what happens when the model runs
//	                           again"; the recording is the INPUT.
//
// A refusal that printed only one of them would be the dead end Rule 15 bans
// from the other side: the reader would believe they had found the door, try
// the wrong one, and conclude the capability is gone. The clause is RENDERED
// from the table (commandHintRoutesClause) rather than from a format string of
// its own, so a route cannot be dropped from the message while the table still
// carries it — that is why there is no commandHintRoutesFmt.
// commandHintRoute is ONE live door for a near-miss word: the whole command
// line to type, and the question it answers. It is a struct rather than a
// formatted string so the two doors of the row below stay comparable, and so
// cmd/agent's live-binary test can drive them from the table instead of from a
// second copy of the vocabulary (Rule 13/14).
type commandHintRoute struct {
	Command string // the whole command line, spelled as the reader should type it
	Answers string // the question that line answers
}

// commandHintReplayRoutes holds the live doors for the play word, in the order
// the refusal prints them. The FIRST is the one a reader who typed the word
// alone most likely wants (the playback of a recording), and the second is the
// re-run — naming both is the whole point of the clause.
var commandHintReplayRoutes = []commandHintRoute{
	{
		Command: cliverbs.Session + " " + commandHintReplayWord,
		Answers: "PLAY the recording back as the screen it was (with transport controls)",
	},
	{
		Command: cliverbs.Chat + " " + commandHintReplayFlag,
		Answers: "RE-RUN the recorded conversation on a live console (the recording is its input)",
	},
}

// commandHintReplayFlag is the chat verb's replay flag — the SECOND live door
// for the play word. Named here for the same reason as the word itself
// (commandHintReplayWord): the flag's authoritative spelling lives in
// cmd/agent's own flag table, and this leaf package cannot import package main.
// The drift guard is the live-binary test, which runs `gopi chat --replay -h`
// and fails if the flag stops being this command's.
const commandHintReplayFlag = "--replay"

// commandHintRoutesClause renders the routes of a row as one sentence, or ""
// when the row names no extra door (every row but the play word). It reads the
// table rather than a per-row format string so a route cannot be dropped from
// the message while the table still carries it.
func commandHintRoutesClause(routes []commandHintRoute) string {
	if len(routes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" This request has more than one live door:")
	for i, r := range routes {
		if i > 0 {
			b.WriteString(" and")
		}
		fmt.Fprintf(&b, " `gopi %s` %s", r.Command, r.Answers)
	}
	b.WriteString(".")
	return b.String()
}

// commandHintHelpFmt is the extra clause for a near-miss that HAS a real
// command behind the request. It names that command's `-h` first (the pointed
// answer) and the top-level page second (the complete list).
// Args: (address).
const commandHintHelpFmt = " `gopi %s -h` shows the command that answers this request; `gopi -h` lists every command this binary has."

// commandHintUnknownFmt is the clause for a word this binary has NO command
// near it at all — the honest answer, since the vocabulary cannot name a verb
// the operator should have typed. It still leaves a route: the top-level page.
// Args: (word).
const commandHintUnknownFmt = " `gopi -h` lists every command this binary has; there is no command spelled %q."

// commandHintHelpFlag is the help/query spelling itself. It is the SAME word
// cmd/agent prints in its own usage page (cmd/agent's HelpFlag) — the two
// packages cannot share the constant (package main is unimportable), so this is
// the guard-side mirror and cmd/agent's cli_verb_guard_test.go drives the real
// binary with it (Rule 13: the drift guard is the test).
const commandHintHelpFlag = "-h"

// commandHintWords is the closed vocabulary of KNOWN NEAR-MISSES: words an
// operator reaches for when asking this binary a question it does not answer
// under that spelling. Each row names the REAL command that answers the
// request when one exists (the row's `address`), or leaves it empty when the
// binary has no command for it — an honest "there is no such command" beats an
// invented hint (Rule 15: never a silent failure, and never a dead end either).
//
// IT IS CLOSED ON PURPOSE. Growth is a deliberate decision with its own
// evidence (a measured invocation that bought a run), never a heuristic over
// word shape — that is what keeps `gopi frobnicate` running.
// commandHintChatsWord is the PLURAL of the chat verb — see its row in
// commandHintWords below. Spelled here rather than derived from cliverbs.Chat
// because the near-miss is not a spelling this binary HAS: the address is the
// real command (cliverbs.Chat, imported), and this constant is only the word an
// operator types instead of it.
const commandHintChatsWord = "chats"

var commandHintWords = []commandHint{
	// `revision` — the request `gopi version` answers (`revision:` is the key
	// its report prints). The one row measured directly while fixing this
	// (task 01a0f72b: the row that reached the run path and registered an
	// `[auto] revision` ledger row).
	{word: "revision", address: cliverbs.Version},
	// `buildinfo` is deliberately ABSENT here: it is class (c) of the
	// PositionalVerbs guard above (the `gopi version` synonym), which refuses
	// it in every argv position already — a row for it in this table would be
	// unreachable, and a test asserts the two vocabularies do not overlap.
	// `status` — no command of that name. `gopi merger --status` answers the
	// merger question; the row is left with no address because no single
	// command is "the" status, and pointing at one would be a guess.
	{word: "status"},
	// `branch` / `settings` — the two-word QUERY forms whose second word is a
	// sub-verb of a family that lives elsewhere (`gopi task show --branch`,
	// `gopi config show`). Neither `gopi branch` nor `gopi settings` exists.
	{word: "branch"},
	{word: "settings"},
	// `scratch` — the same shape; the real family is `gopi tool scratch-gc`.
	{word: "scratch"},
	// `replay` — the SESSION PLAYER's word, and the one row here whose evidence
	// is a whole agent run spent by a machine rather than by a person.
	//
	// THE DEFECT (measured 2026-10-06, this epic's briefing). A task briefing
	// wrote `gopi replay <session-id>` — the spelling the playback had BEEN
	// called by — and this binary does not dispatch that word: the top-level
	// `gopi replay` verb was abolished on 2026-10-03
	// (devdoc/replay-abolished-verb-single-route.md) and its capability moved
	// under the session family. Nothing on the run path recognised the word, so
	// the briefing line was spent as the PROMPT: a real agent session was
	// started, and the reply was the model wondering what "replay" meant.
	//
	// WHY THIS ROW AND NOT A `verbAliases` ROW. A synonym-table row is refused at
	// argv[0] in every argv shape, `-h` included (TestVerbAliasIsRefusedByNameNoRun
	// drives `{alias, "-h"}` and requires exit 1 — measured: `gopi buildinfo -h`
	// is refused). The player's word must NOT behave that way: `replay -h` is a
	// legal top-level help query and has to keep printing the top-level page and
	// exiting 0 — the same contract the near-misses already keep
	// (`TestNearMissHelpQueryStaysLegal`). This vocabulary is the only run-path
	// surface that answers a word the dispatch chain does not know, and it is
	// reached AFTER the help scan, so `-h` never gets here.
	//
	// WHY IT IS NOT IN config.PositionalVerbs EITHER. That list refuses a word
	// by CASTING IT AS A COMMAND ("put the command first: `gopi <address>`"), and
	// every word in it must be one this binary either dispatches or refuses by
	// name — the table's own contract (cli_verb_guard_test.go). `replay` is
	// neither: it is a word for a command under another DOOR. It belongs here,
	// where the message says what is true of it (no command of that name; the
	// player lives in the session family), and its presence there instead is
	// asserted by the sibling abolition gate, which requires the word to be
	// absent from PositionalVerbs (session_replay_abolition_test.go, face (a)).
	//
	// The address is the sub-verb's own page, assembled from the leaf constant
	// and this file's mirror of the word (rule 13's standing exemption: the
	// authoritative constant lives in cmd/agent, package main, which this leaf
	// package cannot import — the drift guard is cmd/agent's live-binary test).
	// The address is the FAMILY word (`session`), not the two-word line, because
	// that is this vocabulary's contract: the address is the word this binary
	// DISPATCHES and whose own page answers the request
	// (TestCommandHintVocabularyIsWellFormed asserts dispatched[address] — a
	// refusal's hint may not lead nowhere). The PRECISE lines live in routes,
	// which the live-binary test drives end to end; the help clause therefore
	// sends the reader to the family page, where every member is listed.
	{word: commandHintReplayWord, address: cliverbs.Session, routes: commandHintReplayRoutes},
	// `chats` — the PLURAL of a command this binary has, and the one row here
	// whose evidence is a LEDGER ROW rather than a sentence (measured
	// 2026-11-01, task 01a194e1-5e79-7ad5-a8b3-bde0a9236b0e).
	//
	// THE DEFECT. Something invoked `gopi chats` (the row's own `run_cmd` reads
	// `gopi chats` verbatim). No verb of that spelling exists, the dispatch
	// chain found none, and the leftover positional became the PROMPT of a real
	// agent run: a session directory was minted and the ledger registered
	// `[auto] chats` — the run then died of SIGPIPE against a closed stdout (a
	// separate defect, fixed at internal/agent/brokenpipe_guard.go) and left the
	// row `failed`, which is how it was found at all.
	//
	// WHY THE PLURAL IS THE WHOLE POINT, and why it is not a spelling or a
	// synonym class. `chats` is not a respelling of `chat` (no library has a
	// PLURAL of a command) and it is not a synonym; it is the word an operator
	// reaches for when asking "what chats are there" — i.e. the LISTING
	// question, which this binary answers with `gopi session list` (the `chats`
	// table is what a session IS). It belongs in the closed near-miss vocabulary
	// because that table is the only run-path surface that answers a word the
	// dispatch chain does not know, and the refusal must keep `gopi chats -h` a
	// legal top-level help query (the reason it cannot be a `verbAliases` row:
	// an alias is refused in every argv shape, `-h` included).
	//
	// THE ADDRESS IS `chat` (the ROUTE below carries the listing door). This
	// vocabulary's contract is that the address is a word the binary DISPATCHES
	// (TestCommandHintVocabularyIsWellFormed asserts dispatched[address]); the
	// listing command line is TWO words and a sub-verb of the session family, so
	// it is a ROUTE — the same split the play word uses, and cmd/agent's
	// live-binary test drives it end to end (`gopi session list -h` must exit 0).
	{word: commandHintChatsWord, address: cliverbs.Chat, routes: commandHintChatsRoutes},
}

// commandHintChatsRoutes names the door that answers the LISTING question — the
// one a reader who typed the plural almost certainly meant. `gopi chat -h`
// answers "how do I talk to a model"; `gopi session list` answers "what chats
// are there". Printing only the first would send them to a page that never
// mentions a listing, which is the dead end the routes clause exists to
// prevent.
var commandHintChatsRoutes = []commandHintRoute{
	{
		Command: cliverbs.Session + " " + commandHintSessionListWord,
		Answers: "LIST the sessions (chats) this profile has recorded",
	},
}

// commandHintSessionListWord is the `list` sub-verb of the session family — the
// door the row above sends the reader to. Spelled here for the same reason as
// commandHintReplayWord: the authoritative constant lives in cmd/agent (package
// main), which this leaf package cannot import, and cliverbs names the GROUP
// verb but not its leaves. The drift guard is cmd/agent's live-binary test,
// which runs this route's `-h` and fails if the sub-verb stops existing.
const commandHintSessionListWord = "list"

// commandHint is ONE near-miss word: the word, the real command that answers
// the request — "" when this binary has no command for it — and, for a word
// whose request has MORE THAN ONE live door, those doors.
type commandHint struct {
	word    string
	address string
	// routes names every additional live command line for this word, each with
	// the question it answers. Empty for every row but the play word, whose two
	// doors answer two different questions and must both be printed (see
	// commandHintRoutesFmt for why one of them alone is a dead end).
	routes []commandHintRoute
}

// CommandHintRow is the EXPORTED projection of one near-miss row. It exists so
// cmd/agent's vocabulary contract can drive the REAL binary over the same
// vocabulary without a second copy of it (Rule 13/14): the table below stays
// the single definition, and the test asserts the words and addresses the
// binary actually behaves on.
type CommandHintRow struct {
	Word    string // the word an operator types instead of a command
	Address string // the command that answers the request ("" = none exists)
	// Routes carries the word's ADDITIONAL live doors, so the live-binary
	// contract drives every command line the refusal prints rather than only
	// the first (an address that is asserted but a route that is not would let
	// a dead hint ship in the very sentence written to avoid one).
	Routes []commandHintRoute
}

// CommandHintRows projects the vocabulary for the contract test, in table
// order. Callers that need the words as a SET build a map from this once.
func CommandHintRows() []CommandHintRow {
	out := make([]CommandHintRow, 0, len(commandHintWords))
	for _, r := range commandHintWords {
		out = append(out, CommandHintRow{Word: r.word, Address: r.address, Routes: r.routes})
	}
	return out
}

// commandHintFor returns the row for a word, or nil when the word is not a
// known near-miss.
func commandHintFor(word string) *commandHint {
	for i := range commandHintWords {
		if commandHintWords[i].word == word {
			return &commandHintWords[i]
		}
	}
	return nil
}

// commandHintRefusalText is the EXPORTED entry the run path calls: the refusal
// message for a near-miss invocation, or "" when the invocation must run as
// ordinary prompt text. It is the ONE call site's access to the vocabulary
// above (Rule 3: no new tool, no new verb — this rides the existing run path),
// and it returns the TEXT rather than printing, so the decision stays testable
// and the process's exit stays main's.
func PositionalCommandHintRefusal(cfg *Config) string { return commandHintRefusal(cfg) }

// commandHintRefusal builds the refusal for an unknown-positional invocation,
// or returns "" when the invocation must NOT be refused.
//
// The four shapes it answers (the ones measured buying a run):
//
//	gopi revision              the word alone
//	gopi revision -h           the help query — which IS a legal top-level query
//	                           today (the help scan prints usage and exits 0), so
//	                           the message must keep that route open; it asserts
//	                           nothing about the exit code (that is main's call)
//	gopi branch list           two-word query, first word a near-miss
//	gopi branch list -h        the same with the help flag
//
// Everything else returns "" — an unknown word with further words after it is
// prompt text (`gopi summarize the docs`), and a known VERB is the other
// guard's business.
func commandHintRefusal(cfg *Config) string {
	if cfg == nil || promptInForce(cfg) {
		return ""
	}
	word := commandHintWord(cfg.Positionals)
	if word == "" {
		return ""
	}
	row := commandHintFor(word)
	if row == nil {
		return fmt.Sprintf(commandHintRefusalFmt, word, word, fmt.Sprintf(commandHintUnknownFmt, word), "")
	}
	if row.address == "" {
		// A near-miss this binary has NO command for (`status`): the honest
		// clause, never a `gopi  -h` pointing at nothing (a refusal whose hint
		// leads nowhere is the second defect this vocabulary exists to avoid).
		return fmt.Sprintf(commandHintRefusalFmt, word, word, fmt.Sprintf(commandHintUnknownFmt, word), "")
	}
	// Rows that carry ROUTES print them alongside the help clause: the clause
	// says where the page is, the routes say which of the two doors answers
	// which question — and for the play word that difference is the whole
	// reason the reader is standing here (they typed the name of a capability
	// that has moved).
	return fmt.Sprintf(commandHintRefusalFmt, word, word,
		fmt.Sprintf(commandHintHelpFmt, row.address), commandHintRoutesClause(row.routes))
}

// commandHintWord picks the near-miss word out of the positionals, or "" when
// the invocation is not the refusal's shape.
//
// The shapes are spelled by ARITY, not by guessing (Rule 1: a value that
// carries meaning is named): ONE positional (the bare query, `gopi revision`),
// or TWO where the FIRST is a near-miss (`gopi branch list` — the second word
// is the near-miss command's sub-verb and says nothing about this binary's
// surface on its own). A near-miss word with two or more words after it is an
// ordinary prompt that happens to open with one of these words
// (`gopi branch off the failed attempt`), and it keeps running.
//
// THE ONE EXCEPTION is the play word's OPERAND shape (`gopi replay
// 56507166-ef62-4d2d-bafe-af504fca9311`): a session id is not a sub-verb, so
// the pair rule above does not describe it, and the id is a datum strong enough
// to answer on — no sentence opens with a command word followed by a bare
// 36-character uuid. It is bounded at one trailing word so a long sentence that
// merely CONTAINS an id stays prompt text (see commandHintReplayOperandMax).
func commandHintWord(positionals []string) string {
	switch len(positionals) {
	case commandHintBareArity, commandHintPairArity:
		return commandHintNearMiss(positionals[0])
	}
	if commandHintReplayOperand(positionals) {
		return positionals[0]
	}
	return ""
}

// commandHintReplayOperand reports whether positionals is the play word's
// OPERAND shape: the word, a session id as the next positional, and at most one
// more positional after it (`gopi replay <id>`, `gopi replay <id> --flag VALUE`
// → the flag pair is consumed as a flag, so this is the two-positional case;
// a third positional keeps the shape only while it is that one word).
//
// It is deliberately NOT a "does this mention `replay`" test: the word must be
// FIRST (the dispatch position the operator meant), and the operand must have
// the shape of a session id — the operand `gopi session list` prints and every
// reader in that family takes.
func commandHintReplayOperand(positionals []string) bool {
	if len(positionals) < commandHintPairArity || len(positionals) > commandHintReplayOperandMax {
		return false
	}
	if positionals[0] != commandHintReplayWord {
		return false
	}
	return isSessionIDShape(positionals[1])
}

// commandHintReplayWord is the play word this guard answers in its OPERAND
// shape as well as its bare-query shape. It is spelled here rather than imported
// because the authoritative constant is cmd/agent's sessionSubcmdReplay and
// cmd/agent is package main, which this leaf package cannot import (Rule 14's
// leaf-package reason) — the same shape as verbAliasBuildInfo above, and for the
// same reason. The drift guard is cmd/agent's live-binary test, which drives the
// real binary with THIS row's word and asserts the address it prints really runs
// (cli_replay_prompt_refusal_test.go).
const commandHintReplayWord = "replay"

// commandHintReplayOperandMax is the largest positional count the OPERAND shape
// covers: the word, its session id, and one further word. Named because the
// bound is the decision that keeps `gopi replay 56507166-… twice` inside the
// refusal and a sentence carrying an id outside it — not an incidental number.
const commandHintReplayOperandMax = 3

// commandHintSessionIDGroups is the session id's spelling as `gopi session
// list` prints it: a canonical RFC 4122 uuid, one hex run per group (8-4-4-4-12)
// with a separator between the runs. Stated as the LAYOUT rather than as the
// total, so the length check below is derived from the shape it is checking
// (Rule 1: the value that carries meaning is the layout).
var commandHintSessionIDGroups = [...]int{8, 4, 4, 4, 12}

// commandHintSessionIDSeparator is the byte between the id's hex runs.
const commandHintSessionIDSeparator = '-'

// isSessionIDShape reports whether word is spelled like a session id. The SHAPE
// is all that is checked, never a lookup: the id a briefing names is usually not
// a session this machine holds (another host's profile, a reclaimed scratch
// root), so a refusal that only fired for ids the local ledger knows would fall
// through to a whole agent run for exactly the case that hurt — and the ledger
// read would make this guard's answer depend on profile state, which no other
// refusal here does.
func isSessionIDShape(word string) bool {
	want := len(commandHintSessionIDGroups) - 1 // the separators between the runs
	for _, g := range commandHintSessionIDGroups {
		want += g
	}
	if len(word) != want {
		return false
	}
	pos := 0
	for gi, g := range commandHintSessionIDGroups {
		if gi > 0 {
			if word[pos] != commandHintSessionIDSeparator {
				return false
			}
			pos++
		}
		for i := 0; i < g; i++ {
			if !isHexDigit(word[pos]) {
				return false
			}
			pos++
		}
	}
	return true
}

// isHexDigit reports whether b is one hex digit — the alphabet a uuid's runs are
// drawn from.
func isHexDigit(b byte) bool {
	switch {
	case b >= '0' && b <= '9', b >= 'a' && b <= 'f', b >= 'A' && b <= 'F':
		return true
	}
	return false
}

// commandHintNearMiss returns the word itself when it is a near-miss, else "".
// The second word of a two-word query is NOT looked at: `gopi branch <x>` is
// the near-miss whatever `<x>` is (the operator asked for a `branch` family
// this binary does not have), and requiring `<x>` to be a known sub-verb would
// turn the refusal into a guess about the sub-verb — while not looking at it is
// what keeps `gopi branch off the failed attempt` (three words) running.
func commandHintNearMiss(word string) string {
	if commandHintFor(word) == nil {
		return ""
	}
	return word
}

// commandHintBareArity / commandHintPairArity are the two ARITIES the near-miss
// refusal answers (one word, or a near-miss followed by one word). Named
// because the numbers carry the decision above.
const (
	commandHintBareArity = 1
	commandHintPairArity = 2
)

// The ADDRESS for that synonym is cliverbs.Version — the leaf constant, not a
// literal, because cmd/agent's CliVerbVersion is the SAME definition and the
// two must not be able to drift (Rule 13/14; internal/cliverbs carries the
// rule's reasoning). A literal here would be the mirror this repo's rules ban,
// and it is exactly the word an operator copies out of the refusal.
