package main

// chat_replay.go — `gopi chat --replay <session-id>`: re-run the REAL chat
// surface over a RECORDED session.
//
// WHAT THIS IS FOR. A session that already happened is the most useful thing to
// show: it is real work, real tool calls, real answers. Until now the only way
// to put one on a screen again was to REBUILD the picture from the session
// transcript (internal/replay, a second renderer that re-draws what the chat
// looked like) — and a second renderer drifts from the surface it imitates: the
// measured defect this verb exists to remove is a film classified by a renderer
// that never asked the terminal anything (see internal/lineedit
// CapabilityForOpenTerminal's comment).
//
// So the re-run does not re-draw. It runs `gopi chat` — this binary's own REPL,
// its own line editor, its own answer band, its own banner, its own status row
// — and takes exactly three things out of the re-run's hands:
//
//  1. THE MODEL. internal/replaydrive's stub is an in-process Claude-SSE server
//     on 127.0.0.1:0 that serves the recording's responses, and THIS run's
//     agent client is pointed at it by setting the environment the client
//     resolves its base URL from (deepseek.ResolveClaudeBaseURL: ANTHROPIC_BASE_URL
//     first). Setting the env before newAgent is what makes the seam real
//     rather than decorative: every client constructor in this binary — the
//     chat's, a later one's — reads the same resolution, so there is no second
//     place to forget. The listener is loopback-only on a KERNEL-assigned port:
//     off-host unreachable, but not a private channel — the wording it must
//     carry is devdoc/replay-concurrency-and-port-semantics.md §1.
//  2. THE TOOLS. Every recorded tool call is answered with the RECORDED result
//     through the agent's exported executor seam (agent.SetToolExecutor), so
//     the real dispatch — the one that reads files, runs bash, writes paths —
//     is never reached. A recording of `bash mkdir -p tasks` therefore creates
//     nothing when it is replayed, and the run can PROVE it: the recording's
//     own result is what the model receives, byte for byte.
//  3. THE OPERATOR. Each recorded turn's text is typed into the REAL editor
//     through the key injector the production path already installs
//     (lineedit.Injector — the same source decorator `chat_surface key` uses),
//     so the line arrives through the editor's own decoder exactly as a
//     keyboard's would, and the surface's own frame is what the dump records.
//
// NOTHING IN THE DRAWING PATH IS TOUCHED. The banner, the tips, the info bar,
// the reserved layout and every renderer are the code that runs for a human;
// this file only decides where the model's words and the tool results COME
// FROM, and who types. `--dump-screen` is already ON by default, so the run's
// own evidence channel needs no flag here.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/replaydrive"
	"github.com/nickhuang99/gopi/internal/replaymode"
)

// The replay vocabulary and its bounds. Every number is a named constant
// (Rule 1) and every word the parser accepts is spelled once (Rule 1b).
const (
	// chatFlagReplay is the switch, and it TAKES A VALUE: the session to re-run.
	chatFlagReplay = "--replay"
	// chatReplayCharsPerKey is how many characters of a recorded turn are typed
	// per injected read. The injector delivers at most injectReadBytes (64) per
	// unit, and each key is paced by the editor's OWN paste window so a run of
	// characters is not classified as a paste (whose Enter is suppressed). 16
	// keeps every unit inside the byte bound with headroom for multi-byte runes.
	chatReplayCharsPerKey = 16
	// chatReplayEnterSettleMS is the pause between the last typed character and
	// the recorded Return — the same reason internal/replay pauses before it:
	// the gap is what makes the Return read as a submit rather than as the tail
	// of a paste burst.
	chatReplayEnterSettleMS = 120
	// chatReplayExitSettleMS is the pause given to the REPL's own closeout after
	// the recorded turns are typed and /quit is injected: the loop consumes the
	// line within a frame, so this is a bounded settle for a completion the
	// driver cannot itself observe without owning REPL internals. It is short
	// because what follows it is the driver's own join on the session, which is
	// the real wait.
	chatReplayExitSettleMS = 300
	// chatReplayPollMS is how often the driver re-reads the stub's request count
	// while waiting for a turn's responses. The wait is NOT a sleep: the stub's
	// counters are the condition, and this is the granularity at which the
	// condition is observed.
	chatReplayPollMS = 20
	// chatReplayTurnTimeout is how long the driver waits for one turn to consume
	// its recorded responses before it gives up and reports the shortfall. A
	// bounded wait is required (an unbounded one hangs a terminal session with
	// no sentence), and the bound is generous next to a replay where NOTHING is
	// computed: the only real cost is the local stub's round trip.
	chatReplayTurnTimeout = 30 * time.Second
	// chatReplayQuiescence is how long the stub's request count must hold still
	// before the driver reads a turn as OVER. It exists because a recording's
	// turn may hold MORE responses than the surface will ever request — the live
	// run logged a response per consumed model answer, and the trailing rows of
	// a session (a final answer, a confirmation round) are exactly the ones a
	// turn can end before — so "we reached the recorded count" is not the only
	// honest end condition. Stillness is: nothing is computed locally in a
	// replay, so a count that has not moved for this long means the loop is back
	// at its prompt.
	chatReplayQuiescence = 700 * time.Millisecond
	// chatReplaySettleQuiescence is how long the stub's request count must hold
	// still before the driver may type the NEXT turn's prompt. It is a MULTIPLE
	// of chatReplayQuiescence on purpose: chatReplayQuiescence is only long
	// enough to notice that a turn has stopped feeding the counter between two
	// of its OWN requests (a tool call's round trip is one request, the answer
	// that follows the tool result is the next), while advancing means deciding
	// that NO further request of this turn is coming. Measured (2026-10-27, the
	// live screen of session 80c07a7d): a status row reading `50s · sent` beside
	// a tool call that had not finished — the surface was still working, the
	// counter was simply quiet between rounds. Three seconds is above every
	// recorded inter-request pause in that session and far below the bound.
	chatReplaySettleQuiescence = 3 * time.Second
	// chatReplayWitnessWait is how long a PREFIX turn waits for the run's own
	// turn-close statement before it falls back to the plain settle
	// (chat_replay_witness.go). It is a REAL bound on a real wait, and it is
	// shorter than chatReplaySettleQuiescence on purpose: the statement is about
	// a turn this surface has already drawn, so it arrives within a frame of the
	// answer's tail being committed; a surface that says nothing within the bound
	// has bought nothing and the ordinary path resumes.
	//
	// IT IS ADDITIVE, AND THAT IS STATED RATHER THAN WISHED AWAY (measured
	// 2026-11-04, after writing the opposite here first). waitTurnSettled starts
	// its stillness clock at the moment it is CALLED (`lastChange := time.Now()`),
	// so a prefix turn whose witness never arrives pays this bound AND then the
	// settle: 1s + 3s instead of 3s. That is a real cost, it lands only on
	// `--from-turn` runs whose REPL never reports a turn closed, and it is
	// accepted for one reason: such a run is already not replaying anything
	// (`--from-turn` on a surface that closes no turns has no prefix to hurry),
	// and the alternative — an unbounded wait for a statement that may never
	// come — hangs a terminal. The DEFAULT path (no `--from-turn`) never reaches
	// this function at all, so it pays nothing.
	chatReplayWitnessWait = 1 * time.Second
	// chatFlagFromTurn is `--from-turn N`: begin the run AT the recording's Nth
	// operator turn.
	//
	// IT IS A TURN WORD, NEVER A ROUND WORD. A turn is the operator's own row
	// and routinely holds several rounds (one `request` row is one HTTP round —
	// the operator's 2026-10-12 ruling), so "start at turn N" means "the turns
	// before N are behind you", rounds included. The `--step`/`--round` concepts
	// this surface once had are NOT revived by it.
	chatFlagFromTurn = "--from-turn"
)

// chatReplayFromTurnNeedsValue / ...NotANumber / ...NotPositive are the three
// refusals this flag can produce. Each is a NAMED sentence (Rule 15): a typo
// that silently fell back to 1 would re-run the whole recording and look as if
// the flag had been obeyed.
const chatReplayFromTurnNeedsValue = chatFlagFromTurn + " needs a turn number — write " + chatFlagFromTurn + " N (1-based, N >= 1)"

