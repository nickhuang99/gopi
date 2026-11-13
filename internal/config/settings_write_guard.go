package config

// settings_write_guard.go — THE SETTINGS-DOCUMENT LOSS GUARD.
//
// THE DEFECT THIS CLOSES (measured on this host, 2026-09-27). The operator's
// live settings document (~/.gopi/settings.json -> .ds_flash_settings.json) is
// periodically re-written by a NON-gopi writer whose round trip is
//
//	decode into a struct it models -> marshal -> rename into place
//
// and every key that struct does not model is DROPPED — silently, with no
// error, no log line and no trace. Measured state of the live document while
// .ds_flash_settings_backup.json (the same document, moments older) still
// carried them:
//
//	compute_nodes   present in the backup, absent from the document
//	task_sync       present in the backup, absent from the document
//	tmpdir          reverted twice inside ~2 minutes (task 01a0e2ea)
//
// and the dropped set is exactly the keys whose zero value is omitted by
// `json:"…,omitempty"` (Rule 15a: `compute_nodes: []` and `task_sync: false`
// are BOTH the zero value, so a struct round trip cannot round-trip them —
// the VALUE and the ABSENCE are the same bytes).
//
// Gopi is NOT that writer. Its only settings write path is `gopi config setup`, which
// by contract never touches an existing file (ensureSetupFile returns early
// when the path exists), and no other production path write-opens the document
// (checked: internal/config, internal/tools, cmd/agent — `os.Rename` sites are
// the atomic-write tool, the sandbox settings COPY, service control files and
// ledger snapshots). What gopi CAN do — and what this file does — is refuse to
// become the same writer, and make the loss LOUD where gopi is the reader.
//
// TWO HALVES, because prevention and detection fail differently:
//
//	① GuardSettingsWrite  — the WRITE side. Every gopi write to a settings
//	   document goes through it; a write that would DROP a top-level key the
//	   file currently carries is REFUSED, by name, with the keys quoted. There
//	   is deliberately NO escape hatch: a flag or env var that silences it
//	   would restore exactly the silent loss this file exists to stop
//	   (Rule 35's "an escape hatch is the rule reversed").
//	② DetectSettingsKeyLoss — the READ side. Compares the live document with
//	   its own sibling backup (the `_backup.json` convention the third-party
//	   writer itself maintains) and NAMES the keys that went missing. Gopi
//	   cannot stop another process from writing, but it must never observe the
//	   loss in silence (Rule 15 — no silent failure), and `gopi config show`
//	   is the surface that reports it.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// SettingsBackupSuffix is the sibling-backup naming convention: the document's
// own basename with this suffix inserted before the extension
// (.ds_flash_settings.json -> .ds_flash_settings_backup.json). It is a named
// constant (Rule 1) because TWO readers must mean the same file: the detector
// below and its regression test. The convention belongs to the third-party
// writer, not to gopi — gopi only READS it, which is why this is a constant and
// not a settings key.
const SettingsBackupSuffix = "_backup"

// settingsKeyLossRemedy is the one-line remedy shared by the detector's report
// and the write guard's refusal, so an operator who sees either one is told the
// same next step (Rule 14: one definition, two surfaces).
const settingsKeyLossRemedy = "re-assert the missing keys, and report the writer " +
	"(a non-gopi process re-marshalling this document drops every key it does not model)"

// settingsWriteGuardOp labels the write side in refusals.
const settingsWriteGuardOp = "settings write refused"

// ModelledSettingsKeys returns the settings.json keys the running build MODELS
// — the top-level json tags of Config, read off the struct by reflection, PLUS
// the registered top-level-ONLY readers (topLevelOnlySettingsKeys: dev_repo,
// workspace_root) which are deliberately not struct fields.
//
// DERIVED, NEVER MAINTAINED (Rules 1b/13): a hand-written second list is how
// the answer and the struct drift apart, and the whole point of this list is
// that it cannot disagree with what the decoder actually accepts. Order is
// stable (sorted) so callers can print it.
//
// BOTH READ PATHS, ONE ANSWER (bug 2026-11-13). This function used to answer
// from the STRUCT alone, and the settings-report therefore called two LIVE knobs
// "unrecognized — so it is IGNORED" while the worker printed its clone-channel
// banner reading them on the same host. "Modelled" means "some production reader
// consumes this key", and there are two such shapes: a decoded field, and a
// top-level read (devrepo.go). The second set is not transcribed here — it is
// read off its own registry, whose entries ARE the production read path.
func ModelledSettingsKeys() []string {
	t := reflect.TypeOf(Config{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			continue
		}
		keys = append(keys, name)
	}
	keys = append(keys, ModelledTopLevelOnlySettingsKeys()...)
	sort.Strings(keys)
	return keys
}

