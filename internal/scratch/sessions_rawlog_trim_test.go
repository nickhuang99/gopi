package scratch

// The raw-log BOUND's contract, driven through the real rewrite (TrimRawLog) on
// real files under the scratch tree (Rule 22: never a temp dir on `/`, never
// inside the source tree — the fixtures reuse sessionsHome/sessionsRoot from
// sessions_gc_test.go, which pin GOPI_HOME to a fresh scratch tree).
//
// The claims the epic makes, one test each, plus the two refusals:
//
//	① a file WITHIN the cap is never touched — its bytes are identical after
//	② a rewritten file is <= the cap, still opens with its original header, and
//	  still ends with a COMPLETE last line
//	③ the file says what it lost: one marker line naming the dropped byte count
//	④ a live process holding the file open vetoes the rewrite
//	⑤ a cap below the floor is refused by name, not silently accepted

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rawLogFixture writes a synthetic raw log of roughly size bytes made of
// complete lines carrying the writer's own round markers, and returns its path.
//
// The shape mirrors internal/agent/rawlog.go: a `#` creation header, then
// "→ REQUEST" / body / "← RESPONSE" + body triples. That is what the tail
// alignment and the "the header survives" assertions are measured against.
func rawLogFixture(t *testing.T, dir string, size int64) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the fixture container: %v", err)
	}
	path := filepath.Join(dir, "fixture"+SessionRawLogFileSuffix)
	var b bytes.Buffer
	fmt.Fprintf(&b, "# gopi session raw HTTP log — created %s pid=1 session=fixture log=fixture.raw.log\n",
		time.Now().UTC().Format(time.RFC3339))
	round := 0
	for int64(b.Len()) < size {
		round++
		b.WriteString("→ REQUEST\n")
		fmt.Fprintf(&b, `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"round %d %s"}]}`+"\n",
			round, strings.Repeat("x", 96))
		b.WriteString("← RESPONSE\n")
		fmt.Fprintf(&b, `{"content":[{"type":"text","text":"answer %d"}]}`+"\n", round)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write the fixture raw log: %v", err)
	}
	return path
}

// rawLogHeldOpen spawns a real process with the file held open (tail -f), which
// is what the /proc fd arm must see. A simulated pid would test nothing.
func rawLogHeldOpen(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("tail", "-f", path)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a holder process (tail -f): %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill() //stylegate:ignore the process this test spawned itself; a kill error cannot fail the test meaningfully
		}
		_ = cmd.Wait() //stylegate:ignore reap of the test's own child
	})
	// Give the child a moment to open the file, so the scan sees a real fd link
	// instead of racing its own exec.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fds, err := os.ReadDir(filepath.Join(ProcRootDir, fmt.Sprint(cmd.Process.Pid), ProcFdName)); err == nil && len(fds) > 0 {
			return cmd
		}
		//stylegate:allow-sleep bounded retry cadence only: the loop above returns the instant the child's fd link is readable, so this wait is never what the assertion rests on and procfs offers no event to wait for instead
		time.Sleep(20 * time.Millisecond)
	}
	return cmd
}

// TestRawLogTrimKeepsWithinCapUntouched is claim ①: a file already inside the cap
// is a NON-candidate and its bytes are not rewritten — measured by content
// identity, not by the function's own report.
func TestRawLogTrimKeepsWithinCapUntouched(t *testing.T) {
	root := sessionsRoot(t)
	path := rawLogFixture(t, filepath.Join(root, sessGCOldUUID), 64<<10)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture before trimming: %v", err)
	}
	capBytes := int64(len(before)) + 1024

	if err := checkRawLogCap(capBytes); err != nil {
		t.Fatalf("the fixture's cap is below this arm's floor: %v", err)
	}
	out, err := TrimRawLog(path, capBytes)
	if err != nil {
		t.Fatalf("TrimRawLog on a within-cap file returned an error instead of doing nothing: %v", err)
	}
	if out.Trimmed {
		t.Fatalf("a %d-byte file under a %d-byte cap reports Trimmed=true — it must never be rewritten",
			out.Before, capBytes)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture after trimming: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a within-cap file CHANGED on disk (%d -> %d bytes) — the bound must not touch it",
			len(before), len(after))
	}
}

