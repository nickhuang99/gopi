// Package testutil — exact env save/restore for the handful of sites where
// t.Setenv is the WRONG tool (a per-call flip inside a closure, which must
// unwind when the call returns, not at test cleanup).
//
// WHY THIS EXISTS (the defect class it closes):
//
//	t.Setenv restores automatically, so it is the right answer for a
//	test-scoped set. A closure that flips an env var PER CALL must instead
//	save and restore around the call — and the obvious hand-rolled form,
//
//	    prev := os.Getenv(key)
//	    os.Setenv(key, v)
//	    ...
//	    os.Setenv(key, prev)          // ← DEFECT when the var was ABSENT
//
//	is lossy: os.Getenv renders "unset" and "set to the empty string"
//	identically, so a variable that was ABSENT before the call comes back SET
//	TO "" afterwards. os.Getenv cannot see the difference; os.LookupEnv can.
//	Anything keyed on presence, or a later test in the same binary asserting
//	the variable is absent, then reads a polluted environment — and the value
//	restored is typically a t.TempDir() path the framework has already
//	removed, so the reader gets a path that no longer exists.
//
// The same class was measured in the pre-sweep test suite (devdoc/
// test-code-discipline-audit.md); the point of this helper is that the
// correct shape is one reviewed implementation rather than a per-site dance.
package testutil

import (
	"os"
	"testing"
)

// SnapshotEnv records the EXACT prior state of the named variable — its value
// AND whether it was present at all — and returns a func that puts that state
// back. An absent variable is UNSET again, never set to "".
//
// Use it as a one-liner around a call that needs a per-call env flip:
//
//	defer testutil.SnapshotEnv(t, "GOPI_MEMORY_DB")()
//	os.Setenv("GOPI_MEMORY_DB", nodeLedger)
//	out, err := somethingThatReadsTheEnv()
//
// The restore is registered with `defer`, NOT with t.Cleanup, which is the
// whole point: it must run when the enclosing call returns.
func SnapshotEnv(t *testing.T, name string) func() {
	t.Helper()
	value, existed := os.LookupEnv(name)
	return func() {
		t.Helper()
		var err error
		if existed {
			err = os.Setenv(name, value)
		} else {
			err = os.Unsetenv(name)
		}
		if err != nil {
			// Never swallowed (Rule 15b): a failed restore silently leaves the
			// variable wrong for every sibling test in this binary.
			t.Errorf("testutil: restore %s: %v", name, err)
		}
	}
}
