package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// ---- parsing ----

func TestParseStatsArgsSelectors(t *testing.T) {
	opts, err := parseStatsArgs([]string{"abc"})
	if err != nil {
		t.Fatalf("session id: %v", err)
	}
	if opts.sessionID != "abc" || opts.lastN != 0 || opts.all {
		t.Errorf("session id parse = %+v", opts)
	}
	if opts, err = parseStatsArgs([]string{"--last"}); err != nil || opts.lastN != 1 {
		t.Errorf("--last: opts=%+v err=%v, want lastN=1", opts, err)
	}
	if opts, err = parseStatsArgs([]string{"--last", "7"}); err != nil || opts.lastN != 7 {
		t.Errorf("--last 7: opts=%+v err=%v, want lastN=7", opts, err)
	}
	if opts, err = parseStatsArgs([]string{"--all"}); err != nil || !opts.all {
		t.Errorf("--all: opts=%+v err=%v", opts, err)
	}
	if opts, err = parseStatsArgs([]string{"abc", "--rounds", "--tools", "--json"}); err != nil {
		t.Fatalf("view flags: %v", err)
	}
	if !opts.rounds || !opts.tools || !opts.json {
		t.Errorf("view flags not set: %+v", opts)
	}
	if opts, err = parseStatsArgs([]string{"abc", "--bytes"}); err != nil {
		t.Fatalf("--bytes: %v", err)
	}
	if !opts.bytes {
		t.Errorf("--bytes not set: %+v", opts)
	}
	// --cost / --bill are booleans like the other view flags.
	if opts, err = parseStatsArgs([]string{"abc", "--cost"}); err != nil {
		t.Fatalf("--cost: %v", err)
	}
	if !opts.cost {
		t.Errorf("--cost not set: %+v", opts)
	}
	if opts, err = parseStatsArgs([]string{"abc", "--bill"}); err != nil {
		t.Fatalf("--bill: %v", err)
	}
	if !opts.bill {
		t.Errorf("--bill not set: %+v", opts)
	}
	if opts, err = parseStatsArgs([]string{"abc", "--cost", "--bill", "--rounds"}); err != nil {
		t.Fatalf("--cost --bill --rounds: %v", err)
	}
	if !opts.cost || !opts.bill || !opts.rounds {
		t.Errorf("combined flags not all set: %+v", opts)
	}
	// --cost is legal with --all parse-wise (rejected at render, not parse).
	if opts, err = parseStatsArgs([]string{"--all", "--cost"}); err != nil {
		t.Fatalf("--all --cost: %v", err)
	}
	if !opts.cost || !opts.all {
		t.Errorf("--all --cost parse: %+v", opts)
	}
	// a duplicate boolean flag is harmless
	if _, err = parseStatsArgs([]string{"abc", "--json", "--json"}); err != nil {
		t.Errorf("duplicate boolean flag rejected: %v", err)
	}
}

func TestParseStatsArgsRejects(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{}, "pick one input"},
		{[]string{"a", "--all"}, "pick one input"},
		{[]string{"a", "--last"}, "pick one input"},
		{[]string{"--all", "--last"}, "pick one input"},
		{[]string{"a", "--last"}, "pick one input"},
		{[]string{"--last", "0"}, "--last must be a positive integer"},
		{[]string{"--last", "-3"}, "--last must be a positive integer"},
		{[]string{"a", "b"}, "unexpected extra argument"},
		{[]string{"a", "--bogus"}, "unknown flag --bogus"},
	}
	for _, c := range cases {
		_, err := parseStatsArgs(c.args)
		if err == nil {
			t.Errorf("args %v accepted, want an error mentioning %q", c.args, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("args %v → %q, want it to mention %q", c.args, err, c.want)
		}
	}
}

// ---- CLI over the real-session fixture ----

