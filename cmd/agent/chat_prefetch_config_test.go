package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// The three-channel tests drive the REAL resolution — config.MemoryPrefetchEnabled
// and the REAL hoist config.BootstrapEnvFromArgs — never a copy of their logic:
// a test that re-implements the precedence asserts nothing about the binary.
//
// The tests are in cmd/agent (not internal/config) on purpose: the hoist is the
// half that decides whether `gopi --memory-prefetch chat` reaches the verb, and
// it exists in this binary's startup path.

// prefetchSettings pins GOPI_SETTINGS at a file carrying the given JSON.
// t.Setenv, never os.Setenv (Rule 17): the value is restored at the test's end,
// however the test ends, and it cannot leak into a sibling test in this binary.
func prefetchSettings(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
}

// TestMemoryPrefetchThreeChannelsResolveToOneSwitch is the acceptance's "three
// config channels each tested and resolving to the same switch": the flag, the
// env var and the settings key are asserted to produce the SAME value through
// the ONE reader, and the OFF default is asserted with all three silent.
func TestMemoryPrefetchThreeChannelsResolveToOneSwitch(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		env      string
		argv     []string
		want     bool
	}{
		// THE DEFAULT FLIP (2026-10-04). With the settings file silent and the
		// env unset, the knob is ON: that is the whole deliverable, and it is the
		// case every other row here is a deviation from.
		{name: "nothing declared → the built-in default (ON)", settings: `{}`, want: true},
		{name: "settings only (on)", settings: `{"memory_prefetch": true}`, want: true},
		// The OFF path that must keep working: a DECLARED false is honoured, and
		// it is distinguishable from silence only because the field is a pointer.
		{name: "settings only (off)", settings: `{"memory_prefetch": false}`, want: false},
		{name: "env only", settings: `{}`, env: "1", want: true},
		{
			// env outranks settings in BOTH directions — an operator who exports
			// the variable has said what he wants for THIS run — and here the
			// override is what TURNS IT OFF under a default-ON knob.
			name: "env off beats the settings declaration (on)", settings: `{"memory_prefetch": true}`, env: "off", want: false,
		},
		{name: "flag only", settings: `{}`, argv: []string{"--memory-prefetch"}, want: true},
		{
			name:     "flag off beats env on",
			settings: `{}`,
			env:      "1",
			argv:     []string{"--memory-prefetch=off"},
			want:     false,
		},
		{
			name:     "flag on beats settings off",
			settings: `{"memory_prefetch": false}`,
			argv:     []string{"--memory-prefetch"},
			want:     true,
		},
		{
			name:     "flag off beats the built-in default",
			settings: `{}`,
			argv:     []string{"--memory-prefetch=off"},
			want:     false,
		},
		{
			// Rule 15: an unusable value cannot answer the question, so it is
			// reported and the NEXT channel decides. "chat" is the pathological
			// case this knob's hoist must never produce.
			name:     "unusable env value falls through to settings",
			settings: `{"memory_prefetch": true}`,
			env:      "chat",
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefetchSettings(t, tc.settings)
			// "" is the tools layer's "unset" for an env pin (ledgerpin.go's
			// reason: a child reads the variable at exec time and an empty value
			// is unset there) — so one call covers both "nobody exported it" and
			// "it was exported empty", and t.Setenv restores either.
			t.Setenv(config.MemoryPrefetchEnv, tc.env)
			if len(tc.argv) > 0 {
				// THE REAL HOIST, and the reason this test lives in this package:
				// the flag's power to reach `gopi chat` comes from this call.
				config.BootstrapEnvFromArgs(tc.argv)
			}
			if got := config.MemoryPrefetchEnabled(); got != tc.want {
				t.Errorf("MemoryPrefetchEnabled() = %v, want %v (settings=%s env=%q argv=%v)",
					got, tc.want, tc.settings, tc.env, tc.argv)
			}
		})
	}
}

