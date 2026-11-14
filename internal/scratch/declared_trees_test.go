package scratch

// declared_trees_test.go — the declared-tree registry's contract, and THE GATE
// the epic asks for.
//
// FOUR KINDS OF ASSERTION, deliberately separated:
//
//  1. THE GATE (TestDeclaredTreesCoverRepoWriters): the declared set must cover
//     every tree this repository's own commands create and never clean. It is
//     the check that would have caught the defect (a GC covering one tree of
//     four) and it is RED-PROVABLE — TestGateCatchesUndeclaredTree drives the
//     same decider over synthetic repository states and asserts they are
//     REFUSED, including the exact historical shape (session raw log missing
//     from the set).
//  2. THE INVARIANT (TestDeclaredTreeRootsAreNeverCandidates): the root of a
//     declared tree is never a candidate for removal, at any age — the incident
//     where a hand sweep deleted GOTMPDIR and broke the next build.
//  3. THE HARD RULE on a real tree (TestDeclaredTreeSweepsOnlyAgedUnheldEntries):
//     age + live holder, on a fixture tree, with the root surviving.
//  4. THE SHAPE (TestDeclaredTreesAreWiredAndNamed): every row's fields are
//     read (a declared field nothing reads is a lie) and every resolver is
//     wired (an unwired resolver would make the sweep walk "").

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// repoRootForDeclaredTreeGate locates the repository root from the test's own
// working directory (the module root holds go.mod). It is the same anchor the
// standards gates use: the test runs with the package's directory as its cwd.
func repoRootForDeclaredTreeGate(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// readRepoFile reads one repository file for the gate, failing the test when it
// is missing — a missing writer file means the gate's own anchors rotted, which
// must never read as "the tree is covered".
func readRepoFile(t *testing.T, root string, rel ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root}, rel...)...)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("gate anchor %s is unreadable: %v", path, err)
	}
	return string(b)
}

// declaredTreeWriter is ONE writer of a tree this repository creates: the file
// that resolves or spells the tree's path, and the evidence string that proves
// the file still holds that tree. The gate reads the file and requires the
// evidence; the declaration that covers it is named back to the reader.
type declaredTreeWriter struct {
	file     string // repo-relative path of a production writer
	evidence string // a string the writer must carry (its tree's path/config spelling)
	covers   string // the declared tree name that must cover it
}

