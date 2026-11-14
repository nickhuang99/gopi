package signalrec

// signalrec_test.go — the regression guard for the kill-traceback gap
// (epic: all gopi processes killed by signal, 2026-09-13) AND for WHERE its
// records land (epic: signalrec session attribution, 2026-09-14).
//
// Three properties are pinned:
//
//  1. A VICTIM records its own death, with enough surrounding facts to name
//     its role and its supervisor (the first suspect).
//  2. A KILLER records its intent BEFORE signalling, which is the only
//     possible evidence for an uncatchable SIGKILL.
//  3. Every record lands in the SESSION DIR of the process that wrote it —
//     asserted as a PATH, per scenario (task-run child, bare session,
//     exported handoff, resident role), and NEVER under <gopi dir>/logs,
//     with no public-dir fallback: a record that cannot be attributed is
//     REFUSED and reported, never quietly filed somewhere global.
//
// (3) is the reason the tests read the FILESYSTEM rather than a return value:
// the contract under test IS the path, and a test that asserted on an
// in-process variable would pass while the artifact stayed unattributable.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/sigsource"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// srChildEnv turns the test binary into a stand-in gopi process: it installs
// the recorder (or not, per EnvOff) and then blocks until it is signalled. The
// OFF-switch test needs a real victim, because the interesting case is exactly
// "nothing catches the signal" — in-process that would kill the test runner.
const srChildEnv = "GOPI_SIGNALREC_TEST_CHILD"

// srReadyEnv hands the victim a FIFO to announce on. The victim writes ONE byte
// to it the instant Install() has RETURNED — register-then-announce, so the
// announcement cannot outrun the registration (a compiled-in property the
// parent relies on; see srAwaitChildReady).
const srReadyEnv = "GOPI_SIGNALREC_TEST_READY"

// srVictimReadyPrefix marks the line the victim writes into the announcement
// FIFO. Tests search the bytes for it, so a stray write from the runtime cannot
// pass for the handshake (Rule 1b: one named source for the wire token).
const srVictimReadyPrefix = "signalrec-victim-ready"

// srOwnerVictimEnv turns the re-executed test binary into the CLAIMING victim:
// it installs its OWN terminating-signal handler, declares the claim
// (ClaimSignalHandlers), announces readiness, and then unwinds for
// srOwnerUnwind -- deliberately TWICE the recorder's grace, so the arm's outcome
// is decided by the ownership predicate and never by scheduling luck.
const srOwnerVictimEnv = "GOPI_SIGNALREC_TEST_OWNER_CHILD"

// srOwnerDoneEnv names the file the claiming victim writes at the END of its
// unwind -- the analogue of the session_end line epic 0x01a0d65f is about: its
// presence proves the handler ran to COMPLETION, not merely that the process
// died after a delay.
const srOwnerDoneEnv = "GOPI_SIGNALREC_TEST_OWNER_DONE"

// srOwnerDonePrefix is the token the owner victim writes when its unwind has
// COMPLETED -- the evidence the parent asserts on (Rule 1b: one named token).
const srOwnerDonePrefix = "signalrec-owner-unwind-complete"

// srOwnerExitCode is what the claiming victim exits with once its unwind has
// finished -- the same "interrupted, so the run ends non-zero" shape a
// bare-prompt gopi has (session_end exit_code=1).
const srOwnerExitCode = 1

// srOwnerUnwind is the claiming victim's simulated graceful unwind.
var srOwnerUnwind = 2 * defaultForwardGrace

// merge-note (resolve merge fix/rawlog-test-home-leak into master): BOTH sides
// replaced this file's fixed 300ms start-up sleep with a real start-up
// handshake and share the rationale (recorded at the const block above). They
// differ in the WIRE, and HEAD's FIFO is the one kept because it is the one
// that works under `go test`: the runner hands the test BINARY a pipe on ITS
// stdout, so master's stderr-token variant signals the child before it has
// entered m.Run() and the runtime's output-copy goroutine has nothing draining
// that pipe — the child does not reach its select until the parent's Wait(),
// the exact deadlock the handshake exists to prevent. The FIFO leaves the
// child's stdout untouched.
//
// What master contributed here is therefore superseded, not silently dropped:
// srReadyToken + its constants (srReadyTimeout/srReadyPollInterval) and the
// srAwaitChildReady(t, cmd, pidOut) call sites, whose parent-side poll loop read
// a temp FILE of the child's stderr, are all replaced by HEAD's
// srAnticipateChildReady/srAwaitChildReady(t, childEnv)/srAnnounceReady trio.
// The semantic contribution both sides made — "the child states its readiness
// instead of the parent sleeping at it" — is what is kept. The master-side
// stylegate/allow-sleep annotation paraphrases in this file differ from HEAD's
// only in wording, so HEAD's are kept (identical marker, identical semantics).

// TestMain re-executes the test binary as that victim when srChildEnv is set.
//
// It routes through internal/testutil's suite pin (scratch root + notify env
// sandbox) like every other suite: without it this package's tests would read
// the OPERATOR's real settings.json (whose notify.cmd writes a live sink) and
// scratch under the ambient temp root — the gate
// TestEveryTestMainSuiteIsNotifyHermetic exists precisely to fail a hand-rolled
// TestMain that bypasses the pin.
func TestMain(m *testing.M) {
	testutil.PinSuiteForTestMain("signalrec")
	// THIS SUITE DISARMS THE SUITE RUN-ROOT SIGNAL ARM, IN EVERY PROCESS IT
	// RUNS, and it must happen HERE — after the pin (which arms it) and BEFORE
	// the child branches below (which are separate processes that each pin
	// again). Measured, and the measurement is the reason:
	//
	//	$ go test ./internal/signalrec/ -run TestSignalRecAnnouncedKillIsClassifiedSourced -count=1
	//	[probe] pid=1809133 ... FIRST delivery of SIGTERM
	//	[probe-arm] pid=1809133 arm HANDLED terminated -> Stop + re-raise to self
	//	[probe] pid=1809133 ... REPEAT delivery 2 of SIGTERM
	//	signalrec_unsourced_test.go:174: last record kind = "signal-repeat", want "signal"
	//
	// The pin arms internal/testutil's suite run-root handler on SIGTERM: to
	// reap a run root after a run is killed from outside, it Stop()s its own
	// channel and RE-RAISES the signal to this same process. It cannot tell an
	// external kill from a signal a test sent itself, so in THIS package — whose
	// whole subject is what a process records when it is signalled — it is a
	// competing subscriber of the signal under test that INJECTS one extra
	// delivery per raise. The recorder records that delivery, so one SIGTERM
	// becomes two records, and the injected one lands AFTER the death record
	// (`kind":"signal-repeat"`), which is exactly the "last record is not the
	// death record" failure. os/signal.Stop does not deregister the runtime
	// subscription while another channel is registered (Go 1.24), so releasing
	// this arm's channel does not stop the fan-out.
	//
	// Both halves of the suite are affected and both are covered here: the
	// parent (TestSignalRecRepeatedDeliveryLeavesEvidenceInsteadOfSilence
	// counts N deliveries and got N+1 records; the session-log arm got three
	// events where it wanted two) and the VICTIM CHILDREN (the arms this file's
	// child branches dispatch), which inherit the same pin.
	//
	// PRECEDENT, not a new mechanism: cmd/agent/worker_stop_accept_test.go:233
	// takes the same documented disarm for the same stated reason ("in
	// production the only subscriber of the stop signal is
	// installWorkerStopSignals, and the arm is test-infrastructure noise that
	// changes the outcome being measured"), and cmd/agent/testmain_test.go:133
	// does it for the whole suite. JoinSuiteRunRootSignalHandler is idempotent
	// and a no-op where nothing was armed.
	//
	// THE TRADE, stated so it is not silent: from here to process exit, a
	// terminating signal has its default disposition, so a run of THIS suite
	// killed from outside keeps its scratch root — the next run of the same
	// suite collects it at start (the pin calls
	// ReapDeadSuiteRunRootsAtSuiteStart, which reaps a root whose recorded owner
	// the kernel confirms dead). Nothing is added to the system (Rule 35): it is
	// a call to an API that already exists and already has a close-out join.
	testutil.JoinSuiteRunRootSignalHandler()
	if os.Getenv(srOwnerVictimEnv) == "1" {
		srBindThisFixtureChild()
		srRunOwnerVictim()
		return
	}
	if os.Getenv(srChildEnv) == "1" || os.Getenv(srUnsourcedVictimEnv) == "1" {
		srBindThisFixtureChild()
		uninstall := Install("")
		defer uninstall()
		// THE DISARMED ARM (signalrec_sicapture_test.go arm ②) is measured in a
		// REAL process: Install armed the capture, and this releases it BEFORE the
		// parent's signal arrives — the shape a capture that cannot report a
		// delivery leaves behind, and the one the old record rendered as silence.
		if os.Getenv(srCaptureDisarmedEnv) == "1" {
			sigsource.Release(watchedSignals)
		}
		srAnnounceReady()
		select {} // block until signalled; the parent does the killing
	}
	// The session-log victim (signalrec_sessionlog_test.go) installs the recorder
	// and hands over a session log through the PUBLIC seam only, so it is a
	// separate fixture from the two above rather than a flag on them.
	if sessionLog := os.Getenv(srSessionLogEnv); sessionLog != "" {
		srBindThisFixtureChild()
		srRunSessionLogVictim(sessionLog)
		return
	}
	// ONLY the suite arm, never a child branch above: this call CREATES a new
	// death pipe and exports it as the hold variable, so a victim that ran it
	// would re-arm onto its own pipe and be bound to itself instead of to the
	// parent whose death must take it down -- the victim would survive, which is
	// the leak. The arm must also come AFTER the child branches, or every child
	// would re-arm before its own dispatch.
	testutil.ArmAndSweepForSuite("signalrec")
	code := m.Run()
	testutil.FinishSuiteForTestMain()
	os.Exit(code)
}

