package signalrec

// signalrec_sessionlog_test.go — the guard for the verdict event a killed
// process writes into its OWN session log (epic signalrec-session-log-verdict,
// 2026-09-17).
//
// THE DEFECT THIS PINS. The recorder documented a session-log verdict line
// (behind the now-retired SessionLogVerdictEnv) that had NEVER been written by
// any process: the sole production caller was `Install("")` in cmd/agent/main.go
// and the writer took the session-log PATH as its own parameter, so the value
// that reached it was that literal `""` — the writer returned immediately and
// the documented line was written by nobody. A documented behaviour with no
// reachable writer is a Rule 15 docs-lie, and a unit test that drove the writer
// directly could not see it: it satisfied itself through the very dead argument
// the bug consisted of.
//
// HOW THE GUARD AVOIDS THAT VACUITY. The victim child is given a session log
// through the POST-Install seam ONLY — `SetSessionDir(dir, taskID, sessionLog)`
// — while `Install` is called with the empty path production calls it with. The
// assertion is that the event lands in the file named by SetSessionDir's third
// argument, so the test cannot pass via Install's argument at all, and it turns
// RED the moment the production resolution of that path is removed.
//
// The written file is then read back through internal/gopilog.ParseFile — the
// SAME parser the session-log readers use (`gopi session log --jsonl/--chat`, `gopi
// stats`) — so "no line is silently skipped" is asserted with the reader's own
// instrument rather than a hand-rolled one. Every other session-log parser in
// that package (ValidateSessionFile, SessionRawLogPath) skips an unparsable line
// silently, so the strict one is the one that must be green.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// srSessionLogEnv is the child hook that turns the re-executed test binary into
// the session-log victim: install the recorder exactly as cmd/agent/main.go does,
// then hand over the session log exactly as printLogPaths does once the run's
// logger exists.
const srSessionLogEnv = "GOPI_SIGNALREC_TEST_SESSION_LOG_VICTIM"

// srSessionLogID is the session uuid the fixture's session log belongs to.
const srSessionLogID = "5f1c0a3e-7d24-4a9b-8f0c-2b6d1e4a9c77"

// srWantSignalWord is the signal vocabulary the record uses (classifySignal
// renders the operator-facing word, never a bare number).
const srWantSignalWord = "SIGTERM"

// srSessionLogWaitBudget / srSessionLogPollInterval bound the wait for the
// appended verdict event (a fixed sleep is the flaky shape — the handler writes
// asynchronously, from the goroutine the signal wakes).
const (
	srSessionLogWaitBudget   = 30 * time.Second
	srSessionLogPollInterval = 10 * time.Millisecond
)

// srRunSessionLogVictim is the victim process's body, run from TestMain: it
// installs the recorder the way main.go does (Install with no log path — at that
// point in main no run has minted a logger, so there is no path to name), then
// hands over the run's own session log the way printLogPaths does, then announces
// readiness and blocks until signalled.
//
// It deliberately touches NOTHING but the public seam — no in-package writer, no
// test-only entry point — so a wiring regression in production is what makes the
// test that drives it fail.
func srRunSessionLogVictim(sessionLog string) {
	uninstall := Install("")
	defer uninstall()
	SetSessionDir(sessionlayout.RecordContainer(sessionLog), "", sessionLog)
	srAnnounceReady()
	select {} // block until signalled; the parent does the killing
}

