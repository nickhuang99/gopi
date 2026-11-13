package tools

// boundedTailWriter accumulates a byte stream while retaining only a BOUNDED
// window of it: the last tailLimit bytes.
//
// WHY IT EXISTS (the measured defect, 2026-10-13, task
// 01a1315950dd7c51b5a546b78494cf88). bashInDirCtx read a command's stdout and
// stderr into two bytes.Buffers that grew with everything the command emitted.
// `pipeReadBufferSize` bounded the READ (4 KiB at a time) and nothing bounded
// the BUFFER, so a command producing output faster than the run could be
// stopped — `yes`, under TestBashIdleTimeout_InfiniteOutputCommand's 2 s budget
// — drove the buffer into a multi-GiB growslice and the runtime aborted the
// whole test BINARY:
//
//	runtime: out of memory: cannot allocate 2147483648-byte block (…)
//	fatal error: out of memory
//	bytes.(*Buffer).Write  bash.go:602
//
// A binary-level abort carries no failing test with it, so the budget harness
// could only render the contradiction "failing tests: 0 / PACKAGE FAILED" — the
// package's verdict died with its evidence.
//
// WHY "THE TAIL" AND NOT "THE FIRST N BYTES": the retention window is the one
// buildOutput KEEPS. Its size is maxOutputSize (the hard cap on what a bash
// call returns), and its truncation keeps the tail, because CLI errors — build
// failures, test failures, git conflicts — land at the END of a command's
// output. Retaining the head instead would retain bytes the renderer is about
// to throw away.
//
// WHY THIS IS NOT A SILENT LOSS (Rule 15): the bytes dropped here are exactly
// the bytes buildOutput would have dropped anyway, and the result the caller
// sees carries buildOutput's own explicit "[output truncated at N bytes …]"
// marker whenever anything was truncated. Bounded retention changes WHAT IS
// HELD IN MEMORY, not what is returned.
//
// It is a WRITER, so it satisfies io.Writer: the read goroutines hand it the
// pipe's chunks exactly as they handed them to bytes.Buffer, and the two call
// sites cannot drift into two different policies (Rule 14).
type boundedTailWriter struct {
	// tail holds the retained window. It is allocated with capacity tailLimit
	// and NEVER grows past it: the drop path below shifts the window with
	// copy() rather than re-slicing, because a re-slice leaves the base pointer
	// advancing into the old array and append() then reallocates periodically.
	// The bound is therefore a property of the type, not of the input.
	tail []byte
	// limit is the retained window size in bytes.
	limit int
	// total is every byte written, retained or not. It is what makes the loss
	// observable (a reader can compare total against len(tail)) instead of
	// silent.
	total int64
}

// newBoundedTailWriter returns a writer retaining the last limit bytes of
// everything written to it. A limit <= 0 is raised to 1: a writer that retains
// nothing would return an empty stream to buildOutput, turning "the command
// printed a lot" into "the command printed nothing" — a silent failure, and the
// one outcome this type must never produce.
func newBoundedTailWriter(limit int) *boundedTailWriter {
	if limit <= 0 {
		limit = 1
	}
	return &boundedTailWriter{tail: make([]byte, 0, limit), limit: limit}
}

// Write implements io.Writer. It never returns an error and never blocks: the
// pipe reader must keep draining, because a reader that stops reading is flow
// control the child did not ask for (a command writing to a full pipe blocks,
// and the idle monitor would then kill it for silence it did not cause).
func (w *boundedTailWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.total += int64(n)

	// A single chunk at least as large as the window replaces it outright.
	if n >= w.limit {
		w.tail = append(w.tail[:0], p[n-w.limit:]...)
		return n, nil
	}
	// Shift the window by exactly the overflow, in place: len+cap stay put, so
	// the array never leaves the bound.
	if over := len(w.tail) + n - w.limit; over > 0 {
		copy(w.tail, w.tail[over:])
		w.tail = w.tail[:len(w.tail)-over]
	}
	w.tail = append(w.tail, p...)
	return n, nil
}

// Bytes returns the retained window. The caller must not retain the result
// across further writes (the next Write may reuse the array).
func (w *boundedTailWriter) Bytes() []byte { return w.tail }

// String returns the retained window as a string.
func (w *boundedTailWriter) String() string { return string(w.tail) }

// Truncated reports whether any written byte was NOT retained — the fact a
// reader needs in order to know the stream it holds is a window, not the whole
// output (Rule 15: an unobservable loss is a silent failure).
func (w *boundedTailWriter) Truncated() bool { return w.total > int64(len(w.tail)) }
