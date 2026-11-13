package main

// cli_term.go — `gopi cli`: the terminal chat carrier's LAUNCHER half.
//
// THE CARRIER (epic: a zero-dependency terminal chat surface). `gopi web`
// serves a page whose entire value is JavaScript + Server-Sent Events; a
// text-mode browser runs no JS, so it can neither see the transcript nor
// submit a turn (measured 2026-09-17: `w3m -dump /` = 116 bytes against a
// 41,611-byte page, no `<form action>`, no named input — see
// devdoc/w3m-cli-feasibility.md). `gopi cli` is therefore a SECOND VIEW over
// the SAME engine, never a rewrite of it:
//
//	GET  /cli       → server-rendered history + a real form   (term.go)
//	POST /cli/chat  → ONE streaming HTML response per turn: the deltas as they
//	                  arrive, then the turn closes its own document with the
//	                  same tail (form + bottom anchor)         (term.go)
//
// The engine is untouched: this verb starts the EXISTING `gopi web` HTTP
// plumbing — the same webServer, the same single persistent agent, the same
// s.mu turn discipline with its TryLock "queued" answer, the same server-held
// s.history, the same slash-command table, the same agent.OnToolEvents tool
// stream — and then execs the text browser at the printed URL. `gopi web`'s
// own page, routes and SSE frames are not modified by any of this.
//
// WHY THE FORM MUST BE REAL (both halves of the wire, measured):
//
//   - a text browser submits only a NAMED field (`<input name="message">`)
//     inside a form with a real `action` — the browser page has neither, so
//     Tab×N never focuses anything and Enter at load exits the browser;
//   - `<textarea>` is UNUSABLE: Enter shells out to $EDITOR, so the field is
//     single-line by necessity, and a multi-line draft is refused loudly
//     (Rule 15) rather than silently truncated.
//
// The page NEVER redirects GET /cli: a reload must not re-run the last turn.
// A terminal cannot stream into a page it has already rendered, so one turn is
// ONE response body — the fragments arrive in a single document and the
// terminal paints them as they land; the SETTLED transcript is the next GET.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/signalrec"
)

// The `gopi cli` service verbs (Rule 1b/14: the dispatch, the usage page and
// the word-completion table all reference these — a rename is one edit).
const (
	// termOpenVerb is the `gopi cli` second-level verb: start the same run
	// explicitly. Accepted so the pair reads like `gopi web`'s group and a
	// script has an unambiguous spelling. (The verb word itself is
	// CliVerbCLI, defined with the other top-level verbs in cli_verbs.go.)
	termOpenVerb = "open"
)

// termToolsFlag is the `--tools` spelling for `gopi cli` — the same switch
// `gopi web --tools` takes (the tool loop is what makes a live tool-activity
// line possible at all).
const termToolsFlag = "--tools"

// termDefaultTurnTimeout bounds one /cli/chat turn. Unlike the browser page —
// where the operator watches a live stream and can stop it — a terminal turn
// is ONE response body: a turn that never returns holds the text browser on a
// blank screen with no key that helps. The budget is therefore ON by default
// here; the browser carrier's own default stays 0 = unlimited, untouched.
const termDefaultTurnTimeout = 10 * time.Minute

// termTurnTimeoutEnv overrides the /cli turn budget in seconds; 0 = unlimited.
// Read in exactly ONE place (termTurnTimeout — Rule 2).
const termTurnTimeoutEnv = "GOPI_CLI_TURN_TIMEOUT_SEC"

// termBrowserExitGrace bounds the wait for the text browser once the server or
// a signal ended the run, so the launcher never leaves a stray child behind.
const termBrowserExitGrace = 3 * time.Second

// termGroupServerFlags is the flag set that makes a bare `gopi cli …`
// invocation the chat SERVER (not a second-level subcommand).
func termGroupServerFlags(a string) bool {
	switch {
	case a == termToolsFlag:
		return true
	case strings.HasPrefix(a, webAddrFlag):
		return true
	case a == HelpFlag || a == HelpFlagLong:
		return true
	}
	return false
}

