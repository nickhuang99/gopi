package tools

// THE ENOENT THIS PINS (measured 2026-10-16, task 01a143696d247e66abe1dda74a3fc9fd,
// host host-a; filed 2026-10-17 as task 01a1453a954871d584bf6d8a9c13d7e5).
//
// A live run's own session directory appeared to vanish mid-run: `ls -la` and
// the file tools returned ENOENT for
// /home/user/.gopi/sessions/01a14369-6d24-7e66-abe1-dda74a3fc9fd, while the
// very same directory resolved seconds later — with every artifact's mtime
// untouched. The incident looked like a filesystem mystery (a directory that
// stops resolving and comes back). It was a SPELLING: the failing call carried
// 35 characters, not 36 — the FOURTH dash of the canonical uuid grouping was
// missing:
//
//	on disk   01a14369-6d24-7e66-abe1-dda74a3fc9fd   36 chars, dashes at 8/13/18/23
//	failed    01a14369-6d24-7e66-abe1dda74a3fc9fd   35 chars, dash at 23 dropped
//
// The two spellings are distinct path elements, so the failure is a plain
// "no such entry" — deterministic, permanent, and repeatable — not a transient
// naming-layer fault. The recovery is the REFUSAL internal/sessionid already
// ships (CanonicalSpellingOf), which is why the same run's later call, spelled
// canonically, resolved: not because the directory came back, but because a
// different name was asked for.
//
// WHY IT IS A TEST AND NOT PROSE: the mechanism has to be checkable without a
// live run, a device, or a race — Rule 27 forbids manufacturing load to make a
// transient reproduce, and this test needs none: it builds the canonical
// container, then measures that the near-miss spelling is ENOENT *beside* it,
// in the same instant, on the same filesystem.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionid"
)

// nearMissDropIndex is the position of the FOURTH dash in a canonical uuid
// (8/13/18/23) — the one the measured failure dropped. It is a named constant
// so the fixture says which spelling it reproduces (Rule 1).
const nearMissDropIndex = 23

// taskSessionNearMissCanonicalID is a canonical task id for the fixture. It is
// the MEASURED one (the row whose run filed the bug), so the test's name and
// the incident read as the same object.
const taskSessionNearMissCanonicalID = "01a14369-6d24-7e66-abe1-dda74a3fc9fd"

// TestNearMissSessionSpellingIsENOENTBesideTheCanonicalContainer is the repro:
// it proves that the spelling WITHOUT its fourth dash names nothing at all,
// while the canonical spelling names a directory that is present in the same
// instant — i.e. the ENOENT is produced by the operand, not by the filesystem.
func TestNearMissSessionSpellingIsENOENTBesideTheCanonicalContainer(t *testing.T) {
	phase1AttachEnv(t)

	// ① The canonical container — exactly what the harness mints and what
	// $GOPI_TASK_REPORT_PATH names.
	dir, err := taskSessionDir(taskSessionNearMissCanonicalID)
	if err != nil {
		t.Fatalf("canonical id must be admissible: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir canonical container: %v", err)
	}
	report := filepath.Join(dir, "task-report.md")
	if err := os.WriteFile(report, []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("write fixture report: %v", err)
	}

	// ② The near-miss spelling: the same 16 bytes, one dash dropped at the
	// measured position.
	canonical := filepath.Base(dir)
	if len(canonical) != 36 || canonical[nearMissDropIndex] != '-' {
		t.Fatalf("fixture premise broken: %q is not a canonical 36-char uuid with a dash at %d", canonical, nearMissDropIndex)
	}
	nearMiss := canonical[:nearMissDropIndex] + canonical[nearMissDropIndex+1:]
	if got, want := len(canonical)-len(nearMiss), 1; got != want {
		t.Fatalf("near-miss fixture must drop exactly %d character (dropped %d)", want, got)
	}
	nearMissReport := filepath.Join(filepath.Dir(dir), nearMiss, "task-report.md")

	// ③ MEASURE, in one instant: canonical resolves, near-miss does not.
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("the canonical report path must resolve — the whole point is that it never left: %v", err)
	}
	_, statErr := os.Stat(nearMissReport)
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the near-miss spelling must be ENOENT (that IS the incident); got err=%v", statErr)
	}

	// ④ The measured failure came from a path the tree already knows how to
	// repair: the refusal names the canonical spelling back. Asserting it here
	// keeps the test tied to the ONE rule rather than to a second uuid regex.
	repaired, ok := sessionid.CanonicalSpellingOf(nearMiss)
	if !ok {
		t.Fatalf("the near-miss %q must be recognised as a uuid's 16 bytes in a non-canonical spelling", nearMiss)
	}
	if repaired != canonical {
		t.Fatalf("the repair must name the row's own spelling: got %q want %q", repaired, canonical)
	}

	// ⑤ And the seam that IS given the near-miss refuses it instead of minting
	// a SECOND container beside the row's own — the behaviour that turns the
	// incident into a loud failure rather than a silent second directory.
	if _, err := taskSessionDir(nearMiss); err == nil {
		t.Fatalf("a near-miss spelling must be REFUSED by the session seam, not joined into a second container")
	} else if !strings.Contains(err.Error(), canonical) {
		t.Fatalf("the refusal must name the canonical repair %q; got: %v", canonical, err)
	}
	// Nothing was created under the near-miss name by that refusal.
	if _, err := os.Stat(filepath.Dir(nearMissReport)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused spelling must leave no directory behind; got err=%v", err)
	}
}
