package tools

// Regression guard for the shared-stash collision family (PROJECT.md Rule 5:
// every fix ships a minimal repro test).
//
// The repro is the incident itself, reduced to its smallest form: a repository
// with a MAIN checkout plus ONE linked worktree is the state in which
// `refs/stash` stops being "this person's stack" and becomes a repo-wide stack
// two sessions push onto.
//
// The epic's whole acceptance set lives in ONE test
// (TestSharedStashGuard_AcceptanceSet), and it is driven through the REAL entry
// point — `BashInDir`, exactly what the bash tool calls — rather than through
// the predicate directly, so a case can only pass if the guard is wired into
// the tool it governs (Rule 31). Every case is answered by the same
// verdict function, so a permitted case and a refused case cannot drift apart:

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stashGuardGit runs a git command in dir for the fixture, failing the test on
// error (a fixture that cannot be built is not a passing test).
func stashGuardGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(withoutGitDirEnv(os.Environ()),
		"GIT_AUTHOR_NAME=guard", "GIT_AUTHOR_EMAIL=guard@test",
		"GIT_COMMITTER_NAME=guard", "GIT_COMMITTER_EMAIL=guard@test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// stashGuardRepo builds a repository at root with one commit.
func stashGuardRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	stashGuardGit(t, root, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "trunk.txt"), []byte("trunk\n"), 0o644); err != nil {
		t.Fatalf("write trunk: %v", err)
	}
	stashGuardGit(t, root, "add", "trunk.txt")
	stashGuardGit(t, root, "commit", "-q", "-m", "seed")
}

// stashGuardFixture is the working set of the acceptance test: a repository
// with the SHARED-stash condition (more than one linked worktree, one of them
// named with the literal token "stash" — the shape the false positive was
// measured on), plus a single-worktree repository for the ordinary local flow,
// plus the report/notes files whose NAMES carry the token.
type stashGuardFixture struct {
	// shared is a 2-worktree repository; the epic's cases run in stashWorktree.
	shared        string
	stashWorktree string
	// single is a repository with exactly one checkout.
	single string
	// notes and report are paths whose names contain "stash".
	notes  string
	report string
}

