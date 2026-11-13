package main

// readppid_parenthesised_comm_test.go — the value-level guard for the
// /proc/<pid>/stat parent read that TestProcStatParseSingleHomeDetector caught
// on master (2026-09-19).
//
// WHY the single-home gate is not enough on its own: that gate is a SOURCE
// shape detector — it fails when the `LastIndex(')')` + `Fields` parse is
// re-inlined, and it says nothing about whether the delegating call reads the
// RIGHT field. Measured while fixing it: the hand-rolled copy it replaced was
// behaviourally EQUIVALENT (it also took the fields after the last ')' and its
// index landed on field 4), so this is a Rule 13/14 single-home violation, not
// a wrong-value bug — stated precisely so nobody later "fixes" a behaviour
// that was never broken. What the delegation must preserve is therefore the
// VALUE: the parent (field 4) of a comm that itself contains spaces and
// parentheses, which is the case the parse's last-')' rule exists for.
//
// A regression to a local parse that mis-numbers the fields fails here as well
// as at the single-home gate.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadPPIDHandlesAParenthesisedComm pins the value — not merely that the
// parse "runs". The fixture is a fake procfs root, so the assertion is exact
// and needs no live process.
func TestReadPPIDHandlesAParenthesisedComm(t *testing.T) {
	root := t.TempDir()
	// `pid (comm) state ppid …` with the two shapes that defeat a naive split:
	// spaces inside comm, and a ')' inside comm.
	const stat = "4242 (gopi worker (x)) S 31337 4242 4242 0 -1 4194560\n"
	const pid = "4242"
	if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
		t.Fatalf("mkdir fake procfs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, pid, procStatFileName), []byte(stat), 0o644); err != nil {
		t.Fatalf("write fake stat: %v", err)
	}

	got := readPPIDAt(root, pid)
	if got != 31337 {
		t.Fatalf("readPPID = %d, want 31337 (field 4 of %q) — a parenthesised comm shifted the fields", got, stat)
	}
}

// TestReadPPIDUnknownStatesAreZero pins the two failure shapes as 0 ("unknown"
// for this caller), never a fabricated parent: an unreadable file, and a stat
// line with no parenthesis at all.
func TestReadPPIDUnknownStatesAreZero(t *testing.T) {
	root := t.TempDir()
	const pid = "4343"
	if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
		t.Fatalf("mkdir fake procfs: %v", err)
	}

	// No stat file at all.
	if got := readPPIDAt(root, pid); got != 0 {
		t.Errorf("readPPID with no stat file = %d, want 0", got)
	}

	// A stat file with no parenthesis: StatField reports "cannot answer".
	if err := os.WriteFile(filepath.Join(root, pid, procStatFileName), []byte("garbage without parens\n"), 0o644); err != nil {
		t.Fatalf("write fake stat: %v", err)
	}
	if got := readPPIDAt(root, pid); got != 0 {
		t.Errorf("readPPID from an unparseable stat line = %d, want 0 (never a fabricated parent)", got)
	}
}
