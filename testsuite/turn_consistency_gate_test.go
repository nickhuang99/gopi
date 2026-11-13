package testsuite

// turn_consistency_gate_test.go — THE TURN CONSISTENCY GATE: one recorded session,
// four consumers, one answer per WORD.
//
// WHY THIS FILE EXISTS. `turn` is declared on four different surfaces, and each
// one computed it from its own material:
//
//	consumer 1  gopi session replay <id> --turns / --turn N   (cmd/agent/cli_session_replay.go)
//	consumer 2  gopi chat --replay <id>  — the SCREEN's band   (cmd/agent/chat_replay.go)
//	consumer 3  gopi session list                              (cmd/agent/cli_session.go)
//	consumer 4  gopi session show <id>                         (cmd/agent/cli_session.go)
//
// Every defect this family produced was one of those four disagreeing with the
// others about the SAME recording, and each was found by a human reading two
// screens, not by a test. This file is the instrument that was missing: ONE frozen
// fixture, each consumer REALLY EXECUTED, and the answers compared by the words
// they print.
//
// THE OPERATOR'S CORRECTED CRITERION (2026-11-04 — it replaces "all four must say
// the same N", which was wrong):
//
//	the SAME WORD must mean the same number on every surface;
//	a DIFFERENT word may hold a different number, but each must be NAMED and its
//	reason readable in this file.
//
// The four claims, one subtest each:
//
//	① `turn N` — the page `--turn N` prints and the band the replay PAINTS are
//	  the same turn, for every N of the fixture, judged by the turn's RECORDED
//	  OPENING TEXT (never by a row count: the two surfaces have different widths).
//	② the bare word `turns` — the LOG's turn-OPENING count (the axis `--turn N`
//	  numbers along): `session show --json` and `session list --json` agree on it,
//	  `turns_provenance` names its producer, and the LEDGER's own `chats.turn_count`
//	  rides `turns_completed` BESIDE it rather than replacing it. Three surfaces
//	  reading one broken column would also agree, which is why the ledger's own
//	  WRITER is the fourth witness.
//	③ the reader pair — `session log <id> --turns` prints BOTH words under their own
//	  names (`turns` = the openings; `completed` = the ledger's) and the person-only
//	  subset cannot exceed the whole.
//	④ `N/total` — every fraction's denominator IS the consumer-1 axis total.
//
// THE NUMBERS ALLOWED TO DIFFER, each with its producer and its reason. The
// fixture is built so they DO differ (6 openings, ledger 4) — a fixture where
// they coincide cannot tell this gate from a test that asserts nothing:
//
//	WORD               PRODUCER                                 WHY IT MAY DIFFER FROM `turns`
//	turns              gopilog.SessionTurns (the log's rows)    this IS the axis (`--turn N`, the band)
//	completed          chats.turn_count (ChatRecordTurn)        advances when a turn COMPLETES; an
//	                                                             interrupted or still-running turn holds a
//	                                                             seam the ledger never charged
//	human turns        gopilog.SessionHumanTurns — a SUBSET     only turns a PERSON opened; a `gopi notify`
//	                                                             delivery and a model-typed line open turns
//	                                                             too (the notify / llm faces)
//	Passes             session_end total_iterations             the agent LOOP's count — one turn routinely
//	                                                             takes several passes
//	rounds / requests  one JSONL `request` row                  one HTTP request/response; a turn is not a
//	                                                             round and a round is not a turn
//
// NOTHING HERE CHANGES PRODUCTION BEHAVIOUR, and nothing asserts those numbers are
// EQUAL: compressing two true numbers into one false one is the defect this gate
// exists to prevent, not a fix for it.
//
// INVOCATION (repo-resident and re-runnable):
//
//	go test ./testsuite/ -run TestTurnConsistency -v -count=1
//
// The binary is resolved by testutil.GopiBin — the pin every e2e case in this
// directory drives — so the revision under test is this run's property, never
// whatever `bin/gopi` happens to be lying around.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ── the fixture's own numbers, each named (Rule 1) ──────────────────────────

