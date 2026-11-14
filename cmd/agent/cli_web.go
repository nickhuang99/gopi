// cli_web.go — `gopi web` subcommand: a pure-Go, zero-dependency local
// browser chat UI.
//
// This is the "second TUI mode" suggested in devdoc/chat-mode-report.md,
// modeled on the deepseek-harness project's web browser UI (packages/client
// host-carrier: JSON-RPC over HTTP + SSE streaming of model deltas to a
// browser page). Where deepseek-harness uses a Node/TS host + React
// dsh-client-web, gopi stands the whole thing up with ONLY Go stdlib
// (net/http for the server, text/template-free embedded HTML, and
// Server-Sent Events for streaming) — no 3rd-party go.mod deps, fully
// offline-buildable.
//
// Architecture (mirrors Chat-Completion-style SSE exactly):
//
//	GET  /            → the embedded single-file HTML+JS chat page.
//	POST /api/chat    → body {"message":"...", "history":[{"role","content"}...]};
//	                     responds text/event-stream, streaming the model's answer
//	                     token-by-token as `event: delta` lines, then a final
//	                     `event: done` carrying the full result.
//
// Per-session architecture (R1 of devdoc/web-align-p0.md): the SERVER owns
// the authoritative conversation history. Each turn is served by the single
// persistent agent (mirroring `gopi chat`'s chatSession) and afterwards the
// full in-memory transcript is captured from ag.Ctx.Messages to seed the next
// turn — the browser is a pure display and only ever POSTs the new message.
// The request's `history` field is still ACCEPTED (back-compat: old clients
// don't break) but IGNORED as a context source — browser re-shipping was
// lossy (tool_use/tool_result dropped) and could not exceed the 4MiB body
// cap once tool results ride in the transcript.
//
// Tool path (R2): `gopi web --tools` (or env GOPI_WEB_TOOLS) routes each turn
// through Agent.ChatOps — the SAME bounded ReAct loop + full toolset `gopi
// chat --tools` uses, zero new engine. Tool activity streams to the browser
// as SSE `event: tool` frames via Agent.OnToolEvents (mirroring OnText).
// SECURITY: web --tools executes tools on the host with NO approval gate
// (same as `gopi chat --tools`) — treat it as local-shell-level trust.
//
// LAN access (this Epic): the default stays loopback because that is the safe
// posture, but reaching the page from another device is a supported,
// one-step path — bind a non-loopback address (`--addr=0.0.0.0:8090`, or the
// machine's own LAN IP) and the banner prints the ready-to-open LAN URL. A
// non-loopback bind is never served bare: it mints a PER-RUN access token
// (crypto/rand, constant-time compared) that every request must carry, so
// pointing a browser at the box no longer hands a network neighbour an
// unauthenticated tool-executing agent (webTokenGate).

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/fullid"
	"github.com/nickhuang99/gopi/internal/gifgen"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionid"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/tools"
)

// webGroupServerFlags is the flag set that makes a bare `gopi web …`
// invocation the SERVER (not a tool subcommand). Anything else after `web`
// is a group subcommand (search|fetch) — a plain `gopi web` with no args is
// the server (the historical behavior, kept).
func webGroupServerFlags(a string) bool {
	switch {
	case a == webToolsFlag:
		return true
	case a == webThinkingFlag:
		return true
	case strings.HasPrefix(a, "--addr"):
		return true
	case a == "-h" || a == "--help":
		return true
	}
	return false
}

// webGroupAddressOperand reports whether a bare word after `web` is a SESSION
// ADDRESS rather than a tool subcommand — the positional form
// `gopi web <session-id>` that continues an existing conversation (see
// webResumeSeedFor). It is what keeps that address from being reported as
// "unknown subcommand": the dispatcher asks this BEFORE it can refuse.
//
// THE CRITERION IS THE WORD'S SHAPE, NOT THAT IT IS A BARE WORD. An earlier cut
// returned true for ANY non-flag word, so `gopi web no-such-subverb` was routed
// to the server and refused as "cannot continue" — the group's own help page,
// which is the whole point of that refusal path, became unreachable (measured:
// TestWebUsagePagesDoNotPromiseARetiredFlatVerb's third case failed, the page
// never printed). A word that is not address-SHAPED is a misspelt subcommand,
// and saying so (with both doors named) is the more useful answer.
func webGroupAddressOperand(rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if a == webGroupSubcmdSearch || a == webGroupSubcmdFetch {
			return false
		}
		return webLooksLikeAddress(a)
	}
	return false
}

// webLooksLikeAddress reports whether a word could NAME a session — an id
// (canonical uuid, a uuid PREFIX, or a legacy `t-…` row id), a path, or a file
// that exists. It judges SHAPE only and never resolves: the resolver is the one
// that answers "does this session exist", and its refusal (with the reason it
// found nothing) is more useful than this predicate could be.
//
// A word that fails here is not an address at all, so the dispatcher treats it
// as a misspelt sub-verb — the reading an operator with a typo actually needs.
func webLooksLikeAddress(word string) bool {
	switch {
	case word == "":
		return false
	case strings.ContainsRune(word, os.PathSeparator):
		return true
	case fullid.IsUUID(word), fullid.TruncatedUUID(word):
		return true
	case strings.HasPrefix(word, sessionid.TaskIDPrefix):
		return true
	}
	// A raw-log path the caller spelled without a separator (a bare file name)
	// is still an address — but only if it IS one on disk.
	_, statErr := os.Stat(word)
	return statErr == nil
}

// webGroupSubcmdSearch / webGroupSubcmdFetch are the web group's TOOL
// sub-verbs — the second level of `gopi web` that is the web_search/web_fetch
// tools rather than the browser chat server. Named constants (Rule 1b): the
// dispatcher below, the unknown-subcommand message, the tab-completion set and
// the retired flat verbs' pointers all read THESE, so a rename cannot leave one
// of them naming a word nothing dispatches.
const (
	webGroupSubcmdSearch = "search"
	webGroupSubcmdFetch  = "fetch"
)

// webGroupSubcommand reports whether the argv tail after `web` is a tool
// subcommand (search|fetch) — the second level of the web group. When it is,
// it runs the subcommand and returns true.
func webGroupSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case webGroupSubcmdSearch:
		runWebSearchSubcommand(args[1:])
		return true
	case webGroupSubcmdFetch:
		runWebFetchSubcommand(args[1:])
		return true
	}
	return false
}

// isWebSubcommand reports whether argv begins with the `web` group verb.
// The server runs in the foreground on the current terminal (Ctrl+C aborts
// it). Dispatch rule: `gopi web search|fetch …` are the web tool group;
// anything else (bare `gopi web`, server flags) starts the web chat server.
func isWebSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == CliVerbWeb {
		rest := args[1:]
		if len(rest) > 0 && !webGroupServerFlags(rest[0]) {
			if webGroupSubcommand(rest) {
				return true
			}
			// A bare word that is not a tool subcommand is a SESSION ADDRESS:
			// `gopi web <session-id>` continues an existing conversation. It is
			// the server (with a seed), never an unknown subcommand — the
			// refusal for an address that resolves to nothing comes from the
			// resolver, with the reason it found nothing.
			if webGroupAddressOperand(rest) {
				os.Exit(runWeb(rest))
			}
			// Unknown web subcommand — a usage error, NOT the server and
			// NOT an agent-loop fallthrough.
			fmt.Fprintf(os.Stderr, "gopi web: unknown subcommand %q — want %s|%s\n\n%s", rest[0], webGroupSubcmdSearch, webGroupSubcmdFetch, webGroupUsage)
			os.Exit(1)
		}
		os.Exit(runWeb(rest))
	}
	return false
}

// webGroupUsage is the second-level help for `gopi web <subcommand>` —
// printed on an unknown web group subcommand. The bare `gopi web` server
// help stays in webUsage (runWeb -h).
const webGroupUsage = `GoPi web group — the web tools plus the chat server.

Usage:
  gopi web search "<query>" [--method M] [--count N]  web_search (gopi web search -h)
  gopi web fetch <url> [--max-chars N]               web_fetch tool (gopi web fetch -h)
  gopi web [SESSION-ID] [server flags]               web chat UI server (gopi web -h)

The flat spellings gopi web_search / gopi web_fetch are RETIRED and refused by
name (exit 1); their dash twins gopi web-search / gopi web-fetch are refused the
same way, so no non-command spelling of these tools is ever spent as a prompt.
The live spelling is ` + "`gopi " + CliVerbTool + " " + CliVerbWeb + " " + webGroupSubcmdSearch + "`" + ` /
` + "`gopi " + CliVerbTool + " " + CliVerbWeb + " " + webGroupSubcmdFetch + "`" + `; the grouped forms above
are the same commands under the web group.
`

const webUsage = `gopi web — serve a local browser chat UI (pure-Go, zero deps)

Usage:
  gopi web [SESSION-ID] [--addr :PORT] [--tools] [--thinking-live]

  SESSION-ID continues an EXISTING session in the browser — the same address
  ` + "`gopi chat <session-id>`" + ` takes (a session id, a task id, or a raw-log path).
  The conversation on disk is seeded (its history is replayed to the model at
  turn one), the run appends to that session's own JSONL and raw log, and the
  switches it recorded at start-up are restored for the ones this command line
  does not state (each restore is reported). The run directory is locked for the
  duration, so a browser session and ` + "`gopi session resume`" + ` cannot execute
  one directory at once. Omit it for a fresh conversation.

Serves an embedded chat page over HTTP. Open the printed URL in a browser and
chat with the model in a ChatGPT-style page. Answers stream in live over
Server-Sent Events. The server holds the authoritative conversation history:
each turn only sends the new message (a legacy ` + "`history`" + ` field is accepted
but ignored).

Flags:
  %s :PORT   listen address (default %s). GOPI_WEB_ADDR env overrides.
                 Bind a non-loopback address (%s=0.0.0.0:%d, or this machine's
                 own LAN IP) to open the page from another device: the banner
                 then prints the ready-to-open LAN URL. Requests from THIS host
                 need no token; other machines need the per-run access token the
                 banner carries as ?%s=<token> — see LAN access below. The
                 joined spelling %s=HOST:PORT works as well.
  --tools        tool-enabled turns (Agent.ChatOps, the full gopi toolset —
                 same as ` + "`gopi chat --tools`" + `). GOPI_WEB_TOOLS env overrides.
                 NOTE: executes tools on this host with no approval gate —
                 loopback-only by default; treat as local-shell-level trust.
  %s  stream the model's live reasoning as ` + "`event: thinking`" + ` frames
                 (one raw fragment per frame, no marker bytes), so a client can
                 draw the model's own thinking while it works. OFF by default
                 (env %s); the same switch ` + "`gopi ui`" + ` takes. With it off
                 NOTHING is installed on the engine, so the wire carries not one
                 extra frame and the default page is unchanged.

LAN access: the default is loopback (safe posture: tool turns have no
approval gate). For other-device access, ` + "`gopi web %s=0.0.0.0:%d`" + ` (or the
machine's own IP, e.g. %s=192.0.2.10:8090) serves the same page on the LAN.
Such a bind mints a per-run access token; the banner prints every URL with
it (http://<ip>:PORT/?%s=<token>) — open that one, no other step needed.
Requests from THIS host are served without the token (a browser here is the
same operator who holds the shell), so http://localhost:%d/ and the host's
own IP just work. The token is a WEB access token — never the model API key
(that comes from the host config, as for any other gopi process). %s env pins
the token instead of minting one; %s=%s demands the token locally as well.

Slash commands in the page (R4): type / for the local command table (task,
log, text2img, code-search, memory) — executed by the server, never the
model; results render as command nodes, not assistant bubbles.
Input-trigger menus (B9): typing a slash lists the command table (filtered as
you type); typing at-sign lists files from the search root. Up/Down move,
Enter/Tab complete (Enter never sends while the menu is open), Esc closes.
Markdown (B8): fenced code blocks and inline code render as pre/code — every
node is built with textContent, so markup in a model answer stays inert text.
Turn queue (B12): a message typed while a turn runs is shown as a QUEUED entry
and delivered as its own turn when the active one finishes (in order, never
dropped). Alt+Enter (or Alt-click Send) marks it STEERING: it is sent to the
run in flight, whose answer carries on, instead of becoming a later turn.
Esc stops the turn that is running.
Images (R5): attach up to %d (≤32 MiB each; png/jpeg/gif/webp) via the
paperclip button, clipboard paste, or drag-drop. Image turns run the vision
model (settings vision_model / GOPI_VISION_MODEL).
Turn budget (B12): each turn is bounded by ` + "`" + `GOPI_WEB_TURN_TIMEOUT_SEC` + "`" + `
seconds (default 0 = unlimited) — a bound exists so a stalled upstream
releases the turn lock for the next request instead of queueing every later
turn behind it forever.
`

// webChatRequest is the browser→server contract for one chat turn. History
// is the legacy field: still decoded (old clients must not break on decode)
// but NOT used as a context source — the server owns the transcript (R1).
// Images (R5) is the picture half of the turn: each entry is one base64
// image + media type, validated by validateWebImage and attached to the
// turn's user message (vision model selected for that turn only).
type webChatRequest struct {
	Message string `json:"message"`
	// QueueID is the id /api/chat/queue handed this message when it was
	// registered as waiting (cli_web_queue.go). It is OPTIONAL and carries
	// `omitempty`: absent means "this POST is the first the server hears of the
	// message" (the ordinary fresh submit), which is exactly the zero value's
	// meaning here — a POST that arrives without one gets its own id at the
	// moment it learns it must wait.
	QueueID int              `json:"queue_id,omitempty"`
	History []webChatMessage `json:"history,omitempty"`
	Images  []webChatImage   `json:"images,omitempty"`
}

// webChatImage is one uploaded picture: base64 data + Anthropic media type.
// (Field names mirror the Anthropic wire's image source so a browser can
// forward what it read off a data URI nearly verbatim.)
type webChatImage struct {
	MediaType string `json:"media_type"`
	DataB64   string `json:"data_b64"`
}

// webChatMessage is one accumulated conversation message the client re-ships
// with each turn (mirrors deepseek.Message but only carries what the browser
// sees: role + text).
type webChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// webServer holds the per-process config for the `gopi web` HTTP server.
type webServer struct {
	addr string
	// tools enables the tool path (R2): --tools flag or GOPI_WEB_TOOLS env.
	// When true every turn runs Agent.ChatOps (full toolset ReAct loop, same
	// as `gopi chat --tools`); when false turns run the plain tool-free
	// Agent.Chat. SECURITY: tool turns execute on this host with no approval
	// gate — loopback-only, local-shell-level trust (see devdoc).
	tools bool

	// agent is the SINGLE agent that serves the entire web chat run (the
	// Epic's single-session ask, mirrored from `gopi chat`): created once at
	// server startup with PersistChatSession=true and reused for EVERY
	// /api/chat turn, so the whole web conversation accumulates into ONE
	// session id + ONE session JSONL + ONE raw log (each turn appends, never
	// a fresh session id + log per turn — the pre-Epic per-turn behavior).
	// It is closed once at server shutdown via CloseChatSession, writing the
	// single session_end.
	agent *agent.Agent

	// newWebAgent builds the single persistent agent at server startup.
	// Defaults to the package-level newAgent (real model). Tests inject a
	// stub pointing at a mock streaming server.
	newWebAgent func() (*agent.Agent, error)

	// startedAt records server startup (for the final session_end + task
	// ledger elapsed stats); iterCount/totalTokens accumulate across every
	// /api/chat turn for the same stats — mirroring `gopi chat`'s
	// sessionStart/iterCount/totalTokens in runChat.
	startedAt   time.Time
	iterCount   int
	totalTokens int

	// chatRegistered latches that this web chat run ensured its CHATS-table
	// row (M4.6 钩子分流 — the tasks-ledger [auto] chat rows are retired).
	// Kept for the struct-shape parity with chatSession; the latch now lives
	// on the agent (Agent.chatRegistered, chatregister.go).
	chatRegistered bool

	// history is the SERVER-OWNED authoritative conversation transcript
	// (R1): captured from ag.Ctx.Messages after every turn and used as the
	// next turn's seed — the exact mirror of `gopi chat`'s chatSession
	// (cli_chat.go). The browser never ships context back; it only POSTs the
	// new message. Guarded by mu (same serialized domain as the turns).
	history []deepseek.Message

	// mu serializes access to the shared persistent agent across concurrent
	// /api/chat requests. Agent.Chat mutates unsynchronized state on the agent
	// (Ctx.Messages, Cost), so a single agent (the Epic's one-session design)
	// must be serviced one turn at a time — mirroring `gopi chat`'s strictly
	// sequential REPL. Multiple browser tabs/turns queue here, each running
	// fully (SessionLog's own mutex covers its append), never two turns
	// interleaving on the same session at once.
	mu sync.Mutex

	// shuttingDown flips before the shutdown finalizer takes the mutex, so an
	// in-flight-or-later /api/chat stops accepting new work instead of
	// queueing behind (or deadlocking with) the shutdown close. 503s are
	// better than a hung turn racing the session_end write.
	shuttingDown bool

	// queue is the SERVER-SIDE record of the messages waiting on s.mu
	// (cli_web_queue.go): arrival order, the typed placement, each queued
	// entry's position, and whether it can still be withdrawn. Its zero value
	// is a usable empty queue, so no constructor is needed and every existing
	// `&webServer{...}` literal keeps working. It has its OWN mutex: it is
	// mutated by a request that is WAITING on s.mu, which by definition cannot
	// hold it.
	queue webQueue

	// voice is the microphone pipeline (web_voice.go). Nil means PRODUCTION —
	// webVoiceProduction, resolved by voicePipeline(). A test injects its own so
	// the two voice endpoints can be driven on a host with no sound card at all,
	// which is the only way to exercise them deterministically (Rule 10.4).
	voice webVoicePipeline

	// voiceState is the mutable half of the voice carrier: at most ONE recording
	// at a time (one microphone, one start/stop pair), plus the sink it will be
	// recognised by. A pointer so the zero value of webServer needs no
	// constructor and a test can install a recording without one.
	voiceState webVoiceState

	// files is the file_id cache for the Files API transport (cli_web_files.go):
	// a picture uploaded on one turn is referenced by id on every later turn
	// instead of travelling again as base64. A VALUE, not a pointer: its zero
	// value is a working cache, so every constructor (and every test that builds
	// a server with a struct literal) gets one without being edited.
	files webFilesCache

	// visionModel resolves the settings/env vision model once at startup
	// (R5): image turns switch the shared agent's Model to this for the
	// duration of the turn and restore it after (inside the s.mu turn
	// domain). Empty = no vision model configured → image turns fail fast
	// with a 4xx telling the user exactly that.
	visionModel string

	// lastTermWrites / lastTermFlushes / lastTermDeltaWrites record what the
	// LAST /cli/chat turn put on the wire: HTTP body writes, explicit flushes,
	// and how many of those writes carried model deltas. They exist because
	// the terminal carrier's whole cost model is "how often does the document
	// get re-laid-out" — a number the epic requires the report to measure
	// rather than assert, and a regression (a flush per token) is invisible in
	// any output-only assertion. Written under s.mu inside the turn, so a
	// reader sees one turn's counts, never a torn pair.
	lastTermWrites      int
	lastTermFlushes     int
	lastTermDeltaWrites int

	// thinking is the LIVE-REASONING switch for this server's turns: when true
	// each turn streams `event: thinking` frames carrying the model's raw
	// reasoning (see webThinkingEnabled for the one place the env is read and
	// webThinkingFlag for the flag spelling). It is a field rather than an env
	// read inside handleChat for the same reason `tools` is: the decision is
	// made ONCE at startup from the operator's switches, and a turn's behaviour
	// must not depend on when something re-read the environment.
	//
	// DEFAULT FALSE — an unset deployment streams not one extra frame, which is
	// what keeps the default screen byte-identical to the pre-epic one.
	thinking bool

	// carrier names the FACE this server answers for, in internal/tools'
	// ChatPolicyCarrier* vocabulary. It is what the chat write boundary degrades a
	// `prompt` answer through: this page's own banner already declares that tool
	// turns run with no approval gate, so asking the browser operator to approve a
	// line they typed is a question with no reader
	// (internal/tools/chat_policy_carrier.go states the measurement).
	//
	// EMPTY MEANS webCarrierName: this struct IS the web engine, and `gopi ui` /
	// `gopi cli` reuse it as their own engine while declaring their own name — a
	// zero value must therefore never silently become the console's behaviour.
	carrier string

	// noticeHub fans a SERVER-started turn's frames out to every page watching
	// /api/events (cli_web_notice.go). Lazy: the zero value of webServer must
	// stay usable, so it is created by ensureNoticeHub on first use and its
	// mutex is a value.
	noticeHub   *webNoticeHub
	noticeHubMu sync.Mutex

	// noticeLoop is the ONE goroutine this carrier adds — the idle wait point
	// for a session-inbox delivery. Created after the agent exists and JOINED at
	// shutdown (startInboxNoticeLoop / stopInboxNoticeLoop), so nothing outlives
	// the server and no schedule, file or socket is created (Rule 35).
	noticeLoop      *webNoticeLoop
	noticeStartOnce sync.Once
	noticeStopOnce  sync.Once

	// state is the SERVER-SIDE STATUS the page renders (cli_web_state.go): which
	// of idle / responding / tool_running(<name>) the assistant is in, plus
	// whether the turn can be interrupted. A VALUE, not a pointer, so every
	// constructor — and every test that builds a server with a struct literal —
	// gets a working machine without being edited; its zero value is IDLE.
	state webStateMachine

	// turnBaseline is how many turn-OPENING rows the session record already
	// held when this server started counting — 0 for a fresh conversation, the
	// resumed record's own count for `gopi web <session-id>`.
	//
	// IT IS SET ONCE, AT STARTUP, FROM THE RECORD (cli_web_turn.go's
	// webTurnBaseline), because that is the only moment the two halves of the
	// number are separable: after the first turn the file holds this run's own
	// rows too, so "how much of turn 20 was here before me?" is no longer
	// answerable. Without it a continued conversation would number its first
	// new turn 1 while the record calls it 21 — a banner that is worse than no
	// banner, because it looks authoritative.
	turnBaseline int
}

// thinkingStream reports whether THIS turn should stream live reasoning.
// A method rather than a field read so the gate has one name at its use site
// and the field stays private to the startup decision.
func (s *webServer) thinkingStream() bool { return s.thinking }

// runWeb starts the blocking HTTP server on the given address (registered via
// the CLI dispatch, so the process exits with the server). It returns the
// process exit code so the dispatcher can os.Exit on it.
//
// Shutdown paths (bug003 fix): (1) normal halt / server error → the finalizer
// runs after http.Serve returns, exactly as before; (2) SIGINT/SIGTERM (Ctrl+C
// or kill) → the signal handler shuts the HTTP server down so the finalizer
// runs too — the one session_end + ledger close are no longer leaked on the
// common interactive exit paths. SIGKILL remains best-effort-impossible (the
// OS reaps the process without running any Go code; documented limitation).
func runWeb(args []string) int {
	addr := webDefaultAddr()
	toolsEnabled := webToolsEnabled()
	thinkingEnabled := webThinkingEnabled()
	opts, err := webParseFlagsServer(args, addr, toolsEnabled, thinkingEnabled)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi web: %v\n", err)
		return 1
	}
	addr, toolsEnabled, thinkingEnabled, wantHelp := opts.Addr, opts.Tools, opts.Thinking, opts.Help
	if wantHelp {
		fmt.Printf(webUsage,
			webAddrFlag, addr, webAddrFlag, webDefaultPort, webTokenQueryKey, webAddrFlag,
			webThinkingFlag, webThinkingEnv,
			webAddrFlag, webDefaultPort, webAddrFlag, webTokenQueryKey, webDefaultPort,
			webTokenEnv, webLocalTrustEnvName, webLocalTrustOffValue,
			webMaxImagesPerRequest)
		return 0
	}
	if addr == "" {
		addr = webDefaultAddr()
	}

	// CONTINUE AN EXISTING SESSION when a positional address is given
	// (`gopi web <session-id>`). The seed is resolved BEFORE anything is bound
	// or written: an address that names no session must refuse here, with the
	// reason, instead of after a server has been half-started. A fresh run
	// (no positional) keeps the zero seed and every path below unchanged.
	var seed webResumeSeed
	if len(opts.Positional) > 0 {
		if len(opts.Positional) > 1 {
			fmt.Fprintf(os.Stderr, "gopi web: one session address only (got %q) — the words after the first are not part of it\n", strings.Join(opts.Positional, " "))
			return 1
		}
		seed, err = webResumeSeedFor(opts.Positional[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		// The lock belongs to THIS process for as long as the server runs, on
		// EVERY exit path below (listen failure included) — so it is released by
		// the one defer the function owns, not by each early return.
		defer seed.Lock()
		// THE CONTINUED SESSION'S RECORDED SWITCHES FILL THE SILENCE, this run's
		// own flags win — one precedence rule, and each restore is reported.
		toolsEnabled, thinkingEnabled = webApplyRecordedOptions(seed.RecordedOptions,
			func(w string) bool { return webSwitchStated(args, w) }, toolsEnabled, thinkingEnabled)
		// THE CAPABILITY LOSS IS REPORTED AFTER THE RESTORE, never before: a
		// warning decided at the seed printed "tools OFF" on one line and
		// "restored --tools" on the next (the chat face's measured case). Here
		// the two sentences can no longer contradict each other.
		webWarnSeededToolsMismatch(seed.ToolDefs, toolsEnabled)
	}

	// THE CONFIGURATION IS RESOLVED HERE, ONCE FOR THE WHOLE SERVICE.
	//
	// WHY THIS LINE HAD TO EXIST (measured 2026-11-12 on this host, the same
	// defect `gopi chat` had — commit 6fd8b134): main() resolves the
	// configuration AFTER the subcommand chain, and `isWebSubcommand` exits from
	// INSIDE that chain, so resolveRunConfig was never reached on this path and
	// config.ApplyEnv never ran. Not one settings.json knob reached the process
	// environment, and internal/agent reads reasoning_effort / thinking from the
	// ENVIRONMENT only — so every web turn went out with thinking=disabled while
	// a bare-prompt run of the same binary sent the configured values.
	//
	// WHY THE POSITION IS THIS ONE. It is AFTER the argv walk, the usage return
	// and the session seed's refusal (a usage page and a refusal must not mutate
	// the environment) and BEFORE the profile lock, the listener and the agent —
	// so the system prompt, the client and every request the server assembles
	// see the resolved values.
	//
	// WHY ONCE, HERE, AND NOT IN newWebAgent. This is a SERVER: newWebAgent is a
	// factory that a resumed conversation or a second engine can call again.
	// Resolution hung there would be a per-agent settings read whose second run
	// could disagree with the first, and the environment it publishes is
	// process-wide — it belongs to the start of the process's work, not to each
	// construction of a client.
	//
	// WHAT IT DELIBERATELY DOES NOT DO: it does not re-implement the precedence
	// (flag > env > settings > default) or any of the five steps. Those have ONE
	// home (run_config_resolution.go) and this is a call into it; see
	// service_config_resolution.go for the argv reduction a verb face needs and
	// for why a failure speaks in web's voice instead of printing the run path's
	// usage block.
	if code := resolveWebRunConfig(args); code != serviceConfigExitOK {
		return code
	}

	// ONE SERVER PER PROFILE. Taken BEFORE the listener and released on every
	// exit path below (the defer), so a second `gopi web` on this profile
	// refuses instead of executing the same conversation twice. Browsers are
	// NOT limited by this: they are clients of the one server.
	releaseServerLock, lockErr := webTakeServerLock()
	if lockErr != nil {
		return 1
	}
	defer releaseServerLock()

	// Verify the address binds before we print "listening" (fail fast on a
	// port conflict rather than leaving a half-started server).
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi web: cannot listen on %s: %v\n", addr, err)
		return 1
	}

	// THE CHAT WRITE BOUNDARY IS FROZEN BEFORE THE FIRST TURN. This face
	// executes tools (--tools), so every write/edit a turn makes is judged by
	// the pair frozen HERE — the operator's ```chat-policy fence plus the
	// startup posture. Without this call the boundary judged against the
	// package defaults (`prompt *`, on-request), so a genuine tool round on a
	// web chat could never land a write no matter what the operator declared
	// (measured 2026-10-22, task 01a16225: the round returned
	// `[NEEDS OPERATOR APPROVAL — command NOT run]` and the target file was
	// never created). There is no `--policy` flag on this face, so the posture
	// comes from the channels it does read: GOPI_CHAT_POLICY, settings.json
	// "chat_policy" (and the legacy chat_op_auto mapping), then the default.
	chatFreezeChatPolicyState(chatFacePrefixWeb, "")

	// THE WEB CONSOLE'S OWN DEFAULT FOR THE SESSION BUDGET, published HERE:
	// after the command line is resolved (so `-h` and every refusal have already
	// returned and a usage query changes nothing) and before the agent — and
	// therefore the system prompt — exists. Every web turn goes through
	// Agent.ChatOpsOnCarrier, i.e. the SAME task prompt assembly a dispatched
	// worker gets, so without this call the console inherits
	// internal/agent's 60-minute TASK ceiling and teaches it to the model
	// (measured 2026-11-07, task 01a1b498: the operator's PA console was told
	// "session budget: 55m28s elapsed of 1h0m0s (92% used)" at the 55th minute of
	// an unbounded conversation). The rule, the never-override behaviour and the
	// failure line are the chat's own — chat_session_budget.go is the ONE
	// implementation (Rule 2/14); this is its second entry point.
	//
	// The table is RESOLVED, not guessed: this face resolves no colour capability
	// of its own (it has none — its transcript is the browser), so it asks the
	// ONE existing resolution with the monochrome spelling. The notice is a
	// start-up remark about the surface, and termToolStylesFor(ColourOff) is the
	// table that renders it as exactly the historic plain bytes.
	publishWebSessionBudgetDefault(termToolStylesFor(lineedit.ColourOff))

	// THE AGENT IS BUILT THROUGH THE SEED when a session is being continued: a
	// resume passes an agent.RunIdentity to newAgentResuming (the SAME seam
	// `gopi chat <id>` uses), so the session id, the JSONL and the raw log are
	// the ones on disk instead of a fresh uuid — which is the whole point
	// (a resumed web conversation must grow the conversation it continues, not
	// mint a second one beside it).
	srv := &webServer{addr: addr, tools: toolsEnabled, thinking: thinkingEnabled}
	if seed.SessionID != "" {
		srv.newWebAgent = func() (*agent.Agent, error) { return newAgentResuming("", seed.identity()) }
	} else {
		srv.newWebAgent = func() (*agent.Agent, error) { return newAgent("") }
	}

	// ONE persistent agent for the whole web chat run (mirrors `gopi chat`'s
	// single-session design): created up front with PersistChatSession=true
	// and reused across every /api/chat turn, so the web conversation is one
	// session id + one session JSONL + one raw log — each turn *appends* to
	// the same files instead of spawning a fresh session per turn (the
	// pre-Epic behavior). The single session_end + task ledger row are
	// written once at server shutdown via CloseChatSession/FinishSessionTask.
	ag, err := srv.newWebAgent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi web: failed to create agent: %v\n", err)
		return 1
	}
	ag.PersistChatSession = true
	srv.agent = ag
	srv.startedAt = time.Now()
	// THE TURN BANNER'S FIXED HALF, measured from the record NOW — before this
	// run writes a single row. A continued conversation's record already holds
	// its turns (say 20), so its next turn is 21; a fresh one holds none, so its
	// first turn is 1. Measured one line later it would be unanswerable: after
	// `webRecordResumeOptions` (and certainly after turn one) the file contains
	// this run's own rows, and "how many of these were here before me?" has no
	// answer in the file any more. cli_web_turn.go owns the counting.
	srv.turnBaseline = webTurnBaseline(ag.SessionLog.Path())
	// SEED THE SERVER-OWNED TRANSCRIPT. The page renders s.history and every
	// turn seeds the model from it (R1), so a continued conversation must START
	// with the history it had — otherwise "continue" would mean "reopen the
	// files but forget the conversation", which is the defect this seed exists
	// to remove. The checkpoint's own messages are used verbatim; the agent had
	// already been handed the same identity, so its Ctx.Messages and this slice
	// agree at turn one.
	if len(seed.History) > 0 {
		srv.history = append([]deepseek.Message{}, seed.History...)
	}
	// R5: resolve the vision model once at startup (settings vision_model >
	// GOPI_VISION_MODEL env > default). Image turns switch the shared agent
	// to it for the turn and restore afterwards — inside the s.mu domain.
	srv.visionModel = deepseek.ResolveVisionModel(cfgForVision())
	// RECORD THIS RUN'S START-UP SWITCHES BEFORE THE FIRST TURN, into its OWN
	// session log — the record a later `gopi web <id>` continues from
	// (webRecordResumeOptions; the writer is the chat face's, shared — Rule 14).
	// It goes here rather than after srv.history is seeded because the record
	// describes THIS RUN's capabilities, and it belongs on disk before a turn
	// can start: the run it exists for dies mid-turn.
	webRecordResumeOptions(ag, webRecordedSwitches(toolsEnabled, thinkingEnabled))
	printLogPaths(ag)
	// REGISTER THIS RUN IN THE SLOT REGISTRY (chat_session_slots.go) — the
	// chat face's ARM-POINT, called here because a web run is the same kind of
	// surface and the sink has no other way to reach it. The environment
	// channel (`GOPI_CHAT_SURFACE_ID`) travels DOWN to this run's children
	// only; `notify.cmd` is exec'd by the LEDGER's own machinery — the worker
	// tick, the merge loop, a third shell — none of which is a descendant of
	// this web server, so without a slot file the task events it broadcasts
	// (`claimed` / `completed` / `failed`) address a chat that does not exist
	// and the operator's browser console receives nothing.
	//
	// It is the SAME call `gopi chat` makes, not a second implementation
	// (Rule 14): slot choice (own entry → free → reclaimable → refuse), the
	// identity spelling and the `--replay` refusal all stay in that one file.
	// Nothing new ticks and nothing is cleaned up here (Rule 35) — a stale slot
	// costs one refused `gopi notify --session`, which the sink owns.
	//
	// Placement: after the agent exists and the session log path is known, and
	// before the first turn can start — this is where both halves of the
	// identity (pid, session directory) are facts. The session directory is
	// taken from the SESSION RECORD's container, the same operand
	// `gopi notify --session` resolves, so `gopi web <id>` (a continuation)
	// registers the id's own directory rather than a fresh one.
	//
	// A FAILURE IS NOT FATAL (Rule 15): the web console is fully usable without
	// a registry entry; what the operator loses is notifications, and the call
	// itself says so on stderr. Never abort a served UI over a missing
	// notification channel.
	chatSessionSlotRegister(ag.SessionLog.Path(), termToolStylesFor(lineedit.ColourOff))

	mux := srv.webChatHandler()

	// LAN access gate: a non-loopback bind is network-reachable, so it must
	// not be served bare (a neighbour holding the URL would drive an agent
	// that executes tools with no approval gate). webGatedHandler owns that
	// decision in ONE place; the loopback default mints nothing and passes
	// straight through, so the local-shell-trust posture is unchanged.
	//
	// A browser ON this host is that same local shell, only over HTTP: it must
	// not be asked for the token either. webLocalTrust carries the evidence
	// (the peer address of a request is one of THIS machine's own addresses),
	// so a gated bind no longer breaks the operator's own browser.
	trust := webLocalTrust(addr)
	handler, token, err := webGatedHandler(addr, mux, trust)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi web: %v\n", err)
		return 1
	}

	// THE IDLE WAIT POINT (cli_web_notice.go): a `gopi notify --session <id>
	// --message <text>` delivered by ANOTHER process must become a turn of THIS
	// server, with no page POST — the browser carrier's twin of the console's
	// parked select. Started only now (the agent exists) and joined at every
	// shutdown path below, so nothing outlives the server (Rule 35).
	srv.startInboxNoticeLoop()

	webPrintListening(addr, ln, token, trust)
	if token != "" {
		fmt.Fprintf(os.Stderr, "%s\n", webLANWarning)
		if trust {
			fmt.Fprintf(os.Stderr, "%s\n", webLocalTrustNotice)
		}
	}
	if srv.tools {
		if token != "" {
			fmt.Fprintf(os.Stderr, "%s\n", webToolsLANWarning)
		} else {
			fmt.Fprintf(os.Stderr, "gopi web: tools ENABLED — turns run the full gopi toolset with no approval gate (loopback only, local-shell trust)\n")
		}
	}

	// bug003 fix: SIGINT/SIGTERM → graceful shutdown. Stopping the HTTP
	// server makes Serve return, and the session finalizer runs on BOTH exit
	// paths (normal halt and signal) — the ONE session_end + task-ledger
	// close are no longer leaked on Ctrl+C / kill. SIGKILL remains a
	// documented best-effort-impossible case (the OS reaps the process
	// without running any Go code).
	httpSrv := &http.Server{Handler: handler}
	srvErr := make(chan error, 1)
	go func() { srvErr <- httpSrv.Serve(ln) }()

	sigCh := make(chan os.Signal, 1)
	// THE SET COMES FROM THE RECORDER (epic 01a15bca): SIGHUP was missing, so a
	// terminal going away recorded a provenance line and then left this server
	// running with its CHATS row open — the recorder declines to re-raise for the
	// "web" mode, so nothing else would ever act on it.
	signal.Notify(sigCh, signalrec.GracefulStopSignals()...)
	select {
	case err := <-srvErr:
		// Normal halt (ErrServerClosed) or an internal server error.
		// The notice goroutine is joined FIRST: it may be mid-turn holding s.mu,
		// and the finalizer takes that same lock (the deadlock
		// abandonInFlightTurn exists to prevent, measured in epic 01a16dab).
		srv.shuttingDown = true
		srv.abandonInFlightTurn()
		srv.stopInboxNoticeLoop()
		srv.finalizeChatSession(err == nil || errors.Is(err, http.ErrServerClosed))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "gopi web: server error: %v\n", err)
			return 1
		}
		return 0
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "gopi web: %v — shutting down, finalizing chat session…\n", sig)
		// Stop accepting new turns; wait (bounded) for the in-flight one.
		srv.shuttingDown = true
		// JOIN the notice goroutine before the finalizer takes s.mu: a notice
		// turn holds that lock for its whole model round-trip, and its own loop
		// exits on the stop channel this closes.
		srv.stopInboxNoticeLoop()
		// ABANDON THE IN-FLIGHT TURN BEFORE WAITING FOR IT (measured leak, epic
		// 01a16dab). Without this the finalizer below waits for s.mu, which the
		// running turn holds for the WHOLE model round-trip — and a round the
		// peer has not answered yet never ends by itself. The rig measured a
		// SIGTERMed server still alive 30s later, and its goroutine dump named
		// both sides of the wait:
		//
		//	goroutine 1 [sync.Mutex.Lock]: finalizeChatSession (cli_web.go:1585)
		//	goroutine 9 [chan receive]:    chatOnceAttempt (loop.go:5907)
		//
		// — so Ctrl+C during a turn degraded to SIGKILL, and a SIGKILLed process
		// runs no Go code: no session_end, an open CHATS row, and the tool round's
		// child (a bash command) left orphaned. The interrupt cancels the run's
		// own context, the round unwinds, and the turn lock is released by the
		// normal path.
		srv.abandonInFlightTurn()
		// A live RECORDING is a device held open by a page that may never come
		// back, so it is released here rather than left to the process exit (Rule
		// 15). It is not a turn: the capture is closed, its goroutine ends, and no
		// engine call is made for a recording nobody is waiting for.
		srv.stopVoiceRecording()
		srv.stopLiveVoiceRecording()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), webShutdownGrace)
		_ = httpSrv.Shutdown(shutdownCtx) //stylegate:ignore embedded server shutdown; the serve goroutine's error is reported separately
		cancel()
		// Drain Serve's return so nothing is left blocked on the channel.
		select {
		case <-srvErr:
		case <-time.After(webShutdownGrace):
			// Grace expired (e.g. a stuck SSE conn) — force-close everything;
			// the in-flight turn then unwinds and the finalizer proceeds.
			_ = httpSrv.Close() //stylegate:ignore final close right after Shutdown; the server is already stopping
			<-srvErr
		}
		srv.finalizeChatSession(true)
		return 0
	}
}