// TestSignalRecVerdictEventReachesTheSessionLogFromTheSetSessionDirSeam is the
// end-to-end guard: a real gopi-shaped process, handed a session log through the
// production seam and nothing else, appends its verdict event to THAT file on
// SIGTERM — and the file still parses, every line, with the session readers' own
// parser.
func TestSignalRecVerdictEventReachesTheSessionLogFromTheSetSessionDirSeam(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, srSessionLogID)
	sessionLog := filepath.Join(dir, sessionlayout.RecordName)
	// The session file as the run's own logger would have written it: one
	// session_start event, which is what makes this file the session RECORD
	// (internal/sessionlayout) rather than a stray JSONL.
	if err := os.WriteFile(sessionLog, []byte(srSessionStartLine(t)+"\n"), 0o644); err != nil {
		t.Fatalf("write the session log %s: %v", sessionLog, err)
	}
	before := srFileSize(t, sessionLog)

	ready := srAnticipateChildReady(t, home)
	// The child's environment states ONE fact: which session log the run handed
	// over. The session DIR arrives the same way production delivers it — through
	// the in-process SetSessionDir call in the child — so this test cannot be
	// satisfied by an env var standing in for the wiring.
	env := srAppendEnv(os.Environ(), config.GopiDirEnv, home)
	for _, k := range []string{SessionDirEnv, TaskIDEnv, EnvAgentChild, taskRunEnvName, wrapperEnvName} {
		env = srAppendEnv(env, k, "")
	}
	env = srAppendEnv(env, srReadyEnv, ready[srReadyEnv])
	env = srAppendEnv(env, srSessionLogEnv, sessionLog)
	cmd := exec.Command(os.Args[0])
	cmd.Env = env
	cmd.Stderr = os.Stderr
	// Bind the victim to this test binary's lifetime before it starts:
	// the child must be armed with the death pipe this process holds, and
	// the guard (run after the readiness handshake) must find its verdict.
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecVerdictEventReachesTheSessionLogFromTheSetSessionDirSeam")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the session-log victim: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }() //stylegate:ignore teardown of a fixture child killed after its verdict was polled; the assertion is the file, not the wait status
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	// SIGTERM: the recorder must append its verdict event to the session log the
	// run handed it. Nothing else writes to this file, so a growing file IS the
	// writer under test.
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal the victim: %v", err)
	}
	srWaitForSessionLogGrowth(t, sessionLog, before)

	// NO LINE IS SILENTLY SKIPPED. This mirrors the strict parser's own algorithm
	// (internal/gopilog.parseFileCeiling): read the file JSONL-wise and
	// json.Unmarshal EVERY line, with a hard failure on any of them. It is the arm
	// that matters, because the package's OTHER parsers (ValidateSessionFile,
	// SessionRawLogPath) unmarshal each line and silently `continue` on a failure
	// — a prose line would have been skipped by those and reported as a hard "JSON
	// parse error" by the unified reader.
	//
	// The reader-side proof with the real parser (internal/gopilog.ParseFile,
	// behind `gopi session log --jsonl/--chat` and `gopi session stats`) lives in the package that
	// owns it — internal/gopilog/session_signal_verdict_test.go — because
	// importing it HERE would close an import cycle (gopilog → agent → tools →
	// signalrec). That test drives this same writer and reads the file back with
	// the SAME open-time discipline (O_APPEND here; the read is the reader's).
	lines := srParseEveryLine(t, sessionLog)
	if len(lines) != 2 {
		t.Fatalf("session log has %d events, want 2 (the run's session_start plus the verdict event):\n%s", len(lines), srReadWholeFile(t, sessionLog))
	}
	ev := srFindEvent(t, sessionLog, sessionevent.EventTypeSignalVerdict)
	if got := ev[signalKeySignal]; got != srWantSignalWord {
		t.Errorf("verdict event %s = %v, want %q (the signal must be readable out of the log)", signalKeySignal, got, srWantSignalWord)
	}
	// signalKeyPID is written under the record's own JSON tag, so the event and
	// the provenance trail name the dying process the same way.
	if got := ev[signalKeyPID]; got == nil {
		t.Error("verdict event carries no pid — the event must name the process that died")
	}
	// The verdict travels in the record's own SourcedField, so the operator's jq
	// filter over the SESSION LOG is the one they already run over the provenance
	// trail. Nothing filed an intent against this victim, and its session dir WAS
	// handed over — so "this dir holds no intent naming me" is a READ ANSWER, not
	// a gap, and the kill is UNSOURCED. The distinction is the whole point of the
	// three states: an ABSENT field means the question was never answered, and the
	// two must not be merged.
	sourced, present := ev[SourcedField]
	if !present {
		t.Fatalf("verdict event carries no %s field for a kill whose provenance WAS answerable (no intent naming it in its own session dir) — an unanswerable question and an answered one must not look the same in the log", SourcedField)
	}
	if sourced != false {
		t.Errorf("verdict event %s = %v, want false — nothing announced this kill", SourcedField, sourced)
	}
}

