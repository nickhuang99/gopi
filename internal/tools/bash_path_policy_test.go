package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// guardPolicyFixture points the ambient layers at sandbox trees so the policy's
// roots are known: the repo's worktree root (through the env override — the ONE
// sandbox hook, Rule 2), the scratch home, and the system temp root (TMPDIR,
// which the suite's pin already moved off the boot device).
//
// It returns (worktreeRoot, sessionDir, fixtureDir): a declared root, a session
// working directory that is NOT a declared root, and a temp fixture dir.
func guardPolicyFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "declared-root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(session, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	t.Setenv("GOPI_TEST_SCRATCH_HOME", filepath.Join(t.TempDir(), "scratch-home"))
	// The system temp root is pinned to the project's OWN default temp root
	// (<home>/workspace/_tmp) instead of following the ambient TMPDIR: the suite
	// pin puts TMPDIR inside /dev/shm, where the guard ALSO declares a scratch
	// home, so an "outside every declared root" path cannot be spelled from
	// t.TempDir() at all. With the fixture root known by construction, the probe
	// below has a location that is provably outside on every host.
	if tmpRoot, err := scratch.DefaultTmpDir(); err == nil {
		t.Setenv("TMPDIR", tmpRoot)
	}
	fixture := t.TempDir()
	return root, session, fixture
}

// TestPathPolicy_RefusesWorktreeAddOutsideDeclaredRoot is acceptance criterion
// (b): `git worktree add /dev/shm/...` — the exact operation the /dev/shm
// assessment measured filling a tmpfs whose cap nobody chose — is refused, and
// the refusal names the target.
func TestPathPolicy_RefusesWorktreeAddOutsideDeclaredRoot(t *testing.T) {
	_, session, _ := guardPolicyFixture(t)
	outside := worktreeroots.ShmDir + "/gopi-hf-control"
	reason := rejectOutsideDeclaredRoots("git worktree add "+outside+" -b feat/x", session)
	if reason == "" {
		t.Fatalf("git worktree add %s was ALLOWED — the blast-radius guard did not fire", outside)
	}
	if !strings.Contains(reason, outside) {
		t.Errorf("refusal does not name the target %q: %s", outside, reason)
	}
	if !strings.Contains(reason, worktreeroots.WorktreeRootSettingsKey) {
		t.Errorf("refusal does not tell the reader how to declare a root: %s", reason)
	}
}

// TestPathPolicy_RefusesOutOfRootRecursiveDelete is the other half of (b): a
// recursive delete whose resolved target lies outside every declared root.
func TestPathPolicy_RefusesOutOfRootRecursiveDelete(t *testing.T) {
	_, session, _ := guardPolicyFixture(t)
	for _, command := range []string{
		"rm -rf /home/user/work/GoPi",
		"rm -rf " + worktreeroots.ShmDir + "/gopi-test-scratch",
		"sudo rm -rf /var/lib/thing",
		"rm -rf /home/user/workspace-other", // prefix STRING match would allow this
		"chown -R root:root /home/user/work",
		"chmod -R 777 /etc",
		"mount -t tmpfs tmpfs " + worktreeroots.ShmDir,
		"dd if=/dev/zero of=/dev/sda",
		"find /home/user/work -name '*.go' -delete",
		"git worktree remove /home/user/work/GoPi-someone-else",
	} {
		if reason := rejectOutsideDeclaredRoots(command, session); reason == "" {
			t.Errorf("%q was ALLOWED — a destructive shape outside every declared root must be refused", command)
		}
	}
}

// TestPathPolicy_AllowsInRootTargets is the NEGATIVE PROBE required by the
// acceptance criteria: the refusal must be a property check, not a blanket ban.
// Every command here is a legitimate cleanup or a legitimate worktree creation
// inside a declared root, and each must stay allowed — otherwise the guard
// would break the very cleanup the system must keep doing (reclaim, fixture
// teardown, scratch GC) and would be switched off.
func TestPathPolicy_AllowsInRootTargets(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	scratchHome := os.Getenv("GOPI_TEST_SCRATCH_HOME")

	for _, tc := range []struct {
		why     string
		command string
	}{
		{"worktree in the declared root (-b before the path)", "git worktree add -b feat/x " + filepath.Join(root, "gopi-task")},
		{"worktree in the declared root (path before -b)", "git worktree add " + filepath.Join(root, "gopi-task") + " -b feat/x"},
		{"worktree in the declared root, tilde spelling of the branch flag", "git worktree add -B feat/x " + filepath.Join(root, "wt")},
		{"relative worktree path inside the declared-root-adjacent session", "git worktree add ./wt -b feat/x"},
		{"recursive delete of a tree inside the session workdir", "rm -rf build"},
		{"recursive delete of a tree inside the session workdir (./)", "rm -rf ./build/ ./dist/"},
		{"absolute recursive delete of a session subtree", "rm -rf " + filepath.Join(session, "build")},
		{"recursive delete inside the scratch home", "rm -rf " + filepath.Join(scratchHome, "suite-x")},
		{"recursive delete inside a temp fixture the guard DOES declare", "rm -rf " + filepath.Join(os.TempDir(), "wbr-time")},
		{"recursive chown inside the declared root", "chown -R nick:nick " + root},
		{"worktree REMOVAL inside the declared root", "git worktree remove " + filepath.Join(root, "wt")},
		{"a non-destructive command mentioning a path", "ls -la /etc && cat /proc/meminfo"},
		{"a read-only git command mentioning worktree", "git worktree list --porcelain"},
		{"a message containing the words (no git invocation)", "echo \"git worktree add /dev/shm/x\""},
	} {
		if reason := rejectOutsideDeclaredRoots(tc.command, session); reason != "" {
			t.Errorf("%s: %q was REFUSED (%s) — the policy must not be blanket", tc.why, tc.command, reason)
		}
	}
}

