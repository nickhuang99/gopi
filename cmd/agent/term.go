package main

// term.go — the /cli ROUTES: a server-rendered page a TEXT-MODE browser can
// actually use, and ONE streaming response per turn.
//
// WHY THIS EXISTS AS A SECOND VIEW (epic: a zero-dependency terminal chat
// surface). `gopi web`'s page is JavaScript + Server-Sent Events end to end; a
// text browser runs no JS, so it renders 116 bytes of it (against a
// 41,611-byte document) and can neither read the transcript nor submit a turn
// — that page has no `<form action>`, no NAME on any field and no `<a href>`
// (measured 2026-09-17, devdoc/w3m-cli-feasibility.md). Nothing here reuses or
// modifies webChatPage: this file adds two routes over the SAME engine.
//
//	GET  /cli       → the transcript as plain <pre> text + a real form whose
//	                  only field is NAMED (`<input name="message">`)
//	POST /cli/chat  → ONE response per turn, a COMPLETE document: the queued
//	                  line (only when the turn had to wait for s.mu), then live
//	                  text deltas, then live compact tool-activity lines, then
//	                  the same tail GET /cli serves (form + bottom anchor)
//
// THE ENGINE IS SHARED, NOT COPIED. handleTermChat runs the discipline
// handleChat runs — the single persistent agent, s.mu (TryLock first, so a
// concurrent turn is TOLD it is queued instead of hanging byte-less), the
// server-held s.history (the request history is ignored: this carrier ships a
// form, not a blob), the slash-command table, and agent.OnToolEvents for tool
// activity. Only the WIRE differs, because only the wire can.
//
// WHY ONE BODY AND NOT SSE, AND WHY THE TURN CANNOT END IN A REDIRECT. A text
// browser paints a document as its bytes arrive and has no re-render primitive,
// so the deltas must BE the body. That choice settles the ending: the status
// line is sent with the first byte, so a 303 after the stream is not
// expressible — `http.Redirect` would return 200 and silently drop its
// Location (measured 2026-09-17 against this exact handler; the epic's original
// "terminal 303 → /cli#bottom" is not what an HTTP response can do once it has
// started). The turn therefore CLOSES ITS OWN DOCUMENT with the tail GET /cli
// serves, so the operator can type the next turn without navigating, and the
// page carries explicit links back to the settled view. GET /cli never
// redirects on its own either: a reload must not re-run the last turn.

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/tools"
)

// The /cli wire: routes, the field name and the transcript roles this renderer
// switches on. Named constants (Rule 1b) — the page's HTML, the form action,
// the redirect target and the renderer must all spell them identically.
const (
	// termChatPath is the chat surface (GET).
	termChatPath = "/cli"
	// termChatSubmitPath is the turn endpoint the form POSTs to.
	termChatSubmitPath = "/cli/chat"
	// termChatBottomAnchor is the in-page fragment the terminal redirect
	// targets, so the text browser lands on the transcript's END (a terminal
	// cannot scroll for you; the fragment is the only lever the wire has).
	termChatBottomAnchor = "bottom"
	// termChatInputName is the ONE field name. A text browser submits only
	// NAMED fields; this is the name the page writes and the server reads (the
	// cross-check lives in cli_term_test.go, which greps the served page for
	// it rather than trusting this sentence).
	termChatInputName = "message"
	// termRoleUser / termRoleAssistant / termRoleTool are the roles the
	// renderer switches on (the strings internal/agent writes into
	// deepseek.Message.Role).
	termRoleUser      = "user"
	termRoleAssistant = "assistant"
	termRoleTool      = "tool"
)

// termFlushInterval is the delta coalescing window: live text is flushed to
// the terminal at most once per interval while output is pending. A text
// browser re-lays-out the WHOLE document on every write, so a flush per model
// token would make a long answer quadratic in terminal work for no gain the
// eye can see. The per-turn write/flush counts are recorded (and asserted) —
// see devdoc/cli-links-carrier.md.
const termFlushInterval = 120 * time.Millisecond