// isTermSubcommand reports whether argv begins with the `cli` group verb, and
// runs it to completion (the process exits inside).
//
// Dispatch rule, mirroring `gopi web`: `gopi cli open [flags]` is the second
// level; a bare `gopi cli` or a server flag opens the chat; anything else is a
// USAGE ERROR — never an agent-loop fallthrough (a typo must not burn an LLM
// session, the M4.13 lesson the CLI contract suite exists for).
func isTermSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbCLI {
		return false
	}
	rest := args[1:]
	// The NON-INTERACTIVE single turn (cli_term_once.go): `gopi cli "prompt"
	// --once` never opens the browser and never needs a terminal. It is
	// recognised on the WHOLE tail, because the flag may sit after the prompt
	// (the familiar `gopi cli "…" -p` shape) as well as before it.
	if termOnceRequested(rest) {
		os.Exit(runTermOnce(rest))
	}
	if len(rest) > 0 && !termGroupServerFlags(rest[0]) {
		if rest[0] == termOpenVerb {
			os.Exit(runTermChat(rest[1:]))
		}
		fmt.Fprintf(os.Stderr, "gopi cli: unknown subcommand %q — want %s, or flags (`gopi cli -h`)\n\n%s",
			rest[0], termOpenVerb, termUsage)
		os.Exit(1)
	}
	os.Exit(runTermChat(rest))
	return true
}

// termTurnTimeout resolves the per-turn budget for the /cli carrier: the env
// override when it parses (0 = unlimited), otherwise the default. handleTermChat
// is the one place that applies the result. The browser budget (webTurnTimeout)
// is a SEPARATE knob: the two carriers fail differently, so neither silently
// inherits the other's value.
func termTurnTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv(termTurnTimeoutEnv))
	if v == "" {
		return termDefaultTurnTimeout
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "[gopi] warning: ignoring %s=%q (want whole seconds ≥ 0), using default %v\n",
			termTurnTimeoutEnv, v, termDefaultTurnTimeout)
		return termDefaultTurnTimeout
	}
	return time.Duration(n) * time.Second
}

