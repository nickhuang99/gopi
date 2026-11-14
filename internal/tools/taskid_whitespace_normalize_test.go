package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// ---------------------------------------------------------------------------
// Ledger accessor task_id normalization contract (one seam, every accessor).
//
// The bug this file pins: the task_id handling was SPLIT among the accessors
// — TaskTags / TaskSessionID / TaskTagResumed trimmed the argument, while
// TaskBriefing / TaskResumeCount did not. One operator-supplied padded id
// (copy-paste space, trailing newline from a shell substitution) therefore
// resolved through one accessor and read as "no task with task_id" / resume
// count 0 through its sibling. Resolution option (a): trim in ALL accessors,
// through the ONE seam taskIDNormalize (tasks.go).
//
// A padded id is the MINIMAL sample (text2img habit 4): the fixture row is
// seeded EXACT-id only, so any resolution of the padded form must come from
// the normalization — never from a munged row.
//
// THE SESSION OPERAND (added when this file went master-red). One accessor in
// the matrix, TaskTagResumed, also TRIMS its session operand — and it now
// validates that operand against the session artifacts this profile wrote on
// disk (internal/tools/task_resumed_session.go, F16.1). The fixture therefore
// lays down a REAL artifact, exactly as the one in-product caller does
// (cmd/agent/cli_resume.go writes the session JSONL first and stamps the id
// second, so the artifact is on disk before the check runs). This is the whole
// point of the session leg: the operand is padded, so the trim is what makes
// the probe FIND the artifact — the check is exercised, never bypassed, and an
// artifact-less operand is still refused (TestTaskIDWhitespaceNormalized
// RefusesAnUnknownSession below).
// ---------------------------------------------------------------------------

// taskIDWSPad is the padded form of the seeded id: leading + trailing space
// (the copy-paste / shell-substitution shape) AND an embedded newline, so
// the test proves TrimSpace's full cutset is applied, not just a trailing " ".
const (
	taskIDWSSeed = "t-ws-normalize-1"
	taskIDWSPad  = "  t-ws-normalize-1 \n "
	taskIDWSGone = "  t-ws-normalize-absent  "
)

// taskIDWSSessionPrefix masks the ROOT of a `GOPI_HOME` that would otherwise be
// read as a sandbox PROFILE (config.LookupSandboxProfileMisuse refuses such a
// value at the ONE resolver, config.GopiDir — so a fixture must not name a
// directory called <leaf> or <x>/<leaf>/<leaf>). Uniqueness is what the session
// tree needs, not the name.
const taskIDWSSessionPrefix = "wsnormalize-"

// taskIDWSSandbox pins EVERY input this file's accessors read, and returns the
// sandbox root:
//
//   - the LEDGER, through the ONE test scaffold (testutil.NewTestDBPath, Rule
//     32) — a fresh per-test memory.db under the pinned scratch root, on the
//     device the suite was pinned to, with the legacy override masked;
//   - the GOPI PROFILE (GOPI_HOME + HOME), so the session-artifact probe walks
//     THIS test's tree and never the operator's live ~/.gopi/sessions (the
//     defect that made the file master-red: an unpinned HOME had the probe
//     reading a profile the fixture does not own);
//   - TMPDIR/GOTMPDIR, so a host redirect cannot move a temp path out from
//     under the test.
//
// The root is testutil.TestScratchDir (a per-test directory under the pinned
// scratch home, off the `/` partition — Rule 22), NOT a hand-rolled
// filepath.Join(t.TempDir()): the profile is a scratch tree like any other and
// takes its path from the same place.
func taskIDWSSandbox(t *testing.T) string {
	t.Helper()
	dir := testutil.TestScratchDir(t, taskIDWSSessionPrefix+"home")
	testutil.NewTestDBPath(t) // Rule 32: the ledger path comes from the scaffold
	t.Setenv("GOTMPDIR", dir)
	t.Setenv("TMPDIR", dir)
	t.Setenv("HOME", dir)
	t.Setenv(config.GopiDirEnv, dir)
	// Checked, not trusted (原则无效，检查才有效): the ONE resolver the production
	// probe goes through must read THIS sandbox. If the pins above ever stop
	// applying, this fails loudly here instead of the fixture quietly walking
	// the operator's live profile.
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolve the sandbox data root: %v", err)
	}
	if want := filepath.Join(dir, config.ProfileDirName); gd != want {
		t.Fatalf("GOPI_HOME pinning failed: the gopi data root is %q, want %q — the session probe "+
			"would read a profile this fixture does not own", gd, want)
	}
	return dir
}

