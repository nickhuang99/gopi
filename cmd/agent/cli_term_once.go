package main

// cli_term_once.go — `gopi cli "<prompt>" --once` (also `-p`): the
// NON-INTERACTIVE single turn.
//
// WHY THIS EXISTS. `gopi cli` is a second VIEW over the `gopi web` engine:
// the same webServer, the same single persistent agent, the same s.mu turn
// discipline, the same server-held history, the same tools and model
// resolution. That view needs a terminal and a text browser, so nothing could
// drive ONE turn from a script — which is what an agent-authored self-test is
// built on. This file is the smallest thing that makes one turn observable
// from a program: one prompt in → ONE turn of the EXISTING engine → the
// answer on stdout → an honest exit status, with no browser, no TTY, and
// nothing left running.
//
// IT IS THE SAME TURN, NOT A HEADLESS MODE. The turn IS the route the text
// browser POSTs to: an in-process httptest server over the REAL
// webChatHandler(), one POST to webChatPath, and the SSE stream read as it
// arrives. The model therefore sees the same request, the same system prompt
// and the same tool wiring a `gopi cli` turn sees — because handleChat IS the
// code path, not a copy of it. A reduced second engine would drift the moment
// either side changed; this cannot.
//
// THE THREE STREAMS (what a caller may rely on):
//
//	stdout  the ANSWER — the text the engine reports as this turn's result —
//	        written once, and NOTHING else, so `gopi cli "…" --once > a.txt`
//	        is one turn's answer byte for byte. Not the raw delta stream: a
//	        delta stream is everything the model said on the way there. Two
//	        measured reasons. (1) In a tool turn the deltas carry the model's
//	        narration between tool calls ("Let me read that file…") — the
//	        answer would arrive buried in its own working notes. (2) The
//	        engine's degenerate-round guard RE-ISSUES a round when a final
//	        answer is shorter than minFinalAnswerChars (40), so short answers
//	        stream TWICE and only the second is the result — measured on the
//	        first live run of this face ("OK" arrived as "OK" twice). The live
//	        delta stream is NOT lost: it is fd 3's `event: delta` frames, in
//	        order, for a caller that wants to watch the turn unfold.
//	stderr  the human-facing plumbing: the session log path, a wait notice, a
//	        one-line notice per tool call/result, slash-command output, and the
//	        failure reason. Tool activity is deliberately NOT on stdout —
//	        mixing it into the answer would make the two inseparable for a
//	        caller.
//	fd 3    the MACHINE-READABLE channel, when the caller opens it (fd 3 is the
//	        inherited-descriptor convention). Each frame is byte for byte the
//	        frame the engine wrote (`event: delta` / `event: tool` /
//	        `event: command` / `event: queued` / `event: done`), so a driver can
//	        branch on what the model DID, not only on the text it produced.
//	        The channel opens ONLY when fd 3 is a pipe or a regular file: a
//	        silent fallback to stdout would corrupt the answer, and refusing the
//	        run over a detail would be worse (Rule 15 — loud, never silently
//	        degraded).
//
// EXIT STATUS (Rule 15, honest): 0 only for a COMPLETED turn that produced
// text. A turn that failed, or that completed with no text at all, is a
// failure — never an empty success.
//
// WHAT A CALLER CANNOT OBSERVE TODAY (the follow-up study's starting list):
// reasoning/thinking tokens reach no stream; tool RESULTS are on fd 3 and
// summarised on stderr, but never on stdout; there is no per-turn timing, no
// token or cost count, no image attachment, and no continuity between runs —
// one invocation is exactly one turn, and the session ends with the process.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/borrowedfd"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/signalrec"
)

// The `gopi cli --once` vocabulary (Rule 1b: every word this face compares on,
// prints, or puts on a wire is declared once and referenced).
const (
	// termOnceFlag is the canonical spelling of the single-turn switch.
	termOnceFlag = "--once"
	// termOnceShortFlag is its short spelling — the familiar one for a
	// prompt-in/answer-out CLI. Both are accepted, both are documented.
	termOnceShortFlag = "-p"
	// termOncePromptFlag is the explicit prompt spelling: the same
	// `<verb> --prompt VALUE` shape the other gopi verbs take, alongside the
	// bare positional one.
	termOncePromptFlag = "--prompt"
)

