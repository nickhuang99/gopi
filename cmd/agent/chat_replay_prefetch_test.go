package main

// chat_replay_prefetch_test.go — THE PREFETCH LOOKUP A REPLAY MUST NOT MAKE
// (epic `gopi chat --replay` must be really read-only; the operator's defect B).
//
// WHY THIS IS NOT A UNIT CASE (Rule 31). The claim is not "the flag is armed" —
// it is that the lookup DOES NOT HAPPEN: no handle is dialled, no SQL is issued,
// the ledger's bytes do not move, and the record line says `refused` instead of
// pretending a search ran and found nothing.
//
// THE THREE INSTRUMENTS, each measuring a different part of "it did not run":
//
//	the executor's handle   — nil after the call ⇒ nothing opened the database;
//	the ledger's sha256      — unchanged ⇒ the file was not touched;
//	the record line          — outcome=refused with hits/ms ABSENT ⇒ the run's
//	                           own record does not claim a measurement.
//
// And the CONTROL arm runs the same call with the flag disarmed and requires a
// real retrieval, so the armed arm is evidence rather than tautology.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/testutil"
)

// replayPrefetchLedgerHash is the ledger file's sha256, or "" when the file does
// not exist — read through the same helper before and after so the two readings
// are comparable by construction.
func replayPrefetchLedgerHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read the ledger %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// replayPrefetchLine is the record's own shape with hits/ms as POINTERS — the
// fields whose absence is the claim ("nothing was measured"), which a plain int
// cannot express (Rule 15a). It is a separate decoder from dupRecordLine on
// purpose: that one answers "did the lookup happen twice", this one answers
// "did the record claim a measurement".
type replayPrefetchLine struct {
	Query   string `json:"query"`
	Hits    *int   `json:"hits"`
	Ms      *int64 `json:"ms"`
	Outcome string `json:"outcome"`
	Adopted string `json:"adopted"`
}

// replayPrefetchLastLine returns the last decodable record line, and how many
// lines the record holds — so "the call recorded one more event" is checked, not
// assumed.
func replayPrefetchLastLine(t *testing.T, dir string) (replayPrefetchLine, int) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, config.MemoryPrefetchRecordName))
	if err != nil {
		t.Fatalf("read the prefetch record: %v", err)
	}
	var out []replayPrefetchLine
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var r replayPrefetchLine
		if uerr := json.Unmarshal([]byte(ln), &r); uerr != nil {
			t.Fatalf("the record line %q is not decodable JSON: %v", ln, uerr)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		t.Fatalf("the prefetch record in %s holds no line after a fetch", dir)
	}
	return out[len(out)-1], len(out)
}

// replayEvidenceText returns what the run's replay-evidence file holds, and pins
// WHERE that file is for the duration of the case.
//
// GOPI_HOME is redirected to a temp directory first, because the evidence is
// written into the PROFILE (config.GopiDir → <home>/.gopi/logs) and a test must
// never touch the operator's own profile — the same boundary testutil enforces
// for the ledger (Rule 32).
func replayEvidenceText(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(replayEvidencePath(home))
	if err != nil {
		t.Fatalf("read the replay evidence file: %v — a refused write must stay READABLE, it just must not be drawn", err)
	}
	return string(b)
}

// replayEvidenceHome pins GOPI_HOME for this case and returns it.
func replayEvidenceHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	return home
}

// replayEvidencePath is where agent.RecordReplayMemoryRefusal writes: the
// profile's logs directory under the pinned home (GOPI_HOME is a HOME, so the
// profile root is <home>/.gopi).
func replayEvidencePath(home string) string {
	return filepath.Join(home, config.ProfileDirName, "logs", "replay-memory-refusals.log")
}

// replayPrefetchStderrCapture points the executor's own stderr channel at a file
// for the duration of one construction and returns a reader for what landed
// there. The channel is a PARAMETER of chatPrefetchFor, so the capture measures
// the real destination rather than a substitute.
func replayPrefetchStderrCapture(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create the stderr capture: %v", err)
	}
	t.Cleanup(func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("close the stderr capture: %v", cerr)
		}
	})
	return f
}

