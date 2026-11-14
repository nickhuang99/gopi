package main

// worker_list_alias_test.go — `gopi worker list`: the read-only enumeration
// under its NATURAL spelling, and the two gates a read-only alias must pass.
//
// WHY THE ALIAS EXISTS (epic worker-list-alias, 2026-10-16): `gopi worker
// --status` already answered "which workers are running?" — and the operator
// could not find it. A capability nobody can reach is a capability that does
// not exist, so the fix is a spelling an operator reaches for without reading
// help: `gopi worker list`.
//
// WHY TWO GATES ARE PINNED HERE (not one): the class was measured on the
// shipped binary 2026-09-14 with the sibling read-only alias
// (workerStatusResidueVerboseFlag) — an alias that opened the ROUTE but was
// never classified READ-ONLY made the read+act refusal blind, so
// `… --status-verbose --stop` printed the report and exited 0 with the act
// silently DROPPED (the F1 defect: a monitor concludes the service was
// stopped; it was not). A POSITIONAL alias adds a second, worse failure mode:
// a bare positional that misses the control route reaches the worker loop's
// scanner, where it is read as an EXPLICIT TASK ID — `gopi worker list` would
// CLAIM AND RUN a row called "list". Both are pinned below, and the CLI-level
// cases drive the REAL binary (a unit assertion over the parser cannot show
// that the routing in main.go runs the reader instead of the loop).

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

// --- the two gates, at the parser ------------------------------------------

// TestWorkerListAliasOpensBothGates pins the ROUTING gate (the word opens the
// control route) and the ROLE gate (the word classifies READ-ONLY), plus the
// two consequences the operator feels: the alias carries the SAME action as
// --status, and a read beside an act is still REFUSED.
func TestWorkerListAliasOpensBothGates(t *testing.T) {
	// ROUTING: every spelling of the read must open the control route. A bare
	// positional that does not is scanned as a task id by the loop below.
	for _, argv := range [][]string{
		{workerControlList},
		{workerControlStatus},
		{workerControlList, workerStatusResidueVerboseFlag},
	} {
		if !workerControlRequested(argv) {
			t.Fatalf("argv %q never opens the control route — a bare positional there is read as a TASK ID, so `%s %s` would claim and run a row",
				argv, CliVerbWorker, workerControlList)
		}
		if !workerStatusRequested(argv) {
			t.Fatalf("argv %q is not recognized as the read-only status query", argv)
		}
	}

	// ONE action, two spellings: the alias must not be a second action (that
	// would make `list --status` a multi-act conflict for what is one read).
	listOpts, listOK := workerControlParse([]string{workerControlList})
	statusOpts, statusOK := workerControlParse([]string{workerControlStatus})
	if !listOK || !statusOK {
		t.Fatalf("parse: list ok=%v, --status ok=%v (both must name the read action)", listOK, statusOK)
	}
	if listOpts.action != workerActionStatus || listOpts.action != statusOpts.action {
		t.Fatalf("the alias must carry the SAME action as %s: list=%q, --status=%q, want %q",
			workerControlStatus, listOpts.action, statusOpts.action, workerActionStatus)
	}
	if msg := controlConflictMessage(workerControlConflictParse(nil, listOpts.actions, listOpts.read, listOpts.act, listOpts.flags)); msg != "" {
		t.Fatalf("%s alone must be unambiguous, got refusal %q", workerControlList, msg)
	}
	both, _ := workerControlParse([]string{workerControlList, workerControlStatus})
	if msg := controlConflictMessage(workerControlConflictParse(nil, both.actions, both.read, both.act, both.flags)); msg != "" {
		t.Fatalf("both spellings of ONE read must not be a conflict, got %q", msg)
	}

	// ROLE: read-only, so the read+act refusal sees it (the F1 defect).
	if got := workerControlRoleOf(workerControlList); got != controlActionReadOnly {
		t.Fatalf("the alias role is %v, want controlActionReadOnly — an unclassified read makes the read+act refusal blind", got)
	}
	if got := workerControlRoleOf(workerControlStatus); got != controlActionReadOnly {
		t.Fatalf("the flag spelling's role changed: %v", got)
	}
	if got := workerControlRoleOf(workerControlStop); got != controlActionActs {
		t.Fatalf("an acting flag must stay classified as an act: %v", got)
	}

	// The consequence: a read beside an act is refused with BOTH words named,
	// never a silent no-op with a success exit code.
	for _, argv := range [][]string{
		{workerControlList, workerControlStop},
		{workerControlList, workerControlRestart},
		{workerControlStop, workerControlList},
		{workerControlList, workerStatusResidueVerboseFlag, workerControlStop},
	} {
		opts, _ := workerControlParse(argv)
		msg := controlConflictMessage(workerControlConflictParse(argv, opts.actions, opts.read, opts.act, opts.flags))
		if msg == "" {
			t.Fatalf("argv %q must be refused as read+act — an unclassified alias drops the act and exits 0", argv)
		}
		// The ACT must be named (that is the word the operator has to remove —
		// the F1 defect was the act disappearing from BOTH the effect and the
		// message). The read-only words need not appear: the refusal names the
		// conflicting PAIR, and several read-only spellings of one read are not
		// several conflicts.
		for _, word := range argv {
			if workerControlRoleOf(word) != controlActionActs {
				continue
			}
			if !strings.Contains(msg, word) {
				t.Fatalf("refusal %q does not name the acting flag %q (an act must never be dropped silently)", msg, word)
			}
		}
	}
}

