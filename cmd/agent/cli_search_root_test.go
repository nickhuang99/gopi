package main

// cli_search_root_test.go — the regression guard for the WRONG-GROUND refusal
// shared by `gopi tool code-search` and `gopi tool keyword_search`
// (devdoc/cli-search-wrong-ground-refusal.md).
//
// THE DEFECT THIS PINS (measured before the fix, on the real binary): searching
// from a directory that is no repository at all answered exactly as a search
// that ran and found nothing — same output shape, same exit code 1. `gopi
// code-search taskverbs.DispatchVerbs` from an empty temp dir printed "no symbol
// found: taskverbs.DispatchVerbs" and exited 1, a claim about a codebase that
// was never read. After the fix the same invocation refuses BY NAME (the
// absolute root, the channel it came from, the git work tree it sits in, what
// the walk saw, the remedy), prints that name on stderr, and exits 4.
//
// Three layers, all necessary (the same shape the keyword_search suite uses):
//  1. unit — the pure predicate and the extension derivation (no process),
//  2. the constants contract — the -h code table documents the codes the code
//     path actually uses (a documented code that drifted is the class this
//     epic exists for),
//  3. live binary — the acceptance's own before/after table, driven on the real
//     binary with real fixtures, asserting exit= on every row.

import (
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestSearchExitCodesAreDistinctAndNamed pins the SPLIT itself: the refusal must
// not collide with success or with the miss, because a caller that cannot tell
// them apart is exactly the reading this epic removes.
func TestSearchExitCodesAreDistinctAndNamed(t *testing.T) {
	if searchExitNoHits == searchExitWrongGround {
		t.Fatalf("searchExitNoHits == searchExitWrongGround (%d): the whole point of the refusal is that \"found nothing\" and \"could not search\" render differently", searchExitNoHits)
	}
	if searchExitWrongGround == 0 {
		t.Fatal("searchExitWrongGround must not be success")
	}
	// The family precedent: "the universe is empty" is the answer code
	// `gopi task show --branch` already publishes for a no-owner lookup. Joining it
	// keeps the CLI to ONE code for that reading instead of two.
	if searchExitWrongGround != tools.TaskForBranchExitNoOwner {
		t.Errorf("searchExitWrongGround = %d, want the family's no-answer code %d (tools.TaskForBranchExitNoOwner)",
			searchExitWrongGround, tools.TaskForBranchExitNoOwner)
	}
}

// TestSearchableExtensionsIn pins the derivation from the scope's globs to the
// extensions the refusal counts — the bug that made the first cut refuse a
// healthy checkout. `**/*.go` must yield ".go", never "/*.go": the trimmed form
// matches no filename, so the walk found zero sources in the repo root and the
// refusal fired on gopi's own tree (measured: 5910 entries, 0 "matching").
func TestSearchableExtensionsIn(t *testing.T) {
	got := searchableExtensionsIn(tools.CodeSearchGlobs)
	if len(got) != len(tools.CodeSearchGlobs) {
		t.Fatalf("derived %d extensions from %d globs", len(got), len(tools.CodeSearchGlobs))
	}
	for _, ext := range got {
		if !strings.HasPrefix(ext, ".") {
			t.Errorf("extension %q does not start with a dot — it can never match a file name", ext)
		}
		if strings.ContainsAny(ext, "*/") {
			t.Errorf("extension %q still carries glob syntax — it can never match a file name", ext)
		}
	}
	if !searchExtListHas(got, ".go") || !searchExtListHas(got, ".tsx") {
		t.Errorf("derived %v, want it to cover the scope's own languages (.go, .tsx)", got)
	}
	// A nil include list is the full-text scope: every file matches, and the
	// predicate must say so rather than "no extension matches anything".
	if ext := searchableExtensionsIn(nil); ext != nil {
		t.Errorf("searchableExtensionsIn(nil) = %v, want nil (no filter = every file is scannable)", ext)
	}
}

func searchExtListHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestSearchRootRefusalDecidesByWhatTheWalkSaw drives the pure predicate over
// the three fixture shapes that matter, with no process spawned for the
// decision itself: a directory with a Go file is searchable (no refusal), a
// directory with only .txt files is NOT searchable by the code-search scope but
// IS by the full-text scope, and an empty directory is not searchable by either.
func TestSearchRootRefusalDecidesByWhatTheWalkSaw(t *testing.T) {
	empty := t.TempDir()
	textOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(textOnly, "notes.txt"), []byte("nothing to index\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedWrite(t, filepath.Join(textOnly, "deep", "inner", "main.go"), "package main\n")

	for _, tc := range []struct {
		name       string
		root       string
		include    []string
		wantRefuse bool
	}{
		{name: "empty dir vs the code-search scope", root: empty, include: tools.CodeSearchGlobs, wantRefuse: true},
		{name: "empty dir vs the full-text scope", root: empty, include: nil, wantRefuse: true},
		{name: "a nested source file makes the root searchable", root: textOnly, include: tools.CodeSearchGlobs, wantRefuse: false},
		{name: "any file at all satisfies the full-text scope", root: textOnly, include: nil, wantRefuse: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal, refused := searchRootRefusal(tc.root, searchRootChannel(false), tc.include)
			if refused != tc.wantRefuse {
				t.Fatalf("refused = %v, want %v (refusal text: %s)", refused, tc.wantRefuse, refusal)
			}
			if !refused {
				return
			}
			for _, want := range []string{searchGroundLabel, tc.root, searchGroundAdviceLine} {
				if !strings.Contains(refusal, want) {
					t.Errorf("refusal does not name %q:\n%s", want, refusal)
				}
			}
		})
	}
}

// nestedWrite writes a file, creating its parent directories — a source file at
// a depth the top-level scan cannot see, which is what makes the walk (rather
// than a ReadDir) the right instrument.
func nestedWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSearchRootRefusalNeverGuesses pins the honest half: a walk that hit its
// entry cap WITHOUT finding a source must NOT refuse — "I did not finish
// looking" is not "there is nothing" (Rule 15), and refusing there would turn a
// large repository into a false refusal.
func TestSearchRootRefusalNeverGuesses(t *testing.T) {
	root := t.TempDir()
	// A source file SORTS FIRST and the walk stops at it, so this fixture is
	// about the cap only in the other direction: prove the positive case is
	// found even when it is buried deep.
	for i := 0; i < 5; i++ {
		nestedWrite(t, filepath.Join(root, "d", "sub", "deep", "f"+strconv.Itoa(i)+".txt"), "x\n")
	}
	nestedWrite(t, filepath.Join(root, "zzz", "deepest", "last.go"), "package last\n")
	if _, refused := searchRootRefusal(root, searchRootChannel(false), tools.CodeSearchGlobs); refused {
		t.Fatal("a root holding a .go file deep in the tree was refused — the walk did not reach it")
	}
}

// TestSearchGroundChannelNamesTheSource pins the channel half of the refusal:
// the caller must be able to see which knob the root came from, because a
// refusal that does not say that leaves the operator guessing whether their
// --root was even read.
func TestSearchGroundChannelNamesTheSource(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name    string
		given   bool
		wantSub string
	}{
		{name: "default", given: false, wantSub: searchRootDefaultName},
		{name: "explicit flag", given: true, wantSub: "--" + searchRootFlag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal, refused := searchRootRefusal(root, searchRootChannel(tc.given), tools.CodeSearchGlobs)
			if !refused {
				t.Fatal("an empty temp dir must be refused by the code-search scope")
			}
			if !strings.Contains(refusal, tc.wantSub) {
				t.Errorf("the refusal does not name the channel %q:\n%s", tc.wantSub, refusal)
			}
		})
	}
}

