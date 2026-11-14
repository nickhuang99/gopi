package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// Crash-dump epic (devdoc/crash-dump-design.md): ONE error-collection path
// feeding three consumers —
//  1. session_end "error" block (bounded — rides the JSONL line),
//  2. <session-id>.dump.json crash dump file (complete request + response),
//  3. the task-ledger failed summary tail (notification pipe carries it to PA).
//
// Collection entry points (all funnel through collectErrorRecord):
//   - LLM round failure (runEngine: chatOnce error → the runEngine defer),
//   - runtime panic (runEngine defer recover → record + re-panic),
//   - chat-session failure (CloseChatSession exitCode != 0).
//
// bug036 owns the taxonomy; this file owns the落盘 (what lands where).

// recentToolCallNamesMax bounds the recent-tool-name ring remembered for the
// dump context (Rule 1 — named constant).
const recentToolCallNamesMax = 10

// runErrMarkers are the error-text markers the run-level classifier
// recognises, grouped by the taxonomy category a hit means. They are
// constants because they ARE the classifier's knowledge: a bare literal here
// is a keyword nobody can audit, and the defect this file just fixed was
// exactly a MISSING spelling ("timed out" beside "timeout") that no reader
// could see was missing (Rule 1/13).
//
// Each group is ordered most-specific-first only in the sense that the
// function below tests the groups in one fixed order; within a group the
// order is irrelevant.
var (
	// runErrVendorMarkers: a vendor-protocol error carried by the error TEXT
	// (no HTTP status) — the streaming variant reports its failure this way.
	runErrVendorMarkers = []string{"stream error"}

	// runErrNetworkMarkers: transport-level failures — the provider was
	// unreachable, the request never got an answer. "timed out"/"timeout"
	// both appear because the two spellings come from different callers
	// (Go's net/http says "timeout"/"timed out"; our own timeout wrappers say
	// "timed out"); "context deadline exceeded"/"context canceled" are the
	// two context ends, matched by their frozen stdlib strings.
	runErrNetworkMarkers = []string{
		"timed out", "timeout",
		"context deadline exceeded", "context canceled",
		"connection refused", "EOF", "no such host",
	}
)

// classifyRunErrorText classifies a terminal run error from its TEXT alone —
// the fallback used when the HTTP snapshot says nothing (transport errors, the
// loop's own engine-forced ends). It returns the category, whether the text
// was recognised at all, and nothing else: how text-classified errors score on
// recoverability is the shared taxonomy's call (deepseek), never a second
// verdict grown here.
//
// The recognition set and the recovery rule are separate on purpose. A
// caller-chosen string this file has never seen is NOT evidence that resume
// cannot help — that is the whole defect this function's contract exists to
// prevent (Rule 15a: absence of evidence is not evidence of unrecoverability).
func classifyRunErrorText(msg string) (deepseek.ErrorCategory, bool) {
	// The taxonomy's classification entry point reads the same text: "network"
	// is the only category its taxonomy decides from transport text, and it
	// owns the verdict for a recognised transport failure.
	if matchAnyMarker(msg, runErrVendorMarkers) {
		return deepseek.CategoryVendor, true
	}
	if matchAnyMarker(msg, runErrNetworkMarkers) {
		return deepseek.CategoryNetwork, true
	}
	return deepseek.CategoryUnknown, false
}

