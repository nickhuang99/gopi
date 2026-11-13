package tools

// task_signal_verdict_test.go — the ledger half's regression guard (epic
// signal-provenance-unsourced, 2026-09-17).
//
// The two verdicts must be TWO DIFFERENT EVENTS. The defect this guards is the
// one the epic was opened for: a graceful operator drain and an unannounced kill
// were indistinguishable. A breakdown where both map to one event name would
// restore exactly that, silently, because a `where event = …` filter would
// still return rows.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/signalrec"
)

// TestSignalVerdictEventsAreDistinct pins the whole point: sourced and
// unsourced are different facts and must be different ledger events.
func TestSignalVerdictEventsAreDistinct(t *testing.T) {
	if TaskSignalSourcedEvent == TaskSignalUnsourcedEvent {
		t.Fatalf("both verdicts map to the event %q — a graceful operator stop and an unannounced kill become indistinguishable in the ledger, which is the defect this guard exists to remove",
			TaskSignalSourcedEvent)
	}
	// The names must be COMPOSED from the shared constants, not typed: a
	// re-inlined literal here would drift the moment the vocabulary moves
	// (Rules 1b/13). The assertion is on the composition, so it fails on a
	// literal that happens to look right today.
	wantSourced := signalrec.EventPrefix + signalrec.VerdictSourced
	wantUnsourced := signalrec.EventPrefix + signalrec.VerdictUnsourced
	if TaskSignalSourcedEvent != wantSourced {
		t.Errorf("sourced event = %q, want %q (composed from signalrec's constants)", TaskSignalSourcedEvent, wantSourced)
	}
	if TaskSignalUnsourcedEvent != wantUnsourced {
		t.Errorf("unsourced event = %q, want %q (composed from signalrec's constants)", TaskSignalUnsourcedEvent, wantUnsourced)
	}
}

// TestSignalVerdictDetailCarriesItsEvidence pins the detail's contract: the
// verdict is machine-readable in the SAME spelling the session record uses, the
// signal is named, and a sourced verdict carries the intent it rests on — the
// claim travels with its evidence, so the ledger line is auditable alone.
func TestSignalVerdictDetailCarriesItsEvidence(t *testing.T) {
	const (
		sig  = "SIGTERM"
		evid = "2026-09-17T07:11:18+08:00 worker → operator ran gopi worker --stop"
	)
	sourced := signalVerdictDetail(sig, true, evid)
	if !strings.Contains(sourced, sig) {
		t.Errorf("sourced detail %q does not name the signal %q", sourced, sig)
	}
	// The field/value pair comes from the shared constant, so an operator's jq
	// filter over the session records also matches the ledger detail.
	if !strings.Contains(sourced, signalrec.SourcedField+"=true") {
		t.Errorf("sourced detail %q does not spell %q — the ledger and the session record must share one vocabulary", sourced, signalrec.SourcedField+"=true")
	}
	if !strings.Contains(sourced, evid) {
		t.Errorf("sourced detail %q does not carry the intent evidence %q — the claim must not travel without it", sourced, evid)
	}

	unsourced := signalVerdictDetail(sig, false, "")
	if !strings.Contains(unsourced, signalrec.SourcedField+"=false") {
		t.Errorf("unsourced detail %q does not spell %q", unsourced, signalrec.SourcedField+"=false")
	}
	if strings.Contains(unsourced, "intent=") {
		t.Errorf("unsourced detail %q claims an intent — an unannounced kill has none", unsourced)
	}
	if !strings.Contains(unsourced, "no known gopi stop path") {
		t.Errorf("unsourced detail %q does not state why it is unsourced", unsourced)
	}
}

// TestRecordSignalVerdictWithoutATaskIsANoOp pins the deliberate skip: a bare
// prompt (no task row) owns no ledger row, and inventing one would file a kill
// verdict against an unrelated task. The no-op is silent by design and needs no
// database to prove it — which is exactly why the check is cheap.
func TestRecordSignalVerdictWithoutATaskIsANoOp(t *testing.T) {
	for _, id := range []string{"", "   ", "\t"} {
		if err := RecordSignalVerdict(id, "SIGTERM", false, ""); err != nil {
			t.Errorf("RecordSignalVerdict(%q) = %v, want nil (a run with no row is skipped, not an error)", id, err)
		}
	}
}
