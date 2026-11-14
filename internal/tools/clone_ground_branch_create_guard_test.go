package tools

// clone_ground_branch_create_guard_test.go — the acceptance set for the
// clone-channel BRANCH-CREATE guard (PROJECT.md Rule 5: every fix ships a
// minimal repro test; Rule 31: the surface is exercised through the REAL entry
// point).
//
// THE FIXTURE IS THE INCIDENT'S OWN SHAPE (row 01a1957a-0e54, 2026-11-01):
//   - a "shared checkout" — a real git repository the host DECLARES in the
//     settings.json `repos` table (the declaration config.ResolveMergerRepos
//     reads, which is also the merger's sweep list, Rule 12);
//   - a "ground" — a CLONE of it standing on the run's own branch, announced
//     through the runner's own channel (GOPI_RUN_GROUND), which is the only
//     signal the guard reads.
//
// TWO HALVES, and the split is the point:
//
//	TestCloneGroundBranchCreate_ReproducesTheIncidentWithoutTheGuard
//	    RED, through RAW git (no bash tool): the measured command really does
//	    create `task/<id>` in the SHARED checkout and move its HEAD off the
//	    integration branch. The guard's predicate is not on that path, which is
//	    why the incident could happen at all.
//	TestCloneGroundBranchCreate_AcceptanceSet
//	    GREEN plus every no-weakening case, each driven through BashInDir — the
//	    real bash-tool entry point — so a case can only pass if the guard is
//	    WIRED IN and not merely present in the package beside it.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The fixture's own spellings, named once (Rule 1).
const (
	cgbcIntegration   = "master"
	cgbcSharedName    = "shared"
	cgbcGroundName    = "ground"
	cgbcSeedFile      = "seed.txt"
	cgbcSeedBody      = "one\n"
	cgbcRunBranch     = "task/01a195bb-fixture"
	cgbcSettingsFile  = "settings.json"
	cgbcFilePerm      = 0o644
	cgbcRefHeads      = "refs/heads/"
	cgbcGuardRefusal  = "cannot create branch"
	cgbcEscapeVarName = "GOPI_ALLOW_DANGEROUS"
)

// cgbcRun drives git in dir, returning stdout+stderr (the fixture does not care
// which stream git used — it asserts on the wording and on the resulting refs).
func cgbcRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// testutil.GitEnv SCRUBS the inherited git routing (GIT_DIR/GIT_WORK_TREE/
	// GIT_INDEX_FILE …) before adding ours: a fixture's git child that inherited
	// the caller's routing writes into the operator's repository — exactly the
	// violation standards gate TestGate9GitEnvRedProof names by file and line.
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// cgbcTry runs git and returns its trimmed stdout plus whether it SUCCEEDED.
// Used for probes that are EXPECTED to fail on the guarded path (`rev-parse
// --verify` on a ref that must not exist exits 128 — a fatal there would make
// the assertion unreachable and the case would "fail" for the wrong reason).
func cgbcTry(dir string, args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// testutil.GitEnv SCRUBS the inherited git routing (GIT_DIR/GIT_WORK_TREE/
	// GIT_INDEX_FILE …) before adding ours: a fixture's git child that inherited
	// the caller's routing writes into the operator's repository — exactly the
	// violation standards gate TestGate9GitEnvRedProof names by file and line.
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// cgbcFixture builds the shared checkout + the run's own clone ground and pins
// GOPI_SETTINGS at a document whose `repos` table declares ONLY the shared
// checkout (the ground is the run's own tree, not a repo this host merges into).
func cgbcFixture(t *testing.T) (shared, ground string) {
	t.Helper()
	root := testutil.TestDeviceScratchDir(t, "clone-ground-branch-create")
	shared = filepath.Join(root, cgbcSharedName)
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}
	cgbcRun(t, shared, "init", "-q", "-b", cgbcIntegration)
	if err := os.WriteFile(filepath.Join(shared, cgbcSeedFile), []byte(cgbcSeedBody), cgbcFilePerm); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	cgbcRun(t, shared, "add", cgbcSeedFile)
	cgbcRun(t, shared, "commit", "-q", "-m", "seed")

	ground = filepath.Join(root, cgbcGroundName)
	cgbcRun(t, root, "clone", "-q", shared, ground)
	cgbcRun(t, ground, "checkout", "-q", "-b", cgbcRunBranch)

	// The declaration the guard reads — the SAME table the merger sweeps.
	doc := map[string]any{config.ReposSettingsKey: map[string]any{shared: map[string]any{}}}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	path := filepath.Join(root, cgbcSettingsFile)
	if err := os.WriteFile(path, encoded, cgbcFilePerm); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)
	return shared, ground
}

// cgbcAnnounce publishes the runner's own ground channel for this process.
func cgbcAnnounce(t *testing.T, ground string) {
	t.Helper()
	t.Setenv(TaskGroundDirEnv, ground)
	t.Setenv(TaskGroundBranchEnv, cgbcRunBranch)
}

// TestCloneGroundBranchCreate_ReproducesTheIncidentWithoutTheGuard is the RED
// half: raw git, no tool in the path. The measured command creates the branch in
// the SHARED checkout and moves that checkout's HEAD off the integration branch
// — which is exactly the state the incident left behind (reflog: `moving from
// master to task/…`, and later `moving from task/… to master`).
func TestCloneGroundBranchCreate_ReproducesTheIncidentWithoutTheGuard(t *testing.T) {
	shared, _ := cgbcFixture(t)
	before := cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD")
	if strings.TrimSpace(before) != cgbcIntegration {
		t.Fatalf("fixture precondition: shared checkout must stand on %q, got %q", cgbcIntegration, before)
	}

	cgbcRun(t, shared, "checkout", "-q", "-b", cgbcRunBranch)

	after := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD"))
	if after != cgbcRunBranch {
		t.Fatalf("the incident's command must park the SHARED checkout on %q, got %q", cgbcRunBranch, after)
	}
	if ref := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--verify", cgbcRefHeads+cgbcRunBranch)); ref == "" {
		t.Fatalf("the incident's command must create %s%s", cgbcRefHeads, cgbcRunBranch)
	}
	t.Logf("RED reproduced: the shared checkout's HEAD moved %s -> %s and %s%s now exists",
		cgbcIntegration, after, cgbcRefHeads, cgbcRunBranch)
}

