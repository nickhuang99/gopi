package tools

// notify_peer_test.go — the CROSS-PEER hop's own cases: the wire document, the
// peer's handler, the derived id and the two idempotence answers.
//
// WHAT ONLY THIS FILE CAN SHOW. The hop's transport is one ssh exec, so the
// seam that replaces it (notifyPeerExec, notify_peer.go) is exercised here with
// the PEER'S REAL HANDLER in-process — NotifyPeerServe reads the body this
// package built and writes the receipt the initiator decodes. That is the
// request/reply pair crossing a real boundary (bytes on a pipe), not a unit
// stub of either side, and it is the shape the dual-DB memory-sync E2E uses for
// the same reason. The one thing it does NOT cover is `ssh` itself being able
// to reach a host — that is the cross-machine acceptance, which no in-process
// case can stand in for (Rule 31).
//
// THE DOUBLE-INJECTION HALF uses the REAL take point: SessionInboxLoad is the
// function the round boundary calls, so "the model was handed this once" is
// measured on the same code path that would inject it twice.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	notifyPeerTestSession = "01a10f78-3977-767d-aaff-640a4f5d9ac2"
	notifyPeerTestPeer    = "hp"
	notifyPeerTestText    = "NOTIFY-PEER-2f19: delivered across hosts"
)

// notifyPeerTestProfile points the profile at a fresh home, declares the test
// peer, and returns the sessions root with one bare chat session in it — the
// layout whose directory IS its id.
func notifyPeerTestProfile(t *testing.T) (home, root, dir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv(config.MemoryPeersEnv, notifyPeerTestPeer)
	root = filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	dir = filepath.Join(root, notifyPeerTestSession)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the session dir: %v", err)
	}
	record := filepath.Join(dir, sessionlayout.RecordName)
	line := `{"type":"session_start","sessionId":"` + notifyPeerTestSession + `"}` + "\n"
	if err := os.WriteFile(record, []byte(line), 0o644); err != nil {
		t.Fatalf("write the session record: %v", err)
	}
	return home, root, dir
}

// peerInProcess runs the peer's REAL handler on the initiator's request and
// returns its receipt bytes — the shim that stands in for `ssh`.
func peerInProcess(t *testing.T) {
	t.Helper()
	notifyPeerExec = func(_ string, body []byte) ([]byte, error) {
		var out bytes.Buffer
		if err := NotifyPeerServe(bytes.NewReader(body), &out); err != nil {
			return out.Bytes(), err
		}
		return out.Bytes(), nil
	}
	t.Cleanup(func() { notifyPeerExec = notifyPeerSSH })
}

// notifyPeerInboxRecords reads the inbox back as parsed records, so a case can
// count the two kinds instead of pattern-matching the raw bytes.
func notifyPeerInboxRecords(t *testing.T, dir string) []sessionMessageRecord {
	t.Helper()
	raw, err := os.ReadFile(SessionInboxPath(dir))
	if err != nil {
		t.Fatalf("read the inbox back: %v", err)
	}
	var recs []sessionMessageRecord
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var r sessionMessageRecord
		if err := dec.Decode(&r); err != nil {
			break
		}
		recs = append(recs, r)
	}
	return recs
}

