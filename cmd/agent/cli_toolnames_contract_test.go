package main

// cli_toolnames_contract_test.go — CLI↔toolnames mirror drift guard.
//
// WHY: internal/toolnames/names.go (04d1bce) declares the package contract
// "the CLI mirrors … MUST use these constants, never a bare string literal"
// (Rule 1: named constants, single source of truth). cmd/agent exposes user
// commands whose argv token IS the tool wire name the LLM emits
// (web_search/web_fetch/vision_analyze/text2img — the isXxxSubcommand argv
// predicates in cli_websearch.go / cli_webfetch.go / cli_vision.go /
// cli_text2img.go, and the memory flat-verb aliases memory_search/store/
// update/feedback in cli_memory.go's memoryLegacyAliases).
//
// These argv predicates now compare against the toolnames CONSTANT (single
// compiled symbol), so a tool rename recompiles the dispatch with it — no
// literal drift CAN survive in the dispatch itself. What can still drift is
// the *user-visible contract tables* that mirror the tool verbs in test
// helpers (cli_contracts_test.go's dispatcherOfToken keys + the completion /
// usage surfaces): they must stay in lockstep so the CLI surfaces that a
// human or script types still match the constant's value.
//
// This suite pins those outer couplings WITHOUT invoking the argv predicates
// (which execute the handler and os.Exit — the established convention is the
// predicates' positive path is covered by live binary smoke, cf.
// cli_webfetch_test.go TestIsWebFetchSubcommandDispatch). Tests here are
// pure table/constant assertions.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// toolMirrorArgvVerb is one argv verb the CLI exposes under a tool wire name.
// cli token is the exact string the dispatch surface types; constant is the
// toolnames value that MUST equal it. A rename that misses a CLI surface
// diverges the two and fails TestCLIToolMirrorVerbsMatchConstants.
var toolMirrorArgvVerb = []struct {
	cliToken string
	toolname string
}{
	{"web_search", toolnames.ToolWebSearch},
	{"web_fetch", toolnames.ToolWebFetch},
	{"vision_analyze", toolnames.ToolVisionAnalyze},
	{"text2img", toolnames.ToolText2img},
}

// cliNonToolSurfaceTokens are argv tokens that LOOK tool-like but are NOT LLM
// tool wire mirrors — user-facing CLI words that stay independent of the wire
// protocol, documented as exemptions at each site (see references):
//   - "code-search": hyphenated subcommand wrapping the code_search TOOL. The
//     tool wire name is code_search; the CLI verb is the separate stable
//     command "code-search" (aliases codesearch/searchcode). Renaming the
//     tool must NOT rename the CLI command.
//   - memory_search / memory_store / memory_update / memory_feedback:
//     deprecated flat-verb LEGACY ALIASES of the pre-merge separate tools.
//     The merge folded them into the single `memory` TOOL (action param), so
//     these argv verbs mirror NO live constant — they are thin shims printing
//     a deprecation note and routing to `gopi memory <subcommand>` (see
//     cli_memory.go memoryLegacyAliases). They were deliberately kept literal
//     because the retired toolnames constants are GONE.
//   - memory_get / memory_list / memory_delete / memory_feedback_list:
//     cmd/agent maintenance verbs (tools.MemoryGet/ListRecent/Delete helpers),
//     NOT tools the model calls — no toolnames constant exists (documented at
//     cli_memory.go memoryLegacyAliases).
//
// A future toolname whose value collides with one of these is flagged by
// TestCLINonToolSurfaceTokensDoNotOverlapTools so the coupling stays a
// conscious decision.
var cliNonToolSurfaceTokens = []string{
	"code-search",
	"memory_search",
	"memory_store",
	"memory_update",
	"memory_feedback",
	"memory_get",
	"memory_list",
	"memory_delete",
	"memory_feedback_list",
}

