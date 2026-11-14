package gopilog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/pricing"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// Golden tests (IMPLEMENT-SPEC §4 — nick's hard requirement): the fixtures in
// tests/fixtures/log-stats-raw are REAL session logs, and the rendered output
// of stats/raw is compared against committed snapshots. Any change that moves
// a number, a column or a byte of these views fails here, so a future feature
// cannot silently break the regression canary.
//
// Re-record the snapshots after an INTENTIONAL output change with
//	GOPI_GOLDEN_UPDATE=1 go test ./internal/gopilog/ -run TestGolden
// and review the diff of tests/fixtures/log-stats-raw/golden/ like code.

const (
	fixtureRootRel  = "../../tests/fixtures/log-stats-raw"
	goldenUpdateEnv = "GOPI_GOLDEN_UPDATE"
	// fixtureSessionID is the real session the fixture was copied from.
	fixtureSessionID = "fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4"
	// fixtureRawLogName is the raw log paired with fixtureSessionID.
	fixtureRawLogName = "gopi-raw-20260904-204046.log"
)

// fixturePath resolves a path under tests/fixtures/log-stats-raw. go test runs
// with the package directory as CWD, so the relative root is stable for every
// package two levels below the repo root (internal/gopilog, cmd/agent, …).
func fixturePath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(fixtureRootRel, rel)
}

// compareGolden asserts got against the committed snapshot. A missing snapshot
// is written on the first run (and by GOPI_GOLDEN_UPDATE=1), so a new golden
// never fails the suite — it fails the SECOND run if the output changed.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(fixtureRootRel, "golden", name)
	want, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read golden %s: %v", name, err)
		}
		if os.Getenv(goldenUpdateEnv) == "" {
			t.Fatalf("golden %s does not exist yet — run GOPI_GOLDEN_UPDATE=1 go test ./... to record it", name)
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			t.Fatalf("mkdir golden dir: %v", mkErr)
		}
		if wErr := os.WriteFile(path, []byte(got), 0o644); wErr != nil {
			t.Fatalf("write golden %s: %v", name, wErr)
		}
		t.Logf("recorded golden %s (%d bytes)", name, len(got))
		return
	}
	if os.Getenv(goldenUpdateEnv) != "" {
		if wErr := os.WriteFile(path, []byte(got), 0o644); wErr != nil {
			t.Fatalf("rewrite golden %s: %v", name, wErr)
		}
	}
	if got != string(want) {
		t.Errorf("golden %s drifted\n--- want (committed) ---\n%s\n--- got ---\n%s", name, diffHint(string(want), got), got)
	}
}

// diffHint names the first differing line — a drifted golden should say WHERE
// it drifted, not just that it did.
func diffHint(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	for i := 0; i < len(wl) && i < len(gl); i++ {
		if wl[i] != gl[i] {
			return "first difference at line " + itoa(i+1) + ":\n  want: " + wl[i] + "\n  got:  " + gl[i]
		}
	}
	if len(wl) != len(gl) {
		return "line count differs: want " + itoa(len(wl)) + ", got " + itoa(len(gl))
	}
	return "(identical?)"
}

func itoa(n int) string { return strconv.Itoa(n) }

// fixtureStats builds the stats block the CLI builds for the fixture session —
// THE CANONICAL PATH: the round NUMBERS come from the session's own `request`
// rows (BuildStatsFromJSONL), and the raw log contributes only what it uniquely
// holds, the wire bodies (AttachRawTraffic → WireLogDetail). It mirrors
// cmd/agent's buildStats on purpose: TestStatsCLIOverFixture asserts the SAME
// committed snapshots, which is only a statement about the CLI adding no
// decoration while both sides build the same block from the same rule.
//
// WHY THE RAW LOG IS NO LONGER THE ROUND SOURCE (measured 2026-10-11, session
// 62a7079b): the raw HTTP log numbers every `→ REQUEST` marker, including sends
// the session JSONL never recorded, so reading it as the round source made
// `gopi session stats --rounds` print 348 where `session log`/`replay` printed
// 321 on the same session. The raw parse stays under test on its own terms —
// TestBuildStatsFromRoundsNumbers / TestBuildStatsFromRoundsMatchesJSONL and the
// raw view goldens (raw-rounds-index.txt, log-request-system.txt).
func fixtureStats(t *testing.T) *SessionStats {
	t.Helper()
	sessionPath := fixturePath(t, filepath.Join("sessions", fixtureSessionID+".jsonl"))
	sess, err := ParseFile(sessionPath)
	if err != nil {
		t.Fatalf("ParseFile(fixture session): %v", err)
	}
	if SessionRounds(sess) == 0 {
		t.Fatalf("fixture session %s holds no `request` row — the canonical "+
			"source cannot be built from it", fixtureSessionID)
	}
	st := BuildStatsFromJSONL(sess)
	st.SourceDetail = fixtureSessionID + ".jsonl"
	rawPath := fixturePath(t, filepath.Join("raw", fixtureRawLogName))
	if rounds, rerr := ParseHTTPRounds(rawPath); rerr == nil {
		AttachRawTraffic(sess, rounds, st)
		st.WireLogDetail = fixtureRawLogName
	}
	if sec, ok := SessionWallSeconds(sessionPath); ok {
		st.WallSeconds = sec
		st.HasWall = true
	}
	return st
}

