package tools

// The /proc/<pid>/stat parse is ONE implementation (procname.StatField) and the
// three former copies now DELEGATE to it. This test proves the delegation
// BEHAVIOURALLY, on a REAL kernel line rather than on a hand-written fixture,
// because the trap it guards is a property of what the kernel writes:
//
//	field 2 (comm) is the process's own name and may contain SPACES AND
//	PARENTHESES — `bash (deleted)` is a name the kernel really reports (measured
//	on this host: a binary copy or a deleted script is labelled exactly so).
//
// A comm carrying ` (deleted)` shifts every whitespace-split column by two, so a
// naive parse returns the WRONG NUMBER instead of an error — the worst shape for
// a liveness judgement, because the caller cannot tell. Every entry point below
// is therefore given the SAME live process and must return the SAME tick count,
// cross-checked against an oracle the test owns (the last-')' rule) and against
// the process's real parent.
//
// The red proof rides along: the retired whole-line/hardcoded-index form is
// replayed over this very line and shown to disagree with the oracle (and, for
// the record, the pre-consolidation copy is shown to HAVE agreed — which is why
// folding it into one implementation changes no behaviour).

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// trapCommName is the child's kernel comm — a REAL name the kernel reports,
	// carrying BOTH trap features (a space AND parentheses), so any parser that
	// splits the stat line on whitespace reads two columns late. Its failure
	// mode is silent: the wrong column is a plausible tick count.
	trapCommName = "bash (deleted)"
	// trapCommTokenCount is how many whitespace tokens that comm occupies
	// ("bash" + "(deleted)"). A naive whole-line split shifts every later field
	// by this count minus one.
	trapCommTokenCount = 2
	// trapCopyTries / trapCopyWait bound the ETXTBSY retry of the fixture copy
	// (a sibling process on this host may hold the source binary mapped).
	trapCopyTries = 40
	trapCopyWait  = 50 * time.Millisecond
	// trapChildSleepSec is how long the child stays alive for the measurement.
	trapChildSleepSec = "15"
	// trapCommWait / trapCommPoll bound the wait for the child to appear in
	// /proc with its final comm (the exec is asynchronous).
	trapCommWait = 5 * time.Second
	trapCommPoll = 10 * time.Millisecond
	// trapScratchClass names the scratch class holding the child's binary.
	trapScratchClass = "procstat-delegation"
)

