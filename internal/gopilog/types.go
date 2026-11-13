package gopilog

import (
	"encoding/json"
	"time"

	"github.com/nick/gopi/internal/requestusage"
)

// RawEvent represents a single JSONL line before processing
type RawEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Message   jsonMessage `json:"message"`
	Model     string      `json:"model,omitempty"`
	SessionID string      `json:"sessionId,omitempty"`
	Workdir   string      `json:"workdir,omitempty"`
	PromptID  string      `json:"promptId,omitempty"`
	// Source is the FACE a `user` row came from (internal/inputsource: Human,
	// LLM, Queue). It is written by SessionLogger.LogUserFrom since
	// 2026-10-25; a row written before that carries no field at all, and the
	// reader applies humanSourceForRow as the default — every pre-change user
	// row came from the person, so the default is a fact, not a guess.
	//
	// WHY IT IS READ AT ALL: `chat_surface action=type` lets the model enqueue
	// its OWN next turn, and the operator's ruling is that such a turn COUNTS
	// as a turn (from the outside the two are indistinguishable, so excluding
	// it would undercount the session). Keeping it counted while recording
	// which face produced it is the only way both halves stay honest.
	Source string `json:"source,omitempty"`
	// PID is the liveness stamp the session logger writes into its
	// session_start event (agent.ProcessAliveStampField). It is read here
	// under the WIRE key it is written with, so a reader can tell a session
	// whose writer stopped (killed, panic, power loss) from one that ended —
	// see Session.SawEnd. 0 for a session that predates the stamp.
	PID int `json:"pid,omitempty"`
	// RawLogPath records the session's raw HTTP log path (written by
	// NewSessionLogger since the resume feature; empty for older sessions).
	// gopi session resume <session-id> prefers it over timestamp heuristics.
	RawLogPath string `json:"rawLogPath,omitempty"`

	// request event fields
	//
	// THE FOUR COUNTERS ARE ONE OF TWO QUANTITIES, and the KEY that says which
	// is the marker pair below (tagged_usage / usage_base): see
	// requestCountersCumulative in requestusage.go for the one predicate, and
	// for why this is written down instead of guessed at read time.
	Iteration        int `json:"iteration,omitempty"`
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CacheHit         int `json:"cache_hit,omitempty"`
	CacheMiss        int `json:"cache_miss,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	// TaggedUsage marks a `request` row whose four counters are READ AS A
	// QUANTITY (internal/requestusage), named by UsageBase. Present = the
	// writer declared it; absent = a row written before the marker existed,
	// whose quantity the reader has to decide from the row's own contents.
	TaggedUsage bool `json:"tagged_usage,omitempty"`
	// UsageBase is UsageBaseCumulative or UsageBaseDelta
	// (internal/requestusage.RequestUsageBase*) when TaggedUsage is true.
	UsageBase string `json:"usage_base,omitempty"`
	// UsageSegment is the run segment the counters were accumulated in
	// (0-based): a resumed session writes a new segment, and its CostTracker
	// starts from zero — so the counted value of work is the LAST cumulative
	// reading of each segment, never the session's last reading alone. Empty on
	// an untagged row.
	UsageSegment *int `json:"usage_segment,omitempty"`

	// session_end fields
	ElapsedSec      float64 `json:"elapsed_sec,omitempty"`
	ExitCode        int     `json:"exit_code,omitempty"`
	TotalIterations int     `json:"total_iterations,omitempty"`
	TotalTokens     int     `json:"total_tokens,omitempty"`
}

type jsonMessage struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock is a parsed element from assistant message content array
type ContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

// ToolResultBlock is a parsed element from tool_result message content array
type ToolResultBlock struct {
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
}

// Iteration groups tool calls of one API request
type Iteration struct {
	Number    int
	ElapsedMs int64 // time from first tool_result to next request (approx)
	// from request event
	PromptTokens     int
	CacheHit         int
	CacheMiss        int
	CompletionTokens int
	// user prompt that triggered this iteration
	UserPrompt string
	// RequestOrdinal is the 1-based ordinal of the `request` ROW this content
	// belongs to, in the session's whole-file request sequence (1 = the first
	// send the log recorded). It is the CANONICAL round number of this
	// content — one `request` row = one HTTP request/response exchange
	// (sessionRoundCanonicalUnit) — and it exists because a content round and
	// a request row are NOT one-to-one: the reader drops an exchange whose
	// blocks were all empty (flushIter), so round k of the content view can be
	// request row k+m. A surface that numbers rounds by the canonical unit
	// prints THIS field, never the content index (measured on a real session:
	// 321 request rows against 256 content rounds).
	//
	// Zero means the content arrived with no request row of its own — the
	// trailing content after the log's last request (or a request-less log):
	// absence is stated, never stamped with a fabricated ordinal (Rule 15).
	RequestOrdinal int
	// RequestOrdinalStamped is whether the exchange's request stamp REACHED this
	// content — the difference between "the log recorded no request row" and
	// "the reader's clock-tick boundary reset the counter before it could stamp
	// this exchange" (measured on a real chat session: 128 request rows, and the
	// reader's per-tick re-open stamped the last one with 0).
	//
	// It exists because a missing ordinal is a fact a surface must state, and
	// the two causes need different words (Rule 15); it is false whenever
	// RequestOrdinal is 0.
	RequestOrdinalStamped bool
	// TurnBoundary is whether a HUMAN round BEGINS at this content: the first
	// content of a t+ clock-tick injection (the harness's own "this round is
	// about to start" marker — internal/replayplay's op0 emits the same shape),
	// or of the request row that carries it when no tick opened it (a log
	// recorded by a writer older than the per-request turn stamp).
	//
	// It is the ONE place the human-round boundary is decided (Rule 14): the
	// count (SessionTurns), the ordinal (TurnOrdinalIn) and the
	// playback's step numbering (internal/replayplay) all read it, so no
	// surface re-derives what "a human round" means.
	TurnBoundary bool
	// HarnessPrompts are the harness-injected user rows (the framework's own
	// messages: clock ticks, start/review checkpoints, job notices) that
	// arrived while this iteration was running — sessionevent.IsHarnessInjected
	// decides, so a reader and the writer agree on what an injection is.
	//
	// THEY ARE KEPT, NOT DROPPED, and they are kept SEPARATE from UserPrompt:
	// the prompt is what the operator asked for, and every surface that shows
	// "what was this round answering" must show that and not a tick. A view
	// that wants to show the framework's own traffic (which really is part of
	// the round: it was appended to the conversation the model saw) reads this
	// field, and can style it as secondary.
	HarnessPrompts []string
	// TurnSource is the FACE the turn-opening row came from
	// (internal/inputsource: Human, LLM, Queue) — set when TurnBoundary is
	// true, empty otherwise.
	//
	// IT DOES NOT CHANGE THE COUNT. The operator's ruling (2026-10-25) is that
	// a self-driven turn COUNTS as a turn, because from the outside the two are
	// indistinguishable — a session whose turns were mostly the model typing to
	// itself would otherwise read as a one-turn conversation. What this field
	// adds is traceability on top of an honest count: `gopi session log
	// --turns` can say WHICH face produced each turn, so a reader who needs to
	// separate the person's asks from the loop's self-continuations can, while
	// the headline number stays one number.
	//
	// Empty means the log did not record a source (every row written before
	// 2026-10-25); the reader's default for those is Human, because that is
	// what they all were.
	TurnSource string
	// TurnOpenedAt is WHEN the turn-opening ROW above was written — the moment
	// the banner shows a person (sessionevent.FormatTurnStamp renders it).
	//
	// WHY THE ROW'S STAMP AND NOT A CLOCK. Every surface that paints a turn
	// banner can reach a clock, and every one of them would be WRONG for the
	// case the banner is worth having: replaying yesterday's session, or
	// reopening a conversation from last week. The record already states when
	// each turn opened, so the moment is READ, never taken.
	//
	// It is the row's own timestamp, carried through the same seam flags that
	// carry TurnBoundary/TurnSource, so a turn with no row (the no-tick
	// fallback's derived seam) has a ZERO here and the banner prints the
	// explicit placeholder instead of inventing a time (Rule 15).
	TurnOpenedAt time.Time
	// TurnSeamRecorded is whether the mark above came from a ROW the log
	// recorded (a turn-opening user row, see sessionevent.IsTurnOpeningRow)
	// rather than from a fallback: SessionTurnBounds' "a session with content
	// has at least one turn" invents a boundary at index 0 for a log with no
	// mark at all, and the reader's no-tick path marks every content round a
	// seam for a log that recorded no operator row (readRequestRoundSeams).
	// Neither says WHO an exchange was, so neither may be read as a measured
	// person-only count.
	//
	// IT IS THE CRITERION THE TURN COUNTS READ (Rule 13/14:
	// SessionHumanTurnsUnmeasured is its one reader). Without it a log whose USER
	// ROWS ARE ALL HARNESS INJECTIONS — a resumed session, a worker run, a task
	// run nobody typed into — landed in those fallbacks and its derived turns
	// came out EQUAL TO ITS ROUND COUNT: 85 `human turns` beside `Iterations: 85`
	// on session 794cbd02-4722-43c3-924c-f7ba6f36534d, where the person had typed
	// nothing and every user row was a `[system]` tick. A count that cannot be
	// told apart from another counter is the collision this package exists to
	// prevent (Rule 15), and the fix is to say it was not measured.
	TurnSeamRecorded bool
	// tool calls in this iteration
	ToolCalls []ToolCall
	// text responses (non-tool-call assistant messages)
	TextBlocks []string
	// thinking/reasoning blocks
	ThinkingBlocks []string
}

