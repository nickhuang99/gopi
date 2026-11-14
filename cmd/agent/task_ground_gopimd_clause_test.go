package main

// task_ground_gopimd_clause_test.go — PROJECT.md Rule 6a's DEFERRAL CLAUSE, pinned
// as a regression (epic ground-announcement, 2026-09-25).
//
// THE PROSE IS THE MECHANISM HERE, which is why it gets a test like any other
// surface. The spare worktree is not created by gopi's Go code — it is created
// by a MODEL following PROJECT.md Rule 6a's instruction to build "a worktree for
// YOUR BRANCH". The ground's branch (task/<uuid>) matches nothing in that
// literal test, so a compliant run built a second worktree every time. The fix
// is therefore half code (the announcement) and half prose (the clause telling
// the run to defer to it), and a clause that can be edited away without a
// failing test is not a fix.
//
// THREE ASSERTIONS, and the third is the constraint on the other two:
//
//  1. the clause is present, near the top of Rule 6a, and names the ground;
//  2. it carries the DEFERRAL (that IS your ground / do NOT create a second
//     one) and BANS the rename (`git branch -m` must appear nowhere in it) —
//     the no-ground steps below it must stay, and stay reachable;
//  3. the anti-force-spellings passage is UNTOUCHED and still names its gate.
//     The clause is inserted into gated prose (internal/branchform +
//     TestBranchForceGateRedProof both read this file), so a "helpful" edit that
//     rewrote or dropped the banned-spelling paragraph would pass 1 and 2 and
//     fail 3.
//
// Rule 6a's reader is the model, and the file is shipped as the child's project
// context, so the assertions are on the shipped artifact — read from the
// repo root, the same file every run loads.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// groundProjectMDPath resolves the repo root's PROJECT.md from this test's cwd
// (the package directory, cmd/agent) — the ONE place this file spells the
// project context file's name is internal/agent's projectContextFileName, but
// that package is not importable for a plain path here and the file's position
// is fixed by PROJECT.md itself.
//
// PROJECT.md, NOT GOPI.md (changed when the rules file was split, commit
// 484583a). PROJECT.md is TRACKED, so it travels with every clone and worktree
// and exists wherever this test runs; GOPI.md is the client's own machine-local
// policy file, gitignored at the repo root, so a test reading it passes on the
// operator's box and dies in every clone — measured: "stat …/GOPI.md: no such
// file or directory" in a fresh linked worktree. Rule 6a's clause lives in
// PROJECT.md; asserting the shipped artifact means asserting the file the run
// actually loads from a clone.
func groundProjectMDPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	p := filepath.Join(root, "PROJECT.md")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return p
}

