// Package replaydrive turns a RECORDED gopi session into the two things a
// faithful re-run of the real chat surface needs: a SCRIPT (the model's
// answers, replayable) and an in-process Claude-SSE STUB SERVER that serves
// that script on the wire shape the real client parses.
//
// WHY IT EXISTS. `gopi chat --replay <session-id>` re-runs the REAL chat
// surface — the real REPL, the real line editor, the real answer band, the real
// renderer — over a session that already happened, so the film of that session
// can be shot from the surface itself instead of being redrawn by a second
// renderer that would drift from it. Two things must therefore be taken out of
// the re-run's hands, and this package takes exactly those two:
//
//   - the MODEL. The stub is an httptest server on 127.0.0.1:0 serving
//     {base}/v1/messages with the same frame vocabulary
//     internal/testutil/mockllm.go uses (message_start → content_block_* →
//     message_delta → message_stop), so the wire the client reads is byte-shaped
//     like a real provider's. It is IN-PROCESS and its listener is the
//     httptest one: nothing is left listening once the run ends (no port, no
//     file, no port file).
//
// WHAT THAT LISTENER IS, IN ACCURATE WORDS (the standard is
// devdoc/replay-concurrency-and-port-semantics.md §1, whose measurement is on
// this host): the socket is bound to **loopback only** on a port the **kernel
// assigns** (the stub asks for `:0`; the number exists only after the bind), so
// it is unreachable from any other HOST. It is NOT a private channel: **any
// process on this machine that knows the port can connect to it** — the
// isolation is that the port is not advertised, not that other processes are
// excluded. Do not write the stronger claim ("no other process can reach it"):
// it was measured FALSE by two independent clients (bash `/dev/tcp` and a
// python socket, each answered `HTTP/1.0 200 OK`). Nothing here is engineered
// around that fact — a unix socket would be a new mechanism (Rule 35) answering
// a question nobody asked.
//   - the TOOLS. Every recorded tool_use call is answered with the RECORDED
//     tool_result, byte for byte, through the agent's exported executor seam
//     (agent.SetToolExecutor). The real dispatch (executeToolDefault) is never
//     reached, so a recording of `bash`/`write`/`bash mkdir` cannot touch the
//     machine it is replayed on.
//
// The recording is the ONLY source of the script: LoadRecording reads the
// session JSONL the live run already wrote, so a replay is a function of that
// file and nothing else.
package replaydrive

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// The recording's row kinds and content-block kinds (Rule 1b: the JSONL's own
// vocabulary, named once and compared against, never retyped inline).
const (
	// fieldRowType is the JSONL's own "type" field — the one key every row is
	// decoded by, named once (Rule 1b).
	fieldRowType = "type"

	rowTypeAssistant  = "assistant"
	rowTypeToolResult = "tool_result"
	rowTypeRequest    = "request"
	rowTypeUser       = "user"

	blockTypeText    = "text"
	blockTypeToolUse = "tool_use"
	// blockTypeThinking is parsed and DROPPED: a thinking block is the model's
	// private reasoning, it never reached the screen in the original run
	// (the chat surface renders the answer, and a thinking block is not one),
	// and re-serving it would make the re-run show something the recording
	// never showed. Dropping is deliberate, and TestLoadRecording names it.
	blockTypeThinking = "thinking"
)

// The stub's wire vocabulary and limits.
const (
	// claudeMessagesPath is the path the real client appends to its base URL
	// when the base does NOT end in "/anthropic" — the shape our override uses
	// (deepseek.Client.claudeMessagesURL). The stub answers it, and answers any
	// other path too: a 404 for a path the client happened to derive would look
	// like a provider outage and would be diagnosed as one.
	claudeMessagesPath = "/v1/messages"
	// stubContentBlockIndex is the index every frame carries. The live
	// providers stream several blocks; the recording's shape is one text block
	// or one tool_use block per response, and the client addresses them by this
	// index, so one value is not a simplification the client can see.
	stubContentBlockIndex = 0
)

