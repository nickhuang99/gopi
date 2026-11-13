package tools

// Durable close-loop mark tests (epic 2026-09-16, branch fix/merge-rebuild-loop).
//
// WHAT THIS PINS: the mark is the close loop's memory ACROSS PROCESS LIFETIMES —
// a landing whose rebuild could not run yet (the pass that landed it was
// interrupted inside the verify judge, or is still inside it) must still be
// rebuilt by whoever comes next. The mechanism is asserted against a REAL
// settings "repos" entry and a REAL GOPI_HOME control dir, because both are what
// the merger actually reads: the mark is only useful if a successor process can
// find it, name the repo, and have that repo carry a declared build.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rebuildPendingFixtureRepo creates a repo directory plus a settings file that
// declares it in the "repos" table, and isolates GOPI_HOME (where the marks
// live: <GOPI_HOME>/merger). Returns the repo path.
func rebuildPendingFixtureRepo(t *testing.T, withContract bool) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	body := `{"repos":{}}`
	if withContract {
		body = `{"repos":{"` + repo + `":{"build":"true"}}}`
	}
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())
	return repo
}

// TestMarkRebuildPendingNamesTheRepoTheDrainCanRecover: the mark must be
// recoverable by a DIFFERENT process — the whole point (the lander is gone).
// Recovery is by reading the mark, so the repo path must be IN the file: the
// filename's digest is one-way.
func TestMarkRebuildPendingNamesTheRepoTheDrainCanRecover(t *testing.T) {
	repo := rebuildPendingFixtureRepo(t, true)
	MarkRebuildPending(repo, "gate landed branch-x while waiting for the judge")

	got := RebuildPendingRepos()
	if len(got) != 1 || got[0] != repo {
		t.Fatalf("RebuildPendingRepos() = %v, want exactly [%s] — a successor pass must be able to recover the repo the mark names", got, repo)
	}
	// The reason must survive too: it is what tells the operator WHY this repo
	// was one merge behind.
	path, err := rebuildPendingPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "gate landed branch-x") {
		t.Fatalf("the mark carries no reason: %q", raw)
	}
}

// TestMarkRebuildPendingRefusesWithoutBuildContract: a repo with no declared
// build can NEVER be rebuilt, so a mark for it would be a file no drain could
// ever discharge. Same deliberate silence serveRebuild keeps.
func TestMarkRebuildPendingRefusesWithoutBuildContract(t *testing.T) {
	repo := rebuildPendingFixtureRepo(t, false)
	MarkRebuildPending(repo, "should not be recorded")
	if got := RebuildPendingRepos(); len(got) != 0 {
		t.Fatalf("a repo with no declared build was marked (%v) — no drain can clear it, so it would accumulate forever", got)
	}
}

// TestClearRebuildPendingDischargesTheObligation: after the drain builds the
// repo, the mark must be gone — otherwise every later pass rebuilds a repo that
// is already current (harmless but noisy, and it hides a real failure).
func TestClearRebuildPendingDischargesTheObligation(t *testing.T) {
	repo := rebuildPendingFixtureRepo(t, true)
	MarkRebuildPending(repo, "landing")
	ClearRebuildPending(repo)
	if got := RebuildPendingRepos(); len(got) != 0 {
		t.Fatalf("the mark survived its own clear (%v) — the close loop would re-run forever", got)
	}
	// Clearing twice is not an error (every landing face clears after its own
	// build, and two faces may describe one repo) — it must simply be a no-op.
	ClearRebuildPending(repo)
}

// TestRebuildPendingMarksArePerRepo: two repos that share a leaf name (the
// ordinary case on this host: a repo checked out twice) must not share a mark —
// a digest of the ABSOLUTE path is what keeps one repo's drain from rebuilding
// another's.
func TestRebuildPendingMarksArePerRepo(t *testing.T) {
	repoA := rebuildPendingFixtureRepo(t, true)
	base := t.TempDir()
	repoB := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoB, 0o755); err != nil {
		t.Fatal(err)
	}
	// Re-declare BOTH repos (repoA's settings file is overwritten with a table
	// naming both, which is what a host with two task repos has).
	settingsPath := os.Getenv("GOPI_SETTINGS")
	body := `{"repos":{"` + repoA + `":{"build":"true"},"` + repoB + `":{"build":"true"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	MarkRebuildPending(repoA, "landing A")
	MarkRebuildPending(repoB, "landing B")
	got := RebuildPendingRepos()
	if len(got) != 2 {
		t.Fatalf("RebuildPendingRepos() = %v, want both repos — same leaf name must not collapse into one mark", got)
	}
	ClearRebuildPending(repoA)
	got = RebuildPendingRepos()
	if len(got) != 1 || got[0] != repoB {
		t.Fatalf("after clearing A, RebuildPendingRepos() = %v, want exactly [%s] — clearing one repo must not discharge another", got, repoB)
	}
}

// TestRebuildPendingReposIgnoresForeignAndEmptyMarks: the scan reads a directory
// that also holds the merger's state/request files, so it must select ONLY mark
// names — and a mark it cannot name a repo for is reported and skipped, never
// guessed at.
func TestRebuildPendingReposIgnoresForeignAndEmptyMarks(t *testing.T) {
	rebuildPendingFixtureRepo(t, true)
	dir, err := RebuildPendingDir()
	if err != nil {
		t.Fatal(err)
	}
	// A foreign control file (the shape the directory really contains). The
	// merger publishes no per-instance record any more (epic
	// merger-ff-park-file), so the foreign neighbour here is an arbitrary file
	// in that directory — which is exactly what the selector must ignore.
	if err := os.WriteFile(filepath.Join(dir, "some-foreign-file"), []byte(`{"pid":1234}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A mark with no repo in it: unattributable, so it must be skipped rather
	// than turned into a rebuild of some empty path.
	if err := os.WriteFile(filepath.Join(dir, RebuildPendingPrefix+"deadbeef"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RebuildPendingRepos(); len(got) != 0 {
		t.Fatalf("RebuildPendingRepos() = %v, want none — a foreign file and an unattributable mark are not repos", got)
	}
}
