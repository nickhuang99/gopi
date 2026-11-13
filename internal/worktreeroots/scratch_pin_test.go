package worktreeroots_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// TestUnderTestRootProviderIsInstalled is the WIRING gate: the resolver's test
// branch is only as good as the installer, and the installer lives in another
// package (internal/testutil's suite entry point). A pin that is merely
// *available* is the shape this epic's defect had — testutil.MockEnv could pin
// the env for a subprocess while the IN-PROCESS producers stayed unwired. So this
// refuses a nil provider by name rather than trusting that someone remembered to
// wire it.
func TestUnderTestRootProviderIsInstalled(t *testing.T) {
	if r := worktreeroots.UnderTestRoot(); r == "" {
		t.Fatalf("no test worktree-root provider is installed in this suite: %s cannot redirect a test process off the operator's real worktree root.\n"+
			"FIX: the suite entry point must install it — call testutil.RunSuiteMain / testutil.PinSuiteForTestMain (internal/testutil/scratchroot.go, pinUnderTestWorktreeRoot)",
			"worktreeroots.UnderTestRoot()")
	}
}

// TestDefaultRootInATestIsNeverTheOperatorWorkspace is the RED→GREEN gate for the
// measured residue (174 orphan `~/workspace/gopi-<uuid>` stubs whose gitdirs died
// with the /dev/shm suite run that created them).
//
// It asserts the PRODUCT property, not the mechanism: whatever this process's HOME
// is, the DEFAULT worktree root a resolver returns inside a test is NOT
// <home>/workspace. The pre-fix behaviour is exactly that value, so this test is
// red on the unwired resolver and green once the suite entry point installs the
// provider (the red proof is run and recorded in the epic's report).
func TestDefaultRootInATestIsNeverTheOperatorWorkspace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory to protect: %v", err)
	}
	operatorRoot := filepath.Join(home, worktreeroots.WorkspaceDirName)

	// The UNPINNED shape, reproduced inside the test: HOME is the operator's real
	// home and no explicit root is in force, i.e. exactly the state the leak's
	// producers resolved in.
	resolved, err := worktreeroots.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if resolved.Path == operatorRoot {
		t.Fatalf("a test process resolved the DEFAULT worktree root to the operator's real root %s (source %q):\n"+
			"this is the leak — a fixture that places a ground under the default root mints a real worktree in %s, and its gitdir dies with this run's scratch",
			operatorRoot, resolved.Source, operatorRoot)
	}
	def, err := worktreeroots.DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot: %v", err)
	}
	if def == operatorRoot {
		t.Fatalf("DefaultRoot() = %s — the operator's real worktree root", operatorRoot)
	}

	// And it must be UNDER the pinned scratch, i.e. a location this run's own
	// cleanup removes (Rule 22: the criterion is that the path is off `/`, and the
	// suite entry point has already established that for its scratch root).
	scratchRoot := testutil.SuiteTempRoot()
	if !strings.HasPrefix(resolved.Path, scratchRoot+string(os.PathSeparator)) {
		t.Fatalf("the test worktree root %s is not under this run's scratch root %s — a root outside it is not collected by the run's own cleanup",
			resolved.Path, scratchRoot)
	}

	// PRECEDENCE IS UNTOUCHED (Rule 2): an explicit root still wins over the pin,
	// so a test that pins its own root keeps it.
	explicit := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, explicit)
	got, err := worktreeroots.Resolve("")
	if err != nil {
		t.Fatalf("Resolve with an explicit root: %v", err)
	}
	if got.Path != explicit {
		t.Fatalf("an explicit %s root was ignored: got %s, want %s", worktreeroots.WorktreeRootEnvName, got.Path, explicit)
	}
	// A repo's DECLARED root wins for that repo too — with the ambient override
	// cleared, so the layer under test is the one being measured.
	t.Setenv(worktreeroots.WorktreeRootEnvName, "")
	declared := t.TempDir()
	got, err = worktreeroots.Resolve(declared)
	if err != nil {
		t.Fatalf("Resolve with a declared root: %v", err)
	}
	if got.Path != declared {
		t.Fatalf("a declared repo root was ignored: got %s, want %s", got.Path, declared)
	}
}

// TestDefaultRootAbstainsWithoutAHomeInsideATest pins the ONE case where the pin
// must NOT engage: a process with no resolvable home has no operator root to leak
// into, so the pre-existing abstention stands. It lives in the INTERNAL test
// package because it must set the home variable by its own constant (homeEnvName)
// rather than by a second, hand-typed "HOME" (Rules 1b/13) — see
// pin_abstention_test.go.
