package tools

// A RESUMED RUN'S SCALE BLOCK (epic: the silent zero).
//
// A run log that `gopi session resume` continued carries MORE THAN ONE
// `session_end`: the killed run's own verdict, then the resumed segment's. The
// tail read (internal/rundir) is last-writer-wins, so the record's Turns was
// the LAST segment's count — and the resumed segment routinely reports 0 loop
// passes of its own, which made `if r.Turns > 0` in taskRunLine false and erased
// the whole pass block from the row's `run:` line, silently.
//
// This fixture is the REAL shape, measured 2026-10-27 on session
// 4a0957a3-bb4e-4337-a761-20606c87d1fd (641754 bytes, two segment starts, two
// segment ends, total_iterations [29, 0]):
//
//	{"type":"session_start", ...}                                  segment 1
//	{"type":"request","iteration":1}                               …
//	{"type":"session_end","total_iterations":29,"total_tokens":24714762}   ← the run's real pass count
//	{"type":"session_start", ...}                                  segment 2 (the resume)
//	{"type":"session_end","total_iterations":0,"total_tokens":0}    ← last-writer-wins erased 29
//
// The fix is NOT to add the two (a tail window cannot see the segments outside
// it, so adding is a new silent lie). The record must SAY that the tail holds
// several segments, and the renderer must print a dash with the reason instead
// of dropping the field (PROJECT.md Rule 15).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/rundir"
	"github.com/nick/gopi/internal/sessionevent"
)

// taskRunDigitSecondsRe finds the elapsed rendering ("12s"): a digit run
// followed by the seconds suffix at a word boundary. It exists so "segments"
// — a word that merely ENDS in s — cannot be mistaken for an elapsed time.
var taskRunDigitSecondsRe = regexp.MustCompile(`\d` + regexp.QuoteMeta(runElapsedSuffix) + `\b`)

const (
	// twoSegTaskID is the fixture's row id (it names the run directory).
	twoSegTaskID = "t-two-segment-run"
	// twoSegRunID is the fixture's run id (the log's file name).
	twoSegRunID = "cccc3333-0000-4000-8000-000000000003"
	// twoSegFirstTurns / twoSegLastTurns are the two segments' own
	// `total_iterations` — the measured pair (29 then 0).
	twoSegFirstTurns = 29
	twoSegLastTurns  = 0
	// twoSegStart is the fixture's session_start timestamp.
	twoSegStart = "2026-10-27T08:01:32.02276172+08:00"
)

// twoSegFixture writes one run log under the row's run directory with two
// segment pairs and returns the record internal/rundir reads off it.
func twoSegFixture(t *testing.T) rundir.Record {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, twoSegTaskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	start := `{"type":"session_start","sessionId":"` + twoSegRunID + `","timestamp":"` + twoSegStart + `"}`
	body := start + "\n" +
		`{"type":"request","iteration":1}` + "\n" +
		`{"type":"session_end","elapsed_sec":4549.425534492,"exit_code":1,"total_iterations":` +
		strconv.Itoa(twoSegFirstTurns) + `,"total_tokens":24714762,"timestamp":"` + twoSegStart + `"}` + "\n" +
		start + "\n" +
		`{"type":"session_end","elapsed_sec":0,"exit_code":0,"total_iterations":` +
		strconv.Itoa(twoSegLastTurns) + `,"total_tokens":0,"timestamp":"` + twoSegStart + `"}` + "\n"
	path := filepath.Join(dir, twoSegRunID+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write run log: %v", err)
	}
	recs := rundir.RecordsDir(dir)
	if len(recs) != 1 {
		t.Fatalf("run count = %d, want 1 (one log file, however many segments it holds)", len(recs))
	}
	return recs[0]
}

