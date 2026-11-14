package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// ---------------------------------------------------------------------------
// parseLogArgs — the pure front-end of the log subcommand.
// ---------------------------------------------------------------------------

func TestParseLogArgsSessionAndFlags(t *testing.T) {
	opts, err := parseLogArgs([]string{"abc123", "--chat", "--text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.sessionID != "abc123" {
		t.Errorf("sessionID = %q, want abc123", opts.sessionID)
	}
	if !opts.chat || !opts.text {
		t.Errorf("chat/text flags not set: %+v", opts)
	}
	if opts.lastN != 0 {
		t.Errorf("lastN = %d, want 0 (no --last)", opts.lastN)
	}
}

func TestParseLogArgsAllViews(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--response", "--thinking", "--request", "--json", "--todo", "--memory", "--all", "--summary"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, got := range map[string]bool{
		"response": opts.response, "thinking": opts.thinking, "request": opts.request,
		"json": opts.json, "todo": opts.todo, "memory": opts.memory,
		"all": opts.all, "summary": opts.summary,
	} {
		if !got {
			t.Errorf("flag %s not set: %+v", name, opts)
		}
	}
}

func TestParseLogArgsFilterRepeatable(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--filter", "bash", "--filter", "read", "--filter=write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"bash", "read", "write"}
	if len(opts.filterNames) != len(want) {
		t.Fatalf("filterNames = %v, want %v", opts.filterNames, want)
	}
	for i := range want {
		if opts.filterNames[i] != want[i] {
			t.Errorf("filterNames[%d] = %q, want %q", i, opts.filterNames[i], want[i])
		}
	}
}

func TestParseLogArgsFilterMemoryCombined(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--memory", "--filter", "bash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.memory {
		t.Error("memory flag not set")
	}
	if len(opts.filterNames) != 1 || opts.filterNames[0] != "bash" {
		t.Errorf("filterNames = %v, want [bash]", opts.filterNames)
	}
}

func TestParseLogArgsFamilyShortcuts(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--search", "--web", "--job", "--context", "--vision", "--text2img", "--git"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, got := range map[string]bool{
		"search": opts.search, "web": opts.web, "job": opts.job, "context": opts.context, "vision": opts.vision, "text2img": opts.text2img, "git": opts.git,
	} {
		if !got {
			t.Errorf("family flag %s not set: %+v", name, opts)
		}
	}
	if filterActive(opts) != true {
		t.Error("family shortcuts must activate the filter mode")
	}
}

