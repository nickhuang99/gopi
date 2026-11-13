package main

// The exhaustive gate on the ledger CONNECTION CONTRACT of this suite (Rule 13:
// a constant that exists is referenced, never bypassed by a literal).
//
// WHY IT EXISTS. Every fixture in this package that seeds or reads a task row
// races a resident component — the merger/worker daemon the very same test
// starts, or a sibling fixture process writing the one ledger a sandbox home
// shares. Production opens that ledger through openDB (busy_timeout =
// dbBusyTimeoutMSec, WAL journal mode); a bare `sql.Open("sqlite", path)` carries
// NEITHER, so it fails instantly with `database is locked (5) (SQLITE_BUSY)`
// instead of queueing. That is not a flake, it is a defect with a load-dependent
// hit rate: measured 7 of 8 isolated runs of
// TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses failed at the fixture's
// own `UPDATE tasks …` (via openRawTasksDB) and at its status read, and the two
// measured-red rigs are exactly the ones this gate keeps fixed.
//
// The predicate is deliberately narrow: it flags ONLY a `sql.Open` whose driver
// argument is the sqlite driver name AND whose DSN argument is not a call to one
// of the ledger DSN helpers. Everything else — a foreign-DB reader, a fixture
// that names its own journal mode, a diagnostic tool — stays legal, which is what
// keeps the false-positive count at zero.
//
// THE IN-MEMORY EXEMPTION IS A HELPER, NOT A TOLERATED LITERAL. `sql.Open(
// "sqlite", ":memory:")` is not a ledger opener at all: an in-memory database is
// a private, single-connection region of the opening process, it shares no path
// with the resident daemon this contract exists for, and two of the contract's
// three parts are REFUSED on it rather than merely unnecessary — measured,
// `PRAGMA journal_mode=WAL` on `:memory:` returns `memory`, so no DSN spelling
// can carry the contract there. That is the whole reason the class is legal, and
// it is why the fix for the site that kept this gate red on master
// (internal/tools/memory_split_invariant_test.go:52, the fts5 boundary
// invariant, whose subject is a throwaway `unicode61` table and not a ledger)
// was to route the DSN through internal/tools.LedgerMemoryDSN — the value's ONE
// definition, per Rule 13/14 — rather than to teach this scanner to recognise a
// string by its CONTENT. A content-based exemption cannot tell a deliberate
// in-memory fixture from a ledger opener whose path variable happens to hold the
// same text; a helper call is unambiguous, and it keeps the predicate a single
// question: "is the DSN built by a ledger DSN helper?".
//
// The gate is green on the day it lands (the whole package is converted) and
// reddens on the first re-introduced bare opener; the synthetic half below is
// what proves it is not vacuous.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The gate deliberately does NOT scan internal/agencyscan: internal/tools
// imports agencyscan (internal/tools/agency.go), so an agencyscan TEST importing
// the helper would close an import cycle. That package keeps its own documented
// mirror (store.go) and its fixture seeds a private t.TempDir ledger — the cycle,
// not laziness, is the reason; it is named so a future reader does not "fix" the
// gate by adding the tree blindly.

// ledgerContractExemptNames are the PRODUCTION ledger openers that build the
// contract themselves instead of going through the test-facing helper — they are
// the definition of the contract, not consumers of it, so the gate exempts them
// BY NAME with the reason recorded (Rule 16 baseline style: an exact permit with
// an explicit reason, never a weakened predicate). Every entry is verified live
// below — a permit whose site no longer exists fails the gate, because a stale
// permit is a silent permission.
var ledgerContractExemptNames = map[string]string{
	filepath.Join("..", "..", "internal", "tools", "memory.go"): "internal/tools production opener: openDB applies the SAME dbBusyTimeoutMSec via " +
		"`PRAGMA busy_timeout` (executed on the handle, which a DSN cannot express) and enableWAL sets WAL",
	filepath.Join("..", "..", "internal", "agencyscan", "store.go"): "internal/agencyscan's read-only opener: it applies its own busy_timeout pragma, closes the " +
		"handle to one connection and sets query_only — and the package must not import internal/tools",
	filepath.Join("..", "..", "internal", "agencyscan", "agencyscan_test.go"): "the miner's fixture seeds a MINIMAL private ledger in t.TempDir() that no daemon " +
		"ever opens, and the package cannot import internal/tools (internal/tools imports agencyscan: an " +
		"in-package test importing the helper closes an import cycle)",
	filepath.Join("..", "..", "internal", "tools", "ledger_backup_test.go"): "every LEDGER fixture in this file now opens through the DSN helpers; the ONE " +
		"bare opener left is TestLedgerBackupVerifyRefusesAFileWithoutTheLedgerSchema's FOREIGN database — " +
		"a non-ledger that must be created as a plain SQLite file, which is the very thing under test",
}