// repoDeclaredTreeWriters is the GATE'S ANCHOR LIST: one row per tree this
// repository's own commands create and never clean.
//
// WHY AN ANCHOR LIST AND NOT A SCAN OF EVERYTHING: a mechanical "find all paths"
// is not decidable from source, and a whole-repo grep for path-shaped strings
// would drown in false positives (that is why the deep check is a review
// obligation, stated in devdoc/scratch-gc-declared-trees.md). The anchor list is
// the ENUMERATION half: a machine-parseable list of the writers, each row
// carrying the evidence string that pins it, so ADDING A WRITER forces an
// explicit decision here — the gate fails when a row's declared tree is not in
// the set, and a new writer that nobody added a row for is exactly what the
// review obligation covers.
func repoDeclaredTreeWriters() []declaredTreeWriter {
	return []declaredTreeWriter{
		{
			// The Go toolchain's work-directory root: the tree `go` fills and
			// abandons. Its DECLARATION on this host is the go env file
			// (GOTMPDIR=…), which is what the resolver reads.
			file:     filepath.Join("internal", "scratch", "declared_trees.go"),
			evidence: GoTmpDirEnvName,
			covers:   "go-tmp",
		},
		{
			// The go-tmp row's THIRD RANK — the fallback the toolchain itself
			// takes when GOTMPDIR is set nowhere. It is a writer anchor because
			// its absence was a defect (measured 2026-10-15: the row reported
			// "not configured on this host" while `go` filled the tree under
			// $TMPDIR), and because the fallback's VALUE is read from the one
			// shared probe rather than spelled here — a re-derivation that
			// drifted would silently point the sweep at a tree `go` never uses.
			file:     filepath.Join("internal", "scratch", "declared_trees.go"),
			evidence: "EffectiveTempRoot()",
			covers:   "go-tmp",
		},
		{
			// The legacy flat raw-log directory: written by
			// internal/agent.NewRawLogger through config's raw-log resolver.
			file:     filepath.Join("internal", "agent", "rawlog.go"),
			evidence: "GOPI_RAW_LOG_DIR",
			covers:   "raw",
		},
		{
			// The per-run raw log co-located with a session: the writer that
			// creates `<run-uuid>.raw.log` inside the sessions tree.
			file:     filepath.Join("internal", "agent", "rawlog.go"),
			evidence: "scratch.SessionRawLogFileSuffix",
			covers:   "session-raw",
		},
		{
			// The raw-log directory's ONE spelling in the gopi-data-root
			// layer (the value is this package's, referenced not re-spelled).
			file:     filepath.Join("internal", "config", "scratch_trees.go"),
			evidence: "scratch.RawLogDirName()",
			covers:   "raw",
		},
		{
			// The sessions tree's spelling — the container the per-run raw
			// logs live in.
			file:     filepath.Join("internal", "config", "scratch_trees.go"),
			evidence: "SessionsDirName",
			covers:   "session-raw",
		},
		{
			// The shared Go build cache: the merge-time BUILD gate resolves the
			// per-checkout key directory here. It is the tree that reached
			// 16.6 GB while the gate's OWN bound stayed green, because that
			// bound is per key and this tree grows by ACCUMULATION of keys.
			file:     filepath.Join("internal", "tools", "merge_build_gate_gocache.go"),
			evidence: "mergeBuildGateGoCacheRepoKey(repo)",
			covers:   BuildCacheTreeName,
		},
		{
			// The cache LEAF join — the root of the tree the sweep resolves.
			file:     filepath.Join("internal", "tools", "merge_build_gate_gocache.go"),
			evidence: "worktreeroots.SharedBuildCacheDirName",
			covers:   BuildCacheTreeName,
		},
		{
			// The key LENGTH: the writer must REFERENCE the leaf constant the
			// sweep's namespace predicate is built from (scratch.BuildCacheKeyHexLen),
			// never re-spell 16 — a key length that drifted would leave the
			// sweep reporting the tree as visited while it matched nothing.
			file:     filepath.Join("internal", "tools", "merge_build_gate_gocache.go"),
			evidence: "scratch.BuildCacheKeyHexLen",
			covers:   BuildCacheTreeName,
		},
		{
			// The sweep side's own root join: <worktree root>/<cache leaf>,
			// through the SAME leaf constant the writer joins.
			file:     filepath.Join("internal", "scratch", "declared_build_cache.go"),
			evidence: "worktreeroots.SharedBuildCacheDirName",
			covers:   BuildCacheTreeName,
		},
		{
			// The declared-root ENUMERATION: a host that declares a worktree
			// root per repo has a cache tree per repo, and the sweep only sees
			// them because this seam hands the enumeration over.
			file:     filepath.Join("internal", "config", "scratch_trees.go"),
			evidence: "RegisterBuildCacheWorktreeRoots",
			covers:   BuildCacheTreeName,
		},
		{
			// The per-task attachments ARCHIVE: the copy area the submit face
			// fills. It is the tree the family registry kept as `keep:` because
			// it can hold the only surviving copy of evidence — and it is now
			// bounded by the EVIDENCE predicate instead
			// (internal/scratch/attachments_archive.go), so the writer's own
			// name must still resolve to that tree.
			file:     filepath.Join("internal", "tools", "task_session.go"),
			evidence: "taskAttachmentsDirName",
			covers:   AttachmentsTreeName,
		},
		{
			// The archive tree's ROOT: the sessions tree, through this
			// package's own resolver — the same root the `session-raw` row
			// resolves, since a task's archive lives inside its session
			// container (session-dir-unify S1).
			file:     filepath.Join("internal", "scratch", "attachments_archive.go"),
			evidence: "SessionRawLogRoot()",
			covers:   AttachmentsTreeName,
		},
		{
			// The archive's NAMESPACE PREDICATE: the one leaf name the tree may
			// take, referenced by the predicate and by the probe — a second
			// spelling of "attachments" anywhere would let the writer and the
			// sweeper disagree about which directory is the archive.
			file:     filepath.Join("internal", "scratch", "attachments_archive.go"),
			evidence: "AttachmentsArchiveDirName",
			covers:   AttachmentsTreeName,
		},
		{
			// The CAPTURE class tree's WRITER: a capture taken with no `--out`
			// lands there (measured 2026-11-07, task 01a1b3b1 — before this row
			// the default was a bare relative name, i.e. the caller's cwd, and a
			// task run's cwd is its clone ground). The anchor is the resolver
			// CALL, so a face that grew its own destination spelling fails here
			// instead of leaving the file outside every sweep.
			file:     filepath.Join("internal", "tools", "screencapture.go"),
			evidence: "scratch.CaptureDumpFile",
			covers:   CaptureTreeName,
		},
		{
			// The same writer anchor for the `gopi tool shot` face: its PNG is a
			// DIFFERENT family (`shot-`) but the same tree, so one row per face
			// keeps "which face went its own way" answerable from this list.
			file:     filepath.Join("cmd", "agent", "cli_shot.go"),
			evidence: "scratch.CaptureDumpFile",
			covers:   CaptureTreeName,
		},
		{
			// The tree's OWN declaration and predicate, in the file that carries
			// both — a row whose namespace predicate moved would leave the sweep
			// matching nothing while reporting the tree as visited.
			file:     filepath.Join("internal", "scratch", "scratch_captures.go"),
			evidence: "CaptureDumpFile",
			covers:   CaptureTreeName,
		},
		{
			// The TEXT2IMG class tree's WRITER anchor: the renderer's `out == ""`
			// branch (measured 2026-11-07, task 01a1b3cf — before this row the
			// default was `<src 去扩展名>.png`, i.e. BESIDE THE SOURCE, which is
			// inside the run's own git ground when src lives there: Rule 30's
			// completed-unclean). The anchor is the resolver CALL, so a face that
			// grows its own destination spelling fails HERE instead of leaving the
			// PNG outside every sweep.
			file:     filepath.Join("internal", "text2img", "render.go"),
			evidence: "scratch.Text2ImgDumpFile",
			covers:   Text2ImgTreeName,
		},
		{
			// The same tree's declaration and namespace predicate, in the file
			// that carries both (Rules 13/14: writer and collector name ONE
			// definition, so the renderer cannot produce a name its sweep refuses).
			file:     filepath.Join("internal", "scratch", "scratch_text2img.go"),
			evidence: "Text2ImgDumpName",
			covers:   Text2ImgTreeName,
		},
		{
			// The TEST-BUDGET entry point's own artifact tree: the per-run
			// built-binary directory is created by `scripts/test-budget.sh`'s own
			// `mkdir -p "$tested_bin_dir"` + `go build -o …`, and nothing ever
			// removes it. The anchor is the SUFFIX the script joins the run tag
			// with, so a rename of the writer's name shape fails THIS gate
			// instead of leaving the sweep matching a name no writer produces.
			file:     filepath.Join("scripts", budgetScriptRelPath),
			evidence: BudgetRunTestedBinSuffix,
			covers:   BudgetRunsTreeName,
		},
		{
			// The same tree's ROOT: the queue child the script joins its artifact
			// directory onto (`budget_dir="$(gopi_scratch_queue)/$budget_subdir"`).
			// The anchor is the script's OWN variable name, so the leaf this
			// package declares (BudgetDirName) and the name the writer uses stay
			// held against each other by a machine rather than by memory.
			file:     filepath.Join("scripts", budgetScriptRelPath),
			evidence: budgetSubdirVarName,
			covers:   BudgetRunsTreeName,
		},
		{
			// The DECLARED TEMP ROOT's own residue family. Its writer is the
			// residue NAMESPACE itself: this row takes exactly the names the base
			// sweep already trusts (`gopi-<token>` / `gocache-<token>` /
			// `probe-<token>`), so the anchor is the predicate function — a row
			// whose predicate drifted from that one definition would collect a
			// family nobody declared.
			file:     filepath.Join("internal", "scratch", "declared_tmpdir_residue.go"),
			evidence: "IsBaseResidueName",
			covers:   TmpDirResidueTreeName,
		},
		{
			// The PUBLICATION that puts the residue there: gopi writes its
			// resolved tmpdir into TMPDIR for every child, so `os.MkdirTemp("")`
			// in any child mints its directory inside this tree. The anchor is the
			// publisher's own function — the tree this row roots at is exactly the
			// value that line installs.
			file:     filepath.Join("cmd", "agent", "tmpdir_inject.go"),
			evidence: "publishTmpDir",
			covers:   TmpDirResidueTreeName,
		},
	}
}

