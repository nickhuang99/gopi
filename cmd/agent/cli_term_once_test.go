package main

// cli_term_once_test.go — the contract of `gopi cli "prompt" --once`: the one
// non-interactive turn.
//
// Five gates, each aimed at a way this face can be quietly wrong:
//
//  1. TestTermOnceFlagSpellings — the four spellings the help advertises must
//     all resolve, and the three ways a caller can get the argv wrong must
//     REFUSE rather than half-work (a prompt lost to a flag is a silent wrong
//     answer, the worst failure this face can have).
//
//  2. TestTermOnceHelpContract (+ its red proof) — the `gopi cli -h` page must
//     name BOTH spellings and the purpose. The red proof applies the predicate
//     to the page that actually shipped before this face existed
//     (testdata/term_help_prefix.txt — `gopi cli -h` as the operator saw it),
//     because a hand-typed sample proves only that the check is awake.
//
//  3. TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer — the turn IS the
//     engine's turn: the model request carries the chat system prompt and the
//     caller's words, stdout receives the answer and nothing else, and fd 3
//     receives the engine's own frames.
//
//  4. TestTermOnceToolTurnIsObservableOutsideTheAnswer — with --tools, a
//     tool-calling turn emits its `event: tool` frames on fd 3 and a line on
//     stderr, while stdout stays the answer alone.
//
//  5. TestTermOnceHonestExitAndNoResidue — a failing turn and a text-less
//     turn both exit non-zero with a reason, and NO listener is left behind
//     (measured on /proc/net/tcp*, not asserted from the code's intent) — with
//     the rows ATTRIBUTED to the sockets THIS process tree holds, because the
//     raw table is host-wide and a sibling suite's listener is not our residue.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// --- the residue measurement's named facts (Rule 1/13) ---------------------
//
// Gate 5 asks one question of the kernel: did this run leave a listening
// socket behind? The channel is /proc/net/tcp* — correct — but a raw row there
// carries NO owner, so the first version of this measurement diffed the
// HOST-WIDE table and called any newly appeared row this run's residue. That
// false-positived on 2026-10-31 (the row 0100007F:8CFB = 127.0.0.1:36091 was a
// sibling `go test` suite's httptest server, with three sibling suites live).
// The missing ingredient was ATTRIBUTION, not a different channel: a socket
// inode in /proc/net/tcp is the same number that appears in the
// `socket:[<inode>]` symlink of /proc/<pid>/fd/<n>, so "ours" is decidable by
// reading the kernel's own descriptor table for this process and its
// descendants. The rows are then keyed BY INODE, never by address — an
// ephemeral port recycled by an unrelated listener must not alias ours.
const (
	// termOnceProcRootDir is the Linux procfs mount point the residue
	// measurement reads — named, never spelled at a call site.
	termOnceProcRootDir = "/proc"

	// termOnceFdDirName is the per-process descriptor directory inside procfs:
	// /proc/<pid>/fd/<n> is a symlink to what descriptor n is open on.
	termOnceFdDirName = "fd"

	// termOnceTaskDirName is the per-process thread directory inside procfs.
	termOnceTaskDirName = "task"

	// termOnceChildrenFileName is the per-thread file procfs exposes a thread's
	// children in (/proc/<pid>/task/<tid>/children). Reading it is EXACT; a
	// /proc/*/stat ppid scan would miss a child re-parented to a subreaper and
	// would race the table.
	termOnceChildrenFileName = "children"

	// termOnceSocketLinkPrefix / termOnceSocketLinkSuffix bracket the inode in a
	// socket descriptor's procfs symlink target: socket:[<inode>].
	termOnceSocketLinkPrefix = "socket:["
	termOnceSocketLinkSuffix = "]"

	// termOnceListenStateHex is the `st` column value that means TCP_LISTEN in
	// /proc/net/tcp*.
	termOnceListenStateHex = "0A"

	// termOnceStateColumn / termOnceInodeColumn are the zero-based column indices
	// of a /proc/net/tcp* row: `st` (the connection state) and the socket inode.
	// The inode's value is the SAME number the descriptor symlink carries — that
	// join is what makes attribution possible at all.
	termOnceStateColumn = 3
	termOnceInodeColumn = 9

	// termOnceOwnListenerNet / termOnceOwnListenerAddr are the listener gate 5
	// opens in THIS process to prove the measurement is awake: an ephemeral port
	// on the loopback, closed before the test ends.
	termOnceOwnListenerNet  = "tcp"
	termOnceOwnListenerAddr = "127.0.0.1:0"

	// termOnceTestForeignInode is the inode the injected foreign-listener arm
	// uses to stand for another process's listener. The arm VERIFIES this inode
	// is absent from this process tree before relying on it, so the "foreign"
	// part of that arm is measured, never assumed.
	termOnceTestForeignInode = "2147483647"

	// termOnceForeignRowKey is the reported key of that injected row — the shape
	// the real false positive was reported in (0100007F:8CFB = 127.0.0.1:36091,
	// the sibling suite's httptest listener as /proc renders it).
	termOnceForeignRowKey = termOnceTCPTableV4 + " 0100007F:8CFB"
)