// TestStatsCLIOverFixture drives the CLI's own path (session lookup, raw-log
// pairing, parse, render) against the real-session fixture and compares the
// bytes with the committed goldens — the same snapshots the gopilog golden
// tests assert, so the CLI layer is proven to add no decoration.
//
// THE READER IS CLEARED FIRST, and that is the point of sharing one snapshot
// with internal/gopilog (measured 2026-10-27, task 01a17ae7). The ledger turn
// reader is INJECTED per process: cmd/agent's TestMain installs it
// (installLedgerTurnCounter), `go test ./internal/gopilog` has no entry point to
// install one, and the committed snapshots therefore render the UNMEASURED
// branch — which `devdoc/session-count-word-collision.md` states outright ("that
// unmeasured branch is what the golden fixtures render"). With the reader left
// installed, THIS test rendered the shipped reason instead
// (tools.ChatLedgerNoRowNote, 125 runes against TurnCountNoReaderNote's 42) and
// the box widened by its own width arithmetic (statsBox adapts to the widest
// row: 155+4 = 159 runes against the golden's 119+4 = 123) — not a stale
// snapshot, a different branch of the same renderer. Clearing the reader makes
// the two processes test the same one; the SHIPPED branch is pinned by
// TestStatsCLIStatesWhyTheLedgerHasNoTurnRow below.
func TestStatsCLIOverFixture(t *testing.T) {
	clearLedgerTurnCounterForGolden(t)
	id := statsFixtureSandbox(t)

	overview, err := runStatsSession(id, &statsOptions{})
	if err != nil {
		t.Fatalf("runStatsSession: %v", err)
	}
	assertGolden(t, "stats-overview.txt", overview)

	rounds, err := runStatsSession(id, &statsOptions{rounds: true})
	if err != nil {
		t.Fatalf("runStatsSession --rounds: %v", err)
	}
	boundary := strings.Index(rounds, "\n══")
	if boundary < 0 {
		t.Fatalf("--rounds output has no overview/table boundary:\n%s", rounds)
	}
	if got := rounds[:boundary+1]; got != overview {
		t.Errorf("--rounds re-renders the overview differently:\n%s", got)
	}
	assertGolden(t, "stats-rounds.txt", rounds[boundary+1:])

	tools, err := runStatsSession(id, &statsOptions{tools: true})
	if err != nil {
		t.Fatalf("runStatsSession --tools: %v", err)
	}
	assertGolden(t, "stats-tools.txt", tools[len(overview):])

	traffic, err := runStatsSession(id, &statsOptions{bytes: true})
	if err != nil {
		t.Fatalf("runStatsSession --bytes: %v", err)
	}
	assertGolden(t, "stats-traffic.txt", traffic[len(overview):])

	js, err := runStatsSession(id, &statsOptions{json: true})
	if err != nil {
		t.Fatalf("runStatsSession --json: %v", err)
	}
	assertGolden(t, "stats-json.json", js)
}

// clearLedgerTurnCounterForGolden renders the committed snapshots on the branch
// they actually hold: NO reader published.
//
// The goldens are SHARED with internal/gopilog (assertGolden reads the same
// files, and the renderer's own recorder is `GOPI_GOLDEN_UPDATE=1 go test
// ./internal/gopilog/`), so whatever a test wants to add to that snapshot it has
// to be reachable in BOTH processes. Only the unmeasured branch is: it is the
// two facts internal/gopilog can produce on its own
// (TurnCountNoReaderNote / TurnCountNoSessionIDNote), while a count or an
// index-absence sentence comes from the injected reader and therefore from
// cmd/agent alone. Restored through t.Cleanup so the rest of the suite keeps the
// shipped wiring (Rule 17: the flip is per-call, the restore is the pair's other
// half).
func clearLedgerTurnCounterForGolden(t *testing.T) {
	t.Helper()
	gopilog.InstallLedgerTurnCount(nil)
	t.Cleanup(installLedgerTurnCounter)
}

