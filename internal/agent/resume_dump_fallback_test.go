package agent

// resume_dump_fallback_test.go — the regression guard for row 01a183a8.
//
// THE MEASURED DEFECT. A run whose FIRST HTTP request died at the transport
// layer (http_status=0, "dial tcp … i/o timeout") leaves a session directory
// whose raw.log is a header and nothing else: the raw logger writes its
// "→ REQUEST" marker only once a request body is actually handed to the
// transport. The SAME directory holds <run>.dump.json with the complete request
// body (model/max_tokens/system/messages/tools), failed_iteration=0,
// category=network. LoadCheckpoint refused with "no → REQUEST checkpoint found
// in <path>" — a statement about the FILE that reads as a statement about the
// CONTEXT, while the context was on disk and was never opened.
//
// WHAT THIS PINS: the dump is a SECOND SOURCE for the same artifact (the
// request body), the recovered history is byte-faithful to it, the source is
// REPORTED, and when neither artifact carries a replayable request the refusal
// names both facts instead of only the raw log.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/rawlogfmt"
)

const (
	// resumeDumpFallbackRunID is the run whose first request died. Its name is
	// the base both the raw log and the dump carry.
	resumeDumpFallbackRunID = "01a183a8-0000-7000-8000-000000000001"

	// resumeDumpFallbackHeader is the ONLY content of a raw log whose first
	// request never produced a body — the 167-byte shape measured on this host
	// (a single comment line, no marker, no payload).
	resumeDumpFallbackHeader = "# gopi session raw HTTP log — created 2026-10-29T00:00:00Z pid=1 session=" +
		resumeDumpFallbackRunID + " log=" + resumeDumpFallbackRunID + ".raw.log\n"

	// resumeDumpFallbackEpic is the user instruction the recovered request
	// carries. It must come back VERBATIM: the whole point of recovering the
	// dump's request is that the resumed context is the one the run died on.
	resumeDumpFallbackEpic = "[EPIC GOAL]\nfix the resume checkpoint fallback"
)

// resumeDumpFallbackFixture lays out the measured session directory: a
// header-only raw.log beside a crash dump carrying the complete request.
func resumeDumpFallbackFixture(t *testing.T, failedIteration int, category string) (rawLogPath, dumpPath string) {
	t.Helper()
	dir := t.TempDir()
	rawLogPath = filepath.Join(dir, resumeDumpFallbackRunID+".raw.log")
	if err := os.WriteFile(rawLogPath, []byte(resumeDumpFallbackHeader), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	// The request the run died on: system blocks, a user epic, an assistant
	// tool_use and its tool_result — enough that a lossy recovery is visible.
	req := deepseek.ClaudeRequest{
		Model:     "deepseek-flash",
		MaxTokens: 16384,
		System:    []deepseek.ClaudeSystemBlock{{Type: "text", Text: "kernel"}, {Type: "text", Text: "GOPI.md"}},
		Messages: []deepseek.ClaudeMessage{
			{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: resumeDumpFallbackEpic}}},
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{
				{Type: "text", Text: "reading the resume path"},
				{Type: "tool_use", ID: "call_dump_1", Name: "read", Input: map[string]interface{}{"path": "internal/agent/resume.go"}},
			}},
			{Role: "user", Content: []deepseek.ClaudeContentBlock{
				{Type: "tool_result", ToolUseID: "call_dump_1", Content: "package agent"},
			}},
		},
	}
	body, err := json.Marshal(req) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	doc := map[string]interface{}{
		"type":             crashDumpDocType,
		"session_id":       resumeDumpFallbackRunID,
		"timestamp":        "2026-10-29T00:00:01Z",
		"category":         category,
		"http_status":      0,
		"failed_iteration": failedIteration,
		"request":          json.RawMessage(body),
	}
	dump, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal dump: %v", err)
	}
	dumpPath = filepath.Join(dir, resumeDumpFallbackRunID+dumpFileSuffix)
	if err := os.WriteFile(dumpPath, dump, 0o644); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	return rawLogPath, dumpPath
}

