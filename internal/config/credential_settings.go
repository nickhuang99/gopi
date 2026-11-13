package config

// credential_settings.go — WHERE A CREDENTIAL MAY LIVE, and the ONE rule about
// a settings document that holds one.
//
// WHY THIS FILE EXISTS (the defect it closes). A settings.json that carries a
// credential is a secret FILE, and its mode is therefore part of the
// configuration's correctness rather than a cosmetic property. Two paths can go
// wrong, and each has an owner:
//
//	(a) WIDENING — gopi stores a credential into a document whose mode is
//	    wider than private. Measured on this host 2026-10-07: the operator's own
//	    ~/.gopi/settings.json (a symlink to .ds_flash_settings.json) is mode
//	    0664 — and it ALREADY carries `api_key` for the DeepSeek/vision
//	    endpoint. A new inline doubao key written into that file must not be a
//	    silent downgrade: the legacy ~/.doubao/apikey.txt was 0664 too, and
//	    moving a secret into a file that is no more protected is not an
//	    improvement. The write path tightens the mode FIRST, in the same
//	    operation, and says so.
//
//	(b) ECHOING — a report prints the document's contents. `gopi config show`
//	    renders every key with its value; a credential key must render its
//	    SHAPE (`<set: 64 chars>`), never its bytes. The set of keys that are
//	    credentials lives HERE, once (Rule 14), so a renderer, a guard and a test
//	    cannot disagree about which keys are secrets.
//
// THE SET IS THE MECHANISM (Rule 13). A second, hand-typed list of "the secret
// keys" is how a key silently stops being redacted: the renderer's copy and the
// guard's copy drift one typo apart, and nothing is red. Every reader in the
// tree refers to CredentialSettingsKeys instead.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// APIKeySettingsKey is the settings.json key carrying the DeepSeek/Anthropic
// credential. It is also the key `vision_analyze` reuses (CONFIG.md §2c
// "api_key: reused (same DeepSeek key)"), which is why one name covers two
// product surfaces.
const APIKeySettingsKey = "api_key"

// SettingsPrivateMode is the ONLY mode a settings document carrying a
// credential may have. Named (Rule 1a) because three sites compare against it:
// the tightening below, `gopi config setup`'s create mode, and the tests that
// assert the tightening happened.
const SettingsPrivateMode os.FileMode = 0o600

// CredentialSettingsKeys names every settings.json key whose VALUE is a
// credential, in one place. A key joins this list when a `gopi config show`
// transcript containing its value would hand an attacker a live secret.
//
// It is deliberately a SHORT, adjudicated list rather than a pattern match on
// `*_key`: `doubao_key_file` ends in `_key` and holds a PATH, whose exposure is
// a much smaller thing than a token's — and a rule that redacted it would teach
// readers that the list is noise.
var CredentialSettingsKeys = []string{
	APIKeySettingsKey,
	DoubaoAPIKeySettingsKey,
	// The streaming channel's credential, inline-able since 2026-10-09: the
	// APPID is not a bearer secret on its own (it names the application) but it
	// is a credential FIELD the vendor issues beside the token, and a renderer
	// that printed one half of a credential pair while redacting the other
	// would teach readers that this list is arbitrary. Both, or the list means
	// nothing.
	DoubaoAppIDSettingsKey,
	DoubaoTokenSettingsKey,
	// The RENAMED surface's engine selector is NOT here (it carries no secret),
	// and the renamed PARAMETER keys are all nested: they are redacted through
	// AudioCredentialParamNames below, which answers for a dotted leaf PATH.
}

// IsCredentialSettingsKey reports whether a settings key carries a credential
// value. Callers that decide whether to PRINT a value or to guard a write ask
// this question rather than carrying their own copy of the list.
//
// (Named for the VALUE it answers, not `isCredentialKey()`-as-a-flag: Rule 20's
// ban is on a niladic ACCESSOR naming no subject; this one takes the key it is
// about.)
func IsCredentialSettingsKey(key string) bool {
	for _, k := range CredentialSettingsKeys {
		if key == k {
			return true
		}
	}
	return false
}

// IsAudioCredentialParamName reports whether a settings key — a TOP-LEVEL one or
// the dotted PATH `gopi config show` renders for a leaf inside the nested
// `audio_engine_param` block — names a credential VALUE.
//
// WHY A PATH AND NOT JUST THE LEAF NAME. `gopi config show` renders the nested
// object one row per leaf, keyed by the full path
// (`audio_engine_param.streaming.token`), because a single compact-JSON cell has
// no key name for the renderer to redact by — and printing a credential is the
// one outcome that is never acceptable. So the question the renderer must be
// able to ask is "is this PATH a credential?", and it is asked here, next to the
// list (Rule 14) rather than in the renderer.
//
// A top-level key answers the same question through IsCredentialSettingsKey, so
// one renderer covers both surfaces.
func IsAudioCredentialParamName(key string) bool {
	leaf := key
	if i := strings.LastIndex(key, "."); i >= 0 {
		leaf = key[i+1:]
	}
	return containsString(AudioCredentialParamNames, leaf)
}