// ToolCall represents one tool invocation
type ToolCall struct {
	ID     string
	Name   string
	Args   map[string]interface{}
	Result string
	// bash-specific
	ExitCode *int
	// RequestOrdinal is the canonical round (the `request` row ordinal) this
	// call belongs to — the number `--toolcall N` reports for it, so a tool
	// call and the exchange that carried it are named by the SAME unit. Zero
	// when the call's exchange recorded no request row (absence stated).
	RequestOrdinal int
	// Ordinal is this call's 1-based position in the session's whole sequence
	// of tool calls, in log order — what the `--toolcall N` selector counts.
	// Zero means the ordinal was never stamped (a caller that built the call
	// itself, not the reader).
	// Timestamp of the assistant tool_use event that produced this call.
	Timestamp time.Time
	// InputRaw preserves the complete, ordered tool input (query/topic/
	// question/answer etc.) as logged, so a filtered tool view can render
	// the full input even for tools the formatToolCall switch doesn't model.
	InputRaw json.RawMessage
}

// Session holds the parsed session data
type Session struct {
	ID      string
	Model   string
	Started time.Time
	Workdir string
	Task    string // user task (first user message)

	Iterations []Iteration

	// RequestEvents is how many `request` events the log carried — one per send
	// to the model, INCLUDING the exchanges whose blocks were all empty and
	// which the reader therefore dropped from Iterations (see flushIter).
	//
	// IT IS THE CANONICAL ROUND COUNT (Operator's definition, 2026-10-10): one
	// `request` row = one HTTP request/response = one round. SessionRounds
	// returns it, so every surface that offers a `--rounds` view, a `--round N`
	// selector or a round ordinal numbers the SAME exchanges.
	//
	// It equals neither TotalIterations (the agent loop's own count, which for
	// a chat session is one per REPL turn) nor len(Iterations) (the exchanges
	// that carried content — sessionContentRounds), and a surface that prints a
	// count must say which of the three it is (sessionevent.CounterUnit* —
	// Rule 15).
	RequestEvents int

	// RequestUsageBases is the QUANTITY each `request` row's four counters
	// carry, one entry per row, in file order (internal/requestusage: a marker
	// the writer sets, or a detection for a row written before the marker
	// existed). RequestUsageRows are those rows' readings, parallel to it.
	//
	// THEY EXIST BECAUSE THE COLUMN HAS TWO MEANINGS (the defect this repairs,
	// epic 01a195b0): the task path logs per-request INCREMENTS and the chat
	// path logs the CostTracker's RUNNING TOTALS, under one event shape. A
	// summary that adds the column is only correct for the first, so the two
	// reader surfaces consult these through requestusage.Counted instead of
	// summing, and TotalPrompt below is that fold's answer.
	RequestUsageBases []requestusage.RequestUsageBase
	RequestUsageRows  []requestusage.Reading
	// RequestUsageUnknown counts the rows whose quantity could not be decided
	// (no marker and no evidence in the values). Non-zero means the folded
	// totals below EXCLUDED those rows and every render must say so — a
	// headline that quietly left a row out would be the same class of lie as
	// summing a cumulative column (Rule 15).
	RequestUsageUnknown int
	// RequestUsageSegments is how many cumulative RUNS the fold spanned (a
	// resumed session restarts its counters and holds more than one). It is the
	// SPAN of TotalPrompt, stated so a render can say "summed over N runs"
	// instead of leaving a reader to wonder why three segments' numbers add up.
	RequestUsageSegments int
	// RequestUsageBasis is the EVIDENCE the fold's reading rests on
	// (requestusage.ClassifyReason*): the writer's own marker, or — for a log
	// written before the marker existed — the rows summing to the recorded
	// session total, a counter falling inside a run segment, or the monotone
	// default. It is carried so a render can state the ground rather than
	// assert a number (Rule 15).
	RequestUsageBasis string

	// final summary
	ElapsedSec float64
	ExitCode   int
	// TotalIterations is how many LOOP PASSES the whole log recorded: the SUM
	// of every `session_end` total_iterations in it, one per run segment.
	//
	// IT IS A SUM AND NOT THE LAST WRITER'S NUMBER (the defect this repairs,
	// task 01a17ae2). A session that was resumed holds TWO runs: the original
	// segment's `session_end total_iterations: 29`, then — after `gopi chat
	// --resume` opened a second segment — the resumed run's own end, which for
	// a resume that ran no turn at all is `total_iterations: 0`. The reader
	// assigned the field (`s.TotalIterations = ev.TotalIterations`), so the
	// empty last segment ERASED the 29 and every surface printed PASSES = 0,
	// measured on session 4a0957a3-bb4e-4337-a761-20606c87d1fd (line 868:
	// total_iterations 29; line 871: 0). Each run's end reports only ITS OWN
	// passes — a resume legitimately starts at 0, because the REPL's pass
	// counter is per process (cmd/agent/cli_chat.go, task 01a16fb3) — so the
	// session-level figure has to accumulate them.
	//
	// The span is therefore the WHOLE LOG, which is the same span
	// SessionTurns counts (every turn the file holds, across segments). The
	// two counts describe different quantities on that one span; neither is a
	// per-run figure, and no surface may print one under the other's word.
	TotalIterations int
	// PassSegments is how many `session_end` events fed TotalIterations — i.e.
	// how many run segments the log holds a completed end for (1 for a session
	// that was never resumed, 0 for one that recorded no end at all). It exists
	// so a render can SAY the sum is a sum: a reader who sees 29 on a log whose
	// tail says "0 passes this run" needs to be told which span the number
	// covers (Rule 15: a count states its span, it does not leave it guessed).
	PassSegments    int
	TotalTokens     int
	TotalPrompt     int
	TotalCompletion int
	// EndUnrecorded is true when the log carried NO session_end event: the
	// session was killed, panicked before the end hook, or is still running.
	// It is the difference between "this session ended with exit code 0" and
	// "nothing ever recorded how this session ended" — two states ExitCode
	// alone cannot tell apart, because the live-session fallback leaves
	// ExitCode at its zero value. A caller that renders an outcome MUST read
	// this flag (Rule 15: a session killed mid-run is not a success).
	//
	// Spelling note (Rule 15a): the flag is POSITIVE and defaults to false, so
	// a caller that sets ExitCode is understood to be reporting a recorded
	// exit. Only a reader that actually looked at the log — and found no end —
	// sets it, and it is set at exactly one place (the log reader).
	EndUnrecorded bool

	// WriterPID is the liveness stamp from the LAST session_start event (0 when
	// the session predates the field). It is what lets a reader say WHICH
	// process stopped writing — the evidence a killed session leaves behind.
	//
	// THE LAST SEGMENT'S, BECAUSE A RESUME IS A NEW PROCESS (task 01a189b4).
	// MEASURED, not assumed: before this task's change the reader ALREADY
	// reported the last header's pid (session 66243525-16ab-4ce7-9133-84d74c539840,
	// three headers → 3954507, the third). The stale pid `session show` printed
	// came from internal/rundir's first-line header read, a different reader —
	// this field needed no fix, only the segment COUNT beside it for a caller to
	// state which segment it is reporting.
	WriterPID int
	// StartSegments is how many `session_start` headers the log holds — one per
	// run segment (1 for a session never resumed, 0 for an unreadable log). It
	// is the SPAN of WriterPID above: a reader that prints a pid beside a
	// segment count of 3 is saying "the third segment's writer", which is a
	// different claim from "the writer" and must not be left to be guessed
	// (Rule 15).
	StartSegments int
}

