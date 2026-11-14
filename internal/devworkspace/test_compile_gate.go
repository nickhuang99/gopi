package devworkspace

// The clone channel's TEST-COMPILE gate — the leg that builds every package's
// test binaries (running no test) on the tip this attempt is about to publish,
// and refuses the delivery while any of them does not compile.
//
// ── THE DEFECT THIS CLOSES (measured 2026-11-03, row 01a19c0a) ─────────────
//
// Commit 928740fe landed a `type result` declaration in a `cmd/agent`
// production file that collided with one already declared in a `cmd/agent`
// _test.go. `go build` stayed GREEN (it does not compile `_test.go`), the test
// package was UNCOMPILABLE for 44 minutes, and EVERY EXISTING GUARD WAS GREEN
// throughout — including the two test-compile legs that catch exactly this
// class.
//
// WHY THEY WERE GREEN. Those legs are real and they work
// (mergeBuildGateTestCompileCmd / mergeUnionGateGoTestCompileCmdFmt in
// internal/tools, both now reading internal/testcompile's ONE spelling), but
// their only callers are the MERGE path (task_merge.go, repo_sweep.go), and
// master has essentially stopped travelling through it: measured on peerbox, 762
// ref updates by `push` against 10 by `merge`, and on this clone **0 merge
// commits in the 192 commits since the last one**. So the guards were correct
// and unreachable.
//
//	$ grep -rn 'mergeBuildGate(repo\|mergeUnionGate(repo' --include='*.go' \
//	      internal/ cmd/ | grep -v _test.go
//	internal/tools/task_merge.go:910, :945
//	internal/tools/repo_sweep.go:1461, :1465      ← merge path only
//	$ grep -h 'go test' internal/devworkspace/*.go | wc -l
//	0                                              ← this channel ran no test leg
//
// ── WHAT THIS LEG DOES NOT DO (Rule 35: no new mechanism) ─────────────────
//
// It is ONE MORE STEP inside the delivery attempt the channel already runs. No
// script, no cron line, no hook, no resident, no config key, no new settings
// row: the command is `internal/testcompile.Command`, the SAME string the merge
// gates execute, and the premise that decides whether it may run at all is the
// one the merge gate already uses — `go.mod` present (a Go module), derived
// from the tree rather than declared (Rule 12's declared-not-discovered rule
// applies to the BUILD; this leg needs no declaration because the toolchain
// reads the tree).
//
// ── WHY IT SITS AFTER THE DECLARED BUILD AND BEFORE THE PUSH ──────────────
//
// After the build, because the man gate immediately above it reads `bin/gopi`
// — the binary THIS branch built — and a leg inserted before it would put the
// long compile in front of the step whose output the next gate consumes, so a
// broken test tree would refuse the delivery without the binary ever being
// re-measured. Order: build → man → test-compile → push. The push is last
// because the whole point is to refuse BEFORE the ref moves.
//
// ── RULE 27, BY CONSTRUCTION ──────────────────────────────────────────────
//
// `-run '^$'` matches no test: every test binary is BUILT and linked, nothing
// is RUN. No ledger opens, no daemon starts, no fixture executes, no load is
// generated. The leg cannot saturate anything because no test body ever
// executes.
//
// ── RULE 15: FOUR SHAPES, AND ONLY ONE BLOCKS ─────────────────────────────
//
//	no `go.mod` in the tree          → ABSTENTION naming what was looked for (a
//	                                   non-Go repo: the question is unanswerable
//	                                   here, which is NOT the answer "clean")
//	the leg exited 0                 → silent (a healthy delivery stays quiet)
//	the leg exited non-zero          → a BLOCKER carrying the compiler's own
//	                                   words and the command to re-run
//	the leg could not be STARTED     → ABSTENTION with the reason (a host
//	                                   without the toolchain is not evidence
//	                                   about the branch)
//
// The blocking/non-blocking split is `*exec.ExitError` versus everything else —
// the man gate's own criterion, reused rather than re-invented. A TIMEOUT is
// deliberately an ABSTENTION, not a refusal: "the compiler did not finish" is
// not the fact "the branch does not compile", and a leg that refuses on its own
// slowness gets switched off by the first operator who meets it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/testcompile"
)

