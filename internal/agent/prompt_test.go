package agent

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// TestAssembleSystemPromptOrder verifies sections are joined in ascending
// Order regardless of registry order (P0-2: order-sorted assembly).
func TestAssembleSystemPromptOrder(t *testing.T) {
	orig := promptSections
	defer func() { promptSections = orig }()

	// Deliberately shuffled registry order.
	promptSections = []PromptSection{
		{Name: "zeta", Order: 100, Text: "ZZZ"},
		{Name: "alpha", Order: 0, Text: "AAA"},
		{Name: "mid", Order: 50, Text: "MMM"},
		{Name: "beta", Order: 10, Text: "BBB"},
	}
	got := systemPromptKernel()
	want := "AAA\n\nBBB\n\nMMM\n\nZZZ"
	if got != want {
		t.Fatalf("systemPromptKernel() = %q, want %q", got, want)
	}
}

// TestAssembleSystemPromptEmptySectionDropped verifies empty sections are
// skipped (dsh renderPrompt semantics: filter empty before join).
func TestAssembleSystemPromptEmptySectionDropped(t *testing.T) {
	orig := promptSections
	defer func() { promptSections = orig }()

	promptSections = []PromptSection{
		{Name: "a", Order: 10, Text: "A"},
		{Name: "empty", Order: 20, Text: ""},
		{Name: "b", Order: 30, Text: "B"},
	}
	got := systemPromptKernel()
	if got != "A\n\nB" {
		t.Fatalf("systemPromptKernel() = %q, want %q (empty section dropped)", got, "A\n\nB")
	}
}

// TestAssembleSystemPromptStableOrder verifies equal orders keep registry
// order (sort.SliceStable).
func TestAssembleSystemPromptStableOrder(t *testing.T) {
	orig := promptSections
	defer func() { promptSections = orig }()

	promptSections = []PromptSection{
		{Name: "first", Order: 10, Text: "1"},
		{Name: "second", Order: 10, Text: "2"},
	}
	got := systemPromptKernel()
	if got != "1\n\n2" {
		t.Fatalf("stable order violated: %q", got)
	}
}

// densityGateBudgetProbe is the budget VALUE the gate renders the prompt with
// when it checks the widest spelling (see TestSystemPromptDensity's second
// arm). It is a PROBE, not a ceiling: the knob takes any minute count, so no
// fixed probe bounds them all. 99999 covers every operator setting a session
// ceiling can plausibly carry, and an absurdly typed 8-digit value would spend
// two more chars against a margin this arm keeps in the high tens.
const densityGateBudgetProbe = "99999"

