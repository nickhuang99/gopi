package tools

// Regression tests for the notify payload's resume witness (epic: notify
// payload carries resumed_from).
//
// THE DEFECT THESE PIN. `~/.gopi/notify/task-events.jsonl` carried, for a
// row's ordinary completion and for its RESUME-close completion, the SAME
// five fields in the same order (`event`/`previous_status`/`task_id`/
// `status`/`summary`) — both transitions were in_progress → completed. A
// subscriber therefore could not tell "this ran to its end" from "this was
// re-dispatched and closed after an earlier failure", although the door had
// known the answer BEFORE the spawn (tools.TaskRedispatchResumeFrom) and had
// already handed it to the executor child.
//
// THREE ARMS, one per claim:
//
//	TestTaskNotifyPayloadOmitsResumedFromOnFirstRun   — an ordinary run adds
//	    ZERO bytes: the key is absent from the wire, not empty in it
//	    (Rule 15a: `omitempty` is the contract).
//	TestTaskNotifyPayloadCarriesResumedFromOnRedispatch — a declared resume
//	    reaches the wire, with the declared session id as its value.
//	TestTaskNotifyResumedFromAddsOnlyItsOwnKey — the byte-level claim: the two
//	    payloads differ by exactly the one key, so no existing consumer's
//	    parsing (or byte comparison) changes.
//
// The fourth, TestTaskNotifyResumedFromJSONKeyMatchesTheStructTag, is the
// Rule 1/13/14 pin: the wire spelling lives in a named constant AND in a
// struct tag, and a tag renamed alone would make the emitted JSON name a field
// the constant (and every reader of it) does not.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testTaskNotifyResumedFromSession is the declaration the resumed arm hands the
// notify layer. It is a fixture value, not a real session id: what is under
// test is the FORWARDING, and a fabricated id makes a false positive (a value
// produced by some other path) impossible to misread as a pass.
const testTaskNotifyResumedFromSession = "probe-resume-session-77c1"

// testTaskNotifyWrapperID marks this process as a `task run` child in the
// resumed arm — the gate taskRunResumeWitness applies before it will report a
// declaration at all.
const testTaskNotifyWrapperID = "probe-wrapper-row"

// testTaskNotifyPlainWire returns the last payload line the fixture delivered,
// as raw JSON (the sink reads bytes, so the wire is what must be checked).
func testTaskNotifyPlainWire(t *testing.T, log string) string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read notify log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("no payload delivered")
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		t.Fatal("last payload line is empty")
	}
	return last
}

// TestTaskNotifyPayloadOmitsResumedFromOnFirstRun is arm 1: the ordinary run.
// The assertion is on the WIRE BYTES, not on the Go struct — a field that only
// exists after unmarshalling proves nothing about what a consumer receives.
func TestTaskNotifyPayloadOmitsResumedFromOnFirstRun(t *testing.T) {
	log := taskNotifyTestSetup(t)
	// THE STEADY STATE, exactly: an ordinary task-run child DOES carry the
	// wrapper id (cmd/agent/cli_tasks.go stamps it on every spawn) and does NOT
	// carry a declaration. Setting the id here is what makes this a control
	// rather than a tautology: it proves the id alone cannot fake a resume.
	t.Setenv(TaskWrapperEnv, testTaskNotifyWrapperID)
	t.Setenv(TaskRunResumeFromEnv, "")

	taskNotifyDriveOneTransition(t)

	line := testTaskNotifyPlainWire(t, log)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("parse delivered payload %q: %v", line, err)
	}
	if v, present := wire[taskNotifyResumedFromJSONKey]; present {
		t.Fatalf("an ordinary first run delivered %q=%v — omitempty is the contract that keeps every non-resumed event byte-identical (a subscriber comparing bytes, or a sink rendering fixed columns, must not see this key at all)",
			taskNotifyResumedFromJSONKey, v)
	}
	// The absence must be the FIELD's, not the whole payload's: an empty line
	// or a dropped delivery would satisfy the check above for the wrong reason.
	if wire["event"] == nil || wire["task_id"] == nil {
		t.Fatalf("payload %q lacks event/task_id — the fixture delivered something other than a task transition, so the omission above proves nothing", line)
	}
}

// TestTaskNotifyPayloadCarriesResumedFromOnRedispatch is arm 2: the door
// declared a re-dispatch, so the event must name the session it continued.
func TestTaskNotifyPayloadCarriesResumedFromOnRedispatch(t *testing.T) {
	log := taskNotifyTestSetup(t)
	// ONLY the declaration, deliberately WITHOUT the wrapper id: that is the
	// `gopi worker --resume` face's child (`gopi session resume`, spawned by
	// workerResumeChild), which carries the declaration and no wrapper
	// identity. The witness must reach the wire there too.
	t.Setenv(TaskWrapperEnv, "")
	t.Setenv(TaskRunResumeFromEnv, testTaskNotifyResumedFromSession)

	taskID := taskNotifyDriveOneTransition(t)

	line := testTaskNotifyPlainWire(t, log)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("parse delivered payload %q: %v", line, err)
	}
	got, present := wire[taskNotifyResumedFromJSONKey]
	if !present {
		t.Fatalf("a re-dispatched run delivered %q with NO %q key — this is exactly the defect: its close is byte-indistinguishable from an ordinary one (payload: %s)",
			taskID, taskNotifyResumedFromJSONKey, line)
	}
	if got != testTaskNotifyResumedFromSession {
		t.Fatalf("payload[%q] = %v, want the door's declaration %q — the field must forward the value decided BEFORE the spawn, not a re-derivation",
			taskNotifyResumedFromJSONKey, got, testTaskNotifyResumedFromSession)
	}
}