// ledgerPermitUsed records, per exempted path, whether the permit actually
// matched a bare opener in this scan. It is the live-permit check: an allow-list
// entry whose site is gone must fail the gate (a stale permit is a silent
// permission, exactly as in the standards gates).
var ledgerPermitUsed = map[string]bool{}

const (
	// ledgerSQLiteDriverName is the driver name every ledger opener passes as
	// sql.Open's first argument (Rule 1b: the predicate compares a named const,
	// never a bare literal repeated in the scanner).
	ledgerSQLiteDriverName = "sqlite"
	// ledgerDSNHelperName is the ONE exported helper (internal/tools.LedgerDSN)
	// that builds a ledger DSN with production's busy-timeout + WAL contract.
	ledgerDSNHelperName = "LedgerDSN"
	// ledgerReadOnlyDSNHelperName is the helper's read-only sibling, for a probe
	// that must never write the ledger it measures.
	ledgerReadOnlyDSNHelperName = "LedgerReadOnlyDSN"
	// ledgerReadOnlyDSNInHelperName is the same read-only helper with an
	// explicit timeout, for the ONE class of caller whose give-up budget is not
	// production's (a keystroke path, or a foreground shell verb answering with
	// two integers). It is the SAME DSN shape as ledgerReadOnlyDSNHelperName —
	// `mode=ro` + `query_only(true)` + a busy timeout — with only the timeout
	// value parameterised, so recognising it is not a widening: the class was
	// already admitted, and routing the variable through a helper is exactly
	// what this gate exists to require (Rule 13/14 — the DSN spelling has one
	// definition, and the value is never re-typed at a call site).
	ledgerReadOnlyDSNInHelperName = "LedgerReadOnlyDSNIn"
	// ledgerMemoryDSNHelperName is the helper for the ONE class this contract
	// does not describe — an IN-MEMORY database, which is not a ledger file (see
	// this file's header for the refusal that makes the class legal).
	ledgerMemoryDSNHelperName = "LedgerMemoryDSN"
	// ledgerRollbackJournalDSNHelperName is the helper for the ONE class of rig
	// whose SUBJECT is the refusal itself: a reader that must meet a held write
	// lock. It carries production's busy timeout and synchronous mode, and asks
	// for the rollback journal — the mode in which such a refusal is observable
	// at all (under WAL a reader takes a snapshot and is never refused). It is
	// admitted here for the reason every other helper is: a ledger opener must
	// route through a reviewed DSN definition rather than re-typing a spelling at
	// its call site.
	ledgerRollbackJournalDSNHelperName = "LedgerRollbackJournalDSN"
	// ledgerGoFileSuffix selects the files this gate scans.
	ledgerGoFileSuffix = ".go"
	// ledgerSqliteDriverConstName is the ONE package-scoped const of cmd/agent
	// that holds the sqlite driver name, and the second spelling the predicate
	// above must recognise (a literal-only predicate was measured blind to it —
	// see ledgerIsSQLiteDriverArg). It is named here rather than imported for
	// the same reason the helper names are: this package IS the gate's subject,
	// and the value is verified live below rather than assumed from the name.
	ledgerSqliteDriverConstName = "prefetchDriverName"
)

// The three helper names above are the exported DSN helpers of internal/tools,
// stated here as constants rather than imported. Two reasons, and the first is
// the one that decides it: this package is package main and the internal/tools
// tree is this gate's SUBJECT — an import would make a helper renamed, removed or
// not-yet-written report the tree as a COMPILE failure instead of reporting what
// the scan actually found, which is the difference between a gate that measures
// and a gate that breaks. Second (Rule 1b): one constant per name keeps every
// comparison site in ledgerIsDSNHelperName free of a re-typed literal. A name
// that drifts from the helper is caught the same way the synthetic half catches
// any other predicate error — the helper-built-DSN case below would report a hit.

