package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// Suite notify-env hermeticity (the 2026-09-12 live-sink pollution).
//
// ROOT CAUSE THIS FILE EXISTS FOR: a suite's TestMain dropped the five
// GOPI_TASK_NOTIFY_* vars and called it hermetic — but config.ResolveNotify
// resolves env > settings.json, and the DURABLE layer it falls back to is
// ResolveSettingsPath(), i.e. the ambient GOPI_SETTINGS. On a developer host
// that is the operator's REAL ~/.gopi/settings.json, whose notify.cmd is
// `cat >> ~/.gopi/notify/task-events.jsonl`. So every test that walks the
// task state machine (submit → claim → complete/fail) delivered into the
// operator's live sink: measured 2026-09-12, one unrelated test
// (TestBidirEpic1SelfSubmittedAndTerminal) wrote 655 bytes, and a full
// `go test ./internal/tools ./cmd/agent` flooded ~200 test task-ids
// (t-cli-e2e, t-div, t-c1, …) into the live sink within two minutes.
//
// Dropping the env vars is NOT enough and unsetting GOPI_SETTINGS is NOT
// enough either: the path resolution then falls back to DefaultSettingsPath()
// — the same operator file under GOPI_HOME/HOME. The only repair is to point
// GOPI_SETTINGS at a per-run sandbox path that DOES NOT EXIST, so the durable
// layer resolves to nothing and a test that wants delivery must name it
// explicitly (t.Setenv / a child env entry — which still wins, env > settings).
//
// This is wired into PinSuiteForTestMain and RunSuiteMain, so every suite
// (17 TestMains) inherits it without repeating the loop. The runtime guard is
// AssertSuiteNotifyEnvHermetic/AssertSuiteHasNoNotifyTarget
// (internal/tools + cmd/agent run both), and
// TestEveryTestMainSuiteIsNotifyHermetic is the static gate that keeps a new
// hand-rolled TestMain from bypassing the pin.

const (
	// suiteEnvDirName is the per-run sandbox directory under the scratch root
	// that holds the suite's settings path.
	suiteEnvDirName = "suite-env"
	// suiteSettingsFileName is the sandbox settings file — deliberately NEVER
	// created: a missing file is exactly the "no notify configured" view, and
	// a test that needs a settings file writes its own.
	suiteSettingsFileName = "settings.json"
)

// notifyEnvNames are the volatile notify knobs a dev shell / CI runner may
// export. A serve daemon inherits env from whatever spawned it, so an ambient
// value here would make every test deliver to a target it never named. The
// names come from config (Rule 1b/14 — never a bare literal).
var notifyEnvNames = []string{
	config.NotifyWebhookEnv,
	config.NotifyCmdEnv,
	config.NotifyEventsEnv,
	config.NotifyPatternEnv,
	config.NotifyAsyncEnv,
}

// PinSuiteNotifyEnv makes the calling suite notify-hermetic: the five volatile
// notify env vars are dropped (always, even on the no-pin escape path) and
// GOPI_SETTINGS is pointed at a per-run sandbox path inside the scratch root,
// so the durable settings layer resolves to nothing rather than to the
// operator's real settings.json.
//
// Errors are REPORTED, never swallowed (Rule 15b): a suite that silently kept
// the ambient settings path would deliver into the operator's live sink again.
func PinSuiteNotifyEnv(suite string) {
	for _, env := range notifyEnvNames {
		// Handled, not discarded (Rule 15b): Unsetenv only fails on a name
		// carrying '=' or NUL, which would mean the constant itself is broken —
		// an ambient variable left in place is exactly the leak this pin exists
		// to close, so the failure is reported rather than swallowed.
		if err := os.Unsetenv(env); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: %s not dropped: %v\n", env, err)
		}
	}
	if !SuiteTempRootPinned() {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: scratch root pin did not engage (%s set) — "+
			"%s is NOT sandboxed, so a test that walks the task state machine may deliver to the "+
			"ambient notify sink. Fix the scratch home or accept this explicitly.\n",
			suite, gotestSuiteNoPinEnvName, config.SettingsEnv)
		return
	}
	dir := filepath.Join(SuiteTempRoot(), suiteEnvDirName)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: settings sandbox %s: %v — %s left ambient\n",
			suite, dir, err, config.SettingsEnv)
		return
	}
	if err := os.Setenv(config.SettingsEnv, filepath.Join(dir, suiteSettingsFileName)); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: pinning %s: %v\n", suite, config.SettingsEnv, err)
	}
}

// AssertSuiteNotifyEnvHermetic is the guard every suite can run as a one-line
// test: it fails when this process's notify environment is NOT the sandbox the
// suite entry point is supposed to have installed. Without this guard a future
// TestMain edit (or a new suite that bypasses PinSuiteForTestMain) silently
// reopens the live-sink leak — the leak is invisible from inside the suite,
// which is why it survived the first fix.
func AssertSuiteNotifyEnvHermetic(t *testing.T) {
	t.Helper()
	for _, env := range notifyEnvNames {
		if v, present := os.LookupEnv(env); present {
			t.Errorf("%s=%q is set in the suite environment — the suite entry point must drop every "+
				"notify env var (%s); an ambient value makes each test deliver to a target it never named",
				env, v, "PinSuiteNotifyEnv")
		}
	}
	path := os.Getenv(config.SettingsEnv)
	if path == "" {
		t.Fatalf("%s is unset — the suite fell back to the default settings path (the developer's real "+
			"profile settings.json, whose notify.cmd writes the operator's live sink). Got %q",
			config.SettingsEnv, path)
	}
	if !SuiteTempRootPinned() || !pathUnder(path, SuiteTempRoot()) {
		t.Fatalf("%s=%q is not inside the suite scratch root %q — the durable settings layer would read "+
			"the operator's real notify configuration", config.SettingsEnv, path, SuiteTempRoot())
	}
}

// AssertSuiteHasNoNotifyTarget is the second half of the guard: the suite's
// RESOLVED notify configuration (env over the durable settings layer) must name
// no target, so a test that never asks for delivery cannot deliver. A test that
// wants delivery names it explicitly (t.Setenv / a child env entry).
func AssertSuiteHasNoNotifyTarget(t *testing.T) {
	t.Helper()
	if n := config.ResolveNotify(); n.Cmd != "" || n.Webhook != "" {
		t.Errorf("the suite resolves a notify target (cmd=%q webhook=%q) — an unconfigured test would deliver "+
			"into it; the notify env sandbox must leave none (PinSuiteNotifyEnv)", n.Cmd, n.Webhook)
	}
}

// pathUnder reports whether p lives inside root (both canonicalised the same
// way the pin canonicalises its own root).
func pathUnder(p, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
