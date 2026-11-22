package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/ctxdump"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// chatStatusRowLegend is `gopi chat -h`'s status-row section, the ONE
// reader-visible statement of what each column of the row is. It is a NAMED
// constant joined into chatUsage so the text and the row cannot be changed
// apart (Rule 13), and every symbol the live block can print is listed HERE —
// `⧉N` was the one that was not, which is operator bug #3: a row that printed
// a symbol the product never defined.
const chatStatusRowLegend = `THE STATUS ROW, and what its numbers mean (read this before reading the row):
  The row under the prompt is a fixed-width read-out, not a decoration. Its
  right-hand group is TWO sub-cells — the context pair, then the prompt cache's —
  carrying three read-outs about THIS session, in this order:
    ctx92k/200k≡         the CONTEXT read-out, and the group's FIRST fact: the
                         CONVERSATION's estimated tokens (92k) against GOPI'S OWN
                         working budget (200k) — not the backend's advertised window
                         — and it LEADS because it predicts a compaction. The
                         denominator IS the budget (three digits, so "200k" not
                         "1M"); the gap, not the label, yields when cells run short.
                         The qualifier's SPELLING says where the count came from:
                           ≡  the backend's OWN prompt count, read back from its
                              last response (a measurement); its denominator is
                              gopi's trim budget, never the model's window
                           ≈  GOPI'S LOCAL ESTIMATE — the tokenizer's price for
                              the prompt about to be sent, because no response has
                              measured this context yet (a fresh or resumed
                              session): an approximation, labelled as such
                         Ladder, cell order and the measurements behind them:
                         devdoc/chat-help-layering.md.
    ↑12k↓4k              the prompt cache's SUB-CELL: 12k tokens the cache SERVED
                         for this prompt and 4k it WROTE. The hit rate below rides
                         it; nothing prints until the cache has answered.
    ` + statusHitRateCumulativeGlyph + `74.5%` + statusHitRateDeltaGlyph + `10%` + `             the PROMPT CACHE's hit rate, riding the
                         sub-cell above, printed as TWO readings side by
                         side, each told apart by its marker —
                         and the marker SAYS WHICH DENOMINATOR the share is of (a
                         percentage whose denominator a reader cannot state is not
                         a measurement — memory #5912):
    ` + statusHitRateCumulativeGlyph + `74.5%                the SESSION-cumulative rate: served/(served+written) over
                         EVERY prompt token THIS SESSION has sent — the pair to its
                         left, divided the same way. It is the reading an operator
                         steers by, and it keeps the fraction because it averages
                         everything.
    ` + statusHitRateDeltaGlyph + `10%                  THIS TURN's rate: the SAME formula over the DIFFERENCE of
                         those counters since this turn's request was issued, whole
                         percent (a tenth of a percent of a turn is noise). The
                         counters are DIFFED BEFORE the ratio, so ` + statusHitRateDeltaGlyph + ` is a turn-level
                         measurement, not the movement of the session average.
    ` + statusMetricsUnmeasured + `          what the group prints when NOT ONE of its read-outs has
                         been measured yet: a word about the group's OWN state,
                         never a "0" or a "0%" a reader could take for a value read.
    ` + statusNoToolLabel + `             the TOOLS fact, stated ONCE, in one column: ` + statusNoToolsLabel + `
                         when this session was started without ` + chatFlagTools + `,
                         ` + statusNoToolLabel + ` when it HAS the toolset and nothing has
                         been called yet this turn, and the tool's own name with a
                         one-line digest (` + toolGlyph + ` bash head -3 go.mod) once one has run.
    ` + lineedit.StatusEllipsis + `                    A FIELD THAT ENDED IN THIS MARK WAS CUT, not corrupted:
                         the mark says the text continues ("⚙ keyword_searc` + lineedit.StatusEllipsis + `",
                         not a broken name) — the one-cell marker the layout's own
                         cut path appends.
` + chatStatusRowLiveLegend + `
`

// chatStatusRowLiveLegend names the live block's own symbols — the counter that
// can PREFIX the state word and the words themselves. It is a SEPARATE constant
// because it is the part the reader meets on a long session and the part that
// was missing: `⧉2 · ready` prints a symbol nothing defined.
//
// Its form is written from the row's own constants (statusCompactionGlyph,
// statusLiveIdle) rather than typed as a sample: the legend must state what the
// row prints, and TestStatusRowSymbolsHaveAReaderVisibleDefinition resolves BOTH
// sides from those constants, so a glyph renamed on the row and not in the
// legend — or the reverse — fails the test instead of shipping a second
// undocumented symbol.
//
// The DEFINITION is the part that must be true off the screen: it points at the
// line the product prints when a compaction happens
// (agent.CompactionAnnouncement — the same constant the agent logs it from), so
// a reader who has just seen `⧉1` appear can find the event that produced it.
const chatStatusRowLiveLegend = `    ` + statusCompactionGlyph + `2` + lineedit.StatusSep + statusLiveIdle + `        the COMPACTION count of this session: how many times
                         the conversation outgrew gopi's working budget and its
                         oldest messages were summarised away into a memory
                         block. It is a count of EVENTS, not an amount of
                         anything, so it prints only once it has happened — a
                         session that has never compacted shows its elapsed
                         time in that cell instead (e.g. "4m00s"), and no "` + statusCompactionGlyph + `0" is ever
                         printed. Every compaction is reported as it happens,
                         through the agent's own one-line record of the event
                         (quoted whole, at a shallower indent — the record is
                         ONE line the agent writes, and breaking it here would
                         quote a line no run prints):
            ` + chatStatusRowCompactionSample + `
                         The cell is one of the reserved live-block suffixes,
                         dropped whole when the terminal is too narrow to hold
                         it beside the state word, never cut.

`

// chatStatusRowCompactionSample is the compaction record as the READER sees it,
// with a concrete number in it. It is QUOTED text, not a second definition of
// the record: TestStatusRowSymbolsHaveAReaderVisibleDefinition parses the number
// out of this line and rebuilds the line from the agent's OWN announcement
// constant (agent.CompactionAnnouncement), asserting the two are equal — so
// rewording the record without updating the help fails the test, and the sample
// cannot become a description of a line no run prints.
const chatStatusRowCompactionSample = agentPrefix + "memory compaction: removed 3 oldest message(s) — the notes this run recorded are carried at the head"

// agentPrefix is the tag the agent's own logger writes each line with
// (internal/agent/loop.go: log.New(os.Stderr, "[gopi] ", 0)). The legend quotes
// a logged line, so it carries the tag the reader sees on screen.
const agentPrefix = "[gopi] "

// chatUsage is the `gopi chat -h` text. Its THE STATUS ROW section is built
// from named parts (below) rather than typed inline: the row's own symbols and
// the words that define them must be ONE set, and
// TestStatusRowSymbolsHaveAReaderVisibleDefinition holds this text to the
// glyphs the status row can actually print (Rule 13 — a value spelled twice is
// two definitions).
//
// WHY THIS IS A `var` AND NOT A `const`. The film's own block (`--film`/`--out`)
// states the pace its animation is written at, and that number belongs to
// internal/gifgen (chatFilmGIFDelayCS). Composing it into the page with
// strconv.Itoa is what keeps the page from RESTATING the encoder's default as a
// literal — the alternative (a hand-written "25cs" in the help text) is the
// second definition Rules 1/13 exist to prevent. Go forbids a constant
// expression containing a function call, so the page is a var; nothing about the
// page's behaviour changes with that, and every reader of it is a string
// operation.
// REBASE NOTE (delivery round 2/3): master's 4bb7b207 added `--sgr-replay` and this
// run's 13ccfc26 rewrote the `--sgr-stream` synopsis word into its two directions.
// They are ORTHOGONAL — one ADDS a word, the other gives the existing word an OFF
// switch — so both survive; dropping either would delete a neighbouring run's
// advertised surface. The OFF switch is spelled from its constant (Rule 13), and
// the replay word from its own, so neither can drift from the parser.
var chatUsage = `Usage: gopi chat [--tools] [--no-editor] [` + chatFlagNoNotify + `|` + chatFlagNoEvents + `] [--no-color] [-v|--verbose] [--dump-screen[=PATH]|--no-dump-screen] [` + chatSgrStreamFlag + `[=PATH]|` + chatNoSgrStreamFlag + `] [` + chatSgrReplayFlag + ` SESSION|FILE] [--voice-audio FILE] [--resume SESSION] [--replay SESSION [` + chatFlagFromTurn + ` N]] [--policy POSTURE] [--film DIR [` + chatFlagOut + ` FILE.gif]] [` + config.ChatIdealGeometryFlagName + ` COLS` + config.ChatIdealGeometryDimSep + `ROWS] [` + config.AmbiguousWidthFlagName + ` ` + config.AmbiguousWidthWide + `|` + config.AmbiguousWidthNarrow + `] [` + config.ChatSurfaceTurnsFlagName + ` N] [raw-log-path|session-id]
       gopi chat --once [-p|--prompt PROMPT] [--tools] [--max-turns N]
                        [--max-seconds S] [--stop-on TEXT]

Start an interactive, multi-turn chat session. Unlike the single-shot
gopi prompt, gopi chat keeps a conversation context across turns: each
reply is sent as a free-form user message that continues the previous
conversation, and the model's text streams to stdout in real time.
` + chatOnceUsage + `
Interactive-session flags:

--no-color         no colour or emphasis anywhere in the chat chrome (banner,
                   prompt, status row, the answer's code/diff emphasis).
                   Automatic when NO_COLOR is set, when TERM=dumb, or when
                   stderr is not a terminal — a redirected run never receives an
                   ANSI byte, so a captured transcript is text.
                   The emphasis TABLE is ` + lineedit.EnvPalette + `, chosen by name and drawn
                   only where the terminal can draw it:
                     ` + lineedit.PaletteNameHermes + `        Hermes' warm gold family for a DARK terminal —
                               the answer body takes ` + lineedit.HermesDarkAnswerHex() + ` ("cornsilk")
                     ` + lineedit.PaletteNameHermesLight + `  the same roles for a LIGHT background
                   Both gold tables are 24-bit and are drawn only where the
                   terminal advertises it, ` + lineedit.EnvColorTerm + `=` + lineedit.ColorTermTruecolor + `;
                   without that the run draws the deepest table the terminal
                   CAN and says which on stderr. Detail: man gopi-chat.

--dump-screen[=PATH]
                   ON BY DEFAULT: every run writes THIS surface's own interpreted
                   screen state — the answer band, the editing line, the status
                   row — to chat-screen.dump, plus a .json twin, so gopi can read
                   what the chat screen shows WITHOUT pixels and without a vision
                   model.
                   With no PATH the dump lands beside this run's own session
                   artifacts (inside the sessions tree ui_state scans). Joined
                   form only: the path cannot be a separate argument, because
                   this verb's remaining positional argument is a session seed.
                   So this flag chooses the DESTINATION
                   (` + "`gopi chat " + chatDumpFlag + "`" + ` is how a run is ARMED), and
                   re-arms a dump in the face of a falsey GOPI_CHAT_DUMP; it is
                   not how the artifact is switched on.

--no-dump-screen   turn the screen dump OFF for this run: no artifact is written
                   and the editor's paint stream is handed through UNCHANGED, so
                   the run is byte-identical to one from before this capability
                   existed. Takes no value (a value is refused by name). The flag
                   wins over GOPI_CHAT_DUMP.

WHO READS IT — gopi itself, from inside, with no human in the loop:
                   - the answering turn, through its own ` + toolnames.ToolChatSurface + ` tool:
                     action=` + agent.ChatSurfaceActionSee + ` reads the painted grid as a cell grid, and
                     action=` + agent.ChatSurfaceActionCompare + ` checks a line it wrote against BOTH layers;
                   - the CLI verb, which audits the WHOLE answer as ONE call:
                       gopi ui_state --target chat --all
                     Nothing above asks a reader to assemble it by hand: the
                     audit derives its authority from the dump, so it needs no
                     line and no session. Its ledger word is outcome=` + uiStateAuditWordAgreed + ` /
                     ` + uiStateAuditWordDisagreed + ` / ` + uiStateAuditWordCouldNotRun + ` — exit 1 = the check could NOT
                     RUN, never "they agree". ` + "`gopi chat " + chatDumpFlag + "`" + ` arms a
                     run; ` + "`man gopi-chat`" + ` carries the damage vocabulary and the
                     single-line narrowing (` + uiStateFlagCompare + `,
                     ` + uiStateFlagSession + `, a different transcript).

` + chatSgrStreamFlag + `[=PATH]
                   ON BY DEFAULT: the renderer's RAW bytes — every paint write,
                   verbatim, unthrottled, no frame dropped — to
                   <session-id>.screen.stream, as length-prefixed frames between
                   a #gopi-screen-stream header and a #gopi-screen-stream-end
                   trailer (a pre-rename .sgr.stream / #gopi-sgr-stream file is
                   still READ; nothing new is written under that name).
                   WHY BESIDE THE DUMP: the dump is this program's
                   INTERPRETATION, so it cannot answer "was that wrong cell the
                   renderer or the reader?". These bytes replay through ANY
                   interpreter. Joined form only; with no PATH it lands beside
                   this run's own session artifacts. A replay is redirected into
                   this chat's scratch class, never the recording it reads.
                   Refused by name on a --film command line; set
                   GOPI_SCREEN_STREAM there instead — the producer inherits it
                   (GOPI_CHAT_SGR_STREAM is the pre-rename spelling, still read).
` + chatNoSgrStreamFlag + ` decline the raw stream for this run: no artifact is written.
                   A decline is ANNOUNCED at startup and in the closing report,
                   because a default that silently did
                   not arm would be indistinguishable from it. Both spellings on
                   one command line: the LAST wins; a falsey
                   GOPI_SCREEN_STREAM does the same. The contract in full is
                   on man gopi-chat.
` + chatSgrReplayFlag + ` SESSION|FILE
                   PLAY BACK a recorded raw byte stream: read the frames
                   the ` + chatSgrStreamFlag + ` switch wrote and write them to stdout VERBATIM,
                   in order, adding nothing and parsing nothing. THE ONE EVIDENCE
                   CHANNEL THAT DOES NOT DEPEND ON THE RENDER ALGORITHM: --replay
                   re-RUNS a session, so its output is this build's reading of it (turn
                   numbering, cache rows, layout, throttle) and two builds correctly
                   differ. A raw stream is what the renderer actually wrote, so two
                   builds replaying one file must produce byte-identical output —
                   compare the sha256 of the two runs. The operand is an existing
                   .screen.stream path (used as written; a pre-rename .sgr.stream is
                   accepted too) or a session id (its stream is played). FOR COMPARING
                   TWO RECORDINGS frame by frame — the check this one cannot do by
                   eye — use ` + "`gopi " + CliVerbSession + " " + sessionSubcmdReplay + " <id> --verify <other>`" + `. CONTROLS
                   are --replay's own: any key pauses, any key resumes, Esc leaves. The
                   picture carries NO text of its own (a state word between two frames
                   is a byte the recording never held), so the state is read off stdout:
                   a paused run stops growing. With stdin redirected there is no
                   watcher: no throttle, no controls. Streams a run never closed out
                   play to their last complete frame and SAY SO on stderr, after the
                   picture. The PRE-RENAME ALIASES (each prints deprecated: use
                   <current>): ` + chatSgrStreamDeprecatedFlag + `, ` + chatNoSgrStreamDeprecatedFlag + ` and ` + chatSgrReplayDeprecatedFlag + `.
                   Full contract: ` + "`man gopi-chat`" + `.
--voice-audio FILE name the audio the /voice mode READS instead of a
                   microphone: a 16kHz mono s16le PCM file, or a WAV of exactly
                   that geometry (anything else is refused by name, with the
                   rate, channels and depth it carries). NO capture device is
                   enumerated, probed or opened, so the mode runs on a host with
                   no sound card at all. The same channel from the environment is
                   GOPI_CHAT_VOICE_AUDIO (the flag wins), and a bare flag is a
                   usage error, never a silent fall back to the microphone.
--no-editor        disable the raw-mode line editor and read plain cooked lines
                   (the pre-editor behavior). Automatic whenever
                   stdin is not a terminal — a pipe, a redirect, a test
                   harness — in which case the input path is byte-identical
                   to it, editor or not: the editor is only ever entered on
                   a real terminal, and falls back with a notice if the
                   terminal refuses raw mode.
--no-notify        decline the chat event slot this run files by DEFAULT, so no
                   notify.cmd delivery (task events: failed, build-failed,
                   cancelled, ...) reaches this chat. --no-events is the same
                   switch under the name the capability has in the operator's
                   own vocabulary; both write no chat_session_id_*.txt and both
                   PRINT that they declined and into which directory, so
                   "this chat receives nothing" is never mistaken for "the
                   write went somewhere I did not look" (Rule 15). Default is
                   SUBSCRIBE: the slot is written, and the run says where.

-v, --verbose      print the startup plumbing (session paths, the loaded
                   GOPI.md size, the live editor path, the status-row wake
                   caveat, the tools-mode banner, the resolved policy posture
                   and its rule count). Off by default: the bar
                   above the prompt carries one line, not eight.

--tools            tool-enabled chat ("ops mode", claude-code style): the
                   model can issue real tool calls — read/write/edit files,
                   run bash, search code, fetch the web, store memory —
                   inside the conversation, so you can ask for actual work
                   ("fix this failing test", "summarize yesterday's git
                   log") without leaving the chat. Default off: plain
                   conversational Q&A, no tools, cheapest path.

` + chatGeometryUsage + chatAmbiguousWidthUsage + chatSurfaceTurnsUsage + chatPrefetchUsage + `--policy POSTURE   the chat POSTURE: the answer this run gives a command line
                   that no rule matched.
                   ` + chatPolicyWordsHelp() + `
                   (` + chatPolicyDefaultNote() + ` is the default — every command
                   is a question).
                   Precedence: this flag > ` + agent.ChatPolicyEnv + ` >
                   settings.json "` + agent.ChatPolicySettingsKey + `" > the default.
                   The posture is resolved ONCE at startup and FROZEN. Rules
                   come from the fenced chat-policy block of the project
                   GOPI.md; --verbose prints how many were loaded. ENFORCED:
                   the chat write boundary (internal/tools/chat_write_guard.go)
                   judges tool calls against this frozen posture, REFUSING one
                   it forbids and running one it allows. Still no promise a
                   yolo posture runs your commands — a fence rule can narrow
                   it, and a command that runs can fail on its own.

--resume SESSION   continue a session the seed argument below could name (a
                   session UUID or a raw-log path), restoring the OPTIONS that
                   session was started with from its own record — the recovery
                   path for a crashed chat (see below). The joined form
                   --resume=<SESSION> is accepted too. Refused when the seed
                   positional names a DIFFERENT session: one resume continues
                   one session.
[raw-log-path]     optional path to a ~/.gopi/raw/gopi-raw-*.log whose
                   checkpoint is loaded as the conversation seed (continue
                   an earlier session from where it left off). Omitted:
                   the chat starts empty.
[session-id]       a session UUID (e.g. from 'gopi session log --last'); the
                   matching raw log is located automatically.

Recovering a chat that CRASHED (killed, power loss, panic):
  every chat RECORDS the options it was started with, so a crashed one comes
  back with them. Find the session, then resume it:
    gopi session log --sessions     list sessions (a crashed one has no END row)
    gopi chat --resume <session-id>
                                   continue it WITH the options that session
                                   was started with (--tools, --no-color, a
                                   --dump-screen destination, --no-dump-screen,
                                   ...), restored from the session's own record.
                                   One word, no command line to reconstruct.
    gopi chat <session-id>         the same resume through the seed argument
                                   above; it restores the recorded options too,
                                   but only when this command line states none
                                   of its own.
  WHAT WINS: the recorded options are COMBINED with this command line, so
  anything written here adds to them — and overrides them for the one option
  that has two candidate values (which dump destination, or no dump at all).
  "gopi session list" shows chat ROWS, which only exist once a turn has
  completed: a chat killed before its first reply is in "gopi session log --sessions"
  and has no row at all. What restoration can and cannot do, and the option an
  older gopi recorded, are stated at start-up and documented in man gopi-chat.

` + chatReplayGroupUsage + `
Slash commands (type a line starting with '/'):
  /quit    end the session and exit (also /exit — one command, two spellings)
  /clear   forget the conversation and start fresh (also /new)
  /tools   flip between tool-enabled and plain chat mid-session
  /queue   queue a prompt to run as the NEXT turn — no interrupt, no retyping
  /queue <prompt>
           park a prompt: it becomes the next turn's input when the current turn
           ends (no extra Enter). Bare /queue lists what is parked; /queue clear
           empties it. Bounded — an add past the bound is refused by name, never
           silently dropped.
  /help    print this help plus the shared slash-command table
  /help <cmd>
           detailed usage for ONE command (also -h on any command)
  /voice   continuous local VOICE INPUT: records the microphone and transcribes
           on this machine every ` + fmt.Sprintf("%d", voiceChatWindowSeconds) + `s, APPENDING each transcript to the
           input line so you keep editing while you speak; Enter sends the whole
           line as ONE ordinary turn, Esc (or /voice stop) leaves the mode and
           keeps the line. Bare /voice toggles, /voice status only reports.
  /screen  capture ONE turn's screen (the newest, or the turn N you name) into a
           FILE and make the NEXT turn's message carry its path and size — never
           the bytes. [N] [--out PATH]. Local, immediate; costs no model turn.
           (/context, /info and /voice document themselves through /help <cmd>, so
           the one page does not carry a second, hand-maintained copy of them.
           /context's export lands in ` + ctxdump.FileNamePattern + ` — a FILE holding
           the exact request the model received, one per turn and named for it.)

CLI subcommands are exposed in-chat through the shared command table
(same dispatch the web UI uses — executed locally, never a model turn):
  /task list|show|new|update|search   the task ledger (gopi task engines)
  /log last|sessions|summary|read|search
                                      session log reader (gopi session log engines)
  /text2img <text> [--out P]          render text to PNG
Each also answers /help <cmd> or -h.

Exit with Ctrl+D (EOF) on an empty prompt or the /quit command.

` + chatStatusRowLegend + `

Exit status: 0 = the session ended the caller's own way (a clean /quit or
EOF); 1 = the run failed (an unresolvable seed, a checkpoint that does not
load); ` + "`2 = usage error`" + ` (an unknown flag, a bad flag value, more than
one positional word). A flag this verb does not define is refused BY NAME as
an unknown flag — never re-read as the [raw-log-path|session-id] positional,
and an unrecognised word is never silently dropped.

Examples:
  gopi chat
  gopi chat --tools
  gopi chat ~/.gopi/raw/gopi-raw-20260826-120000.log
  gopi chat 4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33
`

// This verb's own argv vocabulary (Rule 1b/13: every word compared on, accepted
// or printed is declared once and referenced). The REPL's parser is a CLOSED
// vocabulary: a word that is not one of these — and not the one positional slot
// the synopsis documents — is refused by name rather than quietly read as a
// session seed (see chatRefuseUnknownToken).
const (
	// chatFlagTools is the tool-enabled chat switch ("ops mode").
	chatFlagTools = "--tools"
	// chatFlagNoEditor drops the raw-mode line editor for cooked lines.
	chatFlagNoEditor = "--no-editor"
	// chatFlagNoNotify is the chat event slot's OFF SWITCH: `gopi chat
	// --no-notify` declines the subscription this run files by default, so a
	// `notify.cmd` delivery has no slot of this run's to address. It is the
	// documented way out of a default (Rule 13: the off spelling is compared on,
	// so it is a constant and never a typed literal) — the capability is ON by
	// default and stays that way.
	chatFlagNoNotify = "--no-notify"
	// chatFlagNoEvents is the SECOND spelling of the same switch. The capability
	// is named "events" in the operator's own vocabulary and "notify" in the
	// wiring's, and a caller should not have to guess which noun this verb
	// chose; ONE switch, two words, and the ARM POINT cannot tell them apart.
	chatFlagNoEvents = "--no-events"
	// chatFlagNoColor is the colour switch, taken from the package that owns the
	// colour decision (never re-typed here — the export IS the definition, so a
	// rename follows the compiler).
	chatFlagNoColor = lineedit.FlagNoColor
	// chatFlagVerbose / chatFlagVerboseShort print the startup plumbing.
	chatFlagVerbose      = "--verbose"
	chatFlagVerboseShort = "-v"
	// chatFlagResume is the resume switch: `gopi chat --resume <session-id>`
	// continues a crashed session with the options it was started with, so the
	// operator types one word instead of replaying the original command line.
	// Its target is the SAME thing the positional seed takes (a session id or a
	// raw-log path) and it resolves through the SAME resolver — see
	// sessionevent.EventTypeResumeOptions for why the options have to be
	// recorded at all.
	chatFlagResume = "--resume"
	// chatFlagAnswerAlone is the DECLINE of the unattended context on the
	// one-shot face: `gopi chat --once --answer-alone` runs one answer with an
	// ordinary interactive prompt. It exists because the default is the other
	// way round (see chatOnceUsage): a one-shot face's answer is consumed by a
	// script or an agent, i.e. by nobody who can reply, so the run is told that
	// — and the two cases where a HUMAN is on the other end (a single
	// question-and-answer step, a scripted run a person is watching) get this
	// one word rather than the default costing them a wrong prompt.
	//
	// It is deliberately NOT in chatResumeOptions: the context is a property of
	// the FACE this run is invoked on, not of the session, so a later
	// invocation decides it again from its own argv (`--once` → unattended;
	// a plain `gopi chat` → interactive).
	chatFlagAnswerAlone = "--answer-alone"
	// chatFlagValueSep is what joins a valued flag to its value inline
	// (--resume=<id> as well as --resume <id>).
	chatFlagValueSep = "="
)

// chatGeometryFlag is this verb's spelling of the ideal-geometry knob. It is
// derived from the knob's own flag name (Rule 1b/13/14) so the parser, the usage
// page and the catalog row cannot spell three different flags.
//
// WHY THE VERB ACCEPTS IT AT ALL, given the top-level hoist
// (config.BootstrapEnvFromArgs): the hoist makes `gopi --chat-ideal-geometry
// 100x30 chat` work, but the spelling an operator reaches for on THIS verb is
// the one AFTER the verb (`gopi chat --chat-ideal-geometry 100x30`), and a
// vocabulary that refused it while advertising the knob would be the closed
// vocabulary refusing a flag the help names. Both spellings publish through the
// SAME writer (config.PublishChatIdealGeometryFlag), so there is ONE
// representation and one precedence rule — the flag always beats the export.
const chatGeometryFlag = "--" + config.ChatIdealGeometryFlagName

// chatSurfaceTurnsFlag is this verb's spelling of the self-driven turn budget
// knob, derived from the knob's own flag name (Rule 1b/13/14) so the parser, the
// usage page and the catalog row cannot spell three different flags. It is
// accepted AFTER the verb for exactly the reason the geometry is: the spelling an
// operator reaches for on `gopi chat` is the one that follows it, and the closed
// vocabulary must not refuse a flag the help page advertises. Both spellings
// publish through the SAME writer (config.PublishChatSurfaceTurnsFlag), so there
// is ONE representation and one precedence rule — the flag always beats the
// export.
const chatSurfaceTurnsFlag = "--" + config.ChatSurfaceTurnsFlagName

// chatAmbiguousWidthFlag is this verb's spelling of the A-class width reading,
// derived from the knob's own flag name (Rule 1b/13/14) so the parser, the usage
// page and the catalog row cannot spell three different flags. It is accepted
// here for the same reason the geometry is: the spelling an operator reaches for
// is the one AFTER the verb. The value is published through
// config.ApplyEnvAmbiguousWidth — the knob's ONE env representation — so the
// verb holds no second field and the ONE reader sees every channel.
const chatAmbiguousWidthFlag = "--" + config.AmbiguousWidthFlagName

// The exit statuses `gopi chat` documents. chatExitUsage is the POSIX usage
// class (a malformed command line: an unknown flag, a bad flag value, more than
// one positional word) and it is the SAME value chatOnceExitUsage carries for
// the one-shot face of this verb — one verb, one meaning for 2. They are
// separate constants because the two faces have separate contracts (Rule 1a),
// and the equality is asserted in the repro test rather than assumed.
const (
	// chatExitOK is a session that ended the caller's own way, or a completed
	// turn.
	chatExitOK = 0
	// chatExitFailed is a run that failed (an unresolvable seed, a failed
	// checkpoint load, a failure past startup).
	chatExitFailed = 1
	// chatExitUsage is a usage error: the command line is wrong.
	chatExitUsage = 2
)

// The shapes of the refusal message and its usage line (Rule 1b: the prefix and
// the separators are spelled once).
const (
	// chatErrorPrefix heads this verb's own human lines on stderr.
	chatErrorPrefix = "gopi chat: "
	// chatFlagPrefix is what makes an argv word a FLAG on this surface: the
	// dash. It is what the refusal branches on, so it is a name, not a literal.
	chatFlagPrefix = "-"
	// chatFlagListSep joins the accepted vocabulary in the refusal message.
	chatFlagListSep = ", "
	// chatUsageLine is the ONE-line synopsis printed with a usage refusal: the
	// page itself is long, and a caller who mistyped a flag needs the shape of
	// the command line, not the whole manual.
	// The synopsis is COMPOSED from the same names the parser reads (Rule 13),
	// including the three this repair added to the vocabulary: a one-line usage
	// that omits a flag the verb accepts is the same defect as a refusal list
	// that omits it, one level up.
	chatUsageLine = "usage: gopi chat [--tools] [--no-editor] [" + chatFlagNoNotify + "|" + chatFlagNoEvents + "] [--no-color] [-v|--verbose] " +
		"[--dump-screen[=PATH]|--no-dump-screen] [--resume SESSION] [--replay SESSION] " +
		"[" + chatSgrStreamFlag + "[=PATH]|" + chatNoSgrStreamFlag + "] [" + chatSgrReplayFlag + " SESSION|FILE] [--film DIR [" + chatFlagOut + " FILE.gif]] " +
		"[" + chatGeometryFlag + " COLS" + config.ChatIdealGeometryDimSep + "ROWS] " +
		"[" + chatAmbiguousWidthFlag + " " + config.AmbiguousWidthWide + "|" + config.AmbiguousWidthNarrow + "] " +
		"[" + chatSurfaceTurnsFlag + " N] [raw-log-path|session-id]"
	// chatSeededToolsMismatchFormat is the ONE sentence every face prints when
	// a seeded checkpoint came from a TOOL-ENABLED run but this run has tools
	// off: recovering a crash must not silently strip the capability the
	// crashed run had. %s is the PROGRAM NAME (each face names itself, Rule 14
	// — one rule, two renditions), %d how many tool definitions the checkpoint
	// carried, and the last %s the exact re-run recipe that restores them (a
	// phrase, so a face whose address is positional can spell the whole
	// command line instead of one flag).
	chatSeededToolsMismatchFormat = "%s: ⚠ the seeded session was TOOL-ENABLED (%d tool definition(s) in its checkpoint) " +
		"but this run has tools OFF — the model will answer from context alone and cannot run them. " +
		"Re-run with %s to resume with the same capabilities.\n"
	// chatWordFlag / chatWordArgument name WHICH KIND of word was refused. The
	// distinction is the whole point of this fix: a `--word` that is not a flag
	// of this verb is refused AS a flag, never silently read as the positional
	// session seed.
	chatWordFlag     = "flag"
	chatWordArgument = "argument"
)

// chatSeededToolsMismatch builds the warning printed when a seeded checkpoint
// came from a TOOL-ENABLED run while this run has tools off — recovering a
// crash must not silently strip the capability the crashed run had.
//
// It is a pure function of (checkpoint, toolsEnabled) and returns "" when there
// is nothing to warn about, because THAT decision is the behaviour under test:
// a test that had to drive the whole REPL to learn whether the warning fires
// would be testing stdin, not this rule.
func chatSeededToolsMismatch(ckpt *deepseek.ClaudeRequest, toolsEnabled bool) string {
	return seededToolsMismatch(chatResumeRecordProgName, chatFlagTools, chatResumeToolDefCount(ckpt), toolsEnabled)
}

// chatResumeToolDefCount is how many tool definitions a seeded checkpoint
// carries (0 for no checkpoint) — the ONE reading of that number, so a face
// cannot report a count the others would not.
func chatResumeToolDefCount(ckpt *deepseek.ClaudeRequest) int {
	if ckpt == nil {
		return 0
	}
	return len(ckpt.Tools)
}

// seededToolsMismatch is the ONE rule behind the capability-loss warning every
// face prints (Rule 14): a checkpoint that carried tool definitions, seeded into
// a run that has tools off, is a LOST CAPABILITY and must say so — naming the
// remedy that actually restores it on the face printing it.
//
// It is a pure function so each face's verdict is testable without driving a
// server or a REPL, and returns "" exactly when nothing was lost (no checkpoint,
// tools already on, or a checkpoint that never carried tools) — a warning that
// fires on a healthy resume is noise, and noise is how a real warning gets
// ignored.
func seededToolsMismatch(progName, remedy string, toolDefs int, toolsEnabled bool) string {
	if toolsEnabled || toolDefs == 0 {
		return ""
	}
	return fmt.Sprintf(chatSeededToolsMismatchFormat, progName, toolDefs, remedy)
}

// chatREPLFlags is this verb's accepted flag vocabulary, in one list. It is the
// source of the refusal message AND the walk list of the closed-vocabulary
// guard (chat_argv_test.go) — so a flag added to the parser and forgotten here
// (or listed here and unparsed) fails the suite instead of drifting.
//
// chatOnce usage (--once, -p, --prompt, --turns, --max-seconds, --stop-on) is
// NOT here: isChatOnceRequested dispatches that face before this parser runs,
// and chatOnceParseArgs owns its own vocabulary. `gopi chat --once --bogus` is
// refused by THAT parser with the same usage status.
func chatREPLFlags() []string {
	return []string{
		HelpFlag,
		HelpFlagLong,
		chatFlagTools,
		chatFlagNoEditor,
		chatFlagNoNotify,
		chatFlagNoEvents,
		chatFlagNoColor,
		chatFlagVerbose,
		chatFlagVerboseShort,
		chatDumpFlag,
		chatFlagReplay,
		// --from-turn is a word THIS VERB READS beside --replay (where in that
		// recording the run begins), so it belongs in the same list --replay is
		// in: leaving it out would make the usage page and every unknown-flag
		// refusal describe a command line that cannot express a fast-forward.
		chatFlagFromTurn,
		// --film belongs in this list for the same reason every other word does:
		// it is a word this verb READS, and leaving it out makes the usage page
		// (and every unknown-flag refusal) describe a command line that cannot
		// express filiming a replay.
		chatFlagFilm,
		// --out names the ANIMATION a film's frames are encoded into — the film's
		// second half — and it is in this list for the same reason every other
		// word is: it is a word this verb READS, and leaving it out makes the
		// usage page (and every unknown-flag refusal) describe a command line
		// that cannot express the command the operator types.
		chatFlagOut,
		// --voice-audio names the FILE `/voice` reads instead of a microphone
		// (chat_voice_audio.go). It is in this list for the reason every other
		// word is: it is a word this verb READS, and a usage page that omits it
		// describes a command line that cannot express a file capture.
		chatVoiceAudioFlag,
		// The dump's OFF SWITCH belongs in this list for the same reason the arm
		// flag does: it is a word this verb READS, and leaving it out makes the
		// usage page (and every unknown-flag refusal) describe a command line
		// that cannot express the default's way out.
		chatNoDumpFlag,
		chatFlagResume,
		// The ideal-geometry flag belongs here because this verb READS it: it is
		// the post-verb spelling of a knob the help page names, and a closed
		// vocabulary that refused the flag its own page advertises would refuse
		// the documented command line (the same reason chatNoDumpFlag is listed).
		chatGeometryFlag,
		// --film-terminal is the same case one level down: a knob of the FILM
		// (which terminal it runs inside), advertised on the help page and read
		// by this verb's own walk. A closed vocabulary that refused it would
		// refuse `gopi chat --replay <id> --film --film-terminal xfce4-terminal`,
		// which is the command the knob exists for.
		chatTerminalFlag,
		// --policy names the POSTURE this run resolves at startup (the answer
		// given to a command line no rule matched). It is in this list because
		// this verb READS it and the page advertises it; the value it carries is
		// what makes it a VALUED flag too (chatResumeValuedFlags).
		chatPolicyFlag,
		// --wechat is the same case: a word this verb READS (the phone-shaped
		// animation), advertised on the help page. It takes NO value — a BARE
		// switch — so it is in this list and deliberately NOT in
		// chatResumeValuedFlags: the closed-vocabulary walk hands a flag a value
		// slot only when that list says it takes one, and walking this one bare is
		// what proves it parses as a switch.
		chatFlagWechat,
		// --answer-alone belongs here for exactly the reason every other word
		// does: this verb READS it, and a closed vocabulary that refused the
		// flag its own page advertises would refuse the documented command line
		// (`gopi chat --once --answer-alone "…"`). It is the DECLINE of the
		// one-shot face's unattended default (chatFlagAnswerAlone), so whoever
		// wants an ordinary single answer can say so.
		chatFlagAnswerAlone,
		// --screen-stream is advertised on this page and READ by this verb (it
		// records the renderer's raw bytes), yet it was ABSENT from this list —
		// so `gopi chat --screen-stream=x -h` was refused as an unknown flag by the
		// very verb that defines it, and the refusal's vocabulary described a
		// command line that cannot arm the recording. Measured 2026-10-27 on the
		// shipped binary: the refusal listed 21 words and this was not one.
		chatSgrStreamFlag,
		// --screen-replay is this verb's PLAYBACK word: it plays the bytes the switch
		// above recorded (chat_sgr_replay.go). It is in this list for the reason
		// every other word is — the verb READS it, so a closed vocabulary that
		// omitted it would refuse the command line its own page advertises.
		chatSgrReplayFlag,
		// The PRE-RENAME spellings of the three words above. They are in this list
		// for exactly the reason every other word is: the parser still ACCEPTS them
		// (they are deprecated aliases, not dead words), so a closed vocabulary
		// that omitted them would refuse a command line this binary obeys — and the
		// operator's own shell history is the most likely source of one.
		chatSgrStreamDeprecatedFlag,
		chatNoSgrStreamDeprecatedFlag,
		chatSgrReplayDeprecatedFlag,
		// --no-screen-stream is the OFF switch of that same capability, and it is in
		// this list for the reason chatNoDumpFlag is: the stream is armed by
		// DEFAULT, so a closed vocabulary that refused the way OUT would leave
		// every caller with a switch they cannot turn off — and the page
		// advertises it. Bare switch, so it is deliberately NOT in
		// chatResumeValuedFlags.
		chatNoSgrStreamFlag,
		// Same shape, same defect: `--ambiguous-width` and `--chat-surface-turns`
		// are advertised in the usage line above and parsed by this verb, and both
		// were missing from the refusal vocabulary.
		chatAmbiguousWidthFlag,
		chatSurfaceTurnsFlag,
	}
}