// budgetScriptRelPath / budgetSubdirVarName are the writer's own spellings, read
// out of the entry point's source by the gate above (it reads the FILE, not this
// constant). They are named here because a bare literal in an anchor row is the
// drift Rule 1b bans.
const (
	budgetScriptRelPath = "test-budget.sh"
	budgetSubdirVarName = "budget_subdir"
)

// gateFinding is one refusal the gate produced. It exists so the RED-PROOF can
// drive the SAME decider the real gate uses (TestGateCatchesUndeclaredTree)
// instead of a copy of its logic.
type gateFinding struct {
	writer string
	covers string
	reason string
}

// declaredTreeGateDecider is the gate's whole predicate: given the writers and
// the declared tree names, return one finding per writer whose tree is NOT
// covered. It is pure (no filesystem, no environment) so the red-proof can feed
// it synthetic states — including states the live repository cannot produce.
func declaredTreeGateDecider(writers []declaredTreeWriter, declared []string) []gateFinding {
	covered := make(map[string]bool, len(declared))
	for _, name := range declared {
		covered[name] = true
	}
	var findings []gateFinding
	for _, w := range writers {
		if !covered[w.covers] {
			findings = append(findings, gateFinding{
				writer: w.file,
				covers: w.covers,
				reason: "the repository's own commands create and never clean a tree (" + w.file + ") that the GC's declared set does not cover",
			})
		}
	}
	return findings
}