// TestSystemPromptDensity is the P0-2 acceptance check: the assembled prompt
// (identity + all discipline blocks) must stay under 4.5K characters — the
// density measured against dsh's 4.1K-character prompt (competition #4).
func TestSystemPromptDensity(t *testing.T) {
	// The measurement is the RULE kernel — every section this package writes —
	// NOT the whole kernel: the `commands` section is derived from
	// internal/config's commandIndex, so its length is that table's business and
	// no prompt edit could compress it (see derivedPromptSectionNames). The
	// derived block is guarded by its own equality gate instead.
	p := ruleKernel()
	if n := len(p); n >= systemPromptDensityBudget {
		t.Fatalf("system prompt density: %d chars >= %d target — compress the blocks", n, systemPromptDensityBudget)
	}
	// Every block must actually be present (nothing silently dropped).
	for _, name := range []string{"TOOL ROUTING", "EFFICIENCY RULES", "TASK MANAGEMENT RULES",
		"BASH EFFICIENCY RULES", "JOB RULES", "THINKING RULES"} {
		if !strings.Contains(p, name) {
			t.Errorf("system prompt missing section marker %q", name)
		}
	}
	// Fix 3 (report §8 #3): the memory adoption habit — "start each task with
	// memory_search" — must survive the density gate (this same gate enforces
	// <4500, and the habit line must actually be present).
	if !strings.Contains(p, "start each task with memory(action=search)") {
		t.Errorf("system prompt missing the memory(action=search) first habit (report §8 #3)")
	}
	// 对称强制 (task-end store): the completion checkpoint must mandate a
	// single memory_store, the mirror of the task-start memory_search habit.
	// Guards against the REPEAT-TEST gap where task-end store was a soft
	// suggestion that relied on LLM volition. The harness enforcement is the
	// hard guarantee (see completionCheckpointMessage); this gates the prompt
	// wiring so the model knows store-at-completion is mandatory.
	if !strings.Contains(p, "at completion call memory(action=store)") {
		t.Errorf("system prompt missing the symmetric task-end memory(action=store) mandate")
	}
	if !strings.Contains(p, "call memory(action=store) once") {
		t.Errorf("system prompt missing the single-store-at-completion mandate (task rule 6)")
	}
	// The 4-line if-then memory block (MEMORY-PROMPT-PERSUASION.md §3.1):
	// mid-task notable → store NOW and contradiction → feedback/update are the
	// two NEW triggers that prompt-only persuasion cannot afford to lose.
	if !strings.Contains(p, "mid-task notable find/pitfall") {
		t.Errorf("system prompt missing the mid-task notable → store trigger (memory block c)")
	}
	if !strings.Contains(p, "memory(action=feedback, rating=useless)") {
		t.Errorf("system prompt missing the contradiction → feedback(useless) trigger (memory block d)")
	}
	// 语气守卫 (influence-tactics research §1.4): urgency-style MUST/CRITICAL
	// framing measurably hurts task quality — the prompt must stay trigger-style,
	// with hard guarantees in the harness, never threats in the prompt.
	if strings.Contains(p, "MUST") {
		t.Errorf("system prompt must not use MUST-style urgency framing (influence-tactics research)")
	}
	// CLOCK-TICK: the ticking-clock line must survive the density gate — the
	// elapsed tick is the LLM's primary time signal (see clock.go).
	if !strings.Contains(p, "ticking clock runs from session start") {
		t.Errorf("system prompt missing the CLOCK-TICK ticking-clock line")
	}
	// The prompt must teach the tick's CURRENT spelling, taken from clock.go
	// rather than retyped here: a test asserting a literal would keep passing
	// against a spelling nothing paints any more — which is exactly what the
	// bare-seconds version did after the tick was folded into tiers (Rule 1/13).
	if !strings.Contains(p, tickShapeLabel) {
		t.Errorf("system prompt missing the elapsed-tick explanation (%s)", tickShapeLabel)
	}
	if !strings.Contains(p, tickDecisionLabel) {
		t.Errorf("system prompt does not teach the tick as the clock paints it (%s)", tickDecisionLabel)
	}

	// THE GATE MUST NOT BE ENV-DEPENDENT (measured 2026-11-02, this run).
	// The kernel embeds the operator's session budget, and its DIGITS widen the
	// prompt: at 83b145ee a one-digit-wider budget put the kernel exactly on the
	// ceiling (60/90 -> 4498, 120/600 -> 4500 >= 4500 RED, 1440 -> 4502 RED),
	// so `GOPI_SESSION_BUDGET=120` turned the gate red with no code change and
	// the failure read as a prompt regression. The measurement is a WIDEST-CASE
	// one now: render through the knob's own resolver and assert the budget
	// moves the length at all (so a hardcoded number cannot pass this arm) and
	// that even the widest spelling clears the ceiling.
	t.Setenv(sessionBudgetEnv, densityGateBudgetProbe)
	widest := ruleKernel()
	if !strings.Contains(widest, densityGateBudgetProbe+"min") {
		t.Fatalf("the kernel does not render %s=%s — the budget number is typed in, not rendered",
			sessionBudgetEnv, densityGateBudgetProbe)
	}
	if n := len(widest); n >= systemPromptDensityBudget {
		t.Errorf("system prompt density at %s=%s: %d chars >= %d target — the kernel's margin is smaller than the knob's own digits",
			sessionBudgetEnv, densityGateBudgetProbe, n, systemPromptDensityBudget)
	}
}