// webDefaultAddr resolves the default listen address: GOPI_WEB_ADDR env wins,
// else the constant default. (Rule 1: the literal is a named constant, and
// Rule 2: exactly one place reads the env override.)
func webDefaultAddr() string {
	if v := os.Getenv("GOPI_WEB_ADDR"); v != "" {
		return v
	}
	return webDefaultAddrValue
}

// webDefaultAddrValue is the single source of truth for the default address.
// Loopback-only by DEFAULT (the documented posture — gopi web executes tools
// with no approval gate, so a LAN-reachable default is a footgun): a LAN
// exposure requires the explicit `--addr 0.0.0.0` / GOPI_WEB_ADDR opt-in
// (t-1788053264) and is then gated by the per-run access token
// (webNewAccessToken / webTokenGate) — the opt-in is LOUD, never silent.
const webDefaultAddrValue = "127.0.0.1:8090"

// webDefaultPort is the port half of webDefaultAddrValue, used for the
// flag-help text (a single source of truth: the usage line can then never
// quote a port the default does not use).
const webDefaultPort = 8090

// webCarrierWeb / webCarrierUI prefix every notice of the carrier that prints
// it. The name is DERIVED from the verb constant (Rule 14: one definition of
// the word), so a message can say `gopi ui` only if the verb is really `ui` —
// this is what lets `gopi ui` share gopi web's warning TEXT without lying about
// which surface the operator is looking at.
const (
	webCarrierWeb = "gopi " + CliVerbWeb
	webCarrierUI  = "gopi " + CliVerbUI
)

// The CARRIER names below are the same faces as strings the TURN carries
// (Agent.ChatCarrier), taken from their ONE definition in internal/tools so the
// notice printed on the banner and the degradation the write boundary applies can
// never name different surfaces (Rule 14).
const (
	webChatCarrierWeb  = tools.ChatPolicyCarrierWeb
	webChatCarrierUI   = tools.ChatPolicyCarrierUI
	webChatCarrierCLI  = tools.ChatPolicyCarrierCLI
	webChatCarrierTerm = tools.ChatPolicyCarrierTerminal
)

// webChatCarrier is the face THIS server answers for: an unset field means the
// web engine's own name (see webServer.carrier).
func (s *webServer) webChatCarrier() string {
	if s.carrier == "" {
		return webChatCarrierWeb
	}
	return s.carrier
}

// webPrintListening prints the reachable URL(s) for the running web server.
// When the listen address has an empty host (":8090" → all interfaces), the
// printed form "http://:8090" is NOT a valid browser URL — a browser cannot
// resolve an empty hostname, so instead we print the real addresses a browser
// can reach: the loopback interface(s) plus any non-loopback (LAN) IPs.
// When an explicit host was given, we print that resolved address directly.
//
// token (when non-empty — a non-loopback bind) is appended to every URL as
// "?t=<token>" so the printed LAN URL is openable as-is: one step from
// another device, with the access gate satisfied by the URL the operator
// copied. A URL that points at THIS host is printed without the token when
// localTrust is on — it works as printed, and printing a token there would be
// a false instruction (see webTokenURL).
func webPrintListening(addr string, ln net.Listener, token string, localTrust bool) {
	webPrintListeningRuntime(webCarrierWeb, addr, ln, token, localTrust)
}

// webPrintListeningRuntime is the banner with the SPEAKING carrier's name
// substituted, so `gopi ui` can print the same reachable URLs (and the same
// token rule) under its own name instead of a second implementation of the port
// arithmetic.
func webPrintListeningRuntime(carrier, addr string, ln net.Listener, token string, localTrust bool) {
	urls := webReachableURLs(addr, ln)
	if len(urls) == 0 {
		// No interface resolved (or an address we cannot parse): fall back to
		// the raw config so the operator at least sees what was asked for.
		fmt.Fprintf(os.Stderr, "%s: listening on http://%s  (Ctrl+C to stop)\n", carrier, addr)
		return
	}

	fmt.Fprintf(os.Stderr, "%s: listening (Ctrl+C to stop)\n", carrier)
	for _, u := range urls {
		fmt.Fprintf(os.Stderr, "  open: %s\n", webTokenURL(u, token, localTrust))
	}
}

// webReachableURLs is the ONE resolution from a listen address plus its bound
// listener to the URLs a client can open. The banner and the carriers that need
// the same list as DATA (`gopi ui`'s identity notice, which hands a remote
// browser the URL to open) both come through here — a second copy of the port
// arithmetic would be a second answer to "what did the kernel actually bind".
//
// It returns nil when the address cannot be parsed or names nothing reachable:
// the caller then falls back to the raw configured address rather than
// inventing a URL nobody can open.
func webReachableURLs(addr string, ln net.Listener) []string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	port := webBoundPort(addr, ln)
	if port == "" {
		return nil
	}
	return webDisplayURLs(host, port)
}

// webBoundPort is the port the server is ACTUALLY listening on: the bound
// listener first (so `--addr 127.0.0.1:0` reports the kernel's choice, never
// the literal 0), then the configured address. "" means neither could be read.
func webBoundPort(addr string, ln net.Listener) string {
	if ln != nil {
		if _, p, err := net.SplitHostPort(ln.Addr().String()); err == nil && p != "" {
			return p
		}
	}
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}
	return ""
}

// webTokenURL appends the access token to a display URL (the one-click LAN
// handoff). No token (loopback bind) → the URL is returned unchanged.
//
// A URL that is loopback-BY-DEFINITION ("localhost", 127.0.0.1, [::1]) is also
// returned unchanged while local trust is on: it can only be opened on this
// machine, where the gate serves it without a token anyway — so a token there
// would be a false instruction. An interface-address URL is NOT in that class:
// it is the URL another machine opens, where the token IS required, so it
// keeps the token even though this host could also open it.
func webTokenURL(u, token string, localTrust bool) string {
	if token == "" {
		return u
	}
	if localTrust && webURLIsLoopback(u) {
		return u
	}
	return u + "?" + webTokenPair(token)
}

// webURLIsLoopback reports whether a display URL can only be opened on THIS
// machine (its host is "localhost" or a loopback address). Unparseable →
// false: an unexplained URL keeps the token (fail closed on the secret side).
func webURLIsLoopback(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return webHostIsLoopback(parsed.Hostname())
}

// webHostIsLoopback reports whether a host name/address is loopback by
// definition: the "localhost" name or a loopback address.
func webHostIsLoopback(host string) bool {
	if strings.EqualFold(host, webLoopbackHostName) {
		return true
	}
	return net.ParseIP(host).IsLoopback()
}

// webHostIsLocal reports whether a host name/address is THIS machine:
// "localhost", a loopback address, or one of its own interface addresses. This
// is the PEER-trust predicate (webPeerIsLocal) — broader than
// webHostIsLoopback, because a browser on this host may have reached the page
// through the machine's own LAN address, and that request is still the local
// shell talking to itself.
func webHostIsLocal(host string) bool {
	if webHostIsLoopback(host) {
		return true
	}
	return webIPIsLocal(net.ParseIP(host))
}

// webIPIsLocal reports whether an IP belongs to this machine — loopback, or one
// of its own interface addresses. This is the ONE trust predicate: the peer
// check (webPeerIsLocal) and the banner check (webHostIsLocal) both land here,
// so the URL a request is served at and the URL the banner prints agree.
// nil/unspecified → false (fail closed).
func webIPIsLocal(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	ifs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range ifs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// webLoopbackHostName is the host name that is loopback by definition (it may
// not be resolvable through net.InterfaceAddrs).
const webLoopbackHostName = "localhost"

// webAddrFlag is the listen-address flag of `gopi web` (single source of
// truth: the parser, the usage text and the error message all reference it).
const webAddrFlag = "--addr"

// webToolsFlag is the tool switch's spelling. Named because TWO surfaces
// parse it now (`gopi web` and `gopi ui`, whose wire-guard test asserts the
// two are the SAME spelling) — one definition, referenced (Rules 1b/13).
const webToolsFlag = "--tools"

// webFlagWordPrefix is the character every flag word of this family starts
// with. Named (Rule 1) because it decides THREE readings: a bare word is an
// address, a dash-prefixed word is a flag, and a lone "-" is neither (it is the
// conventional stdin operand).
const webFlagWordPrefix = "-"

// webParseFlagsUnknownFormat refuses a flag word the SERVER face does not know.
// It names the word, then the face's own accepted command line — a refusal that
// only said "unknown flag" would leave the operator guessing which of the two
// other faces' vocabularies they were reaching for (Rule 15).
const webParseFlagsUnknownFormat = "unknown flag %q — this face accepts:\n%s"

// webTokenQueryKey is the query parameter carrying the access token in the
// printed LAN URL (the one-click handoff: the page URL itself authenticates).
const webTokenQueryKey = "t"

// webAccessCookieName is the cookie the gate sets after a token-bearing page
// load, so the page's follow-up fetches (/api/chat, /api/transcript) and a
// later refresh authenticate without the token being re-appended by hand.
const webAccessCookieName = "gopi_web_token"

// webTokenBytes is the entropy of a minted per-run access token (16 bytes →
// 128 bits, 32 hex chars). Sized for an unguessable LAN-facing secret, not a
// password a human types.
const webTokenBytes = 16

// webTokenQuerySep is the query-string separator the page appends before the
// token pair (the page's QUERY_SEP, pinned by the constant-agreement test).
const webTokenQuerySep = "&"

// webTokenPair builds the "t=<token>" query fragment (without a leading "?"
// or "&"): the ONE spelling of the token pair, which the page's JS carries
// (withToken) so a same-origin fetch authenticates by the SAME route the page
// load did. Kept as a function so a change to the parameter name propagates to
// both carriers instead of drifting.
func webTokenPair(token string) string {
	return webTokenQueryKey + "=" + token
}

// webLANWarningFormat is the loud all-interfaces/non-loopback exposure notice,
// parameterised by the CARRIER that is speaking (`gopi web` / `gopi ui`): the
// risk text is shared, the name stays truthful. A named constant so the banner
// and its test assert the SAME string.
const webLANWarningFormat = "%s: LAN-REACHABLE bind — every URL below carries the per-run access token; anyone with that URL can drive this agent"

// webToolsLANWarningFormat is the escalated notice for the combination that
// actually matters: a network-reachable server whose turns execute tools.
const webToolsLANWarningFormat = "%s: tools ENABLED on a LAN-REACHABLE bind — remote tool execution is possible for anyone holding the token URL"

// webLANWarning / webToolsLANWarning are the notices as the BROWSER carrier
// prints them (`gopi web`) — the rendering every existing caller and test
// refers to, derived from the format above so there is one text and one name.
var (
	webLANWarning      = fmt.Sprintf(webLANWarningFormat, webCarrierWeb)
	webToolsLANWarning = fmt.Sprintf(webToolsLANWarningFormat, webCarrierWeb)
)

// addressIsLoopback reports whether a listen address can only be reached from
// the local machine. Empty host / unspecified / wildcard hosts are NOT
// loopback; an unparseable address is treated as NOT loopback (loopback
// cannot be promised for something we do not understand — same rule as
// voiceListenIsWildcard). Used to decide whether the access-token gate is
// required.
func addressIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false // ":8090" = all interfaces
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname we cannot resolve here: only the literal "localhost" is
		// loopback by definition; anything else must be treated as exposed.
		return host == "localhost"
	}
	return ip.IsLoopback()
}

// webLocalTrustNotice tells the operator, on a gated bind, that the browser on
// THIS host is served without the token. Printed next to the LAN warning so the
// banner states the real posture: local requests are trusted, remote ones need
// the token. Its absence is what made a same-host browser look like an
// "apikey" failure. Carrier-parameterised like the warning above.
const webLocalTrustNoticeFormat = "%s: requests from THIS host (loopback or one of its own addresses) skip the token — a browser here opens the page directly; other machines still need the token URL"

// webLocalTrustNotice is that notice as the browser carrier prints it.
var webLocalTrustNotice = fmt.Sprintf(webLocalTrustNoticeFormat, webCarrierWeb)

// webPeerIsLocal reports whether a request's peer address (http.Request
// RemoteAddr, "host:port") is this machine talking to itself: a loopback peer,
// or one of this host's OWN interface addresses. Both mean the request never
// crossed the network, so it carries the same trust as the local shell that
// started `gopi web` — that shell can already run gopi with tools anyway.
//
// Fail closed (Rule 15): an address that cannot be parsed is NOT local.
func webPeerIsLocal(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false // unparseable peer: never trust, never guess
	}
	return webHostIsLocal(host)
}

// webLocalTrustEnv demands the token even for requests from this host
// (GOPI_WEB_LOCAL_TRUST=0) — the operator's escape hatch when the local host is
// itself shared. Absent or any other value keeps local trust ON, which is the
// default posture. The env read lives here alone (Rule 2).
func webLocalTrustEnv() bool {
	return os.Getenv(webLocalTrustEnvName) != webLocalTrustOffValue
}

// webLocalTrustEnvName / webLocalTrustOffValue are the env knob and the one
// value that disables local trust: `GOPI_WEB_LOCAL_TRUST=0`.
const (
	webLocalTrustEnvName  = "GOPI_WEB_LOCAL_TRUST"
	webLocalTrustOffValue = "0"
)

// webLocalTrust reports whether requests from this host skip the access token.
// It is true unless the operator turned it off (webLocalTrustEnv), so the
// ordinary case — one operator on one machine — needs no flag at all.
func webLocalTrust(addr string) bool {
	return webLocalTrustEnv()
}

// webGatedHandler is THE decision point for the LAN exposure that runWeb must
// not get wrong: given the effective listen address, it returns the handler to
// serve and the token to print in the banner.
//
//   - loopback address  → the bare handler, empty token (the local-shell-trust
//     posture is unchanged: no gate, no token in the printed URL).
//   - anything else     → webTokenGate-wrapped handler with the pinned
//     (GOPI_WEB_TOKEN) or freshly minted per-run token.
//
// localTrust says whether requests from this host are served without the token
// (see webPeerIsLocal). It does NOT weaken a remote peer: the gate still
// refuses every request whose peer address is not this machine.
//
// A bind that cannot mint a secret returns the error — refusing to start beats
// serving a network-reachable agent without a gate.
func webGatedHandler(addr string, bare http.Handler, localTrust bool) (http.Handler, string, error) {
	if addressIsLoopback(addr) {
		return bare, "", nil
	}
	token := webPinnedToken()
	if token == "" {
		var err error
		if token, err = webNewAccessToken(); err != nil {
			return nil, "", err
		}
	}
	return webTokenGate(token, bare, localTrust), token, nil
}

// webChatHandler is the mux every `gopi web` server serves (page + SSE turn +
// transcript replay), WITHOUT the access gate — runWeb wraps it in
// webTokenGate. Split out so the gate can be tested over the real routes.
func (s *webServer) webChatHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)                           // the chat page
	mux.HandleFunc(webChatResetPath, s.handleChatReset)         // B1: conversation reset
	mux.HandleFunc(webChatPath, s.handleChat)                   // SSE turn
	mux.HandleFunc(webChatInterruptPath, s.handleChatInterrupt) // abandon the turn in flight
	// THE WAITING-MESSAGE WITHDRAWAL (cli_web_queue.go): POST {"id":N} takes a
	// message back out of the queue it is waiting in, so its turn never runs.
	// Registered on the SAME mux, so webTokenGate covers it identically — an
	// unauthenticated POST is refused before the handler runs.
	mux.HandleFunc(webChatQueueCancelPath, s.handleChatQueueCancel)
	// THE WAITING-MESSAGE REGISTRATION (cli_web_queue.go): POST a message that
	// is waiting its turn and learn the id + position it was given. Without it
	// the server would hear about a waiting message only when it finally POSTs
	// its turn — one turn too late to show it, number it or withdraw it.
	mux.HandleFunc(webChatQueuePath, s.handleChatQueueRegister)
	mux.HandleFunc(webMenuPath, s.handleMenu)             // B9: menu candidates
	mux.HandleFunc(webTranscriptPath, s.handleTranscript) // R4: refresh replay
	// THE STANDING EVENT STREAM (cli_web_notice.go): the page holds this open so
	// a turn the SERVER started — a `gopi notify --session …` delivery — has
	// somewhere to stream to. Registered on the same mux, so webTokenGate covers
	// it identically (an unauthenticated GET is refused before the handler runs).
	mux.HandleFunc(webEventsPath, s.handleWebEvents) // server-driven turn frames
	// THE SERVER-SIDE STATUS (cli_web_state.go): the same document the `state`
	// frame carries, on demand. Registered on the SAME mux so webTokenGate covers
	// it identically — an unauthenticated GET is refused before the handler runs.
	mux.HandleFunc(webStatePath, s.handleWebState) // idle / responding / tool_running
	// The page-capture route (cli_web_shot.go): POST → this instance's own page
	// as base64 PNG, for the «📷» button. Registered on the SAME mux, so the
	// gate webGatedHandler installs over /api/chat covers it identically — an
	// unauthenticated POST is refused before the handler runs.
	mux.HandleFunc(webShotPath, s.handleShot)
	// The MICROPHONE (web_voice.go): route B — the recording runs on THIS host,
	// through the same device policy and engine seam the TUI's /voice uses, and
	// the page receives an editable DRAFT rather than a sent turn.
	mux.HandleFunc(webVoiceStartPath, s.handleVoiceStart) // begin one recording
	mux.HandleFunc(webVoiceStopPath, s.handleVoiceStop)   // stop, transcribe, answer a draft
	// The DISCARD route (the page's Escape on the window path): end the recording
	// and throw the audio away — never transcribe it.
	mux.HandleFunc(webVoiceCancelPath, s.handleVoiceCancel)
	// The LIVE microphone (web_voice_live.go): the same utterance, streamed.
	// POST /api/voice/live holds the connection open and publishes every partial
	// as a `draft` SSE frame while the operator speaks; the stop route ends it.
	// The engine gate is inside the handler (a 501 naming the engine), so the
	// route is registered unconditionally — an engine without a live arm gets a
	// refusal the page turns into the window path above, not a 404.
	mux.HandleFunc(webVoiceLivePath, s.handleVoiceLive)         // stream one utterance
	mux.HandleFunc(webVoiceLiveStopPath, s.handleVoiceLiveStop) // end it, keep the sentence
	// The THIRD ender (the page's Escape): end it and DISCARD it. A separate verb
	// because stop settles into the composer and cancel must not.
	mux.HandleFunc(webVoiceLiveCancelPath, s.handleVoiceLiveCancel)
	// The engine IDENTITY route (web_identity.go): a gated GET answering
	// "which session/model/tools am I talking to?" — the question a remote
	// browser (or `gopi ui --server`) cannot answer from the URL alone.
	mux.HandleFunc(webIdentityPath, s.handleIdentity)
	// The TERMINAL carrier (epic: gopi cli) — a second VIEW over this same
	// engine, served from the same mux so both carriers share one agent, one
	// s.mu turn discipline and one s.history. The routes are additive: no
	// existing pattern, page or frame changes (term.go holds the rationale).
	mux.HandleFunc(termChatPath, s.handleTermPage)       // the /cli chat surface
	mux.HandleFunc(termChatSubmitPath, s.handleTermChat) // one streaming turn
	return mux
}

// webToolsEnv is the env var that enables tool turns for BOTH web carriers
// (`gopi web`, `gopi cli`). Exactly one reader: webToolsEnabled (Rule 2).
const webToolsEnv = "GOPI_WEB_TOOLS"

// webToolsEnabled reports whether the env switch turns tool turns on. The ONE
// place this env var is read: the two carriers pass the value into
// webParseFlags, which lets `--tools` win, so the precedence rule lives in one
// function rather than in each caller.
func webToolsEnabled() bool {
	return os.Getenv(webToolsEnv) != ""
}

// webThinkingFlag is the live-reasoning switch's spelling, shared by both
// carriers the same way webToolsFlag is (one switch, two surfaces — Rule 13).
const webThinkingFlag = "--thinking-live"

// webThinkingEnv is the env channel of that switch. It exists because the
// surface is often started by something that cannot add a flag, and because a
// carrier's own launcher should not have to know the argv shape.
const webThinkingEnv = "GOPI_THINKING_LIVE"

// webThinkingEnabled reports whether the env switch asks for live reasoning.
// The ONE place this env var is read (Rule 2); the flag wins over it through
// webParseFlags's result, exactly as --tools wins over webToolsEnv.
//
// TRUTHY-ONLY (config.EnvBool), never `!= ""`: GOPI_THINKING_LIVE=0 must mean
// OFF. The sibling switch's `!= ""` form is the older, inverted convention
// (GOPI_WEB_TOOLS=0 enables tools) and it is deliberately NOT copied here — a
// new knob has no history to preserve, and "0 turns it on" is the reading
// nobody expects.
func webThinkingEnabled() bool {
	return config.EnvBool(webThinkingEnv)
}

// webPinnedToken returns the operator-pinned access token (GOPI_WEB_TOKEN),
// or "" when unset — the caller then mints a random per-run token. The env
// read lives here alone (Rule 2).
func webPinnedToken() string {
	return os.Getenv(webTokenEnv)
}

// webTokenEnv is the env var that pins the LAN access token (exactly one
// reader: webPinnedToken).
const webTokenEnv = "GOPI_WEB_TOKEN"

// webTurnTimeoutEnv is the env var that overrides the per-turn web turn budget
// (exactly one reader: webTurnTimeout — Rule 2).
const webTurnTimeoutEnv = "GOPI_WEB_TURN_TIMEOUT_SEC"

// webDefaultTurnTimeout is the per-turn budget for a web turn (B12): 0 =
// unlimited, the agent's own default, because a single global timeout for the
// whole process is too blunt for an interactive page — the operator opts in. A
// non-zero setting bounds a turn so a stalled upstream releases s.mu for the
// next POST instead of queueing every later turn behind it forever.
const webDefaultTurnTimeout = 0 * time.Second

// webTurnTimeout resolves the per-turn web turn budget (B12): the
// GOPI_WEB_TURN_TIMEOUT_SEC override when it parses to a positive number of
// seconds, otherwise the default. This is the ONE place that reads the env var
// (Rule 2), and handleChat is the one place that applies the result.
func webTurnTimeout() time.Duration {
	timeout := webDefaultTurnTimeout
	if v := os.Getenv(webTurnTimeoutEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		} else {
			fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %v\n", webTurnTimeoutEnv, v, timeout)
		}
	}
	return timeout
}

// webParseFlags applies the `gopi web` server flags to the (env-derived)
// defaults and returns the effective address, tool switch, live-reasoning
// switch and help request. Pure (no I/O beyond the values handed in) so the CLI
// contract is testable without starting a server.
//
// BOTH switches are returned rather than applied in place for the same reason:
// the env decides the DEFAULT and the flag may only turn it ON (a switch this
// deep has no OFF spelling — omitting the flag is the OFF), so the caller keeps
// one precedence rule per knob instead of a mutable global.
func webParseFlags(args []string, addr string, toolsEnabled, thinkingEnabled bool) (webCLIOptions, error) {
	return webParseFlagsWithPolicy(args, addr, toolsEnabled, thinkingEnabled, false)
}

// webParseFlagsServer is webParseFlags for the SERVER face (`gopi web` itself),
// which refuses a flag word it does not know instead of ignoring it.
//
// WHY THE TWO FACES DIFFER, and why that is not a fork. This parser is shared by
// three faces (Rule 14), and two of them — `gopi cli` and `gopi cli --once` —
// own further switches (`--once`, `--prompt`) that THIS parser must not judge.
// So the unknown-word rule belongs to the caller that owns the whole vocabulary,
// and `gopi web` is such a caller: its accepted words are exactly the ones this
// function parses (plus the positional address).
//
// THE DEFECT IT CLOSES (measured 2026-11-07, row 01a1b50c). `webApplyRecordedOptions`
// restores a switch from the continued session's record only when this command
// line did not STATE it, and "stated" was decided by `webSwitchStated(args, w)`
// — a scan over the RAW argv. A word this parser silently ignored was therefore
// still counted as stated: `gopi web <id> --no-tools` matched nothing in the
// parser (no error) and matched nothing in the stated scan either, while any
// misspelt `--tool` was accepted in silence and the capability was dropped
// without one sentence naming why. A refusal that names the word and the usage
// page is the honest answer (Rule 15).
func webParseFlagsServer(args []string, addr string, toolsEnabled, thinkingEnabled bool) (webCLIOptions, error) {
	return webParseFlagsWithPolicy(args, addr, toolsEnabled, thinkingEnabled, true)
}

// webParseFlagsWithPolicy is the ONE implementation; refuseUnknown selects the
// server face's strictness. It is a parameter rather than a second parser so the
// two faces can never disagree about what `--tools` means.
func webParseFlagsWithPolicy(args []string, addr string, toolsEnabled, thinkingEnabled, refuseUnknown bool) (webCLIOptions, error) {
	opts := webCLIOptions{Addr: addr, Tools: toolsEnabled, Thinking: thinkingEnabled}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == HelpFlag || a == HelpFlagLong:
			opts.Help = true
			return opts, nil
		case a == webToolsFlag:
			opts.Tools = true
		case a == webThinkingFlag:
			opts.Thinking = true
		case strings.HasPrefix(a, webAddrFlag+"="):
			opts.Addr = strings.TrimPrefix(a, webAddrFlag+"=")
		case a == webAddrFlag:
			// "--addr :PORT" as two tokens: the space-separated flag value is
			// the form every other gopi subcommand takes, so the LAN bind must
			// not demand the joined "--addr=:PORT" spelling (one step away).
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value, e.g. %s=0.0.0.0:%d", webAddrFlag, webAddrFlag, webDefaultPort)
			}
			i++
			opts.Addr = args[i]
		case refuseUnknown && strings.HasPrefix(a, webFlagWordPrefix) && a != webFlagWordPrefix:
			// A WORD STARTING WITH DASH IS A FLAG WORD ON THIS FACE, so a
			// word this parser does not know is a REFUSAL and never a silent
			// no-op. A lone "-" is excluded: it is the conventional stdin
			// operand, not a switch.
			return opts, fmt.Errorf(webParseFlagsUnknownFormat, a, webResumeUsageSynopsis)
		case !strings.HasPrefix(a, "-"):
			// A BARE WORD IS AN ADDRESS, NOT A FLAG VALUE. It is COLLECTED here
			// and judged by the CALLER, never refused here: this one parser also
			// serves `gopi cli` / `gopi cli --once`, whose own positionals (the
			// --prompt value) are not session addresses, and a refusal invented
			// in the shared parser would make one face's usage rule the other
			// faces' (Rule 14: one parser, one address rule per face).
			opts.Positional = append(opts.Positional, a)
		}
	}
	return opts, nil
}

