package agent

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// systemPromptDensityBudget is the character ceiling the density gate enforces
// on the KERNEL (the assembled prompt WITHOUT the operator's reply-language
// directive). It is named here rather than typed at the two comparison sites
// (this gate and the unlimited-budget gate in sessionbudget_test.go) so "what
// the budget is" has one definition (Rule 1/13).
//
// WHY THE KERNEL AND NOT THE WHOLE STRING (the 2026-09-29 defect this constant's
// comment exists to prevent). The gate used to measure len(assembleSystemPrompt())
// — the whole string INCLUDING the directive — while calling itself "the
// assembled prompt (identity + all discipline blocks)". Those are not the same
// object: the directive's text and length come from the OPERATOR'S
// configuration (config.OutputLanguageDirective(), resolved from
// --output-language / GOPI_OUTPUT_LANGUAGE / settings output_language), and at
// zh-CN it is 292 chars. So a kernel of 4495 chars — the number several
// comments in this package still cite as the ceiling's consumption — measured
// as 4789 and the gate went RED on master for every operator who configured a
// language, while the kernel itself had 5 chars of headroom. The gate was
// measuring a budget the kernel cannot spend and does not control.
//
// WHAT IS DELIBERATELY NOT DONE HERE. The ceiling is NOT relaxed: 4500 still
// bounds the kernel, and the kernel is the part whose growth is this system's
// decision. Nor is the directive subtracted as a magic number — the directive
// is measured through the same resolver the prompt uses, so a future change to
// its wording moves the measurement with it.
const systemPromptDensityBudget = 4500

// derivedPromptSectionNames are the sections whose LENGTH is not this
// package's to decide, so the density budget cannot be spent on them.
//
// A derived section renders a value another package owns at assembly time.
// Its size is that owner's business: `commands` renders `internal/config`'s
// `commandIndex` — the one table `gopi -h` prints — so its length moves when a
// command is added or retired, and no prompt edit can compress it. Charging
// those characters to the kernel would turn every verb change into a red
// density gate and, worse, would put the prompt's rule budget and the CLI's
// command census in one number.
//
// This is NOT a relaxation of the ceiling: the budget keeps bounding every
// section whose text this package writes, at the same 4500, and a derived
// section that silently grew a rule-like paragraph is caught by its OWN gate
// (commands_section_test.go: the block must be byte-equal to
// `gopi -h`'s own index, so nothing but a `commandIndex` change can move it).
// The exclusion list is a NAMED set rather than a predicate for the same
// reason: adding a second entry is a visible edit to a reviewed list, not a
// property a new section can satisfy by accident.
var derivedPromptSectionNames = []string{commandsSectionName}

// ruleKernelSections returns the registry without the derived sections.
func ruleKernelSections() []PromptSection {
	derived := make(map[string]bool, len(derivedPromptSectionNames))
	for _, n := range derivedPromptSectionNames {
		derived[n] = true
	}
	out := make([]PromptSection, 0, len(promptSections))
	for _, s := range promptSections {
		if derived[s.Name] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ruleKernel is the part of the kernel the density budget governs: the join of
// every section this package writes, in the same order and with the same
// separator the real assembly uses (joinPromptSections — the surface itself,
// not a re-implementation of it).
func ruleKernel() string {
	return joinPromptSections(ruleKernelSections())
}

// TestSystemPromptKernelIgnoresTheOperatorDirective is the regression guard for
// the 2026-09-29 defect itself: the kernel must NOT contain the directive, and
// the composed prompt MUST begin with it. Written as a property of the two
// strings rather than of a length, so it holds in every environment — with no
// directive configured the two are equal, and with one they differ by exactly
// the directive plus one separator.
func TestSystemPromptKernelIgnoresTheOperatorDirective(t *testing.T) {
	kernel := systemPromptKernel()
	full := assembleSystemPrompt()
	d := config.OutputLanguageDirective()
	if d == "" {
		if full != kernel {
			t.Errorf("no directive is configured, but the composed prompt differs from the kernel by %d chars",
				len(full)-len(kernel))
		}
		return
	}
	if strings.Contains(kernel, d) {
		t.Error("the KERNEL contains the operator's reply-language directive — the density budget is again " +
			"counting text the kernel does not control")
	}
	if !strings.HasPrefix(full, d) {
		t.Errorf("the composed prompt does not begin with the resolved directive %q", d)
	}
}