// replayPrefetchReadCapture returns what a capture file holds so far.
func replayPrefetchReadCapture(t *testing.T, f *os.File) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(f.Name()))
	if err != nil {
		t.Fatalf("read the stderr capture: %v", err)
	}
	return string(b)
}

// TestReplayBuildsNoMemoryPrefetch is the regression for the DEFECT THIS CHANGE
// CLOSES: a replay used to BUILD the prefetch machinery and ARM it, so every
// typing and turn-boundary guard fired on its own and printed
// `[gopi replay] refused to … memory prefetch …` (plus "no session directory —
// no prefetch record will be written") on a display whose whole job is to show a
// session that already happened. The refusal was right in OUTCOME and wrong in
// PLACE: a replay should never construct the thing.
//
// FOUR INSTRUMENTS, each measuring a different part of "it was not built":
//
//	the returned executor — nil ⇒ no goroutine, no record, nothing to arm;
//	the record file        — ABSENT ⇒ openPrefetchRecord never ran (the pre-fix
//	                         tree CREATED it, and said so on stderr);
//	the stderr             — exactly ONE announcement, and none of the per-lookup
//	                         refusals (the per-event noise is the operator's bug);
//	the CONTROL arm        — disarmed, the SAME call builds a real executor, so
//	                         the armed arm is evidence rather than a tautology.
func TestReplayBuildsNoMemoryPrefetch(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	testutil.NewTestDBPath(t)             // the ledger this arm must never open (Rule 32)
	evidenceHome := replayEvidenceHome(t) // BEFORE any refusal can be recorded
	dir := t.TempDir()
	recordPath := filepath.Join(dir, config.MemoryPrefetchRecordName)

	replaymode.Arm("/a/recording.jsonl")
	t.Cleanup(replaymode.Disarm)
	capture := replayPrefetchStderrCapture(t)
	if p := chatPrefetchFor(dir, func() string { return "" }, capture); p != nil {
		p.Stop()
		t.Fatalf("chatPrefetchFor built a prefetch executor while a replay is armed: this run would arm a typing sampler and a turn-boundary lookup against a session it is only re-reading")
	}
	if _, err := os.Stat(recordPath); !os.IsNotExist(err) {
		t.Fatalf("the replay opened %s (stat err = %v): the record is evidence of a lookup the replay must not make", recordPath, err)
	}
	// THE SCREEN IS SILENT, THE EVIDENCE IS NOT (2026-10-25). The skip used to
	// print its sentence on this stderr — which a chat run turns into the answer
	// band, so a display drawing a finished conversation showed a refusal row per
	// typing event. It now goes to the run's evidence file, so the assertion
	// moved with it: stderr carries NO refusal, the file carries exactly ONE.
	announced := replayPrefetchReadCapture(t, capture)
	if n := strings.Count(announced, replaymode.RefuseMarker); n != 0 {
		t.Fatalf("the replay skip still printed %d refusal(s) on the run's stderr: a replay's screen must show the recording, not this run's refusals;\ncaptured:\n%s", n, announced)
	}
	if lines := strings.Count(strings.TrimSpace(announced), "\n"); lines != 0 {
		t.Fatalf("the skip produced %d line(s) on stderr, want none;\ncaptured:\n%s", lines+1, announced)
	}
	evidence := replayEvidenceText(t, evidenceHome)
	if n := strings.Count(evidence, replaymode.RefuseMarker); n != 1 {
		t.Fatalf("the skip was RECORDED %d time(s), want exactly ONE line for the run (Rule 15: silence on screen is never silence);\nevidence:\n%s", n, evidence)
	}
	if !strings.Contains(evidence, "memory prefetch") {
		t.Fatalf("the recorded refusal does not say WHAT was skipped;\nevidence:\n%s", evidence)
	}
	if strings.Contains(announced, prefetchNoSessionDirMarker) {
		t.Fatalf("the replay's stderr still carries %q — the record is opened before the skip, so construction is not being short-circuited;\ncaptured:\n%s", prefetchNoSessionDirMarker, announced)
	}

	// AN EMPTY SESSION DIRECTORY is the shape the pre-fix tree complained about
	// ("no session directory — no prefetch record will be written"). Under a
	// replay the construction never gets that far, so that sentence must be gone
	// even here.
	emptyCapture := replayPrefetchStderrCapture(t)
	if p := chatPrefetchFor("", func() string { return "" }, emptyCapture); p != nil {
		p.Stop()
		t.Fatalf("chatPrefetchFor built an executor for a replay with no session directory")
	}
	if got := replayPrefetchReadCapture(t, emptyCapture); strings.Contains(got, prefetchNoSessionDirMarker) {
		t.Fatalf("a replay still reports %q: the skip must happen before the record is opened", prefetchNoSessionDirMarker)
	}

	// CONTROL: the same call with the flag cleared builds a real executor and
	// opens its record — the switch is not accidentally off.
	replaymode.Disarm()
	controlCapture := replayPrefetchStderrCapture(t)
	control := chatPrefetchFor(dir, func() string { return "" }, controlCapture)
	if control == nil {
		t.Fatalf("chatPrefetchFor returned nil with the switch ON and no replay armed: the control arm cannot run")
	}
	defer control.Stop()
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("the control arm did not open the prefetch record %s: %v", recordPath, err)
	}
	if got := replayPrefetchReadCapture(t, controlCapture); strings.Contains(got, replaymode.RefuseMarker) {
		t.Fatalf("the control arm announced a replay refusal while this process is NOT replaying;\ncaptured:\n%s", got)
	}
}