// SettingsDocumentCarriesCredential reports whether the JSON document at path
// carries a credential VALUE, and whether the question could be ANSWERED at all.
//
// "CARRIES" IS ABOUT A VALUE, NOT A KEY NAME, and the distinction is the whole
// contract: the keys of CredentialSettingsKeys are ordinary optional settings
// whose empty string MEANS "unset" (Rule 15a), so a document that spells
// `"doubao_api_key": ""` (or `null`) holds no secret. The guard's action is a
// chmod of the OPERATOR'S OWN FILE, and an intervention of that kind has to be
// justified by a secret that is actually there — a document whose credential
// keys are all empty is left completely alone, mode included.
//
// The second return is not decoration (Rule 15): a document that cannot be read
// or parsed yields (false, false) — "I could not tell" — which callers must
// never present as "no credential here". A missing file is the ordinary
// (false, true): nothing on disk, nothing to protect.
//
// The file is read THROUGH a symlink resolved (the operator's live document is
// settings.json → .ds_flash_settings.json), because the mode that matters is
// the mode of the file the bytes are in — see SettingsFileMode below.
func SettingsDocumentCarriesCredential(path string) (carries bool, readable bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, os.IsNotExist(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return false, false
	}
	for key, raw := range top {
		if IsCredentialSettingsKey(key) && credentialValuePresent(raw) {
			return true, true
		}
		// THE NESTED SURFACE, same question one level down: since the rename
		// (2026-10-09) a credential may live inside `audio_engine_param`, and a
		// guard that only looked at the top level would leave a document holding
		// a live key world-readable — the exact downgrade this file exists to
		// prevent. A block that cannot be parsed is REPORTED as unanswerable
		// (readable=false) rather than read as "no credential here".
		if key != AudioEngineParamSettingsKey {
			continue
		}
		var blocks map[string]json.RawMessage
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return false, false
		}
		for block, blob := range blocks {
			knobs, ok := audioBlockKnobs(blob)
			if !ok {
				return false, false
			}
			for knob, leaf := range knobs {
				if !IsAudioCredentialParamName(block + "." + knob) {
					continue
				}
				if credentialValuePresent(leaf) {
					return true, true
				}
			}
		}
	}
	return false, true
}

// audioBlockKnobs decodes one engine block and reports whether it could be read
// at all. A block that is not an object is "cannot tell", never "empty".
func audioBlockKnobs(blob json.RawMessage) (map[string]json.RawMessage, bool) {
	var knobs map[string]json.RawMessage
	if err := json.Unmarshal(blob, &knobs); err != nil {
		return nil, false
	}
	return knobs, true
}

// RedactedCredentialShape renders the SHAPE of a credential value read as raw
// JSON from a settings document, for a reporter that must show the reader that
// something IS configured without printing it.
//
// It asks the SAME question about presence that the permission guard asks
// (credentialValuePresent), which is why it lives here rather than in the
// renderer: `<unset>` and "leave the operator's file alone" must agree about
// what an empty credential key means, or a document would be treated as
// credential-free by one site and as "set: 2 chars" by the other.
//
// The length is the DECODED string length. The raw cell of a JSON string
// carries its quotes, and reporting a 42-character key as "44 chars" is the
// kind of small lie an operator uses to decide whether the right value is in
// place. A non-string value reports its raw byte count, because that is all
// that can honestly be said about a shape no consumer of this knob expects.
func RedactedCredentialShape(raw json.RawMessage) string {
	if !credentialValuePresent(raw) {
		return CredentialUnsetLabel
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return fmt.Sprintf(CredentialSetShapeFormat, len(s))
	}
	return fmt.Sprintf(CredentialSetShapeFormat, len(raw))
}

// CredentialUnsetLabel is what a reporter prints for a credential key whose
// value means "not configured". It is distinct from the redacted shape on
// purpose: a reader must be able to tell "you have not set this" from "it is
// set and I am not showing you".
const CredentialUnsetLabel = "<unset>"

// CredentialSetShapeFormat is the redacted shape, with one verb for the
// value's length: `fmt.Sprintf(CredentialSetShapeFormat, n)`.
const CredentialSetShapeFormat = "<set: %d chars>"

