package agent

// projectctx_twolevel_test.go — the regression proof for the TWO-LEVEL project
// context (epic: system-level GOPI.md).
//
// THE DEFECT (measured 2026-10-19): GOPI.md is gitignored by design, so a bare
// clone, a worker's worktree or a sandbox profile starts with NO project rules
// and the loader said so only once, in one sentence that read like a normal
// state. The fix is a second, PROFILE-level file resolved through
// config.GopiDir(), merged deterministically with the checkout-level file.
//
// WHAT THESE TESTS PIN, one arm each:
//
//	both present      → system block FIRST, repository second, fixed order
//	only system       → the system block, and the repository ABSENCE named
//	only repository   → today's behaviour, and the system ABSENCE named
//	neither           → "", and TWO DISTINCT absence lines
//	same input twice  → bytes.Equal (prompt-cache byte stability)
//	unreadable file   → treated as absent, with a warning naming the reason
//
// Nothing is mocked: real directories, real files, real bytes, and the profile
// location is redirected through GOPI_HOME — the same seam every other test in
// this package uses — so the resolver is proved to go through
// config.GopiDir() rather than a hardcoded home (Rule 5: every fix ships a
// minimal repro).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

const (
	// twoLevelSystemBody / twoLevelRepoBody are the marker bodies. They differ
	// so an assertion can name WHICH document it found by content alone.
	twoLevelSystemBody = "# System rules fixture\n\n- SYSTEM-MARKER-91: prefer the profile's own rules.\n"
	twoLevelRepoBody   = "# Repository rules fixture\n\n- REPO-MARKER-42: this checkout's rules.\n"
)

// twoLevelProfile points the process at a private gopi profile and returns its
// directory. GOPI_HOME is the documented mechanism, so the profile level is
// resolved by the SAME code a real run uses — a hardcoded ~/.gopi could not
// pass this test on a host that has one (this host does).
func twoLevelProfile(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir, err := config.GopiDir()
	if err != nil {
		t.Fatalf("config.GopiDir(): %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir profile %s: %v", dir, err)
	}
	return dir
}

// twoLevelTree builds a checkout-shaped directory whose upward walk is BOUNDED
// by a `.git` directory, so the repository level's result never depends on what
// this host happens to keep above the temp dir.
func twoLevelTree(t *testing.T) string {
	t.Helper()
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, gitDirEntryName), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return top
}