// matchAnyMarker reports whether msg carries any of the markers.
func matchAnyMarker(msg string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// The orphan-backfill dump's identity in the JSON itself. Both values are
// written by BackfillOrphanCrashDumps and READ BACK by
// reconcileOrphanDump — the retraction keys on them, so they exist once
// here (Rule 1b/13/14: a reader and a writer must never compare two
// spellings of the same value; a typo in the reader would silently stop
// retracting, with no error).
const (
	// crashDumpDocType is the "type" field of every crash-dump document
	// (the error-path dump in session.go writes it too).
	crashDumpDocType = "crash_dump"

	// dumpHeadTrimmedPrefix is the marker tailJSON prepends when a dump's
	// request body exceeded dumpRequestMax and its head was dropped. It is a
	// named constant because it has a SECOND reader: the resume fallback
	// (resume_dump_fallback.go) must recognise it and refuse, rather than hand
	// a truncated body to the wire (Rules 1/13/14 — a writer and a reader must
	// never carry two spellings of the same marker).
	dumpHeadTrimmedPrefix = "…(head trimmed)…"

	// orphanDumpCategory is the "category" of a backfilled orphan dump. It
	// deliberately does NOT reuse the error taxonomy's crash marker: this
	// dump asserts only that no session_end was recorded, and the
	// retraction is allowed to delete a claim that the session later
	// contradicted.
	orphanDumpCategory = "orphan-no-session-end"

	// legacyOrphanDumpCategory is the category every orphan dump written
	// BEFORE the retraction existed carries ("crash", from the error
	// taxonomy). Those ~117 files on disk are exactly the false positives
	// this change exists to clear, and they cannot be rewritten (a dump is
	// evidence — the fix must not require rewriting existing dumps' format),
	// so the retraction reader accepts BOTH spellings while the writer only
	// ever emits orphanDumpCategory. Deprecated: read-only.
	legacyOrphanDumpCategory = "crash"

	// orphanDumpExitMarkerKey is the field a backfilled orphan dump carries
	// when the dead run left a CHAT EXIT MARKER beside its record
	// (sessionlayout.ExitMarkerName). It turns "this process died" into "this
	// process died HERE", which is the one thing the 2026-10-20 postmortem
	// (session 8616b46c) could not get from any artifact: the marker's phase at
	// death separates "never reached a close-out" (live) from "died inside it"
	// (closing).
	orphanDumpExitMarkerKey = "exit_marker"

	// orphanDumpExitMarkerAbsent is what that field says when the run left NO
	// marker at all — a pre-marker binary, or a death before the REPL armed it.
	// It is stated rather than omitted (Rule 15a): `""` would read as "the
	// marker was there and said nothing".
	orphanDumpExitMarkerAbsent = "absent"
)

// requestDigest summarizes the last LLM request in one bounded line:
// model + message count + last message role + approximate size. The dump
// file carries the FULL body; the digest is what session_end shows.
func requestDigest(req *deepseek.ClaudeRequest) string {
	if req == nil {
		return ""
	}
	lastRole := ""
	if n := len(req.Messages); n > 0 {
		lastRole = req.Messages[n-1].Role
	}
	b, err := json.Marshal(req)
	size := 0
	if err == nil {
		size = len(b)
	}
	return fmt.Sprintf("model=%s messages=%d last_role=%s bytes=%d", req.Model, len(req.Messages), lastRole, size)
}

// collectErrorRecord builds the ErrorRecord for a failed session from
// whatever is available at collection time:
//   - runErr (the run's terminal error) — parsed for stream-error markers;
//   - the deepseek client's LastErrDetail (most recent failed HTTP round-trip);
//   - a.lastClaudeRequest (the request that was in flight / last built);
//   - panicErr/panicStack — set ONLY by the panic-recover path.
//
// Never fails: a nil SessionLog or missing pieces degrade to an
// unknown-category record (or nil when there is genuinely nothing).
func (a *Agent) collectErrorRecord(runErr error, iterations, tokens int, elapsedSec float64) *ErrorRecord {
	er := &ErrorRecord{
		FailedIteration: a.failedIteration,
		RecentToolCalls: append([]string(nil), a.recentToolCallNames...),
		ElapsedSec:      elapsedSec,
		Iterations:      iterations,
		Tokens:          tokens,
		Category:        string(deepseek.CategoryUnknown),
	}

	// Last request digest + full body (dump only).
	er.LastRequestDigest = requestDigest(a.lastClaudeRequest)
	if a.lastClaudeRequest != nil {
		if b, err := json.Marshal(a.lastClaudeRequest); err == nil {
			er.LastRequest = tailJSON(b, dumpRequestMax)
		}
	}

	// HTTP-level failure detail from the client (LLM/stream path).
	if a.Client != nil && a.Client.LastErrDetail.HasFailed() {
		snap := a.Client.LastErrDetail.Snapshot()
		er.Category = string(snap.Category())
		er.HTTPStatus = snap.StatusCode
		er.ErrorBody = snap.Body
		er.TransportErr = snap.TransportErr
		er.LastResponse = snap.Body
		er.RecoverableHint = snap.RecoverableHint()
		// Account-level snapshots carry the operator condition + remedy
		// (empty for every other category) — the evidence a resumer needs to
		// tell "rate limited, retry later" from "account out of balance".
		er.Remedy = tailRunes(snap.RemedyText(), remedyTextMax)
	}

	// Vendor/stream errors arrive as plain errors with no HTTP status —
	// classify from the error text when the snapshot says nothing.
	if er.Category == string(deepseek.CategoryUnknown) && runErr != nil {
		msg := runErr.Error()
		er.ErrorBody = tailRunes(msg, sessionEndErrorBodyMax)
		cat, noted := classifyRunErrorText(msg)
		if noted {
			// A recognised transport/vendor failure. Only the CATEGORY is this
			// classifier's to assert; the recoverability verdict is DERIVED
			// from the taxonomy (a snapshot carrying this text as its
			// transport error is a network failure there, and the taxonomy
			// owns what that means) rather than from a boolean grown here —
			// so the text path and the snapshot path can never disagree
			// (Rule 13/14).
			er.Category = string(cat)
			er.RecoverableHint = deepseek.FailureSnapshot{TransportErr: msg}.RecoverableHint()
		} else {
			// NOTHING recognised the failure, and "not recognised" is not
			// "unrecoverable" (Rule 15a: false ≠ unknown). This arm used to
			// assert `RecoverableHint = false` — a certain "resume cannot
			// help" about an error nobody claimed to understand — while the
			// row's own resume hint still offered `gopi session resume`
			// (taskregister.go), so the ledger carried two contradictory
			// verdicts about the same run. Measured 2026-10-16: the
			// round-boundary fallback spelled its timeouts "agent timed out
			// after N iterations", which matched NONE of the arms above
			// ("timeout" is not a substring of "timed out"), so two real peer1
			// rows read `category=unknown recoverable=false`.
			//
			// NO VERDICT IS ISSUED HERE. The field keeps its type (it is
			// written to the session line and the dump document, whose shape
			// older readers already parse — a migration is not this fix's to
			// make), so the unasserted state needs an IN-BAND spelling: it is
			// `false` alongside the taxonomy's own "an unknowable error"
			// category (deepseek.CategoryUnknown, left as collected above).
			// That pair is what every renderer keys on (recoverableKnown), and
			// it is the ONE combination a genuine verdict cannot produce — a
			// classified failure always carries its own category.
			er.RecoverableHint = false
		}
	}

	// Panic path (recover callers overwrite these).
	if panicContext := panicCollectorLoad(); panicContext != nil {
		er.Category = string(deepseek.CategoryPanic)
		er.ErrorBody = tailRunes(panicContext.msg, sessionEndErrorBodyMax)
		er.PanicStack = panicContext.stack
		er.RecoverableHint = false
	}

	// THE SIGNAL ARM, and it comes FIRST for a reason: a handled signal is the
	// one cause that arrives with NO run error at all, so every arm above has
	// nothing to work with and the record keeps the unknown default — the
	// measured ab9e0023 shape (`category: unknown` beside a clean, deliberate
	// close-out). Reading the carrier's declaration instead of guessing is the
	// whole fix: the name of the signal rides the record, so a later reader
	// reads the cause instead of inferring "something unrecognised happened".
	//
	// A PANIC still outranks it (the collector above ran last and a panic is a
	// crash), and a classified HTTP/vendor failure outranks it too: if the client
	// saw a real failure this run, the signal merely ended a run that was already
	// failing, and reporting "signal" would hide the cause worth fixing.
	if a.CloseSignal != "" && er.Category == string(deepseek.CategoryUnknown) && runErr == nil {
		er.Category = string(deepseek.CategorySignal)
		er.ErrorBody = tailRunes(a.closeSignalSentence(), sessionEndErrorBodyMax)
		// NOT a recoverable failure: there is nothing wrong with the run to
		// resume INTO. `gopi session resume` continues the conversation, which
		// is useful, but no hint may read as "retry and it may work".
		er.RecoverableHint = false
	}

	return er
}

// closeSignalSentence renders the ONE sentence the signal arm records: the
// signal's name plus where it came from, in the vocabulary the rest of the tree
// uses for it (signalrec's spelling reaches here THROUGH the carrier — this
// package never re-spells a signal name, Rule 14).
//
// IT SAYS WHAT DID NOT HAPPEN, TOO. The defect this whole arm exists for was not
// a missing word, it was a MISLEADING one: "unknown" made an orderly terminal
// teardown look like a provider failure. So the sentence states both halves —
// what ended the run, and that the run itself did not fail.
func (a *Agent) closeSignalSentence() string {
	return fmt.Sprintf("the session was ended by %s, a signal this process handled and unwound through its normal close-out — the run did not fail: nothing was in flight and no provider error was recorded (a terminal teardown or a supervisor's stop reaches a chat this way)", a.CloseSignal)
}

// panicContext is the one-slot handoff from the recover clause to the
// collector (single-threaded run path — no lock needed; a slice under the
// struct keeps it simple).
type panicContext struct {
	msg   string
	stack string
}

// panicSlot holds the recovered panic info for the current collection round.
var panicSlot *panicContext

// panicElapsed / panicIterations / panicTokens — the run stats snapshot the
// recover path reads (the defer that recovers the panic cannot see the
// closure variables of runEngine's own stats defer). These are package-level
// because the panic may fire anywhere; they are written by the SAME
// single-threaded run path that reads them on recover (no lock needed in
// practice, atomics keep the race detector quiet). Reset per run.
var (
	panicElapsed    atomicValueFloat
	panicIterations atomicValueInt
	panicTokens     atomicValueInt
)

// atomicValueFloat / atomicValueInt are minimal atomic wrappers (sync/atomic
// based) so the panic-recover path can read run stats without racing the
// single-threaded writer.
type atomicValueFloat struct{ v atomic.Value } // holds float64

func (a *atomicValueFloat) Store(f float64) { a.v.Store(f) }
func (a *atomicValueFloat) Load() float64 {
	if f, ok := a.v.Load().(float64); ok {
		return f
	}
	return 0
}

type atomicValueInt struct{ v atomic.Value } // holds int64

func (a *atomicValueInt) Store(i int64) { a.v.Store(i) }
func (a *atomicValueInt) Load() int64 {
	if i, ok := a.v.Load().(int64); ok {
		return i
	}
	return 0
}

// panicCollectorStore records a recovered panic for collectErrorRecord.
func panicCollectorStore(msg, stack string) {
	panicSlot = &panicContext{msg: msg, stack: stack}
}

// panicCollectorLoad drains the slot (nil when no panic was recovered).
func panicCollectorLoad() *panicContext {
	pc := panicSlot
	panicSlot = nil
	return pc
}

// panicCollectorPeek reports whether a panic was recovered WITHOUT draining
// (used by finishSessionTask to decide whether to collect the panic record).
func panicCollectorPeek() *panicContext {
	return panicSlot
}

// recoverRunPanic is the runEngine defer's panic guard (crash-dump epic ④):
// on panic it writes the crash dump + a session_end(error) event, closes the
// session log, flips the task-ledger row to failed — then RE-PANICS so the
// process exit code stays non-zero (recover must never swallow the failure).
//
// panic-ok: the RE-PANIC is the contract (Rule 19 is about panicking INSTEAD of
// returning an error; this function re-raises the panic it caught).
func (a *Agent) recoverRunPanic(panicErr any, finish func(exitCode, iterations, tokens int, elapsedSec float64)) {
	msg := fmt.Sprintf("%v", panicErr)
	stack := string(debug.Stack())
	panicCollectorStore(msg, stack)

	elapsed := panicElapsed.Load()
	iterations := panicIterations.Load()
	tokens := panicTokens.Load()

	if a.SessionLog != nil {
		failure := a.collectErrorRecord(nil, int(iterations), int(tokens), elapsed)
		a.SessionLog.WriteCrashDump(failure)
		// The session_end line is written via the same log path (direct
		// event — LogSessionEnd with the panic record).
		_ = a.SessionLog.LogSessionEnd(1, int(iterations), int(tokens), elapsed, failure, EndCause{Reason: sessionevent.SessionEndReasonPanic}) //stylegate:ignore crash-dump path: the panic record is written first and the process is dying
		a.SessionLog.Close()
	}
	if finish != nil {
		finish(1, int(iterations), int(tokens), elapsed)
	}

	// Re-panic — the exit code and the Go runtime's stderr traceback stay
	// intact (recover is for the breadcrumb, not for masking the crash).
	panic(panicErr)
}

// BackfillOrphanCrashDumps is the startup orphan detector (crash-dump epic
// BackfillOrphanCrashDumps is the startup orphan detector (crash-dump epic
// ② kill -9 case): it classifies every session JSONL via
// ClassifyOrphanSession and writes <id>.dump.json skeletons for the ones
// that are provably crashed — writer process gone AND no session_end marker.
// A JSONL still being written is a live session, not a crash, so the sweep is
// limited to paths QUIET past the live window (DetectOrphanSessions).
// Best-effort — never blocks startup.
//
// Two reconciliation steps keep the count honest, and they exist because the
// pre-fix predicate ("no session_end tail") over-reported: 117 of 473 dumps
// on the reference machine described sessions that went on to end cleanly.
//
//  1. retraction: a dump is a claim that a session died without an end. Once
//     the session writes its session_end the claim is false and the dump is
//     removed — including one written in an earlier round for a session that
//     was merely slow (the ~117 legacy dumps), and including a session that
//     is still running (it has no end yet either). Only dumps that still
//     self-describe as orphan backfills are touched, matched by the
//     "<session_id>" field inside the JSON, so a hand-written or error-path
//     dump is never deleted here.
//  2. only the surviving dumps are counted, so the startup line can never
//     re-announce an old, already-explained death.
//
// Returns (dumps written, stale dumps retracted). A genuinely crashed
// session is unaffected: its JSONL stops being written at the moment of
// death, so it is still dumped once past the window (kill -9 / panic /
// power loss visibility preserved).
// currentSessionID, when non-empty, is excluded (the process's own session
// has no session_end YET — it is alive, not an orphan); the liveness clause
// already covers it, so this is belt-and-braces for ancestor processes.
// The dump cannot reconstruct the request (that died with the process) —
// it records the verdict (no session_end = no recorded end) and the log
// tail so PA can triage without opening the JSONL.
func BackfillOrphanCrashDumps(currentSessionID ...string) (written, retracted int) {
	orphans := DetectOrphanSessions(currentSessionID...)
	for _, p := range orphans {
		dumpPath := strings.TrimSuffix(p, sessionJSONLSuffix) + dumpFileSuffix
		if fileExists(dumpPath) {
			continue // already dumped by an earlier, still-valid sweep
		}
		// THE SESSION'S IDENTITY, RESOLVED ONCE (internal/sessionlayout). Deriving
		// it from the basename wrote "session" for every bare-session record —
		// the record's fixed file name — so the dump named a session that does
		// not exist and the retraction below could never compare it honestly.
		sessionID := sessionlayout.SessionID(p)
		doc := map[string]interface{}{
			"type":             crashDumpDocType,
			"session_id":       sessionID,
			"timestamp":        nowUTC().Format(timeRFC3339),
			"category":         orphanDumpCategory,
			"orphan":           true,
			"session_log":      p,
			"recoverable_hint": true, // resume can replay from the JSONL tail
			"note":             "process died without session_end (kill/panic/power loss); no request context recoverable — see session_log",
			// HOW FAR THE RUN GOT, read from the dead run's own marker rather
			// than guessed from which artifacts are missing (session 8616b46c
			// is the case: the directory proved four guards wrote nothing, and
			// could not say why). Absent is stated, never omitted.
			orphanDumpExitMarkerKey: chatExitMarkerVerdict(p),
		}
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			continue
		}
		if err := osWriteFile(dumpPath, out); err != nil {
			continue
		}
		written++
	}
	return written, reconcileStaleOrphanDumps(currentSessionID...)
}

