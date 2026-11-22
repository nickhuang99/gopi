package main

// runtime_config_snapshot.go — the configuration THIS PROCESS is actually
// running with, read from the live process and rendered as one table.
//
// THE QUESTION THIS ANSWERS, AND THE ONE IT CANNOT (operator, 2026-11-12).
// `gopi config show` re-reads settings.json and describes the FILE. It is
// structurally unable to answer "what is this process using right now", because
// it is a separate process and it can only read what gopi's resolution code
// would have published. This file reads the OTHER HALF: `os.Getenv` as the
// kernel delivered it, plus the agent's own resolved fields — what the request
// builder in THIS process will put on the wire.
//
// The gap between those two answers is not hypothetical. Measured on this host
// on 2026-11-12: settings.json stated `"reasoning_effort": "high"`, the running
// process held no such variable, and every request went out thinking-disabled.
// The document was RIGHT and the process did not have it — and no reprocessing
// of the file can show that. Hence the channel `NOT APPLIED`: a value the
// settings document states and this process never received is a DEAD value on
// any knob whose only reader is os.Getenv.
//
// WHERE IT IS RENDERED. Two doors, one renderer: the chat REPL's `/config`
// (chat_config.go) and the worker's start-up debug dump. Neither reformats the
// other's text.
//
// THIS IS A REPORT, NOT A RESOLVER. It decides nothing, publishes nothing and
// never writes the environment. Every row states what it READ and where the
// value came from; the coupling notes below the table state what the values
// cannot say on their own (an effort that is present and still off the wire).

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/configreport"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/thinkmode"
)

// The vocabulary of this report. Every literal that carries meaning is a named
// constant (Rules 1/13/14) — the channels below are compared against by callers
// and by tests, so they are not prose. Their VALUES now live in configreport
// (the shared parsing layer, epic 01a1eaec); these names alias that one source
// so the chat renderer, the CLI renderer and the decision function cannot spell
// a channel differently.
const (
	runtimeConfigChannelFlag            = configreport.ChannelFlag
	runtimeConfigChannelEnv             = configreport.ChannelEnv
	runtimeConfigChannelSettings        = configreport.ChannelSettings
	runtimeConfigChannelDefault         = configreport.ChannelDefault
	runtimeConfigChannelNotApplied      = configreport.ChannelNotApplied
	runtimeConfigChannelUnset           = configreport.ChannelUnset
	runtimeConfigChannelDecidedByPrefix = configreport.ChannelDecidedByPrefix
	// The THIRD source position (bug row 01a1ffdd): a value THIS PROCESS set at
	// run time, carried by no flag, export or document. Its spelling lives in
	// configreport with the other channels; this name only aliases it so no
	// renderer re-spells a channel (Rules 1/13/14).
	runtimeConfigChannelRunning = configreport.ChannelRunning

	// runtimeConfigCoupledNoteFormat is the `!` line for a coupled row: WHAT
	// decides it, the value in force, and the compiled-in default that therefore
	// does NOT run. The sentence names the escalation default too, because a
	// reader who set the coupled knob must be able to tell when it stops
	// deciding ("non-off": the one effort value that switches thinking off).
	runtimeConfigCoupledNoteFormat = "%s is decided by %s=%q, not by the compiled-in default (%s): an explicit, valid effort that is not %q with no explicit %s auto-enables thinking, so the request carries %s=%s"

	// runtimeConfigSeedRedacted replaces every credential-shaped value. This
	// report is printed to a terminal and copied into a run log, so a key must
	// never be copied out of the process it was read from.
	runtimeConfigSeedRedacted = "<set>"

	// runtimeConfigTitle is the block's first line: ONE spelling, so a reader —
	// and a test — can find the block in a log that also carries prose.
	runtimeConfigTitle = "configuration in force (read from this RUNNING process, not from the file)"

	// runtimeConfigNoteMarker prefixes every coupling note below the table. It
	// is the same `!` the acceptance criterion names, and it exists so a reader
	// (and a test) can tell "a value" from "an observation about the values".
	runtimeConfigNoteMarker = "  ! "

	// runtimeConfigUnsetDefaultFormat is the VALUE cell of a row the process
	// does not hold while the code has a compiled-in default: it says both
	// facts at once, because either one alone is a different answer than
	// either alone. `(unset)` alone says "nothing is set" and leaves the reader
	// unable to tell 16384 from 0; `16384` alone would claim the process holds
	// something it does not. Together: "the process holds nothing — and THIS is
	// what will run".
	//
	// THE THIRD %s IS THE KNOB'S UNIT, and it comes from the FIELD, never from
	// this shared template. This template serves token budgets, counts,
	// ratios and character lengths — hard-coding a unit here would put the
	// wrong one on most rows. `runtimeConfigDefaultUnitOf` supplies either a
	// leading-space unit (" token") or "" for a dimensionless knob, so the two
	// token knobs render `200000 token` / `16384 token` while a count renders a
	// bare number. There is deliberately NO fallback word: the old "%s runs"
	// made readers parse `200000 runs` as "200000 runs of something", which is
	// the bug this change closes. `[built-in default]` in the channel column
	// already states "this value is in force"; a predicate in the value cell
	// was always redundant.
	runtimeConfigUnsetDefaultFormat = "%s, %s%s"

	// runtimeConfigUnitTokens is the unit for the two token-dimension knobs:
	// `context_window` (a token budget) and `max_tokens` (an output cap). It is
	// SINGULAR on purpose — a dimension name used as a quantity modifier takes
	// the singular in English ("200000 token budget"), so both magnitudes read
	// consistently and there is no plural branch at the value 1.
	runtimeConfigUnitTokens = " token"

	// runtimeConfigEffortValuesSeparator is how a LIST-valued knob's members are
	// joined for the value cell — the same comma the settings document and
	// GOPI_EFFORT_VALUES use (internal/config/env.go joins that list with a
	// plain ","), so the report prints the list in the spelling a reader could
	// paste back into their settings.json.
	runtimeConfigEffortValuesSeparator = ","

	// runtimeConfigClampedValueFormat is the VALUE cell of a row whose number
	// does NOT reach the wire as written: the effect first, the configured
	// number second. It is the same shape family as
	// runtimeConfigUnsetDefaultFormat above — "what runs, and what the other
	// fact was" — because a cell that printed only 8192 would hide the
	// operator's own setting, and a cell that printed only 16384 would repeat
	// the defect this row exists to fix (bug row: `/config` claimed 16384 while
	// every request carried 8192).
	runtimeConfigClampedValueFormat = "%d, %d configured"

	// runtimeConfigClampNoteFormat is the `!` line for a clamped row: WHAT was
	// configured, WHAT goes on the wire, and WHY (the builder's max_tokens/2
	// cap, whose only definition point is deepseek.EffectiveThinkingBudget) —
	// plus the knob that lifts it, because a reader who learns the number is
	// halved and not why it may be halved cannot act on it.
	runtimeConfigClampNoteFormat = "%s: thinking budget is clamped — %d configured, %d goes on the wire because the request builder caps the budget at max_tokens/2 (%d/2); that capped number is also the token count the harness aborts a round at, so raise max_tokens to keep a larger thinking budget"

	// runtimeConfigRunningNoteFormat is the `!` line for a row a console command
	// changed. The two %s after the key are the value IN FORCE (what this process
	// holds) and what the CONFIGURATION states, because the difference between
	// those two is the whole point of the row: the change is real and ephemeral.
	runtimeConfigRunningNoteFormat = "%s is in force from THIS process, not from a configuration channel: it holds %q because that value was set at run time, and it is never written back (this session only) — the configuration still states %s, so a new process starts there"

	// runtimeConfigConfigurationSaysWord is what the note says when no
	// configuration channel states the knob at all. It is a phrase rather than a
	// blank so the note cannot read as a truncated sentence.
	runtimeConfigConfigurationSaysWord = "nothing"

	// runtimeConfigKeyAgentThinkingBudget is the resolved row's key: the budget
	// the AGENT resolved, which is the same number the builder then clamps. It
	// is a constant because the clamp logic keys on it (Rule 1).
	runtimeConfigKeyAgentThinkingBudget = "agent.thinking.budget_tokens"

	// The two resolved BOOL fields of the agent's section. They are constants
	// because the run-time registry keys on the knob spellings while these rows
	// name the resolved field, and a test asserts BOTH lists are complete
	// (Rules 1/13).
	runtimeConfigKeyAgentHideThinking = "agent.hide_thinking"
	runtimeConfigKeyAgentKeepThinking = "agent.keep_thinking"

	// The two environment spells the budget clamp reads. internal/agent resolves
	// both by these names (loop.go's maxTokens / thinking-budget statements), so
	// this report must read the SAME names — one spelling, two readers.
	settingsEnvMaxTokens      = "GOPI_MAX_TOKENS"
	settingsEnvThinkingBudget = "GOPI_THINKING_BUDGET"
)