// TestPathPolicy_RefusesOutOfRootWorktreeRemoval: removing another session's
// registration is the same blast-radius shape as deleting its directory, so the
// path must clear the same bar. (The out-of-root path is deliberately NOT a
// system-temp one: the system temp root IS a declared fixture root, by design.)
func TestPathPolicy_RefusesOutOfRootWorktreeRemoval(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	if reason := rejectOutsideDeclaredRoots("git worktree remove "+filepath.Join(root, "wt"), session); reason != "" {
		t.Fatalf("in-root worktree removal refused: %s", reason)
	}
	if reason := rejectOutsideDeclaredRoots("git worktree remove /home/other-agent/work/GoPi-someone-else", session); reason == "" {
		t.Fatal("out-of-root worktree removal allowed")
	}
}

// TestPathPolicy_RefusesUnverifiableTarget: a target hiding behind a shell
// expansion cannot be checked, and an unverifiable target must never be
// created or destroyed (the fail-closed rule the cwd whitelist already had).
func TestPathPolicy_RefusesUnverifiableTarget(t *testing.T) {
	_, session, _ := guardPolicyFixture(t)
	// A relative target is fine; an ABSOLUTE target built from a variable that
	// resolves nowhere is not resolvable and must be refused.
	reason := rejectOutsideDeclaredRoots("rm -rf ${TARGET_DIR}/inner", session)
	if reason == "" {
		t.Fatal("a target built from an explicit directory variable was allowed")
	}
}

// TestPathPolicy_RefusesUnverifiableWorktreeTarget is the regression for the
// measured ASYMMETRY (Rule 41 finding, 2026-10-12): the destructive arm refuses
// a token carrying a shell expansion fail-closed, while the WORKTREE arm had no
// such rule — the token was handed to absolutize() as if it were a literal
// relative path, so a `$`-built target was silently resolved against the workdir
// and judged against a FABRICATED in-root path, while the real path the shell
// would build lies anywhere on the host.
//
// Measured on the live binary (session 01a12e2b, evidence in its artifacts dir):
//
//	V=$S/artifacts/mVar; git worktree add --detach "$V" master   → ALLOWED
//	  test -d "$V" = EXISTS, `git worktree list` = 1 hit
//	git worktree add --detach $S/artifacts/mLit1 master          → REFUSED
//
// A linked worktree is a whole checkout (the same multi-hundred-MB tree the
// /dev/shm assessment measured), so an unverifiable target is refused BY NAME
// exactly as the destructive arm refuses it — the two arms now share ONE
// predicate, which is what keeps them from drifting apart again.
func TestPathPolicy_RefusesUnverifiableWorktreeTarget(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	outside := filepath.Join(filepath.Dir(session), "outside-root")

	for _, tc := range []struct {
		why     string
		command string
		token   string
	}{
		{"variable holding an out-of-root absolute target (the measured shape)",
			"V=" + outside + "/mVar; git worktree add --detach \"$V\" master", "$V"},
		{"variable as the path PREFIX, `--detach` before the path",
			"S=/" + filepath.Base(outside) + "; git worktree add --detach \"$S/wt\" master", "$S"},
		{"braced variable spelling",
			"git worktree add ${WT} -b feat/x", "${WT}"},
		{"command substitution as the whole target",
			"git worktree add \"$(pwd)/../outside\" -b feat/x", "$("},
		{"backtick command substitution as the whole target",
			"git worktree add `hostname`-wt -b feat/x", "`hostname`"},
		{"variable target on the REMOVAL side (the same judgement, both verbs)",
			"git worktree remove \"$WT\"", "$WT"},
		{"git's -C value carries the expansion, target is relative",
			"git -C \"$REPO\" worktree add ./wt -b feat/x", "$REPO"},
	} {
		reason := rejectOutsideDeclaredRoots(tc.command, session)
		if reason == "" {
			t.Errorf("%s: %q was ALLOWED — an unverifiable worktree target must be refused fail-closed", tc.why, tc.command)
			continue
		}
		if !strings.Contains(reason, "shell variable/command substitution") {
			t.Errorf("%s: refusal does not name the cause: %s", tc.why, reason)
		}
		if !strings.Contains(reason, worktreeShapeWhy) {
			t.Errorf("%s: refusal does not name the shape: %s", tc.why, reason)
		}
	}

	// The refusal is about UNVERIFIABILITY, not about where the variable happens
	// to point: a variable that resolves INSIDE the declared root is refused too
	// (the guard cannot know what the shell will substitute). The literal
	// spelling of the same path stays allowed — that is the negative probe that
	// keeps the fix from being a blanket ban on worktrees.
	if reason := rejectOutsideDeclaredRoots("R="+root+"; git worktree add \"$R/wt\" -b feat/x", session); reason == "" {
		t.Error("an in-root target built from a variable was allowed — the predicate must key on resolvability, not on the resolved value")
	}
	for _, literal := range []string{
		"git worktree add -b feat/x " + filepath.Join(root, "wt"),
		"git worktree add --detach " + filepath.Join(root, "wt"),
		"git worktree remove " + filepath.Join(root, "wt"),
		// The scan is scoped to the git invocation (and the cds that precede
		// it), so an expansion in a LATER simple command — the ordinary
		// `… && echo "rc=$?"` trailer every agent command carries — must NOT
		// refuse an otherwise-literal, in-root worktree. A guard that refuses
		// this is a false refusal that teaches the operator to distrust it.
		"git worktree add -b feat/x " + filepath.Join(root, "wt") + ` && echo "rc=$?"`,
		"git worktree add --detach " + filepath.Join(root, "wt") + " && git worktree list | head -1 && echo done",
	} {
		if reason := rejectOutsideDeclaredRoots(literal, session); reason != "" {
			t.Errorf("literal in-root command %q was REFUSED (%s) — the fix must not widen the existing judgement", literal, reason)
		}
	}
}