// reconcileStaleOrphanDumps removes the orphan dumps whose session has since
// recorded its session_end and returns how many were removed. It walks the
// SAME tree shape as DetectOrphanSessions (flat files + one level into each
// session dir) so the two passes cannot drift apart — both now take their
// candidate PATHS from internal/sessionlayout.Records, the shared
// layout-keyed scan — and it never touches a dump that is missing,
// unreadable, not the crash-dump family, not marked orphan, or that names a
// different session than the JSONL it sits next to.
//
// The scan is keyed on each session RECORD (not on a "*.jsonl" glob), so a
// session dir's side files (signal-provenance.jsonl) are never mapped onto a
// dump path: the dump family is derived from the record's own name, and a
// side file's stem has no dump to retire.
func reconcileStaleOrphanDumps(excludeSessionIDs ...string) int {
	excl := make(map[string]bool, len(excludeSessionIDs))
	for _, id := range excludeSessionIDs {
		excl[strings.TrimSpace(id)] = true
	}
	dir, err := config.SessionsRoot()
	if err != nil {
		return 0
	}
	records, err := sessionlayout.Records(dir)
	if err != nil {
		return 0
	}
	retracted := 0
	for _, r := range records {
		dumpPath := orphanDumpPath(r.Path)
		if !fileExists(dumpPath) {
			continue // no dump of this session — nothing to reconcile
		}
		retracted += reconcileOrphanDump(dumpPath, excl)
	}
	return retracted
}

