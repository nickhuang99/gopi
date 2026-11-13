package main

// task_briefing_delivery_e2e_test.go — IS THE BRIEFING DELIVERED WHOLE? (rule
// "a clause appended is not a clause delivered"; epic 01a143b1, 2026-10-16).
//
// WHY THIS EXISTS. The dispatch seam appends a freshness clause to the child's
// briefing at SIX places (ground announcement, premise, base, gate assertion,
// figure, pinned path set, cut point, master reading). Every one of them is
// written to the END of the briefing, and a clause that does not reach the child
// is INVISIBLE: the seam prints its "…NAMED at dispatch" record line, the row
// looks healthy, and the run simply never learns the thing the clause said.
//
// THE MEASURED DEFECT THIS WALK PINS (task 01a14242, re-measured here on
// 2026-10-16 at tip 1921b4a): the child's FIRST model request carried
// bodyLen=52830 with the cut-point and master-reading clauses ABSENT, while the
// pre-change tree carried bodyLen=55587 with them PRESENT at idx 8178/9433 —
// i.e. the wired body was 2757 bytes SHORTER while appending MORE text. The
// hypothesis recorded at the time was a byte BOUND truncating the prompt tail.
// The cause is NOT a bound (measured: no byte cap exists on this path; the
// over-limit arm below pushes the briefing far past every size threshold on the
// seam and the last clause still arrives). It was a Go SHADOWING defect in the
// seam: the premise clause was appended with `briefing, premiseAppended := …`,
// which declared a scope-local `briefing` inside the ground-resolution block, so
// every later `briefing, x = …` assignment — the base, gate-assertion, figure,
// pinned-path-set, cut-point and master-reading clauses — wrote into a variable
// that died with the block. The announced text reached the CHILD (it is printed
// outside that block) while the appended clauses never did. THE OBVIOUS SUSPECT
// WAS THE LAST THING IT WAS.
//
// WHAT THIS WALK ASSERTS, AND WHY EACH HALF IS NEEDED:
//
//  ① THE LAST CLAUSE THE SEAM APPENDED REACHES THE CHILD'S FIRST REQUEST. The
//     master-reading clause is the final append site on this seam, so it is the
//     one that disappears FIRST if a tail is dropped, and it is asserted on the
//     request the mock actually served — not on a seam log line. A run whose
//     clause was appended but not delivered fails here, which is the whole
//     point: the seam's own record line CANNOT distinguish the two (measured —
//     it printed "briefing MASTER READING NAMED at dispatch" on the very tree
//     whose child never received it).
//
//  ② THE FIRST CLAUSE TOO, so a seam that starts truncating the HEAD is caught
//     by the same walk (the two clauses ride different halves of the string).
//
//  ③ A BRIEFING FAR PAST EVERY SIZE THRESHOLD STILL DELIVERS. The over-long arm
//     submits a payload larger than the seam's own inline threshold (the spill
//     point, internal/tools/task_payload_spill.go) so the briefing reaches the
//     child through the SPILL path — the path a byte bound would corrupt first
//     if one existed. This arm is the falsifier for the byte-bound hypothesis:
//     if any cap bounded the tail, this is the input that would show it.
//
// WHAT THIS WALK DOES NOT CLAIM. It does not re-derive the clause text (the
// labels come from internal/tools, Rule 14) and it does not measure the child's
// BEHAVIOUR — only that the bytes arrived, which is the precondition every
// clause on this seam silently assumed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreeroots"
)

// briefingDeliveryRows are the arms' row ids — ONE entry per arm in
// briefingDeliveryArms. They are FULL uuids because the ground's path/branch
// stem is derived from the row id (internal/tools/task_run_ground.go) — a slug
// row has no derivable ground.
//
// THE TABLE MUST COVER EVERY ARM, and that is not a stylistic preference: the
// lookup below is a plain map index, so an arm with no entry silently resolves
// to the empty string. The walk then submits a row whose id is "", spawns
// `gopi worker ""`, and polls for a child carrying "GOPI_TASK_WRAPPER_ID=" —
// which matched 43,825 live processes on this host (a substring match, not an
// identity) before the ownership test rejected each one. The arm failed after
// the FULL 60-second probe budget with a message that named no cause
// ("no executor child of  carrying GOPI_TASK_WRAPPER_ID"), which is how the
// "gate-assertion" arm sat red on master unnoticed: the failure looked like a
// timing flake, not like a missing table entry.
//
// Two guards now make that shape impossible to reintroduce silently:
// TestBriefingDeliveryArmsAllHaveRowIDs (pure, no binary — red the moment an
// arm has no id) and the empty-id assertion at the top of the walk (Rule 15:
// refuse where the cause is still visible instead of timing out 60s later).
var briefingDeliveryRows = map[string]string{
	"near-limit":     "9f6b1c2d-3e4a-4b5c-8d7e-1f2a3b4c5d6e",
	"over-limit":     "7a5e9d31-2c48-4f6b-9e07-5d3a1b8c6f42",
	"gate-assertion": "b3d8f4a7-6e21-4c95-8a0b-2f7d1c4e9a53",
}

