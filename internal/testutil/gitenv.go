package testutil

// Hermetic git environment for test fixtures (the 2026-10-13 data-loss incident).
//
// ROOT CAUSE THIS FILE EXISTS FOR. A class of fixtures builds its own repository
// with
//
//	root := t.TempDir()
//	cmd := exec.Command("git", args...)
//	cmd.Dir = root
//	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", ...)
//
// **`cmd.Dir` does NOT override `GIT_DIR`.** GIT_DIR, once set, has top
// priority: cmd.Dir only changes a cwd that git then ignores. A run that had
// exported GIT_DIR=/home/user/work/GoPi/.git (to give an unrelated provenance
// probe a git context) therefore made the fixture's `git init` RE-INITIALISE
// the real repository, and `git add -A` + `git commit` committed the real
// working tree — 12 fixture commits took the real master tree from 7678 files
// to 2 (507,046 deletions), and a sibling worktree was cut from the wreckage.
//
// THE FIX IS AT THE SURFACE, not per fixture, and it has TWO halves plus one
// checked premise:
//
//  1. GitEnv / WithoutGitRoutingEnv — the environment a fixture's git child is
//     given, with every repository-routing name REMOVED.
//  2. PinSuiteGitEnv — the PROCESS-level guarantee: a suite started from a
//     shell that exported GIT_DIR does not carry it at all (wired into
//     PinSuiteForTestMain, the entry point every suite already shares).
//  3. AssertGitDirInsideRoot — the premise "I built a repository HERE" is
//     ASSERTED, never assumed.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// GitDirEnvName is git's own repository pointer: the routing variable with
	// the HIGHEST priority, and the one whose inheritance caused the incident.
	// Exported so a caller that must reason about the name (a fixture's
	// diagnostic, the standards gate) references it instead of re-typing the
	// literal (Rule 1b/13/14).
	GitDirEnvName = "GIT_DIR"
	// GitWorkTreeEnvName overrides the working tree git reports and adds from.
	// Exported for the same reason as GitDirEnvName: a caller that must NAME the
	// routing variable — the one shape is a child handed the routed environment
	// on purpose, `testutil.GitEnv(testutil.GitDirEnvName+"=…",
	// testutil.GitWorkTreeEnvName+"=…")` — references it instead of re-typing
	// the literal (Rule 1b/13/14). MEASURED (task 01a14162): this name was
	// unexported while internal/docclass's material fixture needed it, so that
	// file spelled a package-local `gitWorkTreeEnvName` that exists in NO
	// package it imports, and every test build of internal/docclass was red.
	GitWorkTreeEnvName = "GIT_WORK_TREE"
	// gitIndexFileEnvName redirects the index — a fixture's `git add` would
	// write the REAL index while every filesystem path looks local.
	gitIndexFileEnvName = "GIT_INDEX_FILE"
	// gitObjectDirectoryEnvName redirects the object store: the same hazard one
	// level down (fixture commits written into the real repository's objects).
	gitObjectDirectoryEnvName = "GIT_OBJECT_DIRECTORY"
	// gitCommonDirEnvName redirects the shared directory of a worktree layout.
	gitCommonDirEnvName = "GIT_COMMON_DIR"

	// GitEnvFuncName is the helper's own name. It is a constant so the standards
	// gate that enforces "a fixture's git child is hermetic" names the same
	// spelling a caller writes (Rule 14).
	GitEnvFuncName = "GitEnv"
)

// gitRoutingEnvNameList is the ONE definition of the routing names, ordered so
// a report lists them the same way every run.
//
// Each entry routes git at a DIFFERENT repository, index or object store than
// the one the fixture built, and every one of them beats a `-C` flag and a
// cmd.Dir alike. They are REMOVED, never blanked: `GIT_DIR=` is not "unset" to
// git, it is an empty path, and git answers `fatal: The empty string is not a
// valid path` (measured — see cmd/agent/cli_doccheck_test.go's own comment).
var gitRoutingEnvNameList = []string{
	GitDirEnvName,
	GitWorkTreeEnvName,
	gitIndexFileEnvName,
	gitObjectDirectoryEnvName,
	gitCommonDirEnvName,
}

// GitRoutingEnvNames returns the routing names this package strips, in the
// fixed report order. It exists so a consumer outside this package (a fixture's
// witness, the standards gate's own proof) references the SAME list instead of
// re-typing the names (Rule 1b/13/14): the returned slice is a COPY, so a
// caller that sorts or appends to it cannot change what everyone else strips.
func GitRoutingEnvNames() []string {
	return append([]string(nil), gitRoutingEnvNameList...)
}

