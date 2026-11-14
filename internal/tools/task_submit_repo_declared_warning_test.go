// Regression tests for the repo-DECLARED submit warning.
//
// The defect (row 01a159a9-ecb8-7664-8beb-57438ac4023f): /home/user/work/GoPi
// (uppercase, frozen at 2026-10-18) and /home/user/work/gopi (lowercase,
// master) both exist; the settings `repos` table declares a build command for
// the lowercase path only; `task submit` without --repo stamps the process CWD;
// and the ONE repo check at the submit seam —
// `git rev-parse --is-inside-work-tree` — PASSES on the uppercase checkout
// because it really is a work tree. The row queues and dispatch then refuses it
// terminally in the clone channel.
//
// Cases:
//
//	(a) RED   — a work-tree repo absent from the repos table → warning fires,
//	            naming the path, the table key, the declared list and the
//	            channel sentence;
//	(b) GREEN — the same repo present in the table → silent;
//	(c) GREEN — the same DIRECTORY reached through another spelling → silent;
//	(d) the channel half: dev_repo set ⇒ the sentence quotes the refusal the
//	    row will actually meet; dev_repo empty ⇒ it says today's run works;
//	(e) an EMPTY table reads as "(none …)", never as a blank list;
//	(f) WRITE FACE — TaskSubmit with an undeclared work tree still QUEUES the
//	    row (warn, never refuse) and keeps the deliverable.repo stamp;
//	(g) WRITE FACE, declared ⇒ the confirmation carries no such warning;
//	(h) a grep gate keeping the literals at their single definition site.
//
// The write-face cases drive the PRODUCTION submit handler, not the predicate:
// the seam is where the defect was invisible, so the seam is what must be red
// on the pre-fix tree.
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// repoDeclaredWarningDefinitionFile is the single definition site of the
// warning's literals (Rule 13); the grep gate below scans the rest of the
// package for them.
const repoDeclaredWarningDefinitionFile = "task_submit_repo_declared_warning.go"

// repoDeclaredWarningNeedles are the strings that may appear ONLY in the
// definition file. They are the parts of THIS file's own literals — a needle
// that is another package's sentence quoted in a neighbour's comment (the
// clone channel's "declares no build command in settings …", quoted by
// task_submit_durable_repo_repair.go) would make the gate demand a rewrite of
// prose it does not own.
var repoDeclaredWarningNeedles = []string{
	"is not a key of the settings",
	"the worktree channel is in force today",
	"so this row's run takes the clone channel",
}

// repoDeclaredSettings writes a settings.json carrying `repos` as the table's
// keys (each with a build command) into a fresh directory and pins it for the
// submit seam. It returns the settings path.
func repoDeclaredSettings(t *testing.T, repos ...string) string {
	t.Helper()
	table := map[string]any{}
	for _, r := range repos {
		table[r] = map[string]any{"build": "true"}
	}
	body := map[string]any{config.ReposSettingsKey: table}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if werr := os.WriteFile(path, data, 0o644); werr != nil {
		t.Fatalf("write settings: %v", werr)
	}
	t.Setenv(config.SettingsPathEnv, path)
	return path
}

// TestRepoUndeclaredWarningFiresForARepoOutsideTheTable is the RED half of the
// predicate: the path is a work tree (that is the whole point), the table does
// not carry it, and the warning must name both facts.
func TestRepoUndeclaredWarningFiresForARepoOutsideTheTable(t *testing.T) {
	const repo = "/home/user/work/GoPi"
	const declaredRepo = "/home/user/work/gopi"
	repoDeclaredSettings(t, declaredRepo)
	t.Setenv(config.DevRepoEnvName, "zpeer:"+declaredRepo) // the clone channel is ON

	got := TaskSubmitRepoUndeclaredWarning(repo)
	if got == "" {
		t.Fatalf("repo %q is absent from the %q table but the submit seam said nothing", repo, config.ReposSettingsKey)
	}
	for _, want := range []string{repo, config.ReposSettingsKey, declaredRepo, "refusing to push"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning does not carry %q:\n%s", want, got)
		}
	}
}

// TestRepoUndeclaredWarningSilentWhenDeclared is the GREEN half: a repo the
// table declares produces nothing, on either channel setting.
func TestRepoUndeclaredWarningSilentWhenDeclared(t *testing.T) {
	const repo = "/home/user/work/gopi"
	repoDeclaredSettings(t, repo)
	for _, devRepo := range []string{"", "zpeer:" + repo} {
		t.Setenv(config.DevRepoEnvName, devRepo)
		if got := TaskSubmitRepoUndeclaredWarning(repo); got != "" {
			t.Errorf("dev_repo=%q: declared repo %q still warned:\n%s", devRepo, repo, got)
		}
	}
}

// TestRepoUndeclaredWarningSilentForAnotherSpellingOfTheSameDir: an operator's
// spelling must not be reported as an undeclared repository.
func TestRepoUndeclaredWarningSilentForAnotherSpellingOfTheSameDir(t *testing.T) {
	dir := t.TempDir()
	repoDeclaredSettings(t, dir)
	t.Setenv(config.DevRepoEnvName, "")
	for _, spelling := range []string{dir + string(os.PathSeparator), dir + "/."} {
		if got := taskSubmitRepoUndeclaredWarningFor(spelling, []string{dir}, false); got != "" {
			t.Errorf("spelling %q of the declared dir %q warned:\n%s", spelling, dir, got)
		}
	}
	// And the SAME-DIR arm, not merely the string arm: a symlink to the
	// declared directory is the declared directory.
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlink unavailable on this filesystem: %v", err)
	}
	if got := taskSubmitRepoUndeclaredWarningFor(link, []string{dir}, false); got != "" {
		t.Errorf("symlink %q to the declared dir %q warned:\n%s", link, dir, got)
	}
}

