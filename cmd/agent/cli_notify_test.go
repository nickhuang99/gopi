package main

// cli_notify_test.go — the `gopi notify` surface's own contract.
//
// TWO LEVELS, and both are needed:
//
//	PARSE level   — the flag grammar, and the three refusals that exist because
//	                the alternative is a silent wrong answer (--follow with no
//	                cursor re-reads the whole sink every tick; --since with an
//	                unparseable value would ignore the window; a sink that
//	                cannot be resolved must not read as "no events").
//	BINARY level  — the REAL built binary against a REAL sink, so a change
//	                that wires the verb to the wrong handler (the 2026-09-07
//	                "unexpected positional argument" routing bug this package's
//	                contract suite exists for) fails here.
//
// The fixtures are the shapes the operator's sink actually has: concatenated
// (pre-2026-09-20) and framed (post-fix), plus a damaged span.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

const (
	notifyTestTaskID    = "t-notify-fixture"
	notifyTestEventName = "failed"
	notifyTestTimeout   = 30 * time.Second
	notifyTestSinkName  = "events.jsonl"
)

// notifyFixturePayload builds one delivered payload of the shape
// internal/tools marshals.
func notifyFixturePayload(t *testing.T, event, taskID, ts string) string {
	t.Helper()
	body := map[string]any{
		"event":           event,
		"previous_status": "in_progress",
		"task_id":         taskID,
		"status":          event,
		"summary":         "notify fixture",
		"result":          "fixture result digest",
		"timestamp":       ts,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal fixture payload: %v", err)
	}
	return string(raw)
}

// notifyFixtureSink writes a CONCATENATED sink (the pre-fix shape, no
// newlines at all) so every binary-level test also exercises the framing the
// line-oriented reader gets wrong.
func notifyFixtureSink(t *testing.T, events ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), notifyTestSinkName)
	var b strings.Builder
	for _, e := range events {
		b.WriteString(notifyFixturePayload(t, e, notifyTestTaskID, "2026-10-01T05:00:00+08:00"))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write fixture sink: %v", err)
	}
	return path
}

// runNotifyBinary drives the REAL binary with an isolated environment: a
// per-test HOME, a settings file that configures NO notify target (so the
// operator's sink is never in play), and no ambient GOPI_TASK_NOTIFY_*.
//
// The argv it hands the binary is an ARGV LITERAL with the verb FIRST, exactly
// the shape the G7 census's `test_drive` column re-derives
// (cmd/agent/cli_verb_census_test.go, censusArgvLiteralDrives). Spelling it as
// the literal rather than as the constant is deliberate: this is the census's
// own evidence shape, so the `notify` row's class is earned by THIS line rather
// than asserted by a reader.
func runNotifyBinary(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	// This is a real argv literal with the verb FIRST — the shape the G7 census's
	// `test_drive` column re-derives (cmd/agent/cli_verb_census_test.go,
	// censusArgvLiteralDrives), here handing it to exec.Command below: {CliVerbNotify, …}
	argv := append([]string{CliVerbNotify}, args...)
	return runNotifyArgv(t, argv, args...)
}

