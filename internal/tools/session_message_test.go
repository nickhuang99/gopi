package tools

// session_message_test.go — the INBOUND half of `gopi notify --session …
// --message …` at the layer that owns it: the durable inbox file, the merge
// into the ONE notice queue, and the read semantics (delivered exactly once).
//
// WHY THESE CASES LIVE HERE AND NOT ONLY IN cmd/agent. The CLI case proves the
// verb reaches this seam; it cannot prove what the seam does under repetition
// (two deliveries, two boundaries, one process restarted), because that needs
// the queue itself, in-process. Every assertion below is about
// internal/tools' own contract:
//
//  1. DELIVERY IS DURABLE — the file is the record, and the reader is a
//     separate call that may happen in another process.
//  2. ONE TAKE POINT — an external message leaves through TakeJobNotices
//     (job.go), the same call a job-completion notice leaves through; it is
//     never a second queue.
//  3. AT-MOST-ONCE INJECTION — a message whose 'delivered' marker exists is
//     never merged again (the session's history already carries it).
//  4. JSON STREAM DECODER — a message containing a newline is ONE record, the
//     defect a line-oriented reader would produce.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionMessageTestDir is a scratch session directory: the inbox lives in
// <dir>/messages/inbox.jsonl, exactly as it does under a real session.
func sessionMessageTestDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// sessionMessageTestReset drains the process-global queue so a case starts
// from "nothing pending" (the same discipline job_notice_wake_test.go uses).
func sessionMessageTestReset(t *testing.T) {
	t.Helper()
	cleanupJobs(t)
	TakeJobNotices()
}

// TestSessionInboxDeliverWritesTheDurableRecord: the message reaches the FILE
// — that is what makes a delivery possible while the session is not running at
// all — and the record carries the text verbatim.
func TestSessionInboxDeliverWritesTheDurableRecord(t *testing.T) {
	dir := sessionMessageTestDir(t)
	del, err := SessionInboxDeliver(dir, "sess-fixture", "hello from outside", "")
	if err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}
	if del.ID == "" {
		t.Fatal("the delivery carries no id — nothing can address it (the delivered marker is keyed on it)")
	}
	if del.Path != SessionInboxPath(dir) {
		t.Errorf("delivery path = %q, want the session's own inbox %q", del.Path, SessionInboxPath(dir))
	}
	raw, err := os.ReadFile(del.Path)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if !strings.Contains(string(raw), "hello from outside") {
		t.Errorf("the inbox does not hold the message:\n%s", raw)
	}
	if !strings.Contains(string(raw), sessionMessageRecordMessage) {
		t.Errorf("the record does not name its kind (%q):\n%s", sessionMessageRecordMessage, raw)
	}
}

// TestSessionInboxLoadMergesIntoTheOneTakePoint is the constraint's own proof:
// after a load, the message is IN the queue TakeJobNotices drains — not in a
// queue of its own, not behind a second reader.
func TestSessionInboxLoadMergesIntoTheOneTakePoint(t *testing.T) {
	sessionMessageTestReset(t)
	dir := sessionMessageTestDir(t)
	del, err := SessionInboxDeliver(dir, "sess-fixture", "delivered once", "")
	if err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	got, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("SessionInboxLoad: %v", err)
	}
	if len(got) != 1 || got[0].ID != del.ID || got[0].Message != "delivered once" {
		t.Fatalf("SessionInboxLoad = %+v, want exactly the delivered message", got)
	}
	// THE ONE TAKE POINT: the message is visible to the level the REPL reads…
	if !JobNoticePending() {
		t.Fatal("the level is false after a load — the message would never be offered to the model")
	}
	// …and it leaves through the SAME call a job completion notice leaves
	// through, shaped as a notice with no job behind it.
	notices := TakeJobNotices()
	if len(notices) != 1 {
		t.Fatalf("TakeJobNotices = %+v, want exactly the external message (a second queue would leave this empty)", notices)
	}
	if notices[0].External == nil {
		t.Fatalf("the notice carries no External message: %+v", notices[0])
	}
	if notices[0].JobID != "" {
		t.Errorf("an external message named a job id (%q) — no job produced it", notices[0].JobID)
	}
	if notices[0].External.Message != "delivered once" {
		t.Errorf("the taken message = %q, want the delivered text", notices[0].External.Message)
	}
	if JobNoticePending() {
		t.Error("the level is still true after the take")
	}
	// Drained means drained: a second take returns nothing.
	if again := TakeJobNotices(); len(again) != 0 {
		t.Errorf("a second take returned %+v, want nothing", again)
	}
}

