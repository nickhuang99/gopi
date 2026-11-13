package scratch

// attachments_archive_test.go — the evidence bound over the sessions tree's
// `attachments/` archives, pinned on BOTH sides.
//
// WHAT IS ASSERTED, and why both halves are load-bearing:
//
//	KEEP   — an archive holding anything at all is untouched, whatever its age.
//	         The operator's principle is that an attachment can be the ONLY
//	         surviving copy of evidence (measured: branch-capture-list-windows.
//	         bundle is the last copy of an archived branch), so a rule that could
//	         take one is worse than no rule. The fixture plants the real shape:
//	         `<container>/attachments/home/nick/workspace/<name>.bundle`.
//	COLLECT — an ORDINARY, EMPTY, unheld, aged `attachments/` directory goes.
//	         It holds no evidence (nothing at all) and it is what the family
//	         costs: measured on this host 2026-10-02, 75,602 of 75,851 archives
//	         are empty.
//
// THE RED-PROOF DRIVES THE PRODUCTION PREDICATE, not a copy: it swaps the ONE
// decision function (attachmentsArchiveHoldsNoEvidence) for a neutered one that
// answers "holds nothing" for everything, then runs the REAL sweep
// (SweepAttachmentsArchive, through the row the registry holds) and requires the
// evidence-holding archive to be TAKEN. A green test whose red direction could
// not be produced proves nothing about the bound — and a red-proof that
// re-implemented the walk would prove nothing about the code that runs.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureArchiveContainer makes one session container holding an `attachments/`
// area, optionally with content planted inside it, and backdates the ARCHIVE
// directory past the tree's window. The container itself is never a candidate:
// this tree only ever takes the archive directory inside it.
func fixtureArchiveContainer(t *testing.T, sessions, container string, content ...string) string {
	t.Helper()
	archive := filepath.Join(sessions, container, AttachmentsArchiveDirName)
	if err := os.MkdirAll(archive, DirPerm); err != nil {
		t.Fatal(err)
	}
	for _, rel := range content {
		full := filepath.Join(archive, rel)
		if err := os.MkdirAll(filepath.Dir(full), DirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("evidence\n"), FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	// The AGE is the directory's own mtime, and it is written LAST: creating
	// content inside it moves its mtime forward again.
	ancient := time.Now().Add(-2 * SessionAttachmentsStaleAge)
	if err := os.Chtimes(archive, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	return archive
}

// fixtureSessionsTree pins GOPI_HOME to a fresh home and returns its sessions
// tree — the tree this row resolves (the same root the `session-raw` row uses,
// because a task's archive lives inside its own session container).
func fixtureSessionsTree(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	sessions := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessions, DirPerm); err != nil {
		t.Fatal(err)
	}
	root, err := SessionRawLogRoot()
	if err != nil {
		t.Fatalf("the sessions tree must resolve under a pinned GOPI_HOME: %v", err)
	}
	if root != sessions {
		t.Fatalf("the sessions tree resolves to %s, want %s — the row would sweep a different tree", root, sessions)
	}
	return sessions
}

// TestAttachmentsArchiveSweepPinsBothSides is the minimal repro: one fixture,
// both verdicts, driven end to end through the declared-tree registry's own row
// (so the plan face and the collect face are the SAME listing).
func TestAttachmentsArchiveSweepPinsBothSides(t *testing.T) {
	sessions := fixtureSessionsTree(t)

	empty := fixtureArchiveContainer(t, sessions, "01a0f4f9-empty-0000-0000-000000000001")
	// The operator's measured shape: a container whose archive holds the only
	// surviving copy of an archived branch.
	withBundle := fixtureArchiveContainer(t, sessions, "01a0f4f9-bundle-0000-0000-000000000002",
		filepath.Join("home", "nick", "workspace", "branch-capture-list-windows.bundle"))
	// Any content at all counts — this is the weakest form of "holds evidence".
	withByte := fixtureArchiveContainer(t, sessions, "01a0f4f9-byte-0000-0000-000000000003",
		"home/nick/workspace/gopi_list.txt")
	// A nested DIRECTORY with no file in it: still not nothing, still kept.
	nested := fixtureArchiveContainer(t, sessions, "01a0f4f9-nested-0000-0000-000000000004")
	if err := os.Mkdir(filepath.Join(nested, "sub"), DirPerm); err != nil {
		t.Fatal(err)
	}
	// A FRESH empty archive: the window is the last lever, and it must hold.
	fresh := fixtureArchiveContainer(t, sessions, "01a0f4f9-fresh--0000-0000-000000000005")
	now := time.Now()
	if err := os.Chtimes(fresh, now, now); err != nil {
		t.Fatal(err)
	}
	// A directory named like an archive but holding a LIVE holder registration:
	// the sweep's own liveness rule is inherited, never re-derived.
	held := fixtureArchiveContainer(t, sessions, "01a0f4f9-held--0000-0000-000000000006")
	RegisterHolder(held)
	t.Cleanup(func() { _ = os.Remove(filepath.Join(held, HolderFileName)) }) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertion it supports already ran

	tree, ok := treeByName(AttachmentsTreeName)
	if !ok {
		t.Fatalf("the declared set has no %q row — the bound is not wired", AttachmentsTreeName)
	}

	// THE PLAN, first: the face an operator vets the sweep with must show
	// exactly the EMPTY archive as a candidate and must NOT promise the ones
	// that hold evidence.
	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	planned := map[string]bool{}
	for _, e := range plan {
		planned[e.Path] = true
	}
	if !planned[empty] {
		t.Errorf("the plan does not list the ordinary empty archive %s — a bound an operator cannot see is a bound nobody can check", empty)
	}
	for _, keep := range []string{withBundle, withByte, nested, fresh, held} {
		if planned[keep] {
			t.Errorf("the plan lists %s as a candidate — it holds evidence (or is fresh, or is held) and must never be promised away", keep)
		}
	}

	removed, _, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != len(plan) {
		t.Errorf("the sweep removed %d while the plan listed %d — the two faces must be ONE listing", removed, len(plan))
	}
	if removed != 1 {
		t.Errorf("removed=%d, want exactly 1 (the ordinary empty archive)", removed)
	}

	// COLLECT side.
	if _, err := os.Lstat(empty); !os.IsNotExist(err) {
		t.Errorf("the ordinary empty archive %s survived the sweep (err=%v)", empty, err)
	}
	// KEEP side — and the KEEP side is checked on the CONTENT, not only on the
	// directory: the evidence file itself must still be readable.
	bundle := filepath.Join(withBundle, "home", "nick", "workspace", "branch-capture-list-windows.bundle")
	if _, err := os.Stat(bundle); err != nil {
		t.Errorf("the archive's evidence file %s is gone — this is the loss the operator's principle forbids: %v", bundle, err)
	}
	for _, keep := range []string{withBundle, withByte, nested, fresh, held} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("archive %s was removed although it must be kept: %v", keep, err)
		}
	}
	// And the containers themselves are never this tree's business.
	for _, container := range []string{"01a0f4f9-empty-0000-0000-000000000001", "01a0f4f9-bundle-0000-0000-000000000002"} {
		if _, err := os.Stat(filepath.Join(sessions, container)); err != nil {
			t.Errorf("the session container %s was removed — this tree takes the archive directory only: %v", container, err)
		}
	}
}