// runNotifyArgv runs an explicit argv, and takes `args` too so the timeout
// message names the real arguments. The literal is built by the entry point
// above; this is where the slice reaches exec.Command, so a caller that wants a
// different literal (the help fingerprint, say) hands one over without a second
// subprocess plumbing.
func runNotifyArgv(t *testing.T, argv []string, args ...string) (string, string, error) {
	t.Helper()
	bin := testutil.GopiBin(t)
	home := t.TempDir()
	settings := filepath.Join(home, "settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	cmd := exec.Command(bin, familyAddressArgs(argv)...)
	cmd.Env = append(notifyScrubbedEnv(),
		"GOPI_HOME="+home,
		"GOPI_SETTINGS="+settings,
		"GOPI_TASK_NOTIFY_CMD=",
		"GOPI_TASK_NOTIFY_WEBHOOK=",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-time.After(notifyTestTimeout):
		_ = cmd.Process.Kill() //stylegate:ignore kills the notify CLI child after its printed record was compared
		t.Fatalf("gopi notify %v did not return within %s — a route that falls through to the agent loop hangs here",
			args, notifyTestTimeout)
		return "", "", nil
	}
}

// TestNotifyEventsReadsAConcatenatedSink is the end-to-end half of the reader's
// regression: through a shell, on a sink with NO newlines, all three events
// must come out. A consumer that read this file line-by-line would report one.
func TestNotifyEventsReadsAConcatenatedSink(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed", "in_progress", notifyTestEventName)
	stdout, stderr, err := runNotifyBinary(t, "events", "--sink", sink)
	if err != nil {
		t.Fatalf("gopi notify events: %v\nstderr: %s", err, stderr)
	}
	for _, want := range []string{"claimed", "in_progress", notifyTestEventName} {
		if !strings.Contains(stdout, want) {
			t.Errorf("gopi notify events output is missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, notifyTestTaskID) {
		t.Errorf("output is missing the task id:\n%s", stdout)
	}
}

// TestNotifyEventsJSONIsOneObjectPerLine pins the machine shape: --json must
// emit the payloads as delimited JSON so a downstream `while read` loop gets
// one event per iteration — the exact consumption the concatenated sink broke.
func TestNotifyEventsJSONIsOneObjectPerLine(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed", "in_progress", notifyTestEventName)
	stdout, stderr, err := runNotifyBinary(t, "events", "--sink", sink, "--json")
	if err != nil {
		t.Fatalf("gopi notify events --json: %v\nstderr: %s", err, stderr)
	}
	lines := notifyNonEmptyLines(stdout)
	if len(lines) != 3 {
		t.Fatalf("--json emitted %d line(s), want 3 — one JSON object per line is the contract:\n%s", len(lines), stdout)
	}
	for i, l := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(l), &payload); err != nil {
			t.Errorf("--json line %d is not a standalone JSON object: %v\n%s", i, err, l)
		}
	}
}

// TestNotifyFailedNamesTheFailure is the consumer's answer to "which failures
// has nobody dealt with": the row is named, and the report carries its own
// honesty note about what the stream cannot see.
func TestNotifyFailedNamesTheFailure(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed", notifyTestEventName)
	stdout, stderr, err := runNotifyBinary(t, "failed", "--sink", sink)
	if err != nil {
		t.Fatalf("gopi notify failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, notifyTestTaskID) {
		t.Errorf("failed report does not name the row:\n%s", stdout)
	}
	if !strings.Contains(stdout, "STREAM's count") {
		t.Errorf("failed report does not state that it is the stream's count, not the ledger's:\n%s", stdout)
	}
}

// TestNotifyFailedClearsOnALaterSuccess: a row that failed and later completed
// is HANDLED, and reporting it would be the false alarm that teaches an
// operator to ignore the report.
func TestNotifyFailedClearsOnALaterSuccess(t *testing.T) {
	sink := notifyFixtureSink(t, notifyTestEventName, "completed")
	stdout, stderr, err := runNotifyBinary(t, "failed", "--sink", sink)
	if err != nil {
		t.Fatalf("gopi notify failed: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(stdout, notifyTestTaskID) {
		t.Errorf("a row that later completed is still reported as failed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "0 id(s)") {
		t.Errorf("the report does not state the zero count:\n%s", stdout)
	}
}

// TestNotifyCursorResumesWithoutDuplicating is the restart contract, at the
// CLI level: read with a cursor, append, read again — the second read must
// deliver exactly the appended records.
func TestNotifyCursorResumesWithoutDuplicating(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed")
	cursor := filepath.Join(t.TempDir(), "feed.cursor")

	first, stderr, err := runNotifyBinary(t, "events", "--sink", sink, "--json", "--cursor", cursor)
	if err != nil {
		t.Fatalf("first read: %v\nstderr: %s", err, stderr)
	}
	if len(notifyNonEmptyLines(first)) != 1 {
		t.Fatalf("first read emitted %d record(s), want 1:\n%s", len(notifyNonEmptyLines(first)), first)
	}
	if _, err := os.Stat(cursor); err != nil {
		t.Fatalf("no cursor was written at %s: %v", cursor, err)
	}
	appendFixtureRecord(t, sink, notifyFixturePayload(t, notifyTestEventName, notifyTestTaskID, "2026-10-01T06:00:00+08:00"))

	second, stderr, err := runNotifyBinary(t, "events", "--sink", sink, "--json", "--cursor", cursor)
	if err != nil {
		t.Fatalf("resumed read: %v\nstderr: %s", err, stderr)
	}
	lines := notifyNonEmptyLines(second)
	if len(lines) != 1 {
		t.Fatalf("resumed read emitted %d record(s), want exactly the 1 appended:\n%s", len(lines), second)
	}
	if !strings.Contains(lines[0], notifyTestEventName) {
		t.Errorf("resumed read delivered %q, want the appended %q", lines[0], notifyTestEventName)
	}
}

// TestNotifyRefusesFollowWithoutCursor is one of the three refusals: a follow
// loop with no resume point re-reads the whole sink every tick, which on the
// operator's 51 MB sink means it never reaches the new events. The refusal
// names the flag that fixes it.
func TestNotifyRefusesFollowWithoutCursor(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed")
	stdout, stderr, err := runNotifyBinary(t, "events", "--sink", sink, "--follow")
	if err == nil {
		t.Fatalf("--follow with no --cursor was accepted (stdout: %s)", stdout)
	}
	if !strings.Contains(stderr, notifyCursorFlag) {
		t.Errorf("the refusal does not name %s:\n%s", notifyCursorFlag, stderr)
	}
}

// TestNotifyRefusesAnUnparseableSince: --since accepts a duration or an
// RFC3339 instant. A silently ignored bound is a consumer reporting the wrong
// window and never knowing, so an unrecognised value is refused by name.
func TestNotifyRefusesAnUnparseableSince(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed")
	_, stderr, err := runNotifyBinary(t, "events", "--sink", sink, "--since", "last tuesday")
	if err == nil {
		t.Fatal("--since with an unparseable value was accepted — the window would be silently ignored")
	}
	if !strings.Contains(stderr, notifySinceFlag) {
		t.Errorf("the refusal does not name %s:\n%s", notifySinceFlag, stderr)
	}
}

// TestNotifySinkReportsProvenance: the sink question is answered with HOW it
// was decided, because a path recognised inside an arbitrary shell command and
// a path the caller typed have different trust.
func TestNotifySinkReportsProvenance(t *testing.T) {
	sink := notifyFixtureSink(t, "claimed")
	stdout, stderr, err := runNotifyBinary(t, "sink", "--sink", sink)
	if err != nil {
		t.Fatalf("gopi notify sink: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, sink) {
		t.Errorf("sink report does not name the path:\n%s", stdout)
	}
	if !strings.Contains(stdout, "decided by") {
		t.Errorf("sink report does not state which channel decided the path:\n%s", stdout)
	}
}

// TestNotifyRefusesWhenNoSinkResolves: with notify configured to a command that
// names no local file and no --sink given, the verb must REFUSE — reading
// nothing and exiting 0 would be the "no events" lie a consumer cannot detect.
func TestNotifyRefusesWhenNoSinkResolves(t *testing.T) {
	_, stderr, err := runNotifyBinary(t, "events")
	if err == nil {
		t.Fatal("events with no sink and no configured file-naming command exited 0 — that reads as 'no events'")
	}
	if !strings.Contains(stderr, notifySinkFlag) {
		t.Errorf("the refusal does not name %s:\n%s", notifySinkFlag, stderr)
	}
}

// TestNotifyUsageClaimsTheIdleWakeItNowHas pins the ONE sentence this feature
// exists to make true. The deliver face's help used to say the console does NOT
// wake for a delivery — a claim that became FALSE the moment the idle poll was
// wired (internal/agent/session_inbox.go), and a false sentence in a help page
// is worse than a missing one: an operator who reads it stops believing the
// feature can work, and the reason they would look for is not there.
//
// Both the page and the delivery's own output are checked, because they are two
// separate renderings of one claim: a reader who runs the verb never sees the
// help page.
func TestNotifyUsageClaimsTheIdleWakeItNowHas(t *testing.T) {
	const (
		// The claim, as the operator reads it: the IDLE console wakes.
		wakeClaim = "THE CONSOLE WAKES FOR IT"
		// The old sentence, which must be gone from BOTH surfaces.
		staleNoWake = "does NOT wake"
		// The busy half of the claim, which must ALSO be stated: a delivery
		// never interrupts a turn in flight, and a reader who is told only
		// "the console wakes" would expect the wrong thing about a busy run.
		// Matched as the sentence the page actually carries, not a paraphrase —
		// a case that lower-cased its expectation would stop noticing a page
		// that reflowed the phrase across two lines.
		busyClaim = "is never interrupted"
	)
	for _, surface := range []struct {
		name string
		text string
	}{
		{"the help page", notifyUsage},
		{"the delivery's own output", notifyDeliverWakeNote},
	} {
		if !strings.Contains(surface.text, wakeClaim) && !strings.Contains(surface.text, "wakes for it") {
			t.Errorf("%s does not claim that an IDLE console wakes for a delivery:\n%s", surface.name, surface.text)
		}
		if strings.Contains(surface.text, staleNoWake) {
			t.Errorf("%s still claims the console does not wake for a delivery:\n%s", surface.name, surface.text)
		}
	}
	if !strings.Contains(notifyUsage, busyClaim) {
		t.Errorf("the help page does not state that a turn in flight is never interrupted:\n%s", notifyUsage)
	}
}

// TestNotifyHelpFingerprint pins the first line: the CLI contract suite asserts
// this string, and a reworded usage must not leave a stale fingerprint behind.
func TestNotifyHelpFingerprint(t *testing.T) {
	stdout, stderr, err := runNotifyBinary(t, "-h")
	if err != nil {
		t.Fatalf("gopi notify -h: %v\nstderr: %s", err, stderr)
	}
	if !strings.HasPrefix(stdout, notifyUsageHead) {
		t.Errorf("gopi notify -h does not start with the fingerprint %q:\n%s", notifyUsageHead, stdout)
	}
	// All three routes must be described: a help page that only sells its own
	// command is how the wrong subscription route gets chosen.
	for _, route := range []string{"FAN-OUT", "THIS VERB", "READ THE FILE"} {
		if !strings.Contains(stdout, route) {
			t.Errorf("the help does not describe the %s route:\n%s", route, stdout)
		}
	}
}

// TestNotifyUsageIsDottedForEverySubVerb is the family-completeness guard: an
// advertised sub-verb must dispatch (a completion entry for a dead word is
// worse than none, and a help index missing one hides it).
func TestNotifySubVerbNamesAreDispatched(t *testing.T) {
	src, err := os.ReadFile("cli_notify.go")
	if err != nil {
		t.Fatalf("read cli_notify.go: %v", err)
	}
	for _, sub := range notifySubcommandNames {
		if !strings.Contains(string(src), `"`+sub+`"`) && !strings.Contains(string(src), sub+"Verb") {
			t.Errorf("sub-verb %q has no dispatch owner in cli_notify.go", sub)
		}
		if !strings.Contains(notifyUsage, "gopi notify "+sub) {
			t.Errorf("the usage text does not advertise `gopi notify %s`", sub)
		}
	}
}

// notifyScrubbedEnv is the parent environment with every GOPI_* (and the
// agent-session signature) removed. It is the same guarantee testutil.MockEnv
// makes — a leaked real endpoint or the OPERATOR'S OWN GOPI_HOME must never
// reach a test subprocess — but without pinning a mock model endpoint, because
// this verb runs no model and must not need one. The operator's sink is the
// thing being protected: an inherited GOPI_TASK_NOTIFY_CMD would point the
// fixture's gopi at /home/nick/.gopi/notify/task-events.jsonl, so it is stripped here
// AND overwritten explicitly by the caller.
func notifyScrubbedEnv() []string {
	current := os.Environ()
	out := make([]string, 0, len(current))
	for _, kv := range current {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "GOPI_") || key == hermesSessionIDEnvName {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// hermesSessionIDEnvName is the agent-session signature testutil.MockEnv strips
// for the same reason. Spelled locally rather than imported so this file does
// not drag the mock-run machinery in for one variable name.
const hermesSessionIDEnvName = config.HermesSessionIDEnv

// notifyNonEmptyLines returns the output's non-blank lines.
func notifyNonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// appendFixtureRecord appends one already-marshalled record to a sink, the way
// the producer's `cat >> FILE` does.
func appendFixtureRecord(t *testing.T, path, record string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open sink for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(record + "\n"); err != nil {
		t.Fatalf("append record: %v", err)
	}
}
