package main

// cli_session_segments_test.go — `session show` ON A RESUMED SESSION.
//
// THE DEFECT (task 01a189b4): a chat resumed three times printed the FIRST
// segment's ending and the FIRST segment's pid, because
//   - `chats.ended_at` is stamped by ChatCloseSession under
//     `WHERE ended_at IS NULL` (idempotent on purpose: one conversation ends
//     once), so a later segment's close never updates it, and
//   - the run record was built from the log's FIRST line only, so its liveness
//     stamp was the first segment's process.
//
// MEASURED ON A REAL SESSION before the fix (66243525-16ab-4ce7-9133-84d74c539840,
// 21398592 bytes, three segments): the page printed
//
//	ended       2026-10-27 21:55:30   <- the FIRST segment's verdict
//	pid         2434502              <- the FIRST segment's process
//
// while the log's last verdict sits at 2026-10-30 05:32:41 under pid 3954507.
// The last `session_start` is 18601354 bytes from the end of that file, which is
// why no bounded tail window could see it: the fix reads the WHOLE log for this
// one-row question (rundir.ShowRecord) and the page STATES the segment count.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// segTestSessionID names the three-segment fixture session.
	segTestSessionID = "cli-segments-11111111-2222"
	// The three segments' stamps: distinct pids (they are three processes) and
	// distinct verdict instants (they are three endings), so a page that printed
	// any other segment than the last fails on the VALUE, not on a guess.
	segPID1, segPID2, segPID3 = 4100001, 4100002, 4100003
	// segPass1/2/3 are the per-segment loop-pass counts, the same 29-then-0
	// shape a resumed chat really records.
	segPass1, segPass2, segPass3 = 29, 0, 0
)

// segStamp formats one segment instant the way the log writes it (RFC3339Nano)
// and returns the string plus its Unix seconds (what the page renders).
func segStamp(t *testing.T, base time.Time, add time.Duration) (string, int64) {
	t.Helper()
	at := base.Add(add)
	return at.Format(time.RFC3339Nano), at.Unix()
}

// writeThreeSegmentSessionLog writes sessions/<id>/session.jsonl holding three
// run segments — the bare layout the resolver probes first — and returns its
// path plus the LAST verdict's unix seconds and the LAST header's pid.
func writeThreeSegmentSessionLog(t *testing.T, sessionID string) (path string, lastEndUnix int64, lastPID int) {
	t.Helper()
	home := os.Getenv("GOPI_HOME")
	if home == "" {
		t.Fatal("GOPI_HOME is not pinned — call sessionChatEnv first")
	}
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	base := time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC)
	type segment struct {
		pid  int
		pass int
	}
	segments := []segment{{segPID1, segPass1}, {segPID2, segPass2}, {segPID3, segPass3}}
	var b strings.Builder
	for i, s := range segments {
		start, _ := segStamp(t, base, time.Duration(i)*24*time.Hour)
		// A resume's header is the SAME identity with the witness key set; the
		// first segment's header has none, exactly as internal/agent writes it.
		resumedKey := ""
		if i > 0 {
			resumedKey = `,"resumedFrom":"` + sessionID + `"`
		}
		b.WriteString(`{"type":"session_start","sessionId":"` + sessionID +
			`","timestamp":"` + start + `","model":"deepseek-flash","pid":` +
			strconv.Itoa(s.pid) + resumedKey + `}` + "\n")
		b.WriteString(`{"type":"request","iteration":1}` + "\n")
		end, endUnix := segStamp(t, base, time.Duration(i)*24*time.Hour+time.Hour)
		b.WriteString(`{"type":"session_end","elapsed_sec":3600,"exit_code":0,` +
			`"reason":"interrupt","timestamp":"` + end + `","total_iterations":` +
			strconv.Itoa(s.pass) + `,"total_tokens":100}` + "\n")
		if i == len(segments)-1 {
			lastEndUnix = endUnix
		}
	}
	path = filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path, lastEndUnix, segPID3
}

