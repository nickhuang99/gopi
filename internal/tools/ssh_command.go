package tools

import (
	"context"
	"os/exec"
	"time"
)

// contextWithTimeout isolates context.WithTimeout for the claim broadcast's
// ssh leg (the timeout constant lives at the top of memory_sync_claim.go).
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// newSSHCommand builds the BatchMode ssh command for the claim leg — the
// argv shape mirrors syncSSHExchange (one remote-command element; the
// quoted form is load-bearing, see memory_sync_task.go's syncRemoteGopi
// comment).
func newSSHCommand(ctx context.Context, peer, remoteCmd string) *exec.Cmd {
	return exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+syncSSHConnectTimeout,
		peer,
		remoteCmd)
}

// runSSHCommand runs a prepared ssh command and returns stdout/stderr
// separately (the claim handler's response document rides stdout; human
// noise rides stderr).
func runSSHCommand(cmd *exec.Cmd) ([]byte, []byte, error) {
	var stdout, stderr syncBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.b, stderr.b, err
}

// syncBuffer is a minimal io.Writer over a byte slice (avoid importing
// bytes just for two buffers in the ssh helpers).
type syncBuffer struct{ b []byte }

func (w *syncBuffer) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
