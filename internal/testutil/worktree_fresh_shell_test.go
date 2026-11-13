package testutil

// worktree_fresh_shell_test.go — the SHELL half of the fresh-path guard
// (scripts/lib/worktree-fresh.sh) is tested, not only written.
//
// WHY A SHELL TEST EXISTS AT ALL. The Go predicate
// (internal/tools/worktree_fresh_path.go) guards the Go creators; the fixture
// and acceptance scripts call `git worktree add` themselves and can never reach
// a Go helper. A shell contract nobody executes is prose — the same failure the
// baseline contract (scripts/lib/baseline-verify.sh) is covered against in
// baseline_gate_test.go. So this drives the REAL sourced helper in a REAL bash
// and asserts both directions, including the case git itself lets through (an
// existing EMPTY directory).
//
// It also pins the ONE thing a shell guard gets wrong here: `[ -e path ]` FOLLOWS
// a symlink, so a DANGLING link reads as absent and the checkout would be written
// THROUGH the link. The helper probes `-e || -L`, and the test proves it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeFreshShellHelper is the shell contract's repo-root-relative path
// (Rule 1b: one spelling, referenced by the test that drives it).
const worktreeFreshShellHelper = "scripts/lib/worktree-fresh.sh"

// runWorktreeFreshShell sources the helper and calls
// gopi_assert_worktree_path_fresh on path, returning combined output and err.
func runWorktreeFreshShell(t *testing.T, path string) (string, error) {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	helper := filepath.Join(root, filepath.FromSlash(worktreeFreshShellHelper))
	if _, err := os.Stat(helper); err != nil {
		t.Fatalf("the shell half of the fresh-path guard is missing: %v", err)
	}
	cmd := exec.Command("bash", "-c",
		`. "$1" && gopi_assert_worktree_path_fresh "$2"`, "bash", helper, path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestWorktreeFreshShellRefusesExistingEmptyDirectory is the shell RED case:
// an existing EMPTY directory is the occupant `git worktree add` silently
// reuses, so the helper must refuse it by name.
func TestWorktreeFreshShellRefusesExistingEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "wt-empty")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runWorktreeFreshShell(t, target)
	if err == nil {
		t.Fatalf("the helper accepted an existing EMPTY directory: %s", out)
	}
	if !strings.Contains(out, target) {
		t.Errorf("the refusal must name the path: %s", out)
	}
	if !strings.Contains(out, "already exists there") {
		t.Errorf("the refusal must say the path is occupied: %s", out)
	}
	// The guard refuses; it never removes. A helper that "made room" would be
	// destroying whatever the directory was.
	if _, serr := os.Lstat(target); serr != nil {
		t.Fatalf("the helper removed the pre-existing directory: %v", serr)
	}
}

// TestWorktreeFreshShellRefusesDanglingSymlink pins the `-L` half: os-level
// `[ -e ]` cannot see a link whose target is gone.
func TestWorktreeFreshShellRefusesDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "wt-link")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), target); err != nil {
		t.Skipf("symlinks unavailable in this fixture: %v", err)
	}
	out, err := runWorktreeFreshShell(t, target)
	if err == nil {
		t.Fatalf("the helper accepted a DANGLING symlink ([ -e ] follows links): %s", out)
	}
	if !strings.Contains(out, "symbolic link") {
		t.Errorf("the refusal must report the link: %s", out)
	}
}

// TestWorktreeFreshShellAllowsAbsentPath is the anti-blanket-ban probe without
// which "refuse everything" would pass every test above.
func TestWorktreeFreshShellAllowsAbsentPath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "wt-absent")
	out, err := runWorktreeFreshShell(t, target)
	if err != nil {
		t.Fatalf("an ABSENT path must be fresh: %v (%s)", err, out)
	}
}

