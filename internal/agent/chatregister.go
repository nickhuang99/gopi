package agent

// Chat-session registration hooks (M4.6 钩子分流, devdoc/task-ledger-m46-47.md).
// Chat conversations and execution tasks are different entities: a chat
// process (gopi chat / gopi web) registers into the CHATS table (session
// facts: mode/model/first_msg/turns/tokens), while run/serve children keep
// registering into the TASKS ledger (taskregister.go — the queue semantics
// belong to execution). The split retires the chat-shaped [auto] rows.
//
// Contract mirrors taskregister.go: best-effort — any DB failure is logged
// and swallowed, never blocks the chat. Default ON;
// GOPI_CHAT_REGISTER=0 (or off/no/false/disabled) turns it off (Rule 2: the
// env var is read in exactly one place).

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/tools"
)

// chatRegisterEnv is the chat-registration kill switch (default ON).
// ChatRegisterEnv is the same string EXPORTED, so a caller that must state the
// switch it is reasoning about (a test asserting the replay's refusal is not the
// ordinary switch being off) names it instead of re-typing it (Rule 1b/13).
const chatRegisterEnv = "GOPI_CHAT_REGISTER"

// ChatRegisterEnv is chatRegisterEnv as an exported name — the ONE spelling,
// reachable from outside this package.
const ChatRegisterEnv = chatRegisterEnv

// chatRegisterEnabled reports whether chat-table registration is on (same
// truthiness convention as autoTaskRegisterEnabled, inverted for default-on:
// only an OFF spelling disables it).
//
// A REPLAY NEVER REGISTERS (internal/replaymode — measured 2026-10-12). The
// chats table lives in the OPERATOR's own ledger (`~/.gopi/memory.db`), and the
// measured run wrote a row there for a session that exists only in a scratch
// tree: a fact about a run nobody had, in the index the operator reads to
// remember the conversations they DID have. The suppression is HERE rather than
// at the three call sites (RegisterChatSession / RecordChatTurn / CloseChatRow)
// because this one function is already the gate all three ask — one definition,
// three consumers, no second answer (Rule 13/14).
//
// The refusal is not silent, but it does not print from here either: this
// predicate is called once per turn, and a sentence per turn would bury the
// run's own output. The announcement is made ONCE, where the flag is armed and
// the run's stderr is this verb's own channel (cmd/agent: the replay's startup
// notice names every write it will refuse).
func chatRegisterEnabled() bool {
	if replaymode.Active() {
		return false
	}
	return !boolvocab.IsFalse(os.Getenv(chatRegisterEnv))
}

// ChatRegisterEnabled is the READ side of chatRegisterEnabled, exported for the
// same reason ChatRegisterEnv is: a caller outside this package that must state
// whether the chats-table write would happen (the replay's own regression case
// asserts the flag stops it) asks this instead of a flag it cannot see.
func ChatRegisterEnabled() bool { return chatRegisterEnabled() }

// chatSessionSource derives the provenance recorded on a chat row — WHO held
// this console. It is the SAME evidence the tasks ledger's `source` column is
// derived from (tasksource.DeriveNow → config.AgentSpawnSignatureNow), not a
// second notion of provenance invented for chats: the two tables therefore
// speak ONE vocabulary, and a reader of either already knows these words.
//
// WHY IT IS READ AT PROCESS START, NOT AT ROW-WRITE TIME (the correction that
// made this field work — measured 2026-10-03). The evidence is heritable, and
// cmd/agent DESTROYS it a moment after startup: printLogPathsQuiet re-exports
// the anchor with the running process's OWN pid (exportAgentSessionAnchor,
// agent_spawn_guard.go), because for the agent-spawn GUARD the process really
// is the session and re-exec'd runs must read as self. A chat row is written
// long afterwards — on the first completed turn — so a live derivation there
// sees `GOPI_AGENT_SESSION=<our own pid>` and concludes "no agent above me".
// Measured consequence: `GOPI_AGENT_SESSION=<my agent pid> gopi chat --once -p
// …` wrote `human`, the exact false claim this column exists to prevent.
// CaptureConsoleProvenance() therefore takes the reading at the moment the
// anchor is still the INHERITED one, and this function answers with that
// snapshot. Nothing else changes: the guard keeps its semantics, the ledger
// keeps deriving live (its rows are written by the same process that would
// have exported the anchor).
//
// WHAT IT MEASURES. The anchor marker (config.AgentSessionAncestorEnv) carries
// the EXPORTING process's pid: a `gopi chat` the operator typed in his own
// terminal is the first exporter it ever sees, so the signature reports
// AnchorSelf (or no anchor at all) and the row is `human`; the same binary
// launched from an agent's shell — a `gopi chat --once -p '…'` verification
// probe, a replay driver, a test harness — inherits the ANCHOR of the agent
// above it, so the row is `agent`. That is exactly the defect this field
// closes: verification traffic drove the REAL chat binary on the REAL profile
// and left rows indistinguishable, in `session list --mode chat`, from the
// operator's own conversations.
//
// HONEST LIMIT (the tasksource package states the same one). The signature
// travels in the ENVIRONMENT, so it proves "no agent chain sat above this
// process when it started" — it cannot prove a person is typing. An
// unattended script with a clean environment still reads `human`, and an
// agent that deliberately deletes the signature reads `human` too. That
// residual is the evidence the repo already trusts for the tasks ledger, and
// it is strictly better than a column that recorded nothing at all.
func chatSessionSource() string {
	if captured := capturedConsoleSource(); captured != "" {
		return captured
	}
	return tasksource.DeriveNow("")
}