// webNewAccessToken mints the per-run access token for a non-loopback bind.
// crypto/rand failure is returned (never a silently weaker/empty secret — a
// gate that cannot mint a secret must fail the start, not serve openly).
func webNewAccessToken() (string, error) {
	buf := make([]byte, webTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint web access token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// webRequestToken extracts the caller's token: query param first (the printed
// one-click URL), then the gate cookie, then an Authorization: Bearer header
// (script/curl access).
func webRequestToken(r *http.Request) string {
	if v := r.URL.Query().Get(webTokenQueryKey); v != "" {
		return v
	}
	if c, err := r.Cookie(webAccessCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return bearerToken(r.Header.Get("Authorization"))
}

// bearerToken returns the token of an "Authorization: Bearer <token>" header,
// or "" when the header is absent/another scheme.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return header[len(prefix):]
	}
	return ""
}

// webUnauthorizedMessage is the 401 body for a request that carries no valid
// access token. It deliberately names BOTH concepts the operator otherwise
// conflates: the WEB access token (per-run, printed in the banner) is not the
// model API key (host config, never involved here) — the old wording made a
// configuration-correct host look like a broken apikey.
const webUnauthorizedMessage = "unauthorized: the gopi web ACCESS TOKEN is required here (this is the per-run token printed in the `gopi web` banner and appended to its URLs as ?" + webTokenQueryKey + "=<token>, or sent as `Authorization: Bearer <token>`) — it is NOT the model API key, and requests from this host do not need it"

// webObservedRemoteMessage is the 401 body for a request that was already
// token-refused from the same peer before. Repeating "unauthorized" alone makes
// a remote browser look like it is stuck on an apikey problem; this form is
// distinguishable in a browser console and in a log, and says what to do.
const webObservedRemoteMessage = "still unauthorized from this peer: the gopi web ACCESS TOKEN is missing or wrong. Open the ?%s=<token> URL printed in the `gopi web` banner (or send `Authorization: Bearer <token>`); a request from THIS host needs no token, so seeing this means the request did not originate on the gopi host"

// webTokenGate wraps the chat mux so that, when a token is configured (any
// non-loopback bind), every request must carry it — EXCEPT a request from this
// host when localTrust is on (the local-shell-trust posture: a browser here is
// the same operator who already holds the shell).
//
// Constant-time comparison; the loopback default configures no token and
// therefore passes straight through. A remote peer is never weakened by
// localTrust: trust is decided by the observed peer address only
// (webPeerIsLocal), never by a client-supplied header.
func webTokenGate(token string, next http.Handler, localTrust bool) http.Handler {
	if token == "" {
		return next
	}
	var observedMu sync.Mutex
	observed := map[string]bool{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if localTrust && webPeerIsLocal(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}
		presented := webRequestToken(r)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			logWebTokenRefusal(r, presented)
			w.Header().Set("WWW-Authenticate", `Bearer realm="gopi web"`)
			msg := webUnauthorizedMessage
			if ip := webPeerIP(r.RemoteAddr); ip != "" {
				observedMu.Lock()
				seen := observed[ip]
				observed[ip] = true
				observedMu.Unlock()
				if seen {
					msg = fmt.Sprintf(webObservedRemoteMessage, webTokenQueryKey)
				}
			}
			http.Error(w, msg, http.StatusUnauthorized)
			return
		}
		// The request MAY still carry a stale cookie from an earlier run (the
		// token is re-minted per run: an old page, or a page that predates the
		// ?t=-carrying fetches, sends the old one). The request is authorized
		// by the token it PRESENTED — the carrier precedence webRequestToken
		// already applied — so replace the stale cookie with the token that
		// actually authenticated this request, or the NEXT cookie-only fetch
		// would 401 again with a page that looks freshly loaded.
		if c, err := r.Cookie(webAccessCookieName); err != nil || c.Value != token {
			http.SetCookie(w, &http.Cookie{
				Name:     webAccessCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}

// webRefusalLogPrefix is the stable prefix of the gate's refusal line — the
// ONE thing an operator greps for when a browser reports "still unauthorized".
// Named (Rule 1b) because the test asserts this exact spelling.
const webRefusalLogPrefix = "[gopi web] token gate: refused"

// webRefusalLogFormat is the refusal line's shape. Every field an operator
// needs to diagnose a 401 is present: WHEN, WHO (peer), WHAT (method+path) and
// WHY (which carriers arrived and whether they matched each other). It is
// declared as a constant so the log line is a fixed shape rather than five
// ad-hoc Sprintf calls, and so the test can assert each part.
const webRefusalLogFormat = webRefusalLogPrefix + " %s peer=%s %s %s query=%s cookie=%s reason=%s"

// webRedactedTokenMarker is the stand-in printed instead of matched token
// material: a secret must never reach a log, so the refusal line records only
// WHETHER each carrier arrived and whether the two agreed.
const webRedactedTokenMarker = "present"

// logWebTokenRefusal records why the gate refused a request. It exists because
// the gate previously logged NOTHING: a 401 existed only on the wire, so a
// browser reporting "still unauthorized from this peer" left no host-side trace
// to diagnose (Rule 15: a refusal is a handled outcome that must be recorded,
// not a silent one).
func logWebTokenRefusal(r *http.Request, presented string) {
	query, cookie := false, false
	if v := r.URL.Query().Get(webTokenQueryKey); v != "" {
		query = true
	}
	cookieToken := ""
	if c, err := r.Cookie(webAccessCookieName); err == nil {
		cookieToken = c.Value
		cookie = true
	}
	reason := "no credential presented"
	switch {
	case query && cookie && cookieToken != presented:
		// The verifier reads the query first, so what was COMPARED is the query
		// token (presented); the cookie differs from it, so one of the two is
		// from an earlier run. This is the shape a browser shows when the URL is
		// fresh while the jar still holds an old run's cookie.
		reason = "query token and cookie DISAGREE (different tokens — one of them is from an earlier run)"
	case query && cookie:
		reason = "query token and cookie agree, but the token is wrong"
	case query:
		reason = "wrong query token, no cookie"
	case cookie:
		reason = "wrong cookie token (the request carried no ?" + webTokenQueryKey + "= — the page did not append the access token)"
	}
	fmt.Fprintf(os.Stderr, webRefusalLogFormat+"\n",
		time.Now().Format(time.RFC3339),
		r.RemoteAddr,
		r.Method,
		r.URL.Path,
		carrierState(query),
		carrierState(cookie),
		reason,
	)
}

// carrierState renders one carrier's presence for the refusal line.
func carrierState(present bool) string {
	if present {
		return webRedactedTokenMarker
	}
	return "absent"
}

// webPeerIP returns the peer's bare IP (no port) for identify-and-record
// purposes, or "" when the address cannot be parsed. It is NOT a trust
// decision (that is webPeerIsLocal, which fails closed).
func webPeerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return ""
	}
	return host
}

// webDisplayURLs returns the browser-reachable URLs for a listen host+port.
//   - host is concrete ("127.0.0.1", "localhost", "192.168.7.10") → exactly
//     that one URL.
//   - host is empty or "0.0.0.0"/"::" (all interfaces) → the loopback URL plus
//     each non-loopback IPv4 address on the machine (so a browser on another
//     LAN host can reach it). Loopback URLs only as a fallback when no
//     non-loopback IP exists.
func webDisplayURLs(host, port string) []string {
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}

	// All-interfaces bind: gather the machine's actual reachable addresses.
	var ips []string
	if ifs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() {
				ips = append(ips, ip4.String())
			}
		}
	}

	var urls []string
	urls = append(urls, "http://"+net.JoinHostPort("localhost", port))
	for _, ip := range ips {
		urls = append(urls, "http://"+net.JoinHostPort(ip, port))
	}
	return urls
}

// maxWebHistoryMessages is retained only as documentation of the pre-R1 cap
// (the legacy client-shipped history field is accepted but ignored — R1).
const maxWebHistoryMessages = 200

// handleRoot serves the embedded single-file chat page. The page is written
// in plain HTML + vanilla JS: it POSTs each message to /api/chat and reads the
// Server-Sent-Events stream. The SERVER owns the conversation (R1) — the page
// only renders it and never re-ships history.
func (s *webServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, webChatPage)
}

// handleChat runs one chat turn and streams the answer back as Server-Sent
// Events. It reuses the server's SINGLE persistent agent (s.agent, created at
// startup with PersistChatSession=true) — every turn serves through the same
// agent, so every turn's user/assistant/request events append to the SAME
// session id + session JSONL + raw log (the Epic's single-session consolidation,
// mirrored from `gopi chat`).
//
// R1: the SERVER owns the authoritative multi-turn context. The seed history
// is s.history — captured from ag.Ctx.Messages after every turn (mirroring
// `gopi chat`'s chatSession at cli_chat.go). A request `history` field is
// accepted for back-compat but deliberately ignored; the browser is a display.
// The single session_end + task ledger close happen at server shutdown.
func (s *webServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST /api/chat)", http.StatusMethodNotAllowed)
		return
	}
	// Shutdown interlock: refuse new turns once the finalizer is closing the
	// session (the finalizer takes s.mu; queueing here would deadlock the
	// close behind a turn that can never stream out).
	if s.shuttingDown {
		http.Error(w, "{\"error\":\"server shutting down\"}", http.StatusServiceUnavailable)
		return
	}

	var req webChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, webMaxChatBodyBytes)).Decode(&req); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	msg := strings.TrimSpace(req.Message)
	// B12: the steering sentinel is carrier METADATA, never content — the page
	// prefixes a message that addresses the running turn, and the model must
	// only ever see the user's own words. Stripping happens BEFORE the routing
	// below, so one rule decides what the message IS (carrier framing removed)
	// and one rule decides where it goes. The sentinel is matched with its own
	// trailing space removed, so a bare "!steer" (or "!steer   ") leaves an
	// EMPTY message and is refused — framing never becomes a turn (Rule 15).
	placement := webPlacementQueued
	if after, ok := strings.CutPrefix(msg, strings.TrimSpace(webSteerPrefix)); ok {
		msg = strings.TrimSpace(after)
		placement = webPlacementSteering
	}
	if msg == "" {
		http.Error(w, "{\"error\":\"empty message\"}", http.StatusBadRequest)
		return
	}

	// R5: validate every attached image BEFORE locking the agent — 4xx with
	// a user-actionable message (bad media type, oversize, broken base64).
	images, err := validateWebImages(req.Images)
	if err != nil {
		http.Error(w, "{\"error\":"+jsonString(err.Error())+"}", http.StatusBadRequest)
		return
	}
	// Files API transport: upload each picture once and hand the model a file
	// REFERENCE instead of its base64 bytes (cli_web_files.go). A failure here
	// is reported verbatim and the turn does not run — deliberately NOT a
	// silent fallback to the inline transport, which is the path the 32 MiB
	// body cap exists to refuse (Rule 15).
	if len(images) > 0 {
		staged, ferr := s.stageImagesAsFiles(r.Context(), images)
		if ferr != nil {
			http.Error(w, "{\"error\":"+jsonString(ferr.Error())+"}", webFilesUploadFailureStatus)
			return
		}
		images = staged
	}

	// The single persistent agent built once at server startup. The
	// conversation context is server-owned: s.history seeds the turn and is
	// re-captured from ag.Ctx.Messages afterwards. req.History is ignored
	// (R1 — browser-shipped history was lossy and body-capped).
	ag := s.agent
	if ag == nil {
		http.Error(w, "{\"error\":\"server agent not initialized\"}", http.StatusInternalServerError)
		return
	}

	// Serialize this turn against the shared persistent agent (concurrent
	// /api/chat requests must not interleave on one session — same
	// single-agent discipline as the sequential `gopi chat` REPL). The lock is
	// held for the whole turn, streaming included, so one conversation's turns
	// queue and never race the shared mutable agent state.
	// B2: answer the carrier BEFORE waiting for the lock. The 200 + SSE
	// headers and one `event: queued` frame are written/flushed here, so a
	// second concurrent POST is acknowledged promptly instead of hanging
	// byte-less behind s.mu for the whole of turn N (dsh's typed
	// `placement: 'queued'` idea, carried as a plain SSE frame — no RPC
	// layer). When s.mu is free the TryLock succeeds and no frame is sent:
	// the common case stays byte-identical to the pre-B2 wire.
	flusher, ok := w.(http.Flusher)
	if !ok {
		// No flusher (non-HTTP response writer) — abort cleanly, before
		// anything has been committed.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: error\ndata: {\"error\":\"streaming unsupported\"}\n\n")
		return
	}
	writeSSEHeaders(w)
	flusher.Flush()
	// B12/queue: `placement` (decided with the sentinel above, as a fact about
	// the REQUEST rather than about the wait) is the type this message carries.
	// THE MESSAGE IS REGISTERED SERVER-SIDE BEFORE THE FRAME IS WRITTEN
	// (cli_web_queue.go): from this moment it has an id, a typed state and — when
	// queued — a position, so the page can NAME it and the withdrawal route can
	// reach it. A POST that arrives without a queue_id (the ordinary fresh
	// submit) is registered at the moment it learns it must wait; a POST that
	// already carries one (the page registered it when the operator typed it)
	// keeps that id, so the entry the operator sees and the entry this request
	// runs are the SAME entry.
	var waitItem *webQueueItem
	if req.QueueID > 0 {
		waitItem = s.queue.byID(req.QueueID)
	}
	if !s.mu.TryLock() {
		if waitItem == nil {
			waitItem = s.enqueueWebMessage(msg, placement)
			writeSSEFrame(w, webEventQueued, webQueuedFramePayload(s.queueView(), placement, waitItem.id))
			flusher.Flush()
		}
		// Queue behind the in-flight turn. mu is held for exactly as long as
		// before B2 — this wait does not extend the holder's domain.
		//
		// THE WITHDRAWAL TAKES EFFECT HERE, and it is checked WHILE waiting, not
		// after: a message taken back must not sit behind the very turn it is no
		// longer asking for, and must not run when that turn ends. The only
		// place that can honour the mark is this request (it owns the turn that
		// was about to run), so it answers one `cancelled` frame and returns
		// WITHOUT touching the model, the history, the turn counters or the
		// transcript.
		if waitForTurnOrWithdrawal(&s.mu, &s.queue, waitItem) {
			defer s.queue.compact()
			writeSSEFrame(w, webEventCancelled, webCancelledPayload(waitItem.id))
			flusher.Flush()
			s.publishWebState()
			return
		}
		defer s.queue.compact()
	}
	defer s.mu.Unlock()

	// A message registered while waiting LEAVES THE PENDING VIEW NOW — at its
	// turn's beginning, not its end, because listing the running turn as also
	// waiting is a state that is never true.
	if waitItem != nil {
		s.queue.markStarted(waitItem)
		s.publishWebState()
	}

	// THE TURN BEGINS HERE (cli_web_state.go): the lock is ours, so from this
	// point the page may be told the assistant is working. The frame is published
	// BEFORE the model is called, so a reader watching the badge sees the
	// transition while the round is still running — not at its end. The matching
	// `idle` is deferred first so EVERY return path (slash command, reset, error,
	// abandoned run) reports the turn over.
	s.state.enterTurn(webNowUnix())
	s.publishWebState()
	defer func() {
		s.state.leaveTurn(webNowUnix())
		// THE IDLE CLOCK STARTS HERE (cli_web_queue.go's webQueueAbandonAfter):
		// a queued message is abandoned only after a turn-end went by with
		// nothing claiming it, so this is the one place that reading is stamped.
		s.queue.noteTurnEnd(webNowUnix())
		s.publishWebState()
	}()

	// R4 slash-command routing, by the SHARED CLASSIFIER (commands.go — one
	// criterion, four carriers). A command-shaped line is a local command: it
	// executes through the shared command table, streams back as ONE `event:
	// command` frame, and never reaches the model. A command-shaped line the
	// table does not accept is REFUSED there and then — never turned into a
	// question for the model (a typo must not silently become a prompt), and
	// never silently sent. EVERYTHING ELSE — a pasted path, a pattern, "//word"
	// asking for the literal — is the model's text, verbatim, and falls
	// through to the turn below.
	classified := classifyChatLine(msg, sharedSlashWordKnown)
	if classified.kind == chatLineCommand || classified.kind == chatLineUnknown {
		msg = classified.text
		// B1: the conversation-reset words are dispatched HERE, before the
		// generic table lookup, because the state they clear is carrier-local
		// (web: s.history under s.mu — the table cannot see it). The WORD
		// itself lives in the shared table (webClearCommandName), so `/new` is
		// discoverable in /help and spelled identically in both carriers; only
		// its effect is per-carrier.
		if isResetCommand(msg) {
			s.serveResetCommand(w, msg)
			return
		}
		s.serveSlashCommand(w, msg)
		return
	}
	if classified.kind == chatLineEscape {
		msg = classified.text
	}

	// THE TURN BANNER, BEFORE THE TURN RUNS (cli_web_turn.go). The number is the
	// SESSION's own ordinal for the turn now starting — turnBaseline + the rows
	// this run's writer has recorded + 1 — and it is published here, ahead of
	// the model, so a reader sees which turn they are in while it is still
	// running rather than when it is over.
	//
	// A message the record charges no turn to (a framework carrier) opens no
	// turn, so no frame is sent and the terminal `done` carries no
	// session_turn — an absent number is the honest answer there; a fabricated
	// one would be a number the record does not give that row.
	turnNo, turnIsTurn := s.webTurnOrdinal(msg, "")
	// THE MOMENT THIS TURN OPENED, minted ONCE and shared: it rides the turn
	// frame to the page, and it is published to the agent so the record's own
	// turn-opening row is stamped with the SAME value (the runner consumes it
	// once). One mint, two readers — the page cannot show a second the session
	// log disagrees with.
	turnOpenedAt := time.Now()
	if turnIsTurn {
		writeSSEFrame(w, webEventTurn, webTurnPayload(turnNo, turnOpenedAt))
		flusher.Flush()
	}

	// R1: the authoritative conversation seed is the server-captured
	// s.history (the full deepseek.Message transcript, incl. tool turns in
	// --tools mode). The request's `history` field is intentionally NOT used
	// — see the R1 note on handleChat.
	seed := s.history
	if seed == nil {
		seed = []deepseek.Message{}
	}

	// NOTE: cancellation of the in-flight model request on client disconnect
	// is intentionally not wired up here. Agent.Chat builds its own internal
	// context (honoring Agent.Timeout when set), so a per-turn request is
	// bounded by that timeout; the web handler itself returns promptly after
	// the turn ends. Earlier attempts to observe r.Context().Done() and close
	// a signal channel PANICKED with "close of closed channel" when the
	// client dropped mid-stream and the goroutine raced the deferred close —
	// the channel added nothing (nothing consumed it) while introducing a
	// crash. Removed: the SSE stream + Agent.Chat's own timeout already bound
	// the request, and a failed write is ignored by the SSE flusher anyway.

	// Stream each model delta straight out as an SSE event before it is
	// consumed anywhere else (OnText fires on both the SSE and the
	// non-streaming fallback chat paths in loop.go).
	ag.OnText = func(delta string) {
		// Ignore empty/whitespace-only deltas (session markers, etc.).
		if delta == "" {
			return
		}
		// JSON-encode the delta so newlines/UTF-8 survive the SSE framing.
		payload, _ := json.Marshal(map[string]string{"text": delta}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		writeSSEFrame(w, webEventDelta, payload)
		flusher.Flush()
	}

	// R2: stream tool activity as SSE `event: tool` frames. OnToolEvents
	// mirrors OnText (call events fire pre-execution, result events
	// post-execution) — pure display, the loop never depends on it.
	ag.OnToolEvents = func(ev agent.ToolEvent) {
		payload, err := json.Marshal(ev)
		if err != nil {
			return
		}
		writeSSEFrame(w, webEventTool, payload)
		flusher.Flush()
		// The STATUS transition rides the SAME callback (cli_web_state.go): the
		// loop fires it BEFORE the tool executes, so `tool_running(<name>)` is
		// true for the whole of the tool's blocking duration rather than for the
		// instant its result arrives.
		s.noteToolState(ev)
	}

	// A statement addressed to the OPERATOR (context pressure — a filled window is
	// ANNOUNCED, never silently emptied: internal/agent/context_pressure.go). It
	// rides its OWN SSE event (`notice` is already taken by a server-started
	// turn, and a warning about this turn's context is not a new turn), so the
	// page can render it in the page's own notice style without the client
	// having to guess from the payload which of the two it received.
	ag.OnUserNotice = func(text string) {
		payload, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return //stylegate:ignore marshal of a string map cannot fail; the guard is shape only
		}
		writeSSEFrame(w, webEventWarning, payload)
		flusher.Flush()
	}

	// R3: stream the LIVE REASONING as SSE `event: thinking` frames — the
	// "the model is responding" line the surface draws for itself.
	//
	// OFF BY DEFAULT, and the gate is a single decision made HERE rather than
	// a filter on the receiving side: with the switch off the handler installs
	// NOTHING (OnThinking stays nil, StreamThinkingDeltas stays false), so the
	// engine's reasoning keeps the historic destination — os.Stderr, wrapped in
	// its own markers — and the wire carries not one extra byte. That matters
	// because this handler runs under the TUI too: a frame nobody draws would
	// still be a repaint, and the default `gopi ui` screen must stay
	// byte-identical to the pre-epic one.
	//
	// WITH it on, TWO things move together and must: OnThinking takes the bytes
	// (so they do not also land on the console the TUI owns — the reason
	// silenceEngineConsole exists) and StreamThinkingDeltas tells the engine to
	// send them RAW. The frame's payload is the fragment, untouched: no
	// `[thinking] ` marker, no SGR (see webThinkingPayload).
	prevOnThinking := ag.OnThinking
	prevStreamThinkingDeltas := ag.StreamThinkingDeltas
	if s.thinkingStream() {
		ag.StreamThinkingDeltas = true
		ag.OnThinking = func(chunk string) {
			// Empty chunks are dropped rather than framed: a frame with no text
			// would make the client repaint for nothing, and the engine emits
			// one at both block boundaries (the marker writes, which RAW mode
			// has emptied to "").
			if chunk == "" {
				return
			}
			writeSSEFrame(w, webEventThinking, webThinkingPayload(chunk))
			flusher.Flush()
		}
	}
	defer func() {
		ag.OnThinking = prevOnThinking
		ag.StreamThinkingDeltas = prevStreamThinkingDeltas
	}()

	// B12: bound this turn by the WEB turn budget — set on the shared agent for
	// exactly this turn and restored right after, both inside the s.mu turn
	// domain (no other turn can observe the momentary value). Restoring rather
	// than leaving it set keeps one knob from silently rewriting the agent's
	// own GOPI_TIMEOUT_SEC resolution for the rest of the process. 0 means
	// unlimited — the agent's own default, so an unset deployment is
	// byte-identical to pre-B12.
	prevTurnTimeout := ag.Timeout
	ag.Timeout = webTurnTimeout()
	defer func() { ag.Timeout = prevTurnTimeout }()

	var result string
	var runErr error
	if len(images) > 0 {
		// R5 image turn: switch the shared agent to the vision model for
		// exactly this turn and restore it after (both inside the s.mu
		// turn domain — no other turn can observe the switch). Errors
		// restore the model too via the unconditional assignment below.
		prevModel := ag.Model
		ag.Model = s.visionModel
		defer func() { ag.Model = prevModel }()
	}
	// WHICH FACE this turn is answered on rides the turn itself (not a process
	// global): the chat write boundary reads it to decide what a `prompt` answer
	// MEANS here — this page's banner already promises "no approval gate", so a
	// question addressed to the operator who typed the line has no reader
	// (internal/tools/chat_policy_carrier.go).
	carrier := s.webChatCarrier()

	// BROWSER DISCONNECT != SERVER RELEASE (epic 01a16dab). A tab that closes
	// mid-stream leaves an http.Request whose context is cancelled while the
	// turn keeps running: nothing else watches it, so the round burns the whole
	// model call AND holds s.mu for it — the next request queues behind a
	// response nobody can receive. Measured on the rig before this watcher
	// existed: a client that hung up after the first delta still cost the server
	// the ENTIRE slow stream, and the following turn waited 1.94s for the lock.
	//
	// The watcher cancels THIS turn only (run-scoped, never the process-wide
	// Interrupt), and it stops when the turn ends — childDone closes on every
	// return path, so no goroutine outlives the request.
	//
	// WHY NOT the shape an earlier attempt used (a signal channel closed by
	// both sides): that PANICKED with "close of closed channel" when the client
	// dropped and the two closers raced. Here every party sends ONE value on a
	// buffered channel and only the handler closes anything, so there is no
	// second closer to race — the panic is structurally impossible rather than
	// avoided by timing.
	turnDone := make(chan struct{})
	defer close(turnDone)
	go s.abandonTurnWhenClientLeaves(r, turnDone)

	// THE ROW'S STAMP, handed to the writer for THIS turn only (consume-once,
	// like TurnSource beside it): the same instant the banner was given above.
	ag.TurnOpenedAt = turnOpenedAt
	if s.tools {
		// Tool-enabled turn: the SAME bounded ReAct loop + full toolset
		// `gopi chat --tools` runs (zero new engine — R2).
		result, runErr = ag.ChatOpsOnCarrier(carrier, msg, seed, images...)
	} else {
		result, runErr = ag.ChatOnCarrier(carrier, msg, seed, images...)
	}
	// Reset the per-turn display callbacks so a future turn (or finalize)
	// never streams into a dead response writer.
	ag.OnText = nil
	ag.OnToolEvents = nil
	ag.OnUserNotice = nil

	// Always emit a terminal event so the browser knows the turn ended,
	// success or error alike.
	// The terminal frame repeats the turn it belonged to, so a client that
	// joined late — or a page whose `turn` frame was dropped by a reconnect —
	// can still label the answer it just received. One number, two frames, one
	// source.
	doneBody := map[string]interface{}{"result": result}
	if turnIsTurn {
		doneBody[webSessionTurnField] = turnNo
		// The moment rides the terminal frame too, for a client whose turn
		// frame a reconnect dropped: the same pair (ordinal, moment) on both
		// frames, from the same two variables.
		doneBody[webSessionTurnOpenedField] = sessionevent.FormatTurnStamp(turnOpenedAt)
	}
	if runErr != nil {
		doneBody["error"] = runErr.Error()
	}
	done, _ := json.Marshal(doneBody) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeSSEFrame(w, webEventDone, done)
	flusher.Flush()

	// Accumulate run stats across turns for the final session_end + task
	// ledger close at server shutdown (mirrors runChat's per-turn bookkeeping).
	s.iterCount++
	if ag.Cost != nil {
		s.totalTokens = ag.Cost.TotalPromptTokens + ag.Cost.TotalCompletionTokens
	}

	// R1: capture the authoritative transcript for the next turn's seed —
	// after EVERY turn (success or error), mirroring `gopi chat`'s
	// `sess.history = append([]deepseek.Message{}, ag.Ctx.Messages...)`.
	s.history = append([]deepseek.Message{}, ag.Ctx.Messages...)

	// M4.6 钩子分流: the web chat registers into the CHATS table (mode=web),
	// NOT the tasks ledger — same split as `gopi chat`. Only after a
	// successful turn (runErr == nil) so a first-turn API failure leaves no
	// row, exactly like gopi chat.
	if runErr == nil && ag.Cost != nil {
		// The browser's line is the operator's own (a POSTed message), so this
		// row's head is theirs to claim — stated as the face, not defaulted.
		ag.RegisterChatSession("web", msg, inputsource.Human)
		ag.RecordChatTurn(ag.Cost.TotalPromptTokens, ag.Cost.TotalCompletionTokens, ag.Cost.TotalCacheHitTokens+ag.Cost.TotalCacheMissTokens)
	}
	// (Log paths are printed once at server startup in runWeb, not per turn —
	// the whole web run is ONE session, so there is nothing new to print here.)
}

// webLockPollInterval is how often a bounded lock attempt re-tries. A named
// constant (Rule 1) because it is a policy number, not an implementation
// detail: a shorter interval costs scheduling wakeups during a shutdown nobody
// is watching, a longer one adds latency to the common case where the turn has
// already unwound.
const webLockPollInterval = 20 * time.Millisecond

// lockWithin takes mu, giving up after d. It reports whether the lock was
// acquired; the caller MUST NOT Unlock when it reports false.
//
// Unlike a Lock parked in its own goroutine this form leaves nothing behind on
// failure: every waiter here is the caller's own goroutine, so a timeout unwinds
// with nothing still parked on the mutex — a parked goroutine would take the
// lock AFTER the close and keep it for the life of the process.
func lockWithin(mu *sync.Mutex, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if mu.TryLock() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(webLockPollInterval)
	}
}

// abandonTurnWhenClientLeaves watches the request channel for the turn in
// handleChat and abandons THAT turn when the browser hangs up. It returns as
// soon as the turn ends (done closes) or the request is over, so it never
// outlives the request that started it.
//
// A server shutdown is NOT this watcher's business: it cancels turns through
// abandonInFlightTurn, and r.Context() also dies when the server closes, so both
// paths end in the same run-scoped cancel with different recorded reasons.
func (s *webServer) abandonTurnWhenClientLeaves(r *http.Request, done <-chan struct{}) {
	if s.agent == nil {
		return
	}
	select {
	case <-done:
		return
	case <-r.Context().Done():
	}
	// The turn may have finished between the two selects; InterruptRunBy then
	// reports false and there is nothing to abandon (Rule 15: the answer is
	// reported, not assumed away).
	if s.agent.InterruptRunBy(agent.InterruptReasonClientGone) {
		fmt.Fprintf(os.Stderr, "gopi web: client left mid-turn — abandoning it so the session's turn lock is released.\n")
	}
}

// abandonInFlightTurn cancels the turn running RIGHT NOW, if any, so a shutdown
// never waits for a model round the peer may not answer.
//
// WHY IT IS A SHUTDOWN STEP (measured, epic 01a16dab): the finalizer closes the
// session under s.mu, and a turn holds s.mu for its whole round-trip, so without
// this call a Ctrl+C during a turn waits forever. The rig measured a SIGTERMed
// server still alive 30s later and SIGKILLed by its own supervisor, with no
// session_end and a bash child left behind; the goroutine dump named both sides:
// finalizeChatSession in sync.Mutex.Lock, chatOnceAttempt in chan receive.
//
// InterruptRunBy (not Interrupt) because interruptBase is cancelled ONCE and
// never re-armed: it exists for "this process is going away", while the
// run-scoped cancel can be released afterwards without poisoning every later
// turn of a server that stays up (see runScopedRoot). The reason carries the
// ORIGIN, so the abandoned turn's witness names a shutdown rather than an
// operator key nobody pressed.
func (s *webServer) abandonInFlightTurn() {
	if s.agent == nil {
		return
	}
	if s.agent.InterruptRunBy(agent.InterruptReasonShutdown) {
		fmt.Fprintf(os.Stderr, "gopi web: abandoning the in-flight turn so the session can close.\n")
	}
}

// finalizeChatSession closes the single persistent web chat session at server
// shutdown — the direct mirror of `gopi chat`'s finishChat: writes the ONE
// session_end (carrying the accumulated cost across all turns) and closes the
// session + raw logs via CloseChatSession, then flips the task-ledger row to
// completed/failed via FinishSessionTask with the same run stats. Called once
// when http.Serve returns, whether from a normal halt (err == nil) or an error
// shutdown, so a web chat conversation always gets its single session_end and
// ledger close — never leaked across restarts, never multiplied per turn.
func (s *webServer) finalizeChatSession(graceful bool) {
	ag := s.agent
	if ag == nil {
		return
	}
	// Wait for any in-flight turn to finish (the per-turn lock), then close
	// the single session on the same serialized mutex so a turn never writes
	// after the session_end.
	//
	// The wait is BOUNDED (measured leak, epic 01a16dab): a turn whose peer
	// never answers holds this lock for as long as the socket lives, and an
	// unbounded Lock made the whole shutdown depend on it — the caller's own
	// supervisor SIGKILLed a SIGTERMed server after 30s, so no session_end was
	// written at all. runWeb abandons the turn before calling here, which is
	// what normally releases the lock; the bound is the belt to that
	// suspenders, and hitting it is REPORTED rather than silent (Rule 15) —
	// closing the session while a turn may still be unwinding is a stated
	// degradation, never a quietly tolerated one.
	if !lockWithin(&s.mu, webShutdownGrace) {
		fmt.Fprintf(os.Stderr, "gopi web: the in-flight turn did not release the session within %v — closing the session anyway (a turn may still be unwinding)\n", webShutdownGrace)
	} else {
		defer s.mu.Unlock()
	}
	elapsed := time.Since(s.startedAt).Seconds()
	ag.CloseChatSession(0, s.iterCount, s.totalTokens, elapsed,
		agent.EndCause{Reason: sessionevent.SessionEndReasonComplete})
	// M4.6 钩子分流: close the chats-table row (mode=web) at shutdown.
	ag.CloseChatRow(0)
	if graceful {
		fmt.Fprintf(os.Stderr, "gopi web: server stopped, chat session finalized.\n")
	}
}