// The answer and diagnostic streams. They are package-level VARIABLES for one
// reason: a test can then capture the exact bytes a caller would see (stdout =
// the answer alone, stderr = the plumbing) without a subprocess. Production
// never reassigns them.
var (
	// termOnceAnswerOut is stdout: the answer text, and nothing else.
	termOnceAnswerOut io.Writer = os.Stdout
	// termOnceDiagOut is stderr: the plumbing and the failure reason.
	termOnceDiagOut io.Writer = os.Stderr

	// termOnceAgentFactory builds the single persistent agent this face drives.
	// It is the SAME construction `gopi cli` and `gopi web` use; a test
	// substitutes a stub agent pointed at a mock model, which is how the turn
	// itself is asserted without a network.
	termOnceAgentFactory = func() (*agent.Agent, error) { return newAgent("") }
)

// termOnceStream3FD is the file descriptor the machine-readable event channel
// is taken from (the inherited-descriptor convention). It is a VARIABLE, not a
// constant, for one reason: a test can then point the channel at a file it
// controls and read back the exact bytes a caller would see, with no
// subprocess. The wire itself is unchanged.
var termOnceStream3FD = 3

// termOnceEventChannelName is the borrowed descriptor's label (the name an fd
// trail shows).
const termOnceEventChannelName = "gopi-cli-once-events"

// termOnceWaitNotice is printed to stderr ONCE if the turn has to wait for
// s.mu (a concurrent turn holds it): a script that sees nothing on stderr must
// not conclude the command hung.
const termOnceWaitNotice = "gopi cli: waiting for the in-flight turn to finish…"

// termOncePrefix is the prefix of every human line this face writes to stderr,
// so a caller can tell this carrier's plumbing from the model's own output at
// a glance.
const termOncePrefix = "gopi cli: "

// termOnceSSEEventPrefix / termOnceSSEDataPrefix are the two line shapes the
// engine's framer writes (writeSSEFrame) — the ONE place a client reads that
// wire format back.
const (
	termOnceSSEEventPrefix = "event: "
	termOnceSSEDataPrefix  = "data: "
)

// termOnceScanBufBytes bounds one SSE line. A tool call digest and a command's
// output can both be long; the engine caps a transcript digest at
// webTranscriptDigestMaxRunes, so this is deliberate headroom.
const termOnceScanBufBytes = 256 * 1024

// The exit statuses this face returns. Named (Rule 1a): the help page, the
// report and the contract test all speak about them.
const (
	// termOnceExitOK is a COMPLETED turn that produced text.
	termOnceExitOK = 0
	// termOnceExitTurnFailed is a turn that failed, or that completed with no
	// text at all.
	termOnceExitTurnFailed = 1
	// termOnceExitUsage is a usage error (a bad flag, or no prompt).
	termOnceExitUsage = 2
)

// termOnceUsageTemplate is the `gopi cli --once -h` page, BEFORE the exit
// statuses are substituted (see termOnceUsage). It opens with its invocations
// — the capability-first shape cmd/agent/help_capability_first_test.go pins for
// a service verb — and holds the why for the tail.
const termOnceUsageTemplate = `GoPi cli ` + termOnceFlag + ` — ask ONE question, print the answer, exit.

  gopi cli "prompt" ` + termOnceFlag + `     one turn, answer on stdout, then exit
  gopi cli ` + termOnceFlag + ` "prompt"     the same, the flag first
  gopi cli ` + termOnceShortFlag + ` "prompt"         the same, short spelling
  gopi cli ` + termOncePromptFlag + ` "prompt"   the same, as a named flag
  gopi cli ` + termOnceShortFlag + ` "prompt" ` + termOnceFlag + ` ` + HelpFlag + `   this help (runs nothing)

Options:
  ` + termOnceFlag + `, ` + termOnceShortFlag + `      run ONE turn and exit — no browser, no terminal, no
                     interaction. Without it ` + "`" + CliVerbCLI + "`" + ` opens the interactive
                     text-browser chat, exactly as before.
  ` + termToolsFlag + `            tool-enabled turns: the same full toolset a
                     ` + "`gopi cli --tools`" + ` turn runs (` + "`" + webToolsEnv + `=1` + "`" + ` does the
                     same). Without it the model answers from its own
                     knowledge and calls no tool.

Streams:
  stdout             the answer text, streamed as the model produces it, and
                     NOTHING else.
  stderr             the session log path, one line per tool call/result, and
                     the reason for a failure.
  fd 3               the machine-readable channel, when the caller opens it:
                     each frame is byte-for-byte what the engine wrote
                     (` + "`event: delta`" + `, ` + "`event: tool`" + `, ` + "`event: done`" + `), e.g.
                       gopi cli ` + termOnceShortFlag + ` "hi" ` + termOnceFlag + ` 3>events.txt

Exit status:
  ` + termOnceExitOKText + `                  the turn completed AND produced text
  ` + termOnceExitTurnFailedText + `                  the turn failed, or produced no text at all
  ` + termOnceExitUsageText + `                  a usage error (a bad flag, or no prompt)

Notes:
  This is the same turn the interactive carrier runs — the same engine, the
  same single session, the same tools and model resolution — reached without a
  terminal. It is what an agent-authored self-test builds on: a script can
  drive ONE turn, read the answer on stdout, read what the model DID on fd 3,
  and branch on the exit status. Environment, flags and defaults are the
  interactive carrier's (` + "`gopi cli " + HelpFlag + "`" + ` prints them), so the two faces
  cannot drift.
`

