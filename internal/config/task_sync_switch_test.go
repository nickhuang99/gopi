package config

// task_sync_switch_test.go — the task-row-synchronization switch
// (settings.json "task_sync" / GOPI_TASK_SYNC, default OFF).
//
// The switch's contract has two halves and both are asserted here, because a
// default-OFF knob fails in two opposite ways: it can be welded to a channel
// that should have stayed ON (the upgrade posturer's bug), or it can be
// reported as OFF while something still crosses the network (the whole epic
// would be a comment). Rule 17: every env flip is t.Setenv (test-scoped).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTaskSyncSettings writes a settings.json fixture and points GOPI_SETTINGS
// at it, with both switch channels cleared (the identity cache is reset too:
// ResolveNodeRole memoises process-wide state, and this file's fixtures sit
// beside node_role_test.go's).
func writeTaskSyncSettings(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsEnv, p)
	t.Setenv(TaskSyncEnv, "")
	t.Setenv(RoleEnv, "")
	t.Setenv(SyncToEnv, "")
	nodeIdentity.role, nodeIdentity.syncTo = "", ""
	t.Cleanup(func() { nodeIdentity.role, nodeIdentity.syncTo = "", "" })
	return p
}

// TestTaskSyncDefaultIsOff is the epic's central claim: absent from BOTH
// channels, the switch resolves OFF. This is what makes an upgrading node with
// a stale role=client + sync_to stop forwarding without touching its settings
// file.
func TestTaskSyncDefaultIsOff(t *testing.T) {
	writeTaskSyncSettings(t, `{"role":"client","sync_to":"peerbox","sync_peers":["peerbox"]}`)
	if TaskSyncEnabled() {
		t.Fatal("task_sync absent from settings AND env must resolve OFF — a stale role/sync_to must not keep automatic exchange on")
	}
	if line := TaskSyncLine(); !strings.Contains(line, "off") {
		t.Fatalf("banner line must state the OFF state, got %q", line)
	}
}

// TestTaskSyncExplicitFalseIsOff pins the explicit spelling too: a settings
// file that has SEEN the knob and carries `false` behaves like one that never
// heard of it.
func TestTaskSyncExplicitFalseIsOff(t *testing.T) {
	writeTaskSyncSettings(t, `{"task_sync":false}`)
	if TaskSyncEnabled() {
		t.Fatal(`"task_sync": false must resolve OFF`)
	}
}

// TestTaskSyncSettingsOn is the enabling half through the settings channel,
// and the banner names the channel that won (so an operator never has to
// guess which file turned it on).
func TestTaskSyncSettingsOn(t *testing.T) {
	writeTaskSyncSettings(t, `{"task_sync":true}`)
	if !TaskSyncEnabled() {
		t.Fatal(`"task_sync": true must resolve ON`)
	}
	line := TaskSyncLine()
	if !strings.Contains(line, "on") || !strings.Contains(line, TaskSyncSettingsKey) {
		t.Fatalf("banner line must state ON and name %s, got %q", TaskSyncSettingsKey, line)
	}
}

// TestTaskSyncEnvOverridesSettings: env > settings, in BOTH directions — the
// precedence every neighbouring knob carries. The OFF direction is the one that
// matters operationally: it lets an operator silence a node whose settings file
// (shared across a fleet) says on.
func TestTaskSyncEnvOverridesSettings(t *testing.T) {
	writeTaskSyncSettings(t, `{"task_sync":false}`)
	t.Setenv(TaskSyncEnv, "1")
	if !TaskSyncEnabled() {
		t.Fatal("GOPI_TASK_SYNC=1 must override an off settings value")
	}
	writeTaskSyncSettings(t, `{"task_sync":true}`)
	t.Setenv(TaskSyncEnv, "off")
	if TaskSyncEnabled() {
		t.Fatal("GOPI_TASK_SYNC=off must override an on settings value")
	}
}

// TestTaskSyncUnreadableSpellingFallsThroughNotOff: a typo is neither an
// enabling nor a disabling spelling. It must not be read as "off" SILENTLY —
// the settings channel still decides (here: ON), and the fall-through is
// recorded on stderr for the operator. (Rule 15: the failure this avoids is a
// node whose operator believed they had enabled exchange.)
func TestTaskSyncUnreadableSpellingFallsThroughNotOff(t *testing.T) {
	writeTaskSyncSettings(t, `{"task_sync":true}`)
	t.Setenv(TaskSyncEnv, "ture") // the typo, not a boolean
	if !TaskSyncEnabled() {
		t.Fatal("an unrecognised env spelling must fall through to the settings channel, never silently disable the switch")
	}
}

