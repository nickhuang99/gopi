// Package cliverbs is the shared home for the TOP-LEVEL gopi CLI verb words
// that MORE THAN ONE package must spell.
//
// WHY IT EXISTS (Rule 14: one definition, in a leaf package every consumer can
// import). cmd/agent/cli_verbs.go is the primary SSOT for the top-level verb
// vocabulary, but it is `package main` — unimportable. The moment a second
// package (internal/agent's system prompt) must NAME a verb, the only
// Rule-1-compliant choices are (a) import a leaf constant package or (b) type
// the literal — and (b) is exactly what Rule 13 bans and what the
// cmd/agent-side aliases below make impossible.
//
// THE SHAPE IS THE PRECEDENT: cmd/agent's CliVerbMemory is already an alias of
// tools.CliVerbMemoryGroup for the same reason (the backup feature's ssh leg
// spells the group verb on the wire). This package is that pattern widened to
// the verbs an agent-facing surface must reference by name.
//
// IT IS DELIBERATELY SMALL AND LEAF: constants only, no imports, no logic —
// so every consumer (cmd/agent included) can import it without a cycle. A verb
// NOT referenced outside cmd/agent stays declared in cmd/agent/cli_verbs.go
// and does not belong here.
//
// NAMESPACE: these are USER-facing CLI words, not tool-wire names. Where the
// two coincide the cmd/agent side keeps its own constant (a tool rename must
// never rename the CLI word — see cli_verbs.go's namespace note).
package cliverbs

