package main

// cli_keyword_search_path_arg_test.go — the contract for the THIRD reading of a
// zero-hit `gopi tool keyword_search` run.
//
// THE DEFECT THIS PINS (measured on the real binary, 2026-11-08):
//
//	$ gopi tool keyword_search "needle" internal/config
//	No matches.                       # exit 1
//
// Every positional word is joined into ONE query — on purpose, because
// `gopi tool keyword_search two words` is a supported two-word query — so the
// path became part of the search TEXT. The command therefore always printed the
// miss line with the miss code, byte-identical to a search of a real tree that
// genuinely holds no `needle`. The caller could not tell "your argument shape
// was wrong" from "the tree does not have it".
//
// THE SPLIT THIS ASSERTS (the acceptance, in one arm):
//
//	same query, same tree, only the argument shape differs:
//	  keyword_search <needle> --root sub   → exit 0, hits         (the remedy WORKS)
//	  keyword_search <needle> sub          → exit 2, "No matches." + a named hint
//	  keyword_search <needle-that-is-nowhere>  → exit 1, plain miss, NO hint
//	  keyword_search <word> <word>         → exit 0, ONE two-word query (kept)
//
// Three layers, the same shape as its siblings: the pure predicate, the
// constants contract (the -h code table documents the code the process exits
// with), and the live binary driven with real fixtures.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// keywordSearchArgVerb is the command word this file drives. It is the CONSTANT
// that owns it (Rules 1/13/14), the same spelling its sibling suites use: the
// probe helpers translate this family head into its group address, so naming
// the leaf here is naming the command a caller actually types.
const keywordSearchArgVerb = toolnames.ToolKeywordSearch

// keywordSearchPathArgNeedle is the query token this file's fixtures contain.
// It is written into a file, never into this package's own sources, so a hit is
// only ever a hit against the fixture.
const keywordSearchPathArgNeedle = "PATHARG_NEEDLE_ZZZ"

// keywordSearchPathArgFixture is the searchable directory the caller was
// probably aiming at: one file holding the needle and one two-word phrase.
const keywordSearchPathArgFixture = "fixture.txt"

// keywordSearchPathArgFixtureBody is the fixture's content — the needle on
// line 1, and a two-word phrase on line 2 (the "several words are ONE query"
// capability the fix must not break).
const keywordSearchPathArgFixtureBody = keywordSearchPathArgNeedle + " lives here\nalpha beta pair\n"

// keywordSearchPathArgDirName is the relative directory name the caller passes
// positionally in the failing shape (and with --root in the working one). Its
// being a REAL directory is the whole premise of the hint.
const keywordSearchPathArgDirName = "sub"

// TestKeywordSearchPathArg pins the pure predicate: fewer than two words is
// never a `<query> <path>` mistake (one word is the query, whatever it names on
// disk), a word that does not exist is no evidence at all, and the first
// EXISTING path among the words is the one the hint names.
func TestKeywordSearchPathArg(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	real := keywordSearchPathFinding(t, cwd)
	if real == "" {
		t.Fatal("no directory at the package root — the fixture premise of this test is gone")
	}
	for _, tc := range []struct {
		name  string
		words []string
		want  string
	}{
		{name: "one word is the query, even when it names a directory", words: []string{real}, want: ""},
		{name: "query then a real path", words: []string{"zzzNoSuchQuery", real}, want: real},
		{name: "path then query", words: []string{real, "zzzNoSuchQuery"}, want: real},
		{name: "two words, neither on disk", words: []string{"zzzNoSuchQuery", "zzzAlsoNothing"}, want: ""},
		{name: "a flag-shaped word is never the path", words: []string{"needle", "--" + searchRootFlag, real}, want: real},
		{name: "empty pack", words: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := keywordSearchPathArg(tc.words); got != tc.want {
				t.Errorf("keywordSearchPathArg(%q) = %q, want %q", tc.words, got, tc.want)
			}
		})
	}
}

// keywordSearchPathFinding returns the first entry of dir that is a directory,
// so the predicate rows use a path that EXISTS on the machine running them
// without naming one (a hardcoded path would make the test host-dependent).
func keywordSearchPathFinding(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return e.Name()
		}
	}
	return ""
}

