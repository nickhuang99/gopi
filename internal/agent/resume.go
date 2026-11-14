package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// Raw log markers. A REQUEST record's payload is the exact bytes handed to the
// HTTP POST — a byte-exact full-context checkpoint (system + GOPI.md + messages
// + tools + thinking/effort params) — and only that side is used for resume.
//
// The DIRECTION markers themselves are no longer spelled here: a reader asks a
// record what it IS (`rawlogfmt`'s tag) and never what text it was written with,
// so the length-prefixed and the legacy framings need no branch and no synonym
// table (Rules 13/14 — the vocabulary's one owner is `internal/rawlogfmt`).
const (
	// rawMarkerTrim opens the `#` HEADER record `gopi session gc
	// --trim-raw-log` writes in place of a log's dropped middle. It comes from
	// the WRITER's own constant (scratch.SessionsRawLogTrimMarkerPrefix), never
	// a re-spelling: Rules 1/13/14, and the marker is matched as a line PREFIX
	// because the writer continues it with a timestamp.
	//
	// It is a MARKER here for the same reason the round directions are: it
	// TERMINATES the record before it. Without it, a trimmed log's head
	// `→ REQUEST` payload ran past the marker line and was handed to
	// json.Unmarshal as `{…}# gopi …`, which failed with
	// `invalid character '#' after top-level value`.
	rawMarkerTrim = scratch.SessionsRawLogTrimMarkerPrefix
)

// resumeTrimmedNextStep is the ONE sentence that tells a reader what to do
// instead: the log cannot answer, and the alternatives are named rather than
// implied.
const resumeTrimmedNextStep = "pick another artifact for this run (an untrimmed raw log, or the run's crash dump), or raise the trim cap so `gopi session gc --trim-raw-log` leaves this file alone"

// checkpointMessageLimit is an upper bound on how many history messages a
// resume will seed. A mismatched/truncated log could otherwise produce an
// unreasonable message array; the limit is a safety valve, far above any real
// session. It is declared here so the bound has a single source of truth.
const checkpointMessageLimit = 2000

// LoadCheckpoint parses a gopi session raw log file and returns the last full
// `→ REQUEST` as a deepseek.ClaudeRequest plus the reconstructed conversation
// history ([]deepseek.Message) that the interrupted session's final request
// carried on the wire. systemPrompts holds the request's top-level system
// blocks (kernel + GOPI.md) in order, for model/context validation.
//
// The conversation messages returned are exactly the request's `messages`
// array translated back into deepseek.Message form (tool_result user messages
// collapse to Role:"tool"; assistant text/thinking/tool_use blocks are joined
// into a single Message). Returning history re-seeded into Ctx.Messages then
// flows through GetMessages()/BuildClaudeRequest() unchanged on the next
// round, giving a faithful in-context continuation.
//
// The system prompt is NOT included in history — GetMessages() preprends the
// freshly-assembled SystemPrompt + ProjectContext. The interrupted request's
// system blocks are returned separately so a caller can validate them against
// the current agent (model/settings drift warning).
func LoadCheckpoint(rawLogPath string) (*deepseek.ClaudeRequest, []deepseek.Message, []string, error) {
	req, history, systemTexts, _, err := LoadCheckpointWithSource(rawLogPath)
	return req, history, systemTexts, err
}

// LoadCheckpointWithSource is LoadCheckpoint plus the FACT of which artifact
// answered, so the resume face can ANNOUNCE a recovery from the crash dump
// instead of implying the raw log carried it (Rule 15 — a recovery from a
// second source is stated, never silent).
//
// THE SECOND SOURCE EXISTS BECAUSE THE FIRST ONE CAN BE EMPTY (measured
// 2026-10-29, row 01a183a8): a run whose FIRST request died at the transport
// layer (http_status=0, dial timeout) leaves a raw.log holding a header and
// nothing else — the raw logger writes its "→ REQUEST" marker only once a body
// is handed to the transport — while the crash dump in the SAME directory holds
// that complete request body. The fallback's own reader and its honest refusal
// live in resume_dump_fallback.go; this function is the ONE place that decides
// which source answered, so every resume face (the verb face and the no-verb
// `gopi --session <id>` face) shares the decision (Rules 13/14).
func LoadCheckpointWithSource(rawLogPath string) (*deepseek.ClaudeRequest, []deepseek.Message, []string, CheckpointProvenance, error) {
	rawBody, rawSize, err := scanRawLogCheckpoint(rawLogPath)
	if err != nil {
		return nil, nil, nil, CheckpointProvenance{}, err
	}
	if len(rawBody) == 0 {
		recovered, dumpErr := loadCheckpointFromDump(rawLogPath)
		if dumpErr != nil {
			return nil, nil, nil, CheckpointProvenance{}, resumeNoCheckpointError(rawLogPath, rawSize, dumpErr.Error())
		}
		provenance := CheckpointProvenance{
			Source:          CheckpointSourceDump,
			Path:            recovered.Path,
			FailedIteration: recovered.FailedIteration,
			Category:        recovered.Category,
		}
		req, history, systemTexts, err := checkpointFromRequest(recovered.Request, provenance.Detail())
		return req, history, systemTexts, provenance, err
	}
	provenance := CheckpointProvenance{Source: CheckpointSourceRawLog, Path: rawLogPath}
	req, history, systemTexts, err := checkpointFromBody(rawBody, rawLogPath)
	return req, history, systemTexts, provenance, err
}

