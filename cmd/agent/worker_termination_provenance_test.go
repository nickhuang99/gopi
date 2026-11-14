package main

// worker_termination_provenance_test.go — the regression pin for the epic
// `"terminated received" is unattributable`.
//
// WHY IT EXISTS. The worker's termination line is the ONLY artifact an operator
// has for "who killed the workers?". It used to be a single string with no
// sender, no signal, no task and no wallclock, IDENTICAL for an operator's
// graceful `--stop` and for an unsourced external cohort kill. Measured on
// peerbox (2026-09-16, ~/.gopi/logs/worker.log, 31 termination lines): 31/31
// read `terminated received`, 31/31 named no sender, only 3/31 resolved to a
// task id — and an operator reading them concluded "routine shutdowns" while at
// least three were an external kill of an unrelated cohort.
//
// WHAT IS PINNED HERE (both exit shapes, so neither can silently regress):
//
//	① a SOURCED stop (a durable request an operator's `--stop` filed) prints the
//	   requester's pid + argv, the signal spelling, the target task and the
//	   wallclock, on the SAME line;
//	② an UNSOURCED stop (a bare signal — Linux hands a handler no sender
//	   identity, so the sender is genuinely unknown) prints a DIFFERENT string
//	   carrying the unsourced marker, and never invents an identity;
//	③ the request-file header round-trips, and a legacy body (a bare pid line
//	   from a pre-provenance sender) degrades to UNSOURCED instead of failing —
//	   the two-generation compatibility the file format depends on.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

// wpTaskID is the row the sourced case claims: the TASK field must name it.
const wpTaskID = "wp-provenance-task-1"

