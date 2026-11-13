package tools

import "testing"

// TestLikeCJKRelevanceExprWeightsLongerTerms pins the route-3 ranking key's
// shape: one weighted clause per rankable term (weight = rune length), so a
// more specific match outranks a shorter one.
func TestLikeCJKRelevanceExprWeightsLongerTerms(t *testing.T) {
	expr := likeCJKRelevanceExpr([]string{"网络", "测试环境"})
	args := likeCJKRelevanceArgs([]string{"网络", "测试环境"})
	if len(args) != 2*len(likeCJKTargets) {
		t.Fatalf("likeCJKRelevanceArgs returned %d params for 2 terms, want %d",
			len(args), 2*len(likeCJKTargets))
	}
	if args[0] != "网络" || args[len(likeCJKTargets)] != "测试环境" {
		t.Fatalf("likeCJKRelevanceArgs bound %v, want the bare terms in order", args)
	}
	if expr == likeCJKNoRelevanceExpr {
		t.Fatalf("likeCJKRelevanceExpr returned the constant key for two rankable terms")
	}
	// A term shorter than the rankable floor keeps its recall (the WHERE is
	// built from all terms) but carries no weight.
	if got := likeCJKRelevanceExpr([]string{"的"}); got != likeCJKNoRelevanceExpr {
		t.Errorf("a 1-rune term produced ranking weight %q, want the constant key", got)
	}
	if got := len(likeCJKRelevanceArgs([]string{"的"})); got != 0 {
		t.Errorf("a 1-rune term bound %d ranking params, want 0", got)
	}
}
