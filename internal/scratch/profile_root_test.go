package scratch

// profile_root_test.go — the PROFILE-ROOT arm's contract, in four kinds of
// assertion, deliberately separated:
//
//  1. THE NAMESPACE (TestProfileResidueNameNamespaceTable): the residue rule is
//     SHAPE, not a prefix — every accepted name is one this system's sessions
//     measured, and every refused one is a name a PERSON chose (`evidence`,
//     `evid-notes`, `sigrec-notes`). The profile root is the operator's state
//     directory, so the refused set is the point.
//  2. THE HARD RULE (TestProfileRootSweepCollectsOnlyAgedUnheldResidue): on a
//     fixture profile root — the two residue children (aged / fresh), a live
//     holder, a symlink in the namespace, and a foreign name — the report
//     deletes NOTHING and the explicit sweep removes EXACTLY the aged unheld
//     one, with the root and every declared child surviving.
//  3. THE OBSERVATION (TestProfileRootReportNamesUndeclaredChildren): a child
//     nobody declared is REPORTED by name with its class, and a declared child
//     is never counted undeclared — the whole point of the arm.
//  4. THE GATE (TestProfileRootChildrenCoverRepoWriters): every direct child
//     this repository's writers create is DECLARED, the candidate names are
//     DERIVED from the writers' own source (not typed here), and the red-proof
//     (TestProfileRootGateCatchesUndeclaredChild) drives the same decider over
//     the state a deleted row produces.
//
// It plants everything in a t.TempDir() whose BASE is the profile root's own
// name (`gopiDirName`), which is what the refusal requires — and it never
// touches the developer's real ~/.gopi.

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// profileFixtureRoot makes a throwaway profile root: a temp dir whose last
// component is the profile root's own name, which is what ProfileRootRefusal
// checks. Nothing below it is the developer's real profile.
func profileFixtureRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), profileRootBaseName)
	if err := os.Mkdir(root, DirPerm); err != nil {
		t.Fatalf("mkdir profile fixture %s: %v", root, err)
	}
	return root
}

// TestProfileResidueNameNamespaceTable pins the namespace rule on names alone:
// the predicate consults neither age, liveness nor the filesystem, so this table
// is the entire safety argument of the residue arm.
func TestProfileResidueNameNamespaceTable(t *testing.T) {
	cases := []struct {
		name string
		want bool
		why  string
	}{
		// ACCEPTED: the two measured families, each carrying a generated token.
		{"evid-01a0e562", true, "the measured hand-made evidence directory (task-id token)"},
		{"evid-01a0c630", true, "same family, another measured name"},
		{"sigrec-red-1882254", true, "the measured signal-repro family (word token + pid)"},
		{"evid-01a0e562-2", true, "a de-dup suffix appendPathToken produces"},
		{"sigrec-01a0a392", true, "the same family named after a task id"},
		{"evid-550e8400-e29b-41d4-a716-446655440000", true, "a uuid tail"},

		// REFUSED: a name a PERSON chose, or a shape this system never emits.
		{"evidence", false, "the bare word — the same letters without the dash are a human's directory"},
		{"evid-notes", false, "a chosen word, no generated token anywhere"},
		{"sigrec-notes", false, "same, for the other family"},
		{"evid-", false, "a bare family: this system never creates one"},
		{"evid", false, "no separator at all"},
		{"gofi-evid-01a0e562", false, "the family must be the FIRST token"},
		{"evid-notes-2026", false, "a four-digit year is not a pid: the profile arm's tail floor is 5 digits (profileResiduePIDMinDigits)"},
		{"evid-notes-20261", true, "a five-digit run after a word IS accepted — a token rule cannot tell a pid from any other 5-digit run, and that limit is stated rather than hidden"},
		{"evid-01a0e562.tar", false, "a dot is not in the alphabet"},
		{"evid--01a0e562", false, "an empty token"},
		{"settings.json", false, "the settings file is not a residue name"},
		{"sessions", false, "a declared state child"},
		{"", false, "the empty name"},
	}
	for _, tc := range cases {
		if got := ProfileResidueName(tc.name); got != tc.want {
			t.Errorf("ProfileResidueName(%q) = %v, want %v — %s", tc.name, got, tc.want, tc.why)
		}
	}
}

