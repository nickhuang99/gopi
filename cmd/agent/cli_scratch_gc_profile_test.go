package main

// cli_scratch_gc_profile_test.go — the PROFILE-ROOT arm's contract
// (`gopi tool scratch-gc profile`), through the REAL binary against a SANDBOX
// profile: the report names the children by class, a child nobody declared is
// counted and named, a declared one is not, the dry run deletes NOTHING, and the
// explicit run removes exactly the aged unheld residue child whose name is in
// the family — never a declared child, never the root.
//
// Nothing here can touch the developer's real ~/.gopi: GOPI_HOME points at a
// t.TempDir whose `.gopi` child is the sandbox profile root (config resolves
// <GOPI_HOME>/.gopi), which is also what satisfies the arm's refusal guard.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// profileArmEnv is the child environment for this arm's cases: a sandbox HOME,
// its profile root, and the scratch-home sandbox so no other arm of the same
// binary can see the developer's trees.
func profileArmEnv(home, profile string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOPI_HOME=" + home,
		"GOPI_SETTINGS=" + filepath.Join(profile, "settings.json"),
		scratch.HomeEnvName + "=" + filepath.Join(home, "workspace", "gopi-test-scratch"),
	}
}

// profileArmFixture plants a sandbox profile root with a declared child, an
// undeclared child, an aged residue child (holding bytes) and a fresh residue
// child, and returns the paths by role.
type profileArmFixture struct {
	home       string
	profile    string
	declared   string
	undeclared string
	aged       string
	fresh      string
}

func seedProfileArmFixture(t *testing.T) profileArmFixture {
	t.Helper()
	home := t.TempDir()
	profile := filepath.Join(home, config.ProfileDirName)
	if err := os.MkdirAll(profile, scratch.DirPerm); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	f := profileArmFixture{
		home:       home,
		profile:    profile,
		declared:   filepath.Join(profile, "settings.json"),
		undeclared: filepath.Join(profile, "tabs"),
		aged:       filepath.Join(profile, "evid-01a0e562"),
		fresh:      filepath.Join(profile, "sigrec-red-1882254"),
	}
	if err := os.WriteFile(f.declared, []byte("{}"), scratch.FilePerm); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	for _, dir := range []string{f.undeclared, f.aged, f.fresh} {
		if err := os.Mkdir(dir, scratch.DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.aged, "bin-gopi-before"), []byte("evidence"), scratch.FilePerm); err != nil {
		t.Fatalf("write residue payload: %v", err)
	}
	stamp := time.Now().Add(-(scratch.DefaultStaleAge + time.Hour))
	for _, p := range []string{f.aged, filepath.Join(f.aged, "bin-gopi-before")} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatalf("backdate %s: %v", p, err)
		}
	}
	return f
}

// TestScratchGCProfileReportNamesTheClassesE2E runs the verb for real and reads
// its output: the count line carries all three numbers, the undeclared child is
// named on stderr, and the dry run deletes nothing at all.
func TestScratchGCProfileReportNamesTheClassesE2E(t *testing.T) {
	f := seedProfileArmFixture(t)
	env := profileArmEnv(f.home, f.profile)

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCDryRunFlag}, "", env, f.home)
	if code != 0 {
		t.Fatalf("gopi %s %s %s exit=%d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s",
			CliVerbScratchGC, scratchGCProfileFlag, scratchGCDryRunFlag, code, stdout, stderr)
	}
	if !strings.Contains(stdout, "role="+scratchGCProfileRole) {
		t.Errorf("stdout carries no %q line — the arm's count line is missing:\n%s", "role="+scratchGCProfileRole, stdout)
	}
	for _, want := range []string{"children=4", "declared=3", "undeclared=1", "dry-run=true"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("count line is missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "class=residue name=evid-01a0e562") {
		t.Errorf("the action-bearing list does not include the collectable residue child:\n%s", stdout)
	}
	if !strings.Contains(stdout, "class=undeclared name=tabs") {
		t.Errorf("the action-bearing list does not include the undeclared child:\n%s", stdout)
	}
	if !strings.Contains(stderr, "tabs") {
		t.Errorf("stderr does not NAME the undeclared child:\n%s", stderr)
	}
	// The dry run deletes NOTHING.
	for _, p := range []string{f.declared, f.undeclared, f.aged, f.fresh} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s was removed by the dry run: %v", p, err)
		}
	}
}

