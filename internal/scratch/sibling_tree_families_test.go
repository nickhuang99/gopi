// sibling_tree_families_test.go — THE RANGE GATE for the gopi profile's SIBLING
// TREES: `archive/`, `backups/`, `notify/`, `dumps/`.
//
// WHY IT EXISTS. The family registry (internal/sessionlayout/families.go)
// answers "what does the SESSIONS TREE hold?" for ONE tree. The profile holds
// four more trees beside it, and until this epic they were measured by a `find`
// typed into a report — an ANCHOR, describing what somebody already noticed. The
// measurement that made the gap concrete is the `dumps/` discrepancy: the dump
// writers document their default as the scratch dumps class tree, the profile's
// `dumps/` nevertheless grew to 4,345 files / 102 MiB in a day, and no report
// could say WHY, because nothing walked the tree and asked which family each
// entry belonged to. The answer — an operator's `GOPI_ERROR_DUMP_DIR` in their
// shell profile, i.e. the env OVERRIDE and not a code path — is readable off a
// census in one line, and invisible to any anchor.
//
// WHAT IT CHECKS, in two halves (the same two halves the sessions-tree gate has,
// and for the same reason):
//
//	GREEN (the range): every entry a real sibling tree yields is classified by
//	  that tree's registry, and every `bound:` family names a declared tree that
//	  really exists in DeclaredTrees().
//	RED (the proof): the same scan, driven with a REDUCED registry, must report
//	  the dropped family as UNCLASSIFIED — and a NEW family landed in the
//	  fixture must be red too. A gate that cannot be shown red is not a gate.
//
// The LIVE trees are REPORT-ONLY (the operator's state, not this repository's
// contract), exactly as the sessions-tree gate treats its own live tree.
//
// THE FIXTURE IS THE CONTRACT, and its ingredients come from the registry's own
// constants wherever the registry has one, so a fixture and a live tree cannot
// describe different families.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionlayout"
)

// The fixture's seed names, one per family the sibling registries claim. They
// are the MEASURED live shapes (2026-10-01) wherever the family's identity is a
// shape and not a constant the writer owns.
const (
	// fixtureArchiveTarball / fixtureArchiveTarGz are the two tarball spellings.
	fixtureArchiveTarball = "sandbox-chat-shot-20260930-215147.tgz"
	fixtureArchiveTarGz   = "sandbox-chat-shot-20260930-215147.tar.gz"
	// fixtureArchiveExport is a raw export a probe wrote for a human.
	fixtureArchiveExport = "legacy-t-rows-export.tsv"
	// fixtureArchiveRescueDir is an unpacked rescue.
	fixtureArchiveRescueDir = "workspace-rescue-20260920"
	// fixtureBackupSnapshot is the writer's own artifact name
	// (`memory.db.backup-<stamp>.db`).
	fixtureBackupSnapshot = "memory.db.backup-20260929-194458.db"
	// fixtureBackupStaging is the same artifact under the staging suffix.
	fixtureBackupStaging = "memory.db.backup-20260929-194458.db.partial"
	// fixtureBackupSidecar is SQLite's sidecar of a snapshot.
	fixtureBackupSidecar = "memory.db.backup-20260929-194458.db-wal"
	// fixtureBackupForeign is a hand-made snapshot prefix.
	fixtureBackupForeign = "memory.db.bak-latprov-20260911-125436"
	// fixtureBackupWorktreeResidue is an archived worktree residue directory.
	fixtureBackupWorktreeResidue = "wt-sweep-20260912"
	// fixtureNotifyStream / fixtureNotifyStreamPeer are the operator's feed and
	// the same feed mirrored from the peer.
	fixtureNotifyStream     = "task-events.jsonl"
	fixtureNotifyStreamPeer = "task-events.hp.jsonl"
	// fixtureNotifyLog is a hand-run watcher's log beside the feed.
	fixtureNotifyLog = "night-watch-merger.log"
	// fixtureDumpName / fixtureDumpCollisionName are the two dump name shapes
	// writeDumpFile mints (bare stamp, and stamp + collision suffix).
	fixtureDumpName          = "20260929-194458.dump.json"
	fixtureDumpCollisionName = "20260929-194458-1.dump.json"
	// fixtureSiblingUnknownDir / fixtureSiblingUnknownFile are the shapes the
	// sibling registries do NOT know: each red-proof's own new family.
	fixtureSiblingUnknownDir  = "brand-new-sibling-family"
	fixtureSiblingUnknownFile = "unheard-of-sibling-artifact.bin"
	// fixtureSiblingForeignNote is a file an operator parked in a sibling tree.
	// It is deliberately NOT in the fixture's seeded set: it is added by the
	// red-proof that proves an unclaimed shape is REPORTED rather than absorbed.
	fixtureSiblingForeignNote = "operator-note.txt"
)

