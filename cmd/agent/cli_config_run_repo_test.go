package main

// cli_config_run_repo_test.go — the FOUR measured shapes of the run-repository
// resolution, driven through the REAL binary (Rule 31: a surface that was never
// executed was never tested, and the resolver has exactly one operator-visible
// surface — `gopi config show`'s `run repo` row).
//
// WHAT A UNIT TEST CANNOT SEE, and therefore what this file exists for:
//
//	internal/reposource's own tests prove the ORDER and the refusal text against
//	a stubbed Input. They cannot prove that the shipped binary, on a real
//	settings document and a real working directory, prints NOTHING where the
//	withdrawn deprecation notice used to print — and that silence is the
//	operator's whole ruling of 2026-10-23 (task 01a16541-57c0).
//
// THE FOUR ARMS, one per acceptance item:
//
//	1. cwd is a git repo, dev_repo UNSET   → resolves, and stderr is EMPTY
//	2. cwd is not a repo, dev_repo UNSET   → refuses, naming all three sources
//	3. cwd is not a repo, dev_repo SET     → resolves, and stderr is EMPTY
//	4. a row's own deliverable.repo        → the resolver prefers it over the
//	                                          cwd (proved at the package
//	                                          level, since `config show`
//	                                          reports the HOST and takes no row)
//
// Every child process is sandboxed: its own GOPI_HOME, its own GOPI_SETTINGS
// document, its own working directory. The operator's profile is never read or
// written (Rule 32/34).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/reposource"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// runRepoTestMockURL is a closed loopback port: `config show` makes no model
	// call, and a run that somehow tried fails fast instead of reaching a
	// provider.
	runRepoTestMockURL = "http://127.0.0.1:1/"
	// runRepoTestLabel is the report row this suite reads. It is spelled here as
	// a literal on purpose: an assertion that reused the production constant
	// would still pass if someone renamed the row, and this file's job is to pin
	// the operator-visible spelling.
	runRepoTestLabel = "run repo"
	// runRepoNotRendered is the row's value when nothing resolved.
	runRepoNotRendered = "(none"
)

// runRepoFixture builds a real git work tree with one commit and returns its
// top level, measured through git itself (a /tmp symlink must not make a correct
// answer compare unequal).
func runRepoFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
			"commit", "-q", "--allow-empty", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

// runRepoNoRepoDir returns a real directory that is NOT inside a git work tree.
// A scratch root that IS inside one would make arms 2/3 untestable, so that
// shape is reported rather than silently asserted.
func runRepoNoRepoDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "no-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").CombinedOutput(); err == nil {
		t.Skipf("the scratch root %s is inside the git work tree %q; the not-a-repo arms are untestable",
			dir, strings.TrimSpace(string(out)))
	}
	return dir
}

// runRepoCase is one invocation: where the child stands, and what its settings
// document declares ("" = a document with no dev_repo key at all).
type runRepoCase struct {
	name       string
	cwd        string
	devRepo    string
	wantSource string // the source label the row must carry ("" = expect a refusal)
}

// runRepoShow runs `gopi config show` for one case and returns (row, stderr).
// The row is the value printed beside the label, so an assertion about it is an
// assertion about what an operator reads.
func runRepoShow(t *testing.T, bin string, tc runRepoCase) (string, string) {
	t.Helper()
	home := t.TempDir()
	settingsDir := t.TempDir()
	body := "{}"
	if tc.devRepo != "" {
		body = `{"` + config.DevRepoSettingsKey + `": ` + runRepoJSONString(tc.devRepo) + `}`
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write the settings fixture: %v", err)
	}
	env := append(testutil.MockEnv(t, home, runRepoTestMockURL),
		config.SettingsEnv+"="+settingsPath)
	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"config", "show"}, "", env, tc.cwd)
	if code != 0 {
		t.Fatalf("`gopi config show` exited %d — it reports configuration and must succeed on every host shape:\n%s\n%s",
			code, stdout, stderr)
	}
	return runRepoRow(t, stdout), stderr
}

// runRepoRow extracts the `run repo` row's VALUE from the report.
func runRepoRow(t *testing.T, stdout string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, runRepoTestLabel) {
			continue
		}
		// The row is `  <label><padding><value>`; the value is what follows the
		// label's own text.
		if i := strings.Index(line, runRepoTestLabel); i >= 0 {
			return strings.TrimSpace(line[i+len(runRepoTestLabel):])
		}
	}
	t.Fatalf("the report carries no %q row:\n%s", runRepoTestLabel, stdout)
	return ""
}

