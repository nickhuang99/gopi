// The counting and pairing half of `gopi tool tokens`.
//
// Two jobs live here, and they are separate on purpose:
//
//  1. TURN A REQUEST INTO A PROMPT STRING. The endpoint renders the official
//     DeepSeek chat template (2566 chars, in tokenizer_config.json) over the
//     messages; our renderer must produce the same BYTES or the count is
//     meaningless. A rendering that is merely close is a wrong answer.
//  2. PAIR A REQUEST WITH ITS OWN RESPONSE inside one raw log — by position,
//     never by ordinal against a second file. Measured failure that produced
//     this rule: pairing raw.log bodies against session.jsonl rows (3,234 vs
//     2,892 entries) misaligned from the second request on and reported a 17%
//     gap that did not exist.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/rawlogfmt"
	"github.com/nick/gopi/internal/thinkmode"
	"github.com/nick/gopi/internal/tokenizer"
)

// markerScanWindow bounds how many RECORDS past a request the matching usage
// block is hunted for. A response that has not been found by then is treated as
// absent rather than searched for across the rest of the log, which would pair
// the wrong response with the wrong request.
//
// It is counted in records, not in bytes, because the pairing walks records
// (internal/rawlogfmt) rather than scanning text: the two framings a raw log can
// be written in spell the same event differently — `→ REQUEST` on a legacy marker
// line, `R 412` on a length-prefixed header — and a pairing that tested for the
// marker TEXT found nothing at all in every log the current writer produces
// (measured: `requests: 0` on a 592 KB framed log carrying three request
// records). The direction now comes from the record's TAG, which both framings
// answer, so one walk serves both and no literal from either grammar appears
// here.
const markerScanWindow = 2000

// paired is one request and the prompt total the server reported for it.
type paired struct {
	res    tokenRow
	server int
	body   []byte
}

// countRequest counts one request body by rendering it through the official
// V4.1 chat template (internal/tokenizer) and encoding the result.
//
// NO ADDITIVE TERM APPEARS HERE, on purpose. The version this replaces added
// `requestFramingOverhead` (28), `toolsSectionOverhead` (200) when the request
// carried tools, and a `markerCostRemainder` computed from a table of per-marker
// costs. All three were fitted against this endpoint, and a fit is a statement
// about the sample it was fitted on: the marker table charged 12 tokens for
// `<｜begin▁of▁sentence｜>` appearing in CONTENT (measured 12, correct) while the
// SAME marker emitted by the template costs 1 — so the same table had to be
// scoped by hand to content, and the two framing constants then absorbed
// whatever the render itself got wrong. The render is what is fixed here; what
// remains is reported by `--compare`, not added.
func countRequest(b *requestBody) (int, error) {
	tk, err := tokenizer.Default()
	if err != nil {
		return 0, err
	}
	schemas, err := toolList(b.Tools)
	if err != nil {
		return 0, err
	}
	msgs, err := tokenizerMessages(b)
	if err != nil {
		return 0, err
	}
	n, err := tk.PromptTokensWith(systemText(b.System), msgs, schemas, promptOptions(b))
	if err != nil {
		// The mask the count is built from is derived per byte from the render,
		// so a failure here means the renderer and the mask builder disagree —
		// a real defect the operator must see, surfaced as an error the caller
		// already handles (PROJECT.md Rule 15, never a silent zero).
		return 0, fmt.Errorf("counting the request prompt: %w", err)
	}
	return n, nil
}

// promptOptions reads the request-level switches the template branches on.
//
// THINKING IS THE ENDPOINT'S DEFAULT, not chat: a body with no `thinking` field
// is counted by the live endpoint in thinking mode (measured: user "hi" counts
// 31 with no field, 5 with `{"type":"disabled"}`). Only an explicit `disabled`
// selects chat mode.
func promptOptions(b *requestBody) tokenizer.PromptOptions {
	opts := tokenizer.PromptOptions{
		Thinking: b.Thinking.Type != thinkmode.Disabled,
		Effort:   tokenizer.DefaultEffort,
	}
	if len(b.ReasoningEffort) > 0 {
		var v any
		dec := json.NewDecoder(bytes.NewReader(b.ReasoningEffort))
		dec.UseNumber()
		if dec.Decode(&v) == nil {
			if n, ok := v.(json.Number); ok {
				if i, err := n.Int64(); err == nil {
					v = int(i)
				}
			}
			opts.Effort = tokenizer.EffortBudget(v)
		}
	}
	return opts
}

