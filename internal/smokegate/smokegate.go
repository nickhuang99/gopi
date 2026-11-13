// Package smokegate names the ONE command the merge/verify path runs, and the
// ONE shape that path must never prescribe again.
//
// WHY A LEAF PACKAGE (PROJECT.md Rule 14). The smoke command is spoken by three
// parties that must not import each other:
//
//   - internal/tools, which RENDERS the LLM-facing briefings a child gopi is
//     handed (verify_gate.go's rework child and verify-retry child, tasks.go's
//     verifier briefing); these are the SHIPPED briefings today — the take-over
//     prompt that used to sit here as the third party was deleted with the
//     retired merger daemon (epic merger-delete-dead-impl, 2026-09-19);
//   - cmd/agent, whose string literals on this path the gate reads;
//   - internal/standards, whose text gate turns a whole-tree prescription on
//     that path RED again.
//
// A literal repeated in three packages is Rule 1b's defect and Rule 13's drift:
// the day the smoke command changes, two of the three keep the old spelling and
// an agent is briefed to run a command nobody maintains. A zero-dependency leaf
// package (stdlib imports only, no project imports — the internal/toolnames
// precedent) is what all three can share.
//
// THE RULE THIS ENCODES, in one sentence: on the merge/verify path the test
// verdict comes from the SMOKE set, BRANCH-SCOPED — never the whole tree.
//
//   - SMOKE because the path runs under a bounded wait cap while other work
//     waits, and the heavy set (minutes of real device work, wide parallelism,
//     host-condition-dependent) declares itself with heavytest.Require and skips
//     itself unless GOPI_HEAVY_TESTS is truthy (PROJECT.md Rule 27);
//   - BRANCH-SCOPED because a merge can only regress what the branch's diff
//     touched — running the whole module re-runs every other package's fixture
//     cost to learn nothing about this branch (measured: ~360 ledger-opening
//     fixtures is what pushes `cmd/agent` past Go's per-package budget);
//   - NEVER THE WHOLE TREE, which is why WholeTreeNeedle exists: the
//     prescription is mechanical to spot, so internal/standards reports it
//     instead of trusting a future editor to remember.
//
// The doc below is the authority the briefings and the docs quote: one text,
// one place, so "what counts as the smoke command" is not answered per-reader.
package smokegate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Command is the smoke command this repo runs on the merge/verify path:
	// the merge target of the repo's own Makefile, which routes through
	// scripts/test-budget.sh (one declared per-package budget, a timeout
	// reported AS a timeout) with the smoke classification in force — heavy
	// tests skip themselves and the skipped set is written to
	// GOPI_HEAVY_SKIP_LEDGER for review.
	//
	// WHY `make test` AND NOT `go test ./...`: the bare go invocation bypasses
	// the declared budget AND the skip ledger, so a merge-path run would both
	// trip Go's untuned 10m default and leave no record of what it skipped. A
	// repo that declares its build (PROJECT.md Rule 12) declares this the same
	// way; the command shown here is this repo's.
	//
	// WHAT `make test` RUNS BY DEFAULT (operator's 2026-11-06 ruling): the
	// packages THIS checkout touched, derived from the diff against the base —
	// never the whole module. The bare entry point is the one a caller types
	// without reading anything, so its default has to be the cheap correct one:
	// the whole module costs 688 s on cmd/agent alone against a 2-minute
	// foreground budget, a run that is structurally un-runnable and looks
	// exactly like a hang. With no .go change at all it runs nothing and says
	// so, because a document-only change has nothing to test. `make test-all`
	// is the NAMED whole-tree opt-in; a caller who wants everything asks.
	Command = "make test"

	// ScopedCommand is the EXPLICIT form: the same smoke command narrowed to a
	// package list the caller already holds. Since `make test` itself defaults
	// to the touched packages, reaching for this one is how a caller SUPPLIES
	// its own list (a merge verdict naming the packages the merge can have
	// regressed, rather than a diff this run would have to trust).
	//
	// The replacement is deliberately spelled as a slot rather than a filled-in
	// example: a briefing that invented package names would be wrong for every
	// branch but one. The single-quoted PKGS= assignment matters —
	// ./internal/agent/ is not a make target, so an unquoted list stops after
	// the first package.
	//
	// A CALLER THAT ALREADY HAS THE PACKAGES does not fill this text by hand:
	// it calls ScopedForPackages, which renders the SAME head with its own list
	// (Rule 13 — the head is spelled once, in ScopedTarget, and this constant is
	// built from it).
	ScopedCommand = ScopedTarget + " " + ScopedPackagesAssignment + "'<the Go packages your diff touched>'"

	// ScopedTarget is the make TARGET that narrows the smoke run to a package
	// list. It is deliberately a separate constant from Command: `make test`
	// takes no scope at all (its recipe runs the whole module) and `make
	// test-budget` takes PKGS — so a caller that needs a bounded run must reach
	// for THIS target, and the ordering in a Makefile recipe is not something a
	// caller may guess at.
	ScopedTarget = makeVerb + " " + scopedTargetName

	// ScopedPackagesAssignment is the make variable the narrowed command takes.
	// Spelled once: the briefing text above and ScopedForPackages both build on
	// it, so a rename cannot leave one of them behind.
	ScopedPackagesAssignment = "PKGS="

	// EntryPointFile is the file the declared smoke command is DEFINED in. A
	// caller deciding whether it can run the command AT ALL in some tree reads
	// this file through DeclaresScopedTarget — an invented command is never the
	// answer for a tree that declares none (Rule 12's contract, read at the
	// other end).
	EntryPointFile = "Makefile"

	// makeVerb and scopedTargetName are the two words ScopedTarget is composed
	// from (unexported: consumers reach the target through ScopedTarget and the
	// probe below, never by re-spelling either word).
	makeVerb         = "make"
	scopedTargetName = "test-budget"

	// WholeTreeNeedle is the shape the merge/verify path must not prescribe:
	// the whole-tree suite spelled as a runnable command. internal/standards
	// reports it on that path in a backtick-quoted command span (the spelling a
	// model reads as an instruction), so the retirement is mechanical rather
	// than a note in a doc.
	//
	// NOT banned in general: the whole-tree suite is exactly what the ledger's
	// baseline sweep and the heavy/off-peak opt-in (make test-heavy) legitimately
	// run. What is retired is PRESCRIBING it to an LLM that is judging a merge
	// under a wait cap.
	WholeTreeNeedle = "go test ./..."

	// Doc is the authoritative statement of the rule, quoted into the docs and
	// the merger prompt so a reader finds the same sentence in each place.
	Doc = "the merge/verify path runs SMOKE tests, BRANCH-SCOPED — never the whole tree"
)

