package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/gopilog"
)

// This file is the READ half of chat resume: it turns the options a session
// RECORDED at start-up (internal/agent.SessionLogger.LogResumeOptions) back
// into the run's own switches.
//
// THE DEFECT IT CLOSES (measured 2026-09-29, task 01a0eb0e…). A checkpoint
// restores a conversation and a tool DEFINITION list, never a command line. So
// `gopi chat --tools` that was SIGKILLed and resumed as `gopi chat <id>`
// inherited a history full of tool_use/tool_result turns with no tools behind
// it: the model answered by DESCRIBING the tool call it could not make. The
// previous fix said so loudly ("re-run with --tools") — which leaves the
// operator replaying flags from memory, the thing this file removes.
//
// PRECEDENCE (one rule, stated once): the command line and the record are
// COMBINED, and the command line is never removed by the record. There is no
// negative spelling to conflict with except the one value that genuinely has
// two candidates — the screen-dump destination — and there the explicit
// command line wins (a recorded dump path is adopted only when this run was
// asked for no dump at all).

// chatWarnSeededToolsMismatch emits the capability warning in the ONE place it
// can be true.
//
// ORDER IS THE WHOLE POINT, and getting it wrong printed a lie (measured in the
// live sandbox run for this feature, 2026-10-01):
//
//	[gopi] chat: ⚠ the seeded session was TOOL-ENABLED (18 tool definition(s) ...) but this run has tools OFF ...
//	[gopi] chat: restored 2 option(s) this session was started with: --tools, --no-editor
//
// Two consecutive lines contradicting each other. The warning was decided at
// the seed, before the recorded options were applied, so it reported a
// CAPABILITY LOSS that the very next line repaired. It must therefore run AFTER
// the apply — and the remedy sentence only appears when the loss is real.
func chatWarnSeededToolsMismatch(ckpt *deepseek.ClaudeRequest, toolsEnabled bool) {
	if warn := chatSeededToolsMismatch(ckpt, toolsEnabled); warn != "" {
		fmt.Fprint(os.Stderr, warn)
	}
}

// chatRecordResumeOptions writes this run's recorded options into its own
// session log and SYNCs, so the record survives the crash the feature exists
// for (internal/agent.SessionLogger.LogResumeOptions carries the rationale).
//
// It must be called AFTER the run's session is minted and BEFORE the first
// turn: the run this exists for is the one that dies mid-turn.
//
// A failure is REPORTED and never fatal: the record is what a LATER run reads,
// and losing it costs the operator the auto-replay, not this run. Silence
// would be the defect (Rule 15) — a resume that restores nothing because
// nothing was written must not look like a session that had nothing to record.
func chatRecordResumeOptions(ag *agent.Agent, flags []string) {
	recordResumeOptions(chatResumeRecordProgName, chatFlagResume, ag, flags)
}

// recordResumeOptions is the ONE writer of the resume record, shared by every
// face that can be continued (Rule 14). The chat face and the web face differ
// only in who they are and how a later run addresses the session again, so
// those two facts are PARAMETERS — not a second copy of this function.
//
// WHY IT IS SHARED RATHER THAN COPIED, with the measured case (2026-11-07, row
// 01a1b50c). `gopi web --tools` was never recorded, because this writer had
// exactly one caller and it was `gopi chat`'s. The web face's READ side
// (webApplyRecordedOptions) was fully wired and always received an empty slice,
// so a continued web session silently ran with no tools: its history carried
// `tool_use` blocks, the request carried no tools array, and the model emitted
// its native tool-call syntax as plain text in the page. A reader that exists
// and a writer that never fires is indistinguishable, from the operator's seat,
// from a resume that had nothing to restore — which is why the ABSENCE is
// stated by the reader (webResumeNoRecordFormat) and the WRITE failure is
// stated here.
func recordResumeOptions(progName, resumeHint string, ag *agent.Agent, flags []string) {
	if ag == nil || ag.SessionLog == nil || len(flags) == 0 {
		return
	}
	if err := ag.SessionLog.LogResumeOptions(flags, chatRunWorkdir()); err != nil {
		fmt.Fprintf(os.Stderr, resumeRecordFailedFormat, progName, err, resumeHint)
		return
	}
	if err := ag.SessionLog.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, resumeRecordFlushFailedFormat, progName, err)
	}
}

