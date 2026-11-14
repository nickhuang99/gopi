package config

// settings_write_guard_test.go — the regression suite for the settings-document
// LOSS guard (settings_write_guard.go).
//
// The defect it reproduces is the measured one on this host (2026-09-27): a
// non-gopi writer re-marshals the operator's live settings document and the
// keys its struct does not model vanish — silently. `memory_peers` and
// `task_sync` are missing from the live document while its own sibling backup
// still carries them, and `tmpdir` was reverted twice inside ~2 minutes.
//
// Rule 32's SCAFFOLD IS UNAVAILABLE HERE, and that is a measured fact rather
// than a preference: internal/testutil imports internal/config, so a config
// test that imported testutil would be an import cycle (the build says
// "import cycle not allowed in test"). This package's own convention is
// t.TempDir() — the same fresh per-test directory the scaffold hands out, under
// the test binary's own temp root — and every fixture below is written there.
// The operator's live profile is never opened, let alone written: no path in
// this file is derived from $HOME or from GOPI_SETTINGS. Rule 27: nothing here
// creates load; the "round trip" is one in-process re-marshal.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settingsGuardFileMode is the fixture document's mode — the ordinary settings
// file mode, not a secret.
const settingsGuardFileMode = 0o600

// guardFixture is a settings document carrying a key that survives a typed round
// trip (`model`) plus two that do NOT (`memory_peers` / `task_sync`) — the
// measured casualties, written into a per-test scratch directory.
//
// WHY THOSE TWO STILL VANISH (re-derived 2026-10-22, task 01a160a2): the typed
// Config MODELS both, and that is not enough — each carries `omitempty` and each
// fixture value IS its zero value (an empty list, `false`), so a decode/marshal
// round trip cannot tell "set to empty" from "never set" (Rule 15a). The
// unmodelled-key casualty this file was written around (`compute_nodes`) was
// DELETED with its knob in the same task, so the fixture key moved to a live
// key of the same shape rather than pinning a name the tree no longer has.
type guardFixture struct {
	dir      string
	path     string
	backup   string
	document map[string]any
}

func newGuardFixture(t *testing.T) guardFixture {
	t.Helper()
	// The scaffold is unavailable in this package (import cycle, see the file
	// header); t.TempDir() is this package's convention and is the same shape —
	// a fresh directory only this test knows, never the operator's profile.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	document := map[string]any{
		"api_key":      "sk-fixture-not-a-real-key",
		"model":        "fixture-model",
		"memory_peers": []any{},
		"task_sync":    false,
		"tmpdir":       filepath.Join(dir, "tmp"),
	}
	fx := guardFixture{dir: dir, path: path, backup: SettingsBackupPath(path), document: document}
	fx.write(t, document)
	// The sibling backup is the writer's OWN convention (Rule 35: gopi reads it,
	// it does not create it) — it is the reference the loss detector compares
	// against, and it is what makes the loss observable at all.
	fx.writeRaw(t, fx.backup, mustJSON(t, document))
	return fx
}

func (f guardFixture) write(t *testing.T, doc map[string]any) {
	t.Helper()
	f.writeRaw(t, f.path, mustJSON(t, doc))
}

