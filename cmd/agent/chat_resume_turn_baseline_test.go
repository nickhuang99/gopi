package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// chat_resume_turn_baseline_test.go — the regression guard for task 01a16fb3:
// a resumed chat numbered its turns from 1 while the session's own log already
// held N, so the screen and `gopi session log <id> --turns` disagreed for the
// rest of the run.
//
// TWO FACTS ARE PINNED, and they are deliberately separate assertions because
// they are two different quantities that used to be ONE variable (the REPL's
// iterCount):
//
//	(a) the DISPLAY number of the first new turn is N+1 — read off the rendered
//	    grid at the same seam the REPL uses (chatBeginTurn + the status row);
//	(b) the close-out's pass count is NOT inflated by the baseline — read off
//	    the `session_end` a REAL resumed run writes.
//
// A fix that only satisfied (a) by seeding iterCount would pass the first and
// fail the second, which is exactly the mistake the briefing warns about.

// chatResumeTurnFixture writes a session record holding exactly `turns` operator
// turns: for each one an operator row, the request row it opened, and the
// assistant row that answered it — the shape internal/gopilog.SessionTurnBounds
// reads its boundaries from (so the fixture is measured by the SAME counter
// production uses, never by a count this test asserts into existence).
func chatResumeTurnFixture(t *testing.T, gopiHome, sessionID string, turns int) string {
	t.Helper()
	dir := chatResumeSessionDir(gopiHome, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	raw := filepath.Join(dir, sessionID+".raw.log")
	ckpt := `{"model":"deepseek-v4-flash","stream":true,` +
		`"system":[{"type":"text","text":"sys"}],` +
		`"messages":[{"role":"user","content":"the crashed run's question"}],"thinking":null}`
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"+ckpt+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	lines := []string{
		`{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"` + sessionID +
			`","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-10-01T00:00:00Z"}`,
	}
	for i := 0; i < turns; i++ {
		n := i + 1
		lines = append(lines,
			`{"type":"user","message":{"content":"question `+itoaForTest(n)+`","role":"user"},"promptId":"p`+itoaForTest(n)+`"}`,
			`{"type":"request","iteration":0,"prompt_tokens":10,"completion_tokens":2}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"answer `+itoaForTest(n)+`"}],"role":"assistant"}}`,
		)
	}
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	return path
}

// itoaForTest lives in chat_status_ladder_test.go (package-wide); this file uses
// that one definition rather than a second (Rule 14).

// TestChatResumeTurnBaselineIsTheLogsOrdinals: the baseline is the number of
// turn-opening rows the LOG holds (gopilog.SessionTurns) — the very producer of
// the ordinals `session log <id> --turns` prints and `session replay <id>
// --turn N` selects by — and NOT the ledger's `chats.turn_count`.
//
// THE FIXTURE IS BUILT SO THE TWO NUMBERS DIFFER ON PURPOSE (log 3, ledger 5): a
// fixture where they coincide cannot tell the fixed reader from the old one. The
// ledger's number is the COMPLETED-turn count and a turn-opening row is a
// different quantity (RecordChatTurnOpened is not called for an interrupted or
// failed turn), so anchoring the screen on it puts the screen one producer behind
// the ordinals — measured on session e1e22e9f-eee0-4c33-a238-0fca04534ad9 on
// 2026-11-04: ledger 619, log 621, screen banner 620, and 16 of 16 readable
// banners sat exactly one BELOW the row carrying the same prompt.
func TestChatResumeTurnBaselineIsTheLogsOrdinals(t *testing.T) {
	gopiHome := t.TempDir()
	t.Setenv(config.GopiDirEnv, gopiHome)

	const turns = 3
	const ledgerTurns = 5
	path := chatResumeTurnFixture(t, gopiHome, chatResumeFixtureID, turns)

	// The premise: the log really holds 3 turns by the PRODUCTION counter, so the
	// assertion below is about the baseline reaching the LOG's number rather than
	// a value this file decided on.
	s, err := gopilog.ParseFile(path)
	if err != nil {
		t.Fatalf("parse the fixture session: %v", err)
	}
	if got := gopilog.SessionTurns(s); got != turns {
		t.Fatalf("the fixture session holds %d turn(s) by gopilog.SessionTurns, want %d — the fixture is not the shape this case asserts", got, turns)
	}
	// The index row, recorded through the same start hook the live surfaces call,
	// with `ledgerTurns` turns charged to it.
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: chatResumeFixtureID, Mode: tools.ChatModeCLI, Model: "deepseek-v4-flash",
		FirstMsg: "the crashed run's question", Workdir: gopiHome, LogPath: path,
	}); err != nil {
		t.Fatalf("seed the index row: %v", err)
	}
	for i := 0; i < ledgerTurns; i++ {
		if err := tools.ChatRecordTurn(chatResumeFixtureID, 10, 2, 0); err != nil {
			t.Fatalf("record turn %d: %v", i+1, err)
		}
	}

	// THE PRODUCTION CALL SHAPE: the run resolves its resume target to a raw log
	// FIRST (resolveResumeLogPath), then asks for the baseline with both the word
	// the operator typed and the raw log it resolved to. Calling with only the
	// session id would test a shape no run has.
	rawPath, err := resolveResumeLogPath(chatResumeFixtureID)
	if err != nil {
		t.Fatalf("resolve the resume target: %v", err)
	}
	got, err := chatResumeTurnBaseline(chatResumeFixtureID, rawPath)
	if err != nil {
		t.Fatalf("chatResumeTurnBaseline: %v", err)
	}
	if got != turns {
		t.Fatalf("chatResumeTurnBaseline(%q) = %d, want the LOG's %d turn-opening rows — the resumed screen would number its next heading %d while `session replay --turn %d` selects a different turn (the ledger's %d is a COMPLETED-turn count, a different quantity)",
			chatResumeFixtureID, got, turns, got+1, got+1, ledgerTurns)
	}
}

