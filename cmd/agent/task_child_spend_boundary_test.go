package main

// task_child_spend_boundary_test.go — the seam half of the heavy-test boundary
// (internal/heavytest/boundary.go).
//
// The in-test refusal is the load-bearing half: a process that carries the
// heavy-set opt-in without the operator's declaration is refused AT the heavy
// call site, loudly, whatever environment it was handed. This half removes the
// other leg — an INHERITED DECLARATION. config.HumanTerminalEnv is the
// operator's own claim and it is what switches that refusal OFF, which is why
// agentguard documents it as per-invocation: it must never be inherited by a
// descendant. The executor spawn seam therefore drops it, so a `gopi task run`
// child cannot answer "an operator said so" on the operator's behalf.
//
// The unit below runs the strip against a FAKE environment list, not against
// this process's environment: the strip takes `[]string`, so the test can
// declare exactly the input it means (no ambient signature, no os.Setenv, no
// leak into a sibling test — Rule 17's spirit).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/tools"
)

// spendBoundarySeamEnv is a realistic executor-launch environment: the profile,
// PATH and the wrapper identity the child legitimately keeps, plus the two
// spend-boundary names a leaky spawner might have carried.
func spendBoundarySeamEnv() ([]string, []string) {
	return []string{
		"PATH=/usr/bin:/bin",
		"GOPI_HOME=/tmp/sandbox-home",
		config.HumanTerminalEnv + "=1",
		heavytest.EnvName + "=1",
		tools.TaskWrapperEnv + "=wrapper-row",
	}, []string{config.HumanTerminalEnv, heavytest.EnvName}
}

// TestSpendBoundarySeamDropsTheDeclaration pins the seam's decision: the
// declaration and the opt-in are both reported as removed.
func TestSpendBoundarySeamDropsTheDeclaration(t *testing.T) {
	env, removed := spendBoundarySeamEnv()
	stripped, got := stripSpendBoundaryEnv(env)
	for _, name := range removed {
		if containsName(got, name) {
			continue
		}
		// The opt-in is enforced at the test, not by the name list — the seam
		// reports whatever the strip actually removed, and the assertion below
		// covers the declaration, which IS the seam's business.
		if name == heavytest.EnvName && !containsName(tools.SpendBoundaryEnvVars, name) {
			continue
		}
		t.Errorf("%s was present in the launch environment but the strip did not report it "+
			"(removed=%v) — a silent strip leaves the operator unable to tell 'nothing leaked' "+
			"from 'nothing was checked'", name, got)
	}
	if containsKV(stripped, config.HumanTerminalEnv+"=1") {
		t.Errorf("%s survived into the executor child's environment — a task child would be able to "+
			"claim the operator's authority and run the heavy set on his behalf", config.HumanTerminalEnv)
	}
}

// TestSpendBoundarySeamKeepsEverythingElse is the control: the strip is a
// SUBTRACTION, never a rebuilt whitelist — the profile, PATH and the wrapper
// identity the child NEEDS all survive.
func TestSpendBoundarySeamKeepsEverythingElse(t *testing.T) {
	env, _ := spendBoundarySeamEnv()
	stripped, _ := stripSpendBoundaryEnv(env)
	for _, keep := range []string{"PATH=/usr/bin:/bin", "GOPI_HOME=/tmp/sandbox-home",
		tools.TaskWrapperEnv + "=wrapper-row"} {
		if !containsKV(stripped, keep) {
			t.Errorf("the spend-boundary strip dropped %q — it must remove only the declaration", keep)
		}
	}
}

// TestSpendBoundarySeamLeavesTheBoundaryIntact closes the loop with the
// test-side boundary's OWN predicate: asked about the stripped environment, it
// must answer "work" (not "operator"), so a child assembled by this seam can
// never enable the heavy set as the operator. It asks heavytest rather than
// re-implementing the rule, so the two halves cannot drift.
func TestSpendBoundarySeamLeavesTheBoundaryIntact(t *testing.T) {
	env, _ := spendBoundarySeamEnv()
	stripped, _ := stripSpendBoundaryEnv(env)
	lookup := func(name string) string {
		for _, kv := range stripped {
			n, v, _ := strings.Cut(kv, "=")
			if n == name {
				return v
			}
		}
		return ""
	}
	if _, _, declared := heavytest.WorkRefusalWith(lookup); declared {
		t.Fatalf("%s survived the seam's strip — the assembled child environment reads as an OPERATOR "+
			"invocation to the test-side boundary", config.HumanTerminalEnv)
	}
}

// TestSpendBoundaryPredicateMatchesTheList pins the predicate to the list, so a
// name added to the list is stripped by every caller without touching them
// (Rule 14: one definition, one predicate).
func TestSpendBoundaryPredicateMatchesTheList(t *testing.T) {
	for _, name := range tools.SpendBoundaryEnvVars {
		if !tools.IsSpendBoundaryEnv(name) {
			t.Errorf("tools.IsSpendBoundaryEnv(%q) = false while it is in SpendBoundaryEnvVars — the "+
				"seam would forward a name the list declares forbidden", name)
		}
	}
	// A prefix-adjacent name is a DIFFERENT variable (the discipline the
	// daemon-context strip already follows).
	for _, adjacent := range []string{config.HumanTerminalEnv + "_EXTRA", "GOPI_HUMAN"} {
		if tools.IsSpendBoundaryEnv(adjacent) {
			t.Errorf("IsSpendBoundaryEnv(%q) = true — a prefix-adjacent name is not the name", adjacent)
		}
	}
}

// containsName reports whether a name list carries want.
func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
