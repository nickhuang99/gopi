// cli_session_surface_axis_test.go — the EPIC's consistency criterion, at the
// seam a test can observe: ONE session, the THREE operator-facing surfaces, ONE
// turn axis, and no ROUND word on the two operator-facing lines.
//
// THE THREE SURFACES, and why each is read from its own renderer:
//
//  1. `gopi session show <id>`'s `turns` row — counted by gopilog.SessionTurns,
//     over SessionTurnBounds. The reverse lookup indexes the SAME derivation, so
//     the two numbers are one fact (asserted below).
//  2. `gopi session context`'s SCREEN LINE — ctxdump.Summary.Line, the one line
//     the export prints (the payload goes to the file).
//  3. the export's PAGE TITLE — ctxdump's H1, written by the RENDERER from the
//     same Address, so the file's name, its title and its screen line cannot
//     drift. `/context` (the chat door) prints SUMMARY.LINE VERBATIM (see
//     commands_session.go), so surface 2 covers both doors — there is no fourth
//     renderer.
//
// WHY THE EXPORT'S TURN IS NOT ASSERTED EQUAL TO THE `turns` ROW. They answer
// different questions and legitimately differ: `session show`'s row is the
// session's turn COUNT (36 on the measured session), while the export names
// WHICH turn its bytes hold (the last one) — 36 only because it is the last of
// 36. The property the epic is about is the one asserted: the export's turn can
// never EXCEED the count, because both come from the one boundary algorithm.
//
// AND THE ROUND WORD is asserted on the FURNITURE only (the screen line and the
// H1), never on the whole page: the system prompt below the title is verbatim
// model bytes and does mention rounds at length — this repository's own rules
// do. That rule wins over any cosmetic one; internal/ctxdump/render_test.go
// draws the same line for the same reason.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/ctxdump"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/rawlogfmt"
)

// The fixture's two quantities, named once (Rule 1). The round count is NOT a
// multiple of the turn count on purpose: an equal split hides an off-by-one, and
// equal totals make both axes print the same number — the blind fixture
// internal/ctxdump/address_test.go's header warns about.
const (
	axisFixtureTurns  = 3
	axisFixtureRounds = 7
)

// axisFixtureWrite builds ONE session record and its paired raw log: 3 turns
// covering 7 rounds (3 + 3 + 1), so a surface that read the round ordinal as a
// turn would print a number the session's own turn count contradicts.
func axisFixtureWrite(t *testing.T) (sessionPath, rawLogPath string) {
	t.Helper()
	dir := t.TempDir()
	sessionPath = filepath.Join(dir, "axis-surface.jsonl")

	lines := []string{
		`{"type":"session_start","sessionId":"axis-surface-fixture","model":"fixture","timestamp":"2026-11-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"first ask"}}`,
	}
	perTurn := []int{3, 3, 1}
	round := 0
	for turn, n := range perTurn {
		for i := 0; i < n; i++ {
			round++
			lines = append(lines, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reply `+
				strconv.Itoa(round)+`"}]}}`)
			lines = append(lines, `{"type":"request","iteration":`+strconv.Itoa(round)+
				`,"prompt_tokens":10,"cache_hit":1,"cache_miss":9,"completion_tokens":2,"tagged_usage":true,"usage_base":"delta"}`)
		}
		if turn < len(perTurn)-1 {
			lines = append(lines, `{"type":"user","message":{"role":"user","content":"next ask"}}`)
		}
	}
	if err := os.WriteFile(sessionPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write session fixture: %v", err)
	}

	rawLogPath = filepath.Join(dir, "axis-surface.raw.log")
	f, err := os.Create(rawLogPath)
	if err != nil {
		t.Fatalf("create raw log fixture: %v", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Fatalf("close raw log fixture: %v", cerr)
		}
	}()
	for i := 1; i <= axisFixtureRounds; i++ {
		req := deepseek.ClaudeRequest{
			Model: "fixture",
			Messages: []deepseek.ClaudeMessage{{
				Role:    "user",
				Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "round-" + strconv.Itoa(i)}},
			}},
		}
		body, merr := json.Marshal(req)
		if merr != nil {
			t.Fatalf("marshal round %d: %v", i, merr)
		}
		if _, werr := f.Write(rawlogfmt.Encode(rawlogfmt.TagRequest, body)); werr != nil {
			t.Fatalf("write round %d: %v", i, werr)
		}
	}
	return sessionPath, rawLogPath
}

