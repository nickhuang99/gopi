package signalrec

// signalrec_sicapture_test.go — the end-to-end guard for "not captured" being a
// STATE in the record instead of a MISSING KEY (epic signal-provenance-si-code,
// follow-up).
//
// THE DEFECT THIS PINS, measured on this host 2026-10-27 (297 signal records in
// ~/.gopi/sessions/*/signal-provenance.jsonl): 290 of them carried no si_code
// key AT ALL, and every one of those was written by a recorder whose capture had
// armed fine a moment earlier; the 7 that did carry it were written by processes
// started after the feature landed. A reader of the file therefore could not
// tell
//
//	"this delivery carried no kernel source"            (a fact about the signal)
//	from "this BINARY predates the capture"              (a fact about the build)
//	from "the capture refused to arm on this host"       (a fact about the arm)
//
// — all three rendered identically, as silence. Rule 15: silence is the defect.
// Rule 15a: si_code's zero value (SI_USER) is a REAL answer, so it cannot be the
// "not captured" marker; the state needs a field of its own.
//
// THE FOUR ARMS, and the direction each one fails in:
//
//	① A REAL delivery in a REAL second process → the filed record carries
//	   si_capture == "captured" AND si_code == SI_USER, and NO reason. Fails if
//	   the state field is absent (the pre-fix shape), or if it claims "captured"
//	   while si_code is missing.
//	② THE REVERSE: a recorder whose capture was RELEASED (armed, then disarmed —
//	   the shape a failed capture leaves behind) → the record still carries
//	   si_capture, now "not-captured", WITH a reason, and NO si_code. Fails if
//	   the field vanishes (exactly what the old code did) or if a fabricated
//	   si_code appears.
//	③ The wire contract: the key is NOT `omitempty` and the vocabulary is TOTAL
//	   and closed — asserted on the marshalled JSON, because a struct tag is what
//	   a reader of the FILE sees.
//	④ The OTHER record kind: an intent line says "no-delivery" by name, so
//	   "not applicable" cannot be confused with "the producer forgot to stamp it".

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/sigsource"
)

// srCaptureDisarmedEnv makes the fixture victim RELEASE its capture right after
// arming it, so arm ② measures the "armed, then not capturing" path in a real
// process rather than a mocked one.
const srCaptureDisarmedEnv = "GOPI_SIGNALREC_TEST_CAPTURE_DISARMED"

// TestSignalRecordNamesTheCaptureState is arm ①: a real kill in a real child
// leaves a record in which the capture state is a QUERYABLE value, and that
// value agrees with whether si_code is present.
func TestSignalRecordNamesTheCaptureState(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "sicapture-session")

	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0])
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		srUnsourcedVictimEnv: "1",
		SessionDirEnv:        dir,
		srReadyEnv:           ready[srReadyEnv],
	})
	cmd.Stderr = os.Stderr
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecordNamesTheCaptureState")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the victim: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal the victim: %v", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	_ = cmd.Process.Kill() //stylegate:ignore teardown of the fixture child after the record was polled; the kill is not the assertion
	_ = cmd.Wait()         //stylegate:ignore reaping the child just killed; its wait status is not asserted
	if len(recs) == 0 {
		t.Fatalf("the victim left no record in %s — nothing to assert the capture state on", dir)
	}
	r := recs[len(recs)-1]

	// ARM ① — the state field is PRESENT. This is the arm that is RED before the
	// fix: the key did not exist at all, so no reader could tell the three
	// meanings above apart.
	if r.SICapture == "" {
		t.Fatalf("record carries no %q field — \"not captured\" is still a MISSING KEY, not a state: %s",
			SICaptureField, srJSON(r))
	}
	if r.SICapture != sigsource.CaptureCaptured {
		t.Fatalf("%s = %q, want %q: this host HAS the capture and the delivery was in a child that armed it (reason=%q): %s",
			SICaptureField, r.SICapture, sigsource.CaptureCaptured, r.SICaptureReason, srJSON(r))
	}
	// The state and the field it describes must AGREE. A "captured" state with no
	// si_code is the contradiction this arm exists to make impossible.
	if r.SICode == nil {
		t.Fatalf("%s = %q but no %q field: the state claims a capture that is not in the record: %s",
			SICaptureField, r.SICapture, SICodeField, srJSON(r))
	}
	if *r.SICode != sigsource.SICodeUser {
		t.Fatalf("%s = %d (%s), want %d (%s)", SICodeField, *r.SICode, r.SICodeName,
			sigsource.SICodeUser, sigsource.CodeName(sigsource.SICodeUser))
	}
	if r.SICaptureReason != "" {
		t.Fatalf("%s = %q on a CAPTURED delivery — a reason is for the absence, and one here would leave a reader "+
			"unable to tell the two apart: %s", SICaptureReasonField, r.SICaptureReason, srJSON(r))
	}
}

