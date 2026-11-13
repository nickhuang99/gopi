package capturesurvey

// capturesurvey_test.go — the survey's own proofs: the READER and the
// ENUMERATION. Nothing here asserts what a gate captures (that verdict comes
// through the Classify callback, and it is agent's classifier — asserted equal
// on the cmd/agent side, where both are reachable).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// surveyTestTier is the fake verdict vocabulary these tests classify with: the
// reader must not care what the tiers ARE, only that a tier and a reason come
// back and are counted where they belong.
const (
	surveyTestTier   = "shape"
	surveyTestReason = "too-short"
)

// surveyFakeClassify answers a fixed verdict, keyed on nothing: the reader's
// tests are about COUNTING, not about the gate.
func surveyFakeClassify(body string) (string, string) {
	if len(body) >= 10 {
		return surveyTestTier, ""
	}
	return "", surveyTestReason
}

// surveyFixture writes one session log and returns its directory.
func surveyFixture(t *testing.T, events ...map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	session := filepath.Join(dir, "session-1")
	if err := os.MkdirAll(session, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(session, SurveySessionFileName))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	for _, ev := range events {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

// surveyAssistantEvent builds an assistant event carrying the given text blocks.
func surveyAssistantEvent(texts ...string) map[string]any {
	blocks := make([]any, 0, len(texts))
	for _, s := range texts {
		blocks = append(blocks, map[string]any{"type": "text", "text": s})
	}
	return map[string]any{"type": "assistant", "message": map[string]any{"content": blocks}}
}

// surveyToolEvent is an assistant event carrying NO text — the common round.
func surveyToolEvent() map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "bash"}},
	}}
}

// TestSurveyCountsBodiesRoundsAndTiers is the survey's core claim: a body is
// counted once, a round is counted once, and a tier lands in its own bucket.
func TestSurveyCountsBodiesRoundsAndTiers(t *testing.T) {
	dir := surveyFixture(t,
		map[string]any{"type": "request"},
		surveyAssistantEvent("aaaaaaaaaaaaaaa"), // 15 runes → captured
		surveyToolEvent(),                       // no text → not a body
		map[string]any{"type": "request"},
		surveyAssistantEvent("short"), // 5 runes → refused
	)
	s, err := Survey(dir, surveyFakeClassify)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if s.Files != 1 {
		t.Errorf("files = %d, want 1", s.Files)
	}
	if s.Rounds != 2 {
		t.Errorf("rounds = %d, want 2 — one per request event", s.Rounds)
	}
	if s.Bodies != 2 {
		t.Errorf("bodies = %d, want 2 — a tool-only round is not a body", s.Bodies)
	}
	if s.Tiers[surveyTestTier] != 1 {
		t.Errorf("tier %q = %d, want 1", surveyTestTier, s.Tiers[surveyTestTier])
	}
	if s.Reasons[surveyTestReason] != 1 {
		t.Errorf("reason %q = %d, want 1", surveyTestReason, s.Reasons[surveyTestReason])
	}
	if s.CapturedBodies != 1 || s.CapturedRounds != 1 {
		t.Errorf("captured = %d bodies / %d rounds, want 1/1", s.CapturedBodies, s.CapturedRounds)
	}
}

// TestSurveyJoinsMultipleTextBlocksInOneEvent pins the body's definition: an
// event's text blocks are ONE body, joined — not one body each. Measured on the
// corpus this was written for: every assistant event carries exactly one
// content block, so the joined rule and the per-block rule agree there; stating
// the joined rule is what keeps a future multi-block round from being counted
// twice.
func TestSurveyJoinsMultipleTextBlocksInOneEvent(t *testing.T) {
	dir := surveyFixture(t, surveyAssistantEvent("first half, long enough", "second half, also long"))
	s, err := Survey(dir, surveyFakeClassify)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if s.Bodies != 1 {
		t.Fatalf("bodies = %d, want 1 (the event's blocks are ONE body)", s.Bodies)
	}
	if s.Runes[0] != len([]rune("first half, long enough\nsecond half, also long")) {
		t.Fatalf("body runes = %d — the blocks were not joined by newline", s.Runes[0])
	}
}