// SettingsTopLevelKeys decodes the TOP-LEVEL key set of a settings document.
//
// A map[string]json.RawMessage, deliberately: the question is "what does the
// FILE say", and decoding into the typed Config would silently answer with
// what the STRUCT knows — the very conflation this whole file exists to undo
// (Rule 15a). An unreadable or non-object document is an ERROR, never an empty
// key set: "I could not tell" must not be presented as "no keys".
func SettingsTopLevelKeys(raw []byte) ([]string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("settings document is not a JSON object: %w", err)
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// SettingsKeyLoss is the outcome of comparing a settings document against a
// reference copy of it: which keys the reference carried and the document no
// longer does (Lost), and which the document gained (Gained — a writer's own
// additions, reported so the comparison reads as a diff and not as an alarm).
type SettingsKeyLoss struct {
	Document  string
	Reference string
	Lost      []string
	Gained    []string
}

// Empty reports whether nothing was lost (gained keys do not count as loss).
func (l SettingsKeyLoss) Empty() bool { return len(l.Lost) == 0 }

// Line renders the one-line report an operator surface prints. Empty loss
// renders "" — the caller decides whether to stay silent (Rule 15's other
// half: a clean document must not raise a false alarm).
func (l SettingsKeyLoss) Line() string {
	if l.Empty() {
		return ""
	}
	return fmt.Sprintf("settings LOSS: %s is missing %d key(s) the reference %s carries: %s — %s",
		l.Document, len(l.Lost), l.Reference, strings.Join(l.Lost, ", "), settingsKeyLossRemedy)
}

// SettingsBackupPath returns the sibling-backup path for a settings document:
// the basename with SettingsBackupSuffix inserted before the extension, in the
// same directory. The extension is kept because the third-party convention
// keeps it (.ds_flash_settings.json -> .ds_flash_settings_backup.json).
//
// The path is RESOLVED FIRST, and that is the difference between the check
// working and silently comparing nothing: the operator's live document is
// reached through a symlink (~/.gopi/settings.json -> .ds_flash_settings.json),
// while the writer's backup is a sibling of the TARGET
// (.ds_flash_settings_backup.json). Asked for the symlink's sibling, this would
// look for `settings_backup.json` — a file that does not exist — and report "no
// reference, no comparison", which is a false all-clear (Rule 15). An
// unresolvable path (the file is absent) is returned unresolved: there is
// nothing to compare anyway.
func SettingsBackupPath(document string) string {
	resolved := document
	if target, err := filepath.EvalSymlinks(document); err == nil {
		resolved = target
	}
	return settingsBackupSiblingPath(resolved)
}

// settingsBackupSiblingPath is the naming rule itself, with no resolution:
// suffix before the extension, same directory.
func settingsBackupSiblingPath(document string) string {
	dir, base := filepath.Split(document)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, stem+SettingsBackupSuffix+ext)
}

// CompareSettingsKeySets is the PURE comparison: keys in ref but not in doc are
// Lost; keys in doc but not in ref are Gained. Both inputs are key LISTS, so
// this is testable without a filesystem and is the one place the direction of
// the comparison is decided (a reversed comparison would silently report a
// healthy document).
func CompareSettingsKeySets(docKeys, refKeys []string) (lost, gained []string) {
	inDoc := make(map[string]bool, len(docKeys))
	for _, k := range docKeys {
		inDoc[k] = true
	}
	inRef := make(map[string]bool, len(refKeys))
	for _, k := range refKeys {
		inRef[k] = true
		if !inDoc[k] {
			lost = append(lost, k)
		}
	}
	for _, k := range docKeys {
		if !inRef[k] {
			gained = append(gained, k)
		}
	}
	sort.Strings(lost)
	sort.Strings(gained)
	return lost, gained
}