// runTermChat is the whole `gopi cli` verb: start the EXISTING web engine on
// the chat surface, hand the terminal to the text browser, exit when it exits.
// `-h` prints the usage page and runs NOTHING (the worker -h pitfall: a help
// query is a zero-side-effect path, asserted by cli_term_test.go).
func runTermChat(args []string) int {
	addr := webDefaultAddr()
	toolsEnabled := webToolsEnabled()
	opts, err := webParseFlags(args, addr, toolsEnabled, webThinkingEnabled())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi cli: %v\n", err)
		return 1
	}
	addr, toolsEnabled, wantHelp := opts.Addr, opts.Tools, opts.Help
	if wantHelp {
		fmt.Print(termUsage)
		return 0
	}
	if addr == "" {
		addr = webDefaultAddr()
	}

	// THE CONFIGURATION IS RESOLVED HERE, ONCE FOR THE WHOLE SERVICE — the same
	// defect and the same cause `gopi chat` had (6fd8b134): main() resolves the
	// configuration after the subcommand chain and this face exits from inside
	// that chain, so config.ApplyEnv never ran and no settings.json knob reached
	// the environment. internal/agent reads thinking / reasoning_effort from the
	// ENVIRONMENT only, so every turn this surface served went out with
	// thinking=disabled while the operator's settings said otherwise.
	//
	// IT COMES BEFORE THE BROWSER PROBE ON PURPOSE. The configuration is the
	// PROCESS's, not a capability of the surface: placing it after the probe
	// would make "which model / effort / endpoint this run uses" depend on
	// whether a text browser happens to be installed, and a refusal about a
	// missing browser would then be reported against a run whose configuration
	// was never resolved. Everything that can REFUSE the command line (the usage
	// return above) has already returned; a refusal about the ENVIRONMENT comes
	// after, and neither mutates what the other reads.
	//
	// WHY HERE AND NOT IN newWebAgent: this is a long-running server and its
	// agent factory can be called more than once. See
	// service_config_resolution.go (one call, one precedence — Rule 2).
	if code := resolveTermRunConfig(args); code != serviceConfigExitOK {
		return code
	}

	// Resolve the browser BEFORE binding anything: a missing text browser is
	// the one failure the operator can act on, and it must not leave a server
	// (or a session) half-started to clean up.
	links, err := resolveTermBrowser()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi cli: %v\n", err)
		return 1
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi cli: cannot listen on %s: %v\n", addr, err)
		return 1
	}

	// The SAME single-persistent-agent construction `gopi web` uses (one
	// session id + one session JSONL + one raw log for the whole conversation,
	// closed once at shutdown by finalizeChatSession) — the engine is shared,
	// so it is built the same way, not with a second copy of the rules. The
	// write boundary is frozen too, for the same reason it is in runWeb: this
	// face executes tools, and a boundary left at the package defaults judges
	// every write against `prompt *` instead of the operator's rules.
	chatFreezeChatPolicyState(chatFacePrefixCli, "")
	srv := &webServer{addr: addr, tools: toolsEnabled, carrier: webChatCarrierCLI, newWebAgent: func() (*agent.Agent, error) { return newAgent("") }}
	ag, err := srv.newWebAgent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi cli: failed to create agent: %v\n", err)
		return 1
	}
	ag.PersistChatSession = true
	srv.agent = ag
	srv.startedAt = time.Now()
	srv.visionModel = deepseek.ResolveVisionModel(cfgForVision())
	printLogPaths(ag)

	// The SAME LAN gate the browser server uses: a non-loopback bind is a
	// tool-executing agent on the network and must never be served bare. The
	// terminal carrier adds no exception to that rule.
	handler, token, err := webGatedHandler(addr, srv.webChatHandler(), webLocalTrust(addr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi cli: %v\n", err)
		return 1
	}

	url := termChatURL(addr, ln, token)
	webPrintListening(addr, ln, token, webLocalTrust(addr))
	if srv.tools {
		fmt.Fprintf(os.Stderr, "%s\n", termToolsNotice)
	}

	httpSrv := &http.Server{Handler: handler}
	srvErr := make(chan error, 1)
	go func() { srvErr <- httpSrv.Serve(ln) }()

	fmt.Fprintf(os.Stderr, "gopi cli: opening %s in the text browser — quit it to end the chat.\n", url)

	shutdown := func() {
		srv.shuttingDown = true
		ctx, cancel := context.WithTimeout(context.Background(), webShutdownGrace)
		defer cancel()
		_ = httpSrv.Shutdown(ctx) //stylegate:ignore carrier shutdown; finalizeChatSession below closes the session either way
		select {
		case <-srvErr:
		case <-time.After(webShutdownGrace):
			_ = httpSrv.Close() //stylegate:ignore final close right after Shutdown; the server is already stopping
			<-srvErr
		}
	}

	browserDone := make(chan error, 1)
	go func() { browserDone <- runTermBrowser(links, url) }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	// `gopi cli` is the second unclassifiable shape the recorder's argv table
	// cannot place (its mode word "cli" owns no handler entry), so the claim is
	// made explicitly here too -- same reason as the bare-prompt path in main.go
	// (see signalrec.ClaimSignalHandlers).
	signalrec.ClaimSignalHandlers()
	defer signal.Stop(sigCh)

	code := 0
	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "gopi cli: server error: %v\n", err)
			code = 1
		}
		shutdown()
		drainBrowser(browserDone)
	case err := <-browserDone:
		// The text browser IS the carrier: when the operator quits it the chat
		// is over. Any other outcome (exec failure, non-zero exit) is reported
		// — never a silent "chat ended" (Rule 15).
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi cli: text browser exited: %v\n", err)
			code = 1
		}
		shutdown()
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "gopi cli: %v — shutting down, finalizing chat session…\n", sig)
		shutdown()
		drainBrowser(browserDone)
	}

	srv.finalizeChatSession(code == 0)
	// The chat face claims its own handlers (above), so it owes the recorder the
	// same settle every claiming door owes: the closeout above is printed, the
	// record may still be in flight, and os.Exit does not wait (Rule 2: ONE
	// waiter, in internal/signalrec).
	settleProvenanceRecord(termProgLabel)
	return code
}

// termProgLabel is this face's name in the recorder-handshake report lines
// (settleProvenanceRecord). It is the program word the face's own lines already
// carry, minus the separator, so an operator greps one spelling.
const termProgLabel = "gopi cli"

// drainBrowser waits (bounded) for the browser goroutine when a shutdown was
// driven by the server or a signal, so no stray child outlives the launcher.
func drainBrowser(done chan error) {
	select {
	case <-done:
	case <-time.After(termBrowserExitGrace):
	}
}

