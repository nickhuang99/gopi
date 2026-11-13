package testsuite

// chat_layout_stream_pty_test.go — WHERE THE CHAT LAYOUT'S BYTES MAY GO, measured
// on a REAL pty with the REAL binary (cmd/agent/chat_stream_split.go is the rule;
// this file is its regression guard).
//
// THE DEFECT THIS PINS (this task's epic). The beat-driven replay launcher runs
// the product inside a private pty and redirects the child's stderr to a log —
// deliberately, so that a diagnostic line can never be drawn onto the screen the
// frames photograph — while it photographs what the pty carries. The surface
// painted its ENTIRE layout (banner, turn headers, answers, the dump notice) on
// fd 2, so the pty carried only the two startup lines and every frame was the
// same blank window: 16 beats, 16 PNGs, ONE distinct md5, with the layout itself
// sitting in `<scratch>/replay-stderr.log`.
//
// WHY A PTY, AND WHY stderr IS A FILE HERE. `lineedit.IsTerminal` asks the KERNEL
// (termios), so a fixture with a bytes.Buffer would be measuring the test's own
// fiction. And the case must reproduce the launcher's SHAPE — two descriptors,
// two destinations — because that split is the only thing the defect needed:
// with stdout and stderr the same terminal (every other pty case here) the
// pre-fix surface looked fine.
//
// WHAT IS ASSERTED. The layout marker must arrive on the pty (the descriptor a
// photographer reads), and it must NOT appear in the stderr log (the descriptor
// a launcher must be free to file). The second half is the half that keeps the
// convention from being satisfied by writing to BOTH streams.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// chatLayoutRedirectTimeout bounds the wait for the pty to carry the layout.
	chatLayoutRedirectTimeout = 30 * time.Second
	// chatLayoutRedirectExitWait bounds the wait for the child to leave.
	chatLayoutRedirectExitWait = 20 * time.Second
	// chatLayoutRedirectBuf is one read from the pty master.
	chatLayoutRedirectBuf = 4096
	// chatLayoutRedirectPoll separates two reads of the pty master.
	chatLayoutRedirectPoll = 25 * time.Millisecond
	// chatLayoutMark is the marker the case waits for on the pty. It is the
	// STARTUP SCREEN's second line — "/quit to exit." — which only a surface
	// that laid its layout out writes. A literal is used because the layout's
	// prose lives in package main; the cmd/agent-side tests assert the real
	// chatLayoutMark, and this case's question is which DESCRIPTOR carried it.
	chatLayoutMark = "/quit to exit."
)

// startPTYChatStderrFile is startPTYChat's second shape: the child's stdin and
// stdout are the pty (so the raw-mode editor runs and the surface draws on a
// terminal), while its stderr is a FILE — the shape a film launcher produces
// (stderr redirected to a log while the surface draws on the pty), and the only
// difference from startPTYChat that this case needs.
func startPTYChatStderrFile(t *testing.T, args []string, env []string, dir string) (p *ptySession, logPath string) {
	t.Helper()
	master, slavePath := openPty(t)
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		_ = master.Close() //stylegate:ignore closing the master on the skip path
		t.Skip("cannot open the pty slave device: this host exposes no usable pty pair")
	}
	logPath = filepath.Join(t.TempDir(), "chat-stderr.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		_ = slave.Close()  //stylegate:ignore closing both ends on the fatal path
		_ = master.Close() //stylegate:ignore closing both ends on the fatal path
		t.Fatalf("create the diagnostic log: %v", err)
	}
	cmd := exec.Command(testutil.GopiBin(t), args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()  //stylegate:ignore closing both ends on the fatal path
		_ = master.Close() //stylegate:ignore closing both ends on the fatal path
		t.Fatalf("start gopi chat on a pty with a redirected stderr: %v", err)
	}
	_ = slave.Close() //stylegate:ignore the child holds its own descriptor for the slave
	t.Cleanup(func() {
		_ = logFile.Close() //stylegate:ignore the harness's own teardown
	})
	// The ONE constructor, so this shape gets the same teardown as every other
	// pty case: kill THIS child, close THIS master, wait for the drain to return.
	p = newPTYSession(t, cmd, master)
	return p, logPath
}

// drainPane reads the pty master until want appears or the bound passes,
// returning what the descriptor carried. It does not Fatal on the deadline: the
// caller's assertion is the assertion, and the collected output is the evidence
// a failure prints.
func drainPane(t *testing.T, p *ptySession, want string, timeout time.Duration) string {
	t.Helper()
	var got strings.Builder
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-p.rx:
			if !ok {
				return got.String()
			}
			got.WriteByte(b)
			if strings.Contains(got.String(), want) {
				return got.String()
			}
		case <-timer.C:
			return got.String()
		}
	}
}

// TestChatLayoutStream_SurvivesADiagnosticRedirect is the regression guard for
// the convention: with the child's stderr filed away, the layout still reaches
// the pty, and the log does not carry it.
func TestChatLayoutStream_SurvivesADiagnosticRedirect(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "answered."})
	defer llm.Close()

	chat, logPath := startPTYChatStderrFile(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)

	stream := drainPane(t, chat, chatLayoutMark, chatLayoutRedirectTimeout)
	if !strings.Contains(stream, chatLayoutMark) {
		t.Fatalf("the pty never carried the layout's banner marker (%q) within %s.\nWhat the pty DID carry:\n%s\nThe layout belongs to the LAYOUT STREAM (cmd/agent/chat_stream_split.go): a run whose stdout is a terminal paints there, so a launcher that files fd 2 to keep diagnostics off the screen still films the surface. This is the defect the replay launcher met — 16 beats, 16 identical blank PNGs.",
			chatLayoutMark, chatLayoutRedirectTimeout, stream)
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 (the layout change must not disturb the normal exit path)", code)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read the diagnostic log: %v", err)
	}
	if strings.Contains(string(data), chatLayoutMark) {
		t.Errorf("the DIAGNOSTIC stream carries the layout's banner marker (%q).\nThe two streams are separate by construction (cmd/agent/chat_stream_split.go): the banner is painted on the layout stream, and the diagnostic stream carries what a reader must be TOLD. A launcher that files fd 2 to keep noise off the screen would otherwise file the screen itself.\nThe log held:\n%s",
			chatLayoutMark, string(data))
	}
}