// TestCloneGroundBranchCreate_AcceptanceSet drives the REAL bash-tool entry
// point. Each case states what must hold; a case can only pass if the guard is
// wired into bashInDir's reject chain.
func TestCloneGroundBranchCreate_AcceptanceSet(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	cases := []struct {
		name string
		// command is rendered per case because it names the fixture's OWN
		// absolute paths (a relative `cd` would resolve against the ground and
		// measure the wrong directory — the first draft of this test did exactly
		// that and "passed" by failing in bash).
		command    func(shared string) string
		workdir    func(ground, shared string) string
		wantRefuse bool
	}{
		{
			// THE INCIDENT, VERBATIM: the tool's own workdir is the ground, the
			// command cd's into the shared checkout and creates the branch there.
			name:       "the incident: cd shared, checkout -b",
			command:    func(shared string) string { return "cd " + shared + " && git checkout -b " + cgbcRunBranch },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// No `cd` at all: the WORKDIR itself is the shared checkout, while the
			// announced ground is elsewhere — the shape 01a1957a ran in for the
			// rounds after it had `cd`'d into the main checkout.
			name:       "workdir IS the shared checkout",
			command:    func(string) string { return "git checkout -b " + cgbcRunBranch },
			workdir:    func(_, shared string) string { return shared },
			wantRefuse: true,
		},
		{
			// The force spelling resets an existing branch — the same event.
			name:       "force spelling in the shared checkout",
			command:    func(shared string) string { return "cd " + shared + " && git checkout -B " + cgbcRunBranch },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			name:       "git switch -c in the shared checkout",
			command:    func(shared string) string { return "cd " + shared + " && git switch -c " + cgbcRunBranch },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// `git switch -C` — switch's FORCE spelling, the same event as
			// `checkout -B`: it RESETS an existing branch and moves HEAD onto it.
			// It is a SEPARATE row from `-c` on purpose: the two are different
			// constants in cloneGroundBranchCreateFlagSet (Rules 13/14), and a set
			// that lost one of them would still pass every `-c` row. Measured
			// refused by the real probe (probe-after-fix-v3.txt, `switch -C`);
			// this row is what keeps it refused without the probe.
			name:       "git switch -C (force) in the shared checkout",
			command:    func(shared string) string { return "cd " + shared + " && git switch -C " + cgbcRunBranch },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// No weakening: creating a branch in one's OWN ground is the
			// ordinary way a ground is used.
			name:       "creation inside the run's own ground",
			command:    func(string) string { return "git checkout -b feat/inside-my-ground" },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: false,
		},
		{
			// No weakening: a plain checkout creates nothing (it only moves HEAD
			// onto a branch that already exists).
			name:       "plain checkout of an existing branch",
			command:    func(shared string) string { return "cd " + shared + " && git checkout " + cgbcIntegration },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: false,
		},
		{
			// No weakening: read-only git in the shared checkout stays allowed —
			// the guard sits on CREATION, not on the directory.
			name:       "read-only git in the shared checkpoint",
			command:    func(shared string) string { return "cd " + shared + " && git status --porcelain" },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: false,
		},
		{
			// A MEASURED HOLE (2026-11-01): git's own global `-C <dir>` names the
			// repository without any `cd` in the command line. The guard read the
			// directory from the SHELL only, so this spelling created the branch in
			// the shared checkout and moved its HEAD off the integration branch —
			// measured on the revision that introduced the worktree face.
			name:       "git -C names the shared checkout (no cd at all)",
			command:    func(shared string) string { return "git -C " + shared + " checkout -b " + cgbcRunBranch },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// The same hole on the force spelling: `-B` RESETS the name, so this
			// writes the ref even when the checkout is detached.
			name:       "git -C with the force spelling",
			command:    func(shared string) string { return "git -C " + shared + " checkout -B " + cgbcRunBranch },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// `-C` also defeats a `cd` that pointed at the GROUND: the shell
			// selected the ground and git still acts on the shared checkout, so the
			// git-level operand has to win over the run directory.
			name: "git -C overrides a cd into the ground",
			command: func(shared string) string {
				return "cd . && git -C " + shared + " switch -c " + cgbcRunBranch
			},
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// The worktree face through `-C`, measured bypassed the same way
			// (`git -C <shared> worktree add -b …` returned exit 0 and created both
			// the branch and the target directory).
			name: "git -C with worktree add -b",
			command: func(shared string) string {
				return "git -C " + shared + " worktree add -b " + cgbcRunBranch + " " + filepath.Join(filepath.Dir(shared), "wt-dashc")
			},
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// The worktree face's FORCE spelling. `git worktree add -B` RESETS the
			// branch (where `-b` refuses an existing name), so it writes a ref the
			// `-b` row alone would not pin: cloneGroundWorktreeCreateFlagSet holds
			// both, and this row is the measurement that it still does. Measured
			// refused by the real probe (probe-after-fix-v3.txt, `worktree add -B`);
			// the target is a sibling directory that must not be created either.
			name: "git -C with worktree add -B (force)",
			command: func(shared string) string {
				return "git -C " + shared + " worktree add -B " + cgbcRunBranch + " " + filepath.Join(filepath.Dir(shared), "wt-dashC")
			},
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: true,
		},
		{
			// No weakening: `-C` pointing at the run's OWN ground is the ordinary
			// way a ground is used, and must stay allowed.
			name:       "git -C into the run's own ground",
			command:    func(string) string { return "git checkout -b feat/via-c" },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: false,
		},
		{
			// No weakening: the same command with NO announced ground stays
			// untouched — the boundary the guard draws, on this spelling too.
			name:       "git -C with no announced ground",
			command:    func(shared string) string { return "git -C " + shared + " status --porcelain" },
			workdir:    func(ground, _ string) string { return ground },
			wantRefuse: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)

			cmd := tc.command(shared)
			// The refusal leaves through the ERROR channel (bashInDir returns
			// ("", err) for a rejected command), so the case reads both.
			text := func(out string, err error) string {
				if err != nil {
					return out + err.Error()
				}
				return out
			}
			// The tool's workdir is the run's ground unless the case says the
			// workdir IS the shared checkout.
			out, err := BashInDir(cmd, tc.workdir(ground, shared))

			headAfter := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD"))
			refAfter, refExists := cgbcTry(shared, "rev-parse", "--verify", "--quiet", cgbcRefHeads+cgbcRunBranch)

			if tc.wantRefuse {
				if err == nil {
					t.Fatalf("the guard must refuse %q, it ran: %s", cmd, out)
				}
				if !strings.Contains(text(out, err), cgbcGuardRefusal) {
					t.Fatalf("the refusal must carry its own wording (%q), got: %s / %v", cgbcGuardRefusal, out, err)
				}
				// The refusal must name the branch, the shared checkout and the
				// ground it should have been created in.
				for _, want := range []string{cgbcRunBranch, shared, ground} {
					if !strings.Contains(text(out, err), want) {
						t.Fatalf("the refusal must name %q, got: %v", want, text(out, err))
					}
				}
				if headAfter != cgbcIntegration {
					t.Fatalf("a refused creation must leave the shared checkout on %q, it is on %q", cgbcIntegration, headAfter)
				}
				if refExists {
					t.Fatalf("a refused creation must create nothing, %s%s exists (%s)", cgbcRefHeads, cgbcRunBranch, refAfter)
				}
				t.Logf("refused as required: %s", strings.TrimSpace(text(out, err)))
				return
			}

			if err != nil {
				t.Fatalf("must NOT refuse %q: %v\n%s", cmd, err, out)
			}
		})
	}
}

