package main

// web_resume.go — `gopi web <session-id>`: the browser chat UI CONTINUES an
// existing session instead of minting a fresh one.
//
// WHY IT EXISTS. The web server has always built ONE agent for the whole run
// (one session id, one JSONL, one raw log) and that agent was ALWAYS fresh: an
// operator who closed the tab and came back got a new, empty conversation while
// the previous one sat untouched in its run directory. `gopi chat` solved the
// same problem with the positional seed (`gopi chat <session-id>` → an
// agent.RunIdentity handed to newAgentResuming); the web face now takes that
// same address, so there is ONE answer to "how do I continue a session" and no
// second implementation of it (Rule 14).
//
// WHAT IS REUSED, VERBATIM:
//   - resolveResumeLogPath   the ONE operand→raw-log resolver (session id, task
//                            id or raw-log path; the ledger row's own record
//                            first, the id layouts second).
//   - chatSessionFileFor     the record whose session_start names that raw log,
//                            so the id this run continues and the record it
//                            reads options from are ONE answer.
//   - chatReadRecordedOptions the recorded start-up switches (the same reader
//                            `gopi chat --resume` uses).
//   - agent.LoadCheckpointWithSource
//                            the conversation seed, with the crash-dump
//                            fallback and its provenance.
//   - agent.RunIdentity      the four facts a resume carries instead of minting.
//   - resumeTakeRunLock      the kernel lock on the run directory, so a browser
//                            session and a `gopi session resume` cannot execute
//                            one directory at the same time.
//
// WHAT IS DELIBERATELY NOT DONE: the resume GATES of `gopi session resume`
// (holder liveness, ledger status, deliverable ground). Those judge a TASK ROW
// being re-dispatched. A web session is a conversation the operator is holding
// in a browser — there is no row to re-judge, and refusing to reopen a chat
// because a ledger row is finished would apply another face's rule to a
// question it does not answer. The one rule that DOES carry over is the kernel
// one, and that is the run lock above.
//
// OPTIONS. This run's command line still wins (`--tools`, `--thinking-live`,
// `--addr`); the continued session's RECORDED switches fill only the silence —
// the same precedence chatApplyRecordedResumeOptions documents. A restored
// switch is REPORTED (Rule 15): a capability that came back from a record must
// not be indistinguishable from one the operator typed.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/gopilog"
)

const (
	// webResumeProgName is the ONE spelling this face's diagnostics use.
	webResumeProgName = "gopi web"

	// webResumeUsageSynopsis is the synopsis line the help page opens with,
	// named on its own so a test asking "does the page advertise the positional
	// address?" compares against the same string the page prints (Rule 14).
	webResumeUsageSynopsis = "  gopi web [SESSION-ID] [--addr :PORT] [--tools] [--thinking-live]"

	// webResumeRestoredFormat reports one switch that came back from the
	// continued session's record. Named (Rule 1) because a test asserts it.
	webResumeRestoredFormat = "%s: restored %s from the continued session's record\n"

	// webResumeContinuingFormat states WHAT this run continues and how much of
	// the conversation it seeded — the only thing an operator can check without
	// reading their own history.
	webResumeContinuingFormat = "%s: continuing session %s — %d history message(s) seeded from %s\n"

	// webResumeNoRecordFormat states that a continued session carries no
	// recorded options. NOT silent: it is the difference between "nothing was
	// recorded" and "something failed to be restored".
	webResumeNoRecordFormat = "%s: the continued session %s recorded no start-up options (it predates the record, or its run had nothing to record) — this run's own switches are the whole configuration\n"

	// webResumeToolsRemedy is the re-run recipe the capability-loss warning
	// names on THIS face. The chat face's remedy is one flag (`--tools`);
	// this face's is a command line, because a web session is addressed
	// POSITIONALLY — `gopi web --tools` alone starts a fresh conversation and
	// would silently answer the loss with a different session. Every word is a
	// constant this face parses (Rule 1/13), never a retyped literal.
	webResumeToolsRemedy = webResumeProgName + " <session-id> " + webToolsFlag

	// webResumeUnappliedFormat reports a switch the continued session recorded
	// that THIS face has no meaning for (a chat-only option such as
	// `--no-editor`). It is a report, never a restore: applying it would be this
	// face pretending to be another (Rule 15 — a record the reader quietly
	// dropped is indistinguishable from a record that was never written).
	webResumeUnappliedFormat = "%s: the continued session recorded %s, which is not a switch of this face — not restored\n"

	// webResumeRecordedFormat / webResumeRecordFailedFormat state the WRITE
	// side, i.e. the record this run leaves for whichever run continues it.
	// The failure half lives in cli_chat_resume_options.go (one writer, shared
	// by every face); this constant only names what was written, so an operator
	// can tell "recorded 1 switch" from "recorded nothing".
	webResumeRecordedFormat = "%s: recording this run's start-up switches for a later continuation: %s\n"
)