// srRunOwnerVictim is the OWNER arm of the victim harness: the shape of a
// process whose graceful shutdown takes LONGER than the recorder's grace -- the
// bare-prompt gopi of epic 0x01a0d65f.
//
// The order mirrors main.go and is the whole point: Install (the recorder, as
// early as possible), then the process's OWN handler, then the ownership claim
// IMMEDIATELY after Notify, then the readiness announcement -- so the parent
// knows the recorder will really see a live claim at signal time.
func srRunOwnerVictim() {
	uninstall := Install("")
	defer uninstall()
	ch := make(chan os.Signal, signalBufSize)
	signal.Notify(ch, syscall.SIGTERM)
	ClaimSignalHandlers()
	srAnnounceReady()
	<-ch
	//stylegate:allow-sleep the wait IS the observation: the owner must unwind its handler before the child checks the done file
	time.Sleep(srOwnerUnwind)
	if path := os.Getenv(srOwnerDoneEnv); path != "" {
		if err := os.WriteFile(path, []byte(srOwnerDonePrefix+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "signalrec owner victim: cannot write %s: %v\n", path, err)
		}
	}
	os.Exit(srOwnerExitCode)
}

// srSessionDir creates a REAL session dir of the given id under the temp gopi
// dir and returns it: <home>/.gopi/sessions/<id>/ with the container's own
// metadata.json, which is what isSessionDir demands as PROOF. A plain
// empty directory is deliberately not enough — a record filed under an id that
// turns out to be a typo reads as evidence, so resolution requires a marker.
func srSessionDir(t *testing.T, home, id string) string {
	t.Helper()
	dir := filepath.Join(home, ".gopi", config.SessionsDirName, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create session dir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionMetadataName), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write %s: %v", sessionMetadataName, err)
	}
	return dir
}

// srSessionDirWithLog is srSessionDir, plus the session JSONL a real run always
// writes — used where a handoff hands over a task dir (which the task-session
// machinery creates before the child's own session log lands in it).
func srSessionDirWithLog(t *testing.T, home, id string) string {
	t.Helper()
	dir := srSessionDir(t, home, id)
	if err := os.WriteFile(filepath.Join(dir, id+sessionJSONLSuffix), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
	return dir
}

// srProvenancePath is where a record written by a process owning dir must land.
func srProvenancePath(dir string) string { return filepath.Join(dir, FileName) }

// srReadIn parses every record in the provenance file of one session dir. It
// returns nil when the file is absent (that absence is what several tests
// assert). Fatal on a parse error: a malformed line would be invisible to the
// operator's grep, which is the failure this package exists to avoid.
func srReadIn(t *testing.T, dir string) []record {
	t.Helper()
	b, err := os.ReadFile(srProvenancePath(dir))
	if err != nil {
		return nil
	}
	var out []record
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("provenance line is not one JSON object per line (%q): %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// srAssertNoLegacySink fails when ANYTHING was written under the retired public
// dir. This is the no-fallback assertion, and it is a PATH check on the real
// filesystem: the epic's whole point is that no code path in this package may
// reach that directory again.
func srAssertNoLegacySink(t *testing.T, home string) {
	t.Helper()
	legacyDir := filepath.Join(home, ".gopi", LogDirName)
	if fi, err := os.Stat(legacyDir); err == nil && fi.IsDir() {
		entries, rerr := os.ReadDir(legacyDir)
		if rerr == nil && len(entries) > 0 {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Fatalf("the retired public sink dir %s is non-empty (%v) — signalrec must never write there", legacyDir, names)
		}
	}
}

// srHome points GOPI_HOME at a temp dir and returns it. t.Setenv restores it
// automatically (Rule 17 — never os.Setenv in a test).
func srHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(EnvOff, "")
	// The in-process tests end with a real signal delivered to the test binary
	// itself. Forwarding (the default-disposition fidelity Install implements —
	// see TestSignalRecForwardingPreservesDefaultDeath) would kill the test
	// runner, so the in-process cases opt out and assert on the FILE; the
	// forwarding behaviour has its own child-process test.
	t.Setenv(EnvNoForward, "1")
	// The recorder's handoff/env facts must not leak in from the environment
	// that runs the test suite (this agent's own session is an agent session!).
	t.Setenv(SessionDirEnv, "")
	t.Setenv(TaskIDEnv, "")
	t.Setenv(EnvAgentChild, "")
	t.Setenv(taskRunEnvName, "")
	t.Setenv(wrapperEnvName, "")
	SetSessionDir("", "", "")
	t.Cleanup(func() { SetSessionDir("", "", "") })
	return home
}

// srSetArgv makes this process LOOK like the given gopi invocation for the
// duration of the test: role detection reads os.Args, and the process layout is
// decided by the mode, so a test that asserts on resident-vs-one-shot behaviour
// has to control argv. In-package only (os.Args is not a seam worth exporting).
func srSetArgv(t *testing.T, args ...string) {
	t.Helper()
	prev := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = prev })
}

// srWaitForRecord polls until dir's provenance file has want lines or the
// deadline passes (a fixed sleep is the flaky shape — the handler writes
// asynchronously).
func srWaitForRecord(t *testing.T, dir string, want int) []record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var recs []record
	for time.Now().Before(deadline) {
		recs = srReadIn(t, dir)
		if len(recs) >= want {
			return recs
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep bounded poll for the handler's async write; the loop exits on the first record and the deadline caps it
	}
	return recs
}