// briefingDeliveryArm is one payload size this walk submits. The sizes are
// chosen against the seam's OWN thresholds rather than invented: inlineMax is
// the spill point (task_payload_inline_verify below), and over-limit is several
// times it so that no plausible cap — spill threshold, tool-array assembly
// order, provider body limit scaled to the fixture — can pass unnoticed.
type briefingDeliveryArm struct {
	name      string
	payload   int
	rationale string
	// claim puts a GATE ASSERTION in the payload — the claim shape the seam's
	// gate-assertion clause exists to re-anchor (a surface + a verdict word
	// about the integration branch). Only this arm carries one, so the other
	// arms stay a control: they must NOT grow that clause.
	claim bool
	// want, when non-empty, are the extra strings the child's first request
	// must carry for this arm beyond the three universal clauses.
	want []string
}

// briefingDeliveryInlineMaxBytes mirrors the seam's inline payload threshold
// from the DOCUMENTED value (internal/tools/task_payload_spill.go). It is
// duplicated here as a TEST INPUT, not as a policy: the arm's job is to be
// above it, and a test that read the policy through a helper would fail to
// notice the policy moving. The companion assertion below checks that the
// spilled form is what actually arrives, so this constant cannot silently stop
// describing the tree.
const briefingDeliveryInlineMaxBytes = 16 << 10

var briefingDeliveryArms = []briefingDeliveryArm{
	{
		name:      "near-limit",
		payload:   5 << 10,
		rationale: "the size the sibling run measured (bodyLen≈55K) — the clause must arrive on an ordinary briefing",
	},
	{
		name:      "over-limit",
		payload:   64 << 10,
		rationale: "4× the seam's own inline spill threshold: an input far past every size decision on this path, so a hidden byte cap on the tail cannot pass (this is the arm that falsifies a bound)",
	},
	{
		name:      "gate-assertion",
		payload:   3 << 10,
		rationale: "a briefing carrying a GATE ASSERTION: the seam's SECOND clause must reach the child too, which is what the restored wiring (01a143b1) is for",
		claim:     true,
		want:      []string{tools.TaskBriefingGateAssertLabel},
	},
}

// TestBriefingLastClauseReachesTheChildsFirstRequest is the delivery walk.
func TestBriefingLastClauseReachesTheChildsFirstRequest(t *testing.T) {
	for _, arm := range briefingDeliveryArms {
		arm := arm
		t.Run(arm.name, func(t *testing.T) {
			// NOT t.Parallel: each arm pins GOPI_HOME-relative worktree roots
			// through t.Setenv (Rule 17), and Go refuses Setenv in a parallel
			// test — the arms are heavy (a real worker each), so they run in
			// sequence by design.
			briefingDeliveryWalk(t, arm)
		})
	}
}

// TestBriefingDeliveryArmsAllHaveRowIDs is the PURE gate over the arm ↔ row-id
// table — no binary, no temp repo, no 60-second probe, so it fails in
// milliseconds on exactly the defect that once cost a full probe budget per
// run.
//
// WHAT IT PINS, all three from one table:
//
//  1. EVERY ARM HAS A ROW ID. A missing key is the measured defect: the walk's
//     plain map index returned "", the submission created a row with no id, and
//     `gopi worker ""` was polled for 60s against an env value that 43,825 live
//     processes matched by substring.
//  2. NO TWO ARMS SHARE ONE. Two arms on one row id would claim the same ground
//     and the second would find the first's worktree, so its measurements would
//     describe the other arm.
//  3. EVERY ROW ID IS A FULL UUID. The ground path and branch stem are derived
//     from the row id, so a slug id has no derivable ground at all.
//
// RED PROOF: delete any line from briefingDeliveryRows and this test fails
// naming the arm and printing the keys it did see.
func TestBriefingDeliveryArmsAllHaveRowIDs(t *testing.T) {
	seen := make(map[string]string, len(briefingDeliveryRows))
	for _, arm := range briefingDeliveryArms {
		row, ok := briefingDeliveryRows[arm.name]
		if !ok {
			t.Errorf("arm %q has no entry in briefingDeliveryRows — the walk would submit row id \"\" and then poll 60s for a child carrying %s= (the measured defect; keys present: %v)",
				arm.name, tools.TaskWrapperEnv, briefingDeliveryRowNames())
			continue
		}
		if strings.TrimSpace(row) == "" {
			t.Errorf("arm %q has an EMPTY row id in briefingDeliveryRows — same defect as a missing key: the submission writes a row with no id", arm.name)
			continue
		}
		if !briefingDeliveryRowIDShape.MatchString(row) {
			t.Errorf("arm %q has row id %q, which is not a full lowercase uuid — the ground's path/branch stem is derived from the row id, so a slug row has no derivable ground", arm.name, row)
		}
		if other, dup := seen[row]; dup {
			t.Errorf("arms %q and %q share row id %s — they would claim the same ground, so the second arm would measure the first arm's worktree", other, arm.name, row)
		}
		seen[row] = arm.name
	}
	for name := range briefingDeliveryRows {
		found := false
		for _, arm := range briefingDeliveryArms {
			if arm.name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("briefingDeliveryRows carries %q but no arm has that name — a dead entry (the table and briefingDeliveryArms must describe the same set)", name)
		}
	}
}