// TestSystemPromptNoGoPiSpecifics guards kernel generality (2026-09-08
// generalization): the assembled system prompt must stay free of GoPi-project
// wording — devdoc/tasks paths, go-build/test idioms, internal case-study
// names. Such rules belong in the project's own GOPI.md, never the kernel; a
// future project without GOPI.md must still get a self-sufficient prompt.
func TestSystemPromptNoGoPiSpecifics(t *testing.T) {
	// MEASURED ON THE RULE KERNEL (2026-11-10, epic 01a1c341). This gate asks
	// whether the wording THIS PACKAGE writes is project-specific. The `commands`
	// section is not this package's wording: it renders internal/config's
	// commandIndex, the table `gopi -h` prints, so its rows name `gopi tool
	// text2img` and `gopi memory brew` because those are commands the binary
	// dispatches — dropping them from the prompt would make the prompt's command
	// list disagree with the CLI, which is the defect the section removes. The
	// generality the rule protects is still enforced, at the same ceiling, over
	// every block this package authors.
	p := ruleKernel()
	projectWords := []string{
		"devdoc/",          // GoPi repo doc layout
		"tasks/",           // GoPi repo report layout
		"go build ./",      // Go build gate idiom
		"build+test green", // Go-centric completion gate (now: project's own checks)
		"-run TestX",       // Go test flag
		"text2img",         // internal case-study name
		"case-study",       // internal case-study reference
		"gopi memory brew", // operator-only command
	}
	for _, w := range projectWords {
		if strings.Contains(p, w) {
			t.Errorf("kernel prompt still contains GoPi-project wording %q — move to GOPI.md", w)
		}
	}
	// Generic engineering wording must remain present.
	for _, w := range []string{"the project's own build+test gate", "ships a minimal repro test"} {
		if !strings.Contains(p, w) {
			t.Errorf("kernel prompt lost generic wording %q", w)
		}
	}
}

// TestSystemPromptEngineeringRules guards the 7 habit rules: constraint
// dual-path, pixel-level render truth, root-cause-before-params, minimal-sample
// verification, repro-test-per-fix, verification recipes to memory, and
// zero-dependency preflight. Each rule must survive the density gate.
func TestSystemPromptEngineeringRules(t *testing.T) {
	p := assembleSystemPrompt()
	markers := map[string]string{
		"1 约束反应 dual-path":   "CREATE within the constraint FIRST",
		"2 渲染真值 pixel truth": "PIXEL-level truth",
		"3 根因优先 5-why":       "5-why root cause",
		"4 最小样本 minimal":     "SMALLEST sample",
		"5 回归测试 repro":       "ships a minimal repro test",
		"6 配方入记忆":            "复用不重做",
		"7 依赖预算 zero-dep":    "ZERO-DEP default",
	}
	for rule, marker := range markers {
		if !strings.Contains(p, marker) {
			t.Errorf("system prompt missing engineering rule %s (marker %q)", rule, marker)
		}
	}
	// Tool-description side (rule 2 double-write): the render tool must tell
	// the model pixel-truth beats OCR, and the vision tool must not be
	// offered as a render-quality measurer.
	for _, td := range claudeToolDefs {
		switch td.Name {
		case "text2img":
			if !strings.Contains(td.Description, "PIXEL-level comparison (IoU)") {
				t.Errorf("text2img description missing pixel-truth verification steer (rule 2)")
			}
		case "vision_analyze":
			if !strings.Contains(td.Description, "pixel-level comparison (IoU)") {
				t.Errorf("vision_analyze description missing pixel-truth steer for render verification (rule 2)")
			}
		}
	}
}