// stashGuardAcceptanceFixture builds the fixture above under the test's own
// scratch directory.
func stashGuardAcceptanceFixture(t *testing.T) stashGuardFixture {
	t.Helper()
	base := t.TempDir()
	f := stashGuardFixture{
		shared: filepath.Join(base, "acceptance-repo"),
		single: filepath.Join(base, "single-repo"),
		notes:  filepath.Join(base, "stash-guard-notes.md"),
		report: filepath.Join(base, "stash-pop-verbatim-report.md"),
	}
	stashGuardRepo(t, f.shared)
	stashGuardRepo(t, f.single)
	// The worktree's own DIRECTORY NAME carries the token — the measured false
	// positive (task 01a0c54a) was a path like this one being read as a stash
	// invocation.
	f.stashWorktree = filepath.Join(base, "gopi-stash-guard-acceptance-wt")
	if err := os.MkdirAll(f.stashWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	stashGuardGit(t, f.shared, "worktree", "add", "-q", f.stashWorktree, "-b", "acceptance")
	if err := os.WriteFile(f.notes, []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if err := os.WriteFile(f.report, []byte("report\n"), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	return f
}

// stashAcceptanceCase is one row of the epic's acceptance set. verb == "" means
// the command must be PERMITTED; a non-empty verb means the command must be
// REFUSED and the refusal must name that verb (the wrong-verb defect: the
// epic's `git stash pop; echo "…"` was announced as `git stash push`).
type stashAcceptanceCase struct {
	name string
	dir  string
	cmd  string
	verb string
}

// TestSharedStashGuard_AcceptanceSet is the epic's acceptance set in ONE test,
// every case driven through BashInDir — the real bash-tool entry point, so the
// wiring at bash.go is exercised and not merely the predicate beside it.
func TestSharedStashGuard_AcceptanceSet(t *testing.T) {
	f := stashGuardAcceptanceFixture(t)

	// Every mutation verb git accepts, in the 1-worktree repo (green before and
	// after: the ordinary local stash flow is never refused) — and the same
	// verbs in the 2-worktree repo, where each must be refused by name.
	singleWorktreePermits := []string{
		"git stash pop", "git stash apply", "git stash push -m wip",
		"git stash save wip", "git stash drop", "git stash clear",
		"git stash create", "git stash store -m x $(git rev-parse HEAD)",
		"git stash import", "git stash branch tmp-branch",
	}
	cases := []stashAcceptanceCase{
		// --- PERMITTED: the measured false positives, verbatim -------------
		{
			name: "permit/cd-into-a-stash-named-worktree",
			dir:  f.shared,
			cmd:  "cd " + f.stashWorktree + " && git status --porcelain",
		},
		{
			name: "permit/echo-stash-path-then-git-log",
			dir:  f.stashWorktree,
			cmd:  "echo " + f.report + "; git log --oneline -1",
		},
		{
			name: "permit/cat-stash-notes",
			dir:  f.stashWorktree,
			cmd:  "cat " + f.notes,
		},
		// --- PERMITTED: the diagnosis path, before and after ---------------
		{name: "permit/stash-list", dir: f.stashWorktree, cmd: "git stash list"},
		{name: "permit/stash-show-p", dir: f.stashWorktree, cmd: "git stash show -p"},
		// --- PERMITTED: the token is an argument, not a git subcommand -----
		{name: "permit/echo-git-stash", dir: f.stashWorktree, cmd: "echo git stash"},
		{name: "permit/grep-stash", dir: f.stashWorktree, cmd: "grep -rn stash " + f.notes},
		// --- REFUSED, naming the verb git was handed -----------------------
		{
			name: "refuse/chained-pop-is-not-push",
			dir:  f.stashWorktree,
			cmd:  `git stash pop; echo "git stash pop is repo-wide"`,
			verb: stashMutationPop,
		},
		{name: "refuse/pop", dir: f.stashWorktree, cmd: "git stash pop", verb: stashMutationPop},
		{name: "refuse/apply", dir: f.stashWorktree, cmd: "git stash apply", verb: stashMutationApply},
		{name: "refuse/drop", dir: f.stashWorktree, cmd: "git stash drop", verb: stashMutationDrop},
		{name: "refuse/clear", dir: f.stashWorktree, cmd: "git stash clear", verb: stashMutationClear},
		{name: "refuse/push-message", dir: f.stashWorktree, cmd: "git stash push -m wip", verb: stashMutationPush},
		{name: "refuse/bare-stash-is-implicit-push", dir: f.stashWorktree, cmd: "git stash", verb: stashMutationPush},
		{name: "refuse/quiet-pop", dir: f.stashWorktree, cmd: "git stash --quiet pop", verb: stashMutationPop},
		{
			name: "refuse/dash-C-operand-repo",
			dir:  f.stashWorktree,
			cmd:  "git -C " + f.shared + " stash drop",
			verb: stashMutationDrop,
		},
		// A read-only clause leading the line must NOT carry the verdict: the
		// chain the agent naturally emits was permitted by the earlier
		// first-clause predicate (measured residual bypass).
		{
			name: "refuse/read-only-clause-does-not-carry-the-verdict",
			dir:  f.stashWorktree,
			cmd:  `git rev-parse --show-toplevel; git stash list; echo "---run---"; git stash pop`,
			verb: stashMutationPop,
		},
		{name: "refuse/newline-separated", dir: f.stashWorktree, cmd: "git stash list\ngit stash clear", verb: stashMutationClear},
		{name: "refuse/xargs-git-stash-pop", dir: f.stashWorktree, cmd: "echo x | xargs -r git stash pop", verb: stashMutationPop},
		// --- REFUSED: EVERY clause separator, because "the mutation sits in a
		// later clause" is the shape the substring predicate missed. The verb
		// named is always the one git was handed: a read-only clause leading
		// the chain never carries the verdict (`list && pop` names `pop`).
		// These five rows are the residual of the archived branch
		// archive/fix/stash-guard-acceptance: `newline` and the read-only-chain
		// case above landed on master via a sibling, the separators below did
		// not, so nothing pinned them until now (re-authored 2026-09-27).
		{name: "refuse/and-separator", dir: f.stashWorktree, cmd: "git stash list && git stash pop", verb: stashMutationPop},
		{name: "refuse/or-separator", dir: f.stashWorktree, cmd: "git stash pop || true", verb: stashMutationPop},
		{name: "refuse/pipe-separator", dir: f.stashWorktree, cmd: "git stash show -p | cat; git stash apply", verb: stashMutationApply},
		{name: "refuse/second-clause-only", dir: f.stashWorktree, cmd: "echo begin; git stash drop", verb: stashMutationDrop},
		{name: "refuse/wrapper-word-before-git", dir: f.stashWorktree, cmd: "nice git stash drop", verb: stashMutationDrop},
		// --- PERMITTED: the token is TEXT, never a git subcommand. The first
		// three are the other half of the same residual: a predicate widened to
		// judge every clause must still not refuse prose, a bare path, or `git
		// log … -- stash`, which are the false positives the whole guard family
		// was measured on (task 01a0c54a).
		{
			name: "permit/quoted-prose-is-not-an-invocation",
			dir:  f.stashWorktree,
			cmd:  `echo "git stash pop"`,
		},
		{
			name: "permit/path-token-without-git",
			dir:  f.stashWorktree,
			cmd:  "ls -d " + f.stashWorktree,
		},
		{
			name: "permit/git-log-named-stash-ref",
			dir:  f.stashWorktree,
			cmd:  "git log --oneline -1 -- stash",
		},
		{
			name: "permit/read-only-pipeline",
			dir:  f.stashWorktree,
			cmd:  "git stash list 2>&1 | head -3",
		},
	}
	for _, cmd := range singleWorktreePermits {
		cases = append(cases, stashAcceptanceCase{name: "permit/single-worktree/" + cmd, dir: f.single, cmd: cmd})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := BashInDir(c.cmd, c.dir)
			if c.verb == "" {
				if err != nil {
					t.Fatalf("%q in %s must be PERMITTED, got refusal: %v", c.cmd, c.dir, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%q in %s must be REFUSED (git ran it), got output: %q", c.cmd, c.dir, out)
			}
			if !strings.Contains(err.Error(), "git stash "+c.verb) {
				t.Fatalf("%q must be refused naming `git stash %s`, got: %v", c.cmd, c.verb, err)
			}
		})
	}
}

// TestSharedStashGuard_RefusesMutationAnywhereInACompoundCommand is the
// regression test for the COMPOUND-CHAIN layer: a clause-scoped predicate is
// not clause-EXHAUSTIVE, so `git stash pop` chained after `;` was bypassed
// whenever a leading clause satisfied the predicate first (measured
// 2026-09-27, task 01a0c545: `git rev-parse --show-toplevel; git stash list;
// echo "---run---"; git stash pop` was PERMITTED and git ran `pop` raw).
//
// The acceptance set above pins that bypass on ONE chain shape. This test pins
// the DIMENSION the bypass lives in — the separator — because a predicate keyed
// on a single separator is a predicate the next command shape walks around.
// Every separator that can carry a second command is covered (`;`, `&&`, `||`,
// `|`, newline), both orders (mutation first, mutation last), and the refusal
// must name the MUTATION it found rather than the read-only clause that
// preceded it. Each case is driven through BashInDir — the real bash-tool entry
// point — so the layer can only pass if it is wired into the tool it governs
// (Rule 31).
func TestSharedStashGuard_RefusesMutationAnywhereInACompoundCommand(t *testing.T) {
	f := stashGuardAcceptanceFixture(t)

	// refused carries the mutation the chained line must be refused BY NAME
	// for; a read-only clause leading the line must NOT carry the verdict.
	refused := []struct {
		name string
		cmd  string
		verb string
	}{
		// The epic's smallest form: the leading clause is read-only and the
		// mutation rides behind `;`.
		{name: "semicolon-epic-minimal", cmd: "true ; git stash pop", verb: stashMutationPop},
		// READ-ONLY FIRST — the measured bypass, verbatim.
		{name: "semicolon-readonly-first", cmd: "git stash list; git stash pop", verb: stashMutationPop},
		// Every separator that can carry a second command.
		{name: "and-and", cmd: "git stash list && git stash drop", verb: stashMutationDrop},
		{name: "or-or", cmd: "git stash list || git stash clear", verb: stashMutationClear},
		{name: "pipe", cmd: "git stash list | git stash pop", verb: stashMutationPop},
		{name: "newline", cmd: "git stash list\ngit stash save wip", verb: stashMutationSave},
		{name: "show-then-apply", cmd: "git stash show -p; git stash apply", verb: stashMutationApply},
		{name: "readonly-echo-mutation", cmd: "git stash list; echo '---run---'; git stash pop", verb: stashMutationPop},
		// The epic's exact measured chain, all four clauses.
		{
			name: "epic-measured-chain",
			cmd:  `git rev-parse --show-toplevel; git stash list; echo "---run---"; git stash pop`,
			verb: stashMutationPop,
		},
		// A read-only clause AFTER the mutation must not become the reported
		// verb either — the diagnostic names the mutation git was given.
		{name: "mutation-first-readonly-last", cmd: "git stash pop; git stash list", verb: stashMutationPop},
		// The mutation's position in the chain is irrelevant.
		{name: "mutation-in-the-middle", cmd: "git stash list; git stash drop; git stash list", verb: stashMutationDrop},
		// A `cd` preamble (the shape a session emits) must not shield it.
		{name: "cd-preamble-then-mutation", cmd: "cd " + f.shared + " && git stash pop", verb: stashMutationPop},
	}

	for _, c := range refused {
		t.Run("refuse/"+c.name, func(t *testing.T) {
			out, err := BashInDir(c.cmd, f.stashWorktree)
			if err == nil {
				t.Fatalf("%q in a 2-worktree repo must be REFUSED (git ran it), got output: %q", c.cmd, out)
			}
			if !strings.Contains(err.Error(), "git stash "+c.verb) {
				t.Fatalf("%q must be refused naming `git stash %s`, got: %v", c.cmd, c.verb, err)
			}
			if !strings.Contains(err.Error(), "git apply --3way") {
				t.Fatalf("%q: refusal does not name the non-destructive alternative:\n%s", c.cmd, err)
			}
		})
	}

	// A chain of read-only verbs mutates nothing: `stash list`/`show` are how a
	// session inspects the shared stack, and refusing them would make the guard
	// unusable for the diagnosis it exists to enable.
	for _, cmd := range []string{
		"git stash list; git stash show -p stash@{0}",
		"git stash list && git stash list",
		"git stash list; echo done",
		"git rev-parse --show-toplevel; git stash list",
	} {
		if out, err := BashInDir(cmd, f.stashWorktree); err != nil {
			t.Errorf("all-read-only chain %q must pass, got refusal %v (output %q)", cmd, err, out)
		}
	}

	// The single-worktree case keeps the ordinary local flow even when chained:
	// the refusal is about the SHARED stack, not about chaining.
	if out, err := BashInDir("git stash list; git stash pop", f.single); err != nil {
		t.Errorf("single-worktree chained stash must pass, got refusal %v (output %q)", err, out)
	}
}

// TestSharedStashGuard_RefusesMutationInMultiWorktreeRepo is the original
// regression test for the incident: with a second linked worktree present, a
// stash mutation must be refused (the refusal is what stops another run's WIP
// being applied through this worktree). It calls the predicate directly, which
// keeps a fine-grained signal beside the end-to-end acceptance set above.
func TestSharedStashGuard_RefusesMutationInMultiWorktreeRepo(t *testing.T) {
	root := t.TempDir()
	stashGuardRepo(t, root)
	wt := filepath.Join(root, "..", "guarded-wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	stashGuardGit(t, root, "worktree", "add", "-q", wt, "-b", "guarded")

	for _, cmd := range []string{
		"git stash pop",
		"git stash apply",
		"git -C " + root + " stash pop",
		"git stash",
		`git stash pop; echo "git stash pop is repo-wide"`,
		"git stash pop; grep stash file",
		"git stash --quiet pop",
		"git stash --keep-index",
		"git -c core.hooksPath=/dev/null stash drop",
	} {
		if reason := guardSharedStash(cmd, wt); reason == "" {
			t.Errorf("%q in a 2-worktree repo: guard permitted it, want a refusal", cmd)
			continue
		} else if !strings.Contains(reason, "git apply --3way") {
			t.Errorf("%q: refusal does not name the non-destructive alternative:\n%s", cmd, reason)
		}
	}

	if reason := guardSharedStash("git stash pop", wt); !strings.Contains(reason, "`git stash pop`") {
		t.Errorf("refusal must name `git stash pop`, got:\n%s", reason)
	}
	if reason := guardSharedStash("git -C "+root+" stash drop", wt); !strings.Contains(reason, "`git stash drop`") {
		t.Errorf("`git -C <dir> stash drop` must be judged as drop, got:\n%s", reason)
	}
}

// TestSharedStashGuard_PermitsReadOnlyAndSingleWorktree pins the two shapes the
// guard must NOT refuse — a refusal that fires on the diagnosis path, or on the
// ordinary single-checkout stash, is a false positive that would make the
// guard worth switching off.
func TestSharedStashGuard_PermitsReadOnlyAndSingleWorktree(t *testing.T) {
	root := t.TempDir()
	stashGuardRepo(t, root)

	for _, cmd := range []string{"git stash pop", "git stash push -m wip"} {
		if reason := guardSharedStash(cmd, root); reason != "" {
			t.Errorf("%q in a 1-worktree repo: unexpected refusal: %s", cmd, reason)
		}
	}

	wt := filepath.Join(root, "..", "readonly-wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	stashGuardGit(t, root, "worktree", "add", "-q", wt, "-b", "readonly")
	for _, cmd := range []string{
		"git stash list",
		"git stash show -p stash@{0}",
		"git status --porcelain",
		"cd /home/user/workspace/gopi-stash-guard && git status --porcelain",
		"echo /home/user/workspace/stash-pop-verbatim-report.md; git log --oneline -1",
		"git status --porcelain",
		"cat stash-guard-notes.md",
	} {
		if reason := guardSharedStash(cmd, wt); reason != "" {
			t.Errorf("%q must pass, got refusal: %s", cmd, reason)
		}
	}
}
