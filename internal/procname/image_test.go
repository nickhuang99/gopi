package procname

// The IMAGE judgement's minimal repro (bug 01a131fcb8337c1b, measured
// 2026-10-13): four judgement sites asked "is this process the gopi program?"
// and answered comm == NameGopi. comm is the basename of the executable FILE, so
// a gopi binary built under another file name (bin/gopi-fix, bin/gopi-test, a
// per-task build) was reported as "not a gopi process" while it ran this very
// program — a fabricated absence of a live worker.
//
// BOTH SIDES are pinned here, because a predicate that only accepts is as broken
// as one that only rejects (Rule 15: a proxy metric may never be spent as the
// measurement, in either direction):
//
//	ACCEPT — an image named gopi-fix (a differently-built gopi), the same via the
//	         argv[0] fallback when /proc/<pid>/exe cannot be read, and the
//	         kernel's " (deleted)" spelling of a live image.
//	REJECT — argv[1] == worker behind a FOREIGN image (the shape a process can
//	         wear without being our program), an image that is not gopi at all,
//	         and the reverse of the bug (comm says gopi while the image says
//	         something else — comm is no longer the channel, so this must not be
//	         counted as ours either).

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	// imageForeignProgram is a program that is NOT gopi, used as the reject
	// side's image.
	imageForeignProgram = "nginx"
	// imageFixName is the differently-built gopi the accept side uses — the
	// exact file name from the measured bug (/tmp/gopi-fix).
	imageFixName = "gopi-fix"
	// imageDeletedSuffix is how the kernel spells a live image whose file was
	// unlinked. It is compared against the package's own constant so a drift in
	// either spelling reddens this test rather than silently passing.
	imageDeletedSpelling = " (deleted)"
)

// imageFixture writes one synthetic /proc/<pid>/ directory: a comm, a
// NUL-separated cmdline, and (when exeTarget is non-empty) the /proc/<pid>/exe
// LINK the image judgement reads first. An empty exeTarget is the
// unreadable-image shape, which must fall back to argv[0].
func imageFixture(t *testing.T, root, pid, comm string, argv []string, exeTarget string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, CommName), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatalf("write comm: %v", err)
	}
	cmdline := ""
	if len(argv) > 0 {
		cmdline = joinNUL(argv)
	}
	if err := os.WriteFile(filepath.Join(dir, CmdlineName), []byte(cmdline), 0o644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}
	if exeTarget == "" {
		return
	}
	// A real file to point at, so Readlink answers exactly what the fixture
	// declares (a dangling link is a different shape: the kernel still reports
	// the TARGET of a link whose file is gone, which the deleted case below
	// spells explicitly).
	target := filepath.Join(dir, "image")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatalf("write image target: %v", err)
	}
	if err := os.Symlink(exeTarget, filepath.Join(dir, ExeName)); err != nil {
		t.Fatalf("plant %s link: %v", ExeName, err)
	}
}

// joinNUL renders an argv vector the way procfs does (NUL-separated, trailing
// NUL).
func joinNUL(argv []string) string {
	out := ""
	for _, a := range argv {
		out += a + "\x00"
	}
	return out
}

// TestWorkerShapeAcceptsADifferentlyBuiltGopiWorker is the ACCEPT side of the
// bug's minimal repro: a worker of a gopi binary built under another file name
// must be recognized — by its image, and by its own argv[0] when the image link
// cannot be read.
func TestWorkerShapeAcceptsADifferentlyBuiltGopiWorker(t *testing.T) {
	root := t.TempDir()
	workerArgv := []string{"/tmp/bin/" + imageFixName, VerbWorker}
	// 2001: the image link answers (the kernel's channel).
	imageFixture(t, root, "2001", imageFixName, workerArgv, "/tmp/bin/"+imageFixName)
	// 2002: NO link and no comm agreement — the process's OWN argv[0] is the
	// remaining channel, and it names the differently-built gopi.
	imageFixture(t, root, "2002", imageFixName, workerArgv, "")
	// 2003: the kernel's " (deleted)" spelling of an unlinked live image.
	imageFixture(t, root, "2003", imageFixName, workerArgv, "/tmp/bin/"+imageFixName+DeletedImageMarker)

	for _, pid := range []string{"2001", "2002", "2003"} {
		comm, _ := os.ReadFile(filepath.Join(root, pid, CommName)) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
		argv, ok := Cmdline(root, pid)
		if !ok {
			t.Fatalf("pid %s: fixture cmdline unreadable", pid)
		}
		if !WorkerShape(root, pid, string(comm), argv) {
			t.Errorf("pid %s (image %q, argv[1] %q) was NOT taken for a %s worker — a gopi binary built "+
				"under another file name runs this very program, and refusing it is the fabricated absence "+
				"of the measured bug 01a131fcb8337c1b", pid, ImageName(root, pid, argv[0]), VerbWorker, ImageGopiPrefix)
		}
	}
	// POSITIVE CONTROL on the same fixture: the judgement is not a constant
	// true. A worker argv behind a FOREIGN image is refused.
	imageFixture(t, root, "2004", imageForeignProgram, []string{"/usr/sbin/" + imageForeignProgram, VerbWorker}, "/usr/sbin/"+imageForeignProgram)
	argv, _ := Cmdline(root, "2004")
	if WorkerShape(root, "2004", imageForeignProgram, argv) {
		t.Errorf("pid 2004 runs %q with argv[1] == %q and was taken for a %s worker — the image test is "+
			"not being applied (any process can be given that argv)", imageForeignProgram, VerbWorker, ImageGopiPrefix)
	}
}