// TestTheThreeSurfacesNameOneTurnAxis is the epic's criterion.
func TestTheThreeSurfacesNameOneTurnAxis(t *testing.T) {
	sessionPath, _ := axisFixtureWrite(t)
	sess, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		t.Fatalf("parse the fixture session: %v", err)
	}
	turns := gopilog.SessionTurns(sess)
	if turns != axisFixtureTurns {
		t.Fatalf("the fixture records %d turn(s), want %d — the assertions below would measure the wrong fixture", turns, axisFixtureTurns)
	}
	if got := gopilog.SessionRounds(sess); got != axisFixtureRounds {
		t.Fatalf("the fixture records %d round(s), want %d", got, axisFixtureRounds)
	}

	// THE LOOKUP AND THE SHOW ROW ARE ONE FACT: both derive from
	// SessionTurnBounds, so a drift here is the second derivation the epic bans
	// showing up where it would first be visible.
	ix := gopilog.BuildTurnRoundIndex(sess)
	if ix.Total() != turns {
		t.Errorf("the lookup index holds %d turns but the show row would print %d — two answers to one question", ix.Total(), turns)
	}

	// SURFACE 1 → 3 — the address the export is written under is the session's
	// OWN last turn, resolved through the one algorithm (TurnAddressFor), never
	// the round ordinal.
	addr, err := ctxdump.TurnAddressFor(sessionPath, 0)
	if err != nil {
		t.Fatalf("TurnAddressFor(--last): %v", err)
	}
	if addr.Turn != turns {
		t.Errorf("the export's last turn is %d but the session's turn count is %d — `--last` is the LAST turn", addr.Turn, turns)
	}
	if addr.Turn > turns {
		t.Errorf("the export names turn %d, past the session's own %d turns", addr.Turn, turns)
	}

	// SURFACE 2 — the screen line: the TURN, and the round word nowhere on it.
	sum := ctxdump.Summary{Path: "/tmp/context.md", Bytes: 1, Address: addr}
	line := sum.Line()
	if !strings.Contains(line, "TURN "+strconv.Itoa(addr.Turn)) {
		t.Errorf("the screen line %q does not name TURN %d", line, addr.Turn)
	}
	if n := strings.Count(strings.ToLower(line), "round"); n != 0 {
		t.Errorf("the screen line carries the round word %d time(s): %q", n, line)
	}

	// SURFACE 3 — the page title, written by the renderer from the same address.
	var page bytes.Buffer
	req := &deepseek.ClaudeRequest{Model: "fixture", MaxTokens: 8}
	if _, rerr := ctxdump.Render(&page, req, addr); rerr != nil {
		t.Fatalf("render the page: %v", rerr)
	}
	title := strings.SplitN(page.String(), "\n", 2)[0]
	if !strings.Contains(title, "turn "+strconv.Itoa(addr.Turn)) {
		t.Errorf("the page title %q does not name turn %d", title, addr.Turn)
	}
	if n := strings.Count(strings.ToLower(title), "round"); n != 0 {
		t.Errorf("the page title carries the round word %d time(s): %q", n, title)
	}

	// AND THE ROUND AXIS DECLARES ITSELF — the other half of the ruling: a round
	// may appear, but only where it says it is NOT a turn.
	roundLine := ctxdump.Summary{Path: "/tmp/r.md", Bytes: 1,
		Address: ctxdump.RoundAddress(5)}.Line()
	for _, want := range []string{"ROUND 5", "NOT a turn"} {
		if !strings.Contains(roundLine, want) {
			t.Errorf("the round-scoped screen line %q does not carry %q", roundLine, want)
		}
	}
	var roundPage bytes.Buffer
	if _, rerr := ctxdump.Render(&roundPage, req, ctxdump.RoundAddress(5)); rerr != nil {
		t.Fatalf("render the round page: %v", rerr)
	}
	roundTitle := strings.SplitN(roundPage.String(), "\n", 2)[0]
	for _, want := range []string{"ROUND 5", "NOT a turn"} {
		if !strings.Contains(roundTitle, want) {
			t.Errorf("the round page title %q does not carry %q", roundTitle, want)
		}
	}
}
