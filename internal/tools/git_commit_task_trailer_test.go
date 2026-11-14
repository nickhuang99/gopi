package tools

// git_commit_task_trailer_test.go — the TWO references a commit carries, and
// the ONE index that reads them (`git log --grep`):
//
//	task: <ledger row>   which row produced this commit
//	host: <hostname>     which MACHINE produced it
//
// Two arms, and the second is the one that matters (Rule 31: a surface not
// executed is not delivered):
//
//	the REWRITE  — the unit face: which commands get the lines, which are left
//	               exactly as written (a quoted string, a heredoc body, an
//	               amend, a caller who already passed --trailer, a run with no
//	               ledger row, values that would break out of the quoting);
//	the GREP     — the real face: a REAL `git commit` executed through the
//	               bash tool in a scratch repository, then
//	               `git log --grep="task: <id>"` AND `--grep="host: <h>"` must
//	               both return that commit.
//
// The pre-fix state is the same test with the injection removed: the rewrite
// arm fails on every positive case and the grep arm prints nothing, which is
// the failing half the report quotes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// trailerTestTaskID is the row this test cites. It is a REAL spelling (hex and
// dashes) because the injection's safety guard refuses anything else — a test
// using a "t-1"-style slug would be exercising the refusal path, not the
// mechanism.
const trailerTestTaskID = "01a17857-537c-7636-a6be-fb94413d7f54"

// gitIn runs one git command in dir and returns its combined output. It is the
// test's OWN probe (never the bash tool: the grep must be made by git itself,
// not by the surface under test).
func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// trailerTestInjection is the exact text the seam should inject for the
// CURRENT process environment: one ` --trailer '<lines>'` shell word, where the
// lines are whatever gitCommitTrailerLines() reports. Building the expectation
// from the same function the production path uses keeps the test from
// re-typing the host name (which is machine-specific and must not be a literal
// in a test, Rule 1) — and the emptiness guard keeps a machine that cannot name
// itself from turning into a silently vacuous green (Rule 15).
func trailerTestInjection(t *testing.T) string {
	t.Helper()
	lines := gitCommitTrailerLines()
	if len(lines) == 0 {
		t.Fatalf("no trailer lines available in this environment — the test would assert nothing")
	}
	out := ""
	for _, line := range lines {
		out += " " + gitCommitTrailerFlag + " " + trailerValueQuote + line + trailerValueQuote
	}
	return out
}

// trailerTestRepo prepares a scratch git repository (off the `/` partition,
// through the suite's own scratch root) with an initial commit and an identity.
func trailerTestRepo(t *testing.T) string {
	t.Helper()
	dir := testutil.TestScratchDir(t, "trailer")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
		}
		return string(out)
	}
	run("init", "-q", ".")
	run("config", "user.email", "trailer@test.invalid")
	run("config", "user.name", "trailer test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	run("add", "seed.txt")
	run("commit", "-q", "-m", "seed")
	return dir
}

