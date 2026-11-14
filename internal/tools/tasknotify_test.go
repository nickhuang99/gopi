package tools

// Tests for task state-transition notifications (tasknotify.go): a
// transition fires the configured targets (cmd with the JSON payload on
// stdin), a non-subscribed status fires nothing, the pattern filter gates
// delivery, and the payload carries task_id/status/summary/elapsed/tokens.
// Every test uses its own PROFILE (GOPI_HOME) and a cmd target that appends the
// payload to a temp file — no network, fully deterministic (sync delivery).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// taskNotifyProfileSandbox gives the test its OWN PROFILE: GOPI_HOME at a temp
// home, with the ledger overrides masked so the ledger resolves to that
// profile's own <home>/.gopi/memory.db.
//
// This is the ONLY sandbox shape a notify test may build, and it is a helper so
// no fixture re-spells it (Rule 14). Two shapes are deliberately refused by
// construction:
//   - "GOPI_MEMORY_DB=<temp>, profile ambient" — the ledger moves while the
//     notify target stays the OPERATOR's. That is not isolation, it is the exact
//     mis-configuration the delivery gate suppresses (taskNotifyDelivers), so a
//     fixture built from it tests the gate instead of the behaviour it names;
//   - "no override at all" — the suite's ambient ledger, which is a different
//     file from a per-test one and leaks rows between tests.
func taskNotifyProfileSandbox(t *testing.T) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")
}

// taskNotifyProfileLedger gives the test a fresh PROFILE and returns the ledger
// that profile OWNS (<home>/.gopi/memory.db), with the directory materialised
// and the ledger pinned at rank 2 to that same file.
//
// Why rank 2 and not just rank 4: the suite entry point pins GOPI_MEMORY_DB
// (testutil.PinSuiteLedgerEnv), and an env override wins over the profile
// default — so setting GOPI_HOME alone would leave the LEDGER on the suite's
// file while the profile (and therefore the notify target) moved, which is the
// foreign-ledger shape the delivery gate suppresses. Both halves must name the
// same file; that is the property this helper exists to make impossible to
// mis-spell.
func taskNotifyProfileLedger(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	profileDir := config.ProfileDirIn(home)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("create the sandbox profile dir: %v", err)
	}
	ledger := filepath.Join(profileDir, config.MemoryDBFileName)
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, ledger)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	return ledger
}

// taskNotifyProfileForLedger makes an EXPLICIT ledger path the CURRENT profile's
// own file, by moving GOPI_HOME to the home that file belongs to. The path must
// have the profile shape (<home>/.gopi/memory.db) — a path that does not is
// REFUSED rather than given a guessed home (Rule 15), because a wrong home would
// silently rebuild the foreign-ledger shape the delivery gate hides.
//
// It exists for fixtures that must NAME their ledger (the two-node sync sims
// swap a local and a remote file): the correct configuration for such a test is
// still a correctly-isolated PROFILE, not "a private db with the operator's
// profile left ambient".
func taskNotifyProfileForLedger(t *testing.T, ledger string) {
	t.Helper()
	profileDir := filepath.Dir(ledger)
	if filepath.Base(profileDir) != ".gopi" || filepath.Base(ledger) != config.MemoryDBFileName {
		t.Fatalf("taskNotifyProfileForLedger(%q): want a profile-shaped path <home>/.gopi/%s — this helper refuses to guess a home", ledger, config.MemoryDBFileName)
	}
	t.Setenv(config.GopiDirEnv, filepath.Dir(profileDir))
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")
}

