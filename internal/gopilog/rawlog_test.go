package gopilog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// ---- parseRequestBody: modern top-level "system" array shape ----

func TestParseRequestBodyModernShape(t *testing.T) {
	body := `{
		"model": "deepseek-v4-pro",
		"max_tokens": 8192,
		"system": [
			{"type": "text", "text": "You are a coding agent."},
			{"type": "text", "text": "Rule 1: no magic numbers."}
		],
		"messages": [{"role": "user", "content": "hello"}]
	}`
	p, err := parseRequestBody([]byte(body))
	if err != nil {
		t.Fatalf("parseRequestBody: %v", err)
	}
	if p.Model != "deepseek-v4-pro" {
		t.Errorf("Model = %q, want deepseek-v4-pro", p.Model)
	}
	if len(p.System) != 2 {
		t.Fatalf("len(System) = %d, want 2", len(p.System))
	}
	if p.System[0].Text != "You are a coding agent." {
		t.Errorf("System[0].Text = %q", p.System[0].Text)
	}
	if len(p.Raw) == 0 {
		t.Error("Raw bytes were not preserved")
	}
}

func TestParseRequestBodyLegacyShape(t *testing.T) {
	// Legacy sessions carry the system prompt as messages[0].content.
	body := `{
		"model": "deepseek-v4-flash",
		"messages": [
			{"role": "system", "content": "legacy prompt text"},
			{"role": "user", "content": "hi"}
		]
	}`
	p, err := parseRequestBody([]byte(body))
	if err != nil {
		t.Fatalf("parseRequestBody: %v", err)
	}
	prompts := extractSystemPrompts(p)
	if len(prompts) != 1 || prompts[0] != "legacy prompt text" {
		t.Errorf("extractSystemPrompts = %q, want [legacy prompt text]", prompts)
	}
}

func TestParseRequestBodyInvalidJSON(t *testing.T) {
	if _, err := parseRequestBody([]byte("{not json")); err == nil {
		t.Error("expected error for invalid JSON body")
	}
}

// ---- extractSystemPrompts: modern array + legacy messages + combinations ----