// DetectSettingsKeyLoss compares the settings document at path with its
// sibling backup and reports the keys that went missing.
//
// The third return says whether a comparison HAPPENED: no document, or no
// reference beside it, is the ordinary case (nothing to compare against, and
// an absent reference is not evidence of loss) — never a silent "no loss".
// A document or reference that cannot be PARSED is the second return: loud,
// because an unparseable reference is exactly when a loss would go unseen.
func DetectSettingsKeyLoss(path string) (SettingsKeyLoss, bool, error) {
	loss := SettingsKeyLoss{Document: path, Reference: SettingsBackupPath(path)}
	if path == "" {
		return loss, false, nil
	}
	docRaw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loss, false, nil
		}
		return loss, false, fmt.Errorf("read the settings document %s to check for key loss: %w", path, err)
	}
	refRaw, err := os.ReadFile(loss.Reference)
	if err != nil {
		if os.IsNotExist(err) {
			return loss, false, nil
		}
		return loss, false, fmt.Errorf("read the settings reference %s to check for key loss: %w", loss.Reference, err)
	}
	docKeys, err := SettingsTopLevelKeys(docRaw)
	if err != nil {
		return loss, false, fmt.Errorf("settings key loss check, document %s: %w", path, err)
	}
	refKeys, err := SettingsTopLevelKeys(refRaw)
	if err != nil {
		return loss, false, fmt.Errorf("settings key loss check, reference %s: %w", loss.Reference, err)
	}
	loss.Lost, loss.Gained = CompareSettingsKeySets(docKeys, refKeys)
	return loss, true, nil
}

// ReportSettingsKeyLoss checks path and, when keys went missing, writes the
// report to w. It returns the loss so a caller can act on it. No loss, and
// nothing to compare against, print nothing: the report exists to break
// silence, not to add noise.
func ReportSettingsKeyLoss(w *os.File, path string) (SettingsKeyLoss, error) {
	loss, compared, err := DetectSettingsKeyLoss(path)
	if err != nil {
		fmt.Fprintf(w, "[gopi] %v\n", err)
		return loss, err
	}
	if !compared || loss.Empty() {
		return loss, nil
	}
	fmt.Fprintf(w, "[gopi] %s\n", loss.Line())
	return loss, nil
}

// GuardSettingsWrite refuses a settings write that would DROP a top-level key
// the document currently carries.
//
// HOW IT DECIDES. The on-disk document's top-level key set (or, when the path
// does not exist yet, nothing — a first write cannot lose anything) is compared
// with the candidate's own top-level key set. A key present on disk and absent
// from the candidate is a LOSS, and the write is refused:
//
//	settings write refused: /…/.ds_flash_settings.json would drop 2 key(s): compute_nodes, task_sync — re-assert the missing keys, …
//
// WHY A REFUSAL AND NOT A MERGE. A merge would decide, on the caller's behalf,
// that the dropped key is wanted — and for `compute_nodes`/`task_sync` the
// caller's own intent may legitimately BE removal. Refusing keeps that decision
// where it belongs (with whoever asked for the write) while making the loss
// impossible to commit BY ACCIDENT. A caller that genuinely means to remove a
// key must say so: see AllowSettingsKeyRemovals.
//
// The comparison is on TOP-LEVEL keys only. That is the measured loss shape
// (whole keys vanishing), a nested-key walk would need a defined merge policy
// per value shape, and a guard that silently narrows a document it cannot
// reason about would be the very defect it closes.
func GuardSettingsWrite(path string, candidate []byte) error {
	if path == "" {
		return fmt.Errorf("%s: no settings path to guard", settingsWriteGuardOp)
	}
	if _, err := SettingsTopLevelKeys(candidate); err != nil {
		return fmt.Errorf("%s for %s: the candidate is not a JSON object: %w", settingsWriteGuardOp, path, err)
	}
	return guardSettingsKeyLoss(path, candidate, nil)
}