// TestMemoryPrefetchHoistCarriesTheFlagToASubcommand pins the flag-hoisting
// property the deliverable names: `gopi --memory-prefetch chat` must reach the
// subcommand, so the flag's value has to be in the environment BEFORE the
// subcommand chain splits — and the verb itself must survive the hoist.
func TestMemoryPrefetchHoistCarriesTheFlagToASubcommand(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, "")

	// The VALUE-LESS form, immediately in front of the verb: the hoist must take
	// the flag and leave `chat` in argv for the subcommand chain to dispatch on.
	rest := config.BootstrapEnvFromArgs([]string{"--memory-prefetch", "chat"})
	if got := os.Getenv(config.MemoryPrefetchEnv); !config.IsTrueValue(got) {
		t.Errorf("the flag did not reach the environment: %s=%q", config.MemoryPrefetchEnv, got)
	}
	if len(rest) != 1 || rest[0] != "chat" {
		t.Errorf("the verb did not survive the hoist: rest=%v (want [chat]) — a hoist that eats its verb gives the operator a bare run", rest)
	}

	// The EQ form carries a value and is hoisted wherever it appears. The
	// knob's env is cleared THROUGH the test's own pin (t.Setenv again, "" =
	// unset for this layer) rather than an ambient os.Unsetenv (Rule 17).
	t.Setenv(config.MemoryPrefetchEnv, "")
	rest = config.BootstrapEnvFromArgs([]string{"--memory-prefetch=off", "chat"})
	if got := os.Getenv(config.MemoryPrefetchEnv); got != "off" {
		t.Errorf("--memory-prefetch=off did not hoist: %s=%q", config.MemoryPrefetchEnv, got)
	}
	if len(rest) != 1 || rest[0] != "chat" {
		t.Errorf("rest=%v, want [chat]", rest)
	}
}

// TestMemoryPrefetchFlagIsInTheCatalogAndHelp is the discoverability half: the
// flag must be a REAL flag on the parser (or the hoist above would be hoisting
// something the parser refuses) and it must be documented where the operator
// looks.
func TestMemoryPrefetchFlagIsInTheCatalogAndHelp(t *testing.T) {
	var found bool
	for _, e := range config.EnvVars {
		if e.Env == config.MemoryPrefetchEnv {
			found = true
			if e.Key != config.MemoryPrefetchSettingsKey || e.CLIFlag != config.MemoryPrefetchFlagName {
				t.Errorf("catalog row disagrees with the constants: %+v", e)
			}
			if !strings.Contains(e.Help, config.MemoryPrefetchDefaultClaim) {
				t.Errorf("catalog help does not state the default (%q): %q", config.MemoryPrefetchDefaultClaim, e.Help)
			}
		}
	}
	if !found {
		t.Fatalf("%s is not in config.EnvVars — the knob would be reachable but undocumented (and `gopi --help-env` would not show it)", config.MemoryPrefetchEnv)
	}
	cfg, err := config.FlagOverrides([]string{"--memory-prefetch=true", "--prompt", "x"})
	if err != nil {
		t.Fatalf("the parser refused --memory-prefetch: %v", err)
	}
	if cfg.MemoryPrefetch == nil {
		t.Fatalf("--memory-prefetch=true left Config.MemoryPrefetch nil — the declaration channel cannot then outrank silence")
	}
	if !*cfg.MemoryPrefetch {
		t.Errorf("--memory-prefetch=true did not set Config.MemoryPrefetch")
	}
	// The OFF direction through the SAME parser: the flag must be able to say
	// "off" as well as "on", or a default-ON knob would have no CLI off switch.
	cfgOff, err := config.FlagOverrides([]string{"--memory-prefetch=off", "--prompt", "x"})
	if err != nil {
		t.Fatalf("the parser refused --memory-prefetch=off: %v", err)
	}
	if cfgOff.MemoryPrefetch == nil || *cfgOff.MemoryPrefetch {
		t.Errorf("--memory-prefetch=off did not declare OFF: %v", cfgOff.MemoryPrefetch)
	}
}

// TestPrefetchRecordIsOneJSONLinePerEvent pins the observable's shape: the
// record the executor writes must be readable back as JSON with the fields the
// deliverable names (query / hits / ms / outcome / adopted).
func TestPrefetchRecordIsOneJSONLinePerEvent(t *testing.T) {
	dir := t.TempDir()
	rec := prefetchRecord{Query: "prefetch ledger", Hits: prefetchHitsLabel(2), Ms: prefetchMsLabel(12), Outcome: prefetchOutcomeHits, Adopted: prefetchRecordAdoptedNo}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, config.MemoryPrefetchRecordName)
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &back); err != nil {
		t.Fatalf("the record is not one JSON object: %v", err)
	}
	for _, k := range []string{"query", "hits", "ms", "outcome", "adopted"} {
		if _, ok := back[k]; !ok {
			t.Errorf("the record is missing %q: %s", k, string(raw))
		}
	}
}