// processConsoleSource is the provenance read ONCE per process, before the
// anchor is overwritten. It is empty until CaptureConsoleProvenance runs.
// Both fields are ATOMIC, not plain: the write happens on the startup path
// (main's log-path banner, before any server exists) and the read happens later
// from whatever goroutine first registers a chat — a `gopi web` turn is NOT the
// main goroutine, so a plain bool/string pair here would be a data race rather
// than a style preference.
//
// The latch is an atomic bool rather than a sync.Once for one reason: a Once
// cannot be reset, so the ordering contract this file exists for could not be
// tested at all (the test must start from a clean process state).
var (
	consoleSourceDone atomic.Bool
	consoleSource     atomic.Value // string
)

// CaptureConsoleProvenance records WHO held this process's console, and it is
// the ONE place that reading is taken (Rule 2: one writer, one reader —
// chatSessionSource). cmd/agent calls it from exportAgentSessionAnchor, i.e.
// immediately BEFORE it overwrites the anchor with this process's pid: after
// that write the inherited evidence is gone, and a chat row written minutes
// later could no longer tell a verification probe from the operator's own
// console.
//
// Idempotent by contract: a path that calls it twice (main.go's printLogPaths
// and the chat REPL's own banner both run in one `gopi chat` process) must not
// re-read an environment it has since modified. Calling it is safe on any
// process — a process that never becomes an agent never calls it, and
// chatSessionSource then derives live.
func CaptureConsoleProvenance() {
	if consoleSourceDone.Swap(true) {
		return
	}
	consoleSource.Store(tasksource.DeriveNow(""))
}

// capturedConsoleSource returns the snapshot, or "" when none was taken.
func capturedConsoleSource() string {
	if v, ok := consoleSource.Load().(string); ok {
		return v
	}
	return ""
}

// resetConsoleProvenanceForTest clears the process-level snapshot so a test can
// observe the CAPTURE ORDER from a clean state. It exists because the latch is
// deliberately not a sync.Once (a Once cannot be reset, and an untestable
// ordering contract is how this defect shipped in the first place); it is
// unexported and lives beside the state it resets, so no production path can
// reach it.
func resetConsoleProvenanceForTest() {
	consoleSource.Store("")
	consoleSourceDone.Store(false)
}

// chatGitContext returns (branch, repoRoot) for the current working
// directory. Best-effort: outside a git repo both come back "" (a chat in
// ~ has no git context to index).
func chatGitContext() (branch, repoRoot string) {
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		repoRoot = strings.TrimSpace(string(out))
	}
	return branch, repoRoot
}

// RegisterChatSession is the chat START hook: ensures the chat's row in the
// chats table (session facts + git context + the turn's text). Called once on
// the first successful turn (cli_chat / cli_web); latched by
// a.chatRegistered. The mode is "chat" (gopi chat) or "web" (gopi web).
//
// firstMsg is a CANDIDATE, and firstMsgSource is the FACE it was recorded with
// (internal/inputsource). The row's first_msg column takes it only when it is
// the OPERATOR'S own line (tools.chatLineIsOperatorOwn); otherwise the row is
// born with tools.ChatFirstMsgPlaceholder and the text lives on in the session
// JSONL. That is the fix for the 2026-10-28 defect where a notify-injected turn
// titled the operator's conversation ("[system] 16 notices are pending: …") —
// the reasoning and the three legs are in internal/tools/chat_first_msg.go.
func (a *Agent) RegisterChatSession(mode, firstMsg, firstMsgSource string) {
	if a.chatRegistered || !chatRegisterEnabled() || a.SessionLog == nil {
		return
	}
	branch, repoRoot := chatGitContext()
	workdir, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	out, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID:      a.SessionLog.SessionID(),
		Mode:           mode,
		Source:         chatSessionSource(),
		Model:          a.Model,
		LogPath:        a.SessionLog.Path(),
		FirstMsg:       firstMsg,
		FirstMsgSource: firstMsgSource,
		Workdir:        workdir,
		GitBranch:      branch,
		GitRepoRoot:    repoRoot,
	})
	if err != nil {
		a.logAutoTaskf("chat table: register failed (continuing): %v", err)
		return
	}
	a.chatRegistered = true
	a.logAutoTaskf("chat table: %s", out)
}