// chatParseFromTurnArg consumes `--from-turn N` and `--from-turn=N`. It reports
// whether the word was this flag's at all, so the argv walk needs no second
// spelling of the name at the call site (Rule 1b/13).
func chatParseFromTurnArg(a, next string, hasNext bool) (n int, consumed bool, err error) {
	if a != chatFlagFromTurn && !strings.HasPrefix(a, chatFlagFromTurn+chatFlagValueSep) {
		return 0, false, nil
	}
	val := ""
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		val = a[k+1:]
		if val == "" {
			return 0, true, fmt.Errorf("%s", chatReplayFromTurnNeedsValue)
		}
	} else {
		if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
			return 0, true, fmt.Errorf("%s", chatReplayFromTurnNeedsValue)
		}
		val = next
	}
	parsed, convErr := strconv.Atoi(strings.TrimSpace(val))
	if convErr != nil {
		return 0, true, fmt.Errorf("%s takes a turn number and %q is not one — write %s N (1-based), e.g. %s 3", chatFlagFromTurn, val, chatFlagFromTurn, chatFlagFromTurn)
	}
	if parsed < 1 {
		return 0, true, fmt.Errorf("%s is 1-based and starts at 1, so %d names no turn — the recording's first turn is turn 1", chatFlagFromTurn, parsed)
	}
	return parsed, true, nil
}

// chatReplayFromTurnNotice says, at start time, WHICH turn this run begins at
// and — the part that matters — that the turns before it are still TYPED, not
// skipped. A run that said only "from turn 3" would read as "turns 1-2 were not
// played", and the screen it goes on to draw contains them.
const chatReplayFromTurnNotice = chatReplayNotice + "starting at turn %d of %d: the turn(s) before it are still typed and painted (a turn's screen carries the rows the earlier turns committed), only their inter-turn settle is shortened — the screen from turn %d on is the recording's own\n"

// chatReplayNotice prefixes every sentence this verb prints about the run it is
// driving, so a reader can tell the replay's own diagnostics from the surface's.
const chatReplayNotice = "[gopi replay] "

// replayRefusalNotice is the ONE sentence that says WHAT this run will refuse to
// write, printed at arm time. It is not decoration: every writer below answers
// internal/replaymode's flag by doing NOTHING, and a run that said nothing would
// make "the replay wrote nothing" and "the write went somewhere I did not look"
// the same observable (Rule 15). The list comes from replayRefusalCatalogue(),
// so the sentence and the code that enforces it cannot drift apart.
const replayRefusalNotice = chatReplayNotice + "read-only in force for the operator's profile: the writes below are short-circuited, not performed (one is REDIRECTED instead of dropped: the screen dump) — %s (recording: %s)\n"

// replayRefusalCatalogue is the ONE spelling of the set of writes this process
// refuses (or redirects) while it is re-running a recording. Each entry names the
// write and the function that answers the flag; the devdoc table
// (devdoc/chat-replay-readonly.md) carries the same rows with their evidence.
//
// THE FORM IS A LIST BECAUSE THE CLAIM IS A SET. "a replay writes nothing at all"
// is only as good as the enumeration behind it, and an enumeration kept in prose
// drifts from the code at the first new writer. What replaced the prose is a
// MACHINE, not diligence: TestChatReplayDumpCatalogueIsExhaustive
// (chat_replay_dump_catalogue_test.go) walks every production call site of
// replaymode.Active(), requires each to be named in replayActiveSiteRows (one row
// per site, with the catalogue phrase it is announced as), and requires every one
// of those phrases to appear in THIS function's output — so a new gate with no
// catalogue entry fails the suite, and a catalogue entry with no gate fails it
// too. A comment inviting a reader to do that comparison is exactly the claim
// that turned out to be false for the screen dump: it was invited, and nobody
// came.
func replayRefusalCatalogue() string {
	return strings.Join([]string{
		"the profile's chat slot registry (" + ChatSessionSlotPrefix + "*)",
		"the editor history file (" + chatHistoryFileName + ")",
		"the chats table in the operator's ledger (RegisterChatSession/RecordChatTurn/CloseChatRow)",
		"every tool dispatch (answered from the recording through agent.SetToolExecutor)",
		"every auto-memory store into the operator's ledger (the notes and captured responses, plus the memory tool's own store action — internal/agent's memoryStoreReplayRefused)",
		"the memory prefetch itself (chatPrefetchFor builds NOTHING while this flag is set — one announcement per run — and the per-lookup guards behind it, chatPrefetch.fetch and the sealed-turn startTurnLookup, remain as defence in depth)",
		"this run's own session record (agent.NewRawLoggerForRun / agent.NewSessionLoggerForRun)",
		// THE SCREEN DUMP IS THE ONE ENTRY THAT IS REDIRECTED RATHER THAN
		// REFUSED, and the wording says so: the artifact still exists, it just
		// cannot land in the recording tree (cmd/agent/chat_dump.go,
		// chatReplayDumpPath). Before this row the dump was the ONE writer that
		// never asked the flag at all, and it resolved to
		// RecordContainer(ag.SessionLog.Path())/chat-screen.dump — inside the
		// recording it was re-running.
		"the screen dump (redirected, not dropped: chatResolveDumpPath answers chatReplayDumpPath while this flag is set, so the dump is written to the chat's own scratch class and never beside the recording it re-runs)",
		// THE RAW SGR BYTE STREAM is the SECOND redirected write, redirected for
		// the same measured reason (cmd/agent/chat_sgr_stream.go,
		// chatResolveSgrPath): a replay READS the recording, so an armed stream
		// must never be appended beside the very bytes it is re-running. Its
		// tail is spelled exactly as the dump entry's tail because the two
		// writes are one claim — a catalogue that said "beside the recording"
		// for one artifact and something else for the other would be two
		// claims, and only one of them could be checked.
		"the raw screen byte stream (redirected, not dropped: chatResolveSgrPath answers chatReplaySgrPath while this flag is set, so the stream is written to the chat's own scratch class and never beside the recording it re-runs)",
		// THE START-UP GROUND LINE is the ONE entry here that names no write at
		// all, and it is listed because the sentence it replaces DID claim one
		// (measured: `gopi chat --replay <id>` printed "this session writes in
		// <dir> … every read/write/edit/bash round lands uncommitted in that
		// checkout's working area" while the catalogue above says every write is
		// short-circuited — one stderr stream making two contradictory claims).
		// The flag does not stop this line; it CHANGES it, and the machine in
		// chat_replay_dump_catalogue_test.go requires that every site asking the
		// flag is announced here, so the change is announced too: while this
		// replay is armed the line states that this run writes nothing and names
		// only the directory it stands in (cmd/agent/chat_ground.go,
		// tools.ChatGroundReadOnlyAnnouncement).
		"the start-up ground line (not a write, but a sentence this flag CHANGES: a replay is told it writes NOTHING and where it stands, never the ordinary line's read/write/edit/bash claim)",
	}, "; ")
}

// chatReplayNoSessionNotice is the consequence of the last catalogue entry, said
// once on the run's own stderr: a replay keeps no record of itself, so there is
// no session file to name and nothing for `gopi session list` to show. Without
// this line a console that prints no session path is indistinguishable from one
// whose session failed to open (Rule 15).
const chatReplayNoSessionNotice = chatReplayNotice + "no session record is kept for this run: the recording at %s is read in place, and this run mints no session directory, no session JSONL, no metadata.json and no raw log\n"

// chatReplayUses is the notice that says WHICH recording is being re-run and how
// much of it the script holds — printed before the surface starts, so the run is
// attributable from its first frame.
const chatReplayUses = chatReplayNotice + "re-running session %s from %s (%d turn(s), %d scripted response(s), %d recorded tool result(s))\n"

// chatReplayEnvNotice names the ONE environment override this verb performs, and
// that it is restored when the run ends (Rule 15: an override that changed how a
// run talked to the world is announced, never left to be inferred).
const chatReplayEnvNotice = chatReplayNotice + "model base URL bound to the in-process stub at %s (no provider is contacted; the override is restored when this run ends)\n"

// chatReplayBudgetNotice states the injection budget this run derived from the
// recording, and what it derived it from. It is printed because the number is
// otherwise invisible while it is being spent — the refusal it prevents is the
// error an operator would see instead — and because a budget sized from a FILE
// must be checkable against that file (the script's own unit/turn/byte count).
const chatReplayBudgetNotice = chatReplayNotice + "injection budget: %d key unit(s) over %d recorded turn(s) plus the exit line, derived from %d byte(s) of recorded text — this period's key ceiling is %d\n"

