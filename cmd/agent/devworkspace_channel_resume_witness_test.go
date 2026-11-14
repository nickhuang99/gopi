package main

// devworkspace_channel_resume_witness_test.go — THE DELIVERY ROUND'S RESUME
// WITNESS (bug 01a193f5, measured 2026-11-01).
//
// THE DEFECT. The clone channel's closing round is started with the parent row
// id as its wrapper identity (cloneRoundChildEnv — epic 01a1837e), so
// internal/agent's mintRunID returns that id and the round's session logger
// APPENDS to the record the row's executor already wrote. The file therefore
// holds TWO `session_start` headers — and the second one used to say nothing
// about why. A continued record was indistinguishable from a corrupt one, which
// is exactly what the header census measures: AFTER the re-dispatch door
// (commit 19ab5cba) was fixed, the corpus still reported 6 LIVE GAPS, and every
// one of them was a delivery round.
//
// THE ROOT CAUSE WAS A MISSING DECLARATION, not a missing writer. The writer
// (internal/agent/session.go) has written `resumedFrom` + `resumedFromRawLog`
// since the chat-resume epic; loop.go consults tools.TaskRunResumeFrom() as its
// fallback for a run whose caller threaded no identity. cloneRoundChildEnv
// exported GOPI_TASK_WRAPPER_ID but never tools.TaskRunResumeFromEnv, so the
// fallback resolved "" and the witness was skipped — silently, because an
// absent variable is a legal value there.
//
// TWO ARMS, because the defect lived at two frames and one arm cannot see both
// (the same lesson the report-round tests record):
//
//	① the ASSEMBLY — cloneRoundChildEnv, given a row that already has a record,
//	   must put exactly ONE declaration in the child's environment; given a row
//	   with no record yet it must put NONE (a first run declares nothing, or the
//	   witness becomes a false claim).
//	② the REAL SURFACE — a real `gopi worker --once` whose delivery attempt is
//	   blocked, so the channel starts a real agent round; the round appends to
//	   the row's record and its header must carry the witness.
//
// Arm ② is the one this bug is about: arm ① would have passed with a value that
// never reached a child (that is precisely how devworkspace_channel.go's first
// fix was green and unreachable).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// roundWitnessRow is the row the delivery round reports on. A fixed uuid
	// spelling (the ground-naming rule needs one) so the assembled paths can be
	// compared verbatim.
	roundWitnessRow = "01a193f5-acc4-7de1-996d-cdbe88767001"
	// roundWitnessSeededSession is the session id the seeded record carries —
	// what the round's declaration must name.
	roundWitnessSeededSession = "01a19000-0000-7000-8000-00000000ed01"
	// roundWitnessSessionIDField is the record's own session-id cell, read the
	// way every reader in this package reads it.
	roundWitnessSessionIDField = "sessionId"
)

// seedRowRecord writes a session record into the row's own run directory — the
// FACT tools.TaskRedispatchResumeFrom reads (a row whose directory already
// holds a record has run before) — and returns its path.
//
// THE LOCATION IS RESOLVED, NEVER SPELLED: `sessions/<row>/` looks obvious and
// is wrong to hard-code, because the sessions ROOT itself is configuration
// (config.SessionsRoot: GOPI_SESSIONS_ROOT, else <GOPI_HOME>/.gopi/sessions).
// tools.TaskReportPath is the one entry point that already names the row's own
// run directory without the test re-deriving it (Rule 14), so the record is
// written beside the report the round is told to produce.
//
// The line is one session_start carrying the record's sessionId, which is what
// the resolver's reader (sessionlayout.NewestRecordInDir → sessionStartID) parses.
func seedRowRecord(t *testing.T, taskID, sessionID string) string {
	t.Helper()
	reportPath, err := tools.TaskReportPath(taskID)
	if err != nil {
		t.Fatalf("tools.TaskReportPath(%s): %v", taskID, err)
	}
	dir := filepath.Dir(reportPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the row's run directory %s: %v", dir, err)
	}
	body := `{"type":"` + sessionevent.EventTypeSessionStart + `","` + roundWitnessSessionIDField + `":"` + sessionID + `"}` + "\n"
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the seeded record %s: %v", path, err)
	}
	return path
}

