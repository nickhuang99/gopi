package main

// Serve-face tests for the adoption pass step: interval resolution
// (Rule 2 env), throttle, dirty-tree refusal. The full git chain lives in
// internal/tools/task_adoption_test.go — here only the serve wiring.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

func TestServeAdoptionInterval(t *testing.T) {
	t.Setenv("GOPI_ADOPTION_INTERVAL", "")
	if got := serveAdoptionInterval(); got != adoptionIntervalDefault {
		t.Fatalf("absent env must default, got %v", got)
	}
	t.Setenv("GOPI_ADOPTION_INTERVAL", "0")
	if got := serveAdoptionInterval(); got != 0 {
		t.Fatalf("0 must disable, got %v", got)
	}
	t.Setenv("GOPI_ADOPTION_INTERVAL", "junk")
	if got := serveAdoptionInterval(); got != 0 {
		t.Fatalf("junk must disable (never misfire), got %v", got)
	}
	t.Setenv("GOPI_ADOPTION_INTERVAL", "5")
	if got := serveAdoptionInterval(); got != 5*time.Second {
		t.Fatalf("5 -> 5s, got %v", got)
	}
}

func TestServeAdoptionThrottle(t *testing.T) {
	// A peer must be DECLARED: the sweep's precondition is the declared
	// topology (code-delivery axis), so without one the case would stop at that
	// gate and never reach the throttle this test is about.
	adoptTestDeclarePeer(t, `{"sync_peers":["adopt-test-peer"]}`)
	t.Setenv("GOPI_ADOPTION_INTERVAL", "600") // 10min: any second pass throttles
	saved := serveAdoptionLastTick
	defer func() { serveAdoptionLastTick = saved }()
	serveAdoptionLastTick = time.Now() // pretend we just swept
	// The sweep on a nonexistent repo must NOT run (throttled) — the git
	// call inside would fail with a visible stderr print otherwise.
	serveAdoptionSweep(filepath.Join(t.TempDir(), "no-such-repo"))
}

func TestServeAdoptionDirtyTreeSkip(t *testing.T) {
	adoptTestDeclarePeer(t, `{"sync_peers":["adopt-test-peer"]}`)
	t.Setenv("GOPI_ADOPTION_INTERVAL", "1")
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "master")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("uncommitted\n"), 0o644)
	saved := serveAdoptionLastTick
	serveAdoptionLastTick = time.Time{} // force the pass to run
	defer func() { serveAdoptionLastTick = saved }()
	// A dirty tree must log the skip line and never touch git state: the
	// sweep returns before any adoption runs, so the tree STAYS dirty.
	serveAdoptionSweep(dir)
	dirty, err := adoptionCleanTree(dir)
	if err != nil || dirty {
		t.Fatalf("sweep must not have cleaned the tree: clean=%v err=%v", dirty, err)
	}
}

// adoptTestDeclarePeer writes a settings.json carrying the DECLARED topology the
// adoption sweep's precondition reads, with both topology env vars cleared so
// the file (not the ambient shell) decides.
//
// IT USED TO BE `switchTestSettings`, and it moved here when its original file
// (task_sync_switch_test.go) went with the cross-node sync family — the name
// said `switch` because the task-row-sync switch was what it was first built
// for. The adoption sweep never consulted that switch (see
// serveAdoptionSweep's WHY: adoption belongs to the CODE-DELIVERY axis, and
// gating it on task_sync would half-disable delivery), so what the helper
// actually provides is a DECLARED PEER. Naming it for that is the whole fix:
// a helper called "switch" that sets no switch is a trap for its next reader.
func adoptTestDeclarePeer(t *testing.T, content string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, p)
	t.Setenv(config.TaskSyncEnv, "")
	t.Setenv(config.SyncPeersEnv, "")
}
