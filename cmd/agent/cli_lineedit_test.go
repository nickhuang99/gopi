package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
)

// TestChatCookedFallback_NonTTY is the A4 parity test: with stdin NOT a terminal
// (the testsuite's shape — a pipe), the chat input path must behave exactly as it
// did before the editor existed. It drives the real binary and asserts the
// observable contract: both piped turns become user messages, one answer each,
// one session, and a clean /quit exit.
//
// THE PARITY CLAIM IS ABOUT THE INPUT PATH, NOT ABOUT THE SESSION FILE'S SHAPE,
// and this fixture was red until it said so. Two product behaviours were added
// after it was written, and both are contract, not accident (epic 01a12be0):
//
//   - ONE TURN STAMP PER REQUEST. Every assembled request carries an appended
//     user row — `[system] <tick> — first request of this session.` — minted
//     and logged together by (*agent.Agent).turnStampLine
//     (internal/agent/clock.go; the suffixes are the named constants
//     turnStampFirstSuffix / turnStampGapSuffix there). So a two-turn run
//     writes FOUR user rows: two the operator typed, two the harness injected.
//     Pinned at the unit level by internal/agent/chat_turn_clock_test.go
//     (TestTurnStamp_FirstAndGap) and internal/agent/sessionbudget_everyround_test.go
//     (stamps == calls, i.e. exactly one per request).
//   - A SESSION SIDE FILE. With the prefetch knob at its default ON the run
//     leaves `memory-prefetch.jsonl` beside the record — one line per prefetch
//     event (internal/config.MemoryPrefetchRecordName, opened in
//     cmd/agent/chat_prefetch.go). It is an artifact of the SAME session, so the
//     session count stays 1 (testutil.LoadSession says so in as many words) and
//     the fixture must not read its presence as a leak.
//
// The parity claim this file exists for is therefore measured on the rows the
// OPERATOR owns: the piped lines must arrive verbatim and unwrapped, and the
// harness rows must be recognisable as the framework's own words rather than
// attributed to the person at the pipe. Which row is which is decided by
// sessionevent.IsHarnessInjected — the same predicate the production readers use
// (internal/gopilog, Rule 14), so the fixture cannot drift from them.
func TestChatCookedFallback_NonTTY(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "first piped answer."},
		testutil.Resp{Text: "second piped answer."},
	)
	defer llm.Close()

	stdin := "hello there\nwhat is two plus two\n/quit\n"
	stdout, stderr, code := runGopiMapped(t, []string{"chat"}, stdin,
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat exit = %d, want 0 (clean /quit over a pipe)\n--- stderr ---\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "first piped answer") || !strings.Contains(stdout, "second piped answer") {
		t.Errorf("stdout = %q, want both streamed answers", stdout)
	}
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("stderr = %q, want the clean-exit bye message", stderr)
	}
	// The cooked path must NOT have entered the editor: on a pipe there is no
	// terminal, so the editor banner must be absent and the prompt must be the
	// plain one the REPL has always printed.
	if strings.Contains(stderr, "line editor active") {
		t.Errorf("stderr = %q, want no editor banner on a non-TTY stdin", stderr)
	}
	if !strings.Contains(stderr, chatPrompt) {
		t.Errorf("stderr = %q, want the cooked prompt %q", stderr, chatPrompt)
	}

	// ONE session, and the sidecar the run legitimately leaves beside its record
	// does not make a second one (testutil.LoadSession's accounting: a session
	// DIRECTORY is one session however many artifacts of it sit in it).
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertEventOrder(t, "session_start", "user", "assistant", "user", "assistant", "session_end")
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"assistant":     2,
		"session_end":   1,
	})

	// The user rows SPLIT IN TWO, and that split is the contract this A4 test
	// guards: the rows the operator's pipe produced (one per piped line, verbatim)
	// and the harness's own turn stamps (one per assembled request). Splitting
	// them with the PRODUCTION predicate — not a local rule — is what keeps this
	// fixture from drifting from the readers (Rule 14).
	//
	// THE CENSUS READS AllUserContents, NOT UserContents: the latter is already
	// the OPERATOR-ONLY view (testutil filters the injections itself, through
	// this same predicate), so splitting it here could only ever leave `injected`
	// empty and compare 0 stamps against the record's requests — the read path
	// that made this fixture red while the product contract was intact. The ALL
	// view is the one that carries both halves; the filter below is what
	// separates them.
	//
	// testutil.Session ALSO exposes the two halves directly (UserContents /
	// HarnessUserContents), and the archived branch this fixture's rework came
	// from read them instead of filtering a union here. Master's own fix for the
	// same measurement (commit 222b42aa8) kept the unfiltered read on purpose:
	// the raw-census arm at the bottom asserts that this census is reading the
	// RECORD, and a view that filtered twice would silently satisfy every arm
	// above it. Both accessors and this loop agree because both call
	// sessionevent.IsHarnessInjected — the readers decide, this fixture follows.
	var operator, injected []string
	for _, u := range sess.AllUserContents() {
		if sessionevent.IsHarnessInjected(u) {
			injected = append(injected, u)
			continue
		}
		operator = append(operator, u)
	}
	if len(operator) != 2 || operator[0] != "hello there" || operator[1] != "what is two plus two" {
		t.Errorf("the piped turns did not survive the cooked reader verbatim: user messages = %v, want [hello there, what is two plus two]", operator)
	}
	// ONE STAMP PER REQUEST (clock.go's turnStampLine is the only mint, and it
	// logs what it sends). This is the same relation
	// internal/agent/sessionbudget_everyround_test.go pins on the wire (one stamp
	// per model call), stated here on the record: a stamp that stopped being
	// written, or a double one, fails both halves.
	//
	// THE REQUEST COUNT IS CHECKED NONZERO FIRST, so the relation can never be
	// satisfied by 0 == 0: a run that reached no endpoint has not measured the
	// stamp rule, and an unmeasured claim is not a passing one. (The "no stamp at
	// all" arm below then reports the missing mint as itself rather than as a
	// mismatch.)
	requests := sess.Counts()["request"]
	if requests == 0 {
		t.Fatalf("the record carries no model request — the run never reached the endpoint, so the stamp relation is unmeasured")
	}
	if len(injected) != requests {
		t.Errorf("harness turn stamps = %d, model requests = %d — want exactly one stamp per request (stamps: %v)", len(injected), requests, injected)
	}
	if len(injected) == 0 {
		t.Errorf("the run wrote no turn stamp at all — every request carries one (internal/agent/clock.go)")
	}
	// WHY THE CENSUS READS AllUserContents IS ITSELF ASSERTED — the flaw that
	// made this fixture report "0 stamps" against a record that held 2 was a
	// read path that filtered twice (UserContents is ALREADY the operator-only
	// view), and that flaw is invisible to the arms above: they would print the
	// same "0 stamps" whether the run wrote none or the census threw them away.
	// Counts stays the RAW census (testutil.Session.Counts), so the record's own
	// user-row total is the arbiter: a census that sees fewer rows than the
	// record holds is filtering twice, not measuring. Red proof: restore
	// `sess.UserContents()` at the loop above and this arm fires (raw 4 ≠ 2).
	if raw := sess.Counts()["user"]; raw != len(operator)+len(injected) {
		t.Errorf("the census read %d user row(s) (operator %d + stamps %d) but the record holds %d — a view that sees fewer rows than the record is a second filter, not a measurement; use AllUserContents (internal/testutil/session.go)",
			len(operator)+len(injected), len(operator), len(injected), raw)
	}

	// THE PREFETCH SIDECAR: with the knob at its default (config.memoryPrefetchDefault
	// is ON) a chat run opens its record in the session directory at startup, so
	// this run leaves one beside the record. Read through the writer's own
	// constant (Rule 1/13 — never retyped here), and present rather than
	// forbidden: the OFF path is pinned by cmd/agent/chat_prefetch_test.go
	// (TestPrefetchOffWritesNoRecord), which is where that half belongs.
	side := filepath.Join(filepath.Dir(sess.Path), config.MemoryPrefetchRecordName)
	if _, err := os.Stat(side); err != nil {
		t.Errorf("the run left no prefetch sidecar at %s (%v) — the side file is an artifact of THIS session and belongs beside its record", side, err)
	}
}

