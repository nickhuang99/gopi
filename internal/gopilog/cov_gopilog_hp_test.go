package gopilog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// This file closes the function-level coverage gap of internal/gopilog for the
// exported functions ParseHTTPSelector, ResolveSessionDir, SessionRawLogPath,
// SessionStarted and (*SessionStats).ToolRanking. Every function is CALLED
// here with real assertions (the func_coverage.py metric is name-appearance
// based, so a mere mention would be dishonest coverage).
//
// Helper prefix: covGlog* — task-unique, so a sibling coverage slice touching
// internal/gopilog cannot collide with a duplicate symbol at merge time.

// Event wire values are referenced through these named constants instead of
// being re-typed (PROJECT.md Rule 1b/13). NOTE: the product source still spells
// "session_start" inline in five places (formatter.go:801, rawlog.go:251/315/
// 342, reader.go:514) — a pre-existing product-side gap that a tests-only
// slice must not "fix"; reader.go:331 defines eventTypeSessionEnd unexported,
// so a same-package test may reference it, and covGlogEventSessionStart is
// this file's honest stand-in for the missing session_start constant.
const (
	covGlogEventSessionStart = "session_start"
	covGlogEventEnd          = eventTypeSessionEnd
	covGlogEventRequest      = "request"

	// covGlogRawLogPath is the raw-log path a fixture session_start records.
	covGlogRawLogPath = "/tmp/cov-glog/raw/session-1.log"
)

// covGlogSessionStartLine renders a session_start JSONL line carrying ts
// (zero value -> the timestamp key is omitted) and rawLogPath ("" -> omitted,
// which is how a pre-resume-feature session file looks).
func covGlogSessionStartLine(ts time.Time, rawLogPath string) string {
	line := fmt.Sprintf(`{"type":%q`, covGlogEventSessionStart)
	if !ts.IsZero() {
		line += fmt.Sprintf(`,"timestamp":%q`, ts.UTC().Format(time.RFC3339))
	}
	if rawLogPath != "" {
		line += fmt.Sprintf(`,"rawLogPath":%q`, rawLogPath)
	}
	return line + "}"
}

// covGlogWriteJSONL writes lines (one per JSONL record) into a fresh temp
// file and returns its path. Everything lives under t.TempDir() — never the
// repo tree and never ~/.gopi.
func covGlogWriteJSONL(t *testing.T, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

// --- ParseHTTPSelector -----------------------------------------------------

// TestCovGlogParseHTTPSelector_Matrix covers the accepted --http grammar:
// empty/whitespace, "all", "last", a positive round, 0 (resolved to the last
// round downstream) and negative offsets.
func TestCovGlogParseHTTPSelector_Matrix(t *testing.T) {
	cases := []struct {
		name string
		arg  string
		want HTTPRoundSelector
	}{
		{"empty means the whole-round listing", "", HTTPRoundSelector{All: true}},
		{"whitespace-only trims to empty", " \t ", HTTPRoundSelector{All: true}},
		{"all", "all", HTTPRoundSelector{All: true}},
		{"all with padding", "  all  ", HTTPRoundSelector{All: true}},
		{"last", "last", HTTPRoundSelector{Detail: true, Round: -1}},
		{"last with padding", "\tlast\n", HTTPRoundSelector{Detail: true, Round: -1}},
		{"positive round number", "7", HTTPRoundSelector{Detail: true, Round: 7}},
		{"explicit zero round", "0", HTTPRoundSelector{Detail: true, Round: 0}},
		{"negative = nth from last", "-3", HTTPRoundSelector{Detail: true, Round: -3}},
		{"leading plus is valid Atoi input", "+9", HTTPRoundSelector{Detail: true, Round: 9}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseHTTPSelector(tc.arg)
			if err != nil {
				t.Fatalf("ParseHTTPSelector(%q): unexpected error: %v", tc.arg, err)
			}
			if got != tc.want {
				t.Errorf("ParseHTTPSelector(%q) = %+v, want %+v", tc.arg, got, tc.want)
			}
		})
	}
}

