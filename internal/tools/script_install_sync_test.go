package tools

// The installed-script refresh's contract (rework of
// fix/launcher-install-refresh, 2026-09-12).
//
// THE DEFECT THESE TESTS PIN (reproduced on the merged state): the original fix
// added an audit seam (`--check`) plus byte-identity gates, all of which passed
// — and the host still ran the PRE-fix installed copy, because nothing re-ran
// `--install` after the merge. A merge changes the repo's script; the operator's
// shell runs ~/.local/bin/<name>. These tests are written from the INSTALLED
// side, which is the side every pre-existing gate missed.
//
// The fixtures use an isolated HOME (t.Setenv) so the real ~/.local/bin is
// never touched, and a self-installing fixture script that mirrors the seam's
// real shape (copy itself to $HOME/.local/bin/<own name>) rather than the
// production script itself — production scripts carry peer ssh / systemd side
// effects that a unit test must not trigger.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installSeamScript is a minimal script carrying the install seam's real
// shape: `--install` copies ITSELF to $HOME/.local/bin/<its own name>, the way
// every operator script in this repo does. extra is appended to the body (used
// to build the failure fixture).
func installSeamScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"SELF=\"${0##*/}\"\n" +
		"if [[ \"${1:-}\" == \"" + scriptSyncInstallFlag + "\" ]]; then\n" +
		body +
		"exit 0\nfi\necho \"launch path (a test must never reach this)\" >&2\nexit 9\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// sandboxHome creates an isolated HOME and returns it.
func sandboxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir is HOME on linux; assert the sandbox took effect rather
	// than trusting it (a leaked real HOME would make these tests destructive).
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Fatalf("sandbox HOME did not take effect: got %q err=%v want %q", got, err, home)
	}
	return home
}

// installedPath is where the seam must land a script for a given HOME.
func installedPath(home, name string) string {
	return filepath.Join(home, filepath.FromSlash(ScriptSyncDirRel), name)
}

// The self-install body every fixture script shares: copy myself to the
// stable dir. Mirrors `install -m 0755 "${BASH_SOURCE[0]}" "$stable"`.
const selfInstallBody = "  mkdir -p \"$HOME/" + ScriptSyncDirRel + "\"\n" +
	"  install -m 0755 \"${BASH_SOURCE[0]}\" \"$HOME/" + ScriptSyncDirRel + "/$SELF\"\n" +
	"  echo \"installed: $HOME/" + ScriptSyncDirRel + "/$SELF\"\n"