// ToolCall is one recorded tool_use block.
type ToolCall struct {
	// ID is the tool_use id the recording carries (e.g. "call_00_…"). It is the
	// key the recorded result map is addressed by, and it is re-served VERBATIM:
	// a re-minted id would break nothing on the wire but would make the replay's
	// transcript impossible to line up with the recording it re-runs.
	ID string
	// Name is the tool name the model asked for ("bash", "read", …). It is
	// replayed verbatim and the executor seam answers by ID, so the NAME is
	// never used to decide what runs — which is what keeps a recorded `write`
	// from reaching the filesystem.
	Name string
	// Input is the recorded arguments object, verbatim on the wire.
	Input json.RawMessage
}

// Step is one content block of one scripted model response, in order.
type Step struct {
	// Kind is blockTypeText or blockTypeToolUse.
	Kind string
	// Text is the answer text (Kind == blockTypeText).
	Text string
	// Tool is the recorded call (Kind == blockTypeToolUse).
	Tool *ToolCall
}

// Turn is one operator turn: the text the operator typed, and the model
// responses it produced, in order.
type Turn struct {
	// Text is the recorded user message. It is what the replay TYPES into the
	// real editor.
	Text string
	// Source is the FACE this row arrived under when it was RECORDED
	// (internal/inputsource: human / queue / notify / llm), resolved the same
	// way the loader's turn predicate resolved it.
	//
	// IT RIDES THE TURN BECAUSE TYPING LOSES IT (measured 2026-11-04 on session
	// 0b51ae4a-fa82-41ba-8147-1ba3ebc92476). A replay re-enters recorded lines
	// as KEYSTROKES, and a keystroke is `human` by construction — so a turn that
	// arrived from `gopi notify` was re-stamped human on the way back in, the
	// surface's own predicate (sessionevent.IsTurnOpeningRow) then answered "no
	// turn" for its carrier text, and the band counter froze: 7 bands numbered
	// 1,2,2,2,2,2,2 on the filmed screen while the recording's own screen read
	// 1..7. The face is a fact about the ROW, and a replay that forgets it is
	// re-running a different conversation.
	//
	// IS IT WHAT THE SURFACE WILL BE TOLD. cmd/agent's driver hands this value to
	// the REPL for the line it types, so the band number advanced at that turn
	// is computed from the SAME predicate, over the SAME face, that put this
	// turn in Turns in the first place (Rule 14: one rule, two readers).
	Source string
	// OpenedAt is WHEN this turn's row was WRITTEN in the recording — the
	// timestamp of the row that opened it.
	//
	// IT EXISTS FOR THE SAME REASON Source DOES, one field over: a replay
	// re-enters a recorded turn as KEYSTROKES, and a live REPL mints a turn's
	// moment from its own clock (`time.Now()` in cmd/agent/cli_chat.go) because
	// on a LIVE run that is the truth. Replayed, it is a lie of exactly the kind
	// those keystrokes would tell: measured 2026-11-08, replaying session
	// 6b01dd46 (opened 2026-11-07 12:11:17) painted
	// `turn 1 · 2026-11-08 15:59:32` — TODAY, on a recording a day old.
	//
	// Zero means THE ROW RECORDED NO USABLE TIMESTAMP (a hand-edited record, a
	// writer older than the field, a truncated line). It is carried as a zero
	// rather than dropped so the surface can state the absence with the shared
	// placeholder (sessionevent.TurnStampPlaceholder) instead of substituting
	// the clock, which would print an authoritative-looking wrong day (Rule 15).
	OpenedAt time.Time
	// Responses are the model responses this turn produced, in request order.
	// Each one is served to exactly one model request, which is why the replay's
	// "wait for the turn to end" predicate is the stub's request counter.
	Responses [][]Step
}

// Recording is a parsed session: its turns, the whole response script in
// request order, and the recorded tool results.
type Recording struct {
	// Path is the session JSONL this was parsed from (recorded for the run's own
	// report; never used to make a decision).
	Path string
	// SessionID is the session the recording belongs to, when the file carried
	// one (the session_start row does) — reported, never guessed.
	SessionID string
	// Turns are the operator's turns, in order.
	Turns []Turn
	// Responses is every response in REQUEST order, flattened across turns: it
	// is the script the stub serves, indexed by the request number.
	Responses [][]Step
	// Results maps a tool_use id to the tool result the recording fed back.
	// The value is the recorded content VERBATIM, header and trailing newline
	// included: the replay's contract is that the model receives the same
	// bytes the recorded run's model received.
	Results map[string]string
}

