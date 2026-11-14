// worker_control_selector_test.go — the regression guard for the control face's
// TARGET SELECTORS (`gopi worker --stop --branch <B>`, `--stop --pid <n>`).
//
// THE BUG (epic, measured 2026-10-13): `gopi worker --stop --branch <B>` was
// accepted and IGNORED. `--branch` is the RESUME family's ground selector; on
// the control face no reader existed, so the invocation fell through to the
// unselected enumeration fallback — which stops EVERY foreground worker of the
// profile, or none. The operator named one worker and the command acted on
// another set entirely, printing nothing about the substitution.
//
// WHAT IS PINNED HERE, one assertion per fact:
//
//	1. the WORD IS ROUTED — it reaches the control face at all (instead of being
//	   read as the resume mode's ground);
//	2. the SELECTOR RESOLVES through the ledger (row → claim stamp → pid), and
//	   EVERY way it can fail names WHICH failure;
//	3. NO SELECTOR FALLS BACK to the enumeration — the defect's own shape;
//	4. a bare selector (no action) is REFUSED with the actions listed.
//
// The RED PROOF lives at the bottom (TestWorkerControlSelectorRedProof): it
// rebuilds the OLD routing predicate and shows it loses the word, which is the
// red/green the fix flips.

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// --- (1) the word is routed -------------------------------------------------

// TestWorkerControlSelectorWordsRouteToTheControlFace pins the ROUTING half:
// both selector spellings must send the invocation to the control face, in the
// `--flag value` and `--flag=value` forms alike. Before the fix the words were
// absent from this predicate's vocabulary, so `--stop --branch B` read as
// "resume the ground B" (the only reader of --branch) with the stop discarded.
func TestWorkerControlSelectorWordsRouteToTheControlFace(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"stop+branch spaced", []string{workerControlStop, workerControlBranchFlag, "feat/x"}},
		{"stop+branch equals", []string{workerControlStop, workerControlBranchFlag + workerFlagValueSep + "feat/x"}},
		{"stop+pid spaced", []string{workerControlStop, workerControlPIDFlag, "4242"}},
		{"status+branch", []string{workerControlStatus, workerControlBranchFlag, "feat/x"}},
		{"restart+branch", []string{workerControlRestart, workerControlBranchFlag, "feat/x"}},
		{"bare branch (no action)", []string{workerControlBranchFlag, "feat/x"}},
		{"bare pid (no action)", []string{workerControlPIDFlag, "4242"}},
	} {
		if !workerControlRequested(tc.args) {
			t.Fatalf("%s: %v must route to the control face — it is now a selected invocation", tc.name, tc.args)
		}
	}
	// The near-miss stays out: `--worktree` is the RESUME family's ground
	// selector and the control face must not claim it (the two families are
	// distinct vocabularies; only `--branch` is shared, and it is now owned by
	// this face when an action rides along).
	if workerControlRequested([]string{workerFlagResume, tools.TaskResumeGroundWorktreeFlag, "/tmp/g"}) {
		t.Fatal("a plain `--resume --worktree P` must NOT route to the control face")
	}
}

