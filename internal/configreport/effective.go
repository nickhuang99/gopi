// Package configreport is the ONE parsing/decision layer behind every surface
// that reports configuration: chat's `/config` and `gopi config show`.
//
// WHY THIS EXISTS (epic 01a1eaec, 2026-11-18). Three defects in one day came
// from the same root: a REPORT inferred a knob's value or effect instead of
// asking one function that owns the decision.
//
//	01a1e6c7  a token count was rendered "200000 runs" (a renderer guess);
//	01a1e7e9  a credential was printed with %q (a renderer guess);
//	01a1eaec  `gopi config show` called the LIVE `messages_path` key IGNORED,
//	          because it asked a struct-reflection list that did not include a
//	          key read by a DIFFERENT package (internal/deepseek).
//
// The rule this package enforces (Rules 2/13/14): a knob's EFFECTIVE VALUE, its
// SOURCE CHANNEL and its IN-EFFECT verdict are decided in exactly ONE place.
// Both frontends consume it; only their PRESENTATION differs (chat annotates
// [channel] for a human, the CLI renders stable text for a script). Run-scoped
// rows are marked, so a reader never mistakes "this run resolved it" for
// "the configuration file says it".
//
// This package is a LEAF: no project imports (Rule 14), so config, deepseek and
// cmd/agent can all depend on it without a cycle.
package configreport

// The channel vocabulary — the single definition (Rules 1/13/14). cmd/agent
// aliases these names; it does not re-spell the literals. The chat `/config`
// help page and both renderers compare against these exact values.
const (
	ChannelFlag            = "flag"
	ChannelEnv             = "env"
	ChannelSettings        = "settings.json"
	ChannelDefault         = "built-in default"
	ChannelNotApplied      = "NOT APPLIED"
	ChannelUnset           = "(unset)"
	ChannelAgent           = "agent"
	ChannelDecidedByPrefix = "decided by "

	// ChannelRunning is the THIRD source position: a value THIS PROCESS set at
	// run time, which no external channel (a flag, an export, the document)
	// carries at all.
	//
	// WHY IT IS ITS OWN CHANNEL AND NOT A ROW-LEVEL `if` (epic 01a1ffdd): a
	// switch the chat console can flip lives on a live struct field, not in the
	// environment, and a report that special-cased ONE such row would leave the
	// next run-time-variable switch reading `(unset)` again. Measured
	// 2026-11-22: after `/thinking off`, `/config` still printed
	// `hide_thinking (unset)` under a title promising "read from this RUNNING
	// process". The source position, not the row, is what was missing.
	//
	// WHAT IT MAY NOT SAY. It is NOT `env`, NOT `settings.json`, and not the
	// agent's resolved-field channel: `/thinking off` deliberately writes no
	// configuration (chat_thinking.go's own rule), so naming a configuration
	// channel here would be a lie about where the value came from. It also never
	// replaces a row's ORIGINAL channel when the run-time value still agrees with
	// it — only a value that DEPARTS from what the external channels say is
	// reported as this one.
	ChannelRunning = "this process (run time)"
)

// Resolution is ONE knob's answer, shared by both frontends. It is the data
// model the two renderers format; nothing in a renderer re-derives these
// fields.
type Resolution struct {
	// Key is the knob name (the settings.json key, or a resolved agent field).
	Key string
	// Value is the value actually in force in this process ("" when held
	// nothing). This is the "effective value" the two frontends must agree on.
	Value string
	// SettingsValue is what the settings document states for this key ("" when
	// the document states none). It is kept separate from Value so a NOT APPLIED
	// row can show both.
	SettingsValue string
	// EnvName is the environment variable this knob is read from ("" for a
	// resolved agent row or a settings-only key).
	EnvName string
	// Channel is what decided Value, one of the Channel* constants.
	Channel string
	// InEffect answers "is this knob actually in force?" — the verdict the CLI
	// used to INFER (and got wrong for messages_path). It is decided here, from
	// Value, never guessed by a renderer from whether it recognises the key.
	InEffect bool
	// RunScoped marks a value that exists only for THIS run (an agent-resolved
	// field, a value this run loaded and published, or a value this process
	// changed at run time). A reader comparing the chat surface with the CLI
	// must be told such a row is run-scoped.
	RunScoped bool
	// Runtime is the value the RUNNING process holds for this knob when that is
	// a fact no configuration channel carries — "" when this caller has no such
	// observation (the CLI, a unit test, a carrier with no live agent).
	//
	// IT IS A FIELD RATHER THAN A PARAMETER OF Resolve ON PURPOSE: every caller
	// of the configuration channels keeps calling the same signature it always
	// called, and a caller that CAN see the live state opts in by handing it
	// over. Resolution then decides ONE thing — whether that observation
	// departs from the external channels — and that decision has one definition
	// point, here (Rules 2/13/14).
	Runtime string
}