// TestStatsCLIStatesWhyTheLedgerHasNoTurnRow pins the branch the shipped binary
// actually renders, which the shared snapshot cannot hold.
//
// THE FACT: with the index in force and no row for this session, the box must
// say WHICH absence it is and name the verb that fixes it — tools'
// ChatLedgerNoRowNote ("… index the logs with `gopi chat backfill`") — and it
// must NOT collapse into the library's "no reader installed" sentence. Those are
// two different facts (Rule 15) and the whole epic this surface came from is
// about a reader not being able to tell two numbers apart.
//
// It ALSO pins the width consequence, because that is what made this defect
// look like two: statsBox adapts the frame to its widest row, so a note 83 runes
// longer widens the frame by exactly that much. The assertion is expressed
// through the same arithmetic (every line of the box is one width) rather than
// as a second committed snapshot, so a future change to the sentence is caught
// here instead of in a golden that cannot represent it.
func TestStatsCLIStatesWhyTheLedgerHasNoTurnRow(t *testing.T) {
	id := statsFixtureSandbox(t)

	out, err := runStatsSession(id, &statsOptions{})
	if err != nil {
		t.Fatalf("runStatsSession: %v", err)
	}
	if !strings.Contains(out, gopilog.TurnCountUnmeasuredPrefix+tools.ChatLedgerNoRowNote) {
		t.Fatalf("the shipped wiring must state the INDEX's absence with the verb that fixes it (%q):\n%s",
			gopilog.TurnCountUnmeasuredPrefix+tools.ChatLedgerNoRowNote, out)
	}
	if strings.Contains(out, gopilog.TurnCountNoReaderNote) {
		t.Errorf("a reader IS installed in this process, so the box must not claim otherwise:\n%s", out)
	}

	// EVERY line is one width, the bottom border included.
	//
	// `statsBox` used to write the bottom rule one rune wider than the frame
	// (W+3 dashes plus two corners = W+5 while every other line was W+4), and
	// the committed snapshots had recorded that drift as if it were the
	// specification (stats-overview.txt 123/124, stats-overview-jsonl.txt 88/89,
	// stats-cost.txt 126/127). Task 01a17b5f settled it the other way: the
	// RENDERER's own comment ("top/row/bottom are W+4 runes") is the design, the
	// drift was the defect, and the frame arithmetic now names its constants
	// (statsBoxFrameOverhead / statsBoxCornerPair). This assertion therefore
	// holds the bottom rule to the same width as the body — the re-recorded
	// goldens are what it measures against.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("overview box has %d lines:\n%s", len(lines), out)
	}
	w := len([]rune(lines[0]))
	for i, line := range lines {
		if n := len([]rune(line)); n != w {
			t.Fatalf("line %d is %d runes, the box frame is %d — statsBox did not keep one width for every line (the bottom rule included):\n%s",
				i+1, n, w, out)
		}
	}
	body := len(lines) - 1
	// …and the row that DECIDED that width is the turns row: it carries the whole
	// sentence, and it fills the frame. Both facts come out of the render, so the
	// assertion does not restate the box's internals (statsKeyWidth is unexported
	// on purpose — a second copy of it is the defect class this file is fixing,
	// not the fix).
	var turnsRow string
	for _, line := range lines[1:body] {
		if strings.Contains(line, gopilog.TurnCountUnmeasuredPrefix) {
			turnsRow = line
			break
		}
	}
	if turnsRow == "" {
		t.Fatalf("no row carries the unmeasured prefix %q:\n%s", gopilog.TurnCountUnmeasuredPrefix, out)
	}
	// THE UNMEASURED SENTENCE SITS ON THE LEDGER'S ROW (`completed`), because the
	// ledger is the only count this block can fail to read: the bare word's count
	// is the parsed session's own and is always measured (epic 2026-11-04).
	if !strings.HasPrefix(turnsRow, "│ "+sessionevent.CounterUnitTurnCompleted+" ") {
		t.Errorf("the unmeasured sentence must sit on the %q row, not another:\n%s",
			sessionevent.CounterUnitTurnCompleted, turnsRow)
	}
	if n := len([]rune(turnsRow)); n != w {
		t.Errorf("the turns row is %d runes but the frame is %d — the note is meant to be the widest row:\n%s",
			n, w, out)
	}
}

