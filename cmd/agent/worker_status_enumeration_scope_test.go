package main

// worker_status_enumeration_scope_test.go — WHAT A STATUS ASSERTION IS ALLOWED
// TO BE ABOUT (epic: the four cmd/agent worker cases that were red on master).
//
// THE DEFECT (measured 2026-10-24, host nick-sager, HEAD 76e6faf): four cases —
// TestWorkerDaemon_IdleDrainedStopLandsOnSIGTERM / …OnStopCommand /
// …LeavesNoResidue and TestWorkerListAliasCLIPrintsTheStatusReport — asserted
// that `worker --status` prints `no workers running` once the daemon under test
// was gone. On a host running several gopi runs at once that line is ABSENT for
// reasons the test does not own: the report rendered
//
//	gopi worker pid 3318293: idle task=(not recorded) — ./bin/gopi worker (no state file …)
//	gopi worker pid 3322659: idle task=(not recorded) — ./bin/gopi worker (no state file …)
//
// for two processes that belong to no profile this run can name. They are
// COUNTED here on purpose — fallbackWorkerPIDs' stated degradation ("own == ""
// || m.profile == "" || m.profile == own ⇒ count it"): an unattributable worker
// reported as this profile's is a visible one-line miscount, one dropped is an
// invisible under-report of the whole fleet. So the production behaviour is
// RIGHT and the assertion was wrong: it measured the whole PAGE, which any
// process on the host can change, instead of THIS profile's ENUMERATION, which
// the test owns.
//
// A note on the mechanism, because the briefing guessed a different one and the
// difference decides the fix: the failing runs also printed a
// "belong to ANOTHER profile — NOT counted in the listing above" block. That
// block is NOT what pushed the empty line out (it is rendered AFTER the
// enumeration and is additive); the counted-unattributable rows above are. The
// narrowing below is therefore stated in terms of the ENUMERATION (which pids
// are listed, and which profile the enumeration is of), and it is immune to
// both shapes.
//
// Load discipline (Rule 27): nothing here generates device load.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
)

// workerStatusMachineDoc runs the MACHINE form of the read (the SAME
// enumeration, worker_status_json.go) with verb as its leading words, and
// decodes the one document it prints. verb is passed in because the two faces
// this package tests are `worker --status` and its alias `worker list`.
//
// The run goes through wlrRun, so this file adds no second spawn site: the
// worker FACE's read-only controls are exactly what the repo-context gate
// already classifies as harmless.
func workerStatusMachineDoc(t *testing.T, bin string, env []string, verb ...string) workerStatusJSONDoc {
	t.Helper()
	args := append(append([]string{}, verb...), workerStatusJSONFlag)
	code, out := wlrRun(t, env, bin, args...)
	if code != workerControlExitOK {
		t.Fatalf("`%s` exited %d, want %d — the machine form is a read of the same enumeration:\n%s",
			strings.Join(args, " "), code, workerControlExitOK, out)
	}
	var doc workerStatusJSONDoc
	// The document is the LAST block on stdout: a start-up NOTICE ("dev_repo:
	// empty — …") may precede it, and that notice belongs to the command's own
	// reporting, not to the enumeration. Trimming to the first '{' keeps the
	// decode honest (a page with no document at all fails below) without
	// teaching this helper the notice's wording.
	if i := strings.Index(out, "{"); i > 0 {
		out = out[i:]
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode the machine form of `%s`: %v\n%s", strings.Join(args, " "), err, out)
	}
	return doc
}

// workerStatusAssertOwnProfileEnumeration is the SCOPE check every status
// assertion in this package now starts from: the report under test is this
// profile's enumeration, not some other profile's and not the host's page.
//
// It is the fact the old substring assertion assumed without measuring. A
// report about the wrong profile would satisfy "no workers running" on a busy
// host and fail on a quiet one — which is exactly the flip-flop these cases
// showed — while this check is stable under either condition.
func workerStatusAssertOwnProfileEnumeration(t *testing.T, doc workerStatusJSONDoc, home string) {
	t.Helper()
	wantProfile := filepath.Join(home, config.ProfileDirName)
	if doc.Profile != wantProfile {
		t.Fatalf("the enumeration is of profile %q, want this profile %q — an assertion about the wrong profile is unmeasurable",
			doc.Profile, wantProfile)
	}
	wantDir := filepath.Join(home, config.ProfileDirName, workerStateDirName)
	if doc.StateDir != wantDir {
		t.Fatalf("the enumeration read state dir %q, want this profile's %q", doc.StateDir, wantDir)
	}
}

