package devworkspace

// ground_facts_test.go — the regression for the SENTENCE defect (row 01a17a48).
//
// THE DEFECT, MEASURED. A "Ground facts" summary asserted, in one sentence, both
// that a directory was "a LINKED WORKTREE (.git is a FILE)" and that "No rebase
// in progress (no .git/rebase-merge|apply)". The second clause is unsound for
// the shape the first clause claims: under a `.git` FILE, `.git/rebase-merge`
// can NEVER exist, so that probe answers "no rebase" vacuously — it is not a
// measurement at all. The source was on disk in the same run: an `ls -d
// .git/rebase-merge` that exited 2 short-circuited an `&&` chain straight into
// the `||` arm, which printed ".git is FILE = linked worktree" for a CLONE
// (`.git` a DIRECTORY). A failed probe became a shape assertion.
//
// WHY IT MATTERS BEYOND THE WORDING: Rule 6c gives the two shapes OPPOSITE
// delivery instructions — a clone's commit is owed by the channel's delivery
// loop, a linked worktree's commit is already in the shared repo. A run that
// believes the wrong one reports finished without publishing.
//
// WHAT THIS FILE PINS, on REAL git, for BOTH shapes:
//
//  1. the shape clause comes from GroundModelOf's answer (clone vs linked, and
//     NEVER one while the other is true);
//  2. the rebase probe is asked of GIT (`git rev-parse --git-path`), so it works
//     on the shape where a hand-joined `.git/rebase-merge` cannot;
//  3. a rebase that IS in progress is reported as in progress — on BOTH shapes,
//     which is the arm a path-joined probe fails on the linked one;
//  4. a probe that CANNOT run reports UNDETERMINED — never "no rebase"
//     (Rule 15: an unmeasured factor is never the reassuring verdict);
//  5. the delivery clause is empty when the shape is undetermined (unknown is
//     never a default).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// groundFactsCloneFixture builds the CLONE shape: a standalone clone of a
// repository, so `.git` is a DIRECTORY and the directory is registered nowhere.
func groundFactsCloneFixture(t *testing.T) string {
	t.Helper()
	repo := groundTestRepo(t)
	ws := filepath.Join(t.TempDir(), "workspace-clone")
	gitIn(t, filepath.Dir(ws), "clone", "-q", repo, ws)
	info, err := os.Stat(filepath.Join(ws, GroundGitEntryName))
	if err != nil || !info.IsDir() {
		t.Fatalf("the fixture must be the CLONE shape (.git a DIRECTORY): err=%v", err)
	}
	return ws
}

// groundFactsLinkedFixture builds the LINKED shape: a registered linked
// worktree, so `.git` is a FILE.
func groundFactsLinkedFixture(t *testing.T) string {
	t.Helper()
	main := groundTestRepo(t)
	wt := filepath.Join(filepath.Dir(main), "wt-linked")
	gitIn(t, main, "worktree", "add", "-q", wt, "-b", "feat/ground-facts-linked")
	info, err := os.Stat(filepath.Join(wt, GroundGitEntryName))
	if err != nil || info.IsDir() {
		t.Fatalf("the fixture must be the LINKED shape (.git a FILE): err=%v", err)
	}
	return wt
}

// startRebase marks a rebase in progress the way git itself does — by creating
// the state directory git resolves for THIS working tree — and returns the
// marker path that was created so the caller can point at it in a failure.
func startRebase(t *testing.T, dir string) string {
	t.Helper()
	probe := GroundRebaseProbe(dir)
	if probe.State != RebaseStateNone {
		t.Fatalf("the fixture must start with NO rebase; probe = %+v", probe)
	}
	if err := os.MkdirAll(probe.MergePath, 0o755); err != nil {
		t.Fatalf("materialise the rebase marker %s: %v", probe.MergePath, err)
	}
	return probe.MergePath
}