// briefingDeliveryRowIDShape is the full-uuid spelling every arm's row id must
// carry (task_run_ground.go derives the ground stem from it). Named rather than
// compiled inline at the call site (Rule 1).
var briefingDeliveryRowIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// briefingDeliveryRowNames returns the table's keys in arm order, for the
// fail-fast message: a refusal that cannot say what it DID find sends the
// reader back to the source it was just reading.
func briefingDeliveryRowNames() []string {
	names := make([]string, 0, len(briefingDeliveryRows))
	for _, arm := range briefingDeliveryArms {
		if row, ok := briefingDeliveryRows[arm.name]; ok {
			names = append(names, arm.name+"="+row)
			continue
		}
		names = append(names, arm.name+"=<MISSING>")
	}
	return names
}

// briefingDeliveryWalk runs ONE arm: submit a row whose payload is sized by the
// arm, spawn a real `gopi worker` on it, and read the child's FIRST model
// request off the mock.
func briefingDeliveryWalk(t *testing.T, arm briefingDeliveryArm) {
	t.Helper()
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	row := briefingDeliveryRows[arm.name]
	// FAIL FAST ON A MISSING TABLE ENTRY (Rule 15). Without this the map index
	// above returns "" and the walk goes on to submit an id-less row and poll
	// for an env value every process matches — 60 seconds later it reports "no
	// executor child of  appeared", which names no cause. The cause is knowable
	// HERE, at zero cost, so it is refused here.
	if strings.TrimSpace(row) == "" {
		t.Fatalf("arm %q has no row id in briefingDeliveryRows (keys: %v) — the walk cannot submit a row without an id; fix the TABLE, not this walk",
			arm.name, briefingDeliveryRowNames())
	}
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := executorCwdProbeRepo(t, "briefing-delivery-"+arm.name)
	// A CLAIM ARM'S FIXTURE MUST MAKE THE CLAUSE FIRE, or the arm measures
	// nothing. The gate-assertion clause is stamped only when the row's
	// AUTHORING moment resolves to a revision the integration branch has MOVED
	// PAST (tools.TaskBriefingGateAssertRefresh; distance 0 is the positive
	// control and stamps nothing). A row submitted a moment ago against a repo
	// whose tip is already in the past is therefore ALWAYS distance 0 — so the
	// fixture gives the branch one commit dated AFTER the row: the row's
	// created_at then resolves to the previous revision and the distance is the
	// one commit this helper adds. Without this, ④ below would fail on a
	// correctly working seam.
	if arm.claim {
		briefingDeliveryMoveBranchPastTheRow(t, repo)
	}

	mock := testutil.NewMockLLM(t,
		testutil.Resp{Hold: true},
		testutil.Resp{Text: "briefing delivery done"},
	)
	t.Cleanup(mock.Close)

	// The payload is shipped as a FILE (@path), not as argv: a 64 KiB argument
	// would test the shell's limit rather than the seam's delivery.
	payloadPath := filepath.Join(t.TempDir(), "payload.txt")
	body := briefingDeliveryPayload(arm)
	if err := os.WriteFile(payloadPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write the arm's payload: %v", err)
	}

	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())
	env = append(env, worktreeroots.WorktreeRootEnvName+"="+root)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"briefing delivery " + arm.name, "--payload", "@" + payloadPath,
		"--task-id", row, "--repo", repo}, "", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	wrapper := exec.Command(bin, "worker", row)
	wrapper.Dir = repo
	wrapper.Env = env
	var wout, werr strings.Builder
	wrapper.Stdout = &wout
	wrapper.Stderr = &werr
	if err := wrapper.Start(); err != nil {
		t.Fatalf("start `gopi worker %s`: %v", row, err)
	}
	wrapperPid := wrapper.Process.Pid
	childPid := 0
	t.Cleanup(func() {
		if wrapper.ProcessState == nil {
			_ = wrapper.Process.Kill()    //stylegate:ignore kills the fixture process the test spawned
			_, _ = wrapper.Process.Wait() //stylegate:ignore reaps the fixture process the test spawned
		}
		groundKill(t, childPid)
	})

	childPid, _ = groundWaitForChildCwd(t, row, wrapperPid)

	groundWaitForFirstModelRequest(t, mock)
	body1 := mock.RequestBody(1)
	if strings.TrimSpace(body1) == "" {
		t.Fatalf("the mock served no request body — the child never reached its first model round, so delivery cannot be measured")
	}

	// ① THE LAST CLAUSE APPENDED ON THIS SEAM. Asserted on the request the
	// mock served, never on the seam's own record line: that line is printed
	// from the pre-fix tree too, which is exactly how this defect hid.
	//
	// The master-reading clause is the seam's FINAL append site (cmd/agent/
	// cli_tasks.go, after the cut-point clause), so a dropped tail takes it
	// first. Its LABEL is asserted as well as its refs: the refs alone would
	// pass on the briefing's own prose about master.
	last := []string{tools.MasterReadingLabel, tools.MasterReadingLocalRef(), tools.MasterReadingTipRef()}
	for _, want := range last {
		if !strings.Contains(body1, want) {
			t.Errorf("the LAST clause the seam appended must reach the child's FIRST model request: %q is absent (arm %s, %d-byte payload).\nThe seam logs a '…NAMED at dispatch' line whether or not the bytes arrived, so that line is NOT delivery evidence:\n%s",
				want, arm.name, arm.payload, briefingDeliveryExcerpt(body1))
		}
	}

	// ② THE FIRST CLAUSE — the other half of the string, so a head-truncation
	// regression is caught by the same walk.
	if !strings.Contains(body1, tools.TaskGroundAnnouncementLabel) {
		t.Errorf("the FIRST clause the seam appended must also reach the child's first request: %q is absent:\n%s",
			tools.TaskGroundAnnouncementLabel, briefingDeliveryExcerpt(body1))
	}

	// ③ THE PREVIOUSLY-LOST SIBLING, asserted by name: the cut-point clause is
	// the one the sibling run measured ABSENT at idx=-1 while base carried it
	// at idx=8178. A regression that re-loses it must fail here.
	if !strings.Contains(body1, tools.GroundCutPointLabel) {
		t.Errorf("the cut-point clause must reach the child's first request (it was the clause the sibling run measured absent at idx=-1 while base carried it at 8178):\n%s",
			briefingDeliveryExcerpt(body1))
	}

	// ④ AN ARM'S OWN EXTRA CLAUSES. arm.want existed as a declared field but was
	// READ NOWHERE before this fix: the gate-assertion arm declared
	// TaskBriefingGateAssertLabel and then never checked it, so the one arm whose
	// whole purpose is "the seam's SECOND clause reaches the child" asserted
	// nothing about that clause. A declared-but-unread expectation is worse than
	// no expectation: the arm reads as coverage in every report that counts arms.
	for _, want := range arm.want {
		if !strings.Contains(body1, want) {
			t.Errorf("arm %s declares it must deliver %q and it is ABSENT from the child's first request (arm.want was read by nothing before this fix, so this arm never checked it):\n%s",
				arm.name, want, briefingDeliveryExcerpt(body1))
		}
	}

	// ⑤ THE OVER-LONG ARM'S OWN CONTROL: for a payload past the seam's inline	// threshold the briefing arrives through the SPILL form, which is the path a
	// byte bound would corrupt first. Asserting it here keeps this arm from
	// silently testing the ordinary inline path under an over-long name.
	if arm.payload > briefingDeliveryInlineMaxBytes {
		if strings.Contains(body1, body) {
			t.Errorf("arm %s submits %d bytes (> the seam's %d-byte inline threshold) so the briefing must arrive through the spill path; the full payload was inlined instead, which means this arm is not exercising what its name claims",
				arm.name, arm.payload, briefingDeliveryInlineMaxBytes)
		}
	}

	mock.ReleaseHold(1)
	// Teardown is left to the t.Cleanup registered above, exactly as the
	// ground-announcement and path-set walks do: it runs at the END of the subtest
	// (after every arm's readings are taken), so the wrapper's own exit — which is
	// not an assertion here — costs no arm any wall time. An explicit
	// groundStopRun HERE would double-wait on the same *exec.Cmd.
}