// TestStatsCLIFallbackOverFixture: the same fixture with NO raw log must fall
// back to the session JSONL and say so (source=jsonl, 0-based iterations).
func TestStatsCLIFallbackOverFixture(t *testing.T) {
	clearLedgerTurnCounterForGolden(t)
	id := statsFixtureSessionsOnly(t)

	out, err := runStatsSession(id, &statsOptions{})
	if err != nil {
		t.Fatalf("runStatsSession: %v", err)
	}
	if !strings.Contains(out, "session jsonl") || !strings.Contains(out, "(no raw log)") {
		t.Errorf("fallback must state its source:\n%s", out)
	}
	assertGolden(t, "stats-overview-jsonl.txt", out)

	rounds, err := runStatsSession(id, &statsOptions{rounds: true})
	if err != nil {
		t.Fatalf("runStatsSession --rounds: %v", err)
	}
	assertGolden(t, "stats-rounds-jsonl.txt", rounds[len(out):])
}

// TestStatsCLIBareLayoutShowsUUID — regression for the session-dir merge
// display bug: a migrated bare session lives at sessions/<uuid>/session.jsonl
// (a FIXED file name). `gopi session stats <id>` must display the real uuid in the
// title — never "Session session" — across the overview and the JSON form.
// The file name says nothing about the session, so the id must resolve from
// the session_start event (via BuildStatsFromUsage on the jsonl path).
func TestStatsCLIBareLayoutShowsUUID(t *testing.T) {
	id := statsFixtureBareLayout(t)

	for _, c := range []struct {
		name string
		opts *statsOptions
	}{
		{"overview", &statsOptions{}},
		{"json", &statsOptions{json: true}},
	} {
		out, err := runStatsSession(id, c.opts)
		if err != nil {
			t.Fatalf("%s: runStatsSession: %v", c.name, err)
		}
		if strings.Contains(out, "Session session") || strings.Contains(out, `"session_id": "session"`) {
			t.Errorf("%s: bare-layout display id fell back to the file name \"session\":\n%s", c.name, out)
		}
		if !strings.Contains(out, id) {
			t.Errorf("%s: bare-layout output must carry the session uuid %q:\n%s", c.name, id, out)
		}
	}
}

// TestStatsBytesFallbackSaysNoRawLog: --bytes over a jsonl-sourced session has
// no wire bodies to show — it must say so, never print an empty table.
func TestStatsBytesFallbackSaysNoRawLog(t *testing.T) {
	id := statsFixtureSessionsOnly(t)

	out, err := runStatsSession(id, &statsOptions{bytes: true})
	if err != nil {
		t.Fatalf("runStatsSession --bytes: %v", err)
	}
	if !strings.Contains(out, "(no raw log — request/response body sizes unavailable)") {
		t.Errorf("--bytes fallback must state the missing raw log:\n%s", out)
	}
}

// TestStatsBytesRejectedOnAll: --bytes over --all is meaningless (the
// aggregate drops the per-round rows) — reject it, don't print an empty table.
func TestStatsBytesRejectedOnAll(t *testing.T) {
	statsFixtureSandbox(t)
	if _, err := runStatsAll(&statsOptions{all: true, bytes: true}); err == nil {
		t.Fatal("runStatsAll --all --bytes accepted, want an error")
	} else if !strings.Contains(err.Error(), "not --all") {
		t.Errorf("--all --bytes error = %q, want it to point at --all", err)
	}
}