// TestWorkerControlSelectorParsedSelections pins the PARSER: which operand each
// spelling yields, and that the two selectors together are refused (a pid and a
// branch can name different workers — an ordering rule would be a silent
// substitution, the very defect this epic is about).
func TestWorkerControlSelectorParsedSelections(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		pid        int
		branch     string
		wantErr    bool
		wantRefuse string
	}{
		{name: "pid spaced", args: []string{workerControlStop, workerControlPIDFlag, "4242"}, pid: 4242},
		{name: "pid equals", args: []string{workerControlStop, workerControlPIDFlag + workerFlagValueSep + "4242"}, pid: 4242},
		{name: "branch spaced", args: []string{workerControlStop, workerControlBranchFlag, "feat/x"}, branch: "feat/x"},
		{name: "branch equals", args: []string{workerControlStop, workerControlBranchFlag + workerFlagValueSep + "feat/x"}, branch: "feat/x"},
		{name: "no selector", args: []string{workerControlStop}},
		{name: "pid without value", args: []string{workerControlStop, workerControlPIDFlag}, wantErr: true, wantRefuse: "needs a value"},
		{name: "branch without value", args: []string{workerControlStop, workerControlBranchFlag}, wantErr: true, wantRefuse: "needs a value"},
		{name: "pid is not a number", args: []string{workerControlStop, workerControlPIDFlag, "abc"}, wantErr: true, wantRefuse: "not a positive process id"},
		{name: "pid repeated", args: []string{workerControlStop, workerControlPIDFlag, "1", workerControlPIDFlag, "2"}, wantErr: true, wantRefuse: "repeated"},
		{name: "both selectors", args: []string{workerControlStop, workerControlPIDFlag, "1", workerControlBranchFlag, "feat/x"}, wantErr: true, wantRefuse: "name ONE target two ways"},
	} {
		sel, err := workerControlSelectionFrom(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: %v must be REFUSED, got selection %+v", tc.name, tc.args, sel)
			}
			sentence := workerControlSelectionRefusal(err)
			if !strings.Contains(sentence, tc.wantRefuse) {
				t.Fatalf("%s: the refusal must carry %q:\n%s", tc.name, tc.wantRefuse, sentence)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected refusal: %v", tc.name, err)
		}
		if sel.pid != tc.pid || sel.branch != tc.branch {
			t.Fatalf("%s: parsed pid=%d branch=%q, want pid=%d branch=%q", tc.name, sel.pid, sel.branch, tc.pid, tc.branch)
		}
		if wantApplied := tc.pid != 0 || tc.branch != ""; sel.applied != wantApplied {
			t.Fatalf("%s: applied=%v, want %v", tc.name, sel.applied, wantApplied)
		}
	}
}

// TestWorkerControlSelectorWithoutActionIsRefused pins the actionless refusal
// for the word THIS face owns: a target with nothing to do to it names the
// three actions rather than falling into the loop's scanner ("unexpected
// argument --pid" — the message that used to be the operator's only clue that
// the selector did nothing).
//
// THE `--pid` SPELLING IS NOT INTERCHANGEABLE WITH `--branch` HERE, and this
// test is the place that says so: `--branch` is the RESUME family's ground
// selector and its bare spelling DEFERS to that face (see
// TestWorkerControlSelectorDeferralDiscriminator), while `--pid` has no other
// reader in the tree — so `--pid` is the spelling that pins the refusal itself.
func TestWorkerControlSelectorWithoutActionIsRefused(t *testing.T) {
	sentence, refuse, applied, deferred := workerControlSelectionGate([]string{workerControlPIDFlag, "12"}, "")
	if !refuse || applied || deferred {
		t.Fatalf("an actionless pid selector must be refused (refuse=%v applied=%v deferred=%v)", refuse, applied, deferred)
	}
	for _, want := range []string{workerControlPIDFlag, workerControlSelectorNoActionFragment, workerControlStop, workerControlStatus} {
		if !strings.Contains(sentence, want) {
			t.Fatalf("the actionless refusal must carry %q:\n%s", want, sentence)
		}
	}
	// With an action it is a legitimate selection, routed onward.
	if _, refuse, applied, _ := workerControlSelectionGate(
		[]string{workerControlStop, workerControlBranchFlag, "feat/x"}, workerActionStop); refuse || !applied {
		t.Fatalf("`--stop --branch feat/x` must be a legitimate selection (refuse=%v applied=%v)", refuse, applied)
	}
	if _, refuse, applied, _ := workerControlSelectionGate(
		[]string{workerControlStop, workerControlPIDFlag, "12"}, workerActionStop); refuse || !applied {
		t.Fatalf("`--stop --pid 12` must be a legitimate selection (refuse=%v applied=%v)", refuse, applied)
	}
}