// The SENSE verbs — the gopi CLI surfaces the agent reaches through the
// existing `bash` tool to observe itself and its machine (Rule 3: no new tool).
// They are named here because the agent's system prompt (internal/agent) must
// spell them and cannot import package main.
const (
	// UIState is the `gopi ui_state` verb — READ a live `gopi ui` screen
	// state as an interpreted cell grid, in ONE call, with no pixels and no
	// vision model. Underscored on purpose: the TOOL-shaped spelling of a
	// surface reached through bash (see cmd/agent/cli_verbs.go's
	// CliVerbUIState and devdoc/cli-naming-convention.md).
	UIState = "ui_state"
	// Log is the `gopi session log` verb — the JSONL session-log reader (what this
	// agent's own past sessions actually did).
	Log = "log"
	// Raw is the `gopi session raw` verb — the raw HTTP log reader (the wire traffic
	// behind a session's rounds).
	Raw = "raw"
	// Stats is the `gopi session stats` verb — session statistics (rounds, tokens,
	// cost, elapsed) for one session or the whole profile.
	Stats = "stats"
	// Show is the `gopi session show` verb — the RECORD-backed view of one
	// session (its row plus the run record: turns, recorded token columns, pid,
	// liveness). Named here because `gopi session log` must POINT at it rather
	// than render the session's cost a second time (the log carries records, the
	// show page carries the recorded totals — see internal/gopilog/cost.go).
	Show = "show"
	// Task is the `gopi task` GROUP verb — the task ledger (a row's state,
	// `gopi task show <id>`; `-h` lists the family).
	Task = "task"
	// THE `merge` VERB IS GONE FROM THIS PACKAGE (epic merge-removal, item 2).
	// It lived here because internal/config's top-level help named it as one of
	// the surfaces whose run is a task; that sentence no longer names it,
	// because the verb no longer dispatches. A constant whose every reader has
	// been removed is the mirror-literal defect in the other direction (Rule
	// 14: one definition, and at least one reader), so the constant goes with
	// the surface rather than sitting here waiting for a reader that will never
	// come back. The merge IMPLEMENTATION is untouched — merge_run.go and the
	// merge internals still compile and still RUN, dispatched through the
	// family spelling `gopi task merge`; what left is the top-level word.
	// Capture is the `gopi tool capture` verb — capture the LIVE X11 desktop
	// (whole screen, the current window, or a rectangle): the agent's eye.
	Capture = "capture"
	// ScreenCapture is the `gopi tool screen_capture` verb — the CLI MIRROR of the
	// screen_capture TOOL (the same string, one dispatch): the whole-screen
	// spelling of Capture. It is named here because the system prompt must
	// teach the model the word that reaches, through bash, the very engine its
	// screen_capture tool call uses.
	ScreenCapture = "screen_capture"
	// Act is the `gopi tool act` verb — synthetic pointer/keyboard input on the
	// live X11 desktop: the agent's hand (the capture→act→verify loop).
	Act = "act"
	// Shot is the `gopi tool shot` verb — render a URL (or a chat surface) to
	// a PNG headlessly. Named here for the same reason as Capture: a test and
	// the tool page both spell the word, and cmd/agent's CliVerbShot aliases
	// this constant rather than the two files each holding the literal
	// (task 01a10e03…, found by the retired-driver gate — the one test that
	// drove `shot` as a top-level word had no constant to reach for).
	Shot = "shot"
	// Sandbox is the `gopi tool sandbox` verb — create/show the ONE configured
	// private profile shape (settings.json 0444 copy + its own ledger +
	// private sessions/raw/logs), never a hand-invented directory.
	Sandbox = "sandbox"
	// ScratchGC is the `gopi tool scratch-gc` verb — the ONE-SHOT sweep of aged,
	// unowned child processes and temp trees. Named here because a test in a
	// package that cannot import package main must drive the FAMILY address
	// (`gopi tool scratch-gc`), and the flat `scratch-gc` the retirement refuses
	// has to come from an owner rather than a bare literal in the test
	// (Rules 1/13/14; found by the retired-driver gate's helper arm, which
	// measured two internal/shot tests driving the refused flat word through
	// testutil.RunGopi and going red on master).
	ScratchGC = "scratch-gc"
	// LedgerWindow is the `gopi tool ledger-window` verb — two wall-clock bounds
	// in, their epoch seconds out (internal/ledgerwindow). Named here because
	// internal/config's positional-verb guard must refuse its UNDERSCORE twin
	// (`ledger_window`) in a leftover positional, and that twin is DERIVED from
	// this word rather than typed a second time (Rules 1/13/14).
	LedgerWindow = "ledger-window"
	// Text2imgVerify is the `gopi tool text2img-verify` verb — render the `知`
	// sample at N px for a pixel-level (IoU) check. Named here for the same
	// reason as LedgerWindow: its underscore twin `text2img_verify` was in NO
	// vocabulary, so it fell through to the bare-prompt path (measured, epic
	// 01a1acc8: `gopi text2img_verify` reaches the agent-creation path instead
	// of a refusal).
	Text2imgVerify = "text2img-verify"
	// Chat is the `gopi chat` verb — the conversational console this agent
	// RUNS IN when the operator talks to it, and therefore the ONE surface
	// whose screen is the operator's own view of the answer. It is named here
	// because the self-knowledge block taught only `gopi ui` (the BubbleTea
	// TUI) while `gopi chat` writes its own dump (`uiclient.ChatDumpFileName`),
	// read back with the same UIState verb plus `--target chat` — so an agent
	// told to look at "a live gopi ui screen" never thought to look at the
	// screen it was actually painting.
	Chat = "chat"
	// SyncRemoteBinaryName is the BINARY NAME gopi invokes on a PEER's own
	// command line (`ssh <alias> gopi …`). It is a constant of the WIRE rather
	// than of this machine — the peer runs whatever `gopi` is on ITS PATH — and
	// it lives here because two layers must spell the same argv: internal/tools
	// composes the remote command (memory_sync_task.go's syncRemoteGopi, which
	// now references this), and a test asserting the bytes the peer's login shell
	// will re-parse must not type a second literal (Rules 1b/13/14).
	SyncRemoteBinaryName = "gopi"
	// Notify is the `gopi notify` verb — named here because a SECOND layer must
	// spell it: the cross-peer DELIVER hop (internal/tools/notify_peer.go) puts
	// `gopi notify --notify-remote` on a peer's ssh command line, and that argv
	// must name the verb the way cmd/agent DISPATCHES it (Rules 1b/13/14 — the
	// wire spelling and the dispatch spelling are one decision). Rule 14's own
	// test: a verb not referenced outside cmd/agent stays in cli_verbs.go, and
	// this one now is.
	Notify = "notify"
	// Version is the `gopi version` verb — the build-metadata reader (also
	// `--version` / `-v`). It is named here because a SECOND package must spell
	// it: internal/config's positional-verb guard prints the corrected line for
	// a word refused in a leftover positional, and for a SYNONYM
	// (`gopi --timeout 3 buildinfo`) that line must name the command the
	// operator meant — printing the refused synonym back would send them into
	// the argv[0] refusal, a dead end (Rule 15). The guard cannot import
	// package main, which is the Rule-14 reason this leaf package exists.
	Version = "version"
	// UI is the `gopi ui` verb — the BubbleTea TUI surface UIState read by
	// default. RETIRED as a command line by the verb-retirement epic
	// (2026-10-05, no replacement); the constant survives because the retained
	// --target flag value `--target ui` is the same word, and because a
	// retired verb is still a thing a reader may name. It is a constant for the same reason Chat is: the prompt names
	// BOTH surfaces now, and a bare "ui" in the section would be a Rule 1b
	// literal beside a spelled verb.
	UI = "ui"
	// CodeSearch is the `gopi tool code-search` leaf — the HYPHENATED CLI name
	// of the code_search TOOL (gopi's own verbs are dash-cased; the tool's wire
	// name keeps its underscore). Named here because internal/config's
	// positional guard prints the corrected line for the mis-spelling
	// `code_search`, and that line is this word under the tool group.
	CodeSearch = "code-search"
	// Session is the `gopi session` GROUP verb — the family that holds the
	// session readers (`gopi session log|raw|stats|list|show|resume|
	// checkpoint`). Named here because the agent's system prompt must spell the
	// LIVE address of its own session surface: it taught the bare words
	// `log`/`raw`/`stats` until the verb-retirement epic (2026-10-05) refused
	// them, at which point the prompt would have been routing the model into a
	// refusal (Rule 11 — the prompt is the only channel that teaches a tool).
	Session = "session"
	// Screen is the `gopi screen` GROUP verb — the readers of a RECORDED SCREEN
	// (`gopi screen replay <id>`). Named here for Rule 14's reason: it is a
	// top-level word that must exist in ONE place, and the positional guard that
	// refuses "this looks like a command but is not one" reads its own list.
	Screen = "screen"
	// ScreenReplayVerb is the `gopi screen-replay <SESSION-ID|FILE>` TOP-LEVEL
	// verb — the SAME reader as `gopi screen replay`, addressed without the
	// family hop. Two doors, one implementation (Rule 14): the operand
	// resolver, the writer, the three ending sentences and the frame
	// comparison are all the sub-verb's own.
	//
	// The word is NOT `replay`: that top-level spelling was abolished
	// (devdoc/replay-abolished-verb-single-route.md) and is pinned as banned by
	// cmd/agent/session_replay_abolition_test.go on four surfaces. The dash
	// keeps the noun that names the artifact and the verb that says what is
	// done with it, and keeps the abolished spelling from coming back as a
	// prefix of a live command.
	ScreenReplayVerb = "screen-replay"
	// Tool is the `gopi tool` GROUP verb — the ONE door every local tool is
	// reached through since the verb-retirement epic (2026-10-05). It is named
	// here because internal/config's positional guard prints the corrected line
	// for a tool word refused in a leftover positional, and that line must spell
	// the group exactly as cmd/agent's dispatcher does — cmd/agent's CliVerbTool
	// aliases this constant (the Rule-14 reason this package exists).
	Tool = "tool"
	// Web is the `gopi web` group verb (the browser chat UI plus the two web
	// tool leaves). Named here for the same reason as Tool: the guard's
	// corrected line for a retired web-tool spelling is `gopi tool web search`,
	// which contains this word.
	Web = "web"
	// WebSearch / WebFetch are the `search` / `fetch` leaves of the web group
	// (`gopi tool web search "<query>"`). Named here because the guard prints
	// that whole line as the home of the dash twins `web-search` / `web-fetch`.
	WebSearch = "search"
	WebFetch  = "fetch"

	// UsageHint is the `gopi <verb> -h` invocation shape the self-knowledge
	// block opens with — the ONE line telling the model any verb's usage page
	// is one call away. It is factored out because the block also embeds the
	// literal verb name, so the two must not be able to drift.
	UsageHint = "`gopi <verb> -h` prints usage"
)

