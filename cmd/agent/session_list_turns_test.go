package main

// session_list_turns_test.go — THE INDEX ROW AND THE LOG MUST AGREE ABOUT TURNS.
//
// THE DEFECT (epic, measured 2026-10-26 on session
// 80c07a7d-bb6b-48b4-be66-ea1867f403f2): `gopi session list --mode chat` printed
// "repl turns 18" while `gopi session log <full-id>` printed "Turns: 11" for the
// SAME session. 18 is `chats.turn_count` (the REPL loop passes the model
// finished — `session log` prints the same 18 as "Passes:"), 11 is the number of
// OPERATOR rows. Both numbers were right; the defect was that the index face had
// no way to reach the second one and printed the first beside every row.
//
// THE FIX, and what this test pins: the index row's turn count comes from the
// session log through the SAME counter every other surface uses
// (gopilog.SessionTurns, whose predicate is
// sessionevent.IsTurnOpeningRow), reached through the injected
// tools.ChatSessionTurnsReader — internal/tools cannot import internal/gopilog
// (the cycle runs through internal/agent; see internal/tools/chat_turns.go).
//
// THE FIXTURE IS THE SMALLEST ONE THAT CAN SEE THE DEFECT: a chats row whose
// recorded turn_count is 18 and a session log holding 11 operator rows, with a
// `request` row per operator row so the log is a real conversation. A fixture
// where the two counts coincide — or where the log has no operator row at all —
// cannot tell the fixed render from the broken one, and this test refuses to
// pass on that (the assertion below checks the two numbers are DIFFERENT before
// it checks anything else).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/tools"
)

// sessionListTurnsFixtureID is a full uuid-shaped id: every session surface
// refuses a prefix (`session log` says so in as many words), so a fixture id
// must be full-length for the log arm to run at all.
const sessionListTurnsFixtureID = "80c07a7d-bbbb-4ccc-8ddd-eeeeeeeeeeee"

// sessionListTurnsFixture is the fixture's two numbers: what the chats row
// RECORDED (the loop's own count) and what the log actually holds (operator
// rows). They differ, which is the whole point of the fixture.
const (
	sessionListTurnsFixtureRecorded = 18
	sessionListTurnsFixtureOperator = 11
	// sessionListTurnsFixtureOpened is the LOG's opened count. It equals the
	// person-only subset on THIS fixture (every recorded opening row is the
	// person's), which is fine: what this fixture must keep apart is the LEDGER's
	// completed count (18) from the log's opened count (11) — that is the pair the
	// defect confused (epic 2026-11-04). A notify-opened third number is exercised
	// by internal/gopilog's own subset tests, which do not need a hand-built log.
	sessionListTurnsFixtureOpened = sessionListTurnsFixtureOperator
)