// taskIDWSSessionArtifact lays down the session artifact a REAL session leaves
// for sessionID, in the bare-session layout (sessions/<session-id>/
// session.jsonl) whose first line is the session_start event carrying the id —
// the minimal sample the funnel's name probe looks for (Rule 10.4).
//
// The path is built from the ONE resolver the production check itself uses
// (config.GopiDir + config.SessionsDirName + sessionlayout.RecordName), so the
// fixture cannot drift from the check: a hand-spelled "sessions" would keep
// passing here while the product looked somewhere else.
//
// The caller passes the UNPADDED id: the artifact is what the real writer lays
// down, and the padded operand under test must reach it through the
// normalization — never through a filename the fixture padded to match.
func taskIDWSSessionArtifact(t *testing.T, sessionID string) {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolve the gopi data root: %v", err)
	}
	path := filepath.Join(gd, config.SessionsDirName, sessionID, sessionlayout.RecordName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := `{"type":"session_start","sessionId":"` + sessionID + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write session artifact %s: %v", path, err)
	}
}

// taskIDWSSeedRow seeds ONE row with the EXACT id and a resumed tag, then
// returns after re-reading it: the fixture is proven present (and the pad
// proven absent) before the accessor matrix runs.
func taskIDWSSeedRow(t *testing.T, taskID, payload, resumedSession string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "ws-normalize row " + taskID,
		TaskID:  taskID,
		Payload: payload,
		Tags:    taskTagResumedPrefix + resumedSession,
	}); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("seed verify: open ledger: %v", err)
	}
	defer db.Close()
	exact, err := taskByID(ctx, db, taskID)
	if err != nil || exact == nil {
		t.Fatalf("seed verify: exact id %q did not resolve: %v", taskID, err)
	}
	padded, err := taskByID(ctx, db, taskIDWSPad)
	if err != nil {
		t.Fatalf("seed verify: padded lookup errored: %v", err)
	}
	if padded != nil {
		t.Fatalf("seed verify: the padded id resolved to a row — the fixture is not minimal")
	}
}

// taskIDWSSessions are the two session artifacts the fixture lays down: the
// row's already-stamped lineage (sess-ws) and the one TaskTagResumed stamps
// through a PADDED operand (sess-ws-2). Both are seeded UNPADDED — the pad
// lives only in the operand, so the trim is what reaches them.
const (
	taskIDWSSessionSeeded = "sess-ws"
	taskIDWSSessionPadded = "sess-ws-2"
)

