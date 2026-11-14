package testutil

// WRITE-PATH tests for the worker-ownership pointer (Rule 5: every fix
// ships its minimal repro).
//
// THE DEFECT: the gate below reddened on EVERY merge round that landed a
// document teaching a launch shape (measured 7 -> 8 -> 11 -> 5 -> 5 -> 6 over
// seven master advances in one task), because the annotation was a manual
// afterthought nobody could know about before the gate reddened. The fix moves
// the annotation into the WRITE PATH (internal/tools' write/edit tools call
// workerownership.AnnotateOwnerDocs), which is only worth anything if the
// emitter and the gate agree — so these tests pin BOTH directions:
//
//	TestWorkerOwnershipWriterEmissionSatisfiesTheGate — the exact bytes the
//	  write path produces through the REAL tool must satisfy the gate's OWN
//	  decision (workerOwnershipUncovered), i.e. a document written through
//	  gopi can no longer be an offender.
//	TestWorkerOwnershipGateDecisionIsNotVacuouslyGreen — the red-proof: the
//	  same decision, on a fixture tree with a PLANTED offender, must still
//	  report it (asserting the plant landed first, or the proof measures
//	  nothing). A gate that cannot fail proves nothing about the writer.
//
// The fixture is a MINIMAL tree (devdoc/ + briefings/ + scripts/ under one
// t.TempDir) — the smallest sample that exercises the walk, the needles and
// the dismiss table, with no repository-wide scan (Rule 10.4).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/workerownership"
)

// workerOwnershipWriteFixture builds the smallest audited tree the gate's walk
// accepts and returns its root.
func workerOwnershipWriteFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range workerownership.AuditedRoots() {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("fixture dir %s: %v", dir, err)
		}
	}
	return root
}

// workerOwnershipTeachingDoc is a document that TEACHES a launch shape — the
// exact shape the gate exists to catch, written the way a real report writes
// it (the needle inside prose, not on a line of its own).
func workerOwnershipTeachingDoc() string {
	return "# Task Report: recycle the stale worker\n\n" +
		"The operator's launch move is `" + workerownership.Needles[0] + "` (or\n`" +
		workerownership.Needles[1] + " <id>`); an agent submits the work instead.\n"
}