// TestAttachmentsArchiveRedProofTakesEvidenceWhenTheVerdictIsNeutered is the
// RED-PROOF: with the ONE decision function neutered (it answers "holds
// nothing" for every candidate), the REAL sweep must take an archive that DOES
// hold evidence. That is what makes the green run above worth something — it
// shows the emptiness verdict, not the test's own fixture, is what keeps the
// bundle on disk.
func TestAttachmentsArchiveRedProofTakesEvidenceWhenTheVerdictIsNeutered(t *testing.T) {
	sessions := fixtureSessionsTree(t)
	withBundle := fixtureArchiveContainer(t, sessions, "01a0f4f9-red----0000-0000-00000000000a",
		filepath.Join("home", "nick", "workspace", "branch-capture-list-windows.bundle"))

	real := attachmentsArchiveHoldsNoEvidence
	t.Cleanup(func() { attachmentsArchiveHoldsNoEvidence = real })
	attachmentsArchiveHoldsNoEvidence = func(string, time.Time) bool { return true }

	tree, ok := treeByName(AttachmentsTreeName)
	if !ok {
		t.Fatalf("the declared set has no %q row", AttachmentsTreeName)
	}
	removed, _, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed == 0 {
		t.Fatalf("with the emptiness verdict neutered the sweep still removed nothing — the predicate under test is not the one deciding, so the green run proves nothing")
	}
	if _, err := os.Lstat(withBundle); !os.IsNotExist(err) {
		t.Fatalf("the neutered verdict did NOT take the evidence-holding archive %s (err=%v) — the emptiness verdict is not what protects it", withBundle, err)
	}
}