// TestPathPolicy_DeclaredRootIsReadFromSettings proves the declaration path:
// with no env override, a repo whose settings entry declares a root is the root
// the policy permits, and the DEFAULT (not the declaration) is what an
// undeclared repo gets.
func TestPathPolicy_DeclaredRootIsReadFromSettings(t *testing.T) {
	declared := filepath.Join(t.TempDir(), "repo-declared-root")
	if err := os.MkdirAll(declared, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(t.TempDir(), "settings.json")
	body := `{"repos": {"` + repo + `": {"build": "make build", "worktree_root": "` + declared + `"}}}`
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	t.Setenv(worktreeroots.WorktreeRootEnvName, "") // exercise the settings layer, not the override
	// The memo key is the workdir; clear it so this test reads the new file.
	settingsMemo.Clear()

	reason := rejectOutsideDeclaredRoots("git worktree add "+filepath.Join(declared, "wt")+" -b feat/x", repo)
	if reason != "" {
		t.Fatalf("a worktree inside the repo's DECLARED root was refused: %s", reason)
	}
	reason = rejectOutsideDeclaredRoots("git worktree add /dev/shm/gopi-adhoc -b feat/x", repo)
	if reason == "" {
		t.Fatal("a worktree outside the repo's declared root was allowed")
	}
}

// TestPathPolicy_WorktreePathIsPositionalNotTheBranch guards the false-refusal
// bug: `-b <branch>` takes a value, so a naive "first non-flag token" read picks
// the BRANCH and would refuse every legitimate worktree. The two argument
// orders must agree.
func TestPathPolicy_WorktreePathIsPositionalNotTheBranch(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	path := filepath.Join(root, "wt")
	orders := []string{
		"git worktree add -b feat/x " + path,
		"git worktree add " + path + " -b feat/x",
		"git -C " + session + " worktree add --detach " + path,
	}
	for _, command := range orders {
		targets := gitWorktreePathTargets(command, session)
		if len(targets) != 1 || targets[0].path != path {
			t.Errorf("gitWorktreePathTargets(%q) = %v, want [%s]", command, targets, path)
		}
		if reason := rejectOutsideDeclaredRoots(command, session); reason != "" {
			t.Errorf("%q refused: %s", command, reason)
		}
	}
	// …and the out-of-root spelling of every order is refused.
	for _, command := range []string{
		"git worktree add -b feat/x " + worktreeroots.ShmDir + "/x",
		"git worktree add " + worktreeroots.ShmDir + "/x -b feat/x",
		"git -C " + session + " worktree add --detach " + worktreeroots.ShmDir + "/x",
	} {
		if reason := rejectOutsideDeclaredRoots(command, session); reason == "" {
			t.Errorf("%q was ALLOWED", command)
		}
	}
}

// TestPathPolicyGitChangeDirIsNotTheTarget pins the measured defect of
// 2026-09-15: git's GLOBAL `-C <path>` names the REPOSITORY the command runs
// against, and a repository is deliberately outside the worktree roots (the
// main checkout is not a worktree root). Read as the removal target, that option
// refused `git -C <repo> worktree remove <wt>` — the shape every reclaim, every
// finish-cleanup step and every agent cleanup uses — and the refusal then became
// permanent residue: the guard's own message blamed the repository, the operator
// saw a policy refusal instead of a broken guard, and the worktree stayed
// registered (the detached probe worktree of a verification run is the live
// instance).
//
// The SAME misread refused `git -C <repo> worktree add <wt>` — the ADD that a
// released worktree needs to come back — so the repair is asserted in both
// directions: the option is consumed, the target is judged, and the target alone.
func TestPathPolicyGitChangeDirIsNotTheTarget(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	repo := filepath.Join(t.TempDir(), "repo-outside-every-root")
	wt := filepath.Join(root, "gopi-task")
	// The out-of-root probe must sit outside EVERY declared root. t.TempDir()
	// does not qualify here (the suite's own scratch home is under it, and the
	// scratch home is a declared root), so this uses the same out-of-root
	// location the sibling refusal tests use (Rule 22: a temp path is judged by
	// the root measurement, never by its spelling).
	outsideTarget := filepath.Join(worktreeroots.ShmDir, "gopi-guard-elsewhere", "gopi-someone-else")

	// ALLOWED: the removal/creation an agent legitimately performs. `repo` is
	// outside every declared root by construction — the repository always is —
	// and the refusal that used to fire here named exactly that path.
	for _, tc := range []struct{ why, command string }{
		{"the reclaim shape (git -C <repo> worktree remove <wt>)", "git -C " + repo + " worktree remove " + wt},
		{"the cleanup shape with --force", "git -C " + repo + " worktree remove --force " + wt},
		{"the creation shape (git -C <repo> worktree add <wt>)", "git -C " + repo + " worktree add " + wt + " -b feat/x"},
		{"the attached spelling (-C<path>)", "git -C" + repo + " worktree add " + wt + " -b feat/x"},
		{"another global option before the subcommand", "git -c core.hooksPath=/dev/null -C " + repo + " worktree add " + wt + " -b feat/x"},
	} {
		if reason := rejectOutsideDeclaredRoots(tc.command, session); reason != "" {
			t.Errorf("%s was REFUSED: %s\n%s must not be read as the worktree target", tc.why, reason, worktreeChangeDirFlag)
		}
	}

	// REFUSED — the property is intact: a target genuinely outside every root is
	// still refused. (A RELATIVE target is judged against its own base — the -C
	// directory — which the baseDir assertion below pins; its resolution point
	// is what the guard and git must agree on, not that every relative spelling
	// is refused.)
	for _, command := range []string{
		"git -C " + repo + " worktree remove " + outsideTarget,
		"git -C " + repo + " worktree add " + outsideTarget + " -b feat/x",
	} {
		if reason := rejectOutsideDeclaredRoots(command, session); reason == "" {
			t.Errorf("%q was ALLOWED — an out-of-root worktree target must stay refused", command)
		}
	}

	// The extractor returns the TARGET, never the -C value (a target read from
	// the wrong token is what the assertions above come down to).
	targets := gitWorktreePathTargets("git -C "+repo+" worktree remove "+wt, session)
	if len(targets) != 1 || targets[0].path != wt {
		t.Fatalf("targets = %+v, want exactly the worktree %s", targets, wt)
	}
	if targets[0].baseDir != repo {
		t.Errorf("the target must carry the -C directory as its base (%q), got %q", repo, targets[0].baseDir)
	}
}

// TestPathPolicyChangeDirCarriesTheTargetBase pins the SHELL half of the run
// directory — the sibling of TestPathPolicyGitChangeDirIsNotTheTarget (git's
// `-C`). A leading `cd <path>` is a shell builtin that runs BEFORE git, so it
// is not part of the git command line at all; git resolves the positional
// operand against the directory the cd selected, while the policy used to
// resolve it against the SESSION workdir. The two were different paths, so the
// guard judged (and refused) a path git would never touch:
//
//	cd /home/user/work/GoPi && git worktree remove ../../workspace/gopi-task
//
// The operand is extracted once (gitWorktreePathTargets) and the base is read
// from the command (commandChangeDir), so the assertion is on the pair — a
// target read correctly from the wrong base is the same defect.
func TestPathPolicyChangeDirCarriesTheTargetBase(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	repo := filepath.Join(t.TempDir(), "repo-outside-every-root")
	wt := filepath.Join(root, "gopi-task")

	// ACTIVE form: the removal the cd was written for. The cd is what makes the
	// (relative and absolute) operands resolvable where git will act on them.
	for _, tc := range []struct{ why, command, wantBase string }{
		{"the cd carry shape", "cd " + repo + " && git worktree remove " + wt, repo},
		{"the cd carry shape with --force", "cd " + repo + " && git worktree remove --force " + wt, repo},
		{"the cd carry shape on creation", "cd " + repo + " && git worktree add " + wt + " -b feat/x", repo},
		{"the POSIX subshell group", "(cd " + repo + " && git worktree remove " + wt + ")", repo},
		{"an initial cd that leads the command", "cd " + repo + " && pwd && git worktree remove " + wt, repo},
		{"a cd leading its own segment (a command word before it)", "cd " + repo + " && git worktree remove " + wt, repo},
		{"a cd whose segment does not lead with the builtin", "echo x cd " + repo + " && git worktree remove " + wt, session},
		{"a cd whose operand resolves through the cd before it", "cd " + repo + " && cd . && git worktree remove " + wt, repo},
	} {
		targets := gitWorktreePathTargets(tc.command, session)
		if len(targets) != 1 || targets[0].path != wt {
			t.Fatalf("%s: targets = %+v, want exactly the worktree %s", tc.why, targets, wt)
		}
		if got := targetBaseDir(targets[0], session); got != tc.wantBase {
			t.Errorf("%s: base = %q, want the cd directory %q", tc.why, got, tc.wantBase)
		}
		if reason := rejectOutsideDeclaredRoots(tc.command, session); reason != "" {
			t.Errorf("%s was REFUSED: %s\nmust not be read as the worktree target", tc.why, reason)
		}
	}

	// PASSIVE directions: a cd that does NOT select the run directory must not
	// contribute a base. A trailing cd is a SIBLING command (its effect never
	// reaches the git invocation) and `cd` with no operand selects HOME, which
	// is not a directory the policy can name. Both leave the session workdir in
	// charge, exactly as before this reader existed.
	for _, tc := range []struct{ why, command, wantBase string }{
		{"a cd that follows the invocation", "git worktree remove " + wt + " && cd " + repo, session},
		{"an operandless cd", "cd && git worktree remove " + wt, session},
		{"a cd to the shell's previous directory", "cd - && git worktree remove " + wt, session},
		{"git's own -C while a cd names a different directory", "cd /tmp && git -C " + repo + " worktree remove " + wt, repo},
	} {
		targets := gitWorktreePathTargets(tc.command, session)
		if len(targets) != 1 {
			t.Fatalf("%s: targets = %+v, want exactly one", tc.why, targets)
		}
		if got := targetBaseDir(targets[0], session); got != tc.wantBase {
			t.Errorf("%s: base = %q, want %q", tc.why, got, tc.wantBase)
		}
	}

	// The base is the directory the CD SELECTED, not the session workdir:
	// asking git for it is the ground truth the reader has to agree with.
	if targets := gitWorktreePathTargets("cd "+repo+" && git worktree remove "+wt, session); len(targets) == 1 {
		if targets[0].baseDir == session {
			t.Errorf("the cd did not contribute a base — the target still resolves against the session workdir %s", session)
		}
	}
}

// TestPathPolicyChangeDirOutsideRootAuthorisesNothing pins the SECOND shape of
// the cd-carry family (Rule 41, measured 2026-10-16 on the live binary): the
// `cd` beam is an AUTHORIZATION, and a cd is a shell builtin anyone can type at
// any directory — so a cd outside every declared root must authorise NOTHING,
// while still resolving the relative operands of the command that follows it.
//
// MEASURED (pre-fix, live binary, this host): with the cd carried as a root, the
// exit statuses and the registration readings split exactly along the presence
// of the leading cd —
//
//	cd <out-of-root> && git -C <repo> worktree add --detach <out-of-root>/wt <rev>  → exit 0, created
//	cd <out-of-root> && git -C <repo> worktree remove --force <out-of-root>/wt     → exit 0, gone
//	git -C <repo> worktree add --detach <out-of-root>/wt <rev>                     → REFUSED
//	git -C <repo> worktree remove --force <out-of-root>/wt                         → REFUSED
//
// — the SAME target, the SAME repository, allowed once the cd is in front. The
// fix makes the cd an authorization only when the directory it selected already
// lies inside a root the operator's configuration declares; the sibling test
// TestPathPolicyChangeDirCarriesTheTargetBase (the RESOLUTION BASE half) must
// stay green, because a relative operand keeps resolving against the cd even
// when the cd authorises nothing.
//
// OUT-OF-ROOT LOCATION: taken from testOutOfRootProbeDir, which asserts the
// directory is inside no declared root before a single case runs (the suite's
// own fixture dirs ARE declared roots here, so a refusal assertion written on
// one of them would pass without the fix).
func TestPathPolicyChangeDirOutsideRootAuthorisesNothing(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	outside := testOutOfRootProbeDir(t)
	repo := filepath.Join(t.TempDir(), "repo-outside-every-root")
	inRoot := filepath.Join(root, "gopi-task")

	// REFUSED — the cd authorises nothing, so the target is judged on its own,
	// exactly as the same command without the cd is judged. Both the shape the
	// briefing measured (git's `-C <repo>`) and the plain shape (no option at
	// all) are asserted: the hole is the cd, not the repository spelling.
	for _, tc := range []struct{ why, command string }{
		{"add, repository named with -C (the measured shape)", "cd " + outside + " && git " + worktreeChangeDirFlag + " " + repo + " worktree add --detach " + filepath.Join(outside, "wt")},
		{"remove --force, repository named with -C (the measured shape)", "cd " + outside + " && git " + worktreeChangeDirFlag + " " + repo + " worktree remove --force " + filepath.Join(outside, "wt")},
		{"add, no repository option", "cd " + outside + " && git worktree add --detach " + filepath.Join(outside, "wt")},
		{"remove --force, no repository option", "cd " + outside + " && git worktree remove --force " + filepath.Join(outside, "wt")},
		{"add, repository named with --git-dir", "cd " + outside + " && git " + gitDirOptionName + "=" + repo + "/.git worktree add --detach " + filepath.Join(outside, "wt")},
		{"a RELATIVE target, resolved against the very cd that authorises nothing", "cd " + outside + " && git worktree remove --force wt"},
		{"a relative target under the repository-naming option", "cd " + outside + " && git " + worktreeChangeDirFlag + " " + repo + " worktree remove --force wt"},
	} {
		if reason := rejectOutsideDeclaredRoots(tc.command, session); reason == "" {
			t.Errorf("%s: %q was ALLOWED — a cd outside every declared root authorises nothing", tc.why, tc.command)
		}
	}

	// THE PREDICATE ITSELF, so a reader can see the beam is withheld rather than
	// inferred from a refusal that some other rule could also have produced.
	configuration := declaredRoots(session)
	if beam := commandChangeDirRoots("cd "+outside+" && git worktree remove --force "+filepath.Join(outside, "wt"), session, configuration); len(beam) != 0 {
		t.Errorf("commandChangeDirRoots carried %+v for a cd OUTSIDE every declared root — the cd must authorise nothing there", beam)
	}
	if beam := commandChangeDirRoots("cd "+root+" && git worktree remove --force "+inRoot, session, configuration); len(beam) != 1 || beam[0].Path != root {
		t.Errorf("commandChangeDirRoots(%q) = %+v, want exactly the in-root cd %s — the beam must survive where the cd really is a declared root", root, beam, root)
	} else if beam[0].Source != worktreeroots.SourceCommandChangeDir {
		t.Errorf("the cd beam must keep naming the source %q, got %q", worktreeroots.SourceCommandChangeDir, beam[0].Source)
	}

	// THE RESOLUTION BASE IS UNTOUCHED — this is the "separate the two halves"
	// requirement, asserted side by side with the refusal above: the same
	// command the policy now REFUSES still resolves its relative operand against
	// the cd directory, because that is the directory git itself will act in.
	targets := gitWorktreePathTargets("cd "+outside+" && git worktree remove --force wt", session)
	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want exactly one", targets)
	}
	if got := targetBaseDir(targets[0], session); got != outside {
		t.Errorf("base = %q, want the cd directory %q — authorisation and resolution are separate questions", got, outside)
	}

	// PERMITTED — the legitimate cleanup the beam exists for: the cd directory
	// IS a declared root, so the beam is granted and the operation inside it goes
	// through. Refusing these is the failure mode that would get the guard
	// switched off.
	for _, tc := range []struct{ why, command string }{
		{"cd into the declared root, target inside it", "cd " + root + " && git worktree remove " + inRoot},
		{"cd into the declared root, absolute target inside it", "cd " + root + " && git worktree add --detach " + inRoot},
		{"cd into the declared root, RELATIVE target inside it", "cd " + root + " && git worktree remove gopi-task"},
		{"cd into the declared root under the repository-naming option", "cd " + root + " && git " + worktreeChangeDirFlag + " " + repo + " worktree remove " + inRoot},
	} {
		if reason := rejectOutsideDeclaredRoots(tc.command, session); reason != "" {
			t.Errorf("%s: %q was REFUSED (%s) — an in-root cd must keep authorising its own tree", tc.why, tc.command, reason)
		}
	}

	// A cd into the SESSION workdir keeps working too (the session workdir is a
	// declared root in its own right): the narrowing is about cds that lie
	// outside every declared root, never about the carry.
	if reason := rejectOutsideDeclaredRoots("cd "+session+" && git worktree remove "+filepath.Join(session, "wt"), session); reason != "" {
		t.Errorf("a cd into the session workdir was REFUSED: %s", reason)
	}
}