const (
	// turnGateSessionID is uuid-shaped on purpose: `session log` refuses a
	// PREFIX in as many words, so a short fixture id would make claim ③
	// unreachable.
	turnGateSessionID = "fixt0000-turn-gate-0000-000000000001"

	// turnGateTurns is the log's TURN-OPENING axis — the length of the
	// `--turn N` range and the thing the band walks. SIX, in five different
	// shapes, because a fixture whose turns all look alike cannot tell the turn
	// predicate from a "count the user rows" heuristic:
	//
	//	1 the person typed it                 (source=human)
	//	2 a `gopi notify` delivery            (source=notify)
	//	3 the person typed it — a long prompt that WRAPS at any terminal width
	//	4 the person typed it, answered in THREE rounds with TWO tool calls
	//	5 a self-typed line                   (source=llm), with a `[system]`
	//	  tick riding INSIDE the turn (a tick opens nothing)
	//	6 the person typed it                 (source=human)
	//
	// The log also holds TWO `[system]` harness rows that are NOT turns, so a
	// predicate that counted every `user` row would report eight.
	turnGateTurns = 6

	// turnGateHumanTurns is the PERSON-ONLY subset: turns 1, 3, 4 and 6. It
	// must differ from turnGateTurns, or the subset has collapsed into its
	// superset and the word `human` says nothing.
	turnGateHumanTurns = 4

	// turnGateLedgerTurns is what the chats INDEX holds (chats.turn_count),
	// charged through the ledger's own writer. FOUR because a ledger charges a
	// turn when one COMPLETES: turn 2 was delivered mid-run (its row already
	// written) and turn 6 was interrupted by the session's end. The number is
	// deliberate — it is what makes `turns` and `completed` differ, which
	// is the pair claim ③ must keep explainable.
	turnGateLedgerTurns = 4

	// turnGatePasses is the session_end total_iterations — the LOOP's count, a
	// different unit from every number above (one turn routinely takes several
	// passes).
	turnGatePasses = 7

	// turnGateRows is the fixture's JSONL line count, asserted so a truncated
	// write cannot make every arm below agree on a shorter log.
	turnGateRows = 28

	// turnGateStartHour anchors the fixture's timestamps, so a failure log's
	// two runs are distinguishable without reading unix seconds.
	turnGateStartHour = 1

	// turnGateUnusedModelURL is a closed loopback address: no consumer here
	// contacts a model (the replay answers from the recording), and pointing the
	// resolver at a dead port makes a run that TRIED a loud failure instead of a
	// silent reach for the operator's provider.
	turnGateUnusedModelURL = "http://127.0.0.1:1"

	// turnGateModel is the model word the fixture records and every surface
	// echoes; it is a literal because it is the fixture's DATA, not a knob.
	turnGateModel = "deepseek-flash"

	// turnGateBandPromptMinChars is the shortest prefix of a recorded opening the
	// band may show and still count as identifying that turn. Below it the prefix
	// check would be satisfiable by any two turns that share a preamble (every
	// notify carrier begins identically), i.e. by nothing.
	turnGateBandPromptMinChars = 12
)

// turnGateReplayTimeout bounds one scripted replay. The run types six turns of
// its own accord; the bound is a failure diagnostic, never a production timer.
const turnGateReplayTimeout = 120 * time.Second

// turnGateMark is one turn's recorded opening text — the identity this gate
// compares across surfaces. It is compared as a MARKER the rendering cannot
// abbreviate away: the band truncates at the row's width and the page wraps, so
// "the same text" has to mean "the same recorded token", not "the same line
// count" (which is exactly the comparison that was useless).
type turnGateMark struct {
	// Ordinal is the 1-based position on the turn axis — the N of `--turn N`.
	Ordinal int
	// Recorded is the opening text the session JSONL holds for that turn, verbatim.
	Recorded string
	// Marker is the token every surface must show for this turn.
	Marker string
}

const (
	// turnGateNotifyCarrier is the sentence a `gopi notify` delivery writes into
	// a session log — the harness's own words with the caller's text after them.
	// Its restatement is byte-identical to this sentence (which is why the face
	// cannot be read off the text and the row carries `source` instead), so the
	// fixture's row uses exactly this spelling.
	turnGateNotifyCarrier = "[system] An external process sent this session a message: MARKER-TWO delivered by notify"

	// turnGateLongPromptBody makes turn 3's prompt long enough to wrap several
	// times at any width the replay resolves (its fallback canvas is 80x24).
	// The wrap is the point: a comparison by ROW COUNT would fail on this turn
	// alone, which is why the comparison is by marker.
	turnGateLongPromptBody = "a deliberately long prompt whose body keeps going so that every terminal " +
		"width the replay could be given will wrap it at least twice and often four times, " +
		"which is exactly the shape that made a row-count comparison useless"

	// turnGateHarnessTick is a `[system]` machine tick BETWEEN turns: it must
	// open nothing (the carrier/restatement rule). A reader that counted every
	// `user` row would report eight turns.
	turnGateHarnessTick = "[system] [t+~1m] — +1s since your previous request."

	// turnGateStateRow is the OTHER harness-injection shape that must not open a
	// turn: a state row the framework writes about its own work.
	turnGateStateRow = "[system] [WRITE_TASK_AREA] task list updated by the tool"
)