// taskNotifyTestSetup points the test at a PROFILE-CORRECT sandbox — GOPI_HOME
// at a temp home, so the ledger resolves to that profile's own
// <home>/.gopi/memory.db — and at a cmd target that appends the payload.
// Returns the log file path.
//
// WHY GOPI_HOME AND NOT A BARE GOPI_MEMORY_DB (changed 2026-11-04). The old
// shape was `GOPI_MEMORY_DB=<temp>` with the profile left ambient: the ledger
// moved while the notify target still resolved to the OPERATOR's settings.json.
// That is not a sandbox, it is the exact mis-configuration the delivery gate
// now suppresses (taskNotifyDelivers) — a fixture built from it either fails
// when the gate is right, or it re-teaches the wrong shape to every test that
// copies it. A test that wants an isolated PROFILE isolates the profile.
func taskNotifyTestSetup(t *testing.T) string {
	t.Helper()
	taskNotifyProfileSandbox(t)
	log := filepath.Join(t.TempDir(), "notify.log")
	// Payload arrives on stdin; sh -c appends it as one line (cat + echo
	// newline) to the log.
	t.Setenv(taskNotifyCmdEnv, "cat >> "+log+" && echo >> "+log)
	t.Setenv(taskNotifyWebhookEnv, "")
	return log
}

// readNotifyLog parses every JSON payload appended to the log file.
func readNotifyLog(t *testing.T, log string) []taskNotifyPayload {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // zero deliveries → the log file was never created
		}
		t.Fatalf("read notify log: %v", err)
	}
	var out []taskNotifyPayload
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p taskNotifyPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("parse payload %q: %v", line, err)
		}
		out = append(out, p)
	}
	return out
}

// TestTaskNotifyTransitionFires exercises the full P2 lifecycle with the
// default event set: pending→claimed (TaskNext CAS), claimed→in_progress and
// in_progress→completed (TaskUpdate) each fire exactly one notification; the
// payload carries task_id/status/summary/elapsed/tokens/previous_status.
func TestTaskNotifyTransitionFires(t *testing.T) {
	log := taskNotifyTestSetup(t)

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "notify lifecycle — elapsed=2.5s iter=3 tokens=1234", Payload: "notify briefing",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)

	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	events := readNotifyLog(t, log)
	if len(events) != 3 {
		t.Fatalf("want 3 notifications (claimed, in_progress, completed), got %d: %+v", len(events), events)
	}
	wantEvents := []string{taskStatusClaimed, taskStatusInProgress, taskStatusCompleted}
	wantPrev := []string{taskStatusPending, taskStatusClaimed, taskStatusInProgress}
	for i, ev := range events {
		if ev.Event != wantEvents[i] || ev.Status != wantEvents[i] {
			t.Errorf("notification %d: event/status = %s/%s, want %s", i, ev.Event, ev.Status, wantEvents[i])
		}
		if ev.PreviousStatus != wantPrev[i] {
			t.Errorf("notification %d: previous_status = %s, want %s", i, ev.PreviousStatus, wantPrev[i])
		}
		if ev.TaskID != taskID {
			t.Errorf("notification %d: task_id = %s, want %s", i, ev.TaskID, taskID)
		}
	}
	last := events[len(events)-1]
	if last.Summary == "" {
		t.Error("completed notification: empty summary")
	}
	if last.Tokens != 1234 {
		t.Errorf("completed notification: tokens = %d, want 1234 (parsed from summary tail)", last.Tokens)
	}
	// bug056: the writeback digest rides the audit trail, NOT the summary —
	// the notify payload's Result field carries it (the summary stays the
	// submit-time digest). The summary's own stats tail here is a legacy /
	// submit-time case; Result falls back to the summary when the events
	// trail is empty, so a plain TaskUpdate still yields a non-empty Result.
	if last.Result == "" {
		t.Error("completed notification: empty result (event digest)")
	}
	if last.ElapsedSeconds < 0 {
		t.Errorf("completed notification: negative elapsed %v", last.ElapsedSeconds)
	}
	if last.Timestamp == "" {
		t.Error("completed notification: missing timestamp")
	}
}