// TestPathPolicyChangeDirOutsideRootAuthorisesNothing is the regression guard for
// the cd-carry AUTHORIZATION hole; the RESOLUTION BASE half it must not break is
// asserted inside the same test and by TestPathPolicyChangeDirCarriesTheTargetBase
// below. The policy is exercised through its own pure predicate here; the same
// sequence is driven through the rebuilt binary in the task report (Rule 31).

// testOutOfRootProbeDir returns a directory that NO declared root covers, for
// the tests that must prove a refusal. The tools suite pins TMPDIR to its
// per-run sandbox, and the guard declares that sandbox as a FIXTURE ROOT — so an
// ordinary t.TempDir() is NOT outside the declared roots here, and a refusal
// assertion written on it would test nothing (it would sit inside a permitted
// root while claiming to be outside every one of them).
//
// The directory is taken from the test pin (testutil.ScratchDir) and READ BACK
// through the policy's own reader: the guard's own verdict is quoted in the skip
// message, so a host where the pin moves inside a declared root reports itself
// loudly instead of passing or failing for the wrong reason (Rule 15 — a check
// that found nothing must be distinguishable from one that never ran).
func testOutOfRootProbeDir(t *testing.T) string {
	t.Helper()
	base := vcsRootDir()
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "policy-probe-outside")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("out-of-root probe dir %s: %v", dir, err)
	}
	// The probe dir is deliberately OUTSIDE every declared root, which also
	// means it is outside the trees the harness cleans: left behind, it is an
	// untracked entry in the checkout and a run's worktree must end clean
	// (Rule 30). Nothing is ever created inside it by these tests (the cases
	// that would create are REFUSED), so removing it at the end of the test is
	// unambiguous — and a leftover from an earlier run is cleaned by the same
	// call.
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if inside, declared := worktreeroots.PermitsAny(declaredRoots(baseSessionDir(t)), dir); declared {
		t.Skipf("probe dir %s is inside the declared root %s on this host — an out-of-root assertion here would prove nothing (TMPDIR=%s)", dir, inside.Path, os.TempDir())
	}
	return dir
}