// declaredTreeNames is the set under test, as names.
func declaredTreeNames() []string {
	var names []string
	for _, tree := range DeclaredTrees() {
		names = append(names, tree.Name)
	}
	return names
}

// TestDeclaredTreesCoverRepoWriters is THE GATE: every writer of a tree this
// repository creates must be covered by the declared set, and the writer's own
// file must still carry the evidence the row claims.
//
// Two ways to fail, both deliberate:
//
//	(a) the decider refuses (a tree lost its declaration) — the RED half,
//	    proved by TestGateCatchesUndeclaredTree;
//	(b) an evidence string is gone from its file (the writer moved or was
//	    rewritten) — the anchor rotted, and a gate whose anchors rot silently
//	    reads green forever.
func TestDeclaredTreesCoverRepoWriters(t *testing.T) {
	root := repoRootForDeclaredTreeGate(t)
	writers := repoDeclaredTreeWriters()

	for _, f := range declaredTreeGateDecider(writers, declaredTreeNames()) {
		t.Errorf("UNDECLARED TREE: %s — %s. Declare it in internal/scratch.DeclaredTrees (or remove the writer).",
			f.writer, f.reason)
	}

	for _, w := range writers {
		body := readRepoFile(t, root, w.file)
		if !strings.Contains(body, w.evidence) {
			t.Errorf("gate anchor rotted: %s no longer carries %q (its tree is %q) — re-point this row at the writer",
				w.file, w.evidence, w.covers)
		}
	}
}

// TestGateCatchesUndeclaredTree is the RED-PROOF of the gate above, as a test
// that runs with the suite: it drives the SAME decider the gate uses over
// repository states that must be refused. The historical state is included
// verbatim — the session-tree raw log had no declaration at all, which is why
// 51.9 GB of it accumulated unmeasured.
func TestGateCatchesUndeclaredTree(t *testing.T) {
	all := declaredTreeNames()
	if len(all) == 0 {
		t.Fatal("declared set is empty — the gate would refuse everything; the red-proof cannot be vacuous")
	}

	cases := []struct {
		name    string
		drop    string
		wantHit string
	}{
		{"the GOTMPDIR tree loses its declaration", TreeNameGoTmp, TreeNameGoTmp},
		{"the flat raw-log tree loses its declaration", TreeNameRaw, TreeNameRaw},
		{"the session raw-log tree loses its declaration", TreeNameSessionRaw, TreeNameSessionRaw},
		{"the shared build-cache tree loses its declaration", BuildCacheTreeName, BuildCacheTreeName},
		{"the test-budget artifact tree loses its declaration", BudgetRunsTreeName, BudgetRunsTreeName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reduced []string
			for _, n := range all {
				if n != tc.drop {
					reduced = append(reduced, n)
				}
			}
			findings := declaredTreeGateDecider(repoDeclaredTreeWriters(), reduced)
			if len(findings) == 0 {
				t.Fatalf("gate passed with %q removed from the declared set — it is not RED-provable", tc.drop)
			}
			var hit bool
			for _, f := range findings {
				if f.covers == tc.wantHit {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("gate reported %+v, none of which names the dropped tree %q", findings, tc.wantHit)
			}
		})
	}

	// The control: with the real set, the gate is GREEN — a "gate" that refuses
	// everything is not a gate.
	if findings := declaredTreeGateDecider(repoDeclaredTreeWriters(), all); len(findings) != 0 {
		t.Fatalf("gate fires on the real declared set: %+v", findings)
	}

	// And the decider must be able to fail for a writer that does not exist in
	// the anchor list either: a synthetic writer proves the check is over the
	// INPUT, not over a hardcoded answer.
	synthetic := []declaredTreeWriter{{file: "internal/newpkg/newtree.go", evidence: "x", covers: "workspace-loose-files"}}
	if findings := declaredTreeGateDecider(synthetic, all); len(findings) != 1 {
		t.Fatalf("a writer for an undeclared tree must be refused exactly once, got %+v", findings)
	}
}