// briefingDeliveryClaimLeadSeconds is how far AHEAD of the row's authoring
// moment the fixture's integration branch is moved for a CLAIM arm — named
// rather than typed (Rule 1), and large enough that the submit itself (which
// stamps created_at) cannot overtake it on a slow machine.
const briefingDeliveryClaimLeadSeconds = 120

// briefingDeliveryMoveBranchPastTheRow dates one commit on the fixture's
// integration branch AFTER the moment the row is about to be authored at.
//
// WHY THE FIXTURE HAS TO DO THIS (measured while fixing this walk, 2026-10-29):
// tools.TaskBriefingGateAssertRefresh stamps its clause only when the row's
// AUTHORING moment resolves to a revision the integration branch has MOVED PAST
// — `git rev-list --first-parent -1 --before=<created_at> <ref>` compared with
// the ref's tip, distance 0 being the POSITIVE CONTROL that stamps nothing (a
// fresh row asserting today's state is honest). A fixture that submits the row
// and never moves the branch is therefore ALWAYS distance 0, and the clause can
// never be appended: the gate-assertion arm would fail on a PERFECTLY WORKING
// seam, and a reader would "fix" the seam to satisfy the test.
//
// So the branch gets one commit whose committer date is later than the row's own
// created_at. The row then resolves to the previous revision, the distance is
// exactly that one commit, and the clause is stamped — deterministically, with
// no sleep and no wall-clock race.
func briefingDeliveryMoveBranchPastTheRow(t *testing.T, repo string) {
	t.Helper()
	const stampLayout = "2006-01-02T15:04:05 +0000"
	at := time.Now().UTC().Add(briefingDeliveryClaimLeadSeconds * time.Second)
	stamp := at.Format(stampLayout)
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-q", "-m",
		"dated past the row's authoring moment so the gate-assertion clause fires")
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("date one commit past the row on %s: %v\n%s", repo, err, out)
	}
}

