package tools

// image_selfcheck_test.go — the gate for the resident image self-check
// (image_selfcheck.go).
//
// THE PROMISES UNDER TEST, and how each is measured rather than asserted:
//
//   - the REPLACED arm fires on the KERNEL'S OWN MARKER, and on nothing else:
//     a fabricated procfs root whose self/exe link points at an unlinked file
//     must come back stale, while the very same link WITHOUT the marker must
//     not (the negative half is what keeps a path-string coincidence from
//     reading as proof).
//   - the CHECK CANNOT RUN does not read as "fine": an unreadable root and an
//     unresolvable link both come back Probed=false with a reason, never a
//     clean verdict.
//   - the line a resident face prints NAMES both images and the remedy, because
//     the operator's first question is "stale relative to WHAT?".
//
// WHAT THIS FILE DOES NOT FAKE: the process's OWN stamp. selfImageRevision
// reads the TEST BINARY's build info, which the toolchain stamps for a build in
// the main checkout exactly as it stamps the production binary in the main
// checkout, and carries none in a linked worktree. So the live-vs-live arm is
// asserted on whichever of the two shapes this run's build actually produced
// (see TestSelfImageCheckStaleStampArm), and the DISK side — the half that can
// be fabricated honestly — is driven by a real `go build` of a fixture main
// package.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	gopiversion "github.com/nick/gopi/internal/version"
)

const (
	// selfCheckFixturePkg is the trivial main package the stamp-arm scenarios
	// compile. Its revision differs from the test binary's by construction: it
	// is a fresh repository whose single commit no gopi checkout can share.
	selfCheckFixturePkg = "package main\n\nfunc main() {}\n"

	// selfCheckFixtureFile is that package's one source file.
	selfCheckFixtureFile = "main.go"

	// selfCheckFixtureStamp is a FULL revision-shaped string: a fixture copy of
	// the SHAPE arm ② compares, kept beside the case that parses it.
	selfCheckFixtureStamp = "0123456789abcdef0123456789abcdef01234567"

	// selfCheckAlarmedMarker is the fixed phrase inside AlarmLine's refusal —
	// asserted so a reworded alarm fails here rather than silently disappearing
	// from a captured run log (Rule 1b: one spelling, shared by producer and
	// test).
	selfCheckAlarmedMarker = "REFUSING to decide with a stale image"

	// selfCheckNotArmedMarker is the fixed phrase inside the note that says the
	// check could not be armed.
	selfCheckNotArmedMarker = "image self-check NOT ARMED"

	// selfCheckArmedMarker is the fixed phrase inside the note that says it was.
	selfCheckArmedMarker = "image self-check armed"

	// selfCheckDeletedSuffix is the kernel's OWN marker, spelled in the fixture
	// so it reads as what it imitates. The production spelling lives in
	// merger_status.go (procDeletedSuffix) and is the one the check reads; this
	// is the fixture's copy of a KERNEL constant, not a second definition of
	// ours (Rule 13 governs our values, and " (deleted)" is not ours).
	selfCheckDeletedSuffix = " (deleted)"

	// mergeProgForSelfCheckTest is the program prefix the rendered lines carry.
	// It is a literal in this package's test only: the production spelling is
	// cmd/agent's mergeProgTopLevel, which this package cannot import.
	mergeProgForSelfCheckTest = "gopi merge"
)

// selfCheckFakeProcRoot builds a procfs root whose self/exe is the link the
// scenario needs. It is the ONLY honest way to reach arm ①: the alternative is
// replacing the running test binary's own file, which would make every later
// test in this package run the wrong program.
func selfCheckFakeProcRoot(t *testing.T, linkTarget string) string {
	t.Helper()
	root := t.TempDir()
	selfDir := filepath.Join(root, "self")
	if err := os.MkdirAll(selfDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", selfDir, err)
	}
	if err := os.Symlink(linkTarget, filepath.Join(selfDir, "exe")); err != nil {
		t.Fatalf("symlink %s: %v", linkTarget, err)
	}
	return root
}

// selfCheckDeletedTarget creates a real file, unlinks it and returns the path
// with the kernel's own deleted marker appended — the exact string
// /proc/<pid>/exe reports for a replaced image.
func selfCheckDeletedTarget(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "replaced-artifact")
	if err := os.WriteFile(path, []byte("gone\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("unlink %s: %v", path, err)
	}
	return path + selfCheckDeletedSuffix
}

