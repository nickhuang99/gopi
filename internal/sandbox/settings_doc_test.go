package sandbox

// settings_doc_test.go — the RED-then-green pair for the 2026-09-27 settings
// clobber, plus its positive control.
//
// THE DEFECT (measured, not hypothesised). The profile's `settings.json` used to
// be a SYMLINK to the operator's live document, documented "read-only by
// contract". A contract is not an enforcement: the worker session that lost the
// file ran, inside a sandbox created by `gopi tool sandbox create`,
//
//	printf '{"tmpdir":"%s/tmp"}' "$SB" > "$SB/.gopi/settings.json"
//
// The redirect follows a symlink, so it TRUNCATED ~/.gopi/settings.json — the
// api_key, the repos table `gopi merge` keys on, the notify sink, twelve keys in
// all — and left a 155-byte test fixture behind. On the PRE-CHANGE tree the tests
// below FAIL: the profile path is a symlink and the redirect rewriting it also
// rewrites the source. After the change (a read-only copy) they pass, and the
// source file is bit-for-bit unchanged.
//
// WHY A COPY CANNOT REPEAT IT: the profile path is now a regular file of its own.
// The redirect truncates THE COPY — a loud, local, harmless mistake — while the
// operator's document is a different inode that nothing in the sandbox reaches.
// And the copy is 0444, so even the redirect itself is refused by the kernel.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// settingsDocBytes reads a settings document, failing the test with the path
// when it cannot (a missing source must never read as an empty document — that
// would make the comparison below vacuous).
func settingsDocBytes(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings document %s: %v", path, err)
	}
	return string(raw)
}

// TestProfileSettingsWriteDoesNotReachTheRealDocument is the RED-then-green
// proof: the exact shell shape that destroyed the live file — a truncating
// redirect through the profile's settings path — must not change the operator's
// document.
func TestProfileSettingsWriteDoesNotReachTheRealDocument(t *testing.T) {
	root := demoRoot(t)
	real := settingsFile(t, root)
	before := settingsDocBytes(t, real)

	sb, err := Create(root, "", Options{Label: "clobberproof", ProfileSettingsPath: real})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	profile := SettingsFileNameIn(sb.Path)
	fixture := []byte(`{"tmpdir":"/tmp/whatever","repos":{"/elsewhere":{}}}`)

	// THE WRITE, in the shape that clobbered the live document: open the profile
	// path for truncation, exactly as `> "$SB/.gopi/settings.json"` does.
	werr := os.WriteFile(profile, fixture, 0o644)

	// The invariants, asserted whether or not the kernel refused the write: the
	// operator's document is bit-for-bit what it was, and the profile path is
	// not a link to it.
	if got := settingsDocBytes(t, real); got != before {
		t.Fatalf("RED: the write through the profile settings path %s CHANGED the real document %s:\n got %q\nwant %q",
			profile, real, got, before)
	}
	if info, lerr := os.Lstat(profile); lerr != nil {
		t.Fatalf("lstat the profile settings %s: %v", profile, lerr)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("RED: the profile settings %s is a symlink — a redirect follows it into the real file", profile)
	}
	if werr == nil {
		// The shape is a copy and the write landed on the COPY: the operator's
		// file is untouched, which is the property under test. Say which arm ran
		// so a reader is never left guessing (Rule 15: no silent either-arm).
		t.Logf("the write landed on the sandbox's own copy %s (a separate inode); the real document %s is unchanged",
			profile, real)
		if got := settingsDocBytes(t, profile); got == before {
			t.Errorf("the copy %s still holds the source bytes — the write did not land anywhere", profile)
		}
		return
	}
	t.Logf("the write was REFUSED (mode 0444 copy): %v — the real document %s is unchanged", werr, real)
}