// TestSurveyCountsAnUnreadableLineAndNeverInventsABody is Rule 15 on the reader:
// a malformed line is stepped over (it cannot be one of the events this reader
// cares about), while a line over the cap FAILS LOUDLY rather than silently
// truncating the corpus.
func TestSurveyCountsAnUnreadableLineAndNeverInventsABody(t *testing.T) {
	dir := surveyFixture(t, surveyAssistantEvent("aaaaaaaaaaaaaaa"))
	path := filepath.Join(dir, "session-1", SurveySessionFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.WriteString("{not json at all\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s, err := Survey(dir, surveyFakeClassify)
	if err != nil {
		t.Fatalf("a malformed line must not fail the survey: %v", err)
	}
	if s.Bodies != 1 {
		t.Fatalf("bodies = %d, want 1 — the malformed line is not a body", s.Bodies)
	}
}

// TestSurveyRefusesAnEmptyCorpus: a survey over a directory with no session log
// must FAIL, naming what it looked for — a rate printed off nothing reads as a
// finding.
func TestSurveyRefusesAnEmptyCorpus(t *testing.T) {
	dir := t.TempDir()
	if _, err := Survey(dir, surveyFakeClassify); err == nil {
		t.Fatal("Survey over a directory with no session log returned a result; it must refuse by name")
	}
	if _, err := Survey(filepath.Join(dir, "absent"), surveyFakeClassify); err == nil {
		t.Fatal("Survey over a nonexistent root returned a result")
	}
}

// TestSurveyAcceptsASingleLogFile is the smallest sample (Rule 10.4): one file
// is a legal corpus, so a threshold can be re-derived against ONE round.
func TestSurveyAcceptsASingleLogFile(t *testing.T) {
	dir := surveyFixture(t, surveyAssistantEvent("aaaaaaaaaaaaaaa"))
	path := filepath.Join(dir, "session-1", SurveySessionFileName)
	s, err := Survey(path, surveyFakeClassify)
	if err != nil {
		t.Fatalf("Survey(one file): %v", err)
	}
	if s.Bodies != 1 || s.Files != 1 {
		t.Fatalf("bodies=%d files=%d, want 1/1", s.Bodies, s.Files)
	}
}

// TestSurveyRatesHaveNamedDenominators pins the two rates and their guards: a
// rate with no rounds is 0 (never NaN), and the round rate is the one that
// compares to the note corpus' measured percentage.
func TestSurveyRatesHaveNamedDenominators(t *testing.T) {
	s := newStats()
	if s.BodyRate() != 0 || s.RoundRate() != 0 {
		t.Fatalf("an empty corpus reported non-zero rates: %v / %v", s.BodyRate(), s.RoundRate())
	}
	if _, ok := s.Quantile(0.5); ok {
		t.Fatal("an empty corpus reported a median rune count")
	}
	dir := surveyFixture(t,
		map[string]any{"type": "request"},
		surveyAssistantEvent("aaaaaaaaaaaaaaa"),
	)
	full, err := Survey(dir, surveyFakeClassify)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if got := full.RoundRate(); got != 1 {
		t.Fatalf("round rate = %v, want 1 (one round, one capture)", got)
	}
	if got := full.BodyRate(); got != 1 {
		t.Fatalf("body rate = %v, want 1", got)
	}
	if v, ok := full.Largest(); !ok || v != 15 {
		t.Fatalf("largest = %d/%v, want 15/true", v, ok)
	}
}

// TestSessionLogFilesIsSortedAndRecursive pins the enumeration: a profile's
// session directory holds one subdirectory per session, so the walk must be
// recursive and the order stable (two runs must diff clean).
func TestSessionLogFilesIsSortedAndRecursive(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b", "a"} {
		sub := filepath.Join(dir, name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sub, SurveySessionFileName), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	files, err := SessionLogFiles(dir)
	if err != nil {
		t.Fatalf("SessionLogFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2", files)
	}
	if !strings.HasSuffix(files[0], filepath.Join("a", SurveySessionFileName)) {
		t.Fatalf("files are not sorted: %v", files)
	}
}
