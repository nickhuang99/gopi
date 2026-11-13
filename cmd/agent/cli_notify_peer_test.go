package main

// cli_notify_peer_test.go — the CROSS-PEER hop of `gopi notify`, on the REAL
// binary and across a boundary that is NOT in-process.
//
// WHAT ONLY THIS FILE CAN SHOW. internal/tools' cases (notify_peer_test.go)
// prove the wire document, the derived id and the peer's handler; they replace
// the transport with a function call and therefore cannot show the two things
// the operator depends on: that the initiator's argv really reaches `ssh` the
// way the sync family reaches it, and that the message the peer writes is TAKEN
// by a RUNNING session on that host with no keystroke from anybody.
//
// THE BOUNDARY IS A REAL PROCESS BOUNDARY, and it is reached the real way. The
// one thing this case controls is `ssh` itself: a shim on the initiator's PATH
// runs the SAME gopi binary against a DIFFERENT profile — so the peer side is a
// genuine second process with its own $HOME, reached through argv the production
// code composed. What is NOT covered here is ssh's own ability to reach a host
// (that needs a second machine, and it is the Rule 31 acceptance); everything
// on both sides of the wire is the shipped code.
//
// THE IDLE-WAKE HALF USES THE UNMODIFIED RECEIVER. The live console is started
// with an OPEN stdin that carries NO LINE AT ALL, and the model's own request
// body is the subject: a turn can be opened without a request, but the delivered
// text cannot reach a request that no turn produced.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/tools"
)

const (
	// notifyPeerCLISession is the session id this case delivers to. It is a bare
	// chat layout (the directory IS the id), which is the shape a cross-host
	// hand-off addresses.
	notifyPeerCLISession = "01a10f78-3977-767d-aaff-640a4f5d9ac2"
	// notifyPeerCLIText is the delivered sentence: distinctive, so "the model
	// read it" cannot be satisfied by the console's own startup content.
	notifyPeerCLIText = "NOTIFY-PEER-CLI-3b71: this arrived from another host"
	// notifyPeerCLIUnknown is an id no session has.
	notifyPeerCLIUnknown = "01a10f78-0000-0000-0000-000000000000"
	// notifyPeerCLITaskID and notifyPeerCLIRunID are the TASK-RUN layout's two
	// names: the container carries the task id, and the record's file name IS
	// the run's session uuid (the value `gopi task show` prints as session_log).
	// They differ on purpose — the defect this case guards could only fire when
	// they do.
	notifyPeerCLITaskID = "01a10f79-1111-2222-3333-444444444444"
	notifyPeerCLIRunID  = "01a10f79-5555-6666-7777-888888888888"
	// notifyPeerCLIPeer is the alias the case declares and the shim answers to.
	notifyPeerCLIPeer = "hp"
	// notifyPeerCLIQuit is the ONLY line ever written on the live console's
	// stdin, and it is written AFTER the delivery has been read.
	notifyPeerCLIQuit = "/quit"
	// notifyPeerCLIWait bounds the polls below: the console must open its record,
	// and the idle poller (agent.sessionInboxIdleLoadInterval = 2s) must take the
	// message. Both are waits, never loads (Rule 27).
	notifyPeerCLIWait = 30 * time.Second
	// notifyPeerCLIStep is the poll cadence.
	notifyPeerCLIStep = 50 * time.Millisecond
	// notifyPeerCLIShimTag is what the shim records in its log, so "the
	// transport really ran, with this argv" is an observation rather than an
	// assumption.
	notifyPeerCLIShimTag = "NOTIFY-PEER-SSH-SHIM"
	// The three variables the shim reads: the peer profile it must bind, the
	// binary it must exec, and the file it records its argv in. They are named
	// once (Rule 1b) because three places spell them — the script, the
	// environment, and the reader of the log.
	notifyPeerCLIShimHomeVar = "NOTIFY_TEST_PEER_HOME"
	notifyPeerCLIShimBinVar  = "NOTIFY_TEST_BIN"
	notifyPeerCLIShimLogVar  = "NOTIFY_TEST_SHIM_LOG"
)

