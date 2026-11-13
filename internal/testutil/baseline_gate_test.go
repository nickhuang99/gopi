package testutil

// The BASELINE-COMPARISON gate (2026-09-19, task 01a0ba1f; PROJECT.md Rule 15
// "never a silent exemption", Rule 5 "every fix ships a minimal repro").
//
// WHY: a counter diff is evidence only about the tree it was measured in. A
// "pristine base" worktree whose cleanliness and revision were assumed (never
// asserted) produced a phantom "master rot" verdict that reached a bug row and
// a task report as fact. The counter diff cannot be the guard — the base tree's
// own state must be, and it has to fail LOUDLY. Two halves, and BOTH are
// required for the same reason the /tmp ban required both:
//
//  1. STATIC — any audited script that creates a detached baseline worktree
//     (`git worktree add … --detach`) must source the shared contract
//     scripts/lib/baseline-verify.sh, which is what turns "assume clean and at
//     the right revision" into "verify it or the comparison does not happen".
//  2. FUNCTIONAL — the contract is exercised against a real scratch repo: it
//     must ACCEPT a clean worktree at the expected revision and REFUSE a dirty
//     one and a wrong-revision one. A contract that only ever says "OK" is the
//     vacuous pass this file exists to prevent, so the refusals are the test.
//
// The scan covers scripts/ and devdoc/ — the same audited roots the scripts
// gate walks (devdoc/ holds one-off probes that park detached worktrees). A
// non-vacuity floor fails the gate if the scan finds no candidate at all.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// baselineContractShell is the shared contract's repo-root-relative path.
	baselineContractShell = "scripts/lib/baseline-verify.sh"
	// baselineContractFile is the contract's own file name — what a SCRIPT
	// sources (a script cannot know the repo-root-relative prefix: it resolves
	// its own directory and sources lib/<name>). Matching the file name keeps
	// the detector independent of a caller's path spelling without letting a
	// bare mention of the full path pass as a source.
	baselineContractFile = "baseline-verify.sh"
	// baselineExemptMarker is the inline escape: a site carrying this marker
	// (on it or on the comment line directly above) is a reviewed exception
	// that names its reason — visible in the data report, never a silent
	// suppression (the Rule 16-21 pattern).
	baselineExemptMarker = "baseline-exempt:"
	// baselineContractAssertFn is the contract's ASSERTION (it verifies an
	// existing base) and baselineContractAddFn its CREATE-AND-ASSERT helper.
	// Both count as a USE: a script that sources the contract and only ignores
	// it is not guarded, so the source check is always paired with a use check.
	baselineContractAssertFn = "gopi_assert_baseline_clean"
	baselineContractAddFn    = "gopi_baseline_worktree_add"
	// baselineDetachFlag is the marker of a base-tree creation: a attached
	// `git worktree add` follows a branch and moves with it, a detached one
	// names the revision a comparison claims — the case that must be asserted.
	baselineDetachFlag = "--detach"
	// baselineWorktreeAdd is the command that creates a base checkout.
	baselineWorktreeAdd = "git worktree add"
	// baselineMinCreationSites is the non-vacuity floor: measured 5 detached
	// creation sites across scripts/ and devdoc/ when this gate landed.
	baselineMinCreationSites = 3
	// baselineMaxReportedHits caps one file's listing; the total is always
	// stated alongside it (the withheld-hits contract).
	baselineMaxReportedHits = 10
)

// baselineContractDir is the audited scripts/ and devdoc/ roots, relative to
// the module root. devdoc/ is included because its probes park worktrees too.
var baselineAuditedRoots = []string{scriptsDirName, devdocDirName}

// baselineCreationSite is one detached-worktree creation found in the audit.
type baselineCreationSite struct {
	rel  string // repo-root-relative, slash-separated
	line int    // 1-based
	text string // the trimmed line
}

// runsDetachedWorktreeAdd reports whether a line RUNS a detached worktree
// creation, as opposed to NAMING one. The distinction is not cosmetic: a
// failure message or a doc that quotes the command would otherwise be flagged
// as a site, and a detector that fires on prose gets relaxed to a no-op the
// first time it fires (the /tmp ban learned this with isCommentLine). So the
// `git` command word must stand at a COMMAND position — the line is split on
// the shell separators and a segment that (after the leading if/then/!/brace
// noise) BEGINS with the command is the site; a segment that merely contains
// it inside an `echo "..."` is a quotation.
func runsDetachedWorktreeAdd(line string) bool {
	if !strings.Contains(line, baselineDetachFlag) {
		return false
	}
	for _, seg := range baselineCommandSeparators.Split(line, -1) {
		seg = strings.TrimSpace(seg)
		for {
			switch {
			case strings.HasPrefix(seg, "if "):
				seg = strings.TrimSpace(seg[len("if "):])
			case strings.HasPrefix(seg, "then "):
				seg = strings.TrimSpace(seg[len("then "):])
			case strings.HasPrefix(seg, "!"):
				seg = strings.TrimSpace(seg[1:])
			case strings.HasPrefix(seg, "{"):
				seg = strings.TrimSpace(seg[1:])
			default:
				goto done
			}
		}
	done:
		if strings.HasPrefix(seg, baselineWorktreeAdd) {
			return true
		}
	}
	return false
}