// ResponseCount is how many model responses the recording holds — the length of
// the script, and therefore how many requests are replayed faithfully.
func (r *Recording) ResponseCount() int {
	if r == nil {
		return 0
	}
	return len(r.Responses)
}

// RequestsThroughTurn reports how many model requests the recording services by
// the END of turn i (0-based): the request count a replay must see before it may
// type the NEXT turn. A turn with no recorded response still counts the
// responses before it, so the number is monotonic and a caller can wait on it
// without knowing the recording's shape.
func (r *Recording) RequestsThroughTurn(i int) int {
	if r == nil {
		return 0
	}
	n := 0
	for t := 0; t < len(r.Turns) && t <= i; t++ {
		n += len(r.Turns[t].Responses)
	}
	return n
}

// --- JSONL parsing ---

// recordedRow is one session JSONL row, decoded only as far as the parser
// needs: the row type, and the message it may carry.
type recordedRow struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	// Source is the FACE a `user` row came from (internal/inputsource). It is
	// read HERE because the turn predicate needs it
	// (sessionevent.IsTurnOpeningRow): a `gopi notify` delivery is one complete
	// input, and its TEXT is the framework's own sentence, so text alone cannot
	// tell it from a clock tick.
	Source string `json:"source"`
	// Timestamp is WHEN the row was written, in the recording's own zone. It is
	// read here because a `user` row's own stamp is the ONLY record of when its
	// turn opened (replaydrive.Turn.OpenedAt carries it out). A row that carries
	// nothing leaves this empty, and the turn it opens then states an explicit
	// absence rather than a clock reading.
	Timestamp string `json:"timestamp"`
	Message   *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// LoadRecording parses a session JSONL into a Recording.
//
// WHAT IS KEPT, and why the parsing is not "read every row":
//
//   - user rows whose content is a STRING are the operator's turns. A user row
//     whose content is a LIST is the TOOL-RESULT carrier (that is how the
//     Claude wire represents a tool result — role "user", one tool_result
//     block), and those are collected into Results instead of becoming turns.
//   - assistant rows carry text/tool_use/thinking blocks. Consecutive assistant
//     rows are ONE response: the live run logs each block family as its own row
//     (text, then tool_use) and the response boundary is the `request` row it
//     wrote when that response had been consumed.
//   - request rows are the response BOUNDARY (the live session log writes one
//     after each consumed model response), which is what makes the script's
//     request-order index exact rather than inferred from a turn's shape.
//
// A file with no turn and no response is an error: a replay of nothing would
// run a real chat surface against an empty script and report success.
// recordedRowMoment decodes one row's `timestamp` field into the moment a turn
// banner prints, and answers the ZERO time when the row carried none or carried
// one this program cannot read.
//
// THE PARSE IS RFC3339 AND THE ZONE IS THE ROW'S OWN. The session log writer
// stamps `time.Time` through encoding/json (internal/gopilog's own row type does
// the same), so the field is RFC3339Nano and carries the recorder's offset.
// Parsing it that way keeps the wall clock the session ran on: a turn written at
// 12:11:17 +08:00 prints 12:11:17, not the same instant restated in the
// reader's zone — the rule sessionevent.FormatTurnStamp documents one layer up.
//
// AN UNREADABLE STAMP IS AN ABSENCE, NEVER A FALLBACK: returning the zero time
// is what lets the banner state sessionevent.TurnStampPlaceholder. Substituting
// time.Now() here would be indistinguishable on screen from a correct banner and
// would state the wrong day on exactly the records a banner is worth having.
func recordedRowMoment(stamp string) time.Time {
	stamp = strings.TrimSpace(stamp)
	if stamp == "" {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}
	}
	return at
}