// webResumeSeed is the resolved continuation: everything the run needs in order
// to APPEND to the session named on the command line instead of minting one.
// The zero value means "start a fresh session", which is what an argument-free
// `gopi web` gets — so no other path changes.
type webResumeSeed struct {
	// SessionID is the session this run CONTINUES (an existing id, never a new
	// uuid). Its consequences are the run directory, the JSONL name and the raw
	// log name, all derived from it.
	SessionID string
	// RawLogPath is the raw HTTP log to APPEND to.
	RawLogPath string
	// SessionLogPath is the session RECORD to append to.
	SessionLogPath string
	// History is the conversation the continued session had, seeded into the
	// server-owned transcript so the first turn picks up where it left off.
	History []deepseek.Message
	// RecordedOptions are the switch words the session recorded at start-up.
	RecordedOptions []string
	// ToolDefs is how many tool definitions the seeded CHECKPOINT carried — the
	// evidence the continued conversation was tool-enabled. It is what the
	// capability-loss warning reports, so a resumed run can say what it lost
	// instead of only that it has nothing.
	ToolDefs int
	// Lock releases the run directory's kernel lock; the caller MUST call it
	// when the server stops. Always non-nil once the seed resolved (a no-op when
	// nothing was lockable, in which case the notice already said why).
	Lock func()
}

// identity renders the RunIdentity this seed hands to newAgentResuming — the
// SAME type the positional `gopi chat <id>` seed passes, so the two faces
// cannot diverge in what continuing a session means.
func (s webResumeSeed) identity() agent.RunIdentity {
	return agent.RunIdentity{
		SessionID:      s.SessionID,
		ResumedFrom:    s.SessionID,
		RawLogPath:     s.RawLogPath,
		SessionLogPath: s.SessionLogPath,
	}
}

