package main

// Regression (2026-11-05): `gopi worker --resume <task-id>` answered ONE
// sentence for TWO opposite situations — a row no run of which ever started, and
// a row that ran whose checkpoint is gone. The refusal is required to say which
// one it measured, because the two have different ways out (a NEW row with a new
// UUID vs a fresh attempt of the same work), and because "never started" is
// exactly the state in which a resume must NOT be reinterpreted as "start over"
// (the 2026-10-28 incident: an unstarted row's resume adopted the submitter
// chat's checkpoint and closed the row out from that other conversation).
//
// THE THREE ARMS, one per state the filesystem can be in:
//
//	(a) the row's run directory does not exist / holds neither a run log nor a
//	    raw log  →  verdict never-started, and the refusal names the new-UUID way
//	    out instead of a resume;
//	(b) the row's run directory holds a run LOG whose raw log was deleted
//	    →  verdict ran-no-checkpoint, still refused, and the refusal quotes the
//	    run log it measured (the semantics this path already had);
//	(c) a session id that really ran  →  BYTE-FOR-BYTE unchanged: it resumes.
//
// RED WITHOUT THE NEW PREDICATE: arm (a) then falls into the shared
// rawness-one-sentence error, so its assertion on the verdict word and on the
// new-UUID remedy fails while arm (b) still passes — which is the point of
// measuring the verdict instead of "an error happened".
//
// Fixtures: the sibling resume tests' own helpers (resumeLatchSandbox pins a
// private profile through testutil, seedRowIntoProfile writes a REAL ledger row
// through tools.TaskSubmit), so no test here hand-rolls a database path (Rule 32)
// and no arm talks to a real provider (the mock LLM answers).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

const (
	// resumeNoCheckpointFixtureRow is the row both refusal arms file: the id is
	// a task id (never a uuid), because the whole question is what a ROW with no
	// checkpoint answers.
	resumeNoCheckpointFixtureRow = "t-resume-no-checkpoint"

	// resumeNoCheckpointRanUUID is the run id inside the row's directory for arm
	// (b): a run log exists under this name and the raw log it recorded is gone.
	resumeNoCheckpointRanUUID = "7e7e7e7e-1111-2222-3333-444444444444"

	// resumeNoCheckpointLiveRow / resumeNoCheckpointLiveRun are arm (c)'s row and
	// the session that really ran for it — the unchanged-behaviour control.
	resumeNoCheckpointLiveRow = "t-resume-no-checkpoint-live"
)