// TestCovGlogParseHTTPSelector_Malformed covers the error path: a non-numeric
// / half-numeric argument is refused, no partial selector leaks out, and the
// rejected value is echoed so the operator can see what was wrong.
func TestCovGlogParseHTTPSelector_Malformed(t *testing.T) {
	for _, arg := range []string{"abc", "1x", "-", "--2", "1.5", "all2", "last1", "0x10", "٣"} {
		t.Run(fmt.Sprintf("arg=%q", arg), func(t *testing.T) {
			got, err := ParseHTTPSelector(arg)
			if err == nil {
				t.Fatalf("ParseHTTPSelector(%q) = %+v, want an error", arg, got)
			}
			if got != (HTTPRoundSelector{}) {
				t.Errorf("error path must not return a partial selector, got %+v", got)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", arg)) {
				t.Errorf("error %q should echo the rejected argument %q", err, arg)
			}
		})
	}
}

// --- ResolveSessionDir -----------------------------------------------------

// TestCovGlogResolveSessionDir_Happy covers the happy path under an isolated
// GOPI_HOME plus the purity contract: resolving a path must not create it.
func TestCovGlogResolveSessionDir_Happy(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.GopiDirEnv, root)

	got, err := ResolveSessionDir()
	if err != nil {
		t.Fatalf("ResolveSessionDir: %v", err)
	}
	want := filepath.Join(root, ".gopi", config.SessionsDirName)
	if got != want {
		t.Errorf("ResolveSessionDir() = %q, want %q", got, want)
	}
	// Single source of truth: the sessions root is exactly one level under the
	// gopi data dir, and its base name IS config.SessionsDirName.
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("config.GopiDir: %v", err)
	}
	if filepath.Dir(got) != gd {
		t.Errorf("parent of %q = %q, want the gopi dir %q", got, filepath.Dir(got), gd)
	}
	if filepath.Base(got) != config.SessionsDirName {
		t.Errorf("base name = %q, want %q", filepath.Base(got), config.SessionsDirName)
	}
	// Boundary: resolving is not creating.
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("ResolveSessionDir must not create %q (stat err = %v)", got, err)
	}
}

// TestCovGlogResolveSessionDir_Error covers the error path: GOPI_HOME empty
// AND no HOME to fall back on -> the error is wrapped with the gopi-dir
// context, never swallowed.
func TestCovGlogResolveSessionDir_Error(t *testing.T) {
	t.Setenv(config.GopiDirEnv, "")
	t.Setenv("HOME", "")

	got, err := ResolveSessionDir()
	if err == nil {
		t.Fatalf("ResolveSessionDir() = %q, want an error when no home is resolvable", got)
	}
	if got != "" {
		t.Errorf("error path must return an empty dir, got %q", got)
	}
	if !strings.Contains(err.Error(), "cannot resolve gopi dir") {
		t.Errorf("error %q should carry the gopi-dir context", err)
	}
}

// --- SessionRawLogPath -----------------------------------------------------

