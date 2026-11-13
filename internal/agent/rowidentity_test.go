package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/tools"
)

// epic: one task row, one id. The FOUR values that used to disagree —
// the run DIRECTORY's name, the record FILE's name, the record's
// session_start.sessionId, and (through taskRunSessionFromRowDir) the ledger's
// session_id column — must be one string for a `task run` child, and a fresh
// uuid for every other face.
const (
	rowIdentityTaskID   = "01a17de9-e772-7470-80c1-f954f07c6b59"
	rowIdentityModel    = "row-identity-model"
	rowIdentityGopiComp = ".gopi"
)

// rowIdentityEnv isolates the profile and says "this process is / is not a
// task-run child". GOPI_RAW_LOG stays ON here (unlike newTestAgentEnv): the raw
// log's path is one of the four values under test.
func rowIdentityEnv(t *testing.T, wrapperID string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "no-settings.json"))
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GOPI_RAW_LOG", "")
	t.Setenv(tools.TaskWrapperEnv, wrapperID)
	return filepath.Join(home, rowIdentityGopiComp, config.SessionsDirName)
}

// TestTaskRunChildMintsTheRowID is the epic's core claim, measured on the
// AGENT the way `task run` builds it: with the wrapper identity in the
// environment, the run id, the session directory, the record file and the
// record's own session_start all say the TASK ID.
func TestTaskRunChildMintsTheRowID(t *testing.T) {
	sessionsRoot := rowIdentityEnv(t, rowIdentityTaskID)

	ag, err := NewAgent(rowIdentityModel)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.SessionLog == nil {
		t.Fatal("no session log — this run recorded nothing")
	}

	sid := ag.SessionLog.SessionID()
	if sid != rowIdentityTaskID {
		t.Errorf("session id = %q, want the row id %q (a task run does not mint its own)", sid, rowIdentityTaskID)
	}

	wantPath := filepath.Join(sessionsRoot, rowIdentityTaskID, rowIdentityTaskID+sessionlayout.JSONLSuffix)
	if got := ag.SessionLog.Path(); got != wantPath {
		t.Errorf("record path = %q, want %q", got, wantPath)
	}
	if filepath.Base(ag.SessionLog.Path()) != rowIdentityTaskID+sessionlayout.JSONLSuffix {
		t.Errorf("record FILE name = %q, want it named by the row id", filepath.Base(ag.SessionLog.Path()))
	}
	// The display id a READER derives must be the same string, or the ledger's
	// session_id column and `gopi session log <task-id>` still miss.
	if got := sessionlayout.SessionID(ag.SessionLog.Path()); got != rowIdentityTaskID {
		t.Errorf("sessionlayout.SessionID(record) = %q, want %q", got, rowIdentityTaskID)
	}
	// The run DIRECTORY is named by the row — unchanged behaviour, asserted
	// here so the four values are compared in one place.
	if got := filepath.Base(filepath.Dir(ag.SessionLog.Path())); got != rowIdentityTaskID {
		t.Errorf("run directory = %q, want %q", got, rowIdentityTaskID)
	}
	if ag.RawLog == nil {
		t.Fatal("no raw log — the task run lost its wire log")
	}
	wantRaw := filepath.Join(sessionsRoot, rowIdentityTaskID, rowIdentityTaskID+scratch.SessionRawLogFileSuffix)
	if got := ag.RawLog.Path(); got != wantRaw {
		t.Errorf("raw log = %q, want %q", got, wantRaw)
	}
}