// TestDeclaredTreesAreWiredAndNamed pins the shape of every row: its fields are
// read (a declared field nobody reads is a lie), its resolver is wired (an
// unwired resolver would sweep ""), and its age is one of the named constants —
// so a bare literal age cannot creep in (Rules 1/13).
func TestDeclaredTreesAreWiredAndNamed(t *testing.T) {
	// A LIST, not a duration-keyed map: two named windows legitimately share a
	// value (GoTmpStaleAge == BuildCacheStaleAge == 24h, measured — see
	// declared_build_cache.go), so a map keyed by the duration could not even
	// hold both — it pins the value, not the name, and the name is what a
	// reader follows to the reason.
	named := []namedWindow{
		{"GoTmpStaleAge", GoTmpStaleAge},
		{"RawLogStaleAge", RawLogStaleAge},
		{"SessionRawLogStaleAge", SessionRawLogStaleAge},
		{"SessionRawLogDirStaleAge", SessionRawLogDirStaleAge},
		{"SessionAttachmentsStaleAge", SessionAttachmentsStaleAge},
		{"BuildCacheStaleAge", BuildCacheStaleAge},
		{"SessionLegacyDirStaleAge", SessionLegacyDirStaleAge},
		{"SessionLegacyContainerStaleAge", SessionLegacyContainerStaleAge},
		{"BudgetRunsStaleAge", BudgetRunsStaleAge},
		{"TmpDirResidueStaleAge", TmpDirResidueStaleAge},
		{"ScratchDumpsStaleAge", ScratchDumpsStaleAge},
		{"CaptureStaleAge", CaptureStaleAge},
	}
	seen := map[string]bool{}
	used := map[time.Duration]bool{}
	for _, tree := range DeclaredTrees() {
		if tree.Name == "" || tree.Class == "" || tree.Resolve == nil {
			t.Errorf("declared tree %+v is incomplete: Name/Class/Resolve are all read by the sweep and by the gate", tree)
		}
		if seen[tree.Name] {
			t.Errorf("declared tree name %q appears twice — the log line could not be attributed", tree.Name)
		}
		seen[tree.Name] = true
		if namedAge(named, tree.StaleAge) == "" {
			t.Errorf("declared tree %q carries an age (%v) that is not one of the named constants — Rule 1/13", tree.Name, tree.StaleAge)
		}
		used[tree.StaleAge] = true
		if tree.RootStaleAge != 0 {
			used[tree.RootStaleAge] = true
		}
		// A tree that names its own staleness or size rule must carry BOTH the
		// rule and the namespace predicate: a custom rule on an unconstrained
		// tree would be a blunt "delete everything older than N here".
		if tree.EntryStale != nil || tree.EntrySize != nil {
			if tree.Kind != EntryKindDir || tree.EntryMatch == nil {
				t.Errorf("declared tree %q overrides how an entry ages or measures without a namespace predicate — every aged child would be collectable", tree.Name)
			}
		}
		if tree.Kind == EntryKindDir && tree.EntryMatch == nil {
			t.Errorf("declared tree %q takes EVERY directory (no namespace predicate) — an aged operator directory in that tree would be collectable", tree.Name)
		}
		// A row whose candidates are found ONE LEVEL DOWN carries its namespace in
		// a predicate rather than in a suffix (the container arms): a suffix
		// would be a second, weaker spelling of the same rule.
		suffixless := tree.Kind == EntryKindFileMatch || tree.Kind == EntryKindTreeFileSuffix
		if tree.Kind != EntryKindDir && !suffixless && tree.Suffix == "" {
			t.Errorf("declared tree %q has kind %v but no suffix: no entry could ever match it", tree.Name, tree.Kind)
		}
		// A FileMatch row has no suffix to carry its namespace, so the WHOLE
		// rule is its predicate: without one it would match every file in the
		// tree (the blunt "everything older than N here" this package exists to
		// prevent) — so the field is required, not merely read.
		if tree.Kind == EntryKindFileMatch && tree.EntryMatch == nil {
			t.Errorf("declared tree %q is a file-match row with no namespace predicate — every aged file in the tree would be collectable", tree.Name)
		}
		if _, err := tree.Resolve(); err != nil && !strings.Contains(err.Error(), ErrDeclaredTreeUnconfigured.Error()) {
			t.Errorf("declared tree %q resolver is not wired: %v (an unwired resolver would sweep the wrong path)", tree.Name, err)
		}
	}
	if len(seen) != len(DeclaredTrees()) {
		t.Errorf("declared tree names are not unique: %v", seen)
	}
	// Every named window must be REFERENCED by a tree: a constant no tree
	// carries is a bound nobody applies (Rules 1/13).
	for _, w := range named {
		if !used[w.d] {
			t.Errorf("%s (%v) is named but no declared tree carries it — an unreferenced bound", w.name, w.d)
		}
	}
}