var turnGateMarks = []turnGateMark{
	{Ordinal: 1, Recorded: "MARKER-ONE the first question a person typed", Marker: "MARKER-ONE"},
	{Ordinal: 2, Recorded: turnGateNotifyCarrier, Marker: "MARKER-TWO"},
	{Ordinal: 3, Recorded: "MARKER-THREE " + turnGateLongPromptBody, Marker: "MARKER-THREE"},
	{Ordinal: 4, Recorded: "MARKER-FOUR a question the tools answered", Marker: "MARKER-FOUR"},
	{Ordinal: 5, Recorded: "MARKER-FIVE typed by the surface itself", Marker: "MARKER-FIVE"},
	{Ordinal: 6, Recorded: "MARKER-SIX the last question", Marker: "MARKER-SIX"},
}

// turnGateTimeline is the fixture's session JSONL in the WRITER'S OWN ROW ORDER
// (measured on a real session: the opening row, the assistant's rows, the
// `request` row that closes the exchange, then the tool call and its result). The
// order is not cosmetic — a fixture that put `request` first makes the reader
// attribute every exchange to the NEXT one, and it would move every boundary this
// gate counts.
func turnGateTimeline() string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":%q,"timestamp":"2026-11-04T%02d:00:00Z","model":%q,"sessionId":%q}`+"\n",
		sessionevent.EventTypeSessionStart, turnGateStartHour, turnGateModel, turnGateSessionID)

	// turn 1 — the person, one round, one tool call.
	fmt.Fprintf(&b, `{"type":"user","source":"human","message":{"role":"user","content":%q}}`+"\n", turnGateMarks[0].Recorded)
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[`+
		`{"type":"thinking","thinking":"weighing one option"},`+
		`{"type":"text","text":"answer one"},`+
		`{"type":"tool_use","id":"t1","name":"bash","input":{"command":"echo hi"}}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":0}`+"\n")
	fmt.Fprint(&b, `{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"hi from the tool"}]}}`+"\n")

	// a harness tick BETWEEN turns — opens nothing.
	fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":%q}}`+"\n", turnGateHarnessTick)

	// turn 2 — a notify delivery.
	fmt.Fprintf(&b, `{"type":"user","source":"notify","message":{"role":"user","content":%q}}`+"\n", turnGateMarks[1].Recorded)
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer two"}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":1}`+"\n")

	// turn 3 — the person, a WRAPPING prompt.
	fmt.Fprintf(&b, `{"type":"user","source":"human","message":{"role":"user","content":%q}}`+"\n", turnGateMarks[2].Recorded)
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer three"}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":2}`+"\n")

	// turn 4 — the person, THREE rounds, TWO tool calls.
	fmt.Fprintf(&b, `{"type":"user","source":"human","message":{"role":"user","content":%q}}`+"\n", turnGateMarks[3].Recorded)
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"starting four"},`+
		`{"type":"tool_use","id":"t4a","name":"bash","input":{"command":"echo a"}}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":3}`+"\n")
	fmt.Fprint(&b, `{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4a","content":"a from the tool"}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"continuing four"},`+
		`{"type":"tool_use","id":"t4b","name":"bash","input":{"command":"echo b"}}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":4}`+"\n")
	fmt.Fprint(&b, `{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4b","content":"b from the tool"}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer four"}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":5}`+"\n")

	// turn 5 — the surface typed it for itself, with a state row INSIDE the turn.
	fmt.Fprintf(&b, `{"type":"user","source":"llm","message":{"role":"user","content":%q}}`+"\n", turnGateMarks[4].Recorded)
	fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":%q}}`+"\n", turnGateStateRow)
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer five"}]}}`+"\n")
	fmt.Fprint(&b, `{"type":"request","iteration":6}`+"\n")

	// turn 6 — the person, the last one (and the one the ledger never charged).
	fmt.Fprintf(&b, `{"type":"user","source":"human","message":{"role":"user","content":%q}}`+"\n", turnGateMarks[5].Recorded)
	fmt.Fprint(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer six"}]}}`+"\n")

	fmt.Fprintf(&b, `{"type":%q,"timestamp":"2026-11-04T%02d:30:00Z","total_iterations":%d}`+"\n",
		sessionevent.EventTypeSessionEnd, turnGateStartHour, turnGatePasses)
	return b.String()
}

// ── the fixture on disk ─────────────────────────────────────────────────────