// TestReplayRefusesTheMemoryPrefetchLookup is the regression for defect B.
func TestReplayRefusesTheMemoryPrefetchLookup(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	ledger := seedTurnLedger(t) // pins GOPI_MEMORY_DB at a real, seeded ledger
	query := strings.Fields(turnFixtureSubjects[0].topic)
	if len(query) == 0 {
		t.Fatalf("the fixture subject carries no topic words: the lookup would ask nothing")
	}
	q := strings.Join(query, " ")

	// CONTROL: not replaying ⇒ the lookup really runs and really finds rows.
	controlDir := t.TempDir()
	control := chatPrefetchFor(controlDir, func() string { return "" }, os.Stderr)
	if control == nil {
		t.Fatalf("chatPrefetchFor returned nil with the switch ON: the control arm cannot run")
	}
	control.fetch(q)
	line, n := replayPrefetchLastLine(t, controlDir)
	if n != 1 {
		t.Fatalf("the control recorded %d line(s) for one fetch, want 1", n)
	}
	if line.Outcome == prefetchOutcomeRefused {
		t.Fatalf("the control's lookup was REFUSED while this process is NOT replaying: the case below would measure a gate that is always on")
	}
	if line.Hits == nil || *line.Hits == 0 {
		t.Fatalf("the control's lookup returned 0 hits (outcome %q): the fixture ledger answered nothing, so a later refusal could not be told from an empty result", line.Outcome)
	}
	if control.handleRef == nil {
		t.Fatalf("the control never dialled the ledger handle: a lookup that opened nothing cannot be the positive arm")
	}
	control.Stop()

	// ARMED: replaying ⇒ nothing is dialled, nothing is queried, nothing moves.
	before := replayPrefetchLedgerHash(t, ledger)
	armedDir := t.TempDir()
	armed := chatPrefetchFor(armedDir, func() string { return "" }, os.Stderr)
	if armed == nil {
		t.Fatalf("chatPrefetchFor returned nil for the armed arm")
	}
	replaymode.Arm("/a/recording/that/does/not/exist.jsonl")
	defer replaymode.Disarm()
	armed.fetch(q)

	line, n = replayPrefetchLastLine(t, armedDir)
	if n != 1 {
		t.Fatalf("the armed fetch recorded %d line(s), want exactly 1", n)
	}
	if line.Outcome != prefetchOutcomeRefused {
		t.Fatalf("the armed fetch recorded outcome %q, want %q: the record must not report a lookup that never ran", line.Outcome, prefetchOutcomeRefused)
	}
	if line.Hits != nil || line.Ms != nil {
		t.Fatalf("the refused lookup recorded hits=%v ms=%v: nothing was measured, so nothing may be reported as measured (Rule 15a)", line.Hits, line.Ms)
	}
	if armed.handleRef != nil {
		t.Fatalf("the armed fetch dialled the operator's ledger handle: the lookup reached the database before any decision was made about the query")
	}
	if after := replayPrefetchLedgerHash(t, ledger); after != before {
		t.Fatalf("the ledger's bytes changed while a replay was armed:\n before %s\n after  %s", before, after)
	}
	armed.Stop()
}

