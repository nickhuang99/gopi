// cli_session.go — the `gopi session` VERB FAMILY.
//
// WHAT IT IS. `gopi session` is the group verb over the SESSION surfaces this
// binary already had as top-level verbs: read a session's JSONL log, its
// numbers, its raw wire log, resume it — plus the two this family adds, which
// read the chats TABLE (the chat index of every `gopi chat` / `gopi web`
// conversation): `session list` and `session show`.
//
// WHY A FAMILY AND NOT SIX FLAT VERBS. The six spellings answer ONE question
// ("what did this session do?") about ONE operand (a session id), so
// `gopi session -h` is the page that teaches them together, and `gopi session
// list` PRINTS the very operand every sibling takes (a full session uuid) —
// discoverability by construction rather than by documentation.
//
// THIN ALIASES, ONE IMPLEMENTATION (Rules 13/14). log / stats / raw / resume
// were top-level verbs and REMAIN dispatched top-level verbs: their argv
// predicates and their run functions are unchanged, and `gopi session <verb>`
// reaches the SAME run function by SUBSTITUTING the group word out of argv.
// There is no second renderer, no second parser and no copied flag table —
// `gopi session log <id>` and `gopi session log <id>` produce byte-identical output
// because they are the same function called with the same arguments.
//
// THE PLAYBACK IS HERE, re-homed as promised. `session replay` (the
// recorded-chat renderer) was the door this file reserved for a sibling task;
// it now lives in cli_session_replay.go, reached as `gopi session replay <id>`
// — NOT as a top-level verb, because `gopi replay` was abolished on 2026-10-03
// and stays un-dispatched (devdoc/replay-abolished-verb-single-route.md; the
// regression gate is cmd/agent/session_replay_abolition_test.go). The
// COMPATIBILITY that re-homing needed is unchanged: `session list` prints full
// session uuids — the exact operand every sibling takes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/rundir"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// sessionSubcommandNames is the `gopi session <sub>` vocabulary: the group's
// second-level candidate set, the dispatcher's switch, the unknown-subcommand
// error and the family help all read THIS slice (Rule 13: one definition,
// referenced — never mirrored; the `gopi session <TAB>` completion set is the
// same value, so a sub-verb added here reaches tab completion by construction).
var sessionSubcommandNames = []string{
	sessionSubcmdList,
	sessionSubcmdShow,
	CliVerbLog,
	CliVerbStats,
	CliVerbRaw,
	CliVerbResume,
	// sessionSubcmdReplay is the seventh member and the reason this family
	// exists at all beyond the four readers: the recorded-chat PLAYBACK. It is
	// a SUB-VERB WORD, never a top-level verb — the top-level `gopi replay` was
	// abolished on 2026-10-03 (devdoc/replay-abolished-verb-single-route.md)
	// and is refused by name today (cli_verb_aliases.go's row, which points
	// here).
	sessionSubcmdReplay,
	CliVerbCheckpoint,
	// The ROUND-LEVEL note index's reader (cli_session_notes.go): the notes a
	// session's rounds recorded, searchable. It belongs to this family because a
	// note is a property of a session — and because a new TOP-LEVEL verb is not
	// an option (Rule 3), the group's own second level is where a new reader
	// goes.
	sessionSubcmdNotes,
	// THE CONTEXT EXPORT (cli_session_context.go): the request the model
	// actually received at one turn, written to a file, one line on screen. It
	// is a READER of the raw HTTP log — the same record `session resume` seeds
	// from — and it belongs to this family for the same reason every other
	// member does: its operand is a session id.
	sessionSubcmdContext,
	// THE RECLAIM VERB — the family's first and only destructive member, and the
	// reason it exists: every other member of this slice is a READER, so nothing
	// bounded the tree's growth (90 GB / 3175 directories measured on
	// 2026-10-29). It is a one-shot a human runs (Rule 35: no schedule, no
	// resident), and its default is a dry run.
	sessionSubcmdGC,
}

// The family's own verbs. They are named constants because they are compared in
// the dispatcher, enumerated in the help and offered by completion (Rule 1b:
// never type the word twice).
//
// sessionSubcmdReplay is a SUB-VERB WORD, not a top-level verb: it is the second
// level of the `session` group, and the predicate the verb census counts is that
// group's (isSessionSubcommand). The top-level `gopi replay` was abolished on
// 2026-10-03 and stays un-dispatched
// (devdoc/replay-abolished-verb-single-route.md): the playback lives here.
const (
	sessionSubcmdList   = "list"
	sessionSubcmdShow   = "show"
	sessionSubcmdReplay = "replay"
)

// sessionModeFlag is the `--mode` selector of `session list`. Its two VALUES
// are tools.ChatModeCLI / tools.ChatModeWeb — the chats table's own mode
// column (Rule 13: the schema's vocabulary, never re-typed here).
const sessionModeFlag = "--mode"

// sessionShowKeyWidth is the label column of the `session show` page's key/value
// lines (`  turns       200`). It is a constant so the render and the tests that
// read it cannot drift into two widths (Rule 1: no bare literal).
//
// THE COLUMN IS 12 CELLS, SO EVERY KEY MUST FIT IN 11 — checked by
// TestSessionShowKeysFitTheirColumn (cmd/agent/cli_session_token_total_label_test.go),
// which walks the page's key vocabulary against sessionShowKeyMaxKey. The page
// renders with `%-*s%s`, so a key as wide as the column leaves ZERO separating
// cells and the value is welded to its label. Naming this page's token key
// `total tokens` (exactly 12 cells) did that, and the shipped page read:
//
//	total tokenstotal tok in=109705 out=262 cache=109705
//
// Every substring assertion still passed, because a glued line CONTAINS both
// fragments — which is why TestSessionShowLabelsTheTokenLineAsATotal now asserts
// the SEPARATED form (Rule 31: a surface nobody ran is a surface nobody has
// seen). A second, wider lesson: the page has always mixed two paddings, these
// `%-*s` lines and a block of keys hardcoded at 12 cells (the `ended`, `started`, `workdir` lines),
// so a width change moves half the page and shears the other half. Both halves
// agree at 12, so 12 is the column; the guard below keeps new keys inside
// sessionShowKeyMaxKey and the hardcoded half is left as the page's own shape.
const sessionShowKeyWidth = 12

// sessionShowKeyMaxKey is the widest key the column can carry while still
// leaving one separating cell. A key that cannot fit must be RENAMED, not given
// a wider column: the page's hardcoded lines are 12 cells, so widening shears
// them (see the measurement above).
const sessionShowKeyMaxKey = sessionShowKeyWidth - 1

// sessionShowUnmeasured is the value column's mark for a quantity nobody took.
// It is a DASH, not a 0: a zero is a measurement, and printing one where nothing
// was measured is the fabrication Rule 15 forbids (the note on the next line
// says which channel failed).
const sessionShowUnmeasured = "—"

// sessionJSONFlag is `--json`: the machine-readable rendering of `session list`
// / `session show`, so a script can read the ids without parsing the table.
const sessionJSONFlag = "--json"

// sessionSourceFlag is `--source VALUE` on `session list`: narrow by the row's
// recorded provenance (the tasksource vocabulary, plus tools.ChatSourceUnrecorded
// for the rows that recorded none).
const sessionSourceFlag = "--source"

// sessionAllFlag is `--all` on `session list`: the explicit door to the COMPLETE
// chat index, including the rows whose session log is not on this machine. The
// spelling is tools.ChatListAllFlag — the SAME constant the disclosure line
// names (Rule 13/1b): the line that tells a reader how to reveal the hidden
// rows must print the flag this parser accepts, and one constant is what makes
// that true by construction rather than by review.
const sessionAllFlag = tools.ChatListAllFlag

// sessionListLocalOnly is the DEFAULT of `session list`: show only the sessions
// this machine really holds.
//
// WHY THE DEFAULT IS THIS (operator's ruling, 2026-10-05). `session list --mode
// chat` answered every row the chats index holds, and on the live profile 48 of
// 50 named a session JSONL this machine does not have — the index records where
// a conversation WAS (another host's profile, a reclaimed scratch root, a
// fixture db), so the default view over-reported what is here and could not be
// read to open anything. The ruled default is the machine's OWN sessions; the
// complete index costs one explicit flag.
//
// It is a named constant rather than a bare `true` at the call site (Rule 1):
// the default is the RULING, and a ruling written as an unlabelled boolean is
// one edit away from being silently reversed.
const sessionListLocalOnly = true

// sessionListDefaultLimit caps one `session list` render when --limit is
// absent (an index page, not an export — the same posture as the chat index).
const sessionListDefaultLimit = 50

