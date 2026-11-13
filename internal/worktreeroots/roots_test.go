package worktreeroots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultRootIsWorkspaceUnderHome pins the ONE default: <home>/workspace.
// It is the path GOPI.md has documented since the beginning, so the default
// must not drift with a refactor.
//
// BEHAVIOUR CHANGE (2026-10-01, epic test-worktree-root-leak): inside a TEST
// PROCESS the default is the suite's pinned root instead (see
// pin_abstention_test.go / scratch_pin_test.go), because resolving
// <home>/workspace in a suite is exactly how 174 orphan stubs landed in the
// operator's real workspace. This test therefore asks for the UNPINNED
// computation explicitly — the pin's own no-redirect case, which is how the
// <home>/workspace shape stays covered — rather than asserting a value a test
// process can no longer obtain by accident.
func TestDefaultRootIsWorkspaceUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvName, home)
	restore := pinNothingForTest(t)

	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot: %v", err)
	}
	want := filepath.Join(home, WorkspaceDirName)
	if root != want {
		t.Fatalf("DefaultRoot() = %q, want %q", root, want)
	}
	restore()
}

// TestResolvePrecedence pins env > declared > default. Each layer is a distinct
// authorisation source, and the refusal text depends on knowing which one won.
func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvName, home)
	declared := filepath.Join(t.TempDir(), "declared-root")

	// 1. No env, no declaration → the default, sourced from the default layer.
	// UNPINNED on purpose: the default layer's value is <home>/workspace, and in a
	// suite that value is reachable only through the pin's no-redirect case.
	restore := pinNothingForTest(t)
	root, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve(default): %v", err)
	}
	if root.Path != filepath.Join(home, WorkspaceDirName) || root.Source != SourceDefault {
		t.Fatalf("default resolve = %+v, want %s from %s", root, filepath.Join(home, WorkspaceDirName), SourceDefault)
	}
	restore()

	// 2. A declaration wins over the default and names the settings layer.
	root, err = Resolve(declared)
	if err != nil {
		t.Fatalf("Resolve(declared): %v", err)
	}
	if root.Path != declared || root.Source != SourceSettingsKey {
		t.Fatalf("declared resolve = %+v, want %s from %s", root, declared, SourceSettingsKey)
	}

	// 3. The ambient override wins over BOTH (the sandbox hook).
	envRoot := filepath.Join(t.TempDir(), "env-root")
	t.Setenv(WorktreeRootEnvName, envRoot)
	root, err = Resolve(declared)
	if err != nil {
		t.Fatalf("Resolve(env): %v", err)
	}
	if root.Path != envRoot || root.Source != SourceSettingsEnv {
		t.Fatalf("env resolve = %+v, want %s from %s", root, envRoot, SourceSettingsEnv)
	}
}

// TestResolveTildeExpands: a declared "~/workspace" must compare equal to the
// absolute spelling, because the tilde is expanded by ONE reader (Rule 2).
func TestResolveTildeExpands(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvName, home)
	root, err := Resolve("~/" + WorkspaceDirName)
	if err != nil {
		t.Fatalf("Resolve(~/): %v", err)
	}
	if root.Path != filepath.Join(home, WorkspaceDirName) {
		t.Fatalf("Resolve(~/workspace) = %q, want %q", root.Path, filepath.Join(home, WorkspaceDirName))
	}
}

// TestResolveRefusesRelativeRoot: a relative declared root would authorise
// different paths in different working directories, so it is refused by name
// rather than silently anchored to the process cwd.
func TestResolveRefusesRelativeRoot(t *testing.T) {
	t.Setenv(homeEnvName, t.TempDir())
	_, err := Resolve("workspace")
	if err == nil {
		t.Fatal("Resolve(\"workspace\") accepted a relative root")
	}
	var rel *RelativeRootError
	if !asRelativeRoot(err, &rel) {
		t.Fatalf("Resolve(\"workspace\") error = %v, want *RelativeRootError", err)
	}
}