// namedWindow is one named age constant plus its identifier, so a failure can
// name the constant a tree should have referenced.
type namedWindow struct {
	name string
	d    time.Duration
}

// namedAge returns the identifier of the named window equal to d, or "".
func namedAge(named []namedWindow, d time.Duration) string {
	for _, w := range named {
		if w.d == d {
			return w.name
		}
	}
	return ""
}

// TestDeclaredTreeRootsAreNeverCandidates is the INVARIANT, tested against every
// declared tree: the root is refused by the collectability predicate at ANY age,
// and by the last-moment re-check that guards the delete.
func TestDeclaredTreeRootsAreNeverCandidates(t *testing.T) {
	home := t.TempDir()
	goTmp := filepath.Join(home, "workspace", "gopi-go-tmp")
	t.Setenv("GOPI_HOME", home)
	t.Setenv(GoTmpDirEnvName, goTmp)
	if err := os.MkdirAll(goTmp, DirPerm); err != nil {
		t.Fatal(err)
	}
	// The root itself is backdated far past every window: age alone must never
	// make a declared root a candidate.
	ancient := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(goTmp, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	if IsDeclaredTreeRoot(goTmp) != true {
		t.Fatalf("%s must be recognised as a declared tree root (a resolved-path comparison)", goTmp)
	}
	tree, ok := treeByName("go-tmp")
	if !ok {
		t.Fatal("the go-tmp tree must be declared")
	}
	if declaredEntryCollectable(tree, goTmp, time.Now(), time.Hour, heldTreeScan{}) {
		t.Fatalf("the ROOT of declared tree %s is a candidate — this is the incident that deleted GOTMPDIR", tree.Name)
	}
	if err := removeCollectableOfTree(tree, goTmp); err == nil {
		t.Fatalf("removeCollectableOfTree removed the ROOT of %s — the last-moment check must refuse it", tree.Name)
	}
	if _, err := os.Stat(goTmp); err != nil {
		t.Fatalf("the declared root %s did not survive: %v", goTmp, err)
	}
}

// treeByName finds one declared tree by name.
func treeByName(name string) (DeclaredTree, bool) {
	for _, tree := range DeclaredTrees() {
		if tree.Name == name {
			return tree, true
		}
	}
	return DeclaredTree{}, false
}

// TestDeclaredTreeSweepsOnlyAgedUnheldEntries is the hard rule on a fixture tree:
// an aged unheld entry goes, a fresh one and a live-held one stay, the root
// survives, and the reclaimed BYTES come back measured.
func TestDeclaredTreeSweepsOnlyAgedUnheldEntries(t *testing.T) {
	home := t.TempDir()
	goTmp := filepath.Join(home, "workspace", "gopi-go-tmp")
	t.Setenv("GOPI_HOME", home)
	t.Setenv(GoTmpDirEnvName, goTmp)
	if err := os.MkdirAll(goTmp, DirPerm); err != nil {
		t.Fatal(err)
	}
	tree, ok := treeByName("go-tmp")
	if !ok {
		t.Fatal("the go-tmp tree must be declared")
	}

	// The entries are aged past the TREE'S OWN window (GoTmpStaleAge), not past a
	// window this test supplies: the sweep takes no window from a caller any more,
	// so a fixture built around a shorter override would be asserting about a
	// judgement that cannot be made (task 01a17b8b-581a).
	agedAge := GoTmpStaleAge + time.Hour
	aged := seedGoBuildDir(t, goTmp, "go-build-aged", agedAge, 4096)
	fresh := seedGoBuildDir(t, goTmp, "go-build-fresh", 0, 4096)
	held := seedGoBuildDir(t, goTmp, "go-build-held", agedAge, 4096)
	RegisterHolder(held)
	t.Cleanup(func() { _ = os.Remove(filepath.Join(held, HolderFileName)) }) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertion it supports already ran

	removed, bytes, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed=%d, want 1 (only the aged unheld entry) — the age and holder rules must hold", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytes=%d, want the measured size of the reclaimed entry (the epic requires bytes on the log line)", bytes)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("aged unheld entry %s survived (err=%v)", aged, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("FRESH entry %s was collected — the age rule must protect it: %v", fresh, err)
	}
	if _, err := os.Stat(held); err != nil {
		t.Errorf("HELD entry %s was collected — the hard rule is violated: %v", held, err)
	}
	if _, err := os.Stat(goTmp); err != nil {
		t.Errorf("the declared ROOT %s did not survive: %v", goTmp, err)
	}
}