// baseSessionDir is the session directory the guard is asked about when this
// package's own fixture pinning must not apply: the caller's session dir when
// one is exported, otherwise the plain temp root. It deliberately does NOT call
// guardPolicyFixture — that fixture pins TMPDIR, which is exactly the variable
// whose value makes an "out-of-root" probe sit inside a declared root.
func baseSessionDir(t *testing.T) string {
	t.Helper()
	if dir := vcsRootDir(); dir != "" {
		return filepath.Join(dir, "internal", "tools", "testdata", "policy-probe-session")
	}
	return filepath.Join(os.TempDir(), "gopi-policy-probe-session")
}

// vcsRootDir walks up from the test's working directory to the checkout root.
// It exists because the tools suite pins TMPDIR into a sandbox the guard
// DECLARES as a fixture root, so "outside every declared root" cannot be spelled
// with os.TempDir() here — while a checkout (this repository's own tree) is not
// a declared worktree/scratch/temp root and therefore IS a valid out-of-root
// location. Returning "" leaves the caller on the plain temp root.
func vcsRootDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// TestPathPolicyChangeDirNamesNoRootWhenGitIsGivenItsRepository: the cd carry
// authorises the tree the COMMAND RUNS IN, so a cd whose git invocation names
// its OWN repository (`-C <path>` / `--git-dir[=]<path>`) names no tree at all —
// git acts on the repository it was handed, from wherever the caller stood.
//
// REGRESSION GUARD for the hole measured 2026-10-14 (Rule 41): with the cd
// still carried as a root, BOTH of these SUCCEEDED on the live binary while the
// same targets were refused without the leading cd —
//
//	cd <out-of-root> && git --git-dir=<repo>/.git worktree add --detach <out-of-root>/wt <rev>
//	cd <out-of-root> && git --git-dir=<repo>/.git worktree remove --force <out-of-root>/wt
//
// — i.e. a worktree created (and destroyed) at a path no declared root covers.
// The fix is a CONTENT rule, so every spelling is asserted: the separated form,
// the attached form, git's `-C`, and the two shapes that must stay allowed (a cd
// that DID select the tree the operation runs in).
//
// OUT-OF-ROOT LOCATION: the tools suite pins TMPDIR to its per-run sandbox, which
// the guard declares as a fixture root — so an ordinary t.TempDir() is NOT
// "outside every declared root" here and an assertion written on it would pass
// without the fix. The probe root is taken from the test pin (testutil.ScratchRoot)
// and asserted to be inside no declared root before a single case runs.
func TestPathPolicyChangeDirNamesNoRootWhenGitIsGivenItsRepository(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	outside := testOutOfRootProbeDir(t)
	repo := filepath.Join(t.TempDir(), "repo-outside-every-root")
	inRoot := filepath.Join(root, "gopi-task")
	outsideWT := filepath.Join(outside, "wt")

	// REFUSED: the cd must not authorise a target, because the repository was
	// named explicitly and the cd therefore selected nothing for git.
	for _, command := range []string{
		"cd " + outside + " && git -C " + repo + " worktree add --detach " + outsideWT,
		"cd " + outside + " && git -C " + repo + " worktree add " + outsideWT + " -b feat/x",
		"cd " + outside + " && git " + gitDirOptionName + "=" + repo + "/.git worktree add --detach " + outsideWT,
		"cd " + outside + " && git " + gitDirOptionName + "=" + repo + "/.git worktree remove --force " + outsideWT,
		"cd " + outside + " && git " + gitDirOptionName + " " + repo + "/.git worktree remove --force " + outsideWT,
	} {
		if reason := rejectOutsideDeclaredRoots(command, session); reason == "" {
			t.Errorf("%q was ALLOWED — a repository-naming option takes the cd's authority away and the target must be judged on its own", command)
		}
	}

	// ALLOWED: the cd selected the directory the operation runs in (git resolves
	// a RELATIVE repository against it, exactly as the shell does), and the
	// target is inside that tree. Refusing these would break the cleanup an
	// agent writes after itself — the beam the cd carry exists for.
	for _, command := range []string{
		"cd " + root + " && git -C " + root + " worktree add --detach " + inRoot + " -b feat/x",
		"cd " + root + " && git " + gitDirOptionName + "=" + root + "/.git worktree remove " + inRoot,
	} {
		if reason := rejectOutsideDeclaredRoots(command, session); reason != "" {
			t.Errorf("%q was REFUSED (%s) — the cd DID select the tree the operation runs in", command, reason)
		}
	}

	// The fix narrows the beam rather than disabling it: a repository-naming
	// option removes the cd's authority, so a target that lies in the DECLARED
	// root is still permitted (the refusal must name the TARGET, not the shape).
	if reason := rejectOutsideDeclaredRoots("cd "+outside+" && git -C "+repo+" worktree remove "+inRoot, session); reason != "" {
		t.Errorf("an IN-ROOT target under a repository-naming option was REFUSED (%s) — the option removes the cd's authority, not the declared root's", reason)
	}
	if reason := rejectOutsideDeclaredRoots("cd "+outside+" && git -C "+repo+" worktree remove "+outsideWT, session); reason == "" {
		t.Errorf("an out-of-root target under a repository-naming option was ALLOWED")
	}
}