// chatResumeValuedFlags is the list of this verb's flags that TAKE A VALUE in
// their next argv slot, so that `--resume <id>` and every other valued flag
// can be skipped as a pair when the command line is recorded
// (chatRecordableFlags) — a valued flag left unpaired would record the VALUE
// as if it were a bare option.
//
// The ideal-geometry flag is HERE because it genuinely takes a value in the next
// slot (that is what this list means), and the vocabulary walk
// (TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses) uses the same list to
// give every advertised flag a value when it exercises it. It is NOT in this
// verb's RECORDED options: it never reaches `recordable`, because the parser
// consumes it before the switch — the geometry is a property of the terminal a
// run is on, and a resume that restored it would ask a future terminal to be the
// size this one happened to be.
//
// `--replay` and `--film` are here for a SECOND reason beside the record: the
// closed-vocabulary walk (TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses)
// asks THIS list which declared flags are valued before it hands one a value
// slot, and a valued flag missing from it is walked bare — `--film -h` — which
// is refused BY DESIGN (a directory cannot be a flag). The walk then reports the
// flag as unparsable, and that failure is the list's, not the flag's: measured
// while adding --film.
func chatResumeValuedFlags() []string {
	return []string{
		chatFlagResume,
		chatFlagReplay,
		// --from-turn TAKES A VALUE (a turn number): the closed-vocabulary walk
		// hands a valued flag the word in its next slot, and a flag missing from
		// here would have its number read as a bare positional — i.e. as a
		// session to continue (the exact confusion --replay's presence in this
		// list already prevents).
		chatFlagFromTurn,
		chatFlagFilm,
		chatFlagOut,
		// --voice-audio TAKES A VALUE (the audio file `/voice` reads) and it is in
		// this list for the reason the list exists: the closed-vocabulary walk hands
		// a valued flag the word in its next slot, and a flag missing from here
		// would have its value read as a bare positional (chat_voice_audio.go).
		chatVoiceAudioFlag,
		// --screen-replay TAKES A VALUE (a session id or a stream path), and it is here
		// for the list's own reason: without it the walk would hand its operand back
		// as a bare positional — i.e. as a session to continue.
		chatSgrReplayFlag,
		// The deprecated replay spelling TAKES A VALUE too, for the same reason:
		// without it the closed-vocabulary walk would hand its operand back as a
		// bare positional (a session seed).
		chatSgrReplayDeprecatedFlag,
		chatGeometryFlag,
		chatTerminalFlag,
		// --policy TAKES A VALUE (a posture word) and it is here for the reason
		// the list exists: without it the walk would hand its value back as the
		// next iteration's word and read a posture name as a positional.
		chatPolicyFlag,
		// --ambiguous-width and --chat-surface-turns both READ THE NEXT WORD as
		// their value. Without an entry here the closed-vocabulary walk would
		// hand that value back as a bare positional — and, worse, the duplicate
		// check (chatValuedFlagStatedBefore) would not see a flag stated twice.
		// --screen-stream is deliberately NOT here: it takes NO separate slot (its
		// path must be joined with "="), which is exactly the property that makes
		// it safe to spell beside a session seed.
		chatAmbiguousWidthFlag,
		chatSurfaceTurnsFlag,
	}
}

// chatResumeOptionWord reports whether an argv word is one of the OPTIONS this
// verb records for a later resume, and chatApplyResumeOption applies it.
//
// The two share ONE table (chatResumeOptions) for the reason Rule 1b/13 gives:
// a recorded word that no longer applies, or an applicable word that is never
// recorded, is a resume that silently restores less than it captured. The
// table is the whole vocabulary of this feature.
//
// What is NOT recorded, and why: the resume target itself (it names WHICH
// session — the record already lives in that session's file, and a session id
// printed into a resumed run's flags would make the next resume re-seed
// itself), and the argv words this verb refuses (an unknown flag never
// reaches a record because the refusal fires first).
func chatResumeOptionWord(a string) bool {
	for _, f := range chatResumeOptions() {
		if a == f {
			return true
		}
	}
	return false
}

// chatResumeOptions is the recorded-option vocabulary: every flag a resumed
// run must have to be the run it was. It is a SUBSET of chatREPLFlags() —
// the help flags are answered and never recorded, and --resume is consumed
// before the record is written (a resumed run records the options that made
// it, not the word that asked for the resume).
func chatResumeOptions() []string {
	return []string{
		chatFlagTools,
		chatFlagNoEditor,
		chatFlagNoNotify,
		chatFlagNoEvents,
		chatFlagNoColor,
		chatFlagVerbose,
		chatFlagVerboseShort,
		chatDumpFlag,
	}
}

// chatValuedFlagWord reports whether an argv word is one of this verb's flags
// that take a value in their next argv slot — the two spellings included
// (`--resume` and `--resume=ID`). It reads chatResumeValuedFlags, the ONE list
// the parser and the closed-vocabulary walk already share (Rule 13), so a flag
// that gains a value slot is covered by the usage-query arm in the argv walk
// without being named there a second time.
func chatValuedFlagWord(a string) bool {
	for _, f := range chatResumeValuedFlags() {
		if a == f || strings.HasPrefix(a, f+chatFlagValueSep) {
			return true
		}
	}
	return false
}

// chatUsageQueryWord reports whether a word is one of the two spellings of the
// usage query this verb answers FIRST (`-h` / `--help`). The two doors are
// declared once (HelpFlag / HelpFlagLong) and read from here by every surface
// that has to recognize the query inside another flag's slot on either chat
// face, so the beat protocol's parser cannot hold its own idea of the spelling.
func chatUsageQueryWord(a string) bool { return a == HelpFlag || a == HelpFlagLong }

// chatValuedFlagSpelling is the flag a valued-flag WORD names, with its inline
// value removed (`--from-turn=2` → `--from-turn`) — the identity the walk needs
// to ask "was this flag already stated on this line". "" means the word is not a
// valued flag at all. It reads the ONE list (chatResumeValuedFlags, Rule 13), so
// a flag that gains a value slot is covered by the duplicate rule below by
// construction rather than by being named there a second time.
func chatValuedFlagSpelling(a string) string {
	for _, f := range chatResumeValuedFlags() {
		if a == f || strings.HasPrefix(a, f+chatFlagValueSep) {
			return f
		}
	}
	return ""
}

// chatValuedFlagStatedBefore reports whether the valued flag named by args[i] was
// already stated EARLIER on the same command line — the question a usage query
// must still answer with a refusal, because being stated twice is a statement
// about the COMMAND LINE, not about a value.
//
// WHY THE WALK NEEDS IT. The query arms below step over a valued flag's value
// slot WITHOUT running that flag's parser (that is what makes a query win over a
// value the parser could not use). The parser is also where "was given twice"
// lives, so stepping over it skipped the duplicate check too, and
// `gopi chat --from-turn 2 --from-turn 3 -h` answered the PAGE at rc 0 — a
// command line the same walk refuses at rc 2 the moment the query is removed.
// Measured 2026-10-27 (master 6f6f7ff) on the real binary: with `-h` exit 0 and
// the replay page on stdout; without it exit 2 and "--from-turn was given twice
// (2 and 3) — one run begins at one turn".
//
// The rule stays ONE-SIDED: it is consulted only when a query is on the line
// (the caller's condition), so a command line with NO query is refused by the
// flag's own parser exactly as before, with that parser's own sentence.
func chatValuedFlagStatedBefore(args []string, i int) bool {
	spelling := chatValuedFlagSpelling(args[i])
	if spelling == "" {
		return false
	}
	for _, earlier := range args[:i] {
		if chatValuedFlagSpelling(earlier) == spelling {
			return true
		}
	}
	return false
}

// chatUsageQueryOnLine reports whether a usage query appears ANYWHERE on the
// command line — the question a valued flag's own parser cannot ask, because a
// parser sees one word pair and has no idea what followed it.
//
// WHY THE WALK NEEDS IT. The arm beside `chatValuedFlagWord` answers a query
// that sits IN a valued flag's VALUE SLOT (`gopi chat --replay -h`). A query
// that comes LATER on the line — `gopi chat --from-turn some-target -h`, and
// every `flag DUMMY -h` the vocabulary walk types — never reached it: the flag's
// parser read the ordinary word in the slot, its value refused it, and
// `chatRefuseUsage` returned exit 2 before the page was ever asked for. Measured
// 2026-10-27 at master d2dcd2b: `--from-turn some-target -h` exited 2 with the
// "is not one" refusal while its eight siblings answered the page, because
// --from-turn is the only valued flag whose parser VALIDATES what it reads (the
// others accept any word and fail later, or not at all).
//
// The spelling is read from the one definition (chatUsageQueryWord), so a third
// spelling of the query joins both doors by construction.
func chatUsageQueryOnLine(args []string) bool {
	for _, a := range args {
		if chatUsageQueryWord(a) {
			return true
		}
	}
	return false
}

// chatParseGeometryArg parses this verb's spelling of the ideal-geometry knob:
// the bare flag, its inline value (--chat-ideal-geometry=100x30) and its spaced
// value. consumed=false means the word is not this flag at all.
//
// A missing or empty value is a USAGE error naming the flag — the same rule the
// resume switch applies, for the same reason: a bare `--chat-ideal-geometry`
// would otherwise be swallowed and the run would draw at the built-in default
// while the operator believes they sized the screen (Rule 15).
//
// The VALUE ITSELF is not validated here. The ONE validator lives beside the
// resolution (config.parseChatIdealGeometry), and it REPORTS an unusable value
// and falls through to the next channel rather than refusing the run — so a
// typo cannot make `gopi chat` unusable, and the operator still learns their
// declaration was not understood.
// chatParseAmbiguousWidthArg parses this verb's spelling of the A-class width
// knob: the bare flag, its inline value (--ambiguous-width=wide) and its spaced
// value. consumed=false means the word is not this flag at all, and ok=false
// means it is not this flag either — the two are separate because a BARE flag
// (no value in this slot and none after it) is a usage error this parser reports
// by returning consumed=false with ok=true, which is what lets the caller name
// the flag and the two accepted values instead of letting the word fall through
// to a generic "unknown flag" (Rule 15: the refusal must say what was wrong).
func chatParseAmbiguousWidthArg(a, next string, hasNext bool) (value string, consumed, ok bool) {
	if a != chatAmbiguousWidthFlag && !strings.HasPrefix(a, chatAmbiguousWidthFlag+chatFlagValueSep) {
		return "", false, false
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		return a[k+len(chatFlagValueSep):], true, true
	}
	if !hasNext {
		return "", false, true
	}
	return next, true, true
}

func chatParseGeometryArg(a, next string, hasNext bool) (value string, consumed bool, err error) {
	if a != chatGeometryFlag && !strings.HasPrefix(a, chatGeometryFlag+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s was given an empty value — write %s COLS%sROWS",
				chatGeometryFlag, chatGeometryFlag, config.ChatIdealGeometryDimSep)
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a value — write %s COLS%sROWS",
			chatGeometryFlag, chatGeometryFlag, config.ChatIdealGeometryDimSep)
	}
	return next, true, nil
}

// chatParseResumeArg parses the resume switch: the bare flag, its inline value
// (--resume=<id>) and its spaced value (--resume <id>). consumed=false means
// the word is not this flag at all (the caller keeps walking its vocabulary).
//
// A missing or empty value is a USAGE error naming the flag: "--resume" alone
// names no session, and swallowing it would send the run to the ordinary
// empty-chat path while the operator believes they resumed something (Rule 15).
// chatParseSurfaceTurnsArg parses this verb's spelling of the self-driven turn
// budget knob: the bare flag, its inline value (--chat-surface-turns=30) and its
// spaced value.
//
// A missing or empty value is a USAGE error naming the flag — never swallowed,
// because a bare `--chat-surface-turns` would otherwise leave the run at the
// built-in bound while the operator believes they raised it (Rule 15). The VALUE
// itself is validated by the ONE resolver (config.ParseChatSurfaceTurns), which
// REPORTS an unusable value and falls through to the next channel: a typo must
// not make `gopi chat` unusable.
func chatParseSurfaceTurnsArg(a, next string, hasNext bool) (value string, consumed bool, err error) {
	if a != chatSurfaceTurnsFlag && !strings.HasPrefix(a, chatSurfaceTurnsFlag+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s was given an empty value — write %s N",
				chatSurfaceTurnsFlag, chatSurfaceTurnsFlag)
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a value — write %s N",
			chatSurfaceTurnsFlag, chatSurfaceTurnsFlag)
	}
	return next, true, nil
}

func chatParseResumeArg(a, next string, hasNext bool) (target string, consumed bool, err error) {
	if a != chatFlagResume && !strings.HasPrefix(a, chatFlagResume+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s was given an empty target — write %s SESSION (a session id or a raw-log path)",
				chatFlagResume, chatFlagResume)
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a target — write %s SESSION (a session id or a raw-log path)",
			chatFlagResume, chatFlagResume)
	}
	return next, true, nil
}

// chatRecordableFlags is the argv this run RECORDS for a later resume
// (sessionevent.EventTypeResumeOptions): what the operator stated, MERGED with
// what the record restored, de-duplicated, order preserved.
//
// WHY THE INHERITED OPTIONS ARE RE-RECORDED — the CHAIN. A resumed run mints
// its OWN session (buildAgent -> NewAgent -> a fresh uuid), so the record a
// LATER resume reads is the one written HERE. Recording only the explicit
// words would break resume at the SECOND crash: `chat --tools` killed, then
// `chat --resume <id>` (tools restored, but nothing recorded), then killed
// again, and `chat --resume <id2>` restores nothing — the capability silently
// gone one hop later. What this run records is therefore what this run HAD.
//
// THE SEED IS NEVER RECORDED, in either spelling. It names WHICH session, and
// the record already lives inside that session's own file: a session id in the
// flags would make a later resume re-seed itself from a conversation it is
// already continuing, and a raw-log path would pin a filesystem fact of THIS
// run onto a run that may not have it. The options are what a record carries;
// the target is the file it is written into.
func chatRecordableFlags(collected, inherited []string) []string {
	all := make([]string, 0, len(collected)+len(inherited))
	all = append(all, collected...)
	all = append(all, inherited...)
	out := make([]string, 0, len(all))
	seen := make(map[string]bool, len(all))
	for _, f := range all {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// chatREPLSwitch applies one of this verb's own switches and reports whether the
// word was one of them (the REPL's half of the closed vocabulary above).
// The resume flag is handled with the valued flags (it carries a session id),
// not here.
func chatREPLSwitch(a string, sess *chatSession, noEditor, noColor, verbose *bool) bool {
	switch a {
	case chatFlagTools:
		sess.toolsEnabled = true
	case chatFlagNoEditor:
		*noEditor = true
	case chatFlagNoNotify, chatFlagNoEvents:
		sess.noEventSlotWord = a
	case chatFlagNoColor:
		*noColor = true
	case chatFlagVerbose, chatFlagVerboseShort:
		*verbose = true
	default:
		return false
	}
	return true
}

// chatRefuseUsage reports a USAGE error (the command line itself is wrong) with
// the documented status and this verb's page, returning the status for the
// caller to hand to os.Exit. ONE rendering site, so the two usage paths — an
// unknown flag and a malformed flag value — cannot disagree about the status.
func chatRefuseUsage(err error) int {
	fmt.Fprintf(os.Stderr, chatErrorPrefix+"%v\n\n%s", err, chatUsage)
	return chatExitUsage
}

// chatRefuseUnknownToken refuses an argv word this verb does not recognize: it
// names the word AND its kind (flag vs positional argument), lists the accepted
// flag vocabulary and the usage line, and returns the usage status. Nothing else
// is attempted — no REPL, no provider call, no session artifact.
//
// The kind matters: `gopi chat --bogus-flag` used to reach the positional seed
// branch, where the word was hunted for as a raw-log path / session id and the
// run died with a "resume target" message describing a positional the caller
// never meant to write. A `-`-prefixed word is a FLAG with no matching
// definition; saying so is the honest reading of the command line.
func chatRefuseUnknownToken(a string) int {
	kind := chatWordArgument
	if strings.HasPrefix(a, chatFlagPrefix) {
		kind = chatWordFlag
	}
	// A word that IS a gopi flag, just not one this verb parses, is told so — with
	// the channel that does work. "unknown flag --film-terminal" would be false in
	// its first word; the top-level parser accepts it (measured: `gopi
	// --film-terminal xterm version` is fine), so the honest reading is "this verb
	// does not take it BEFORE the subcommand" and the fix is `gopi --film-terminal
	// NAME chat …`.
	if kind == chatWordFlag {
		if env, takesValue, ok := config.AgentFlagToken(a); ok {
			// The SHAPE of the working spelling follows the flag's own arity: a
			// scalar row needs its VALUE, a boolean row must not be given one
			// (measured 2026-10-27: the hint told a boolean-knob caller to write
			// `--memory-prefetch VALUE`, which would have made VALUE the value).
			form := "`gopi " + a + " VALUE chat …`"
			if !takesValue {
				form = "`gopi " + a + " chat …`"
			}
			fmt.Fprintf(os.Stderr, "%s%s is a gopi flag, but this verb does not accept it here — it belongs BEFORE the subcommand: %s%s\n%s\n\n%s",
				chatErrorPrefix, a, form, chatTopLevelFlagHint(env, takesValue), chatUsageLine, chatUsage)
			return chatExitUsage
		}
	}
	fmt.Fprintf(os.Stderr, "%sunknown %s %s — this verb's flags are: %s\n%s\n\n%s",
		chatErrorPrefix, kind, a, strings.Join(chatREPLFlags(), chatFlagListSep), chatUsageLine, chatUsage)
	return chatExitUsage
}

// chatTopLevelFlagHint names the env channel of a flag that must be written
// before the subcommand, when the flag has one: the operator who typed the flag
// in the wrong position is one line away from a working command, and naming the
// env var gives them the other working form.
func chatTopLevelFlagHint(env string, takesValue bool) string {
	if env == "" {
		return ""
	}
	if !takesValue {
		return " (or export " + env + "=1)"
	}
	return " (or export " + env + "=VALUE)"
}

// isChatSubcommand reports whether argv begins with the `chat` subcommand.
// When it does it runs the subcommand and returns true (the caller must not
// treat the remaining args as an input file). Checked alongside the other
// tool subcommands so `gopi chat -h` prints chat usage, not top-level usage.
func isChatSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == CliVerbChat {
		if rest, ok := chatIndexArgs(args); ok {
			os.Exit(runChatBackfill(rest))
		}
		// ONE PROMPT IN, ONE TURN OUT: `--once` (and its -p/--prompt spellings)
		// is dispatched BEFORE the REPL so the prompt can arrive on the command
		// line or on stdin. It runs the SAME REPL — see chat_once.go for why
		// that is the whole design.
		if isChatOnceRequested(args[1:]) {
			os.Exit(runChatOnce(args[1:]))
		}
		os.Exit(runChat(args[1:]))
	}
	return false
}

// chatIndexArgs resolves the `gopi chat backfill ...` argv tail: it reports
// whether args is the chat-index dispatch shape and returns the argv FOR
// runChatBackfill — WITH the verb included, because the runner branches on that
// verb. Slicing it off here (the original args[2:] dispatch) made
// `gopi chat backfill` unreachable — the verb-less tail fell into the list
// branch and "No chats found" printed instead of the migration report. THAT
// list branch is now GONE (epic verb-restructure, 2026-10-05): listing the
// index is `gopi session list`, one door.
//
// `chat list` IS RESOLVED HERE TOO, and that is deliberate — but it does NOT
// dispatch (epic verb-restructure, 2026-10-05). The verb was gopi's second door
// onto the chat index; the ONE door is `gopi session list` (the same
// tools.ChatList renderer, plus --mode/--source which `chat list` never had).
// It is caught in this resolver — rather than deleted from it — because the
// word must be REFUSED BY NAME with both the retirement and the new home: an
// argv that fell out of this function would be handed to the REPL, where
// `gopi chat list` would open a chat session whose first message is the word
// "list". The refusal is the same shape `gopi session checkpoint resume` gets. And a
// USAGE QUERY IS NOT A RUN: `gopi chat list -h` prints the pointer and exits 0,
// so a help call is never answered with a failure (the P0-1 rule the retired
// `task run -h` case states).
func chatIndexArgs(args []string) ([]string, bool) {
	if len(args) < 2 {
		return nil, false
	}
	switch args[1] {
	case chatSubcommandBackfill:
		return args[1:], true
	case chatSubcommandList, chatSubcommandKeeps:
		os.Exit(refuseChatIndexList(args[1], args[2:]))
	}
	return nil, false
}

// refuseChatIndexList answers the retired `gopi chat list` / `gopi chat keeps`
// spelling: the -h half (a usage query, exit 0, the pointer) and the run half
// (exit non-zero, the same pointer on stderr, NOTHING executed — no query, no
// table, no REPL, no session). word is the spelling the operator typed, so the
// refusal names the word they used rather than a canonical one.
func refuseChatIndexList(word string, rest []string) int {
	for _, a := range rest {
		if a == HelpFlag || a == HelpFlagLong {
			fmt.Printf(chatListRetiredPointer, word)
			return chatExitOK
		}
	}
	fmt.Fprintf(os.Stderr, chatListRetiredPointer, word)
	return chatExitFailed
}

// chatListRetiredPointer is the ONE statement of the retirement (Rule 1b/13:
// the -h half, the run half and the test all read THIS string, so the answer
// cannot differ between the two argv shapes). Its %s is the word typed.
const chatListRetiredPointer = "gopi chat %s: retired — the chat index has ONE door: `gopi session list` " +
	"(the same rows and ordering, plus --mode chat|web, --source and --limit; `gopi session -h`)\n"

// chatIndexSubverbs is the `gopi chat <verb>` index vocabulary. `backfill` is
// the LIVE word (chatIndexArgs hands its argv to runChatBackfill); `list` and
// `keeps` are the RETIRED pair (both refused by name, each for its own measured
// reason — see chatListRetiredPointer and the note on chatSubcommandKeeps).
// Named constants (Rule 1b): the words are compared in chatIndexArgs and
// spelled out in chatListUsage.
const (
	chatSubcommandList     = "list"
	chatSubcommandBackfill = "backfill"
	chatSubcommandKeeps    = "keeps"
)

// chatListUsage is the help tail for the chat index subcommand that is still
// reachable: `gopi chat backfill`. The LISTING half is `gopi session list`
// (epic verb-restructure, 2026-10-05): this page must not advertise a word the
// dispatcher refuses, so the bullet that used to teach `gopi chat list` now
// names the live door (Rule 31: a page that promises a command the dispatcher
// answers with a refusal is the help lying).
const chatListUsage = `Chat index subcommands (the chats TABLE, not the tasks ledger):

  gopi chat backfill [--dry-run] [--verbose] [--repair-first-msg]
      Migrate the tasks ledger's chat-origin [auto] rows into the chats
      table (facts re-derived from each session JSONL: first user message,
      turn count, token sums). Converted ledger rows are marked
      " — chat: backfilled" and skipped on re-runs (append-only — nothing
      deleted from the tasks ledger).

      --repair-first-msg re-derives the index's head column (` + "`first_msg`" + `) from
      the authoritative session logs, for rows an INJECTED turn titled — a
      gopi notify delivery, a background-job notice, a model-typed line. The
      head is the OPERATOR's first line, or the explicit placeholder
      "(no operator line yet)" when no operator line exists yet; a row whose log
      this machine does not hold is reported (unreadable-log) and left alone,
      never blanked. Idempotent, one line per scanned row, --dry-run reports
      without writing. NOTHING IS DISCARDED: the injected text stays in the
      session JSONL, which is the record this table indexes.

  LISTING the chat index is ` + "`gopi session list`" + ` (retired here — one door,
  ` + "`gopi session -h`" + `), which prints the same rows plus --mode/--source.
`

// runChatBackfill implements `gopi chat backfill`: the ledger→chats-table
// migration. The LISTING sibling (`gopi chat list`) is retired — the live door
// to the index is `gopi session list`, whose renderer is the session family's;
// this function is reached only with the `backfill` verb, which chatIndexArgs
// hands over WITH the verb included.
func runChatBackfill(args []string) int {
	dbPath := ""
	dryRun := false
	verbose := false
	repairFirstMsg := false
	// The verb WORD is included in argv on purpose (chatIndexArgs hands the
	// tail over with it, which is what made the old dispatch shape reachable at
	// all); it is consumed here, and a leading word that is not this verb is
	// refused rather than ignored — a refusal this subcommand can give, since
	// it has no positional of its own.
	if len(args) > 0 && args[0] == chatSubcommandBackfill {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := ""
		if strings.HasPrefix(a, "--") {
			if eq := strings.Index(a, "="); eq >= 0 {
				val = a[eq+1:]
				a = a[:eq]
			}
		}
		switch a {
		case HelpFlag, HelpFlagLong:
			// A USAGE QUERY IS NOT A RUN (the P0-1 rule): --db is deliberately
			// NOT exported before this arm, so `-h` prints the page having
			// changed nothing in the environment.
			fmt.Print(chatListUsage)
			return chatExitOK
		case "--db":
			dbPath = nextVal(args, &i, val, "--db")
		case "--dry-run":
			dryRun = true
		case "--verbose", "-v":
			verbose = true
		case "--repair-first-msg":
			repairFirstMsg = true
		default:
			// The vocabulary is CLOSED: backfill takes a mode review pair and
			// --db, nothing else. An unrecognised `-`-word is refused as a flag
			// (one line, the verb's own status), never swallowed as a
			// positional — this subcommand has no positional at all, and a
			// silently ignored argument on a MIGRATION would be read as "it
			// ran".
			fmt.Fprintf(os.Stderr, "gopi chat %s: unknown %s %s\n%s", chatSubcommandBackfill, chatWordFlag, a, chatListUsage)
			return chatExitUsage
		}
	}
	// The --db override is exported ONCE, here, after the parse and before the
	// command — it is the syntax's common flag, and exporting it inside a
	// branch made `gopi chat backfill --db PATH` accept the flag and then read
	// the default database (a silently ignored flag, found by the G5
	// help-surface probe).
	if dbPath != "" {
		os.Setenv("GOPI_CHAT_DB", dbPath)
	}
	stats, report, err := tools.ChatBackfill(tools.ChatBackfillOptions{DryRun: dryRun, Verbose: verbose})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi chat %s error: %v\n", chatSubcommandBackfill, err)
		return chatExitFailed
	}
	fmt.Print(report)
	_ = stats
	if repairFirstMsg {
		if code := runChatRepairFirstMsg(dryRun); code != chatExitOK {
			return code
		}
	}
	return chatExitOK
}

// runChatRepairFirstMsg re-derives the chats index's head column from the
// authoritative session logs — the repair for rows titled by an injected turn
// (internal/tools/chat_first_msg_repair.go carries the reasoning).
//
// IT IS A FLAG ON `backfill`, NOT A NEW VERB, because backfill already owns
// exactly this question ("make the chats index agree with the session logs") and
// Rule 35 warns against adding a second door to the same room. It is also
// IDEMPOTENT and reports one line per row it scanned, so a second run is
// evidence rather than a no-op nobody can see.
func runChatRepairFirstMsg(dryRun bool) int {
	repairs, err := tools.ChatRepairFirstMsgs(dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi chat %s --repair-first-msg error: %v\n", chatSubcommandBackfill, err)
		return chatExitFailed
	}
	repaired, kept, unreadable, unchanged := 0, 0, 0, 0
	for _, r := range repairs {
		switch {
		case r.Reason == tools.ChatFirstMsgRepairKept:
			kept++
		case r.Reason == tools.ChatFirstMsgRepairNothingToChange:
			unchanged++
		case strings.HasPrefix(r.Reason, tools.ChatFirstMsgRepairNoLogPrefix):
			unreadable++
			fmt.Printf("  keep %s (%s)\n", r.SessionID, r.Reason)
		case r.Reason != "":
			fmt.Printf("  skip %s (%s)\n", r.SessionID, r.Reason)
		default:
			repaired++
			fmt.Printf("  %s %s: %q -> %q\n", chatRepairVerbWord, r.SessionID, r.Before, r.After)
		}
	}
	fmt.Printf("chat first-msg repair: repaired=%d kept=%d unchanged=%d unreadable-log=%d dryrun=%v\n",
		repaired, kept, unchanged, unreadable, dryRun)
	return chatExitOK
}

// chatRepairVerbWord is the per-row verb the repair report prints, named so the
// report and any reader of it agree on one spelling (Rule 1).
const chatRepairVerbWord = "repair"

// nextVal resolves a flag's value: inline (--x=v) or the next argv slot.
func nextVal(args []string, i *int, inline, flag string) string {
	if inline != "" {
		return inline
	}
	if *i+1 < len(args) {
		*i++
		return args[*i]
	}
	fmt.Fprintf(os.Stderr, chatErrorPrefix+"%s needs a value\n", flag)
	os.Exit(chatExitUsage)
	return ""
}

// atoiOr parses n or returns fallback (best-effort CLI parsing).
func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

// chatSession tracks the accumulated conversation across chat turns. After
// each turn the run's in-memory message history is captured so the next turn
// resumes in the same context (free-form — no [EPIC GOAL] re-framing).
type chatSession struct {
	// history is the accumulated []deepseek.Message passed as the seed of
	// each turn's Chat/ChatOps call. Captured from the previous turn's agent
	// context after the run returns.
	history []deepseek.Message

	// continuedFrom records the optional initial session checkpoint seed
	// (raw log path / session-id) for observability.
	//
	// IT IS A CARRIER, NOT A DEAD FIELD (epic: `gopi chat --resume <id>` minted
	// a second session). The session id this run continues is read off the
	// record that carries this raw log, and it is what the agent is built with;
	// the raw log path beside it is the file the agent APPENDS to. Nothing else
	// in the run needs to re-derive either.
	continuedFrom string

	// turnBaseline is how many turns the SESSION ALREADY HOLDS when this run
	// starts — the number gopilog.SessionTurns reports for the session this run
	// continues, and 0 for a fresh one.
	//
	// IT IS A DISPLAY FACT AND NOTHING ELSE (measured defect, task 01a16fb3). A
	// resumed chat re-numbered its turns from 1 while `gopi session log --turns`
	// said the session already held 19, so the operator read "turn 1" for what
	// the log calls turn 20. The counter that says "turn N" and the counter the
	// close-out writes as `session_end` total_iterations are TWO facts, and until
	// this field existed they were ONE variable (the REPL's iterCount): a turn is
	// the operator's own row and routinely takes several loop passes, which is
	// exactly what total_iterations counts. Seeding iterCount with a baseline
	// would have made every resumed session report 19 extra passes. So the
	// baseline lives HERE, is added at the two DISPLAY seams (the body heading and
	// the status row) and is never added to iterCount.
	turnBaseline int

	// turnWriter is the session record's WRITER, and it is here for the same
	// reason webServer holds one: the only authority for a turn ordinal is the
	// record, and the record's tail is still in the writer's buffer at the
	// instant a band is drawn, so the number has to be asked of the writer
	// rather than re-derived from the file — or, worse, counted by this REPL.
	//
	// It is an INTERFACE, not *agent.SessionLogger, so the numbering rule can be
	// driven without a live record (chat_turn_display_writer_test.go). A run
	// whose record was refused (nil logger) leaves it nil, and the numbering
	// then falls back to the REPL's own count — the pre-existing behaviour,
	// stated here rather than inherited by accident. See chatTurnDisplay.
	turnWriter interface{ TurnsRecorded() int }

	// costBase is the cache pair the CONTINUED session had already accumulated
	// when this run started, folded once from its session record (Rule 14: the
	// same two sums `gopi session stats` prints — see cli_chat_cache_base.go).
	//
	// IT EXISTS SO THE ROW'S Σ MEANS WHAT IT SAYS (task: the Σ reading was this
	// RUN's counters, which restart at zero, so a resumed session read 81.5% on
	// the row against 86.03% in `gopi session stats`). Like turnBaseline it is a
	// DISPLAY fact — added at the read-out (statusOpsFor) and NEVER written into
	// ag.Cost, because that tracker's running totals are what the chat path logs
	// onto every `request` row and a segment seeded with the previous segments'
	// total would be counted twice by internal/requestusage.
	//
	// Zero and unknown for a fresh run, which is exactly this run's own counters.
	costBase chatCostBase

	// chatRegistered kept for struct-shape parity with webServer; the latch
	// now lives on the agent (Agent.chatRegistered, chatregister.go — M4.6
	// 钩子分流: chats go to the CHATS table, not the tasks ledger).
	chatRegistered bool

	// toolsEnabled selects the tool-enabled turn runner: true → Agent.ChatOps
	// (full toolset ReAct rounds, claude-code-style operations), false →
	// Agent.Chat (lightweight Q&A, no tools). Flipped by the --tools flag
	// and the /tools slash command.
	toolsEnabled bool

	// noEventSlotWord records `--no-notify` / `--no-events`: the WORD this run
	// was started with, or "" when it subscribes (the default). The word itself
	// is kept rather than a bool because the ARM POINT's refusal NAMES what was
	// typed — "declines the chat event slot" without saying which spelling took
	// effect is the silent form of the switch this epic exists to make
	// observable (Rule 15).
	//
	// THE ZERO VALUE IS "SUBSCRIBE" ON PURPOSE: the default is the capability,
	// and the flag is the documented way out of it. It is read at the ARM POINT
	// (chat_session_slots.go), which is the one call that performs the write —
	// so a run that declines is not a run whose slot was written and then
	// deleted, it is a run that never wrote one.
	noEventSlotWord string

	// unattended is whether this run carries the UNATTENDED context — the
	// sentence telling the model that no human will ever answer it, so it must
	// choose the conventions itself rather than stop and ask
	// (chat_unattended.go; agent.Agent.Unattended is what the prompt assemblies
	// read). It is decided by the FACE this run is invoked on: the one-shot
	// carrier (`gopi chat --once`, whose answer goes to a script or an agent)
	// sets it by default and declines it with --answer-alone, while an
	// interactive console leaves it false — a human IS reading that one.
	unattended bool

	// ground is the session's WRITE-LANDING REPORT (epic
	// chat-session-ground-retired-when-not-a-row): a chat owns no worktree and
	// no branch — the creation path was retired — so all this carries is the
	// directory the session writes in and whether it is a repository's main
	// checkout. It is PRINTED at start-up, not remembered for the close-out:
	// there is nothing to leave behind, so there is no exit notice any more.
	ground tools.ChatGroundResolution

	// sessionDir is THIS run's session directory — the one beside
	// session.jsonl, resolved once the agent exists. It is where the message
	// boundary spills an over-threshold input (chat_boundary.go), so the bytes
	// live and are swept with the session they belong to. Empty means the
	// session has no directory and the boundary has nowhere to bill the bytes
	// (see chatSealMessage: it refuses the turn rather than forward them raw).
	sessionDir string

	// pasteStore is sessionDir's paste store, built lazily by spillStore so a
	// session that never crosses the boundary creates no directory at all.
	pasteStore *lineedit.PasteStore

	// sgr is THIS run's raw screen recording (chat_sgr_stream.go), or nil when the
	// run declined it (`--no-screen-stream`) or could not open it. `/screen` reads it
	// and says which state it found — the pointer is the ONE handle to the live
	// stream, so the command cannot resolve a second, possibly-different path
	// (chat_screen.go).
	sgr *uiclient.SgrStream

	// screenPending is the ONE-SHOT note a `/screen` capture parks for the NEXT
	// turn's message, and it is CLEARED by the read (chatScreenTakePending) so one
	// capture is described to the model exactly once. It holds a reference (path,
	// byte count, how to read it) and never SGR bytes.
	screenPending string

	// beats is this run's BEAT PROTOCOL driver, non-nil exactly when the run was
	// given a scratch (chat_replay_beats.go). Nil-safe everywhere it is called:
	// an ordinary chat run has no beats and every seam must stay unchanged.
	beats *chatReplayBeatDriver

	// turnRunning is true exactly while a model turn is in flight, and
	// turnClaimed latches the turn the operator abandoned. Together they are the
	// REPL's answer to "what does Ctrl+C mean right now" (see claimTurnInterrupt):
	// the claim is one-per-turn, so the several transports a single key can take
	// (the raw 0x03 byte AND a real SIGINT, when the terminal's ISIG is on) can
	// never cancel the same turn twice — the second would land on the NEXT turn's
	// context and abandon something the operator never saw.
	//
	// Both are written by the REPL's own goroutine and read by the input layer's
	// read loop / signal handler, hence the mutex.
	turnMu      sync.Mutex
	turnRunning bool
	turnClaimed bool

	// replay is this run's replay driver, non-nil exactly when `--replay` named a
	// session (chat_replay.go). It lives on the session so the ONE closeout every
	// exit path shares (finishChat) can JOIN it and report what it served —
	// putting the join anywhere else would make one exit path report a replay and
	// another stay silent about it.
	replay *chatReplay

	// witness is this run's TURN-CLOSE COUNTER (chat_replay_witness.go): the
	// REPL bumps it at the one seam where a turn's screen is final, and the
	// replay driver reads it to decide whether a prefix turn is behind it.
	// Nil-safe — a session that never sets one reports 0 closed turns, which
	// reads as "no witness" and leaves the plain settle in place.
	witness *chatTurnWitness

	// turnClaimedOrigin records WHICH input claimed the turn (a lone Escape, the
	// Ctrl+C byte, or a SIGINT), so the marker this REPL prints and the reason the
	// session records name the same key. It is cleared with the latch it belongs
	// to (endTurn), because a turn claimed by Escape must not label the NEXT
	// turn's interrupt.
	turnClaimedOrigin lineedit.KeyOrigin

	// queue is THIS session's own prompt queue (`/queue`): prompts the operator
	// parked while a turn was in flight, waiting to become the next turns. It is
	// the OPERATOR's seam, deliberately separate from the agent's one-slot
	// self-driven seam the model types into (chat_queue.go's header states the
	// division); the two are consumed by ONE loop in runChatWithInput, with the
	// agent's line taken first.
	queue chatQueue

	// exitMark is THIS run's death certificate (chat_exit_marker.go): armed
	// before the REPL takes the terminal and advanced by the ONE closeout every
	// exit path shares. Nil for a run with no session directory (`--replay`),
	// and every method on it is nil-safe.
	exitMark *chatExitMark
}

