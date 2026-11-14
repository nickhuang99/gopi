package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

// Exit-status prefixes of the Bash result string ("exit=N\n", bash.go). Named
// because for the escape-hatch test that field IS the evidence, and a bare
// "exit=0" literal hides what is being read (Rule 1); the bare "exit=" anchor
// has a precedent in bash_andlist_exit_status_test.go (andListExitPrefix).
const (
	bashExitPrefixZero = "exit=0"
	bashExitPrefixOne  = "exit=1"
)

// bashExitField returns the status field of a Bash result — the line a caller
// reads to learn the TESTED command's status. It never slices past the end: the
// result is the status line plus the command's raw stdout, so the string can be
// as short as the prefix itself ("exit=127\n" is 9 bytes) and its last line need
// not end in a newline at all (measured: `printf x; false 2>/dev/null` ->
// "exit=1\nx"). A redirect of stderr does NOT shorten the prefix line — measured
// `false 2>/dev/null` -> "exit=1\n", exactly as without it.
func bashExitField(out string) string {
	line, _, _ := strings.Cut(out, "\n")
	return line
}

// Diagnostic windows for this file's assertion messages.
//
// tools.Bash answers a command with NO stdout with a SHORT result — measured on
// this host: "exit=0\n" (7 bytes) for `true`, "exit=1\n" (7 bytes) for `false`,
// "exit=127\n" (9 bytes) for a missing command, because the result is the
// exit=N prefix line plus whatever the command printed (internal/tools/bash.go,
// buildOutput). A diagnostic that slices the raw result by a FIXED COLUMN
// WIDTH therefore panics on slice bounds at the exact moment the assertion
// fails — replacing the real diagnostic with a bounds stack (Rule 15/31). Every
// window below is bounded by the VALUE'S OWN length, so a short result yields
// the whole result and can never panic, i.e. out[:min(len(out), N)] for a head
// window and out[max(0, len(out)-N):] for a tail window.
const (
	// bashDiagWindowBytes / bashDiagTailBytes size a DIAGNOSTIC slice, not a
	// behaviour limit (the tool's own output cap is maxOutputSize, elsewhere).
	bashDiagWindowBytes = 200
	bashDiagTailBytes   = 120
	// bashDiagHeadProbeBytes is the small head window (what follows the exit=N
	// prefix). Its FIXED form — out[:bashDiagHeadProbeBytes] — is the defect
	// this file's sites carried: the regression test at the end of the file
	// still proves that shape PANICS on a short result, so it cannot come back
	// unnoticed.
	bashDiagHeadProbeBytes = 40
	// bashDiagScannedFiles is the number of files the guard's static arm covers
	// (this file + its two bash-surface siblings). The scan asserts the COUNT it
	// read against it, so a table that silently lost a file — the way a widened
	// guard rots — is reported rather than passing as a clean scan (Rule 15).
	bashDiagScannedFiles = 3
)

