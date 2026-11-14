package main

// task_briefing_prose_test_seam_test.go — the SEVENTH clause at the door a real
// run walks through, driven on the REAL binary.
//
// WHY AN E2E IS THE ONLY HONEST PROOF HERE. The clause is a function of
// (ground, briefing) — but the thing the epic repairs is that a briefing REACHES
// THE CHILD with a dead test name in it and nobody says so. A unit test can call
// tools.TaskBriefingProseTestNamesRefresh directly (its sibling file does, over
// real directories); it cannot show that `gopi task run <row>` — the seam at
// cmd/agent/cli_tasks.go — actually invokes it, appends the stamp to the string
// bound into the child's stdin, and records the fact on the run's own stream.
// So this drives the production door with a real ledger row and a real mock
// model, exactly as the foreground-console e2e beside it does.
//
// THE RED PROOF is the sixth clause being SILENT on this shape, measured before
// this file was written and kept in this row's session directory
// (evidence/red-green-before.txt): the prose sentence carries no `-run` token,
// so the sixth clause — correctly, by its own contract — said nothing.
//
// BOTH DIRECTIONS come from the SAME walk, because "it stamped the dead one" is
// worth nothing without "it left the live one alone": the dead row's briefing
// names a test this ground implements, and the record must show it counted
// (1 top-level test function reaches it) without stamping it.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// proseSeamLiveTest is the test function the fixture ground REALLY implements —
// the control name every briefing below also carries.
const proseSeamLiveTest = "TestProseSeamLiveProbe"

// proseSeamDeadTest is the name the dead row writes and no tree implements —
// the shape of the incident (two retired `TestDeliveryRetry…` names).
const proseSeamDeadTest = "TestDeliveryRetryIsIdempotentOnADeliveredRow"

// proseSeamGroundRepo builds a git work tree carrying ONE real test file, so the
// census the clause takes has a population and the live control can reach it.
func proseSeamGroundRepo(t *testing.T, name string) string {
	t.Helper()
	repo := executorCwdProbeRepo(t, name)
	testFile := "package probe\n\nimport \"testing\"\n\nfunc " + proseSeamLiveTest + "(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(repo, "probe_test.go"), []byte(testFile), 0o644); err != nil {
		t.Fatalf("write probe_test.go: %v", err)
	}
	if out, err := proseSeamGit(repo, "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := proseSeamGit(repo, "commit", "-m", "probe test"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return repo
}