// TestStatsCostOverFixture: --cost over the fixture prices the REAL session's
// CANONICAL rounds — the session's own `request` rows (9 of them), which is the
// round source every surface now reads — at its own session_start (2026-09-04
// 20:40 +08:00, DS off-peak evening): 0.0170 元 = hit 0.0043 (86K @0.05) +
// miss 0.0070 (5K @1.5) + out 0.0057 (1K @4.5). The raw log is still paired —
// it supplies the wire BODIES (wire_log) — but it no longer supplies the round
// count (its 5 `→ REQUEST` markers are not the 9 exchanges this session had).
func TestStatsCostOverFixture(t *testing.T) {
	id := statsFixtureSandbox(t)

	out, err := runStatsSession(id, &statsOptions{cost: true})
	if err != nil {
		t.Fatalf("runStatsSession --cost: %v", err)
	}
	for _, want := range []string{
		"COST", "0.0170 元", "DS v4-flash 平价", "deepseek", "price ref",
		"priced at", "Beijing",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--cost missing %q:\n%s", want, out)
		}
	}
	// money split: hit .0043 / miss .0070 / out .0057 → 25.3% / 41.0% / 33.7%
	if !strings.Contains(out, "25.3%") {
		t.Errorf("--cost hit money share missing ~25%%:\n%s", out)
	}

	bill, err := runStatsSession(id, &statsOptions{bill: true})
	if err != nil {
		t.Fatalf("runStatsSession --bill: %v", err)
	}
	// The table's own labels come from the package that renders it (Rule 13:
	// a value is referenced, never mirrored) — the money column is per ROUND:
	// one JSONL `request` row = one HTTP request/response = one iteration, the
	// unit every session surface numbers (the operator's ruling, 2026-10-12).
	for _, want := range []string{
		"PER-" + gopilog.SessionRoundColumnWord() + " COST",
		gopilog.SessionRoundColumnWord(),
		gopilog.SessionRoundColumnWord() + " 元",
		"HIT 元", "CUM 元", "0.0170",
	} {
		if !strings.Contains(bill, want) {
			t.Errorf("--bill missing %q", want)
		}
	}
	if strings.Contains(bill, "EXCHANGE") {
		t.Errorf("--bill still spells the counted quantity EXCHANGE — the retired word; the `request`-row unit is %q again (the operator's ruling, 2026-10-12):\n%s", sessionevent.CounterUnitRound, bill)
	}
	// --bill alone implies the money view (COST box + curve).
	if !strings.Contains(bill, "COST") {
		t.Errorf("--bill must also render the COST box")
	}
	// Per-tool marginal ranking rides on --bill (research pick: LangSmith/
	// Langfuse per-trace aggregation → gopi per-round attribution). It shows
	// each tool's money share and sums back to the box total.
	for _, want := range []string{"PER-TOOL COST", "MARG 元", "write_task_area", "memory_feedback", "55.2%"} {
		if !strings.Contains(bill, want) {
			t.Errorf("--bill missing per-tool %q", want)
		}
	}
	// Sum-back invariant: the ranked shares never exceed the priced total
	// (rounding keeps them within a hair) — assert the top marginal 元 rows
	// add to the box total 0.0170. The invariant itself lives in the unit
	// test (ToolCostsSumBack); this guards the rendered columns.
	if !strings.Contains(bill, "0.0094") || !strings.Contains(bill, "0.0046") || !strings.Contains(bill, "0.0011") {
		t.Errorf("--bill per-tool money rows wrong:\n%s", bill)
	}

	// JSON with --cost carries the money block; without it the field is absent.
	js, err := runStatsSession(id, &statsOptions{json: true, cost: true})
	if err != nil {
		t.Fatalf("runStatsSession --json --cost: %v", err)
	}
	if !strings.Contains(js, `"total_yuan"`) || !strings.Contains(js, `"regime"`) {
		t.Errorf("--json --cost must carry the cost block")
	}
}

// TestStatsCostUnknownModelSaysSo: a session whose model no price table covers
// must FAIL the --cost view with the reason — never invent a number.
func TestStatsCostUnknownModelSaysSo(t *testing.T) {
	st := &gopilog.SessionStats{Model: "hy4-preview", Source: gopilog.StatsSourceRaw, Sessions: 1, Started: time.Now()}
	if _, err := renderStats(st, &statsOptions{cost: true}); err == nil {
		t.Fatal("--cost on an unpriced model accepted, want an error")
	} else if !strings.Contains(err.Error(), "hy4-preview") {
		t.Errorf("--cost error = %q, want it to name the model", err)
	}
}

