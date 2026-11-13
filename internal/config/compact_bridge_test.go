package config

import (
	"os"
	"testing"
)

// TestApplyEnv_CompactStringPropagates pins the settings→env bridge: a
// settings.json "compact":"off" must reach the process env as the canonical
// "off" the loop's GOPI_COMPACT reader disables on.
func TestApplyEnv_CompactStringPropagates(t *testing.T) {
	t.Setenv("GOPI_COMPACT", "")
	p := writeSettings(t, `{"compact":"off"}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("FromSettingsFile: %v", err)
	}
	ApplyEnv(cfg, false)
	if got := os.Getenv("GOPI_COMPACT"); got != "off" {
		t.Errorf("GOPI_COMPACT = %q, want off", got)
	}
}