// resumeRecordFailedFormat / resumeRecordFlushFailedFormat state the two ways
// the record can fail to reach the disk. Named (Rule 1) because both faces
// print them through one writer and a test asserts the web face's sentence.
const (
	resumeRecordFailedFormat      = "%s: cannot record the resume options (%v) — a later %s of this session will not restore them\n"
	resumeRecordFlushFailedFormat = "%s: cannot flush the resume options to disk (%v) — a crash before the next flush loses them\n"
	// chatResumeRecordProgName is the chat face's spelling in those sentences.
	chatResumeRecordProgName = "[gopi] chat"
)

// chatRunWorkdir is the directory this run was started in, as the resume
// record reports it. A read failure yields "" — the field is a report, never a
// gate, and an unreadable cwd must not stop a chat from starting.
func chatRunWorkdir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// chatStatedOptions reports whether the command line STATED any option this
// verb records, so the autodetect path knows whether the operator's silence
// was total. The dump flag is tested on the ARGV, not on dumpOpts: that value
// may have come from the ENVIRONMENT, which is not a statement about this
// run's options and must not suppress the recorded ones.
func chatStatedOptions(args []string) bool {
	for _, a := range args {
		if chatResumeOptionWord(a) {
			return true
		}
		if a == chatDumpFlag || strings.HasPrefix(a, chatDumpFlag+chatFlagValueSep) {
			return true
		}
		// The RAW stream flag names a DESTINATION too, so it is tested on the
		// ARGV for the dump's reason: a value that came from the environment is
		// not a statement about this run's options, and an explicit flag must stop
		// the autodetect from restoring the destination a crashed run recorded.
		// The OFF switch is a statement in exactly the same sense — it says which
		// state this run is in, so it must not be overruled by a recording.
		if a == chatSgrStreamFlag || strings.HasPrefix(a, chatSgrStreamFlag+chatFlagValueSep) ||
			a == chatNoSgrStreamFlag || strings.HasPrefix(a, chatNoSgrStreamFlag+chatFlagValueSep) ||
			// The PRE-RENAME spellings state the same thing (they are the same
			// switch, and parseChatSgrArg honours them), so a command line that used
			// one must stop the autodetect exactly as the current word does —
			// otherwise the deprecated form would parse AND be overruled, which is
			// the quiet drop this predicate exists to prevent (Rule 15).
			a == chatSgrStreamDeprecatedFlag || strings.HasPrefix(a, chatSgrStreamDeprecatedFlag+chatFlagValueSep) ||
			a == chatNoSgrStreamDeprecatedFlag || strings.HasPrefix(a, chatNoSgrStreamDeprecatedFlag+chatFlagValueSep) {
			return true
		}
	}
	return false
}

// chatSessionFileFor resolves the SESSION RECORD whose recorded options belong
// to the run being continued, from what the caller wrote (seed) and from the
// raw log this run already resolved.
//
// It never guesses a different session: a seed that IS a session file is used
// as-is, and otherwise the session is the one that RECORDED this raw log as
// its own (gopilog.SessionsRecordingRawLog — the same scoped lookup `gopi session raw`
// uses, so the two cannot disagree about who owns a raw log). More than one
// owner means the file is SHARED by two sessions and the answer is ambiguous:
// that is refused by name rather than resolved to whichever came first.
func chatSessionFileFor(seed, rawLogPath string) (string, bool, error) {
	if st, err := os.Stat(seed); err == nil && st.Mode().IsRegular() {
		// The seed IS a file. A session record is identified by its
		// session_start event, not by its name, so a raw log passed here falls
		// through to the ownership lookup below instead of being misread as an
		// (empty) record.
		if _, err := gopilog.SessionStartTime(seed); err == nil {
			return seed, true, nil
		}
	}
	if rawLogPath == "" {
		return "", false, nil
	}
	ids := gopilog.SessionsRecordingRawLog(rawLogPath)
	switch len(ids) {
	case 0:
		return "", false, nil
	case 1:
		dir, err := gopilog.ResolveSessionDir()
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve session dir: %w", err)
		}
		p, err := gopilog.FindSession(dir, ids[0])
		if err != nil {
			return "", false, fmt.Errorf("cannot resolve session %q: %w", ids[0], err)
		}
		if p == "" {
			return "", false, nil
		}
		return p, true, nil
	default:
		return "", false, fmt.Errorf(
			"the raw log %s is recorded by %d sessions (%s) — resume one of them by its own session id, so the options cannot be read from the wrong run",
			rawLogPath, len(ids), strings.Join(ids, chatFlagListSep))
	}
}