// TestChatResumeTurnBaselineIsZeroWithoutARecord: no session record resolved is
// NOT a failure — it is a fresh numbering. A refusal here would make resuming a
// bare raw log impossible.
func TestChatResumeTurnBaselineIsZeroWithoutARecord(t *testing.T) {
	gopiHome := t.TempDir()
	t.Setenv(config.GopiDirEnv, gopiHome)
	if err := os.MkdirAll(filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}

	got, err := chatResumeTurnBaseline("no-such-session", "")
	if err != nil {
		t.Fatalf("chatResumeTurnBaseline on an absent record: %v", err)
	}
	if got != 0 {
		t.Fatalf("chatResumeTurnBaseline on an absent record = %d, want 0 (a run that continues nothing numbers from 1)", got)
	}
}

// TestChatResumeTurnDisplayIsBaselinePlusLocal pins the DERIVATION the six display
// call sites share: the heading of the first new turn of a resumed run is N+1.
//
// It is a pure-function case on purpose. The same number is read from the grid in
// TestChatResumeTurnHeadingOnTheGrid below; this one states the arithmetic once so
// a regression in either half names itself.
func TestChatResumeTurnDisplayIsBaselinePlusLocal(t *testing.T) {
	const baseline = 19
	sess := &chatSession{turnBaseline: baseline}

	// The first new turn: no turn has completed in THIS process yet, so the local
	// count is 0 and the heading the REPL asks for is baseline+1.
	if got, want := sess.chatTurnDisplay(0+1), baseline+1; got != want {
		t.Fatalf("the first new turn's display number = %d, want %d (the session already held %d)", got, want, baseline)
	}
	// After one turn completed in this process the status row shows baseline+1.
	if got, want := sess.chatTurnDisplay(1), baseline+1; got != want {
		t.Fatalf("the status row after one turn = %d, want %d", got, want)
	}
	// A fresh session (no resume) is unchanged: baseline 0 keeps today's numbering.
	fresh := &chatSession{}
	if got, want := fresh.chatTurnDisplay(1), 1; got != want {
		t.Fatalf("a fresh session's first turn = %d, want %d — a non-resumed run must be byte-identical to before", got, want)
	}
}

// TestChatResumeHeadingAndStatusOnTheGrid is (a) asserted where the operator reads
// it: the rendered grid, through the SAME functions the REPL calls — chatBeginTurn
// for the body heading and publishChatStatus for the status row.
func TestChatResumeHeadingAndStatusOnTheGrid(t *testing.T) {
	h := newStreamHarness(t)
	status := newChatStatusSource("glm-5.3-flash", false)
	h.in.SetStatus(status.status)

	// A session whose log already held 19 turns.
	sess := &chatSession{turnBaseline: 19}

	renderer := newChatTurnRenderer(h.in)
	chatBeginTurn(renderer, sess.chatTurnDisplay(0+1), chatOutbound{Message: "the new question", Display: "the new question"})
	renderer.Write("the resumed answer\n")
	renderer.Finish()
	renderer.EndTurn()
	publishChatStatus(h.in, status, sess.chatTurnDisplay(1), statusLiveIdleTurn, 1234)

	h.waitFor(t, "the resumed turn's number on both the heading and the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Contents(), "turn 20") &&
			strings.Contains(h.screen.Line(h.layout().StatusRow), "turn20")
	})

	// And the OLD number must be gone from the heading: a screen carrying both
	// "turn 1" and "turn 20" would mean the baseline reached only one seam.
	band := h.rows(h.layout().EditorTop-1, h.layout().StatusRow)
	if strings.Contains(band, "turn 1 ") {
		t.Fatalf("the screen still carries an un-baselined heading — one seam was missed:\n--- screen ---\n%s", h.screen.Contents())
	}
}