// chatTurnDisplay is the ONE derivation of "which turn is this, as a reader
// counts turns": the session's own baseline (how many turns the continued
// session already held) plus this run's own local count.
//
// `local` is operatorTurns — the count of THIS run's turns that the session log
// will open (chatCountsAsOperatorTurn), NOT iterCount: iterCount is the
// close-out's loop-pass count and folding it in here is the two-quantities-summed
// defect this epic removes.
//
// WHAT THE NUMBER MEANS, STATED HERE BECAUSE THE SCREEN SHOWS IT UNDER THE BARE
// WORD `turn` AND A READER MUST BE ABLE TO SETTLE THE QUESTION ONCE (task
// 01a17a37's item (b); measured on session 4a0957a3-bb4e-4337-a761-20606c87d1fd,
// where the same session was called 29, 29 and 30 by three surfaces):
//
//	THE STATUS ROW / INFO BAR prints chatTurnDisplay(operatorTurns) = the number
//	of turns this session HOLDS on completion — the count the session's own
//	TURN-OPENING ROWS add up to. At the idle prompt before this run's first turn
//	it therefore prints the session's baseline itself (29 on the session above),
//	which is the number of turn-opening rows in the log and the number the resume
//	notice calls "this session already holds".
//
//	THE BODY HEADING prints chatTurnDisplay(operatorTurns)+1 when the turn about
//	to run opens a turn (see the caller) — the ORDINAL of that turn. It is the
//	number the resume notice names as "the next turn is turn 30", and it is one
//	MORE than the bar shows at the idle prompt before the turn runs.
//
// The two are not two quantities: they are the same sequence read at two
// positions (how many are behind you / which one you are on), and the bar
// advances to the ordinal the moment the turn completes. What they are NOT is
// interchangeable, so a caller must never pass this function a number of a
// different kind — and a surface that prints the bare word `turn` must print one
// of these two, never a third thing.
//
// THE ORDINAL IS THE LOG'S, NO LONGER THE LEDGER'S, AND THE DIFFERENCE IS THE
// WHOLE DEFECT (measured 2026-11-04 on session
// e1e22e9f-eee0-4c33-a238-0fca04534ad9, all four surfaces read at one instant):
//
//	session show <id>          turns 619    ← chats.turn_count (COMPLETED turns)
//	session log <id> --turns   rows to 621  ← the log's turn-opening rows
//	session replay --turns     turns=621    ← the same rows
//	the screen's heading       620          ← the ledger's 619 + this run
//
// so the number the operator COPIES off the screen (`turn 605`) selected a
// DIFFERENT turn in `replay --turn 605` — the epic's own reproduction, and not
// noise: 16 of 16 readable banners sat exactly ONE below the row carrying the
// same prompt. `chats.turn_count` counts turns that FINISHED (RecordChatTurnOpened
// registers nothing for an interrupted or failed turn) while an ordinal is a
// POSITION, and a position cannot have a hole; anchoring one on the other is a
// category error (Rule 14). turn_ordinal_baseline.go carries the measurement, the
// superseded 2026-10-27 ruling, and why that ruling's premise (the log counter
// was the person-only subset) no longer holds.
//
// SO A DISAGREEMENT IS NOW EXPECTED IN EXACTLY ONE PLACE, AND IT IS STATED
// RATHER THAN PAPERED OVER: `gopi session show <id>`'s `turns` line stays the
// LEDGER's COMPLETED-turn count — a different quantity, whose own provenance
// sentence says so (gopilog.TurnCountSource). The numbers this function prints
// are the ones the reader's ROW NUMBERS use, which is the question an operator
// asks when copying a number into `--turn N`.
//
// AND THE CORRECTION OUTRANKS THE OLDER COMMENT: drift is not one-directional.
// A ledger-anchored baseline drifted LOW (the measured case above: 619 against
// 621 rows), while a display that advances on a row the log opens NO turn at
// drifts HIGH (the 2026-10-27 case: ledger 18, reader 11). Both directions were
// measured; chatOpensSessionTurn exists so the second cannot recur from this
// side, and the anchor exists so the first cannot.
//
// IT EXISTS SO THE ADD IS NOT SPELLED AT SIX CALL SITES: the body heading, the
// status row's begin and its three publishes all need the same number, and a
// bare `+ sess.turnBaseline` in each of them is six chances to forget one — a
// screen numbered by halves is the defect this task measured.
//
// THE WRITE SIDE NEVER CALLS IT. `session_end` total_iterations is
// iterCount alone: a TURN is the operator's own row and routinely takes several
// loop passes (the unit total_iterations counts), so folding a turn baseline
// into it would report fabricated passes for every resumed session.
//
// IT ADDS WHAT THE WRITER SAW, NOT WHAT THIS REPL COUNTED (measured 2026-11-08,
// session e68fddf5-32a9-4e6d-b06f-13a101295883). `local` is the REPL's OWN
// counter (operatorTurns), and the record's turn axis is a POSITION in its
// turn-opening rows. The REPL is NOT the only writer of that record: internal/
// agent writes turn-opening rows of its own — a `gopi notify` delivery, a job
// notice opened to report one, a boundary carrier — and every such row advances
// the log while operatorTurns never hears about it. Measured: the screen
// numbered a turn 169 for the prompt the reader gave 168, and every band after
// that stayed exactly one ahead (before it, band 76 == row 76). A permanent
// one-turn offset is not cosmetic: the number the operator copies into
// `session replay --turn N` selects a different turn.
//
// The fix is the one `gopi web` already uses (webTurnOrdinal): ask the WRITER,
// and consult the REPL's own tally only when there is no writer to ask. A
// "take the larger of the two" rule would NOT have worked here: the measured
// offset ran with the SCREEN HIGH (band 169 for the prompt the record calls
// 168), so a max() would have preserved the defect instead of removing it. The
// writer's count is the record's own axis, so no mix of writers can move it.
// `SessionLogger.TurnsRecorded` counts the turn-opening rows THIS run has
// written under the SAME predicate AND THE SAME FACE the reader uses, so a
// foreign row is inside the number by construction rather than missing from it.
//
// THE FACE IS THE WHOLE FIX (measured 2026-11-17, session
// 3965695b-6f70-4067-8b61-68628d7a6dfc, 358 user rows): this called
// `TurnsSoFar`, which judges every row by its TEXT (`TurnCounterFace`) because
// that is the clock the CONTEXT FOLD needs — the fold reads this session's
// message array, which carries no provenance. The reader judges by the recorded
// FACE. The two answers were 9 and 13, and the gap was exactly the 4 rows
// recorded `source=notify`: each is a delivery the record counts as one complete
// input, and a text-face judge cannot see it (its sentence starts `[system] `).
// So the screen was one turn behind from the first delivery onward and four
// behind by the end — `--turn N` with the number copied off the screen exported
// a turn that had already finished.
//
// SO THERE ARE TWO NAMED COUNTS AND NO ARITHMETIC BETWEEN THEM: `TurnsSoFar`
// feeds the note anchor (noteTurnNumber, selfrecord_noteindex.go — where counting
// rows the anchor has no window for is task 01a1db5b's measured "turn 46 does not
// exist" defect), and `TurnsRecorded` feeds every surface whose number a reader will
// copy into `--turn N`. A caller never adds, subtracts or guesses which one it
// holds.
//
// `local` therefore survives only for a run whose record was refused (a nil
// logger under replaymode); it is never consulted when a writer exists.
func (s *chatSession) chatTurnDisplay(local int) int {
	return s.turnBaseline + s.chatTurnsSinceBaseline(local)
}

// chatTurnOpenedAt is THE decision a turn band's clock is drawn from, and it is
// one function so the decision cannot be re-made somewhere else.
//
// A LIVE LINE TAKES THE CLOCK. That is not a fallback: on a live run the REPL is
// the process that writes the turn's row, so `now` IS the turn-opening moment, and
// the same value is published to the agent (ag.TurnOpenedAt) so the screen and the
// record cannot disagree about one turn.
//
// A REPLAYED LINE TAKES THE RECORD'S MOMENT, INCLUDING WHEN THAT MOMENT IS ZERO.
// `line.replayStamped` — the fact that a replay driver queued a moment for this
// very read — is the discriminator, and the moment's zero-ness is NOT: a recorded
// turn whose row held no timestamp must reach the band as an ABSENCE so it prints
// sessionevent.TurnStampPlaceholder, while a line no driver typed (every line of
// an ordinary run, and a replay's trailing `/quit`) must keep the clock. Keying on
// `!moment.IsZero()` collapses those two facts and puts time.Now() back on an old
// record — the measured defect (2026-11-08: a replay of session 6b01dd46, opened
// 2026-11-07 12:11:17, painted `turn 1 · 2026-11-08 15:59:32`).
//
// `now` is a parameter rather than a call inside, so the rule is exercisable
// without a clock and a test cannot pass by agreeing with the wall. The two
// recording fields are passed rather than a lineRead because that struct is
// declared inside the turn loop (it is that loop's own vocabulary), and widening
// it to package scope to please a test would be the test dictating the shape.
func chatTurnOpenedAt(replayMoment time.Time, replayStamped bool, now time.Time) time.Time {
	if replayStamped {
		return replayMoment
	}
	return now
}

// chatTurnsSinceBaseline is the "how many turns has THIS run opened" half of
// chatTurnDisplay: the record writer's count — the session's own turn axis —
// and the REPL's own tally only when no writer is open.
func (s *chatSession) chatTurnsSinceBaseline(local int) int {
	if s.turnWriter == nil {
		return local
	}
	return s.turnWriter.TurnsRecorded()
}

// chatOpensSessionTurn answers the ONE question the turn counter poses, with the
// ONE predicate that answers it elsewhere: sessionevent.IsTurnOpeningRow over the
// row's TEXT and its recorded FACE.
//
// IT EXISTS SO THIS SURFACE CANNOT HAVE A SECOND RULE. The REPL used to decide
// with chatCountsAsOperatorTurn (a face whitelist) while the session reader
// decided with the text+face predicate, and the two disagreed on exactly the rows
// whose text says "the framework is talking" (measured 2026-10-27, session
// 80c07a7d-bb6b-48b4-be66-ea1867f403f2: ledger 18, reader 11). A display that
// advances on a row the log opens no turn at drifts above `session log --turns`
// for the rest of the run — the defect this epic removes, arriving from the
// display's side.
//
// The face whitelist is KEPT, as a second condition, and deliberately: an
// unnameable face is not a turn this REPL can claim (chatCountsAsOperatorTurn
// states why), so the two conditions are `the row opens a turn` AND `the face is
// one this surface counts`. Both are necessary; neither is a second definition of
// the unit.
func chatOpensSessionTurn(message, face string) bool {
	if !chatCountsAsOperatorTurn(face) {
		return false
	}
	return sessionevent.IsTurnOpeningRow(message, face)
}

// chatCountsAsOperatorTurn reports whether a turn opened by `source` is one the
// session log counts — i.e. whether the status row's number advances for it.
//
// IT IS NOW EVERY RECORDED FACE, AND THAT IS THE OPERATOR'S RULING (2026-10-27),
// NOT A LOOSENING. The old body returned true for Human and Queue only, on the
// reasoning that a `notify` row is spelled "[system] …" and the reader's
// predicate was "not a harness marker", so it opened no turn. That reasoning
// rested on the very text test this revision removes: the reader now decides by
// the row's recorded FACE (sessionevent.IsTurnOpeningRow), and under that rule a
// `gopi notify` delivery and a `chat_surface action=type` self-typed line are
// both COMPLETE INPUTS that open turns — the operator's words are 一次完整的输入
// / one complete input, whoever produced the text.
//
// SO THE STATUS ROW MUST ADVANCE FOR ALL FOUR: if it advanced only for the
// person's own lines it would drift BELOW the number `session log --turns` and
// `session list` print for the same session — the very two-quantities-summed
// defect (task 01a17492, 38 log turns added to 82 passes) in a new direction.
//
// AN UNKNOWN OR EMPTY FACE STILL COUNTS FOR NOTHING. That is not a claim about
// the row; it is the honest refusal to guess a face this process cannot name,
// and such a row is not one this REPL opens turns under (chatSealSource sets it
// from inputsource).
func chatCountsAsOperatorTurn(source string) bool {
	switch source {
	case inputsource.Human, inputsource.Queue, inputsource.Notify, inputsource.LLM:
		return true
	}
	return false
}

// runChat handles the `gopi chat` subcommand. It runs an interactive REPL:
// read a line from stdin → run a turn (tool-enabled ChatOps or plain Chat)
// → stream the model's text to stdout → capture the accumulated history →
// repeat. args is the argv tail AFTER "chat".
//
// Signal semantics (chat-once/bare-prompt SIGTERM zombie fix, cli_web bug003
// pattern): SIGINT/SIGTERM mid-run used to kill the process outright, so the
// agent's end-of-run defer (which writes the ONE session_end + closes the
// chat ledger row via finishChat) never ran — an in_progress zombie. Now a
// signal channel fires ag.Interrupt() (graceful ctx cancellation — an
// in-flight Chat/ChatOps turn unwinds through ITS session_end defer), which
// breaks the loop's blocking pending-input state and lands in the same
// finishChat closeout as /quit. A single-session chat must never leak its
// session_end on Ctrl+C / kill.
func runChat(args []string) int { return runChatWithInput(args, nil) }

// chatReplSlashVocabulary is the /help <word> answer the REPL gives for a word
// in its OWN vocabulary that is not a shared-table entry — the reset word
// (webClearCommandName, which this switch accepts beside /clear) is the only
// such word today. Returning the shared entry's usage block keeps ONE
// definition of that text (never a second copy here).
func chatReplSlashVocabulary(word string) (string, bool) {
	if word == chatQueueCommandName {
		// A REPL-native word whose usage block lives with its own vocabulary
		// (chat_queue.go): the shared registry does not own it, so the answer
		// comes from that ONE definition rather than a second copy here.
		return chatQueueUsage, true
	}
	if word == chatInfoCommandName {
		// Same shape, same reason (chat_info.go): a REPL-native word whose usage
		// block is defined beside the block it documents.
		return chatInfoUsage, true
	}
	if word == chatScreenCommandName {
		// Same shape (chat_screen.go): the block documents WHICH turn a capture
		// holds and how a boundary is located, and both are that one file's
		// statements (the locator lives in internal/uiclient, where it is tested
		// against a real recording).
		return chatScreenUsage, true
	}
	if word == chatContextCommandName {
		// Same shape (chat_context.go): one renderer (internal/ctxdump) serves this
		// arm and `gopi session context`, and the usage block documents the two
		// facts that renderer enforces — the file gets everything, the screen gets
		// one line.
		return chatContextUsage, true
	}
	if word == chatVoiceCommandName {
		// Same shape (chat_voice.go): the usage block is built from the SAME
		// constants the recorder's window and the refusal sentences read, so the
		// help cannot promise a bound the loop does not keep.
		return voiceChatUsage, true
	}
	if word != webClearCommandName {
		return "", false
	}
	c, ok := lookupCommand(word)
	if !ok {
		// The registry owns this word (the vocabulary derives from it), so this
		// branch means the two disagree: report it unknown — a visible
		// disagreement, never a usage block invented here (Rule 15).
		return "", false
	}
	return c.Usage, true
}

// chatUnknownCommandError is the chat REPL's refusal: the shared renderer over
// the REPL's OWN vocabulary (chatReplCommandNames), so what the refusal names is
// exactly what the REPL's /help page prints — the words the switch in
// runChatWithInput answers. Kept as its own function so the vocabulary contract
// is testable without driving a terminal (Rule 31's unit half).
func chatUnknownCommandError(word string) error {
	return slashUnknownCommandError(word, chatReplCommandNames())
}

// chatQueueHandleLine answers ONE `/queue` line — the mutation, its refusal, and
// the visible note row every branch owes (the epic's visibility contract: the
// operator reads the queue change, never infers it).
//
// The seam check comes FIRST, before the argument is even read: on a run that
// cannot carry a queued turn there is no state this command may touch, so the
// refusal is the whole answer.
func chatQueueHandleLine(input *chatInput, sess *chatSession, msg string) {
	if refusal := chatQueueSeamRefusal(input); refusal != "" {
		input.WriteNotice(refusal)
		return
	}
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(msg), completionSlashPrefix+chatQueueCommandName))
	switch {
	case arg == "" || strings.EqualFold(arg, chatQueueListWord):
		chatQueueWriteList(input, &sess.queue)
	case strings.EqualFold(arg, chatQueueClearWord):
		if n := sess.queue.Clear(); n == 0 {
			input.WriteNotice(chatQueueClearEmptyNote)
		} else {
			input.WriteNotice(fmt.Sprintf(chatQueueClearedNote, n))
		}
	default:
		pos, ok, refusal := sess.queue.Add(arg)
		if !ok {
			input.WriteNotice(refusal)
			return
		}
		// The note carries the position AND the depth, so the operator needs no
		// memory of the previous note to know what is parked.
		input.WriteNotice(fmt.Sprintf(chatQueueAddedNote, pos, sess.queue.Len(), arg))
	}
}

// chatQueueWriteList renders the queue, or says plainly that it is empty — the
// branch an operator reads after a bare `/queue` on a session that has parked
// nothing. A silent bare `/queue` would be indistinguishable from a command that
// never ran.
func chatQueueWriteList(input *chatInput, q *chatQueue) {
	lines := chatQueueListLines(q)
	if len(lines) == 0 {
		input.WriteNotice(chatQueueEmptyNote)
		return
	}
	// WriteLines (not WriteNotice): this is the /help-panel family — a multi-line
	// block on the panel stream — while the notes above are one-line notices on
	// the REPL's own stream (lineedit_wire.go's two writers).
	input.WriteLines(strings.Join(lines, ""))
}

// chatQueueAnnounceDrop states what is about to happen to prompts still parked
// when the session ends. It is called from leaveChat, the ONE closeout every
// clean exit shares, so no future exit path can end a session without stating
// the queue's fate — the requirement is "stated, never silent", and one call
// site is how that survives the next edit.
func chatQueueAnnounceDrop(input *chatInput, q *chatQueue) {
	if n := q.Len(); n > 0 {
		input.WriteNotice(fmt.Sprintf(chatQueueQuitNote, n))
	}
}

// chatInterruptMarker is the line the operator sees when a turn ends because THEY
// ended it. It says three things and nothing else: the turn stopped by the
// operator's key, the partial answer above is real and INCOMPLETE, and the
// session is still there — because the alternative (silence) is what made the
// interrupted turn indistinguishable from a model that simply stopped typing.
//
// It is spelled once (Rule 1b) and carries the partial answer's own size, which
// is the one number that separates "the answer was cut here" from "that was the
// whole answer".
func chatInterruptMarker(partialRunes int) string {
	return chatInterruptMarkerFor(agent.InterruptReasonCtrlC, partialRunes)
}

// chatInterruptMarkerFor is the marker for a turn abandoned by a KNOWN input:
// the reason is the key the operator actually pressed (agent.InterruptReasonSet),
// never a fixed spelling. It exists because one cancel serves several keys — a
// lone Escape (the pure interrupt) and Ctrl+C reach the same turn — and a marker
// that always said "ctrl-c" would tell the operator about a key they did not
// press, in the one line that exists to explain what just happened.
func chatInterruptMarkerFor(reason string, partialRunes int) string {
	return fmt.Sprintf("%s%s (%s, %d chars shown) — the session is still open; type the next prompt, /quit to exit.",
		chatInterruptMarkerPrefix, reason, agent.InterruptSourceOperator, partialRunes)
}

// chatSignalCloseoutNotice announces the signal-driven closeout on the ONE
// writer that owns the screen.
//
// WHY IT EXISTS rather than the Fprintf it replaces: while the reserved layout
// is installed the cursor is parked on the status row, so a flowed stderr write
// lands ON that row instead of after it. Measured on the live chat c5f1189a
// (117x26, 2026-09-29): the notice overwrote the row's cells 0..57, so the
// operator read
//
//	deeps[gopi] terminated received — finalizing chat session…hit 97% ·↑8M↓1M …
//
// — the same tear chat_flow_seam.go and WriteNotice were built to remove, one
// call site that fix missed. The route is WriteNotice: it hands the row to the
// editor (the layout's only writer) and degrades to the historic stderr bytes
// when no layout is installed (a cooked run, --no-editor, a pipe), which is what
// keeps the testsuite's "finalizing" greps reading the same stream they always
// did.
//
// A nil input is legal and is the pre-layout window: the signal can arrive
// before the input layer exists, and the notice then takes the historic path
// rather than being dropped (Rule 15 — silence is the defect).
func chatSignalCloseoutNotice(input *chatInput, sig os.Signal) {
	notice := fmt.Sprintf(chatSignalCloseoutNoticeFormat, sig)
	if input == nil {
		fmt.Fprint(os.Stderr, notice)
		return
	}
	input.WriteNotice(notice)
}

// chatSignalCloseoutNoticeFormat is the closeout sentence, spelled once
// (Rule 1b): it is asserted by testsuite's SIGTERM guards through the
// "finalizing session" fragment, so the wording and the assertion have one
// definition between them.
const chatSignalCloseoutNoticeFormat = "\n[gopi] %v received — finalizing chat session…\n"

// chatInterruptReason maps the input layer's own origin vocabulary
// (lineedit.KeyOrigin — the key the decoder classified, or the signal the
// terminal delivered) onto the reason recorded in the session JSONL
// (agent.InterruptReasonSet). It is the ONE place the two vocabularies meet:
// keeping them separate means internal/lineedit does not have to know what a
// carrier records, and this function — not a string literal at each call site —
// is what a new origin is added to.
//
// An origin it does not know is reported as the honest default (ctrl-c) AND said
// on stderr, because a silent fallback would attribute the operator's key to the
// wrong one in the permanent record (Rule 15).
func chatInterruptReason(origin lineedit.KeyOrigin) string {
	switch origin {
	case lineedit.OriginEscape:
		return agent.InterruptReasonEscape
	case lineedit.OriginEOF:
		// Ctrl+D mid-turn: the operator asked for the SESSION to end, so the
		// record must not borrow Ctrl+C's word for it (task 01a1650f).
		return agent.InterruptReasonEOF
	case lineedit.OriginCtrlC, lineedit.OriginSignal:
		// The same key on two transports: a SIGINT is Ctrl+C delivered by the
		// tty's ISIG rather than as a byte, and the record names the KEY.
		return agent.InterruptReasonCtrlC
	}
	fmt.Fprintf(os.Stderr, "[gopi] unknown interrupt origin %q — recorded as %q\n", string(origin), agent.InterruptReasonCtrlC)
	return agent.InterruptReasonCtrlC
}

// chatInterruptMarkerPrefix begins the marker line. Named so the wording is one
// string and the tests can find it without restating a sentence.
const chatInterruptMarkerPrefix = "[gopi] turn interrupted by "

// claimTurnInterrupt is the ONE place an interrupt KEY is turned into a per-turn
// interrupt, whichever transport delivered it (the editor's 0x03 byte, a lone
// Escape, or a real SIGINT on a cooked input / a terminal whose ISIG is on). It
// reports whether it CLAIMED the key — true means a turn was really cancelled
// and the caller must keep going, false means nothing was in flight and the key
// keeps its historic meaning.
//
// origin is the input that carried the key (lineedit.KeyOrigin). It travels INTO
// the cancel so the session JSONL's turn_interrupted witness names the key the
// operator actually pressed: Escape is the pure interrupt, Ctrl+C at the prompt
// ends the session, and a reader who was not at the terminal cannot tell which
// happened from a partial answer.
//
// The single-claim latch is why this is not just a call to InterruptRun: the
// editor's key path and the signal path can BOTH fire for one keystroke on a
// terminal that has ISIG on, and a second cancel would land on the context of
// the turn that started after the first — silently abandoning a turn the
// operator never asked to stop.
func claimTurnInterrupt(ag *agent.Agent, sess *chatSession, origin lineedit.KeyOrigin) bool {
	sess.turnMu.Lock()
	defer sess.turnMu.Unlock()
	if !sess.turnRunning || sess.turnClaimed {
		return false
	}
	if ag == nil || !ag.InterruptRunBy(chatInterruptReason(origin)) {
		// No run context to cancel (the turn ended between the check and the
		// call): NOT a claim, so the key falls through to its prompt meaning
		// rather than being swallowed (Rule 15).
		return false
	}
	sess.turnClaimed = true
	sess.turnClaimedOrigin = origin
	return true
}

// beginTurn / endTurn bracket the window in which Ctrl+C means "stop this turn".
// They are the REPL's own statements about its state — never derived from the
// agent, because "the agent has a run context" is true for a moment on either
// side of the turn and the operator's key must not be classified by that margin.
func beginTurn(sess *chatSession) {
	sess.turnMu.Lock()
	sess.turnRunning = true
	sess.turnClaimed = false
	sess.turnClaimedOrigin = ""
	sess.turnMu.Unlock()
}

// endTurn closes the turn window and reports the ORIGIN of the key that claimed
// THAT turn — empty when the turn ended on its own. Returning the origin rather
// than a bool is what lets the marker name the key: "was it claimed" and "by
// what" are the same fact, read once, at the one moment the window closes.
func endTurn(sess *chatSession) lineedit.KeyOrigin {
	sess.turnMu.Lock()
	defer sess.turnMu.Unlock()
	claimed := sess.turnClaimedOrigin
	sess.turnRunning = false
	sess.turnClaimed = false
	sess.turnClaimedOrigin = ""
	return claimed
}

// turnRunning reports whether a turn is in flight, for the input layer's Ctrl+C
// predicate.
func turnRunning(sess *chatSession) bool {
	sess.turnMu.Lock()
	defer sess.turnMu.Unlock()
	return sess.turnRunning
}