// TestGitCommitTaskTrailerRewrite is the REWRITE arm: the textual face, with
// no git process involved. Both lines are present in the environment, so every
// positive case pins the FULL injected text (the two lines travel as one
// --trailer argument).
func TestGitCommitTaskTrailerRewrite(t *testing.T) {
	t.Setenv(TaskRunIDEnv, trailerTestTaskID)
	want := trailerTestInjection(t)

	cases := []struct {
		name string
		in   string
		want string // "" means: unchanged
	}{
		{
			name: "plain commit gets both trailers",
			in:   `git commit -m "fix: thing"`,
			want: `git commit` + want + ` -m "fix: thing"`,
		},
		{
			name: "second link of a chain gets it too",
			in:   `git add -A && git commit -m "x"`,
			want: `git add -A && git commit` + want + ` -m "x"`,
		},
		{
			name: "git -C <dir> is still a git commit",
			in:   `git -C /tmp/repo commit -am "x"`,
			want: `git -C /tmp/repo commit` + want + ` -am "x"`,
		},
		{
			name: "a caller who passed its own --trailer keeps control (no duplication)",
			in:   `git commit -m "x" --trailer "task: someone-else"`,
			want: "",
		},
		{
			name: "--amend REWRITES an existing commit — never touched",
			in:   `git commit --amend --no-edit`,
			want: "",
		},
		{
			name: "a quoted string is DATA, not a command",
			in:   `echo "run git commit -m x later"`,
			want: "",
		},
		{
			name: "a heredoc body is DATA, not a command",
			in:   "cat <<'EOF' > /tmp/doc.md\ngit commit -m x\nEOF",
			want: "",
		},
		{
			name: "the word commit without git is not a commit",
			in:   `grep commit file.txt`,
			want: "",
		},
		{
			name: "another git subcommand is not a commit",
			in:   `git log --grep=commit`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, lines, injected := gitCommitTaskTrailerRewrite(tc.in)
			if tc.want == "" {
				if injected || got != tc.in {
					t.Fatalf("command was modified but must not be:\n in: %s\nout: %s", tc.in, got)
				}
				return
			}
			if !injected {
				t.Fatalf("nothing was injected; want\n  %s\ngot\n  %s", tc.want, got)
			}
			if got != tc.want {
				t.Fatalf("rewrite mismatch:\n want: %s\n  got: %s", tc.want, got)
			}
			if len(lines) != 2 {
				t.Fatalf("want 2 announced lines (ledger row + host), got %d: %v", len(lines), lines)
			}
		})
	}
}

// TestGitCommitHostTrailerSurvivesWithNoLedgerRow: the host line is INDEPENDENT
// of the ledger seam. A chat, a hand-run gopi, a `make`-spawned commit — none
// of them publishes GOPI_TASK_RUN_ID, and they are exactly the commits whose
// machine is unknowable today. The host line must still land, and the announce
// must name it.
func TestGitCommitHostTrailerSurvivesWithNoLedgerRow(t *testing.T) {
	t.Setenv(TaskRunIDEnv, "")
	hostLine := localHostTrailerLine()
	if hostLine == "" {
		t.Skipf("this machine cannot name itself (os.Hostname failed) — the skip is the mechanism working")
	}
	want := " " + gitCommitTrailerFlag + " " + trailerValueQuote + hostLine + trailerValueQuote

	got, lines, injected := gitCommitTaskTrailerRewrite(`git commit -m "x"`)
	if !injected {
		t.Fatalf("no injection with no ledger row; the host line must stand alone")
	}
	if wantGot := `git commit` + want + ` -m "x"`; got != wantGot {
		t.Fatalf("rewrite mismatch:\n want: %s\n  got: %s", wantGot, got)
	}
	if len(lines) != 1 || lines[0] != hostLine {
		t.Fatalf("announced lines = %v; want exactly [%q]", lines, hostLine)
	}
	// The ledger line must NOT be invented out of an empty variable.
	if strings.Contains(got, TaskTrailerKey+":") {
		t.Fatalf("a ledger trailer was invented with no row: %s", got)
	}
}

// TestGitCommitTrailersSkipOnlyUnspellableValues pins WHICH half is dropped
// when a value cannot be spelled safely: the offending line is skipped, the
// other still travels, and the command never gains a value that could close
// the single quote and become further command.
func TestGitCommitTrailersSkipOnlyUnspellableValues(t *testing.T) {
	const cmd = `git commit -m "x"`

	t.Run("hostile ledger id: host line still injected", func(t *testing.T) {
		t.Setenv(TaskRunIDEnv, "01a17857'; rm -rf /; echo '")
		hostLine := localHostTrailerLine()
		got, lines, injected := gitCommitTaskTrailerRewrite(cmd)
		if hostLine == "" {
			if injected || got != cmd {
				t.Fatalf("nothing is spellable here, so nothing may be injected: %s", got)
			}
			return
		}
		if !injected {
			t.Fatalf("the host line was dropped along with the unusable id")
		}
		if strings.Contains(got, "rm -rf") {
			t.Fatalf("the hostile id reached the command line: %s", got)
		}
		if len(lines) != 1 || lines[0] != hostLine {
			t.Fatalf("announced lines = %v; want exactly [%q]", lines, hostLine)
		}
	})

	t.Run("an unspellable host name is refused by HostTrailerLine", func(t *testing.T) {
		for _, host := range []string{
			"nick'; rm -rf /; echo '",
			"two words",
			"",
			"   ",
			"pad ",
		} {
			if line := HostTrailerLine(host); line != "" {
				t.Fatalf("host %q produced a line %q; want none", host, line)
			}
		}
	})

	t.Run("a blank ledger row invents nothing", func(t *testing.T) {
		t.Setenv(TaskRunIDEnv, "   ")
		got, _, _ := gitCommitTaskTrailerRewrite(cmd)
		if strings.Contains(got, TaskTrailerKey+":") {
			t.Fatalf("a ledger trailer was invented from a blank row: %s", got)
		}
	})
}