// TestGroundFactsLineRendersTheShapeTheDirectoryHas is arms 1+2: for BOTH ground
// shapes the rendered sentence names that shape, and the rebase clause is a
// MEASURED "none" whose evidence names the paths git resolved — on the linked
// shape those paths are NOT `<dir>/.git/…`, which is exactly the fact the defect
// sentence had wrong.
func TestGroundFactsLineRendersTheShapeTheDirectoryHas(t *testing.T) {
	for _, tc := range []struct {
		name    string
		build   func(*testing.T) string
		model   GroundModel
		want    string
		notWant string
	}{
		{name: "clone", build: groundFactsCloneFixture, model: GroundModelClone,
			want: "is a CLONE", notWant: "is a LINKED WORKTREE"},
		{name: "linked-worktree", build: groundFactsLinkedFixture, model: GroundModelLinkedWorktree,
			want: "is a LINKED WORKTREE", notWant: "is a CLONE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.build(t)
			// THE SHAPE HALF COMES FROM THE ONE DISCRIMINATOR — asserted, not
			// assumed, so this test fails the day a second opinion appears.
			if got := GroundModelOf(dir); got != tc.model {
				t.Fatalf("GroundModelOf(%s) = %q, want %q", dir, got, tc.model)
			}
			line := GroundFactsLine(dir)
			if !strings.HasPrefix(line, GroundFactsLabel+" ") {
				t.Fatalf("the line must carry its label %q, got %q", GroundFactsLabel, line)
			}
			if !strings.Contains(line, tc.want) {
				t.Errorf("the shape clause must say %q, got:\n%s", tc.want, line)
			}
			if strings.Contains(line, tc.notWant) {
				t.Errorf("the sentence must not claim the OTHER shape (%q), got:\n%s", tc.notWant, line)
			}
			// THE REBASE HALF IS A MEASUREMENT, and its evidence is the resolved
			// path — the fact a `<dir>/.git/rebase-merge` guess does not have.
			if !strings.Contains(line, "no rebase in progress") {
				t.Errorf("a clean tree must read as no rebase, got:\n%s", line)
			}
			probe := GroundRebaseProbe(dir)
			if probe.State != RebaseStateNone {
				t.Fatalf("probe = %+v, want none", probe)
			}
			for _, path := range []string{probe.MergePath, probe.ApplyPath} {
				if strings.TrimSpace(path) == "" {
					t.Fatalf("the probe must NAME the path git resolved, got %+v", probe)
				}
				if !strings.Contains(line, path) {
					t.Errorf("the rebase clause must quote the resolved path %q so the claim is checkable, got:\n%s", path, line)
				}
			}
			if !strings.Contains(line, "git rev-parse --git-path") {
				t.Errorf("the clause must name the command it ran (Rule 31/15), got:\n%s", line)
			}
		})
	}
}

// TestGroundRebaseProbeSeesARebaseOnBothShapes is arm 3, and it is the arm a
// hand-joined probe CANNOT pass on the linked shape: there, `.git` is a FILE, so
// `<dir>/.git/rebase-merge` never exists even while git says a rebase is in
// progress. The measured marker path for the linked shape lives under the MAIN
// checkout, which is what `--git-path` answers and what a guess misses.
func TestGroundRebaseProbeSeesARebaseOnBothShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*testing.T) string
		// outsideDirGit: the linked shape's marker is NOT inside <dir>/.git —
		// the fact that makes the naive probe fail there.
		outsideDirGit bool
	}{
		{name: "clone", build: groundFactsCloneFixture, outsideDirGit: false},
		{name: "linked-worktree", build: groundFactsLinkedFixture, outsideDirGit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.build(t)
			marker := startRebase(t, dir)
			naive := filepath.Join(dir, GroundGitEntryName, rebaseMergeGitPathName)
			if tc.outsideDirGit {
				if marker == naive {
					t.Fatalf("the linked shape's marker must NOT be the hand-joined path %s — that is the "+
						"path a naive probe would stat and always miss", naive)
				}
				if _, err := os.Stat(naive); err == nil {
					t.Fatalf("a naive stat of %s must MISS on the linked shape, got a hit", naive)
				}
			}
			probe := GroundRebaseProbe(dir)
			if probe.State != RebaseStateInProgress {
				t.Fatalf("an in-progress rebase must be reported as in progress; probe = %+v", probe)
			}
			if !strings.Contains(probe.Detail, marker) {
				t.Errorf("the detail must name the marker it found (%q), got %q", marker, probe.Detail)
			}
			line := GroundFactsLine(dir)
			if !strings.Contains(line, "A REBASE IS IN PROGRESS") {
				t.Errorf("the rendered sentence must say the rebase is in progress, got:\n%s", line)
			}
			if strings.Contains(line, "no rebase in progress") {
				t.Errorf("the sentence must NOT say there is no rebase while one is in progress, got:\n%s", line)
			}
		})
	}
}

