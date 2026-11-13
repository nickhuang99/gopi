package tools

// git_publish_target_guard_test.go — the acceptance set for the clone-channel
// publish-target guard (PROJECT.md Rule 5: every fix ships a minimal repro
// test; Rule 31: the surface is exercised through the REAL entry point).
//
// THE FIXTURE IS THE INCIDENT'S OWN SHAPE. The server is a BARE repository (so
// no inbound push is refused by the scripted side, and every ref that lands is
// the push's own doing); the run's ground is a CLONE of it — `.git` is a
// DIRECTORY, which is exactly the discriminator the channel uses — standing on
// the integration branch it cloned. The announcement pair the runner exports
// (GOPI_RUN_GROUND / GOPI_RUN_GROUND_BRANCH) is set to that clone and to that
// branch, which is the only channel signal the guard reads.
//
// TWO HALVES, and the split is the point:
//
//	TestCloneGroundPublishTarget_ReproducesTheIncidentWithoutTheGuard
//	    RED, through RAW git (no bash tool): the measured command really does
//	    create `refs/heads/task/<id>` on the server. The guard's predicate is not
//	    on that path, which is why the four stray refs exist.
//	TestCloneGroundPublishTarget_AcceptanceSet
//	    GREEN plus every no-weakening case, each driven through BashInDir — the
//	    real bash-tool entry point — so a case can only pass if the guard is
//	    WIRED IN and not merely present in the package beside it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture's own spellings, named once (Rule 1).
const (
	pgFixtureIntegration = "master"
	pgFixtureOrigin      = "origin"
	pgFixtureSeedFile    = "seed.txt"
	pgFixtureSeedBody    = "one\n"
	pgFixtureStrayBranch = "task/01a15b70-fixture"
	pgFixtureFeatBranch  = "feat/fixture-stray"
	// pgFixtureProbeBranch is the destination Rule 31's real-entry probe names
	// (`HEAD:refs/heads/rule31-probe`), so the fixture-side proof spells exactly
	// the command the live probe runs.
	pgFixtureProbeBranch = "rule31-probe"
	// pgNoDryRunFlag is git's NEGATED spelling of the option (`--[no-]dry-run`,
	// git 2.43), i.e. a REAL push — the dangerous form on this git.
	pgNoDryRunFlag    = "--no-dry-run"
	pgFixtureRefHeads = "refs/heads/"
)