// turnGateProfile writes the fixture session into a fresh profile and seeds the
// chats INDEX row through the PRODUCTION write path, so consumers 3 and 4 read a
// row the LEDGER wrote rather than one a test typed into a table.
//
// The directory shape is composed from the owners' own constants
// (config.ProfilePathIn + config.SessionsDirName): a test that spelled
// ".gopi/sessions" would keep passing after the layout moved, while the verb
// under it started refusing every session.
func turnGateProfile(t *testing.T) (profileRoot, sessionID string) {
	t.Helper()
	profileRoot = t.TempDir()
	sessionID = turnGateSessionID
	dir := filepath.Join(config.ProfilePathIn(profileRoot, config.SessionsDirName), sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the session dir: %v", err)
	}
	body := turnGateTimeline()
	if got := strings.Count(body, "\n"); got != turnGateRows {
		t.Fatalf("the fixture writes %d rows, want %d — the timeline and its bound drifted apart", got, turnGateRows)
	}
	logPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(logPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write the session jsonl: %v", err)
	}

	// THE LEDGER IS PINNED BEFORE THE ROW IS SEEDED, at the path the CHILD
	// resolves for the same profile — GOPI_HOME is the one channel every surface
	// resolves its profile through, and the row's writer and its readers must
	// meet in the same file (config.MemoryDBFileName is that leaf's name).
	t.Setenv(config.GopiDirEnv, profileRoot)
	t.Setenv(config.MemoryDBEnv, config.ProfilePathIn(profileRoot, config.MemoryDBFileName))

	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: sessionID,
		Mode:      tools.ChatModeCLI,
		Model:     turnGateModel,
		LogPath:   logPath,
	}); err != nil {
		t.Fatalf("seed the chats index row: %v", err)
	}
	// THE CHARGES, through the ledger's own writer, exactly turnGateLedgerTurns of
	// them: a constant typed into the table would let this gate pass while the
	// writer charged something else.
	for i := 0; i < turnGateLedgerTurns; i++ {
		if err := tools.ChatRecordTurn(sessionID, 0, 0, 0); err != nil {
			t.Fatalf("charge turn %d through the ledger writer: %v", i+1, err)
		}
	}
	return profileRoot, sessionID
}

// turnGateEnv is the environment every consumer runs with: the fixture's profile
// (MockEnv also strips the ambient GOPI_* half, so nothing leaks in from the host).
func turnGateEnv(t *testing.T, profileRoot string) []string {
	t.Helper()
	return testutil.MockEnv(t, profileRoot, turnGateUnusedModelURL)
}

// runTurnGate runs one consumer for real and fails on a non-zero exit: a consumer
// that REFUSED tells this gate nothing, and a silent zero exit would turn "the
// verb broke" into "the number changed".
func runTurnGate(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, code := testutil.RunGopi(t, args, "", env, dir)
	if code != 0 {
		t.Fatalf("gopi %v exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", args, code, stdout, stderr)
	}
	return stdout
}

// ── ① `turn N` — one axis, two surfaces ─────────────────────────────────────

// TestTurnConsistency is the gate: the four claims the operator's corrected
// criterion states, one subtest each. Every arm fails on the NUMBER it names and
// prints both spellings side by side.
func TestTurnConsistency(t *testing.T) {
	profileRoot, sessionID := turnGateProfile(t)
	env := turnGateEnv(t, profileRoot)
	work := t.TempDir()

	t.Run("turn-N-is-one-axis-on-page-and-band", func(t *testing.T) {
		turnGateAssertOneAxis(t, env, work, sessionID)
	})
	t.Run("turns-is-one-word-on-show-and-list", func(t *testing.T) {
		turnGateAssertTurnsWord(t, env, work, sessionID)
	})
	t.Run("openings-and-ledger-are-separate-named-words", func(t *testing.T) {
		turnGateAssertDecomposition(t, env, work, sessionID)
	})
	t.Run("fraction-denominator-is-the-axis-total", func(t *testing.T) {
		turnGateAssertDenominators(t, env, work, sessionID)
	})
}

// turnGateAssertOneAxis is claim ①. For every N, the page `session replay --turn N`
// prints and the band a real `gopi chat --replay` PAINTS are the same turn, judged
// by the recorded opening text.
//
// The band's NUMBER is compared too, not only its text: a replay that painted
// turn 5 with the number 2 would satisfy a text-only assertion and is precisely
// the measured defect (seven bands numbered 1,2,2,2,2,2,2 on a filmed screen).
func turnGateAssertOneAxis(t *testing.T, env []string, work, sessionID string) {
	t.Helper()
	for _, m := range turnGateMarks {
		out := runTurnGate(t, env, work, "session", "replay", sessionID, "--turn", fmt.Sprint(m.Ordinal))
		head := fmt.Sprintf("turn %d/%d", m.Ordinal, turnGateTurns)
		if !strings.Contains(out, head) {
			t.Fatalf("`session replay %s --turn %d` prints no %q — the page's own fraction disagrees with the axis:\n%s",
				sessionID, m.Ordinal, head, out)
		}
		if !strings.Contains(out, m.Marker) {
			t.Fatalf("`--turn %d` does not print its turn's opening text (marker %q):\n%s", m.Ordinal, m.Marker, out)
		}
	}

	bands := turnGateReplayBands(t, env, work, sessionID)
	if len(bands) != turnGateTurns {
		t.Fatalf("the replay painted %d band(s), want %d — one recording cannot have two lengths:\n%v",
			len(bands), turnGateTurns, bands)
	}
	for i, b := range bands {
		if b.Turn != i+1 {
			t.Fatalf("band #%d (1-based) is numbered turn %d — the bands are not the axis in order:\n%v", i+1, b.Turn, bands)
		}
		want := turnGateMarks[b.Turn-1]
		// THE BAND PAINTS A PREFIX OF THE RECORDED OPENING, never the whole of
		// it: the row is capped at the canvas width and ends with an ellipsis.
		// So the comparison is "is what the band shows the BEGINNING of what the
		// log recorded for this N" — which is exactly the claim, and it is still
		// a discriminating one (band 3 showing turn 4's text is not a prefix of
		// turn 3's). A marker search would be wrong here for a reason worth
		// stating: a notify carrier is 56 characters of harness preamble before
		// the caller's text even begins, so its marker can NEVER fit in the band
		// — the marker is the right instrument for the PAGE and the wrong one
		// for the row.
		shown := strings.TrimSuffix(strings.TrimSpace(b.Prompt), "…")
		if len(shown) < turnGateBandPromptMinChars {
			t.Fatalf("band %d shows only %q (%d chars) — too little of the turn to identify it, so a prefix check "+
				"would pass on nothing:\n%v", b.Turn, shown, len(shown), bands)
		}
		if !strings.HasPrefix(want.Recorded, shown) {
			t.Fatalf("band %d shows %q, which is NOT how turn %d of the recording opens (%q) — the band's NUMBER and "+
				"the turn it paints have drifted apart:\n%v", b.Turn, shown, b.Turn, want.Recorded, bands)
		}
	}
}