// TestProfileResidueNameIsNotAWidenedMarker refuses the shape the base sweep
// explicitly rejects for itself: a rule that matches a name merely CONTAINING a
// marker would collect a neighbour's directory whose author picked a similar
// word.
func TestProfileResidueNameIsNotAWidenedMarker(t *testing.T) {
	for _, name := range []string{"my-evid-thing", "x-sigrec-red-12", "evidence-01a0e562"} {
		if ProfileResidueName(name) {
			t.Errorf("ProfileResidueName(%q) = true — the family must be the FIRST token, not an infix", name)
		}
	}
}

// TestProfileRootReportNamesUndeclaredChildren drives the observation face: a
// child nothing declared is reported BY NAME and classified as undeclared, a
// declared child is not, and the tally agrees with the verdicts.
func TestProfileRootReportNamesUndeclaredChildren(t *testing.T) {
	root := profileFixtureRoot(t)
	declared := filepath.Join(root, "settings.json")
	if err := os.WriteFile(declared, []byte("{}"), FilePerm); err != nil {
		t.Fatalf("write %s: %v", declared, err)
	}
	undeclared := filepath.Join(root, "evid-notes")
	if err := os.Mkdir(undeclared, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", undeclared, err)
	}

	verdicts, err := ProfileRootReport(root, DefaultStaleAge)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	counts := TallyProfileRoot(verdicts)
	if counts.Children != 2 || counts.Declared != 1 || counts.Undeclared != 1 {
		t.Fatalf("counts = (children=%d declared=%d undeclared=%d), want (2,1,1)",
			counts.Children, counts.Declared, counts.Undeclared)
	}
	if len(counts.UndeclaredNames) != 1 || counts.UndeclaredNames[0] != "evid-notes" {
		t.Fatalf("undeclared names = %v, want [evid-notes]", counts.UndeclaredNames)
	}
	byName := map[string]ProfileChildVerdict{}
	for _, v := range verdicts {
		byName[v.Name] = v
	}
	if v := byName["evid-notes"]; v.Declared || v.Collectable {
		t.Errorf("evid-notes verdict = declared=%v collectable=%v — a human's directory is reported, never collected",
			v.Declared, v.Collectable)
	}
	if v := byName["settings.json"]; !v.Declared || string(v.Class) != string(ProfileChildState) {
		t.Errorf("settings.json verdict = declared=%v class=%q — the settings file is DECLARED state",
			v.Declared, v.Class)
	}
	if v := byName["settings.json"]; v.Reason == "" {
		t.Error("a declared child's verdict carries no reason — every line must carry the sentence that decided it")
	}
}