// webResumeSeedFor resolves the positional address `gopi web <id>` into a
// continuation, or returns an error whose text is the whole refusal.
//
// A REFUSAL NAMES ITS CAUSE (Rule 15). The id SHAPE is judged before anything is
// resolved (a uuid written with the wrong dashes is refused by name, with the
// canonical spelling it meant, instead of being joined into a second directory),
// and an operand that resolves to no session record says exactly that.
func webResumeSeedFor(operand string) (webResumeSeed, error) {
	var seed webResumeSeed
	operand = strings.TrimSpace(operand)
	if operand == "" {
		return seed, fmt.Errorf("%s: empty session address", webResumeProgName)
	}
	rawLogPath, err := resolveResumeLogPath(operand)
	if err != nil {
		// A BARE WORD HAS TWO DOORS ON THIS VERB, AND THE REFUSAL NAMES BOTH.
		// `gopi web <word>` is either a session address (this file) or a tool
		// subcommand (`gopi web search|fetch`), so a word that resolves to no
		// session is refused with the address's reason AND the subcommand list —
		// otherwise a misspelt `gopi web serch` would be told only about
		// sessions, and an operator holding a typo would be sent to the wrong
		// door (Rule 15: a refusal names its own cause, including the one the
		// caller may actually have meant).
		hint := ""
		if webLooksLikeBareWord(operand) {
			hint = fmt.Sprintf(" — this verb's other door is a tool subcommand, want %s|%s",
				webGroupSubcmdSearch, webGroupSubcmdFetch)
		}
		return seed, fmt.Errorf("%s: cannot continue %q: %w%s", webResumeProgName, operand, err, hint)
	}
	sp, ok, serr := chatSessionFileFor(operand, rawLogPath)
	if serr != nil {
		// NOT FATAL TO THE CONVERSATION, FATAL TO THE IDENTITY: the session could
		// still be seeded from the checkpoint, but this run could not reuse its
		// id — it would mint a second sessions directory beside it, which is the
		// exact defect agent.RunIdentity's header records. So it refuses.
		return seed, fmt.Errorf("%s: cannot continue %q: %w", webResumeProgName, operand, serr)
	}
	if !ok || sp == "" {
		return seed, fmt.Errorf("%s: cannot continue %q: no session record owns the raw log %s, so this run could not append to the session that holds this conversation (it would mint a second one beside it)", webResumeProgName, operand, rawLogPath)
	}
	seed.SessionID = gopilog.FileToID(sp)
	seed.SessionLogPath = sp
	seed.RawLogPath = rawLogPath
	if seed.SessionID == "" {
		return seed, fmt.Errorf("%s: cannot continue %q: the record %s names no session id, so there is no directory to append to", webResumeProgName, operand, sp)
	}
	ckpt, history, _, provenance, cerr := agent.LoadCheckpointWithSource(rawLogPath)
	if cerr != nil {
		return seed, fmt.Errorf("%s: cannot continue %q: %w", webResumeProgName, operand, cerr)
	}
	if provenance.Source == agent.CheckpointSourceDump {
		fmt.Fprintf(os.Stderr, resumeDumpCheckpointNoticeFormat, webResumeProgName, rawLogPath,
			provenance.Detail(), resumeDumpCheckpointDigest(ckpt))
	}
	seed.History = history
	// HOW MANY TOOLS the continued conversation was generated with. This is the
	// evidence the capability-loss warning reports: a checkpoint that carries
	// tool definitions came from a tool-enabled run, and seeding it into a run
	// with tools off strips a capability the history still refers to.
	seed.ToolDefs = chatResumeToolDefCount(ckpt)
	// THE RECORDED SWITCHES, through the SAME reader `gopi chat --resume` uses
	// (Rule 14). A session with no record says so rather than resuming silently
	// with an empty capability set.
	opts, found, oerr := chatReadRecordedOptions(operand, rawLogPath)
	if oerr != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot read the continued session's recorded options: %v\n", webResumeProgName, oerr)
	}
	if found {
		for _, f := range opts.Flags {
			if f == "" || f == operand || f == rawLogPath {
				continue
			}
			seed.RecordedOptions = append(seed.RecordedOptions, f)
		}
	} else if oerr == nil {
		fmt.Fprintf(os.Stderr, webResumeNoRecordFormat, webResumeProgName, seed.SessionID)
	}
	// THE RUN DIRECTORY IS LOCKED BEFORE ANYTHING WRITES — the same kernel lock,
	// the same notice and the same release discipline as `gopi session resume`
	// (run_lock_resume_seam.go). A directory that is not one of this profile's is
	// NOT lockable and says so there; the release stays a no-op so the caller's
	// defer is unconditional.
	rel, _, lerr := resumeTakeRunLock(webResumeProgName, rawLogPath)
	if lerr != nil {
		return seed, lerr
	}
	seed.Lock = rel
	fmt.Fprintf(os.Stderr, webResumeContinuingFormat, webResumeProgName, seed.SessionID, len(history), rawLogPath)
	return seed, nil
}

// webApplyRecordedOptions restores the switches a continued session recorded for
// the ones this command line did NOT state, and reports each restore.
//
// The precedence is `gopi chat`'s: the command line wins, the record fills the
// silence. Only the two switches this face owns are applied — an option recorded
// by a chat face (`--no-editor`) has no meaning on a browser server, and applying
// it would be this face pretending to be another.
func webApplyRecordedOptions(recorded []string, stated func(string) bool, toolsEnabled, thinkingEnabled bool) (bool, bool) {
	for _, f := range recorded {
		switch f {
		case webToolsFlag:
			if stated(webToolsFlag) {
				continue
			}
			toolsEnabled = true
			fmt.Fprintf(os.Stderr, webResumeRestoredFormat, webResumeProgName, webToolsFlag)
		case webThinkingFlag:
			if stated(webThinkingFlag) {
				continue
			}
			thinkingEnabled = true
			fmt.Fprintf(os.Stderr, webResumeRestoredFormat, webResumeProgName, webThinkingFlag)
		default:
			// A RECORDED SWITCH THIS FACE DOES NOT OWN IS REPORTED, NOT DROPPED
			// SILENTLY. The chat face records `--no-editor` and `--no-color`;
			// neither means anything to a browser server, so neither is applied
			// — but an operator who reads "the switches it recorded were
			// restored" must be able to see WHICH one was not.
			if webSwitchOfThisFace(f) {
				continue // a switch of this face, already handled above
			}
			fmt.Fprintf(os.Stderr, webResumeUnappliedFormat, webResumeProgName, f)
		}
	}
	return toolsEnabled, thinkingEnabled
}