// AllowSettingsKeyRemovals is GuardSettingsWrite for a caller that MEANS to
// remove keys: exactly these keys may disappear, and any OTHER loss is still
// refused. It is not an escape hatch — it cannot silence the guard, it can only
// declare, by name, which removals were intended.
func AllowSettingsKeyRemovals(path string, candidate []byte, intentional []string) error {
	if _, err := SettingsTopLevelKeys(candidate); err != nil {
		return fmt.Errorf("%s for %s: the candidate is not a JSON object: %w", settingsWriteGuardOp, path, err)
	}
	return guardSettingsKeyLoss(path, candidate, intentional)
}

// guardSettingsKeyLoss is the one comparison behind both entry points (Rule 2:
// one place decides what "a loss" is).
func guardSettingsKeyLoss(path string, candidate []byte, intentional []string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing on disk → nothing to lose
		}
		return fmt.Errorf("%s for %s: read the current document: %w", settingsWriteGuardOp, path, err)
	}
	oldKeys, err := SettingsTopLevelKeys(existing)
	if err != nil {
		// The document on disk is unreadable as an object. Refusing is the only
		// safe answer: a write would replace bytes gopi cannot account for.
		return fmt.Errorf("%s for %s: the current document cannot be read to check for key loss: %w",
			settingsWriteGuardOp, path, err)
	}
	newKeys, err := SettingsTopLevelKeys(candidate)
	if err != nil {
		return fmt.Errorf("%s for %s: the candidate is not a JSON object: %w", settingsWriteGuardOp, path, err)
	}
	sort.Strings(intentional)
	dropped := make([]string, 0, len(oldKeys))
	for _, k := range oldKeys {
		if containsString(newKeys, k) || containsString(intentional, k) {
			continue
		}
		dropped = append(dropped, k)
	}
	if len(dropped) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s would drop %d key(s): %s — %s",
		settingsWriteGuardOp, path, len(dropped), strings.Join(dropped, ", "), settingsKeyLossRemedy)
}

// containsString reports whether want is in the (sorted) list.
func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// WriteSettingsPreservingKeys is the ONE gopi write path for a settings
// document: it guards the write (GuardSettingsWrite) and only then installs the
// candidate — tmp file in the destination directory, then rename, so a reader
// never sees a half-written record.
//
// A caller with deliberate removals uses WriteSettingsPreservingKeysExcept.
// There is no "force" spelling: a write that would lose a key is a decision,
// and the API makes the caller state it.
func WriteSettingsPreservingKeys(path string, candidate []byte, perm os.FileMode) error {
	return writeSettingsGuarded(path, candidate, nil, perm)
}

// WriteSettingsPreservingKeysExcept is WriteSettingsPreservingKeys for a caller
// that declares which key removals are intentional. Any OTHER loss is refused.
func WriteSettingsPreservingKeysExcept(path string, candidate []byte, intentional []string, perm os.FileMode) error {
	return writeSettingsGuarded(path, candidate, intentional, perm)
}

func writeSettingsGuarded(path string, candidate []byte, intentional []string, perm os.FileMode) error {
	if err := AllowSettingsKeyRemovals(path, candidate, intentional); err != nil {
		return err
	}
	// INSTALL ONTO THE RESOLVED PATH. The operator's live document is a symlink
	// (settings.json -> .ds_flash_settings.json), and rename(2) replaces the
	// NODE — installing onto the link path would silently swap the link for a
	// regular file and leave the file it pointed at behind (this repo documents
	// the same hazard for the sandbox profile). Resolving keeps the link a link
	// and the write where the operator's document actually lives.
	install := path
	if target, err := filepath.EvalSymlinks(path); err == nil {
		install = target
	}
	tmp, err := os.CreateTemp(filepath.Dir(install), filepath.Base(install)+".tmp-*")
	if err != nil {
		return fmt.Errorf("stage the settings write for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //stylegate:ignore the rename below removes it; this is the failure path
	if _, err := tmp.Write(candidate); err != nil {
		tmp.Close() //stylegate:ignore closing the staging handle after a failed write; the error reported is the write
		return fmt.Errorf("write the staged settings document %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close() //stylegate:ignore closing the staging handle after a failed chmod; the error reported is the chmod
		return fmt.Errorf("set the mode of the staged settings document %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close the staged settings document %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, install); err != nil {
		return fmt.Errorf("install the guarded settings document %s: %w", install, err)
	}
	return nil
}