func (f guardFixture) writeRaw(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, settingsGuardFileMode); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func (f guardFixture) keys(t *testing.T) []string {
	t.Helper()
	keys, err := SettingsTopLevelKeys(mustRead(t, f.path))
	if err != nil {
		t.Fatalf("read fixture keys: %v", err)
	}
	return keys
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestSettingsKeyLossIsReproducedPresentThenAbsent is the REPRODUCTION the
// epic asks for, in-process and deterministic: the key is PRESENT, one
// writer-shaped round trip happens, and the key is ABSENT — with the same
// document's sibling backup still carrying it.
//
// The "writer" here is the SHAPE, not the unknown process: decode into the
// typed Config (which models neither key) -> marshal -> install. That is
// exactly the round trip the live document shows, and reproducing it as a SHAPE
// is what lets the guard be tested at all.
func TestSettingsKeyLossIsReproducedPresentThenAbsent(t *testing.T) {
	fx := newGuardFixture(t)

	// ① PRESENT: the document carries both keys the round trip will destroy.
	before := fx.keys(t)
	for _, key := range []string{MemoryPeersSettingsKey, TaskSyncSettingsKey} {
		if !containsString(before, key) {
			t.Fatalf("fixture starts without %q — the reproduction needs it PRESENT first (keys: %v)", key, before)
		}
	}

	// ② ONE WRITER-SHAPED ROUND TRIP: decode into the typed Config, marshal it
	// back. This is the lossy step, and it is silent — no error to observe.
	typed := mustRead(t, fx.path)
	var cfg Config
	if err := json.Unmarshal(typed, &cfg); err != nil {
		t.Fatalf("decode fixture into Config: %v", err)
	}
	roundTripped := mustJSON(t, &cfg)
	if err := os.WriteFile(fx.path, roundTripped, settingsGuardFileMode); err != nil {
		t.Fatalf("install the round-tripped document: %v", err)
	}

	// ③ ABSENT: both keys are gone, and the typed struct has no field for them
	// (Rule 15a — `[]` and `false` ARE their zero values, so omitempty erases
	// the difference between "set to empty" and "never set").
	after := fx.keys(t)
	for _, key := range []string{MemoryPeersSettingsKey, TaskSyncSettingsKey} {
		if containsString(after, key) {
			t.Fatalf("%q survived the round trip — this fixture no longer reproduces the loss (keys: %v)", key, after)
		}
	}

	// ④ The sibling backup still carries them, which is how the loss is visible
	// at all — and the detector must name exactly those keys, in both directions.
	loss, compared, err := DetectSettingsKeyLoss(fx.path)
	if err != nil {
		t.Fatalf("detect key loss: %v", err)
	}
	if !compared {
		t.Fatal("no comparison happened — a sibling backup was written, so one was expected")
	}
	if loss.Empty() {
		t.Fatal("the detector reported no loss, but two keys are gone")
	}
	want := []string{MemoryPeersSettingsKey, TaskSyncSettingsKey}
	if strings.Join(loss.Lost, ",") != strings.Join(want, ",") {
		t.Errorf("lost = %v, want exactly %v", loss.Lost, want)
	}
	line := loss.Line()
	for _, key := range want {
		if !strings.Contains(line, key) {
			t.Errorf("the loss report does not name %q: %s", key, line)
		}
	}
}

// TestGuardSettingsWriteRefusesToDropAKey is the guard's headline contract: a
// write that would drop a key the document carries is REFUSED, by name, and the
// document on disk is UNTOUCHED.
func TestGuardSettingsWriteRefusesToDropAKey(t *testing.T) {
	fx := newGuardFixture(t)
	before := mustRead(t, fx.path)

	lossy := mustJSON(t, map[string]any{"api_key": "sk-fixture", "model": "fixture-model"})
	err := WriteSettingsPreservingKeys(fx.path, lossy, settingsGuardFileMode)
	if err == nil {
		t.Fatal("the guard allowed a write that drops memory_peers, task_sync and tmpdir")
	}
	for _, key := range []string{MemoryPeersSettingsKey, TaskSyncSettingsKey, TmpDirSettingsKey} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the refusal does not name %q: %v", key, err)
		}
	}
	if got := mustRead(t, fx.path); string(got) != string(before) {
		t.Errorf("the document was modified by a REFUSED write:\n got %s\nwant %s", got, before)
	}
}