// TestSearchUsageDocumentsItsExitCodes is the code-table half: the -h page must
// carry each exit code WITH its own meaning, derived from the constants so a
// code change and its documentation cannot drift apart silently. It is the
// derivation (never a hand-typed "4") that keeps this honest — printing the
// number twice in two languages is exactly the mirror Rule 13 bans.
func TestSearchUsageDocumentsItsExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage string
	}{
		{name: CliVerbCodeSearch, usage: codeSearchUsageText()},
		{name: keywordSearchVerbName, usage: keywordSearchUsageText()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.usage, "Exit status:") {
				t.Fatalf("the usage page carries no exit-status table at all")
			}
			for _, code := range []int{searchExitOK, searchExitNoHits, searchExitWrongGround} {
				line := "\n  " + strconv.Itoa(code)
				if !strings.Contains(tc.usage, line) {
					t.Errorf("the usage page has no row for exit code %d — the code table and the code path must agree", code)
				}
			}
			// The two readings must be spelled apart on the page: a reader who
			// only sees "1 = nothing found" would keep the very confusion this
			// change removes.
			if !strings.Contains(tc.usage, "REFUSED") {
				t.Error("the usage page does not name the refusal row — the two outcomes read alike again")
			}
			// The page names the refusal with the SAME first words the stderr
			// line uses (its label's leading phrase), so a reader who greps the
			// docs and a reader who greps a log find the same thing. The full
			// label is checked against the rendered refusal elsewhere.
			if !strings.Contains(tc.usage, searchGroundLabelLead) {
				t.Errorf("the usage page does not carry the refusal's own label prefix %q", searchGroundLabelLead)
			}
		})
	}
}