// TestCloneGroundBranchCreate_NoAnnouncedGroundIsStillJudgedByTheDeclaration is
// the REGRESSION half of the 2026-11-01 widening, and it replaces the boundary it
// used to pin ("no announced ground ⇒ never refused").
//
// That old boundary WAS the hole. `git -C <shared> …` names the repository
// without the caller standing in it, so "this process has no ground" says
// nothing about where the ref lands. Measured with a process that had no ground
// at all: the branch was created in the declared shared checkout and the reflog
// kept `HEAD@{1}: checkout: moving from master to task/probe-NOGROUND`.
//
// What survives, and what this case asserts, is the part that is still true: the
// exemption is the DECLARATION, not the ground. So for a process with NO
// announced ground —
//
//	inside a declared shared checkout  → REFUSED, and no ref is created;
//	outside it (its own repository)    → allowed, the branch really appears.
//
// The ref readback is the assertion, not the sentence (a refusal that "says" it
// refused while git ran anyway is the shape this file exists to prevent).
func TestCloneGroundBranchCreate_NoAnnouncedGroundIsStillJudgedByTheDeclaration(t *testing.T) {
	cgbcGuardsOn(t)

	shared, _ := cgbcFixture(t)
	t.Setenv(TaskGroundDirEnv, "")
	t.Setenv(TaskGroundBranchEnv, "")

	// (a) A declared shared checkout is refused WITH NO GROUND IN SIGHT, and the
	//     `git -C` spelling is used on purpose: it is the measured bypass, and a
	//     `cd` would let a shell-only reader answer the same question by luck.
	cmd := "OUT=$(git -C " + shared + " checkout -b " + cgbcRunBranch + " 2>&1)"
	out, err := BashInDir(cmd, t.TempDir())
	if err == nil {
		t.Fatalf("a declared shared checkout must be refused even with no announced ground\n%s", out)
	}
	if !strings.Contains(err.Error()+out, cgbcGuardRefusal) {
		t.Fatalf("refused for the wrong reason (want %q): %v\n%s", cgbcGuardRefusal, err, out)
	}
	if _, exists := cgbcTry(shared, "rev-parse", "--verify", "--quiet", "refs/heads/"+cgbcRunBranch); exists {
		t.Fatalf("the refusal claimed it created nothing, but refs/heads/%s exists in %s", cgbcRunBranch, shared)
	}
	if head := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD")); head != cgbcIntegration {
		t.Fatalf("the shared checkout's HEAD moved to %q; it must still be %q", head, cgbcIntegration)
	}

	// (b) The SAME command in a repository nothing declares is untouched — the
	//     guard is not a general "you may not branch anywhere" rule.
	own := filepath.Join(t.TempDir(), "own-repo")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatalf("mkdir own repo: %v", err)
	}
	cgbcRun(t, own, "init", "-q", "-b", cgbcIntegration)
	// SEED a commit: an unborn HEAD answers 128 to `rev-parse --abbrev-ref HEAD`,
	// which would make the readback below fail for a reason that has nothing to
	// do with the guard (the first draft did exactly that).
	if err := os.WriteFile(filepath.Join(own, cgbcSeedFile), []byte(cgbcSeedBody), cgbcFilePerm); err != nil {
		t.Fatalf("seed own repo: %v", err)
	}
	cgbcRun(t, own, "add", cgbcSeedFile)
	cgbcRun(t, own, "commit", "-q", "-m", "seed")
	out, err = BashInDir("git checkout -b "+cgbcRunBranch, own)
	if err != nil {
		t.Fatalf("an UNDECLARED repository must not be refused, ground or no ground: %v\n%s", err, out)
	}
	if head := strings.TrimSpace(cgbcRun(t, own, "rev-parse", "--abbrev-ref", "HEAD")); head != cgbcRunBranch {
		t.Fatalf("the undeclared repository must really have created %q, HEAD is %q", cgbcRunBranch, head)
	}
}