// RecordChatTurn is the per-turn hook: bumps the chat row's activity facts
// (turn_count, tokens — cumulative CostTracker totals, ChatRecordTurn folds
// them monotonically). Best-effort; a missing row re-registers it first so
// an ensure-failure on turn 1 doesn't erase the session from the index.
//
// It is the "this turn opened a session turn" form, kept for the callers whose
// turn always does (the browser/term carriers pass a typed draft and nothing
// else). A caller that can answer the question must use RecordChatTurnOpened:
// a row that opens NO session turn must not charge `turn_count`, or the column
// runs ahead of `session log --turns` (task 01a17a37).
func (a *Agent) RecordChatTurn(prompt, completion, cacheTokens int) {
	a.RecordChatTurnOpened(true, prompt, completion, cacheTokens)
}

// RecordChatTurnOpened is RecordChatTurn with the TURN question answered by the
// caller — `opened` must come from sessionevent.IsTurnOpeningRow over the row's
// TEXT and FACE (the reader's predicate), never from a face whitelist of its own.
func (a *Agent) RecordChatTurnOpened(opened bool, prompt, completion, cacheTokens int) {
	if !chatRegisterEnabled() || a.SessionLog == nil {
		return
	}
	if err := tools.ChatRecordTurnOutcome(a.SessionLog.SessionID(), opened, prompt, completion, cacheTokens); err != nil {
		// Row absent (the ensure failed earlier) — re-ensure once, then
		// record. Any further failure is swallowed (best-effort). The re-ensure
		// passes NO first message: it exists to recreate the row, not to claim
		// the head with whatever text this later turn happened to carry.
		a.RegisterChatSession("chat", "", "")
		_ = tools.ChatRecordTurnOutcome(a.SessionLog.SessionID(), opened, prompt, completion, cacheTokens) //stylegate:ignore best-effort chat-row fallback after the re-ensure; the turn is already logged
	}
}

// OfferChatFirstMsg lets a LATER turn hand the row its first operator line.
//
// WHY IT IS NEEDED AT ALL: RegisterChatSession latches on the FIRST successful
// turn, so a session whose first turn was opened by a `gopi notify` delivery (or
// a background-job notice) is born with tools.ChatFirstMsgPlaceholder — and it
// must still be able to acquire a real head the moment the operator types. The
// write is idempotent by construction (tools.ChatOfferFirstMsg writes only over
// an unset column), so calling it every turn is correct and cheap; the first
// operator line wins and no later one can displace it.
//
// It is BEST-EFFORT in the same sense as its siblings: a chats row is an index
// over the session JSONL, so a failed offer costs a title, never the turn.
func (a *Agent) OfferChatFirstMsg(text, source string) {
	if !chatRegisterEnabled() || a.SessionLog == nil {
		return
	}
	if _, err := tools.ChatOfferFirstMsg(a.SessionLog.SessionID(), text, source); err != nil {
		a.logAutoTaskf("chat table: first msg offer failed (continuing): %v", err)
	}
}

// CloseChatRow is the chat END hook: stamps ended_at + end_reason
// (normal|interrupt|crash). Exit 0 → normal; a non-zero chat exit is an
// API/turn failure → interrupt. Idempotent (already-ended rows keep their
// first ending). Latches off.
func (a *Agent) CloseChatRow(exitCode int) {
	if !a.chatRegistered || a.SessionLog == nil {
		return
	}
	a.chatRegistered = false // one ending per session
	reason := "normal"
	if exitCode != 0 {
		reason = "interrupt"
	}
	if err := tools.ChatCloseSession(a.SessionLog.SessionID(), reason); err != nil {
		a.logAutoTaskf("chat table: close failed (continuing): %v", err)
		return
	}
	a.logAutoTaskf("chat table: %s ended (%s)", a.SessionLog.SessionID(), reason)
	_ = fmt.Sprint()
}

// ChatRegisterLatch reports whether the chat row was ensured this run (test
// seam — package main cannot read the unexported latch directly).
func (a *Agent) ChatRegisterLatch() bool { return a.chatRegistered }