// TestNotifyPeerHopDeliversOnceUnderOneID is the hop's core contract, in the
// order the facts matter:
//
//  1. the first send APPENDS one message record, under the DERIVED id the
//     initiator printed;
//  2. the same send again appends NOTHING and says so (the re-run that follows
//     an ssh leg which died after the peer had already written);
//  3. the session TAKES it once through the real take point, and a later load
//     injects nothing (the model sees the text exactly once);
//  4. a send of text the session has ALREADY TAKEN is refused BY NAME — the one
//     case where re-appending would repeat an instruction.
func TestNotifyPeerHopDeliversOnceUnderOneID(t *testing.T) {
	peerInProcess(t)
	_, _, dir := notifyPeerTestProfile(t)

	first, err := NotifyPeerDeliver(notifyPeerTestPeer, notifyPeerTestSession, notifyPeerTestText, "")
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if !first.Recorded {
		t.Fatalf("the first delivery reports recorded=false (note %q) — nothing was appended", first.Note)
	}
	wantID := NotifyDeliveryID(notifyPeerTestSession, notifyPeerTestText, sessionMessageSource)
	if first.ID != wantID {
		t.Errorf("the record's id is %q, want the derived %q — the id the initiator prints must BE the id on the peer's disk",
			first.ID, wantID)
	}
	if first.Inbox != SessionInboxPath(dir) {
		t.Errorf("the receipt's inbox is %q, want %q", first.Inbox, SessionInboxPath(dir))
	}

	// THE RE-SEND: the same text, a second invocation, nothing appended.
	again, err := NotifyPeerDeliver(notifyPeerTestPeer, notifyPeerTestSession, notifyPeerTestText, "")
	if err != nil {
		t.Fatalf("the re-send must be answered, not refused: %v", err)
	}
	if again.Recorded {
		t.Errorf("the re-send reports recorded=true — the same text was appended twice")
	}
	if again.ID != first.ID {
		t.Errorf("the re-send reports id %q, want the first delivery's %q", again.ID, first.ID)
	}
	if again.Note == "" {
		t.Errorf("the re-send appended nothing and says nothing (Note empty) — the caller cannot tell a repeat from a fresh write")
	}
	recs := notifyPeerInboxRecords(t, dir)
	msgs := 0
	for _, r := range recs {
		if r.Kind == sessionMessageRecordMessage {
			msgs++
			if r.Message != notifyPeerTestText {
				t.Errorf("message record carries %q, want the delivered text verbatim", r.Message)
			}
		}
	}
	if msgs != 1 {
		t.Fatalf("the inbox holds %d message record(s), want exactly 1 — a duplicate injection is the failure this hop exists to prevent\n%v", msgs, recs)
	}

	// THE TAKE, through the ONE take point the round boundary uses.
	t.Cleanup(func() { TakeJobNotices() })
	got, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("SessionInboxLoad: %v", err)
	}
	if len(got) != 1 || got[0].Message != notifyPeerTestText {
		t.Fatalf("the round boundary merged %d message(s) (%v), want exactly the delivered one", len(got), got)
	}
	TakeJobNotices()
	if second, err := SessionInboxLoad(dir); err != nil {
		t.Fatalf("second SessionInboxLoad: %v", err)
	} else if len(second) != 0 {
		t.Errorf("a second load re-injected %d message(s) — the model would be told twice", len(second))
	}

	// THE TAKEN REFUSAL: the same text, already injected, is refused by name.
	_, err = NotifyPeerDeliver(notifyPeerTestPeer, notifyPeerTestSession, notifyPeerTestText, "")
	if err == nil {
		t.Fatal("a delivery of text the session has already TAKEN was accepted — re-sending it would repeat the instruction")
	}
	if !strings.Contains(err.Error(), "TAKEN") {
		t.Errorf("the refusal does not say the message was already taken:\n%v", err)
	}
	// AND NOTHING WAS APPENDED BY IT: the file is still one message + one marker.
	recs = notifyPeerInboxRecords(t, dir)
	markers := 0
	for _, r := range recs {
		if r.Kind == sessionMessageRecordDelivered {
			markers++
		}
	}
	if len(recs) != 2 || markers != 1 {
		t.Errorf("the refused delivery changed the inbox: %d record(s), %d marker(s), want 2 and 1\n%v", len(recs), markers, recs)
	}
}

// TestNotifyPeerRefusesAnUnknownSessionByNamingIt pins the refusal that must
// never be a success: the peer has no such session, and the message is not
// written anywhere.
func TestNotifyPeerRefusesAnUnknownSessionByNamingIt(t *testing.T) {
	peerInProcess(t)
	home, root, _ := notifyPeerTestProfile(t)

	_, err := NotifyPeerDeliver(notifyPeerTestPeer, "01a10f78-0000-0000-0000-000000000000", "hello", "")
	if err == nil {
		t.Fatal("the peer accepted a delivery for a session it does not have — an unknown session is never a silent success")
	}
	if !strings.Contains(err.Error(), "unknown session") {
		t.Errorf("the refusal does not name the failure:\n%v", err)
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("the refusal does not name the tree that was searched (%s):\n%v", root, err)
	}
	// NO INBOX ANYWHERE: not under the profile, not beside it.
	if found := notifyPeerFindInbox(home); found != "" {
		t.Errorf("a refused delivery created an inbox at %s", found)
	}
}

// TestNotifyPeerRefusesAnUnwrittenSessionAsNotReady is the SAME defect one face
// over (row 01a1ac2f-84c6): the peer's own resolver classified by
// sessionlayout.DirHasRecord alone, so a session DIRECTORY that exists with no
// record yet — the ordinary claimed-but-not-started window — was reported with
// the unknown-session sentence, which tells the sender to give up instead of to
// retry. The fix must (a) say not-ready and point at the retry, (b) keep the
// truly-absent id on "unknown session" (the case above), and (c) write nothing:
// a delivery into an unwritten session reaches nobody.
//
// The local DELIVER face (cmd/agent.resolveNotifySessionDir) is pinned by
// cmd/agent/cli_notify_deliver_notready_test.go; both faces share the SENTENCE,
// which lives in internal/sessionlayout so they cannot drift.
func TestNotifyPeerRefusesAnUnwrittenSessionAsNotReady(t *testing.T) {
	peerInProcess(t)
	home, root, _ := notifyPeerTestProfile(t)

	const unwritten = "01a10f78-3977-767d-aaff-000000000000"
	container := filepath.Join(root, unwritten)
	if err := os.MkdirAll(container, 0o755); err != nil {
		t.Fatalf("create the claimed-but-not-started container: %v", err)
	}

	_, err := NotifyPeerDeliver(notifyPeerTestPeer, unwritten, "hello", "")
	if err == nil {
		t.Fatal("the peer accepted a delivery into a session directory that holds no record — writing there reaches nobody")
	}
	msg := err.Error()
	if !strings.Contains(msg, "NOT READY YET") || !strings.Contains(strings.ToLower(msg), "retry") {
		t.Errorf("the refusal does not say NOT READY YET and point at a retry:\n%v", err)
	}
	if !strings.Contains(msg, container) || !strings.Contains(msg, sessionlayout.RecordName) {
		t.Errorf("the refusal does not name the directory (%s) and the record (%s) it searched:\n%v", container, sessionlayout.RecordName, err)
	}
	if strings.Contains(msg, "unknown session") {
		t.Errorf("an unwritten session was reported as an UNKNOWN SESSION — the state that wants a retry got the sentence for an id that never exists:\n%v", err)
	}
	// NOTHING WRITTEN, anywhere in the profile.
	if found := notifyPeerFindInbox(home); found != "" {
		t.Errorf("a refused delivery created an inbox at %s", found)
	}
}

