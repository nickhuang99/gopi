package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// TestSessionAddressesIsAConstantExpression is the anti-regression guard for the
// SESSION-address shape, and it exists because the shape was already lost once
// (measured 2026-10-05, branch `feat/verb-retirement-complete`): the addresses
// were written as JOINER FUNCTIONS (`cliverbs.SessionAddress(word) string`), a
// function call cannot appear in a constant expression, so `selfKnowledgeRules`
// — the self-knowledge section's text — was downgraded from `const` to `var` to
// call them. That silently gave up the property `TestSelfKnowledgeSectionIsStatic`
// exists to protect: the section is a COMPILE-TIME string, so the rendered prompt
// is byte-identical on every turn and the prompt-cache prefix cannot be disturbed
// by a per-turn lookup.
//
// The vector below is the only spelling that keeps both properties at once — the
// section stays `const` AND the addresses stay built from the leaf constants —
// and this test pins BOTH halves:
//
//	(1) the section's declaration is still a `const` (a `var` fails here with the
//	    reason, not with a mysterious cache miss months later);
//	(2) cliverbs exports the address as a DECLARATION whose right-hand side is a
//	    `+`-joined expression over the leaf constants — no function, and no
//	    second literal of the group word.
//
// It is scoped to what it can actually see: the SOURCE of the declaration. A
// constant expression that is correct here and fails to compile is impossible
// (the compiler is the other half of this guard), and a `var` reintroduced in
// loop.go is caught by (1) even if the addresses stayed constant.
func TestSessionAddressesIsAConstantExpression(t *testing.T) {
	// (1) The section is declared `const`.
	src, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatalf("read loop.go: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, "const selfKnowledgeRules =") {
		t.Fatal("selfKnowledgeRules is not declared `const` — the section's text must be a compile-time " +
			"constant expression so the prompt-cache prefix cannot be disturbed by a per-turn lookup " +
			"(a `var` computed at init would still be stable per process, but it forfeits the guarantee " +
			"TestSelfKnowledgeSectionIsStatic asserts and invites the very lookup this comment warns about)")
	}
	if strings.Contains(text, "var selfKnowledgeRules") {
		t.Error("loop.go declares `var selfKnowledgeRules` as well — one definition, and it is the const one")
	}

	// (2) cliverbs exports the address as a constant expression over the leaves.
	cbs, err := os.ReadFile("../cliverbs/cliverbs.go")
	if err != nil {
		t.Fatalf("read ../cliverbs/cliverbs.go: %v", err)
	}
	cts := string(cbs)
	const name = "SessionAddresses"
	decl := "const " + name + " = "
	i := strings.Index(cts, decl)
	if i < 0 {
		t.Fatalf("cliverbs declares no `const %s = …` — a joiner FUNCTION there forces the section back to a "+
			"`var`; an address is a fixed string over the group word and the leaf verbs, so it belongs in a "+
			"constant expression", name)
	}
	rhs := cts[i+len(decl):]
	if j := strings.IndexByte(rhs, '\n'); j >= 0 {
		rhs = rhs[:j]
	}
	// The expression must NAME each leaf constant (identifier), never retype the
	// word (literal) — that is the whole difference between a constant expression
	// over one definition and a second literal.
	for _, leafID := range []string{"Session", "SessionLogAddress", "SessionRawAddress", "SessionStatsAddress"} {
		if !strings.Contains(rhs, leafID) {
			t.Errorf("%s never builds the address from %s — the leaves must come from their own constants, "+
				"never be retyped", name, leafID)
		}
	}
	for _, banned := range []string{`"` + cliverbs.Log + `"`, `"` + cliverbs.Raw + `"`, `"` + cliverbs.Stats + `"`,
		`"` + cliverbs.Session + `"`} {
		if strings.Contains(rhs, banned) {
			t.Errorf("%s retypes %s — the address is a constant expression over cliverbs' own constants "+
				"(Rule 13: a defined constant bypassed by a literal is the defect this guards)", name, banned)
		}
	}
	if strings.Contains(rhs, "func") || strings.Contains(rhs, "(") {
		t.Errorf("%s is not a constant expression (%q) — a call there forces selfKnowledgeRules back to a var",
			name, strings.TrimSpace(rhs))
	}

	// (3) The section actually renders the address, so the guard above is about a
	// live path rather than a decorative constant.
	if !strings.Contains(text, "cliverbs."+name) {
		t.Errorf("selfKnowledgeRules never renders cliverbs.%s — the address constant exists but the section "+
			"still spells the session family some other way", name)
	}
}
