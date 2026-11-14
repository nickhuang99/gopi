// cli_notify_deliver_runlayout_test.go — the TASK-RUN layout at the DELIVER
// seam: `gopi notify --session <run-uuid>` must land in the CONTAINER the run
// belongs to, and an id that resolves nowhere must be refused by name.
//
// WHAT WENT WRONG (measured 2026-10-13, task 01a1330e-dfe3). The operand of
// `--session` here is a RUN uuid — the value `gopi task show` prints as
// `session_log` and the one a PA naturally copies — whose record lives at
//
//	<sessions>/<task-id>/<run-uuid>.jsonl
//
// gopilog.FindSession resolves it exactly (that half is fine). The deliver face
// then took filepath.Dir(path) — already the container <sessions>/<task-id> —
// and peeled ONE MORE level whenever `filepath.Base(path) != session.jsonl`,
// which is true for BOTH non-bare layouts: the legacy flat record
// (<root>/<id>.jsonl) and the task-run record. Only the flat case survives that
// peel (its dir IS the root). For a run uuid the directory was computed as the
// sessions ROOT, so the message was appended to
//
//	<sessions>/messages/inbox.jsonl      ← read by NO session
//
// while the verb exited 0 and printed `queued … (resolved as a session record
// with that id)` — a silent failure (Rule 15), and the worst kind: the sender
// believes it was received.
//
// WHY THIS FILE AND NOT THE EXISTING DELIVER CASE. TestNotifyDeliverReaches-
// ARunningChatSession addresses a BARE session dir (the name is the id), which
// is exactly the case the old peel handled correctly — it could not see this.
// The layout with two identities (task id and run uuid) is the one that needs
// its own case, and it is driven here through the REAL binary against a REAL
// profile, so the resolution, the printed receipt and the file on disk are all
// the production path's own (Rule 31).

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// notifyRunLayoutTaskID names the CONTAINER directory. A real task id shape
	// (the ledger's own spelling), so nothing about the case depends on a
	// convenience value.
	notifyRunLayoutTaskID = "01a1330e-dfe3-7130-8ddf-f0aa73f5a274"
	// notifyRunLayoutRunUUID names the RUN — the record's file name, and the
	// operand handed to --session. Distinct from the container name by
	// construction: that difference is the whole subject.
	notifyRunLayoutRunUUID = "5f0c1a92-6b7d-4e51-8a3c-1d9b7e40c2aa"
	// notifyRunLayoutText is the delivered body. Distinctive so that "the
	// message arrived" cannot be satisfied by anything else in the profile.
	notifyRunLayoutText = "NOTIFY-RUN-LAYOUT-4c7f: this must reach the task container inbox"
)