// TestSignalRecInstallRecordsTheDeathInTheSessionDir is the minimal repro of
// BOTH halves of this epic: a gopi process receives SIGTERM and must leave a
// durable record — and that record must be INSIDE the session dir the process
// owns, not in a host-global file (`原则无效，检查才有效`: the path is asserted,
// never eyeballed).
func TestSignalRecInstallRecordsTheDeathInTheSessionDir(t *testing.T) {
	home := srHome(t)
	const sessionID = "a0d9340b-3550-47ad-831b-f79f0137d60c"
	dir := srSessionDir(t, home, sessionID)
	SetSessionDir(dir, "", "")

	uninstall := Install("")
	defer uninstall()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("self-signal SIGTERM: %v", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 provenance record in %s after one SIGTERM, got %d", dir, len(recs))
	}
	r := recs[0]
	if r.Kind != KindSignal {
		t.Errorf("record kind = %q, want %q", r.Kind, KindSignal)
	}
	if r.Signal != "SIGTERM" {
		t.Errorf("record signal = %q, want SIGTERM (an operator must read the name, not a number)", r.Signal)
	}
	if r.PID != os.Getpid() {
		t.Errorf("record pid = %d, want %d", r.PID, os.Getpid())
	}
	if r.PPID == 0 || r.ParentPID == 0 {
		t.Errorf("record carries no parent facts (ppid=%d parent_pid=%d) — the supervisor is the first suspect", r.PPID, r.ParentPID)
	}
	if r.Argv == "" {
		t.Error("record carries no argv — the record cannot say WHICH gopi mode died")
	}
	if r.Cgroup == "" {
		t.Error("record carries no cgroup — a scope teardown reaches every member and is invisible without it")
	}
	if r.TS == "" {
		t.Error("record carries no timestamp")
	}
	if r.SessionDir != dir {
		t.Errorf("record session_dir = %q, want the dir it was written in %q (a record copied out of the tree must still name its session)", r.SessionDir, dir)
	}
	if r.Layout != LayoutSession {
		t.Errorf("record layout = %q, want %q (the dir is keyed by a session id)", r.Layout, LayoutSession)
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecTaskRunChildWritesIntoTheLedgerTaskSessionDir pins the task-run
// child scenario: the process carries the LEDGER id (GOPI_TASK_RUN_ID), so its
// record belongs in sessions/<ledger id>/ beside the run's session.jsonl and
// task-report.md — the directory a post-mortem of that row opens.
func TestSignalRecTaskRunChildWritesIntoTheLedgerTaskSessionDir(t *testing.T) {
	home := srHome(t)
	const taskID = "01a09805-6dae-7892-b19c-69c8ce9c5008"
	dir := srSessionDirWithLog(t, home, taskID)
	// The child of `gopi task run` sees the wrapper row's id (set by the
	// launcher at spawn: cmd/agent/cli_tasks.go) and NOTHING else.
	t.Setenv(taskRunEnvName, taskID)
	srSetArgv(t, "gopi")

	uninstall := Install("")
	defer uninstall()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("self-signal SIGTERM: %v", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	if len(recs) != 1 {
		t.Fatalf("task-run child left %d records in %s, want 1 (the ledger task's session dir)", len(recs), dir)
	}
	if recs[0].Layout != LayoutTaskRun {
		t.Errorf("layout = %q, want %q", recs[0].Layout, LayoutTaskRun)
	}
	if recs[0].TaskRun != taskID {
		t.Errorf("record task_run = %q, want the ledger id %q — the existing record fields must survive the move", recs[0].TaskRun, taskID)
	}
	srAssertNoLegacySink(t, home)
}

// srStaleLedgerID is a ledger id that names NO session dir in the sandbox — the
// shape an operator's shell inherits from the agent that launched it (the id is
// real in the OTHER profile, absent here).
const srStaleLedgerID = "0f5c5d3e-0000-4000-8000-000000000000"

// srMergerKind is the kind production records for a merger stop request
// (cmd/agent's mergerProgTopLevel); spelled here so the record's discriminator
// is asserted against a named value rather than an inline one.
const srMergerKind = "gopi merger"

// srSetResidentArgv makes this process look like the merger daemon's own argv
// (`<bin> merger …`), which is what roleFromArgv reads to label a RESIDENT mode
// (it takes argv[1] as the verb; a real invocation is exactly this shape).
func srSetResidentArgv(t *testing.T) {
	t.Helper()
	srSetArgv(t, "gopi", RoleMergerVerb)
}

// srSetOneShotArgv makes this process look like a NON-resident gopi mode (a
// one-shot verb), so the process layout is deliberately not available to it.
func srSetOneShotArgv(t *testing.T) {
	t.Helper()
	srSetArgv(t, "gopi", verbTask, "list")
}

// srVerifyChildArgv makes this process look like the merger's VERIFY CHILD —
// `gopi task verify <id>` — which is the argv mode the attribution below is
// about. It is an agent-session mode that OWNS its signal handlers
// (taskRunMode-style: "task verify" is not in the resident table, and its
// longest-matching mode owns the signal), so the process-keyed layout is not
// available to it.
func srVerifyChildArgv(t *testing.T) {
	t.Helper()
	srSetArgv(t, "gopi", verbTask, "verify", "01a0a72c-48ba-4e2f-93d1-4f3bcb9a1c9e")
}

// srSetVerifyChildDir creates the TASK session dir the gate hands to its verify
// child (<sessions>/<ledger task id>, the dir `gopi task verify` runs the child
// in — tools.TaskSessionDir) and returns it. It carries the session container's
// marker + the run JSONL, which is what the container looks like by the time the
// child's own session log has landed in it.
func srSetVerifyChildDir(t *testing.T, home, taskID string) string {
	t.Helper()
	return srSessionDirWithLog(t, home, taskID)
}

// TestSignalRecVerifyChildWithTheHandedTaskDirIsAttributed is the regression for
// the epic signalrec-verify-attribution (2026-09-17): the merger's verify child
// dies at the wait cap — the DOMINANT end of that role, since the cap is what
// kills it — and before this fix it left NOTHING durable.
//
// MECHANISM (all three handles absent at once, which is why it is this one
// process):
//
//	① no GOPI_SIGNALREC_SESSION_DIR — nothing handed it one;
//	② no ledger id — the gate STRIPS the daemon contract on purpose (an
//	   independent verification must not inherit the resolving run's handle),
//	   and the child mints its own session uuid, so the judged row's key is
//	   not derivable inside it;
//	③ `task verify` owns its signal handlers, so the resident process-keyed
//	   layout is unavailable by construction.
//
// sessionDest therefore fell all the way through to the refusal — correctly, by
// the no-public-fallback contract, but uselessly: the dir WAS known to the
// launcher. The fix is the handoff asserted here — the dir plus the judged row
// plus the marker that says the pair is MEANT (signalrec.EnvAgentChild), set by
// cmd/agent/merger_verify_child.go immediately before the spawn.
//
// THE FALL-THROUGH HOLE (named, and DELIBERATELY NOT CLOSED): the destination is
// honourable ONLY because the launcher opted in with the marker. Inferring it
// instead — "SessionDirEnv + a ledger id, therefore task-keyed" — reopens
// over-attribution: a bare `gopi` re-launched from an agent's shell inherits the
// SAME pair (a stale ledger id and a dir that belonged to another process's run),
// so every descendant gopi would file its records under its ancestor's session.
// The marker is the difference between a handoff and an inheritance, and it is
// read as a PAIR with the dir: a marker without a dir resolves nothing (fail
// closed, back to the ordinary resolution) rather than guessing at a directory.
// The two negative arms below pin exactly that hole.
func TestSignalRecVerifyChildWithTheHandedTaskDirIsAttributed(t *testing.T) {
	home := srHome(t)
	srVerifyChildArgv(t)
	const taskID = "01a0a72c-48ba-4e2f-93d1-4f3bcb9a1c9e"
	dir := srSetVerifyChildDir(t, home, taskID)
	// The handoff is the gate's: dir + judged row + the opt-in marker.
	t.Setenv(SessionDirEnv, dir)
	t.Setenv(TaskIDEnv, taskID)
	t.Setenv(EnvAgentChild, "1")

	if err := Intent(srVerifyChildKind, "verify child exceeded the wait cap", 4242, 4242); err != nil {
		t.Fatalf("the verify child's record must be FILED in the handed task dir, got refusal: %v", err)
	}
	recs := srReadIn(t, dir)
	if len(recs) != 1 {
		t.Fatalf("verify child's record landed %d times in %s, want 1", len(recs), dir)
	}
	if recs[0].Layout != LayoutTaskRun {
		t.Errorf("layout = %q, want %q — the handed dir is the JUDGED ROW's session container", recs[0].Layout, LayoutTaskRun)
	}
	if recs[0].TaskRun != taskID {
		t.Errorf("task_run = %q, want the judged ledger id %q", recs[0].TaskRun, taskID)
	}
	if recs[0].SessionDir != dir {
		t.Errorf("session_dir = %q, want %q (the record must say which session it belongs to, not only where it sits)", recs[0].SessionDir, dir)
	}
	srAssertNoLegacySink(t, home)
}

// srVerifyChildKind is the intent kind cmd/agent's verify path records (the
// `verify-child` reason channel); spelled once so the record's discriminator is
// asserted against a named value rather than an inline one.
const srVerifyChildKind = "verify-child"

// TestSignalRecUnmarkedHandoffKeepsTheSessionLayout is the FIRST negative arm of
// the fall-through hole named above, stated exactly: the marker does NOT gate
// WHETHER a handed dir may receive a record (an env handoff has always meant
// "file it here" — unchanged), it gates the RECORD'S TASK ATTRIBUTION. The same
// dir, handed to an UNMARKED process, is classified as a session dir and is never
// relabelled as the declared row's container. Without this arm, "the launcher
// marked an agent child" and "somebody inherited a dir" would produce byte-equal
// records, and an inherited pair could be read back as a handoff.
func TestSignalRecUnmarkedHandoffKeepsTheSessionLayout(t *testing.T) {
	home := srHome(t)
	srVerifyChildArgv(t)
	const taskID = "01a0a72c-48ba-4e2f-93d1-4f3bcb9a1c9e"
	// The handed dir is a run-uuid-keyed one (the shape whose ownership the
	// marker decides), and the process carries the judged row's id — a pair a
	// descendant shell inheriting a stale id would exhibit. What the dir is
	// NOT is the container of that row, so no task layout may be claimed.
	dir := srSetVerifyChildDir(t, home, srVerifyRunUUID)
	t.Setenv(taskRunEnvName, taskID)
	t.Setenv(SessionDirEnv, dir)
	t.Setenv(EnvAgentChild, "") // NOT a handed-over agent child

	if err := Intent(srVerifyChildKind, "verify child exceeded the wait cap", 4242, 4242); err != nil {
		t.Fatalf("an env handoff has always been a filing instruction; only its ATTRIBUTION is marker-gated: %v", err)
	}
	recs := srReadIn(t, dir)
	if len(recs) != 1 {
		t.Fatalf("want 1 record in the handed dir %s, got %d", dir, len(recs))
	}
	if recs[0].Layout == LayoutTaskRun {
		t.Errorf("layout = %q — an UNMARKED handoff must not be relabelled as the declared row's container (that is the over-attribution the marker exists to separate from a handoff)", recs[0].Layout)
	}
	if recs[0].SessionDir != dir {
		t.Errorf("session_dir = %q, want the dir the record actually landed in (%q)", recs[0].SessionDir, dir)
	}
	srAssertNoLegacySink(t, home)
}

// srVerifyRunUUID is the session-uuid form of a dir (the shape a run's own
// session log is keyed by), used to make the marker's two negative arms
// unambiguous: it is deliberately NOT any ledger id.
const srVerifyRunUUID = "00000000-1111-4222-8333-444444444444"

// TestSignalRecAgentChildMarkerWithAMismatchedRowIsRefused is the THIRD arm: the
// marker is a CONSISTENCY ASSERTION, so a marked handoff whose declared row is
// NOT the key of the handed dir is refused loudly rather than relabelled. Filing
// the record under a row the directory does not belong to is precisely the
// unattributable artifact the session-dir rule exists to remove — the record
// would sit in one session and name another.
func TestSignalRecAgentChildMarkerWithAMismatchedRowIsRefused(t *testing.T) {
	home := srHome(t)
	srVerifyChildArgv(t)
	dir := srSetVerifyChildDir(t, home, srVerifyRunUUID)
	t.Setenv(SessionDirEnv, dir)
	t.Setenv(TaskIDEnv, "01a0a72c-48ba-4e2f-93d1-4f3bcb9a1c9e") // declares a row the dir does not key
	t.Setenv(EnvAgentChild, "1")

	err := Intent(srVerifyChildKind, "verify child exceeded the wait cap", 4242, 4242)
	if err == nil {
		t.Fatal("a marked handoff whose dir is not the declared row's container must be refused — the launcher may not relabel a record into another row's session")
	}
	if !strings.Contains(err.Error(), srVerifyRunUUID) {
		t.Errorf("refusal %q does not name the handed dir — the operator must be able to see WHICH pair disagreed", err.Error())
	}
	if recs := srReadIn(t, dir); len(recs) != 0 {
		t.Errorf("the refused intent wrote %d record(s) into %s — a refusal must write nothing", len(recs), dir)
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecAgentChildMarkerWithoutADirResolvesNothing is the SECOND negative
// arm: the marker is read as a PAIR with the dir, so a marker with no dir falls
// back to the ordinary resolution instead of inventing a destination. A bare
// marker must never become a licence to write somewhere (Rule 15: fail closed).
func TestSignalRecAgentChildMarkerWithoutADirResolvesNothing(t *testing.T) {
	home := srHome(t)
	srVerifyChildArgv(t)
	t.Setenv(EnvAgentChild, "1") // armed, but no dir accompanies it

	err := Intent(srVerifyChildKind, "verify child exceeded the wait cap", 4242, 4242)
	if err == nil {
		t.Fatal("a marker with no dir must resolve NO destination — the marker licenses a handoff, it does not create one")
	}
	if !strings.Contains(err.Error(), LogDirName) {
		t.Errorf("refusal %q does not name the retired public sink — the no-fallback contract must still be readable", err.Error())
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecResidentModeWithAForeignLedgerIDFilesIntoTheProcessDir is the
// regression for the operator-stop defect (2026-09-14): `gopi merger --stop`
// printed a failure-looking line — "signal provenance NOT FILED (no public-dir
// fallback) … refusing to write" — on a stop that had in fact succeeded.
//
// MECHANISM. The operator's shell carries GOPI_TASK_RUN_ID inherited from the
// agent that launched it, so the merging CLI process reads a ledger id whose
// session dir cannot exist in this profile. sessionDest's step 2 outranks the
// process layout, so that id used to be a REFUSAL — for a process whose argv
// mode is RESIDENT, which step 3 would have filed happily (process-<pid>). A
// record that CAN be filed must never be refused because of an id that cannot
// be honoured.
//
// Both halves are asserted: the record lands in the process dir (with a note
// naming the unusable id, so the placement stays auditable) AND no bucket is
// minted for the foreign id.
func TestSignalRecResidentModeWithAForeignLedgerIDFilesIntoTheProcessDir(t *testing.T) {
	home := srHome(t)
	srSetResidentArgv(t)
	t.Setenv(taskRunEnvName, srStaleLedgerID)

	if err := Intent(srMergerKind, "operator ran gopi merger --stop", 4242, 4242); err != nil {
		t.Fatalf("Intent refused a record that the process layout could file: %v", err)
	}

	dir := filepath.Join(home, ".gopi", config.SessionsDirName, fmt.Sprintf("%s%d", processDirPrefix, os.Getpid()))
	recs := srReadIn(t, dir)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 record in the process dir %s, got %d", dir, len(recs))
	}
	if recs[0].Layout != LayoutProcess {
		t.Errorf("layout = %q, want %q (the ledger id could not be honoured, so the process layout holds it)", recs[0].Layout, LayoutProcess)
	}
	if recs[0].TaskRun != srStaleLedgerID {
		t.Errorf("record task_run = %q, want the inherited ledger id %q — the record must carry the tree it descends from even when that id could not key the dir", recs[0].TaskRun, srStaleLedgerID)
	}
	if !strings.Contains(recs[0].Note, srStaleLedgerID) {
		t.Errorf("the record's note %q does not name the unusable ledger id %q — why the id was not honoured has to be readable from the record", recs[0].Note, srStaleLedgerID)
	}
	// The foreign id must NOT have been given a bucket of its own.
	if entries, err := os.ReadDir(filepath.Join(home, ".gopi", config.SessionsDirName)); err == nil {
		if len(entries) != 1 || entries[0].Name() != processDirPrefix+strconv.Itoa(os.Getpid()) {
			t.Errorf("sessions root holds %v, want only the process dir — a foreign ledger id must not mint a session bucket", entries)
		}
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecOneShotWithAMissingLedgerDirIsStillRefused is the boundary of the
// fix: the fall-through to the process layout is for RESIDENT modes ONLY. A
// one-shot process (argv names no resident mode) that carries an unusable ledger
// id still gets a refusal and leaves no bucket — its id IS the session the record
// belongs to, and minting an empty bucket per invocation is the litter the
// process layout is deliberately not for.
func TestSignalRecOneShotWithAMissingLedgerDirIsStillRefused(t *testing.T) {
	home := srHome(t)
	srSetOneShotArgv(t)
	t.Setenv(taskRunEnvName, srStaleLedgerID)

	err := Intent(srMergerKind, "operator ran gopi merger --stop", 4242, 4242)
	if err == nil {
		t.Fatal("a one-shot process with an unusable ledger id must be refused — the resident fall-through must not become a general one")
	}
	if !strings.Contains(err.Error(), LogDirName) {
		t.Errorf("refusal %q does not name the retired public sink", err.Error())
	}
	if entries, rerr := os.ReadDir(filepath.Join(home, ".gopi", config.SessionsDirName)); rerr == nil && len(entries) > 0 {
		t.Errorf("the refusal created %d entries under the sessions root (%v) — a refused record must leave no bucket behind", len(entries), entries)
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecClaimedHandlerOneShotWithAForeignLedgerIDFilesIntoTheProcessDir
// is the regression for the OTHER half of the attribution rule (measured
// 2026-10-12, task 01a12f76-c01f): a handler-owning ONE-SHOT — `gopi memory
// sync --every 5s --push`, `gopi merge --every 5s` — launched as a CHILD of an
// agent session under a NON-default profile (GOPI_HOME=<sandbox>).
//
// MECHANISM. The child INHERITS the agent session's GOPI_TASK_RUN_ID, which is
// the EXECUTOR IDENTITY of the run it descends from, NOT a claim on the
// container it writes into. That id's dir cannot exist in the sandbox profile,
// and the mode is not in the resident argv table, so sessionDest's step 2 was a
// REFUSAL — while the process-keyed destination was filable one branch away.
// The failure is SILENT from outside: exit 0 and the verb's own stop line are
// unchanged, so a caller reading the exit status sees nothing (the SIGTERM
// differential is what loses even the refusal line — the verb's own handler
// wins the race; measured with SIGUSR1, which the recorder watches and the verb
// does not).
//
// BOTH ARMS ARE ASSERTED, because the fix must be narrow: the claim is the ONLY
// thing that opens the process layout to a one-shot. Without it the refusal
// stands (the sibling test above) — a one-shot that owns no handler has no
// lifecycle to mint a bucket for, and the retirement of the `merge` alias is
// pinned by a residue test that must stay green
// (cmd/agent: TestMergeEveryKilledLoopLeavesNoResidueInTheProfile — its loop
// never claims).
func TestSignalRecClaimedHandlerOneShotWithAForeignLedgerIDFilesIntoTheProcessDir(t *testing.T) {
	home := srHome(t)
	srSetOneShotArgv(t)
	t.Setenv(taskRunEnvName, srStaleLedgerID)
	prevClaimed := handlersClaimed.Load()
	t.Cleanup(func() { handlersClaimed.Store(prevClaimed) })
	handlersClaimed.Store(false)

	// ARM 1 — no claim: the refusal still stands (the boundary this fix must
	// not widen into a general "any one-shot gets a process bucket").
	if err := Intent(srMergerKind, "no claim", 4242, 4242); err == nil {
		t.Fatal("a one-shot that claims nothing must still be refused — the process layout is not generally available")
	}

	// ARM 2 — the claim: the record is FILED, and the placement is auditable.
	ClaimSignalHandlers()
	if err := Intent(srMergerKind, "agent-launched loop stopped under a foreign id", 4242, 4242); err != nil {
		t.Fatalf("Intent refused a record that the process layout could file for a CLAIMING process: %v", err)
	}
	dir := filepath.Join(home, ".gopi", config.SessionsDirName, fmt.Sprintf("%s%d", processDirPrefix, os.Getpid()))
	recs := srReadIn(t, dir)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 record in the process dir %s, got %d", dir, len(recs))
	}
	if recs[0].Layout != LayoutProcess {
		t.Errorf("layout = %q, want %q", recs[0].Layout, LayoutProcess)
	}
	if recs[0].TaskRun != srStaleLedgerID {
		t.Errorf("record task_run = %q, want the inherited ledger id %q — the record must still name the run it descends from (executor identity, carried, never trusted as ownership)", recs[0].TaskRun, srStaleLedgerID)
	}
	if !strings.Contains(recs[0].Note, "ClaimSignalHandlers") {
		t.Errorf("the note %q does not name the arm that admitted the process layout (the claim) — the placement must be auditable, and a claim that opened a destination has to be readable from the record", recs[0].Note)
	}
	// The inherited id must NOT have been given a container of its own in this
	// profile: identity is carried in the record, never minted as a directory.
	if entries, err := os.ReadDir(filepath.Join(home, ".gopi", config.SessionsDirName)); err == nil {
		if len(entries) != 1 || entries[0].Name() != processDirPrefix+strconv.Itoa(os.Getpid()) {
			t.Errorf("sessions root holds %v, want only the process dir — an inherited ledger id must not mint a session bucket in this profile", entries)
		}
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecExportedHandoffWinsAndIsValidated pins the CHILD handoff: the
// launcher knows the session dir the child cannot derive (`gopi task run`
// mints the child's run uuid and keys the ledger row by it), so the env carries
// it — and a handoff that is NOT a session dir is refused loudly rather than
// ignored in favour of another resolution.
func TestSignalRecExportedHandoffWinsAndIsValidated(t *testing.T) {
	home := srHome(t)
	const taskID = "01a0986d-e176-7c59-a3ee-907a9be5ec91"
	dir := srSessionDirWithLog(t, home, taskID)
	t.Setenv(SessionDirEnv, dir)
	t.Setenv(TaskIDEnv, taskID)

	if err := Intent("verify-child", "cap exceeded", 4242, 4242); err != nil {
		t.Fatalf("Intent with a handed-over session dir: %v", err)
	}
	recs := srReadIn(t, dir)
	if len(recs) != 1 {
		t.Fatalf("handoff record landed %d times in %s, want 1", len(recs), dir)
	}
	if recs[0].TaskRun != taskID {
		t.Errorf("task_run = %q, want %q (the handed pair carries the ledger id)", recs[0].TaskRun, taskID)
	}
	if recs[0].SessionDir != dir {
		t.Errorf("session_dir = %q, want %q", recs[0].SessionDir, dir)
	}

	// A handed dir that is not a session dir: the write must FAIL. Filing the
	// record somewhere else would silently contradict the caller that believed
	// it had handed over the right directory.
	bogus := filepath.Join(home, "not-a-session-dir")
	if err := os.MkdirAll(bogus, 0o755); err != nil {
		t.Fatalf("mkdir bogus: %v", err)
	}
	t.Setenv(SessionDirEnv, bogus)
	err := Intent("verify-child", "cap exceeded", 4242, 4242)
	if err == nil {
		t.Fatal("Intent accepted a handoff that is not a session dir — a wrong destination must be a refusal, not a fallback")
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("refusal %q does not name the rejected dir %q", err.Error(), bogus)
	}
	if got := len(srReadIn(t, dir)); got != 1 {
		t.Errorf("the refused intent appended a second record to %s (now %d) — a refusal must write nothing", dir, got)
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecBareSessionWithNoHandoffIsRefused is the no-exception case for
// the bare/auto prompt shape: a process that cannot name a session dir and is
// not a resident mode must NOT write anywhere — least of all the retired public
// dir. The refusal is an ERROR the caller sees (Rule 15).
func TestSignalRecBareSessionWithNoHandoffIsRefused(t *testing.T) {
	home := srHome(t)
	srSetArgv(t, "gopi", "task", "list") // a one-shot mode: owns no session, no dir

	err := Intent("verify-child", "cap exceeded", 1, 1)
	if err == nil {
		t.Fatal("Intent succeeded with no session dir to write into — a public-dir fallback has crept back in")
	}
	if !strings.Contains(err.Error(), LogDirName) {
		t.Errorf("refusal %q does not name the retired public sink — an operator grepping the old path must be told it is no longer used", err.Error())
	}
	srAssertNoLegacySink(t, home)
	if entries, rerr := os.ReadDir(filepath.Join(home, ".gopi", config.SessionsDirName)); rerr == nil && len(entries) > 0 {
		t.Errorf("the refusal created %d entries under the sessions root — a refused record must leave no bucket behind", len(entries))
	}
}

// TestSignalRecResidentRoleGetsAProcessScopedSessionDir covers the one process
// that legitimately has no session of its own: the resident merger daemon,
// which executes no task and owns no session log. Its dir is process-keyed (the
// pid form cannot be mistaken for a session uuid or a ledger id) and the record
// says so.
func TestSignalRecResidentRoleGetsAProcessScopedSessionDir(t *testing.T) {
	home := srHome(t)
	srSetArgv(t, "gopi", RoleMergerVerb) // the resident daemon

	uninstall := Install("")
	defer uninstall()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("self-signal SIGTERM: %v", err)
	}
	dir := filepath.Join(home, ".gopi", config.SessionsDirName, fmt.Sprintf("%s%d", processDirPrefix, os.Getpid()))
	recs := srWaitForRecord(t, dir, 1)
	if len(recs) != 1 {
		t.Fatalf("resident merger wrote %d records into %s, want 1", len(recs), dir)
	}
	if recs[0].Layout != LayoutProcess {
		t.Errorf("layout = %q, want %q", recs[0].Layout, LayoutProcess)
	}
	if recs[0].Note == "" {
		t.Error("a process-keyed destination must say WHY in the record — the placement has to be auditable, not inferred")
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecRecordsOncePerSignalKind pins the dedup contract: a second
// SIGTERM must not append a second DEATH line. The operator greps these files
// during an incident; duplicate death lines per signal would make a fleet-wide
// kill look like twice as many dead processes as there were.
//
// THE DEATH LINE IS STILL EXACTLY ONE — and the repeats are no longer INVISIBLE.
// A repeat is now recorded as its own kind (KindSignalRepeat), which is the
// other half of the same contract: the count of "a gopi process died here" is
// untouched, while "the request was delivered three times" stops being a fact
// the evidence cannot state (before this, the handler answered with a bare
// `continue` and the three deliveries left one line, indistinguishable from
// one delivery).
func TestSignalRecRecordsOncePerSignalKind(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "dedup-session")
	SetSessionDir(dir, "", "")

	uninstall := Install("")
	defer uninstall()

	const deliveries = 3
	for i := 0; i < deliveries; i++ {
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("self-signal %d: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep makes the 3 signals land as distinct deliveries the dedup must collapse; bounded by the arm
	}
	time.Sleep(200 * time.Millisecond) //stylegate:allow-sleep settle budget so a WRONG extra record has time to appear before the count is asserted

	recs := srReadIn(t, dir)
	var deaths, repeats int
	for _, r := range recs {
		switch r.Kind {
		case KindSignal:
			deaths++
		case KindSignalRepeat:
			repeats++
		}
	}
	if deaths != 1 {
		t.Fatalf("%d SIGTERMs produced %d death record(s), want 1 (dedup per signal kind):\n%s",
			deliveries, deaths, srReadWholeFile(t, srProvenancePath(dir)))
	}
	if repeats != deliveries-1 {
		t.Fatalf("%d SIGTERMs produced %d repeat record(s), want %d — every delivery but the first must be EVIDENCE, not a silent drop:\n%s",
			deliveries, repeats, deliveries-1, srReadWholeFile(t, srProvenancePath(dir)))
	}
}

// TestSignalRecIntentNamesTheKiller is the regression guard for the SIGKILL
// blind spot: an uncatchable kill leaves NO victim record, so the killer's own
// intent line is the entire evidence chain. It must name the target and the
// reason.
func TestSignalRecIntentNamesTheKiller(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "killer-session")
	SetSessionDir(dir, "", "")

	const (
		wantKind   = "verify-child"
		wantReason = "verify child for 01a09805 exceeded the 45m0s wait cap"
		wantTarget = 424242
	)
	if err := Intent(wantKind, wantReason, wantTarget, wantTarget); err != nil {
		t.Fatalf("Intent: %v", err)
	}
	recs := srReadIn(t, dir)
	if len(recs) != 1 {
		t.Fatalf("want 1 intent record in %s, got %d", dir, len(recs))
	}
	r := recs[0]
	if r.Kind != KindIntent {
		t.Errorf("kind = %q, want %q", r.Kind, KindIntent)
	}
	if r.TargetPID != wantTarget {
		t.Errorf("target_pid = %d, want %d", r.TargetPID, wantTarget)
	}
	if !strings.Contains(r.Reason, wantKind) || !strings.Contains(r.Reason, wantReason) {
		t.Errorf("reason = %q, want it to carry both the kind %q and the reason %q", r.Reason, wantKind, wantReason)
	}
	if r.PID == 0 {
		t.Error("intent record does not say WHO killed (no killer pid)")
	}
	if r.SessionDir != dir {
		t.Errorf("intent session_dir = %q, want %q", r.SessionDir, dir)
	}
}

// TestSignalRecAppendOnlyOneJSONPerLineUnderConcurrency pins the file contract
// under contention: several writers (a killer recording intents while the
// process's own signal handler records a death) must interleave at LINE
// granularity, never into a half line — the file is read by grep, and a torn
// line is invisible evidence.
func TestSignalRecAppendOnlyOneJSONPerLineUnderConcurrency(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "concurrent-session")
	SetSessionDir(dir, "", "")

	const writers = 8
	const perWriter = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := Intent("test-writer", fmt.Sprintf("writer %d record %d", w, i), 1000+w, 1000+w); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Intent failed: %v", err)
	}

	recs := srReadIn(t, dir) // Fatal on any unparseable line — the assertion
	if len(recs) != writers*perWriter {
		t.Fatalf("concurrent writers produced %d records, want %d (append-only, no overwrite)", len(recs), writers*perWriter)
	}
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.Reason] = true
	}
	if len(seen) != writers*perWriter {
		t.Errorf("only %d distinct records survived %d writes — lines were lost", len(seen), writers*perWriter)
	}
	// One JSON object per line, exactly: the raw bytes must have one line per
	// record (a trailing newline, no blank interior lines).
	raw, err := os.ReadFile(srProvenancePath(dir))
	if err != nil {
		t.Fatalf("read provenance file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != writers*perWriter {
		t.Errorf("file has %d lines for %d records — one JSON object per line is the operator's grep contract", len(lines), writers*perWriter)
	}
	for i, line := range lines {
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("line %d is not a standalone JSON object (%q): %v", i+1, line, err)
		}
	}
}

// TestSignalRecOffIsHonored pins the escape hatch: with GOPI_SIGNAL_RECORD_OFF
// set nothing is installed and nothing is written. A host that must not write
// this file (or a test that must not pollute a real one) depends on it.
func TestSignalRecOffIsHonored(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "off-session")

	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0])
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		SessionDirEnv: dir,
		EnvOff:        "1",
		srReadyEnv:    ready[srReadyEnv],
	})
	// Bind the victim to this test binary's lifetime before it starts:
	// the child must be armed with the death pipe this process holds, and
	// the guard (run after the readiness handshake) must find its verdict.
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecOffIsHonored")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start victim child: %v", err)
	}
	// The child announces that it is at its blocking select; with the off
	// switch set that is Install()'s early return, so `select {}` and "ready"
	// are the same instant here — which is exactly what the death assertion
	// needs (the child must be PAST start-up before it is signalled).
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal victim child: %v", err)
	}
	// The child must DIE (nothing caught the signal) — that is the proof the
	// off switch removed the handler rather than merely muting its output.
	if err := cmd.Wait(); err == nil {
		t.Fatalf("victim survived SIGTERM with %s=1 — a handler was still installed", EnvOff)
	}
	if recs := srReadIn(t, dir); len(recs) != 0 {
		t.Fatalf("recorder wrote %d record(s) into %s with %s=1 — the off switch is not honored", len(recs), dir, EnvOff)
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecChildVictimRecordsEndToEnd is the process-level red proof: a
// REAL gopi-shaped child (a separate process, matching production) is signalled
// and must leave a record in the session dir it was handed, naming the signal
// and its supervisor. The in-process tests above prove the handler; this one
// proves it inside a process that is actually killed, which is the shape of the
// incident.
func TestSignalRecChildVictimRecordsEndToEnd(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	home := srHome(t)
	dir := srSessionDir(t, home, "child-session")

	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0])
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		SessionDirEnv:                  dir,
		config.AgentSessionAncestorEnv: "worker",
		srReadyEnv:                     ready[srReadyEnv],
	})
	// Bind the victim to this test binary's lifetime before it starts:
	// the child must be armed with the death pipe this process holds, and
	// the guard (run after the readiness handshake) must find its verdict.
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecChildVictimRecordsEndToEnd")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start victim child: %v", err)
	}
	childPID := cmd.Process.Pid
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()
	if err := syscall.Kill(childPID, syscall.SIGTERM); err != nil {
		t.Fatalf("signal victim child: %v", err)
	}
	// The child catches SIGTERM (handler installed), records, then returns to
	// its blocking select — so the parent must not wait for it to exit. Poll
	// the sink instead, then stop the child.
	recs := srWaitForRecord(t, dir, 1)
	_ = cmd.Process.Kill() //stylegate:ignore teardown of the fixture child after the sink poll; the assertion is on the records, not the kill
	_ = cmd.Wait()         //stylegate:ignore reaping the child we just killed; its wait status is not asserted here

	if len(recs) == 0 {
		t.Fatal("a killed gopi-shaped child left NO provenance record — the incident is not traceable")
	}
	r := recs[0]
	if r.PID != childPID {
		t.Errorf("victim pid = %d, want the child %d", r.PID, childPID)
	}
	if r.Signal != "SIGTERM" {
		t.Errorf("signal = %q, want SIGTERM", r.Signal)
	}
	if r.ParentPID != os.Getpid() {
		t.Errorf("parent_pid = %d, want the test (the child's real supervisor) %d", r.ParentPID, os.Getpid())
	}
	if r.Role != "worker" {
		t.Errorf("role = %q, want the exported role marker %q", r.Role, "worker")
	}
	srAssertNoLegacySink(t, home)
}