// termOnceTCPTableV4 / termOnceTCPTableV6 are the two kernel tables that carry
// TCP rows, one per address family — a listener appears only in the table that
// matches its family, so both are read.
const (
	termOnceTCPTableV4 = termOnceProcRootDir + "/net/tcp"
	termOnceTCPTableV6 = termOnceProcRootDir + "/net/tcp6"
)

// termOnceTCPTables is the list the table reader walks, in report order.
var termOnceTCPTables = []string{termOnceTCPTableV4, termOnceTCPTableV6}

// termOnceListenerRow is ONE TCP_LISTEN row: what to report (Key) and what to
// attribute by (Inode, the kernel socket inode in decimal, exactly as the table
// prints it).

// termOnceHarness is one in-process `--once` run: the captured streams plus
// the model requests the turn actually made.
type termOnceHarness struct {
	answer *strings.Builder
	diag   *strings.Builder
	// events reads the fd-3 file back at ACCESS time: the frames are written
	// by the run under test, and a snapshot taken at setup would always be
	// empty (which would make the fd-3 gates green-blind).
	events   func() string
	requests func() [][]byte
}

// termOnceTestEnv wires a mock model server, a stub agent pointing at it, and
// the three captured streams. It returns the harness and the mock's handler
// hook (called per model request, so a test can answer round 1 with a tool_use
// and round 2 with text).
func termOnceTestEnv(t *testing.T, respond func(round int, w http.ResponseWriter)) *termOnceHarness {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	var (
		mu   = make(chan struct{}, 1)
		seen [][]byte
		n    int
	)
	mu <- struct{}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; a short read is acceptable
		<-mu
		seen = append(seen, body)
		n++
		round := n
		mu <- struct{}{}
		w.Header().Set("Content-Type", "text/event-stream")
		respond(round, w)
	}))
	t.Cleanup(srv.Close)

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   webTestTurnModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true

	answer, diag := &strings.Builder{}, &strings.Builder{}
	var prevFactory func() (*agent.Agent, error)
	prevFactory, termOnceAgentFactory = termOnceAgentFactory, func() (*agent.Agent, error) { return ag, nil }
	prevAnswer, prevDiag, prevFD := termOnceAnswerOut, termOnceDiagOut, termOnceStream3FD
	termOnceAnswerOut, termOnceDiagOut = answer, diag

	// fd 3 is a real regular file this test reads back: the borrowed-descriptor
	// path (os.NewFile + Detach) is exercised for real, not simulated.
	evPath := filepath.Join(t.TempDir(), "events.sse")
	evFile, err := os.OpenFile(evPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open the events file: %v", err)
	}
	termOnceStream3FD = int(evFile.Fd())
	t.Cleanup(func() {
		termOnceAgentFactory, termOnceAnswerOut, termOnceDiagOut, termOnceStream3FD = prevFactory, prevAnswer, prevDiag, prevFD
		evFile.Close() //stylegate:ignore test fixture teardown of a file this test opened
	})

	return &termOnceHarness{
		answer: answer, diag: diag,
		events:   func() string { return mustReadFile(t, evPath) },
		requests: func() [][]byte { <-mu; out := append([][]byte{}, seen...); mu <- struct{}{}; return out },
	}
}

// mustReadFile reads a fixture file, or fails the test.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// --- 1. the argv contract -------------------------------------------------

