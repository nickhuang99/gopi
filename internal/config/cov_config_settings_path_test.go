package config

// cov_config_settings_path_test.go — function-level coverage slice for
// config.ResolveSettingsPath (branch feat/cov-config-hp). Tests only.
//
// Contract under test (read from config.go:555): GOPI_SETTINGS (SettingsEnv,
// whitespace-trimmed) wins; otherwise the DEFAULT settings.json under the
// gopi data root; "" when no home directory exists. The function is a pure
// TEXTUAL resolution — it performs NO symlink canonicalization (no
// filepath.EvalSymlinks / os.Symlink handling exists anywhere in
// internal/config), so this slice pins the real contract: the override is
// returned verbatim, and it IS the file the loader then reads even when a
// stale default file exists next to it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCovCfgResolveSettingsPath_EnvOverrideWinsVerbatim: a non-empty
// GOPI_SETTINGS is returned as-is (only outer whitespace trimmed), and the
// gopi data root is not consulted at all.
func TestCovCfgResolveSettingsPath_EnvOverrideWinsVerbatim(t *testing.T) {
	home := t.TempDir()
	override := filepath.Join(t.TempDir(), "elsewhere", "custom.json")
	t.Setenv(GopiDirEnv, home) // a DIFFERENT root — must be ignored
	t.Setenv(SettingsEnv, override)

	if got := ResolveSettingsPath(); got != override {
		t.Errorf("ResolveSettingsPath() = %q, want the GOPI_SETTINGS override %q", got, override)
	}
	if strings.HasPrefix(ResolveSettingsPath(), home) {
		t.Errorf("ResolveSettingsPath() = %q is under the gopi root %q — the override must win outright", ResolveSettingsPath(), home)
	}
}

// TestCovCfgResolveSettingsPath_OverrideIsTrimmed: leading/trailing spaces
// (a shell-exported value) are trimmed before use.
func TestCovCfgResolveSettingsPath_OverrideIsTrimmed(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv(GopiDirEnv, t.TempDir())
	t.Setenv(SettingsEnv, "  "+override+"\t")

	if got := ResolveSettingsPath(); got != override {
		t.Errorf("ResolveSettingsPath() = %q, want the trimmed override %q", got, override)
	}
}

// TestCovCfgResolveSettingsPath_WhitespaceOnlyFallsBack: a whitespace-only
// override is NOT an override (TrimSpace == ""), so the default path is used.
func TestCovCfgResolveSettingsPath_WhitespaceOnlyFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SettingsEnv, "   ")

	got := ResolveSettingsPath()
	if got != DefaultSettingsPath() {
		t.Errorf("ResolveSettingsPath() = %q, want the default %q for a whitespace-only override", got, DefaultSettingsPath())
	}
	if filepath.Base(got) != "settings.json" {
		t.Errorf("default settings path %q does not end in settings.json", got)
	}
	if !strings.HasPrefix(got, home) {
		t.Errorf("default settings path %q is not under the gopi root %q", got, home)
	}
}

// TestCovCfgResolveSettingsPath_DefaultFollowsGopiRoot: with no override the
// result tracks GOPI_HOME (GopiDirEnv) — set GopiDirEnv to two different
// sandboxes and the resolved default follows each one.
func TestCovCfgResolveSettingsPath_DefaultFollowsGopiRoot(t *testing.T) {
	t.Setenv(SettingsEnv, "")
	for _, home := range []string{t.TempDir(), t.TempDir()} {
		t.Setenv(GopiDirEnv, home)
		got := ResolveSettingsPath()
		want := DefaultSettingsPath()
		if got != want {
			t.Errorf("ResolveSettingsPath() = %q, want %q (= DefaultSettingsPath())", got, want)
		}
		if !strings.HasPrefix(got, home) {
			t.Errorf("ResolveSettingsPath() = %q does not live under GOPI_HOME=%q", got, home)
		}
	}
}

// TestCovCfgResolveSettingsPath_EmptyOverrideUsesDefault: an explicitly empty
// GOPI_SETTINGS is the "unset" case, not a "" path.
func TestCovCfgResolveSettingsPath_EmptyOverrideUsesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SettingsEnv, "")

	if got := ResolveSettingsPath(); got == "" {
		t.Fatalf("ResolveSettingsPath() = \"\" with a GOPI_HOME sandbox in place, want a real path")
	}
	if got := ResolveSettingsPath(); got != DefaultSettingsPath() {
		t.Errorf("ResolveSettingsPath() = %q, want %q", got, DefaultSettingsPath())
	}
}