// The exit statuses AS TEXT, for the usage page: a const block cannot call
// strconv, and the numbers have exactly ONE definition (the constants above) —
// these are derived from them below, so a changed status cannot leave the help
// page lying.
const (
	termOnceExitOKText         = "0"
	termOnceExitTurnFailedText = "1"
	termOnceExitUsageText      = "2"
)

// termOnceUsage is the usage page the binary prints: the template above with
// the exit statuses rendered from their own constants.
var termOnceUsage = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(
	termOnceUsageTemplate,
	"\n  "+termOnceExitOKText+"                  ", "\n  "+strconv.Itoa(termOnceExitOK)+"                  "),
	"\n  "+termOnceExitTurnFailedText+"                  ", "\n  "+strconv.Itoa(termOnceExitTurnFailed)+"                  "),
	"\n  "+termOnceExitUsageText+"                  ", "\n  "+strconv.Itoa(termOnceExitUsage)+"                  ")

// termOnceOptions is one resolved `--once` invocation.
type termOnceOptions struct {
	prompt   string
	tools    bool
	wantHelp bool
}

// termOnceRequested reports whether the argv tail selects the non-interactive
// single turn. It reads the ONE list of spellings, so the dispatch and the
// parser cannot disagree about what "once" means.
func termOnceRequested(args []string) bool {
	for _, a := range args {
		if a == termOnceFlag || a == termOnceShortFlag {
			return true
		}
	}
	return false
}

// termOnceParseArgs resolves the `--once` argv tail. The flags the interactive
// carrier takes are parsed by the SAME function the carrier uses
// (webParseFlags: --tools / --addr / help), so a switch cannot mean one thing
// here and another there; the once-specific words are read here.
//
// A MISSING PROMPT IS A USAGE ERROR (Rule 15): `gopi cli --once` with no
// prompt is a caller mistake, and a silent fall-through to the browser or to
// the agent loop would be a bug a script cannot see.
func termOnceParseArgs(args []string) (termOnceOptions, error) {
	var opts termOnceOptions
	parsed, err := webParseFlags(args, "", webToolsEnabled(), webThinkingEnabled())
	if err != nil {
		return opts, err
	}
	opts.tools = parsed.Tools
	opts.wantHelp = parsed.Help

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == termOnceFlag || a == termOnceShortFlag || a == termToolsFlag ||
			a == HelpFlag || a == HelpFlagLong:
			// A flag word with no value: webParseFlags owns its meaning.
		case a == webAddrFlag:
			// The address flag's VALUE is the next word — never the prompt.
			i++
		case strings.HasPrefix(a, webAddrFlag+"="):
			// Joined form: nothing to consume.
		case a == termOncePromptFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a prompt, e.g. %s %q", termOncePromptFlag, termOncePromptFlag, termOncePromptExample)
			}
			i++
			opts.prompt = args[i]
		case strings.HasPrefix(a, termOncePromptFlag+"="):
			opts.prompt = strings.TrimPrefix(a, termOncePromptFlag+"=")
		case strings.HasPrefix(a, "-") && a != "-":
			return opts, fmt.Errorf("unknown flag %q — `gopi %s %s %s %s %s` prints this face's usage",
				a, CliVerbCLI, termOnceShortFlag, `"`+termOncePromptExample+`"`, termOnceFlag, HelpFlagLong)
		default:
			// The bare positional prompt: the familiar spelling.
			if opts.prompt != "" {
				return opts, fmt.Errorf("more than one prompt given (%q and %q) — one turn takes ONE prompt", opts.prompt, a)
			}
			opts.prompt = a
		}
	}
	return opts, nil
}

