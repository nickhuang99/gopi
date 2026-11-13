package tools

// chat_op_project_table_redgreen_test.go — the RED/GREEN pair this task exists to
// land, driven through the REAL classifier and the REAL parser.
//
// THE DEFECT (measured, task 01a196bf). The category table used to be compiled in
// and every row of it named a tree of THIS repository (`internal/`, `cmd/`,
// `web/`). A console opened in a project whose source lives under `src/` matched
// not one row, so a write into that project's own source classified as OPERATOR
// WORK — the class that RUNS. The vocabulary was right; the POPULATION was wrong.
//
// THE PAIR, and both halves must be observed or neither is evidence:
//
//	RED    a checkout shaped `src/lib`, judged by the SHIPPED table alone, does
//	       NOT call a write into `src/lib/x.go` a repository write.
//	GREEN  the same checkout with a tracked `.gopi/chat-policy.md` declaring
//	       `repo-write > src/` DOES — because the project said so, not because a
//	       binary guessed its layout.
//
// THE TABLE IS NEVER TYPED HERE on the green side: it is parsed by
// chatpolicy.ParseCategories from the file the fixture wrote, so the test cannot
// pass while the file says something else.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
)

// redGreenProjectPolicyFile is the tracked file the fixture repository carries.
// It declares ONE tree — `src/`, a name gopi's own repository never uses — which
// is what makes the green half a statement about the PROJECT rather than about
// the binary.
const redGreenProjectPolicyFile = "# the project's own table\n" +
	"```" + "chat-op-category\n" +
	"repo-write > src/\n" +
	"```\n"

// redGreenProjectWrite is the command line the pair judges: a write into the
// fixture project's own source, spelled the way an operator writes it.
const redGreenProjectWrite = "echo x > src/lib/x.go"

// writeRedGreenProject materialises a checkout shaped like a NON-gopi project:
// a `.git` entry at the root, a `src/lib` tree, and (optionally) the tracked
// `.gopi/chat-policy.md`.
func writeRedGreenProject(t *testing.T, policy string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "lib"), 0o755); err != nil {
		t.Fatalf("mkdir src/lib: %v", err)
	}
	if policy == "" {
		return root
	}
	if err := os.MkdirAll(filepath.Join(root, chatOpProjectPolicyRelPathDir()), 0o755); err != nil {
		t.Fatalf("mkdir .gopi: %v", err)
	}
	path := filepath.Join(root, chatOpProjectPolicyRelPathDir(), chatOpProjectPolicyRelPathBase())
	if err := os.WriteFile(path, []byte(policy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return root
}

// chatOpProjectPolicyRelPathDir / Base spell the tracked file's two components
// from the ONE value this package already carries for it.
func chatOpProjectPolicyRelPathDir() string  { return filepath.Dir(chatOpProjectPolicyRelPath) }
func chatOpProjectPolicyRelPathBase() string { return filepath.Base(chatOpProjectPolicyRelPath) }

// TestRedProjectWithoutItsPolicyFileCannotSeeItsOwnSource is the RED half, and
// it must RUN the shipped table rather than describe it: with no policy file the
// classifier falls back to the built-in rows, and a write into `src/lib/` is
// therefore OPERATOR WORK — the exact defect, reproduced from the fixture.
func TestRedProjectWithoutItsPolicyFileCannotSeeItsOwnSource(t *testing.T) {
	root := writeRedGreenProject(t, "")
	ResetChatOpCategoriesToDefaults()
	t.Cleanup(ResetChatOpCategoriesToDefaults)

	if got := ChatOpClassifyIn(redGreenProjectWrite, root); got != ChatOpClassOperatorWork {
		t.Fatalf("RED half is not red: %q classified as %s under the shipped table — either the "+
			"built-in table has grown a path row (the defect reinstated) or this fixture stopped "+
			"describing a project with a `src/` tree", redGreenProjectWrite, got)
	}
	// The same line WITH the project's own table, as a control on the fixture:
	// the command is a write the grammar can express, so the red above is about
	// the POPULATION and not about a line no rule could ever match.
	tab, perr := chatpolicy.ParseCategories("fixture", redGreenProjectPolicyFile)
	if perr != nil {
		t.Fatalf("the fixture policy does not parse: %v", perr)
	}
	SetChatOpCategories(tab)
	if got := ChatOpClassifyIn(redGreenProjectWrite, root); got != ChatOpClassRepoWrite {
		t.Fatalf("control failed: with `repo-write > src/` frozen, %q classified as %s, want %s — "+
			"the RED above would then prove nothing about the table", redGreenProjectWrite, got, ChatOpClassRepoWrite)
	}
}

// TestGreenProjectPolicyFileIsWhatMakesItsOwnSourceARepositoryWrite is the GREEN
// half on the SAME fixture shape, with the tracked file present. The table is
// parsed from that file with the production parser and then frozen through the
// production setter, so the judgement below is the one a console would make.
func TestGreenProjectPolicyFileIsWhatMakesItsOwnSourceARepositoryWrite(t *testing.T) {
	root := writeRedGreenProject(t, redGreenProjectPolicyFile)
	ResetChatOpCategoriesToDefaults()
	t.Cleanup(ResetChatOpCategoriesToDefaults)

	path := filepath.Join(root, chatOpProjectPolicyRelPathDir(), chatOpProjectPolicyRelPathBase())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the tracked file back: %v", err)
	}
	tab, perr := chatpolicy.ParseCategories(path, string(data))
	if perr != nil {
		t.Fatalf("the tracked project file does not parse: %v", perr)
	}
	if !tab.Fenced || tab.Len() != 1 {
		t.Fatalf("the parsed project table is fenced=%v with %d rows, want one row", tab.Fenced, tab.Len())
	}
	SetChatOpCategories(tab)

	if got := ChatOpClassifyIn(redGreenProjectWrite, root); got != ChatOpClassRepoWrite {
		t.Fatalf("GREEN half is not green: %q classified as %s, want %s — the project declared "+
			"`repo-write > src/` and the classifier did not apply it", redGreenProjectWrite, got, ChatOpClassRepoWrite)
	}

	// REPLACEMENT, NOT MERGE: a row the BINARY ships does not reach this project.
	// `git commit` is in the built-in table and in NO row of this fixture's file,
	// so under a merge it would still be a repository write — and that shape is
	// exactly how `> cmd/` used to refuse somebody else's source.
	if _, ok := tab.MatchCategory([]string{"git", "commit", "-m", "x"}); ok {
		t.Error("the shipped `git commit` row matched inside the project's table — the file must REPLACE " +
			"the built-in table, never extend it")
	}
}