// TestCovCfgResolveSettingsPath_StaleDefaultIsNotConsulted is the semantics
// that matters operationally: a STALE default settings.json (an old role /
// provider left in $GOPI_HOME/.gopi/settings.json) must not leak into a run
// launched with an explicit override. The assertion is end-to-end — the file
// ResolveSettingsPath names is the file FromSettingsFile actually reads.
func TestCovCfgResolveSettingsPath_StaleDefaultIsNotConsulted(t *testing.T) {
	home := t.TempDir()
	staleDir := filepath.Join(home, ".gopi")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(staleDir, filepath.Base(DefaultSettingsPath()))
	if err := os.WriteFile(stale, []byte(`{"role":"server","sync_to":"stale-node"}`), 0o600); err != nil {
		t.Fatalf("write stale default: %v", err)
	}
	freshDir := t.TempDir()
	fresh := filepath.Join(freshDir, "settings.json")
	if err := os.WriteFile(fresh, []byte(`{"role":"client"}`), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SettingsEnv, fresh)

	p := ResolveSettingsPath()
	if p != fresh {
		t.Fatalf("ResolveSettingsPath() = %q, want the override %q", p, fresh)
	}
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("FromSettingsFile(%q): %v", p, err)
	}
	if cfg.Role != NodeRoleClient {
		t.Errorf("role read back = %q, want %q — the STALE default's role leaked in", cfg.Role, NodeRoleClient)
	}
	if cfg.SyncTo != "" {
		t.Errorf("sync_to read back = %q, want empty — the STALE default's sync_to leaked in", cfg.SyncTo)
	}
}

// TestCovCfgResolveSettingsPath_IsTheFileActuallyRead closes the loop from the
// other side: a MALFORMED file at the resolved path makes the loader fail and
// the error names that exact path (proof the resolver's answer is really the
// one handed to the reader).
func TestCovCfgResolveSettingsPath_IsTheFileActuallyRead(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(GopiDirEnv, t.TempDir())
	t.Setenv(SettingsEnv, bad)

	p := ResolveSettingsPath()
	if p != bad {
		t.Fatalf("ResolveSettingsPath() = %q, want %q", p, bad)
	}
	if _, err := FromSettingsFile(p); err == nil {
		t.Fatal("malformed settings decoded without error")
	} else if !strings.Contains(err.Error(), bad) {
		t.Errorf("error %q does not name the resolved path %q", err, bad)
	}
}

// TestCovCfgResolveSettingsPath_NoHomeDirectoryIsEmpty pins the documented
// degenerate branch: with BOTH GOPI_HOME and HOME unset, GopiDir errors, so
// the default resolution is "" — and the consequence is recorded honestly: a
// "" path feeds FromSettingsFile, which returns an EMPTY config with NO error,
// i.e. every settings-driven knob (role, compute_nodes, notify) silently
// degrades to its default. Reachable in a sandbox/container with no HOME.
func TestCovCfgResolveSettingsPath_NoHomeDirectoryIsEmpty(t *testing.T) {
	t.Setenv(SettingsEnv, "")
	t.Setenv(GopiDirEnv, "")
	t.Setenv("HOME", "")

	got := ResolveSettingsPath()
	if got != "" {
		t.Fatalf("ResolveSettingsPath() = %q, want \"\" when no home directory exists", got)
	}
	if d := DefaultSettingsPath(); d != "" {
		t.Fatalf("DefaultSettingsPath() = %q, want \"\"", d)
	}
	cfg, err := FromSettingsFile(got)
	if err != nil {
		t.Fatalf("FromSettingsFile(\"\") error: %v — the documented behavior is a soft empty fallback", err)
	}
	if cfg.Role != "" || cfg.SyncTo != "" || cfg.SettingsPath != "" {
		t.Errorf("FromSettingsFile(\"\") = %+v, want a zero Config (silent default fallback)", cfg)
	}
}

// TestCovCfgResolveSettingsPath_CreatesNothing: resolution is pure text — it
// must not mkdir the gopi root nor create a settings file as a side effect.
func TestCovCfgResolveSettingsPath_CreatesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SettingsEnv, "")

	p := ResolveSettingsPath()
	if p == "" {
		t.Fatal("empty resolution under a sandbox root")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("stat %q = %v, want IsNotExist — resolution must not create the file", p, err)
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Errorf("stat %q = %v, want IsNotExist — resolution must not create the directory", filepath.Dir(p), err)
	}
}
