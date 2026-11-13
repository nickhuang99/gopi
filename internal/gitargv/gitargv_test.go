package gitargv

// gitargv_test.go — the ONE reader of git's global-option vocabulary, proved on
// the shapes that made a second copy look tempting: the separated and the attached
// spelling of an operand-taking option, an option that takes none, several globals
// stacked, a wrapper word in front of `git`, and the two answers that mean "there
// is no subcommand to read".

import (
	"strings"
	"testing"
)

// TestSubcommandPathReadsGitGlobalOptions is the table's contract: what the path
// is for each spelling, including the ones that must answer nil.
func TestSubcommandPathReadsGitGlobalOptions(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string // the path, space-joined; "" means nil
	}{
		{"no globals", []string{"git", "push", "origin", "master"}, "git push origin master"},
		{"-C with its operand", []string{"git", "-C", "/repo", "push", "--force"}, "git push --force"},
		{"-c with its operand", []string{"git", "-c", "k=v", "reset", "--hard"}, "git reset --hard"},
		{"--git-dir separated", []string{"git", "--git-dir", "/r/.git", "update-ref", "x"}, "git update-ref x"},
		{"--git-dir attached", []string{"git", "--git-dir=/r/.git", "update-ref", "-d", "x"}, "git update-ref -d x"},
		{"--work-tree attached", []string{"git", "--work-tree=/w", "push", "-f"}, "git push -f"},
		{"--namespace separated", []string{"git", "--namespace", "ns", "push"}, "git push"},
		{"an option with no operand", []string{"git", "--no-pager", "push", "-f"}, "git push -f"},
		{"a short option with no operand", []string{"git", "-p", "push"}, "git push"},
		{"several globals stacked", []string{"git", "-C", "/r", "--no-pager", "-c", "k=v", "push"}, "git push"},
		{"a wrapper word in front", []string{"sudo", "git", "-C", "/r", "push"}, "git push"},
		// ── A WRAPPER'S OWN OPERAND IS READ (row 01a19d8a) ──────────────────
		// These cases answered "" until this row, which was the hole: the scan
		// treated every plain word before `git` as an argument to some other
		// command, so a wrapper's OPERAND ended it — and the ref-move fence never
		// saw the subcommand behind the wrapper. Closed by OperatorWords +
		// OperandWords and the consumption rule in GitHead.
		{"a privileged wrapper's separated operand", []string{"sudo", "-u", "nick", "git", "push"}, "git push"},
		{"a privileged wrapper's group operand", []string{"sudo", "-g", "staff", "git", "push"}, "git push"},
		{"env's assignment operand", []string{"env", "FOO=1", "git", "push"}, "git push"},
		{"env's several assignment operands", []string{"env", "LC_ALL=C", "LANG=C", "git", "push"}, "git push"},
		{"env's consuming option and its operand", []string{"env", "-u", "GIT_DIR", "git", "push"}, "git push"},
		{"a bare assignment prefix", []string{"GIT_DIR=/nowhere", "git", "push"}, "git push"},
		{"a scheduler's operand", []string{"nice", "-n", "10", "git", "push"}, "git push"},
		{"time's operand", []string{"time", "-p", "git", "push"}, "git push"},
		{"a wrapper that takes no operand", []string{"nohup", "git", "push"}, "git push"},
		{"xargs with its own operand", []string{"xargs", "-n", "1", "git", "push"}, "git push"},
		{"stacked wrappers", []string{"sudo", "-u", "nick", "env", "FOO=1", "git", "push"}, "git push"},
		// ── AND THE DISCRIMINATOR STILL HOLDS ──────────────────────────────
		// `git` here really IS an argument to another command, and the operand
		// rule is exactly what tells the two apart: `echo` consumes nothing, so
		// the word after it is not its operand and the scan refuses.
		{"an operation word before git is an argument", []string{"echo", "git", "push"}, ""},
		{"a wrapper with an unknown option stays unreadable", []string{"sudo", "-Z", "opaque", "git", "push"}, ""},
		{"git with no subcommand", []string{"git", "-C", "/r"}, ""},
		{"a bare option with no operand left", []string{"git", "--git-dir"}, ""},
		{"no git at all", []string{"ls", "-la"}, ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SubcommandPath(c.in)
			if c.want == "" {
				if got != nil {
					t.Fatalf("path = %q, want nil (there is no subcommand to read)", strings.Join(got, " "))
				}
				return
			}
			if got == nil {
				t.Fatalf("path = nil, want %q", c.want)
			}
			if joined := strings.Join(got, " "); joined != c.want {
				t.Fatalf("path = %q, want %q", joined, c.want)
			}
		})
	}
}