// TestRepoUndeclaredWarningSaysWhichChannelIsInForce: the consequence differs
// by channel, and the warning must state the one that applies — a warning that
// claims a refusal which cannot happen is a false alarm.
func TestRepoUndeclaredWarningSaysWhichChannelIsInForce(t *testing.T) {
	const repo = "/home/user/work/GoPi"
	on := taskSubmitRepoUndeclaredWarningFor(repo, []string{"/home/user/work/gopi"}, true)
	if !strings.Contains(on, "refusing to push") {
		t.Errorf("clone channel ON must quote the dispatch refusal:\n%s", on)
	}
	if strings.Contains(on, "would still run") {
		t.Errorf("clone channel ON must not claim the row would run:\n%s", on)
	}
	off := taskSubmitRepoUndeclaredWarningFor(repo, []string{"/home/user/work/gopi"}, false)
	if !strings.Contains(off, "would still run") {
		t.Errorf("clone channel OFF must say today's run works:\n%s", off)
	}
	if strings.Contains(off, "refusing to push") {
		t.Errorf("clone channel OFF must not claim a refusal:\n%s", off)
	}
}

// TestRepoUndeclaredWarningRendersAnEmptyTableAsNone: "nothing is declared" and
// "you picked a declared repo" must be distinguishable — a blank list reads as
// a rendering bug.
func TestRepoUndeclaredWarningRendersAnEmptyTableAsNone(t *testing.T) {
	got := taskSubmitRepoUndeclaredWarningFor("/home/user/work/GoPi", nil, true)
	if !strings.Contains(got, taskSubmitRepoUndeclaredNone) {
		t.Errorf("an empty table must render its own marker, got:\n%s", got)
	}
	if got := taskSubmitRepoUndeclaredWarningFor("   ", nil, true); got != "" {
		t.Errorf("an empty repo path must be silent, got:\n%s", got)
	}
}

// TestRepoUndeclaredWarningWriteFaceQueuesButWarns is the seam-level proof: the
// production submit handler prints the warning AND still creates the row with
// the repo it was given. This is the case that was silent on the pre-fix tree
// (the observed output was a bare "Task queued: <id> …").
func TestRepoUndeclaredWarningWriteFaceQueuesButWarns(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	_ = path
	repo := filepath.Join(t.TempDir(), "Uppercase Checkout")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustGitInit(t, repo)
	repoDeclaredSettings(t, "/somewhere/else")
	t.Setenv(config.DevRepoEnvName, "")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "submit from an undeclared checkout", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit = %v (the declared-repo warning must never refuse a row)", err)
	}
	if !strings.Contains(out, repo) {
		t.Fatalf("the confirmation does not carry the warning for %q:\n%s", repo, out)
	}
	if !strings.Contains(out, TaskSubmitRepoUndeclaredChannelWaits) {
		t.Errorf("the confirmation does not state the channel consequence:\n%s", out)
	}
	fields := strings.Fields(out)
	if len(fields) < 3 {
		t.Fatalf("confirmation unreadable: %q", out)
	}
	taskID := strings.TrimSpace(fields[2])
	show, serr := TaskShow(taskID)
	if serr != nil {
		t.Fatalf("TaskShow: %v", serr)
	}
	if !strings.Contains(show, "deliverable.repo: "+repo) {
		t.Errorf("row lost its deliverable.repo stamp:\n%s", show)
	}
}

// TestRepoUndeclaredWarningWriteFaceSilentWhenDeclared: the declared case must
// stay quiet on the same seam, or the warning becomes noise an operator learns
// to skip.
func TestRepoUndeclaredWarningWriteFaceSilentWhenDeclared(t *testing.T) {
	_ = testutil.NewTestDBPath(t)
	repo := filepath.Join(t.TempDir(), "declared-checkout")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustGitInit(t, repo)
	repoDeclaredSettings(t, repo)
	t.Setenv(config.DevRepoEnvName, "")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "submit from a declared checkout", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit = %v", err)
	}
	if strings.Contains(out, TaskSubmitRepoUndeclaredChannelWaits) ||
		strings.Contains(out, "is not a key of the settings") {
		t.Errorf("a declared repo produced the undeclared warning:\n%s", out)
	}
}

// TestRepoUndeclaredWarningLiteralsLiveAtOneSite: the template and both channel
// sentences are REFERENCED, never re-typed (Rules 1b/13). The absence half
// scans the package; the presence half makes deletion an unsatisfying answer.
func TestRepoUndeclaredWarningLiteralsLiveAtOneSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == repoDeclaredWarningDefinitionFile {
			continue
		}
		data, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		scanned++
		for _, n := range repoDeclaredWarningNeedles {
			if strings.Contains(string(data), n) {
				t.Errorf("%s re-types %q — reference the constant in %s instead (Rule 13)",
					name, n, repoDeclaredWarningDefinitionFile)
			}
		}
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d production files — the gate's glob is broken, refusing a vacuous pass", scanned)
	}
	def, derr := os.ReadFile(repoDeclaredWarningDefinitionFile)
	if derr != nil {
		t.Fatalf("read definition file: %v", derr)
	}
	for _, n := range repoDeclaredWarningNeedles {
		if !strings.Contains(string(def), n) {
			t.Errorf("%s no longer defines %q — the gate cannot be satisfied by deletion",
				repoDeclaredWarningDefinitionFile, n)
		}
	}
}
