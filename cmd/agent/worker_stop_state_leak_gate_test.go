package main

// worker_stop_state_leak_gate_test.go — the regression guard for the SECOND
// face of the process-wide stop record.
//
// THE DEFECT IT PINS (measured 2026-10-17, task 01a148bc). `workerStopState` is
// a deliberate module-level record (cli_tasks_worker_stop.go: "the ONE flag
// every exit path reads"), so every test that drives it must hand it back.
// Several did not: they reset it on ENTRY and left whatever the production code
// under test had recorded standing for the rest of the binary. One of them is
// enough — `consumeDrainRequests` records a REAL stop, so after
// TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest ran, every later
// case in the package saw `workerStopRequested() == true`:
//
//	--once round:   the loop returns at its first stop check → no claim, no
//	                writeback → "task-sync-off claim gate did not fire" and
//	                "runTaskWorker returned with the file-watcher poll still on
//	                the stack" (the loop never installed it)
//	--non-once:     the loop returns at its first stop check, before the idle
//	                re-check → "no round ran" from a test that wanted one
//
// The three shapes are EXACTLY the names a full-package run reported red while
// a scoped `-run` on the same blobs was green (the briefing's "load-only"
// reading): the difference was never the load, it was WHICH OTHER TESTS the
// selection happened to include. Measured on the unchanged tree:
//
//	go test ./cmd/agent/ -run '<ack test>|<one red test>'     → FAIL
//	go test ./cmd/agent/ -run '<the red test alone>'          → ok
//
// WHY A SOURCE CENSUS AND NOT A RUNTIME CHECK. The property is "this test
// hands the record back after itself", and only the source carries it: a
// runtime guard inside TestMain cannot see WHICH case leaked, and a `-shuffle`
// run is the only runtime proof — it is expensive, and it reports the victim,
// never the culprit. The census names the culprit, costs milliseconds, and
// fails on a NEW test the day it is written (Rule 13: the exhaustive test is
// the anti-regression guard).
//
// WHAT IT DOES NOT REPLACE: resetting on entry. A test that restores but never
// resets still inherits a sibling's leftovers; both arms are required, which is
// why the census asserts the RETURN half on every test that ENGAGES the record
// (engaging = calling any of the entry points below, not merely calling the
// reset).

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

// workerStopEngageNames are the spellings that make a test engage the
// process-wide stop record. Each one either sets the flag (workerRequestStop,
// workerMarkDrainRequested, the durable-file consume) or clears it
// (resetWorkerStopState) — in both directions the record must be handed back,
// because the ONLY safe resting state for a module-level flag is "cleared".
var workerStopEngageNames = []string{
	"resetWorkerStopState",
	"workerRequestStop",
	"workerRequestStopFile",
	"workerMarkDrainRequested",
	// The HANGUP shape is a THIRD writer into the same record (a terminal
	// teardown's drain flag). It is listed here for the same reason as the other
	// two: a test that sets it must hand the record back, or the next test in the
	// package inherits a drain nobody requested.
	"workerMarkHangupDrain",
}

// workerStopRestoreName is the ONE accepted restore spelling (the helper that
// clears every field of the record, provenance included).
const workerStopRestoreName = "resetWorkerStopState"

// workerStopLeaksInFile reports the top-level test functions in src that engage
// the stop record without arranging to clear it afterwards. A restore counts as
// arranged when the body carries `t.Cleanup(resetWorkerStopState)` or
// `defer resetWorkerStopState()`.
func workerStopLeaksInFile(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "census.go", src, 0)
	if err != nil {
		return nil, err
	}
	var leaks []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		if !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		engaged, restored := false, false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if id, ok := node.Fun.(*ast.Ident); ok {
					for _, name := range workerStopEngageNames {
						if id.Name == name {
							engaged = true
						}
					}
				}
				// t.Cleanup(resetWorkerStopState)
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Cleanup" {
					for _, arg := range node.Args {
						if id, ok := arg.(*ast.Ident); ok && id.Name == workerStopRestoreName {
							restored = true
						}
					}
				}
			case *ast.DeferStmt:
				if call, ok := node.Call.Fun.(*ast.Ident); ok && call.Name == workerStopRestoreName {
					restored = true
				}
			}
			return true
		})
		if engaged && !restored {
			leaks = append(leaks, fn.Name.Name)
		}
	}
	return leaks, nil
}

// workerStopLeaks reports "<file>: <TestName>" for every leaking case under root.
func workerStopLeaks(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		leaks, err := workerStopLeaksInFile(src)
		if err != nil {
			return nil, err
		}
		for _, name := range leaks {
			out = append(out, e.Name()+": "+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// TestWorkerStopStateIsHandedBackByEveryTest is the census over the real
// package: no test may leave the process-wide stop record set.
func TestWorkerStopStateIsHandedBackByEveryTest(t *testing.T) {
	leaks, err := workerStopLeaks(".")
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if len(leaks) > 0 {
		t.Fatalf("these tests engage the process-wide stop record without handing it back:\n  %s\n\n"+
			"A leftover `stop=true` makes every LATER case in the package see a pending graceful stop: an in-process "+
			"`--once` round returns before its claim (\"claim gate did not fire\", \"no file-watcher poll on the stack\") "+
			"and a long-running round returns at its first boundary check before it re-checks the queue. Add "+
			"`t.Cleanup(resetWorkerStopState)` next to the reset — measured precedent: "+
			"devdoc/worker-stop-state-test-leak.md.",
			strings.Join(leaks, "\n  "))
	}
}

// TestWorkerStopStateCensusCatchesALeak is the RED proof: on a fixture that
// reproduces the original shape (reset on entry, real stop recorded, nothing
// handed back) the census must name the test. Without it a green real tree
// cannot be told apart from a census that never looked.
func TestWorkerStopStateCensusCatchesALeak(t *testing.T) {
	const leaking = `package main

import "testing"

func TestLeaksTheStopRecord(t *testing.T) {
	resetWorkerStopState()
	workerRequestStop("test", false)
}

func TestHandsItBack(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)
	workerRequestStop("test", false)
}

func TestNeverTouchesIt(t *testing.T) {
	_ = 1
}
`
	leaks, err := workerStopLeaksInFile([]byte(leaking))
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if len(leaks) != 1 || leaks[0] != "TestLeaksTheStopRecord" {
		t.Fatalf("census = %v, want exactly [TestLeaksTheStopRecord] — a test that never touches the record must "+
			"stay out (a census that flags everything is not a guard), and the restoring case must pass", leaks)
	}
}

// TestWorkerStopStateCensusAcceptsTheDeferSpelling: the deferred form is the
// same promise, so the census must read it as one (a guard that only knows the
// t.Cleanup spelling would red a correct test).
func TestWorkerStopStateCensusAcceptsTheDeferSpelling(t *testing.T) {
	const restored = `package main

import "testing"

func TestDefersTheRestore(t *testing.T) {
	resetWorkerStopState()
	defer resetWorkerStopState()
	workerRequestStop("test", false)
}
`
	leaks, err := workerStopLeaksInFile([]byte(restored))
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if len(leaks) != 0 {
		t.Fatalf("census = %v, want none — the defer spelling clears the record too", leaks)
	}
}
