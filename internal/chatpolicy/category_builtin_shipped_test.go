package chatpolicy

// category_builtin_shipped_test.go — the property that made the move necessary.
//
// The built-in table exists for a project that declares no table of its own, and
// a table that ships inside a BINARY cannot know what any project calls its
// source. It used to try: fourteen rows naming `internal/`, `cmd/` and `web/` —
// this repository's layout, correct here and meaningless everywhere else, where
// `sed -i` on a project's own source classified as OPERATOR WORK and ran
// (measured, task 01a196bf). The paths now live in the TRACKED project file
// (`.gopi/chat-policy.md`), and this test fails if one comes back.
//
// THE TWO HALVES. (a) No row of the built-in table names a directory prefix, any
// project's (`internal/`, `cmd/`, `web/`) — the paths that were removed — and no
// common user-project tree either (`src/`, `lib/`, `app/`), because "add the
// other layouts to the binary" is the same defect with a longer list. (b) The
// built-in table still declares both classes it must, so the fallback is a real
// table and not an emptied one that would classify everything as operator work.

import (
	"strings"
	"testing"
)

// builtInCategoryForbiddenPathWords are the directory-prefix spellings a compiled
// in table must never carry: the three this repository is built from (the defect)
// and the three a user's project is most likely built from (the "fix" that would
// reintroduce it in a wider form).
var builtInCategoryForbiddenPathWords = []string{
	"internal/", "cmd/", "web/", "src/", "lib/", "app/", "./internal/", "./cmd/", "./src/",
}

// TestBuiltInCategoryTableNamesNoPath is (a): the shipped fallback carries no
// directory prefix at all.
func TestBuiltInCategoryTableNamesNoPath(t *testing.T) {
	tab, err := DefaultCategories()
	if err != nil {
		t.Fatalf("the built-in table does not parse: %v", err)
	}
	for _, rule := range tab.Rules {
		for _, w := range rule.Pattern {
			if !strings.HasSuffix(w, "/") {
				continue
			}
			t.Errorf("the BUILT-IN table carries the directory-prefix word %q (row %q) — a table compiled "+
				"into the binary cannot know a project's layout; the path rows belong in the tracked "+
				"project file", w, rule.PatternString())
		}
		for _, forbidden := range builtInCategoryForbiddenPathWords {
			if strings.Contains(rule.PatternString(), forbidden) {
				t.Errorf("the BUILT-IN table names the tree %q (row %q)", forbidden, rule.PatternString())
			}
		}
	}
	// The bytes, not only the parsed rows: a comment that re-introduced a path as
	// a worked example would be the same defect one round trip later, because the
	// next editor copies what the comment shows.
	fence := strings.TrimSuffix(strings.TrimPrefix(DefaultCategoryText(), fenceOpenPrefix), fenceOpenPrefix)
	for _, line := range strings.Split(fence, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, commentPrefix) {
			continue
		}
		for _, forbidden := range builtInCategoryForbiddenPathWords {
			if strings.Contains(trimmed, " "+forbidden) || strings.HasPrefix(trimmed, commentPrefix+" "+forbidden) {
				t.Errorf("the built-in table's COMMENT names the tree %q (%q) — the next editor copies what "+
					"the comment shows", forbidden, trimmed)
			}
		}
	}
}