func LoadRecording(path string) (*Recording, error) {
	raw, err := os.ReadFile(path) //stylegate:ignore a session JSONL is read whole on purpose — the parser needs every row and the file is bounded by the session it records
	if err != nil {
		return nil, fmt.Errorf("read the recording %s: %w", path, err)
	}
	rec := &Recording{Path: path, Results: map[string]string{}}
	var cur []Step
	flush := func() {
		if len(cur) == 0 {
			return
		}
		rec.Responses = append(rec.Responses, cur)
		if len(rec.Turns) == 0 {
			// A response before any operator turn (a seeded/resumed session can
			// open that way). It still belongs to the script, so it gets a turn
			// of its own with empty text — the replay then types nothing and
			// waits for the response, which is exactly what the recording shows.
			rec.Turns = append(rec.Turns, Turn{})
		}
		last := &rec.Turns[len(rec.Turns)-1]
		last.Responses = append(last.Responses, cur)
		cur = nil
	}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row recordedRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		if rec.SessionID == "" && row.SessionID != "" {
			rec.SessionID = row.SessionID
		}
		// THE RESPONSE BOUNDARY IS A ROW WITH NO MESSAGE, so it must be handled
		// BEFORE the message-nil skip below. Measured on the real run (replay4):
		// with the skip first, every request row was dropped, ALL of the
		// session's assistant blocks fused into ONE response, and the replay
		// then re-served that single tool-bearing response for every later
		// request — the agent looped 40 rounds against a recording that holds
		// five separate answers.
		if row.Type == rowTypeRequest {
			flush()
			continue
		}
		if row.Message == nil {
			continue
		}
		switch row.Type {
		case rowTypeAssistant:
			steps, err := parseAssistant(row.Message.Content)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: %w", path, i+1, err)
			}
			cur = append(cur, steps...)
		case rowTypeToolResult:
			id, content := parseToolResult(row.Message.Content)
			if id != "" {
				rec.Results[id] = content
			}
		case rowTypeUser:
			msg, ok := stringContent(row.Message.Content)
			if !ok {
				break
			}
			// A HARNESS INJECTION IS NOT A TURN THE OPERATOR TYPED. The re-run
			// a replay films is the SURFACE's, and the surface never showed the
			// injected text as the person's own line: a `[system] …` notice (a
			// task event, a clock tick) arrives from the harness, not from the
			// keyboard, so typing it back would put words on the filmed screen
			// that the original screen never carried.
			//
			// It is also what keeps the re-run FINITE. Measured on session
			// 8616b46c (2026-10-20, 702 rows): 202 string user rows, of which
			// 187 are `[system] …` injections and only 15 are the operator's.
			// Treating all 202 as turns made `gopi chat --replay` type 187 lines
			// the person never typed and report `turns typed: 201/201` beside a
			// status row reading `turn201`, while every other session surface —
			// which counts turns by the SAME operator-row rule
			// (sessionevent.IsTurnOpeningRow, the reader's TurnBoundary) —
			// said 15. One recorded session cannot have two turn counts.
			//
			// The PREDICATE is the reader's own, not a second spelling of
			// "[system]": the replay and the session views must answer "whose
			// row is this?" with one function, or they will drift again. The
			// row is still PARSED as a seam (`flush` runs): it bounds the
			// preceding exchange; it simply opens no turn of its own.
			flush()
			// THE SAME PREDICATE THE SESSION READER USES
			// (sessionevent.IsTurnOpeningRow) — one recorded conversation cannot
			// have two turn counts, and the two surfaces this drives
			// (`chat --replay`'s typed turns and `session log --turns`) are
			// required to agree on the number as well as on the rule.
			//
			// THE OPERATOR'S RULING (2026-10-27) widened the rule rather than
			// moving it: a `gopi notify` delivery and a `chat_surface type`
			// line are COMPLETE INPUTS and open turns, so both count. Neither
			// is typed back on the filmed screen (the person typed neither), so
			// a caller that asks "how many lines must I type?" reads TurnCount
			// from the same field this appends to and gets the same rule.
			// THE FACE RIDES THE TURN (Turn.Source), and it is resolved through
			// THIS package's one resolver so the value a replayed line is typed
			// back under is the very value the predicate below judged it with.
			// Typing loses it otherwise: a keystroke is `human` by construction,
			// so a `notify` turn re-entered through the editor would be re-stamped
			// and the surface's own predicate would then deny it a turn — 7 bands
			// numbered 1,2,2,2,2,2,2 (measured 2026-11-04, session
			// 0b51ae4a-fa82-41ba-8147-1ba3ebc92476).
			if face := sessionevent.TurnRowSource(row.Source); sessionevent.IsTurnOpeningRow(msg, face) {
				// THE MOMENT RIDES THE TURN BESIDE THE FACE, and for the same
				// reason: typing a recorded line back through the editor loses
				// everything the ROW knew, including when it was written. A
				// timestamp this parser cannot read is kept as the ZERO value
				// (never "now", never a guess) so the surface states an explicit
				// absence — a truncated or hand-edited record must not produce a
				// banner that names an authoritative-looking wrong day.
				rec.Turns = append(rec.Turns, Turn{Text: msg, Source: face, OpenedAt: recordedRowMoment(row.Timestamp)})
			}
		}
	}
	flush()
	if len(rec.Responses) == 0 {
		return nil, fmt.Errorf("%s carries no model response — there is nothing to replay (%d turns parsed)", path, len(rec.Turns))
	}
	// A recording whose FIRST turn holds no response is a turn the script cannot
	// serve: the replay would type a line and then wait forever. Dropping the
	// leading empty turn is the honest repair (the alternative — keeping it —
	// makes the replay's own completion predicate unreachable), and the count is
	// reported by TurnCount so a caller can say what it did.
	if len(rec.Turns) > 0 && len(rec.Turns[0].Responses) == 0 {
		rec.Turns = rec.Turns[1:]
	}
	if len(rec.Turns) == 0 {
		return nil, fmt.Errorf("%s has no operator turn that a model response follows — a replay needs at least one turn to type", path)
	}
	return rec, nil
}