// termChatURL is the URL the text browser opens: the first display URL plus
// the chat path, carrying the access token exactly when the gate would demand
// one for that URL (webTokenURL owns that decision — a loopback URL is served
// without a token under local trust, so printing one there would be a false
// instruction).
func termChatURL(addr string, ln net.Listener, token string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = ""
	}
	port := ""
	if ln != nil {
		if _, p, e := net.SplitHostPort(ln.Addr().String()); e == nil {
			port = p
		}
	}
	if port == "" {
		_, port, _ = net.SplitHostPort(addr) //stylegate:ignore best-effort port recovery; the default below covers a parse failure
	}
	if port == "" {
		port = strconv.Itoa(webDefaultPort)
	}
	urls := webDisplayURLs(host, port)
	if len(urls) == 0 {
		return "http://" + webLoopbackHostName + ":" + port + termChatPath
	}
	return webTokenURL(urls[0], token, webLocalTrust(addr)) + termChatPath
}

// termUsage is the `gopi cli -h` page. It opens with its invocations — the
// capability-first shape `cmd/agent/help_capability_first_test.go` pins for the
// service verbs — and holds the rationale for the end.
const termUsage = `GoPi cli — chat in a TEXT-mode browser (zero-dependency terminal carrier).

  gopi cli                    open the chat surface in the text browser
  gopi cli open               the same, spelled explicitly
  gopi cli "prompt" ` + termOnceFlag + `      ONE turn, no browser: answer on stdout,
                              then exit (also ` + termOnceShortFlag + ` "prompt")
  gopi cli ` + termOnceFlag + ` --tools        the same, with the model's tools enabled
  gopi cli --tools            the same, with the model's tools enabled
  gopi cli --addr HOST:PORT   bind a specific address
  gopi cli ` + termOnceFlag + ` ` + HelpFlag + `             this help (runs nothing)
  gopi cli ` + HelpFlag + `                 the same help

Options:
  ` + termOnceFlag + `, ` + termOnceShortFlag + `      run ONE turn non-interactively: no browser, no
                     terminal, no interaction (` + termOncePromptFlag + ` "…" is the
                     same switch spelled as a named flag). The answer goes to
                     stdout, tool activity to stderr, and the exit status says
                     whether the turn really completed. This is the shape a
                     script (or an agent-authored self-test) drives.
  --tools            tool-enabled turns (the full gopi toolset — the same
                     switch ` + "`gopi web --tools`" + ` takes). Without it the model
                     answers from its own knowledge and calls no tool.
  --addr HOST:PORT   listen address (default ` + webDefaultAddrValue + `).
                     A non-loopback bind is served behind the per-run access
                     token, exactly as ` + "`gopi web`" + ` serves it.

Environment:
  GOPI_CLI_TURN_TIMEOUT_SEC=N   per-turn budget in seconds (default 600;
                                0 = unlimited)
  ` + webToolsEnv + `=1              same as --tools (shared with gopi web)
  ` + termLinksEnv + `=PATH              the text browser to exec (default: the
                                vendored links2 at ` + termVendoredLinksRel + `)

Notes:
  The text-browser carrier is a second VIEW over the running ` + "`gopi web`" + ` engine:
  the same agent, the same single session, the same server-held history, the
  same slash-command table (` + "`/help`" + ` lists it, and a command never reaches the
  model). ` + termOnceFlag + ` drives that same engine without the browser — the same
  turn, reached from a pipe — so a human typing interactively and a script
  running ` + termOnceFlag + ` are asking the same thing of the same code.

  The page is server-rendered and the form is REAL: a text browser submits
  only a NAMED field inside a form with an action, and the field is ONE line
  (Enter submits; a text browser cannot type a newline, and a multi-line body
  is refused with a message rather than silently joined). The settled
  transcript is what the next page load renders — scroll with the browser's
  own keys, then type at the form at the bottom.

  Under --tools each tool call appears LIVE as a ` + "`⚙ kind name digest`" + ` line
  while the turn runs (under ` + termOnceFlag + ` the same lines go to stderr, and the
  raw frames to fd 3). Quitting the text browser ends the chat: the session is
  finalized, and whatever tools ran under --tools, ran — there is no approval
  gate.
`

// termToolsNotice is printed at startup when tool turns are enabled — the same
// trust warning the browser carrier prints, in the terminal's words.
const termToolsNotice = "gopi cli: tools ENABLED — turns run the full gopi toolset with no approval gate (treat as local-shell trust)"