// TestRawLogTrimBoundsAndKeepsHeadAndWholeTail is claims ② and ③: the rewrite
// lands the file at or under the cap, the original header still opens it, the
// last line is whole, and one marker line states the loss.
func TestRawLogTrimBoundsAndKeepsHeadAndWholeTail(t *testing.T) {
	root := sessionsRoot(t)
	path := rawLogFixture(t, filepath.Join(root, sessGCOldUUID), 1<<20) // 1 MiB of rounds
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}
	capBytes := int64(128 << 10)
	if before.Size() <= capBytes {
		t.Fatalf("the fixture (%d bytes) does not exceed the cap (%d) — the test would prove nothing",
			before.Size(), capBytes)
	}

	out, err := TrimRawLog(path, capBytes)
	if err != nil {
		t.Fatalf("TrimRawLog: %v", err)
	}
	if !out.Trimmed {
		t.Fatalf("a %d-byte file over a %d-byte cap reports Trimmed=false", before.Size(), capBytes)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the rewritten file: %v", err)
	}
	if after.Size() > capBytes {
		t.Errorf("the rewritten file is %d bytes, above the %d-byte cap", after.Size(), capBytes)
	}
	if after.Size() != out.After {
		t.Errorf("the outcome reports After=%d but the file on disk is %d bytes — the report must be MEASURED",
			out.After, after.Size())
	}
	// The mode survives: a rewritten audit record must not become world-writable.
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("the rewrite changed the file mode %v -> %v", before.Mode().Perm(), after.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the rewritten file: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if !strings.HasPrefix(lines[0], "# gopi session raw HTTP log — created ") {
		t.Errorf("the rewritten file no longer opens with the creation header; first line = %q", lines[0])
	}
	markers := 0
	for _, ln := range lines {
		if !strings.HasPrefix(ln, SessionsRawLogTrimMarkerPrefix) {
			continue
		}
		markers++
		if !strings.Contains(ln, "dropped ") {
			t.Errorf("the trim marker does not state how many bytes were dropped: %q", ln)
		}
	}
	if markers != 1 {
		t.Fatalf("the rewritten file carries %d trim markers, want exactly 1", markers)
	}
	// The last line is COMPLETE: trimming on a partial line would make the final
	// record look corrupt to every reader downstream.
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("the rewritten file does not end with a newline — its last line is incomplete")
	}
	// The tail survives: rounds are still present, so the trim bounded the file
	// instead of emptying it.
	if !strings.Contains(string(data), "→ REQUEST") {
		t.Errorf("the rewritten file carries no round marker at all — the tail was lost")
	}
}

// TestRawLogTrimRefusesACapBelowTheFloor is refusal ⑤: a cap that would keep no
// round is refused by name rather than producing a file with nothing in it.
func TestRawLogTrimRefusesACapBelowTheFloor(t *testing.T) {
	root := sessionsRoot(t)
	rawLogFixture(t, filepath.Join(root, sessGCOldUUID), 1<<20)
	if _, err := TrimRawLog(filepath.Join(root, sessGCOldUUID, "fixture"+SessionRawLogFileSuffix),
		SessionsRawLogTrimMinCapBytes-1); err == nil {
		t.Fatalf("a cap of %d bytes (below the %d-byte floor) was accepted",
			SessionsRawLogTrimMinCapBytes-1, SessionsRawLogTrimMinCapBytes)
	}
	if _, err := PlanRawLogTrim(root, SessionsRawLogTrimMinCapBytes-1); err == nil {
		t.Fatalf("the PLAN accepted a cap below the floor instead of refusing it")
	}
}

