package config

// screenstream_test.go — the three states, the precedence rungs, and the two
// surfaces' defaults, pinned so a later edit cannot collapse "off" into "nobody
// said anything" (the defect this parser exists to make impossible).

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

func TestScreenStreamEnvClassifiesTheThreeStates(t *testing.T) {
	for _, c := range []struct {
		name  string
		raw   string
		state string
		path  string
	}{
		{"unset says nothing", "", ScreenStreamStateUnset, ""},
		{"whitespace is unset", "   ", ScreenStreamStateUnset, ""},
		{"zero declines", "0", ScreenStreamStateOff, ""},
		{"false declines", "false", ScreenStreamStateOff, ""},
		{"off declines", "off", ScreenStreamStateOff, ""},
		{"no declines", "no", ScreenStreamStateOff, ""},
		{"disabled declines", "disabled", ScreenStreamStateOff, ""},
		{"one arms at the resolved path", "1", ScreenStreamStateOn, ""},
		{"true arms", "true", ScreenStreamStateOn, ""},
		{"on arms", "on", ScreenStreamStateOn, ""},
		{"anything else IS the path", "/tmp/x.run.stream", ScreenStreamStatePath, "/tmp/x.run.stream"},
		{"a relative path is a path too", "out/run.stream", ScreenStreamStatePath, "out/run.stream"},
	} {
		got := ScreenStreamChoiceOf(cliverbs.ScreenStreamEnv, c.raw)
		if got.State != c.state {
			t.Errorf("%s: state = %q, want %q", c.name, got.State, c.state)
		}
		if got.Path != c.path {
			t.Errorf("%s: path = %q, want %q", c.name, got.Path, c.path)
		}
		if got.Source != cliverbs.ScreenStreamEnv {
			t.Errorf("%s: source = %q, want %q", c.name, got.Source, cliverbs.ScreenStreamEnv)
		}
	}
}

// TestScreenStreamEnvReadsTheCurrentNameFirst pins the ONE read point: the
// current spelling wins when both are exported, the pre-rename one still answers
// when it is alone, and `Source` names which one did (a caller prints that).
func TestScreenStreamEnvReadsTheCurrentNameFirst(t *testing.T) {
	t.Setenv(cliverbs.ScreenStreamEnv, "")
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")
	if got := ScreenStreamFromEnv(); got.State != ScreenStreamStateUnset {
		t.Fatalf("both unset: state = %q, want %q", got.State, ScreenStreamStateUnset)
	}

	t.Setenv(cliverbs.LegacyScreenStreamEnv, "1")
	got := ScreenStreamFromEnv()
	if got.State != ScreenStreamStateOn || got.Source != cliverbs.LegacyScreenStreamEnv {
		t.Fatalf("legacy alone: %+v, want on via %s", got, cliverbs.LegacyScreenStreamEnv)
	}

	// The current spelling WINS even when it declines and the alias arms it: an
	// operator who exported the new name for one run must not be overridden by a
	// stale profile entry.
	t.Setenv(cliverbs.ScreenStreamEnv, "0")
	got = ScreenStreamFromEnv()
	if got.State != ScreenStreamStateOff || got.Source != cliverbs.ScreenStreamEnv {
		t.Fatalf("current declines over an arming alias: %+v, want off via %s", got, cliverbs.ScreenStreamEnv)
	}
}

// TestScreenStreamPrecedenceIsArgvThenEnvThenDefault is the precedence grading
// this epic requires to be written down: a test drives every rung, including the
// two "the higher channel said nothing" falls-throughs, so the order cannot be
// silently rearranged.
func TestScreenStreamPrecedenceIsArgvThenEnvThenDefault(t *testing.T) {
	noChoice := ScreenStreamChoice{State: ScreenStreamStateUnset}
	flagOff := ScreenStreamFromFlag(cliverbs.NoScreenStream, "", true)
	flagOn := ScreenStreamFromFlag(cliverbs.ScreenStream, "", false)
	flagPath := ScreenStreamFromFlag(cliverbs.ScreenStream, "/explicit/x.stream", false)
	envOff := ScreenStreamChoiceOf(cliverbs.ScreenStreamEnv, "0")
	envOn := ScreenStreamChoiceOf(cliverbs.ScreenStreamEnv, "1")
	envPath := ScreenStreamChoiceOf(cliverbs.ScreenStreamEnv, "/env/x.stream")

	for _, c := range []struct {
		name    string
		flag    ScreenStreamChoice
		env     ScreenStreamChoice
		dflt    bool
		state   string
		origin  string
		wantPth string
	}{
		{"argv declines over an arming env", flagOff, envOn, true, ScreenStreamStateOff, ScreenStreamOriginArgv, ""},
		{"argv arms over a declining env", flagOn, envOff, false, ScreenStreamStateOn, ScreenStreamOriginArgv, ""},
		{"argv path over an env path", flagPath, envPath, true, ScreenStreamStatePath, ScreenStreamOriginArgv, "/explicit/x.stream"},
		{"env declines when argv is silent", noChoice, envOff, true, ScreenStreamStateOff, ScreenStreamOriginEnv, ""},
		{"env arms when argv is silent", noChoice, envOn, false, ScreenStreamStateOn, ScreenStreamOriginEnv, ""},
		{"env path when argv is silent", noChoice, envPath, true, ScreenStreamStatePath, ScreenStreamOriginEnv, "/env/x.stream"},
		{"chat default arms", noChoice, noChoice, ScreenStreamDefaultChat, ScreenStreamStateOn, ScreenStreamOriginDefault, ""},
		{"worker default declines", noChoice, noChoice, ScreenStreamDefaultWorker, ScreenStreamStateOff, ScreenStreamOriginDefault, ""},
	} {
		got := ScreenStreamResolve(c.flag, c.env, c.dflt)
		if got.State != c.state || got.Origin != c.origin || got.Path != c.wantPth {
			t.Errorf("%s: got state=%q origin=%q path=%q; want state=%q origin=%q path=%q",
				c.name, got.State, got.Origin, got.Path, c.state, c.origin, c.wantPth)
		}
		if got.Armed() != (c.state == ScreenStreamStateOn || c.state == ScreenStreamStatePath) {
			t.Errorf("%s: Armed() = %v contradicts state %q", c.name, got.Armed(), got.State)
		}
	}
}

// TestScreenStreamOffIsNeverTheUnsetState is the regression this parser was
// written for: `GOPI_SCREEN_STREAM=0` must be a DECISION, not an absence — the
// difference is what lets a surface announce a declined artifact instead of
// silently falling back to its default.
func TestScreenStreamOffIsNeverTheUnsetState(t *testing.T) {
	off := ScreenStreamChoiceOf(cliverbs.ScreenStreamEnv, "0")
	unset := ScreenStreamChoice{State: ScreenStreamStateUnset}
	if off.State == unset.State {
		t.Fatalf("a declining channel must not read as %q", unset.State)
	}
	if off.Armed() {
		t.Fatalf("a declining channel must not be armed: %+v", off)
	}
	// And the default does NOT resurrect a declined run: resolving an explicit
	// 0 against an arming surface default stays off.
	if got := ScreenStreamResolve(unset, off, ScreenStreamDefaultChat); got.State != ScreenStreamStateOff {
		t.Fatalf("env 0 + chat default: %+v, want off", got)
	}
}
