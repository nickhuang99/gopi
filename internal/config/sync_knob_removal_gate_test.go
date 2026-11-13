package config

// The sync-knob removal gate: the three serve-tick/sync variables that the
// catalog and `gopi --help-env` advertised while NOTHING read them must stay
// removed, and the mechanism that kept one of them alive must stay deleted.
//
// WHY IT EXISTS (Rule 21's defect shape, measured 2026-11-05). A knob is a
// PROMISE: a row in the catalog is what `gopi --help-env` renders from, so a
// row for a variable no production path reads tells an operator to set
// something that changes nothing — and worse, it tells them the switch exists
// at all. Three such rows sat in the catalog:
//
//	GOPI_SYNC_INTERVAL       zero readers, ever (the serve tick's interval was
//	                         never read after the sync surface was deleted)
//	GOPI_SYNC_GIT_INTERVAL   zero readers, ever (its named reader
//	                         `serveGitSyncInterval` does not exist)
//	GOPI_SYNC_FULL_EVERY     read ONLY by its own tests: the mechanism it paced
//	                         (`SyncFullReconcile` + the per-peer counter) had no
//	                         production caller — a knob kept alive by the tests
//	                         that read it, which is a closed loop, not a feature
//
// WHAT THIS TEST FREEZES — three directions, because the defect could return
// through any of them (the same three the precedent gate for the heartbeat
// knobs uses: `heartbeat_knob_removal_gate_test.go`):
//
//  1. the CATALOG must not carry the names (a row is what makes
//     `gopi --help-env` render one);
//  2. the RENDERED front door (`FormatEnvTable`, the text `gopi --help-env`
//     prints) must not carry them either — the catalog and the page are two
//     surfaces and a row could come back without the section, or vice versa;
//  3. no PRODUCTION file may read or declare them, and the dead mechanism's
//     files and symbols must stay gone. This is the half that catches a
//     re-wiring attempt ("let us just read the env var again") rather than only
//     a re-advertisement.
//
// WHY LITERALS HERE ARE CORRECT (Rule 13, stated so a later reader does not
// "fix" it): Rule 13 bans a bare literal standing in for a constant that
// exists. Here no constant may exist — each constant's whole reason for being
// was the removed knob, so a `syncIntervalEnv` constant would resurrect the
// spelling as a first-class knob name. The literal IS the artifact under test:
// these constants are the gate's subject, and the assertion is that they occur
// NOWHERE else in production.
//
// THE POINTER-COMMENT PROBLEM, and why the production scan is not a substring
// match. The comments that USED to point at these knobs were rewritten to
// record the removal (Rule 41a: a reader of `task_sync_git.go` or
// `cli_tasks_worker.go` must not be sent to a variable that does not exist).
// Line-WRAPPING is not predictable from code, so a scan that forbade the
// spelling in a comment would forbid the honest removal note too, and the
// owners would simply stop writing the note. The split therefore matches a
// STRING LITERAL — `"GOPI_..."` — which is what a reader (catalog row,
// `os.Getenv`, `t.Setenv`) must spell, and a comment never does.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// removedSyncIntervalEnv is the serve-tick auto-sync pacing knob's exact
	// spelling; nothing ever read it.
	removedSyncIntervalEnv = "GOPI_SYNC_INTERVAL"
	// removedSyncGitIntervalEnv is the git-pass cadence knob's exact spelling;
	// its advertised reader (`serveGitSyncInterval`) never existed.
	removedSyncGitIntervalEnv = "GOPI_SYNC_GIT_INTERVAL"
	// removedSyncFullEveryEnv is the periodic-full backstop's knob; only its
	// own tests read it.
	removedSyncFullEveryEnv = "GOPI_SYNC_FULL_EVERY"
	// removedFullSyncBoundEnv overrode the FULL reconciliation's ssh ceiling.
	// It is removed with the same mechanism (its ONE reader was
	// `fullSyncSSHBound`, in the file below).
	removedFullSyncBoundEnv = "GOPI_TASK_SYNC_FULL_TIMEOUT_SEC"
	// removedFullBoundFile held the work-sized ceiling model for the FULL
	// leg's ssh bound; its only caller was the deleted session path.
	removedFullBoundFile = "internal/tools/memory_sync_full_bound.go"
	// syncKnobProductionScanDirs are the trees a knob may legitimately be read from.
	// `internal/config` is included although it is this test's own package: the
	// catalog row IS the advertisement, and a re-added row must fail here too.
	syncKnobProductionScanDirs = "internal,cmd"
)