// TestSystemPromptCounterExamples guards the "one sentence + one
// counter-example" discipline (P0-2: read/grep/glob/bash/code_search all
// carry a "not shell X" negation, both in routing rules and tool
// descriptions — description side lives in claudeToolDefs).
func TestSystemPromptCounterExamples(t *testing.T) {
	p := assembleSystemPrompt()
	negations := []string{
		"not shell cat/head/sed", // read
		"not shell grep",         // code_search + keyword_search
	}
	for _, n := range negations {
		if !strings.Contains(p, n) {
			t.Errorf("system prompt missing counter-example %q", n)
		}
	}
	// Tool-description side (double-write): bash and code_search descriptions
	// must carry the same routing negation as the guidance section.
	desc := ""
	for _, td := range claudeToolDefs {
		if td.Name == "bash" || td.Name == "code_search" || td.Name == "keyword_search" {
			desc += td.Description + " "
		}
	}
	for _, n := range []string{"NOT for searching code", "Faster and cleaner than bash grep", "For locating a symbol DEFINITION use code_search"} {
		if !strings.Contains(desc, n) {
			t.Errorf("tool descriptions missing counter-example %q", n)
		}
	}
}

// TestSystemPrompt_TaskListCadence guards R2 (perf-vs-claude-fix-plan.md):
// the task-list rule must demand the full JSON list only on change (or every
// maxRoundsWithoutTaskList responses at the latest), not every round, and
// must no longer require the fabricated create_at field.
func TestSystemPrompt_TaskListCadence(t *testing.T) {
	p := assembleSystemPrompt()
	if strings.Contains(p, "Every response MUST end with") {
		t.Error("task list rule still demands the list every round (R2)")
	}
	if !strings.Contains(p, fmt.Sprintf("every %d tool rounds", maxRoundsWithoutTaskList)) {
		t.Errorf("task list rule missing the %d-tool-round emission cadence", maxRoundsWithoutTaskList)
	}
	// The old required-fields spec ("create_at (unix string, set on
	// creation)") must be gone; an explicit "no create_at" steer is fine.
	if strings.Contains(p, "create_at (unix string") {
		t.Error("task list rule still requires create_at (R2)")
	}
}