const (
	// testCompileGateNotePrefix is the wording an ABSTENTION opens with, spelled
	// once so the sentence and the tests that assert it cannot drift (Rule 1b).
	testCompileGateNotePrefix = "clone-channel: test-compile gate abstained"

	// testCompileGateStep is the step NAME this blocker reports. It names the
	// step, never the command: the command is quoted beside it by the report
	// itself (b.Step is filled with testcompile.Command), and a step label that
	// repeated it would read as two separate invocations.
	testCompileGateStep = "test-compile"

	// testCompileGateGoModFile is the premise. Its presence is what makes this
	// question answerable at all — the same file and the same test the merge
	// gate's own Go-module check uses (`mergeBuildGateIsGoRepo`), restated here
	// as a path constant because that predicate lives in a package this one
	// cannot import.
	testCompileGateGoModFile = "go.mod"

	// testCompileGateRefusalFmt is the refusal's own sentence. It states the
	// three facts a reader acts on: the command that was run, that NO test was
	// executed (so nobody hunts for a failing test that does not exist), and
	// that the delivery was refused before the push — i.e. origin did not move.
	testCompileGateRefusalFmt = "the %s pass refused: %s does not compile " +
		"(no test was RUN — the filter builds every test binary and executes nothing), " +
		"so this delivery was refused before the push and origin did not move"

	// testCompileGateWordsFmt wraps the tool's own output under the label the
	// other kinds use, so a reader always knows which sentences came from a
	// program (Rule 15: never dress this channel's prose as a tool's output).
	testCompileGateWordsFmt = "the compiler's own words: %s"

	// testCompileGateNoPackagesToken is the toolchain's OWN words for "this
	// module contains no package at all" — and it is a NON-ZERO exit, which
	// makes it the one shape that would turn a legitimate tree into a refusal.
	//
	// MEASURED (2026-11-03, evidence/premise-probe.sh), a module whose only file
	// is go.mod:
	//
	//	go build ./...                        → "go: warning: ./... matched no
	//	                                        packages", EXIT 0
	//	go test -run '^$' -count=1 ./...      → same warning + "no packages to
	//	                                        test", EXIT 1
	//
	// So "nothing to compile" is not "does not compile", and a leg that treated
	// the exit status alone as the answer refused a perfectly healthy delivery
	// whose fixture repository has no Go package yet — the man gate's own
	// control arm, found by the branch-scoped suite, not by a review.
	testCompileGateNoPackagesToken = "no packages to test"
)

// BlockerKindTestCompile is its OWN kind because the fact it carries sends a
// reader somewhere the other kinds never do: the code COMPILES for production
// and the TEST tree does not, which is a collision between a production
// declaration and a `_test.go` declaration (or a test referencing something
// that was removed). Sharing BlockerKindBuild's kind would make the refusal say
// "the build failed" about a build that succeeded — the conflation
// BlockerKindDiskExhaustion was split out for, one step further.
const BlockerKindTestCompile BlockerKind = "test-compile"