// sessionUsage is the family page, shaped like `gopi task -h`: one synopsis
// line per sub-verb, the detail one hop away in each verb's own -h.
//
// The prose SPELLS the sub-verb names instead of splicing the constants in:
// a usage text is read by a human, and a spliced literal is one quoting slip
// away from a broken build (measured during this task's own first build). What
// keeps the page honest is a GATE, not the splice —
// TestSessionFamilyUsageAdvertisesEverySubVerb fails if any name in
// sessionSubcommandNames (or either --mode value) stops appearing here, which
// is the same direction the repo's other family help gates check.
//
// EVERY `Usage:` LINE IS ONE COMMAND AN OPERATOR CAN TYPE, and that is checked
// rather than trusted (Rule 5): cli_session_usage_dispatch_test.go refuses an
// ALTERNATION in a word slot of these synopses and drives each derived argv
// through the real dispatcher. The line this wording replaced —
// `gopi session checkpoint list|export    → the raw-log snapshot registry` — is
// the case that forced it (the dispatcher answered `unknown subcommand
// "list|export"`); the checkpoint cross-reference it carried survives as the
// paragraph after the synopses, which is where a note belongs.
const sessionUsage = `GoPi session subcommands — inspect and continue sessions.

Usage:
  gopi session list [--kind KIND] [--mode MODE] [--source WHO] [--limit N]
                    [--all] [--json]
      List EVERY session this machine has, newest first, from TWO sources: the
      chat index (conversations) and the sessions tree (the run logs a task run,
      a one-shot prompt or a resume writes — they register no chat row). One row
      per session: its id, kind, state and, for a run, the pid that wrote it.
      The id is a FULL session uuid, and it is what every sibling verb takes.
      --kind takes ONE of the two species, never a list:
        --kind chat     conversations (a chats-table row)
        --kind run      run logs (a task run, a one-shot prompt, a resume)
      What the view did NOT show is printed as numbers on its own lines:
      [filtered] dropped by the filter, [hidden] a chat row whose log is not
      here, [limit] cut by --limit, [scanned] a directory with no run log.
      "turns" is THE turn count (chats.turn_count, every input face equal) —
      NOT the rounds (session log --rounds) nor the loop passes (Passes). It is
      the CHAT row's number: a run row prints "turns none (no chats row)".
      --mode takes ONE of the two values the chats table holds, never a list:
        --mode chat     the terminal chats (gopi chat, gopi ui)
        --mode web      the browser conversations (gopi web, gopi cli)
        --source WHO    who held the console: human, agent, worker, merger …
        --all           the COMPLETE index, rows whose log is NOT on this
                        machine included ([missing] marker); the default view
                        reports what it hid on a [hidden] line.
      No --mode/--source lists everything; --json prints "turns" and the
      person-only subset "human_turns" (JSON KEYS in man gopi-session);
      "repl_turns" is a DEPRECATED alias, announced on every row. Every row
      also carries "turns_provenance", which NAMES the half it belongs to: a
      run row OMITS the turn keys rather than zeroing them (contract in man
      gopi-session).
  gopi session show <id> [--json] [--round R] [--turn N] [--mapping]
      One session's summary, whichever source holds it: mode, source, state,
      model, turns, human turns, total token counts, times, end reason, workdir,
      branch/repo, parent, log path — plus, for a run, its RUNTIME facts: pid,
      whether that process is still alive, the ledger row it executed for, its
      run id and the raw request log. The id is a chat session id, a run session
      id, or a task/run id naming a session directory.
      --round R / --turn N / --mapping (pass ONE) answer the JOIN between the
      two counters instead of printing the page: which TURN owns round R, which
      ROUNDS turn N covers, or every turn's interval. Same boundaries as the
      turns row — one algorithm, so they cannot disagree (man gopi-session).
  gopi session log <id> [view flags]     → the JSONL session log reader
  gopi session stats <id> [flags]        → session numbers: human turns first,
                                            the request count beside it (the
                                            turn count is session show's)
  gopi session raw <id> [round] [part]   → the verbatim raw HTTP log
  gopi session resume <id> [--task T]    → continue from its checkpoint.
                                            ONE holder per run directory, decided
                                            by its run-scope kernel lock — not by
                                            pid liveness. Criterion and the gaps
                                            left: devdoc/resume-gate-is-the-run-lock.md
  gopi session replay <id> [--terminal N] → PLAY the recording back as the screen it
                                            was (cell grid + its truecolour), with
                                            transport controls; --turns lists the
                                            TURNS and --turn N prints one in full.
                                            NOT the chat verb's --replay flag.
  gopi session context <id> [--out PATH] → EXPORT one turn's context to a FILE
                                            (newest, or --turn N); one line on screen.
  gopi session notes <query> [--limit N]  → SEARCH the round-level [IMPORTANT] note
                                            index: what each round recorded, across
                                            every session, with the machine and the
                                            full path it was recorded on
  gopi session gc [--apply] [--older-than DAYS] [--largest N]
                  [--trim-raw-log SIZE [--apply]]
                                         → RECLAIM: report which session directories
                                            are collectable and why. A DRY RUN by
                                            default — nothing is deleted unless you
                                            pass --apply, and a directory any live
                                            process stands in, or any task ledger
                                            row names, is never collectable.
                                            --largest N sorts by BYTES instead of
                                            age (read-only); --trim-raw-log SIZE
                                            bounds each *.raw.log (dry run unless
                                            --apply). Contract: man gopi-session.

The checkpoint sub-group is reached in PROSE, and the paragraph is the ruling
every synopsis here obeys. It used to have a line above, reading "gopi session
checkpoint list|export → the raw-log snapshot registry". That line was
WITHDRAWN (2026-10-07) because it is a CROSS-REFERENCE, not a synopsis: the
sub-group dispatches exactly TWO commands (gopi session checkpoint list,
gopi session checkpoint export <log>) and the shorthand was neither. Under
"Usage:" it was a PROMISE the dispatcher refused (exit 1), so the prose pair
gopi session checkpoint list|export carries it, and every Usage line above
names one command (cli_help_shorthand_gate_test.go: a pipe belongs in a
bracketed group or in prose, never in a word slot an operator types).

The first four are RETIRED TOP-LEVEL SPELLINGS, kept as alias WORDS so a
reader who remembers them is handed the door rather than an agent prompt: the
commands moved here (epic verb-restructure, 2026-10-05) and the flat argv is
refused BY NAME (measured 2026-11-06: gopi log -h exits 1, "log is RETIRED —
use gopi session log"). What survives is ONE implementation reached through the
family address, with the flags being that verb's own — NOT two working
spellings. An earlier revision of this page claimed the flat half still ran,
which the binary has never done; the claim was false from the day it was
written because nothing executed the flat argv (task 01a1aca1).

gopi replay is NOT a command: the top-level verb was abolished on 2026-10-03
(devdoc/replay-abolished-verb-single-route.md) and the word is refused by name
so it can never be spent as an agent prompt. The playback lives here.

Every sub-verb answers its OWN page — gopi session <sub> -h prints its flags.

Scope: a session is one run of this binary — a chat, a web conversation, a
one-shot prompt, a task run. Its JSONL log is the authoritative record, and every
verb here reads that log or the index built from it. Why the page is this shape:
devdoc/help-page-layering-small-pages.md.
`

// isSessionSubcommand reports whether argv begins with the `session` group
// verb. It consumes the full argv and handles its own exit, exactly like
// every other subcommand predicate.
func isSessionSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbSession {
		return false
	}
	runSessionGroup(args[1:])
	return true
}

// runSessionGroup dispatches one `gopi session …` invocation.
//
// THE BARE GROUP AND AN UNKNOWN SUB-VERB ARE USAGE ERRORS, never an agent run:
// a bare `gopi session` (or a typo) that fell through to the loop would spend a
// whole LLM session on a mistyped command — the M4.13 lesson the other groups
// (memory/web/task) already follow.
func runSessionGroup(args []string) {
	// -h/--help BEFORE the sub-verb is a usage query for the GROUP page
	// (`gopi session -h`). AFTER a sub-verb it belongs to THAT verb and must
	// reach it: `gopi session log -h` printing the family page would make the
	// family page's own promise ("the flags are that verb's own — run
	// `gopi session log -h`") a lie, which is how this was found (the first
	// build answered every `session <verb> -h` with the family page).
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "gopi session: missing subcommand (want %s)\n%s",
			strings.Join(sessionSubcommandNames, "|"), sessionUsage)
		os.Exit(1)
	}
	if args[0] == HelpFlag || args[0] == HelpFlagLong {
		fmt.Print(sessionUsage)
		os.Exit(0)
	}
	verb, rest := args[0], args[1:]
	if !containsVerb(sessionSubcommandNames, verb) {
		fmt.Fprintf(os.Stderr, "gopi session: unknown subcommand %q (want %s)\n%s",
			verb, strings.Join(sessionSubcommandNames, "|"), sessionUsage)
		os.Exit(1)
	}
	switch verb {
	case sessionSubcmdList:
		os.Exit(runSessionList(rest))
	case sessionSubcmdShow:
		os.Exit(runSessionShow(rest))

	// THE PLAYBACK. It is the session family's OWN reader (see
	// cli_session_replay.go for why it is a sub-verb and not the top-level
	// `gopi replay` the 2026-10-03 consolidation abolished), so it is dispatched
	// directly rather than through the argv-substitution arm the four aliases
	// use — there is no top-level spelling of it to substitute back in.
	case sessionSubcmdReplay:
		os.Exit(runSessionReplay(rest))

	// The round-level NOTE INDEX's reader. Its own implementation, not an
	// alias: there is no top-level spelling of it and no other verb searches
	// the notes.
	case sessionSubcmdNotes:
		os.Exit(runSessionNotes(rest))

	// THE CONTEXT EXPORT — one turn's request rendered to a file. Its own
	// implementation, not an alias: no top-level spelling exists, and the
	// renderer it uses (internal/ctxdump) is shared with the chat carrier's
	// /context, not with any other session verb.
	case sessionSubcmdContext:
		os.Exit(runSessionContext(rest))

	// THE RECLAIM VERB — the family's ONE destructive member and the only way a
	// session directory is ever removed through gopi. Its own implementation, not
	// an alias: there is no top-level spelling of it, and its default (a dry run
	// that deletes nothing) is a property of this dispatch being reached.
	case sessionSubcmdGC:
		os.Exit(runSessionGC(rest))

	// THE FOUR THIN ALIASES. The substitution is argv[0]: the verb word the
	// top-level spelling carries is put back in front of the operands, and the
	// verb's OWN predicate is invoked — so the -h page, the flag parsing and
	// the run function are that verb's, never a copy.
	//
	// WHY THE PREDICATE AND NOT THE RUN FUNCTION. The four verbs do not agree
	// about where -h is handled: stats/raw/resume answer it INSIDE their
	// predicate, log inside its run function. A hand-rolled re-dispatch would
	// have to know that per verb — the second-parser defect, spelled as a
	// lookup table in somebody's head. Calling the predicate inherits each
	// verb's own shape with nothing to keep in sync. (Found by running it:
	// the first build sent `session log -h` to a hand-rolled arm and printed
	// the family page instead of the log page.)
	// THE CHECKPOINT SUB-GROUP is the ONE three-level spelling of this family
	// (`gopi session checkpoint list|export`). It is not an alias — the group
	// has its own second level — so it is handed to the checkpoint group's OWN
	// dispatcher with the group word substituted, exactly the way the four
	// aliases are handed to their own predicates: one implementation, two
	// spellings (Rule 13/14).
	case CliVerbCheckpoint:
		runCheckpointSubcommand(CliVerbCheckpoint, rest)

	case CliVerbLog, CliVerbStats, CliVerbRaw, CliVerbResume:
		if !sessionAliasPredicate(verb)(append([]string{verb}, rest...)) {
			// A predicate that matched no verb is a wiring bug, never a silent
			// no-op: `gopi session <verb>` would otherwise print nothing and
			// exit 0 (Rule 15).
			fmt.Fprintf(os.Stderr, "gopi session: %s dispatched nothing for sub-verb %q\n", CliVerbSession, verb)
			os.Exit(1)
		}
	}
}

// sessionAliasPredicates maps each aliased verb to the top-level predicate that
// owns it (the one main.go's dispatch chain calls). It is a FUNCTION TABLE, not
// a second implementation: every entry is the same function the top-level
// spelling reaches, and the four names are the cliVerbs constants — a rename
// cannot leave a stale key silently unreachable, because the switch above and
// this map are read together.
var sessionAliasPredicates = map[string]func([]string) bool{
	CliVerbLog:    isLogSubcommand,
	CliVerbStats:  isStatsSubcommand,
	CliVerbRaw:    isRawSubcommand,
	CliVerbResume: isResumeSubcommand,
}

