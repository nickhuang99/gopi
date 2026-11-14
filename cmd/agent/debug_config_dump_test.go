package main

// debug_config_dump_test.go — what the switch must do at the ONE seam that
// writes it, observed on the writer itself.
//
// The two properties a reader depends on are opposite: ARMED it must print the
// SAME block `/config` prints (that equality is what makes the log usable as
// evidence — a second, smaller renderer would answer a different question), and
// DISARMED it must print NOTHING at all (an opt-in record that leaves a stray
// divider in every log is not opt-in). Both are asserted here, and the title is
// read from the renderer's own constant (Rules 1b/13) so a rename moves the
// assertion with the page instead of pinning a spelling this test invented.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestDebugConfigDumpWritesTheConfigBlockWhenArmed pins the ON path: the block
// is the renderer's own title, it is fenced by the seam's divider, and it is
// byte-identical to what the REPL door would assemble from the same builder.
func TestDebugConfigDumpWritesTheConfigBlockWhenArmed(t *testing.T) {
	t.Setenv(config.DebugConfigDumpEnv, "1")

	var buf bytes.Buffer
	writeDebugConfigDump(&buf, nil)
	got := buf.String()

	if !strings.Contains(got, runtimeConfigTitle) {
		t.Fatalf("the dump does not carry the renderer's title %q:\n%s", runtimeConfigTitle, got)
	}
	want := strings.Join(renderRuntimeConfigSnapshot(
		buildRuntimeConfigSnapshot(nil, liveRuntimeConfigDeps(runtimeConfigKnownKeys))), "\n") + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("the dump is not the /config block byte for byte.\nwant it to contain:\n%s\ngot:\n%s", want, got)
	}
	if strings.Count(got, debugConfigDumpSep+"\n") != 2 {
		t.Errorf("the block must be fenced by exactly two %q lines, got:\n%s", debugConfigDumpSep, got)
	}
	// The fence must not BE the title: a divider that repeated it would make
	// `grep -A40 "configuration in force"` land on the divider instead of the
	// table.
	if strings.Contains(debugConfigDumpSep, runtimeConfigTitle) {
		t.Errorf("the divider %q repeats the renderer's title — the grep anchor would match it", debugConfigDumpSep)
	}
}

// TestDebugConfigDumpIsSilentWhenDisarmed is the DEFAULT path, and it is the
// one that matters most: every run on this host takes it. Not a divider, not a
// blank line — zero bytes.
func TestDebugConfigDumpIsSilentWhenDisarmed(t *testing.T) {
	for _, value := range []string{"", "0", "off", "false"} {
		t.Setenv(config.DebugConfigDumpEnv, value)
		var buf bytes.Buffer
		writeDebugConfigDump(&buf, nil)
		if buf.Len() != 0 {
			t.Errorf("%s=%q wrote %d bytes into the run log, want nothing:\n%s",
				config.DebugConfigDumpEnv, value, buf.Len(), buf.String())
		}
	}
}

// TestDebugConfigDumpEnvIsCatalogued keeps the switch DISCOVERABLE: the catalog
// is what `gopi --help-env` renders from, so a knob missing there is a knob an
// operator concludes does not exist (the help_env_catalog gate's own rule,
// asserted here from the knob's side so the failure names the switch).
func TestDebugConfigDumpEnvIsCatalogued(t *testing.T) {
	for _, e := range config.EnvVars {
		if e.Env == config.DebugConfigDumpEnv {
			if strings.TrimSpace(e.Help) == "" {
				t.Errorf("the %s catalog row carries no help prose", e.Env)
			}
			return
		}
	}
	t.Fatalf("%s has no row in config.EnvVars — an uncatalogued knob is invisible to `gopi --help-env`",
		config.DebugConfigDumpEnv)
}
