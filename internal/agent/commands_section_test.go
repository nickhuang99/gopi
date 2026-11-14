package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// The DERIVATION GATE. `commands` is the only prompt section whose text this
// package does not write: it renders internal/config's commandIndex, the one
// table `gopi -h` prints. So the density budget cannot judge it (its length is
// that table's business — see derivedPromptSectionNames) and three other
// properties have to be checked instead, in this order:
//
//	1. the block IS derived — byte-equal to `gopi -h`'s own index, so a
//	   commandIndex edit moves both surfaces together and a verb retirement
//	   removes the row from both;
//	2. nothing else in this package carries a copy of it (Rule 14) — the
//	   section is a Render over the foreign table, never a Text;
//	3. it reaches the ASSEMBLED prompt — the defect being repaired was a
//	   capability that existed in a constant nothing rendered.
//
// A green here is what replaces a character ceiling: it says the two surfaces
// cannot disagree, which is a stronger claim than "the block is small".

// helpIndexPreamble is the line `gopi -h` prints immediately above the index
// rows. The extraction below cuts from it, so the test proves the section is
// the SAME REGION of the page a reader sees, not merely some substring.
const helpIndexPreamble = "Commands (each accepts -h for its full help"

// helpExamplesHeading is the heading that ends the index region on the help
// page. It is the cut's other edge.
const helpExamplesHeading = "Examples:"

// helpIndexRegion returns the COMMANDS region of `gopi -h` exactly as the page
// renders it: everything after the preamble paragraph and before `Examples:`,
// with the leading/trailing blank lines trimmed. It is the oracle the prompt's
// section is compared against, read from config.FormatUsage() — the function
// the binary's own `-h` prints — rather than from a transcription.
func helpIndexRegion(t *testing.T) string {
	t.Helper()
	page := config.FormatUsage()
	start := strings.Index(page, helpIndexPreamble)
	if start < 0 {
		t.Fatalf("`gopi -h` carries no %q preamble — the index region cannot be located", helpIndexPreamble)
	}
	region := page[start:]
	// Drop the preamble paragraph itself: the prompt block has its own marker
	// and must not repeat the shell user's sentence about `-h` per command.
	if i := strings.Index(region, "\n\n"); i >= 0 {
		region = region[i:]
	}
	end := strings.Index(region, helpExamplesHeading)
	if end < 0 {
		t.Fatalf("`gopi -h` has no %q heading after its index — the region's right edge is unknown", helpExamplesHeading)
	}
	return strings.TrimSpace(region[:end])
}

// TestCommandsSectionIsDerivedFromTheHelpIndex is property 1: the rendered
// section is the help page's own index, byte for byte. A hand-maintained copy
// in this package would satisfy "the prompt lists commands" and fail this.
func TestCommandsSectionIsDerivedFromTheHelpIndex(t *testing.T) {
	got := commandsSection()
	want := commandsSectionMarker + "\n" + helpIndexRegion(t)
	if got != want {
		t.Errorf("the prompt's command block is not `gopi -h`'s index.\n--- prompt block ---\n%s\n--- help index ---\n%s",
			got, want)
	}
	if got != want {
		// Report WHERE they diverge: a single moved row is the realistic
		// failure (a summary edited on one side only), and a full dump of two
		// 5KB blocks is unreadable in a test log.
		gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := 0; i < len(gl) && i < len(wl); i++ {
			if gl[i] != wl[i] {
				t.Errorf("first divergence at line %d:\n prompt: %q\n   help: %q", i+1, gl[i], wl[i])
				break
			}
		}
		if len(gl) != len(wl) {
			t.Errorf("line counts differ: prompt %d, help %d", len(gl), len(wl))
		}
	}
}

