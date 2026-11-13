package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/sessionevent"
)

// cli_session_replay_units_test.go — the CROSS-SURFACE pin for the session
// counter epic, run over the REAL fixture log through the REAL binary.
//
// ONE DEFINITION OF "round" (the operator's ruling, 2026-10-12). A round is
// exactly one JSONL `request` row — one HTTP request/response, one iteration —
// and every surface prints THAT number: `gopi session log <id> --rounds`,
// `gopi session replay <id> --rounds`'s `rounds=`, `gopi session stats`' round
// figure, the log's own iteration count and the count of `type=="request"` lines
// in the session JSONL. The operator's words: "round ≡ iteration ≡ 一次 HTTP
// request/response（一行 JSONL `request`），notify 消息和人类输入都算".
//
// THE DEFECT THIS PINS, AND WHY IT IS TESTED THROUGH THE BINARY. An earlier
// change (commit c04bcb345) split the word: it renamed this quantity EXCHANGE and
// lent ROUND to the operator's own row, so one flag spelled `--rounds` answered
// with two numbers on two surfaces (measured on the briefing's own session:
// `log --rounds` 1, `replay rounds=` 1, while the log held 14 `request` rows and
// the iteration count was 12). The operator revoked the split: the operator-row
// quantity is a differently-named NEIGHBOUR (`--turns`), and ROUND is the
// `request` row again.
//
// The fixture is the shared real one (`statsFixtureSandbox`): fc426fed, whose log
// holds several `request` rows under ONE operator row — the shape that makes the
// two quantities visibly different, so a test that conflated them could not pass
// vacuously on a log where they happen to agree.
func TestEverySurfacePrintsOneRoundDefinition(t *testing.T) {
	id := statsFixtureSandbox(t)
	home := os.Getenv(config.GopiDirEnv)
	if home == "" {
		t.Fatalf("statsFixtureSandbox did not pin %s", config.GopiDirEnv)
	}
	logPath := filepath.Join(statsFixtureRootRel, config.SessionsDirName, id+".jsonl")

	// (0) THE INDEPENDENT GROUND TRUTH: count `type=="request"` lines in the
	// session JSONL by reading the FILE, not the reader — a test whose
	// expectation comes out of the same code it checks stays green under
	// mutation.
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read the fixture log: %v", err)
	}
	requestRows := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(trimmed), &ev) != nil {
			continue
		}
		if ev.Type == "request" {
			requestRows++
		}
	}
	if requestRows == 0 {
		t.Fatalf("the fixture log %s carries no `request` row — this case cannot check what a round is", logPath)
	}

	sess, err := gopilog.ParseFile(logPath)
	if err != nil {
		t.Fatalf("parse the fixture session: %v", err)
	}
	rounds := gopilog.SessionRounds(sess)
	turns := gopilog.SessionTurns(sess)
	if rounds != requestRows {
		t.Fatalf("the reader says %d rounds, the JSONL holds %d `type==\"request\"` lines — the reader's round count IS that line count", rounds, requestRows)
	}
	if turns == rounds {
		t.Fatalf("the fixture no longer distinguishes a turn from a round (%d each) — this case would pass vacuously, so it needs a log with several rounds per operator row", turns)
	}

	logRoundsOut, err := runGopiCapture(t, home, CliVerbSession, CliVerbLog, id, "--"+logFlagRounds)
	if err != nil {
		t.Fatalf("`session log --rounds` failed: %v\n%s", err, logRoundsOut)
	}
	logTurnsOut, err := runGopiCapture(t, home, CliVerbSession, CliVerbLog, id, "--"+logFlagTurns)
	if err != nil {
		t.Fatalf("`session log --turns` failed: %v\n%s", err, logTurnsOut)
	}
	replayOut, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--"+logFlagRounds)
	if err != nil {
		t.Fatalf("`session replay --rounds` failed: %v\n%s", err, replayOut)
	}

	// (1) `log --rounds` tabulates THE ROUND — the `request`-row count, which is
	// the ground truth counted off the file above. The check is the (word,
	// number) PAIRING, not the surrounding punctuation: the header now reads
	// "1 turn(s), 9 rounds" (the operator's ruling, 2026-10-22), so pinning the
	// old "(%d round(s))" spelling would pin the punctuation instead of the
	// quantity the test exists to defend.
	if want := fmt.Sprintf("%d %s", requestRows, sessionevent.CounterUnitRound); !strings.Contains(logRoundsOut, want) {
		t.Fatalf("`session log --rounds` does not tabulate the %d `request` row(s) as %q — one definition of round across the surfaces:\n%s",
			requestRows, want, logRoundsOut)
	}
	if !strings.Contains(logRoundsOut, gopilog.SessionRoundTableWord()) {
		t.Fatalf("`session log --rounds` does not head its table %q:\n%s", gopilog.SessionRoundTableWord(), logRoundsOut)
	}

	// (2) `replay --turns` is the USER surface and carries ONE number: the TURN
	// count. The retired `steps=` field must not appear in its output at all,
	// and neither must a session-level `rounds=`: the transport's number is the
	// internal listing's business, not this one's.
	turnsOut, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turns")
	if err != nil {
		t.Fatalf("`session replay --turns` failed: %v\n%s", err, turnsOut)
	}
	if got := replayField(t, turnsOut, "turns"); got != turns {
		t.Fatalf("`session replay --turns` reports turns=%d, want %d:\n%s", got, turns, turnsOut)
	}
	if strings.Contains(turnsOut, "steps") {
		t.Fatalf("`session replay --turns` prints the retired word `steps` — the playback has ONE unit, the TURN:\n%s", turnsOut)
	}
	if strings.Contains(turnsOut, "rounds=") {
		t.Fatalf("`session replay --turns` prints a session-level `rounds=` field — the operator surface reports turns only:\n%s", turnsOut)
	}
	// (2b) The INTERNAL round listing still tabulates the transport's unit, and
	// it must say so in its own output rather than relying on the -h page.
	if got := replayField(t, replayOut, "rounds"); got != requestRows {
		t.Fatalf("`session replay --rounds` reports rounds=%d, want the %d `request` row(s):\n%s", got, requestRows, replayOut)
	}
	if !strings.Contains(replayOut, "[internal]") {
		t.Fatalf("the round listing does not mark itself internal:\n%s", replayOut)
	}

	// (2a) THE ROUND TABLE'S OWN COUNT IS THE ROUND COUNT, AND THE LOOP'S OWN
	// COUNT IS A SEPARATE, NAMED LINE. Both are printed by the same table and
	// they are NOT the same quantity: the round count comes from the JSONL's
	// `request` rows, the loop's from the log's own `session_end`
	// total_iterations. In this fixture the two happen to be EQUAL (the log
	// recorded an end and made one request per round) — and that coincidence is
	// exactly why the assertion below is on the WORDS: a table that printed the
	// loop's figure under a word belonging to the round count would read as
	// truth on this fixture and as a lie on a chat session, where a
	// notify-driven or failed REPL pass makes them differ (measured
	// 5c6cd329: 31 passes, 134 rounds).
	if want := fmt.Sprintf("%d %s", requestRows, sessionevent.CounterUnitRound); !strings.Contains(logRoundsOut, want) {
		t.Fatalf("`session log --rounds` does not tabulate the reader's %d round(s) as %q:\n%s", requestRows, want, logRoundsOut)
	}
	if want := fmt.Sprintf("%s = %d %s", gopilog.SessionPassRowWord(), sess.TotalIterations,
		sessionevent.CounterUnitLoopPass); !strings.Contains(logRoundsOut, want) {
		t.Fatalf("`session log --rounds` does not print the loop's own count as %q — the loop's figure is NOT the round count and must not borrow its word:\n%s",
			want, logRoundsOut)
	}

	// (3) THE TURN TABLE is the neighbour, and it prints ITS OWN count: it must
	// not report the round count under the turn's name, and the round table must
	// not report the turn count under the round's (a change that swapped them
	// back fails here rather than reading as truth). The assertion is the
	// (word, number) PAIRING — `"9 rounds"` must be absent when 9 is the TURN
	// count — because a punctuation-shaped check passes vacuously once the
	// surrounding spelling moves (measured: the "(%d round(s))" form it used
	// before this ruling appears in no surface any more).
	// The banner names the SUBSET now (`SESSION HUMAN TURNS — <id> (<n>)`), not
	// the bare word: `turns` is `chats.turn_count`, which this package cannot
	// read (the operator's ruling, 2026-10-27).
	if want := fmt.Sprintf("(%d)", turns); !strings.Contains(logTurnsOut, want) {
		t.Fatalf("`session log --turns` does not tabulate the reader's %d human turn(s) (%q):\n%s", turns, want, logTurnsOut)
	}
	if !strings.Contains(logTurnsOut, gopilog.SessionTurnTableWord()) {
		t.Fatalf("`session log --turns` banner does not name the subset %q:\n%s", gopilog.SessionTurnTableWord(), logTurnsOut)
	}
	if got := fmt.Sprintf("%d %s", turns, sessionevent.CounterUnitRound); strings.Contains(logRoundsOut, got) {
		t.Fatalf("`session log --rounds` reports the TURN count (%d) under the round's word (%q) — the operator row is not a round:\n%s",
			turns, got, logRoundsOut)
	}
	if got := fmt.Sprintf("(%d)", requestRows); strings.Contains(logTurnsOut, got) {
		t.Fatalf("`session log --turns` reports the ROUND count (%d) as its turn total:\n%s", requestRows, logTurnsOut)
	}

	// (4) NO SURFACE spells the retired unit word as a quantity name any more.
	// EXCHANGE was the split's word for the `request` row (commit c04bcb345); the
	// operator's ruling retired it, so a table headed by it — or a flag named
	// after it — is the defect coming back.
	for _, out := range []string{logRoundsOut, logTurnsOut, replayOut} {
		if strings.Contains(out, "exchange") || strings.Contains(out, "EXCHANGE") {
			t.Fatalf("a surface still prints the retired unit word EXCHANGE — the `request`-row unit is %q again (operator's ruling, 2026-10-12):\n%s",
				sessionevent.CounterUnitRound, out)
		}
	}
}

// replayField reads a `name=<int>` field out of the playback's static listing.
// It fails loudly when the field is ABSENT — "the number was not printed" and
// "the number was 0" must never read the same (Rule 15).
func replayField(t *testing.T, out, name string) int {
	t.Helper()
	m := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(name) + `=(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("`session replay --rounds` prints no %q field:\n%s", name, out)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s=%q is not a number:\n%s", name, m[1], out)
	}
	return n
}
