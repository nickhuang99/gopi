package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The regression guard for the measured defect of 2026-10-13: two concurrent
// runs under ONE label (different task ids) were handed the SAME directory,
// because Create's reuse predicate matched on the LABEL alone. A caller's
// sandbox is private, so a live foreign incarnation must disqualify the
// candidate — the second caller gets a fresh directory instead of the first
// one's profile, whose `t.Cleanup(os.RemoveAll(sb))` deleted it mid-flight.
//
// The foreign owner is a REAL second process (a live `sleep`), because the
// property under test is about a live incarnation that is not this one: an
// in-process fake could only re-derive the very pid identity the check reads.

const (
	// foreignSandboxHolderSeconds keeps the planted foreign process alive for
	// the whole test — long enough that no assertion races the holder's exit.
	foreignSandboxHolderSeconds = "30"
	// foreignSandboxTouch is the mtime stamped on a planted leftover, so
	// "newest wins" cannot be satisfied by accident of wall-clock timing.
	foreignSandboxTouch = "2026-10-13T00:00:00Z"
)

// TestCreateDoesNotAdoptALiveForeignSandbox is the red-proof of the fix: with
// the label-only predicate restored it fails, because Create returns the
// occupied directory.
func TestCreateDoesNotAdoptALiveForeignSandbox(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	opts := Options{Label: "regress", ProfileSettingsPath: settings}

	occupied := plantLiveForeignSandbox(t, root, opts.Label)
	before, err := ReadOwner(occupied)
	if err != nil {
		t.Fatalf("read the planted marker: %v", err)
	}

	sb, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("Create under a live foreign sandbox: %v", err)
	}
	if sb.Path == occupied {
		t.Fatalf("Create handed out %s — a sandbox another live process is standing in; two callers would share one private profile and the first one's cleanup would delete the second one's ground", occupied)
	}
	if !sb.Created || sb.Reused {
		t.Errorf("Create reported created=%t reused=%t, want created=true reused=false (the occupied sandbox is not ours to reuse)",
			sb.Created, sb.Reused)
	}
	after, err := ReadOwner(occupied)
	if err != nil {
		t.Fatalf("the occupied sandbox's marker was lost — Create must not touch a foreign sandbox: %v", err)
	}
	if after != before {
		t.Errorf("the occupied sandbox's marker was rewritten (%+v -> %+v): Create must leave a live foreign sandbox exactly as it found it", before, after)
	}
}

// TestCreateDoesNotAdoptADeadRunsLeftover is the SECOND half of the measured
// defect, and the one that survives the obvious fix: `gopi tool sandbox create`
// is a SHORT-LIVED process, so the marker of a sandbox a caller is still using
// names a pid that has already exited. "The previous incarnation is gone" is
// therefore NOT evidence that the directory is free — adopting it deletes a
// live caller's ground. The leftover is left to the reclaimer, which owns that
// decision (aged + unowned).
func TestCreateDoesNotAdoptADeadRunsLeftover(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	opts := Options{Label: "leftover", ProfileSettingsPath: settings}

	stale := plantDeadForeignSandbox(t, root, opts.Label)

	sb, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("Create beside a dead run's leftover: %v", err)
	}
	if sb.Path == stale {
		t.Fatalf("Create adopted %s — a directory whose owner is gone, which is exactly what a caller's still-in-use sandbox looks like when the initializer ran as a one-shot CLI; adopting it hands out (and later deletes) another run's ground", stale)
	}
	if !sb.Created || sb.Reused {
		t.Errorf("Create reported created=%t reused=%t, want created=true reused=false (nothing under this root is provably ours)",
			sb.Created, sb.Reused)
	}
	if _, err := os.Stat(filepath.Join(stale, config.SandboxOwnerFileName)); err != nil {
		t.Errorf("Create touched the foreign leftover: %v", err)
	}
}