// TestTermOnceFlagSpellings pins every accepted spelling and every refusal.
func TestTermOnceFlagSpellings(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"flag after the prompt (the documented shape)", []string{"say OK", termOnceFlag}, "say OK"},
		{"flag before the prompt", []string{termOnceFlag, "say OK"}, "say OK"},
		{"short spelling", []string{termOnceShortFlag, "say OK"}, "say OK"},
		{"named prompt flag", []string{termOncePromptFlag, "say OK", termOnceFlag}, "say OK"},
		{"named prompt flag joined", []string{termOncePromptFlag + "=say OK", termOnceFlag}, "say OK"},
		{"tools rides along", []string{"say OK", termOnceFlag, termToolsFlag}, "say OK"},
		{"the address flag's value is not the prompt", []string{termOnceFlag, webAddrFlag, "127.0.0.1:0", "say OK"}, "say OK"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := termOnceParseArgs(tc.argv)
			if err != nil {
				t.Fatalf("termOnceParseArgs(%q): %v", tc.argv, err)
			}
			if opts.prompt != tc.want {
				t.Errorf("prompt = %q, want %q", opts.prompt, tc.want)
			}
			if !termOnceRequested(tc.argv) {
				t.Errorf("termOnceRequested(%q) = false — the dispatch would not route this argv to the once face", tc.argv)
			}
		})
	}

	// Tools is the SAME switch the interactive carrier takes, resolved by the
	// SAME parser: `--tools` must mean enabled here too.
	opts, err := termOnceParseArgs([]string{"hi", termOnceFlag, termToolsFlag})
	if err != nil {
		t.Fatalf("parse with %s: %v", termToolsFlag, err)
	}
	if !opts.tools {
		t.Error("--tools did not enable tool turns — the two carriers would disagree about one switch")
	}

	// Refusals: a usage error, never a half-run.
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"two prompts", []string{"one", "two", termOnceFlag}},
		{"unknown flag", []string{"hi", termOnceFlag, "--nope"}},
		{"prompt flag without a value", []string{termOnceFlag, termOncePromptFlag}},
	} {
		if _, err := termOnceParseArgs(tc.argv); err == nil {
			t.Errorf("%s: termOnceParseArgs(%q) accepted the argv — it must refuse (a lost prompt is a silent wrong answer)", tc.name, tc.argv)
		}
	}

	// A MISSING PROMPT is refused at RUN time with the usage status (the parse
	// cannot know whether the browser carrier was meant, so the once face
	// itself must refuse rather than run an empty turn).
	if code := runTermOnce([]string{termOnceFlag}); code != termOnceExitUsage {
		t.Errorf("`gopi cli --once` with no prompt exited %d, want %d (a usage error)", code, termOnceExitUsage)
	}
	if opts, err := termOnceParseArgs([]string{termOnceFlag}); err == nil && strings.TrimSpace(opts.prompt) != "" {
		t.Errorf("a prompt appeared from nowhere: %q", opts.prompt)
	}

	// The dispatch must NOT route an ordinary interactive invocation.
	for _, argv := range [][]string{{}, {"open"}, {termToolsFlag}, {termOncePromptFlag}} {
		if termOnceRequested(argv) {
			t.Errorf("termOnceRequested(%q) = true — `gopi cli` would stop opening the browser carrier", argv)
		}
	}
}

// --- 2. the help contract, and its red proof ------------------------------

// termOnceHelpOffenses is the predicate: it returns every reason a help page
// (either the group's or the once face's own) fails to name this capability.
// An empty result is a pass.
func termOnceHelpOffenses(text string) []string {
	var out []string
	for _, want := range []struct{ spelling, why string }{
		{termOnceFlag, "the canonical spelling"},
		{termOnceShortFlag, "the short spelling"},
	} {
		if !strings.Contains(text, want.spelling) {
			out = append(out, "does not name "+want.spelling+" ("+want.why+")")
		}
	}
	// The PURPOSE, not just the spelling: a reader of `gopi cli -h` must learn
	// that one turn can be driven without the browser.
	if !strings.Contains(text, termOnceFlag) || !strings.Contains(strings.ToLower(text), "one turn") {
		out = append(out, "does not state the purpose (ONE turn, non-interactively)")
	}
	return out
}

// TestTermOnceHelpContract pins both help pages: the group's (`gopi cli -h`,
// where a reader looks first) and the face's own (`gopi cli --once -h`).
func TestTermOnceHelpContract(t *testing.T) {
	for _, page := range []struct{ name, text string }{
		{"termUsage (`gopi cli -h`)", termUsage},
		{"termOnceUsage (`gopi cli --once -h`)", termOnceUsage},
	} {
		for _, offense := range termOnceHelpOffenses(page.text) {
			t.Errorf("%s %s", page.name, offense)
		}
	}
	// The group page must keep its own capability-first shape: it still opens
	// with its invocations and still ends with a Notes tail (the page was
	// edited, not restructured).
	if offense := hcfCapabilityFirstOffenses(termUsage); len(offense) != 0 {
		t.Errorf("the `gopi cli -h` page lost its capability-first shape: %v", offense)
	}
	// The once page must carry the exit statuses it documents — rendered from
	// the SAME constants the code returns, so the page cannot lie.
	for _, code := range []int{termOnceExitOK, termOnceExitTurnFailed, termOnceExitUsage} {
		if !strings.Contains(termOnceUsage, strconv.Itoa(code)) {
			t.Errorf("the once usage page does not document exit status %d", code)
		}
	}
}