// sessionAliasPredicate resolves one aliased verb's predicate, or nil when the
// verb is not an alias at all (the caller's switch guards that, so a nil here
// is a wiring bug the caller reports rather than panics on).
func sessionAliasPredicate(verb string) func([]string) bool {
	return sessionAliasPredicates[verb]
}

// containsVerb reports whether want is one of the group's sub-verbs.
func containsVerb(hay []string, want string) bool {
	for _, v := range hay {
		if v == want {
			return true
		}
	}
	return false
}

// runSessionList implements `gopi session list`: the chat index, newest first,
// optionally narrowed to one mode. It is a THIN face over tools.ChatList — the
// same renderer `gopi chat list` uses (one query, one ordering, no second
// implementation) — plus the flags the session family adds, and the LOCAL-ONLY
// default the operator ruled on (sessionListLocalOnly): the sessions this
// machine really holds, with the rows it dropped disclosed in numbers.
func runSessionList(args []string) int {
	mode := ""
	kind := ""
	source := ""
	limit := 0
	asJSON := false
	// includeMissing is the COMPLETE-index switch. Its zero value is the ruled
	// default (sessionListLocalOnly, false once negated) — the flag is what
	// turns it on.
	includeMissing := !sessionListLocalOnly
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := ""
		if strings.HasPrefix(a, "--") {
			if eq := strings.Index(a, "="); eq >= 0 {
				val, a = a[eq+1:], a[:eq]
			}
		}
		switch a {
		case HelpFlag, HelpFlagLong:
			fmt.Print(sessionUsage)
			return 0
		case sessionModeFlag:
			mode = nextVal(args, &i, val, sessionModeFlag)
		case sessionKindFlag:
			kind = nextVal(args, &i, val, sessionKindFlag)
		case sessionSourceFlag:
			source = nextVal(args, &i, val, sessionSourceFlag)
		case sessionLimitFlag:
			limit = atoiOr(nextVal(args, &i, val, sessionLimitFlag), 0)
		case sessionJSONFlag:
			asJSON = true
		case sessionAllFlag:
			includeMissing = true
		default:
			fmt.Fprintf(os.Stderr, "gopi session %s: unexpected argument %q\n%s", sessionSubcmdList, a, sessionUsage)
			return 1
		}
	}
	// The local-only default is expressed as the ABSENCE of --all (LocalOnly =
	// !includeMissing), so the CLI and the filter cannot disagree about what
	// the default is: one switch, one place it is negated.
	//
	// THE MERGED SOURCE (Rule 13: one listing, not two). `session list` used to
	// read the chats table alone, so every TASK RUN — which writes a session log
	// without registering a chat row — was invisible (measured 2026-10-28: 33
	// table rows beside 1817 run logs on disk). SessionSourceList reads both and
	// orders the union; the chat half keeps the table's own semantics.
	page, err := tools.SessionSourceList(tools.SessionSourceFilter{
		Chat: tools.ChatListFilter{Mode: chatModeForSources(mode), Source: source, LocalOnly: !includeMissing},
		Kind: kind, Mode: mode, Limit: limit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdList, err)
		return 1
	}
	if asJSON {
		// The JSON face is built from the SAME page the table renders, so the
		// two faces cannot disagree about which sessions matched — and the
		// disclosures are the page's own counts, so the machine-readable view
		// cannot silently omit what it dropped (Rule 15). The disclosures ride
		// STDERR: stdout stays exactly one JSON document, so a script can pipe
		// it into a parser without stripping prose first.
		if note := sessionSourceDisclosure(page); note != "" {
			fmt.Fprint(os.Stderr, note)
		}
		out, err := jsonSessionSources(page.Rows)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdList, err)
			return 1
		}
		fmt.Println(out)
		// A half-read listing is NOT a success, on either face: the document
		// above describes the rows this run could see, and the [degraded]
		// disclosure on stderr says which half was not looked at.
		if page.ChatError != nil {
			return sessionDegradedExit
		}
		return 0
	}
	fmt.Print(sessionSourceTable(page))
	if page.ChatError != nil {
		return sessionDegradedExit
	}
	return 0
}

// chatModeForSources narrows the CHAT half's own mode column without refusing a
// mode that belongs to the OTHER kind: `--mode run` must not be handed to the
// chats query (its WHERE clause refuses an unknown mode by name — correctly, for
// its own column), so a run-only mode reaches the chats half as "every mode" and
// the merged filter then drops the chat rows. One decision, stated once.
func chatModeForSources(mode string) string {
	if mode == tools.SessionModeRun {
		return ""
	}
	return mode
}

// sessionSourceDisclosure renders the merged listing's disclosure lines — what a
// narrowed, truncated or partly-unreadable view did NOT show — built from the
// page's own counts. It is the one rendering BOTH faces share (the table prints
// it under the rows; the JSON face prints it on stderr), so a script and a human
// are told the same thing about the same view (Rule 13/15).
func sessionSourceDisclosure(page tools.SessionSourcePage) string {
	var b strings.Builder
	if page.HiddenKind > 0 {
		fmt.Fprintf(&b, sessionHiddenKindFormat+"\n", page.HiddenKind, sessionKindFlag, sessionModeFlag)
	}
	if note := tools.ChatFileHiddenNote(page.HiddenPresence); note != "" {
		b.WriteString(note + "\n")
	}
	if page.Truncated > 0 {
		fmt.Fprintf(&b, sessionHiddenLimitFormat+"\n", page.Truncated, sessionLimitFlag)
	}
	if page.RecordlessDirs > 0 {
		fmt.Fprintf(&b, sessionRecordlessFormat+"\n", page.RecordlessDirs, page.Dirs)
	}
	if page.ChatError != nil {
		// The chats half failed and the run half was still listed: saying so is
		// the difference between "this profile has no chats" and "the table could
		// not be read" (Rule 15).
		fmt.Fprintf(&b, sessionDegradedFormat+"\n", page.ChatError)
	}
	return b.String()
}

// sessionSourceTable renders the merged listing: the count header, the rows (each
// in its own species' shape), and the disclosures above them. The rows come from
// tools.SessionSourceRowLine — the ONE place a row's shape is decided.
func sessionSourceTable(page tools.SessionSourcePage) string {
	var b strings.Builder
	fmt.Fprintf(&b, sessionSourceCountFormat+"\n", len(page.Rows))
	if len(page.Rows) == 0 {
		// WHICH zero-row sentence is printed is a MEASUREMENT question, not a
		// style one: an empty listing whose chat index failed did not find "no
		// sessions", it failed to look at half the sources (Rule 15).
		if page.ChatError != nil {
			b.WriteString(sessionUnreadableResult + "\n")
		} else {
			b.WriteString(sessionSourceEmptyResult + "\n")
		}
	}
	for _, r := range page.Rows {
		b.WriteString(tools.SessionSourceRowLine(r))
	}
	b.WriteString(sessionSourceDisclosure(page))
	return b.String()
}

// runSessionShow implements `gopi session show <id>`: one session's summary.
// The id is the FULL session uuid `session list` printed (a truncated id is
// refused by the ledger/reader layer rather than guessed — the same rule
// `gopi session log` keeps), and the fields come from tools.ChatShow, the same table
// `session list` reads.
func runSessionShow(args []string) int {
	// THE REVERSE-LOOKUP FLAGS ARE PARSED FIRST (cli_session_turnround.go): the
	// scan below refuses every `-`-prefixed word it does not know, so the two
	// address flags must be lifted out before it runs. Their absence leaves argv
	// exactly as it was.
	lookup, args, lkErr := parseSessionTurnRoundLookup(args)
	if lkErr != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n%s", sessionSubcmdShow, lkErr, sessionUsage)
		return 1
	}
	id := ""
	asJSON := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case HelpFlag, HelpFlagLong:
			fmt.Print(sessionUsage)
			return 0
		case sessionJSONFlag:
			asJSON = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "gopi session %s: unknown flag %q\n%s", sessionSubcmdShow, a, sessionUsage)
				return 1
			}
			if id != "" {
				fmt.Fprintf(os.Stderr, "gopi session %s: one session id, got %q and %q\n%s", sessionSubcmdShow, id, a, sessionUsage)
				return 1
			}
			id = a
		}
	}
	if id == "" {
		fmt.Fprintf(os.Stderr, "gopi session %s: missing <id> (the uuid `gopi session %s` prints)\n%s",
			sessionSubcmdShow, sessionSubcmdList, sessionUsage)
		return 1
	}
	// THE RESOLVER, NOT THE CHATS TABLE (Rule 13: one reader per question, and
	// the question is now "this session", not "this chat row"). SessionSourceShow
	// asks the index first and the runs tree second, so a TASK RUN — which has no
	// chats row — resolves and its pid/workdir/log are printed. It is also what
	// attaches a run's recorded facts to a chat row that names one.
	show, err := tools.SessionSourceShow(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdShow, err)
		return 1
	}
	if asJSON {
		out, err := jsonSessionShow(show)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdShow, err)
			return 1
		}
		fmt.Println(out)
		return 0
	}
	// A LOOKUP ANSWERS ALONE: it is a question about this session's axes, not a
	// page to be buried under one, so it prints its own line(s) and nothing else
	// (the answer must be copy-pasteable and greppable, which a full page's
	// `turns` row beside it would make ambiguous).
	if lookup.asked() {
		return runSessionTurnRound(show, lookup)
	}
	fmt.Print(renderSessionShow(show))
	return 0
}

// sessionLimitFlag is `--limit N` on `session list`.
const sessionLimitFlag = "--limit"

// sessionDegradedExit is the exit code `session list` returns when it rendered
// a HALF-READ listing: the chats half failed and the runs half was printed.
//
// It exists because exit 0 beside a `[degraded]` line is the command claiming
// success for a view it knows is incomplete — "there is a problem, but I am
// telling you there is none" (measured 2026-11-06 on an unreadable index). A
// script that pipes `session list` must be able to tell a measured empty view
// from a partially-read one, and the summary line alone cannot carry that
// across a pipe.
//
// 3 is the code this family already uses for "ran, but NOT completely" —
// `session gc` returns it for a DEFERRED sweep (cli_session_gc.go). 1 stays
// what it is everywhere in this CLI: the command could not run at all (bad
// flag, bad operand). So the three outcomes stay distinguishable: 0 = measured,
// 3 = partially measured, 1 = refused.
const sessionDegradedExit = 3

// sessionKindFlag is `--kind VALUE` on `session list`: narrow the merged listing
// to ONE species of session (a chats row, or a run's log). Its accepted values
// are tools.SessionKinds — the vocabulary the filter validates against — so a
// typo is a named refusal rather than an empty list (Rule 1/13/15).
const sessionKindFlag = "--kind"