// TestGuardSettingsWriteAllowsAdditionsAndIntentionalRemovals keeps the guard
// narrow: it must not become a "never write" rule. An ADDITION passes, and a
// declared removal passes — while an UNDECLARED one beside it is still refused.
func TestGuardSettingsWriteAllowsAdditionsAndIntentionalRemovals(t *testing.T) {
	fx := newGuardFixture(t)

	// Additions only: every existing key is preserved, one new key appears.
	added := map[string]any{}
	for k, v := range fx.document {
		added[k] = v
	}
	added[OutputLanguageSettingsKey] = "zh"
	if err := WriteSettingsPreservingKeys(fx.path, mustJSON(t, added), settingsGuardFileMode); err != nil {
		t.Fatalf("an ADDITIVE write was refused: %v", err)
	}
	if keys := fx.keys(t); !containsString(keys, OutputLanguageSettingsKey) {
		t.Fatalf("the addition did not land (keys: %v)", keys)
	}

	// A DECLARED removal passes …
	withoutTmp := map[string]any{}
	for k, v := range added {
		if k == TmpDirSettingsKey {
			continue
		}
		withoutTmp[k] = v
	}
	if err := WriteSettingsPreservingKeysExcept(fx.path, mustJSON(t, withoutTmp), []string{TmpDirSettingsKey}, settingsGuardFileMode); err != nil {
		t.Fatalf("a declared removal was refused: %v", err)
	}

	// … and an UNDECLARED one is still refused (there is no force spelling).
	err := WriteSettingsPreservingKeys(fx.path, mustJSON(t, map[string]any{"api_key": "sk-fixture"}), settingsGuardFileMode)
	if err == nil {
		t.Fatal("the guard allowed an undeclared removal of every modelled key")
	}
}

// TestGuardSettingsWriteIsLoudOnAnUnreadableDocument pins Rule 15: "I could not
// tell" must never be answered as "the write is fine". A document that is not a
// JSON object cannot be checked for loss, so the write is refused and the
// refusal says why.
func TestGuardSettingsWriteIsLoudOnAnUnreadableDocument(t *testing.T) {
	fx := newGuardFixture(t)
	fx.writeRaw(t, fx.path, []byte("{not json"))

	err := WriteSettingsPreservingKeys(fx.path, mustJSON(t, fx.document), settingsGuardFileMode)
	if err == nil {
		t.Fatal("the guard allowed a write over a document it could not read")
	}
	if !strings.Contains(err.Error(), "cannot be read to check for key loss") {
		t.Errorf("the refusal does not explain the unreadable document: %v", err)
	}
	if !strings.Contains(err.Error(), settingsWriteGuardOp) {
		t.Errorf("the refusal is not labelled as the write guard: %v", err)
	}
}

// TestGuardSettingsWriteOnAFreshPathIsAllowed keeps the first-write case
// ordinary: with nothing on disk there is nothing to lose, so the write lands.
func TestGuardSettingsWriteOnAFreshPathIsAllowed(t *testing.T) {
	fx := newGuardFixture(t)
	fresh := filepath.Join(fx.dir, "fresh-settings.json")
	if err := WriteSettingsPreservingKeys(fresh, mustJSON(t, fx.document), settingsGuardFileMode); err != nil {
		t.Fatalf("a first write to an absent path was refused: %v", err)
	}
	if keys, err := SettingsTopLevelKeys(mustRead(t, fresh)); err != nil || len(keys) != len(fx.document) {
		t.Fatalf("the fresh write did not land intact: keys=%v err=%v", keys, err)
	}
}

// TestModelledSettingsKeysCannotDriftFromTheStruct is the anti-drift check on
// the list the guard's reports derive from (Rules 1b/13): the keys are read off
// Config by reflection, so a field the struct gains is reported without a second
// edit — and a key the SETTINGS SURFACE names as modelled must appear here.
func TestModelledSettingsKeysCannotDriftFromTheStruct(t *testing.T) {
	keys := ModelledSettingsKeys()
	for _, key := range []string{
		MemoryPeersSettingsKey, TaskSyncSettingsKey, TmpDirSettingsKey,
		ReposSettingsKey, SyncPeersSettingsKey, NotifySettingsKey,
	} {
		if !containsString(keys, key) {
			t.Errorf("ModelledSettingsKeys() does not carry %q — the reflection list drifted from Config", key)
		}
	}
	// The two measured casualties ARE modelled today: their absence from the live
	// document is the third-party writer's loss, not an unmapped key.
	for _, key := range []string{MemoryPeersSettingsKey, TaskSyncSettingsKey} {
		if !containsString(keys, key) {
			t.Errorf("Config does not model %q, but gopi reads the key — the loss would be invisible", key)
		}
	}
}