// turnGateBand is one band the replay painted: its number and its prompt text.
type turnGateBand struct {
	Turn   int
	Prompt string
}

// turnGateBandRe is the band heading the chat surface paints, matched against the
// pty stream with the escapes REMOVED: the row is drawn cell by cell with SGR runs,
// so the visible text is what a reader sees, and a byte-level match would test the
// colour layer instead of the numbering. The `·` and the `────` rule are the
// surface's own spelling (measured on a live replay, 2026-11-04); the prompt after
// them is truncated at the row's width, which is why every arm searches for a
// MARKER rather than comparing the whole line.
//
// THE BAND'S LABEL IS THREE FIELDS — `turn N · <moment> · <prompt>`
// (lineedit.turnLabel composes it that way, and TurnStampSep is the same `·`
// between all three). The moment is therefore NOT part of the prompt, and a
// pattern that took everything after the first `·` read the timestamp as the
// opening text: on a replay whose recording carries a moment it compared
// `2026-11-04 17:00:00 · MARKER-ONE …` against `MARKER-ONE …`, and on a recording
// with no usable timestamp it compared `--:--:-- · MARKER-ONE …`. Both are the
// gate misreading a correct screen, so the moment group is matched explicitly and
// only the text after the SECOND separator is the prompt.
var turnGateBandRe = regexp.MustCompile(`──── turn (\d+) · (?:[^─\n·]* · )?([^─\n]*)`)

// turnGateReplayBands runs `gopi chat --replay <id>` on a REAL pty — the band is
// the half of this consumer a human looks at, and the chat surface refuses to run
// without a terminal — and returns the bands it painted, in order.
//
// It REUSES the pty harness in chat_pty_test.go (openPty/ptySession/close), never
// a second copy: two pty harnesses would be two behaviours for one product.
func turnGateReplayBands(t *testing.T, env []string, work, sessionID string) []turnGateBand {
	t.Helper()
	p := startPTYChat(t, []string{"chat", "--replay", sessionID, "--no-dump-screen"}, env, work)
	p.waitFor(t, chatPromptMark, ptyPromptTimeout)
	var buf strings.Builder
	deadline := time.NewTimer(turnGateReplayTimeout)
	defer deadline.Stop()
	rx := p.rx
	for rx != nil {
		select {
		case b, ok := <-rx:
			if !ok {
				rx = nil
			} else {
				buf.WriteByte(b)
			}
		case <-deadline.C:
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill() //stylegate:ignore the exit status is no longer meaningful
			}
			_ = p.master.Close() //stylegate:ignore the master is torn down with the child
			t.Fatalf("the replay did not finish within %s; output so far:\n%s", turnGateReplayTimeout, turnGateStripANSI(buf.String()))
		}
	}
	if code := p.close(t); code != 0 {
		t.Fatalf("`chat --replay %s` exited %d, want 0; output:\n%s", sessionID, code, turnGateStripANSI(buf.String()))
	}
	return turnGateParseBands(t, buf.String())
}

// turnGateANSIMatches are the escape sequences a cell renderer emits: CSI (colours,
// cursor moves, erases), OSC (titles, terminated by BEL or ST) and the
// two-character escapes (charset select, cursor save/restore).
var turnGateANSIMatches = []*regexp.Regexp{
	regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]"),
	regexp.MustCompile("\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)"),
	regexp.MustCompile("\x1b[@-Z\\\\-_]"),
}

