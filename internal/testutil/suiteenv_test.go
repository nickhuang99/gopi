package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
)

// suiteEnvRawEnvName asks a re-exec'd child to run in the PRE-FIX suite shape:
// scratch pin only, notify environment left exactly as the ambient environment
// hands it over. That is the state that delivered into the operator's live
// sink (suiteenv.go) — the child exists to prove the guard CATCHES it.
const suiteEnvRawEnvName = "GOPI_TESTUTIL_NOTIFY_RAW"

// suiteEnvAnchorTest is the anchor test the raw child runs: it asserts the
// suite notify hermeticity a pinned suite guarantees, so a raw suite must fail
// it. It also runs (and passes) in the pinned parent — the control.
const suiteEnvAnchorTest = "TestSuiteNotifyEnvAnchor"

// TestSuiteNotifyEnvAnchor is the two-sided anchor: GREEN in the pinned suite
// this package's TestMain installs, RED in a raw (pre-fix) child. Every other
// suite calls the same assertion (AssertSuiteNotifyEnvHermetic).
func TestSuiteNotifyEnvAnchor(t *testing.T) {
	AssertSuiteNotifyEnvHermetic(t)
	AssertSuiteHasNoNotifyTarget(t)
}

// TestSuiteNotifyEnvRawSuiteFails is the red proof demanded by the incident:
// a suite that does NOT install the notify sandbox must FAIL, not run. The
// child is this test binary re-exec'd twice, each reproducing one of the two
// shapes the leak actually appeared in — an ambient GOPI_SETTINGS pointing at
// the operator's profile (a dev shell / harness export), and no GOPI_SETTINGS
// at all (resolution falling back to the default path under HOME, the SAME
// operator file). Both must make the anchor fatal, and the child's exit must
// be non-zero with the actionable message.
func TestSuiteNotifyEnvRawSuiteFails(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	if os.Getenv(reexecQueryEnvName) != "" {
		t.Skip("already inside the re-exec child — the parent owns this assertion")
	}
	operatorSettings := filepath.Join(os.Getenv("HOME"), ".gopi", "settings.json")
	cases := []struct {
		name        string
		settingsEnv string // "" = set GOPI_SETTINGS to the operator's profile path
		wantMessage string
	}{
		{
			name:        "ambient GOPI_SETTINGS points at the operator's profile",
			settingsEnv: operatorSettings,
			wantMessage: "operator's real notify configuration",
		},
		{
			name:        "no GOPI_SETTINGS — resolution falls back to the default profile path",
			settingsEnv: "",
			wantMessage: "fell back to the default settings path",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run", "^"+suiteEnvAnchorTest+"$", "-test.v")
			cmd.Env = append(scrubbedNotifyEnv(),
				suiteEnvRawEnvName+"=1",
				reexecQueryEnvName+"=1",
				config.SettingsEnv+"="+tc.settingsEnv)
			out, err := cmd.CombinedOutput()
			text := string(out)
			exit, ok := exitCodeOf(err)
			if !ok {
				t.Fatalf("child did not exit with a status (err = %v):\n%s", err, text)
			}
			if exit == 0 {
				t.Fatalf("a suite in the PRE-FIX shape (no notify sandbox, %s=%q) ran the anchor GREEN — "+
					"the live-sink leak is unguarded:\n%s", config.SettingsEnv, tc.settingsEnv, text)
			}
			if !strings.Contains(text, tc.wantMessage) {
				t.Fatalf("child failed without the actionable message %q — an undiagnosable guard is a quiet "+
					"guard:\n%s", tc.wantMessage, text)
			}
		})
	}
}

// scrubbedNotifyEnv is the parent environment with the notify channel and the
// settings override removed, so the child's configuration is decided ONLY by
// what the case sets — the child must not inherit the parent's own sandbox.
func scrubbedNotifyEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if key == config.SettingsEnv {
			continue
		}
		drop := false
		for _, n := range notifyEnvNames {
			if key == n {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// TestSuiteNotifyEnvPinnedByMain proves this package's own TestMain installed
// the sandbox BEFORE any test ran (the guard above is only meaningful if the
// pin is what the suite actually runs with): the settings path must be inside
// the pinned scratch root, never the developer's profile file.
func TestSuiteNotifyEnvPinnedByMain(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" || os.Getenv(suiteEnvRawEnvName) != "" {
		t.Skip("re-exec child — it deliberately runs without the pin")
	}
	path := os.Getenv(config.SettingsEnv)
	if !SuiteTempRootPinned() {
		t.Fatalf("scratch root pin did not engage, so the notify sandbox cannot be placed")
	}
	if !pathUnder(path, SuiteTempRoot()) {
		t.Fatalf("%s=%q is not under the pinned scratch root %q — the suite would read the real profile settings",
			config.SettingsEnv, path, SuiteTempRoot())
	}
}