// TestCLIToolMirrorVerbsMatchConstants: every CLI surface token that mirrors a
// tool wire name still equals the toolnames constant. This is the drift
// canary the package header demands: if internal/toolnames renames a tool,
// any CLI surface (the argv dispatchers, this table) not updated diverges
// here BEFORE the rename ships — no silent un-route of a typed command.
func TestCLIToolMirrorVerbsMatchConstants(t *testing.T) {
	for _, c := range toolMirrorArgvVerb {
		if c.cliToken != c.toolname {
			t.Errorf("CLI tool-mirror verb %q diverged from toolnames %q — a tool rename missed the cmd/agent surface; update the argv predicates/aliases to the constant", c.cliToken, c.toolname)
		}
	}
}

// TestCLIMirrorVerbsCoveredByContractInventory cross-checks the contract
// suite's authoritative argv table (cli_contracts_test.go dispatcherOfToken)
// so every tool-mirror verb that ships a typed command is exercised by the
// live-binary smoke there. A tool-mirror verb missing from the inventory is a
// surface gap: renames to it would ship without an end-to-end routing guard.
func TestCLIMirrorVerbsCoveredByContractInventory(t *testing.T) {
	// dispatcherOfToken lives in package main's cli_contracts_test.go and is
	// built for the whole set of tested binaries; the four argv tool-mirror
	// subcommands must appear as inventory first-tokens (reverse check:
	// each tool mirror verb has a contract case pinning its -h routing).
	covered := map[string]bool{}
	for _, inv := range cliContractInventory {
		for _, a := range inv.args {
			covered[a] = true
		}
	}
	for _, c := range toolMirrorArgvVerb {
		// The four argv tool-mirror SUBCOMMANDS ship as typed top-level
		// commands (gopi web_search, web_fetch, vision_analyze, text2img) and
		// each must appear as a cli_contracts inventory first-token. The
		// memory_* flat verbs are legacy aliases of the merged `memory` tool
		// and route through cli_memory.go's memoryLegacyAliases into the
		// `gopi memory` GROUP — not standalone inventory entries (the memory
		// group itself is a cli_contracts inventory case, see cli_contracts).
		if strings.HasPrefix(c.cliToken, "memory_") {
			continue
		}
		if !covered[c.cliToken] {
			t.Errorf("tool-mirror verb %q has no cli_contracts.go inventory entry — a typed-command rename/regression to it is not covered by the live-binary contract smoke; add a cliContractInventory case", c.cliToken)
		}
	}
}

// TestCLINonToolSurfaceTokensDoNotOverlapTools pins the deliberate
// exemptions' boundary. If a future toolnames value equaled one of these
// literal CLI verbs, the verb would start silently mirroring a tool — a
// coupling the design deliberately leaves open. Failure forces the decision:
// bind the verb to the new constant, or rename the verb.
func TestCLINonToolSurfaceTokensDoNotOverlapTools(t *testing.T) {
	known := map[string]bool{
		toolnames.ToolBash:          true,
		toolnames.ToolRead:          true,
		toolnames.ToolWrite:         true,
		toolnames.ToolEdit:          true,
		toolnames.ToolTask:          true,
		toolnames.ToolCodeSearch:    true,
		toolnames.ToolKeywordSearch: true,
		toolnames.ToolMemory:        true,
		toolnames.ToolJob:           true,
		toolnames.ToolWebSearch:     true,
		toolnames.ToolWebFetch:      true,
		toolnames.ToolTaskSubmit:    true,
		toolnames.ToolGitBlame:      true,
		toolnames.ToolContextTrim:   true,
		toolnames.ToolVisionAnalyze: true,
		toolnames.ToolText2img:      true,
	}
	for _, tok := range cliNonToolSurfaceTokens {
		if known[tok] {
			t.Errorf("non-tool CLI surface token %q overlaps a toolnames value — CLI verb now mirrors a tool unintentionally; bind it to the constant or rename the verb", tok)
		}
	}
}