// TestCloneGroundBranchCreate_GroundDoesNotExemptAReachedSharedCheckout is the
// GREEN half: the widening must not LOSE the behaviour the guard was built for.
// A run that owns a ground and still names the declared shared checkout (through
// `git -C`, or through its own workdir) is refused exactly as before — the
// judgement is not "does this process have a ground" but "is the target
// declared".
func TestCloneGroundBranchCreate_GroundDoesNotExemptAReachedSharedCheckout(t *testing.T) {
	cgbcGuardsOn(t)

	shared, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)

	cmd := "OUT=$(git -C " + shared + " checkout -b " + cgbcRunBranch + " 2>&1)"
	out, err := BashInDir(cmd, ground)
	if err == nil {
		t.Fatalf("a run with a ground must still be refused in the declared shared checkout\n%s", out)
	}
	if !strings.Contains(err.Error()+out, cgbcGuardRefusal) {
		t.Fatalf("refused for the wrong reason (want %q): %v\n%s", cgbcGuardRefusal, err, out)
	}
	// The sentence must name BOTH facts this population has: the declaration and
	// the ground (Rule 15 — a refusal states its own evidence).
	if !strings.Contains(err.Error()+out, shared) || !strings.Contains(err.Error()+out, ground) {
		t.Fatalf("the refusal must name the declared root %q and the ground %q: %v\n%s", shared, ground, err, out)
	}
	if _, exists := cgbcTry(shared, "rev-parse", "--verify", "--quiet", "refs/heads/"+cgbcRunBranch); exists {
		t.Fatalf("refs/heads/%s exists in %s — the creation ran", cgbcRunBranch, shared)
	}
}

// TestCloneGroundBranchCreate_NoGroundRefusalNamesNoPhantomGround pins the
// wording half of the same change: a process that owns NO ground must not be
// told "while this run's own ground is <path>" — there is no such path, and a
// refusal that cites one is a refusal that lies about its evidence.
func TestCloneGroundBranchCreate_NoGroundRefusalNamesNoPhantomGround(t *testing.T) {
	cgbcGuardsOn(t)

	shared, _ := cgbcFixture(t)
	t.Setenv(TaskGroundDirEnv, "")
	t.Setenv(TaskGroundBranchEnv, "")

	reason := guardCloneGroundBranchCreate("git -C "+shared+" checkout -b "+cgbcRunBranch, "")
	if reason == "" {
		t.Fatal("the guard must judge a declared shared checkout with no announced ground")
	}
	if strings.Contains(reason, cgbcPhantomGroundClaim) {
		t.Fatalf("the no-ground refusal claims a ground this process does not have:\n%s", reason)
	}
	if !strings.Contains(reason, shared) {
		t.Fatalf("the refusal must still name the declared root %q:\n%s", shared, reason)
	}
}

// cgbcPhantomGroundClaim is the phrase the WITH-ground sentence uses to cite the
// announced ground. Its absence is what the case above measures.
const cgbcPhantomGroundClaim = "this run's own ground is"

// TestCloneGroundBranchCreate_UndeclaredDirectoryIsNeverRefused pins the other
// boundary: the guard reads the host's DECLARATION (`repos` table) and invents
// none. The same command in a directory that is a git repo but NOT declared is
// untouched — the guard is not a general "you may not branch here" rule.
func TestCloneGroundBranchCreate_UndeclaredDirectoryIsNeverRefused(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	shared, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)

	// Rewrite the declaration to name a DIFFERENT repo, so the shared checkout
	// above is no longer declared.
	other := filepath.Join(filepath.Dir(shared), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	cgbcRun(t, other, "init", "-q", "-b", cgbcIntegration)
	doc := map[string]any{config.ReposSettingsKey: map[string]any{other: map[string]any{}}}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	path := filepath.Join(filepath.Dir(shared), cgbcSettingsFile)
	if err := os.WriteFile(path, encoded, cgbcFilePerm); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)

	cmd := "cd " + shared + " && git checkout -b " + cgbcRunBranch
	out, err := BashInDir(cmd, ground)
	if err != nil {
		t.Fatalf("an UNDECLARED directory must not be refused: %v\n%s", err, out)
	}
}

// ── THE OTHER TWO FACES ─────────────────────────────────────────────────────
//
// The incident had two halves and the branch creation was only the first: the
// run then EDITED cmd/agent/cli_scratch_gc.go in the shared checkout. The guard
// above is wired into the bash reject chain; these two faces are the write
// path (the file tools' own seam) and `git worktree add -b` (a creation that
// writes OUTSIDE the checkout the command runs in).
//
// Each case is driven through its REAL entry point — tools.Write / tools.Edit
// and BashInDir — so a case can only pass if the judgement is wired in, not
// merely present in the package beside it.

const (
	cgbcWriteFileName = "written-into-shared.txt"
	cgbcWriteFileBody = "this must never land in the shared checkout\n"
	cgbcWorktreeName  = "shared-wt"
)

// cgbcGuardsOn pins BOTH switches the file-tool face honours, for the duration of
// a test, and restores them.
//
// Pinning RejectDangerous is not decoration: another test in this package
// (deadcode_behavior_test.go) sets it false, so a case that only pinned the
// escape would pass or fail depending on test ORDER — a fixture that measures its
// neighbour instead of the guard (Rule 15: a pass that did not observe what it
// claims is worse than a red).
func cgbcGuardsOn(t *testing.T) {
	t.Helper()
	prevEscape, prevReject := escapeDangerous, RejectDangerous
	escapeDangerous, RejectDangerous = false, true
	t.Cleanup(func() { escapeDangerous, RejectDangerous = prevEscape, prevReject })
}