// TestCommandsSectionIsNotFrozenAndNotCopied is property 2, in two arms.
//
// (a) NOT FROZEN: the block must be a Render, not a Text. A Text would be
// evaluated once at package init; more importantly it would be a second place
// the command list exists, which is the Rule 14 defect this section removes.
//
// (b) NOT COPIED: the block must track the table. This arm MUTATES the help
// page's own output path by checking the relation rather than a snapshot — it
// asserts that every command line the help index prints appears in the prompt
// block, and that the counts agree, so a row added to `commandIndex` shows up on
// both sides or the test goes red. It cannot prove a FUTURE edit propagates
// without running one; the run that landed this section did exactly that with a
// real edit and recorded the before/after in the task report.
func TestCommandsSectionIsNotFrozenAndNotCopied(t *testing.T) {
	var entry *PromptSection
	for i := range promptSections {
		if promptSections[i].Name == commandsSectionName {
			entry = &promptSections[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("promptSections has no %q entry — the derived index never reaches the prompt",
			commandsSectionName)
	}
	if entry.Render == nil {
		t.Fatal("the commands section carries Text, not Render — a frozen copy of the command table is exactly the defect this section removes")
	}
	if entry.Text != "" {
		t.Errorf("the commands section carries Text (%q) beside its Render — the table would exist twice", entry.Text)
	}

	// (b) Every `  gopi …` row of the help index is in the block, and the
	// block adds no row of its own.
	block := commandsSection()
	index := helpIndexRegion(t)
	countBlock := strings.Count(block, "\n  gopi ")
	countIndex := strings.Count(index, "\n  gopi ")
	if countBlock != countIndex {
		t.Errorf("the block names %d command rows, the help index names %d — the two surfaces disagree about what gopi dispatches",
			countBlock, countIndex)
	}
	if countIndex < minHelpIndexRows {
		t.Fatalf("the help index carries only %d rows — a scan this small cannot tell a derived block from an empty one (vacuum floor %d)",
			countIndex, minHelpIndexRows)
	}
	for _, line := range strings.Split(index, "\n") {
		if !strings.HasPrefix(line, "  gopi ") {
			continue
		}
		if !strings.Contains(block, line) {
			t.Errorf("the help index prints a command row the prompt block does not carry:\n%s", line)
		}
	}
}

// minHelpIndexRows is the vacuum floor: the command index has had ~35 rows for
// as long as this gate has existed, so a reading below this means the
// extraction broke, not that the CLI shrank. A gate that compares two empty
// strings would pass forever (the failure shape Rule 36 records).
const minHelpIndexRows = 20

// TestCommandsSectionReachesTheAssembledPrompt is property 3. The section
// exists so the MODEL can read it; a registry entry nothing joins is the exact
// shape the block it replaces was written to repair.
func TestCommandsSectionReachesTheAssembledPrompt(t *testing.T) {
	p := systemPromptKernel()
	block := commandsSection()
	if block == "" {
		t.Fatal("commandsSection() rendered nothing — the prompt carries no command index at all")
	}
	if !strings.Contains(p, block) {
		t.Error("the assembled prompt does not carry the derived command block")
	}
	if !strings.Contains(p, commandsSectionMarker) {
		t.Errorf("the assembled prompt does not carry the block's marker %q", commandsSectionMarker)
	}
	// And the two prompt paths: the task assembly is the kernel; the chat
	// --tools path uses a.SystemPrompt, which cmd/agent fills from
	// assembleSystemPrompt(), so the kernel is the right object to assert on.
	// The plain chat path deliberately sends a smaller persona prompt (it has no
	// tools), so it must NOT be judged by this block.
	if !strings.Contains(assembleSystemPrompt(), block) {
		t.Error("assembleSystemPrompt (the string the task path sends) lost the command block")
	}
}

// TestCommandsSectionOrderIsLast pins the placement: the index is reference
// material, so it rides AFTER every rule block. Moving it earlier would put a
// 5KB table between the identity paragraph and the routing rules the model
// reads first — and it would move the prompt-cache prefix on every commandIndex
// edit, which is the one thing the placement can control.
func TestCommandsSectionOrderIsLast(t *testing.T) {
	last := ""
	for _, s := range promptSections {
		if s.Name == commandsSectionName {
			continue
		}
		if s.Order > commandsSectionOrder {
			t.Errorf("section %q sits at Order %d, after the command index (%d) — the index must come last",
				s.Name, s.Order, commandsSectionOrder)
		}
		last = s.Name
	}
	if last == "" {
		t.Fatal("the registry has no section besides the command index")
	}
}