// merge-note (resolve merge fix/rawlog-test-home-leak into master): master's
// srAwaitChildReady(t, cmd, pidOut) — the stderr/temp-FILE poll loop that read
// srReadyToken — is REMOVED here, not dropped in intent. Both sides fixed the
// same start-up race (the fixed 300ms sleep); HEAD announces through a FIFO the
// parent creates BEFORE Start (srAnticipateChildReady → srAnnounceReady → the
// FIFO read, see srAwaitChildReady(t, childEnv) below), and all three victim
// tests in this file are built on that wire. Master's variant could not be kept
// alongside it: srAwaitChildReady would be declared twice, and its call shape
// (it owned cmd.Start and took the pidOut pointer) is what HEAD's callers no
// longer do.

// a single value per key regardless of what the parent's environment carried.
// This dedup is NOT cosmetic: Go's os.Getenv returns the FIRST match in the
// environment block, so a blind append leaves the PARENT's value winning — the
// bug this helper was written after watching a child ignore its own cleared
// switch and survive a signal it should have died from.
func srAppendEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return append(out, prefix+value)
}

// srChildReadyTimeout bounds the wait for the victim's readiness
// announcement. It is a HANG GUARD, not a handshake: the child announces the
// instant Install() returns, which is milliseconds — but the parent must not
// wait forever on a child that can never announce (a refused start-up, a
// missing FIFO), so the wait fails the test with a diagnosis instead.
const srChildReadyTimeout = 30 * time.Second

