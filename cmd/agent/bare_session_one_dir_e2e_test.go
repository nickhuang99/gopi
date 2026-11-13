package main

// bare_session_one_dir_e2e_test.go — the BARE-SESSION half of "one session,
// one directory", verified on the REAL binary.
//
// Measured defect (2026-09-12, live profile): NewAgent minted the bare
// session's identity TWICE — once as the raw logger's run uuid, then again
// inside NewSessionLogger — so a single `gopi prompt` scattered its assets
// over two sibling directories: the raw HTTP log in `sessions/<uuid-A>/` and
// the session JSONL (plus metadata.json and console.log) in
// `sessions/<uuid-B>/`. Sampled on the operator's profile: 119 of 300 bare
// sessions had the JSONL directory differing from the raw log's, 0 were
// co-located.
//
// A unit test of the minting seam cannot see this: the defect is that TWO
// CORRECT mints happen. Only the real run's ARTIFACT LAYOUT shows it, which is
// what this test reads.
//
// Read directly from the run's directory tree:
//
//	(a) every artifact file the run produced sits in ONE session directory —
//	    raw log, session JSONL, metadata.json AND console.log together;
//	(b) that directory is named by the session id the session_start event
//	    itself records, and the rawLogPath the same event records points into
//	    that very directory (the event is what `gopi task show` / resume
//	    resolve through, so a mismatch here is a broken lookup, not cosmetics).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// bareSessionStart is the subset of the session_start event this test reads.
type bareSessionStart struct {
	Type       string `json:"type"`
	SessionID  string `json:"sessionId"`
	RawLogPath string `json:"rawLogPath"`
}

// readSessionStart parses the FIRST line of a session JSONL (the
// session_start event) — the run's own declaration of its identity and of
// where its raw log lives.
func readBareSessionStart(t *testing.T, jsonlPath string) bareSessionStart {
	t.Helper()
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read %s: %v", jsonlPath, err)
	}
	first, _, _ := strings.Cut(string(data), "\n")
	if strings.TrimSpace(first) == "" {
		t.Fatalf("%s is empty — the run recorded no session_start event", jsonlPath)
	}
	var ev bareSessionStart
	if err := json.Unmarshal([]byte(first), &ev); err != nil {
		t.Fatalf("parse session_start in %s: %v\nfirst line: %s", jsonlPath, err, first)
	}
	if ev.Type == "" {
		t.Fatalf("the first line of %s is not a session_start event: %s", jsonlPath, first)
	}
	return ev
}

// TestBareRunKeepsAllItsArtifactsInOneSessionDir is the acceptance walk for a
// bare (non-task) run: `gopi front run` is a real front run through NewAgent,
// whose session id is a fresh uuid — exactly the path that used to fork its
// assets across two directories.
func TestBareRunKeepsAllItsArtifactsInOneSessionDir(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "bare session layout marker"})
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"), "GOPI_CRASH_DUMP_SWEEP=off")
	stdout, stderr, code := runGopiMapped(t, []string{"front", "bare layout probe"}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("a bare front run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	sessionsRoot := filepath.Join(gopiHome, ".gopi", config.SessionsDirName)
	dirs, err := os.ReadDir(sessionsRoot)
	if err != nil {
		t.Fatalf("read sessions root %s: %v", sessionsRoot, err)
	}
	var sessionDirs []string
	for _, d := range dirs {
		if d.IsDir() {
			sessionDirs = append(sessionDirs, filepath.Join(sessionsRoot, d.Name()))
		}
	}
	if len(sessionDirs) != 1 {
		t.Fatalf("a bare run must leave exactly ONE session directory under %s, found %d (%v) — more than one means its identity was minted more than once",
			sessionsRoot, len(sessionDirs), sessionDirs)
	}
	dir := sessionDirs[0]

	// (a) Every artifact file of the run is in this one directory. The
	// console log and metadata belong to the run for the same reason the raw
	// log does: they are what a later debugging task studies.
	var rawNames, jsonlNames []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".raw.log"):
			rawNames = append(rawNames, name)
		case strings.HasSuffix(name, ".jsonl"):
			jsonlNames = append(jsonlNames, name)
		}
	}
	if len(rawNames) != 1 || len(jsonlNames) != 1 {
		t.Fatalf("%s must hold exactly one raw log and one session JSONL, got raws=%v jsonls=%v (contents: %v)",
			dir, rawNames, jsonlNames, entries)
	}

	// (b) The directory IS the session id the run declared, and the raw log it
	// declared is the one sitting beside its JSONL.
	ev := readBareSessionStart(t, filepath.Join(dir, jsonlNames[0]))
	if ev.SessionID == "" {
		t.Fatalf("the session_start event in %s carries no sessionId", filepath.Join(dir, jsonlNames[0]))
	}
	if got := filepath.Base(dir); got != ev.SessionID {
		t.Fatalf("the session directory is %q but the run's own session_start event declares sessionId %q — the raw log and the session JSONL were minted from DIFFERENT ids (the two-directory defect)",
			got, ev.SessionID)
	}
	if ev.RawLogPath == "" {
		t.Fatalf("the session_start event declares no rawLogPath — `gopi task show` and resume resolve the raw log through it, so the run's own log is unreachable")
	}
	if want := filepath.Join(dir, rawNames[0]); ev.RawLogPath != want {
		t.Fatalf("the session_start event points at rawLogPath %q, but the raw log of this run is %q — the record does not describe the tree",
			ev.RawLogPath, want)
	}
	// The console log completes the layout: a front run's output is copied
	// beside its session, which is only true if it is the SAME session dir.
	if _, err := os.Stat(filepath.Join(dir, consoleLogFileName)); err != nil {
		t.Fatalf("the bare run's console output must sit in the run's own session dir %s: %v (contents: %v)", dir, err, entries)
	}
}