// runChatWithInput is the REPL itself, and it is the ONE implementation the two
// faces of this verb share: the interactive `gopi chat` (provided == nil, input
// taken from the real stdin) and the one-shot `gopi chat --once` (chat_once.go,
// which hands in a cooked input over its own prompt-then-EOF reader and the argv
// tail with the one-shot words already stripped). A second REPL would drift from
// this one the first time either changed, and the whole point of the one-shot
// face is that a script drives the SAME turn a human types.
func runChatWithInput(args []string, provided *chatInput) int {
	// THE TURN-CLOSE COUNTER IS BUILT WITH THE SESSION, not when a replay asks for
	// it: a replay driver is started from inside this same function and runs on
	// its own goroutine, so a counter installed later is a counter the first
	// turns close without (chat_replay_witness.go).
	sess := &chatSession{witness: &chatTurnWitness{}}
	// replay is this run's replay driver, non-nil exactly when `--replay` named a
	// session (chat_replay.go). It is declared HERE, before the argv walk, because
	// the walk is what decides it and everything below reads the one value.
	var replay *chatReplay
	noEditor := false
	noColor := false
	verbose := false
	// answerAlone declines the UNATTENDED context on the one-shot face
	// (chat_unattended.go): `gopi chat --once --answer-alone` runs one answer
	// with an ordinary interactive prompt. It is read once, below, where the
	// face decides what kind of run this is — never a session property.
	answerAlone := false
	// input/closeInput are the REPL's line source. The interactive path builds
	// it below from the real stdin; the one-shot path (provided != nil) hands in
	// its own. Declared here so both paths share the ONE declaration.
	var input *chatInput
	var closeInput func()
	// The dump request starts from the ENVIRONMENT (GOPI_CHAT_DUMP), which is
	// also where the DEFAULT lives: a run that says nothing gets the dump ARMED.
	// The flag below overrides the env outright (the ui verb's own precedence,
	// so one capability has one precedence rule across the two surfaces), and
	// `--no-dump-screen` overrides it in the other direction.
	dumpOpts := chatDumpFromEnv()
	// The RAW byte-stream switch starts from the ENVIRONMENT and is ON BY
	// DEFAULT (GOPI_SCREEN_STREAM; the pre-rename GOPI_CHAT_SGR_STREAM is still
	// read). It is a SEPARATE request from the dump, deliberately: the two
	// artifacts answer different questions ("what does the grid show" vs "what
	// bytes were written"), and one of them must be armable without paying for
	// the other (chat_sgr_stream.go).
	sgrOpts := chatSgrFromEnv()
	chatSgrEnvAliasWarn(os.Stderr)
	// sgrReplay is `--screen-replay <SESSION-ID|FILE>` — PLAY BACK a recorded byte
	// stream instead of running anything (chat_sgr_replay.go). It is a separate
	// axis from --replay: that one re-runs a session through this build's
	// renderer, this one plays the bytes the renderer wrote at the time, which is
	// the only evidence channel that does not depend on the render algorithm.
	var sgrReplay chatSgrReplayTarget
	// sgrFlagStated records that the RAW stream was named on THIS command line
	// (see chatFilmSgrRefusal): in a film run the flag cannot reach the process
	// that paints, so it is refused by name rather than silently accepted.
	sgrFlagStated := false
	rest := make([]string, 0, len(args))
	// helpWanted is set by the help scan: `-h`/`--help` documents the command
	// line, so it is answered FIRST — and it may NOT be combined with a
	// positional word (see below). It is a separate pass because the flag walk
	// below sends `-h`, like every other `-`-word, down the unknown-token path
	// (its definition lives with the help it prints).
	helpWanted := false
	// helpScope is WHICH PAGE a usage query asks for, read from the command line
	// the operator wrote BEFORE the walk below consumes any of it: `--replay`,
	// `--film` and the rest of that family name a help SCOPE, so
	// `gopi chat --replay -h` renders that family's own page instead of the
	// ordinary session page (chat_replay_help_scope.go). Reading the raw argv is
	// the only way to know: a successful parse leaves no trace of which flags
	// were named, and the value of `--replay` is consumed into replayTarget.
	helpScope := chatHelpScopeFor(args)
	// policyFlagValue receives the `--policy VALUE` word — the CLI channel of the
	// chat posture (chat_policy_posture.go). policyFlagSet distinguishes "not
	// said" from a value, so a second spelling of the flag is refused rather than
	// silently letting the last one win.
	policyFlagValue := ""
	policyFlagSet := false
	// spacedBesideHelp counts the argv words this walk consumed as a FLAG'S
	// SPACED VALUE. It exists for ONE measured command line: `gopi chat --replay
	// <id> -h` (and its siblings `--film DIR -h`, `--resume <id> -h`,
	// `--out F.gif -h`, `--<geometry> 100x30 -h`) exited 2 with stdout EMPTY
	// before this field, so an operator asking to READ the page of the mode they
	// named was refused by the very word they asked about. Such a word is not a
	// positional: its flag's parser ate it (rules below), so the walk knows
	// exactly how many were swallowed that way. `rest` keeps its old meaning —
	// only what reached the POSITIONAL branch — so `gopi chat -h some.log` is
	// still refused (a real positional cannot ride a usage query), and the two
	// are told apart by the WALK rather than by a word's spelling.
	spacedBesideHelp := 0
	// helpRefusal is the usage error a flag's own parser raised on the way to a
	// command line that ENDS in a usage query. It is HELD rather than returned on
	// the spot, which is the repair for the sibling the ValueError fix left open:
	// `gopi chat --from-turn some-target -h` answered rc=2 (measured on master,
	// TestChatUsageQueryBesideAValuedFlagsOwnValue) because the flag's parser
	// refused the probe before `-h` was ever read. The QUERY WINS over a value a
	// flag could not use, and the rule stays ONE-SIDED exactly as the arm above
	// is: with no query anywhere on the line the refusal is returned verbatim, so
	// `gopi chat --from-turn some-target` still exits 2 naming the flag (the
	// contract TestChatUsageQueryInventsNoValueForAFlagThatGotNone pins).
	//
	// A DUPLICATE spelling is NOT held: "was given twice" is a statement about the
	// command line, not about a value, and a query must not launder it into a page
	// that runs nothing while the caller believes they wrote one command line.
	var helpRefusal error
	// holdRefusal records a flag parser's refusal and steps over the WORD that
	// caused it (the value in the next slot, when the flag was spelled without
	// `=`), so the walk keeps reaching the rest of the command line instead of
	// dying on the first bad value. Returning the index the walk continues from
	// is what keeps the value from falling through to the POSITIONAL branch and
	// being read as a session to continue — the confusion every valued flag in
	// this walk exists to prevent.
	holdRefusal := func(err error, argv []string, i int) int {
		helpRefusal = err
		if !strings.Contains(argv[i], chatFlagValueSep) && i+1 < len(argv) {
			// The refused VALUE is stepped over but NOT counted as a value this
			// walk consumed: spacedBesideHelp is what lets a legitimate
			// `<flag> VALUE -h` answer the page, and an errored flag consumed
			// nothing — counting it would let a positional ride the same query
			// (`--from-turn bad some.log -h`), which
			// TestChatUsageQueryStillRefusesAPositionalBesideIt forbids.
			return i + 1
		}
		return i
	}
	// resumeTarget receives the `--resume <session-id>` value. It is carried
	// separately from rest because the two are the SAME seed reached two ways,
	// and the parser has to be able to say "both at once" rather than silently
	// letting one shadow the other.
	resumeTarget := ""
	// recordable collects the argv words this run will RECORD for a later
	// resume (see chatRecordableFlags). It is filled as the walk accepts each
	// word, so the record is what the run ACTUALLY got — never a re-parse.
	// The dump flag's own statement is tracked SEPARATELY from the env's, because
	// the two take different paths in a FILM run: the environment reaches the
	// filmed process, the flag does not (§ B3 below and chatFilmDumpRefusal).
	dumpFlagStated := false
	// wechatFlagStated records that this run was asked for the PHONE-SHAPED
	// animation (--wechat). It is a bare switch with no knobs of its own: see
	// chat_film_gif.go for why the destination is a PRODUCT ATTRIBUTE rather than
	// a four-channel setting.
	wechatFlagStated := false
	recordable := make([]string, 0, len(args))
	// inherited collects the options the RECORD restored, so this run's own
	// record carries them forward (chatRecordableFlags states the chain reason
	// — without it, resume survives exactly one crash).
	inherited := make([]string, 0, len(args))
	// replayTarget receives the `--replay <session-id>` value: the session this
	// run RE-RUNS (chat_replay.go) instead of continuing. It is carried beside
	// resumeTarget because the two are opposite intents — continue a session vs
	// re-run one — and the parser must be able to say "both at once" rather than
	// letting one shadow the other.
	replayTarget := ""
	// fromTurn is `--from-turn N`'s value: the ordinal (1-based) of the first
	// turn this replay plays IN FULL, 0 when the whole recording plays.
	// fromTurnSet records that the operator stated it, so a duplicated flag is
	// refused rather than silently taking the last number (Rule 15a: 0 cannot
	// mean "stated as 0" — the parser refuses 0 outright).
	var fromTurn int
	var fromTurnSet bool
	// filmDir receives the `--film <dir>` value: the directory the frames land
	// in. It is the REPLAY's own driver side (chat_film.go), so it is handled
	// before any surface exists — a film run needs no editor of its own, it owns
	// a private display and a private terminal and runs a SECOND gopi inside it.
	filmDir := ""
	// filmSet distinguishes `--film DIR` from silence, because the value alone
	// cannot: a missing value is already refused by its own parser.
	filmSet := false
	// gifOut receives the `--out <file.gif>` value: the ANIMATION the film's
	// frames are encoded into (chat_film_gif.go). It is the film's second half,
	// so it lives beside filmDir and is dispatched with it.
	gifOut := ""
	// gifOutSet distinguishes `--out FILE` from silence for the same reason
	// filmSet does.
	gifOutSet := false
	// voiceAudio receives the `--voice-audio FILE` value: the audio `/voice`
	// reads instead of a microphone (chat_voice_audio.go). It is carried as the
	// walk's own value and resolved with the environment ONCE, at the recorder's
	// construction, so the flag and the env channel cannot be read in two places.
	voiceAudio := ""
	// filmTerminal receives the `--film-terminal NAME` value: which terminal the
	// film runs inside. It stays a NAME here — the registry's own resolver
	// (shot.ResolveFilmTerminal) is what turns it into a launch shape, so this
	// verb carries a channel of the knob rather than a decision about terminals.
	filmTerminal := ""
	// terminalSet distinguishes `--film-terminal NAME` from silence: the word
	// without `--film` is REFUSED by name, which is only possible if the walk
	// remembers that it was said.
	terminalSet := false
	// beatFlag carries the BEAT PROTOCOL's words (--scratch and its companions,
	// chat_replay_beats.go): the same handshake scripts/replay/replay.sh speaks to
	// `gopi replay`, so the driver films THIS surface with the launcher it already
	// has. It is consumed before the walk below for the reason the walk's own
	// comment gives — this vocabulary is closed and would refuse them as unknown.
	var beatFlag chatBeatFlag
	var beatRest []string
	var beatErr error
	if beatFlag, beatRest, beatErr = parseChatBeatFlags(args); beatErr != nil {
		return chatRefuseUsage(beatErr)
	}
	args = beatRest
	// INDEXED, not ranged: a VALUED flag consumes the word in its next slot, and
	// a ranged loop would hand that value back as the next iteration's word —
	// `--resume <id>` then reads the id as a bare positional and refuses the
	// command line it was asked to run (measured while writing the vocabulary
	// walk: "--resume names a session and the argument names another").
	for i := 0; i < len(args); i++ {
		a := args[i]
		if chatValuedFlagWord(a) && chatUsageQueryOnLine(args) {
			// A DUPLICATE SPELLING IS NOT LAUNDERED BY THE QUERY. Both arms
			// below step over a valued flag's value slot WITHOUT running that
			// flag's parser, and "was given twice" lives in the parser — so the
			// swallow used to skip the duplicate check as well, and
			// `gopi chat --from-turn 2 --from-turn 3 -h` answered the PAGE at
			// rc 0 while the same line without `-h` is refused at rc 2
			// (measured 2026-10-27, master 6f6f7ff, on the real binary).
			// Being stated twice is a statement about the COMMAND LINE, not
			// about a value, so it is refused here — the same one-sided rule:
			// with no query on the line this arm cannot fire and the flag's own
			// parser refuses in its own words.
			if chatValuedFlagStatedBefore(args, i) {
				return chatRefuseUsage(fmt.Errorf("%s was given twice — a duplicate statement about one run is a command-line error a usage query cannot answer away",
					chatValuedFlagSpelling(a)))
			}
		}
		// A USAGE QUERY IN A VALUED FLAG'S VALUE SLOT IS THE QUERY, NOT A VALUE.
		// `gopi chat --replay -h` asked to READ the command line of the mode it
		// named; the word sitting in --replay's value slot is the help flag, and
		// reading it as the session to re-run answered a usage error at rc=2 with
		// stdout EMPTY — the recorded C5 defect (task 01a10351), whose row reads
		// completed while scripts/replay-check.sh still measured it red.
		//
		// The rule is ONE-SIDED, and that is what keeps the refusal policy below
		// intact: the word is taken for the QUERY, never for a value, so
		// `--replay -h` re-runs nothing and `--tools -h` keeps answering the page
		// with the switch ignored. A valued flag followed by any other word parses
		// exactly as it did, and ONE valued flag with NO value beside it is still
		// a usage error (`gopi chat --replay` alone) — no value is invented for it.
		//
		// The vocabulary is chatResumeValuedFlags, the parser's own list of the
		// flags that take a value (Rule 13): a flag that joins that list gets this
		// arm by construction rather than by being named here a second time.
		if chatValuedFlagWord(a) && chatUsageQueryWord(chatNextArg(args, i)) {
			helpWanted = true
			i++ // the query word was in the value slot: consume it, answer the page
			continue
		}
		// A USAGE QUERY ANYWHERE ON THE LINE WINS OVER THIS FLAG'S VALUE
		// REFUSAL — but never over its CONSUMPTION. The arm above answers the
		// query that sits IN the value slot; this one covers the query that
		// comes LATER, which the walk could not survive: the flag's own parser
		// read the word in the slot, refused it as a value, and `chatRefuseUsage`
		// returned exit 2 with the page never reached. Measured 2026-10-27 (master
		// d2dcd2b): `gopi chat --from-turn some-target -h` exited 2 while all
		// eight sibling valued flags answered the page — --from-turn is the only
		// one whose parser VALIDATES the word it reads.
		//
		// WHAT IT DOES NOT DO, and the two halves are what keep this honest:
		//   (a) it does not VALIDATE the value — a help request documents a
		//       command line, it does not run one, so a flag whose operand is a
		//       typo is not an error when the operator asked for the page;
		//   (b) it does not leave the operand in argv. Consumption is unchanged:
		//       the word in the slot is swallowed exactly as the flag's parser
		//       would have swallowed it, so it never reaches the positional
		//       handler and the "cannot be combined with the argument" refusal
		//       stays about REAL positionals (`gopi chat -h some.log`).
		// The rule stays ONE-SIDED, which is the whole point: with NO query on
		// the line this arm cannot fire, so `gopi chat --from-turn some-target`
		// still refuses, at exit 2, naming the operand — Rule 15's refusal is
		// what the flag exists for, and it is pinned by its own test.
		if chatValuedFlagWord(a) && chatUsageQueryOnLine(args) {
			helpWanted = true
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value slot is swallowed UNVALIDATED; the page is what was asked for
			}
			// NOT counted in spacedBesideHelp, deliberately. That counter exempts
			// a word from the "usage only" refusal because it was a flag's VALUE
			// rather than a positional — and a real positional BESIDE the query
			// must keep being refused (`gopi chat --replay <id> -h some.log`, the
			// contract TestChatUsageQueryStillRefusesAPositionalBesideIt pins).
			// Counting this swallow would hand that line the page instead: the
			// exemption would be bought by the arm that never validated anything.
			continue
		}
		// --replay TAKES A VALUE and is consumed FIRST: its target is not a seed
		// (it is not continued) and it must not be read as one.
		if target, consumed, perr := chatParseReplayArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if perr != nil {
				i = holdRefusal(perr, args, i)
				continue
			}
			if replayTarget != "" {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one target re-runs one session",
					chatFlagReplay, replayTarget, target))
			}
			replayTarget = target
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		// --from-turn TAKES A NUMBER and is consumed here, beside --replay: it
		// states WHERE in that recording this run begins (chat_replay.go). It is
		// read before the closed-vocabulary walk so its value never reaches the
		// positional handler and is never read as a session to continue.
		if n, consumed, ferr := chatParseFromTurnArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if ferr != nil {
				i = holdRefusal(ferr, args, i)
				continue
			}
			if fromTurnSet {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%d and %d) — one run begins at one turn",
					chatFlagFromTurn, fromTurn, n))
			}
			fromTurn, fromTurnSet = n, true
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		// --film-terminal TAKES A NAME and is consumed here, beside --film: it
		// chooses WHICH terminal the film runs inside, and the name it carries is
		// resolved ONCE, later, by shot.ResolveFilmTerminal — the same function
		// the env and settings channels go through, so this verb is a channel of
		// that knob rather than a second place that decides.
		if name, consumed, terr := chatParseTerminalArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if terr != nil {
				i = holdRefusal(terr, args, i)
				continue
			}
			if terminalSet {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one film runs inside one terminal",
					chatTerminalFlag, filmTerminal, name))
			}
			filmTerminal, terminalSet = name, true
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		// --policy TAKES A VALUE: the posture this run resolves (and, for now,
		// only reports). It is consumed here, before the switch, so the closed
		// vocabulary never sees the value as a positional.
		if v, consumed, perr := chatParsePolicyArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if perr != nil {
				i = holdRefusal(perr, args, i)
				continue
			}
			if policyFlagSet {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one run resolves one posture",
					chatPolicyFlag, policyFlagValue, v))
			}
			policyFlagValue, policyFlagSet = v, true
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		// --film TAKES A VALUE and is consumed here, beside --replay (the two are
		// one feature's two halves: the recording and where its frames go).
		if dir, consumed, ferr := chatParseFilmArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if ferr != nil {
				i = holdRefusal(ferr, args, i)
				continue
			}
			if filmSet {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one film goes into one directory",
					chatFlagFilm, filmDir, dir))
			}
			filmDir, filmSet = dir, true
			// A BARE `--film` names no directory (dir == ""): where its frames land
			// is settled by resolveFilmDir at the dispatch point, once `--out` is
			// known too, so the refusal there can state both spellings.
			//
			// ONLY the space form consumes the next slot. A bare `--film` must NOT
			// advance past the next word — that word is the NEXT FLAG (measured:
			// `--film --out F.gif` advanced past `--out`, and its value then fell
			// through to the positional handler and was read as a session to
			// continue).
			if !strings.Contains(a, chatFlagValueSep) && dir != "" {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		// --out TAKES A VALUE and is consumed here, beside --film: it names the
		// ANIMATION those frames are encoded into, and the two are one command
		// line (`--film DIR --out FILE.gif`).
		if path, consumed, oerr := chatParseOutArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if oerr != nil {
				i = holdRefusal(oerr, args, i)
				continue
			}
			if gifOutSet {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one film is animated once",
					chatFlagOut, gifOut, path))
			}
			gifOut, gifOutSet = path, true
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if path, consumed, oerr := chatParseVoiceAudioArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if oerr != nil {
				i = holdRefusal(oerr, args, i)
				continue
			}
			if voiceAudio != "" {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one run reads one audio source",
					chatVoiceAudioFlag, voiceAudio, path))
			}
			voiceAudio = path
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if parsed, matched, perr := parseChatSgrReplayArgWithNotice(a, chatNextArg(args, i), i+1 < len(args), os.Stderr); matched {
			if perr != nil {
				return chatRefuseUsage(perr)
			}
			// NOT recorded for a resume, deliberately: a playback is a SUBCOMMAND in
			// disguise — it runs once, plays bytes and exits — so recording it would
			// make a later `--resume` of this session re-play a recording instead of
			// continuing a conversation (the same reason a film's knobs are not the
			// session's recorded options).
			if sgrReplay.set {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one run plays one stream",
					chatSgrReplayFlag, sgrReplay.target, parsed.target))
			}
			sgrReplay = parsed
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if parsed, matched, perr := parseChatSgrArgWithNotice(a, chatNextArg(args, i), i+1 < len(args), os.Stderr); matched {
			if perr != nil {
				return chatRefuseUsage(perr)
			}
			// Recorded for a later resume, in the flag's own spelling (a bare
			// flag, the joined path, or the OFF switch) — the same rule the dump
			// flag follows, so `--resume` restores what the run was actually
			// started with. The OFF word counts for the dump's reason: a decline
			// is a CHOICE, and a resumed run that ignored it would re-arm an
			// artifact the operator had turned off. The DEFAULT is not recorded —
			// it is not a statement (see chatSgrOptions.stated).
			switch {
			case parsed.disabled:
				recordable = append(recordable, chatNoSgrStreamFlag)
			case parsed.path != "":
				recordable = append(recordable, chatSgrStreamFlag+chatFlagValueSep+parsed.path)
			default:
				recordable = append(recordable, chatSgrStreamFlag)
			}
			sgrOpts = parsed
			sgrFlagStated = true
			continue
		}
		if parsed, matched, perr := parseChatDumpArg(a, chatNextArg(args, i), i+1 < len(args)); matched {
			if perr != nil {
				return chatRefuseUsage(perr)
			}
			// The word this run was GIVEN is recorded, so a resume restores what
			// the run was started with. The OFF switch counts: a decline is a
			// choice, and a resumed run that ignored it would re-arm an artifact
			// the operator had turned off. The DEFAULT is not recorded — it is
			// not a statement, and recording it would make every run's own
			// record supersede a resumed session's destination.
			//
			// statedAlsoTOTheFilmedRun records that the dump was named on THIS
			// command line — see chatFilmDumpRefusal: in a film run the flag
			// cannot reach the process that paints, so it is refused by name
			// rather than silently accepted (B3).
			dumpFlagStated = true
			switch {
			case parsed.disabled:
				recordable = append(recordable, chatNoDumpFlag)
			case parsed.path != "":
				recordable = append(recordable, chatDumpFlag+chatFlagValueSep+parsed.path)
			case parsed.requested:
				recordable = append(recordable, chatDumpFlag)
			}
			dumpOpts = parsed
			continue
		}
		if a == HelpFlag || a == HelpFlagLong {
			helpWanted = true
			continue
		}
		// --answer-alone is the one-shot face's DECLINE of the unattended
		// context (see chatFlagAnswerAlone): consumed here, beside the other
		// bare switches, so the closed vocabulary accepts it and the run's own
		// option record never carries it (it is a property of THIS invocation).
		if a == chatFlagAnswerAlone {
			answerAlone = true
			continue
		}
		// --wechat is a BARE switch asking for the phone-shaped animation. It is
		// consumed here, beside --film/--out, and it carries no value to advance
		// past — so it never moves the argument walk.
		if matched, werr := chatParseWechatArg(a); matched {
			if werr != nil {
				return chatRefuseUsage(werr)
			}
			if wechatFlagStated {
				return chatRefuseUsage(fmt.Errorf("%s was given twice — one run writes one animation, in one shape", chatFlagWechat))
			}
			wechatFlagStated = true
			continue
		}
		// --resume TAKES A VALUE and is consumed here, before the switch: the
		// value names the session whose recorded options this run inherits.
		if target, consumed, perr := chatParseResumeArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if perr != nil {
				i = holdRefusal(perr, args, i)
				continue
			}
			if resumeTarget != "" {
				return chatRefuseUsage(fmt.Errorf("%s was given twice (%q and %q) — one target names one session",
					chatFlagResume, resumeTarget, target))
			}
			resumeTarget = target
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if v, consumed, ok := chatParseAmbiguousWidthArg(a, chatNextArg(args, i), i+1 < len(args)); ok {
			if !consumed {
				return chatRefuseUsage(fmt.Errorf("%s needs a value (%s or %s)",
					chatAmbiguousWidthFlag, config.AmbiguousWidthWide, config.AmbiguousWidthNarrow))
			}
			// The flag's ONE representation is the env var (Rules 2/13/14): the
			// verb carries no second field, so the surface cannot read a width the
			// reader never saw. An unusable value is NOT refused here either — the
			// ONE reader (config.ResolveAmbiguousWidthMode) reports it and the next
			// channel decides (Rule 15), the same soft policy the geometry applies.
			config.ApplyEnvAmbiguousWidth(v)
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if v, consumed, perr := chatParseGeometryArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if perr != nil {
				i = holdRefusal(perr, args, i)
				continue
			}
			// The flag's ONE representation is the env var (Rules 2/13/14): this
			// walk does not carry a second field, so the surface cannot read a
			// value the resolver never saw. It is NOT recorded for resume — the
			// geometry is a property of the TERMINAL this run is on, not of the
			// session, and restoring it would ask a future terminal to be the
			// size this one happened to be.
			config.PublishChatIdealGeometryFlag(v)
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if v, consumed, perr := chatParseSurfaceTurnsArg(a, chatNextArg(args, i), i+1 < len(args)); consumed {
			if perr != nil {
				i = holdRefusal(perr, args, i)
				continue
			}
			// The flag's ONE representation is the env var (Rules 2/13/14): this
			// walk carries no second field, so the surface cannot enforce a bound
			// the resolver never saw. An unusable value is NOT refused here — the
			// ONE resolver (config.ChatSurfaceTurns) reports it and the next
			// channel decides (Rule 15), the same soft policy the geometry applies.
			config.PublishChatSurfaceTurnsFlag(v)
			if !strings.Contains(a, chatFlagValueSep) {
				i++ // the value was in the next slot: consume it
				spacedBesideHelp++
			}
			continue
		}
		if chatREPLSwitch(a, sess, &noEditor, &noColor, &verbose) {
			if chatResumeOptionWord(a) {
				recordable = append(recordable, a)
			}
			continue
		}
		// The vocabulary is CLOSED. Every word that reached here is either the
		// one positional the synopsis documents (a raw-log path / session id) or
		// a word this verb does not know — and the second must never be read as
		// the first (Rule 15): `gopi chat --bogus-flag` is a FLAG error, not a
		// seed the resolver then hunts for and reports as a "resume target".
		//
		// The synopsis documents exactly ONE positional, so a SECOND bare word
		// is refused rather than silently dropped: `gopi chat a.log -v b.log`
		// means something the caller did not get, and taking the first word and
		// discarding the second is the quiet drop this parser exists to close.
		if strings.HasPrefix(a, chatFlagPrefix) {
			return chatRefuseUnknownToken(a)
		}
		if len(rest) > 0 {
			return chatRefuseUsage(fmt.Errorf("gopi chat takes ONE %s (the %s %s) — %s was already given",
				chatWordArgument, "[raw-log-path|session-id]", rest[0], a))
		}
		rest = append(rest, a)
	}
	// A HELD REFUSAL IS RETURNED HERE — AFTER the walk, never inside it. The walk
	// had to reach the END of the command line before it could know whether a
	// usage query was on it, and that is the whole repair: `gopi chat --from-turn
	// some-target -h` used to be refused by --from-turn's own parser at rc=2 with
	// stdout empty, so the operator asking how --from-turn is spelled was refused
	// by the flag they were asking about (measured on master,
	// TestChatUsageQueryBesideAValuedFlagsOwnValue's --from-turn subtest).
	//
	// With NO query anywhere on the line the refusal is the value the parser
	// produced, returned verbatim and at the same status, so nothing is weakened:
	// `gopi chat --from-turn some-target` still exits 2 naming the flag, and the
	// one-sided rule TestChatUsageQueryInventsNoValueForAFlagThatGotNone pins is
	// untouched (a flag with no value beside it never reaches here — its parser
	// reports the missing value, which is held by the same arm).
	if helpRefusal != nil && !helpWanted {
		return chatRefuseUsage(helpRefusal)
	}
	// THE USAGE QUERY WINS OVER EVERYTHING BELOW. A help request documents the
	// command line; it never runs one. Answering it HERE — before the seed
	// reconciliation, the seed refusals and the dispatch — is what makes the page
	// reachable from EVERY argv the operator can type, and it is the contract the
	// surface had lost: measured 2026-10-04, `gopi chat --replay -h` printed a
	// usage error naming the missing session at rc=2 with stdout EMPTY, so the
	// operator asking how `--replay` is spelled was refused by the flag they were
	// asking about (C5 of scripts/replay-check.sh; the row that owned it, task
	// 01a10351, reads completed while the defect is live — Rule 26).
	//
	// WHY IT SITS ABOVE THE SEED LOGIC RATHER THAN INSIDE IT. The seeds below are
	// reasons a RUN cannot start, and a usage query is not a run: `--replay X -h`
	// names a session and a bare positional at once, which the reconciliation
	// refuses at rc=2 — a refusal of a command line the caller never wanted
	// executed. Answering the page first removes that whole class, for every
	// flag, without weakening one refusal: with no query present the walk below
	// is exactly what it was.
	//
	// A positional BESIDE the query is still refused (below), so the quiet drop
	// the closed vocabulary closed stays closed: it is a word the run would have
	// consumed, and help was asked for instead. A valued flag's own spaced value
	// is NOT such a word — the walk consumed it as the flag's value (see
	// spacedBesideHelp) — and a flag whose value slot held the QUERY itself left
	// no value to consume at all.
	if helpWanted && len(rest) == 0 {
		if _, werr := os.Stdout.WriteString(chatHelpPage(helpScope)); werr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", werr)
			return chatExitFailed
		}
		return chatExitOK
	}
	// `--resume <id>` and the positional seed are the SAME thing asked two
	// ways: both name the session this run continues and both resolve through
	// the ONE resolver below. Two targets at once is refused — nothing silently
	// wins — and one target of either spelling lands in rest[0], so everything
	// after this point has ONE case to handle rather than two that can drift.
	seed := ""
	if len(rest) > 0 {
		seed = rest[0]
	}
	switch {
	case replayTarget != "" && resumeTarget != "":
		return chatRefuseUsage(fmt.Errorf("%s re-runs a recorded session (%q) and %s continues one (%q) — a replay continues nothing",
			chatFlagReplay, replayTarget, chatFlagResume, resumeTarget))
	case replayTarget != "" && seed != "":
		return chatRefuseUsage(fmt.Errorf(chatReplaySeedConflict,
			chatFlagReplay, replayTarget, chatWordArgument, seed))
	case resumeTarget != "" && seed != "":
		return chatRefuseUsage(fmt.Errorf("%s names a session (%q) and the %s %q names another — one resume continues one session",
			chatFlagResume, resumeTarget, chatWordArgument, seed))
	case replayTarget != "":
		// A replay is not a resume seed: the recording is reached through its own
		// resolver (chat_replay.go), so nothing is written into `rest` and the
		// checkpoint path below never runs. `resumed` stays false for the same
		// reason — there are no recorded options to inherit from a session this
		// run is not continuing.
	case resumeTarget != "":
		seed = resumeTarget
	}
	rest = rest[:0]
	if seed != "" {
		rest = append(rest, seed)
	}
	// resumed says this run's target came from the resume SWITCH, which is the
	// one thing that differs between the two spellings: the switch means
	// "restore the recorded options", the positional seed means "continue this
	// conversation" and only autodetects them when nothing else was stated
	// (see chatApplyResumeOptions).
	resumed := resumeTarget != ""
	if helpWanted {
		// The query itself was answered above, for every command line that named
		// no positional. What is left here is the ONE case the early return
		// deliberately leaves to this point: a positional BESIDE the query, which
		// is refused rather than ignored — the run the caller spelled cannot start
		// (help was asked for), and swallowing the seed silently discards the
		// argument they wrote (Rule 15), which is exactly the class of quiet drop
		// this parser exists to close.
		//
		// The !resumed arm is the --resume spelling of the same word: its target
		// reaches rest[0] through the switch, and the switch means "continue this
		// session", not "read a page about it".
		if len(rest) > 0 && !resumed && spacedBesideHelp == 0 {
			return chatRefuseUsage(fmt.Errorf("%s prints this verb's usage only — it cannot be combined with the %s %s",
				HelpFlag, chatWordArgument, rest[0]))
		}
		// The usage page is written VERBATIM: it carries format-shaped text (a
		// frame's %04d) that is part of the page, not a directive for this call.
		// WHICH page is the scope's answer — `gopi chat --replay <id> -h` reads the
		// replay family's own page, and the choice is made in ONE place (above) so
		// the two answer points cannot disagree.
		if _, werr := os.Stdout.WriteString(chatHelpPage(helpScope)); werr != nil {
			fmt.Fprintf(os.Stderr, "gopi chat: %v\n", werr)
			return chatExitFailed
		}
		return chatExitOK
	}
	// --screen-replay IS A PLAYBACK, NOT A CHAT RUN, and it is dispatched HERE for the
	// same reason --film is: before any surface exists. It needs no editor, no agent,
	// no checkpoint and no provider — it needs a file and a terminal — so building a
	// chat around it would create everything the verb exists to avoid paying for.
	// The conflicts below are refused BY NAME rather than silently dropping one of
	// the two words (Rule 15): a command line that names two things to run has one
	// answer, and it is a usage error, not a winner.
	if sgrReplay.set {
		if err := chatSgrReplayConflict(replayTarget, resumed, seed, filmSet); err != nil {
			return chatRefuseUsage(err)
		}
		// THE RECORDING SWITCH CANNOT RECORD A PLAYBACK, and it is stated only when
		// the caller SAID something. The raw stream is armed inside a chat run's
		// sink, and a playback has no sink — so an armed switch here would produce
		// no file while looking armed.
		//
		// THE DEFAULT IS NOT A STATEMENT (measured 2026-11-08, bug 01a1b823-5070).
		// The stream is recorded by DEFAULT, so `sgrOpts.enabled` is true on every
		// run that never mentioned the switch — which made this notice fire on every
		// playback and put a line on stderr for a HEALTHY recording. The replay verb's
		// contract is that a closed, self-consistent recording prints NOTHING (a
		// capture comparing two builds must not have to filter our prose out), and
		// this line broke it from the other side of the verb. `stated` is the field
		// that already distinguishes "the caller asked for this" from "the default
		// chose it" (chatSgrOptions.stated, set by the flag and the environment).
		if sgrOpts.stated && sgrOpts.enabled {
			fmt.Fprintf(os.Stderr, chatSgrReplayNoArchiveFormat, chatErrorPrefix, chatSgrStreamFlag+"/"+chatSgrStreamEnv)
		}
		return runChatSgrReplay(sgrReplay.target)
	}
	// --film IS A DRIVER, NOT A CHAT RUN, and it is dispatched HERE: before any
	// surface exists. The driver owns a private X display and a private terminal,
	// and runs a SECOND copy of this binary inside that terminal as the PRODUCER
	// (chat_film.go) — so this process needs no editor, no agent and no provider,
	// and the run it films is the producer's own. Dispatching it any later would
	// build a surface this run would then never photograph.
	if filmSet {
		if replayTarget == "" {
			return chatRefuseUsage(fmt.Errorf("%s", chatFilmNoReplay))
		}
		if resumeTarget != "" || seed != "" {
			return chatRefuseUsage(fmt.Errorf("%s re-runs a recorded session (%q) — a film re-runs the recording, it continues nothing",
				chatFlagFilm, replayTarget))
		}
		// B3, CLOSED HERE: a dump flag on a film run names an artifact the
		// PRODUCER would have to write — the process that paints the screen is
		// the second copy of this binary inside the private terminal, and this
		// run's argv does not travel to it. Accepting the word and using it on
		// THIS process would arm a sink that paints nothing (there is no surface
		// here at all), which is exactly the silent no-op the defect was.
		if err := chatFilmDumpRefusal(dumpFlagStated, dumpOpts); err != nil {
			return chatRefuseUsage(err)
		}
		// The RAW stream flag is refused on a film line for the dump flag's own
		// reason (chatFilmDumpRefusal): the process that paints is the second
		// gopi inside the private terminal, and this run's argv does not travel
		// to it. The ENVIRONMENT is deliberately not refused — the producer
		// inherits it, which is the channel that works there.
		if err := chatFilmSgrRefusal(sgrFlagStated, sgrOpts); err != nil {
			return chatRefuseUsage(err)
		}
		// --voice-audio on a FILM line is refused for the dump's own reason: the
		// process that would read the file is the one inside the private terminal,
		// and this driver builds its argv (chat_voice_audio.go).
		//
		// THE FLAG IS WHAT IS REFUSED, NOT THE ENVIRONMENT — the same distinction
		// chatFilmDumpRefusal draws, and for the same reason: an exported variable
		// is ambient context that the producer inherits (so it WORKS there), while a
		// word on this command line is an instruction to THIS process, which runs no
		// chat surface at all. Refusing the ambient spelling would break a shell that
		// exported it for another terminal.
		if err := chatVoiceAudioRefusal(voiceAudio, true, false); err != nil {
			return chatRefuseUsage(err)
		}
		// --wechat asks for a DELIVERABLE, and the deliverable is the animation:
		// the film directory it is shot from is the run's own record, not the thing
		// a phone is sent. So the destination without a product is refused by name
		// rather than accepted as a word that changes nothing (Rule 15).
		if wechatFlagStated && gifOut == "" {
			return chatRefuseUsage(fmt.Errorf(chatFilmWechatNeedsOut,
				chatFlagWechat, chatFlagOut, chatFlagReplay, chatFlagFilm, chatFlagOut, chatFlagWechat))
		}
		// WHERE THE ARTIFACTS GO. A bare `--film` (no directory) takes the
		// animation's own directory, so the operator's single command line
		// (`--film --out FILE.gif`) is complete without a second path; with no
		// `--out` either there is nowhere at all, and that is refused by name.
		resolved, rerr := resolveFilmDir(filmDir, gifOut, chatFlagFilm)
		if rerr != nil {
			return chatRefuseUsage(rerr)
		}
		return runChatFilmOut(resolved, replayTarget, gifOut, filmTerminal, beatFlag, wechatFlagStated)
	}
	// --film-terminal names the terminal a FILM runs inside and nothing else: with
	// no --film there is no film to photograph, so the word is refused by name
	// rather than dropped (Rule 15 — an accepted flag that changes nothing is a
	// silent no-op, and the operator would believe the next run had used it).
	if terminalSet {
		return chatRefuseUsage(fmt.Errorf("%s chooses the terminal a %s runs inside and no %s was given — write %s SESSION %s DIR %s NAME",
			chatTerminalFlag, chatFlagFilm, chatFlagFilm, chatFlagReplay, chatFlagFilm, chatTerminalFlag))
	}
	// --out is the FILM's animation arm and nothing else: it encodes the frames a
	// film photographed, so without --film there are no beats to animate, and a
	// live conversation would be animated from nothing. Refused by name rather
	// than ignored (Rule 15).
	if gifOutSet {
		return chatRefuseUsage(fmt.Errorf("%s", chatFilmOutNeedsFilm))
	}
	// --voice-audio names the audio `gopi chat`'s `/voice` mode reads instead of a
	// microphone, and the ONE-SHOT path has no such mode: it runs a single turn
	// with no prompt loop and no editor, so accepting the word here would arm a
	// capture nothing ever reads (Rule 15). The refusal is shared with the film
	// arm — one function, so the two shapes cannot answer one mistake two ways.
	// The FLAG ALONE is what it answers (see the film arm above); the environment
	// is ambient and is simply not read where the mode does not exist.
	if err := chatVoiceAudioRefusal(voiceAudio, false, provided != nil); err != nil {
		return chatRefuseUsage(err)
	}

	// Optional session seed: continue an existing checkpoint. rest carries
	// the argv with the --tools flag already stripped. rawLogPath is hoisted
	// because the resume record needs the SAME resolution this run already
	// performed (a second call would be a second answer to one question).
	rawLogPath := ""
	// continuedSessionID is the session this run CONTINUES — the id the agent
	// must reuse instead of minting its own (epic: `gopi chat --resume <id>`
	// minted a second session, so the resumed conversation never grew a byte and
	// every resume left one more stranded sessions/<uuid>/ directory behind).
	// Empty for a fresh run, which is what keeps every other path unchanged.
	//
	// It is the SESSION RECORD's own id, resolved through chatSessionFileFor —
	// the reader the recorded options and the turn baseline already go through —
	// so the id this run continues and the record it read the options from are
	// the same answer to the same question (a second resolution could pick a
	// different layout and mint the split this epic removes).
	continuedSessionID := ""
	// continuedSessionLog is the RECORD FILE of the session being continued —
	// the id alone cannot spell it (a task run keeps sessions/<task-id>/
	// <run-uuid>.jsonl; the task id comes from an env var a resume does not
	// carry), so the path is carried the same way the id is. Empty means the
	// record could not be resolved, in which case the id-derived layout is what
	// there is and the announcement above already said so.
	continuedSessionLog := ""
	// seededCkpt is the checkpoint this run continues from, kept so the
	// capability warning can be decided AFTER the recorded options are applied.
	var seededCkpt *deepseek.ClaudeRequest
	if len(rest) > 0 {
		p, err := resolveResumeLogPath(rest[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi chat: %v\n", err)
			return chatExitFailed
		}
		rawLogPath = p
		// THE RUN-SCOPE LOCK ON THE CHAT RESUME FACE (run_lock_resume_seam.go).
		//
		// THE MEASURED HOLE THIS CLOSES, and it is not hypothetical — it is
		// session `1918704d-3823-40d4-b1bb-e928ac791f73` on this host
		// (2026-11-06 08:05:10 … 08:06:12, evidence:
		// ~/.gopi/sessions/01a1b347-…/evidence/). A long-lived interactive
		// chat (pid 962224, started 11-05 13:01, alive throughout) held that
		// session directory. From INSIDE that session, eleven more
		// `gopi chat --once --resume=<that same id>` processes were started in
		// 60 seconds (pids 3652633, 3657409, 3658426, 3658958, 3660177,
		// 3660554, 3664131, 3665806, 3667318, 3668301, 3675218 — session_start
		// rows 2..12 of its own session.jsonl), each of them appending a SECOND
		// session_start to the same record and each appending to the same
		// .raw.log. Every pair of those lifetimes OVERLAPS (measured: 36
		// overlapping pairs), and all eleven overlap the original holder.
		//
		// That is one directory written by up to twelve processes at once. The
		// write-side damage was already fixed from the other end (the SGR
		// stream's flock, task 01a1b336); this is the missing half — the
		// ARBITRATION that stops the second process from running at all.
		//
		// WHY HERE, AND WHY IT IS THE SAME MECHANISM. `gopi session resume`
		// (cli_resume.go) and `gopi web` (web_resume.go) both take this exact
		// lock on this exact directory before they write anything; the chat
		// resume face was the ONE resume face that never asked, so the exclusion
		// the other two bought did not cover the surface that actually collides
		// in practice (Rule 14: one body, taskRunTakeLockDir — not a second
		// lock, not a second definition of "this run directory is executing").
		//
		// WHERE IT SITS IS THE REQUIREMENT, not a preference: after
		// resolveResumeLogPath (the directory has to be named before it can be
		// locked) and BEFORE LoadCheckpointWithSource / chatApplyRecordedResume
		// Options / newAgentResuming, because those are the calls that WRITE —
		// a refusal that happened after them would be a refusal that already
		// appended a session_start.
		//
		// The holder stamp (pid + start ticks) is in the refusal, so an operator
		// can find the live process without a second investigation; the lock
		// dies with its holder, so `kill -9` leaves nothing to clean up.
		if sp, ok, serr := chatSessionFileFor(rest[0], rawLogPath); serr == nil && ok {
			continuedSessionID = gopilog.FileToID(sp)
			continuedSessionLog = sp
		} else if serr != nil {
			// NOT FATAL ON ITS OWN, AND NOT SILENT (Rule 15): the conversation
			// is still resumable from the checkpoint below, but this run
			// cannot reuse a session id — which means it would MINT one. That
			// mint is exactly what the check immediately below decides, so the
			// reason is reported here and the consequence there.
			fmt.Fprintf(os.Stderr, chatResumeIDUnresolvedFormat, serr)
		}
		// NO RECORD, NO RESUME (row 01a17df9-99c4) — SCOPE: a checkpoint that
		// sits inside a RUN DIRECTORY. Measured before this gate: `gopi chat
		// --resume <run-dir>/<id>.raw.log` with the JSONL deleted exited 0,
		// seeded the conversation, and wrote the whole run into a BRAND-NEW
		// sessions/<uuid>/ while the old run directory kept its bytes — a
		// resume that minted, reported as a success. A checkpoint from anywhere
		// else names no session, so there is no identity to lose and the
		// documented "seed from a log" behaviour stands (see
		// resumeCheckpointNamesARunDir for why the scope is exactly this).
		if continuedSessionID == "" && resumeCheckpointNamesARunDir(rawLogPath) {
			if proceed, refuseCode := resumeRecordGapCheck(chatProgLabel+" "+chatFlagResume, strings.TrimSpace(rest[0]), rawLogPath); !proceed {
				return refuseCode
			}
			// THE DOWNGRADE GOES ON THE ROW TOO (epic 01a17df9-99c4). The
			// warning above corrects the operator standing here; stderr is gone
			// by the time anyone reads the ledger. A chat resumed inside a task
			// run carries its row id in this process's environment (the
			// launcher's own channel, internal/signalrec), so the row records
			// that its session was NOT continued — otherwise the only trace of
			// this run on the row is the resumed:<new-session> tag, which reads
			// as if the named session grew. An empty id (a bare operator chat)
			// is a no-op: there is no row to tell.
			if noteErr := tools.TaskResumeNewSessionNote(os.Getenv(signalrec.TaskIDEnv),
				tools.TaskResumeNewSessionNoteText(strings.TrimSpace(rest[0]), filepath.Dir(rawLogPath), config.ResumeAllowNewSessionEnv)); noteErr != nil {
				fmt.Fprintf(os.Stderr, chatProgLabel+" "+chatFlagResume+": %v\n", noteErr)
			}
		}
		// TWO SOURCES, ONE DECISION (row 01a183a8): the same fallback the resume
		// faces use — a chat whose raw log holds no completed request round
		// (a first request that died at the transport layer writes no body)
		// recovers the request from the crash dump beside it, and SAYS so.
		chatResumeLockRelease, _, lockErr := resumeTakeRunLock(chatProgLabel+" "+chatFlagResume, rawLogPath)
		if lockErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", lockErr)
			return chatExitFailed
		}
		defer chatResumeLockRelease()
		ckpt, history, _, provenance, err := agent.LoadCheckpointWithSource(rawLogPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi chat: %v\n", err)
			return chatExitFailed
		}
		if provenance.Source == agent.CheckpointSourceDump {
			fmt.Fprintf(os.Stderr, resumeDumpCheckpointNoticeFormat, chatProgLabel+" "+chatFlagResume, rawLogPath, provenance.Detail(), resumeDumpCheckpointDigest(ckpt))
		}
		sess.history = history
		sess.continuedFrom = rawLogPath
		fmt.Fprintf(os.Stderr, "[gopi] chat: seeded %d history message(s) from %s\n", len(history), rawLogPath)
		// THE TURN THIS RUN CONTINUES FROM (task 01a16fb3). A resumed chat used to
		// number its turns from 1 while the session's own log already held N, so
		// the heading and the status row disagreed with `gopi session log --turns`
		// for the rest of the run. The baseline is read ONCE, here, from the same
		// counter the log prints, and it feeds DISPLAY only (see turnBaseline).
		baseline, berr := chatResumeTurnBaseline(rest[0], rawLogPath)
		if berr != nil {
			fmt.Fprintf(os.Stderr, "gopi chat: %v\n", berr)
			return chatExitFailed
		}
		sess.turnBaseline = baseline
		if baseline > 0 {
			fmt.Fprintf(os.Stderr, chatResumeTurnBaselineFormat, baseline, baseline+1)
		}
		// THE SESSION'S CACHE BASE (task: the row's Σ was this run alone). Folded
		// from the SAME record the turn baseline came from, once, here — a
		// resumed conversation inherits hundreds of millions of prompt tokens and
		// a runtime counter cannot know that. An unreadable record is NOT fatal
		// (the conversation resumes from the checkpoint either way) but it is
		// REPORTED by name, because the consequence is the very misreading this
		// fix removes (Rule 15).
		base, cerr := chatResumeCacheBase(rest[0], rawLogPath)
		sess.costBase = base
		reportChatResumeCacheBase(base, cerr, continuedSessionLog)
		// The checkpoint carries the interrupted run's `tools` array, and that
		// is the ONE fact a resume cannot recover by itself: a seeded history
		// full of tool_use/tool_result turns means the model WILL keep
		// reasoning in tool terms, and a run that inherits it with tools OFF
		// answers from memory only (measured 2026-09-29: a SIGKILLed `chat
		// --tools` resumed without the flag replied with a description of the
		// tool call it could not make instead of the tool's result).
		//
		// The warning is EMITTED AFTER the recorded options are applied (below),
		// never here — see chatWarnSeededToolsMismatch for why the order is the
		// difference between a warning and a lie.
		seededCkpt = ckpt
		// AUTODETECT, for the positional spelling only: the operator who types
		// `gopi chat <session-id>` is asking to continue that conversation, and
		// the options it was started with are part of what it was. Nothing else
		// on the command line said otherwise (that is the condition this branch
		// tests), so the record fills in what silence left out.
		//
		// The switch (`--resume`) does NOT come here: it is handled below,
		// AUTODETECT, for the positional spelling only: the operator who types
		// `gopi chat <session-id>` is asking to continue that conversation, and
		// the options it was started with are part of what it was. Nothing on
		// the command line stated one (that is the condition this branch
		// tests), so the record fills in what the silence left out.
		//
		// The switch (`--resume`) does NOT come here: it is handled below,
		// unconditionally, because there the operator ASKED for the recorded
		// options and silence about one of them is not a reason to drop it.
		if !resumed && !chatStatedOptions(args) {
			got, code, applyErr := chatApplyRecordedResumeOptions(sess, &noEditor, &noColor, &verbose, &dumpOpts, &sgrOpts, sgrFlagStated,
				seed, rawLogPath, false)
			if applyErr != nil {
				fmt.Fprintf(os.Stderr, "%s%v\n", chatErrorPrefix, applyErr)
				return code
			}
			inherited = append(inherited, got...)
		}
	}
	if resumed {
		// The switch states the intent the positional seed only infers: restore
		// the OPTIONS this session was started with (root cause of the whole
		// feature — a checkpoint restores a conversation, never a command line).
		got, code, applyErr := chatApplyRecordedResumeOptions(sess, &noEditor, &noColor, &verbose, &dumpOpts, &sgrOpts, sgrFlagStated,
			seed, rawLogPath, true)
		if applyErr != nil {
			fmt.Fprintf(os.Stderr, "%s%v\n", chatErrorPrefix, applyErr)
			return code
		}
		inherited = append(inherited, got...)
	}
	if replayTarget != "" {
		// THIS RUN IS A REPLAY, AND THAT — NOT "this run is a film's producer" —
		// IS WHERE ITS OWN DIAGNOSTICS GO (chat_notice_sink.go carries the
		// measurement). The route is installed HERE, before the first sentence is
		// spoken: startReplay announces three of them (the session it re-runs, the
		// stub the model is bound to, the injection budget) and the refusal
		// catalogue, so a route installed after it would be a route that missed
		// the very lines the operator sees first.
		//
		// A FILMED PRODUCER RE-ROUTES LATER, and that is not a second rule: when
		// the beat protocol is armed the run has a session record to write beside,
		// so the console.log route below takes over (it is a route, and a route
		// that finds no session record now leaves THIS one in place rather than
		// falling back to the screen). A replay that films nothing has no such
		// block and keeps this destination for its whole life.
		//
		// A destination that cannot be opened KEEPS THE SCREEN and says so
		// (Rule 15: a silent fallback is indistinguishable from a run that had
		// nothing to report).
		if npath, nrestore, nerr := routeChatNoticesForReplay(); nerr != nil {
			fmt.Fprintf(os.Stderr, "[gopi] chat: this replay's driver notices cannot be recorded (%v) — they will be written to the screen that draws the recording\n", nerr)
		} else {
			defer nrestore()
			if verbose {
				fmt.Fprintf(os.Stderr, "[gopi] driver notices: %s (this run re-runs a recording — its own diagnostics are kept off the screen it draws)\n", npath)
			}
		}
		// The replay's own setup, before the agent exists: the recording is
		// parsed, the in-process stub is listening, and the process's base URL
		// already points at it — so the ONE client this run builds (below) talks
		// to the recording's model and no provider is contacted (chat_replay.go).
		rp, rpCleanup, rpErr := startReplay(replayTarget)
		if rpErr == nil && fromTurnSet {
			// THE ORDINAL IS CHECKED AGAINST THE RECORDING, not against the
			// flag's own syntax: 1..N are meaningful only up to the number of
			// turns this session holds. A number past the end is REFUSED by
			// name rather than clamped — a silent clamp would play the whole
			// recording and report that it began at turn 99.
			if fromTurn > rp.turns {
				rpCleanup()
				fmt.Fprintf(os.Stderr, "%s%s %d names no turn in this recording — it holds %d operator turn(s)\n",
					chatErrorPrefix, chatFlagFromTurn, fromTurn, rp.turns)
				return chatExitFailed
			}
			rp.fromTurn = fromTurn
			// THE WITNESS THE PREFIX CONSULTS IS THE REPL'S OWN COUNTER, handed
			// over HERE — before the driver's goroutine starts — so the first
			// prefix turn already has somewhere to look.
			rp.witness = sess.witness
			chatNoticef(chatReplayFromTurnNotice, fromTurn, rp.turns, fromTurn)
		}
		if rpErr != nil {
			fmt.Fprintf(os.Stderr, "gopi chat: %v\n", rpErr)
			return chatExitFailed
		}
		replay = rp
		defer rpCleanup()
		// THE RECORDED TERMINAL SIZE BECOMES THIS RUN'S CANVAS
		// (chat_replay_geometry.go): a session happened at a size, the
		// recording now carries it, and a re-run that drew at the built-in ideal
		// instead would be a picture of a session that never happened. An
		// explicit --chat-ideal-geometry wins, and either way the source of the
		// number is printed — a canvas chosen by the recording and one chosen by
		// a fallback are different facts about the same picture (Rule 15).
		chatReplayApplyRecordedGeometry(replay, chatNoticeWriter())
		// The recorded options of the session being re-run are NOT inherited: a
		// replay reproduces the SURFACE, and the command line that produces the
		// surface is this run's own (a recorded `--no-editor` would ask for a
		// run that has no layout to film).
		// THE ONE DECISION THIS VERB MAKES ABOUT THE SURFACE: the recorded
		// session's tool calls mean the recording was made with the tool-enabled
		// turn runner, so the re-run uses it too. Any OTHER option the recorded
		// run had is the operator's to state on this command line — the replay
		// reproduces the session, not the argv.
		if len(replay.rec.Results) > 0 {
			sess.toolsEnabled = true
			fmt.Fprint(chatNoticeWriter(), chatReplayToolsNotice)
		}
	}
	// A REPLAY RUN LANDS NOWHERE NEW — THERE IS NO SESSION-ROOT RELOCATION HERE
	// ANY MORE, and that absence is the change (epic: `gopi chat --replay` must
	// be really read-only).
	//
	// WHAT USED TO BE HERE. Until this row, a replay pointed the process's own
	// sessions root at a scratch tree (cmd/agent/chat_sessions_root.go, deleted)
	// so the run's discarded session did not flood `gopi session list`. The
	// relocation was never the fix, only the hiding: it moved the WRITE instead
	// of stopping it, and — measured on this repo's own history — it made "the
	// replay wrote into scratch" and "the replay wrote nothing" look the same to
	// anyone reading the operator's sessions tree.
	//
	// WHERE THE WRITE IS STOPPED NOW. At the two minters, by the process-wide
	// flag every write path already consults: internal/agent's
	// NewRawLoggerForRun and NewSessionLoggerForRun refuse when
	// replaymode.Active() and say so on stderr, so a replay run reaches this
	// point with no session directory, no .raw.log, no session.jsonl and no
	// metadata.json to place anywhere. The recording was resolved through the
	// operator's own sessions root (above) and stays in place, read only.
	//
	// The `--scratch` flag keeps its OTHER job — the beat protocol's tree for a
	// `--film` run (chat_film.go) — and is deliberately no longer consulted
	// here: a flag that could move a session tree would be the escape hatch
	// Rule 35 refuses, and there is no tree left to move.

	// THE RUN'S COLOUR CAPABILITY, RESOLVED ONCE — AND RESOLVED HERE, ABOVE THE
	// START-UP NOTICES, because three of them are rows too.
	//
	// This is ONE decision, moved earlier, not a second one. Until this change the
	// resolution sat below (beside the editor) and the three start-up writers
	// (the session-budget default, the slot registration, the policy line) were
	// plain text because no table existed yet at their point in the flow. Hoisting
	// the SAME call and deriving the SAME table (termToolStylesFor) from it lets
	// them draw as termRow values without any of them asking "do I have colour?" —
	// a second capability decision is the measured defect (task 01a10e2b: three
	// prints for one decision), and the fix is a value handed DOWN, never a
	// re-resolution.
	//
	// THE ORDER OF THE THREE NOTICES AND OF THE OTHER START-UP LINES IS UNCHANGED,
	// and so is their stream: each writer keeps the stream it always had.
	//
	// IT DOES NOT SPEAK, for the reason stated at its old site: this is the FIRST
	// stage of a decision the input layer finishes (newChatInputWithPalette asks
	// the terminal and re-derives the table from its answer), so the sentence this
	// stage owes is not the run's sentence. The holder below says the ONE that
	// survives.
	chatCap := lineedit.CapabilityForEnv(noColor, chatLayoutSink())
	chatToolStyles := termToolStylesFor(chatCap.Colour())

	// THE POLICY STATE IS RESOLVED AND PRINTED ONCE, HERE, beside the other
	// startup notices: the posture (with the channel that decided it) and, under
	// --verbose, the rule count the project's GOPI.md contributed. Nothing below
	// consults either value — this run only REPORTS what a wired judgement would
	// do (chat_policy_flag.go states why).
	//
	// The line prints under --verbose OR whenever the operator NAMED a posture:
	// someone who typed `--policy yolo` must be able to see what it resolved to
	// without knowing a second flag, and that echo is also the only evidence the
	// value was not swallowed by the argv walk.
	chatAnnounceChatPolicy(policyFlagValue, verbose || policyFlagSet, chatToolStyles)

	chatWarnSeededToolsMismatch(seededCkpt, sess.toolsEnabled)
	if sess.toolsEnabled && verbose {
		fmt.Fprintf(os.Stderr, "[gopi] chat: tool-enabled mode (ops chat) — the model can read/edit files, run bash, search the web. /tools to toggle off.\n")
	}
	if sess.unattended && verbose {
		// The context is invisible in the default bytes (stderr is this face's
		// documented plumbing channel and stdout is the answer), so the ONE
		// place an operator can see which kind of run this is prints it here —
		// beside the tools notice, for the same reason: a mode that changes how
		// the model behaves must be observable without reading the raw log.
		fmt.Fprintf(os.Stderr, "[gopi] chat: UNATTENDED run — the prompt states that no human will answer, so the model chooses the conventions itself instead of stopping to ask. Decline it with %s for an ordinary single answer.\n",
			chatFlagAnswerAlone)
	}

	// THE LAST DOOR BEFORE ANYTHING IS MINTED (chat_stdin_guard.go carries the
	// measured defect this closes). Everything above is argv parsing and
	// recording; everything below builds the agent, and the agent's own two
	// minters (agent.NewRawLoggerForRun, agent.NewSessionLoggerForRun) are what
	// create the session directory, the .raw.log, the session.jsonl and the
	// exit marker. So this is the exact point where "the console cannot read a
	// single keystroke" is still free to refuse: a `gopi chat < /dev/null` run
	// exits non-zero here, names the three non-interactive carriers that DO
	// work, and leaves the operator's sessions tree byte-identical.
	if code, refused := chatStdinGuardRefusal(provided, replayTarget, resumeTarget, seed, filmSet); refused {
		return code
	}

	// THE CONFIGURATION IS RESOLVED HERE, AND IT IS THE RUN PATH'S OWN FUNCTION.
	//
	// WHY THIS LINE HAD TO EXIST (measured 2026-11-12 on this host, evidence in
	// the run's session directory): `gopi chat` never called resolveRunConfig,
	// so config.ApplyEnv never ran on this path and NO settings.json knob
	// reached the process environment. `GOPI_REASONING_EFFORT`,
	// `GOPI_THINKING`, `GOPI_THINKING_BUDGET`, `ANTHROPIC_MODEL`/`DEEPSEEK_MODEL`,
	// `ANTHROPIC_BASE_URL` — none of them, on a machine whose settings.json says
	// `"reasoning_effort": "high"` and `"thinking_budget": 16384`. The request
	// assembler (internal/agent/loop.go) reads those from the ENVIRONMENT only,
	// so every chat turn went out with thinking=disabled and no effort, while a
	// bare-prompt run of the same binary sent the configured values. The cause
	// is structural, not a typo: `main()` resolves the configuration AFTER the
	// subcommand chain, and `isChatSubcommand` EXITS from inside that chain —
	// this was the only face that could reach an agent without passing it.
	//
	// WHY THE POSITION IS THIS ONE. It is AFTER the argv walk (so `-h`, every
	// refusal, `--film` and `--screen-replay` have already returned — a usage
	// page must not mutate the process environment, and a playback needs no
	// provider) and BEFORE the agent is built (so the system prompt, the client
	// and every request the run assembles see the resolved values). The one
	// thing it does NOT precede is chat's own capability resolution, which reads
	// the terminal, not settings.
	//
	// WHAT IT DELIBERATELY DOES NOT DO: it does not re-implement the precedence
	// (flag > env > settings > default) or the five steps — those have ONE home
	// (run_config_resolution.go) and this is a call into it. See
	// chat_config_resolution.go for the argv reduction a verb face needs and for
	// why a failure here speaks in chat's voice rather than printing the
	// top-level usage page.
	if code := resolveChatRunConfig(args); code != chatExitOK {
		return code
	}

	// THE CHAT SURFACE'S OWN DEFAULT FOR THE SESSION BUDGET, published HERE:
	// after the command line is resolved (so `-h`, refusals and `--film` have
	// already returned and a usage query changes nothing) and before the agent —
	// and therefore the system prompt — exists. It is the ONE call every chat
	// carrier passes through (interactive REPL, `--once`, the scripted sequence,
	// the pipe-back driver), so no face can forget it; the rule and the argument
	// are in chat_session_budget.go. The table is the run's ONE capability
	// decision (resolved above), handed down so this writer resolves nothing.
	publishChatSessionBudgetDefault(chatToolStyles)

	// ONE agent for the whole chat session (the Epic's single-session ask):
	// created here and reused across every turn so the run has exactly ONE
	// session id, ONE session JSONL and ONE raw log — each turn *appends* its
	// user/assistant/request events to the same files rather than spawning a
	// fresh session id + log per turn (the pre-Epic behavior). The session
	// stays open (PersistChatSession) until chat exit, when CloseChatSession
	// writes the single session_end.
	// The REPL drives chatAgentFactory (declared in chat_boundary.go), which is
	// newAgent by default and a stub under test: the boundary's payload gates
	// must be asserted against the message a REAL turn sends, so the test needs
	// the one seam every other face already has (termOnceAgentFactory,
	// webServer.newWebAgent).
	buildAgent := newAgent
	if chatAgentFactory != nil {
		buildAgent = chatAgentFactory
	}
	// A RESUMED RUN BUILDS THE SAME AGENT, UNDER THE SESSION IT CONTINUES (epic:
	// `gopi chat --resume <id>` minted a second session). This is the line where
	// the identity reaches the mint: everything below — the session directory,
	// the raw log, the JSONL, every turn's rows — is derived from the id the
	// agent was built with, so continuing the id is what makes the resumed
	// conversation GROW instead of leaving it untouched beside a new directory.
	//
	// The test seam (chatAgentFactory) is untouched by design: a stub agent is a
	// stub agent under any identity, and the identity path is driven for real by
	// the binary itself (chat_resume_single_session_e2e_test.go).
	var ag *agent.Agent
	var err error
	if continuedSessionID != "" {
		ag, err = newAgentResuming("", agent.RunIdentity{
			SessionID:      continuedSessionID,
			ResumedFrom:    continuedSessionID,
			RawLogPath:     rawLogPath,
			SessionLogPath: continuedSessionLog,
		})
	} else {
		ag, err = buildAgent("")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		return chatExitFailed
	}
	// THE REPLAY'S EXECUTOR SEAM, installed the instant the agent exists and
	// before any turn can run: from here on, every tool call this run makes is
	// answered from the recording (chat_replay.go). Nothing about the surface
	// changes — the tool ROW, the tool event, the round structure and the answer
	// band all run exactly as they do for a live session; only the result's
	// origin does.
	if replay != nil {
		fmt.Fprint(chatNoticeWriter(), installReplayToolExecutor(ag, replay))
		// THE RUN HAS NO SESSION RECORD, AND THAT IS THE DELIVERABLE — say so
		// here, where both facts are in hand (this is a replay, and the mint
		// refused).
		//
		// WHY THE ANNOUNCEMENT IS HERE AND NOT ONLY IN THE MINTER: the minter's
		// sentence names the FILE it did not open; this one names the
		// CONSEQUENCE for the operator, who otherwise looks at a console that
		// prints no session path and cannot tell "this run keeps no record
		// because it is a replay" from "the record failed to open". The paths
		// that a normal chat prints (printLogPathsQuiet) are silent for this
		// run; one sentence covers all of them (Rule 15).
		if ag.SessionLog == nil {
			fmt.Fprintf(chatNoticeWriter(), chatReplayNoSessionNotice, replay.rec.Path)
		}
	}
	// WHERE THIS SESSION'S WRITES LAND (epic
	// chat-session-ground-retired-when-not-a-row): the chat ground is REPORTED,
	// never minted. A chat is not a ledger row, so a worktree it created could
	// be collected by no arm — the `chat/*` refs this machine still carries are
	// exactly that residue (devdoc/chat-session-ground-lifecycle-diagnosis.md).
	// The session therefore runs in the caller's own directory, and this call's
	// only effect is one stderr line saying so — in a repository's main checkout
	// that line names the checkout, so nobody believes a branch of their own
	// exists.
	//
	// WHY THE REPORT CANNOT FAIL THE SESSION: every unresolvable shape is a
	// NAMED degradation (tools.ChatGroundFor never returns an error), because a
	// chat is a human's session and taking it away — for a directory that cannot
	// be resolved — would remove the thing that was asked for. The degraded run
	// prints its reason on stderr, so the operator is never left believing a
	// worktree exists (Rule 15).
	if report, gerr := chatGroundReport(ag); gerr != nil {
		fmt.Fprintf(os.Stderr, "[gopi] chat: %v\n", gerr)
	} else {
		sess.ground = report
	}
	ag.PersistChatSession = true
	// THE UNATTENDED CONTEXT, armed by the face rather than by the prompt
	// (Rule 3: no new tool, no new mechanism — a bit of context only). A
	// `gopi chat --once` run is UNATTENDED BY CONTRACT: its answer is consumed
	// by a script or an agent, i.e. by nobody who can reply, so the run is told
	// that nobody will answer and that it may choose the conventions itself
	// (chat_unattended.go states the measured A/B the default comes from).
	// `--answer-alone` states the other case — one question, one answer, with
	// the ordinary interactive prompt — and every other carrier leaves this
	// false, so the interactive console's own prompt is untouched.
	sess.unattended = provided != nil && !answerAlone
	ag.Unattended = sess.unattended
	// THE SAME FACT, PUBLISHED WHERE THE DECISION IS MADE (Rule 2: one fact,
	// two readers). agent.Agent.Unattended branches the PROMPT's context; the
	// write boundary lives in internal/tools, which cannot see the Agent — and
	// a `prompt` decision's text must not tell a run with no operator to "ask
	// the operator and wait" (internal/tools/chat_operator_presence.go states
	// the measured deadlock). So the ONE place that knows the run's contract
	// publishes it here, beside the assignment above, and the two readings can
	// never disagree.
	tools.SetChatOperatorAbsent(sess.unattended)
	// RECORD THE OPTIONS THIS RUN CAN BE RESUMED WITH (sessionevent.
	// EventTypeResumeOptions). Written ONCE here, beside the session's own
	// start, and synced: the record has to be on disk BEFORE the first turn,
	// because the run this exists for is the one that dies mid-turn. The flags
	// are what the walk above ACCEPTED (recordable + the seed), never a
	// re-parse of os.Args — what is recorded is what this run actually got.
	chatRecordResumeOptions(ag, chatRecordableFlags(recordable, inherited))
	printLogPathsQuiet(ag, !verbose)
	// The message boundary's spill target (chat_boundary.go): the same session
	// directory the banner just named, resolved through chatSessionDir
	// (lineedit_wire.go) — the SAME function the editor's own paste store is
	// built from below. One derivation, so the directory the user is shown, the
	// directory the editor's spills land in and the directory the boundary's
	// spills land in cannot disagree.
	sess.sessionDir = chatSessionDir(ag.SessionLog)
	// THE TURN ORDINAL'S AUTHORITY, wired at the same seam and for the same
	// reason: every band this run draws is numbered from the session RECORD's
	// own turn axis (see chatTurnDisplay — the number `session log --turns`
	// prints and `session replay --turn N` selects for that same row), and the
	// record's writer is the only thing that can answer it before the row has
	// been flushed. A nil logger (replaymode, or a refused record) leaves this
	// nil and the numbering falls back to the REPL's own count.
	if ag.SessionLog != nil {
		sess.turnWriter = ag.SessionLog
	}
	// THE SAME ANCHOR FOR THE RECORD, wired at the same seam. The screen's turn
	// ordinal and the number a `[IMPORTANT]` note is stamped with (noteTurnNumber,
	// selfrecord_noteindex.go) are ONE axis, so a resumed chat that numbers its
	// first band N+1 must stamp its first note t(N+1) — not t1. A negative
	// baseline cannot come from the reader (a count), so it needs no clamp; a run
	// with no record keeps the zero value, which is the ordinary fresh case.
	ag.TurnBaseline = sess.turnBaseline
	// THE DEATH CERTIFICATE IS ARMED HERE — the last moment before this run
	// takes the terminal, and the first moment the session directory exists to
	// put it in (chat_exit_marker.go states the measured case it answers:
	// session 8616b46c stopped writing at 08:04:38 with no session_end, no
	// provenance record and no error-path dump, so nothing in the directory
	// could say WHICH kind of death it was).
	//
	// It is armed for every carrier that reaches this point — the interactive
	// REPL, `--once`, a scripted sequence — because the question it answers is
	// about the PROCESS, not about how a human drove it. A `--replay` run has no
	// session directory at all (the minter refuses one), so the call returns
	// nil and every use below stays a no-op.
	//
	// A failure to arm is SAID OUT LOUD and never stops the run (Rule 15b): the
	// session is what the operator asked for, and a chat that refused to start
	// because its certificate could not be opened would trade a diagnosability
	// gap for a broken tool. What the operator learns here is that the next
	// death will be undecidable — the one fact worth having BEFORE it happens.
	if mark, err := armChatExitMarker(sess.sessionDir, chatSessionID(ag)); err != nil {
		fmt.Fprintf(os.Stderr, chatExitMarkerArmFailedNotice, err)
	} else {
		sess.exitMark = mark
		// The HONEST `live` phase: armed, and only a returning run may advance
		// it. A path that returns without finishChat (a pre-REPL refusal after
		// the agent existed) marks itself, so `live` at read time means exactly
		// one thing — this process never returned at all.
		defer func() {
			if err := sess.exitMark.returnedWithoutCloseout(); err != nil {
				fmt.Fprintf(os.Stderr, chatExitMarkerWriteFailedNotice, err)
			}
		}()
	}
	// The replay driver rides the session, so the ONE closeout (finishChat) can
	// join it and report what it served — every exit path already goes through
	// that function.
	sess.replay = replay

	// SIGINT/SIGTERM → graceful single-session closeout (cli_web bug003
	// pattern, chat-once/bare-prompt zombie fix): the signal fires
	// ag.Interrupt() (ctx cancellation — an in-flight turn unwinds through
	// its session_end defer) and closes interruptCh so the REPL's prompt
	// select wakes and breaks to the SAME finishChat closeout as /quit.
	// Without this a kill mid-turn or Ctrl+C at the prompt killed the
	// process outright — session_end never written, the CHATS ledger row
	// left in_progress forever.
	sigCh := make(chan os.Signal, chatSignalBuf)
	signal.Notify(sigCh, chatGracefulSignals()...)
	defer signal.Stop(sigCh)
	interruptCh := make(chan struct{}) // closed exactly once on the first signal
	// The watcher is JOINED on the way out, and the join is the reason for the
	// done channel: without it a run that ends NORMALLY (no signal ever arrives)
	// leaves this goroutine parked on sigCh forever. That is a real leak, not a
	// reporting artefact — the suite's close-out sweep counts parked repository
	// goroutines and named this frame (cmd/agent/suite_stall_watchdog_test.go:
	// "parked repository goroutines: N" naming runChatWithInput.func1), which is
	// exactly what the sweep exists to catch. The `if !ok` branch below was
	// already written for a closed channel; nothing closed one.
	watchDone := make(chan struct{})
	watchStop := make(chan struct{})
	go func() {
		defer close(watchDone)
		// A LOOP, because a claimed per-turn interrupt does NOT end the
		// watcher's job: the session is still open and the next Ctrl+C must be
		// seen as well. Without the loop, the first interrupt consumed the
		// registration's only delivery and the operator's next Ctrl+C at the
		// prompt would do nothing at all (Rule 15 — a key that silently stops
		// working is worse than one with no feature behind it).
		for {
			select {
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				// WHAT THE SIGNAL MEANS IS DECIDED ONCE, HERE (task 01a0e4390).
				// signal.Notify is registered above for the whole run, so this
				// watcher owns SIGINT — which is exactly why signalrec leaves this
				// mode alone (modesThatOwnTheirSignalHandlers: "chat") instead of
				// re-raising. A turn in flight makes Ctrl+C the operator's per-turn
				// interrupt: the turn is abandoned and the session stays open. With
				// no turn in flight the signal keeps its historic meaning — the
				// graceful single-session closeout (bug003 / the chat-once zombie
				// fix), which is what Ctrl+C at the prompt has always done.
				//
				// SIGINT ONLY. The key and the signal are the same intent, but a
				// SIGTERM is not: its meaning is "terminate this process" and
				// nothing about it says which turn the operator was reading. So a
				// signal that is not os.Interrupt never claims a turn — it takes
				// the historic closeout below, mid-turn or not (the run unwinds
				// through Chat's ctx and writes session_end, which is what the
				// SIGTERM tests assert).
				if sig == os.Interrupt && claimTurnInterrupt(ag, sess, lineedit.OriginSignal) {
					fmt.Fprintf(os.Stderr, "\n%s\n", chatInterruptMarkerFor(chatInterruptReason(lineedit.OriginSignal), ag.PartialAnswerRunes()))
					continue
				}
				// THE NOTICE RIDES THE LAYOUT'S OWN WRITER, never a bare
				// Fprintf. This is the very defect the interrupt marker one
				// screenful below already names, and this call site was missed
				// by that fix: with the reserved layout installed, stderr is the
				// same descriptor the editor paints on and the cursor is parked
				// on the status row, so a flowed stderr write lands ON it.
				// Measured (live chat c5f1189a, 117x26, 2026-09-29): the
				// closeout notice overwrote the status row's first 53 cells —
				// columns 0..58 of
				//   `deepseek-flash ·  iter 13 ·  no tool · hit 97% … · ready`
				// came back as 5 surviving cells plus the notice, i.e.
				//   `deeps[gopi] terminated received — finalizing chat session…`
				// followed by the row's own tail from column 58. WriteNotice
				// gives the row to the ONE writer that owns the screen, and
				// keeps today's bytes exactly when no layout is installed (a
				// cooked run, --no-editor, a pipe) — which is why the
				// testsuite's "finalizing" greps still see it.
				chatSignalCloseoutNotice(input, sig)
				// THE CAUSE IS DECLARED BEFORE THE UNWIND (measured defect,
				// session ab9e0023, 2026-10-21). The close-out that runs next
				// collects a failure record whenever the exit code is non-zero,
				// and every one of that collector's arms keys on a RUN ERROR — a
				// signal death has none, so the record fell through to the
				// unknown default and the session's own dump read
				// `category: unknown` beside an orderly, deliberate close-out.
				// That is the "disappeared with no explanation" report: the one
				// fact an operator needs afterwards — WHICH signal ended this
				// run — was the one fact nothing recorded.
				//
				// Set here, on the goroutine that decoded the signal, because
				// this is the only moment it is known; the close-out reads it
				// later and internal/agent never re-spells a signal name (the
				// spelling is signalrec's, Rule 14).
				ag.CloseSignal = signalrec.SignalName(sig)
				ag.Interrupt()
				close(interruptCh)
				return
			case <-watchStop:
				return
			}
		}
	}()
	// The watcher is JOINED on the way out. It has to be stopped EXPLICITLY:
	// signal.Stop does not close the channel it deregisters (it only stops
	// relaying), so a `<-sigCh` alone would park this goroutine forever on a run
	// that ends normally — a real leak, and the one the suite's close-out sweep
	// counts and names (cmd/agent/suite_stall_watchdog_test.go: "parked
	// repository goroutines: N" naming runChatWithInput.func1). Hence the
	// select above and the stop channel here: Stop first (no new signal can be
	// relayed), then release the watcher, then join it. Every REPL exit path is
	// a return, so this always runs.
	defer func() {
		signal.Stop(sigCh)
		close(watchStop)
		<-watchDone
	}()

	// sessionStart tracks the overall chat wall-time for the final
	// session_end + task-ledger stats; iterCount/totalTokens accumulate
	// across turns for the same reason.
	sessionStart := time.Now()
	var iterCount, totalTokens int
	// operatorTurns is the DISPLAY counter: the turns of this run that the
	// session log will count as operator rows (chatCountsAsOperatorTurn). It is
	// the only counter the status row and the body heading are numbered from,
	// because the word they print is the log's word (see chatTurnDisplay).
	var operatorTurns int

	// The banner's own colours come from the SAME table the rest of the chrome
	// uses (resolved once, above): monochrome when colour is off, so a redirected
	// run is byte-identical to the historic output.
	//
	// THE capability decision was HOISTED to the start-up notices (see chatCap
	// there) — one resolution, earlier in the flow, because three start-up writers
	// now draw as rows too. What stays HERE is the palette derived from it: the
	// palette's downgrade notice is this stage's to speak (the input layer finishes
	// the decision), and announcing it anywhere else would move the line in the
	// stream the operator has always read it in.
	chatPal, chatPalNotice := lineedit.PaletteForCapabilityNoticed(chatCap)

	// The input source (M1.1–M1.3): a raw-mode line editor on a terminal, and
	// the cooked reader everywhere else. Editor availability is decided ONCE
	// here; the loop below does not branch on it. The cleanup restores the
	// terminal's full attribute snapshot and is deferred so every return path
	// (including a panic) restores it.
	// WHERE THE SELF-OBSERVATION GOES. Resolved BEFORE the layout takes the
	// operator's) and armed BEFORE the first frame, so the dump cannot describe
	// a screen the reader never saw. A resolution failure STOPS the run rather
	// than leaving an observer waiting for a file nothing will write (Rule 15).
	dumpPath, dumpErr := chatResolveDumpPath(dumpOpts, ag)
	if dumpErr != nil {
		fmt.Fprintf(os.Stderr, "gopi chat: %v\n", dumpErr)
		return chatExitFailed
	}
	dumpCols, dumpRows := chatTerminalGeometry(chatLayoutSink())
	// WHERE THE RAW BYTES GO. Same resolution shape, same failure rule (an
	// armed switch that cannot write must stop the run, not leave a run that
	// believes it is recording). Opened BEFORE the sink so the header's
	// geometry is the geometry the sink is built at.
	sgrPath, sgrErr := chatResolveSgrPath(sgrOpts, ag)
	if sgrErr != nil {
		fmt.Fprintf(os.Stderr, "gopi chat: %v\n", sgrErr)
		return chatExitFailed
	}
	sgrStream, sgrErr := chatOpenSgrStream(sgrPath, dumpCols, dumpRows)
	if sgrErr != nil {
		fmt.Fprintf(os.Stderr, "gopi chat: %v\n", sgrErr)
		return chatExitFailed
	}
	// THE LIVE STREAM IS REPL STATE (chat_screen.go): `/screen` captures from the
	// SAME handle the renderer is appending to, so the command cannot resolve a
	// second, possibly-different path and the capture cannot describe a file other
	// than the one this run is writing. Nil here means the switch is off, and the
	// command says exactly that.
	sess.sgr = sgrStream
	// THE STREAM IS FINALIZED FROM THE MOMENT IT EXISTS (bug 01a1b823-5070, measured
	// 2026-11-08). `SgrStream.Close` is what writes the close-out record, and it used
	// to be called ONLY from the big teardown below — which is registered ~200 lines
	// later, after the dump is armed and the startup screen is composed. Any return
	// in between (a dump that cannot be armed, a beat driver that refuses) therefore
	// left a stream with a header and no trailer, which is indistinguishable from a
	// run that died mid-write. A census of this host found five such files: 116 bytes,
	// header only, nothing else — the signature of exactly this window.
	//
	// It is registered HERE, before the code that can return, so every exit path
	// after the open writes its close-out record. Close is idempotent, so the
	// teardown's own call stays (it runs FIRST — defers are LIFO — which is what
	// keeps the report line reading a finished artifact).
	defer func() {
		if sgrStream == nil {
			return
		}
		if cerr := sgrStream.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "gopi chat: the raw screen stream could not be finalized: %v\n", cerr)
		}
	}()
	dumper, dumpErr := chatArmDump(dumpPath, dumpCols, dumpRows, sess.toolsEnabled, chatDumpFallbackNotice(), sgrStream)
	if dumpErr != nil {
		fmt.Fprintf(os.Stderr, "gopi chat: %v\n", dumpErr)
		return chatExitFailed
	}
	if dumpPath != "" {
		fmt.Fprint(os.Stderr, chatDumpPathNotice(dumpPath))
	}
	if sgrPath != "" {
		fmt.Fprintf(os.Stderr, chatSgrStreamNotice, sgrPath)
	} else if !sgrOpts.recorded() {
		// The decline is ANNOUNCED, not inferred from a missing file: the stream
		// is armed by default, so "off" is a decision this run made and the
		// caller is owed it (Rule 15). Without this line a run that failed to arm
		// for any other reason would look exactly like a run that declined.
		fmt.Fprintf(os.Stderr, chatSgrStreamOffNotice, chatNoSgrStreamFlag, uiclient.ScreenStreamSuffix)
	}
	if provided != nil {
		// The one-shot face supplies its own input layer (chat_once.go): a
		// cooked reader over the prompt, then EOF. Its dump sink is none — the
		// one-shot face never enters the editor, which is the only path that
		// paints an observed screen.
		//
		// NO EDITOR MEANS NO LATER STAGE: nothing will open a terminal and
		// overrule the table resolved above, so that resolution IS this run's
		// decision and the sentence it owes is owed here (Rule 15 — a cooked run
		// that silently served a table the operator did not ask for is exactly
		// the silent downgrade this notice exists to prevent).
		writePaletteNotice(chatPalNotice)
		input, closeInput = provided, func() {}
	} else {
		// chatPal, not a second resolution: the table was decided above and is
		// handed down with the CAPABILITY it came from (chatCap), so the builder
		// below derives the run's one sentence from the same decision rather than
		// from a second, parallel value that could disagree with it.
		input, closeInput = newChatInputWithPalette(os.Stdin, chatLayoutStream(), chatPrompt, noEditor, NewSlashCompleter(), dumper, chatCap)
		// THE MEASURED TABLE IS THE RUN'S TABLE. The editor probed the terminal
		// while opening it (lineedit's capability probe), so the table it will
		// draw with is the one the REPL must use for the rows it owns itself —
		// the banner below, the band's tool rows. Reading it back is what makes
		// the run have ONE colour decision instead of a declared one for the
		// non-editor surfaces and a measured one inside the layout.
		chatPal = input.Palette()
		// ...AND THE ARTIFACT MUST SAY SO. The screen dump explains the cells an
		// agent reads back, and the colours in them come from this decision, so
		// the evidence behind it rides the record (chatCapabilityReport). It is
		// recorded HERE, before the first paint: a record that gained the field
		// later would leave earlier frames attributing their colours to nothing.
		chatRecordCapability(dumper, input)
	}
	// LONG PASTES ARE SPILLED, not typed: the run's own session directory is where
	// the spill files live (<session dir>/paste/), so a pasted block leaves a
	// reference on the input line and the bytes in a file beside this run's
	// session log — the paste never has to fit in the editing buffer, and the
	// prompt never freezes re-laying-out it. The store is resolved from the
	// session logger (one owner of "where is this run's session directory"), and
	// a run whose session logging failed gets NO store: the paste is then typed
	// exactly as before, rather than filed somewhere nothing owns.
	input.SetPasteStore(chatPasteStore(ag.SessionLog))
	// THE STARTUP SCREEN rides the ONE carrier that can hold it: the answer band
	// (chat_banner.go carries the measurement — a startup print is pushed off the
	// screen by the layout that installs over it, while the band's rows go to
	// SCROLLBACK, which is where a startup screen belongs and where it stays
	// readable after the first answer scrolls past).
	//
	// It is drawn IN ADDITION to the one-liner above, never instead of it, and
	// when the band cannot carry it — the cooked/redirected path, a terminal too
	// narrow, a layout that never installed — nothing is drawn at all and the
	// one-liner's bytes are the whole of what this surface emits. That is the
	// design: ONE design with an optional screen, not two designs to keep in
	// step.
	// THE BEAT PROTOCOL, when a scratch was given, IS STARTED HERE — BEFORE THE
	// STARTUP SCREEN IS COMPOSED, and that order is the deliverable rather than a
	// detail. The screen's identity block is where this surface names ITSELF
	// (this run's session uuid, the directory it stands in), so it is exactly the
	// place a filmed run would print an identifying run into its first frames
	// unless the pass is already running when the screen is composed. The driver
	// needs two things it has by now and neither later: this run's line layer
	// (the sink the walk above installed) and the band's geometry — and the band
	// exists as soon as the editor does, so the geometry check below still reads
	// the layout the surface actually drew at, never the caller's word.
	//
	// A refusal (no sink, an unwritable scratch) STOPS THE RUN: the driver would
	// otherwise poll a directory nobody writes (Rule 15).
	beatDriver, berr := startChatReplayBeats(beatFlag, input)
	if berr != nil {
		if replay != nil {
			replay.err = berr
		}
		chatNoticef(chatReplayNotice+"%v\n", berr)
		return 1
	}
	if beatDriver != nil {
		sess.beats = beatDriver
		// THE BEAT PROTOCOL IS INSTALLED IFF SOMETHING OUTSIDE IS READING THIS
		// RUN'S BEATS — i.e. this process is a FILM's producer — so this is the
		// one place that knows the screen is being PHOTOGRAPHED rather than merely
		// watched. The driver's own notices (`[gopi replay] turn 2/4: …`, the
		// stub's script-exhausted sentence, the beat report) are then moved to the
		// session's console.log: they are diagnostics this run must keep, and a
		// bare write to the terminal the layout owns is photographed INTO the
		// deliverable (chat_notice_sink.go's header carries the measurement — the
		// sentences are readable inside the rejected GIF).
		//
		// A run whose notices cannot be recorded with its session keeps whatever
		// destination it already had — the replay route above installed one, so a
		// failure here degrades to a FILE rather than to the photographed screen —
		// and it SAYS SO (Rule 15: a silent fallback would look exactly like a run
		// that had nothing to report).
		if npath, nrestore, nerr := routeChatNoticesToConsoleLog(ag); nerr != nil {
			fmt.Fprintf(os.Stderr, "[gopi] chat: driver notices cannot be recorded beside this session (%v) — they stay at this run's own notice destination instead of the recorded screen\n", nerr)
		} else if npath != "" {
			defer nrestore()
			if verbose {
				fmt.Fprintf(os.Stderr, "[gopi] driver notices: %s (this run is a film's producer — its own diagnostics are kept off the photographed screen)\n", npath)
			}
		}
		defer func() {
			if cerr := beatDriver.close(); cerr != nil {
				chatNoticef(chatReplayNotice+"%v\n", cerr)
			}
		}()
	}

	// THE HISTORIC ONE-LINER IS ALWAYS PRINTED, and it is printed HERE: before
	// the layout installs, so it is the FIRST thing in the stream, exactly as it
	// has always been. It is not merely a fallback kept for old readers — it is
	// the line that carries this surface's CONTRACT (what Ctrl+C does, how to
	// leave), and this surface's own e2e reads the pty stream for it BEFORE the
	// prompt (testsuite/chat_pty_interrupt_test.go waits for the exit sentence,
	// then for `you>`; a marker that arrived after the prompt would be consumed
	// by the prompt's own wait and never read). The startup SCREEN is drawn
	// IN ADDITION, into the band below: the two are not alternatives, because
	// they answer different readers — the stream carries the contract, the
	// screen carries it where a human is looking. The layout's first paint
	// pushes these rows off the screen, so the reader never sees them twice.
	fmt.Fprintf(chatLayoutStream(), "%s", chatPal.Banner.Wrap(chatBannerLine))
	if sess.toolsEnabled {
		fmt.Fprintf(chatLayoutStream(), "%s", chatPal.Hint.Wrap(chatBannerToolsHint))
	}
	fmt.Fprintln(chatLayoutStream())

	// The chat-scoped tool (`chat_surface`) is wired HERE, after both halves it
	// needs exist: the screen this run paints (dumper, possibly nil = unarmed)
	// and the line layer that can carry a self-driven turn (input). Installing it
	// is what makes the tool's def on the chat wire ANSWERABLE — the wire
	// advertises the capability, and a capability with nothing behind it is a
	// tool that can only apologise. The unwire rides the same ONE teardown as the
	// dump, so a finished run cannot be read as a live surface.
	//
	// IT RETURNS THE PUBLISHED IDENTITY rather than printing a notice: the fact
	// belongs on the startup screen's side panel, not in the answer band
	// (chat_panel_notice.go carries the report and the design, and the notices
	// block below is what consumes it).
	uninstallChatSurface, surfaceIdentity := installChatSurface(ag, input, dumper, chatToolStyles, sess.noEventSlotWord)

	// THE RUN'S OWN START-UP NOTICES, GATHERED BEFORE THE SCREEN IS DRAWN. Every
	// line gathered here used to be committed into the ANSWER BAND — the area the
	// operator reads as the conversation — and the report is exact: those are tips
	// for the user, and the startup screen is where they belong
	// (chat_panel_notice.go carries the measurement, the design and the fallback).
	//
	// They are gathered BEFORE the composition because the screen is committed in
	// ONE WriteLines: the panel's surface fact and the box's own notice section
	// are arguments to that same call, so no notice can land ON the panel after it
	// was printed (the defect commit 6544dad5 fixed).
	//
	// THE KEYBOARD NEGOTIATION'S OUTCOME IS READ HERE, once: a terminal that
	// answered nothing does not report Shift+Enter as anything different from
	// Enter, so the answer has to reach the operator — as the panel's own newline
	// fact when a screen can state it, and as the historic sentence when none can.
	// Under --verbose the supported path states its result on stderr in both
	// cases: a negotiation nobody can observe is a negotiation nobody can debug
	// (see chat_keyboard_protocol.go).
	keyboardVerdict := chatAnnounceKeyboardProtocol(input, verbose)
	notices := chatStartupNotices(sess.toolsEnabled, dumper, surfaceIdentity, keyboardVerdict)

	// The screen itself: composed from this run's facts — with the section the
	// start-up notices are moved into — and committed into the band. An empty
	// result means the band cannot carry it, and then nothing is drawn: the
	// one-liner above is the whole of what this surface emitted, and every held
	// notice takes the historic stream instead (the fallback below).
	// THE STARTUP SCREEN GOES THROUGH THE REDACTION PASS TOO (chat_replay_redact.go).
	// It is the surface's own CHROME, and its identity block is the one place the
	// surface names itself: this run's session uuid and the directory it stands in
	// are drawn there, so a filmed run that masked only the recorded content would
	// still print an identifying run into the film's very first frames — and the
	// film's own checker greps the dumped grids, where the banner lands. The mask
	// is applied to the WHOLE composed screen, before WriteLines hands it to the
	// band, and it is length-preserving, so the panel's geometry stands exactly as
	// composed. A run without the beat protocol has no driver and gets its screen
	// back untouched (maskText is nil-safe).
	statedNotices := 0
	screenDrawn := false
	if composed, remaining := chatComposeStartupScreenWithNotices(ag, sess, input, chatPal, notices); composed != "" {
		if screen := sess.beats.maskText(composed); screen != "" {
			input.WriteLines(screen)
			statedNotices = len(notices.bannerNotices()) - len(remaining)
			screenDrawn = true
		}
	}
	// Whatever the box could not state is printed through the historic stream —
	// moved, never lost (Rule 15). A cooked/redirected run, or a terminal too
	// narrow to draw the box, reaches this with every notice still held, which is
	// what keeps that path's bytes what they were.
	if screenDrawn {
		chatPanelNoticesFallback(input, notices, statedNotices)
	} else {
		chatPanelNoticesStream(input, notices)
	}

	// ONE teardown, in the order the artifact needs: freeze the sink BEFORE the
	// layout is taken down (the erase, the scroll-region reset and the cursor
	// show describe the EXIT, not the screen a reader is about to quote), then
	// release the terminal, then report the artifact where the console can be
	// read again.
	defer func() {
		// chatDumpFinalize freezes the sink AND closes the RAW leg it carries
		// (uiclient.ScreenDumper.Close), which is what writes the stream's
		// trailer — so the report below reads a finished artifact.
		chatDumpFinalize(dumper)
		// THE NIL-SINK CASE. Close is what writes the raw stream's trailer, and
		// the sink owns that call — but a run that declined the SCREEN DUMP and
		// armed only the raw leg has no sink at all, so the stream is closed
		// here. SgrStream.Close is idempotent, so the ordinary shape (sink
		// present, trailer already written) pays one no-op call.
		if sgrStream != nil {
			if cerr := sgrStream.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "gopi chat: the raw screen stream could not be finalized: %v\n", cerr)
			}
		}
		closeInput()
		uninstallChatSurface()
		chatReportDump(dumper)
		chatReportSgrStream(sgrStream, sgrOpts)
	}()
	// The AGENT's own flow output (the live reasoning block, the `[gopi]`
	// notices, the agent's warnings) is routed through the SAME single writer
	// while the editor path is live — it used to reach os.Stderr directly and
	// tore the layout. Installed BEFORE the banner and the first turn, so no
	// earlier notice can slip past it on the editor path; the restore keeps the
	// seam a property of this run rather than of the agent.
	//
	// The seam owns BOTH destinations (Agent.OnThinking and the agent's
	// log.Logger), which is why the branch's separate diagnostics redirect is
	// not installed as well: two redirects of the logger would make "where did
	// the notice go" depend on which one ran last. The seam commits each notice
	// as its OWN whole row, which is strictly better than the branch's
	// console.log redirect for the notices — a row committed by the layout's
	// single writer cannot tear the layout, and it stays VISIBLE to the user
	// instead of being filed away.
	flowSeam, restoreFlowSeam := installChatFlowSeam(ag, input)
	defer restoreFlowSeam()

	// The run's session directory is where a notice is also RECORDED
	// (console.log, beside session.jsonl): a front run over a TTY has no
	// console record at all (consoleLogAttachFor deliberately refuses a TTY),
	// so the screen is the only place a notice would otherwise survive.
	//
	// BOTH destinations are composed into ONE writer rather than installed as
	// two competing redirects — the seam owns the screen and the file owns the
	// record, so a notice is visible AND durable, and "where did the notice go"
	// has one answer instead of depending on which redirect ran last.
	//
	// The branch's own reason for installing the redirect HERE still holds and
	// is why it is inside this branch and only when a band answers: the screen
	// is OWNED the moment the reserved layout is installed (the band and the
	// status row are painted at absolute rows and repainted, so the agent's own
	// diagnostics must not share stderr with them — chat_diagnostics.go carries
	// the measurement). With the band active they go to the session directory's
	// console.log — a front run over a TTY has no console record otherwise —
	// and the destination is named on the same line as the rest of the run's
	// plumbing, under --verbose.
	if input.Answer() != nil {
		// WHERE THE AGENT'S OWN NOTICES GO, and the ONE case in which the screen
		// is DENIED them: when this run is a FILM's producer (the beat protocol
		// has an outside reader), the screen is not a display the operator is
		// watching — it is the object being PHOTOGRAPHED, and a notice drawn into
		// the band's rows is a notice drawn INTO THE DELIVERABLE. Measured
		// 2026-10-04: `[gopi] chat table: chat row ensured: 8293257b-…` was
		// readable inside the frames of the GIF the operator rejected, and it
		// reached the band as a whole row through this very seam (the layout's own
		// `\e[24;1H\e[2K` precedes it in the producer's raw stream at byte offset
		// 115131 — the layout drew it, which is what makes the seam the right place
		// to cut).
		//
		// So a filmed run passes nil: the logger keeps its record (console.log,
		// beside session.jsonl) and stops being a painter. Every other run keeps
		// the seam, because there the row IS the operator's only sight of it.
		var noticeScreen io.Writer = flowSeam
		if sess.beats != nil {
			noticeScreen = nil
		}
		if path, rerr := routeChatDiagnostics(ag, noticeScreen); rerr != nil {
			fmt.Fprintf(os.Stderr, "[gopi] chat: diagnostics cannot be recorded (%v) — agent notices will be written to the screen and may disturb the layout\n", rerr)
		} else if path != "" {
			// THE EDITOR'S OWN DEGRADATIONS GO TO THE SAME RECORD. The redirect
			// above moves the AGENT'S logger; without this line the input
			// layer's sentences ("a band note was not drawn within 4s (the read
			// loop is stalled?)") still reached only os.Stderr, so the one
			// artifact a reader would grep to decide whether a stall happened
			// could never contain it (measured, task 01a18990 — Rule 15).
			input.RecordEditorDegradations(chatDiagnosticsFile)
			if verbose {
				fmt.Fprintf(os.Stderr, "[gopi] agent diagnostics: %s\n", path)
			}
		}
		defer closeChatDiagnostics(ag)
	}
	if verbose {
		if input.Mode() == chatInputEditor {
			fmt.Fprintf(os.Stderr, "[gopi] line editor active — Up/Down recall, Ctrl+A/E, Alt+Enter for a newline; a pasted block stays ONE message.\n")
			if input.Answer() != nil {
				fmt.Fprintf(os.Stderr, "[gopi] scrollback-safe output active — Type / for the command menu (Tab completes, Esc closes).\n")
			}
		}
	}

	// The one-line status (model / iteration / tokens) reads the REPL's own
	// counters: the model name from the agent, the turn counters this loop
	// already maintains, and the running token total. No new state is created
	// for it — the status is a VIEW of what exists. The counter updates are
	// published (see publishStatus below), never drawn: the editor's read loop
	// owns every terminal write.
	// The tools fact is installed as the surface's OWN state, read from the same
	// flag that selects the turn runner (Agent.ChatOps vs Agent.Chat) — so the row
	// cannot claim a mode the runner is not in, and the row states it ONCE (the
	// tool column's own field; operator bug #5, task 01a0e4c7).
	status := newChatStatusSource(ag.Model, sess.toolsEnabled)
	// The table the run resolved ONCE (chatPal, below) is handed to the source:
	// the status row's measured-quality tier (the cache hit rate's green / amber
	// / red) is resolved where the measurement is, and the table must be the run's
	// OWN — resolving a second one here would decide the capability twice and
	// print the downgrade notice twice into the terminal whose reader is reading
	// it (rule: ONE capability decision per run).
	status.setPalette(chatPal)
	// The operational read-outs come from the agent's own counters, refreshed on
	// every publish (see chatStatusSource.opsFn). Installing the SOURCE rather
	// than a value is what keeps the elapsed time and the context size live —
	// both are clock-driven, and a value captured here would freeze at startup.
	// [Carried over from the branch by the rework merge (task 01a0db73, 4c4b64a3
	// x master): the auto-merge dropped this comment with the hunk it rode on,
	// and it is the branch's own statement of WHY the source is installed. The
	// code below is unchanged.]
	// MERGE NOTE (rework run 01a0db73, resolving 4c4b64a3 x master): both
	// contributions are KEPT, and the ORDER is forced. turnStatus is master's
	// in-flight tracker: it owns the phase + the keepalive AND it is the status
	// accessor the input layer is handed (input.SetStatus(turnStatus.status)), so it
	// is built over the source. The branch's opsFn must be installed BEFORE that
	// accessor is handed over — the update and the ops refresh are one PUBLISH step
	// (the branch's status() reads s.ops), so an opsFn installed later would leave
	// the rows painted before it with a stale read-out. SetStatus is therefore the
	// LAST line rather than the branch's, and it names master's accessor: the
	// branch's accessor (status.status) and master's (turnStatus.status) are two
	// spellings of the SAME seam — both return source.status(), and master's is
	// source.status() reached through the tracker that owns the keepalive — so the
	// survivor is master's, and nothing about the branch's read-out is lost by it.
	// REBASE NOTE (delivery round 2/3, 01a198ab x 9c0ce255): both sides are KEPT
	// and the ORDER they were written in is the reason. Master's hunk changes the
	// CALL — statusOpsFor gains the session's cache base so the row's Σ means the
	// session — and this branch's hunk adds a STATEMENT BEFORE the call, which
	// records the chat wire's tool defs so the ctx number a resumed session paints
	// immediately counts what the server is about to bill. They are not two
	// answers to one question: the hunk boundary is an accident of both edits
	// landing in the same 8 lines. The setOpsFn line takes MASTER's three-argument
	// form (dropping it would silently restore the 81.5%-vs-86.03% defect), and
	// the RecordChatToolDefs block is kept verbatim (dropping it restores the
	// 36k→131k one-round-later defect). RecordChatToolDefs runs BEFORE the opsFn
	// is installed because an opsFn installed earlier would already have painted a
	// frame without the tool cost — the same ordering argument the MERGE NOTE
	// above makes for this seam.
	status.setOpsFn(func() statusOps { return statusOpsFor(ag, sessionStart, sess.costBase) })
	// The context's tool-cost read-out follows the CHAT wire: the defs the
	// runner will send are recorded HERE, before the first round, so the row a
	// resumed session paints immediately counts what the server is about to
	// bill for them. Without this the reading corrects itself only AFTER the
	// first response — which is exactly the defect (36k → 131k one round later).
	if sess.toolsEnabled {
		ag.RecordChatToolDefs()
	}
	// turnStatus owns the in-flight turn's STATE and the keepalive that repaints
	// it: the phase word (sent / thinking / responding) plus the animation frame
	// that makes each keepalive frame a REAL repaint (the renderer deduplicates
	// an unchanged row). It writes nothing itself — every change is published
	// through the editor's own seam — so the read loop stays the only writer.
	turnStatus := newChatTurnStatus(status, input.PublishStatus)
	// The ladder's input (defect fix, task 01a0dbc7): the width the aligned layout
	// may spend is a property of the TERMINAL, so the source asks the live band for
	// it at COMPOSE time instead of being handed a value at publish time. It used
	// to arrive only through publishChatStatus -> setAvailCols, a helper the live
	// loop stopped calling when this tracker took over the publish path, so the
	// composer read availCols == 0 on every terminal and fell back to the pinned
	// geometry — measured on the real pty at 100/95/80 columns, where no shed form
	// could ever survive the aligned-fit check. One hook, read where it is used.
	status.setViewportFunc(input.bandCols)
	input.SetStatus(turnStatus.status)
	// VOICE MODE (/voice, chat_voice.go). Built ONCE per session and BEFORE the
	// loop, because the state it owns outlives the line that starts it: the key
	// gate it installs at the editor's seam and the capture goroutine it starts
	// must survive the `/voice` line, and a mode rebuilt per line would leave
	// each invocation's gate behind. The RECORDER is the production wiring (the
	// same voiceListDevices / voiceResolveSelection / voiceRecord / transcribeFile
	// calls the bare verb makes); on the cooked path the mode exists but refuses
	// to start, because there is no input line for a transcript to be appended to
	// (it says exactly that — chat_voice.go's no-editor refusal).
	// THE RECORDER'S AUDIO SOURCE IS RESOLVED HERE, ONCE (chat_voice_audio.go):
	// the `--voice-audio FILE` flag when the run stated one, the environment
	// otherwise, and "" — the production capture — when neither spoke. The
	// resolution is the ONE reader of the two channels, so the flag and the env
	// cannot be read in two places and disagree.
	voiceMode := newChatVoiceMode(input, chatVoiceRecorder(chatVoiceAudioPath(voiceAudio)))
	// THE VOICE SEAM IS PLUGGED HERE (task 01a116b0): the status row asks the mode
	// for its live phase through the source's ONE read-out hook, so the row paints
	// "rec" / "stt" while `/voice` is on and nothing at all when it is not.
	// HONEST SCOPE: the status row is a member of this function's state and the
	// mode is built over the voiceChatSession seam (not over *chatInput), so the
	// hook is installed by the caller that owns BOTH. A caller that wires a voice
	// mode onto a session whose carrier is not this row's (the tests build their own
	// carriers) must plug the same function itself — there is no global registry,
	// by design: the row's fact has exactly one owner (Rule 3) and the wiring is
	// where the two objects meet.
	// The connection is made in THIS direction (the row holds a function, the mode
	// holds the state) because the mode is the fact's only owner — a second copy of
	// "is the microphone open" on the status source would be the drift this wiring
	// exists to prevent. It is set BEFORE the read loop starts, so no repaint can
	// compose a row against a seam that is not there yet.
	status.setVoiceFn(voiceMode.livePhase)
	// AND TAKEN DOWN ON EVERY EXIT. A session that ends with the mode on must stop
	// the recorder and give the key seam back — the deferred shutdown is the
	// SILENT half by design (no row is written at a moment the exit path is
	// erasing the screen and handing the terminal back), and it covers all of the
	// REPL's exits at once (/quit, /exit, Ctrl+D, a read error, a signal) rather
	// than one arm remembering to.
	defer voiceMode.shutdown()
	// TYPING-TIME MEMORY PREFETCH (chat_prefetch.go). Built and armed BEFORE the
	// loop, because the thing it watches is the line the operator is about to
	// type: it samples the editor's PUBLISHED snapshot on its own clock (no new
	// lineedit seam, and no I/O on the frame path — the read loop stays the only
	// writer) and leaves a record in this session's directory. With the knob OFF
	// chatPrefetchFor returns nil: no goroutine is started and the outbound
	// message below is byte-identical to the pre-knob tree.
	prefetch := chatPrefetchFor(sess.sessionDir, chatPrefetchLineSource(input), os.Stderr)
	if prefetch != nil {
		// The run's OWN cancellation, so the loop dies with the chat: /quit,
		// Ctrl+C and a signal-driven closeout all reach it through the same
		// deferred stop, and none of them needs a second lifecycle.
		prefetch.Start(context.Background())
		defer prefetch.Stop()
		if verbose {
			// The knob's own report (Rule 15): WHICH channel decided is not
			// decoration — "off" from a settings file and "off" because nobody
			// asked are the same value and two different facts, and only this
			// line tells them apart. The record's path is named for the same
			// reason: an observable nobody can find is not an observable.
			_, source := config.MemoryPrefetchSource()
			fmt.Fprintf(os.Stderr, "[gopi] memory prefetch: on (source: %s) — record: %s\n",
				source, filepath.Join(sess.sessionDir, config.MemoryPrefetchRecordName))
		}
	}
	// THE PER-TURN CTRL+C SEAM. Installed here, beside the status seam and for
	// the same reason: both are the line layer asking the REPL about state only
	// the REPL has. `turnRunning` is the predicate (Ctrl+C means "abandon the
	// turn" iff a turn is in flight), `claimTurnInterrupt` is the action and it
	// latches, so the key cannot cancel two turns however many transports deliver
	// it (see claimTurnInterrupt).
	input.SetInterrupt(func() bool { return turnRunning(sess) },
		func(origin lineedit.KeyOrigin) bool { return claimTurnInterrupt(ag, sess, origin) })
	if verbose && input.Mode() == chatInputEditor && !input.StatusNotifyEnabled() {
		// Rule 15: a publish that cannot wake the read loop means the status
		// row only refreshes when a keystroke does — say so rather than let
		// the user read a stale row and wonder.
		fmt.Fprintf(os.Stderr, "[gopi] the status row refreshes on the next keystroke (no wake channel on this input source).\n")
	}

	// The REPL's line read is hoisted into a feeder goroutine so the main
	// loop can SELECT on input OR an interrupt signal. A bare blocking read
	// would sit forever on Ctrl+C at the prompt (the signal handler runs, but
	// nothing wakes the read) — the process would hang instead of finalizing.
	// The feeder sends each line (or the EOF marker) on a channel; the loop
	// consumes one per iteration.
	type lineRead struct {
		line string
		// eof is the end-of-input signal. It is an explicit flag, NOT an empty
		// line: an empty line is legitimate input (the user pressed Enter, or
		// the editor returned an empty buffer) and treating it as EOF would end
		// the session on a stray newline.
		eof bool
		// endKind is the input layer's own word for WHICH end this was
		// (lineedit.EOFKindKey / EOFKindSource / chatEndKindInterrupt /
		// chatEndKindCookedEnd), carried through to the close-out so the
		// session_end records the event rather than only "the input ended"
		// (task 01a163f8).
		endKind string
		err     error
		// noticeTurn says WHICH ARM produced this read: true when the loop did
		// not have a line at all and opened a turn because the job-notice LEVEL
		// was true with the operator idle (epic 01a0eeea). It is the same kind
		// of fact as `selfDriven` one seam above — the read arm IS the evidence,
		// never the text — and it is what tells the loop, later, that this
		// iteration may open a turn with no operator message.
		noticeTurn bool
		// queueTurn says WHICH ARM produced this read, the same kind of fact as
		// noticeTurn: true when the line came OFF THE REPL'S OWN QUEUE — a prompt the
		// operator parked with `/queue <prompt>` and `chatQueue.Take()` handed to this
		// turn (chat_queue.go). It is set at the ONE place a line leaves that queue
		// and nowhere else, so "this turn's line was a parked one" is known by the arm
		// that produced it rather than inferred from the text — the parked prompt IS
		// the line, so there is nothing in the bytes to infer it from.
		//
		// It is the boundary's provenance input (chatSealSource, chat_boundary.go):
		// without it a `/queue`d prompt is stamped inputsource.Human and the field
		// lies about which producer made the turn.
		queueTurn bool
		// replayFace is the face a REPLAY driver recorded for this line
		// (chatInput.takeReplayFace), "" on every run that is not a replay.		//
		// IT IS POPPED IN THE FEEDER, at the read that produced the line, because
		// that is the ONE place the pop can be paired with the push: the driver
		// pushes one face per line it types, in order, and this goroutine receives
		// the lines in that same order. Reading it anywhere else — in the loop,
		// from the input layer — would let a reordering between the two goroutines
		// attach a face to the wrong turn.
		replayFace string
		// replayMoment is WHEN the recording says this line's turn opened, and
		// replayStamped says whether a replay driver queued one for THIS read at
		// all (`chatInput.takeReplayMoment`). Both are zero/false on every run that
		// is not a replay.
		//
		// THE TWO ARE SEPARATE FIELDS, and that is the whole point: `replayStamped
		// && replayMoment.IsZero()` is a RECORDED TURN WITH NO USABLE TIMESTAMP,
		// which must print the explicit placeholder — while `!replayStamped` is a
		// LIVE turn, which prints the moment the REPL mints for it. Collapsing
		// them into one field would make an old record's absent stamp fall through
		// to time.Now(), i.e. a replayed session claiming today (Rule 15).
		replayMoment  time.Time
		replayStamped bool
		// voiceRunes is HOW MANY RUNES OF THIS LINE CAME FROM DICTATION, measured
		// by the feeder the instant the line was read (chatInput.VoiceRunes) —
		// never inferred later from "the voice mode ran this turn".
		//
		// WHY IT IS MEASURED HERE, in the reader goroutine, and not in the loop:
		// the editing line is RESET at the start of the next read (Editor.Run
		// does it), so by the time the loop is doing anything else the buffer it
		// would measure belongs to the NEXT line. This is the one moment the
		// submitted line's own provenance is still the buffer's provenance, and
		// it is the same moment every other per-read fact (the replay face, the
		// replay moment) is taken, for the same reason.
		//
		// ZERO IS THE HONEST ANSWER ON THE COOKED PATH (no editor, so no line to
		// measure) and on any line the operator typed themselves.
		voiceRunes int
	}
	lineCh := make(chan lineRead, 1)
	// turnRunning tells the READER goroutine whether a turn is in flight, which
	// is the ONLY state in which it may park a `/queue <prompt>` line: with no
	// turn running there is nothing to not-interrupt, and the loop is at its
	// prompt to dispatch the line itself. It is one bool, written by the loop
	// (around the turn) and read by the reader, so it lives behind the queue's
	// The predicate is the REPL's OWN — `turnRunning(sess)`, the same one Ctrl+C's
	// per-turn claim consults — so "is a turn in flight?" has exactly ONE answer
	// shared by the key handler and the queue reader. A second flag here would
	// drift from it precisely when a keystroke and a key race, which is when both
	// are read.
	// wasTurn is the CROSS-ITERATION half of the same question: did the iteration
	// that just ended RAN a turn? A parked prompt becomes the NEXT TURN — one that
	// follows a turn — and only at a turn's end does "the queued prompt's turn"
	// mean anything. Without it, a `/queue X` typed at an idle `you>` would be
	// popped on the very next iteration and run at once, which is indistinguishable
	// from having typed X, makes the queue's own bound unreachable in the one state
	// where an operator would fill it, and takes away the chance to queue a few
	// prompts and then look at them. It is written on the loop goroutine only.
	wasTurn := false
	go func() {
		for {
			line, ok, kind, err := input.ReadLine()
			if err != nil {
				// A read error ends the feeder exactly like EOF: the loop
				// reports it through the same channel rather than printing
				// from a goroutine whose lifecycle we do not own.
				lineCh <- lineRead{err: err}
				return
			}
			// BLOCKING send — never drop. A drop-on-full select would lose
			// input that arrives while the loop is busy with a turn (the
			// 1-buffer is full), including the EOF marker: the loop would then
			// block forever on lineCh waiting for a line that was silently
			// discarded (testsuite two-turn + /quit hang). The loop is the ONLY
			// consumer, so a full channel only means the loop is mid-turn and
			// will drain it; a blocking send cannot deadlock. If a signal makes
			// the loop return, the process exits and this goroutine dies with it.
			// A `/queue <prompt>` read WHILE the loop is mid-turn is parked
			// HERE, by the one goroutine that sees it the instant it is typed.
			// The loop is blocked inside the turn it is running, so its own
			// `/queue` case cannot run until that turn ends — which is exactly
			// when parking the prompt has become useless. The prompt goes into
			// the SAME chatQueue the loop drains at the turn boundary, so no
			// second next-turn mechanism appears: the reader only fills the slot
			// the loop already reads. chat_queue.go's chatQueueReaderLine and
			// chatQueueParkedLines carry the design and the one-form-per-
			// goroutine restriction that keeps the console single-writer.
			if ok && parkQueueReaderLine(func() bool { return turnRunning(sess) }, sess, input, line) {
				continue
			}
			// THE RECORDED MOMENT, POPPED BESIDE THE FACE AND IN THE SAME ORDER
			// (chatInput.takeReplayMoment): a replay types one line per turn, so
			// the queue's order is this feeder's order. It is read HERE for the
			// reason the face is — this is the one place the pop can be paired
			// with the push, and a reordering between the two goroutines would
			// attach a moment to the wrong turn.
			replayAt, replayStamped := input.takeReplayMoment()
			lineCh <- lineRead{
				line:          line,
				eof:           !ok,
				endKind:       kind,
				voiceRunes:    input.VoiceRunes(),
				replayFace:    input.takeReplayFace(),
				replayMoment:  replayAt,
				replayStamped: replayStamped,
			}
			if !ok {
				return
			}
		}
	}()

	// THE REPLAY DRIVER STARTS HERE — after the surface has drawn its banner and
	// its startup rows, and before the first prompt is read, so the recording's
	// first turn is typed into a layout that is already installed and whose dump
	// is already armed. Starting it earlier would let the driver race the
	// layout's own first paint (a WILD screen); starting it at the agent's
	// construction would race the editor's existence.
	if sess.replay != nil {
		go sess.replay.run(input, sess.beats)
	}

	// A REPLAY THAT GAVE UP MUST NOT LEAVE THE LOOP WAITING, and the repr: below
	// carries what that sentence means. The channel is read from the driver (nil
	// for every non-replay run, and nil for a driver that ended CLEANLY — a
	// closed-but-never-read channel of a successful run is not a signal, it is
	// the same value), so the two arms below cost a non-replay run nothing and
	// cannot fire for a replay that typed its own /quit.
	var replayAbort <-chan struct{}
	if sess.replay != nil {
		replayAbort = sess.replay.abort
	}

	// THE IDLE INBOX POLLER (epic 01a10e14): a delivery made by ANOTHER process
	// (`gopi notify --session <id> --message <text>`) raises no in-process edge,
	// so the parked `select`s below would never learn that `theJobs.notices`
	// became non-empty — the message would sit on disk until the operator typed
	// something, which is the cycle this closes.
	//
	// The goroutine merges this session's own inbox into the ONE notice queue
	// every sessionInboxIdleLoadInterval while the console is idle, and signals
	// agent.SessionInboxIdleWake when that made anything pending. BOTH idle
	// `select`s below carry that arm, and a wake is only a NUDGE: the LEVEL
	// (tools.JobNoticePending, through chatNoticeTurnOwed) still decides whether
	// a turn is owed, so a stale token mints nothing.
	//
	// It is stopped with the session and joined on the way out: an abandoned
	// ticker goroutine is the leak the suite's close-out sweep counts and names,
	// and the channel it writes to is read by this loop alone.
	inboxStop := make(chan struct{})
	inboxDone := make(chan struct{})
	go func() {
		defer close(inboxDone)
		// BEFORE the first tick: a message delivered while this session was not
		// running at all is already in the file (that is what makes the delivery
		// durable), and it must mint a turn at the FIRST idle prompt rather than
		// at the first tick. The wake it raises is buffered, and the boundary
		// below also asks the LEVEL on the way in, so the token cannot be lost.
		ag.SessionInboxIdleLoad()
		ag.SessionInboxIdlePoll(inboxStop)
	}()
	defer func() {
		close(inboxStop)
		<-inboxDone
	}()

	for {
		// Signal closeout takes priority over a pending/typed line: once a
		// SIGINT/SIGTERM has been seen (interruptCh closed), the loop breaks
		// to the SAME finishChat closeout as /quit — session_end + CHATS row
		// never leak on Ctrl+C / kill.
		select {
		case <-interruptCh:
			return leaveChat(ag, sess, input, sessionStart, iterCount, totalTokens, 1, agent.EndCause{Reason: sessionevent.SessionEndReasonSignalled})
		default:
		}

		// The cooked path prints its own prompt (as it always has); the editor
		// draws its prompt as part of the editable line.
		if input.Mode() == chatInputCooked {
			fmt.Fprint(chatLayoutStream(), chatPrompt)
		} else {
			// The editor path MUST NOT reach the terminal through stdout: with
			// the reserved layout installed the terminal is inside a scrolling
			// region and the cursor sits on the row the editor last drew, so a
			// flowed write lands on the editing row and every LF scrolls the
			// band up by one over the caller's own text (the torn /help render
			// this closes). Erase the previous turn's trailer so the band never
			// keeps a row the user already read — an explicit blank line is
			// printed instead.
			input.WriteLines("\n")
		}
		// Back at the prompt: publish the counters the previous turn left and END
		// the in-flight state (the tracker stops its keepalive here, so no
		// animation survives the turn it described). The editor draws the row;
		// the publish only raises a flag and wakes it. The turn number is the
		// DISPLAY number (session baseline + this run's turns) — iterCount stays
		// the close-out's pass count and is never fed to the row (see turnBaseline).
		turnStatus.publish(status, sess.chatTurnDisplay(operatorTurns), statusLiveIdleTurn, totalTokens)
		// A SELF-DRIVEN line (the `chat_surface` tool's `type` action) is this
		// surface's own input, typed by the turn that just ended. It is consumed
		// HERE, which is what makes it a turn like any other: it goes through the
		// same prompt, the same renderer, the same tool rounds — the model is
		// never told to "continue" inside one turn, it is handed a line and
		// treated as what it is (the next thing the user says).
		//
		// The decision lives in the Agent (ChatSurfaceTakePending), not here: the
		// carrier CONSUMES the event, it does not make it — a second counter in
		// this loop is how a bound gets enforced against a number nobody spent.
		// What the carrier owes is the VISIBLE half: the loop says when it starts,
		// when it is on its last turn, and when it is over
		// (devdoc/runaway-loop-20260923.md: a self-driving seam whose stop nobody
		// can see is the incident).
		//
		// The second source at this SAME boundary is the OPERATOR's queue
		// (`/queue`, chat_queue.go): when the agent has nothing pending, a parked
		// prompt becomes the next turn. One seam, two sources, one consumer.
		//
		// THE ACCOUNTING IS DELIBERATELY DIFFERENT, and it is the one thing this
		// seam must not blur. A self-driven line is charged against the agent's
		// bound (ChatSurfaceTakePending spends it; the tool refuses a `type` past
		// it). A QUEUED line is charged against NOTHING: it is the operator typing
		// ahead, it spends no loop turn, and a parked prompt therefore runs even
		// on a session whose self-driven loop is already closed. Charging it to
		// the agent's counter would spend the model's budget on a person's
		// keystroke — and would refuse that person a prompt they typed for
		// themselves. The two budgets are two constants (chatSurfaceLoopBound,
		// chatQueueBound) for exactly this reason.
		// WHAT THE READER RECORDED WHILE THIS TURN RAN. A `/queue <prompt>` read
		// mid-turn was parked by the reader goroutine — the only place a line is
		// seen the instant it is typed — and its note row was RECORDED rather
		// than written, because the console has one writer and that writer is
		// this loop. Replaying it HERE, just before the boundary below, is what
		// makes a mid-turn park visible at the very moment its prompt becomes a
		// turn: the operator reads "queued #1 of 8", then "→ (queued) …".
		for _, note := range sess.queue.DrainNotes() {
			input.WriteNotice(note)
		}
		var lr lineRead
		// selfDriven records WHICH ARM supplied the line — the fact the renewal needs
		// and the only place it can be known. It is set by the seam's own branch below,
		// never inferred from the text (see the boundary comment further down). BOTH
		// halves are load-bearing and neither survives alone: the switch is what lets
		// the agent-pending arm fall through to the QUEUE (this feature's seam), and
		// selfDriven is what keeps the budget renewal off the model's own arm. A line
		// that came off this seam is the model's; every other line reached the REPL
		// through lineCh, the one path a line the model wrote cannot take.
		selfDriven := false
		turn := ag.ChatSurfaceTakePending()
		switch {
		case turn.Kind == agent.ChatSurfaceSeamTyped:
			selfDriven = true
			lr = lineRead{line: turn.Line}
			input.WriteNotice(fmt.Sprintf(chatSelfDrivenTurnNotice, turn.Used, turn.Bound))
			if turn.Closing {
				// The LAST turn announces both itself and the end, on one line: the
				// operator must not have to infer the bound from the silence that
				// follows it.
				input.WriteNotice(fmt.Sprintf(chatSelfDrivenClosingNotice, turn.Bound))
			}
		default:
			// THE NOTICE ARM (epic 01a0eeea): the read that reached this
			// boundary was produced by the LEVEL — no operator input at all —
			// so nothing here can collide with it. The self-driven seam's
			// pending line waits one iteration (nothing consumed: this arm does
			// not touch it), the queue is not touched, and the operator's
			// already-typed line wins as always, because a line in lineCh would
			// have been the value of lr instead of this arm.
			//
			// The message is composed downstream, next to the operator's own
			// line handling, for ONE reason: the notice is a turn's OPENING
			// message, and the opening message is the one thing every arm must
			// go through the same way.
			if lr.noticeTurn {
				break
			}
			if turn.Kind == agent.ChatSurfaceSeamClosed { // The loop is over — either it reached its bound or a `stop`
				// cancelled a line it had already typed. Either way the operator
				// reads it NOW, on the queued path too: a parked prompt must not
				// swallow the notice that the self-driven loop has ended.
				input.WriteNotice(fmt.Sprintf(chatSelfDrivenBoundNotice, turn.Used, turn.Bound))
			}
			// THE ORDER AT THIS BOUNDARY: a line the operator ALREADY TYPED wins
			// over a parked one, then the queue, then the blocking wait.
			//
			// Why the typed line wins (measured while writing the `/quit` leg of
			// this feature's own acceptance test): the queue is taken at the TOP
			// of every iteration, so an eager pop drains it before the loop ever
			// reads the next line — which makes the queue's closeout sentence
			// ("N parked prompts will be DROPPED") UNREACHABLE, and an unreachable
			// honest sentence is worse than none: the operator could never cancel
			// a parked prompt, and no test could show the sentence at all. With
			// this order, `/queue X` then `/quit` (both typed while a turn is in
			// flight) ends the session with X STILL PARKED and the session SAYING
			// so.
			//
			// It costs nothing the epic asked for: the parked prompt still becomes
			// the next turn with NO extra Enter — it is taken here the instant
			// nothing has been typed.
			// A parked prompt is taken ONLY at a turn boundary (wasTurn), and the
			// take is SKIPPED entirely — never evaluated and discarded — when this
			// iteration does not follow a turn. The distinction is the whole
			// difference between a queue and a leak: `Take()` MUTATES the queue, so
			// a take whose value is then thrown away destroys an operator's parked
			// prompt with nothing on screen to say so. Measured while writing this
			// feature's own bound case (`/queue fill-01` … `/queue fill-02` typed in
			// a row at an idle prompt): the discarded take left the queue one prompt
			// short each time, so `queued #1 of 1` was all the operator ever saw and
			// the ceiling could not be reached.
			// THE ORDER AT A TURN BOUNDARY — three questions, asked in this
			// order, and each answer is load-bearing:
			//
			//  1. IS THIS A BOUNDARY AT ALL (wasTurn)? A parked prompt becomes the
			//     NEXT TURN, so it is only taken after the iteration that RAN a
			//     turn. Without the guard, `/queue X` typed at an idle `you>` would
			//     be popped on the next iteration and run at once — indistinguish-
			//     able from having typed X — and the queue's own ceiling could
			//     never be reached in the one state where an operator would fill
			//     it. The guard must SKIP the take, never take-then-discard:
			//     Take() MUTATES the queue, so a take whose value is thrown away
			//     destroys a parked prompt with nothing on screen to say so.
			//  2. HAS THE OPERATOR ALREADY TYPED SOMETHING (the non-blocking
			//     receive)? A line in hand wins over a parked one, and this is what
			//     makes `/quit` able to cancel a queued prompt at all: with an
			//     eager pop, `/queue X` then `/quit` would run X first and the
			//     closeout sentence about a still-parked prompt would be
			//     unreachable.
			//  3. otherwise the queue hands the loop its next line, and only then
			//     does the loop block waiting for input.
			if wasTurn {
				select {
				case lr = <-lineCh:
				default:
					if queued, ok := sess.queue.Take(); ok {
						// queueTurn is the ARM'S OWN FACT, set at the ONE place
						// a prompt leaves the queue and read by the boundary
						// below to stamp provenance (chatSealSource). It is
						// recorded HERE rather than derived later because the
						// taken prompt IS the line: nothing downstream can tell
						// a parked turn from a typed one by looking at it.
						lr = lineRead{line: queued, queueTurn: true}
						// Announced where the operator reads, BEFORE it becomes
						// a turn: a parked prompt must never arrive as text
						// nobody can account for, and the note uses the same
						// arrow marker the self-driven turn does — ONE way to say
						// "this line was not typed just now".
						input.WriteNotice(fmt.Sprintf(chatQueuePopNote, queued))
						break
					}
					// THE LEVEL, ASKED ON THE WAY IN (epic 01a0eeea): a notice
					// that arrived BEFORE this select was reached must still
					// become a turn — waiting for the next edge would leave it
					// in the inbox until the operator typed something, which is
					// the cycle this arm exists to break.
					if chatNoticeTurnOwed() {
						lr = lineRead{noticeTurn: true}
						break
					}
					// …and the WAIT, with the wake as its third arm. A false
					// level after a wake means the token was stale (a round
					// boundary drained the inbox first), so the loop waits
					// again rather than minting a turn from nothing — see the
					// contract in internal/tools/job_notice_wake.go.
					for {
						select {
						case <-interruptCh:
							return leaveChat(ag, sess, input, sessionStart, iterCount, totalTokens, 1, agent.EndCause{Reason: sessionevent.SessionEndReasonSignalled})
						case <-replayAbort:
							return replayAbortExit(ag, sess, input, sessionStart, iterCount, totalTokens)
						case lr = <-lineCh:
						case <-tools.JobNoticeWake():
							// Woken, not taken: the LEVEL decides whether a turn
							// is owed, and a stale token (a round boundary
							// drained the inbox first) means the loop waits
							// again rather than minting a turn from nothing.
							if !chatNoticeTurnOwed() {
								continue
							}
							lr = lineRead{noticeTurn: true}
						case <-agent.SessionInboxIdleWake():
							// THE CROSS-PROCESS ARM, and its own reason: the arm
							// above is the TOOLS package's edge, raised by a
							// producer inside THIS process (the job reaper). A
							// message delivered by `gopi notify --session … ` is
							// merged into the same queue by the idle poller
							// instead, which raises this channel. Both are only
							// nudges — the LEVEL below is the decision, and it is
							// the SAME level for both kinds of notice, so the two
							// arms cannot disagree about whether a turn is owed.
							if !chatNoticeTurnOwed() {
								continue
							}
							lr = lineRead{noticeTurn: true}
						}
						break
					}
				}
			} else {
				// NOT a turn boundary (the first iteration of a session, or an
				// iteration that only answered a REPL-native command): a parked
				// prompt is never taken here — it waits for a turn to end — so the
				// loop simply blocks for the operator's own input. The notice arm
				// is present here too, and for a reason of its own: on a session
				// whose FIRST turn has not started yet there is no boundary to
				// reach, so without it the very first notice would be the one
				// notice the console could never report.
				if chatNoticeTurnOwed() {
					lr = lineRead{noticeTurn: true}
					break
				}
				for {
					select {
					case <-interruptCh:
						return leaveChat(ag, sess, input, sessionStart, iterCount, totalTokens, 1, agent.EndCause{Reason: sessionevent.SessionEndReasonSignalled})
					case <-replayAbort:
						return replayAbortExit(ag, sess, input, sessionStart, iterCount, totalTokens)
					case lr = <-lineCh:
					case <-tools.JobNoticeWake():
						if !chatNoticeTurnOwed() {
							continue
						}
						lr = lineRead{noticeTurn: true}
					case <-agent.SessionInboxIdleWake():
						// The cross-process arm, same contract as its twin at the
						// turn boundary above: a nudge, and the LEVEL decides.
						if !chatNoticeTurnOwed() {
							continue
						}
						lr = lineRead{noticeTurn: true}
					}
					break
				}
			}
		}
		if lr.err != nil {
			// A real read failure (not EOF): report it and finalize the session
			// like any other closeout, so the CHATS row is stamped.
			fmt.Fprintf(os.Stderr, "[gopi] input error: %v\n", lr.err)
			return finishChat(ag, sess, sessionStart, iterCount, totalTokens, 1, agent.EndCause{Reason: sessionevent.SessionEndReasonInputError})
		}
		if lr.eof {
			// END OF INPUT, and WHICH end it was is recorded rather than
			// collapsed (task 01a163f8): Ctrl+D, Ctrl+C at the prompt, a closed
			// pipe and a cooked reader all used to write the same
			// `exit_code:0` session_end, so "the session ended" was answerable
			// and "why" was not. The kind comes from the input layer, which is
			// the only party that measured it; a kind this switch does not know
			// is said as `eof` with eof_kind=unknown, never silently borrowed.
			// A normal clean exit either way — the pre-editor behavior.
			return leaveChat(ag, sess, input, sessionStart, iterCount, totalTokens, 0, chatEndCause(lr.endKind))
		}
		line := lr.line
		msg := strings.TrimSpace(line)
		// THE NOTICE TURN'S MESSAGE, composed here and ONLY here — one line of
		// reply for the boundary's `lr.noticeTurn` arm to have broken out at, so
		// the arm stays a pure "which read wins" decision and every message a
		// turn opens with is built in the same place, by the same rules
		// (the seal below, the local echo, the renderer). It is the PEEK that
		// builds it: the notices are named, never taken — the take stays at this
		// turn's own round boundary, which injects them "BEFORE the request is
		// built" (internal/agent/chat_ops.go), exactly as it always has.
		// sealNotices is the PEEK this turn opened from, kept in scope for the
		// boundary below: the boundary must know WHICH face produced the line it is
		// about to bill (chatSealSource reads this, never the text), and a notice
		// turn carrying an external message is the `notify` face. Never taken here
		// — the take stays at this turn's own round boundary.
		var sealNotices []tools.JobNotice
		if lr.noticeTurn {
			notices := tools.PeekJobNotices()
			sealNotices = notices
			msg = agent.ChatJobNoticeOpenLine(notices)
			line = msg
			input.WriteNotice(chatNoticeTurnNoticeFor(notices))
		}
		if msg == "" && !lr.noticeTurn {
			continue
		}
		if msg != "" && sess.queue.consumeParked(msg) {
			// A `/queue <prompt>` the READER already parked while the previous
			// turn ran. The queue owns that prompt now — running this line as an
			// ordinary turn as well would ask the model the same question twice
			// (once here, once when the parked copy becomes a turn). Its note row
			// was replayed at the boundary above, so the park is not silent.
			continue
		}

		// THE PERIOD BOUNDARY, and it is the READ ARM THAT IS THE EVIDENCE — never the
		// text. A `type`-action line arrives on the seam above (selfDriven=true); every
		// other line reached the REPL through lineCh, which is the one path a line the
		// model wrote cannot take. So the renewal is gated on WHICH ARM produced the
		// line, and NOTHING else.
		//
		// THE GATE IS NOT OPTIONAL, and this is measured, not reasoned: without it the
		// renewal ran on the self-driven arm too, so every model-typed turn renewed its
		// own budget. A live session that asked for one more turn on each round never
		// reached its bound at all — the console read "self-driven turn 1/8 of this
		// period" and "budget RENEWED by your own submitted line" forever, one pair per
		// turn, with the counter pinned at 1 (observed on this case's first end-to-end
		// run, which is why the case exists at this level and not only in internal/agent:
		// the unit gate drove ChatSurfaceUserTurn directly and could not see the wiring).
		// The line is the ONLY thing that distinguishes them, so a renewal keyed on the
		// text would be self-renewal wearing the operator's clothes — which is exactly
		// the ruling this implements (task 01a0e44a).
		//
		// The console says so in both directions, because a budget that refills silently
		// is a bound the operator cannot watch.
		if !selfDriven && !lr.noticeTurn {
			if renew := ag.ChatSurfaceUserTurn(); renew.Renewed {
				input.WriteNotice(fmt.Sprintf(chatSelfDrivenRenewalNotice, renew.Bound))
			} else if renew.Deferred {
				// The line IS the operator's, but a self-driven line is still in
				// hand — so the boundary belongs to the model's turn, and the reset
				// is refused BY NAME rather than applied by inference (an inferred
				// reset is the self-renewal this seam exists to prevent). Said out
				// loud, because a renewal the operator expected and did not get is
				// exactly the silence this console exists to break.
				input.WriteNotice(chatSelfDrivenRenewalTransientNotice)
			}
		}

		// Slash commands (R4): the four session-state commands stay
		// REPL-native (they mutate chatSession, which the shared table can't
		// see); EVERYTHING ELSE resolves through the shared command table
		// (cmd/agent/commands.go) so chat exposes the same /task /log
		// /text2img surface as the web carrier — executed locally, never a
		// model turn. (`/code-search` and `/memory` were retired on 2026-11-07:
		// the model's own `code_search` / `memory` tools are the door.)
		// THE INPUT CLASSIFIER decides what this line IS before anything acts on
		// it (commands.go): a command-shaped line whose word this carrier
		// accepts dispatches below; a command-shaped line with a word it does
		// not accept is refused HERE (with the literal-send channel named);
		// EVERYTHING ELSE — a pasted path, a pattern, a sentence — is the
		// model's text, untouched.
		classified := classifyChatLine(msg, chatReplWordKnown)
		if classified.kind == chatLineText || classified.kind == chatLineEscape {
			// TEXT, or the caller's own literal-send escape ("//word" →
			// "/word"). Neither is a command, so the dispatch below is skipped
			// entirely — including for the escape, whose whole point is that
			// the slash-carrying text it produces must NOT be re-classified as
			// a command (that would leave the channel unreachable).
			msg = classified.text
		} else {
			if classified.kind == chatLineUnknown {
				// Command-shaped and not a word this carrier accepts. (A
				// REPL-native session word cannot arrive here: chatReplWordKnown
				// is the vocabulary the classifier consulted, so such a word is
				// classified as a COMMAND and answered by the switch below.)
				err := chatUnknownCommandError(classified.word)
				fmt.Fprintf(os.Stderr, "[gopi] %v\n", err)
				chatOnceReportFailure(chatOnceFailureCommand, err)
				continue
			}
			msg = classified.text
			word := strings.ToLower(strings.Fields(msg)[0])
			switch word {
			case "/" + quitCommandName, "/" + exitCommandName:
				return leaveChat(ag, sess, input, sessionStart, iterCount, totalTokens, 0, agent.EndCause{Reason: sessionevent.SessionEndReasonQuit})
			case "/" + webClearAliasName, "/" + webClearCommandName:
				// B1: ONE reset, two spellings. The word `/new` is the shared
				// table's canonical name (so it appears in /help and the
				// completion surface); `/clear` is the REPL's long-standing
				// spelling. Both clear the carrier-local transcript HERE —
				// dispatching `/new` through the table would only print the
				// ACK without clearing (the table cannot see chatSession).
				sess.history = nil
				input.WriteNotice("[gopi] conversation cleared.\n")
				continue
			case "/" + toolsCommandName:
				sess.toolsEnabled = !sess.toolsEnabled
				// The row follows the toggle: the tool column's field is composed
				// from this flag, so a `/tools` that did not reach the source would
				// leave the row stating the previous mode.
				status.setTools(sess.toolsEnabled)
				if sess.toolsEnabled {
					input.WriteNotice("[gopi] tools ENABLED — turns may issue real tool calls (read/bash/edit/…).\n")
				} else {
					input.WriteNotice("[gopi] tools disabled — plain conversational Q&A.\n")
				}
				continue
			case "/" + chatQueueCommandName:
				// The queue is the OPERATOR's own next-turn seam, and it mutates
				// state the shared table cannot see (chatSession.queue) — so it is
				// answered HERE, like /quit /clear /tools /help, and never
				// dispatched. `/help queue` is answered from the SAME const block
				// (chat_queue.go) the refusal and the list render.
				chatQueueHandleLine(input, sess, msg)
				continue
			case "/" + chatInfoCommandName:
				// /INFO: the session's own identity, read live and printed as
				// one copyable block. Answered HERE and never dispatched — every
				// fact it prints belongs to this PROCESS (the logger's UUID, the
				// ground, the tools flag), which the shared table cannot see. The
				// block and its usage text live in ONE file (chat_info.go), so a
				// rename cannot leave the help documenting a word this switch
				// does not answer.
				chatInfoHandleLine(input, ag, sess)
				continue
			case "/" + chatVoiceCommandName:
				// /VOICE: continuous local capture + transcription, appending to
				// the editing line. Answered HERE and never dispatched — the state
				// it mutates (which process is recording, and the gate installed at
				// the editor's key seam) belongs to this REPL, which the shared
				// table cannot see. Its usage block lives in the same file as the
				// block it documents (chat_voice.go).
				voiceMode.handleLine(input, msg)
				continue
			case "/" + chatContextCommandName:
				// /CONTEXT: export the request the model ACTUALLY RECEIVED at one
				// turn — this run's raw HTTP log holds every one of them — into a
				// file, and print ONE line (chat_context.go). Answered HERE and
				// never dispatched: its subject is this PROCESS's live raw log and
				// this session's directory, neither of which the shared table can
				// see. It costs no model turn and injects nothing into the next
				// one: this is a file for the operator to read.
				chatContextHandleLine(input, ag, sess, msg)
				continue
			case "/" + chatConfigCommandName:
				// /CONFIG: print the configuration THIS PROCESS is running with
				// (chat_config.go + runtime_config_snapshot.go). Answered HERE and
				// never dispatched — the ROWS it reports above the environment
				// half are the live AGENT's resolved fields, which the shared
				// table cannot see (it has no *agent.Agent). It costs no model
				// turn and injects nothing into the next one.
				if chatConfigWantsHelp(chatConfigArg(msg)) {
					input.WriteNoticeRuns(termNoteRows(chatConfigUsage, input.tool), input.flowWriter(), true)
					continue
				}
				chatConfigHandleLine(input, ag)
				continue
			case "/" + chatThinkingCommandName:
				// /THINKING: show or hide the model's LIVE reasoning display for
				// the rest of this session (chat_thinking.go). Answered HERE and
				// never dispatched — the switch is `Agent.HideThinking`, a field of
				// the LIVE agent that the shared table's Run(args, CommandContext)
				// signature cannot reach; the table entry exists for /help and the
				// completion menu, and its Run refuses by name on a carrier with no
				// agent. It costs no model turn and injects nothing into the next
				// one: the field is read at each emit point, so the NEXT reasoning
				// fragment already obeys the new value.
				chatThinkingHandleLine(input, ag, msg)
				continue
			case "/" + chatScreenCommandName:
				// /SCREEN: copy ONE TURN's recorded render bytes — the turn named
				// by the argument, or the newest one — into a file and park a
				// ONE-SHOT reference for the NEXT turn's message (chat_screen.go).
				// The span is whole (there is no bound any more) and comes from the
				// renderer's own turn header. Answered HERE and never dispatched —
				// its subject is this PROCESS's live stream and this REPL's next
				// message, neither of which the shared table can see — and it
				// costs no model turn: nothing is sent now, and the file is only
				// described to the model when a real turn happens.
				chatScreenHandleLine(input, ag, sess, msg)
				continue
			case "/" + helpCommandName, "/" + helpShortWord:
				// R4's help contract, REPL side: bare /help prints the REPL
				// banner plus the shared table; `/help <cmd>` prints that ONE
				// command's usage (chatHelpText — the same standard the web
				// carrier gets from execSlashCommand). Before this, the REPL
				// ignored the argument and dumped the whole table.
				out, err := chatHelpText(msg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[gopi] %v\n", err)
					// The REPL continues (an interactive user retypes); a FACE
					// driving this run must still be able to see that a scripted
					// line did not run (chatOnceFailureObserver).
					chatOnceReportFailure(chatOnceFailureCommand, err)
					continue
				}
				input.WriteLines(out)
				continue
			default:
				// The REPL's OWN vocabulary is what a refusal must name, and it
				// is LARGER than the shared table: /quit /exit /clear /tools
				// /help mutate chatSession, so the table cannot see them. The
				// table's dispatch therefore did not own this word — either a
				// REPL-native word (answered here, with the usage block the
				// same constant defines) or nothing the carrier accepts at all.
				bare := strings.TrimPrefix(word, "/")
				if usage, ok := chatReplSlashVocabulary(bare); ok {
					input.WriteLines(usage)
					continue
				}
				// THE REPL'S FACTS TRAVEL WITH THE CALL (epic 01a1b818-e02f):
				// the table's /info and /context describe THIS session, and the
				// console holds the same values the browser does. Built from
				// chatInfoFieldsFor, so the table's block and the /info arm's
				// block are one derivation.
				out, err := execSlashCommand(msg, replCommandContext(ag, sess))
				if err != nil {
					// ONLY a word the carrier does not accept is re-rendered as
					// a refusal, over the REPL's FULL vocabulary; a KNOWN
					// command's own failure keeps its own message (a
					// `/task update … --status bogus` must not be reported as an
					// unknown command — Rule 15: the message names the real
					// failure).
					if _, known := lookupCommand(bare); !known {
						err = chatUnknownCommandError(bare)
					}
					fmt.Fprintf(os.Stderr, "[gopi] %v\n", err)
					chatOnceReportFailure(chatOnceFailureCommand, err)
					continue
				}
				input.WriteLines(out)
				continue
			}
		}

		// Stream the model's answer: through the reserved answer band when the
		// terminal hosts the A5 layout (a finalized line is committed ONCE and
		// never repainted), through the historic buffered renderer otherwise
		// (piped stdin, --no-editor, or a terminal too small for the layout).
		// One turn = one numbered block in the band: the header names the turn
		// and repeats what was asked, the body carries the turn's marker column,
		// and the closing rule (EndTurn, after the tail is committed) separates
		// it from the next turn. The index is the REPL's own turn counter, so the
		// header and the status row's "iter N" cannot disagree.
		//
		// The renderer is built from the REPL's input layer (newChatTurnRenderer
		// resolves both the live band and the historic writer from it), so a turn
		// cannot render its answer into a band while writing the fallback
		// somewhere else.
		// THE MESSAGE BOUNDARY (chat_boundary.go) — the ONE point where the line
		// the operator typed becomes the message the model is asked. `msg` is
		// used ABOVE this line only for things that are not the model's input:
		// the slash dispatch (done — a command returns or continues before
		// here) and the local echo. Everything that reaches ag.Chat/ChatOps
		// below is `out`, so there is exactly one outbound message per turn and
		// no path around it.
		//
		// A line inside the boundary comes back byte-identical: the framing, the
		// spill and the reference are all conditional on chatBoundaryApplies,
		// which short input fails on both of its limbs.
		//
		// The seal runs BEFORE the turn's renderer exists, which is why the
		// refusal below has nothing to unwind: a turn that is never announced
		// must not leave a half-drawn block behind.
		//
		// IT IS TOLD WHICH FACE THE LINE CAME FROM. `selfDriven`, `lr.queueTurn`
		// (set where the line left the REPL's queue) and the notice peek above are
		// the arms that know it; chatSealSource turns them into the vocabulary
		// (internal/inputsource), and the boundary stamps it on any reference it
		// renders. Nothing about the line's TEXT is consulted — a `/queue`d prompt
		// is stamped `queue`, not `human`, because the arm that produced it is the
		// queue (task 01a10f9b).
		// IT IS ALSO TOLD WHICH FACE THE RECORDING SAYS THIS LINE ARRIVED UNDER
		// when a replay driver typed it (`lr.replayFace`): a replay re-enters
		// recorded lines as KEYSTROKES, and the arms below answer `human` for
		// every keystroke by construction. Without this the recorded face is lost
		// on the way back in and the counter below freezes — measured 2026-11-04,
		// session 0b51ae4a-fa82-41ba-8147-1ba3ebc92476: 7 bands numbered
		// 1,2,2,2,2,2,2 against 1..7 on the screen being re-run.
		turnFace := chatSealSource(lr.replayFace, selfDriven, lr.queueTurn, sealNotices)
		// WHICH TURN NUMBER THE READER IS SHOWN, derived ONCE per turn from the
		// SAME face the session log will stamp. Every recorded face opens a turn
		// under the operator's 2026-10-27 ruling, so the number this REPL prints
		// and the number `session log --turns` prints move by one for the SAME
		// rows — the property task 01a17492 was missing when 38 log turns were
		// added to 82 passes.
		out, sealErr := chatSealMessage(sess, msg, sess.toolsEnabled, turnFace)
		// THE PREDICATE IS APPLIED LATER (after the prefetch adoption, where the
		// message that will actually be logged exists) — see below. A refused
		// seal opens nothing, which is the zero value this holds meanwhile.
		thisTurnOpens := false
		if sealErr != nil {
			// Refused, not degraded: the boundary was obliged to bill this
			// message and could not, so the turn does NOT run. Sending the raw
			// bytes as a fallback is the one outcome the boundary exists to
			// prevent (Rule 15: the failure is reported, never swallowed).
			fmt.Fprintf(os.Stderr, "[gopi] input refused: %v\n", sealErr)
			chatOnceReportFailure(chatOnceFailureSeal, sealErr)
			iterCount++
			if thisTurnOpens {
				operatorTurns++
			}
			status.clearTool()
			turnStatus.publish(status, sess.chatTurnDisplay(operatorTurns), statusLiveIdleTurn, totalTokens)
			continue
		}

		// THE SCREEN CAPTURE REFERENCE RIDES THIS MESSAGE, ONCE (chat_screen.go).
		// It is taken HERE — after the seal succeeded (a REFUSED turn must not eat
		// the operator's capture, which is why this is not above the seal check)
		// and BEFORE the prefetch adoption, so the retrieval sees the message the
		// model will actually receive. The take CLEARS the slot, so one capture
		// describes exactly one turn; the block carries a PATH and a byte count and
		// no SGR bytes, because the artifact is one `read` away — which is the whole
		// reason this command exists.
		if note := sess.chatScreenTakePending(); note != "" {
			out.screenNote = note
			out, _ = chatScreenInject(out)
			if verbose {
				fmt.Fprintf(os.Stderr, "[gopi] screen capture reference injected into this turn's message (%d bytes of framing)\n", len(note))
			}
		}

		// THE ADOPTION POINTS, and there are two of them on purpose — one per
		// KIND of retrieval, both landing in this one outbound message:
		//
		//   (a) the SEALED TURN (chat_prefetch_turn.go): ONE bounded retrieval
		//       over the turn's whole input, run AFTER the boundary has framed it,
		//       because that is the only moment the payloads the turn refers to
		//       exist in their final form. It covers the four faces the typing
		//       sampler structurally cannot see — a `notify`-delivered turn, a
		//       whole-block paste, a `/queue`d prompt, a model-typed line — and it
		//       is recorded with the FACE it came from.
		//   (b) the TYPING-TIME section, when the operator paused on the line
		//       before pressing Enter (chat_prefetch.go). It is matched against
		//       `line`, not `out.Message`, because the boundary may have rewritten
		//       the message (a spilled paste) while the prefetch watched the
		//       buffer the OPERATOR typed.
		//
		// ONE SECTION PER TURN, and the sealed turn wins: it was fetched from the
		// turn's own input, so a second section for the same turn would be paid
		// for twice and would each claim the same authority. A turn that already
		// carries the turn-boundary section therefore DROPS the typing one — as a
		// recorded supersession, never as a silent discard (Rule 15).
		//
		// Between them they touch only `Message`: Display is the operator's own
		// line, and a background retrieval must not edit what the operator is
		// shown (already, for the spill boundary, the two are distinct fields).
		// Sealing happens BEFORE the renderer exists, so a section that is refused
		// — or one the operator moved on from while it was in flight — leaves no
		// half-drawn turn behind.
		//
		// THE TURN ADOPTION RUNS AFTER THE SEAL, and it must: it reads the payload
		// files the framing names, so before the boundary there is nothing to read.
		turnSource := turnFace
		// The terms are computed INSIDE the nil guard, so the OFF path does not
		// reach even the pure text assembly: "the switch is off" must mean this
		// whole mechanism is not on the path, not that it runs and discards.
		turnPrefetched := false
		if prefetch != nil {
			out, turnPrefetched = chatPrefetchAdoptTurn(out, prefetch, turnSource,
				chatPrefetchTurnTerms(selfDriven, sealNotices, line))
		}
		if turnPrefetched {
			prefetch.discardTypingSection(line)
			if verbose {
				fmt.Fprintf(os.Stderr, "[gopi] memory prefetch: adopted a turn-boundary retrieval into this prompt (%d bytes, source: %q)\n",
					len(out.Message), turnSource)
			}
		} else {
			adoptedOut, prefetched := chatPrefetchAdopt(out, prefetch, line)
			out = adoptedOut
			if prefetched && verbose {
				fmt.Fprintf(os.Stderr, "[gopi] memory prefetch: adopted into this turn's prompt (%d bytes)\n", len(out.Message))
			}
		}

		// THE VOICE-INPUT PREFIX, composed LAST so it describes the bytes that will
		// actually be asked and recorded: the seal has framed the message, the
		// screen reference has been injected, the prefetch section has been
		// adopted — and HERE is what is left, which is the turn's message.
		//
		// IT IS APPLIED ONLY WHEN THE SUBMITTED LINE STILL CONTAINS DICTATED
		// CHARACTERS, and that count was measured by the feeder off the editing
		// buffer (lr.voiceRunes, chat_voice_input.go). "Voice mode was open this
		// turn" is deliberately NOT the question: a dictated sentence the operator
		// deleted and retyped is pure typing, and marking it would tell the model
		// something about text that is not there.
		var voicePrefixed bool
		if out, voicePrefixed = chatVoiceMessage(out, lr.voiceRunes); voicePrefixed {
			if verbose {
				fmt.Fprintf(os.Stderr, "[gopi] voice input: %d dictated rune(s) on this line — the turn's message opens with the provenance line\n", lr.voiceRunes)
			}
		}

		// THE SAME PREDICATE THE SESSION READER USES, over the SAME two inputs it
		// reads: sessionevent.IsTurnOpeningRow(message, face) — the message that
		// will be LOGGED (after the prefetch adoption above, which is the text
		// ChatOps hands the session log) and the FACE this turn is stamped with.
		// Not the face alone: the face-only form charged a turn for a
		// harness-chatter row (a background-job notice written from the human
		// face), which the reader answers as "no turn" — so the ledger charged and
		// the display advanced while `session log --turns` did not. Measured
		// 2026-10-27 on session 80c07a7d-bb6b-48b4-be66-ea1867f403f2: the ledger
		// held 18 turns, the reader counted 11, and the gap was those rows.
		thisTurnOpens = chatOpensSessionTurn(out.Message, turnFace)
		turnNo := sess.chatTurnDisplay(operatorTurns)
		if thisTurnOpens {
			turnNo++
		}
		renderer := newChatTurnRenderer(input)
		// THE TURN'S MOMENT, MINTED HERE AND SHARED — UNLESS THE RECORD HAS ONE.
		// On a LIVE run the REPL's clock is the truth, and the value it mints is
		// both what the banner paints and what the record's turn-opening row is
		// stamped with (published to the agent below, consumed once by the
		// runner): two readers of one value, so the screen cannot state a second
		// the session log disagrees with.
		//
		// A REPLAYED LINE IS NOT A LIVE ONE, and the clock is wrong for it by
		// exactly one day in the measured case: a replay re-enters a recorded
		// turn as KEYSTROKES, so without this branch session 6b01dd46 (first turn
		// 2026-11-07 12:11:17) painted `turn 1 · 2026-11-08 15:59:32` — today, on
		// a recording a day old (measured 2026-11-08). The recording holds the
		// moment the row was written (replaydrive.Turn.OpenedAt), so the surface
		// ASKS THE RECORD and a replay of last week prints last week.
		//
		// A RECORDED TURN WITH NO USABLE TIMESTAMP TAKES THE ZERO VALUE, not the
		// clock: the band then prints sessionevent.TurnStampPlaceholder, which is
		// the honest statement, while time.Now() would print an
		// authoritative-looking wrong day (Rule 15). `lr.replayStamped` is what
		// distinguishes that from an ordinary run — NOT whether the moment is
		// zero, because a zero moment is a recorded fact here and a live line in
		// the other arm.
		turnOpenedAt := chatTurnOpenedAt(lr.replayMoment, lr.replayStamped, time.Now())
		chatBeginTurnAt(renderer, turnNo, out, turnOpenedAt)
		if out.Notice != "" {
			// The operator's own report of what the model received instead of
			// their bytes. It rides the BAND (WriteNotice), never stderr: on the
			// reserved layout stderr is off-screen, and a note about the message
			// that is about to be sent must be on the screen the user is reading.
			input.WriteNotice(out.Notice)
		}
		ag.OnText = func(delta string) {
			// The FIRST answer delta is a real event and moves the phase: the
			// prompt's own callbacks are the only place that knows the model has
			// stopped reasoning and started answering. setPhase publishes even
			// when the phase is already responding, so the row refreshes on the
			// stream's own cadence as well as on the keepalive's.
			turnStatus.setPhase(chatTurnResponding)
			// THE REDACTION PASS, UPSTREAM OF BOTH CONSUMERS (chat_replay_redact.go):
			// the tail of the stream that could still grow an identifying shape is
			// withheld, and what is released here is what the RENDERER DRAWS and
			// what the BEAT'S LEDGER ROW RECORDS — the same string, from the same
			// call, so the film's pixels and the ledger's rows cannot differ by a
			// withheld run. The hold is released by the next delta and, at the
			// latest, by flushReplayAnswerHold below (before the tail is
			// committed), so no rune of an answer is lost.
			for _, chunk := range sess.beats.maskAnswerStream(delta) {
				renderer.Write(chunk)
				// A beat per delta, AT the seam that draws it
				// (chat_replay_beats.go): the film shows the answer arriving
				// rather than appearing whole, because this is the same call that
				// streams it to the operator.
				if berr := sess.beats.emit(chatBeatKindAnswer, chunk, 0); berr != nil {
					chatNoticef(chatReplayNotice+"%v\n", berr)
				}
			}
		}
		// One turn with tools carries SEVERAL assistant messages (a round ends,
		// its tools run, the next round answers). The renderer's input is one
		// stream of deltas and cannot see where one message ended — so the agent
		// announces it, and the band closes its format classifier there. Without
		// it, a message that stops inside a fenced block it never closed leaves
		// that block open, and the NEXT message's prose and table rows are
		// painted as code with their `**markers**` literal (the operator's
		// reported screen, 2026-09-26).
		prevMessageEnd := ag.OnMessageEnd
		ag.OnMessageEnd = func() { renderer.AnswerBoundary() }
		// Live reasoning goes through the SAME renderer as the answer, and that
		// is the point: the reserved band repaints one row absolutely, so a
		// second writer on stderr both destroys the band and (in the raw
		// capture) leaves the reasoning text interleaved mid-row with the
		// answer — the "flickering, unreadable" report. Inside the band the
		// reasoning is committed line by line like any other text, so it
		// scrolls with the answer instead of fighting it.
		//
		// ONLY when the band is active: without it (cooked stdin, --no-editor,
		// a terminal too small for the layout) the historic stderr display
		// stays exactly as it was, because a piped capture's stdout/stderr
		// separation is something scripts already depend on.
		//
		// prevThinking is what the error branch below restores: the run-level
		// wiring (the chat flow seam) must survive a failed turn, and a
		// renderer belonging to a turn that is over must never receive the
		// NEXT turn's text.
		prevThinking := ag.OnThinking
		if input.Answer() != nil {
			ag.OnThinking = func(delta string) {
				turnStatus.setPhase(chatTurnThinking)
				renderer.Thinking(delta)
			}
		}
		// Tool activity goes to the status line (the web UI's `⚙` rows) AND —
		// on the reserved layout — into the answer band as a durable row. The
		// callback fires BEFORE execution for a call and AFTER for its result,
		// so the live marker names the tool that is running right now (the
		// in-process equivalent of the web SSE `event: tool` frames), while the
		// committed row is the transcript's record that the call happened: the
		// status row is cleared when the turn ends, so without the row a reader
		// cannot tell which commands produced the answer.
		//
		// Only the CALL is committed (the result's own digest is the answer's
		// material): one row per tool call, the shape the reference TUI prints.
		// The band is the only surface that carries it, so the cooked path's
		// byte stream is untouched (WriteToolRow reports false and drops it).
		// Display only: a nil callback would still run the tools.
		ag.OnToolEvents = func(ev agent.ToolEvent) {
			// The one-shot face's activity channel: a no-op unless `--once` is
			// driving this run (see chatOnceToolActivityOut).
			chatOnceToolActivity(ev)
			switch ev.Kind {
			case agent.ToolEventKindCall:
				status.setTool(ev.Name, ev.Summary)
				// The activity row IS the drawn event; the beat carries the same
				// text the row does (chat_ops.go composes one string for both), so
				// the film's tool row and the ledger's claim cannot disagree.
				//
				// THE MASK RUNS BEFORE THE ROW IS COMPOSED, and that ordering is
				// the deliverable rather than a detail: this row is built out of
				// the CALL'S ARGUMENTS, which is where a path or an id lives, and a
				// mask applied only to the ledger's copy would leave the row's own
				// pixel carrying the run the log claims to have withheld (the
				// containment check reads the dumped grids). One string, masked
				// once, drawn and recorded.
				//
				// THE ROW IS THE RENDERER'S (term_tool_row.go): ONE pure function
				// decides the cells — the marker, the kind, the tool name and the
				// command it marks, each with its own emphasis — and this arm
				// draws them on the band. The film's ledger keeps the TEXT of the
				// same cells, so the beat and the pixel still carry one string's
				// worth of truth while only the band carries the emphasis.
				//
				// The mask is applied PER CELL and BEFORE either destination
				// sees the row, for the reason the old single-string version
				// recorded: the containment check reads the dumped grid.
				runs := termToolRuns(termToolRow(ev, input.tool), sess.beats.maskText)
				row := lineedit.NoteRunText(runs) + "\n"
				if berr := sess.beats.emit(chatBeatKindToolCall, row, 0); berr != nil {
					chatNoticef(chatReplayNotice+"%v\n", berr)
				}
				// deliberately ignored: false means the cooked path owns the
				// bytes, where the historic status-row-only behaviour stands
				// (the row is the reserved layout's transcript, and that path's
				// byte stream is its contract).
				_ = input.WriteToolRowRuns(runs)
			case agent.ToolEventKindResult:
				status.setToolDone(ev.Name)
			}
			input.PublishStatus()
		}
		// A statement addressed to the OPERATOR rather than to the model — today
		// that is the ONE context compression (internal/agent/context_compress.go:
		// what it removed, or that it could not shorten anything). It goes through
		// WriteNotice, the ONE seam every console notice takes, because stderr is
		// erased by the editor's repaint on the reserved layout (lineedit_wire.go
		// records the measurement: `/new` vanished from the screen).
		//
		// The mask is applied first, for the same reason the tool row's is: the
		// notice carries the operator's own token counts, and every other
		// operator-facing string on this console is masked on the way out.
		ag.OnUserNotice = func(text string) {
			input.WriteNotice(sess.beats.maskText(text))
			input.PublishStatus()
		}
		// The in-flight state goes up AFTER ReadLine returned, i.e. after the
		// editor's feeder parked on the next read: the row says "sent" from the
		// moment the request is issued, and the editor (already blocked in its
		// own read) draws it on this wake. The tracker then keeps repainting it
		// (statusLiveKeepalive) until the phase changes or the turn ends — a
		// state painted once is the defect the operator reported, because one
		// lost wake then leaves the row on its PREVIOUS value for the whole wait.
		turnStatus.begin(turnNo)

		// THE DELIVERY BOX: <session-dir>/attachments/inbox/ is scanned ONCE per
		// turn, immediately before the request is assembled, and what it holds
		// is attached to THIS turn — images as native image content blocks (the
		// model sees the pixels), text as a note on the user message. Delivery
		// MOVES each file into attachments/inbox-sent/, which is the marker that
		// keeps the next turn from attaching the same picture twice
		// (cmd/agent/chat_inbox.go states the mechanism, the failure shapes and
		// who reclaims the directory).
		inbox := takeChatInbox(sess.sessionDir)
		reportChatInbox(inbox)

		// One turn: either the simplified no-tools chat path (minimal chat
		// system prompt, no task scaffolding) or the tool-enabled ops path
		// (full GoPi toolset, bounded ReAct rounds — claude-code-style work
		// inside the conversation). The SAME agent + SAME session id/log is
		// reused for every turn (single-session chat); sess.history carries
		// the accumulated conversation.
		var (
			result string
			runErr error
			// interrupted says THIS turn was abandoned by the operator's key.
			// It is a local, not the agent's latch, because the rendering
			// decisions below must follow the turn that actually happened —
			// see the interrupt arm after the renderer is closed.
			interrupted bool
		)
		// The window in which Ctrl+C means "abandon THIS turn" opens immediately
		// before the request and closes the instant it returns (endTurn, below
		// the call). It is opened by the REPL, never derived from the agent:
		// "the agent has a run context" is true for a moment on either side of
		// the turn, and the operator's key must not be classified by that margin.
		//
		// That ONE window is also what the queue reader reads: the reader parks a
		// `/queue <prompt>` exactly while a turn is in flight (parkQueueReaderLine,
		// chat_queue.go), because a prompt parked then is the only one with a turn
		// boundary left to become. A second flag would be a second answer.
		beginTurn(sess)
		if sess.toolsEnabled {
			// THE PREFETCH DECLARATION rides the agent, not the message: the
			// hits must reach the model as its OWN memory(action=search) pair
			// (internal/agent/chat_prefetch_seed.go), which the runner seeds
			// before the first request. The plain chat path has no tools at all,
			// so a pair there would be a tool_use the wire never advertised —
			// that path keeps the historical text suffix, and the declaration is
			// left unconsumed (and therefore not inherited by the next turn:
			// ChatOps clears it on entry).
			ag.PrefetchSeed = agent.SeededRetrieval{Query: out.Prefetch.Query, Section: out.Prefetch.Section}
			// THE FACE THIS TURN CAME FROM, published beside the seed and
			// consumed once by the runner so the JSONL's `user` row records it.
			// It is the SAME value chatSealMessage already computed for the
			// display label; the session log needs it too (a self-driven turn
			// counts as a turn but must stay traceable).
			ag.TurnSource = out.Source
			ag.TurnOpenedAt = turnOpenedAt
			result, runErr = ag.ChatOps(chatInboxTurnText(out.Message, inbox), sess.history, inbox.Images...)
		} else {
			// The NO-TOOLS path (the historical default) keeps the text suffix,
			// and it is a limit rather than a preference: this path advertises no
			// tools, so a memory(action=search) pair would be a tool_use the wire
			// never declared, and the model cannot vote on hits it has no tool to
			// vote with. The retrievability half of the prefetch survives here;
			// the VOTABILITY half exists only where a tool round does.
			ag.TurnOpenedAt = turnOpenedAt
			result, runErr = ag.Chat(chatInboxTurnText(chatPrefetchTurnText(out), inbox), sess.history, inbox.Images...)
		}
		claimed := endTurn(sess)
		// The turn ended by the operator's key: the run reports it as
		// agent.errTurnInterrupted (the agent already wrote the session JSONL's
		// turn_interrupted witness, so the record exists even if this REPL dies
		// before printing). Report it as an INTERRUPTION — the partial answer
		// stays on screen, the marker names it, and the loop continues to the
		// next prompt exactly like a failed turn (bug 01a0d513: the session
		// survives one turn ending badly).
		//
		// WHICH KEY is read from the claim when this REPL made one, and from the
		// agent's own record otherwise (the same cancel has other carriers, and it
		// is the agent that owns the reason the witness carries).
		interruptOrigin := claimed
		if agent.IsTurnInterrupted(runErr) || claimed != "" {
			runErr = nil
			interrupted = true
			if interruptOrigin == "" {
				interruptOrigin = lineedit.KeyOrigin(ag.TurnInterruptReason())
			}
		}
		// The REPL reports the turn's outcome to whoever owns the line source —
		// the pipe-back driver (chat_once.go) is the only carrier that integrates
		// a sink, and it is nil on every other face, so this costs an interactive
		// turn nothing. It sits HERE, between the turn and the branches below,
		// because both matter to the driver: a failed turn ends its loop, and a
		// completed one must be reported with the SAME text the session records.
		if chatOnceSink != nil {
			chatOnceSink.TurnFinished(result, runErr)
		}
		// THE HELD TAIL IS RELEASED FIRST: the redaction pass withholds the last
		// MaxIdentShapeRunes runes of the answer stream (a shape can straddle two
		// deltas), and the turn's end is the moment the stream is known to be
		// complete. It goes through the SAME two consumers as every earlier
		// chunk — the renderer and the beat — so the tail is drawn, recorded and
		// masked exactly like the rest, and Finish below then commits it.
		if ferr := sess.beats.flushAnswerHold(func(chunk string) error {
			renderer.Write(chunk)
			return sess.beats.emit(chatBeatKindAnswer, chunk, 0)
		}); ferr != nil {
			chatNoticef(chatReplayNotice+"%v\n", ferr)
		}
		renderer.Finish() // never drop the tail of the answer
		// THE RENDER-TIME ELLIPSIS BACKSTOP, at the one moment the turn's body is
		// known complete and is already on screen (chat_answer_ellipsis.go): a
		// U+2026 that reached the body is REPORTED with its turn and rune offset,
		// and the drawn bytes are left exactly as the model wrote them. It runs
		// after Finish — before it, the tail of the answer has not been observed
		// by the detector, and a hit in the tail would be missed.
		chatReportAnswerEllipses(input, renderer)
		// Close the turn's block: the rule goes AFTER the tail (a rule committed
		// first would sit above the answer's last line).
		renderer.EndTurn()
		// The agent's reasoning stream arrives as fragments of one line, so the
		// seam holds the unfinished tail (a row per token would stack the
		// sentence one word per row). A turn boundary is where it is committed:
		// the turn's last row must not stay invisible in the buffer. Both
		// writers are flushed — the seam for the flow text, the renderer for the
		// answer — because they are two consumers of the one screen.
		flowSeam.Flush()
		// THE OPERATOR'S OWN NOTICE, after the tail is committed and before the
		// screen is settled — order is the whole message: the partial answer has
		// to be on screen first (the marker says "the answer above is
		// incomplete"), and the settled dump has to CONTAIN the marker, because
		// the artifact is what a reader opens to find out why the text stopped.
		// It rides the layout's own writer (WriteNotice), never a bare Fprintf:
		// with the reserved band installed a flowed stderr write lands UNDER the
		// band and the next repaint erases it — the very defect this marker
		// exists to avoid, one layer down.
		if interrupted {
			input.WriteNotice("\n" + chatInterruptMarkerFor(chatInterruptReason(interruptOrigin), ag.PartialAnswerRunes()) + "\n")
		}
		// …and the screen dump is SETTLED here, at the one moment the run knows
		// the screen is final: the tail is committed and its bytes are already
		// in the sink, on this goroutine. Without this the artifact is written
		// only by a paint-stream CHANGE, so an armed run that is merely observed
		// (no keystroke, therefore no further paint) freezes on a pre-answer
		// frame — measured 2026-09-27: `frames=35 dumps=28 … iter 0 · ready`,
		// answer absent, byte-identical for 10s over 40 samples (see
		// chatInput.SettleDump and uiclient.ScreenDumper.Settle).
		input.SettleDump()
		// THE TURN'S LAST BEAT, here and only here: the answer's tail is committed
		// and its bytes are already in the sink, so this is the first moment the
		// screen a reader will see for this turn is final. tools/replay/verify.py
		// groups the ledger into turns by this beat, so it must be emitted once
		// per turn and never per fragment.
		if berr := sess.beats.finishTurn(); berr != nil {
			chatNoticef(chatReplayNotice+"%v\n", berr)
		}
		// THE SAME MOMENT, SAID TO THE REPLAY DRIVER. `finishTurn` above is what
		// an --film run's driver reads; this counter is what an ORDINARY run's
		// driver reads, and both are bumped HERE because this is the one place
		// that knows the turn's screen is final (the tail is committed, the dump
		// is settled, the block is closed). Without it `--from-turn N` had no
		// witness to consult on the ordinary path and shortened nothing
		// (measured 2026-11-04: 13.789s / 13.798s / 13.807s).
		sess.witness.mark()

		// Track per-turn iteration + token counts for the final session_end.
		// One turn = one logical iteration regardless of internal ReAct
		// rounds (CostTracker has no request counter; the session JSONL's
		// per-request LogRequest rows carry the round-level detail).
		iterCount++
		if thisTurnOpens {
			operatorTurns++
		}
		// The boundary below is now a TURN boundary: a prompt parked while this
		// turn ran may become the next one — and the reader may park one again
		// once the next turn starts.
		wasTurn = true
		if ag.Cost != nil {
			totalTokens = ag.Cost.TotalPromptTokens + ag.Cost.TotalCompletionTokens
		}
		// Publish the finished turn to the status line (iter/tokens) and drop
		// the live marker: the next repaint shows the new counters. The tool
		// row is cleared FIRST — it describes activity inside the turn that
		// just ended, and a stale "⚙ bash …" at the prompt would claim a tool
		// is still running. The turn number is the DISPLAY number; iterCount
		// (the close-out's pass count) never reaches the row.
		status.clearTool()
		turnStatus.publish(status, sess.chatTurnDisplay(operatorTurns), statusLiveIdleTurn, totalTokens)

		// M4.6 钩子分流: chat processes register into the CHATS table (session
		// facts), NOT the tasks ledger — the [auto] chat rows are retired.
		// Registering only after a successful Chat (runErr == nil) means a
		// first-turn API failure leaves no row at all. RecordChatTurn then
		// bumps turn_count/tokens on every subsequent turn (cumulative).
		// An INTERRUPTED turn is not a completed one, so it registers nothing:
		// RecordChatTurn's numbers describe a turn the model finished, and its
		// CHATS row's turn_count is the count of answers the session actually
		// holds. Billing a truncated turn as a whole one is how an abandoned
		// answer gets quoted later as if the model had said all of it.
		if runErr == nil && !interrupted && ag.Cost != nil {
			// THE HEAD COLUMN IS THE OPERATOR'S, AND ONLY THEIRS (2026-10-28):
			// the turn's text rides in as a CANDIDATE beside its FACE, so
			// RegisterChatSession can refuse a notify-injected line and store
			// tools.ChatFirstMsgPlaceholder instead. The offer that follows is
			// what lets a session born on a notice turn acquire a real head the
			// moment the person types (it writes only over an unset column, so
			// the first operator line wins and a later one cannot displace it).
			ag.RegisterChatSession("chat", out.Display, out.Source)
			ag.OfferChatFirstMsg(out.Display, out.Source)
			ag.RecordChatTurnOpened(thisTurnOpens, ag.Cost.TotalPromptTokens, ag.Cost.TotalCompletionTokens, ag.Cost.TotalCacheHitTokens+ag.Cost.TotalCacheMissTokens)
		}

		if runErr != nil {
			// MERGE NOTE (rework run 01a0d5d2-b38c-707a-aefe-53afe4d034d3): master's arm here closed the
			// screen and RETURNED — ``input.CloseOut()`` plus a bare
			// ``fmt.Fprintf(os.Stderr, "\n[gopi] error: %v\n", runErr)`` and
			// ``return finishChat(...)``. Its two halves are deliberately dropped on
			// this path, and the drops are recorded here rather than left silent
			// (the lines themselves still live in master's history):
			//
			//   * the RETURN is what bug 01a0d513 fixed — one failed turn (the bounded
			//     ReAct round cap the model alone can trip) ended the whole session —
			//     and the merged tree's own testsuite asserts the new behaviour
			//     (testsuite/chat_test.go and testsuite/chat_pty_test.go grep
			//     ``turn failed:``), so this arm continues the loop instead;
			//   * the ``CloseOut()`` belonged to that return: erasing the reserved
			//     layout is the LAST thing a leaving process does, and the session that
			//     stays open needs the band for its next turn. The Fprintf is
			//     superseded by ``WriteNotice`` below — the same stderr stream, with the
			//     band-awareness master's own close-out comment asked for (a flowed
			//     write lands UNDER the band once the OnThinking seam is installed, and
			//     the editor's repaint erases it).
			//
			// Master's close-out ORDER still holds everywhere it applies: the normal
			// exit path below and ``leaveChat`` keep ``input.CloseOut()`` before the
			// goodbye.
			// A failed turn is a failed TURN, not the end of the session
			// (bug 01a0d513): this branch was the one place in the loop that
			// returned on model error, so ONE unlucky turn — most often the
			// bounded ReAct round cap, which the model alone can trip by
			// keeping its ReAct loop open — killed the whole process. The
			// operator saw the transcript stop with the input prompt on screen
			// and nothing else: no bye, no summary, no next `you>`, the shell
			// prompt back (measured, session bbc4efd8-6bdd-4d6c-aa16-86bae2cecc00
			// — 40 rounds, ~1.5M prompt tokens, then the process exited with a
			// crash dump). Every OTHER exit in this loop is deliberate: an
			// explicit /quit or /exit, Ctrl+D, a signal, or a real read
			// failure. A recoverable turn error belongs with them no more than
			// a 500 from the API does: the session stays OPEN, the user reads
			// what happened and gets the prompt back.
			//
			// The notice goes through the layout's OWN writer (WriteNotice),
			// never a bare Fprintf: once the agent's OnThinking seam is
			// installed, a flowed write to stderr lands UNDER the band and the
			// editor's repaint erases it — the same defect cli_chat.go's other
			// notices were routed for, and the reason the operator's report
			// reads "without notice" as well as "exit". The seam is dropped
			// FIRST (the renderer that owned the answer band is going away) and
			// restored by the next turn's setup, so a stale renderer can never
			// receive this turn's text.
			turnStatus.end()
			ag.OnText = nil
			ag.OnThinking = prevThinking
			ag.OnMessageEnd = prevMessageEnd
			ag.OnUserNotice = nil
			input.WriteNotice(fmt.Sprintf("\n[gopi] turn failed: %v\n", runErr))
			// A face driving this run sees the FAILED TURN as a class of its own
			// (chatOnceFailureObserver): the REPL continues by design (bug
			// 01a0d513 — one unlucky turn must not kill the session), so the exit
			// status is the only place a scripted caller can learn of it.
			chatOnceReportFailure(chatOnceFailureTurn, runErr)
			iterCount++
			if thisTurnOpens {
				operatorTurns++
			}
			if ag.Cost != nil {
				totalTokens = ag.Cost.TotalPromptTokens + ag.Cost.TotalCompletionTokens
			}
			status.clearTool()
			turnStatus.publish(status, sess.chatTurnDisplay(operatorTurns), statusLiveIdleTurn, totalTokens)
			// The failed turn is still a turn that ENDED: the reader may park
			// again, and the boundary below is a real turn boundary — a prompt
			// parked while this turn ran must not be stranded by its failure
			// (a failure ends the turn, it does not end the session).
			wasTurn = true
			continue
		}

		// Capture the turn's accumulated context for the next turn's seed.
		sess.history = append([]deepseek.Message{}, ag.Ctx.Messages...)
		// A trailing newline on stderr separates turns in the historic
		// (buffered) layout. In the reserved layout the answer owns its band and
		// the status row sits below it, so an extra stderr newline would push
		// the layout around for no gain.
		if strings.TrimSpace(result) != "" && !renderer.Reserved() {
			fmt.Fprint(chatLayoutStream(), "\n")
		}
	}
}