func TestCovGlogSessionRawLogPath(t *testing.T) {
	reqLine := fmt.Sprintf(`{"type":%q,"iteration":0}`, covGlogEventRequest)

	t.Run("happy: rawLogPath recorded by session_start", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl",
			covGlogSessionStartLine(time.Now(), covGlogRawLogPath),
			reqLine,
		)
		if got := SessionRawLogPath(path); got != covGlogRawLogPath {
			t.Errorf("SessionRawLogPath = %q, want %q", got, covGlogRawLogPath)
		}
	})

	t.Run("boundary: event on a later line after junk is still found", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl",
			`{"type":"unknown_legacy_event"}`,
			`{"type":`, // malformed JSON — skipped, the scan continues
			"",         // blank line — skipped
			covGlogSessionStartLine(time.Now(), covGlogRawLogPath),
		)
		if got := SessionRawLogPath(path); got != covGlogRawLogPath {
			t.Errorf("SessionRawLogPath = %q, want %q (scan must survive malformed lines)", got, covGlogRawLogPath)
		}
	})

	t.Run("boundary: pre-resume session has no rawLogPath", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl",
			covGlogSessionStartLine(time.Now(), ""),
			reqLine,
		)
		if got := SessionRawLogPath(path); got != "" {
			t.Errorf("SessionRawLogPath = %q, want \"\" for a session predating the field", got)
		}
	})

	t.Run("boundary: no session_start event at all", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl", reqLine, reqLine)
		if got := SessionRawLogPath(path); got != "" {
			t.Errorf("SessionRawLogPath = %q, want \"\"", got)
		}
	})

	t.Run("boundary: empty and blank-only files", func(t *testing.T) {
		for name, lines := range map[string][]string{
			"empty":      {},
			"blank-only": {"", "  "},
		} {
			path := covGlogWriteJSONL(t, name+".jsonl", lines...)
			if got := SessionRawLogPath(path); got != "" {
				t.Errorf("%s: SessionRawLogPath = %q, want \"\"", name, got)
			}
		}
	})

	t.Run("error: unreadable inputs return the empty fallback", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope.jsonl")
		if got := SessionRawLogPath(missing); got != "" {
			t.Errorf("missing file: SessionRawLogPath = %q, want \"\"", got)
		}
		// A directory opens fine but cannot be scanned.
		if got := SessionRawLogPath(t.TempDir()); got != "" {
			t.Errorf("directory: SessionRawLogPath = %q, want \"\"", got)
		}
	})
}

// --- SessionStarted --------------------------------------------------------

func TestCovGlogSessionStarted(t *testing.T) {
	ts := time.Date(2026, 9, 10, 8, 30, 15, 0, time.UTC)
	reqLine := fmt.Sprintf(`{"type":%q,"iteration":0}`, covGlogEventRequest)
	// A request event whose CONTENT embeds the probe literal: the pre-filter
	// lets it through, the decode rejects it — the documented harmless path.
	probeDecoy := fmt.Sprintf(
		`{"type":%q,"message":{"role":"user","content":"we should log \"%s\" here"}}`,
		covGlogEventRequest, covGlogEventSessionStart)

	t.Run("happy: session_start timestamp", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl",
			covGlogSessionStartLine(ts, covGlogRawLogPath), reqLine)
		got, ok := SessionStarted(path)
		if !ok {
			t.Fatalf("SessionStarted(%s) ok = false, want true", path)
		}
		if !got.Equal(ts) {
			t.Errorf("SessionStarted = %s, want %s", got.Format(time.RFC3339), ts.Format(time.RFC3339))
		}
	})

	t.Run("boundary: zero timestamp is not a start time", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl", covGlogSessionStartLine(time.Time{}, ""), reqLine)
		got, ok := SessionStarted(path)
		if ok {
			t.Fatalf("SessionStarted = %s, want ok=false for a zero timestamp", got)
		}
		if !got.IsZero() {
			t.Errorf("SessionStarted = %s, want the zero time on failure", got)
		}
	})

	t.Run("boundary: decoy probe match does not mask the real event", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl",
			probeDecoy,
			`{"type":`, // malformed line that carries no probe — skipped by the pre-filter
			covGlogSessionStartLine(ts, ""),
		)
		got, ok := SessionStarted(path)
		if !ok || !got.Equal(ts) {
			t.Errorf("SessionStarted = (%s, %v), want (%s, true)", got, ok, ts.Format(time.RFC3339))
		}
	})

	t.Run("boundary: session_end line is not a start", func(t *testing.T) {
		path := covGlogWriteJSONL(t, "s.jsonl",
			fmt.Sprintf(`{"type":%q,"timestamp":%q,"elapsed_sec":12.5}`,
				covGlogEventEnd, ts.Format(time.RFC3339)),
			reqLine)
		if got, ok := SessionStarted(path); ok {
			t.Errorf("SessionStarted = (%s, true), want ok=false (only session_start counts)", got)
		}
	})

	t.Run("error: unreadable inputs report not-found", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope.jsonl")
		if got, ok := SessionStarted(missing); ok || !got.IsZero() {
			t.Errorf("missing file: SessionStarted = (%s, %v), want (zero, false)", got, ok)
		}
		if got, ok := SessionStarted(t.TempDir()); ok || !got.IsZero() {
			t.Errorf("directory: SessionStarted = (%s, %v), want (zero, false)", got, ok)
		}
		empty := covGlogWriteJSONL(t, "empty.jsonl")
		if got, ok := SessionStarted(empty); ok || !got.IsZero() {
			t.Errorf("empty file: SessionStarted = (%s, %v), want (zero, false)", got, ok)
		}
	})
}

