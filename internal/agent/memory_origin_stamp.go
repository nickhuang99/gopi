// The provenance stamp a captured memory row carries (epic 自动记忆 item 2/5).
//
// This file is the AGENT-SIDE half of internal/tools/memory_origin.go: the tools
// layer owns the columns, the type and the write/read/refusal rules; the agent
// owns the two things only the running loop knows — WHICH session the store call
// happens in, and WHICH round of that session it happens at.
//
// The stamp is not decoration. A self-contained captured body (scheme C, the
// epic's item-1 ruling) survives a cross-host sync precisely because it carries
// its own text; the pointer is what keeps the row ADDRESSABLE — without it a
// reader holding a distilled note has no way back to the turn that produced it,
// and "where did this come from?" is answerable only by the sentence inside the
// note itself.
//
// WHY THE MODEL PASSES NOTHING. The session and the round are properties of the
// RUN, not judgments the model makes, so they are stamped here instead of being
// asked for as tool arguments. That is a deliberate boundary, not an oversight:
// prompting the model for its own session id would (a) put two spellings of one
// fact on the wire, (b) invite a hallucinated id, and (c) change the tool's
// schema and routing text for a value the model can never know better than the
// loop — the ground Rule 11 requires adoption proof for is not entered at all.
// An explicit override stays available where a human or a script actually has
// the fact: the `gopi memory store` flags (--session-id / --round-index).
package agent

import (
	"strings"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// storeSessionID resolves the session a memory store call belongs to: THIS
// run's session log id. A run with no session log — an embedded or test agent —
// resolves to "", and the row records no session, which is the honest answer
// (Rule 15): never a made-up id.
func (a *Agent) storeSessionID() string {
	if a.SessionLog == nil {
		return ""
	}
	return strings.TrimSpace(a.SessionLog.SessionID())
}

// storeRoundNumber resolves the round a memory store call happens at.
//
// The current round needs no conversion for the TASK loop: runEngine sets
// hippocampusRound to i+1 — the same numbering sessionevent.RoundNumberOf
// defines from the loop's 0-based iteration, the numbering gopilog prints and
// `gopi session log <id> <round>` takes.
//
// ON THE CHAT SURFACE THE CURSOR IS SILENT, and it is not a hypothesis
// (measured 2026-10-20, epic `chat --replay` defect C). The chat paths stamp
// hippocampusRound through stampHippocampusRound (chat_ops.go), which returns
// EARLY when a.Hippocampus is nil — and the host is nil whenever
// newHippocampusHost's gate is off (GOPI_HIPPOCAMPUS unset, which it is on this
// host). The field therefore keeps its zero value for the whole conversation,
// and every captured row is stamped round_index=0 — the ABSENCE value. Evidence
// from the operator's own ledger: 7782 rows carry no session at all, and among
// named sessions 2c394553-… has 10 rows ALL at round 0 while that same session
// log holds 52 `request` events, i.e. 52 rounds the row does not name.
//
// THE FIX USES THE HOUSE'S OWN DEFINITION RATHER THAN A SECOND ONE. The note
// index already solved exactly this problem with the session log's own count of
// `request` events: that is the numbering which survives a chat's per-turn agent
// re-creation, because the SESSION spans the conversation while the Agent does
// not. So when the cursor was never stamped, this asks the session log instead of
// reporting "no round" for a run that demonstrably had 52 of them. A stamped
// cursor still wins: the task loop's i+1 is the same number derived at the same
// instant, and there it is the authoritative reading.
//
// IT IS A ROUND, NOT A TURN, and that is why it no longer calls the note layer's
// resolver. The note index's number is now a TURN (one complete input — the
// operator's ruling, 2026-11-10), while a memory row's `round_index` is one
// HTTP request/response; handing a turn number to a column named for the round
// would be the category error Rule 14 forbids, one field over. The quantity is
// still the session's own request count, read from the same writer.
func (a *Agent) storeRoundNumber() int {
	if sessionevent.RoundIndexRecorded(a.hippocampusRound) {
		return a.hippocampusRound
	}
	if a.SessionLog == nil {
		return sessionevent.RoundIndexUnknown
	}
	return a.SessionLog.RequestsSoFar() + sessionevent.RoundIndexBase
}

// memoryStoreOrigin is the whole stamp, in one call, for the store site: the
// session this run belongs to and the round it is on.
//
// The stamp is SESSION-FIRST, and that is a correctness rule, not a style one. A
// round is a cursor INTO a session, so a round with no session is a pointer to
// nothing — the store refuses it (MemoryOrigin.Complete), and rightly so. The
// loop must therefore never MANUFACTURE that shape: a run with no session log
// (an embedded agent, a test harness) claims nothing at all, rather than
// stamping a round it cannot anchor. Getting this backwards is not theoretical:
// it was measured, and it turned every store in a session-less run into a
// refusal that the completion checkpoint answered by forcing another round.
func (a *Agent) memoryStoreOrigin() tools.MemoryOrigin {
	session := a.storeSessionID()
	if session == "" {
		return tools.MemoryOrigin{}
	}
	return tools.MemoryOrigin{SessionID: session, Round: a.storeRoundNumber()}
}