// webSwitchOfThisFace reports whether a recorded word is one of the switches
// THIS face parses. The apply loop and the "recorded but not mine" report must
// answer the same question with the same list, or a word could be silently
// dropped by one and reported by the other (Rule 14 — derived, never a second
// hand-maintained list).
func webSwitchOfThisFace(word string) bool {
	switch word {
	case webToolsFlag, webThinkingFlag:
		return true
	}
	return false
}

// webWarnSeededToolsMismatch reports a REAL capability loss: the continued
// session's checkpoint carried tool definitions, and this run has tools off.
//
// It must be called AFTER webApplyRecordedOptions has run, not before. The
// measured reason is the chat face's (cli_chat_resume_options.go): warning at
// the seed printed "tools OFF" on one line and "restored --tools" on the next,
// two consecutive lines contradicting each other. The remedy sentence therefore
// appears only when the loss is still real once every restore has been applied.
func webWarnSeededToolsMismatch(toolDefs int, toolsEnabled bool) {
	if warn := seededToolsMismatch(webResumeProgName, webResumeToolsRemedy, toolDefs, toolsEnabled); warn != "" {
		fmt.Fprint(os.Stderr, warn)
	}
}

// webRecordedSwitches is the switch vocabulary THIS face records for a later
// continuation — derived from what the run is about to DO, never from a re-parse
// of argv (Rule 14: what is recorded is what this run actually got, after the
// env defaults and after any restore from the continued session's record).
//
// A run with both switches off records NOTHING, which is the right answer:
// LogResumeOptions writes no event for an empty slice, so "nothing to record"
// stays distinguishable from "recorded an empty command line" (Rule 15).
func webRecordedSwitches(toolsEnabled, thinkingEnabled bool) []string {
	flags := make([]string, 0, 2)
	if toolsEnabled {
		flags = append(flags, webToolsFlag)
	}
	if thinkingEnabled {
		flags = append(flags, webThinkingFlag)
	}
	return flags
}

// webRecordResumeOptions records this run's start-up switches into its OWN
// session log, BEFORE the first turn, through the ONE writer the chat face
// uses (recordResumeOptions in cli_chat_resume_options.go).
//
// THIS IS THE HALF THAT WAS MISSING (measured 2026-11-07, row 01a1b50c). The
// read side (webResumeSeedFor → webApplyRecordedOptions) was fully wired and
// always received an empty slice, because nothing on this face ever WROTE the
// record: `chatRecordResumeOptions` had exactly one caller and it was the chat
// REPL. The observable consequence was a continuation that silently lost the
// tool capability — the page then showed the model's native tool-call syntax as
// plain text, because the request carried no tools array.
//
// It is called once the agent (and therefore the session directory and JSONL)
// exists and before the HTTP server serves a turn — the run this exists for is
// the one that dies mid-turn.
func webRecordResumeOptions(ag *agent.Agent, flags []string) {
	if len(flags) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, webResumeRecordedFormat, webResumeProgName, strings.Join(flags, chatFlagListSep))
	recordResumeOptions(webResumeProgName, webResumeToolsRemedy, ag, flags)
}

// webSwitchStated reports whether argv states the switch (bare or joined form),
// which is the fact the restore's precedence turns on.
func webSwitchStated(args []string, word string) bool {
	for _, a := range args {
		if a == word || strings.HasPrefix(a, word+"=") {
			return true
		}
	}
	return false
}

// webLooksLikeBareWord reports whether the operand could be either an address or
// a tool subcommand — a single word with no path separator and no id shape. It
// decides nothing about whether the word RESOLVES (the resolver already said
// no); it decides only which second door the refusal names.
func webLooksLikeBareWord(operand string) bool {
	if operand == "" || strings.ContainsAny(operand, string(os.PathSeparator)) {
		return false
	}
	return !strings.Contains(operand, "-")
}