// TestWorkerListAliasIsDiscoverableInBothUsageFaces pins the DISCOVERABILITY
// half of the epic: the alias is useless if it is not on the usage page, and
// the page an operator reaches for it from (`gopi worker -h`) and the page the
// alias itself answers (`gopi worker list -h`) are the SAME page.
func TestWorkerListAliasIsDiscoverableInBothUsageFaces(t *testing.T) {
	if !strings.Contains(workerUsageHead, CliVerbWorker+" "+workerControlList) {
		t.Fatalf("the usage head does not spell `%s %s` — the alias would exist without being findable:\n%s",
			CliVerbWorker, workerControlList, workerUsageHead)
	}
	// The word is recognized as a VERB WORD of this face (the pre-check that
	// makes `list -h` a usage query rather than a positional task id).
	if !workerVerbWordNamesStatus([]string{workerControlList}) {
		t.Fatalf("%q is not recognized as the verb word of the worker face", workerControlList)
	}
	if workerVerbWordNamesStatus([]string{workerControlStop}) {
		t.Fatalf("%q must not be read as a verb word (it is a flag)", workerControlStop)
	}
	if workerVerbWordNamesStatus(nil) {
		t.Fatal("an empty argv has no verb word")
	}
	for _, argv := range [][]string{{workerFlagHelp}, {workerFlagHelpLong}} {
		if !workerHelpRequested(argv) {
			t.Fatalf("argv %q is not recognized as a usage query", argv)
		}
	}
	if workerHelpRequested([]string{workerControlList}) {
		t.Fatal("`list` alone is a read, not a usage query")
	}
}

// --- the same read, through the REAL binary --------------------------------

// wlrResidueFixturePID is the DEAD pid the alias case plants a state record
// for. Dead on purpose: the record must be counted as residue and must never be
// rendered as a worker, and neither fact needs a live process.
const wlrResidueFixturePID = 999999