// runtimeConfigBudgetKeys are the TWO rows whose number the request builder
// clamps before it reaches the wire — the knob the operator sets and the field
// the agent resolved. Naming them once keeps the classifier
// (runtimeConfigBudgetClampOf) and the note loop from disagreeing about which
// rows carry a clamp.
var runtimeConfigBudgetKeys = []string{settingsKeyThinkingBudget, runtimeConfigKeyAgentThinkingBudget}

// runtimeConfigClamp is the ONE record of a configured number the request
// builder LOWERS before it reaches the provider.
//
// WHY IT IS A FIELD AND NOT A SECOND CALCULATION IN THE RENDERER: the clamp has
// exactly one definition point (`deepseek.EffectiveThinkingBudget`, Rule 2/14),
// and this report may only CALL it. Filing the answer on the row keeps the value
// column and the `!` note from computing it twice and disagreeing.
type runtimeConfigClamp struct {
	// Configured is what the environment, the document or the agent states.
	Configured int
	// Effective is what the request will carry.
	Effective int
	// MaxTokens is the output cap the budget was clamped against, so the note
	// can show the arithmetic instead of asserting a result.
	MaxTokens int
}

// runtimeConfigField is one knob's row.
type runtimeConfigField struct {
	// Key names the knob as the operator knows it — the settings key, which is
	// also the name `gopi config show` reports it under.
	Key string
	// InForce is the value the running process holds, and therefore what the
	// next request will carry. "" means the process holds nothing.
	InForce string
	// EnvName is the environment variable the row was READ from — the channel
	// record is keyed by it. "" for a row the agent resolved.
	EnvName string
	// Document is the settings document's own value for Key, "" when the
	// document states none.
	Document string
	// Channel is what decided InForce, computed from the three facts above by
	// the ONE classifier (runtimeConfigChannelOf / configreport.ResolveRun).
	Channel string
	// Configured is what the CONFIGURATION channels held for this knob before
	// any run-time value was folded in — the environment's value, in the row's
	// own value vocabulary. It is "" when the process carried nothing.
	//
	// IT EXISTS SO A RUN-TIME ROW CAN SAY WHAT THE CONFIGURATION STILL STATES
	// (bug row 01a1ffdd): `/thinking off` writes no configuration, so the report
	// must be able to print "in force here: hidden; the configuration says
	// shown" without reading the environment a second time (Rule 2).
	Configured string
	// Default is the compiled-in value that runs when nothing decided this
	// knob, read from runtimeConfigBuiltinDefaultOf and stored here so the
	// renderer NAMES the value instead of leaving the reader to reconcile the
	// channel column with the resolved row below it. It is "" for a knob with
	// no compiled-in default (and for every resolved agent row, which carries
	// its own answer), and "" for a COUPLED row — see DecidedBy.
	Default string
	// DefaultUnit is the UNIT of Default, carried on the field rather than
	// hard-coded in the shared value template. It is "" for a dimensionless
	// knob (a count, a ratio, a toggle) and a leading-space unit such as
	// " token" for the two token-dimension knobs. The renderer appends it only
	// on the `(unset), <default>` row, so `context_window` reads
	// `(unset), 200000 token` while a count reads a bare number — no unit is
	// ever guessed from the shared template.
	DefaultUnit string
	// DecidedBy is the KEY of another row of this same table that decides this
	// knob's value, or "" for the ordinary case. It exists for the one knob with
	// a cross-field coupling: `thinking` is auto-enabled by an explicit non-off
	// `reasoning_effort` (agent.EffortImpliesThinking). On such a row the value
	// cell must NOT name the compiled-in default — that default does not run —
	// and the channel must not claim it did.
	DecidedBy string
	// Clamp records that the number this row HOLDS is not the number that
	// reaches the provider, and names both. It is set by the ONE budget clamp
	// call (runtimeConfigBudgetClampOf → deepseek.EffectiveThinkingBudget), so
	// the value cell and the `!` note state the same measurement rather than
	// two calculations that could drift.
	//
	// THIS ROW EXISTS BECAUSE THE VALUE COLUMN LIED (measured 2026-11-14): with
	// `thinking_budget 16384` configured and the default output cap, the table
	// said 16384 and every request body carried `"budget_tokens":8192`. A reader
	// budgeting a run from this column was out by 2x — and 8192 is also the
	// token count the harness aborts a round at, so the two facts a reader needs
	// were both missing.
	Clamp *runtimeConfigClamp
}

// runtimeConfigSnapshot is the whole report, built once and rendered once.
type runtimeConfigSnapshot struct {
	// Fields are printed in this order: the model wire first (the knobs whose
	// values reach the provider), then the agent's resolved fields, then the
	// run's shape and the profile's paths.
	Fields []runtimeConfigField
	// Agent is the agent the resolved rows were read from, nil when this
	// carrier had none. Kept so deriveNotes can state the couplings the
	// resolved fields imply.
	Agent *agent.Agent
	// Notes are one-line observations the values alone cannot state (a knob
	// that is dead, a coupling that clears it). Empty is a normal case.
	Notes []string
}

// runtimeConfigEnvSources is the ONE record of which env names this process
// chose (as opposed to inherited), in precedence order when a name appears in
// both.
//
// WHY IT IS FILED RATHER THAN RE-DERIVED. There is exactly one place that
// decides the precedence — resolveRunConfig / resolveChatRunConfig / the web's
// own call — and re-reading settings.json here would be a SECOND resolution
// that could disagree with the one actually in force (Rule 2). So the resolver
// hands this surface its answer, and the surface reports it. A caller with no
// resolution in hand (a unit test, a carrier that resolved nothing) passes the
// zero value: every env-carried row is then honestly `env`, and every
// document-stated row with no variable is `NOT APPLIED` — the same verdict,
// with the ambiguity resolved toward the claim that can be checked.
type runtimeConfigEnvSources struct {
	// Flag names the env names a CLI flag decided. REQUIRED to state
	// runtimeConfigChannelFlag.
	Flag map[string]bool
	// Settings names the env names THIS RUN loaded from the settings document
	// and published. This is ApplyEnv's own published set, so it also proves
	// the value came from the document rather than from an ambient export.
	Settings map[string]bool
}

// recordEnvSources remembers what this run's own resolution decided, so a later
// runtimeConfigSnapshot reads the channels instead of re-deriving them.
//
// It is a PROCESS-WIDE fact (the resolution is process-wide: resolveRunConfig
// publishes into os.Environ once), so one write per process is the honest
// shape. A second call REPLACES rather than merges: two resolutions in one
// process would mean the second one's precedence is the one in force.
func recordEnvSources(flag, settings map[string]bool) {
	runtimeConfigFlagEnvNames = runtimeConfigCopyNames(flag)
	runtimeConfigSettingsEnvNames = runtimeConfigCopyNames(settings)
}

// The process-wide record, read through runtimeConfigSourcesSnapshot.
var (
	runtimeConfigFlagEnvNames     = map[string]bool{}
	runtimeConfigSettingsEnvNames = map[string]bool{}
)

// runtimeConfigEnvSnapshot captures the process environment as (name → value)
// for the differential record below. Absence and emptiness are DIFFERENT facts
// here — ApplyEnv's fill rule tests `!present || v == ""` — so the map keeps
// every present name, including the empty ones.
func runtimeConfigEnvSnapshot() map[string]string {
	env := os.Environ()
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// recordResolutionSources records WHAT THIS RUN'S RESOLUTION DECIDED, from
// OBSERVATION rather than from a second reading of the document.
//
// THE MEASUREMENT: `before` is the environment as the process inherited it, and
// the caller takes it BEFORE config.ApplyEnv runs. ApplyEnv is FILL-ONLY over
// the settings layer, so a name whose value the settings load changed is one the
// document supplied; a name the operator had already exported is untouched and
// therefore stays the operator's own. That is the whole difference between the
// `env` and `settings.json` channels, and it is measured, never inferred from
// "the two values happen to match".
//
// The FLAG half comes from overrides.SetEnvVars, which FlagOverrides fills as
// it parses — the parser's own record of what a flag decided, not a re-walk of
// argv (Rule 2).
func recordResolutionSources(before map[string]string, overrides *config.Config) {
	settings := map[string]bool{}
	after := runtimeConfigEnvSnapshot()
	for name, value := range after {
		if prev, had := before[name]; had && prev == value {
			continue // untouched: an inherited export, or nothing was published
		}
		settings[name] = true
	}
	flag := map[string]bool{}
	if overrides != nil {
		for name, set := range overrides.SetEnvVars {
			if set {
				flag[name] = true
			}
		}
	}
	recordEnvSources(flag, settings)
}

// runtimeConfigCopyNames copies a name set, so the caller's map cannot be
// mutated under the record (and a nil map stays nil-safe to read).
func runtimeConfigCopyNames(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		if v {
			out[k] = true
		}
	}
	return out
}