// TestWorkerShapeRejectsAForeignImageWearingTheWorkerArgv is the REJECT side,
// stated as the property the predicate exists for: argv[1] == the worker verb is
// NOT evidence of anything on its own.
func TestWorkerShapeRejectsAForeignImageWearingTheWorkerArgv(t *testing.T) {
	root := t.TempDir()
	// 2101: THE bug's reverse — comm says gopi, the IMAGE says another program.
	// comm is no longer the channel, so this must NOT be counted as ours.
	imageFixture(t, root, "2101", ImageGopiPrefix, []string{"/usr/sbin/" + imageForeignProgram, VerbWorker}, "/usr/sbin/"+imageForeignProgram)
	// 2102: argv[1] is the worker verb but argv[0] names a foreign program, and
	// no image link can be read (the fallback is the only channel).
	imageFixture(t, root, "2102", imageForeignProgram, []string{"/usr/sbin/" + imageForeignProgram, VerbWorker}, "")
	// 2103: our program, but NOT the worker verb (a `task serve` daemon).
	imageFixture(t, root, "2103", imageFixName, []string{"/tmp/bin/" + imageFixName, VerbTask}, "/tmp/bin/"+imageFixName)
	// 2104: our program with no argv[1] at all (an operator's own invocation).
	imageFixture(t, root, "2104", imageFixName, []string{"/tmp/bin/" + imageFixName}, "/tmp/bin/"+imageFixName)

	for _, pid := range []string{"2101", "2102", "2103", "2104"} {
		comm, _ := os.ReadFile(filepath.Join(root, pid, CommName)) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
		argv, _ := Cmdline(root, pid)
		if WorkerShape(root, pid, string(comm), argv) {
			t.Errorf("pid %s (image %q, argv %v) was taken for a %s worker — either the image is not this "+
				"program or argv[1] is not the worker verb; accepting it is the same proxy substitution in "+
				"the other direction", pid, ImageName(root, pid, argv[0]), argv, ImageGopiPrefix)
		}
	}
}

// TestImageNameChannelsAndFallback pins WHICH channel answers, so a caller that
// reports its evidence can say which one it used, and so the fallback order
// cannot silently invert.
func TestImageNameChannelsAndFallback(t *testing.T) {
	root := t.TempDir()
	imageFixture(t, root, "2201", imageFixName, []string{"/elsewhere/" + imageFixName, VerbWorker}, "/tmp/bin/"+imageFixName)
	if got := ImageName(root, "2201", "/elsewhere/"+imageFixName); got != imageFixName {
		t.Errorf("ImageName = %q, want %q — the %s link is the kernel's own answer and is read first", got, imageFixName, ExeName)
	}
	// No link: argv[0]'s BASENAME is the answer (argv[0] is a path).
	imageFixture(t, root, "2202", imageForeignProgram, []string{"/opt/x/" + imageFixName, VerbWorker}, "")
	if got := ImageName(root, "2202", "/opt/x/"+imageFixName); got != imageFixName {
		t.Errorf("ImageName with no %s link = %q, want %q — the process's own argv[0] is the fallback, "+
			"read as a path and reduced to its base name", ExeName, got, imageFixName)
	}
	// Neither channel: "" is a MISSING measurement, never a name.
	imageFixture(t, root, "2203", "", nil, "")
	if got := ImageName(root, "2203", ""); got != "" {
		t.Errorf("ImageName with no link and no argv[0] = %q, want the empty string — a missing measurement "+
			"must be distinguishable from a name (Rule 15a)", got)
	}
	// The comm channel is LAST and is REPORTED as such: only an empty image
	// reaches it, and the caller learns that the acceptance rests on it.
	isGopi, fromComm := IsGopiImage(root, "2203", "", imageFixName)
	if !isGopi || !fromComm {
		t.Errorf("IsGopiImage with no readable image and comm %q = (%v, %v), want (true, true) — comm is the "+
			"kernel's truncation of the SAME image file, so it is evidence, and the second value must say "+
			"the weaker channel was used (Rule 15: an acceptance states its evidence)", imageFixName, isGopi, fromComm)
	}
	if isGopi, fromComm := IsGopiImage(root, "2203", "", ""); isGopi || fromComm {
		t.Errorf("IsGopiImage with NO channel at all = (%v, %v), want (false, false) — nothing was measured", isGopi, fromComm)
	}
}