// TestAttachmentsArchiveProbeLeavesNoTrace pins the ROOT CAUSE of a defect this
// branch measured on itself: the probe WRITES inside the candidate, and a
// directory's mtime moves when an entry inside it is created or removed — so a
// pass that only MEASURED made its own candidate read as FRESH for a whole
// window. The observed shape was `scratch-gc trees --dry-run` listing the archive
// as a candidate while the sweep immediately after it found nothing stale: the
// plan had bumped the very timestamp the age rule reads. The plan must leave both
// the timestamps and the contents exactly as it found them.
func TestAttachmentsArchiveProbeLeavesNoTrace(t *testing.T) {
	sessions := fixtureSessionsTree(t)
	archive := fixtureArchiveContainer(t, sessions, "01a0f4f9-trace-0000-0000-00000000000b")

	before, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachmentsArchivePlan(sessions, time.Now(), SessionAttachmentsStaleAge); err != nil {
		t.Fatalf("plan: %v", err)
	}
	after, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the plan moved the candidate's mtime (%v → %v): the probe left a trace, so the NEXT pass reads the archive as fresh and keeps it — a measured candidate would never be collected",
			before.ModTime(), after.ModTime())
	}
	entries, err := os.ReadDir(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the plan left %d entry/entries in the candidate — the probe must remove everything it created: %v", len(entries), entries)
	}
}

// TestAttachmentsArchiveRowIsDeclaredAndNamed pins the row's SHAPE: it is in the
// declared set, its namespace predicate takes the archive's own name and nothing
// else, its window is the named constant, and it roots at the sessions tree.
func TestAttachmentsArchiveRowIsDeclaredAndNamed(t *testing.T) {
	sessions := fixtureSessionsTree(t)
	tree, ok := treeByName(AttachmentsTreeName)
	if !ok {
		t.Fatalf("the declared set has no %q row", AttachmentsTreeName)
	}
	if tree.Class != TreeClassSessionArchive {
		t.Errorf("row class is %q, want %q — a reader of a log line must tell which bound a line reports", tree.Class, TreeClassSessionArchive)
	}
	if tree.StaleAge != SessionAttachmentsStaleAge {
		t.Errorf("row window is %v, want the named constant %v (Rules 1/13)", tree.StaleAge, SessionAttachmentsStaleAge)
	}
	if tree.EntryMatch == nil || !tree.EntryMatch(AttachmentsArchiveDirName) {
		t.Fatalf("the row's namespace predicate does not take %q — the tree could never collect its own family", AttachmentsArchiveDirName)
	}
	// A foreign name in the tree is never a candidate, whatever it holds.
	for _, foreign := range []string{"evidence", "raw", "attachments-old", ""} {
		if tree.EntryMatch(foreign) {
			t.Errorf("the namespace predicate takes %q — an aged operator directory in that container would become collectable", foreign)
		}
	}
	root, err := tree.Resolve()
	if err != nil {
		t.Fatalf("the row's resolver is unwired: %v", err)
	}
	if root != sessions {
		t.Errorf("the row roots at %s, want the sessions tree %s", root, sessions)
	}
	// The plan face must exist for a row that owns its listing: a nil List would
	// make --dry-run report nothing for a tree the sweep collects.
	if tree.List == nil || tree.Sweep == nil {
		t.Errorf("the row must own its listing and its collection (List+Sweep), or the plan and the sweep become two answers")
	}
}

// TestAttachmentsArchiveNameMatchesWriters pins the three spellings of the
// archive's name and the two of the tree's name to their production sources: the
// writer (internal/tools), the family registry (internal/sessionlayout) and this
// package. Each of the three cannot import the other (the tools layer imports
// this package, which would make a reverse import a cycle; the registry cannot
// import this package because importing it pulls the tools layer in), so the pin
// reads each SOURCE and compares the constants — the drift Rules 13/14 ban is
// invisible without it.
func TestAttachmentsArchiveNameMatchesWriters(t *testing.T) {
	repo := repoRootForDeclaredTreeGate(t)
	cases := []struct {
		name string
		rel  string
		want string
	}{
		// The writer's own constant: the directory the submit face fills.
		{"internal/tools/task_session.go", filepath.Join("internal", "tools", "task_session.go"),
			"taskAttachmentsDirName = \"" + AttachmentsArchiveDirName + "\""},
		// The family registry's re-homed value.
		{"internal/sessionlayout/families.go", filepath.Join("internal", "sessionlayout", "families.go"),
			"AttachmentsDirName = \"" + AttachmentsArchiveDirName + "\""},
		// The family registry's TREE name — the value this row's
		// AttachmentsTreeName must equal, since a `bound:` row naming a tree
		// that does not exist bounds nothing.
		{"internal/sessionlayout/families.go", filepath.Join("internal", "sessionlayout", "families.go"),
			"TreeSessionAttachments = \"" + AttachmentsTreeName + "\""},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			body := readRepoFile(t, repo, tc.rel)
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s no longer declares %q — the writer, the registry and the sweep have drifted apart (Rules 13/14)", tc.rel, tc.want)
			}
		})
	}
}