// srAnticipateChildReady creates the announcement FIFO the victim will write to
// and returns the child's environment override for it. Call it while BUILDING
// the victim's env, before Start: the pipe is open and waiting when the child
// reaches its first instruction, so no announcement can be lost.
func srAnticipateChildReady(t *testing.T, home string) map[string]string {
	t.Helper()
	pipe := filepath.Join(home, "victim-ready.fifo")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatalf("create the victim readiness pipe %s: %v", pipe, err)
	}
	return map[string]string{srReadyEnv: pipe}
}

// srAwaitChildReady blocks until the victim announces that its recorder is
// INSTALLED, then returns the pipe handle so the caller can close it.
//
// WHY this replaces the start-up sleep: the old handshake was
// `time.Sleep(300 * time.Millisecond)` — "the child has surely started by now".
// Measured on this host (2026-09-13, both a master-only tree and the merged
// one): the child's Install() lands at ~0.25-0.30s, so the fixed sleep sits
// EXACTLY on the latency, and under load the signal arrives before the handler
// exists — the record is never written and the test reports a false
// regression ("a killed gopi-shaped child left NO provenance record") on
// whichever tree happens to be running while the host is busy. A sleep cannot
// be tuned out of that: it is a race by construction. The child knows when it
// is ready, so it says so.
//
// The read is opened in a goroutine (a FIFO blocks until BOTH ends are open),
// and the wait is bounded so a child that never starts fails loudly rather than
// hanging the suite (Rule 15).
func srAwaitChildReady(t *testing.T, childEnv map[string]string) *os.File {
	t.Helper()
	pipe := childEnv[srReadyEnv]
	if pipe == "" {
		t.Fatal("srAwaitChildReady called without srAnticipateChildReady — the readiness handshake is not set up")
	}
	type opened struct {
		f   *os.File
		err error
	}
	ch := make(chan opened, 1)
	go func() {
		f, err := os.OpenFile(pipe, os.O_RDONLY, 0)
		ch <- opened{f, err}
	}()
	select {
	case o := <-ch:
		if o.err != nil {
			t.Fatalf("open the victim readiness pipe %s: %v", pipe, o.err)
		}
		if err := o.f.SetReadDeadline(time.Now().Add(srChildReadyTimeout)); err != nil {
			t.Fatalf("bound the readiness read: %v", err)
		}
		buf := make([]byte, 64)
		n, err := o.f.Read(buf)
		if err != nil || n == 0 {
			t.Fatalf("the victim never announced its readiness within %s (read %d bytes, err=%v) — "+
				"the child could not Install() its recorder, so signalling it would prove nothing "+
				"about the handler", srChildReadyTimeout, n, err)
		}
		if !strings.Contains(string(buf[:n]), srVictimReadyPrefix) {
			t.Fatalf("the victim's readiness announcement was %q, want it to contain %q", buf[:n], srVictimReadyPrefix)
		}
		return o.f
	case <-time.After(srChildReadyTimeout):
		t.Fatalf("the victim did not announce its readiness within %s — it never reached Install()", srChildReadyTimeout)
		return nil
	}
}

