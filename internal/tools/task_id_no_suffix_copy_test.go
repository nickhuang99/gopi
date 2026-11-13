package tools

// task_id_no_suffix_copy_test.go — the regression guard for the HALF of the
// clone-id ruling that a passing mint cannot catch.
//
// THE TWO HALVES OF THE RULING (operator, 2026-11-05, memory #27273: a task id
// is an IDENTITY, `parent_task_id` / `deliverable` / notify routing / the
// `--resume` selector all resolve a row by it, so appending an ordinal destroys
// exactly that). Both halves changed the same day, and only one of them is
// covered by a test that a mint can exercise:
//
//	① THE MINT. `taskCloneMintID` now calls the ONE existing minter
//	   (`taskIDGenerate`, uuid v7) instead of `taskCloneFreeID`'s `%s-c%d`
//	   loop. Pinned by TestTaskCloneMintsACleanRoomRow in task_clone_test.go —
//	   a test that FAILS when the mint is forced back to a suffix.
//	② THE PROSE. Every surface that TELLS an operator what a clone produces
//	   used to advertise `<id>-c1`. A mint nobody reads cannot make that true
//	   again, so ①'s arms stay green while the advertised id is one the ledger
//	   would never issue — and `gopi task clone` is precisely the surface an
//	   operator reads to decide which of two rows to address.
//
// WHY THIS FILE EXISTS AT ALL, measured: when ① landed (dd369c8d) the prose
// lagged it by four surfaces — `taskRunResumeRefusalFmt` (the string this
// package's own status gate PRINTS), a second printed line in
// cmd/agent/cli_resume.go's `resumeGateRefuseStatus` arm, `gopi task clone`'s
// own usage text in cmd/agent/cli_tasks.go, and the normative page
// devdoc/task-clone-clean-room.md. A green suite said nothing about any of
// them, because not one of them is a code path.
//
// THE PREDICATE IS NARROW ON PURPOSE. It matches a task-id SPELLING, not the
// substring `-c1`: a concrete suffixed uuid, or an angle-bracket placeholder
// (`<id>-c1`, `<uuid>-r1`, `<src>-c2`). A broad `-<letter><digit>` would also
// match `report-r3.md` in internal/standards — a FILENAME, and a false positive
// that would push the next author to weaken the gate rather than fix the text.
//
// WHAT IS DELIBERATELY EXEMPT, and why it is not a loophole:
//
//   - `_test.go` — fixtures prove the MIGRATION readers (a pre-ruling id must
//     still resolve, a near-miss `-r01` must still be refused). Removing the
//     shape from the tests would remove the only proof the old rows still work.
//   - any line naming `TaskCloneSuccessorIDFmt` / `taskSuccessorIDFmt` — the
//     named constants ARE the legacy shape's single definition (Rule 14), and
//     the three `Sprintf` sites beside them are migration READERS
//     (taskCloneIsSuccessorID, taskCloneLiveSuccessorFind's legacy arm,
//     sessionContainerRowOfTask's directory fallback).
//   - a full-line `//` comment. Prose MAY name the retired shape to explain it
//     (task_clone.go's constant comment does exactly that, and it is the best
//     documentation this rule has). What may not carry it is anything a `fmt`
//     call can put on an operator's terminal.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/taskverbs"
)

const (
	// suffixIDTaskFloor is the non-vacuity floor: a scan that reached fewer
	// production Go files than this measured nothing and must not pass
	// (Rule 15 — a green that never looked is worse than a red).
	suffixIDTaskFloor = 200
	// suffixIDScannedDirs are the two production trees this gate reads.
	suffixIDScannedDirInternal = "internal"
	suffixIDScannedDirCmd      = "cmd"
)

// suffixIDShape matches a task-id SPELLING carrying a successor ordinal: the
// legacy `<uuid>-c1`/`-r1`, or a placeholder that teaches one.
var suffixIDShape = regexp.MustCompile(
	`(<(id|uuid|src[A-Za-z]*)>[ ]?-[cr][0-9])` + // `<id>-c1`, `<uuid>-r2`
		`|` + `([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}-[cr][0-9])`) // a concrete one

// TestNoProductionSurfaceTeachesASuffixTaskID walks BOTH production trees and
// fails on any line that could print a suffixed task id at an operator. It
// reports every file it read, so a scan that reached nothing cannot read green.
func TestNoProductionSurfaceTeachesASuffixTaskID(t *testing.T) {
	root := filepath.Join("..", "..")
	var (
		scanned int
		bad     []string
	)
	for _, dir := range []string{suffixIDScannedDirInternal, suffixIDScannedDirCmd} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				// An unreadable production file is NOT a pass: it is a scan
				// with a hole in it (Rule 15).
				return readErr
			}
			for i, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") {
					continue // prose may name the retired shape to explain it.
				}
				if strings.Contains(line, "TaskCloneSuccessorIDFmt") || strings.Contains(line, "taskSuccessorIDFmt") {
					continue // the single definition and its migration readers.
				}
				if suffixIDShape.MatchString(line) {
					bad = append(bad, path+":"+strconv.Itoa(i+1)+": "+trimmed)
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}
	if scanned < suffixIDTaskFloor {
		t.Fatalf("scanned only %d production Go files (floor %d) — the walk measured nothing, so this green is a vacuum",
			scanned, suffixIDTaskFloor)
	}
	if len(bad) > 0 {
		t.Errorf("a task id is an IDENTITY (operator ruling 2026-11-05, memory #27273): no production line may teach a\n"+
			"suffixed id — `gopi task clone` mints a FRESH uuid and records the lineage in the row's own\n"+
			"`clone` + `rework-of:<id>` tags. Fix the wording (say \"a fresh pending row under a NEW uuid\").\n"+
			"Scanned %d files; offending lines:\n  %s", scanned, strings.Join(bad, "\n  "))
	}
}

// TestTheResumeRefusalDoesNotAdvertiseASuffixID drives the REAL refusal
// renderer — the one `gopi task status`/`gopi session resume` print through
// taskRunResumeRefusalText — and asserts the operator-facing sentence names the
// remedy WITHOUT spelling the id it used to mint. This is ② at the exact seam
// that shipped, so the gate above cannot be satisfied by deleting the string.
func TestTheResumeRefusalDoesNotAdvertiseASuffixID(t *testing.T) {
	const srcID = "01a1a8d4-ef5b-7161-8461-db3172ae9b9b"
	msg := taskRunResumeRefusalText(srcID, TaskStatusCompleted)

	if suffixIDShape.MatchString(msg) {
		t.Errorf("the resume refusal teaches a suffixed task id — the ledger would never issue one:\n%s", msg)
	}
	// The remedy must still be NAMED, and named from the shared constant (Rule
	// 14): a test that retyped "gopi task clone" would pass while the surface
	// renamed the verb.
	cloneVerb := runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Clone
	if !strings.Contains(msg, cloneVerb) {
		t.Errorf("the refusal must still name the clone remedy verbatim (%q), got:\n%s", cloneVerb, msg)
	}
	if !strings.Contains(msg, "NEW uuid") {
		t.Errorf("the refusal must say the copy gets a NEW uuid, so the operator knows what to look for, got:\n%s", msg)
	}
	if !strings.Contains(msg, srcID) {
		t.Errorf("the refusal must carry the SOURCE row id (it is the operand of both ways forward), got:\n%s", msg)
	}
}
