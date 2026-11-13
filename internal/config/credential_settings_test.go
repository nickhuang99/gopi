package config

// credential_settings_test.go — the regression guard for the ONE rule about a
// settings document that carries a credential: it must be 0600, and the keys
// that ARE credentials must be named in exactly one place.
//
// THE DEFECT THIS PINS (measured on this host 2026-10-07). The live
// ~/.gopi/settings.json is a symlink to .ds_flash_settings.json and its mode is
// 0664 — world-readable — and it already carries `api_key`. The same host's
// legacy ~/.doubao/apikey.txt is 0664 too. Adding a second credential to
// settings.json without touching the mode would be a silent security
// downgrade dressed as a configuration improvement, and the SYMLINK is what
// makes the naive guard useless: chmod on the link path does not reach the
// bytes' file.
//
// The scaffold note (Rule 32): internal/testutil imports internal/config, so a
// config test importing it is an import cycle — this package's own convention
// is t.TempDir(), the same fresh per-test directory the scaffold hands out.
// Neither the operator's profile nor the live settings path appears anywhere
// below.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// credentialFixtureMode is the WIDE mode the fixture starts from: the mode the
// operator's own document has today (0664), so the tightening is exercised
// against the shape that produced the finding rather than a convenient one.
const credentialFixtureMode = 0o664

// credentialFixtureBody is the document with an inline credential.
func credentialFixtureBody(key string) string {
	return `{"` + DoubaoAPIKeySettingsKey + `": ` + key + `, "voice_engine": "doubao"}` + "\n"
}

// TestCredentialKeySetIsTheOneList pins the set AND its membership: the two keys
// whose values are secrets, and the near-miss that must NOT be swallowed by a
// `_key` prefix rule.
func TestCredentialKeySetIsTheOneList(t *testing.T) {
	for _, key := range []string{APIKeySettingsKey, DoubaoAPIKeySettingsKey} {
		if !IsCredentialSettingsKey(key) {
			t.Errorf("IsCredentialSettingsKey(%q) = false — a credential would be PRINTED by `gopi config show`", key)
		}
	}
	// The near miss: a PATH ending in `_key`. Over-redacting it would teach
	// readers that the list is noise; the assertion is here so the choice is a
	// decision someone can find.
	for _, key := range []string{DoubaoKeyFileSettingsKey, "monkey", "", "api_keys"} {
		if IsCredentialSettingsKey(key) {
			t.Errorf("IsCredentialSettingsKey(%q) = true — %q is a path (or nothing), not a credential", key, key)
		}
	}
}

// TestInlineKeySettingIsModelled pins the settings key's presence in the typed
// Config — the loss guard reports key loss BY comparing the document against
// the modelled set, so an unmodelled key is a key whose disappearance is
// invisible.
func TestInlineKeySettingIsModelled(t *testing.T) {
	keys := ModelledSettingsKeys()
	for _, want := range []string{DoubaoAPIKeySettingsKey, DoubaoKeyFileSettingsKey} {
		if !containsString(keys, want) {
			t.Errorf("ModelledSettingsKeys() does not carry %q — the document's key-loss guard cannot see it", want)
		}
	}
}

