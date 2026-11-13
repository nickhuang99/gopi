package main

// session_show_turns_test.go — THE DETAIL PAGE REPORTS THE LEDGER'S TURN COUNT,
// AND THE PERSON-ONLY SUBSET UNDER ITS OWN WORD.
//
// THE SHAPE THIS PINS (the operator's ruling, 2026-10-27). There is ONE turn
// count — `chats.turn_count`, the turns the session ran, every input face equal
// — and it is what the bare word `turns` names on every surface. The narrower
// question "how many of those did the PERSON open?" is a DETAIL: it prints UNDER
// the count, under `human turns`, and it can be unmeasured without affecting the
// count above it (which needs no log at all).
//
// THE DEFECTS THIS FILE REMEMBERS, because each one is a shape that can come
// back. (a) `session show <id>` never called the injected ChatSessionTurnsReader
// at all, so the page printed `turns —` and `--json` shipped
// `"turns_known": false` for a session whose log held 11 operator rows
// (measured 2026-10-26). (b) The repair for that qualified BOTH numbers
// (`turns 66` beside `repl turns 200`) instead of asking which one the reader
// wanted — the operator's verdict is that the pair has no subject.
//
// The fixture is not built here: session_list_turns_test.go owns it (a chats row
// recording 200 turns over a log of 66 operator rows). A second fixture would be
// the mirror Rule 14 forbids, and — worse — two fixtures could drift while both
// tests stayed green.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// showTurnsJSON is the subset of the `session show --json` / `session list
// --json` document this test reads. The keys come from internal/sessionevent's
// constants — a test that typed "turns" here would keep passing after the wire
// name changed, which is the opposite of a guard (Rule 13).
type showTurnsJSON struct {
	SessionID string `json:"session_id"`
	// Turns is the LOG's opened count: always present, never optional.
	Turns int `json:"turns"`
	// TurnsCompleted is the LEDGER's count (`chats.turn_count`), its own key
	// since epic 2026-11-04 so a script that needed the old meaning of `turns`
	// moves one key to the right instead of losing the quantity.
	TurnsCompleted int `json:"turns_completed"`
	// HumanTurns is `omitempty`, so an unmeasured row OMITS the key: the absent
	// case must be distinguishable from a measured zero, and pointing a missing
	// key at an int would silently produce 0 if it were not checked.
	HumanTurns      *int   `json:"human_turns"`
	HumanTurnsKnown bool   `json:"human_turns_known"`
	TurnsNote       string `json:"human_turns_note"`
}

// parseShowTurnsJSON decodes the CLI's own output — not a struct the test
// marshalled itself — so a renamed or dropped key fails here.
func parseShowTurnsJSON(t *testing.T, out string) showTurnsJSON {
	t.Helper()
	var v showTurnsJSON
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("the JSON face did not emit one decodable document: %v\n%s", err, out)
	}
	return v
}

// TestSessionShowAndListAgreeOnTurns drives THREE faces over ONE fixture:
//
//	(1) `session show <id>`             — the rendered detail page
//	(2) `session show <id> --json`      — the machine-readable detail
//	(3) `session list --mode cli --json` — the index row for the same session
//
// and requires all three to report the SAME ONE turn count — the ledger's
// recorded number — plus the same named subset, which it reads from the ORACLE
// the `session log --turns` table uses (gopilog.SessionTurns over
// gopilog.ParseFile), because that table's own count is that call and nothing
// else (cli_log_round.go: FormatSessionTurnTable).

