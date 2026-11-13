package testutil

// The launcher's INSTALLATION contract (defect found 2026-09-12, epic
// worker-log-session-attribution follow-up).
//
// The operator's shell does not run scripts/gopi-worker-up.sh — it runs the
// INSTALLED copy (~/.local/bin/gopi-worker-up.sh, symlinked from ~/bin/).
// On this host that copy was still the PRE-rename build: sha256 b2cd1ef0…
// (the blob at 776cb789^, 4232 bytes), which starts workers as
// `setsid nohup "$bin" worker > "$LOG" 2>&1` — no `--worker-log-root`, no
// GOPI_WORKER_LOG_ROOT — while the repo's copy carried the attribution root
// (sha256 4b2ad5a2…). Every worker the OPERATOR started therefore ran with
// per-task output attribution OFF and said nothing about it: the Rule 15
// silent-degradation class, live on the host.
//
// WHY THIS IS A GATE AND NOT A ONE-OFF: the drift is invisible to every test
// that drives the REPO script — and five of them do (gopi_home_hygiene_test.go
// drives the attribution contract, procname_gate_test.go drives --count,
// glm_night_watch_test.go drives the delegation) — because none of them ever
// looks at the copy that actually starts workers. `--install` is the seam that
// keeps the two in step (the install-scratch-gc-watchdog.sh precedent: the
// installed launcher must not depend on a worktree, which comes and goes), so
// that seam is what is asserted here, with an isolated HOME.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// installDirRel is --install's stable directory, relative to HOME
// (`~/.local/bin/<name>`). The directory it re-points at the stable copy is
// `<HOME>/bin/<name> -> <HOME>/.local/bin/<name>`, which reuses this package's
// ONE binDirName (defined in glm_night_watch_test.go) — the same directory name
// as the launcher's own `<repo>/bin`, so it is referenced, never re-declared
// (Rule 13/14: a second `bin` literal in this package is drift waiting to happen).
const installDirRel = ".local/bin"

// gopiWorkerOutputOption is the option the launcher no longer accepts and must
// now REFUSE (epic child-stream-task-output). It is spelled here rather than
// imported because no production constant defines it any more — the launcher's
// own refusal message is the only place the retired spelling survives.
const gopiWorkerOutputOption = gopiWorkerRetiredLogRootFlag

// launcherInstallEnv returns the child environment for an --install run: an
// ISOLATED HOME (the whole point — the real ~/.local/bin must never be touched
// by a test) plus PATH for bash's own lookups. It is built explicitly rather
// than appended to os.Environ(), so an ambient HOME cannot shadow the sandbox
// one (a duplicate HOME key leaves the winner to getenv's first-match rule).
func launcherInstallEnv(t *testing.T, home string) []string {
	t.Helper()
	env := []string{"HOME=" + home}
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	return env
}

// sha256Of is the identity of a file's bytes — the same measure the incident's
// diagnosis used on both sides of the drift.
func sha256Of(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// TestWorkerUpInstallIsByteIdenticalToTheRepoScript is the regression test for
// the stale-installed-launcher defect: after `--install` the stable copy must
// carry the REPO script's bytes exactly, and the ~/bin entry must point at that
// copy (not at a worktree, and not at a stale earlier install).
//
// Byte-identity is the whole contract. A merge that changes the launcher
// without re-running `--install` is precisely how the host ended up starting
// workers with attribution OFF, so the gate compares content, not "it runs".
func TestWorkerUpInstallIsByteIdenticalToTheRepoScript(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	src := filepath.Join(root, scriptsDirName, gopiWorkerUpScript)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("%s/%s is missing: %v", scriptsDirName, gopiWorkerUpScript, err)
	}
	home := t.TempDir()
	// The operator's shape: ~/bin exists and is what --install re-points. It
	// is created first because --install only links when the directory is
	// there (a launcher must not invent a directory in a stranger's home).
	if err := os.MkdirAll(filepath.Join(home, binDirName), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDirName, err)
	}

	cmd := exec.Command("bash", src, "--install")
	cmd.Env = launcherInstallEnv(t, home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--install failed: %v\n%s", err, out)
	}

	installed := filepath.Join(home, filepath.FromSlash(installDirRel), gopiWorkerUpScript)
	gotBytes, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("--install must write %s: %v\n%s", installed, err, out)
	}
	wantBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("the installed launcher is NOT the repo script's bytes — this is the stale-installed-launcher defect "+
			"(the operator runs the installed copy, so a drift here silently changes how every worker starts):\n"+
			"  installed %s sha256=%s (%d bytes)\n  repo      %s sha256=%s (%d bytes)",
			installed, sha256Of(t, installed), len(gotBytes),
			src, sha256Of(t, src), len(wantBytes))
	}

	// Executable: a non-executable copy is an installed launcher that cannot
	// have started anything (the operator's start would fail, not degrade).
	if fi, err := os.Stat(installed); err != nil {
		t.Fatalf("stat %s: %v", installed, err)
	} else if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable: mode %v", installed, fi.Mode().Perm())
	}

	// The ~/bin entry is a SYMLINK to the stable copy — the shape that keeps
	// the installed launcher independent of a worktree that comes and goes.
	link := filepath.Join(home, binDirName, gopiWorkerUpScript)
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("%s must be a symlink to %s (%v)", link, installed, err)
	}
	if target != installed {
		t.Errorf("%s -> %s, want %s (the stable copy)", link, target, installed)
	}
}