// finishChat finalizes a single-session chat run: closes the session log's
// session_end, closes the raw log, and flips the task-ledger row to
// completed/failed (finishSessionTask — the end hook). Returns the exit code.
// statusLiveIdleTurn is the in-flight value that means "no turn is running": the
// status line then shows the completed count (see cmd/agent's turnLabel).
const statusLiveIdleTurn = 0

// MERGE NOTE (rework run 01a0db73): both functions are KEPT — the branch's
// statusOpsFor (the ops source the read-out is assembled from) and master's
// publishChatStatus, whose body follows this point (auto-merged, so it carries
// BOTH the tracker's update and the branch's setAvailCols). They sit here
// together only because both sides added a function at this point.
// statusOpsFor assembles the operational read-outs the status row shows from the
// agent's own counters. It is the ONE place the row's numbers are read off the
// runtime, so the provenance of each is stated once, here, next to the read:
//
//	ctxUsed/ctxMeasured ← Agent.Ctx.CalibratedTokens() and Agent.Ctx.PromptTokens
//	                      (internal/agent/context.go + context_tokens.go: the
//	                      API-measured prompt count when the last response provided
//	                      one, otherwise a BPE count that includes the tool
//	                      definitions; the chars/4 estimate is the fallback when no
//	                      tokenizer is available. The flag is the measured/estimated
//	                      distinction)
//	ctxMax              ← Agent.ContextWindow            (internal/agent/loop.go)
//	cacheRead/cacheWrite← deepseek.CostTracker.TotalCacheHitTokens /
//	                      TotalCacheMissTokens           (internal/deepseek/types.go)
//	compressions        ← Agent.CompactCount             (internal/agent/context_compress.go)
//
// A money read-out was the FIFTH counter here and it is GONE (task 01a0dcd1):
// pricing.CostOf is no longer called on this path, because the row does not show
// money — the amount is derived (price card x Beijing civil-time regime x a price
// page) rather than measured, and the operator's verdict on the seven cells it
// spent was "this is a fooling". The row's own RETIRED note (statusCostCol in
// chat_stream.go) carries the reasoning; `gopi session stats --cost` is where a session's
// bill belongs, computed once, off the repaint path.
//
// Two of the counters below report an ABSENCE rather than a zero, and that is the
// point: a nil Context or CostTracker leaves the corresponding read-out empty
// rather than inventing 0, and the context read-out reports the token total alone
// when no window is known (no denominator, no percentage).
//
// elapsed is passed in rather than read from a clock here: the REPL owns the
// session's start time, and a second clock in this function could disagree with
// the one the session_end record reports.
//
// base is the cache pair the CONTINUED session had already accumulated (zero and
// unknown for a fresh run). It is ADDED HERE rather than seeded into ag.Cost, and
// both reasons are load-bearing:
//
//   - the row's Σ must mean the SESSION (the defect: a resumed run read 81.5%
//     against `gopi session stats`'s 86.03% for one session, because the runtime
//     counters restart at zero);
//   - ag.Cost's running totals are WRITTEN to every `request` row and to the
//     run's session_end, and internal/requestusage folds a log by taking each run
//     SEGMENT's last reading — a segment that started at the previous segments'
//     total would be counted twice (the 48.6x defect that package removes).
//
// The row's Δ reading is unaffected: it is the DIFFERENCE of two readings taken
// from this same function, so a constant base cancels out of it exactly.
func statusOpsFor(ag *agent.Agent, start time.Time, base chatCostBase) statusOps {
	ops := statusOps{
		elapsed:      time.Since(start),
		elapsedKnown: !start.IsZero(),
	}
	if ag == nil {
		return ops
	}
	ops.ctxMax = ag.ContextWindow
	ops.compressions = ag.CompactCount
	ops.compressionsKnown = true
	if ag.Ctx != nil {
		// CalibratedTokens, not EstimatedTokens: the row's number is compared
		// against the API's prompt_tokens, which is BPE-exact and includes the
		// tool definitions — EstimatedTokens answers the TRIM trigger's question
		// in its own (pinned) units. See internal/agent/context_tokens.go.
		ops.ctxUsed = ag.Ctx.CalibratedTokens()
		ops.ctxMeasured = ag.Ctx.PromptTokens > 0
	}
	if ag.Cost != nil {
		// The cache pair is a fact the TRANSPORT reports about itself, which is
		// why it is the one cost-adjacent read-out the row still carries. The
		// money that costs those tokens is deliberately not computed here any
		// more (see this function's note on the removed read-out).
		ops.cacheRead = ag.Cost.TotalCacheHitTokens + base.cacheRead
		ops.cacheWrite = ag.Cost.TotalCacheMissTokens + base.cacheWrite
		// THE PROMPT SIDE rides the same snapshot, and it is what the turn reading
		// is derived from (statusCacheTurnRatio): the pair above cannot state its own
		// denominator at one instant, because a sum of counters is only as old as its
		// LAST arriving term. `cacheWrite` already carries the same quantity as the
		// remainder it defines (TotalCacheMissTokens == cache_creation + input), and
		// TotalPromptTokens == input + cache_creation + cache_read, so the two agree
		// by construction: prompt − read == write.
		ops.cachePrompt = ag.Cost.TotalPromptTokens
	}
	return ops
}

