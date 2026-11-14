package main

// cli_voice_doubao_key_test.go — WHERE THE DOUBAO CREDENTIAL COMES FROM.
//
// The defect this pins: the doubao engine accepted a key from exactly one place
// (a file, default `~/.doubao/apikey.txt`) while the operator's credential for
// the sibling model behind the same endpoint already lived in `settings.json`.
// The repair added one rung — the inline `doubao_api_key` — WITHOUT weakening
// the file rungs, and these tests are the statement of that precedence
// (Rule 5: every behaviour change ships with a minimal repro).
//
// Every fixture points `GOPI_SETTINGS` at a scratch document (Rule 32: a test
// pins the environment rather than touching the operator's own profile), and no
// assertion below ever reads a key VALUE out of a message — the credential is
// checked where it is CONSUMED (doubaoasr.Options.APIKey) and nowhere else,
// which is also the reason the settings document may carry a real-looking
// sentinel at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// doubaoKeySentinel is the fixture credential. It is deliberately NOT a real
// key shape and carries the "redaction fixture" marker, so a leak into any log,
// report or transcript is unmistakable in a grep.
const doubaoKeySentinel = "fixture-key-redaction-sentinel-0001"

// writeDoubaoKeySettings writes a settings document carrying the inline key (or
// a document with no credential at all when key is empty) and returns its path.
// mode is the fixture's starting mode: the tests that exercise the permission
// guard pass 0o664 (the mode this host's live document actually has), the rest
// pass 0o600.
func writeDoubaoKeySettings(t *testing.T, key string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := "{"
	if key != "" {
		body += `"` + config.DoubaoAPIKeySettingsKey + `": "` + key + `", `
	}
	body += `"voice_engine": "doubao"}`
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	return path
}

// clearDoubaoKeyEnv removes every channel that could shadow the settings file,
// so each subtest is about the rung it names. Both names are cleared: a
// developer's own shell export must not decide what this test proves.
func clearDoubaoKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.AudioKeyFileEnv, "")
	t.Setenv(config.DoubaoAPIKeyEnv, "")
}

// TestVoiceDoubaoInlineKeyIsUsed is the positive arm: with the key ONLY in
// settings.json (its env channel unset, the legacy default path irrelevant),
// the credential reaches the request.
func TestVoiceDoubaoInlineKeyIsUsed(t *testing.T) {
	clearDoubaoKeyEnv(t)
	t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, doubaoKeySentinel, config.SettingsPrivateMode))

	do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording, audio: "clip.wav"})
	if err != nil {
		t.Fatalf("the inline key must be enough: %v", err)
	}
	if do.APIKey != doubaoKeySentinel {
		t.Errorf("APIKey = %q, want the inline value", do.APIKey)
	}
}

// TestVoiceDoubaoInlineKeySatisfiesThePreflightCheck: the dependency check is a
// SEPARATE call site from the request assembly, and a key the check cannot see
// is a key the engine refuses before it is ever tried. Both must read one
// resolution, which is why this test exists beside the one above.
func TestVoiceDoubaoInlineKeySatisfiesThePreflightCheck(t *testing.T) {
	clearDoubaoKeyEnv(t)
	t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, doubaoKeySentinel, config.SettingsPrivateMode))

	if msg := voiceDoubaoDepsReady(&voiceOptions{engine: config.AudioEngineRecording}); msg != "" {
		t.Errorf("the pre-flight check refused a run the resolver would have accepted: %q", msg)
	}
}

// TestVoiceDoubaoExplicitFileOutranksTheInlineKey is the asymmetry, asserted in
// BOTH directions: an explicit file wins over settings, and settings is still
// there when nobody named one. A path is a deliberate statement about where the
// credential lives; gopi does not overwrite that with whatever sits in the
// document.
func TestVoiceDoubaoExplicitFileOutranksTheInlineKey(t *testing.T) {
	dir := t.TempDir()
	fileKey := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(fileKey, []byte("key-from-the-file\n"), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	for _, tc := range []struct {
		name     string
		settings string
		settle   func(t *testing.T)
		opts     *voiceOptions
		wantKey  string
	}{
		{
			// The CLI flag: the strongest statement an operator can make. The
			// document carries ONLY the inline key, so what is measured is the
			// flag's win over it.
			name:     "flag names a file",
			settings: `{"` + config.DoubaoAPIKeySettingsKey + `": "` + doubaoKeySentinel + `"}`,
			settle:   func(t *testing.T) { clearDoubaoKeyEnv(t) },
			opts:     &voiceOptions{engine: config.AudioEngineRecording, doubaoKeyFile: fileKey},
			wantKey:  "key-from-the-file",
		},
		{
			// The env channel, which the flag must still beat and which beats
			// the settings document.
			name:     "env names a file",
			settings: `{"` + config.DoubaoAPIKeySettingsKey + `": "` + doubaoKeySentinel + `"}`,
			settle: func(t *testing.T) {
				t.Setenv(config.AudioKeyFileEnv, fileKey)
				t.Setenv(config.DoubaoAPIKeyEnv, "")
			},
			opts:    &voiceOptions{engine: config.AudioEngineRecording},
			wantKey: "key-from-the-file",
		},
		{
			// settings names a file: a rung the doc states and no other test
			// exercises, asserted against the inline value sitting beside it.
			name: "settings names a file",
			settings: `{"` + config.DoubaoKeyFileSettingsKey + `": "` + fileKey + `", "` +
				config.DoubaoAPIKeySettingsKey + `": "` + doubaoKeySentinel + `"}`,
			settle:  func(t *testing.T) { clearDoubaoKeyEnv(t) },
			opts:    &voiceOptions{engine: config.AudioEngineRecording},
			wantKey: "key-from-the-file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settingsPath := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(settingsPath, []byte(tc.settings), config.SettingsPrivateMode); err != nil {
				t.Fatalf("write settings: %v", err)
			}
			t.Setenv(config.SettingsPathEnv, settingsPath)
			tc.settle(t)

			do, err := voiceResolveDoubaoOpts(tc.opts)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if do.APIKey != tc.wantKey {
				t.Errorf("APIKey = %q, want %q — an explicitly named FILE outranks the inline settings value",
					do.APIKey, tc.wantKey)
			}
		})
	}
}