// retiredLauncherKnob / retiredLauncherEnvKnob are the output-root knobs the
// launcher's contract RENAMED away from and has now RETIRED entirely. Neither
// may come back anywhere in the script: a knob spelling is a supported way to
// set the output location (Rule 21's defect, in shell — an env var that is
// documented and read by nobody), and the destination now belongs to the row.
const (
	retiredLauncherKnob    = "GOPI_WORKER_LOG_DIR"
	retiredLauncherEnvKnob = "GOPI_WORKER_LOG_ROOT"
)

// retiredPositionalArg is the retired positional log-directory argument. It is
// asserted against the USAGE LINE rather than the whole file: the header
// deliberately DOCUMENTS the retirement ("the old positional [logdir] is
// GONE"), and a gate that flagged the documentation of a removal would be red
// on the day it landed. What must not carry it is the advertised contract —
// the Usage line is what tells an operator how to call the launcher.
const retiredPositionalArg = "[logdir]"

// TestWorkerUpCarriesNoRetiredLauncherForms pins the retirement as COMPLETE:
// the launcher has NO output-location knob at all any more (epic
// child-stream-task-output) — neither the renamed-away GOPI_WORKER_LOG_DIR, nor
// the retired GOPI_WORKER_LOG_ROOT / --worker-log-root, nor a positional log
// dir advertised in its usage line. Each of those reads as a supported way to
// set where a task's output goes, and there is nothing left to set: the
// destination is derived from the row at the spawn seam.
func TestWorkerUpCarriesNoRetiredLauncherForms(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, scriptsDirName, gopiWorkerUpScript))
	if err != nil {
		t.Fatalf("read %s: %v", gopiWorkerUpScript, err)
	}
	body := string(raw)
	if strings.Contains(body, retiredLauncherKnob) {
		t.Errorf("%s carries the retired knob %q — the output root is spelled GOPI_WORKER_LOG_ROOT "+
			"(the live name), and a second name is a setting nothing reads", gopiWorkerUpScript, retiredLauncherKnob)
	}
	usage := usageLine(body)
	if usage == "" {
		t.Fatalf("%s has no usage line — this gate reads the advertised contract from it, so its absence "+
			"would make the assertion below vacuous", gopiWorkerUpScript)
	}
	if strings.Contains(usage, retiredPositionalArg) {
		t.Errorf("%s still advertises the retired positional log dir in its usage line:\n  %s\n"+
			"a task's output goes to <gopi-dir>/sessions/<task-id>/output.log; no positional names it",
			gopiWorkerUpScript, usage)
	}
	// Neither retired env spelling may be ADVERTISED (the header may explain
	// what was retired and why — that is the record, not a contract).
	for _, knob := range []string{retiredLauncherKnob, retiredLauncherEnvKnob} {
		if strings.Contains(usage, knob) {
			t.Errorf("%s advertises the retired output knob %s in its usage line:\n  %s", gopiWorkerUpScript, knob, usage)
		}
	}
	// The RETIRED output option must not be advertised as live either — it may
	// appear only as the refusal's own text (named so an old caller learns why).
	if strings.Contains(usage, gopiWorkerOutputOption) {
		t.Errorf("%s still advertises %s in its usage line:\n  %s\n"+
			"a task's output goes to <gopi-dir>/sessions/<task-id>/output.log; there is no root to pass",
			gopiWorkerUpScript, gopiWorkerOutputOption, usage)
	}
	// The live destination must be stated, or the absence assertions above
	// could pass on an empty/renamed-away contract.
	for _, want := range []string{taskOutputLogSuffixDryRun} {
		if !strings.Contains(body, want) {
			t.Errorf("%s no longer carries %q — the output contract's own spelling is missing, so "+
				"the retired-form assertions above prove nothing", gopiWorkerUpScript, want)
		}
	}
}