// --- the deferral discriminator (epic worker-branch-bare-resume-face) --------

// TestWorkerControlSelectorDeferralDiscriminator pins the ONE argv shape where a
// selector arrives at this gate WITHOUT an action and this gate must not answer
// it, and — just as load-bearing — the shapes that only LOOK like it.
//
// THE DEFECT (measured): `gopi worker --branch feat/x` is the resume family's
// command line with its mode switch missing, and cli_worker_resume.go already
// refuses exactly that with the sentence that teaches the repair ("`--branch` is
// the GROUND of a resume — it means nothing without `--resume`"). This gate
// answered first with "names a TARGET but no ACTION", whose three remedies
// (`--status`/`--stop`/`--restart`) cannot start a resume at all — so the
// operator was sent to a second refusal. The gate must DEFER.
//
// It is a table rather than a single assertion because the near-misses are the
// whole risk: the two earlier shapes of this fix both broke something, and each
// row below is one of those failures (see the case comments). The E2E rehearsal
// of the same table — a real binary, the real sentence on stderr — is
// TestWorkerResumeByTaskIDRefusesNamesTheRepair (the "a ground flag without the
// mode" case), which is the row this epic's acceptance names.
func TestWorkerControlSelectorDeferralDiscriminator(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		action       workerControlAction
		wantRefuse   bool
		wantApplied  bool
		wantDeferred bool
		why          string
	}{
		{
			name: "bare branch defers to the resume face", args: []string{workerControlBranchFlag, "feat/x"},
			wantDeferred: true,
			why:          "the word is the resume family's ground selector: its bare spelling is that face's to refuse, and its sentence is the one that teaches `--resume`",
		},
		{
			name: "bare branch, equals spelling", args: []string{workerControlBranchFlag + workerFlagValueSep + "feat/x"},
			wantDeferred: true,
			why:          "the deferral is a fact about the WORD, not about the spacing: `--branch=feat/x` is the same word and the resume scanner reads it too",
		},
		{
			name: "bare pid still refuses here", args: []string{workerControlPIDFlag, "12"},
			wantRefuse: true,
			why:        "no other face reads `--pid` — deferring it would hand the word to a face that has no sentence for it, and the operator would get no refusal at all",
		},
		{
			name: "branch WITH an action is a selection", args: []string{workerControlStop, workerControlBranchFlag, "feat/x"},
			action: workerActionStop, wantApplied: true,
			why: "`--stop --branch B` is the leaf epic's whole feature — an early return on `branch != \"\"` swallowed it and handed the stop back to the resume face, which cannot stop anything",
		},
		{
			name: "pid WITH an action is a selection", args: []string{workerControlStop, workerControlPIDFlag, "12"},
			action: workerActionStop, wantApplied: true,
			why: "the action is the other half of the conjunction: a deferral keyed on the selector alone would take this face's own spelling away from it",
		},
		{
			name: "read-only status with a branch is a selection", args: []string{workerControlStatus, workerControlBranchFlag, "feat/x"},
			action: workerActionStatus, wantApplied: true,
			why: "--status is an action: the selected read reports the ONE resolved target, the unselected read enumerates (the substitution the leaf epic ends)",
		},
		{
			name: "a malformed branch still refuses here", args: []string{workerControlBranchFlag},
			wantRefuse: true,
			why:        "a vocabulary question (whose word is it) must not swallow a grammar question (it has no operand): the keyword is there and unusable, and this gate is where that is said",
		},
		{
			name: "no selector at all", args: []string{workerControlStop},
			action: workerActionStop,
			why:    "the ordinary unselected invocation reaches neither arm",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sentence, refuse, applied, deferred := workerControlSelectionGate(tc.args, tc.action)
			if refuse != tc.wantRefuse || applied != tc.wantApplied || deferred != tc.wantDeferred {
				t.Fatalf("%v (action=%q): refuse=%v applied=%v deferred=%v, want refuse=%v applied=%v deferred=%v\n  %s",
					tc.args, tc.action, refuse, applied, deferred, tc.wantRefuse, tc.wantApplied, tc.wantDeferred, tc.why)
			}
			if refuse && sentence == "" {
				t.Fatalf("%v: a refusal must carry a sentence", tc.args)
			}
			if !refuse && sentence != "" {
				t.Fatalf("%v: only a refusal has a sentence to print, got %q", tc.args, sentence)
			}
			// The deferral is a SILENT hand-over: the resume face's sentence is
			// reachable only if this gate put nothing on the wire. If it ever
			// starts rendering one, the operator reads this face's words about
			// another face's command line — the defect, one layer down.
			if deferred && sentence != "" {
				t.Fatalf("%v: a deferred invocation must print NOTHING (the resume face answers), got %q", tc.args, sentence)
			}
		})
	}

	// The union shape that reddened the naive one-line patch: a branch beside a
	// PID-less argv is the ONLY deferral, and the same test run against a
	// discriminator of `sel.branch != ""` alone would report `--pid 12` deferred
	// (the row above). Stated as its own assertion so the pair cannot be read
	// apart: exactly one of the two bare spellings defers.
	var deferredWords []string
	for _, argv := range [][]string{{workerControlPIDFlag, "12"}, {workerControlBranchFlag, "feat/x"}} {
		if _, _, _, d := workerControlSelectionGate(argv, ""); d {
			deferredWords = append(deferredWords, argv[0])
		}
	}
	if len(deferredWords) != 1 || deferredWords[0] != workerControlBranchFlag {
		t.Fatalf("exactly the branch spelling must defer with no action, got %v", deferredWords)
	}
}