// termMaxPromptRunes caps a submitted prompt. The field is one line; a tool
// result pasted into it is the realistic oversize case, and the limit is
// enforced with a message rather than truncation (Rule 15).
const termMaxPromptRunes = 8000

// The rendered vocabulary of the wire — the words the operator reads.
const (
	termUserPrefix      = "you> "
	termAssistantPrefix = "gopi> "
	// termToolPrefix is the tool row's glyph — the ONE definition of `⚙`, which
	// the render layer's own table reads back (lineedit.ToolGutter) so the row
	// here and the row the band commits cannot mark a call two different ways.
	termToolPrefix      = lineedit.ToolGlyph + " "
	termQueuedLine      = "[queued: the previous turn is still running]\n"
	termTurnFailedLine  = "[turn failed] "
	termEmptyAnswerLine = "[the model returned no text]\n"
	termRunningLine     = "[running…]\n"
)

// termEmptyTranscriptNote is the fresh-conversation page text: an empty screen
// reads as a broken page, so the surface says what it is.
const termEmptyTranscriptNote = "(new conversation — type a message below and press Enter)\n"

// termPageHead is the static document preamble: no script, no stylesheet, no
// external anything (a text browser fetches nothing it was not asked to). The
// `<pre>` holds the whole settled transcript; the turn endpoint appends to an
// equivalent pre so the browser appends instead of re-laying-out the page.
const termPageHead = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>gopi cli</title></head>
<body>
<pre>`

// termPageTail is the form, then the bottom anchor. The form carries the ONLY
// input this carrier accepts and NO submit button: a text browser submits a
// single-input form on Enter (measured for w3m: Tab then Enter; Links: Enter
// in the field), and a button would add a second way to fire the same POST
// without adding a capability.
const termPageTail = `</pre>
<form action="` + termChatSubmitPath + `" method="post">
<input type="text" name="` + termChatInputName + `" size="72" autocomplete="off">
</form>
<pre><a name="` + termChatBottomAnchor + `" id="` + termChatBottomAnchor + `"></a>` +
	`<a href="` + termChatPath + `#` + termChatBottomAnchor + `">[settled page]</a> ` +
	`<a href="` + termChatPath + `">[top]</a>` + `
