package config

// The clone channel's two knobs: dev_repo is the SWITCH (non-empty = clone
// channel), workspace_root is the reclaimable parent, and the defaults are the
// compatibility contract — an unset pair must reproduce today's behaviour.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// devRepoTestSettings writes a settings.json into a temp dir and points
// GOPI_SETTINGS at it for the duration of the test.
func devRepoTestSettings(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsEnv, path)
}

// TestCloneChannelDefaultIsOffWithoutChangingAnything is the compatibility
// clause: with neither key set, the channel is OFF and the workspace root is
// the SAME default the worktree channel uses.
func TestCloneChannelDefaultIsOffWithoutChangingAnything(t *testing.T) {
	devRepoTestSettings(t, `{"model":"x"}`)
	t.Setenv(DevRepoEnvName, "")
	t.Setenv(WorkspaceRootEnvName, "")

	on, _, err := CloneChannelConfigured()
	if err != nil {
		t.Fatalf("CloneChannelConfigured: %v", err)
	}
	if on {
		t.Fatal("dev_repo unset must mean the clone channel is OFF (today's worktree channel)")
	}
	root, source, err := WorkspaceRootConfigured()
	if err != nil {
		t.Fatalf("WorkspaceRootConfigured: %v", err)
	}
	if source != "default" {
		t.Fatalf("source = %q, want the documented default", source)
	}
	// The DEFAULT is worktreeroots.DefaultRoot() — <home>/workspace, or the
	// suite's pinned root under test. Asserting the same function is what
	// makes the claim "the clone channel lands where the worktree channel
	// does" checkable without hard-coding a path.
	want, derr := worktreeroots.DefaultRoot()
	if derr != nil {
		t.Fatalf("worktreeroots.DefaultRoot: %v", derr)
	}
	if root != want {
		t.Fatalf("workspace root = %q, want the worktree channel's default %q", root, want)
	}
}

// TestDevRepoSettingsValueTurnsTheSwitchOn proves the switch is the VALUE, not
// a second boolean.
func TestDevRepoSettingsValueTurnsTheSwitchOn(t *testing.T) {
	devRepoTestSettings(t, `{"dev_repo":"peer1:/home/user/work/gopi"}`)
	t.Setenv(DevRepoEnvName, "")

	got, source, err := DevRepoConfigured()
	if err != nil {
		t.Fatalf("DevRepoConfigured: %v", err)
	}
	if got != "peer1:/home/user/work/gopi" {
		t.Fatalf("dev_repo = %q, want the declared host:path", got)
	}
	if !strings.Contains(source, DevRepoSettingsKey) {
		t.Fatalf("source = %q, want it to name the settings key", source)
	}
	on, _, err := CloneChannelConfigured()
	if err != nil || !on {
		t.Fatalf("a non-empty dev_repo must turn the channel ON (on=%v err=%v)", on, err)
	}
}

// TestDevRepoEnvOutranksSettings is the precedence rule every knob in this
// package follows.
func TestDevRepoEnvOutranksSettings(t *testing.T) {
	devRepoTestSettings(t, `{"dev_repo":"peer1:/settings/path"}`)
	t.Setenv(DevRepoEnvName, "hp:/env/path")

	got, source, err := DevRepoConfigured()
	if err != nil {
		t.Fatalf("DevRepoConfigured: %v", err)
	}
	if got != "hp:/env/path" || source != DevRepoEnvName {
		t.Fatalf("dev_repo = (%q,%q), want the env channel to win", got, source)
	}
}