func TestParseLogArgsGrep(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--grep", "timeout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.grep != "timeout" {
		t.Errorf("grep = %q, want %q", opts.grep, "timeout")
	}
	if filterActive(opts) {
		t.Error("--grep alone must not activate the tool filter mode")
	}
	// --grep=TEXT form and combination with a family shortcut.
	opts, err = parseLogArgs([]string{"s1", "--grep=exit=1", "--job"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.grep != "exit=1" || !opts.job {
		t.Errorf("grep=%q job=%v, want grep=exit=1 job=true", opts.grep, opts.job)
	}
	// --grep without a value is an error (mirrors --filter).
	if _, err := parseLogArgs([]string{"s1", "--grep"}); err == nil {
		t.Error("--grep without a value should be rejected")
	}
}

func TestParseLogArgsLastDefault(t *testing.T) {
	opts, err := parseLogArgs([]string{"--last"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.lastN != 1 {
		t.Errorf("lastN = %d, want 1 (default)", opts.lastN)
	}
}

func TestParseLogArgsLastN(t *testing.T) {
	opts, err := parseLogArgs([]string{"--last", "7"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.lastN != 7 {
		t.Errorf("lastN = %d, want 7", opts.lastN)
	}
}

func TestParseLogArgsLastWithViewFlag(t *testing.T) {
	opts, err := parseLogArgs([]string{"--last", "--chat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.lastN != 1 {
		t.Errorf("lastN = %d, want 1", opts.lastN)
	}
	if !opts.chat {
		t.Error("chat flag not set")
	}
}

func TestParseLogArgsLastBadValue(t *testing.T) {
	for _, v := range []string{"abc", "0", "-3"} {
		_, err := parseLogArgs([]string{"--last", v})
		if err == nil || !strings.Contains(err.Error(), "positive integer") {
			t.Errorf("--last %s: want positive-integer error, got %v", v, err)
		}
	}
}

func TestParseLogArgsFilterMissingValue(t *testing.T) {
	_, err := parseLogArgs([]string{"s1", "--filter"})
	if err == nil || !strings.Contains(err.Error(), "--filter requires a tool name") {
		t.Fatalf("want missing-filter-value error, got %v", err)
	}
}

func TestParseLogArgsUnknownFlag(t *testing.T) {
	_, err := parseLogArgs([]string{"s1", "--bogus", "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error, got %v", err)
	}
}

// parseLogArgs must not mutate the process: verify it rejects flags belonging
// to sibling subcommands (mirrors the strict tool-arg validation).
func TestParseLogArgsRejectsSiblingFlags(t *testing.T) {
	for _, args := range [][]string{
		{"s1", "--db", "/tmp/x.db"},
		{"s1", "--method", "google"},
		{"s1", "--topic", "t"},
	} {
		_, err := parseLogArgs(args)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%v: want unknown-flag error, got %v", args, err)
		}
	}
}

// TestParseLogArgsExtraPositional: the positional slots are <session-id>
// [<round>], so a second positional that is NOT a round is refused by name (it
// cannot be a session id — one input only), and a THIRD is still the plain
// extra-argument error. Both refusals are pinned here because the round
// positional is what introduced the distinction.
//
// The token the refusal rejects is read from the SHARED grammar's own wording
// (the verb refuses it as a round, because that is the quantity the positional
// numbers — logFlagRounds), so renaming the unit again does not silently leave
// this test asserting a phrase nothing prints.
func TestParseLogArgsExtraPositional(t *testing.T) {
	_, err := parseLogArgs([]string{"s1", "s2"})
	if err == nil || !strings.Contains(err.Error(), "is not a round") {
		t.Fatalf("want a named non-round refusal for the second positional, got %v", err)
	}
	_, err = parseLogArgs([]string{"s1", "2", "3"})
	if err == nil || !strings.Contains(err.Error(), "unexpected extra argument") {
		t.Fatalf("want extra-argument error, got %v", err)
	}
}

func TestParseLogArgsEmpty(t *testing.T) {
	opts, err := parseLogArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.sessionID != "" || opts.lastN != 0 {
		t.Errorf("empty args should parse to zero-value options, got %+v", opts)
	}
}

func TestParseLogArgsContextTrim(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--context-trim"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.contextTrim {
		t.Errorf("contextTrim not set: %+v", opts)
	}
	if opts.sessionID != "s1" {
		t.Errorf("sessionID = %q, want s1", opts.sessionID)
	}
	// --context-trim combined with --last renders each of the last N sessions.
	opts2, err := parseLogArgs([]string{"--last", "3", "--context-trim"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts2.contextTrim || opts2.lastN != 3 {
		t.Errorf("last+context-trim = %+v, want contextTrim=true lastN=3", opts2)
	}
	// context-trim is a valid flag (not rejected as unknown).
	if !logFlagMember("context-trim") {
		t.Error("logFlagMember(context-trim) = false")
	}
}

// ---------------------------------------------------------------------------
// isLogSubcommand dispatch
// ---------------------------------------------------------------------------

func TestIsLogSubcommandNegative(t *testing.T) {
	if isLogSubcommand([]string{"web_search", "x"}) {
		t.Error("web_search must not dispatch log")
	}
	if isLogSubcommand(nil) {
		t.Error("empty args must not dispatch log")
	}
	if isLogSubcommand([]string{"memory_search", "x"}) {
		t.Error("memory_search must not dispatch log")
	}
	// NOTE: the positive dispatch case (args[0] == "log") is not unit-tested
	// here because it runs the subcommand, which reads real ~/.gopi/sessions
	// and calls os.Exit — covered by the smoke test in the task report.
}

// filterActive / resolveFilterSet — the family shortcut + --filter semantics.
func TestResolveFilterSetMemoryAddsMemoryTools(t *testing.T) {
	opts := &logOptions{memory: true, filterNames: []string{"bash"}}
	set := resolveFilterSet(opts)
	for _, name := range []string{"memory_search", "memory_store", "memory_feedback", "memory_update", "memory_delete", "memory_list", "bash"} {
		if !set[name] {
			t.Errorf("resolveFilterSet(memory) missing %s: %v", name, set)
		}
	}
	if set["read"] {
		t.Errorf("resolveFilterSet must not add unrelated tools: %v", set)
	}
}

func TestResolveFilterSetPlain(t *testing.T) {
	set := resolveFilterSet(&logOptions{filterNames: []string{"read", "write"}})
	if len(set) != 2 || !set["read"] || !set["write"] {
		t.Errorf("set = %v, want {read, write}", set)
	}
	if filterActive(&logOptions{}) {
		t.Error("filterActive(empty opts) must be false")
	}
	if !filterActive(&logOptions{memory: true}) || !filterActive(&logOptions{filterNames: []string{"x"}}) {
		t.Error("filterActive must be true when any family flag or filter name is given")
	}
}

// TestResolveFilterSetSearchWebJobContext — every new tool family shortcut
// expands to exactly the agent tools it advertises (mirrors loop.go inventory).
func TestResolveFilterSetSearchWebJobContext(t *testing.T) {
	cases := []struct {
		name string
		flag func(opts *logOptions)
		want []string
	}{
		{"search", func(o *logOptions) { o.search = true }, []string{"code_search", "keyword_search"}},
		{"web", func(o *logOptions) { o.web = true }, []string{"web_search", "web_fetch"}},
		{"job", func(o *logOptions) { o.job = true }, []string{"job"}},
		{"context", func(o *logOptions) { o.context = true }, []string{"context_trim"}},
		{"vision", func(o *logOptions) { o.vision = true }, []string{"vision_analyze"}},
		{"text2img", func(o *logOptions) { o.text2img = true }, []string{"text2img"}},
		{"git", func(o *logOptions) { o.git = true }, []string{"git_blame"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := &logOptions{}
			c.flag(opts)
			set := resolveFilterSet(opts)
			if len(set) != len(c.want) {
				t.Fatalf("%s: set = %v, want exactly %v", c.name, set, c.want)
			}
			for _, name := range c.want {
				if !set[name] {
					t.Errorf("%s: missing %s: %v", c.name, name, set)
				}
			}
		})
	}
	// Families compose with explicit --filter names.
	opts := &logOptions{search: true, web: true, filterNames: []string{"bash"}}
	set := resolveFilterSet(opts)
	for _, name := range []string{"code_search", "keyword_search", "web_search", "web_fetch", "bash"} {
		if !set[name] {
			t.Errorf("composed set missing %s: %v", name, set)
		}
	}
	if set["job"] {
		t.Errorf("composed set must not include unrelated family: %v", set)
	}
}

// ---------------------------------------------------------------------------
// runLast — regression: `--last N` combined with view flags must honor N.
// ---------------------------------------------------------------------------

// logFixtureSession writes a minimal valid session file (one memory_search
// tool call) into dir and returns its path. The session_start timestamp is
// derived from modTime so the fixture's ordering is identical whether
// ListSessionFiles sorts by mtime or by session_start (the P1 fix) — the
// test asserts session_start-based ordering.
func logFixtureSession(t *testing.T, dir, id string, modTime time.Time) string {
	t.Helper()
	startTS := modTime.Format(time.RFC3339)
	lines := []string{
		fmt.Sprintf(`{"type":"session_start","sessionId":%q,"model":"test-model","timestamp":%q}`, id, startTS),
		fmt.Sprintf(`{"type":"user","message":{"content":"task","role":"user"},"timestamp":%q}`, startTS),
		fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"call_1","name":"memory_search","input":{"query":"x"}}],"role":"assistant"},"timestamp":%q}`, startTS),
		fmt.Sprintf(`{"type":"session_end","elapsed_sec":1.0,"exit_code":0,"total_iterations":1,"total_tokens":10,"timestamp":%q}`, startTS),
	}
	path := filepath.Join(dir, id+".jsonl")
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write fixture %s: %v", id, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", id, err)
	}
	return path
}

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

// TestRunLastViewHonorsN is the regression for the bug where
// `gopi session log --last N --memory` rendered only the most recent session for
// every N > 1 (identical output for --last 2, --last 5, ...).
func TestRunLastViewHonorsN(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // this fixture's profile WINS over the suite pin
	sessionsDir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	base := time.Now().Add(-10 * time.Minute)
	// Newest first: A (base+3m), B (base+2m), C (base+1m).
	logFixtureSession(t, sessionsDir, "sess-AAA", base.Add(3*time.Minute))
	logFixtureSession(t, sessionsDir, "sess-BBB", base.Add(2*time.Minute))
	logFixtureSession(t, sessionsDir, "sess-CCC", base.Add(1*time.Minute))

	run := func(args ...string) string {
		opts, err := parseLogArgs(args)
		if err != nil {
			t.Fatalf("parseLogArgs(%v): %v", args, err)
		}
		return captureStdout(t, func() { runLast(opts.lastN, opts) })
	}

	// --last 1 --memory: only the newest session.
	out1 := run("--last", "1", "--memory")
	if !strings.Contains(out1, "sess-AAA") || strings.Contains(out1, "sess-BBB") {
		t.Errorf("--last 1 --memory rendered wrong sessions:\n%s", out1)
	}

	// --last 2 --memory: newest TWO sessions (the bug showed only one).
	out2 := run("--last", "2", "--memory")
	if !strings.Contains(out2, "sess-AAA") || !strings.Contains(out2, "sess-BBB") {
		t.Errorf("--last 2 --memory must render the 2 newest sessions:\n%s", out2)
	}
	if strings.Contains(out2, "sess-CCC") {
		t.Errorf("--last 2 --memory must NOT render the 3rd session:\n%s", out2)
	}
	if out1 == out2 {
		t.Errorf("--last 1 --memory and --last 2 --memory produced identical output:\n%s", out1)
	}

	// --last 5 --memory: clamps to all 3 sessions.
	out5 := run("--last", "5", "--memory")
	for _, id := range []string{"sess-AAA", "sess-BBB", "sess-CCC"} {
		if !strings.Contains(out5, id) {
			t.Errorf("--last 5 --memory must render %s:\n%s", id, out5)
		}
	}
}

// TestRunLastSkipsDegenerateSessions is the regression for the reported bug
// "gopi session log --last N is broken — all fields empty": a bare session_start-only
// JSONL file (an agent SessionLogger opened by a unit test / a run that died
// before its first turn) rendered as an empty "test-model 0.0s 0 iter 0 tok
// OK" row and crowded real sessions out of --last N. runLast must skip them.
func TestRunLastSkipsDegenerateSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // this fixture's profile WINS over the suite pin
	sessionsDir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	base := time.Now().Add(-10 * time.Minute)
	// Two REAL sessions (newest second of the pair: A, then C).
	logFixtureSession(t, sessionsDir, "sess-AAA", base.Add(3*time.Minute))
	logFixtureSession(t, sessionsDir, "sess-CCC", base.Add(1*time.Minute))
	// Two DEGENERATE session_start-only files, interleaved as the NEWEST —
	// the exact shape that made --last N show empty rows first.
	for i, id := range []string{"sess-STUB-NEW", "sess-STUB-OLD"} {
		p := filepath.Join(sessionsDir, id+".jsonl")
		ts := base.Add(time.Duration(2-i) * time.Minute).Format(time.RFC3339)
		if err := os.WriteFile(p, []byte(
			fmt.Sprintf(`{"type":"session_start","sessionId":%q,"model":"test-model","timestamp":%q}`, id, ts)),
			0644); err != nil {
			t.Fatalf("write stub %s: %v", id, err)
		}
	}

	run := func(args ...string) string {
		opts, err := parseLogArgs(args)
		if err != nil {
			t.Fatalf("parseLogArgs(%v): %v", args, err)
		}
		return captureStdout(t, func() { runLast(opts.lastN, opts) })
	}

	// --last 2: the two REAL sessions, in order; NO degenerate rows.
	out := run("--last", "2")
	if !strings.Contains(out, "sess-AAA") || !strings.Contains(out, "sess-CCC") {
		t.Errorf("--last 2 must render both real sessions:\n%s", out)
	}
	if strings.Contains(out, "sess-STUB-NEW") || strings.Contains(out, "sess-STUB-OLD") {
		t.Errorf("--last 2 must skip degenerate session_start-only files:\n%s", out)
	}
	// The degenerate files must not be counted toward N (they do nothing —
	// --last 1 shows only the newest REAL session).
	out1 := run("--last", "1")
	if !strings.Contains(out1, "sess-AAA") || strings.Contains(out1, "sess-CCC") {
		t.Errorf("--last 1 rendered wrong session after skipping degenerates:\n%s", out1)
	}
}

// ---------------------------------------------------------------------------
// --raw view: parse + dispatch plumbing
// ---------------------------------------------------------------------------

func TestParseLogArgsRaw(t *testing.T) {
	opts, err := parseLogArgs([]string{"s1", "--raw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.raw {
		t.Errorf("raw not set: %+v", opts)
	}
	if opts.sessionID != "s1" {
		t.Errorf("sessionID = %q, want s1", opts.sessionID)
	}
	if !logFlagMember("raw") {
		t.Error("logFlagMember(raw) = false")
	}
	// --raw --json: JSON flag coexists (dispatch order gives raw precedence).
	opts, err = parseLogArgs([]string{"s1", "--raw", "--json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.raw || !opts.json {
		t.Errorf("raw/json not both set: %+v", opts)
	}
	// --last N --raw: raw is a view flag honored per session.
	opts, err = parseLogArgs([]string{"--last", "3", "--raw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.lastN != 3 || !opts.raw {
		t.Errorf("last+raw = %+v, want lastN=3 raw=true", opts)
	}
}

// ---------------------------------------------------------------------------
// BUG-3 regression: one corrupt session file must NOT kill `--last N --view`.
// Previously renderSessionView called os.Exit(1) on a ParseFile error, so a
// single bad file aborted the whole run; the summary path skipped gracefully.
// Now it warns on stderr and continues, and the other rows still render.
// ---------------------------------------------------------------------------

func TestRunLastViewContinuesPastCorruptFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // this fixture's profile WINS over the suite pin
	sessionsDir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	base := time.Now().Add(-10 * time.Minute)
	// Session A: corrupt JSONL (the old crash trigger). Session B: valid.
	logFixtureSession(t, sessionsDir, "sess-GOOD", base.Add(2*time.Minute))
	corrupt := filepath.Join(sessionsDir, "sess-CORRUPT.jsonl")
	if err := os.WriteFile(corrupt, []byte("{not-json\n"), 0644); err != nil {
		t.Fatalf("write corrupt session: %v", err)
	}
	_ = corrupt

	run := func(args ...string) string {
		opts, err := parseLogArgs(args)
		if err != nil {
			t.Fatalf("parseLogArgs(%v): %v", args, err)
		}
		return captureStdout(t, func() { runLast(opts.lastN, opts) })
	}

	// --last 2 --memory: the corrupt file must not abort the run — the valid
	// session still renders.
	out := run("--last", "2", "--memory")
	if !strings.Contains(out, "sess-GOOD") {
		t.Errorf("--last 2 --memory must render the valid session past the corrupt one:\n%s", out)
	}
	// And the corrupt file's id must not appear as a rendered session.
	if strings.Contains(out, "sess-CORRUPT") {
		t.Errorf("corrupt session leaked into output:\n%s", out)
	}
}

// TestRunLastCorruptFirstFile: corrupt file is NEWEST (sorted first) — the
// view still continues to the older valid session.
func TestRunLastCorruptFirstFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // this fixture's profile WINS over the suite pin
	sessionsDir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	base := time.Now().Add(-10 * time.Minute)
	logFixtureSession(t, sessionsDir, "sess-OLD", base.Add(1*time.Minute))
	corrupt := filepath.Join(sessionsDir, "sess-NEWCORRUPT.jsonl")
	if err := os.WriteFile(corrupt, []byte("{bad\n"), 0644); err != nil {
		t.Fatalf("write corrupt session: %v", err)
	}
	// Newest mtime so it sorts first.
	if err := os.Chtimes(corrupt, time.Now(), time.Now()); err != nil {
		t.Fatalf("chtimes corrupt: %v", err)
	}

	opts, err := parseLogArgs([]string{"--last", "2", "--memory"})
	if err != nil {
		t.Fatalf("parseLogArgs: %v", err)
	}
	out := captureStdout(t, func() { runLast(opts.lastN, opts) })
	if !strings.Contains(out, "sess-OLD") {
		t.Errorf("--last 2 --memory must render the older valid session when the newest is corrupt:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// --http flag parsing
// ---------------------------------------------------------------------------

func TestParseLogArgsHTTP(t *testing.T) {
	// bare --http → summary (All)
	opts, err := parseLogArgs([]string{"s1", "--http"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.http || opts.httpArg != "" {
		t.Errorf("--http: http/httpArg = %v/%q, want true/\"\"", opts.http, opts.httpArg)
	}

	// --http N → detail of round N
	opts, err = parseLogArgs([]string{"s1", "--http", "3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.http || opts.httpArg != "3" {
		t.Errorf("--http 3: http/httpArg = %v/%q, want true/\"3\"", opts.http, opts.httpArg)
	}

	// --http last → last round
	opts, err = parseLogArgs([]string{"s1", "--http", "last"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.http || opts.httpArg != "last" {
		t.Errorf("--http last: http/httpArg = %v/%q, want true/\"last\"", opts.http, opts.httpArg)
	}

	// --last N --http → view applied per session
	opts, err = parseLogArgs([]string{"--last", "2", "--http"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.lastN != 2 || !opts.http {
		t.Errorf("--last 2 --http: lastN/http = %d/%v, want 2/true", opts.lastN, opts.http)
	}

	// --http N --request / --http N --response parse as http+their view flag.
	opts, err = parseLogArgs([]string{"s1", "--http", "2", "--request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.http || opts.httpArg != "2" || !opts.request {
		t.Errorf("--http 2 --request: http/httpArg/request = %v/%q/%v", opts.http, opts.httpArg, opts.request)
	}
	opts, err = parseLogArgs([]string{"s1", "--http", "2", "--response"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.http || opts.httpArg != "2" || !opts.response {
		t.Errorf("--http 2 --response: http/httpArg/response = %v/%q/%v", opts.http, opts.httpArg, opts.response)
	}
}

// ---------------------------------------------------------------------------
// The raw-HTTP-log views moved out of `gopi session log` (gopi session stats / gopi session raw).
// The flags still PARSE so the refusal can name the exact equivalent command;
// runLogSubcommand prints logMovedPointer's one line and exits non-zero.
// ---------------------------------------------------------------------------

// TestLogMovedPointerTable pins the migration table from
// FINAL-USAGE-PROPOSAL: every moved invocation names its new equivalent.
func TestLogMovedPointerTable(t *testing.T) {
	cases := []struct {
		name string
		opts *logOptions
		want string
	}{
		{"--http bare", &logOptions{http: true},
			`gopi session log <id> --http moved out of gopi session log — use: gopi session stats <id> --rounds`},
		{"--http all", &logOptions{http: true, httpArg: "all"},
			`gopi session log <id> --http moved out of gopi session log — use: gopi session stats <id> --rounds`},
		{"--http N", &logOptions{http: true, httpArg: "3"},
			`gopi session log <id> --http 3 moved out of gopi session log — use: gopi session raw <id> 3`},
		{"--http N --request", &logOptions{http: true, httpArg: "3", request: true},
			`gopi session log <id> --http 3 --request moved out of gopi session log — use: gopi session raw <id> 3 request`},
		{"--http N --response", &logOptions{http: true, httpArg: "3", response: true},
			`gopi session log <id> --http 3 --response moved out of gopi session log — use: gopi session raw <id> 3 response`},
		{"--http last", &logOptions{http: true, httpArg: "last"},
			`gopi session log <id> --http last moved out of gopi session log — use: gopi session raw <id> last`},
		{"--http negative -2", &logOptions{http: true, httpArg: "-2"},
			`gopi session log <id> --http -2 moved out of gopi session log — use: gopi session raw <id> -2`},
		{"--raw", &logOptions{raw: true},
			`gopi session log <id> --raw moved out of gopi session log — use: gopi session raw <id>`},
		{"--last --raw", &logOptions{raw: true, lastN: 4},
			`gopi session log --last 4 --raw moved out of gopi session log — use: gopi session raw --last 4`},
		{"--last --http", &logOptions{http: true, lastN: 4},
			`gopi session log --last N --http moved out of gopi session log — use: gopi session stats --last`},
	}
	for _, c := range cases {
		if got := logMovedPointer(c.opts); got != c.want {
			t.Errorf("%s: logMovedPointer =\n  %q\nwant\n  %q", c.name, got, c.want)
		}
		if line := strings.Count(c.want, "\n"); line != 0 {
			t.Errorf("%s: pointer must be ONE line", c.name)
		}
	}
}

// TestLogUsageNamesTheNewCommands: the usage documents the move (a user must
// learn where the views went, not just get an error).
func TestLogUsageNamesTheNewCommands(t *testing.T) {
	for _, want := range []string{
		"Moved to the raw-log commands",
		"gopi session stats <id> [--rounds]",
		"gopi session raw <id> [N] [request|response]",
	} {
		if !strings.Contains(logUsage, want) {
			t.Errorf("logUsage is missing %q", want)
		}
	}
	// The moved flags are no longer advertised as usable views.
	if strings.Contains(logUsage, "gopi session log <session-id> --raw") {
		t.Error("logUsage still advertises --raw as a log view")
	}
	if strings.Contains(logUsage, "--http N --request") && !strings.Contains(logUsage, "Moved to") {
		t.Error("logUsage still advertises --http as a log view")
	}
}
