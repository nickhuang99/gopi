package main

// signal_verdict.go — the worker/merger half of the unsourced-kill guard (epic
// signal-provenance-unsourced, 2026-09-17).
//
// THE DEFECT THIS REMOVES. Two deaths that are NOT the same fact produced the
// SAME trail. `gopi worker --stop` (an operator draining a worker) and an
// unannounced `kill <pid>` from a stranger both landed in
// installWorkerStopSignals' handler, both set the same stop flag, and both
// printed "draining". The 2026-09-16T22:23:13-14 cohort — three worker daemons,
// their three run children, the merger daemon and the web server, all SIGTERMed
// inside ~1 s with ZERO kind=intent records anywhere in the tree — left no
// ledger event at all and a session log whose words were indistinguishable from
// a graceful drain.
//
// THE VERDICT comes from the recorder (internal/signalrec), which is the only
// place that can see both halves of the protocol: a stop path writes a
// kind=intent record naming its target and only THEN signals, and the mirror of
// that record is filed in the TARGET's own session dir, so the victim answers
// the question with one file read while its handler is running.
//
// THREE STATES, never two (see signalrec.classifySourced). A record whose
// `sourced` field is absent was NOT classified — no session dir to read, no
// provenance file, or the killer's write still in flight. That is reported as
// an explicit third word: an unclassified death is a gap in the evidence, and
// printing "graceful" for it is exactly the collapse this guard exists to end.
import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/tools"
)

// The two phrases the operator-facing stop line gains. They are the words the
// session log already uses (signalrec.Verdict*), so the console and the record
// cannot drift.
const (
	stopVerdictSourced   = "announced by a gopi stop path"
	stopVerdictUnsourced = "UNSOURCED — no gopi stop path announced this kill (an external signal: kill(1), a supervisor's timeout, a service manager or a session teardown)"
	stopVerdictUnknown   = "provenance not classified (no readable provenance trail for this process)"
)

// stopVerdictPhrase renders the provenance clause of a stop line from the
// recorder's verdict.
func stopVerdictPhrase(v signalrec.KillVerdict) string {
	switch {
	case !v.Classified:
		return stopVerdictUnknown
	case v.Sourced:
		return stopVerdictSourced
	default:
		return stopVerdictUnsourced
	}
}

// stopVerdictUnattributable is the explicit REASON the verdict line prints when
// no task id can be resolved — the other half of the fix. A verdict that prints
// `task ""` states a value nobody can act on and reads as a fact ("the task id is
// empty"); this states the finding (there is no row to attach the event to, and
// here is why) so the operator stops looking for a missing id and sees a gap in
// the evidence instead.
const stopVerdictUnattributable = "no task row can be attributed: this process announced no task id at launch and had claimed no round when the signal arrived, so the ledger event cannot be written at all"

// stopVerdictLedgerLine renders the sentence that says what the ledger will carry
// for an unsourced kill — the ONE renderer of it (Rule 2), so the console line and
// any test read the same text.
//
// IT NEVER PRINTS AN EMPTY TASK ID. An empty id is not a value to render: it means
// no row can carry the event, and the honest line says so and names the reason
// (stopVerdictUnattributable). The measured defect was exactly this renderer's
// predecessor printing `task ""` on the line AFTER one that knew the id.
func stopVerdictLedgerLine(role, taskID string) string {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Sprintf("%s: the ledger will carry no %s event — %s",
			role, signalrec.VerdictUnsourced, stopVerdictUnattributable)
	}
	return fmt.Sprintf("%s: the ledger will carry %s for task %q", role, signalrec.VerdictUnsourced, taskID)
}

// stopVerdictTaskID is the ONE producer of the task id a stop verdict is
// attributed to (Rule 2: one decision, every sink — the console line, the ledger
// event — reads it here, so they cannot disagree).
//
// OBSERVATION BEFORE ANNOUNCEMENT, and the order IS the fix. The launch handoff
// ANNOUNCES one task id, in the environment, before the process starts
// (signalrec.AnnouncedTaskID); a resident `gopi worker` announces none, because it
// claims a different row every round. What the worker has instead is its own
// claimed-round latch — an OBSERVATION of the row the death actually interrupted.
// Reading the announcement first is what produced the measured disagreement
// (2026-09-27, adjacent lines of one session log):
//
//	SENDER UNSOURCED SIGNAL SIGINT TASK 01a0deb7-… AT 2026-09-27T02:39:54+08:00
//	the ledger will carry unsourced for task ""
//
// The first line rendered the latch; the second asked the environment. Both were
// "the task id" to their author, which is exactly why there is now one reader.
//
// The announcement remains the FALLBACK, not the fix's victim: it is the only
// evidence a process that claims no round ever has (a `gopi task run` child, whose
// row the handoff named, and which has no latch to observe).
func stopVerdictTaskID() string {
	if id := strings.TrimSpace(workerStopTargetTask()); id != "" {
		return id
	}
	return strings.TrimSpace(signalrec.AnnouncedTaskID())
}