// TestGoTmpResolverFallsBackToTheProcessTempRoot is the REGRESSION GUARD for the
// defect measured 2026-10-15 on this host: GOTMPDIR set NOWHERE (this host's go
// env file carries only GOFLAGS and GOPROXY), so `go` created its `go-build*`
// work directories under the standard temp root — and the declared row reported
// `not configured on this host — nothing to sweep` while the tree it describes
// was being filled (three abandoned `go-build*` directories under
// /home/user/workspace/_tmp, ~30 MB each, all from killed builds).
//
// THE FALLBACK IS A RANK, NOT A CONVENIENCE: `go`'s own ladder is
// GOTMPDIR($env) > GOTMPDIR(go env file) > the standard temp root, so a GC that
// stops at rank 2 bounds a tree the toolchain never writes to while the real one
// grows unbounded. The case below drives exactly that state through the REAL
// resolver and the REAL sweep, and asserts the ROOT survives.
func TestGoTmpResolverFallsBackToTheProcessTempRoot(t *testing.T) {
	home := t.TempDir()
	tmp := filepath.Join(home, "workspace", "tmp-root")
	if err := os.MkdirAll(tmp, DirPerm); err != nil {
		t.Fatal(err)
	}
	t.Setenv(WorkspaceHomeEnvName, home)

	// Rank 3 in force: GOTMPDIR in neither the environment nor the go env file.
	// The go env file EXISTS and is readable — it simply does not carry the key,
	// which is this host's measured state, and is what makes the case about the
	// FALLBACK rather than about a missing file.
	t.Setenv(GoTmpDirEnvName, "")
	if err := os.MkdirAll(filepath.Join(home, ".config", "go"), DirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "go", "env"),
		[]byte("GOFLAGS=-mod=mod\nGOPROXY=direct\n"), FilePerm); err != nil {
		t.Fatal(err)
	}
	// The process temp root is SPIED, not assumed: whatever os.TempDir() reports
	// is what `go` would use, so the fixture points TMPDIR at its own tree and
	// the assertion is about the RANK, never about this host's /tmp.
	t.Setenv(TmpDirEnvName, tmp)

	got, err := ResolveGoTmpDir()
	if err != nil {
		t.Fatalf("ResolveGoTmpDir returned %v — the fallback rank is missing, so the go-tmp row would report 'not configured' while `go` fills the tree", err)
	}
	if want := CanonicalPath(tmp); got != want {
		t.Fatalf("ResolveGoTmpDir = %q, want the process temp root %q", got, want)
	}

	// And the tree is then SWEEPABLE through it: the whole point of resolving
	// rank 3 is that the abandoned children there are finally reachable. AGED is
	// past the ROW's own window (GoTmpStaleAge) — the sweep takes no caller's
	// window any more (task 01a17b8b-581a), so a 2h entry under a 24h row is a
	// keep by construction.
	aged := seedGoBuildDir(t, tmp, "go-build-aged", GoTmpStaleAge+time.Hour, 4096)
	fresh := seedGoBuildDir(t, tmp, "go-build-fresh", 0, 4096)
	tree, ok := treeByName(TreeNameGoTmp)
	if !ok {
		t.Fatal("the go-tmp tree must be declared")
	}
	removed, _, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed=%d, want 1 (only the aged entry)", removed)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("aged go-build entry %s survived the sweep (err=%v)", aged, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("FRESH entry %s was collected — the age rule must protect it: %v", fresh, err)
	}
	// THE INVARIANT THE INCIDENT IS ABOUT: the root is the temp root `go`
	// itself writes into, so a sweep that took it would break the next build.
	if _, err := os.Stat(tmp); err != nil {
		t.Errorf("the resolved go-tmp ROOT %s did not survive the sweep: %v", tmp, err)
	}
}

