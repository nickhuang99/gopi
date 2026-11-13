package worktreeroots

import "testing"

// pinNothingForTest installs a provider that answers "no redirect" — the pin's
// documented no-op — and returns a restore func. It exists so the tests that pin
// the UNPINNED default (<home>/workspace) can still obtain it inside a suite whose
// entry point has installed the real provider.
//
// It restores by RE-INSTALLING the provider's current answer, which is what the
// suite's own provider would return anyway (its value is memoised for the run: the
// per-run scratch root does not change). Calling the restore func is the test's
// obligation, and it is explicit rather than a t.Cleanup so a test cannot forget
// to leave the suite as it found it mid-body.
func pinNothingForTest(t *testing.T) func() {
	t.Helper()
	saved := UnderTestRoot()
	if saved == "" {
		// Nothing installed: the unpinned computation is already in force.
		return func() {}
	}
	ProvideUnderTestRoot(func() string { return "" })
	return func() { ProvideUnderTestRoot(func() string { return saved }) }
}

// TestDefaultRootAbstainsWithoutAHomeInsideATest pins the ONE case where the test
// pin must NOT engage: a process with no resolvable home has no operator root to
// leak into, so the pre-existing abstention (ErrNoHome) must stay reachable. The
// callers that assert `HOME="" ⇒ no root` are testing exactly that contract, and
// the pin must not quietly change it into "here is a root after all".
func TestDefaultRootAbstainsWithoutAHomeInsideATest(t *testing.T) {
	// The provider IS installed in this suite (the wiring gate asserts it), so
	// this test genuinely exercises the branch order: HOME is resolved BEFORE the
	// pin is consulted, which is what keeps the abstention reachable.
	if UnderTestRoot() == "" {
		t.Fatalf("no test worktree-root provider is installed — this test would pass vacuously")
	}
	t.Setenv(homeEnvName, "")
	if _, err := DefaultRoot(); err == nil {
		t.Fatalf("DefaultRoot() succeeded with no HOME inside a test — the pin must not invent a root where the resolver abstains")
	}
}

// The mechanism's own unit assertion — "with a provider installed, DefaultRoot
// returns it" — lives in the EXTERNAL test package
// (scratch_pin_test.go: TestDefaultRootInATestIsNeverTheOperatorWorkspace), which
// reaches the resolver exactly the way production callers do. It is deliberately
// NOT repeated here: this test file must not swap the suite's installed provider
// mid-run (ProvideUnderTestRoot ignores a nil argument by design, so a test-level
// swap would have to re-install the suite's own provider, and a test that reaches
// into the wiring to undo its own change is testing its own bookkeeping).