// siblingFixtureProfile builds a profile root holding every sibling tree,
// seeded with ONE entry per family their registries claim.
func siblingFixtureProfile(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	for _, name := range sessionlayout.SiblingTreeNames() {
		mkdirFixture(t, filepath.Join(profile, name))
	}
	archive := filepath.Join(profile, sessionlayout.SiblingTreeArchive)
	writeFixtureFile(t, filepath.Join(archive, fixtureArchiveTarball))
	writeFixtureFile(t, filepath.Join(archive, fixtureArchiveTarGz))
	writeFixtureFile(t, filepath.Join(archive, fixtureArchiveExport))
	mkdirFixture(t, filepath.Join(archive, fixtureArchiveRescueDir))

	backups := filepath.Join(profile, sessionlayout.SiblingTreeBackups)
	writeFixtureFile(t, filepath.Join(backups, fixtureBackupSnapshot))
	writeFixtureFile(t, filepath.Join(backups, fixtureBackupStaging))
	writeFixtureFile(t, filepath.Join(backups, fixtureBackupSidecar))
	writeFixtureFile(t, filepath.Join(backups, fixtureBackupForeign))
	mkdirFixture(t, filepath.Join(backups, fixtureBackupWorktreeResidue))

	notify := filepath.Join(profile, sessionlayout.SiblingTreeNotify)
	writeFixtureFile(t, filepath.Join(notify, fixtureNotifyStream))
	writeFixtureFile(t, filepath.Join(notify, fixtureNotifyStreamPeer))
	writeFixtureFile(t, filepath.Join(notify, fixtureNotifyLog))

	// BOTH dump trees hold the same family (one registry row serves the two
	// knobs), so both are seeded: the `dumps-1214` tree exists on this host only
	// because its own knob points it, and a fixture that skipped it would leave
	// the gate's subject empty for a tree the census really walks.
	for _, tree := range []string{sessionlayout.SiblingTreeDumps, sessionlayout.SiblingTreeDumps1214} {
		dumps := filepath.Join(profile, tree)
		writeFixtureFile(t, filepath.Join(dumps, fixtureDumpName))
		writeFixtureFile(t, filepath.Join(dumps, fixtureDumpCollisionName))
	}
	return profile
}

// siblingFixtureRoot resolves one fixture tree's root by joining the REGISTRY's
// own tree name onto the fixture profile — so the fixture cannot describe a tree
// under a name the registry does not carry.
func siblingFixtureRoot(t *testing.T, profile, tree string) string {
	t.Helper()
	if _, ok := sessionlayout.SiblingTreeByName(tree); !ok {
		t.Fatalf("the sibling registry does not carry a tree named %q — the fixture cannot seed one", tree)
	}
	return filepath.Join(profile, tree)
}

// scanSiblingFixture walks one fixture tree with the registry's REAL family list
// for that tree, at the root the caller names.
//
// The root is passed in rather than resolved through the seam: the seam points
// at the operator's profile (wired by internal/config), and a gate must never
// walk the operator's trees to assert its own contract. The seam's own
// behaviour is pinned where the wiring lives — internal/config's
// TestProfileSiblingTreeResolversAreWired — so nothing here is unchecked, it is
// checked once in the one place that can decide it.
func scanSiblingFixture(t *testing.T, tree, root string) sessionlayout.SiblingScan {
	t.Helper()
	st, ok := sessionlayout.SiblingTreeByName(tree)
	if !ok {
		t.Fatalf("sibling tree %q is not registered", tree)
	}
	scan, err := sessionlayout.ScanSiblingTreeWith(tree, root, st.Families)
	if err != nil {
		t.Fatalf("scan %s at %s: %v", tree, root, err)
	}
	return scan
}