// TestWorkerOwnershipWriterEmissionSatisfiesTheGate is the anti-drift test:
// the REAL write tool is handed a teaching document inside an audited tree,
// and the gate's OWN decision function must come back with nothing uncovered.
// If the writer and the gate ever disagree — a changed marker, a changed
// canonical path, a changed needle — this fails HERE, on the writer's output,
// rather than as a merge-round red on somebody else's branch.
func TestWorkerOwnershipWriterEmissionSatisfiesTheGate(t *testing.T) {
	root := workerOwnershipWriteFixture(t)
	path := filepath.Join(root, workerownership.DirDevdoc, "report.md")

	if err := tools.Write(path, workerOwnershipTeachingDoc()); err != nil {
		t.Fatalf("write through the REAL tool: %v", err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !workerownership.MentionsLaunchShape(string(onDisk)) {
		t.Fatalf("the fixture does not mention a launch shape — the test would pass vacuously:\n%s", onDisk)
	}
	if !workerownership.Annotated(string(onDisk)) {
		t.Errorf("the writer landed a teaching doc with NO canonical pointer — this is the merge-round red the write path exists to prevent:\n%s", onDisk)
	}

	missing, err := workerOwnershipUncovered(root)
	if err != nil {
		t.Fatalf("gate decision on the writer's output: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("the gate's own decision still reports the writer's output as uncovered: %v", missing)
	}

	// Idempotence: re-writing the file the tool just produced must not stack a
	// second pointer (the tool is called on every write, including rewrites).
	rewrite := "The operator's move is `" + workerownership.Needles[0] + "` again.\n\n" + string(onDisk)
	if err := tools.Write(path, rewrite); err != nil {
		t.Fatalf("second write: %v", err)
	}
	twice, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back after rewrite: %v", err)
	}
	if n := strings.Count(string(twice), workerownership.Marker); n != 1 {
		t.Errorf("the pointer appears %d times after a rewrite through the real tool, want exactly 1 (the injection must be idempotent):\n%s", n, twice)
	}
}

// TestWorkerOwnershipWriterEditsAreAnnotated is the second door: the edit tool
// reaches the atomic write directly, so it needs its own proof that a launch
// shape ADDED by an edit is annotated too (a pre-existing annotated file that
// is edited must also not lose or duplicate the pointer).
func TestWorkerOwnershipWriterEditsAreAnnotated(t *testing.T) {
	root := workerOwnershipWriteFixture(t)
	path := filepath.Join(root, workerownership.DirDevdoc, "edit-target.md")

	// A file that starts CLEAN (no launch shape, no pointer) — the edit below
	// introduces the shape.
	if err := os.WriteFile(path, []byte("# Notes\n\nThe queue is the handoff.\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := tools.Edit(path, "The queue is the handoff.", "Run `"+workerownership.Needles[0]+"` to drain it.", false); err != nil {
		t.Fatalf("edit through the REAL tool: %v", err)
	}
	edited, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !workerownership.MentionsLaunchShape(string(edited)) {
		t.Fatalf("the edit did not introduce a launch shape — vacuous test:\n%s", edited)
	}
	if !workerownership.Annotated(string(edited)) {
		t.Errorf("an edit that introduces a launch shape landed NO pointer:\n%s", edited)
	}
	if n := strings.Count(string(edited), workerownership.Marker); n != 1 {
		t.Errorf("the pointer appears %d times after one edit, want 1:\n%s", n, edited)
	}
}

// TestWorkerOwnershipGateDecisionIsNotVacuouslyGreen is the RED-PROOF: with a
// PLANTED offender in the fixture the gate's decision must name it. Without
// this, the test above could pass because the decision is blind — the classic
// vacuous green.
func TestWorkerOwnershipGateDecisionIsNotVacuouslyGreen(t *testing.T) {
	root := workerOwnershipWriteFixture(t)
	offender := filepath.Join(root, workerownership.DirScripts, "planted.sh")
	planted := "#!/usr/bin/env bash\n# teaches the move\n" + workerownership.Needles[0] + "\n"
	if err := os.WriteFile(offender, []byte(planted), 0o755); err != nil {
		t.Fatalf("plant: %v", err)
	}
	// Assert the plant LANDED before trusting the verdict (the 2026-09-13
	// lesson: a silent non-write makes a red-proof measure nothing).
	if _, err := os.Stat(offender); err != nil {
		t.Fatalf("the planted offender is not on disk — the red-proof would measure nothing: %v", err)
	}

	missing, err := workerOwnershipUncovered(root)
	if err != nil {
		t.Fatalf("gate decision: %v", err)
	}
	want := filepath.ToSlash(filepath.Join(workerownership.DirScripts, "planted.sh"))
	found := false
	for _, rel := range missing {
		if rel == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the planted offender %s is NOT reported (reported: %v) — the gate decision is no longer a gate", want, missing)
	}

	// And the same tree, made clean by the WRITER, must come back empty —
	// i.e. the offender's disappearance is caused by the annotation, not by
	// the decision going blind.
	if err := tools.Write(offender, planted); err != nil {
		t.Fatalf("annotate through the real tool: %v", err)
	}
	missing, err = workerOwnershipUncovered(root)
	if err != nil {
		t.Fatalf("gate decision after annotation: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("the writer annotated the offender but the gate still reports it: %v", missing)
	}
}

// TestWorkerOwnershipInjectionIsScopedAndIdempotent pins the NO-OP half: off
// the audited scope, or on a document that teaches nothing, the write path
// must not touch the bytes (an annotation sprinkled into every file would make
// the marker meaningless — and would corrupt captured transcripts).
//
// Its last case is the REAL-TREE red-proof, merged in here because it asks the
// same question with real input: the canonical block — the repository's own
// document, which names every launch shape — with its pointer lines stripped
// and planted at an audited location, is a GENUINE offender and must be
// reported by the gate's own decision; the same bytes at a non-audited
// location must be left alone. (Writing to the two places and asserting both
// halves beats damaging the repository, which a gate whose only callable form
// reads the real tree would require.)
func TestWorkerOwnershipInjectionIsScopedAndIdempotent(t *testing.T) {
	teaching := workerOwnershipTeachingDoc()
	already := "# Doc\n\n" + workerownership.CommentLine(workerownership.SuffixDoc) + "\n\n" + teaching

	cases := []struct {
		name, path, content string
		want                bool
	}{
		{"off-scope tree", filepath.Join("other", "report.md"), teaching, false},
		{"off-scope code file", filepath.Join(workerownership.DirDevdoc, "snippet.go"), teaching, false},
		{"in-scope but teaches nothing", filepath.Join(workerownership.DirDevdoc, "quiet.md"), "# Doc\n\nno launch shape here.\n", false},
		{"captured transcript is never injected", filepath.Join(workerownership.DirDevdoc, "capture.txt"), teaching, false},
		{"already annotated", filepath.Join(workerownership.DirDevdoc, "done.md"), already, false},
		{"shell teaching", filepath.Join(workerownership.DirScripts, "run.sh"), "#!/usr/bin/env bash\necho " + workerownership.Needles[0] + "\n", true},
		{"markdown teaching", filepath.Join(workerownership.DirBriefings, "brief.md"), teaching, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := workerownership.AnnotateOwnerDocs(tc.path, tc.content)
			if changed != tc.want {
				t.Fatalf("changed=%v, want %v (path %s)", changed, tc.want, tc.path)
			}
			if !tc.want {
				if got != tc.content {
					t.Errorf("a no-op changed the bytes:\n--- got ---\n%s", got)
				}
				return
			}
			if !workerownership.Annotated(got) {
				t.Fatalf("the emitted content does not satisfy the gate's predicate:\n%s", got)
			}
			// The pointer must be IN the comment form of the kind: an HTML
			// comment in markdown renders invisible; a `#` line in a script is
			// a comment line. A shell script's shebang must also survive as
			// line 1 (a shell reads it to pick the interpreter).
			if strings.HasSuffix(tc.path, workerownership.SuffixSh) {
				if !strings.Contains(got, "\n"+workerownership.CommentLine(workerownership.SuffixSh)) {
					t.Errorf("shell annotation is not a `#` comment line:\n%s", got)
				}
				if !strings.HasPrefix(got, "#!") {
					t.Errorf("the shebang is no longer the first line:\n%s", got)
				}
			} else if !strings.Contains(got, workerownership.CommentLine(workerownership.SuffixDoc)) {
				t.Errorf("markdown annotation is not an HTML comment:\n%s", got)
			}
		})
	}
}

// TestWorkerOwnershipGateStillReddensOnAPlantedOffenderInTheRealTree is the
// demanded "a planted offender still reddens the gate" proof at the GATE's own
// entry point (not only its pure decision): the smallest possible damage is
// done to a scratch fixture, so the real repository tree is never touched.
func TestWorkerOwnershipGateStillReddensOnAPlantedOffenderInTheRealTree(t *testing.T) {
	srcRoot, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	root := workerOwnershipWriteFixture(t)
	// The canonical block names every launch shape, so it is the most
	// realistic offender material there is. Read the REAL file, strip its
	// pointer (every line carrying the marker), and plant the result in the
	// fixture: a 100%-faithful teaching document without the annotation.
	body, err := os.ReadFile(filepath.Join(srcRoot, filepath.FromSlash(workerownership.CanonicalRel)))
	if err != nil {
		t.Fatalf("read the canonical block: %v", err)
	}
	var kept []string
	strippedPointer := 0
	for _, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, workerownership.Marker) {
			strippedPointer++
			continue
		}
		kept = append(kept, line)
	}
	stripped := strings.Join(kept, "\n")
	if strippedPointer == 0 {
		t.Fatalf("the plant stripped no pointer line from %s — the red-proof would measure nothing", workerownership.CanonicalRel)
	}
	if !workerownership.MentionsLaunchShape(stripped) {
		t.Fatalf("the de-pointered canonical block no longer mentions a launch shape — vacuous plant")
	}
	if workerownership.Annotated(stripped) {
		t.Fatalf("the plant left a pointer line — the red-proof would measure nothing")
	}

	dst := filepath.Join(root, workerownership.DirDevdoc, "copy.md")
	if err := os.WriteFile(dst, []byte(stripped), 0o644); err != nil {
		t.Fatalf("plant: %v", err)
	}
	missing, err := workerOwnershipUncovered(root)
	if err != nil {
		t.Fatalf("gate decision: %v", err)
	}
	want := filepath.ToSlash(filepath.Join(workerownership.DirDevdoc, "copy.md"))
	found := false
	for _, rel := range missing {
		if rel == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("a planted offender made of the canonical block is not reported (reported: %v)", missing)
	}

	// And the writer clears it: the SAME bytes through the real tool are no
	// longer an offender, which is the whole claim of this task.
	if err := tools.Write(dst, stripped); err != nil {
		t.Fatalf("annotate through the real tool: %v", err)
	}
	missing, err = workerOwnershipUncovered(root)
	if err != nil {
		t.Fatalf("gate decision after annotation: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("the writer annotated the planted offender but the gate still reports it: %v", missing)
	}
}