// tokenizerMessages maps Anthropic message shapes onto the template's: a
// tool_result block lives inside a `user` message here and is a `tool_result`
// content block there, an assistant's `tool_use` block is the template's
// `tool_calls`, and a plain string content stays a string.
//
// The mapping is NOT cosmetic: the template renders a tool_result through the
// user-turn block join while an assistant's call renders through the DSML
// branch, and swapping them moves thousands of tokens.
func tokenizerMessages(b *requestBody) ([]tokenizer.Message, error) {
	out := make([]tokenizer.Message, 0, len(b.Messages))
	for _, raw := range b.Messages {
		var m struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("message: %w", err)
		}
		var plain string
		if json.Unmarshal(m.Content, &plain) == nil {
			out = append(out, tokenizer.Message{Role: m.Role, Text: plain})
			continue
		}
		var blocks []contentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			return nil, fmt.Errorf("message blocks: %w", err)
		}
		msg := tokenizer.Message{Role: m.Role}
		for _, bl := range blocks {
			switch bl.Type {
			case blockTypeText:
				msg.Blocks = append(msg.Blocks, tokenizer.Block{Type: tokenizer.BlockText, Text: bl.Text})
			case blockTypeThinking:
				// The V4.1 template reads the assistant's reasoning from
				// `thinking`/`reasoning_content` and renders it in thinking
				// mode; dropping it here is the same defect as dropping text.
				msg.Reasoning += bl.Thinking
			case blockTypeToolUse:
				args, err := compactValueJSON(bl.Input)
				if err != nil {
					return nil, fmt.Errorf("tool_use %q: %w", bl.Name, err)
				}
				msg.Blocks = append(msg.Blocks, tokenizer.Block{
					Type: tokenizer.BlockToolUse, Name: bl.Name, ID: bl.ID, Input: args,
				})
			case blockTypeToolResult:
				msg.Blocks = append(msg.Blocks, tokenizer.Block{
					Type: tokenizer.BlockToolResult, ID: bl.ToolUseID, Content: flattenContent(bl.Content),
				})
			}
		}
		out = append(out, msg)
	}
	return out, nil
}

// contentBlock is the subset of an Anthropic content block the template reads.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Content   json.RawMessage `json:"content"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
}

// The content-block type discriminators the template maps.
const (
	blockTypeText       = "text"
	blockTypeThinking   = "thinking"
	blockTypeToolUse    = "tool_use"
	blockTypeToolResult = "tool_result"
)

// toolList decodes the `tools` field into its entries, keeping each entry's
// bytes so it can be re-serialized compact.
func toolList(raw json.RawMessage) ([]json.RawMessage, error) {
	// An ABSENT `tools` field is not an error and not a `[]`: the template's
	// `{% if tools %}` is false for both, so both render no section at all.
	if len(raw) == 0 {
		return nil, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("tools: %w", err)
	}
	return list, nil
}