// runRepoJSONString renders a JSON string literal without a dependency on
// encoding/json's field plumbing (a settings document is text an operator
// writes, so the fixture writes text).
func runRepoJSONString(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// TestRunRepoResolutionFourShapes is the acceptance suite: four real runs of the
// shipped binary, each asserting what an operator reads.
func TestRunRepoResolutionFourShapes(t *testing.T) {
	bin := requireE2EGopiBin(t)

	cwdRepo := runRepoFixture(t, "cwd-repo")
	noRepo := runRepoNoRepoDir(t)
	devRepo := runRepoFixture(t, "dev-repo")

	// ① cwd is a git repo, dev_repo UNSET → RESOLVES, AND SAYS NOTHING.
	t.Run("cwd repo with no dev_repo resolves silently", func(t *testing.T) {
		row, stderr := runRepoShow(t, bin, runRepoCase{name: "cwd-repo", cwd: cwdRepo})
		if !strings.Contains(row, cwdRepo) {
			t.Errorf("the row must name the cwd's repository %q, got %q", cwdRepo, row)
		}
		if !strings.Contains(row, reposource.SourceCWD) {
			t.Errorf("the row must name the deciding source %q, got %q", reposource.SourceCWD, row)
		}
		if strings.TrimSpace(stderr) != "" {
			t.Errorf("a cwd that IS a git repository is the NORMAL path — dev_repo is optional and nothing may "+
				"be warned about (operator ruling 2026-10-23); stderr carried:\n%s", stderr)
		}
	})

	// ② cwd is NOT a repo, dev_repo UNSET → ONE refusal naming all three.
	t.Run("nothing available refuses naming three sources", func(t *testing.T) {
		row, stderr := runRepoShow(t, bin, runRepoCase{name: "no-repo", cwd: noRepo})
		if !strings.HasPrefix(row, runRepoNotRendered) {
			t.Errorf("the row must say no repository resolved, got %q", row)
		}
		for _, want := range []string{
			reposource.SourceRowRepo,
			reposource.SourceCWD,
			reposource.SourceDevRepo,
			noRepo, // the cwd's own measured verdict quotes the directory
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("the refusal must carry %q; stderr was:\n%s", want, stderr)
			}
		}
		// The anti-regression arm: the withdrawn notice's own words must not
		// come back. A refusal that says "DEPRECATED" is the old shape wearing a
		// new label.
		if strings.Contains(strings.ToUpper(stderr), "DEPRECATED") {
			t.Errorf("the refusal must not call any source deprecated — dev_repo is an OPTIONAL fallback, "+
				"not a switch that lapsed:\n%s", stderr)
		}
	})

	// ③ cwd is NOT a repo, dev_repo SET → resolves from the third source, silently.
	t.Run("dev_repo resolves when the cwd is not a repo", func(t *testing.T) {
		row, stderr := runRepoShow(t, bin, runRepoCase{name: "dev-repo", cwd: noRepo, devRepo: devRepo})
		if !strings.Contains(row, devRepo) {
			t.Errorf("the row must name the declared dev_repo %q, got %q", devRepo, row)
		}
		if !strings.Contains(row, reposource.SourceDevRepo) {
			t.Errorf("the row must name the deciding source %q, got %q", reposource.SourceDevRepo, row)
		}
		if strings.TrimSpace(stderr) != "" {
			t.Errorf("resolving from a declared dev_repo must be silent; stderr carried:\n%s", stderr)
		}
	})

	// ④ the ROW's own repo beats a cwd in a DIFFERENT repository. `config show`
	// reports the host and takes no row, so this arm is the resolver's own —
	// driven directly, on the same fixture repos the three runs above used, so
	// the four shapes are one table of evidence rather than two stories.
	t.Run("the row's deliverable.repo wins over the cwd", func(t *testing.T) {
		res, err := reposource.Resolve(reposource.Input{RowRepo: devRepo, CWD: cwdRepo})
		if err != nil {
			t.Fatalf("a row naming a real repo must resolve: %v", err)
		}
		if res.Dir != devRepo {
			t.Errorf("resolved %q, want the row's repo %q (the cwd's %q must not win)", res.Dir, devRepo, cwdRepo)
		}
		if res.Source != reposource.SourceRowRepo {
			t.Errorf("resolved from %q, want %q", res.Source, reposource.SourceRowRepo)
		}
	})
}