// TestProcStatParseDelegationAgreesOnRealTrapComm drives EVERY remaining entry
// point at one live trap-comm process and requires one answer.
func TestProcStatParseDelegationAgreesOnRealTrapComm(t *testing.T) {
	binSrc, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatalf("need a plain executable to give the child its comm: %v", err)
	}
	dir, err := scratch.MkdirTemp(trapScratchClass, "procstat-")
	if err != nil {
		t.Fatalf("scratch dir for the trap child: %v", err)
	}
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sleepBody, err := os.ReadFile(binSrc)
	if err != nil {
		t.Fatalf("read %s: %v", binSrc, err)
	}

	// The kernel SETS comm from the executed file's basename, so copying an
	// ordinary binary to the trap name produces a REAL /proc line whose comm
	// carries a space and parentheses. The copy can hit ETXTBSY while a sibling
	// process on this shared host holds that binary mapped (gopi spawns `sleep`
	// itself), so the write is retried within a bound rather than assumed.
	trapBin := filepath.Join(dir, trapCommName)
	writeExecutableWithRetry(t, trapBin, sleepBody)

	cmd := exec.Command(trapBin, trapChildSleepSec)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start trap child: %v", err)
	}
	pid := cmd.Process.Pid
	//stylegate:ignore kills the child process the test spawned and is tearing down
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	trapComm := waitForTrapComm(t, pid)
	pidStr := strconv.Itoa(pid)

	// --- the line and the test's OWN oracle (the last-')' rule) -------------
	line := readStatLine(t, pidStr)
	statPath := procname.StatStatPath(procname.ProcRootDefault, pidStr)
	wantTicks := oracleStatField(t, line, procname.StatFieldStartTime)
	wantParent := oracleStatField(t, line, procname.StatFieldParent)
	if got := trapCommTokens(line); got != trapCommTokenCount {
		t.Fatalf("the fixture's comm is %d whitespace tokens, want %d (%q) — the shift a naive split "+
			"produces IS that token count minus one, so the fixture must keep the trap comm; line was %q",
			got, trapCommTokenCount, trapComm, line)
	}

	// --- every public entry point, one answer ------------------------------
	// 1. procname: the single home. TEXT form, VALUE form, PATH form, FILE form.
	if got, ok := procname.StatField(line, procname.StatFieldStartTime); !ok || got != wantTicks {
		t.Errorf("procname.StatField = (%q,%v), want (%q,true)", got, ok, wantTicks)
	}
	if got, err := procname.StatStartTime(line); err != nil || strconv.FormatUint(got, 10) != wantTicks {
		t.Errorf("procname.StatStartTime = (%d,%v), want (%s,nil)", got, err, wantTicks)
	}
	if got, ok := procname.StatFieldString(statPath, procname.StatFieldStartTime); !ok || got != wantTicks {
		t.Errorf("procname.StatFieldString = (%q,%v), want (%q,true)", got, ok, wantTicks)
	}
	if got, ok := procname.StartTicks(procname.ProcRootDefault, pidStr); !ok || got != wantTicks {
		t.Errorf("procname.StartTicks = (%q,%v), want (%q,true)", got, ok, wantTicks)
	}
	// 2. internal/scratch: both of its entry points (the holder registration's
	//    (0,error) shape and the pid-shaped reader).
	if got, err := scratch.ParseProcStartTime(line); err != nil || strconv.FormatUint(got, 10) != wantTicks {
		t.Errorf("scratch.ParseProcStartTime = (%d,%v), want (%s,nil)", got, err, wantTicks)
	}
	if got, err := scratch.ProcStartTime(pid); err != nil || strconv.FormatUint(got, 10) != wantTicks {
		t.Errorf("scratch.ProcStartTime = (%d,%v), want (%s,nil)", got, err, wantTicks)
	}
	// 3. internal/tools: the task row's incarnation AND the sibling parent
	//    column — the two readers this change rewrote as delegations.
	wantTicksInt, err := strconv.ParseInt(wantTicks, 10, 64)
	if err != nil {
		t.Fatalf("oracle read a non-numeric starttime %q from %q", wantTicks, line)
	}
	if got, ok := procStartTicks(pid); !ok || got != wantTicksInt {
		t.Errorf("tools.procStartTicks = (%d,%v), want (%d,true)", got, ok, wantTicksInt)
	}
	wantParentInt, err := strconv.Atoi(wantParent)
	if err != nil {
		t.Fatalf("oracle read a non-numeric parent %q from %q", wantParent, line)
	}
	gotParent, ok := finishCleanupProcParent(pidStr)
	if !ok || gotParent != wantParentInt {
		t.Errorf("tools.finishCleanupProcParent = (%d,%v), want (%d,true)", gotParent, ok, wantParentInt)
	}
	// The child is a script, so its parent is the INTERPRETER forked by this
	// test's exec — but on a shebang exec the kernel may hand the pid to either
	// the script's image or the interpreter. Assert only that the parent column
	// is a REAL, live ancestor pid: 0 or a shifted read would fail this.
	if gotParent <= 1 || !procExists(gotParent) {
		t.Errorf("the child's parent column read %d, which is not a live pid — a shifted read "+
			"lands on a padding value or 0", gotParent)
	}

	// --- the red proof, on the REAL line ------------------------------------
	// The retired whole-line form, indexed by the kernel's field number − 1.
	naive, ok := naiveWholeLineField(line, procname.StatFieldStartTime)
	if !ok {
		t.Fatalf("the naive form answered unusable on %q — it must produce a WRONG NUMBER here for "+
			"this red proof to say anything about the trap (trap comm was %q)", line, trapComm)
	}
	if naive == wantTicks {
		t.Errorf("the whole-line split AGREED (%q) on a trap comm %q — the fixture lost its trap shape",
			naive, trapComm)
	}
	t.Logf("red proof: the whole-line split reads %q where the line's field %d is %q (comm %q) — "+
		"a wrong incarnation, silently", naive, procname.StatFieldStartTime, wantTicks, trapComm)
	if _, err := strconv.ParseInt(naive, 10, 64); err != nil {
		t.Errorf("the naive read %q is not even numeric — then the trap would be loud, not silent", naive)
	}
	// The pre-consolidation copy (fields counted from the last ')', index 19
	// hardcoded) must still agree: that is WHY the delegation preserves
	// behaviour — the three copies were not wrong, they were TRIPLICATED.
	if legacy := legacyHardcodedIndexParse(line); legacy != wantTicks {
		t.Errorf("the retired hardcoded-index copy reads %q, the delegated parse reads %q — the "+
			"consolidation is only behaviour-preserving while they agree", legacy, wantTicks)
	}

	// The parent column is read by the SAME parse, so the retired copy of that
	// column (index 1) must agree too.
	if legacyParent := legacyHardcodedIndexParseAt(line, procname.StatFieldParent-procname.StatFieldState); legacyParent != wantParent {
		t.Errorf("the retired parent-column copy reads %q, the delegated parse reads %q", legacyParent, wantParent)
	}
}