func TestGoldenStatsOverview(t *testing.T) {
	st := fixtureStats(t)
	compareGolden(t, "stats-overview.txt", FormatStatsOverview(st))
}

func TestGoldenStatsRounds(t *testing.T) {
	compareGolden(t, "stats-rounds.txt", FormatStatsRounds(fixtureStats(t)))
}

func TestGoldenStatsTools(t *testing.T) {
	compareGolden(t, "stats-tools.txt", FormatStatsTools(fixtureStats(t)))
}

func TestGoldenStatsTraffic(t *testing.T) {
	compareGolden(t, "stats-traffic.txt", FormatStatsTraffic(fixtureStats(t)))
}

// TestGoldenStatsCost pins the --cost money box on the real fixture session:
// deepseek-v4-flash at its actual session_start (2026-09-04 20:40 +08:00, DS
// off-peak) prices the CANONICAL 9 rounds (the session's own `request` rows —
// the raw log's 5-round slice is not the round source any more) to 0.0170 元 =
// hit 0.0043 (86K @0.05) + miss 0.0070 (5K @1.5) + out 0.0057 (1K @4.5). The
// golden locks the exact rendered frame + money math so a price-table or
// formatting change fails here first.
func TestGoldenStatsCost(t *testing.T) {
	st := fixtureStats(t)
	st.Started = time.Date(2026, 9, 4, 20, 40, 46, 0, pricing.Beijing())
	out, err := FormatStatsCost(st)
	if err != nil {
		t.Fatalf("FormatStatsCost: %v", err)
	}
	compareGolden(t, "stats-cost.txt", out)
}

// TestGoldenStatsBill pins the per-round money curve (and the per-tool money
// ranking) on the same fixture: every canonical round with a tool call is
// attributed and the ranking sums back to the box total 0.0170.
func TestGoldenStatsBill(t *testing.T) {
	st := fixtureStats(t)
	st.Started = time.Date(2026, 9, 4, 20, 40, 46, 0, pricing.Beijing())
	out, err := FormatStatsBill(st)
	if err != nil {
		t.Fatalf("FormatStatsBill: %v", err)
	}
	if tools, err := FormatStatsToolCosts(st); err != nil {
		t.Fatalf("FormatStatsToolCosts: %v", err)
	} else {
		out += tools
	}
	compareGolden(t, "stats-bill.txt", out)
}

// TestToolCostsSumBack: a per-tool money ranking is honest only when it sums
// back to the priced total (or is verifiably short — a residual "(no tool
// call)" row). Equal-split per round must never lose or invent a 元.
func TestToolCostsSumBack(t *testing.T) {
	st := fixtureStats(t)
	st.Started = time.Date(2026, 9, 4, 20, 40, 46, 0, pricing.Beijing())
	if !st.ComputeCost(st.Started) {
		t.Fatal("fixture session not priceable")
	}
	tc := st.ToolCosts()
	if len(tc) == 0 {
		t.Fatal("fixture session has no tool rows")
	}
	var sum float64
	for _, row := range tc {
		sum += row.Yuan
	}
	// Rows are equal-split shares; each round's share sums exactly back, so
	// the drift is bounded by per-row money4 rounding — a 0.0002 hair.
	if d := sum - st.Cost.Total; d > 0.0002 || d < -0.0002 {
		t.Errorf("ToolCosts sum %.4f != box total %.4f (drift %.4f)", sum, st.Cost.Total, d)
	}
	// Every ranked tool must carry at least one call.
	for _, row := range tc {
		if row.Name == "(no tool call)" {
			continue
		}
		if row.Calls < 1 {
			t.Errorf("tool %q ranked with 0 calls", row.Name)
		}
	}
}