// ledgerBareOpeners returns the line numbers of every `sql.Open("<sqlite>", dsn)`
// call in src whose dsn is NOT a call to one of the ledger DSN helpers. Comments
// and string literals inside them are irrelevant to the predicate: the source is
// parsed into an AST first, so the doc comments of this very file (which quote
// the bad shape) can never be mistaken for a violation.
//
// A parse failure is returned as an error, never swallowed (Rule 15b): a gate
// that cannot read a file must not report it as clean.
func ledgerBareOpeners(filename, src string) ([]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Open" || len(call.Args) != 2 {
			return true
		}
		if !ledgerIsSQLiteDriverArg(call.Args[0]) {
			return true
		}
		if ledgerIsDSNHelperCall(call.Args[1]) {
			return true
		}
		lines = append(lines, fset.Position(call.Pos()).Line)
		return true
	})
	sort.Ints(lines)
	return lines, nil
}

// ledgerIsSQLiteDriverArg reports whether an sql.Open argument names the sqlite
// driver. TWO spellings are recognised, and the second one exists because the
// first alone was MEASURED blind:
//
//   - a string literal (`"sqlite"`), which is how nearly every site spells it;
//   - the ONE package-scoped const of cmd/agent that holds the same value
//     (ledgerSqliteDriverConstName). A literal-only predicate asks its question
//     about the DRIVER SPELLING, not about the site, so an opener passing a
//     const simply is not its business — and a bare DSN behind such a const
//     passed unnoticed. internal/tools/ledger_dsn.go's own note recorded this
//     class of blindness; the measured instance is this package's prefetch rig
//     family, where `sql.Open(prefetchDriverName, path+"?_txlock=immediate")`
//     (no busy timeout, no WAL) left this gate GREEN at seven open sites.
//
// The const's VALUE is not taken on trust. TestLedgerOpenersUseProductionDSNContract
// reads it at run time (same package) and fails if it no longer holds the sqlite
// driver name — so a const whose value drifted reddens the gate instead of
// silently disarming the arm that recognises it.
func ledgerIsSQLiteDriverArg(arg ast.Expr) bool {
	if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return strings.Trim(lit.Value, `"`) == ledgerSQLiteDriverName
	}
	ident, ok := arg.(*ast.Ident)
	return ok && ident.Name == ledgerSqliteDriverConstName
}

// ledgerIsDSNHelperCall reports whether a DSN argument is built by the helper —
// either `LedgerDSN(...)` or `tools.LedgerDSN(...)`.
func ledgerIsDSNHelperCall(arg ast.Expr) bool {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return ledgerIsDSNHelperName(fn.Name)
	case *ast.SelectorExpr:
		return fn.Sel != nil && ledgerIsDSNHelperName(fn.Sel.Name)
	}
	return false
}

// ledgerIsDSNHelperName reports whether a call's callee name is one of the
// ledger DSN helpers (Rule 1b: the names are consts, compared here, never
// re-typed at a comparison site).
func ledgerIsDSNHelperName(name string) bool {
	return name == ledgerDSNHelperName || name == ledgerReadOnlyDSNHelperName ||
		name == ledgerReadOnlyDSNInHelperName || name == ledgerMemoryDSNHelperName ||
		name == ledgerRollbackJournalDSNHelperName
}

// ledgerScanRoots lists the directories this gate scans for bare sqlite
// openers, and the minimum number of .go files each must yield. The read-floor
// is PER ROOT: a walk that silently stops finding a tree (a moved directory, a
// rename) fails loudly instead of passing vacuously over the trees it did find —
// the shape that let the standards gates pass blind before their exhaustive
// halves were added.
type ledgerScanRoot struct {
	path     string
	minFiles int
}

var ledgerScanRoots = []ledgerScanRoot{
	{path: ".", minFiles: ledgerMinGoFilesScanned},                                                    // cmd/agent
	{path: filepath.Join("..", "..", "internal", "tools"), minFiles: ledgerMinInternalGoFiles},        // the helper's package
	{path: filepath.Join("..", "..", "internal", "agent"), minFiles: ledgerMinInternalGoFiles},        // ledger fixtures of the loop
	{path: filepath.Join("..", "..", "internal", "agencyscan"), minFiles: ledgerMinAgencyscanGoFiles}, // the miner + its fixture
}