// TestCreateAdoptsItsOwnSandboxOverAnOlderOne pins the half that must keep
// working: an idempotent re-ask lands on THIS process's sandbox even when a
// foreign directory of the same label carries a newer mtime.
func TestCreateAdoptsItsOwnSandboxOverAnOlderOne(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	opts := Options{Label: "idem", ProfileSettingsPath: settings}

	mine, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	// A foreign directory of the same label, touched to be strictly newer than
	// ours: "newest wins" must not be able to outbid this process's own sandbox.
	foreign := plantDeadForeignSandbox(t, root, opts.Label)
	mineInfo, err := os.Stat(mine.Path)
	if err != nil {
		t.Fatalf("stat this process's sandbox: %v", err)
	}
	touched := mineInfo.ModTime().Add(time.Hour)
	if err := os.Chtimes(foreign, touched, touched); err != nil {
		t.Fatalf("touch the foreign directory: %v", err)
	}
	foreignInfo, err := os.Stat(foreign)
	if err != nil {
		t.Fatalf("stat the foreign directory: %v", err)
	}
	if !foreignInfo.ModTime().After(mineInfo.ModTime()) {
		t.Fatalf("fixture is not discriminating: the foreign mtime %s is not newer than ours %s",
			foreignInfo.ModTime(), mineInfo.ModTime())
	}

	again, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if again.Path != mine.Path {
		t.Fatalf("the idempotent re-ask landed on %s instead of this process's own %s (a foreign directory outbid it)", again.Path, mine.Path)
	}
	if !again.Reused || again.Created {
		t.Errorf("the re-ask reported created=%t reused=%t, want created=false reused=true", again.Created, again.Reused)
	}
}

// plantLiveForeignSandbox materialises a sandbox directory whose marker names a
// REAL live process that is not this one (a `sleep` the test keeps alive), and
// returns the directory.
func plantLiveForeignSandbox(t *testing.T, root, label string) string {
	t.Helper()
	cmd := exec.Command("sleep", foreignSandboxHolderSeconds)
	// The foreign holder's subject is its PID (a live process the marker names),
	// never its cwd — so a neutral dir plus Pdeathsig keeps it from outliving
	// the binary that started it.
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the foreign holder process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()    //stylegate:ignore kills the child process the test spawned and is tearing down
		_, _ = cmd.Process.Wait() //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	})
	ticks, err := scratch.ProcStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("read the foreign process's start ticks: %v", err)
	}
	return writeSandboxMarker(t, root, label, cmd.Process.Pid, ticks)
}

// plantDeadForeignSandbox materialises a sandbox whose marker names a pid that
// has exited: the leftover of a run that is gone.
func plantDeadForeignSandbox(t *testing.T, root, label string) string {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run the short-lived process: %v", err)
	}
	pid := cmd.Process.Pid
	if ticks, err := scratch.ProcStartTime(pid); err == nil {
		t.Fatalf("the short-lived process (pid %d, ticks %d) is still in /proc — the fixture cannot build a dead owner", pid, ticks)
	}
	// A pid that has exited has no start ticks to read; the marker keeps the
	// zero value, which Live() reports as "cannot tell" and ThisProcess()
	// rejects — and a pid the kernel has already recycled is refused by the
	// identity pair the same way. Either way it is NOT this process.
	return writeSandboxMarker(t, root, label, pid, 0)
}

// writeSandboxMarker creates a sandbox-shaped directory under root (the name
// carries the label, as Create's does) and writes the given owner marker into
// it. It intentionally does NOT materialise the profile: Ensure is what the
// adoption assertions observe doing that.
func writeSandboxMarker(t *testing.T, root, label string, pid int, ticks uint64) string {
	t.Helper()
	dir := filepath.Join(root, sandboxDirPrefix+sanitizeLabel(label)+"-"+strconv.Itoa(pid))
	if err := os.MkdirAll(dir, sandboxDirPerm); err != nil {
		t.Fatalf("create the sandbox-shaped directory: %v", err)
	}
	owner := Owner{
		TaskID:     "planted-" + strconv.Itoa(pid),
		Label:      sanitizeLabel(label),
		PID:        pid,
		StartTicks: ticks,
		CreatedAt:  foreignSandboxTouch,
		Cmd:        "planted",
		Version:    ownerMarkerVersion,
	}
	if err := writeOwner(dir, owner); err != nil {
		t.Fatalf("write the planted marker: %v", err)
	}
	return dir
}