// TestReplayPrefetchRefusalIsRecordedAndAnnounced keeps the two halves of Rule 15
// on this path: the record line says WHAT was refused, and the sentence reaches
// the run's stderr (the same channel every other refusal in this epic uses).
// Without it, "refused" would be indistinguishable from "the prefetch simply
// found nothing" — and the operator would have no way to know the run was
// drawing from a recording rather than from their own store.
func TestReplayPrefetchRefusalIsRecordedAndAnnounced(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	testutil.NewTestDBPath(t)
	evidenceHome := replayEvidenceHome(t) // pinned BEFORE the refusal is recorded
	dir := t.TempDir()
	p := chatPrefetchFor(dir, func() string { return "" }, os.Stderr)
	if p == nil {
		t.Fatalf("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	replaymode.Arm("/a/recording.jsonl")
	defer replaymode.Disarm()
	p.fetch("some query")

	// THE REFUSAL IS RECORDED, NOT DRAWN (2026-10-25). This case used to require
	// the sentence on stderr — which a chat run turns into the answer band, so the
	// refusal appeared inside the replay's own conversation. The two halves are now
	// in different places, so both are asserted: the run's screen carries nothing,
	// and the run's evidence file carries the sentence that names WHAT stopped.
	evidence := replayEvidenceText(t, evidenceHome)
	if !strings.Contains(evidence, replaymode.RefuseMarker) {
		t.Fatalf("the refusal was RECORDED nowhere: Rule 15 forbids silence, it forbids only the SCREEN;\nevidence:\n%s", evidence)
	}
	if !strings.Contains(evidence, "memory prefetch") {
		t.Fatalf("the recorded refusal does not say WHICH write it stopped;\nevidence:\n%s", evidence)
	}
}

// TestReplayRefusesTheTurnBoundaryLookup pins the SECOND leg (epic defect B,
// measured 2026-10-20): guarding fetch() alone left the record full of `hits`
// rows during a real replay, because the sealed-turn retrieval calls
// prefetchLookupDetailed directly. The case drives the REAL startTurnLookup —
// goroutine included — against a seeded ledger and requires the recorded line to
// say `refused` with hits/ms absent, plus an untouched ledger.
func TestReplayRefusesTheTurnBoundaryLookup(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	ledger := seedTurnLedger(t)
	query := strings.Join(strings.Fields(turnFixtureSubjects[0].topic), " ")
	before := replayPrefetchLedgerHash(t, ledger)

	dir := t.TempDir()
	p := chatPrefetchFor(dir, func() string { return "" }, os.Stderr)
	if p == nil {
		t.Fatalf("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()
	replaymode.Arm("/a/recording.jsonl")
	defer replaymode.Disarm()

	l := p.startTurnLookup(query, chatSealSource("", false, false, nil))
	<-l.done

	if after := replayPrefetchLedgerHash(t, ledger); after != before {
		t.Fatalf("the ledger's bytes changed while the turn-boundary lookup ran under a replay:\n before %s\n after  %s", before, after)
	}
	line, n := replayPrefetchLastLine(t, dir)
	if n != 1 {
		t.Fatalf("the turn lookup recorded %d line(s), want 1", n)
	}
	if line.Outcome != prefetchOutcomeRefused {
		t.Fatalf("the turn lookup recorded outcome %q, want %q — a replay must not report a retrieval it never made", line.Outcome, prefetchOutcomeRefused)
	}
	if line.Hits != nil || line.Ms != nil {
		t.Fatalf("the refused turn lookup recorded hits=%v ms=%v, want both absent", line.Hits, line.Ms)
	}
}