// chatReplayAbortNotice is the sentence the REPL prints when it leaves because
// the replay driver gave up: the driver's own reason is on stderr above it, and
// this line says which decision that reason caused (Rule 15: a run that ends for
// a reason it did not state is indistinguishable from a crash).
const chatReplayAbortNotice = chatReplayNotice + "the replay driver stopped without finishing — this session ends now with a failure exit rather than waiting at a prompt no line can reach\n"

// chatReplayToolsNotice announces the ONE thing this verb decides about the
// surface: the recording contains tool calls, so the re-run runs the tool-enabled
// turn runner (Agent.ChatOps) — the same runner the recorded session used. This
// is a turn-runner choice, never a renderer one.
const chatReplayToolsNotice = chatReplayNotice + "the recording contains tool calls — the tool-enabled turn runner is on for this re-run; every call is answered from the recording and never dispatched\n"

// chatReplayShortfallNotice is the honest end of a turn whose responses did not
// all arrive: the run says how many it saw instead of pretending the turn matched
// the recording (Rule 15).
const chatReplayShortfallNotice = chatReplayNotice + "turn %d of %d: the surface consumed %d of %d recorded response(s) and then went quiet for %s — the turn ended before its recorded script did; the replay advances anyway (the alternative is a hang) and the run's closing summary states the cost\n"

// chatReplayTurnTimeoutNotice is the OTHER way a turn ends short: the bound
// expired while the count was still moving. It is a different sentence because it
// is a different fact — the surface was still working when the driver ran out of
// patience, not idle with responses on the table (Rule 15: one sentence per
// cause).
const chatReplayTurnTimeoutNotice = chatReplayNotice + "turn %d of %d: the bound (%s) expired with %d of %d recorded response(s) served and the surface still working — the replay advances and reports the shortfall\n"

// chatReplayMidTurnNotice is the driver's own stop reason when the PREVIOUS turn
// never stopped asking: typing now would put the next prompt inside a turn that
// is still working. It is a refusal with a cause, not a timeout message.
const chatReplayMidTurnNotice = chatReplayNotice + "turn %d of %d: the previous turn was still asking for model responses after %s — this replay stops here instead of typing the next prompt into a turn that has not finished\n"

// chatReplayNoEditorNotice is the refusal for a run with no reserveable layout.
// A replay is a film of the chat LAYOUT: with no editor (or a terminal too small
// for the region) there is no band, no status row and no dump frames, so the run
// would produce a transcript that claims to be a replay and shows none of it.
//
// IT CARRIES THE MEASURED CAUSE, not a menu of possibilities: the argument is
// chatInputNoLayoutReason's answer (lineedit_wire.go is where the cause is
// decided). It also names what is MISSING — the playback controls live at the
// editor's key seam, so on this path there are no keys to install and the hint
// line will never be announced — which is the fact a watcher waiting for that
// hint needs to be told (Rule 15).
const chatReplayNoEditorNotice = chatReplayNotice + "this run has no interactive editor, so the playback controls (pause/step/help/quit) are NOT installed and their key hint will never appear — reason: %s; a replay films the chat LAYOUT, so it is refused here rather than producing a transcript that looks like one\n"

// chatReplayNeedsSession is the refusal for a `--replay` with no value.
const chatReplayNeedsSession = chatFlagReplay + " needs a session — write " + chatFlagReplay + " SESSION (a session id or a session JSONL path)"

// chatReplaySeedConflict is the refusal for a replay BESIDE a positional seed:
// the two ask for different conversations and nothing here may silently pick one.
const chatReplaySeedConflict = "%s names the session to re-run (%q) and the %s %q names a session to continue — a replay re-runs its own recording and continues nothing"

// chatReplayUsage is the replay family's own block. It renders on the SCOPED page
// (`gopi chat --replay -h`, chat_replay_help_scope.go) — the page that documents
// the family as one unit — and the ordinary session page points at that page
// instead of repeating this text.
const chatReplayUsage = "  " + chatFlagReplay + " SESSION   re-run a RECORDED session on the real chat surface: the\n" +
	"             model's answers and tool results come from the recording (an\n" +
	"             in-process stub; no provider is contacted and no tool is\n" +
	"             dispatched), and each recorded operator turn is typed into the real\n" +
	"             editor. Use it to audit or film a session that already happened.\n" +
	"  " + chatFlagFromTurn + " N   begin at the recording's Nth operator TURN (1-based). The turns\n" +
	"             before N are still typed and painted — a turn's screen carries the\n" +
	"             rows the earlier turns committed — so the screen from turn N on is\n" +
	"             the recording's own; what shortens is only their inter-turn settle.\n" +
	"             A TURN is the operator's own row and holds its ROUNDS: \"from turn 3\"\n" +
	"             skips every round of turns 1-2.\n"

// chatParseReplayArg consumes the `--replay` switch and its value, in either
// spelling (`--replay ID` and `--replay=ID`). It is the value-carrying twin of
// chatParseResumeArg, and it exists as a function for the same reason: the argv
// walk must consume the value in the next slot, which a ranged loop cannot
// express.
func chatParseReplayArg(a, next string, hasNext bool) (target string, consumed bool, err error) {
	if a != chatFlagReplay && !strings.HasPrefix(a, chatFlagReplay+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%w", errReplayNeedsSession()) //nolint:nilerr // the refusal IS the return value here
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%w", errReplayNeedsSession()) //nolint:nilerr // the refusal IS the return value here
	}
	return next, true, nil
}

// errReplayNeedsSession returns the missing-value refusal as an error, so both
// spellings of the parse path produce the SAME sentence (one definition, Rule 13).
func errReplayNeedsSession() error { return fmt.Errorf("%s", chatReplayNeedsSession) }

// chatApplyReplayEnv points this PROCESS at the replay stub and returns the
// restore function. It is the ONE writer of the override, and it saves and
// restores BOTH spellings the client resolves (ANTHROPIC_BASE_URL and
// DEEPSEEK_BASE_URL): restoring only the first would leave a machine with
// DEEPSEEK_BASE_URL set talking to a closed loopback port after the replay ends.
func chatApplyReplayEnv(stubURL string) (func(), error) {
	keys := []string{chatReplayBaseURLEnv, chatReplayBaseURLFallbackEnv}
	prev := make(map[string]*string, len(keys))
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			val := v
			prev[k] = &val
			continue
		}
		prev[k] = nil
	}
	// The writes are CHECKED, not discarded: `os.Setenv` fails on a malformed
	// name or a value with a NUL, and a replay whose override did not take would
	// then talk to the operator's REAL provider while its own notice claimed the
	// stub was bound — the one failure this whole verb must not have. So the
	// initial bind reports and the restore reports; neither is silenced (Rule 15).
	if err := os.Setenv(chatReplayBaseURLEnv, stubURL); err != nil {
		// REFUSED, not degraded: a replay that cannot bind the stub would talk to
		// the operator's REAL provider while its own notice claimed otherwise.
		return func() {}, fmt.Errorf("bind the stub base URL into %s: %w", chatReplayBaseURLEnv, err)
	}
	if err := os.Unsetenv(chatReplayBaseURLFallbackEnv); err != nil {
		chatNoticef(chatReplayNotice+"could not clear %s: %v\n", chatReplayBaseURLFallbackEnv, err)
	}
	return func() {
		for _, k := range keys {
			if v := prev[k]; v != nil {
				if err := os.Setenv(k, *v); err != nil {
					chatNoticef(chatReplayNotice+"could not restore %s: %v — set it back before running a live session\n", k, err)
				}
				continue
			}
			if err := os.Unsetenv(k); err != nil {
				chatNoticef(chatReplayNotice+"could not unset %s: %v — clear it before running a live session\n", k, err)
			}
		}
	}, nil
}

// The two env names the client's base-URL resolution reads. They are spelled
// here as constants (Rule 1b) and TestChatReplayBaseURLEnvMatchesTheResolver
// asserts they are the ones internal/deepseek actually consults — a rename over
// there with no rename here would leave the stub unreachable and the replay
// talking to the operator's real provider.
const (
	chatReplayBaseURLEnv         = "ANTHROPIC_BASE_URL"
	chatReplayBaseURLFallbackEnv = "DEEPSEEK_BASE_URL"
)

