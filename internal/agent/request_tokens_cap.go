package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/thinkmode"
	"github.com/nickhuang99/gopi/internal/tokenizer"
)

// The SEND-TIME token ceiling: the last thing that happens to a request before
// it leaves the process is a count, and a request that does not fit is not
// sent.
//
// WHY THIS EXISTS AT ALL (the shape it closes, measured): every other size
// control in this package is a TRIGGER — trimContext fires on
// EstimatedTokens(), the fold fires on the same number, compaction fires on the
// same number. A trigger answers "should I shrink this?", never "may this go on
// the wire?" — so a single tool_result large enough to blow the window BETWEEN
// two rounds is assembled, sent, and charged for before anything notices, and the
// round is then thrown away. The gate below is the only place that asks the
// second question.
//
// THE TWO BASES HAVE CONVERGED, AND THE TWO QUESTIONS HAVE NOT. This gate asks
// the endpoint's own question through the official V4.1 template (request tokens,
// markers and all); the trigger asks "is the conversation too big to keep?".
// EstimatedTokens() was moved onto the embedded tokenizer in the same change that
// touched this file's neighbours, so a trigger no longer under-counts Chinese
// text — but it remains a different arithmetic from this one (a message-level
// count plus a recorded-measurement prefix), and a reader must not fold them
// together just because both say "tokenizer" now.

// RequestTokensCap is the ceiling on a request's prompt-side token count: a
// request counted above it is NOT sent.
//
// 200000 is the model's context window, and it is the number the ceiling is
// about: past it the provider refuses the request anyway, so sending it buys
// nothing but a wasted round trip and an opaque provider-side error.
const RequestTokensCap = 200000

// ErrRequestTokensOverCap is the sentinel a caller matches with errors.Is to
// tell "the prompt does not fit" apart from any other send failure.
var ErrRequestTokensOverCap = errors.New("request over the token cap")

// RequestTokenCapEnv overrides RequestTokensCap. It is read in exactly ONE
// place — requestTokensCap (below) — so the value that gates a send can never
// be decided by two different readers disagreeing (PROJECT.md Rule 2).
const RequestTokenCapEnv = "GOPI_REQUEST_TOKEN_CAP"

// requestTokensCap returns the ceiling in force: RequestTokensCap unless
// RequestTokenCapEnv names a positive integer. A value that is not a positive
// integer is IGNORED (the default stands) rather than turning the gate off —
// an unreadable ceiling must not silently mean "no ceiling" (Rule 15).
func requestTokensCap() int {
	v := os.Getenv(RequestTokenCapEnv)
	if v == "" {
		return RequestTokensCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return RequestTokensCap
	}
	return n
}

// requestTokenExceeded reports the ONE decision this file makes: may a request
// whose prompt counts `counted` tokens be sent under `cap`? It is a named
// predicate rather than an inline comparison so the retry loop in chatOnce and
// the regression tests ask the same question (Rule 14).
func requestTokenExceeded(counted, cap int) bool { return counted > cap }

// RequestTokensOverCapError is the failure the gate reports. It names BOTH
// numbers — the count and the ceiling — because a refusal whose reader cannot
// tell "how far over" cannot act on it (Rule 15: never a silent or a vague
// failure), and it embeds ErrRequestTokensOverCap so a caller can classify it
// without matching prose.
type RequestTokensOverCapError struct {
	Counted int
	Cap     int
}

func (e *RequestTokensOverCapError) Error() string {
	return fmt.Sprintf("request not sent: counted=%d cap=%d — the prompt is over the send-time token ceiling", e.Counted, e.Cap)
}

// Unwrap makes errors.Is(err, ErrRequestTokensOverCap) answer for the typed
// error as well as for a bare sentinel.
func (e *RequestTokensOverCapError) Unwrap() error { return ErrRequestTokensOverCap }

// RequestTokenCount is the reading the gate takes and reports: the exact
// prompt-side token count of the request as it would go on the wire, the
// ceiling in force at the moment it was taken, and whether the two forbid the
// send.
type RequestTokenCount struct {
	Counted int
	Cap     int
	Over    bool
}

// countRequestTokens counts one built request the way the endpoint counts it:
// the system blocks, every message's content / tool_use input / tool_result
// payload, and the `tools` array, rendered through the official V4.1 chat
// template and encoded with the embedded BPE vocabulary
// (tokenizer.PromptTokensWith). The count is a MEASUREMENT, not an estimate.
//
// A counting FAILURE is an error, never a zero: a request that cannot be
// counted has not been shown to fit (Rule 15).
func countRequestTokens(req *deepseek.ClaudeRequest) (int, error) {
	if req == nil {
		return 0, fmt.Errorf("counting request tokens: nil request")
	}
	tk, err := tokenizer.Default()
	if err != nil {
		return 0, fmt.Errorf("counting request tokens: %w", err)
	}
	schemas, err := requestToolSchemas(req.Tools)
	if err != nil {
		return 0, err
	}
	msgs, err := requestTokenizerMessages(req.Messages)
	if err != nil {
		return 0, err
	}
	n, err := tk.PromptTokensWith(requestSystemText(req.System), msgs, schemas, requestPromptOptions(req))
	if err != nil {
		return 0, fmt.Errorf("counting request tokens: %w", err)
	}
	return n, nil
}