// termOncePromptExample is the example prompt the usage errors quote, so every
// message spells one invocation the reader can paste.
const termOncePromptExample = "say OK"

// The signal vocabulary of this face. `--once` OWNS its terminating signals:
// the turn in flight is work worth finishing (its closeout writes ONE
// session_end and closes the ledger row — see termOnceSignalWatch), and the
// recorder's re-raise would take that away from a run that is already mid-turn.
// So the two names the handler needs are declared here (Rule 1b) and the grace
// this face waits for its own abort to unwind is bounded by its own constant,
// never by a literal.
const (
	// termOnceSignalBuffer is the buffered-signal channel depth. One is
	// enough: the handler acts once and the process goes on to unwind.
	termOnceSignalBuffer = 1
	// termOnceSignalUnwindWait bounds how long a turn is given to unwind
	// after the abort was requested, before this face stops waiting and goes
	// on to the closeout it owns. The turn's own cancellation is immediate;
	// the wait exists because the SSE reader is a separate goroutine and a
	// bound, not a race, is what makes the exit deterministic (Rule 15: a
	// silent hang would be worse than an honest deadline).
	termOnceSignalUnwindWait = 5 * time.Second
	// termOnceSignalAbortReason is what stderr carries when a signal ended
	// the turn: an operator reading the run's output must learn WHY the turn
	// stopped, not infer it from an exit status.
	termOnceSignalAbortReason = "received %v — aborting the turn and writing the session closeout"
)