</pre>
</body></html>
`

// termHTMLContentType is what every /cli response is served as. The turn's
// fragments are part of the same document, which is why the content type
// cannot be text/plain.
const termHTMLContentType = "text/html; charset=utf-8"

// handleTermPage renders the SETTLED transcript: plain text, nothing a text
// browser has to interpret, and the form at the END (where a reader finishes
// and types next).
func (s *webServer) handleTermPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed (GET "+termChatPath+")", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	msgs := append([]deepseek.Message{}, s.history...)
	s.mu.Unlock()

	w.Header().Set("Content-Type", termHTMLContentType)
	w.WriteHeader(http.StatusOK)
	body := termPageHead + html.EscapeString(termHistoryText(msgs)) + termPageTail
	if _, err := io.WriteString(w, body); err != nil {
		// A browser that went away mid-page is not fatal, but a silently
		// dropped page is indistinguishable from an empty transcript to the
		// operator — so it is recorded (Rule 15).
		fmt.Fprintf(os.Stderr, "gopi cli: page write failed: %v\n", err)
	}
}

// termHistoryText renders the server-held transcript as plain text.
//
// Three kinds of row are left out, each for a reason the operator would
// otherwise have to decode:
//
//   - TOOL rows (and assistant rows carrying only tool calls): their payload
//     already appeared as a compact activity line while the turn ran, and a raw
//     result is a JSON blob a text browser cannot collapse;
//   - HARNESS-INJECTED user rows — the framework's own continuations, which
//     open with its prefix. Rendering one under the operator's prefix would
//     attribute the FRAMEWORK's words to the operator. The predicate is the
//     engine's own (isHarnessInjectedUserMessage) rather than a prefix test
//     here, because the marker set belongs to the engine: measured in the live
//     acceptance run, where a tool-mode turn's end-turn continuation appeared on
//     the settled page as if the operator had typed it.
func termHistoryText(msgs []deepseek.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		body := strings.TrimRight(m.Content, "\n")
		if strings.TrimSpace(body) == "" {
			continue
		}
		switch m.Role {
		case termRoleUser:
			if agent.IsHarnessInjectedUserMessage(m) {
				continue // the framework's words are not the operator's
			}
			b.WriteString(termUserPrefix)
		case termRoleAssistant:
			b.WriteString(termAssistantPrefix)
		case termRoleTool:
			continue // the live activity line already carried the digest
		default:
			continue
		}
		b.WriteString(body)
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return termEmptyTranscriptNote
	}
	return b.String()
}

// handleTermChat serves ONE turn as one streaming HTML body, then redirects to
// the settled page. The engine calls are the ones handleChat makes; only the
// bytes differ.
func (s *webServer) handleTermChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST "+termChatSubmitPath+")", http.StatusMethodNotAllowed)
		return
	}
	// Shutdown interlock, same as the browser carrier: a turn queued behind
	// the finalizer could never stream out.
	if s.shuttingDown {
		http.Error(w, "gopi cli: server is shutting down", http.StatusServiceUnavailable)
		return
	}
	// The carrier accepts exactly the form it renders. Anything else (a
	// hand-built request with extra context) is refused instead of being
	// half-honoured.
	if strings.TrimSpace(r.Header.Get("Content-Type")) != termFormContentType {
		http.Error(w, "gopi cli: this endpoint takes "+termFormContentType+" (the /cli form); send the message as the `"+
			termChatInputName+"` field", http.StatusUnsupportedMediaType)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "gopi cli: bad form body: "+err.Error(), http.StatusBadRequest)
		return
	}
	draft := strings.TrimSpace(r.PostFormValue(termChatInputName))
	// The field is ONE line. A text browser can only send a single-line field,
	// but the wire is public, so a multi-line body is refused with a message
	// rather than silently joined into one message (Rule 15).
	if strings.ContainsAny(draft, "\r\n") {
		http.Error(w, "gopi cli: the input is ONE line — send one message per turn (a newline cannot be typed in this field)",
			http.StatusBadRequest)
		return
	}
	if draft == "" {
		http.Error(w, "gopi cli: empty message", http.StatusBadRequest)
		return
	}
	if runes := []rune(draft); len(runes) > termMaxPromptRunes {
		http.Error(w, fmt.Sprintf("gopi cli: message is %d characters (max %d)", len(runes), termMaxPromptRunes), http.StatusRequestEntityTooLarge)
		return
	}

	ag := s.agent
	if ag == nil {
		http.Error(w, "gopi cli: server agent not initialized", http.StatusInternalServerError)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "gopi cli: this carrier needs a streaming response writer", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", termHTMLContentType)
	w.WriteHeader(http.StatusOK)
	// The turn IS a document: the head first, so the deltas below are painted
	// as a page rather than as loose text. The response therefore cannot end
	// in a redirect: once the head is out the status line is frozen (measured
	// — http.Redirect after the first flush drops its Location silently). See
	// termPageTail for what ends the turn instead.
	writes, flushes, deltaWrites := 0, 0, 0
	write := func(text string) error {
		if text == "" {
			return nil
		}
		if _, err := io.WriteString(w, text); err != nil {
			return err // the reader is gone; the turn still finishes and is recorded
		}
		writes++
		return nil
	}
	flush := func() {
		flusher.Flush()
		flushes++
	}
	paint := func(text string) {
		if err := write(text); err == nil {
			flush()
		}
	}
	paint(termPageHead)
	// Echo the turn's message first, so the streamed answer has a subject on
	// screen: a terminal shows one document, and without this line the
	// operator's own words never appear in it.
	paint(html.EscapeString(termUserPrefix + draft + "\n\n"))

	// TryLock FIRST, exactly as handleChat does (B2): a concurrent turn is
	// TOLD it is queued instead of hanging byte-less behind s.mu.
	if !s.mu.TryLock() {
		paint(termQueuedLine)
		s.mu.Lock()
	}
	defer s.mu.Unlock()

	// R4 slash-command routing, by the SHARED CLASSIFIER (commands.go — one
	// criterion, four carriers): a command-shaped line goes to the shared table
	// (the same one the browser page and `gopi chat` use) and never reaches the
	// model; a command-shaped line the table does not accept is refused HERE,
	// with the literal-send channel named; a path or a "//word" literal is the
	// model's text, verbatim. The reset words are dispatched first because the
	// state they clear is carrier-local (s.history lives under s.mu, which the
	// shared table cannot see) — the words themselves come from the same table.
	classified := classifyChatLine(draft, sharedSlashWordKnown)
	if classified.kind == chatLineCommand || classified.kind == chatLineUnknown {
		draft = classified.text
		if isResetCommand(draft) {
			paint(html.EscapeString(termResetText(s)))
		} else if out, err := execSlashCommand(draft, s.webCommandContext()); err != nil {
			paint(html.EscapeString(termTurnFailedLine + err.Error() + "\n"))
		} else {
			paint(html.EscapeString(out))
		}
		s.lastTermWrites, s.lastTermFlushes, s.lastTermDeltaWrites = writes, flushes, deltaWrites
		paint(termPageTail)
		return
	}
	if classified.kind == chatLineEscape {
		// The caller asked for this line LITERALLY (`//word` → `/word`): it is
		// the model's message like any other text.
		draft = classified.text
	}

	seed := s.history
	if seed == nil {
		seed = []deepseek.Message{}
	}

	// Delta coalescing: the model's text accumulates and is flushed at most
	// once per termFlushInterval, so the terminal's whole-document re-layout
	// cost is bounded by TIME rather than by token count.
	var pending strings.Builder
	lastFlush := time.Now()
	ag.OnText = func(delta string) {
		if delta == "" {
			return
		}
		pending.WriteString(html.EscapeString(delta))
		if time.Since(lastFlush) < termFlushInterval {
			return
		}
		deltaWrites++
		if err := write(pending.String()); err == nil {
			flush()
		}
		pending.Reset()
		lastFlush = time.Now()
	}
	// Tool activity: a compact ONE-LINE card with its argument digest, so a
	// live tool call is visible while it runs under --tools. Flushed
	// immediately — a tool line is a discrete event the operator waits for and
	// costs one line, not a document re-render's worth of tokens.
	ag.OnToolEvents = func(ev agent.ToolEvent) {
		paint(html.EscapeString(termToolLine(ev)))
	}
	// A statement addressed to the OPERATOR (context pressure — a filled window
	// is announced, never silently emptied): it rides the same page the answer
	// streams into, because that is the surface this carrier draws. Nil-ing it
	// below with OnText/OnToolEvents keeps the pattern identical: no turn can
	// inherit a previous turn's writer.
	ag.OnUserNotice = func(text string) {
		paint("\n" + html.EscapeString(text))
	}
	// Bound this turn by the terminal carrier's budget, set on the shared
	// agent for exactly this turn and restored right after, both inside the
	// s.mu turn domain (no other turn can observe the momentary value).
	prevTurnTimeout := ag.Timeout
	ag.Timeout = termTurnTimeout()
	defer func() { ag.Timeout = prevTurnTimeout }()

	var result string
	var runErr error
	if s.tools {
		result, runErr = ag.ChatOps(draft, seed)
	} else {
		result, runErr = ag.Chat(draft, seed)
	}
	ag.OnText = nil
	ag.OnToolEvents = nil
	ag.OnUserNotice = nil

	// The tail: whatever the coalescer still held, then the outcome.
	if pending.Len() > 0 {
		deltaWrites++
		if err := write(pending.String()); err == nil {
			flush()
		}
		pending.Reset()
	}
	switch {
	case runErr != nil:
		paint(html.EscapeString("\n" + termTurnFailedLine + runErr.Error() + "\n"))
	case strings.TrimSpace(result) != "" && deltaWrites == 0:
		// The model answered without streaming a single delta (the
		// non-streaming fallback path): show the answer, rather than an empty
		// turn the operator would have to reload to understand.
		paint(html.EscapeString(result))
	case strings.TrimSpace(result) == "" && deltaWrites == 0:
		paint(termEmptyAnswerLine)
	}

	// The bookkeeping is the browser carrier's, unchanged: accumulate run
	// stats for the single session_end, capture the authoritative transcript
	// for the next turn's seed, and record the chats-table row on success.
	s.iterCount++
	if ag.Cost != nil {
		s.totalTokens = ag.Cost.TotalPromptTokens + ag.Cost.TotalCompletionTokens
	}
	s.history = append([]deepseek.Message{}, ag.Ctx.Messages...)
	if runErr == nil && ag.Cost != nil {
		// The sources here are the PERSON's by construction — this carrier's
		// line is the form field the operator typed (term.go's `draft`), never
		// a notify delivery — so the head column is theirs to claim. The face is
		// stated rather than defaulted, because that is the question tools'
		// predicate asks (internal/tools/chat_first_msg.go).
		ag.RegisterChatSession(termChatSessionMode, draft, inputsource.Human)
		ag.RecordChatTurn(ag.Cost.TotalPromptTokens, ag.Cost.TotalCompletionTokens, ag.Cost.TotalCacheHitTokens+ag.Cost.TotalCacheMissTokens)
	}
	s.lastTermWrites, s.lastTermFlushes, s.lastTermDeltaWrites = writes, flushes, deltaWrites
	// The turn ends by closing the document with the SAME tail GET /cli
	// serves: the form, so the reader can type again without navigating, and
	// the bottom anchor, so the settled page can be jumped to. This is what
	// replaces the redirect the epic first reached for — a 303 after a
	// streamed body is not expressible (the status is sent before the first
	// delta), and a terminal that ends on a dead document has no way back.
	paint(termPageTail)
}