// TestSearchWrongGroundExitCodeOnTheRealBinary is the acceptance's own
// before/after table, driven on the real binary. Every row asserts an exit code
// AND an output shape, because the whole defect was that the codes agreed.
//
// Before the fix the marked rows were: [before 1/1] (both wrong-ground shapes
// answered exactly like the miss).
func TestSearchWrongGroundExitCodeOnTheRealBinary(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	// A directory outside any git work tree: the search ground does not exist.
	empty := filepath.Join(home, "no-repo-here")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real repository to prove the controls are not "everything refuses":
	// the test's own checkout.
	repo := repoRoot(t)

	// The miss operands are BUILT at run time, never written whole: a literal
	// spelled in this file is itself a match inside the repository, so a
	// hand-typed token would make the miss rows hit their own source. That is
	// not hypothetical — the first cut of this test used the literal
	// "zzzNoSuchSymbolXYZ" and the ripgrep fallback found it in an evidence
	// document committed under docs/ (measured: exit 0, three hits in
	// scripts/materials/gen-topA.sh). The suffix makes the operand unique to
	// THIS run, so no file on disk can contain it.
	suffix := uniqueSuffix(t)
	const realSymbol = "taskverbs.DispatchVerbs"
	missingSymbol := "zzzNoSuchSymbol" + suffix
	missingToken := "zzzNoSuchToken" + suffix

	t.Run("refusal: empty dir, a symbol that exists in the repo", func(t *testing.T) {
		out, code := searchProbe(t, bin, home, empty, CliVerbCodeSearch, realSymbol)
		if code != searchExitWrongGround { // [before 1]
			t.Fatalf("exit = %d, want %d (the refusal code) — a wrong-ground search must not render as a miss\n%s", code, searchExitWrongGround, out)
		}
		if strings.Contains(out, "no symbol found") {
			t.Errorf("a search that never ran reported a MISS:\n%s", out)
		}
		for _, want := range []string{searchGroundLabel, empty, searchRootFlag} {
			if !strings.Contains(out, want) {
				t.Errorf("refusal does not name %q:\n%s", want, out)
			}
		}
	})

	t.Run("refusal: empty dir, keyword_search", func(t *testing.T) {
		out, code := searchProbe(t, bin, home, empty, keywordSearchVerbName, missingToken)
		if code != searchExitWrongGround { // [before 1]
			t.Fatalf("exit = %d, want %d\n%s", code, searchExitWrongGround, out)
		}
		if strings.Contains(out, "No matches.") {
			t.Errorf("a search that never ran answered \"No matches.\":\n%s", out)
		}
	})

	t.Run("miss: the repository, a symbol that is nowhere", func(t *testing.T) {
		requireCtags(t)
		out, code := searchProbe(t, bin, home, repo, CliVerbCodeSearch, missingSymbol)
		if code != searchExitNoHits {
			t.Fatalf("exit = %d, want %d (unchanged by this change)\n%s", code, searchExitNoHits, out)
		}
		if !strings.Contains(out, "no symbol found: "+missingSymbol) {
			t.Errorf("the miss message changed shape:\n%s", out)
		}
	})

	t.Run("miss: the repository, a keyword that is nowhere", func(t *testing.T) {
		requireRipgrep(t)
		out, code := searchProbe(t, bin, home, repo, keywordSearchVerbName, missingToken)
		if code != searchExitNoHits {
			t.Fatalf("exit = %d, want %d\n%s", code, searchExitNoHits, out)
		}
		if !strings.Contains(out, "No matches.") {
			t.Errorf("the miss message changed shape:\n%s", out)
		}
	})

	t.Run("hit: the repository, a real symbol (the control)", func(t *testing.T) {
		requireCtags(t)
		out, code := searchProbe(t, bin, home, repo, CliVerbCodeSearch, realSymbol)
		if code != searchExitOK {
			t.Fatalf("exit = %d, want %d — the guard must not refuse a real checkout\n%s", code, searchExitOK, out)
		}
		if !strings.Contains(out, "DispatchVerbs") {
			t.Errorf("the hit output does not name the symbol:\n%s", out)
		}
	})

	t.Run("refusal: --root naming a path that does not exist", func(t *testing.T) {
		missing := filepath.Join(home, "not-a-directory")
		out, code := searchProbe(t, bin, home, repo, CliVerbCodeSearch, realSymbol, "--"+searchRootFlag, missing)
		if code != searchExitWrongGround {
			t.Fatalf("exit = %d, want %d — \"the search could not start\" is the same outcome to a caller\n%s", code, searchExitWrongGround, out)
		}
		if !strings.Contains(out, missing) {
			t.Errorf("the refusal does not name the path it could not read:\n%s", out)
		}
	})
}

