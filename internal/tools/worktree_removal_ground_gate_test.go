package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryWorktreeRemovalArmQuestionsTheGround is the exhaustive anti-regression
// guard for the epic this package just landed: EVERY arm that frees a run's
// worktree must first ask the shared deterministic checklist
// (reclaimGroundChecklistVerdict, reclaim_ground_checklist.go) whether the ground
// is its to free.
//
// Why a scan and not a runtime test: the defect class is "a NEW arm is added and
// nobody remembers the checklist" — the arms are separate code paths with three
// different callers (row reclaim, delivery-chain close-out, background sweeps),
// so no single runtime entry point exercises all of them, and a further arm
// would arrive silently green. The packages' own audit record is the marker of
// an arm (package removalaudit: one record per removal attempt), so the guard
// anchors on `removalaudit.WorktreeRemove(` — the line no arm can omit without
// losing its evidence trail — and requires each site to be either
// checklist-guarded or explicitly exempted with a reason.
//
// The exemption is a REVIEWED act with a reason, never a weakened check: the
// arms that legitimately need no rung from the checklist carry
// `reclaim-checklist-exempt: <reason>` in the comment block above their call,
// and the census (arms, guarded, exempted) is pinned by the read floors at the
// foot of this test — an arm can appear or disappear only when its count is
// re-read.
func TestEveryWorktreeRemovalArmQuestionsTheGround(t *testing.T) {
	const (
		removalMarker = "removalaudit.WorktreeRemove("
		exemptMarker  = "reclaim-checklist-exempt:"
		// removalTipMarker is the CLONE leg's spelling. af5618e moved that arm off
		// `removalaudit.WorktreeRemove(` onto `RecordGroundRemovalTip(...)` — the
		// clone ground's record must carry the tip it is about to destroy, which
		// `WorktreeRemove` has no parameter for — and this gate, which keyed on the
		// old spelling alone, stopped SEEING the arm instead of judging it. The
		// census then read 11 against an inventory of 12, and the arm that frees a
		// whole repository was the one no longer audited. Two markers, one site
		// each kind: a marker added here is a class of destruction this census
		// judges, never a way to make a count come out even.
		removalTipMarker = "RecordGroundRemovalTip("
		// commentWindow is how many lines above the call are scanned for its
		// justification. 80, and NOT the 40 this gate shipped with, because 40 was
		// measuring DISTANCE where the contract is about the FACT: the server
		// close-out arm (ArmFinishCleanupServer) was checklist-guarded the whole
		// time and its guard sat 41 lines up (measured 2026-09-27 on master
		// 885b0b8f) — three further guards, each with its own explanation, ran
		// between the checklist call and the removal, so the census read "6
		// guarded" and this gate's own read floor became unreachable. Nothing was
		// missing from the production arm; the WINDOW was. A window is still
		// bounded on purpose — the justification must stay local to the removal it
		// authorises, or the marker could be satisfied from anywhere in the file.
		commentWindow = 80
		// 9, measured 2026-10-30 on this tree (the census printed all nine). It was
		// 10 when 10 sites existed; the two close-out sites named below left with
		// the mechanism (58b5618), so the FLOOR follows the tree. The floor is a
		// vacuity guard, not the inventory — `armInventory` is the count that
		// notices an arm arriving or leaving.
		armReadFloor = 9
		// guardReadFloor stays 7 (2026-09-27, measured): the new out-of-root
		// disposal arm (reclaimSelfOwnOutOfRootDisposal) arrived GUARDED — it asks
		// this same checklist before it discards — so the count went 6 → 7 guarded
		// of 10 → 11 total, and the number of UNGUARDED (flagged) sites did not
		// move. Bumping this floor to the new total would have skipped a step and
		// handed a future unguarded arm a free pass; `armInventory` below is the
		// counter that tracks the total.
		guardReadFloor = 7
		// The inventory below is the MEASURED truth of the landed tree, re-read
		// 2026-09-27 after this branch's two lineages met: 11 sites = 8
		// checklist-guarded + 3 reviewed-exempt. The two lineage notes that used
		// to sit here argued 10 (this branch's read) and 11 (master's read) — both
		// were read against a base that lacked the other's new arm, and the count
		// that matters is the one the census prints, which is why the census now
		// prints the whole list beside the number.
		//
		//	guarded  task_finish_cleanup.go  ArmFinishCleanupServer  (close-out form)
		//	guarded  task_finish_cleanup.go  ArmFinishCleanupForce   (close-out form)
		//	guarded  run_ground_anchor.go    ArmTaskGroundAnchor     (linked worktree)
		//	guarded  run_ground_anchor.go    ArmTaskGroundAnchorClone (clone ground)
		//	guarded  task_reclaim.go         ArmRowReclaim
		//	guarded  task_reclaim_self.go    ArmSelfReclaim
		//	guarded  task_reclaim_self.go    ArmSelfOwnSweep  (sweep)
		//	guarded  task_reclaim_self.go    ArmSelfOwnSweep  (out-of-root disposal)
		//	guarded  worktree_sweep.go       ArmSweepReclaim
		//	exempt   merge_build_gate.go     ArmMergeBuildGateGround
		//	exempt   task_merged_cleanup.go  ArmMergedCleanupWorktree
		//	exempt   tasks.go                ArmVerifyGround   ← RETIRED 2026-10-30
		//
		// 12 as of 2026-10-26 (+1 guarded): the anchor axis grew a SECOND removal
		// verb, because one verb cannot free both ground shapes. `.git` as a FILE
		// is a linked worktree and `git worktree remove` is right for it; `.git`
		// as a DIRECTORY is a clone-channel ground (internal/devworkspace) and
		// that same verb answers exit 128 `is not a working tree` for it — which
		// is why 725 collectable clone grounds on this host had never been
		// released (351 directories / 28.9 GB on a 96 %-full `/`). The clone leg
		// is a separate SITE and a separate ARM (ArmTaskGroundAnchorClone)
		// because it is a different destruction (os.RemoveAll of a repository,
		// not a registration drop) decided by different evidence, and a record
		// that could not tell them apart could not be audited. It asks the same
		// checklist, at the same place in the ladder: before any act, after every
		// refusal. Why the INVENTORY moves rather than the threshold: this census
		// is what notices that an arm arrived or left at all, so the count is
		// re-read deliberately, never relaxed.
		//
		// Two of the twelve carry the SAME arm constant (ArmSelfOwnSweep): the
		// sweep site and the out-of-root disposal site it grew. The arm constant is
		// a label, not a key — the census reports the file:line beside it, which is
		// what distinguishes them.
		//
		// The server close-out site (task_finish_cleanup.go, ArmFinishCleanupServer)
		// is the one this branch un-flags: its checklist call moved down against the
		// removal it authorises, so at a 40-line window it read UNGUARDED on this
		// branch's base AND on master — pre-existing, recorded in the report.
		//
		// 12 → 11: retiring the `gopi task retry-delivery` verb removed its
		// worktreerelease.ReleaseReporting call (cli_tasks_retry_delivery.go,
		// deleted whole). That call released a worktree, so it was one of the
		// arms this census counts; the verb's retirement is the deliberate
		// departure, and no other site changed — the run reported 11 arms with
		// this one gone and the other ten unchanged.
		//
		// The `exempt tasks.go ArmVerifyGround` line above is marked RETIRED and
		// is NO LONGER a site: the same epic's slice 2/4 retired the `verify`
		// verb, and taskVerifyGround — the function that created and then freed
		// the verifier's private scratch checkout — was deleted with it. The arm
		// constant went with its emit (internal/removalaudit/audit.go), so the
		// reverse-direction gate in internal/standards has no declared arm left
		// without a producer. The inventory below did NOT move for that
		// retirement, and this is the measured reason rather than an omission:
		// the census counts CALL SITES in this package, and once the verify
		// ground's own site was gone the total was already 11.
		// 11 → 9 (2026-10-30, task 01a18ea5): 58b5618 retired the finish-cleanup
		// mechanism, and the two sites this census counted there went with it —
		// `task_finish_cleanup.go ArmFinishCleanupServer` (close-out form) and
		// `task_finish_cleanup.go ArmFinishCleanupForce` (close-out form), both
		// listed above and now gone from the scan. No other site changed: the
		// run reports nine arms, seven checklist-guarded (run_ground_anchor twice,
		// task_reclaim, task_reclaim_self twice, worktree_sweep …) — the same
		// guarded count as before, because both retired sites were on the guarded
		// side and `guardReadFloor` stays 7, which is the measurement that the
		// retirement took TWO GUARDED arms and nothing unguarded.
		armInventory = 9
	)

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no .go files found — the gate would pass vacuously")
	}

	arms, guarded, exempted := 0, 0, 0
	var sites []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lines := strings.Split(string(b), "\n")
		for i, line := range lines {
			// The marker is a CALL SPELLING, so the function's own DEFINITION
			// (`func RecordGroundRemovalTip(arm …`) is not a removal site and must
			// not be counted as one — a census that counted the helper would report
			// one more arm than the tree has, and the number would drift the moment
			// the helper moved files.
			isDefinition := strings.HasPrefix(strings.TrimSpace(line), "func ")
			if isDefinition {
				continue
			}
			if !strings.Contains(line, removalMarker) && !strings.Contains(line, removalTipMarker) {
				continue
			}
			arms++
			lo := i - commentWindow
			if lo < 0 {
				lo = 0
			}
			window := strings.Join(lines[lo:i], "\n")
			verdict := removalVerdictUnguarded
			switch {
			case groundGateContainsAny(window, guardMarkers):
				guarded++
				verdict = removalVerdictGuarded
			case strings.Contains(window, exemptMarker):
				exempted++
				verdict = removalVerdictExempt
			default:
				t.Errorf("%s:%d frees a worktree without the ground checklist.\n"+
					"  Either call reclaimGroundChecklistVerdict(repo, worktree, branch) "+
					"before the removal and refuse on a non-empty verdict, or — if this "+
					"arm's ground is genuinely its own, or the question does not apply "+
					"(e.g. a detached checkout with no branch) — state the reason in a "+
					"comment block above the call as `%s <reason>`.\n"+
					"  line: %s", name, i+1, exemptMarker, strings.TrimSpace(line))
			}
			sites = append(sites, fmt.Sprintf("%s:%d %s [%s]", name, i+1, removalArmName(line), verdict))
		}
	}

	// Read floors: a scan that found nothing (renamed marker, moved package)
	// must FAIL rather than read green.
	if arms < armReadFloor {
		t.Errorf("found %d worktree-removal arms, expected at least %d — the scan lost "+
			"its subject (marker renamed?) and would pass vacuously", arms, armReadFloor)
	}
	if guarded < guardReadFloor {
		t.Errorf("found %d checklist-guarded arms, expected at least %d — the guard is "+
			"being bypassed wholesale", guarded, guardReadFloor)
	}
	// The INVENTORY (exact, not a floor): a site entering or leaving this check
	// while the counts above still look healthy is the quiet failure the floors
	// cannot see. Add or remove a removal site → update armInventory in the SAME
	// commit, with the reason in its comment — and the list is LOGGED beside the
	// census below, so a reader can check the number against the arms rather than
	// trusting it.
	if arms != armInventory {
		t.Errorf("found %d worktree-removal arms, the inventory records %d — a removal site "+
			"entered or left this check; update armInventory in the same commit (and say why)",
			arms, armInventory)
	}
	t.Logf("worktree-removal arms: %d total (inventory %d), %d checklist-guarded, %d reviewed-exempt",
		arms, armInventory, guarded, exempted)
	for _, s := range sites {
		t.Logf("  %s", s)
	}
}