// reconcileOrphanDump removes one dump file when every one of these holds:
// the session inside it is not excluded, the JSONL it belongs to now HAS a
// session_end, and the dump is still an orphan backfill of that same session.
// Returns 1 when the dump was removed.
func reconcileOrphanDump(dumpPath string, excl map[string]bool) int {
	raw, err := osReadFile(dumpPath)
	if err != nil {
		return 0 // unreadable: the dump stays (never delete what cannot be read)
	}
	var doc struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		Category  string `json:"category"`
		Orphan    bool   `json:"orphan"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0 // not our format — leave it alone
	}
	if doc.Type != crashDumpDocType || !doc.Orphan || !isOrphanDumpCategory(doc.Category) {
		return 0
	}
	// BOTH SIDES OF THE COMPARISON COME FROM THE SESSION, never from the dump's
	// own file name. Deriving the expected id from the dump's basename made the
	// check agree with itself: a dump named <dir>/session.dump.json yielded
	// "session", the claim (written by the same basename rule) was "session",
	// and a WRONG id passed. The record beside the dump is what the dump is
	// ABOUT, so it is what the claim is verified against.
	jsonlPath := strings.TrimSuffix(dumpPath, dumpFileSuffix) + sessionJSONLSuffix
	if !fileExists(jsonlPath) {
		return 0 // no record to check the claim against — never delete on a guess
	}
	sessionID := sessionlayout.SessionID(jsonlPath)
	// The pre-retraction writer stored the JSONL BASENAME in session_id
	// (".jsonl" included: "abc.jsonl"); the current one stores the bare id.
	// Accept both spellings — the legacy one is what every dump of the
	// false-positive backlog carries, so rejecting it would leave exactly the
	// files this change exists to clear.
	claimed := strings.TrimSuffix(doc.SessionID, sessionJSONLSuffix)
	if sessionID == "" || excl[sessionID] || claimed != sessionID {
		return 0
	}
	if !HasSessionEnd(jsonlPath) {
		return 0 // still no recorded end — the claim may still be true
	}
	if err := osRemove(dumpPath); err != nil {
		return 0
	}
	return 1
}

// orphanDumpPath maps a session JSONL path to its crash-dump sibling.
func orphanDumpPath(sessionJSONLPath string) string {
	return strings.TrimSuffix(sessionJSONLPath, sessionJSONLSuffix) + dumpFileSuffix
}

// retireStaleOrphanDumps removes crash dumps whose session is NOT in the
// current crashed set — i.e. sessions that ended cleanly (a session_end was
// written after the dump) or that are still running. Without this the 117
// misreported legacy dumps stay on disk forever, asserting a crash that
// never happened, and every future `gopi session log --crashes` reader believes
// them. Best-effort: an unreadable/unremovable dump is left alone (it is
// not counted either way — the count comes from the classification, never
// from the directory listing).
func retireStaleOrphanDumps(crashed map[string]bool) {
	for _, dumpPath := range listOrphanDumpPaths() {
		// <id>.dump.json → the session JSONL it describes. The dump lives
		// next to its session in the same run dir (legacy flat form) or in
		// the per-session dir; both are basename-relative, so the mapping is
		// the inverse of orphanDumpPath.
		sessionPath := strings.TrimSuffix(dumpPath, dumpFileSuffix) + sessionJSONLSuffix
		if crashed[sessionPath] {
			continue
		}
		verdict := ClassifyOrphanSession(sessionPath)
		if verdict.Status == OrphanCrashed {
			// Session unreachable from this scan (deleted log) — leave the
			// dump: it is the only surviving evidence. Do not count it.
			continue
		}
		if verdict.Status == OrphanLive && verdict.PID == 0 {
			continue // defensive: a live verdict always names a PID
		}
		// Live (clean end / running) or unclassifiable: the dump's crash
		// claim cannot be sustained, so it is retired.
		if err := os.Remove(dumpPath); err != nil {
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: could not retire stale dump %s: %v\n", dumpPath, err)
		}
	}
}

// listOrphanDumpPaths returns every crash-dump file under the sessions tree
// (legacy flat <id>.dump.json and per-session <dir>/<id>.dump.json), in the
// same layout DetectOrphanSessions walks so the retire sweep and the
// detection agree on what exists. Best-effort: an unreadable tree yields nil.
// listOrphanDumpPaths returns the crash-dump files of the sessions tree, one
// level into each session dir (flat form) or at the root (legacy form).
//
// DERIVED FROM THE RECORDS, not from a "*.dump.json" glob: the dump family is
// defined as "the sibling of a session RECORD", so scanning for the dump
// suffix independently would also pick up a stray *.dump.json belonging to
// nothing (and would have to re-implement the tree walk). The unit is the
// session, and the record is what identifies it — internal/sessionlayout
// decides which "*.jsonl" is that record.
func listOrphanDumpPaths() []string {
	dir, err := config.SessionsRoot()
	if err != nil {
		return nil
	}
	records, err := sessionlayout.Records(dir)
	if err != nil {
		return nil
	}
	var dumps []string
	for _, r := range records {
		p := orphanDumpPath(r.Path)
		if fileExists(p) {
			dumps = append(dumps, p)
		}
	}
	return dumps
}

// sessionEndErrorContext exists so the runEngine defer can hand the END of a
// failed ctx into the collector when the failure IS a context error.
var _ = context.Canceled // (context import kept for signature parity)

// fileExists reports whether path exists (any type).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fileBaseName is filepath.Base (alias kept for readability in backfill).
func fileBaseName(path string) string {
	return filepath.Base(path)
}

// isOrphanDumpCategory reports whether a dump's "category" is one an orphan
// backfill ever wrote: the current orphanDumpCategory, or the
// legacyOrphanDumpCategory that pre-retraction dumps carry (those ARE the
// backlog of false positives — refusing them would leave the ~117 stale
// dumps in place). Any other category — an error-path dump's taxonomy value
// above all — is a different claim and is left untouched.
func isOrphanDumpCategory(category string) bool {
	return category == orphanDumpCategory || category == legacyOrphanDumpCategory
}

// osReadFile reads a whole file (indirection for tests).
func osReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// osRemove deletes one file (indirection for tests).
func osRemove(path string) error {
	return os.Remove(path)
}

// nowUTC is time.Now().UTC() (test seam).
func nowUTC() time.Time {
	return time.Now().UTC()
}

// timeRFC3339 is the timestamp layout used in dumps.
const timeRFC3339 = time.RFC3339

// osWriteFile writes data to path (0o644) — indirection for tests.
func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// tailJSON returns at most max trailing bytes of a JSON document, keeping it
// parseable-ish: the head is trimmed INSIDE the array of messages (the
// system prompt head is the least useful part of a failed request). When the
// document is small it is returned verbatim.
func tailJSON(b []byte, max int) json.RawMessage {
	if len(b) <= max {
		return json.RawMessage(b)
	}
	// Simple tail semantics (same convention as tailRunes): head-trim with a
	// marker. The dump consumer knows "…(head trimmed)…" is not valid JSON —
	// it is a replay aid, not a replayable artifact.
	return json.RawMessage(dumpHeadTrimmedPrefix + string(b[len(b)-max:]))
}