// TestSessionShowAndListAgreeOnTurns drives THREE faces over ONE fixture:
//
//	(1) `session show <id>`          — the rendered detail page
//	(2) `session show <id> --json`   — the machine-readable detail
//	(3) `session list --mode cli --json` — the index row for the same session
//
// and requires all three to report the log's operator-row count, which it reads
// from the ORACLE the `session log --turns` table uses (gopilog.SessionTurns over
// gopilog.ParseFile) — because that table's own `(N turn(s))` header is that call
// and nothing else (cli_log_round.go: FormatSessionTurnTable).
//
// Red before the fix: (1) printed `turns       —`, (2) carried
// `operator_turns_known: false` with no operator_turns key, and (3) reported 11 —
// three surfaces, two answers.
func TestSessionShowAndListAgreeOnTurns(t *testing.T) {
	if sessionListTurnsFixtureRecorded == sessionListTurnsFixtureOperator {
		t.Fatalf("the fixture's two counts are equal (%d) — it cannot see the defect this test exists for",
			sessionListTurnsFixtureRecorded)
	}
	if !tools.ChatSessionTurnsInstalled() {
		t.Fatal("no turn counter is installed in this process — the suite's TestMain must call installSessionTurnsReader(), otherwise every face below renders its unmeasured branch and this test proves nothing")
	}
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, sessionListTurnsFixtureOperator)
	seedTurnsFixtureRow(t, sessionListTurnsFixtureID, path)

	// THE ORACLE for the SUBSET: the number `session log --turns` tabulates. Read
	// through the same two calls that table makes, so the expected value is not a
	// constant this test invented — if the counter changes its mind, this moves
	// with it and the disagreement (if any) shows up as a real failure.
	s, perr := gopilog.ParseFile(path)
	if perr != nil {
		t.Fatalf("parse the fixture log: %v", perr)
	}
	want := gopilog.SessionTurns(s)
	if want != sessionListTurnsFixtureOperator {
		t.Fatalf("the fixture log holds %d operator rows, the fixture claims %d — the fixture is wrong, not the code",
			want, sessionListTurnsFixtureOperator)
	}
	// A fixture whose numbers coincide cannot distinguish a measured count from
	// a recorded one (the defect's whole shape).
	if want == sessionListTurnsFixtureRecorded {
		t.Fatalf("the oracle and the recorded count agree (%d) — this fixture cannot see the collision", want)
	}
	// The table `session log --turns` prints, rendered by the SAME function the
	// verb calls (gopilog.FormatSessionTurnTable, via printSessionTurns) — so
	// the expected value is that surface's own output, not a re-derivation.
	// Rendered rather than run: the verb os.Exit()s on a missing session, and a
	// test that takes the process down reports nothing.
	table := gopilog.FormatSessionTurnTable(s)
	if wantTable := fmt.Sprintf("%s — %s (%d)", gopilog.SessionTurnTableWord(), sessionListTurnsFixtureID, want); !strings.Contains(table, wantTable) {
		t.Fatalf("the `--turns` table does not report %q — the oracle this test compares against is not the operator's own surface:\n%s",
			wantTable, table)
	}

	// (1) THE DETAIL PAGE, RENDERED.
	page := captureStdout(t, func() {
		if code := runSessionShow([]string{sessionListTurnsFixtureID}); code != 0 {
			t.Fatalf("runSessionShow = %d, want 0", code)
		}
	})
	// THE BARE TURN WORD: the LOG'S OPENED COUNT (epic 2026-11-04 — it is the axis
	// `--turn N` and every replay band number along, so `session list`, `session
	// show`, `session stats` and `session replay` cannot disagree about "turn N").
	wantTurnLine := fmt.Sprintf("  %-*s%d", sessionShowKeyWidth, sessionevent.CounterUnitTurn, want)
	if !strings.Contains(page, wantTurnLine) {
		t.Errorf("the show page's %q line does not report the log's opened count %d (want %q):\n%s",
			sessionevent.CounterUnitTurn, want, wantTurnLine, page)
	}
	// THE LEDGER'S COMPLETED COUNT keeps its own word, so nothing was deleted —
	// only named. It prints when it differs from the opened count, which this
	// fixture guarantees.
	wantCompletedLine := fmt.Sprintf("  %-*s%d", sessionShowKeyWidth, sessionevent.CounterUnitTurnCompleted,
		sessionListTurnsFixtureRecorded)
	if !strings.Contains(page, wantCompletedLine) {
		t.Errorf("the show page's %q line does not report the ledger's completed count %d (want %q):\n%s",
			sessionevent.CounterUnitTurnCompleted, sessionListTurnsFixtureRecorded, wantCompletedLine, page)
	}
	// THE SUBSET: its own word, UNDER the count, and never under the bare one.
	wantHumanLine := fmt.Sprintf("  %-*s%d", sessionShowKeyWidth, sessionevent.CounterUnitHumanTurn, want)
	if !strings.Contains(page, wantHumanLine) {
		t.Errorf("the show page's %q line does not report the person-only subset %d (want %q):\n%s",
			sessionevent.CounterUnitHumanTurn, want, wantHumanLine, page)
	}
	// The page must not state a REASON for a count it measured.
	for _, note := range []string{tools.ChatTurnsLogAbsentNote, tools.ChatTurnsNoCounterNote, tools.ChatTurnsDirUnresolvedNote} {
		if strings.Contains(page, note) {
			t.Errorf("the show page printed the unmeasured note %q for a session whose log it just read:\n%s", note, page)
		}
	}
	// The subset must appear exactly ONCE: a second copy is how the pair grew
	// back the first time.
	if n := strings.Count(page, sessionevent.CounterUnitHumanTurn+" ") + strings.Count(page, strings.TrimSpace(sessionevent.CounterUnitHumanTurn)+" "); n > 2 {
		t.Errorf("the show page prints %q %d times — a subset is a detail, not a column:\n%s",
			sessionevent.CounterUnitHumanTurn, n, page)
	}

	// (2) THE DETAIL PAGE, AS JSON.
	showJSON := parseShowTurnsJSON(t, captureStdout(t, func() {
		if code := runSessionShow([]string{sessionListTurnsFixtureID, sessionJSONFlag}); code != 0 {
			t.Fatalf("runSessionShow --json = %d, want 0", code)
		}
	}))
	if !showJSON.HumanTurnsKnown {
		t.Errorf("%s = false on the show face for a readable log — the unmeasured claim is the defect:\n%+v",
			sessionevent.JSONKeyHumanTurnsKnown, showJSON)
	}
	if showJSON.HumanTurns == nil || *showJSON.HumanTurns != want {
		t.Errorf("show --json %s = %v, want %d", sessionevent.JSONKeyHumanTurns, showJSON.HumanTurns, want)
	}
	if showJSON.TurnsNote != "" {
		t.Errorf("show --json carries a %s (%q) for a count it measured — a note is the reason ABSENCE is stated",
			sessionevent.JSONKeyTurnsNote, showJSON.TurnsNote)
	}
	// The bare wire key is the LOG's opened count (epic 2026-11-04); the ledger's
	// number rides its own key, so nothing was deleted when the meaning moved.
	if showJSON.Turns != sessionListTurnsFixtureOpened {
		t.Errorf("show --json %s = %d, want the log's opened count %d",
			sessionevent.JSONKeyTurns, showJSON.Turns, sessionListTurnsFixtureOpened)
	}
	if showJSON.TurnsCompleted != sessionListTurnsFixtureRecorded {
		t.Errorf("show --json %s = %d, want the ledger's completed count %d",
			sessionevent.JSONKeyTurnsCompleted, showJSON.TurnsCompleted, sessionListTurnsFixtureRecorded)
	}

	// (3) THE INDEX ROW, AS JSON — the face that was already fixed.
	var rows []showTurnsJSON
	if err := json.Unmarshal([]byte(captureStdout(t, func() {
		if code := runSessionList([]string{sessionModeFlag, tools.ChatModeCLI, sessionJSONFlag}); code != 0 {
			t.Fatalf("runSessionList --json = %d, want 0", code)
		}
	})), &rows); err != nil {
		t.Fatalf("the list JSON face did not emit one decodable document: %v", err)
	}
	var listRow *showTurnsJSON
	for i := range rows {
		if rows[i].SessionID == sessionListTurnsFixtureID {
			listRow = &rows[i]
			break
		}
	}
	if listRow == nil {
		t.Fatalf("the seeded session is missing from the index: %+v", rows)
	}
	if !listRow.HumanTurnsKnown || listRow.HumanTurns == nil {
		t.Fatalf("the index row reports no measured operator count — this face was the fixed one:\n%+v", listRow)
	}

	// THE CLAIM OF THE EPIC: one session, one answer — the two faces must agree
	// about BOTH numbers they now both carry. Guarded rather than
	// dereferenced straight through: in the RED state (before the fix) the show
	// face measures nothing, and a nil dereference would replace the diagnosis
	// with a panic — the failure above is the evidence, this block must not
	// obscure it.
	if listRow.Turns != sessionListTurnsFixtureOpened {
		t.Errorf("the index row reports %s %d, want the log's opened count %d",
			sessionevent.JSONKeyTurns, listRow.Turns, sessionListTurnsFixtureOpened)
	}
	if listRow.TurnsCompleted != sessionListTurnsFixtureRecorded {
		t.Errorf("the index row reports %s %d, want the ledger's completed count %d",
			sessionevent.JSONKeyTurnsCompleted, listRow.TurnsCompleted, sessionListTurnsFixtureRecorded)
	}
	if listRow.HumanTurns == nil || showJSON.HumanTurns == nil || *showJSON.HumanTurns != *listRow.HumanTurns {
		t.Errorf("THE TWO FACES DISAGREE: list says %s %d, show says %d",
			sessionevent.JSONKeyHumanTurns, *listRow.HumanTurns, *showJSON.HumanTurns)
	}
	if showJSON.HumanTurns != nil && *showJSON.HumanTurns != want {
		t.Errorf("show reports %d operator turns, the log's own table counts %d", *showJSON.HumanTurns, want)
	}
}

