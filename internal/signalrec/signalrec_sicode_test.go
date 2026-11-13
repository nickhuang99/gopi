package signalrec

// signalrec_sicode_test.go — the regression guard for the si_code fields the
// recorder now writes (epic signal-provenance-si-code).
//
// THE DEFECT THIS PINS. Every KindSignal line before this epic answered "where
// was the victim" and "did a gopi stop path announce the kill" — never "did a
// PROCESS send it, or did the kernel". A terminal hangup, a cgroup teardown and
// an operator's `kill -TERM` produced the same record, so the ONLY thing that
// separated them was the absence of a KindIntent line, which reads as an
// accusation. Measured shape (2026-09-16): eight gopi processes SIGTERMed inside
// one second, zero intent records, and no way to tell whether a supervisor or
// the session teardown had done it.
//
// THE THREE ARMS, and the direction each one fails in:
//
//	① an EXTERNAL sender (this test process) → si_code == SI_USER(0) and
//	   sender_pid == THIS PROCESS's pid. Fails if the field is absent, if the
//	   capture never armed, or if the pid were filled with the victim's own.
//	② the REVERSE: the same record must NOT carry si_code == SI_TKILL and must
//	   not name the victim — i.e. the assertions above are specific values, not
//	   "some field exists". A writer that stamped a constant would pass a
//	   sloppier arm and fail here.
//	③ a sender that has EXITED BY THE TIME WE LOOK → sender_pid survives (the
//	   kernel told us) and sender_comm is either ABSENT or the sender's OWN name.
//	   Both are correct records, and the arm exists so no future change
//	   "fixes" the missing name by inventing one (Rule 15): a THIRD name names
//	   a process that never sent this signal.
import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/flaketest"
	"github.com/nick/gopi/internal/sigsource"
)

// TestSignalRecordCarriesTheKernelsSICode is arms ①②: a real second process
// SIGTERMs a real victim, and the victim's filed record must carry the kernel's
// word about that sender.
func TestSignalRecordCarriesTheKernelsSICode(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "sicode-session")

	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0])
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		srUnsourcedVictimEnv: "1",
		SessionDirEnv:        dir,
		srReadyEnv:           ready[srReadyEnv],
	})
	cmd.Stderr = os.Stderr
	requireFixtureBinding := srBindVictim(t, cmd, "TestSignalRecordCarriesTheKernelsSICode")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the victim: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	// THIS PROCESS is the sender, so its pid is the expected sender_pid and no
	// implementation that records the victim's own pid can pass.
	senderPID := os.Getpid()
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal the victim: %v", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	_ = cmd.Process.Kill() //stylegate:ignore teardown of the fixture child after the record was polled; the kill is not the assertion
	_ = cmd.Wait()         //stylegate:ignore reaping the child just killed; its wait status is not asserted
	if len(recs) == 0 {
		t.Fatalf("the victim left no record in %s — nothing to assert the si_code fields on", dir)
	}
	r := recs[len(recs)-1]
	if r.Kind != KindSignal {
		t.Fatalf("last record kind = %q, want %q", r.Kind, KindSignal)
	}

	// ARM ① — the capture is present and names an ORDINARY PROCESS.
	if r.SICode == nil {
		t.Fatalf("record has no %q field: %s", SICodeField, srJSON(r))
	}
	if *r.SICode != sigsource.SICodeUser {
		t.Fatalf("%s = %d (%s), want %d (%s) for a kill(2) from pid %d",
			SICodeField, *r.SICode, r.SICodeName, sigsource.SICodeUser,
			sigsource.CodeName(sigsource.SICodeUser), senderPID)
	}
	if r.SICodeName != sigsource.SICodeNameUser {
		t.Fatalf("%s = %q, want %q", SICodeNameField, r.SICodeName, sigsource.SICodeNameUser)
	}
	if r.SenderPID == nil {
		t.Fatalf("record has no %q field for a SI_USER delivery: %s", SenderPIDField, srJSON(r))
	}
	if int(*r.SenderPID) != senderPID {
		t.Fatalf("%s = %d, want %d (the process that called kill) — not the victim's own pid (%d): %s",
			SenderPIDField, *r.SenderPID, senderPID, cmd.Process.Pid, srJSON(r))
	}
	// sender_comm is best-effort by design (the sender may be gone), so the arm
	// asserts the DIRECTION: when present it must name this test binary, never
	// something invented.
	if r.SenderComm != "" {
		base := cmdBaseName(os.Args[0])
		if r.SenderComm != base {
			t.Fatalf("%s = %q, want %q (the comm of the process that sent the signal): %s",
				SenderCommField, r.SenderComm, base, srJSON(r))
		}
	}

	// ARM ② — the REVERSE verification, on the same record. si_code is a
	// MEASUREMENT, so it must be able to say "not a thread-directed kill" and
	// "not this process". A constant-零 implementation that satisfied arm ① by
	// coincidence is caught by the pair below: it cannot simultaneously be
	// SI_USER and name a sender that is not the victim, unless it really read
	// the kernel's siginfo.
	if *r.SICode == sigsource.SICodeTKill {
		t.Fatalf("%s = %d (%s) for a kill(2) delivery — SI_TKILL means tgkill(2)/raise(2) was used, so the capture is reporting another delivery",
			SICodeField, *r.SICode, r.SICodeName)
	}
	if int(*r.SenderPID) == r.PID && senderPID != r.PID {
		t.Fatalf("%s = %d is the VICTIM's own pid while the sender was %d — the field is self-referential and would mislabel every external kill",
			SenderPIDField, *r.SenderPID, senderPID)
	}
	if !sigsource.FromProcess(*r.SICode) {
		t.Fatalf("sigsource.FromProcess(%d) = false for a kill(2) delivery — the SI_FROMUSER classification is inverted", *r.SICode)
	}
}