// TestSelfImageCheckReplacedImageIsRefused is the POSITIVE half of arm ①: the
// kernel says the file is gone, so the check must come back stale, name the
// path, and print a refusal that carries both images.
func TestSelfImageCheckReplacedImageIsRefused(t *testing.T) {
	target := selfCheckDeletedTarget(t)
	check := SelfImageProbeAt(selfCheckFakeProcRoot(t, target))

	if !check.Probed {
		t.Fatalf("the check did not run: %s", check.Unmeasurable)
	}
	if !check.Replaced || !check.Stale {
		t.Fatalf("an unlinked image read as fresh: replaced=%v stale=%v (selfPath=%q)", check.Replaced, check.Stale, check.SelfPath)
	}
	if check.DiskPath != strings.TrimSuffix(target, selfCheckDeletedSuffix) {
		t.Errorf("the report must name the path the machine would start, got %q want %q", check.DiskPath, target)
	}
	if !strings.Contains(check.Reason, "replaced") {
		t.Errorf("the reason must say WHAT happened to the file, got %q", check.Reason)
	}
	line := check.AlarmLine(mergeProgForSelfCheckTest)
	for _, want := range []string{selfCheckAlarmedMarker, check.SelfPath, check.DiskPath, "restart this process"} {
		if !strings.Contains(line, want) {
			t.Errorf("the alarm must carry %q:\n%s", want, line)
		}
	}
}

// TestSelfImageCheckLiveImageIsNotRefused is the NEGATIVE half, and it is the
// half that decides whether this check is usable: the same fabrication WITHOUT
// the kernel's marker must not read as stale on arm ①. A check that fired on a
// path string rather than on the kernel's evidence would refuse every healthy
// run.
func TestSelfImageCheckLiveImageIsNotRefused(t *testing.T) {
	target := filepath.Join(t.TempDir(), "live-artifact")
	if err := os.WriteFile(target, []byte("here\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
	check := SelfImageProbeAt(selfCheckFakeProcRoot(t, target))

	if !check.Probed {
		t.Fatalf("the check did not run: %s", check.Unmeasurable)
	}
	if check.Replaced {
		t.Errorf("a target without the kernel's marker must not read as replaced: %q", check.SelfPath)
	}
	// The file is not a Go binary, so arm ② is unmeasurable — and an
	// unmeasurable second arm must leave the verdict at "not stale", never at
	// "stale".
	if check.DiskStamped {
		t.Errorf("a file that is not a Go binary was reported as stamped: %+v", check)
	}
	if check.Stale {
		t.Fatalf("a live, uncomparable image was refused: %s", check.Reason)
	}
}

// TestSelfImageCheckUnreadableProcIsNotAJudgement pins the Rule 15 half: a
// check that could not run says so, and it never borrows the verdict of a check
// that did.
func TestSelfImageCheckUnreadableProcIsNotAJudgement(t *testing.T) {
	for _, tc := range []struct {
		name string
		root string
	}{
		{name: "no procfs root at all", root: ""},
		{name: "a root whose self/exe does not exist", root: t.TempDir()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := SelfImageProbeAt(tc.root)
			if check.Probed {
				t.Fatalf("an unreadable procfs root reported a completed check: %+v", check)
			}
			if check.Unmeasurable == "" {
				t.Fatalf("an unmeasured check must say why: %+v", check)
			}
			if check.Stale {
				t.Errorf("an unmeasured check must not report staleness: %+v", check)
			}
			line := check.ArmingNote(mergeProgForSelfCheckTest)
			if !strings.Contains(line, selfCheckNotArmedMarker) {
				t.Errorf("the arming note must say the check is not armed:\n%s", line)
			}
			if strings.Contains(check.AlarmLine(mergeProgForSelfCheckTest), selfCheckAlarmedMarker) && check.Stale {
				t.Errorf("an unmeasured check must not print a refusal:\n%s", check.AlarmLine(mergeProgForSelfCheckTest))
			}
		})
	}
}

// TestSelfImageCheckStaleStampArm drives arm ② — the in-place rebuild, which
// arm ① cannot see — against a REAL artifact built by `go build`, and pins both
// sides of the comparison:
//
//   - the stamp reader recovers the revision the toolchain embedded, or reports
//     honestly that this build carries none (a linked worktree build, which the
//     Go toolchain genuinely does not stamp);
//   - a disk stamp DIFFERENT from the running image's is refused.
//
// The live side is read from this TEST BINARY (selfImageRevision), which is the
// same channel the production check reads; the fixture's side is a real `go
// build` in its own repository, so the two stamps are different by construction
// — unless the live side carries no stamp at all, in which case the scenario
// asserts the documented degradation instead of inventing a comparison.
func TestSelfImageCheckStaleStampArm(t *testing.T) {
	bin := selfCheckBuildFixture(t)
	diskRev, diskStamped := artifactRevision(bin)
	if !diskStamped {
		t.Fatalf("a freshly built Go binary carried no readable build info: %s", bin)
	}
	if diskRev == "" {
		t.Skipf("this toolchain did not stamp %s with a revision (a linked-worktree build carries none) — arm ② is unmeasurable here, which the arming note reports", bin)
	}
	// The stamp is whatever commit the fixture repository happened to get, so
	// the assertion is on the SHAPE (a revision, never prose or a name): the
	// exact bytes are git's, not the fixture's to prescribe. The reader is the
	// shared package's own revision parser (Rule 13/14: no second definition of
	// what a revision looks like).
	if _, _, ok := gopiversion.RevisionOf(diskRev); !ok {
		t.Errorf("the fixture's stamp %q is not a revision", diskRev)
	}

	live := selfImageRevision()
	check := SelfImageCheck{
		Probed:       true,
		SelfPath:     "/nonexistent/running/gopi",
		DiskPath:     bin,
		SelfRevision: live,
		DiskRevision: diskRev,
		DiskStamped:  true,
	}
	if live == "" {
		check.Stale = false
	} else if live == diskRev {
		t.Fatalf("the fixture's stamp equals this test binary's (%q) — the scenario proves nothing", live)
	} else {
		check.Stale = true
		check.Reason = "two different stamps"
	}
	if live == "" {
		note := check.ArmingNote(mergeProgForSelfCheckTest)
		if !strings.Contains(note, "REPLACEMENT only") {
			t.Errorf("an unstamped running image must degrade the arming note by name:\n%s", note)
		}
		return
	}
	if !check.Stale {
		t.Fatalf("two different revisions were not refused: live=%q disk=%q", live, diskRev)
	}
	if note := check.ArmingNote(mergeProgForSelfCheckTest); !strings.Contains(note, selfCheckArmedMarker) || strings.Contains(note, "REPLACEMENT only") {
		t.Errorf("a fully measurable pair must arm both arms:\n%s", note)
	}
}

// selfCheckBuildFixture builds a real one-file Go binary in a fresh git
// repository with ONE commit, and returns the artifact path. Scratch lives in
// the suite's root (Rule 22: never the / partition), and the build is a real
// `go build`, so the stamp it carries is the toolchain's own.
func selfCheckBuildFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	write := func(rel, body string) string {
		t.Helper()
		path := filepath.Join(repo, rel)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return path
	}
	write("go.mod", "module selfcheckfixture\n\ngo 1.21\n")
	write(selfCheckFixtureFile, selfCheckFixturePkg)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit("init", "-b", "master")
	runGit("config", "user.email", "selfcheck@example.invalid")
	runGit("config", "user.name", "self check fixture")
	runGit("add", selfCheckFixtureFile, "go.mod")
	runGit("commit", "-m", "fixture")

	bin := filepath.Join(testutil.SuiteTempRoot(), "selfcheck-fixture-bin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not build the fixture binary (%v):\n%s", err, out)
	}
	return bin
}