// runtimeConfigSourcesSnapshot returns the record, for this surface and for a
// test that wants to assert what a resolution left behind.
func runtimeConfigSourcesSnapshot() runtimeConfigEnvSources {
	return runtimeConfigEnvSources{
		Flag:     runtimeConfigCopyNames(runtimeConfigFlagEnvNames),
		Settings: runtimeConfigCopyNames(runtimeConfigSettingsEnvNames),
	}
}

// runtimeConfigChannelOf is THE classifier — the one place the five channels are
// decided, so a caller cannot invent a sixth and a test can drive it directly.
//
// The order is precedence order, and each rule is a fact that was MEASURED:
//   - the process holds a value and a flag named it → flag;
//   - the process holds a value and the settings load published it → settings.json;
//   - the process holds a value and neither recorded it → env (the operator's
//     own export, or a parent process's);
//   - the process holds nothing and the document states a value → NOT APPLIED
//     (THE row this surface exists for);
//   - neither → unset (rendered as "(unset)").
//
// A caller may override the third rule's reading with src.Settings having the
// name, which is checked first among the two records, so a name both a flag and
// the document set reads as flag — because that IS what happened.
func runtimeConfigChannelOf(envName, inForce, document string, src runtimeConfigEnvSources) string {
	// THE DECISION IS configreport.Resolve's, not this renderer's (epic
	// 01a1eaec). Every channel — and the in-effect verdict beside it — comes
	// from the one classifier both frontends call. This wrapper only adapts the
	// process-wide source record to the resolver's two booleans.
	flagSet := envName != "" && src.Flag[envName]
	settingsSet := envName != "" && src.Settings[envName]
	return configreport.Resolve("", envName, inForce, document, flagSet, settingsSet).Channel
}

// runtimeConfigRowChannelOf is the classifier PLUS the one refinement a row can
// state and a bare (envName, value, document) triple cannot: a knob NOTHING
// specified may still have a compiled-in default, and naming it is the useful
// answer.
//
// The two columns answer ONE question together and must be read together: the
// CHANNEL says which channel decided (here: none — the code's constant runs),
// the VALUE says what the process holds, and when nothing is held the value cell
// NAMES the constant that runs (runtimeConfigDefaultForValueColumn). Neither
// column alone is the answer: `[built-in default]` with a bare `(unset)` left a
// reader unable to tell 16384 from 0 and made the resolved `agent.max_tokens`
// row look like a contradiction.
//
// A knob with NO compiled-in default keeps `(unset)` in both columns — labelling
// it `[built-in default]` would promise a fallback the code does not have.
func runtimeConfigRowChannelOfResolved(key string, decided configreport.Resolution) string {
	if decided.Channel == runtimeConfigChannelUnset && runtimeConfigBuiltinDefaultOf(key) != "" {
		return runtimeConfigChannelDefault
	}
	return decided.Channel
}

// runtimeConfigDeps is the ONE dependency seam: every source this report reads
// goes through it, so a test can put a known document and a known environment
// in front of the classifier without touching the operator's profile.
type runtimeConfigDeps struct {
	// Getenv is the process environment reader (os.Getenv in production).
	Getenv func(string) string
	// Document returns the settings document's own value for a key. It is
	// called per key, so a reader that only models a few keys is fine.
	Document func(key string) string
	// Sources is what this run's own resolution decided (see
	// runtimeConfigEnvSources).
	Sources runtimeConfigEnvSources
	// DocumentError is the settings document READ failure, when there was one.
	// It is reported as a note rather than raised: a report that refuses to
	// say anything because the file is unreadable is less useful than one that
	// says so and still describes the environment (Rule 15 — the failure is
	// named, never swallowed).
	DocumentError error
}

// liveRuntimeConfigDeps builds the production dependency set: the real
// environment, and a document read from the settings file the process actually
// resolved (config.SettingsPathEnv first, then the default).
//
// A document that cannot be read is NOT fatal here — this is a report, and a
// report that refuses because the file is missing would be less useful than one
// that says every knob is unset. A parse error is reported as a note by the
// builder itself (Rule 15: silence about a broken document is the defect).
func liveRuntimeConfigDeps(known []string) runtimeConfigDeps {
	path := os.Getenv(config.SettingsPathEnv)
	if path == "" {
		path = deepseek.SettingsPath()
	}
	doc := map[string]string{}
	docErr := error(nil)
	if path != "" {
		cfg, err := config.FromSettingsFile(path)
		if err != nil {
			docErr = err
		} else if cfg != nil {
			for _, key := range known {
				doc[key] = settingsDocumentString(cfg, key)
			}
		}
	}
	return runtimeConfigDeps{
		Getenv:        os.Getenv,
		Document:      func(key string) string { return doc[key] },
		Sources:       runtimeConfigSourcesSnapshot(),
		DocumentError: docErr,
	}
}

// runtimeConfigKnownKeys is the settings keys this report reads from the
// document. It exists so the document half is ONE list rather than a per-row
// call site — a row added here without a settings field is reported as the
// document saying nothing, which is the honest answer for a key this build does
// not model.
var runtimeConfigKnownKeys = []string{
	settingsKeyAPIKey,
	settingsKeyBaseURL,
	settingsKeyModel,
	settingsKeyMaxTokens,
	settingsKeyContextWindow,
	settingsKeyThinking,
	settingsKeyReasoningEffort,
	settingsKeyThinkingBudget,
	settingsKeyEffortValues,
	settingsKeyDebug,
	settingsKeyHideThinking,
	settingsKeyKeepThinking,
	settingsKeyTimeout,
	settingsKeyTmpDir,
	settingsKeyGopiHome,
	settingsKeySettingsPath,
}

// The settings keys, declared once (Rules 1/13). They are the settings.json
// spellings, which is also what the report prints in its key column.
const (
	settingsKeyAPIKey          = "api_key"
	settingsKeyBaseURL         = "base_url"
	settingsKeyModel           = "model"
	settingsKeyMaxTokens       = "max_tokens"
	settingsKeyContextWindow   = "context_window"
	settingsKeyThinking        = "thinking"
	settingsKeyReasoningEffort = "reasoning_effort"
	settingsKeyThinkingBudget  = "thinking_budget"
	settingsKeyEffortValues    = "effort_values"
	settingsKeyDebug           = "debug"
	settingsKeyHideThinking    = "hide_thinking"
	settingsKeyKeepThinking    = "keep_thinking"
	settingsKeyTimeout         = "timeout"
	settingsKeyTmpDir          = config.TmpDirSettingsKey
	settingsKeyGopiHome        = "gopi_home"
	settingsKeySettingsPath    = "settings_path"
)

// runtimeConfigSecretKeys names the report's keys that ARE credentials, whatever
// their spelling looks like. It is an exact list on purpose: a CREDENTIAL is a
// fact about the knob, and the alternative — guessing from a substring — was
// measured wrong on the first run (`max_tokens` tripped a "token" substring test
// and printed `<set>`, hiding a limit behind a redaction marker).
var runtimeConfigSecretKeys = []string{settingsKeyAPIKey}

// runtimeConfigSecretSegmentShapes are the LAST underscore-separated SEGMENTS of
// an env name that mark a credential: DEEPSEEK_API_KEY, ANTHROPIC_AUTH_TOKEN,
// a future *_SECRET. Segment-exact, never a substring — `max_tokens` ends in
// "TOKENS", which is a limit.
var runtimeConfigSecretSegmentShapes = []string{"key", "token", "secret", "password"}