// TestWorkerListAliasCLIPrintsTheStatusReport byte-compares the two spellings on
// the SHIPPED binary, in a private profile: the property is "the alias answers
// with the SAME enumeration", which only an end-to-end run can show (the route
// lives in main.go, and a unit test of the parser would pass while main.go ran
// the loop instead).
func TestWorkerListAliasCLIPrintsTheStatusReport(t *testing.T) {
	bin := gopiTestBinPath(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	// The read the profile backs: a residue record (a pid that is long gone) so
	// the report has a second block whose rendering could differ between the two
	// spellings if they were not the same reader.
	t.Setenv("GOPI_HOME", home)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve worker state dir: %v", err)
	}
	if werr := os.WriteFile(filepath.Join(dir, workerStateFilePrefix+itoa(wlrResidueFixturePID)),
		[]byte(`{"pid":`+itoa(wlrResidueFixturePID)+`,"state":"idle"}`), 0o644); werr != nil {
		t.Fatalf("plant residue record: %v", werr)
	}

	env := wsrStatusEnv(t, home)
	listCode, listOut := wlrRun(t, env, bin, CliVerbWorker, workerControlList)
	statusCode, statusOut := wlrRun(t, env, bin, CliVerbWorker, workerControlStatus)
	if listCode != statusCode {
		t.Fatalf("exit codes differ: list=%d, --status=%d\nlist:\n%s\n--status:\n%s",
			listCode, statusCode, listOut, statusOut)
	}
	if listOut != statusOut {
		t.Fatalf("the alias and %s must be the SAME read, byte for byte:\nlist:\n%q\n--status:\n%q",
			workerControlStatus, listOut, statusOut)
	}
	// And it really was the status report (not an empty stdout from a route
	// that silently did nothing) — measured as THIS profile's ENUMERATION.
	//
	// The assertion used to be `strings.Contains(listOut, workerStatusNoWorkers)`,
	// i.e. a substring over the whole page. That page is host-wide by design: a
	// worker-shaped process belonging to another run, or one whose own
	// environment names no profile at all (which the scan deliberately COUNTS
	// rather than drops — fallbackWorkerPIDs' degradation), renders a worker
	// line, the empty-page line never appears, and the case went red on master
	// 76e6faf for a fact this fixture does not own (measured 2026-10-24). The
	// question here is "did the alias route reach the one reader, and did that
	// reader enumerate THIS profile?" — both are answerable without the page.
	docs := make([]workerStatusJSONDoc, 0, 2)
	for _, spelling := range []string{workerControlList, workerControlStatus} {
		doc := workerStatusMachineDoc(t, bin, env, CliVerbWorker, spelling)
		workerStatusAssertOwnProfileEnumeration(t, doc, home)
		docs = append(docs, doc)
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatalf("the alias and %s must be the SAME enumeration:\nlist: %+v\n--status: %+v",
			workerControlStatus, docs[0], docs[1])
	}
	// The planted record names a DEAD pid (999999): the reader must COUNT it as
	// residue and must NOT render it as a worker — the same "the file says the
	// pid, the process says whether it is one" rule that keeps ghost workers off
	// the page. Both facts are about THIS profile's enumeration, so neither
	// depends on what else the host is running.
	if docs[0].ResidueCount == 0 {
		t.Fatalf("the planted residue record was not counted (%+v) — this profile's enumeration is not the one under test",
			docs[0])
	}
	for _, doc := range docs {
		if containsWorkerPID(doc, wlrResidueFixturePID) {
			t.Fatalf("the dead-pid record was rendered as a worker of this profile: %+v", doc.Workers)
		}
	}
	// READ-ONLY by contract: the residue file is still there afterwards.
	if _, serr := os.Stat(filepath.Join(dir, workerStateFilePrefix+itoa(wlrResidueFixturePID))); serr != nil {
		t.Fatalf("the read-only alias wrote or removed something: %v", serr)
	}
}

// TestWorkerListAliasHelpIsTheWorkerUsage pins `gopi worker list -h`: the
// alias's own help is the worker usage page (a usage query, exit 0, claiming
// nothing — the P0-1 contract), so the word is discoverable from both ends.
func TestWorkerListAliasHelpIsTheWorkerUsage(t *testing.T) {
	bin := gopiTestBinPath(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	code, out := wlrRun(t, wsrStatusEnv(t, home), bin, CliVerbWorker, workerControlList, workerFlagHelp)
	if code != 0 {
		t.Fatalf("`%s %s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlList, workerFlagHelp, code, out)
	}
	if out != workerUsageHead {
		t.Fatalf("`%s %s %s` must print the worker usage page verbatim\n got:\n%s\nwant:\n%s",
			CliVerbWorker, workerControlList, workerFlagHelp, out, workerUsageHead)
	}
}

// wlrRun drives the shipped binary with an explicit argv and returns its exit
// code plus combined output.
func wlrRun(t *testing.T, env []string, bin string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run %q: %v\n%s", args, err, out)
		}
		code = ee.ExitCode()
	}
	return code, string(out)
}