// chatReplay is the driver: one start/replace goroutine that types the recorded
// turns into the REAL editor and waits on the STUB's own counters, plus the
// request-count predicate the driver waits for.
//
// WHY THE STUB'S COUNTER AND NOT A TIMER. "The turn is over" has exactly one
// honest witness in a replay — the model request that closed it arrived and was
// answered — and the stub is the thing that sees it. A sleep long enough to
// cover a real answer would make the film a function of the machine's mood; the
// counter makes it a function of the recording.
type chatReplay struct {
	rec  *replaydrive.Recording
	stub *replaydrive.Stub
	done chan struct{}

	// beats is this run's beat-protocol driver when one is installed, nil
	// otherwise (chat_replay_beats.go). It is the run's OWN turn-over witness:
	// see run().
	beats *chatReplayBeatDriver

	turns     int
	typed     int
	shortfall int
	err       error

	// gaps is one row per turn that ended before consuming its recorded
	// responses — the per-turn half of the completion statement
	// (chat_replay_account.go). It is filled while the turns are typed, in turn
	// order, and read ONCE at closeout: the summary must be able to say WHICH
	// turn lost the script, not only that the whole run did.
	gaps []chatReplayTurnGap

	// abort is closed when — and ONLY when — this driver ends in FAILURE, and it
	// is the REPL's way out of a replay it can no longer drive.
	//
	// IT EXISTS BECAUSE STOPPING IS NOT ENDING (measured 2026-10-04). The old
	// shape closed `done` and returned: the driver was finished, the REPL sat at
	// its prompt with nobody left to type, and the film driver waited for a
	// completion marker that is written only when the REPL itself returns — the
	// same process, the same session, 7h06m of nothing. A driver that gave up has
	// to say so to the loop it was driving, and a channel is the only thing that
	// can wake a select already parked on a line that will never come.
	//
	// NIL WHEN THE RUN SUCCEEDED, and that is the whole of the distinction: a
	// successful driver has typed `/quit`, so the REPL is already leaving through
	// its own closeout and must not be told twice.
	abort     chan struct{}
	abortOnce sync.Once

	// fromTurn is the ordinal (1-based) of the FIRST turn this run plays in
	// full, and 0 when the whole recording plays. It is set from
	// `--from-turn N` and read in run(): turns BEFORE it are the PREFIX.
	//
	// THE PREFIX IS STILL TYPED AND STILL PAINTED. It has to be: the screen that
	// closes turn N carries every earlier turn's rows (measured 2026-10-27: the
	// frame that ends turn 3 of session d6120d04 still holds turn 1's and turn
	// 2's seam rows and turn 2's answer band), so a run that skipped the
	// prefix's typing would draw a screen the recording never had. What the
	// prefix does NOT owe is the long inter-turn settle, which exists to protect
	// a turn the watcher is about to read.
	fromTurn int

	// witness is this run's REPL, as a turn-close counter (chat_replay_witness.go),
	// and it is what makes the prefix shortcut reachable WITHOUT `--film`. It is
	// nil for a driver built by a test that does not want one.
	//
	// WHY IT EXISTS (measured 2026-11-04): the shortcut used to require a beat
	// driver, and a beat driver is installed IFF `--film` was given — so on the
	// ordinary path `--from-turn N` changed nothing but a notice line (13.789s
	// without the flag, 13.798s with it, 13.807s at N=3 on session d6120d04).
	// The witness is the same FACT the beat would have carried (this turn is
	// closed), reported by the surface that closes it.
	witness *chatTurnWitness

	// injectBound is the ceiling this run's injector was OPENED at, and the
	// ceiling this driver's own injections are charged against
	// (chat_replay_budget.go). Carried on the driver so the typing and the
	// injector's construction cannot read two different numbers.
	injectBound int

	// control is the run's PLAYBACK CONTROL layer (chat_replay_control.go), nil
	// for a driver built by a test that does not want one. It is consulted
	// before EVERY keystroke this driver types, so a paused run types nothing,
	// and it is asked whether the watcher left, so `q`/Esc ends the replay at the
	// beat it was on. The layer owns the watcher's keys at the editor's key seam;
	// this driver only obeys it.
	control *chatReplayControl

	// heldBeats / heldAt ARE DELETED (2026-10-20) with the cursor they counted:
	// they tallied beats the watcher held back by stepping, and stepping is no
	// longer a thing the surface offers.
}

// waitBeatTurn waits (bounded) for the REPL to close turn n — the beat the run
// emits once that turn's own screen is final. It reports whether the turn was
// closed before the bound; a false is REPORTED by the caller, never swallowed.
func waitBeatTurn(d *chatReplayBeatDriver, n int, timeout time.Duration) bool {
	if d == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if d.turnsClosed() >= n {
			return true
		}
		time.Sleep(chatBeatPollMS * time.Millisecond) //stylegate:allow-sleep polling the run's own turn-end beat
	}
	return false
}

// startReplay builds the stub and the restore hook for a replay target. It
// returns the driver and the cleanup the caller must defer: closing the stub
// releases the loopback listener, and the restore puts the operator's own
// environment back.
func startReplay(target string) (*chatReplay, func(), error) {
	path, err := resolveReplayRecordingPath(target)
	if err != nil {
		return nil, nil, err
	}
	rec, err := replaydrive.LoadRecording(path)
	if err != nil {
		return nil, nil, err
	}
	// THE BUDGET IS LATCHED HERE — from the recording itself, before anything is
	// typed — because this is the last moment the script's length is known and
	// the last moment the injector's ceiling can still be established (the REPL
	// builds the injector after this returns). chat_replay_budget.go carries the
	// measured 64-key deadlock this closes.
	bound := recordChatReplayInjectBound(rec)
	stub := replaydrive.NewStub(rec)
	// The stub's OWN notices ride the run's notice channel rather than the raw
	// stderr (chat_notice_sink.go): a self-filming replay has the reserved layout
	// painting absolute rows onto that terminal, and the stub writes from the
	// server's own goroutine, so a bare stderr write is photographed into the
	// frames. A run that is not filming routes nothing and the writer is stderr.
	stub.Notice = chatNoticeWriter
	restore, envErr := chatApplyReplayEnv(stub.URL())
	if envErr != nil {
		stub.Close()
		return nil, nil, envErr
	}
	cleanup := func() {
		restore()
		stub.Close()
		replaymode.Disarm()
	}
	units, turns, bytes := chatReplayInjectPlan(rec)
	chatNoticef(chatReplayUses, rec.SessionID, rec.Path, len(rec.Turns), rec.ResponseCount(), len(rec.Results))
	chatNoticef(chatReplayEnvNotice, stub.URL())
	chatNoticef(chatReplayBudgetNotice, units, turns, bytes, bound)
	// THE PLAYBACK CONTROL LAYER, built with the run (chat_replay_control.go).
	// It is created HERE, at the same moment the driver is, because the two are
	// one surface: the layer OWNS the watcher's keys (installed at the editor's
	// key seam the moment the editor exists) and the driver asks it, before every
	// keystroke, whether it may type. A replay with no witness on its keys would
	// be the one-way performance this layer exists to end.
	//
	// THE CONTROLS ARE ANNOUNCED LATER, by the driver itself (run()), and that
	// order is deliberate rather than tidy: this function runs BEFORE a filmed
	// run has moved its notices off the photographed screen
	// (routeChatNoticesToConsoleLog, cli_chat.go), so announcing here would print
	// the keys onto the terminal a film is photographing. Announcing from the
	// driver puts the line through whatever destination the run has chosen by
	// then — the session's console.log under --film, the operator's terminal
	// otherwise — which is exactly what chat_notice_sink.go exists to decide.
	control := newChatReplayControl(chatNoticef)
	// ARM THE PROCESS-WIDE REPLAY FLAG (internal/replaymode) — THE authoritative
	// fact every write path consults. It is armed HERE, the moment the recording
	// is loaded and before the agent (and so the session logger, the surface, the
	// slot registry and the chats-table hooks) exists, because a flag armed after
	// the writer it is meant to stop has already run is prose and not a
	// guarantee. The cleanup disarms it, so the flag describes the run IN FLIGHT
	// rather than the process's history.
	//
	// THE REFUSALS ARE ANNOUNCED ONCE, HERE, and this is the one place they can
	// be announced honestly: every writer below answers the flag by doing
	// nothing, and a run that says nothing at all would make "the replay wrote
	// nothing" and "the write went somewhere I did not look" the same observable
	// (Rule 15). The list is spelled once (replayRefusalCatalogue) so the
	// sentence and the code that enforces it cannot drift.
	replaymode.Arm(rec.Path)
	chatNoticef(replayRefusalNotice, replayRefusalCatalogue(), rec.Path)
	return &chatReplay{
		rec: rec, stub: stub, done: make(chan struct{}), turns: len(rec.Turns),
		abort: make(chan struct{}), injectBound: bound, control: control,
	}, cleanup, nil
}