// TestTaskNotifyNonSubscribedStatusSkipped is the negative half of the spec:
// GOPI_TASK_NOTIFY_EVENTS=completed must silence claimed/in_progress
// transitions AND the summary-only edit (no status move, no notification).
func TestTaskNotifyNonSubscribedStatusSkipped(t *testing.T) {
	log := taskNotifyTestSetup(t)
	t.Setenv(taskNotifyEventsEnv, "completed")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "silent transitions"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)

	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil { // claimed: not subscribed
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil { // not subscribed
		t.Fatalf("start: %v", err)
	}
	// M4.5 ⑤: a summary-only edit is REFUSED (content immutable) — the
	// old "summary edit fires nothing" expectation is now a hard error.
	if _, err := TaskUpdate(taskID, ""); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("summary-only edit must be refused, got err=%v", err)
	}
	if events := readNotifyLog(t, log); len(events) != 0 {
		t.Fatalf("non-subscribed statuses fired %d notifications: %+v", len(events), events)
	}

	if _, err := TaskUpdate(taskID, taskStatusCompleted); err != nil { // subscribed
		t.Fatalf("complete: %v", err)
	}
	if events := readNotifyLog(t, log); len(events) != 1 || events[0].Status != taskStatusCompleted {
		t.Fatalf("subscribed status did not fire exactly once: %+v", events)
	}
}

// TestTaskNotifyPatternFilter covers the task_type_pattern equivalent: the
// glob matches a task's tags (whole string or an individual tag) and
// task_id; a non-matching task stays silent.
func TestTaskNotifyPatternFilter(t *testing.T) {
	log := taskNotifyTestSetup(t)
	t.Setenv(taskNotifyPatternEnv, "feat*")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "tagged task", Payload: "tagged briefing", Tags: "feat,web"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taggedID := taskNotifyExtractID(t, out)
	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taggedID, taskStatusInProgress); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Untagged task: same transitions, pattern must NOT match.
	out2, err := TaskSubmit(TaskSubmitOptions{Summary: "untagged task", Tags: "chore"})
	if err != nil {
		t.Fatalf("submit 2: %v", err)
	}
	untaggedID := taskNotifyExtractID(t, out2)
	if _, err := TaskUpdate(untaggedID, taskStatusInProgress); err != nil {
		t.Fatalf("start 2: %v", err)
	}

	events := readNotifyLog(t, log)
	for _, ev := range events {
		if ev.TaskID == untaggedID {
			t.Errorf("pattern-filtered task %s fired a notification: %+v", untaggedID, ev)
		}
	}
	// Both default-set transitions of the tagged task fire; the untagged
	// task's in_progress must not appear.
	if len(events) != 2 {
		t.Fatalf("want exactly 2 notifications for the tagged task, got %+v", events)
	}
	for _, ev := range events {
		if ev.TaskID != taggedID {
			t.Fatalf("unexpected notification for %s: %+v", ev.TaskID, ev)
		}
	}
	if events[0].Status != taskStatusClaimed || events[1].Status != taskStatusInProgress {
		t.Fatalf("want claimed then in_progress, got %s then %s", events[0].Status, events[1].Status)
	}
}

// TestTaskNotifyDisabledByDefault: no env vars → zero notifications (the
// ledger stays a quiet side-effect-free default).
func TestTaskNotifyDisabledByDefault(t *testing.T) {
	taskNotifyTestSetup(t)
	t.Setenv(taskNotifyCmdEnv, "")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "quiet"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)
	// Real transition path (pending → failed is illegal; walk the machine:
	// pending → claimed → in_progress → failed).
	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusFailed); err != nil {
		t.Fatalf("fail: %v", err)
	}
}