// TestSessionInboxLoadDeliversEachMessageOnce: the second load must return
// NOTHING (the delivered marker is the state that makes it true across
// processes), while the text stays on disk as the record that it happened.
func TestSessionInboxLoadDeliversEachMessageOnce(t *testing.T) {
	sessionMessageTestReset(t)
	dir := sessionMessageTestDir(t)
	if _, err := SessionInboxDeliver(dir, "sess-fixture", "only once", ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}
	if got, err := SessionInboxLoad(dir); err != nil || len(got) != 1 {
		t.Fatalf("first load = %+v, err = %v, want one message", got, err)
	}
	TakeJobNotices()

	second, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second load re-delivered %+v — a repeated instruction is worse than noise (it runs twice)", second)
	}
	if JobNoticePending() {
		t.Error("the second load raised the level for an already-delivered message")
	}
	// A message delivered AFTER the first load still gets through: the marker
	// keys the individual message, never "the file was read".
	if _, err := SessionInboxDeliver(dir, "sess-fixture", "only once too", ""); err != nil {
		t.Fatalf("second deliver: %v", err)
	}
	third, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if len(third) != 1 || third[0].Message != "only once too" {
		t.Fatalf("third load = %+v, want exactly the newly delivered message", third)
	}
	TakeJobNotices()
}

// TestSessionInboxRecordWithNewlineIsOneRecord pins constraint ③ on the read
// path: a message carrying an escaped newline is ONE record. A line-oriented
// reader returns a fragment per record (or a record per fragment) — the exact
// defect the operator's own 51 MB sink had before its framing fix.
func TestSessionInboxRecordWithNewlineIsOneRecord(t *testing.T) {
	sessionMessageTestReset(t)
	dir := sessionMessageTestDir(t)
	text := "first line\nsecond line"
	if _, err := SessionInboxDeliver(dir, "sess-fixture", text, ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}
	got, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("SessionInboxLoad: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("load returned %d message(s) — a two-line message is ONE record:\n%+v", len(got), got)
	}
	if got[0].Message != text {
		t.Errorf("message = %q, want %q (the newline round-tripped)", got[0].Message, text)
	}
	TakeJobNotices()
}