// TestKeywordSearchExitPathArgIsDistinctAndDocumented is the constants half: the
// new reading must not collide with success, with the miss, or with the
// wrong-ground refusal (a caller branching on the exit status must be able to
// tell all four apart), and the -h page must document the code the process
// actually exits with (Rule 13 — a page that drifted from the code path is a
// defect of the same cost as a wrong code).
func TestKeywordSearchExitPathArgIsDistinctAndDocumented(t *testing.T) {
	for _, other := range []struct {
		name string
		code int
	}{
		{name: "searchExitOK", code: searchExitOK},
		{name: "searchExitNoHits", code: searchExitNoHits},
		{name: "searchExitWrongGround", code: searchExitWrongGround},
	} {
		if keywordSearchExitPathArg == other.code {
			t.Fatalf("keywordSearchExitPathArg == %s (%d): the hint would be indistinguishable from that reading — which is the defect it removes",
				other.name, other.code)
		}
	}
	usage := keywordSearchUsageText()
	if !strings.Contains(usage, "\n  "+strconv.Itoa(keywordSearchExitPathArg)) {
		t.Errorf("the -h page has no exit-status row for code %d — the code table and the code path must agree", keywordSearchExitPathArg)
	}
	if !strings.Contains(usage, keywordSearchPathArgLabel) {
		t.Errorf("the -h page does not carry the hint's own label %q — a reader who greps the page and one who greps stderr must find the same words",
			keywordSearchPathArgLabel)
	}
	// The hint's remedy must be the FULL working command: the flat leaf word
	// alone is refused by name, so a remedy spelled that way is a dead end.
	hint := keywordSearchPathArgHint(keywordSearchPathArgDirName, []string{"needle", keywordSearchPathArgDirName})
	if want := CliVerbTool + " " + keywordSearchArgVerb; !strings.Contains(hint, want) {
		t.Errorf("the hint does not spell the working command %q:\n%s", want, hint)
	}
	if !strings.Contains(hint, "--"+searchRootFlag+" "+keywordSearchPathArgDirName) {
		t.Errorf("the hint does not carry the exact --%s remedy:\n%s", searchRootFlag, hint)
	}
	// The remedy must not REPEAT the mistake: the path word has to be out of the
	// query it prescribes, or the command a caller pastes finds nothing. Measured
	// on the first cut, which printed `keyword_search "needle sub" --root sub`.
	remedy := keywordSearchPathArgQueryFor([]string{"needle", keywordSearchPathArgDirName}, keywordSearchPathArgDirName)
	if remedy != "needle" {
		t.Errorf("the repaired query is %q, want %q — the path word must be taken OUT of it, not carried along", remedy, "needle")
	}
	if strings.Contains(hint, keywordSearchPathArgDirName+"\" --"+searchRootFlag) {
		t.Errorf("the hint prescribes a query that still contains the path:\n%s", hint)
	}
}

// TestKeywordSearchPathArgumentOnTheRealBinary is the ACCEPTANCE arm: the same
// query against the same tree, with only the argument SHAPE differing, must
// produce four distinguishable answers. Before the fix the first two rows were
// byte-identical ("No matches." + exit 1).
func TestKeywordSearchPathArgumentOnTheRealBinary(t *testing.T) {
	requireRipgrep(t)
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	// cwd is where the child STANDS: the positional path is resolved relative to
	// it, so the fixture has to live there for the shape to be reproducible.
	cwd := t.TempDir()
	sub := filepath.Join(cwd, keywordSearchPathArgDirName)
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, keywordSearchPathArgFixture), []byte(keywordSearchPathArgFixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// The token that is nowhere: built at run time so no file on disk contains
	// it (a literal spelled here would be found by the search of this very
	// package's tree — the same trap its sibling file records).
	nowhere := "zzzNoSuchToken" + uniqueSuffix(t)

	t.Run("argument shape: hits, and the remedy WORKS", func(t *testing.T) {
		out, code := searchProbe(t, bin, home, cwd, keywordSearchArgVerb, keywordSearchPathArgNeedle, "--"+searchRootFlag, keywordSearchPathArgDirName)
		if code != searchExitOK {
			t.Fatalf("exit = %d, want %d — `--root %s` is the remedy the hint prescribes and it must find the fixture\n%s",
				code, searchExitOK, keywordSearchPathArgDirName, out)
		}
		if !strings.Contains(out, keywordSearchPathArgFixture+":1:") {
			t.Errorf("the hit output is not the tool's `file:line: snippet` shape\n%s", out)
		}
		if strings.Contains(out, keywordSearchPathArgLabel) {
			t.Errorf("a run that MADE HITS printed the path-argument hint — a hint that fires on healthy calls trains its reader to ignore it\n%s", out)
		}
	})

	t.Run("path beside the query: a NAMED reading, not the plain miss", func(t *testing.T) {
		out, code := searchProbe(t, bin, home, cwd, keywordSearchArgVerb, keywordSearchPathArgNeedle, keywordSearchPathArgDirName)
		if code != keywordSearchExitPathArg {
			t.Fatalf("exit = %d, want %d (the path-argument reading) — this row is the defect: it used to be %d (the plain miss), which is a claim about a tree that was never asked anything\n%s",
				code, keywordSearchExitPathArg, searchExitNoHits, out)
		}
		if !strings.Contains(out, "No matches.") {
			t.Errorf("the miss line disappeared — the search DID run, so its verdict belongs in the output\n%s", out)
		}
		for _, want := range []string{keywordSearchPathArgLabel, keywordSearchPathArgDirName, "--" + searchRootFlag} {
			if !strings.Contains(out, want) {
				t.Errorf("the hint does not name %q — the caller cannot act on a message that does not name its own cause\n%s", want, out)
			}
		}
	})

	t.Run("plain miss: unchanged, and NO hint", func(t *testing.T) {
		out, code := searchProbe(t, bin, home, cwd, keywordSearchArgVerb, nowhere)
		if code != searchExitNoHits {
			t.Fatalf("exit = %d, want %d — a genuine miss keeps its code (this fix must not renumber it)\n%s", code, searchExitNoHits, out)
		}
		if !strings.Contains(out, "No matches.") {
			t.Errorf("the miss message changed shape\n%s", out)
		}
		if strings.Contains(out, keywordSearchPathArgLabel) {
			t.Errorf("a one-word miss printed the path-argument hint — one word IS the query, whatever it names on disk\n%s", out)
		}
	})

	t.Run("two positional words are still ONE query (the capability is kept)", func(t *testing.T) {
		out, code := searchProbe(t, bin, home, cwd, keywordSearchArgVerb, "alpha", "beta")
		if code != searchExitOK {
			t.Fatalf("exit = %d, want %d — `keyword_search alpha beta` must stay a single two-word query, not become a usage error\n%s",
				code, searchExitOK, out)
		}
		if !strings.Contains(out, "alpha beta") {
			t.Errorf("the two words were not joined into one query\n%s", out)
		}
	})
}
