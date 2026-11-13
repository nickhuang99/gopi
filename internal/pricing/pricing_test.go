package pricing

import (
	"math"
	"os"
	"testing"
	"time"
)

// beijing helper for tests.
func bj(y, mo, d, h, mi int) time.Time {
	return time.Date(y, time.Month(mo), d, h, mi, 0, 0, beijing)
}

func almost(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// TestDefaultTableAgainstVerifiedDevdoc pins the triple-verified price table
// (devdoc/glm-5.3-pricing.md, 2026-08-30 bigmodel.cn SPA截图+vision+DOM
// 三重互证) as a permanent regression guard: 0.4/0.115/1.4 五折 → 9/9,
// 0.8/0.23/2.8 原价 after, and the DS 账单-verified 1.5/0.05/4.5 (peak
// double 9-12/14-18).
func TestDefaultTableAgainstVerifiedDevdoc(t *testing.T) {
	cases := []struct {
		model  string
		at     time.Time
		want   Prices
		regime string
		ok     bool
	}{
		// GLM 五折 window (session 2026-08-30, before 9/10 00:00 +08:00).
		{"glm-5.3-flash", bj(2026, 8, 30, 11, 29), Prices{0.4, 0.115, 1.4}, "GLM-5.3 五折", true},
		// GLM on the LAST discounted day (9/9 23:59 +08:00) still 五折.
		{"glm-5.3-flash", bj(2026, 9, 9, 23, 59), Prices{0.4, 0.115, 1.4}, "GLM-5.3 五折", true},
		// GLM 原价 from 9/10 00:00 +08:00.
		{"glm-5.3-flash", bj(2026, 9, 10, 0, 0), Prices{0.8, 0.23, 2.8}, "GLM-5.3 原价", true},
		{"glm-5.3-flash", bj(2026, 9, 20, 10, 0), Prices{0.8, 0.23, 2.8}, "GLM-5.3 原价", true},
		// DS 平价 evening (session 88299d17 22:30, fixture 20:40).
		{"deepseek-v4-flash", bj(2026, 9, 7, 22, 30), Prices{1.5, 0.05, 4.5}, "DS v4-flash 平价", true},
		{"deepseek-v4-flash", bj(2026, 9, 4, 20, 40), Prices{1.5, 0.05, 4.5}, "DS v4-flash 平价", true},
		// DS peak window 09:00-11:59.
		{"deepseek-v4-flash", bj(2026, 9, 7, 10, 0), Prices{3.0, 0.10, 9.0}, "DS v4-flash 高峰×2", true},
		// DS peak window 14:00-17:59.
		{"deepseek-v4-flash", bj(2026, 9, 7, 17, 59), Prices{3.0, 0.10, 9.0}, "DS v4-flash 高峰×2", true},
		// DS back to flat at 18:00.
		{"deepseek-v4-flash", bj(2026, 9, 7, 18, 0), Prices{1.5, 0.05, 4.5}, "DS v4-flash 平价", true},
		// DS flat at 12:00-13:59 (lunch gap).
		{"deepseek-v4-flash", bj(2026, 9, 7, 13, 0), Prices{1.5, 0.05, 4.5}, "DS v4-flash 平价", true},
		// Model prefix revisions price like the base model.
		{"deepseek-v4-flash-0731", bj(2026, 9, 7, 22, 30), Prices{1.5, 0.05, 4.5}, "DS v4-flash 平价", true},
		// Unknown provider → not priced.
		{"hy4-preview", bj(2026, 9, 7, 22, 30), Prices{}, "", false},
		{"test-model", bj(2026, 9, 7, 22, 30), Prices{}, "", false},
	}
	for _, c := range cases {
		regime, price, _, ok := Rates(c.model, c.at)
		if ok != c.ok {
			t.Errorf("Rates(%s @ %v) ok=%v want %v", c.model, c.at, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if regime != c.regime {
			t.Errorf("Rates(%s @ %v) regime=%q want %q", c.model, c.at, regime, c.regime)
		}
		if !almost(price.Input, c.want.Input, 1e-9) || !almost(price.CacheHit, c.want.CacheHit, 1e-9) ||
			!almost(price.Output, c.want.Output, 1e-9) {
			t.Errorf("Rates(%s @ %v) = %+v want %+v", c.model, c.at, price, c.want)
		}
	}
}

// TestCostOfMatchesVerifiedRealSessions re-derives the REAL session totals
// that devdoc/cost-game-ds-glm-real-session-analysis-20260908.md verified
// (0.68 元 / 46% output for 88299d17 at DS off-peak; 0.372 元 / 67% cache
// hit for the GLM session 002c43f2) through the public CostOf API — the
// numbers behind the epic's "DS head=output 46-51%, GLM head=cache-hit
// 68-70%" claim, locked as a regression guard.
func TestCostOfMatchesVerifiedRealSessions(t *testing.T) {
	// 88299d17: DS off-peak (2026-09-07 22:30 +08:00), 101 req, hit 3,158,016
	// / miss 141,124 / out 68,834 → 0.6793 元 total, hit 23%/miss 31%/out 46%,
	// 22.4x token amp (REAL session numbers — re-derived from the JSONL above).
	const (
		hit, miss, out = 3_158_016, 141_124, 68_834
	)
	res, ok := CostOf("deepseek-v4-flash", hit, miss, out, bj(2026, 9, 7, 22, 30))
	if !ok {
		t.Fatal("CostOf(deepseek-v4-flash @ off-peak) not ok")
	}
	if !almost(res.Total, 0.6793, 0.0001) {
		t.Errorf("88299d17 total = %.4f want ~0.6793", res.Total)
	}
	if !almost(res.Output.Pct, 46, 1) {
		t.Errorf("88299d17 output pct = %.1f want ~46", res.Output.Pct)
	}
	if !almost(res.Hit.Yuan, 0.1579, 0.0005) {
		t.Errorf("88299d17 hit yuan = %.4f want ~0.1579", res.Hit.Yuan)
	}
	if !almost(res.Miss.Yuan, 0.2117, 0.0005) {
		t.Errorf("88299d17 miss yuan = %.4f want ~0.2112", res.Miss.Yuan)
	}
	// Token-count amplification is 22.4x but MONEY amplification is tiny
	// (hit tokens are 30x cheaper): hit money 0.158 / miss money 0.212 < 1.
	// This is the research finding that n²-token ≠ n²-money — worth pinning.
	tokAmp := float64(hit) / float64(miss)
	if !almost(tokAmp, 22.4, 0.3) {
		t.Errorf("88299d17 token amp = %.1f want ~22.4", tokAmp)
	}
	if res.Amplification >= 1 {
		t.Errorf("88299d17 money amp = %.2f — DS cache-hit money should NOT exceed miss money", res.Amplification)
	}

	// 002c43f2 (GLM, 2026-08-30 11:29 +08:00, 五折 window): 65 req, hit
	// 2,164,736 / miss 111,929 / out 55,924 → 0.372 元 total, 67% cache-hit
	// money — GLM's head is cache-hit (opposite lever from DS).
	res2, ok2 := CostOf("glm-5.3-flash", 2_164_736, 111_929, 55_924, bj(2026, 8, 30, 11, 29))
	if !ok2 {
		t.Fatal("CostOf(glm-5.3-flash @ 五折) not ok")
	}
	if !almost(res2.Total, 0.372, 0.001) {
		t.Errorf("002c43f2 total = %.4f want ~0.372", res2.Total)
	}
	if !almost(res2.Hit.Pct, 67, 1) {
		t.Errorf("002c43f2 hit pct = %.1f want ~67", res2.Hit.Pct)
	}
	if !almost(res2.Hit.Yuan, 0.2489, 0.001) {
		t.Errorf("002c43f2 hit yuan = %.4f want ~0.2489", res2.Hit.Yuan)
	}
	// Same GLM session priced AFTER the 五折 window must cost ~2x more.
	res3, ok3 := CostOf("glm-5.3-flash", 2_164_736, 111_929, 55_924, bj(2026, 9, 20, 11, 29))
	if !ok3 {
		t.Fatal("CostOf(glm-5.3-flash @ 原价) not ok")
	}
	if !almost(res3.Total, res2.Total*2, res2.Total*2*0.001+0.001) {
		t.Errorf("GLM 原价/五折 total ratio = %.3f want ~2.0 (0.8/0.4)", res3.Total/res2.Total)
	}
}

// TestPeakDoublesMoney: a DS session with identical usage costs exactly 2x at
// peak hours (the 翻倍 is on every dimension, so the total doubles too).
func TestPeakDoublesMoney(t *testing.T) {
	const hit, miss, out = 1_000_000, 100_000, 200_000
	flat, _ := CostOf("deepseek-v4-flash", hit, miss, out, bj(2026, 9, 7, 22, 0))
	peak, _ := CostOf("deepseek-v4-flash", hit, miss, out, bj(2026, 9, 7, 10, 0))
	if !almost(peak.Total, flat.Total*2, flat.Total*2*0.001+0.001) {
		t.Errorf("peak total %.4f should be 2x flat %.4f", peak.Total, flat.Total)
	}
}

// TestProviderOfModel pins the provider-inference rule.
func TestProviderOfModel(t *testing.T) {
	cases := map[string]Provider{
		"deepseek-v4-flash":            DeepSeek,
		"deepseek-v4-pro":              DeepSeek,
		"deepseek-v4-flash-0731":       DeepSeek,
		"deepseek-v4-flash-vision-exp": DeepSeek,
		"glm-5.3-flash":                GLM,
		"hy4-preview":                  Unknown,
		"test-model":                   Unknown,
		"":                             Unknown,
	}
	for model, want := range cases {
		if got := ProviderOfModel(model); got != want {
			t.Errorf("ProviderOfModel(%q) = %q want %q", model, got, want)
		}
	}
}

// TestLoadSettingsOverride covers the settings.json "pricing" escape hatch:
// a user's effective-rate table replaces the built-ins, a missing section
// keeps them, and a malformed section errors loudly (money must be right).
func TestLoadSettingsOverride(t *testing.T) {
	SetDefault()
	defer SetDefault()

	// A settings file with no "pricing" section → defaults stay.
	dir := t.TempDir()
	plain := dir + "/plain.json"
	if err := os.WriteFile(plain, []byte(`{"model":"glm-5.3-flash"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadSettingsOverride(plain); err != nil {
		t.Fatalf("plain settings: %v", err)
	}
	if regime, _, _, ok := Rates("glm-5.3-flash", bj(2026, 8, 30, 10, 0)); !ok || regime != "GLM-5.3 五折" {
		t.Errorf("defaults should survive a plain settings file, got regime=%q ok=%v", regime, ok)
	}

	// An override with a custom rate (until-window honored).
	ov := dir + "/override.json"
	body := `{"api_key":"x","pricing":[
	  {"provider":"glm","model":"glm-5.3-flash","name":"my glm",
	   "price":{"input":0.1,"cache_hit":0.02,"output":0.3},
	   "until":"2026-10-01T00:00:00+08:00"},
	  {"provider":"deepseek","model":"deepseek-v4-flash","name":"my ds",
	   "price":{"input":2.0,"cache_hit":0.1,"output":5.0}}
	]}`
	if err := os.WriteFile(ov, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadSettingsOverride(ov); err != nil {
		t.Fatalf("override settings: %v", err)
	}
	regime, price, _, ok := Rates("glm-5.3-flash", bj(2026, 9, 20, 10, 0))
	if !ok || !almost(price.Input, 0.1, 1e-9) || regime != "my glm" {
		t.Errorf("override glm not applied: regime=%q price=%+v ok=%v", regime, price, ok)
	}
	// The until window: after 2026-10-01 the glm override no longer matches
	// (no fallback tier was declared) → not priceable.
	if _, _, _, ok := Rates("glm-5.3-flash", bj(2026, 10, 2, 10, 0)); ok {
		t.Error("glm priced after its override until-window (no fallback tier declared)")
	}
	// DS override is flat (no peak flag) → peak hours still use it (the
	// operator declared a single price for all hours).
	regime, price, _, ok = Rates("deepseek-v4-flash", bj(2026, 9, 20, 10, 0))
	if !ok || !almost(price.Input, 2.0, 1e-9) || regime != "my ds" {
		t.Errorf("override ds not applied at peak: regime=%q price=%+v ok=%v", regime, price, ok)
	}

	// A malformed section is loud.
	bad := dir + "/bad.json"
	if err := os.WriteFile(bad, []byte(`{"pricing":[{"provider":"glm"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadSettingsOverride(bad); err == nil {
		t.Error("malformed pricing section accepted, want an error")
	}
}

// TestOverrideRoundTrip covers the direct SetOverride/SetDefault seam (the
// loader's backend): an installed override replaces the defaults, and
// SetDefault restores them.
func TestOverrideRoundTrip(t *testing.T) {
	SetDefault()
	defer SetDefault()
	SetOverride([]Tier{{
		Provider: GLM, Model: "glm-5.3-flash",
		Name: "test 0.1/0.01/0.2", Price: Prices{Input: 0.1, CacheHit: 0.01, Output: 0.2},
	}})
	defer SetDefault()
	regime, price, _, ok := Rates("glm-5.3-flash", bj(2026, 9, 7, 10, 0))
	if !ok || !almost(price.Input, 0.1, 1e-9) || regime != "test 0.1/0.01/0.2" {
		t.Fatalf("override not applied: regime=%q price=%+v ok=%v", regime, price, ok)
	}
	// The override table prices only glm → deepseek is no longer priceable.
	if _, _, _, ok := Rates("deepseek-v4-flash", bj(2026, 9, 7, 10, 0)); ok {
		t.Error("deepseek still priceable after a glm-only override — expected no tier")
	}
	// Model-prefix fallback: an unknown glm revision still matches the glm
	// provider-wide tier only when Model is "" — here Model is concrete, so a
	// different glm model is NOT priced by this table (realistic? the built-in
	// table also only prices glm-5.3-flash).
	if _, _, _, ok := Rates("glm-4.6", bj(2026, 9, 7, 10, 0)); ok {
		t.Error("glm-4.6 priced by a glm-5.3-flash-only table — expected no tier")
	}
}