// resolveReplayRecordingPath accepts a session id (resolved through the SAME
// session resolver every other verb uses — `gopilog.FindSession`, the dual-read
// across all three session layouts) or a direct path to a session JSONL.
//
// THE DEPENDENCY IS THE JSONL, NOT THE RAW LOG, and that distinction was a
// measured defect rather than a preference (replay7): the first version routed a
// session id through `resolveResumeLogPath`, which is built for RESUME and needs
// the raw HTTP log — so `--replay <session-id>` refused to run on any session
// that has no raw log, which is the COMMON case (the raw logger is optional) and
// the exact case a replay must support: the session JSONL is the recording, and
// it is written for every session. The raw-log path is therefore the FALLBACK
// (an exotic layout where the JSONL is not named by the id), never the route.
func resolveReplayRecordingPath(target string) (string, error) {
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	dir, dirErr := gopilog.ResolveSessionDir()
	if dirErr == nil {
		if p, err := gopilog.FindSession(dir, target); err == nil && p != "" {
			return p, nil
		}
	}
	// Fallback: the resume-era resolver answers a RAW LOG path for some layouts,
	// so a raw log whose sibling is a session JSONL still resolves.
	p, err := resolveResumeLogPath(target)
	if err != nil {
		return "", fmt.Errorf("replay target %q: %w", target, err)
	}
	if strings.HasSuffix(p, ".jsonl") {
		return p, nil
	}
	if sib := replaySiblingSessionJSONL(p); sib != "" {
		return sib, nil
	}
	return p, nil
}

// run drives the whole replay: type turn 1, wait for its responses, type turn 2,
// … then the operator's own quit word — so the session ends the way a human's
// does (the same finishChat closeout, the same goodbye, the same single
// session_end).
func (r *chatReplay) run(input *chatInput, beats *chatReplayBeatDriver) {
	r.beats = beats
	defer close(r.done)
	// THE DRIVER'S OWN EXIT CONDITION, in ONE defer: a driver that ends in
	// FAILURE tells the REPL, and a driver that ends by typing /quit does not.
	// That split is why this is a deferred decision rather than a call at each
	// `return` — the failure paths are several (a refused key, a failed emit, a
	// bad input layer) and a per-path signal is how one of them ends up silent,
	// while the success path must NOT signal at all.
	defer func() {
		// THE WATCHER'S QUIT IS AN ENDING, and it is decided HERE, in the same
		// one defer as the failure arm, for the reason that put the failure arm
		// here: the paths that obey a quit are several (the stop check before a
		// turn, the one before each chunk, the one before the Return), and a
		// per-path signal is how one of them ends up silent.
		//
		// WHAT WAS MEASURED WRONG WITHOUT THIS (2026-10-04, the operator's
		// "confusing mode"): the driver stopped typing, said "the watcher left
		// the replay", and returned — while the REPL went on living at a prompt
		// whose answers still came from the recording. Nothing could end that
		// run: in a replay the ONLY source of a line is the recording, and the
		// recording had stopped. The run is ended here instead, through the same
		// abort channel a failure uses, so the loop's ONE exit arm serves both
		// and the closeout is the ordinary one.
		if r.err == nil && r.control != nil && r.control.leftByWatcher() {
			// THE MODE FLIP, BEFORE the REPL hears about it: from here on the
			// key gate claims nothing but a repeat quit, so the watcher's next
			// keystroke is the REPL's own — which is what makes the surface they
			// see and the surface that answers them the same one.
			r.control.markClosing()
			chatNoticef(chatReplayLeftNotice)
			if r.abort != nil {
				r.abortOnce.Do(func() { close(r.abort) })
			}
		}
		if r.err != nil {
			// THE MODE FLIP BELONGS TO THE FAILURE ARM TOO, and for the same
			// reason the quit arm has it: this driver is over. What differs is
			// only how the run ends (the abort channel a failure uses). Without
			// this call a driver that died on a refused key would leave the
			// editor's line HELD while the REPL took the prompt back — the
			// watcher's first characters would be dropped by a lock whose owner
			// no longer exists, which is the silent failure this file removes
			// and not a new one to add (Rule 15).
			if r.control != nil {
				r.control.markClosing()
			}
			chatNoticef("%v\n", r.err)
			// Only a driver that has a REPL to tell: a driver built by a test
			// carries no abort channel and must not panic on the way out.
			if r.abort != nil {
				r.abortOnce.Do(func() { close(r.abort) })
			}
		}
	}()
	if input == nil || input.Mode() != chatInputEditor || !input.bandActive() {
		// THE CAUSE IS NAMED, NOT LISTED. This refusal used to spell a menu of
		// three possibilities ("piped input, --no-editor, or a terminal too
		// small") because the mode string cannot tell them apart — and the
		// fourth cause, a terminal that opened but whose layout was refused,
		// was not in the menu at all. chatInput.noLayoutReason is where that
		// measurement lives (lineedit_wire.go decides it, ONE place), so the
		// sentence below states WHICH cause this run met (Rule 15: a report
		// that offers a menu where one fact is known is a report that did not
		// do its job).
		//
		// It also NAMES WHAT IS MISSING rather than only refusing: the
		// playback controls are installed at the editor's key seam, so with no
		// layout there are no keys — the watcher would otherwise wait for a
		// hint line that can never come. announceStart() is never reached on
		// this path, and that is correct: a layer installed on an editor that
		// does not exist would own no keys.
		r.err = fmt.Errorf(chatReplayNoEditorNotice, chatInputNoLayoutReason(input))
		r.typed = r.turns // nothing to type: do not leave the REPL waiting on a driver that refused
		return
	}
	// THE PLAYBACK CONTROLS ARE ARMED ON THE EDITOR THE SURFACE JUST BUILT, and
	// they are armed HERE because this is the first moment the editor is known to
	// exist and to be the interactive one (the check above). The driver is
	// already running on its own goroutine, so the layer is installed before the
	// first keystroke below — a control layer installed later would leave the
	// opening beats uncontrollable.
	r.control.install(input.editor)
	// NO KEY LIST IS PRINTED (2026-10-20): announceStart() is deleted with the
	// arrows it advertised. The surface opens on the recording, and the pause line
	// names the two keys that exist the first time one of them matters.
	// The replay's OWN key period, opened at the ceiling this run derived from
	// the recording (chat_replay_budget.go). It runs AFTER the input layer exists
	// (there is nothing to reset before that) and BEFORE the first turn, so the
	// very first injected unit is charged against the script's budget rather
	// than against the model's.
	if !applyChatReplayInjectBudget(input.injector) {
		r.err = fmt.Errorf("%s this run's editor has no key injector — the recorded turns cannot be typed", chatReplayNotice)
		r.typed = r.turns
		return
	}
	// EVERY TURN BEGINS BY ASKING THE WATCHER'S CONTROL LAYER, and the order is
	// the whole of what pause means: the driver parks BEFORE the turn's first
	// keystroke, so a paused replay types nothing and the picture stays on the
	// beat it was on. The same call is what lets `q`/Esc end the run — the layer
	// reports the watcher left, and the driver stops here rather than typing the
	// rest of the recording at somebody who walked away.
	if r.stopped(input) {
		return
	}
	count := func() int { return r.stub.RequestCount() }
	for i, turn := range r.rec.Turns {
		// A PAUSE TAKEN MID-RECORDING IS OBEYED AT THE NEXT BEAT, which is the
		// granularity a beat loop has: the turn in flight finishes (nothing can
		// un-type a keystroke the editor already read), and the next one waits.
		if r.stopped(input) {
			return
		}
		want := r.rec.RequestsThroughTurn(i)
		// NOTHING CAN HOLD A BEAT BUT THE PAUSE (2026-10-20): the step-back the
		// cursor used to offer is gone, so there is no "the watcher is behind this
		// beat" state left. The pause is served by stopped() above — it parks the
		// driver before the turn's first keystroke — and that is the whole of the
		// interrupt.
		// THE NEXT TURN WAITS FOR THE LAST ONE TO STOP ASKING — bounded, and
		// never satisfied by a short silence (waitTurnSettled's own comment
		// carries the measured `50s · sent` screen this exists to respect).
		// A run that cannot reach that state ends here with its reason stated,
		// rather than typing into a turn that is still executing a tool call.
		// A turn at or after the start point waits on the same bound a plain
		// replay uses; a turn BEFORE it is the prefix. This turn is IN THE
		// PREFIX and its end was witnessed by the beat the
		// REPL emitted for it (the previous iteration's waitBeatTurn):
		// advancing needs no quiet wait, because the condition that wait exists
		// to establish is already measured. See prefixAdvanceIsWitnessed.
		// NOTE: this is a guard AROUND THE WAIT, never a `continue` — a continue
		// here would skip the turn's own typing, which is exactly the kind of
		// "fast path" that draws a screen the recording never had.
		// THE TURN THIS WAIT IS ABOUT IS THE PREVIOUS ONE — turn number `i`,
		// because the turn about to be typed is number i+1. The guard passed
		// `i+1` until 2026-11-04, i.e. it asked whether the turn it was ABOUT TO
		// type was a prefix turn, so `--from-turn N` could only ever shorten the
		// waits for turns strictly below N-1: with N=2 it shortened NOTHING, and
		// with N=3 exactly one wait instead of two.
		if i > 0 && !r.prefixClosureWitnessed(chatReplayWaitTurn(i+1)) {
			if _, settled := waitTurnSettled(i+1, want, count); !settled {
				r.err = fmt.Errorf(chatReplayMidTurnNotice, i+1, r.turns, chatReplayTurnTimeout)
				break
			}
		}
		if err := r.typeTurn(input, i+1, turn); err != nil {
			r.err = err
			break
		}
		r.typed = i + 1
		// THE TURN-OVER WITNESS, in this order of preference: when this run
		// films itself (a beat driver is installed) the boundary is the beat the
		// REPL emits once the turn's own screen is final — a fact about the
		// TURN, not about the transport. Otherwise the stub's request counter is
		// all a replay has.
		//
		// WHY THE COUNTER ALONE IS NOT ENOUGH (measured): a turn with a tool call
		// is SEVERAL assistant blocks in ONE model response, and the counter moves
		// the moment the request arrives — i.e. mid-stream. Waiting on it lets the
		// next turn's keys be typed while the previous answer is still being
		// painted, which the film then records as the two interleaved.
		// THIS TURN'S OWN SHARE of the script, and the counter reading taken
		// before it was typed: the difference between that reading and the one
		// taken when the turn ends is what this turn CONSUMED. It is the only
		// honest per-turn number — the stub's counter is global and cumulative,
		// so "we reached `want`" and "this turn ate its own responses" are two
		// different questions, and the pre-fix driver only ever asked the first.
		recorded := len(turn.Responses)
		before := count()
		if r.beats != nil {
			closed := waitBeatTurn(r.beats, i+1, chatReplayTurnTimeout)
			r.closeTurn(i+1, recorded, count()-before, !closed)
			continue
		}
		if want == 0 {
			continue
		}
		after, stable := waitTurn(chatReplayTurnTimeout, chatReplayQuiescence, want, count)
		// THE QUIESCENCE ARM IS NOT A SUCCESS (measured 2026-10-27). It reports
		// that the surface stopped asking — which, when the turn still had
		// responses left, is the SHORTFALL, not an honest end. Treating
		// `stable` as "the turn is over, fine" is what let a run finish with 93
		// of 152 requests served and say `turns typed: 11/11`.
		r.closeTurn(i+1, recorded, count()-before, !stable && after < want)
	}
	// The exit line is the operator's own vocabulary, typed the same way, so the
	// session's closeout is reached exactly as a human's would reach it.
	if err := r.injectLine(input, completionSlashPrefix+quitCommandName); err != nil {
		r.err = err
		return
	}
	// THE RECORDING IS OVER THE MOMENT ITS OWN `/quit` IS TYPED, so the mode is
	// flipped HERE rather than at the driver's return: what follows is the
	// bounded settle and the REPL's own closeout, and a watcher pressing a key
	// during either is talking to the REPL — the surface that is about to end
	// the session — not to a playback that has nothing left to play.
	r.control.markClosing()
	// A bounded settle, not a wait on a condition: the loop consumes /quit
	// within a frame, and the real wait for this run to end is the caller's join
	// on r.done (finishChat). The settle exists so the line is READ while the
	// layout is still installed, which is what makes the closeout paint the way
	// a human's /quit does.
	time.Sleep(chatReplayExitSettleMS * time.Millisecond) //stylegate:allow-sleep a bounded settle so the injected /quit is read before the caller tears the surface down
}