// ContextTrimEvent is one context_trim activity parsed from a session log:
// the context_trim call itself (CallID + arguments) plus the size comparison
// between the trimmed call's original result and its post-trim replacement.
type ContextTrimEvent struct {
	// CallID is the context_trim tool call's own tool_use id.
	CallID string
	// TargetCallID is the trimmed call's id (the call_id argument).
	TargetCallID string
	// ReplaceWith is the replace_with argument. "" means the pair was
	// DELETED, not replaced.
	ReplaceWith string
	// TargetName is the trimmed tool's name ("" when the target call is not
	// present in this session's log).
	TargetName string
	// Outcome classifies the trim from the context_trim tool_result:
	// "replaced" (pair kept, content swapped), "removed" (pair deleted), or
	// "noop" (unknown/incomplete/already-processed call).
	Outcome string
	// RemovedMessages is the N from "removed call X + its result (N
	// message(s) dropped from context)". 0 for replaced/noop.
	RemovedMessages int
	// BeforeBytes is the byte size of the trimmed call's original logged
	// tool_result. 0 when the target call is not in the log.
	BeforeBytes int
	// AfterBytes is the post-trim size: len(ReplaceWith) for a replacement,
	// 0 for a deletion (the whole pair is gone from context).
	AfterBytes int
	// Timestamp of the context_trim tool_use event.
	Timestamp time.Time
}