// wpWallclockRE matches the AT field's RFC3339 stamp (`2026-09-17T05:43:25+08:00`).
var wpWallclockRE = regexp.MustCompile(`AT \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

// TestWorkerTerminationLineCarriesSenderSignalTaskAndClock is the SOURCED shape:
// a durable request filed by THIS process, consumed at the claim boundary,
// produces a termination line that answers who/what/which/when on ONE line.
func TestWorkerTerminationLineCarriesSenderSignalTaskAndClock(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	ctx := drainTestCtx(t, dir)

	// The round that was running, then the operator's stop — the real order
	// (claim latch → request file → boundary consume).
	workerRecordStopTask(wpTaskID)
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	ctx.consumeDrainRequests()
	if !workerStopRequested() {
		t.Fatal("the durable request was not consumed — the premise of this test does not hold")
	}

	line := workerDrainLine(workerStopReason(), false)

	// ① ONE line: the whole event must be readable by grepping a single line of
	// the worker log (a multi-line report would be split across a grep).
	if strings.ContainsAny(line, "\n\r") {
		t.Errorf("the termination line is not a single line: %q", line)
	}
	// ② The drain verdict itself is unchanged (the operator contract asserted
	// elsewhere keys on this word).
	if !strings.Contains(line, workerDrainVerb) || !strings.Contains(line, "consumed at the claim boundary") {
		t.Errorf("the drain verdict was lost from the line: %q", line)
	}
	// ③ SENDER: the requester's own pid and argv — the one sender identity a
	// victim can hold, because the requester wrote the file.
	wantSender := workerStopFieldSenderLabel + " pid " + strconv.Itoa(os.Getpid()) + " argv "
	if !strings.Contains(line, wantSender) {
		t.Errorf("the line does not name the sender (%q missing): %q", wantSender, line)
	}
	if !strings.Contains(line, strconv.Quote(strings.Join(os.Args, " "))) {
		t.Errorf("the line does not carry the requester's argv: %q", line)
	}
	// ④ SIGNAL: the spelling the requester recorded (a graceful `--stop` sends
	// SIGTERM).
	if !strings.Contains(line, workerStopFieldSignalLabel+" "+workerGracefulSignalName) {
		t.Errorf("the line does not name the signal %s: %q", workerGracefulSignalName, line)
	}
	// ⑤ TASK: the row the worker had claimed.
	if !strings.Contains(line, workerStopFieldTaskLabel+" "+wpTaskID) {
		t.Errorf("the line does not name the target task %s: %q", wpTaskID, line)
	}
	// ⑥ WALLCLOCK.
	if !wpWallclockRE.MatchString(line) {
		t.Errorf("the line carries no wallclock: %q", line)
	}
	// ⑦ A sourced line is NOT the unsourced shape.
	if strings.Contains(line, workerStopUnsourcedMarker) {
		t.Errorf("a sourced termination rendered as unsourced: %q", line)
	}
}

// TestWorkerTerminationUnsourcedIsADifferentString is the UNSOURCED shape — the
// class of event that could not be told apart from a graceful stop before this
// work. A bare signal carries no sender record, so the line must SAY SO (the
// unsourced marker) and must be a different string from the sourced one.
func TestWorkerTerminationUnsourcedIsADifferentString(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	// A bare signal: the handler records the reason and the spelling, and
	// nothing else exists — no request file, therefore no sender identity.
	workerRequestStopWithSignal("terminated received", false, "SIGTERM")

	unsourced := workerDrainLine(workerStopReason(), false)
	if !strings.Contains(unsourced, workerStopUnsourcedMarker) {
		t.Errorf("an unsourced termination does not carry the %q marker: %q", workerStopUnsourcedMarker, unsourced)
	}
	if !strings.Contains(unsourced, workerStopFieldSignalLabel+" SIGTERM") {
		t.Errorf("an unsourced termination must still name the signal it received: %q", unsourced)
	}
	if !strings.Contains(unsourced, workerStopFieldTaskLabel+" "+workerStopNoTaskMarker) {
		t.Errorf("a worker that never claimed must say %q rather than a made-up task: %q", workerStopNoTaskMarker, unsourced)
	}

	// The SOURCED line for the same moment, so the two are compared directly:
	// the acceptance is that a reader can grep the two events apart.
	workerRecordStopSender(workerStopSender{PID: 4242, Argv: "gopi worker --stop", Signal: "SIGTERM", TS: "2026-09-17T05:43:25+08:00"})
	sourced := workerDrainLine(workerStopReason(), false)

	if sourced == unsourced {
		t.Fatal("a sourced and an unsourced termination render the SAME string — the defect this epic exists to fix")
	}
	if strings.Contains(sourced, workerStopUnsourcedMarker) {
		t.Errorf("the sourced shape still carries the unsourced marker: %q", sourced)
	}
	// Grep-ability in both directions: the unsourced marker occurs ONLY on the
	// unsourced line, so `grep UNSOURCED worker.log` selects exactly the
	// unattributable events.
	if got := strings.Count(sourced, workerStopUnsourcedMarker); got != 0 {
		t.Errorf("the sourced line carries %d unsourced markers, want 0: %q", got, sourced)
	}
	if !strings.Contains(unsourced, workerStopFieldSenderLabel+" "+workerStopUnsourcedMarker) {
		t.Errorf("the unsourced SENDER field must BE the marker (a blank field would read as 'no sender was involved'): %q", unsourced)
	}
}

// TestWorkerStopSenderFieldCarriesNoSignal pins the SHAPE of the SENDER field
// itself: identity only. The first live stop rendered the signal twice on one
// line (`signal SIGTERM SIGNAL SIGTERM` — once inside SENDER, once in the
// line's own SIGNAL field), which gives a reader two places to disagree with
// each other. The signal is a separate fact with its own field; SENDER must
// never repeat it, and must never render as an empty string (a blank sender
// field is the unattributable artifact this epic removes).
func TestWorkerStopSenderFieldCarriesNoSignal(t *testing.T) {
	sourced := workerStopSender{
		PID:    4242,
		Argv:   "gopi worker --stop",
		Signal: workerGracefulSignalName,
	}.Render()
	if strings.Contains(sourced, workerGracefulSignalName) || strings.Contains(sourced, workerStopFieldSignalLabel) {
		t.Errorf("the SENDER field repeats the signal (it has its own field): %q", sourced)
	}
	if !strings.Contains(sourced, "4242") || !strings.Contains(sourced, "gopi worker --stop") {
		t.Errorf("the SENDER field dropped the identity it exists to carry: %q", sourced)
	}
	if (workerStopSender{}).Render() == "" {
		t.Error("an empty sender rendered as an empty string — a blank SENDER field reads as 'no sender was involved'")
	}
}

// TestWorkerStopRequestCarriesSenderHeaderAndKeepsLegacyBodyReadable pins the
// WIRE contract of the request file in both directions: the header a
// provenance-aware reader parses, and the legacy body shape (a bare pid line)
// that must degrade to UNSOURCED rather than to a parse failure — a
// pre-provenance worker receiving the new body, and a provenance-aware worker
// receiving an old one, both have to keep working (the stop is delivered by the
// file NAME, never by its body).
func TestWorkerStopRequestCarriesSenderHeaderAndKeepsLegacyBodyReadable(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	target := os.Getpid()

	if err := workerRequestStopFile(dir, target); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	// The NAME-keyed readers are untouched by the body change (they never parse
	// it) — the compatibility half that keeps a running worker stoppable.
	if !workerDrainRequestedFor(dir, target) {
		t.Error("the durable request is no longer visible to the name-keyed reader — a second --stop would re-signal")
	}

	body, err := os.ReadFile(tools.ControlRequestPath(dir, workerStopRequestPrefix, target))
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	sender := parseWorkerStopSender(body)
	if !sender.Sourced() {
		t.Fatalf("the request body carries no sender header: %q", body)
	}
	if sender.PID != os.Getpid() {
		t.Errorf("sender pid = %d, want the requester's %d", sender.PID, os.Getpid())
	}
	if sender.Argv != strings.Join(os.Args, " ") {
		t.Errorf("sender argv = %q, want %q", sender.Argv, strings.Join(os.Args, " "))
	}
	if sender.Signal != workerGracefulSignalName {
		t.Errorf("sender signal = %q, want %q", sender.Signal, workerGracefulSignalName)
	}
	if _, err := time.Parse(time.RFC3339, sender.TS); err != nil {
		t.Errorf("sender ts %q is not RFC3339: %v", sender.TS, err)
	}
	// The addressed pid stays the LAST line, so the file still reads as a
	// request addressed to a pid when the header is ignored.
	bodyStr := string(body)
	lines := strings.Split(strings.TrimSpace(bodyStr), "\n")
	final := strings.TrimSpace(lines[len(lines)-1])
	if final != strconv.Itoa(target) {
		t.Errorf("the body's last line = %q, want the addressed pid %d", final, target)
	}

	// LEGACY body (a pre-provenance sender): a bare pid line. The stop must
	// still be honoured (file name) and the sender must read UNSOURCED — never a
	// fabricated identity and never a refused parse.
	legacy := workerStopSender{}
	legacyPath := tools.ControlRequestPath(dir, workerStopRequestPrefix, target)
	if err := tools.WriteControlText(legacyPath, []byte(strconv.Itoa(target)+"\n")); err != nil {
		t.Fatalf("write legacy request body: %v", err)
	}
	legacy = workerStopSenderFromRequest(dir, target)
	if legacy.Sourced() {
		t.Errorf("a legacy bare-pid body was read as a sourced sender: %+v", legacy)
	}
	if legacy.Render() != workerStopUnsourcedMarker {
		t.Errorf("a legacy body renders sender %q, want %q", legacy.Render(), workerStopUnsourcedMarker)
	}
	if !workerDrainRequestedFor(dir, target) {
		t.Error("a legacy request body made the request invisible — a pre-provenance sender must still stop a worker")
	}

	// The FORCE spelling records itself in the same header (the force file is a
	// different NAME, same body contract).
	if err := workerRequestForceFile(dir, target); err != nil {
		t.Fatalf("write force request: %v", err)
	}
	forceBody, err := os.ReadFile(filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(target)))
	if err != nil {
		t.Fatalf("read force request body: %v", err)
	}
	force := parseWorkerStopSender(forceBody)
	if force.Signal != workerStopForceSignalName {
		t.Errorf("the force request records signal %q, want %q", force.Signal, workerStopForceSignalName)
	}
	if !force.Sourced() {
		t.Errorf("the force request carries no sender: %q", string(forceBody))
	}
}