// THE RESUME CROSS-GUIDANCE (Rule 14: ONE definition of a value two packages
// must spell).
//
// WHY THESE CONSTANTS EXIST. TWO live faces resume work, and they differ in the
// one way that matters to the operator who has to pick:
//
//	`gopi session resume <task-id|raw-log-path|session-id>` — the SESSION layer:
//	    starts a run and does NOT close the row out. A successful resume here
//	    leaves the row's worktree registered for good (measured 2026-10-13).
//	`gopi worker --resume <task-id>` (+ `--branch B` / `--worktree P`) — the
//	    WORKER layer: takes over the row's EXISTING ground and runs the standard
//	    close-out chain, which RELEASES the worktree.
//
// Choosing wrong leaks a worktree and nothing says so (Rule 15), so each face
// names the other. THREE surfaces render the choice — the session resume page,
// the worker page's resume paragraph, and `gopi task show`'s advice beside the
// row's ground — and the COMMANDS they name are defined ONCE, here, so the
// three cannot drift (and cliverbs' own test holds the addresses to the verb
// constants they are built from).
//
// The sentences themselves stay on their own pages: a shared blob would carry
// one page's indentation onto another's. What is shared is the SPELLING of
// every command.
const (
	// Worker is the `gopi worker` verb — the task worker loop. It is named
	// here because internal/tools renders the worker-layer resume address into
	// `gopi task show`'s output, and cmd/agent's CliVerbWorker must alias it
	// (this package's own test asserts the two spell the same word).
	Worker = "worker"
	// ResumeFlag is the `--resume` flag of the WORKER face. (The chat surface
	// has a flag of the same spelling in cmd/agent and keeps its own constant:
	// a chat resumes a conversation, never a task row.)
	ResumeFlag = "--resume"
	// Program is the gopi COMMAND WORD itself — the first token of every
	// address this package renders. cmd/agent's gopiProgramWord (the word it
	// splices into a remote command line) aliases THIS constant, so a rendered
	// address and a dispatched word cannot drift (Rules 13/14).
	Program = "gopi"
	// ResumeLeaf is the `resume` leaf of the session family.
	ResumeLeaf = "resume"
	// ResumeTaskIDPlaceholder / ResumeGroundPlaceholder / ResumeSelectorPlaceholder
	// are the OPERAND spellings of the three rendered addresses below. They are
	// placeholders, never words an operator types, and they live here because
	// two packages render them into a command line.
	ResumeTaskIDPlaceholder   = "<task-id>"
	ResumeGroundPlaceholder   = "<branch>"
	ResumeSelectorPlaceholder = "<id>"
)