// --- (2)+(3) resolution, and no fallback ------------------------------------

// wcsOwnLedger gives the calling test a ledger of its OWN (Rule 32: the path
// comes from the testutil scaffold, never hand-rolled) and pins the two env
// spellings at it, so every tools call below resolves THIS file.
//
// WHY IT IS NOT OPTIONAL (measured 2026-10-27, /dev/shm/suite-cmd-agent-run-
// 308907003): a test that leaves the ledger ambient inherits the suite's SHARED
// ledger file, and that file is not a knowledge base when these cases run —
// `gopi task: …/suite-ledger/memory.db is not a distilled knowledge base
// (missing distilled_qa table)`. The shared file had exactly 17 schema objects,
// all of them the chat index's (chats + chats_fts + chats_tri + their indexes
// and triggers) and none of them distilled_qa: whichever surface opened the
// suite's ledger first had created it through the CHAT opener, which builds only
// its own tables. These fixtures WRITE rows, so they must write them into a
// database whose schema is theirs.
//
// Called ONCE at the top of a test, never per row: a second call would repoint
// GOPI_MEMORY_DB at a fresh file and the rows seeded before it would be gone.
func wcsOwnLedger(t *testing.T) {
	t.Helper()
	testutil.NewTestDBPath(t)
}

// wcsSeedOwnedRow writes a row into the profile's own ledger whose deliverable
// names `branch`, and returns its id. It is the writer half of the
// branch→row→claim-stamp chain the `--branch` selector reads; the stamp itself
// is applied by the cases below, because WHICH stamp (live / foreign / dead /
// absent) is the variable under test.
func wcsSeedOwnedRow(t *testing.T, branch string) string {
	t.Helper()
	repo := wcsFixtureRepo(t)
	_, id, err := tools.TaskSubmitReturningID(tools.TaskSubmitOptions{
		Summary: "worker control selector fixture row",
		Payload: "a row that owns " + branch,
		Source:  "human",
		Repo:    repo,
	})
	if err != nil {
		t.Fatalf("seed row for %s: %v", branch, err)
	}
	if err := tools.MergeVerifySetDeliverableBranchForTest(id, repo, branch); err != nil {
		t.Fatalf("stamp branch %s on row %s: %v", branch, id, err)
	}
	if _, err := tools.TaskForBranch(tools.TaskForBranchQuery{Branch: branch}, ""); err != nil {
		t.Fatalf("the fixture row is not readable through the selector's own reader: %v", err)
	}
	return id
}

