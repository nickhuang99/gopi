package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionid"
)

// The ONE RUN, TWO DIRECTORIES defect, in the shape it was measured on this
// host (2026-10-17, task 01a1452c). The row id and the container minted beside
// it differ by ONE hexadecimal digit:
//
//   row / canonical spelling  01a1452c-928d-7e0f-9ef3-52c1482cfbc3  (32 hex)
//   the sibling container     01a1452c-928d-7e0f9-9ef3-52c1482cfbc3  (33 hex)
//
// The sibling holds a DIFFERENT 16 bytes — it names no row at all (the ledger
// answers "no task with task_id" for it) — yet the session-dir seam joined it
// silently, so a run reading its own evidence back under the canonical name
// found nothing and reported files that "vanished from a live session
// directory". The seam is the ONLY place that turns an id into a path element
// (internal/sessionid's rule, applied by taskSessionDir), so it is the one
// place the refusal belongs.
const (
	extraDigitRowID     = "01a1452c-928d-7e0f-9ef3-52c1482cfbc3"
	extraDigitSiblingID = "01a1452c-928d-7e0f9-9ef3-52c1482cfbc3" // 33 hex — the digit-too-many arm
	// extraDigitDashMoveID is the THIRD spelling measured beside the row on this
	// host: the dashes moved so that a group of ONE stands alone. Its hex run is
	// 32 characters like every canonical id, so it belongs to the GROUPING arm
	// (whose repair IS determined by the operand) and is pinned separately below.
	extraDigitDashMoveID  = "01a1452c-928d-7e0f-9-ef3-52c1482cfbc3"
	extraDigitAnonymousID = "01a1452c928d7e0f9ef352c1482cfbc3a" // 33 hex, no row, NOT any row ± one digit
)

// TestTaskSessionDirRefusesAnExtraHexDigit is the minimal repro of the class
// the shape gate missed: the operand's hex run is 33 characters, so the
// gate's single length test (32) let it through and the seam minted a second
// container. RED before the fix (the join succeeds and a directory exists),
// GREEN after (refused by name, and nothing on disk).
func TestTaskSessionDirRefusesAnExtraHexDigit(t *testing.T) {
	home := covLifeSessionEnv(t)

	for _, id := range []string{extraDigitSiblingID} {
		sd, err := TaskSessionDir(id)
		if err == nil {
			t.Fatalf("TaskSessionDir(%q) = (%q, nil) — a uuid's bytes with a digit too many must be REFUSED, not joined into a second session directory beside the row's own", id, sd)
		}
		if !strings.Contains(err.Error(), "DIGIT TOO MANY") {
			t.Errorf("TaskSessionDir(%q) refusal = %q, want it to name the extra-digit shape", id, err)
		}
		// No "did you mean" is expected here, and that is deliberate: the digit
		// to delete is not derivable from the operand (measured — the one
		// deletion that parses yields a uuid that names no row). A refusal that
		// printed a guessed canonical id would be handing the caller a WRONG
		// repair, so this test pins its ABSENCE.
		if strings.Contains(err.Error(), "did you mean") {
			t.Errorf("TaskSessionDir(%q) refusal = %q — it invents a canonical spelling for a repair the operand does not determine", id, err)
		}
		joined := filepath.Join(home, ".gopi", config.SessionsDirName, id)
		if _, statErr := os.Stat(joined); !os.IsNotExist(statErr) {
			t.Errorf("the refusal left %q on disk (stat err = %v) — refusing must mint NOTHING", joined, statErr)
		}
	}

	// The grouping arm, whose repair the operand DOES determine: the dashes are
	// right characters in the wrong places, so re-grouping names the row.
	sd, err := TaskSessionDir(extraDigitDashMoveID)
	if err == nil {
		t.Fatalf("TaskSessionDir(%q) = (%q, nil) — a uuid with the dashes moved must be REFUSED", extraDigitDashMoveID, sd)
	}
	if !strings.Contains(err.Error(), extraDigitRowID) {
		t.Errorf("TaskSessionDir(%q) refusal = %q, want it to name the canonical row id %q", extraDigitDashMoveID, err, extraDigitRowID)
	}
}