// seedGoBuildDir creates one go-build*-shaped child of a fixture tree with a
// known payload (so the reclaimed-bytes assertion is exact) and backdates it.
func seedGoBuildDir(t *testing.T, root, name string, age time.Duration, payload int) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), make([]byte, payload), FilePerm); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, "payload.bin"), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestDeclaredTreeWalksPastTheScratchScanLimit is the REGRESSION GUARD for the
// defect this arm shipped first and the real tree caught (2026-09-20): the walk
// reused ScanLimit (10,000) — the bound for ONE DIRECTORY of the shared scratch
// home — as the bound for a whole declared TREE, so the sessions tree (measured:
// 74,994 top-level entries on this host) refused to be walked at all:
//
//	scratch-gc role=cli-trees … tree=session-raw bytes=0
//	declared tree session-raw has more than 10000 entries — stopped early
//
// The whole 40 GB tree stayed unbounded while the log line said the tree was
// visited. The guard plants a candidate DEEPER than ScanLimit and asserts the
// walk still reaches it, so re-tightening the bound to the scratch limit fails
// here instead of silently skipping a real tree in production.
func TestDeclaredTreeWalksPastTheScratchScanLimit(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, ".gopi", "sessions")
	t.Setenv("GOPI_HOME", home)
	if err := os.MkdirAll(sessions, DirPerm); err != nil {
		t.Fatal(err)
	}
	tree, ok := treeByName("session-raw")
	if !ok {
		t.Fatal("the session-raw tree must be declared")
	}
	if TreeEntryScanLimit <= ScanLimit {
		t.Fatalf("TreeEntryScanLimit (%d) must exceed the per-directory ScanLimit (%d): a declared tree is a different shape than one scratch directory",
			TreeEntryScanLimit, ScanLimit)
	}

	// ScanLimit filler containers, then the candidate — so the candidate sits
	// PAST the scratch bound and only the tree bound reaches it.
	filler := ScanLimit + 10
	for i := 0; i < filler; i++ {
		if err := os.Mkdir(filepath.Join(sessions, "filler-"+strconv.Itoa(i)), DirPerm); err != nil {
			t.Fatal(err)
		}
	}
	deep := filepath.Join(sessions, "deep-run")
	if err := os.Mkdir(deep, DirPerm); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(deep, "run-uuid"+SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("x\n"), FilePerm); err != nil {
		t.Fatal(err)
	}
	backdate := time.Now().Add(-(SessionRawLogStaleAge + time.Hour))
	if err := os.Chtimes(raw, backdate, backdate); err != nil {
		t.Fatal(err)
	}

	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan refused a tree of %d entries: %v — the tree bound must be the tree's own", filler, err)
	}
	found := false
	for _, e := range plan {
		if e.Path == raw {
			found = true
		}
	}
	if !found {
		t.Fatalf("the walk did not reach the candidate placed deeper than ScanLimit (%d entries) — a real tree would stay unbounded", filler)
	}
}

// TestHeldSetIndexesTreeAncestors pins the index that replaced the per-candidate
// holder comparison: it must answer for a directory a live process holds, for an
// ancestor of one, and must not answer for a sibling.
func TestHeldSetIndexesTreeAncestors(t *testing.T) {
	root := "/tree/root"
	held := heldSet(HolderScan{Paths: []string{"/tree/root/run-a/sub", "/elsewhere"}}, root)
	for _, want := range []string{"/tree/root/run-a/sub", "/tree/root/run-a", root} {
		if !heldIn(want, held) {
			t.Errorf("%s must read as held (a live process is inside the tree below it)", want)
		}
	}
	for _, want := range []string{"/tree/root/run-b", "/tree/other", "/elsewhere"} {
		if heldIn(want, held) {
			t.Errorf("%s must NOT read as held — the index may only cover the tree", want)
		}
	}
}