const (
	// ResumeTaskRowAddress is the WORKER-layer address that resumes a task ROW:
	// it takes the row's ground over and closes it out.
	ResumeTaskRowAddress = Program + " " + Worker + " " + ResumeFlag + " " + ResumeTaskIDPlaceholder
	// ResumeTaskRowGroundAddress is the same face addressed by the GROUND rather
	// than the row — what an operator holds who is standing in the worktree
	// (`git branch --show-current` prints B). The flag is spelled here rather
	// than in internal/tools because this is a RENDERED command line, not a
	// parser token; internal/tools owns the parser-side constant and its test
	// holds the two equal.
	ResumeTaskRowGroundAddress = ResumeTaskRowAddress + " " + "--branch" + " " + ResumeGroundPlaceholder
	// ResumeSessionAddress is the SESSION-layer address: it continues a session
	// (or a row's session) and closes NOTHING out.
	ResumeSessionAddress = Program + " " + Session + " " + ResumeLeaf + " " + ResumeSelectorPlaceholder
)

// ResumeGroundLineLabel is the label `gopi task show` prints beside a row's
// ground: the ONE spelling of that advice (the renderer and the tests address
// it here rather than re-typing it), deliberately just wide enough for the
// digest block's own gutter.
const ResumeGroundLineLabel = "resume this row:"

// THE ADDRESSES. An address is the spelling the CLI DISPATCHES and the refusals
// name (`tool capture`, `session log`): after the verb-retirement epic
// (2026-10-05) the bare words `log`/`raw`/`stats`/`capture` are REFUSED at
// argv[0], so both channels that teach a spelling must teach the address.
//
// WHY CONSTANTS AND NOT JOINER FUNCTIONS (measured 2026-10-06, task
// 01a10e03-9bc6-7427-b7e6-90a47f27f96f). The first landing of this spelled them
// `func ToolAddress(word string) string` / `func SessionAddress(word string)
// string`. A function call cannot appear in a CONSTANT EXPRESSION, so the
// system prompt's own declaration — `selfKnowledgeRules`, whose text must be a
// compile-time string so the rendered prompt is byte-identical on every turn and
// the prompt-cache prefix cannot be disturbed — had to be downgraded from `const`
// to `var` to call them. That is a silent loss of the property
// TestSelfKnowledgeSectionIsStatic exists to protect, and it is avoidable: every
// caller passes ONE FIXED leaf, so the address is a constant expression over the
// group and leaf constants, folded at compile time.
//
// The alternative (keep the functions and call them from the prompt) is not
// available at any price: a const cannot call a function, and hand-copying the
// text to keep the declaration const would put a second literal of the address
// in the tree — the mirror Rule 13/14 exists to forbid.
const (
	// ToolCaptureAddress / ToolScreenCaptureAddress / ToolActAddress /
	// ToolSandboxAddress are the `gopi tool <leaf>` addresses of the desktop
	// surfaces and the sandbox: the eye, the eye's whole-screen mirror, the
	// hand, and the private profile.
	ToolCaptureAddress       = Tool + " " + Capture
	ToolScreenCaptureAddress = Tool + " " + ScreenCapture
	ToolActAddress           = Tool + " " + Act
	ToolSandboxAddress       = Tool + " " + Sandbox
	// SessionLogAddress / SessionRawAddress / SessionStatsAddress are the
	// `gopi session <leaf>` addresses of the session readers: the JSONL log, the
	// raw HTTP wire log, and the session statistics. Each leaf comes from its own
	// constant, so a leaf rename moves its address with it.
	SessionLogAddress   = Session + " " + Log
	SessionRawAddress   = Session + " " + Raw
	SessionStatsAddress = Session + " " + Stats
	// SessionShowAddress is the `gopi session show` address — the RECORD-backed
	// view, and where a reader is sent by `session log`'s cost pointer.
	SessionShowAddress = Session + " " + Show
)