// TestWorkspaceRootEnvAndSettingsAreCanonicalized proves the tilde spelling and
// the absolute spelling resolve to the SAME path (one reader, Rule 2).
func TestWorkspaceRootEnvAndSettingsAreCanonicalized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	devRepoTestSettings(t, `{"workspace_root":"~/ws-settings"}`)

	got, source, err := WorkspaceRootConfigured()
	if err != nil {
		t.Fatalf("WorkspaceRootConfigured: %v", err)
	}
	if got != filepath.Join(home, "ws-settings") {
		t.Fatalf("workspace_root = %q, want %q", got, filepath.Join(home, "ws-settings"))
	}
	if !strings.Contains(source, WorkspaceRootSettingsKey) {
		t.Fatalf("source = %q, want the settings key", source)
	}

	t.Setenv(WorkspaceRootEnvName, "~/ws-env")
	got, source, err = WorkspaceRootConfigured()
	if err != nil {
		t.Fatalf("WorkspaceRootConfigured (env): %v", err)
	}
	if got != filepath.Join(home, "ws-env") || source != WorkspaceRootEnvName {
		t.Fatalf("workspace_root = (%q,%q), want the env channel to win", got, source)
	}
}

// TestCloneChannelBrokenSettingsIsAnError refuses rather than silently picking
// a channel: falling back would start a run in a workspace nobody declared.
func TestCloneChannelBrokenSettingsIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsEnv, path)
	t.Setenv(DevRepoEnvName, "")

	if _, _, err := CloneChannelConfigured(); err == nil {
		t.Fatal("a settings file that cannot be parsed must be an ERROR, not a silent fallback")
	}
}

// TestCloneChannelKeysAreCatalogued pins Rule 2/14: both env names the channel
// reads are catalog rows, so `gopi --help-env` and CONFIG.md can name them.
func TestCloneChannelKeysAreCatalogued(t *testing.T) {
	for _, want := range []string{DevRepoEnvName, WorkspaceRootEnvName} {
		found := false
		for _, row := range EnvVars {
			if row.Env == want {
				found = true
				if row.Key == "" {
					t.Fatalf("catalog row %s carries no settings key", want)
				}
				if row.Help == "" {
					t.Fatalf("catalog row %s carries no help text", want)
				}
			}
		}
		if !found {
			t.Fatalf("%s is read by the tree but is not a catalog row", want)
		}
	}
}