// TestSessionShowReportsTheLastSegmentOfAResumedSession is the epic's
// regression test: a session holding THREE start/end pairs must render the LAST
// verdict's instant and the LAST header's pid — and must SAY there are three
// segments, because a reader who is not told the span cannot tell which
// segment's reading the page is showing (Rule 15).
func TestSessionShowReportsTheLastSegmentOfAResumedSession(t *testing.T) {
	sessionChatEnv(t)
	logPath, wantEndUnix, wantPID := writeThreeSegmentSessionLog(t, segTestSessionID)
	sessionSeedRow(t, segTestSessionID, logPath)

	// THE CHAT ROW'S OWN ENDING IS THE FIRST ONE — that is the idempotent close
	// hook's contract (ChatCloseSession keeps the first ending), and the fixture
	// must carry it or the test would not exercise the disagreement it is about.
	if err := tools.ChatCloseSession(segTestSessionID, "interrupt"); err != nil {
		t.Fatalf("close chat row: %v", err)
	}

	page := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{segTestSessionID}) })

	wantEnded := time.Unix(wantEndUnix, 0).Format("2006-01-02 15:04:05")
	if !strings.Contains(page, "  ended       "+wantEnded+"\n") {
		t.Errorf("`session show` does not report the LAST segment's ended (%s):\n%s", wantEnded, page)
	}
	if !strings.Contains(page, "  pid         "+strconv.Itoa(wantPID)+"\n") {
		t.Errorf("`session show` does not report the LAST segment's pid (%d):\n%s", wantPID, page)
	}
	// The span must be STATED, not left to be guessed.
	if !strings.Contains(page, "  segments    3") {
		t.Errorf("the page does not disclose that the log holds three run segments:\n%s", page)
	}
	// The row's own (first) ending stays visible under its own label: two true
	// statements about one session, each named (Rule 15).
	if !strings.Contains(page, "  first ended ") {
		t.Errorf("the page dropped the chat row's own ending without saying so:\n%s", page)
	}
}

// TestSessionShowJSONCarriesTheLastSegment is the machine face of the same
// fact: `--json` must publish the recorded ending and the last segment's pid,
// and must disclose the span — a script must not be handed the stale row value
// the human page no longer shows (Rule 13: one decision, two renderings).
func TestSessionShowJSONCarriesTheLastSegment(t *testing.T) {
	sessionChatEnv(t)
	logPath, wantEndUnix, wantPID := writeThreeSegmentSessionLog(t, segTestSessionID+"-json")
	sessionSeedRow(t, segTestSessionID+"-json", logPath)
	if err := tools.ChatCloseSession(segTestSessionID+"-json", "interrupt"); err != nil {
		t.Fatalf("close chat row: %v", err)
	}

	out := captureStdoutAndStderr(t, func() int {
		return runSessionShow([]string{segTestSessionID + "-json", sessionJSONFlag})
	})
	for _, want := range []string{
		`"ended_at": ` + strconv.FormatInt(wantEndUnix, 10),
		`"pid": ` + strconv.Itoa(wantPID),
		`"segments": 3`,
		`"segments_scanned": true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`session show --json` is missing %s:\n%s", want, out)
		}
	}
}

// TestSessionShowSaysWhenTheLogWasNotScanned pins the other half of the
// disclosure: a row whose log is absent (or unreadable) must render the segment
// count as UNKNOWN, never as the ordinary one — "one segment" and "nobody
// looked" must not look alike (Rule 15).
func TestSessionShowSaysWhenTheLogWasNotScanned(t *testing.T) {
	sessionChatEnv(t)
	const absentID = "cli-segments-absent-3333"
	sessionSeedRow(t, absentID, filepath.Join(t.TempDir(), "gone.jsonl"))
	if err := tools.ChatCloseSession(absentID, "normal"); err != nil {
		t.Fatalf("close chat row: %v", err)
	}
	page := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{absentID}) })
	if !strings.Contains(page, "  segments    "+sessionSegmentsUnscanned) {
		t.Errorf("a row whose log is not on this machine does not say the log was unscanned:\n%s", page)
	}
	if strings.Contains(page, "  segments    1") {
		t.Errorf("an unscanned log is presented as a single-segment one:\n%s", page)
	}
}