// serveSlashCommand executes one locally-dispatched slash command (R4) and
// streams the result as a single `event: command` frame. Called from
// handleChat with s.mu held; never touches the model, s.history, or the
// task ledger. Unknown commands stream a command frame carrying the error
// text (the browser renders it as a command node) — not an HTTP error, so
// the page's stream parser stays uniform.
func (s *webServer) serveSlashCommand(w http.ResponseWriter, msg string) {
	arts := &CommandArtifacts{}
	// REBASE NOTE (delivery round 2/3): this is the UNION of two independent
	// seams that meet on ONE call site. origin/master's b4c3b474 (R7) needs the
	// PRODUCT SINK (ctx.Artifacts — the /text2img pages that ride the next
	// turn); this branch needs the CARRIER'S OWN FACTS (ctx.Session — what
	// /info and /context describe). Both are fields of the SAME CommandContext,
	// so the carrier fills both and the dispatch is unchanged:
	// webCommandContext() supplies the session facts, the sink is attached to
	// it here. Dropping either side would silently delete a landed capability
	// (the terminal REPL keeps passing a nil sink: it has nowhere to put a
	// product, and Artifacts' nil receiver is a no-op by design).
	//
	// THE CARRIER'S OWN FACTS TRAVEL WITH THE CALL (epic 01a1b818-e02f): /info
	// and /context describe THIS session, and this server holds the same values
	// the chat REPL does — one persistent agent's session logger, its raw HTTP
	// log, its ground. Filled per command so a reader sees the facts of the
	// moment, never a snapshot taken at startup.
	ctx := s.webCommandContext()
	ctx.Artifacts = arts
	out, runErr := execSlashCommand(msg, ctx)
	frame := map[string]interface{}{"command": msg}
	if runErr != nil {
		frame["error"] = runErr.Error()
	} else {
		frame["output"] = out
	}
	// THE PRODUCTS OF THE COMMAND, ATTACHED (the /text2img line that used to
	// dead-end at a path string). A command that rendered files hands them back
	// through ctx.Artifacts; each page becomes one webChatImage — the SAME
	// {media_type,data_b64} object /api/shot returns and /api/chat accepts, so
	// the page pushes it into images[] and the next turn ships it with no
	// translation step.
	//
	// A page that cannot be read back is an ERROR FRAME, never a silent path
	// (Rule 15): the render succeeded, so the operator must be told the picture
	// exists but cannot ride the conversation, and why.
	var attached []webChatImage
	if len(arts.PNGs) > 0 {
		var note string
		attached, note, runErr = webSlashCommandImages(arts.PNGs)
		if runErr != nil {
			delete(frame, "output")
			frame["error"] = runErr.Error()
		} else {
			frame[webCommandImagesField] = attached
			if note != "" {
				frame["output"] = out + "\n" + note
			}
		}
	}
	payload, _ := json.Marshal(frame) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeSSEFrame(w, webEventCommand, payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// webCommandImagesField is the command frame's optional image member: an ARRAY
// of webChatImage (the /api/shot object shape, one entry per rendered page).
// Named (Rule 1b) because the writer above and the page's parser both spell it.
const webCommandImagesField = "images"

// webSlashCommandImages reads the files a slash command rendered into the
// carrier's attachment shape.
//
// THREE refusals, all explicit (Rule 15) — the caller turns each into an error
// frame carrying the path it is about:
//
//  1. the file cannot be read at all (removed between render and attach, a
//     permission problem, an I/O error);
//  2. it exceeds webMaxChatImageBytes — the SAME 32 MiB ceiling /api/chat
//     enforces, so a file this reader accepts is one the turn can ship;
//  3. it is not a PNG. The renderer writes PNGs, so bytes without the PNG
//     signature mean the file is not the picture the summary claims.
//
// It reuses validateWebImage rather than restating the four checks, so "what
// this path produces" and "what a turn accepts" cannot drift apart (a second
// copy would be a second contract — the reason handleShot checks its capture
// through the same function).
//
// The returned note is the ATTACHMENT BUDGET's honest half: a render that
// produced more pages than one turn may carry attaches the first
// webMaxImagesPerRequest and says how many fell out. Truncation without a
// sentence would be exactly the silent path this change exists to remove.
func webSlashCommandImages(paths []string) ([]webChatImage, string, error) {
	out := make([]webChatImage, 0, len(paths))
	for i, path := range paths {
		if i >= webMaxImagesPerRequest {
			note := fmt.Sprintf("⚠ %d 页未附上：单轮最多携带 %d 张图，请分次渲染或用 --cols 减少页数",
				len(paths)-webMaxImagesPerRequest, webMaxImagesPerRequest)
			return out, note, nil
		}
		img, err := webSlashCommandImage(path)
		if err != nil {
			return nil, "", err
		}
		out = append(out, img)
	}
	return out, "", nil
}

// webSlashCommandImage reads ONE rendered page into the carrier's attachment
// shape, refusing unreadable, oversize and non-PNG files by name.
func webSlashCommandImage(path string) (webChatImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return webChatImage{}, fmt.Errorf("渲染的图片无法读取（%s）：%w — 图片已在磁盘上，但没有随命令进入对话", path, err)
	}
	defer f.Close() //stylegate:ignore closing a file after a bounded read; there is no write to flush
	// Read at most one byte PAST the ceiling: that is how an oversize file is
	// DETECTED without reading a byte more than the budget allows (a plain
	// ReadAll would pull a 200 MB file into memory to discover it is too big).
	data, err := io.ReadAll(io.LimitReader(f, webMaxChatImageBytes+1))
	if err != nil {
		return webChatImage{}, fmt.Errorf("渲染的图片无法读取（%s）：%w — 图片已在磁盘上，但没有随命令进入对话", path, err)
	}
	if len(data) > webMaxChatImageBytes {
		return webChatImage{}, fmt.Errorf("渲染的图片超过 %d 字节上限（%s）— 图片已在磁盘上，但没有随命令进入对话", webMaxChatImageBytes, path)
	}
	if !bytes.HasPrefix(data, []byte(gifgen.PNGMagic)) {
		return webChatImage{}, fmt.Errorf("渲染结果不是 PNG（%s）— 图片已在磁盘上，但没有随命令进入对话", path)
	}
	img := webChatImage{MediaType: webShotMediaType, DataB64: base64.StdEncoding.EncodeToString(data)}
	if _, err := validateWebImage(img); err != nil {
		return webChatImage{}, fmt.Errorf("渲染的图片无法随命令进入对话（%s）：%w", path, err)
	}
	return img, nil
}

// SSE frame event names — the wire vocabulary the page's frames() parser
// switches on and the server writes (Rule 1b: never a bare literal on either
// side of the wire; declared once here, asserted by the web tests).
const (
	webEventDelta    = "delta"    // one streamed model text fragment
	webEventThinking = "thinking" // one RAW reasoning fragment (live, gated)
	webEventTool     = "tool"     // R2 tool activity (call/result)
	webEventDone     = "done"     // terminal frame of a model turn
	webEventError    = "error"    // carrier-level failure
	webEventCommand  = "command"  // R4 locally-executed slash command
	webEventQueued   = "queued"   // B2: this turn is waiting on s.mu
	webEventNotice   = "notice"   // a SERVER-started turn (session inbox delivery)
	// webEventWarning is a statement for the OPERATOR about the turn in flight,
	// not about a turn that started: today the only producer is context pressure
	// (internal/agent/context_pressure.go). A separate name from webEventNotice
	// because the page renders the two differently — a notice opens an answer
	// bubble (a turn follows it), a warning does not.
	webEventWarning = "warning"
	webEventTurn    = "turn" // the session ordinal of the turn now STARTING
	// webSessionTurnField is the `done` frame's copy of that ordinal. It is a
	// DIFFERENT name from the `turn` frame's field on purpose: the frame's body
	// is `{"turn":N}` and the terminal frame is a result envelope that carries
	// the number as one more fact about the turn — one wire name per meaning
	// (Rule 1b), and the page's parser reads both by these constants' values.
	webSessionTurnField = "session_turn"
	// webSessionTurnOpenedField is the MOMENT the turn opened, carried beside
	// that ordinal on the `done` frame and on every `/api/transcript` row that
	// has one. Its VALUE is what sessionevent.FormatTurnStamp renders — the
	// same string the terminal band prints — so the two faces cannot drift.
	webSessionTurnOpenedField = "session_turn_opened_at"
)

// webEventsPath is the standing SSE endpoint the page holds open with
// EventSource (cli_web_notice.go). It is a GET that never answers a turn; it
// streams the frames of turns the SERVER started by itself — the browser's
// substitute for the parked `select` the console waits on.
const webEventsPath = "/api/events"

// webEventsKeepAliveInterval is how often an otherwise idle /api/events
// connection writes an SSE COMMENT frame. A comment is ignored by EventSource
// and by the page's parser, and it exists so a proxy or a browser does not reap
// a connection that has been legitimately quiet for minutes.
const webEventsKeepAliveInterval = 25 * time.Second

// webEventsKeepAliveComment is that frame, spelled as SSE itself: a line
// starting with ':' is a comment and carries no event.
const webEventsKeepAliveComment = ": keepalive\n\n"

// webPlacementQueued is the queued frame's typed placement value — the ONE
// borrow taken from dsh's `QueuedInboxItem.placement` (events.ts): a message
// that arrives while a turn runs is TYPED as queued rather than silently
// swallowed. Carried as a plain SSE field; no RPC layer.
const webPlacementQueued = "queued"

// webPlacementSteering is the second placement value (B12): a mid-turn message
// that ADDRESSES the run in flight rather than becoming a turn of its own. The
// page types it on the queued entry it renders; the server sees the same typed
// value on the wire through webSteerPrefix, so both carriers spell one idea one
// way (dsh's `placement: 'queued' | 'steering'`).
const webPlacementSteering = "steering"

// webSteerPrefix is the sentinel the page prefixes a steering message with
// (Alt+Enter / Alt+Send). It is carrier metadata, NOT content: handleChat
// strips it before the turn, so the model only ever sees the user's own words.
// The page's STEER_PREFIX is this same string (asserted by the regression
// test).
const webSteerPrefix = "!steer "

// writeSSEHeaders writes the streaming response preamble every turn shares:
// tell the browser to keep the connection open and treat the body as typed
// events, and keep the response unchunked-by-proxy (SSE must not be gzipped).
// It does NOT flush — the caller flushes right after, before any slow work.
func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// writeSSEFrame writes one SSE frame (event name + JSON payload + the blank
// line that terminates it). The single framer for every server-side event; a
// caller that needs the bytes on the wire before more work happens flushes
// itself.
func writeSSEFrame(w io.Writer, event string, payload []byte) {
	io.WriteString(w, "event: "+event+"\n")
	io.WriteString(w, "data: "+string(payload)+"\n\n")
}

// webQueuedPayload is the `queued` frame's body (B2): the typed placement plus
// the human notice the page renders in the pending bubble.
func webQueuedPayload() []byte {
	p, _ := json.Marshal(map[string]string{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"placement": webPlacementQueued,
		"notice":    webBusyWaitNotice,
	})
	return p
}

// webThinkingPayload is the `thinking` frame's body: {"text": <one raw
// reasoning fragment>}, JSON-encoded so newlines and UTF-8 survive the SSE
// framing exactly as the delta frame already does.
//
// THE FRAGMENT IS CARRIED VERBATIM — no `[thinking] ` marker, no SGR, no
// trailing newline. The marker is the TERMINAL's way of separating reasoning
// from the answer on a stream a person reads in place (see
// Agent.thinkingMarkersFor); this frame's reader is a RENDERER, which draws the
// separation itself, and a marker byte it has to strip is a byte it could
// render as content on a terminal that counts cells.
func webThinkingPayload(text string) []byte {
	p, _ := json.Marshal(map[string]string{"text": text}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return p
}

// webChatResetPath is the B1 reset endpoint — the SERVER-side half of the
// `/new` affordance. Declared as a constant so the route table, the page JS
// and the regression test cannot drift apart (Rule 1b: a path carrying
// meaning is not typed inline).
const webChatResetPath = "/api/chat/reset"

// webChatInterruptPath is the route that abandons the turn in flight. It exists
// because closing an SSE connection CANNOT stop the turn behind it: handleChat
// deliberately ignores client disconnect (a r.Context().Done() watcher was
// removed after it panicked "close of closed channel"), and the process-wide
// Agent.Interrupt() is cancel-once and never re-armed, so a long-lived server
// cannot use it per turn. An interactive client therefore needs a route that
// cancels exactly the run in flight — without one, "stop this turn" leaves the
// engine working, holding its single-turn lock and making the operator's next
// message wait with nothing on screen accounting for the wait (the `gopi ui`
// freeze). The client's spelling of it is uiclient.InterruptPath, pinned to this
// constant by cli_ui_wire_guard_test.go.
const webChatInterruptPath = "/api/chat/interrupt"

// webResetBodyMessage is the reset frame's payload text. Named (Rule 1b)
// because both the endpoint and the regression test assert this exact string.
const webResetBodyMessage = "conversation reset — the transcript and per-turn state were cleared"

// webResetCleared is the page's sentinel: when /new resolves to this text (the
// shared command table's own /new output — the single source of truth, echoing
// webResetBodyMessage) the browser wipes #log after the command node renders.
// The page never resets on its own: the server is authoritative (R1).
const webResetCleared = webResetBodyMessage

// webBusyWaitNotice is the `queued` frame's payload (B2). It is written BEFORE
// s.mu is taken, so a second concurrent POST learns it is waiting on the
// in-flight turn instead of hanging byte-less behind the lock.
const webBusyWaitNotice = "waiting for the in-flight turn to finish…"

// webMoreFlag is the flag shorthand the page's `/` menu advertises for "there
// are more candidates than the menu shows": the page's MENU_MORE_FLAG holds the
// same spelling (the regression test pins the pair).
const webMoreFlag = "+more"

// webMarkdownFence is the fence delimiter of the page's minimal markdown (B8).
// The page spells the same delimiter by code point (MD_TICK_CHARCODE) because
// the page itself is a Go raw string (backtick-delimited, Rule 4); this constant
// is the server-side truth the regression test pins that construction to.
const webMarkdownFence = "```"

// webMarkdownBold is the intra-line emphasis delimiter of the page's minimal
// markdown (B8) — and it is THE SAME MARKER the terminal's emphasis layer reads,
// because it is lineedit.BoldMarker itself, not a second spelling of it (Rule 14:
// one definition, checked). The page spells the delimiter by code point
// (MD_ASTERISK_CHARCODE) for the same reason it spells the fence that way, and the
// regression test pins the page's construction to this value, so `gopi web` and
// `gopi ui` cannot come to different conclusions about what `**` means.
const webMarkdownBold = lineedit.BoldMarker

// webTriggerSlash is the input trigger that opens the SLASH-COMMAND menu: the
// character typed at the start of the composer, and the one the shared command
// table keys its words with. Declared once so the detector, the JSON endpoint
// the page fetches its candidates from, and the tests cannot drift.
const webTriggerSlash = "/"

// webTriggerAt is the input trigger that opens the FILE-REFERENCE menu (@).
const webTriggerAt = "@"

// webMenuSourceSlash / webMenuSourceFile name the two candidate sources the
// detect core resolves (Rule 1b: the kind is compared and rendered, never a
// bare literal). The page renders an inert label per source, so the wording
// lives on the server with its single definition.
const (
	webMenuSourceSlash = "slash"
	webMenuSourceFile  = "file"
)

// webMenuMaxItems caps the candidate list AND the JSON array the endpoint
// ships. One budget for the API and the view (a client cap and a server cap
// that disagree is a defect, not a preference).
const webMenuMaxItems = 8

// webFileListMaxEntries bounds the directory walk behind the @ menu: a big
// repository must not turn one keystroke into an unbounded scan. The walk stops
// as soon as it has found the candidates it can possibly rank.
const webFileListMaxEntries = 2000

// webFileListMaxDepth bounds that walk by depth as well — node_modules-style
// trees are skipped by NAME (webFileListSkipDirs) and deep trees by depth, so
// the bound holds even when every directory is unique.
const webFileListMaxDepth = 6

// webFileListSkipDirs are the directory names the @ menu never descends into:
// build output and VCS internals, where a user's own files never live.
var webFileListSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
}

// webMenuMoreSuffix is appended to the menu's candidate count when more
// candidates exist than webMenuMaxItems (the page shows it verbatim).
const webMenuMoreSuffix = " " + webMoreFlag

// webMenuCandidates is the detect core's output: the query the composer holds,
// the source that answered ("" = no menu), and the ranked candidates. It is
// BOTH the endpoint's JSON payload and the object the page's `detectMenu`
// contracts to return, so the pure decision has exactly one shape in both
// carriers.
type webMenuCandidates struct {
	// Trigger is the character that opened the menu (webTriggerSlash /
	// webTriggerAt); empty when no menu is open.
	Trigger string `json:"trigger"`
	// Query is the text after the trigger (the composer's filter).
	Query string `json:"query"`
	// Source is webMenuSourceSlash / webMenuSourceFile ("" = no menu).
	Source string `json:"source"`
	// Candidates are the ranked matches, capped at webMenuMaxItems.
	Candidates []string `json:"candidates"`
	// Total is how many candidates matched before the cap (the page shows
	// webMoreFlag when Total > len(Candidates)).
	Total int `json:"total"`
}

// detectSlashMenu is the SERVER half of the shared detect core: the query a
// composer holds → the slash commands it should offer. It reads the EXISTING
// shared command table (slashCommandNames — the registry `gopi chat` types
// into), never a second list, so a command added to the table appears in the
// browser menu with no further edit.
func detectSlashMenu(query string) []string {
	q := strings.ToLower(query)
	names := slashCommandNames()
	matches := make([]string, 0, len(names))
	for _, n := range names {
		if q == "" || strings.HasPrefix(n, q) {
			matches = append(matches, n)
		}
	}
	// A prefix query that matches nothing falls back to the whole table: a
	// partial word is a filter, never a dead end (the user typed `/` to
	// discover, so the menu keeps discovering).
	if len(matches) == 0 {
		matches = append(matches, names...)
	}
	return matches
}

// detectFileMenu is the @ half of the detect core: the path fragment a
// composer holds → repo-relative file candidates, ranked by the file NAME so a
// basename query finds its file without typing directories. The walk is bounded
// three ways (entries, depth, skipped directory names) because it runs off a
// keystroke, and it shares slashSearchRoot with /code-search so both the menu
// and the tool look at the same tree.
//
// It returns EVERY match the bounded walk saw (never a truncated list), so the
// caller can report an honest total and cap the shipped page itself — a walk
// that stopped at the first webMenuMaxItems matches could never tell the page
// "+more", because it cannot know more exist.
func detectFileMenu(query string) []string {
	root := slashSearchRoot()
	q := strings.ToLower(query)
	// A fragment with a path separator filters directories too (the user is
	// walking a tree); a bare fragment matches file BASENAMES only, so `@` in
	// the composer offers files, not directories.
	baseOnly := !strings.Contains(q, string(os.PathSeparator)) && !strings.Contains(q, "/")
	matches := make([]string, 0, webMenuMaxItems)
	seen := 0
	// The walk's own error is the root being unreadable: the menu then offers
	// nothing rather than failing — the same degradation an unreadable subtree
	// takes inside the callback (handled there, never swallowed silently).
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is skipped, not fatal: the menu degrades to
			// what it can see (Rule 15 — the walk's error is handled by the
			// caller's bounded contract, never silently swallowed mid-list).
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if d.IsDir() {
			if webFileListSkipDirs[d.Name()] || depth >= webFileListMaxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > webFileListMaxEntries {
			// The walk's own budget: stop descending, but return what was
			// already matched so the caller still gets a truthful count.
			return filepath.SkipAll
		}
		name := d.Name()
		candidate := name
		if !baseOnly {
			candidate = filepath.ToSlash(rel)
		}
		if q != "" && !strings.HasPrefix(strings.ToLower(candidate), q) {
			return nil
		}
		matches = append(matches, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && len(matches) == 0 {
		// An unreadable root is an empty menu, never an open error: the trigger
		// is a keystroke and the honest answer is "no candidates".
		return []string{}
	}
	return matches
}

// webMenuTriggerArg / webMenuQueryArg are the menu endpoint's two query
// parameters (Rule 1b: the page builds the URL from them and the handler reads
// them by the same names).
const (
	webMenuTriggerArg = "trigger"
	webMenuQueryArg   = "q"
)

// webMenuPath is the B9 candidate endpoint. Named because the page's fetch
// target, the route table and the regression test must agree on one spelling.
const webMenuPath = "/api/menu"

// webChatPath is the SSE turn endpoint. Named for the same reason as
// webMenuPath: the route table and the page's fetch target spell ONE value
// (Rule 1b — a path carrying meaning is never typed inline).
const webChatPath = "/api/chat"

// webTranscriptPath is the R4 refresh-replay endpoint (the server-owned
// transcript the page replays after a reload).
const webTranscriptPath = "/api/transcript"

// capMenuList truncates a candidate list to the ONE menu budget. The full
// count travels separately (webMenuCandidates.Total), so the view can say "and
// more" without a second request.
func capMenuList(all []string) []string {
	if len(all) > webMenuMaxItems {
		return all[:webMenuMaxItems]
	}
	if all == nil {
		return []string{}
	}
	return all
}

// handleMenu answers GET /api/menu with the candidates for one composer query
// (B9). The decision itself runs in the shared detect core in the browser —
// this endpoint only RESOLVES candidates, so the page needs no copy of the
// command table and no file listing of its own. It takes no lock: it reads the
// static command table and the file tree, never the conversation.
func (s *webServer) handleMenu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed (GET "+webMenuPath+")", http.StatusMethodNotAllowed)
		return
	}
	trigger := r.URL.Query().Get(webMenuTriggerArg)
	query := r.URL.Query().Get(webMenuQueryArg)
	out := webMenuCandidates{}
	switch trigger {
	case webTriggerSlash:
		out.Source = webMenuSourceSlash
		all := detectSlashMenu(query)
		out.Total = len(all)
		out.Candidates = capMenuList(all)
	case webTriggerAt:
		out.Source = webMenuSourceFile
		all := detectFileMenu(query)
		out.Total = len(all)
		out.Candidates = capMenuList(all)
	default:
		// An unknown trigger is an empty answer, not an error: the page may
		// ask about any keystroke and the honest reply is "no menu".
		out.Candidates = []string{}
	}
	out.Trigger = trigger
	out.Query = query
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(out) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	w.Write(payload)                //stylegate:ignore a failed write to a menu JSON body is the client's disconnect, not a server fault
}

// webFollowThreshold is the page's bottom-follow gate in CSS px — the same
// number the page JS holds as FOLLOW_THRESHOLD, ported from deepseek-harness
// (ChatView.tsx:25, FOLLOW_THRESHOLD = 24). Declared here as well so the value
// is NAMED on the server side of the wire (Rule 1a) and the test can pin the
// page's copy to it: within this threshold the reader counts as "at the
// bottom" and the stream follows; beyond it the reader keeps their position
// and the "jump to latest" affordance appears.
const webFollowThreshold = 24

// handleChatReset clears the server-owned conversation (B1): s.history — the
// authoritative transcript every turn seeds from — plus the per-turn run stats
// (iterCount/totalTokens, which feed the single session_end at shutdown).
//
// Lock discipline (invariant 2: nothing in Table B may hold s.mu longer than a
// turn does): the reset is a slice reset, so it takes s.mu for O(1) work —
// strictly less than a turn holds it — and never touches the agent (no model
// call, no session_end: the single session id/JSONL/raw log live for the whole
// server run and are closed once by finalizeChatSession).
//
// The agent's own Ctx.Messages is deliberately NOT rewritten here: every turn
// re-seeds it from s.history (`a.Ctx.Messages = append([]deepseek.Message{},
// history...)` in Agent.Chat/ChatOps), so an empty history makes turn N+1
// amnesiac by construction rather than by an unverifiable field poke.
//
// The browser affordance is the shared slash-command table's `/new` (R4 — one
// table, both carriers, never a second dispatch path); this endpoint is the
// programmatic twin the tests and scripts drive.
func (s *webServer) handleChatReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST "+webChatResetPath+")", http.StatusMethodNotAllowed)
		return
	}
	// Refuse once the shutdown finalizer is closing the session — the same
	// interlock handleChat uses (a reset queued behind the finalizer's mutex
	// would be pointless work, not a deadlock).
	if s.shuttingDown {
		http.Error(w, "{\"error\":\"server shutting down\"}", http.StatusServiceUnavailable)
		return
	}
	cleared := s.resetConversation()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"ok":               true,
		"message":          webResetBodyMessage,
		"history_messages": cleared,
	})
	w.Write(payload) //stylegate:ignore a failed write to a reset JSON body is the client's disconnect, not a server fault
}

// handleChatInterrupt abandons the turn in flight: it cancels the CURRENT run's
// context and answers a small JSON ack.
//
// It deliberately does NOT take s.mu. The whole point is to reach a turn that
// HOLDS that lock; a handler queueing behind it would be served only after the
// very turn it was asked to abandon — the opposite of the request. Cancelling is
// enough: the turn's ctx unwinds at its next round boundary (or its in-flight
// stream aborts), and the turn releases the lock through its own defer.
func (s *webServer) handleChatInterrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST "+webChatInterruptPath+")", http.StatusMethodNotAllowed)
		return
	}
	// The same shutdown interlock the reset route uses: once the finalizer is
	// closing the session there is no turn left to abandon.
	if s.shuttingDown {
		http.Error(w, "{\"error\":\"server shutting down\"}", http.StatusServiceUnavailable)
		return
	}
	abandoned := s.agent != nil && s.agent.InterruptRun()
	payload, _ := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"ok":          abandoned,
		"interrupted": abandoned,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(payload) //stylegate:ignore a failed write to an interrupt ack is the client's disconnect, not a server fault
}

// resetConversationLocked is the ONLY mutation of the reset state: it clears
// the server-owned transcript and the per-turn run stats and returns how many
// messages were dropped. The caller already holds s.mu (handleChat's slash
// path, where the lock is part of the turn domain) — see resetConversation for
// the locking entry point.
func (s *webServer) resetConversationLocked() int {
	cleared := len(s.history)
	// s.history == nil (not an empty slice) is the documented empty state: the
	// turn seed maps nil → []deepseek.Message{} before handing it to the agent.
	s.history = nil
	// Per-turn run stats reset with it, so a post-reset session_end reports the
	// turns that actually produced the (now discarded) conversation.
	s.iterCount = 0
	s.totalTokens = 0
	return cleared
}

// resetConversation clears the conversation STRICTLY under s.mu and returns
// how many messages were dropped. Extracted so the slash-command route (`/new`,
// resolved inside handleChat with s.mu already held) and the HTTP endpoint
// share ONE mutation — the lock is never re-entered, and there is no second
// code path that could clear half the state.
func (s *webServer) resetConversation() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resetConversationLocked()
}

// isResetCommand reports whether a slash input addresses the conversation
// reset: the shared table's word (webClearCommandName — the canonical B1
// spelling, used by the page) or the chat REPL's spelling (webClearAliasName),
// accepted so one reset has one meaning in both carriers.
func isResetCommand(input string) bool {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false
	}
	word := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	return word == webClearCommandName || word == webClearAliasName
}

// serveResetCommand performs the B1 reset for a slash input and answers with
// ONE `event: command` frame. Called from handleChat with s.mu held (so it
// never re-locks); the reset itself is O(1) — a slice drop — and never touches
// the model or the session id (the single session lives for the whole server
// run and is closed once by finalizeChatSession).
//
// The frame's text is the SHARED TABLE's own output for the word
// (execSlashCommand → webClearCommand), so the reset message has exactly one
// definition and the page's sentinel (webResetCleared) is that same string.
func (s *webServer) serveResetCommand(w http.ResponseWriter, input string) {
	cleared := s.resetConversationLocked()
	payload, _ := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"command": input,
		"output":  webResetOutput(),
		"cleared": cleared,
	})
	writeSSEFrame(w, webEventCommand, payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// webResetOutput resolves the reset command's user-facing text through the
// shared table (single source of truth). The fallback keeps the frame
// meaningful if the table entry is ever removed — the reset itself has already
// happened, so its acknowledgement must never be empty.
func webResetOutput() string {
	out, err := execSlashCommand("/"+webClearCommandName, CommandContext{})
	if err != nil || strings.TrimSpace(out) == "" {
		return webResetBodyMessage
	}
	return out
}

// webMaxChatImageBytes is the per-image decoded-size ceiling — the SAME
// 32 MiB budget tools.LoadImage enforces for file-based images (one
// validation rule, two entry points). The per-request budget is NOT this
// ceiling cut by the body cap: webMaxChatBodyBytes is DERIVED from this
// constant times webMaxImagesPerRequest, so a request this validator accepts
// always reaches it (see webMaxChatBodyBytes).
const webMaxChatImageBytes = 32 << 20 // 32 MiB

// webAllowedImageMediaTypes is the Anthropic vision media-type set (the
// same formats visionMediaTypes accepts for files).
var webAllowedImageMediaTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// validateWebImages decodes and validates every uploaded image (R5):
// supported media type, sane base64, decoded size ≤ the 32 MiB vision
// ceiling, and a hard count cap. Returns wire-ready ClaudeImageSources.
func validateWebImages(in []webChatImage) ([]*deepseek.ClaudeImageSource, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > webMaxImagesPerRequest {
		return nil, fmt.Errorf("too many images: %d (max %d per message)", len(in), webMaxImagesPerRequest)
	}
	out := make([]*deepseek.ClaudeImageSource, 0, len(in))
	for i, img := range in {
		src, err := validateWebImage(img)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i+1, err)
		}
		out = append(out, src)
	}
	return out, nil
}

// validateWebImage applies the ONE image contract to a single picture:
// supported media type, sane base64, non-empty, ≤ the 32 MiB vision ceiling.
//
// It exists as its own function because TWO producers ship images to this
// contract — a browser upload (validateWebImages, above) and the server-side
// page capture (handleShot, cli_web_shot.go) — and a second copy of these four
// checks would be a second answer to "is this shippable to the vision model".
// The capture path is checked by the page's OWN validator so a capture that
// comes back empty or truncated is refused at the endpoint, not at send time.
func validateWebImage(img webChatImage) (*deepseek.ClaudeImageSource, error) {
	mt := strings.ToLower(strings.TrimSpace(img.MediaType))
	if !webAllowedImageMediaTypes[mt] {
		return nil, fmt.Errorf("unsupported media type %q — supported: image/jpeg, image/png, image/gif, image/webp", img.MediaType)
	}
	dataB64 := strings.TrimSpace(img.DataB64)
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 data: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	if len(data) > webMaxChatImageBytes {
		return nil, fmt.Errorf("too large (%d bytes, max %d)", len(data), webMaxChatImageBytes)
	}
	return &deepseek.ClaudeImageSource{Type: "base64", MediaType: mt, Data: dataB64}, nil
}

// webMaxImagesPerRequest bounds one turn's image count (display sanity, not a
// wire limit — the whole turn rides in ONE user message carrying this many
// image blocks; the endpoint accepts 8/9/12 blocks in one message, measured
// 2026-11-07).
const webMaxImagesPerRequest = 8

// jsonString marshals s as a JSON string value (error-payload helper).
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `"internal error"`
	}
	return string(b)
}

// handleTranscript replays the server-owned conversation (R4): the browser
// re-fetches it after a refresh/reconnect so the display recovers what the
// SERVER remembers (a refresh never loses context — R1's display-half gap).
// The transcript mirrors the wire shape: user/assistant/tool messages in
// order, with image turns marked (base64 bodies are NOT re-shipped — a
// refresh replay doesn't need megabytes of pixels).
func (s *webServer) handleTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed (GET /api/transcript)", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	msgs := append([]deepseek.Message{}, s.history...)
	s.mu.Unlock()

	type transcriptEntry struct {
		Role       string              `json:"role"`
		Content    string              `json:"content,omitempty"`
		ToolCalls  []deepseek.ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string              `json:"tool_call_id,omitempty"`
		HasImage   bool                `json:"has_image,omitempty"`
		// SessionTurn is the SESSION's ordinal for this row when the row OPENS
		// A TURN — absent on every other row, because the record gives no number
		// to a row that opened nothing (assistant text, a tool result).
		//
		// IT IS WHY A REFRESHED PAGE CAN STILL DRAW THE BANNERS. The replayed
		// log is the server's in-memory history, which carries no turn axis of
		// its own; the numbers come from the session RECORD (cli_web_turn.go's
		// webTranscriptTurnOrdinals), so a continued conversation's history is
		// numbered 1..20 by the same authority that will call its next live turn
		// 21 — instead of being renumbered from 1 by a page counting bubbles.
		SessionTurn int `json:"session_turn,omitempty"`
		// SessionTurnOpenedAt is WHEN that turn opened, in the banner's shared
		// spelling (sessionevent.FormatTurnStamp), on the same rows that carry
		// SessionTurn. It is the RECORD's timestamp for the turn-opening row —
		// never a render-time clock — so a refresh of last week's conversation
		// redraws last week's moments.
		SessionTurnOpenedAt string `json:"session_turn_opened_at,omitempty"`
		// B5: the tool identity the replay needs (the deepseek.ToolCall wire
		// fields are json:"-" so a raw marshal ships an empty object), plus the
		// result call's digests. A tool row lands on the assistant row that
		// requested the call and is repeated on the tool result row by its call
		// id — so a refresh can rebuild `name` / `→ result` rows.
		Tools []transcriptTool `json:"tools,omitempty"`
	}
	entries := make([]transcriptEntry, 0, len(msgs))
	// namesByCallID resolves a tool RESULT message back to the tool that
	// produced it (the result row carries only tool_call_id). Built in one pass
	// over the same snapshot — no second read of s.history.
	// The turn ordinals come from the RECORD, in one pass, before the entries
	// are built (a mismatch is reported there, not silently absorbed here).
	turnOf, turnOpenedAt, turnErr := s.webTranscriptTurnMarks(msgs)
	if turnErr != nil {
		fmt.Fprintf(os.Stderr, "%s the replayed history is numbered by position only (%v)\n", webTurnReport, turnErr)
	}
	namesByCallID := make(map[string]string)
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && tc.Name != "" {
				namesByCallID[tc.ID] = tc.Name
			}
		}
	}
	for i, m := range msgs {
		e := transcriptEntry{Role: m.Role, Content: m.Content}
		if len(m.Images) > 0 {
			e.HasImage = true
		}
		e.ToolCalls = m.ToolCalls
		e.ToolCallID = m.ToolCallID
		e.Tools = transcriptTools(m, namesByCallID)
		e.SessionTurn = turnOf[i]
		e.SessionTurnOpenedAt = turnOpenedAt[i]
		entries = append(entries, e)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"messages": entries})
}