// TestGuardedWriteThroughASymlinkKeepsTheLink pins the install-path rule: the
// operator's live document is reached through a symlink
// (settings.json -> .ds_flash_settings.json), and rename(2) REPLACES the node,
// so installing onto the link path would swap the link for a regular file and
// leave the target behind — the write would land somewhere the operator's
// document is not. The guarded write resolves first.
func TestGuardedWriteThroughASymlinkKeepsTheLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-settings.json")
	link := filepath.Join(dir, "settings.json")
	document := map[string]any{"api_key": "sk-fixture", "memory_peers": []any{}}
	if err := os.WriteFile(target, mustJSON(t, document), settingsGuardFileMode); err != nil {
		t.Fatalf("write the target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create the symlink: %v", err)
	}

	grown := map[string]any{"api_key": "sk-fixture", "memory_peers": []any{}, "model": "fixture"}
	if err := WriteSettingsPreservingKeys(link, mustJSON(t, grown), settingsGuardFileMode); err != nil {
		t.Fatalf("guarded write through a symlink: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat the link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the write REPLACED the symlink with a regular file — the install path was not resolved")
	}
	keys, err := SettingsTopLevelKeys(mustRead(t, target))
	if err != nil {
		t.Fatalf("read the target after the guarded write: %v", err)
	}
	if !containsString(keys, "model") {
		t.Fatalf("the write did not land in the LINK TARGET (keys: %v)", keys)
	}
	// The staged temp file must not survive either (no litter beside the doc).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the fixture dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}
}

// TestSettingsBackupPathFollowsTheSiblingConvention pins the naming rule the
// detector depends on: suffix inserted before the extension, same directory —
// and that the SYMLINK case resolves to the target's sibling, because the
// operator's live document is a symlink and comparing the link's own sibling
// finds a file that does not exist (a false all-clear).
func TestSettingsBackupPathFollowsTheSiblingConvention(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/home/user/.gopi/.ds_flash_settings.json", "/home/user/.gopi/.ds_flash_settings_backup.json"},
		{"/tmp/x/settings.json", "/tmp/x/settings_backup.json"},
		{"/tmp/x/settings", "/tmp/x/settings_backup"},
	}
	for _, tc := range cases {
		if got := settingsBackupSiblingPath(tc.in); got != tc.want {
			t.Errorf("settingsBackupSiblingPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// The symlink case, with a real link on disk: the reference is the TARGET's
	// sibling, and the empty-backup case proves the resolver is doing the work
	// (unresolved, it would have looked for `settings_backup.json` and found
	// nothing — the false all-clear this test exists to forbid).
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(target, []byte("{}"), settingsGuardFileMode); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create the symlink: %v", err)
	}
	want := filepath.Join(dir, "settings_backup.json")
	if got := SettingsBackupPath(link); got != want {
		t.Errorf("SettingsBackupPath(symlink) = %q, want the target's sibling %q", got, want)
	}
	// …and the reference it names must be the one the detector actually opens.
	_, _, err := DetectSettingsKeyLoss(link)
	if err != nil {
		t.Fatalf("detect on a symlinked document: %v", err)
	}
}

// TestCompareSettingsKeySetsDirectionIsNotReversed is the one-line guard on the
// comparison's DIRECTION: a reversed comparison reports a healthy document as
// healthy and a damaged one as healthy too.
func TestCompareSettingsKeySetsDirectionIsNotReversed(t *testing.T) {
	lost, gained := CompareSettingsKeySets([]string{"api_key", "model"}, []string{"api_key", "model", "tmpdir"})
	if strings.Join(lost, ",") != "tmpdir" {
		t.Errorf("lost = %v, want [tmpdir] (the key the DOCUMENT lost is the one the REFERENCE carries)", lost)
	}
	if len(gained) != 0 {
		t.Errorf("gained = %v, want none", gained)
	}
}