// runtimeConfigSpeechBool renders a bool the code's own vocabulary speaks
// (`shown`/`hidden`), so a row's value cell is a word rather than a number AND
// the report cannot describe one state with two words (Rules 1/13/14: the words
// are chatThinkingStateHidden/chatThinkingStateShown, the ONE mapping
// chatThinkingStateWord already publishes).
//
// HIDDEN IS THE POSITIVE SIDE, which is the whole point: `hide_thinking` is true
// when the display is hidden, so the row reads "hidden" exactly when the field
// does.
func runtimeConfigSpeechBool(b bool) string {
	return chatThinkingStateWord(b)
}

// runtimeConfigRunTimeReaders is THE registry of knobs whose LIVE value lives on
// the agent rather than in the environment or the document, and it is the shape
// the 2026-11-22 defect is fixed in: `hide_thinking` and `keep_thinking` are
// rows HERE, not `if`s at a call site, so a third run-time switch is one line
// added to this table instead of a third special case.
//
// THE READER IS ASKED ONLY WHEN A LIVE AGENT EXISTS. A carrier with none (the
// web table's `slashConfigCommand`) has no run-time face to read, so its answer
// comes from the configuration channels exactly as before.
var runtimeConfigRunTimeReaders = map[string]func(*agent.Agent, string, string) string{
	settingsKeyHideThinking: func(ag *agent.Agent, env, document string) string {
		return runtimeConfigDepartingBoolWord(settingsKeyHideThinking, ag.HideThinking, env, document)
	},
	settingsKeyKeepThinking: func(ag *agent.Agent, env, document string) string {
		return runtimeConfigDepartingBoolWord(settingsKeyKeepThinking, ag.KeepThinking, env, document)
	},
}

// runtimeConfigRowValueWord renders the held value of a BOOL row in the
// vocabulary its row speaks, leaving every other value untouched.
//
// WHY THE VALUE COLUMN NEEDS THIS. The environment carries `1` for a switch and
// the document carries `1` too (ApplyEnv publishes exactly that spelling), so a
// reader who set GOPI_HIDE_THINKING=1 saw `1` on the knob row while the command
// that flips the same field said `hidden`. One state, two words, two rows apart.
// A value that is neither a true- nor a false-spelling is printed VERBATIM: an
// unparsable environment value is a fact about the process, not something a
// report may round into a state word.
func runtimeConfigRowValueWord(key, value string) string {
	if value == "" || !config.IsBoolSettingsKey(key) {
		return value
	}
	switch {
	case config.IsTrueValue(value):
		return runtimeConfigBoolWordFor(true, key)
	case config.IsFalseValue(value):
		return runtimeConfigBoolWordFor(false, key)
	}
	return value
}

// runtimeConfigRunTimeOf answers the run-time observation for one knob: "" when
// this carrier has none — and "" ALSO when the live value AGREES with the
// configuration.
//
// THE AGREEMENT RULE IS WHAT KEEPS A SESSION THAT NEVER TOUCHED THE SWITCH
// BYTE-IDENTICAL (acceptance 3). `ag.HideThinking == false` is not an
// observation of a change: it is the state every process starts in, and the
// configuration channels already describe it (the compiled-in default is
// `shown`). Reporting it as a run-time value would relabel every row of every
// untouched session `[this process (run time)]`, which is the regression this
// whole design exists to avoid. Only a value that DEPARTS from what the
// configuration resolves to is a run-time fact.
func runtimeConfigRunTimeOf(ag *agent.Agent, key, env, document string) string {
	if ag == nil {
		return ""
	}
	read, ok := runtimeConfigRunTimeReaders[key]
	if !ok {
		return ""
	}
	return read(ag, env, document)
}

// runtimeConfigDepartingBoolWord renders the run-time value of a BOOL knob in its
// row's vocabulary, or "" when the live field agrees with the configuration.
//
// THE COMPARISON IS IN THE KNOB'S OWN DOMAIN, not between raw strings: the
// environment may spell the same state `1`, `true` or `on`, and a document that
// states `false` must compare equal to a field holding false. Spreading the
// three channels into one bool first (runtimeConfigConfiguredBool) is what makes
// the comparison meaningful, and it is the same precedence ApplyEnv publishes:
// a held environment value wins, else the document, else the compiled-in zero —
// which for both of these switches is `false` (loop.go's EnvBool reads).
func runtimeConfigDepartingBoolWord(key string, live bool, env, document string) string {
	if live == runtimeConfigConfiguredBool(key, env, document) {
		return ""
	}
	return runtimeConfigBoolWordFor(live, key)
}

// runtimeConfigConfiguredBool resolves what the CONFIGURATION says a bool knob
// is: the held environment value when the process carries one, else the
// document's declared value, else false (the Go zero, and the code's own default
// for every bool switch it reads through config.EnvBool).
func runtimeConfigConfiguredBool(key, env, document string) bool {
	if env != "" {
		return runtimeConfigBoolValueOf(key, env)
	}
	return runtimeConfigBoolValueOf(key, document)
}

// runtimeConfigBoolValueOf reads a bool knob's value out of a RAW cell, in
// whichever vocabulary that cell speaks.
//
// IT EXISTS BECAUSE THE VOCABULARY IS TWO-WAY (Rules 1/13/14): the document
// column of a speech knob is rendered as `shown`/`hidden`, so a comparison that
// only understood the boolean spellings would read `hidden` as "not one of the
// true values" — i.e. hide=false — and report the OPPOSITE of what the file
// says. One mapping function, used by the renderer and by the comparison, is
// what keeps those two from disagreeing.
func runtimeConfigBoolValueOf(key, raw string) bool {
	if raw == "" {
		return false
	}
	if runtimeConfigBoolSpeaksSpeech(key) {
		switch raw {
		case chatThinkingStateHidden:
			return true
		case chatThinkingStateShown:
			return false
		}
	}
	return config.IsTrueValue(raw)
}

// settingsDocumentString marshals ONE settings key out of a parsed document.
// The keys are read from the parsed Config, so a key this build does not model
// is reported as absent rather than invented.
//
// A BOOL KEY ANSWERS THREE STATES, NEVER TWO (bug row 01a1ffdd). `false` used to
// render as "" — the same cell as "the document does not state this key at all"
// — so `"hide_thinking": false` and an absent `hide_thinking` were
// indistinguishable in a report whose whole job is to tell those apart. The
// presence question is asked of the document that was decoded
// (config.Config.SettingsCarriesKeyInDecoded), and the VALUE column's word for a
// declared bool comes from runtimeConfigSpeechBool.
func settingsDocumentString(cfg *config.Config, key string) string {
	if cfg == nil {
		return ""
	}
	switch key {
	case settingsKeyAPIKey:
		return cfg.APIKey
	case settingsKeyBaseURL:
		return cfg.BaseURL
	case settingsKeyModel:
		return cfg.Model
	case settingsKeyMaxTokens:
		return runtimeConfigInt(cfg.MaxTokens)
	case settingsKeyContextWindow:
		return runtimeConfigInt(cfg.ContextWindow)
	case settingsKeyThinking:
		if cfg.Thinking == nil {
			return ""
		}
		return string(*cfg.Thinking)
	case settingsKeyReasoningEffort:
		return cfg.ReasoningEffort
	case settingsKeyThinkingBudget:
		return runtimeConfigInt(cfg.ThinkingBudget)
	case settingsKeyEffortValues:
		return strings.Join(cfg.EffortValues, ",")
	case settingsKeyDebug:
		return runtimeConfigDeclaredBoolOf(cfg, key, cfg.Debug)
	case settingsKeyHideThinking:
		return runtimeConfigDeclaredBoolOf(cfg, key, cfg.HideThinking)
	case settingsKeyKeepThinking:
		return runtimeConfigDeclaredBoolOf(cfg, key, cfg.KeepThinking)
	case settingsKeyTimeout:
		return runtimeConfigInt(cfg.TimeoutSec)
	case settingsKeyTmpDir:
		return cfg.TmpDir
	case settingsKeyGopiHome:
		return cfg.GopiHomePath
	}
	// settings_path is deliberately absent: the key LOCATES the document, so the
	// document cannot state it (internal/config's own table renders it as a
	// pointer for the same reason).
	return ""
}

