package main

// task_run_writeback_lost_gate_test.go — THE SOURCE GATE for the shared
// failed-branch writeback seam (epic writeback-lost, 2026-10-27).
//
// WHY A SOURCE GATE AND NOT AN END-TO-END. The defect is not a wrong DECISION,
// it is an ABSENT READ: five failure arms called
//
//	writebackOutcome(target, tools.TaskStatusFailed, …, opts.reportTo, opts.repo)
//
// as an expression statement, so the ledger's refusal was dropped on the floor.
// There is no observable behaviour to drive — a discarded error is invisible at
// runtime precisely because nobody looks at it — so the property is asserted
// where it lives: on the source of the production file, by parsing it.
//
// TWO assertions, and they cover each other:
//
//	① no bare `writebackOutcome(...)` EXPRESSION STATEMENT survives anywhere in
//	   cmd/agent — every failure arm goes through writebackFailure, which reads
//	   the error (the success arm's `wbErr := writebackOutcome(…)` is a
//	   DECISION and stays legal: task_run_success_path_ledger_read_test.go
//	   documents that the landed check needs the ledger's verdict);
//	② the seam is actually USED — a floor on the writebackFailure call sites,
//	   so an edit that "cleans up" the gate by deleting its subject cannot pass
//	   as green (a scan that judged nothing is a failure, not a pass).
//
// The precedent for the shape is cli_tasks_child_retry_wiring_test.go: a
// source-level property asserted by parsing the source, in this package, beside
// the file it constrains.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// writebackGateFile is the production file the seam lives in — the same
	// file the five arms were measured in.
	writebackGateFile = "cli_tasks.go"
	// writebackGateRaw is the call whose ERROR MUST BE READ on a failing arm.
	writebackGateRaw = "writebackOutcome"
	// writebackGateSeam is the one wrapper a failure arm may use.
	writebackGateSeam = "writebackFailure"
	// writebackGateMinSeamSites is the floor on seam call sites. Five failure
	// arms exist today (the run-failure, the three refusal arms, and the
	// child-exit arm); the floor is set at that measured population so a file
	// whose arms silently disappeared is refused instead of passing.
	writebackGateMinSeamSites = 5
)

// TestNoBareWritebackOutcomeCallSurvives is assertion ①: the raw writeback is
// never a statement. A discarded return value is how the defect happened, and
// the compiler cannot see it — `_ = f()` is deliberate, a bare `f()` is a
// dropped error that reads like a statement of fact.
func TestNoBareWritebackOutcomeCallSurvives(t *testing.T) {
	file := writebackGateParse(t, writebackGateFile)
	fset := writebackGateFset

	var offenders []string
	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if writebackGateCalleeName(call) != writebackGateRaw {
			return true
		}
		pos := fset.Position(call.Pos())
		offenders = append(offenders, "  "+filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line))
		return true
	})
	if len(offenders) > 0 {
		t.Fatalf("a bare %s(...) statement discards the ledger's verdict, so a REFUSED terminal write leaves "+
			"the row open with nothing on it saying why (the measured defect: four rows with dead holders, no "+
			"account of the missing write). Route the call through %s, which reads the error and records the "+
			"loss. Offending site(s):\n%s", writebackGateRaw, writebackGateSeam, strings.Join(offenders, "\n"))
	}
}

// TestWritebackFailureSeamIsActuallyUsed is assertion ②: the seam carries the
// failure arms. Without it, a file with no arms at all would satisfy ①
// vacuously.
func TestWritebackFailureSeamIsActuallyUsed(t *testing.T) {
	file := writebackGateParse(t, writebackGateFile)
	sites := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && writebackGateCalleeName(call) == writebackGateSeam {
			sites++
		}
		return true
	})
	if sites < writebackGateMinSeamSites {
		t.Fatalf("%s carries %d %s call site(s), want at least %d — either a failure arm was deleted or the "+
			"seam was renamed; re-point this gate at the new spelling rather than lowering the floor "+
			"(a gate that judged nothing must not read green)", writebackGateFile, sites, writebackGateSeam,
			writebackGateMinSeamSites)
	}
}

// TestWritebackSuccessArmStillReadsItsOwnVerdict is the CONTRAST control, and
// it is why ① does not simply ban the raw call: the success arm's landed check
// NEEDS the returned error. It pins that the success arm is still an
// ASSIGNMENT (not a bare statement), so a future "simplification" that routes
// it through the void seam would drop the one decision this epic kept.
func TestWritebackSuccessArmStillReadsItsOwnVerdict(t *testing.T) {
	file := writebackGateParse(t, writebackGateFile)
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, rhs := range as.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if ok && writebackGateCalleeName(call) == writebackGateRaw {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Fatal("the success arm no longer ASSIGNS the writeback's error — its landed check (taskRunCloseoutLanded) " +
			"needs the ledger's verdict, and this epic must not convert that decision into the reporting seam")
	}
}

// writebackGateFset is the FileSet of the last parsed production file; a
// FileSet is only meaningful together with the file it parsed, so the parse
// helper publishes both.
var writebackGateFset *token.FileSet

// writebackGateParse parses one production file of this package.
func writebackGateParse(t *testing.T, name string) *ast.File {
	t.Helper()
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("the gate's subject %q is not here (%v) — re-point it at the file that holds the wrapper's "+
			"terminal write rather than letting the scan pass over nothing", name, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	writebackGateFset = fset
	return file
}

// writebackGateCalleeName resolves a call's callee to its bare name; the
// selector arm is kept for a future refactor that qualifies the call.
func writebackGateCalleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}
