package main

// devworkspace_channel_trace_test.go — the clone channel's SOURCE-REPOSITORY
// FALLBACK, proved through the channel's real entry point (bug 01a1515c, second
// half; Rule 31 — a command that was never run is not a delivery).
//
// The three red lines the acceptance names, each as an assertion:
//
//	① a row whose repo is a workspace clone of a DECLARED source runs: the log
//	   carries the trace line (attempt + hit) and the run passes the
//	   build-declaration check with the SOURCE's command;
//	② a row whose repo is a normal repository (outside the workspace root) is
//	   never traced — no trace line, and a declared build is used as before, so
//	   the log of a normal repo is unchanged;
//	③ when neither the repo nor its traced source declares a build the channel
//	   still REFUSES, with the sentence it has always had, spelled verbatim.
//
// The unit tests in internal/devworkspace pin TraceToSource's own four lines;
// this file pins what the RUN does with them.

import (
	"bytes"
	"github.com/nick/gopi/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/tools"
)

// channelTraceFixture builds: a SOURCE repository with one commit, a CLONE of it
// at <root>/<task-id> (the shape a reframe child inherits from its parent), a
// settings document that switches the clone channel ON for the source and
// declares `build` only when declaredSource says so, and a ledger row whose
// deliverable.repo names `rowRepo`. Returns (taskID, source, clone, root).
func channelTraceFixture(t *testing.T, declaredSource bool, rowRepoInWorkspace bool) (string, string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the clone channel clones and traces real repositories")
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	channelGit(t, source, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	channelGit(t, source, "add", "a.txt")
	channelGit(t, source, "commit", "-q", "-m", "init")

	root := filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	taskID := "01a1515c-0000-7000-8000-0000000trace"
	clone := devworkspace.Dir(root, taskID)
	channelGit(t, base, "clone", "-q", source, clone)

	declared := `"` + source + `":{"build":"echo TRACED-BUILD"}`
	if !declaredSource {
		declared = `"` + source + `":{"build_timeout_sec":60}`
	}
	// The row repo is normally the parent's WORKSPACE (the measured defect);
	// the "normal repo" case points the row at the SOURCE itself instead.
	rowRepo := clone
	if !rowRepoInWorkspace {
		rowRepo = source
	}
	settings := filepath.Join(base, "settings.json")
	body := `{"dev_repo":"` + source + `","workspace_root":"` + root + `","repos":{` + declared + `}}`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsPathEnv, settings)
	// The env channel WINS over settings, so a stray value from the parent
	// environment would silently redirect the fixture's channel.
	t.Setenv(config.DevRepoEnvName, "")
	t.Setenv(config.WorkspaceRootEnvName, "")

	dbPath := filepath.Join(base, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv(config.SkipClaimBroadcastEnv, "1")
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "clone channel trace fixture",
		Payload: "seed for the source-trace test",
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
	blob := `{"repo":"` + rowRepo + `"}`
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("stamp the row's deliverable: %v", err)
	}
	if got := tools.TaskDeliverableRepoFor(taskID); !tools.SameDir(got, rowRepo) {
		t.Fatalf("fixture: row repo = %q, want %q", got, rowRepo)
	}
	return taskID, source, clone, root
}

// channelGit runs one git command in dir, failing the test on error.
func channelGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestCloneChannelTracesAWorkspaceRepoToItsSourcesDeclaredBuild is ①: the row
// names the parent's workspace, the workspace's origin is the declared source,
// and the run starts with the SOURCE's build command — loudly.
func TestCloneChannelTracesAWorkspaceRepoToItsSourcesDeclaredBuild(t *testing.T) {
	taskID, source, clone, _ := channelTraceFixture(t, true, true)
	var log bytes.Buffer

	state, _, err := taskRunCloneChannelFor(taskID, &log)
	t.Logf("the channel's own log:\n%s", log.String())
	if err != nil {
		t.Fatalf("the traced source declares a build, so the run must start; got: %v\nlog:\n%s", err, log.String())
	}
	if state == nil {
		t.Fatalf("the clone channel reported OFF for a configured dev_repo")
	}
	if state.cfg.Build != "echo TRACED-BUILD" {
		t.Fatalf("the run's build = %q, want the SOURCE repository's declared command", state.cfg.Build)
	}
	// THE TRACE LINE, printed for the attempt AND for the hit: a fallback that
	// rescued the run without saying so would be a silent repair (Rule 15).
	for _, want := range []string{devworkspace.TraceLineFormat[:12], clone, source} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("the log must name %q; got:\n%s", want, log.String())
		}
	}
	if !strings.Contains(log.String(), "→ build declared in settings "+config.ReposSettingsKey) {
		t.Fatalf("the log must report the trace's OUTCOME (the hit and the declaration it will use); got:\n%s", log.String())
	}
}

// TestCloneChannelNeverTracesARepoOutsideTheWorkspaceRoot is ②: a normal
// repository's run is unchanged — declared build used, not one trace line.
func TestCloneChannelNeverTracesARepoOutsideTheWorkspaceRoot(t *testing.T) {
	taskID, source, _, root := channelTraceFixture(t, true, false)
	var log bytes.Buffer

	state, _, err := taskRunCloneChannelFor(taskID, &log)
	t.Logf("the channel's own log:\n%s", log.String())
	if err != nil {
		t.Fatalf("a declared repo must run; got: %v\nlog:\n%s", err, log.String())
	}
	if state == nil || state.cfg.Build != "echo TRACED-BUILD" {
		t.Fatalf("the declared build must be used verbatim; got %+v", state)
	}
	if strings.Contains(log.String(), "source trace:") {
		t.Fatalf("a repo outside the workspace root %s must not be traced; got:\n%s", root, log.String())
	}
	if !strings.Contains(log.String(), source) {
		t.Fatalf("the channel line should still name the dev repo; got:\n%s", log.String())
	}
}