// compactValueJSON re-serializes one value without insignificant whitespace.
func compactValueJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// systemText flattens the `system` field, which arrives either as a string or
// as a list of text blocks.
func systemText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, bl := range blocks {
		if bl.Type == "text" {
			parts = append(parts, bl.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// flattenContent reads a tool_result's payload, which may be a string or a
// list of content blocks. The template joins the list's TEXT parts with a
// blank line and renders any other block type as `[Unsupported <type>]`, so
// this does the same: joining with "" would drop the separators the endpoint
// counts.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, bl := range blocks {
		switch bl.Type {
		case blockTypeText, "":
			parts = append(parts, bl.Text)
		default:
			parts = append(parts, "[Unsupported "+bl.Type+"]")
		}
	}
	return strings.Join(parts, "\n\n")
}

// toolCount reports how many tool definitions a request carries, which is the
// variable this tool exists to chase next.
func toolCount(b *requestBody) int {
	if len(b.Tools) == 0 {
		return 0
	}
	var list []json.RawMessage
	if json.Unmarshal(b.Tools, &list) != nil {
		return 0
	}
	return len(list)
}

// pairRawLog walks a raw HTTP log once, pairing each request with the usage of
// its OWN response.
//
// IT WALKS RECORDS, NOT LINES, and that is what makes it work on both framings a
// raw log is ever written in (internal/rawlogfmt's package doc): the direction of
// a record comes from its TAG (`R 412\n<payload>\n` framed, `→ REQUEST` legacy),
// so no marker literal appears here and neither grammar can drift away from this
// reader. The alternative — testing each line for the legacy marker text — is the
// defect this function was rewritten to remove: on a length-prefixed log it
// matched nothing, so the report said `requests: 0` for a log holding three
// request bodies.
//
// The pairing is still BY POSITION inside one file: a request is followed by the
// records of its own response, and the FIRST usage-bearing record found before the
// next request supplies the server's number.
func pairRawLog(path string) ([]paired, error) {
	r, err := rawlogfmt.NewReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var out []paired
	var pending []byte
	var pendingLine int
	scanned := 0
	for {
		rec, rerr := r.Next()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			// A truncated tail (a killed run) is reported WITH whatever paired
			// before it: runRaw keeps the rows and says so on stderr, because a
			// partial answer plus a warning beats a silent empty one (Rule 15).
			return out, rerr
		}
		switch rec.Tag {
		case rawlogfmt.TagRequest, rawlogfmt.TagProbeRequest:
			// A request OPENS a pair. The body is the record's payload; a request
			// with no following usage is reported with no server number rather
			// than paired against a later response.
			pending, pendingLine, scanned = rec.Payload, rec.Line, 0
		default:
			if pending == nil {
				continue
			}
			scanned++
			if scanned > markerScanWindow {
				// No usage inside the window: treat the response as absent instead
				// of pairing it with some later request's answer.
				pending, pendingLine = nil, 0
				continue
			}
			u, ok := usageInPayload(rec.Payload)
			if !ok {
				continue
			}
			var b requestBody
			if json.Unmarshal(pending, &b) != nil {
				pending, pendingLine, scanned = nil, 0, 0
				continue
			}
			// A body that cannot be rendered is reported rather than printed as a
			// silent `ours 0`: a zero in that column reads as a MEASUREMENT, and
			// this tool exists to compare measurements against the server's. The
			// rows that DID pair are returned with the failure (the same shape the
			// truncated-tail arm below uses), so runRaw still prints them and says
			// why on stderr (Rule 15).
			ours, cerr := countRequest(&b)
			if cerr != nil {
				return out, fmt.Errorf("count request at line %d: %w", pendingLine, cerr)
			}
			out = append(out, paired{
				res: tokenRow{line: pendingLine, msgs: len(b.Messages),
					tools: toolCount(&b), ours: ours, server: u},
				server: u,
				body:   pending,
			})
			pending, pendingLine, scanned = nil, 0, 0
		}
	}
	return out, nil
}

// usageInPayload extracts a prompt-side total from ONE record's payload: a
// non-streamed response body, or a streamed chunk that may carry several SSE
// events. It reads the payload LINE BY LINE because one streamed chunk can carry
// more than one `data:` event, and the usage block rides whichever event has it.
func usageInPayload(payload []byte) (int, bool) {
	for _, line := range strings.Split(string(payload), "\n") {
		if u, ok := usageInLine(line); ok {
			return u, true
		}
	}
	return 0, false
}

// usageInLine extracts a prompt-side total from one response line, if that
// line carries a usage block. Both the buffered JSON form and the SSE
// message_start form are accepted, because the log holds both.
func usageInLine(line string) (int, bool) {
	if !strings.Contains(line, "usage") || !strings.Contains(line, "input_tokens") {
		return 0, false
	}
	start := strings.Index(line, `{"id"`)
	if k := strings.Index(line, `{"type"`); k >= 0 && (start < 0 || k < start) {
		start = k
	}
	if start < 0 {
		return 0, false
	}
	var obj struct {
		Usage   *promptUsage `json:"usage"`
		Message *struct {
			Usage *promptUsage `json:"usage"`
		} `json:"message"`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(line[start:])))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return 0, false
	}
	if obj.Usage != nil {
		return obj.Usage.prompt(), true
	}
	if obj.Message != nil && obj.Message.Usage != nil {
		return obj.Message.Usage.prompt(), true
	}
	return 0, false
}

// printHeader is the report's column title.
func printHeader() {
	fmt.Printf("%-8s %-6s %-7s %-8s %-9s %s\n",
		"line", "msgs", "tools", "ours", "server", "diff")
}

// printRow is one line of the report. A diff is printed as a signed integer so
// a gap of zero is visibly zero rather than blank.
func printRow(r tokenRow) {
	if r.server == 0 {
		fmt.Printf("%-8d %-6d %-7d %-8d %-9s %s\n",
			r.line, r.msgs, r.tools, r.ours, "-", "(no server number)")
		return
	}
	fmt.Printf("%-8d %-6d %-7d %-8d %-9d %+d\n",
		r.line, r.msgs, r.tools, r.ours, r.server, r.ours-r.server)
}