// recordLostLine is printed when a claiming door's unwind runs out of budget
// with no record on disk AND no refusal — i.e. the recorder was still working
// when the door left. It is the ONE renderer of that finding (Rule 2), and it
// is printed rather than swallowed because a record that never landed is exactly
// the invisible loss the handshake exists to end: an operator reading the door's
// stop line must be able to tell it from a door that was never signalled.
//
// IT IS NOT THE LINE FOR A REFUSAL (see recordRefusedLine): "the record did not
// land" is true of both, and the difference — a race the door lost versus a
// decision the recorder already explained — is the whole reason the recorder
// publishes a THIRD state.
const recordLostLine = "signal provenance — the recorder's record did NOT land on disk before this door exited: this kill is recorded NOWHERE in this profile (a record that is missing cannot be distinguished from one that was never attempted)"

// recordRefusedLine is printed INSTEAD of recordLostLine when the recorder
// deliberately refused the record (signalrec.RecordOutcomeRefused) and reported
// it with its own reason (the NOT FILED line above, from reportUnfiled).
//
// WHY A DOOR SAYS ANYTHING AT ALL. The refusal is already on stderr, so this is
// not the report — it is the statement that nothing more is coming: the operator
// must not read the door's silence as "the record is still on its way", and must
// not read recordLostLine's "NOWHERE … a record that is missing cannot be
// distinguished from one that was never attempted" as a race the door lost. The
// kill still has no record in this profile (that part is true and the line says
// so), but it is a DECISION with a printed reason, not a loss.
const recordRefusedLine = "signal provenance — the recorder REFUSED this record on purpose (reason: the NOT FILED line above): this kill has no record in this profile BY DESIGN, not because this door outran the writer"

// roleLabelMainPrompt is the bare-prompt face's name in those report lines. It is
// the program word itself: the face the operator typed IS `gopi`, and inventing a
// longer label for it would name a verb that does not exist.
const roleLabelMainPrompt = "gopi"

// settleProvenanceRecord is THE door-side half of the record-filed handshake: a
// claiming door calls it ONCE on its unwind, after its own stop line, before it
// lets the process exit, and it blocks until the recorder's record has landed
// (signalrec.AwaitRecordFiled — the ONE place the synchronisation lives).
//
// WHY A DOOR MUST CALL IT. The recorder's handler goroutine and this door's own
// handler are woken by the SAME signal delivery and then race: this door's unwind
// is short and ends in os.Exit, while the recorder still has to resolve a session
// destination, read /proc and append a line. Measured on the pre-fix tree
// (2026-10-12): the since-removed `gopi memory sync --every 5s --push` spelling
// SIGTERMed printed its stop line and left NO record in 12/12 runs, while the same
// signal-wait with SIGHUP (which wakes no door) filed it every time. Nothing in
// the tree reported that loss, which is why the report below is not optional.
//
// It returns whether the record landed, so a caller that wants to assert the fact
// can (the regression guard does). An ordinary exit — no signal reached this
// process — returns false WITHOUT printing anything and without paying the
// budget: there is no record to wait for and nothing was lost.
//
// THE VERDICT IS READ OUTSIDE THE WAIT, AND READING IT CANNOT DISTURB THE WAIT.
// AwaitRecordFiled's primary fact stays "bytes on disk" — settling on "we tried"
// would reinstate the silent loss the handshake exists to remove — so nothing
// about the refusal can make the wait answer EARLIER. What the refusal changes is
// only the door's REPORT: the recorder publishes the third state
// (signalrec.RecordOutcomeRefused) from reportUnfiled, which is where the
// deliberate rejection is decided and printed with its reason.
func settleProvenanceRecord(role string) bool {
	filed := signalrec.AwaitRecordFiled(signalrec.DefaultRecordFiledWait)
	if filed {
		return true
	}
	// A signal DID reach the process and the record is still not there. Two very
	// different findings live behind that, and they are read from the recorder's
	// own state — never re-derived here (Rule 2):
	//
	//   REFUSED — the recorder decided this record must not be written anywhere
	//   and said so with its reason (reportUnfiled's NOT FILED line). Claiming a
	//   LOSS here is a false statement about a deliberate rejection, so the door
	//   states the decision instead. The reason is rendered from the state the
	//   recorder published, so this line stays self-contained: a log that kept the
	//   door's line (a worker's round log) still names WHY, without having to find
	//   the recorder's own line above it.
	//
	//   NOTHING YET — a signal arrived, the recorder has not answered, and the
	//   budget ran out: that IS the loss this handshake exists to make visible,
	//   and it is reported unchanged.
	if refused, reason := signalrec.RecordRefusedNow(); refused {
		fmt.Fprintf(os.Stderr, "%s: %s (refusal reason: %s)\n", role, recordRefusedLine, reason)
		return false
	}
	// The remaining distinguishable case is the genuine loss. Note the state is
	// read rather than inferred from KillVerdictNow alone: an ordinary exit (no
	// signal reached the recorder) answers false with NOTHING published, which is
	// not a finding and must stay silent.
	if signalrec.KillVerdictNow().Received {
		fmt.Fprintf(os.Stderr, "%s: %s\n", role, recordLostLine)
	}
	return false
}