// TestLoadCheckpointWithSource_RecoversTheDumpRequestWhenTheRawLogBarelyExists
// is the repro: header-only raw.log, complete request in the dump → the SAME
// context comes back, and the SOURCE says it came from the dump.
func TestLoadCheckpointWithSource_RecoversTheDumpRequestWhenTheRawLogBarelyExists(t *testing.T) {
	rawLogPath, dumpPath := resumeDumpFallbackFixture(t, 0, string(deepseek.CategoryNetwork))

	req, history, systemTexts, provenance, err := LoadCheckpointWithSource(rawLogPath)
	if err != nil {
		t.Fatalf("the dump beside a header-only raw log must be resumable, got: %v", err)
	}
	if provenance.Source != CheckpointSourceDump {
		t.Fatalf("source = %q, want %q — a recovery from the dump must be REPORTED, not passed off as the raw log", provenance.Source, CheckpointSourceDump)
	}
	if !strings.Contains(provenance.Detail(), dumpPath) {
		t.Errorf("provenance must NAME the dump it read (%s), got %q", dumpPath, provenance.Detail())
	}
	if req.Model != "deepseek-flash" || req.MaxTokens != 16384 {
		t.Errorf("recovered request = model %q max_tokens %d, want deepseek-flash/16384", req.Model, req.MaxTokens)
	}
	if len(systemTexts) != 2 || systemTexts[0] != "kernel" || systemTexts[1] != "GOPI.md" {
		t.Errorf("systemTexts = %v, want [kernel GOPI.md]", systemTexts)
	}
	// 3 messages: user epic, assistant (text+tool_use), tool result.
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3: %+v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != resumeDumpFallbackEpic {
		t.Errorf("history[0] = %+v, want the recovered epic verbatim", history[0])
	}
	if len(history[1].ToolCalls) != 1 || history[1].ToolCalls[0].ID != "call_dump_1" {
		t.Errorf("history[1].ToolCalls = %+v, want call_dump_1", history[1].ToolCalls)
	}
	if history[2].Role != "tool" || history[2].ToolCallID != "call_dump_1" {
		t.Errorf("history[2] = %+v, want the tool_result for call_dump_1", history[2])
	}
	if _, statErr := os.Stat(dumpPath); statErr != nil {
		t.Fatalf("fixture dump vanished: %v", statErr)
	}
}