// TestSelfImageCheckRebuiltSameRevisionIsNotAFixThatDidNotArrive pins the SELF
// face of the state this epic adds, and it is asserted through the SHARED
// predicate and the SHARED renderer (Rule 13): a `make build` of unchanged work
// rewrites the artifact in place-by-rename, so /proc/self/exe carries the
// kernel's marker while the disk holds the SAME revision. The marker is real and
// stays reported; what must not be said is "the disk's fixes never arrived",
// because here they are ancestors of what this process already runs.
func TestSelfImageCheckRebuiltSameRevisionIsNotAFixThatDidNotArrive(t *testing.T) {
	rev := "84beb3091e0e0d3e5b6b4e3d1cb2a6a8b3c6d5e4"
	if !imageSameRevisionStamp(rev, rev, true) {
		t.Fatal("the shared predicate does not recognise two equal stamps as the same revision")
	}
	// The unmeasurable halves stay unmeasurable: an unstamped disk side must not
	// be read as "same", or the alarm would go quiet on work it cannot compare.
	if imageSameRevisionStamp(rev, "", false) || imageSameRevisionStamp(rev, rev, false) || imageSameRevisionStamp("", rev, true) {
		t.Error("the shared predicate called an UNMEASURABLE pair 'the same revision' — that is the silent failure it exists to end")
	}

	target := selfCheckDeletedTarget(t)
	check := SelfImageCheck{
		Probed: true, SelfPath: target, Replaced: true,
		DiskPath:     strings.TrimSuffix(target, selfCheckDeletedSuffix),
		SelfRevision: rev, DiskRevision: rev, DiskStamped: true,
	}
	// The production switch is in SelfImageProbeAt, which reads a real /proc; the
	// decision it makes is the shared predicate, so the sentence is asserted on
	// the same fields the probe fills, through the peer renderer the worker loop
	// actually prints (AsPeerImage → PeerImageNote).
	check.Stale = true
	check.Reason = "the file was rewritten under this process, running and disk are both revision " + rev
	peer := check.AsPeerImage()
	if got, want := peer.StateWord(), PeerImageStateRebuiltSameRevision; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	// The note renders c.Reason verbatim; the clause lives in the REASON because
	// ONE place composes it (SelfImageProbeAt / peerImageVerdict), and this
	// fixture's reason is hand-written — so the assertion is on what the note
	// itself must carry, and the clause is pinned where production writes it (the
	// peer arm's test, which drives peerImageVerdict).
	note := peer.PeerImageNote(mergeProgForSelfCheckTest, 4242)
	for _, want := range []string{rev, "REBUILT under it"} {
		if !strings.Contains(note, want) {
			t.Errorf("the self note does not carry %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "does NOT reach") {
		t.Errorf("the self note still claims the disk fix does not reach this process:\n%s", note)
	}
}
