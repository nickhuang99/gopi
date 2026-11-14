// sessions_tree_families_test.go — THE RANGE GATE: what the sessions tree
// actually HOLDS, checked against the declared decisions.
//
// WHY THIS GATE EXISTS (the defect it closes, measured 2026-10-01). The
// declared-tree gate (TestDeclaredTreesCoverRepoWriters) is ANCHOR-driven: it
// checks that the trees on a HAND-WRITTEN writer list are declared. It can
// refuse a writer whose tree is undeclared, and it does that well. What it
// cannot see is everything nobody wrote a row for: measured on this host, its
// 8-row anchor list covered `*.raw.log` (45.8 GiB / 1655 files) while 202,534
// files / 9.3 GiB of the SAME tree — 75,843 `attachments/` directories, 79,401
// `metadata.json`, per-run working directories, a whole probe checkout — were
// bounded by nothing and named by nothing. An anchor list answers "is every tree
// we know about covered?"; it can never answer "is there a tree we do not know
// about?" — that is a question about the RANGE, and only a walk can ask it.
//
// WHAT IT CHECKS, in two halves:
//
//	GREEN (the range): every entry a real sessions tree yields is classified by
//	  the registry in internal/sessionlayout, and every `bound:` family names a
//	  tree that really exists in DeclaredTrees().
//	RED (the proof): the same scan, driven with a REDUCED registry, must report
//	  the dropped family as UNCLASSIFIED — and a NEW family landed in a fixture
//	  tree must be red too. A gate that cannot be shown red is not a gate.
//
// The LIVE tree is walked only when it is present, and only as a CORROBORATION:
// it is REPORTED, never asserted. A live tree's contents are the operator's
// state, not this repository's contract, and a gate that failed because a run
// parked a file in the sessions tree would be refusing the wrong party. What is
// asserted on the live tree is only that the walk FINISHED and read a non-zero
// number of entries (a walk that read nothing must never pass as "nothing
// unclassified").
//
// The fixture tree is the CONTRACT: one entry per measured family, built from
// the registry's own name constants wherever they exist — so the fixture and the
// live tree cannot describe different families.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// The fixture tree's ingredients: the name of every family the registry
// registers, plus the shapes the registry does NOT know (the red-proof's new
// family). Values come from the registry's own constants wherever the writer's
// value lives there; the rest are the measured shapes of a probe's residue.
const (
	// fixtureSessionID / fixtureRunUUID / fixtureTaskID are the three container
	// names the ledger and the pre-session layout mint (measured shapes).
	fixtureSessionID = "01a0f4f9-baf5-7f13-a707-d05a68ea12e4"
	fixtureRunUUID   = "2247d577-8044-43c5-bcc1-512642883822"
	fixtureTaskID    = "01a0f4f9baf57f13a707d05a68ea12e4"
	// fixtureTaskEpochDir / fixtureProcessDir are the two task-named container
	// shapes.
	fixtureTaskEpochDir = "t-1788897004"
	fixtureProcessDir   = "process-12345"
	// fixtureCheckoutDir is a marker-named container holding a checkout — the
	// family that makes a sessions tree grow by hundreds of megabytes.
	fixtureCheckoutDir = "smoke-clone"
	// fixtureSandboxDir is a container holding a private profile.
	fixtureSandboxDir = "sandbox-home"
	// fixtureStrayDir is a hand-run probe's directory at the root.
	fixtureStrayDir = "zz-live-probe"
	// fixtureStrayFile is a file parked directly in the sessions root.
	fixtureStrayFile = "note.txt"
	// fixtureScratchDir is a run's own working area inside its container.
	fixtureScratchDir = "evidence"
	// fixtureRawLogName is a per-run raw HTTP log's file name.
	fixtureRawLogName = fixtureRunUUID + ".raw.log"
	// fixtureCrashDumpName is a per-run crash dump's file name.
	fixtureCrashDumpName = fixtureRunUUID + ".dump.json"
	// fixtureRootDumpName is an orphaned dump at the root (its container gone).
	fixtureRootDumpName = fixtureSessionID + ".dump.json"
	// fixtureUnknownDir / fixtureUnknownFile are the shapes the registry does
	// NOT know: the red-proof's own new family, landed at the root.
	fixtureUnknownDir  = "brand-new-family"
	fixtureUnknownFile = "unheard-of-artifact.bin"
)

// The two marker children the checkout and profile families test for.
const (
	fixtureGitMarker     = ".git"
	fixtureProfileMarker = ".gopi"
)

