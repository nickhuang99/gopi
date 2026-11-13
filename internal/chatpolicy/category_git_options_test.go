package chatpolicy

// category_git_options_test.go — the category table matches an INVOCATION, not a
// typed word order (row 01a19d6e).
//
// THE DEFECT THIS PINS. Every ref-move row is `<class> git <subcommand> …`, and the
// table is a WORD-PREFIX match, so inserting one of git's own global options before
// the subcommand made the line match NO row: `git -C <dir> push --force` fell
// through to the consumer's default class (internal/tools' OPERATOR WORK — the
// class that runs) and the fence Rule 41 (2b) keeps for a ref move was bypassed by
// one added word. Measured on the pre-fix tree: all eight spellings below read
// OPERATOR WORK / allowed=true.
//
// THE UNIT UNDER TEST IS MatchCategory, deliberately: the class argument is that
// the TABLE cannot see the subcommand, so the table is where normalisation belongs.
// The end-to-end verdict (class + allowed + the demotions that still read the
// typed words) is pinned by internal/tools' chat_ref_move_git_options_test.go — a
// green here with a red there would mean the classifier threw the normalised
// reading away.

import "testing"

// TestCategoryMatchSeesTheSubcommandBehindGitGlobalOptions is the header case.
func TestCategoryMatchSeesTheSubcommandBehindGitGlobalOptions(t *testing.T) {
	tab, err := ParseCategories("test", "```chat-op-category\n"+
		"ref-move git push *\n"+
		"ref-move git push --force *\n"+
		"ref-move git branch -D *\n"+
		"ref-move git update-ref *\n"+
		"ref-move git reset --hard *\n"+
		"repo-write git add *\n"+
		"```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		name  string
		words []string
		want  string
		ok    bool
	}{
		{"the unadorned spelling still matches", []string{"git", "push", "origin", "master"}, CategoryRefMoveWord, true},
		{"-C before the subcommand", []string{"git", "-C", "/repo", "push", "--force", "origin", "master"}, CategoryRefMoveWord, true},
		{"-c before the subcommand", []string{"git", "-c", "core.hooksPath=/tmp/x", "push", "--force"}, CategoryRefMoveWord, true},
		{"--git-dir attached", []string{"git", "--git-dir=/repo/.git", "push", "--force"}, CategoryRefMoveWord, true},
		{"--work-tree separated", []string{"git", "--work-tree", "/w", "update-ref", "-d", "refs/heads/x"}, CategoryRefMoveWord, true},
		{"a branch force-delete behind -C", []string{"git", "-C", "/repo", "branch", "-D", "main"}, CategoryRefMoveWord, true},
		{"a hard reset behind -C", []string{"git", "-C", "/repo", "reset", "--hard", "HEAD~5"}, CategoryRefMoveWord, true},
		{"an option with no operand", []string{"git", "--no-pager", "push", "--force"}, CategoryRefMoveWord, true},
		{"the more specific row still wins", []string{"git", "-C", "/r", "push", "--force", "o", "m"}, CategoryRefMoveWord, true},
		{"a repository write behind -C", []string{"git", "-C", "/repo", "add", "x.go"}, CategoryRepoWriteWord, true},
		// NOT A REF MOVE: a read, and a subcommand no row names.
		{"a read behind -C", []string{"git", "-C", "/repo", "status"}, "", false},
		{"a log behind -C", []string{"git", "-C", "/repo", "log", "--oneline"}, "", false},
		// NOT A GIT INVOCATION AT ALL: the git word is an argument to echo, so the
		// typed words decide — and no row names `echo`.
		{"git only as an argument", []string{"echo", "git", "push", "--force"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule, ok := tab.MatchCategory(c.words)
			if ok != c.ok {
				t.Fatalf("matched=%v, want %v (rule %+v)", ok, c.ok, rule)
			}
			if ok && rule.Category != c.want {
				t.Errorf("class = %q, want %q", rule.Category, c.want)
			}
		})
	}
}