// TestSignalRecEveryCapturedSignalIsWrittenToTheSessionLog is the arm that kept
// the ownership PREDICATE and demoted it from gate to CONTENT.
//
// The pre-fix writer returned "" for a process that does not own its
// terminating-signal handlers: the "nobody announced this kill" case — the exact
// death an operator investigates after a mass kill — was the one that got NO
// line, while its harmless sibling did. The predicate did not disappear with the
// knob: it is the event's SourcedField (true = a gopi stop path announced the
// kill, false = nobody did), so the verdict is stated instead of filtered, and
// the re-raise half still uses the SAME predicate for its own decision
// (signalForwarding — one predicate, two consumers, Rule 13; the other half has
// its own guards: TestSignalRecForwardingPreservesDefaultDeath and
// TestSignalRecClaimedHandlerIsNotReRaised).
//
// Both arms run in THIS process: the process-local declaration is flipped through
// the cross-process channel that feeds the same predicate (envOwnsHandlers).
func TestSignalRecEveryCapturedSignalIsWrittenToTheSessionLog(t *testing.T) {
	t.Setenv(EnvNoForward, "")
	t.Setenv(envOwnsHandlers, "")

	sourced := false
	rec := record{Signal: "SIGTERM", Sourced: &sourced}

	// ARM A — no declaration: the unclassifiable argv shape (a bare-prompt run),
	// which signalForwarding reports as NOT owning handlers. The kill is still the
	// fact the log exists for, so the event must be rendered here.
	if !forwardsDefault(roleFromArgv(os.Args)) {
		t.Fatalf("this fixture assumes an unclassifiable argv; roleFromArgv(os.Args) = %q owns handlers", roleFromArgv(os.Args))
	}
	if !signalForwarding() {
		t.Fatal("the fixture assumes signalForwarding() is true (no handler claim) for this argv")
	}
	unowned := sessionLogVerdictEvent(rec)
	if len(unowned) == 0 {
		t.Fatal("no verdict event for a process that does NOT own its signal handlers — that is the unsourced mass-kill shape, and filtering it out is how the run that lost work writes the least evidence into the log an operator reads afterwards")
	}
	srAssertEventVerdict(t, unowned, false)

	// ARM B — the declaration. It must not change WHETHER the event is written
	// (it is, in both arms) but it must be visible in what the event says when
	// the kill was announced.
	t.Setenv(envOwnsHandlers, "1")
	claimed := sessionLogVerdictEvent(record{Signal: "SIGTERM", Sourced: &sourced})
	srAssertEventVerdict(t, claimed, false)

	announced := true
	srAssertEventVerdict(t, sessionLogVerdictEvent(record{Signal: "SIGTERM", Sourced: &announced}), true)

	// THE THIRD STATE — an UNCLASSIFIED kill: the question could not be answered,
	// so the field is ABSENT rather than false. "Nobody announced this" and "we
	// could not find out" are different claims and the log must not merge them.
	var ev map[string]any
	if err := json.Unmarshal(sessionLogVerdictEvent(record{Signal: "SIGTERM"}), &ev); err != nil {
		t.Fatalf("decode the unclassified verdict event: %v", err)
	}
	if _, present := ev[SourcedField]; present {
		t.Errorf("an UNCLASSIFIED kill wrote %s=%v — a guess where the honest answer is that no question could be answered", SourcedField, ev[SourcedField])
	}
	if ev[envelopeKeyType] != sessionevent.EventTypeSignalVerdict {
		t.Errorf("verdict event %s = %v, want %q", envelopeKeyType, ev[envelopeKeyType], sessionevent.EventTypeSignalVerdict)
	}
}

// srAssertEventVerdict decodes one encoded verdict event and asserts the wire
// facts every reader depends on: the type, and the verdict value in the record's
// own field name.
func srAssertEventVerdict(t *testing.T, line []byte, want bool) {
	t.Helper()
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		t.Fatalf("the verdict event %q is not one JSON object: %v", line, err)
	}
	if ev[envelopeKeyType] != sessionevent.EventTypeSignalVerdict {
		t.Errorf("verdict event %s = %v, want %q", envelopeKeyType, ev[envelopeKeyType], sessionevent.EventTypeSignalVerdict)
	}
	got, present := ev[SourcedField]
	if !present {
		t.Fatalf("verdict event carries no %s field: %v", SourcedField, ev)
	}
	if got != want {
		t.Errorf("verdict event %s = %v, want %v", SourcedField, got, want)
	}
}

