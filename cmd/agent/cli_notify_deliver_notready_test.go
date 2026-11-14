// cli_notify_deliver_notready_test.go — the "directory exists, record not
// written yet" state at the DELIVER seam: `gopi notify --session <uuid>` must
// say NOT READY and point at a retry, never "unknown session".
//
// WHAT WENT WRONG (measured 2026-11-05, row 01a1ac2f-84c6). A run's session
// directory is created when the run is CLAIMED and its session record is
// written a moment later, when the run STARTS. resolveNotifySessionDir used
// sessionlayout.DirHasRecord as its only test, so the operand fell through to
// gopilog.FindSession and its "no record" answer — and the face printed
//
//	unknown session "<uuid>" — no session record with that id under <root>
//
// which is the sentence for an id that will NEVER exist. The two states want
// opposite responses: an unknown id ends the delivery, an unwritten record
// wants the same bytes again in a few seconds. The measured cost is a
// coordination message to a just-claimed peer that was one retry away from
// being abandoned ("差一次就被放弃").
//
// THREE CASES, and the second and third are the ones that keep the fix from
// being a licence to deliver into the void or to change what already worked:
//
//	① directory present, NO record   → refused, and the refusal says "not ready"
//	                                   and says retry — and NOTHING is written;
//	② an id that names nothing at all → STILL "unknown session" (a relaxed test
//	                                   that always succeeded would be worse);
//	③ a READY session (its record on disk) → the delivery is unchanged: a
//	                                   `queued` receipt and the text on disk.
//
// The whole file drives the REAL binary against a REAL profile (Rule 31), so
// the resolution, the refusal text, the exit status and the file on disk are
// all the production path's own.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// notifyNotReadyUUID is the CLAIMED-but-not-started run: the container
	// directory exists under the sessions root and holds no record.
	notifyNotReadyUUID = "01a1ac2f-84c6-7135-afff-7227b3e86af7"
	// notifyNotReadyText is the delivered body. Distinctive, so that "the text
	// arrived" cannot be satisfied by anything else in the profile.
	notifyNotReadyText = "NOTIFY-NOT-READY-9d31: this must NOT be delivered into an unwritten session"
	// notifyNotReadyWant is the phrase the ① refusal must carry. It is the
	// ACTION ("retry"), not the cause alone: a refusal a reader cannot follow is
	// a dead end (Rule 15).
	notifyNotReadyWant = "NOT READY YET"
)