// termFormContentType is the ONE body encoding the turn endpoint accepts (the
// encoding the page's form produces).
const termFormContentType = "application/x-www-form-urlencoded"

// termChatSessionMode is the mode label this carrier registers in the CHATS
// table (M4.6 钩子分流). It is the ORDINARY chat mode, from the table's own
// constant — not a label invented here: the carrier changes the WIRE, not the
// kind of conversation, and the chats table accepts exactly chat|web (a
// literal "cli" was rejected by its gate in the live acceptance run). The row
// stays distinguishable by its log path and first message.
//
// The constant is spelled locally (Rule 1b) rather than imported at each use
// site because the value belongs to internal/tools; cli_term_test.go asserts
// the two agree, so a rename there fails here.
const termChatSessionMode = tools.ChatModeCLI

// termToolLine renders one compact tool-activity line: the kind, the tool and
// its digest (the call's argument digest / the result's first line — the SAME
// agent.ToolCallSummary / agent.ToolResultDigest the browser carrier streams,
// so the two carriers cannot describe one call differently).
//
// THE SHAPE IS THE RENDERER'S (term_tool_row.go), not this file's: the one pure
// row renderer decides the glyph, the name and the payload's arrangement, and a
// carrier that cannot show SGR (this page — HTML has its own emphasis, and its
// bytes are escaped into <pre>) asks it for the CELLS and joins their text. That
// is why the two carriers cannot describe one call differently even though only
// one of them draws colour.
func termToolLine(ev agent.ToolEvent) string {
	return termToolRowText(ev) + "\n"
}

// termResetText performs the carrier-local conversation reset and returns the
// acknowledgement. Called with s.mu held (the same contract serveResetCommand
// has), so it never re-locks.
func termResetText(s *webServer) string {
	cleared := s.resetConversationLocked()
	return fmt.Sprintf("%s (%d messages dropped)\n", webResetBodyMessage, cleared)
}
