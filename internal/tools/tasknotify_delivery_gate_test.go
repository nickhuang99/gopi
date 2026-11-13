package tools

// tasknotify_delivery_gate_test.go — the ONE gate that decides whether an
// event may reach a configured notify target, and the three arms that pin it.
//
// THE DEFECT (measured on this host, 2026-11-04). gopi resolves the LEDGER and
// the NOTIFY TARGET through two INDEPENDENT channels: `--db` / GOPI_MEMORY_DB
// moves only the ledger, while config.ResolveNotify reads the PROFILE ROOT's
// settings.json ($GOPI_HOME/.gopi, else $HOME/.gopi). A run that isolated only
// its ledger therefore delivered into the OPERATOR's live sink (chat slots plus
// ~/.gopi/notify/task-events.jsonl), naming task ids that exist in NO ledger the
// console can open. Red-proven on the pre-fix binary: one private-ledger row's
// in_progress+completed appended 909 bytes / 2 events to the operator's sink,
// each carrying `ledger=<private.db>`.
//
// WHY IT IS NOT A RARE ACCIDENT. Every red-proof run of every row writes its own
// private ledger. Measured on a sibling row's session: redproof/ carried g2, g3,
// g5, g6, r1, r2 — six ledgers, six broadcast rounds of invented rows ("parent
// A"/"parent B"). So the cost is one false group per lane per round, paid by the
// operator's trust in a channel that is supposed to mean "real work moved".
//
// WHY SUPPRESSION IS THE DEFAULT (Rule 35). The safe direction must be the one
// nobody has to remember: a knob that must be passed would leave every future
// probe, test and `--db` invocation one forgotten flag away from the same leak.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// TestTaskNotifySandboxProfileStillDelivers is the arm that proves the gate is
// NARROW, not a mute: a GOPI_HOME-isolated sandbox IS a profile, so its ledger
// is that profile's OWN, and its events reach its OWN sink exactly as before.
//
// This is the shape Rule 34 describes for a sandbox and the shape every
// correctly-isolated run already has — if this arm ever fails, the gate has
// become "suppress everything that is not the operator's machine", which would
// silence the sandboxed runs that are supposed to work.
func TestTaskNotifySandboxProfileStillDelivers(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")

	// The sandbox's OWN sink — the notify target a GOPI_HOME-isolated profile
	// resolves, not a hand-picked file.
	sink := filepath.Join(config.ProfileDirIn(home), "notify", "task-events.jsonl")
	// The append target is this profile's OWN notify directory — materialise it
	// the way a real profile has it, rather than pointing the sink somewhere
	// convenient (a hand-picked temp file would not prove the sandbox's own
	// sink is reachable).
	if err := os.MkdirAll(filepath.Dir(sink), 0o755); err != nil {
		t.Fatalf("create the sandbox notify dir: %v", err)
	}
	t.Setenv(taskNotifyCmdEnv, "cat >> "+sink+" && echo >> "+sink)
	t.Setenv(taskNotifyWebhookEnv, "")

	ledger := config.LedgerPath()
	wantLedger := filepath.Join(config.ProfileDirIn(home), "memory.db")
	if ledger != wantLedger {
		t.Fatalf("sandbox ledger = %q, want the profile's own %q — this arm only means something if the ledger really IS the profile's own", ledger, wantLedger)
	}

	taskID := taskNotifyDriveOneTransition(t)

	payloads := readNotifyLog(t, sink)
	if len(payloads) == 0 {
		t.Fatalf("a GOPI_HOME-isolated sandbox (ledger %s, which IS its profile's own) delivered NOTHING to its own sink — the gate has become over-broad and silence is now the failure", ledger)
	}
	last := payloads[len(payloads)-1]
	if last.TaskID != taskID {
		t.Fatalf("sandbox sink got task_id %q, want %q", last.TaskID, taskID)
	}
	if last.Ledger != "" {
		t.Fatalf("sandbox payload carries ledger = %q — the profile's OWN ledger must not be stamped; the stamp marks a FINDING (an event you cannot look up), and this event IS lookable-up", last.Ledger)
	}
}