// writeTwoLevelFile writes body at dir/<name>.
func writeTwoLevelFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestTwoLevelContextBothLevelsSystemFirst is the headline arm: with both files
// on disk the merged context carries BOTH bodies, the system one first, each
// under its own heading.
func TestTwoLevelContextBothLevelsSystemFirst(t *testing.T) {
	profile := twoLevelProfile(t)
	sysPath := writeTwoLevelFile(t, profile, localProjectContextFileName, twoLevelSystemBody)
	repo := twoLevelTree(t)
	repoPath := writeTwoLevelFile(t, repo, projectContextFileName, twoLevelRepoBody)

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	sysAt := strings.Index(ctx, strings.TrimSpace(twoLevelSystemBody))
	repoAt := strings.Index(ctx, strings.TrimSpace(twoLevelRepoBody))
	if sysAt < 0 || repoAt < 0 {
		t.Fatalf("the merged context does not carry both levels:\n%s", ctx)
	}
	if sysAt > repoAt {
		t.Fatalf("the system block came AFTER the repository block (sys@%d repo@%d) — the order is fixed system→repository:\n%s",
			sysAt, repoAt, ctx)
	}
	// The heading names the document that was LOADED (the profile's own local
	// file), not the tracked default: a reader greps the name it will find.
	wantSystemHeading := systemHeading(localProjectContextFileName)
	if !strings.HasPrefix(ctx, wantSystemHeading) {
		t.Fatalf("the merged context does not open with the system heading %q:\n%s", wantSystemHeading, ctx)
	}
	if !strings.Contains(ctx, repositoryProjectContextHeading) {
		t.Fatalf("the merged context carries no repository heading %q — the two documents are blended:\n%s",
			repositoryProjectContextHeading, ctx)
	}
	// One loaded line per level, each naming its own path.
	for _, want := range []string{
		"[system] — from " + sysPath,
		"[repository] — from " + repoPath,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("stderr does not distinguish the level with %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "no ") && strings.Contains(msg, "found —") {
		t.Fatalf("both levels loaded, yet stderr reports an absence:\n%s", msg)
	}
}

// TestTwoLevelContextOnlySystem: the profile file alone must still inject, and
// the missing checkout file must be reported as the REPOSITORY absence.
func TestTwoLevelContextOnlySystem(t *testing.T) {
	profile := twoLevelProfile(t)
	writeTwoLevelFile(t, profile, localProjectContextFileName, twoLevelSystemBody)
	repo := twoLevelTree(t)

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if !strings.Contains(ctx, strings.TrimSpace(twoLevelSystemBody)) {
		t.Fatalf("the system level was not injected when it is the only level present:\n%s", ctx)
	}
	if strings.Contains(ctx, repositoryProjectContextHeading) {
		t.Fatalf("an absent repository level contributed a heading:\n%s", ctx)
	}
	if !strings.Contains(msg, "no repository rules file (tried "+coreNameList()+") — searched:") {
		t.Fatalf("stderr does not report the REPOSITORY level missing:\n%s", msg)
	}
	if strings.Contains(msg, "no system rules file") {
		t.Fatalf("stderr claims the system level is missing while it loaded:\n%s", msg)
	}
}

// TestTwoLevelContextOnlyRepository: today's behaviour — the checkout file is
// still injected — plus the distinct SYSTEM absence line.
func TestTwoLevelContextOnlyRepository(t *testing.T) {
	twoLevelProfile(t) // a profile that carries no rules file
	repo := twoLevelTree(t)
	writeTwoLevelFile(t, repo, projectContextFileName, twoLevelRepoBody)

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if !strings.Contains(ctx, strings.TrimSpace(twoLevelRepoBody)) {
		t.Fatalf("the repository level was not injected when it is the only level present:\n%s", ctx)
	}
	if !strings.Contains(msg, "no system rules file") {
		t.Fatalf("stderr does not report the SYSTEM level missing:\n%s", msg)
	}
	if strings.Contains(msg, "no repository rules file") {
		t.Fatalf("stderr claims the repository level is missing while it loaded:\n%s", msg)
	}
}

// TestTwoLevelContextNeitherIsEmptyAndBothAbsencesAreDistinct: no file anywhere
// → empty context, no failure, and the two missing cases carry DIFFERENT text
// (one sentence for both is what made a rule-less run look normal).
func TestTwoLevelContextNeitherIsEmptyAndBothAbsencesAreDistinct(t *testing.T) {
	twoLevelProfile(t)
	repo := twoLevelTree(t)

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if ctx != "" {
		t.Fatalf("neither level present returned %q, want the empty context (absence is optional, never fatal)", ctx)
	}
	sysLine, repoLine := "", ""
	for _, line := range strings.Split(msg, "\n") {
		if strings.Contains(line, "no system rules file") {
			sysLine = line
		}
		if strings.Contains(line, "no repository rules file") {
			repoLine = line
		}
	}
	if sysLine == "" || repoLine == "" {
		t.Fatalf("the two missing cases are not both reported:\n%s", msg)
	}
	if sysLine == repoLine {
		t.Fatalf("both absences print the SAME line %q — the operator cannot tell which level to fix", sysLine)
	}
	if strings.Contains(msg, "warning") {
		t.Fatalf("a plain absence must not warn (optional stays optional):\n%s", msg)
	}
}

// TestTwoLevelContextIsByteDeterministic: the same inputs twice produce the
// same BYTES. This is the prompt-cache invariant — no timestamp, no hostname,
// no map order may reach the assembled context.
func TestTwoLevelContextIsByteDeterministic(t *testing.T) {
	profile := twoLevelProfile(t)
	writeTwoLevelFile(t, profile, localProjectContextFileName, twoLevelSystemBody)
	repo := twoLevelTree(t)
	writeTwoLevelFile(t, repo, projectContextFileName, twoLevelRepoBody)

	read := captureStderr(t)
	first := LoadProjectContext(repo)
	second := LoadProjectContext(repo)
	read()

	if !bytes.Equal([]byte(first), []byte(second)) {
		t.Fatalf("two calls with identical inputs differ:\nfirst:  %q\nsecond: %q", first, second)
	}
	// The pure merge is deterministic on its own too, so the property does not
	// rest on the file system being unchanged.
	if a, b := mergeProjectContext(twoLevelSystemBody, twoLevelRepoBody),
		mergeProjectContext(twoLevelSystemBody, twoLevelRepoBody); !bytes.Equal([]byte(a), []byte(b)) {
		t.Fatalf("mergeProjectContext is not deterministic:\n%q\n%q", a, b)
	}
	// And the empty/edge shapes: an absent level must not leak a stray heading
	// or separator into the bytes.
	if got := mergeProjectContext("", ""); got != "" {
		t.Fatalf("merging two absent levels produced %q, want the empty string", got)
	}
	if got := mergeProjectContext(twoLevelSystemBody, ""); strings.Contains(got, repositoryProjectContextHeading) {
		t.Fatalf("a system-only merge emitted the repository heading:\n%s", got)
	}
}

// TestTwoLevelSystemUnreadableIsAbsentWithWarning: a profile file that exists
// but cannot be read must not be mistaken for no file — and must not fail the
// run either.
func TestTwoLevelSystemUnreadableIsAbsentWithWarning(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny the read, so this arm cannot be measured")
	}
	profile := twoLevelProfile(t)
	p := writeTwoLevelFile(t, profile, localProjectContextFileName, twoLevelSystemBody)
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", p, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Logf("restore mode on %s: %v", p, err)
		}
	})
	repo := twoLevelTree(t)
	writeTwoLevelFile(t, repo, projectContextFileName, twoLevelRepoBody)

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if strings.Contains(ctx, "SYSTEM-MARKER-91") {
		t.Fatalf("an unreadable system file contributed its body:\n%s", ctx)
	}
	if !strings.Contains(ctx, strings.TrimSpace(twoLevelRepoBody)) {
		t.Fatalf("an unreadable system file took the repository level down with it:\n%s", ctx)
	}
	if !strings.Contains(msg, "warning: system "+p+" could not be read") {
		t.Fatalf("stderr does not name the unreadable path and its remedy:\n%s", msg)
	}
}