// chatReadRecordedOptions reads the resume record for the session being
// continued. found=false means the session carries NO record: it predates the
// event, or the run had nothing to record. That is a SENTENCE to the operator,
// never a silent default (Rule 15) — the caller prints it.
func chatReadRecordedOptions(seed, rawLogPath string) (opts gopilog.ResumeOptions, found bool, err error) {
	sessionPath, ok, err := chatSessionFileFor(seed, rawLogPath)
	if err != nil {
		return gopilog.ResumeOptions{}, false, err
	}
	if !ok {
		return gopilog.ResumeOptions{}, false, nil
	}
	return gopilog.ResumeOptionsFromFile(sessionPath)
}

// chatApplyRecordedResumeOptions applies the recorded options of the session
// this run continues, reports what it restored, and returns the option words
// this run INHERITED (so the run can re-record them — see chatRecordableFlags
// for why the chain needs it).
//
// forced=false is the AUTODETECT path (the positional seed, nothing stated on
// the command line): a missing record is silent, because the operator asked to
// continue a conversation, not to restore options.
//
// forced=true is `--resume`, where the operator asked for the recorded options
// by name: a missing record is still not fatal — an older session has no
// record to restore and its conversation is resumable regardless — but it is
// REPORTED as a loss, because the capability the crashed run had cannot be
// recovered from anywhere else.
func chatApplyRecordedResumeOptions(sess *chatSession, noEditor, noColor, verbose *bool,
	dumpOpts *chatDumpOptions, sgrOpts *chatSgrOptions, sgrStated bool, seed, rawLogPath string, forced bool) (inherited []string, status int, err error) {
	opts, found, err := chatReadRecordedOptions(seed, rawLogPath)
	if err != nil {
		return nil, chatExitFailed, err
	}
	if !found {
		if forced {
			fmt.Fprintf(os.Stderr, chatResumeNoRecordNoticeFormat, chatFlagResume, chatFlagTools)
		}
		return nil, chatExitOK, nil
	}
	// The record's own target is not an option (the writer never records one),
	// but a hand-edited or foreign record could carry one: dropped here rather
	// than applied, and never inherited into the next record.
	flags := make([]string, 0, len(opts.Flags))
	for _, f := range opts.Flags {
		if f == "" || f == seed || f == rawLogPath {
			continue
		}
		flags = append(flags, f)
	}

	applied, superseded := chatApplyResumeFlags(sess, noEditor, noColor, verbose, dumpOpts, sgrOpts, flags, dumpOpts.stated, sgrStated)
	unknown := chatUnknownResumeFlags(flags, applied, superseded)

	if len(applied) > 0 {
		fmt.Fprintf(os.Stderr, chatResumeRestoredFormat, len(applied), strings.Join(applied, chatFlagListSep))
	}
	if len(superseded) > 0 {
		fmt.Fprintf(os.Stderr, chatResumeSupersededFormat, strings.Join(superseded, chatFlagListSep))
	}
	if len(unknown) > 0 {
		fmt.Fprintf(os.Stderr, chatResumeUnknownFormat, strings.Join(unknown, chatFlagListSep))
	}
	if opts.Workdir != "" {
		if wd, err := os.Getwd(); err == nil && wd != opts.Workdir {
			fmt.Fprintf(os.Stderr, chatResumeOtherWorkdirFormat, opts.Workdir, wd)
		}
	}
	return applied, chatExitOK, nil
}

