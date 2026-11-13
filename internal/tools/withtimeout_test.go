package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// These tests lock the withTimeout[T] generic wrapper that Phase-2 extracted
// from the five structurally-identical per-tool goroutine+channel+context
// timeouts (read/write/edit/code_search/keyword_search). The per-tool FIFO
// tests pin the integration; these pin the helper's own contract: a fast op
// returns its result, a deadline-crossing op returns the supplied timeoutErr,
// and the op's context is the deadline context (so ctx-aware impls cancel).

// TestWithTimeout_NormalPath locks that a fast op delivers its value/error
// through the channel (not the timeout branch).
func TestWithTimeout_NormalPath(t *testing.T) {
	v, err := withTimeout(5*time.Second, nil, func(context.Context) (string, error) {
		return "hello", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "hello" {
		t.Errorf("v = %q, want hello", v)
	}
}

// TestWithTimeout_NormalPathError locks that a fast op's error is returned as-is
// (not swallowed).
func TestWithTimeout_NormalPathError(t *testing.T) {
	v, err := withTimeout(5*time.Second, nil, func(context.Context) (string, error) {
		return "", customTimeoutErr("boom")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want boom", err)
	}
	if v != "" {
		t.Errorf("v = %q, want empty", v)
	}
}

// TestWithTimeout_TimeoutBranch locks that an op which outlives the tiny
// deadline returns the caller-supplied timeoutErr (never the op's eventual
// result) — this is the contract the file/search FIFO tests rely on.
func TestWithTimeout_TimeoutBranch(t *testing.T) {
	timeoutErr := customTimeoutErr("typed-out")
	v, err := withTimeout(10*time.Millisecond, timeoutErr, func(ctx context.Context) (string, error) {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			// op honors ctx cancellation — it would return promptly here.
		}
		return "late", nil
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err.Error() != "typed-out" {
		t.Errorf("err = %q, want the supplied timeoutErr", err)
	}
	if v != "" {
		t.Errorf("v = %q, want zero value on timeout", v)
	}
}

// TestWithTimeout_ContextCancelledByDeadline locks that the op receives the
// deadline context — context-aware ops (search's exec.CommandContext) must
// observe cancellation at the deadline rather than running to completion.
func TestWithTimeout_ContextCancelledByDeadline(t *testing.T) {
	opCtxObserved := make(chan context.Context, 1)
	_, _ = withTimeout(10*time.Millisecond, customTimeoutErr("t"), func(ctx context.Context) (struct{}, error) {
		opCtxObserved <- ctx
		<-ctx.Done() // return when the deadline fires
		return struct{}{}, nil
	}) //stylegate:ignore runs a fixture closure under a timeout; the returned sentinel is the assertion
	ctx := <-opCtxObserved
	select {
	case <-ctx.Done():
		// deadline context fired — as expected
	case <-time.After(2 * time.Second):
		t.Fatal("op context never cancelled by deadline")
	}
}

// TestWithTimeoutErr_NormalPath locks the error-only variant (write/edit).
func TestWithTimeoutErr_NormalPath(t *testing.T) {
	err := withTimeoutErr(5*time.Second, nil, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = withTimeoutErr(5*time.Second, nil, func(context.Context) error {
		return customTimeoutErr("op-failed")
	})
	if err == nil || err.Error() != "op-failed" {
		t.Errorf("err = %v, want op-failed", err)
	}
}

// customTimeoutErr builds a sentinel error for asserting exact timeoutErr
// passthrough.
type sentinelErr string

func (s sentinelErr) Error() string { return string(s) }

func customTimeoutErr(msg string) error { return sentinelErr(msg) }