// taskRunLayoutProfile materialises a profile whose sessions tree holds ONE
// task-run record at the real layout — <home>/.gopi/sessions/<task-id>/<run-uuid>.jsonl
// — carrying a genuine session_start event for that run uuid. It returns the
// sessions root and the environment that points a child process at that
// profile.
//
// The record's body is the shared fixture (tests/fixtures/log-stats-raw), the
// same one every other session-layout case in this package builds from, so the
// file is a real session log and not a hand-written stub.
func taskRunLayoutProfile(t *testing.T) (root string, env []string) {
	t.Helper()
	home := t.TempDir()
	settings := filepath.Join(home, "settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	root = filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	container := filepath.Join(root, notifyRunLayoutTaskID)
	if err := os.MkdirAll(container, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", container, err)
	}
	record := filepath.Join(container, notifyRunLayoutRunUUID+".jsonl")
	copyFixture(t, filepath.Join("sessions", statsFixtureSessionID+".jsonl"), record)
	// The session_start event must carry THIS run's id: it is the identity
	// gopilog resolves the operand against (FileToID reads it first, the file's
	// own name only as the fallback).
	b, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	nl := indexByte(b, '\n')
	if nl < 0 {
		t.Fatalf("the fixture session has no newline-terminated first line")
	}
	first := strings.Replace(string(b[:nl]),
		`"sessionId":"`+statsFixtureSessionID+`"`,
		`"sessionId":"`+notifyRunLayoutRunUUID+`"`, 1)
	if err := os.WriteFile(record, append([]byte(first), b[nl:]...), 0o600); err != nil {
		t.Fatalf("write the record: %v", err)
	}
	env = append(notifyScrubbedEnv(),
		"GOPI_HOME="+home,
		"GOPI_SETTINGS="+settings,
	)
	return root, env
}

// TestNotifyDeliverRunUUIDLandsInTheContainerInbox is the regression: a RUN
// uuid resolves to the container directory that holds it, the message is
// appended to that container's inbox, and the receipt names that path.
//
// It asserts the outcome on DISK as well as in the receipt — the receipt alone
// is what made the defect invisible for a day (a `queued` line whose inbox was
// a file no session reads).
func TestNotifyDeliverRunUUIDLandsInTheContainerInbox(t *testing.T) {
	root, env := taskRunLayoutProfile(t)
	wantInbox := filepath.Join(root, notifyRunLayoutTaskID, "messages", "inbox.jsonl")

	stdout, stderr, err := runNotifyDeliverArgv(t, env, notifySessionFlag, notifyRunLayoutRunUUID, notifyMessageFlag, notifyRunLayoutText)
	if err != nil {
		t.Fatalf("gopi notify %s %s %s %q = %v\nstdout: %s\nstderr: %s",
			notifySessionFlag, notifyRunLayoutRunUUID, notifyMessageFlag, notifyRunLayoutText, err, stdout, stderr)
	}
	// THE CONTAINER, named as the session dir: <root>/<task-id>, never the root.
	if !strings.Contains(stdout, "session dir: "+filepath.Join(root, notifyRunLayoutTaskID)+" ") {
		t.Errorf("the receipt does not name the container %s as the session dir:\n%s",
			filepath.Join(root, notifyRunLayoutTaskID), stdout)
	}
	if strings.Contains(stdout, "session dir: "+root+" ") {
		t.Errorf("the session dir was peeled to the sessions ROOT — the message lands where no session reads (Rule 15):\n%s", stdout)
	}
	if !strings.Contains(stdout, wantInbox) {
		t.Errorf("the receipt does not name the inbox it wrote (%s):\n%s", wantInbox, stdout)
	}
	// THE FILE ON DISK is the acceptance, not the sentence about it.
	raw, err := os.ReadFile(wantInbox)
	if err != nil {
		t.Fatalf("the delivery did not create the container inbox %s: %v\nstdout: %s", wantInbox, err, stdout)
	}
	if !strings.Contains(string(raw), notifyRunLayoutText) {
		t.Errorf("the container inbox %s does not carry the delivered text:\n%s", wantInbox, raw)
	}
	// NOTHING WAS WRITTEN ONE LEVEL UP: <root>/messages/inbox.jsonl is the
	// landing site the defect produced — a file NO session reads. Asserting its
	// ABSENCE is what keeps the peel from coming back as a "harmless" extra
	// append once a message is delivered twice.
	stray := filepath.Join(root, "messages", "inbox.jsonl")
	if _, err := os.Stat(stray); err == nil {
		t.Errorf("a stray inbox exists at %s — the sessions root is not a session directory", stray)
	}
}

// TestNotifyDeliverUnknownRunUUIDIsRefusedByName pins the other half: an id
// that resolves nowhere under a sessions tree that DOES hold records exits
// non-zero, and the refusal names the directory that was searched (Rule 15 — a
// refusal a reader cannot follow is a dead end).
func TestNotifyDeliverUnknownRunUUIDIsRefusedByName(t *testing.T) {
	root, env := taskRunLayoutProfile(t)
	stdout, stderr, err := runNotifyDeliverArgv(t, env,
		notifySessionFlag, "01a1330e-dfe3-7130-8ddf-000000000000", notifyMessageFlag, notifyRunLayoutText)
	if err == nil {
		t.Fatalf("an unknown session was accepted (stdout %q) — a delivery that did not happen must not report success", stdout)
	}
	if !strings.Contains(stderr, "unknown session") {
		t.Errorf("the refusal does not name the condition:\nstderr: %s", stderr)
	}
	if !strings.Contains(stderr, root) {
		t.Errorf("the refusal does not name the tree it searched (%s):\nstderr: %s", root, stderr)
	}
	if strings.Contains(stdout, "queued:") {
		t.Errorf("a refused delivery printed a success line:\n%s", stdout)
	}
}