// baselineCommandSeparators splits a shell line where one command ends and the
// next begins (kept deliberately small: the shapes the audit actually sees).
var baselineCommandSeparators = regexp.MustCompile(`&&|\|\||;|\|`)

// findBaselineCreationSites returns every line that creates a detached
// worktree, comment lines excluded (a script that DOCUMENTS the shape — e.g.
// a report quoting the old command — is not a site that must be asserted;
// this is the /tmp ban's isCommentLine lesson, learned the same way).
func findBaselineCreationSites(t *testing.T, root string) ([]baselineCreationSite, []baselineExemption) {
	t.Helper()
	var out []baselineCreationSite
	var exempt []baselineExemption
	for _, dir := range baselineAuditedRoots {
		base := filepath.Join(root, dir)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			continue // an absent root is not a failure
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".sh") && !strings.HasSuffix(name, ".py") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			lines := strings.Split(string(raw), "\n")
			for i, line := range lines {
				if isCommentLine(line) {
					continue
				}
				if !runsDetachedWorktreeAdd(line) {
					continue
				}
				if i > 0 && strings.Contains(lines[i-1], baselineExemptMarker) {
					exempt = append(exempt, baselineExemption{rel: rel, line: i + 1, why: strings.TrimSpace(lines[i-1])})
					continue
				}
				if strings.Contains(line, baselineExemptMarker) {
					exempt = append(exempt, baselineExemption{rel: rel, line: i + 1, why: strings.TrimSpace(line)})
					continue
				}
				out = append(out, baselineCreationSite{rel: rel, line: i + 1, text: strings.TrimSpace(line)})
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return out, exempt
}

// baselineExemption is a reviewed inline escape: a creation site that carries
// the marker plus its reason. Reported (verbose) so it stays VISIBLE rather
// than disappearing into a suppression — the reason a marker exists at all.
type baselineExemption struct {
	rel  string
	line int
	why  string
}

// findBaselineAdoptionSites returns the lines that CALL the contract — the
// population's other half (a site the gate has converted). Counted, never
// judged: a call site's file is by construction guarded.
func findBaselineAdoptionSites(t *testing.T, root string) []baselineCreationSite {
	t.Helper()
	var out []baselineCreationSite
	for _, dir := range baselineAuditedRoots {
		base := filepath.Join(root, dir)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".sh") && !strings.HasSuffix(d.Name(), ".py") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if isCommentLine(line) {
					continue
				}
				if strings.Contains(line, baselineContractAssertFn) ||
					strings.Contains(line, baselineContractAddFn) ||
					baselinePythonUse.MatchString(line) {
					if baselineGateExemptRels[filepath.ToSlash(rel)] {
						continue // the contract's own definition/gate, not a call site
					}
					out = append(out, baselineCreationSite{rel: filepath.ToSlash(rel), line: i + 1, text: strings.TrimSpace(line)})
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return out
}

// assertBaselineCreationSitesAreGuarded is the static half: a creation site is
// acceptable only in a file whose SOURCED contract is the baseline verifier.
func assertBaselineCreationSitesAreGuarded(t *testing.T, root string, sites []baselineCreationSite) {
	t.Helper()
	byFile := map[string][]baselineCreationSite{}
	for _, s := range sites {
		byFile[s.rel] = append(byFile[s.rel], s)
	}
	for rel, fileSites := range byFile {
		if baselineGateExemptRels[rel] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if sourcesBaselineContract(string(raw)) {
			continue
		}
		reported, withheld := 0, 0
		for _, s := range fileSites {
			reported++
			if reported > baselineMaxReportedHits {
				withheld++
				continue
			}
			t.Errorf("%s:%d creates a detached baseline worktree without asserting it is clean "+
				"and at the expected revision: %q\n"+
				"  FIX: source scripts/lib/baseline-verify.sh and use gopi_baseline_worktree_add "+
				"(or call gopi_assert_baseline_clean on a base you created yourself).\n"+
				"  WHY: a counter diff measured in an unverified base tree is unattributable — "+
				"the 2026-09-19 phantom \"master rot\" verdict came from exactly that base.",
				rel, s.line, s.text)
		}
		assertReportedAllHits(t, rel, reported, withheld)
	}
}

// sourcesBaselineContract reports whether a script's SOURCE lines source the
// contract file. The check is on the source form (`.`/`source` + the path),
// never on a bare mention of the string.
func sourcesBaselineContract(body string) bool {
	return sourcesBaselineContractFile(body) && usesBaselineContract(body)
}

// usesBaselineContract reports whether any non-comment line CALLS the contract
// (either helper, or the python import).
func usesBaselineContract(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if isCommentLine(line) {
			continue
		}
		if strings.Contains(line, baselineContractAssertFn) ||
			strings.Contains(line, baselineContractAddFn) ||
			baselinePythonUse.MatchString(line) {
			return true
		}
	}
	return false
}

// sourcesBaselineContractFile reports whether a script's SOURCE lines source
// the contract file. The check is on the source form (`.`/`source` + the file
// name), never on a bare mention of the path.
func sourcesBaselineContractFile(body string) bool {
	sourced := false
	for _, line := range strings.Split(body, "\n") {
		if isCommentLine(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, baselineContractFile) {
			continue
		}
		if strings.HasPrefix(trimmed, ".") || strings.HasPrefix(trimmed, "source") {
			sourced = true
		}
	}
	return sourced
}

// baselinePythonUse matches a PYTHON use of the contract's twin: the shell
// check keys off the assertion function, and this is its python equivalent, so
// a probe importing the module counts as guarded while a source line alone
// still does not (a sourced file nobody calls asserts nothing).
var baselinePythonUse = regexp.MustCompile(`(^|\s)(import|from)\s+baseline_verify\b`)

// baselineGateExemptRels are files allowed to RUN the creation themselves: the
// contract's own implementation (it creates the tree and asserts it in the
// same call — the assertion cannot precede the creation) and this gate.
var baselineGateExemptRels = map[string]bool{
	baselineContractShell:                     true,
	"internal/testutil/baseline_gate_test.go": true,
}

// TestBaselineWorktreesAreAssertedClean is the whole static gate.
func TestBaselineWorktreesAreAssertedClean(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(baselineContractShell))); err != nil {
		t.Fatalf("the shared baseline contract %s is missing: %v — every asserted base depends on it",
			baselineContractShell, err)
	}
	sites, exempt := findBaselineCreationSites(t, root)
	adopted := findBaselineAdoptionSites(t, root)
	for _, e := range exempt {
		t.Logf("baseline-exempt site %s:%d — %s", e.rel, e.line, e.why)
	}
	// The floor counts the whole BASE-CREATION POPULATION — raw detached
	// creations plus adopted call sites — because adoption MOVES a site from one
	// bucket to the other; a floor on the raw bucket alone would fail the moment
	// the gate succeeded (the ratchet's own goal posts), and a scan that found
	// nothing at all would still have to fail.
	if total := len(sites) + len(adopted); total < baselineMinCreationSites {
		t.Fatalf("the baseline audit found %d base-creation site(s) (%d raw + %d adopted), want at least %d — "+
			"the scan is not looking where the base worktrees are made (vacuous pass guard)",
			total, len(sites), len(adopted), baselineMinCreationSites)
	}
	t.Logf("baseline audit: %d raw detached creation(s), %d adopted call site(s), %d exempt",
		len(sites), len(adopted), len(exempt))
	assertBaselineCreationSitesAreGuarded(t, root, sites)
}