// TestTaskSyncUnreadableSettingsFailsSafeOff: an unreadable settings file means
// the answer is UNKNOWN, and unknown must resolve to the safe posture. A
// malformed file that read as ON would start network exchange nobody declared.
func TestTaskSyncUnreadableSettingsFailsSafeOff(t *testing.T) {
	writeTaskSyncSettings(t, "{ this is not json")
	if TaskSyncEnabled() {
		t.Fatal("an unparseable settings file must resolve OFF, not ON")
	}
}

// TestTaskSyncLineNamesTheEnablingKnobs: the off line is the operator's only
// pointer at how to turn the feature on, so it must carry both spellings — a
// switch documented nowhere reachable is a switch that stays off forever.
// The ABSENT case is exactly the case that keeps the pointer (a declaration
// needs no instructions), so this test is also the guard that the new
// declared-off branch did not swallow the default wording.
func TestTaskSyncLineNamesTheEnablingKnobs(t *testing.T) {
	writeTaskSyncSettings(t, `{}`)
	line := TaskSyncLine()
	for _, want := range []string{TaskSyncSettingsKey, TaskSyncEnv} {
		if !strings.Contains(line, want) {
			t.Errorf("the off banner line must name %q, got %q", want, line)
		}
	}
}

// TestTaskSyncLineDistinguishesTheThreeOffSources is the epic's acceptance:
// OFF arrives from three different places and the banner must render three
// DISTINCT strings, because "my settings file declares false — was it seen?"
// is unanswerable when a declaration and an absence print the same words.
//
// The three cases:
//
//	env-supplied   GOPI_TASK_SYNC=0 (with a settings file saying nothing) → names the env var
//	declared       the settings file CARRIES "task_sync": false        → says the file declared it
//	absent         neither channel mentions the knob                   → the default wording
//
// A fourth row (env=0 over a file that DECLARES true) pins that the env
// channel wins the LABEL too, not merely the value: the banner must not credit
// a settings declaration the env var has just overridden.
func TestTaskSyncLineDistinguishesTheThreeOffSources(t *testing.T) {
	type c struct {
		name     string
		settings string
		env      string
		want     string // a substring UNIQUE to this case
	}
	cases := []c{
		{"absent", `{}`, "", "off (default"},
		{"declared", `{"task_sync":false}`, "", "off (" + taskSyncDeclaredHere + ": false"},
		{"env-supplied", `{}`, "0", "off (" + TaskSyncEnv + ": false"},
		{"env-beats-declared-label", `{"task_sync":true}`, "off", "off (" + TaskSyncEnv + ": false"},
	}
	lines := make(map[string]string, len(cases))
	for _, tc := range cases {
		writeTaskSyncSettings(t, tc.settings)
		if tc.env != "" {
			t.Setenv(TaskSyncEnv, tc.env)
		}
		if TaskSyncEnabled() {
			t.Fatalf("%s: resolved ON, expected OFF (the switch's own behaviour must not move)", tc.name)
		}
		line := TaskSyncLine()
		lines[tc.name] = line
		if !strings.Contains(line, tc.want) {
			t.Errorf("%s: banner %q must contain %q — the deciding channel has to be visible", tc.name, line, tc.want)
		}
	}
	// The point of the exercise: three DIFFERENT strings for three different
	// facts. Any collapse back to one wording fails here, not in an operator's
	// terminal at 2am. Only the three distinct SOURCES are compared: the
	// fourth row is env-supplied too, and two env-decided OFFs SHARING one
	// line is correct (same channel, same value, same fact).
	seen := map[string]string{}
	for _, tc := range cases[:3] {
		if prev, dup := seen[lines[tc.name]]; dup {
			t.Errorf("%s and %s render the SAME banner %q — an operator cannot tell a declaration from an absence",
				prev, tc.name, lines[tc.name])
			continue
		}
		seen[lines[tc.name]] = tc.name
	}
	if got := lines["absent"]; !strings.Contains(got, TaskSyncSettingsKey) || !strings.Contains(got, TaskSyncEnv) {
		t.Errorf("absent case %q must keep naming both enabling knobs", got)
	}
	if got := lines["declared"]; strings.Contains(got, "default") {
		t.Errorf("declared case %q must not claim to be the default — the file declared it", got)
	}
}