func TestGoldenStatsJSON(t *testing.T) {
	out, err := FormatStatsJSON(fixtureStats(t))
	if err != nil {
		t.Fatalf("FormatStatsJSON: %v", err)
	}
	compareGolden(t, "stats-json.json", out)
}

// TestGoldenStatsJSONLFallback covers the no-raw-log path against the same
// real session: the same canonical round numbers off the session's own
// `request` rows, with no wire bodies attached (raw HTTP logging was off), so
// the source label says "(no raw log)".
func TestGoldenStatsJSONLFallback(t *testing.T) {
	sess, err := ParseFile(fixturePath(t, filepath.Join("sessions", fixtureSessionID+".jsonl")))
	if err != nil {
		t.Fatalf("ParseFile(fixture session): %v", err)
	}
	st := BuildStatsFromJSONL(sess)
	st.SourceDetail = fixtureSessionID + ".jsonl"
	compareGolden(t, "stats-overview-jsonl.txt", FormatStatsOverview(st))
	compareGolden(t, "stats-rounds-jsonl.txt", FormatStatsRounds(st))
}

// TestGoldenRawRoundIndex covers `gopi session raw <id> --rounds`.
func TestGoldenRawRoundIndex(t *testing.T) {
	rounds, err := ParseHTTPRounds(fixturePath(t, filepath.Join("raw", fixtureRawLogName)))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	compareGolden(t, "raw-rounds-index.txt", FormatRawRoundIndex(rounds, fixtureRawLogName))
}

// TestGoldenRawRequestIsByteExact is the verbatim guarantee: the request body
// printed for round N must be the exact bytes logged after round N's marker —
// compared against the fixture file itself, not against a snapshot.
func TestGoldenRawRequestIsByteExact(t *testing.T) {
	path := fixturePath(t, filepath.Join("raw", fixtureRawLogName))
	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 5 {
		t.Fatalf("fixture rounds = %d, want 5 (the fixture is cut on the round boundary)", len(rounds))
	}
	for _, want := range []int{1, 3, 5} {
		r := &rounds[want-1]
		body, ok := RawRoundRequest(r)
		if !ok {
			t.Fatalf("round %d has no request body", want)
		}
		line := readRawLine(t, path, r.LineStart+1) // the line after the marker
		if body != line {
			t.Errorf("round %d request body is not the logged bytes\n got: %.80q\nwant: %.80q", want, body, line)
		}
	}
}

// TestGoldenRawResponseSpanIsByteExact: the response side of round N is the
// exact line span RespLineStart..LineEnd, markers included.
func TestGoldenRawResponseSpanIsByteExact(t *testing.T) {
	path := fixturePath(t, filepath.Join("raw", fixtureRawLogName))
	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	r := &rounds[2] // round 3
	first, last, ok := RawRoundResponseSpan(r)
	if !ok {
		t.Fatal("round 3 has no response span")
	}
	lines, truncated, err := RawLogLineSpan(path, first, last)
	if err != nil {
		t.Fatalf("RawLogLineSpan: %v", err)
	}
	if truncated {
		t.Fatal("the fixture must not hit the raw view cap")
	}
	if len(lines) != last-first+1 {
		t.Fatalf("round 3 response lines = %d, want %d", len(lines), last-first+1)
	}
	for i, l := range lines {
		if want := readRawLine(t, path, first+i); l != want {
			t.Errorf("response line %d is not the logged bytes\n got: %.60q\nwant: %.60q", i+1, l, want)
		}
	}
	if lines[0] != markerChunkPrefix {
		t.Errorf("round 3 response span must start with the ← CHUNK marker, got %.20q", lines[0])
	}
}

// readRawLine returns raw-log line n (1-based) verbatim, through the SAME dual-
// format line view the production readers use — so a fixture in either framing is
// readable here (before, this helper opened the file and scanned lines itself,
// which silently assumed the legacy framing).
func readRawLine(t *testing.T, path string, n int) string {
	t.Helper()
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		t.Fatalf("detect %s: %v", path, err)
	}
	sc, err := rawlogfmt.OpenLineScanner(path, format)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo == n {
			return sc.Text()
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	t.Fatalf("raw log %s has no line %d", path, n)
	return ""
}