// TestSiblingTreeFamiliesAreClassified is THE GATE. For each of the profile's
// sibling trees it walks a fixture holding one entry per family that tree's
// registry claims, refuses any entry the registry does not classify, and
// cross-checks the `bound:` families against the LIVE declared set.
func TestSiblingTreeFamiliesAreClassified(t *testing.T) {
	profile := siblingFixtureProfile(t)
	classified := 0
	for _, tree := range sessionlayout.SiblingTreeNames() {
		scan := scanSiblingFixture(t, tree, siblingFixtureRoot(t, profile, tree))
		if scan.Files+scan.Dirs == 0 {
			t.Fatalf("the %s fixture walk read nothing — a gate whose subject is empty cannot be green", tree)
		}
		classified += len(scan.ByShape)
		reportUnclassified(t, tree, widenSiblingScanForGate(scan))
	}
	if classified < len(sessionlayout.SiblingTreeNames()) {
		t.Fatalf("the fixture produced %d shapes across %d trees — every tree must yield at least one family",
			classified, len(sessionlayout.SiblingTreeNames()))
	}
	assertSiblingBoundFamiliesNameDeclaredTrees(t, sessionlayout.SiblingBoundTreeNames(), declaredTreeNames())
}

// TestSiblingTreeGateIsRedWhenAFamilyIsDropped is the RED-PROOF of the gate
// above: it drives the SAME scan and the SAME classifier over a registry with
// one family removed, and requires the entries that family owned to come back
// UNCLASSIFIED.
//
// Every case here is LOAD-BEARING — there is no catch-all family in these
// registries, so a dropped row means its entries are reported rather than
// absorbed. That is the property the sessions-tree registry could only prove by
// dropping a row AND its shadow; here the drop alone is the proof, and the gate
// asserts it for one family per tree, INCLUDING the bound one.
func TestSiblingTreeGateIsRedWhenAFamilyIsDropped(t *testing.T) {
	profile := siblingFixtureProfile(t)
	drops := []struct {
		name   string
		tree   string
		family string
		want   string
	}{
		{"the archive's rescue tarballs lose their decision", sessionlayout.SiblingTreeArchive, "archive-rescue-tarball", fixtureArchiveTarball},
		{"the archive's exports lose their decision", sessionlayout.SiblingTreeArchive, "archive-export", fixtureArchiveExport},
		{"the archive's unpacked rescues lose their decision", sessionlayout.SiblingTreeArchive, "archive-rescue-dir", fixtureArchiveRescueDir},
		{"the ledger snapshot loses its decision", sessionlayout.SiblingTreeBackups, "ledger-backup-snapshot", fixtureBackupSnapshot},
		{"the staging name loses its decision", sessionlayout.SiblingTreeBackups, "ledger-backup-staging", fixtureBackupStaging},
		{"the sidecar loses its decision", sessionlayout.SiblingTreeBackups, "ledger-backup-sidecar", fixtureBackupSidecar},
		{"the worktree residue loses its decision", sessionlayout.SiblingTreeBackups, "worktree-residue-archive", fixtureBackupWorktreeResidue},
		{"the notify feed loses its decision", sessionlayout.SiblingTreeNotify, "notify-event-stream", fixtureNotifyStream},
		{"the dump family loses its declaration", sessionlayout.SiblingTreeDumps, "error-request-dump", fixtureDumpCollisionName},
		{"the 1214 dump family loses its declaration", sessionlayout.SiblingTreeDumps1214, "error-request-dump", fixtureDumpName},
	}
	for _, tc := range drops {
		t.Run(tc.name, func(t *testing.T) {
			reduced, ok := reducedSiblingFamilies(tc.tree, tc.family)
			if !ok {
				t.Fatalf("family %q is not registered for tree %q — the red-proof cannot drop what does not exist", tc.family, tc.tree)
			}
			root := siblingFixtureRoot(t, profile, tc.tree)
			scan, err := sessionlayout.ScanSiblingTreeWith(tc.tree, root, reduced)
			if err != nil {
				t.Fatalf("scan %s: %v", root, err)
			}
			var hit bool
			for _, u := range scan.Unclassified {
				if u.Name == tc.want {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("dropping family %q reported %d unclassified entries, none of which was %q — the gate failed for the wrong reason",
					tc.family, len(scan.Unclassified), tc.want)
			}
		})
	}

	// The control: with the REAL registries the same fixture is GREEN. A "gate"
	// that refuses everything is not a gate either.
	for _, tree := range sessionlayout.SiblingTreeNames() {
		scan := scanSiblingFixture(t, tree, siblingFixtureRoot(t, profile, tree))
		if len(scan.Unclassified) != 0 {
			t.Fatalf("the real %s registry leaves %d entries unclassified in its own fixture: %+v",
				tree, len(scan.Unclassified), scan.Unclassified)
		}
	}
}

// TestSiblingTreeGateIsRedForANewFamily pins the REGRESSION the epic asks for: a
// writer that lands a family these registries do not know — a new directory
// shape and a new file shape — must make the gate red, so a sibling tree can
// never grow a family in silence.
func TestSiblingTreeGateIsRedForANewFamily(t *testing.T) {
	profile := siblingFixtureProfile(t)
	tree := sessionlayout.SiblingTreeDumps
	root := siblingFixtureRoot(t, profile, tree)

	before := scanSiblingFixture(t, tree, root)
	if len(before.Unclassified) != 0 {
		t.Fatalf("the seeded %s fixture is not clean: %+v", tree, before.Unclassified)
	}

	mkdirFixture(t, filepath.Join(root, fixtureSiblingUnknownDir))
	writeFixtureFile(t, filepath.Join(root, fixtureSiblingUnknownFile))
	writeFixtureFile(t, filepath.Join(root, fixtureSiblingForeignNote))

	after := scanSiblingFixture(t, tree, root)
	shapes := widenSiblingScanForGate(after).UnclassifiedShapes()
	for _, want := range []string{fixtureSiblingUnknownDir + "/", fixtureSiblingUnknownFile, fixtureSiblingForeignNote} {
		if shapes[want] == 0 {
			t.Fatalf("a new family (%q) landed in the %s tree and the gate stayed GREEN (unclassified shapes: %v) — silence is the defect this gate exists to refuse",
				want, tree, shapes)
		}
	}
}

// TestSiblingTreeGateOnTheLiveTrees corroborates the fixture against the LIVE
// sibling trees. It is REPORT-ONLY by construction: these trees are the
// operator's state, so their unclassified entries are logged and never turned
// into a failure. What IS asserted is that each walk ran and read something — a
// walk that read nothing must never be mistaken for a clean tree.
func TestSiblingTreeGateOnTheLiveTrees(t *testing.T) {
	profile, err := siblingProfileRootForGate()
	if err != nil {
		t.Skipf("the gopi profile root is not resolvable on this host: %v", err)
	}
	walked := 0
	for _, st := range sessionlayout.SiblingTrees() {
		root, rerr := st.Resolve()
		if rerr != nil {
			t.Logf("sibling tree %s has no root on this host: %v", st.Name, rerr)
			continue
		}
		fi, serr := os.Stat(root)
		if serr != nil || !fi.IsDir() {
			t.Logf("no %s tree at %s on this host (%v)", st.Name, root, serr)
			continue
		}
		if !strings.HasPrefix(root, profile) {
			t.Fatalf("sibling tree %s resolved to %s, which is not under the profile %s — the seam is wired to the wrong root", st.Name, root, profile)
		}
		scan, werr := sessionlayout.ScanSiblingTree(st)
		if werr != nil {
			t.Fatalf("scan live tree %s at %s: %v", st.Name, root, werr)
		}
		walked++
		t.Logf("live sibling tree %s at %s: %d shapes, %d files, %d dirs, %d bytes, %d entries unclassified",
			st.Name, root, len(scan.ByShape), scan.Files, scan.Dirs, scan.Bytes, len(scan.Unclassified))
		for shape, n := range scan.UnclassifiedShapes() {
			t.Logf("live %s UNCLASSIFIED shape %q — %d entries: the registry has no decision for it (Rule 15: reported, never ignored)",
				st.Name, shape, n)
		}
	}
	if walked == 0 {
		t.Skip("no sibling tree exists on this host — the live corroboration has no subject")
	}
}

// TestSiblingSiblingNamesMatchTheirWriters pins the SPELLINGS the sibling
// registry shares with the code that writes those trees, and the ones the two
// packages re-spell (internal/scratch cannot import internal/sessionlayout in
// production — the direction is the other way, through internal/tools — so a
// pair of literals with nothing joining them is exactly the drift Rules 13/14
// ban).
//
// TWO KINDS OF PIN, deliberately:
//
//   - a VALUE comparison, where both sides are Go values this test can see
//     (sessionlayout's tree names against the declared tree's own constants, and
//     the declared tree's name against a declared ROW's name) — the strongest
//     form, and the one that survives any rename on either side;
//   - a SOURCE read, where the far side is an unexported constant in a package
//     that imports this one (internal/tools' LedgerBackupDirName,
//     internal/deepseek's dumpFileSuffix) — the spelling is asserted against the
//     file that writes it, so a rename there fails HERE instead of leaving the
//     registry describing a tree nothing writes.
func TestSiblingSiblingNamesMatchTheirWriters(t *testing.T) {
	// The four tree names the registry carries are the four directories the
	// profile holds: the census resolves them by joining the registry's own name
	// onto the profile root, so the name IS the leaf.
	for _, name := range sessionlayout.SiblingTreeNames() {
		if name != filepath.Base(name) || name == "" {
			t.Errorf("sibling tree name %q is not a single path element — the census joins it onto the profile root", name)
		}
	}
	// The declared tree this package contributes and the registry's `bound:` row
	// must be the SAME value (the one cross-package name this epic adds).
	if SiblingDumpsTreeName != sessionlayout.SiblingDumpsTree {
		t.Errorf("declared tree name %q (internal/scratch) != the registry's bound tree %q (internal/sessionlayout) — the `bound:` row names a tree the sweep does not hold",
			SiblingDumpsTreeName, sessionlayout.SiblingDumpsTree)
	}
	if SiblingDumpsDirName != sessionlayout.SiblingTreeDumps {
		t.Errorf("the declared tree roots at %q while the registry censuses %q — the bound and the census would cover different directories",
			SiblingDumpsDirName, sessionlayout.SiblingTreeDumps)
	}
	// The SECOND dump knob's pair, the same two ways (measured on this host:
	// GOPI_1214_DUMP_DIR points the profile's `dumps-1214/`).
	if SiblingDumps1214TreeName != sessionlayout.SiblingDumps1214Tree {
		t.Errorf("declared tree name %q (internal/scratch) != the registry's bound tree %q (internal/sessionlayout)",
			SiblingDumps1214TreeName, sessionlayout.SiblingDumps1214Tree)
	}
	if SiblingDumps1214DirName != sessionlayout.SiblingTreeDumps1214 {
		t.Errorf("the declared tree roots at %q while the registry censuses %q",
			SiblingDumps1214DirName, sessionlayout.SiblingTreeDumps1214)
	}
	// Every declared tree a family is BOUND to must really be a row of the set
	// the sweep walks: a constant with no row is a name nothing resolves, and a
	// `bound:` decision naming it bounds nothing. Driven off the REGISTRY, so a
	// third bound family is checked the day it lands rather than when somebody
	// remembers this test exists.
	for _, name := range sessionlayout.SiblingBoundTreeNames() {
		var found bool
		for _, tree := range DeclaredTrees() {
			if tree.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("declared tree %q is not in DeclaredTrees() — the registry binds a family to a tree nothing sweeps", name)
		}
	}
	// The two dump rows must be DISTINCT trees with DISJOINT roots: they share
	// one implementation, so a copy-paste slip would silently point both at the
	// same directory and the census would report one of the knobs twice.
	if SiblingDumpsTreeName == SiblingDumps1214TreeName || SiblingDumpsDirName == SiblingDumps1214DirName {
		t.Errorf("the two dump trees share a name (%q/%q) or a leaf (%q/%q) — they are two knobs' targets, not one",
			SiblingDumpsTreeName, SiblingDumps1214TreeName, SiblingDumpsDirName, SiblingDumps1214DirName)
	}

	// The SOURCE-read half: the spellings the far side owns.
	repo := repoRootForDeclaredTreeGate(t)
	sources := []struct {
		why  string
		rel  string
		want string
	}{
		{
			why: "the ledger backup WRITER's directory name",
			rel: filepath.Join("internal", "tools", "ledger_backup.go"),
			// The tree the registry calls `backups` is the one this constant
			// resolves; a rename here must fail this pin.
			want: `LedgerBackupDirName = "` + sessionlayout.SiblingTreeBackups + `"`,
		},
		{
			why:  "the error-dump WRITER's file suffix (the tree's namespace predicate)",
			rel:  filepath.Join("internal", "deepseek", "dump_file.go"),
			want: `dumpFileSuffix = "` + siblingDumpFileSuffix + `"`,
		},
		{
			why:  "the error-dump writer's FIRST-ranked source, which is what points this tree at the profile",
			rel:  filepath.Join("internal", "deepseek", "minimal_core.go"),
			want: `os.Getenv("GOPI_ERROR_DUMP_DIR")`,
		},
		{
			why:  "the error-dump writer's DOCUMENTED DEFAULT (the scratch dumps class tree)",
			rel:  filepath.Join("internal", "deepseek", "minimal_core.go"),
			want: `return scratch.DumpDir(minimalCoreDumpClass)`,
		},
		{
			why:  "the 1214 dump writer's FIRST-ranked source, the second knob that can point a tree at the profile",
			rel:  filepath.Join("internal", "deepseek", "error_1214.go"),
			want: `os.Getenv("GOPI_1214_DUMP_DIR")`,
		},
		{
			why:  "the 1214 dump writer's DOCUMENTED DEFAULT (its own scratch dumps class tree)",
			rel:  filepath.Join("internal", "deepseek", "error_1214.go"),
			want: `return scratch.DumpDir(guardDumpClass)`,
		},
		{
			why:  "the profile root the sibling trees hang off — the ONE resolution, in the package that owns it",
			rel:  filepath.Join("internal", "config", "scratch_trees.go"),
			want: "scratch.RegisterSiblingDataRootResolver(GopiDir)",
		},
		{
			why:  "the family census's own resolver seam",
			rel:  filepath.Join("internal", "config", "scratch_trees.go"),
			want: "sessionlayout.RegisterSiblingDataRootResolver(GopiDir)",
		},
	}
	for _, tc := range sources {
		t.Run(tc.want, func(t *testing.T) {
			body := readRepoFile(t, repo, tc.rel)
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s no longer carries %q (%s) — the census/writer pair has drifted (Rules 13/14)", tc.rel, tc.want, tc.why)
			}
		})
	}
}