// runtimeConfigDeclaredBoolOf answers the document column of a BOOL key, and the
// answer is three-state by construction:
//
//	the document does not carry the key   -> "" (absent — the honest blank)
//	it carries it as true                 -> the report's bool vocabulary
//	it carries it as false                -> that SAME vocabulary, "not on"
//
// Which spelling is used comes from the vocabulary itself (config.IsFalseValue /
// config.IsTrueValue), never from a literal here, and which words a bool row's
// value column speaks is the report's own decision (runtimeConfigDeclaredBoolWord).
func runtimeConfigDeclaredBoolOf(cfg *config.Config, key string, declared bool) string {
	if !cfg.SettingsCarriesKeyInDecoded(key) {
		return ""
	}
	return runtimeConfigDeclaredBoolWord(declared, key)
}

// runtimeConfigDeclaredBoolWord renders a DECLARED bool for a BOOL key: the
// code's own true/false spelling, taken from boolvocab's canonical lists so this
// report and the resolver cannot disagree about which word means "on".
//
// A bool row whose subject is the SPEECH vocabulary (hide_thinking: the
// reasoning DISPLAY) speaks that vocabulary instead — see
// runtimeConfigBoolWordFor, which owns that decision once.
func runtimeConfigDeclaredBoolWord(declared bool, key string) string {
	if !config.IsBoolSettingsKey(key) {
		// A non-bool key cannot have a declared bool; answering "" keeps the cell
		// honest instead of printing a word the knob's dimension does not have.
		return ""
	}
	if runtimeConfigBoolSpeaksSpeech(key) {
		return runtimeConfigSpeechBool(declared)
	}
	return boolvocab.Canonical(declared)
}

// runtimeConfigBoolWordFor renders a LIVE bool (an env-carried value, a declared
// document value, a field the process holds) in the vocabulary its ROW speaks.
// It is the ONE mapping, so the same knob cannot read `1` in one column and
// `hidden` in another.
func runtimeConfigBoolWordFor(b bool, key string) string {
	return runtimeConfigDeclaredBoolWord(b, key)
}

// runtimeConfigBoolSpeaksSpeech reports whether a knob's value cell speaks the
// reasoning-display vocabulary (`shown` / `hidden`) rather than the generic
// boolean spelling. hide_thinking IS that display switch: it is the same field
// `/thinking` flips, so a report that called it `1` while the command's own
// confirmation said `hidden` would describe one state with two words.
func runtimeConfigBoolSpeaksSpeech(key string) bool {
	return key == settingsKeyHideThinking
}