// TestTermOnceHelpPredicateIsNotBlind is the RED PROOF: the predicate is
// applied to the page that ACTUALLY SHIPPED before this face existed, and must
// flag it. Both spellings and the purpose were absent from it, which is exactly
// the discoverability defect this epic fixes.
func TestTermOnceHelpPredicateIsNotBlind(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "term_help_prefix.txt"))
	if err != nil {
		t.Fatalf("read the shipped pre-epic `gopi cli -h` page: %v", err)
	}
	shipped := string(b)
	if !strings.HasPrefix(shipped, "GoPi cli —") || len(shipped) < 1000 {
		t.Fatalf("testdata/term_help_prefix.txt is not the shipped help page (it must open with the title and be a full usage block), got %d bytes", len(shipped))
	}
	if !termOnceRequested([]string{termOnceFlag}) {
		t.Fatal("the spelling the red proof searches for is not the one the dispatch accepts — the fixture and the predicate have drifted")
	}
	// The fixture must genuinely LACK both spellings, or the red proof proves
	// nothing (a fixture that already contains them would pass the predicate).
	for _, spelling := range []string{termOnceFlag, termOnceShortFlag} {
		if strings.Contains(shipped, spelling) {
			t.Fatalf("the pre-epic fixture already carries %q — it is not the page this epic reports", spelling)
		}
	}
	if offenses := termOnceHelpOffenses(shipped); len(offenses) == 0 {
		t.Fatal("the predicate failed to flag the shipped page — a green sweep would be blind")
	}
	// …and the other direction: a page in the shape this face asks for passes
	// cleanly, or the gate forbids the very structure it exists for.
	good := "GoPi cli — chat in a text browser.\n\n  gopi cli \"prompt\" " + termOnceFlag + "   one turn, then exit\n  gopi cli " + termOnceShortFlag + " \"prompt\"\n"
	if offenses := termOnceHelpOffenses(good); len(offenses) != 0 {
		t.Errorf("the predicate rejected a page that names both spellings and the purpose: %v", offenses)
	}
}

// --- 3. the turn IS the engine's turn -------------------------------------

// TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer is gate 3. It asserts on
// what the ENGINE RECEIVED (the model request) and on what the caller SAW (the
// three streams) — the two halves of "this is the same turn, not a headless
// mode".
func TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer(t *testing.T) {
	h := termOnceTestEnv(t, func(round int, w http.ResponseWriter) { textSSE(w, "OK there") })
	// The reply-language knob is a HOST-level channel this assertion must not
	// inherit (Rule 17): with it set, internal/config prepends the directive to
	// the whole system assembly and the head of the slot is no longer the prompt
	// (measured on a host whose settings.json carried output_language — the same
	// turn passed with the channel cleared and failed with it set). Clearing both
	// channels makes this gate a statement about the PROMPT rather than about the
	// machine it happens to run on.
	outputLanguageUnset(t)

	code := runTermOnce([]string{"say OK", termOnceFlag})
	if code != termOnceExitOK {
		t.Fatalf("runTermOnce = %d, want %d (stderr: %s)", code, termOnceExitOK, h.diag.String())
	}
	if got := h.answer.String(); got != "OK there" {
		t.Errorf("stdout = %q, want the model's answer and nothing else", got)
	}
	// What the engine received: the caller's words, under the CHAT system
	// prompt (the same one the browser carrier's turns carry).
	reqs := h.requests()
	if len(reqs) != 1 {
		t.Fatalf("the turn made %d model requests, want 1", len(reqs))
	}
	texts := turnContextTexts(t, reqs[0])
	if len(texts) == 0 || texts[len(texts)-1] != "say OK" {
		t.Errorf("the model never received the caller's prompt: %q", texts)
	}
	// The turn IS the chat turn: the prompt slot carries the chat system prompt
	// and the CHAT-surface block, and it is NOT the task assembly. Both
	// directions are asserted, because "the chat prompt arrived" alone would be
	// satisfied by a prompt that carried the task routing as well — and the
	// head of the slot is NOT a safe place to look: the operator's reply-language
	// directive is prepended to the whole assembly by
	// internal/config.OutputLanguageDirective (measured: this arm failed on a
	// host whose GOPI_OUTPUT_LANGUAGE / settings output_language was set, with
	// the chat prompt one block behind the directive).
	got := turnSystemPrompt(t, reqs[0])
	if !strings.Contains(got, chatSystemPromptMarker) {
		t.Errorf("the request's system prompt carries no %q — this is not the same turn the interactive carrier runs:\n%q", chatSystemPromptMarker, got)
	}
	if !strings.Contains(got, chatSurfacePromptMarker) {
		t.Errorf("the request's system prompt carries no %q block — the chat surface's own block is what only this path can emit:\n%q", chatSurfacePromptMarker, got)
	}
	if strings.Contains(got, taskSystemPromptMarker) {
		t.Errorf("the request's system prompt carries the TASK prompt's opening %q — this is the task assembly, not the chat turn:\n%q", taskSystemPromptMarker, got)
	}
	// No tools without --tools (the switch must mean what it says).
	if strings.Contains(string(reqs[0]), `"tools":[{"`) {
		t.Error("a tool-free turn shipped a tool set")
	}

	// The machine-readable channel: the engine's own frames, and the answer
	// text is NOT among them as raw prose.
	events := h.events()
	for _, want := range []string{termOnceSSEEventPrefix + webEventDelta, termOnceSSEEventPrefix + webEventDone} {
		if !strings.Contains(events, want) {
			t.Errorf("fd 3 carries no %q frame:\n%s", want, events)
		}
	}
	if strings.Contains(h.answer.String(), termOnceSSEEventPrefix) {
		t.Error("an engine frame leaked onto stdout — the answer stream must carry text only")
	}
	if strings.Contains(h.diag.String(), "OK there") {
		t.Error("the answer text was duplicated onto stderr — a caller must be able to read the answer from stdout alone")
	}
}

