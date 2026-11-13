package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/worktreeroots"
)

// TestPathPolicy_DestructiveArmJudgesTheRunDirectory pins the Rule-41 shape the
// shape policy got wrong on b03796d: EVERY arm resolves a relative target
// against the directory the shell is standing in when it runs that target, but
// the destructive arm resolved against the SESSION WORKDIR even when the
// command had already `cd`-ed somewhere else. The worktree arm had the run
// directory (`carriedChangeDir` feeding `targetBaseDir`); the destructive arm
// had only half of it — the cd was read as an AUTHORIZATION root
// (`commandChangeDirRoots`) and never as a RESOLUTION base.
//
// Measured 2026-10-14 on b03796d (the integration tip this branch is cut from),
// with the pre-repair code:
//
//	cd <root>/a && rm -rf ../../declared-root/build   → REFUSED
//	  "target \"/declared-root/build\" is outside every declared root"
//	the same operand with the shell's real run directory (<root>/a) resolves to
//	<root>/build — INSIDE the declared root, so the refusal names a path the
//	shell never visits.
//
// A guard that judges a fabricated path is not a stricter guard: it refuses the
// legitimate spelling (so the agent learns to spell it absolutely) while the
// shape it was built to bound is judged by a path nobody runs.
func TestPathPolicy_DestructiveArmJudgesTheRunDirectory(t *testing.T) {
	root, session, _ := guardPolicyFixture(t)
	runDir := filepath.Join(root, "a")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// (1) THE BEHAVIOUR: the target is inside the declared root once it is
	// resolved against the directory the command actually runs in.
	command := "cd " + runDir + " && rm -rf ../../" + filepath.Base(root) + "/build"
	if reason := rejectOutsideDeclaredRoots(command, session); reason != "" {
		t.Fatalf("%q was REFUSED, but its target resolves inside the declared root %q:\n%s", command, root, reason)
	}

	// (2) THE PROPERTY (one definition, both arms): the destructive arm's target
	// carries the run directory the way the worktree arm's does.
	targets := commandPathTargets(command, session)
	var deleteTarget string
	for _, tg := range targets {
		if filepath.Base(tg.path) == "build" {
			deleteTarget = tg.path
		}
	}
	if deleteTarget == "" {
		t.Fatalf("the recursive delete's operand was not extracted from %q (got %+v)", command, targets)
	}
	want := filepath.Clean(filepath.Join(runDir, "../../", filepath.Base(root), "build"))
	for _, tg := range targets {
		if tg.path != deleteTarget {
			continue
		}
		if got := targetBaseDir(tg, session); got != runDir {
			t.Fatalf("delete target %q judged against %q, want the command's run directory %q", deleteTarget, got, runDir)
		}
		if abs, ok := absolutize(tg.path, targetBaseDir(tg, session), nil); !ok || abs != want {
			t.Fatalf("delete target %q resolved to %q (ok=%v), want %q", deleteTarget, abs, ok, want)
		}
	}

	// (3) THE GUARD STILL GUARDS: an out-of-root target reached through a cd is
	// still refused, and the refusal names the path the shell really destroys.
	outside := worktreeroots.ShmDir + "/gopi-run-dir-probe"
	escaped := "cd " + runDir + " && rm -rf " + outside
	reason := rejectOutsideDeclaredRoots(escaped, session)
	if reason == "" {
		t.Fatalf("%q was ALLOWED — a recursive delete outside every declared root", escaped)
	}
	if !strings.Contains(reason, outside) {
		t.Errorf("refusal does not name the destroyed path %q: %s", outside, reason)
	}
}