// TestPathPolicyChangeDirInsideRootIsNotRefused: the cd carry must not turn a
// legitimate cleanup into a refusal. The cd moves OFF the session workdir into
// another tree a declared root already covers, and the relative operand resolves
// against that cd — refusing it would break the "cd <my tree> && git worktree
// remove <wt>" an agent writes when it cleans up after itself.
//
// The cd directory must itself be inside a declared root: a cd is an
// AUTHORIZATION only where the operator's configuration already reaches (see
// TestPathPolicyChangeDirOutsideRootAuthorisesNothing). The declared ROOT is used
// here — not the suite's temp fixture, which is a declared root only on hosts
// where TMPDIR happens to sit under one, so the case would silently assert the
// opposite of its name.
func TestPathPolicyChangeDirInsideRootIsNotRefused(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	command := "cd " + root + " && git worktree remove wt"
	if reason := rejectOutsideDeclaredRoots(command, session); reason != "" {
		t.Fatalf("a cd-carried removal inside the directory the cd selected was REFUSED: %s", reason)
	}
	targets := gitWorktreePathTargets(command, session)
	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want exactly one", targets)
	}
	if got := targetBaseDir(targets[0], session); got != root {
		t.Errorf("base = %q, want the cd directory %q", got, root)
	}
	// The judged path is the one git will act on: root/wt, not session/wt.
	judged, ok := absolutize(targets[0].path, targetBaseDir(targets[0], session), nil)
	if !ok || judged != filepath.Join(root, targets[0].path) {
		t.Errorf("judged path = %q (ok=%v), want %q", judged, ok, filepath.Join(root, targets[0].path))
	}
}