// TestSubcommandPathDoesNotMutateItsInput: the path is built into a fresh slice,
// so a caller that keeps the typed words (every demotion leg does — a `-C <dir>`
// names the directory git will run in) still holds them unchanged.
func TestSubcommandPathDoesNotMutateItsInput(t *testing.T) {
	in := []string{"git", "-C", "/repo", "push", "--force"}
	before := strings.Join(in, " ")
	if SubcommandPath(in) == nil {
		t.Fatal("fixture no longer yields a path — this test would pass vacuously")
	}
	if after := strings.Join(in, " "); after != before {
		t.Fatalf("the input was mutated: %q -> %q", before, after)
	}
}

// TestOptionsWithOperandIsThePublishedVocabulary pins the table's CONTENT, because
// it is now shared: a caller in another package resolves `-C`'s operand and
// refuses `--git-dir`, and both read THIS map. Dropping a row here would silently
// stop a guard somewhere else rather than fail to compile.
func TestOptionsWithOperandIsThePublishedVocabulary(t *testing.T) {
	for _, w := range []string{DashCDirOption, "-c", GitDirOption, "--work-tree", "--namespace"} {
		if !OptionsWithOperand[w] {
			t.Errorf("%q is missing from OptionsWithOperand — every reader shares this one table", w)
		}
	}
	if OptionsWithOperand[BinaryName] || OptionsWithOperand["--no-pager"] {
		t.Error("a word that consumes no operand is in OptionsWithOperand — the subcommand reader would " +
			"swallow the NEXT word and lose the subcommand")
	}
	if !OperatorWords["sudo"] || !OperatorWords["xargs"] || !OperatorWords["nohup"] {
		t.Error("OperatorWords lost a wrapper command — `sudo git push` would read as no git invocation")
	}
	// THE OPERAND TABLE IS WHAT MAKES A WRAPPER READABLE AT ALL, so the shapes
	// this row was measured on are pinned BY NAME: dropping one row silently
	// turns that spelling back into "no git here" rather than failing to compile.
	for _, shape := range []struct{ wrapper, opt string }{
		{"sudo", "-u"}, {"sudo", "-g"}, {"doas", "-u"}, {"env", "-u"},
		{"nice", "-n"}, {"xargs", "-n"}, {"time", "-f"},
	} {
		if !OperandWords[shape.wrapper][shape.opt] {
			t.Errorf("OperandWords[%q][%q] is missing — `%s %s <operand> git push` would read as "+
				"no git invocation, which is the defect this table closes",
				shape.wrapper, shape.opt, shape.wrapper, shape.opt)
		}
	}
	// `nohup` takes no operand at all: it must be an OperatorWord and must NOT
	// carry an operand row (an empty row would be a claim that it consumes
	// something).
	if OperandWords["nohup"] != nil {
		t.Error("nohup carries an operand row and takes none — it would swallow the git word")
	}
}

// TestAssignmentWordIsTheShellsRule pins the assignment SHAPE, because two
// wrappers consume words by shape rather than by flag (`env FOO=1 git …`, and the
// bare `FOO=1 git …` prefix the shell itself strips). A shape that is too loose
// eats a real command word; too tight and env's operand reads as "no git here".
func TestAssignmentWordIsTheShellsRule(t *testing.T) {
	for _, w := range []string{"FOO=1", "_A9=2", "LC_ALL=C", "PATH=/usr/bin:/bin", "X="} {
		if !AssignmentWord(w) {
			t.Errorf("AssignmentWord(%q) = false, want true", w)
		}
	}
	for _, w := range []string{"=x", "a/b=c", "-u", "git", "1FOO=x", "FOO", ""} {
		if AssignmentWord(w) {
			t.Errorf("AssignmentWord(%q) = true, want false — the shape must not swallow a "+
				"command word", w)
		}
	}
}