// seedNeverStartedRunDir makes the row's run directory the way an unstarted row
// leaves it: a claim happened (so the directory can exist) and NOTHING that
// records a run was written. An empty directory is the shape measured on this
// host for 1 582 sessions-tree entries; the row-with-no-directory-at-all shape
// (a row that was never even dispatched) is exercised by simply not seeding.
func seedNeverStartedRunDir(t *testing.T, root, runID string) {
	t.Helper()
	dir := filepath.Join(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //stylegate:ignore a fixture directory this test created, removed at its own end
}

// seedRunWhoseRawLogWasDeleted writes a run LOG that RECORDS a raw-log path
// beside it, and deletes the raw log — the "the row ran, the checkpoint was
// pruned" state. It is deliberately not seedCheckpointlessRunDir: that one
// records no rawLogPath at all, and a fixture whose record is silent cannot tell
// "the checkpoint is gone" from "the run never named one".
func seedRunWhoseRawLogWasDeleted(t *testing.T, root, runID string) {
	t.Helper()
	dir := filepath.Join(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //stylegate:ignore a fixture directory this test created, removed at its own end
	raw := filepath.Join(dir, resumeNoCheckpointRanUUID+scratch.SessionRawLogFileSuffix)
	rawJSON := `"` + raw + `"`
	lines := []string{
		`{"type":"session_start","sessionId":"` + resumeNoCheckpointRanUUID +
			`","model":"deepseek-v4-flash","rawLogPath":` + rawJSON + `,"timestamp":"2026-09-01T00:00:00Z"}`,
		`{"type":"session_end","elapsed_sec":1.0,"exit_code":0}`,
	}
	if err := os.WriteFile(filepath.Join(dir, resumeNoCheckpointRanUUID+".jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	// The file the record names is NOT there: the checkpoint was pruned. Stated
	// as an assertion (Rule 30's posture: measure the fixture, do not assume it).
	if _, err := os.Stat(raw); !os.IsNotExist(err) {
		t.Fatalf("the fixture must NOT have a raw log at %s (this arm is the LOST checkpoint); stat err=%v", raw, err)
	}
}

// clearRowRunDir removes any directory the row has under a sessions root, so an
// arm measures its own fixture rather than the suite's residue (the trap the
// sibling test documents: a killed `go test` leaves a run directory behind).
func clearRowRunDir(t *testing.T, root, runID string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, runID)); err != nil {
		t.Fatalf("clear the row's run directory under %s: %v", root, err)
	}
}

// TestResumeNoCheckpointVerdict is arm (a) + arm (b) at the resolver seam: the
// verdict, the evidence and the remedy, all measured on a real ledger row.
func TestResumeNoCheckpointVerdict(t *testing.T) {
	t.Run("a_never_started_row_names_that_verdict_and_a_new_row_as_the_way_out", func(t *testing.T) {
		gopiHome, _ := resumeLatchSandbox(t)
		seedRowIntoProfile(t, gopiHome, resumeNoCheckpointFixtureRow)
		root := processSessionsRoot(t)
		clearRowRunDir(t, root, resumeNoCheckpointFixtureRow)
		seedNeverStartedRunDir(t, root, resumeNoCheckpointFixtureRow)

		_, err := resolveResumeTarget(resumeNoCheckpointFixtureRow)
		if err == nil {
			t.Fatal("a row with no checkpoint must NOT resolve — a resume continues recorded context or does not run")
		}
		msg := err.Error()
		if !strings.Contains(msg, resumeNoCheckpointNeverStarted) {
			t.Fatalf("the refusal must state verdict %q for a row no run of which ever started, got:\n%s",
				resumeNoCheckpointNeverStarted, msg)
		}
		if strings.Contains(msg, resumeNoCheckpointRanNoCheckpoint) {
			t.Fatalf("the refusal must not also claim %q — the two verdicts are exclusive, got:\n%s",
				resumeNoCheckpointRanNoCheckpoint, msg)
		}
		// THE WAY OUT IS A NEW ROW WITH ITS OWN UUID (operator ruling #27273).
		// The old sentence offered `gopi task clone`, which mints `<id>-c%d` —
		// an id with a suffix, which is exactly what the ruling forbids.
		if strings.Contains(msg, "gopi task clone") {
			t.Errorf("the never-started refusal must NOT recommend `gopi task clone` (it mints <id>-c1, and ruling #27273 forbids suffix ids), got:\n%s", msg)
		}
		if !strings.Contains(msg, "gopi task submit") {
			t.Errorf("the never-started refusal must name the new-UUID way out (`gopi task submit`), got:\n%s", msg)
		}
		// The benchmark for the reader: the row itself, and what the directory holds.
		if !strings.Contains(msg, resumeNoCheckpointFixtureRow) {
			t.Errorf("the refusal must name the row, got:\n%s", msg)
		}
	})

	t.Run("b_a_row_that_ran_and_lost_its_checkpoint_keeps_its_own_verdict", func(t *testing.T) {
		gopiHome, _ := resumeLatchSandbox(t)
		seedRowIntoProfile(t, gopiHome, resumeNoCheckpointFixtureRow)
		root := processSessionsRoot(t)
		clearRowRunDir(t, root, resumeNoCheckpointFixtureRow)
		seedRunWhoseRawLogWasDeleted(t, root, resumeNoCheckpointFixtureRow)

		_, err := resolveResumeTarget(resumeNoCheckpointFixtureRow)
		if err == nil {
			t.Fatal("a row whose checkpoint is gone must still be REFUSED — relaxing it into a fresh start is the 2026-10-28 defect")
		}
		msg := err.Error()
		if !strings.Contains(msg, resumeNoCheckpointRanNoCheckpoint) {
			t.Fatalf("the refusal must state verdict %q for a row that ran, got:\n%s", resumeNoCheckpointRanNoCheckpoint, msg)
		}
		if strings.Contains(msg, resumeNoCheckpointNeverStarted) {
			t.Fatalf("a row holding a run log did not \"never start\" — got:\n%s", msg)
		}
		// The evidence the verdict rests on is IN the sentence: the run log's
		// name, so a reader can go and look at it.
		if !strings.Contains(msg, resumeNoCheckpointRanUUID) {
			t.Errorf("the ran-no-checkpoint refusal must quote the run log it measured (%s), got:\n%s",
				resumeNoCheckpointRanUUID, msg)
		}
	})

	// THE PREDICATE ITSELF, over the four shapes a directory can have — so a
	// future edit that widens "never started" (e.g. back onto the output.log's
	// `executor cwd →` line, which was measured NOT to discriminate) turns red
	// here rather than in production.
	t.Run("c_the_predicate_is_the_run_log_not_the_output_log", func(t *testing.T) {
		cases := []struct {
			name        string
			ev          resumeRowEvidence
			wantVerdict string
		}{
			{"directory absent", resumeRowEvidence{}, resumeNoCheckpointNeverStarted},
			{"directory exists, empty", resumeRowEvidence{Exists: true}, resumeNoCheckpointNeverStarted},
			{"run log only (raw log pruned)", resumeRowEvidence{Exists: true, RunLogs: []string{"x.jsonl"}}, resumeNoCheckpointRanNoCheckpoint},
			{"raw log only (record never flushed)", resumeRowEvidence{Exists: true, RawLogs: []string{"x.raw.log"}}, resumeNoCheckpointRanNoCheckpoint},
		}
		for _, c := range cases {
			if got := c.ev.verdict(); got != c.wantVerdict {
				t.Errorf("%s: verdict = %q, want %q", c.name, got, c.wantVerdict)
			}
		}
	})
}

// TestReallyRanSessionStillResumes is arm (c): the CONTROL. A row whose run
// recorded a checkpoint resolves exactly as it did before this change, and the
// REAL binary resumes it. If the new verdict logic ever starts refusing a row
// that does have a checkpoint, this arm goes red.
func TestReallyRanSessionStillResumes(t *testing.T) {
	gopiHome, env := resumeLatchSandbox(t)
	seedRowIntoProfile(t, gopiHome, resumeNoCheckpointLiveRow)
	for _, root := range []string{processSessionsRoot(t), sessionsRootOf(gopiHome)} {
		clearRowRunDir(t, root, resumeNoCheckpointLiveRow)
		seedRunDirUnder(t, root, resumeNoCheckpointLiveRow)
	}

	got, err := resolveResumeTarget(resumeNoCheckpointLiveRow)
	if err != nil {
		t.Fatalf("a row that really ran must still resolve: %v", err)
	}
	want := filepath.Join(processSessionsRoot(t), resumeNoCheckpointLiveRow,
		resumeTaskTargetRunUUID+scratch.SessionRawLogFileSuffix)
	if got != want {
		t.Fatalf("resolveResumeTarget(%q) = %q, want %q", resumeNoCheckpointLiveRow, got, want)
	}
	if !resumeTargetWasTaskID || resumeTargetRowID != resumeNoCheckpointLiveRow {
		t.Fatalf("the ROW view must answer for the row: wasTask=%v rowID=%q",
			resumeTargetWasTaskID, resumeTargetRowID)
	}

	// ...and the real binary still continues it (the observable behaviour, not
	// the resolver's opinion).
	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, resumeNoCheckpointLiveRow, resumeLatchContinuationFile(t)},
		"", env, "")
	if code != 0 {
		t.Fatalf("a checkpointed row must resume: code=%d\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, resumeTaskTargetRunUUID) {
		t.Errorf("the resume must continue the row's own run (%s), stderr:\n%s", resumeTaskTargetRunUUID, stderr)
	}
	// The new verdict words must not appear anywhere on a successful resume.
	for _, verdict := range []string{resumeNoCheckpointNeverStarted, resumeNoCheckpointRanNoCheckpoint} {
		if strings.Contains(stderr, verdict) {
			t.Errorf("a successful resume must not print the verdict %q, stderr:\n%s", verdict, stderr)
		}
	}
}

// TestNeverStartedRowNeverStartsASession is the Rule-15 half: the refusal must
// not be a refusal-then-run. A never-started row must exit non-zero AND leave
// the sessions root with no new entry — the "silent fresh session" shape the
// epic removed, re-pinned here for the branch this change adds.
func TestNeverStartedRowNeverStartsASession(t *testing.T) {
	gopiHome, env := resumeLatchSandbox(t)
	seedRowIntoProfile(t, gopiHome, resumeNoCheckpointFixtureRow)
	root := sessionsRootOf(gopiHome)
	clearRowRunDir(t, root, resumeNoCheckpointFixtureRow)
	clearRowRunDir(t, processSessionsRoot(t), resumeNoCheckpointFixtureRow)
	seedNeverStartedRunDir(t, root, resumeNoCheckpointFixtureRow)

	before := countSessionDirs(t, root)
	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, resumeNoCheckpointFixtureRow, resumeLatchContinuationFile(t)},
		"", env, "")
	if code == 0 {
		t.Fatalf("an unstarted row must exit non-zero, got 0\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, resumeNoCheckpointNeverStarted) {
		t.Errorf("the CLI refusal must carry the %q verdict, stderr:\n%s", resumeNoCheckpointNeverStarted, stderr)
	}
	if after := countSessionDirs(t, root); after != before {
		t.Errorf("a refused resume must start NO session: %d entries before, %d after — the silent-fresh-session shape came back",
			before, after)
	}
}