func TestExtractSystemPromptsModernAndLegacy(t *testing.T) {
	p := &RequestPayload{
		System: []SystemBlock{
			{Type: "text", Text: "modern-a"},
			{Type: "text", Text: "modern-b"},
		},
		Messages: []RequestMessage{
			{Role: "system", Content: json.RawMessage(`"legacy"`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	}
	prompts := extractSystemPrompts(p)
	want := []string{"modern-a", "modern-b", "legacy"}
	if len(prompts) != len(want) {
		t.Fatalf("got %d prompts %q, want %q", len(prompts), prompts, want)
	}
	for i := range want {
		if prompts[i] != want[i] {
			t.Errorf("prompts[%d] = %q, want %q", i, prompts[i], want[i])
		}
	}
}

func TestExtractSystemPromptsLegacyContentBlocks(t *testing.T) {
	// Legacy content may be an array of text blocks inside a system message.
	p := &RequestPayload{
		Messages: []RequestMessage{
			{Role: "system", Content: json.RawMessage(`[{"type":"text","text":"block-1"},{"type":"text","text":"block-2"}]`)},
		},
	}
	prompts := extractSystemPrompts(p)
	want := []string{"block-1", "block-2"}
	if len(prompts) != len(want) {
		t.Fatalf("got %d prompts %q, want %q", len(prompts), prompts, want)
	}
}

func TestExtractSystemPromptsEmpty(t *testing.T) {
	p := &RequestPayload{Messages: []RequestMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	if prompts := extractSystemPrompts(p); len(prompts) != 0 {
		t.Errorf("expected no prompts, got %q", prompts)
	}
}

// ---- ParseFirstRequest: marker-driven raw log scanning ----

func TestParseFirstRequest(t *testing.T) {
	rawLog := filepath.Join(t.TempDir(), "gopi-raw-20260803-190611.log")
	content := "junk preamble\n" +
		"→ REQUEST\n" +
		"{\"model\":\"deepseek-v4-flash\",\"system\":[{\"type\":\"text\",\"text\":\"SYSPROMPT\"}],\"messages\":[]}\n" +
		"← RESPONSE\n" +
		"{\"choices\":[]}\n"
	if err := os.WriteFile(rawLog, []byte(content), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	p, err := ParseFirstRequest(rawLog)
	if err != nil {
		t.Fatalf("ParseFirstRequest: %v", err)
	}
	if p.Model != "deepseek-v4-flash" {
		t.Errorf("Model = %q", p.Model)
	}
	if len(p.System) != 1 || p.System[0].Text != "SYSPROMPT" {
		t.Errorf("System = %+v", p.System)
	}
}

func TestParseFirstRequestNoMarker(t *testing.T) {
	rawLog := filepath.Join(t.TempDir(), "gopi-raw-20260803-190611.log")
	if err := os.WriteFile(rawLog, []byte("← CHUNK\n{}\n"), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	if _, err := ParseFirstRequest(rawLog); err == nil {
		t.Error("expected error when no → REQUEST marker exists")
	}
}

func TestParseFirstRequestMarkerNotFollowedByBody(t *testing.T) {
	rawLog := filepath.Join(t.TempDir(), "gopi-raw-20260803-190611.log")
	if err := os.WriteFile(rawLog, []byte("→ REQUEST\n"), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	if _, err := ParseFirstRequest(rawLog); err == nil {
		t.Error("expected error when → REQUEST marker has no body")
	}
}

// ---- FormatRequestSystem ----

func TestFormatRequestSystem(t *testing.T) {
	p := &RequestPayload{
		Model: "deepseek-v4-pro",
		System: []SystemBlock{
			{Type: "text", Text: "prompt-one"},
			{Type: "text", Text: "prompt-two"},
		},
	}
	out := FormatRequestSystem(p, "gopi-raw-20260803-190611.log", false)
	for _, want := range []string{
		"REQUEST — system prompt (verbatim)",
		"Raw log: gopi-raw-20260803-190611.log",
		"Model:   deepseek-v4-pro",
		"SYSTEM PROMPT [1/2]:",
		"SYSTEM PROMPT [2/2]:",
		"prompt-one",
		"prompt-two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatRequestSystem output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatRequestSystemTextMode(t *testing.T) {
	p := &RequestPayload{System: []SystemBlock{{Type: "text", Text: "plain-prompt"}}}
	out := FormatRequestSystem(p, "gopi-raw-20260803-190611.log", true)
	if out != "plain-prompt\n" {
		t.Errorf("text mode = %q, want %q", out, "plain-prompt\n")
	}
	if strings.Contains(out, headerSep) || strings.Contains(out, "SYSTEM PROMPT") {
		t.Errorf("text mode must not contain header/labels:\n%s", out)
	}
}

func TestFormatRequestSystemNoPrompts(t *testing.T) {
	p := &RequestPayload{Model: "m"}
	out := FormatRequestSystem(p, "gopi-raw-20260803-190611.log", false)
	if !strings.Contains(out, "no system prompt blocks found") {
		t.Errorf("expected 'no system prompt blocks found' notice:\n%s", out)
	}
}

// ---- FormatRequestJSON ----

func TestFormatRequestJSON(t *testing.T) {
	body := `{"model":"m","system":[{"type":"text","text":"x"}],"messages":[{"role":"user","content":"hi"}]}`
	p, err := parseRequestBody([]byte(body))
	if err != nil {
		t.Fatalf("parseRequestBody: %v", err)
	}
	out := FormatRequestJSON(p)
	if !json.Valid([]byte(out)) {
		t.Fatalf("FormatRequestJSON output is not valid JSON:\n%s", out)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded["model"] != "m" {
		t.Errorf("decoded model = %v, want m", decoded["model"])
	}
}

// ---- SessionStartTime ----

func TestSessionStartTime(t *testing.T) {
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "sess.jsonl")
	content := "{\"type\":\"session_start\",\"sessionId\":\"abc\",\"timestamp\":\"2026-08-03T19:06:11.922864935+08:00\"}\n" +
		"{\"type\":\"user\",\"message\":{\"content\":\"hi\"}}\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	start, err := SessionStartTime(sessionFile)
	if err != nil {
		t.Fatalf("SessionStartTime: %v", err)
	}
	if start.Hour() != 19 || start.Minute() != 6 {
		t.Errorf("start = %v, want 19:06", start)
	}
}

func TestSessionStartTimeMissing(t *testing.T) {
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(sessionFile, []byte("{\"type\":\"user\",\"message\":{\"content\":\"hi\"}}\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if _, err := SessionStartTime(sessionFile); err == nil {
		t.Error("expected error when session has no session_start event")
	}
}

// ---- FindRawLogForSession (via temp $HOME, raw log dir = ~/.gopi/raw) ----

// writeRawLog writes a raw log file into t's temp $HOME/.gopi/raw directory
// and returns its path.
//
// The seam is GUARDED (Rule 15): the caller must have sandboxed $HOME. Without
// the guard this helper resolves the OPERATOR'S live raw-log directory for any
// caller that forgot its `t.Setenv("HOME", t.TempDir())`, silently rewriting
// real raw logs on every `go test` — measured 2026-09-12 in this very file (see
// internal/testutil/profileguard.go). The guard is a refusal, never a silent
// re-point, so a future omission is loud instead of corrupting the operator's
// profile.
func writeRawLog(t *testing.T, name, content string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), testutil.GopiProfileDirName, rawLogsDir)
	testutil.RefuseOperatorProfileWrite(t, "writeRawLog", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir raw dir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write raw log %s: %v", name, err)
	}
	return path
}

func TestFindRawLogForSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeRawLog(t, "gopi-raw-20260803-190611.log", "→ REQUEST\n{}\n")
	// Session starts 9s after the raw log's embedded start — well under
	// maxRawLogMatchDelta (2 min).
	start := time.Date(2026, 8, 3, 19, 6, 20, 0, time.Local)
	got, err := FindRawLogForSession(start)
	if err != nil {
		t.Fatalf("FindRawLogForSession: %v", err)
	}
	if filepath.Base(got) != "gopi-raw-20260803-190611.log" {
		t.Errorf("matched %s, want gopi-raw-20260803-190611.log", got)
	}
}

func TestFindRawLogForSessionTooFar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeRawLog(t, "gopi-raw-20260803-190611.log", "→ REQUEST\n{}\n")
	// Session starts ~4 minutes after the raw log — beyond the 2 min delta.
	start := time.Date(2026, 8, 3, 19, 10, 30, 0, time.Local)
	if _, err := FindRawLogForSession(start); err == nil {
		t.Error("expected error when closest raw log is beyond maxRawLogMatchDelta")
	}
}

func TestFindRawLogForSessionNoLogs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	start := time.Date(2026, 8, 3, 19, 6, 20, 0, time.Local)
	if _, err := FindRawLogForSession(start); err == nil {
		t.Error("expected error when no raw logs exist")
	}
}

func TestFindRawLogForSessionAmbiguous(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Two raw logs equally close to the session start → ambiguous error.
	writeRawLog(t, "gopi-raw-20260803-190610.log", "→ REQUEST\n{}\n")
	writeRawLog(t, "gopi-raw-20260803-190612.log", "→ REQUEST\n{}\n")
	start := time.Date(2026, 8, 3, 19, 6, 11, 0, time.Local)
	if _, err := FindRawLogForSession(start); err == nil {
		t.Error("expected ambiguous-match error")
	}
}

func TestRawLogStartParsing(t *testing.T) {
	ts, ok := rawLogStart("gopi-raw-20260803-190611.log", time.UTC)
	if !ok {
		t.Fatal("rawLogStart failed to parse valid name")
	}
	if ts.Format("20060102-150405") != "20260803-190611" {
		t.Errorf("parsed %v, want 2026-08-03 19:06:11", ts)
	}
	if _, ok := rawLogStart("not-a-raw-log.txt", time.UTC); ok {
		t.Error("rawLogStart accepted invalid filename")
	}
}

// ---- --raw view: ResolveRawLogForSession + FormatRawLog ----

// TestResolveRawLogForSessionPrefersRecordedPath: the exact rawLogPath
// recorded in session_start wins over timestamp pairing — even when a
// timestamp-closer raw log exists (the custom GOPI_RAW_LOG_DIR trap).
func TestResolveRawLogForSessionPrefersRecordedPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionsDir := filepath.Join(os.Getenv("HOME"), ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	// Session file whose session_start records a raw log path in a custom dir.
	sessionPath := filepath.Join(sessionsDir, "sess-custom.jsonl")
	recorded := "/tmp/custom-raw/gopi-raw-20260803-190000.log"
	if err := os.MkdirAll(filepath.Dir(recorded), 0o700); err != nil {
		t.Fatalf("mkdir custom raw dir: %v", err)
	}
	if err := os.WriteFile(recorded, []byte("→ REQUEST\n{}\n"), 0o600); err != nil {
		t.Fatalf("write custom raw log: %v", err)
	}
	// Default-dir raw log that is timestamp-CLOSER to the session start.
	writeRawLog(t, "gopi-raw-20260803-190010.log", "→ REQUEST\n{}\n")
	session := `{"type":"session_start","sessionId":"sess-custom","rawLogPath":"` + recorded + `","timestamp":"2026-08-03T19:00:30+08:00"}`
	if err := os.WriteFile(sessionPath, []byte(session+"\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	got, err := ResolveRawLogForSession(sessionPath)
	if err != nil {
		t.Fatalf("ResolveRawLogForSession: %v", err)
	}
	if got != recorded {
		t.Errorf("resolved %q, want recorded path %q", got, recorded)
	}
}

// TestResolveRawLogForSessionFollowsARehomedRawLog: a raw log that MOVED into
// its session directory (the epic task-session-cwd re-home) still resolves —
// the recorded path is stale, the recorded BASENAME is not, and only the
// session's own directory may supply the file.
//
// Regression guard for the trap the sweep would otherwise spring: the re-home
// moves the evidence, and `gopi session raw`/`gopi session stats`/`gopi session resume` on a moved
// session must still find it (measured: 417 of 1432 flat logs carry exactly
// one located owner).
func TestResolveRawLogForSessionFollowsARehomedRawLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionsDir := filepath.Join(os.Getenv("HOME"), ".gopi", "sessions")
	sessionDir := filepath.Join(sessionsDir, "sess-moved")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	const name = "gopi-raw-20260803-191500.log"
	// The record still points at the retired flat dir — the file is gone from
	// there (moved) and now sits beside this session's JSONL.
	stale := filepath.Join(os.Getenv("HOME"), ".gopi", "raw", name)
	sessionPath := filepath.Join(sessionDir, "session.jsonl")
	session := `{"type":"session_start","sessionId":"sess-moved","rawLogPath":"` + stale + `","timestamp":"2026-08-03T19:15:00+08:00"}`
	if err := os.WriteFile(sessionPath, []byte(session+"\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	moved := filepath.Join(sessionDir, name)
	if err := os.WriteFile(moved, []byte("→ REQUEST\n{}\n"), 0o600); err != nil {
		t.Fatalf("write moved raw log: %v", err)
	}

	got, err := ResolveRawLogForSession(sessionPath)
	if err != nil {
		t.Fatalf("ResolveRawLogForSession: %v", err)
	}
	if got != moved {
		t.Errorf("resolved %q, want the re-homed sibling %q", got, moved)
	}

	// A DIFFERENT session directory holding the same basename must never be
	// offered to this session — the sibling lookup is per-session, not a
	// basename search over the tree.
	otherDir := filepath.Join(sessionsDir, "sess-other")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatalf("mkdir other dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "session.jsonl"),
		[]byte(`{"type":"session_start","sessionId":"sess-other","timestamp":"2026-08-03T19:15:00+08:00"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write other session: %v", err)
	}
	if sib := SiblingRawLog(filepath.Join(otherDir, "session.jsonl"), stale); sib != "" {
		t.Errorf("SiblingRawLog offered %q to a session that does not own the file", sib)
	}
}

// TestResolveRawLogForSessionFallsBackToTimestamp: sessions that predate the
// rawLogPath field resolve via timestamp pairing.
func TestResolveRawLogForSessionFallsBackToTimestamp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionsDir := filepath.Join(os.Getenv("HOME"), ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	// session_start WITHOUT rawLogPath (pre-resume sessions).
	sessionPath := filepath.Join(sessionsDir, "sess-old.jsonl")
	session := `{"type":"session_start","sessionId":"sess-old","timestamp":"2026-08-03T19:06:20+08:00"}`
	if err := os.WriteFile(sessionPath, []byte(session+"\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	raw := writeRawLog(t, "gopi-raw-20260803-190620.log", "→ REQUEST\n{}\n")

	got, err := ResolveRawLogForSession(sessionPath)
	if err != nil {
		t.Fatalf("ResolveRawLogForSession: %v", err)
	}
	if got != raw {
		t.Errorf("resolved %q, want timestamp-paired %q", got, raw)
	}
}

// TestResolveRawLogForSessionMissing: no raw log anywhere → explicit error.
func TestResolveRawLogForSessionMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionsDir := filepath.Join(os.Getenv("HOME"), ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	sessionPath := filepath.Join(sessionsDir, "sess-noraw.jsonl")
	session := `{"type":"session_start","sessionId":"sess-noraw","timestamp":"2026-08-03T19:06:20+08:00"}`
	if err := os.WriteFile(sessionPath, []byte(session+"\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if _, err := ResolveRawLogForSession(sessionPath); err == nil {
		t.Error("expected error when no raw log exists")
	}
}

// TestFormatRawLogVerbatim: markers and payloads pass through byte-exact.
func TestFormatRawLogVerbatim(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	content := "→ REQUEST\n{\"model\":\"m\",\"max_tokens\":1,\"messages\":[]}\n" +
		"← CHUNK\nevent: message_start\ndata: {}\n\n" +
		"← CHUNK\nevent: message_stop\ndata: {}\n\n"
	path := writeRawLog(t, "gopi-raw-20260803-190620.log", content)

	got, err := FormatRawLog(path)
	if err != nil {
		t.Fatalf("FormatRawLog: %v", err)
	}
	if got != content {
		t.Errorf("verbatim round-trip failed:\n got %q\nwant %q", got, content)
	}
}

// TestFormatRawLogTruncation: a log larger than maxRawViewBytes is cut with
// an explicit marker naming the file.
func TestFormatRawLogTruncation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	big := strings.Repeat("← CHUNK\ndata: {}\n\n", maxRawViewBytes/16+1)
	path := writeRawLog(t, "gopi-raw-20260803-190620.log", big)

	got, err := FormatRawLog(path)
	if err != nil {
		t.Fatalf("FormatRawLog: %v", err)
	}
	if len(got) > maxRawViewBytes+256 {
		t.Errorf("output %d bytes, want <= cap %d + marker slack", len(got), maxRawViewBytes)
	}
	if !strings.Contains(got, "truncated at") || !strings.Contains(got, path) {
		t.Errorf("truncation marker missing: ...%s", got[len(got)-200:])
	}
}

func TestRawLogStartParsingCollisionSuffix(t *testing.T) {
	// The writer takes a -<n> suffix when a second agent starts inside the
	// same wall-clock second. The suffix carries no time, so the embedded
	// second must parse identically for the bare and the suffixed name.
	bare, ok := rawLogStart("gopi-raw-20260803-190611.log", time.UTC)
	if !ok {
		t.Fatal("bare name failed to parse")
	}
	for _, name := range []string{
		"gopi-raw-20260803-190611-1.log",
		"gopi-raw-20260803-190611-12.log",
		"gopi-raw-20260803-190611-u2042548-10.log",
	} {
		got, ok := rawLogStart(name, time.UTC)
		if !ok {
			t.Fatalf("%s failed to parse", name)
		}
		if !got.Equal(bare) {
			t.Errorf("%s parsed %v, want %v", name, got, bare)
		}
	}
	for _, name := range []string{
		"gopi-raw-20260803-190611-.log", // empty suffix
		"gopi-raw-20260803-190611-x.log",
		"gopi-raw-20260803-190611-1.txt",
	} {
		if _, ok := rawLogStart(name, time.UTC); ok {
			t.Errorf("rawLogStart accepted malformed name %q", name)
		}
	}
}

// TestFindRawLogForSessionCollisionSibling: a collision-suffixed raw log stays
// reachable through timestamp pairing (the fallback for sessions that predate
// the rawLogPath field).
func TestFindRawLogForSessionCollisionSibling(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeRawLog(t, "gopi-raw-20260803-190611-1.log", "→ REQUEST\n{}\n")
	start := time.Date(2026, 8, 3, 19, 6, 20, 0, time.Local)
	got, err := FindRawLogForSession(start)
	if err != nil {
		t.Fatalf("FindRawLogForSession: %v", err)
	}
	if filepath.Base(got) != "gopi-raw-20260803-190611-1.log" {
		t.Errorf("matched %s, want gopi-raw-20260803-190611-1.log", got)
	}
}

// TestFindRawLogForSessionCollisionAmbiguous: two logs sharing one embedded
// second are genuinely unattributable from the name alone, so timestamp
// pairing refuses rather than silently guessing.
func TestFindRawLogForSessionCollisionAmbiguous(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeRawLog(t, "gopi-raw-20260803-190611.log", "→ REQUEST\n{}\n")
	writeRawLog(t, "gopi-raw-20260803-190611-1.log", "→ REQUEST\n{}\n")
	start := time.Date(2026, 8, 3, 19, 6, 20, 0, time.Local)
	if _, err := FindRawLogForSession(start); err == nil {
		t.Error("expected ambiguous-match error for collision siblings")
	}
}

// writeSession records one session whose session_start names a raw log.
func writeSession(t *testing.T, id, rawLogPath string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".gopi", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	start := map[string]interface{}{
		"type":       "session_start",
		"sessionId":  id,
		"model":      "deepseek-v4-flash",
		"rawLogPath": rawLogPath,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(start)
	if err != nil {
		t.Fatalf("marshal session_start: %v", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatalf("write session %s: %v", id, err)
	}
	return path
}

// TestSessionsRecordingRawLogSingle: a raw log one session recorded reports
// exactly that session.
func TestSessionsRecordingRawLogSingle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rawLog := writeRawLog(t, "gopi-raw-20260803-190611.log", "→ REQUEST\n{}\n")
	writeSession(t, "aaaa1111-0000-0000-0000-000000000001", rawLog)
	writeSession(t, "bbbb2222-0000-0000-0000-000000000002", filepath.Join(filepath.Dir(rawLog), "gopi-raw-20260803-190000.log"))

	got := SessionsRecordingRawLog(rawLog)
	if len(got) != 1 || got[0] != "aaaa1111-0000-0000-0000-000000000001" {
		t.Errorf("SessionsRecordingRawLog = %v, want the single owner", got)
	}
}

// TestSessionsRecordingRawLogShared: two sessions recording the same raw log
// is the interleaving signature — both ids come back, sorted.
func TestSessionsRecordingRawLogShared(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rawLog := writeRawLog(t, "gopi-raw-20260803-190611.log", "→ REQUEST\n{}\n")
	writeSession(t, "aaaa1111-0000-0000-0000-000000000001", rawLog)
	writeSession(t, "bbbb2222-0000-0000-0000-000000000002", rawLog)

	got := SessionsRecordingRawLog(rawLog)
	want := []string{
		"aaaa1111-0000-0000-0000-000000000001",
		"bbbb2222-0000-0000-0000-000000000002",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionsRecordingRawLog = %v, want %v", got, want)
	}
}

// TestSessionsRecordingRawLogIgnoresNoise: sessions whose first line is not a
// parseable session_start, and files that are not session JSONL, are skipped —
// the scan is best-effort attribution, never a hard dependency.
func TestSessionsRecordingRawLogIgnoresNoise(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rawLog := writeRawLog(t, "gopi-raw-20260803-190611.log", "→ REQUEST\n{}\n")
	writeSession(t, "aaaa1111-0000-0000-0000-000000000001", rawLog)

	dir := filepath.Join(os.Getenv("HOME"), ".gopi", "sessions")
	if err := os.WriteFile(filepath.Join(dir, "broken.jsonl"), []byte("not json\n"), 0o600); err != nil {
		t.Fatalf("write broken session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notsession.txt"), []byte("\n"), 0o600); err != nil {
		t.Fatalf("write non-session: %v", err)
	}

	if got := SessionsRecordingRawLog(rawLog); len(got) != 1 {
		t.Errorf("SessionsRecordingRawLog = %v, want only the real owner", got)
	}
}

// TestFirstUserMessage covers both wire shapes for the user's own input: a
// plain-string content and a content-block array. The modern shape's FIRST
// text block is the answer — the user's text with whatever the agent appended
// to it before the request was built (the wire does not separate the two).
func TestFirstUserMessage(t *testing.T) {
	modern := `{"model":"m","system":[{"type":"text","text":"SYS"}],"messages":[
		{"role":"user","content":[{"type":"text","text":"EPIC TEXT\n[system] TASK-START"}]}]}`
	p, err := parseRequestBody([]byte(modern))
	if err != nil {
		t.Fatalf("parse modern: %v", err)
	}
	got, ok := FirstUserMessage(p)
	if !ok {
		t.Fatal("FirstUserMessage: not found in modern shape")
	}
	if got != "EPIC TEXT\n[system] TASK-START" {
		t.Errorf("modern shape user message = %q", got)
	}

	legacy := `{"model":"m","messages":[{"role":"user","content":"PLAIN PROMPT"}]}`
	if p, err = parseRequestBody([]byte(legacy)); err != nil {
		t.Fatalf("parse legacy: %v", err)
	}
	if got, ok = FirstUserMessage(p); !ok || got != "PLAIN PROMPT" {
		t.Errorf("legacy shape user message = %q ok=%v", got, ok)
	}

	// A request with no user message (a system-only legacy body) reports
	// absence rather than returning "".
	sysOnly := `{"model":"m","messages":[{"role":"system","content":"SYS"}]}`
	if p, err = parseRequestBody([]byte(sysOnly)); err != nil {
		t.Fatalf("parse system-only: %v", err)
	}
	if _, ok = FirstUserMessage(p); ok {
		t.Error("FirstUserMessage: reported a user message for a system-only body")
	}

	if out := FormatRequestUserPrompt(p); out != "(no user message found in the request body)\n" {
		t.Errorf("FormatRequestUserPrompt on a system-only body = %q", out)
	}
}

// TestFormatRequestUserPromptVerbatim: the view is the text plus one trailing
// newline — no header, no decoration, so it pipes like --sysprompt.
func TestFormatRequestUserPromptVerbatim(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"用 bash 工具真实创建 /tmp/proof.txt"}]}]}`
	p, err := parseRequestBody([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := FormatRequestUserPrompt(p)
	if got != "用 bash 工具真实创建 /tmp/proof.txt\n" {
		t.Errorf("FormatRequestUserPrompt = %q", got)
	}
}