// wcsFixtureRepo is a git repo path the submit seam accepts (it verifies the
// repo before writing the row). This package's own checkout is one.
func wcsFixtureRepo(t *testing.T) string {
	t.Helper()
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs cwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

// wcsLocalHolder is the claim stamp a REAL local claim writes: `<hostname>:<pid>`
// (claimHolder in internal/tools parses exactly this, and compares the host
// against os.Hostname — a fixture that invented the host side would test the
// parser's refusal instead of the happy path).
func wcsLocalHolder(pid int) string {
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	return host + ":" + strconv.Itoa(pid)
}

// wcsStampHolder claims the row for `holder` so the selector's ledger chain has
// a claim stamp to read. The live case uses THIS process's pid, so the pid is
// genuinely alive without spawning anything.
func wcsStampHolder(t *testing.T, id, holder string, pid int) {
	t.Helper()
	if pid == 0 {
		// The unheld case: a row that owns the branch and carries NO claim
		// stamp at all — nobody has claimed it, so there is no process to
		// address.
		return
	}
	if _, err := tools.TaskUpdate(id, tools.TaskStatusInProgress); err != nil {
		t.Fatalf("move row %s to in_progress: %v", id, err)
	}
	// The PRODUCTION writer (Rule 14): the stamp the selector reads is written
	// by the same function a real claim uses, so a fixture cannot pass by
	// writing a shape production never produces.
	if err := tools.TaskStampHolder(id, holder, pid); err != nil {
		t.Fatalf("stamp holder on row %s: %v", id, err)
	}
}

// TestWorkerControlSelectorBranchResolvesToTheHolderPid is the GREEN half: a
// branch whose owner row carries a live local holder resolves to that pid, and
// the report names the whole chain (branch → row → status → pid) so the
// operator reads WHERE the number came from rather than trusting it.
func TestWorkerControlSelectorBranchResolvesToTheHolderPid(t *testing.T) {
	wcsOwnLedger(t)
	const branch = "feat/selector-resolves"
	id := wcsSeedOwnedRow(t, branch)
	wcsPlantWorkerProc(t, osGetpid())
	wcsStampHolder(t, id, wcsLocalHolder(osGetpid()), osGetpid())

	sel := workerControlSelection{branch: branch, applied: true, raw: workerControlBranchFlag + " " + branch}
	targets, err := workerControlResolveSelection(sel, t.TempDir())
	if err != nil {
		t.Fatalf("`%s %s %s` must resolve: %v", workerControlStop, workerControlBranchFlag, branch, err)
	}
	if len(targets) != 1 || targets[0].pid != osGetpid() {
		t.Fatalf("resolved %+v, want exactly the holder pid %d", targets, osGetpid())
	}
	for _, want := range []string{branch, id, strconv.Itoa(osGetpid())} {
		if !strings.Contains(targets[0].report, want) {
			t.Fatalf("the resolution report must name %q (the chain, not just the pid):\n%s", want, targets[0].report)
		}
	}
}

// TestWorkerControlSelectorRefusesWhatItCannotResolve is the DEFECT's own
// shape, pinned from four sides: every unresolvable selector must produce an
// ERROR that names its reason. A nil error here would mean the command fell
// through to the enumeration fallback and acted on workers nobody named.
func TestWorkerControlSelectorRefusesWhatItCannotResolve(t *testing.T) {
	wcsOwnLedger(t)
	const missingBranch = "feat/no-such-row-anywhere"
	sel := func(branch string) workerControlSelection {
		return workerControlSelection{branch: branch, applied: true, raw: workerControlBranchFlag + " " + branch}
	}

	// (a) no row names the branch at all.
	_, err := workerControlResolveSelection(sel(missingBranch), t.TempDir())
	if err == nil {
		t.Fatal("a branch with no owner row must be REFUSED, never answered by the enumeration fallback")
	}
	for _, want := range []string{missingBranch, "no ledger row names branch", "no worker for this branch HERE"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the no-owner refusal must carry %q:\n%v", want, err)
		}
	}

	// (b) the owner row is held on ANOTHER host.
	const foreignBranch = "feat/selector-foreign"
	foreignID := wcsSeedOwnedRow(t, foreignBranch)
	wcsStampHolder(t, foreignID, "some-other-host:"+strconv.Itoa(osGetpid()), osGetpid())
	_, err = workerControlResolveSelection(sel(foreignBranch), t.TempDir())
	if err == nil {
		t.Fatal("a FOREIGN holder must be REFUSED: that pid names a different process here")
	}
	for _, want := range []string{foreignID, "some-other-host", "means nothing on this host"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the foreign-holder refusal must carry %q:\n%v", want, err)
		}
	}

	// (c) the owner row records no holder at all.
	const unheldBranch = "feat/selector-unheld"
	unheldID := wcsSeedOwnedRow(t, unheldBranch)
	wcsStampHolder(t, unheldID, "", 0)
	_, err = workerControlResolveSelection(sel(unheldBranch), t.TempDir())
	if err == nil {
		t.Fatal("a row with NO claim stamp must be REFUSED: there is no process to address")
	}
	for _, want := range []string{unheldID, "records NO claim stamp"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the unheld refusal must carry %q:\n%v", want, err)
		}
	}

	// (d) the `--pid` spelling, on a pid that is not running. It must say the
	// pid is GONE — not act on the enumeration.
	_, err = workerControlResolveSelection(workerControlSelection{pid: wcsDeadPID, applied: true,
		raw: workerControlPIDFlag + " " + strconv.Itoa(wcsDeadPID)}, t.TempDir())
	if err == nil {
		t.Fatal("a dead --pid must be REFUSED")
	}
	if !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("the dead-pid refusal must say the pid is not running:\n%v", err)
	}
}