// The merged listing's disclosure line shapes. Each names the flag a reader must
// type to widen the view, so they are built from the CLI's OWN flag constants
// (Rule 13: the instruction and the parser share one spelling).
const (
	// sessionSourceCountFormat is the header: how many rows the view holds.
	sessionSourceCountFormat = "%d session(s):"
	// sessionHiddenKindFormat discloses the rows the --kind/--mode filter
	// dropped (the chat index's own [hidden] line covers the presence drop).
	sessionHiddenKindFormat = "[filtered] %d session(s) hidden by %s/%s — drop the filter to see them"
	// sessionHiddenLimitFormat discloses the rows --limit cut off the END of
	// the ordered view (not "the oldest": the view is newest-first).
	sessionHiddenLimitFormat = "[limit] %d more session(s) matched; %s N raises the cut"
	// sessionRecordlessFormat closes the arithmetic between the directories a
	// scan entered and the rows it produced — a directory holding no run log is
	// not a hidden row, it is a directory with nothing to list (Rule 15).
	sessionRecordlessFormat = "[scanned] %d of %d session director(ies) held no run log"
	// sessionDegradedFormat reports a half-read: the chat index failed and the
	// runs half was still listed. It names the file and, for a SQLITE_CANTOPEN,
	// the real cause (a PATH or a PERMISSION — never an allocation failure),
	// because the driver's own wording for that code is "unable to open
	// database file: out of memory (14)" and sends a reader hunting memory
	// pressure for a defect that has nothing to do with memory.
	sessionDegradedFormat = "[degraded] the chat index could not be read: %v"
	// sessionUnreadableResult REPLACES sessionSourceEmptyResult when the chats
	// half failed. "No sessions found." is a claim that a listing MEASURED
	// nothing; a listing whose index could not be opened measured nothing about
	// chats and can only say so. Printing both lines was the contradiction this
	// constant removes (measured 2026-11-06: `No sessions found.` beside
	// `[degraded] … out of memory (14)`, both at exit 0).
	sessionUnreadableResult = "No sessions found in the RUN half; the chat index was NOT read — the listing above is incomplete, not empty."
	// sessionFilteredPrefix is the opening word of the filter disclosure. It is a
	// constant because a TEST asserts on it: a narrowed view that stopped saying
	// what it hid would otherwise be a silent behaviour change (Rule 15's check).
	sessionFilteredPrefix = "[filtered]"
	// sessionSourceEmptyResult is the zero-row text — a DIFFERENT claim from the
	// chat index's ("No chats found."): nothing in EITHER source.
	sessionSourceEmptyResult = "No sessions found."
)

// sessionFileWord renders the one-row presence fact as a word — the same
// vocabulary the list marker and the JSON field come from (Rule 13), so the
// show page cannot use a third spelling for a fact two other surfaces already
// name. The page's `index` line (sessionIndexNote) is what explains an absent
// log and hands over the remedy; this word stays one word on purpose, so the
// field reads the same way here as in the list marker.
func sessionFileWord(present bool) string {
	if present {
		return tools.ChatFilePresentWord
	}
	return tools.ChatFileMissingWord
}

// sessionTokensValue renders the show page's token fact: the measured counts,
// or the unknown word when no positive usage was ever reported (Rule 15). The counts go
// through the tools layer's ONE format string, so the summary and the index
// cannot print the same numbers two ways — and that format now SAYS "total",
// because the columns it reads are the CostTracker's cumulative counts folded
// with MAX, i.e. the session's running total (see
// tools.ChatTokenCountsFormat for the measurement that settles it; an earlier
// label read "peak" and was withdrawn as false).
func sessionTokensValue(d tools.ChatSessionDetail) string {
	if !d.TokensReported {
		return tools.ChatTokensUnmeasuredNote
	}
	return fmt.Sprintf(tools.ChatTokenCountsFormat, d.InputTokens, d.OutputTokens, d.CacheTokens)
}

// sessionTotalTokensKey is the show page's key for the token line. It carries the
// WORD "total" as well as the value's own "total tok in=…", because a reader
// scans the key column first, and the column really is the session's running
// total at its last recorded turn (Rule 1/13: one spelling, referenced by the
// render and the tests). Eleven cells, so ONE separating cell remains inside
// sessionShowKeyWidth — a twelve-cell key would leave none and glue the value on.
const sessionTotalTokensKey = "token total"

// sessionTokenTotalNote is the show page's indented explanation under the token
// line — the same slot `human turns` uses for its own "why is this unmeasured"
// note. It prints only when the counts WERE measured: an unmeasured row already
// carries tools.ChatTokensUnmeasuredNote as its value, and two long sentences on
// one fact is how a page stops being read.
func sessionTokenTotalNote(d tools.ChatSessionDetail) []string {
	if !d.TokensReported {
		return nil
	}
	return tools.ChatTokenBasisNote
}

// sessionIndexNote is the ONE decision behind the show page's `index` line and
// the JSON face's index_note: it answers "the session is indexed here, is its
// LOG?" — and it says NOTHING when the log is here, because a disclosure that
// fires on the normal case stops meaning anything (the same reason
// ChatFileHiddenNote returns "" for zero). Both show faces read it, so the
// human page and the machine payload cannot disagree about the row the reader
// is looking at.
func sessionIndexNote(d tools.ChatSessionDetail) string {
	if d.FilePresent {
		return ""
	}
	return tools.ChatLogAbsentIndexNote
}

// sessionState renders one row's lifecycle word from the SAME source the table
// renderer uses: tools.ChatSessionRows/ChatShow fill the same two columns, and
// the word comes from the tools layer's own derivation (ChatStateRunning /
// ChatStateEnded / the recorded end reason) — one vocabulary for the table, the
// summary and the JSON.
func sessionState(d tools.ChatSessionDetail) string {
	switch {
	case d.Running():
		return tools.ChatStateRunning
	case d.EndReason != "":
		return d.EndReason
	default:
		return tools.ChatStateEnded
	}
}

// renderSessionDetail renders the `session show` page. Every field comes from
// the row; a field the row does not carry renders as the shared em dash rather
// than as an empty gap (an empty line reads as "no such column", which is a
// different claim from "the row has no value here").
func renderSessionDetail(d tools.ChatSessionDetail) string {
	var b strings.Builder
	head := d.Title
	if head == "" {
		head = d.FirstMsg
	}
	if head == "" {
		// THE OWNER'S VALUE, not a second spelling (Rule 1/13/14): the chat
		// faces render this placeholder from the tools constant.
		head = tools.ChatNoTextWord
	}
	fmt.Fprintf(&b, "session %s\n", d.SessionID)
	fmt.Fprintf(&b, "  mode        %s\n", d.Mode)
	fmt.Fprintf(&b, "  source      %s\n", tools.ChatSourceWord(d.Source))
	fmt.Fprintf(&b, "  state       %s\n", sessionState(d))
	fmt.Fprintf(&b, "  model       %s\n", orDashString(d.Model))
	// THE TURN COUNT COMES FIRST, UNDER THE BARE WORD, AND IT IS THE LOG'S
	// OPENED COUNT — the axis `--turn N`, a replay band and the `--turns` table
	// number along (epic 2026-11-04).
	//
	// WHY NOT `chats.turn_count`, which this page printed until now: the ledger
	// advances only when a turn COMPLETES (cmd/agent/cli_chat.go's
	// `runErr == nil && !interrupted` gate), while the log records a seam when one
	// OPENS. Measured on session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: `session
	// list` said `turns 29` while `session replay --turns` printed 30 rows and the
	// replay screen's last band read `──── turn 30` — that session's thirtieth
	// turn opened and quit, so the ledger never charged it. The completed count
	// also runs AHEAD on other sessions (e364e357: ledger 209, 34 boundaries), so
	// anchoring the ordinal on it addresses a different turn on either side.
	//
	// THE LEDGER'S NUMBER IS KEPT, UNDISTORTED, UNDER ITS OWN WORD — `turns
	// completed` — so a reader who came for it still finds it and the gap is
	// accounted in numbers (Rule 14: one meaning per word; Rule 15: the
	// difference is stated, never left to be discovered).
	//
	// THE PERSON-ONLY SUBSET IS KEPT AS A STATED DETAIL, never as a rival total:
	// it prints UNDER the count, under its own word (`human turns`). An unmeasured
	// subset says WHY rather than printing a number nobody took
	// (tools.ChatTurnsNote, Rule 15).
	if d.TurnsKnown {
		fmt.Fprintf(&b, "  %-*s%d\n", sessionShowKeyWidth, sessionevent.CounterUnitTurn, d.TurnsOpened)
	} else {
		fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionevent.CounterUnitTurn, sessionShowUnmeasured)
		fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, "", d.TurnsNote)
	}
	fmt.Fprintf(&b, "  %-*s%d\n", sessionShowKeyWidth, sessionevent.CounterUnitTurnCompleted, d.TurnCount)
	if d.TurnsKnown {
		fmt.Fprintf(&b, "  %-*s%d\n", sessionShowKeyWidth, sessionevent.CounterUnitHumanTurn, d.Turns)
	} else {
		// UNMEASURED: the key is stated, the value is a dash, and the REASON is
		// on its own indented line — a reader must be able to tell "nobody
		// looked" from "the person typed nothing" (Rule 15), and a long note
		// pushed into the value column wraps the page into an unreadable block.
		fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionevent.CounterUnitHumanTurn, sessionShowUnmeasured)
		fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, "", d.TurnsNote)
	}
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionTotalTokensKey, sessionTokensValue(d))
	for _, line := range sessionTokenTotalNote(d) {
		// The note rides the same indented slot the unmeasured `human turns`
		// note uses: a sentence pushed into the value column wraps the page into
		// an unreadable block, and this one has to name a command. Its OWN line
		// breaks are kept (tools.ChatTokenBasisNote is a slice), so the page breaks
		// where the author put them rather than wherever the terminal lands.
		fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, "", line)
	}
	fmt.Fprintf(&b, "  started     %s\n", formatSessionTime(d.StartedAt))
	fmt.Fprintf(&b, "  last msg    %s\n", formatSessionTime(d.LastMsgAt))
	fmt.Fprintf(&b, "  ended       %s\n", formatSessionTime(d.EndedAt))
	fmt.Fprintf(&b, "  end reason  %s\n", orDashString(d.EndReason))
	fmt.Fprintf(&b, "  workdir     %s\n", orDashString(d.Workdir))
	fmt.Fprintf(&b, "  git branch  %s\n", orDashString(d.GitBranch))
	fmt.Fprintf(&b, "  git repo    %s\n", orDashString(d.GitRepoRoot))
	fmt.Fprintf(&b, "  parent      %s\n", orDashString(d.ParentSession))
	fmt.Fprintf(&b, "  log path    %s\n", orDashString(d.LogPath))
	// The FILE line answers the question the reader is about to act on ("can I
	// open this session here?"), and it is rendered from the same measured fact
	// the list view's marker and the JSON face's file_present carry — one
	// probe, three renderings (Rule 13).
	fmt.Fprintf(&b, "  log file    %s\n", sessionFileWord(d.FilePresent))
	if note := sessionIndexNote(d); note != "" {
		fmt.Fprintf(&b, "  index       %s\n", note)
	}
	fmt.Fprintf(&b, "  first msg   %s\n", head)
	return b.String()
}