// writePeerSession materialises ONE bare chat session in a profile, which is
// what the peer side must find for a delivery to be accepted.
func writePeerSession(t *testing.T, home, id string) string {
	t.Helper()
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, id)
	if err := os.MkdirAll(filepath.Join(dir, "messages"), 0o755); err != nil {
		t.Fatalf("create the peer session dir: %v", err)
	}
	record := filepath.Join(dir, "session.jsonl")
	line := `{"type":"session_start","sessionId":"` + id + `"}` + "\n"
	if err := os.WriteFile(record, []byte(line), 0o644); err != nil {
		t.Fatalf("write the peer session record: %v", err)
	}
	return dir
}

// writePeerTaskRunSession materialises ONE TASK-RUN session in a profile: the
// layout a real peer has after a worker run — <sessions>/<task-id>/<run-uuid>.jsonl,
// where the run uuid is the record's file NAME (and its session_start id) and
// the task id names the CONTAINER the runs of that task share. It returns the
// container dir, which is where the session's inbox belongs.
func writePeerTaskRunSession(t *testing.T, home, taskID, runID string) string {
	t.Helper()
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, taskID)
	if err := os.MkdirAll(filepath.Join(dir, sessionlayout.SessionMessagesDirName), 0o755); err != nil {
		t.Fatalf("create the peer task-run session dir: %v", err)
	}
	record := filepath.Join(dir, runID+sessionlayout.JSONLSuffix)
	line := `{"type":"session_start","sessionId":"` + runID + `"}` + "\n"
	if err := os.WriteFile(record, []byte(line), 0o644); err != nil {
		t.Fatalf("write the peer run record: %v", err)
	}
	return dir
}

// writeSSHShim writes the ONE `ssh` the initiator will find on PATH. It parses
// the argv the way ssh does (option pairs, then the host, then the remote
// command), announces itself on stderr, and execs the SAME binary under the
// profile the case named — stdin and stdout pass straight through, which is the
// whole protocol.
func writeSSHShim(t *testing.T, bin string) string {
	t.Helper()
	shimHomeVar := "$" + notifyPeerCLIShimHomeVar
	shimBinVar := "$" + notifyPeerCLIShimBinVar
	shimLogVar := "$" + notifyPeerCLIShimLogVar
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"peer=$1; shift\n" +
		// THE ANNOUNCEMENT GOES TO A FILE, not to stderr: the initiator's
		// transport collects the peer's stderr and prints it only when the leg
		// FAILS (runSSHCommand's contract, shared with every sync leg), so a
		// successful hop would leave the observation invisible. The file is the
		// shim's own record of what it was asked to do.
		"echo \"" + notifyPeerCLIShimTag + " peer=$peer argv=$*\" >> \"" + shimLogVar + "\"\n" +
		// THE REMOTE COMMAND IS ONE ARGV ELEMENT, and the peer's LOGIN SHELL
		// re-parses it — that is ssh's own contract (syncRemoteGopi's comment in
		// memory_sync_task.go states it, and the `gopi` argv split depends on
		// it). So the shim splits it the way that shell would: an unquoted
		// expansion, never a single argument carrying spaces (which gopi would
		// read as ONE positional prompt and answer by running its AGENT LOOP —
		// an LLM session instead of a handler, which is exactly the hazard the
		// quoted form exists to avoid).
		"remote=\"$1\"; shift\n" +
		// The remote command's FIRST word is the BINARY the peer is expected to
		// run (`gopi`): on a real host the login shell resolves it on PATH, and
		// here the shim substitutes this build. The rest of the argv is handed
		// through untouched — the split into separate arguments is what gopi's
		// own parser sees on the far side.
		"set -- $remote; shift\n" +
		"GOPI_HOME=\"" + shimHomeVar + "\" exec \"" + shimBinVar + "\" \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write the ssh shim: %v", err)
	}
	return dir
}

// notifyPeerInboxLines reads the peer's inbox back as decoded records.
func notifyPeerInboxLines(t *testing.T, dir string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(tools.SessionInboxPath(dir))
	if err != nil {
		t.Fatalf("read the peer inbox at %s: %v", tools.SessionInboxPath(dir), err)
	}
	var out []map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	for {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			break
		}
		out = append(out, rec)
	}
	return out
}

// notifyPeerCountKind counts the records of one kind.
func notifyPeerCountKind(recs []map[string]any, kind string) int {
	n := 0
	for _, r := range recs {
		if r["kind"] == kind {
			n++
		}
	}
	return n
}