// parseAssistant decodes one assistant row's content into steps, dropping
// thinking blocks (see blockTypeThinking).
func parseAssistant(content json.RawMessage) ([]Step, error) {
	blocks, ok := contentList(content)
	if !ok {
		return nil, nil
	}
	var out []Step
	for _, b := range blocks {
		switch b.Type {
		case blockTypeText:
			if b.Text != "" {
				out = append(out, Step{Kind: blockTypeText, Text: b.Text})
			}
		case blockTypeThinking:
			// Deliberately dropped — see blockTypeThinking's comment.
		case blockTypeToolUse:
			if b.Name == "" || b.ID == "" {
				return nil, fmt.Errorf("a tool_use block with no id/name cannot be replayed: id=%q name=%q", b.ID, b.Name)
			}
			input := b.Input
			if len(input) == 0 {
				// An argument-less call is legal on the wire; the client expects
				// an OBJECT for the accumulated input_json_delta, so an absent
				// input is replayed as the empty object rather than as a missing
				// field (a missing field would arrive as no arguments at all).
				input = json.RawMessage("{}")
			}
			out = append(out, Step{Kind: blockTypeToolUse, Tool: &ToolCall{ID: b.ID, Name: b.Name, Input: input}})
		}
	}
	return out, nil
}

// parseToolResult pulls the tool_use id and the recorded result text out of a
// tool_result row. The content is a string in every recording this repo writes;
// a list-shaped content is flattened to its text parts so an older or foreign
// shape still replays (never silently dropped — an unmatched id would make the
// executor seam report a missing result at run time, which is later than the
// parse).
func parseToolResult(content json.RawMessage) (id, text string) {
	blocks, ok := contentList(content)
	if !ok {
		return "", ""
	}
	for _, b := range blocks {
		if b.Type != "" && b.Type != "tool_result" {
			continue
		}
		if b.ToolUseID != "" {
			id = b.ToolUseID
		}
		if b.Content != nil {
			text = contentText(b.Content)
		}
	}
	return id, text
}

// stringContent reports the row's content when it is a plain JSON string.
func stringContent(content json.RawMessage) (string, bool) {
	if len(content) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(content, &s); err != nil {
		return "", false
	}
	return s, true
}

