package main

// notify_deliver_multi_test.go — the BATCH face of `gopi notify`, and the
// cross-language pin that keeps scripts/notify/notify.sh able to call it.
//
// WHY BOTH HALVES LIVE IN ONE FILE. The flag name is spelled in three places
// that cannot see each other at compile time: internal/tools (the wire
// vocabulary), cmd/agent (the parser), and scripts/notify/notify.sh (the caller).
// The shell half is where the O(n) delivery shape actually lived, so a pin that
// only proved the Go parser works would leave the script free to drift onto a
// spelling no binary answers — and the failure mode of that drift is a LOST
// NOTIFICATION, not a slow one (the batch would be an argument error).
//
// The behaviour arm exists because the batch face is a SECOND delivery path into
// the same inbox, and two paths is exactly where a delivery rule drifts. It
// asserts the fact the sink depends on: ONE invocation delivers EVERY message to
// EVERY registered slot — which is what makes a peer backlog cost one process per
// batch instead of one per line per slot (task 01a14194).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// notifyMultiScriptFlag reads the flag the sink actually calls the reader with,
// out of the script's own readonly declaration. Reading it (rather than spelling
// it) is the point: a renamed constant must break this test, not silently stop
// being checked.
func notifyMultiScriptFlag(t *testing.T, body string) string {
	t.Helper()
	// The script declares it like every other name in this family (bare, and
	// pinned by the same reader the slot pin uses) — so it goes through
	// notifyScriptShellValue, which is the ONE reader for `readonly NAME=…`.
	if v := notifyScriptShellValue(t, body, "NOTIFY_DELIVER_MULTI_FLAG"); v != "" {
		return v
	}
	t.Fatalf("scripts/notify/notify.sh declares no `readonly NOTIFY_DELIVER_MULTI_FLAG=…` — the batch reader cannot be pinned")
	return ""
}

// TestNotifyScriptAgreesOnTheBatchFlag is the cross-language half: the spelling
// the SINK puts on the command line must be the one the CLI parses, both of which
// must be the tools package's single definition (Rule 1b/13/14).
func TestNotifyScriptAgreesOnTheBatchFlag(t *testing.T) {
	body, err := os.ReadFile(filepath.Clean(notifyScriptRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", notifyScriptRelPath, err)
	}
	text := string(body)

	scriptFlag := notifyMultiScriptFlag(t, text)
	if scriptFlag != tools.NotifyDeliverMultiFlag {
		t.Fatalf("notify.sh NOTIFY_DELIVER_MULTI_FLAG=%q, want %q (internal/tools.NotifyDeliverMultiFlag)",
			scriptFlag, tools.NotifyDeliverMultiFlag)
	}
	if notifyDeliverMultiFlag != tools.NotifyDeliverMultiFlag {
		t.Fatalf("cmd/agent notifyDeliverMultiFlag=%q, want %q", notifyDeliverMultiFlag, tools.NotifyDeliverMultiFlag)
	}
	// The script must actually CALL the reader with it. A declaration nothing
	// expands is a comment wearing a variable's name (the same defect the slot
	// limit's assertion catches) — and here it would mean every batch silently
	// took the per-line route the change exists to remove.
	if !strings.Contains(text, `"$NOTIFY_DELIVER_MULTI_FLAG"`) {
		t.Fatalf("notify.sh declares %s but never expands it on a command line — the batch route would never be taken", scriptFlag)
	}
}

// TestNotifyDeliverBatchReachesEveryMessageAndEverySlot drives the batch face
// against a real tree: two messages, two registered slots, and the assertion is
// the product — FOUR inbox records from ONE invocation.
//
// It is the property the shell sink buys its O(1) with, so it is asserted where
// it can be asserted cheaply and deterministically (the shell rig measures the
// process COUNT; this measures the deliveries those processes produce).
func TestNotifyDeliverBatchReachesEveryMessageAndEverySlot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	slotsDir := filepath.Join(home, ".gopi")

	const slotCount = 2
	var sessionDirs []string
	for i := 0; i < slotCount; i++ {
		id := "1111111" + strconv.Itoa(i) + "-2222-4333-8444-55555555555" + strconv.Itoa(i)
		dir := filepath.Join(slotsDir, "sessions", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir session %d: %v", i, err)
		}
		// A session DIRECTORY is the resolution's acceptance criterion: it holds
		// the session record (gopilog.FindSession's fast path).
		rec := `{"type":"session_start","session_id":"` + id + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(rec), 0o644); err != nil {
			t.Fatalf("write record %d: %v", i, err)
		}
		if err := os.WriteFile(ChatSessionSlotPath(slotsDir, i), []byte("42424"+strconv.Itoa(i)+":"+dir+"\n"), 0o600); err != nil {
			t.Fatalf("write slot %d: %v", i, err)
		}
		sessionDirs = append(sessionDirs, dir)
	}

	// The batch rides stdin, as the sink sends it: a JSON array of messages.
	batch := `["first batch message","second batch message"]`
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		_, _ = w.WriteString(batch) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
		_ = w.Close()               //stylegate:ignore closes the fixture write end
	}()
	if err := notifyDeliverBatch(r); err != nil {
		t.Fatalf("notifyDeliverBatch: %v", err)
	}
	_ = r.Close() //stylegate:ignore closes the fixture read end

	// Every message must have reached EVERY session: slotCount x messages.
	for _, dir := range sessionDirs {
		raw, err := os.ReadFile(filepath.Join(dir, "messages", "inbox.jsonl"))
		if err != nil {
			t.Fatalf("read inbox in %s: %v", dir, err)
		}
		got := string(raw)
		for _, want := range []string{"first batch message", "second batch message"} {
			if !strings.Contains(got, want) {
				t.Errorf("inbox %s does not carry %q — the batch did not reach this slot:\n%s", dir, want, got)
			}
		}
		if n := strings.Count(got, `"kind":"message"`); n != 2 {
			t.Errorf("inbox %s holds %d message record(s), want 2 (one per batch entry)", dir, n)
		}
	}
}

// TestNotifyDeliverBatchRefusesAStdinThatIsNotABatch is the refusal arm: a
// payload that is not the array it must be delivers NOTHING and says why. It is
// the shape a truncated pipe or a shell that echoed first would produce, and the
// alternative — delivering whatever half parsed — would put partial text into a
// live console with no way to tell it was partial (Rule 15).
func TestNotifyDeliverBatchRefusesAStdinThatIsNotABatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".gopi"), 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		_, _ = w.WriteString(`{"not":"an array"}`) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
		_ = w.Close()                              //stylegate:ignore closes the fixture write end
	}()
	err = notifyDeliverBatch(r)
	_ = r.Close() //stylegate:ignore closes the fixture read end
	if err == nil {
		t.Fatal("notifyDeliverBatch accepted a payload that is not a JSON array — it must refuse by name")
	}
	if !strings.Contains(err.Error(), "NOTHING was delivered") {
		t.Fatalf("the refusal does not state that nothing was delivered: %v", err)
	}
}