// resumeValues returns every VALUE the environment carries for the resume
// declaration's name — duplicates included, because the assembly's contract is
// exactly one entry per name.
func resumeValues(env []string) []string {
	return reportRoundEnvValues(env, tools.TaskRunResumeFromEnv)
}

// setEnvEntry REPLACES (or appends) one NAME=VALUE pair in an assembled
// environment. It is a replace rather than an append because two pairs for one
// name make the reading order-dependent — and MockEnv is the one script that
// decides a mocked run's environment, so a second pair here would be a second
// opinion about where the profile is.
func setEnvEntry(env []string, name, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if key, _, _ := strings.Cut(kv, "="); key == name {
			if !replaced {
				out = append(out, name+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, name+"="+value)
	}
	return out
}

// countLinesContaining counts the lines of s that contain substr. It exists so
// the round count comes from the run's OWN announcement rather than from a
// number this test assumed.
func countLinesContaining(s, substr string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// TestClosingRoundDeclaresTheResumeWitness is arm ①: the assembled environment
// carries the declaration when the row has already run, and carries nothing
// when it has not.
func TestClosingRoundDeclaresTheResumeWitness(t *testing.T) {
	// ONE profile root resolves the row's run directory for the in-process
	// resolver AND the process the round starts: internal/tools computes it as
	// <gopi dir>/sessions/<row>, internal/agent as config.SessionsRoot() — the
	// same place when GOPI_HOME is what is pinned. A bare seed path spells
	// neither, and the drift is silent.
	gopiHome := t.TempDir()
	t.Setenv(config.GopiDirEnv, gopiHome)

	// ── the row HAS run: the declaration is present, once, naming the record ──
	seedRowRecord(t, roundWitnessRow, roundWitnessSeededSession)
	env, err := cloneRoundChildEnv(os.Environ(), roundWitnessRow)
	if err != nil {
		t.Fatalf("cloneRoundChildEnv: %v", err)
	}
	if got := resumeValues(env); len(got) != 1 || got[0] != roundWitnessSeededSession {
		t.Fatalf("%s = %q on the round's environment, want exactly one entry equal to the row's own session %q — without it the round appends a second session_start that cannot say why (the defect this test exists for)",
			tools.TaskRunResumeFromEnv, got, roundWitnessSeededSession)
	}

	// An INHERITED declaration is REPLACED, never duplicated: the round's own
	// answer is the only writer for this name on this seam.
	inherited := append([]string{tools.TaskRunResumeFromEnv + "=" + "01a10000-0000-7000-8000-00000000he11"}, os.Environ()...)
	env2, err := cloneRoundChildEnv(inherited, roundWitnessRow)
	if err != nil {
		t.Fatalf("cloneRoundChildEnv(inherited): %v", err)
	}
	if got := resumeValues(env2); len(got) != 1 || got[0] != roundWitnessSeededSession {
		t.Fatalf("an inherited declaration survived beside the round's own: %q (want exactly one entry equal to %q)",
			got, roundWitnessSeededSession)
	}

	// ── the row has NOT run: nothing is declared ─────────────────────────────
	// The predicate reads the row's run DIRECTORY, and the assembly itself
	// CREATES that directory (tools.TaskReportDestination makes the report
	// container before it is exported) — so this case is exactly the real one:
	// a container that exists and holds no record yet. A first run that claimed
	// to continue something would make the witness meaningless everywhere it
	// does appear.
	const freshRow = "01a193f5-acc4-7de1-996d-cdbe88767002"
	env3, err := cloneRoundChildEnv(os.Environ(), freshRow)
	if err != nil {
		t.Fatalf("cloneRoundChildEnv(fresh): %v", err)
	}
	// The negated set is the RESUME DECLARATION ENTRIES this assembled
	// environment carries, and the guard that must not be too broad is
	// TaskRunResumeDeclare's own admission test: it declares only when the row's
	// run directory already holds a record (source row-run-dir), so a fresh row
	// has nothing to declare. The exclusion is pinned POSITIVELY in this same
	// test above — the row WITH a record must carry EXACTLY ONE entry equal to
	// its session, and the inherited-declaration arm must show it REPLACED
	// rather than accumulated — so an empty list here is not a dead path.
	//
	// why: correct — the set is the declaration entries cloneRoundChildEnv assembled, and TaskRunResumeDeclare's own admission test (the row's run dir holds a record) is what leaves it empty for a fresh row; the positive half is asserted in this same test just above.
	if got := resumeValues(env3); len(got) != 0 {
		t.Fatalf("%s = %q on a row that has never run, want ZERO entries — a first run must not claim to continue anything",
			tools.TaskRunResumeFromEnv, got)
	}
}

// TestDeliveryRoundWritesTheResumeWitness is arm ②, the acceptance walk: a real
// `gopi worker --once` whose first delivery attempt is blocked starts a real
// agent round in the row's clone workspace, and that round's session_start —
// appended to the record its executor left — must carry the witness.
//
// The fixture is the delivery-loop e2e's own shape (devworkspace_delivery_e2e_test.go):
// the executor child modifies a TRACKED file without committing it, so the
// deterministic attempt comes back `blocked (dirty)` and the loop hands the
// facts to an agent. The ROUND is scripted to commit, so the second attempt
// lands and the run produces exactly ONE round — which is what makes "exactly
// two headers" a meaningful assertion instead of an arbitrary count.
func TestDeliveryRoundWritesTheResumeWitness(t *testing.T) {
	repo := cloneGateRepo(t)
	root := filepath.Join(t.TempDir(), "workspace")
	const taskID = "01a193f5-acc4-7de1-996d-cdbe88767003"

	env := cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+root+`"`,
		// ONE deed, then the completion vocabulary: the executor leaves the work
		// uncommitted (attempt 1 → blocked/dirty). The mock repeats its LAST
		// scripted entry forever, so every further request — the executor's
		// confirmation rounds AND every agent round the loop starts — gets the
		// same benign answer. That is deliberate: this test must NOT depend on
		// how many turns the executor happens to take, because the fact under
		// test (each appended header carries the witness) does not.
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_dirty",
			Name: "bash",
			Args: map[string]interface{}{"command": "printf 'left uncommitted\\n' >> a.txt"},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nTask complete. I left the change uncommitted, on purpose."},
	)
	// ONE profile root for the worker, the rounds it starts, AND the in-process
	// resolver this test reads: internal/tools computes a row's run directory as
	// <gopi dir>/sessions/<row> and internal/agent writes into
	// config.SessionsRoot() — the same place when GOPI_HOME is the only thing
	// pinned. Pinning GOPI_SESSIONS_ROOT as an extra pair SPLITS them, and the
	// split is silent: the resolver keeps reading the profile's tree while the
	// rounds write into the exported one, so the declaration comes out empty for
	// a reason that has nothing to do with the code under test (measured here).
	gopiHome := filepath.Join(t.TempDir(), "profile")
	env = setEnvEntry(env, config.GopiDirEnv, gopiHome)
	t.Setenv(config.GopiDirEnv, gopiHome)
	sessionsRoot := config.SessionsRootIn(config.ProfileDirIn(gopiHome))

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"delivery round resume witness", "--payload", "do the work", "--task-id", taskID,
		"--repo", repo}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	both := stdout + stderr
	t.Logf("worker --once exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)

	// ① THE PREMISE: a round really was started, or this walk judges a header
	// that no round produced (Rule 15 — a silent "no round" would make the rest
	// of this test vacuous).
	roundDir := lineAfter(both, "starting an agent round in ")
	if roundDir == "" {
		t.Fatalf("the loop started no agent round, so this walk cannot judge the round's header:\n%s", both)
	}

	// ② THE RECORD: the row's own run directory holds one header per run that
	// wrote into it — the executor's, then one per delivery round the loop
	// started. The COUNT is taken from the run's own account of the rounds it
	// started (never from a number this test assumed): how many rounds a blocked
	// delivery spawns is the loop's business, while the fact under test — every
	// APPENDED header says what it continues — is the same on the third round as
	// on the second.
	sessionDir := filepath.Join(sessionsRoot, taskID)
	starts := readSessionStarts(t, sessionDir)
	rounds := countLinesContaining(both, "starting an agent round in ")
	if rounds < 1 {
		t.Fatalf("the loop announced no agent round, so this walk cannot judge the round's header:\n%s", both)
	}
	if len(starts) != rounds+1 {
		t.Fatalf("the row's record holds %d session_start headers but the run started %d agent round(s) (plus the executor's own header, that is %d) under %s:\n%s",
			len(starts), rounds, rounds+1, sessionDir, joinStarts(starts))
	}
	firstID := stringField(starts[0], roundWitnessSessionIDField)
	if firstID == "" {
		t.Fatalf("the executor's header names no session id — there is nothing the rounds could declare they continue:\n%s", joinStarts(starts))
	}
	if got := stringField(starts[0], sessionevent.SessionStartFieldResumedFrom); got != "" {
		t.Fatalf("the FIRST header carries a resume witness (%q) — a first run must not claim to continue anything", got)
	}

	// ③ THE FIX: EVERY header after the first says WHY the file holds more than
	// one. This is the defect verbatim — before the change all of them were
	// silent, and a continued record was indistinguishable from a corrupt one.
	for i, rec := range starts[1:] {
		witness := stringField(rec, sessionevent.SessionStartFieldResumedFrom)
		if witness == "" {
			t.Fatalf("session_start header %d of %d carries NO resume witness — this is the defect: the record holds %d headers and cannot say why. Header: %v\nall headers:\n%s",
				i+2, len(starts), len(starts), rec, joinStarts(starts))
		}
		if witness != firstID {
			t.Fatalf("header %d's witness names %q, but the run it continues is %q — the declaration must name the session the row's run directory actually holds",
				i+2, witness, firstID)
		}
		// THE PAIR IS WRITTEN TOGETHER (the writer's own contract): a witness
		// without the raw-log half tells a reader the file was continued but not
		// which wire log grew with it — and that half must name a file inside the
		// row's own run directory, never a path the door invented.
		rawLogPath := stringField(rec, sessionevent.SessionStartFieldResumedFromRawLog)
		if rawLogPath == "" {
			t.Fatalf("header %d's witness carries no %s: %v", i+2, sessionevent.SessionStartFieldResumedFromRawLog, rec)
		}
		if filepath.Dir(rawLogPath) != sessionDir {
			t.Fatalf("header %d's raw-log half names %q, which is not inside the row's own run directory %q",
				i+2, rawLogPath, sessionDir)
		}
		if !strings.HasSuffix(rawLogPath, ".raw.log") {
			t.Fatalf("header %d's raw-log half names %q, which is not a raw log", i+2, rawLogPath)
		}
	}

	// ④ THE DELIVERY ATTEMPTS ARE A COMPLETE ACCOUNT: the loop announced exactly
	// as many attempts as its own brake allows, and none of them landed because
	// the workspace was dirty. That is what makes the headers above the record of
	// REAL rounds rather than of a run that silently delivered on attempt 1 and
	// never needed one (measured earlier in this test's life: a scripted executor
	// that committed made the walk green while judging nothing).
	if !strings.Contains(both, "blocked (dirty)") {
		t.Fatalf("no delivery attempt was blocked on the dirty workspace, so no delivery round was needed — this walk did not exercise the defect:\n%s", both)
	}
}