// countServer asks the endpoint what IT counts for this body.
//
// This is the ground truth the tool exists to measure against: the server's
// own arithmetic on the same bytes, not a second local guess.
func countServer(body []byte) (int, error) {
	u := os.Getenv(config.TokensCountURLEnv)
	if u == "" {
		return 0, fmt.Errorf("%s not set", config.TokensCountURLEnv)
	}
	key := os.Getenv(config.TokensCountKeyEnv)
	req, err := httpRequest(u, body, key)
	if err != nil {
		return 0, err
	}
	resp, err := httpDo(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	var out struct {
		InputTokens int `json:"input_tokens"`
		Error       *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &out) != nil {
		return 0, fmt.Errorf("unreadable response: %.120s", data)
	}
	if out.Error != nil {
		return 0, fmt.Errorf("%s", out.Error.Message)
	}
	return out.InputTokens, nil
}

// runBody counts ONE request body and, under --compare, asks the server.
func runBody(path string, compare bool) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: "+err.Error())
		return tokensExitError
	}
	var b requestBody
	if err := json.Unmarshal(raw, &b); err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: not a request body: "+err.Error())
		return tokensExitError
	}
	ours, err := countRequest(&b)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: "+err.Error())
		return tokensExitError
	}
	printHeader()
	row := tokenRow{line: 1, msgs: len(b.Messages), tools: toolCount(&b), ours: ours}
	if compare {
		server, cerr := countServer(raw)
		if cerr != nil {
			printRow(row)
			fmt.Fprintf(os.Stderr, "gopi tool tokens: server: %v\n", cerr)
			return tokensExitError
		}
		row.server = server
	}
	printRow(row)
	return 0
}

// runRaw walks a raw HTTP log and reports every request against the usage block
// of the response that follows it.
func runRaw(path string, limit int) int {
	reqs, err := pairRawLog(path)
	if err != nil {
		// PARTIAL RESULTS ARE STILL RESULTS: a log killed mid-write yields the
		// pairs that did complete, and printing them with the reason on stderr is
		// strictly more useful than printing nothing (Rule 15 — the failure is
		// said out loud, never swallowed).
		fmt.Fprintln(os.Stderr, "gopi tool tokens: "+err.Error())
	}
	printHeader()
	var examined, matched, exact int
	for _, r := range reqs {
		if limit > 0 && examined >= limit {
			break
		}
		examined++
		if r.server > 0 {
			matched++
			if r.res.ours == r.server {
				exact++
			}
		}
		printRow(r.res)
	}
	fmt.Printf("\nrequests: %d, with a server number: %d, exactly equal: %d\n",
		examined, matched, exact)
	if err != nil {
		return tokensExitError
	}
	return 0
}

// tokensExitUsage / tokensExitError are the tool's two failure statuses: a
// misuse (2) and a run that could not answer (1) — the POSIX classes the other
// `gopi tool` verbs use.
const (
	tokensExitUsage = 2
	tokensExitError = 1
)

// sscanfInt parses a decimal integer without pulling fmt.Sscanf's error
// vocabulary into the caller.
func sscanfInt(s string, out *int) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*out = n
	return nil
}

// tokensUsage is the tool's own page. It states ALL FOUR input shapes, in the
// order a reader meets them: the two a PERSON holds (a text file, a session id)
// first, then the two this repo uses to verify its own template against the
// server — which is what the tool was built for and is now one shape among
// four, not the only door.
var tokensUsage = `GoPi tool tokens — count tokens, and put our count beside the server's

Usage:
  gopi tool tokens --file <path>
        Count ANY text file with the embedded DeepSeek-V4.1 BPE tokenizer — the
        same encoder and the same vocabulary the model is billed on. Prints the
        count with the file's bytes and lines, and names the path it read.

  gopi tool tokens --session <session-id>
        That session's verification results, from the raw HTTP log it already
        wrote: per request, what our chat-template render counted (ours) beside
        what the server reported for the same bytes (server). The pairing is
        BY POSITION inside one file: a request is matched to the FIRST usage
        block found in the window of records after it, and a response that
        reports no usage inside that window is counted as "(no server number)"
        rather than searched for further on — so a cut or retried stream shows
        FEWER compared rows, never a wrongly paired one. The window is
        ` + strconv.Itoa(markerScanWindow) + ` records (markerScanWindow). Reads only — nothing
        is sent anywhere. An unknown id, or a session with no raw log, is
        refused by name.

  gopi tool tokens --raw <raw.log> [--limit N]
        The same pairing over a raw log named directly rather than through a
        session id.

  gopi tool tokens --body <request.json> [--compare]
        One request body. --compare asks the server (count_tokens) for its own
        number instead of reading one out of a log; it needs
        GOPI_TOKENS_COUNT_URL (and GOPI_TOKENS_COUNT_KEY for the key).

Counting is internal/tokenizer's own; the comparison is against what the server
reported, never against a second local implementation.`