// TestGroundRebaseProbeReportsUndeterminedRatherThanNone is arm 4: a directory
// git cannot answer about is UNDETERMINED, and the rendered sentence says so
// instead of printing the reassuring "no rebase" — the exact substitution the
// defect made by accident.
func TestGroundRebaseProbeReportsUndeterminedRatherThanNone(t *testing.T) {
	// A bare directory: no repository here and none above it (t.TempDir() sits
	// under the scratch root, which is not a git work tree).
	dir := t.TempDir()
	probe := GroundRebaseProbe(dir)
	if probe.State != RebaseStateUndetermined {
		t.Fatalf("a non-repository directory must be UNDETERMINED, never none; probe = %+v", probe)
	}
	if !strings.Contains(probe.Detail, "UNDETERMINED") {
		t.Errorf("the detail must say the state is undetermined, got %q", probe.Detail)
	}
	line := GroundFactsLine(dir)
	if !strings.Contains(line, "rebase state UNDETERMINED") {
		t.Errorf("the rendered sentence must say the rebase state is undetermined, got:\n%s", line)
	}
	if strings.Contains(line, "no rebase in progress") {
		t.Errorf("an unmeasured rebase state must NOT be rendered as no rebase, got:\n%s", line)
	}
	// AND THE SHAPE IS UNDETERMINED TOO, said as such: no .git entry here.
	if !strings.Contains(line, "UNDETERMINED shape") {
		t.Errorf("a directory with no .git entry must render an undetermined SHAPE, got:\n%s", line)
	}
	// Arm 5: no delivery instruction may be inferred from an undetermined shape.
	st := ResolveGroundState(dir)
	if st.Model == GroundModelClone || st.Model == GroundModelLinkedWorktree {
		t.Fatalf("the fixture must not be a decidable ground; model = %q", st.Model)
	}
	if c := st.GroundFactsDeliveryClause(); c != "" {
		t.Errorf("an undetermined shape must yield NO delivery clause (unknown is not a default), got %q", c)
	}
}

// TestGroundFactsDeliveryClauseIsTheOppositeForTheTwoShapes is the REASON the
// sentence must be measured rather than asserted: Rule 6c's two instructions are
// opposites, so a run told the wrong one either pushes what the channel owns or
// reports finished with the commit unpublished.
func TestGroundFactsDeliveryClauseIsTheOppositeForTheTwoShapes(t *testing.T) {
	clone := ResolveGroundState(groundFactsCloneFixture(t))
	if clone.Model != GroundModelClone {
		t.Fatalf("fixture model = %q, want %q", clone.Model, GroundModelClone)
	}
	cloneClause := clone.GroundFactsDeliveryClause()
	if !strings.Contains(cloneClause, "clone channel") || !strings.Contains(cloneClause, "do NOT push") {
		t.Fatalf("a CLONE ground's delivery clause must send the run to the channel and refuse the push, got %q", cloneClause)
	}

	linked := ResolveGroundState(groundFactsLinkedFixture(t))
	if linked.Model != GroundModelLinkedWorktree {
		t.Fatalf("fixture model = %q, want %q", linked.Model, GroundModelLinkedWorktree)
	}
	linkedClause := linked.GroundFactsDeliveryClause()
	if !strings.Contains(linkedClause, "no push is needed") {
		t.Fatalf("a LINKED worktree's delivery clause must say no push is needed, got %q", linkedClause)
	}
	if cloneClause == linkedClause {
		t.Fatal("the two shapes MUST get opposite delivery clauses — one instruction for both is the defect")
	}
}

// TestGroundFactsLineIsTheOppositeOnTheShapeIsolatedByTheFailedShellProbe pins
// the ACTUAL incident: the same shell idiom the misreporting run used — an `ls`
// of the naive rebase path driving an `&&`/`||` chain — prints the WRONG shape
// on a CLONE, while the rendered line prints the right one. This is the
// reproducer, kept as a test so the shape of the mistake cannot come back
// unnoticed.
func TestGroundFactsLineIsTheOppositeOnTheShapeIsolatedByTheFailedShellProbe(t *testing.T) {
	dir := groundFactsCloneFixture(t)
	// The naive probe's own verdict, measured the way the shell idiom reaches it:
	// the marker does not exist, so `ls` fails — and in `A && B || C` that
	// failure selects C, which asserted the LINKED shape.
	naive := filepath.Join(dir, GroundGitEntryName, rebaseMergeGitPathName)
	if _, err := os.Stat(naive); err == nil {
		t.Fatalf("the naive probe %s must MISS on a clone with no rebase (that miss is what misrouted the chain)", naive)
	}
	// What the shell printed (the wrong arm), versus what the measured renderer
	// prints for the same directory at the same moment.
	wrong := ".git is FILE = linked worktree"
	line := GroundFactsLine(dir)
	if strings.Contains(line, wrong) {
		t.Fatalf("the rendered sentence reproduced the incident's wrong arm on a CLONE:\n%s", line)
	}
	if !strings.Contains(line, "is a CLONE") {
		t.Fatalf("the rendered sentence must state the measured shape, got:\n%s", line)
	}
	if got := GroundModelOf(dir); got != GroundModelClone {
		t.Fatalf("GroundModelOf(%s) = %q — the ONE discriminator must say clone here", dir, got)
	}
}
