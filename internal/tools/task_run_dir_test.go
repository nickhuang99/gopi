package tools

// PLURAL-RUN RESOLUTION (epic run-identity-unify, 2026-09-20).
//
// The defect these tests pin: a row's run directory is 1:N (one task, several
// SERIAL runs — a killed run plus its resume/retry) while tasks.session_id is a
// 1:1 column, and the resume path used to resolve its checkpoint THROUGH that
// column. Three consequences, each measured on the operator's live ledger in
// the epic's evidence:
//
//   - it names only the FIRST run of the chain, which is the run a recovery
//     does not want;
//   - it is frequently the wrong run entirely (its documented meaning is "the
//     session that submitted/executed the row", and one worker session
//     executes many rows — measured: row 01a0b47e's column names 69fd4bba…,
//     which lives in NO directory of that row);
//   - reading it cost a whole-tree search for a path the row's own task id
//     already names.
//
// These tests are HERMETIC: the fixture is the REAL shape of the one multi-run
// directory on this host (sessions/t-1788773479: a long finished run with a
// recorded raw log, plus a later 2-turn run whose checkpoint is gone) with the
// ids rewritten, so the assertion is about the resolution RULE and not about
// one machine's history.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// The fixture's two runs and the value `tasks.session_id` would carry for it.
const (
	pluralRunTaskID = "t-plural-run"

	// pluralRowSessionID is the FIRST (older, short) run — the value the
	// ledger column names. It has NO raw-log checkpoint on disk: the run died
	// before recording one (the real host shape — row t-1788773479's second
	// run is a 2-turn log whose checkpoint is gone).
	pluralRowSessionID = "aaaa1111-0000-4000-8000-000000000001"

	// pluralOtherRunID is the SECOND (later, long) run — the one a recovery
	// must continue, and the one that exists in the run directory and in
	// NOTHING else: no ledger column names it. The epic's evidence found this
	// exact shape live (row 01a0b47e's column names 69fd4bba…, which lives in
	// NO directory of that row), so the old resolution could not reach the run
	// at all.
	pluralOtherRunID = "bbbb2222-0000-4000-8000-000000000002"
)

// writeRunLog writes one run log under dir with the given session_start facts
// and, when turns > 0, progress events. ended adds the terminal session_end
// line. It is the fixture's only writer, so every test here describes one
// on-disk shape.
func writeRunLog(t *testing.T, dir, runID, rawLogPath, started string, turns int, ended bool) string {
	t.Helper()
	path := filepath.Join(dir, runID+".jsonl")
	body := `{"type":"session_start","sessionId":"` + runID + `","taskId":"` + filepath.Base(dir) +
		`","model":"deepseek-flash","timestamp":"` + started + `"`
	if rawLogPath != "" {
		body += `,"rawLogPath":"` + rawLogPath + `"`
	}
	body += "}\n"
	if turns > 0 {
		// Every run records a request event per round as it progresses — an
		// unfinished run's turn count comes from the LAST one, which is why
		// the fixture writes it in both cases.
		body += `{"type":"request","iteration":` + strconv.Itoa(turns) + "}\n"
	}
	if ended {
		body += `{"type":"session_end","elapsed_sec":123.5,"exit_code":0,"total_iterations":` +
			strconv.Itoa(turns) + `,"total_tokens":4242,"timestamp":"` + started + `"}` + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write run log %s: %v", path, err)
	}
	return path
}