// renderSessionShow renders the show page for EITHER species. A chat row keeps
// the page it always had (renderSessionDetail) with the run's recorded facts
// merged into the columns it already prints, and then gains the runtime block —
// the facts that made this page useless for a task run before: the process it
// was written by, whether that process is still alive, and the raw log beside
// the session log. A run row has no chat columns at all, so it prints the run
// page, built from the same key column so the two species read as one page shape.
//
// THE RUNTIME BLOCK IS ALWAYS PRINTED, for both species, because it answers a
// question no chat column can: "is anything still writing this, and as which
// process?" A row that recorded no process says so in words
// (tools.SessionSourceAliveWord's not-recorded) rather than leaving a blank a
// reader would read as "alive" or as a rendering bug (Rule 15).
func renderSessionShow(show tools.SessionShow) string {
	if show.HasChat {
		d := show.Chat
		// The chats row wins where it has a value (it is the index); the run's
		// recorded facts fill the columns it left empty — the resolver already
		// applied that precedence, so this only copies its answer onto the row
		// the chat renderer prints.
		d.Workdir, d.LogPath = show.Workdir, show.LogPath
		d.GitBranch, d.GitRepoRoot = show.GitBranch, show.GitRepo
		if s := strings.TrimSpace(d.Model); s == "" {
			d.Model = show.Model
		}
		// THE ENDING COMES FROM THE LOG WHEN THE LOG RECORDED ONE (task
		// 01a189b4). `chats.ended_at` is a CLOSE HOOK's stamp and keeps the
		// FIRST ending by design (ChatCloseSession's idempotent WHERE), so a
		// session resumed three times reports day-one's ending under a page
		// that also prints a live-looking pid. The log's LAST `session_end` is
		// the recorded ending of the SESSION; the row's value is kept and
		// disclosed beside it, because "the conversation ended once" and "the
		// row was closed once" are two different facts.
		firstEnd, firstReason := d.EndedAt, d.EndReason
		recorded := sessionEndedFacts(show)
		if recorded.ok {
			d.EndedAt, d.EndReason = recorded.at, recorded.reason
		}
		page := renderSessionDetail(d)
		if recorded.ok && recorded.at != firstEnd {
			page += fmt.Sprintf("  %-*s%s\n", sessionShowKeyWidth, sessionEndedFirstKey,
				formatSessionTime(firstEnd)+sessionReasonSuffix(firstReason))
		}
		return page + renderSessionSegments(show) + renderSessionRuntime(show) + sessionCostBlock(show)
	}
	return renderSessionRun(show) + sessionCostBlock(show)
}

// sessionCostBlock renders THE COST BLOCK on the show page — the RECORD-backed
// surface — for EITHER species.
//
// IT MOVED HERE FROM `gopi session log` (operator's ruling, 2026-11-04), and the
// reason is stated where the log now prints its pointer instead
// (internal/gopilog/cost.go): a cost block at the tail of a LOG reads as
// something the session wrote, while every number in it was CALCULATED by a
// formatter from the parsed `request` rows. A calculated number belongs on the
// view backed by a RECORD, not inside the record.
//
// WHAT IT READS, and this is the second half of the ruling: the session's
// RECORDED totals — `session_end`'s total_iterations / total_tokens /
// elapsed_sec, summed over run segments by the one whole-file scan
// (tools.SessionShow, filled from rundir.SegmentStamp) — and, for a chat
// session, the row's own recorded token columns. It never re-sums the
// per-request rows: a reader asking this page for the cost wants the number the
// run reported when it finished, not a second opinion about the same log.
//
// THE ABSENCES ARE RENDERED, NOT FILLED (Rule 15). A run that wrote no verdict
// (still alive, killed before its end hook) has no recorded totals and the
// block says so in words; a run's species records no per-direction split, and
// the block names that limitation instead of inventing one from the rows.
func sessionCostBlock(show tools.SessionShow) string {
	cost := gopilog.RecordedCost{
		HasEndTotals:    show.HasEndTotals,
		Passes:          show.EndPasses,
		TotalTokens:     show.EndTokens,
		ElapsedSec:      show.EndElapsedSec,
		HasTokenColumns: show.HasChat && show.Chat.TokensReported,
	}
	if cost.HasTokenColumns {
		cost.InputTokens = show.Chat.InputTokens
		cost.OutputTokens = show.Chat.OutputTokens
		cost.CacheTokens = show.Chat.CacheTokens
	}
	return "\n" + gopilog.FormatRecordedCost(cost)
}

// sessionEnded is one resolved ending: the instant, the recorded cause, and
// whether anything recorded it at all.
type sessionEnded struct {
	at     float64
	reason string
	ok     bool
}

// sessionEndedFacts is the ONE decision behind "when did this session end":
// the log's LAST session_end (a recorded instant, tools.SessionShow's
// HasRecordedEnd) when the log carries one, else the chat row's own columns.
//
// THE ABSENT-LOG CASE IS NOT A FALLBACK TO ZERO: recorded.ok is false when no
// verdict time was recorded, and the caller then keeps the row's value rather
// than printing an unmeasured dash over a value the machine holds (Rule 15).
func sessionEndedFacts(show tools.SessionShow) sessionEnded {
	if show.HasRecordedEnd {
		return sessionEnded{
			at:     float64(show.RecordedEnd.Unix()),
			reason: show.RecordedEndReason,
			ok:     true,
		}
	}
	return sessionEnded{}
}

// sessionReasonSuffix renders a reason beside a time, or nothing when the
// record carried none (an older log: absence stays an absence, Rule 15).
func sessionReasonSuffix(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " (" + reason + ")"
}

// renderSessionSegments is the SEGMENT DISCLOSURE: how many run segments the
// log holds, and therefore whether the pid/ended above describe the last one.
//
// It prints NOTHING for a session that was never resumed AND whose log was
// scanned — a disclosure that fires on the normal case stops meaning anything.
// A log that was NOT scanned says so instead: "one segment" and "nobody looked"
// must not render alike (Rule 15).
func renderSessionSegments(show tools.SessionShow) string {
	if !show.SegmentsScanned {
		return fmt.Sprintf("  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeSegmentsKey,
			sessionSegmentsUnscanned)
	}
	if show.Segments <= sessionSegmentsUnresumed {
		return ""
	}
	// A RESUMED LOG. The count is a fact; the WITNESS is the second, separate
	// fact, and the page states whichever one it has (Rule 15: an unwitnessed
	// resume is not a resume that did not happen).
	return fmt.Sprintf("  %-*s%d (%s%s)\n", sessionShowKeyWidth, sessionRuntimeSegmentsKey,
		show.Segments, sessionSegmentsResumedNote, sessionWitnessSuffix(show))
}

// sessionWitnessSuffix renders the resume WITNESS beside the segment count:
// how many headers named the session they continued, or — when the scan found
// headers carrying no witness at all — the word that says so. A log whose
// witness state was not counted (no scan) gets nothing, because the count it
// would qualify is already reported as unscanned above.
//
// THE ABSENT CASE IS RENDERED, NOT OMITTED: on this tree every task-run header
// is unwitnessed (the producer holds no resume identity), so the segment count
// alone would read as "3 segments, 0 resumed explanation" — and a reader would
// be entitled to conclude the log is corrupt. Naming the absence says which
// fact is missing.
func sessionWitnessSuffix(show tools.SessionShow) string {
	switch show.SegmentWitness {
	case rundir.SegmentWitnessCarrying:
		return fmt.Sprintf(sessionSegmentsWitnessedFmt, show.WitnessedSegments)
	case rundir.SegmentWitnessAbsent:
		return sessionSegmentsWitnessAbsent
	}
	return ""
}

// renderSessionRuntime renders the block both species share: the liveness stamp
// and its verdict, the ledger row the run executed for, the run's own id and the
// raw request log beside the session log.
func renderSessionRuntime(show tools.SessionShow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeKindKey, show.Kind)
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimePIDKey, sessionPIDValue(show.PID))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeAliveKey, tools.SessionRunAliveWord(show.PID, show.Alive))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeTaskKey, orDashString(show.TaskID))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeRunKey, orDashString(show.RunID))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeRawKey, orDashString(show.RawLogPath))
	return b.String()
}

// renderSessionRun renders the page for a RUN: the columns a run log really has,
// under the same labels the chat page uses where the fact is the same (state,
// model, workdir, git branch/repo, log path) so a reader does not have to learn
// a second vocabulary to read the second species.
func renderSessionRun(show tools.SessionShow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session %s\n", show.SessionID)
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeModeKey, show.Mode)
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeStateKey, orDashString(show.State))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeModelKey, orDashString(show.Model))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeStartedKey, sessionRunTime(show.Started, show.HasStart))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeLastWriteKey, sessionRunTime(show.LastWrite, !show.LastWrite.IsZero()))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeWorkdirKey, orDashString(show.Workdir))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeBranchKey, orDashString(show.GitBranch))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeRepoKey, orDashString(show.GitRepo))
	fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeLogKey, orDashString(show.LogPath))
	b.WriteString(renderSessionSegments(show))
	b.WriteString(renderSessionRuntime(show))
	if !show.Recorded {
		// The run's own session_start was never read — so every "—" above is a
		// provenance gap, not an absence the machine established. Stating it is
		// the difference between "this run recorded no workdir" and "the record
		// could not be read" (Rule 15).
		fmt.Fprintf(&b, "  %-*s%s\n", sessionShowKeyWidth, sessionRuntimeRecordKey, sessionRunUnrecordedNote)
	}
	return b.String()
}

// sessionPIDValue renders the liveness stamp: the pid, or the shared em dash for
// a row that stamped none (the alive column then says not-recorded, so the dash
// is not the only fact a reader gets).
func sessionPIDValue(pid int) string {
	if pid <= 0 {
		return sessionShowUnmeasured
	}
	return strconv.Itoa(pid)
}

