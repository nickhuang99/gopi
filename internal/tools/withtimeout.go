package tools

import (
	"context"
	"time"
)

// timeoutResult carries a generic operation's value and error through the
// buffered channel used by withTimeout.
type timeoutResult[T any] struct {
	v   T
	err error
}

// withTimeout runs op in a goroutine and returns once either op completes
// (channel result) or the deadline fires (timeoutErr). The context handed to
// op is the deadline context, so an op that honors context cancellation (the
// search tools, via exec.CommandContext) is cancelled at the deadline; ops
// that cannot take a context (read/write/edit file I/O) may ignore it and are
// left to finish harmlessly — the caller is unblocked either way.
//
// The buffered channel (size 1) plus the default send makes the send
// non-blocking, so a timed-out goroutine never leaks on the channel send.
//
// This is the single goroutine+channel+context deadline wrapper that the file
// tools (read/write/edit) and the search tools (code_search/keyword_search)
// previously each implemented by copy-paste (5 structurally identical
// readWithTimeout/writeWithTimeout/editWithTimeout/codeSearchWithTimeout/
// keywordSearchWithTimeout). The caller supplies the exact timeoutErr so each
// per-tool error message ("read: <path>: timed out after <timeout>") is
// preserved byte-for-byte.
func withTimeout[T any](timeout time.Duration, timeoutErr error, op func(ctx context.Context) (T, error)) (T, error) {
	ch := make(chan timeoutResult[T], 1)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	go func() {
		v, err := op(ctx)
		select {
		case ch <- timeoutResult[T]{v, err}:
		default:
			// Channel already consumed by the timeout path; result discarded.
		}
	}()

	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, timeoutErr
	}
}

// withTimeoutErr runs an error-only operation (write/edit) under the shared
// timeout wrapper, returning its error or timeoutErr.
func withTimeoutErr(timeout time.Duration, timeoutErr error, op func(ctx context.Context) error) error {
	_, err := withTimeout(timeout, timeoutErr, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}