// SessionAddresses is what the system prompt RENDERS for the session family: one
// `session` prefix carrying its three leaves. It is a CONSTANT EXPRESSION over
// the three address constants above — the prompt's own declaration
// (selfKnowledgeRules) is a `const`, so nothing here may be typed as a call
// (measured 2026-10-06: the joiner-function form forced that declaration to a
// `var`, losing the byte-identical-every-turn property).
//
// WHY THE NOUN APPEARS ONCE HERE, not once per leaf: the prompt is bounded by
// `systemPromptDensityBudget` (internal/agent) and was within 17 chars of it when
// this landed — spelling `session log/session raw/session stats` costs +16, the
// single-noun form costs +8. The per-leaf address constants stay available (the
// per-address assertions use them), and every leaf still comes from its own
// constant, so a leaf rename moves this string too.
const SessionAddresses = SessionLogAddress + "/" + SessionRawAddress + "/" + SessionStatsAddress

// Compare is a FLAG, not a verb, and it lives here for the same Rule-14 reason
// the verbs do: the agent's system prompt (internal/agent) must spell it and
// cannot import package main. It is separated from the verb block above because
// the distinction is real — `gopi <verb> -h` lists verbs, and this is only
// reachable under the verb that owns it.
//
// It is the `--compare` flag of UIState: THE ONE-CALL CHECK, answering "is this
// line of my output on the PAINTED grid, and is it in the AUTHORITATIVE
// transcript?" — how a screen that disagrees with what the agent remembers
// saying is caught.
const Compare = "--compare"

// SessionFlag is the `--session` flag of UIState, the OTHER half of the
// one-call check above: `--compare` carries the line to look for, `--session`
// names the AUTHORITATIVE transcript it is checked against. It is a constant
// for the same reason Compare is — the prompt names the invocation — and the
// pair is deliberately declared together, because the check is indivisible:
// naming only `--compare` is what produced the first-call refusal
// (uiStateCompareRefusalNoSession) that sent a caller back for a second call.
const SessionFlag = "--session"

// DumpScreen is the `--dump-screen` flag of the Chat verb, and it is the ARM
// step of the chat self-observation loop: an UNARMED chat writes no dump at
// all, so `gopi ui_state` finds nothing and exits 1. The prompt names the flag
// and the verb together because they are one sentence — arming and reading are
// two calls, not one — and a prompt that named only the reader would send the
// model to a surface that has no file to read (Rule 14: the prompt spells it,
// so it is a constant rather than a typed literal).
//
// THE CHAT DUMP IS ARMED BY DEFAULT (2026-10-01), so this flag is the
// DESTINATION spelling of a capability the run already has — it is how a caller
// picks WHERE the artifact goes, and how a caller that wants the dump armed
// says so in the face of GOPI_CHAT_DUMP=0. The off switch is NoDumpScreen below.
const DumpScreen = "--dump-screen"

// NoDumpScreen is the chat dump's OFF SWITCH: `gopi chat --no-dump-screen`
// declines the artifact that is written by default. It lives here beside
// DumpScreen because they are one capability's two directions (Rule 13: the off
// spelling is compared on, so it is a constant and never a typed literal), and
// because a default needs a documented way out as much as a flag needs a
// documented default.
const NoDumpScreen = "--no-dump-screen"

// ScreenStream is the `--screen-stream` flag of the Chat verb: the renderer's
// RAW bytes (every paint write, verbatim, unthrottled) recorded to
// `<session-id>.screen.stream` beside the dump. Where DumpScreen names the
// INTERPRETATION of a screen, this names the bytes it was interpreted from — the
// one artifact that can answer "was the wrong cell the renderer or the reader?".
//
// THE NAME IS `screen`, NOT `sgr` (operator ruling, 2026-11-08, the same one the
// on-disk header already follows in internal/uiclient): the bytes recorded are a
// SCREEN PAINT sequence — SGR is one escape class inside it, not the thing being
// recorded — and the mechanism's question ("did the screen come out
// byte-identical?") is a screen question. The pre-rename spelling lives on as
// SgrStream below: still PARSED, and it says so on stderr through
// DeprecatedFlagNoticeFormat.
//
// ARMED BY DEFAULT (operator ruling, 2026-10-31: 数据的不可再生性高于代码完整性
// — a run that did not record cannot be re-recorded), so this flag is the
// DESTINATION spelling of a capability the run already has, and the off switch is
// NoScreenStream below.
const ScreenStream = "--screen-stream"