// --- 4. tool activity is observable OUTSIDE the answer ---------------------

// TestTermOnceToolTurnIsObservableOutsideTheAnswer is gate 4: the model's
// decision to CALL a tool must be visible to the caller, and it must not be
// welded into the answer.
func TestTermOnceToolTurnIsObservableOutsideTheAnswer(t *testing.T) {
	h := termOnceTestEnv(t, func(round int, w http.ResponseWriter) {
		if round == 1 {
			toolUseSSE(w, "call_once_1", "read", `{"path":"/etc/hostname"}`)
			return
		}
		textSSE(w, "the hostname is peer1")
	})

	code := runTermOnce([]string{"read /etc/hostname", termOnceFlag, termToolsFlag})
	if code != termOnceExitOK {
		t.Fatalf("the tool turn exited %d (stderr: %s)", code, h.diag.String())
	}
	events := h.events()
	if !strings.Contains(events, termOnceSSEEventPrefix+webEventTool) {
		t.Fatalf("a tool-calling turn emitted no tool frame on fd 3:\n%s", events)
	}
	var ev struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}
	for _, line := range strings.Split(events, "\n") {
		if data, ok := strings.CutPrefix(line, termOnceSSEDataPrefix); ok {
			if json.Unmarshal([]byte(data), &ev) == nil && ev.Kind == string(agent.ToolEventKindCall) {
				if ev.Name != "read" {
					t.Errorf("the tool call frame names %q, want %q", ev.Name, "read")
				}
			}
		}
	}
	// The human channel names the same call, on stderr.
	if !strings.Contains(h.diag.String(), "read") {
		t.Errorf("stderr does not report the tool activity:\n%s", h.diag.String())
	}
	// stdout stays the answer alone: no frame, no tool line.
	if got := h.answer.String(); got != "the hostname is peer1" {
		t.Errorf("stdout = %q, want the answer text only (tool activity must not ride in the answer stream)", got)
	}
	// The tool-enabled turn really shipped tools to the model.
	reqs := h.requests()
	if len(reqs) < 2 {
		t.Fatalf("the tool turn made %d model requests, want at least 2 (tool round + answer round)", len(reqs))
	}
	if !strings.Contains(string(reqs[0]), `"tools"`) {
		t.Error("--tools did not put a tool set on the wire — the switch was accepted and then ignored")
	}
}

// --- 5. honest exit status, and no residue ---------------------------------

// TestTermOnceHonestExitAndNoResidue is gate 5: a failed turn and a text-less
// turn both exit non-zero WITH a reason, and the run leaves no listener
// behind — measured on the kernel's own socket table, not inferred.
func TestTermOnceHonestExitAndNoResidue(t *testing.T) {
	// The snapshot is a first-class measurement, not a raw table dump: it is
	// already restricted to listeners THIS process tree owns, so the comparison
	// at the end and the arm that replays the reported false positive speak
	// about the same thing.
	before, _ := termOnceListeners(t)

	t.Run("a model failure is a non-zero exit with a reason", func(t *testing.T) {
		h := termOnceTestEnv(t, func(round int, w http.ResponseWriter) {
			http.Error(w, `{"error":{"message":"upstream is down"}}`, http.StatusInternalServerError)
		})
		code := runTermOnce([]string{"say OK", termOnceFlag})
		if code == termOnceExitOK {
			t.Fatalf("a failed turn exited 0 — an honest status is the whole point (stdout: %q)", h.answer.String())
		}
		if strings.TrimSpace(h.diag.String()) == "" {
			t.Error("a failed turn said nothing on stderr — the caller cannot learn why")
		}
	})

	t.Run("a text-less turn is a failure, not an empty success", func(t *testing.T) {
		h := termOnceTestEnv(t, func(round int, w http.ResponseWriter) {
			// A completed round that produced no text at all.
			writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":1,"output_tokens":0}}}`)
			writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
		})
		code := runTermOnce([]string{"say OK", termOnceFlag})
		if code == termOnceExitOK {
			t.Fatalf("a text-less turn exited 0 — `completed` must mean there is an answer (stdout: %q)", h.answer.String())
		}
		if !strings.Contains(h.diag.String(), "no text") && !strings.Contains(h.diag.String(), "without producing") {
			t.Errorf("the reason does not say the turn produced no text:\n%s", h.diag.String())
		}
	})

	// The residue claim. Its deterministic arms run FIRST: "did the gate go
	// blind" and "was another process's row ever attributed to us" are decided
	// from a decision table this test owns, not from a hope about who else is
	// on the host.
	if !t.Run("the measurement is awake and attributed", func(t *testing.T) {
		termOnceResidueArms(t)
	}) {
		return
	}
	after, _ := termOnceListeners(t)
	if left := termOnceResidue(before, after, termOnceOwnedNow()); len(left) > 0 {
		for _, row := range left {
			t.Errorf("the run left a listening socket behind: %s", row)
		}
	}
}

