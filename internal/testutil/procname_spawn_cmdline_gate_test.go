package testutil

// The regression guard for the shape that made
// TestRealWorkerIncarnationMatchesProcStat intermittently RED (task
// 01a12df6-a237-7a1a-a1a0-37da52723a54).
//
// THE DEFECT, as the kernel actually behaves (measured, this host= nick-sager,
// 2026-10-12, production reader internal/procname, 200 trials):
//
//	exec.Command(...).Start() returns once the FORK is done; the child has not
//	necessarily reached execve. In that window /proc/<pid>/cmdline EXISTS and
//	holds the fork-time junk (0 bytes, or the comm padded to 16 — measured 37×
//	 0 bytes and 52× exactly 8 bytes for a 7-digit pid), the pid directory and
//	/proc/<pid>/stat are BOTH fully readable, and the process state reads R.
//	A SINGLE cmdline read taken right after Start() therefore answered "no
//	argv" for a perfectly alive process in 89 of 200 trials (44.5%) — and in
//	22 of 200 (11%) in an independent run — which is exactly the reading the
//	pre-fix code turned into `t.Fatalf("cannot read the cmdline of the probe
//	process %d", pid)`.
//
// THE SHAPE THIS GATE REFUSES (the pre-fix source, kept verbatim below in
// spawnCmdlineGatePreFixFragment):
//
//	argv, ok := procname.Cmdline(root, pidStr)
//	if !ok {
//	        t.Fatalf("cannot read the cmdline of the probe process %d", pid)
//	}
//
// The read is not wrong — an empty cmdline is genuinely NO evidence, so the
// ASSERTION must not be relaxed. What is wrong is reading ONCE and declaring
// failure: the read has to be RETRIED (internal/procname's shared
// CmdlineAwaitAttempts × CmdlineAwaitStep budget) until the kernel answers.
//
// WHAT THIS GATE IS, AND IS NOT (Rule 15: an honest scope beats a confident
// one): it is a SHAPE detector, deliberately narrow. It fires on the exact
// pre-fix construct — one bare procname.Cmdline call whose ok result is
// immediately negated into a test-fatal call, with no intervening retry — and
// it is blind to any re-spelling with the same defect (the read split across
// two statements, a helper that reads once and returns ok, a non-fatal report).
// It is a guard against the regression that ALREADY happened, not a proof that
// every current call site is race-free — the audit in the task report carries
// that half, and it is prose a reader must judge.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// spawnCmdlineGatePackage / spawnCmdlineGateCall are the reader under
	// scrutiny: procname.Cmdline (Rule 1b — the name is spelled once, here).
	spawnCmdlineGatePackage = "procname"
	spawnCmdlineGateCall    = "Cmdline"
	// spawnCmdlineGateOKVar is the result name the pre-fix shape used for the
	// reader's second return value. Only this name participates: a call whose
	// ok is not immediately negated into a fatal is not the shape.
	spawnCmdlineGateOKVar = "ok"
	// spawnCmdlineGateEscape is the inline reviewed-exception comment, the repo's
	// usual convention (Rule 13/16 style). A site carrying it is exempt and must
	// say WHY in the same line.
	spawnCmdlineGateEscape = "cmdline-race-ok:"
	// spawnCmdlineGateFatalMethods are the calls that turn an empty reading into
	// a test failure — the "declare failure without retrying" half of the shape.
	spawnCmdlineGateFatalMethods = "Fatal|Fatalf|Error|Errorf"
	// spawnCmdlineGateAssignLhs/Rhs are the shape's arity: `argv, ok := call(...)`.
	spawnCmdlineGateAssignLhs = 2
	spawnCmdlineGateAssignRhs = 1
	// spawnCmdlineGateAwait is the name of the ONE retry helper every just-spawned
	// read must go through in this package. It exists so the fixed shape is
	// recognizable and so the read cannot be re-inlined: the gate's green run is a
	// statement about reads that go through it.
	spawnCmdlineGateAwait = "awaitProbeCmdline"
)

// The scan roots: the Go trees (Rule 32/39's scope convention — internal/ + cmd/).
var spawnCmdlineGateScanDirs = []string{"internal", "cmd"}