// transcriptTool is one replayed tool row (B5): the tool's name plus the two
// one-line digests the live wire carried. `call` is the call's argument digest
// (the live `event: tool` call summary); `result` is the matching result's
// first digestable line, present only on the tool-result row. Both go through
// the SAME rule the live frames use (agent.ToolCallSummary /
// agent.ToolResultDigest), so a replayed row and a streamed row cannot drift.
type transcriptTool struct {
	// ID is the tool call's id: it pairs a call row with its result row on
	// replay (the deepseek.ToolCall wire fields are json:"-" so the id must be
	// re-shipped explicitly — without it a refresh cannot join the two).
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Call   string `json:"call,omitempty"`
	Result string `json:"result,omitempty"`
}

// transcriptTools renders the tool rows for one transcript message:
//
//   - a message carrying tool_calls yields one row per call (name + the call's
//     argument digest — the same digest the live `event: tool` call frame sent,
//     from the loop's own summary helper);
//   - a tool RESULT message yields one row naming the tool that produced it
//     (resolved through its call id) plus the result digest.
//
// The two shapes are told apart by STRUCTURE (which field is populated), not by
// a role spelling, so this adds no second source of truth for role names — and
// any other message replays exactly as it did before B5.
func transcriptTools(m deepseek.Message, namesByCallID map[string]string) []transcriptTool {
	switch {
	case len(m.ToolCalls) > 0:
		out := make([]transcriptTool, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			out = append(out, transcriptTool{ID: tc.ID, Name: tc.Name, Call: agent.ToolCallSummary(tc)})
		}
		return out
	case m.ToolCallID != "":
		name := namesByCallID[m.ToolCallID]
		if name == "" {
			// An unresolvable call id (a truncated or hand-built transcript)
			// still replays as an unnamed row rather than dropping the result
			// (Rule 15).
			name = webTranscriptUnknownToolName
		}
		return []transcriptTool{{ID: m.ToolCallID, Name: name, Result: agent.ToolResultDigest(m.Content, webTranscriptDigestMaxRunes)}}
	default:
		return nil
	}
}

// webTranscriptUnknownToolName labels a tool-result row whose call id has no
// matching call in the same transcript (never silently dropped — Rule 15).
const webTranscriptUnknownToolName = "tool"

// webTranscriptDigestMaxRunes caps a replayed digest so one huge tool result
// cannot dominate the replay payload (pixels and full outputs are not what a
// refresh needs — the live stream already showed them). The rendering rule
// itself lives in the agent package (ToolResultDigest), which the live
// `event: tool` frame also uses, so a replayed row and a streamed row cannot
// drift apart.
const webTranscriptDigestMaxRunes = 200

// webBase64ExpansionRatioNum/Den express base64's expansion exactly (4 encoded
// characters per 3 decoded bytes) as a fraction, so the body budget below needs
// no float arithmetic (the integer division truncates — hence the padding slack).
const (
	webBase64ExpansionRatioNum = 4
	webBase64ExpansionRatioDen = 3
)

// webBase64PaddingSlack absorbs base64's trailing "=" padding (at most 2 bytes
// per image) with room to spare, so the truncating division above can never
// leave the derived body cap short of a legal request.
const webBase64PaddingSlack = 4 * webMaxImagesPerRequest

// webMaxChatTextBytes is the room the body budget leaves for the message text,
// the vision framing and the JSON envelope.
const webMaxChatTextBytes = 1 << 20 // 1 MiB

// webMaxChatBodyBytes caps a single chat request body. It is DERIVED from ONE
// budget: the per-image DECODED ceiling (webMaxChatImageBytes, the same 32 MiB
// tools.LoadImage enforces) times the image count, with base64's 4/3 expansion
// and its block padding added back — so the client's stated budget and the
// server's accepted body cannot disagree. Text and JSON framing ride in
// webMaxChatTextBytes on top. A flat hand-typed cap here is what let the page
// ship an image the server's own validator (validateWebImages) accepts while
// the decoder cut the body before it ever reached the validator.
const webMaxChatBodyBytes = webMaxChatImageBytes*webMaxImagesPerRequest*webBase64ExpansionRatioNum/webBase64ExpansionRatioDen + webBase64PaddingSlack + webMaxChatTextBytes

// webShutdownGrace bounds the graceful-shutdown wait for an in-flight turn
// (bug003 fix): after this the server force-closes conns so the session_end
// write can proceed. Model turns can be long; SSE writes are also killed by
// this since a stuck client connection must not block the close.
const webShutdownGrace = 5 * time.Second