// --- (*SessionStats).ToolRanking -------------------------------------------

// TestCovGlogToolRanking_Ordering covers the happy path and both ordering
// keys: calls descending, then name ascending; plus the empty boundary and the
// caller-independence contract (the receiver's slice is copied, never sorted
// in place).
func TestCovGlogToolRanking_Ordering(t *testing.T) {
	st := &SessionStats{ToolKinds: []ToolCount{
		{Name: toolnames.ToolBash, Calls: 2},
		{Name: toolnames.ToolRead, Calls: 9},
		{Name: toolnames.ToolWrite, Calls: 2},
		{Name: toolnames.ToolEdit, Calls: 2},
	}}

	// Snapshot the receiver so the purity contract can be asserted after.
	before := append([]ToolCount(nil), st.ToolKinds...)

	got := st.ToolRanking()
	want := []ToolCount{
		{Name: toolnames.ToolRead, Calls: 9},
		{Name: toolnames.ToolBash, Calls: 2},
		{Name: toolnames.ToolEdit, Calls: 2},
		{Name: toolnames.ToolWrite, Calls: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("ToolRanking len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ToolRanking()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Purity: ranking must not reorder the receiver's own slice.
	for i := range before {
		if st.ToolKinds[i] != before[i] {
			t.Fatalf("ToolRanking mutated the receiver: ToolKinds[%d] = %+v, want %+v",
				i, st.ToolKinds[i], before[i])
		}
	}
	// Determinism: a repeat call yields the identical order.
	again := st.ToolRanking()
	for i := range want {
		if again[i] != got[i] {
			t.Errorf("ToolRanking is not stable: [%d] %+v vs %+v", i, again[i], got[i])
		}
	}

	// Boundary: no tool calls recorded.
	if empty := (&SessionStats{}).ToolRanking(); len(empty) != 0 {
		t.Errorf("ToolRanking on an empty stats = %+v, want no entries", empty)
	}
}

// TestCovGlogToolRanking_FromRounds exercises the ranking on real parsed
// rounds (the fixture's round 3 issues read, read, bash) instead of a hand-
// built slice.
func TestCovGlogToolRanking_FromRounds(t *testing.T) {
	_, rounds := statsRawLog(t)
	st := BuildStatsFromRounds(rounds)

	rank := st.ToolRanking()
	if len(rank) != 2 {
		t.Fatalf("ToolRanking = %+v, want 2 tools (read, bash)", rank)
	}
	if rank[0] != (ToolCount{Name: toolnames.ToolRead, Calls: 2}) {
		t.Errorf("rank[0] = %+v, want read with 2 calls", rank[0])
	}
	if rank[1] != (ToolCount{Name: toolnames.ToolBash, Calls: 1}) {
		t.Errorf("rank[1] = %+v, want bash with 1 call", rank[1])
	}
	// The ranking is a view over the stats' own tool kinds, not a new tally.
	if st.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3", st.ToolCalls)
	}
}