// NoScreenStream is the raw stream's OFF SWITCH: `gopi chat --no-screen-stream`
// declines the artifact that is recorded by default. It lives here beside
// ScreenStream for the reason NoDumpScreen lives beside DumpScreen: they are ONE
// capability's two directions (Rule 13 — the off spelling is compared on, so it
// is a constant and never a typed literal), and a default needs a documented way
// out as much as a flag needs a documented default. It takes NO value
// (`--no-screen-stream=x` is refused by name, the shape NoDumpScreen keeps).
const NoScreenStream = "--no-screen-stream"

// ScreenReplay is the `--screen-replay` flag of the Chat verb: PLAY BACK a
// recorded raw byte stream — the frames ScreenStream wrote — to stdout verbatim.
// It is the one evidence channel that does not depend on the render algorithm
// (`--replay` re-RUNS a session, so its output is this build's reading of it),
// which is why the flag has to be spellable from outside cmd/agent: the replay
// reports and the docs both name it. Its operand is a session id or a stream
// path (both file suffixes are accepted — uiclient.ScreenStreamSuffixes).
const ScreenReplay = "--screen-replay"

// The PRE-RENAME spellings, kept as DEPRECATED ALIASES (operator ruling,
// 2026-11-08). They are not dead words: a shell history, a script, a recorded
// resume line and nine months of operator muscle memory all carry them, so they
// still PARSE — a rename that refused them would break the command line of the
// person it is meant to serve. What they must not do is pass SILENTLY: each one
// prints DeprecatedFlagNoticeFormat, so a caller learns the current spelling from
// the run itself rather than from a changelog nobody reads (Rule 15 — a
// tolerated-but-replaced spelling is never a silent one).
//
// They are NOT the same string as the Screen* constants, and that is the whole
// point: a deprecated alias is a DIFFERENT word that the parser recognizes and
// translates, so it can name itself in the deprecation line. What is shared is
// the DEFINITION — each spelling is written exactly once, here, and every
// consumer imports it (Rule 13/14); a `const SgrStream = ScreenStream` would make
// the notice read "`--screen-stream` is deprecated: use `--screen-stream`", which
// is the one thing an alias must never say (measured on the first build of this
// rename). When the deprecation window closes, these three lines are the whole
// deletion.
const (
	SgrStream   = "--sgr-stream"
	NoSgrStream = "--no-sgr-stream"
	SgrReplay   = "--sgr-replay"
)

// DeprecatedFlagNoticeFormat is the ONE sentence a pre-rename flag prints when it
// still works: the old spelling, the current one. It is a constant because the
// parser, the tests and the docs must not be able to disagree about what a
// caller is told (Rule 13), and because the substring `deprecated: use <flag>` is
// what a script greps to prove the alias path was taken.
const DeprecatedFlagNoticeFormat = "gopi: %s is deprecated: use %s (still honoured; the old spelling is an alias)\n"

// ChatDumpEnv is the environment channel of the same capability: a falsey value
// turns the dump off, `1` asks for it at the resolver's chosen path, and any
// other value IS the path. Named here (not only in cmd/agent, where it is read)
// because internal/agent RENDERS it — the chat-scoped tool's refusal has to name
// the switch that turned the observation off, and a refusal that names a knob
// nothing reads is worse than no refusal (Rule 13/14: one definition, every
// consumer imports it).
const ChatDumpEnv = "GOPI_CHAT_DUMP"

// TargetFlag is the `--target` flag of the UIState verb: WHICH self-observing
// surface to read. Its values are the surface constants above — `UIState` +
// `TargetFlag` + `Chat` is the read-back of the dump `Chat` + `DumpScreen`
// produced, and `TargetFlag` + `UI` is the BubbleTea TUI the verb defaults to.
// Named here for the same reason the verbs are: internal/agent renders the
// invocation into the system prompt and cannot import package main, where the
// dispatch-side spelling (cmd/agent's uiStateFlagTarget) lives.
const TargetFlag = "--target"

// PolicyFlag is the `--policy` flag of the Chat verb — the operator's STARTUP
// POSTURE for a chat run (`yolo` runs everything, `on-request`/`prompt` asks).
// It lives here for a reason the other flag constants do not have: TWO layers
// must spell it, and one of them cannot reach the other. cmd/agent parses it
// (its `chatPolicyFlag` is an alias of internal/agent's `ChatPolicyFlag`, which
// is an alias of this constant — the chain stays because the prompt-facing name
// is the agent's), and internal/tools NAMES it in the text a `prompt` decision
// returns to an UNATTENDED run: the posture is decided BEFORE the run starts,
// so re-running with this flag is the only authorisation that run can tell its
// reader about (internal/tools/chat_operator_presence.go). internal/tools
// cannot import internal/agent (that would be a cycle), so the spelling has to
// sit in the leaf both already import (Rule 14).
const PolicyFlag = "--policy"