// TestTaskIDWhitespaceNormalizedAllAccessors pins the UNIFIED contract: every
// ledger accessor that takes a task_id resolves the SAME row for a padded id
// as for the exact id.
func TestTaskIDWhitespaceNormalizedAllAccessors(t *testing.T) {
	taskIDWSSandbox(t)
	const payload = "payload for the normalized accessor matrix"
	taskIDWSSeedRow(t, taskIDWSSeed, payload, taskIDWSSessionSeeded)
	taskIDWSSessionArtifact(t, taskIDWSSessionSeeded)
	taskIDWSSessionArtifact(t, taskIDWSSessionPadded)

	// The session leg is an EXACT/PADDED COMPARISON, so it pins the same
	// contract the task_id leg does: the artifact is seeded UNPADDED, so only
	// the normalization makes the padded operand find it — while the exact
	// operand proves the fixture is otherwise satisfiable (the check is
	// exercised, never bypassed, and the pad is the single variable).
	if out, err := TaskTagResumed(taskIDWSSeed, taskIDWSSessionSeeded); err != nil {
		t.Fatalf("TaskTagResumed(exact session): %v", err)
	} else if !strings.Contains(out, taskIDWSSessionSeeded) {
		t.Errorf("TaskTagResumed output = %q, want the seeded session", out)
	}

	// --- the trimmed family (behavior preserved from before the fix) ---
	gotTags, err := TaskTags(taskIDWSPad)
	if err != nil {
		t.Fatalf("TaskTags(padded): %v", err)
	}
	if gotTags != taskTagResumedPrefix+taskIDWSSessionSeeded {
		t.Errorf("TaskTags(padded) = %q, want the seeded tags", gotTags)
	}
	if sid, err := TaskSessionID(taskIDWSPad); err != nil || sid != "" {
		t.Errorf("TaskSessionID(padded) = (%q, %v), want (\"\", nil) — the seed row has no session_id", sid, err)
	}
	// The sess-ws-2 artifact is on disk and the operand is PADDED on both
	// sides: the session operand is trimmed by the same accessor, and the
	// funnel's artifact probe still finds the unpadded artifact (an artifact-
	// less operand is refused — TestTaskIDWhitespaceNormalizedRefusesAnUnknown
	// Session).
	if out, err := TaskTagResumed(taskIDWSSeed, " "+taskIDWSSessionPadded+" "); err != nil {
		t.Fatalf("TaskTagResumed: %v", err)
	} else if !strings.Contains(out, taskIDWSSeed) {
		t.Errorf("TaskTagResumed output = %q, want the seeded id", out)
	}
	// The write above proves the tag landed on the EXACT row (not a munged one)
	// AND that it carries the TRIMMED session id.
	gotTags, err = TaskTags(taskIDWSSeed)
	if err != nil {
		t.Fatalf("TaskTags(exact) after TagResumed: %v", err)
	}
	if !strings.Contains(gotTags, taskTagResumedPrefix+taskIDWSSessionPadded) {
		t.Fatalf("tags after TagResumed(exact) = %q, want the second resumed tag on the seeded row", gotTags)
	}

	// --- the fixed family: the same padded id must now agree ---
	summary, gotPayload, err := TaskBriefing(taskIDWSPad)
	if err != nil {
		t.Fatalf("TaskBriefing(padded) = (%q, %q, %v) — the accessors must agree on a padded id", summary, gotPayload, err)
	}
	if summary != "ws-normalize row "+taskIDWSSeed || gotPayload != payload {
		t.Errorf("TaskBriefing(padded) = (%q, %q), want the seeded pair", summary, gotPayload)
	}
	if n := TaskResumeCount(taskIDWSPad); n != 2 {
		t.Errorf("TaskResumeCount(padded) = %d, want 2 (the two resumed tags the exact id also counts)", n)
	}
	if _, err := TaskArchive(taskIDWSPad); err != nil && !strings.Contains(err.Error(), "not terminal") {
		t.Errorf("TaskArchive(padded) = %v, want the not-terminal refusal (it FOUND the row)", err)
	}
	if out, err := TaskShow(taskIDWSPad); err != nil {
		t.Errorf("TaskShow(padded): %v", err)
	} else if !strings.Contains(out, taskIDWSSeed) {
		t.Errorf("TaskShow(padded) did not render the seeded row:\n%s", out)
	}
	// The seam itself: the shared normalization is the exported-for-test rule.
	if got := taskIDNormalize(taskIDWSPad); got != taskIDWSSeed {
		t.Errorf("taskIDNormalize(%q) = %q, want %q", taskIDWSPad, got, taskIDWSSeed)
	}

	// --- pad-equality: padded and exact must produce the SAME answers ---
	exactTags, err := TaskTags(taskIDWSSeed)
	if err != nil {
		t.Fatalf("TaskTags(exact): %v", err)
	}
	paddedTags, err := TaskTags(taskIDWSPad)
	if err != nil {
		t.Fatalf("TaskTags(padded): %v", err)
	}
	if exactTags != paddedTags {
		t.Errorf("padded/exact tags disagree: %q vs %q", paddedTags, exactTags)
	}
}