// typeTurn types ONE recorded turn into the editor: the recorded text, a settle,
// then the recorded Return.
func (r *chatReplay) typeTurn(input *chatInput, ordinal int, turn replaydrive.Turn) error {
	if strings.TrimSpace(turn.Text) == "" {
		// A turn with no text (a seeded session's opening response, see
		// replaydrive.Turn) is nothing to type — the responses are already
		// scripted and the surface will ask for them on its own.
		return nil
	}
	chatNoticef(chatReplayNotice+"turn %d/%d: typing the recorded prompt\n", ordinal, r.turns)
	// THE FACE THE RECORDING SAYS THIS LINE ARRIVED UNDER, queued at the ONE
	// moment it can be paired with the keystrokes about to be typed. The REPL
	// pops it at the read that produces the line (chatInput.takeReplayFace), so
	// push and pop are one per line in the same order.
	// WHY IT GOES IN HERE RATHER THAN AT THE SEAL: the seal is downstream of the
	// editor, and by then the only provenance left is "a keystroke" — which is
	// `human` for every line a replay types, including the turns the recording
	// says arrived from `gopi notify`. That loss is what froze the band counter
	// (measured 2026-11-04, session 0b51ae4a-fa82-41ba-8147-1ba3ebc92476).
	input.noteReplayFace(turn.Source)
	// THE MOMENT THE ROW WAS WRITTEN rides the same queue, pushed in the same
	// call so the two stay paired entry for entry. The REPL asks the RECORD for
	// its banner's clock instead of minting one, which is what makes a replay of
	// an old session print the OLD moment: measured 2026-11-08, replaying
	// session 6b01dd46 (first turn 2026-11-07 12:11:17) painted
	// `turn 1 · 2026-11-08 15:59:32` before this push existed.
	//
	// A ZERO moment IS STILL PUSHED, unlike an empty face: "the record states no
	// moment" and "there is no record behind this line" are different facts, and
	// only the second may fall through to the live arm's clock. The banner prints
	// sessionevent.TurnStampPlaceholder for the first (chatInput.noteReplayMoment
	// carries the reasoning).
	input.noteReplayMoment(turn.OpenedAt)
	return r.injectLine(input, turn.Text)
}

