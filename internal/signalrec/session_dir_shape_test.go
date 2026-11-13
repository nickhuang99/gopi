package signalrec

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionid"
)

// srdssRowID is the ROW spelling of the id the tests below mangle: a canonical
// uuid, exactly the shape the ledger holds (these tests are about the SHAPE of
// the operand, never about whether the row exists — this package never reaches
// the ledger).
const srdssRowID = "01a12f3c-d888-720e-b7b5-f458f55e98aa"

// srdssBranchSpelling is srdssRowID as the BRANCH/WORKTREE spelling — every
// non-alphanumeric character dropped (internal/tools.taskGroundIDStem). It is
// the measured near-miss: 11 containers with this spelling sat beside 1300
// canonical ones on the host that produced bug row 01a12f3c, and the case that
// surfaced the defect had a caller reading its evidence out of a directory gopi
// never wrote.
func srdssBranchSpelling() string {
	return strings.ReplaceAll(srdssRowID, "-", "")
}

// srdssMintRowContainer materialises a REAL session container for the row —
// metadata.json, the identity file isSessionDir insists on — so the ONLY thing
// that can refuse the record is the id's SHAPE and not a missing directory.
// That distinction is the whole point: before the fix, the same setup with the
// branch spelling was accepted silently.
func srdssMintRowContainer(t *testing.T, home, id string) string {
	t.Helper()
	dir := filepath.Join(home, ".gopi", config.SessionsDirName, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mint session container %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionMetadataName), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", sessionMetadataName, err)
	}
	return dir
}

// srdssSessionsRootEntries lists the sessions root's children (names only) —
// the "did a bucket get minted?" measurement.
func srdssSessionsRootEntries(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, ".gopi", config.SessionsDirName))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestSignalRecRefusesTheBranchSpellingOfTheLedgerID is the RED→GREEN regression
// for bug row 01a12f3c's remaining READ-side seam: sessionDest's step 2 joins
// the carried ledger id into a path WITHOUT asking whether this tree spells a
// task id that way.
//
// BEFORE the fix (measured, and the reason this test exists): the branch
// spelling has no container under the sessions root, so the record fell through
// to the resident process layout — or, on a one-shot, to a refusal that named a
// MISSING DIRECTORY. Neither says the true fact: this operand is not a task-id
// spelling at all, and a record attributed to it explains a run whose directory
// it does not name. Worse, the miss is invisible precisely when it matters: a
// container that DOES carry the branch spelling (a tree already littered by the
// old, ungated writers) was accepted without a word.
//
// AFTER the fix: the shape is decided by internal/sessionid — the SAME
// definition the two session-directory writers use (Rules 13/14) — so this gate
// cannot drift from theirs, and the refusal is that package's own sentence
// (reused, never re-written here).
func TestSignalRecRefusesTheBranchSpellingOfTheLedgerID(t *testing.T) {
	home := srHome(t)
	srSetResidentArgv(t) // the WEAKEST caller: even the resident fall-through must not accept it
	t.Setenv(taskRunEnvName, srdssBranchSpelling())
	// Give the branch spelling a REAL, session-shaped container. This is the
	// pre-fix litter the host still carries, and it is what makes the ungated
	// behaviour a SILENT join rather than a visible miss.
	srdssMintRowContainer(t, home, srdssBranchSpelling())

	err := Intent(srMergerKind, "operator ran gopi merger --stop", 4242, 4242)
	if err == nil {
		t.Fatalf("Intent accepted the branch/worktree spelling %q as a ledger id — the shape gate is not on sessionDest's step 2", srdssBranchSpelling())
	}
	// The refusal must be the SHARED sentence (internal/sessionid), not a
	// second wording minted here: a reworded copy is the drift Rules 13/14 ban.
	if want := sessionid.Refuse(srdssBranchSpelling()).Error(); !strings.Contains(err.Error(), want) {
		t.Errorf("refusal %q does not carry internal/sessionid's own sentence %q", err.Error(), want)
	}
	// It must name the repair (the ROW id), because a refusal an operator
	// cannot act on is the silence this package exists to end.
	if !strings.Contains(err.Error(), srdssRowID) {
		t.Errorf("refusal %q does not name the row id %q — the near-miss has to be repairable from the message", err.Error(), srdssRowID)
	}
	// The process-key fall-through must NOT have caught it: an id this tree
	// does not spell names no row's container, so filing the record under
	// process-<pid> would attribute it to a directory the run never wrote.
	if dir := filepath.Join(home, ".gopi", config.SessionsDirName, processDirPrefix+strconv.Itoa(os.Getpid())); srReadIn(t, dir) != nil {
		t.Errorf("the record was filed into the process dir %s — an inadmissible id must not fall through to the process layout", dir)
	}
	srAssertNoLegacySink(t, home)
}