// TestCloneGroundFileWrite_RefusesTheIncidentSecondHalf drives tools.Write — the
// write tool's own entry point — at an absolute path inside the declared shared
// checkout while the run has a ground elsewhere.
func TestCloneGroundFileWrite_RefusesTheIncidentSecondHalf(t *testing.T) {
	cgbcGuardsOn(t)
	shared, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)

	target := filepath.Join(shared, cgbcWriteFileName)
	err := Write(target, cgbcWriteFileBody)
	if err == nil {
		t.Fatalf("the write path must refuse a file inside the declared shared checkout %s", shared)
	}
	for _, want := range []string{shared, ground, "shared checkout"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got: %v", want, err)
		}
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("a refused write must create NOTHING, %s exists", target)
	}
	t.Logf("write refused as required: %v", err)
}

// TestCloneGroundFileWrite_EditRefusesTheSameWay pins the EDIT face: same
// predicate, its own sentence, and the target's bytes untouched.
func TestCloneGroundFileWrite_EditRefusesTheSameWay(t *testing.T) {
	cgbcGuardsOn(t)
	shared, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)

	seed := filepath.Join(shared, cgbcSeedFile)
	err := Edit(seed, cgbcSeedBody, "two\n", false)
	if err == nil {
		t.Fatalf("the edit path must refuse a file inside the declared shared checkout %s", shared)
	}
	if !strings.Contains(err.Error(), shared) || !strings.Contains(err.Error(), ground) {
		t.Fatalf("the refusal must name the shared checkout and the ground, got: %v", err)
	}
	body, readErr := os.ReadFile(seed)
	if readErr != nil {
		t.Fatalf("read seed: %v", readErr)
	}
	if string(body) != cgbcSeedBody {
		t.Fatalf("a refused edit must leave the bytes alone, got %q", body)
	}
	t.Logf("edit refused as required: %v", err)
}

// TestCloneGroundFileWrite_InsideTheGroundLands is the no-weakening half: the
// ORDINARY case — the same writer, a path inside the run's own ground — must
// land, or the guard would stop every run from working.
func TestCloneGroundFileWrite_InsideTheGroundLands(t *testing.T) {
	cgbcGuardsOn(t)
	_, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)

	target := filepath.Join(ground, cgbcWriteFileName)
	if err := Write(target, cgbcWriteFileBody); err != nil {
		t.Fatalf("a write inside the run's own ground must land: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != cgbcWriteFileBody {
		t.Fatalf("the write must land its bytes in %s, got %q / %v", target, body, err)
	}
	// And the edit face on the same file, same writer family.
	if err := Edit(target, cgbcWriteFileBody, "edited\n", false); err != nil {
		t.Fatalf("an edit inside the run's own ground must land: %v", err)
	}
	t.Logf("write and edit landed inside the ground: %s", target)
}

// TestCloneGroundFileWrite_NoAnnouncedGroundIsNeverRefused pins the boundary the
// constraint names: a process with NO GOPI_RUN_GROUND (an operator's shell, a
// chat, a repo-less row) keeps writing wherever it likes — the same file the
// test above refuses.
func TestCloneGroundFileWrite_NoAnnouncedGroundIsNeverRefused(t *testing.T) {
	cgbcGuardsOn(t)
	shared, _ := cgbcFixture(t)
	t.Setenv(TaskGroundDirEnv, "")
	t.Setenv(TaskGroundBranchEnv, "")

	target := filepath.Join(shared, cgbcWriteFileName)
	if err := Write(target, cgbcWriteFileBody); err != nil {
		t.Fatalf("no announced ground must mean no refusal: %v", err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != cgbcWriteFileBody {
		t.Fatalf("the write must land when no ground is announced, got %q / %v", body, err)
	}
}

// TestCloneGroundWorktreeAdd_ReproducesTheShapeWithoutTheGuard is the RED half
// for the worktree spelling: raw git, no tool in the path, proves that
// `git worktree add -b` really does create the branch — so the green half below
// is a guard and not a spell.
func TestCloneGroundWorktreeAdd_ReproducesTheShapeWithoutTheGuard(t *testing.T) {
	shared, _ := cgbcFixture(t)
	target := filepath.Join(filepath.Dir(shared), cgbcWorktreeName)
	cgbcRun(t, shared, "worktree", "add", "-b", "task/worktree-red-fixture", target)
	if _, ok := cgbcTry(shared, "rev-parse", "--verify", "--quiet", cgbcRefHeads+"task/worktree-red-fixture"); !ok {
		t.Fatalf("raw `git worktree add -b` must create the branch — the guard's subject is real")
	}
}

// TestCloneGroundWorktreeAdd_IsRefused drives the bash entry point: the same
// command, refused, with nothing created.
func TestCloneGroundWorktreeAdd_IsRefused(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	shared, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)
	target := filepath.Join(filepath.Dir(shared), cgbcWorktreeName)

	cmd := "git worktree add -b task/worktree-green-fixture " + target
	out, err := BashInDir(cmd, shared)
	if err == nil {
		t.Fatalf("the guard must refuse %q in the shared checkout, it ran: %s", cmd, out)
	}
	if !strings.Contains(err.Error()+out, "cannot create branch") {
		t.Fatalf("the refusal must carry the branch-create wording, got: %v / %s", err, out)
	}
	if _, ok := cgbcTry(shared, "rev-parse", "--verify", "--quiet", cgbcRefHeads+"task/worktree-green-fixture"); ok {
		t.Fatalf("a refused worktree add must create no branch")
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("a refused worktree add must create no directory, %s exists", target)
	}
	t.Logf("worktree add refused as required: %v", err)
}

// TestCloneGroundWorktreeAdd_TargetInsideSharedIsRefused pins the OTHER
// directory this face judges: the command is issued from the run's own ground,
// but the worktree it asks git to CREATE is inside the shared checkout.
func TestCloneGroundWorktreeAdd_TargetInsideSharedIsRefused(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	shared, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)
	target := filepath.Join(shared, cgbcWorktreeName)

	out, err := BashInDir("git worktree add -b task/worktree-target-fixture "+target, ground)
	if err == nil {
		t.Fatalf("a worktree created INSIDE the shared checkout must be refused, it ran: %s", out)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("a refused worktree add must create no directory, %s exists", target)
	}
	t.Logf("target leg refused as required: %v", err)
}