// TestCloneChannelRefusesWhenNeitherTheRepoNorItsSourceDeclaresABuild is ③: the
// fallback rescues nothing it cannot prove, and the refusal sentence is the
// pre-existing one, spelled verbatim.
func TestCloneChannelRefusesWhenNeitherTheRepoNorItsSourceDeclaresABuild(t *testing.T) {
	taskID, _, clone, _ := channelTraceFixture(t, false, true)
	var log bytes.Buffer

	state, _, err := taskRunCloneChannelFor(taskID, &log)
	t.Logf("the channel's own log:\n%s", log.String())
	if err == nil {
		t.Fatalf("no declaration anywhere must refuse; got state %+v\nlog:\n%s", state, log.String())
	}
	want := "clone channel: repo \"" + clone + "\" declares no build command in settings repos → " + clone +
		".build — refusing to push a workspace that was never built (declare the command; it is never guessed)"
	if err.Error() != want {
		t.Fatalf("the refusal sentence must be unchanged, verbatim:\n got: %s\nwant: %s", err.Error(), want)
	}
	// AND THE MISS IS SAID OUT LOUD: the attempt line and the miss line are both
	// there, so the operator can see the fallback was tried and came back empty.
	if !strings.Contains(log.String(), devworkspace.TraceLineFormat[:12]) {
		t.Fatalf("the attempted trace must be printed even when it misses; got:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "declares no build command in settings "+config.ReposSettingsKey+" either") {
		t.Fatalf("the miss must be reported; got:\n%s", log.String())
	}
}

// TestCloneChannelARepoOutsideTheRootCostsNoGitCall is ②'s second half, measured
// at the PROCESS boundary rather than inferred from the source: a git wrapper on
// PATH records every invocation the resolution makes. For a normal repository
// (declared build, outside the workspace root) the count is ZERO — the fallback
// spawns nothing and prints nothing, so the run is unchanged.
func TestCloneChannelARepoOutsideTheRootCostsNoGitCall(t *testing.T) {
	_, source, _, root := channelTraceFixture(t, true, false)
	calls := installGitCallRecorder(t)

	var log bytes.Buffer
	cfg := devworkspace.Config{Root: root, RootSource: "settings workspace_root", Log: &log}
	rc, err := resolveCloneChannelBuild(cfg, source)
	if err != nil {
		t.Fatalf("the declared repo must resolve; got: %v", err)
	}
	if rc.Build != "echo TRACED-BUILD" {
		t.Fatalf("build = %q, want the declared command", rc.Build)
	}
	if got := calls(); got != 0 {
		t.Fatalf("a repo outside the workspace root must spawn no git process; the fallback ran git %d time(s)", got)
	}
	if strings.Contains(log.String(), "source trace:") {
		t.Fatalf("nothing may be traced or printed for a normal repo; got:\n%s", log.String())
	}
}

// installGitCallRecorder puts a counting shim in front of git for the rest of the
// test and returns a reader for the count. It is the cheapest way to assert "no
// git process was spawned" without reading the implementation.
func installGitCallRecorder(t *testing.T) func() int {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not installed: the recorder wraps a real git")
	}
	dir := t.TempDir()
	countFile := filepath.Join(dir, "calls")
	shim := filepath.Join(dir, "git")
	script := "#!/bin/sh\necho call >> " + countFile + "\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		body, err := os.ReadFile(countFile)
		if err != nil {
			return 0
		}
		return len(strings.Fields(string(body)))
	}
}

// TestCloneChannelRefusesAnUndeclaredRepoOutsideTheRootVerbatim is the third
// red line's other half: a repo that is NEITHER inside the workspace root NOR
// declared anywhere keeps the channel's original sentence, and is not traced at
// all — the fallback fires only for the channel's own checkouts.
func TestCloneChannelRefusesAnUndeclaredRepoOutsideTheRootVerbatim(t *testing.T) {
	source, _, root := func() (string, string, string) {
		_, src, _, rt := channelTraceFixture(t, true, false)
		return src, "", rt
	}()
	// Re-point the settings so the SOURCE declares nothing (the fixture already
	// declares it; overwrite the whole document and keep the channel ON).
	settings := filepath.Join(t.TempDir(), "settings.json")
	body := `{"dev_repo":"` + source + `","workspace_root":"` + root + `","repos":{"` + source + `":{"build_timeout_sec":60}}}`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsPathEnv, settings)

	var log bytes.Buffer
	cfg := devworkspace.Config{Root: root, RootSource: "settings workspace_root", Log: &log}
	_, err := resolveCloneChannelBuild(cfg, source)
	if err == nil {
		t.Fatalf("no declaration must refuse")
	}
	want := "clone channel: repo \"" + source + "\" declares no build command in settings repos → " + source +
		".build — refusing to push a workspace that was never built (declare the command; it is never guessed)"
	if err.Error() != want {
		t.Fatalf("the refusal sentence must be unchanged, verbatim:\n got: %s\nwant: %s", err.Error(), want)
	}
	if strings.Contains(log.String(), "source trace:") {
		t.Fatalf("a repo outside the workspace root must not be traced even when undeclared; got:\n%s", log.String())
	}
}