// TestOneRunGetsOneSessionDirectory is the epic's acceptance test: for ONE row
// id, the report path the harness exports and the container the run writes its
// evidence into are THE SAME DIRECTORY, and the divergent spelling cannot
// become a path element at all.
//
// The two assertions are deliberately separate: the first pins the agreement
// that makes the report readable, the second pins the refusal that stops the
// second container from being born. Before the fix the second assertion was
// red (the sibling resolved to a directory of its own) while the first was
// green — which is exactly why the defect was silent.
func TestOneRunGetsOneSessionDirectory(t *testing.T) {
	covLifeSessionEnv(t)

	report, err := TaskReportPath(extraDigitRowID)
	if err != nil {
		t.Fatalf("TaskReportPath(%q): %v", extraDigitRowID, err)
	}
	sd, err := TaskSessionDir(extraDigitRowID)
	if err != nil {
		t.Fatalf("TaskSessionDir(%q): %v", extraDigitRowID, err)
	}
	if got := filepath.Dir(report); got != sd {
		t.Fatalf("the report path's directory %q is not the run's session dir %q — one row must have ONE container", got, sd)
	}

	// The divergent spelling: refused, and provably not the same directory.
	if _, err := TaskSessionDir(extraDigitSiblingID); err == nil {
		t.Fatalf("TaskSessionDir(%q) succeeded — the divergent spelling became a path element, so one run can own TWO session directories (%q and %q)",
			extraDigitSiblingID, sd, filepath.Join(filepath.Dir(sd), extraDigitSiblingID))
	} else if !strings.Contains(err.Error(), sessionid.TaskIDPrefix) && !strings.Contains(err.Error(), "task id shape") {
		t.Errorf("the refusal %q does not come from the shared shape rule (internal/sessionid) — a second refusal face is a second spelling rule (Rule 14)", err)
	}
}

// TestTaskSessionDirKeepsTheIdsItsBlacklistAdmits is the non-over-blocking
// half. The rule is a BLACKLIST of what it can prove is a mangled uuid (the
// measured lesson: a first cut that whitelisted broke nine real tests), so the
// ids the tree really carries must still resolve — including the very operand
// shape this change refuses when it is a row's bytes, and the near-miss
// hex+non-hex tokens that never reach the uuid parse.
func TestTaskSessionDirKeepsTheIdsItsBlacklistAdmits(t *testing.T) {
	home := covLifeSessionEnv(t)

	// extraDigitAnonymousID is 33 hex characters that are NOT any row's bytes
	// plus one digit — but the rule refuses the SHAPE (see sessionid.Refuse for
	// why no deletion can be singled out), so it is recorded here as the one
	// accepted cost of the conservative arm rather than asserted to resolve.
	for _, id := range []string{
		"01a11548-tracked",
		"01a11548",
		"01a09aaa-dead-beef-cafe-000000000001",
		"0000-emptydir",
		"t-1788146419",
		"t-worker-stale",
		"capture-round-probe",
	} {
		sd, err := TaskSessionDir(id)
		if err != nil {
			t.Errorf("TaskSessionDir(%q) = err %v — an id the tree really carries must keep resolving (the rule is a blacklist of provable mangled uuids, never a whitelist)", id, err)
			continue
		}
		if want := filepath.Join(home, ".gopi", config.SessionsDirName, id); sd != want {
			t.Errorf("TaskSessionDir(%q) = %q, want %q", id, sd, want)
		}
	}

	// The cost, named rather than hidden (Rule 15): an anonymous overlong
	// all-hex operand is refused, because that is the only way to refuse the
	// near-miss that shares its shape.
	if _, err := TaskSessionDir(extraDigitAnonymousID); err == nil {
		t.Errorf("TaskSessionDir(%q) succeeded — the conservative arm narrowed: a 33-hex operand is no longer refused, so the near-miss it exists for is not either", extraDigitAnonymousID)
	}
}
