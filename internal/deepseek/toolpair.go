package deepseek

import (
	"fmt"
	"log"
	"strings"
)

// Wire-role and content-block type names of the Claude Messages protocol.
// Every one of these values is COMPARED or SWITCHED ON by the pairing walker
// below, so it is a bare-string keyword by construction (Rule 1b) — naming it
// here gives the protocol vocabulary a single source of truth instead of
// typing the literal at each predicate.
const (
	wireRoleAssistant       = RoleAssistant
	wireRoleUser            = RoleUser
	wireBlockTypeText       = BlockTypeText
	wireBlockTypeToolUse    = BlockTypeToolUse
	wireBlockTypeToolResult = BlockTypeToolResult
)

// toolUseOrphanRepairLogTag prefixes every pairing-repair log line, so an
// operator can find them the same way as the minimal-core degrade lines.
const toolUseOrphanRepairLogTag = "[GOPI][tool-pairing]"

// ToolPairOrphanResult is the outcome of repairing an assistant tool_use whose
// tool_result is missing from the immediately following message. It is
// returned (not only logged) so the outbound choke point can carry the fact
// into the run's crash dump and the caller's log.
type ToolPairOrphanResult struct {
	// Repaired counts assistant messages that gained a synthesized
	// tool_result block for one or more orphaned tool_use ids.
	Repaired int
	// OrphanIDs lists, in message order, the tool_use ids that had no
	// matching tool_result — the exact ids the API rejected with HTTP 400
	// "`tool_use` ids were found without `tool_result` blocks immediately
	// after" (measured 2026-09-22, ~/.gopi/dumps/20260922-040822.dump.json).
	OrphanIDs []string
}

// Touched reports whether RepairToolUsePairs changed anything.
func (r ToolPairOrphanResult) Touched() bool { return r.Repaired > 0 }

// orphanResultPlaceholder is the tool_result text a synthesized repair block
// carries. It is deliberately NOT the string "error": an error-shaped result
// is classified by the model as a failed tool call and it will re-run the tool
// or apologise for the failure, which is not what happened — the result was
// produced and then compacted away. The text states the mechanical fact.
const orphanResultPlaceholder = "[gopi] tool result unavailable — this call's output is not in the context (the round was interrupted before it returned, or its result was dropped by a context compaction); the call did not necessarily fail. Re-run the tool if you still need the output."

