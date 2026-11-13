package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// The executor-spawn seam's half of the throwaway-profile guard (epic: a child
// must never resolve a throwaway profile).
//
// taskChildExecEnv is the environment the `task run` child is spawned with, so a
// checkout-shaped GOPI_HOME must not survive it — and a sandbox home must.
// ---------------------------------------------------------------------------

// TestStripCheckoutHomeEnv_DropsACheckout is the RED side: delete the call to
// stripCheckoutHomeEnv in taskChildExecEnv (or the function) and this fails.
func TestStripCheckoutHomeEnv_DropsACheckout(t *testing.T) {
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, config.CheckoutMarkerName), 0o755); err != nil {
		t.Fatalf("mark %s as a code checkout: %v", worktree, err)
	}
	in := []string{
		"PATH=/usr/bin",
		config.GopiDirEnv + "=" + worktree,
		"GOPI_MEMORY_DB=" + filepath.Join(worktree, "mem.db"),
	}
	got, removed := stripCheckoutHomeEnv(in)
	if len(removed) != 1 || removed[0] != config.GopiDirEnv {
		t.Fatalf("stripCheckoutHomeEnv removed %v, want exactly [%s]", removed, config.GopiDirEnv)
	}
	for _, kv := range got {
		if strings.HasPrefix(kv, config.GopiDirEnv+"=") {
			t.Fatalf("the executor child would be spawned with %s — it would resolve %s/%s as its profile",
				kv, worktree, config.ProfileDirName)
		}
	}
	// Only the checkout home goes: the child legitimately inherits the rest.
	if len(got) != 2 {
		t.Fatalf("stripCheckoutHomeEnv removed more than the checkout home: %v", got)
	}
}

// TestStripCheckoutHomeEnv_KeepsASandbox is the KEEP ARM at the spawn seam.
func TestStripCheckoutHomeEnv_KeepsASandbox(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.MkdirAll(config.ProfileDirIn(sandbox), 0o755); err != nil {
		t.Fatalf("materialise the sandbox profile: %v", err)
	}
	in := []string{config.GopiDirEnv + "=" + sandbox}
	got, removed := stripCheckoutHomeEnv(in)
	if len(removed) != 0 || len(got) != 1 {
		t.Fatalf("stripCheckoutHomeEnv touched a sandbox home: removed=%v got=%v", removed, got)
	}
	if tools.IsCheckoutHomeEnv(config.GopiDirEnv, sandbox) {
		t.Fatalf("tools.IsCheckoutHomeEnv refused the sandbox home %q — the seam and the predicate disagree", sandbox)
	}
}

// TestTaskCheckoutHomeStripLine_AnnouncesOnlyTheDroppedArm pins the record: the
// dropped arm names value+reason, the kept arm prints nothing.
func TestTaskCheckoutHomeStripLine_AnnouncesOnlyTheDroppedArm(t *testing.T) {
	worktree := t.TempDir()
	env := []string{config.GopiDirEnv + "=" + worktree}

	line := taskCheckoutHomeStripLine([]string{config.GopiDirEnv}, env)
	for _, want := range []string{config.GopiDirEnv, worktree, config.CheckoutMarkerName} {
		if !strings.Contains(line, want) {
			t.Fatalf("the executor strip record does not name %q: %q", want, line)
		}
	}
	if kept := taskCheckoutHomeStripLine(nil, env); kept != "" {
		t.Fatalf("a KEPT GOPI_HOME produced a strip record: %q", kept)
	}
}

// TestTaskChildExecEnv_DropsACheckoutGOPIHome drives the REAL seam function the
// spawn path calls, not just its helper.
func TestTaskChildExecEnv_DropsACheckoutGOPIHome(t *testing.T) {
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, config.CheckoutMarkerName), 0o755); err != nil {
		t.Fatalf("mark %s as a code checkout: %v", worktree, err)
	}
	t.Setenv(config.GopiDirEnv, worktree)

	env, removed := taskChildExecEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, config.GopiDirEnv+"=") {
			t.Fatalf("taskChildExecEnv kept %s in the executor child's environment", kv)
		}
	}
	if len(removed) == 0 {
		t.Fatal("taskChildExecEnv reported no removals although it dropped a checkout-shaped home (the strip must be announced)")
	}
}
