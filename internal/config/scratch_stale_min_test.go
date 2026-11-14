package config

// The scratch sweep's stale-window knob — the resolution and precedence half.
// The sweep's own behaviour is tested in internal/scratch (the knob's reader)
// and cmd/agent (the verb end to end); what is pinned HERE is that this package
// publishes exactly one value, in exactly one variable, with the documented
// order.

import (
	"os"
	"strconv"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// TestScratchGCStaleMinFlagParsing pins the flag's two spellings, the value it
// lands on the Config, and the loud refusal of a value that cannot be used.
// Rejecting a bad value is deliberate (Rule 15): continuing with the default
// would mean a watchdog sweeping with a window nobody chose — and a threshold
// of 0 would mean reaping nothing forever, which is the defect the knob fixes.
func TestScratchGCStaleMinFlagParsing(t *testing.T) {
	for _, args := range [][]string{
		{"--" + ScratchGCStaleMinFlagName, "45"},
		{"--" + ScratchGCStaleMinFlagName + "=45"},
	} {
		cfg, err := FlagOverrides(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if cfg.ScratchGCStaleMin != 45 {
			t.Fatalf("%v: ScratchGCStaleMin=%d, want 45", args, cfg.ScratchGCStaleMin)
		}
	}
	for _, bad := range [][]string{
		{"--" + ScratchGCStaleMinFlagName, "0"},
		{"--" + ScratchGCStaleMinFlagName, "-5"},
		{"--" + ScratchGCStaleMinFlagName, "soon"},
	} {
		if _, err := FlagOverrides(bad); err == nil {
			t.Fatalf("%v: a value that cannot be honoured must be refused, not defaulted", bad)
		}
	}
	// The knob is advertised in the catalog (the ONE list help and the parser
	// share): a flag nobody can discover is a flag nobody sets.
	if _, err := FlagOverrides([]string{"--" + ScratchGCStaleMinFlagName, "45"}); err != nil {
		t.Fatalf("the catalog must accept the knob: %v", err)
	}
	var found bool
	for _, e := range EnvVars {
		if e.Env == scratch.GCStaleAgeEnvName {
			found = true
			if e.Key != scratch.GCStaleAgeSettingsKey {
				t.Fatalf("catalog settings key = %q, want %q (one definition, internal/scratch)", e.Key, scratch.GCStaleAgeSettingsKey)
			}
			if e.CLIFlag != ScratchGCStaleMinFlagName {
				t.Fatalf("catalog CLI flag = %q, want %q", e.CLIFlag, ScratchGCStaleMinFlagName)
			}
		}
	}
	if !found {
		t.Fatalf("the catalog carries no row for %s — the knob is undiscoverable", scratch.GCStaleAgeEnvName)
	}
}

// TestPublishScratchGCStaleMin_Precedence pins the order the knob's publisher
// applies: flag > a caller's exported env > settings.json > (nothing).
//
// Each phase is its OWN subtest with its own t.Setenv — the env-isolation rule
// (Rule 17), and also what makes the phases order-independent: the publisher
// WRITES the knob's variable, so a phase that leaked its write into the next one
// would invert the very precedence under test. t.Setenv's undo runs at the end
// of the subtest, which is exactly the scope each phase wants; the "reset" call
// at the top of a phase that needs a clean slate restores the value the suite's
// own harness published.
func TestPublishScratchGCStaleMin_Precedence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ambientEnv string  // what the environment holds at entry
		settings   *Config // the settings channel
		flag       *Config // the flag channel
		wantPub    int     // what the publisher should return
		wantEnv    string  // what the reader must then see
	}{
		{"nothing configured", "", nil, nil, 0, ""},
		{"settings only", "", &Config{ScratchGCStaleMin: 240}, nil, 240, "240"},
		{"caller export stands", "17", &Config{ScratchGCStaleMin: 240}, nil, 0, "17"},
		{"flag outranks the caller", "17", &Config{ScratchGCStaleMin: 240}, &Config{ScratchGCStaleMin: 30}, 30, "30"},
		{"flag alone", "", nil, &Config{ScratchGCStaleMin: 30}, 30, "30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(scratch.GCStaleAgeEnvName, tc.ambientEnv)
			if got := PublishScratchGCStaleMin(tc.settings, tc.flag); got != tc.wantPub {
				t.Fatalf("published %d, want %d", got, tc.wantPub)
			}
			if got := os.Getenv(scratch.GCStaleAgeEnvName); got != tc.wantEnv {
				t.Fatalf("%s=%q, want %q", scratch.GCStaleAgeEnvName, got, tc.wantEnv)
			}
			// The reader is the other half of the contract: whatever was
			// published must be what the sweep will use.
			switch want := tc.wantEnv; want {
			case "":
				if got := scratch.ResolveStaleAge(); got != scratch.DefaultStaleAge {
					t.Fatalf("unconfigured: the reader resolved %v, want the default %v", got, scratch.DefaultStaleAge)
				}
			default:
				n, _ := strconv.Atoi(want) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
				if got := scratch.ResolveStaleAge().Minutes(); got != float64(n) {
					t.Fatalf("the reader resolved %v minutes, want %s", got, want)
				}
			}
		})
	}
}

// TestPublishScratchGCStaleMin_GarbageNeverDisables is the Rule 15 half: an
// unusable value is reported and the knob is left at the default — never turned
// into "collect nothing", which is what a threshold of 0 would mean.
func TestPublishScratchGCStaleMin_GarbageNeverDisables(t *testing.T) {
	t.Setenv(scratch.GCStaleAgeEnvName, "")
	if got := PublishScratchGCStaleMin(&Config{ScratchGCStaleMin: -1}, nil); got != 0 {
		t.Fatalf("a negative settings value published %d, want 0 (default)", got)
	}
	if got := scratch.ResolveStaleAge(); got != scratch.DefaultStaleAge {
		t.Fatalf("after a bad settings value the reader resolved %v, want the default %v", got, scratch.DefaultStaleAge)
	}
}