// publishChatStatus applies the REPL's current counters to the status source and
// then PUBLISHES the change. It is the ONE status update path in the turn loop:
// the update is atomic inside the source, and the publish is a flag plus a wake,
// never a terminal write — the editor's read loop is the only writer on the fd
// (the two-writer defect this wiring removes).//
// live is the in-flight turn (statusLiveIdleTurn at the prompt), iterCount the
// completed-turn count. An in-flight call deliberately sits AFTER ReadLine
// returned — i.e. after the editor's feeder goroutine parked on the next read —
// so the wake always finds a BLOCKED reader rather than racing one into the park
// (chatTurnStatus.begin is that path).
func publishChatStatus(input *chatInput, s *chatStatusSource, iterCount, live, tokens int) {
	s.update(iterCount, live, tokens, "")
	// The viewport rides the same publish: the row can only degrade for the
	// terminal it is drawn on, and this is the one place the REPL touches the
	// source before a repaint. The width is read from the live band (0 when the
	// reserved layout is not installed, which leaves the full geometry).
	s.setAvailCols(input.bandCols())
	input.PublishStatus()
}

// leaveChat ends the REPL: it hands the screen back and then says goodbye, in
// THAT order, and finalizes the session. Every close-out path (/quit, /exit,
// Ctrl+D, a read error, a signal) goes through it, so "what the user is left
// looking at" has one answer instead of one per exit reason.
//
// The order is the whole point. The input layer's own cleanup is deferred, so it
// runs after this call returns: erasing here means the terminal is already blank
// and the goodbye below is the only thing on the screen — whereas erasing from
// the deferred cleanup would wipe the goodbye the user just read.
func leaveChat(ag *agent.Agent, sess *chatSession, input *chatInput, start time.Time, iter, tokens, exitCode int, cause agent.EndCause) int {
	// What happens to parked prompts is stated BEFORE the teardown: the notes are
	// written through the layout the closeout is about to take down, and a queue
	// that vanished with no sentence would be the silent drop this feature exists
	// to make impossible. leaveChat is the ONE closeout every clean exit shares
	// (/quit, /exit, Ctrl+D, Ctrl+C), which keeps the statement from depending on
	// the exit path taken.
	chatQueueAnnounceDrop(input, &sess.queue)
	input.CloseOut()
	fmt.Fprint(chatLayoutStream(), chatGoodbye)
	return finishChat(ag, sess, start, iter, tokens, exitCode, cause)
}

