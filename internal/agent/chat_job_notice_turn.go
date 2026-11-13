package agent

// The job-notice TURN OPENER: the message a chat turn opens with when its only
// reason for existing is that a background job finished (epic 01a0eeea,
// flagship 5 — "a notice reaching an IDLE prompt mints a new turn").
//
// WHY IT IS HERE AND NOT IN THE CARRIER. The notice's own wording belongs to
// the agent package, which owns the inbox injection the turn's round boundary
// performs. The REPL is a CARRIER: it decides WHEN a turn opens (it is the
// party parked at the prompt), and it asks the agent for the ONE line that
// states why. Two formatters would be two answers to "what does a notice
// say?", and the second one would drift from the text the model is injected
// with — this single short form is the fact half of BOTH.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// jobNoticeFact is the ONE statement of what a completion notice says, without
// the instruction that follows it. It opens the injection message below and it
// is the whole of a single-notice turn opener: the fact is stated by the party
// that opened the turn, and the instruction ("call job(action=output, …)") is
// added by the round boundary that also owns the take.
//
// The `[system] ` prefix (harnessMessagePrefix) is deliberate and load-bearing
// on the turn opener: it is the SAME prefix every harness-injected user message
// carries, so the model reads it as "the harness is telling you this" rather
// than as something the operator typed. The operator never typed it — that is
// the entire point of the feature.
func jobNoticeFact(n tools.JobNotice) string {
	if n.External != nil {
		return sessionMessageFact(*n.External)
	}
	return fmt.Sprintf("%sBackground job %s finished: status=%s code=%d (command: %s).",
		harnessMessagePrefix, n.JobID, n.Status, n.ExitCode, n.Command)
}

// sessionMessageFact is the ONE statement of an INBOUND message notice — the
// text an external caller handed this session with
// `gopi notify --session <id> --message <text>` (internal/tools/session_message.go).
//
// The external caller's text is inserted VERBATIM: it is a quotation, and
// rewording it would be gopi editing somebody else's message. The harness
// prefix is on it for the same reason a completion notice carries one — the
// operator never typed these words, and the model must read them as the harness
// telling it something rather than as its own user speaking.
func sessionMessageFact(m tools.ExternalMessage) string {
	return fmt.Sprintf("%s%s%s",
		harnessMessagePrefix, sessionevent.HarnessInboxNoticeWord, m.Message)
}

// jobNoticeInjectionMessage is the message the turn's round boundary injects
// for ONE notice: the fact plus the instruction that keeps the model from
// re-running the job. Byte-identical to the sentence this seam has always
// added (the fact half is now shared with the turn opener, which is the point
// — nothing about the injection changed).
func jobNoticeInjectionMessage(n tools.JobNotice) string {
	if n.External != nil {
		// An external message stands alone: it names no job to inspect, so the
		// completion notice's follow-up instruction would tell the model to
		// call job(action=output) on nothing.
		return jobNoticeFact(n)
	}
	return fmt.Sprintf("%s Call job(action=output, job_id=%q) when you need its output — do not re-run it.",
		jobNoticeFact(n), n.JobID)
}

// injectJobNotice puts ONE notice's message into the conversation AND mirrors
// it into the session JSONL.
//
// WHY THE LOGGING IS PART OF THE INJECTION and not a separate concern: an
// injected wire message that is not in the session record is indistinguishable
// from a missing one (the same rule roundBoundaryInjections and
// maybeChatOpsStoreCheckpoint already obey for their own injections) — and for
// an EXTERNAL message it is worse than untidy, because the session record is the
// only durable evidence that `gopi notify --session <id> --message <text>`
// reached the model at all. The sender's own copy of the text is not evidence
// that anything was read.
//
// One function rather than two call-site pairs: both round boundaries (the chat
// turn's, chat_ops.go, and the task loop's, loop.go) inject through THIS, so the
// message the model reads and the row the JSONL records cannot drift — and
// neither boundary can forget the logging half.
func (a *Agent) injectJobNotice(n tools.JobNotice) {
	msg := jobNoticeInjectionMessage(n)
	a.Ctx.AddUserMessage(msg)
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), msg)
	}
}