func TestBashEcho(t *testing.T) {
	output, err := Bash("echo hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(output, "exit=0\n") {
		t.Errorf("expected output to start with 'exit=0\\n', got: %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("expected output to contain 'hello world', got: %q", output)
	}
}

func TestBashCommandFailure(t *testing.T) {
	// exit 1 is a valid command execution — the exit code is in the result.
	output, err := Bash("exit 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(output, "exit=1\n") {
		t.Errorf("expected output to start with 'exit=1\\n', got: %q", output)
	}
}

func TestBashCommandTimeout(t *testing.T) {
	output, err := bash("sleep 2", 1*time.Millisecond, 30*time.Second)
	if err == nil {
		t.Fatal("expected timeout error for sleep 2 with 1ms timeout, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error message, got: %v", err)
	}
	if !strings.Contains(output, "timed out") {
		t.Errorf("expected output to mention timeout, got: %q", output)
	}
}

func TestBashWithTimeout_PositiveKills(t *testing.T) {
	// timeout_sec=1 on a 2s silent sleep → absolute timeout fires at ~1s.
	start := time.Now()
	output, err := BashWithTimeout("sleep 2", 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error for sleep 2 with timeout_sec=1, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
	if !strings.Contains(output, "timed out") {
		t.Errorf("expected 'timed out' in output, got: %q", output)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took too long: %v (expected ~1s)", elapsed)
	}
}

func TestBashWithTimeout_ZeroUnlimited(t *testing.T) {
	// timeout_sec=0 = unlimited absolute time. A 2s sleep must run to
	// completion (it would have been killed at 60s under the old framework
	// default — the point is the deadline simply does not exist). The 60s
	// idle safety net does not fire because the command finishes first.
	start := time.Now()
	output, err := BashWithTimeout("sleep 2", 0)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error with timeout_sec=0: %v", err)
	}
	if !strings.HasPrefix(output, "exit=0\n") {
		t.Errorf("expected exit=0, got: %q", output)
	}
	if elapsed < 1500*time.Millisecond {
		t.Errorf("sleep 2 finished too fast: %v (expected ~2s)", elapsed)
	}
}

func TestBashWithTimeout_NegativeFallsBackToDefault(t *testing.T) {
	// Negative values are invalid → fall back to the default (BashTimeout(),
	// 120s unless GOPI_BASH_TIMEOUT_SEC overrides). A 2s sleep must complete.
	output, err := BashWithTimeout("sleep 2", -5)
	if err != nil {
		t.Fatalf("unexpected error with timeout_sec=-5: %v", err)
	}
	if !strings.HasPrefix(output, "exit=0\n") {
		t.Errorf("expected exit=0, got: %q", output)
	}
}

func TestBashWithTimeout_IdleKillUnderUnlimited(t *testing.T) {
	// timeout_sec=0 removes the absolute deadline, but the idle-output safety
	// net must still fire on a silent stuck process. Exercised at the
	// internal level with a 1s idle (BashWithTimeout uses the configured 60s
	// default — too long for a unit test).
	start := time.Now()
	output, err := bash("sleep 60", 0, 1*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected idle timeout error under unlimited absolute timeout, got nil")
	}
	if !strings.Contains(err.Error(), "no output for") {
		t.Errorf("expected 'no output for' in error, got: %v", err)
	}
	if !strings.Contains(output, "no output for") {
		t.Errorf("expected 'no output for' in output, got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
	if elapsed > 10*time.Second {
		t.Errorf("idle timeout took too long: %v (expected ~1s)", elapsed)
	}
}

func TestBashWithTimeout_UnlimitedFiniteCommand(t *testing.T) {
	// Internal level: abs=0 (unlimited) with a finite command and a generous
	// idle timeout → completes normally with exit=0.
	output, err := bash("echo unlimited-ok", 0, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error under unlimited timeout: %v", err)
	}
	if !strings.Contains(output, "unlimited-ok") {
		t.Errorf("expected output, got: %q", output)
	}
}

func TestBashCommandNotFound(t *testing.T) {
	output, err := Bash("nonexistent_command_xyz_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// exit code depends on the shell; must be non-zero.
	if strings.HasPrefix(output, "exit=0\n") {
		t.Errorf("expected non-zero exit for unknown command, got: %q", output)
	}
	if !strings.Contains(output, "[stderr]") {
		t.Errorf("expected stderr capture for unknown command, got: %q", output)
	}
}

func TestBashOutputTruncation(t *testing.T) {
	// 20KB of output → soft cap (16KB) with the narrowing hint.
	soft, err := Bash("head -c 20480 /dev/zero | tr '\\0' 'x'")
	if err != nil {
		t.Fatalf("Bash: %v", err)
	}
	if len(soft) > softOutputSize+120 { // 16384 + short hint suffix
		t.Fatalf("expected soft truncation at %d bytes, got %d", softOutputSize, len(soft))
	}
	if !strings.Contains(soft, "narrow the command with grep/head/sed") {
		t.Fatalf("expected narrowing hint in truncated output")
	}

	// dd if=/dev/zero bs=1024 count=500 produces ~500KB → must be truncated.
	output, err := Bash("dd if=/dev/zero bs=1024 count=500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "truncated") {
		t.Errorf("expected truncation warning for large output, got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
	if len(output) > maxOutputSize+200 {
		t.Errorf("output too large after truncation: %d bytes", len(output))
	}
}

func TestBashEmptyCommand(t *testing.T) {
	_, err := Bash("")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in error message, got: %v", err)
	}

	_, err = Bash("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only command")
	}
}

func TestBashStderrCapture(t *testing.T) {
	output, err := Bash("echo stdout; echo stderr >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "stdout") {
		t.Errorf("expected stdout in output, got: %q", output)
	}
	if !strings.Contains(output, "[stderr] stderr") {
		t.Errorf("expected [stderr] prefix on stderr lines, got: %q", output)
	}
}

func TestBashTildeExpansion(t *testing.T) {
	output, err := Bash("echo ~/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(output, "~/") {
		t.Errorf("tilde was not expanded in output: %q", output)
	}
}

// TestBashRejectDangerousByDefault asserts the SECURITY default: RejectDangerous
// is on unless explicitly turned off, so dangerous commands are rejected
// without any opt-in. The old safe-default (RejectDangerous=false) is gone.
func TestBashRejectDangerousByDefault(t *testing.T) {
	// RejectDangerous is true by default; force it on for determinism.
	RejectDangerous = true
	defer func() { RejectDangerous = true }()
	savedEscape := escapeDangerous
	escapeDangerous = false
	defer func() { escapeDangerous = savedEscape }()

	_, err := Bash("sudo rm -rf /")
	if err == nil {
		t.Fatal("expected error for dangerous command")
	}
	if !strings.Contains(err.Error(), "dangerous") {
		t.Errorf("expected 'dangerous' in error message, got: %v", err)
	}
}

// TestBashDangerousOutOfCwdRejected verifies the cwd whitelist: a dangerous
// command whose target resolves OUTSIDE the working directory is rejected,
// even though the pattern itself matched.
func TestBashDangerousOutOfCwdRejected(t *testing.T) {
	RejectDangerous = true
	defer func() { RejectDangerous = true }()
	savedEscape := escapeDangerous
	escapeDangerous = false
	defer func() { escapeDangerous = savedEscape }()

	// Absolute path to the filesystem root / another dir: outside any cwd.
	_, err := BashInDir("rm -rf /", t.TempDir())
	if err == nil {
		t.Fatal("rm -rf / must be rejected — target is outside the working dir")
	}
	_, err = BashInDir("rm -rf /tmp/other", t.TempDir())
	if err == nil {
		t.Fatal("rm -rf /tmp/other must be rejected — absolute target outside cwd")
	}
	_, err = BashInDir("mkfs.ext4 /dev/sda", t.TempDir())
	if err == nil {
		t.Fatal("mkfs on a device must be rejected")
	}
	_, err = BashInDir("dd if=/dev/sda of=/dev/null", t.TempDir())
	if err == nil {
		t.Fatal("dd reading a raw disk device must be rejected")
	}
	_, err = BashInDir("sudo whoami", t.TempDir())
	if err == nil {
		t.Fatal("sudo must be rejected — not whitelistable by cwd")
	}
}

// TestBashDangerousInCwdAllowed verifies the whitelist lets a dangerous
// command through when its target stays inside the working directory
// (a legitimate "clean the build dir" cleanup).
func TestBashDangerousInCwdAllowed(t *testing.T) {
	RejectDangerous = true
	defer func() { RejectDangerous = true }()
	savedEscape := escapeDangerous
	escapeDangerous = false
	defer func() { escapeDangerous = savedEscape }()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := BashInDir("rm -rf ./build/", dir)
	if err != nil {
		t.Fatalf("in-cwd rm -rf ./build/ should be allowed: %v", err)
	}
	if !strings.HasPrefix(out, "exit=0") {
		t.Errorf("expected exit=0 for allowed cleanup, got %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("build dir should have been removed by the allowed command")
	}
}

// TestBashEscapeHatchAllowsDangerous verifies GOPI_ALLOW_DANGEROUS: with the
// escape hatch open, otherwise-rejected dangerous commands run (legit
// user-initiated cleanup / disk operations).
func TestBashEscapeHatchAllowsDangerous(t *testing.T) {
	RejectDangerous = true
	defer func() { RejectDangerous = true }()
	savedEscape := escapeDangerous
	escapeDangerous = true
	defer func() { escapeDangerous = savedEscape }()

	// A harmless command that the guard rejects on its own: "sudo " is the
	// non-whitelistable pattern (dangerousPatterns, bash.go), and `sudo -n true`
	// never prompts. NO masking tail: this test's evidence is the TESTED
	// command's own exit status, and `|| true` would replace it with the tail's
	// 0 — the assertion below would then hold even when `sudo` does not exist at
	// all (measured: `nosuchcmd 2>/dev/null || true` prints exit=0), i.e. it
	// could no longer tell "the dangerous command ran" from "it was never
	// found". The guard refuses by returning a Go error ("dangerous command
	// rejected"), so err == nil is the load-bearing half.
	//
	// The status field is truncated at its newline on purpose: the result is the
	// status line followed by the command's raw stdout, whose tail need not end
	// in a newline at all (measured: `printf x; false 2>/dev/null` ->
	// "exit=1\nx"), so HasPrefix on the field is the honest anchor.
	// TestBashCommandNotFound above asserts the same prefix shape.
	out, err := Bash("sudo -n true 2>/dev/null")
	if err != nil {
		t.Fatalf("escape hatch should allow sudo-prefixed command: %v", err)
	}
	if _, sudoErr := exec.LookPath("sudo"); sudoErr != nil {
		// No sudo on this host: the tested command is then a name that exists
		// nowhere, and the whole point of dropping the tail is that the status
		// SAYS so rather than reporting a success it never had.
		if strings.HasPrefix(out, bashExitPrefixZero) {
			t.Errorf("no sudo on PATH, yet the unmasked status reads exit=0: %q", bashExitField(out))
		}
		return
	}
	if !strings.HasPrefix(out, bashExitPrefixZero) && !strings.HasPrefix(out, bashExitPrefixOne) {
		t.Errorf("expected the tested command's own status (exit=0 passwordless sudo | exit=1 sudo refused), got %q", bashExitField(out))
	}
}

// TestBashMaskingTailHidesTheTestedStatus is the minimal repro of the defect
// this file's escape-hatch test carried: with a `|| true` (or `| true`, `; true`)
// tail, the exit=N prefix reports the TAIL's status, so an assertion of exit=0
// no longer reads the tested command at all. The host is irrelevant here — the
// tested command is a name that exists nowhere — which is exactly why the shape
// is dangerous: its status can only ever be the tail's.
func TestBashMaskingTailHidesTheTestedStatus(t *testing.T) {
	// A command that exists nowhere: its own status is 127, never 0.
	const noSuchCommand = "gopi_no_such_command_01a12eab"

	masked, err := Bash(noSuchCommand + " 2>/dev/null || true")
	if err != nil {
		t.Fatalf("masked shape should not error: %v", err)
	}
	if !strings.HasPrefix(masked, bashExitPrefixZero) {
		t.Fatalf("the mask must report the tail's status; got %q", bashExitField(masked))
	}

	unmasked, err := Bash(noSuchCommand + " 2>/dev/null")
	if err != nil {
		t.Fatalf("unmasked shape should not error: %v", err)
	}
	if strings.HasPrefix(unmasked, bashExitPrefixZero) {
		t.Fatalf("unmasked shape must NOT report success for a command that does not exist; got %q", bashExitField(unmasked))
	}
}

// TestBash_TruncationKeepsTail verifies the tail-retention semantics of the
// output caps (dsh bash render): the exit=N prefix stays first, the
// truncation marker is visible, and the LAST lines of a large output survive
// the soft cap — the failure lines a model needs to debug are at the end.
func TestBash_TruncationKeepsTail(t *testing.T) {
	out, err := Bash("seq 1 3000")
	if err != nil {
		t.Fatalf("Bash: %v", err)
	}
	if !strings.HasPrefix(out, "exit=0\n") {
		t.Fatalf("expected exit=0 prefix, got %q", out[:min(len(out), bashDiagHeadProbeBytes)])
	}
	if !strings.Contains(out, "[output truncated") {
		t.Fatalf("expected truncation marker, got tail: %q", out[max(0, len(out)-bashDiagTailBytes):])
	}
	if !strings.Contains(out, "3000") {
		t.Fatalf("expected the LAST line (3000) to survive truncation, got tail: %q", out[max(0, len(out)-bashDiagTailBytes):])
	}
	if strings.Contains(out, "1\n2\n") {
		t.Fatalf("expected the HEAD to be trimmed away, got head: %q", out)
	}
}

// TestBashInDir_ChangesDirectory verifies the workdir parameter actually runs
// the command in the given directory (pwd reflects it).
func TestBashInDir_ChangesDirectory(t *testing.T) {
	dir := t.TempDir()

	output, err := BashInDir("pwd", dir)
	if err != nil {
		t.Fatalf("BashInDir: %v", err)
	}
	if !strings.HasPrefix(output, "exit=0\n") {
		t.Errorf("expected exit=0, got: %q", output[:min(len(output), bashDiagHeadProbeBytes)])
	}
	if !strings.Contains(output, dir) {
		t.Errorf("pwd output %q should contain workdir %q", output, dir)
	}
}

// TestBashInDir_TildeWorkdir verifies ~/ expansion applies to the workdir.
func TestBashInDir_TildeWorkdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	output, err := BashInDir("pwd", "~/sub")
	if err != nil {
		t.Fatalf("BashInDir with ~/ workdir: %v", err)
	}
	if !strings.Contains(output, filepath.Join(dir, "sub")) {
		t.Errorf("pwd output %q should contain expanded workdir %q", output, filepath.Join(dir, "sub"))
	}
}

// TestBashInDir_MissingWorkdirErrors verifies a nonexistent workdir is
// rejected before the command runs.
func TestBashInDir_MissingWorkdirErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	output, err := BashInDir("pwd", missing)
	if err == nil {
		t.Fatal("expected error for nonexistent workdir, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention the missing workdir, got: %v", err)
	}
	_ = output
}

// TestBashInDir_FileWorkdirErrors verifies a workdir that is a regular file
// (not a directory) is rejected.
func TestBashInDir_FileWorkdirErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := BashInDir("pwd", file)
	if err == nil {
		t.Fatal("expected error for a file workdir, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should say 'not a directory', got: %v", err)
	}
}

// TestBashInDir_EmptyWorkdirDefaultsToCwd verifies the default behavior is
// unchanged: empty workdir behaves exactly like Bash (current directory).
func TestBashInDir_EmptyWorkdirDefaultsToCwd(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	plain, err := Bash("pwd")
	if err != nil {
		t.Fatalf("Bash: %v", err)
	}
	inDir, err := BashInDir("pwd", "")
	if err != nil {
		t.Fatalf("BashInDir with empty workdir: %v", err)
	}
	if plain != inDir {
		t.Errorf("empty workdir should behave like Bash: %q vs %q", inDir, plain)
	}
	if !strings.Contains(plain, cwd) {
		t.Errorf("pwd output %q should contain cwd %q", plain, cwd)
	}
}

// TestBashWithTimeoutInDir_Combined verifies workdir composes with the
// timeout_sec semantics (the dispatch path used by the agent loop).
func TestBashWithTimeoutInDir_Combined(t *testing.T) {
	dir := t.TempDir()

	output, err := BashWithTimeoutInDir("pwd", 10, dir)
	if err != nil {
		t.Fatalf("BashWithTimeoutInDir: %v", err)
	}
	if !strings.Contains(output, dir) {
		t.Errorf("pwd output %q should contain workdir %q", output, dir)
	}

	// timeout_sec=0 (unlimited) still honors the workdir.
	output, err = BashWithTimeoutInDir("pwd", 0, dir)
	if err != nil {
		t.Fatalf("BashWithTimeoutInDir timeout_sec=0: %v", err)
	}
	if !strings.Contains(output, dir) {
		t.Errorf("pwd output %q should contain workdir %q", output, dir)
	}
}

// TestBashDangerousRejectedTable locks the rejection of the core dangerous
// patterns with the guard on: each must be refused BEFORE execution. The
// table covers the whitelist's escape hatches too — flag tricks, cd escapes,
// ~/ targets and out-of-cwd absolute targets.
func TestBashDangerousRejectedTable(t *testing.T) {
	prev := RejectDangerous
	RejectDangerous = true
	defer func() { RejectDangerous = prev }()
	savedEscape := escapeDangerous
	escapeDangerous = false
	defer func() { escapeDangerous = savedEscape }()

	for _, cmd := range []string{
		"rm -rf /",
		"rm -rf /tmp/other",
		"rm -rf ../../etc",               // relative traversal out of cwd
		"rm -rf ~/build",                 // home dir is never the workdir
		"cd /tmp && rm -rf build",        // cd escape must not defeat the whitelist
		"sudo apt install x",             // privilege escalation, no target
		"mkfs.ext4 /dev/sda",             // device, not a file
		"mkfs.ext4 -F /dev/sda",          // a flag before the device must not hide it
		"chmod -R 777 /etc",              // recursive permission change out of cwd
		"chmod -r 777 /etc",              // lowercase flag is the same recursive chmod
		"chown -R root:root /etc",        // recursive ownership change out of cwd
		"chown -R root /tmp/evil",        // owner arg then absolute target out of cwd
		"chown -r root:root /etc",        // lowercase flag is the same recursive chown
		"dd if=/dev/sda of=/dev/null",    // raw disk read
		"dd if=./backup.img of=/dev/sda", // raw disk write
		"echo x > /dev/sda",              // raw disk redirection
		// Fail-closed hardening (dsh): quoting, expansion and the root
		// target must not defeat the whitelist.
		"rm -rf \"/tmp/evil\"",     // quotes must not hide an absolute target
		"rm -rf \"../outside\"",    // quotes must not hide a relative escape
		"rm -rf $HOME/evil",        // shell variable target — unverifiable
		"rm -rf \"$HOME/evil\"",    // quoted variable is equally unverifiable
		"rm -rf $(echo /tmp/evil)", // command substitution — unverifiable
		"rm -rf `pwd`/evil",        // backtick expansion — unverifiable
		"dd of=\"/dev/sda\"",       // quoted dd operand must not hide the device
	} {
		_, err := Bash(cmd)
		if err == nil {
			t.Errorf("command %q must be rejected, got nil error", cmd)
			continue
		}
		if !strings.Contains(err.Error(), "dangerous") {
			t.Errorf("command %q rejection must mention 'dangerous', got: %v", cmd, err)
		}
	}
}

// TestBashDangerousInCwdWhitelistAllowed locks the whitelist's positive side:
// dangerous commands whose targets ALL resolve inside the working directory
// run (legitimate in-repo cleanup and tooling).
func TestBashDangerousInCwdWhitelistAllowed(t *testing.T) {
	prev := RejectDangerous
	RejectDangerous = true
	defer func() { RejectDangerous = prev }()
	savedEscape := escapeDangerous
	escapeDangerous = false
	defer func() { escapeDangerous = savedEscape }()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "disk"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Relative in-cwd target: the classic build cleanup.
	out, err := BashInDir("rm -rf ./build", dir)
	if err != nil || !strings.HasPrefix(out, "exit=0") {
		t.Fatalf("rm -rf ./build must run in-cwd: err=%v out=%q", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("in-cwd build dir should have been removed")
	}

	// Quoted in-cwd target: quote stripping must not over-reject — a quoted
	// relative target still resolves inside the workdir.
	if err := os.MkdirAll(filepath.Join(dir, "qdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = BashInDir("rm -rf \"./qdir\"", dir)
	if err != nil || !strings.HasPrefix(out, "exit=0") {
		t.Fatalf("quoted in-cwd rm -rf \"./qdir\" must run: err=%v out=%q", err, out)
	}

	// Absolute in-cwd target is equally whitelisted.
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = BashInDir("rm -rf "+filepath.Join(dir, "build"), dir)
	if err != nil || !strings.HasPrefix(out, "exit=0") {
		t.Fatalf("rm -rf <abs-in-cwd> must run: err=%v out=%q", err, out)
	}

	// Recursive permission change on an in-cwd tree.
	out, err = BashInDir("chmod -R 755 ./disk", dir)
	if err != nil || !strings.HasPrefix(out, "exit=0") {
		t.Fatalf("chmod -R 755 ./disk must run in-cwd: err=%v out=%q", err, out)
	}

	// Recursive ownership change on an in-cwd tree: the guard must let it
	// through. chown itself may fail with EPERM as a non-root user — a
	// non-zero exit is REPORTED, not an error; only the guard matters here.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = BashInDir("chown -R root ./sub", dir)
	if err != nil {
		t.Fatalf("chown -R root ./sub must pass the guard: err=%v out=%q", err, out)
	}
	if !strings.HasPrefix(out, "exit=") {
		t.Errorf("chown -R root ./sub must execute (exit=N), got out=%q", out)
	}

	// dd from a pseudo-device into an in-cwd file (harmless byte-stream idiom).
	out, err = BashInDir("dd if=/dev/zero of=./zero.bin bs=1024 count=1", dir)
	if err != nil || !strings.HasPrefix(out, "exit=0") {
		t.Fatalf("dd from /dev/zero into cwd must run: err=%v out=%q", err, out)
	}
}

// TestBashDangerousRootTargetAlwaysRejected locks the fail-closed root rule
// (dsh): a target that resolves to the filesystem root is rejected even when
// the working directory IS the root — the only case where the lexical
// inside-check would otherwise pass (Rel("/", "/") == ".").
func TestBashDangerousRootTargetAlwaysRejected(t *testing.T) {
	prev := RejectDangerous
	RejectDangerous = true
	defer func() { RejectDangerous = prev }()
	savedEscape := escapeDangerous
	escapeDangerous = false
	defer func() { escapeDangerous = savedEscape }()

	for _, cmd := range []string{
		"rm -rf /",
		"rm -rf //",
		"rm -rf /tmp/..", // cleans to the root
	} {
		_, err := BashInDir(cmd, "/")
		if err == nil {
			t.Errorf("command %q with workdir=/ must be rejected (resolves to the root)", cmd)
			continue
		}
		if !strings.Contains(err.Error(), "dangerous") {
			t.Errorf("command %q rejection must mention 'dangerous', got: %v", cmd, err)
		}
	}
}

// TestGOPI_ALLOW_DANGEROUS_EnvDisablesGuard verifies the env wiring: with
// GOPI_ALLOW_DANGEROUS=1 the guard is OFF from package init, so a dangerous
// command runs (the escape hatch for user-initiated cleanups). Uses the
// re-exec pattern because the env var is read exactly once, in init().
func TestGOPI_ALLOW_DANGEROUS_EnvDisablesGuard(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if os.Getenv("GOPI_SEC_ENV_PROBE") == "1" {
		// Child: the guard must be off. "sudo -n true" never prompts and is
		// rejected only by the dangerous-command guard — any exit code here
		// proves the command RAN.
		if _, err := Bash("sudo -n true"); err != nil {
			fmt.Fprintf(os.Stderr, "guard still on in child: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestGOPI_ALLOW_DANGEROUS_EnvDisablesGuard$")
	cmd.Env = append(os.Environ(), "GOPI_SEC_ENV_PROBE=1", "GOPI_ALLOW_DANGEROUS=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child failed (GOPI_ALLOW_DANGEROUS=1 must disable the guard): %v\n%s", err, out)
	}
}

// TestBashChildLocalePinnedToC is the minimal repro for the pre-existing
// master red (TestAgent_Run_ContextTrimReplaceSlimming /
// TestAgent_Run_ToolResultPayloadOnce): the bash child inherits the host
// LANG (zh_CN.UTF-8 on zh machines) and coreutils localize their output —
// `ls /nonexistent` returned 无法访问 instead of "cannot access", breaking
// every agent-side assertion on real command output. The C locale pin makes
// coreutils diagnostics byte-stable across hosts.
func TestBashChildLocalePinnedToC(t *testing.T) {
	output, err := Bash("ls /nonexistent-path-gopi-locale-probe")
	if err != nil {
		t.Fatalf("bash tool must report failure via exit=N, not a Go error: %v", err)
	}
	// The English diagnostic — present only when the child runs under the
	// C locale (LC_ALL/LANG pinned by bashChildEnv).
	if !strings.Contains(output, "No such file or directory") {
		t.Fatalf("bash child locale not pinned to C — output lacks the English diagnostic (locale-dependent): %q", output)
	}
	if strings.Contains(output, "无法访问") || strings.Contains(output, "没有那个文件") {
		t.Fatalf("bash child inherited a localized host locale: %q", output)
	}
}

// TestBash_ShortOutDiagnosticNeverSlices — regression guard for the
// slice-bounds defect this file's failure branches carried.
//
// THE DEFECT (Rule 41 filed as task 01a12efb): a diagnostic that evaluates
// a fixed-width slice of the result panics whenever the result is shorter than the fixed width. The
// result IS short exactly when the command printed nothing (measured on this
// host: 7 bytes for `true`/`false`, 9 for a missing command — see the constants
// at the top of this file), so the panic fired only in the FAILURE branch and
// replaced the real diagnostic with a slice-bounds stack. A diagnostic that
// dies instead of reporting is a silent failure (Rule 15): the reader learns
// neither why the assertion failed nor what the tool actually returned.
//
// Two independent arms, because the fixed shape is only reachable from a
// `%q` verb (a constant slice folds to a constant and vet is silent, so the
// hazard is not vet-catchable — it has to be measured):
//  1. BEHAVIOURAL: for each real no-stdout command, the fixed width used to
//     panic while the bounded shape this file now uses reports the whole
//     result. The assertion message is built exactly as a failing branch builds
//     it, so the shape is exercised, not inspected.
//  2. STATIC: a fixed top-only window written directly as an assertion-message
//     argument is itself the defect — unbounded by the value's length, and
//     invisible to vet whenever the value is dynamic. The scan below is scoped
//     to the assertion Message (the text after the format string), where a
//     message is what is being BUILT; inside an assertion's own condition a
//     window is protected by that condition. It covers the SIBLING files of
//     this package's bash surface — bash_test.go, bash_daemon_test.go and
//     timeout_test.go — because the defect was never a property of one file:
//     the daemon and timeout fixtures carry the same `output[:200]` in their
//     failure branches, and a fixed window is a defect wherever it is written.
//     (Task 01a12f0b widened the scan to them in the same change that bounded
//     their five sites.)
func TestBash_ShortOutDiagnosticNeverSlices(t *testing.T) {
	const shortOutTrue = "true"
	const shortOutMissingCmd = "gopi-no-such-command-01a12efb"
	// bashMinScannedLines: the file is far larger than this, so a scan that
	// read nothing is reported rather than silently green (Rule 15).
	const bashMinScannedLines = 100
	// A fixed-width slice written as a `%q` ARGUMENT — the shape that panics.
	fixedWidthDiagnostic := func(out string) string {
		return fmt.Sprintf("expected exit=0, got: %q", out[:bashDiagHeadProbeBytes])
	}
	fixedWidthWithShortOutPanicked := func(out string) (panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = fixedWidthDiagnostic(out)
		return false
	}
	// The bounded shape every site in this file now uses.
	boundedDiagnostic := func(out string) string {
		return fmt.Sprintf("expected exit=0, got: %q", out[:min(len(out), bashDiagHeadProbeBytes)])
	}
	_ = fixedWidthDiagnostic

	for _, cmd := range []string{shortOutTrue, "false", "printf x; false 2>/dev/null", shortOutMissingCmd + " 2>/dev/null"} {
		out, err := Bash(cmd)
		if err != nil {
			t.Fatalf("Bash(%q) must report failure via exit=N, not a Go error: %v", cmd, err)
		}
		if len(out) >= bashDiagHeadProbeBytes {
			t.Fatalf("premise not established: Bash(%q) returned %d bytes, so the fixed-width shape could not panic", cmd, len(out))
		}
		if !fixedWidthWithShortOutPanicked(out) {
			t.Errorf("Bash(%q) → %q (%d bytes): the fixed-width diagnostic shape must PANIC — if it does not, this guard has stopped proving the defect", cmd, out, len(out))
		}
		// %q renders the value ESCAPED, so the real content is the quoted form.
		if want := fmt.Sprintf("%q", out); !strings.Contains(boundedDiagnostic(out), want) {
			t.Errorf("Bash(%q): the bounded diagnostic dropped the real content — got %q, want it to carry %s", cmd, boundedDiagnostic(out), want)
		}
	}

	// Arm 2 — static scan of the bash surface's assertion messages, over the
	// whole file set (this file + its two siblings), never one hand-picked file.
	// Each entry carries its own read floor: the file is far larger than its
	// floor, so a scan that read a fragment — or a table that silently lost a
	// file — is reported rather than passing green (Rule 15).
	scanned := []struct {
		path  string
		floor int
	}{
		{"bash_test.go", 100},
		{"bash_daemon_test.go", 100},
		{"timeout_test.go", 100},
	}
	totalScanned := 0
	for _, f := range scanned {
		src, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", f.path, err)
		}
		lines := strings.Split(string(src), "\n")
		if len(lines) < f.floor {
			t.Fatalf("scanned only %d lines of %s (floor %d) — the guard read a fragment, not the file", len(lines), f.path, f.floor)
		}
		totalScanned += len(lines)
		scanAssertionMessages(t, f.path, lines)
	}
	// The floor is also on the FILE SET: three files were read, so a table that
	// lost one (or a loop that never ran) cannot pass as a clean scan.
	if len(scanned) != bashDiagScannedFiles {
		t.Fatalf("the scan covered %d files, want %d — the file set was trimmed", len(scanned), bashDiagScannedFiles)
	}
	if totalScanned < bashDiagScannedFiles*bashMinScannedLines {
		t.Fatalf("scanned %d lines across %d files — the guard read a fragment", totalScanned, len(scanned))
	}
}

// scanAssertionMessages reports every assertion MESSAGE in lines that slices a
// value by a FIXED width, naming the file and line. It is a named helper so the
// three files are scanned by ONE rule (Rule 14) — a per-file copy is free to
// drift, and a drifted copy is how a widened guard silently stops covering the
// file it was widened for.
func scanAssertionMessages(t *testing.T, path string, lines []string) {
	t.Helper()
	directive := regexp.MustCompile(`%[a-z]`)
	// Both panic shapes: a TAIL window (x[:40]) and a LOW-bound window (x[7:]).
	// Deliberately NOT anchored at the end — the argument text inside a call is
	// `out[:40])`, because the message slice runs to the end of the LINE and so
	// carries the closing paren. A bound that is an expression (min(len(x),N) /
	// max(0,len(x)-N)) matches neither alternation and is the safe shape.
	window := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\[\s*(:[0-9]+|[0-9]+\s*:)`)
	for i, line := range lines {
		// The verb is on the assertion's own line and the diagnostic argument
		// follows the format directive on that SAME line. NOTE: no
		// trailing-';' requirement and no message-length threshold — every one
		// of the sites this guard exists for was a short one-line
		// `t.Errorf("...", out[:N])` ending in ')', so both filters skipped
		// precisely the defect (measured: the mutation stayed GREEN under both).
		verb := strings.Index(line, "Errorf(")
		if verb < 0 {
			verb = strings.Index(line, "Fatalf(")
		}
		if verb < 0 {
			continue
		}
		loc := directive.FindStringIndex(line[verb:])
		if loc == nil {
			continue
		}
		for _, arg := range strings.Split(line[verb+loc[1]:], ",") {
			arg = strings.TrimSpace(arg)
			if window.MatchString(arg) {
				// The message slice runs to the end of the LINE, so the last
				// argument still carries the call's own closing paren; strip it
				// so the reported shape is the slice expression itself. A
				// leading argument (there are more after the comma) has no paren
				// to strip, which is why this is a TrimSuffix and not a slice.
				t.Errorf("%s:%d slices a value by a FIXED width in an assertion message — bound it by the value's own length (%s): %s",
					path, i+1, strings.TrimSuffix(arg, ")"), strings.TrimSpace(line))
			}
		}
	}
}