// pluralRunFixture builds the sessions tree the defect is about:
//
//	<gopi>/.gopi/sessions/<task_id>/<first-run>.jsonl     short, no checkpoint
//	<gopi>/.gopi/sessions/<task_id>/<other-run>.jsonl     long, checkpoint recorded
//	<gopi>/.gopi/sessions/<task_id>/<other-run>.raw.log   the checkpoint itself
//	<gopi>/.gopi/sessions/<task_id>/signal-provenance.jsonl  a side file (NOT a run)
//
// and returns the task id plus the resume target's raw-log path.
func pluralRunFixture(t *testing.T) (taskID, wantRawLog string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	// config.GopiDir() is $GOPI_HOME/.gopi, so the run directory is
	// <home>/.gopi/sessions/<task_id> — the same address
	// internal/agent/sessionDirForID mints from a task id.
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, pluralRunTaskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	// The FIRST run (the one the ledger column would name): short, UNFINISHED,
	// and it kept no checkpoint.
	writeRunLog(t, dir, pluralRowSessionID, "", "2026-09-09T04:33:30.88765579+08:00", 2, false)
	// The LATER run: long, UNFINISHED (killed), with its raw log recorded —
	// the run a resume must continue.
	wantRawLog = filepath.Join(dir, pluralOtherRunID+".raw.log")
	if err := os.WriteFile(wantRawLog, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	writeRunLog(t, dir, pluralOtherRunID, wantRawLog, "2026-09-09T04:37:48.626336812+08:00", 68, false)
	// A side file of the SAME runs: internal/sessionlayout classifies it, and a
	// run enumeration that ignored that classification would report 3 runs.
	if err := os.WriteFile(filepath.Join(dir, "signal-provenance.jsonl"),
		[]byte(`{"ts":"2026-09-09T04:40:00+08:00","kind":"signal","signal":"SIGTERM"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write side file: %v", err)
	}
	return pluralRunTaskID, wantRawLog
}

// TestRunDirResolution_PluralRunRowResolvesTheRunThatShouldBeResumed is THE
// test of this epic's resolution change.
//
// THE ROW-KEYED RESOLUTION (shipped): the row's run DIRECTORY is enumerated and
// the newest run that still owns a raw-log checkpoint is the resume target —
// here the LATER run, which the ledger's columns do not name at all. The walk is
// O(runs in ONE directory the row's own id names): no session_id column, no
// session tree search.
//
// THE OLD RESOLUTION (what this replaced; the chain is still reachable in-code
// as the legacy fallback, and TestRunDirResolution_OldChainCannotReachTheRun
// pins its answer): `TaskResumeTarget` read `st.SessionID` — the 1:1 column —
// and resolved THAT session's raw log, so for this fixture (whose column names
// the FIRST run, which kept no checkpoint) it answers "" and the row cannot be
// resumed at all, while the run that should be continued sits in the row's own
// directory.
func TestRunDirResolution_PluralRunRowResolvesTheRunThatShouldBeResumed(t *testing.T) {
	taskID, wantRawLog := pluralRunFixture(t)

	recs := taskRuns(taskID)
	if len(recs) != 2 {
		t.Fatalf("run count = %d, want 2 (the side file must not count as a run): %+v", len(recs), recs)
	}
	if got := recs[0].RunID; got != pluralRowSessionID {
		t.Errorf("oldest run = %q, want %q", got, pluralRowSessionID)
	}
	if got := recs[1].RunID; got != pluralOtherRunID {
		t.Errorf("newest run = %q, want %q", got, pluralOtherRunID)
	}
	// The row-keyed resolution: the run with the checkpoint.
	if got := taskRunResumeRawLog(taskID); got != wantRawLog {
		t.Errorf("row-keyed resume target = %q, want %q", got, wantRawLog)
	}
	// Terminal facts come off the run logs themselves.
	if recs[0].HasEnd || recs[1].HasEnd {
		t.Errorf("both fixture runs are unfinished; HasEnd must stay false: %+v", recs)
	}
	if recs[1].Turns != 68 {
		t.Errorf("unfinished run must still report its progress: turns=%d, want 68", recs[1].Turns)
	}
}

// TestRunDirResolution_OldChainCannotReachTheRun is the RED proof, kept as a
// permanent assertion: it reproduces the LEGACY session-keyed chain verbatim
// (the one `TaskResumeTarget` ran before this epic — row session_id →
// taskRawLogPathFromSession → taskFindSessionJSONL) against the same fixture and
// asserts it CANNOT answer. Under the old resolution this is the row's whole
// recovery story, so the assertion documents the defect the new path removes:
// the answer is "" — no checkpoint, no resume — while the row's own directory
// holds the run that should be continued.
func TestRunDirResolution_OldChainCannotReachTheRun(t *testing.T) {
	taskID, wantRawLog := pluralRunFixture(t)
	old := taskRawLogPathFromSession(pluralRowSessionID)
	newWay := taskRunResumeRawLog(taskID)
	t.Logf("RED PROOF — same row, same fixture, two resolutions:")
	t.Logf("  OLD (session_id column -> taskRawLogPathFromSession) = %q", old)
	t.Logf("  NEW (row id -> sessions/<task_id>/*.jsonl)            = %q", newWay)
	if old != "" {
		t.Fatalf("legacy chain = %q, want empty for a column naming a run with no checkpoint", old)
	}
	if newWay != wantRawLog {
		t.Fatalf("row-keyed chain = %q, want %q — the two resolutions must differ on this fixture, "+
			"which is what makes the new one an improvement", newWay, wantRawLog)
	}
}

// TestRunDirResolution_DoesNotReadTheLedger is the structural half: the
// resolution takes ONLY the run id, so it answers with no ledger row and no
// session-id argument — under the old chain both were required first steps.
func TestRunDirResolution_DoesNotReadTheLedger(t *testing.T) {
	taskID, oldRawLog := pluralRunFixture(t)
	if got := taskRunResumeRawLog(taskID); got != oldRawLog {
		t.Fatalf("resolution without a ledger row = %q, want %q (the row's session_id read was the old chain's first step)",
			got, oldRawLog)
	}
}

// TestRunDirResolution_EmptySessionIDRowStillResumes is the legacy-state guard
// the epic's acceptance names: 672 rows on this host carry no session_id. Such
// a row resolves from its OWN id — an empty column can never be read as "this
// row has no run". The rendered row makes the same claim: the run block answers
// and no session_id line appears at all for an empty column.
func TestRunDirResolution_EmptySessionIDRowStillResumes(t *testing.T) {
	taskID, wantRawLog := pluralRunFixture(t)
	// The row exists, and its ledger column is EMPTY — the state 672 live rows
	// are in.
	if _, err := TaskNew("plural run row", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if got := taskRunResumeRawLog(taskID); got != wantRawLog {
		t.Fatalf("empty-session_id row resolve = %q, want %q", got, wantRawLog)
	}
	if !dirExists(taskRunDir(taskID)) {
		t.Fatalf("fixture run directory missing: %s", taskRunDir(taskID))
	}
	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, TaskShowRunDirLabel) || !strings.Contains(out, pluralOtherRunID) {
		t.Errorf("empty-session_id row must still render its run block:\n%s", out)
	}
	// The ledger column renders as an EMPTY value, never a vanished line — and
	// the line under it states the column's role, so an empty value cannot be
	// read as "this row has no run".
	if !strings.Contains(out, "session_id:     \n") {
		t.Errorf("an empty ledger column must render an explicit empty value:\n%s", out)
	}
}

// TestRunDirResolution_RunBlockRendersRunsAndDirectory is the `task show` half:
// the block names the run DIRECTORY and every run in it, marks the resume
// target, and prints no bare uuid line.
func TestRunDirResolution_RunBlockRendersRunsAndDirectory(t *testing.T) {
	taskID, _ := pluralRunFixture(t)
	joined := strings.Join(taskRunLines(taskID, pluralRowSessionID), "\n")
	for _, want := range []string{
		TaskShowRunDirLabel,
		filepath.Join(config.ProfileDirName, config.SessionsDirName, pluralRunTaskID),
		TaskShowRunLogLabel,
		TaskShowRunsLabel,
		"2 run(s)",
		"1 with a raw-log checkpoint",
		pluralRowSessionID,
		pluralOtherRunID,
		runStatusUnfinished,
		taskRunResumeMark,
		// THE RUN LINE NAMES THE COUNT IT CARRIES. rundir's Turns field is
		// session_end's total_iterations — the LOOP PASS count, not operator
		// rows — so the line says "loop passes". Printing it as "turns" made
		// this progress line (31) disagree with `gopi session log <id> --turns`
		// (16) on the same session: one word, two quantities (Rule 14; measured
		// 2026-10-16 on session 5c6cd329).
		"68 " + sessionevent.CounterUnitLoopPass,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("run block missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "68 turns") {
		t.Errorf("the run line prints the loop's own count as \"turns\" — that word belongs to the operator's rows:\n%s", joined)
	}
	if strings.Contains(joined, "session_id:") {
		t.Errorf("run block must not render a bare session_id line:\n%s", joined)
	}
}

// TestRunDirResolution_LegacySessionKeyedFallbackStillAnswers pins the retained
// fallback: a row whose run directory holds NO run log keeps the behaviour the
// build had before this epic — taskFindSessionJSONL still finds a session's log
// by session id across the layouts. Without this, the change would silently
// strand the t- rows whose logs live outside their own directory.
func TestRunDirResolution_LegacySessionKeyedFallbackStillAnswers(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A legacy FLAT record (sessions/<uuid>.jsonl) — a layout with no directory
	// of its own, so only the session-keyed scan can find it.
	legacy := filepath.Join(dir, "legacy-sess.jsonl")
	if err := os.WriteFile(legacy,
		[]byte(`{"type":"session_start","sessionId":"legacy-sess","model":"m","timestamp":"2026-01-01T00:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy log: %v", err)
	}
	if got := taskFindSessionJSONL(dir, "legacy-sess"); got != legacy {
		t.Errorf("legacy session-keyed fallback = %q, want %q", got, legacy)
	}
	// The row-keyed path correctly reports a definite absence for a row that
	// never ran: "" — never a guess at some other row's run.
	if got := taskRunResumeRawLog("t-never-ran"); got != "" {
		t.Errorf("row with no run directory = %q, want empty (a definite absence, never a guess)", got)
	}
}

// dirExists is the fixture's directory probe.
func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