const (
	spawnCmdlineGateTestSuffix = "_test.go"
	spawnCmdlineGateGoMod      = "go.mod"
	// spawnCmdlineGateContent is the prefilter: the EXACT call text. A bare
	// by-call read always contains it, so the prefilter cannot hide the shape —
	// it only skips files that hold no call at all (the AST scan remains the
	// detector, this is a cheap gate before the parser).
	spawnCmdlineGateContent = "procname.Cmdline("
	// spawnCmdlineGateCarrierFile is the file whose PRE-FIX content caused this
	// task's red. The scan must reach it, or a clean result says nothing about the
	// defect that was actually observed (Rule 15: an empty scan must be
	// distinguishable from a scan that never ran).
	spawnCmdlineGateCarrierFile = "procname_triple_redproof_test.go"
	// spawnCmdlineGateMinFiles is the floor on files actually parsed. MEASURED
	// 2026-10-12 in this worktree: 4 test files under internal/ + cmd/ carry the
	// literal call text procname.Cmdline( — the two other files a bare
	// `grep procname.Cmdline` reports (live_claim_worktree_veto_test.go,
	// procname_gate_test.go) spell CmdlineName / CmdlineAwaitAttempts and hold no
	// call at all. The floor sits one below the measurement so an ordinary
	// consolidation does not fail the suite, while a collapsed Walk still does —
	// and the carrier assertion below independently pins the file that matters.
	spawnCmdlineGateMinFiles = 3
	// spawnCmdlineGateFileMode is the fixture file mode for the red proof.
	spawnCmdlineGateFileMode = 0o644
)

// spawnCmdlineGateFatalCall reports whether an expression statement is a call to
// one of the test-fatal methods (t.Fatalf / t.Errorf / a bare Fatal).
func spawnCmdlineGateFatalCall(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	for _, m := range strings.Split(spawnCmdlineGateFatalMethods, "|") {
		if sel.Sel.Name == m {
			return true
		}
	}
	return false
}

// spawnCmdlineGateFatalOnNotOK reports whether stmt is `if !<okName> { … t.Fatal… }`
// — the "one read, then declare the probe broken" decision the retry replaces.
func spawnCmdlineGateFatalOnNotOK(stmt ast.Stmt, okName string) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok {
		return false
	}
	un, ok := ifStmt.Cond.(*ast.UnaryExpr)
	if !ok || un.Op != token.NOT {
		return false
	}
	id, ok := un.X.(*ast.Ident)
	if !ok || id.Name != okName {
		return false
	}
	for _, inner := range ifStmt.Body.List {
		if spawnCmdlineGateFatalCall(inner) {
			return true
		}
	}
	return false
}

// spawnCmdlineGateIsBareReader reports whether expr is exactly procname.Cmdline(…).
func spawnCmdlineGateIsBareReader(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != spawnCmdlineGateCall {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == spawnCmdlineGatePackage
}

// spawnCmdlineGateViolations parses ONE Go test file and returns a sentence per
// bare-read-then-fatal site. A site carrying the inline escape is exempt.
func spawnCmdlineGateViolations(path string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	// Lines carrying the inline escape comment are reviewed exceptions.
	escaped := map[int]bool{}
	for i, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, spawnCmdlineGateEscape) {
			escaped[i+1] = true
		}
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		blk, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i := 0; i+1 < len(blk.List); i++ {
			as, ok := blk.List[i].(*ast.AssignStmt)
			if !ok || len(as.Lhs) != spawnCmdlineGateAssignLhs || len(as.Rhs) != spawnCmdlineGateAssignRhs {
				continue
			}
			if !spawnCmdlineGateIsBareReader(as.Rhs[0]) {
				continue
			}
			okIdent, ok := as.Lhs[1].(*ast.Ident)
			if !ok || okIdent.Name != spawnCmdlineGateOKVar {
				continue
			}
			// The shape is the PAIR: the bare read, and the very next statement
			// turning its ok into a failure. Anything in between (a retry, a
			// helper call) means the failure is not declared on one reading.
			if !spawnCmdlineGateFatalOnNotOK(blk.List[i+1], okIdent.Name) {
				continue
			}
			line := fset.Position(as.Pos()).Line
			if escaped[line] {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d", path, line))
		}
		return true
	})
	return out, nil
}

// spawnCmdlineGateRepoRoot walks up from the test's working directory to the
// directory holding go.mod.
func spawnCmdlineGateRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, spawnCmdlineGateGoMod)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s above %s — cannot locate the repo root to scan", spawnCmdlineGateGoMod, dir)
		}
		dir = parent
	}
}