// TestCloneGroundWorktreeAdd_InsideTheGroundLands is the no-weakening half: a
// worktree created inside the run's OWN ground is the ordinary use.
func TestCloneGroundWorktreeAdd_InsideTheGroundLands(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	_, ground := cgbcFixture(t)
	cgbcAnnounce(t, ground)
	target := filepath.Join(ground, cgbcWorktreeName)

	out, err := BashInDir("git worktree add -b task/worktree-inside-my-ground "+target, ground)
	if err != nil {
		t.Fatalf("a worktree inside the run's own ground must be allowed: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("the worktree must exist at %s: %v\n%s", target, statErr, out)
	}
}

// ── THE REF-WRITE FAMILY (2026-11-01, row 01a19640) ─────────────────────────
//
// WHY THIS SECOND ACCEPTANCE SET EXISTS. The set above covers the FOUR creation
// spellings the guard was built for (checkout -b/-B, switch -c/-C, worktree add
// -b/-B, and the `git -C` bypass of each). It left three ref writes that carry no
// creation FLAG, and all three were MEASURED walking through the guard in the
// declared shared checkout with this run's ground announced:
//
//	git -C /home/user/work/gopi branch task/probe-b1 a4899a4f          exit 0
//	git -C /home/user/work/gopi branch -f task/probe-b1 90927414       exit 0
//	git -C /home/user/work/gopi update-ref refs/heads/task/probe-u1 …  exit 0
//
// The fix restates the guard's subject as "a git invocation that CREATES, MOVES
// or RENAMES a ref" (cloneGroundRefWritingVerbs) and adds a FAIL-CLOSED arm for
// a subcommand in neither that list nor the closed read-only list. These rows are
// that contract, one per verb form, each driven through BashInDir — the real bash
// tool — and each re-reading the repository afterwards, so "nothing was written"
// is a measurement and never an inference from an error string.

const (
	cgbcRefFamilyBranch = "task/ref-family-fixture"
	cgbcRefFamilyTag    = "probe-tag-fixture"
	cgbcRefFamilyOther  = "task/ref-family-other"
	cgbcUnknownGitVerb  = "commit-tree"
	cgbcHeadRefName     = "HEAD"
	// cgbcWorktreeProbeTarget is the DIRECTORY operand handed to
	// cloneGroundWrittenRefs in the recogniser probe below. It is never created
	// and never resolved — the recogniser only reads the argv shape — so it is a
	// name, not a path (and deliberately not under /tmp: a probe operand must not
	// look like a place this suite might write, Rule 22).
	cgbcWorktreeProbeTarget = "worktree-probe-target"
	cgbcRefFamilyBranchNS   = "refs/heads/"
	cgbcRefFamilyTagNS      = "refs/tags/"
)

// cgbcRefFamilyRefs lists a repository's branch AND tag refs, short form, so a
// case can assert the whole namespace is unchanged rather than only its own row.
func cgbcRefFamilyRefs(t *testing.T, dir string) string {
	t.Helper()
	return cgbcRun(t, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/tags")
}

// TestCloneGroundBranchCreate_RefWriteFamilyIsRefused is the verb table: every
// form that writes a ref in a declared shared checkout is refused, the shared
// checkout's HEAD is unmoved, and its whole ref set is byte-identical afterwards.
func TestCloneGroundBranchCreate_RefWriteFamilyIsRefused(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	cases := []struct {
		name string
		// ref is the ref this form WRITES — asserted to appear in the refusal
		// sentence. It is a separate field because the real probe caught a
		// defect exactly here (2026-11-01): `git branch -f task/probe-b1 HEAD`
		// was refused with `cannot create branch "HEAD"` — the guard read the
		// destination off the LAST positional, which for a FORCE is the start
		// point (a READ). A refusal that names the wrong ref is not a cosmetic
		// problem: it tells the caller the guard objected to something they did
		// not do. The fail-closed rows write nothing, so `ref` is empty there.
		ref string
		// command is a function of the fixture's paths for the same reason the
		// first set's is: the spellings name absolute directories.
		command func(shared, base string) string
	}{
		{
			// `git branch <name> [<start>]` — a ref CREATION with no flag at all,
			// which is why a flag-driven recogniser missed it entirely.
			name:    "branch creates a ref (no flag)",
			ref:     cgbcRefFamilyBranch,
			command: func(shared, _ string) string { return "git -C " + shared + " branch " + cgbcRefFamilyBranch },
		},
		{
			// `git branch -f <name> <base>` — a ref MOVE onto a new value. The
			// MOVED ref is the FIRST positional and `HEAD` after it is the start
			// point, which is the distinction the assertion below pins.
			name: "branch -f moves a ref",
			ref:  cgbcRefFamilyBranch,
			command: func(shared, _ string) string {
				return "git -C " + shared + " branch -f " + cgbcRefFamilyBranch + " HEAD"
			},
		},
		{
			// `git branch -m <old> <new>` — a RENAME creates the destination name.
			name: "branch -m renames a ref",
			ref:  cgbcRefFamilyOther,
			command: func(shared, _ string) string {
				return "git -C " + shared + " branch -m " + cgbcIntegration + " " + cgbcRefFamilyOther
			},
		},
		{
			// `git update-ref` — the plumbing write the earlier guard never
			// classified; measured exit 0 in the shared checkout.
			name: "update-ref writes a ref",
			ref:  cgbcRefFamilyBranch,
			command: func(shared, _ string) string {
				return "git -C " + shared + " update-ref " + cgbcRefFamilyBranchNS + cgbcRefFamilyBranch + " HEAD"
			},
		},
		{
			// `git symbolic-ref HEAD <ref>` — points the shared checkout's HEAD at
			// another branch, which is the incident's effect without a checkout.
			name: "symbolic-ref moves HEAD",
			// The ref this form writes is HEAD itself — the symref, not the
			// branch it points at — so that is the name the sentence must carry.
			ref: cgbcHeadRefName,
			command: func(shared, _ string) string {
				return "git -C " + shared + " symbolic-ref HEAD " + cgbcRefFamilyBranchNS + cgbcRefFamilyBranch
			},
		},
		{
			// `git tag [-f] <name>` — a DECISION ref (Rule 9) is nobody's to write
			// in the shared repository on a run's authority.
			name:    "tag writes a tag ref",
			ref:     cgbcRefFamilyTag,
			command: func(shared, _ string) string { return "git -C " + shared + " tag " + cgbcRefFamilyTag },
		},
		{
			// The FAIL-CLOSED arm: a real git subcommand this guard has never
			// classified. It must be refused, not guessed harmless.
			name: "unknown subcommand is fail-closed",
			command: func(shared, _ string) string {
				return "git -C " + shared + " " + cgbcUnknownGitVerb + " $(git -C " + shared + " rev-parse HEAD^{tree})"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			before := cgbcRefFamilyRefs(t, shared)
			headBefore := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD"))

			cmd := tc.command(shared, filepath.Dir(shared))
			out, err := BashInDir(cmd, ground)
			if err == nil {
				t.Fatalf("the guard must refuse %q, it ran: %s", cmd, out)
			}
			sentence := out + err.Error()
			// ONE of the two sentences: the ref-write wording, or the
			// fail-closed wording. Both name the shared checkout and the ground.
			if !strings.Contains(sentence, cgbcGuardRefusal) && !strings.Contains(sentence, cgbcFailClosedRefusal) {
				t.Fatalf("the refusal must carry the guard's own wording, got: %s", sentence)
			}
			if !strings.Contains(sentence, shared) || !strings.Contains(sentence, ground) {
				t.Fatalf("the refusal must name the shared checkout %q and the ground %q, got: %s", shared, ground, sentence)
			}
			// The sentence must name the ref THIS form writes. Measured defect
			// (2026-11-01, real probe): `git branch -f task/probe-b1 HEAD` was
			// refused as `cannot create branch "HEAD"` — the destination was read
			// off the last positional, which for a FORCE is the start point. A
			// refusal naming the wrong ref tells the caller the guard objected to
			// something they did not do, so this is asserted per verb form.
			if tc.ref != "" && !strings.Contains(sentence, `"`+tc.ref+`"`) {
				t.Fatalf("the refusal must name the ref this form writes (%q), got: %s", tc.ref, sentence)
			}
			// "Nothing was written" is MEASURED: same ref set, HEAD unmoved.
			if after := cgbcRefFamilyRefs(t, shared); after != before {
				t.Fatalf("a refused ref write must leave the ref set alone:\n before=%q\n after =%q", before, after)
			}
			if headAfter := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD")); headAfter != headBefore {
				t.Fatalf("a refused ref write must leave HEAD on %q, it is on %q", headBefore, headAfter)
			}
			t.Logf("refused as required: %s", strings.TrimSpace(sentence))
		})
	}
}

// cgbcFailClosedRefusal is the fail-closed sentence's anchor word — the phrase
// the unknown-verb refusal carries and the ref-write refusal does not, so a case
// can tell WHICH of the two arms answered.
const cgbcFailClosedRefusal = "does NOT recognise that subcommand"

// TestCloneGroundBranchCreate_ReadersAndDeletionsStillPass is the no-weakening
// half of the widening: a guard that refuses readers would make the shared
// checkout unusable for inspection, and one that refuses DELETIONS would take
// over `gopi task archive`'s lane (Rule 9). Each case is counted against the ref
// set too, so "allowed" is not confused with "allowed because it errored".
func TestCloneGroundBranchCreate_ReadersAndDeletionsStillPass(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	cases := []string{
		"git -C %s status --porcelain",
		"git -C %s rev-parse --abbrev-ref HEAD",
		"git -C %s for-each-ref --format=%(refname) refs/heads",
		"git -C %s branch --list",
		"git -C %s branch -a",
		"git -C %s tag",
		"git -C %s tag --list",
		"git -C %s symbolic-ref HEAD",
		"git -C %s worktree list",
		// The SUB-FORM readers (2026-11-01, second measured defect). The first
		// cut of the fail-closed arm refused all four of these as "unrecognised",
		// which made the shared checkout un-inspectable — a run could no longer
		// ask what its remote was, what the reflog said, or what a config key
		// held, all of them plain reads GOPI.md §2.8 tells a run to perform
		// without asking. Measured: 4 of 17 read commands refused
		// (probe-overreach.txt); after judging these verbs by sub-form, 0 of 17
		// (probe-overreach-after.txt).
		"git -C %s remote -v",
		"git -C %s remote",
		"git -C %s remote show origin",
		"git -C %s shortlog -sn",
		"git -C %s reflog",
		"git -C %s reflog -3",
		"git -C %s config --get remote.origin.url",
		"git -C %s config --list",
	}
	for _, tmpl := range cases {
		t.Run(fmt.Sprintf(tmpl, "shared"), func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			cmd := fmt.Sprintf(tmpl, shared)
			if out, err := BashInDir(cmd, ground); err != nil {
				t.Fatalf("a READ of the shared checkout must stay allowed: %q → %v\n%s", cmd, err, out)
			}
		})
	}
}