// TestTaskIDWhitespaceNormalizedUnknownIDStillFails pins the OTHER half of
// the contract: normalization must not turn a genuinely unknown id into a
// resolution. A padded UNKNOWN id errors / counts 0 exactly like the bare
// unknown id, and a blank id stays a refusal (never a lookup of "").
func TestTaskIDWhitespaceNormalizedUnknownIDStillFails(t *testing.T) {
	taskIDWSSandbox(t)
	taskIDWSSeedRow(t, taskIDWSSeed, "payload", taskIDWSSessionSeeded)

	for _, id := range []string{taskIDWSGone, "t-ws-normalize-absent"} {
		if _, err := TaskTags(id); err == nil {
			t.Errorf("TaskTags(%q) resolved an unknown id, want an error", id)
		}
		if _, err := TaskSessionID(id); err == nil {
			t.Errorf("TaskSessionID(%q) resolved an unknown id, want an error", id)
		}
		if _, _, err := TaskBriefing(id); err == nil {
			t.Errorf("TaskBriefing(%q) resolved an unknown id, want an error", id)
		}
		if n := TaskResumeCount(id); n != 0 {
			t.Errorf("TaskResumeCount(%q) = %d, want 0 for an unknown id", id, n)
		}
		if _, err := TaskShow(id); err == nil {
			t.Errorf("TaskShow(%q) resolved an unknown id, want an error", id)
		}
	}

	// A blank / whitespace-only id never addresses a row: the readers refuse
	// it, the count returns 0 (both are the empty-after-trim contract).
	for _, id := range []string{"", "   ", "\t\n"} {
		if _, err := TaskTags(id); err == nil {
			t.Errorf("TaskTags(%q) accepted a blank id, want a refusal", id)
		}
		if _, err := TaskSessionID(id); err == nil {
			t.Errorf("TaskSessionID(%q) accepted a blank id, want a refusal", id)
		}
		if _, _, err := TaskBriefing(id); err == nil {
			t.Errorf("TaskBriefing(%q) accepted a blank id, want a refusal", id)
		}
		if n := TaskResumeCount(id); n != 0 {
			t.Errorf("TaskResumeCount(%q) = %d, want 0 for a blank id", id, n)
		}
	}
}

