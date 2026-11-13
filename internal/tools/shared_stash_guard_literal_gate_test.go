package tools

// Rule 13 gate for the shared-stash guard: the git subcommand word and the
// stash-invocation spellings exist as constants in shared_stash_guard.go
// (`stashCommandWord`, `stashVerbList`/`stashVerbShow`, `stashMutation*`,
// `gitBinaryName`) and are REFERENCED, never re-inlined. The package's sibling
// gate — TestFinishCleanupWorktreeVerbLiteralGate in
// task_finish_cleanup_verbs_gate_test.go — guards the worktree verbs the same
// way, and this file follows that pattern.
//
// WHY this guard needs its own gate: the whole defect family it closes was a
// SUBSTRING search over the raw command text — a bare `stash` literal compared
// against whatever the shell line happened to contain, which refused a PATH
// carrying the word and announced the wrong verb. A second spelling of the
// subcommand word in a sibling production file is exactly how that defect
// returns: the new site matches text the predicate never judged, and the two
// spellings drift (Rule 1b: the value's meaning must come from a named source).
//
// RE-AUTHORED (not re-applied) on top of current master. The archived branch
// archive/fix/stash-guard-acceptance carried this gate already, but its
// acceptance-test half (shared_stash_guard_test.go) has since landed on master
// through a sibling, so that file is no longer this gate's deliverable. The one
// semantic change from the archived version is the SCAN SET below.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sharedStashLiteralGateDefinitionFile is the ONE production file allowed to
// carry the literals — their single definition site, exempt by BASE NAME so it
// stays exempt wherever it is reached from during the walk.
const sharedStashLiteralGateDefinitionFile = "shared_stash_guard.go"

// sharedStashLiteralGateLiterals are the invocation spellings that must exist
// only at their definition site. Single generic words (`list`/`show`/`pop`) are
// deliberately NOT gated — they are ordinary English words the package spells in
// unrelated code — so the gate pins the SHAPES that can only mean a stash
// invocation.
var sharedStashLiteralGateLiterals = []string{`"stash"`, `"git stash"`, `"stash pop"`, `"stash push"`}

// sharedStashLiteralGateRoots are the trees the gate walks. They are the two
// production roots, the same pair the package's sibling gate reasons about; the
// walk is RECURSIVE, so a re-inlined spelling one directory down
// (cmd/agent/foo.go) is caught too — the failure mode this gate exists for is a
// spelling re-introduced in a file nobody thought to list.
const sharedStashLiteralGateRoots = "internal/tools,cmd"

// sharedStashLiteralGateReachableFloor is the minimum number of production
// files the walk must have read for its verdict to mean anything. A gate that
// scans nothing passes vacuously; the two roots hold 227 production .go files
// today (132 + 95), so the floor is well below the real count and can only
// break if the walk itself stops working.
const sharedStashLiteralGateReachableFloor = 50

// sharedStashLiteralGateScan walks the gate's roots once, RETURNING every
// offending pair with the file path that carries it. Exemptions: the definition
// file (by base name, wherever it is reached from) and every _test.go file (the
// acceptance test legitimately spells the commands it drives — that is the
// entry point it must exercise, Rule 31).
func sharedStashLiteralGateScan(roots []string) (hits []string, scanned int, err error) {
	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				// testdata holds fixtures, not production code.
				if name == "testdata" && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			if name == sharedStashLiteralGateDefinitionFile {
				return nil
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				hits = append(hits, path+": unreadable: "+readErr.Error())
				return nil
			}
			scanned++
			for _, lit := range sharedStashLiteralGateLiterals {
				if bytes.Contains(b, []byte(lit)) {
					hits = append(hits, path+" carries "+lit)
				}
			}
			return nil
		})
		if walkErr != nil {
			return hits, scanned, walkErr
		}
	}
	return hits, scanned, nil
}

// sharedStashLiteralGateRootsFor resolves the gate's roots relative to the
// package directory the test runs in.
func sharedStashLiteralGateRootsFor(dir string) []string {
	roots := make([]string, 0, 2)
	for _, rel := range strings.Split(sharedStashLiteralGateRoots, ",") {
		roots = append(roots, filepath.Join(dir, filepath.FromSlash(rel)))
	}
	return roots
}

// sharedStashLiteralGateRepoRoot ascends from dir until it finds the module
// root (the directory holding go.mod), so the walk is anchored to the repo
// rather than to a hard-coded number of parent hops — the test's package may
// move, and a wrong guess here would silently scan the wrong tree or nothing.
func sharedStashLiteralGateRepoRoot(t *testing.T, dir string) string {
	t.Helper()
	for cur := dir; ; {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("no go.mod found above %s — cannot anchor the gate's walk", dir)
		}
		cur = parent
	}
}

// TestSharedStashGuardVerbLiteralGate asserts no production file outside the
// guard's own definition file re-inlines the stash invocation spellings.
func TestSharedStashGuardVerbLiteralGate(t *testing.T) {
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	repoRoot := sharedStashLiteralGateRepoRoot(t, pkgDir)
	hits, scanned, err := sharedStashLiteralGateScan(sharedStashLiteralGateRootsFor(repoRoot))
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, h := range hits {
		t.Errorf("%s — reference the stashCommandWord/stashVerb*/stashMutation* constant instead (defined in %s)",
			h, sharedStashLiteralGateDefinitionFile)
	}
	if scanned < sharedStashLiteralGateReachableFloor {
		t.Fatalf("gate scanned only %d production files (floor %d) — the walk is broken and would pass vacuously",
			scanned, sharedStashLiteralGateReachableFloor)
	}
}

// TestSharedStashGuardVerbLiteralGateHasTeeth plants the literals in a throwaway
// tree and asserts the gate CATCHES each of them one directory down — a gate
// that cannot fail is not a gate. The fixture plants EVERY gated literal, so a
// literal added to the gate without a matching plant fails here rather than
// silently gating nothing.
func TestSharedStashGuardVerbLiteralGateHasTeeth(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "cmd", "agent")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	planted := "package tools\n\n" +
		"var a = \"stash\"\nvar b = \"git stash\"\nvar c = \"stash pop\"\nvar d = \"stash push\"\n"
	// A NESTED production file — the gate must find it through the walk, not by
	// listing one directory.
	if err := os.WriteFile(filepath.Join(nested, "planted_production.go"), []byte(planted), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// A _test.go file must NOT be gated (tests legally spell what they drive).
	if err := os.WriteFile(filepath.Join(nested, "planted_test.go"), []byte(planted), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// testdata holds fixtures, not production code.
	fixtures := filepath.Join(nested, "testdata")
	if err := os.MkdirAll(fixtures, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "planted_fixture.go"), []byte(planted), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// The definition file is exempt wherever it is reached from — that is its
	// whole purpose.
	if err := os.WriteFile(filepath.Join(nested, sharedStashLiteralGateDefinitionFile), []byte(planted), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	hits, scanned, err := sharedStashLiteralGateScan([]string{root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scanned != 1 {
		t.Fatalf("only the one planted production file may be scanned, got %d (%v)", scanned, hits)
	}
	if len(hits) != len(sharedStashLiteralGateLiterals) {
		t.Fatalf("the gate must catch all %d planted literals, got %v", len(sharedStashLiteralGateLiterals), hits)
	}
	for _, h := range hits {
		if !strings.Contains(h, "planted_production.go") {
			t.Fatalf("gate must catch the planted literal, got %v", hits)
		}
	}
}