// TestSyncInstalledScriptsRefreshesDriftedCopy is THE defect test: the installed
// copy is the pre-merge bytes while the repo copy has moved on (exactly the
// incident: installed 4b2ad5a2…/16457 B vs repo 98e3ead4…/19138 B) → the pass
// must re-install it AND the installed bytes must then equal the repo's.
//
// Asserting byte identity — not "the child exited 0" — is the point: an install
// that silently no-ops is the same class of silent degradation the whole epic
// is about.
func TestSyncInstalledScriptsRefreshesDriftedCopy(t *testing.T) {
	home := sandboxHome(t)
	repo := t.TempDir()
	scriptsDir := filepath.Join(repo, scriptSyncRepoDirName)
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "gopi-worker-up.sh"
	src := installSeamScript(t, scriptsDir, name, selfInstallBody)

	// Pre-seed a DRIFTED installed copy (the incident's shape: same name, other
	// bytes — the pre-merge build).
	if err := os.MkdirAll(filepath.Dir(installedPath(home, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath(home, name), []byte("#!/usr/bin/env bash\n# the PRE-fix build\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := SyncInstalledScripts(repo)
	if len(res.Failed) != 0 {
		t.Fatalf("a healthy refresh must not report failures: %v", res.Failed)
	}
	if len(res.Refreshed) != 1 || res.Refreshed[0] != name {
		t.Fatalf("refreshed = %v, want [%s] (the installed copy had drifted)", res.Refreshed, name)
	}
	got, err := os.ReadFile(installedPath(home, name))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("after the refresh the installed copy still is not the repo's bytes:\n got %q\nwant %q", got, want)
	}
}

// TestSyncInstalledScriptsLeavesCurrentCopyAlone: an already-identical installed
// copy is reported Current and NOT re-installed (no needless writes on every
// merge round).
func TestSyncInstalledScriptsLeavesCurrentCopyAlone(t *testing.T) {
	home := sandboxHome(t)
	repo := t.TempDir()
	scriptsDir := filepath.Join(repo, scriptSyncRepoDirName)
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "gopi-worker-up.sh"
	src := installSeamScript(t, scriptsDir, name, selfInstallBody)
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(installedPath(home, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath(home, name), raw, 0o755); err != nil {
		t.Fatal(err)
	}

	res := SyncInstalledScripts(repo)
	if len(res.Refreshed) != 0 {
		t.Fatalf("refreshed = %v, want none (the copies already matched)", res.Refreshed)
	}
	if len(res.Current) != 1 || res.Current[0] != name {
		t.Fatalf("current = %v, want [%s]", res.Current, name)
	}
}

// TestSyncInstalledScriptsNeverInstallsWhatWasNotInstalled pins the scope
// boundary that keeps this pass non-destructive: WHICH scripts a host runs is
// the operator's decision, so a script with no installed copy is reported and
// left alone — the merge must never create a new installed script.
func TestSyncInstalledScriptsNeverInstallsWhatWasNotInstalled(t *testing.T) {
	home := sandboxHome(t)
	repo := t.TempDir()
	scriptsDir := filepath.Join(repo, scriptSyncRepoDirName)
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "gopi-home-gc.sh"
	installSeamScript(t, scriptsDir, name, selfInstallBody)

	res := SyncInstalledScripts(repo)
	if len(res.NotInstalled) != 1 || res.NotInstalled[0] != name {
		t.Fatalf("notInstalled = %v, want [%s]", res.NotInstalled, name)
	}
	if _, err := os.Stat(installedPath(home, name)); !os.IsNotExist(err) {
		t.Fatalf("a merge must NOT create an installed script the operator never installed (stat err=%v)", err)
	}
}

// TestSyncInstalledScriptsSkipsScriptWithoutInstallSeam: Rule 21's shape — a
// script that does not DECLARE the seam is not invoked with it (calling it would
// be an unknown-flag error, i.e. a fabricated failure). The fixture exits 9 on
// any non-install invocation, so a wrong call cannot pass silently.
func TestSyncInstalledScriptsSkipsScriptWithoutInstallSeam(t *testing.T) {
	home := sandboxHome(t)
	repo := t.TempDir()
	scriptsDir := filepath.Join(repo, scriptSyncRepoDirName)
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "no-seam.sh"
	contract := "#!/usr/bin/env bash\nset -euo pipefail\nexit 9\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, name), []byte(contract), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(installedPath(home, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath(home, name), []byte("drifted\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := SyncInstalledScripts(repo)
	if len(res.Failed) != 0 || len(res.Refreshed) != 0 {
		t.Fatalf("a script declaring no install seam must be skipped entirely: refreshed=%v failed=%v",
			res.Refreshed, res.Failed)
	}
}

// TestSyncInstalledScriptsReportsFailureWithoutFailing: a broken install is a
// LOGGED wart, never a fatal error and never a rollback — the merge is done and
// good (the contract the rebuild step next to it holds).
func TestSyncInstalledScriptsReportsFailureWithoutFailing(t *testing.T) {
	home := sandboxHome(t)
	repo := t.TempDir()
	scriptsDir := filepath.Join(repo, scriptSyncRepoDirName)
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "broken.sh"
	installSeamScript(t, scriptsDir, name, "  echo \"install is broken\" >&2\n  exit 7\n")
	if err := os.MkdirAll(filepath.Dir(installedPath(home, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath(home, name), []byte("drifted\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := SyncInstalledScripts(repo)
	if len(res.Refreshed) != 0 {
		t.Fatalf("refreshed = %v, want none (the install failed)", res.Refreshed)
	}
	detail, ok := res.Failed[name]
	if !ok {
		t.Fatalf("the failed install must be reported: %+v", res.Failed)
	}
	if !strings.Contains(detail, "install is broken") {
		t.Errorf("the failure detail must carry the child's output for the operator: %q", detail)
	}
}

// TestSyncInstalledScriptsNoScriptsDirIsQuiet: a repo with no scripts directory
// (docs-only repo / foreign task field) is a silent no-op — and no scripts
// directory is created anywhere.
func TestSyncInstalledScriptsNoScriptsDirIsQuiet(t *testing.T) {
	sandboxHome(t)
	repo := t.TempDir()

	res := SyncInstalledScripts(repo)
	if res == nil {
		t.Fatal("SyncInstalledScripts must never return nil")
	}
	if len(res.Refreshed)+len(res.Current)+len(res.NotInstalled)+len(res.Failed) != 0 {
		t.Fatalf("a repo with no scripts dir must be a quiet no-op, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(repo, scriptSyncRepoDirName)); !os.IsNotExist(err) {
		t.Fatalf("no scripts directory may be created in the audited repo (stat err=%v)", err)
	}
}

// TestSyncInstalledScriptsHonoursItsOwnSeamValue pins the flag the pass drives
// from the constant (Rule 13: the value is referenced, never re-typed inline) —
// a drift between the constant and the bytes this pass sends would call the
// script with an unknown flag.
func TestSyncInstalledScriptsHonoursItsOwnSeamValue(t *testing.T) {
	if scriptSyncInstallFlag != "--install" {
		t.Fatalf("scriptSyncInstallFlag = %q — it IS the scripts' documented seam; "+
			"changing it is a change to every operator script's contract", scriptSyncInstallFlag)
	}
	if ScriptSyncDirRel != ".local/bin" {
		t.Fatalf("ScriptSyncDirRel = %q — it must match every --install seam's stable dir", ScriptSyncDirRel)
	}
}
