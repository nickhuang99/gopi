package agent

import (
	"strings"

	"github.com/nick/gopi/internal/config"
)

// commandsSectionName is the registry Name of the derived command index
// (Rule 1: the name is a value the registry, the density gate's exclusion set
// and the derivation test must agree on, so it is declared once).
const commandsSectionName = "commands"

// commandsSectionOrder places the derived index LAST: it is reference material,
// not a rule. Everything before it (identity, self-knowledge, tool routing, the
// discipline blocks, the tick clock) tells the model HOW to work; the index
// tells it WHAT the binary dispatches, and a model that reads it does so when
// it needs a command. Putting it at Order 70 also keeps the append position
// stable — the block is the registry's only machine-derived member, so a
// prompt-cache prefix that ends before it is unaffected by a `commandIndex` row
// whose summary grew a word.
const commandsSectionOrder = 70

// commandsSectionMarker is the block's bracketed heading — the one string that
// proves the DERIVED index reached the assembled prompt rather than merely
// existing as a function nobody calls.
const commandsSectionMarker = "[COMMAND INDEX — derived from `gopi -h`'s one source, not a copy]"

// commandsSection renders the command index into the system prompt.
//
// WHY A Render AND NOT A Text (this is the whole point of the section): the
// commands are not this package's to own. `internal/config`'s `commandIndex` is
// the single table `gopi -h` prints, and Rule 14 forbids a second copy of a
// value two packages must agree on — a hand-typed list here would go stale the
// first time a verb retired, which is exactly the defect this section removes
// (the block it replaces taught a fixed handful of commands and left the model
// to discover the rest by probing `-h` one level at a time). A `Text` field is
// evaluated once at package init and would freeze whatever the table said then;
// `Render` re-reads the table at assembly time, so editing `commandIndex` moves
// both `gopi -h` and this block in the same build, and a retired command
// disappears from both without a second edit.
//
// The cost is that this section's length is not a constant. That is why the
// density gate excludes it BY NAME (prompt_density_budget_test.go) rather than
// because it is large: the block is machine-derived, so its size is
// `commandIndex`'s business, and the gate that guards it is the derivation
// equality in commands_section_test.go, not a character ceiling nobody could
// compress anyway.
func commandsSection() string {
	rows := strings.TrimSpace(config.FormatCommandIndex())
	if rows == "" {
		// An empty index is a state the section must not paper over: it would
		// mean `gopi -h` lists no command at all. Returning "" drops the section
		// (the registry's emptied-section contract) and the derivation test
		// fails loudly against the real table.
		return ""
	}
	return commandsSectionMarker + "\n" + rows
}