// replayAbortExit is the REPL's exit when the REPLAY DRIVER stopped: the session
// ends through the ONE closeout every other exit uses, and the reason is already
// on the run's notice channel — the driver printed it before it closed the
// channel this arm read.
//
// TWO DIFFERENT ENDINGS ARRIVE HERE, and the exit code is what tells them apart:
//
//	the WATCHER left (q/Esc) — the replay ended at their request. NOT a failure:
//	  exit 0, the ordinary closeout, the same goodbye a /quit gets.
//	the DRIVER failed — a refused key, a failed emit, a missing input layer.
//	  exit 1, the failure code the rest of this binary uses for "the run did not
//	  complete".
//
// WHY THE LOOP MUST NOT SIMPLY KEEP WAITING, which is the measured deadlock this
// arm exists for. When the driver stopped, nothing was left that could type a
// line: the source of every line in a replay run is the recording, and the
// operator at a filmed terminal is looking at a film, not typing into it. A loop
// that keeps blocking on lineCh waits for a keystroke that can never come, and
// the whole run — driver, terminal, private display — waits with it (measured
// 2026-10-04: 7h06m of nothing at the prompt, the driver killed by hand).
func replayAbortExit(ag *agent.Agent, sess *chatSession, input *chatInput, start time.Time, iter, tokens int) int {
	// THE WATCHER'S QUIT IS AN ENDING, NOT A FAILURE, and it is asked of the
	// control layer rather than inferred here: the same ONE predicate the key
	// gate and the driver read decides it (chat_replay_control.go's
	// leftByWatcher), so the exit code cannot contradict the sentence the
	// watcher was just shown.
	if sess != nil && sess.replay != nil && sess.replay.control != nil && sess.replay.control.leftByWatcher() {
		return leaveChat(ag, sess, input, start, iter, tokens, chatExitOK, agent.EndCause{Reason: sessionevent.SessionEndReasonQuit})
	}
	fmt.Fprint(chatNoticeWriter(), chatReplayAbortNotice)
	return leaveChat(ag, sess, input, start, iter, tokens, chatExitFailed, agent.EndCause{Reason: sessionevent.SessionEndReasonReplayAborted})
}

