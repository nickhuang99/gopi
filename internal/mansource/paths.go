package mansource

// The man pipeline's SHARED SPELLINGS (Rules 1/13/14): one definition per value,
// referenced by BOTH the writer (internal/standards/testdata/mancensus.go) and
// the readers that judge its output (the delivery channel's man gate in
// internal/devworkspace, the Makefile-consistency test). The package stays a
// LEAF — constants only, no imports, no logic — so every consumer can import it
// without risking a cycle.
//
// WHY THEY LIVE HERE AND NOT IN THE GENERATOR. The generator is a `main`
// package under testdata/, so nothing can import it; a consumer that retyped
// `internal/standards/testdata/mancensus.go` beside it would be the mirror
// Rule 13 forbids, and the day the file moves the reader would silently measure
// a program that no longer exists (Rule 15: an unmeasurable tree must say so,
// and it can only say so about the path it was actually reading).
const (
	// GeneratorRel is the generator's repo-relative path: the `main` package that
	// rewrites the generated regions of every man source (and whose `--check`
	// re-derives them without writing).
	GeneratorRel = "internal/standards/testdata/mancensus.go"

	// SourceDirRel is the repo-relative directory holding the man SOURCES (the
	// markdown the generator rewrites), not the rendered `.1` pages.
	SourceDirRel = "man"

	// BuiltBinaryRel is the built binary the help pages are read FROM: the fact
	// source is the artifact an operator runs (Rule 31), never a usage constant
	// in the source tree that merely ought to agree with it.
	BuiltBinaryRel = "bin/gopi"

	// CheckFlag re-derives and compares without writing; exit 1 means at least
	// one source is stale.
	CheckFlag = "--check"

	// StaleToken is the word the generator prints for a source whose generated
	// region no longer matches the binary's help page. It is a CONSTANT because
	// a reader (the delivery channel's man gate) recognises the verdict by this
	// token and names the page it belongs to — a second spelling on either side
	// would let a real drift read as "up to date".
	StaleToken = "STALE"

	// UpToDateToken is the word the generator prints for a source that matched —
	// one verdict line per page, healthy ones included. It is a constant for the
	// SAME reason as StaleToken and one more: the green half of the delivery gate
	// COUNTS these lines and refuses to call a scan clean when it counted none, so
	// a second spelling here would turn "measured 12 pages, all current" into
	// "measured nothing", which that gate reports as an abstention rather than a
	// pass. The two tokens are the generator's own vocabulary, and this package is
	// where both sides read it from.
	UpToDateToken = "up to date"

	// RegenerateHint is the ONE command a refusal may prescribe: the generator's
	// write mode. A reader must never be told to hand-write a SYNOPSIS block —
	// the block is copied verbatim from `-h` by the generator, and a hand-written
	// one is the drift this whole pipeline exists to make impossible.
	RegenerateHint = "make man"
)
