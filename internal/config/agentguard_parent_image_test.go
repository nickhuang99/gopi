package config

// The fourth judgement site of bug 01a131fcb8337c1b: parentIsWorkerGopi answered
// "is my parent a `gopi worker` process?" with comm == NameGopi, so a worker
// started from a gopi binary BUILT UNDER ANOTHER FILE NAME (bin/gopi-fix, a
// per-task build) was not recognized as a worker parent — the sanctioned
// worker→`task run` chain was then treated as an unsanctioned one.
//
// The predicate now reads the parent's IMAGE through the leaf package
// (procname.IsGopiImage: /proc/<ppid>/exe, then the parent's own argv[0], comm
// only as the weakest fallback). The fixture therefore has to state the parent's
// /proc/<ppid>/exe link, which is what the new test below drives — and the OLD
// behaviour is asserted on the SAME fixture, so this test fails on the old code
// rather than merely passing on the new.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

const (
	// guardFixturePpid is the parent pid the fixture names. It is not a real
	// process: /proc/self/stat is the fixture's own, so the chain read resolves
	// every path under the fixture root.
	guardFixturePpid = "4001"
	// guardFixingImage is a gopi binary built under another file name — the
	// measured shape of the bug (/tmp/gopi-fix).
	guardFixingImage = procname.ImageGopiPrefix + "-fix"
	// guardForeignImage is a program that is NOT gopi.
	guardForeignImage = "nginx"
	// guardImageDir is the synthetic directory the fixture's exe links point into.
	guardImageDir = "/synthetic/guard-fixture"
	// guardSelfStatFiller is how many stat fields the fixture writes after the
	// parenthesised comm, so field 4 (the parent pid) really is field 4.
	guardSelfStatFiller = 3
)

// guardProcFixture materialises a procfs root holding SELF's stat (naming
// guardFixturePpid as the parent) and the parent's comm/cmdline/exe.
func guardProcFixture(t *testing.T, parentComm string, parentArgv []string, parentImage string) string {
	t.Helper()
	root := t.TempDir()
	selfDir := filepath.Join(root, "self")
	if err := os.MkdirAll(selfDir, 0o755); err != nil {
		t.Fatalf("mkdir self: %v", err)
	}
	// /proc/self/stat: "<pid> (<comm>) <state> <ppid> ..." — the reader counts the
	// parent field from the LAST ')' (comm may hold spaces and parens).
	selfStat := "7 (gopi guard self) S " + guardFixturePpid
	for i := 0; i < guardSelfStatFiller; i++ {
		selfStat += " 0"
	}
	if err := os.WriteFile(filepath.Join(selfDir, procname.StatName), []byte(selfStat+"\n"), 0o644); err != nil {
		t.Fatalf("write self stat: %v", err)
	}

	parentDir := filepath.Join(root, guardFixturePpid)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, procname.CommName), []byte(parentComm+"\n"), 0o644); err != nil {
		t.Fatalf("write parent comm: %v", err)
	}
	cmdline := ""
	for _, a := range parentArgv {
		cmdline += a + "\x00"
	}
	if err := os.WriteFile(filepath.Join(parentDir, procname.CmdlineName), []byte(cmdline), 0o644); err != nil {
		t.Fatalf("write parent cmdline: %v", err)
	}
	if parentImage != "" {
		target := filepath.Join(parentDir, "image")
		if err := os.WriteFile(target, nil, 0o644); err != nil {
			t.Fatalf("write image target: %v", err)
		}
		if err := os.Symlink(guardImageDir+"/"+parentImage, filepath.Join(parentDir, procname.ExeName)); err != nil {
			t.Fatalf("plant parent %s link: %v", procname.ExeName, err)
		}
	}
	return root
}

// withGuardProcRoot points the guard's procfs root at a fixture for the duration
// of one test. The variable defaults to the shared constant and is never written
// in production; this is the seam that makes the parent-chain predicate testable
// over a fixture instead of over whatever this host happens to be running.
func withGuardProcRoot(t *testing.T, root string) {
	t.Helper()
	prev := procFSRoot
	procFSRoot = root
	t.Cleanup(func() { procFSRoot = prev })
}

// TestParentIsWorkerGopiAcceptsADifferentlyBuiltGopiWorker is the ACCEPT side:
// a worker parent whose build carries another file name must still be recognized
// — by its image, and by its own argv[0] when the image cannot be read.
func TestParentIsWorkerGopiAcceptsADifferentlyBuiltGopiWorker(t *testing.T) {
	argv := []string{"/tmp/build/bin/" + guardFixingImage, procname.VerbWorker}
	cases := []struct {
		name   string
		comm   string
		image  string
		reason string
	}{
		{"image names the build", guardFixingImage, guardFixingImage,
			"the parent RUNS the gopi program under another file name"},
		{"image unreadable, argv[0] answers", guardFixingImage, "",
			"the exe link could not be read, so the parent's own argv[0] is the channel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withGuardProcRoot(t, guardProcFixture(t, c.comm, argv, c.image))
			if !parentIsWorkerGopi() {
				t.Errorf("the parent was NOT recognized as a %s worker (%s): its comm is %q while the gopi "+
					"program it runs is %q — refusing it makes the sanctioned worker→`%s run` chain look "+
					"unsanctioned (bug 01a131fcb8337c1b)", procname.ImageGopiPrefix, c.reason, c.comm, c.image, procname.VerbTask)
			}
		})
	}
}

// TestParentIsWorkerGopiRejectsAForeignImage is the REJECT side: argv[1] == the
// worker verb is not evidence on its own, and comm is not the channel any more.
func TestParentIsWorkerGopiRejectsAForeignImage(t *testing.T) {
	cases := []struct {
		name   string
		comm   string
		argv   []string
		image  string
		reason string
	}{
		{"foreign image, worker argv", guardFixingImage,
			[]string{"/usr/sbin/" + guardForeignImage, procname.VerbWorker}, guardForeignImage,
			"a comm that LOOKS like a gopi name says nothing about the program the process runs"},
		{"gopi image, non-worker verb", guardFixingImage,
			[]string{"/tmp/build/bin/" + guardFixingImage, procname.VerbTask}, guardFixingImage,
			"a `task` invocation of the same binary is not a worker parent"},
		{"gopi image, no verb", guardFixingImage,
			[]string{"/tmp/build/bin/" + guardFixingImage}, guardFixingImage,
			"an operator's own one-word invocation is not a worker parent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withGuardProcRoot(t, guardProcFixture(t, c.comm, c.argv, c.image))
			if parentIsWorkerGopi() {
				t.Errorf("the parent was taken for a %s worker (%s): image %q, argv %v", procname.ImageGopiPrefix,
					c.reason, c.image, c.argv)
			}
		})
	}
}

// TestParentIsWorkerGopiFailsOpenOnAnUnreadableParent pins the fail-open
// contract: nothing readable means no refusal is invented, never a crash and
// never a panic on a half-written /proc entry.
func TestParentIsWorkerGopiFailsOpenOnAnUnreadableParent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatalf("mkdir self: %v", err)
	}
	// A self stat whose parent field points at a pid that has no directory.
	if err := os.WriteFile(filepath.Join(root, "self", procname.StatName),
		[]byte("7 (self) S "+strconv.Itoa(999999)+"\n"), 0o644); err != nil {
		t.Fatalf("write self stat: %v", err)
	}
	withGuardProcRoot(t, root)
	if parentIsWorkerGopi() {
		t.Error("a parent with no /proc entry must not be reported as a worker (fail-open, never a fabricated yes)")
	}
}