// runTermOnce is the whole `--once` face: build the engine, run one turn
// through it, print, exit honestly. Nothing is left running — there is no
// listener the operator must know about (the HTTP server is in-process, bound
// to loopback on an ephemeral port and closed before this returns), no browser
// and no child process.
func runTermOnce(args []string) int {
	opts, err := termOnceParseArgs(args)
	if err != nil {
		fmt.Fprintf(termOnceDiagOut, "%s%v\n", termOncePrefix, err)
		return termOnceExitUsage
	}
	if opts.wantHelp {
		fmt.Print(termOnceUsage)
		return termOnceExitOK
	}
	if strings.TrimSpace(opts.prompt) == "" {
		fmt.Fprintf(termOnceDiagOut, "%s%s needs a prompt — `gopi %s %q %s`\n\n%s",
			termOncePrefix, termOnceFlag, CliVerbCLI, termOncePromptExample, termOnceFlag, termOnceUsage)
		return termOnceExitUsage
	}

	// The machine-readable channel is planned BEFORE the turn: a caller that
	// asked for fd 3 must learn it is unusable BEFORE the model is called,
	// never after the answer was already produced (Rule 15).
	events, closeEvents, err := termOnceOpenEventSink()
	if err != nil {
		fmt.Fprintf(termOnceDiagOut, "%s%v\n", termOncePrefix, err)
		return termOnceExitUsage
	}
	defer closeEvents()

	// THE TURN OWNS ITS SIGNALS (see termOnceSignalWatch): signal.Notify first,
	// then the ownership claim in the very next statement — the window between
	// the two is the only one in which the recorder can still re-raise us.
	sigCh := termOnceSignalWatch()
	defer signal.Stop(sigCh)

	// The SAME single-persistent-agent construction `gopi cli` and `gopi web`
	// use (one session id + one session JSONL + one raw log), so the engine is
	// built the same way rather than with a second copy of the rules. The write
	// boundary is frozen beside it: this surface executes tools, so a boundary
	// left at the package defaults would judge every write as a question.
	// THE SAME DEFAULT THE INTERACTIVE LEG PUBLISHES (see the call in runTermChat's
	// launch chain, cli_term.go): `gopi cli "<prompt>" --once` is the same
	// conversational face over the same engine, so its system prompt must not
	// teach internal/agent's 60-minute TASK ceiling either. It is published by
	// the ONE implementation (publishSessionBudgetDefault), so the value, the
	// never-override rule and the failure line cannot drift between the two legs
	// of this face — only the start-up prefix differs.
	chatFreezeChatPolicyState(chatFacePrefixCli, "")
	publishCliSessionBudgetDefault(termToolStylesFor(lineedit.ColourOff))
	srv := &webServer{addr: webDefaultAddr(), tools: opts.tools, newWebAgent: termOnceAgentFactory}
	ag, err := srv.newWebAgent()
	if err != nil {
		fmt.Fprintf(termOnceDiagOut, "%sran no turn — failed to create the agent: %v\n", termOncePrefix, err)
		return termOnceExitTurnFailed
	}
	ag.PersistChatSession = true
	srv.agent = ag
	srv.startedAt = time.Now()
	srv.visionModel = deepseek.ResolveVisionModel(cfgForVision())
	printLogPathsQuiet(ag, true)

	// The turn runs in its own goroutine so a signal can ABORT it: the
	// run-scoped cancel (ag.InterruptRun) reaches the in-flight model request,
	// the engine's loop unwinds through its own end-of-run path, and this
	// goroutine keeps working — the finish below (HttpSrv.Close, the session
	// closeout) is exactly what the recorder's re-raise used to take away.
	//
	// It also makes the signal arm scheduling-proof (the regression guard's
	// contract): the recorder's grace and this face's unwind both start at the
	// SIGTERM, and the unwind is termOnceSignalUnwindWait — many times the
	// grace — so the arm is decided by the CODE, never by which goroutine won a
	// race. A synchronous turn would make the arm depend on whether the
	// goroutine scheduler had run the watcher yet.
	httpSrv := httptest.NewServer(srv.webChatHandler())
	turnDone := make(chan termOnceTurnResult, 1)
	go func() {
		// Buffered (one send ever happens), so this send never blocks even
		// when the signal arm below has already stopped listening: the turn
		// still ran and is still recorded, and the closeout this function owns
		// reports what it produced.
		turnDone <- termOnceRunTurn(httpSrv.URL, opts.prompt, events)
	}()

	var result termOnceTurnResult
	select {
	case result = <-turnDone:
	case sig := <-sigCh:
		fmt.Fprintf(termOnceDiagOut, "%s"+termOnceSignalAbortReason+"\n", termOncePrefix, sig)
		ag.InterruptRun()
		select {
		case result = <-turnDone:
		case <-time.After(termOnceSignalUnwindWait):
			result = termOnceTurnResult{exitCode: termOnceExitTurnFailed,
				reason: fmt.Sprintf(termOnceUnwoundReason, termOnceSignalUnwindWait)}
		}
	}
	// The server/clients go away in every arm: an in-process loopback listener
	// must never outlive the turn it was built for.
	httpSrv.Close()

	elapsed := time.Since(srv.startedAt).Seconds()
	if result.reason != "" {
		fmt.Fprintf(termOnceDiagOut, "%s%s\n", termOncePrefix, result.reason)
	}
	ag.CloseChatSession(result.exitCode, srv.iterCount, srv.totalTokens, elapsed,
		agent.EndCause{Reason: sessionevent.SessionEndReasonComplete})
	ag.CloseChatRow(result.exitCode)
	// THE SAME SETTLE EVERY CLAIMING DOOR OWES (Rule 2: ONE waiter, in
	// internal/signalrec). This face claims the recorder's handler and its
	// closeout above is written, but the record is filed by the recorder's own
	// goroutine — which os.Exit here does not wait for.
	settleProvenanceRecord(termProgLabel)
	return result.exitCode
}

// termOnceUnwoundReason is the reason reported when the turn did not finish
// unwinding inside termOnceSignalUnwindWait. stderr says what ran out, never a
// bare silence (Rule 15).
const termOnceUnwoundReason = "the turn did not finish unwinding within %v — closing the session anyway"