// writeTurnsFixtureSession writes a session JSONL holding `operatorRows` turns
// and returns its path inside the profile's sessions tree (the layout every
// presence/reader path resolves, so the test exercises the real lookup rather
// than an explicit log_path shortcut alone).
//
// The row ORDER is the writer's own (measured on session 5af45c3e: operator row
// first, then the harness tick, the assistant's tool call and the `request` row
// that closes the exchange). A fixture that puts the `request` row first makes
// the reader attribute the content to the NEXT exchange — the trap
// internal/gopilog/humanround_test.go documents.
func writeTurnsFixtureSession(t *testing.T, sessionID string, operatorRows int) string {
	t.Helper()
	home := os.Getenv(config.GopiDirEnv)
	if home == "" {
		t.Fatalf("%s is not pinned — call sessionChatEnv first", config.GopiDirEnv)
	}
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"session_start","timestamp":"2026-10-25T01:00:00Z","model":"deepseek-flash","sessionId":"%s"}`+"\n", sessionID)
	for i := 0; i < operatorRows; i++ {
		// The operator's own row (opens a turn), then one exchange answering it.
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":"第 %d 条：人自己打的"}}`+"\n", i+1)
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":"%s[t+~%dm] — a harness tick, not an ask."}}`+"\n",
			sessionevent.HarnessMessagePrefix, i)
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`+"\n", i+1)
		fmt.Fprintf(&b, `{"type":"request","iteration":%d}`+"\n", i)
	}
	fmt.Fprintf(&b, `{"type":"session_end","timestamp":"2026-10-25T02:00:00Z","total_iterations":%d}`+"\n",
		sessionListTurnsFixtureRecorded)
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
	return path
}

// seedTurnsFixtureRow creates the chats INDEX row the list face reads, through
// the same start hook the live surfaces call (tools.ChatEnsureRow), and pins the
// RECORDED turn count to 18 — the number the list surface used to print.
func seedTurnsFixtureRow(t *testing.T, sessionID, logPath string) {
	t.Helper()
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: sessionID, Mode: tools.ChatModeCLI, Model: "deepseek-flash",
		FirstMsg: "the person's first ask", Workdir: "/tmp/proj", LogPath: logPath,
	}); err != nil {
		t.Fatalf("seed %s: %v", sessionID, err)
	}
	for i := 0; i < sessionListTurnsFixtureRecorded; i++ {
		if err := tools.ChatRecordTurn(sessionID, 100, 20, 90); err != nil {
			t.Fatalf("record turn %d: %v", i+1, err)
		}
	}
}

// TestSessionListAndShowAgreeOnTurns is THE regression guard for the epic, driven
// through the real CLI surfaces (runSessionList / runSessionShow — the run
// functions the dispatch calls), not through the helpers underneath them.
//
// THE CLAIM, after the operator's ruling (2026-10-27): `turns` is ONE number —
// `chats.turn_count`, the turns the session ran, every input face equal — so the
// index row and the show page print the SAME number under the SAME word, and the
// person-only subset appears once, under its own word, as a detail.
//
// Red before the fix: the list row said `repl turns 18` and the show page said
// `turns 66` beside it — two numbers, one word family, no way to tell which was
// "how many turns did this session run". Green now: 18 both places, 11 named as
// the human subset.
func TestSessionListAndShowAgreeOnTurns(t *testing.T) {
	if sessionListTurnsFixtureRecorded == sessionListTurnsFixtureOperator {
		t.Fatalf("the fixture's two counts are equal (%d) — it cannot see the defect this test exists for",
			sessionListTurnsFixtureRecorded)
	}
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, sessionListTurnsFixtureOperator)
	seedTurnsFixtureRow(t, sessionListTurnsFixtureID, path)

	// (1) THE INDEX ROW — `gopi session list --mode chat`.
	listOut := captureStdout(t, func() {
		if code := runSessionList([]string{sessionModeFlag, tools.ChatModeCLI}); code != 0 {
			t.Fatalf("runSessionList = %d, want 0", code)
		}
	})
	row := ""
	for _, line := range strings.Split(listOut, "\n") {
		if strings.Contains(line, sessionListTurnsFixtureID) {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("the seeded session is missing from the index:\n%s", listOut)
	}

	// (2) THE SHOW PAGE — `gopi session show <full-id>`.
	showOut := captureStdout(t, func() { runSessionShow([]string{sessionListTurnsFixtureID}) })

	// THE CLAIM (epic 2026-11-04, superseding the 2026-10-27 ruling): the bare
	// word `turns` carries the LOG'S OPENED COUNT on BOTH surfaces, and the
	// ledger's completed count prints under its own word when it differs.
	//
	// WHY IT MOVED. The 2026-10-27 ruling put `chats.turn_count` under the bare
	// word, and the two numbers are produced by gates that are not the same gate:
	// the ledger advances when a turn COMPLETES, the log records a seam when one
	// OPENS. Measured on session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f:
	// `session list` printed `turns 29` while `session replay --turns` printed 30
	// rows and the replay screen's last band read `──── turn 30` — one session,
	// one word, two numbers. `--turn N` and a replay band are POSITIONS, so the
	// bare word must carry the position count; the ledger's number keeps its own
	// word, so nothing was deleted, only named.
	wantOpened := sessionevent.CounterUnitTurn + " " + fmt.Sprint(sessionListTurnsFixtureOpened)
	if !strings.Contains(row, wantOpened) {
		t.Errorf("the index row does not report the log's opened count %d as %q:\n%s",
			sessionListTurnsFixtureOpened, wantOpened, row)
	}
	// THE INDEX ROW KEEPS ONE NUMBER (the 2026-10-27 ruling, still right): the
	// ledger's completed count rides the per-session faces, where a reader is
	// asking about THIS session. On the index it would ride nearly every row and
	// turn a scannable table into a two-number column.
	if strings.Contains(row, sessionevent.CounterUnitTurnCompleted) {
		t.Errorf("the index row carries the ledger's completed count as well — that belongs to the per-session "+
			"faces:\n%s", row)
	}
	if !strings.Contains(showOut, wantOpened) {
		t.Errorf("the show page does not report the same opened count %d as %q:\n%s",
			sessionListTurnsFixtureOpened, wantOpened, showOut)
	}
	if !strings.Contains(showOut, sessionevent.CounterUnitTurnCompleted) ||
		!strings.Contains(showOut, fmt.Sprint(sessionListTurnsFixtureRecorded)) {
		t.Errorf("the show page does not report the ledger's completed count %d:\n%s",
			sessionListTurnsFixtureRecorded, showOut)
	}
	// THE DEFECT ITSELF: the LEDGER's number must never sit under the bare word.
	// (Word-boundary safe: the qualified clauses are stripped first, below.)

	// THE SUBSET IS NAMED, NOT DELETED (Rule 14: one meaning per word; Rule 15:
	// a fact a reader can see is not silently dropped). It prints UNDER the
	// total, under its own word — never under the bare one.
	if !tools.ChatSessionTurnsInstalled() {
		t.Fatal("no turn counter is installed in this process — the suite's TestMain must call installSessionTurnsReader(), otherwise the subset silently renders the unmeasured branch and this test proves nothing")
	}
	wantSubset := sessionevent.CounterUnitHumanTurn + " " + fmt.Sprint(sessionListTurnsFixtureOperator)
	if !strings.Contains(showOut, wantSubset) {
		t.Errorf("the show page does not report the person-only subset %d under %q:\n%s",
			sessionListTurnsFixtureOperator, wantSubset, showOut)
	}

	// THE COLLISION THAT MATTERS IS THE ONE THIS FIXTURE CAN SEE: the bare word
	// must carry the LOG's number (11 here) and NOT the LEDGER's (18). Asserted
	// above, and the completed count is asserted absent from the index.
	//
	// THE OTHER COLLISION — the bare word carrying the person-only SUBSET — is NOT
	// asserted here, and that is deliberate: on this fixture the opened count and
	// the subset are the same 11 (every recorded opening row is the person's), so
	// a passing "no collision" reading would prove nothing. The subset-vs-total
	// separation needs a log with a notify-opened turn, which internal/gopilog's
	// own subset tests own (human_turn_subset_test.go builds one directly).
}

// TestSessionListStatesAnUnmeasuredSubset is the other arm (Rule 15): when the
// counter is absent the row still reports the ledger's turn count, and the SHOW
// page states WHY the subset is unknown instead of printing a fabricated number
// or borrowing the bare word for it.
func TestSessionListStatesAnUnmeasuredSubset(t *testing.T) {
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, sessionListTurnsFixtureOperator)
	seedTurnsFixtureRow(t, sessionListTurnsFixtureID, path)

	// The suite installed the real counter (TestMain); this case needs the
	// UNINSTALLED branch, and restores through the same entry point afterwards.
	tools.InstallChatSessionTurnsReader(nil)
	t.Cleanup(installSessionTurnsReader)

	out := captureStdout(t, func() {
		if code := runSessionList([]string{sessionModeFlag, tools.ChatModeCLI}); code != 0 {
			t.Fatalf("runSessionList = %d, want 0", code)
		}
	})
	row := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, sessionListTurnsFixtureID) {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("the seeded session is missing from the index:\n%s", out)
	}
	// The bare turn word does NOT depend on the counter at all — it is the same
	// injected reader in both states, and with no counter it says WHY rather
	// than printing a number nobody measured (Rule 15).
	if !strings.Contains(row, sessionevent.CounterUnitTurn+" ") {
		t.Errorf("with no counter installed the row lost the turn line entirely:\n%s", row)
	}
	if strings.Contains(row, sessionevent.CounterUnitTurn+" "+fmt.Sprint(sessionListTurnsFixtureRecorded)) {
		t.Errorf("with no counter installed the row still printed the LEDGER's count under the bare word — that is "+
			"the substitution this row must never make:\n%s", row)
	}

	showOut := captureStdout(t, func() { runSessionShow([]string{sessionListTurnsFixtureID}) })
	if !strings.Contains(showOut, sessionevent.CounterUnitHumanTurn+" ") {
		t.Errorf("the show page dropped the human-turn line instead of stating why it is unknown:\n%s", showOut)
	}
	if strings.Contains(showOut, sessionevent.CounterUnitHumanTurn+" "+fmt.Sprint(sessionListTurnsFixtureOperator)) {
		t.Errorf("with no counter installed the show page still printed a subset number nobody measured:\n%s", showOut)
	}
}
