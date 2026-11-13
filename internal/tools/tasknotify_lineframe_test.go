package tools

// Line-framing regression for the cmd notification target.
//
// The bug (measured on the operator's sink 2026-09-20): deliverTaskNotify
// marshalled the payload and handed the bare bytes to `sh -c` on stdin, so the
// documented sink configuration — notify.cmd = `cat >> …/task-events.jsonl` —
// appended object after object with no line terminator: 135,159 events under a
// single '\n', `wc -l` = 1, and every line-oriented reader (tail/jq/read/wc)
// silently wrong. The producer frames; the consumer never patches.
//
// The fixture below is the REAL documented command shape (`cat >> FILE` — no
// `&& echo` repair bolt-on, which is exactly what would hide the defect).

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// taskNotifyFrameSinkName is the sink file the fixture command appends to.
	taskNotifyFrameSinkName = "task-events.jsonl"
	// taskNotifyFrameSinkCmd is the DOCUMENTED sink configuration: plain append,
	// no terminator of its own. A fixture that added `&& echo` would mask the bug.
	taskNotifyFrameSinkCmd = "cat >> "
	// taskNotifyFrameEvents is the number of transitions each framing test fires.
	taskNotifyFrameEvents = 2
)

// taskNotifyFrameSetup points GOPI_MEMORY_DB at a per-test DB and the cmd target
// at a per-test sink using the real append-only command shape. The suite TestMain
// pins GOPI_SETTINGS at an empty sandbox (testutil.PinSuiteForTestMain), so the
// durable notify layer cannot reach the operator's live sink.
// taskNotifyFrameSetup points the test at a PROFILE-CORRECT sandbox (GOPI_HOME,
// so the ledger is that profile's own — see taskNotifyTestSetup) and the cmd
// target at a per-test sink using the real append-only command shape.
func taskNotifyFrameSetup(t *testing.T) (sink, taskID string) {
	t.Helper()
	taskNotifyProfileSandbox(t)
	sink = filepath.Join(t.TempDir(), taskNotifyFrameSinkName)
	t.Setenv(taskNotifyCmdEnv, taskNotifyFrameSinkCmd+sink)
	t.Setenv(taskNotifyWebhookEnv, "")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "notify line framing", Payload: "framing briefing"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return sink, taskNotifyExtractID(t, out)
}

// TestTaskNotifyCmdSinkIsLineFramed is the regression guard for the defect: two
// consecutive deliveries must land as two self-contained lines, each of which
// json.Unmarshal consumes on its own — and no delivery may leave a blank line
// behind for the next one to collide with.
func TestTaskNotifyCmdSinkIsLineFramed(t *testing.T) {
	sink, taskID := taskNotifyFrameSetup(t)

	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusCompleted); err != nil {
		t.Fatalf("completed: %v", err)
	}

	data, err := os.ReadFile(sink)
	if err != nil {
		t.Fatalf("read sink: %v", err)
	}
	text := string(data)

	// A reader that trusts `wc -l` counts terminators; every appended record must
	// contribute exactly one.
	if got := strings.Count(text, "\n"); got != taskNotifyFrameEvents {
		t.Fatalf("sink holds %d line terminators for %d events — line-oriented readers (wc -l/jq/tail/read) misread it:\n%q",
			got, taskNotifyFrameEvents, text)
	}
	// No empty record: the NEXT append must start a fresh line, not a blank one.
	if strings.Contains(text, "\n\n") {
		t.Errorf("sink carries an empty line — framing is not idempotent:\n%q", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Errorf("sink's last record is unterminated — an appending consumer would join the next event to it:\n%q", text)
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != taskNotifyFrameEvents {
		t.Fatalf("want %d lines, got %d:\n%q", taskNotifyFrameEvents, len(lines), text)
	}
	for i, line := range lines {
		var p taskNotifyPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("line %d is not independently parseable (%v): %q", i+1, err, line)
		}
		if p.TaskID != taskID {
			t.Errorf("line %d: task_id = %q, want %q", i+1, p.TaskID, taskID)
		}
	}
}

// TestTaskNotifyTargetsShareOneJSONBody proves the two delivery targets are
// framed for their own transport: the webhook carries the marshalled JSON
// byte-for-byte (no terminator — an HTTP body is not a line), while the cmd
// target carries the SAME bytes plus exactly one '\n'.
func TestTaskNotifyTargetsShareOneJSONBody(t *testing.T) {
	received := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore reads the line-frame request body the fixture itself sent
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink, taskID := taskNotifyFrameSetup(t)
	t.Setenv(taskNotifyWebhookEnv, srv.URL)

	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}

	hooked := <-received
	if !json.Valid(hooked) {
		t.Fatalf("webhook body is not valid JSON: %q", hooked)
	}
	if bytes.Contains(hooked, []byte("\n")) {
		t.Errorf("webhook body gained a line terminator — an HTTP payload is not a line: %q", hooked)
	}

	data, err := os.ReadFile(sink)
	if err != nil {
		t.Fatalf("read sink: %v", err)
	}
	if !bytes.Equal([]byte(strings.TrimSuffix(string(data), "\n")), hooked) {
		t.Errorf("cmd stdin is not the webhook body plus one terminator:\n cmd=%q\nwebhook=%q", data, hooked)
	}
}