// turnGateStripANSI reduces the pty stream to the characters a reader saw.
func turnGateStripANSI(stream string) string {
	out := strings.ReplaceAll(stream, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	for _, re := range turnGateANSIMatches {
		out = re.ReplaceAllString(out, "")
	}
	return out
}

// turnGateParseBands reads every band heading out of the cleaned stream. Each turn
// number is taken at its FIRST occurrence: the walk visits the axis once, so a
// repaint (the editor redrawing a row) must not be counted as a second band.
func turnGateParseBands(t *testing.T, stream string) []turnGateBand {
	t.Helper()
	plain := turnGateStripANSI(stream)
	seen := map[int]bool{}
	var bands []turnGateBand
	for _, m := range turnGateBandRe.FindAllStringSubmatch(plain, -1) {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
			t.Fatalf("a band heading carries %q where a turn number belongs: %v", m[1], err)
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		bands = append(bands, turnGateBand{Turn: n, Prompt: strings.TrimSpace(m[2])})
	}
	if len(bands) == 0 {
		t.Fatalf("the replay painted no band at all — the surface this gate measures did not run:\n%s", plain)
	}
	return bands
}

// ── ② `turns` — one word, two JSON faces, and the writer's own number ───────

// turnGateShowJSON is the subset of `session show --json` this file reads. The keys
// are the ones the surface PUBLISHES (cmd/agent/cli_session.go's sessionJSONRow):
// `turns` is the LOG's turn-opening axis — the count `--turn N` and every replay
// band number along (gopilog.SessionTurns, surfaced as
// tools.ChatSessionDetail.TurnsOpened) — and the LEDGER's own `chats.turn_count`
// rides `turns_completed` beside it (549b8cab). The pair is the point: the two
// producers advance on different events (a seam OPENS vs a turn COMPLETES), so a
// reader must be able to tell which half it is reading; `turns_provenance` says
// which, explicitly, instead of leaving the species to be inferred.
//
// `human_turns` is that count's person-only subset (omitted, with
// `human_turns_known=false`, when nobody measured it).
type turnGateShowJSON struct {
	SessionID       string `json:"session_id"`
	Turns           int    `json:"turns"`
	TurnsCompleted  *int   `json:"turns_completed"`
	TurnsProvenance string `json:"turns_provenance"`
	HumanTurns      *int   `json:"human_turns"`
	HumanTurnsKnown bool   `json:"human_turns_known"`
}

// turnGateListJSON is one `session list --json` row: the chat index's row embedded
// under the merged listing's own keys.
type turnGateListJSON struct {
	SessionID       string `json:"session_id"`
	Kind            string `json:"kind"`
	Turns           int    `json:"turns"`
	TurnsCompleted  *int   `json:"turns_completed"`
	TurnsProvenance string `json:"turns_provenance"`
}

// turnGateAssertTurnsWord is claim ②: the two faces that print the LEDGER's `turns`
// agree, and both equal what the ledger's OWN WRITER recorded — not merely each
// other, which three surfaces reading one broken column would also satisfy.
func turnGateAssertTurnsWord(t *testing.T, env []string, work, sessionID string) {
	t.Helper()
	showOut := runTurnGate(t, env, work, "session", "show", sessionID, "--json")
	var show turnGateShowJSON
	if err := json.Unmarshal([]byte(showOut), &show); err != nil {
		t.Fatalf("`session show --json` did not emit one decodable document: %v\n%s", err, showOut)
	}
	if show.SessionID != sessionID {
		t.Fatalf("`session show --json` answered for %q, want %q", show.SessionID, sessionID)
	}

	listOut := runTurnGate(t, env, work, "session", "list", "--json", "--all")
	var rows []turnGateListJSON
	if err := json.Unmarshal([]byte(listOut), &rows); err != nil {
		t.Fatalf("`session list --json` did not emit one decodable array: %v\n%s", err, listOut)
	}
	// THE ROW IS SELECTED BY KIND, NOT BY ID ALONE, and that is a MEASURED
	// requirement rather than a convenience: one session id is listed TWICE —
	// once as the CHAT row the index holds (`kind=chat`, which carries the log's
	// turn count) and once as the RUN row the sessions tree yields (`kind=run`,
	// which carries NO `turns` key at all: no chats column stands behind it).
	// Taking "the row with this id" would therefore compare two different
	// questions, and on a run row the answer is an absent key, which a plain
	// `int` decode reads as 0.
	//
	// THE DUAL LISTING IS DELIBERATE, not a bug to file (PA ruling 2026-11-04,
	// devdoc/session-list-turns-provenance.md): the two rows are two facts — a
	// conversation, and the run that produced it — and each now states which half
	// it is in `turns_provenance`. What the ruling forbids is collapsing them, and
	// what it fixed is the ambiguity this comment used to describe.
	listTurns := -1
	rowsForSession := 0
	for _, r := range rows {
		if r.SessionID != sessionID {
			continue
		}
		rowsForSession++
		if r.Kind == tools.SessionKindChat {
			listTurns = r.Turns
		}
	}
	if listTurns < 0 {
		t.Fatalf("`session list --json` lists no %s row for the fixture session %q (%d row(s) carry that id):\n%s",
			tools.SessionKindChat, sessionID, rowsForSession, listOut)
	}
	if rowsForSession > 1 {
		t.Logf("`session list` lists ONE session (%s) as %d rows — the chats index and the sessions tree each yield "+
			"one; only the %s row carries `turns`, and the run row states %q instead of a zero, so summing this "+
			"column no longer counts a row that has no number",
			sessionID, rowsForSession, tools.SessionKindChat, tools.SessionSourceRunTurnsClause)
	}

	// THE TWO WORDS, CHECKED AS A PAIR (549b8cab, and the criterion this gate
	// asserts): `turns` is the LOG's turn-OPENING axis — the count `--turn N` and
	// every replay band number along — and `turns_completed` is the LEDGER's
	// `chats.turn_count`, which advances only when a turn FINISHES. The fixture
	// forces them apart (6 boundaries, 4 charges), so this arm can tell the two
	// producers from "one number printed twice".
	if show.Turns != turnGateTurns {
		t.Fatalf("`session show` prints turns=%d, want the LOG's %d turn openings (the axis `--turn N` and the "+
			"bands walk — gopilog.SessionTurns):\n%s", show.Turns, turnGateTurns, showOut)
	}
	if show.TurnsCompleted == nil {
		t.Fatalf("`session show --json` publishes no `turns_completed` — the LEDGER's own count must not "+
			"vanish when it disagrees with the axis (Rule 15: an absence is stated, never dropped):\n%s", showOut)
	}
	if *show.TurnsCompleted != turnGateLedgerTurns {
		t.Fatalf("`session show` prints turns_completed=%d, want the ledger's %d (tools.ChatRecordTurn charged "+
			"it %d time(s)):\n%s", *show.TurnsCompleted, turnGateLedgerTurns, turnGateLedgerTurns, showOut)
	}
	if show.Turns == *show.TurnsCompleted {
		t.Fatalf("the fixture no longer separates the two producers (%d == %d) — every comparison here went "+
			"vacuous:\n%s", show.Turns, *show.TurnsCompleted, showOut)
	}
	// THE PROVENANCE KEY NAMES THE HALF, on both wire faces, so a consumer reads
	// one field instead of inferring the species from whether a number is zero.
	if want := sessionevent.TurnsProvenanceChat; show.TurnsProvenance != want {
		t.Fatalf("`session show --json` provenance = %q, want %q — the number under `turns` must say which "+
			"half it is:\n%s", show.TurnsProvenance, want, showOut)
	}
	if listTurns != show.Turns {
		t.Fatalf("`session list` prints turns=%d while `session show` prints %d for ONE row — both read "+
			"tools.ChatSessionDetail.TurnsOpened (the log's axis), so they cannot differ\n--- show --json ---\n%s\n"+
			"--- list --json --all ---\n%s", listTurns, show.Turns, showOut, listOut)
	}
	t.Logf("turns (the log's openings) = %d on BOTH faces; completed (the ledger, chats.turn_count) = %d — "+
		"two words, two producers, each named", show.Turns, *show.TurnsCompleted)
}

// ── ③ the decomposition ────────────────────────────────────────────────────

// turnGateLogTurnsRe is the bare `turns` word on the log's report — 549b8cab's
// spelling: the LOG's turn-OPENING count, the axis `--turn N` numbers along. It is
// anchored on the line's own shape (`  turns = N — `) because the report mentions
// the bare word `turns` in half a dozen sentences, and a substring match would
// read prose as a number.
var turnGateLogTurnsRe = regexp.MustCompile(`(?m)^\s*turns = (\d+)`)

// turnGateLogCompletedRe is the LEDGER's count — `chats.turn_count` — under its OWN
// word (`  completed = N`). It prints only when the ledger was read AND disagrees
// with the axis, which is exactly why the fixture forces that disagreement.
//
// THIS IS THE ARM THAT WAS ASSERTING A WORD NOBODY PRINTS. Until this change the
// regex here was `turn boundaries = (\d+)` — a spelling
// sessionevent.CounterUnitTurnBoundary still owns, but one NO production renderer
// emits (`git grep -c CounterUnitTurnBoundary` finds only its declaration, its own
// test and a devdoc). The word the surfaces actually print for the ledger's number
// is `completed`, set by 549b8cab; the boundary word had been retired from the
// report before this gate was written. So the arm could never pass: it asserted a
// line the product has not printed since 549b8cab, and the gate has been red on
// master since it landed (measured: `go test ./testsuite/ -run
// TestTurnConsistency -count=1` fails on this arm and on claim ②).
var turnGateLogCompletedRe = regexp.MustCompile(`(?m)^\s*completed = (\d+)`)

// turnGateLogHumanRe is the SUBSET word (`HUMAN TURNS = human turns — …`), whose
// number rides the report's headline for the same session.
var turnGateLogHumanRe = regexp.MustCompile(`(?m)SESSION HUMAN TURNS — \S+ \((\d+)\)`)

// turnGateAssertDecomposition is claim ③: the log prints BOTH words under their own
// names, they are not the same number (the fixture guarantees it), and the reader can
// say which producer each came from. If it cannot, the log describes one axis with
// two rules and nobody can reconcile them.
func turnGateAssertDecomposition(t *testing.T, env []string, work, sessionID string) {
	t.Helper()
	out := runTurnGate(t, env, work, "session", "log", sessionID, "--turns")

	openings := turnGateMatchInt(t, out, turnGateLogTurnsRe, "turns (the log's turn OPENINGS)")
	ledger := turnGateMatchInt(t, out, turnGateLogCompletedRe, "completed (the ledger's chats.turn_count)")
	human := turnGateMatchInt(t, out, turnGateLogHumanRe, "SESSION HUMAN TURNS (the person-only subset)")

	if openings != turnGateTurns {
		t.Fatalf("the log reports %d turn openings, want %d (the axis `--turn N` and the band walk):\n%s",
			openings, turnGateTurns, out)
	}
	if human != turnGateHumanTurns {
		t.Fatalf("the log reports %d human turns, want %d — the person opened turns 1, 3, 4 and 6 only (a notify "+
			"delivery and a self-typed line are inputs, but they are not the PERSON's):\n%s", human, turnGateHumanTurns, out)
	}
	if human > openings {
		t.Fatalf("the subset (%d human) exceeds its superset (%d openings) — a subset larger than its whole is not a "+
			"rounding, it is two different counters:\n%s", human, openings, out)
	}
	// THE LEDGER WORD, on the surface that explains it: it must be the number the
	// writer charged, and it must NOT silently equal the openings (which would mean
	// the fixture — and the explanation the log prints beside both — went vacuous).
	if ledger != turnGateLedgerTurns {
		t.Fatalf("the log reports completed=%d, want the ledger's %d (tools.ChatRecordTurn charged it %d time(s)):\n%s",
			ledger, turnGateLedgerTurns, turnGateLedgerTurns, out)
	}
	if ledger == openings {
		t.Fatalf("completed (%d) equals turns (%d) in this fixture: the two words can no longer be told apart, so "+
			"every comparison above became vacuous:\n%s", ledger, openings, out)
	}
	t.Logf("turn openings %d, of which human %d; the ledger's completed = %d (a seam is recorded when a turn OPENS, "+
		"the ledger advances when one COMPLETES — turn 2 arrived mid-run and turn 6 was interrupted)",
		openings, human, ledger)
}

// turnGateMatchInt returns the first capture of re in out, failing when the surface
// printed no such number: an absent number is never a passed assertion (Rule 15).
func turnGateMatchInt(t *testing.T, out string, re *regexp.Regexp, what string) int {
	t.Helper()
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no %s in the output — an absent number is not a passed assertion:\n%s", what, out)
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
		t.Fatalf("%s carries %q where a number belongs: %v", what, m[1], err)
	}
	return n
}