// termOnceResidueArms holds the three properties the residue measurement must
// have, all decided from data this test OWNS rather than from whatever else
// happens to be running on the host (Rule 27: no synthetic load — an injected
// row plus one real descriptor of our own is enough to decide both directions).
func termOnceResidueArms(t *testing.T) {
	t.Helper()

	t.Run("a listener THIS process opens is measured, and the gate flags it", func(t *testing.T) {
		beforeOpen, _ := termOnceListeners(t)
		ln, err := net.Listen(termOnceOwnListenerNet, termOnceOwnListenerAddr)
		if err != nil {
			t.Fatalf("open a loopback listener to prove the measurement is awake: %v", err)
		}
		defer ln.Close() //stylegate:ignore test teardown of the probe listener this test opened

		rows := termOnceReadListenerTable()
		afterOpen, oursNow := termOnceListeners(t)
		inode := termOnceOwnedListenerInode(afterOpen, beforeOpen, rows)
		if inode == "" {
			t.Fatalf("the listener this test just opened was not attributed to this process — the measurement is blind (owned rows: %v)", afterOpen)
		}
		if left := termOnceResidue(beforeOpen, afterOpen, oursNow); len(left) == 0 {
			t.Errorf("a listener this process opened AFTER the snapshot was not flagged — the gate has gone blind")
		}
	})

	t.Run("a listener owned by no process in our tree is not attributed to us", func(t *testing.T) {
		// The reported false positive, replayed: the snapshot the run took
		// before `after` does not know the row; an unrelated process's listener
		// is what `after` sees. Keyed by inode and attributed, it is NOT ours.
		snapshot := map[string][]string{}
		landed := map[string][]string{termOnceTestForeignInode: {termOnceForeignRowKey}}
		_, oursNow := termOnceListeners(t)
		if oursNow[termOnceTestForeignInode] {
			t.Fatalf("inode %s belongs to this process tree — this arm could not tell a foreign row from ours", termOnceTestForeignInode)
		}
		if left := termOnceResidue(snapshot, landed, oursNow); len(left) != 0 {
			t.Errorf("a listener no process in our tree owns was reported as our residue: %v (this is the 2026-10-31 false positive)", left)
		}
		// …and the SAME decision with the attribution flipped: NOT a weakened
		// assertion — the only thing that changes is who owns the row, so this
		// arm proves the first half passed because of the attribution and not
		// because the decision stopped reporting anything.
		owned := map[string]bool{termOnceTestForeignInode: true}
		if left := termOnceResidue(snapshot, landed, owned); len(left) == 0 {
			t.Errorf("a listener WE own, newly appeared, was not flagged after an owned snapshot — the gate cannot see its own residue")
		}
	})

	t.Run("the attribution join reads the kernel, not the table", func(t *testing.T) {
		// The table says WHAT is listening; our descriptors decide WHOSE. A row
		// the table reports for an inode this tree holds no descriptor on is not
		// ours — pinned here so a future edit cannot quietly revert the join to
		// "whatever the table says".
		table := map[string][]string{termOnceTestForeignInode: {termOnceForeignRowKey}}
		_, oursNow := termOnceListeners(t)
		if oursNow[termOnceTestForeignInode] {
			t.Fatalf("inode %s is owned by this process — pick another fixture inode", termOnceTestForeignInode)
		}
		left := termOnceResidue(map[string][]string{}, table, oursNow)
		if len(left) != 0 {
			t.Errorf("a table row no descriptor in this tree names was attributed to us: %v", left)
		}
	})
}

// termOnceListeners is the residue measurement: from ONE read, both the
// listening rows keyed by socket inode (the snapshot the diff is taken over)
// and the set of socket inodes THIS process and its descendants own (the
// attribution). Reading them together is deliberate — a table read and a
// descriptor scan taken at different moments can disagree about a socket opened
// or closed in between, and that disagreement would read as residue.
//
// It SKIPS when /proc/net/tcp* cannot be read here (non-Linux, or a restricted
// mount): the pre-existing behaviour, kept — a measurement that cannot be taken
// must say so rather than quietly report nothing (Rule 15).
func termOnceListeners(t *testing.T) (map[string][]string, map[string]bool) {
	t.Helper()
	table := termOnceReadListenerTable()
	if len(table) == 0 {
		t.Skip(termOnceTCPTableV4 + "* is unreadable here — the residue measurement cannot be taken")
	}
	return table, termOnceOwnedNow()
}