// TestGOPIMDRule6aDefersToTheGroundTheRunnerSupplied pins the clause and its
// boundaries.
func TestGOPIMDRule6aDefersToTheGroundTheRunnerSupplied(t *testing.T) {
	data, err := os.ReadFile(groundProjectMDPath(t))
	if err != nil {
		t.Fatalf("read PROJECT.md: %v", err)
	}
	text := string(data)

	// ① The clause, identified by the SAME label the announcement renders
	// (tools.TaskGroundAnnouncementLabel — Rules 1/13/14: the briefing text and
	// the rule that tells the run how to read it must not drift apart).
	if !strings.Contains(groundNormWS(text), tools.TaskGroundAnnouncementLabel) {
		t.Fatalf("PROJECT.md must name the briefing block it defers to (%q) — a deferral the run cannot recognise is not a deferral\n"+
			"the announcement label lives in internal/tools; the rule must quote it",
			tools.TaskGroundAnnouncementLabel)
	}
	// ② The deferral itself, in Rule 6a's own section. The section's end is the
	// next "### " heading, so the assertions cannot be satisfied by prose that
	// drifted into another rule (6b, 6c, …) where no reader of 6a would find it.
	//
	// The rename assertion INVERTED on 2026-10-07 (epic: the task uuid is the
	// branch name). Rule 6a used to bless `git branch -m <descriptive-name>`,
	// and that blessing is what severed the ref from the row — measured
	// specimen `task/crelease`, which no ledger row resolves. So the section
	// must now carry the BAN, and the blessing must be absent from it: a
	// clause asserted only for its new sentence could be edited back with the
	// old one beside it.
	section := groundNormWS(groundRule6aSection(t, text))
	// The phrases are pinned CASE-INSENSITIVELY: the two bans open sentences
	// ("Do not rename its branch either", "do NOT create a second one"), so a
	// case-sensitive pin would fail on the shipped capital and pass only on a
	// paragraph rewritten to suit the test. The vocabulary is asserted, not the
	// capitalization.
	for _, want := range []string{
		"do not create a second one", // the ban — the spare-worktree fix's whole point
		"do not rename its branch",   // the ban that keeps the ref readable as a row address
		"gopi task show --branch",    // WHY — the door the uuid name is read back through
		// The deferral's affirmative half. It names the GROUND, not a
		// worktree: since 5a30b40 the ground's SHAPE is measured
		// (devworkspace.GroundModelOf — a clone-channel ground is a CLONE),
		// so "that IS your worktree" would re-state the error that commit
		// removed. The deferral is shape-neutral by design: whatever the
		// runner created, this is it.
		"that is your ground",
	} {
		if !strings.Contains(strings.ToLower(section), want) {
			t.Errorf("Rule 6a must carry %q (case-insensitive) — the clause is what stops a compliant run building a spare or severing the row from its ref:\n%s", want, section)
		}
	}
	if strings.Contains(section, "git branch -m") {
		t.Errorf("Rule 6a must NOT bless the branch rename (`git branch -m`): the branch name IS the ledger address, and a rename leaves a ref no row can be read off:\n%s", section)
	}
	// ③ THE MODEL-FACING CREATION RECIPE IS GONE, AND THAT IS THE ASSERTION
	// (epic worktree-removal, batch 1, 2026-10-27). Until that batch this arm
	// pinned the opposite — the three-step "make your own worktree" recipe,
	// verbatim — on the grounds that "a run with no ground must still make its
	// own worktree".
	//
	// THE MEASUREMENT THAT REVERSED IT, and why the two halves of that batch
	// were graded differently: the batch first read "207 of 230 ground-shaped
	// dirs are clones, 23 are linked" as "the linked-worktree channel is dead"
	// and deleted GOPI's OWN creation leg (tools.TaskRunGroundFor's create arm).
	// That turned 51 tests red — every e2e walk that sets up a repo with no
	// dev_repo and still expects a ground — so the RUNNER's creation legs were
	// restored: a ground is a CLONE when dev_repo is declared and a LINKED
	// WORKTREE when it is not. What stayed deleted is the recipe below, and the
	// distinction is load-bearing: the RUNNER creates the ground in both shapes
	// (this test's own subject: "the ground the runner supplied"), while a MODEL
	// that made one of its own produced a SECOND worktree that no sweep
	// collected — `internal/tools/task_run_ground.go`'s create arm exists for
	// the runner's call, never for the agent's.
	//
	// ASSERTING ABSENCE IS ONLY HALF A TEST, so the retirement is asserted
	// AFFIRMATIVELY as well: the section must SAY that a run never creates a
	// worktree, must name both shapes a ground can have, and must name the
	// channel that decides which. A rule that merely lost its steps leaves the
	// next reader to invent them again.
	for _, gone := range []string{
		"git worktree add ~/workspace/gopi-<task> -b feat/<name>",
		"Always use a linked worktree — never clone by default.",
	} {
		if strings.Contains(section, gone) {
			t.Errorf("Rule 6a must NOT carry the retired model-facing creation step %q — the RUNNER supplies the ground in both shapes, and a run that builds its own leaves a spare no sweep collects:\n%s", gone, section)
		}
	}
	for _, want := range []string{
		"a run never creates a worktree", // the retirement, stated
		"clone",                          // shape 1
		"linked worktree",                // shape 2 — the fallback that 51 red tests proved live
		"dev_repo",                       // the channel that decides which
	} {
		if !strings.Contains(strings.ToLower(section), want) {
			t.Errorf("Rule 6a must state the retirement affirmatively, naming both ground shapes and the channel that decides; missing %q:\n%s", want, section)
		}
	}
	// ④ The anti-force-spellings passage is untouched — this is the constraint
	// the epic names explicitly, and the clause is inserted into the very
	// section that passage lives in.
	for _, want := range []string{
		"A TAKEN NAME MEANS PICK ANOTHER NAME — never make room for it.",
		"the banned vocabulary",
		"TestBranchForceGateRedProof",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("Rule 6a's anti-force-spellings passage must be untouched; missing %q:\n%s", want, section)
		}
	}
}