// The three lines a resume prints (Rule 1b: spelled once, here, and referenced
// by the tests that assert them).
const (
	// chatResumeRestoredFormat reports WHAT came back. It is the answer to "is
	// my session really the one I left", which is the only thing an operator
	// can check without reading their own history.
	chatResumeRestoredFormat = "[gopi] chat: restored %d option(s) this session was started with: %s\n"
	// chatResumeSupersededFormat reports a recorded option this run's own command
	// line outranked — an explicit choice is never silently overridden.
	chatResumeSupersededFormat = "[gopi] chat: note — this run's own command line stands for: %s (recorded, not adopted)\n"
	// chatResumeUnknownFormat reports a recorded word this binary does not know:
	// recorded by another gopi VERSION. Named, never dropped in silence — a
	// resume that restores less than it captured must say so.
	chatResumeUnknownFormat = "[gopi] chat: ⚠ recorded option(s) this binary does not know were NOT restored: %s — they were recorded by another gopi version\n"
	// chatResumeNoRecordNoticeFormat is the honest report for `--resume` on a
	// session that recorded NOTHING (it predates the record, or that run had no
	// options): the conversation is resumed, the options are not.
	chatResumeNoRecordNoticeFormat = "[gopi] chat: %s asked for the options this session was started with, but it recorded none (it predates the record, or that run had no options) — the conversation is resumed, the options are not. Replay them by hand if it was a %s run.\n"
	// chatResumeOtherWorkdirFormat reports that the session was started
	// somewhere else: the restored options describe THAT run's context, and a
	// caller who is not told cannot understand why a restored path is odd.
	chatResumeOtherWorkdirFormat = "[gopi] chat: note — this session was started in %s, this run is in %s\n"
	// chatResumeTurnBaselineFormat states the turn number the session's own log
	// already carries, i.e. what the FIRST new turn of this run will be numbered.
	// It is printed because the two numbers used to disagree (task 01a16fb3: the
	// screen said turn 1 of a session `session log --turns` called 19 turns long),
	// and a reader who is told the baseline can see which one they are in.
	chatResumeTurnBaselineFormat = "[gopi] chat: this session already holds %d turn(s) — the next turn is turn %d\n"
	// chatResumeIDUnresolvedFormat reports a resume that could NOT reuse the
	// session's id (its record could not be resolved). The conversation is still
	// resumed from the checkpoint, but this run mints its own id — which leaves a
	// SECOND sessions/ directory beside the one the operator asked to continue.
	// That is exactly the defect the id-reuse fix removes, so a run that falls
	// back to it must say so instead of letting the operator find the extra
	// directory later (Rule 15). The one failure left after the fix is a resume
	// from a BARE RAW LOG with no session record behind it: there is no id to
	// reuse, so there is nothing to continue but the conversation.
	chatResumeIDUnresolvedFormat = "[gopi] chat: cannot resolve the session record this resume continues (%v) — the conversation is resumed, but this run keeps its OWN session id, so its turns land in a new sessions/ directory\n"
)

// chatResumeTurnBaseline reports how many turns the session this run continues
// ALREADY HOLDS — the ORDINAL the next turn-opening row will carry, so that the
// number this run's heading and status row print is the number the operator can
// hand to `gopi session log <id> --turns` and `gopi session replay <id> --turn N`
// and land on the SAME turn.
//
// IT COMES FROM THE LOG'S OWN TURN COUNT (gopilog.SessionTurns, i.e.
// SessionTurnBounds), WHICH IS THE PRODUCER OF THE ORDINALS THEMSELVES. That is
// the whole point, and it is what the LEDGER route got wrong.
//
// THE DEFECT IT REPLACES, MEASURED (2026-11-04, session
// e1e22e9f-eee0-4c33-a238-0fca04534ad9, read at one instant):
//
//	session show <id>            turns 619      ← chats.turn_count (the LEDGER)
//	session log <id> --turns     rows to 621    ← SessionTurnBounds (the LOG)
//	session replay <id> --turns  turns=621      ← the same LOG producer
//	the screen's own heading     620            ← LEDGER baseline + this run
//
// and per turn the gap was constant: 16 of 16 readable banners sat exactly ONE
// BELOW the row carrying the same prompt (banner 592 ↔ row 593 … 620 ↔ 621). So
// the operator copying `turn 605` off the screen into `replay --turn 605` was
// shown a DIFFERENT turn — the epic's own reproduction.
//
// WHY THE LEDGER IS THE WRONG ANCHOR, and not merely a stale one. `chats.
// turn_count` is bumped only by a turn that FINISHED (`RecordChatTurnOpened` runs
// on `runErr == nil && !interrupted` — an interrupted or failed turn registers
// nothing), so it counts COMPLETED turns; a turn-opening row is a different
// quantity and there are arbitrarily many of them behind the same number. Two
// producers, two quantities, one word on the screen (Rule 14). The ordinals the
// reader prints are positional — a row cannot have a hole — so the anchor has to
// be the row count, and that is SessionTurns.
//
// THE EARLIER RULING THIS SUPERSEDES IS NOT WRONG, IT IS OUT OF DATE: it was made
// when SessionTurns WAS the person-only SUBSET (measured 2026-10-27 on
// a60787b0-9081-451f-bd5a-9e396cfc048b: log 8 operator rows, ledger 16 turns), so
// reading the log under-numbered a notify-driven session. That split has since
// been made explicit — SessionTurns is the TOTAL and SessionHumanTurns is the
// subset (`internal/gopilog/sessionround.go`) — so the reason to reach for the
// ledger is gone, and what is left of it is a baseline one producer behind the
// ordinals on screen. `session show`'s `turns` stays the ledger's number: it is
// the COMPLETED-turn count and it says so by its own provenance sentence
// (gopilog.TurnCountSource).
//
// Zero is a real answer, not a failure: a fresh session holds none, and a
// session record that cannot be resolved (no record at all — the operator
// resumed a bare raw log) gives 0, which leaves the numbering exactly as it was.
// A log that EXISTS but cannot be READ is an error, never a quiet zero: numbering
// from 1 would put the operator straight back into the defect this removes, with
// nothing saying so (Rule 15).
func chatResumeTurnBaseline(seed, rawLogPath string) (int, error) {
	path, ok, err := chatSessionFileFor(seed, rawLogPath)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return chatTurnOrdinalAt(path)
}

