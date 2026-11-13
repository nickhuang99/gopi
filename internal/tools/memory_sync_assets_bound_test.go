package tools

// memory_sync_assets_bound_test.go — the 2026-09-26 defect: the asset ssh leg
// was bounded by the TASK leg's 15s constant, while it carries the WHOLE local
// corpus (measured 5209 rows / 15 MB) and the peer must apply every row before
// answering (measured 47.5s on the real pair). A healthy transfer was
// SIGKILLed and reported as `ssh hp: signal: killed (stderr: )` — no cause,
// no number, nothing to act on.
//
// These tests pin the two halves of the fix:
//   ① the leg's bound is its OWN, size-appropriate value — and NEVER the task
//      leg's 15s (the regression guard: restoring the old constant fails here);
//   ② a bound that DOES elapse is reported as a timeout naming the value and
//      the knob, instead of forwarding ssh's opaque signal line.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// TestAssetSyncBoundIsItsOwnValueAndOutlivesTheTaskLeg is the regression guard
// for the defect: the asset leg's default bound must be strictly larger than
// the task leg's, because its payload is the whole corpus rather than a delta
// window. Reverting to `15 * time.Second` (or to syncSSHTimeout itself) fails
// here.
func TestAssetSyncBoundIsItsOwnValueAndOutlivesTheTaskLeg(t *testing.T) {
	bound, err := assetSyncSSHBound()
	if err != nil {
		t.Fatalf("default bound: %v", err)
	}
	if bound <= syncSSHTimeout {
		t.Fatalf("asset leg bound = %s, want > the task leg's %s: the asset leg carries the WHOLE local corpus (measured 5209 rows/15MB taking 47.5s to apply on the peer), so the delta window's bound SIGKILLs a healthy transfer", bound, syncSSHTimeout)
	}
	// The measured real leg (47.5s) must fit with room to spare, and so must
	// a corpus order(s) of magnitude larger — the bound exists to catch a
	// STALL, not to estimate work.
	if bound < 10*time.Minute {
		t.Fatalf("asset leg bound = %s, want at least 10m (the measured leg alone took 47.5s)", bound)
	}
}

// TestAssetSyncBoundEnvOverride — the knob: a positive seconds value wins, and
// a malformed one is REFUSED rather than silently ignored (Rule 15).
func TestAssetSyncBoundEnvOverride(t *testing.T) {
	t.Setenv(config.MemorySyncTimeoutEnv, "42")
	bound, err := assetSyncSSHBound()
	if err != nil {
		t.Fatalf("override 42: %v", err)
	}
	if bound != 42*time.Second {
		t.Fatalf("override 42 → %s, want 42s", bound)
	}
	for _, bad := range []string{"0", "-3", "abc", "1.5"} {
		t.Setenv(config.MemorySyncTimeoutEnv, bad)
		if _, err := assetSyncSSHBound(); err == nil {
			t.Errorf("%s=%q must be refused, not silently ignored", config.MemorySyncTimeoutEnv, bad)
		}
	}
	// An empty value is the documented "unset" spelling → the default.
	t.Setenv(config.MemorySyncTimeoutEnv, "  ")
	if bound, err := assetSyncSSHBound(); err != nil || bound != memoryAssetSyncSSHTimeout {
		t.Fatalf("blank override → (%s, %v), want the default %s and no error", bound, err, memoryAssetSyncSSHTimeout)
	}
}

// TestAssetSyncTimeoutErrorNamesBoundAndCause — an elapsed bound is the most
// likely failure of this feature and must not read as ssh's own verdict. The
// rendered error carries the peer, the bound, the knob and ssh's stderr.
func TestAssetSyncTimeoutErrorNamesBoundAndCause(t *testing.T) {
	err := assetSyncSSHTimeoutError("hp", 15*time.Minute, "")
	msg := err.Error()
	// BOTH spellings of the one knob are wanted (2026-11-06): `--timeout` came
	// back with the verb, so naming only the env var would send the operator to
	// a shell export when a flag on the command they are already running works.
	for _, want := range []string{"hp", "15m", config.MemorySyncTimeoutEnv, "--timeout", "bound elapsed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("timeout error missing %q:\n%s", want, msg)
		}
	}
	// ssh's own stderr is carried through, not swallowed (Rule 15).
	if msg := assetSyncSSHTimeoutError("hp", 90*time.Second, "Timeout, server hp not responding").Error(); !strings.Contains(msg, "not responding") {
		t.Errorf("the peer's own message must survive, got:\n%s", msg)
	}
}

// TestAssetSyncExchangeDialsTheRestoredVerb — the leg was RETIRED on
// 2026-11-05 with the whole cross-node sync CLI family (5677cc6e), and this
// file used to pin the ABSENCE: a shim proved no ssh argv was composed at all.
// The verb is back (2026-11-06) and this test pins the opposite fact — the leg
// really dials, and what it dials is the PEER-SIDE handler of the verb that
// exists: `<gopi> memory sync --serve-assets`, ONE remote-argv element.
//
// WHY THIS IS THE TEST THAT MATTERS: the pre-deletion transport kept composing
// the DELETED spelling, so every session ended in a removal notice that the
// initiator could only read as a protocol error. The assertion below fails if
// the remote command drifts from the constant the CLI dispatches.
func TestAssetSyncExchangeDialsTheRestoredVerb(t *testing.T) {
	oldHook := syncSSHExchangeHook
	syncSSHExchangeHook = nil
	t.Cleanup(func() { syncSSHExchangeHook = oldHook })

	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	shim := filepath.Join(dir, "ssh")
	// One argv element per line, so the LAST line is the remote command string
	// and the earlier lines are the options ssh itself receives.
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > " + argvFile + "\nprintf '{\"rows\":[]}'\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	reply, err := syncAssetsSSHExchange("hp", []byte("{}"), 7*time.Minute)
	if err != nil {
		t.Fatalf("the restored leg must run, got: %v", err)
	}
	if len(reply) == 0 {
		t.Fatal("the leg returned no reply body")
	}
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("the leg did not dial (no argv recorded): %v", err)
	}
	argv := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(argv) < 2 {
		t.Fatalf("ssh argv looks truncated: %q", argv)
	}
	// The REMOTE COMMAND is one element, and it names the peer-side handler of
	// a verb the dispatcher actually recognises (Rule 14: composed from the
	// constants, never re-typed).
	wantRemote := syncRemoteGopi + " " + CliVerbMemoryGroup + " " + MemorySyncVerb + " " + syncFlagPrefixRow + MemorySyncServeAssetsFlag
	if got := argv[len(argv)-1]; got != wantRemote {
		t.Errorf("remote command = %q, want %q", got, wantRemote)
	}
	// The bound and the keepalives are still parts of the composed argv: without
	// them a silent peer holds the leg until the absolute deadline.
	// Each knob is its OWN `-o` value (never one comma-joined argument), so the
	// assertion names the two separately: MemoryAssetSyncKeepaliveHint is the
	// RENDERED sentence for a human, and looking for it in argv finds a string
	// ssh never receives (measured 2026-11-06 — this arm failed on exactly that).
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"BatchMode=yes",
		"ConnectTimeout=" + syncSSHConnectTimeout,
		"ServerAliveInterval=" + memoryAssetSyncKeepaliveInterval,
		"ServerAliveCountMax=" + memoryAssetSyncKeepaliveCount,
		"hp",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh argv missing %q:\n%s", want, joined)
		}
	}
}