// The per-site verdict vocabulary the census log prints — Rule 1b: these words
// are read by a human comparing the census against armInventory, so they have
// names rather than being typed at the reporting site.
const (
	removalVerdictGuarded   = "guarded"
	removalVerdictExempt    = "reviewed-exempt"
	removalVerdictUnguarded = "UNGUARDED"
)

// removalArmName reads the removalaudit arm constant out of a removal line
// ("removalaudit.WorktreeRemove(removalaudit.ArmSweepReclaim, …" → "ArmSweepReclaim")
// so the census names the ARM, not just a file and a line — a reader checking
// armInventory against it does not have to open each file.
func removalArmName(line string) string {
	const qualifier = "removalaudit."
	i := strings.Index(line, qualifier)
	if i < 0 {
		return "?"
	}
	rest := line[i+len(qualifier):]
	if j := strings.Index(rest, qualifier); j >= 0 {
		rest = rest[j+len(qualifier):]
	}
	if j := strings.IndexAny(rest, ",)"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// guardMarkers are the checklist's CALL SPELLINGS — the plain rung every
// reclamation arm asks, and the close-out form the run's own delivery chain
// asks (reclaim_ground_checklist.go documents why that second entry point
// exists: for a run's own close-out the asker's cwd is not a competing holder).
//
// BOTH ENTRIES ARE NEEDED, and the reason is measured, not stylistic: the gate
// used to carry only the plain spelling, and when a tenth arm arrived
// (run_ground_anchor.go:391, ArmTaskGroundAnchor) the census read "4 guarded"
// because the delivery chain's TWO arms — which had asked the close-out form all
// along — matched no marker. A gate keyed on one spelling measures THAT SPELLING,
// not the fact it exists for, and the fix is to enumerate the call shapes rather
// than to loosen the match.
//
// They stay call PREFIXES (trailing paren) on purpose: a comment or a doc
// sentence naming the predicate is not a guard, so prose must never satisfy the
// marker. A THIRD entry point must be added HERE deliberately — until it is, the
// arms asking it read unguarded and fail loudly rather than passing blind.
var guardMarkers = []string{
	"reclaimGroundChecklistVerdict(",
	"reclaimGroundChecklistVerdictForCloseout(",
}

// groundGateContainsAny reports whether s contains any of the given markers.
func groundGateContainsAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