// noticesToInject filters the notices a round boundary just DRAINED down to the
// ones it must inject: those whose statement this conversation does not already
// carry.
//
// THE TAKE STAYS AT THE BOUNDARY (it is the boundary's own act, through the ONE
// take point `tools.TakeJobNotices`, and `TestSessionInboxLoadHasOneTakePoint`
// pins both call sites to the files that also call loadSessionInbox). What lives
// here is only the DECISION, in one place for both boundaries.
//
// THE DUPLICATE THIS PREVENTS, measured rather than imagined. A console parked
// at its prompt mints its next turn from the queue itself, and that turn's
// OPENING MESSAGE is composed by ChatJobNoticeOpenLine — which, for an external
// message, IS the notice statement (`sessionMessageFact`) and goes into the
// conversation (and the session record) as the turn's user message. Injecting
// the same notice at the round boundary a moment later states the IDENTICAL
// sentence a second time, and the operator reads one delivery as two
// notifications: before the fix, a request built for such a turn carried the
// statement twice (internal/agent/session_message_notice_once_test.go).
//
// THE TEST IS THE CONVERSATION, not the notice's kind, and that is deliberate:
//
//   - a message that arrived WHILE the console was parked has its statement in
//     the history (the opener put it there) → withhold the injection;
//   - the SAME message arriving while the console is MID-TURN has no opener and
//     no history row → inject it here, exactly as before. Deciding by kind would
//     drop it: the withhold would cover a delivery that was never stated
//     anywhere.
//
// CONTAINMENT on the INJECTION MESSAGE, not on some paraphrase of it: the
// question is "does this conversation already carry the very sentence this
// boundary is about to add", and the sentence is jobNoticeInjectionMessage(n).
//
// WHY NOT THE BARE FACT (jobNoticeFact), which is what the opener carries for a
// job: a job notice's injection is the fact PLUS the follow-up instruction
// ("Call job(action=output, …)"), so for a job the boundary's message is not in
// the history and IS injected — exactly as designed, and that is the half that
// keeps the model from re-running the job. For an EXTERNAL message the injection
// is the fact alone (there is no job to inspect), so when the console's opener
// already stated it, the injection is the IDENTICAL sentence — the duplicate the
// operator saw. Matching the injection message therefore draws the line where
// the duplication actually is, and needs no second rule about which kind of
// notice this is.
//
// Matching is on the TEXT, not on the notice's id: the id is not in the sentence
// at all (opener and injection build the same text from the same message), and a
// text match cannot withhold a notice whose sentence this conversation lacks.
func (a *Agent) noticesToInject(pending []tools.JobNotice) []tools.JobNotice {
	if len(pending) == 0 {
		return nil
	}
	stated := a.conversationUserTexts()
	out := make([]tools.JobNotice, 0, len(pending))
	for _, n := range pending {
		if msg := jobNoticeInjectionMessage(n); msg == "" || statedContains(stated, msg) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// statedContains reports whether any text in the conversation carries s.
func statedContains(texts []string, s string) bool {
	for _, t := range texts {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// conversationUserTexts is every USER-message text this turn's request would
// carry — the READ by which noticesToInject decides whether a notice's statement
// is already in the conversation.
//
// It reads `GetMessages` (the SAME slice the request is built from) rather than
// the session log: the log is a durable record written by several parties, while
// this question is strictly about what is on the wire for the round about to be
// sent. An injected message reaches both at the same moment (injectJobNotice), so
// the two agree — and the wire copy is the one that decides.
func (a *Agent) conversationUserTexts() []string {
	if a.Ctx == nil {
		return nil
	}
	msgs := a.Ctx.GetMessages()
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "user" && m.Content != "" {
			out = append(out, m.Content)
		}
	}
	return out
}

// ChatJobNoticeOpenLine is the opening user message of a turn minted by a
// completion notice, for the notices the caller PEEKED (never took): the
// carrier must be able to say why the turn exists without becoming a second
// take point, so this reads a peek and leaves the inbox untouched for the
// round boundary.
//
// One notice: the fact alone (jobNoticeFact) — short, and it names the job id
// the model needs for the follow-up output call. Several: one line naming the
// count and the ids, because the boundary injects each job's own full fact one
// moment later and a second copy of every command line would be noise.
//
// The multi form is not a summary of a count nobody spent: it carries EVERY id
// the peek saw, so a turn that opened for three jobs names all three.
//
// AN EXTERNAL MESSAGE IS A SPECIAL CASE OF "the boundary injects it later": for
// a message an outside process delivered, the opening line IS the whole notice
// (`sessionMessageFact`), and the round boundary must therefore NOT state it
// again — `Agent.noticesToInject` withholds exactly the notices whose
// statement this conversation already carries, which is the duplicate this
// function's single-notice form used to produce (see its comment there).
func ChatJobNoticeOpenLine(notices []tools.JobNotice) string {
	if len(notices) == 1 {
		return jobNoticeFact(notices[0])
	}
	parts := make([]string, 0, len(notices))
	for _, n := range notices {
		parts = append(parts, JobNoticeLabel(n))
	}
	return fmt.Sprintf("%s%d%s%s%s; each one's own notice is injected before the next request is built.",
		harnessMessagePrefix, len(notices), sessionevent.HarnessInboxPendingWord,
		strings.Join(parts, ", "), sessionevent.HarnessInboxOpenedWord)
}

// JobNoticeLabel names ONE pending notice without stating its content: the job
// id for a completion notice, and the delivery id for an external message
// (which has no job behind it — `gopi notify --session … --message …` merged it
// into the same inbox, internal/tools/session_message.go). Exported because the
// REPL prints the same label in its own operator-facing note, and two namings
// of one notice would drift.
func JobNoticeLabel(n tools.JobNotice) string {
	if n.External != nil {
		return fmt.Sprintf("external message %s", n.External.ID)
	}
	return n.JobID
}

// JobNoticeSources reports the provenance of every EXTERNAL message among the
// given notices, in first-appearance order and without repeats: the face that
// wrote each inbox record (internal/inputsource — the deliver face stamps
// notify). A notice produced by a background job carries no external message and
// therefore contributes nothing.
//
// It is exported for the ONE caller that must state WHERE a turn's line came
// from: the chat boundary stamps the source on the reference it renders when it
// spills an over-threshold line (chatSealSource, chat_boundary.go). The
// vocabulary is the inbox record's own `source` field — read here rather than
// re-derived, so the reference and the inbox cannot disagree.
func JobNoticeSources(notices []tools.JobNotice) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range notices {
		if n.External == nil || n.External.Source == "" || seen[n.External.Source] {
			continue
		}
		seen[n.External.Source] = true
		out = append(out, n.External.Source)
	}
	return out
}