// FileFlag is the `--file` input selector of the `gopi session raw` verb: read ONE raw
// log FILE directly instead of resolving a session by id — the invocation the
// chat prompt pairs with the path of the run's OWN wire log, so an agent asked
// "what did you actually send?" can print it instead of guessing.
//
// TWO SPELLINGS, ONE DEFINITION (Rule 13/14), and the reason is the parser's
// shape rather than taste: cmd/agent's raw-verb parser matches BARE words
// (`rawFlagMember` compares against un-prefixed names) and adds the `--` at the
// boundary, so the dispatch side must hold the word and the prompt must hold the
// flag. RawFileWord is the ONE definition; FileFlag is DERIVED from it, so the
// two spellings cannot drift — the same reason Compare/SessionFlag live here at
// all (cmd/agent is package main and the prompt cannot import it).
const RawFileWord = "file"

// FileFlag is the `--file` spelling the chat prompt renders, derived from
// RawFileWord so no second literal of the word exists anywhere.
const FileFlag = "--" + RawFileWord

// TaskTagsWord / TaskTagsFlag are the `--tags` input selector of `gopi task
// submit` — the CLASSIFIER a filed row is born with, and the one the bug-flow
// closure reads back off the row.
//
// TWO SPELLINGS, ONE DEFINITION (Rule 13/14), the exact shape RawFileWord /
// FileFlag has and for the same reason: the tasks parser matches BARE words
// (cmd/agent's flag table and `fs.get` take `tags`, and prepend the `--` at the
// boundary — see cli_tasks.go's taskTagsFlagName, an ALIAS of the word below),
// while internal/agent must render the DASHED flag the operator types.
//
// WHY internal/agent MUST SPELL IT AT ALL: the chat PA-duty paragraph teaches
// the console to file a defect the moment it sees one
// (`gopi task submit "<finding>" --tags bug`), and a prompt that spells a flag
// nobody can import would be a typed literal in the one place the repository
// cannot afford one — Rule 1/13/14's mirror defect, on a command the model is
// told to run immediately.
const TaskTagsWord = "tags"

// TaskTagsFlag is the dashed spelling — DERIVED from the word above, so the two
// halves of this one capability cannot drift apart.
const TaskTagsFlag = "--" + TaskTagsWord

// TaskUpdateForceWord is the `--force` of `gopi task update` — the CLI face of
// the operator's delivery-proof escape (GOPI_TASK_DELIVERY_PROOF_ALLOW).
//
// WHY IT LIVES HERE AND NOT IN cmd/agent (Rule 13/14, measured): THREE sites in
// TWO packages must agree on this one spelling. cmd/agent admits it in the flag
// table and parses it; internal/tools RECORDS it on the row's trail and PRINTS
// the operator notice — and internal/tools cannot import package main, so a
// constant declared only in cli_tasks.go would be a bare literal on the tools
// side, which is the defect Rule 1 exists to ban. The word is also DERIVED into
// its dashed form below, so no site types `--force`.
const TaskUpdateForceWord = "force"

// TaskUpdateForceFlag is the dashed spelling — DERIVED from the word above, the
// same shape TaskTagsFlag has, so the two halves cannot drift.
const TaskUpdateForceFlag = "--" + TaskUpdateForceWord

// TaskClearGroundWord is the `--clear-ground` of `gopi task update` — the FIRST
// half of the deliberate TWO-step move that re-points a row's recorded ground
// (deliverable.worktree + branch).
//
// WHY IT IS A SHARED WORD (Rule 13/14, measured): the refusal that sends an
// operator here is RENDERED by internal/tools (`task_repo_repair.go`'s
// TaskRepairGroundResetRefusalFmt — the 2026-09-12 foreign-owner guard), and the
// flag is ADMITTED by cmd/agent. Before this constant existed the refusal said
// "clear the recorded ground first" and named NO spelling at all, so a reader
// went looking for a verb or a flag that does not exist (epic 01a1fc52, Rule 15:
// a refusal must name a door that is real). A tools-side literal would be the
// mirror Rule 13 bans — on the one string the operator then types.
const TaskClearGroundWord = "clear-ground"

// TaskClearGroundFlag is the dashed spelling — DERIVED, the same shape
// TaskUpdateForceFlag has above, so the two halves cannot drift.
const TaskClearGroundFlag = "--" + TaskClearGroundWord