// TestVoiceDoubaoInlineKeyEnvChannelIsHonoured: the env rung of the inline value
// is READ (a catalog row the code ignores is the silent failure Rule 15 bans),
// and it outranks the settings document — the package's standing order is
// flags > env > settings.
func TestVoiceDoubaoInlineKeyEnvChannelIsHonoured(t *testing.T) {
	clearDoubaoKeyEnv(t)
	t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, "key-from-settings", config.SettingsPrivateMode))
	t.Setenv(config.DoubaoAPIKeyEnv, "key-from-env")

	do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if do.APIKey != "key-from-env" {
		t.Errorf("APIKey = %q, want the exported value (env outranks settings)", do.APIKey)
	}
}

// TestVoiceDoubaoEmptyInlineKeyFallsBackToTheFile is the Rule 15a regression on
// the consumer side: an EMPTY inline value means "unset" and must leave the
// legacy file rung in force. Failing here would present a configuration that
// worked yesterday as "no key", which is the failure mode an optional field with
// the wrong zero-value semantics produces.
func TestVoiceDoubaoEmptyInlineKeyFallsBackToTheFile(t *testing.T) {
	clearDoubaoKeyEnv(t)
	t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, "", config.SettingsPrivateMode))

	dir := t.TempDir()
	legacy := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(legacy, []byte("key-from-legacy-file\n"), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write legacy key file: %v", err)
	}

	do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording, doubaoKeyFile: legacy})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if do.APIKey != "key-from-legacy-file" {
		t.Errorf("APIKey = %q, want the file's value — an empty inline key must not shadow the file rungs", do.APIKey)
	}
}

// TestVoiceDoubaoKeyModeIsTightenedAndAnnounced is the security half of the same
// change, driven through the REAL entry point: the document is world-readable
// (0664, the mode this host's live profile actually carries), it holds a
// credential, and the run that reads it must (a) narrow the file and (b) say so.
//
// The announcement is asserted on the STRUCT the resolver produced rather than
// on captured stderr: the sentence is built in the resolver and printed by its
// caller, so this is the seam where "we told nobody" is detectable.
func TestVoiceDoubaoKeyModeIsTightenedAndAnnounced(t *testing.T) {
	clearDoubaoKeyEnv(t)
	path := writeDoubaoKeySettings(t, doubaoKeySentinel, 0o664)
	t.Setenv(config.SettingsPathEnv, path)

	src := voiceDoubaoKeySource(&voiceOptions{engine: config.AudioEngineRecording})
	if src.modeProblem != "" {
		t.Fatalf("the guard could not protect the file: %s", src.modeProblem)
	}
	if src.modeNote == "" {
		t.Fatal("a 0664 settings document carrying a credential was read with NOTHING said about its mode")
	}
	if !strings.Contains(src.modeNote, path) {
		t.Errorf("note %q does not name the file it changed", src.modeNote)
	}
	if strings.Contains(src.modeNote, doubaoKeySentinel) {
		t.Errorf("note %q quotes the credential — a permission report must never carry the secret", src.modeNote)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != config.SettingsPrivateMode {
		t.Errorf("settings mode = %04o, want %04o — the credential was read out of a world-readable file",
			got, config.SettingsPrivateMode)
	}
}

// TestVoiceDoubaoCredentialFreeDocumentIsLeftAlone: gopi does not chmod a
// document that holds no credential. Reading the operator's profile is not a
// licence to reformat it, and a guard that fired on every run would be turned
// off by the first person it inconvenienced.
func TestVoiceDoubaoCredentialFreeDocumentIsLeftAlone(t *testing.T) {
	clearDoubaoKeyEnv(t)
	path := writeDoubaoKeySettings(t, "", 0o664)
	t.Setenv(config.SettingsPathEnv, path)

	src := voiceDoubaoKeySource(&voiceOptions{engine: config.AudioEngineRecording})
	if src.modeNote != "" || src.modeProblem != "" {
		t.Errorf("a credential-free document produced note=%q problem=%q, want neither",
			src.modeNote, src.modeProblem)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o664 {
		t.Errorf("mode = %04o, want 0664 untouched — the guard chmodded a file with no secret in it", got)
	}
}

// TestVoiceDoubaoKeyNeverAppearsInAMessage is the redaction contract, asserted
// against every string this change can produce: the dependency check's message,
// the resolver's error, and the mode note. A credential in a message reaches a
// terminal, a scrollback, a session log and (through the agent's own output) a
// report — so a single leak here defeats the entire 0600/permission machinery
// beside it.
func TestVoiceDoubaoKeyNeverAppearsInAMessage(t *testing.T) {
	clearDoubaoKeyEnv(t)
	t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, doubaoKeySentinel, config.SettingsPrivateMode))
	// A file rung that does NOT exist, so the resolver's error path is the one
	// under test — the message an operator reads when a key cannot be produced.
	missing := filepath.Join(t.TempDir(), "absent-key.txt")
	opts := &voiceOptions{engine: config.AudioEngineRecording, doubaoKeyFile: missing}

	src := voiceDoubaoKeySource(opts)
	var messages []string
	messages = append(messages, src.modeNote, src.modeProblem, src.hint(), src.missing())
	if _, err := voiceResolveDoubaoOpts(opts); err != nil {
		messages = append(messages, err.Error())
	} else {
		t.Fatal("a missing key file must fail the resolve — the error path is what this test measures")
	}
	for _, msg := range messages {
		if strings.Contains(msg, doubaoKeySentinel) {
			t.Errorf("message %q carries the credential value", msg)
		}
	}
}