// injectLine hands one line to the real editor through the production
// injector: characters in bounded chunks, a settle, then Return.
func (r *chatReplay) injectLine(input *chatInput, text string) error {
	if input.injector == nil {
		return fmt.Errorf("%s this run's editor has no key injector — the recorded turns cannot be typed", chatReplayNotice)
	}
	// THE MASK RUNS ON THE WHOLE LINE, BEFORE IT IS SPLIT, and that order is the
	// deliverable rather than a detail. The typing is a STREAM too: the line goes
	// into the real editor chatReplayCharsPerKey runes at a time, so a shape can
	// straddle two chunks exactly as it can straddle two answer deltas — and a
	// masker that judged each chunk alone would type the head of an identifier
	// into the surface and emit it as that beat's `request` row. Masking the
	// complete line first, then splitting it, is safe because the substitution
	// preserves rune counts: the chunks keep their boundaries and every one of
	// them is already masked.
	text = r.beats.maskText(text)
	// EVERY CHUNK THIS DRIVER TYPES WAITS FOR THE WATCHER'S CONTROL LAYER FIRST,
	// so a pause taken in the middle of a long recorded line stops the line where
	// it is — the picture freezes on the beat the watcher interrupted, which is
	// what "pause" has to mean for a run that is being typed at them. (A HOLD by
	// the cursor is a coarser thing and lives at the beat boundary: see
	// beatAllowed. Cutting a TURN in half would leave a half-typed line the
	// recording never contained.)
	for _, chunk := range chatReplayChunks(text) {
		if r.stopped(input) {
			return nil
		}
		res := input.injector.InjectBytesBounded(chunk, r.injectBound)
		if !res.Accepted {
			return fmt.Errorf("%s injecting %q was refused: %s", chatReplayNotice, chunk, res.Why)
		}
		// The typed chunk IS this beat's drawn event: it is emitted HERE, at the
		// call that types it, so the film's typing frames and the ledger's rows
		// are the same event and cannot drift apart into two accounts of the
		// same keystrokes (chat_replay_beats.go).
		if err := r.beats.emit(chatBeatKindRequest, chunk, 0); err != nil {
			return err
		}
	}
	time.Sleep(chatReplayEnterSettleMS * time.Millisecond) //stylegate:allow-sleep the settle is what keeps the recorded Return from being read as part of a paste burst (the editor suppresses that Enter)
	// THE RETURN OBEYS THE SAME QUESTION as the characters above: a quit or a
	// pause arriving during the settle must not be broken by the Enter.
	if r.stopped(input) {
		return nil
	}
	res := input.injector.InjectBytesBounded(chatReplayKeyEnter, r.injectBound)
	if !res.Accepted {
		return fmt.Errorf("%s injecting Return was refused: %s", chatReplayNotice, res.Why)
	}
	return nil
}

// joinChatReplay stops waiting for a replay driver and reports what it served.
// It is a no-op for a run that is not a replay (r == nil), so the ONE closeout
// can call it unconditionally — the alternative (a nil check at every exit path)
// is how one path ends up not reporting.
//
// The wait is BOUNDED: the driver stops after its own last turn, and a driver
// that somehow never stops must not turn the closeout into a hang. A timeout is
// REPORTED, never silent (Rule 15).
//
// WHAT A TIMEOUT DOES NOT DO — and this is the measured correction. The old
// sentence claimed the session "is being closed anyway", which is a claim this
// function cannot make: it is reached FROM the closeout, so the REPL has already
// decided to leave, and the driver it gave up on is still holding the editor,
// the injector and the session's goroutines. What a timeout DOES mean here is
// that the closeout stops waiting — and for a driver that never finished, the
// observable consequence is a process that does not exit (measured 2026-10-04:
// the film driver's own frame-timeout ended the FILM, while the producer process
// behind it stayed alive, re-parented to init, 7h06m at that prompt until it was
// killed by hand).
func joinChatReplay(r *chatReplay) {
	if r == nil {
		return
	}
	select {
	case <-r.done:
	case <-time.After(chatReplayJoinTimeout):
		chatNoticef(chatReplayNotice+"the replay driver did not stop within %s — the session is being closed anyway\n", chatReplayJoinTimeout)
	}
	reportReplayResults(r)
}

// chatReplayJoinTimeout bounds the closeout's wait for the driver. Named (Rule 1)
// and short: the driver's own turn waits are bounded by their own constants, so
// reaching this one means something already went wrong and the only useful thing
// left is to say so.
const chatReplayJoinTimeout = 5 * time.Second

// chatReplayKeyEnter is the byte a terminal delivers for Return (the same value
// internal/replay injects; spelled here because it is this file's vocabulary).
const chatReplayKeyEnter = "\r"

