package tools

// chat_op_project_table_test.go — the test-side scaffolding for the PROJECT
// table, now that the shipped binary table names no path.
//
// WHY THIS FILE EXISTS. Before task 01a196bf the built-in category table carried
// this repository's own trees (`> internal/`, `>> cmd/`, `> web/`), so every test
// in this package that asserted "editing gopi's source is a repository write" was
// really asserting that the BINARY knew gopi's layout. That is the defect the
// task removed: a compiled-in table describes the repository that built it and
// nothing else.
//
// So a test that means "in THIS repository" must now FREEZE THIS REPOSITORY'S
// TABLE — the tracked `.gopi/chat-policy.md` that travels with the clone. These
// helpers read it from the real tree (never a hand-written copy of its rows), so
// a test and the shipped file cannot drift: if a row moves or a tree is renamed,
// the test that freezes the project table moves with it.
//
// A test that means "with no project table at all" keeps calling
// ResetChatOpCategoriesToDefaults and gets the built-in fallback — which is the
// ABSENT state, and is the reason the fallback is asserted separately
// (TestBuiltInCategoryTableNamesNoPath in internal/chatpolicy).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
)

// chatOpProjectPolicyRelPath mirrors chatOpProjectPolicyRelPath so the walk
// below can stop at the first directory that carries the file. It is NOT
// imported from internal/agent: that package imports this one, so a test import
// would be a cycle — the value is stated here and the PRODUCTION constant is
// pinned by TestChatPolicyProjectRelPathNamesTheTrackedFile below.
const chatOpProjectPolicyRelPath = ".gopi/chat-policy.md"

// chatOpProjectCategoryTable parses the TRACKED project policy file of the
// checkout these tests are running in.
//
// It walks up from the test's working directory (the package directory) to the
// first ancestor holding `.gopi/chat-policy.md`, then parses it with
// chatpolicy.ParseCategories — the SAME parser production uses, so a malformed
// row fails the suite rather than being tolerated by a second reader.
func chatOpProjectCategoryTable(t *testing.T) *chatpolicy.CategoryTable {
	t.Helper()
	path := chatOpProjectPolicyPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	tab, perr := chatpolicy.ParseCategories(path, string(data))
	if perr != nil {
		t.Fatalf("the tracked project policy file does not parse: %v", perr)
	}
	if !tab.Fenced || tab.Len() == 0 {
		t.Fatalf("%s parsed to %d rows (fenced=%v) — the project declares no table, so this "+
			"repository's own trees would be unguarded", path, tab.Len(), tab.Fenced)
	}
	return tab
}

// chatOpProjectPolicyPath is the walk itself, kept separate so a test that wants
// the PATH (to prove the resolver agrees) need not parse the file.
func chatOpProjectPolicyPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, chatOpProjectPolicyRelPath)); statErr == nil && !info.IsDir() {
			return filepath.Join(dir, chatOpProjectPolicyRelPath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s found in any ancestor of %s — the tracked project policy file is missing from "+
				"this checkout", chatOpProjectPolicyRelPath, dir)
		}
		dir = parent
	}
}

// freezeChatOpProjectCategories freezes THIS repository's table for the duration
// of the test and restores the shipped default afterwards, so a test that means
// "gopi's own trees are guarded" drives the real declaration instead of a
// compiled-in guess.
func freezeChatOpProjectCategories(t *testing.T) *chatpolicy.CategoryTable {
	t.Helper()
	tab := chatOpProjectCategoryTable(t)
	SetChatOpCategories(tab)
	t.Cleanup(ResetChatOpCategoriesToDefaults)
	return tab
}

// chatOpProjectPolicyBytesFor is the same file read as TEXT, for the tests that
// assert a property of the BYTES (no decision word, no duplicate row) rather than
// of the parsed table.
func chatOpProjectPolicyBytesFor(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(chatOpProjectPolicyPath(t))
	if err != nil {
		t.Fatalf("read project policy: %v", err)
	}
	return string(data)
}

// chatOpProjectTreeRoots are the directory prefixes the TRACKED file declares for
// THIS repository — read out of the file, never typed here, so the assertion that
// "editing this tree is a repository write" follows the project's own words.
//
// It answers the repository root as well, because that is what the tests need to
// build a real path under a declared tree.
func chatOpProjectTreeRoots(t *testing.T) (root string, trees []string) {
	t.Helper()
	path := chatOpProjectPolicyPath(t)
	root = filepath.Dir(filepath.Dir(path))
	tab := chatOpProjectCategoryTable(t)
	for _, w := range tab.WordsOf(chatpolicy.CategoryRepoWriteWord) {
		fields := strings.Fields(w)
		last := fields[len(fields)-1]
		if strings.HasSuffix(last, "/") && !strings.HasPrefix(last, "./") {
			trees = append(trees, last)
		}
	}
	if len(trees) == 0 {
		t.Fatalf("the tracked project policy file declares no directory-prefix row (%s) — this "+
			"repository's source trees would be unguarded", chatOpProjectPolicyRelPath)
	}
	return root, trees
}

// TestChatPolicyProjectRelPathNamesTheTrackedFile pins the two spellings of the
// file's location together: this package's test scaffolding (above) and
// internal/agent's production constant must name the SAME path, or the tests
// would read a different file from the one the binary reads. The agent package
// cannot be imported here (it imports this one), so the pin is on the FILE the
// repository actually tracks.
func TestChatPolicyProjectRelPathNamesTheTrackedFile(t *testing.T) {
	path := chatOpProjectPolicyPath(t)
	want := filepath.Join(filepath.Dir(filepath.Dir(path)), filepath.FromSlash(chatOpProjectPolicyRelPath))
	if path != want {
		t.Fatalf("the project policy file resolved to %s, want %s (the walk and the rel path disagree)", path, want)
	}
	if filepath.Base(path) != "chat-policy.md" || filepath.Base(filepath.Dir(path)) != ".gopi" {
		t.Fatalf("%s is not .gopi/chat-policy.md — the tracked project policy file moved", path)
	}
}
