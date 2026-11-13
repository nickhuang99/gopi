package main

import (
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/gopilog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- parsing ----

// TestRawFileFlagFromLeafConstantIsAccepted is the DISPATCH half of the drift
// guard for the one flag the chat prompt renders that has two spellings: the
// parser matches a bare WORD (rawFlagFile) while the prompt renders the flag
// (cliverbs.FileFlag). The prompt's claim — "`gopi session raw --file <path>` prints
// this run's wire log verbatim" — is only true if the CLI accepts exactly that
// spelling, so the parser is driven with the LEAF CONSTANT's value rather than a
// literal typed here. A rename on either side (the word, or the flag derived
// from it) fails this test instead of leaving the prompt teaching an invocation
// the CLI rejects with "unknown flag".
func TestRawFileFlagFromLeafConstantIsAccepted(t *testing.T) {
	opts, err := parseRawArgs([]string{cliverbs.FileFlag, "/tmp/x.log", "last"})
	if err != nil {
		t.Fatalf("parseRawArgs rejected the chat prompt's own invocation %q %q last: %v",
			cliverbs.FileFlag, "/tmp/x.log", err)
	}
	if opts.filePath != "/tmp/x.log" {
		t.Fatalf("the flag did not select the file: filePath=%q", opts.filePath)
	}
	// The parser's word and the rendered flag are the SAME flag, spelled for the
	// two boundaries — this is the relationship the chat prompt depends on.
	if want := "--" + cliverbs.RawFileWord; cliverbs.FileFlag != want {
		t.Fatalf("cliverbs.FileFlag=%q is not %q — the two spellings drifted", cliverbs.FileFlag, want)
	}
	if rawFlagFile != cliverbs.RawFileWord {
		t.Fatalf("rawFlagFile=%q is not the leaf constant %q", rawFlagFile, cliverbs.RawFileWord)
	}
}

func TestParseRawArgsPositionals(t *testing.T) {
	opts, err := parseRawArgs([]string{"sess"})
	if err != nil {
		t.Fatalf("id only: %v", err)
	}
	if opts.sessionID != "sess" || opts.sel.Detail || opts.part != "" {
		t.Errorf("id only = %+v", opts)
	}
	if opts, err = parseRawArgs([]string{"sess", "3"}); err != nil || opts.sel.Round != 3 || !opts.sel.Detail {
		t.Errorf("id+round: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "3", "request"}); err != nil || opts.part != "request" {
		t.Errorf("id+round+part: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "3", "RESPONSE"}); err != nil || opts.part != "response" {
		t.Errorf("part is case-insensitive: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"--file", "/tmp/x.log", "3", "request"}); err != nil {
		t.Fatalf("--file + round: %v", err)
	}
	if opts.filePath != "/tmp/x.log" || opts.sel.Round != 3 || opts.part != "request" {
		t.Errorf("--file + round = %+v", opts)
	}
}

func TestParseRawArgsFlags(t *testing.T) {
	opts, err := parseRawArgs([]string{"sess", "--rounds"})
	if err != nil || !opts.roundsIndex {
		t.Errorf("--rounds: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "--sysprompt"}); err != nil || !opts.sysprompt {
		t.Errorf("--sysprompt: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "--userprompt"}); err != nil || !opts.userprompt {
		t.Errorf("--userprompt: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "--lines", "8,431-8,612"}); err == nil {
		t.Errorf("--lines with commas must be rejected, got %+v", opts)
	}
	if opts, err = parseRawArgs([]string{"sess", "--lines", "10-12"}); err != nil || opts.lineFirst != 10 || opts.lineLast != 12 {
		t.Errorf("--lines 10-12: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "--lines", "10"}); err != nil || opts.lineFirst != 10 || opts.lineLast != 10 {
		t.Errorf("--lines 10: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"sess", "--lines", "10-"}); err != nil || opts.lineLast != -1 {
		t.Errorf("--lines 10- (to EOF): opts=%+v err=%v", opts, err)
	}
	if opts, err = parseRawArgs([]string{"--last"}); err != nil || opts.lastN != 1 {
		t.Errorf("--last: opts=%+v err=%v", opts, err)
	}
}

func TestParseRawArgsRejects(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{}, "pick one input"},
		{[]string{"sess", "--file", "/x.log"}, "EXCLUSIVE"},
		{[]string{"sess", "3", "4"}, "the last argument is request|response"},
		{[]string{"sess", "3", "4", "extra"}, "unexpected extra argument"},
		{[]string{"sess", "3", "sideways"}, "request|response"},
		{[]string{"sess", "all"}, "<round> must name one round"},
		{[]string{"sess", "ALL"}, "--http takes all|last|N"},
		{[]string{"sess", "--bogus"}, "unknown flag --bogus"},
		{[]string{"--file", "/x.log", "--sysprompt", "--rounds"}, "separate modes"},
		{[]string{"sess", "--userprompt", "--sysprompt"}, "separate modes"},
		{[]string{"sess", "3", "--rounds"}, "cannot be combined"},
		{[]string{"sess", "request"}, "--http takes all|last|N"},
		{[]string{"sess", "--lines", "9-2"}, "B >= A"},
		{[]string{"sess", "--lines", "abc"}, "A-B with positive line numbers"},
	}
	for _, c := range cases {
		_, err := parseRawArgs(c.args)
		if err == nil {
			t.Errorf("args %v accepted, want an error mentioning %q", c.args, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("args %v → %q, want it to mention %q", c.args, err, c.want)
		}
	}
}

// TestParseRawArgsRefusesFormatFlags is the "raw = BYTES" guard: a format flag
// must be an error naming the flag, never a silent reinterpretation.
func TestParseRawArgsRefusesFormatFlags(t *testing.T) {
	for _, flag := range []string{"--json", "--text"} {
		_, err := parseRawArgs([]string{"sess", flag})
		if err == nil {
			t.Errorf("%s accepted — raw must never take a format flag", flag)
			continue
		}
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("error for %s must name it: %v", flag, err)
		}
	}
	if _, err := parseRawArgs([]string{"--file", "/x.log", "--json"}); err == nil {
		t.Error("--json accepted under --file")
	}
	// a session id that merely LOOKS like the flag name is still an id
	opts, err := parseRawArgs([]string{"json"})
	if err != nil || opts.sessionID != "json" {
		t.Errorf("positional 'json' must stay a session id: %+v err=%v", opts, err)
	}
}

// ---- CLI over the real-session fixture ----

// TestRawRoundRequestByteExact drives `gopi session raw <id> <round> request` through
// the CLI's own resolution (session → paired raw log) and compares the printed
// bytes with the fixture file's own line.
func TestRawRoundRequestByteExact(t *testing.T) {
	id := statsFixtureSandbox(t)
	// Round 3 starts at line 801 (marker), its body is line 802.
	want := statsFixtureRawLine(t, 802) + "\n"

	got := captureStdoutStream(t, func() {
		printRawRound(resolveRawForSession(id), &rawOptions{sel: gopilog.HTTPRoundSelector{Detail: true, Round: 3}, part: rawPartRequest})
	})
	if got != want {
		t.Errorf("round 3 request is not byte-exact\n got: %.90q\nwant: %.90q", got, want)
	}
	if !strings.HasPrefix(got, `{"model":"deepseek-v4-flash"`) {
		t.Errorf("round 3 request does not start with the logged body: %.60q", got)
	}
}

// TestRawRoundResponseCoversTheWholeRound: `raw <id> N` prints the round's full
// line span, marker line first.
func TestRawRoundWholeSpan(t *testing.T) {
	id := statsFixtureSandbox(t)
	got := captureStdoutStream(t, func() {
		printRawRound(resolveRawForSession(id), &rawOptions{sel: gopilog.HTTPRoundSelector{Detail: true, Round: 2}})
	})
	// Round 2 spans lines 287..800 (514 lines, the last one the SSE blank
	// separator). Count the newlines: every printed line — an empty one too —
	// contributes exactly one.
	if n := strings.Count(got, "\n"); n != 514 {
		t.Fatalf("round 2 printed %d lines, want 514", n)
	}
	if !strings.HasPrefix(got, "→ REQUEST\n") {
		t.Errorf("round span must start with the request marker, got %.30q", got)
	}
	if !strings.HasPrefix(got[len("→ REQUEST\n"):], `{"model":`) {
		t.Errorf("second line must be the request body, got %.30q", got[len("→ REQUEST\n"):])
	}
}

// TestRawLinesSpan: `--lines A-B` prints exactly that span of the file.
func TestRawLinesSpan(t *testing.T) {
	id := statsFixtureSandbox(t)
	rawLog := resolveRawForSession(id)
	got := captureStdoutStream(t, func() { printRawSpan(rawLog, 1, 3) })
	want := statsFixtureRawLine(t, 1) + "\n" + statsFixtureRawLine(t, 2) + "\n" + statsFixtureRawLine(t, 3) + "\n"
	if got != want {
		t.Errorf("--lines 1-3 mismatch\n got: %.120q\nwant: %.120q", got, want)
	}
}

// TestRawRoundsIndexOverFixture: the index table matches the committed golden.
func TestRawRoundsIndexOverFixture(t *testing.T) {
	id := statsFixtureSandbox(t)
	rawLog := resolveRawForSession(id)
	got := captureStdoutStream(t, func() { printRawRoundsIndex(rawLog) })
	assertGolden(t, "raw-rounds-index.txt", got)
}

// TestRawSyspromptOverFixture: the system prompt as sent, no header.
func TestRawSyspromptOverFixture(t *testing.T) {
	id := statsFixtureSandbox(t)
	rawLog := resolveRawForSession(id)
	got := captureStdoutStream(t, func() { printRawSysprompt(rawLog) })
	if !strings.HasPrefix(got, "You are GoPi.") {
		t.Errorf("--sysprompt must print the prompt text only, got %.60q", got)
	}
	if strings.Contains(got, "SYSTEM PROMPT") || strings.Contains(got, "═══") {
		t.Errorf("--sysprompt must carry no header decoration:\n%.120s", got)
	}
}

// TestRawFileInput: `--file` reads a raw log directly, without a session.
func TestRawFileInput(t *testing.T) {
	statsFixtureSandbox(t) // only to prove --file does not need the session dir
	path := statsFixtureRawPath(t)
	got := captureStdoutStream(t, func() { printRawFile(path) })
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if got != string(want) {
		t.Errorf("--file output is not the file's bytes (%d vs %d bytes)", len(got), len(want))
	}
}

// TestRawRoundBoundMatchesIndex: the bound the CLI checks a round number
// against is the parsed round count, so a `gopi session raw <id> N` built from
// `gopi session raw <id> --rounds` (or `gopi session stats <id> --rounds`) always resolves.
func TestRawRoundBoundMatchesIndex(t *testing.T) {
	id := statsFixtureSandbox(t)
	rawLog := resolveRawForSession(id)
	got := captureStdoutStream(t, func() { printRawRoundsIndex(rawLog) })
	indexRows := strings.Count(got, "request + stream") + strings.Count(got, "request + response") +
		strings.Count(got, "HTTP-ERROR") + strings.Count(got, "probe ·") + strings.Count(got, "no response")
	if indexRows != 5 {
		t.Errorf("index rows = %d, want 5:\n%s", indexRows, got)
	}
}

// TestRawUserpromptOverFixture: --userprompt answers "what did the user ask?"
// straight from the paired raw log — the first request's user message verbatim,
// no header, and with the agent's own appended directives intact (the wire
// carries them inside the same user block).
func TestRawUserpromptOverFixture(t *testing.T) {
	id := statsFixtureSandbox(t)
	rawLog := resolveRawForSession(id)

	// The authoritative source: round 1's request body, parsed here so the
	// assertion cannot drift from what the view is defined to print.
	rounds, err := gopilog.ParseHTTPRounds(rawLog)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) == 0 {
		t.Fatal("fixture has no rounds")
	}
	p, err := gopilog.ParseFirstRequest(rawLog)
	if err != nil {
		t.Fatalf("ParseFirstRequest: %v", err)
	}
	want, ok := gopilog.FirstUserMessage(p)
	if !ok {
		t.Fatal("fixture first request has no user message")
	}

	got := captureStdoutStream(t, func() { printRawUserprompt(rawLog) })
	if got != want+"\n" {
		t.Errorf("--userprompt = %.120q, want the first user message %.120q", got, want)
	}
	if strings.Contains(got, "═══") || strings.Contains(got, "USER PROMPT") {
		t.Errorf("--userprompt must carry no header decoration:\n%.120s", got)
	}
	if rounds[0].Request == nil || !strings.Contains(got, want) {
		t.Errorf("--userprompt output is not part of round 1's request body")
	}
}

// ---- empty and missing raw logs (2026-09-25: "raw takes too long to return
// nothing") ----

// TestPrintRawFile_EmptyLogSaysSo pins the honesty of the empty case: 0 bytes
// printed with a 0 exit code is indistinguishable from a broken verb, so the
// verb must state the fact — the log exists and captured no HTTP byte.
func TestPrintRawFile_EmptyLogSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.raw.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var out string
	errOut := captureStderrStream(t, func() {
		out = captureStdout(t, func() { printRawFile(path) })
	})
	if out != "" {
		t.Errorf("stdout = %q, want empty (stdout is the byte stream)", out)
	}
	if !strings.Contains(errOut, "0 bytes") {
		t.Errorf("stderr must explain the empty log, got:\n%s", errOut)
	}
}

// TestMissingRawLogNotice distinguishes a session whose raw log was COLLECTED
// from one that never had one. The first must name the recorded path (the
// operator asked about this session); the second must stay silent so the
// ordinary resolution runs.
func TestMissingRawLogNotice(t *testing.T) {
	sessDir := t.TempDir()
	recorded := filepath.Join(sessDir, "gone.raw.log")
	sessPath := filepath.Join(sessDir, "session.jsonl")
	write := func(rawLogPath string) {
		line := `{"type":"session_start","sessionId":"aaaa1111-2222-4333-8444-555555555555"`
		if rawLogPath != "" {
			line += `,"rawLogPath":"` + rawLogPath + `"`
		}
		line += "}\n"
		if err := os.WriteFile(sessPath, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Recorded but gone: the notice names the missing file and the intact record.
	write(recorded)
	notice := missingRawLogNotice("aaaa1111-2222-4333-8444-555555555555", sessPath)
	if !strings.Contains(notice, recorded) {
		t.Errorf("notice must name the recorded raw log %s, got:\n%s", recorded, notice)
	}
	if !strings.Contains(notice, sessPath) {
		t.Errorf("notice must point at the intact session record, got:\n%s", notice)
	}

	// Never recorded: no notice — the caller resolves as usual.
	write("")
	if got := missingRawLogNotice("aaaa1111-2222-4333-8444-555555555555", sessPath); got != "" {
		t.Errorf("a session that recorded no raw log must not produce a notice, got:\n%s", got)
	}

	// Recorded and present: no notice either.
	if err := os.WriteFile(recorded, []byte("→ REQUEST\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(recorded)
	if got := missingRawLogNotice("aaaa1111-2222-4333-8444-555555555555", sessPath); got != "" {
		t.Errorf("a present raw log must not produce a notice, got:\n%s", got)
	}
}