// scanRawLogCheckpoint reads the raw log and returns the LAST "→ REQUEST"
// payload plus the file's size. An empty payload with a nil error is the
// ordinary "this log holds no completed request round" answer — the case the
// dump fallback exists for — and is deliberately NOT an error here, so the
// source decision stays in ONE place.
func scanRawLogCheckpoint(rawLogPath string) ([]byte, int64, error) {
	f, err := os.Open(rawLogPath)
	if err != nil {
		return nil, 0, fmt.Errorf("resume: open raw log: %w", err)
	}
	defer f.Close()

	info, statErr := f.Stat()
	var size int64
	if statErr == nil {
		size = info.Size()
	}

	// A TRIMMED LOG CANNOT ANSWER, AND SAYS SO BY NAME. The rewrite kept the
	// head and the tail and dropped everything between them, so the head's
	// first `→ REQUEST` is the ONLY candidate this scanner would find — and it
	// is the run's FIRST round offered as "the last state", which is not a
	// degraded resume but a wrong one (it would silently discard every round
	// the run completed after round 1). Refusing is the only honest answer, and
	// it carries the path and the reason (Rule 15).
	trimmed, terr := scratch.RawLogCarriesTrimMarker(rawLogPath)
	if terr != nil {
		return nil, size, fmt.Errorf("resume: read raw log: %w", terr)
	}
	if trimmed {
		return nil, size, fmt.Errorf("resume: %s: %s (%s)", rawLogPath,
			scratch.TrimmedLogResumeRefusal, resumeTrimmedNextStep)
	}

	// RECORD-driven scan: walk the log's RECORDS and keep the last REQUEST's
	// payload. The direction comes from the record's TAG, never from its header
	// text, because the two framings spell the same event differently (`→ REQUEST`
	// on a legacy line, `R 412` on a length-prefixed one) and a text test would
	// quietly find no checkpoint in every log the current writer produces.
	//
	// The walk also stops at the framing, not at a marker word: the trim HEADER is a
	// comment line in both framings (rawlogfmt's reader yields it as a skipped
	// comment, never as a payload line), so the trimmed-log refusal above is the only
	// place that has to reason about it.
	format, ferr := rawlogfmt.Detect(rawLogPath)
	if ferr != nil {
		return nil, size, fmt.Errorf("resume: read raw log: %w", ferr)
	}
	r := rawlogfmt.NewReaderFrom(f, format)
	var reqBody []byte
	for {
		rec, rerr := r.Next()
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return nil, size, fmt.Errorf("resume: read raw log: %w", rerr)
		}
		if rec.Tag != rawlogfmt.TagRequest && rec.Tag != rawlogfmt.TagProbeRequest {
			continue
		}
		// ONE trailing newline belongs to the framing (the logger appends it after
		// every write); the payload is the JSON body itself.
		body := rec.Payload
		if n := len(body); n > 0 && body[n-1] == '\n' {
			body = body[:n-1]
		}
		reqBody = append(reqBody[:0], body...)
	}
	return reqBody, size, nil
}

// checkpointFromBody parses a raw request body (already recovered from
// whichever artifact carried it) into the checkpoint triple the resume seeds
// from. origin names that artifact in every error the parse can produce.
func checkpointFromBody(reqBody []byte, origin string) (*deepseek.ClaudeRequest, []deepseek.Message, []string, error) {
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return nil, nil, nil, fmt.Errorf("resume: cannot parse request checkpoint from %s (truncated log? last request may be incomplete): %w", origin, err)
	}
	return checkpointFromRequest(&req, origin)
}

// checkpointFromRequest is the ONE validator/converter for a recovered request,
// whatever artifact it came from (raw log or crash dump): message presence, the
// safety bound and the tool_use/tool_result pairing invariant all live here so
// no second source can quietly skip a check.
func checkpointFromRequest(req *deepseek.ClaudeRequest, origin string) (*deepseek.ClaudeRequest, []deepseek.Message, []string, error) {
	if len(req.Messages) == 0 {
		return nil, nil, nil, fmt.Errorf("resume: checkpoint from %s has no messages", origin)
	}

	history, systemTexts := requestToMessages(req)
	if len(history) > checkpointMessageLimit {
		return nil, nil, nil, fmt.Errorf("resume: checkpoint has %d messages, exceeding safety limit %d", len(history), checkpointMessageLimit)
	}
	// Pairing invariant on the RECOVERED history — the single point where a
	// persisted checkpoint enters the process (both the resume CLI and the
	// chat turn loop read it here). A checkpoint is whatever the interrupted
	// process last put on the wire, so a damaged one is precisely how the
	// 400-death became per-resume instead of one-off (measured 2026-09-22: an
	// unanswered head tool_use was recorded as the checkpoint, and every
	// resume died on its first request). Repair the recoverable damage and
	// report what could not be repaired; the same invariant is gated again at
	// the wire choke point (internal/deepseek) and at the loop's outbound
	// build, which are the nets under every other builder.
	history, orphanIDs := repairToolUsePairs(history)
	if len(orphanIDs) > 0 {
		log.Printf("\U0001f517 checkpoint: repaired %d unanswered tool_use(s) in the recovered history (ids: %s) — the interrupted request carried them without their tool_result", len(orphanIDs), strings.Join(orphanIDs, ", "))
	}
	if err := verifyToolUsePairs(history); err != nil {
		return nil, nil, nil, fmt.Errorf("resume: the recovered checkpoint violates the Claude Messages tool_use/tool_result pairing invariant and could not be repaired — %w", err)
	}
	return req, history, systemTexts, nil
}