// TestTaskRunSecondRunContinuesTheSameRecord is the OTHER half: because the id
// is the row's, a second run of the same row lands on the same files — and the
// writers must APPEND rather than refuse. Before this change the raw logger
// opened with O_EXCL, so unifying the id would have cost every re-run its wire
// log.
func TestTaskRunSecondRunContinuesTheSameRecord(t *testing.T) {
	sessionsRoot := rowIdentityEnv(t, rowIdentityTaskID)

	first, err := NewAgent(rowIdentityModel)
	if err != nil {
		t.Fatalf("first NewAgent: %v", err)
	}
	firstPath := first.SessionLog.Path()
	firstRaw := first.RawLog.Path()
	if err := first.SessionLog.Close(); err != nil {
		t.Fatalf("close first session log: %v", err)
	}
	if err := first.RawLog.Close(); err != nil {
		t.Fatalf("close first raw log: %v", err)
	}

	second, err := NewAgent(rowIdentityModel)
	if err != nil {
		t.Fatalf("second NewAgent: %v", err)
	}
	if second.SessionLog == nil || second.RawLog == nil {
		t.Fatal("second run minted no logs (the O_EXCL collision would look like this)")
	}
	if got := second.SessionLog.Path(); got != firstPath {
		t.Errorf("second run's record = %q, want the first run's %q", got, firstPath)
	}
	if got := second.RawLog.Path(); got != firstRaw {
		t.Errorf("second run's raw log = %q, want the first run's %q", got, firstRaw)
	}
	if err := second.SessionLog.Close(); err != nil {
		t.Fatalf("close second session log: %v", err)
	}
	if err := second.RawLog.Close(); err != nil {
		t.Fatalf("close second raw log: %v", err)
	}

	// One directory, two records in ONE file — and no third directory.
	dirs, err := os.ReadDir(sessionsRoot)
	if err != nil {
		t.Fatalf("read sessions root: %v", err)
	}
	if len(dirs) != 1 || dirs[0].Name() != rowIdentityTaskID {
		t.Fatalf("sessions root holds %d entries, want exactly the row's dir: %v", len(dirs), dirNames(dirs))
	}
	body, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if n := strings.Count(string(body), `"`+eventTypeSessionStartKey+`"`); n < 2 {
		t.Errorf("record holds %d session_start rows, want 2 (each run writes its own segment header)", n)
	}
	rawBody, err := os.ReadFile(firstRaw)
	if err != nil {
		t.Fatalf("read raw log: %v", err)
	}
	if !strings.Contains(string(rawBody), rawLogResumeHeaderPrefix) {
		t.Errorf("the continued raw log carries no resume header — one file cannot tell a second run from a first")
	}
}

// TestBareSessionStillMintsAUUID is the boundary that keeps `gopi chat` and a
// bare prompt exactly as they were: with no wrapper identity the run id is a
// minted uuid, and the directory is named by it.
func TestBareSessionStillMintsAUUID(t *testing.T) {
	sessionsRoot := rowIdentityEnv(t, "")

	ag, err := NewAgent(rowIdentityModel)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer func() { _ = ag.SessionLog.Close() }() //stylegate:ignore teardown close of the fixture agent's log; the test read the record already and a close error cannot change the id/path assertions above it
	sid := ag.SessionLog.SessionID()
	if sid == "" || sid == rowIdentityTaskID {
		t.Fatalf("bare session id = %q, want a freshly minted uuid", sid)
	}
	want := filepath.Join(sessionsRoot, sid, sessionlayout.RecordName)
	if got := ag.SessionLog.Path(); got != want {
		t.Errorf("bare record path = %q, want %q", got, want)
	}
}

// TestMintRunIDRefusesToAdaptAnInadmissibleID pins the third answer: an id this
// tree does not accept as a path element is NEVER adapted into something else.
// The refusal comes from sessionDirForID (by name); mintRunID must not paper
// over it with a uuid, which would make the run write somewhere the operator
// did not name.
func TestMintRunIDRefusesToAdaptAnInadmissibleID(t *testing.T) {
	const bad = "01a17de9e772747080c1f954f07c6b59" // the branch spelling: 32 hex, no dashes
	if got := mintRunID(bad); got == bad {
		t.Fatalf("mintRunID(%q) = %q — an inadmissible id must not become the run id", bad, got)
	} else if got != "" && !strings.Contains(got, "-") {
		t.Fatalf("mintRunID(%q) = %q, want a uuid (never an adaptation of the refused spelling)", bad, got)
	}
	if got := mintRunID(rowIdentityTaskID); got != rowIdentityTaskID {
		t.Errorf("mintRunID(%q) = %q, want it verbatim", rowIdentityTaskID, got)
	}
}

// eventTypeSessionStartKey is the record's own JSON key for the header row
// (the value sessionevent writes); named once here rather than inline (Rule 1).
const eventTypeSessionStartKey = "type"

// dirNames lists directory entry names for a failure message (no omission).
func dirNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
