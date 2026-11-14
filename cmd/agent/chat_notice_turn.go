package main

// chat_notice_turn.go — the REPL's half of epic 01a0eeea (flagship 5): a
// background job's completion notice reaching an IDLE prompt mints a NEW TURN,
// with no keystroke from the operator.
//
// THE CYCLE DEPENDENCY THIS CLOSES. The inbox (`tools.TakeJobNotices`) already
// had exactly one take point — the chat turn's round boundary
// (`internal/agent/chat_ops.go`, "BEFORE the request is built"). But that
// boundary is only reachable from inside a turn, and a turn only started when
// the operator typed a line, so a notice could only ever be delivered to a turn
// that was already running for some other reason. A job whose completion nobody
// was waiting on could therefore never come back: the REPL sat in its idle
// `select` on `lineCh`/`interruptCh` forever.
//
// WHAT THIS FILE ADDS, and nothing else:
//  1. `chatNoticeTurnOwed` — the ONE reader of the LEVEL, so "a turn is minted"
//     and "the inbox is non-empty" cannot disagree;
//  2. `jobNoticeTurnLabel` and the operator-facing note, so a turn the model did
//     not get from a person is never a turn nobody can account for.
//
// The two idle `select`s themselves (and their arms — `tools.JobNoticeWake` for
// a notice THIS process produced, and `agent.SessionInboxIdleWake` for one
// ANOTHER process delivered) live in cli_chat.go, where the loop and the
// `lineRead` type are. Both arms ask the SAME level and therefore cannot
// disagree about whether a turn is owed; the second one exists only because a
// cross-process delivery can raise no in-process edge by itself
// (internal/agent/session_inbox.go, SessionInboxIdlePoll).
//
// WHAT IT DELIBERATELY DOES NOT DO. It does not take from the inbox (the take
// point stays unique — the turn's opening line is built from a PEEK, so the very
// notice it names is still there for the round boundary to inject), it does not
// inject anything into the conversation, and it does not interrupt a turn: this
// arm is only ever reached with the loop at its prompt, so every busy-path
// behaviour is unchanged by construction.
//
// THE BUSY CASE, stated because it is a CHOICE and not an omission (the epic asks
// for the trade-off explicitly). Option A — QUEUE the notice for the next turn
// boundary — was rejected. A is preferred nowhere it could be observed: a notice
// carries no text the model must answer, so a turn opened mid-conversation would
// have no input to be about; injecting it into a running turn would put it
// between a tool result and the assistant message, which is precisely the
// hermes invariant this epic forbids ("a notice may only appear as a NEW turn
// while idle, never between a tool result and an assistant message, never a
// rewrite of the past"). So the busy case takes Option B: THE NOTICE WAITS IN
// THE INBOX, WHERE IT ALREADY WAITED BEFORE THIS EPIC. Nothing is dropped (the
// level stays true, the slice keeps every notice until it is drained) and nothing
// is rewritten (no past context is touched). It becomes the very next turn the
// moment the current one ends: `wasTurn` is set at that boundary, the notice is
// still pending, and the wake arm is reachable on the next iteration — so the
// model reads it at the round boundary it was always read at, as the opening
// message of a turn that exists for it.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatJobNoticeTurnNotice is printed when the level opens a turn of its own.
// The operator must be able to tell "a job finished and the console answered by
// itself" from "I asked something" — the turn header repeats this text (it is
// the turn's OWN opening message), so the two are the same fact said twice, on
// purpose: this row appears at the moment the turn opens, which is before the
// model's answer exists.
const chatJobNoticeTurnNotice = "[gopi] %s — a background job finished; opening a turn to report it (no input from you)\n"

// jobNoticeTurnLabel names the jobs a notice turn was opened for: the id alone
// for one, "N jobs (…)" for several. It is the LABEL the operator reads, so it
// never carries a command line — those are injected into the conversation,
// which is where the model reads them.
func jobNoticeTurnLabel(notices []tools.JobNotice) string {
	if len(notices) == 1 {
		return agent.JobNoticeLabel(notices[0])
	}
	labels := make([]string, 0, len(notices))
	for _, n := range notices {
		labels = append(labels, agent.JobNoticeLabel(n))
	}
	return fmt.Sprintf("%d notices (%s)", len(notices), strings.Join(labels, ", "))
}

// chatSessionMessageTurnNotice is the operator-facing note for a turn minted by
// an EXTERNAL message (`gopi notify --session … --message …`). It is a second
// constant rather than a reworded first one because the two facts are genuinely
// different — the operator's question "why did the console answer by itself?"
// has a different answer, and a note that said "a background job finished" when
// no job existed would be a false statement about the machine.
const chatSessionMessageTurnNotice = "[gopi] %s — an external process sent this session a message; opening a turn to deliver it (no input from you)\n"

// chatNoticeTurnNoticeFor picks the note for a turn the LEVEL minted. The inbox
// carries both kinds of fact (a job finished, an external message arrived), so
// the note names the one that actually opened this turn; a turn minted by both
// is announced as the external case, whose text is the one the operator cannot
// otherwise account for.
func chatNoticeTurnNoticeFor(notices []tools.JobNotice) string {
	for _, n := range notices {
		if n.External != nil {
			return fmt.Sprintf(chatSessionMessageTurnNotice, jobNoticeTurnLabel(notices))
		}
	}
	return fmt.Sprintf(chatJobNoticeTurnNotice, jobNoticeTurnLabel(notices))
}

// chatNoticeTurnOwed reports whether the job-notice LEVEL owes this idle prompt
// a turn. It is the ONE reader of the level (`tools.JobNoticePending`), so
// "a turn is minted" and "the inbox is non-empty" cannot disagree — for BOTH
// kinds of notice the queue carries (a background job that finished in this
// process, and a message another process delivered into this session's inbox):
//
//   - FALSE → no turn. A wake token can be consumed while the level is false (a
//     turn's round boundary drained the inbox first, or two jobs finished and
//     the level was read once), and minting a turn from a stale token would open
//     a turn whose only content is a note that no longer exists
//     (`internal/tools/job_notice_wake.go` states the contract);
//   - TRUE → a turn, and it is minted with NO operator input: the read that
//     carries it sets `lineRead.noticeTurn`, whose message is composed at the
//     turn boundary by `agent.ChatJobNoticeOpenLine` — the carrier does not
//     decide what a notice says.
//
// It reads no carrier state and writes nothing: the announcement belongs at the
// boundary, where the notices are peeked and the message is built.
func chatNoticeTurnOwed() bool { return tools.JobNoticePending() }