// TestGOPIMDRule6aClauseDoesNotPrescribeAForceSpelling is the local half of the
// branch-force gate (Rule 6a is scanned by internal/standards; this asserts the
// same property in the package that owns the clause, so the failure names the
// edit rather than a generic gate verdict).
//
// The check is on the CLAUSE, not the whole file — the file legitimately NAMES
// the banned flags in the passage the epic protects, and a whole-file scan would
// either fire on that (a false positive) or have to re-implement the gate.
func TestGOPIMDRule6aClauseDoesNotPrescribeAForceSpelling(t *testing.T) {
	data, err := os.ReadFile(groundProjectMDPath(t))
	if err != nil {
		t.Fatalf("read PROJECT.md: %v", err)
	}
	norm := groundNormWS(string(data))
	clauseStart := strings.Index(norm, groundRule6aClauseOpener)
	if clauseStart < 0 {
		t.Fatalf("the deferral clause is missing from PROJECT.md")
	}
	clause := norm[clauseStart:]
	// The clause ends where the RETIREMENT paragraph begins. It used to end at
	// "**Always use a linked worktree" — the model-facing creation recipe — which
	// the worktree-removal batch deleted; with that marker gone the slice runs to
	// the end of the section and swallows the anti-force-spellings passage, which
	// legitimately NAMES the banned flags (the false positive this boundary
	// exists to avoid). The retirement paragraph's own opener is the boundary
	// now, and arm ③ pins that sentence, so a rewrite which removes it fails
	// there rather than silently widening this clause.
	if end := strings.Index(clause, "**A RUN NEVER CREATES A WORKTREE"); end >= 0 {
		clause = clause[:end]
	}
	// A verb plus a force flag in ONE segment is the destructive form; the
	// clause has no reason to contain either, since it BLESSES a rename.
	for _, banned := range []string{"-B ", "-B\n", "-f ", "checkout -B", "switch -C", "branch -f"} {
		if strings.Contains(clause, banned) {
			t.Errorf("the deferral clause must not prescribe the banned force spelling %q (internal/branchform owns the vocabulary; TestBranchForceGateRedProof enforces it):\n%s",
				banned, clause)
		}
	}
}

// groundRule6aClauseOpener is the deferral clause's own opening sentence — the
// anchor this file slices the clause from. It is asserted PRESENT by the
// companion test (through tools.TaskGroundAnnouncementLabel, which is what the
// clause quotes), so a slice taken from here can never silently begin somewhere
// that is no longer the clause.
const groundRule6aClauseOpener = "THE RUNNER MAY HAVE GIVEN YOU YOUR GROUND ALREADY"

// groundNormWS collapses every whitespace run to a single space. PROJECT.md is
// hard-wrapped prose, so a phrase this file pins can legitimately span two
// source lines — comparing normalized text asserts the SENTENCE, not the
// wrapping (a test that failed on a re-wrap would be testing the line width).
func groundNormWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// groundRule6aSection returns Rule 6a's own text: from its heading to the next
// third-level heading (or EOF).
func groundRule6aSection(t *testing.T, text string) string {
	t.Helper()
	const heading = "### 6a. "
	start := strings.Index(text, heading)
	if start < 0 {
		t.Fatalf("PROJECT.md has no %q heading — Rule 6a moved or was renamed", heading)
	}
	rest := text[start+len(heading):]
	if end := strings.Index(rest, "\n### "); end >= 0 {
		rest = rest[:end]
	}
	if strings.TrimSpace(rest) == "" {
		t.Fatalf("Rule 6a's section is empty")
	}
	return rest
}