// keywordSearchVerbName is the command word of the second search verb. It is the
// tool-name SSOT's own constant (Rule 13/14) — the dispatcher answers the word
// `internal/toolnames` publishes, so a rename cannot leave a test driving a
// literal nothing answers.
var keywordSearchVerbName = toolnames.ToolKeywordSearch

// TestSearchScopeStateNamesComeFromTheirOwnDefinitions pins Rule 14 for the two
// names whose EXACT spelling decides whether gopi's own state is set aside: the
// profile directory and the sandbox ownership marker. A mirrored literal here
// would keep passing while matching nothing the initializer writes, so the test
// reads both names from the same definitions the initializer uses.
func TestSearchScopeStateNamesComeFromTheirOwnDefinitions(t *testing.T) {
	if config.ProfileDirName == "" || config.SandboxOwnerFileName == "" {
		t.Fatal("the shared names are empty — the predicates below would match nothing")
	}
	if !searchScopeStateDir(config.ProfileDirName) {
		t.Errorf("searchScopeStateDir(%q) = false — the profile directory is NOT set aside", config.ProfileDirName)
	}
	if !searchScopeStateFile(config.SandboxOwnerFileName) {
		t.Errorf("searchScopeStateFile(%q) = false — a sandbox of only the marker and the profile would pass as searchable", config.SandboxOwnerFileName)
	}
	// The negative controls, and they are the reason the match is EXACT: a
	// suffix rule on ".json" would swallow a colleague's manifest and refuse a
	// real tree — the false refusal this whole family of changes must not ship.
	for _, name := range []string{"package.json", "settings.json", "main.go", "notes.txt", ".git", "vendor"} {
		if searchScopeStateFile(name) {
			t.Errorf("searchScopeStateFile(%q) = true — gopi state is matched by NAME, never by a suffix", name)
		}
		if searchScopeStateDir(name) {
			t.Errorf("searchScopeStateDir(%q) = true — only the profile directory is state", name)
		}
	}
}

