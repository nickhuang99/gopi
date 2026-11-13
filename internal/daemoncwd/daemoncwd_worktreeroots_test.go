package daemoncwd

// daemoncwd_worktreeroots_test.go — the regression test for the DECLARED-LAYER
// divergence (epic daemon-cwd-declared-root, task 01a1789d).
//
// THE DEFECT THIS PINS (measured 2026-10-26): productionDeps built its
// worktreeRoots list from worktreeroots.DefaultRoot() and hard-coded
// worktreeroots.SourceDefault, so this package saw the DEFAULT layer alone
// while every other consumer goes through worktreeroots.Resolve. Under
// GOPI_WORKTREE_ROOT=<path> the two answers disagreed (this list: the
// default/test-pinned root, Source=default; the resolver: the declared root,
// Source="settings worktree_root / GOPI_WORKTREE_ROOT"), and a scratch tree
// standing inside the DECLARED root was reported as provably outside it.
//
// The test drives the PRODUCTION seam (productionDeps / productionWorktreeRoots
// / the exported WorktreeRoots), never a re-implementation: that is what makes
// it fail when the fix is disabled instead of merely describing it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/worktreeroots"
)

// TestProductionWorktreeRootsHonoursTheDeclaredLayer is the red/green pin: for
// every layer the ONE resolver knows about, the production seam must carry the
// SAME root and the SAME Source — and the declared layers must WIN over the
// default one (Rule 2's precedence, observed from the consumer side).
func TestProductionWorktreeRootsHonoursTheDeclaredLayer(t *testing.T) {
	t.Setenv(worktreeroots.WorktreeRootEnvName, "")
	declaredDefault, err := productionWorktreeRoots("")
	if err != nil {
		t.Fatalf("production roots (default layer): %v", err)
	}
	if len(declaredDefault) != 1 {
		t.Fatalf("production roots = %d entries, want exactly 1", len(declaredDefault))
	}
	// The default layer may still be the TEST PIN (scratch_pin.go keeps that
	// branch), so the assertion is about the SOURCE and about agreeing with the
	// resolver — never about the literal <home>/workspace.
	if got := declaredDefault[0].Source; got != worktreeroots.SourceDefault {
		t.Fatalf("default-layer Source = %q, want %q", got, worktreeroots.SourceDefault)
	}
	if got, want := declaredDefault[0].Path, mustResolve(t, "").Path; got != want {
		t.Fatalf("default-layer Path = %q, want the resolver's %q", got, want)
	}
	if got := declaredDefault[0].Role; got != worktreeroots.RoleWorktree {
		t.Fatalf("default-layer Role = %q, want %q", got, worktreeroots.RoleWorktree)
	}

	t.Run("env layer wins over default", func(t *testing.T) {
		envRoot := t.TempDir()
		t.Setenv(worktreeroots.WorktreeRootEnvName, envRoot)

		roots := WorktreeRoots()
		if len(roots) != 1 {
			t.Fatalf("WorktreeRoots() = %d entries, want exactly 1", len(roots))
		}
		want := mustResolve(t, "")
		if roots[0].Path != want.Path {
			t.Errorf("WorktreeRoots()[0].Path = %q, want %q", roots[0].Path, want.Path)
		}
		if roots[0].Source != worktreeroots.SourceSettingsEnv {
			t.Errorf("WorktreeRoots()[0].Source = %q, want %q", roots[0].Source, worktreeroots.SourceSettingsEnv)
		}
		if got, wantSource := roots[0].Source, worktreeroots.SourceDefault; got == wantSource {
			t.Errorf("the DECLARED env layer was reported as %q — the default layer still won", got)
		}
		if roots[0].Path != filepath.Clean(envRoot) {
			t.Errorf("WorktreeRoots()[0].Path = %q, want the declared env root %q", roots[0].Path, envRoot)
		}
		// The measurement this list exists for: the DECLARED root must now be
		// recognised, i.e. a candidate inside it is no longer provably outside.
		inside := filepath.Join(roots[0].Path, "some-scratch-tree")
		if _, ok := worktreeroots.PermitsAny(roots, inside); !ok {
			t.Errorf("the declared root %q refused to claim its own child %q", roots[0].Path, inside)
		}
		t.Logf("declared env root = %s, Source = %s", roots[0].Path, roots[0].Source)
	})

	t.Run("settings layer wins over default", func(t *testing.T) {
		// The settings `worktree_root` reaches this package as the `declared`
		// argument (the production seam passes "", because the daemon spawn has
		// no repo context — the same value config.ResolveRepoWorktreeRoot passes
		// for a repo absent from the table).
		declared := t.TempDir()
		roots, rerr := productionWorktreeRoots(declared)
		if rerr != nil {
			t.Fatalf("production roots (declared): %v", rerr)
		}
		if len(roots) != 1 {
			t.Fatalf("production roots = %d entries, want exactly 1", len(roots))
		}
		if roots[0].Path != filepath.Clean(declared) {
			t.Errorf("declared-layer Path = %q, want %q", roots[0].Path, declared)
		}
		if roots[0].Source != worktreeroots.SourceSettingsKey {
			t.Errorf("declared-layer Source = %q, want %q", roots[0].Source, worktreeroots.SourceSettingsKey)
		}
		t.Logf("declared settings root = %s, Source = %s", roots[0].Path, roots[0].Source)
	})
}

// TestProductionWorktreeRootsAgreesWithTheResolverOverTheProcessEnvironment is
// the live half: whatever THIS process's environment declares, the production
// seam must answer exactly what worktreeroots.Resolve answers. It is the check
// that has no fixture at all, so it cannot be satisfied by a re-derived layer.
func TestProductionWorktreeRootsAgreesWithTheResolverOverTheProcessEnvironment(t *testing.T) {
	roots, err := productionWorktreeRoots("")
	if err != nil {
		t.Fatalf("production roots: %v", err)
	}
	want, err := worktreeroots.Resolve("")
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("production roots = %d entries, want exactly 1", len(roots))
	}
	if roots[0] != want {
		t.Fatalf("production roots[0] = {Path:%q Role:%q Source:%q}, resolver = {Path:%q Role:%q Source:%q}",
			roots[0].Path, roots[0].Role, roots[0].Source, want.Path, want.Role, want.Source)
	}
	if strings.TrimSpace(roots[0].Path) == "" {
		t.Fatalf("production roots[0].Path is empty — an empty root authorises nothing")
	}
	t.Logf("live environment: %s = %q -> root %s (Source = %s)",
		worktreeroots.WorktreeRootEnvName, os.Getenv(worktreeroots.WorktreeRootEnvName), roots[0].Path, roots[0].Source)
}

// mustResolve asks the ONE resolver and fails the test when it cannot answer.
func mustResolve(t *testing.T, declared string) worktreeroots.Root {
	t.Helper()
	root, err := worktreeroots.Resolve(declared)
	if err != nil {
		t.Fatalf("worktreeroots.Resolve(%q): %v", declared, err)
	}
	return root
}