// TestTaskIDWhitespaceNormalizedRefusesAnUnknownSession is the RED PROOF for the
// half of the matrix that has a WRITE behind it: trimming a session operand
// must not turn an id that names no artifact into a resolution. The artifact
// below belongs to a DIFFERENT session, so the refusal cannot be passing merely
// because the sandbox tree is empty, and the row's tags are asserted UNCHANGED
// (the audit record is not stamped — Rule 15: refused input is never silently
// accepted, and nothing is written).
//
// What it pins: delete the session-existence validation (the
// taskResumedSessionArtifact funnel in internal/tools/task_resumed_session.go)
// and this test fails — a nonexistent session would be tagged, and the tags
// assertion below would read the phantom tag. That is the red proof the
// preserved behaviour needs; the acceptance test above is the green half.
func TestTaskIDWhitespaceNormalizedRefusesAnUnknownSession(t *testing.T) {
	taskIDWSSandbox(t)
	taskIDWSSeedRow(t, taskIDWSSeed, "payload", taskIDWSSessionSeeded)
	// A real artifact exists — just not for the operand under test.
	taskIDWSSessionArtifact(t, taskIDWSSessionSeeded)
	before, err := TaskTags(taskIDWSSeed)
	if err != nil {
		t.Fatalf("TaskTags(exact): %v", err)
	}

	const unknown = "sess-ws-absent"
	for _, operand := range []string{unknown, " " + unknown + "\n "} {
		// TaskTagResumed refuses the operand, naming it.
		_, err := TaskTagResumed(taskIDWSSeed, operand)
		if err == nil {
			t.Fatalf("TaskTagResumed(%q) stamped an id that names no session — the existence check is gone", operand)
		}
		if !strings.Contains(err.Error(), unknown) {
			t.Errorf("the refusal must NAME the operand it rejected: %v", err)
		}
		// Nothing was written: neither the padded nor the bare spelling of the
		// unknown id may appear on the row.
		after, terr := TaskTags(taskIDWSSeed)
		if terr != nil {
			t.Fatalf("TaskTags(exact) after the refusal: %v", terr)
		}
		if after != before {
			t.Fatalf("tags changed on a REFUSED stamp: %q -> %q", before, after)
		}
	}
	// The count is untouched too (the phantom tag would have raised it).
	if n := TaskResumeCount(taskIDWSSeed); n != 1 {
		t.Errorf("TaskResumeCount = %d, want 1 — the refused operands each counted a session", n)
	}
	// And the padded TASK id still resolves the row for the readers: the
	// refusal above is about the SESSION operand, never about the normalization
	// this file exists to pin.
	if tags, err := TaskTags(taskIDWSPad); err != nil || tags != before {
		t.Errorf("TaskTags(padded) = (%q, %v), want the unchanged %q", tags, err, before)
	}
}

// TestTaskIDNormalizeIsTheSingleSeam is the Rule 13/14 anti-drift gate: the
// normalization rule has exactly ONE definition and every ledger accessor
// REACHES it instead of re-typing strings.TrimSpace(taskID). A future
// accessor copy-pasting the raw trim fails here (the gate is the guard the
// hardcoding review demands — a constant that exists must be referenced).
func TestTaskIDNormalizeIsTheSingleSeam(t *testing.T) {
	const (
		seamCall = "taskID = taskIDNormalize(taskID)"
		rawTrim  = "taskID = strings.TrimSpace(taskID)"
	)
	for _, file := range []string{"tasks.go", "task_session.go", "taskzombie.go",
		"finish_cleanup_survivors.go", "task_merge_executor.go", "reframe.go",
		"task_deliverable_worktree.go", "task_deliverable_branch.go",
		"memory_sync_task.go", "memory_sync_delta.go",
		// The residual write / filesystem / sync entry points (epic
		// taskid-whitespace-normalize-residual): every file that carries a
		// task_id entry point is gated here, so a future entry point that
		// re-types the trim fails THIS test rather than shipping.
		"task_claim_local.go", "memory_sync_terminal_push.go", "writetaskarea.go"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(body), rawTrim) {
			t.Errorf("%s still trims task_id inline (%q) — call %s instead (one seam)",
				file, rawTrim, "taskIDNormalize")
		}
	}
	// Per-file seam-call FLOOR. The floor is what turns the gate above from
	// "carries no inline trim" into "actually reaches the seam": deleting
	// every seam call (the mutation probe) leaves no literal behind, so the
	// raw-literal check alone would pass while the whole fix is reverted.
	// A new entry point in a listed file raises its count, never lowers it.
	for file, floor := range map[string]int{
		"tasks.go":                     taskIDSeamCallersFloor,
		"task_session.go":              taskIDSeamSessionCallersFloor,
		"task_deliverable_worktree.go": taskIDSeamWorktreeCallersFloor,
		"task_claim_local.go":          taskIDSeamClaimLocalCallersFloor,
		"memory_sync_terminal_push.go": taskIDSeamTerminalPushCallersFloor,
		"finish_cleanup_survivors.go":  taskIDSeamFinishCleanupCallersFloor,
	} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if n := strings.Count(string(body), seamCall); n < floor {
			t.Errorf("%s calls the normalization seam %d time(s), want >= %d — a task_id entry point stopped normalizing",
				file, n, floor)
		}
	}
	// The struct-input entry points (TaskSubmitOptions.TaskID — reached by both
	// TaskSubmit and TaskSubmitReturningID in tasks.go — and
	// WriteTaskAreaOptions.TaskID in writetaskarea.go): the
	// `taskID = taskIDNormalize(taskID)` shape cannot fit a field name, so
	// they are pinned by their own call sites.
	verifySrc, err := os.ReadFile("tasks.go")
	if err != nil {
		t.Fatalf("read tasks.go: %v", err)
	}
	if !strings.Contains(string(verifySrc), taskIDSeamStructInputCall) {
		t.Errorf("tasks.go no longer calls the seam on the struct input (%q) — a struct-input entry point stopped normalizing its task_id",
			taskIDSeamStructInputCall)
	}
	areaSrc, err := os.ReadFile("writetaskarea.go")
	if err != nil {
		t.Fatalf("read writetaskarea.go: %v", err)
	}
	if !strings.Contains(string(areaSrc), taskIDSeamWriteAreaInputCall) {
		t.Errorf("writetaskarea.go no longer calls the seam on its struct input (%q)", taskIDSeamWriteAreaInputCall)
	}
}

