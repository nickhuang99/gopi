package main

// cli_resume_dump_checkpoint_test.go — the REAL surface for row 01a183a8.
//
// The defect was observed on this command line, not on a library call:
//
//	$ gopi session resume <run>/<run>.raw.log
//	gopi session resume: resume: no → REQUEST checkpoint found in
//	    …/<run>.raw.log
//
// while `<run>.dump.json` in the same directory carried the complete request
// body (failed_iteration=0, category=network). The internal/agent test pins the
// decision; THIS one pins the operator-visible behaviour: the resume starts, the
// fallback is ANNOUNCED on stderr, and the continuation is seeded from the dump's
// request body.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// cliResumeDumpRunID names the run whose FIRST request died at the
	// transport layer — the shape that produces a header-only raw log.
	cliResumeDumpRunID = "01a183a8-c11d-7000-8000-00000000d0d0"

	// cliResumeDumpEpic is the instruction the recovered request carries. The
	// fake model answers a question about it, so the assertion below can tell
	// the resumed context was seeded from the DUMP and not from nothing.
	cliResumeDumpEpic = "[EPIC GOAL]\nrecover the resume checkpoint from the dump"
)

// cliResumeDumpFixture writes the measured session directory: a raw log holding
// only its header line, beside a crash dump carrying the full request.
func cliResumeDumpFixture(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".gopi", "sessions", cliResumeDumpRunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	rawLogPath := filepath.Join(dir, cliResumeDumpRunID+scratch.SessionRawLogFileSuffix)
	header := "# gopi session raw HTTP log — created 2026-10-29T00:00:00Z pid=1 session=" +
		cliResumeDumpRunID + " log=" + cliResumeDumpRunID + scratch.SessionRawLogFileSuffix + "\n"
	if err := os.WriteFile(rawLogPath, []byte(header), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	req := map[string]interface{}{
		"model":      "deepseek-flash",
		"max_tokens": 16384,
		"system":     []map[string]interface{}{{"type": "text", "text": "kernel"}},
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]interface{}{{"type": "text", "text": cliResumeDumpEpic}}},
			{"role": "assistant", "content": []map[string]interface{}{{"type": "text", "text": "reading the resume path"}}},
		},
	}
	doc := map[string]interface{}{
		"type":             "crash_dump",
		"session_id":       cliResumeDumpRunID,
		"timestamp":        "2026-10-29T00:00:01Z",
		"category":         "network",
		"http_status":      0,
		"error_body":       "Post \"https://api.example/anthropic/messages\": dial tcp: i/o timeout",
		"failed_iteration": 0,
		"recoverable_hint": true,
		"request":          req,
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal dump: %v", err)
	}
	dumpPath := filepath.Join(dir, cliResumeDumpRunID+".dump.json")
	if err := os.WriteFile(dumpPath, body, 0o644); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	return rawLogPath
}

// TestSessionResumeRecoversCheckpointFromDump is the acceptance test: the real
// CLI, a header-only raw log, a dump beside it.
func TestSessionResumeRecoversCheckpointFromDump(t *testing.T) {
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	rawLogPath := cliResumeDumpFixture(t, gopiHome)

	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "recovered from the dump"}, testutil.Resp{Text: "all done"})
	defer mock.Close()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := append(testutil.MockEnv(t, gopiHome, mock.URL()),
		"GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, rawLogPath}, "", env, "")

	if code != 0 {
		t.Fatalf("the resume must succeed from the dump: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	// ① THE FALLBACK IS ANNOUNCED, with the artifact it read.
	if !strings.Contains(stderr, "holds no → REQUEST checkpoint") {
		t.Fatalf("the resume must say the raw log carried no request round:\n%s", stderr)
	}
	if !strings.Contains(stderr, "recovered from the "+string(agent.CheckpointSourceDump)) {
		t.Fatalf("the resume must name the dump as the source:\n%s", stderr)
	}
	// ② the pre-fix refusal is GONE — the exact line the measured run printed.
	if strings.Contains(stderr, "resume: no → REQUEST checkpoint found in") {
		t.Fatalf("the pre-fix refusal is still reached:\n%s", stderr)
	}
	// ③ the continuation was SEEDED from the dump's body, not from nothing:
	// the loaded-history line counts the request's two messages.
	if !strings.Contains(stderr, "loaded 2 history message(s)") {
		t.Fatalf("the recovered request's messages must seed the run:\n%s", stderr)
	}
	// ④ and the FIRST message the provider saw carries the DUMP's instruction,
	// i.e. the recovered context is the one the interrupted run died on.
	carried := mock.RequestsContaining("recover the resume checkpoint from the dump")
	if len(carried) == 0 {
		t.Fatal("the resumed request does not carry the dump's recovered context — the checkpoint was not seeded")
	}
}

// TestSessionResumeRefusalNamesBothArtifacts pins the other half on the real
// surface: header-only raw log and NO dump ⇒ a refusal that names the file, its
// size and the candidate dump path — never the bare pre-fix sentence.
func TestSessionResumeRefusalNamesBothArtifacts(t *testing.T) {
	gopiHome := t.TempDir()
	dir := filepath.Join(gopiHome, ".gopi", "sessions", cliResumeDumpRunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	rawLogPath := filepath.Join(dir, cliResumeDumpRunID+scratch.SessionRawLogFileSuffix)
	header := "# gopi session raw HTTP log — created 2026-10-29T00:00:00Z pid=1 session=" +
		cliResumeDumpRunID + " log=" + cliResumeDumpRunID + scratch.SessionRawLogFileSuffix + "\n"
	if err := os.WriteFile(rawLogPath, []byte(header), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "unused"})
	defer mock.Close()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := append(testutil.MockEnv(t, gopiHome, mock.URL()),
		"GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, rawLogPath}, "", env, "")

	if code != 1 {
		t.Fatalf("nothing to resume must exit 1: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"no replayable crash dump",
		cliResumeDumpRunID + ".dump.json",
		"died before any request body reached disk",
	} {
		// The refusal is one line carrying all three facts.
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal must contain %q:\n%s", want, stderr)
		}
	}
}
