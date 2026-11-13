package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// gopiDocPath locates the repo root's rule file (PROJECT.md since the 2026-10-19 split) by walking up from the test's
// working directory. Tests run inside the package dir, but walking up keeps
// this working from any package and from linked worktrees.
func gopiDocPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		p := filepath.Join(dir, projectContextFileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate " + projectContextFileName + " above the test working directory")
	return ""
}

// claudeToolDefNames returns the registered tool names, sorted.
func claudeToolDefNames() []string {
	names := make([]string, 0, len(claudeToolDefs))
	for _, td := range claudeToolDefs {
		names = append(names, td.Name)
	}
	sort.Strings(names)
	return names
}

// TestGOPIRule3_MatchesClaudeToolDefs closes the drift the 2026-09-07 review
// found (A3): PROJECT.md Rule 3 claimed 18 tools while claudeToolDefs registers
// 23 — git_blame, context_trim, write_task_area, task_submit and
// task_evaluate postdated the rule, so the "HARD CONSTRAINT" file asserted a
// false inventory and forbade tools the agent already had. GOPI.md is read at
// startup as a constraint, so a stale list is not a docs bug, it is a prompt
// bug. The doc names claudeToolDefs as the source of truth; this test holds
// the two together.
func TestGOPIRule3_MatchesClaudeToolDefs(t *testing.T) {
	raw, err := os.ReadFile(gopiDocPath(t))
	if err != nil {
		t.Fatalf("read GOPI.md: %v", err)
	}
	doc := string(raw)

	start := strings.Index(doc, "## Rule 3: Tools Are Minimal")
	if start < 0 {
		t.Fatal("GOPI.md has no Rule 3 heading")
	}
	end := strings.Index(doc[start:], "\n## ")
	if end < 0 {
		t.Fatal("Rule 3 is the last section — expected a following heading")
	}
	rule3 := doc[start : start+end]

	// The inventory line follows the "GoPi has N tools:" prefix.
	const marker = "GoPi has "
	i := strings.Index(rule3, marker)
	if i < 0 {
		t.Fatal("Rule 3 does not state the tool inventory")
	}
	rest := rule3[i+len(marker):]
	// Count comes first, then the comma-separated names up to the period.
	countEnd := strings.Index(rest, " tools")
	if countEnd < 0 {
		t.Fatal("Rule 3 does not state the tool count")
	}
	declared := strings.Split(strings.TrimSpace(rest[:countEnd]), " ")[0]
	listEnd := strings.Index(rest[countEnd:], ".\n")
	if listEnd < 0 {
		t.Fatal("Rule 3 tool inventory line is not terminated by a period")
	}
	names := map[string]bool{}
	for _, n := range strings.Split(rest[countEnd+len(" tools:"):countEnd+listEnd], ",") {
		n = strings.TrimSpace(n)
		if n != "" {
			names[n] = true
		}
	}

	if len(claudeToolDefs) != len(names) {
		t.Errorf("GOPI.md Rule 3 lists %d tools, claudeToolDefs registers %d", len(names), len(claudeToolDefs))
	}
	if declared != strconv.Itoa(len(claudeToolDefs)) {
		t.Errorf("GOPI.md Rule 3 says %q tools; claudeToolDefs registers %d", declared, len(claudeToolDefs))
	}
	for _, n := range claudeToolDefNames() {
		if !names[n] {
			t.Errorf("tool %q is registered in claudeToolDefs but missing from GOPI.md Rule 3", n)
		}
	}
	for n := range names {
		found := false
		for _, td := range claudeToolDefs {
			if td.Name == n {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool %q is listed in GOPI.md Rule 3 but not registered in claudeToolDefs", n)
		}
	}
}