// TestChatCookedFallback_EditorNotEnteredWithoutTTY pins the decision the REPL
// makes once, at startup: a non-terminal stdin must select the cooked reader. The
// test is on the constructor, so a future refactor that "helpfully" enters raw
// mode on a pipe fails here rather than corrupting a scripted invocation.
func TestChatCookedFallback_EditorNotEnteredWithoutTTY(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		// deliberately ignored: test cleanup of pipe ends; a close error is
		// meaningless here.
		_ = pr.Close()
		// deliberately ignored: as above.
		_ = pw.Close()
	}()
	in, cleanup := newChatInput(pr, os.Stderr, chatPrompt, false, false)
	defer cleanup()
	if got := in.Mode(); got != chatInputCooked {
		t.Fatalf("mode on a pipe = %q, want %q (raw mode must never be entered off a terminal)", got, chatInputCooked)
	}
	// A regular file is not a terminal either.
	f, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer func() {
		// deliberately ignored: test cleanup of an fd the test owns.
		_ = f.Close()
	}()
	fin, fcleanup := newChatInput(f, os.Stderr, chatPrompt, false, false)
	defer fcleanup()
	if got := fin.Mode(); got != chatInputCooked {
		t.Fatalf("mode on a regular file = %q, want %q", got, chatInputCooked)
	}
}