// TestWorktreeFreshShellIsSourcedByEveryCreator pins the WIRING, not the helper:
// a contract that no script sources is the defect this whole epic exists for. The
// scan finds every script that runs `git worktree add` and requires each to
// source the helper and call it on the target it is about to create.
//
// TWO ROOTS, measured: the first version of this audit walked `scripts/` only,
// and the very next read of the tree found THREE more creators under `devdoc/`
// (demo-park-worktree-release.sh, wip-worktree-guard-e2e.sh,
// worktree-cwd-release-coverage/run.sh) — all of them hand-rolling
// `git worktree add`. An audit scoped to the directory someone happened to think
// of is how a guard stays partial; the root list below is the fix.
//
// The check is deliberately source-form based (the `.`/`source` line plus the
// helper name), and it carries a READ FLOOR so a broken walk cannot pass
// vacuously.
func TestWorktreeFreshShellIsSourcedByEveryCreator(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	roots := []string{"scripts", "devdoc"}
	var scanned, creators int
	var exempt []string
	for _, r := range roots {
		dir := filepath.Join(root, r)
		if _, serr := os.Stat(dir); serr != nil {
			t.Fatalf("the audit root %s is missing: %v", r, serr)
		}
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".sh") {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			body := string(raw)
			rel, _ := filepath.Rel(root, path) //stylegate:ignore path arithmetic in a fixture; the compared string is the assertion
			scanned++
			if !createsWorktree(body) {
				return nil
			}
			creators++
			if exemptsWorktreeFresh(body) {
				// A REVIEWED EXCEPTION, cited in the script itself with its reason
				// (`worktree-fresh-exempt: <why>`). It is reported by the test
				// below so the exemption stays VISIBLE rather than silent.
				exempt = append(exempt, rel)
				return nil
			}
			if !sourcesWorktreeFresh(body) || !callsWorktreeFresh(body) {
				t.Errorf("%s runs `git worktree add` without proving the path is free first.\n"+
					"  FIX: source %s and call `gopi_assert_worktree_path_fresh <the-path>` before the add.\n"+
					"  WHY: git refuses a NON-empty target and silently REUSES an EMPTY one, so a leftover\n"+
					"  directory becomes the new worktree's home — and a worktree's identity is its path.",
					rel, worktreeFreshShellHelper)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s/: %v", r, err)
		}
	}
	if scanned == 0 {
		t.Fatalf("the audit read 0 scripts under %v — the walk is not looking where the scripts live (vacuous pass guard)", roots)
	}
	if creators == 0 {
		t.Fatalf("the audit found 0 worktree creators among %d scripts — the creator matcher is broken (vacuous pass guard)", scanned)
	}
	// The exemptions are REPORTED, never silent: a reader of this test's output
	// sees every creator that deliberately does not carry the guard, and the
	// script itself carries the reason.
	for _, rel := range exempt {
		t.Logf("reviewed exemption: %s proves its own precondition instead of calling the helper (see its `worktree-fresh-exempt:` note)", rel)
	}
	t.Logf("the audit read %d script(s) across %v and found %d worktree creator(s)", scanned, roots, creators)
}

// exemptsWorktreeFresh reports whether a script carries the reviewed-exemption
// marker. The marker is a SOURCE line (never a bare mention in prose in a way
// the reader cannot find), and it must name a reason — an empty reason is not an
// exemption, it is a silent skip.
func exemptsWorktreeFresh(body string) bool {
	const marker = "worktree-fresh-exempt:"
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if !strings.Contains(trimmed, marker) {
			continue
		}
		reason := strings.TrimSpace(strings.SplitN(trimmed, marker, 2)[1])
		if reason != "" {
			return true
		}
	}
	return false
}

// createsWorktree reports whether a script body runs a `git … worktree add`
// invocation. Comment lines are skipped, so a script that only MENTIONS the verb
// (in its own rationale, as this test's own docs do) is not a creator.
func createsWorktree(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "worktree add") {
			return true
		}
	}
	return false
}

// sourcesWorktreeFreshFile is the file-name half of the source-form check.
func sourcesWorktreeFreshFile(body string) bool {
	return strings.Contains(body, filepath.Base(worktreeFreshShellHelper))
}

// sourcesWorktreeFresh reports whether a script (or a helper it sources) brings
// the guard into scope. The check is on the SOURCE FORM (`.`/`source` + the
// file), never on a bare mention of the name — and it accepts the one level of
// INDIRECTION the repo actually uses: scripts/lib/baseline-verify.sh sources the
// guard itself (so the base helper is self-contained), and a script that sources
// baseline-verify inherits it. Refusing that reading would force a second, purely
// cosmetic source line into every caller, which is how a rule becomes noise.
func sourcesWorktreeFresh(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, ". ") && !strings.HasPrefix(trimmed, "source ") {
			continue
		}
		if sourcesWorktreeFreshFile(trimmed) || strings.Contains(trimmed, baselineContractFile) {
			return true
		}
	}
	return false
}

// callsWorktreeFresh reports whether a script CALLS the assert helper.
func callsWorktreeFresh(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "gopi_assert_worktree_path_fresh ") {
			return true
		}
	}
	return false
}
