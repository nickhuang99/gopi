package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestBootstrapEnvFromArgs_SettingsFlag: a leading `--settings PATH` (and the
// equals-form) must reach the GOPI_SETTINGS env — the subcommand-side config
// hoist that lets `gopi --settings X worker T` run its child under X
// (run-cmd-settings epic).
func TestBootstrapEnvFromArgs_SettingsFlag(t *testing.T) {
	path := "/tmp/custom-settings.json"
	t.Setenv("GOPI_SETTINGS", "/some/unrelated/preset")
	BootstrapEnvFromArgs([]string{"--settings", path, "task", "run", "t-1"})
	if got := os.Getenv("GOPI_SETTINGS"); got != path {
		t.Fatalf("GOPI_SETTINGS = %q, want %q", got, path)
	}
}

func TestBootstrapEnvFromArgs_SettingsFlagEqualsForm(t *testing.T) {
	path := "/etc/gopi/settings.json"
	BootstrapEnvFromArgs([]string{"--settings=" + path, "task", "run", "t-1"})
	if got := os.Getenv("GOPI_SETTINGS"); got != path {
		t.Fatalf("GOPI_SETTINGS = %q, want %q", got, path)
	}
}

// TestBootstrapEnvFromArgs_DoesNotClobberAbsentFlag: with no --settings flag,
// a pre-exported GOPI_SETTINGS must be left alone (never overwritten).
func TestBootstrapEnvFromArgs_DoesNotClobberAbsentFlag(t *testing.T) {
	t.Setenv("GOPI_SETTINGS", "/preset/settings.json")
	BootstrapEnvFromArgs([]string{"task", "run", "t-1"})
	if got := os.Getenv("GOPI_SETTINGS"); got != "/preset/settings.json" {
		t.Fatalf("GOPI_SETTINGS clobbered to %q despite absent --settings flag", got)
	}
}

// TestBootstrapEnvFromArgs_IgnoresSubcommandFlags: only --settings/--home are
// mapped; a foreign --model flag's value must never be consumed as a config
// path.
func TestBootstrapEnvFromArgs_IgnoresSubcommandFlags(t *testing.T) {
	prevHome, hadHome := os.LookupEnv("GOPI_HOME")
	prevSet, hadSet := os.LookupEnv("GOPI_SETTINGS")
	BootstrapEnvFromArgs([]string{"--model", "deepseek-v4", "task", "run", "t-1"})
	if got := os.Getenv("GOPI_HOME"); hadHome != (func() bool { _, s := os.LookupEnv("GOPI_HOME"); return s }()) || (hadHome && got != prevHome) {
		t.Fatalf("GOPI_HOME changed by a foreign --model arg: had=%v prev=%q now=%q", hadHome, prevHome, got)
	}
	if got := os.Getenv("GOPI_SETTINGS"); hadSet != (func() bool { _, s := os.LookupEnv("GOPI_SETTINGS"); return s }()) || (hadSet && got != prevSet) {
		t.Fatalf("GOPI_SETTINGS changed by a foreign --model arg: had=%v prev=%q now=%q", hadSet, prevSet, got)
	}
}

// TestBootstrapEnvFromArgs_RemainderNoFlags: with no config flags the argv is
// returned untouched (identity — no pointless copy, dispatch semantics
// unchanged for the plain `gopi task …` path).
func TestBootstrapEnvFromArgs_RemainderNoFlags(t *testing.T) {
	in := []string{"task", "submit", "a b c", "--payload", "x y"}
	if got := BootstrapEnvFromArgs(in); !reflect.DeepEqual(got, in) {
		t.Fatalf("remainder = %#v, want the input unchanged %#v", got, in)
	}
}

// TestBootstrapEnvFromArgs_RemainderStripsFlagPair: the dispatch-remainder
// contract — a leading `--settings PATH` pair is consumed from the RETURN
// value (the env hoist is the side effect), so the caller can dispatch
// subcommands on the stripped remainder.
func TestBootstrapEnvFromArgs_RemainderStripsFlagPair(t *testing.T) {
	in := []string{"--settings", "/tmp/s.json", "task", "run", "t-1"}
	if got := BootstrapEnvFromArgs(in); !reflect.DeepEqual(got, []string{"task", "run", "t-1"}) {
		t.Fatalf("remainder = %#v, want [task run t-1]", got)
	}
	// Equals-form too.
	if got := BootstrapEnvFromArgs([]string{"--settings=/tmp/s.json", "--home", "/tmp/h", "memory", "list"}); !reflect.DeepEqual(got, []string{"memory", "list"}) {
		t.Fatalf("equals+home remainder = %#v, want [memory list]", got)
	}
}

// TestBootstrapEnvFromArgs_RemainderPreservesQuoting: the positional-param
// regression — the reported bug destroyed a quoted payload by word-splitting
// it. The stripped remainder must keep each argv element VERBATIM (the
// shell already did the quoting; gopi must never re-split).
func TestBootstrapEnvFromArgs_RemainderPreservesQuoting(t *testing.T) {
	got := BootstrapEnvFromArgs([]string{"--settings", "/tmp/s.json", "task", "submit", "let us test bug fixing", "--payload", "just want to verify", "--source", "human"})
	want := []string{"task", "submit", "let us test bug fixing", "--payload", "just want to verify", "--source", "human"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remainder = %#v\nwant       = %#v\n(quoted argv elements must survive verbatim)", got, want)
	}
}

// TestBootstrapEnvFromArgs_RemainderMidArgs: the flags may appear anywhere
// before the dispatch verb (isTasksSubcommand's ssh-passthrough retry scans
// for a "task" token deeper in argv); each recognized pair is consumed
// wherever it sits.
func TestBootstrapEnvFromArgs_RemainderMidArgs(t *testing.T) {
	got := BootstrapEnvFromArgs([]string{"ssh", "-o", "BatchMode=yes", "--settings", "/tmp/s.json", "bnode", "task", "sync"})
	want := []string{"ssh", "-o", "BatchMode=yes", "bnode", "task", "sync"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remainder = %#v, want %#v", got, want)
	}
}

// TestLaunchSettingsArg_EnvDefaultSuppressed: GOPI_SETTINGS pointing at the
// default path must NOT be echoed (the /proc-cleanliness rule).
func TestLaunchSettingsArg_EnvDefaultSuppressed(t *testing.T) {
	def := DefaultSettingsPath()
	if def == "" {
		t.Skip("no default settings path resolvable")
	}
	t.Setenv("GOPI_SETTINGS", def)
	if got := LaunchSettingsArg(); got != "" {
		t.Fatalf("LaunchSettingsArg() = %q, want \"\" for env==default", got)
	}
}

// TestLaunchSettingsArg_EnvNonDefault: a non-default GOPI_SETTINGS env IS the
// operator's explicit configure → must surface for re-runnability.
func TestLaunchSettingsArg_EnvNonDefault(t *testing.T) {
	if DefaultSettingsPath() == "" {
		t.Skip("no default settings path")
	}
	nonDefault := filepath.Join(t.TempDir(), "other.json")
	t.Setenv("GOPI_SETTINGS", nonDefault)
	if got := LaunchSettingsArg(); got != "--settings "+nonDefault {
		t.Fatalf("LaunchSettingsArg() = %q, want %q", got, "--settings "+nonDefault)
	}
}

// TestShellQuote: spaced / metachar paths round-trip; plain tokens verbatim.
func TestShellQuote(t *testing.T) {
	if got := shellQuote("/plain/path.json"); got != "/plain/path.json" {
		t.Errorf("plain = %q", got)
	}
	if got := shellQuote("/dir with space/s.json"); got != "'/dir with space/s.json'" {
		t.Errorf("spaced = %q", got)
	}
	if got := shellQuote("/it's/s.json"); got != `'/it'\''s/s.json'` {
		t.Errorf("apostrophe = %q", got)
	}
	if got := shellQuote(""); got != "''" {
		t.Errorf("empty = %q", got)
	}
}
