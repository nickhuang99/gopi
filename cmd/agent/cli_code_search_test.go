package main

import (
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestParseCodeSearchArgs covers the pure arg-parsing front-end of the
// code-search subcommand. Purity matters: parseCodeSearchArgs must not call
// os.Exit or mutate the environment, so it is directly unit-testable.
func TestParseCodeSearchArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantSym string
		wantErr string // substring; "" = expect success
	}{
		{"basic symbol", []string{"NewAgent"}, "NewAgent", ""},
		{"symbol with --root", []string{"codeSearchScope", "--root", "internal/agent"}, "codeSearchScope", ""},
		{"symbol with --root=form", []string{"NewClient", "--root=internal/tools"}, "NewClient", ""},
		{"trims spaces", []string{"  NewAgent  "}, "NewAgent", ""},
		{"missing symbol", []string{}, "", "missing <symbol>"},
		{"empty symbol", []string{"   "}, "", "missing <symbol>"},
		{"two symbols", []string{"NewAgent", "NewClient"}, "", "unexpected extra argument"},
		{"unknown flag", []string{"NewAgent", "--limit", "5"}, "", "unknown flag --limit"},
		{"default root", []string{"Foo"}, "Foo", ""}, // root defaults to "."
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, err := parseCodeSearchArgs(c.args)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("parseCodeSearchArgs(%v) = nil error, want %q", c.args, c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("parseCodeSearchArgs(%v) error %q does not contain %q", c.args, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCodeSearchArgs(%v) unexpected error: %v", c.args, err)
			}
			if opts.symbol != c.wantSym {
				t.Errorf("parseCodeSearchArgs(%v).symbol = %q, want %q", c.args, opts.symbol, c.wantSym)
			}
		})
	}
}

// TestCodeSearchDefaultRootInParse verifies that --root omitted resolves to
// ".", and that an explicit --root is honored.
func TestCodeSearchDefaultRootInParse(t *testing.T) {
	opts, err := parseCodeSearchArgs([]string{"Foo"})
	if err != nil {
		t.Fatalf("parseCodeSearchArgs([Foo]) error: %v", err)
	}
	if opts.root != "." {
		t.Errorf("default root = %q, want %q", opts.root, ".")
	}
	opts, err = parseCodeSearchArgs([]string{"Foo", "--root", "internal/"})
	if err != nil {
		t.Fatalf("parseCodeSearchArgs with --root error: %v", err)
	}
	if opts.root != "internal/" {
		t.Errorf("--root root = %q, want %q", opts.root, "internal/")
	}
}

// TestIsCodeSearchSubcommand ensures the dispatcher recognizes the
// code-search subcommand and ignores sibling subcommands. The true-dispatch
// positive case is exercised by the real-binary integration run (running it
// here would execute a live CodeSearch against the test's cwd); only the
// non-dispatching sibling checks are unit-testable without side effects.
func TestIsCodeSearchSubcommand(t *testing.T) {
	// Every sibling is the CONSTANT that owns its word (Rule 1/13/14): the
	// group noun `memory` (the knowledge door `memory_search` became), the
	// session family's reader, and the two tool-named spellings.
	for _, sibling := range []string{toolnames.ToolWebSearch, CliVerbMemory, CliVerbLog, toolnames.ToolText2img, toolnames.ToolVisionAnalyze} {
		if isCodeSearchSubcommand([]string{sibling, "NewAgent"}) {
			t.Errorf("isCodeSearchSubcommand([%s]) = true, want false", sibling)
		}
	}
	if isCodeSearchSubcommand(nil) {
		t.Error("isCodeSearchSubcommand(nil) = true, want false")
	}
}

