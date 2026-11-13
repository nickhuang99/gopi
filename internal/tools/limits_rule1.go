package tools

// Named size limits for the bash tool's pipe readers and the write tool's
// path-suggestion hint (PROJECT.md Rule 1/13: each bare literal was inlined at
// its call site — the 4 KiB pipe read buffer twice, the slice bound and the
// suggestion cap once each — and is declared ONCE here so a retune is a
// one-line change).
//
// Why a separate file rather than the top of bash.go / write.go: normative
// devdocs cite lines in those files by LINE NUMBER (e.g.
// devdoc/perf-fix-R1-R4-implemented.md names `internal/tools/bash.go:24`), so
// inserting declarations there would drift those citations off their symbols.
// A new file adds no line to any existing file — that is the reason, and it
// survives on its own.
//
// HISTORY, so nobody satisfies a check that does not exist. This used to say
// "the doc-anchor gate turns red": the deleted devdoc line-anchor gate asserted
// that devdoc line anchors resolved, and it was deleted with internal/docclass
// in 5a35f61. Nothing resolves those citations now — they are prose that can rot
// silently, and the cost of a drift is hand re-keying, not a red gate. The
// executable anchor check that replaced it — the config-anchor census over
// CONFIG.md rows (`config-anchor-census.go --check`, its TestMergeConfigAnchorGate*
// arms) — was itself deleted with the documentation-citation family in 78bdf05a
// (2026-11-05). Nothing executable checks an anchor on this tree; the whole
// subject is prose now.
const (
	// pipeReadBufferSize is the byte size of the stdout/stderr pipe read
	// buffer used by bash's output-copy goroutines. 4KB is ample for
	// line-oriented command output and keeps memory bounded per concurrent
	// command.
	pipeReadBufferSize = 4096

	// pipeRetainedTailSlackSize is the margin by which the retained window
	// exceeds maxOutputSize. It is ONE byte of substance and the rest is
	// headroom: buildOutput decides to truncate with a STRICT `len(body) >
	// maxOutputSize` test, so a window of exactly maxOutputSize would silently
	// stop the "[output truncated …]" marker from firing on any command whose
	// output exceeds the cap — the model would then read a 100 KB tail that
	// looks like the whole output. The retained bytes are the TRUE tail, so the
	// window being a suffix means the rendered result is byte-identical to what
	// the unbounded buffer produced, in every case. The margins cancel:
	// buildOutput prepends the "exit=N\n" line ("-" slack once) and the
	// truncation marker ("-" slack again), and any command whose output clears
	// the cap by less than the marker's own width leaves the body under the cap
	// either way, so the marker's presence is decided identically in both.
	pipeRetainedTailSlackSize = 4 * 1024

	// pipeRetainedTailSize is the bytes bash RETAINS per stream while a command
	// runs, and it is the whole fix for the package-level abort. It exists
	// because pipeReadBufferSize bounds the CHUNK, not the BUFFER: the two read
	// goroutines in bashInDirCtx appended every chunk to a bytes.Buffer that
	// grew without limit, so the amount retained was not a policy at all but
	// whatever the command happened to emit. Measured 2026-10-13, task
	// 01a1315950dd7c51b5a546b78494cf88: `yes` (TestBashIdleTimeout_Infinite-
	// OutputCommand, a 2 s budget) drove that buffer into a multi-GiB growslice
	// and the runtime aborted the whole test BINARY —
	//
	//	runtime: out of memory: cannot allocate 2147483648-byte block (…)
	//	bytes.(*Buffer).Write  internal/tools/bash.go:602
	//
	// — and a binary-level abort carries no failing test with it, so the budget
	// harness could only render the contradiction "failing tests: 0 / PACKAGE
	// FAILED", taking the package's verdict with its evidence.
	//
	// WHY THE VALUE IS DERIVED, NOT CHOSEN: the bytes buildOutput KEEPS are the
	// tail, capped at maxOutputSize, so retaining that window (plus the slack
	// above) can never change what the caller sees. Sizing the buffer by the
	// SAME constant that bounds the result is what makes the pair impossible to
	// drift — a retune of maxOutputSize moves both. The tail, not the head, is
	// retained because the tail is the half the renderer keeps: CLI errors —
	// build failures, test failures, git conflicts — land at the END of a
	// command's output.
	pipeRetainedTailSize = maxOutputSize + pipeRetainedTailSlackSize

	// maxPathSuggestions caps how many similar-directory hints a write error
	// message lists, so the LLM gets useful options without a wall of text.
	maxPathSuggestions = 3

	// suggestionPrefixLen is the name-prefix length used to fuzzy-match
	// similar directories when a target path's parent cannot be created.
	suggestionPrefixLen = 3
)
