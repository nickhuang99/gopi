package main

// devworkspace_channel_dead_repo_test.go — THE REFUSAL MUST NAME THE REAL
// REASON (epic 01a15629).
//
// THE MEASURED DEFECT. Three live rows died at the clone channel with one
// sentence:
//
//	clone channel: repo "/home/user/workspace/01a1510e-…" declares no build
//	command in settings repos → …build
//
// and that sentence was FALSE. The directory was gone: it was the PARENT run's
// workspace, reclaimed when the parent's push landed. A path with nothing on it
// declares nothing for the trivial reason that it does not exist, and telling an
// operator to go and declare a build command for a deleted directory points at
// the wrong repair (the repair lives on the ROW, not in the settings file).
//
// THIS TEST IS THE RED PROOF: it names no symbol the fix adds (it asserts on the
// channel's real entry point and on the text of the error), so it compiles and
// runs on the pre-fix tree, where the error carries "declares no build command"
// and the assertion fails.
//
// The THIRD case lives in this file too, because the fix must not swallow it: a
// repository that really exists and really declares nothing still refuses with
// the original sentence — that sentence is not wrong, it was only being said in
// the wrong circumstances (see
// TestCloneChannelRefusesAnUndeclaredRepoOutsideTheRootVerbatim).

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/tools"
)

// deadRepoChannelFixture builds a source repository that DECLARES a build, a
// workspace root, and a row whose deliverable.repo names `rowRepo`.
func deadRepoChannelFixture(t *testing.T, rowRepo func(root, taskID string) string) (taskID, source, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the clone channel reads real repositories")
	}
	base := t.TempDir()
	source = filepath.Join(base, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	channelGit(t, source, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	channelGit(t, source, "add", "a.txt")
	channelGit(t, source, "commit", "-q", "-m", "init")

	root = filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	taskID = "01a15629-0000-7000-8000-00000000dead"
	repo := rowRepo(root, taskID)

	settings := filepath.Join(base, "settings.json")
	body := `{"dev_repo":"` + source + `","workspace_root":"` + root +
		`","repos":{"` + source + `":{"build":"echo TRACED-BUILD"}}}`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsPathEnv, settings)
	t.Setenv(config.DevRepoEnvName, "")
	t.Setenv(config.WorkspaceRootEnvName, "")

	dbPath := filepath.Join(base, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv(config.SkipClaimBroadcastEnv, "1")
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "dead repo path fixture",
		Payload: "the row names a path the channel must diagnose",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed the row: %v", err)
	}
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore process-lifetime handle closed by the fixture
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', deliverable = ? WHERE task_id = ?`,
		`{"repo":"`+repo+`"}`, taskID); err != nil {
		t.Fatalf("stamp the row's deliverable: %v", err)
	}
	if got := tools.TaskDeliverableRepoFor(taskID); got != repo {
		t.Fatalf("fixture: row repo = %q, want %q", got, repo)
	}
	return taskID, source, root
}

// TestCloneChannelNamesADeadRepoPathAsDead is the acceptance's red: the channel
// must say the directory is GONE (or that what is there is no git work tree),
// and must NOT claim the repository declares no build command.
func TestCloneChannelNamesADeadRepoPathAsDead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rowRepo func(root, taskID string) string
		want    string
	}{
		{
			name: "the parent's workspace was reclaimed",
			rowRepo: func(root, taskID string) string {
				return devworkspace.Dir(root, taskID) // never created: the shape after a reclaim
			},
			want: devworkspace.RepoPathMissingReason,
		},
		{
			name: "something is there, but it is no git work tree",
			rowRepo: func(root, taskID string) string {
				dir := devworkspace.Dir(root, taskID)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			want: devworkspace.RepoPathNotGitReason,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID, _, _ := deadRepoChannelFixture(t, tc.rowRepo)
			var log bytes.Buffer
			_, _, err := taskRunCloneChannelFor(taskID, &log)
			t.Logf("the channel's own log:\n%s", log.String())
			if err == nil {
				t.Fatalf("a repo path that cannot carry a build must refuse; log:\n%s", log.String())
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("the refusal must name the MEASURED state %q; got: %s", tc.want, msg)
			}
			if strings.Contains(msg, "declares no build command") {
				t.Fatalf("a dead path must not be reported as an UNDECLARED repository — that sentence sends the operator to the settings file for a repair that lives on the row; got: %s", msg)
			}
			// The remedy travels with the refusal: the refactor of the sentence
			// must not drop the door out.
			if !strings.Contains(msg, tools.TaskRepoRepairVerb) {
				t.Fatalf("the refusal must carry the repair verb %q; got: %s", tools.TaskRepoRepairVerb, msg)
			}
			// And the log says it too, so the row's own output records WHY.
			if !strings.Contains(log.String(), fmt.Sprintf(RepoRefusalDeadPathFmt, tools.TaskDeliverableRepoFor(taskID), tc.want)) {
				t.Fatalf("the channel log must carry the same diagnosis; got:\n%s", log.String())
			}
		})
	}
}