// pgFixtureGit runs git for the fixture; a fixture that cannot be built is
// fatal, never a passing test.
func pgFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBinaryName, args...)
	cmd.Dir = dir
	cmd.Env = append(withoutGitDirEnv(os.Environ()),
		"GIT_AUTHOR_NAME=pubguard", "GIT_AUTHOR_EMAIL=pubguard@test",
		"GIT_COMMITTER_NAME=pubguard", "GIT_COMMITTER_EMAIL=pubguard@test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// pgFixtureBuild materialises a bare server plus a clone of it, standing on the
// integration branch, and announces that clone as this process's ground.
//
// t.TempDir() is pinned off the `/` partition by the suite's own entry-point pin
// (Rule 22) — no scratch path is invented here.
func pgFixtureBuild(t *testing.T) (server, ground string) {
	t.Helper()
	base := t.TempDir()
	server = filepath.Join(base, "server.git")
	pgFixtureGit(t, base, "init", "--bare", "--initial-branch="+pgFixtureIntegration, server)

	// A seed commit so the clone has an integration branch to stand on.
	seed := filepath.Join(base, "seed")
	pgFixtureGit(t, base, "init", "--initial-branch="+pgFixtureIntegration, seed)
	if err := os.WriteFile(filepath.Join(seed, pgFixtureSeedFile), []byte(pgFixtureSeedBody), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	pgFixtureGit(t, seed, "add", pgFixtureSeedFile)
	pgFixtureGit(t, seed, "commit", "-m", "seed")
	pgFixtureGit(t, seed, "remote", "add", pgFixtureOrigin, server)
	pgFixtureGit(t, seed, "push", pgFixtureOrigin, "HEAD:"+pgFixtureRefHeads+pgFixtureIntegration)

	ground = filepath.Join(base, "ground")
	pgFixtureGit(t, base, "clone", server, ground)

	t.Setenv(TaskGroundDirEnv, ground)
	t.Setenv(TaskGroundBranchEnv, pgFixtureIntegration)
	return server, ground
}

// pgServerHeads lists the server's branch refs, short-form, sorted.
func pgServerHeads(t *testing.T, server string) []string {
	t.Helper()
	out := pgFixtureGit(t, server, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// TestCloneGroundPublishTarget_ReproducesTheIncidentWithoutTheGuard is the RED
// half: the measured command, run through raw git, creates the stray ref.
func TestCloneGroundPublishTarget_ReproducesTheIncidentWithoutTheGuard(t *testing.T) {
	server, ground := pgFixtureBuild(t)

	cmd := exec.Command(gitBinaryName, "push", pgFixtureOrigin, "HEAD:"+pgFixtureRefHeads+pgFixtureStrayBranch)
	cmd.Dir = ground
	cmd.Env = withoutGitDirEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("raw push (the incident's own command) failed, so this fixture cannot reproduce it: %v\n%s", err, out)
	}

	heads := pgServerHeads(t, server)
	if !pgContainsString(heads, pgFixtureStrayBranch) {
		t.Fatalf("the raw push did NOT create %s on the server (heads=%v) — the fixture does not reproduce the incident, and the green half would prove nothing",
			pgFixtureStrayBranch, heads)
	}
	if !pgContainsString(heads, pgFixtureIntegration) {
		t.Fatalf("the integration branch vanished from the server (heads=%v)", heads)
	}
}

// TestCloneGroundPublishTarget_AcceptanceSet drives every case through
// BashInDir — the real bash tool — so the guard can only pass if it is wired in.
func TestCloneGroundPublishTarget_AcceptanceSet(t *testing.T) {
	cases := []struct {
		name string
		// build writes the command for one fixture; it is a function because the
		// refused cases are judged on exact spellings.
		command string
		// inClone makes the fixture a CLONE ground (announced) or a WORKTREE
		// ground (.git is a FILE — the channel discriminator).
		inWorktreeChannel bool
		// noGround drops the announcement pair entirely.
		noGround bool
		// probeNoWrite marks a case whose command is a DRY RUN: it must be
		// allowed, AND it must leave the server's head set untouched (a dry run
		// writes nothing — that is why the guard skips it).
		probeNoWrite bool
		refuse       bool
	}{
		{name: "task branch destination", command: "git push origin HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch, refuse: true},
		{name: "feat branch destination", command: "git push origin HEAD:" + pgFixtureFeatBranch, refuse: true},
		{name: "dwim destination", command: "git push origin HEAD:" + pgFixtureStrayBranch, refuse: true},
		{name: "force-prefixed destination", command: "git push origin +HEAD:" + pgFixtureStrayBranch, refuse: true},
		{name: "destination that a -C'd repo resolves", command: "git -C . push origin HEAD:" + pgFixtureStrayBranch, refuse: true},
		{name: "chained clause destination", command: "cd . && git push origin HEAD:" + pgFixtureStrayBranch, refuse: true},

		// REFUSED, and it must stay refused after the commit-message fix: a
		// commit whose message quotes the refspec is permitted, while the push
		// spelled after it on the same line is still the run choosing its own
		// publish target. This arm is the pair the bug report demanded — the
		// allowance must not open the verb.
		{name: "commit message allowed, real push after it refused", command: "git commit --allow-empty -q -m \"docs: ban " + "git push origin " + pgFixtureStrayBranch + "\" && git push origin HEAD:" + pgFixtureStrayBranch, refuse: true},

		// NOT refused: a `git commit -m` whose MESSAGE quotes a refspec. A commit
		// writes no ref on the server, so the guard's subject (the `git push`
		// verb) is not present at all — the message is DATA (measured: task
		// 01a1622f; the guard used to refuse the very commit that documented it).
		{name: "commit message quotes a refspec", command: "git commit --allow-empty -q -m \"docs: ban git push origin HEAD:" + pgFixtureStrayBranch + "\""},
		// NOT refused, and this is the shape that was actually measured: an
		// ordinary multi-line `-m` body quotes the refused spelling on its own
		// line. The shell does not cut a simple command inside a quote, so the
		// scanner must not either.
		{name: "multi-line commit message quotes a refspec", command: "git commit --allow-empty -q -m \"docs: the publish ban\n\ngit push origin HEAD:" + pgFixtureStrayBranch + " is the banned form\""},
		// NOT refused: the same quote-protection for the shell's other separator
		// inside a message.
		{name: "commit message with an in-line separator", command: "git commit --allow-empty -q -m \"docs: the ban; git push origin HEAD:" + pgFixtureStrayBranch + "\""},

		// NOT refused: the ground's own branch, in either spelling.
		{name: "ground branch by name", command: "git push origin HEAD:" + pgFixtureIntegration},
		{name: "ground branch fully qualified", command: "git push origin HEAD:" + pgFixtureRefHeads + pgFixtureIntegration},
		// NOT refused: no destination chosen (the caller's own upstream).
		{name: "bare push", command: "git push"},
		{name: "push with an upstream arg", command: "git push -u origin " + pgFixtureIntegration},
		// NOT refused: a deletion writes no new ref.
		{name: "delete refspec", command: "git push origin :" + pgFixtureRefHeads + pgFixtureStrayBranch},
		// NOT refused: ref FAMILIES nobody picked.
		{name: "push --tags", command: "git push origin --tags"},
		// NOT refused: a worktree ground (Rule 6a/6c's route stays intact).
		{name: "worktree channel", command: "git push origin HEAD:" + pgFixtureStrayBranch, inWorktreeChannel: true},
		// NOT refused: no announced ground at all.
		{name: "no announced ground", command: "git push origin HEAD:" + pgFixtureStrayBranch, noGround: true},
		// NOT refused: the words in a non-git command.
		{name: "quoted words", command: "echo git push origin HEAD:" + pgFixtureStrayBranch},

		// ── THE DRY-RUN CASES (row 01a17ddf) ────────────────────────────────
		//
		// A dry run writes NO ref, so the harm this guard exists for cannot
		// occur, and refusing it refused the very self-check PROJECT.md Rule 6c
		// prescribes. Measured before the fix: `git push --dry-run origin
		// HEAD:refs/heads/rule31-probe` in a clone-channel ground came back with
		// "Nothing was pushed." and a refusal — true, and exactly why it was
		// wrong: nothing was going to be pushed either way.
		//
		// Each of these names a DESTINATION THE REAL FORM REFUSES (the same
		// stray branch as the "task branch destination" case above), and each
		// asserts afterwards that the server is unchanged — so the case proves
		// both "allowed" and "allowed because nothing is written".
		{name: "dry-run long form", command: "git push " + gitPublishDryRunLong + " origin HEAD:" + pgFixtureRefHeads + pgFixtureProbeBranch, probeNoWrite: true},
		{name: "dry-run short form", command: "git push " + gitPublishDryRunShort + " origin HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch, probeNoWrite: true},
		{name: "dry-run explicit true", command: "git push " + gitPublishDryRunLong + "=true origin HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch, probeNoWrite: true},
		{name: "dry-run before the subcommand spelling", command: "git push -n origin HEAD:" + pgFixtureStrayBranch, probeNoWrite: true},

		// REFUSED: the value form with an explicit `false`. MEASURED (git 2.43):
		// push's `--dry-run` takes no value at all, so this spelling is a syntax
		// error that writes nothing — the predicate does not exempt it, so the
		// destination is judged exactly as before and the refusal costs nothing
		// (the command could never have written a ref). The case exists to keep
		// the allowance an allowance rather than a "any word containing dry-run"
		// hole.
		{name: "dry-run explicitly false is not exempted", command: "git push " + gitPublishDryRunLong + "=false origin HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch, refuse: true},
		// REFUSED: the NEGATED spelling — `--no-dry-run` on git 2.43 — IS a real
		// push (measured: raw git writes the ref under it, and
		// TestCloneGroundPublishTarget_DryRunSpellingsAreWhatGitAccepts proves
		// exactly that). It matches neither the short/long word nor the
		// assignment prefix, so the dry-run predicate leaves it judged.
		{name: "negated dry run is a real push", command: "git push " + pgNoDryRunFlag + " origin HEAD:" + pgFixtureRefHeads + pgFixtureProbeBranch, refuse: true},
		// REFUSED: the allowance is CLAUSE-LOCAL. A dry run beside a real push on
		// the same line may not open the second clause.
		{name: "real push chained after a dry run is still refused", command: "git push " + gitPublishDryRunLong + " origin HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch + " ; git push origin HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch, refuse: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, ground := pgFixtureBuild(t)
			switch {
			case tc.noGround:
				t.Setenv(TaskGroundDirEnv, "")
				t.Setenv(TaskGroundBranchEnv, "")
			case tc.inWorktreeChannel:
				// A linked worktree's `.git` is a FILE: rewrite the announced
				// ground to that shape, which is the discriminator the guard reads.
				wt := filepath.Join(filepath.Dir(ground), "linked")
				pgFixtureGit(t, ground, "worktree", "add", "-b", pgFixtureFeatBranch, wt)
				t.Setenv(TaskGroundDirEnv, wt)
				t.Setenv(TaskGroundBranchEnv, pgFixtureFeatBranch)
				ground = wt
			}

			out, err := BashInDir(tc.command, ground)
			if tc.refuse {
				if err == nil {
					t.Fatalf("%q was ALLOWED — a clone-channel run must not choose its own publish target (output: %s)", tc.command, out)
				}
				if !strings.Contains(err.Error(), "clone-channel run publishes its work through the channel") {
					t.Fatalf("%q was refused for the wrong reason: %v", tc.command, err)
				}
				// The refusal must be TRUE: nothing may have been written.
				for _, b := range pgServerHeads(t, server) {
					if b == pgFixtureStrayBranch || b == pgFixtureFeatBranch || b == pgFixtureProbeBranch {
						t.Fatalf("%q was refused but %s exists on the server — the refusal ran AFTER the write", tc.command, b)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("%q was REFUSED but must be allowed: %v (output: %s)", tc.command, err, out)
			}
			// A DRY-RUN case is allowed BECAUSE it writes nothing — so the claim
			// is measured, not assumed: the stray destination is still absent
			// from the server, and git itself reported the dry run.
			if tc.probeNoWrite {
				if heads := pgServerHeads(t, server); pgContainsString(heads, pgFixtureStrayBranch) {
					t.Fatalf("%q is a dry run but %s exists on the server (heads=%v) — the case does not prove a dry run writes nothing",
						tc.command, pgFixtureStrayBranch, heads)
				}
				if !strings.Contains(out, "dry-run") {
					t.Fatalf("%q was allowed but git did not report a dry run (output: %s)", tc.command, out)
				}
			}
		})
	}
}

// pgContainsString is the slice membership test the assertions above share.
func pgContainsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestCloneGroundPublishTarget_DryRunSpellingsAreWhatGitAccepts pins the SEMANTICS
// the dry-run predicate rests on, measured against git itself (raw git — no guard
// on this path, so the reading is git's own answer).
//
// MEASURED on this host (git 2.43.0; `git push -h` prints `-n, --[no-]dry-run`):
//
//	--dry-run          accepted, writes nothing   → the predicate ALLOWS it
//	--dry-run=<any>    REJECTED: "option `dry-run' takes no value" (exit 129)
//	                   → writes nothing either way, so the predicate's handling
//	                     of the value form cannot let a ref be written
//	--no-dry-run       accepted, and it is a REAL push
//	                   → the predicate does NOT match it (neither the short/long
//	                     word nor the assignment prefix), so it stays judged
//
// The last line is the one that matters for the guard: the dangerous spelling on
// this git is the NEGATED form, not the `=false` form. This test proves the guard
// refuses it by running it through BashInDir and checking nothing landed.
func TestCloneGroundPublishTarget_DryRunSpellingsAreWhatGitAccepts(t *testing.T) {
	server, ground := pgFixtureBuild(t)

	raw := func(args ...string) (string, int) {
		cmd := exec.Command(gitBinaryName, args...)
		cmd.Dir = ground
		cmd.Env = withoutGitDirEnv(os.Environ())
		out, _ := cmd.CombinedOutput() //stylegate:ignore the exit status IS the subject (asserted through ProcessState below) and this helper is only ever driven by spellings pinned to be accepted or refused — a spawn failure would surface as code!=-1 on the very next line
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return string(out), code
	}

	// (1) the plain dry run: accepted, writes nothing.
	if out, code := raw("push", gitPublishDryRunLong, pgFixtureOrigin,
		"HEAD:"+pgFixtureRefHeads+pgFixtureProbeBranch); code != 0 {
		t.Fatalf("git refused %s (exit %d), so the allowance case is not exercising the predicate:\n%s",
			gitPublishDryRunLong, code, out)
	}
	if heads := pgServerHeads(t, server); pgContainsString(heads, pgFixtureProbeBranch) {
		t.Fatalf("git WROTE %s under %s (heads=%v) — a dry run must write nothing",
			pgFixtureProbeBranch, gitPublishDryRunLong, heads)
	}

	// (2) the value form: git rejects BOTH values as a syntax error, so the
	// predicate's exemption for it can never let a ref through.
	for _, v := range []string{"true", gitPublishBoolFalse} {
		arg := gitPublishDryRunLong + "=" + v
		if out, code := raw("push", arg, pgFixtureOrigin,
			"HEAD:"+pgFixtureRefHeads+pgFixtureProbeBranch); code == 0 {
			t.Fatalf("git ACCEPTED %s (output: %s) — the predicate's value-form handling must be re-derived", arg, out)
		}
	}
	if heads := pgServerHeads(t, server); pgContainsString(heads, pgFixtureProbeBranch) {
		t.Fatalf("%s was written by a value-form spelling that writes nothing (heads=%v)", pgFixtureProbeBranch, heads)
	}

	// (3) THE DANGEROUS SPELLING: `--no-dry-run` is a REAL push. Raw git writes
	// the ref (that is what makes the spelling worth refusing) …
	if out, code := raw("push", pgNoDryRunFlag, pgFixtureOrigin,
		"HEAD:"+pgFixtureRefHeads+pgFixtureProbeBranch); code != 0 {
		t.Fatalf("git refused %s (exit %d), so this fixture cannot show it is a real push:\n%s",
			pgNoDryRunFlag, code, out)
	}
	if heads := pgServerHeads(t, server); !pgContainsString(heads, pgFixtureProbeBranch) {
		t.Fatalf("%s did NOT write %s (heads=%v) — if it writes nothing, the guard need not refuse it",
			pgNoDryRunFlag, pgFixtureProbeBranch, heads)
	}
}

// TestPublishTargetRefusalAssertsNoUnobservedPublish pins the WORDING rule for
// this guard's refusal (Rule 15: no unmeasured assertion).
//
// THE DEFECT (measured 2026-10-27, row 01a17da5, session 488463): the sentence
// said the ground's branch was "already published by the channel's delivery
// loop". It is not — the guard runs when a run pushes BEFORE its end hook, so
// the loop has not had its turn yet, and on the measured row the ground's ref
// (`refs/heads/task/01a17da5…`) was ABSENT from the server:
//
//	$ git ls-remote --heads origin
//	…  refs/heads/master
//	…  refs/heads/feat/exists
//	…  refs/heads/task/linked
//	   (no refs/heads/task/01a17da507297625b5bcddbd64dc8034)
//
// A refusal that asserts a publish nobody measured teaches the operator to
// distrust the very message that stopped a harmful push. The sentence must state
// the CHANNEL CONTRACT (which destination the loop publishes, from which hook) —
// a fact this file can state — and never a delivery it did not observe.
func TestPublishTargetRefusalAssertsNoUnobservedPublish(t *testing.T) {
	server, ground := pgFixtureBuild(t)
	stray := "HEAD:" + pgFixtureRefHeads + pgFixtureStrayBranch
	out, err := BashInDir("git push origin "+stray, ground)
	if err == nil {
		t.Fatalf("%q must be refused from a clone-channel ground (output: %s)", stray, out)
	}
	sentence := err.Error()
	// THE ASSERTION THAT WAS FALSE ON A MEASURED ROW: the refusal must not claim
	// the delivery loop already published the ground's ref.
	if strings.Contains(sentence, "already published") {
		t.Fatalf("the refusal asserts a publish nobody measured — the guard runs BEFORE the end hook's delivery leg:\n%s", sentence)
	}
	// AND THE FACT THAT IS TRUE MUST STILL BE THERE: the destination the loop
	// does publish, and where it runs from.
	for _, want := range []string{
		"the ONLY destination the channel's delivery loop publishes",
		"end hook",
		"Nothing was pushed.",
	} {
		if !strings.Contains(sentence, want) {
			t.Fatalf("the refusal must carry %q (the measured contract, not an observation):\n%s", want, sentence)
		}
	}
	// The refusal is still TRUE: nothing appeared on the server.
	if heads := pgServerHeads(t, server); pgContainsString(heads, pgFixtureStrayBranch) {
		t.Fatalf("the refusal ran AFTER the write: %v", heads)
	}
}