// removedSyncSymbols are the dead mechanism's symbols. Each was reachable only
// from the removed cross-node sync surface (or from the tests that were deleted
// with it), so their reappearance in production means the mechanism came back.
var removedSyncSymbols = []string{
	"SyncFullReconcile",
	"syncPeerSessionFull",
	"SyncFullEvery",
	"MustSyncFullEvery",
	"SyncTickShouldRunFull",
	"syncTickShouldRunFull",
	"syncFullCounterLoad",
	"syncFullCounterSave",
	"syncFullCounterKey",
	"fullSyncSSHBound",
}

// removedSyncSymbolRE matches a production DECLARATION or CALL of one of the
// symbols above. It is deliberately a word-boundary match on the identifier
// followed by a paren or a definition keyword, not a bare substring: comments
// in this tree legitimately NAME these symbols to explain their removal (Rule
// 41a), and a gate that failed on prose is a gate its owners route around.
var removedSyncSymbolRE = regexp.MustCompile(
	`(?m)\bfunc\s+(\([^)]*\)\s*)?(` + strings.Join(removedSyncSymbols, "|") + `)\b|\b(` +
		strings.Join(removedSyncSymbols, "|") + `)\s*\(`)

// TestRemovedSyncKnobsStayRemoved is the gate. It fails if the dead wiring or
// its advertising returns in any of the three directions above.
func TestRemovedSyncKnobsStayRemoved(t *testing.T) {
	root := repoRootForConfigTest(t)
	names := []string{
		removedSyncIntervalEnv,
		removedSyncGitIntervalEnv,
		removedSyncFullEveryEnv,
		removedFullSyncBoundEnv,
	}

	// Direction 1 — the catalog. The catalog is the source of truth the help
	// renders from, so a row here IS the advertisement.
	for _, e := range EnvVars {
		for _, n := range names {
			if e.Env == n {
				t.Errorf("catalog row %q is back: %s was removed because no production path read it "+
					"(Rule 21) — a row is what makes `gopi --help-env` promise the knob", e.Env, n)
			}
		}
	}

	// Direction 2 — the rendered front door. Asserted against the RENDERED
	// text, not the section table: that is the surface an operator reads.
	envTable := FormatEnvTable()
	for _, n := range names {
		if strings.Contains(envTable, n) {
			t.Errorf("`gopi --help-env` (FormatEnvTable) advertises %s again — "+
				"a knob that nothing consumes must not be documented as one", n)
		}
	}
	// The section must still render: the removal dropped four NAMES, not the
	// distibuted-ledger section, so a reader who knows where the knobs lived
	// still finds the rest (`GOPI_ADOPTION_INTERVAL` is live and stays).
	if !strings.Contains(envTable, usageDistributedLedgerSection) {
		t.Errorf("the distributed-ledger section vanished from `gopi --help-env` — the four names "+
			"were removed, the section was not (want it still rendering: %s)", usageDistributedLedgerSection)
	}
	if !strings.Contains(envTable, "GOPI_ADOPTION_INTERVAL") {
		t.Errorf("GOPI_ADOPTION_INTERVAL is LIVE (there is a production reader) and must stay advertised")
	}

	// Direction 3 — production code. A re-read (or a re-declaration) of any
	// name, and the resurrection of the dead mechanism, both fail here.
	for _, dir := range strings.Split(syncKnobProductionScanDirs, ",") {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			body, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			rel, _ := filepath.Rel(root, p) //stylegate:ignore relativises a path inside the test's own root; an error would only mislabel the finding this gate prints
			rel = filepath.ToSlash(rel)
			// The gate's own file is a production-scanned tree member only when
			// this package lives under `internal/config` — which it does. Its
			// literals ARE the subject, so the scan exempts exactly this file
			// and nothing else.
			if rel == removedSyncGateSelfPath {
				return nil
			}
			for _, n := range names {
				if strings.Contains(string(body), `"`+n+`"`) {
					t.Errorf("%s carries the literal %q — the removed knob's reader/declaration is back "+
						"in production; re-wiring a knob nothing consumes is the defect, not the fix", rel, n)
				}
			}
			if removedSyncSymbolRE.Match(body) {
				t.Errorf("%s declares or calls a symbol of the removed periodic-full mechanism (%s) — "+
					"it had no production caller; do not revive it", rel, strings.Join(removedSyncSymbols, ", "))
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("scan %s: %v", dir, walkErr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(removedFullBoundFile))); err == nil {
		t.Errorf("%s exists again — its only caller was the deleted FULL session path; the surviving "+
			"delta leg keeps the fixed stall detector and needs no work-sized model", removedFullBoundFile)
	}
}

// usageDistributedLedgerSection is the `gopi --help-env` section header the
// removed names lived under. Named here so the assertion above and the section
// table in help.go cannot drift.
const usageDistributedLedgerSection = "Distributed task ledger"

// removedSyncGateSelfPath is this file's repo-relative path: the ONE production
// file the scan exempts, because the removed spellings are its subject.
const removedSyncGateSelfPath = "internal/config/sync_knob_removal_gate_test.go"
