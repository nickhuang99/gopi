package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGopiDir_EnvOverridesHome — the core isolation guarantee: GOPI_HOME set
// means every gopi-data path resolves under $GOPI_HOME/.gopi, and the real
// HOME is never consulted (it may not even exist in a test sandbox).
func TestGopiDir_EnvOverridesHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOPI_HOME", root)
	t.Setenv("HOME", "/nonexistent-home-gopi-test")

	gd, err := GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}
	if want := filepath.Join(root, ".gopi"); gd != want {
		t.Errorf("GopiDir = %q, want %q", gd, want)
	}
}

// TestGopiDir_FallsBackToUserHome — no GOPI_HOME → classic ~/.gopi behavior.
func TestGopiDir_FallsBackToUserHome(t *testing.T) {
	t.Setenv("GOPI_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available in test env")
	}
	gd, err := GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}
	if want := filepath.Join(home, ".gopi"); gd != want {
		t.Errorf("GopiDir = %q, want %q", gd, want)
	}
}

// TestGopiDir_WhitespaceOnlyIsUnset — a whitespace-only GOPI_HOME counts as
// unset (a "" value must behave exactly like not setting it).
func TestGopiDir_WhitespaceOnlyIsUnset(t *testing.T) {
	t.Setenv("GOPI_HOME", "   ")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available in test env")
	}
	gd, err := GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}
	if want := filepath.Join(home, ".gopi"); gd != want {
		t.Errorf("GopiDir = %q, want the HOME fallback %q", gd, want)
	}
}

// TestGopiHome_GopiHomeReturnsTheRootItself — GopiHome exposes the GOPI_HOME
// root (not the .gopi child) for the few tilde/sandbox consumers that need
// HOME itself.
func TestGopiHome_ReturnsTheRootItself(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOPI_HOME", root)

	got, err := GopiHome()
	if err != nil {
		t.Fatalf("GopiHome: %v", err)
	}
	if got != root {
		t.Errorf("GopiHome = %q, want %q", got, root)
	}
}

// TestFlagOverrides_HomeFlag — --home PATH parses into GopiHomePath and
// ApplyEnv(fromFlags=true) injects it as GOPI_HOME so every existing
// GopiDir() reader picks it up unchanged.
func TestFlagOverrides_HomeFlag(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--home", "/tmp/gopi-sandbox"})
	if err != nil {
		t.Fatalf("FlagOverrides: %v", err)
	}
	if cfg.GopiHomePath != "/tmp/gopi-sandbox" {
		t.Errorf("GopiHomePath = %q, want /tmp/gopi-sandbox", cfg.GopiHomePath)
	}

	ApplyEnv(cfg, true)
	defer os.Unsetenv("GOPI_HOME")
	if got := os.Getenv("GOPI_HOME"); got != "/tmp/gopi-sandbox" {
		t.Errorf("GOPI_HOME after ApplyEnv = %q, want /tmp/gopi-sandbox", got)
	}
}

// TestFlagOverrides_HomeEqualsForm — --home=PATH (the = spelling) parses too.
func TestFlagOverrides_HomeEqualsForm(t *testing.T) {
	cfg, err := FlagOverrides([]string{"--home=/tmp/gopi-sandbox2"})
	if err != nil {
		t.Fatalf("FlagOverrides: %v", err)
	}
	if cfg.GopiHomePath != "/tmp/gopi-sandbox2" {
		t.Errorf("GopiHomePath = %q", cfg.GopiHomePath)
	}
}