// sessionRunTime renders a time a run recorded, or the unmeasured mark.
func sessionRunTime(t time.Time, ok bool) string {
	if !ok || t.IsZero() {
		return sessionShowUnmeasured
	}
	return t.Format(sessionRunTimeLayout)
}

// sessionRunTimeLayout is the timestamp layout the run page prints (the same
// reading order as formatSessionTime, with seconds because a run's clock is an
// event time rather than a session boundary).
const sessionRunTimeLayout = "2006-01-02 15:04:05"

// sessionRunUnrecordedNote explains a run page whose provenance columns are all
// empty: the run's session_start was not read.
const sessionRunUnrecordedNote = "no session_start was read — the fields above are unrecorded, not empty"

// The show page's runtime labels. They are named constants (Rule 1) because the
// render, the JSON keys beside it and the tests that read the page must not each
// spell them.
const (
	sessionRuntimeKindKey      = "kind"
	sessionRuntimeModeKey      = "mode"
	sessionRuntimeStateKey     = "state"
	sessionRuntimeModelKey     = "model"
	sessionRuntimeStartedKey   = "started"
	sessionRuntimeLastWriteKey = "last write"
	sessionRuntimeWorkdirKey   = "workdir"
	sessionRuntimeBranchKey    = "git branch"
	sessionRuntimeRepoKey      = "git repo"
	sessionRuntimeLogKey       = "log path"
	sessionRuntimePIDKey       = "pid"
	sessionRuntimeAliveKey     = "alive"
	sessionRuntimeTaskKey      = "task id"
	sessionRuntimeRunKey       = "run id"
	sessionRuntimeRawKey       = "raw log"
	sessionRuntimeRecordKey    = "record"
	sessionRuntimeSegmentsKey  = "segments"
	// sessionEndedFirstKey is the row's OWN close-hook stamp, disclosed beside
	// the session's recorded ending. The label is two words inside the page's
	// key column (sessionShowKeyWidth) — a longer spelling overruns it and
	// breaks the column alignment the rest of the page keeps.
	sessionEndedFirstKey = "first ended"
)

// The segment disclosure's words. They are constants because the JSON face and
// the human page must say the same thing about the same count (Rule 1/13).
const (
	// sessionSegmentsUnresumed is the count a session that was never resumed
	// holds: one `session_start` header.
	sessionSegmentsUnresumed = 1
	// sessionSegmentsResumedNote says what the count MEANS — the pid and ended
	// above belong to the last segment, not to the session's beginning.
	sessionSegmentsResumedNote = "run segments: the pid and ended above are the LAST one's"
	// sessionSegmentsUnscanned says the log was not read end to end, so its
	// segment count is unknown rather than one (Rule 15).
	sessionSegmentsUnscanned = "— (the log was not scanned end to end)"
	// sessionSegmentsWitnessedFmt is the witness clause: how many of those
	// headers named the session they continued. It is a FORMAT (the count is
	// the fact) and lives here so both faces that need it read one spelling.
	sessionSegmentsWitnessedFmt = "; %d carrying resumedFrom"
	// sessionSegmentsWitnessAbsent is the second state: headers were read and
	// NONE names a resume. It must not read as "not resumed" — the writers on
	// this tree are not all able to witness (Rule 15a).
	sessionSegmentsWitnessAbsent = "; no resume witness"
)

// formatSessionTime renders a unix-seconds float, or the unset marker for the
// rows where the column is genuinely absent (a running conversation has no
// ended_at). 0 is that marker in the table, so it is that marker here.
func formatSessionTime(ts float64) string {
	if ts <= 0 {
		return "—"
	}
	return time.Unix(int64(ts), 0).Format("2006-01-02 15:04:05")
}