// taskIDSeamStructInputCall is the seam call for the entry points whose
// task_id arrives inside an options struct (TaskSubmitOptions.TaskID) — the
// `taskID = taskIDNormalize(taskID)` shape cannot fit a field name.
const taskIDSeamStructInputCall = "taskID := taskIDNormalize(opts.TaskID)"

// taskIDSeamWriteAreaInputCall is the same contract for the working-area
// writer (WriteTaskAreaOptions.TaskID).
const taskIDSeamWriteAreaInputCall = "opts.TaskID = taskIDNormalize(opts.TaskID)"

// taskIDSeamCallersFloor is the number of ledger accessors in tasks.go that
// MUST route their task_id through taskIDNormalize. It is a floor (a new
// accessor raises the count, never lowers it): dropping one drops below the
// floor and fails the gate above. Raised 16 → 19 by the residual epic
// (TaskStampRun / TaskStampSelfClaim / TaskResumeClaimed).
const taskIDSeamCallersFloor = 19

// The residual epic's per-file floors (same contract as the tasks.go floor
// above): task_session.go = taskSessionDir + TaskDeliverableAttachments +
// TaskCopyAttachments; task_deliverable_worktree.go = the existing reader +
// TaskDeliverableRepoFor.
//
// memory_sync_claim.go'S FLOOR MOVED WITH ITS CODE, not with its file name
// (Rule 43). That file was split when the cross-node claim lock was deleted:
// RollbackLocalClaim went to task_claim_local.go and the terminal-push entry
// points (SyncTaskPushOnly / SyncTaskPushOnlyPeers / PushTerminalTaskSync)
// went to memory_sync_terminal_push.go. The two floors sum to the old 5, so
// deleting a seam call still fails this gate — a floor that followed the file
// NAME would have read zero readers and gone quiet on a live entry point.
const (
	taskIDSeamSessionCallersFloor      = 3
	taskIDSeamWorktreeCallersFloor     = 2
	taskIDSeamClaimLocalCallersFloor   = 1 // RollbackLocalClaim
	taskIDSeamTerminalPushCallersFloor = 4 // SyncTaskPushOnly, SyncTaskPushOnlyPeers, PushTerminalTaskSync (+1)
	// 3, measured on this tree: 58b5618 moved the three surviving task_id entry
	// points (taskIDNormalize call sites) out of task_finish_cleanup.go into
	// finish_cleanup_survivors.go, so the floor follows the CODE, not the file
	// name. A drop below 3 means an entry point stopped normalizing.
	taskIDSeamFinishCleanupCallersFloor = 3
	taskIDSeamWriteAreaCallersFloor     = 1
)