// chatApplyResumeFlags applies the recorded options to the run's own switches
// and reports which were APPLIED and which were SUPERSEDED by this command
// line.
//
// It is a pure function of (the run's switches, the recorded words, whether the
// command line STATED a dump choice), which is what lets the precedence be
// tested without starting a chat — the whole rule is here, stated once.
//
// The one precedence: a recorded dump word is adopted only when this command
// line stated no dump choice at all (dumpStated=false), because that option has
// two candidate VALUES and an explicit one is a choice, not a default. Every
// other recorded word is a switch this run either has or has not, so applying it
// can only ADD capability.
//
// dumpStated is about STATEMENTS, not about the default: the dump is armed by
// default now, so a run with a bare command line carries an ARMED request that
// nobody asked for — if that counted as "stated", every resumed session would
// silently supersede the destination the crashed run had recorded. The caller
// therefore passes the request's `stated` flag, not its `requested` flag.
func chatApplyResumeFlags(sess *chatSession, noEditor, noColor, verbose *bool,
	dumpOpts *chatDumpOptions, sgrOpts *chatSgrOptions, flags []string, dumpStated, sgrStated bool) (applied, superseded []string) {
	for _, f := range flags {
		if f == "" {
			continue
		}
		if parsed, matched, perr := parseChatSgrArg(f, "", false); matched {
			if perr != nil || (!parsed.enabled && !parsed.disabled) {
				continue // not a usable record: chatUnknownResumeFlags reports it
			}
			if sgrStated {
				superseded = append(superseded, f)
				continue
			}
			*sgrOpts = parsed
			applied = append(applied, f)
			continue
		}
		if parsed, matched, perr := parseChatDumpArg(f, "", false); matched {
			if perr != nil || (!parsed.requested && !parsed.disabled) {
				continue // not a usable record: chatUnknownResumeFlags reports it
			}
			if dumpStated {
				superseded = append(superseded, f)
				continue
			}
			*dumpOpts = parsed
			applied = append(applied, f)
			continue
		}
		if chatREPLSwitch(f, sess, noEditor, noColor, verbose) {
			applied = append(applied, f)
		}
	}
	return applied, superseded
}

// chatUnknownResumeFlags returns the recorded words this binary neither applied
// nor recognised — the ones a NEWER or OLDER gopi wrote. It is derived (flags
// minus what was handled), never accumulated beside the apply loop: two lists
// maintained in parallel drift, and the drift would be silent.
func chatUnknownResumeFlags(flags, applied, superseded []string) []string {
	handled := make(map[string]bool, len(applied)+len(superseded))
	for _, f := range applied {
		handled[f] = true
	}
	for _, f := range superseded {
		handled[f] = true
	}
	unknown := make([]string, 0)
	seen := make(map[string]bool, len(flags))
	for _, f := range flags {
		if f == "" || handled[f] || seen[f] {
			continue
		}
		if chatResumeKnownWord(f) {
			continue // recognised, just not applicable to this run — not a version mismatch
		}
		seen[f] = true
		unknown = append(unknown, f)
	}
	return unknown
}

// chatResumeKnownWord reports whether this binary knows the word at all —
// either as one of this verb's own switches or as the dump flag in any spelling.
// It is what separates "recorded by another gopi version" from "known here, but
// it landed in the superseded list".
func chatResumeKnownWord(word string) bool {
	if chatResumeOptionWord(word) {
		return true
	}
	// A word the dump flag OWNS in a shape it refuses — a value handed to the
	// off switch, `--no-dump-screen=x` — is still a word THIS binary knows: the
	// question here is "was it written by another version?", and a refusal is
	// not that. Every error arm of parseChatDumpArg returns matched=true, so
	// the error cannot change this predicate's answer; it is checked rather
	// than discarded because nothing here is allowed to swallow one.
	if _, sgrMatched, sgrErr := parseChatSgrArg(word, "", false); sgrMatched || sgrErr != nil {
		return true
	}
	_, matched, err := parseChatDumpArg(word, "", false)
	if err != nil {
		return true
	}
	return matched
}