// TestBareSpawnedCmdlineReadIsGated is the GREEN half: no test in internal/ or
// cmd/ declares a probe broken on the strength of ONE cmdline read.
func TestBareSpawnedCmdlineReadIsGated(t *testing.T) {
	root := spawnCmdlineGateRepoRoot(t)
	scanned := 0
	sawCarrier := false
	var violations []string
	for _, dir := range spawnCmdlineGateScanDirs {
		base := filepath.Join(root, dir)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, spawnCmdlineGateTestSuffix) {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// Cheap prefilter (NOT the detection — the parser decides): a by-call
			// read always carries the call text, so a file holding one is never
			// skipped; this only spares the parser the files with no call at all.
			if !bytes.Contains(src, []byte(spawnCmdlineGateContent)) {
				return nil
			}
			scanned++
			if filepath.Base(path) == spawnCmdlineGateCarrierFile {
				sawCarrier = true
			}
			got, err := spawnCmdlineGateViolations(path)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			violations = append(violations, got...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	if scanned < spawnCmdlineGateMinFiles {
		t.Fatalf("only %d test files carrying %q were scanned under %v, floor is %d — a scan this small proves "+
			"nothing about the tree", scanned, spawnCmdlineGateContent, spawnCmdlineGateScanDirs, spawnCmdlineGateMinFiles)
	}
	if !sawCarrier {
		t.Fatalf("the scan never reached %s, the file whose PRE-FIX content caused this task's red — a clean "+
			"verdict from a scan that missed the carrier says nothing", spawnCmdlineGateCarrierFile)
	}
	for _, v := range violations {
		t.Errorf("%s reads %s.%s ONCE and turns its ok into a test failure — a process that was just spawned may not "+
			"have reached execve yet, so that read answers \"no argv\" for a live process (measured 89/200, 44.5%%, "+
			"on this host). Route the read through %s (the shared %s budget: CmdlineAwaitAttempts × CmdlineAwaitStep), "+
			"or carry `// %s <reason>` if this site genuinely cannot see a just-spawned process",
			v, spawnCmdlineGatePackage, spawnCmdlineGateCall, spawnCmdlineGateAwait,
			spawnCmdlineGatePackage, spawnCmdlineGateEscape)
	}
	t.Logf("scanned %d test files under %v; bare-read-then-fatal sites: %d", scanned, spawnCmdlineGateScanDirs, len(violations))
}

// TestBareSpawnedCmdlineReadGateRedProof drives the gate with the PRE-FIX source
// itself — the exact bytes that produced this task's red — so "the gate would
// have caught it" is a measured claim, not a hope (Rule 5: every bug fix ships
// its minimal repro). It also drives the POST-FIX shape to prove the gate is not
// merely always-red.
func TestBareSpawnedCmdlineReadGateRedProof(t *testing.T) {
	dir := t.TempDir()
	redPath := filepath.Join(dir, "prefix_fixture"+spawnCmdlineGateTestSuffix)
	redSrc, err := format.Source([]byte(spawnCmdlineGatePreFixFragment))
	if err != nil {
		t.Fatalf("the pre-fix fixture does not parse (the red proof would be testing a typo, not the shape): %v", err)
	}
	if err := os.WriteFile(redPath, redSrc, spawnCmdlineGateFileMode); err != nil {
		t.Fatalf("write the pre-fix fixture: %v", err)
	}
	got, err := spawnCmdlineGateViolations(redPath)
	if err != nil {
		t.Fatalf("the gate could not parse the pre-fix fixture: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the gate found %d bare-read sites in the pre-fix fixture (%v), want exactly 1 — a gate that does "+
			"not fire on the shape that caused this task's red cannot stop it returning", len(got), got)
	}

	// The escape must exempt the same site — otherwise the exemption is a promise
	// the gate does not keep.
	escPath := filepath.Join(dir, "escaped_fixture"+spawnCmdlineGateTestSuffix)
	escSrc, err := format.Source([]byte(spawnCmdlineGateEscapedFragment))
	if err != nil {
		t.Fatalf("the escaped fixture does not parse: %v", err)
	}
	if err := os.WriteFile(escPath, escSrc, spawnCmdlineGateFileMode); err != nil {
		t.Fatalf("write the escaped fixture: %v", err)
	}
	got, err = spawnCmdlineGateViolations(escPath)
	if err != nil {
		t.Fatalf("the gate could not parse the escaped fixture: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a site carrying `// %s <reason>` was still reported (%v) — the exemption is a promise the gate does "+
			"not keep", spawnCmdlineGateEscape, got)
	}

	// The POST-FIX shape must be CLEAN: the retry helper is the shape the gate
	// exists to keep available.
	fixPath := filepath.Join(dir, "fixed_fixture"+spawnCmdlineGateTestSuffix)
	fixSrc, err := format.Source([]byte(spawnCmdlineGatePostFixFragment))
	if err != nil {
		t.Fatalf("the post-fix fixture does not parse: %v", err)
	}
	if err := os.WriteFile(fixPath, fixSrc, spawnCmdlineGateFileMode); err != nil {
		t.Fatalf("write the post-fix fixture: %v", err)
	}
	got, err = spawnCmdlineGateViolations(fixPath)
	if err != nil {
		t.Fatalf("the gate could not parse the post-fix fixture: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the post-fix shape (a read routed through %s) was reported as a violation (%v) — the gate would "+
			"refuse the very fix this task landed", spawnCmdlineGateAwait, got)
	}
}

// TestAwaitProbeCmdlineBudgetCoversTheMeasuredWindow keeps the retry's budget
// pinned to the window it exists for: a future editor who trims
// CmdlineAwaitAttempts × CmdlineAwaitStep down past the measured window would
// turn a rare red into a frequent one, and this is the check that refuses it.
func TestAwaitProbeCmdlineBudgetCoversTheMeasuredWindow(t *testing.T) {
	budget := time.Duration(cmdlineProbeAttempts) * cmdlineProbeStep
	if budget < spawnCmdlineGateMeasuredWindowFloor {
		t.Errorf("the retry budget is %s, below the measured floor of %s — the window this retry closes was measured "+
			"at mean 39.7µs / max 59.15µs over 200 trials on this host, so a budget under %s does not cover it",
			budget, spawnCmdlineGateMeasuredWindowFloor, spawnCmdlineGateMeasuredWindowFloor)
	}
	if cmdlineProbeAttempts <= 1 {
		t.Errorf("cmdlineProbeAttempts = %d: a single attempt is not a retry — that IS the pre-exec defect",
			cmdlineProbeAttempts)
	}
}

// spawnCmdlineGateMeasuredWindowFloor is the window the retry must cover. The
// rollout measured mean 39.7µs and max 59.15µs (200 trials, this host); 1ms is
// ~17× the widest observation, so the budget is not tuned to a single sample
// while still being far below any budget a slow host would find surprising.
const spawnCmdlineGateMeasuredWindowFloor = time.Millisecond

// spawnCmdlineGatePreFixFragment is the PRE-FIX source, transcribed verbatim
// from the red run's HEAD (2dda4b0bf, internal/testutil/procname_triple_redproof_test.go
// lines 233-235). It is the artifact under test: the gate is red on it.
const spawnCmdlineGatePreFixFragment = `package fixture

import (
	"strconv"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

func fixture(t *testing.T, root string, pid int) {
	pidStr := strconv.Itoa(pid)
	argv, ok := procname.Cmdline(root, pidStr)
	if !ok {
		t.Fatalf("cannot read the cmdline of the probe process %d", pid)
	}
	_ = argv
}
`

// spawnCmdlineGateEscapedFragment is the same shape carrying the reviewed
// exception: the gate must exempt it (a gate whose escape hatch does not work
// gets worked around instead of used).
const spawnCmdlineGateEscapedFragment = `package fixture

import (
	"strconv"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

func fixture(t *testing.T, root string, pid int) {
	pidStr := strconv.Itoa(pid)
	argv, ok := procname.Cmdline(root, pidStr) // cmdline-race-ok: caller proves the process already reached execve
	if !ok {
		t.Fatalf("cannot read the cmdline of the probe process %d", pid)
	}
	_ = argv
}
`

// spawnCmdlineGatePostFixFragment is the shape this task's fix landed: the read
// goes through the shared bounded retry, so no single empty reading is a failure.
const spawnCmdlineGatePostFixFragment = `package fixture

import (
	"strconv"
	"testing"
)

func fixture(t *testing.T, root string, pid int) {
	pidStr := strconv.Itoa(pid)
	argv := requireProbeCmdline(t, root, pid, "the real probe process's own cmdline")
	live := argv
	_ = live
	_ = pidStr
}
`