// requestToMessages converts a ClaudeRequest's wire `messages` array back into
// []deepseek.Message for re-seeding into Context.Messages. It mirrors the
// per-message logic of BuildClaudeRequest in reverse:
//   - user with text blocks       → ONE user Message PER text block
//   - user with tool_result blocks → one Message per tool_result (Role:"tool")
//   - assistant → one Message combining thinking + text + tool_use blocks
//
// ONE MESSAGE PER TEXT BLOCK, NOT ONE PER WIRE MESSAGE, and the difference is
// load-bearing rather than tidy. The builder FUSES consecutive user messages
// into one wire message (`internal/deepseek/claude_client.go`'s user case,
// "Merge ALL consecutive user messages, images included") — that is required by
// the API's role alternation and it is CORRECT on the way out. This function is
// the way back in, so it must undo exactly that fusion: a wire message carrying
// N text blocks is N messages, not one.
//
// WHAT THE JOIN COST (measured 2026-11-14 on session 6f982833, request #122 —
// the first request after a resume). Two different producers write consecutive
// user rows, and BOTH are fused on the wire:
//
//	the fold note + the operator's next input  (turnfold.go appends the note
//	    where the folded turn was; the new turn's input follows it with no
//	    assistant message between them)
//	two inbox notices, a notice + a voice input, and every other pair of
//	    back-to-back harness rows
//
// Reconstructed with the join, the first shape became ONE message whose text
// merely STARTS with the note head, and then two predicates read it in opposite
// directions: isTurnFoldNoteMessage answered TRUE (the head is a prefix of the
// concatenation) while isTurnOpeningRow answered FALSE (a harness prefix is not
// an operator row). The operator's own input therefore inherited the note's
// identity: turnRangeFoldable's first exclusion refused to fold that turn for
// the rest of the session, and turnFoldNote never collected its anchors. The
// text still reached the model — this is not lost data in the request — so the
// failure was silent: one turn per resume, permanently un-foldable.
//
// The arithmetic that makes "split" the right inverse rather than a taste: the
// round trip `parse(build(msgs))` must return the SAME block partition. With the
// join it does not (one user wire message in, `text:1716 text:1990` out as
// `text:3707`); with the split it does (0 shape diffs, measured on a real body).
//
// systemTexts collects the request's top-level system blocks (kernel + GOPI.md).
func requestToMessages(req *deepseek.ClaudeRequest) (history []deepseek.Message, systemTexts []string) {
	for _, sb := range req.System {
		systemTexts = append(systemTexts, sb.Text)
	}

	for _, cm := range req.Messages {
		switch cm.Role {
		case "user":
			// A user message is either text blocks or a tool-result carrier —
			// and when it is text blocks, each one is its own message (see the
			// comment above: this is the inverse of the builder's fusion).
			for _, b := range cm.Content {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) != "" {
						history = append(history, deepseek.Message{Role: roleUserMessage, Content: b.Text})
					}
				case "tool_result":
					content := ""
					if b.Content != nil {
						content = fmt.Sprint(b.Content)
					}
					history = append(history, deepseek.Message{
						Role:       "tool",
						Content:    content,
						ToolCallID: b.ToolUseID,
					})
				}
			}
		case "assistant":
			var asst deepseek.Message
			asst.Role = "assistant"
			var texts []string
			for _, b := range cm.Content {
				switch b.Type {
				case "thinking":
					// Replay only non-empty thinking with its signature (the
					// API requires it in assistant history).
					if b.Thinking != "" {
						asst.ReasoningContent = b.Thinking
						asst.ThinkingSignature = b.Signature
					}
				case "text":
					if b.Text != "" {
						texts = append(texts, b.Text)
					}
				case "tool_use":
					asst.ToolCalls = append(asst.ToolCalls, deepseek.ToolCall{
						ID:        b.ID,
						Name:      b.Name,
						Arguments: b.Input,
					})
				}
			}
			asst.Content = strings.Join(texts, "\n")
			history = append(history, asst)
		}
	}
	return history, systemTexts
}
