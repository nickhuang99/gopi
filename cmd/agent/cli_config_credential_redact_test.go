package main

// cli_config_credential_redact_test.go — `gopi config show` renders every
// settings key with its value, and two of those keys are CREDENTIALS
// (internal/config.CredentialSettingsKeys). The redaction is what makes the
// inline-key feature safe to document: an operator can now keep the doubao key
// in settings.json without the one command they run to inspect their
// configuration printing it into a terminal, a scrollback or a pasted report.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// TestConfigShowRedactsEveryCredentialKey drives the REAL renderer over a
// document carrying BOTH credential keys plus an ordinary one, and asserts the
// three-way contract: credentials render as their SHAPE, the ordinary key
// renders its value, and no credential value survives anywhere in the output.
func TestConfigShowRedactsEveryCredentialKey(t *testing.T) {
	const apiKeyValue = "fixture-api-key-redaction-sentinel-0002"
	const doubaoKeyValue = "fixture-doubao-key-redaction-sentinel-0003"
	// The STREAMING channel's pair (task 01a11f90) — two more credentials, and
	// they are written into the document on purpose: the loop below asserts
	// every key in config.CredentialSettingsKeys is NAMED by the report, and a
	// key absent from the document is absent from the report for a reason that
	// has nothing to do with redaction.
	const streamAppIDValue = "fixture-stream-appid-redaction-sentinel-0004"
	const streamTokenValue = "fixture-stream-token-redaction-sentinel-0005"

	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{"` + config.APIKeySettingsKey + `": "` + apiKeyValue + `", ` +
		`"` + config.DoubaoAPIKeySettingsKey + `": "` + doubaoKeyValue + `", ` +
		`"` + config.DoubaoAppIDSettingsKey + `": "` + streamAppIDValue + `", ` +
		`"` + config.DoubaoTokenSettingsKey + `": "` + streamTokenValue + `", ` +
		`"voice_engine": "doubao"}`
	if err := os.WriteFile(path, []byte(body), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	out := captureStdout(t, func() { showSettingsFile(path) })

	for _, secret := range []string{apiKeyValue, doubaoKeyValue, streamAppIDValue, streamTokenValue} {
		if strings.Contains(out, secret) {
			t.Errorf("`gopi config show` printed a credential value (%q) — the renderer must show its shape, never its bytes", secret)
		}
	}
	for _, key := range config.CredentialSettingsKeys {
		if !strings.Contains(out, key) {
			t.Errorf("output does not carry the key %q — a credential the report cannot even name is one nobody can audit", key)
		}
	}
	// The SHAPE must be there, and the number it reports must be the value's
	// length — otherwise the redaction would be hiding a value the reader is
	// entitled to check (``was this actually set?'').
	if !strings.Contains(out, " chars>") {
		t.Errorf("output %q carries no shape marker for the redacted values", out)
	}
	if want := len(doubaoKeyValue); !strings.Contains(out, strconv.Itoa(want)) {
		t.Errorf("output %q does not report the doubao key's length (%d)", out, want)
	}
	// An ordinary key is NOT redacted: a blanket mask would make the command
	// useless for the thing it exists to do.
	if !strings.Contains(out, "doubao") {
		t.Errorf("output %q does not show the ordinary settings values beside the credentials", out)
	}
}

// TestConfigShowMarksAnUnsetCredential: "" is a legal value meaning "not
// configured", and the report must say so rather than render an empty cell a
// reader could mistake for a redacted one.
func TestConfigShowMarksAnUnsetCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{"` + config.DoubaoAPIKeySettingsKey + `": ""}`
	if err := os.WriteFile(path, []byte(body), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	out := captureStdout(t, func() { showSettingsFile(path) })
	if !strings.Contains(out, "<unset>") {
		t.Errorf("output %q does not distinguish an unset credential from a redacted one", out)
	}
}