// TestSessionInboxLoadReportsDamageWithoutLosingTheGoodOnes: a torn tail is
// NAMED, and the messages before it are still delivered (Rule 15 — never a
// silent "no messages" that is really "I could not read it").
func TestSessionInboxLoadReportsDamageWithoutLosingTheGoodOnes(t *testing.T) {
	sessionMessageTestReset(t)
	dir := sessionMessageTestDir(t)
	if _, err := SessionInboxDeliver(dir, "sess-fixture", "good one", ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}
	f, err := os.OpenFile(SessionInboxPath(dir), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open inbox for damage: %v", err)
	}
	if _, err := f.WriteString("{\"kind\":\"message\",\"id\":\"torn"); err != nil {
		t.Fatalf("append damage: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got, loadErr := SessionInboxLoad(dir)
	if loadErr == nil {
		t.Fatal("a torn record was read as a clean inbox — the reader cannot distinguish 'nothing' from 'unreadable'")
	}
	if !strings.Contains(loadErr.Error(), SessionInboxPath(dir)) {
		t.Errorf("the damage report does not name the file: %v", loadErr)
	}
	if len(got) != 1 || got[0].Message != "good one" {
		t.Errorf("load with a torn tail = %+v, want the good message still delivered", got)
	}
	TakeJobNotices()
}

// TestSessionInboxDeliverRefusesAnEmptyMessage: the queue exists to carry text,
// so empty text is refused at the seam rather than injected as a blank
// instruction.
func TestSessionInboxDeliverRefusesAnEmptyMessage(t *testing.T) {
	dir := sessionMessageTestDir(t)
	if _, err := SessionInboxDeliver(dir, "sess-fixture", "   ", ""); err == nil {
		t.Fatal("an empty message was accepted")
	}
	if _, err := os.Stat(SessionInboxPath(dir)); !os.IsNotExist(err) {
		t.Errorf("a refused delivery touched the filesystem: %v", err)
	}
}

// TestSessionInboxLoadOnAMissingInboxIsNotAnError: a session nobody ever
// messaged has no inbox, and that must read as "nothing was delivered", not as
// a failure at every round boundary.
func TestSessionInboxLoadOnAMissingInboxIsNotAnError(t *testing.T) {
	dir := sessionMessageTestDir(t)
	got, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("SessionInboxLoad on a session with no inbox: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionInboxDirName)); !os.IsNotExist(err) {
		t.Errorf("the read created the inbox directory (a load must not write): %v", err)
	}
}

// TestSessionInboxTakeBudgetBoundsOneBoundary: a burst of deliveries must not
// hand the model one enormous round. The remainder stays pending — the level
// is still true — so the NEXT boundary takes the next slice.
func TestSessionInboxTakeBudgetBoundsOneBoundary(t *testing.T) {
	sessionMessageTestReset(t)
	dir := sessionMessageTestDir(t)
	total := sessionInboxTakeBudget + 1
	for i := 0; i < total; i++ {
		if _, err := SessionInboxDeliver(dir, "sess-fixture", "burst", ""); err != nil {
			t.Fatalf("deliver %d: %v", i, err)
		}
	}
	first, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(first) != sessionInboxTakeBudget {
		t.Fatalf("first load took %d message(s), want the budget %d", len(first), sessionInboxTakeBudget)
	}
	if !JobNoticePending() {
		t.Fatal("the level is false while messages remain pending — the next boundary would never take them")
	}
	TakeJobNotices()
	second, err := SessionInboxLoad(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(second) != total-sessionInboxTakeBudget {
		t.Fatalf("second load took %d, want the remainder %d", len(second), total-sessionInboxTakeBudget)
	}
	TakeJobNotices()
}

// TestSessionInboxRoundTripsASummaryThatCarriesEscapableCharacters is the
// INBOUND half of the `\u003e` defect (task 01a1a61b-9e80): the text a caller
// hands to SessionInboxDeliver must come back out of SessionInboxLoad
// BYTE-IDENTICAL, for the exact characters the notify producer's JSON wire
// format escapes.
//
// WHY IT IS NOT REDUNDANT WITH THE SHELL TEST. The shell test pins the SINK
// (scripts/notify/notify.sh's field() once-decodes the payload). This one pins
// the SEAM the sink's argv lands in, and the property it defends is the one
// that makes a decoder fix meaningful: the inbox layer does not transform the
// message. A `>` written on disk as `\u003e` and read back as `\u003e` would
// mean the VALUE was already escaped when it arrived — the sink's bug — but a
// `>` that came back as `\\u003e`, or lost its `\`, or was trimmed, would be a
// defect HERE, and the two failures are indistinguishable from the model's
// side. Measured 2026-11-04 on the reporting session: 78 literal `\u003e` in
// one PA session and 0 real `>` in the records notify delivered to its inbox.
//
// THE CASES are the classes encoding/json escapes (HTML escaping is on by
// default, so `<`, `>`, `&` leave the producer as `\u003c`, `\u003e`, `\u0026`),
// plus the two shapes a careless fix invents: a message that CONTAINS the
// literal six characters (it must stay literal — decoding twice would turn it
// into `>`), and a message carrying CJK plus a surrogate pair.
func TestSessionInboxRoundTripsASummaryThatCarriesEscapableCharacters(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{
			// The operator's own line: what the defect looked like in a console.
			name:    "comparison operators",
			message: "task event: summary=slack 13 > margin 12; x >= 3",
		},
		{
			name:    "the HTML trio the producer escapes",
			message: `dial tcp <edge IP>:443 i/o timeout & "quoted" done`,
		},
		{
			// A writer describing THIS defect types the escape itself. One
			// decode of the wire gives it back; two would rewrite the reader.
			name:    "a literal escape in the text stays literal",
			message: `the literal \u003e must survive as six characters`,
		},
		{
			name:    "cjk and a surrogate pair",
			message: "中文 x >= 3 😀 \\ backslash",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionMessageTestReset(t)
			dir := sessionMessageTestDir(t)
			del, err := SessionInboxDeliver(dir, "sess-fixture", tc.message, "")
			if err != nil {
				t.Fatalf("SessionInboxDeliver: %v", err)
			}
			// THE READER'S OWN PATH: one load, one merge, one take — what the
			// model actually receives at a round boundary.
			got, err := SessionInboxLoad(dir)
			if err != nil {
				t.Fatalf("SessionInboxLoad: %v", err)
			}
			if len(got) != 1 || got[0].ID != del.ID {
				t.Fatalf("SessionInboxLoad = %+v, want exactly the delivered message", got)
			}
			if got[0].Message != tc.message {
				t.Errorf("the message came back changed\n got: %q\nwant: %q", got[0].Message, tc.message)
			}
			// …and the ON-DISK record decodes to the same bytes, which is what
			// proves the value was never double-encoded on the way in.
			raw, err := os.ReadFile(del.Path)
			if err != nil {
				t.Fatalf("read inbox: %v", err)
			}
			recs, damaged := decodeSessionInboxForTest(t, raw)
			if damaged != nil {
				t.Fatalf("the inbox record is damaged: %v", damaged)
			}
			stored := ""
			for _, r := range recs {
				if r.Kind == sessionMessageRecordMessage && r.ID == del.ID {
					stored = r.Message
				}
			}
			if stored != tc.message {
				t.Errorf("the record on disk decodes to %q, want %q", stored, tc.message)
			}
			if strings.Contains(stored, `\\u003e`) {
				t.Errorf("the record on disk carries a DOUBLE-escaped sequence (%q) — that is the defect's disk signature", stored)
			}
			// the take point hands the same bytes on, so nothing downstream
			// re-transforms it either.
			notices := TakeJobNotices()
			if len(notices) != 1 || notices[0].External == nil {
				t.Fatalf("TakeJobNotices = %+v, want one external message", notices)
			}
			if notices[0].External.Message != tc.message {
				t.Errorf("the taken message = %q, want %q", notices[0].External.Message, tc.message)
			}
		})
	}
}

// decodeSessionInboxForTest runs the inbox's OWN record decoder over raw bytes
// — the same stream decoder the take point uses, so the assertion is about the
// file format rather than about the writer's in-memory value.
func decodeSessionInboxForTest(t *testing.T, raw []byte) ([]sessionMessageRecord, error) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var recs []sessionMessageRecord
	for {
		var rec sessionMessageRecord
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			return recs, nil
		}
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
}