// TestSearchRefusesAProfileOnlyRootForBothScopes is the criterion's red proof,
// and it is the defect's own fixture: a root whose ONLY entries are gopi's own
// state — the profile directory and, in a sandbox, the ownership marker — is not
// a code repository, and BOTH search faces must refuse it.
//
// Measured before the fix, on the real binary (table in
// devdoc/cli-search-wrong-ground-refusal.md): `code-search` refused this tree
// (exit 4) while `keyword_search` answered "No matches." (exit 1) — one tree,
// two verbs, two readings. The rows below are driven through BOTH scopes so the
// two faces can never drift apart again.
//
// The state is built from the SHARED names (Rule 14), not from literals, so a
// rename of the profile directory cannot make this test pass vacuously; the
// contents are a plausible profile (a ledger file, a session log) precisely
// because a file inside the profile must not make the root searchable either.
func TestSearchRefusesAProfileOnlyRootForBothScopes(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, config.ProfileDirName)
	nestedWrite(t, filepath.Join(profile, config.MemoryDBFileName), "not a database\n")
	nestedWrite(t, filepath.Join(profile, "logs", "session.log"), "irrelevant\n")
	for _, tc := range []struct {
		name    string
		include []string
	}{ // the two faces' own scopes: the code-search scope and the full-text one
		{name: "code-search scope", include: tools.CodeSearchGlobs},
		{name: "keyword_search scope", include: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal, refused := searchRootRefusal(root, searchRootChannel(false), tc.include)
			if !refused {
				t.Fatalf("a root holding only %s is NOT a code repository, but it was accepted as searchable — this is the defect: keyword_search then answers \"No matches.\" instead of refusing\n%s",
					config.ProfileDirName, refusal)
			}
			if !strings.Contains(refusal, searchGroundLabel) {
				t.Errorf("the refusal does not carry the shared label %q:\n%s", searchGroundLabel, refusal)
			}
			if !strings.Contains(refusal, config.ProfileDirName) {
				t.Errorf("the refusal does not name the state it found (%s) — an observation, not a shrug:\n%s", config.ProfileDirName, refusal)
			}
			if !strings.Contains(refusal, searchGroundStateLead) {
				t.Errorf("the refusal does not carry the shared state line %q:\n%s", searchGroundStateLead, refusal)
			}
		})
	}

	// The sandbox shape is the same class plus its ownership marker, and the
	// marker alone is NOT state-by-suffix leniency: it is named exactly.
	sandboxRoot := t.TempDir()
	nestedWrite(t, filepath.Join(sandboxRoot, config.ProfileDirName, config.MemoryDBFileName), "not a database\n")
	nestedWrite(t, filepath.Join(sandboxRoot, config.SandboxOwnerFileName), "{}\n")
	if _, refused := searchRootRefusal(sandboxRoot, searchRootChannel(false), nil); !refused {
		t.Fatalf("a sandbox-shaped root (%s + %s, nothing else) was accepted as searchable — the shape the sandbox profile dir has in production",
			config.ProfileDirName, config.SandboxOwnerFileName)
	}

	// The other direction, on the same fixture builder: state does not SPOIL a
	// root that really does hold material. A profile beside one source file is a
	// real directory, and the full-text scope must still answer about it.
	mixed := t.TempDir()
	nestedWrite(t, filepath.Join(mixed, config.ProfileDirName, config.MemoryDBFileName), "not a database\n")
	nestedWrite(t, filepath.Join(mixed, config.SandboxOwnerFileName), "{}\n")
	nestedWrite(t, filepath.Join(mixed, "main.go"), "package main\n")
	if _, refused := searchRootRefusal(mixed, searchRootChannel(false), tools.CodeSearchGlobs); refused {
		t.Fatal("a root holding a real .go file was refused because it ALSO holds gopi state — a false refusal of a healthy tree")
	}
}