// TestTaskNotifyPatternMatchUnit pins the matching matrix (tags whole /
// individual / task_id / broadcast / invalid glob).
func TestTaskNotifyPatternMatchUnit(t *testing.T) {
	cases := []struct {
		pattern, taskID, tags string
		want                  bool
	}{
		{"", "t-1", "", true},                 // broadcast
		{"feat", "t-1", "feat,web", true},     // individual tag
		{"feat,web", "t-1", "feat,web", true}, // whole tag string
		{"feat*", "t-1", "feat,web", true},    // glob over tag
		{"t-*", "t-123", "", true},            // task_id
		{"web", "t-1", "feat", false},         // no match
		{"[", "t-1", "feat", false},           // invalid glob matches nothing
	}
	for _, c := range cases {
		if got := taskNotifyPatternMatch(c.pattern, c.taskID, c.tags); got != c.want {
			t.Errorf("taskNotifyPatternMatch(%q, %q, %q) = %v, want %v", c.pattern, c.taskID, c.tags, got, c.want)
		}
	}
}

// TestTaskNotifyTokensUnit pins best-effort token parsing from the summary
// tail the auto-register harness appends.
func TestTaskNotifyTokensUnit(t *testing.T) {
	cases := map[string]int{
		"done — elapsed=1.5s iter=2 tokens=42": 42,
		"tokens=7 then tokens=99":              99, // last wins
		"no stats here":                        0,
		"":                                     0,
	}
	for summary, want := range cases {
		if got := taskNotifyTokens(summary); got != want {
			t.Errorf("taskNotifyTokens(%q) = %d, want %d", summary, got, want)
		}
	}
}

// taskNotifyExtractID pulls the task id out of TaskSubmit's rendered output
// (uuid v7 for auto-submits, t-* or custom for explicit-id fixtures).
func taskNotifyExtractID(t *testing.T, out string) string {
	t.Helper()
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "t-") || len(f) == 36 {
			return f
		}
	}
	t.Fatalf("no task id in submit output %q", out)
	return ""
}

// --- task-notify settings.json layer (2026-09-02 incident regression
// guards) ---
//
// Resolution precedence per knob is GOPI_TASK_NOTIFY_* env >
// settings.json "notify" section > code default (config.ResolveNotify —
// the single resolver every notify path funnels through). These tests
// exercise the SETTINGS layer, which the ambient env TestMain guard leaves
// untouched: each test points GOPI_SETTINGS at a sandbox file so the
// ambient live settings.json can never leak a notify target in (memory #1311
// pitfall — GOPI_SETTINGS sandbox isolation).

// taskNotifySettingsSandbox points GOPI_SETTINGS at a sandbox settings.json
// carrying the given "notify" section ("" = no notify section), and the
// ledger at a temp memory.db. Returns the settings path.
func taskNotifySettingsSandbox(t *testing.T, notifySection string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	body := "{"
	if notifySection != "" {
		body += `"notify": {` + notifySection + `}`
	}
	body += "}"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", p)
	// PROFILE-CORRECT sandbox (see taskNotifyTestSetup): GOPI_HOME moves the
	// profile, so the ledger resolves to its OWN <home>/.gopi/memory.db and the
	// delivery gate passes it through. A bare GOPI_MEMORY_DB here would leave the
	// notify target on the operator's profile — the exact shape the gate
	// suppresses.
	t.Setenv(config.GopiDirEnv, dir)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")
	return p
}

// TestTaskNotifySettingsCmdDelivers (acceptance 1): NO notify env, but the
// settings.json "notify" section declares a cmd + events — a status change
// must fire the cmd (the durable layer survives a spawn with no env, the
// exact 2026-09-02 scenario).
func TestTaskNotifySettingsCmdDelivers(t *testing.T) {
	log := filepath.Join(t.TempDir(), "notify.log")
	taskNotifySettingsSandbox(t, `"cmd": "cat >> `+log+` && echo >> `+log+`", "events": "claimed,completed"`)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "settings-notify lifecycle", Payload: "settings briefing"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)
	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("start: %v", err) // not in the settings events list → must NOT fire
	}
	if _, err := TaskUpdate(taskID, taskStatusCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	events := readNotifyLog(t, log)
	if len(events) != 2 {
		t.Fatalf("settings notify.cmd must deliver claimed+completed, got %d: %+v", len(events), events)
	}
	if events[0].Event != taskStatusClaimed || events[1].Event != taskStatusCompleted {
		t.Fatalf("want claimed then completed, got %s then %s", events[0].Event, events[1].Event)
	}
}

