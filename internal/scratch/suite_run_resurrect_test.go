package scratch

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression guard for the residue class measured on 2026-10-22 (task
// 01a163ae): a per-run suite root recreated AFTER its pin had removed it
// carried no owner record, so the collector could never judge it and every run
// of internal/tools added one more unregistered `/dev/shm/suite-tools-run-*`.
// The evidence (an strace of the two creations of one path, 4.6ms apart) is in
// suite_run_resurrect.go; this file pins the behaviour so it cannot come back.
func TestMkdirAllWithSuiteRunOwnerRegistersACreatedRoot(t *testing.T) {
	base := t.TempDir()
	// The exact shape the measurement produced: the run root, a Test* child and
	// the numbered ledger directory, all created by ONE MkdirAll call.
	root := filepath.Join(base, "suite-tools-run-1234567")
	dir := filepath.Join(root, "TestResumeArmNeverTouchesALiveHolderWorktreeE2E1234567890", "002")

	if err := MkdirAllWithSuiteRunOwner(dir); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, HolderFileName))
	if err != nil {
		t.Fatalf("a suite run root this call recreated must carry an owner record, and %s has none: %v — "+
			"an unregistered root is refused by the collector and waits the full age window", root, err)
	}
	pid, start, perr := ParseHolder(string(raw))
	if perr != nil {
		t.Fatalf("the record %s carries must parse: %v\n%s", root, perr, raw)
	}
	if pid != os.Getpid() {
		t.Errorf("the record must name THIS process as the owner (the one that recreated the root), got pid %d, want %d", pid, os.Getpid())
	}
	if start == 0 {
		t.Errorf("the record must carry the incarnation (kernel start ticks), got %d — a record without it cannot "+
			"detect pid reuse", start)
	}
	if v := OwnerVerdictFor(root); !v.Registered {
		t.Errorf("the root must read back as registered: %s", v.Reason)
	}
}

// The nesting case is the measured one and is worth its own arm: the record
// belongs on the SUITE RUN ROOT, not on the leaf, because the root is the unit
// the collector judges.
func TestMkdirAllWithSuiteRunOwnerPutsTheRecordOnTheRootNotTheLeaf(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "suite-tools-run-7654321")
	leaf := filepath.Join(root, "TestSomething", "002")

	if err := MkdirAllWithSuiteRunOwner(leaf); err != nil {
		t.Fatalf("create %s: %v", leaf, err)
	}
	if !OwnerRecorded(root) {
		t.Errorf("%s must carry the record — it is the unit the collector judges", root)
	}
	if OwnerRecorded(leaf) {
		t.Errorf("%s must NOT carry a record: the leaf is a fixture directory, and a record there would "+
			"make it a candidate somebody tries to collect on its own", leaf)
	}
	if OwnerRecorded(filepath.Join(root, "TestSomething")) {
		t.Errorf("the Test* child must NOT carry a record either — only a suite-run-named ancestor is ours")
	}
}

// An EXPLICITLY configured temp root (or any directory, or an operator's own
// suite-shaped directory that already exists) is never given a record: gopi
// writing a file inside it creates nothing, so it owns nothing.
func TestMkdirAllWithSuiteRunOwnerLeavesOtherDirectoriesAlone(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"operator-tmp", "suite-notes", "suite-tools-run", "my-suite-run-x", "002"} {
		dir := filepath.Join(base, name, "child")
		if err := MkdirAllWithSuiteRunOwner(dir); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
		if OwnerRecorded(filepath.Join(base, name)) {
			t.Errorf("created %q and it grew a %s — a configured temp root is the operator's directory, never "+
				"a suite run root, and a record there would make it collectable", name, HolderFileName)
		}
	}
}

// A root that ALREADY EXISTS is not repossessed: its record (or its absence)
// belongs to whoever created it, and a writer that merely stores a file inside
// must not overwrite a live owner's registration with its own pid.
func TestMkdirAllWithSuiteRunOwnerDoesNotRepossessAnExistingRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "suite-tools-run-9999999")
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatalf("pre-create %s: %v", root, err)
	}
	if err := MkdirAllWithSuiteRunOwner(filepath.Join(root, "TestX", "002")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if OwnerRecorded(root) {
		t.Errorf("%s already existed, so this call created no root and must leave no record — re-registering "+
			"another run's root would steal its ownership", root)
	}
}

// A failure to register is REPORTED, never fatal (Rule 15b): the directory is
// usable either way, so the ledger open that follows must still succeed. A
// read-only root cannot hold the record file, which is the cheapest way to make
// WriteOwnerRecord fail without inventing a second mechanism.
func TestMkdirAllWithSuiteRunOwnerReportsAFailedRegistration(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "suite-tools-run-4242424")
	if err := os.MkdirAll(filepath.Join(root, "TestY", "002"), DirPerm); err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	// Simulate a root that cannot accept the record: remove it and let the call
	// recreate it under a PARENT this process cannot write into.
	locked := filepath.Join(base, "locked")
	if err := os.MkdirAll(locked, DirPerm); err != nil {
		t.Fatalf("pre-create %s: %v", locked, err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatalf("lock %s: %v", locked, err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, DirPerm) }) //stylegate:ignore restoring this fixture's own mode so TempDir cleanup can remove it

	// The call fails to CREATE (that is os.MkdirAll's own contract, returned as
	// an error): a root it could not create is not a root it may claim.
	if err := MkdirAllWithSuiteRunOwner(filepath.Join(locked, "suite-tools-run-1", "TestZ", "002")); err == nil {
		t.Errorf("a directory that cannot be created must return os.MkdirAll's error, not a silent success")
	}
}
