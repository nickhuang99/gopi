// cli_tool_usage_family_test.go — pins the two DIMENSIONS of `gopi tool -h`
// that were aligned with the rest of the family, and pins the one that is
// deliberately NOT aligned.
//
// WHY THE ASYMMETRY IS TESTED RATHER THAN DESCRIBED. The page used to
// introduce itself in its own form (`gopi tool — …`) and to run straight from
// the title into `Usage:`, while every sibling family page says
// `GoPi <x> subcommands — …` and keeps a blank line in between
// (`cmd/agent/cli_session.go`, `cli_tasks.go`, `cli_checkpoint.go`). Neither
// dimension carried a function, so both moved. The COLUMN-ALIGNED command block
// did carry one — it is an INDEX a reader scans down, not a list of synopses —
// and it stays, which is why an arm below asserts the alignment is still there:
// a later "consistency" sweep that un-aligned it would delete the reason this
// page is shaped differently from `session`.
package main

import (
	"strings"
	"testing"
)

// TestToolUsageOpensInTheFamilyForm asserts the page's first line and the blank
// line under it, in ONE place, so the two cannot drift apart.
func TestToolUsageOpensInTheFamilyForm(t *testing.T) {
	lines := strings.Split(toolUsage, "\n")
	if len(lines) < 3 {
		t.Fatalf("toolUsage has %d lines — the page is truncated", len(lines))
	}
	if !strings.HasPrefix(lines[0], "GoPi ") || !strings.Contains(lines[0], " subcommands — ") {
		t.Errorf("first line is %q — the family form is `GoPi <x> subcommands — <what>`, which every sibling page uses", lines[0])
	}
	if lines[1] != "" {
		t.Errorf("line 2 is %q — the family pages keep a BLANK line between the title and `Usage:`", lines[1])
	}
	if lines[2] != "Usage:" {
		t.Errorf("line 3 is %q, want \"Usage:\"", lines[2])
	}
}

// TestToolUsageKeepsItsColumnAlignedBlock asserts the deviation that was NOT
// removed: the direct-command rows pad the argument shape to the block's own
// widest entry so every description starts at the same column. The check is a
// measurement (all description columns equal), not a string comparison, so a
// command added to the table cannot make the page wrong without failing here.
func TestToolUsageKeepsItsColumnAlignedBlock(t *testing.T) {
	rendered := renderToolLeafRows()
	rows := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	if len(rows) < 2 {
		t.Fatalf("renderToolLeafRows() produced %d rows — nothing to align", len(rows))
	}
	// The expected description column is DERIVED from the same table the page
	// renders from (widest `word form`, plus the two-space gap), never typed:
	// a number copied here would go stale the day a longer command is added,
	// and the test would then be asserting yesterday's page.
	width := 0
	for _, c := range toolLeafCommands {
		if w := len(c.word) + 1 + len(c.form); w > width {
			width = w
		}
	}
	want := 2 + width + 2
	for _, row := range rows {
		if !strings.HasPrefix(row, "  ") {
			t.Errorf("row %q does not start at the block's own indent", row)
			continue
		}
		if len(row) < want {
			t.Errorf("row %q is shorter than the aligned column %d", row, want)
			continue
		}
		if row[want-1] != ' ' || row[want-2] != ' ' {
			t.Errorf("row %q does not carry the two-space gap before column %d — the block is column-aligned", row, want)
			continue
		}
		if strings.TrimSpace(row[:want]) == strings.TrimSpace(row) {
			t.Errorf("row %q carries no description after column %d", row, want)
		}
	}
}