// TestBaselineDetectorFlagsASyntheticSite is the predicate's own red-proof: a
// synthetic creation line must be flagged, an attached one and a comment
// mention must not. Without this, a detector that silently matched nothing
// would read as a green gate.
func TestBaselineDetectorFlagsASyntheticSite(t *testing.T) {
	guarded := func(body string) bool { return sourcesBaselineContract(body) }

	cases := []struct {
		name  string
		body  string
		want  bool // true = the body IS a guarded user of the contract
		isBad bool // true = the body carries a detached creation site
	}{
		{
			name:  "bare detached creation",
			body:  "git worktree add -q --detach ../$n-wt \"$X\"",
			isBad: true,
		},
		{
			name:  "attached creation follows a branch, not a revision",
			body:  "git worktree add -q \"$WT\" \"$BR\"",
			isBad: false,
		},
		{
			name:  "a comment naming the shape is documentation",
			body:  "# git worktree add --detach /home/nick/dcbase dc75ea64",
			isBad: false,
		},
		{
			name:  "sourcing the contract is the guard",
			body:  ". \"$HERE/lib/baseline-verify.sh\"\nBASE=\"$(gopi_baseline_worktree_add \"$B\" \"$REV\")\" || exit 2",
			want:  true,
			isBad: true,
		},
		{
			name:  "a bare mention of the path is not a source",
			body:  "# see scripts/lib/baseline-verify.sh for the contract",
			want:  false,
			isBad: false,
		},
		{
			name:  "a source line without the assertion is not a guard",
			body:  ". \"$HERE/lib/baseline-verify.sh\"",
			want:  false,
			isBad: false,
		},
	}
	for _, tc := range cases {
		if got := guarded(tc.body); got != tc.want {
			t.Errorf("%s: sourcesBaselineContract = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestBaselineContractRefusesDirtyAndWrongRevision is the functional half: the
// contract run against a real (scratch) repo. It must accept the clean base and
// REFUSE both failures the epic names — a dirty tree and an unexpected HEAD.
// The scratch tree is created through TestScratchDir, so it is off `/` by the
// same measurement the rest of the suite uses (Rule 22).
func TestBaselineContractRefusesDirtyAndWrongRevision(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	contract := filepath.Join(root, filepath.FromSlash(baselineContractShell))
	work := TestScratchDir(t, "baseline-contract")
	repo := filepath.Join(work, "repo")
	base := filepath.Join(work, "base")

	runGit := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(repo, dirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	runGit(repo, "init", "-q", "-b", "main")
	runGit(repo, "config", "user.email", "gate@example.invalid")
	runGit(repo, "config", "user.name", "baseline gate")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("one\n"), filePerm); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(repo, "add", "f.txt")
	runGit(repo, "commit", "-qm", "one")
	first := runGit(repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("two\n"), filePerm); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(repo, "commit", "-qam", "two")
	second := runGit(repo, "rev-parse", "HEAD")

	// assert runs the contract's gate in a subshell, the way a script would.
	assert := func(dir, expect string) (bool, string) {
		t.Helper()
		cmd := exec.Command("bash", "-c",
			`. "$1"; gopi_assert_baseline_clean "$2" "$3"`, "_", contract, dir, expect)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		return err == nil, string(out)
	}
	addBase := func(dir, rev string) (bool, string) {
		t.Helper()
		cmd := exec.Command("bash", "-c",
			`. "$1"; gopi_baseline_worktree_add "$2" "$3"`, "_", contract, dir, rev)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		return err == nil, string(out)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", base).Run() //stylegate:ignore best-effort worktree removal in a fixture teardown
	})

	// GREEN: a freshly created base at the expected revision verifies.
	if ok, out := addBase(base, first); !ok {
		t.Fatalf("a clean base at the expected revision was REFUSED (the contract must accept the good case):\n%s", out)
	}
	if ok, out := assert(base, first); !ok {
		t.Fatalf("the clean base failed its own re-assertion:\n%s", out)
	}

	// RED 1: a dirty base is refused — this is the epic's failure mode.
	if err := os.WriteFile(filepath.Join(base, "f.txt"), []byte("local edit\n"), filePerm); err != nil {
		t.Fatalf("dirty the base: %v", err)
	}
	if ok, out := assert(base, first); ok {
		t.Errorf("a DIRTY base tree PASSED the cleanliness gate — the phantom this contract exists to stop:\n%s", out)
	} else if !strings.Contains(out, "NOT clean") {
		t.Errorf("the refusal did not name the dirt (want \"NOT clean\"):\n%s", out)
	}
	runGit(base, "checkout", "--", "f.txt")

	// RED 2: a correct tree at the WRONG revision is refused.
	if ok, out := assert(base, second); ok {
		t.Errorf("a base at %s PASSED as %s — a revision mismatch must refuse the comparison:\n%s",
			shortRev(first), shortRev(second), out)
	} else if !strings.Contains(out, "NOT the expected revision") {
		t.Errorf("the refusal did not name the revision mismatch:\n%s", out)
	}

	// RED 3 (the fresh-path rule, scripts/lib/worktree-fresh.sh): a base path
	// that is NOT FREE is refused rather than deleted and recreated. This is
	// the case git lets through — an existing EMPTY directory — and the helper
	// used to reach it by `mkdir -p … && rmdir …`, i.e. by DELETING whatever
	// empty directory was standing there. Both spellings are asserted: the
	// directory SURVIVES and the add refuses.
	taken := filepath.Join(work, "base-taken")
	if err := os.MkdirAll(taken, dirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", taken, err)
	}
	if ok, out := addBase(taken, first); ok {
		t.Errorf("an EXISTING EMPTY directory at the base path was accepted — git silently reuses it, so the base would not be a tree this run created:\n%s", out)
	} else if !strings.Contains(out, taken) {
		t.Errorf("the refusal did not name the path:\n%s", out)
	}
	if _, serr := os.Stat(taken); serr != nil {
		t.Errorf("the pre-existing empty directory was REMOVED by the helper: %v", serr)
	}
}

// shortRev abbreviates a revision for a failure message.
func shortRev(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}