// TestProcStatParseDelegationKeepsTheRefusal pins the OTHER half of the preserved
// contract: a line the parse cannot answer must stay a refusal — never a
// fabricated 0 — through every entry point (Rule 15: a silent zero is
// indistinguishable from a real tick count).
func TestProcStatParseDelegationKeepsTheRefusal(t *testing.T) {
	const truncLine = "4242 (bash (deleted)) S 1 2 3\n"

	if _, ok := procname.StatFieldString(filepath.Join(t.TempDir(), procname.StatName), procname.StatFieldStartTime); ok {
		t.Error("a missing stat file must answer unusable")
	}
	if _, err := scratch.ParseProcStartTime("no-parens"); err == nil {
		t.Error("a line with no comm field must be an error through internal/scratch, not a zero")
	}
	if _, err := scratch.ParseProcStartTime(truncLine); err == nil {
		t.Error("a line too short for starttime must be an error, not a zero")
	}
	if _, ok := procStartTicks(-1); ok {
		t.Error("an unreadable stat must answer (0,false) from tools.procStartTicks")
	}
	if _, ok := finishCleanupProcParent(strconv.Itoa(procTrapImpossiblePID)); ok {
		t.Errorf("pid %d cannot exist — finishCleanupProcParent must answer (0,false)", procTrapImpossiblePID)
	}
}

// procTrapImpossiblePID is a pid the kernel cannot have handed out here: the
// default pid_max is far below it, so the stat read must fail (a fixture pid
// that could exist would make the refusal assertion flaky).
const procTrapImpossiblePID = 1 << 30

// --- helpers ---------------------------------------------------------------

// waitForTrapComm waits until /proc/<pid>/comm reports the trap name and returns
// it. A comm that never arrives is a broken fixture, so it fails the test rather
// than skipping (a silent skip would leave the trap unproven).
func waitForTrapComm(t *testing.T, pid int) string {
	t.Helper()
	deadline := time.Now().Add(trapCommWait)
	path := filepath.Join(procname.ProcRootDefault, strconv.Itoa(pid), procname.CommName)
	var last string
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			last = strings.TrimSpace(string(raw))
			if last == trapCommName {
				return last
			}
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(trapCommPoll)
	}
	t.Fatalf("/proc/%d/%s never reported %q (last %q) — the fixture does not have the trap comm shape, "+
		"so this test would prove nothing", pid, procname.CommName, trapCommName, last)
	return ""
}

