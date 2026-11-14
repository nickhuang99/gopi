package signalrec

// signalrec_repeat_test.go — the CONTRACT of a repeated delivery (epic:
// repeated signals are at least observable).
//
// The minimal repro lives next door (signalrec_repeat_count_test.go) and counts
// records, because a repro must be able to FAIL on the pristine baseline. THIS
// file owns the vocabulary the fix introduces, so it needs the fixed tree to
// compile — which is correct: a test written against a new symbol is a guard,
// not a repro.
//
// FOUR PROPERTIES, each one a different way the fix can be wrong:
//
//  1. A REPEAT IS RECORDED, AS A RE-AFFIRM — the second delivery of SIGTERM
//     leaves a line, that line is KindSignalRepeat (never a second KindSignal:
//     see the kind's own doc — a fleet-wide kill must not look like twice as
//     many dead processes), and it says which delivery it is.
//  2. THE DEATH RECORD IS UNTOUCHED — exactly ONE KindSignal line for the
//     process, with the FIRST delivery's verdict, whatever the key was pressed.
//  3. NO DISPOSITION MOVES — a repeat publishes neither the received/verdict
//     outcome nor the FILED outcome a claiming door waits on, and it never
//     re-raises. This is the "no escalation" half of the contract: the repeat
//     arm's whole job is to be observable and inert.
//  4. THE RUN'S OWN SESSION LOG GETS ITS OWN EVENT TYPE — an operator's
//     `"type":"signal_verdict"` filter still returns ONE verdict per run, while
//     the repeat is visible under its own type with its own count.
//
// The arms that pin the UNCHANGED half — signalForwarding()'s decision table and
// the ClaimSignalHandlers contract — are at the bottom, deliberately in the same
// file as the change they must not have moved.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// repeatFixture sets up a session dir OWNED by this process plus a live session
// log, installs the recorder, and delivers the same signal repeatTestDeliveries
// times. It returns the provenance records and the session-log path.
func repeatFixture(t *testing.T, id string) ([]record, string) {
	t.Helper()
	home := srHome(t)
	dir := srSessionDir(t, home, id)
	logPath := filepath.Join(dir, sessionlayout.RecordName)
	if err := os.WriteFile(logPath, []byte(`{"type":"session_start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write the run's session log: %v", err)
	}
	SetSessionDir(dir, "", logPath)

	uninstall := Install("")
	defer uninstall()

	for i := 0; i < repeatTestDeliveries; i++ {
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("self-signal %d: %v", i+1, err)
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep makes the deliveries land as distinct events the recorder can tell apart; bounded by the arm
	}
	return srWaitForRecord(t, dir, repeatTestDeliveries), logPath
}

// repeatTestDeliveries is the number of SAME-signal deliveries the fixture
// makes: one death plus two re-affirms, which is the smallest shape in which a
// wrong "first only" and a wrong "every one is a death" both show up.
const repeatTestDeliveries = 3

// TestSignalRecRepeatIsRecordedAsAReAffirm pins property 1 and 2: every
// delivery is evidence, and only the first is a DEATH.
func TestSignalRecRepeatIsRecordedAsAReAffirm(t *testing.T) {
	recs, _ := repeatFixture(t, "repeat-kind-session")
	if len(recs) != repeatTestDeliveries {
		t.Fatalf("%d deliveries left %d record(s), want %d", repeatTestDeliveries, len(recs), repeatTestDeliveries)
	}
	if recs[0].Kind != KindSignal {
		t.Errorf("record 1 kind = %q, want %q — the FIRST delivery is the death", recs[0].Kind, KindSignal)
	}
	for i, r := range recs[1:] {
		if r.Kind != KindSignalRepeat {
			t.Errorf("record %d kind = %q, want %q — a repeat must not be filed as a second death (that is how a fleet-wide kill comes to look like twice as many dead processes)", i+2, r.Kind, KindSignalRepeat)
		}
		if r.Signal != SignalSIGTERM {
			t.Errorf("record %d: signal = %q, want %q", i+2, r.Signal, SignalSIGTERM)
		}
		// The delivery's ORDINAL is the fact a reader cannot reconstruct from
		// anything else — "a repeat happened" tells them nothing about whether
		// their second request or their twentieth arrived.
		want := fmt.Sprintf("delivery %d of %s", i+2, SignalSIGTERM)
		if !strings.Contains(r.Note, want) {
			t.Errorf("record %d note = %q, want it to state %q (which delivery this is)", i+2, r.Note, want)
		}
		if !strings.Contains(r.Note, repeatVerb) {
			t.Errorf("record %d note = %q, want the word %q — the note must say this is a RE-AFFIRM, never an escalation", i+2, r.Note, repeatVerb)
		}
	}
}

// TestSignalRecRepeatDoesNotMoveTheDeathVerdictOrTheDoorOutcome pins property 3:
// the re-affirm is inert. The facts a claiming door synchronises on are the
// DEATH record's, and a repeat must not be able to answer for it.
func TestSignalRecRepeatDoesNotMoveTheDeathVerdictOrTheDoorOutcome(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "repeat-inert-session")
	SetSessionDir(dir, "", "")

	uninstall := Install("")
	defer uninstall()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("self-signal: %v", err)
	}
	first := srWaitForRecord(t, dir, 1)
	if len(first) != 1 {
		t.Fatalf("the first delivery left %d records, want 1", len(first))
	}
	// The death record is on disk, so the door's question has an answer.
	if !RecordFiledNow() {
		t.Fatal("the death record is on disk but the recorder did not publish FILED — the door would wait out its budget and print a loss that did not happen")
	}
	deathVerdict := KillVerdictNow()
	deathOutcome := RecordOutcomeNow()

	// NOW the repeats. Nothing they do may be observable through these three.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("repeat signal: %v", err)
	}
	recs := srWaitForRecord(t, dir, 2)
	if len(recs) != 2 {
		t.Fatalf("the repeat left %d records, want 2 — the re-affirm must be WRITTEN (this is the whole fix)", len(recs))
	}
	if got := KillVerdictNow(); got != deathVerdict {
		t.Errorf("the re-affirm MOVED the published verdict: %+v, want the death delivery's %+v (a repeat re-affirms; it does not re-classify)", got, deathVerdict)
	}
	if got := RecordOutcomeNow(); got != deathOutcome {
		t.Errorf("the re-affirm moved the published record outcome: %q, want the death delivery's %q", got, deathOutcome)
	}
	if recs[0].Kind != KindSignal || strings.Contains(recs[0].Note, repeatVerb) {
		t.Errorf("the FIRST record became %q / note %q — the death record must not have been rewritten into a re-affirm", recs[0].Kind, recs[0].Note)
	}
}

// TestSignalRecRepeatReachesTheSessionLogAsItsOwnEvent pins property 4 on the
// file an operator actually opens after a kill: the repeat is visible, and the
// verdict count stays ONE.
func TestSignalRecRepeatReachesTheSessionLogAsItsOwnEvent(t *testing.T) {
	recs, logPath := repeatFixture(t, "repeat-log-session")
	if len(recs) < repeatTestDeliveries {
		t.Fatalf("%d deliveries left %d records, want %d", repeatTestDeliveries, len(recs), repeatTestDeliveries)
	}
	lines := srParseEveryLine(t, logPath)

	var verdicts, repeats []map[string]any
	for _, ev := range lines {
		switch ev[envelopeKeyType] {
		case sessionevent.EventTypeSignalVerdict:
			verdicts = append(verdicts, ev)
		case sessionevent.EventTypeSignalRepeat:
			repeats = append(repeats, ev)
		}
	}
	// ONE verdict per run, WHATEVER the key was pressed: this is the arm that
	// would fail if a re-affirm were folded into the verdict type, and it is the
	// reason the type is separate.
	if len(verdicts) != 1 {
		t.Fatalf("the session log carries %d %s event(s), want 1 — an operator's post-kill filter must return ONE verdict per run, not one per keystroke:\n%s",
			len(verdicts), sessionevent.EventTypeSignalVerdict, srReadWholeFile(t, logPath))
	}
	if len(repeats) != repeatTestDeliveries-1 {
		t.Fatalf("the session log carries %d %s event(s), want %d (one per extra delivery):\n%s",
			len(repeats), sessionevent.EventTypeSignalRepeat, repeatTestDeliveries-1, srReadWholeFile(t, logPath))
	}
	// The repeat's own count field: the fact that makes the event worth writing.
	for i, ev := range repeats {
		want := float64(i + 2) // delivery 2, 3, … — JSON numbers decode as float64
		if got := ev[repeatCountField]; got != want {
			t.Errorf("repeat event %d carries %s=%v, want %v (the delivery's ordinal must be machine-readable, not only prose in the note)", i+1, repeatCountField, got, want)
		}
		if got := ev[signalKeyPID]; got == nil {
			t.Errorf("repeat event %d carries no %s — the event must name the process it describes, like the verdict event does", i+1, signalKeyPID)
		}
		// SourcedField belongs to the VERDICT. A repeat states no verdict, so
		// the field must be ABSENT rather than a repeated (or fabricated) value.
		if v, present := ev[SourcedField]; present {
			t.Errorf("repeat event %d carries %s=%v — a re-affirm classifies nothing; the verdict field is the verdict event's own", i+1, SourcedField, v)
		}
	}
}

// TestSignalRecRepeatNeverMovesSignalForwarding pins the UNCHANGED half: the
// re-raise decision is the same predicate it was, on the same inputs.
//
// The table is asserted through the REAL function (signalForwarding), not
// through a copy of its terms: the point of the arm is that adding a repeat path
// did not change which processes own their signal handlers — a repeat must never
// arm or disarm a re-raise.
func TestSignalRecRepeatNeverMovesSignalForwarding(t *testing.T) {
	t.Setenv(EnvNoForward, "")
	t.Setenv(envOwnsHandlers, "")

	// An unclassifiable argv (this test binary's own) forwards; the table's
	// handler-owning modes never do. Both halves are asserted because a repeat
	// path that flipped either would be an escalation.
	if !forwardsDefault(roleFromArgv(os.Args)) {
		t.Fatalf("this fixture assumes an unclassifiable argv; roleFromArgv(os.Args) = %q owns handlers", roleFromArgv(os.Args))
	}
	if !signalForwarding() {
		t.Fatal("signalForwarding() = false with no handler claim — a one-shot process must keep SIGTERM's default death")
	}
	for _, mode := range []string{RoleMergerVerb, "worker", "chat", "serve", "web", "resume", "session", taskRunMode, taskMergerMode} {
		if forwardsDefault(mode) {
			t.Errorf("forwardsDefault(%q) = true — a handler-owning mode must be left to its own graceful path, repeats included", mode)
		}
	}
	// ClaimSignalHandlers is PROCESS-WIDE and one-way (see its own doc): the arm
	// asserts the claim alone suppresses the re-raise, which is the behaviour the
	// repeat path must inherit rather than re-decide.
	handlersClaimed.Store(true)
	if signalForwarding() {
		t.Error("signalForwarding() = true after ClaimSignalHandlers — the claim must suppress the re-raise, and a repeat must not restore it")
	}
	handlersClaimed.Store(false)
	if !signalForwarding() {
		t.Error("signalForwarding() = false after the claim was cleared — the predicate must be a function of its live inputs, not of history")
	}
}

// TestSignalRecRepeatOffAndUnchangedDispositionOnDecode pins the last inert
// property: the repeat record decodes as a plain record, so every reader that
// already parses this file (an operator's jq filter over `kind`) sees a line it
// understands rather than a shape it must learn.
func TestSignalRecRepeatOffAndUnchangedDispositionOnDecode(t *testing.T) {
	r := record{TS: nowStamp(), Kind: KindSignalRepeat, Signal: SignalSIGTERM, PID: os.Getpid(), Note: repeatVerb}
	line, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal a repeat record: %v", err)
	}
	var back record
	if err := json.Unmarshal(line, &back); err != nil {
		t.Fatalf("a repeat record is not readable by the package's own decoder: %v", err)
	}
	if back.Kind != KindSignalRepeat || back.Signal != SignalSIGTERM || back.PID != os.Getpid() {
		t.Errorf("round-trip lost a field: %+v", back)
	}
	// An operator filtering the DEATH records by kind must not be handed a
	// repeat — the two kinds are the whole separation.
	if back.Kind == KindSignal {
		t.Errorf("a repeat record reports kind=%q — it would be counted as a termination", KindSignal)
	}
}