// TestScratchGCProfileRemovesOnlyTheResidueE2E runs the verb WITHOUT the dry-run
// flag: exactly the aged unheld residue child goes, and the declared child, the
// undeclared child, the fresh residue child and the root all survive.
func TestScratchGCProfileRemovesOnlyTheResidueE2E(t *testing.T) {
	f := seedProfileArmFixture(t)
	env := profileArmEnv(f.home, f.profile)

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCStubsYesFlag}, "", env, f.home)
	if code != 0 {
		t.Fatalf("gopi %s %s exit=%d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s",
			CliVerbScratchGC, scratchGCProfileFlag, code, stdout, stderr)
	}
	if !strings.Contains(stdout, "role="+scratchGCProfileRole+" ") || !strings.Contains(stdout, "removed=1") {
		t.Errorf("the removal line does not report removed=1:\n%s", stdout)
	}
	if _, err := os.Lstat(f.aged); !os.IsNotExist(err) {
		t.Errorf("%s survived an explicit profile sweep, want removed (err=%v)", f.aged, err)
	}
	for _, p := range []string{f.declared, f.undeclared, f.fresh} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed — only the aged unheld residue family may go: %v", p, err)
		}
	}
	if _, err := os.Stat(f.profile); err != nil {
		t.Errorf("the profile root %s was removed: %v", f.profile, err)
	}
}

// TestScratchGCProfileRefusesToWalkTheRealHome is the SAFETY case: pointed at a
// path that is not a profile root, the arm refuses by name and does nothing —
// the guard that keeps a mistyped invocation off somebody's tree.
func TestScratchGCProfileRefusesToWalkTheRealHome(t *testing.T) {
	f := seedProfileArmFixture(t)
	// A HOME whose `.gopi` does NOT exist: the sane reading is "no profile on
	// this host" — a reported no-op on the count line, exit 0, not an alarm.
	bare := t.TempDir()
	stdout, _, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCDryRunFlag}, "",
		profileArmEnv(bare, filepath.Join(bare, config.ProfileDirName)), bare)
	if code != 0 {
		t.Fatalf("a host with no profile root: exit=%d, want 0 (a reported no-op, not an alarm)\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "children=0") {
		t.Errorf("a missing profile root must still print the count line with children=0:\n%s", stdout)
	}
	// And the fixture tree is untouched by that invocation.
	if _, err := os.Stat(f.aged); err != nil {
		t.Errorf("the missing-root invocation touched another tree: %v", err)
	}
}

// TestScratchGCTickReportsUndeclaredProfileChildren is the RESIDENT face: the
// tick REPORTS an undeclared profile child on its own line and deletes nothing —
// the property the epic's constraint ⑧ asks for (the automatic face must not
// carry a delete over the profile root).
func TestScratchGCTickReportsUndeclaredProfileChildren(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	f := seedProfileArmFixture(t)
	t.Setenv("HOME", f.home)
	t.Setenv(config.GopiDirEnv, f.home)
	t.Setenv("GOPI_SETTINGS", filepath.Join(f.profile, "settings.json"))
	t.Setenv(scratch.HomeEnvName, filepath.Join(f.home, "workspace", "gopi-test-scratch"))
	// The tick's own cadence gate: arm it and make it due now.
	t.Setenv(scratch.GCIntervalEnvName, "1")
	scratchGCLastTick = time.Time{}

	out := captureStdout(t, func() { scratchGCTick(workerRoleLabel, time.Now()) })
	if !strings.Contains(out, "undeclared child(ren)") || !strings.Contains(out, "tabs") {
		t.Errorf("the tick did not report the undeclared profile child:\n%s", out)
	}
	// NOTHING was deleted: not the residue, not the undeclared child.
	for _, p := range []string{f.aged, f.fresh, f.undeclared, f.declared} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed by the resident tick — the profile root's deleting face is explicit-only: %v", p, err)
		}
	}
}

// TestProfileRootSessionsRowIsWiredFromConfig pins the ROW that crosses a package
// boundary: `sessions` is declared from internal/config (whose SessionsDirName
// owns the name — spelling it in the registry would be the second spelling the
// hardcoded-literal gate refuses). This test binary imports config, so the row
// must be there; if the registration were dropped, the profile root would report
// its largest state child as UNDECLARED and the gate over in internal/scratch
// could not see it (a leaf package's test binary has no config on its import
// path).
func TestProfileRootSessionsRowIsWiredFromConfig(t *testing.T) {
	rows := scratch.ProfileRootChildren()
	for _, row := range rows {
		if row.Name != config.SessionsDirName {
			continue
		}
		if row.Class != scratch.ProfileChildState {
			t.Errorf("the %q row has class %q, want %q — the session tree is live state",
				row.Name, row.Class, scratch.ProfileChildState)
		}
		if strings.TrimSpace(row.Bound) == "" {
			t.Errorf("the %q row carries no bound sentence", row.Name)
		}
		return
	}
	t.Fatalf("no registry row declares config.SessionsDirName (%q) — internal/config's registration is not wired",
		config.SessionsDirName)
}