// TestRunLinePrintsADashWhenTheLogHoldsTwoSegmentEnds is THE repro: the run
// line must not silently omit the pass block, and it must not print the last
// segment's 0 as if it were the run's count.
func TestRunLinePrintsADashWhenTheLogHoldsTwoSegmentEnds(t *testing.T) {
	rec := twoSegFixture(t)
	line := taskRunLine(rec, false)
	t.Logf("run line under test: %q", line)

	if !rec.HasEnd {
		t.Fatalf("HasEnd = false — the fixture ends with a session_end")
	}
	// (1) The absence is STATED: a dash plus the reason names how many
	// segments the tail held.
	if !strings.Contains(line, taskRunPassNoTotalMarker) {
		t.Fatalf("the run line silently dropped the pass block:\n  got %q\n"+
			"a resumed log's tail holds %d session_end segments, so no window number is the run's total — "+
			"it must render a dash and say why (Rule 15)", line, 2)
	}
	if !strings.Contains(line, fmt.Sprintf(taskRunPassSegmentedFmt, 2)) {
		t.Fatalf("the dash must name the segment count (%d segments):\n  got %q", 2, line)
	}
	// (2) The last segment's 0 is never printed as the run's pass count.
	if strings.Contains(line, " "+strconv.Itoa(twoSegLastTurns)+" "+sessionevent.CounterUnitLoopPass) {
		t.Fatalf("the last segment's count was printed as the run's pass total:\n  got %q", line)
	}
	// (3) Its own scarce numbers (tokens, elapsed) are not silently printed as
	// the run's either — they came from the same last-writer-wins overwrite.
	if strings.Contains(line, " "+taskRunTokenUnit) {
		t.Fatalf("the untrusted scale block still printed the last segment's token count:\n  got %q", line)
	}
	if taskRunDigitSecondsRe.MatchString(line) {
		t.Fatalf("the untrusted scale block still printed the last segment's elapsed time:\n  got %q", line)
	}
}

// TestRunLineStillPrintsATrustedPassCount pins the two shapes the fix must NOT
// disturb: a single-segment log prints its count, and a real 0 prints as 0 (a
// measurement) rather than being dropped by `turns > 0`.
func TestRunLineStillPrintsATrustedPassCount(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, twoSegTaskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	for _, tc := range []struct {
		name  string
		turns int
	}{{"single-segment", twoSegFirstTurns}, {"single-segment-zero", 0}} {
		path := filepath.Join(dir, tc.name+".jsonl")
		body := `{"type":"session_start","sessionId":"` + tc.name + `","timestamp":"` + twoSegStart + `"}` + "\n" +
			`{"type":"session_end","elapsed_sec":12.5,"exit_code":0,"total_iterations":` +
			strconv.Itoa(tc.turns) + `,"total_tokens":99}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		recs := rundir.RecordsDir(dir)
		var rec rundir.Record
		for _, r := range recs {
			if r.RunID == tc.name {
				rec = r
			}
		}
		if rec.Path == "" {
			t.Fatalf("%s: record not found in %+v", tc.name, recs)
		}
		line := taskRunLine(rec, false)
		want := strconv.Itoa(tc.turns) + " " + sessionevent.CounterUnitLoopPass
		if !strings.Contains(line, want) {
			t.Errorf("%s: run line = %q, want it to carry %q", tc.name, line, want)
		}
		if strings.Contains(line, taskRunPassNoTotalMarker) {
			t.Errorf("%s: a single-segment log must not render the multi-segment dash: %q", tc.name, line)
		}
	}
}

// TestTwoSegmentLogIsNotTotalledByAddition is the anti-regression on the fix's
// DIRECTION: the record must not report a SUMMED count either (a tail window
// cannot see the segments outside it, so 29+0 is not "the run's total" — it is
// a number that happens to look right on this fixture).
func TestTwoSegmentLogIsNotTotalledByAddition(t *testing.T) {
	rec := twoSegFixture(t)
	if rec.Turns == twoSegFirstTurns+twoSegLastTurns && rec.Turns != twoSegLastTurns {
		t.Fatalf("Turns = %d — the tail window's segments were ADDED; a window read cannot total a run", rec.Turns)
	}
	if rec.ScaleTrusted {
		t.Fatalf("TurnsTrusted = true for a log with %d session_end segments", rec.EndSegments)
	}
	if rec.EndSegments != 2 {
		t.Fatalf("EndSegments = %d, want 2 (the tail holds both segments' verdicts)", rec.EndSegments)
	}
	if rec.Turns != twoSegLastTurns {
		t.Fatalf("Turns = %d, want the LAST segment's own count %d (the record states WHERE the number came from)",
			rec.Turns, twoSegLastTurns)
	}
}