// THE BUG THIS FILE'S LAST ARMS EXIST FOR (measured 2026-11-01, task
// 01a1945d): WorkspaceRootConfigured used to pin the SOURCE label to whichever
// of ITS OWN keys it had just read and then ask worktreeroots.Resolve for the
// PATH — and that resolver consults GOPI_WORKTREE_ROOT FIRST. With both
// variables set, the PATH came from GOPI_WORKTREE_ROOT while the SOURCE still
// read "GOPI_WORKSPACE_ROOT": a run reported a channel that had not decided
// anything, so an operator (or a fixture author) following the label
// investigated the wrong variable. Nothing failed, so nothing said so (Rule 15).
//
//	measured before the fix, both set to different directories:
//	  path="/tmp/probe-worktree-root" source="GOPI_WORKSPACE_ROOT" err=<nil>
//
// The FOUR arms below pin the three states that matter, and each one asserts the
// PAIR (path, source), never the source alone — a label is only wrong relative
// to the value it claims to explain.
func TestWorkspaceRootSourceNamesTheChannelThatDecided(t *testing.T) {
	// No settings file declares a workspace root: these arms are about the two
	// environment channels and nothing else.
	devRepoTestSettings(t, `{}`)

	// ① BOTH SET, DIFFERENT DIRECTORIES — one setting with two values. The
	// resolver's own branch (GOPI_WORKTREE_ROOT) is in force, and the label must
	// say so AND name the declaration that lost, so the reader is never sent to
	// the variable that carried nothing.
	t.Run("both set to different directories name the branch in force", func(t *testing.T) {
		declared := filepath.Join(t.TempDir(), "declared-workspace")
		ambient := filepath.Join(t.TempDir(), "ambient-worktree")
		t.Setenv(WorkspaceRootEnvName, declared)
		t.Setenv(worktreeroots.WorktreeRootEnvName, ambient)

		path, source, err := WorkspaceRootConfigured()
		if err != nil {
			t.Fatalf("WorkspaceRootConfigured: %v", err)
		}
		if path != filepath.Clean(ambient) {
			t.Fatalf("path = %q, want the branch in force %q", path, ambient)
		}
		// THE PAIR IS THE ASSERTION: the label must name the channel the path
		// came from. A label naming GOPI_WORKSPACE_ROOT alone is the reported
		// bug, whatever the path says.
		if !strings.Contains(source, worktreeroots.WorktreeRootEnvName) {
			t.Fatalf("source = %q, want it to name %q — the channel that decided the path",
				source, worktreeroots.WorktreeRootEnvName)
		}
		// And the declaration that lost is NAMED with its value, so a reader
		// learns both facts from one line (Rule 15: no silent disagreement).
		for _, want := range []string{WorkspaceRootEnvName, declared} {
			if !strings.Contains(source, want) {
				t.Errorf("source = %q, want it to name the outranked declaration %q", source, want)
			}
		}
	})

	// ② BOTH SET, THE SAME DIRECTORY — not a conflict. This is the shape every
	// fixture uses (pin both names at one temp dir so nothing can reach the
	// operator's real workspace), and a refusal here would be a false alarm on
	// the documented recipe.
	t.Run("both set to the same directory are not a conflict", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "one-root")
		t.Setenv(WorkspaceRootEnvName, root)
		t.Setenv(worktreeroots.WorktreeRootEnvName, root)

		path, source, err := WorkspaceRootConfigured()
		if err != nil {
			t.Fatalf("the same directory through both names is not a conflict: %v", err)
		}
		if path != filepath.Clean(root) {
			t.Fatalf("path = %q, want %q", path, root)
		}
		for _, want := range []string{WorkspaceRootEnvName, worktreeroots.WorktreeRootEnvName} {
			if !strings.Contains(source, want) {
				t.Errorf("source = %q, want it to name %q (both channels agreed, so both belong in the label)", source, want)
			}
		}
	})

	// ③ ONLY GOPI_WORKSPACE_ROOT — unchanged behaviour: the declared channel
	// decides, and the label names it.
	t.Run("the workspace channel alone still decides", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "declared-only")
		t.Setenv(WorkspaceRootEnvName, root)
		t.Setenv(worktreeroots.WorktreeRootEnvName, "")

		path, source, err := WorkspaceRootConfigured()
		if err != nil {
			t.Fatalf("WorkspaceRootConfigured: %v", err)
		}
		if path != filepath.Clean(root) || source != WorkspaceRootEnvName {
			t.Fatalf("(path,source) = (%q,%q), want (%q,%q)", path, source, filepath.Clean(root), WorkspaceRootEnvName)
		}
	})

	// ④ ONLY GOPI_WORKTREE_ROOT — THE ORIGINAL LIE, in its purest form. The
	// PATH comes from that variable, so the SOURCE must name it; the old code
	// printed "GOPI_WORKSPACE_ROOT" here for a value GOPI_WORKSPACE_ROOT never
	// carried.
	t.Run("the ambient override is named as itself", func(t *testing.T) {
		ambient := filepath.Join(t.TempDir(), "ambient-only")
		t.Setenv(WorkspaceRootEnvName, "")
		t.Setenv(worktreeroots.WorktreeRootEnvName, ambient)

		path, source, err := WorkspaceRootConfigured()
		if err != nil {
			t.Fatalf("WorkspaceRootConfigured: %v", err)
		}
		if path != filepath.Clean(ambient) {
			t.Fatalf("path = %q, want the ambient override %q", path, ambient)
		}
		if source != WorkspaceRootSourceWorktreeEnv {
			t.Fatalf("source = %q, want %q", source, WorkspaceRootSourceWorktreeEnv)
		}
		if strings.Contains(source, WorkspaceRootEnvName) {
			t.Fatalf("source = %q names %s, which carried no value at all — this is the reported bug", source, WorkspaceRootEnvName)
		}
	})
}