// contentText renders a tool_result content field as text: a string as itself,
// a list of text blocks concatenated, anything else by its JSON.
func contentText(raw json.RawMessage) string {
	if s, ok := stringContent(raw); ok {
		return s
	}
	blocks, ok := contentList(raw)
	if !ok {
		return string(raw)
	}
	var sb strings.Builder
	for _, b := range blocks {
		sb.WriteString(b.Text)
	}
	return sb.String()
}

// textBlock is the union of the fields the parser reads from ANY content block.
// One struct for every kind keeps the parser to one decode pass (the blocks
// overlap: tool_use and text share no field names, but a second struct per kind
// would double the decode for no fact).
type textBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// contentList decodes a content field as a list of blocks. A JSON null, an
// absent field and a string all report false — the caller decides what that
// means for its row kind.
func contentList(raw json.RawMessage) ([]textBlock, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var blocks []textBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, false
	}
	return blocks, true
}

// --- the stub server ---

// Stub is an in-process Claude-compatible server that serves a Recording's
// script, one response per request, repeating the LAST response for any request
// beyond the script.
//
// THE OVERLONG RULE, and its reason: a replay drives the real chat surface, and
// the surface makes requests the recording's shape cannot always predict — a
// confirmation round, a nudge after a `/tools` toggle, or the extra round the
// agent issues when it decides the task is not finished. Refusing those would
// end the replay mid-turn with a hard wire error that says nothing about the
// replay; re-serving the LAST recorded answer keeps the run alive and lands it
// on a response the recording actually contains, with stop_reason end_turn —
// which terminates the turn instead of inviting another tool round (the
// failure mode of repeating a tool_use response). Every such request is
// COUNTED and reported (OverlongRequests), so a run can state how much of what
// it showed was script and how much was the tail.
type Stub struct {
	srv *httptest.Server

	// Notice, when set, reports where this stub's own notices go. It is a
	// FUNCTION rather than an io.Writer so a caller that routes the notices
	// later (or repoints them) is honoured at the moment of the write: the
	// notice below is emitted from an HTTP handler on the server's own
	// goroutine, which may already be running when the caller takes the screen.
	//
	// WHY IT IS A SEAM AT ALL (measured 2026-10-04). The overlong notice used to
	// be a bare `fmt.Fprintf(os.Stderr, …)`. A replay that films ITSELF has the
	// reserved chat layout painting absolute rows onto that same terminal, and a
	// bare write with no cursor-positioning sequence lands wherever the layout's
	// cursor is — so the sentence was photographed INTO the frames
	// (`[gopi replay] turn 2/4: typing the recorded prompt` and a `[gopi] chat
	// table: …` line are readable inside the delivered GIF, byte offsets 114435
	// and 115131 of the producer's raw stream, each with no preceding `\e[r;cH`).
	// A nil Notice keeps the historic destination, so a caller that is not
	// filming changes nothing.
	Notice func() io.Writer

	mu        sync.Mutex
	reqs      int
	bodies    []string
	overlong  int
	served    []ServedResult
	missingID []string
	rec       *Recording
}

// noticeWriter resolves where this stub's notices go RIGHT NOW: the caller's
// Notice function when one was installed, and the process's stderr otherwise.
// The historic destination is the default so a caller that never installs one
// (every existing caller, and every test) sees byte-for-byte what it saw before
// this seam existed.
func (s *Stub) noticeWriter() io.Writer {
	if s != nil && s.Notice != nil {
		if w := s.Notice(); w != nil {
			return w
		}
	}
	return os.Stderr
}

// ServedResult is one tool result the stub handed back to the agent through the
// executor seam, kept so a run can prove byte-for-byte that what the model
// received is what the recording held (Rule 15: the proof is the record, not
// the intent).
type ServedResult struct {
	ID     string
	Name   string
	Result string
	// Recorded is false when the recording held no result for this id — the
	// run then served an explicit "no recorded result" sentence instead of an
	// empty string, and this flag is how the report says so.
	Recorded bool
}