// TestBuiltInCategoryTableStillAnswersBothClasses is (b): shrinking the table
// must not have emptied it. The three classes that need rows — repo-write,
// ref-move, long — each keep their verb families, and operator work stays a
// DEFAULT that names no word.
func TestBuiltInCategoryTableStillAnswersBothClasses(t *testing.T) {
	tab, err := DefaultCategories()
	if err != nil {
		t.Fatalf("the built-in table does not parse: %v", err)
	}
	for _, class := range []string{CategoryRepoWriteWord, CategoryRefMoveWord, CategoryLongWord} {
		if len(tab.WordsOf(class)) == 0 {
			t.Errorf("the built-in table declares no %q row — with no project file that class would match "+
				"nothing, which is a widening nobody declared", class)
		}
	}
	if w := tab.WordsOf(CategoryOperatorWorkWord); len(w) != 0 {
		t.Errorf("the built-in table declares %d %q row(s) (%v) — the default class must stay a default",
			len(w), CategoryOperatorWorkWord, w)
	}
	// The git verbs whose effect is a repository act hold in EVERY checkout,
	// whatever its trees are called — they are what is left, on purpose.
	for _, want := range []string{"git add *", "git commit *", "git push *"} {
		found := false
		for _, w := range append(tab.WordsOf(CategoryRepoWriteWord), tab.WordsOf(CategoryRefMoveWord)...) {
			if w == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the built-in table no longer declares %q — that row is true of every repository "+
				"and is the part the fallback keeps", want)
		}
	}
}

// TestBuiltInCategoryTableClassifiesTheGitActsEveryRepoShares drives the compiled
// in table's own rows through the matcher: a stage and a commit are repository
// writes, a force push is a ref move — the vocabulary that survives without a
// project file. It is the positive half of the test above (which only counts
// rows), so a table whose rows no longer MATCH cannot pass by being non-empty.
func TestBuiltInCategoryTableClassifiesTheGitActsEveryRepoShares(t *testing.T) {
	tab, err := DefaultCategories()
	if err != nil {
		t.Fatalf("built-in: %v", err)
	}
	cases := []struct {
		words []string
		want  string
	}{
		{[]string{"git", "add", "x.go"}, CategoryRepoWriteWord},
		{[]string{"git", "commit", "-m", "x"}, CategoryRepoWriteWord},
		{[]string{"gofmt", "-w", "x.go"}, CategoryRepoWriteWord},
		{[]string{"sed", "-i", "s/a/b/", "x.go"}, CategoryRepoWriteWord},
		// EVERY PUSH SPELLING IS A REPOSITORY WRITE (operator's ruling,
		// 2026-11-03: "push 都可以"). The whole family used to be ref-move, so
		// publishing a commit the console had just made was refused forever
		// while `git commit` itself ran. What the ref-move class keeps is the
		// family that rewrites what a LOCAL branch points at.
		{[]string{"git", "push", "origin", "master"}, CategoryRepoWriteWord},
		{[]string{"git", "push"}, CategoryRepoWriteWord},
		{[]string{"git", "push", "--force", "origin", "master"}, CategoryRepoWriteWord},
		{[]string{"git", "push", "-f", "origin", "master"}, CategoryRepoWriteWord},
		{[]string{"git", "push", "--delete", "origin", "x"}, CategoryRepoWriteWord},
		{[]string{"git", "push", "--mirror", "origin"}, CategoryRepoWriteWord},
		{[]string{"git", "push", "--all", "origin"}, CategoryRepoWriteWord},
		{[]string{"git", "reset", "--hard", "HEAD~1"}, CategoryRefMoveWord},
		{[]string{"make", "test"}, CategoryLongWord},
	}
	for _, c := range cases {
		rule, ok := tab.MatchCategory(c.words)
		if !ok {
			t.Errorf("%v matched no built-in row — that act is a repository write in every checkout",
				c.words)
			continue
		}
		if rule.Category != c.want {
			t.Errorf("%v classified %q, want %q", c.words, rule.Category, c.want)
		}
	}
	// And the hole the path rows left: a project's OWN tree is not the binary's
	// business. This is the RED state the project file fixes, pinned here so the
	// fallback's limitation is a documented property rather than a surprise.
	if _, ok := tab.MatchCategory([]string{"echo", "x", ">", "src/mine.go"}); ok {
		t.Error("the BUILT-IN table matched a redirection into an unknown project's tree — it must not " +
			"guess; that is what the tracked project file is for")
	}
}