// TestPathPolicyChangeDirStillRefusesOutOfRootTarget: the cd carry ADDS a beam,
// it does not remove the property. A target that is inside neither the session
// workdir nor the cd directory is refused, and the refusal names the resolved
// target so the reader can see the path the policy actually judged.
func TestPathPolicyChangeDirStillRefusesOutOfRootTarget(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	repo := filepath.Join(t.TempDir(), "repo-outside-every-root")
	// A path outside EVERY declared root: neither the session workdir, nor the
	// cd directory, nor the declared worktree root, nor the scratch/temp roots.
	outside := filepath.Join(worktreeroots.ShmDir, "gopi-guard-elsewhere", "gopi-someone-else")
	reason := rejectOutsideDeclaredRoots("cd "+repo+" && git worktree remove "+outside, session)
	if reason == "" {
		t.Fatalf("a cd-carried removal of %s was ALLOWED — the cd carry must not relax the property", outside)
	}
	if !strings.Contains(reason, outside) {
		t.Errorf("the refusal must name the target %s it judged: %s", outside, reason)
	}
	if !strings.Contains(reason, root) {
		t.Errorf("the refusal must print the roots it consulted, including the declared root %s: %s", root, reason)
	}
	// …and the same target is refused identically when the cd moves the base
	// INSIDE a root: the cd never widens the judgement to paths outside it.
	reason2 := rejectOutsideDeclaredRoots("cd "+root+" && git worktree remove "+outside, session)
	if reason2 == "" {
		t.Errorf("a cd into the declared root must not allow an out-of-root target: %s", outside)
	}
}

// TestBashEnvironmentRefusesOutOfRootWorktree drives the REAL entry point (the
// bash tool) rather than the predicate, so the wiring is proven and not just the
// policy function: the refusal must reach the caller as an error and no
// directory may appear.
func TestBashEnvironmentRefusesOutOfRootWorktree(t *testing.T) {
	_, session, _ := guardPolicyFixture(t)
	target := worktreeroots.ShmDir + "/gopi-guard-test-must-not-exist"
	if _, err := os.Stat(target); err == nil {
		t.Skipf("%s already exists — refusing to run a destructive-shape probe against real host state", target)
	}
	_, err := BashInDir("git worktree add "+target+" -b feat/x", session)
	if err == nil {
		t.Fatalf("bash ran `git worktree add %s` — the guard is not wired into the bash entry point", target)
	}
	if !strings.Contains(err.Error(), "declared root") {
		t.Errorf("error does not name the policy: %v", err)
	}
	if _, serr := os.Stat(target); serr == nil {
		t.Fatalf("%s was created despite the refusal", target)
	}

	// The same wiring probe for the UNVERIFIABLE spelling: a `git worktree add`
	// whose target is a shell variable must be refused by the ENTRY POINT too —
	// the predicate is what the fix added, and this is the case that measured
	// the asymmetry (a variable naming an out-of-root path was allowed while
	// its literal spelling was refused).
	variable := "V=" + worktreeroots.ShmDir + "/gopi-var-probe; git worktree add --detach \"$V\""
	_, verr := BashInDir(variable, session)
	if verr == nil {
		t.Fatalf("bash ran %q — an unverifiable worktree target reached the shell", variable)
	}
	if !strings.Contains(verr.Error(), "shell variable/command substitution") {
		t.Errorf("error does not name the unverifiable token: %v", verr)
	}
}

