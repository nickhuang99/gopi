package signalrec

// signalrec_repeat_count_test.go — THE MINIMAL REPRO of the silent drop
// (Rule 15), and it is deliberately written with NO symbol this fix introduces.
//
// WHY THAT CONSTRAINT IS THE POINT. A minimal repro must be able to FAIL ON THE
// PRISTINE BASELINE for the behaviour it names. A test that mentions
// KindSignalRepeat / EventTypeSignalRepeat cannot even compile there, so its
// "red" would be a compile error — an error about a NAME, not a measurement of
// the defect — and a red proof of that shape proves nothing about the drop. So
// this file counts RECORDS and asserts nothing else: the count is the observable
// the defect destroyed, and it exists on both sides of the fix.
//
// Measured on 1c5b086 (the baseline) and on the fixed tree; both outputs are in
// this run's session artifacts.
//
// THE CONTRACT. Every delivery of a stop signal leaves evidence, in a file the
// operator already reads. Before the fix, Install's handler answered the second
// and every later delivery of a signal kind with a bare `continue`: three
// SIGTERMs produced ONE record, and nothing anywhere distinguished "three stop
// requests arrived" from "one arrived". The count is therefore the whole
// falsifiable claim, and it needs no new vocabulary to state.

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// repeatCountDeliveries is how many times the fixture delivers the SAME signal.
// Three is the smallest number that separates the two wrong shapes: a drop
// (1 record), a duplicate-death bug (3 records all reading "a process died
// here"), and the contract (3 records, one death and two re-affirms — the
// kinds are separated in signalrec_repeat_test.go, which owns the vocabulary).
const repeatCountDeliveries = 3

// TestSignalRecRepeatedDeliveryLeavesEvidenceInsteadOfSilence is the minimal
// repro: N deliveries of one signal kind must leave N evidence lines.
func TestSignalRecRepeatedDeliveryLeavesEvidenceInsteadOfSilence(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "repeat-count-session")
	SetSessionDir(dir, "", "")

	uninstall := Install("")
	defer uninstall()

	for i := 0; i < repeatCountDeliveries; i++ {
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("self-signal %d: %v", i+1, err)
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep makes the deliveries land as distinct events; bounded by the arm
	}
	recs := srWaitForRecord(t, dir, repeatCountDeliveries)
	if len(recs) != repeatCountDeliveries {
		t.Fatalf("%d deliveries of SIGTERM left %d provenance record(s) in %s, want %d — every delivery but the last of its kind was DROPPED IN SILENCE (Rule 15), so an operator who pressed Ctrl-C four times and one who pressed it once leave identical evidence",
			repeatCountDeliveries, len(recs), dir, repeatCountDeliveries)
	}
	// srReadIn already fatals on a line that is not one JSON object, so reaching
	// here means all N lines parse; each must still be readable as THIS signal
	// (the fact the operator greps for), whichever kind it carries.
	for i, r := range recs {
		if r.Signal != SignalSIGTERM {
			t.Errorf("record %d: signal = %q, want %q — the delivery's name must be readable, not a number and not blank", i+1, r.Signal, SignalSIGTERM)
		}
		if r.TS == "" {
			t.Errorf("record %d carries no timestamp", i+1)
		}
		if r.PID != os.Getpid() {
			t.Errorf("record %d: pid = %d, want %d", i+1, r.PID, os.Getpid())
		}
	}
}
