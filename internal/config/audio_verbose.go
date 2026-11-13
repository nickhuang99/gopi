package config

import (
	"fmt"
	"os"
	"strings"
)

// audio_verbose.go — THE DEBUG-SINK SWITCH of the voice surfaces.
//
// THE STATE THIS CLOSES. `gopi tool voice_input --engine streaming` printed its
// whole debugging stream unconditionally: one `[voice] partial N at T: …` line
// per partial (19 of them in one operator capture), the channel's counters (the
// partial count, the first/final partial instants, the vendor's logid/reqid, how
// much audio went out), the live-shape note and the credential/endpoint line.
// None of that is the ANSWER to "what was said" — and the answer (the
// transcript, the per-run timing line) was buried under it. The operator's
// verdict: that is debugging-by-printout, not a tool surface.
//
// SO THE PER-PARTIAL STREAM AND THE CHANNEL'S COUNTERS BECOME OPT-IN, and the
// transcript and the ordinary per-run lines stay exactly as they were on the
// default path. This file owns the SWITCH; cmd/agent owns what the switch gates
// (the printer call sites), and the two cannot disagree because the gate is read
// from here, once, and passed down as a plain bool.
//
// THE PRECEDENCE, one place (Rule 2):
//
//	--verbose / -v (subcommand flag)  >  $GOPI_AUDIO_VERBOSE  >  settings "audio_verbose"  >  OFF
//
// The flag rung is the CALLER's own answer and therefore wins outright — it is
// the one channel an operator can use without editing a file or exporting a
// variable, which is what a per-invocation debugging need looks like.

// AudioVerboseEnv is the env channel of the switch (GOPI_AUDIO_VERBOSE=1). Rule 2:
// this constant is the ONE spelling, and ResolveAudioVerbose below is the ONE
// reader.
const AudioVerboseEnv = "GOPI_AUDIO_VERBOSE"

// AudioVerboseSettingsKey is the settings.json key of the same switch
// ("audio_verbose": true) — the headless path's channel, where no flag can be
// passed (a cron-like run, a script that always invokes the same command).
const AudioVerboseSettingsKey = "audio_verbose"

// AudioVerboseFlagName is the CLI flag (`--verbose`, and its `-v` short spelling)
// on the `voice_input` subcommand. It is NOT a top-level agent flag: this knob
// governs one tool's stderr stream, and a top-level `--verbose` would have to
// mean something about the agent loop instead (which already has --debug). The
// catalog row therefore carries no flag spelling, exactly like the other
// subcommand-scoped audio knobs ({AudioEngineEnv, …}) — see EnvVars.
const AudioVerboseFlagName = "verbose"

// AudioVerboseDefaultClaim is the ONE wording of the default the help surfaces
// and CONFIG.md print (Rules 1b/13/14). The default is OFF, and the claim says
// so, because a knob whose whole purpose is to reduce default output must not
// itself be discoverable only by reading the code.
const AudioVerboseDefaultClaim = "default OFF"

// audioVerboseDefault is the switch's value when NO channel declares it. Named
// (Rule 1) because the default IS the contract here: the debugging stream is
// silent unless something asked for it.
const audioVerboseDefault = false

// audioVerboseNotDeclared is the source label for "no channel mentioned the
// knob", kept distinct from a declared OFF so a caller that reports the switch
// can tell "nobody asked" from "the operator said off" (Rule 15: an ambiguous
// report is a silent failure with better manners).
const audioVerboseNotDeclared = "not declared"

// ResolveAudioVerbose is the ONE resolution of the switch (Rule 2), and it is a
// pure function of the FLAG plus the two lower channels: it returns the value and
// the channel that decided it, so a caller that reports the switch cannot
// disagree with the switch.
//
// flagSet is "the caller passed --verbose/-v", passed in rather than read from a
// global: the flag belongs to the subcommand parser (cmd/agent), and this
// function must stay callable by anything that already knows the answer.
//
// THE ENV CHANNEL accepts the documented truthy vocabulary (IsTrueValue) AND the
// documented falsey set (IsFalseValue), so an ApplyEnv-written "off" is
// understood rather than misread as enabled. A value that is NEITHER spelling (a
// typo like "ture") cannot answer the question: it is reported on stderr and the
// next channel decides — never silently read as OFF, which beside a default of
// OFF would be indistinguishable from nobody having spoken at all (Rule 15).
//
// THE SETTINGS CHANNEL is read HERE, directly, and the read FAILURE is
// deliberately not reported — the one place in this package where an unreadable
// document is silent, and the reason is this knob's own failure mode: it can only
// ever lose DEBUG OUTPUT, never change what a run transcribes or where the audio
// goes. A line printed here would land on the DEFAULT path of every voice run on
// a host whose settings file is missing, i.e. it would add exactly the kind of
// unconditional noise this switch exists to remove. (Contrast MemoryPrefetch,
// whose unreadable-document case IS reported: that knob changes what the agent
// sends to the model, so an unknown answer there is a behavioural one.)
func ResolveAudioVerbose(flagSet bool) (enabled bool, source string) {
	if flagSet {
		return true, "--" + AudioVerboseFlagName
	}
	if raw := strings.TrimSpace(os.Getenv(AudioVerboseEnv)); raw != "" {
		if IsTrueValue(raw) {
			return true, AudioVerboseEnv
		}
		if IsFalseValue(raw) {
			return false, AudioVerboseEnv
		}
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a recognised boolean (%s/off) — ignored; %s decides\n",
			AudioVerboseEnv, raw, EnvBoolOn, AudioVerboseSettingsKey)
	}
	if cfg, err := FromSettingsFile(ResolveSettingsPath()); err == nil && cfg != nil && cfg.AudioVerbose {
		return true, AudioVerboseSettingsKey
	}
	return audioVerboseDefault, audioVerboseNotDeclared
}
