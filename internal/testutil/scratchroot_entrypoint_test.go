package testutil

// Entry-point tests: the /tmp ban must FAIL A WHOLE SUITE, not merely return
// an error a caller might ignore. The ban's value is that the suite exits with
// SuiteScratchUnsafeExitCode BEFORE a single test runs, so the deliverable is
// the wiring, not the error value.
//
// The proof is a RE-EXEC: the test binary spawns itself with an environment
// that pins the scratch root onto the / filesystem (GOPI_TEST_TMPDIR pointed
// at a fixture directory created under /tmp), runs one trivial test, and
// asserts the child exits with SuiteScratchUnsafeExitCode, prints the
// actionable message, and never reached the test body.
//
// The override points at a fixture DIRECTORY (not at /tmp itself) on purpose:
// the child's pin runs a scratch GC inside its scratch home, and the test must
// never sweep the host's real /tmp.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reexecQueryEnvName tells the child test binary why it was started.
const reexecQueryEnvName = "GOPI_TESTUTIL_REEXEC"

// reexecTrivialTest names the trivial test the child would run if the ban
// failed to refuse.
const reexecTrivialTest = "TestScratchReexecTrivial"

// reexecFallbackEnvName asks the child to simulate the IMPLICIT fallback: skip
// the pin entirely, i.e. run with whatever temp root the ambient environment
// happened to report. It is the one state a suite reaches without anyone
// choosing it — PinSuiteTempRoot refuses to engage (its scratch home is
// unusable) and the ban is what remains between the suite and `/tmp`.
const reexecFallbackEnvName = "GOPI_TESTUTIL_NO_PIN"

// TestSuitePinFallbackToBootDeviceFails is the IMPLICIT-fallback assertion
// demanded by the audit: a suite whose pin did not engage must FAIL on a boot
// device instead of silently writing its scratch there.
//
// The child is this same test binary re-exec'd with the pin suppressed and
// TMPDIR aimed at a fixture inside the boot device's temp root (/tmp), so the
// ambient-temp-root branch of AssertSuiteScratchRootSafe is the one exercised.
// Three properties are asserted, in the order that matters:
//
//  1. the child exits with SuiteScratchUnsafeExitCode — a non-zero status, so
//     a CI wrapper can never mistake it for a pass;
//  2. the message names the IMPLICIT fallback (not an operator's pin) plus the
//     actionable fix, because the two cases need different fixes;
//  3. no test body ran — the refusal is pre-test, not a mid-suite failure.
func TestSuitePinFallbackToBootDeviceFails(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" {
		t.Skip("already inside the re-exec child — the parent owns this assertion")
	}
	if fi, err := os.Stat(banProbeDir); err != nil || !fi.IsDir() {
		t.Skipf("%s is missing on this host — an ambient-temp-root fixture cannot be built", banProbeDir)
	}
	if shares, err := ScratchRootSharesRootFS(banProbeDir); err != nil || !shares {
		t.Skipf("%s is not on the same filesystem as / here (an operator split it off) — the fallback cannot be demonstrated", banProbeDir)
	}
	fixture, err := os.MkdirTemp(banProbeDir, "gopi-tmpfallback-reexec-")
	if err != nil {
		t.Fatalf("ambient temp fixture under %s: %v", banProbeDir, err)
	}
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(fixture) })
	// The child's scratch home must be UNCREATABLE, so the pin genuinely does
	// not engage: GOPI_TEST_HOME points at a regular file, and every MkdirAll
	// under it fails with ENOTDIR (a friendlier, unprivileged way to reach the
	// fallback than a chmod-000 directory, which root could still create).
	blocked := filepath.Join(fixture, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file, not a home"), filePerm); err != nil {
		t.Fatalf("write %s: %v", blocked, err)
	}

	// The child: no pin can engage (GOPI_TEST_HOME is a file), TMPDIR at the
	// boot-device fixture, and a PRIVATE memory base so its (skipped) sweep can
	// never see the host's real one.
	cmd := exec.Command(os.Args[0], "-test.run", "^"+reexecTrivialTest+"$", "-test.v")
	cmd.Env = append(scrubbedScratchEnv(),
		TempDirEnvName+"="+fixture,
		MemoryBaseEnvName+"="+fixture,
		WorkspaceHomeEnvName+"="+blocked,
		reexecFallbackEnvName+"=1",
		reexecQueryEnvName+"=1")
	out, err := cmd.CombinedOutput()
	text := string(out)

	exit, ok := exitCodeOf(err)
	if !ok {
		t.Fatalf("child did not exit with a status (err = %v):\n%s", err, text)
	}
	if exit == 0 {
		t.Fatalf("child exited 0 with the pin suppressed and TMPDIR on the boot device — a suite can silently "+
			"write its scratch to /tmp:\n%s", text)
	}
	if exit != SuiteScratchUnsafeExitCode {
		t.Fatalf("child exit = %d, want the documented refusal code %d:\n%s", exit, SuiteScratchUnsafeExitCode, text)
	}
	for _, want := range []string{"did NOT engage", "AMBIENT temp root", "SAME filesystem as /", "FIX:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("fallback refusal does not carry %q — an implicit fallback must be distinguishable from an "+
				"operator's explicit pin:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--- PASS: "+reexecTrivialTest) {
		t.Fatalf("the child RAN a test before refusing — the ban must fire pre-test:\n%s", text)
	}
}

// scrubbedScratchEnv is the parent's environment with every scratch variable
// removed, so the child's path is decided ONLY by what the test sets. MemoryBaseEnvName
// is included: a child that runs the legacy sweep must never be handed the
// host's real memory base by an ambient variable (every caller of this helper
// sets it to a private base instead).
func scrubbedScratchEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		switch key {
		case ScratchHomeEnvName, WorkspaceHomeEnvName, SuiteTempRootEnvName, TempDirEnvName, MemoryBaseEnvName:
			continue
		}
		env = append(env, kv)
	}
	return env
}

// TestScratchReexecTrivial is the child's trivial test: reaching it proves the
// ban did NOT fire, which is exactly the failure the parent asserts against.
func TestScratchReexecTrivial(t *testing.T) {
	t.Log("child scratch-rexec probe ran — the ban did not refuse this suite")
}

// exitCodeOf extracts a child's exit status; ok is false for a non-exit error
// (a failed spawn, which must not be mistaken for a clean refusal).
func exitCodeOf(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}