// TestTaskNotifyEnvWinsOverSettings (acceptance 2): env set → env value wins
// over the settings section for the SAME knob.
func TestTaskNotifyEnvWinsOverSettings(t *testing.T) {
	settingsLog := filepath.Join(t.TempDir(), "settings-notify.log")
	envLog := filepath.Join(t.TempDir(), "env-notify.log")
	taskNotifySettingsSandbox(t, `"cmd": "cat >> `+settingsLog+` && echo >> `+settingsLog+`"`)
	// Env override for the cmd knob only (events stays via settings default
	// — but cmd is fully replaced by the env value).
	t.Setenv(taskNotifyCmdEnv, "cat >> "+envLog+" && echo >> "+envLog)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "env-wins lifecycle", Payload: "env briefing"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)
	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if ev := readNotifyLog(t, envLog); len(ev) != 3 {
		t.Fatalf("env cmd must receive all 3 default-set events, got %d: %+v", len(ev), ev)
	}
	if sl := readNotifyLog(t, settingsLog); len(sl) != 0 {
		t.Fatalf("settings cmd must NOT fire when env overrides, got %+v", sl)
	}
}

// TestTaskNotifyNeitherConfiguresDisabled (acceptance 3): neither env nor a
// settings notify section → no delivery, no error (backward compat — the
// pre-settings behavior).
func TestTaskNotifyNeitherConfiguresDisabled(t *testing.T) {
	log := filepath.Join(t.TempDir(), "notify.log")
	taskNotifySettingsSandbox(t, "") // settings.json with NO notify section

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "quiet settings", Payload: "quiet briefing"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)
	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if ev := readNotifyLog(t, log); len(ev) != 0 {
		t.Fatalf("no notify config must deliver nothing, got %+v", ev)
	}
}

// TestAlarmEventLandsWithSettingsOnlyConfig (acceptance 4): an alarm event
// actually lands in the notify target with settings-only config — the exact
// 2026-09-02 regression (serve restarted without the env, delivery silently
// broken).
//
// REWRITTEN 2026-11-05 with the stale sweep's deletion: the assertion used to
// be produced by DetectStaleTasks (a dead-pid zombie row → a "stale" event);
// that producer is deleted 2026-11-05, so the event is raised through the
// surviving production entry point (NotifySyntheticEvent).
// That detector and its whole carrier set are gone, so the SURVIVING duty is
// proven through the producer that remains — NotifySyntheticEvent, the
// production entry point every session-level alarm uses. The coverage kept is
// the one this test was for: settings-only config (no env) reaches the sink.
func TestAlarmEventLandsWithSettingsOnlyConfig(t *testing.T) {
	log := filepath.Join(t.TempDir(), "notify.log")
	taskNotifySettingsSandbox(t, `"cmd": "cat >> `+log+` && echo >> `+log+`", "events": "stale"`)

	NotifySyntheticEvent(TaskHeartbeatNotifyEvent, "sess-settings-only", "test-model",
		"a settings-only notify config must still deliver an alarm")

	deadline := time.Now().Add(5 * time.Second)
	var body []byte
	var err error
	for {
		body, err = os.ReadFile(log)
		if err == nil && strings.Contains(string(body), `"event":"`+TaskHeartbeatNotifyEvent+`"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("settings-only alarm event not delivered (log=%s err=%v)", log, err)
		}
		time.Sleep(100 * time.Millisecond) //stylegate:allow-sleep poll interval while the notify command delivers the alarm
	}
	if !strings.Contains(string(body), "sess-settings-only") {
		t.Errorf("payload must name the alarm's subject, got:\n%s", body)
	}
}