// proseSeamGit runs one git command in the fixture repo and returns its combined
// output — the tests below need a COMMITTED test file, because the census reads
// the tree the ground resolved.
func proseSeamGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestTaskRunStampsAProseNamedDeadTest is the epic's Ran: the briefing's tests
// are named in a SENTENCE (no `-run` token anywhere), one of them exists nowhere,
// and the seam stamps it — naming it and the reason — on the run's own stream,
// while leaving the live control unjudged.
func TestTaskRunStampsAProseNamedDeadTest(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "prose-seam probe answered"})
	t.Cleanup(mock.Close)
	repo := proseSeamGroundRepo(t, "prose-seam-project")
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())

	const taskID = "9e3f0c2a-4b17-4a51-8d0f-6b2c1e7a4d31"
	// THE INCIDENT'S SHAPE, verbatim: the test names in a sentence, with no
	// `go test … -run` command on the line.
	payload := "## Acceptance\n\n三条测试 " + proseSeamDeadTest + " 与 " + proseSeamLiveTest + " 必须转绿。\n"
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit", "prose seam dead name",
		"--payload", payload, "--task-id", taskID, "--repo", repo}, "", env, t.TempDir()); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	_, runStderr, code := runGopiMapped(t, []string{CliVerbWorker, taskID}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi worker %s` must succeed (the clause is a WARNING, never a refusal): code=%d\n%s",
			taskID, code, runStderr)
	}
	// The seam RECORDED the measurement (Rule 15: the destination is announced).
	if !strings.Contains(runStderr, "PROSE-NAMED TEST judged at dispatch") {
		t.Fatalf("the seam never recorded a prose-named-test judgement — the clause is not reached by the production door\n--- stderr ---\n%s", runStderr)
	}
	// The census ran over THIS ground: both names get a count line.
	if !strings.Contains(runStderr, "briefing prose test-name census →") {
		t.Fatalf("no census line on the run's stream\n--- stderr ---\n%s", runStderr)
	}
	if !strings.Contains(runStderr, "ZERO REACH \""+proseSeamDeadTest+"\"") {
		t.Errorf("the dead name was not stamped: the record carries no ZERO REACH line for %q\n--- stderr ---\n%s",
			proseSeamDeadTest, runStderr)
	}
	// THE LIVE CONTROL: the same sentence names a test this ground implements,
	// and it must be counted WITHOUT being stamped.
	if strings.Contains(runStderr, "ZERO REACH \""+proseSeamLiveTest+"\"") {
		t.Errorf("%q exists in this ground and must NOT be stamped\n--- stderr ---\n%s", proseSeamLiveTest, runStderr)
	}
	if !strings.Contains(runStderr, "\""+proseSeamLiveTest+"\": 1 top-level test function(s)") {
		t.Errorf("%q was not counted against this ground — a census that judged it nothing is the defect\n--- stderr ---\n%s",
			proseSeamLiveTest, runStderr)
	}
	// THE STAMP REALLY REACHED THE CHILD. The stream says the seam DECIDED to
	// append; the evidence that the bytes arrived is the model request the child
	// actually sent — the briefing rides its prompt, so the clause must be in it.
	// The wire body is JSON, and encoding/json escapes `<`, `>` and `&` as
	// \u003c-forms — so a raw substring match would MISS the clause's own
	// instruction (`go test -run <dead name>`) on a body that really carries it.
	// The search therefore runs over the DECODED strings, never the wire bytes.
	body := ""
	for n := 1; n <= mock.RequestCount(); n++ {
		decoded := mockDecodedJSONStrings(t, mock.RequestBody(n))
		if strings.Contains(decoded, tools.TaskBriefingProseTestNamesLabel) {
			body = decoded
			break
		}
	}
	if body == "" {
		t.Fatalf("the clause never reached the CHILD's prompt (%d request(s) served) — the seam recorded a stamp the child did not receive",
			mock.RequestCount())
	}
	if !strings.Contains(body, proseSeamDeadTest) || !strings.Contains(body, tools.TaskBriefingProseTestNamesVerifyOnly) {
		head := body
		if len(head) > 3000 {
			head = head[:3000] + "\n…(body truncated; full body is %d chars)"
		}
		t.Errorf("the child's briefing carries the label but not the dead NAME (%q) and the instruction (%q)\n--- body head ---\n%s",
			proseSeamDeadTest, tools.TaskBriefingProseTestNamesVerifyOnly, head)
	}
}

// TestTaskRunStaysSilentOnAProseBriefingOfLiveNames is the other direction from
// the SAME door: a sentence whose names all exist stamps nothing and records no
// judgement line — the ordinary briefing, which must not degrade.
func TestTaskRunStaysSilentOnAProseBriefingOfLiveNames(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "prose-seam probe answered"})
	t.Cleanup(mock.Close)
	repo := proseSeamGroundRepo(t, "prose-seam-live-project")
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())

	const taskID = "2a7c5b91-0d43-4e68-9b16-7f8a3c5d2e04"
	payload := "## Acceptance\n\n三条测试 " + proseSeamLiveTest + " 必须转绿。\n"
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit", "prose seam live name",
		"--payload", payload, "--task-id", taskID, "--repo", repo}, "", env, t.TempDir()); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	_, runStderr, code := runGopiMapped(t, []string{CliVerbWorker, taskID}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi worker %s` must succeed: code=%d\n%s", taskID, code, runStderr)
	}
	if strings.Contains(runStderr, "PROSE-NAMED TEST judged at dispatch") {
		t.Errorf("stamped a briefing whose every prose-named test exists — the clause must be SILENT here\n--- stderr ---\n%s", runStderr)
	}
	if !strings.Contains(runStderr, "\""+proseSeamLiveTest+"\": 1 top-level test function(s)") {
		t.Errorf("the census did not run at all on the live row — silence must come from a measurement, never from a skipped scan\n--- stderr ---\n%s", runStderr)
	}
}

// mockDecodedJSONStrings decodes one mock request body into the concatenation of
// every JSON string value it carries — keys and structure dropped, escapes
// resolved. See the note at its call site: an undecoded match would miss text
// that JSON escaped (`<` -> `\u003c`), which is exactly the clause's instruction.
func mockDecodedJSONStrings(t *testing.T, raw string) string {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("the mock's request body is not JSON: %v", err)
	}
	var b strings.Builder
	var walk func(x interface{})
	walk = func(x interface{}) {
		switch y := x.(type) {
		case string:
			b.WriteString(y)
			b.WriteString("\n")
		case []interface{}:
			for _, e := range y {
				walk(e)
			}
		case map[string]interface{}:
			for _, e := range y {
				walk(e)
			}
		}
	}
	walk(v)
	return b.String()
}