// srAnnounceReady is the VICTIM's half: it writes the readiness token to the
// FIFO the parent handed it, AFTER Install() has returned. A child without the
// override (the ordinary in-suite run) does nothing.
func srAnnounceReady() {
	pipe := os.Getenv(srReadyEnv)
	if pipe == "" {
		return
	}
	f, err := os.OpenFile(pipe, os.O_WRONLY, 0)
	if err != nil {
		// Deliberately not fatal: the victim's job is to be signalled, and the
		// parent diagnoses a missing announcement itself (it owns the FIFO).
		// Recorded rather than swallowed (Rule 15b).
		fmt.Fprintf(os.Stderr, "signalrec victim: cannot announce readiness on %s: %v\n", pipe, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(srVictimReadyPrefix + "\n"); err != nil {
		fmt.Fprintf(os.Stderr, "signalrec victim: readiness write on %s failed: %v\n", pipe, err)
	}
}

// srChildEnvFor builds a victim child's environment: the parent's env with the
// gopi data dir, the child marker, and every override applied exactly once. The
// parent's own signalrec handoff vars are CLEARED first: this agent session's
// env (the suite runs inside gopi!) must not decide where a child test's record
// lands.
func srChildEnvFor(env []string, home string, overrides map[string]string) []string {
	out := srAppendEnv(env, config.GopiDirEnv, home)
	out = srAppendEnv(out, srChildEnv, "1")
	// The readiness handshake is ON by default for every victim child: a child
	// that does not announce Install() would leave the parent guessing with a
	// sleep again (see srAwaitChildReady).
	out = srAppendEnv(out, srReadyEnv, "1")
	for _, k := range []string{SessionDirEnv, TaskIDEnv, EnvAgentChild, taskRunEnvName, wrapperEnvName} {
		if _, ok := overrides[k]; !ok {
			out = srAppendEnv(out, k, "")
		}
	}
	for k, v := range overrides {
		out = srAppendEnv(out, k, v)
	}
	return out
}

// TestSignalRecIntentFailsLoudlyWithoutASink is the Rule 15 half: when the
// handed session dir is unusable Intent must return an error rather than
// silently dropping the only evidence of a SIGKILL — and it must not place the
// record anywhere else.
func TestSignalRecIntentFailsLoudlyWithoutASink(t *testing.T) {
	home := srHome(t)
	// The handoff points INSIDE a regular file, so the dir can never be
	// validated: the destination is genuinely unusable.
	blocker := filepath.Join(home, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	t.Setenv(SessionDirEnv, blocker)

	if err := Intent("verify-child", "cap exceeded", 1, 1); err == nil {
		t.Error("Intent returned nil with an unusable destination — a swallowed SIGKILL intent is the incident this package exists to end")
	}
	srAssertNoLegacySink(t, home)

	// With no gopi dir at all the same rule holds (and the error names the
	// reason, never a silent success).
	t.Setenv(config.GopiDirEnv, "")
	t.Setenv("HOME", "")
	if err := Intent("verify-child", "cap exceeded", 1, 1); err == nil {
		t.Error("Intent returned nil with no gopi dir at all")
	}
}

// TestSignalRecForwardingPreservesDefaultDeath is the guard for the risk this
// package introduces. Registering a signal with signal.Notify REPLACES its
// default disposition, so a naive recorder turns every one-shot gopi command
// into a process that ignores `kill -TERM`. This test drives a real child whose
// argv names a verb that owns NO handler and asserts the child still DIES — and
// that it left its record first, which is the ordering the re-raise guarantees.
func TestSignalRecForwardingPreservesDefaultDeath(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	home := srHome(t)
	t.Setenv(EnvNoForward, "") // the child inherits: forwarding MUST be on
	dir := srSessionDir(t, home, "forward-session")

	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0], "task", "list") // a verb that owns no handler
	// EnvNoForward is explicitly CLEARED: srHome set it for this process and
	// the child must not inherit it.
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		SessionDirEnv: dir,
		EnvOff:        "",
		EnvNoForward:  "",
		srReadyEnv:    ready[srReadyEnv],
	})
	// Bind the victim to this test binary's lifetime before it starts:
	// the child must be armed with the death pipe this process holds, and
	// the guard (run after the readiness handshake) must find its verdict.
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecForwardingPreservesDefaultDeath")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start victim child: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal victim child: %v", err)
	}
	// Died? The default action for SIGTERM must still be taken. The probe is
	// Wait() — NOT `Signal(0)`, which keeps succeeding on a zombie child that
	// no one has reaped yet (an earlier version of this test was fooled by
	// exactly that and reported a false failure).
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("child exited without a signal status (%v) — expected a default-disposition death", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore teardown of the child that just failed the assertion; the t.Fatal below is the reported failure
		<-waitErr
		t.Fatal("child with a non-handler verb SURVIVED SIGTERM — the recorder stole the signal's default disposition")
	}

	recs := srReadIn(t, dir)
	if len(recs) == 0 {
		t.Fatal("child died but left no record — forwarding must not outrun the evidence write")
	}
	if recs[0].Signal != "SIGTERM" {
		t.Errorf("recorded signal = %q, want SIGTERM", recs[0].Signal)
	}

	// The policy table itself, on both sides. THE PAIR THAT MATTERS is
	// "task list" vs "task run": same verb, opposite policy — the sub-mode is
	// what decides, and conflating them is the bug this table exists to prevent.
	mustForward := []string{"task list", "task show", "task status", "stats", "verify", RoleUnknown, "shot"}
	for _, m := range mustForward {
		if !forwardsDefault(m) {
			t.Errorf("forwardsDefault(%q) = false — a one-shot/unclassifiable mode must keep SIGTERM's default death", m)
		}
	}
	mustNotForward := []string{RoleMergerVerb, "worker", "chat", "serve", taskRunMode, taskMergerMode}
	for _, m := range mustNotForward {
		if forwardsDefault(m) {
			t.Errorf("forwardsDefault(%q) = true — a handler-owning mode must be left to its own graceful path", m)
		}
	}
}