// NewStub starts the stub over a recording. The caller MUST Close it: the
// listener is the httptest one, and Close is what releases it.
//
// panic-ok: an empty recording is a programming error AT THE CALL SITE (the
// caller built the recording), not a runtime condition to degrade from, and a
// stub answering every request with an empty stream would turn the replay into
// a mystery (Rule 15 - refuse by name). The marker lives on the function doc
// comment because the gate reports a panic at its enclosing FuncDecl line.
func NewStub(rec *Recording) *Stub {
	if rec == nil || len(rec.Responses) == 0 {
		// Rule 15: a stub with no script would answer every request with an
		// empty stream and turn the replay into a mystery. Refused by name.
		panic("replaydrive: NewStub needs a recording with at least one response") // panic-ok: a nil script is a programming error at the call site, not a runtime condition to degrade from
	}
	s := &Stub{rec: rec}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// URL is the stub's base URL. It is what the client's BaseURL is set to, and it
// is 127.0.0.1 on an ephemeral port (httptest's own listener): reachable from
// THIS machine by anything that learns the port, and unreachable from any other
// host — see the package doc for the wording this must (and must not) carry.
func (s *Stub) URL() string { return s.srv.URL }

// Close releases the listener. Outstanding handlers are allowed to finish
// (httptest's Close waits), so a Close during a live request does not truncate
// a stream the surface is reading.
func (s *Stub) Close() { s.srv.Close() }

// RequestCount is how many model requests the stub has served.
func (s *Stub) RequestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs
}

// OverlongRequests is how many requests were served by REPEATING the last
// response (see the Stub doc comment).
func (s *Stub) OverlongRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlong
}

// Bodies returns a copy of every request body served, in arrival order.
func (s *Stub) Bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// ServedResults returns every tool result the executor seam handed back, in
// order.
func (s *Stub) ServedResults() []ServedResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ServedResult(nil), s.served...)
}

// MissingResults returns the tool_use ids the recording held no result for.
func (s *Stub) MissingResults() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.missingID...)
}

// handle serves one request: it records the body, picks the scripted response,
// and writes it as Claude SSE frames.
func (s *Stub) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body) //stylegate:ignore a diagnostic read of the request we are about to script; a short read costs the recorded body, not the answer
	s.mu.Lock()
	s.reqs++
	idx := s.reqs - 1
	s.bodies = append(s.bodies, string(body))
	over := false
	if idx >= len(s.rec.Responses) {
		over = true
		idx = len(s.rec.Responses) - 1
		s.overlong++
	}
	resp := s.rec.Responses[idx]
	if over {
		// A REQUEST BEYOND THE SCRIPT IS ANSWERED WITH THE LAST RECORDED ANSWER'S
		// TEXT AND NOTHING ELSE, and the reason is measured rather than
		// stylistic: re-serving a TOOL-BEARING response verbatim hands the agent
		// the same calls again, so it executes them, comes back, and is handed
		// them a third time — the 40-round loop guard is what ended the replay4
		// run. Keeping the recorded TEXT keeps the tail truthful (it is the
		// recording's own closing words, not an invention) while removing the
		// one thing that cannot be replayed: a call whose result the script no
		// longer holds. What is served this way is counted (OverlongRequests)
		// and announced on stderr, so no run mistakes the tail for the script.
		resp = closingSteps(resp)
	}
	s.mu.Unlock()

	if over {
		notice := s.noticeWriter()
		fmt.Fprintf(notice, "[gopi replay] the script is exhausted: request %d is served as the tail (the last recorded answer's text, no tool calls)\n", idx+2)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"replay-1","usage":{"input_tokens":0,"output_tokens":0}}}`)
	flush(w)
	block := 0
	for _, step := range resp {
		switch step.Kind {
		case blockTypeText:
			writeTextBlock(w, block, step.Text)
			block++
		case blockTypeToolUse:
			writeToolUseBlock(w, block, *step.Tool)
			block++
		}
	}
	stop := "end_turn"
	if len(resp) > 0 && resp[len(resp)-1].Kind == blockTypeToolUse {
		stop = "tool_use"
	}
	writeClaudeSSE(w, "message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`, stop))
	writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	flush(w)
}