// termOnceSignalWatch installs this face's terminating-signal handler and
// declares the resulting ownership to the recorder. It returns the channel the
// caller selects on, and the caller MUST `defer signal.Stop(sigCh)`.
//
// WHY THIS FACE OWNS THE SIGNAL (measured, not assumed). Before this, `--once`
// registered nothing, so the ONLY handler for SIGTERM was the recorder's
// (signalrec.Install, early in main). Its argv table cannot classify
// `gopi cli "<prompt>" --once` — the mode word is `cli`, which owns no entry —
// so it forwarded: a provenance record, then signal.Reset(SIGTERM) (which
// clears the disposition GLOBALLY) and a self-kill one grace later. Measured on
// master: `gopi cli … --once` SIGTERMed mid-turn exited 143 and session.jsonl
// held session_start + signal_verdict and NO session_end. The turn in flight IS
// work worth finishing — one turn, one session, one ledger row — so the
// re-raise was destroying exactly the closeout this face exists to write.
//
// ORDER MATTERS AND IS NOT COSMETIC: signal.Notify first, ClaimSignalHandlers
// second. The claim is read at SIGNAL TIME, and between the two calls the
// recorder still reads "unclaimed" — the claim is what makes the difference.
func termOnceSignalWatch() chan os.Signal {
	sigCh := make(chan os.Signal, termOnceSignalBuffer)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	// The recorder must still file its provenance record; it must then leave
	// the signal ALONE. ClaimSignalHandlers is one-way on purpose: this face's
	// handler stays installed for the rest of the process.
	signalrec.ClaimSignalHandlers()
	return sigCh
}

// termOnceTurnResult is one turn's outcome: the exit status and the sentence
// stderr carries when it is non-zero.
type termOnceTurnResult struct {
	exitCode int
	reason   string
}

// termOnceRunTurn drives ONE turn through the real engine and reports its
// outcome: POST the prompt to the engine's own route, then decode the SSE
// stream as it arrives.
func termOnceRunTurn(baseURL, prompt string, events *termOnceEventSink) termOnceTurnResult {
	body, err := json.Marshal(webChatRequest{Message: prompt})
	if err != nil {
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed, reason: "could not encode the prompt: " + err.Error()}
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+webChatPath, strings.NewReader(string(body)))
	if err != nil {
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed, reason: "could not build the turn request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed, reason: "the engine did not answer: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, webMaxChatTextBytes)) //stylegate:ignore best-effort read of a refusal body; the status IS the failure and is reported below
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed,
			reason: fmt.Sprintf("the engine refused the turn (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(msg)))}
	}
	// The answer STREAMS: each delta is written to stdout as it lands, so a
	// caller can watch and time the turn instead of waiting for a blob.
	return termOnceConsume(resp.Body, termOnceAnswerOut, events)
}

// termOnceConsume reads the engine's SSE stream to its terminal frame,
// mirroring every frame to the machine-readable channel when the caller opened
// fd 3, and writing the engine's own terminal result to the answer stream. It
// returns the turn's outcome.
//
// Both carriers' frames arrive here unchanged, because there is only one
// framer (writeSSEFrame) and one vocabulary (webEvent*).
func termOnceConsume(r io.Reader, answer io.Writer, events *termOnceEventSink) termOnceTurnResult {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, termOnceScanBufBytes), termOnceScanBufBytes)
	event := ""
	doneResult := ""
	reason := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, termOnceSSEEventPrefix):
			event = strings.TrimPrefix(line, termOnceSSEEventPrefix)
		case strings.HasPrefix(line, termOnceSSEDataPrefix):
			data := strings.TrimPrefix(line, termOnceSSEDataPrefix)
			events.writeFrame(event, data)
			switch event {
			case webEventQueued:
				fmt.Fprintln(termOnceDiagOut, termOnceWaitNotice)
			case webEventDelta:
				// The live text. It is NOT the answer stream — see the file
				// header: a tool turn's deltas carry the model's narration
				// between calls, and a short final answer is re-issued by the
				// engine's degenerate-round guard, so an early delta can be
				// superseded before the turn ends. fd 3 carries them (mirrored
				// above); the answer stream gets the result, once, below.
			case webEventTool:
				if notice, err := termOnceToolNotice(data); err == nil {
					fmt.Fprint(termOnceDiagOut, notice)
				}
			case webEventCommand:
				if out, err := termOnceCommandText(data); err == nil {
					fmt.Fprint(termOnceDiagOut, out)
				}
			case webEventError:
				reason = termOnceEventErrorReason(data)
			case webEventDone:
				var d struct {
					Result string `json:"result"`
					Error  string `json:"error"`
				}
				if json.Unmarshal([]byte(data), &d) == nil {
					if d.Error != "" {
						reason = d.Error
					}
					doneResult = d.Result
				}
			}
			event = ""
		}
	}
	if err := sc.Err(); err != nil {
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed, reason: "the engine's stream broke: " + err.Error()}
	}
	if reason != "" {
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed, reason: reason}
	}
	if strings.TrimSpace(doneResult) == "" {
		// A completed turn with NO text is a FAILURE, not an empty success: a
		// caller seeing exit 0 must be able to trust there is an answer.
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed,
			reason: "the turn completed without producing any text"}
	}
	// stdout: the turn's answer, once — the engine's own result string.
	if _, err := io.WriteString(answer, doneResult); err != nil {
		// The consumer went away (a closed pipe): the turn still ran and is
		// recorded, but the caller must learn the answer never reached them
		// (Rule 15).
		return termOnceTurnResult{exitCode: termOnceExitTurnFailed,
			reason: "the answer could not be written: " + err.Error()}
	}
	return termOnceTurnResult{exitCode: termOnceExitOK}
}