// TestVoiceDoubaoKeySourceNamesTheWinningRung: the refusal has to point at the
// knob the operator actually set, or the "fix it" half of the message is a
// guess. Each rung is checked by the channel name it reports.
func TestVoiceDoubaoKeySourceNamesTheWinningRung(t *testing.T) {
	dir := t.TempDir()
	fileKey := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(fileKey, []byte("k\n"), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write key: %v", err)
	}

	t.Run("explicit file", func(t *testing.T) {
		clearDoubaoKeyEnv(t)
		t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, "", config.SettingsPrivateMode))
		if got := voiceDoubaoKeySource(&voiceOptions{doubaoKeyFile: fileKey}).hint(); !strings.Contains(got, fileKey) {
			t.Errorf("hint %q does not name the chosen file", got)
		}
	})

	t.Run("inline from settings", func(t *testing.T) {
		clearDoubaoKeyEnv(t)
		t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, doubaoKeySentinel, config.SettingsPrivateMode))
		hint := voiceDoubaoKeySource(&voiceOptions{}).hint()
		// The CURRENT spelling, addressed by the path the operator would write
		// (the surface was renamed 2026-10-09; the hint is what tells them
		// where the inline value lives, so it must name the live location).
		wantKey := config.AudioParamSettingsPath(config.AudioEngineRecording, config.AudioParamKeyAPIKey)
		if !strings.Contains(hint, wantKey) {
			t.Errorf("hint %q does not name the settings key %q", hint, wantKey)
		}
		if strings.Contains(hint, doubaoKeySentinel) {
			t.Errorf("hint %q carries the credential", hint)
		}
	})

	t.Run("inline from env", func(t *testing.T) {
		clearDoubaoKeyEnv(t)
		t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, "", config.SettingsPrivateMode))
		t.Setenv(config.DoubaoAPIKeyEnv, doubaoKeySentinel)
		hint := voiceDoubaoKeySource(&voiceOptions{}).hint()
		// The hint must name a channel the operator can act on, and it must
		// never carry the VALUE. Which of the two env spellings it prefers is
		// not this test's subject (both are read at the same rung); that the
		// credential is absent from the hint is.
		namedSomewhere := strings.Contains(hint, config.AudioAPIKeyEnv) ||
			strings.Contains(hint, config.DoubaoAPIKeyEnv) ||
			strings.Contains(hint, config.AudioParamSettingsPath(config.AudioEngineRecording, config.AudioParamKeyAPIKey))
		if !namedSomewhere {
			t.Errorf("hint %q names no channel the operator can set", hint)
		}
		if strings.Contains(hint, doubaoKeySentinel) {
			t.Errorf("hint %q carries the credential", hint)
		}
	})

	t.Run("legacy fallback", func(t *testing.T) {
		clearDoubaoKeyEnv(t)
		t.Setenv(config.SettingsPathEnv, writeDoubaoKeySettings(t, "", config.SettingsPrivateMode))
		src := voiceDoubaoKeySource(&voiceOptions{})
		if src.file != "" || src.inline != "" {
			t.Fatalf("fixture did not reach the fallback rung: file=%q inline=%q", src.file, src.inline)
		}
		if !strings.Contains(src.hint(), filepath.Base(src.legacyFile)) {
			t.Errorf("hint %q does not name the legacy file %q", src.hint(), src.legacyFile)
		}
	})
}
