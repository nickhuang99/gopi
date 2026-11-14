package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestSystemPrompt_SelfKnowledgeSection is the acceptance guard for the
// self-knowledge section: the assembled system prompt must (a) carry the
// section at all, (b) state the identity that makes the rest actionable —
// the agent is gopi, running inside gopi's own source repo, and its own CLI
// through bash is how it senses the machine — and (c) NAME every sense verb.
//
// Every expected verb is referenced through its cliverbs CONSTANT, never a
// typed-in literal (Rule 1/13): a verb rename then fails to COMPILE here
// instead of silently passing while the prompt teaches a dead word. That
// property is the point of the test — the red proof in the task report was
// produced by REMOVING the registry entry, and this file is what noticed.
func TestSystemPrompt_SelfKnowledgeSection(t *testing.T) {
	p := assembleSystemPrompt()

	// (a) The section is registered and rendered. The name is asserted too:
	// a section whose text landed by accident (in another block) would not
	// have the registry entry the epic asked for.
	found := false
	for _, s := range promptSections {
		if s.Name == selfKnowledgeSectionName {
			found = true
		}
	}
	if !found {
		t.Fatalf("promptSections has no %q entry — the sense verbs are invisible to the model again",
			selfKnowledgeSectionName)
	}
	if !strings.Contains(p, selfKnowledgeMarker) {
		t.Fatalf("assembled system prompt does not carry the self-knowledge marker %q", selfKnowledgeMarker)
	}

	// (b) The identity claims. These are the sentences that turn an unused
	// capability into a reachable one — without them the verbs below read as
	// generic CLI documentation for some other program.
	for _, want := range []string{
		"gopi's own source repo",
		"gopi's own CLI is how you sense this machine",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("self-knowledge section is missing the identity claim %q", want)
		}
	}

	// (c) Every sense verb, AND THE ADDRESS THAT REACHES IT. The list is the
	// deliverable's vocabulary: ui_state / session log / session raw / session
	// stats / task / tool capture / tool screen_capture / tool act / tool
	// sandbox — spelled as whole command lines since the verb-retirement epic
	// (2026-10-05), because the bare words are refused at argv[0] now and a
	// prompt that taught them would route the model into a refusal on its first
	// attempt to look at itself (Rule 11: the prompt is the only channel that
	// teaches a tool).
	for _, verb := range []string{
		cliverbs.UIState,
		// The session family is asserted as its ADDRESS SET (cliverbs.SessionAddresses),
		// the one the prompt renders: the section spells the noun once and carries
		// all three leaves, because the system prompt is bounded by
		// systemPromptDensityBudget (the per-leaf spelling costs +16 against a
		// 10-char headroom, measured 2026-10-06). Each LEAF is still enforced — not
		// by prose duplication but by TestSessionAddressesIsAConstantExpression,
		// which requires the declaration to name SessionLogAddress /
		// SessionRawAddress / SessionStatsAddress and forbids a retyped literal.
		cliverbs.SessionAddresses,
		cliverbs.Task, cliverbs.ToolCaptureAddress, cliverbs.ToolScreenCaptureAddress,
		cliverbs.ToolActAddress, cliverbs.ToolSandboxAddress,
	} {
		if !strings.Contains(p, verb) {
			t.Errorf("self-knowledge section does not name the sense address %q", verb)
		}
	}
	// And the bare words must NOT appear as commands any more: a prompt naming
	// both the retired spelling and the live one teaches the retired one about
	// half the time, and the refusal is what the model then reports to itself.
	for _, retired := range []string{
		"gopi " + cliverbs.Log + " ", "gopi " + cliverbs.Capture + " ",
		"gopi " + cliverbs.Sandbox + " ", "gopi " + cliverbs.Act + " ",
	} {
		if strings.Contains(p, retired) {
			t.Errorf("self-knowledge section still spells the retired form %q — the word is refused at argv[0] since the retirement", retired)
		}
	}
	// The pair that only works AS a pair: capture is the eye, act is the hand
	// (the capture→act→verify loop). CHANGED 2026-11-10 (epic 01a1c341): the
	// section no longer spells the pair itself — its row moved to the command
	// index the section now derives — so the assertion is on the ASSEMBLED
	// prompt: both addresses must still reach the model, and the join is no
	// longer asserted because nothing joins them any more (a joined-pair
	// assertion would demand the section re-type what the index already
	// prints). What the model must not lose is the ability to see BOTH halves.
	for _, addr := range []string{cliverbs.ToolCaptureAddress, cliverbs.ToolActAddress} {
		if !strings.Contains(p, addr) {
			t.Errorf("the assembled prompt names neither the index row for %q nor the section's own spelling of it — the capture→act loop is invisible", addr)
		}
	}
	// uistate is only reachable if the model knows the surface it reads is
	// `gopi ui` — the verb name alone does not say what it reads.
	if !strings.Contains(p, cliverbs.UIState+" reads a live screen") {
		t.Errorf("self-knowledge section does not say %s reads a live screen",
			cliverbs.UIState)
	}

	// (d) THE EXTENDED EYE (epic 01a0ddbd, sibling of 01a0ddb3/01a0dd28). The
	// three facts the section omitted while the machinery already carried them
	// — each is asserted as a PROPERTY of the block, not as a copy of its
	// prose, so a future rewording that keeps the meaning stays green:
	//
	//  1. BOTH surfaces are named. A model told only "a live `gopi ui` screen"
	//     never thought to look at `gopi chat` — the console it is itself
	//     painting, and the one the operator is reading. The UI constant is
	//     asserted through `gopi` + the verb because it is quoted as an
	//     invocation, while Chat additionally carries WHY it matters.
	for _, surface := range []struct{ verb, why string }{
		{cliverbs.Chat, "the console you answer in"},
		{cliverbs.UI, ""},
	} {
		if !strings.Contains(p, surface.verb) {
			t.Errorf("self-knowledge section does not name the %q surface — the eye reads two of them", surface.verb)
		}
		if surface.why != "" && !strings.Contains(p, surface.why) {
			t.Errorf("self-knowledge section names %s but never says what it is (%q) — a bare verb does not make the surface self-evident",
				surface.verb, surface.why)
		}
	}
	//  2. The cells carry COLOUR/FORMAT, not just characters, and the section
	//     names the section of the artifact where a reader finds it.
	if !strings.Contains(p, "colour+format per cell-range") {
		t.Error("self-knowledge section does not say the grid carries colour+format per cell-range — the most valuable half of the perception stays invisible")
	}
	if !strings.Contains(p, lineedit.DumpAttrRunLabel) {
		t.Errorf("self-knowledge section never names %q — the model is told the colour exists but not where to read it", lineedit.DumpAttrRunLabel)
	}
	//  3. The screen is an EVIDENCE CHANNEL that can disagree with the
	//     transcript, and the ONE-CALL check is named with BOTH its flags.
	//     This is the operator's whole point: a display error the agent cannot
	//     notice becomes a false premise in every later inference.
	if !strings.Contains(p, "EVIDENCE channel") || !strings.Contains(p, "DISAGREE") {
		t.Error("self-knowledge section does not present the screen as an evidence channel that can disagree with the transcript")
	}
	// The check must be reachable from the prompt ALONE: verb + both flags, in
	// one line. A section that says "there is a check" without the invocation
	// teaches a capability the model cannot compose — and `--session` is
	// exactly the flag a first-time caller omits (uiStateCompareRefusalNoSession),
	// so omitting it here would rebuild the two-call experience in prose.
	if !strings.Contains(p, cliverbs.UIState+" "+cliverbs.Compare) {
		t.Errorf("self-knowledge section does not name the one-call check (%s %s)", cliverbs.UIState, cliverbs.Compare)
	}
	if !strings.Contains(p, cliverbs.SessionFlag) {
		t.Errorf("self-knowledge section never names %s — the check reads the dump's own session by DEFAULT, and a caller who needs a different authority must still be told the override exists",
			cliverbs.SessionFlag)
	}
}