// termOnceResidue is the pure decision behind the residue claim: the owned rows
// that appeared since the snapshot, keyed by inode. `ours` is the attribution
// set, so a row no process in this tree owns can never be reported — that is
// the whole fix, stated once and testable without a running host.
func termOnceResidue(before, after map[string][]string, ours map[string]bool) []string {
	var left []string
	for inode, row := range after {
		if before[inode] != nil {
			continue
		}
		if !ours[inode] {
			continue
		}
		left = append(left, row[0])
	}
	sort.Strings(left)
	return left
}

// termOnceReadListenerTable reads every TCP_LISTEN row of the kernel's v4 and
// v6 tables, keyed by socket inode. /proc/net/tcp* is read directly: a
// net.Listen probe would itself change the thing it measures.
func termOnceReadListenerTable() map[string][]string {
	out := map[string][]string{}
	for _, name := range termOnceTCPTables {
		data, err := os.ReadFile(name)
		if err != nil {
			continue //stylegate:ignore a missing /proc entry (non-Linux or restricted) simply contributes no rows
		}
		for i, line := range strings.Split(string(data), "\n") {
			if i == 0 || strings.TrimSpace(line) == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) <= termOnceInodeColumn || fields[termOnceStateColumn] != termOnceListenStateHex {
				continue
			}
			inode := fields[termOnceInodeColumn]
			out[inode] = append(out[inode], name+" "+fields[1])
		}
	}
	return out
}

// termOnceRootPID is the process the attribution starts from: this one. The
// residue question is about THIS run, so a socket owned by an unrelated process
// — sibling suite, editor, ssh client — is not an answer to it.
func termOnceRootPID() int { return os.Getpid() }