// mkdirFixture makes a directory (and its parents).
func mkdirFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// writeFixtureFile makes a file with a little content (the classifier is a NAME
// rule, so the content is irrelevant — the point is that the walk never opens
// it).
func writeFixtureFile(t *testing.T, path string) {
	t.Helper()
	mkdirFixture(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// seedSessionsFixture writes ONE entry per measured family into root: every
// family the registry registers is represented, so both the green gate and each
// red-proof have a subject.
func seedSessionsFixture(t *testing.T, root string) {
	t.Helper()

	// A session container with every container-level family inside it.
	session := filepath.Join(root, fixtureSessionID)
	mkdirFixture(t, session)
	writeFixtureFile(t, filepath.Join(session, sessionlayout.RecordName))
	writeFixtureFile(t, filepath.Join(session, fixtureRunUUID+sessionlayout.JSONLSuffix))
	writeFixtureFile(t, filepath.Join(session, fixtureRawLogName))
	writeFixtureFile(t, filepath.Join(session, fixtureCrashDumpName))
	writeFixtureFile(t, filepath.Join(session, sessionlayout.MetadataFileName))
	writeFixtureFile(t, filepath.Join(session, sessionlayout.SideFileNames()[0]))
	writeFixtureFile(t, filepath.Join(session, "task-report.md"))
	writeFixtureFile(t, filepath.Join(session, "output.log"))
	writeFixtureFile(t, filepath.Join(session, "chat-screen.dump"))
	mkdirFixture(t, filepath.Join(session, sessionlayout.AttachmentsDirName))
	mkdirFixture(t, filepath.Join(session, fixtureScratchDir))

	// A probe's checkout and a sandbox profile, INSIDE a container (the live
	// tree's shape: a run clones or materialises a profile in its own session
	// directory). Both are marker-named containers.
	mkdirFixture(t, filepath.Join(session, fixtureCheckoutDir, fixtureGitMarker))
	mkdirFixture(t, filepath.Join(session, fixtureSandboxDir, fixtureProfileMarker))

	// The root-level container families the ledger mints.
	mkdirFixture(t, filepath.Join(root, fixtureTaskID))
	mkdirFixture(t, filepath.Join(root, fixtureTaskEpochDir))
	mkdirFixture(t, filepath.Join(root, fixtureProcessDir))

	// The measured residue families: a hand-run probe's directory, a file
	// parked directly in the root.
	mkdirFixture(t, filepath.Join(root, fixtureStrayDir))
	writeFixtureFile(t, filepath.Join(root, fixtureStrayFile))
}

// TestSessionsTreeFamiliesAreClassified is THE GATE. It walks a fixture tree
// holding one entry per measured family and refuses any entry the registry does
// not classify, then cross-checks the `bound:` families against the LIVE
// declared set.
func TestSessionsTreeFamiliesAreClassified(t *testing.T) {
	root := t.TempDir()
	seedSessionsFixture(t, root)

	scan, err := sessionlayout.ScanFamilies(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if scan.Files == 0 || scan.Dirs == 0 {
		t.Fatalf("the fixture walk read nothing (files=%d dirs=%d) — a gate whose subject is empty cannot be green",
			scan.Files, scan.Dirs)
	}
	reportUnclassified(t, "fixture", scan)
	assertBoundFamiliesNameDeclaredTrees(t, sessionlayout.BoundTreeNames(), declaredTreeNames())
}

// TestSessionsTreeGateIsRedWhenAFamilyIsDropped is the RED-PROOF of the gate
// above: it drives the SAME scan and the SAME classifier over a registry with
// one family removed, and requires the entries that family owned to come back
// UNCLASSIFIED. A green gate that could not be shown red is not a gate.
//
// The case the epic names is `attachments` — the family that grew to 75,843
// directories unbounded — so it is first in the table.
func TestSessionsTreeGateIsRedWhenAFamilyIsDropped(t *testing.T) {
	root := t.TempDir()
	seedSessionsFixture(t, root)

	// Each case names the family AND the CATCH-ALL that shadows it. A
	// container's own file/dir shapes are claimed by a last-resort family by
	// design (measured: hundreds of one-off probe directory names inside
	// containers, which no registry of meaningful families could enumerate), so
	// a specific family row is load-bearing AGAINST that shadow rather than
	// against silence — and the proof of "no row decides this" is to drop the
	// pair. The assertion is always the same and always the strong one: with
	// those rows gone, the dropped family's entries come back UNCLASSIFIED.
	drops := []struct {
		name   string
		family string
		shadow string // the catch-all that would otherwise absorb the entries ("" = none)
		want   string // a path fragment whose entries must become unclassified
	}{
		{"the attachments archive loses its decision", sessionlayout.FamilyAttachmentsDir, sessionlayout.FamilySessionScratchDir, sessionlayout.AttachmentsDirName},
		{"the session record loses its decision", sessionlayout.FamilySessionRecord, sessionlayout.FamilyRunEvidence, sessionlayout.RecordName},
		{"the raw HTTP log loses its declaration", sessionlayout.FamilyRawHTTPLog, sessionlayout.FamilyRunEvidence, ".raw.log"},
		{"the metadata record loses its decision", sessionlayout.FamilySessionMetadata, sessionlayout.FamilyRunEvidence, sessionlayout.MetadataFileName},
		{"the checkout family loses its decision", sessionlayout.FamilyForeignCheckout, sessionlayout.FamilySessionScratchDir, filepath.Join(fixtureSessionID, fixtureCheckoutDir)},
		{"the container family loses its decision", sessionlayout.FamilySessionContainer, "", fixtureSessionID},
		{"the task-container family loses its decision", sessionlayout.FamilyTaskContainer, sessionlayout.FamilyRootStrayDir, fixtureTaskEpochDir},
	}
	for _, tc := range drops {
		t.Run(tc.name, func(t *testing.T) {
			drop := []string{tc.family}
			if tc.shadow != "" {
				drop = append(drop, tc.shadow)
			}
			reduced, ok := reducedFamilies(drop...)
			if !ok {
				t.Fatalf("one of %v is not in the registry — the red-proof cannot drop what does not exist", drop)
			}
			scan, err := sessionlayout.ScanFamiliesWith(root, reduced)
			if err != nil {
				t.Fatalf("scan %s: %v", root, err)
			}
			if len(scan.Unclassified) == 0 {
				t.Fatalf("the walk stayed GREEN with %v removed — the gate is not red-provable", drop)
			}
			var hit bool
			for _, u := range scan.Unclassified {
				if strings.Contains(u.Path, tc.want) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("dropping %v reported %d unclassified entries, none of which was %q — the gate failed for the wrong reason",
					drop, len(scan.Unclassified), tc.want)
			}
		})
	}

	// The control: with the REAL registry the same tree is GREEN. A "gate" that
	// refuses everything is not a gate either.
	scan, err := sessionlayout.ScanFamilies(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if len(scan.Unclassified) != 0 {
		t.Fatalf("the real registry leaves %d entries unclassified in its own fixture: %+v",
			len(scan.Unclassified), scan.Unclassified)
	}
}

// TestSessionsTreeGateIsRedForANewFamily pins the REGRESSION the epic asks for:
// a writer that lands a family the registry does not know — a new directory
// shape and a new file shape at the root — must make the gate red, so the tree
// can never grow a family in silence again.
func TestSessionsTreeGateIsRedForANewFamily(t *testing.T) {
	root := t.TempDir()
	seedSessionsFixture(t, root)

	// The control run: the fixture as seeded is green.
	before, err := sessionlayout.ScanFamilies(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if len(before.Unclassified) != 0 {
		t.Fatalf("the seeded fixture is not clean: %+v", before.Unclassified)
	}

	mkdirFixture(t, filepath.Join(root, fixtureUnknownDir))
	writeFixtureFile(t, filepath.Join(root, fixtureUnknownFile))

	after, err := sessionlayout.ScanFamilies(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	shapes := after.UnclassifiedShapes()
	for _, want := range []string{fixtureUnknownDir + "/", fixtureUnknownFile} {
		if shapes[want] == 0 {
			t.Fatalf("a new family (%q) landed in the tree and the gate stayed GREEN (unclassified shapes: %v) — silence is the defect this gate exists to refuse",
				want, shapes)
		}
	}
	// And the failure line the gate WOULD print must name the shape, so an
	// operator reads a family rather than a file count. The rendering is
	// asserted, not triggered: the gate's own red path is exercised by the
	// fixture above, and a test that fails itself on purpose cannot be told
	// apart from one that caught a genuine defect.
	for _, want := range []string{fixtureUnknownDir + "/", fixtureUnknownFile} {
		if firstPathOfShape(after, want) == "" {
			t.Errorf("the gate could not name the first path of unclassified shape %q — a report that names no path cannot be acted on", want)
		}
	}
}

// TestSessionsTreeGateOnTheLiveTree corroborates the fixture against the LIVE
// sessions tree. It is REPORT-ONLY by construction: the live tree is the
// operator's state, so its unclassified entries are logged (t.Logf) and never
// turned into a failure. What IS asserted is that the walk ran and read the
// tree — a walk that read nothing must never be mistaken for a clean tree.
func TestSessionsTreeGateOnTheLiveTree(t *testing.T) {
	root, err := SessionRawLogRoot()
	if err != nil {
		t.Skipf("the sessions tree is not configured on this host: %v", err)
	}
	if fi, statErr := os.Stat(root); statErr != nil || !fi.IsDir() {
		t.Skipf("no sessions tree at %s on this host (%v)", root, statErr)
	}
	// The live tree is walked ONCE and reported. It is not marked heavy: it
	// stats ~250k entries in ~80k directories (measured: seconds), never opens a
	// file and reads no bytes — Rule 27 bans generating DEVICE LOAD, and this
	// walk creates none.
	scan, err := sessionlayout.ScanFamilies(root)
	if err != nil {
		t.Fatalf("scan live tree %s: %v", root, err)
	}
	if scan.Files+scan.Dirs == 0 {
		t.Fatalf("the live walk at %s read nothing — a census that read nothing must not read as a clean tree", root)
	}
	t.Logf("live sessions tree %s: %d shapes, %d files, %d dirs, %d bytes, %d entries unclassified",
		root, len(scan.ByShape), scan.Files, scan.Dirs, scan.Bytes, len(scan.Unclassified))
	for shape, n := range scan.UnclassifiedShapes() {
		t.Logf("live tree UNCLASSIFIED shape %q — %d entries: the registry has no decision for it (Rule 15: reported, never ignored)",
			shape, n)
	}
}

// reducedFamilies returns the registry with one family removed. ok is false when
// that family is not registered — the red-proof must not be able to "drop" a
// family that does not exist and call the resulting green a proof.
func reducedFamilies(drop ...string) ([]sessionlayout.Family, bool) {
	all := sessionlayout.Families()
	out := make([]sessionlayout.Family, 0, len(all))
	missing := make([]string, 0, len(drop))
	for _, name := range drop {
		if _, ok := sessionlayout.FamilyByName(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return out, false
	}
	gone := make(map[string]bool, len(drop))
	for _, name := range drop {
		gone[name] = true
	}
	for _, f := range all {
		if gone[f.Name] {
			continue
		}
		out = append(out, f)
	}
	return out, true
}

// reportUnclassified renders a scan's unclassified entries as gate failures.
// They are reported by SHAPE (one line per family) so a tree that grew a family
// produces one actionable line rather than a hundred thousand.
func reportUnclassified(t *testing.T, label string, scan sessionlayout.FamilyScan) {
	t.Helper()
	for shape, n := range scan.UnclassifiedShapes() {
		t.Errorf("%s tree holds %d entries of shape %q that NO family claims (first: %s) — register it in internal/sessionlayout.families with an explicit bound/keep/legacy decision",
			label, n, shape, firstPathOfShape(scan, shape))
	}
}

// firstPathOfShape names one path carrying a shape, so the failure line points
// at something a reader can open.
func firstPathOfShape(scan sessionlayout.FamilyScan, shape string) string {
	for _, u := range scan.Unclassified {
		key := u.Shape
		if u.Dir {
			key += "/"
		}
		if key == shape {
			return u.Path
		}
	}
	return ""
}

// assertBoundFamiliesNameDeclaredTrees is the cross-check: a family that says
// `bound:<tree>` must name a tree the declared set REALLY holds, and that tree
// must root at the sessions tree itself. It is what makes "declared but not
// swept" and "bound to a tree that does not exist" both impossible to pass.
func assertBoundFamiliesNameDeclaredTrees(t *testing.T, bound []string, declared []string) {
	t.Helper()
	if len(bound) == 0 {
		t.Fatalf("no sessions-tree family is `bound:` — the ONE bounded family (*.raw.log) has lost its declaration")
	}
	live := make(map[string]bool, len(declared))
	for _, name := range declared {
		live[name] = true
	}
	for _, name := range bound {
		if !live[name] {
			t.Errorf("family decision broken: the registry binds a family to declared tree %q, which DeclaredTrees() does not hold (it holds %v) — a bound naming a tree that does not exist bounds nothing",
				name, declared)
		}
	}
	{
		// The other direction for the tree the registry names: it must be the
		// SESSIONS tree, not some other tree that happens to carry the name.
		declaredRoots := DeclaredTreeRoots()
		sessionsRoot, err := SessionRawLogRoot()
		if err != nil {
			t.Skipf("the sessions tree is not configured on this host: %v", err)
		}
		for _, name := range bound {
			root, ok := declaredRoots[name]
			if !ok {
				continue // already reported above
			}
			// A tree rooted at the sessions tree itself covers part of the
			// SESSIONS TREE's entries; the archive tree roots there too (a
			// task's archive lives inside its session container, S1). A tree
			// rooted in a DIFFERENT directory still fails.
			if root != sessionsRoot {
				t.Errorf("declared tree %q roots at %s, but the sessions tree is %s — a family bound to it would bound a different tree",
					name, root, sessionsRoot)
			}
		}
	}
}