// webChatPage is the single-file embedded chat UI. It is intentionally plain
// HTML + vanilla JS (no build step, no bundler, no framework — the same
// zero-dependency ethos as the Go backend). The SERVER owns the conversation
// (R1): the page only renders it, POSTing just the new message each turn. In
// --tools mode (R2) tool activity streams in as `event: tool` frames and
// renders as collapsible cards (call opens, result appends + collapses).
//
// The JS reads Server-Sent Events from fetch(): each `delta` event appends
// its text into the current assistant bubble; the final `done` event
// finalizes it and re-enables the input.
const webChatPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>gopi web chat</title>
<style>
  :root{--bg:#0f1117;--panel:#171a23;--bubble:#232837;--mine:#2b5cff;--fg:#e6e8ee;--muted:#8b92a5;--accent:#4c8bff;--red:#ff6b6b;--lb:#262b3a;--l:#2c3245;--e:#3a4358;--hr:#262e40;--cd:#0d1017;--bd:#161b26;--bf:#8a93a8;--tc:#1b2233;--tl:#2c3a5c;--tf:#9fc1ff;--cm:#14201a;--cl:#2c5c3a;--cb:#241417;--ck:#5c2c34;--co:#9fe0b8;--cr:#e09f9f;--eb:#5a2a2a;--eg:#2a1f1f;--q:#20283a;--ql:#4a5a86;--qf:#b9c6e8;--sf:#dce6ff;--qs:#9fb2d8;--qd:#8a94a8;--qc:#7a4a4a;--qc2:#ff9c9c;--qm:#c8b06a;--nt:#1d2530;--nl:#5a6b8a;--nf:#cfd9ea;--sl:#2b3550;--w:#ffd479;--wl:#4a3f1f;--wb:#241f10;--in:#6fb3ff;--il:#2c4a6c;--ok:#8ce99a;--tw:#ffd88a;--wr:#ffc457;--wr2:#5c4a1f;--gl:#2f5f3a;--rl:#b3313f;--rb:#7a1f28;--et:#ff8a8a;--sg:#eef3ff;--white:#fff}
  *{box-sizing:border-box}
  html,body{height:100%;margin:0}
  body{background:var(--bg);color:var(--fg);font:15px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;display:flex;flex-direction:column}
  header{padding:12px 20px;border-bottom:1px solid var(--lb);display:flex;align-items:baseline;gap:12px}
  header h1{font-size:16px;margin:0;font-weight:600}
  header .sub{color:var(--muted);font-size:12px}
  /* THE SERVER-SIDE STATUS (cli_web_state.go). A resident badge in the header:
     it is drawn from the server's own state frames, never from the page's
     guess about its own request, so it stays true for a turn the SERVER
     started (a gopi notify delivery) that this page never POSTed.
     One colour per state so a glance is enough; the tool name rides beside it
     because "waiting" with no noun is what the page had before. */
  #status{font-size:12px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;padding:2px 8px;border-radius:10px;border:1px solid var(--l);color:var(--muted);white-space:nowrap}
  #status[data-state="idle"]{color:var(--muted)}
  #status[data-state="responding"]{color:var(--in);border-color:var(--il)}
  #status[data-state="tool_running"]{color:var(--wr);border-color:var(--wr2)}
  #status .stool{color:var(--tw)}
  #status .sint{color:var(--ok);margin-left:6px}
  #status[data-interruptible="true"]{box-shadow:0 0 0 1px var(--gl) inset}
  #log{flex:1;overflow-y:auto;padding:20px;display:flex;flex-direction:column;gap:14px}
  .msg{max-width:80%;padding:10px 14px;border-radius:14px;white-space:pre-wrap;word-wrap:break-word}
  .msg.user{align-self:flex-end;background:var(--mine)}
  .msg.assistant{align-self:flex-start;background:var(--bubble);border:1px solid var(--l)}
  .msg.err{color:var(--red);border-color:var(--eb);background:var(--eg)}
  /* THE TURN BAND: a sticky separator between turns, so a long scrolled-back
     conversation says where turn 7 ended and turn 8 began instead of reading as
     one undifferentiated column. It sticks to the top of the log while its own
     turn is on screen — the number stays visible while you read the turn it
     labels, and scrolls away with it. */
  .turnband{position:sticky;top:0;align-self:stretch;z-index:2;
    background:linear-gradient(180deg,var(--bd),var(--bd) 60%,rgba(22,27,38,.86));
    color:var(--bf);font-size:12px;font-variant-numeric:tabular-nums;
    letter-spacing:.04em;padding:6px 4px 5px;border-bottom:1px solid var(--hr);
    text-transform:uppercase}
  .toolcard{align-self:flex-start;background:var(--tc);border:1px solid var(--tl);border-radius:10px;padding:8px 12px;font-size:13px;max-width:80%}
  .toolcard .thead{cursor:pointer;display:flex;gap:8px;align-items:baseline;color:var(--tf);font-weight:600}
  .toolcard .tarrow{display:inline-block;transition:transform .15s}
  .toolcard.closed .tarrow{transform:rotate(-90deg)}
  .toolcard .tbody{margin-top:6px;color:var(--muted);white-space:pre-wrap;word-wrap:break-word;font-family:ui-monospace,SFMono-Regular,monospace;font-size:12px}
  .toolcard.closed .tbody{display:none}
  .msg.command{align-self:center;background:var(--cm);border:1px solid var(--cl);border-radius:10px;padding:8px 12px;max-width:85%;font-family:ui-monospace,SFMono-Regular,monospace;font-size:13px;color:var(--co)}
  .msg.command.err{background:var(--cb);border-color:var(--ck);color:var(--cr)}
  .msg.command .cmdline{color:var(--accent);font-weight:600}
  .msg.command pre{margin:6px 0 0;white-space:pre-wrap;word-wrap:break-word;color:inherit}
  /* the engine identity line (identifyEngine): deliberately quiet — it is a
     fact about the session, not a message from the model */
  .msg.engine{align-self:center;background:transparent;border:1px dashed var(--e);color:var(--muted);font-family:ui-monospace,SFMono-Regular,monospace;font-size:12px;max-width:95%}
  #imgstrip{padding:0 20px;display:flex;gap:8px;flex-wrap:wrap}
  /* B3: the "jump to latest" affordance — shown only while the reader is
     scrolled away from the bottom, so following is CONDITIONAL. */
  #jump{position:absolute;right:22px;bottom:96px;display:none;background:var(--panel);color:var(--fg);border:1px solid var(--l);border-radius:999px;padding:8px 14px;font-size:13px;font-weight:600;cursor:pointer;box-shadow:0 4px 14px rgba(0,0,0,.45)}
  #jump:hover{border-color:var(--accent);color:var(--white)}
  #jump.show{display:block}
  /* B12: a message typed while a turn runs is visibly QUEUED (dashed) or
     STEERED (solid + badge) — never silently swallowed.
     THE BORDER IS NO LONGER THE READING (cli_web_queue.go): the .qstatus line
     below carries the WORDS ("排队中 · 当前回合结束后发送（第 N 位）" /
     "已插入当前回合"), because a dash pattern told a reader nothing they could
     act on — the pre-epic source called that difference "cosmetic only". */
  .msg.queued{align-self:flex-start;background:var(--q);border:1px dashed var(--ql);color:var(--qf);font-style:italic}
  .msg.queued.steering{border-style:solid;color:var(--sf)}
  /* THE QUEUE'S OWN STATUS LINE (one entry) and the badge's queue summary. */
  .msg .qstatus{display:block;margin-top:4px;font-size:11px;font-style:normal;color:var(--qs)}
  .msg .qstatus.steering{color:var(--sf)}
  .msg .qstatus.done{color:var(--qd);text-decoration:line-through}
  .msg .qcancel{margin-left:8px;font-size:11px;font-style:normal;padding:0 8px;border-radius:8px;border:1px solid var(--e);background:transparent;color:var(--qf);cursor:pointer}
  .msg .qcancel:hover{border-color:var(--qc);color:var(--qc2)}
  #status .qsum{color:var(--qm);margin-left:6px}
  .msg.notice{align-self:flex-start;background:var(--nt);border:1px dashed var(--nl);color:var(--nf);font-style:italic}
  /* A statement for the OPERATOR about the turn in flight (context pressure:
     the window is filling and NOTHING was removed). Solid brick border rather
     than the notice's dashed one, because it asks for a decision — finish here
     or start a new session — and a dashed frame reads as passing commentary. */
  .msg.warning{align-self:flex-start;background:var(--wb);border:1px solid var(--wl);color:var(--w);border-radius:10px;padding:8px 12px;max-width:85%;font-size:13px;line-height:1.45}
  .msg .placement{display:inline-block;margin-left:8px;font-size:10px;letter-spacing:.04em;text-transform:uppercase;color:var(--muted);border:1px solid var(--e);border-radius:999px;padding:0 6px}
  .imgthumb{position:relative;width:64px;height:64px;border-radius:8px;border:1px solid var(--l);overflow:hidden}
  .imgthumb img{width:100%;height:100%;object-fit:cover}
  .imgthumb .rm{position:absolute;top:0;right:0;background:rgba(0,0,0,.6);border:0;color:var(--white);border-radius:0 0 0 6px;cursor:pointer;font-size:11px;padding:1px 5px}
  .imgthumb .st{position:absolute;bottom:0;left:0;right:0;background:rgba(0,0,0,.55);color:var(--w);font-size:9px;text-align:center}
  #log.dragover{outline:2px dashed var(--accent);outline-offset:-10px}
  #clip{background:var(--panel);color:var(--muted);border:1px solid var(--l);border-radius:10px;padding:9px 12px;cursor:pointer;font-size:15px}
  #clip:hover{color:var(--fg);border-color:var(--accent)}
  /* THE MICROPHONE: one button, two states. The rec class is the ONLY thing that
     says a recording is running, and it is set from the SERVER's answer (the
     start endpoint's 200), never from the click — a device that refused to open
     must not leave a button claiming to record. */
  #mic{background:var(--panel);color:var(--muted);border:1px solid var(--l);border-radius:10px;padding:9px 12px;cursor:pointer;font-size:15px}
  /* THE LIVE RECORDING STATE (web_voice_live.go): elapsed time and the device,
     OUTSIDE the button — a glyph plus a title is not a clock, and "how long has
     this been recording" plus "which microphone" are the two questions the
     button's two characters cannot answer.
     IT MUST NOT BE A FLEX ITEM. #form is display:flex, so an in-flow child of
     it is charged its intrinsic width out of the composer's share: measured on
     the real page, a nowrap span reading "● recording <device> 0s" took 174.72
     px plus the 10 px gap — a delta of 184.72 px off #input at 900/1200/1600 px
     alike (27 % of the composer at 900). It is therefore taken OUT OF FLOW:
     absolutely positioned above the composer's right end, pointer-events:none so
     it never blocks a click, with its own solid background so it stays readable
     over the scrolled conversation behind it. Anchored RIGHT (the menu above the
     composer is left-anchored and full width) so the two only meet while a
     trigger menu is open AND a recording is running. */
  #voicelive{position:absolute;right:20px;bottom:100%;margin-bottom:6px;z-index:30;
    font-size:12px;color:var(--w);white-space:nowrap;padding:2px 9px;
    border:1px solid var(--wl);border-radius:999px;background:var(--wb);pointer-events:none}
  #voicelive[hidden]{display:none}
  #mic:hover{color:var(--fg);border-color:var(--accent)}
  #mic.rec{color:var(--white);background:var(--rb);border-color:var(--rl);animation:micpulse 1.2s ease-in-out infinite}
  @keyframes micpulse{50%{opacity:.55}}
  #mic[disabled]{opacity:.5;cursor:default}
  #form{padding:14px 20px;border-top:1px solid var(--lb);display:flex;gap:10px;align-items:center;position:relative}
  /* B9: the input-trigger menu — a candidate list anchored above the composer.
     Rendered with textContent only (a file path or a command word is content,
     never markup), keyboard-driven, and never a send path: Enter while the
     menu is open arbitrates to the highlighted candidate. */
  #menu{position:absolute;left:20px;right:20px;bottom:100%;margin-bottom:8px;background:var(--panel);border:1px solid var(--e);border-radius:10px;padding:6px;box-shadow:0 -6px 18px rgba(0,0,0,.45);max-height:220px;overflow-y:auto}
  #menu[hidden]{display:none}
  #menu .mi{padding:6px 10px;border-radius:6px;font:13px/1.45 ui-monospace,SFMono-Regular,monospace;color:var(--fg);cursor:pointer;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  #menu .mi.sel{background:var(--sl);color:var(--white)}
  #menu .mi .src{color:var(--muted);font-size:11px;margin-left:8px}
  #menu .mi.more{color:var(--muted);font-style:italic;cursor:default}
  /* B8: minimal markdown — fenced blocks and inline code. Every node is built
     from textContent; the styling here carries the "this is code" signal. */
  .mdpre{background:var(--cd);border:1px solid var(--l);border-radius:8px;padding:8px 10px;margin:6px 0;overflow-x:auto;white-space:pre;font-family:ui-monospace,SFMono-Regular,monospace;font-size:12.5px}
  .mdcode{background:var(--cd);border:1px solid var(--l);border-radius:4px;padding:0 4px;font-family:ui-monospace,SFMono-Regular,monospace;font-size:12.5px}
  .mdstrong{font-weight:650;color:var(--sg)}
  #input{flex:1;background:var(--panel);border:1px solid var(--l);color:var(--fg);border-radius:10px;padding:11px 14px;font:inherit;resize:none}
  #input:focus{outline:none;border-color:var(--accent)}
  button{background:var(--accent);color:var(--white);border:0;border-radius:10px;padding:11px 18px;font:inherit;font-weight:600;cursor:pointer}
  button:disabled{opacity:.45;cursor:not-allowed}
  /* B12: the composer and Send stay FUNCTIONAL while a turn runs (a message
     typed now is queued); the busy look is cosmetic only. */
  #send.busy{opacity:.6}
  #input.busy{border-color:var(--e)}
  .caret{display:inline-block;width:.55em}
  .caret::after{content:"▍";animation:blink 1s steps(1) infinite;color:var(--accent)}
  @keyframes blink{50%{opacity:0}}
  /* R6: the page-capture button. Neutral until armed, accent while the server
     is rendering (a Chromium start is seconds — the state must be visible), and
     it reports its own failure in the strip instead of silently doing nothing. */
  #shot,#theme{background:var(--panel);color:var(--muted);border:1px solid var(--l);padding:9px 10px}
  #shot.busy{opacity:.6;cursor:progress}
  /* R6: a capture failure is TEXT in the strip, next to where the picture would
     have appeared — never a silent no-op, never an alert() that blocks the page. */
  #imgstrip .shoterr{color:var(--et);font-size:12.5px;padding:6px 2px}
  /* LIGHT + RESPONSIVE: light redefines every --* dark has; breakpoints fix
     #form (at 380px buttons leave #input unusable), bubble cap, card body. */
  [data-theme="light"]{--bg:#f4f6fa;--panel:#fff;--bubble:#e9eef6;--fg:#12161d;--muted:#5b6474;--accent:#1d5fd0;--red:#b3261e;--lb:#d8dee8;--l:#ccd3df;--e:#c2cad8;--hr:#dde3ec;--cd:#eef1f6;--bd:#e6ebf3;--bf:#5b6474;--tc:#fff;--tl:#ccd3df;--tf:#1d5fd0;--cm:#eaf4ee;--cl:#b6d8c2;--cb:#fbebec;--ck:#e6b4b8;--co:#1c5f37;--cr:#a83232;--eb:#e6b4b8;--eg:#fbebec;--q:#eef2f9;--ql:#9aa9c2;--qf:#2a3550;--sf:#12233f;--qs:#4a5a78;--qd:#6b7484;--qc:#c9a2a2;--qc2:#a83232;--qm:#8a6a1f;--nt:#f3f5f9;--nl:#9aa9c2;--nf:#2a3550;--sl:#dbe4f4;--w:#8a5a00;--wl:#e3cf9e;--wb:#fdf5e2;--in:#1d5fd0;--il:#a9c2ea;--ok:#1c7a3d;--tw:#8a5a00;--wr:#8a5a00;--wr2:#e3cf9e;--gl:#a9c2ea;--rl:#d08a92;--rb:#b3261e;--et:#a83232;--sg:#12161d;--mine:#2b5cff;--white:#fff;}
  @media(max-width:900px){.msg,.toolcard{max-width:92%}.toolcard .tbody{overflow-x:auto}}
  @media(max-width:600px){header{flex-wrap:wrap;padding:8px 12px;gap:6px 10px}#log{padding:12px}.msg,.toolcard{max-width:100%}#form{flex-wrap:wrap;padding:10px 12px;gap:8px}#input{flex:1 1 100%;order:-1}button{padding:9px 12px}#imgstrip{padding:0 12px}#menu{left:12px;right:12px}#voicelive{right:12px}}
</style>
</head>
<body>
<header><h1>gopi web</h1><span class="sub">pure-Go browser chat · SSE streaming</span><button id="theme" type="button" title="theme: following the system — click to pin light, then dark, then follow again">auto</button><span id="status" data-state="idle" data-tool="" data-interruptible="false" title="server-side assistant status (GET /api/state)">● idle</span></header>
<div id="log"></div>
<button id="jump" type="button" title="jump to the latest message">↓ jump to latest</button>
<div id="imgstrip"></div>
<form id="form"><div id="menu" class="menu" hidden></div><span id="voicelive" hidden></span><button id="mic" type="button" title="record a voice message — transcribes on the machine running gopi web and drops the text into the composer">🎤</button><button id="clip" type="button" title="attach images (or paste / drag-drop)">📎</button><button id="shot" type="button" title="截当前页 — capture THIS page's rendering and attach it (画面问题用它代替文字描述)">📷</button><textarea id="input" rows="1" placeholder="Ask gopi anything… (/ for commands, Enter to send, Shift+Enter for newline)"></textarea>
<button id="send" type="submit">Send</button></form>
<input id="file" type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple hidden/>
<script>
// THE THEME, decided before anything is drawn: a stored preference wins, and
// with none the page FOLLOWS THE SYSTEM (window.matchMedia, so a machine that
// switches to a light palette at sunset is followed without a reload — the
// listener below re-applies while no preference is stored). Three stored values
// are legal: "light", "dark", and the ABSENCE of a key, which means "follow the
// system". The header button cycles all three, so the follow state is always
// reachable by clicking — never a hidden long-press.
const THEME_KEY='gopi.theme';
const THEME_LIGHT='light',THEME_DARK='dark';
const THEME_MQ='(prefers-color-scheme: light)';
// themeStored reads the preference; storage can be denied (a private window),
// and a denied read is the FOLLOW-SYSTEM state rather than an error.
function themeStored(){try{return window.localStorage.getItem(THEME_KEY)||''}catch(e){return ''}}
function themePrefersLight(){try{return !!(window.matchMedia&&window.matchMedia(THEME_MQ).matches)}catch(e){return false}}
function themeApply(){
  const pref=themeStored();
  const light=pref?pref===THEME_LIGHT:themePrefersLight();
  const rootEl=document.documentElement;
  if(rootEl&&rootEl.setAttribute){rootEl.setAttribute('data-theme',light?THEME_LIGHT:THEME_DARK)}
  const btn=document.getElementById('theme');
  if(!btn){return}
  // THE LABEL IS A WORD, not a glyph: "auto" is the follow-system state said
  // in the one place the reader looks, and a three-letter word survives every
  // font a browser might substitute for ☀/☾ (a missing glyph renders as an
  // empty box — measured in this run's own headless capture, where the toggle
  // photographed as a blank square).
  btn.textContent=pref||'auto';
  btn.title=pref?('theme: '+pref+' — click to cycle light → dark → auto (follow the system)'):
    'theme: following the system ('+(light?'light':'dark')+' here) — click to pin light, then dark, then follow again';
}
// themeFollowSystem CLEARS the stored preference, which IS the follow-system
// state — the restore path the cycle reaches after two more clicks.
function themeFollowSystem(){try{window.localStorage.removeItem(THEME_KEY)}catch(e){}themeApply()}
themeApply();
if(window.matchMedia){
  try{window.matchMedia(THEME_MQ).addEventListener('change',function(){if(!themeStored()){themeApply()}})}catch(e){}
}
document.addEventListener('DOMContentLoaded',function(){
  themeApply();
  const btn=document.getElementById('theme');
  if(!btn){return}
  btn.addEventListener('click',function(){
    const order=['',THEME_LIGHT,THEME_DARK];
    const next=order[(order.indexOf(themeStored())+1)%order.length];
    try{ if(next){window.localStorage.setItem(THEME_KEY,next)}else{window.localStorage.removeItem(THEME_KEY)} }catch(e){}
    themeApply();
  });
});
const logEl=document.getElementById('log');
// THE SERVER-SIDE STATUS ELEMENT (see the CSS block above): one resident node
// whose data-state / data-tool / data-interruptible attributes ARE the status.
const statusEl=document.getElementById('status');
const form=document.getElementById('form');
const input=document.getElementById('input');
const sendBtn=document.getElementById('send');
const fileEl=document.getElementById('file');
const clipBtn=document.getElementById('clip');
// THE MICROPHONE (web_voice.go). It is ONE button with two states: idle, and
// mic recording state while a recording is open on the SERVER. The two routes are the page's
// copy of the server's webVoiceStartPath / webVoiceStopPath — a page constant is
// not importable, so TestWebVoicePageCarriesTheRoutes pins the pair Go-side.
const micBtn=document.getElementById('mic');
// THE LIVE MICROPHONE (web_voice_live.go): the streaming route and its stop
// route, plus the in-form state span that shows elapsed time and the device
// OUTSIDE the button. A page constant is not importable, so the Go test pins
// every one of these strings against its own constant.
const API_VOICE_LIVE='/api/voice/live';
const API_VOICE_LIVE_STOP='/api/voice/live/stop';
// THE ESCAPE ROUTE: end the live utterance and DISCARD it. Distinct from the
// stop route on purpose — ⏹ settles the sentence into the composer, Escape
// throws it away, and the two must not share a verb or the discard would have to
// be a page-side decision the server never hears about.
const API_VOICE_LIVE_CANCEL='/api/voice/live/cancel';
const voiceLiveEl=document.getElementById('voicelive');
// THE SSE EVENT NAMES the live route publishes (webVoiceLiveEvent* Go-side).
const EV_LIVE_READY='ready';
const EV_LIVE_DRAFT='draft';
const EV_LIVE_FINAL='final';
// THE FALLBACK TRIGGER: the server answers 501 for an engine with no live arm,
// which is the ONE status that means "use the start/stop path instead".
const HTTP_NOT_IMPLEMENTED=501;
// A CONFLICT on a cancel means the recording had already ended — the state the
// caller asked for, not a failure (see micCancel).
const HTTP_CONFLICT=409;
const API_VOICE_START='/api/voice/start';
const API_VOICE_STOP='/api/voice/stop';
// THE WINDOW PATH'S DISCARD ROUTE (Escape on the one-shot path): end the
// recording and throw the audio away instead of transcribing it.
const API_VOICE_CANCEL='/api/voice/cancel';
const strip=document.getElementById('imgstrip');
const shotBtn=document.getElementById('shot');
const jumpBtn=document.getElementById('jump');
const menuEl=document.getElementById('menu');
// B9: the candidate endpoint and its two parameter names — the page's copy of
// the server's webMenuPath / webMenuTriggerArg / webMenuQueryArg (Rule 1b: the
// URL is built from constants, and the regression test pins the pair).
const API_MENU='/api/menu';
// The engine IDENTITY route (web_identity.go): the browser asks the server
// WHICH session/model it is actually driving. The URL bar alone cannot answer
// it — the access token is re-minted per run and the page survives a restart of
// the engine — and for a REMOTE browser that is exactly the question that
// matters (a reloaded page on the wrong model/session looks identical).
const API_IDENTITY='/api/identity';
const MENU_TRIGGER_ARG='trigger', MENU_QUERY_ARG='q';
// B9 keyboard arbitration: the key names the menu reads and the two walk
// directions. They are NAMED (never typed inline) because the branch that acts
// on a key and the listener that must IGNORE that key's keyup are two carriers
// of the same value — a rename that reached only one of them would be silent.
const KEY_ARROW_DOWN='ArrowDown';
const KEY_ARROW_UP='ArrowUp';
const KEY_ENTER='Enter';
const KEY_TAB='Tab';
const KEY_ESCAPE='Escape';
// KEY_CLEAR_INPUT is the Ctrl+U chord's letter: the WHOLE-COMPOSER CLEAR. It is
// the familiar terminal/readline erase-to-start key, and it is DELIBERATELY
// UNANNOUNCED (no placeholder text, no button, no notice) — the operator's own
// design point: a novice who does not know the chord cannot trip it, and
// "I know it exists but not how to undo it" is the harm the silence prevents.
// Both spellings are NAMED because the branch compares the key it was handed.
const KEY_CLEAR_INPUT='u';
const KEY_CLEAR_INPUT_UPPER='U';
const MENU_MOVE_NEXT=1, MENU_MOVE_PREV=-1;
// MENU_NAV_KEYS are the PURE-NAVIGATION keys: their keydown only moves the
// highlight or closes the menu, so the keyup that follows must NOT re-run the
// detect core — a full menuRefresh there rebuilds the list and would undo the
// move (and spend a /api/menu round-trip) milliseconds after it happened.
const MENU_NAV_KEYS=new Set([KEY_ARROW_DOWN,KEY_ARROW_UP,KEY_TAB,KEY_ESCAPE,KEY_ENTER]);
// B12: the steering sentinel the page prefixes an Alt+Enter message with. The
// server recognizes and STRIPS it (webSteerPrefix), so the typed placement
// never reaches the model as content.
const STEER_PREFIX='!steer ';
// The per-run ACCESS TOKEN the gate demands (Rule 1b: the page carries its own
// copy of the server's webTokenQueryKey / webTokenQuerySep and of the route
// paths — a page inside a Go raw string cannot import Go constants, and this
// file's raw string is NOT interpolated). The constant-agreement regression
// test pins every one of these pairs, so a rename on either side fails the test
// rather than silently desynchronizing the two carriers (Rule 13).
const TOKEN_ARG='t';
const QUERY_SEP='&';
const API_CHAT='/api/chat';
const API_TRANSCRIPT='/api/transcript';
// THE STANDING EVENT STREAM (cli_web_notice.go): the page holds it open so a
// turn the SERVER started by itself — a gopi notify delivery — has somewhere to
// arrive. The names are the server's own (webEventsPath / webEventNotice); the
// regression test pins the pair.
const API_EVENTS='/api/events';
const EV_NOTICE='notice';
// EV_WARNING is a statement addressed to the OPERATOR about the turn in flight
// (context pressure — internal/agent/context_pressure.go). A separate name from
// EV_NOTICE because the two mean different things to this page: a notice
// ANNOUNCES a server-started turn and the bubble below it carries that turn's
// answer, while a warning opens no bubble and continues the turn already
// running. Pinned by the same regression test as the pair above.
const EV_WARNING='warning';
// THE TURN BANNER (cli_web_turn.go). EV_TURN is the frame the server sends when
// a turn STARTS, carrying the session's own ordinal for it; SESSION_TURN_FIELD
// is the same number riding the terminal done frame, for a client whose turn
// frame was dropped by a reconnect. A page inside a Go raw string cannot import
// the server's constants, so the pair is pinned by the regression test exactly
// like EV_NOTICE / API_EVENTS above.
const EV_TURN='turn';
const SESSION_TURN_FIELD='session_turn';
const TURN_BAND_PREFIX='turn ';
// THE BANNER'S MOMENT (sessionevent.FormatTurnStamp's spelling, rendered on the
// server so the page never formats a time of its own). It rides the same two
// frames the ordinal does — the turn frame and the terminal done frame — and
// every transcript row that carries a session_turn carries this beside it.
const SESSION_TURN_OPENED_FIELD='session_turn_opened_at';
const TURN_BAND_MOMENT_SEP=' \u00b7 ';
// THE SERVER-SIDE STATUS (cli_web_state.go). The snapshot endpoint and the four
// state names are the server's own spellings; the regression test pins every
// pair, because a page inside a Go raw string cannot import them.
const API_STATE='/api/state';
// THE WAITING-MESSAGE ROUTES (cli_web_queue.go). The registration route gives a
// mid-turn message an id and a place in the queue ON THE SERVER, and the cancel
// route withdraws it before its turn runs — without the first, the server hears
// about a waiting message only when it finally POSTs, one turn too late to
// name, number or withdraw it.
const API_QUEUE='/api/chat/queue';
const API_QUEUE_CANCEL='/api/chat/queue/cancel';
const EV_CANCELLED='cancelled';
const EV_STATE='state';
const ST_IDLE='idle';
const ST_RESPONDING='responding';
const ST_TOOL_RUNNING='tool_running';
// The words the badge shows for them. They are TEXT, not a border style: the
// pre-epic page distinguished the two by CSS alone and called it "cosmetic
// only", which told a reader nothing they could act on.
const QUEUED_LABEL='排队中 · 当前回合结束后发送';
const STEERING_LABEL='已插入当前回合';
const QUEUED_POSITION_LABEL='第 ';
const QUEUED_POSITION_SUFFIX=' 位';
// The withdraw button's word, and the notice that replaces a withdrawn entry's
// body — a message taken back must SAY so, not merely disappear.
const QUEUE_CANCEL_LABEL='撤销';
const QUEUE_CANCELLED_LABEL='已撤销 · 未发送';
const QUEUE_CANCEL_FAILED='撤销失败：条目可能已经开始执行';
// R6: the page-capture route. The endpoint takes NO parameters — the server
// captures ITS OWN page — so the page sends an empty body and never a URL.
const API_SHOT='/api/shot';
// The command frame's optional image member — the SAME name the server writes
// (cli_web.go webCommandImagesField). A slash command that produced pictures
// (/text2img) sends them here; the page stages them into images[] with no
// second upload mechanism.
const CMD_IMAGES_FIELD='images';
// PNG_MEDIA is the one media type the page ever fabricates a default for: every
// product it stages itself (a command's rendered page, a capture) is a PNG, and
// a frame that omits media_type still has to name one. The server always sends a
// validated value, so this is the fallback spelling, not a second source of
// truth (webShotMediaType owns the real answer).
const PNG_MEDIA='image/png';
// locationToken reads the token out of the page URL the banner
// printed (…?t=<token>), re-reading it on EVERY call: a fresh URL
// pasted into the same tab self-heals without a reload.
function locationToken(){
  try{return new URLSearchParams((window.location&&window.location.search)||'').get(TOKEN_ARG)||'';}
  catch(err){return '';}
}
// withToken appends the per-run access token to a same-origin API URL.
// Without it a request authenticates through the gate cookie ALONE, and a
// cookie minted by a PREVIOUS run is stale (the token is re-minted per run)
// → every fetch 401s while the page load that carried the ?t= URL succeeded.
function withToken(url){
  const t=locationToken();
  if(!t)return url;
  return url+((url.indexOf('?')<0)?'?':QUERY_SEP)+TOKEN_ARG+'='+encodeURIComponent(t);
}
let busy=false;          // a stream is in flight
let pendingCards=[];     // tool cards awaiting their result event (FIFO)
let images=[];           // staged {media_type,data_b64,name} uploads (R5)
// B12: the messages the user submitted while the active turn was running —
// each is on screen as a queued entry and POSTed as a turn of its own when the
// active one finishes (never dropped). Each entry is {text,node,images,placement}.
let pendingMsgs=[];
// B12: the running request's AbortController. Replaced by the NEXT queued turn,
// so "stop" aborts whatever turn is running right now — the earlier turns are
// already done. Declared here (never inside a turn closure) because the queue's
// future turns are handed a NEWER controller.
let activeCtl=null;
const MAX_IMAGES=8;
// Budget alignment carried with the page: the upload cap is expressed in
// DECODED bytes — the same 32 MiB ceiling the server enforces
// (webMaxChatImageBytes = tools.LoadImage's limit) — and the base64 length it
// compares against is DERIVED from it by the 4/3 expansion, so "32 MiB" is one
// budget on both sides instead of two disagreeing ones.
const MAX_IMAGE_BYTES=32*1024*1024, BASE64_RATIO_NUM=4, BASE64_RATIO_DEN=3;
const MAX_B64=Math.ceil(MAX_IMAGE_BYTES*BASE64_RATIO_NUM/BASE64_RATIO_DEN)+4;
const MAX_IMAGE_LABEL='32 MiB decoded';
// B9/B12 composer guards (the input path both build on): the textarea grows
// with its content up to INPUT_MAX_ROWS rows, then scrolls (hermes' TextArea
// min=1 max=8); one line height is NAMED because the growth maths and its cap
// both use it. IME_PROCESSING_KEYCODE is the legacy "the IME owns this
// keystroke" signal for engines without KeyboardEvent.isComposing.
const INPUT_MAX_ROWS=8, INPUT_LINE_HEIGHT=22;
const IME_PROCESSING_KEYCODE=229;
// B3: follow-bottom is CONDITIONAL — the stream only auto-scrolls while the
// reader is already at the bottom (within FOLLOW_THRESHOLD px), so scrolling
// back to read is never yanked by an incoming delta. Ported from
// deepseek-harness ChatView.tsx (FOLLOW_THRESHOLD = 24) — same name, same
// value, and the server keeps the same number as webFollowThreshold so the
// two carriers cannot drift (Rule 1: named, not inline).
const FOLLOW_THRESHOLD=24;
// B1: the server's reset acknowledgement (webResetCleared in cli_web.go). When
// /new or /clear resolves to exactly this text the browser wipes #log — the
// reset is the SERVER's action, the page only reflects it.
const RESET_NOTICE='conversation reset — the transcript and per-turn state were cleared';
let lastQueued=null;     // B2: the "waiting for the in-flight turn" bubble
// B12 (steering availability): the SHARED command table's reset words, ported
// to the page. A queued turn whose FIRST word is one of these is a stop/reset,
// not a message — the server would answer it as a local command frame. Same two
// spellings the server accepts (Rule 1b).
const RESET_WORDS=['new','clear'];
// B12: the placement badge's two values — dsh's QueuedInboxItem.placement idea,
// typed in the page. A normal mid-turn message is 'queued' (and becomes a turn
// of its own); an Alt-gesture message is 'steering'.
const PLACEMENT_QUEUED='queued', PLACEMENT_STEERING='steering';
// THE FIFTH AND SIXTH STATES (cli_web_queue.go's webStateQueued/webStateSteering):
// a message waiting for the current turn, and one that addresses it. They are
// ALIASES of the placement pair above — not a second vocabulary (Rule 14: one
// idea, one spelling), so the wire's state and placement fields cannot drift
// apart — and they are declared AFTER it, because a top-level const referring
// to a later const is a temporal-dead-zone ReferenceError at load, not a
// forward declaration.
const ST_QUEUED=PLACEMENT_QUEUED;
const ST_STEERING=PLACEMENT_STEERING;
// B9: the two input triggers, the menu budget and the per-source labels. The
// DETECT core below decides from the composer's own text; the endpoint resolves
// candidates (the shared command table for '/', the file tree for '@') so the
// page needs no second copy of either.
const TRIGGER_SLASH='/', TRIGGER_AT='@';
const MENU_MAX_ITEMS=8;
const MENU_MORE_FLAG='+more';
const MENU_SOURCE_LABEL={slash:'command',file:'file'};
// B8: the markdown fence and inline-code delimiters, and the fence token cap.
// The delimiter is a BACKTICK — the one character the page cannot spell
// literally, because the page itself lives inside a Go raw string (delimited by
// backticks). It is therefore built from its code point on the page
// (MD_TICK_CHARCODE) while the server names the same character as
// webMarkdownFence; the regression test asserts the two agree, so the
// construction is pinned to the value rather than trusted. A fence whose body
// exceeds MD_MAX_FENCE_LINES is NOT formatted — the text is simply shown, so a
// pathological input stays inert text instead of becoming an unbounded node.
const MD_TICK_CHARCODE=96, MD_FENCE_LEN=3, MD_MAX_FENCE_LINES=200;
const MD_TICK=String.fromCharCode(MD_TICK_CHARCODE);
const MD_FENCE=MD_TICK+MD_TICK+MD_TICK;
// The intra-line emphasis delimiter (webMarkdownBold on the server, which is
// lineedit.BoldMarker — the TUI's own constant). It is spelled from its code
// point for the same reason the fence is, and MD_BOLD_LEN is the server's
// len(webMarkdownBold): one value, pinned by the regression test.
const MD_ASTERISK_CHARCODE=42, MD_BOLD_LEN=2;
const MD_ASTERISK=String.fromCharCode(MD_ASTERISK_CHARCODE);
const MD_BOLD=MD_ASTERISK+MD_ASTERISK;

// B3: follow-bottom gate + the "jump to latest" affordance.
function atBottom(){return logEl.scrollHeight-logEl.scrollTop-logEl.clientHeight<=FOLLOW_THRESHOLD;}
function scrollToBottom(){logEl.scrollTop=logEl.scrollHeight;}
function follow(force){if(force||atBottom()){scrollToBottom();jumpBtn.classList.remove('show');}else{jumpBtn.classList.add('show');}}
logEl.addEventListener('scroll',()=>{if(atBottom())jumpBtn.classList.remove('show');});
jumpBtn.addEventListener('click',()=>{scrollToBottom();jumpBtn.classList.remove('show');});

// addTurnBand mounts one turn separator. One node per turn, at the turn's
// beginning, labelled with the SESSION's own ordinal — never a page counter: the
// number arrives on the wire (the turn frame, or a replayed row's
// session_turn), so a refreshed or resumed page shows the numbers the record
// gives those turns.
//   n     the session turn ordinal (1-based)
//   quiet B3 gate pass-through, same as addMsg (replay mounts without a jump)
function addTurnBand(n,quiet,at){
  const band=document.createElement('div');
  band.className='turnband';
  band.dataset.turn=String(n);
  // THE MOMENT THE TURN OPENED, appended only when the server sent one. The
  // STRING is the server's (sessionevent.FormatTurnStamp) — the page does no
  // date arithmetic and owns no timezone, so the terminal band and this banner
  // cannot spell the same instant two ways.
  band.textContent=TURN_BAND_PREFIX+n+(at?TURN_BAND_MOMENT_SEP+at:'');
  logEl.appendChild(band);
  if(quiet){follow(false);}else{follow(true);}
  return band;
}

// addMsg appends one conversation node and follows it. quiet is the ONE
// exception to the forced scroll, and it is for a node appended MID-STREAM: an
// assistant bubble mounted by its first frame must obey the B3 gate like every
// other stream-time insertion (a forced scroll there would yank a reader who
// had scrolled up to read an earlier answer).
function addMsg(role,text,quiet){
  const div=document.createElement('div');
  div.className='msg '+role;
  div.textContent=text||'…';
  logEl.appendChild(div);
  if(quiet){follow(false);}else{follow(true);}
  return div;
}

// identifyEngine renders the engine's identity as the FIRST line of the log:
// "session=… model=… tools=… addr=…" plus the counters a remote operator needs
// to tell an EMPTY session from a live conversation they did not start. It is
// a plain msg node (textContent only) and it degrades silently: a bind that
// refuses the route (an older server, or a 401) leaves the page exactly as it
// was rather than printing an identity nobody verified.
async function identifyEngine(){
  try{
    const res=await fetch(withToken(API_IDENTITY));
    if(!res.ok)return;
    const id=await res.json();
    addMsg('engine','engine session='+(id.session_id||'unknown')+' model='+(id.model||'unknown')
      +' tools='+(id.tools?'on':'off')+' addr='+(id.addr||'unknown')
      +' · messages='+(id.messages||0)+' turns='+(id.turns||0)+' tokens='+(id.tokens||0)
      +(id.started?' · started='+id.started:''));
  }catch(err){/* no identity banner: the page still works, and it never claims an identity it did not read */}
}

// addCommand renders one locally-executed slash command result (R4): the
// command line plus its output as a conversation node — the model never
// saw this exchange.
function addCommand(cmdline,output,err){
  const div=document.createElement('div');
  div.className='msg command'+(err?' err':'');
  const head=document.createElement('div');
  head.className='cmdline';head.textContent=cmdline;
  div.appendChild(head);
  const pre=document.createElement('pre');
  pre.textContent=output||'';
  div.appendChild(pre);
  logEl.appendChild(div);
  follow(true);
}

// --- B8: minimal markdown ------------------------------------------------
//
// markdownNodes is the PURE tokenizer: it turns a message's text into a flat
// list of {kind,text} nodes — 'text' and 'code' — with NO DOM knowledge, so the
// decision is testable on its own and the renderer below only has to build text
// nodes.
//
// Scope is deliberately minimal: fenced code blocks and inline code spans (the
// delimiter is MD_TICK). No bold/italic/links/lists/HTML — every node the
// renderer creates gets its text through textContent, so a script tag (or any
// other markup) inside a delta is INERT TEXT, never markup. A fence with no
// closing fence ends at the end of the text, and a fence body longer than
// MD_MAX_FENCE_LINES is not tokenized as code at all (the text stays text).
function markdownNodes(text){
  const out=[];
  if(!text){return out;}
  const lines=text.split('\n');
  let buf=[],inFence=false,fence=[];
  function flushText(){
    if(buf.length){out.push({kind:'text',text:buf.join('\n')});buf=[];}
  }
  for(const line of lines){
    const isFence=line.trimStart().startsWith(MD_FENCE);
    if(isFence){
      if(inFence){
        if(fence.length<=MD_MAX_FENCE_LINES){out.push({kind:'code',text:fence.join('\n')});}
        else{out.push({kind:'text',text:MD_FENCE+'\n'+fence.join('\n')+'\n'+MD_FENCE});}
        fence=[];inFence=false;
      }else{
        flushText();inFence=true;
      }
      continue;
    }
    if(inFence){fence.push(line);}else{buf.push(line);}
  }
  if(inFence){buf.push(MD_FENCE);buf=buf.concat(fence);}   // unterminated fence: plain text
  flushText();
  return out;
}

// codeSpanSplit splits ONE text node into alternating plain/code pieces around
// single-backtick spans. Content between the ticks is preserved verbatim (no
// escaping needed — the renderer builds a text node), and an unmatched tick is
// literal text.
function codeSpanSplit(text){
  const parts=[];
  let i=0;
  while(i<text.length){
    const start=text.indexOf(MD_TICK,i);
    if(start<0){parts.push({kind:'text',text:text.slice(i)});break;}
    if(start>i){parts.push({kind:'text',text:text.slice(i,start)});}
    const end=text.indexOf(MD_TICK,start+1);
    if(end<0){parts.push({kind:'text',text:text.slice(start)});break;}
    parts.push({kind:'code',text:text.slice(start+1,end),inline:true});
    i=end+1;
  }
  return parts;
}

// boldSpanSplit splits ONE text piece into alternating plain/bold pieces around
// MD_BOLD. The RULES are the terminal emphasis layer's own (internal/lineedit's
// tokenizer is the single definition of what a marker means): the markers that
// AROUND an emphasised run are NOT part of any piece, an unclosed marker is literal text,
// and two adjacent markers are literal — so the same answer reads the same way on
// both carriers.
function boldSpanSplit(text){
  const parts=[];
  let i=0;
  while(i<text.length){
    const start=text.indexOf(MD_BOLD,i);
    if(start<0){parts.push({kind:'text',text:text.slice(i)});break;}
    const end=text.indexOf(MD_BOLD,start+MD_BOLD_LEN);
    if(end<0){parts.push({kind:'text',text:text.slice(i)});break;}   // unclosed: literal
    if(end===start+MD_BOLD_LEN){                                     // '****': empty span
      parts.push({kind:'text',text:text.slice(i,start+MD_BOLD_LEN)});
      i=start+MD_BOLD_LEN;
      continue;
    }
    if(start>i){parts.push({kind:'text',text:text.slice(i,start)});}
    parts.push({kind:'bold',text:text.slice(start+MD_BOLD_LEN,end)});
    i=end+MD_BOLD_LEN;
  }
  return parts;
}

// renderMarkdown renders a message's text into the parent element — a fenced
// block becomes a <pre>, an inline span a <code>, everything else a text node.
// The ONLY DOM writes are createElement + textContent + appendChild: no
// innerHTML anywhere on this path (B8's XSS contract, asserted by the
// regression test).
function renderMarkdown(parent,text){
  for(const node of markdownNodes(text)){
    if(node.kind==='code'){
      const pre=document.createElement('pre');
      pre.className='mdpre';pre.textContent=node.text;
      parent.appendChild(pre);
      continue;
    }
    for(const part of codeSpanSplit(node.text)){
      if(part.kind==='code'){
        const code=document.createElement('code');
        code.className='mdcode';code.textContent=part.text;
        parent.appendChild(code);
        continue;
      }
      // Plain text: the intra-line emphasis layer decides the runs. A code span
      // wins over emphasis (codeSpanSplit already took it), exactly as a fence
      // wins over the terminal's emphasis layer.
      for(const b of boldSpanSplit(part.text)){
        if(!b.text){continue;}
        if(b.kind==='bold'){
          const strong=document.createElement('strong');
          strong.className='mdstrong';strong.textContent=b.text;
          parent.appendChild(strong);
          continue;
        }
        parent.appendChild(document.createTextNode(b.text));
      }
    }
  }
}

// clearNode empties an element without innerHTML: the B8/B12 render paths must
// never interpolate content into markup, so clearing goes through the same
// append-only discipline the rendering does.
function clearNode(el){while(el.firstChild){el.removeChild(el.firstChild);}}

// renderAssistantBody re-renders a streaming assistant bubble from its RAW text
// (B8): markdown is applied as the deltas arrive, so a fence opens when its
// opening fence arrives. Every node is built with textContent.
function renderAssistantBody(el,text){clearNode(el);renderMarkdown(el,text);}

// --- B9: input-trigger menu ---------------------------------------------
//
// The DETECT core is pure: the composer's text and caret decide whether a menu
// is open, which trigger opened it, and what the query is. The VIEW below only
// renders what the core decided and routes keys into it; candidates come from
// /api/menu (the shared command table / the file tree), never from a table
// copied into the page.
//
//   /command   → open on '/' typed at the START of the composer, and only
//                while the caret is still inside the first word.
//   @fragment  → open on '@' introduced by start-of-text or whitespace, with
//                the fragment taken up to the next whitespace.
//
// A menu is NEVER open inside a code fence; the composer's fence parity gates
// the whole thing (fenceParity), so typing a path inside a pasted code block
// does not pop a menu.
function fenceParity(text){
  let open=false;
  for(const line of text.split('\n')){
    if(line.trimStart().startsWith(MD_FENCE)){open=!open;}
  }
  return open;
}

// detectMenu is the pure detect core. It returns {trigger,query} — trigger ''
// means no menu — and never touches the DOM or the network.
function detectMenu(text,caret){
  if(typeof text!=='string'||caret==null||caret>text.length){return {trigger:'',query:''};}
  if(fenceParity(text)){return {trigger:'',query:''};}
  if(text.charAt(0)===TRIGGER_SLASH){
    const word=text.slice(1);
    // The caret must still be inside the first word (a '/task list' argument
    // is not a command filter any more).
    const stop=word.search(/[\s]/);
    if(caret-1>(stop<0?word.length:stop)){return {trigger:'',query:''};}
    const query=word.slice(0,caret-1);
    if(query.indexOf('/')>=0){return {trigger:'',query:''};}
    return {trigger:TRIGGER_SLASH,query};
  }
  const upto=text.slice(0,caret);
  const at=upto.lastIndexOf(TRIGGER_AT);
  if(at<0){return {trigger:'',query:''};}
  // '@' must start the text or follow whitespace — never split an email or a
  // mid-word token.
  const before=at===0?'':upto.charAt(at-1);
  if(before!==''&&!/\s/.test(before)){return {trigger:'',query:''};}
  const query=upto.slice(at+1);
  if(/\s/.test(query)){return {trigger:'',query:''};}
  return {trigger:TRIGGER_AT,query};
}

let menu={open:false,trigger:'',query:'',items:[],sel:0,candidates:[],total:0};

// menuCandidatesFor asks the server for the candidates of one query. The
// response is the same shape the detect core's callers use, and it is the ONLY
// place candidates come from (the page holds no command list and no file list).
async function menuCandidatesFor(trigger,query){
  try{
    const res=await fetch(withToken(API_MENU+'?'+MENU_TRIGGER_ARG+'='+encodeURIComponent(trigger)+'&'+MENU_QUERY_ARG+'='+encodeURIComponent(query)));
    if(!res.ok)return {candidates:[],total:0};
    const data=await res.json();
    return {candidates:data.candidates||[],total:(typeof data.total==='number'?data.total:(data.candidates||[]).length)};
  }catch(err){return {candidates:[],total:0};}
}

// menuRefresh re-runs the detect core on the composer and re-renders the menu.
// It is called from the input handler and from the caret moves that can change
// the decision; the async candidate fetch is guarded against a stale response
// (a fast typist must never see the answer to an older query).
//
// RESET_SEL is the caller's decision, not this function's: a NEW query (a
// keystroke, a caret move to a different query) starts its list at the top,
// while a refresh of the SAME trigger+query (the click/keyup pass) keeps the
// highlight the user already walked to. Rebuilding with the highlight at 0
// unconditionally is what made the arrow keys look dead: the keydown moved the
// selection and the keyup that followed put it straight back.
async function menuRefresh(resetSel){
  const d=detectMenu(input.value,input.selectionStart==null?input.value.length:input.selectionStart);
  if(!d.trigger){menuClose();return;}
  const token=d.trigger+'\u0000'+d.query;
  // The identity of the query this menu belongs to — the SAME test the stale-
  // response guard makes, read against the menu that is open NOW (token is set
  // below, but its old value is the open menu's).
  const sameQuery=menu.open&&menu.trigger===d.trigger&&menu.query===d.query;
  menu.token=token;
  const got=await menuCandidatesFor(d.trigger,d.query);
  if(menu.token!==token)return;   // a newer query won
  if(got.candidates.length===0){menuClose();return;}
  // The highlight survives a refresh of the same query, clamped: a shorter list
  // (the query grew while the fetch was in flight) must never leave the index
  // past the end, where nothing is highlighted and Enter has nothing to accept.
  const sel=(sameQuery&&!resetSel)?Math.min(menu.sel,got.candidates.length-1):0;
  menu={open:true,trigger:d.trigger,query:d.query,items:got.candidates,sel,candidates:got.candidates,total:got.total,token};
  renderMenu();
}

// renderMenu draws the candidate list. Every row is built from textContent —
// a file path or a command word is content, never markup — and the "more" row
// reports the count the endpoint withheld.
function renderMenu(){
  clearNode(menuEl);
  if(!menu.open){menuEl.hidden=true;return;}
  menu.items.forEach((label,i)=>{
    const row=document.createElement('div');
    row.className='mi'+(i===menu.sel?' sel':'');
    row.textContent=label;
    const src=document.createElement('span');
    src.className='src';src.textContent=MENU_SOURCE_LABEL[menu.trigger]||'';
    row.appendChild(src);
    // A pointer click completes the candidate; the row never becomes a send
    // path by itself (the submit is still the composer's own gesture).
    row.addEventListener('mousedown',e=>{e.preventDefault();menuComplete(label);});
    menuEl.appendChild(row);
  });
  if(menu.total>menu.items.length){
    const more=document.createElement('div');
    more.className='mi more';
    more.textContent=menu.total+MENU_MORE_FLAG;
    menuEl.appendChild(more);
  }
  menuEl.hidden=false;
}

function menuClose(){
  menu.open=false;menu.items=[];menu.candidates=[];menu.sel=0;menu.total=0;menu.token=null;
  // The trigger/query describe the CLOSED menu too: with them left behind, a
  // refresh of the same query would read as "the same menu" and preserve a
  // highlight for a menu that is not on screen.
  menu.trigger='';menu.query='';
  menuEl.hidden=true;
}

// menuMove walks the highlight, wrapping at both ends (a keyboard menu never
// dead-ends).
function menuMove(delta){
  if(!menu.open||menu.items.length===0)return;
  const n=menu.items.length;
  menu.sel=((menu.sel+delta)%n+n)%n;
  renderMenu();
}

// applyCompletion replaces the COMPOSER's text with the completed candidate and
// returns the caret position after it (the last path segment is replaced for a
// file — dsh's completion behaviour).
function applyCompletion(text,candidate,trigger){
  if(trigger===TRIGGER_SLASH){return {text:TRIGGER_SLASH+candidate+' ',caret:candidate.length+2};}
  const upto=text.slice(0,input.selectionStart==null?text.length:input.selectionStart);
  const at=upto.lastIndexOf(TRIGGER_AT);
  const head=at<0?text:text.slice(0,at)+TRIGGER_AT;
  const tail=text.slice(input.selectionStart==null?text.length:input.selectionStart);
  return {text:head+candidate+tail,caret:head.length+candidate.length};
}

// menuComplete accepts the highlighted candidate: it fills the composer (never
// sends) and closes the menu.
function menuComplete(candidate){
  const c=candidate!=null?candidate:(menu.items[menu.sel]||'');
  if(!c){menuClose();return;}
  const next=applyCompletion(input.value,c,menu.trigger);
  input.value=next.text;
  menuClose();
  autogrow();
  if(typeof input.setSelectionRange==='function'){input.setSelectionRange(next.caret,next.caret);}
}

// menuSteerPrompt is the ONE steering gesture this increment lands: Alt+Enter
// (or Alt-click on Send) posts '!steer <message>' — the shared convention the
// SERVER recognizes and strips — and renders the entry with the STEERING
// placement, so a message that addresses the run in flight is visibly typed as
// such (dsh's QueuedInboxItem.placement: 'queued' | 'steering').
function menuSteerPrompt(message){return STEER_PREFIX+message;}

// R5 upload plumbing: stage images via 📎 / paste / drag-drop, render
// thumbnails in the strip above the input, ship {media_type,data_b64} with
// the next message, clear on send.
function stageFiles(files){
  for(const f of files){
    if(!f||!f.type||!f.type.startsWith('image/'))continue;
    if(images.length>=MAX_IMAGES){addCommand('','image limit reached ('+MAX_IMAGES+' per message) — drop some first',true);break;}
    const rd=new FileReader();
    rd.onload=()=>{
      const b64=(rd.result||'').split(',')[1]||'';
      if(b64.length>MAX_B64){addCommand('','image too large: '+f.name+' (over 32 MiB)',true);return;}
      images.push({media_type:f.type,data_b64:b64,name:f.name});
      renderStrip();
    };
    rd.readAsDataURL(f);
  }
}
function renderStrip(){
  strip.innerHTML='';
  images.forEach((im,i)=>{
    const d=document.createElement('div');d.className='imgthumb';
    const img=document.createElement('img');
    img.src='data:'+im.media_type+';base64,'+im.data_b64;
    const rm=document.createElement('button');rm.className='rm';rm.type='button';rm.textContent='✕';
    rm.onclick=()=>{images.splice(i,1);renderStrip();};
    d.appendChild(img);d.appendChild(rm);
    if(im.sizeMB){const st=document.createElement('div');st.className='st';st.textContent=im.sizeMB;d.appendChild(st);}
    strip.appendChild(d);
  });
}
// attachCommandImages stages the pictures a SLASH COMMAND produced into the
// SAME images[] a 📎 upload and a 📷 capture use. There is deliberately no
// second staging path: the array is what renderStrip() draws and what the next
// Enter ships in the /api/chat body, so a command's picture becomes a picture
// the model receives through the existing machinery and nothing new.
//
// The server decides what it can attach (readable, ≤ the shared byte budget,
// a real PNG) and reports its own refusals as an error frame; this side only
// has to stay inside the per-message COUNT cap — and when it cannot, it SAYS
// so instead of dropping pictures quietly (a command that drew five pages and
// attached three must not look like it drew three).
function attachCommandImages(list,cmdline,failed){
  if(failed)return;               // an error frame's pictures never rode with it
  let dropped=0;
  for(const im of list){
    if(!im||!im.data_b64)continue;
    if(images.length>=MAX_IMAGES){dropped++;continue;}
    images.push({media_type:im.media_type||PNG_MEDIA,data_b64:im.data_b64,name:'command.png'});
  }
  renderStrip();
  if(dropped){
    addCommand(cmdline,'image limit reached ('+MAX_IMAGES+' per message) — '+dropped+' 张图未附上，先撤下一些再重新执行命令',true);
  }
}
// ---------------------------------------------------------------- MICROPHONE
// (web_voice.go, route B). A click starts a recording ON THE SERVER — the
// microphone is the one attached to the machine running gopi web, which is the
// price of the web and TUI carriers sharing ONE device policy — and the second
// click stops it, transcribes it, and DROPS THE TEXT INTO THE COMPOSER as an
// editable draft. It never sends: the operator reads it, fixes the engine's
// mistakes, and presses Enter, which is exactly the TUI /voice contract.
//
// THE STATE IS THE SERVER'S, NOT THE CLICK'S. micBusy is set only from a
// successful answer, so a device that refused to open (a 503 with its own
// sentence) leaves the button idle instead of pulsing for a recording that does
// not exist. A failed STOP re-tries nothing and says why: a second stop of a
// recording that already ended is an error the server names.
let micBusy=false;
// micInsert puts a draft where the operator is TYPING: at the caret of a
// composer they may already be editing, never at the end of it (appending after
// the caret would move text the operator wrote out from under them). A space is
// added on each side only when one is missing, so two drafts in a row do not run
// together and a draft is never glued to a word.
function micInsert(draft){
  if(!draft)return null;
  const cur=input.value||'';
  const at=(typeof input.selectionStart==='number')?input.selectionStart:cur.length;
  const head=cur.slice(0,at), tail=cur.slice(at);
  const sep=(head&&!/\s$/.test(head))?' ':'';
  const next=head+sep+draft+tail;
  input.value=next;
  // THE CARET IS PLACED BY ARITHMETIC ON WHAT WAS INSERTED, then CLAMPED into
  // the composer that now exists (micCaretTo). An index computed from a caret
  // the page believes it has — while the real one is elsewhere, which is the
  // state a live settle leaves behind — would otherwise point past the end or
  // into text this step did not write.
  micCaretTo(head.length+sep.length+draft.length);
  // THE SETTLED SENTENCE IS RECORDED AS A WHOLE-VALUE IDENTITY, so Shift+Escape
  // can still discard it after the recording is over (see micVoiceRun). It is
  // recorded HERE because this is the ONE door voice text enters the composer
  // through: the live settle and the window stop both arrive at this line.
  //
  // THE SEPARATOR IS KEPT APART FROM THE HEAD (the separator is the space this
  // function ADDED; the head is what the operator had). Folding the two together
  // loses the operator's own trailing space on the way out: measured, "my own
  // words: " plus a settled sentence came back as "my own words:" — a character of
  // the operator's text eaten by the discard.
  micVoiceRun={head:head,sep:sep,text:draft,tail:tail,value:next};
  autogrow();
  input.focus();
  // The composer changed under the menu's feet, so the candidate list is
  // re-derived from the text that is now there (a draft ending in "/wo" is a
  // real query; one ending in a word is not).
  menuRefresh(true);
}
// micNotice renders the endpoint's answer in the operator's own log, so a
// refusal or a truncated capture is READ rather than inferred from a button
// that did not change (Rule 15: a failure is spoken, never swallowed).
function micNotice(kind,text,err){
  const div=document.createElement('div');
  div.className='msg command'+(err?' err':'');
  const head=document.createElement('div');
  head.className='cmdline';head.textContent=kind;
  div.appendChild(head);
  const pre=document.createElement('pre');
  pre.textContent=text||'';
  div.appendChild(pre);
  logEl.appendChild(div);
  follow(false);
  return div;
}
// micLabel writes the button's own state: the glyph tells a screen reader and the
// title tells a mouse, and neither says "recording" while nothing is.
function micSetState(recording){
  micBusy=!!recording;
  if(!micBtn)return;
  micBtn.classList.toggle('rec',micBusy);
  micBtn.textContent=micBusy?'⏹':'🎤';
  micBtn.title=micBusy?'stop recording and transcribe':'record a voice message — transcribes on the machine running gopi web and drops the text into the composer';
}
// micReadAnswer parses one voice answer: the HTTP status decides whether the
// endpoint's own sentence is an ERROR or a RESULT, and both are printed.
async function micReadAnswer(res){
  let body={};
  try{body=await res.json();}catch(e){body={};}
  if(!res.ok||body.ok===false){
    micSetState(false);
    micNotice('voice',body.error||('request failed: HTTP '+(res.status||'no stream')),true);
    return null;
  }
  return body;
}
let micLiveActive=false;
// micKeepCaret is set by micStopKeep (Escape's STOP-AND-KEEP) and consumed by the
// settle step that follows it: the sentence still lands where the caret was, but
// when the dust settles the caret moves to the END of the composer. The operator
// pressed Escape to start PROOF-READING what was recognised — the place to edit
// from is after the last character, never back at the position they happened to
// be typing at when the sentence arrived.
let micKeepCaret=false;
// THE LIVE DRAFT REGION. Both live backends publish the WHOLE text so far on
// every partial (never a delta), so a draft REPLACES the previous one. The
// region remembers the text that was in the composer BEFORE and AFTER it, so the
// operator can keep typing around a sentence that is still being recognised.
let liveDraft=null;
let liveClock=null;
// micCaretTo moves the caret to an index, CLAMPED into the composer: a real
// textarea keeps the caret inside its own text, but the page must not lean on
// that (the harness's shim, and a future <div contenteditable>, keep the raw
// number), and an out-of-range caret is exactly how a settled sentence ends up
// spliced into the middle of text that is no longer there.
function micCaretTo(at){
  const end=(input.value||'').length;
  const p=at>end?end:at;
  if(typeof input.setSelectionRange==='function'){input.setSelectionRange(p,p);}
}
function micCaretNow(){
  return (typeof input.selectionStart==='number')?input.selectionStart:(input.value||'').length;
}
// micDraftRegion locates the LIVE INTERIM REGION inside the composer AS IT
// STANDS NOW, and is the ONE reading of "where is the draft" — shared by
// micDraftClear and micDraftSet, because THE GUARD AND THE REMOVAL MUST AGREE
// ABOUT IT. That agreement is the whole fix: the version this replaced compared
// the region BY IDENTITY (cur.slice(head,head+len)===liveDraft.text) and then
// removed it BY LENGTH (head + cur.slice(head.length+liveDraft.text.length)). An
// operator who typed one character INSIDE the interim draft made the two
// disagree: the guard said "not mine any more" so nothing was removed,
// liveDraft was then nulled, and the recognised sentence was inserted at a
// caret sitting in the middle of the ORPHANED interim text — the composer read
// "请注意 尼X 尼好世界。好" instead of "请注意 尼X好 你好世界。".
//
// Three answers, and every caller handles all three:
//   intact — the region still holds exactly the engine's text.
//   edited — head and tail are still where they were, so the region is FOUND,
//            but what lies between them is the operator's (they typed into it).
//   lost   — not even the surrounding text is where it was (a select-all and
//            paste, a clear that has not run yet), so nothing can be located.
function micDraftRegion(){
  if(!liveDraft)return null;
  const cur=input.value||'';
  const d=liveDraft;
  if(cur.slice(0,d.head.length)!==d.head){return {lost:true};}
  if(cur.slice(cur.length-d.tail.length)!==d.tail){return {lost:true};}
  const mid=cur.slice(d.head.length,cur.length-d.tail.length);
  return {at:d.head.length,length:mid.length,intact:mid===d.text};
}
function micDraftClear(){
  if(!liveDraft)return;
  const d=liveDraft;
  // THE REGION IS READ BEFORE liveDraft IS RELEASED: the location is a fact
  // about the composer, not about the bookkeeping this function is ending.
  const region=micDraftRegion();
  const caret=micCaretNow();
  liveDraft=null;
  const cur=input.value||'';
  if(region&&!region.lost&&region.intact){
    // THE ENGINE'S TEXT IS STILL THERE VERBATIM: take exactly it out — the head
    // and the tail are spliced back unchanged, so the operator's surroundings
    // are never rewritten by this step.
    input.value=cur.slice(0,d.head.length)+cur.slice(cur.length-d.tail.length);
    // THE CARET IS TRANSLATED THROUGH THE DELETION, exactly as a textarea does
    // when a range is removed under it: before the region it is unchanged,
    // inside the region it collapses to the region's left edge, after it it
    // moves back by the removed length. Leaving the raw index behind is how a
    // settled sentence lands one character into text the operator typed.
    micCaretTo(caret>=d.head.length+d.text.length?caret-d.text.length
      :(caret>d.head.length?d.head.length:caret));
  }else if(region&&!region.lost){
    // THE OPERATOR EDITED INSIDE THE REGION, so their edit IS the region now:
    // it stays, because removing "a region that is no longer the engine's" can
    // only be done by slicing a length the engine guessed — and that slice is
    // the defect. What must not happen is the settle step inserting INTO the
    // fragment, so a caret parked inside it is moved to the region's END: the
    // recognised sentence then lands AFTER what the operator wrote.
    const end=region.at+region.length;
    if(caret>=region.at&&caret<end){micCaretTo(end);}
  }
  // NOT LOCATED (region.lost): nothing is removed and nothing is moved. The
  // composer is the operator's, and guessing where the engine's text used to be
  // would destroy typing to tidy up a bookkeeping entry.
  autogrow();
}
function micDraftSet(text){
  if(!liveDraft){
    const cur=input.value||'';
    const at=(typeof input.selectionStart==='number')?input.selectionStart:cur.length;
    const head=cur.slice(0,at), tail=cur.slice(at);
    // A space is added on each side only when one is missing, exactly like
    // micInsert, so a draft is never glued to a word the operator typed.
    liveDraft={head:head+((head&&!/\s$/.test(head))?' ':''),text:'',tail:tail};
  }
  liveDraft.text=text;
  input.value=liveDraft.head+liveDraft.text+liveDraft.tail;
  micCaretTo(liveDraft.head.length+liveDraft.text.length);
  autogrow();
  // The menu is NOT re-derived on every partial (a sentence being recognised is
  // not a command query, and a per-partial rebuild would spend a round trip per
  // packet); micInsert re-derives it ONCE when the sentence settles.
}
// micLiveState drives the recording state OUTSIDE the button: the device and the
// elapsed seconds. The glyph alone cannot answer "how long has this been open",
// and a microphone left open by a forgotten tab is the failure that question
// catches.
function micLiveState(on,device){
  if(liveClock){clearInterval(liveClock);liveClock=null;}
  if(!voiceLiveEl)return;
  if(!on){voiceLiveEl.hidden=true;voiceLiveEl.textContent='';return;}
  voiceLiveEl.hidden=false;
  const name=device||'microphone';
  const t0=Date.now();
  const draw=()=>{voiceLiveEl.textContent='● recording '+name+' '+Math.floor((Date.now()-t0)/1000)+'s';};
  draw();
  liveClock=setInterval(draw,1000);
}
// micLiveRun performs ONE live utterance: it opens the streaming route, draws
// every draft as it arrives, and returns the FINAL frame's body. A 501 is the
// server's NAMED refusal ("this engine has no live arm") and is the ONE answer
// that sends the caller down the window path; every other failure is spoken.
async function micLiveRun(){
  const res=await fetch(withToken(API_VOICE_LIVE),{method:'POST'});
  if(res.status===HTTP_NOT_IMPLEMENTED){
    let body={};
    try{body=await res.json();}catch(e){body={};}
    return {fallback:true,error:body.error||''};
  }
  if(!res.ok||!res.body){
    let body={};
    try{body=await res.json();}catch(e){body={};}
    return {error:body.error||('the live voice endpoint refused: HTTP '+(res.status||'no stream'))};
  }
  const reader=res.body.getReader();
  const decoder=new TextDecoder();
  let buf='',final=null;
  while(true){
    const step=await reader.read();
    if(step.done)break;
    buf+=decoder.decode(step.value,{stream:true});
    let idx;
    while((idx=buf.indexOf('\n\n'))>=0){
      const frame=buf.slice(0,idx);buf=buf.slice(idx+2);
      let ev='message',data='';
      for(const line of frame.split('\n')){
        if(line.startsWith('event:'))ev=line.slice(6).trim();
        else if(line.startsWith('data:'))data+=line.slice(5).trim();
      }
      let payload={};
      try{payload=JSON.parse(data||'{}');}catch(e){payload={};}
      if(ev===EV_LIVE_READY){micLiveState(true,payload.device);}
      else if(ev===EV_LIVE_DRAFT){micDraftSet(payload.draft||'');}
      else if(ev===EV_LIVE_FINAL){final=payload;}
    }
  }
  micLiveState(false);
  return {final:final||{}};
}
// micSettleLive hands the settled sentence to the composer through the SAME
// road the window path uses (micInsert), and reports the arm's own verdicts.
// A CANCELLED utterance settles NOTHING: the server answers draft:"" plus
// cancelled:true for a recording the operator ended with Escape, and inserting
// what it heard would be the exact loss the cancel route exists to prevent — so
// the discard is checked BEFORE any notice, and it is silent (the operator knows
// what they did; a warning row for a deliberate act is noise).
function micSettleLive(body){
  // THE STOP HAS LANDED — whatever the verdict, the page is no longer waiting on
  // one, and Escape must be arbitrated again from scratch (see micEscAct). This
  // is the FIRST statement because the cancelled branch below returns early, and
  // a flag left set there would wedge every later Escape into the recording rung.
  micStopping=false;
  micDraftClear();
  // THE DISCARD IS TESTED BY ITS VALUE, not by the field's truthiness. The stop
  // route's answer carries NO "cancelled" key at all (webVoiceStopResult has
  // ok/draft/transcript/device/notes/truncated/tail_dropped), so "if(body.cancelled)"
  // read as "false" only because the field was MISSING — correct by accident, and
  // silently inverting the day a route starts sending an explicit "false" or a
  // later edit asks "is it not false?". The verdict is "true" or it is not a
  // discard; anything else settles.
  if(body.cancelled===true){return;}
  const verdict=[];
  if(body.draft){micInsert(body.draft);}
  else{verdict.push('nothing was said — the live utterance carried no speech');}
  if(body.timed_out){verdict.push('the engine did not answer within the session bound — the tail may be missing');}
  if(body.truncated){verdict.push('the recording hit the length ceiling and was cut short');}
  if(body.error){verdict.push(body.error);}
  if(verdict.length){micNotice('voice',verdict.join('; '),true);}
  // THE CARET SETTLES LAST, after whatever the sentence inserted: Escape's
  // STOP-AND-KEEP ends with the operator proof-reading from the end, not from
  // wherever they happened to be typing when the sentence landed.
  micCaretSettle();
}
// micCaretToEnd puts the caret AFTER the last character and focuses the composer:
// the end state of a STOP-AND-KEEP, where the operator's next act is to
// proof-read what was recognised (an ASR result always needs a human pass).
function micCaretToEnd(){
  micCaretTo((input.value||'').length);
  input.focus();
}
// micCaretSettle runs the deferred half of micStopKeep. It is called at the END of
// every settle step, after the sentence (if any) has been inserted, so the caret
// lands after the text rather than at the position the operator was typing at.
function micCaretSettle(){
  if(!micKeepCaret)return;
  micKeepCaret=false;
  micCaretToEnd();
}
// micRecordingOpen reports whether a recording is open — on EITHER route. The two
// are mutually exclusive on the server (one microphone, one 409), so asking both
// flags is asking "is a recording running", never "which one do I mean".
function micRecordingOpen(){return !!(micLiveActive||micBusy);}
// THE FOUR MEANINGS OF ESCAPE, and the ONE function that decides between them.
//
// Escape used to be arbitrated in two places with two different guards (the
// composer's listener asked micRecordingOpen, the bubbling one asked the same),
// which is one decision too many — and neither guard could see the SETTLE WINDOW:
// the recording has already been stopped, the engine's sentence is already in the
// composer, no request is in flight and busy is false. There, Escape fell through
// every branch and did nothing at all, which is the same operator experience as a
// broken key ("I pressed Escape and the page ignored me").
//
// So the answer is ONE ordered decision, and each rung is disjoint from the rest:
//
//	ESC_MENU       the input-trigger menu owns the key (it closes; no draft is touched)
//	ESC_RECORDING  a recording is open OR its stop is still travelling — STOP AND KEEP
//	ESC_VOICE_TEXT a VOICE LAYER IS ON SCREEN (see micVoiceLayerUp) — the layer's
//	               own act, even while a turn streams: KEEP it, caret last
//	ESC_TURN       a turn is in flight AND no voice layer is on screen — abort it
//	ESC_NONE       nothing to arbitrate: the browser's own default stands
//
// The rungs CANNOT BOTH FIRE: micRecordingOpen() is the recording, the voice-layer
// rung is the settled sentence STILL ON SCREEN, and ESC_TURN is a running turn with
// no such layer up — so the turn rung and the voice rung are now one either/or
// decided by ONE measured fact instead of by position. The order is the whole
// arbitration, so both key listeners read THIS function instead of re-deriving the
// answer — a second copy is how the two listeners would come to disagree.
const ESC_NONE='';
const ESC_MENU='menu';
const ESC_RECORDING='recording';
const ESC_VOICE_TEXT='voice-text';
const ESC_TURN='turn';
// micVoiceLayerUp reports whether a VOICE LAYER IS ON SCREEN — the state that
// outranks a streaming turn.
//
// THE DEFECT THIS REPLACES (operator, 2026-11-09, measured on the real page). The
// arbiter used to ask "if(busy)return ESC_TURN;" BEFORE the settled-sentence rung,
// on the recorded premise that the operator's Escape while a turn streams means
// 'stop this answer'. That premise is HALF true and was never measured against
// the case that contradicts it: with the voice layer up AND a turn streaming, the
// operator's intent is not "stop this answer" — it is "close this layer", which is
// every GUI's contract for Escape on a visible overlay. Measured: the key aborted
// the response and left the voice layer exactly where it was, i.e. the ONE visible
// thing on screen was the ONE thing the key did not address.
//
// WHY THE RUNG IS NOT SIMPLY MOVED (the correction inside the correction). The old
// comment's measurement is REAL and still binds: with the settled-sentence rung
// placed first unconditionally, a turn in flight made the arbiter answer
// voice-text and the turn kept running — the key lost the interrupt it has always
// had. So the criterion is NOT "is there a settled sentence" (a sentence left over
// from ten minutes ago is not a layer; it must not steal the interrupt) — it is
// "is a voice layer VISIBLE".
//
// VISIBILITY IS MEASURED, NOT GUESSED, and it is the SAME measurement the discard
// already uses: the recorded run describes a layer on screen exactly while the
// composer still holds the value voice left there. The moment the operator types
// into or around it, or clears the composer, or sends the turn, the record no
// longer matches — the sentence is no longer a layer this key owns, and the turn
// rung takes the key back. That is why this predicate is a comparison and not a
// flag: a flag would have to be cleared from five call sites, and every missed one
// would be a key that silently dies in the middle of a stream.
function micVoiceLayerUp(){
  const run=micVoiceRun;
  if(!run)return false;
  return (input.value||'')===run.value;
}
function micEscAct(){
  if(menu.open)return ESC_MENU;
  // A RECORDING OUTRANKS THE TURN: a recording runs while no turn does, so this
  // rung would never fire for it if TURN came first.
  if(micRecordingOpen()||micStopping)return ESC_RECORDING;
  // A VISIBLE VOICE LAYER OUTRANKS THE TURN — the operator's report, and the same
  // principle the recording rung above already follows: an INPUT INTERACTION that
  // is on screen owns Escape ahead of the response it is layered over.
  if(micVoiceLayerUp())return ESC_VOICE_TEXT;
  if(busy)return ESC_TURN;
  // THE RESIDUAL SENTENCE KEEPS ITS OLD RUNG, unchanged and deliberately BELOW the
  // turn. It is not a layer on screen — the composer no longer holds what voice
  // inserted — so it must not steal the interrupt (that is the measured reason the
  // turn rung exists at all). It still answers Escape while nothing is streaming,
  // because the one thing it can do there is REFUSE the discard out loud, and a
  // Shift+Escape on a stale record that does nothing at all is the broken-key
  // experience this file exists to end (Rule 15).
  if(micVoiceRun)return ESC_VOICE_TEXT;
  return ESC_NONE;
}
// micVoiceRun is THE SENTENCE THE ENGINE SETTLED INTO THE COMPOSER — the run of
// text voice just inserted, remembered as an EXACT reconstruction of the value it
// produced (prefix + text + suffix). It exists for ONE purpose: Shift+Escape's
// DISCARD, which must be able to take the voice sentence back out after the
// recording is over — and must never take anything else out with it.
//
// THE RECORD IS A WHOLE-VALUE IDENTITY, not a range. "Remove characters 8..15" is
// only correct while those characters are still the engine's; the moment the
// operator types into or around the sentence, that arithmetic slices their typing
// instead (the defect micDraftRegion was written to end, one layer up). So the
// discard re-derives the value from the record and compares it to what is in the
// composer NOW: equal ⇒ the sentence is exactly where voice put it ⇒ remove it;
// not equal ⇒ the operator has touched it ⇒ the discard REFUSES and says so.
let micVoiceRun=null;
// micStopping is true from the moment a stop or a cancel has been REQUESTED until
// its settle step ends. Without it a second Escape re-sends the stop (the stream is
// still open while the request travels), paying a second round trip to be told
// what the page already knew.
let micStopping=false;
// micVoiceText reports the settled sentence that is still in the composer, or ''.
function micVoiceText(){return micVoiceRun?micVoiceRun.text:'';}
// micEscRun performs the act micEscAct chose. The discard flag is the Shift
// modifier, and it is read ONLY on the two rungs where it means something
// (discarding a recording, discarding a settled sentence); on the other three it
// is ignored, so Shift+Escape can never abort a turn or close a menu differently —
// a modifier the act does not own is not silently given a second meaning.
function micEscRun(act,discard){
  if(act===ESC_MENU){menuClose();return;}
  if(act===ESC_RECORDING){if(discard){micCancel();}else{micStopKeep();}return;}
  if(act===ESC_VOICE_TEXT){
    // THE SETTLE WINDOW. There is no request left to send — the recording already
    // ended and the engine's sentence is already in the composer — so plain Escape
    // does the KEEP half's visible part (put the operator at the end to
    // proof-read) and touches no text, and Shift+Escape takes the sentence back out
    // through micCancel's settle-window arm. Nothing here can reach the server.
    if(discard){micCancel();}else{micCaretToEnd();}
    return;
  }
  if(act===ESC_TURN){stopTurn();return;}
}
// micCancel is SHIFT+ESCAPE'S ACT: end the open recording and DISCARD what was
// said. It is NOT what plain Escape does (see micStopKeep) — an utterance the
// operator decided not to say is the rarer intent, so it moved behind a modifier:
// the destructive act is the one nobody trips into.
//
// THE TWO ACTS MUST NOT SHARE A VERB. "Stopped" and "cancelled" differ in the one
// thing that matters — whether the sentence reaches the composer — so each route
// says which it is and the server's final answer carries the verdict (a page-side
// "ignore the draft" would still pay the engine call and still put the sentence in
// the server's log).
//
// A 409 is NOT AN ERROR HERE: it means the recording already ended (the ceiling,
// the tab, a race with ⏹), which is the state the caller was asking for. Every
// other refusal is spoken (Rule 15).
function micResetState(){
  micSetState(false);
  micDraftClear();
  micLiveState(false);
  micStopping=false;
}
async function micCancel(){
  let route='';
  if(micLiveActive){route=API_VOICE_LIVE_CANCEL;}
  else if(micBusy){route=API_VOICE_CANCEL;}
  else{
    // NO RECORDING IS OPEN — the SETTLE WINDOW. The discard must still be
    // reachable here, or the one act that throws a sentence away would be
    // available only while the microphone happens to be open, and the operator
    // who reads their transcript a second too late would have no answer at all.
    // The server route is not sent: there is no live session left to cancel
    // (it ended when the stop settled), so the discard is purely the page's,
    // and it is the one case where the page — not the engine — is the owner of
    // the text.
    return micDiscardSettled();
  }
  // THE LOCAL STATE IS CLEARED FIRST, deliberately: the cancel is the operator's
  // decision and the button must not keep pulsing red while the request travels
  // (the recording is over the moment they said so). A refusal below re-states
  // itself in the log; the server's own 409 path is the same state.
  micResetState();
  micStopping=true;
  try{
    const res=await fetch(withToken(route),{method:'POST'});
    if(!res.ok){
      let body={};
      try{body=await res.json();}catch(e){body={};}
      if(res.status===HTTP_CONFLICT){
        micNotice('voice','the recording had already ended, so nothing was left to cancel');
      }else{
        micNotice('voice',body.error||('the cancel request was refused: HTTP '+(res.status||'?')),true);
      }
    }else{
      // THE DISCARD IS SPOKEN. It used to be silent on the theory that "the
      // operator knows what they did" — but a key that deletes a sentence and
      // leaves no trace is indistinguishable from a key that did nothing, and
      // the two need opposite follow-ups (one is finished, the other is a bug
      // report). One line, and it names the size of what went.
      micNotice('voice',micDiscardReport());
    }
  }catch(err){
    micNotice('voice','the cancel request could not be reached: '+(err&&err.message?err.message:String(err)),true);
  }finally{
    micStopping=false;
  }
  return true;
}
// micDiscardReport is what the log says after a discard, and it is built from
// what was ACTUALLY thrown away — a notice claiming a sentence was discarded when
// the engine never recognised one is a lie the operator cannot check.
function micDiscardReport(){
  return 'the recording was discarded — what it heard was not kept';
}
// micDiscardSettled is Shift+ESCAPE'S ACT IN THE SETTLE WINDOW: take the
// sentence the engine settled back out of the composer.
//
// THE REFUSAL IS THE INTERESTING HALF. The sentence can only be removed while the
// composer is EXACTLY what voice left there; the moment the operator typed into it
// or around it, the characters at that position are no longer the engine's, and
// removing "the sentence" would delete their typing instead. Deleting the
// operator's own words is the one outcome this whole feature exists to prevent, so
// the mismatched case REFUSES — and says so, because a discard that silently does
// nothing is a key that looks broken.
function micDiscardSettled(){
  const run=micVoiceRun;
  if(!run){
    micNotice('voice','there is no recognised sentence to discard — the composer holds only what you typed');
    return false;
  }
  if((input.value||'')!==run.value){
    // THE REFUSAL NAMES WHAT IT MEASURED, not what it guessed. Two different
    // situations land here — the operator typed into or around the sentence, or
    // the composer was cleared (Ctrl+U, sending the turn) — and in both the
    // sentence is no longer a run this function can cut out without cutting
    // something else. Guessing which one and reporting it would be a claim the
    // measurement does not support (Rule 41), so the sentence says exactly what
    // the comparison found.
    micNotice('voice','the composer no longer holds what voice inserted, so nothing was discarded — removing the recognised sentence now would take your own text with it');
    return false;
  }
  micVoiceRun=null;
  // THE VALUE IS REBUILT FROM THE RECORD (head + tail), never sliced off the end:
  // the sentence sits where voice PUT it, which is not where it would be if the
  // operator had moved the caret since. The separator is dropped with the
  // sentence — it existed only to keep the sentence off the operator's last word.
  const caretWas=micCaretNow();
  const seam=run.head.length;
  input.value=run.head+run.tail;
  // THE CARET IS TRANSLATED THROUGH THE DELETION the same way a textarea does it:
  // before the removed run it is unchanged, at or after it, it collapses to the
  // seam.
  micCaretTo(caretWas>=seam+run.sep.length+run.text.length?seam
    :(caretWas>seam?seam:caretWas));
  autogrow();
  micNotice('voice',micDiscardReport());
  return true;
}
// micStopKeep is PLAIN ESCAPE'S ACT: end the open recording, KEEP what was
// recognised, and put the caret at the end of the composer. It is the stop route,
// not the cancel route — "I have said enough" and "I never meant to say this" are
// two different intents, and only the second may throw the sentence away.
//
// WHY THIS IS THE DEFAULT AND DISCARD IS NOT (the defect this replaced): the page
// bound Escape to micCancel, which cleared the composer AND asked the server for
// "cancelled:true, draft:""". Pressing the key an operator reaches for when they
// start PROOF-READING a half-right transcript therefore deleted that transcript —
// irreversibly, since the composer has no undo stack. An ASR result needs a human
// pass by nature; the key that says "I am about to edit this" must not erase it.
//
// THE SENTENCE ARRIVES LATER, on the live stream the first click is reading — so
// this returns once STOP is accepted and micCaretSettle (below, called from the
// settle step) moves the caret once the text is actually there.
function micCancelKeepFlag(){micKeepCaret=false;}
async function micStopKeep(){
  // A STOP ALREADY TRAVELLING IS NOT SENT TWICE: the first Escape is the
  // operator's decision and the request is on the wire — a second keypress adds a
  // round trip that can only answer 409, and the notice that follows would report
  // a race as if it were the operator's mistake.
  if(micStopping)return true;
  if(micLiveActive||micBusy){
    micStopping=true;
  }
  if(micLiveActive){
    micKeepCaret=true;
    micBtn.disabled=true;
    try{
      const res=await fetch(withToken(API_VOICE_LIVE_STOP),{method:'POST'});
      if(!res.ok){
        let body={};
        try{body=await res.json();}catch(e){body={};}
        if(res.status===HTTP_CONFLICT){
          // The recording ended on its own (the ceiling, another tab): the state
          // the caller asked for, so the caret still settles where they expect.
          micNotice('voice','the recording had already ended; what it recognised is in the composer');
        }else{
          micCancelKeepFlag();
          micNotice('voice',body.error||('the stop request was refused: HTTP '+(res.status||'?')),true);
        }
      }
    }catch(err){
      micCancelKeepFlag();
      micNotice('voice','the stop request could not be reached: '+(err&&err.message?err.message:String(err)),true);
    }finally{
      micBtn.disabled=false;
    }
    return true;
  }
  if(!micBusy)return false;
  micKeepCaret=true;
  micBtn.disabled=true;
  try{
    micSetState(true);
    const res=await fetch(withToken(API_VOICE_STOP),{method:'POST'});
    const body=await micReadAnswer(res);
    if(!body){micCancelKeepFlag();return true;}
    micSetState(false);
    const verdict=[];
    if(body.draft){micInsert(body.draft);}
    else{verdict.push('nothing was said — the transcript was empty, so nothing was inserted');}
    if(body.truncated){verdict.push('the recording hit the length ceiling and was cut short');}
    if(body.tail_dropped){verdict.push('the stop deadline was spent; the last window of audio was lost');}
    if(verdict.length){micNotice('voice',verdict.join('; '),true);}
    micCaretSettle();
  }finally{
    micStopping=false;
    micBtn.disabled=false;
  }
  return true;
}
async function micToggle(){
  if(!micBtn||micBtn.disabled)return;
  // THE SECOND CLICK WHILE THE STREAM IS OPEN ENDS THE UTTERANCE. The sentence
  // arrives on the stream the first click is reading, so this request answers at
  // once and does not wait for the transcript (the button would otherwise stay
  // down for the length of the engine's final answer).
  if(micLiveActive){
    micBtn.disabled=true;
    try{
      const res=await fetch(withToken(API_VOICE_LIVE_STOP),{method:'POST'});
      if(!res.ok){
        let body={};
        try{body=await res.json();}catch(e){body={};}
        micNotice('voice',body.error||('the stop request was refused: HTTP '+(res.status||'?')),true);
      }
    }catch(err){
      micNotice('voice','the stop request could not be reached: '+(err&&err.message?err.message:String(err)),true);
    }finally{
      micBtn.disabled=false;
    }
    return;
  }
  // THE BUTTON IS NOT RE-ENTRANT ON THE WINDOW PATH: the start half waits on a
  // device probe (~1 s), and a second click inside that window would ask for a
  // second recording the server would refuse.
  micBtn.disabled=true;
  try{
    if(micBusy){
      micSetState(true);
      const res=await fetch(withToken(API_VOICE_STOP),{method:'POST'});
      const body=await micReadAnswer(res);
      if(!body)return;
      micSetState(false);
      // THE DRAFT IS THE ANSWER, and it is ALWAYS a draft (never a sent turn,
      // never a slash command): webVoiceDraftText has already neutralised a
      // transcript that starts with "/" into the carrier's literal-send form.
      const verdict=[];
      if(body.draft){micInsert(body.draft);}
      else{verdict.push('nothing was said — the transcript was empty, so nothing was inserted');}
      if(body.truncated){verdict.push('the recording hit the length ceiling and was cut short');}
      if(body.tail_dropped){verdict.push('the stop deadline was spent; the last window of audio was lost');}
      if(verdict.length){micNotice('voice',verdict.join('; '),true);}
      return;
    }
    // THE LIVE ARM COMES FIRST. The route is the same engine seam the chat
    // surface's /voice runs, so the page gets partials while the operator
    // speaks; an
    // engine without a live arm answers 501 and the EXISTING start/stop path
    // below runs — the fallback is a first-class path, not an error handler.
    micLiveActive=true;
    micSetState(true);
    // THE BUTTON STAYS CLICKABLE WHILE THE STREAM IS OPEN — micLiveActive is the
    // re-entrancy guard (the branch at the top of this function turns the second
    // click into the STOP), and the disabled=true above only had to cover the
    // moment before that flag was set. Leaving it set made STOP UNREACHABLE:
    // measured on the real page (evid/access-after-prev.log, step=after_click),
    // a click at t+9 s of a running utterance produced NO request at all while
    // #voicelive kept counting — the only way out of a recording was to wait for
    // the ceiling. The ⏹ then remains STOP AND TRANSCRIBE, which is what the
    // Escape key is defined AGAINST (micCancel discards) — plain Escape now
    // STOPS AND KEEPS like this button, minus the caret-at-end step.
    micBtn.disabled=false;
    const live=await micLiveRun().catch(err=>({error:'the live voice endpoint could not be reached: '+(err&&err.message?err.message:String(err))}));
    micLiveActive=false;
    micSetState(false);
    // THE STREAM HAS CLOSED, so nothing is travelling any more whatever the
    // verdict below (a fallback, an error, a settled sentence). micSettleLive also
    // clears it — this covers the two paths that do not reach it.
    micStopping=false;
    if(live.final){micSettleLive(live.final);return;}
    if(!live.fallback){
      micDraftClear();
      micNotice('voice',live.error||'the live utterance ended without an answer',true);
      return;
    }
    // THE FALLBACK IS ANNOUNCED, never silent: the operator learns WHICH engine
    // could not stream, and the capture they are about to make is the one-shot
    // kind (nothing appears until they stop).
    micNotice('voice',(live.error||'this engine has no live streaming arm')+' — recording the one-shot way instead');
    const res=await fetch(withToken(API_VOICE_START),{method:'POST'});
    const body=await micReadAnswer(res);
    if(!body)return;
    micSetState(true);
    micNotice('voice','recording — '+body.device+' (click 🎤 again to stop and transcribe)');
  }catch(err){
    micSetState(false);
    micDraftClear();
    micNotice('voice','the voice endpoint could not be reached: '+(err&&err.message?err.message:String(err)),true);
  }finally{
    micBtn.disabled=false;
  }
}
if(micBtn){micBtn.addEventListener('click',micToggle);}
clipBtn.addEventListener('click',()=>fileEl.click());
// R6: «📷» — capture THIS page's server-side rendering and stage it as an
// attachment. The endpoint accepts exactly ONE parameter, "viewport", which
// carries this window's own innerWidth/innerHeight (see capturePage); the TARGET
// is never a parameter — the server always captures its own page. The returned
// {image:{media_type,data_b64}} has exactly the shape stageFiles pushes, so the
// picture rides the SAME images[] → renderStrip() → /api/chat path an uploaded
// file does — no second upload mechanism.
//
// A capture costs one headless-browser start (seconds), so the button is
// disabled and marked .busy for the duration, and it refuses locally at the
// image cap instead of spending that start on a picture that cannot be sent.
// Every failure is RENDERED (strip .shoterr + the server's own message): a
// capture that fails silently would look like a button that does nothing.
let shotBusy=false;
function shotFail(reason){
  const node=document.createElement('div');
  node.className='shoterr';
  node.textContent=reason;
  strip.appendChild(node);
}
async function capturePage(){
  if(shotBusy)return;
  if(images.length>=MAX_IMAGES){
    shotFail('image limit reached ('+MAX_IMAGES+' per message) — drop some first');
    return;
  }
  shotBusy=true;
  shotBtn.classList.add('busy');
  shotBtn.disabled=true;
  // Clear a PREVIOUS FAILURE LINE and nothing else. This used to be
  // strip.innerHTML='', which emptied the whole container — so a capture that
  // failed also erased the thumbnails of images the operator had already
  // staged, while images[] kept them: the pictures vanished from the screen and
  // the next Enter still shipped them. The strip's thumbnails are renderStrip()'s
  // output and are redrawn from images[] alone; a failure line is the only thing
  // this function may remove.
  strip.querySelectorAll('.shoterr').forEach(n=>n.remove());
  try{
    // THE VIEWPORT IS THE CALLER'S, and it is the ONLY thing this request may
    // carry — the server's parser refuses any other key by name (SSRF/privacy:
    // the capture target is always the server's own page). Sending the real
    // window size is what makes the picture MATCH what the operator is looking
    // at; without it the server falls back to its own 1440x2800 default and the
    // thumbnail shows a desktop-wide page the operator never saw.
    //
    // window.innerWidth/innerHeight are CSS PIXELS. They are sent UNSCALED and
    // the server does not multiply by devicePixelRatio (CDP runs with
    // deviceScaleFactor 1), so on a 2x display the PNG is half the physical
    // pixels of the window — the geometry matches the LAYOUT, which is what
    // makes the picture read the same as the screen. Both are rounded DOWN and
    // floored at 1 so a fractional/zero reading can never send a non-integer or
    // a zero (the server treats 0 as "use the default", which would silently
    // swap the operator's window for 1440x2800).
    const vp={
      w:Math.max(1,Math.floor(window.innerWidth||0)),
      h:Math.max(1,Math.floor(window.innerHeight||0)),
    };
    const res=await fetch(withToken(API_SHOT),{
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({viewport:vp}),
    });
    const text=await res.text().catch(()=>'');
    let payload=null;
    try{payload=JSON.parse(text||'{}');}catch(parseErr){payload=null;}
    if(!res.ok){
      const detail=(payload&&payload.error)||text||'';
      shotFail('capture failed: HTTP '+(res.status||'no status')+(detail?(' — '+detail):''));
      return;
    }
    const im=payload&&payload.image;
    if(!im||!im.data_b64){
      shotFail('capture failed: the server returned no image');
      return;
    }
    images.push({media_type:im.media_type||PNG_MEDIA,data_b64:im.data_b64,name:'page.png'});
    renderStrip();
  }catch(err){
    shotFail('capture failed: '+((err&&err.message)||err));
  }finally{
    shotBusy=false;
    shotBtn.classList.remove('busy');
    shotBtn.disabled=false;
  }
}
shotBtn.addEventListener('click',()=>{capturePage();});
fileEl.addEventListener('change',()=>{stageFiles([...fileEl.files]);fileEl.value='';});
document.addEventListener('paste',e=>{
  const items=[...(e.clipboardData||{items:[]}).items||[]].filter(i=>i.kind==='file');
  if(items.length){e.preventDefault();stageFiles(items.map(i=>i.getAsFile()));}
});
const logDropTarget=document.getElementById('log');
['dragover','dragleave','drop'].forEach(evName=>{
  document.addEventListener(evName,e=>{e.preventDefault();logDropTarget.classList.toggle('dragover',evName==='dragover');});
});
document.addEventListener('drop',e=>{stageFiles([...(e.dataTransfer||{files:[]}).files||[]]);});

// R4 refresh recovery: replay the server-owned transcript so a reload
// doesn't blank the display (image turns render a marker — pixels aren't
// re-shipped by /api/transcript).
//
// B5: tool turns replay too. /api/transcript now carries each tool row's name
// plus its call/result digests, so a refreshed page rebuilds the SAME
// collapsible card the live stream drew: the call row opens a card, the
// matching result row (paired by call id) appends "→ output" and collapses it.
async function replayTranscript(){
  try{
    const res=await fetch(withToken(API_TRANSCRIPT));
    if(!res.ok)return;
    const {messages}=await res.json();
    const openCards={};   // B5: call id → the card still waiting for its result
    for(const m of messages||[]){
      // THE REPLAYED BANNER. session_turn is the RECORD's ordinal for this row
      // (the server reads it from the session JSONL), so a refreshed page shows
      // the same numbers the live frames gave, and a continued conversation's
      // history keeps 1..20 instead of being renumbered from 1.
      if(m.role==='user'){
        if(m[SESSION_TURN_FIELD]>0)addTurnBand(m[SESSION_TURN_FIELD],true,m[SESSION_TURN_OPENED_FIELD]||'');
        addMsg('user',m.content+(m.has_image?'  [📷 image attached]':''));
      }
      // B5: a tool-calling assistant row often carries NO text of its own — the
      // tool card below IS its content, so an empty bubble is not rendered
      // (addMsg would draw a "…" placeholder).
      else if(m.role==='assistant'&&m.content)addMsg('assistant',m.content);
      for(const t of (m.tools||[])){
        const pending=(t.id&&openCards[t.id])||null;
        if(pending){
          if(t.result)pending.querySelector('.tbody').textContent+='\n→ '+t.result;
          else pending.querySelector('.tbody').textContent+='\n→ (no output)';
          pending.classList.add('closed');
          delete openCards[t.id];
        }else{
          const card=addToolCard(t.name,t.call||('→ '+(t.result||'(no output)')));
          if(t.result)card.classList.add('closed');
          else if(t.id)openCards[t.id]=card;
        }
      }
    }
  }catch(err){/* first load with an empty session — nothing to replay */}
}
identifyEngine();
replayTranscript();
connectEvents();
refreshState();

// applyStatus renders ONE status document (the same JSON the state frame and
// GET /api/state both carry) into the resident badge. It is the ONLY writer of
// that node, and it REPLACES the whole rendering rather than patching it: the
// document is complete, so a reader never sees a half-updated badge.
//
// WHY THE BADGE IS NOT THE PAGE'S OWN busy FLAG. That flag is a guess about this
// page's request: it is true from the POST until the stream ends, so it cannot
// name a tool, it cannot say "interruptible", and it is silent for a turn the
// SERVER started by itself (a gopi notify delivery). The badge says what the
// SERVER says is happening, which is the only version that stays true when
// nobody in this tab typed anything.
function applyStatus(d){
  if(!statusEl||!d)return;
  const st=d.state||ST_IDLE;
  statusEl.dataset.state=st;
  statusEl.dataset.tool=d.tool||'';
  statusEl.dataset.interruptible=d.interruptible?'true':'false';
  clearNode(statusEl);
  const dot=document.createElement('span');dot.textContent='● ';
  statusEl.appendChild(dot);
  const name=document.createElement('span');
  name.textContent=st===ST_TOOL_RUNNING?(st+'('+(d.tool||'?')+')'):st;
  if(st===ST_TOOL_RUNNING)name.className='stool';
  statusEl.appendChild(name);
  // A LONG TURN IS VISIBLY LONG: the elapsed seconds come from the server's own
  // since, so the number is about the turn, not about when this tab opened.
  if(st!==ST_IDLE&&d.since){
    const secs=Math.max(0,Math.floor(Date.now()/1000-d.since));
    const el=document.createElement('span');el.className='sub';
    el.textContent=' '+secs+'s';statusEl.appendChild(el);
  }
  if(d.interruptible){
    const i=document.createElement('span');i.className='sint';
    i.textContent='⎋ stoppable';statusEl.appendChild(i);
  }
  statusEl.title='server state: '+st+(d.tool?(' ('+d.tool+')'):'')+' · turn '+d.turn+' · GET /api/state';
  // THE WAITING MESSAGES ARE PART OF THE SAME DOCUMENT (cli_web_queue.go). A
  // status that says "tool_running" and stays silent about three messages
  // waiting behind it is the reading this epic exists to end.
  applyQueue(d.pending||[]);
}

// refreshState reads the status ONCE at load, so the badge is right before the
// first frame arrives (a page opened in the middle of a turn must not sit on a
// default "idle" it never measured). It is a single GET, NOT a poll: every
// later change arrives on the standing stream.
function refreshState(){
  fetch(withToken(API_STATE)).then(r=>r.ok?r.json():null).then(applyStatus).catch(()=>{});
}

// connectEvents holds the STANDING stream open (EventSource, GET /api/events)
// and renders the frames of a turn the SERVER started by itself — a session
// inbox delivery (gopi notify --session <id> --message <text>).
//
// WHY THIS IS NOT ANOTHER /api/chat CALL. A delivery is made by ANOTHER
// process; nothing the page does can start that turn, and there is no request
// whose body could carry the answer. So the server opens the turn on its own
// (cli_web_notice.go) and the frames arrive here, already framed exactly like a
// POSTed turn's: notice (the opening line, so the reader can tell WHY the page
// answered by itself), then delta / tool / done.
//
// EVENTSOURCE RECONNECTS BY ITSELF, which is why this is the primitive rather
// than a fetch loop: a dropped connection re-opens on the browser's own timer,
// and there is no stream for the page to lose track of.
function connectEvents(){
  if(typeof EventSource==='undefined')return;   // an ancient engine: the notifier is simply absent
  let es=null,ob=null,oraw='';
  function openStream(){
    es=new EventSource(withToken(API_EVENTS));
    es.addEventListener(EV_NOTICE,ev=>{
      let d={};try{d=JSON.parse(ev.data);}catch(err){d={text:ev.data||''};}
      addMsg('notice',d.text||'(notice)');
      ob=addMsg('assistant','',true);oraw='';
    });
    // A warning is about the turn ALREADY running, so it opens no bubble and
    // touches no accumulator: it is one node at the log's end, and follow()
    // respects a reader who scrolled up (the same rule the deltas obey).
    es.addEventListener(EV_WARNING,ev=>{
      let d={};try{d=JSON.parse(ev.data);}catch(err){d={text:ev.data||''};}
      addMsg('warning',d.text||'(warning)');
    });
    es.addEventListener('delta',ev=>{
      if(!ob)ob=addMsg('assistant','',true);
      try{oraw+=(JSON.parse(ev.data).text||'');}catch(err){return;}
      renderAssistantBody(ob,oraw);follow(false);
    });
    es.addEventListener('tool',ev=>{
      let t={};try{t=JSON.parse(ev.data);}catch(err){return;}
      addToolCard(t.name,t.summary);follow(false);
    });
    es.addEventListener(EV_TURN,ev=>{
      // A SERVER-STARTED turn carries its band too: which turn this is must not
      // depend on who opened it (same rule as the badge).
      let d={};try{d=JSON.parse(ev.data);}catch(err){d={};}
      if(d.turn>0)addTurnBand(d.turn,false,d[SESSION_TURN_OPENED_FIELD]||'');
    });
    es.addEventListener(EV_STATE,ev=>{
      let d={};try{d=JSON.parse(ev.data);}catch(err){return;}
      applyStatus(d);
    });
    es.addEventListener('done',ev=>{
      let d={};try{d=JSON.parse(ev.data);}catch(err){d={};}
      if(ob){
        if(d.error){ob.classList.add('err');if(!oraw)ob.textContent='turn failed: '+d.error;}
        ob=null;oraw='';
      }
      follow(false);
    });
  }
  openStream();
}

// addToolCard renders one collapsible tool-activity card (R2, display only).
// A call opens a card; its matching result appends "→ output" and collapses.
function addToolCard(name,summary){
  const card=document.createElement('div');
  card.className='toolcard';
  const head=document.createElement('div');
  head.className='thead';
  const arrow=document.createElement('span');
  arrow.className='tarrow';arrow.textContent='▾';
  const label=document.createElement('span');
  label.textContent=name;
  head.appendChild(arrow);head.appendChild(label);
  const body=document.createElement('div');
  body.className='tbody';
  body.textContent=summary||'';
  card.appendChild(head);card.appendChild(body);
  head.addEventListener('click',()=>card.classList.toggle('closed'));
  logEl.appendChild(card);
  return card;
}

form.addEventListener('submit',e=>{e.preventDefault();submitGesture(false);});

// submitGesture turns one composer gesture into either a NEW turn (nothing in
// flight) or a QUEUED / STEERING entry (B12 — the turn in flight keeps running).
// steer marks the Alt gesture: the message is typed as STEERING (the run is
// addressed with it and kept alive) instead of QUEUED (a turn of its own).
function submitGesture(steer){
  const raw=input.value.trim();
  if(!raw)return;
  if(busy){
    enqueueMessage(raw,steer);
    input.value='';autogrow();menuClose();
    return;
  }
  input.value='';autogrow();menuClose();
  runTurn(raw,null);
}

// enqueueMessage renders a mid-turn message as a VISIBLE queued entry (B12) and
// records it for delivery. The entry carries its typed placement — dsh's
// QueuedInboxItem.placement: 'queued' (becomes a turn of its own) or 'steering'
// (the run in flight is addressed with it) — so a message typed while a turn
// runs is never silently swallowed. Images staged at that moment ride THIS
// entry (never a later turn), and the entry's own turn is what shows them as
// sent.
function enqueueMessage(message,steer){
  const node=addMsg('queued',message+(images.length?('  [+ '+images.length+' image(s)]'):''));
  node.classList.add('pending');
  const placement=steer?PLACEMENT_STEERING:PLACEMENT_QUEUED;
  const badge=document.createElement('span');
  badge.className='placement';
  badge.textContent=placement;
  node.appendChild(badge);
  const queuedImages=images;images=[];renderStrip();
  const wire=steer?menuSteerPrompt(message):message;
  // ONE object per waiting message: the SAME entry is what the page renders,
  // what drainQueue delivers and what the withdrawal route addresses. The
  // pre-epic version kept the rendered node and the delivered item as two
  // separate literals, which is how a cancelled entry could still run.
  const item={text:wire,node,bubble:node,images:queuedImages,placement,cancelled:false,id:0,position:0};
  pendingMsgs.push(item);
  renderQueueStatus();
  // THE SERVER IS TOLD, so the entry has an id, a POSITION and a withdrawal
  // route instead of being a client-side array the server cannot see. The
  // registration runs in the background: the message is already on screen, and
  // a slow round trip must not delay the composer.
  registerQueued(item);
  follow(false);   // B3: the queue grows while the reader may be scrolled up
}

// registerQueued tells the server one waiting message exists and renders the
// answer onto its entry: the POSITION it will run in and the withdrawal button.
// A failure is drawn ON THE ENTRY — a queue entry that silently lost its
// server-side id would be a message the operator believes is cancellable and is
// not (Rule 15).
async function registerQueued(item){
  try{
    const res=await fetch(withToken(API_QUEUE),{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({message:item.text,placement:item.placement})});
    if(!res.ok){item.error='HTTP '+res.status;renderQueueStatus();return;}
    const d=await res.json();
    item.id=d.id||0;
    renderQueueStatus();
  }catch(err){item.error=err.message;renderQueueStatus();}
}

// renderQueueStatus writes EVERY waiting entry's status line, and their
// positions, from the queue itself. It is the ONE renderer of that text (one
// rule, one place), so a shape change — a message delivered, withdrawn or
// registered — cannot leave one entry numbered against a queue it is no longer
// in.
//
// The WORDS are the point: the pre-epic page separated queued from steering by
// a border style and its own source called that "cosmetic only", so a reader
// had no sentence telling them what would happen to the message they had just
// typed, and no way at all to take it back.
function renderQueueStatus(){
  let pos=0;
  pendingMsgs.forEach(e=>{
    const old=e.node&&e.node.querySelector('.qstatus');
    if(old)old.remove();
    const oldBtn=e.node&&e.node.querySelector('.qcancel');
    if(oldBtn)oldBtn.remove();
    if(e.cancelled){e.position=0;}
    else if(e.placement===PLACEMENT_QUEUED){pos++;e.position=pos;}
    else{e.position=0;}
    if(!e.node)return;
    const line=document.createElement('span');
    line.className='qstatus '+e.placement+(e.cancelled?' done':'');
    if(e.cancelled){line.textContent=QUEUE_CANCELLED_LABEL;}
    else if(e.error){line.textContent='排队登记失败（'+e.error+'）';line.classList.add('done');}
    else if(e.placement===PLACEMENT_STEERING){line.textContent=STEERING_LABEL;}
    else{line.textContent=QUEUED_LABEL+(e.position?('（'+QUEUED_POSITION_LABEL+e.position+QUEUED_POSITION_SUFFIX+'）'):'');}
    e.node.appendChild(line);
    if(!e.cancelled&&!e.error&&e.placement===PLACEMENT_QUEUED&&e.id){
      const btn=document.createElement('button');
      btn.className='qcancel';
      btn.type='button';
      btn.textContent=QUEUE_CANCEL_LABEL;
      btn.title='withdraw this waiting message (POST '+API_QUEUE_CANCEL+')';
      btn.addEventListener('click',ev=>{ev.preventDefault();withdrawQueued(e);});
      e.node.appendChild(btn);
    }
  });
}

// withdrawQueued takes a waiting message back out of the queue. On success the
// entry STAYS in the log — a deleted message is how a reader loses track of
// what they typed — but SAYS it was withdrawn, and its turn never runs.
async function withdrawQueued(e){
  if(!e||!e.id||e.cancelled)return;
  try{
    const res=await fetch(withToken(API_QUEUE_CANCEL),{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:e.id})});
    if(!res.ok){addCommand(QUEUE_CANCEL_LABEL,QUEUE_CANCEL_FAILED+' (HTTP '+res.status+')',true);return;}
    const d=await res.json();
    if(!d.ok){
      // The server refused: that message had already STARTED. Say so, rather
      // than leaving a button that looks like it worked.
      addCommand(QUEUE_CANCEL_LABEL,QUEUE_CANCEL_FAILED,true);return;
    }
    e.cancelled=true;
    renderQueueStatus();
    // The entry stops being delivered AND the server is told once more through
    // the queue view it just answered with — the page renders from the server.
    applyQueue(d.pending||[]);
  }catch(err){addCommand(QUEUE_CANCEL_LABEL,'cancel failed: '+err.message,true);}
}

