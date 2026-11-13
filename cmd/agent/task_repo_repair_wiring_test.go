package main

// task_repo_repair_wiring_test.go — the WIRING half of the dead-repo repair at
// the CLAIM/LAUNCH seam (epic 01a17980).
//
// WHY A SOURCE GATE INSTEAD OF A LIVE END-TO-END. The repair hangs off the one
// spawn funnel every dispatch path shares (runTaskRunOnce), between the claim and
// the clone channel — but the BEHAVIOUR it produces is only observable when a row
// is actually dispatched with a dead repo on it. Driving that here would mean
// spawning a real executor child (a model call) against the operator's ledger,
// which is exactly what this epic is forbidden to do outside its ONE owed
// reissue. So the two halves are pinned separately, each where it can be pinned
// honestly (the cli_tasks_child_retry_wiring_test precedent):
//
//   - the DECISION — repair / identity / silent miss, and the ledger write read
//     back — is exercised in-process against a real ledger and real git trees
//     (internal/tools' TaskRepairDeadRepoInPlace suite + the reopen arm);
//   - the CALL SITE is asserted here, on the source of the production function,
//     in BOTH directions: the repair must run on the dispatch path, and it must
//     run BEFORE the clone channel (the seam that refuses a dead repo). A repair
//     after the channel is a repair that never gets the chance.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

const (
	// The production file and the three identifiers this gate ties together.
	repoRepairWiringFile    = "cli_tasks.go"
	repoRepairWiringFn      = "runTaskRunOnce"
	repoRepairWiringArm     = "TaskRepairDeadRepoInPlace"
	repoRepairWiringChannel = "taskRunCloneChannelFor"
)

// TestDeadRepoRepairIsWiredAheadOfTheCloneChannel: the repair is CALLED from
// inside runTaskRunOnce, and its call site precedes the clone channel's — the
// first thing dispatch hands the row's repo to.
func TestDeadRepoRepairIsWiredAheadOfTheCloneChannel(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, repoRepairWiringFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", repoRepairWiringFile, err)
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok && d.Name.Name == repoRepairWiringFn {
			fn = d
			break
		}
	}
	if fn == nil {
		t.Fatalf("%s not found in %s — the spawn funnel moved; re-point this gate", repoRepairWiringFn, repoRepairWiringFile)
	}
	var repairLine, channelLine int
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		switch name {
		case repoRepairWiringArm:
			if repairLine == 0 {
				repairLine = fset.Position(call.Pos()).Line
			}
		case repoRepairWiringChannel:
			if channelLine == 0 {
				channelLine = fset.Position(call.Pos()).Line
			}
		}
		return true
	})
	if repairLine == 0 {
		t.Fatalf("%s does not call %s — a row would carry a dead deliverable.repo into the channel that "+
			"refuses it, which is how nineteen rows died at dispatch", repoRepairWiringFn, repoRepairWiringArm)
	}
	if channelLine == 0 {
		t.Fatalf("%s no longer calls %s — the clone channel moved; re-point this gate at the seam that "+
			"first acts on a row's repo", repoRepairWiringFn, repoRepairWiringChannel)
	}
	if repairLine > channelLine {
		t.Fatalf("%s is called at line %d, AFTER the clone channel at %d — a repair on the far side of the "+
			"refusal never runs: the row is failed by the channel first", repoRepairWiringArm, repairLine, channelLine)
	}
}