// orDashString is the shared "absent value" rendering (the chat index's own
// convention), so a summary field and an index field never disagree about what
// an empty column looks like.
func orDashString(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// sessionJSONRow is the wire shape of one `session list --json` row. It is a
// struct (never a hand-built map) so the field set is checked by the compiler
// against the same detail the table renders.
type sessionJSONRow struct {
	SessionID string `json:"session_id"`
	Mode      string `json:"mode"`
	// Source is the recorded provenance (tools.ChatSourceUnrecorded when the
	// row carries none). It rides the JSON face too: a script filtering
	// verification traffic out of the operator's index needs the same fact the
	// human reading the table sees, and a field that exists in one rendering
	// and not the other is how the two faces start disagreeing (Rule 13).
	Source string `json:"source"`
	State  string `json:"state"`
	Model  string `json:"model"`
	// Turns is the session's TURN COUNT along the axis `turn N` numbers — the
	// count the session LOG opened (epic 2026-11-04).
	//
	// IT CARRIED `chats.turn_count` UNTIL THEN, AND THE MEASUREMENT IS WHY IT
	// MOVED: session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f's index row said
	// `turns 29` while `gopi session replay --turns` printed 30 rows and the replay
	// screen's last band read `──── turn 30`. The ledger advances only when a turn
	// COMPLETES (cmd/agent/cli_chat.go's `runErr == nil && !interrupted` gate) and
	// that session's thirtieth turn opened then quit; the completed count can also
	// run AHEAD (e364e357-a9ea-4210: ledger 209 against 34 boundaries). Ordinals are
	// POSITIONS, so the bare word carries the position count and the ledger's number
	// rides TurnsCompleted below — nothing was deleted, only named.
	//
	// IT WAS SPELLED `repl_turns` FOR ONE DAY (2026-10-26); that spelling is
	// published only as the ANNOUNCED deprecated alias in DeprecatedKeys below.
	//
	// A STRUCT TAG CANNOT HOLD A CONSTANT, so the tag literal below is pinned to
	// that constant by cmd/agent/session_json_wire_test.go (the shape
	// internal/tools/tasknotify.go uses for its own wire key).
	//
	// IT IS A POINTER, AND `omitempty` IS THE FIX FOR THE EPIC'S DEFECT
	// (2026-11-04). `gopi session list` answers ONE session id with TWO rows:
	// kind=chat (this row — it HAS a count) and kind=run (a run log, which has
	// none). This key used to be a plain `int`, so the run row published
	// `"turns": 0` beside the chat row's `"turns": 200` and a consumer summing the
	// column could not tell a MEASURED ZERO from "no chats row stands behind this
	// number" — the ambiguity the PA ruling names, not the row count. A nil
	// pointer OMITS the key on the run row, so the number is published only where
	// a producer stands behind it, and TurnsProvenance below says which half a
	// reader is holding (Rule 15: an absent fact is stated, never shipped as 0).
	Turns *int `json:"turns,omitempty"`
	// TurnsProvenance names WHICH PRODUCER fed the turn block above, on EVERY row
	// of both species: sessionevent.TurnsProvenanceChat for a chats row (the
	// session log's turn-opening count) and sessionevent.TurnsProvenanceRun for a
	// run row (no chats row exists, so no count is published at all). Without it
	// two rows of one id are distinguishable only by `kind` plus the suspicion
	// that a 0 means nothing — which is exactly the ambiguity this epic removed.
	TurnsProvenance string `json:"turns_provenance"`
	// TurnsCompleted is the LEDGER's count of turns that FINISHED
	// (`chats.turn_count`) — the neighbour Turns is NOT, published so a consumer
	// that needs the ledger quantity keeps getting it instead of losing it when
	// the bare word moved to the opened axis.
	//
	// THE BARE WORD MOVED, AND THIS KEY IS WHY IT COULD (epic 2026-11-04, measured
	// on session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: `session list` said `turns
	// 29` while `session replay --turns` printed 30 rows and the replay screen's
	// last band read `──── turn 30`). `--turn N` and a replay band are POSITIONS,
	// so the bare word has to carry a POSITION count; the ledger's completed count
	// can sit behind (that session) or 175 ahead (e364e357: ledger 209, 34
	// boundaries). Nothing was deleted: the old number keeps its own key, and a
	// script that relied on it moves one key to the right, with the change stated
	// here rather than discovered.
	//
	// IT IS A POINTER FOR THE SAME REASON AS Turns ABOVE: a run row has no
	// `chats.turn_count` to publish, and shipping 0 there is the ambiguity the
	// epic removed, not a measurement (Rule 15).
	TurnsCompleted *int `json:"turns_completed,omitempty"`
	// HumanTurns is the PERSON-ONLY SUBSET of the count above (tools.
	// ChatSessionDetail.Turns): one per row the operator typed, the same number
	// `session log --turns` tabulates. It is a DETAIL, so it rides its own key and
	// never the bare one. It is OMITTED when nobody measured it, with human_turns_note
	// saying why (Rule 15a: an optional outgoing field).
	HumanTurns int `json:"human_turns,omitempty"`
	// HumanTurnsKnown says whether HumanTurns is a MEASUREMENT; the pair
	// exists so a zero it reports is a real "the person typed nothing" and not a
	// default (Rule 15).
	HumanTurnsKnown bool `json:"human_turns_known"`
	// TurnsNote names the reason HumanTurns is absent.
	TurnsNote string `json:"human_turns_note,omitempty"`
	// TOTAL, NOT PEAK — and the wire key keeps its spelling while the payload
	// TELLS the consumer which quantity it holds (tokens_basis below). The key
	// cannot be renamed without breaking every script already reading it, and a
	// silent re-point of a key's MEANING is the breakage Rule 15 forbids; so the
	// fact rides a NEW field instead of being hidden in a doc nobody parses.
	//
	// IT SAID "PEAK, NOT TOTAL" UNTIL 01a1954e (2026-11-01), ON A FALSE PREMISE.
	// Measured: the recorder is handed the CostTracker's CUMULATIVE counts, and
	// MAX over a monotonically increasing series returns its last element, so
	// these three numbers are the session's running total — see
	// tools.ChatTokenCountsFormat's comment for the two sessions that measure it.
	// The old comment's 8,086,768-vs-393,314,088 comparison was real but read
	// backwards: 8,086,768 IS the per-request sum, and the larger number came
	// from a reader that adds up the session log's own CUMULATIVE rows.
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheTokens  int `json:"cache_tokens"`
	// TokensBasis names the QUANTITY the three counts above carry: the wire
	// value of tools.ChatTokenBasisWord ("session-total"). A consumer that wants
	// per-request figures must read the session log (`gopi session log`), whose
	// rows carry the delta on the task path and the running total on the chat
	// paths — this field is how a consumer learns which quantity the LEDGER holds
	// without reading this repository's source. It is published even when the
	// counts are unmeasured, because "which quantity would these be" is true of
	// the row either way, and tokens_reported already says whether they were
	// taken.
	TokensBasis string `json:"tokens_basis"`
	// TokensReported answers whether the three counts above are a measurement
	// or the schema default left by a provider that never reported usage. It
	// is a STRUCTURED field for the same reason file_present is: a script
	// summing input_tokens must be able to skip the rows where 0 means
	// "nobody measured" rather than "nothing was spent" (Rule 15), and it
	// cannot recover that from the numbers themselves.
	TokensReported bool    `json:"tokens_reported"`
	StartedAt      float64 `json:"started_at"`
	EndedAt        float64 `json:"ended_at"`
	// FilePresent says whether THIS machine holds the row's session log — the
	// same fact the table view's "[missing] " marker renders (tools.
	// ChatSessionDetail.FilePresent, filled by the one presence probe both
	// faces run). It is a STRUCTURED field rather than only a marker because a
	// script consuming the JSON needs to decide for itself whether to try
	// opening a session, and parsing a bracketed marker out of a human table is
	// exactly the work this face exists to save (Rule 15: the machine-readable
	// rendering must not be the one that loses a fact the human render shows).
	FilePresent bool `json:"file_present"`

	// THE DEPRECATED ALIAS, and why it is a struct EMBED rather than a third
	// plain field. An anonymous embedded struct does NOT flatten — encoding/json
	// publishes its field under the EMBEDDED TYPE's name as a nested object —
	// which is exactly what a deprecation needs: the old key must remain
	// READABLE (so an existing script keeps working) while ceasing to be MISTAKEN
	// for the live quantity (a nested object cannot be parsed as a number by a
	// script that did not read this comment, and a script that did read it moves
	// to `turns`).
	//
	// THE TOP-LEVEL DUPLICATE IS GONE ON PURPOSE. Publishing `"repl_turns": 200`
	// beside `"turns": 200` would put two top-level keys on one number and make
	// the wire shape depend on which spelling a reader happened to pick; the
	// alias is announced, nested, and dated instead. It is the QUALIFIED
	// spelling that is dying here — the live key is the bare word, because the
	// bare word is now the count (the operator's ruling, 2026-10-27).
	//
	// IT IS A POINTER for the same reason `turns` is: the block ALIASES the live
	// count, and a run row has no count to alias — publishing
	// `deprecated_keys.repl_turns: 0` on a run row would be a number no chats
	// column stands behind, announced as if it were a measurement.
	DeprecatedKeys *deprecatedSessionKeys `json:"deprecated_keys,omitempty"`
}

// deprecatedSessionKeys is the migration surface of the wire row: the old key,
// the value it held, the key to move to, and the date the alias disappears. It
// is a STRUCT rather than a bare number so the value can never be read as the
// current shape (see the field's comment on sessionJSONRow) and so the notice
// travels WITH the number instead of in a changelog nobody reads (Rule 15).
//
// The key names come from internal/sessionevent, the leaf that owns
// JSONKeyTurns and the deprecated alias — the same package that names the unit
// the human table prints (Rule 14: one definition, and the leaf is where both
// faces reach it).
type deprecatedSessionKeys struct {
	// ReplTurns is the value the qualified key carried — the SAME turn count the
	// live `turns` field publishes, never re-pointed at a different quantity
	// (re-pointing a key's MEANING is the silent breakage Rule 15 forbids).
	ReplTurns int `json:"repl_turns"`
	// ReplacedBy names the key that carries the same value in the live shape.
	ReplacedBy string `json:"replaced_by"`
	// RemoveAfter is the date this block is deleted; after it, a consumer
	// still reading `turns` gets nothing and the report says so.
	RemoveAfter string `json:"remove_after"`
	// Note is the one sentence a script's author needs, on the payload.
	Note string `json:"note"`
}

// sessionJSONRowOf converts ONE chats row into its wire shape. It is the ONE
// conversion (Rule 13): the chat list's JSON face and the merged listing's chat
// rows both go through it, so a key added to one is added to both, and the
// deprecated-alias block is built in exactly one place.
//
// THE TURN KEYS ARE ALWAYS PUBLISHED HERE. This function's subject is a row that
// EXISTS in the chats table, so every turn quantity it carries has a producer
// standing behind it — including a real 0 ("this session ran no turn"). The row
// that has NO such producer (kind=run, a run log) never reaches this function;
// it is built by sessionJSONChatRow, which strips the keys instead of calling
// this with zeroes (see its comment).
func sessionJSONRowOf(d tools.ChatSessionDetail) sessionJSONRow {
	turns, completed := d.TurnsOpened, d.TurnCount
	replTurns := d.TurnsOpened
	return sessionJSONRow{
		SessionID: d.SessionID, Mode: d.Mode, Source: tools.ChatSourceWord(d.Source), State: sessionState(d), Model: d.Model,
		Turns: &turns, TurnsCompleted: &completed,
		TurnsProvenance: sessionevent.TurnsProvenanceChat,
		HumanTurns:      d.Turns, HumanTurnsKnown: d.TurnsKnown,
		TurnsNote: d.TurnsNote,
		DeprecatedKeys: &deprecatedSessionKeys{
			ReplTurns:   replTurns,
			ReplacedBy:  sessionevent.JSONKeyTurns,
			RemoveAfter: sessionevent.DeprecatedJSONKeyReplTurnsRemoveAfter,
			Note:        sessionevent.DeprecatedReplTurnsKeyNote,
		},
		InputTokens: d.InputTokens, OutputTokens: d.OutputTokens,
		CacheTokens: d.CacheTokens, TokensBasis: tools.ChatTokenBasisWord,
		TokensReported: d.TokensReported, StartedAt: d.StartedAt, EndedAt: d.EndedAt,
		FilePresent: d.FilePresent,
	}
}

// jsonSessionRows encodes the list face. Encoding a struct never fails for
// these field types, but the error is returned rather than discarded (Rule 15:
// no silent failure).
func jsonSessionRows(rows []tools.ChatSessionDetail) (string, error) {
	out := make([]sessionJSONRow, 0, len(rows))
	for _, d := range rows {
		out = append(out, sessionJSONRowOf(d))
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("session list: encode json: %w", err)
	}
	return string(b), nil
}

// sessionSourceJSONRow is the wire shape of ONE merged-listing row: the chat
// index's row (embedded, so a chat row keeps every key it already published)
// plus the facts that only a RUN carries — and the two FACTS EVERY row has
// (kind, pid), which are what let a script tell the species apart and reach a
// running session's process without parsing prose (the acceptance criterion for
// `session list --json`).
type sessionSourceJSONRow struct {
	// kind is the species: tools.SessionKindChat / tools.SessionKindRun.
	Kind string `json:"kind"`
	// pid is the liveness STAMP (0 = none recorded); alive says what the probe
	// measured about it, in procstat's vocabulary (tools.SessionSource's own
	// words) — a stamp and a verdict are two facts, never folded into one
	// (Rule 15). A chat row stamps no process at all, and says so with
	// "not-recorded" rather than with a blank.
	PID   int    `json:"pid"`
	Alive string `json:"alive"`
	// Workdir / LogPath / RawLogPath / GitBranch / GitRepo are the run's
	// recorded paths and the branch git reports for them; all "" when the row
	// has none (a chat row's are its own columns).
	Workdir    string `json:"workdir,omitempty"`
	LogPath    string `json:"log_path,omitempty"`
	RawLogPath string `json:"raw_log_path,omitempty"`
	GitBranch  string `json:"git_branch,omitempty"`
	GitRepo    string `json:"git_repo_root,omitempty"`
	// TaskID is the ledger row the run executed for; RunID is the run's own id.
	TaskID string `json:"task_id,omitempty"`
	RunID  string `json:"run_id,omitempty"`

	// The chat row's own payload, published inline for both species so the wire
	// shape is ONE document per row (a nested object would make a script test the
	// species before it could read a field).
	sessionJSONRow
}

// jsonSessionSources encodes the merged listing. The zero-filled chat payload of
// a RUN row is not a lie on the wire: turns/tokens are 0 with
// "tokens_reported": false and "human_turns_known": false, which is exactly the
// "nobody measured this here" claim those fields already carry (Rule 15) — a run
// log reports no chat accounting, and the row says so.
func jsonSessionSources(rows []tools.SessionSource) (string, error) {
	out := make([]sessionSourceJSONRow, 0, len(rows))
	for _, r := range rows {
		chat := sessionJSONChatRow(r)
		out = append(out, sessionSourceJSONRow{
			Kind: r.Kind,
			// A source carries its verdict as procstat.Verdict; the WORD comes
			// from the tools layer's own rendering so the table and the JSON
			// cannot spell one row's liveness two ways (Rule 13).
			PID:            r.PID,
			Alive:          tools.SessionSourceAliveWord(r),
			Workdir:        r.Workdir,
			LogPath:        r.LogPath,
			RawLogPath:     r.RawLogPath,
			GitBranch:      r.GitBranch,
			GitRepo:        r.GitRepo,
			TaskID:         r.TaskID,
			RunID:          r.RunID,
			sessionJSONRow: chat,
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("session list: encode json: %w", err)
	}
	return string(b), nil
}

// sessionJSONChatRow builds the chat-index half of a merged row: the real chats
// columns when a chat row answered, and a row that STATES the absence when a run
// row is being rendered.
func sessionJSONChatRow(r tools.SessionSource) sessionJSONRow {
	if d, ok := r.ChatDetail(); ok {
		return sessionJSONRowOf(d)
	}
	row := sessionJSONRowOf(tools.ChatSessionDetail{SessionID: r.SessionID, Mode: r.Mode, Source: r.Source, Model: r.Model})
	// A run's lifecycle word is the RUN derivation's (running / ended / stopped /
	// unstamped / live-unknown), not the chats one — a run has no "ended_at"
	// column, so the chat derivation would call every run "running". The word is
	// copied from the source, which got it from the ONE run derivation.
	row.State = r.State
	// The run's clock is its own start, and its record WAS on disk (it is what
	// this row was built from) — so `started_at` and `file_present` answer for a
	// run row instead of shipping the chat struct's zero values, which would read
	// as "started at the epoch" and "the log is missing" (Rule 15).
	row.StartedAt = r.Started
	row.FilePresent = true
	// THE TURN BLOCK COMES OFF THIS ROW THROUGH THE ONE FUNCTION, AND THAT IS THE
	// EPIC'S FIX (2026-11-04).
	//
	// chats.turn_count is the CHAT accounting; a run log has no chats column
	// behind it (its own progress counter is the loop-pass quantity the family
	// already refuses to print under the bare turn word). The row used to ship
	// `turns: 0` / `turns_completed: 0` / `deprecated_keys.repl_turns: 0` for that
	// absence, so ONE session id came back as two rows carrying the SAME bare key
	// with two different meanings — a consumer summing the column read a
	// "nothing was measured" 0 as a turn count, and a reader could not tell which
	// row the number belonged to. The PA ruling keeps BOTH rows (they are
	// different facts, not a duplicate); what it forbids is the collapse, and what
	// it asks for is that the provenance be visible (Rule 15: an absent fact is
	// stated, never shipped as a zero that looks measured).
	sessionJSONStripTurns(&row)
	return row
}

// sessionJSONStripTurns removes the turn block from a row that has NO chats row
// behind it, and says so in the provenance field instead. It is ONE function
// because three faces render that absence (the merged listing's run row, the
// `session show --json` run page, and any future face) and three hand-written
// pairs of `nil` assignments would be three places to forget one (Rule 13).
func sessionJSONStripTurns(row *sessionJSONRow) {
	row.Turns = nil
	row.TurnsCompleted = nil
	row.DeprecatedKeys = nil
	row.TurnsProvenance = sessionevent.TurnsProvenanceRun
}

// jsonSessionShow encodes the show face for EITHER species: the chat row's
// payload (when one answered) plus the runtime facts — the same ones the page
// prints, under the same words, so a script and a reader get one answer
// (Rule 13/15). It is a documented type rather than an anonymous struct so the
// merged shape is checkable by the compiler against the page.
type sessionShowJSON struct {
	sessionJSONRow
	// kind / pid / alive / task_id / run_id / raw_log_path are the runtime
	// facts; a run row has no chat columns, so the embedded chat payload is its
	// zero value with the "unmeasured" flags set (never a fabricated number).
	Kind string `json:"kind"`
	PID  int    `json:"pid"`
	// Alive is the probe's word (tools.SessionRunAliveWord), or "not-recorded"
	// when the row stamped no process.
	Alive    string `json:"alive"`
	Recorded bool   `json:"run_recorded"`
	TaskID   string `json:"task_id,omitempty"`
	RunID    string `json:"run_id,omitempty"`

	Title       string `json:"title,omitempty"`
	FirstMsg    string `json:"first_msg,omitempty"`
	Workdir     string `json:"workdir,omitempty"`
	LogPath     string `json:"log_path,omitempty"`
	RawLogPath  string `json:"raw_log_path,omitempty"`
	GitBranch   string `json:"git_branch,omitempty"`
	GitRepoRoot string `json:"git_repo_root,omitempty"`
	Parent      string `json:"parent_session_id,omitempty"`
	EndReason   string `json:"end_reason,omitempty"`
	// ChatEndedAt is the CHAT ROW's own ended_at — the close hook's stamp,
	// which keeps the FIRST ending by design. It is published beside the
	// session's recorded ending above rather than instead of it: a resumed
	// session has two true statements about "when it ended" and a consumer must
	// be able to tell which one it is reading (PROJECT.md Rule 15).
	ChatEndedAt float64 `json:"chat_ended_at,omitempty"`
	// Segments is how many run segments (session_start headers) the log holds,
	// and SegmentsScanned says whether the log was read end to end. Together
	// they state the SPAN of pid/ended_at: 3 means "the last one's".
	Segments        int  `json:"segments"`
	SegmentsScanned bool `json:"segments_scanned"`
	// SegmentsResumed is how many of those headers NAMED the session they
	// continued, and SegmentWitness is the three-state word beside it:
	// `carrying` / `absent` / `not-counted`. The pair is published because a
	// consumer must be able to tell "3 segments, 2 witnessed resumes" from
	// "3 segments, no witness at all" — the second is a fact about the writers
	// and has been collapsed into "never resumed" by every reader that only
	// had the count (PROJECT.md Rule 15a).
	SegmentsResumed int    `json:"segments_resumed"`
	SegmentWitness  string `json:"segment_witness"`

	// RecordedCost is the COST BLOCK's machine face — the same numbers the
	// human page prints, from the same recording, so a script and a person
	// reading the same session cannot be told different costs (Rule 13: one
	// decision, two renderings).
	//
	// The three totals come from `session_end`, SUMMED over run segments; they
	// are populated only when a verdict was read, and `recorded_cost_passes` et
	// al. are then OMITTED rather than published as 0 (Rule 15a is the reason
	// these are pointers to int/float: an omitted `0` and a measured `0` are
	// different facts and a plain int cannot tell them apart).
	CostPasses     *int     `json:"recorded_cost_passes,omitempty"`
	CostTokens     *int     `json:"recorded_cost_tokens,omitempty"`
	CostElapsedSec *float64 `json:"recorded_cost_elapsed_sec,omitempty"`
	// RecordCostRecorded states whether a `session_end` was read at all — the
	// boolean a consumer keys on instead of inferring absence from three
	// omitted fields.
	RecordCostRecorded bool `json:"recorded_cost_recorded"`
}

// jsonSessionShow builds and encodes that payload.
func jsonSessionShow(show tools.SessionShow) (string, error) {
	// THE CHAT HALF COMES FROM THE ROW ITSELF when a chat row answered — turns,
	// tokens and the start clock live on that row and nowhere else, so building
	// the head from the resolver's summary fields would publish zeroes for facts
	// the machine holds (Rule 15: a fabricated 0 is a wrong answer, not a gap).
	head := sessionJSONRowOf(tools.ChatSessionDetail{
		SessionID: show.SessionID, Mode: show.Mode, Source: show.Source,
		Model: show.Model, TurnCount: 0,
	})
	if show.HasChat {
		head = sessionJSONRowOf(show.Chat)
	} else {
		// A RUN row: no chats row exists, so the head is the run's own derived
		// state and start; `file_present` answers the question it answers for a
		// chat row — did the log this row describes exist on this machine — with
		// the run record's own reading.
		head.State = show.State
		head.FilePresent = show.Recorded
		if show.HasStart {
			head.StartedAt = float64(show.Started.Unix())
		}
		// AND THE TURN BLOCK COMES OFF, by the ONE function the merged listing's
		// run rows use, so `session show --json <run-id>` and the run row of
		// `session list --json` cannot state the absence two ways (Rule 13).
		sessionJSONStripTurns(&head)
	}
	payload := sessionShowJSON{
		sessionJSONRow: head,
		Kind:           show.Kind,
		PID:            show.PID,
		Alive:          tools.SessionRunAliveWord(show.PID, show.Alive),
		Recorded:       show.Recorded,
		TaskID:         show.TaskID,
		RunID:          show.RunID,
		Workdir:        show.Workdir,
		LogPath:        show.LogPath,
		RawLogPath:     show.RawLogPath,
		GitBranch:      show.GitBranch,
		GitRepoRoot:    show.GitRepo,
	}
	if show.HasChat {
		// A chat row fills the fields that only a chat row has. Copying them
		// here (not in the head above) keeps ONE source for "which fields are
		// the chat table's" — sessionJSONRowOf.
		payload.Title = show.Chat.Title
		payload.FirstMsg = show.Chat.FirstMsg
		payload.Parent = show.Chat.ParentSession
		payload.EndReason = show.Chat.EndReason
		// THE ENDING IS OVERRIDDEN BY THE LOG'S LAST VERDICT, exactly as the
		// human page overrides it (one decision, two renderings — Rule 13): a
		// resumed session's `chats.ended_at` keeps the FIRST ending by design,
		// so publishing it unchanged here would hand a script the stale number
		// the page no longer shows. The row's own value is disclosed under its
		// own key, so a consumer can tell "the session ended" from "the row was
		// closed" (Rule 15: two facts, never folded).
		payload.ChatEndedAt = show.Chat.EndedAt
		if ended := sessionEndedFacts(show); ended.ok {
			payload.EndedAt = ended.at
			payload.EndReason = ended.reason
		}
	}
	// THE SEGMENT DISCLOSURE rides both faces (Rule 13): a machine consumer
	// summing sessions needs the same span a human reading the page gets.
	payload.Segments = show.Segments
	payload.SegmentsScanned = show.SegmentsScanned
	payload.SegmentsResumed = show.WitnessedSegments
	payload.SegmentWitness = show.SegmentWitness.String()
	// THE RECORDED COST, on the machine face (Rule 13). A consumer that reads
	// the page and a consumer that reads the JSON must be told the same cost;
	// the numbers are the recorded ones and an absence is an omitted field plus
	// the boolean that names it (never a published 0 — Rule 15a).
	payload.RecordCostRecorded = show.HasEndTotals
	if show.HasEndTotals {
		passes, tokens, elapsed := show.EndPasses, show.EndTokens, show.EndElapsedSec
		payload.CostPasses, payload.CostTokens, payload.CostElapsedSec = &passes, &tokens, &elapsed
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("session show: encode json: %w", err)
	}
	return string(b), nil
}

// jsonSessionDetail encodes the show face — the same struct, one row.
func jsonSessionDetail(d tools.ChatSessionDetail) (string, error) {
	payload := struct {
		sessionJSONRow
		Title       string `json:"title"`
		FirstMsg    string `json:"first_msg"`
		Workdir     string `json:"workdir"`
		LogPath     string `json:"log_path"`
		GitBranch   string `json:"git_branch"`
		GitRepoRoot string `json:"git_repo_root"`
		Parent      string `json:"parent_session_id"`
		EndReason   string `json:"end_reason"`
		// IndexNote carries the show page's `index` line in machine-readable
		// form — the session is indexed but its log is elsewhere — and is
		// OMITTED when the log is here: the disclosure is a fact about a gap,
		// so its absence (rather than an empty string) is what "no gap" looks
		// like on this face (Rule 15a: an optional outgoing field).
		IndexNote string `json:"index_note,omitempty"`
	}{
		// THE TURN KEYS COME FROM THE ONE CONVERSION (Rule 13). This literal
		// re-spelled the whole chats payload, which is how a key added to
		// sessionJSONRowOf could have been missing from the show face — and, since
		// the two must publish the same numbers under the same provenance
		// (`turns_provenance`), a second spelling here is exactly the drift the
		// epic's fix would invisibly undo on one face only.
		sessionJSONRow: sessionJSONRowOf(d),
		Title:          d.Title, FirstMsg: d.FirstMsg, Workdir: d.Workdir, LogPath: d.LogPath,
		GitBranch: d.GitBranch, GitRepoRoot: d.GitRepoRoot, Parent: d.ParentSession,
		EndReason: d.EndReason, IndexNote: sessionIndexNote(d),
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("session show: encode json: %w", err)
	}
	return string(b), nil
}

// sessionGroupCompletionNames is the `gopi session <TAB>` candidate set: the
// group's own list, referenced (never mirrored) so a sub-verb added to
// sessionSubcommandNames reaches completion by construction.
var sessionGroupCompletionNames = sessionSubcommandNames