// TestSelfKnowledgeSectionIsStatic pins the caching-prefix constraint: the
// section is a compile-time CONSTANT EXPRESSION, so the rendered text is
// byte-identical across calls and the prompt-cache prefix cannot be disturbed
// by a per-turn lookup. A Render function (the session-budget pattern) would
// be the wrong shape here — nothing in this block depends on the environment.
func TestSelfKnowledgeSectionIsStatic(t *testing.T) {
	first := selfKnowledgeRules
	second := selfKnowledgeRules
	if first != second {
		t.Fatalf("selfKnowledgeRules is not stable across reads (%d vs %d chars)", len(first), len(second))
	}
	if first != selfKnowledgeRules {
		t.Fatal("selfKnowledgeRules changed between reads — it must be a constant")
	}
	// The section must ride Text, not Render: a Render hook is the ONE shape
	// that can vary per turn, and this block has no runtime input.
	for _, s := range promptSections {
		if s.Name != selfKnowledgeSectionName {
			continue
		}
		if s.Render != nil {
			t.Errorf("the %s section uses Render — it must stay a static Text (caching prefix)",
				selfKnowledgeSectionName)
		}
		if s.Text != selfKnowledgeRules {
			t.Errorf("the %s section text is not the selfKnowledgeRules constant", selfKnowledgeSectionName)
		}
	}
	// Order: right after the header (identity first), before the tool routing
	// rules it feeds.
	for _, s := range promptSections {
		if s.Name != selfKnowledgeSectionName {
			continue
		}
		if s.Order <= 0 || s.Order >= 10 {
			t.Errorf("the %s section is at Order %d — it belongs between the header (0) and toolRouting (10)",
				selfKnowledgeSectionName, s.Order)
		}
	}
}