// wcsDeadPID is a pid this test asserts is NOT running: it is far above the
// kernel's pid_max on every host this suite runs on, and the assertion below
// re-measures that rather than trusting the number.
const wcsDeadPID = 1 << 22

// TestWorkerControlSelectorDeadPIDFixtureIsHonest guards the fixture itself: a
// `wcsDeadPID` that happened to be alive would turn the assertion above green
// for the wrong reason (Rule 31 — the test's own premise is measured too).
func TestWorkerControlSelectorDeadPIDFixtureIsHonest(t *testing.T) {
	if tools.PidAlive(wcsDeadPID) {
		t.Fatalf("the fixture pid %d is ALIVE on this host — pick another; the dead-pid case above is meaningless with it", wcsDeadPID)
	}
}

// TestWorkerControlSelectorPIDAcceptedRefusedByShape pins the `--pid` face's two
// outcomes on a process this test controls: its own pid is alive but is NOT a
// `gopi worker`, so the selector refuses it by name (a recycled pid aimed at a
// signal is exactly what this refusal exists to prevent).
func TestWorkerControlSelectorPIDShapeRefusesANonWorker(t *testing.T) {
	sel := workerControlSelection{pid: osGetpid(), applied: true, raw: workerControlPIDFlag + " " + strconv.Itoa(osGetpid())}
	_, err := workerControlResolveSelection(sel, t.TempDir())
	// No fixture proc root here: this process really is not a `gopi worker`,
	// so the refusal is measured against the REAL /proc.
	if os.Getenv(procname.EnvProcRoot) != "" {
		t.Fatal("this case must read the real /proc — a planted root would make its premise false")
	}
	if err == nil {
		t.Fatal("the test binary is alive but is not a `gopi worker` — the selector must refuse it, not signal it")
	}
	if !strings.Contains(err.Error(), "is NOT a gopi worker process") {
		t.Fatalf("the shape refusal must name what the pid IS:\n%v", err)
	}
}