// TestNotifyPeerCLIIdempotentAndNamedRefusals is the hop's ANSWER half: against
// a peer-side profile whose session is NOT running, the same command twice
// appends one record and says so the second time, and an id the peer does not
// have is refused BY NAME with a non-zero status.
func TestNotifyPeerCLIIdempotentAndNamedRefusals(t *testing.T) {
	initiatorHome := t.TempDir()
	peerHome := t.TempDir()
	bin := testBinPath(t)
	shimDir := writeSSHShim(t, bin)
	peerDir := writePeerSession(t, peerHome, notifyPeerCLISession)

	env := notifyPeerInitiatorEnv(t, initiatorHome, peerHome, bin, shimDir)

	stdout, stderr, err := runNotifyDeliverArgv(t, env,
		notifyPeerFlag, notifyPeerCLIPeer, notifySessionFlag, notifyPeerCLISession, notifyMessageFlag, notifyPeerCLIText)
	if err != nil {
		t.Fatalf("cross-peer delivery failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	// THE TRANSPORT, AS THE SHIM SAW IT: the remote argv really was
	// `gopi notify --notify-remote` (the verb, the handler flag, the binary the
	// peer is expected to run) — the wire spelling the peer's dispatch must
	// know, observed rather than assumed.
	shim := shimLog(t, env)
	if !strings.Contains(shim, notifyPeerCLIShimTag+" peer="+notifyPeerCLIPeer) {
		t.Errorf("the shim did not run with the declared alias — its log:\n%s", shim)
	}
	if !strings.Contains(shim, "argv="+cliVerbNotifyRemoteArgv()) {
		t.Errorf("the shim was handed a different remote argv than %q — its log:\n%s", cliVerbNotifyRemoteArgv(), shim)
	}
	if !strings.Contains(stdout, "queued on "+notifyPeerCLIPeer) {
		t.Fatalf("the delivery does not report what the peer answered:\n%s", stdout)
	}
	recs := notifyPeerInboxLines(t, peerDir)
	if got := notifyPeerCountKind(recs, "message"); got != 1 {
		t.Fatalf("the peer inbox holds %d message record(s), want 1:\n%v", got, recs)
	}
	// THE ID IS THE SAME ON BOTH SIDES: the initiator printed it, the peer's
	// disk carries it. That is what makes the re-send below land on THIS record.
	firstID, _ := recs[0]["id"].(string)
	if firstID == "" {
		t.Fatalf("the message record carries no id:\n%v", recs)
	}
	if !strings.Contains(stdout, firstID) {
		t.Errorf("the initiator did not print the id the peer recorded (%s):\n%s", firstID, stdout)
	}
	if recs[0]["message"] != notifyPeerCLIText {
		t.Errorf("the record carries %q, want the delivered text verbatim", recs[0]["message"])
	}

	// THE RE-SEND: same command, nothing appended, and the answer SAYS so.
	stdout2, stderr2, err := runNotifyDeliverArgv(t, env,
		notifyPeerFlag, notifyPeerCLIPeer, notifySessionFlag, notifyPeerCLISession, notifyMessageFlag, notifyPeerCLIText)
	if err != nil {
		t.Fatalf("the re-send must be answered, not refused: %v\nstdout: %s\nstderr: %s", err, stdout2, stderr2)
	}
	if !strings.Contains(stdout2, "already queued on "+notifyPeerCLIPeer) {
		t.Errorf("the re-send does not report that nothing was appended:\n%s", stdout2)
	}
	if !strings.Contains(stdout2, firstID) {
		t.Errorf("the re-send names a different id than the record's (%s):\n%s", firstID, stdout2)
	}
	if recs2 := notifyPeerInboxLines(t, peerDir); len(recs2) != 1 {
		t.Fatalf("the re-send changed the peer's inbox (%d record(s), want 1):\n%v", len(recs2), recs2)
	}

	// THE UNKNOWN SESSION: refused by NAME, non-zero, and nothing written.
	stdout3, stderr3, err := runNotifyDeliverArgv(t, env,
		notifyPeerFlag, notifyPeerCLIPeer, notifySessionFlag, notifyPeerCLIUnknown, notifyMessageFlag, "hello")
	if err == nil {
		t.Fatalf("the peer accepted a delivery for a session it does not have:\nstdout: %s\nstderr: %s", stdout3, stderr3)
	}
	if !strings.Contains(stderr3, "unknown session") {
		t.Errorf("the refusal does not name the failure:\n%s", stderr3)
	}
	if strings.Contains(stdout3, "queued") {
		t.Errorf("a refused delivery printed a success line:\n%s", stdout3)
	}
	if recs3 := notifyPeerInboxLines(t, peerDir); len(recs3) != 1 {
		t.Errorf("the refused delivery wrote to the peer's inbox (%d record(s)):\n%v", len(recs3), recs3)
	}
}

// TestNotifyPeerCLIWakesTheFarSessionWithNoInput is the Rule 31 acceptance on
// the ZBOOK→HP SHAPE, with the second machine stood in for by a second profile
// and a real process boundary: a live console on the PEER side, sitting at its
// prompt with an open stdin that carries NO line at all, and one
// `gopi notify --peer hp --session <that session> --message <text>` on the
// other side of the hop. The console opens the turn by itself.
func TestNotifyPeerCLIWakesTheFarSessionWithNoInput(t *testing.T) {
	initiatorHome := t.TempDir()
	peerHome := t.TempDir()
	work := t.TempDir()
	mock := newMockLLMForNotifyIdle(t)
	defer mock.Close()
	bin := testBinPath(t)
	shimDir := writeSSHShim(t, bin)

	// THE PEER-SIDE CONSOLE, running for real: the receiver is NOT modified by
	// this feature, and this case holds it to that.
	peerEnv := mockEnvForNotifyDeliver(t, peerHome, mock.URL())
	cmd := exec.Command(bin, CliVerbChat, chatFlagTools, chatFlagNoEditor)
	cmd.Env = peerEnv
	cmd.Dir = work
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	var stdout, stderr syncBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the peer-side console: %v", err)
	}
	done := armNotifyChildExit(cmd)
	defer func() {
		_ = stdin.Close() //stylegate:ignore closes the peer-notify child's stdin to let it drain its queue
		if !done.waitFor(notifyTestTimeout) {
			_ = cmd.Process.Kill() //stylegate:ignore kills the peer-notify child after the forwarded record was read
		}
	}()

	sessionsRoot := filepath.Join(peerHome, config.ProfileDirName, config.SessionsDirName)
	record := ""
	deadline := time.Now().Add(notifyPeerCLIWait)
	for time.Now().Before(deadline) {
		record = sessionRecordUnder(t, sessionsRoot)
		if record != "" {
			break
		}
		time.Sleep(notifyPeerCLIStep) //stylegate:allow-sleep bounded poll for the console to open its own session record, deadline notifyPeerCLIWait
	}
	if record == "" {
		t.Fatalf("the peer-side console opened no session record under %s within %s\nstderr:\n%s",
			sessionsRoot, notifyPeerCLIWait, stderr.String())
	}
	peerDir := sessionlayout.RecordContainer(record)
	peerSession := filepath.Base(peerDir)
	if peerSession == notifyPeerCLISession {
		t.Fatalf("the console reused this case's static session dir — the two cases would share one inbox")
	}

	// THE DELIVERY, FROM THE OTHER SIDE OF THE HOP. Nothing is written on the
	// console's stdin, before or after: whatever it does next, it does without a
	// keystroke.
	env := notifyPeerInitiatorEnv(t, initiatorHome, peerHome, bin, shimDir)
	out, errOut, err := runNotifyDeliverArgv(t, env,
		notifyPeerFlag, notifyPeerCLIPeer, notifySessionFlag, peerSession, notifyMessageFlag, notifyPeerCLIText)
	if err != nil {
		t.Fatalf("cross-peer delivery to the live session failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if !strings.Contains(out, "queued on "+notifyPeerCLIPeer) {
		t.Fatalf("the delivery does not report the peer's answer:\n%s", out)
	}

	// THE WAKE: the model is handed the delivered text with no line on any
	// stdin having produced it. This is the receiving host's UNCHANGED wake path
	// — the idle poller, the one take point, the round boundary.
	if !waitForDescribe(t, mock, notifyPeerCLIText) {
		t.Fatalf("no request carried the delivered message within %s — the far console did not wake for it\nconsole stdout:\n%s\nconsole stderr:\n%s",
			notifyPeerCLIWait, stdout.String(), stderr.String())
	}
	if n := len(mock.RequestsContaining(notifyPeerCLIText)); n != 1 {
		t.Errorf("the delivered message appears in %d request(s), want exactly 1 — a re-minted notice would hand the model the same instruction twice", n)
	}

	// THE FILE, read back on the PEER side: the two records of the protocol,
	// under ONE id — the message as it was written, and the marker the console's
	// own take appended.
	recs := notifyPeerInboxLines(t, peerDir)
	if len(recs) != 2 {
		t.Fatalf("the peer inbox holds %d record(s), want 2 (the message and its delivered marker):\n%v", len(recs), recs)
	}
	if recs[0]["kind"] != "message" || recs[0]["message"] != notifyPeerCLIText {
		t.Errorf("the first record is not the delivered message:\n%v", recs[0])
	}
	if recs[1]["kind"] != "delivered" {
		t.Errorf("the second record is not the delivered marker:\n%v", recs[1])
	}
	if recs[0]["id"] != recs[1]["id"] {
		t.Errorf("the marker names a different id than the message (%v vs %v) — the take would not suppress a re-injection", recs[0]["id"], recs[1]["id"])
	}
	if !strings.Contains(out, recs[0]["id"].(string)) {
		t.Errorf("the initiator printed an id the peer's inbox does not carry:\n%s", out)
	}

	// AND THE SAME SEND AGAIN IS REFUSED BY NAME — the message has been TAKEN,
	// so a re-delivery would repeat an instruction the model has already acted
	// on. The inbox is unchanged by it.
	out2, errOut2, err := runNotifyDeliverArgv(t, env,
		notifyPeerFlag, notifyPeerCLIPeer, notifySessionFlag, peerSession, notifyMessageFlag, notifyPeerCLIText)
	if err == nil {
		t.Fatalf("a re-delivery of a TAKEN message was accepted:\nstdout: %s\nstderr: %s", out2, errOut2)
	}
	if !strings.Contains(errOut2, "TAKEN") {
		t.Errorf("the refusal does not say the message was already taken:\n%s", errOut2)
	}
	if recs2 := notifyPeerInboxLines(t, peerDir); len(recs2) != 2 {
		t.Errorf("the refused re-delivery changed the peer's inbox (%d record(s), want 2):\n%v", len(recs2), recs2)
	}

	if _, err := stdin.Write([]byte(notifyPeerCLIQuit + "\n")); err != nil {
		t.Fatalf("write %s: %v", notifyPeerCLIQuit, err)
	}
	if err := stdin.Close(); err != nil {
		t.Errorf("close stdin: %v", err)
	}
	if !done.waitFor(notifyTestTimeout) {
		t.Fatalf("the console did not exit after %s\nstderr:\n%s", notifyPeerCLIQuit, stderr.String())
	}
}

// TestNotifyPeerCLITaskRunRecordKeepsItsContainer is the regression guard for
// the ONE layout the bare case above cannot reach.
//
// WHAT WENT WRONG (measured 2026-10-13): the peer-side resolver peeled one
// directory level whenever the record's basename was not session.jsonl, reading
// that as "a task-run layout". A LEGACY FLAT record ("<uuid>.jsonl") and a TASK
// RUN record ("<task_id>/<uuid>.jsonl") are indistinguishable by basename, so a
// run uuid — the value `gopi task show` prints as session_log, the easiest one
// to copy — resolved to the SESSIONS ROOT and the message was appended to
// <sessions>/messages/inbox.jsonl: a directory no session reads, on the far
// host, while the handler exited 0 and the initiator printed a successful hop.
//
// THE ASSERTION IS TWO-SIDED, which is what makes it a guard rather than a
// description: the container's own inbox holds the message AND the stray
// <sessions>/messages/inbox.jsonl does NOT exist. A one-sided check would pass
// on a resolver that wrote to both.
func TestNotifyPeerCLITaskRunRecordKeepsItsContainer(t *testing.T) {
	initiatorHome := t.TempDir()
	peerHome := t.TempDir()
	bin := testBinPath(t)
	shimDir := writeSSHShim(t, bin)

	// THE PEER PROFILE: a task-run session, addressed by its RUN uuid.
	taskID := notifyPeerCLITaskID
	runID := notifyPeerCLIRunID
	peerDir := writePeerTaskRunSession(t, peerHome, taskID, runID)
	sessionsRoot := filepath.Join(peerHome, config.ProfileDirName, config.SessionsDirName)
	if got, want := peerDir, filepath.Join(sessionsRoot, taskID); got != want {
		t.Fatalf("the fixture is not the task-run layout: container %s, want %s", got, want)
	}
	stray := filepath.Join(sessionsRoot, sessionlayout.SessionMessagesDirName)

	env := notifyPeerInitiatorEnv(t, initiatorHome, peerHome, bin, shimDir)
	stdout, stderr, err := runNotifyDeliverArgv(t, env,
		notifyPeerFlag, notifyPeerCLIPeer, notifySessionFlag, runID, notifyMessageFlag, notifyPeerCLIText)
	if err != nil {
		t.Fatalf("cross-peer delivery addressed by the run uuid failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "queued on "+notifyPeerCLIPeer) {
		t.Fatalf("the delivery does not report the peer's answer:\n%s", stdout)
	}
	// THE RECEIPT NAMES THE FILE THE PEER ACTUALLY WROTE — the initiator's own
	// report of where a message addressed by a run uuid lands.
	if !strings.Contains(stdout, tools.SessionInboxPath(peerDir)) {
		t.Errorf("the receipt does not name the container's inbox (%s):\n%s", tools.SessionInboxPath(peerDir), stdout)
	}

	// SIDE ONE: the message is in the CONTAINER's inbox, under the run uuid.
	recs := notifyPeerInboxLines(t, peerDir)
	if got := notifyPeerCountKind(recs, "message"); got != 1 {
		t.Fatalf("the container inbox holds %d message record(s), want 1 — the record's own directory is the container, and it was peeled away:\n%v", got, recs)
	}
	if recs[0]["message"] != notifyPeerCLIText {
		t.Errorf("the record carries %q, want the delivered text verbatim", recs[0]["message"])
	}

	// SIDE TWO: the stray directory the defect created does not exist — neither
	// the file nor the directory that holds it (an empty dir left behind by a
	// refusal would still be a claim about where messages go).
	if _, statErr := os.Stat(stray); !os.IsNotExist(statErr) {
		t.Errorf("the peer profile carries a stray %s (stat: %v) — a run uuid was peeled past its container", stray, statErr)
	}
}

// cliVerbNotifyRemoteArgv is the remote command this feature must put on the
// wire, spelled from the SAME constants the production code composes it from
// (the binary name the initiator invokes, the verb, and the handler flag) — a
// second literal here would let the test agree with a wire nobody sends.
func cliVerbNotifyRemoteArgv() string {
	return cliverbs.SyncRemoteBinaryName + " " + CliVerbNotify + " --" + tools.NotifyPeerRemoteFlagName
}

// shimLog reads the shim's own record of the argv it was handed.
func shimLog(t *testing.T, env []string) string {
	t.Helper()
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, notifyPeerCLIShimLogVar+"="); ok {
			path = v
		}
	}
	if path == "" {
		t.Fatal("the initiator environment carries no shim log path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the shim log at %s: %v", path, err)
	}
	return string(raw)
}

// notifyPeerInitiatorEnv is the initiator's environment: the shared mocked-run
// environment (isolated profile, no ambient GOPI_*) with the THREE things this
// hop needs — the declared peer, the shim FIRST on PATH, and the shim's own
// binding of the peer profile and binary.
func notifyPeerInitiatorEnv(t *testing.T, initiatorHome, peerHome, bin, shimDir string) []string {
	t.Helper()
	env := mockEnvForNotifyDeliver(t, initiatorHome, "")
	logPath := filepath.Join(t.TempDir(), "ssh-shim.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatalf("create the shim log: %v", err)
	}
	return append(env,
		config.MemoryPeersEnv+"="+notifyPeerCLIPeer,
		"PATH="+shimDir+":"+os.Getenv("PATH"),
		notifyPeerCLIShimHomeVar+"="+peerHome,
		notifyPeerCLIShimBinVar+"="+bin,
		notifyPeerCLIShimLogVar+"="+logPath,
	)
}