// TaskReasonWord is the `--reason` carried by every edit door that records a WHY
// (`gopi task update`'s status moves, its briefing revision, its reparent and
// the ground clear). The tools side renders it inside the ground refusal, so it
// lives here rather than as a literal on each side.
const TaskReasonWord = "reason"

// TaskReasonFlag is the dashed spelling — DERIVED, the same shape above.
const TaskReasonFlag = "--" + TaskReasonWord

// TaskIDWord / TaskIDFlag are the `--task-id` selector — the ROW ADDRESS of the
// task ledger (submit's id, the manual push's single-row choice, and the row
// legs' row).
//
// WHY IT IS SHARED (Rule 13/14, and this one is measured rather than
// theoretical): TWO packages must spell it in the SAME sentence. cmd/agent's
// `taskIDFlagName` dispatches it, and internal/tools RENDERS it into the manual
// push's receipt — the honest replacement for `push <peer>: accepted (… name
// the LOCK while the operator reads "the row arrived")` is a sentence carrying
// the deposit verb it should have used (`gopi task send --task-id T`) plus a
// peer-side check (`ssh <peer> 'gopi task show T'`). A second literal on the
// tools side would be exactly the mirror Rule 13 bans, on the one string whose
// drift would print a command the operator then types into a refusal.
//
// THE SAME WORD/FLAG SPLIT as RawFileWord/FileFlag and TaskTagsWord/
// TaskTagsFlag above: cmd/agent's parser matches BARE words (taskIDFlagName),
// while the rendered receipt carries the DASHED flag — so the word is the ONE
// definition and the dashed form is DERIVED from it.
const TaskIDWord = "task-id"

// TaskIDFlag is the dashed spelling — DERIVED from the word above (Rule 13:
// the two halves of one capability cannot drift apart).
const TaskIDFlag = "--" + TaskIDWord

// (THE `gopi memory sync` COMMAND LINE IS NOT IN THIS PACKAGE — 2026-11-06,
// because the verb's spelling belongs beside the argv that carries it. `MemorySyncVerb`, `MemorySyncPeerFlag`,
// `MemorySyncPushFlag` and `MemorySyncPullFlag` used to live here as the ONE
// definition of a command line that THREE callers composed; two of them
// (cmd/agent/memory_sync_tick.go, internal/tools/memory_store_trigger.go) were
// deleted in 2026-10, and the third — cmd/agent/cli_memory_sync.go, the
// dispatcher that admitted the flags — went with the whole cross-node sync CLI
// family in 5677cc6e. That commit left these four constants with NO READER
// anywhere in the tree, and a shared value kept alive past its last consumer is
// the mirror-literal defect Rule 13/14 bans. So they are DELETED rather than
// annotated, which is
// the same end the retired top-level `merge` constant met here when its last
// reader left (internal/standards/retired_pipeline_reference_gate.go records
// that precedent).
//
// WHAT TOOK THESE CONSTANTS' PLACE (2026-11-06): `cmd/agent/cli_memory_sync.go`
// dispatches the verb again, and its vocabulary lives in
// `internal/tools/memory_sync_verb.go` (`MemorySyncVerb`,
// `MemorySyncServeAssetsFlag`) — NOT here, for the reason `gopi memory backup`
// already taught: the same word is simultaneously the local verb an operator
// types and the REMOTE command this node ssh's, so it belongs beside the
// machinery that composes the argv, and a copy in a package that imports nothing
// would be a fifth mirror of one string (Rule 13/14). The one-way ledger copy
// `gopi memory backup` (internal/tools/ledger_backup.go) is the sibling door,
// and its vocabulary follows the same rule for the same reason.)

// ScreenStreamEnv is the environment channel of the screen-stream recording, and
// it is deliberately TERMINAL-AGNOSTIC: the mechanism used to be switched from a
// chat-only flag namespace (`GOPI_CHAT_SGR_STREAM`), which meant a worker run — a
// run that paints no screen of its own but whose output a reader still wants
// verbatim — could not be asked on or off at all. The name says what is recorded
// (the SCREEN) rather than which escape class happens to be inside the bytes, and
// it says nothing about which surface is running.
//
// Values: unset/empty = the surface's own default; `1`/any config.EnvBool truthy =
// record at the resolved path; any config.IsFalseValue value = decline; anything
// else IS the path.
const ScreenStreamEnv = "GOPI_SCREEN_STREAM"

// LegacyScreenStreamEnv is the pre-rename spelling. It is still READ (an operator
// who exported it and never changed their shell profile keeps recording), and it
// is documented as an alias rather than silently accepted: the run that finds it
// says so, so nobody's stale profile outlives its reason (Rule 15).
const LegacyScreenStreamEnv = "GOPI_CHAT_SGR_STREAM"
