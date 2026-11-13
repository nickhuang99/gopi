package main

// cli_voice_deprecation.go — THE PRE-RENAME SURFACE, kept honoured and made
// VISIBLE.
//
// The audio configuration surface was renamed on 2026-10-09 (devdoc/
// audio-engine-config-surface.md): a flat set of vendor-named keys became
// `audio_engine` + `audio_engine_param.<engine>.<knob>`, and the engine VALUES
// became functional names. gopi reads the old spellings STILL — an operator's
// working configuration must not stop taking effect because a key moved — and
// that tolerance is exactly why this file exists: a knob that is honoured but
// never mentioned is a permanent, invisible second surface, and the next person
// to read `gopi config show` has no way to learn which of the two they are
// looking at (Rule 15: no silent failure — including the silent SUCCESS of an
// old key).
//
// ONE NOTICE PER RUN, not one per call site: the resolver runs several times in
// an invocation (the deps check, the dispatch, the provenance line), so the
// notice is printed through a Once. The sentence itself is composed in
// internal/config (AudioLegacyEnvNote / AudioLegacyKeyNote), so the CLI, the
// chat surface and `gopi config show` cannot describe the migration three
// different ways.

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/nick/gopi/internal/config"
)

// voiceLegacyNoticeOnce makes the migration notice a once-per-process event.
// It is a print, not state (Rule 35): nothing persists, nothing is written, and
// the next process prints it again — which is the point, because the operator
// has to see it until they migrate.
var voiceLegacyNoticeOnce sync.Once

// voiceLegacyAliasPrefix is the fixed head of every line this file prints. It
// is named (Rule 1/13) because a test asserts the shape and CONFIG.md quotes it:
// one spelling, so an operator grepping a session log for "renamed" finds all
// of them.
const voiceLegacyAliasPrefix = "[gopi] renamed configuration: "

// voiceReportLegacyAudioConfig prints, once per run, what this invocation read
// from the PRE-RENAME surface: engine names spelled "doubao"/"doubao-stream",
// the old top-level settings keys, and the old env var names. It prints nothing
// on a document and an environment that use the current surface — the notice is
// for the reader who has not migrated, and noise for the one who has.
func voiceReportLegacyAudioConfig(opts *voiceOptions) {
	voiceLegacyNoticeOnce.Do(func() {
		for _, line := range voiceLegacyAudioConfigNotes(opts) {
			fmt.Fprintf(os.Stderr, "%s%s\n", voiceLegacyAliasPrefix, line)
		}
	})
}

// voiceLegacyAudioConfigNotes composes the lines, so the decision about WHAT is
// legacy lives in one function the tests can call directly (a message buried
// inside a Once cannot be asserted — the same reason doubaoKeySource carries its
// mode note as a field).
func voiceLegacyAudioConfigNotes(opts *voiceOptions) []string {
	var lines []string
	// The engine NAME, wherever it was spelled: the flag the parser recorded,
	// the env channel, or the settings value. Any of the three naming a legacy
	// value is a legacy surface in use. opts is nil when a caller only wants the
	// environment half (the chat path resolves its own).
	//
	// The name the FLAG or the ENVIRONMENT carried, raw (the settings rung is
	// asked separately below, for the reason stated there).
	name := engineRungValue(opts)
	// The SETTINGS rung is asked for the name the DOCUMENT carries, never for
	// the translated one: the load path folds the pre-rename value into the
	// current spelling (normalizeAudioParams), so by the time a Config exists
	// the legacy name is gone from AudioEngine — and a notice that could only
	// be produced from a folded value could never fire. LegacyVoiceEngine holds
	// exactly the raw document value, which is why the field exists.
	if cfg, err := config.FromSettingsFile(config.ResolveSettingsPath()); err == nil && cfg != nil {
		// The document's raw value WINS over the resolved one, and the
		// condition is not "name is empty": the folded value is never empty on
		// a document that carries the legacy key (the fold puts the translation
		// into AudioEngine), so an emptiness test here would ask the wrong
		// question and the notice would never fire for a migrated-file-by-name
		// document. The raw value is the only one that can say "the operator
		// wrote doubao".
		if v := strings.TrimSpace(cfg.LegacyVoiceEngine); v != "" {
			name = v
		}
	}
	if _, wasLegacy := config.AudioEngineLegacyVendorName(name); wasLegacy {
		lines = append(lines, fmt.Sprintf(
			"the engine name %q is the pre-rename spelling — the engine is now named for the shape of the work: "+
				"%s (cloud batch), %s (cloud real-time) or %s (on this machine)",
			name, config.AudioEngineRecording, config.AudioEngineStreaming, config.AudioEngineLocal))
	}
	if note := config.AudioLegacyEnvNote(os.Getenv); note != "" {
		lines = append(lines, note)
	}
	// The KEY note asks the document, so it is answered by loading it — the
	// same read every other rung of this surface uses (Rule 2). A file that
	// cannot be read is not reported here: the run's own settings load reports
	// it, with the parse failure, and duplicating that would be noise.
	if cfg, err := config.FromSettingsFile(config.ResolveSettingsPath()); err == nil && cfg != nil {
		if note := cfg.AudioLegacyKeyNote(); note != "" {
			lines = append(lines, note)
		}
		if note := cfg.AudioEngineUnknownNote(); note != "" {
			lines = append(lines, note)
		}
	}
	return lines
}

// engineRungValue is the engine name the FIRST rung that names one carries —
// the same three-rung precedence voiceResolveEngine walks (Rule 2: one order,
// so "which value is legacy?" cannot disagree with "which engine runs?").
func engineRungValue(opts *voiceOptions) string {
	if opts != nil {
		// The RAW flag value first (what the operator typed), never the
		// translated one the parser stored — see voiceOptions.engineRaw.
		if v := strings.TrimSpace(opts.engineRaw); v != "" {
			return v
		}
		if v := strings.TrimSpace(opts.engine); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(os.Getenv(config.AudioEngineEnv)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(config.AudioEngineLegacyEnv)); v != "" {
		return v
	}
	if cfg, err := config.FromSettingsFile(config.ResolveSettingsPath()); err == nil && cfg != nil {
		if v := strings.TrimSpace(cfg.AudioEngine); v != "" {
			return v
		}
	}
	return ""
}