// testCompileGate runs the test-compile leg on the tree this attempt is about to
// publish. It returns nil when the delivery may go on (compiles, or the question
// cannot be asked here) and a *Blocker when the branch must not be published.
func testCompileGate(ctx context.Context, cfg Config, dir string) *Blocker {
	if why := testCompileGateAbsence(dir); why != "" {
		testCompileGateNote(cfg.Log, why)
		return nil
	}
	// THE ITERATION'S OWN BUDGET, not cfg.BuildTimeout: that field bounds the
	// DECLARED BUILD (which the caller may raise for a slow repo), while this
	// command's cost is a property of the toolchain and is bounded by the same
	// figure the merge gates give it (Rule 2: one number, one reader).
	cctx, cancel := context.WithTimeout(ctx, testcompile.Timeout)
	defer cancel()
	out, err := run(cctx, dir, shellBinary, "-c", testcompile.Command)
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		// THE LEG COULD NOT ANSWER: no toolchain, a killed child, a context that
		// expired. Reported (Rule 15), never taken for a pass, and never dressed
		// as a branch defect — "the compiler did not finish" and "the branch
		// does not compile" send a reader to two different places.
		testCompileGateNote(cfg.Log, fmt.Sprintf(
			"%s could not be run in %s (%v; output: %s) — the TEST TREES were NOT compiled",
			testcompile.Command, dir, err, tail(out)))
		return nil
	}
	// A MODULE WITH NO PACKAGE EXITS NON-ZERO AND IS HEALTHY (measured, see
	// testCompileGateNoPackagesToken): the toolchain has nothing to build, which
	// is not the same statement as "the test trees do not compile". Abstaining
	// here is what keeps this leg from refusing a real delivery — and the
	// refusal it would otherwise produce names a compile error that does not
	// exist.
	if strings.Contains(tail(out), testCompileGateNoPackagesToken) {
		testCompileGateNote(cfg.Log, fmt.Sprintf(
			"this module carries no package at all — %s said %q, so the TEST TREES were NOT compiled",
			testcompile.Command, testCompileGateNoPackagesToken))
		return nil
	}
	return &Blocker{
		Kind:  BlockerKindTestCompile,
		Step:  testcompile.Command,
		Words: fmt.Sprintf(testCompileGateWordsFmt, tail(out)),
	}
}

// testCompileGateAbsence answers whether the question can be asked of THIS tree
// at all, and says what was looked for when it cannot (Rule 15: "there is no Go
// module here" and "the test trees compile" are different facts, and a fixture
// repository is the first one).
func testCompileGateAbsence(dir string) string {
	if dir == "" {
		return fmt.Sprintf("there is no workspace to measure (looked for %s)", testCompileGateGoModFile)
	}
	if _, err := os.Stat(filepath.Join(dir, testCompileGateGoModFile)); err != nil {
		return fmt.Sprintf("this tree is not a Go module — looked for %s in %s and did not find it, "+
			"so the TEST TREES were NOT compiled (a non-Go tree has no KNOWN way to compile its tests, "+
			"and inventing one for an unknown language is how a gate starts false-refusing)",
			testCompileGateGoModFile, dir)
	}
	return ""
}

// testCompileGateNote prints a NON-BLOCKING sentence: the abstention half of this
// leg. It is the one place an unmeasurable tree is spoken, so "nothing to
// report" and "never measured" cannot look alike (Rule 15).
func testCompileGateNote(w io.Writer, sentence string) {
	if w == nil || strings.TrimSpace(sentence) == "" {
		return
	}
	fmt.Fprintf(w, testCompileGateNotePrefix+" — %s\n", sentence)
}

// testCompileGateReport renders this kind's refusal. It opens with the
// channel's own sentence (which names the command and says no test ran) and ends
// with the compiler's own words verbatim, so the report prints them as the
// refusal itself and does not append the generic "the tool's own words" label a
// second time — the label would then frame the channel's prose as a program's
// output (the same reasoning BlockerKindMan carries).
func testCompileGateReport(b *Blocker) string {
	sentence := fmt.Sprintf(CloneChannelPrefix+testCompileGateRefusalFmt, testCompileGateStep, testcompile.Command)
	if words := strings.TrimSpace(b.Words); words != "" {
		return sentence + "; " + words
	}
	return sentence
}

// testCompileGateTimeout is exported for the package's own tests to assert that
// the leg uses the leaf package's budget rather than the caller's build timeout
// — a second budget would be a second answer to "how long may this take".
func testCompileGateTimeout() time.Duration { return testcompile.Timeout }
