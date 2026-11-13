package config

// Per-repo build contract resolver tests (epic 2026-09-02): settings.json
// "repos" table → ResolveRepoBuild. Hermetic via GOPI_SETTINGS.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeReposSettings writes a settings.json carrying the given repos map.
func writeReposSettings(t *testing.T, repos map[string]RepoConfig) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body, err := json.Marshal(map[string]any{"repos": repos})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", path)
	return path
}

// TestResolveRepoBuildDeclared: an entry with a build command resolves with
// the default timeout.
func TestResolveRepoBuildDeclared(t *testing.T) {
	writeReposSettings(t, map[string]RepoConfig{
		"/repo/a": {Build: "make build"},
	})
	rc, found, err := ResolveRepoBuild("/repo/a")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected found for a declared repo")
	}
	if rc.Build != "make build" {
		t.Fatalf("build = %q, want %q", rc.Build, "make build")
	}
	if rc.BuildTimeoutSec != repoBuildTimeoutDefaultSec {
		t.Fatalf("timeout = %d, want default %d", rc.BuildTimeoutSec, repoBuildTimeoutDefaultSec)
	}
}

// TestResolveRepoBuildAbsent: a repo with no entry → found=false, no error.
func TestResolveRepoBuildAbsent(t *testing.T) {
	writeReposSettings(t, map[string]RepoConfig{
		"/repo/a": {Build: "make build"},
	})
	if _, found, err := ResolveRepoBuild("/repo/other"); err != nil || found {
		t.Fatalf("expected not-found with no error, got found=%v err=%v", found, err)
	}
}

// TestResolveRepoBuildEmptyBuildIsMergeOnly (architecture-fix-v2): an entry
// with NO build command is a valid PURE MERGE TARGET — the merger sweeps it
// but has no rebuild contract, so found=false, no error (the caller skips
// the rebuild silently). Before the config-driven-sweep redesign an empty
// entry was a config error; now it is a normal merge-only repo.
func TestResolveRepoBuildEmptyBuildIsMergeOnly(t *testing.T) {
	writeReposSettings(t, map[string]RepoConfig{
		"/repo/a": {},
	})
	rc, found, err := ResolveRepoBuild("/repo/a")
	if err != nil {
		t.Fatalf("merge-only repo must not error on build resolution: %v", err)
	}
	if found {
		t.Fatalf("merge-only repo (no build) must be found=false (no rebuild contract), got found rc=%+v", rc)
	}
}

// TestResolveRepoBuildTimeoutPrecedence: repo entry > env > default.
func TestResolveRepoBuildTimeoutPrecedence(t *testing.T) {
	// repo entry wins over env.
	writeReposSettings(t, map[string]RepoConfig{
		"/repo/a": {Build: "b", BuildTimeoutSec: 42},
	})
	t.Setenv(RepoBuildTimeoutEnv, "7")
	rc, _, err := ResolveRepoBuild("/repo/a")
	if err != nil {
		t.Fatal(err)
	}
	if rc.BuildTimeoutSec != 42 {
		t.Fatalf("timeout = %d, want repo entry 42 (entry > env)", rc.BuildTimeoutSec)
	}
	// env fills when the repo entry has no timeout.
	writeReposSettings(t, map[string]RepoConfig{
		"/repo/a": {Build: "b"},
	})
	rc, _, err = ResolveRepoBuild("/repo/a")
	if err != nil {
		t.Fatal(err)
	}
	if rc.BuildTimeoutSec != 7 {
		t.Fatalf("timeout = %d, want env 7 (env > default)", rc.BuildTimeoutSec)
	}
}

// ResolveMergerRepos tests (epic architecture-fix-v2, config-driven sweep):
// the settings.json "repos" table keys ARE the merger's per-tick sweep
// list — sorted for determinism, every key a sweep target (build contract
// optional; rebuilt only when declared).

// TestResolveMergerReposSorted: keys come back sorted regardless of map
// insertion order (a single-instance daemon's per-repo sweep must be
// reproducible across restarts).
func TestResolveMergerReposSorted(t *testing.T) {
	writeReposSettings(t, map[string]RepoConfig{
		"/repo/maps": {Build: "make maps"},
		"/repo/a":    {},
		"/repo/z":    {},
	})
	repos, err := ResolveMergerRepos()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/repo/a", "/repo/maps", "/repo/z"}
	if len(repos) != len(want) {
		t.Fatalf("got %d repos %v, want %d %v", len(repos), repos, len(want), want)
	}
	for i := range want {
		if repos[i] != want[i] {
			t.Fatalf("repos[%d] = %q, want %q (sorted)", i, repos[i], want[i])
		}
	}
}

// TestResolveMergerReposEmpty: no repos table / empty map → empty sweep
// list, nil error (the merger no-ops with nothing configured).
func TestResolveMergerReposEmpty(t *testing.T) {
	writeReposSettings(t, map[string]RepoConfig{})
	repos, err := ResolveMergerRepos()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("empty repos table must yield no sweep targets, got %v", repos)
	}
}