// gitRoutingEnvNames is the membership set, DERIVED from the list so the two
// can never disagree (Rule 14).
var gitRoutingEnvNames = routingEnvNameSet(gitRoutingEnvNameList)

// routingEnvNameSet builds the membership set of a name list.
func routingEnvNameSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// GitEnv returns os.Environ() with every repository-routing variable REMOVED,
// followed by extra (name=value pairs the caller pins, e.g. the fixture commit
// identity). It is the environment a fixture's git child must be given:
//
//	cmd := exec.Command("git", args...)
//	cmd.Dir = root
//	cmd.Env = testutil.GitEnv("GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid")
//
// The strip is applied to the INHERITED environment and the caller's own
// entries are appended after it, so a caller cannot lose the strip by adding
// its identity pins — and a caller that deliberately pins a routing name still
// can, which is the documented escape for the rare site whose subject IS the
// routing.
func GitEnv(extra ...string) []string {
	out := make([]string, 0, len(os.Environ())+len(extra))
	out = append(out, WithoutGitRoutingEnv(os.Environ())...)
	return append(out, extra...)
}

// WithoutGitRoutingEnv returns env with every repository-routing entry removed.
// It exists for a caller that has already built an environment slice (the
// production seams do: internal/tools' probes and internal/docclass's corpus) and
// must strip it before handing it to a git child.
//
// The match is on the WHOLE name, so `GIT_DIRECTIVE=…` — a name that merely
// starts with a routing name — is kept: over-removing a foreign variable is
// its own silent defect.
func WithoutGitRoutingEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if gitRoutingEnvNames[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// THERE IS DELIBERATELY NO ENVIRONMENT TICKET HERE — and the name
// `GOPI_TEST_GIT_ROUTING_ENV` must not come back. Until 2026-10-16 this file
// declared `const pinSuiteGitRoutingEnvName = "GOPI_TEST_GIT_ROUTING_ENV"` as a
// "ticket" for a witness that would undo the process pin for one child. No
// reader ever arrived, and none was ever possible: the restore below takes NO
// argument — it re-arms every name the PIN actually dropped, whichever caller
// asks — so naming a variable in the environment could never have changed which
// names came back. The name had zero other references in this tree, in every
// revision of it, in CONFIG.md, in the help-env catalog and in every test
// harness (measured), i.e. it was a control surface that only LOOKED usable:
// an operator who exported it would have changed nothing (Rule 21, whose gate
// fails exactly this shape; Rule 35 — the dead interface is deleted rather than
// wired, because wiring it would mean inventing a consumer whose only purpose
// is to justify the name). A caller that must undo the pin for one child calls
// RestoreGitRoutingEnv below and gets the honest, argument-free contract.

// PinSuiteGitEnv drops every repository-routing variable from THIS process, so
// a suite started from a shell that exported GIT_DIR cannot carry it into any
// git child it spawns.
//
// It is the process-level half of the fix, and it rides PinSuiteForTestMain —
// the ONE entry point every suite already shares, so no suite repeats the loop
// and a new TestMain gets it for free. Remediation is reported on stderr rather
// than swallowed (Rule 15): an operator who exported GIT_DIR learns that the
// suite dropped it, instead of silently running in a different environment than
// the one they set up.
//
// The dropped values are KEPT (pinSuiteDroppedRoutingEnv) rather than thrown
// away, because one legitimate caller has to re-arm them for a single child —
// see RestoreGitRoutingEnv.
func PinSuiteGitEnv(suite string) {
	for _, name := range gitRoutingEnvNameList {
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: suite %q: %s not dropped: %v — a git child of this "+
				"suite may answer about that repository instead of its fixture's (cmd.Dir cannot "+
				"override it; 2026-10-13)\n", suite, name, err)
			continue
		}
		pinSuiteDroppedRoutingEnv[name] = value
		fmt.Fprintf(os.Stderr, "testutil: suite %q: dropped %s=%s from this process — an inherited "+
			"routing variable makes a fixture's git child answer about SOMEONE ELSE'S repository, "+
			"which cmd.Dir cannot override (2026-10-13: 12 fixture commits landed on real master)\n",
			suite, name, value)
	}
}

// pinSuiteDroppedRoutingEnv holds what PinSuiteGitEnv removed from this process,
// so RestoreGitRoutingEnv can put it back for the one child that has to inherit
// it. Written only by the pin (which runs once, from TestMain) and read only by
// the restore, so no lock is needed.
var pinSuiteDroppedRoutingEnv = map[string]string{}