// TestCloneGroundBranchCreate_SubFormWriteVerbsStillRefused is the OTHER half of
// the sub-form rule: the verbs whose READS are now allowed (`remote`, `config`,
// `reflog`) must still be refused in their WRITE forms. A sub-form rule that let
// `git remote prune` through would be the original defect wearing a new name —
// `prune`/`update` delete or move remote-tracking refs, `reflog expire` deletes
// reflog entries and can drop refs, and `config <key> <value>` / `--unset`
// rewrite the shared checkout's own .git/config.
//
// Measured on the real shared checkout before this test existed
// (probe-subform-writes.txt, 7/7 refused, `state unchanged: true` on every row);
// this table is what keeps that true without the probe.
func TestCloneGroundBranchCreate_SubFormWriteVerbsStillRefused(t *testing.T) {
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	cases := []string{
		"git -C %s remote prune origin",
		"git -C %s remote update",
		"git -C %s remote add probe-x /nowhere",
		"git -C %s remote remove probe-x",
		"git -C %s config --unset user.name",
		"git -C %s config user.name probe-x",
		"git -C %s reflog expire --expire=now --all",
		"git -C %s reflog delete --rewrite HEAD@{0}",
	}
	for _, tmpl := range cases {
		t.Run(fmt.Sprintf(tmpl, "shared"), func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			cmd := fmt.Sprintf(tmpl, shared)
			before := cgbcRefFamilyRefs(t, shared)
			headBefore := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD"))
			out, err := BashInDir(cmd, ground)
			if err == nil {
				t.Fatalf("the WRITE form must be refused, it ran: %q → %s", cmd, out)
			}
			sentence := out + err.Error()
			if !strings.Contains(sentence, cgbcFailClosedRefusal) {
				t.Fatalf("the refusal must be the fail-closed sentence (%q), got: %s", cgbcFailClosedRefusal, sentence)
			}
			// "Refused" must mean "nothing was written": the ref set and HEAD are
			// re-read rather than trusted from the error string.
			if after := cgbcRefFamilyRefs(t, shared); after != before {
				t.Fatalf("a refused write form must leave the ref set alone:\n before=%q\n after =%q", before, after)
			}
			if headAfter := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD")); headAfter != headBefore {
				t.Fatalf("a refused write form must leave HEAD on %q, it is on %q", headBefore, headAfter)
			}
		})
	}
}