// TestSignalRecForegroundMergeIsNotTheRetiredMerger is the regression guard for
// the argv-alias defect measured on master 2026-09-28 (task 01a0e5d2).
//
// MECHANISM. roleFromArgv used to map argv[1]=="merge" onto RoleMergerVerb, and
// RoleMergerVerb is in modesThatOwnTheirSignalHandlers. `gopi merge` — a
// FOREGROUND command whose `--every D` arm owns its own SIGINT/SIGTERM handler
// (runMergeLoop, cmd/agent/merge_run.go) and never claims signalrec's handlers —
// was therefore classified as the RETIRED resident merger. sessionDest's step 3
// then minted <gopi dir>/sessions/process-<pid> for its record, so a loop
// stopped while a pass was in flight wrote a directory into its own profile.
// Measured: 3 of 10 runs of
// `go test ./cmd/agent/ -run TestMergeEveryLoopWritesNoStateFile -count=1` were
// red on master, always naming .gopi/sessions; the residue is what the test
// asserts must not exist.
//
// The assertions are the POLICY, both halves:
//   - `merge` must take the default death (a foreground command that never owns
//     the handlers must not be made unkillable by the recorder — the same
//     "one-shot verbs must stay killable" contract the sibling table test
//     pins), and
//   - it must NOT be labelled the merger any more (the alias is what routed it
//     into the resident layout).
//
// RoleMergerVerb itself stays resident, and that is deliberate rather than
// forgotten: `gopi merger` answers "retired" and exits before installing any
// handler, so the classification costs a refusal, never an unbounded process.
func TestSignalRecForegroundMergeIsNotTheRetiredMerger(t *testing.T) {
	label := roleFromArgv([]string{"gopi", RoleMergeAlias, "--every", "5m"})
	if label == RoleMergerVerb {
		t.Fatalf("argv[1]=%q is still labelled %q — the FOREGROUND merge command inherits the retired daemon's policy and its process-keyed session dir", RoleMergeAlias, RoleMergerVerb)
	}
	if label != RoleMergeAlias {
		t.Errorf("roleFromArgv(gopi %s …) = %q, want %q — the label must name the command that actually ran", RoleMergeAlias, label, RoleMergeAlias)
	}
	if !forwardsDefault(label) {
		t.Errorf("forwardsDefault(%q) = false — `%s` is a foreground command: it owns no signal handler of its own, so it must keep SIGTERM's default death rather than being classified resident", label, RoleMergeAlias)
	}
	// The retired face keeps its own policy (see the test's doc comment).
	if forwardsDefault(RoleMergerVerb) {
		t.Errorf("forwardsDefault(%q) = true — the retired merger face must keep its classification: it refuses and exits", RoleMergerVerb)
	}
}

// TestSignalRecClaimedHandlerIsNotReRaised is the regression guard for epic
// 0x01a0d65f: a process whose graceful unwind is LONGER than the recorder's
// grace must not be killed by the recorder mid-unwind.
//
// The victim is the REAL failure shape -- Install at the bottom (as main.go
// does), then its OWN signal.Notify, then the ownership claim, with readiness
// announced over the same FIFO handshake the other victim arms use. Its argv is
// a BARE PROMPT (a mode word argv, which roleFromArgv cannot classify -- the
// reason the claim exists). Its unwind (srOwnerUnwind) is TWICE
// defaultForwardGrace, so the arm cannot be decided by scheduling luck: on the
// old predicate the re-raise lands halfway through and the completion marker is
// never written; with the claim the unwind finishes and the process exits
// srOwnerExitCode.
//
// RED before the fix: "the unwind never reached its end". GREEN after: marker
// present, exit code 1, and the provenance record still filed.
func TestSignalRecClaimedHandlerIsNotReRaised(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	home := srHome(t)
	// Forwarding must be ARMED for this child, or the test would pass for the
	// wrong reason (a disarmed recorder re-raises nobody).
	t.Setenv(EnvNoForward, "")
	dir := srSessionDir(t, home, "claimed-session")

	ready := srAnticipateChildReady(t, home)
	done := filepath.Join(home, "owner-unwind.done")
	cmd := exec.Command(os.Args[0], "start", "a long analysis and report back")
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		SessionDirEnv:    dir,
		EnvOff:           "",
		EnvNoForward:     "",
		srOwnerVictimEnv: "1",
		srOwnerDoneEnv:   done,
		srReadyEnv:       ready[srReadyEnv],
	})
	// Bind the victim to this test binary's lifetime before it starts:
	// the child must be armed with the death pipe this process holds, and
	// the guard (run after the readiness handshake) must find its verdict.
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecClaimedHandlerIsNotReRaised")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start owner victim child: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal owner victim child: %v", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("owner victim exited without a signal status (%v) -- expected the graceful arm to exit %d", err, srOwnerExitCode)
		}
		if got := ee.ExitCode(); got != srOwnerExitCode {
			t.Fatalf("owner victim exit = %d, want %d -- the recorder re-raised SIGTERM and killed the graceful unwind (exit 143/-1 is Go's default disposition)", got, srOwnerExitCode)
		}
	case <-time.After(srOwnerUnwind + 5*time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore teardown of the child that just failed the assertion; the t.Fatal below is the reported failure
		<-waitErr
		t.Fatal("owner victim never finished its graceful unwind -- the recorder's re-raise won a race it must lose when the process owns its handler")
	}

	body, err := os.ReadFile(done)
	if err != nil {
		t.Fatalf("read the unwind-completion marker %s: %v (the unwind never reached its end -- this is the RED condition the guard exists for)", done, err)
	}
	if !strings.Contains(string(body), srOwnerDonePrefix) {
		t.Errorf("unwind marker = %q, want it to carry %q", string(body), srOwnerDonePrefix)
	}

	// The claim suppresses the RE-RAISE only: the provenance record is still
	// filed. A fix that bought the graceful exit by costing the evidence would
	// undo the epic that put the recorder there.
	recs := srReadIn(t, dir)
	if len(recs) == 0 {
		t.Fatal("owner victim left no provenance record -- the claim must silence the re-raise, never the record")
	}
	if recs[0].Signal != "SIGTERM" {
		t.Errorf("recorded signal = %q, want SIGTERM", recs[0].Signal)
	}
}

// TestSignalRecWorkerStopArgvNeedsNoClaim is the RESIDUAL's negative, pinned on
// the DERIVATION rather than on the table's row (a row alone is a value a later
// edit can drop without anything going red — the shape Rule 13 exists for).
//
// THE FLAG IT ANSWERS (devdoc/signalrec-claim-vs-reraise.md, "Not fixed here"):
// `installWorkerStopSignals` (cmd/agent/cli_tasks_worker_stop.go) installs its
// own signal.Notify and does NOT call ClaimSignalHandlers, so it was flagged as
// possibly still re-raised. For that to be a defect, BOTH halves must hold:
//
//	① roleFromArgv must FAIL to classify the worker-stop argv — otherwise
//	   forwardsDefault is already false and signalForwarding() is false by
//	   construction, claim or no claim, and reRaise never runs;
//	② and it must do so for the argv the stop path actually runs under.
//
// ① is what this test pins, over the WHOLE argv grammar the worker face has
// (argv0 is discarded by roleFromArgv, so the two spellings of argv[0] the
// process can carry — the path a shell types, and the stable argv[0] a detached
// daemon is re-exec'd with — are the same derivation):
//
//	forwarding must be OFF for every worker argv, which is exactly
//	`forwardsDefault(roleFromArgv(argv)) == false`.
//
// It is deliberately NOT a heavytest: no child process, no device work, no
// signal — the property is a pure function of argv, and it is the property the
// production measurement (a real `gopi worker` under a real SIGTERM) rests on.
func TestSignalRecWorkerStopArgvNeedsNoClaim(t *testing.T) {
	workerArgvs := [][]string{
		// The two stable argv[0] spellings: what a shell/detached-child carries.
		{"/usr/local/bin/gopi", "worker"},
		{"gopi-worker", "worker"},
		// The FOREGROUND stop faces — both install installWorkerStopSignals.
		{"gopi", "worker", "--once"},
		{"gopi", "worker", "<task-id>"},
		// The DETACHED daemon (re-exec'd with --daemon consumed) — the argv the
		// residual's `worker --stop` actually signals.
		{"gopi-worker", "worker", "--once"},
		{"gopi-worker", "worker"},
	}
	for _, args := range workerArgvs {
		role := roleFromArgv(args)
		if role != procname.VerbWorker {
			t.Errorf("roleFromArgv(%q) = %q, want %q — the worker argv must be CLASSIFIED, not read as unclassifiable",
				strings.Join(args, " "), role, procname.VerbWorker)
		}
		if forwardsDefault(role) {
			t.Errorf("forwardsDefault(roleFromArgv(%q)) = true — a worker argv must never keep the re-raise armed, regardless of whether the caller claims its handlers",
				strings.Join(args, " "))
		}
	}
	// The HALF THAT KEEPS THE TEST HONEST: without the ownership claim the
	// predicate must be false for these argv because the ROLE owns handlers —
	// not because forwarding is globally disarmed in this environment (a test
	// that passed for that reason would pin nothing).
	t.Setenv(EnvNoForward, "")
	t.Setenv(envOwnsHandlers, "")
	prevArgv, prevClaimed := os.Args, handlersClaimed.Load()
	os.Args = workerArgvs[0]
	handlersClaimed.Store(false)
	t.Cleanup(func() {
		os.Args = prevArgv
		handlersClaimed.Store(prevClaimed)
	})
	if signalForwarding() {
		t.Error("signalForwarding() = true for a worker argv with NO claim — the recorder would re-raise SIGTERM one grace after the signal and kill the forwarder mid-awaitWorkerStopChild")
	}
}