// TestImageGopiPrefixIsAPrefixNotAnEquality records the semantics the four
// judgement sites now share, so it is a decision on the record rather than an
// accident: an image named gopi-<anything> IS this program (that is the bug's
// fix), which also means a program whose name merely BEGINS with the prefix is
// accepted. The alternative — an equality, or an allow-list of exact names —
// is what produced the false absence; a caller that needs to separate two gopi
// builds compares the full image name it read (ImageName returns it).
func TestImageGopiPrefixIsAPrefixNotAnEquality(t *testing.T) {
	for _, name := range []string{ImageGopiPrefix, "gopi-fix", "gopi-test", "gopi-01a131fc"} {
		if !hasImagePrefix(name) {
			t.Errorf("%q must be recognized as the gopi program (prefix %q)", name, ImageGopiPrefix)
		}
	}
	for _, name := range []string{"", "gop", "worker", "nginx"} {
		if hasImagePrefix(name) {
			t.Errorf("%q must NOT be recognized as the gopi program (prefix %q)", name, ImageGopiPrefix)
		}
	}
}

// hasImagePrefix is the fixture's own spelling of the prefix test, kept
// independent of IsGopiImage so the assertion is about the CONSTANT's value
// rather than about the function under test.
func hasImagePrefix(name string) bool {
	return len(name) >= len(ImageGopiPrefix) && name[:len(ImageGopiPrefix)] == ImageGopiPrefix
}

// TestDeletedImageMarkerMatchesTheKernelSpelling pins the constant against the
// spelling this test file declares independently: the marker is part of a
// /proc/<pid>/exe target, and a drift here would make every unlinked-image
// worker invisible again.
func TestDeletedImageMarkerMatchesTheKernelSpelling(t *testing.T) {
	if DeletedImageMarker != imageDeletedSpelling {
		t.Fatalf("DeletedImageMarker = %q, want %q — the kernel appends this to %s's target once the image "+
			"file is unlinked, and a process running a deleted gopi image is still running gopi",
			DeletedImageMarker, imageDeletedSpelling, ExeName)
	}
	if ExeName != "exe" {
		t.Fatalf("ExeName = %q, want %q — it is the procfs entry holding the executable link", ExeName, "exe")
	}
}

// TestWorkerImageIsGopiIsTheNoCommAdapter pins the CLI adapter's contract: it
// answers from the image and argv[0] alone, and an unreadable image with no
// argv[0] is NOT a pass (the empty comm it passes cannot manufacture evidence).
func TestWorkerImageIsGopiIsTheNoCommAdapter(t *testing.T) {
	root := t.TempDir()
	imageFixture(t, root, "2301", imageFixName, []string{"/tmp/bin/" + imageFixName, VerbWorker}, "/tmp/bin/"+imageFixName)
	if !WorkerImageIsGopi(root, "2301", "/tmp/bin/"+imageFixName) {
		t.Error("the adapter must accept a gopi image")
	}
	imageFixture(t, root, "2302", imageFixName, nil, "")
	if WorkerImageIsGopi(root, "2302", "") {
		t.Errorf("the adapter passed a pid with no readable image and no argv[0] — it supplies no comm, so "+
			"there is no channel left, and a pass here would be a fabricated identification (pid %s)", strconv.Itoa(2302))
	}
}
