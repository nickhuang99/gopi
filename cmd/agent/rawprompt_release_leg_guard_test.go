package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// THE GAP THIS CLOSES (bug row 01a18eb3, judged by session 01a18ef3).
//
// The bare-prompt face's worktree release leg was fixed by a722320e — a
// `defer a.releaseOwnWorktreesAtCloseout()` at the top of the ONE end hook
// every face runs (finishSessionTaskRecorded, internal/agent/taskregister.go).
// Its e2e acceptance walk (rawprompt_worktree_release_e2e_test.go) proves the
// BEHAVIOUR; nothing pinned the SHAPE. The sibling call in the same hook —
// worktreerelease.ReleaseReporting — is pinned by
// TestTaskRunReleaseIsDeferredInTheEndHook, and the asymmetry is the defect
// shape this repository has measured twice: a duty that only exists as a bare
// statement below the hook's gates is skipped by the early returns that follow
// it (the hook returns early on `!a.autoTaskRegistered || a.SessionLog == nil`,
// and a failed ledger close), which is exactly the run where a ground is left
// standing.
//
// Two properties are asserted, and both are about SHAPE, not behaviour:
//
//  1. the call is a DEFER (not a direct call), and there is exactly one of it —
//     a second direct spelling re-opens the coverage hole the defer closes;
//  2. the function it defers goes through the ONE release pass
//     (tools.ReclaimOwnWorktreesAtCloseoutWithin) rather than a hand-written
//     second implementation. Rule 13/14's "never mirror, never bare-write" is
//     the reason: a bespoke release path would drift from the arm the census
//     derives its removal set from.

// TestRawPromptFaceReleaseLegIsDeferred is the structural pin on the bare-prompt
// face's worktree release leg (see the file header for what it is defending).
func TestRawPromptFaceReleaseLegIsDeferred(t *testing.T) {
	const (
		endHook       = "finishSessionTaskRecorded"
		releaseMethod = "releaseOwnWorktreesAtCloseout"
		onePass       = "ReclaimOwnWorktreesAtCloseoutWithin"
	)
	path := endHookSourcePath(t)
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, path, nil, 0)
	if perr != nil {
		t.Fatalf("parse %s: %v", path, perr)
	}
	hook := findFuncDecl(file, endHook)
	if hook == nil {
		t.Fatalf("%s not found in %s — the guard's anchor moved, re-point it", endHook, path)
	}

	var deferred, direct int
	ast.Inspect(hook, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !selectorNamed(call, releaseMethod) {
			return true
		}
		if enclosingDefer(hook, call) {
			deferred++
		} else {
			direct++
		}
		return true
	})
	if deferred != 1 {
		t.Errorf("%s must DEFER exactly one %s call (found %d) — the bare-prompt face has no wrapper row and no other close-out tail, so a missing or extra release call is the whole difference between freeing the ground its executor created and leaking it", endHook, releaseMethod, deferred)
	}
	if direct != 0 {
		t.Errorf("%s must have exactly ONE release spelling: the deferred one (found %d direct call(s)) — a direct call is skipped by the hook's early returns, which is how a ground is left standing on exactly the run that failed", endHook, direct)
	}

	// Property 2: the deferred function routes through the ONE release pass.
	// A nil declaration is its own failure — a renamed helper would otherwise
	// make the whole file silently vacuous.
	impl := findFuncDecl(file, releaseMethod)
	if impl == nil {
		t.Fatalf("%s not found in %s — this guard would otherwise pass by finding nothing to inspect", releaseMethod, path)
	}
	var routed int
	ast.Inspect(impl, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && selectorNamed(call, onePass) {
			routed++
		}
		return true
	})
	if routed == 0 {
		t.Errorf("%s must call tools.%s — the ONE release pass (Rules 13/14: never a second spelling of the two halves). A hand-written release path drifts from the arm the removal census derives its set from", releaseMethod, onePass)
	}
}

// selectorNamed reports whether the call's fun is `<x>.<name>(...)`.
func selectorNamed(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == name
}
