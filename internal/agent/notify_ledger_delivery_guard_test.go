package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// Suite NOTIFY DELIVERY — the functional half of the ledger/profile relation
// (epic: the five loop tests that captured no notify, 2026-11-05, task
// 01a1aab5).
//
// THE DEFECT THIS PINS. deliverTaskNotify drops every event whose ledger is not
// its profile's own (tools/tasknotify.go: taskNotifyDelivers /
// taskNotifyOriginLedger compare memoryDBPath() with defaultMemoryDBPath(),
// i.e. "<GOPI_HOME>/.gopi/memory.db"). The suppression is CORRECT: an event
// from a private ledger must not reach another profile's sink. The fixture was
// the defect: this suite's TestMain pinned the ledger at
// <scratch>/gopi-agent-suite-ledger-*/memory.db while PinSuiteGopiProfile
// pointed GOPI_HOME at <scratch>/suite-gopi-home — SIBLINGS — so every event
// this suite fired was recorded and reported but never broadcast, and the loop
// tests failed on the ABSENCE of the log they had just asked the code to write:
//
//	TestAgent_StuckLoop_TerminatesAndNotifies
//	TestAgent_StuckToolfulLoop_TerminatesAndNotifies
//	TestAgent_WallclockCap_GracefulFinish
//	TestAgent_PollingJobOutput_FrozenJobKilled
//	TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips
//
// WHY A STATIC GUARD IS NOT ENOUGH, and this test exists beside it.
// TestSuiteLedgerIsInsideItsOwnProfile compares two PATHS: it asks whether the
// pins spell the same file. That is the right question for a pin, and it is
// blind to everything downstream of the spelling — a resolver regression, an
// extra rank in the ledger fallback chain (the legacy GOPI_MEMORY_DB_LEGACY
// spelling is one), or a changed default all leave the two paths equal while
// delivery is suppressed. This test asks the question the FAILING TESTS ask:
// does an alarm this suite fires actually arrive at the configured target? It
// measures DELIVERY, the observable property, through the production producer
// (tools.NotifySyntheticEvent → deliverTaskNotify → the ONE gate) and the
// production target resolution (config.ResolveNotify), never through a
// re-implementation of the gate's predicate (Rules 1/13/14: a mirrored
// predicate is a second definition, and it would pass while the real one
// suppressed).
//
// NON-VACUOUS BY CONSTRUCTION (Rule 15). An empty capture file is the SYMPTOM,
// so this test can never read an absent file as success: it fails when the
// capture is missing, when it is empty, and when it does not carry the event —
// each with a different sentence, because "the file is absent", "the command
// wrote nothing" and "the event was filtered" are three different defects with
// three different repairs. Restoring the sibling ledger shape in
// pinSuiteLedgerEnv makes this test red on the FIRST of those, with
// `[task-notify] … NOT delivered: emitted from ledger <A>, which is not this
// profile's own (<B>)` on stderr naming both sides.
func TestSuiteNotifyDeliveryIsNotSuppressedAsForeign(t *testing.T) {
	t.Helper()
	// A capture target resolved by a CHILD process (`cat >>`), i.e. the same
	// shape the loop tests use — the notify command runs as a child and reads
	// this process's environment at exec time, so t.Setenv is the right tool
	// (Rule 17) and the suite's ambient notify sandbox is restored afterwards.
	capture := filepath.Join(t.TempDir(), "notify.jsonl")
	t.Setenv(config.NotifyCmdEnv, "cat >> "+capture)
	t.Setenv(config.NotifyEventsEnv, tools.NotifyEventLoopStuck)
	t.Setenv(config.NotifyAsyncEnv, "0")

	// Production producer, production gate: an alarm with no ledger row behind
	// it (the loop alarms are session-level), exactly what the loop tests fire.
	tools.NotifySyntheticEvent(tools.NotifyEventLoopStuck, t.Name(), "guard", "ledger/profile relation guard")

	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("this suite's own alarm never reached the configured target (%v). The suite's ledger is "+
			"%q and the ledger this profile resolves as its own is %q: when they differ, taskNotifyDelivers "+
			"reads the event as coming from a FOREIGN ledger and suppresses delivery — so every notify "+
			"assertion in this package fails on the absence of a file its own code was told to write. "+
			"Fix the FIXTURE (the pin), never the gate",
			err, os.Getenv(config.MemoryDBEnv),
			config.ProfilePathIn(os.Getenv(config.GopiDirEnv), config.MemoryDBFileName))
	}
	if len(b) == 0 {
		t.Fatalf("the notify command ran but wrote nothing to %s — the delivery leg is broken below the "+
			"gate (empty payload or a target that discards), not suppressed by it", capture)
	}
	if !strings.Contains(string(b), tools.NotifyEventLoopStuck) {
		t.Fatalf("the capture at %s does not carry %q: %q — the event allow-list or the payload shape "+
			"changed, so this guard is measuring the wrong event", capture, tools.NotifyEventLoopStuck, string(b))
	}
}

// TestAgent_ObservationLoop_RedProof_B_SameCallSameResultTrips is an ADDRESS,
// not a second proof.
//
// The canonical definition is TestAgent_ObservationLoop_RedProofB_SameCallSame
// ResultTrips (loop_observation_test.go), cited by name in loop.go's OFFSET
// comment and in devdoc/observation-bound-repeated-look-criterion.md. This
// row's acceptance command spells it with an underscore before the B, and
// `go test -run <that spelling>` therefore printed `[no tests to run]` and
// exited 0 — a green indistinguishable from a clean run (the vacuum Rule 38
// names: "a named check that does not exist is worse than no check at all").
//
// So the acceptance spelling gets a resolver, and the resolver delegates:
// exactly ONE body, so the two names can never drift into two different
// proofs. Same for the underscore spelling of the RedProofA sibling below.
func TestAgent_ObservationLoop_RedProof_B_SameCallSameResultTrips(t *testing.T) {
	TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips(t)
}