// TestNotifyPeerRefusesBeforeItDials pins the two gates that run BEFORE the
// transport — an undeclared peer (topology) and an option-shaped alias
// (syntax). The transport is replaced with a function that fails the case if it
// is reached at all: these refusals cost no ssh, which is the property that
// makes them safe to hit.
func TestNotifyPeerRefusesBeforeItDials(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv(config.MemoryPeersEnv, notifyPeerTestPeer)
	dialed := false
	notifyPeerExec = func(_ string, _ []byte) ([]byte, error) {
		dialed = true
		return nil, nil
	}
	t.Cleanup(func() { notifyPeerExec = notifyPeerSSH })

	cases := []struct {
		name string
		peer string
		want string
	}{
		{"undeclared peer", "peer1", "not in the memory_peers whitelist"},
		{"option-shaped alias", "-oProxyCommand=sh", "invalid peer alias"},
		{"blank alias", "   ", "invalid peer alias"},
		{"alias carrying a colon", "hp:/tmp", "invalid peer alias"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NotifyPeerDeliver(tc.peer, notifyPeerTestSession, "hello", "")
			if err == nil {
				t.Fatalf("peer %q was accepted", tc.peer)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name %q:\n%v", tc.want, err)
			}
			if dialed {
				t.Errorf("peer %q reached the transport — a refusal that dials is a refusal that can hang", tc.peer)
			}
		})
	}
}

// TestNotifyPeerHandlerRefusesByFieldAndVersion pins the PEER side's own
// refusals: an unknown protocol version, a request missing either field, and a
// stdin that is not this protocol at all. Each names what the SENDER must fix,
// and none of them writes anything.
func TestNotifyPeerHandlerRefusesByFieldAndVersion(t *testing.T) {
	home, root, _ := notifyPeerTestProfile(t)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown version", `{"version":99,"session_id":"x","message":"y"}`, "request version 99"},
		{"no session", `{"version":1,"message":"y"}`, notifyPeerSessionField},
		{"no message", `{"version":1,"session_id":"x"}`, notifyPeerMessageField},
		{"unknown field", `{"version":1,"session_id":"x","message":"y","extra":1}`, "unknown field"},
		{"not json", `gopi notify --serve`, "cannot read the request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := NotifyPeerServe(strings.NewReader(tc.body), &out)
			if err == nil {
				t.Fatalf("the handler accepted %s (receipt %q)", tc.body, out.String())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name %q:\n%v", tc.want, err)
			}
			if out.Len() != 0 {
				t.Errorf("a refused request wrote a receipt:\n%s", out.String())
			}
			if found := notifyPeerFindInbox(home); found != "" {
				t.Errorf("a refused request created an inbox at %s (root %s)", found, root)
			}
		})
	}
}

// TestNotifyDeliveryIDIsStableAndFramed pins the id derivation itself: the same
// triple gives the same id on two hosts (that is the whole point — the printed
// id and the peer's record must agree), and a different message gives a
// different one.
func TestNotifyDeliveryIDIsStableAndFramed(t *testing.T) {
	a := NotifyDeliveryID("s1", "hello", "notify")
	if b := NotifyDeliveryID("s1", "hello", "notify"); a != b {
		t.Errorf("the same triple derived two ids: %q and %q", a, b)
	}
	if a == NotifyDeliveryID("s1", "hello!", "notify") {
		t.Error("two different messages derived the same id")
	}
	if a == NotifyDeliveryID("s1", "hello", "chat") {
		t.Error("two different sources derived the same id")
	}
	if a == NotifyDeliveryID("s2", "hello", "notify") {
		t.Error("two different sessions derived the same id")
	}
	if len(a) != sessionMessageIDHexLen {
		t.Errorf("the derived id is %d chars, want %d (the shape sessionMessageID mints)", len(a), sessionMessageIDHexLen)
	}
	// THE FRAMING: "ab"+"c" and "a"+"bc" must not collide, or a message could
	// be delivered as another one's repeat.
	if NotifyDeliveryID("ab", "c", "notify") == NotifyDeliveryID("a", "bc", "notify") {
		t.Error("length-framing failed: shifted fields derived the same id")
	}
}

// notifyPeerFindInbox returns the first inbox under dir, or "".
func notifyPeerFindInbox(dir string) string {
	var found string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error { //stylegate:ignore the walk's function error is handled by the walk itself; the collected file list is the assertion
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == sessionInboxFileName {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
