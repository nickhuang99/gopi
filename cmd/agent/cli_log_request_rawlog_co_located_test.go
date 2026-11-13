package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// Regression for the `gopi session log <id> --request` failure of 2026-09-23:
//
//	$ gopi session log --last --request --text
//	Error: no raw logs found in /home/nick/.gopi/raw (the raw HTTP logger
//	may not be enabled)
//
// The raw log EXISTED — it sat beside the session JSONL, which is where
// session-dir-unify S1 (commit 3a816775) puts it (<run-uuid>.raw.log inside
// the session directory). What was stale was the READER: cmd/agent/cli_log.go
// runRequest still resolved through gopilog.FindRawLogForSession, the
// timestamp-pairing fallback that lists ONLY the legacy flat raw directory —
// empty for every run since S1. The recorded rawLogPath in the session_start
// event names the co-located file, and the shared resolver
// (gopilog.ResolveRawLogForSession, already used by `gopi session raw` / `gopi session stats`)
// follows it.
//
// These tests pin the CO-LOCATED layout so the reader can never drift back
// to the timestamp-only lookup, and they go through the CLI's own dispatch
// (runLogSubcommand) rather than the package internals, so a change in
// cli_log.go's wiring breaks the test here instead of silently in production.

// coLocatedSandbox builds a sandbox whose session JSONL lives in the
// post-S1 task-run layout — .gopi/sessions/<task_id>/<run-uuid>.jsonl —
// with its raw log beside it and the session_start event recording that
// ABSOLUTE co-located path. The legacy flat raw dir exists but is EMPTY,
// exactly as it is on a real profile after S1.
func coLocatedSandbox(t *testing.T) (home, taskID, runUUID string) {
	t.Helper()
	home = t.TempDir()
	taskID = "01a0ce03-3b08-7d9e-84c6-deb1817e696a"
	runUUID = "9642d9a7-589d-4cba-9e46-e037081f149e"

	// The legacy flat raw dir must EXIST (resolution must not fail on a
	// missing directory) but hold no raw log — that is the whole point.
	for _, d := range []string{
		filepath.Join(home, ".gopi", config.SessionsDirName),
		filepath.Join(home, ".gopi", scratch.RawLogDirName()),
		filepath.Join(home, ".gopi", config.SessionsDirName, taskID),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	sessionPath := filepath.Join(home, ".gopi", config.SessionsDirName, taskID, runUUID+".jsonl")
	recordedRaw := filepath.Join(home, ".gopi", config.SessionsDirName, taskID,
		runUUID+scratch.SessionRawLogFileSuffix)

	// Session JSONL: the real fixture session's head event, rewritten with the
	// recorded rawLogPath this layout implies (the fixture predates the field,
	// so it carries no rawLogPath at all).
	copyFixture(t, filepath.Join("sessions", statsFixtureSessionID+".jsonl"), sessionPath)

	// The raw log body is the real fixture raw log (a genuine → REQUEST marker
	// plus payload) copied to its co-located name.
	copyFixture(t, filepath.Join("raw", statsFixtureRawLogName), recordedRaw)

	// Rewrite line 1 so session_start records the co-located absolute path and
	// the run's own task/session ids.
	b, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	nl := indexByte(b, '\n')
	if nl < 0 {
		t.Fatalf("fixture session has no newline-terminated first line")
	}
	first := string(b[:nl])
	first = strings.Replace(first, `"sessionId":"`+statsFixtureSessionID+`"`,
		`"sessionId":"`+runUUID+`"`, 1)
	// Insert rawLogPath right after the opening brace (JSON key order is not
	// significant to encoding/json).
	first = `{"rawLogPath":` + jsonQuote(recordedRaw) + `,` + strings.TrimPrefix(first, "{")
	if err := os.WriteFile(sessionPath, append([]byte(first), b[nl:]...), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	t.Setenv(config.GopiDirEnv, home)
	return home, taskID, runUUID
}

// jsonQuote renders s as a JSON string literal (backslash-free paths only —
// the temp dirs these tests use are).
func jsonQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// TestRunRequestResolvesCoLocatedRawLog is the RED-then-GREEN regression: it
// fails on the pre-fix reader ("no raw logs found in .../raw", exit 1) and
// passes once runRequest resolves through ResolveRawLogForSession.
func TestRunRequestResolvesCoLocatedRawLog(t *testing.T) {
	_, _, runUUID := coLocatedSandbox(t)

	out := captureStdout(t, func() { runLogSubcommand([]string{runUUID, "--request"}) })

	// The resolved raw log's BASENAME must be named in the header — proof the
	// co-located file was found, not merely that something printed.
	wantName := runUUID + scratch.SessionRawLogFileSuffix
	if !strings.Contains(out, wantName) {
		t.Fatalf("--request did not resolve the co-located raw log %q; output:\n%s", wantName, out)
	}
	if !strings.Contains(out, "SYSTEM PROMPT") {
		t.Fatalf("--request did not render a system prompt; output:\n%s", out)
	}
	// The legacy flat-dir error must be gone.
	if strings.Contains(out, "no raw logs found") {
		t.Fatalf("--request still reports the legacy flat raw dir as empty; output:\n%s", out)
	}
}

// TestRunRequestCoLocatedJSON: --request --json reads the same co-located log
// and prints the first request body as JSON.
func TestRunRequestCoLocatedJSON(t *testing.T) {
	_, _, runUUID := coLocatedSandbox(t)

	out := captureStdout(t, func() { runLogSubcommand([]string{runUUID, "--request", "--json"}) })
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("--request --json did not print a JSON object; output head:\n%.200s", out)
	}
	if !strings.Contains(out, `"messages"`) {
		t.Fatalf("--request --json output is not a request body (no \"messages\"); head:\n%.200s", out)
	}
}

// TestRunRequestCoLocatedText: the --text form is the exact invocation from the
// bug report (`gopi session log --last --request --text`, driven here by session id);
// it must emit the prompt text with no header decoration.
func TestRunRequestCoLocatedText(t *testing.T) {
	_, _, runUUID := coLocatedSandbox(t)

	out := captureStdout(t, func() { runLogSubcommand([]string{runUUID, "--request", "--text"}) })
	if strings.Contains(out, "REQUEST — system prompt") {
		t.Fatalf("--text must omit the header block; output:\n%.200s", out)
	}
	if !strings.Contains(out, "You are GoPi") {
		t.Fatalf("--text did not print the system prompt text; output head:\n%.200s", out)
	}
}