// chatReplayChunks splits text into the injector's read units. The split is
// RUNE-safe on purpose: a chunk that cut a multi-byte rune would hand the
// editor's decoder half a character, which is a byte sequence no keyboard can
// produce.
func chatReplayChunks(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	n := 0
	for _, ru := range text {
		b.WriteRune(ru)
		n++
		if n >= chatReplayCharsPerKey {
			out = append(out, b.String())
			b.Reset()
			n = 0
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// waitTurn waits for a turn to consume its recorded responses, and reports BOTH
// what it saw and which condition ended the wait:
//
//   - `want` requests arrived — the recording's script was consumed as written;
//   - or the count held still for stableFor — the surface stopped asking, which
//     in a replay (nothing is computed locally) means the turn is over. This arm
//     is what makes a recording whose turn holds MORE responses than the surface
//     requests a TERMINATING run rather than a timeout.
//
// THE SECOND ARM IS NOT A SUCCESS, and reading it as one is the measured defect
// this comment now records (2026-10-27). A quiet counter means the turn is over;
// it does NOT mean the turn consumed its script. Session 80c07a7d ended this
// run's every turn on this arm — 93 of 152 requests, 82 of 134 tool results —
// and the closeout still printed `turns typed: 11/11`. The caller therefore
// reads `stable` as "ended short", never as "fine" (run() → closeTurn), and the
// closeout compares both sides (chat_replay_account.go).
//
// A deadline that expires with the count still moving is neither, and the caller
// reports it (Rule 15).
func waitTurn(bound, stableFor time.Duration, want int, count func() int) (seen int, stable bool) {
	deadline := time.Now().Add(bound)
	last := count()
	lastChange := time.Now()
	for {
		if last >= want {
			return last, true
		}
		if time.Since(lastChange) >= stableFor {
			return last, true
		}
		if time.Now().After(deadline) {
			return last, false
		}
		time.Sleep(chatReplayPollMS * time.Millisecond) //stylegate:allow-sleep the sleep is the observation cadence of the stub's own counters, not a fixed wait for an external event
		if n := count(); n != last {
			last, lastChange = n, time.Now()
		}
	}
}

// prefixAdvanceIsWitnessed reports whether this turn's closure was witnessed by
// a FACT about the turn rather than by a timer — this run films itself and the
// REPL emitted the turn-end beat for it, OR (the ordinary path) the REPL's own
// turn-close counter already states the turn is behind it (chat_replay_witness.go)
// — in which case the prefix needs no quiet wait at all: both are emitted after
// the answer band is committed and the screen is final, which is the whole of
// what the quiet wait was standing in for.
//
// IT IS THE PREFIX'S ONLY PERMITTED SHORTCUT, and the reason it is permitted is
// that it removes a TIMER without removing a CONDITION: the condition (this turn
// is closed) still holds, it is simply reported by the surface instead of
// inferred from stillness.
//
// THE WITNESS IS ASKED, NOT ASSUMED (measured 2026-11-04). This used to return
// true on `r.beats != nil` alone, i.e. the presence of a FILM lifted the wait —
// while an ordinary run, which has no beats, kept the full settle even with
// `--from-turn` given. The measured cost: 13.789s / 13.798s / 13.807s for no
// flag / N=2 / N=3 on the same recording. Now the beat case asks turnsClosed and
// the ordinary case asks the REPL's counter, and a run whose surface has not
// stated the closure keeps the wait.
func (r *chatReplay) prefixAdvanceIsWitnessed(ordinal int) bool {
	if r == nil || r.fromTurn <= 1 || ordinal >= r.fromTurn {
		return false
	}
	if r.beats != nil && r.beats.turnsClosed() >= ordinal {
		return true
	}
	return r.witness.count() >= ordinal
}

// chatReplayWaitTurn names WHICH turn a wait is about: the one whose closure it
// is establishing, which is the turn that just ENDED — not the one about to be
// typed.
//
// IT IS A FUNCTION SO THE OFF-BY-ONE CANNOT COME BACK SILENTLY (measured
// 2026-11-04). run() used to pass the ordinal of the turn it was ABOUT TO type,
// so with `--from-turn 2` the only prefix wait in the run was judged ineligible
// and the flag shortened nothing at all; with N=3 it shortened one wait of two.
// The relationship is a named fact here and is pinned by
// TestChatReplayPrefixGuardNamesTheTurnItWaitsFor.
func chatReplayWaitTurn(typing int) int { return typing - 1 }

// prefixClosureWitnessed answers "may this prefix turn be advanced without the
// plain settle wait?" — and it ASKS the surface, bounded by the very interval the
// settle wait would have spent anyway.
//
// IT COSTS SOMETHING WHEN IT FAILS, AND THE COST IS NAMED (chatReplayWitnessWait's
// own comment carries it): the bound is spent BEFORE the plain settle begins, so
// a `--from-turn` run whose surface never reports a turn closed pays 1s + 3s
// rather than 3s on each prefix boundary. That run is not replaying a prefix
// either — there is no closure to hurry — and the default path never reaches this
// function, so the price is confined to the one case it describes.
func (r *chatReplay) prefixClosureWitnessed(ordinal int) bool {
	if r == nil || r.fromTurn <= 1 || ordinal >= r.fromTurn {
		return false
	}
	if r.prefixAdvanceIsWitnessed(ordinal) {
		return true
	}
	return r.witness.awaitTurnWitness(ordinal, chatReplayWitnessWait)
}

// waitTurnSettled waits, BOUNDED, for a turn to stop asking before it types the
// next one — without ever reading silence as completion.
//
// THE PREMATURE ADVANCE IS THE MECHANISM THIS CLOSES (measured 2026-10-27). The
// live screen of the recorded session shows a status row reading `50s · sent`
// while a tool call is still running, and a driver that advances the moment the
// request counter holds still for chatReplayQuiescence types the next prompt
// INTO that turn: the editor takes the keys, the in-flight turn's own answer
// arrives afterwards, and the recording's script advances past responses the
// surface is about to need. So the wait is the LONG bound (chatReplayTurnTimeout)
// and the condition is not "quiet for a while" but "no request for longer than
// any recorded turn has ever paused" — chatReplaySettleQuiescence, which is a
// named multiple of the short one rather than the short one itself.
//
// It never reports the turn as consumed: the caller compares its own counts
// (closeTurn). This function answers exactly one question — may I type the next
// turn now? — and a false answer ends the run instead of advancing into the
// middle of somebody's tool call.
func waitTurnSettled(turn int, want int, count func() int) (seen int, settled bool) {
	before := count()
	deadline := time.Now().Add(chatReplayTurnTimeout)
	last, lastChange := before, time.Now()
	for {
		if time.Since(lastChange) >= chatReplaySettleQuiescence {
			return last, true
		}
		if time.Now().After(deadline) {
			return last, false
		}
		time.Sleep(chatReplayPollMS * time.Millisecond) //stylegate:allow-sleep the sleep is the observation cadence of the stub's own counters, not a fixed wait for an external event
		if n := count(); n != last {
			last, lastChange = n, time.Now()
		}
	}
}

// chatReplaySiblingSessionName is the session JSONL's basename in the two
// layouts that carry one file per run: the bare `sessions/<uuid>/session.jsonl`
// layout. A raw log beside it shares the directory.
const chatReplaySiblingSessionName = "session.jsonl"

// replaySiblingSessionJSONL returns the session JSONL beside a raw log path, or
// "" when there is none (the caller then keeps the path it had and the loader
// reports what it found).
func replaySiblingSessionJSONL(rawLog string) string {
	dir := strings.TrimSuffix(rawLog, "/"+baseNameOf(rawLog))
	if dir == rawLog {
		return ""
	}
	cand := dir + "/" + chatReplaySiblingSessionName
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	return ""
}

// baseNameOf is filepath.Base without importing path/filepath into a file whose
// other uses are all string-composed (one derivation, used once).
func baseNameOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// installReplayToolExecutor wires the recording's results into the agent's
// exported executor seam and returns the reported summary line. It is a
// function (not two lines in the REPL) so the emission EXISTS even when the
// recording holds no tool call: "the recording had none" and "the seam was
// installed and never used" are different facts, and only one of them is a
// defect (Rule 15).
func installReplayToolExecutor(ag *agent.Agent, r *chatReplay) string {
	ag.SetToolExecutor(r.stub.Executor())
	if len(r.rec.Results) == 0 {
		return chatReplayNotice + "no recorded tool result in this session — the executor seam is installed and will refuse an unscripted call by name\n"
	}
	return fmt.Sprintf(chatReplayNotice+"executor seam installed: %d recorded tool result(s) will be served verbatim; the real dispatch is unreachable for this run\n",
		len(r.rec.Results))
}

// reportReplayResults prints the run's own account of what the seam served, so a
// reader can check byte-for-byte identity against the recording without reading
// this process's memory (the dump is the SCREEN's evidence; this is the wire's).
func reportReplayResults(r *chatReplay) {
	served := r.stub.ServedResults()
	var missing []string
	for _, s := range served {
		if !s.Recorded {
			missing = append(missing, s.ID)
		}
	}
	// THE COMPLETION STATEMENT IS A COMPARISON, NOT A COUNT (2026-10-27). The
	// line this replaces reported only what the run DID (`served 82 … requests
	// 93 … turns typed 11/11`) beside a recording holding 152 and 134, and a
	// reader had no way to see the gap from the line itself. Both sides are now
	// printed together and the gap is named when there is one
	// (chat_replay_account.go carries the sentences and the predicate).
	for _, line := range chatReplaySummaryLines(r.accounting()) {
		_, _ = fmt.Fprint(chatNoticeWriter(), line) //stylegate:ignore a failed write to the run's own notice channel cannot be reported: the channel IS the report
	}
	// THE BYTE-FOR-BYTE PROOF RIDES THE RUN'S OWN OUTPUT: the length and SHA-256
	// of every string the seam handed the model, in order. The recording is a
	// separate artifact, so a reader can re-derive the same digests from it and
	// compare — which is what "the model received the recorded bytes" MEANS, as
	// opposed to being asserted. Digests rather than the payloads: the payloads
	// are the recording, and quoting them here would only invite a reader to
	// compare the transcript against itself.
	for i, s := range served {
		sum := sha256.Sum256([]byte(s.Result))
		chatNoticef(chatReplayNotice+"served[%d] id=%s tool=%s bytes=%d sha256=%x recorded=%t\n",
			i, s.ID, s.Name, len(s.Result), sum, s.Recorded)
	}
	for _, id := range missing {
		chatNoticef(chatReplayNotice+"no recorded result for %s\n", id)
	}
}

// deepseekToolCallName is a compile-time assertion that the executor seam this
// file installs is the one internal/agent exports: agent.SetToolExecutor takes
// func(deepseek.ToolCall) string, and this unused declaration fails the build if
// that signature ever changes without this file following it.
var _ func(*agent.Agent, func(deepseek.ToolCall) string) = func(a *agent.Agent, f func(deepseek.ToolCall) string) { a.SetToolExecutor(f) }

// THE DRIVER'S THREE QUESTIONS TO THE WATCHER'S CONTROL LAYER, in ONE place each
// (chat_replay_control.go carries the controls themselves). They are nil-safe by
// construction — a driver built without a layer answers "no control, no pause,
// everything may be typed" — so a test can exercise the typing path without
// inventing a watcher.

// stopped reports that the watcher left the replay (q/Esc): the driver types
// nothing further and the run leaves through the ordinary closeout.
func (r *chatReplay) stopped(input *chatInput) bool {
	if r.control == nil {
		return false
	}
	// A PAUSE IS ALSO A REASON NOT TO GO ON, and it is served here rather than
	// at each call site: the layer's barrier returns when the run may advance OR
	// when the watcher left, and this call reports which.
	r.control.waitWhilePaused()
	return r.control.quitRequested()
}

// beatAllowed / sayHeld / waitForCursor ARE DELETED (2026-10-20), and their
// removal is the driver's half of this epic. They implemented "the watcher's
// cursor is behind this beat": beatAllowed asked the control layer whether the
// cursor had caught up, sayHeld wrote chatControlHeldLine (the line that offered
// "a step or a skip moves it on" — keys that no longer exist), and waitForCursor
// parked the driver until the cursor advanced.
//
// WITH THE ARROWS GONE THERE IS NO CURSOR AND NO HOLD. What remains between the
// watcher and the typing is ONE thing, the pause, and it is served in ONE place
// (stopped(), which waitWhilePaused-then-quitRequested) before every turn and
// before the run's first keystroke. A hold branch that no key can reach is the
// unexecuted surface Rule 31 names — and it was already unreachable in practice
// when the cursor was last written (a paused run never reaches here at all).

// advance() IS DELETED (2026-10-20): it told the control layer which beat the
// driver had finished (setHead), and the layer no longer keeps a position. The
// driver's progress is still counted where it always was — r.typed — and that is
// the value its own turn-over witnesses read.
