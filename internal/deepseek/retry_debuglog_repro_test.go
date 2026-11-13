package deepseek

// Repro test for the minor deepseek/retry.go bug filed by the 2026-08-30
// self-review (t-1788053264-3):
//
// `var debugLog = os.Getenv("GOPI_DEBUG") != ""` evaluated the env var at
// package-init time (BEFORE config.ApplyEnv runs in main) and treated any
// non-empty value — including the canonical "off" ApplyEnv force-writes —
// as ENABLED. Two failures: GOPI_DEBUG=off turned retry logging ON, and
// settings.json "debug": true (ApplyEnv writes GOPI_DEBUG=1 at runtime)
// never reached the package var, so retry logging stayed OFF.
//
// Fix contract: the flag is read LAZILY with truthy parsing identical to
// agent.boolEnvTrue ("1", "on", "yes", "true", "enabled"), which is provided
// by config.EnvBool — one shared implementation, per PROJECT.md Rule 2.

import (
	"os"
	"testing"

	"github.com/nick/gopi/internal/config"
)

func TestReproDebugLogTruthy(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want bool
	}{
		{"canonical off must be OFF", "off", false},
		{"false", "false", false},
		{"zero", "0", false},
		{"no", "no", false},
		{"OFF case-insensitive", "OFF", false},
		{"one", "1", true},
		{"true", "true", true},
		{"on", "on", true},
		{"yes", "yes", true},
		{"enabled", "enabled", true},
		{"padded on", "  on  ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GOPI_DEBUG", c.val)
			if got := debugLogEnabled(); got != c.want {
				t.Errorf("debugLogEnabled() with GOPI_DEBUG=%q = %v, want %v", c.val, got, c.want)
			}
		})
	}
}

// The unset state must read OFF (before the fix, unset was fine — but the
// lazy reader must not regress it either).
func TestReproDebugLogUnset(t *testing.T) {
	t.Setenv("GOPI_DEBUG", "") // register restore; then truly unset
	if err := os.Unsetenv("GOPI_DEBUG"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if debugLogEnabled() {
		t.Error("debugLogEnabled() with GOPI_DEBUG unset = true, want false")
	}
}

// Settings passthrough (acceptance #2): settings.json "debug": true reaches
// the process env via config.ApplyEnv(cfg, false) — which runs in main,
// AFTER any package init — and must then switch retry logging on. Before the
// fix the init-time snapshot was already frozen, so this channel never worked.
func TestReproDebugLogSettingsPassthrough(t *testing.T) {
	t.Setenv("GOPI_DEBUG", "")
	if err := os.Unsetenv("GOPI_DEBUG"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if debugLogEnabled() {
		t.Fatal("precondition: unset GOPI_DEBUG must read off")
	}
	// Exactly what main does with settings.json {"debug": true}.
	config.ApplyEnv(&config.Config{Debug: true}, false)
	if v := os.Getenv("GOPI_DEBUG"); v != "1" {
		t.Fatalf("ApplyEnv(settings debug=true) wrote GOPI_DEBUG=%q, want \"1\"", v)
	}
	if !debugLogEnabled() {
		t.Error("debugLogEnabled() after settings passthrough = false, want true")
	}
	// And the inverse: settings debug=false force-writes "off" (fromFlags
	// semantics) which must read OFF, not on.
	t.Setenv("GOPI_DEBUG", "off")
	if debugLogEnabled() {
		t.Error("debugLogEnabled() with GOPI_DEBUG=off = true, want false")
	}
}