// TestTaskNotifyResumedFromAddsOnlyItsOwnKey is arm 3, the byte-level claim:
// removing the one key turns the resumed payload back into the ordinary one,
// byte for byte. This is what "no impact on ordinary events" MEANS — a
// consumer that slices, hashes or diffs payloads sees one added member and
// nothing else (no reordering, no re-quoting, no changed spacing).
func TestTaskNotifyResumedFromAddsOnlyItsOwnKey(t *testing.T) {
	t.Setenv(TaskWrapperEnv, "") // ordinary run, no declaration
	plain, err := json.Marshal(taskNotifyPayload{
		Event:          TaskStatusCompleted,
		PreviousStatus: TaskStatusInProgress,
		TaskID:         "01a190b4-3da1-7175-95ea-4007aac3913e",
		Status:         TaskStatusCompleted,
		Summary:        "byte-shape probe",
		Timestamp:      "2026-10-31T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}

	t.Setenv(TaskRunResumeFromEnv, testTaskNotifyResumedFromSession) // re-dispatched run
	resumed, err := json.Marshal(taskNotifyPayload{
		Event:          TaskStatusCompleted,
		PreviousStatus: TaskStatusInProgress,
		TaskID:         "01a190b4-3da1-7175-95ea-4007aac3913e",
		Status:         TaskStatusCompleted,
		Summary:        "byte-shape probe",
		Timestamp:      "2026-10-31T00:00:00Z",
		ResumedFrom:    taskRunResumeWitness(),
	})
	if err != nil {
		t.Fatalf("marshal resumed: %v", err)
	}

	if string(plain) == string(resumed) {
		t.Fatalf("the resumed payload is byte-identical to the plain one (%s) — the declaration never reached the wire", resumed)
	}
	// ResumedFrom is the LAST field of the struct, so omitempty appends it as
	// one trailing member: `…,"resumed_from":"<id>"}`. Cut exactly that and
	// the remaining bytes must equal the plain payload.
	quoted, err := json.Marshal(testTaskNotifyResumedFromSession)
	if err != nil {
		t.Fatalf("marshal the fixture id: %v", err)
	}
	suffix := `,"` + taskNotifyResumedFromJSONKey + `":` + string(quoted) + `}`
	if !strings.HasSuffix(string(resumed), suffix) {
		t.Fatalf("resumed payload %s does not end with the appended member %s — either the field moved off the struct's last position or the wire spelling drifted from the constant", resumed, suffix)
	}
	stripped := strings.TrimSuffix(string(resumed), suffix) + "}"
	if stripped != string(plain) {
		t.Fatalf("removing %q did not restore the plain payload:\n plain:    %s\n stripped: %s", taskNotifyResumedFromJSONKey, plain, stripped)
	}
}

// TestTaskNotifyResumedFromJSONKeyMatchesTheStructTag is the Rule 1/13/14 pin,
// the shape taskNotifyLedgerJSONKey already uses: a Go struct tag cannot hold a
// constant, so the wire spelling exists twice by force and something has to
// hold the two in step. Without this, a tag renamed alone would emit a field
// the constant (and every reader of it) never sees — a field nobody reads is
// Rule 15's defect one language over.
func TestTaskNotifyResumedFromJSONKeyMatchesTheStructTag(t *testing.T) {
	src, err := os.ReadFile("tasknotify.go")
	if err != nil {
		t.Fatalf("read tasknotify.go: %v", err)
	}
	want := `json:"` + taskNotifyResumedFromJSONKey + `,omitempty"`
	if !strings.Contains(string(src), want) {
		t.Fatalf("tasknotify.go does not carry the tag %q: the constant taskNotifyResumedFromJSONKey and the struct tag have drifted", want)
	}
	// The constant must be the ONLY spelling in the struct declaration: a
	// literal `json:"resumed_from"` beside the constant would pass the check
	// above while the constant had no reader at all.
	if count := strings.Count(string(src), `json:"`+taskNotifyResumedFromJSONKey); count != 1 {
		t.Fatalf("tasknotify.go declares %q %d time(s), want exactly 1 — the wire spelling must have one declaration site", taskNotifyResumedFromJSONKey, count)
	}
}

// TestTaskRunResumeDeclarationIsStrippedFromShells pins the OTHER half of the
// claim: the declaration may only exist because a door put it there, so no
// shell gopi spawns may inherit it. Without the strip, a `bash` tool that
// re-launches gopi would announce a resume it is not part of — and a false
// witness is worse than an absent one (Rule 15).
func TestTaskRunResumeDeclarationIsStrippedFromShells(t *testing.T) {
	env := []string{
		TaskWrapperEnv + "=" + testTaskNotifyWrapperID,
		TaskRunResumeFromEnv + "=" + testTaskNotifyResumedFromSession,
		"PATH=/usr/bin",
	}
	stripped, removed := stripWrapperIdentity(env)
	for _, kv := range stripped {
		if strings.HasPrefix(kv, TaskRunResumeFromEnv+"=") {
			t.Fatalf("stripWrapperIdentity kept %s in a spawned shell's environment — a re-launched gopi would stamp somebody else's resume onto its own notify payload", TaskRunResumeFromEnv)
		}
	}
	found := false
	for _, name := range removed {
		if name == TaskRunResumeFromEnv {
			found = true
		}
	}
	if !found {
		t.Fatalf("stripWrapperIdentity removed %s but did not REPORT it (removed=%v) — a silent strip hides which identity was inherited", TaskRunResumeFromEnv, removed)
	}
}
