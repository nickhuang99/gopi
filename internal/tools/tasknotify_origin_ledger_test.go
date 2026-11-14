package tools

// tasknotify_origin_ledger_test.go — the payload must say WHERE it came from
// when "where it came from" is not the profile's own ledger.
//
// THE MEASURED DEFECT (2026-10-23). gopi resolves the LEDGER and the NOTIFY
// TARGET through two INDEPENDENT channels: `--db` / GOPI_MEMORY_DB moves only
// the ledger, while config.ResolveNotify reads the PROFILE ROOT's
// settings.json ($GOPI_HOME/.gopi, else $HOME/.gopi). A run that isolates only
// its ledger therefore delivers into the OPERATOR's live sink, and the event it
// lands names a task id that exists in NO ledger the console can open. Two such
// rows reached the operator's console — `01a16515-dae5-76a2-88c9-c8f5a15a0c37`
// (from a probe's private ledger, seed 1792697293) and `probe-dead-holder`
// (from `probe/ledger.db` under session 01a16409) — and neither could be
// confirmed, reopened or reissued because there was nothing to open.
//
// THE FIRST FIX WAS HALF (2026-10-23 → 2026-11-04). It stamped those events with
// the emitting ledger and DELIVERED them anyway. Traceability does not make an
// alarm true: the reader still saw invented rows ("parent A"/"parent B" from a
// red proof's g5.db/g6.db) arrive as work in progress, and the shape is
// structural — every red-proof run of every row writes its own private ledger,
// so the leak is one broadcast group per lane per round, not an accident.
//
// SO THE SECOND FIX IS SUPPRESSION, and these arms test the gate:
//   - a PRIVATE ledger's event is not delivered at all (TestTaskNotifyForeignLedgerIsSuppressed);
//   - the profile's OWN ledger still delivers, byte for byte as before
//     (TestTaskNotifyPayloadOmitsTheOwnLedger);
//   - a GOPI_HOME-isolated sandbox is the profile's own ledger, so it still
//     delivers (TestTaskNotifySandboxProfileStillDelivers).
//
// The `ledger` field survives as the diagnostic's evidence, not as the fix.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestTaskNotifyForeignLedgerIsSuppressed replaces the old
// "names a foreign ledger" arm, and the replacement IS the fix.
//
// The stamp was the whole of the prior change: a private-ledger event named its
// ledger and was then DELIVERED to the operator's sink anyway. Naming a source
// the reader cannot open does not make the alarm true — it makes it traceable
// noise, and the noise is structural (every red-proof run of every row writes
// its own private ledger). So the arm that used to assert the stamp now asserts
// the suppression, and the delivery gate is the thing under test.
func TestTaskNotifyForeignLedgerIsSuppressed(t *testing.T) {
	log := taskNotifyTestSetup(t)
	// Re-introduce the FOREIGN shape on top of the profile-correct sandbox: the
	// ledger moves away from the profile, and nothing else does.
	foreign := filepath.Join(t.TempDir(), "private.db")
	t.Setenv(config.MemoryDBEnv, foreign)

	taskNotifyDriveOneTransition(t)

	if payloads := readNotifyLog(t, log); len(payloads) != 0 {
		t.Fatalf("a transition from the PRIVATE ledger %s delivered %d payload(s) to the configured target (%+v) — its events belong to that ledger's own sink; the operator reads them as work in progress on rows that exist in no ledger they can open",
			foreign, len(payloads), payloads)
	}
}

// TestTaskNotifyPayloadOmitsTheOwnLedger is the control arm: the ordinary run
// (the profile's own ledger) must NOT carry the stamp, or every consumer sees a
// field that means nothing and the operator learns to ignore it.
func TestTaskNotifyPayloadOmitsTheOwnLedger(t *testing.T) {
	// The profile's own ledger = <gopi dir>/memory.db. Point the gopi dir at a
	// sandbox and let the ledger resolve to its DEFAULT rank, so this is the
	// steady-state shape rather than a second override.
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")

	log := filepath.Join(t.TempDir(), "notify.log")
	t.Setenv(taskNotifyCmdEnv, "cat >> "+log+" && echo >> "+log)
	t.Setenv(taskNotifyWebhookEnv, "")

	taskNotifyDriveOneTransition(t)
	payloads := readNotifyLog(t, log)
	if len(payloads) == 0 {
		t.Fatal("no payload delivered")
	}
	if got := payloads[len(payloads)-1].Ledger; got != "" {
		t.Fatalf("payload ledger = %q for the profile's OWN ledger, want empty — the stamp marks a FINDING (this event came from a ledger you cannot open), so stamping every event makes it noise", got)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read notify log: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal([]byte(notifyFirstPayloadLine(t, string(raw))), &wire); err != nil {
		t.Fatalf("parse delivered payload: %v", err)
	}
	if _, present := wire[taskNotifyLedgerJSONKey]; present {
		t.Fatalf("wire payload carries %q for the profile's own ledger (%v) — omitempty is the contract that keeps the steady state byte-identical", taskNotifyLedgerJSONKey, wire[taskNotifyLedgerJSONKey])
	}
}

// taskNotifyDriveOneTransition submits a row, claims it and moves it to
// in_progress through the REAL tool entry points — the same call sequence the
// shipped lifecycle test uses, so the payload this test reads is produced by
// production code and not by a hand-built struct.
func taskNotifyDriveOneTransition(t *testing.T) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "origin ledger probe row", Payload: "origin ledger briefing"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	taskID := taskNotifyExtractID(t, out)
	if _, err := TaskNext(taskDefaultClaimHolder(), ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
		t.Fatalf("transition: %v", err)
	}
	return taskID
}

// notifyFirstPayloadLine returns the first line of s with content (the cmd target
// appends one framed payload per line).
func notifyFirstPayloadLine(t *testing.T, s string) string {
	t.Helper()
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				return line
			}
			start = i + 1
		}
	}
	t.Fatalf("no non-empty payload line in %q", s)
	return ""
}

// TestTaskNotifyLedgerJSONKeyMatchesTheStructTag pins the CONSTANT to the
// struct tag. A Go struct tag cannot hold a constant, so the two spellings of
// the field name live apart by force — and the shell sink renders the wire
// spelling, so a tag renamed alone would make the sink silently stop printing
// the ledger (a field nobody sees: Rule 15's defect, one language over). Same
// shape as the notify slot-limit pin in cmd/agent.
func TestTaskNotifyLedgerJSONKeyMatchesTheStructTag(t *testing.T) {
	src, err := os.ReadFile("tasknotify.go")
	if err != nil {
		t.Fatalf("read tasknotify.go: %v", err)
	}
	want := `json:"` + taskNotifyLedgerJSONKey + `,omitempty"`
	if !strings.Contains(string(src), want) {
		t.Fatalf("tasknotify.go does not carry the tag %q: the constant taskNotifyLedgerJSONKey and the struct tag have drifted, and scripts/notify/notify.sh renders the constant's spelling", want)
	}
}