// recordStopVerdict writes BOTH halves of the guard for a process that is
// leaving because a signal arrived: the one-line verdict on stderr (the
// operator's console) and the ledger event on the run's own row (the durable
// record). It is a no-op when this process received no signal.
//
// IT RESOLVES THE TASK ITSELF (stopVerdictTaskID) rather than taking one as an
// argument: the caller used to hand it `signalrec.CurrentTaskID()` — the
// ANNOUNCED value — while the line beside it rendered the OBSERVED one, which is
// the one piece of knowledge printed two ways. A parameter is an invitation for
// the next caller to pass a third spelling.
//
// It never fails the exit: the process is on its way out, and a ledger that
// cannot be written must not cost the shutdown it is describing — but the
// failure IS reported (Rule 15b), so "no event in the ledger" is never confused
// with "the event was never attempted".
func recordStopVerdict(role string, line string) {
	// THE DOOR-SIDE HALF OF THE RECORD-FILED HANDSHAKE, for the ONLY role that
	// reaches here (the worker). The others (the bare prompt, `cli`, chat, term)
	// call settleProvenanceRecord themselves on their own unwind — a worker's
	// unwind runs through THIS function, so without the call the worker was the
	// one claiming door that could print "terminated received" and exit without
	// ever stating whether the recorder's record landed, refused or was lost.
	// That is precisely the silent loss the handshake exists to remove, and it
	// is why the settlement lives HERE: one call site per door, and this is the
	// worker's (Rule 2).
	defer settleProvenanceRecord(role)
	v := signalrec.AwaitKillVerdict(signalrec.DefaultVerdictWait)
	if !v.Received {
		return
	}
	taskID := stopVerdictTaskID()
	phrase := stopVerdictPhrase(v)
	if line != "" {
		fmt.Fprintf(os.Stderr, "%s: %s\n", role, line)
	}
	fmt.Fprintf(os.Stderr, "%s: signal provenance — %s (%s)\n", role, v.Signal, phrase)
	if v.Classified && !v.Sourced && v.IntentEvidence == "" {
		// THE ATTRIBUTION IS RENDERED FROM THE SAME ID THE EVENT IS WRITTEN WITH
		// (stopVerdictTaskID). When there is none, the line does not print an
		// empty value — it names the reason and states that no event can be
		// written, because RecordSignalVerdict below is a silent no-op on an
		// empty id (Rule 15: a refusal must say what it did NOT do).
		fmt.Fprintln(os.Stderr, stopVerdictLedgerLine(role, taskID))
	}
	if err := tools.RecordSignalVerdict(taskID, v.Signal, v.Sourced, v.IntentEvidence); err != nil {
		tools.ReportSignalVerdictFailure(taskID, err)
	}
}