// TestTaskNotifyForeignLedgerSuppressionIsDiagnosed is Rule 15 applied to the
// gate: a suppressed event must SAY SO, naming the ledger and the criterion.
// Without the line, "no notification" and "the notify pipeline is broken" are
// indistinguishable to whoever is hunting a missing event — and "an alarm that
// vanished" is exactly the failure the operator cannot debug from the console.
func TestTaskNotifyForeignLedgerSuppressionIsDiagnosed(t *testing.T) {
	log := taskNotifyTestSetup(t)
	foreign := filepath.Join(t.TempDir(), "private.db")
	t.Setenv(config.MemoryDBEnv, foreign)

	stderr := captureStderr(t, func() { taskNotifyDriveOneTransition(t) })

	if payloads := readNotifyLog(t, log); len(payloads) != 0 {
		t.Fatalf("private-ledger event reached the target (%+v) — suppression did not happen", payloads)
	}
	if !strings.Contains(stderr, foreign) {
		t.Fatalf("the suppression diagnostic does not name the emitting ledger %q:\n%s\n— an operator who sees a missing event must be able to find WHICH ledger it belonged to", foreign, stderr)
	}
	if !strings.Contains(stderr, "not this profile's own") {
		t.Fatalf("the suppression diagnostic does not state the CRITERION (it must say the ledger is not this profile's own, not merely name a path):\n%s", stderr)
	}
	if !strings.Contains(stderr, "NOT delivered") {
		t.Fatalf("the suppression diagnostic does not say the event was NOT delivered:\n%s", stderr)
	}
}

// TestTaskNotifyGateIsTheOriginProperty pins the JUDGEMENT, not a spelling: the
// gate asks taskNotifyOriginLedger, so "private" means "not this profile's own"
// — a property — and never a filename or a path prefix.
//
// A path-shaped criterion would pass this table for the wrong reason (every row
// here changes the PATH), so the arm that matters is the last one: a ledger that
// lives under a directory NAMED like a sandbox still counts as the profile's own
// when it IS the profile's own. Suppressing by name would silence it.
func TestTaskNotifyGateIsTheOriginProperty(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")

	own := filepath.Join(config.ProfileDirIn(home), "memory.db")

	foreign := filepath.Join(t.TempDir(), "memory.db")

	cases := []struct {
		name       string
		ledgerEnv  string
		wantPath   string
		wantDelivr bool
	}{
		{
			name:       "profile default (no override)",
			ledgerEnv:  "",
			wantPath:   own,
			wantDelivr: true,
		},
		{
			name:       "a ledger beside the profile, but not the profile's own",
			ledgerEnv:  foreign,
			wantPath:   foreign,
			wantDelivr: false,
		},
		{
			// The discriminating row: the path CONTAINS "redproof" and lives under
			// a sessions directory — the exact spelling of the measured leak — and
			// it still delivers because it IS the profile's own file. A
			// name-matching gate fails here, which is the point.
			name:       "a path that LOOKS like a probe but IS the profile's own",
			ledgerEnv:  own,
			wantPath:   own,
			wantDelivr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(config.MemoryDBEnv, tc.ledgerEnv)
			if got := config.LedgerPath(); got != tc.wantPath {
				t.Fatalf("ledger resolved to %q, want %q — the fixture does not describe what it claims", got, tc.wantPath)
			}
			ok, reason := taskNotifyDelivers(taskNotifyPayload{Event: taskStatusCompleted, TaskID: "t-gate"})
			if ok != tc.wantDelivr {
				t.Fatalf("delivers = %v, want %v (reason: %q)", ok, tc.wantDelivr, reason)
			}
			if ok && reason != "" {
				t.Fatalf("a delivered event carried a suppression reason %q", reason)
			}
			if !ok {
				if reason == "" {
					t.Fatal("a suppressed event carried no reason — Rule 15 forbids the silent drop")
				}
				if !strings.Contains(reason, own) {
					t.Fatalf("the reason does not name the profile's own ledger %q (the criterion's other half): %q", own, reason)
				}
			}
		})
	}
}