// RestoreGitRoutingEnv re-arms the routing variables this suite dropped, from
// the values the suite itself held, and returns a function that drops them
// again.
//
// IT EXISTS FOR EXACTLY ONE SHAPE, and a caller should be able to say which one
// it is: a test whose SUBJECT is the routing itself. The hermetic-fixture
// witness (internal/docclass) re-executes its own binary to prove that a git
// child which INHERITS GIT_DIR commits into a foreign repository — the measurement
// is the damage, so the child must inherit the poison, and a suite-level strip
// applied to every child would delete the red arm instead of the defect.
//
// WHY IT CANNOT INJECT A VALUE: only names the suite actually dropped are
// restorable, and their values are the ones the suite was started with. A test
// that wants a FIXTURE repository to be routed at therefore has to build that
// child's environment itself (cmd.Env), which is what the two-arm witness does.
// It is NOT a general escape from the strip and it does not re-open the
// incident: production and fixtures keep the dropped environment for the whole
// suite; this call is scoped to the child it is arranged around.
//
// The record is CONSUMED: a second call restores nothing, so a suite that calls
// it twice cannot re-arm the poison long after the child it was meant for.
func RestoreGitRoutingEnv() func() {
	restored := make([]string, 0, len(pinSuiteDroppedRoutingEnv))
	for name, value := range pinSuiteDroppedRoutingEnv {
		delete(pinSuiteDroppedRoutingEnv, name)
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: %s not restored for the child: %v — the child would "+
				"inherit a different environment than the measurement needs\n", name, err)
			continue
		}
		restored = append(restored, name)
	}
	return func() {
		for _, name := range restored {
			if err := os.Unsetenv(name); err != nil {
				fmt.Fprintf(os.Stderr, "testutil: %s left set after the child it was restored for: "+
					"%v — the rest of the suite would run on the pre-pin environment\n", name, err)
			}
		}
	}
}

// gitDirProbeArgs is the ONE question this file asks git. `rev-parse --git-dir`
// answers the repository git would actually use for a command run in the
// working directory — the fact the incident's fixtures assumed and never
// checked.
var gitDirProbeArgs = []string{"rev-parse", "--git-dir"}

// AssertGitDirInsideRoot fails the test unless the repository git resolves in
// root (with root as the working directory) is INSIDE root.
//
// "I built a repository HERE" is the fixture's premise, and the whole incident
// is that premise being false while nothing noticed: the fixture's `git init`
// had re-initialised a FOREIGN repository, and every assertion after it was
// measured against a tree the fixture never built. This is the assertion that
// makes the premise checked rather than assumed (Rule 31 — a surface nobody
// executed was never tested; here, a repository nobody verified was never
// built).
func AssertGitDirInsideRoot(t *testing.T, root string) {
	t.Helper()
	dir, inside, err := checkGitDirInsideRoot(root)
	if err != nil {
		t.Fatalf("fixture repository: %v\n"+
			"  the fixture asked git about %s and git could not answer — a fixture that cannot "+
			"name the repository it writes has no evidence it built one", err, root)
	}
	if !inside {
		t.Fatalf("fixture wrote to a repository OUTSIDE its own root:\n"+
			"  root           %s\n"+
			"  git --git-dir  %s\n"+
			"  this is the 2026-10-13 shape: an inherited %s (which cmd.Dir cannot override) "+
			"routed the fixture's git child at a foreign repository. Build the child's "+
			"environment with testutil.%s and this assertion holds.",
			root, dir, GitDirEnvName, GitEnvFuncName)
	}
}

// checkGitDirInsideRoot reports which repository git resolves for a command run
// in root, and whether that repository lies inside root. It is the predicate
// AssertGitDirInsideRoot reports on, exposed so a test can exercise the
// refusing arm without a subprocess and without a Fatal.
//
// The probe runs with the routing names STRIPPED: a fixture that still inherits
// a foreign GIT_DIR must SEE the mismatch and fail rather than be rescued by
// the probe.
func checkGitDirInsideRoot(root string) (dir string, inside bool, err error) {
	cmd := exec.Command("git", gitDirProbeArgs...)
	cmd.Dir = root
	cmd.Env = GitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", false, fmt.Errorf("git %s in %q: %w", strings.Join(gitDirProbeArgs, " "), root, err)
	}
	dir = strings.TrimSpace(string(out))
	if dir == "" {
		return "", false, fmt.Errorf("git %s in %q printed nothing", strings.Join(gitDirProbeArgs, " "), root)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	dir = filepath.Clean(dir)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		// Different volumes: no relative path exists at all, so the repository
		// is provably not inside root — that is an answer, not a failure.
		return dir, false, nil // no-wrap: the absence of a relative path IS the verdict
	}
	return dir, rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