// termOnceToolNotice renders one tool event as a stderr line, reusing the
// terminal carrier's own line builder so both carriers describe one call
// identically (termToolLine: "kind name digest").
func termOnceToolNotice(data string) (string, error) {
	var ev agent.ToolEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return "", err
	}
	return termOncePrefix + termToolLine(ev), nil
}

// termOnceCommandText is a slash command's output (a command never reaches the
// model): it goes to stderr, because stdout belongs to the answer alone.
func termOnceCommandText(data string) (string, error) {
	var d struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return "", err
	}
	if d.Error != "" {
		return termOncePrefix + d.Error + "\n", nil
	}
	return d.Output, nil
}

// termOnceEventErrorReason extracts a carrier-level failure frame's message.
func termOnceEventErrorReason(data string) string {
	var d struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &d); err != nil || d.Error == "" {
		return "the carrier reported an error"
	}
	return d.Error
}

// termOnceEventSink is the machine-readable channel (fd 3). It is a no-op when
// the caller did not open one — a frame write must never be a failure of the
// turn.
type termOnceEventSink struct {
	w      io.Writer
	opened bool
}

// writeFrame mirrors one engine frame: the event line, the payload line and
// the blank line that terminates it — exactly the bytes writeSSEFrame emitted.
func (s *termOnceEventSink) writeFrame(event, data string) {
	if !s.opened {
		return
	}
	io.WriteString(s.w, termOnceSSEEventPrefix+event+"\n")
	io.WriteString(s.w, termOnceSSEDataPrefix+data+"\n\n")
}

// termOnceOpenEventSink takes the machine-readable channel from
// termOnceStream3FD. The descriptor is BORROWED — it belongs to the caller —
// so it is wrapped with os.NewFile and the wrapper's finalizer is disarmed on
// every exit (internal/borrowedfd): a dropped wrapper's finalizer would close
// a descriptor this process never opened.
//
// The channel opens only for a pipe or a regular file. A terminal, a closed
// descriptor or an unreadable one leaves it SHUT (no-op) rather than
// redirecting engine frames somewhere the caller did not ask for.
func termOnceOpenEventSink() (*termOnceEventSink, func(), error) {
	f := os.NewFile(uintptr(termOnceStream3FD), termOnceEventChannelName)
	if f == nil {
		return &termOnceEventSink{}, func() {}, nil
	}
	closeSink := func() { borrowedfd.Detach(f) }
	st, err := f.Stat()
	if err != nil {
		// No such descriptor (the ordinary invocation): no channel, and no
		// error — the caller simply did not ask for one.
		return &termOnceEventSink{}, closeSink, nil
	}
	if mode := st.Mode(); !mode.IsRegular() && mode&os.ModeNamedPipe == 0 {
		return &termOnceEventSink{}, closeSink, nil
	}
	return &termOnceEventSink{w: f, opened: true}, closeSink, nil
}
