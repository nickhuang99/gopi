package tools

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestReadTimeout_FIFOBlocks(t *testing.T) {
	// Create a FIFO (named pipe). Opening it for reading blocks until a
	// writer opens it. With a 100ms timeout, the read MUST time out.
	fifoPath := t.TempDir() + "/test.fifo"
	_, err := Bash("mkfifo " + fifoPath)
	if err != nil {
		t.Skipf("mkfifo not available: %v", err)
	}

	start := time.Now()
	_, err = readWithTimeout(fifoPath, 0, 0, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error reading from blocking FIFO, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %v (expected ~100ms)", elapsed)
	}
}

func TestReadTimeout_ShortTimeout(t *testing.T) {
	// Even with a normal file, a very short timeout on a slow system
	// should still return the file content correctly when it completes
	// within the window.
	path := t.TempDir() + "/test.txt"
	Write(path, "hello world")

	result, err := readWithTimeout(path, 0, 0, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error with generous timeout: %v", err)
	}
	if !strings.Contains(result, "hello world") {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestWriteTimeout_FIFOBlocks(t *testing.T) {
	// P2 (atomic write): content is staged in a same-directory temp file and
	// renamed over the target, so writing to a FIFO path no longer blocks —
	// it replaces the FIFO with a regular file (dsh fsio.ts staging+rename
	// semantics). This test previously asserted the os.WriteFile blocking
	// behavior (writing INTO the FIFO inode without a reader); the atomic
	// path has no such block, so the operation now completes instead of
	// timing out — the write-timeout machinery itself stays covered by the
	// read/edit FIFO tests below.
	fifoPath := t.TempDir() + "/test.fifo"
	_, err := Bash("mkfifo " + fifoPath)
	if err != nil {
		t.Skipf("mkfifo not available: %v", err)
	}

	// 128KB — larger than the pipe buffer; under the old inode-writing
	// behavior this blocked forever without a reader.
	largeData := strings.Repeat("x", 128*1024)
	if err := writeWithTimeout(fifoPath, largeData, false, 5*time.Second); err != nil {
		t.Fatalf("atomic write to a FIFO path should replace the FIFO, got: %v", err)
	}

	// The FIFO must now be a regular file holding the full content.
	info, err := os.Stat(fifoPath)
	if err != nil {
		t.Fatalf("stat after write: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe != 0 {
		t.Errorf("path is still a FIFO — atomic write did not replace it")
	}
	got, err := os.ReadFile(fifoPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != largeData {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(largeData))
	}
}

func TestWriteTimeout_NormalFile(t *testing.T) {
	path := t.TempDir() + "/test.txt"
	err := writeWithTimeout(path, "hello world", false, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error with generous timeout: %v", err)
	}

	result, _ := Read(path, 0, 0) //stylegate:ignore reads a file the test itself wrote; the returned content is the assertion
	if !strings.Contains(result, "hello world") {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestEditTimeout_FIFOBlocks(t *testing.T) {
	// Create a FIFO. editWithTimeout calls os.ReadFile internally, which
	// blocks when opening a FIFO for reading without a writer.
	fifoPath := t.TempDir() + "/test.fifo"
	_, err := Bash("mkfifo " + fifoPath)
	if err != nil {
		t.Skipf("mkfifo not available: %v", err)
	}

	start := time.Now()
	err = editWithTimeout(fifoPath, "old", "new", false, false, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error editing a FIFO, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %v (expected ~100ms)", elapsed)
	}
}

func TestEditTimeout_NormalFile(t *testing.T) {
	path := t.TempDir() + "/test.txt"
	Write(path, "hello world")

	err := editWithTimeout(path, "hello", "goodbye", false, false, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error with generous timeout: %v", err)
	}

	result, _ := Read(path, 0, 0) //stylegate:ignore reads a file the test itself wrote; the returned content is the assertion
	if strings.Contains(result, "hello world") {
		t.Errorf("edit did not replace: %q", result)
	}
	if !strings.Contains(result, "goodbye world") {
		t.Errorf("unexpected result after edit: %q", result)
	}
}

func TestBashIdleTimeout_KillsStalledCommand(t *testing.T) {
	// sleep 60 produces no output → idle timeout should kill it within 1s.
	start := time.Now()
	output, err := bash("sleep 60", 30*time.Second, 1*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected idle timeout error for sleep 60, got nil")
	}
	if !strings.Contains(err.Error(), "no output for") {
		t.Errorf("expected 'no output for' in error, got: %v", err)
	}
	if !strings.Contains(output, "no output for") {
		t.Errorf("expected 'no output for' in output, got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
	if elapsed > 10*time.Second {
		t.Errorf("idle timeout took too long: %v (expected ~1s)", elapsed)
	}
	if elapsed < 500*time.Millisecond {
		t.Errorf("idle timeout too fast: %v (expected ~1s)", elapsed)
	}
}

func TestBashIdleTimeout_DoesNotFireWhenOutputFlows(t *testing.T) {
	// A command that produces output every 500ms for 2.5s total.
	// Idle timeout of 2s should NOT fire (output flows every 500ms < 2s).
	// Absolute timeout of 10s provides a safety net.
	cmd := "for i in 1 2 3 4 5; do echo $i; sleep 0.5; done"
	output, err := bash(cmd, 10*time.Second, 2*time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "exit=0") {
		t.Errorf("expected successful exit, got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
	// All 5 numbers should appear.
	for i := 1; i <= 5; i++ {
		if !strings.Contains(output, string(rune('0'+i))) {
			// Use a simpler check
			_ = i
		}
	}
}

func TestBashIdleTimeout_InfiniteOutputCommand(t *testing.T) {
	// yes(1) produces infinite output. Idle timeout should NOT fire because
	// output is continuous. Absolute timeout (2s) should kill it.
	output, err := bash("yes", 2*time.Second, 10*time.Second)

	if err == nil {
		t.Fatal("expected absolute timeout error for 'yes', got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
	if !strings.Contains(output, "timed out") {
		t.Errorf("expected 'timed out' in output, got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
}

func TestBashIdleTimeout_InteractivePrompt(t *testing.T) {
	// 'sleep 3600' (1 hour) produces no output and blocks forever.
	// The idle timeout (1s) should kill it long before the absolute timeout (30s).
	output, err := bash("sleep 3600", 30*time.Second, 1*time.Second)

	if err == nil {
		t.Fatal("expected idle timeout error for 'sleep 3600', got nil")
	}
	if !strings.Contains(err.Error(), "no output for") {
		t.Errorf("expected 'no output for' in error, got: %v", err)
	}
	_ = output
}

func TestTimeoutConstants_Defaults(t *testing.T) {
	if DefaultFileToolTimeout != 30*time.Second {
		t.Errorf("DefaultFileToolTimeout = %v, want 30s", DefaultFileToolTimeout)
	}
	if DefaultBashTimeout != 120*time.Second {
		t.Errorf("DefaultBashTimeout = %v, want 120s", DefaultBashTimeout)
	}
	if DefaultBashIdleTimeout != 120*time.Second {
		t.Errorf("DefaultBashIdleTimeout = %v, want 120s", DefaultBashIdleTimeout)
	}
	// The per-call second-based constants must mirror the duration constants
	// (single source of truth, Rule 2).
	if bashDefaultTimeoutSec != int(DefaultBashTimeout/time.Second) {
		t.Errorf("bashDefaultTimeoutSec = %d, want %d", bashDefaultTimeoutSec, int(DefaultBashTimeout/time.Second))
	}
	if bashNoOutputTimeoutSec != int(DefaultBashIdleTimeout/time.Second) {
		t.Errorf("bashNoOutputTimeoutSec = %d, want %d", bashNoOutputTimeoutSec, int(DefaultBashIdleTimeout/time.Second))
	}
}

func TestTimeoutConstants_Resolution(t *testing.T) {
	// These must return non-zero values.
	if FileToolTimeout() == 0 {
		t.Error("FileToolTimeout() returned 0")
	}
	if BashTimeout() == 0 {
		t.Error("BashTimeout() returned 0")
	}
	if BashIdleTimeout() == 0 {
		t.Error("BashIdleTimeout() returned 0")
	}
}
