package agent

// chat_policy_category_file_test.go — the TRACKED project policy file's three
// states, driven through the production resolver.
//
// The criterion this file pins (task 01a196bf, CONFIG.md carries the same row):
//
//	absent      → nil, nil: the next channel answers (GOPI.md's fence, then the
//	              built-in table).
//	malformed   → an error naming `file:line: what is wrong`.
//	well-formed → the table REPLACES the built-in one; nothing merges.
//
// Every arm builds a REAL repository on disk (a `.git` directory is what the walk
// looks for) rather than passing a path, because the question "which project does
// this console belong to" is answered by the walk and by nothing else.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
)

// writeProjectPolicyRepo builds a throwaway repository whose root carries the
// given `.gopi/chat-policy.md` body, and returns its root. An empty body writes
// NO file at all, which is the ABSENT state.
func writeProjectPolicyRepo(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if body == "" {
		return root
	}
	dir := filepath.Join(root, ChatPolicyProjectDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, ChatPolicyProjectFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return root
}

// TestProjectCategoryTableAbsentFallsBack is state ONE: a checkout with no policy
// file answers (nil, "", nil) so the caller moves to the next channel — and the
// built-in table it then uses is a real table, not an empty one.
func TestProjectCategoryTableAbsentFallsBack(t *testing.T) {
	root := writeProjectPolicyRepo(t, "")
	tab, path, err := LoadProjectCategoryTable(root)
	if err != nil {
		t.Fatalf("an absent project file must not be an error, got: %v", err)
	}
	if tab != nil || path != "" {
		t.Fatalf("an absent project file answered tab=%v path=%q, want (nil, \"\")", tab, path)
	}

	fallback, ferr := chatpolicy.DefaultCategories()
	if ferr != nil {
		t.Fatalf("the built-in fallback does not parse: %v", ferr)
	}
	if fallback.Len() == 0 {
		t.Fatal("the built-in fallback is EMPTY — with no project file and no GOPI.md fence the console " +
			"would judge every line as operator work, which is a widening nobody declared")
	}
}

// TestProjectCategoryTableReplacesTheBuiltInTable is state THREE, and it is the
// property the whole move exists for: the project's rows apply, and a row the
// BINARY ships does NOT.
//
// THE NEGATIVE HALF IS THE IMPORTANT ONE. A merge would mean a project that
// declared only `src/` still got `git push` as a ref move from the compiled-in
// table — reasonable there, but the same shape is what let `> cmd/` reach into a
// user's project and refuse their own source as a repository write. The file
// states the whole truth or it states nothing.
func TestProjectCategoryTableReplacesTheBuiltInTable(t *testing.T) {
	root := writeProjectPolicyRepo(t, "```chat-op-category\n"+
		"repo-write > src/\n"+
		"repo-write > lib/\n"+
		"```\n")
	tab, path, err := LoadProjectCategoryTable(root)
	if err != nil {
		t.Fatalf("LoadProjectCategoryTable: %v", err)
	}
	if tab == nil || !tab.Fenced {
		t.Fatalf("the project file parsed to %+v — expected a fenced table", tab)
	}
	if !strings.HasSuffix(path, filepath.Join(".gopi", "chat-policy.md")) {
		t.Errorf("the reported path is %q — a diagnostic must name the file the operator has to edit", path)
	}
	// The project's OWN rows are in the table …
	if rule, ok := tab.MatchCategory([]string{"echo", "x", ">", "src/x.go"}); !ok || rule.Category != chatpolicy.CategoryRepoWriteWord {
		t.Errorf("the project's own `> src/` row did not apply (matched=%v rule=%+v)", ok, rule)
	}
	// … and the built-in table's rows are NOT: this is REPLACEMENT, not a merge.
	builtin, berr := chatpolicy.DefaultCategories()
	if berr != nil {
		t.Fatalf("built-in: %v", berr)
	}
	for _, w := range builtin.WordsOf(chatpolicy.CategoryRepoWriteWord) {
		if _, ok := tab.MatchCategory(strings.Fields(w)); ok {
			continue // the project happens to declare this row too — not a merge
		}
	}
	if _, ok := tab.MatchCategory([]string{"git", "commit", "-m", "x"}); ok {
		t.Error("the BUILT-IN rows are still applied alongside the project's — the file must REPLACE the " +
			"table, not extend it")
	}
	if tab.Len() != 2 {
		t.Errorf("the parsed project table carries %d rows, want exactly the 2 written", tab.Len())
	}
}

// TestProjectCategoryTableReportsFileAndLine is state TWO, and the report is the
// product: a line the operator wrote is either obeyed or NAMED. The error must
// carry the file (so a reader knows what to open) and the line number (so a
// terminal can jump to it).
func TestProjectCategoryTableReportsFileAndLine(t *testing.T) {
	root := writeProjectPolicyRepo(t, "# a comment\n"+
		"```chat-op-category\n"+
		"repo-write > src/\n"+
		"ask git commit *\n"+
		"```\n")
	tab, path, err := LoadProjectCategoryTable(root)
	if err == nil {
		t.Fatalf("a category line naming a decision word (`ask`) parsed successfully (tab=%v)", tab)
	}
	if tab != nil {
		t.Errorf("a failing parse answered a table too (%v) — a caller could judge by a half-read table", tab)
	}
	if !strings.HasSuffix(path, filepath.Join(".gopi", "chat-policy.md")) {
		t.Errorf("the failure does not name the file: path=%q", path)
	}
	msg := err.Error()
	wantPrefix := path + ":4:"
	if !strings.HasPrefix(msg, wantPrefix) {
		t.Fatalf("the error is %q — a parse error must render `file:line: what is wrong` so the operator "+
			"is pointed at the line (want prefix %q)", msg, wantPrefix)
	}
	var perr *chatpolicy.ParseError
	if !asParseError(err, &perr) {
		t.Fatalf("the error is %T, not *chatpolicy.ParseError — a caller cannot read the line number", err)
	}
	if perr.Line != 4 {
		t.Errorf("the error names line %d, want 4 (the `ask` line)", perr.Line)
	}
}

// asParseError is errors.As without importing errors for one call site — kept
// explicit so the assertion above reads as "the TYPE is the parser's own".
func asParseError(err error, target **chatpolicy.ParseError) bool {
	for err != nil {
		if pe, ok := err.(*chatpolicy.ParseError); ok {
			*target = pe
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// TestProjectCategoryFileIsReadAtTheRepositoryRoot proves WHERE it is read from:
// a file one directory above the `.git` entry is not this project's, and a file
// in a SUBdirectory is not the root's. Both negatives are the difference between
// "the project's policy" and "some file that happens to be near".
func TestProjectCategoryFileIsReadAtTheRepositoryRoot(t *testing.T) {
	// A directory ABOVE the repository: its .gopi/chat-policy.md is not the
	// repository's own and must not be read.
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".gopi"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".gopi", "chat-policy.md"),
		[]byte("```chat-op-category\nrepo-write > outside/\n```\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	inner := filepath.Join(parent, "project")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir inner .git: %v", err)
	}
	tab, path, err := LoadProjectCategoryTable(inner)
	if err != nil {
		t.Fatalf("LoadProjectCategoryTable: %v", err)
	}
	if tab != nil || path != "" {
		t.Fatalf("a file ABOVE the repository was read (tab=%v path=%q) — the walk must stop at the "+
			"checkout root", tab, path)
	}

	// And from a SUBDIRECTORY of the repository, the root's file is found — that
	// is what makes the file govern the whole project rather than one directory.
	root := writeProjectPolicyRepo(t, "```chat-op-category\nrepo-write > src/\n```\n")
	deep := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir deep: %v", err)
	}
	tab, path, err = LoadProjectCategoryTable(deep)
	if err != nil {
		t.Fatalf("LoadProjectCategoryTable(deep): %v", err)
	}
	if tab == nil || tab.Len() != 1 {
		t.Fatalf("from %s the root's policy file answered %v (want the one-row table)", deep, tab)
	}
	if !strings.HasPrefix(path, root) {
		t.Errorf("the resolved path %q is not under the repository root %q", path, root)
	}

	// The same answer from the path helper, so a caller that only wants to PRINT
	// where the file would be cannot disagree with the reader.
	if got, found := ChatPolicyProjectPath(deep); !found || got != path {
		t.Errorf("ChatPolicyProjectPath(%s) = (%q, %v), want (%q, true)", deep, got, found, path)
	}
}

// TestProjectCategoryFileWithNoFenceIsAnEmptyTable: a file that exists and
// carries no fence parses to a VALID EMPTY table — the parser's own documented
// rule. The production caller treats it as "this channel declared nothing" and
// moves on (see chatFreezeChatPolicyState), which is the reading that does not
// widen the criterion by accident.
func TestProjectCategoryFileWithNoFenceIsAnEmptyTable(t *testing.T) {
	root := writeProjectPolicyRepo(t, "# no fence in this file at all\n")
	tab, path, err := LoadProjectCategoryTable(root)
	if err != nil {
		t.Fatalf("a file with no fence must not be a parse error: %v", err)
	}
	if tab == nil || tab.Fenced || tab.Len() != 0 {
		t.Fatalf("a fence-less file answered %+v — expected a valid EMPTY, unfenced table", tab)
	}
	if path == "" {
		t.Error("the path is empty — the caller cannot report WHICH file declared nothing")
	}
}

// TestThisRepositoryDeclaresItsOwnTables is the REGRESSION half on the real tree:
// gopi's own checkout carries the tracked file, it parses, and its rows are the
// repository's own trees. Without this, the suite above could pass on fixtures
// while the shipped file said something else (Rule 31: the surface that ships is
// the one that must be executed).
func TestThisRepositoryDeclaresItsOwnTables(t *testing.T) {
	repoRoot := repoRootForChatPolicyTest(t)
	tab, path, err := LoadProjectCategoryTable(repoRoot)
	if err != nil {
		t.Fatalf("this repository's tracked policy file does not parse: %v", err)
	}
	if tab == nil {
		t.Fatalf("no project policy table was found from %s — gopi's own trees would be judged by the "+
			"path-free fallback, so editing `internal/` would classify as OPERATOR WORK", repoRoot)
	}
	if want := filepath.Join(repoRoot, ChatPolicyProjectRelPath); path != want {
		t.Errorf("resolved %q, want %q", path, want)
	}
	if got := tab.WordsOf(chatpolicy.CategoryRepoWriteWord); len(got) == 0 {
		t.Fatal("the tracked file declares no repo-write row — nothing would be a repository write")
	}
	// The three trees THIS repository is built out of, as DIRECTORY PREFIXES.
	for _, tree := range []string{"internal/", "cmd/", "web/"} {
		found := false
		for _, w := range tab.WordsOf(chatpolicy.CategoryRepoWriteWord) {
			if strings.HasSuffix(w, " "+tree) || strings.HasSuffix(w, " "+"> "+tree) || strings.HasSuffix(w, " "+">> "+tree) || strings.HasSuffix(w, " "+tree) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the tracked file declares no row for the %q tree — this repository's own source "+
				"would fall through to operator work", tree)
		}
	}
	// And the fallback that answers WITHOUT the file names no path, which is what
	// makes the file necessary rather than decorative.
	fallback, ferr := chatpolicy.DefaultCategories()
	if ferr != nil {
		t.Fatalf("built-in: %v", ferr)
	}
	for _, w := range fallback.WordsOf(chatpolicy.CategoryRepoWriteWord) {
		for _, tree := range []string{"internal/", "cmd/", "web/", "src/", "lib/", "app/"} {
			if strings.HasSuffix(w, " "+tree) {
				t.Errorf("the BUILT-IN table names the tree %q (row %q) — a compiled-in table cannot know "+
					"a project's layout; the path rows belong in the tracked file", tree, w)
			}
		}
	}
}

// repoRootForChatPolicyTest walks up from the package directory to the checkout
// root — the directory holding `.git`, which is a DIRECTORY in a clone and a FILE
// in a linked worktree (the same two shapes the production walk accepts).
func repoRootForChatPolicyTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no .git found in any ancestor of %s", dir)
		}
		dir = parent
	}
}