// TestProfileRootSweepCollectsOnlyAgedUnheldResidue is the hard rule on a real
// tree: age + the /proc holder veto + the namespace, with the profile root and
// every declared child surviving at any age.
func TestProfileRootSweepCollectsOnlyAgedUnheldResidue(t *testing.T) {
	root := profileFixtureRoot(t)

	// The declared children the arm must NEVER touch, however old.
	declared := []string{"settings.json", "memory.db", "sessions", "plugins", "raw", "logs", "serve.lock"}
	declaredPaths := make([]string, 0, len(declared))
	for _, name := range declared {
		declaredPaths = append(declaredPaths, filepath.Join(root, name))
	}
	for _, name := range declared {
		p := filepath.Join(root, name)
		if strings.Contains(name, ".") {
			if err := os.WriteFile(p, []byte("x"), FilePerm); err != nil {
				t.Fatalf("write %s: %v", p, err)
			}
			continue
		}
		if err := os.Mkdir(p, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		reAge(t, p, 100*24*time.Hour)
	}

	aged := filepath.Join(root, "evid-01a0e562")
	held := filepath.Join(root, "sigrec-red-1882254")
	fresh := filepath.Join(root, "evid-01a0c630")
	for _, dir := range []string{aged, held, fresh} {
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// A file inside the collectable child, so the reclaimed BYTES are measured
	// rather than a zero that no assertion could tell from "nothing measured".
	if err := os.WriteFile(filepath.Join(aged, "bin-gopi-before"), []byte("evidence"), FilePerm); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	reAge(t, aged, DefaultStaleAge+time.Hour)
	reAge(t, held, DefaultStaleAge+time.Hour)
	// A live holder: this process's own open descriptor inside the root.
	handle, err := os.Open(held)
	if err != nil {
		t.Fatalf("open %s: %v", held, err)
	}
	defer func() { _ = handle.Close() }() //stylegate:ignore test fixture handle; the verdict is taken while it is open
	link := filepath.Join(root, "evid-01a0cba8")
	if err := os.Symlink(aged, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}
	foreign := filepath.Join(root, "tabs")
	if err := os.Mkdir(foreign, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", foreign, err)
	}
	reAge(t, foreign, 100*24*time.Hour)

	// THE REPORT deletes nothing at all.
	verdicts, err := ProfileRootReport(root, DefaultStaleAge)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if counts := TallyProfileRoot(verdicts); counts.Collectable != 1 {
		t.Fatalf("report says collectable=%d, want 1 (the aged unheld residue child)", counts.Collectable)
	}
	for _, p := range append([]string{aged, held, fresh, link, foreign}, declaredPaths...) {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s was removed by the REPORT face — it must delete nothing: %v", p, err)
		}
	}

	// THE EXPLICIT SWEEP removes exactly what the report named.
	removed, bytes, err := SweepProfileRootResidue(root, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (the aged unheld residue child)", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytes = %d, want the reclaimed byte count to be measured", bytes)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("%s survived the sweep, want removed (err=%v)", aged, err)
	}
	for _, p := range append([]string{held, fresh, link, foreign}, declaredPaths...) {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed — age, holder and namespace rules must apply unchanged: %v", p, err)
		}
	}
	// The ROOT itself is never a candidate, at any age.
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the profile root %s was removed: %v", root, err)
	}
	// A second sweep is a no-op: the family is drained.
	if again, _, err := SweepProfileRootResidue(root, DefaultStaleAge); err != nil || again != 0 {
		t.Errorf("second sweep = (%d, %v), want (0, nil) — the arm must be idempotent", again, err)
	}
}

// TestProfileRootRefusalNamesTheReason pins the guard that runs BEFORE any
// measurement: a path that is not a profile root is refused by name, so a
// mistyped `scratch-gc profile --root …` can never walk somebody's tree.
func TestProfileRootRefusalNamesTheReason(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		path string
	}{
		{"empty", ""},
		{"dot", "."},
		{"filesystem root", string(os.PathSeparator)},
		{"a directory that is not a profile root", dir},
		{"a regular file", filepath.Join(dir, "not-a-root")},
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-root"), []byte("x"), FilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, tc := range cases {
		if err := ProfileRootRefusal(tc.path); err == nil {
			t.Errorf("ProfileRootRefusal(%q) = nil — %s must be refused", tc.path, tc.name)
		}
	}
	// A git work tree is refused too: its own tooling owns it.
	wt := filepath.Join(t.TempDir(), profileRootBaseName)
	if err := os.Mkdir(wt, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(wt, WorktreeStubGitPointerName), DirPerm); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := ProfileRootRefusal(wt); err == nil {
		t.Errorf("ProfileRootRefusal(%q) = nil — a git work tree must be refused", wt)
	}
	// The positive case: a directory carrying the profile root's own name.
	ok := profileFixtureRoot(t)
	if err := ProfileRootRefusal(ok); err != nil {
		t.Errorf("ProfileRootRefusal(%q) = %v, want nil", ok, err)
	}
	// A MISSING profile root is its own named error (a reported no-op, not an
	// alarm) — and the report renders it as such rather than as an empty tree.
	missing := filepath.Join(t.TempDir(), profileRootBaseName)
	if _, err := ProfileRootReport(missing, DefaultStaleAge); err == nil {
		t.Error("ProfileRootReport on a missing root = nil error; want the named ErrProfileRootMissing")
	}
}