// siblingProfileRootForGate resolves the gopi profile root the live sibling
// trees hang under, from a declared tree's own root: the sessions tree lives at
// `<profile>/sessions`, so the profile is that root's parent. It is derived, not
// re-resolved — the point of the gate is to check the tree THAT resolver answers,
// not a second resolution of the same thing.
func siblingProfileRootForGate() (string, error) {
	root, err := SessionRawLogRoot()
	if err != nil {
		return "", err
	}
	return filepath.Dir(root), nil
}

// assertSiblingBoundFamiliesNameDeclaredTrees is the cross-check the `bound:`
// decision needs: a sibling family that says `bound:<tree>` must name a tree the
// declared set REALLY holds.
func assertSiblingBoundFamiliesNameDeclaredTrees(t *testing.T, bound, declared []string) {
	t.Helper()
	if len(bound) == 0 {
		t.Fatalf("no sibling family is `bound:` — the ONE bound sibling tree (the profile's dumps) has lost its declaration")
	}
	live := make(map[string]bool, len(declared))
	for _, name := range declared {
		live[name] = true
	}
	for _, name := range bound {
		if !live[name] {
			t.Errorf("sibling decision broken: the registry binds a family to declared tree %q, which DeclaredTrees() does not hold (it holds %v) — a bound naming a tree that does not exist bounds nothing",
				name, declared)
		}
	}
}

// reducedSiblingFamilies returns one tree's registry with one family removed. ok
// is false when that family is not registered — the red-proof must not be able
// to "drop" a family that does not exist and call the resulting green a proof.
func reducedSiblingFamilies(tree, drop string) ([]sessionlayout.SiblingFamily, bool) {
	st, ok := sessionlayout.SiblingTreeByName(tree)
	if !ok {
		return nil, false
	}
	out := make([]sessionlayout.SiblingFamily, 0, len(st.Families))
	for _, f := range st.Families {
		if f.Name == drop {
			continue
		}
		out = append(out, f)
	}
	return out, len(out) != len(st.Families)
}

// widenSiblingScanForGate adapts a sibling census to the sessions census's type,
// so this gate reuses the EXISTING reportUnclassified helper rather than growing
// a second renderer of the same failure.
func widenSiblingScanForGate(ss sessionlayout.SiblingScan) sessionlayout.FamilyScan {
	return sessionlayout.FamilyScan{
		Root:         ss.Root,
		ByShape:      ss.ByShape,
		Unclassified: ss.Unclassified,
		Dirs:         ss.Dirs,
		Files:        ss.Files,
		Bytes:        ss.Bytes,
	}
}