// TestSignalRecInstallArgumentIsLiveAndTheLaterHandoffWins pins the closure of
// the docs-lie the epic names: the session-log path Install is given is a LIVE
// half of the handoff, not the write-only literal "" the pre-fix writer took its
// path from. It also pins the precedence resolveSessionLog states ONCE (Rule 2's
// discipline applied to a fact): the path a run hands over later — its own logger
// — wins, and clearing it falls back to what Install was given.
func TestSignalRecInstallArgumentIsLiveAndTheLaterHandoffWins(t *testing.T) {
	home := srHome(t)
	installLog := filepath.Join(t.TempDir(), "known-before-the-logger.jsonl")
	uninstall := Install(installLog)
	defer uninstall()
	if got := resolveSessionLog(); got != installLog {
		t.Fatalf("resolveSessionLog = %q with Install(%q) — the argument must be recorded and read, or it is the dead \"\" the defect consisted of", got, installLog)
	}

	// The run mints its logger: the handoff it makes later is the authority.
	dir := srSessionDir(t, home, "install-arg-session")
	ownLog := filepath.Join(dir, sessionlayout.RecordName)
	SetSessionDir(dir, "", ownLog)
	if got := resolveSessionLog(); got != ownLog {
		t.Errorf("resolveSessionLog = %q after SetSessionDir handed over %q — the run's own logger must win over the earlier argument", got, ownLog)
	}

	// Clearing the handoff (a following run in the same process) uncovers the
	// argument again rather than leaving a stale path behind.
	SetSessionDir("", "", "")
	if got := resolveSessionLog(); got != installLog {
		t.Errorf("resolveSessionLog = %q after the handoff was CLEARED, want the Install argument %q — a cleared handoff must not keep naming the previous run's log", got, installLog)
	}
}

// srParseEveryLine reads path the way internal/gopilog's unified parser does —
// one JSON object per line, every line unmarshalled, a hard failure on any
// unparsable line — and returns the decoded events.
func srParseEveryLine(t *testing.T, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split([]byte(srReadWholeFile(t, path)), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("session-log line %q is not JSON (%v) — every line of a session log must parse, or the unified reader (gopilog.ParseFile) fails on the whole file", line, err)
		}
		out = append(out, ev)
	}
	return out
}

// srSessionStartLine renders the session_start event the fixture writes before
// the victim starts.
func srSessionStartLine(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		envelopeKeyType:      sessionevent.EventTypeSessionStart,
		envelopeKeyTimestamp: time.Now().Format(time.RFC3339Nano),
		"sessionId":          srSessionLogID,
		"model":              "fixture-model",
	})
	if err != nil {
		t.Fatalf("marshal the fixture session_start: %v", err)
	}
	return string(b)
}

// srFindEvent returns the FIRST event of the given type, read the way an
// operator's jq/grep reads it: one JSON object per line.
func srFindEvent(t *testing.T, path, typ string) map[string]any {
	t.Helper()
	for _, line := range bytes.Split([]byte(srReadWholeFile(t, path)), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("session-log line %q is not JSON: %v", line, err)
		}
		if ev[envelopeKeyType] == typ {
			return ev
		}
	}
	t.Fatalf("no %q event in %s:\n%s", typ, path, srReadWholeFile(t, path))
	return nil
}

// srReadWholeFile returns a file's content, failing the test when it cannot be
// read.
func srReadWholeFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// srFileSize returns the file's size, failing the test when it cannot be
// stat'ed.
func srFileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Size()
}

// srWaitForSessionLogGrowth blocks, bounded, until path is larger than size —
// the appended verdict event.
func srWaitForSessionLogGrowth(t *testing.T, path string, size int64) {
	t.Helper()
	deadline := time.Now().Add(srSessionLogWaitBudget)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Size() > size {
			return
		}
		time.Sleep(srSessionLogPollInterval) //stylegate:allow-sleep poll interval for the appended verdict event; bounded by the loop deadline
	}
	t.Fatalf("the session log %s did not grow past %d bytes within %s — the verdict event never reached the session log the run handed over:\n%s",
		path, size, srSessionLogWaitBudget, srReadWholeFile(t, path))
}