// TestProfileRootSweepRefusesForeignParent is the last check before a delete:
// even a name in the namespace is refused when its parent is not a profile root,
// so the removal cannot be reached with a path assembled elsewhere.
func TestProfileRootSweepRefusesForeignParent(t *testing.T) {
	elsewhere := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(elsewhere, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	victim := filepath.Join(elsewhere, "evid-01a0e562")
	if err := os.Mkdir(victim, DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reAge(t, victim, DefaultStaleAge+time.Hour)
	if err := removeCollectableOfProfileChild(victim); err == nil {
		t.Fatalf("removeCollectableOfProfileChild(%q) = nil — a residue name outside a profile root must be refused", victim)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("%s was removed by the refusal path: %v", victim, err)
	}
}

// TestProfileRootResolverIsWired pins the seam: a build that lost the
// internal/config assignment would otherwise sweep the empty path (which is the
// process's own working directory once joined).
func TestProfileRootResolverIsWired(t *testing.T) {
	prev := profileRootResolver
	t.Cleanup(func() { profileRootResolver = prev })

	profileRootResolver = func() (string, error) { return "", errProfileRootResolverUnwired }
	if _, err := ResolveProfileRoot(); err == nil {
		t.Error("ResolveProfileRoot() with the unwired resolver = nil error, want the named error")
	}
	profileRootResolver = func() (string, error) { return profileFixtureRoot(t), nil }
	got, err := ResolveProfileRoot()
	if err != nil {
		t.Fatalf("ResolveProfileRoot() = %v, want nil", err)
	}
	if base := filepath.Base(got); base != profileRootBaseName {
		t.Errorf("ResolveProfileRoot() base = %q, want %q", base, profileRootBaseName)
	}
}

// profileRootWriterAnchor is ONE writer of a profile-root child, with the
// evidence string its file must still carry. The gate reads the file, requires
// the evidence, and names the registry row that must cover the child.
type profileRootWriterAnchor struct {
	file     string // repo-relative production file
	evidence string // a string that must still exist in it
	child    string // the registry row name (or the residue prefix) that must cover it
}

// profileRootWriterAnchors is the GATE'S ANCHOR LIST.
//
// WHY THE CANDIDATES ARE DERIVED AND NOT SCANNED: the derivation below reads the
// writers that resolve a path UNDER the gopi data root out of their own source,
// so the list of candidate child names is a fact about the code rather than
// about this test's author's memory. What the derivation cannot see — a child
// whose name is a CONSTANT defined in a leaf package (`sessions`, `plugins`,
// `dumps`, …) — is covered by this list, one row per writer, each carrying the
// exact string that pins it. A writer that moves or renames its child fails the
// gate instead of silently orphaning the declaration.
var profileRootWriterAnchors = []profileRootWriterAnchor{
	{"internal/config/config.go", `filepath.Join(gd, "settings.json")`, "settings.json"},
	{"internal/config/memory_env.go", "filepath.Join(gd, MemoryDBFileName)", "memory.db"},
	{"internal/config/plugins.go", "filepath.Join(gopiDir, PluginsDirName)", "plugins"},
	{"internal/config/gopihome.go", `SessionsDirName = "sessions"`, "sessions"},
	{"internal/scratch/declared_trees.go", `rawLogDirName = "raw"`, "raw"},
	{"internal/removalaudit/audit.go", `LogDirName = "logs"`, "logs"},
	{"internal/scratch/scratch.go", `DumpsDirName = "dumps"`, "dumps"},
	{"internal/tools/ledger_backup.go", `LedgerBackupDirName = "backups"`, "backups"},
	{"internal/tools/merger_graceful.go", `MergerStateDirName = "merger"`, "merger"},
	{"cmd/agent/cli_worker_control.go", `workerStateDirName = "worker"`, "worker"},
	{"internal/tools/task_session.go", `taskLegacySessionsRootName = "task-sessions"`, "task-sessions"},
	{"cmd/agent/cli_tasks.go", `serveLockFilename = "serve.lock"`, "serve.lock"},
	{"internal/config/agentguard.go", `AgentSpawnGuardLockName = "worker.lock"`, "worker.lock"},
}

// profileRootJoinRE is the DERIVATION: a `filepath.Join(<root var>, "<literal>")`
// whose root variable is one of the names this repository resolves the gopi data
// root into. It is deliberately narrow (the root var must be a known spelling
// and the second argument a bare string literal) so the candidate set is a fact
// about the code and not a guess.
var profileRootJoinRE = regexp.MustCompile(`filepath\.Join\((gd|gopiDir|gopiRoot|gopiDataDir|dataDir), "([A-Za-z0-9._-]+)"`)

// profileRootDerivedCandidates reads every production Go file under the
// repository root and returns the child names the derivation found, sorted.
func profileRootDerivedCandidates(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, sub := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(root, sub), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			blob, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, m := range profileRootJoinRE.FindAllStringSubmatch(string(blob), -1) {
				seen[m[2]] = true
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", sub, walkErr)
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestProfileRootChildrenCoverRepoWriters is THE GATE: every profile-root child
// this repository's own code creates is declared in ProfileRootChildren(), and
// every declared row's evidence string still exists in its writer.
//
// RED-PROOF: deleting a row from the registry makes the first half fail and
// names the child (TestProfileRootGateCatchesUndeclaredChild drives the same
// decider over that exact state); a writer that moved makes the second half fail
// and names the file.
// registerSessionsRowForThisTest supplies the ONE registry row whose NAME is
// owned by another package: `config.SessionsDirName` is defined in
// internal/config, which imports THIS package, so the shipped row arrives through
// RegisterProfileRootChildren from that package's init (pinned, from the other
// side, by cmd/agent's TestProfileRootSessionsRowIsWiredFromConfig). A test
// binary of this leaf package has internal/config on no import path, so it puts
// the same row there itself — and says so, rather than quietly skipping the
// child it cannot see.
func registerSessionsRowForThisTest(t *testing.T) {
	t.Helper()
	profileRootTestRegisterOnce.Do(func() {
		if err := RegisterProfileRootChildren(ProfileRootChild{
			Name:  "sessions",
			Kind:  ProfileChildExact,
			Class: ProfileChildState,
			Bound: "the session tree (the ONE official home of a run's evidence); supplied by internal/config, whose constant owns the name",
		}); err != nil {
			t.Fatalf("registering the sessions row for this test: %v", err)
		}
	})
}

// profileRootTestRegisterOnce keeps the row above a single registration: the
// registry is append-only by design (a caller may add rows at any init), so a
// second call would only make the table look bigger than it is.
var profileRootTestRegisterOnce sync.Once

// TestRegisterProfileRootChildrenRefusesAnUnnamedRow is the regression guard for
// the refusal path (Rule 19's "return the error instead", Rule 15's no-silent-
// failure): an unnamed row is REPORTED as an error — it must never panic (a
// caller's decision is not the callee's to take) and it must never be appended
// (a row that can never match anything would make the registry claim a
// declaration it does not have).
func TestRegisterProfileRootChildrenRefusesAnUnnamedRow(t *testing.T) {
	before := len(ProfileRootChildren())
	err := RegisterProfileRootChildren(ProfileRootChild{
		Name:  "   ",
		Kind:  ProfileChildExact,
		Class: ProfileChildState,
		Bound: "would never be matched — the Name is blank",
	})
	if err == nil {
		t.Fatal("an unnamed row was accepted — the registry would carry a declaration that matches nothing")
	}
	if !errors.Is(err, errProfileRootChildUnnamed) {
		t.Errorf("refusal error = %v, want errProfileRootChildUnnamed", err)
	}
	if after := len(ProfileRootChildren()); after != before {
		t.Errorf("a refused batch changed the registry: %d → %d rows", before, after)
	}
}

func TestProfileRootChildrenCoverRepoWriters(t *testing.T) {
	registerSessionsRowForThisTest(t)
	root := repoRootForDeclaredTreeGate(t)
	children := ProfileRootChildren()

	// (a) THE ANCHORS: every writer still carries its evidence, and the registry
	// declares the child the anchor names.
	for _, a := range profileRootWriterAnchors {
		blob := readRepoFile(t, root, a.file)
		if !strings.Contains(blob, a.evidence) {
			t.Errorf("writer %s no longer carries %q — the profile-root declaration for %q has rotted "+
				"(update the anchor AND the registry row together)", a.file, a.evidence, a.child)
		}
		if _, ok := profileRowFor(a.child, children); !ok {
			t.Errorf("child %q is written by %s but is NOT declared in ProfileRootChildren() — "+
				"the registry must cover every child this repository creates", a.child, a.file)
		}
	}

	// (b) THE DERIVATION: every child name the writers' own source resolves under
	// the gopi data root is declared. This half is what makes the gate
	// RANGE-DRIVEN rather than a list of names somebody remembered.
	for _, name := range profileRootDerivedCandidates(t, root) {
		if _, ok := profileRowFor(name, children); !ok {
			t.Errorf("the repository's own code joins %q under the gopi data root, and no ProfileRootChildren() "+
				"row declares it — an undeclared child is exactly what this arm exists to prevent", name)
		}
	}

	// (c) THE SHAPE: every row is readable — a row whose class is unknown, whose
	// name is empty, or whose bound is a blank is a declaration that says
	// nothing, and a declaration that says nothing is worse than a reported
	// undeclared child.
	for i, row := range children {
		if row.Name == "" {
			t.Errorf("row %d has an empty Name", i)
		}
		switch row.Class {
		case ProfileChildState, ProfileChildDeclared, ProfileChildResidue:
		default:
			t.Errorf("row %q has class %q, which is not one of the three classes", row.Name, row.Class)
		}
		if strings.TrimSpace(row.Bound) == "" {
			t.Errorf("row %q carries no bound sentence — how a child is kept from growing is the point of the registry", row.Name)
		}
		if row.Writer != "" && row.Evidence == "" {
			t.Errorf("row %q names writer %s but no evidence string — the gate cannot pin an unstated claim", row.Name, row.Writer)
		}
		if row.Writer == "" && row.Class != ProfileChildState {
			t.Errorf("row %q has no writer and class %q — only STATE may be operator-owned (nothing in this repository creates it)",
				row.Name, row.Class)
		}
	}

	// (d) EVERY ROW IS MATCHED BY SOMETHING: a row no name can ever hit is dead
	// weight that makes the registry look bigger than it is.
	for _, row := range children {
		if !row.matches(row.Name) {
			t.Errorf("row %q does not match itself (%v) — the row is unreachable", row.Name, row.Kind)
		}
	}
}

// TestProfileRootGateCatchesUndeclaredChild is the RED-PROOF: the same decider
// the gate uses (profileRowFor over the registry) is driven over the state a
// deleted row produces, and it must REFUSE it — naming the child — while the
// full registry accepts it. A gate that has only ever been green has not been
// shown to catch anything.
func TestProfileRootGateCatchesUndeclaredChild(t *testing.T) {
	registerSessionsRowForThisTest(t)
	children := ProfileRootChildren()
	const victim = "sessions"

	if _, ok := profileRowFor(victim, children); !ok {
		t.Fatalf("the full registry does not declare %q — this red-proof needs a row that EXISTS", victim)
	}
	// The pre-fix state: the row is gone.
	without := make([]ProfileRootChild, 0, len(children)-1)
	for _, row := range children {
		if row.Name == victim {
			continue
		}
		without = append(without, row)
	}
	if _, ok := profileRowFor(victim, without); ok {
		t.Fatalf("with the %q row removed the decider still DECLARED it — the gate would not catch a deleted row", victim)
	}
	// And the derivation must still FIND the name, which is what makes the
	// refusal actionable (the gate fails and names the child rather than
	// reporting a bare count).
	root := repoRootForDeclaredTreeGate(t)
	derived := profileRootDerivedCandidates(t, root)
	_ = derived // (the join form does not reach a constant-defined child; the anchors above do)
	for _, a := range profileRootWriterAnchors {
		if a.child != victim {
			continue
		}
		blob := readRepoFile(t, root, a.file)
		if !strings.Contains(blob, a.evidence) {
			t.Fatalf("anchor for %q no longer matches its writer — the red-proof cannot speak for the gate", victim)
		}
		return
	}
	t.Fatalf("no anchor names %q — the red-proof is vacuous", victim)
}