// TestSystemPrompt_Review20260907Fixes guards the fixes from
// devdoc/system-prompt-review-2026-09-07.md. Each marker is one review
// finding; a future edit dropping the text = a silently regressed fix.
func TestSystemPrompt_Review20260907Fixes(t *testing.T) {
	p := assembleSystemPrompt()

	// T1 — todo is read-only; status changes happen by emitting the block.
	// (rule text + the todo tool description both carry it)
	if !strings.Contains(p, "never narrate progress in thinking") {
		t.Errorf("T1: task rules missing the read-only-todo steer")
	}
	// T2 — three-state vocabulary with the cardinality rule.
	for _, w := range []string{"pending -> progressing (exactly ONE) -> completed"} {
		if !strings.Contains(p, w) {
			t.Errorf("T2: task rules missing the three-state vocabulary %q", w)
		}
	}
	// T3 — a decomposed parent waits on its children (was: parent "completed").
	if !strings.Contains(p, "a parent waits on its children") {
		t.Errorf("T3: decomposition rule no longer inverted")
	}
	// T4 — cadence is counted in TOOL ROUNDS, not the undefined "responses".
	if strings.Contains(p, "3 responses") {
		t.Errorf("T4: task rules still say 'responses' — must be 'tool rounds'")
	}
	// T6 — the undefined "Epic ≠ task" jargon is gone.
	if strings.Contains(p, "Epic ≠ task") {
		t.Errorf("T6: undefined 'Epic ≠ task' jargon is back")
	}
	// T7 — the trivial-task exemption (>=3 real steps).
	if !strings.Contains(p, ">=3 real steps gets a list; a one-shot lookup skips it") {
		t.Errorf("T7: task rules missing the trivial-task exemption")
	}
	// T8 — DONE carries a verification+report gate. The kernel keeps the
	// generic wording ("verification green") and the report requirement names
	// the report PATH SOURCE as the environment variable the harness exports
	// (taskreport.PathEnv) rather than a project directory — PROJECT.md Rule 5
	// supplies the GoPi-specific devdoc/ or tasks/ location on top of it, and
	// the task-report seam's harness face is what actually guarantees the
	// file (the prompt is the model-facing half of the contract).
	for _, w := range []string{"verification green", taskreport.PathEnv, "## Completed", "## Verification"} {
		if !strings.Contains(p, w) {
			t.Errorf("T8: DONE gate missing %q", w)
		}
	}
	// Anti-drift (Rules 1b/13): the prompt must name the report's cap from the
	// package that enforces it, never a typed-in number of its own.
	if !strings.Contains(p, fmt.Sprintf("<=%dKB", taskreport.MaxBytes/1024)) {
		t.Errorf("T8: DONE gate does not render taskreport.MaxBytes (%d bytes)", taskreport.MaxBytes)
	}
	// T9 (reframe+evaluate merge) — the routing line now teaches the
	// three-verdict zero-loophole protocol. task_evaluate is RETIRED, so the
	// old tool pair must not reappear; the epic-judgment beat must name all
	// three exits and their conditional payloads.
	if !strings.Contains(p, "task(action=reframe)") {
		t.Errorf("T9: no task(action=reframe) routing line")
	}
	for _, w := range []string{"DOABLE", "TOO_BIG", "IMPOSSIBLE", "roadmap", "reason", "never dodge"} {
		if !strings.Contains(p, w) {
			t.Errorf("T9: reframe routing line missing %q", w)
		}
	}
	if strings.Contains(p, "task_evaluate") {
		t.Errorf("T9: task_evaluate is retired — the prompt must not route to it")
	}
	if strings.Contains(p, "NEEDS_RESEARCH") {
		t.Errorf("T9: NEEDS_RESEARCH was dropped (research is a roadmap item) — it must appear nowhere")
	}
	if !strings.Contains(p, "task_submit: an independent work item") {
		t.Errorf("T9: task_submit must be routed narrowly (independent work item, not a dodge exit)")
	}
	// T10 — the memory_feedback enum is complete (was a two-value pair).
	if !strings.Contains(p, "memory(action=feedback, rating=useful/useless/unused)") {
		t.Errorf("T10: feedback enum is incomplete")
	}
	// T11 — "取不到→酿造" named a tool the model does not have.
	if strings.Contains(p, "酿造") {
		t.Errorf("T11: prompt still instructs 酿造/memory_brew, which is not in claudeToolDefs")
	}
	// A1 — read vs sed is stated once, with the compound-command carve-out.
	if !strings.Contains(p, "sed/head/tail = byte-precise extraction INSIDE a compound command") {
		t.Errorf("A1: read-vs-sed distinction missing")
	}
	// A2 — "ONE command" no longer reads as a ban on parallel batches.
	if !strings.Contains(p, "parallel batches are separate and fine") {
		t.Errorf("A2: one-command rule still collides with the parallel rule")
	}
	// A4 — the header no longer implies a 3-tool set.
	if strings.Contains(p, "23 tools") {
		t.Errorf("A4: header still hardcodes a tool count (drifts from claudeToolDefs)")
	}
	// A5 — the only softness label carried an enforced imperative; it is gone.
	if strings.Contains(p, "— soft]") {
		t.Errorf("A5: WORKING MEMORY LAYOUT is still labelled soft")
	}
	// A6 — the tick clock now carries a decision, not just "sense time". The
	// threshold is asserted as the string the CLOCK renders for it
	// (clock.go tickDecisionLabel), never as a literal: "t+300s" was a
	// hand-written second count beside a tick that now counts in minutes.
	if !strings.Contains(p, tickDecisionLabel) {
		t.Errorf("A6: tick clock has no attached decision (%s)", tickDecisionLabel)
	}
	// C5 — the truthful-reporting line.
	if !strings.Contains(p, "Report outcomes faithfully") {
		t.Errorf("C5: truthful-reporting line missing")
	}
}