// TestCategoryMatchSeesTheSubcommandBehindWrapperOperands is the second half of
// the same defect (row 01a19d8a). The table cannot see the subcommand when a
// WRAPPER's operand stands before `git` either: `sudo -u <user> git push …` and
// `env FOO=1 git push …` matched NO row until the leaf package learned the
// wrapper→operand rule.
//
// THE UNIT IS STILL MatchCategory, and the words are the TYPED words: this is the
// reading the classifier actually hands the table, so a table that only answered
// for pre-normalised input would be a green here and a red in the classifier.
func TestCategoryMatchSeesTheSubcommandBehindWrapperOperands(t *testing.T) {
	tab, err := ParseCategories("test", "```chat-op-category\n"+
		"ref-move git push --force *\n"+
		"ref-move git reset --hard *\n"+
		"ref-move git update-ref *\n"+
		"```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		name  string
		words []string
		want  string
		ok    bool
	}{
		{"a privileged wrapper's operand", []string{"sudo", "-u", "nick", "git", "push", "--force", "origin", "master"}, CategoryRefMoveWord, true},
		{"a group operand", []string{"sudo", "-g", "staff", "git", "reset", "--hard", "HEAD~3"}, CategoryRefMoveWord, true},
		{"env's assignment operand", []string{"env", "FOO=1", "git", "push", "--force", "origin", "master"}, CategoryRefMoveWord, true},
		{"env's consuming option", []string{"env", "-u", "GIT_DIR", "git", "update-ref", "-d", "refs/heads/x"}, CategoryRefMoveWord, true},
		{"a bare assignment prefix", []string{"GIT_DIR=/nowhere", "git", "push", "--force", "origin", "main"}, CategoryRefMoveWord, true},
		{"a scheduler's operand", []string{"nice", "-n", "10", "git", "push", "--force", "origin", "main"}, CategoryRefMoveWord, true},
		{"stacked wrappers", []string{"sudo", "-u", "nick", "env", "FOO=1", "git", "push", "--force", "origin", "master"}, CategoryRefMoveWord, true},
		// THE DISCRIMINATOR: `git` is `echo`'s argument, so no row applies, and
		// the typed words (which name no `echo` row) decide.
		{"git only as an argument", []string{"echo", "git", "push", "--force"}, "", false},
		{"a read behind a wrapper", []string{"sudo", "-u", "nick", "git", "status"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule, ok := tab.MatchCategory(c.words)
			if ok != c.ok {
				t.Fatalf("matched=%v, want %v (rule %+v)", ok, c.ok, rule)
			}
			if ok && rule.Category != c.want {
				t.Errorf("class = %q, want %q", rule.Category, c.want)
			}
		})
	}
}

// TestCategoryMatchKeepsTheTypedWordsAsTheFallback: a table that names a GLOBAL
// OPTION on purpose is a table the operator wrote, so the typed reading must still
// apply when the normalised one finds nothing.
func TestCategoryMatchKeepsTheTypedWordsAsTheFallback(t *testing.T) {
	tab, err := ParseCategories("test", "```chat-op-category\n"+
		"ref-move git -C *\n"+
		"```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The normalised path is `git push origin master` — no row — so the typed
	// words (`git -C …`) are what the table gets matched against, and its own row
	// applies.
	rule, ok := tab.MatchCategory([]string{"git", "-C", "/repo", "push", "origin", "master"})
	if !ok || rule.Category != CategoryRefMoveWord {
		t.Fatalf("matched=%v rule=%+v — a row written against the TYPED words (`git -C …`) must still "+
			"apply when the normalised reading matches nothing", ok, rule)
	}
}

// TestCategoryMatchNeverLowersAClass: normalisation may only RAISE a line into the
// class its subcommand names. The demotions that lower a class (a dry run, a clone
// ground's delivery, a fast-forward publish) read the ORIGINAL words in their own
// package, so the risk here is the opposite one — a table whose LONGER row is
// spelled against the typed words must not be beaten by a shorter normalised match.
func TestCategoryMatchNeverLowersAClass(t *testing.T) {
	tab, err := ParseCategories("test", "```chat-op-category\n"+
		"long git push *\n"+
		"repo-write git push *\n"+
		"```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// File order decides an equal-length tie on the NORMALISED path, exactly as it
	// does on the typed one — the two readings use the same chooser.
	rule, ok := tab.MatchCategory([]string{"git", "-C", "/r", "push", "origin", "master"})
	if !ok || rule.Category != CategoryLongWord {
		t.Fatalf("matched=%v rule=%+v — the tie must be decided by FILE ORDER on the normalised path "+
			"too, the same rule the typed path uses", ok, rule)
	}
}
