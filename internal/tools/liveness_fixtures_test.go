package tools

// Liveness fixtures that OUTLIVED the zombie sweep's tests.
//
// Rule 43's recipe, applied: `internal/tools/taskzombie_test.go` was deleted
// with the sweep, and two of its helpers were NOT the sweep's own —
// `itoa` and `spawnAndKill` are referenced by `claim_dead_pid_test.go`,
// `claim_holder_freshness_test.go`, `tasknotify_test.go` and
// `task_reclaim_self_test.go`, i.e. by tests that still exist and still need
// a real, killable child and a decimal pid spelling. So they are MOVED here in
// the same commit that removes the sweep, never deleted with it (Rule 43: the
// symbols a deleted mechanism carries along are decided by the reference
// distribution, not by the file they happened to live in).

import (
	"encoding/json"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// spawnAndKill returns a (kill func, pid) pair for a real child process:
// before kill() the pid is ALIVE, after it the pid is DEAD (killed and
// reaped). Using a real process keeps every signal-0 probe test honest (a
// hand-picked constant pid could exist). The fixture is spawned through the
// shared pgid-scoped reaper, so its cleanup is owned by the test that created
// it even if an assertion fails first.
func spawnAndKill(t *testing.T) (func(), int) {
	t.Helper()
	mp := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	return mp.Reap, mp.Pid()
}

// itoa is the decimal spelling of a pid for the `host:pid` claim stamp.
func itoa(n int) string {
	b, _ := json.Marshal(n) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return string(b)
}