// --- CONSISTENCY: the pid `--status` prints is the pid `--stop --pid` takes ---

// TestWorkerStatusPIDIsAcceptedByStopPID pins the epic's consistency clause on
// the REAL binary: the pid the enumeration PRINTS is the pid the stop selector
// ACCEPTS. They are two different readers (the enumeration reads state files
// plus /proc; the selector's shape test checks the daemon records and the
// foreground shape), so "the operator can copy the pid out of the listing and
// paste it into --stop" is a claim that has to be measured, not assumed.
//
// The fixture is a private profile plus a SYNTHETIC procfs root
// (procname.EnvProcRoot — the same seam the enumeration's own tests use) with a
// REAL live pid behind it: liveness is read from the real /proc
// (tools.PidAlive), while the process SHAPE (image, argv, session) comes from
// the synthetic tree. That is what lets one pid be simultaneously
// worker-shaped and harmlessly disposable — it is a /bin/sleep child, so no
// worker work exists to disturb.
func TestWorkerStatusPIDIsAcceptedByStopPID(t *testing.T) {
	bin := gopiTestBinPath(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	procRoot := t.TempDir()

	// A live, disposable pid whose synthetic shape is a DETACHED gopi worker.
	child := exec.Command("/bin/sleep", workerStatusPIDFixtureSleepSec)
	// A direct child: Pdeathsig covers it even when the test binary is SIGKILLed,
	// and its cwd is not the fixture's subject (the synthetic /proc shape is).
	child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	child.Dir = t.TempDir()
	if err := child.Start(); err != nil {
		t.Fatalf("spawn the fixture process: %v", err)
	}
	t.Cleanup(func() {
		if child.Process != nil {
			_ = child.Process.Kill()    //stylegate:ignore best-effort kill of the fixture child in t.Cleanup; the assertion is about pid ACCEPTANCE, not its fate
			_, _ = child.Process.Wait() //stylegate:ignore reap of the fixture child in t.Cleanup — the exit status is not what this test asserts
		}
	})
	pid := child.Process.Pid
	procFixture(t, procRoot, pid, binaryNameGopi,
		[]string{binaryNameGopi, procname.VerbWorker}, pid) // session == pid ⇒ detached

	env := append(wsrStatusEnv(t, home), procname.EnvProcRoot+"="+procRoot)

	// ① what the enumeration prints.
	code, out := wlrRun(t, env, bin, CliVerbWorker, workerControlStatus)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}
	printed := wlrPIDInWorkerLine(t, out)
	if printed != pid {
		t.Fatalf("the listing named pid %d, want the fixture's %d:\n%s", printed, pid, out)
	}

	// ② the pid the SELECTOR accepts, read-only (the same shape test the stop
	// arm uses, with no request written anywhere).
	code, out = wlrRun(t, env, bin, CliVerbWorker, workerControlStatus, workerControlPIDFlag, strconv.Itoa(printed))
	if code != workerControlExitOK {
		t.Fatalf("`%s %s %s %d` exited %d — the printed pid was refused by the selector:\n%s",
			CliVerbWorker, workerControlStatus, workerControlPIDFlag, printed, code, out)
	}
	// The selector must NAME the resolved pid AND its shape: a resolved target
	// that renders as a blank line reads as "the selector reached nothing", which
	// is the one answer this face must never give silently (Rule 15 — found by
	// this very case, 2026-10-16, on the shape that has no state record).
	if !strings.Contains(out, strconv.Itoa(printed)) {
		t.Fatalf("the selector did not name the printed pid %d:\n%s", printed, out)
	}
	if !strings.Contains(out, workerControlShapeDetached) && !strings.Contains(out, workerControlShapeForeground) &&
		!strings.Contains(out, workerControlShapeDaemon) {
		t.Fatalf("the selector named no shape for pid %d — the report is blank:\n%s", printed, out)
	}

	// ③ the STOP arm accepts the very same pid. `--wait 0` is deliberate: this
	// fixture is not a real worker, so waiting for it to leave would only burn
	// the bound — what is asserted is the ACCEPTANCE (the pid is addressed),
	// and that the delivery is file-only, which is what keeps a signal off a
	// process whose binary is not this one.
	code, out = wlrRun(t, env, bin, CliVerbWorker, workerControlStop, workerControlPIDFlag,
		strconv.Itoa(printed), workerControlWait, "0")
	if code != workerControlExitFailed {
		t.Fatalf("`%s %s %s %d %s 0` exited %d, want %d (the request is delivered, the end state is not observed with no wait):\n%s",
			CliVerbWorker, workerControlStop, workerControlPIDFlag, printed, workerControlWait,
			code, workerControlExitFailed, out)
	}
	for _, refusal := range []string{wlrDeadRefusalMarker, wlrNotWorkerRefusalMarker} {
		if strings.Contains(out, refusal) {
			t.Fatalf("the pid %d that %s printed was REFUSED by %s (%q appears):\n%s",
				printed, workerControlStatus, workerControlStop, refusal, out)
		}
	}
	if !strings.Contains(out, "pid "+strconv.Itoa(printed)) {
		t.Fatalf("the stop arm never named the pid %d the listing printed:\n%s", printed, out)
	}
	// THE SIGNAL IS OFF: the target runs another binary, so the delivery is the
	// durable request file (and the fixture process is still alive).
	//
	// THE ASSERTION READS THE PRODUCTION CONSTANT'S OWN SENTENCE, not a phrase
	// this test remembers (measured 2026-10-28, a02af69): the stop channel was
	// re-cut so the delivery criterion is the ACK(req_id), and the old
	// "… requesting BY FILE ONLY" wording went with the guessing it encoded.
	// The pinned fact is unchanged — the file IS the delivery and NO signal
	// accompanies it — so the check names that clause, which is what
	// workerForegroundForeignImageFmt prints ("… so no signal accompanies the
	// request; the request file is the delivery and the receipt is its proof").
	for _, clause := range []string{
		workerStopNoSignalClause,
		workerStopFileIsDeliveryClause,
	} {
		if !strings.Contains(out, clause) {
			t.Fatalf("the stop arm must deliver by FILE for a target whose binary it cannot prove "+
				"(missing %q — no signal may accompany the request, and the file must be named as the delivery):\n%s",
				clause, out)
		}
	}
	if !tools.PidAlive(pid) {
		t.Fatalf("the fixture pid %d was signalled — the delivery must not signal an unproven binary", pid)
	}
}