// TestRawLogTrimPlanIsReadOnly is the dry run's own contract: planning names the
// candidates and the bytes it would free, and changes nothing on disk.
func TestRawLogTrimPlanIsReadOnly(t *testing.T) {
	root := sessionsRoot(t)
	path := rawLogFixture(t, filepath.Join(root, sessGCOldUUID), 1<<20)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}

	plans, err := PlanRawLogTrim(root, 128<<10)
	if err != nil {
		t.Fatalf("PlanRawLogTrim: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("the plan lists %d raw logs, want 1", len(plans))
	}
	p := plans[0]
	if !p.Trim {
		t.Fatalf("a %d-byte log under a %d-byte cap is not a candidate: %s", p.Bytes, 128<<10, p.Reason)
	}
	if p.DropBytes <= 0 {
		t.Fatalf("the plan promises %d bytes saved for an over-cap log", p.DropBytes)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture after planning: %v", err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("PLANNING wrote to the file: %d -> %d bytes, mtime %v -> %v",
			before.Size(), after.Size(), before.ModTime(), after.ModTime())
	}
	if _, err := os.Stat(path + SessionsRawLogTrimTmpSuffix); err == nil {
		t.Fatalf("planning left a %s file beside the log", SessionsRawLogTrimTmpSuffix)
	}
}

// TestRawLogTrimRefusesAFileALiveProcessHolds is refusal ④: a running process
// holding the log open vetoes the rewrite — through the ONE holder probe this
// package already owns, not a second one.
func TestRawLogTrimRefusesAFileALiveProcessHolds(t *testing.T) {
	root := sessionsRoot(t)
	path := rawLogFixture(t, filepath.Join(root, sessGCOldUUID), 1<<20)
	held := rawLogHeldOpen(t, path)

	plans, err := PlanRawLogTrim(root, 128<<10)
	if err != nil {
		t.Fatalf("PlanRawLogTrim with a live holder: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("the plan lists %d logs, want 1", len(plans))
	}
	if plans[0].Trim {
		t.Fatalf("a log held open by a live process (pid %d) is a TRIM candidate", held.Process.Pid)
	}
	if plans[0].Reason != SessionsRawLogHeldReason {
		t.Fatalf("the refusal reason is %q, want %q", plans[0].Reason, SessionsRawLogHeldReason)
	}
}

// TestLargestSessionsOrdersByBytesDescending pins the ONE thing `--largest`
// promises: descending order by bytes, and a read-only pass. The three
// containers' sizes deliberately disagree with their names, so an
// implementation that sorted by name cannot pass by accident.
func TestLargestSessionsOrdersByBytesDescending(t *testing.T) {
	root := sessionsRoot(t)
	withLedger(t, map[string]string{}, nil)
	names := []string{sessGCOldUUID, sessGCOldUUID2, sessGCFreshID}
	sizes := []int64{1 << 20, 3 << 20, 2 << 20}
	var before int64
	for i, name := range names {
		dir := sessionContainer(t, root, name)
		if err := os.WriteFile(filepath.Join(dir, "payload.bin"), make([]byte, sizes[i]), 0o600); err != nil {
			t.Fatalf("write the payload of container %d: %v", i, err)
		}
		before += sizes[i]
	}

	plans, err := LargestSessions(root, SessionsGCStaleAge, 2)
	if err != nil {
		t.Fatalf("LargestSessions: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("LargestSessions(limit=2) returned %d rows", len(plans))
	}
	if plans[0].Bytes < plans[1].Bytes {
		t.Fatalf("rows are not in descending byte order: %d then %d", plans[0].Bytes, plans[1].Bytes)
	}
	if plans[0].Name != sessGCOldUUID2 {
		t.Fatalf("the biggest container is %q, want %q (the 3 MiB one)", plans[0].Name, sessGCOldUUID2)
	}
	// READ-ONLY: every payload is still exactly where it was written.
	for i, name := range names {
		fi, serr := os.Stat(filepath.Join(root, name, "payload.bin"))
		if serr != nil {
			t.Fatalf("the payload of %s disappeared: %v", name, serr)
		}
		if fi.Size() != sizes[i] {
			t.Fatalf("the payload of %s changed size %d -> %d — the listing wrote something", name, sizes[i], fi.Size())
		}
	}
}