// TestCodeSearchUsageShowsHelp guards the subcommand's own -h: the usage must
// document the <symbol> positional, the --root flag, and the exit codes so a
// script can rely on them.
func TestCodeSearchUsageShowsHelp(t *testing.T) {
	for _, want := range []string{
		"gopi tool code-search <symbol>", // primary usage
		"--root DIR",                     // the root flag
		".",                              // default root
		"no symbol found: <symbol>",      // the miss message (exit 1)
		"Exit status:",                   // exit-code contract for scripts
		"ctags",                          // backend documented
		searchGroundLabelLead,            // the refusal row (exit searchExitWrongGround)
		"keyword_search",                 // the sibling verb sharing that code
	} {
		if !strings.Contains(codeSearchUsageText(), want) {
			t.Errorf("the rendered code-search usage page is missing %q", want)
		}
	}
}

// TestCodeSearchBackendFindsDefinition is an integration test of the code_search
// backend the CLI dispatches to: a real symbol in this repo must come back with
// a file:line plus a signature (the core deliverable — greppable output). ctags
// is a real dependency; if it is missing the test reports that explicitly rather
// than pretending the backend works.
func TestCodeSearchBackendFindsDefinition(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	root := repoRoot(t)
	scope := tools.SearchScope{
		RootPath:    root,
		IncludeGlob: tools.CodeSearchGlobs,
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   50,
	}
	hits, _, _, err := tools.CodeSearch(scope, "CodeSearch")
	if err != nil {
		if strings.Contains(err.Error(), "ctags") || strings.Contains(err.Error(), "exec") {
			t.Skipf("ctags not available in this environment: %v", err)
		}
		t.Fatalf("CodeSearch(CodeSearch) error: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("CodeSearch(CodeSearch) returned no hits for a symbol that definitely exists")
	}
	found := false
	for _, h := range hits {
		if h.FilePath == "internal/tools/search.go" && h.SymbolName == "CodeSearch" {
			found = true
			if h.LineNo <= 0 {
				t.Errorf("CodeSearch hit has LineNo=%d, want > 0 (file:line contract)", h.LineNo)
			}
			if h.Signature == "" {
				t.Error("CodeSearch hit has empty Signature")
			}
			break
		}
	}
	if !found {
		t.Errorf("did not find CodeSearch in internal/tools/search.go; hits: %+v", hits)
	}
}

// TestCodeSearchBackendNoSymbol is the negative case: a symbol that exists
// nowhere must produce zero hits (the CLI renders "no symbol found" + exit 1).
func TestCodeSearchBackendNoSymbol(t *testing.T) {
	requireCtags(t)
	root := repoRoot(t)
	scope := tools.SearchScope{
		RootPath:    root,
		IncludeGlob: tools.CodeSearchGlobs,
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   50,
	}
	hits, _, _, err := tools.CodeSearch(scope, "ZZZDefinitelyNoSuchSymbol"+uniqueSuffix(t))
	if err != nil {
		t.Fatalf("CodeSearch(no-symbol) error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("CodeSearch(no-symbol) = %d hits, want 0: %+v", len(hits), hits)
	}
}

// uniqueSuffix returns an alphanumeric string from the current time, so the
// negative test's symbol truly exists nowhere (a literal would be found by the
// ripgrep fallback as a "reference" in this same test file).
func uniqueSuffix(t *testing.T) string {
	t.Helper()
	return fmt.Sprint(time.Now().UnixNano() % 9999999999)
}

// repoRoot walks up from the test's cwd (the package dir) to the directory
// containing go.mod — the repo root the code_search integration tests anchor on.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod) walking up from cwd")
		}
		dir = parent
	}
}

// TestTopLevelUsageShowsCodeSearch guards against regressing "the user has no
// idea how to use the code-search subcommand": gopi -h must advertise it, and
// the parameter/miss detail must live in the command's own -h.
func TestTopLevelUsageShowsCodeSearch(t *testing.T) {
	for _, want := range []string{
		"gopi tool code-search <symbol>", // subcommand advertised in the index
		"DEFINED",                        // what it answers
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level gopi -h usage is missing %q", want)
		}
	}
	for _, want := range []string{
		"--root DIR",                // parameter surface
		"no symbol found: <symbol>", // miss behavior documented
		searchGroundLabelLead,       // the refusal row, advertised where the codes are
	} {
		if !strings.Contains(codeSearchUsageText(), want) {
			t.Errorf("gopi tool code-search -h is missing %q", want)
		}
	}
}