// workerStatusAssertNoWorkerPID states the property the daemon cases are really
// about: the enumeration lists no worker carrying pid — whatever else the host
// is running. why is the sentence a future reader needs (the same wording the
// caller would have used on the page text).
func workerStatusAssertNoWorkerPID(t *testing.T, doc workerStatusJSONDoc, pid int, why string) {
	t.Helper()
	if containsWorkerPID(doc, pid) {
		t.Fatalf("the enumeration still lists pid %d as a worker of this profile: %s\n%+v", pid, why, doc.Workers)
	}
}

// containsWorkerPID reports whether the machine document lists pid among this
// profile's workers.
func containsWorkerPID(doc workerStatusJSONDoc, pid int) bool {
	for _, w := range doc.Workers {
		if w.PID == pid {
			return true
		}
	}
	return false
}

// workerStatusWorkerLineFor returns the worker LINE the text report renders for
// pid, or "" when the page carries none. It is ANCHORED on the worker line's own
// shape (`<program> pid <n>:`) rather than being a substring search over the
// page: a pid can appear in a residue row, in a foreign row, or inside another
// process's command line, and none of those says "this profile is running it".
func workerStatusWorkerLineFor(out string, pid int) string {
	prefix := workerProgTopLevel + " pid " + itoa(pid) + ":"
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// TestWorkerStatusAssertionMeasuresTheProfileNotThePage is the minimal
// reproduction of the four red cases, with the hostile condition PRESENT and
// planted deterministically instead of waiting for the host to leak a fixture.
//
// The fixture: a synthetic procfs root holding a worker-SHAPED process whose
// OWN environment names no profile at all (the measured ambient shape — on this
// host it was `env -i gopi worker`, whose /proc/<pid>/environ carries no HOME;
// on the red run it was another run's leaked child). The production scan counts
// it, so the page renders a worker line and the empty-enumeration line never
// appears — the exact condition under which the old assertion failed while the
// daemon under test was really gone.
//
// THREE THINGS ARE PINNED, and the first two are what make this a regression
// guard rather than a tautology:
//
//	① the OLD predicate (page substring) FAILS here — if a future edit reverts
//	   to it, this case goes red and names the reason;
//	② the NEW predicates (this profile's enumeration, no worker with the
//	   absent pid) HOLD here — they are true regardless of what else runs;
//	③ the ambient process IS listed — the fixture really reproduced the
//	   condition, so ① and ② are not passing over an empty scan.
func TestWorkerStatusAssertionMeasuresTheProfileNotThePage(t *testing.T) {
	bin := gopiTestBinPath(t)
	// repo-context sandbox: the CHILD would otherwise inherit this process's cwd
	// — the live repository — and its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()

	root := t.TempDir()
	pids := liveWorkerChildren(t, 2)
	ambientPid := pids[0]
	absentPid := pids[1] // NOT planted: the pid the daemon cases would be asserting about
	// An EMPTY environ = /proc/<pid>/environ unreadable → "unattributable" →
	// counted by the stated degradation. That is the ambient shape.
	workerFixtureWithEnv(t, root, ambientPid, nil)

	env := append(wsrStatusEnv(t, home), procname.EnvProcRoot+"="+root)

	code, out := wlrRun(t, env, bin, CliVerbWorker, workerControlStatus)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` exited %d, want %d:\n%s", CliVerbWorker, workerControlStatus, code, workerControlExitOK, out)
	}

	// ③ the condition is really present (non-vacuity of ①/② below).
	if workerStatusWorkerLineFor(out, ambientPid) == "" {
		t.Fatalf("the fixture did not reproduce the ambient condition: pid %d is not listed.\n%s", ambientPid, out)
	}
	// ① THE OLD PREDICATE, kept as the trap.
	if containsLine(out, workerStatusNoWorkers) {
		t.Fatalf("the page still printed %q beside a listed ambient worker — the fixture no longer reproduces the red "+
			"condition the four cmd/agent cases failed on, so this guard would pass over nothing.\n%s",
			workerStatusNoWorkers, out)
	}
	// ② THE NEW PREDICATES — the ones the cases now use.
	if line := workerStatusWorkerLineFor(out, absentPid); line != "" {
		t.Fatalf("the text page lists pid %d, which was never planted: %q", absentPid, line)
	}
	doc := workerStatusMachineDoc(t, bin, env, CliVerbWorker, workerControlStatus)
	workerStatusAssertOwnProfileEnumeration(t, doc, home)
	workerStatusAssertNoWorkerPID(t, doc, absentPid, "this pid was never started in this profile")
	if !containsWorkerPID(doc, ambientPid) {
		t.Fatalf("the machine form does not list the ambient pid %d, so it is not the same enumeration the text "+
			"page rendered: %+v", ambientPid, doc.Workers)
	}
}