// applyQueue renders the SERVER's queue view (the pending list carried by
// /api/state, the state frame and the registration/cancel answers) onto the
// badge: how many messages wait and what they are. The page never derives this
// from its own array — the server is authoritative, exactly as it is for the
// transcript (R1) — and an id this page does not know about is REPORTED rather
// than silently dropped.
function applyQueue(pending){
  if(!statusEl)return;
  const old=statusEl.querySelector('.qsum');
  if(old)old.remove();
  if(!pending||!pending.length)return;
  const el=document.createElement('span');
  el.className='qsum';
  const queued=pending.filter(e=>e.state===ST_QUEUED).length;
  const steer=pending.filter(e=>e.state===ST_STEERING).length;
  const parts=[];
  if(queued)parts.push(QUEUED_LABEL+QUEUED_POSITION_LABEL+queued+QUEUED_POSITION_SUFFIX.replace('位','条'));
  if(steer)parts.push(STEERING_LABEL);
  el.textContent=' · '+parts.join(' · ');
  el.title=pending.map(e=>e.state+' #'+e.id+(e.position?(' ('+e.position+')'):'')+': '+e.preview).join('\n');
  statusEl.appendChild(el);
}

// drainQueue delivers what arrived while a turn was running, ONE entry at a
// time and each as a turn of its own (B12). Strictly sequential: no two turns
// ever overlap, so the single session id / JSONL / raw log can never be written
// by two turns at once — the consolidation invariant holds by construction (the
// queue adds no second agent and no second lock).
//
// A WITHDRAWN ENTRY IS SKIPPED HERE — the client-side half of the withdrawal;
// the server half marks the id, so its turn cannot start even when this check
// and the cancel route race.
async function drainQueue(){
  while(pendingMsgs.length){
    const item=pendingMsgs.shift();
    if(item.cancelled)continue;
    await runTurn(item.text,item);
  }
  renderQueueStatus();
}

