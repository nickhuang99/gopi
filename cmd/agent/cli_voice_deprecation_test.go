package main

// cli_voice_deprecation_test.go — the migration notice's contract (2026-10-09),
// tested through the function that COMPOSES the lines rather than through a
// captured stderr: a message buried inside a sync.Once cannot be asserted, and
// the Once is about how often the line prints, not about what it says.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// writeLegacySettings documents at a scratch path with the test's own body and
// returns the path. It exists rather than reusing writeDoubaoKeySettings
// (cli_voice_doubao_key_test.go) because that helper composes a body of its own;
// this file needs to write the exact document each case is about.
func writeLegacySettings(path, body string) error {
	return os.WriteFile(path, []byte(body), config.SettingsPrivateMode)
}

// TestVoiceLegacyNotesNameTheNestedReplacement: on a document and an environment
// that still speak the pre-rename surface, the notice must name BOTH halves of
// each discovery — what is old, and where the value now belongs. A notice that
// said only "this is old" would leave the operator reading the source.
func TestVoiceLegacyNotesNameTheNestedReplacement(t *testing.T) {
	for _, k := range []string{
		config.DoubaoAPIKeyEnv,
		config.DoubaoAppIDEnv,
		config.DoubaoTokenEnv,
		config.DoubaoStreamEndpointEnv,
		config.DoubaoHotwordsEnv,
		config.DoubaoResourceIDEnv,
		config.DoubaoBaseURLEnv,
		config.DoubaoKeyFileEnv,
	} {
		// Every legacy env name must be reachable by the notice, or an operator
		// who exports one is never told. Each is set alone so a missing entry
		// cannot hide behind another's line.
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, "legacy-value")
			notes := voiceLegacyAudioConfigNotes(nil)
			if len(notes) == 0 {
				t.Fatalf("%s is set and the notice is empty — the operator is never told", k)
			}
			joined := strings.Join(notes, "\n")
			if !strings.Contains(joined, k) {
				t.Errorf("no line names the variable %q:\n%s", k, joined)
			}
			if !strings.Contains(joined, config.AudioEngineParamSettingsKey) {
				t.Errorf("no line names the replacement surface %q:\n%s", config.AudioEngineParamSettingsKey, joined)
			}
		})
	}
}

// TestVoiceLegacyNotesAreSilentOnACurrentSurface is the other half, and the one
// that keeps the notice readable: an operator who has migrated must see nothing,
// or the line becomes background noise and stops being read on the host that
// needs it most.
func TestVoiceLegacyNotesAreSilentOnACurrentSurface(t *testing.T) {
	// A settings file under the test's own control, written the CURRENT way.
	dir := t.TempDir()
	path := dir + "/settings.json"
	if err := writeLegacySettings(path, `{"audio_engine":"recording",
	  "audio_engine_param":{"recording":{"api_key":"k"}}}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)
	// Mask every pre-rename variable: the ambient environment of a developer's
	// shell is not part of this test's subject.
	for _, k := range config.AudioLegacyParamEnvNames {
		t.Setenv(k, "")
	}
	if notes := voiceLegacyAudioConfigNotes(&voiceOptions{}); len(notes) != 0 {
		t.Errorf("a migrated document produced %d notice line(s): %v", len(notes), notes)
	}
}

// TestVoiceLegacyNoteTellsTheEngineNamesNewValues: the notice for a pre-rename
// engine NAME must spell the three current values. "doubao is old" without the
// replacement is the same defect as above, in the engine half of the surface.
func TestVoiceLegacyNoteTellsTheEngineNamesNewValues(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/settings.json"
	if err := writeLegacySettings(path, `{"voice_engine":"doubao"}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)
	for _, k := range config.AudioLegacyParamEnvNames {
		t.Setenv(k, "")
	}
	// opts is nil on purpose: the settings rung alone must be able to produce
	// the engine-name line, because that is the rung a migrated-by-hand file
	// leaves behind (the loader folds the value, so the notice asks for the RAW
	// document value — a condition on the resolved one could never fire).
	notes := voiceLegacyAudioConfigNotes(nil)
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, config.AudioEngineLegacyVendorRecording) {
		t.Fatalf("the document's legacy engine name is not named:\n%s", joined)
	}
	for _, want := range []string{config.AudioEngineRecording, config.AudioEngineStreaming, config.AudioEngineLocal} {
		if !strings.Contains(joined, want) {
			t.Errorf("the engine-name notice does not name the current value %q:\n%s", want, joined)
		}
	}
}

// TestVoiceEngineNameTranslationIsAcceptedByTheValidator: the migration is not
// a breaking change only if every door accepts the old name. The validator is
// one of them; the resolver is the other, and both ask the ONE mapping.
func TestVoiceEngineNameTranslationIsAcceptedByTheValidator(t *testing.T) {
	for _, name := range []string{
		config.AudioEngineLegacyVendorRecording,
		config.AudioEngineLegacyVendorStreaming,
	} {
		if err := voiceValidateEngine(name); err != nil {
			t.Errorf("voiceValidateEngine(%q) = %v, want the pre-rename name accepted", name, err)
		}
	}
	for _, name := range config.AudioEngineEngineNames {
		if err := voiceValidateEngine(name); err != nil {
			t.Errorf("voiceValidateEngine(%q) = %v, want the current name accepted", name, err)
		}
	}
	if err := voiceValidateEngine("not-an-engine"); err == nil {
		t.Error("voiceValidateEngine accepted a name that is not an engine")
	}
}

// TestVoiceResolveEngineReadsTheLegacyChannel: the pre-rename env variable must
// select the SAME engine as its successor — a translator wired into the message
// path but not the resolution path would report a migration that did not happen.
func TestVoiceResolveEngineReadsTheLegacyChannel(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.AudioEngineLegacyEnv, config.AudioEngineLegacyVendorStreaming)
	got := voiceResolveEngine(&voiceOptions{})
	if got != config.AudioEngineStreaming {
		t.Errorf("voiceResolveEngine with %s=%s = %q, want %q",
			config.AudioEngineLegacyEnv, config.AudioEngineLegacyVendorStreaming, got, config.AudioEngineStreaming)
	}
	// And the current variable outranks it.
	t.Setenv(config.AudioEngineEnv, config.AudioEngineLocal)
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineLocal {
		t.Errorf("the current env name did not outrank the pre-rename one: got %q", got)
	}
}