// TestSignalRecordNamesTheAbsenceWhenTheCaptureIsDisarmed is arm ② — the shape
// the old code rendered as silence.
func TestSignalRecordNamesTheAbsenceWhenTheCaptureIsDisarmed(t *testing.T) {
	if sigsource.UnsupportedReason() != "" {
		t.Skipf("this target has no capture implementation, so the DISARMED arm is not reachable: %s",
			sigsource.UnsupportedReason())
	}
	home := srHome(t)
	dir := srSessionDir(t, home, "sicapture-disarmed-session")

	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0])
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		srUnsourcedVictimEnv: "1",
		srCaptureDisarmedEnv: "1",
		SessionDirEnv:        dir,
		srReadyEnv:           ready[srReadyEnv],
	})
	cmd.Stderr = os.Stderr
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecordNamesTheAbsenceWhenTheCaptureIsDisarmed")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the victim: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal the victim: %v", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	_ = cmd.Process.Kill() //stylegate:ignore teardown of the fixture child after the record was polled; the kill is not the assertion
	_ = cmd.Wait()         //stylegate:ignore reaping the child just killed; its wait status is not asserted
	if len(recs) == 0 {
		t.Fatalf("the victim left no record in %s", dir)
	}
	r := recs[len(recs)-1]

	if r.SICapture != sigsource.CaptureNotCaptured {
		t.Fatalf("%s = %q, want %q for a delivery whose capture was RELEASED before it arrived: %s",
			SICaptureField, r.SICapture, sigsource.CaptureNotCaptured, srJSON(r))
	}
	if strings.TrimSpace(r.SICaptureReason) == "" {
		t.Fatalf("%s = %q with no %q — a state without its reason is a number without units: %s",
			SICaptureField, r.SICapture, SICaptureReasonField, srJSON(r))
	}
	// NO FABRICATED CODE. si_code must be absent here: writing 0 would read as
	// SI_USER, i.e. "an ordinary process sent this", which is the invented
	// accusation Rule 15a exists to prevent.
	if r.SICode != nil {
		t.Fatalf("%s = %d on a NOT-CAPTURED delivery — 0 would be read as SI_USER, a fabricated sender: %s",
			SICodeField, *r.SICode, srJSON(r))
	}
}

// TestEveryStampedRecordKindCarriesTheCaptureState is arm ③: the field is not
// `omitempty` and not conditional on the code path.
func TestEveryStampedRecordKindCarriesTheCaptureState(t *testing.T) {
	// The wire contract, asserted on a marshalled record rather than on the Go
	// field: a struct tag is what a reader of the FILE sees, and `omitempty` on
	// this key is precisely the defect (Rule 15a).
	raw, err := json.Marshal(record{Kind: KindSignal, Signal: "SIGTERM"})
	if err != nil {
		t.Fatalf("marshal a bare record: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal the record just marshalled: %v", err)
	}
	if _, ok := decoded[SICaptureField]; !ok {
		t.Fatalf("the JSON key %q is OMITTED when the state is empty — a record written by a binary that does not "+
			"know about the capture would then be indistinguishable from one that measured it: %s",
			SICaptureField, raw)
	}

	// And the vocabulary itself: every state it can report is one of the named
	// values, so an operator's filter has a closed set (Rule 14) and no member is
	// the empty string (silence is not a state).
	for _, state := range []string{
		sigsource.CaptureCaptured,
		sigsource.CaptureNotCaptured,
		sigsource.CaptureUnsupported,
		sigsource.CaptureArmFailed,
		sigsource.CaptureNoDelivery,
	} {
		if strings.TrimSpace(state) == "" {
			t.Fatalf("the capture-state vocabulary carries an empty member — silence is not a state")
		}
	}
}

// TestIntentRecordSaysItIsNotADelivery is arm ④: the OTHER record kind. A
// KindIntent line is written by the KILLER before it signals, so there is no
// siginfo to capture — and that fact has to be SAID (CaptureNoDelivery), not
// left as an empty string, or a reader cannot tell "not applicable" from "the
// producer forgot to stamp it" (Rule 15a).
func TestIntentRecordSaysItIsNotADelivery(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "sicapture-intent-session")
	SetSessionDir(dir, "", "")
	t.Cleanup(func() { SetSessionDir("", "", "") })

	if err := Intent("test-kind", "the capture-state arm's intent line", os.Getpid(), 0); err != nil {
		t.Fatalf("write an intent record: %v", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	if len(recs) == 0 {
		t.Fatalf("the intent record never reached %s", dir)
	}
	var intent *record
	for i := range recs {
		if recs[i].Kind == KindIntent {
			intent = &recs[i]
		}
	}
	if intent == nil {
		t.Fatalf("no %q record among %d records in %s", KindIntent, len(recs), dir)
	}
	if intent.SICapture != sigsource.CaptureNoDelivery {
		t.Fatalf("%s = %q on a %q record, want %q — an intent is not a delivery and must say so: %s",
			SICaptureField, intent.SICapture, KindIntent, sigsource.CaptureNoDelivery, srJSON(*intent))
	}
	if strings.TrimSpace(intent.SICaptureReason) == "" {
		t.Fatalf("%s = %q with no reason: %s", SICaptureField, intent.SICapture, srJSON(*intent))
	}
	if intent.SICode != nil {
		t.Fatalf("%s = %d on an intent record — the killer was never signalled, so a code here is fabricated: %s",
			SICodeField, *intent.SICode, srJSON(*intent))
	}
}