// ── ④ denominators ─────────────────────────────────────────────────────────

// turnGateAssertDenominators is claim ④: every fraction and every total a surface
// prints uses the SAME axis length. A denominator that were the ledger's count, or
// the round count, would make `turn 3/6` on one surface and `turn 3/4` on another
// for the same recording — the confusion this gate exists to end.
func turnGateAssertDenominators(t *testing.T, env []string, work, sessionID string) {
	t.Helper()
	// Consumer 1's own headline.
	out := runTurnGate(t, env, work, "session", "replay", sessionID, "--turns")
	if want := fmt.Sprintf("turns=%d", turnGateTurns); !strings.Contains(out, want) {
		t.Fatalf("`session replay --turns` prints no %q:\n%s", want, out)
	}
	// Consumer 1's per-turn page, at both ends of the axis.
	for _, n := range []int{1, turnGateTurns} {
		page := runTurnGate(t, env, work, "session", "replay", sessionID, "--turn", fmt.Sprint(n))
		if want := fmt.Sprintf("turn %d/%d", n, turnGateTurns); !strings.Contains(page, want) {
			t.Fatalf("`--turn %d` prints no %q — the page's denominator is not the axis total:\n%s", n, want, page)
		}
	}
	// Consumer 2 walked the same axis, so its length is the same length.
	bands := turnGateReplayBands(t, env, work, sessionID)
	if len(bands) != turnGateTurns {
		t.Fatalf("the replay walked %d turn(s) while `session replay --turns` reports %d — one recording, two lengths:\n%v",
			len(bands), turnGateTurns, bands)
	}
}