// asRelativeRoot is a tiny errors.As shim kept local so this test does not drag
// the errors package into the assertion's noise.
func asRelativeRoot(err error, target **RelativeRootError) bool {
	for err != nil {
		if e, ok := err.(*RelativeRootError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestPermitsIsOneWayContainment: a root permits itself and anything below it,
// and nothing beside it. The "beside it" case is the one a prefix STRING match
// gets wrong, so it is asserted explicitly.
func TestPermitsIsOneWayContainment(t *testing.T) {
	base := t.TempDir()
	root := Root{Path: filepath.Join(base, "workspace"), Role: RoleWorktree, Source: SourceDefault}
	inside := []string{
		root.Path,
		filepath.Join(root.Path, "gopi-task"),
		filepath.Join(root.Path, "a", "b", "c"),
	}
	for _, p := range inside {
		if !Permits(root, p) {
			t.Errorf("Permits(%q) = false, want true", p)
		}
	}
	outside := []string{
		filepath.Join(base, "workspace-old"), // prefix STRING match would allow this
		filepath.Join(base, "other"),
		"/",
		"/etc",
		"",
		base,
	}
	for _, p := range outside {
		if Permits(root, p) {
			t.Errorf("Permits(%q) = true, want false", p)
		}
	}
}

// TestPermitsRootTheRootItselfIsNotAUniverse: a root of "/" or "." claims
// nothing, so a mis-declared root cannot authorise the whole filesystem.
func TestPermitsRootTheRootItselfIsNotAUniverse(t *testing.T) {
	for _, bad := range []string{"/", ".", ""} {
		root := Root{Path: bad, Role: RoleWorktree, Source: SourceSettingsKey}
		if Permits(root, "/etc/passwd") {
			t.Errorf("root %q permitted /etc/passwd", bad)
		}
	}
}

// TestPermitsRootWithNoRoleClaimsNothing: authorisation comes from the ROLE, so
// a Root value built without one must not permit anything.
func TestPermitsRootWithNoRoleClaimsNothing(t *testing.T) {
	root := Root{Path: t.TempDir(), Source: SourceSettingsKey}
	if Permits(root, filepath.Join(root.Path, "x")) {
		t.Fatal("a root with no role permitted a path")
	}
}

// TestPermitsResolvesSymlinkedRoot: a scratch home can be a symlink (on this
// host it was one — …/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch —
// until the memory backing was retired on 2026-09-18) and the kernel reports
// RESOLVED paths. A permits-check that compares only the literal
// spelling refuses a path the operator legitimately declared, so both spellings
// must be compared.
func TestPermitsResolvesSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "linked-root")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := Root{Path: link, Role: RoleWorktree, Source: SourceSettingsKey}

	// A path spelled through the link…
	if !Permits(root, filepath.Join(link, "wt")) {
		t.Errorf("Permits through the link spelling = false, want true")
	}
	// …and the same path spelled through the real directory (what getcwd and
	// /proc report) must both be permitted.
	if !Permits(root, filepath.Join(real, "wt")) {
		t.Errorf("Permits through the resolved spelling = false, want true")
	}
	// The neighbour is still outside.
	if Permits(root, real+"-other") {
		t.Errorf("Permits(%q) = true, want false", real+"-other")
	}
}

// TestResolveExplicitlyPermitsDevShmWhenDeclared is the NEGATIVE probe of the
// /dev/shm rule: /dev/shm is not universally forbidden — it is forbidden as a
// WORKTREE root UNLESS the operator declares it. A blanket ban would break the
// legitimate fixture case the assessment measured (the suite's tmpfs scratch).
func TestResolveExplicitlyPermitsDevShmWhenDeclared(t *testing.T) {
	t.Setenv(homeEnvName, t.TempDir())
	root, err := Resolve(ShmDir)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", ShmDir, err)
	}
	if !Permits(root, filepath.Join(ShmDir, "gopi-fixture-wt")) {
		t.Fatalf("an explicitly declared %s root did not permit a worktree inside it", ShmDir)
	}
}

// TestRootConstantsAreNotDuplicated is the in-package half of the literal gate:
// the /dev/shm and /tmp values live here and nowhere else in this package, and
// every consumer must reference these names.
func TestRootConstantsAreNotDuplicated(t *testing.T) {
	if ShmDir != "/dev/shm" {
		t.Errorf("ShmDir = %q — the standard value changed; update every consumer deliberately", ShmDir)
	}
	if AmbientTempRoot != "/tmp" {
		t.Errorf("AmbientTempRoot = %q", AmbientTempRoot)
	}
	if !strings.HasSuffix(DefaultRootLeaf, WorkspaceDirName) {
		t.Errorf("DefaultRootLeaf %q does not name the workspace directory %q", DefaultRootLeaf, WorkspaceDirName)
	}
}