// runtimeConfigInt renders a positive int, or "" when the document states none
// (0 is the document's own "not stated" for every int knob here).
func runtimeConfigInt(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// buildRuntimeConfigSnapshot reads the LIVE process through the given deps. ag
// may be nil (a carrier with no agent yet); the agent-side rows are then absent
// rather than invented, and the environment half still answers.
func buildRuntimeConfigSnapshot(ag *agent.Agent, deps runtimeConfigDeps) *runtimeConfigSnapshot {
	if deps.Getenv == nil {
		deps.Getenv = os.Getenv
	}
	if deps.Document == nil {
		deps.Document = func(string) string { return "" }
	}
	s := &runtimeConfigSnapshot{Agent: ag}
	add := func(key, envName, document string) {
		// THE THREE SOURCES ANSWERED ONCE, IN ONE CALL (bug row 01a1ffdd): the
		// configuration channels (env / flag / settings.json) and the RUN-TIME
		// observation this process holds, decided together by configreport — so a
		// knob the console can flip is a row of the MODEL, never an `if` at this
		// call site. A knob with no run-time face answers "" and resolves exactly
		// as it did before this change.
		decided := configreport.ResolveRun(
			key, envName, deps.Getenv(envName), document,
			envName != "" && deps.Sources.Flag[envName],
			envName != "" && deps.Sources.Settings[envName],
			runtimeConfigRunTimeOf(s.Agent, key, deps.Getenv(envName), document),
		)
		f := runtimeConfigField{
			Key:         key,
			EnvName:     envName,
			InForce:     runtimeConfigRowValueWord(key, decided.Value),
			Configured:  runtimeConfigRowValueWord(key, deps.Getenv(envName)),
			Document:    document,
			Channel:     runtimeConfigRowChannelOfResolved(key, decided),
			Default:     runtimeConfigDefaultForValueColumn(key, deps.Getenv(envName), document),
			DefaultUnit: runtimeConfigDefaultUnitOf(key),
			DecidedBy:   runtimeConfigDecidedByOf(s.Agent, key, deps.Getenv(envName), document, deps.Getenv),
			Clamp:       runtimeConfigBudgetClampOf(s.Agent, key, deps),
		}
		if f.Clamp != nil && f.Channel != runtimeConfigChannelDefault {
			// A clamped row's number was NOT taken from a compiled-in default,
			// so the default's number must not appear beside it: the cell names
			// the configured value and the effective one, and only those two.
			f.Default = ""
		}
		if f.DecidedBy != "" {
			// A coupled row names NEITHER the compiled-in default NOR
			// `[built-in default]`: that default is exactly what does not run,
			// and saying it does is the contradiction this row used to print.
			// The channel column names the row that decides; the `!` note below
			// the table states the mechanism in words.
			f.Default = ""
			f.Channel = runtimeConfigDecidedByChannel(f.DecidedBy)
		}
		s.Fields = append(s.Fields, f)
	}

	// --- the model wire: the knobs whose values reach the provider ----------
	// Each is read from the ENVIRONMENT, because that is the channel every
	// reader in internal/agent and internal/deepseek actually consults.
	add(settingsKeyAPIKey, "ANTHROPIC_AUTH_TOKEN", deps.Document(settingsKeyAPIKey))
	add(settingsKeyBaseURL, "ANTHROPIC_BASE_URL", deps.Document(settingsKeyBaseURL))
	add(settingsKeyModel, "ANTHROPIC_MODEL", deps.Document(settingsKeyModel))
	add(settingsKeyMaxTokens, "GOPI_MAX_TOKENS", deps.Document(settingsKeyMaxTokens))
	add(settingsKeyContextWindow, "GOPI_CONTEXT_WINDOW", deps.Document(settingsKeyContextWindow))
	add(settingsKeyThinking, "GOPI_THINKING", deps.Document(settingsKeyThinking))
	add(settingsKeyReasoningEffort, "GOPI_REASONING_EFFORT", deps.Document(settingsKeyReasoningEffort))
	add(settingsKeyThinkingBudget, "GOPI_THINKING_BUDGET", deps.Document(settingsKeyThinkingBudget))

	// --- the run's shape ----------------------------------------------------
	add(settingsKeyEffortValues, "GOPI_EFFORT_VALUES", deps.Document(settingsKeyEffortValues))
	add(settingsKeyDebug, "GOPI_DEBUG", deps.Document(settingsKeyDebug))
	add(settingsKeyKeepThinking, "GOPI_KEEP_THINKING", deps.Document(settingsKeyKeepThinking))
	add(settingsKeyHideThinking, "GOPI_HIDE_THINKING", deps.Document(settingsKeyHideThinking))
	add(settingsKeyTimeout, "GOPI_TIMEOUT_SEC", deps.Document(settingsKeyTimeout))

	// --- the profile's paths ------------------------------------------------
	// These two are not settings-fed knobs: they LOCATE the document and the
	// data root, so their channel is whatever the process carries.
	add(settingsKeyGopiHome, config.GopiDirEnv, deps.Document(settingsKeyGopiHome))
	// WHICH DOCUMENT WAS READ. Without this row a reader who compares this report
	// with `gopi config show` can be comparing two DIFFERENT FILES and never know
	// it: GOPI_SETTINGS (or --settings) redirects the read, and an inherited value
	// from a parent process is invisible in every other column. Measured while
	// writing this file — a probe whose HOME named one settings.json was in fact
	// reading another, and only this row would have said so.
	add(settingsKeySettingsPath, config.SettingsPathEnv, "")
	// THE TEMP ROOT IS REPORTED FROM ITS PUBLISHED PAIR, not from a re-ranking
	// of its channels. This knob has an input name (GOPI_TMPDIR / --tmpdir / the
	// settings key) and an OUTPUT name (TMPDIR, which every child inherits), and
	// the publisher writes the value AND the label beside it
	// (scratch.TmpDirSourceEnvName) precisely so a later reader does not re-rank
	// a world the publication itself changed — the name that lost is the name
	// whose value was overwritten, so a second ranking reports the wrong channel
	// BY CONSTRUCTION (internal/scratch/tmproot.go says this at the constant).
	//
	// Measured on the first real run of this command: reading only GOPI_TMPDIR
	// printed `tmpdir … NOT APPLIED` on a host where the temp root was set and in
	// force — a false alarm in the one column whose entire value is that it never
	// cries wolf.
	s.addTmpDir(deps)

	// --- the RESOLVED agent fields: what the request will consume -----------
	// A row here can disagree with the row above (the env says
	// reasoning_effort=high while the agent's resolved effort is empty because
	// thinking is off). That difference is this report's most valuable
	// statement, and it is a NOTE below the table rather than a silent
	// contradiction between two columns.
	if ag != nil {
		budgetRow := runtimeConfigField{Key: runtimeConfigKeyAgentThinkingBudget, InForce: runtimeConfigBudgetOf(ag.Thinking), Channel: runtimeConfigChannelAgent,
			// The resolved row is the SECOND of the two clamped rows: the number
			// the agent holds is the number the builder passes through
			// deepseek.EffectiveThinkingBudget, so its cell must name the
			// effective value too — otherwise the two rows disagree and the
			// reader is back to 16384 on one line and 8192 on the wire.
			Clamp: runtimeConfigBudgetClampOf(ag, runtimeConfigKeyAgentThinkingBudget, deps)}
		s.Fields = append(s.Fields,
			runtimeConfigField{Key: "agent.model", InForce: ag.Model, Channel: runtimeConfigChannelAgent},
			runtimeConfigField{Key: "agent.reasoning_effort", InForce: ag.ReasoningEffort, Channel: runtimeConfigChannelAgent},
			runtimeConfigField{Key: "agent.thinking", InForce: runtimeConfigThinkingOf(ag.Thinking), Channel: runtimeConfigChannelAgent},
			budgetRow,
			runtimeConfigField{Key: "agent.max_tokens", InForce: strconv.Itoa(ag.MaxTokens), Channel: runtimeConfigChannelAgent},
			runtimeConfigField{Key: "agent.context_window", InForce: strconv.Itoa(ag.ContextWindow), Channel: runtimeConfigChannelAgent},
			// THE TWO ROWS THAT MAKE THIS SECTION THE WHOLE RESOLVED AGENT
			// (bug row 01a1ffdd). Without them a reader could see the knob rows
			// (`hide_thinking`, `keep_thinking`) and the resolved `agent.*` block
			// and never learn that these two switches ARE agent fields — which is
			// why a console command could change one and reach no row at all.
			// The words come from the SAME vocabulary the knob rows and
			// `/thinking` use (runtimeConfigBoolWordFor → chatThinkingStateWord).
			runtimeConfigField{Key: runtimeConfigKeyAgentHideThinking, InForce: runtimeConfigBoolWordFor(ag.HideThinking, settingsKeyHideThinking), Channel: runtimeConfigChannelAgent},
			runtimeConfigField{Key: runtimeConfigKeyAgentKeepThinking, InForce: runtimeConfigBoolWordFor(ag.KeepThinking, settingsKeyKeepThinking), Channel: runtimeConfigChannelAgent},
		)
	}

	s.Notes = runtimeConfigNotesOf(ag, s.Fields)
	if deps.DocumentError != nil {
		s.Notes = append(s.Notes, fmt.Sprintf(
			"the settings document could not be read (%v): every document column below is blank, and a NOT APPLIED row cannot be told apart from a knob the file never stated", deps.DocumentError))
	}
	return s
}

// addTmpDir appends the temp-root row from the PAIR the publisher wrote: the
// value in TMPDIR and the channel label beside it. The label is mapped back to
// this report's channel vocabulary, so the two surfaces cannot disagree about
// where the directory came from.
func (s *runtimeConfigSnapshot) addTmpDir(deps runtimeConfigDeps) {
	value := deps.Getenv(scratch.TmpDirEnvName)
	source := deps.Getenv(scratch.TmpDirSourceEnvName)
	f := runtimeConfigField{
		Key:      settingsKeyTmpDir,
		EnvName:  scratch.TmpDirEnvName,
		InForce:  value,
		Document: deps.Document(settingsKeyTmpDir),
		Channel:  runtimeConfigChannelOf(scratch.GopiTmpDirEnvName, deps.Getenv(scratch.GopiTmpDirEnvName), deps.Document(settingsKeyTmpDir), deps.Sources),
	}
	// THE LABEL WINS when the publisher wrote one: it is the measured answer,
	// and the classifier above is only the fallback for a process whose temp
	// root was never published (a unit test, a carrier started before the
	// resolution). An unrecognised label is reported verbatim rather than
	// silently reclassified — a channel nobody can name is still a fact.
	if source != "" {
		f.Channel = runtimeConfigTmpDirChannelOf(source, f.Channel)
	}
	s.Fields = append(s.Fields, f)
}

// runtimeConfigTmpDirChannelOf maps the publisher's label to this report's
// vocabulary. The four labels are internal/config's own constants, so a rename
// is one edit rather than a silent "unknown".
func runtimeConfigTmpDirChannelOf(source, fallback string) string {
	switch source {
	case config.TmpDirSourceFlag:
		return runtimeConfigChannelFlag
	case config.TmpDirSourceEnv:
		return runtimeConfigChannelEnv
	case config.TmpDirSourceAmbient:
		// The caller's own TMPDIR — an environment channel by this report's
		// vocabulary, and the label distinguishes it for the note below.
		return runtimeConfigChannelEnv
	case config.TmpDirSourceSettings:
		return runtimeConfigChannelSettings
	}
	if source != "" {
		return source
	}
	return fallback
}

// runtimeConfigChannelAgent labels a row the AGENT resolved. It is not a
// precedence channel: it says "this is what the agent will send", which is a
// different question from "which channel decided the variable".
const runtimeConfigChannelAgent = "agent"

// runtimeConfigDefaultForValueColumn answers "what value belongs in this row's
// VALUE cell, given that the code may have a compiled-in default". It returns ""
// — meaning "print the held value, or `(unset)`" — unless BOTH hold:
//
//   - the process holds nothing AND the document states nothing (the row's
//     channel is `built-in default`), and
//   - the code actually HAS a default for this key.
//
// That pair is THE defect this cell exists to close: `max_tokens (unset)
// [built-in default]` told a reader that nothing was set while refusing to say
// what would run, and the row three lines below (`agent.max_tokens 16384`) then
// looked like a contradiction rather than the same fact twice.
//
// A NOT APPLIED row is deliberately NOT named here: its own `!` note states the
// default in words, together with the far more important fact that the value the
// document states is dead. Naming a default beside a dead value in the same cell
// would blur the one distinction that note draws.
// runtimeConfigDecidedByOf answers "which OTHER row of this table decides this
// knob's value", returning that row's KEY or "" for the ordinary uncoupled case.
//
// IT IS THE FIX FOR THE 2026-11-13 CONTRADICTION (bug row 01a1d16d-9ce7): the
// `thinking` row claimed `(unset), disabled runs [built-in default]` while the
// `agent.thinking` row twenty lines below printed `enabled`. Both were read from
// the same process, in the same render — the value column had asserted a
// RESOLVED fact ("disabled runs") on a knob another row decides, which is the one
// thing the value column must never do: it states what the environment, the
// document or the compiled-in constant HOLDS, never what the resolver concludes.
//
// The predicate is the resolver's own (agent.ThinkingImpliedByEffort), so this
// report cannot drift from the behaviour it describes. The three facts it needs
// are exactly the row's own: nothing in the environment, nothing in the document,
// and an effort that implies thinking.
func runtimeConfigDecidedByOf(ag *agent.Agent, key, inForce, document string, getenv func(string) string) string {
	if key != settingsKeyThinking || inForce != "" || document != "" {
		return ""
	}
	if !agent.ThinkingImpliedByEffort(getenv) {
		return ""
	}
	// THE AGENT'S OWN ANSWER WINS WHEN THERE IS ONE. A resolved agent in hand means
	// this process ALREADY ran the switch, and its verdict is the ground truth a
	// prediction must not overrule: the environment can only be read as "the
	// coupling WOULD hold", while the agent says whether it DID. The two agree in
	// production (the report reads the same environment the agent was built in);
	// where they can differ — a fixture, a resumed run — the observation is the
	// answer and the prediction stays silent.
	if ag != nil && runtimeConfigThinkingDisabled(ag) {
		return ""
	}
	return settingsKeyReasoningEffort
}

// runtimeConfigDecidedByChannel renders the channel column of a coupled row: it
// names the row that decides, because "decided by reasoning_effort" is the true
// answer and `built-in default` is a false one (that default does not run).
func runtimeConfigDecidedByChannel(decidingKey string) string {
	return runtimeConfigChannelDecidedByPrefix + decidingKey
}

func runtimeConfigDefaultForValueColumn(key, inForce, document string) string {
	if inForce != "" || document != "" {
		return ""
	}
	return runtimeConfigBuiltinDefaultOf(key)
}

// runtimeConfigBuiltinDefaultOf states the compiled-in default for a key, so a
// row nothing decided can name what the code will actually do rather than say
// nothing. It is a REPORT of constants that live elsewhere — never a second
// copy of the value (each is the real constant or the real resolver's answer).
func runtimeConfigBuiltinDefaultOf(key string) string {
	switch key {
	case settingsKeyMaxTokens:
		return strconv.Itoa(deepseek.DefaultClaudeMaxTokens)
	case settingsKeyContextWindow:
		return strconv.Itoa(agent.DefaultContextWindow)
	case settingsKeyThinking:
		return thinkmode.Disabled
	case settingsKeyReasoningEffort:
		// The resolver's own baseline, not a literal: asking the ONE function
		// what it answers for "nothing asked" keeps this report from drifting
		// from the behaviour it describes.
		resolved, _ := config.ResolveReasoningEffort("", nil)
		return resolved
	case settingsKeyThinkingBudget:
		return strconv.Itoa(deepseek.DefaultClaudeThinkingBudget)
	case settingsKeyEffortValues:
		// The list `resolveReasoningEffort` actually falls back to, asked of the
		// package that owns it — never a literal retyped here (Rules 13/14).
		// Without this case the row printed `(unset)` in BOTH columns while a
		// real whitelist was deciding what effort values are legal, so a reader
		// could not answer "is `high` valid?" — the one question this row exists
		// to answer.
		return strings.Join(config.DefaultEffortValues(), runtimeConfigEffortValuesSeparator)
	case settingsKeyTimeout:
		return "0"
	}
	return ""
}

// runtimeConfigDefaultUnitOf names the unit of a knob's compiled-in default,
// carried on the FIELD so the shared value template never hard-codes one.
//
// A unit belongs to the knob's DIMENSION, not to the template that happens to
// render it: the same template serves token budgets, counts, ratios and
// character lengths. Only the two token-dimension knobs in this fix's scope get
// a unit here — `context_window` (a token budget) and `max_tokens` (an output
// cap). Every other knob returns "" and renders a bare number. The bar for
// adding a unit is deliberately high: a knob left out of scope keeps a bare
// number rather than a guessed one, because a wrong unit is worse than none.
// This is why `thinking`, `timeout`, `effort_values` and every resolved
// `agent.*` row carry no unit; `thinking_budget` is token-dimension too but was
// explicitly left out of this fix's two-knob scope.
//
// The leading space is part of the unit: the shared template is "%s, %s%s", so
// a unit-carrying row reads `(unset), 200000 token` while a dimensionless one
// reads `(unset), 8` — neither carries a stray space.
func runtimeConfigDefaultUnitOf(key string) string {
	switch key {
	case settingsKeyMaxTokens, settingsKeyContextWindow:
		return runtimeConfigUnitTokens
	}
	return ""
}

// runtimeConfigNotesOf states the couplings the values alone do not say.
func runtimeConfigNotesOf(ag *agent.Agent, fields []runtimeConfigField) []string {
	var notes []string
	// THE CLAMP, stated once. Both budget rows carry the same clamp record (the
	// knob the operator set and the field the agent resolved are the same number
	// passed through the same function), so the note is de-duplicated by its own
	// numbers: two identical sentences about one fact would read as two facts.
	clampSeen := map[string]bool{}
	for _, f := range fields {
		if f.Clamp == nil {
			continue
		}
		sig := fmt.Sprintf("%d/%d/%d", f.Clamp.Configured, f.Clamp.Effective, f.Clamp.MaxTokens)
		if clampSeen[sig] {
			continue
		}
		clampSeen[sig] = true
		notes = append(notes, fmt.Sprintf(runtimeConfigClampNoteFormat,
			f.Key, f.Clamp.Configured, f.Clamp.Effective, f.Clamp.MaxTokens))
	}
	for _, f := range fields {
		if f.Channel != runtimeConfigChannelNotApplied {
			continue
		}
		// The row this whole surface exists for, said in words as well as in
		// the channel column: the document states it, the process does not
		// hold it, so the code's default is what runs.
		notes = append(notes, fmt.Sprintf(
			"%s is stated by the settings document but NOT held by this process — the default (%s) is what runs",
			f.Key, runtimeConfigBuiltinDefaultOrUnknown(f.Key)))
	}
	for _, f := range fields {
		if f.Channel != runtimeConfigChannelRunning {
			continue
		}
		// THE ROW A CONSOLE COMMAND JUST CHANGED, in words. The channel column
		// already says `this process (run time)`; this line adds the two facts
		// that column cannot carry — that nothing was written back, and what the
		// configuration therefore still says. Without the second half a reader
		// comparing `/config` with `gopi config show` has to guess why the two
		// processes disagree about the same knob.
		notes = append(notes, fmt.Sprintf(runtimeConfigRunningNoteFormat,
			f.Key, f.InForce, runtimeConfigConfigurationSays(f)))
	}
	for _, f := range fields {
		if f.Channel != runtimeConfigChannelEnv || f.Document == "" || f.Document == f.InForce {
			continue
		}
		// The OTHER direction of the same question: the process carries a value
		// and the document disagrees. Nothing is dead here — the value IS in
		// force — but a reader comparing this report with the file would
		// otherwise have to spot the difference in a column this table does not
		// print.
		//
		// BOTH values go through the ONE redaction decision before they reach
		// %q. This note is printed and copied into run logs exactly like the
		// value column, so a credential here would leak even though the cell
		// itself shows <set>. Routing through redactIfSecret (the same function
		// the value column calls) keeps the decision in one place (Rules 1/13/14).
		notes = append(notes, fmt.Sprintf(
			"%s: the process carries %q while the settings document states %q — the environment wins, and the file is not what this run is using",
			f.Key, redactIfSecret(f.Key, f.InForce), redactIfSecret(f.Key, f.Document)))
	}
	// The COUPLED row, in words. It is stated even when no agent is in hand: the
	// coupling is decided by the ENVIRONMENT, so a carrier that resolved nothing
	// (`/config` on the web/server table) still knows thinking will not be the
	// compiled-in default.
	byKey := make(map[string]runtimeConfigField, len(fields))
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for _, f := range fields {
		if f.DecidedBy == "" {
			continue
		}
		// The value of the DECIDING row, read from the table itself rather than
		// from the environment a second time: the two must not disagree, and the
		// row is already the report's own record of it.
		decidingKey := f.DecidedBy
		deciding := byKey[decidingKey].InForce
		// What the request will carry. The agent's own resolution when one is in
		// hand; otherwise the fact that the coupling IMPLIES enabled (that is
		// what runtimeConfigDecidedByOf measured) — never the compiled-in
		// default, which is precisely the claim this note corrects.
		effective := thinkmode.Enabled
		if ag != nil {
			effective = runtimeConfigThinkingOf(ag.Thinking)
		}
		notes = append(notes, fmt.Sprintf(runtimeConfigCoupledNoteFormat,
			f.Key, decidingKey, redactIfSecret(decidingKey, deciding), runtimeConfigBuiltinDefaultOrUnknown(f.Key),
			config.EffortOff, agent.ThinkingEnvName, f.Key, effective))
	}
	if ag == nil {
		return notes
	}
	if runtimeConfigThinkingDisabled(ag) {
		if ag.ReasoningEffort != "" {
			notes = append(notes, fmt.Sprintf(
				"thinking is OFF while reasoning_effort=%q is set: the request builder CLEARS effort whenever thinking is disabled, so the effort never reaches the wire", redactIfSecret(settingsKeyReasoningEffort, ag.ReasoningEffort)))
			return notes
		}
		notes = append(notes, "thinking is OFF: every request is sent thinking=disabled, so reasoning_effort is absent from the wire")
	}
	return notes
}

// runtimeConfigBudgetClampOf answers "is this row's number the one that reaches
// the provider" — and it answers it by CALLING the builder's own function,
// never by re-deriving the min() here.
//
// WHY THIS ROW EXISTS (measured 2026-11-14): the table printed
// `thinking_budget  16384` and `agent.thinking.budget_tokens  16384` while every
// request body in the same run carried `"budget_tokens":8192` — the builder caps
// the budget at max_tokens/2 (deepseek.EffectiveThinkingBudget, the ONE
// definition point, Rule 2/14). A reader budgeting context from this column was
// out by a factor of two, and 8192 is also the number the harness aborts a round
// at, so the table was hiding the two facts a reader needs.
//
// THE THREE PREMISES, each measured rather than assumed:
//
//  1. The clamp happens ONLY while thinking is enabled — the builder sets
//     BudgetTokens inside `if thinking.Type != thinkmode.Disabled`
//     (claude_client.go). With thinking off the budget never reaches the wire
//     at all, so reporting "8192" there would be a third false number.
//  2. It needs a RESOLVED agent. `max_tokens` and the budget the agent will
//     send are the agent's own fields; without one (the web table's carrier,
//     `slashConfigCommand` with a nil agent) this report would be inventing the
//     denominator. Widening the search to the environment is the second
//     resolution Rule 2 forbids.
//  3. A row the process does not HOLD carries no wire number: `configured <= 0`
//     means nothing named this knob, and a clamp statement about a value that is
//     not in force would be prose about a hypothetical.
func runtimeConfigBudgetClampOf(ag *agent.Agent, key string, deps runtimeConfigDeps) *runtimeConfigClamp {
	if ag == nil || runtimeConfigThinkingDisabled(ag) {
		return nil
	}
	if !runtimeConfigIsBudgetKey(key) {
		return nil
	}
	configured := runtimeConfigConfiguredBudgetOf(ag, key, deps)
	if configured <= 0 {
		return nil
	}
	maxTokens := ag.MaxTokens
	if maxTokens <= 0 {
		// The constructor always sets it; a hand-built literal may not. Falling
		// back to the code's own constant is the honest denominator, and it is
		// what the request builder would receive from such an agent anyway.
		maxTokens = deepseek.DefaultClaudeMaxTokens
	}
	effective := deepseek.EffectiveThinkingBudget(maxTokens, configured)
	if effective >= configured {
		return nil
	}
	return &runtimeConfigClamp{Configured: configured, Effective: effective, MaxTokens: maxTokens}
}

// runtimeConfigConfiguredBudgetOf reads the CONFIGURED number for one of the two
// budget rows, from the row's own source and from nowhere else:
//
//   - the knob row (`thinking_budget`) is the ENVIRONMENT's number — that is the
//     only reader internal/agent has for it (loop.go), so the document's value
//     is reported by the channel column, never here;
//   - the resolved row (`agent.thinking.budget_tokens`) is the AGENT's field,
//     which is the number the request builder will pass through the clamp.
func runtimeConfigConfiguredBudgetOf(ag *agent.Agent, key string, deps runtimeConfigDeps) int {
	if key == runtimeConfigKeyAgentThinkingBudget {
		if ag.Thinking == nil {
			return 0
		}
		return ag.Thinking.BudgetTokens
	}
	raw := deps.Getenv(settingsEnvThinkingBudget)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		// An unparsable value is not a configured budget: internal/agent warns
		// and falls back to the default, which cannot be clamped at the shipped
		// caps — and reporting a clamp off a garbage number would be a fiction.
		return 0
	}
	return n
}