// TestStatsCostRejectedOnAll: --cost needs one session's model + start time —
// --all mixes models and dates, so it is rejected (not mis-priced).
func TestStatsCostRejectedOnAll(t *testing.T) {
	statsFixtureSandbox(t)
	if _, err := runStatsAll(&statsOptions{all: true, cost: true}); err == nil {
		t.Fatal("runStatsAll --all --cost accepted, want an error")
	} else if !strings.Contains(err.Error(), "not --all") {
		t.Errorf("--all --cost error = %q, want it to point at --all", err)
	}
}

// TestStatsLastOverFixture checks the per-session listing shape.
func TestStatsLastOverFixture(t *testing.T) {
	id := statsFixtureSandbox(t)
	blocks, err := runStatsLast(3, &statsOptions{})
	if err != nil {
		t.Fatalf("runStatsLast: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1 (the sandbox holds one session)", len(blocks))
	}
	if !strings.Contains(blocks[0], id) {
		t.Errorf("block does not name the session:\n%s", blocks[0])
	}
}

// TestStatsAllOverFixture checks the aggregate over the sandbox.
func TestStatsAllOverFixture(t *testing.T) {
	statsFixtureSandbox(t)
	blocks, err := runStatsAll(&statsOptions{all: true})
	if err != nil {
		t.Fatalf("runStatsAll: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	// The aggregate sums the session JSONL (see the --all note in statsUsage),
	// so it reports the FULL session's 9 rounds — not the 5 the raw fixture
	// carries. The single-session view and --all are both correct.
	for _, want := range []string{"All sessions (1)", "jsonl totals"} {
		if !strings.Contains(blocks[0], want) {
			t.Errorf("aggregate block missing %q:\n%s", want, blocks[0])
		}
	}
	// The count row is matched by SHAPE rather than by the padding between the
	// key and the number (the key column's width is internal/gopilog's, and a
	// test that hard-codes it goes stale the day it changes): the line whose key
	// is the unit word must carry the number 9 — the suite's own exit criterion
	// is the count, not the whitespace (Rule 1: no second spelling of the width).
	if !hasCountRow(blocks[0], sessionevent.CounterUnitRound, 9) {
		t.Errorf("aggregate block has no %q row carrying 9:\n%s", sessionevent.CounterUnitRound, blocks[0])
	}
}

// hasCountRow reports whether a rendered box holds a key/value line for key whose
// value is the single integer want. The box's key column is padded to an
// internal width, so the match is on the two FIELDS, never on the spacing.
func hasCountRow(box, key string, want int) bool {
	for _, line := range strings.Split(box, "\n") {
		fields := strings.Fields(strings.Trim(line, "│ "))
		if len(fields) == 2 && fields[0] == key && fields[1] == strconv.Itoa(want) {
			return true
		}
	}
	return false
}

// TestStatsJSONHasRoundNumbers: the machine-readable form carries the per-round
// rows so a script can do the whole job.
func TestStatsJSONHasRoundNumbers(t *testing.T) {
	id := statsFixtureSandbox(t)
	js, err := runStatsSession(id, &statsOptions{json: true})
	if err != nil {
		t.Fatalf("runStatsSession --json: %v", err)
	}
	for _, want := range []string{
		`"number": 5`,
		`"prompt_tokens": 9448`, // round 1 — verified against the session JSONL
		`"cache_hit": 9344`,
		`"completion_tokens": 97`,
		`"source": "jsonl"`, // the canonical source: the session's own `request` rows
	} {
		if !strings.Contains(js, want) {
			t.Errorf("--json missing %s", want)
		}
	}
	if gopilog.StatsSourceRaw != "raw" {
		t.Errorf("stats source constant changed: %q", gopilog.StatsSourceRaw)
	}
}