// TestSignalRecNoProductionPathJoinsTheLegacySink is the exhaustive gate for the
// no-fallback contract (Rule 13's shape, applied to a PATH): the retired public
// sink may be spelled in exactly ONE place — legacySinkHint, which builds it only
// to REPORT it in a refusal — and nowhere in a write path. A future edit that
// re-introduces `filepath.Join(gd, LogDirName, LogFileName)` as a destination
// fails here instead of quietly re-splitting the evidence.
func TestSignalRecNoProductionPathJoinsTheLegacySink(t *testing.T) {
	src, err := os.ReadFile("signalrec.go")
	if err != nil {
		t.Fatalf("read package source: %v", err)
	}
	text := string(src)
	writes := []string{
		"filepath.Join(gd, LogDirName, LogFileName)",
		"filepath.Join(gd, LogDirName,",
	}
	for _, w := range writes {
		if n := strings.Count(text, w); n > 1 {
			t.Errorf("signalrec.go contains %q %d times — the legacy public sink must be spelled exactly once (in legacySinkHint, for the refusal message) and never as a write destination", w, n)
		}
	}
	// The legacy sink is never OPENED for writing: the only O_APPEND open in
	// the package must be the session-dir file.
	if !strings.Contains(text, "os.OpenFile(d.Path") {
		t.Error("the package no longer opens the resolved session-dir path — the write target changed shape without this gate being updated (the gate would then be vacuous)")
	}
}

// TestAwaitRecordFiledSettlesOnTheRecordAndNeverOnTheAttempt pins the waiter's
// contract, which the door-side handshake depends on in both directions:
//
//   - an ordinary exit (no signal) pays only the delivery settle and does NOT
//     claim the record landed — a waiter that answered true there would let a
//     door believe a record exists that nobody ever wrote;
//   - once the record IS filed (the publication step inside write), the wait
//     returns immediately, which is what makes the door's unwind short;
//   - with a signal received but no record filed, the wait EXPIRES at its bound
//     instead of hanging or answering true (the loss must be reportable).
func TestAwaitRecordFiledSettlesOnTheRecordAndNeverOnTheAttempt(t *testing.T) {
	// The in-process facts this test moves are process-global by design (this IS
	// the process's provenance state), so restore them.
	prevFiled := RecordFiledNow()
	prevVerdict := KillVerdictNow()
	t.Cleanup(func() {
		recordFiled.mu.Lock()
		if !prevFiled {
			recordFiled.at = time.Time{}
		}
		recordFiled.mu.Unlock()
		latestVerdict.mu.Lock()
		latestVerdict.v = prevVerdict
		latestVerdict.mu.Unlock()
	})

	// ARM 1 — nothing happened: no signal, no record. The answer is false and
	// the wait is the delivery settle, never the full budget.
	recordFiled.mu.Lock()
	recordFiled.at = time.Time{}
	recordFiled.mu.Unlock()
	latestVerdict.mu.Lock()
	latestVerdict.v = KillVerdict{}
	latestVerdict.mu.Unlock()

	start := time.Now()
	if AwaitRecordFiled(DefaultRecordFiledWait) {
		t.Fatalf("AwaitRecordFiled answered true with NO record filed and no signal — an ordinary exit must not claim the record landed")
	}
	if elapsed := time.Since(start); elapsed >= DefaultRecordFiledWait {
		t.Errorf("AwaitRecordFiled blocked %s with no signal to wait for — an ordinary exit must pay only the delivery settle (%s), not the budget", elapsed, recordFiledDeliverySettle)
	}

	// ARM 2 — the record landed: the wait returns true, immediately.
	noteRecordFiled()
	start = time.Now()
	if !AwaitRecordFiled(DefaultRecordFiledWait) {
		t.Fatalf("AwaitRecordFiled answered false although the record was filed")
	}
	if elapsed := time.Since(start); elapsed > DefaultRecordFiledWait/2 {
		t.Errorf("AwaitRecordFiled waited %s on a filed record — it is not settling on the publication step", elapsed)
	}

	// ARM 3 — a signal arrived and the record never did: the wait expires at its
	// bound (bounded, never a hang, never a false success).
	recordFiled.mu.Lock()
	recordFiled.at = time.Time{}
	recordFiled.mu.Unlock()
	bestEffortNoteSignalReceived()
	budget := 30 * time.Millisecond
	start = time.Now()
	if AwaitRecordFiled(budget) {
		t.Fatalf("AwaitRecordFiled answered true after a signal with NO record on disk — the loss would be reported as success")
	}
	if elapsed := time.Since(start); elapsed < budget {
		t.Errorf("AwaitRecordFiled returned after %s with a %s budget — it did not wait for the record at all", elapsed, budget)
	}
}

// bestEffortNoteSignalReceived arms the received half of the verdict for ARM 3
// without going through the signal machinery (a real signal would also be
// recorded by the installed handler and could not be scoped to this test).
func bestEffortNoteSignalReceived() { noteSignalReceived(SignalSIGTERM) }

// TestRecordOutcomePublishesTheThirdStateNeverAFalseLoss pins the publication
// side of the record-filed handshake's THIRD state, which the door-side guard
// (cmd/agent/signal_record_outcome_guard_test.go) consumes over real processes.
//
// The three states are the whole point: "the record did NOT land" has TWO causes
// a door must not conflate — the recorder never answered (a race the door can
// lose, a real loss) and the recorder DELIBERATELY REFUSED (an answer, already
// printed with its reason by reportUnfiled). Before this state existed the door
// could only see RecordFiledNow()==false and reported the second cause with the
// first one's sentence ("recorded NOWHERE"), a false loss claim for a rejection.
//
// Three contracts are pinned, and each one is a way a later change could break it:
//
//  1. a refusal publishes the refusal AND ITS REASON (the sentence reportUnfiled
//     prints), while RecordFiledNow() keeps answering false — the refusal is a
//     third state, never a redefinition of "filed";
//  2. a FILED record still wins over a refusal: bytes on disk are evidence, and a
//     process that wrote its record has lost nothing;
//  3. the zero value is "nothing yet" — the state a door must keep WAITING on, so
//     an unanswered recorder is never mistaken for a decision.
func TestRecordOutcomePublishesTheThirdStateNeverAFalseLoss(t *testing.T) {
	prevOutcome := RecordOutcomeNow()
	prevFiled := RecordFiledNow()
	prevReason := recordFiled.reason
	t.Cleanup(func() {
		recordFiled.mu.Lock()
		recordFiled.outcome = prevOutcome
		if !prevFiled {
			recordFiled.at = time.Time{}
		}
		recordFiled.reason = prevReason
		recordFiled.mu.Unlock()
	})

	// ARM 1 — a refusal, reported with its reason. Refusals are process-wide
	// (refuseOnce) by design, so the state is driven the way the caller drives it:
	// through reportUnfiled, which is where the decision is made.
	recordFiled.mu.Lock()
	recordFiled.at = time.Time{}
	recordFiled.outcome = RecordOutcomeNothingYet
	recordFiled.reason = ""
	recordFiled.mu.Unlock()

	refusal := errors.New("signalrec: cannot attribute this record: no session dir handed over — refusing to write (no public-dir fallback)")
	reportUnfiled(refusal, record{TS: "fixture", Kind: KindSignal, Signal: SignalSIGTERM})

	if !RecordOutcomeNow().Refused() {
		t.Fatalf("RecordOutcomeNow() = %q after a refusal, want the refusal state — a refused record that publishes nothing is the state the door used to read as a LOSS", RecordOutcomeNow())
	}
	if refused, reason := RecordRefusedNow(); !refused || reason != refusal.Error() {
		t.Errorf("RecordRefusedNow() = (%v, %q) — the refusal must carry the very reason reportUnfiled was handed (%q), so the door and the NOT FILED line cannot disagree", refused, reason, refusal.Error())
	}
	if RecordFiledNow() {
		t.Error("RecordFiledNow() answered true for a REFUSED record — its semantics are 'bytes on disk', unchanged; the refusal is the third state, not a redefinition of this one")
	}

	// ARM 2 — a filed record outranks a refusal (bytes on disk are the stronger
	// fact, and noteRecordRefused must not overwrite them).
	noteRecordFiled()
	if !RecordFiledNow() {
		t.Fatal("RecordFiledNow() answered false after noteRecordFiled — the publication step regressed")
	}
	noteRecordRefused("a second, later refusal must not overwrite a landed record")
	if RecordOutcomeNow().Refused() {
		t.Error("a refusal published AFTER a record landed overwrote the filed state — a process with bytes on disk has lost nothing and must not be reported as refused")
	}
	if !RecordFiledNow() {
		t.Error("the later refusal cleared the filed fact")
	}

	// ARM 3 — the zero value: nothing has been decided, which is what a door must
	// keep waiting on (and the one state an unanswered recorder leaves behind).
	recordFiled.mu.Lock()
	recordFiled.at = time.Time{}
	recordFiled.outcome = RecordOutcomeNothingYet
	recordFiled.reason = ""
	recordFiled.mu.Unlock()
	if got := RecordOutcomeNow(); got != RecordOutcomeNothingYet || got.Refused() {
		t.Errorf("RecordOutcomeNow() = %q on a fresh state — the nothing-yet state must stay distinguishable from both the filed and the refused answers", got)
	}
	if refused, reason := RecordRefusedNow(); refused || reason != "" {
		t.Errorf("RecordRefusedNow() = (%v, %q) with nothing published, want (false, \"\")", refused, reason)
	}
}