// credentialValuePresent reports whether a credential key's raw JSON value is a
// NON-EMPTY value. `null`, `""` and a value that is only whitespace all mean
// "unset" — the same reading every other string knob in this package gives them
// (see Config's own omitempty contract).
func credentialValuePresent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "", "null", `""`:
		return false
	}
	// A JSON string: unwrap it, so `"   "` is unset rather than "some bytes".
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s) != ""
	}
	return true // a non-string value is present; the caller's own decode decides what it means
}

// SettingsFileMode returns the mode of the file a settings PATH resolves to,
// following a symlink — with the two facts a caller needs to decide whether to
// tighten: the mode, and whether the path is a link.
//
// FOLLOWING THE LINK IS THE WHOLE POINT. The operator's live document is
// `~/.gopi/settings.json` → `.ds_flash_settings.json`, and `chmod` on the link
// path does NOT reach the target on a system without the Linux-only -h/--deref
// behaviour: it either fails or, worse, changes the link's own permissions
// while the file the bytes live in stays 0664. The path gopi WRITES is resolved
// for the same reason the guard's own write path resolves it (rename(2)
// replaces the link NODE — see settings_write_guard.go's TestGuardedWrite...).
func SettingsFileMode(path string) (mode os.FileMode, err error) {
	fi, err := os.Stat(path) // Stat, not Lstat: the TARGET's mode is the question
	if err != nil {
		return 0, err
	}
	return fi.Mode().Perm(), nil
}

// TightenSettingsToPrivateMode narrows the mode of the settings document at
// path to SettingsPrivateMode when it is wider, and reports whether it changed
// anything.
//
// THE CALLER'S CONTRACT (Rule 15 — never silent): this changes the mode of a
// file the operator owns, so the caller announces a `true` return on stderr
// naming the path and both modes. A mode change nobody was told about is the
// kind of "helpful" side effect this project treats as a defect.
//
// An error means the mode is still wider than private — the caller decides
// whether that is fatal. It is reported, never swallowed: a document that stays
// world-readable while gopi believes it protected the secret is exactly the
// silent failure Rule 15 bans.
func TightenSettingsToPrivateMode(path string) (changed bool, err error) {
	mode, err := SettingsFileMode(path)
	if err != nil {
		return false, fmt.Errorf("config: cannot read the mode of %s: %w", path, err)
	}
	if mode&^SettingsPrivateMode == 0 {
		return false, nil // already private (or narrower — Group/Other bits unset)
	}
	if err := os.Chmod(path, SettingsPrivateMode); err != nil {
		return false, fmt.Errorf("config: %s is mode %04o and cannot be tightened to %04o: %w",
			path, mode, SettingsPrivateMode, err)
	}
	return true, nil
}

// CredentialModeNote renders the sentence a caller prints after
// TightenSettingsToPrivateMode returned true.
//
// ONE spelling, because the sentence is a claim about the operator's own file
// and the devdoc, the tests and the CLI all quote it: the path, the mode it had,
// and why it mattered. It never contains a credential VALUE — the message
// exists to report a permission change, and a message that quoted the secret to
// prove it was there would be the defect it is warning about.
func CredentialModeNote(path string, from os.FileMode) string {
	return fmt.Sprintf(
		"settings %s was mode %04o and carries a credential — tightened to %04o",
		path, from, SettingsPrivateMode)
}

// EnsureSettingsPrivateForCredential is the ONE call the credential-writing and
// credential-reading paths make: it refuses to proceed when the document it is
// about to handle carries a credential AND its mode cannot be made private, and
// it returns the note to print when it had to tighten.
//
// The shape of the decision, stated so a future reader does not "simplify" it:
// a path whose document carries NO credential is left completely alone (gopi
// does not reformat the operator's profile as a side effect of reading it), and
// a document that carries one is tightened FIRST — before any value is read out
// of it and before any value is written into it — so there is no window in
// which the secret is handled while the file is world-readable.
func EnsureSettingsPrivateForCredential(path string) (note string, err error) {
	carries, readable := SettingsDocumentCarriesCredential(path)
	if !carries {
		if !readable {
			// A document whose keys cannot be established: gopi cannot claim
			// the credential is absent, and it is not going to rewrite a file
			// it cannot read to find out. The CALLER's own load of the value
			// reports the parse failure with its own context; this function
			// states only what it knows.
			return "", nil
		}
		return "", nil
	}
	mode, merr := SettingsFileMode(path)
	if merr != nil {
		return "", fmt.Errorf("config: cannot read the mode of %s: %w", path, merr)
	}
	changed, cerr := TightenSettingsToPrivateMode(path)
	if cerr != nil {
		return "", cerr
	}
	if !changed {
		return "", nil
	}
	return CredentialModeNote(path, mode), nil
}