// TestGitCommitHostTrailerLineShape pins the rendered line and its key, so a
// reader that greps `host: <name>` (a person, a report, a triage script) and a
// writer that renders it cannot drift apart (Rules 1/13/14).
func TestGitCommitHostTrailerLineShape(t *testing.T) {
	if got, want := HostTrailerLine("peerbox"), HostTrailerKey+": peerbox"; got != want {
		t.Fatalf("HostTrailerLine = %q, want %q", got, want)
	}
	if HostTrailerKey != "host" {
		t.Fatalf("HostTrailerKey = %q; the documented spelling is %q", HostTrailerKey, "host")
	}
	// The two keys must differ, or `git log --grep` cannot separate the axes.
	if TaskTrailerKey == HostTrailerKey {
		t.Fatalf("the ledger key and the host key are the same word (%q)", TaskTrailerKey)
	}
}

// TestBashCommitCarriesTheLedgerReference is the GREP arm — the acceptance the
// epic states, executed for real: a commit made THROUGH THE BASH TOOL lands
// BOTH a `task: <id>` line and a `host: <hostname>` line, and `git log --grep`
// finds the commit by either one, with no other index.
//
// It also pins the negative half in the same test: a command that is NOT a git
// commit must not acquire the lines, so the greps are evidence of the injection
// and not of a blanket rewrite.
func TestBashCommitCarriesTheLedgerReference(t *testing.T) {
	dir := trailerTestRepo(t)
	t.Setenv(TaskRunIDEnv, trailerTestTaskID)
	hostLine := localHostTrailerLine()
	if hostLine == "" {
		t.Fatalf("this machine cannot name itself — os.Hostname failed, so the host arm cannot be measured")
	}

	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	// The injection is ANNOUNCED on the process's own stderr (Rule 15: a tool
	// never silently executes something other than what was asked) — the same
	// seam checkoutHomeStripRecord uses in bash.go. captureStderr is the package's
	// ONE helper for this (retired_merge_suite_survivors_test.go), reused rather
	// than re-implemented.
	var out string
	var err error
	notice := captureStderr(t, func() {
		out, err = BashInDir(`git add work.txt && git commit -m "feat: the work"`, dir)
	})
	if err != nil {
		t.Fatalf("bash commit failed: %v\n%s", err, out)
	}
	for _, line := range []string{TaskTrailerLine(trailerTestTaskID), hostLine} {
		if !strings.Contains(notice, line) {
			t.Fatalf("the run never announced %q; stderr was:\n%s", line, notice)
		}
	}

	// THE ACCEPTANCE: git's own index, nothing else consulted. Each axis is
	// grep-able ON ITS OWN, which is the whole point of two keys.
	headOut, herr := gitIn(dir, "rev-parse", "HEAD")
	if herr != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", herr, headOut)
	}
	head := strings.TrimSpace(headOut)
	for _, line := range []string{TaskTrailerLine(trailerTestTaskID), hostLine} {
		grepOut, gerr := gitIn(dir, "log", "--format=%H", "--grep="+line)
		if gerr != nil {
			t.Fatalf("git log --grep=%q: %v\n%s", line, gerr, grepOut)
		}
		if strings.TrimSpace(grepOut) == "" {
			t.Fatalf("git log --grep=%q matched NO commit — the trailer did not reach the commit body", line)
		}
		if got := strings.TrimSpace(grepOut); got != head {
			t.Fatalf("git log --grep=%q matched %q, want HEAD %q", line, got, head)
		}
	}

	// The body carries both lines, each as its OWN trailer line at the END of
	// the body — a reader (and `git interpret-trailers`) sees trailers, not
	// prose. The assertion is per-LINE, not per-substring: a space-joined
	// `task: X host: Y` contains both strings but carries neither reference,
	// because git wrote one line and each axis now belongs to the other's
	// value. Measured on git 2.43.0 — that spelling is why this check exists.
	bodyOut, berr := gitIn(dir, "log", "-1", "--format=%B")
	if berr != nil {
		t.Fatalf("log -1 --format=%%B: %v\n%s", berr, bodyOut)
	}
	bodyLines := map[string]bool{}
	for _, l := range strings.Split(bodyOut, "\n") {
		bodyLines[strings.TrimSpace(l)] = true
	}
	for _, line := range []string{TaskTrailerLine(trailerTestTaskID), hostLine} {
		if !bodyLines[line] {
			t.Fatalf("the body carries no LINE equal to %q — the two trailers were merged into one line:\n%s",
				line, bodyOut)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(bodyOut), hostLine) {
		t.Fatalf("host trailer is not the body's last line:\n%s", bodyOut)
	}

	// NEGATIVE HALF: a command that creates no commit gets no trailer, so the
	// greps above are evidence of the injection and not of a blanket rewrite.
	if _, serr := BashInDir(`git status --porcelain`, dir); serr != nil {
		t.Fatalf("bash status failed: %v", serr)
	}
	statusOut, _ := gitIn(dir, "log", "--format=%H", "--grep="+hostLine) //stylegate:ignore a log read in a fixture whose 1-commit assertion below fails on an empty result, so a git failure cannot pass
	if n := len(strings.Fields(statusOut)); n != 1 {
		t.Fatalf("want exactly 1 commit carrying the host reference, got %d:\n%s", n, statusOut)
	}
}

