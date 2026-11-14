// Package buildidentity — the ONE definition of the linker operand that stamps a
// gopi build with the revision it was made from. Leaf package, constants only
// (no imports, no logic): it is read by the build recipes (internal/testutil's
// suite builder, cmd/agent's own test-binary builder, a shell runner) AND by the
// freshness guard that reads the stamp back out of the artifact, and those
// consumers cannot all import one another — internal/version's tests import
// internal/testutil, so putting the operand there would close a cycle (Rule 14:
// a value shared across packages lives in a small exported-constant package every
// consumer can import).
//
// WHY THE RECIPE MUST SUPPLY THE STAMP, measured: the Go toolchain writes
// `vcs.revision` only when it recognizes a repository root, and it recognizes one
// only when `.git` is a DIRECTORY (cmd/go internal/vcs/vcs.go, isVCSRoot rejects a
// gitfile). A linked worktree — the shape every gopi task ground has (Rule 6a) —
// has a `.git` FILE, so the stamp is dropped in SILENCE: exit 0, empty stderr,
// and no `vcs.*` setting in the artifact. The experiment is
// devdoc/binary-identity-on-a-worktree-ground.md.
//
// WHY IT IS A CONSTANT AND NOT TYPED WHERE IT IS USED: the linker checks no
// operand. A misspelled `-X` path stamps NOTHING — exit 0, empty stderr, a binary
// that merely LOOKS stamped (measured twice while writing
// devdoc/binary-identity-on-a-worktree-ground.md; the guard that keeps this
// spelling honest is internal/version/module_path_agreement_test.go).
package buildidentity

const (
	// RevisionLdflagPath is the `importpath.name` the linker writes: the
	// package-level `var Revision` declared in internal/version. The recipe
	// composes `-X <this>=<40-hex>`, and the reader in cmd/agent accepts nothing
	// else, so a build that names any other variable carries no identity and the
	// freshness verdict says so instead of guessing.
	RevisionLdflagPath = "github.com/nickhuang99/gopi/internal/version.Revision"

	// LdflagXKey is the linker flag that writes a string variable; with its
	// operand it reads `-X <importpath>.<name>=<value>`.
	LdflagXKey = "-X"

	// LdflagsFlag is the `go build`/`go test` flag carrying linker flags. Named
	// because every recipe that stamps a build spells it.
	LdflagsFlag = "-ldflags"

	// RevisionVarName is the variable half of RevisionLdflagPath, on its own, so
	// a checker can assert the two halves agree rather than pattern-matching the
	// full string.
	RevisionVarName = "Revision"

	// BuildInfoKeyRevision is the key the Go toolchain writes into a binary it
	// stamped from a repository root it recognized (runtime/debug.BuildInfo
	// .Settings → "vcs.revision"). It is the TOOLCHAIN's wire name, not a gopi
	// value — but MORE THAN ONE package reads it back (internal/version's
	// printer, internal/tools' image self-check, internal/sessionevent's
	// session record), so it is a value they must AGREE on and it lives here,
	// once (Rule 13/14).
	//
	// WHY HERE AND NOT internal/version, which declares it today: internal/version
	// is NOT importable from internal/sessionevent. The test build closes the
	// cycle — internal/version's testmain imports internal/testutil, which
	// imports internal/sessionevent, which would import internal/version — and
	// Go refuses it (`import cycle not allowed in test`, measured while writing
	// devdoc/session-records-its-own-binary.md). internal/version re-exports
	// this constant, so its own readers keep their spelling and there is still
	// exactly ONE definition (Rule 14).
	BuildInfoKeyRevision = "vcs.revision"

	// BuildInfoKeyModified is the key the toolchain sets when the source tree
	// had uncommitted tracked changes at build time. Shared for the same reason
	// as BuildInfoKeyRevision; its only value meaning dirty is
	// BuildInfoValueModifiedTrue.
	BuildInfoKeyModified = "vcs.modified"

	// BuildInfoKeyTime is the committer time (RFC3339, UTC) of the stamped
	// revision. It is part of the same toolchain-written family, so it lives
	// with its siblings even though internal/tools does not read it back yet.
	BuildInfoKeyTime = "vcs.time"

	// BuildInfoValueModifiedTrue is the only BuildInfoKeyModified value that
	// means the tree was dirty; any other value means clean.
	BuildInfoValueModifiedTrue = "true"
)