// TestTaskToolViewDescription_CarriesTaskProtocol guards 6.4 (C1): the
// task-list emission protocol lives in the `task` tool description (sent
// every round, not counted against the system-prompt density gate) so the
// system prompt can stay under 4500 chars. If this drifts, the system prompt
// has to grow it back. (tool-family merge — task family: was the `todo` tool;
// the protocol now sits on the task tool's view action.)
func TestTaskToolViewDescription_CarriesTaskProtocol(t *testing.T) {
	var desc string
	for _, td := range claudeToolDefs {
		if td.Name == "task" {
			desc = td.Description
		}
	}
	if desc == "" {
		t.Fatal("task missing from claudeToolDefs")
	}
	for _, w := range []string{
		"action=view", "[TASK_LIST_BEGIN]", "[TASK_LIST_END]",
		"task_id", "summary", "status",
		"pending", "progressing", "completed",
		"exactly ONE action",
		"never drop a task_id",
	} {
		if !strings.Contains(desc, w) {
			t.Errorf("task description missing protocol detail %q", w)
		}
	}
}

// TestPromptSectionsSorted verifies the production registry is already
// sorted by order (so the assembled prompt is deterministic).
func TestPromptSectionsSorted(t *testing.T) {
	orders := make([]int, len(promptSections))
	for i, s := range promptSections {
		orders[i] = s.Order
	}
	if !sort.IntsAreSorted(orders) {
		t.Fatalf("promptSections not in order: %v", orders)
	}
}

// TestSystemPrompt_ToolRoutingAuditFixes guards the 4 changes from the
// tool-usage audit (devdoc/tool-usage-audit.md, fixes 2026-08-27): each new
// text must survive the density gate in the assembled prompt. No change here
// = a future edit silently dropped one of the audit's fixes.
func TestSystemPrompt_ToolRoutingAuditFixes(t *testing.T) {
	p := assembleSystemPrompt()
	wants := []string{
		// Fix 1 [高]: git_blame routing line (was 0 calls / 21435 — bash stole it).
		//
		// THE QUOTE IS LEFT OPEN ON PURPOSE. This assertion used to end at the
		// closing `"`, and a later commit (4cc2b5ba) extended the quoted example
		// with a third question — a change that made the routing line BETTER and
		// this gate RED, with a message ("missing audit-fix text") that named a
		// defect nobody had introduced. What the audit fix actually requires is
		// that the routing line carries the example at all, so the assertion pins
		// the text up to the point where authoring legitimately continues.
		`git_blame: git file line-history — "谁写的这行 / 哪次 commit 引入 bug`,
		"not bash git blame",
		// Fix 2 [高]: memory_feedback must be voted immediately, not deferred to task end.
		"IMMEDIATELY after each memory(action=search)",
		// Fix 3 [中]: code_search one-call vs bash-grep two-call-trip anchor.
		"returns every definition in ONE structured call",
		// Fix 5 [高]: context_trim dead-end trigger + trim-NOW anchor (was 4/21435, 0.0%).
		"dead-end/errored/no-value result",
		"trim NOW + leave one-line conclusion",
		"copy header tool_use id",
	}
	for _, w := range wants {
		if !strings.Contains(p, w) {
			t.Errorf("system prompt missing audit-fix text %q", w)
		}
	}
	// Fix 4 [中]: bash description warns that reading a file via bash is a
	// logged violation (tool-description side — the GLM bash-垃圾场 pattern).
	var bashDesc string
	for _, td := range claudeToolDefs {
		if td.Name == "bash" {
			bashDesc = td.Description
		}
	}
	for _, w := range []string{`Reading a file (cat/head/sed -n FILE) when read exists = a logged violation`} {
		if !strings.Contains(bashDesc, w) {
			t.Errorf("bash tool description missing the file-read violation warning %q", w)
		}
	}
}