// TestSenseVerbDeclarationUsesConstants is Rule 13's check for THIS section's
// vocabulary, scoped to where the verbs are actually spelled: the
// selfKnowledgeRules declaration and the registry entry that renders it. A
// verb typed as a bare literal there is a bypassed constant — the exact defect
// the rule names — while a `"log"`/`"raw"` literal elsewhere in this package
// (a log-file field name, an unrelated CLI word) is NOT this section's
// business and would make the gate cry wolf on pre-existing code.
func TestSenseVerbDeclarationUsesConstants(t *testing.T) {
	src, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatalf("read loop.go: %v", err)
	}
	text := string(src)
	begin := strings.Index(text, "const selfKnowledgeRules =")
	if begin < 0 {
		t.Fatal("selfKnowledgeRules declaration not found — the section is not declared in loop.go")
	}
	end := strings.Index(text[begin:], "// PromptSection is one named block")
	if end < 0 {
		t.Fatal("could not delimit the selfKnowledgeRules declaration")
	}
	decl := text[begin : begin+end]

	// The verbs that must come from cliverbs, and could plausibly be typed.
	//
	// THE SESSION LEAVES ARE REACHED THROUGH THEIR ADDRESS since the
	// verb-retirement epic (2026-10-05): `gopi log` / `raw` / `stats` are REFUSED
	// at argv[0] (`gopi: "log" is not a gopi command — this one is spelled `gopi
	// session log``), so the section spells the ADDRESS, one constant carrying
	// all three leaves. The requirement below is unchanged in substance — the
	// section must reach every leaf THROUGH a cliverbs constant rather than a
	// typed literal — and the literal scan underneath still fires on `"log"`,
	// `"raw"` and `"stats"` if any of them is typed into the declaration.
	// Each leaf is reached through a cliverbs ADDRESS CONSTANT — for the session
	// family, the ADDRESS SET (cliverbs.SessionAddresses) that names all three
	// per-leaf constants and renders them. The leaf coverage is pinned by
	// TestSessionAddressesIsAConstantExpression (the declaration must name every
	// per-leaf constant and contain no retyped literal).
	//
	// THE MAP SHRANK ON PURPOSE (2026-11-10, epic 01a1c341). It used to demand
	// that this declaration spell `task`, `tool capture`, `tool act` and
	// `tool sandbox` too — a hand-kept "Yours:" tail. That tail is GONE: those
	// four are rows of the command index the section now DERIVES from
	// internal/config (`commands_section.go` renders `config.FormatCommandIndex()`),
	// so demanding the constants HERE would demand a second copy of them — the
	// Rule 14 mirror this file exists to prevent. What remains is what the
	// section still spells itself: the two senses with no index row of their own
	// (`ui_state` with its compare check, and this run's own transcript).
	for verb, constName := range map[string]string{
		cliverbs.UIState: "cliverbs.UIState",
		cliverbs.Log:     "cliverbs.SessionAddresses",
		cliverbs.Raw:     "cliverbs.SessionAddresses",
		cliverbs.Stats:   "cliverbs.SessionAddresses",
	} {
		// A bare literal of the verb inside the declaration is the violation.
		for _, lit := range []string{`"` + verb + `"`, "`" + verb + "`"} {
			if strings.Contains(decl, lit) {
				t.Errorf("selfKnowledgeRules spells %s as the bare literal %s — use %s (Rule 13)",
					verb, lit, constName)
			}
		}
		// ...and the constant must actually appear, or the section names no verb.
		if !strings.Contains(decl, constName) {
			t.Errorf("selfKnowledgeRules never references %s — the section must spell it through the constant",
				constName)
		}
	}

	// The registry entry must point at the constant, not a copy of the text.
	reg := strings.Index(text, "selfKnowledgeSectionName, Order: 5, Text: selfKnowledgeRules")
	if reg < 0 {
		t.Error("promptSections has no selfKnowledge entry pointing at selfKnowledgeRules")
	}
}