// runtimeConfigIsBudgetKey reports whether key is one of the two rows the
// builder clamps (Rule 1: the membership lives in one declared list).
func runtimeConfigIsBudgetKey(key string) bool {
	for _, k := range runtimeConfigBudgetKeys {
		if k == key {
			return true
		}
	}
	return false
}

// runtimeConfigBuiltinDefaultOrUnknown renders the default for a key, or a
// phrase naming that the value is the code's own when there is no single
// constant to point at.
func runtimeConfigBuiltinDefaultOrUnknown(key string) string {
	if d := runtimeConfigBuiltinDefaultOf(key); d != "" {
		return d
	}
	return "the code's own default"
}

// runtimeConfigThinkingDisabled reports whether the agent's resolved thinking is
// the disabled state (a nil Thinking is the disabled default — the agent's
// constructor always sets one, a hand-built literal may not).
func runtimeConfigThinkingDisabled(ag *agent.Agent) bool {
	return ag.Thinking == nil || ag.Thinking.Type == thinkmode.Disabled
}

// runtimeConfigThinkingOf renders the agent's thinking object without importing
// its shape into the report's vocabulary.
func runtimeConfigThinkingOf(t *deepseek.Thinking) string {
	if t == nil {
		return thinkmode.Disabled
	}
	return string(t.Type)
}