// notifyNotReadyProfile builds a profile whose sessions tree holds ONE empty
// session container — the claimed-but-not-started shape — and returns the
// sessions root with the environment that points a child process at it.
//
// The directory name is a real task-id shape, so nothing about the case
// depends on a convenience value; what makes it "not ready" is that NOTHING
// was written inside it.
func notifyNotReadyProfile(t *testing.T) (root, container string, env []string) {
	t.Helper()
	home := t.TempDir()
	settings := filepath.Join(home, "settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	root = filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	container = filepath.Join(root, notifyNotReadyUUID)
	if err := os.MkdirAll(container, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", container, err)
	}
	env = append(notifyScrubbedEnv(),
		"GOPI_HOME="+home,
		"GOPI_SETTINGS="+settings,
	)
	return root, container, env
}

// TestNotifyDeliverUnreadySessionDirSaysRetryNotUnknown is case ①: the empty
// directory is refused, the refusal names the state and the retry, it does NOT
// claim the id is unknown, and the tree it was handed is untouched.
func TestNotifyDeliverUnreadySessionDirSaysRetryNotUnknown(t *testing.T) {
	root, container, env := notifyNotReadyProfile(t)

	stdout, stderr, err := runNotifyDeliverArgv(t, env,
		notifySessionFlag, notifyNotReadyUUID, notifyMessageFlag, notifyNotReadyText)
	if err == nil {
		t.Fatalf("an unwritten session directory was accepted (stdout %q) — a delivery into it reaches nobody, so it must be a REFUSAL", stdout)
	}
	// THE REFUSAL NAMES THE STATE, the path it searched, and the record.
	if !strings.Contains(stderr, notifyNotReadyWant) {
		t.Errorf("the refusal does not say the session is not ready yet (want %q):\nstderr: %s", notifyNotReadyWant, stderr)
	}
	if !strings.Contains(stderr, container) {
		t.Errorf("the refusal does not name the directory it looked in (%s):\nstderr: %s", container, stderr)
	}
	if !strings.Contains(stderr, sessionlayout.RecordName) {
		t.Errorf("the refusal does not name the record it looked for (%s):\nstderr: %s", sessionlayout.RecordName, stderr)
	}
	// THE OTHER ACTION, STATED: retry.
	if !strings.Contains(strings.ToLower(stderr), "retry") {
		t.Errorf("the refusal does not point at the action (a retry):\nstderr: %s", stderr)
	}
	// AND IT MUST NOT ASSIGN THE WRONG CAUSE: "unknown session" ends a delivery
	// that a retry would have completed. That word is the defect.
	if strings.Contains(stderr, "unknown session") {
		t.Errorf("an unwritten session directory was reported as an UNKNOWN SESSION — the state that wants a retry was given the sentence for an id that never exists:\nstderr: %s", stderr)
	}
	// NOTHING WAS WRITTEN: not into the container, not to the sessions root.
	if _, statErr := os.Stat(filepath.Join(container, "messages", "inbox.jsonl")); statErr == nil {
		t.Errorf("a refused delivery created an inbox under the unwritten session dir %s", container)
	}
	if _, statErr := os.Stat(filepath.Join(root, "messages", "inbox.jsonl")); statErr == nil {
		t.Errorf("a refused delivery created an inbox at the sessions ROOT %s", root)
	}
	if strings.Contains(stdout, "queued:") {
		t.Errorf("a refused delivery printed a success line:\n%s", stdout)
	}
}

// TestNotifyDeliverAbsentSessionStillUnknown is case ②: relaxing the empty-dir
// rule must not relax the unknown-id rule. An operand naming no directory AND
// no record keeps the refusal it always had, with the tree it searched.
func TestNotifyDeliverAbsentSessionStillUnknown(t *testing.T) {
	root, container, env := notifyNotReadyProfile(t)

	const absent = "01a1ac2f-84c6-7135-afff-000000000000"
	stdout, stderr, err := runNotifyDeliverArgv(t, env,
		notifySessionFlag, absent, notifyMessageFlag, notifyNotReadyText)
	if err == nil {
		t.Fatalf("an unknown session was accepted (stdout %q) — a delivery that did not happen must not report success", stdout)
	}
	if !strings.Contains(stderr, "unknown session") {
		t.Errorf("the refusal does not name the condition (a relaxed rule must not swallow the truly-unknown id):\nstderr: %s", stderr)
	}
	if !strings.Contains(stderr, root) {
		t.Errorf("the refusal does not name the tree it searched (%s):\nstderr: %s", root, stderr)
	}
	if strings.Contains(stderr, notifyNotReadyWant) {
		t.Errorf("an absent id was reported as NOT READY — that tells the caller to retry something that will never appear:\nstderr: %s", stderr)
	}
	if strings.Contains(stdout, "queued:") {
		t.Errorf("a refused delivery printed a success line:\n%s", stdout)
	}
	// The empty container beside it is untouched by this case as well.
	if _, statErr := os.Stat(filepath.Join(container, "messages", "inbox.jsonl")); statErr == nil {
		t.Errorf("the refusal wrote into the empty container %s", container)
	}
}

// TestNotifyDeliverReadySessionUnchanged is case ③, the compatibility half: a
// session whose record IS on disk still resolves by its directory name and is
// delivered to, exactly as before. Without this arm the fix could "pass" by
// refusing more, which is the opposite defect.
func TestNotifyDeliverReadySessionUnchanged(t *testing.T) {
	root, container, env := notifyNotReadyProfile(t)
	// Write the record the claimed run had not written yet: the SAME directory,
	// one event later.
	record := filepath.Join(container, sessionlayout.RecordName)
	line := `{"type":"session_start","sessionId":"` + notifyNotReadyUUID + `"}` + "\n"
	if err := os.WriteFile(record, []byte(line), 0o600); err != nil {
		t.Fatalf("write the session record: %v", err)
	}
	wantInbox := filepath.Join(container, "messages", "inbox.jsonl")

	stdout, stderr, err := runNotifyDeliverArgv(t, env,
		notifySessionFlag, notifyNotReadyUUID, notifyMessageFlag, notifyNotReadyText)
	if err != nil {
		t.Fatalf("a READY session was refused: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "queued:") {
		t.Errorf("a ready session's delivery did not report queued:\n%s", stdout)
	}
	// Resolved by its DIRECTORY NAME — the bare-session layout whose directory
	// IS its id — never by a record search that happens to find it.
	if !strings.Contains(stdout, notifySessionByDirName) {
		t.Errorf("the receipt does not report the resolution used (%q):\n%s", notifySessionByDirName, stdout)
	}
	if !strings.Contains(stdout, "session dir: "+container+" ") {
		t.Errorf("the receipt does not name the session dir %s:\n%s", container, stdout)
	}
	// THE FILE ON DISK is the acceptance, not the sentence about it.
	raw, readErr := os.ReadFile(wantInbox)
	if readErr != nil {
		t.Fatalf("the delivery did not create %s: %v\nstdout: %s", wantInbox, readErr, stdout)
	}
	if !strings.Contains(string(raw), notifyNotReadyText) {
		t.Errorf("%s does not carry the delivered text:\n%s", wantInbox, raw)
	}
	// And the near-miss the whole epic is about: no stray inbox at the root.
	if _, statErr := os.Stat(filepath.Join(root, "messages", "inbox.jsonl")); statErr == nil {
		t.Errorf("a stray inbox exists at the sessions root %s — the root is not a session directory", root)
	}
}