// closingSteps is the tail a request beyond the script is served with: the last
// recorded response's TEXT blocks, and the run's own sentence when that response
// carried none (a response that was only a tool call). The sentence is this
// package's, and it says what happened — a fabricated ANSWER would be worse than
// a named absence (Rule 15).
func closingSteps(last []Step) []Step {
	var text []Step
	for _, st := range last {
		if st.Kind == blockTypeText {
			text = append(text, st)
		}
	}
	if len(text) > 0 {
		return text
	}
	return []Step{{Kind: blockTypeText, Text: tailAnswerNotice}}
}

// tailAnswerNotice is the sentence served when the script is exhausted AND its
// last recorded response carried no text — it names the boundary instead of
// inventing an answer over it.
const tailAnswerNotice = "[the recording ends here — gopi replay has no further scripted answer for this turn]"

// Executor is the function agent.SetToolExecutor takes: it answers each
// recorded tool call with the RECORDED result and never dispatches anything.
//
// The returned strings are the recording's, VERBATIM — including the
// "<tick> [tool_use id: …]" header the live run stamped on them. Re-stamping
// would look more honest ("this is a fresh run") and would be exactly wrong:
// the replay's claim is that the model receives the same bytes the recorded run
// received, and a claim that holds only after post-processing holds for nothing.
func (s *Stub) Executor() func(tc deepseek.ToolCall) string {
	return func(tc deepseek.ToolCall) string {
		result, ok := s.rec.Results[tc.ID]
		s.mu.Lock()
		defer s.mu.Unlock()
		if !ok {
			s.missingID = append(s.missingID, tc.ID)
			msg := fmt.Sprintf("[gopi replay] no recorded tool_result for %s (%s) — the recording's tool result map has no such id", tc.ID, tc.Name)
			s.served = append(s.served, ServedResult{ID: tc.ID, Name: tc.Name, Result: msg})
			return msg
		}
		s.served = append(s.served, ServedResult{ID: tc.ID, Name: tc.Name, Result: result, Recorded: true})
		return result
	}
}

// --- SSE frame writers (the shape internal/testutil/mockllm.go uses) ---

// writeClaudeSSE writes one SSE frame: an event type and its JSON payload.
func writeClaudeSSE(w io.Writer, eventType, payload string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
}

// writeTextBlock streams one text block in two fragments. Two — not one — so
// the surface's streaming renderer is exercised the way the recording exercised
// it (the live provider streams; a single frame would paint the answer only
// when it is already whole, which is a different screen).
func writeTextBlock(w io.Writer, index int, text string) {
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, index))
	mid := len(text) / 2
	for _, frag := range []string{text[:mid], text[mid:]} {
		if frag == "" {
			continue
		}
		j, _ := json.Marshal(frag) //stylegate:ignore marshalling a string cannot fail
		writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, index, j))
		flush(w)
	}
	writeClaudeSSE(w, "content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
}

// writeToolUseBlock streams one tool_use block: the block opens with an empty
// input, then the recorded arguments arrive as one input_json_delta carrying
// the JSON as a STRING (the loop accumulates the fragments and parses the
// combined text at content_block_stop — a bare object there reads as no input
// at all, which is the defect internal/testutil/mockllm.go's ToolUseSSE
// comment records).
func writeToolUseBlock(w io.Writer, index int, tc ToolCall) {
	args := tc.Input
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	partial, _ := json.Marshal(string(args)) //stylegate:ignore marshalling a string cannot fail
	idJSON, _ := json.Marshal(tc.ID)         //stylegate:ignore marshalling a string cannot fail
	nameJSON, _ := json.Marshal(tc.Name)     //stylegate:ignore marshalling a string cannot fail
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%s,"name":%s,"input":{}}}`, index, idJSON, nameJSON))
	flush(w)
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, index, partial))
	writeClaudeSSE(w, "content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
}

// flush pushes a frame to the wire when the writer can: the surface must see a
// stream (a partial paint is one of the things a replay exists to film), and a
// buffered response would deliver the whole answer in one read.
func flush(w io.Writer) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