// TestBashCommitHostTrailerWithoutLedgerRow is the same REAL run with the
// ledger seam unset — the chat / hand-run case. The commit must still be
// attributable to this machine.
func TestBashCommitHostTrailerWithoutLedgerRow(t *testing.T) {
	dir := trailerTestRepo(t)
	t.Setenv(TaskRunIDEnv, "")
	hostLine := localHostTrailerLine()
	if hostLine == "" {
		t.Skipf("this machine cannot name itself (os.Hostname failed) — the skip is the mechanism working")
	}

	if err := os.WriteFile(filepath.Join(dir, "solo.txt"), []byte("solo\n"), 0o600); err != nil {
		t.Fatalf("write solo.txt: %v", err)
	}
	var out string
	var err error
	notice := captureStderr(t, func() {
		out, err = BashInDir(`git add solo.txt && git commit -m "feat: no ledger row here"`, dir)
	})
	if err != nil {
		t.Fatalf("bash commit failed: %v\n%s", err, out)
	}
	if !strings.Contains(notice, hostLine) {
		t.Fatalf("the run never announced %q; stderr was:\n%s", hostLine, notice)
	}
	grepOut, gerr := gitIn(dir, "log", "--format=%H", "--grep="+hostLine)
	if gerr != nil {
		t.Fatalf("git log --grep=%q: %v\n%s", hostLine, gerr, grepOut)
	}
	if strings.TrimSpace(grepOut) == "" {
		t.Fatalf("git log --grep=%q matched NO commit — the host line did not reach the body", hostLine)
	}
	bodyOut, berr := gitIn(dir, "log", "-1", "--format=%B")
	if berr != nil {
		t.Fatalf("log -1 --format=%%B: %v\n%s", berr, bodyOut)
	}
	if strings.Contains(bodyOut, TaskTrailerKey+":") {
		t.Fatalf("a ledger trailer was invented with no row:\n%s", bodyOut)
	}
	if !strings.HasSuffix(strings.TrimSpace(bodyOut), hostLine) {
		t.Fatalf("host trailer is not the body's last line:\n%s", bodyOut)
	}
}