// TestChatCookedFallback_NoEditorFlag pins the explicit escape hatch: --no-editor
// selects the cooked reader even when stdin IS a terminal, which is the operator's
// way out if an editor bug ever makes a terminal unusable.
func TestChatCookedFallback_NoEditorFlag(t *testing.T) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no controlling terminal on this host: the --no-editor flag needs a real TTY to be exercised")
	}
	defer func() {
		// deliberately ignored: test cleanup.
		_ = tty.Close()
	}()
	in, cleanup := newChatInput(tty, os.Stderr, chatPrompt, true, false)
	defer cleanup()
	if got := in.Mode(); got != chatInputCooked {
		t.Fatalf("mode with --no-editor = %q, want %q", got, chatInputCooked)
	}
}

// TestChatCookedFallback_ReaderParity asserts the cooked reader returns exactly
// the lines the pre-editor code returned, including the line WITHOUT a trailing
// newline at end of input: the old feeder checked the error before using the line,
// so a trailing partial line was dropped, and dropping it is the behavior the
// cooked path must reproduce byte for byte.
func TestChatCookedFallback_ReaderParity(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"two lines", "a\nb\n", []string{"a\n", "b\n"}},
		{"trailing partial line is dropped", "full\npartial", []string{"full\n"}},
		{"empty input", "", nil},
		{"blank lines are lines", "\n\n", []string{"\n", "\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stdin")
			if err := os.WriteFile(path, []byte(c.input), 0o600); err != nil {
				t.Fatalf("write stdin fixture: %v", err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open stdin fixture: %v", err)
			}
			defer func() {
				// deliberately ignored: test cleanup of an fd the test owns.
				_ = f.Close()
			}()
			in, cleanup := newChatInput(f, os.Stderr, chatPrompt, false, false)
			defer cleanup()
			var got []string
			for i := 0; i < chatInputRounds; i++ {
				line, ok, _, err := in.ReadLine()
				if err != nil {
					t.Fatalf("ReadLine: %v", err)
				}
				if !ok {
					break
				}
				got = append(got, line)
			}
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("cooked lines = %q, want %q", got, c.want)
			}
		})
	}
}

// TestChatNoEditorFlagIsAccepted asserts the flag is consumed before the seed
// positional is read: `gopi chat --no-editor -h` must print usage and exit 0
// (not try to resolve "--no-editor" as a session id).
func TestChatNoEditorFlagIsAccepted(t *testing.T) {
	if code := runChat([]string{"--no-editor", "-h"}); code != 0 {
		t.Fatalf("runChat([--no-editor -h]) = %d, want 0", code)
	}
}