// TestLoadCheckpointWithSource_PrefersTheRawLogWhenItHasARound: the fallback
// must never OUTRANK the byte-exact source. A log that carries a completed
// request round wins even when a dump sits beside it.
func TestLoadCheckpointWithSource_PrefersTheRawLogWhenItHasARound(t *testing.T) {
	rawLogPath, _ := resumeDumpFallbackFixture(t, 3, string(deepseek.CategoryNetwork))
	// Append a real round: its payload says a DIFFERENT instruction, so the
	// assertion can tell which source answered.
	logReq := deepseek.ClaudeRequest{
		Model: "deepseek-flash",
		Messages: []deepseek.ClaudeMessage{
			{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "from the raw log"}}},
		},
	}
	body, err := json.Marshal(logReq) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f, err := os.OpenFile(rawLogPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open raw log: %v", err)
	}
	// The record is written through the WRITER's own encoder, so this fixture
	// cannot drift from the framing a real log carries (before, it spelled the
	// legacy marker by hand — a second copy of the format, and the reason this
	// test kept passing while the reader changed under it).
	if _, err := f.Write(rawlogfmt.Encode(rawlogfmt.TagRequest, body)); err != nil {
		_ = f.Close() //stylegate:ignore the fixture is already failing; draining the handle before t.Fatalf cannot change the verdict
		t.Fatalf("append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, history, _, provenance, err := LoadCheckpointWithSource(rawLogPath)
	if err != nil {
		t.Fatalf("LoadCheckpointWithSource: %v", err)
	}
	if provenance.Source != CheckpointSourceRawLog {
		t.Fatalf("source = %q, want %q — the byte-exact source must win over the dump", provenance.Source, CheckpointSourceRawLog)
	}
	if len(history) != 1 || history[0].Content != "from the raw log" {
		t.Errorf("history = %+v, want the raw log's own round", history)
	}
}

// TestLoadCheckpointWithSource_RefusalNamesBothArtifacts is the other half of
// the deliverable: when NEITHER source carries a replayable request, the refusal
// must say so honestly — naming the raw log, its size, and every dump candidate
// examined — instead of the pre-fix "no → REQUEST checkpoint found", which read
// as "there is no context anywhere".
func TestLoadCheckpointWithSource_RefusalNamesBothArtifacts(t *testing.T) {
	dir := t.TempDir()
	rawLogPath := filepath.Join(dir, resumeDumpFallbackRunID+".raw.log")
	if err := os.WriteFile(rawLogPath, []byte(resumeDumpFallbackHeader), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	_, _, _, _, err := LoadCheckpointWithSource(rawLogPath)
	if err == nil {
		t.Fatal("a header-only raw log with no dump must be REFUSED (nothing to resume)")
	}
	msg := err.Error()
	for _, want := range []string{
		"no → REQUEST checkpoint",
		"no replayable crash dump",
		rawLogPath,
		resumeDumpFallbackRunID + dumpFileSuffix, // the candidate it looked for, named
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must name %q, got:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "checkpoint has no messages") {
		t.Errorf("the refusal must describe the ARTIFACTS, not a parse failure:\n%s", msg)
	}
}

// TestLoadCheckpointWithSource_RefusesAHeadTrimmedDump: the dump caps the
// request body by dropping its head (tailJSON). A trimmed body is not JSON and
// must never be replayed — the refusal says so by name.
func TestLoadCheckpointWithSource_RefusesAHeadTrimmedDump(t *testing.T) {
	rawLogPath, dumpPath := resumeDumpFallbackFixture(t, 0, string(deepseek.CategoryNetwork))
	// The trimmed body reaches the document as a QUOTED string: it is not valid
	// JSON on its own, so any writer that stored it had to encode it as a string
	// (json.Marshal of an unparseable RawMessage fails outright). Both spellings
	// must be recognised — hence the check reads the marker, not the first byte.
	doc := `{"type":"` + crashDumpDocType + `","session_id":"` + resumeDumpFallbackRunID +
		`","category":"` + string(deepseek.CategoryNetwork) + `","failed_iteration":0,` +
		`"request":"` + dumpHeadTrimmedPrefix + `{\"model\":\"deepseek-flash\",\"messages\":[]}"}`
	if err := os.WriteFile(dumpPath, []byte(doc), 0o644); err != nil {
		t.Fatalf("write dump: %v", err)
	}

	_, _, _, _, err := LoadCheckpointWithSource(rawLogPath)
	if err == nil {
		t.Fatal("a head-trimmed dump body must be refused, not replayed")
	}
	if !strings.Contains(err.Error(), "head-trimmed") {
		t.Errorf("the refusal must name the trimming, got:\n%s", err)
	}
}

// TestLoadCheckpointWithSource_BareSessionDumpName pins the SECOND naming shape
// this host really carries: a bare-session container records session.jsonl (so
// its dump is session.dump.json) while its raw log is named after the run uuid.
// Reading only <rawlog-base>.dump.json would refuse exactly those sessions.
func TestLoadCheckpointWithSource_BareSessionDumpName(t *testing.T) {
	rawLogPath, dumpPath := resumeDumpFallbackFixture(t, 0, string(deepseek.CategoryNetwork))
	bareDump := filepath.Join(filepath.Dir(rawLogPath), bareSessionDumpFileName)
	if err := os.Rename(dumpPath, bareDump); err != nil {
		t.Fatalf("rename dump: %v", err)
	}

	_, history, _, provenance, err := LoadCheckpointWithSource(rawLogPath)
	if err != nil {
		t.Fatalf("the bare-session dump name must be found, got: %v", err)
	}
	if provenance.Source != CheckpointSourceDump || len(history) != 3 {
		t.Fatalf("source=%q history=%d, want the dump's 3-message context", provenance.Source, len(history))
	}
}

// TestLoadCheckpointWithSource_RefusesAForeignRunsDump is the guard against the
// fallback recovering the WRONG context. A task-run directory holds one dump per
// RUN of that row (sessions/<task-id>/<uuid>.dump.json), so a bare *.dump.json
// glob would let a resume of run A continue run B's conversation — silently, and
// with a context the resumed run never had. The document's own session_id is the
// only tie, so a candidate that was found by SCANNING must prove it.
func TestLoadCheckpointWithSource_RefusesAForeignRunsDump(t *testing.T) {
	siblingRunID := resumeDumpFallbackRunID + "-sibling"
	rawLogPath, ownDump := resumeDumpFallbackFixture(t, 0, string(deepseek.CategoryNetwork))
	// Re-point the only dump at a DIFFERENT run, then put it under the sibling's
	// name — exactly the shape of two runs sharing one directory.
	foreign := filepath.Join(filepath.Dir(rawLogPath), siblingRunID+dumpFileSuffix)
	doc, err := os.ReadFile(ownDump)
	if err != nil {
		t.Fatalf("read own dump: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("parse own dump: %v", err)
	}
	parsed["session_id"] = siblingRunID
	rewritten, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.Remove(ownDump); err != nil {
		t.Fatalf("remove own dump: %v", err)
	}
	if err := os.WriteFile(foreign, rewritten, 0o644); err != nil {
		t.Fatalf("write sibling dump: %v", err)
	}

	_, history, _, _, err := LoadCheckpointWithSource(rawLogPath)
	if err == nil {
		t.Fatalf("a sibling run's dump must NOT be recovered: got %d message(s) of another run's context", len(history))
	}
	if !strings.Contains(err.Error(), "another run's dump") {
		t.Errorf("the refusal must say the dump belongs to another run, got:\n%s", err)
	}
}