// Resolve is THE classifier: given the facts one caller gathered, answer every
// field of a Resolution. Both frontends call THIS function, so they cannot
// disagree about a knob's value or in-effect verdict.
//
// The precedence is measured and deliberate:
//   - a value the process HOLDS, decided by a CLI flag      -> ChannelFlag
//   - a value the process HOLDS, published from the document -> ChannelSettings
//   - a value the process HOLDS, from neither                -> ChannelEnv
//   - the process holds nothing, document states a value     -> ChannelNotApplied
//     (a DEAD value on a knob whose only reader is os.Getenv)
//   - neither                                                -> ChannelUnset
//
// flagSet / settingsSet record what THIS run's own resolution decided for
// EnvName; a caller with no such record passes false (the honest answer is
// then ChannelEnv for a held value, ChannelNotApplied for a document-only one).
func Resolve(key, envName, inForce, settingsValue string, flagSet, settingsSet bool) Resolution {
	r := Resolution{
		Key:           key,
		Value:         inForce,
		SettingsValue: settingsValue,
		EnvName:       envName,
		InEffect:      inForce != "",
	}
	switch {
	case inForce != "" && envName != "" && flagSet:
		r.Channel = ChannelFlag
		r.RunScoped = true
	case inForce != "" && envName != "" && settingsSet:
		r.Channel = ChannelSettings
		r.RunScoped = true
	case inForce != "":
		r.Channel = ChannelEnv
	case settingsValue != "":
		r.Channel = ChannelNotApplied
	default:
		r.Channel = ChannelUnset
	}
	return r
}

// ResolveRun is THE classifier for a knob the RUNNING process may also hold at
// run time. It answers first the ordinary channel question (Resolve, unchanged),
// then ONE more:
//
//   - the run-time value is absent            → the configuration answer stands;
//   - it AGREES with the channel answer       → that channel stands, and the row
//     says so — a value the process set keeps no second label for a departure it
//     did not make, so a session that never touched the switch renders exactly
//     as it did before;
//   - it DEPARTS from the channel answer      → ChannelRunning, and the value is
//     the run-time one.
//
// The comparison is against the channel's ANSWER, not the raw inputs, because
// that answer already folded precedence (a flag beats an export beats the
// document) and re-deciding it here would be the second resolution Rule 2
// forbids. `(unset)` is compared as the empty answer it stands for: a run-time
// value where nothing else named the knob IS a departure, and that is precisely
// the 2026-11-22 row (`/thinking off` under an empty environment).
//
// Every answer carries Runtime and RunScoped so a presenter can state the fact
// without re-deriving it.
func ResolveRun(key, envName, inForce, settingsValue string, flagSet, settingsSet bool, runtime string) Resolution {
	r := Resolve(key, envName, inForce, settingsValue, flagSet, settingsSet)
	r.Runtime = runtime
	if runtime == "" {
		return r
	}
	channelValue := r.Value
	if r.Channel == ChannelUnset || r.Channel == ChannelDefault || r.Channel == ChannelNotApplied {
		// No channel HOLDS a value: the knob's answer is "nothing", so a
		// run-time observation is by construction a departure from it.
		channelValue = ""
	}
	if runtime == channelValue {
		// AGREEMENT: the run-time observation is the same value the channel
		// already states. The channel keeps the column (it is still the true
		// source of that value) — but the row is run-scoped, because the process
		// holding it is a fact about this run, not about the file.
		r.RunScoped = true
		return r
	}
	r.Channel = ChannelRunning
	r.Value = runtime
	r.InEffect = true
	r.RunScoped = true
	return r
}