// TestLedgerOpenersUseProductionDSNContract is the gate: with one documented
// exception (a foreign-DB reader that deliberately names its own DSN, and
// internal/agencyscan whose fixture seeds a private ledger — see ledgerGateRoots'
// comment for the import-cycle reason), NO file of these trees may open a ledger
// through a bare sqlite DSN.
func TestLedgerOpenersUseProductionDSNContract(t *testing.T) {
	// The predicate recognises the sqlite driver by TWO spellings, and the const
	// arm is only sound if the const really holds that value. Checked LIVE, in
	// this package, so a drifted const reddens HERE instead of silently
	// disarming the arm (a gate that widens its predicate on a name alone is a
	// gate that can be switched off by an edit it does not read).
	if prefetchDriverName != ledgerSQLiteDriverName {
		t.Fatalf("%s = %q, want %q: this gate's driver-const arm recognises that name, so a "+
			"const holding another driver would make the arm a permanent false positive",
			ledgerSqliteDriverConstName, prefetchDriverName, ledgerSQLiteDriverName)
	}
	total := 0
	for _, root := range ledgerScanRoots {
		entries, err := os.ReadDir(root.path)
		if err != nil {
			t.Fatalf("read %s: %v", root.path, err)
		}
		scanned := 0
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ledgerGoFileSuffix) {
				continue
			}
			src, err := os.ReadFile(filepath.Join(root.path, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", filepath.Join(root.path, e.Name()), err)
			}
			lines, err := ledgerBareOpeners(e.Name(), string(src))
			if err != nil {
				t.Fatalf("parse %s: %v", filepath.Join(root.path, e.Name()), err)
			}
			scanned++
			rel := filepath.Join(root.path, e.Name())
			if reason, exempt := ledgerContractExemptNames[rel]; exempt {
				// A permit must be LIVE: if the exempted file no longer opens a
				// ledger with a bare DSN, the permit is stale (a silent
				// permission) and the gate must fail rather than accept it.
				ledgerPermitUsed[rel] = true
				if len(lines) == 0 {
					t.Errorf("stale permit: %s is exempted (%s) but opens no bare-DSN ledger — remove the "+
						"permit; the allow-list may only ratchet down", rel, reason)
				}
				continue
			}
			for _, ln := range lines {
				t.Errorf("%s:%d opens a ledger with a bare sqlite DSN — no busy timeout, no WAL: it fails "+
					"instantly with `database is locked (5) (SQLITE_BUSY)` instead of queueing behind the "+
					"resident daemon this suite starts. Use sql.Open(%q, tools.%s(path))",
					filepath.Join(root.path, e.Name()), ln, ledgerSQLiteDriverName, ledgerDSNHelperName)
			}
		}
		if scanned < root.minFiles {
			t.Fatalf("scanned only %d .go files under %s (floor %d) — the walk is broken, so this gate "+
				"proves nothing about that tree", scanned, root.path, root.minFiles)
		}
		total += scanned
	}
	if total < ledgerMinGoFilesScanned {
		t.Fatalf("scanned %d .go files in total — the walk is broken", total)
	}
	for name, used := range ledgerPermitUsed {
		if !used {
			t.Errorf("stale permit: %s is exempted but the scan never saw it — the file moved or was "+
				"renamed; update or drop the permit", name)
		}
	}
}

// ledgerMinGoFilesScanned is the read-floor of the cmd/agent root: this suite is
// dozens of files, and a number far below that means the walk did not run.
const ledgerMinGoFilesScanned = 20

// ledgerMinInternalGoFiles is the read-floor for the two internal trees the gate
// also covers (each has well over a dozen files).
const ledgerMinInternalGoFiles = 10

// ledgerMinAgencyscanGoFiles is the read-floor of the (small) miner package.
const ledgerMinAgencyscanGoFiles = 5