// runtimeConfigBudgetOf renders the thinking budget, "" when there is no
// thinking object to carry one.
func runtimeConfigBudgetOf(t *deepseek.Thinking) string {
	if t == nil || t.BudgetTokens <= 0 {
		return ""
	}
	return strconv.Itoa(t.BudgetTokens)
}

// runtimeConfigConfigurationSays answers, in the report's own value vocabulary,
// what the configuration channels state for a knob whose run-time value is in
// force: the document's value when it states one, else the environment's
// configured value, else the phrase for "nothing".
//
// IT IS DERIVED FROM THE ROW, not re-read from the environment: the row already
// carries both, and a second read could disagree with the cell above it (Rule 2).
func runtimeConfigConfigurationSays(f runtimeConfigField) string {
	if f.Document != "" {
		return fmt.Sprintf("%q", redactIfSecret(f.Key, f.Document))
	}
	if f.Configured != "" {
		return fmt.Sprintf("%q", redactIfSecret(f.Key, f.Configured))
	}
	return runtimeConfigConfigurationSaysWord
}

// renderRuntimeConfigSnapshot is the ONE renderer both doors call: the REPL arm
// and the shared table's answer are the same bytes.
func renderRuntimeConfigSnapshot(s *runtimeConfigSnapshot) []string {
	if s == nil {
		return nil
	}
	lines := []string{runtimeConfigTitle, ""}
	width := 0
	for _, f := range s.Fields {
		if len(f.Key) > width {
			width = len(f.Key)
		}
	}
	for _, f := range s.Fields {
		value := f.InForce
		switch {
		case f.Clamp != nil:
			// THE ROW THIS CELL EXISTS FOR: the number that reaches the
			// provider first, the configured number beside it. Printing only one
			// of the two is exactly the defect the clamp note repairs — 16384
			// alone is a wire number the reader will never see.
			value = fmt.Sprintf(runtimeConfigClampedValueFormat, f.Clamp.Effective, f.Clamp.Configured)
		case value == "" && f.Default != "":
			// The cell states BOTH facts, because either alone is a different
			// (and misleading) answer: the process holds nothing, and this is
			// what runs anyway. The unit comes from the FIELD, not this shared
			// template — only the two token knobs carry one; a dimensionless
			// knob renders a bare number with no invented predicate.
			value = fmt.Sprintf(runtimeConfigUnsetDefaultFormat, runtimeConfigChannelUnset, f.Default, f.DefaultUnit)
		case value == "":
			value = runtimeConfigChannelUnset
		default:
			value = redactIfSecret(f.Key, value)
		}
		lines = append(lines, fmt.Sprintf("  %-*s  %s  [%s]", width, f.Key, value, f.Channel))
	}
	if len(s.Notes) > 0 {
		lines = append(lines, "")
		for _, n := range s.Notes {
			lines = append(lines, runtimeConfigNoteMarker+n)
		}
	}
	return lines
}

// redactIfSecret keeps credentials out of a report that is printed to a
// terminal and copied into a log. The NAME decides, never the value: a key
// whose name is credential-shaped is redacted even when its content looks
// harmless, because "harmless" is a judgement this renderer has no basis for.
func redactIfSecret(key, value string) string {
	if value == "" || value == runtimeConfigChannelUnset {
		return value
	}
	for _, name := range runtimeConfigSecretKeys {
		if strings.EqualFold(key, name) {
			return runtimeConfigSeedRedacted
		}
	}
	// SEGMENT matching, not substring: normalize the env-name spelling
	// (ANTHROPIC_AUTH_TOKEN) to the report's key spelling (anthropic_auth_token)
	// and compare the LAST segment. That is what keeps `max_tokens` visible while
	// `gopi_audio_api_key` is not.
	segments := strings.Split(strings.ToLower(strings.ReplaceAll(key, "-", "_")), "_")
	last := segments[len(segments)-1]
	for _, shape := range runtimeConfigSecretSegmentShapes {
		if last == shape {
			return runtimeConfigSeedRedacted
		}
	}
	return value
}