// runTurn runs ONE turn (B12: fresh is a composer submit, entry is a queued
// entry being delivered). It owns the busy flag, the composer guards and the
// SSE stream; on completion drainQueue delivers the next entry.
async function runTurn(message,entry){
  const fresh=!entry;
  const turnImages=entry?entry.images:images;
  if(fresh){images=[];renderStrip();}
  if(fresh){addMsg('user',message+(turnImages.length?('  [+ '+turnImages.length+' image(s)]'):''));}
  // B12: a turn in flight must NOT lock the composer — the textarea stays
  // editable so a message typed now can be queued (the pre-B12 page disabled it,
  // which is what lost the message). The Send button keeps working too: with a
  // turn running it queues instead of sending.
  busy=true;sendBtn.classList.add('busy');input.classList.add('busy');
  // B12: the running request's controller replaces any earlier one, so "stop"
  // aborts whatever is running NOW (the earlier turns already finished).
  activeCtl=new AbortController();
  if(fresh){lastQueued=null;}

  // DOM ORDER = ARRIVAL ORDER (this page's one ordering invariant). The
  // assistant bubble is NOT appended when the POST is sent: it is created by
  // the first frame that has to draw into it (a delta, or a stream failure)
  // and mounted at the log's END at that moment. So a tool card that arrived
  // BEFORE that frame keeps the rows above it, and the reader sees the call
  // above the answer it produced — the TUI's order. The bubble is created at
  // most once and the SAME node is reused for the whole turn: every branch
  // below calls openBubble() instead of holding a node that already exists.
  //
  // R4: a slash message renders ONLY the local command node — openBubble()
  // returns null for it (the server answers with one command frame, not a
  // model turn), so no bubble is ever opened on that path.
  let bubble=null,caret=null;
  function openBubble(){
    if(bubble||message.startsWith('/'))return bubble;
    bubble=addMsg('assistant','',true);   // B3: the gate, not a forced scroll
    caret=document.createElement('span');
    caret.className='caret';bubble.appendChild(caret);
    return bubble;
  }
  let raw='';   // B8: the turn's raw assistant text (markdown re-rendered per delta)

  try{
    const res=await fetch(withToken(API_CHAT),{
      method:'POST',
      headers:{'Content-Type':'application/json'},
      // queue_id: the id /api/chat/queue gave this message, so the request
      // that finally runs it is the SAME entry the operator saw waiting
      // (a withdrawal of that id therefore reaches this turn, not a copy).
      body:JSON.stringify(Object.assign({message},(entry&&entry.id)?{queue_id:entry.id}:{},turnImages.length?{images:turnImages}:{})),
      signal:activeCtl.signal,
      // R1: the server owns the transcript — no history shipped. R5: images ride in the body.
    });
    if(!res.ok||!res.body){
      const detail=await res.text().catch(()=>'');
      const failNode=entry?entry.node:openBubble();
      if(failNode){
        failNode.classList.add('err');
        failNode.textContent='request failed: HTTP '+(res.status||'no stream')+(detail?(' — '+detail):'');
      }else{
        addCommand(message,'request failed: HTTP '+(res.status||'no stream')+(detail?(' — '+detail):''),true);
      }
      return;
    }
    const reader=res.body.getReader();
    const decoder=new TextDecoder();
    let buf='';

    // Parse the SSE stream into {event,data} frames.
    async function frames(){
      while(true){
        const {done,value}=await reader.read();
        if(done)break;
        buf+=decoder.decode(value,{stream:true});
        let idx;
        while((idx=buf.indexOf('\n\n'))>=0){
          const frame=buf.slice(0,idx);buf=buf.slice(idx+2);
          let ev='message',data='';
          for(const line of frame.split('\n')){
            if(line.startsWith('event:'))ev=line.slice(6).trim();
            else if(line.startsWith('data:'))data+=line.slice(5).trim();
          }
          handle(ev,data);
        }
      }
      handle('__done__','');
    }
    function handle(ev,data){
      if(ev==='delta'){
        if(message.startsWith('/'))return;   // slash turns never carry deltas
        // THE ordering decision: the bubble comes into existence HERE, at the
        // first frame that draws into it — so it lands at the log's END, after
        // any tool card the server already streamed for this turn.
        openBubble();
        if(caret&&caret.parentNode)caret.remove();
        // B8: the bubble is re-rendered from the accumulated RAW text, so
        // markdown appears as it streams (a fence opens on its own frame).
        raw+=JSON.parse(data).text||'';
        renderAssistantBody(bubble,raw);
        follow(false);   // B3: never yank a reader who scrolled up
      }else if(ev==='command'){
        // R4: locally-executed slash command result — its own node. B1: a
        // reset command resolves to the server's reset notice, and the page
        // then wipes the transcript it was displaying (the SERVER already
        // dropped its copy under s.mu).
        const c=JSON.parse(data);
        const text=c.output||'';
        if(text===RESET_NOTICE){
          clearNode(logEl);
          pendingCards=[];
        }
        addCommand(c.command||message,text,!!c.error);
        // THE COMMAND'S OWN PRODUCTS (the /text2img line). A slash command that
        // rendered pictures hands them back ON THE FRAME; they are staged
        // exactly like a 📎 upload or a 📷 capture — same images[], same
        // renderStrip(), same next-turn /api/chat body — so the model finally
        // SEES what the command drew instead of reading a path it cannot open.
        // The server only sends this member when it has pictures to attach, so
        // every other command's frame is unchanged.
        if(Array.isArray(c[CMD_IMAGES_FIELD])){
          attachCommandImages(c[CMD_IMAGES_FIELD],c.command||message,!!c.error);
        }
      }else if(ev===EV_TURN){
        // THE TURN BANNER (cli_web_turn.go). The server sends the SESSION's own
        // ordinal for the turn that is starting, before the model runs — so the
        // band is mounted at the turn's beginning, above the answer it labels,
        // and it never has to be guessed from a local counter (which a refresh
        // would reset to 1 while the record says 21).
        let n=0,at='';try{const td=JSON.parse(data);n=td.turn||0;at=td[SESSION_TURN_OPENED_FIELD]||'';}catch(err){n=0;}
        if(n>0)addTurnBand(n,false,at);
      }else if(ev==='queued'){
        // B2: this turn is waiting on the in-flight one. The frame arrives
        // BEFORE the server takes its turn lock, so the wait is visible
        // instead of a byte-less hang.
        const q=JSON.parse(data);
        if(message.startsWith('/'))return;
        // B2: the wait notice is mounted WITHOUT opening the answer bubble —
        // the answer has not started, so nothing may claim its row yet. The
        // server's done frame (which comes before the answer's deltas) is
        // what removes the notice, so it can never sit between the turn's cards
        // and its answer.
        if(!lastQueued){lastQueued=addMsg('queued',q.notice||'waiting…');}
        else{lastQueued.textContent=q.notice||lastQueued.textContent;}
        // The frame carries the WHOLE queue (cli_web_queue.go), so the badge
        // renders the server's view even for an entry this tab did not type.
        applyQueue(q.pending||[]);
        follow(false);
      }else if(ev==='tool'){
        // R2: tool activity renders as a collapsible card (display only). It is
        // mounted at the log's END at ARRIVAL — that is the whole point of the
        // deferred bubble: the card keeps the row its frame earned.
        if(message.startsWith('/'))return;
        if(caret&&caret.parentNode)caret.remove();
        const t=JSON.parse(data);
        if(t.kind==='result'&&pendingCards.length){
          const card=pendingCards.shift();
          card.querySelector('.tbody').textContent+='\n→ '+(t.summary||'(no output)');
          card.classList.add('closed');
        }else{
          pendingCards.push(addToolCard(t.name,t.summary));
        }
        follow(false);   // B3
      }else if(ev===EV_CANCELLED){
        // The message was withdrawn while it waited: this stream ends here
        // without a model turn, and the entry says so rather than running.
        const c=JSON.parse(data);
        if(entry){entry.cancelled=true;entry.notice=c.notice||QUEUE_CANCELLED_LABEL;}
        renderQueueStatus();
      }else if(ev==='done'){
        // The server owns the transcript (R1) — nothing to track locally.
        // Slash turns don't emit done (the command frame is terminal).
        // B2: the turn is running now, so the queued notice has served its
        // purpose (the deltas that follow are the answer).
        if(lastQueued){lastQueued.remove();lastQueued=null;}
      }else if(ev==='error'){
        if(bubble)bubble.classList.add('err');
      }
    }
    await frames();
  }catch(err){
    const aborted=!!(err&&err.name==='AbortError');
    // A failure that is NOT a stop has to draw somewhere: it opens the bubble
    // here — still at the log's end, after any card that already arrived — so
    // the failure keeps the row its frame earned (a stop that streamed nothing
    // mounts nothing, because there is nothing left to show).
    if(!aborted)openBubble();
    if(bubble){
      bubble.classList.remove('caret');
      if(aborted){
        // A stopped turn keeps what it streamed (the server's transcript holds
        // it too) and is marked, rather than pretending it never happened.
        if(raw){bubble.classList.add('err');bubble.textContent=raw+'\n[stopped]';}
        else{clearNode(bubble);bubble.remove();}
      }else{
        bubble.classList.add('err');
        bubble.textContent='network error: '+err.message;
      }
    }else if(!aborted){
      addCommand(message,'network error: '+err.message,true);
    }
  }finally{
    busy=false;sendBtn.classList.remove('busy');input.classList.remove('busy');
    if(entry){
      // B12: the queued entry is now a delivered turn — the delivered marker
      // replaces the pending look (the badge stays, so a reader can still see
      // how it was placed).
      entry.node.classList.remove('pending');
      entry.node.classList.remove('queued');
      entry.node.classList.add('user');
    }
    // Hand the composer back only when nothing else is waiting: a queue
    // continues below, and every delivered entry gets its own turn in order.
    drainQueue();
    if(!busy)input.focus();
  }
}

// stopTurn aborts the running turn (Escape). The single agent is shared, so
// this abandons the HTTP stream — the server's turn keeps its own discipline
// (its s.mu tenure and the single session_end are untouched by a client-side
// stop); the queue, if any, continues with the next entry.
function stopTurn(){
  if(activeCtl){try{activeCtl.abort();}catch(err){/* an aborted controller stays aborted — nothing to do */}}
}

input.addEventListener('keydown',e=>{
  // IME-safe Enter: never submit mid-composition. An IME commits its candidate
  // with Enter, and that keydown arrives with isComposing true (or keyCode 229
  // on engines that predate the flag) — submitting there would send a
  // half-composed line and lose the candidate.
  if(e.isComposing||e.keyCode===IME_PROCESSING_KEYCODE)return;
  // B9: while the input-trigger menu is open it is ARBITRATED before the
  // composer's own keys — the arrow keys walk the candidates, Enter/Tab accept
  // the highlighted one (a completion, never a send), Escape closes the menu.
  // Enter never submits from a menu: the only way to send is to accept first.
  if(menu.open){
    if(e.key===KEY_ARROW_DOWN){e.preventDefault();menuMove(MENU_MOVE_NEXT);return;}
    if(e.key===KEY_ARROW_UP){e.preventDefault();menuMove(MENU_MOVE_PREV);return;}
    if(e.key===KEY_ENTER||e.key===KEY_TAB){e.preventDefault();menuComplete();return;}
    if(e.key===KEY_ESCAPE){e.preventDefault();menuClose();return;}
  }
  // ESCAPE IS ONE DECISION WITH FOUR MEANINGS, and micEscAct is where they are
  // ordered (menu → recording → turn → settled voice sentence — the voice rung is
  // the RESIDUAL one, so it answers only after the recording and the turn have
  // both declined). This listener used to carry two of the rungs itself, which is
  // how the SETTLE WINDOW — the recording already stopped, the engine's sentence
  // already in the composer, no turn running — fell through every branch and left
  // Escape doing nothing at all. The rungs are disjoint (see micEscAct), so the
  // order is the whole arbitration and neither listener re-derives it.
  if(e.key===KEY_ESCAPE){
    const act=micEscAct();
    if(act!==ESC_NONE){
      e.preventDefault();
      micEscRun(act,e.shiftKey);
      return;
    }
  }
  // CTRL+U IS THE WHOLE-COMPOSER CLEAR, and it is UNANNOUNCED BY DESIGN: no
  // placeholder text, no button, no notice. A destructive keystroke only a
  // practised user reaches for is safe precisely because nobody trips into it —
  // "I know it exists but not how to recover" is the harm the silence avoids. It
  // is arbitrated before the composer's other keys and works whether or not a
  // recording is open (the clear is about the TEXT, not about the microphone).
  if((e.ctrlKey||e.metaKey)&&!e.altKey&&(e.key===KEY_CLEAR_INPUT||e.key===KEY_CLEAR_INPUT_UPPER)){
    e.preventDefault();
    micDraftClear();
    // THE WHOLE COMPOSER IS GONE, so the settled sentence is no longer in it: a
    // later Shift+Escape must not claim to discard text that Ctrl+U already took.
    micVoiceRun=null;
    input.value='';
    autogrow();
    menuClose();
    return;
  }
  // B12: Alt+Enter is the steering gesture — the message addresses the turn in
  // flight (posted with the shared sentinel the server strips) and the run
  // keeps going. Decided BEFORE the plain-Enter branch, so the modifier is
  // never lost to the send path.
  if(e.key===KEY_ENTER&&e.altKey){e.preventDefault();submitGesture(true);return;}
  // Repeat guard: a held-down Enter must not queue one turn per auto-repeat
  // tick — only the first keydown submits.
  if(e.repeat)return;
  if(e.key===KEY_ENTER&&!e.shiftKey){e.preventDefault();form.requestSubmit();}
});
input.addEventListener('input',autogrow);
// B9: the menu is re-decided whenever the composer's text or caret can change
// the answer (typing, caret moves, clicks) — the detect core stays pure, only
// this wiring touches the DOM. A keystroke that CHANGES the query starts its
// candidate list at the top (resetSel); the click/keyup passes re-decide the
// SAME query and keep the highlight.
input.addEventListener('input',()=>menuRefresh(true));
input.addEventListener('click',()=>menuRefresh());
// The keyup pass exists for the CARET MOVES the keyboard makes (Home/End/
// shift-arrow move the caret without an input event), but a pure-navigation
// key owns its keydown entirely — re-running the detect core on its keyup would
// rebuild the menu a few milliseconds after the highlight moved and undo it
// (and spend a /api/menu round-trip per arrow press).
input.addEventListener('keyup',e=>{if(!MENU_NAV_KEYS.has(e.key))menuRefresh();});
// ESCAPE WHILE A RECORDING RUNS IS TAKEN WHATEVER HAS THE FOCUS. The composer's
// own listener above handles the ordinary case (the caret is there), but a
// recording outlives a click on the page — the operator may be reading the log —
// and a key that works only while one element is focused is a key that appears
// broken. This listener runs on the BUBBLE, so it sees only keys no handler
// below claimed: defaultPrevented is the hand-off, and the composer sets it on
// every key it arbitrates (the menu, a turn, the recording itself).
//
// IT IS A SEPARATE ACT FROM ⏹: Escape STOPS AND KEEPS (micStopKeep) — the same act
// as the button except that the caret is left at the end of the composer for the
// proof-reading pass. Shift+Escape is the DISCARD. One key cannot mean both, and a
// discarded recording must not reach the composer.
//
// IT ASKS THE SAME ARBITER AS THE COMPOSER'S LISTENER (micEscAct) instead of
// re-deriving the answer. The old copy asked only micRecordingOpen, so the settle
// window — recording closed, sentence in the composer — was answered by neither
// listener; and any future rung added to one copy would have been invisible to the
// other, which is precisely the class of defect a second copy of a decision makes
// unavoidable.
document.addEventListener('keydown',e=>{
  if(e.defaultPrevented||e.isComposing||e.keyCode===IME_PROCESSING_KEYCODE)return;
  if(e.key!==KEY_ESCAPE)return;
  const act=micEscAct();
  if(act===ESC_NONE)return;
  e.preventDefault();
  micEscRun(act,e.shiftKey);
});
// autogrow: the composer grows with its content up to INPUT_MAX_ROWS lines,
// then scrolls — the cap is applied from the same two constants the growth
// maths uses, so the CSS limit cannot drift from the JS one.
input.style.maxHeight=(INPUT_LINE_HEIGHT*INPUT_MAX_ROWS)+'px';
function autogrow(){
  input.style.height='auto';
  const max=INPUT_LINE_HEIGHT*INPUT_MAX_ROWS;
  input.style.height=Math.min(input.scrollHeight,max)+'px';
  input.style.overflowY=input.scrollHeight>max?'auto':'hidden';
}
autogrow();
// B12: sendBtn.click is a submit GESTURE, never a bypass — with a turn in
// flight the click goes to the queue (the pre-B12 page's Send button was
// disabled while busy, so a typed message could only be lost).
sendBtn.addEventListener('click',e=>{e.preventDefault();submitGesture(e.altKey);});
input.focus();
</script>
</body>
</html>
`

// webCLIOptions is the parsed `gopi web` command line. It is a struct rather
// than a return tuple because the positional SESSION ADDRESS joined it
// (webResumeSeedFor's operand): four booleans plus an address plus a slice is
// exactly the shape where a caller swaps two same-typed results, and a swapped
// pair here means "wrote to the wrong session".
type webCLIOptions struct {
	// Addr is the bind address (flag > env-derived default).
	Addr string
	// Tools is the tool switch (`--tools` on this run, or restored from the
	// continued session's record).
	Tools bool
	// Thinking is the live-reasoning switch (`--thinking-live`, or restored).
	Thinking bool
	// Help is `-h` / `--help`.
	Help bool
	// Positional holds the bare words after the flags. This parser is SHARED
	// with the single-turn faces (`gopi cli`, `gopi cli --once`), whose own
	// positionals are not session addresses, so the words are collected here and
	// judged by the caller that owns the address rule.
	Positional []string
}