// --- (4) the red proof ------------------------------------------------------

// TestWorkerControlSelectorRedProof is the RED HALF, and it is STRUCTURAL: it
// reconstructs the predicate the OLD control face answered with — a selection
// vocabulary that did not exist, i.e. "whatever argv arrives, there is nothing
// selected" — and shows what the rest of the machinery then does with it. That
// pair is the bug, stated as a fact about code rather than as a story:
//
//	no selection  →  resolveSelection returns (nil, nil)  →  the stop arm's
//	                 enumeration routes run  →  EVERY foreground worker of the
//	                 profile is addressed, and nothing in the output says the
//	                 operator's word was dropped.
//
// It is red against a tree where workerControlSelectionFrom does not exist
// (the file would not compile), and green here — which is the honest form of
// "the fix is what makes these observations possible at all". It asserts on the
// OLD shape (an empty selection), never on a copy of the new code, so it cannot
// drift into a tautology.
func TestWorkerControlSelectorRedProof(t *testing.T) {
	argv := []string{workerControlStop, workerControlBranchFlag, "feat/x"}

	// THE OLD FACE, reconstructed: it parsed actions, --force and --status and
	// had no notion of a target, so every argv produced the zero selection.
	oldSelection := func([]string) workerControlSelection { return workerControlSelection{} }

	// ① the word is invisible to the old face…
	old := oldSelection(argv)
	if old.applied {
		t.Fatal("the red proof's premise is wrong: the old face must see NO selection, or this is not the shape the epic describes")
	}
	// ② …and an unselected resolution is not an error — it is the FALLBACK,
	// which is exactly why the defect was silent: nil targets, nil error, and
	// the caller then enumerates everything.
	targets, err := workerControlResolveSelection(old, t.TempDir())
	if err != nil || targets != nil {
		t.Fatalf("the old shape must resolve to the enumeration fallback (nil, nil), got targets=%v err=%v", targets, err)
	}

	// ③ THE FIX, observed on the same argv: the word is now SEEN, so the
	// invocation is selected and can never reach the fallback silently.
	sel, perr := workerControlSelectionFrom(argv)
	if perr != nil {
		t.Fatalf("`%s %s feat/x` must parse as a selection: %v", workerControlStop, workerControlBranchFlag, perr)
	}
	if !sel.applied || sel.branch != "feat/x" {
		t.Fatalf("the selector must be seen and carried: %+v", sel)
	}
}

// --- helpers ----------------------------------------------------------------

// wcsPlantWorkerProc makes `pid` read as a foreground `gopi worker` to the
// shape test, by planting the fixture /proc entry the launcher/status tests use
// (procFixture) under the env knob the production reader honours
// (procname.EnvProcRoot). The PID stays REAL: its liveness is the kernel's
// answer, and only its command line is the fixture — which is the honest half to
// fake, because "this process is a gopi worker" is a fact about a process this
// test did not spawn and cannot make true.
func wcsPlantWorkerProc(t *testing.T, pid int) {
	t.Helper()
	root := t.TempDir()
	// Session 1, not this pid: a process leading its OWN session reads as a
	// detached daemon, and the case under test is the FOREGROUND shape.
	procFixtureExe(t, root, pid, binaryNameGopi, []string{"/" + binaryNameGopi, procname.VerbWorker}, 1,
		"/"+binaryNameGopi)
	t.Setenv(procname.EnvProcRoot, root)
}

// osGetpid is the pid the fixtures stamp as the holder: THIS process, which is
// alive for the whole test by construction, so no child has to be spawned to
// make the "live holder" case real.
func osGetpid() int { return os.Getpid() }