// TestChatResumeDoesNotInflateTheSessionEndIterations is (b): a REAL resumed run
// must write `session_end` total_iterations reflecting only its own turns, never
// the session's existing turn count added in.
//
// This is the leg that a fix seeding iterCount would break, and it is why the
// baseline lives on chatSession instead of in iterCount.
func TestChatResumeDoesNotInflateTheSessionEndIterations(t *testing.T) {
	gopiHome, env := chatResumeSandbox(t)
	const existingTurns = 3
	path := chatResumeTurnFixture(t, gopiHome, chatResumeFixtureID, existingTurns)
	// THE LEDGER IS THE BASELINE'S SOURCE, so the fixture must record it. This
	// arm was written when chatResumeTurnBaseline counted the LOG; 7734b63 moved
	// that number onto `chats.turn_count` (the one turn count every surface
	// prints), and a fixture that only writes the log then reads back a session
	// the index has no row for — baseline 0, no notice, and this test went red
	// for a reason that had nothing to do with the close-out it guards.
	// Seed it through the production writer the sibling arm above uses, so the
	// two arms cannot drift apart in how they say "this session holds N turns".
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: chatResumeFixtureID, Mode: tools.ChatModeCLI, Model: "deepseek-v4-flash",
		FirstMsg: "the crashed run's question", Workdir: gopiHome, LogPath: path,
	}); err != nil {
		t.Fatalf("seed the index row: %v", err)
	}
	for i := 0; i < existingTurns; i++ {
		if err := tools.ChatRecordTurn(chatResumeFixtureID, 10, 2, 0); err != nil {
			t.Fatalf("record turn %d: %v", i+1, err)
		}
	}

	// ONE operator line, so exactly ONE turn runs in this process.
	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbChat, chatFlagResume, chatResumeFixtureID, chatFlagNoEditor},
		chatResumeProbeLine+"\n\n", env, "")
	if code != chatExitOK {
		t.Fatalf("`gopi chat %s <id>` = %d, want %d\nstdout:\n%s\nstderr:\n%s",
			chatFlagResume, code, chatExitOK, stdout, stderr)
	}

	// The operator is TOLD the baseline, so the two numbers can never disagree
	// without a sentence saying which one they are in.
	wantNotice := "this session already holds 3 turn(s) — the next turn is turn 4"
	if !strings.Contains(stderr, wantNotice) {
		t.Fatalf("the run did not state the turn it continues from (want %q)\nstderr:\n%s", wantNotice, stderr)
	}

	iterations := chatResumeSessionEnd(t, gopiHome, chatResumeFixtureID)
	if iterations > existingTurns {
		t.Fatalf("session_end total_iterations = %d for a run that ran at most one turn, with %d turns already on disk — the turn baseline was folded into the PASS count (a resumed session must not report passes it never ran)",
			iterations, existingTurns)
	}
}

// chatResumeProbeLine is the one line the resumed probe types.
const chatResumeProbeLine = "and one more question"

// chatResumeSessionEnd reads back the `session_end` the resumed run wrote, from
// the RESUMED SESSION'S OWN record — the fixture's directory.
//
// THE PREMISE THIS HELPER WAS WRITTEN ON IS GONE, and it was gone before this
// test noticed: it used to look for "the one session directory that is NOT the
// fixture", because `--resume` minted a NEW session and wrote its record there.
// d258d1e made a resume CONTINUE the id it was handed (so the conversation grows
// instead of leaving a second directory beside it), so the run now appends to
// the fixture's own `session.jsonl` — measured: the fixture directory is the
// ONLY session directory the run leaves, and after the run it carries
// `session_end` (3253 bytes). The old search therefore skipped the only record
// that existed and reported "the close-out never landed" for a close-out that
// had landed. The second directory is the defect d258d1e removed, so its
// reappearance is stated as an error here rather than silently followed.
//
// The shape is stated HERE rather than taken from sessionevent because that
// package owns the EVENT VOCABULARY (type names, field names) and no exported
// struct decodes session_end; a test that needs one number decodes the one
// number instead of asking production for a type it does not otherwise need.
func chatResumeSessionEnd(t *testing.T, gopiHome, fixtureID string) int {
	t.Helper()
	root := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read sessions root: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != fixtureID {
			t.Errorf("the resumed run left a SECOND session directory (%s) beside %s — a resume continues the session it was handed, it does not mint another (d258d1e)",
				e.Name(), fixtureID)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, fixtureID, "session.jsonl"))
	if err != nil {
		t.Fatalf("read the resumed session's record: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, `"type":"`+sessionevent.EventTypeSessionEnd+`"`) {
			continue
		}
		var end struct {
			TotalIterations int `json:"total_iterations"`
		}
		if err := json.Unmarshal([]byte(line), &end); err != nil {
			t.Fatalf("parse session_end from %s: %v", fixtureID, err)
		}
		return end.TotalIterations
	}
	t.Fatalf("no session_end was written for the run (looked in %s) — the close-out never landed", filepath.Join(root, fixtureID, "session.jsonl"))
	return 0
}