// runLauncher runs the launcher with a sandbox HOME and returns its output and
// exit status. The status matters: the check seam's contract is an EXIT CODE
// (0 identical / 1 stale / 2 absent), not a line of prose.
func runLauncher(t *testing.T, src, home string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{src}, args...)...)
	cmd.Env = launcherInstallEnv(t, home)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %s %v: %v\n%s", src, args, err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// launcherContractFragment is the launcher's OWN refusal arm for the retired
// output option — the line that exists only because the output contract
// changed (epic child-stream-task-output). It anchors the drift mutation below:
// an installed copy WITHOUT that arm is a launcher written against the old
// contract, which is the incident's own shape (a stale installed copy whose
// worker-start path no longer matches the repo's).
//
// The shell spelling is built from the constant so the fragment cannot drift
// from the script (the mutation fails loudly if the text is not found, so a
// rename here can never make the fixture vacuous).
const launcherContractFragment = gopiWorkerOutputOption + "|" + gopiWorkerOutputOption + "=*)"

// TestWorkerUpCheckReportsStaleInstalledCopy is the regression test for the
// reported defect, and it is the one that FAILS against the pre-fix installed
// bytes: the check seam must go LOUD when the installed copy is not this
// script's bytes, because the installed copy is what actually starts the
// operator's workers.
//
// The mutation is the incident's own shape, not a random byte flip: the
// installed copy is rewritten to start workers WITHOUT the attribution flag —
// exactly how the stale copy on this host differed (it predated the
// --worker-log-root contract), so the drift silently turned per-task output
// attribution OFF. A launcher that reports that as fine is the defect class
// this asserts against.
func TestWorkerUpCheckReportsStaleInstalledCopy(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	src := filepath.Join(root, scriptsDirName, gopiWorkerUpScript)
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, binDirName), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDirName, err)
	}
	installed := filepath.Join(home, filepath.FromSlash(installDirRel), gopiWorkerUpScript)

	// (a) Nothing installed: "nothing to audit" must be its OWN non-zero
	// status — an operator who never ran --install is not running a stale
	// launcher, they are running the repo's (or nothing at all).
	if out, code := runLauncher(t, src, home, "--check"); code != 2 {
		t.Errorf("--check with no installed copy: exit %d, want 2 (distinct from \"stale\"):\n%s", code, out)
	}

	// (b) Freshly installed: byte-identical, so the audit passes.
	if out, code := runLauncher(t, src, home, "--install"); code != 0 {
		t.Fatalf("--install: exit %d\n%s", code, out)
	}
	if out, code := runLauncher(t, src, home, "--check"); code != 0 {
		t.Fatalf("--check right after --install must pass: exit %d\n%s", code, out)
	}

	// (c) THE DEFECT: the installed copy drifts (here: the attribution flag is
	// dropped, the incident's shape). The check must fail loudly and NAME both
	// identities, so the operator can see WHICH copy is behind.
	drifted := strings.Replace(string(raw), launcherContractFragment, "--probe-stale-option)", 1)
	if drifted == string(raw) {
		t.Fatalf("the fixture could not drop %q from %s — the drift mutation would be vacuous",
			launcherContractFragment, gopiWorkerUpScript)
	}
	if err := os.WriteFile(installed, []byte(drifted), 0o755); err != nil {
		t.Fatalf("write drifted copy: %v", err)
	}
	out, code := runLauncher(t, src, home, "--check")
	if code == 0 {
		t.Errorf("--check passed against a drifted installed copy — this is the silent-degradation defect: the "+
			"operator's workers would start under a contract nothing announced\n%s", out)
	}
	for _, want := range []string{"STALE", "--install"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stale report must carry %q (what is wrong, and the one command that fixes it):\n%s", want, out)
		}
	}
	// The drifted copy is a DIFFERENT identity: the report has to make the
	// mismatch measurable, not just assert a boolean.
	if sha256Of(t, installed) == sha256Of(t, src) {
		t.Errorf("the fixture did not actually change the installed copy's bytes")
	}
	if !strings.Contains(out, sha256Of(t, installed)[:16]) {
		t.Errorf("the report must name the installed copy's own digest, so the two sides can be compared:\n%s", out)
	}

	// (d) --install is the repair, and it is enough on its own.
	if _, code := runLauncher(t, src, home, "--install"); code != 0 {
		t.Fatalf("re-install after drift: exit %d", code)
	}
	if out, code := runLauncher(t, src, home, "--check"); code != 0 {
		t.Errorf("--check must pass again after --install repaired the drift: exit %d\n%s", code, out)
	}
}

// usageLine returns the launcher's advertised usage line (the header line
// beginning with "# Usage:"), or "" when the header carries none.
func usageLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, usageLinePrefix) {
			return line
		}
	}
	return ""
}

// usageLinePrefix marks the launcher's usage line inside its header comment.
const usageLinePrefix = "# Usage:"