// requestSystemText joins the request's system blocks the way the template
// renders them: one system message whose parts are separated by a blank line.
func requestSystemText(blocks []deepseek.ClaudeSystemBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	out := ""
	for _, b := range blocks {
		if b.Type != "text" || b.Text == "" {
			continue
		}
		if out != "" {
			out += "\n\n"
		}
		out += b.Text
	}
	return out
}

// requestToolSchemas marshals each tool definition to the JSON the template
// renders. The definition is marshalled WITHOUT its cache marker: the marker
// is a cache-control hint this client attaches, not content the model reads,
// and counting it would price bytes the template never renders.
func requestToolSchemas(defs []deepseek.ClaudeToolDef) ([]json.RawMessage, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(defs))
	for _, def := range defs {
		plain := def
		plain.CacheControl = nil
		b, err := json.Marshal(plain)
		if err != nil {
			return nil, fmt.Errorf("counting request tokens: tool %q: %w", def.Name, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// requestTokenizerMessages maps the wire's message blocks onto the template's
// model: text stays text, an assistant's tool_use block becomes the template's
// call, a tool_result block keeps its payload, and a thinking block is the
// assistant's reasoning (the template renders it while thinking is on).
func requestTokenizerMessages(msgs []deepseek.ClaudeMessage) ([]tokenizer.Message, error) {
	out := make([]tokenizer.Message, 0, len(msgs))
	for _, m := range msgs {
		msg := tokenizer.Message{Role: m.Role}
		for _, bl := range m.Content {
			switch bl.Type {
			case "text":
				msg.Blocks = append(msg.Blocks, tokenizer.Block{Type: tokenizer.BlockText, Text: bl.Text})
			case "thinking":
				msg.Reasoning += bl.Thinking
			case "tool_use":
				args, err := json.Marshal(bl.Input)
				if err != nil {
					return nil, fmt.Errorf("counting request tokens: tool_use %q: %w", bl.Name, err)
				}
				msg.Blocks = append(msg.Blocks, tokenizer.Block{
					Type: tokenizer.BlockToolUse, Name: bl.Name, ID: bl.ID, Input: string(args),
				})
			case "tool_result":
				msg.Blocks = append(msg.Blocks, tokenizer.Block{
					Type: tokenizer.BlockToolResult, ID: bl.ToolUseID, Content: requestToolResultText(bl.Content),
				})
			}
		}
		out = append(out, msg)
	}
	return out, nil
}

// toolResultText flattens a tool_result payload, which is either a string or a
// list of content blocks; a list's text parts are joined with a blank line,
// which is what the template renders between them.
func requestToolResultText(v interface{}) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(b, &blocks) != nil {
		return string(b)
	}
	out := ""
	for _, bl := range blocks {
		switch bl.Type {
		case "text", "":
			if out != "" {
				out += "\n\n"
			}
			out += bl.Text
		default:
			if out != "" {
				out += "\n\n"
			}
			out += "[Unsupported " + bl.Type + "]"
		}
	}
	return out
}

// requestPromptOptions derives the template's request-level switches. Thinking
// is ON unless the request explicitly disables it — the endpoint's own default
// (measured: a body with no `thinking` field is counted in thinking mode).
func requestPromptOptions(req *deepseek.ClaudeRequest) tokenizer.PromptOptions {
	opts := tokenizer.PromptOptions{Effort: tokenizer.DefaultEffort}
	if req.Thinking == nil || req.Thinking.Type != thinkmode.Disabled {
		opts.Thinking = true
	}
	if req.ReasoningEffort != "" {
		opts.Effort = tokenizer.EffortBudget(req.ReasoningEffort)
	}
	return opts
}

// checkRequestTokens is what the send path calls before every attempt: it
// counts the request, compares the count against the ceiling in force, and
// returns the reading. ON A READ IT IS PURE — it sends nothing.
func (a *Agent) checkRequestTokens(req *deepseek.ClaudeRequest) (RequestTokenCount, error) {
	cap := requestTokensCap()
	counted, err := countRequestTokens(req)
	if err != nil {
		return RequestTokenCount{Cap: cap}, err
	}
	return RequestTokenCount{Counted: counted, Cap: cap, Over: requestTokenExceeded(counted, cap)}, nil
}

// requestBuilder builds the request one round would send. It is a function,
// not a value, because the gate may have to SHRINK the context and re-render:
// a request built before compaction describes a context that no longer exists.
type requestBuilder func() *deepseek.ClaudeRequest

// requestTokenCapRetries is how many times the gate re-renders and re-counts
// after shrinking the context before it refuses the send. ONE retry: the
// shrink path is the existing deterministic fold + compaction, and running it
// twice spends another summarizer round-trip to shrink what it already tried
// to shrink.
const requestTokenCapRetries = 1

// chatOnceGated is THE send path: every round of the task loop, the chat REPL
// and the chat-once turn goes through it, so a request that does not fit is
// refused wherever it was assembled from. It builds, counts, and — only when
// the count fits — sends.
//
// The retry chain, in order (and it is deliberately not longer):
//  1. count the request the caller's builder produces;
//  2. fits → send it (chatOnce: streaming, fallback, thinking budget);
//  3. over → shrink the context with the machinery that already exists
//     (fold old tool results, then LLM compaction, then the deterministic trim)
//     and re-render the request through the SAME builder;
//  4. still over → return *RequestTokensOverCapError naming counted and cap.
//
// NOTHING IS EVER SILENTLY DROPPED: every message count that leaves here either
// went on the wire or came back as a numbered error (Rule 15).
func (a *Agent) chatOnceGated(ctx context.Context, build requestBuilder, iter int) (*chatResult, error) {
	var last RequestTokenCount
	for attempt := 0; ; attempt++ {
		req := build()
		tc, err := a.checkRequestTokens(req)
		if err != nil {
			// A request that could not be COUNTED has not been shown to fit.
			// Refusing it is the only answer consistent with the gate's purpose
			// (and a counting failure is a real defect the operator must see).
			return nil, err
		}
		last = tc
		a.recordRequestTokens(tc)
		// A FITTING reading is NOT printed: the gate runs on every round of
		// every chat, and a line per round saying "this request fits" is noise
		// that buries the rounds where something actually happened. The reading
		// is not lost — recordRequestTokens above keeps it on the Context
		// (LastRequestTokenCount) and the status row renders it — and the
		// OVER path still prints its ⛔ line below and still returns a numbered
		// *RequestTokensOverCapError (Rule 15: no silent failure).
		if !tc.Over {
			// The crash dump's "last request" is the one that was actually
			// SENT — recorded here, where the send happens, so a request
			// re-rendered after a shrink is the one on record.
			a.lastClaudeRequest = req
			return a.chatOnce(ctx, req, iter)
		}
		// Remember the request the gate REFUSED, so a crash dump / digest
		// describes the bytes that were blocked rather than the ones a previous
		// round sent.
		a.lastClaudeRequest = req
		if attempt >= requestTokenCapRetries {
			break
		}
		if a.Logger != nil {
			a.Logger.Printf("⛔ request over the send-time cap (counted=%d cap=%d) — NOT sent; shrinking the context and retrying once", tc.Counted, tc.Cap)
		}
		if !a.shrinkForRequestCap(ctx, tc) {
			// The shrink path had nothing to work with: the context as it stands
			// is the smallest this run can send, and it still does not fit. The
			// last reading IS the number to report — recounting the identical
			// bytes would spend a full BPE pass to print the same integer.
			break
		}
	}
	return nil, &RequestTokensOverCapError{Counted: last.Counted, Cap: last.Cap}
}

// shrinkForRequestCap is the ONE shrink step the send-time gate asks for: the
// request it just counted did not fit RequestTokensCap, so the context is
// compressed before the retry. It reports whether anything changed — the retry
// loop stops when it did not, because recounting the identical bytes would spend
// a full BPE pass to print the same integer.
//
// IT CALLS THE SAME FUNCTION THE ROUND BOUNDARY CALLS (context_compress.go's
// compressContext). Two reduction policies used to meet here — a fold, an
// LLM compaction and a fallback trim, each with its own threshold — so "the
// request did not fit" and "the window is full" could shrink the conversation in
// two different shapes and leave two different transcripts for the same number
// of tokens. There is one shape now: delete the oldest run, carry the notes back.
//
// WHAT IT DOES NOT DO: it does not change EstimatedTokens' arithmetic, and it
// does not pick its own threshold — the send-time gate has already decided that
// SOMETHING must shrink, and the waterline is the one the threshold path uses.
func (a *Agent) shrinkForRequestCap(ctx context.Context, measured RequestTokenCount) bool {
	if a == nil || a.Ctx == nil || a.ContextWindow <= 0 {
		return false
	}
	// THE READING TRAVELS WITH THE DEMAND. The gate has just counted this request
	// and found it over the ceiling; that number is the only one the operator's
	// notice may quote, because it is the number the REFUSAL is made on. Passing it
	// (rather than letting the compression re-derive one) is what keeps the gate's
	// "counted=N cap=M" line and the compression notice from printing two different
	// token counts for one request at one instant.
	return a.compressContext(compressReasonSendCap, &measured) > 0
}

// recordRequestTokens keeps the last reading the gate took, so a status row or
// a report can compare what the gate counted against what the row shows
// without re-counting (and without a second basis inventing its own number).
func (a *Agent) recordRequestTokens(tc RequestTokenCount) {
	if a == nil || a.Ctx == nil {
		return
	}
	a.Ctx.requestTokens = tc
	a.Ctx.requestTokensSet = true
}

// LastRequestTokenCount returns the gate's most recent reading and whether one
// was taken at all.
func (c *Context) LastRequestTokenCount() (RequestTokenCount, bool) {
	if c == nil || !c.requestTokensSet {
		return RequestTokenCount{}, false
	}
	return c.requestTokens, true
}