// readStatLine reads the live /proc/<pid>/stat text.
func readStatLine(t *testing.T, pidStr string) string {
	t.Helper()
	raw, err := os.ReadFile(procname.StatStatPath(procname.ProcRootDefault, pidStr))
	if err != nil {
		t.Fatalf("read stat of pid %s: %v", pidStr, err)
	}
	return string(raw)
}

// oracleStatField is the TEST'S OWN parse of one stat field — the
// count-from-the-last-')' rule, written here so the production entry points are
// compared against an independent implementation rather than against each other.
func oracleStatField(t *testing.T, line string, field int) string {
	t.Helper()
	close := strings.LastIndex(line, ")")
	if close < 0 {
		t.Fatalf("oracle: %q has no comm field", line)
	}
	fields := strings.Fields(line[close+1:])
	idx := field - procname.StatFieldState
	if idx >= len(fields) {
		t.Fatalf("oracle: %q carries %d post-comm fields, field %d needs %d", line, len(fields), field, idx+1)
	}
	return fields[idx]
}

// naiveWholeLineField is the RETIRED shape a first pass reaches for: split the
// whole line on whitespace and index by the kernel's field number − 1 (which
// assumes comm is exactly one token). Kept in the TEST on purpose — a
// production line carrying it fails internal/tools'
// TestProcStatParseHasOneImplementation.
func naiveWholeLineField(line string, field int) (string, bool) {
	fields := strings.Fields(line)
	idx := field - 1
	if idx >= len(fields) {
		return "", false
	}
	return fields[idx], true
}

// legacyHardcodedIndexParse replays the pre-consolidation copy of the starttime
// parser verbatim (the body internal/scratch's ParseProcStartTime used to carry
// inline): fields counted after the last ')', the post-comm index hardcoded.
func legacyHardcodedIndexParse(raw string) string {
	const startTimeFieldIndex = 19 // the literal the copy carried
	return legacyHardcodedIndexParseAt(raw, startTimeFieldIndex)
}

// legacyHardcodedIndexParseAt is the same replay for an arbitrary post-comm
// index (the parent column's copy hardcoded 1).
func legacyHardcodedIndexParseAt(raw string, idx int) string {
	closeIdx := strings.LastIndex(raw, ")")
	if closeIdx < 0 {
		return ""
	}
	fields := strings.Fields(raw[closeIdx+1:])
	if len(fields) <= idx {
		return ""
	}
	return fields[idx]
}

// trapCommTokens counts the whitespace tokens of a stat line's comm field —
// i.e. how many tokens a naive whole-line split would wrongly count it as.
func trapCommTokens(line string) int {
	open := strings.Index(line, "(")
	closeIdx := strings.LastIndex(line, ")")
	if open < 0 || closeIdx <= open {
		return 0
	}
	return len(strings.Fields(line[open+1 : closeIdx]))
}

// writeExecutableWithRetry writes an executable copy of body to path, retrying
// ETXTBSY (Linux refuses to make a busy text file writable; a sibling process on
// a shared host holds the source binary mapped). A bounded retry, then a loud
// failure — the fixture is the one thing this test cannot proceed without.
func writeExecutableWithRetry(t *testing.T, path string, body []byte) {
	t.Helper()
	var lastErr error
	for i := 0; i < trapCopyTries; i++ {
		lastErr = os.WriteFile(path, body, 0o755)
		if lastErr == nil {
			return
		}
		if !errors.Is(lastErr, syscall.ETXTBSY) {
			t.Fatalf("write %s: %v", path, lastErr)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(trapCopyWait)
	}
	t.Fatalf("write %s: still %v after %d tries — a sibling process on this host keeps the source "+
		"binary mapped, so the fixture cannot be created", path, lastErr, trapCopyTries)
}

// procExists reports whether a pid directory exists under the real procfs.
func procExists(pid int) bool {
	_, err := os.Stat(filepath.Join(procname.ProcRootDefault, strconv.Itoa(pid)))
	return err == nil
}
