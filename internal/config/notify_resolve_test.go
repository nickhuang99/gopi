package config

// ResolveNotify unit tests (task-notify settings epic, 2026-09-02
// incident): the per-knob precedence env > settings.json "notify" section
// > zero-value default, plus the fail-soft degradation on a missing
// settings file. Every test isolates GOPI_SETTINGS at a sandbox file and
// unsets the five GOPI_TASK_NOTIFY_* vars so neither the ambient env nor
// the ambient live settings.json can leak in.

import (
	"os"
	"path/filepath"
	"testing"
)

// clearNotifyEnv truly UNSETS the five notify env vars (env is the volatile
// channel — the test must start from "no env" and set only what it wants).
// t.Setenv cannot unset (it only sets, making LookupEnv present), so the
// original values are captured and restored on cleanup.
func clearNotifyEnv(t *testing.T) {
	t.Helper()
	for _, env := range []string{
		NotifyWebhookEnv, NotifyCmdEnv, NotifyEventsEnv,
		NotifyPatternEnv, NotifyAsyncEnv,
	} {
		orig, wasSet := os.LookupEnv(env)
		if err := os.Unsetenv(env); err != nil {
			t.Fatalf("unset %s: %v", env, err)
		}
		t.Cleanup(func() {
			if wasSet {
				_ = os.Setenv(env, orig) //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset'
			} else {
				_ = os.Unsetenv(env) //stylegate:ignore clears a fixture variable; the presence check afterwards is the assertion
			}
		})
	}
}

// TestResolveNotifySettingsOnly: no env, a settings "notify" section with a
// cmd → the resolver returns the settings cmd (the durable layer — the
// 2026-09-02 scenario: a serve daemon spawned with NO notify env must still
// deliver to the settings-configured target).
func TestResolveNotifySettingsOnly(t *testing.T) {
	clearNotifyEnv(t)
	p := writeSettings(t, `{
		"notify": {"cmd": "cat >> /tmp/notify.log", "events": "claimed,stale,completed,failed", "async": true}
	}`)
	t.Setenv("GOPI_SETTINGS", p)

	got := ResolveNotify()
	if got.Cmd != "cat >> /tmp/notify.log" {
		t.Errorf("Cmd = %q, want the settings cmd", got.Cmd)
	}
	if got.Events != "claimed,stale,completed,failed" {
		t.Errorf("Events = %q, want the settings events", got.Events)
	}
	if !got.Async {
		t.Error("Async must come from settings")
	}
	if got.Webhook != "" || got.Pattern != "" {
		t.Errorf("unset settings knobs must stay empty, got webhook=%q pattern=%q", got.Webhook, got.Pattern)
	}
}

// TestResolveNotifyEnvWinsOverSettings: env set for a knob → the env value
// wins over the settings section for THAT knob; other knobs keep the
// settings value (per-knob merge).
func TestResolveNotifyEnvWinsOverSettings(t *testing.T) {
	clearNotifyEnv(t)
	p := writeSettings(t, `{
		"notify": {"cmd": "cat >> /tmp/settings.log", "events": "completed", "async": false}
	}`)
	t.Setenv("GOPI_SETTINGS", p)
	t.Setenv(NotifyCmdEnv, "cat >> /tmp/env.log")

	got := ResolveNotify()
	if got.Cmd != "cat >> /tmp/env.log" {
		t.Errorf("Cmd = %q, want the env value (env wins)", got.Cmd)
	}
	if got.Events != "completed" {
		t.Errorf("Events = %q, want the settings value (env unset → settings fills)", got.Events)
	}
	if got.Async {
		t.Error("Async must stay false from settings (env unset)")
	}
}

// TestResolveNotifyEnvEmptyStringOverrides: an env var SET to "" is an
// explicit override — it must blank the settings value for that knob (the
// operator's explicit choice wins over the durable file).
func TestResolveNotifyEnvEmptyStringOverrides(t *testing.T) {
	clearNotifyEnv(t)
	p := writeSettings(t, `{
		"notify": {"cmd": "cat >> /tmp/settings.log"}
	}`)
	t.Setenv("GOPI_SETTINGS", p)
	// cmd env present but empty (the hermetic pattern used across the
	// suite: `t.Setenv("GOPI_TASK_NOTIFY_WEBHOOK", "")` to veto an ambient
	// real webhook) — must disable the settings cmd, not fall through.
	t.Setenv(NotifyCmdEnv, "")

	got := ResolveNotify()
	if got.Cmd != "" {
		t.Errorf("Cmd = %q, want empty (env set to '' explicitly disables the settings cmd)", got.Cmd)
	}
}

// TestResolveNotifyNoConfigDisabled: no env, no settings notify section →
// zero-value config = notify disabled (backward compat: no delivery, no
// error).
func TestResolveNotifyNoConfigDisabled(t *testing.T) {
	clearNotifyEnv(t)
	p := writeSettings(t, `{"sync_peers":[]}`)
	t.Setenv("GOPI_SETTINGS", p)

	got := ResolveNotify()
	if got != (NotifySettings{}) {
		t.Errorf("no notify config must resolve to zero-value, got %+v", got)
	}
}

// TestResolveNotifyMissingSettingsFileFailsSoft: a missing settings file is
// NOT an error — the resolver degrades to the env/default view (a config
// slip must silently disable notify, never wedge the delivery path).
func TestResolveNotifyMissingSettingsFileFailsSoft(t *testing.T) {
	clearNotifyEnv(t)
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "nonexistent.json"))
	t.Setenv(NotifyCmdEnv, "cat >> /tmp/env.log") // env still wins over the missing file

	got := ResolveNotify()
	if got.Cmd != "cat >> /tmp/env.log" {
		t.Errorf("Cmd = %q, want the env value (missing settings file must fail soft)", got.Cmd)
	}
}

// TestEnvBoolValue pins the truthy env parsing used by ResolveNotify's async
// knob (1/true/yes/on/enabled enable; everything else false).
func TestEnvBoolValue(t *testing.T) {
	cases := map[string]bool{
		"1": true, "true": true, "yes": true, "on": true, "enabled": true,
		"TRUE": true, " On ": true,
		"": false, "0": false, "false": false, "off": false, "no": false,
		"banana": false,
	}
	for in, want := range cases {
		if got := envBoolValue(in); got != want {
			t.Errorf("envBoolValue(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestFromSettingsFileNotifySection: the notify section parses into the
// Config.Notify struct (schema check — FromSettingsFile round-trip).
func TestFromSettingsFileNotifySection(t *testing.T) {
	p := writeSettings(t, `{
		"notify": {"webhook": "https://example.com/hook", "cmd": "cat > /tmp/n.log",
		           "events": "all", "pattern": "feat*", "async": true}
	}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if cfg.Notify.Webhook != "https://example.com/hook" {
		t.Errorf("Webhook = %q", cfg.Notify.Webhook)
	}
	if cfg.Notify.Cmd != "cat > /tmp/n.log" {
		t.Errorf("Cmd = %q", cfg.Notify.Cmd)
	}
	if cfg.Notify.Events != "all" || cfg.Notify.Pattern != "feat*" || !cfg.Notify.Async {
		t.Errorf("Notify = %+v, want events=all pattern=feat* async=true", cfg.Notify)
	}
}

// silence unused-import lint if os is ever dropped from this file's use.
var _ = os.Getenv