// TestBashEnvironmentAllowsInRootWorktree is the same wiring probe from the
// allowed side: the guard must not break a legitimate worktree, which is the
// failure mode that would get it switched off. It runs a REAL git worktree in a
// fixture repo under a declared root.
func TestBashEnvironmentAllowsInRootWorktree(t *testing.T) {
	root, _, _ := guardPolicyFixture(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := BashInDir("git init -q && git config user.email t@t && git config user.name t && git commit -q --allow-empty -m seed", repo); err != nil {
		t.Skipf("git unavailable in this fixture: %v (%s)", err, out)
	}
	target := filepath.Join(root, "gopi-allowed-wt")
	if out, err := BashInDir("git worktree add "+target+" -b feat/allowed", repo); err != nil {
		t.Fatalf("a legitimate in-root worktree was REFUSED: %v (%s)", err, out)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the worktree was not created at %s: %v", target, err)
	}
}

// TestPathPolicyWorktreeMoveJudgesBothOperands is the regression guard for the
// measured hole: `git worktree move` was not a shape this arm recognised at all
// (`gitWorktreePathTargets` read `add|remove` only), so a move whose DESTINATION
// lay outside every declared root never entered the arm — it was not refused and,
// more quietly, not even LOOKED AT (measured: the move SUCCEEDED, DEST=EXISTS),
// while the same destination spelled through `add` was refused. A shape the
// policy does not read is a shape it does not guard.
//
// A move has TWO positional operands, and they are two DIFFERENT questions:
//
//   - the SOURCE is a directory that exists and is being relocated, so it is
//     judged by the declared-root property only (applying the fresh-path
//     predicate to it would refuse every move — it exists by definition);
//   - the DESTINATION is a path being CREATED at a location git chose, so it is
//     judged by BOTH the declared-root property and the fresh-path predicate,
//     exactly as `worktree add`'s operand is.
//
// Both are read from ARGUMENT ORDER (a flag value is never a path), both carry
// the same base directory (git's `-C` value, else the `cd` carry, else the
// session workdir — the directory git itself resolves a relative spelling
// against).
func TestPathPolicyWorktreeMoveJudgesBothOperands(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	src := filepath.Join(root, "gopi-src")
	inRootDest := filepath.Join(root, "gopi-moved")
	// Outside EVERY declared root — the same out-of-root location the sibling
	// refusal tests use (Rule 22: a temp path is judged by the root measurement,
	// never by its spelling).
	outside := filepath.Join(worktreeroots.ShmDir, "gopi-guard-elsewhere", "gopi-moved")

	// REFUSED — the destination, the source, and the base-carrying spellings all
	// clear the same bar as `add`/`remove`.
	for _, tc := range []struct {
		why     string
		command string
		named   string
	}{
		{"an out-of-root DESTINATION (the measured hole)", "git worktree move " + src + " " + outside, outside},
		{"an out-of-root DESTINATION through git's own -C", "git -C " + session + " worktree move " + src + " " + outside, outside},
		{"an out-of-root DESTINATION carried by a cd", "cd " + session + " && git worktree move " + src + " " + outside, outside},
		{"an out-of-root SOURCE (relocating someone else's tree into a root)", "git worktree move " + outside + " " + inRootDest, outside},
		{"an out-of-root DESTINATION with a flag before the operands", "git worktree move --force " + src + " " + outside, outside},
	} {
		reason := rejectOutsideDeclaredRoots(tc.command, session)
		if reason == "" {
			t.Errorf("%s: %q was ALLOWED — a worktree move is judged by the same property as add/remove", tc.why, tc.command)
			continue
		}
		if !strings.Contains(reason, tc.named) {
			t.Errorf("%s: the refusal must name the operand it judged (%q): %s", tc.why, tc.named, reason)
		}
	}

	// ALLOWED — a move entirely inside a declared root is the operation this
	// guard must keep permitting (relocating a worktree is ordinary work).
	for _, tc := range []struct {
		why     string
		command string
	}{
		{"absolute operands inside the declared root", "git worktree move " + src + " " + inRootDest},
		{"relative operands resolved against git's -C directory", "git -C " + root + " worktree move gopi-src gopi-moved"},
		{"relative operands resolved against the cd carry", "cd " + root + " && git worktree move gopi-src gopi-moved"},
		{"a boolean flag before the operands", "git worktree move --force " + src + " " + inRootDest},
	} {
		if reason := rejectOutsideDeclaredRoots(tc.command, session); reason != "" {
			t.Errorf("%s: %q was REFUSED (%s) — the move arm must not be a blanket ban", tc.why, tc.command, reason)
		}
	}

	// The extraction itself: TWO targets, in argument order, with the DESTINATION
	// flagged as a creation and the SOURCE not (the flag is what selects the
	// fresh-path predicate, so pairing them wrongly would refuse every move).
	for _, tc := range []struct{ why, command, wantBase string }{
		{"the plain shape", "git worktree move " + src + " " + inRootDest, session},
		{"git's -C as the base", "git -C " + root + " worktree move gopi-src gopi-moved", root},
		{"the cd carry as the base", "cd " + root + " && git worktree move gopi-src gopi-moved", root},
	} {
		targets := gitWorktreePathTargets(tc.command, session)
		if len(targets) != 2 {
			t.Fatalf("%s: gitWorktreePathTargets(%q) = %+v, want exactly two operands", tc.why, tc.command, targets)
		}
		if got := targetBaseDir(targets[0], session); got != tc.wantBase {
			t.Errorf("%s: source base = %q, want %q", tc.why, got, tc.wantBase)
		}
		if got := targetBaseDir(targets[1], session); got != tc.wantBase {
			t.Errorf("%s: destination base = %q, want %q", tc.why, got, tc.wantBase)
		}
		if targets[0].create {
			t.Errorf("%s: the SOURCE was flagged as a creation — a move relocates a directory that exists", tc.why)
		}
		if !targets[1].create {
			t.Errorf("%s: the DESTINATION was not flagged as a creation — it is the path being created", tc.why)
		}
	}

	// A DESTINATION that already exists is refused by the fresh-path predicate,
	// the same as `add`: a move onto an occupied path is the shared-path hazard
	// (and git's own behaviour there is what the predicate exists to make
	// explicit rather than silent).
	if err := os.MkdirAll(inRootDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if reason := rejectOutsideDeclaredRoots("git worktree move "+src+" "+inRootDest, session); reason == "" {
		t.Error("a move onto an ALREADY OCCUPIED destination was allowed")
	}
}