// chatGoodbye is the REPL's last line. Named because it is the one string the
// exit paths share — and because a test asserts it is what a user sees.
const chatGoodbye = "bye.\n"

// chatSessionID is the identity the exit marker records: the session logger's
// own id (one definition, internal/agent), and "" for a run with no record —
// which is the same nil case every marker call already tolerates.
func chatSessionID(ag *agent.Agent) string {
	if ag == nil || ag.SessionLog == nil {
		return ""
	}
	return ag.SessionLog.SessionID()
}

// The two lines the marker's writer may say, and the ONE thing each claims
// (Rule 15b: a failure is never swallowed, and the two failures are different
// facts):
const (
	// Arm failed: the run proceeds WITHOUT the instrument, so the operator knows
	// the next death will be undecidable BEFORE it happens.
	chatExitMarkerArmFailedNotice = "[gopi] chat: no exit marker for this session (%v) — a death that reaches no close-out will not be decidable from the session directory\n"
	// Advance failed: the run is already leaving (the closeout or the returning
	// path), so this cannot be retried; it is reported and the exit code is
	// unchanged.
	chatExitMarkerWriteFailedNotice = "[gopi] chat: could not record this run's exit phase (%v) — the marker still says the last phase it reached\n"
)

func finishChat(ag *agent.Agent, sess *chatSession, start time.Time, iter, tokens, exitCode int, cause agent.EndCause) int {
	// THE DEATH CERTIFICATE IS ADVANCED FIRST — before the replay join, before
	// the in-flight sync is cancelled, before anything that can block or fail.
	// This is the ONE statement whose ordering matters: a process that dies
	// INSIDE the closeout must be distinguishable from one that never reached
	// it, and that difference is exactly "phase=closing" versus "phase=live"
	// (chat_exit_marker.go's five-row table). Putting it anywhere later would
	// re-open the very ambiguity it exists to close.
	if err := sess.exitMark.closing(exitCode, cause); err != nil {
		fmt.Fprintf(os.Stderr, chatExitMarkerWriteFailedNotice, err)
	}
	// THE REPLAY'S JOIN, before anything is closed: the driver types the recorded
	// turns and stops, and this is where the run says what it served. It runs
	// FIRST (before the session log is closed) because the summary is about the
	// session this process just re-ran, and it is written to the operator's own
	// stream — where the close-out diagnostics below also land.
	joinChatReplay(sess.replay)
	// THE IN-FLIGHT STORE-TRIGGERED ROUND DIES WITH THE RUN (measured leak, task
	// 01a16518). The round is asynchronous on purpose — that is what keeps the
	// store's return instant — but nothing used to cancel it, so a chat that quit
	// in 5.09s left the store-triggered memory-sync child (the since-DELETED
	// `gopi memory sync` spelling, removed by 5677cc6e) and its `ssh …
	// --serve-assets` leg alive with PPID=1, cwd still this run's own ground,
	// still carrying the whole corpus to a peer. Four such orphans were measured
	// on the peer, each write-locking that peer's ledger in turn — the 216.8s /
	// 256.1s / 306.8s rounds this epic was filed about.
	//
	// It is placed FIRST in the closeout and it NEVER WAITS: the kill is
	// delivered and the child's own ctx watcher reaps it, because a closeout that
	// blocked on a peer would replace one hang with another. The round is
	// recorded as FAILED by its own outcome path (Rule 15), and the next success
	// reports how many misses it cleared.
	elapsed := time.Since(start).Seconds()
	// THE ENDING CAUSE IS STATED HERE, by the ONE close-out every REPL exit path
	// already shares (task 01a163f8). Which leg ran is known only at the call
	// site that chose to return, so it is carried down rather than re-derived:
	// before this, /quit and a closed stdin both wrote exit_code=0 with nothing
	// else and no reader could tell them apart. A recorded signal still wins
	// inside CloseChatSession — it is the more specific fact, set on the
	// goroutine that decoded it — and what this call adds is the leg the
	// operator took when no signal was involved.
	ag.CloseChatSession(exitCode, iter, tokens, elapsed, cause)
	// M4.6 钩子分流: the chat row lives in the CHATS table — stamp
	// ended_at/end_reason there (the tasks-ledger [auto] row is retired).
	ag.CloseChatRow(exitCode)
	// THE RECORDER'S RECORD IS SETTLED BEFORE THE PROCESS LEAVES (measured
	// defect, session ab9e0023, 2026-10-21). Chat CLAIMS its own signal handlers
	// (signalrec.modesThatOwnTheirSignalHandlers: "chat"), which means the
	// recorder writes the provenance record on ITS OWN goroutine while this one
	// unwinds — and then main() calls os.Exit, which does not wait. Every other
	// claiming door pays this handshake on its way out (cli_term.go,
	// cli_term_once.go, main.go's bare prompt; the memory-sync `--every` loop
	// and merge_run.go were two more doors that have since been DELETED);
	// chat did not, so a hangup that this process HANDLED and wound down
	// through its normal close-out left NO provenance file at all — the session
	// directory held a console, a dump and a log, and nothing that named the
	// signal. That is the "vanished without a trace" report, reproduced on a pty
	// against this branch's own binary: hangup received on screen, exit 1,
	// `signal-provenance.jsonl` absent.
	//
	// It is silent on an ordinary exit (no signal reached the recorder → nothing
	// to wait for, ~5ms) and it never changes the exit code: the process is
	// already leaving, and the handshake's whole purpose is that a LOSS gets said
	// out loud rather than swallowed (Rule 15).
	settleProvenanceRecord(chatProgLabel)
	// The closeout's own diagnostics (the two calls above) are logged AFTER the
	// redirect is released, so they belong to the operator's terminal again —
	// the screen has no layout to protect once the editor is closed.
	closeChatDiagnostics(ag)
	// NOTHING IS SAID ABOUT A GROUND HERE ANY MORE, and the absence is the
	// point: this session owns no worktree and no branch (epic
	// chat-session-ground-retired-when-not-a-row), so there is no directory to
	// name and nothing left in place. The start-up line said where the writes
	// would land; repeating it at exit would imply an artifact this run does not
	// have (Rule 15: a record that says nothing is not a record).
	return exitCode
}

// chatEndCause maps the input layer's own word for an end-of-input event onto
// the session_end cause (task 01a163f8).
//
// WHY A MAPPING AND NOT A DIRECT PASSTHROUGH. The two vocabularies are owned by
// different layers on purpose: internal/lineedit may not know what a session log
// is (it is a key stream, not a carrier), and cmd/agent must not re-spell a key
// event the editor measured. So the editor names the EVENT and this function
// names its RECORD — one translation, here, where both are in hand.
//
// A kind this switch does not know takes the `eof` reason with eof_kind=unknown
// rather than being guessed into Ctrl+D: the two endings mean different things
// about the run, and a wrong one is worse than an admitted unknown (Rule 15).
func chatEndCause(kind string) agent.EndCause {
	switch kind {
	case lineedit.EOFKindKey:
		return agent.EndCause{Reason: sessionevent.SessionEndReasonEof, EOFKind: sessionevent.SessionEndEOFKindKey}
	case lineedit.EOFKindSource:
		return agent.EndCause{Reason: sessionevent.SessionEndReasonEof, EOFKind: sessionevent.SessionEndEOFKindSource}
	case chatEndKindInterrupt:
		// Ctrl+C at the prompt: the operator's own key, ending the run cleanly.
		// Its own reason, because it is neither an EOF nor a signal.
		return agent.EndCause{Reason: sessionevent.SessionEndReasonInterrupt}
	default:
		// Includes chatEndKindCookedEnd: the cooked reader has no key stream, so
		// which byte ended the input is genuinely unknowable here.
		return agent.EndCause{Reason: sessionevent.SessionEndReasonEof, EOFKind: sessionevent.SessionEndEOFKindUnknown}
	}
}

// chatProgLabel is this face's name in the recorder-handshake report lines
// (settleProvenanceRecord). It is the program word the face's own lines already
// carry — `[gopi] hangup received — finalizing chat session…` — so an operator
// greps one spelling (the same Rule 14 discipline termProgLabel states).
const chatProgLabel = "gopi chat"

// chatGracefulSignals is the set chat registers for its graceful single-session// closeout — ONE spelling (Rule 1b), asked by the registration site and by any
// reader that must know the set, so the two cannot drift.
//
// SIGHUP IS IN THE SET (task 01a15bb9, 2026-10-21) for the same measured reason
// as the worker's: internal/signalrec registers SIGHUP for every verb (which
// replaces the default disposition) and then declines to re-raise for "chat"
// because chat is listed as owning its own handlers — so a hangup absent from
// THIS set was recorded and silently dropped. A terminal session teardown would
// leave the CHATS row in_progress forever, which is precisely the zombie the
// closeout below exists to prevent.
//
// The meaning is the historic closeout, not a per-turn interrupt: the watcher
// claims a turn ONLY for os.Interrupt (a SIGTERM — and now a hangup — says
// "this process is going away", nothing about which turn the operator was
// reading), so a hangup unwinds through ctx and writes session_end.
func chatGracefulSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