// TestInlineKeyEmptyValueKeepsTheDocumentValid is Rule 15a in the shape it is
// actually dangerous: "" IS a legal value for this knob ("no inline key"), so
// `omitempty` must drop the key from the encoding without the document
// becoming invalid, and an absent key must resolve exactly like an empty one.
func TestInlineKeyEmptyValueKeepsTheDocumentValid(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"key absent", `{"voice_engine": "doubao"}`},
		{"key empty", `{"` + DoubaoAPIKeySettingsKey + `": "", "voice_engine": "doubao"}`},
		{"key null", `{"` + DoubaoAPIKeySettingsKey + `": null, "voice_engine": "doubao"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".json")
			if err := os.WriteFile(path, []byte(tc.body), SettingsPrivateMode); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			cfg, err := FromSettingsFile(path)
			if err != nil {
				t.Fatalf("FromSettingsFile(%s) = %v, want the document to stay valid", tc.name, err)
			}
			if cfg.AudioEngineParam.Recording.APIKey != "" {
				t.Errorf("%s: DoubaoAPIKey = %q, want the empty string", tc.name, cfg.AudioEngineParam.Recording.APIKey)
			}
			if cfg.AudioEngine != AudioEngineRecording {
				t.Errorf("%s: the sibling key was LOST (VoiceEngine = %q) — an empty optional value broke the document",
					tc.name, cfg.AudioEngine)
			}
			// And the guard must answer "no credential here" — an empty value
			// is not a secret to protect, and refusing to proceed on it would
			// make every settings file special.
			carries, readable := SettingsDocumentCarriesCredential(path)
			if carries || !readable {
				t.Errorf("%s: carries=%v readable=%v, want false/true", tc.name, carries, readable)
			}
		})
	}
}

// TestSettingsDocumentCarriesCredentialFindsBothKeys is the detector's positive
// arm, and its "I could not tell" arm (Rule 15): a broken document must be
// reported as unanswerable, never as "no credential".
func TestSettingsDocumentCarriesCredentialFindsBothKeys(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{APIKeySettingsKey, DoubaoAPIKeySettingsKey} {
		path := filepath.Join(dir, key+".json")
		body := `{"` + key + `": "sentinel-not-a-real-secret"}`
		if err := os.WriteFile(path, []byte(body), SettingsPrivateMode); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if carries, readable := SettingsDocumentCarriesCredential(path); !carries || !readable {
			t.Errorf("%q: carries=%v readable=%v, want true/true", key, carries, readable)
		}
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), SettingsPrivateMode); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}
	if carries, readable := SettingsDocumentCarriesCredential(broken); carries || readable {
		t.Errorf("an unparseable document answered carries=%v readable=%v — \"I could not tell\" must not read as \"clean\"",
			carries, readable)
	}
	missing := filepath.Join(dir, "absent.json")
	if carries, readable := SettingsDocumentCarriesCredential(missing); carries || !readable {
		t.Errorf("a missing document answered carries=%v readable=%v — nothing on disk is the ordinary clean case",
			carries, readable)
	}
}

// TestTightenSettingsNarrowsOnlyAndReports is the re-measure: the mode really
// moves, a private file is left alone (no gratuitous chmod of the operator's
// document on every run), and the note names path and both modes WITHOUT the
// document's contents.
func TestTightenSettingsNarrowsOnlyAndReports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	const sentinel = "sentinel-key-value-must-not-appear"
	if err := os.WriteFile(path, []byte(credentialFixtureBody(`"`+sentinel+`"`)), credentialFixtureMode); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	mode, err := SettingsFileMode(path)
	if err != nil {
		t.Fatalf("SettingsFileMode: %v", err)
	}
	if mode != credentialFixtureMode {
		t.Fatalf("fixture mode = %04o, want %04o — the fixture did not reproduce the finding's shape", mode, credentialFixtureMode)
	}

	note, err := EnsureSettingsPrivateForCredential(path)
	if err != nil {
		t.Fatalf("EnsureSettingsPrivateForCredential: %v", err)
	}
	if note == "" {
		t.Fatal("the tightening happened silently — an unreported permission change on the operator's own file is the defect")
	}
	if !strings.Contains(note, path) || !strings.Contains(note, "0664") || !strings.Contains(note, "0600") {
		t.Errorf("note %q does not name the path and both modes", note)
	}
	if strings.Contains(note, sentinel) {
		t.Errorf("note %q carries the credential value — the report of a permission change must not quote the secret", note)
	}
	if got, _ := SettingsFileMode(path); got != SettingsPrivateMode { //stylegate:ignore stats the mode of the settings file this test wrote; the mode comparison below is the contract
		t.Errorf("mode after tightening = %04o, want %04o", got, SettingsPrivateMode)
	}

	// SECOND CALL IS A NO-OP: the file is already private, so nothing changes
	// and nothing is reported. (An operator re-running a command must not see
	// the same "tightened" line forever — a report that fires when nothing
	// happened is how a real one gets ignored.)
	note2, err := EnsureSettingsPrivateForCredential(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if note2 != "" {
		t.Errorf("second call reported %q, want nothing — the file was already private", note2)
	}

	// A DOCUMENT WITH NO CREDENTIAL IS LEFT ALONE, mode included: reading the
	// operator's profile must not reformat it.
	plain := filepath.Join(dir, "plain.json")
	if err := os.WriteFile(plain, []byte(`{"model": "fixture-model"}`), credentialFixtureMode); err != nil {
		t.Fatalf("write plain fixture: %v", err)
	}
	note3, err := EnsureSettingsPrivateForCredential(plain)
	if err != nil {
		t.Fatalf("plain document: %v", err)
	}
	if note3 != "" {
		t.Errorf("a document with no credential was reported as %q", note3)
	}
	if got, _ := SettingsFileMode(plain); got != credentialFixtureMode { //stylegate:ignore same mode read on the world-readable fixture, whose non-private mode is what is asserted
		t.Errorf("a credential-free document's mode was changed to %04o — gopi chmods only what it must", got)
	}
}

// TestTighteningFollowsASymlinkToTheRealFile is the half that the naive guard
// gets wrong: the operator's document is a LINK, and the link's own mode is not
// the secret's protection. The assertion is on the TARGET's mode, and on the
// link still being a link (chmod that replaced the node would be a far worse
// outcome than an open mode).
func TestTighteningFollowsASymlinkToTheRealFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".ds_flash_settings.json")
	link := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(target, []byte(credentialFixtureBody(`"sentinel-value"`)), credentialFixtureMode); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	note, err := EnsureSettingsPrivateForCredential(link)
	if err != nil {
		t.Fatalf("EnsureSettingsPrivateForCredential(link): %v", err)
	}
	if note == "" {
		t.Fatal("no note for a world-readable LINKED credential document")
	}
	targetMode, err := SettingsFileMode(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if targetMode != SettingsPrivateMode {
		t.Errorf("the TARGET is mode %04o, want %04o — the guard protected the link and not the file holding the secret",
			targetMode, SettingsPrivateMode)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was REPLACED by a regular file — the guard renamed onto a symlink instead of chmod-ing its target")
	}
}