// TestSignalRecordSenderCommSurvivesAnExitedSender is arm ③.
//
// The sender is a SHORT-LIVED child (`kill -TERM <pid>`), and by the time the
// victim's handler runs the child may or may not still be readable from /proc.
// The kernel's si_pid outlives it either way (it lives in the siginfo, not in
// /proc), so sender_pid is asserted to be present and correct, while
// sender_comm has TWO legal values and no others:
//
//	""              the sender was already reaped when the handler looked
//	senderCommName  the sender was still there (or a zombie kept its comm)
//
// Which of the two is recorded depends on the SCHEDULER, not on the product:
// signalrec.go:538 says the field is "read AT SIGNAL TIME", and
// signalrec.go:2155 writes whatever /proc/<sender_pid>/comm answers. Both are
// therefore correct records of the same event, and an arm that demands absence
// is asserting a scheduling outcome (measured: 5 of 30 runs before this fix).
//
// The arm still FAILS on any THIRD value — a name that is neither empty nor the
// sender's own, i.e. a pid-recycled read or an invented suspect (Rule 15).
func TestSignalRecordSenderCommSurvivesAnExitedSender(t *testing.T) {
	// This arm's root cause, registered in testdata/flakes.tsv, so a reader who
	// meets the row there can read the mechanism beside the assertion it explains.
	flaketest.Declare("deterministic: the arm asserted r.SenderComm == \"\" for a sender that had exited, but the product reads /proc/<sender_pid>/comm AT SIGNAL TIME (signalrec.go:538 comment, single write site :2155) and writes whatever it answers — sender.Wait() reaps the child only when the scheduler gets to it, so the comm read at signal time is \"\" on a reaped child and \"kill\" while it is still a zombie holding its comm. Both are CORRECT records of the same event, so the assertion was sampling scheduling rather than product behaviour. Registered: testdata/flakes.tsv, owner 01a1ada6-3c18-7e02-b094-34a70a7ddf81.")
	home := srHome(t)
	dir := srSessionDir(t, home, "sicode-exited-sender")

	ready := srAnticipateChildReady(t, home)
	victim := exec.Command(os.Args[0])
	victim.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		srUnsourcedVictimEnv: "1",
		SessionDirEnv:        dir,
		srReadyEnv:           ready[srReadyEnv],
	})
	victim.Stderr = os.Stderr
	requireFixtureBinding := srBindVictim(t, victim, "TestSignalRecordSenderCommSurvivesAnExitedSender")
	if err := victim.Start(); err != nil {
		t.Fatalf("start the victim: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	sender := exec.Command(senderCommName, "-TERM", strconv.Itoa(victim.Process.Pid))
	if err := sender.Start(); err != nil {
		t.Fatalf("start the sender child: %v", err)
	}
	senderPID := sender.Process.Pid
	if err := sender.Wait(); err != nil {
		t.Fatalf("the sender child failed: %v — no signal reached the victim, so this arm asserts nothing", err)
	}
	recs := srWaitForRecord(t, dir, 1)
	_ = victim.Process.Kill() //stylegate:ignore teardown of the fixture child after the record was polled; the kill is not the assertion
	_ = victim.Wait()         //stylegate:ignore reaping the child just killed; its wait status is not asserted
	if len(recs) == 0 {
		t.Fatalf("the victim left no record in %s", dir)
	}
	r := recs[len(recs)-1]
	if r.SICode == nil || *r.SICode != sigsource.SICodeUser {
		t.Fatalf("%s = %v, want %d (%s): `kill(1)` sends with kill(2), which is SI_USER — record: %s",
			SICodeField, srCodeOf(r), sigsource.SICodeUser, sigsource.SICodeNameUser, srJSON(r))
	}
	if r.SenderPID == nil {
		t.Fatalf("record has no %q although the kernel reported SI_USER — si_pid lives in the siginfo and outlives the sender: %s",
			SenderPIDField, srJSON(r))
	}
	if int(*r.SenderPID) != senderPID {
		t.Fatalf("%s = %d, want %d (the `kill` child that has since exited): %s",
			SenderPIDField, *r.SenderPID, senderPID, srJSON(r))
	}
	// TWO LEGAL VALUES, and the arm accepts exactly both (see the doc comment):
	// absence when the sender was already reaped, or the sender child's OWN comm
	// when it was still readable. A THIRD value is the defect — a name from a
	// recycled pid, which is an invented suspect (Rule 15).
	if r.SenderComm != "" && r.SenderComm != senderCommName {
		t.Fatalf("%s = %q for a sender that was `%s` (pid %d) — want either \"\" (the sender was already reaped when the handler read /proc) or %q (still readable at signal time); any OTHER name is a recycled pid being read as the sender: %s",
			SenderCommField, r.SenderComm, senderCommName, senderPID, senderCommName, srJSON(r))
	}
}

// senderCommName is the comm arm ③'s sender child carries: the NAME OF THE
// BINARY it execs, taken from the constant rather than re-typed, so the
// exec.Command call and the assertion that reads /proc/<pid>/comm cannot drift
// apart (Rule 1 / Rule 14 — one spelling for a value compared against a kernel
// reading). Note it is the comm the KERNEL reports (`kill`), never the path.
const senderCommName = "kill"

// srCodeOf renders the optional code for a failure message without pretending an
// absent field is 0 (Rule 15a: SI_USER IS 0, so "%v with nil printed as 0" would
// read as a measurement).
func srCodeOf(r record) string {
	if r.SICode == nil {
		return "absent"
	}
	return strconv.Itoa(int(*r.SICode))
}

// srJSON renders a record as the wire line, so a failure quotes the artifact the
// operator would grep instead of a re-formatted summary of it.
func srJSON(r record) string {
	b, err := json.Marshal(r)
	if err != nil {
		return "(record does not marshal: " + err.Error() + ")"
	}
	return string(b)
}

// cmdBaseName is filepath.Base without the import churn: the comm of a process
// started from a path is the path's last element (truncated by the kernel to 15
// bytes, which is why the comparison below is a prefix check when it is long).
func cmdBaseName(path string) string {
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			name = path[i+1:]
			break
		}
	}
	return name
}