// ScopedForPackages renders the branch-scoped smoke command line for a caller
// that ALREADY HOLDS the package list (the touched-package primitive's answer).
//
// It exists so the head of the command is spelled nowhere else (Rule 13): the
// caller supplies only the list and ONE quoting function for the argument, and
// this function supplies the target and the assignment. The quoting is a
// parameter rather than an imported helper because the caller owns the shell it
// will run (`sh -c`), and a leaf constants package has no business deciding
// that for it.
//
// The list is joined into ONE argument on purpose: `make test-budget
// PKGS=./a/... ./b/...` would make `./b/...` a make TARGET, so the run would
// silently cover a subset — the measured failure TestScopedCommandStaysOne-…
// pins one level up.
func ScopedForPackages(packages []string, quote func(string) string) string {
	return ScopedTarget + " " + ScopedPackagesAssignment + quote(strings.Join(packages, " "))
}

// DeclaresScopedTarget reports whether the tree rooted at dir DECLARES the
// scoped smoke target this package names — read off the tree itself, never
// assumed (Rule 12: a repo's build contract is declared, not discovered).
//
// It is the honest answer to "may this caller run the scoped command here?": a
// tree that does not declare the target cannot be made to by inventing a
// command (Rule 15 — a caller that cannot run the declared thing must say so,
// not substitute one of its own). The scan looks for the target as a make RULE
// (a line beginning with `<name>:`), so a comment or a recipe line that merely
// mentions the word does not count.
func DeclaresScopedTarget(dir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, EntryPointFile))
	if err != nil {
		return false, fmt.Errorf("smokegate: read %s in %s: %w", EntryPointFile, dir, err)
	}
	want := scopedTargetName + ":"
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimRight(line, "\r"), want) {
			return true, nil
		}
	}
	return false, nil
}