// briefingDeliveryPayload builds the payload for one arm: prose that looks like a
// briefing (so the seam's scanners have something ordinary to read) — carrying a
// GATE ASSERTION when the arm asks for one — plus a padding run, closed with a
// marker so a truncated tail is visible by eye in an excerpt. The padding lines
// are deliberately NOT clause labels and never name a test surface, so the arms
// WITHOUT arm.claim cannot grow a gate-assertion clause by accident: their
// control property (that clause absent) is what makes the claim arm's assertion
// mean something.
func briefingDeliveryPayload(arm briefingDeliveryArm) string {
	const head = "[Briefing payload]\nExercise filler for the delivery walk.\n"
	sb := strings.Builder{}
	sb.WriteString(head)
	if arm.claim {
		// THE CLAIM MUST STATE A **RED** VERDICT, and that is the scanner's
		// rule, not a preference: taskreport.GateAssertionsIn fires on a
		// standalone verdict word (red / fails / failing / broken) bound to the
		// integration branch, and a GREEN sentence asserts nothing at all — no
		// clause is appended, so there is nothing for this arm to observe. The
		// pre-fix payload said "is GREEN on master", which means the arm whose
		// whole purpose is "the seam's SECOND clause reaches the child" was
		// structurally incapable of seeing that clause even if it had been
		// delivered.
		//
		// This is measuring DELIVERY, not the verdict's truth: the clause
		// re-anchors an assertion to the row's authoring moment whether or not
		// the surface is still red, and re-running the named gate is the child's
		// job, not this walk's.
		sb.WriteString("`go test -run TestBriefingLastClauseReachesTheChildsFirstRequest ./cmd/agent/` is RED on master.\n")
	}
	line := "padding line for the delivery walk — the seam must deliver every byte it appended, not merely log that it did.\n"
	for sb.Len()+len(line) < arm.payload {
		sb.WriteString(line)
	}
	sb.WriteString("[end of payload]")
	return sb.String()
}

// briefingDeliveryExcerpt bounds a request body for an error message (a real
// body is tens of kilobytes; an assertion message must stay readable). It shows
// the HEAD, where the briefing starts, because the failure being reported is
// "the briefing's tail is missing" and the head is what proves the briefing
// arrived at all.
func briefingDeliveryExcerpt(body string) string {
	const limit = 1200
	if len(body) <= limit {
		return body
	}
	return body[:limit] + "… (truncated)"
}