// TestKeywordSearchRefusalMatchesCodeSearchOnTheRealBinary is the two-faces
// contract, driven on the real binary and on the SAME tree: one root, both
// verbs, one refusal. The sandbox is materialised by the REAL initializer
// (`gopi tool sandbox create` — never a hand-built directory), so the fixture is the
// shape production actually produces: the profile directory and the ownership
// marker, and nothing else.
//
// Before the fix the two rows disagreed: code-search exited
// searchExitWrongGround with the named refusal while keyword_search exited
// searchExitNoHits with "No matches." — the same tree read as an answer about a
// codebase that was never read.
func TestKeywordSearchRefusalMatchesCodeSearchOnTheRealBinary(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	root := t.TempDir()
	sb := cliSandboxCreate(t, home, root, "wrong-ground")
	// The premise, asserted rather than assumed: the fixture really is the
	// profile-and-marker shape this whole test is about. A sandbox that grew
	// other entries would make the rows below prove something else.
	entries, err := os.ReadDir(sb)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len([]string{config.ProfileDirName, config.SandboxOwnerFileName}) {
		t.Fatalf("the fixture sandbox holds %d entries, want exactly the profile dir + the owner marker — this test's whole premise", len(entries))
	}

	suffix := uniqueSuffix(t)
	type row struct {
		verb string
		arg  string
	}
	for _, r := range []row{
		{verb: CliVerbCodeSearch, arg: "taskverbs.DispatchVerbs"},
		{verb: keywordSearchVerbName, arg: "zzzNoSuchToken" + suffix},
	} {
		t.Run(r.verb, func(t *testing.T) {
			out, code := searchProbe(t, bin, home, sb, r.verb, r.arg)
			if code != searchExitWrongGround {
				t.Fatalf("exit = %d, want %d (the family's refusal code) — this verb answered for a tree it never read\n%s", code, searchExitWrongGround, out)
			}
			if !strings.Contains(out, searchGroundLabel) {
				t.Errorf("the refusal does not carry the SHARED label %q — the two search faces must refuse with the same words:\n%s", searchGroundLabel, out)
			}
			if strings.Contains(out, "No matches.") || strings.Contains(out, "no symbol found") {
				t.Errorf("a search that never ran rendered as a MISS:\n%s", out)
			}
			if !strings.Contains(out, sb) {
				t.Errorf("the refusal does not name the absolute root it refused:\n%s", out)
			}
		})
	}

	// The control on the SAME binary: the empty directory's refusal must stay
	// the empty-directory refusal (the "0 files at all" observation), so the
	// new state line has not swallowed the older one.
	empty := t.TempDir()
	out, code := searchProbe(t, bin, home, empty, keywordSearchVerbName, "zzzNoSuchToken"+suffix)
	if code != searchExitWrongGround {
		t.Fatalf("empty dir: exit = %d, want %d\n%s", code, searchExitWrongGround, out)
	}
	if strings.Contains(out, config.ProfileDirName) {
		t.Errorf("an empty directory's refusal claims to hold gopi state — the two observations must not be merged:\n%s", out)
	}
}

// searchProbeToolTimeoutEnv raises the child's tool timeout above the 30s
// default (GOPI_TOOL_TIMEOUT_SEC — the ONE reader of that knob,
// internal/tools/timeout.go). The rows below are contract rows, not performance
// rows: under a fully parallel suite run the search tool can exceed 30s of wall
// time and answer "timed out" instead of its contract, which would turn a load
// artefact into a false red (observed once, keyword miss row, 30.04s). The
// timeout is not what these cases assert.
const searchProbeToolTimeoutEnv = "GOPI_TOOL_TIMEOUT_SEC=180"

// searchProbe runs the real binary in dir with the isolated CLI-contract
// environment (no ambient GOPI_*/provider variables, an isolated GOPI_HOME), and
// returns its combined output plus the exit code. It deliberately does NOT fail
// on a non-zero exit: the exit code IS half of what these cases assert.
func searchProbe(t *testing.T, bin, home, dir string, args ...string) (string, int) {
	t.Helper()
	return searchProbeEnv(t, bin, home, dir, []string{searchProbeToolTimeoutEnv}, args...)
}

// searchProbeEnv is searchProbe with extra environment entries — the shape a
// case needs when the thing under test is a variable rather than an argument.
func searchProbeEnv(t *testing.T, bin, home, dir string, extra []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Dir = dir
	cmd.Env = append(cliContractEnv(home), extra...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), searchExitOK
	}
	var ee *exec.ExitError
	if !asExitError(err, &ee) {
		t.Fatalf("gopi %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), ee.ExitCode()
}