// TestCloneGroundBranchCreate_EveryWritingVerbIsJudged is the anti-drift arm.
// cloneGroundRefWritingVerbs and cloneGroundWrittenRefs are two readers of one
// contract, and a verb added to the map without a branch in the switch would be
// a SILENT allowance — exactly the shape that let `branch -f` and `update-ref`
// through. This test asserts the two agree, verb by verb: for every writing verb
// a probe invocation must produce at least one judged ref write.
func TestCloneGroundBranchCreate_EveryWritingVerbIsJudged(t *testing.T) {
	probes := map[string][]string{
		cloneGroundBranchVerb:       {cgbcRefFamilyBranch},
		gitCheckoutSubcmd:           {taskBranchCreateFlagShort, cgbcRefFamilyBranch},
		gitSwitchSubcmd:             {taskBranchCreateFlagSwitch, cgbcRefFamilyBranch},
		worktreeToken:               {worktreeAddToken, taskBranchCreateFlagShort, cgbcRefFamilyBranch, cgbcWorktreeProbeTarget},
		refGuardUpdateRefVerb:       {cgbcRefFamilyBranchNS + cgbcRefFamilyBranch, "HEAD"},
		refGuardSymbolicRefVerb:     {cgbcIntegration, cgbcRefFamilyBranchNS + cgbcRefFamilyBranch},
		refGuardFetchVerb:           {"origin", "+refs/heads/x:refs/heads/" + cgbcRefFamilyBranch},
		cloneGroundTagVerb:          {cgbcRefFamilyTag},
		cloneGroundReplaceVerb:      {"HEAD"},
		cloneGroundPackRefsVerb:     {},
		cloneGroundFilterBranchVerb: {"--", "HEAD"},
	}
	for verb := range cloneGroundRefWritingVerbs {
		t.Run(verb, func(t *testing.T) {
			probe, ok := probes[verb]
			if !ok {
				t.Fatalf("verb %q is in cloneGroundRefWritingVerbs but this test has no probe for it — add one, or the vocabulary and its recogniser have drifted apart (the defect this test exists for)", verb)
			}
			if got := cloneGroundWrittenRefs(verb, probe); len(got) == 0 {
				t.Fatalf("cloneGroundWrittenRefs(%q, %v) judged NOTHING — a writing verb with no recogniser is a silent allowance", verb, probe)
			}
		})
	}
	for verb := range probes {
		if !cloneGroundRefWritingVerbs[verb] {
			t.Fatalf("this test probes %q, which the vocabulary does not list as a writing verb — one of the two is wrong", verb)
		}
	}
}

// TestCloneGroundBranchCreate_VerbListsAreDisjoint pins the two closed lists as
// a PARTITION: a verb in both would be refused by one arm and allowed by the
// other depending on call order, which is a verdict that depends on nothing the
// caller can see.
func TestCloneGroundBranchCreate_VerbListsAreDisjoint(t *testing.T) {
	for verb := range cloneGroundReadOnlyVerbs {
		if cloneGroundRefWritingVerbs[verb] {
			t.Fatalf("%q is listed BOTH as a ref writer and as read-only", verb)
		}
	}
}