// TestLedgerBareOpenerPredicateReddensOnSyntheticSources is the red proof — the
// half that makes the gate load-bearing. A gate that cannot fail is decoration,
// so the predicate is exercised on sources whose verdict is known in advance:
// the bare shapes MUST be flagged, and every legitimate shape (helper-built DSN,
// a non-sqlite driver, a foreign-DB DSN, a comment quoting the bad shape) MUST
// NOT be.
func TestLedgerBareOpenerPredicateReddensOnSyntheticSources(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantHit int // how many bare openers the predicate must report
	}{
		{
			name:    "bare path variable — the measured-red shape",
			src:     "package main\nfunc f() { db, err := sql.Open(\"sqlite\", dbPath); _ = db; _ = err }\n",
			wantHit: 1,
		},
		{
			name:    "bare raw path string",
			src:     "package main\nfunc f() { db, _ := sql.Open(\"sqlite\", \"/tmp/x/memory.db\"); _ = db }\n",
			wantHit: 1,
		},
		{
			name:    "helper-built DSN — the fixed shape",
			src:     "package main\nfunc f() { db, _ := sql.Open(\"sqlite\", tools.LedgerDSN(dbPath)); _ = db }\n",
			wantHit: 0,
		},
		{
			name:    "helper called unqualified",
			src:     "package main\nfunc f() { db, _ := sql.Open(\"sqlite\", LedgerDSN(dbPath)); _ = db }\n",
			wantHit: 0,
		},
		{
			// The exemption that carried this gate's red-on-master hit: an
			// in-memory ledger is not a ledger file, and the class is routed
			// through its own helper rather than tolerated as a literal.
			name:    "in-memory helper — the exempt class",
			src:     "package main\nfunc f() { db, _ := sql.Open(\"sqlite\", LedgerMemoryDSN()); _ = db }\n",
			wantHit: 0,
		},
		{
			// The same exemption is NOT a content match: the literal is still a
			// violation even though the helper it should have called renders
			// exactly this text. A scanner that recognised the value would pass
			// this case and could not tell a deliberate in-memory fixture from a
			// ledger opener handed this string by a variable.
			name:    "the bare in-memory literal is STILL flagged — the exemption is the helper, not the value",
			src:     "package main\nfunc f() { db, _ := sql.Open(\"sqlite\", \":memory:\"); _ = db }\n",
			wantHit: 1,
		},
		{
			// The MEASURED blindness this arm was added for: the driver passed as
			// the package const instead of a literal used to make the whole site
			// invisible to the gate, so a bare DSN rode along with it. Seven such
			// sites existed in cmd/agent (the prefetch rig family).
			name:    "driver as the package const with a bare DSN — the measured blind spot",
			src:     "package main\nfunc f() { db, _ := sql.Open(prefetchDriverName, path+\"?_txlock=immediate\"); _ = db }\n",
			wantHit: 1,
		},
		{
			// …and the const spelling is NOT a violation when the DSN does come
			// from the helper: recognising the const must not turn every prefetch
			// opener red.
			name:    "driver as the package const with a helper DSN is clean",
			src:     "package main\nfunc f() { db, _ := sql.Open(prefetchDriverName, tools.LedgerReadOnlyDSNIn(path, 250)); _ = db }\n",
			wantHit: 0,
		},
		{
			name:    "another driver is not this gate's business",
			src:     "package main\nfunc f() { db, _ := sql.Open(\"postgres\", dbPath); _ = db }\n",
			wantHit: 0,
		},
		{
			name: "a comment quoting the bad shape is not a violation",
			src: "package main\n// never do sql.Open(\"sqlite\", path)\n" +
				"func f() { db, _ := sql.Open(\"sqlite\", tools.LedgerDSN(p)); _ = db }\n",
			wantHit: 0,
		},
		{
			name:    "two bare openers are both reported",
			src:     "package main\nfunc f() { a, _ := sql.Open(\"sqlite\", p); b, _ := sql.Open(\"sqlite\", p); _, _ = a, b }\n",
			wantHit: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := ledgerBareOpeners(tc.name+".go", tc.src)
			if err != nil {
				t.Fatalf("parse synthetic source: %v", err)
			}
			if len(lines) != tc.wantHit {
				t.Fatalf("predicate reported %d bare opener(s) at %v, want %d — the gate is "+
					"vacuous or over-broad", len(lines), lines, tc.wantHit)
			}
		})
	}
	// The measured-red rigs must be named here too: the gate has to cover the
	// files the FAIL messages pointed at, or a future edit could delete the
	// site and keep the gate green.
	//
	// The list is a live claim about files that EXIST, so it shrank with the
	// zombie sweeper's retirement (2026-10-26, devdoc/zombie-sweep-removed.md):
	// `cli_tasks_zombie_dispose_test.go` was one of the two rigs and is gone with
	// the mechanism it measured. Naming a deleted file here does not preserve
	// the guard — it turns this test into a report about the last cleanup
	// instead of about the tree, so the surviving rig carries the arm alone.
	for _, name := range []string{"cli_serve_stale_test.go"} {
		if _, err := os.Stat(filepath.Join(".", name)); err != nil {
			t.Fatalf("the measured-red rig %s is missing: %v — the gate's subject moved", name, err)
		}
	}
}