// TestProfileSettingsCopyIsMode0444: the copy is read-only, so the ordinary
// fixture redirect does not merely miss the operator's file — it fails loudly at
// the write, in the sandbox, where the mistake was made.
func TestProfileSettingsCopyIsMode0444(t *testing.T) {
	root := demoRoot(t)
	real := settingsFile(t, root)
	sb, err := Create(root, "", Options{Label: "copymode", ProfileSettingsPath: real})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, err := os.Lstat(SettingsFileNameIn(sb.Path))
	if err != nil {
		t.Fatalf("lstat the copy: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o444 {
		t.Errorf("the profile settings copy is mode %o, want 0444", perm)
	}
	if _, werr := os.OpenFile(SettingsFileNameIn(sb.Path), os.O_WRONLY|os.O_TRUNC, 0); werr == nil {
		t.Errorf("the profile settings copy %s accepted a truncating write — the kernel is the enforcement "+
			"behind calling it read-only", SettingsFileNameIn(sb.Path))
	}
}

// TestOperatorDocumentIsNotAWriteTarget is the POSITIVE CONTROL: a legitimate
// settings write still works. A sandbox run declares its own knobs in its OWN
// settings file (GOPI_SETTINGS names it) — the guard must not have become
// "nothing can write settings".
func TestOperatorDocumentIsNotAWriteTarget(t *testing.T) {
	root := demoRoot(t)
	real := settingsFile(t, root)
	if _, err := Create(root, "", Options{Label: "control", ProfileSettingsPath: real}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The real document is still an ordinary writable file for its OWNER — the
	// run's fixture writes it directly, as every test fixture in the tree does.
	want := `{"model":"demo","tmpdir":"/tmp/legit"}`
	if err := os.WriteFile(real, []byte(want), 0o600); err != nil {
		t.Fatalf("a direct write to a settings document must still work (%s): %v", real, err)
	}
	if got := settingsDocBytes(t, real); got != want {
		t.Errorf("the settings document reads %q, want %q", got, want)
	}
}

// TestPreFixSymlinkAtTheProfilePathIsReplacedNotFollowed: the migration arm. A
// sandbox created by the PRE-CHANGE initializer has a symlink at the profile
// path; re-materialising the profile (Create/Ensure — the documented repair path)
// must UNLINK it, never write through it into the operator's file.
func TestPreFixSymlinkAtTheProfilePathIsReplacedNotFollowed(t *testing.T) {
	root := demoRoot(t)
	real := settingsFile(t, root)
	before := settingsDocBytes(t, real)

	sb, err := Create(root, "", Options{Label: "migrate", ProfileSettingsPath: real, SkipSettingsDoc: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	profile := SettingsFileNameIn(sb.Path)
	// Rebuild the OLD shape deliberately (what an existing sandbox on this host
	// carries): the profile path is a symlink to the real document.
	if err := os.Symlink(real, profile); err != nil {
		t.Fatalf("link the real document as the legacy profile settings: %v", err)
	}

	if _, err := Ensure(sb.Path, root, config.SandboxRootSourceFlag, Options{
		ProfileSettingsPath:       real,
		ReplaceLegacySettingsLink: true,
	}); err != nil {
		t.Fatalf("Ensure over a pre-fix symlink: %v", err)
	}
	info, err := os.Lstat(profile)
	if err != nil {
		t.Fatalf("lstat the migrated profile settings: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the pre-fix symlink survived the migration — a later redirect would reach %s", real)
	}
	if got := settingsDocBytes(t, real); got != before {
		t.Fatalf("the migration CHANGED the real document %s:\n got %q\nwant %q", real, got, before)
	}
	if got := settingsDocBytes(t, profile); got != before {
		t.Errorf("the migrated copy holds %q, want the operator's document %q", got, before)
	}
}

// TestPreFixSymlinkIsRefusedWithoutTheMigrationFlag: the DEFAULT answer to a
// pre-fix symlink is a refusal that names the path, the target and the repair —
// never a silent unlink. A silent unlink leaves a window in which anything that
// opened the profile path (a shell redirect, a fixture) is still writing THROUGH
// the link into the operator's live document, which is the whole defect.
func TestPreFixSymlinkIsRefusedWithoutTheMigrationFlag(t *testing.T) {
	root := demoRoot(t)
	real := settingsFile(t, root)
	before := settingsDocBytes(t, real)

	sb, err := Create(root, "", Options{Label: "legacyrefused", ProfileSettingsPath: real, SkipSettingsDoc: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	profile := SettingsFileNameIn(sb.Path)
	if err := os.Symlink(real, profile); err != nil {
		t.Fatalf("link the real document as the legacy profile settings: %v", err)
	}

	_, err = Ensure(sb.Path, root, config.SandboxRootSourceFlag, Options{ProfileSettingsPath: real})
	if err == nil {
		t.Fatalf("Ensure ACCEPTED a pre-fix settings symlink — the write-through shape must be refused by name")
	}
	msg := err.Error()
	if !strings.Contains(msg, profile) || !strings.Contains(msg, real) {
		t.Errorf("the refusal does not name both the link (%s) and its target (%s): %s", profile, real, msg)
	}
	// Nothing was touched: the link is still the link and the operator's
	// document is unchanged (a refusal that repaired would be the silent window
	// this test exists to forbid).
	if cur, lerr := os.Readlink(profile); lerr != nil || cur != real {
		t.Errorf("the refused settings link was modified (target %q, err %v)", cur, lerr)
	}
	if got := settingsDocBytes(t, real); got != before {
		t.Errorf("the refusal changed the real document %s:\n got %q\nwant %q", real, got, before)
	}
}

// TestSettingsDocSourceInsideTheSandboxIsRefused: the self-referential shape the
// link era refused must stay refused by the copy — a profile configured by its
// own contents tests nothing, and now it would also copy a file onto itself.
func TestSettingsDocSourceInsideTheSandboxIsRefused(t *testing.T) {
	root := demoRoot(t)
	sb, err := Create(root, "", Options{Label: "selfrefcopy", SkipSettingsDoc: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	inside := filepath.Join(sb.Path, config.SandboxSettingsFileName)
	_, err = Ensure(sb.Path, root, config.SandboxRootSourceFlag, Options{ProfileSettingsPath: inside})
	if err == nil {
		t.Fatalf("a settings source INSIDE the sandbox was accepted — the self-referential shape must be refused")
	}
	if !strings.Contains(err.Error(), inside) {
		t.Errorf("the refusal does not name the source it refused (%s): %v", inside, err)
	}
}