// TestTwoLevelSystemPathGoesThroughGopiDir pins the resolution seam: the system
// path must follow GOPI_HOME (config.GopiDir()), never a hardcoded ~/.gopi and
// never a shell-built path. Two different home values must resolve to two
// different files.
func TestTwoLevelSystemPathGoesThroughGopiDir(t *testing.T) {
	homes := []string{t.TempDir(), t.TempDir()}
	var paths []string
	for _, home := range homes {
		t.Setenv(config.GopiDirEnv, home)
		dir, err := config.GopiDir()
		if err != nil {
			t.Fatalf("config.GopiDir(): %v", err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		p, ok := systemProjectContextPath()
		if !ok {
			t.Fatalf("systemProjectContextPath() refused a resolvable profile (%s)", home)
		}
		if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			t.Fatalf("system path %q is not under the resolved profile %q — the join bypassed config.GopiDir()", p, dir)
		}
		if filepath.Base(p) != localProjectContextFileName {
			t.Fatalf("system path %q does not end in %q — the PROFILE level is this machine's own policy (localProjectContextFileName), not the tracked contract",
				p, localProjectContextFileName)
		}
		paths = append(paths, p)
	}
	if paths[0] == paths[1] {
		t.Fatalf("both GOPI_HOME values resolved to the same system path %q — the home is not consulted", paths[0])
	}
}

// TestTwoLevelUnresolvedProfileSaysWhy: when the profile directory itself cannot
// be resolved (no HOME, or a refused nested GOPI_HOME), the system level is
// absent for a DIFFERENT reason than "the profile carries no rules file" — the
// run prints that reason instead of a bare absence line, and the repository
// level still loads.
func TestTwoLevelUnresolvedProfileSaysWhy(t *testing.T) {
	// GOPI_HOME cleared and HOME cleared: config.GopiDir() then has no home to
	// resolve and returns its error, which is the "profile unavailable" case
	// (a test sandbox with no real HOME is exactly the shape config documents).
	t.Setenv(config.GopiDirEnv, "")
	t.Setenv("HOME", "")
	repo := twoLevelTree(t)
	writeTwoLevelFile(t, repo, projectContextFileName, twoLevelRepoBody)

	read := captureStderr(t)
	ctx := LoadProjectContext(repo)
	msg := read()

	if !strings.Contains(msg, "system "+localProjectContextFileName+" unavailable: the gopi profile directory could not be resolved") {
		t.Fatalf("stderr does not say WHY the system level is unavailable:\n%s", msg)
	}
	if strings.Contains(msg, "no system rules file") {
		t.Fatalf("an unresolvable profile was reported as an absent file — two different remedies, one sentence:\n%s", msg)
	}
	if !strings.Contains(ctx, strings.TrimSpace(twoLevelRepoBody)) {
		t.Fatalf("an unresolvable profile took the repository level down with it:\n%s", ctx)
	}
}