// wlrDeadRefusalMarker / wlrNotWorkerRefusalMarker are the two substrings that
// would appear if the stop arm had REJECTED a pid the listing printed: the
// gone-holder refusal's own wording, and the alive-but-not-a-worker refusal's.
// They are matched as the refusal's MEASURED CLAIM rather than as a format
// string (the operands differ per case), so the assertion is about what the
// operator would read, not about a template.
const (
	wlrDeadRefusalMarker      = "is not running — nothing to address"
	wlrNotWorkerRefusalMarker = "is alive but is NOT a"
	// workerStopNoSignalClause / workerStopFileIsDeliveryClause are the two
	// facts the stop channel promises a caller holding a pid it cannot prove:
	// NO signal is sent, and the durable REQUEST FILE is the delivery. They are
	// spelled as the passed-over prose both the production format and this test
	// read, so a re-cut of the wording that keeps the promise keeps passing,
	// while one that drops either half goes red by name.
	workerStopNoSignalClause       = "no signal accompanies the request"
	workerStopFileIsDeliveryClause = "the request file is the delivery"
)

// wlrPIDInWorkerLine returns the pid of the first worker line the report
// printed (the "<prog> pid N:" shape the enumeration renders), failing the test
// when there is none — an empty listing is not a pass.
func wlrPIDInWorkerLine(t *testing.T, out string) int {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(workerProgTopLevel) + ` pid ([0-9]+):`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the listing named no worker (no %q line):\n%s", workerProgTopLevel+" pid N:", out)
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse the listed pid from %q: %v", m[0], err)
	}
	return pid
}

// workerStatusPIDFixtureSleepSec bounds the fixture process's life: long enough
// that the three CLI runs cannot outlive it, short enough that a leaked child
// cannot outlive the session (the cleanup kills it first in any case).
const workerStatusPIDFixtureSleepSec = "120"