// RepairToolUsePairs enforces the Claude Messages pairing invariant on a wire
// message array IN PLACE:
//
//	for every assistant message carrying tool_use block(s), the immediately
//	following message must be a user message carrying a tool_result block for
//	every one of those ids.
//
// Until now this invariant was only ASSERTED (comments in
// BuildClaudeRequest and agent.trimCommittedTaskArea claimed the conversions
// were "orphan-safe by design"). It is not something to assert: the invariant
// is violated not by a malformed conversation but by the harness's own context
// management. Measured 2026-09-22 (epic: the p1 hand-off death): a run's first
// message was an assistant tool_use whose result lived inside a window that
// the committed-working-region trim dropped; the array kept the head, the API
// answered HTTP 400 for exactly that id ("messages.1: `tool_use` ids were
// found without `tool_result` blocks immediately after:
// call_00_ET_TBCTnLz8k12TGe2ZEVxl2105"), the live run died at the hand-off and
// the same damaged array was persisted into the raw-log checkpoint, so EVERY
// resume died on its first request too.
//
// The repair is fail-closed and minimal:
//   - an assistant message with zero tool_use blocks is never touched;
//   - a tool_use that already has a tool_result directly after it is never
//     touched (healthy sessions are byte-identical through this function);
//   - an UNMATCHED tool_use gets a synthesizing tool_result block, added to
//     the following user message of the tool round when that message carries
//     tool_result blocks (the shape this call site always emits — the
//     in-context tool result lives there), inserted as a new user message
//     directly after the assistant otherwise;
//   - blocks are never REMOVED: a tool_use that the user has already seen and
//     answered stays in history, so the wire never silently rewrites the
//     conversation — the minimal repair is the one that adds the missing half;
//   - reported ids are the true orphans (present in no message of the array),
//     which is what the outbound choke point surfaces in a run's crash dump.
//
// Because the array is repaired in the caller's own map/slice, repairing
// Ctx.Messages-descended state as well as the outbound request means no
// damaged array can reach the checkpoint (LoadCheckpoint reads the request the
// raw log recorded — the post-repair request is what is logged).
//
// Returns a zero result (no repair, no ids) for a request that already
// satisfies the invariant.
func RepairToolUsePairs(req *ClaudeRequest) ToolPairOrphanResult {
	var res ToolPairOrphanResult
	if req == nil {
		return res
	}
	// everyResult indexes the ids any tool_result block in the array answers —
	// used to distinguish "the result is genuinely absent from the context"
	// (the true orphan, worth reporting upwards) from "the result exists but
	// not directly after its tool_use" (a shape violation, repaired silently
	// because the pairing information is present).
	everyResult := map[string]bool{}
	for i := range req.Messages {
		for _, b := range req.Messages[i].Content {
			if b.Type == wireBlockTypeToolResult && b.ToolUseID != "" {
				everyResult[b.ToolUseID] = true
			}
		}
	}

	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Role != wireRoleAssistant {
			continue
		}
		var orphans []string
		for _, b := range m.Content {
			if b.Type != wireBlockTypeToolUse || b.ID == "" {
				continue
			}
			if !hasToolResultAfter(req.Messages, i, b.ID) {
				orphans = append(orphans, b.ID)
			}
		}
		if len(orphans) == 0 {
			continue
		}

		// Preferred insertion point: the message directly after the assistant
		// when it is the tool round's user message (it carries tool_result
		// blocks) — the extra blocks join that message, exactly as the API
		// wants them. Never merged into a tool-result message from an OLDER
		// round: appending there would place a result for call N inside the
		// response to call N-1, a shape the API also rejects.
		if i+1 < len(req.Messages) && req.Messages[i+1].Role == wireRoleUser &&
			messageCarriesToolResult(&req.Messages[i+1]) {
			for _, id := range orphans {
				req.Messages[i+1].Content = append(req.Messages[i+1].Content, synthesizedToolResult(id))
			}
		} else {
			blocks := make([]ClaudeContentBlock, 0, len(orphans))
			for _, id := range orphans {
				blocks = append(blocks, synthesizedToolResult(id))
			}
			repaired := make([]ClaudeMessage, 0, len(req.Messages)+1)
			repaired = append(repaired, req.Messages[:i+1]...)
			repaired = append(repaired, ClaudeMessage{Role: wireRoleUser, Content: blocks})
			repaired = append(repaired, req.Messages[i+1:]...)
			req.Messages = repaired
			// The inserted message shifts every later index; the loop's own
			// next iteration resumes at i+1, which is the message we just
			// inserted (all tool_result, no tool_use) — no index bookkeeping
			// needed because the walk only ever inspects assistant messages.
		}

		res.Repaired++
		for _, id := range orphans {
			if !everyResult[id] {
				res.OrphanIDs = append(res.OrphanIDs, id)
			}
		}
	}

	if res.Touched() {
		log.Printf("%s repaired %d assistant message(s) whose tool_use had no tool_result immediately after (ids: %s)",
			toolUseOrphanRepairLogTag, res.Repaired, strings.Join(res.OrphanIDs, ", "))
	}
	return res
}

// hasToolResultAfter reports whether the message directly after the assistant
// at index i is a user message carrying a tool_result for id.
func hasToolResultAfter(msgs []ClaudeMessage, i int, id string) bool {
	if i+1 >= len(msgs) || msgs[i+1].Role != wireRoleUser {
		return false
	}
	for _, b := range msgs[i+1].Content {
		if b.Type == wireBlockTypeToolResult && b.ToolUseID == id {
			return true
		}
	}
	return false
}

// messageCarriesToolResult reports whether a message holds any tool_result
// block (the tool round's user message, in GoPi's conversion).
func messageCarriesToolResult(m *ClaudeMessage) bool {
	for _, b := range m.Content {
		if b.Type == wireBlockTypeToolResult {
			return true
		}
	}
	return false
}

// synthesizedToolResult builds the fail-closed placeholder block for an
// orphaned tool_use id.
func synthesizedToolResult(id string) ClaudeContentBlock {
	return ClaudeContentBlock{
		Type:      wireBlockTypeToolResult,
		ToolUseID: id,
		Content:   orphanResultPlaceholder,
	}
}

// ValidateToolUsePairs reports the FIRST pairing violation of the Claude
// Messages invariant, or nil when the array satisfies it:
//
//	for every assistant message at index i carrying tool_use block id X,
//	messages[i+1] must be a user message carrying a tool_result for X, and
//	the last message must never end on an unanswered tool_use.
//
// It is the assertion half of the invariant — RepairToolUsePairs is the
// execution half — and it is a pure predicate over a request, which is what
// makes the outbound choke-point check trustworthy: the gate can refuse a
// damaged array AND be asserted, per-message, by the package's tests.
func ValidateToolUsePairs(req *ClaudeRequest) error {
	if req == nil {
		return nil
	}
	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Role != wireRoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type != wireBlockTypeToolUse || b.ID == "" {
				continue
			}
			if !hasToolResultAfter(req.Messages, i, b.ID) {
				return fmt.Errorf(
					"deepseek: tool_use %q at messages.%d has no tool_result in the immediately following message (Claude Messages pairing invariant)",
					b.ID, i)
			}
		}
	}
	return nil
}