// TestSignalRecAcceptanceIsUnchangedForAdmissibleIDs is the other half of the
// gate, and it is not decoration: the fix must be a REFUSAL OF THE NEAR-MISS,
// never a general narrowing of what this package files. Both admissible shapes
// keep their exact behaviour.
//
//   - the canonical row id: a container that exists is USED (LayoutTaskRun);
//   - a t-… auxiliary/legacy id: the ledger's pre-uuid channel (~256 rows on
//     this host) — its container is used the same way;
//   - an unknown-but-admissible id: the PRE-EXISTING behaviour stands. A
//     one-shot with no usable dir is still refused, and that refusal is about
//     the missing DIRECTORY (a real row in a profile where no dir can exist) —
//     a different defect, deliberately untouched.
func TestSignalRecAcceptanceIsUnchangedForAdmissibleIDs(t *testing.T) {
	for _, id := range []string{srdssRowID, "t-1759600000"} {
		t.Run(id, func(t *testing.T) {
			home := srHome(t)
			srSetOneShotArgv(t)
			t.Setenv(taskRunEnvName, id)
			dir := srdssMintRowContainer(t, home, id)

			if err := Intent(srMergerKind, "one-shot run of a row with a container", 4242, 4242); err != nil {
				t.Fatalf("Intent refused an admissible id with a usable container: %v", err)
			}
			recs := srReadIn(t, dir)
			if len(recs) != 1 {
				t.Fatalf("want 1 record in %s, got %d", dir, len(recs))
			}
			if recs[0].Layout != LayoutTaskRun {
				t.Errorf("layout = %q, want %q — an admissible id keys the record by its own container", recs[0].Layout, LayoutTaskRun)
			}
			if recs[0].TaskRun != id {
				t.Errorf("record task_run = %q, want %q", recs[0].TaskRun, id)
			}
			// No second container, and no bucket for anything else.
			if got := srdssSessionsRootEntries(t, home); len(got) != 1 || got[0] != id {
				t.Errorf("sessions root holds %v, want exactly [%s] — an admitted id must key ONE directory", got, id)
			}
			srAssertNoLegacySink(t, home)
		})
	}
}

// TestSignalRecUnknownAdmissibleIDKeepsTheMissingDirRefusal pins the boundary
// between the two defects so a later change cannot merge them: an id that IS a
// task-id spelling, whose directory simply does not exist, keeps the
// pre-existing refusal — and that refusal talks about the DIRECTORY, not about
// the shape. The shape gate must never widen into "refuse ids with no container"
// (the resident fall-through exists precisely for that case).
func TestSignalRecUnknownAdmissibleIDKeepsTheMissingDirRefusal(t *testing.T) {
	home := srHome(t)
	srSetOneShotArgv(t)
	unknown := "01a12f3c-0000-4000-8000-000000000000" // admissible, and absent from this profile
	t.Setenv(taskRunEnvName, unknown)

	err := Intent(srMergerKind, "one-shot with no container in this profile", 4242, 4242)
	if err == nil {
		t.Fatal("a one-shot with an admissible id and no usable dir must still be refused (pre-existing contract)")
	}
	if strings.Contains(err.Error(), "task id shape") {
		t.Errorf("refusal %q is about the SHAPE — an admissible id with no container is a missing-directory defect, not an inadmissible spelling", err.Error())
	}
	if !strings.Contains(err.Error(), unknown) {
		t.Errorf("refusal %q does not name the id it could not honour", err.Error())
	}
	srAssertNoLegacySink(t, home)
}