// termOnceProcessTree returns the root pid and every descendant of it, walked
// through /proc/<pid>/task/<tid>/children (exact: a child re-parented to a
// subreaper still appears in its thread's children file, while a ppid scan
// would lose it, and the walk is bounded by itself — a cycle cannot be walked
// into because a child is added to the result at most once).
func termOnceProcessTree(root int) []int {
	seen := map[int]bool{root: true}
	queue := []int{root}
	out := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range termOnceDirectChildren(pid) {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

// termOnceDirectChildren returns pid's live children, deduplicated across its
// threads.
func termOnceDirectChildren(pid int) []int {
	taskDir := filepath.Join(termOnceProcRootDir, strconv.Itoa(pid), termOnceTaskDirName)
	threads, err := os.ReadDir(taskDir)
	if err != nil {
		return nil // the process exited between the walk and the read
	}
	var out []int
	for _, th := range threads {
		raw, err := os.ReadFile(filepath.Join(taskDir, th.Name(), termOnceChildrenFileName))
		if err != nil {
			continue // the thread exited between the readdir and the read
		}
		for _, field := range strings.Fields(string(raw)) {
			child, err := strconv.Atoi(field)
			if err != nil {
				continue //stylegate:ignore a malformed children field is not a socket — it contributes no inode
			}
			out = append(out, child)
		}
	}
	return out
}

// termOnceOwnedSocketInodes returns the socket inodes named by the descriptors
// of every process in pids. A non-socket descriptor, a process that exited
// mid-walk and a permission refusal all simply contribute nothing: the
// attribution is a WHITELIST of sockets this tree provably holds, so a read
// that fails can never make a foreign socket look like ours.
func termOnceOwnedSocketInodes(pids []int) map[string]bool {
	out := map[string]bool{}
	for _, pid := range pids {
		fdDir := filepath.Join(termOnceProcRootDir, strconv.Itoa(pid), termOnceFdDirName)
		entries, err := os.ReadDir(fdDir)
		if err != nil {
			continue //stylegate:ignore a process that exited mid-walk owns no descriptor we could claim
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
			if err != nil {
				continue //stylegate:ignore the descriptor closed between the readdir and the read
			}
			if inode, ok := termOnceSocketInodeOf(target); ok {
				out[inode] = true
			}
		}
	}
	return out
}

// termOnceSocketInodeOf extracts the inode from a descriptor symlink target of
// the form socket:[<inode>].
func termOnceSocketInodeOf(target string) (string, bool) {
	if !strings.HasPrefix(target, termOnceSocketLinkPrefix) || !strings.HasSuffix(target, termOnceSocketLinkSuffix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(target, termOnceSocketLinkPrefix), termOnceSocketLinkSuffix), true
}

// termOnceOwnedListenerInode returns the socket inode of the listener this
// process owns that `before` did not know about — the arm's own subject,
// resolved from the kernel rather than from the listener's address (a listener
// on port 0 cannot be looked up by the port it was assigned).
// termOnceOwnedListenerInode returns the socket inode this process owns, is
// listening according to `table`, and did NOT own before the probe — the arm's
// own subject, resolved from the kernel rather than from the listener's address
// (a listener on port 0 cannot be looked up by the port it was assigned).
func termOnceOwnedListenerInode(afterTable, beforeTable map[string][]string, rows map[string][]string) string {
	var found []string
	for inode := range afterTable {
		if beforeTable[inode] != nil {
			continue
		}
		if _, listening := rows[inode]; !listening {
			continue
		}
		found = append(found, inode)
	}
	sort.Strings(found)
	if len(found) == 0 {
		return ""
	}
	return found[len(found)-1]
}

// termOnceOwnedNow is the attribution set alone: the socket inodes this process
// tree holds at this instant. The residue claim uses it as the filter over the
// after-snapshot, so a listener no process in this tree owns can never be
// reported as this run's residue.
func termOnceOwnedNow() map[string]bool {
	return termOnceOwnedSocketInodes(termOnceProcessTree(termOnceRootPID()))
}

// --- the no-terminal claim -------------------------------------------------

// TestTermOnceNeedsNoTerminal pins the property the whole face exists for, on
// the source that ships: the once path must not resolve a browser and must not
// consult a terminal. The claim is checked against the FILE, because the
// in-process run above cannot prove the absence of a path it never takes.
func TestTermOnceNeedsNoTerminal(t *testing.T) {
	src, err := os.ReadFile("cli_term_once.go")
	if err != nil {
		t.Fatalf("read cli_term_once.go: %v", err)
	}
	for _, forbidden := range []struct{ needle, why string }{
		{"resolveTermBrowser", "the once face must never look for a text browser"},
		{"isTerminal", "the once face must never require a terminal"},
		{"runTermBrowser", "the once face must never exec a browser"},
		{"termChatPath", "the once face drives the engine's own route, not the text page"},
	} {
		if strings.Contains(string(src), forbidden.needle) {
			t.Errorf("cli_term_once.go references %s — %s", forbidden.needle, forbidden.why)
		}
	}
	// …and the dispatch really routes there BEFORE the browser is resolved: the
	// once arm must appear before the runTermChat call in cli_term.go.
	term, err := os.ReadFile("cli_term.go")
	if err != nil {
		t.Fatalf("read cli_term.go: %v", err)
	}
	onceAt := strings.Index(string(term), "termOnceRequested(rest)")
	chatAt := strings.Index(string(term), "runTermChat(rest)")
	if onceAt < 0 || chatAt < 0 || onceAt > chatAt {
		t.Error("the once arm does not precede the interactive launch in cli_term.go — a `--once` invocation could reach the browser path")
	}
}

// turnSystemPrompt returns a model request's top-level system field — the chat
// path sends it there, NOT as a message (internal/agent Chat/ChatOps pass
// chatSystemPrompt to BuildClaudeRequest's system slot).
func turnSystemPrompt(t *testing.T, raw []byte) string {
	t.Helper()
	// The Anthropic wire carries `system` as a string OR as a block array
	// depending on the builder; accept both so the assertion pins the PROMPT,
	// not the encoding.
	var req struct {
		System json.RawMessage `json:"system"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("bad model request JSON: %v", err)
	}
	var asString string
	if json.Unmarshal(req.System, &asString) == nil {
		return asString
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(req.System, &blocks) != nil {
		t.Fatalf("the request's system field is neither a string nor a block array: %s", req.System)
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

// The three prompt markers this gate reads, each quoted from the side that OWNS
// it rather than from a copy in this test:
//
//   - chatSystemPromptMarker opens internal/agent's chat prompt (chatSystemPrompt),
//     the one the browser carrier's turns carry.
//   - chatSurfacePromptMarker opens the CHAT-surface block that ONLY the chat
//     assembly appends (internal/agent's chat_context.go — its exported head),
//     so a turn answered by the task assembly cannot satisfy the gate by
//     coincidence.
//   - taskSystemPromptMarker opens the TASK assembly (internal/agent's
//     headerRules), asserted ABSENT: it is the marker that tells the two
//     prompts apart, and it is the one a "send the full routing prompt"
//     regression would introduce.
//
// Each is an OPENING, not the whole text: a re-worded tail must not make this
// gate brittle, while a turn that stopped sending that prompt still fails. They
// are read with Contains, never with HasPrefix — the operator's reply-language
// directive is prepended to the whole assembly by internal/config, so the head of
// the system slot belongs to the configuration, not to the prompt (measured: a
// prefix assertion failed where the chat prompt was in fact present, one block
// behind the directive).
const chatSystemPromptMarker = "You are GoPi in chat mode"

var (
	chatSurfacePromptMarker = agent.ChatSurfacePromptHead()
	taskSystemPromptMarker  = agent.TaskSystemPromptHead()
)

var _ = fmt.Sprintf
var _ = time.Second

// outputLanguageUnset points BOTH reply-language channels at nothing, so a gate
// that reads the system prompt sees the prompt and not this host's
// configuration. It is the pair rule of Rule 17: t.Setenv's scope is the TEST,
// the env channel is cleared with it, and the settings channel is redirected to
// a file that declares no output_language (an ABSENT settings path resolves to
// all-defaults, so no key has to be deleted — the same shape
// internal/config's own tests use).
func outputLanguageUnset(t *testing.T) {
	t.Helper()
	t.Setenv(config.OutputLanguageEnv, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
}