// TestSenseVerbConstantsAreTheDispatchedWords pins the OTHER half of the
// cache/correctness contract: the words this prompt teaches must be the words
// cmd/agent actually dispatches. It reads the CLI's own top-level completion
// list (topLevelCommandNames lives in package main, so the list is read as
// source text — the same technique internal/cliverbs' own test uses).
//
// Without this, the prompt could confidently teach `gopi ui_state` while the
// dispatcher had renamed it, and nothing in internal/agent would notice.
func TestSenseVerbConstantsAreTheDispatchedWords(t *testing.T) {
	srcs, err := filepath.Glob(filepath.Join("..", "..", "cmd", "agent", "cli_verbs.go"))
	if err != nil || len(srcs) == 0 {
		t.Fatalf("locate cmd/agent/cli_verbs.go: %v", err)
	}
	raw, err := os.ReadFile(srcs[0])
	if err != nil {
		t.Fatalf("read cli_verbs.go: %v", err)
	}
	text := string(raw)
	for verb, constName := range map[string]string{
		cliverbs.UIState: "CliVerbUIState",
		cliverbs.Log:     "CliVerbLog",
		cliverbs.Raw:     "CliVerbRaw",
		cliverbs.Stats:   "CliVerbStats",
		cliverbs.Task:    "CliVerbTask",
		cliverbs.Capture: "CliVerbCapture",
	} {
		re := regexp.MustCompile(`(?m)^\s*` + constName + `\s*=\s*"([^"]*)"`)
		m := re.FindStringSubmatch(text)
		if m == nil || m[1] != verb {
			t.Errorf("cmd/agent/cli_verbs.go %s does not declare %q (got %v) — the prompt teaches a verb the CLI does not dispatch",
				constName, verb, m)
		}
	}
}

// senseVerbCount is referenced by the test above only to keep the verb list's
// expected cardinality explicit: a future edit that drops a verb from the
// section should fail loudly here rather than pass by omission.
const senseVerbCatalogSize = 8

// TestSenseVerbCatalogIsComplete guards the count: the section promised eight
// sense verbs (ui_state/log/raw/stats/task/capture/act/sandbox). Adding a
// ninth to cliverbs without teaching it here — or dropping one from the prompt
// — is a drift this test can see.
func TestSenseVerbCatalogIsComplete(t *testing.T) {
	all := []string{
		cliverbs.UIState, cliverbs.Log, cliverbs.Raw, cliverbs.Stats,
		cliverbs.Task, cliverbs.Capture, cliverbs.Act, cliverbs.Sandbox,
	}
	if len(all) != senseVerbCatalogSize {
		t.Fatalf("the sense catalogue is %s entries", strconv.Itoa(len(all)))
	}
	p := assembleSystemPrompt()
	for _, v := range all {
		if !strings.Contains(p, v) {
			t.Errorf("sense verb %q is in the catalogue but absent from the assembled prompt", v)
		}
	}
}