// TestSessionShowStatesAnUnmeasuredTurnCount is the other arm (Rule 15): with no
// counter published, the page must print a REASON — never 0, and never a bare
// dash with nothing behind it.
//
// This is not a formality. The defect this epic fixes was invisible precisely
// because an unmeasured value rendered as a dash that looked like "this session
// has no turns"; the note is what makes the next reader able to tell "nobody
// looked" from "the person typed nothing".
func TestSessionShowStatesAnUnmeasuredTurnCount(t *testing.T) {
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, sessionListTurnsFixtureOperator)
	seedTurnsFixtureRow(t, sessionListTurnsFixtureID, path)

	// The suite installed the real counter (TestMain); this case needs the
	// UNINSTALLED branch and restores through the same entry point afterwards.
	tools.InstallChatSessionTurnsReader(nil)
	t.Cleanup(installSessionTurnsReader)

	page := captureStdout(t, func() {
		if code := runSessionShow([]string{sessionListTurnsFixtureID}); code != 0 {
			t.Fatalf("runSessionShow = %d, want 0", code)
		}
	})
	if !strings.Contains(page, tools.ChatTurnsNoCounterNote) {
		t.Errorf("with no counter installed the show page must state %q, not a number:\n%s",
			tools.ChatTurnsNoCounterNote, page)
	}
	// The precise form of the ban: a MEASURED-looking turn line.
	if strings.Contains(page, fmt.Sprintf("  %-11s 0", sessionevent.CounterUnitTurn)) {
		t.Errorf("with no counter installed the show page printed a fabricated 0 under %q:\n%s",
			sessionevent.CounterUnitTurn, page)
	}

	showJSON := parseShowTurnsJSON(t, captureStdout(t, func() {
		if code := runSessionShow([]string{sessionListTurnsFixtureID, sessionJSONFlag}); code != 0 {
			t.Fatalf("runSessionShow --json = %d, want 0", code)
		}
	}))
	if showJSON.HumanTurnsKnown {
		t.Errorf("%s = true with no counter installed — nobody measured anything:\n%+v",
			sessionevent.JSONKeyHumanTurnsKnown, showJSON)
	}
	if showJSON.HumanTurns != nil {
		t.Errorf("%s must be OMITTED when unmeasured (it is an optional outgoing field, the presence of the key IS the claim), got %d",
			sessionevent.JSONKeyHumanTurns, *showJSON.HumanTurns)
	}
	if showJSON.TurnsNote != tools.ChatTurnsNoCounterNote {
		t.Errorf("%s = %q, want the sentence naming the mechanism: %q",
			sessionevent.JSONKeyTurnsNote, showJSON.TurnsNote, tools.ChatTurnsNoCounterNote)
	}
}
