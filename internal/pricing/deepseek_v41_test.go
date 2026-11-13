package pricing

import (
	"strings"
	"testing"
	"time"
)

// This file pins the DeepSeek-V4.1-Flash data repair (2026-09-11, epic
// 01a08daf): before it the table knew ONLY the retired name
// `deepseek-v4-flash`, so `gopi session stats --cost` refused to price
// `deepseek-flash` — the only model in production — and the legacy numbers
// it did price were 2.5x too high on cache hits.
//
// Prices/provenance: https://api-docs.deepseek.com/zh-cn/quick_start/pricing/
// (zh-cn CNY column, fetched 2026-09-11T08:39:59+08:00) + change log
// 2026-09-10 (card effective 04:00 UTC = 12:00 +08; V4 Pro routed to V4.1
// Flash from 2026-09-14 12:00 +08). See devdoc/deepseek-v41-flash-pricing-
// table-repair-20260911.md for the fetch transcript and the delta table.

// TestFlashModelPricesAtOfficialV41Rates: the current model name must resolve
// at the official CNY rates, off-peak and peak, with the new card as its
// provenance (the money number has to trace to the page it came from —
// epic rule: 钱要可追溯).
func TestFlashModelPricesAtOfficialV41Rates(t *testing.T) {
	cases := []struct {
		name   string
		at     time.Time
		want   Prices
		regime string
	}{
		{"off-peak evening", bj(2026, 9, 11, 22, 30), dsFlashIdle, regimeDSFlashIdle},
		{"off-peak lunch gap", bj(2026, 9, 11, 13, 0), dsFlashIdle, regimeDSFlashIdle},
		{"off-peak before 9", bj(2026, 9, 11, 8, 59), dsFlashIdle, regimeDSFlashIdle},
		{"peak morning", bj(2026, 9, 11, 10, 0), dsFlashPeak, regimeDSFlashPeak},
		{"peak afternoon", bj(2026, 9, 11, 17, 59), dsFlashPeak, regimeDSFlashPeak},
		{"peak ends at 18:00", bj(2026, 9, 11, 18, 0), dsFlashIdle, regimeDSFlashIdle},
	}
	for _, c := range cases {
		regime, price, ref, ok := Rates(modelDSFlash, c.at)
		if !ok {
			t.Fatalf("%s: Rates(%s @ %v) not ok — the production model must price", c.name, modelDSFlash, c.at)
		}
		if regime != c.regime {
			t.Errorf("%s: regime = %q want %q", c.name, regime, c.regime)
		}
		if !almost(price.Input, c.want.Input, 1e-9) || !almost(price.CacheHit, c.want.CacheHit, 1e-9) ||
			!almost(price.Output, c.want.Output, 1e-9) {
			t.Errorf("%s: price = %+v want %+v", c.name, price, c.want)
		}
		if !strings.Contains(ref, "api-docs.deepseek.com") {
			t.Errorf("%s: ref = %q — must name the official pricing page", c.name, ref)
		}
	}

	// Off-peak is exactly half of peak on every dimension (official rule).
	if got := dsFlashPeak.Input / dsFlashIdle.Input; !almost(got, 2, 1e-9) {
		t.Errorf("peak/off-peak input ratio = %v want 2", got)
	}
	if got := dsFlashPeak.Output / dsFlashIdle.Output; !almost(got, 2, 1e-9) {
		t.Errorf("peak/off-peak output ratio = %v want 2", got)
	}

	// The absolute numbers the official page prints (元 / 1M tokens).
	if dsFlashIdle.CacheHit != 0.02 || dsFlashIdle.Input != 1 || dsFlashIdle.Output != 4 {
		t.Errorf("off-peak card = %+v want 0.02/1/4", dsFlashIdle)
	}
	if dsFlashPeak.CacheHit != 0.04 || dsFlashPeak.Input != 2 || dsFlashPeak.Output != 8 {
		t.Errorf("peak card = %+v want 0.04/2/8", dsFlashPeak)
	}

	// A flash session costs exactly 2x at peak (same usage, both dims doubled).
	const hit, miss, out = 1_000_000, 100_000, 200_000
	flat, okFlat := CostOf(modelDSFlash, hit, miss, out, bj(2026, 9, 11, 22, 0))
	peak, okPeak := CostOf(modelDSFlash, hit, miss, out, bj(2026, 9, 11, 10, 0))
	if !okFlat || !okPeak {
		t.Fatal("flash session not priceable")
	}
	if !almost(peak.Total, flat.Total*2, flat.Total*0.001+0.001) {
		t.Errorf("peak total %.4f should be 2x off-peak %.4f", peak.Total, flat.Total)
	}
}

// TestLegacyV4FlashNameStillPrices: historical sessions under the retired name
// must keep pricing — at the OLD card before the 2026-09-10 12:00 +08 cutover
// (what they were actually billed) and at the Flash price from it (the
// official footnote: legacy-name requests are served by V4.1-Flash and billed
// at the Flash price).
func TestLegacyV4FlashNameStillPrices(t *testing.T) {
	cases := []struct {
		name   string
		at     time.Time
		want   Prices
		regime string
	}{
		{"old card, off-peak", bj(2026, 9, 7, 22, 30), dsV4FlashOldIdle, regimeDSV4FlashOld},
		{"old card, peak", bj(2026, 9, 7, 10, 0), dsV4FlashOldPeak, regimeDSV4FlashOldP},
		{"old card, last off-peak hour before cutover", bj(2026, 9, 10, 8, 59), dsV4FlashOldIdle, regimeDSV4FlashOld},
		// 11:59 on the cutover day is BOTH the last minute of the old card and
		// inside the 09-12 peak window — the card changes at 12:00, not at the
		// end of peak, so the OLD peak card still applies here.
		{"old card, still peak one minute before", bj(2026, 9, 10, 11, 59), dsV4FlashOldPeak, regimeDSV4FlashOldP},
		{"new card at the cutover", bj(2026, 9, 10, 12, 0), dsFlashIdle, regimeDSV4FlashNew},
		{"new card after", bj(2026, 9, 11, 22, 30), dsFlashIdle, regimeDSV4FlashNew},
		{"new card, peak after", bj(2026, 9, 11, 10, 0), dsFlashPeak, regimeDSV4FlashNewP},
	}
	for _, c := range cases {
		regime, price, _, ok := Rates(modelDSV4Flash, c.at)
		if !ok {
			t.Fatalf("%s: Rates(%s @ %v) not ok — legacy sessions must stay priceable", c.name, modelDSV4Flash, c.at)
		}
		if regime != c.regime {
			t.Errorf("%s: regime = %q want %q", c.name, regime, c.regime)
		}
		if !almost(price.Input, c.want.Input, 1e-9) || !almost(price.CacheHit, c.want.CacheHit, 1e-9) ||
			!almost(price.Output, c.want.Output, 1e-9) {
			t.Errorf("%s: price = %+v want %+v", c.name, price, c.want)
		}
	}

	// Revision-suffixed legacy names ride the same rule (prefix match), both
	// sides of the cutover.
	if regime, _, _, ok := Rates("deepseek-v4-flash-0731", bj(2026, 9, 9, 22, 0)); !ok || regime != regimeDSV4FlashOld {
		t.Errorf("deepseek-v4-flash-0731 pre-cutover regime = %q ok=%v", regime, ok)
	}
	if regime, _, _, ok := Rates("deepseek-v4-flash-vision-exp", bj(2026, 9, 11, 10, 0)); !ok || regime != regimeDSV4FlashNewP {
		t.Errorf("deepseek-v4-flash-vision-exp post-cutover regime = %q ok=%v", regime, ok)
	}
}

// TestNewAndLegacyFlashPricesDiffer: the headline defect this repair fixes —
// pricing the production model at the LEGACY name's rates overestimates
// cache-hit money 2.5x and input 1.5x. Same tokens, same moment (pre-cutover
// so both names resolve), different money.
func TestNewAndLegacyFlashPricesDiffer(t *testing.T) {
	const hit, miss, out = 1_000_000, 1_000_000, 1_000_000
	at := bj(2026, 9, 7, 22, 30) // DS off-peak, before the 9/10 cutover

	nw, okNew := CostOf(modelDSFlash, hit, miss, out, at)
	lg, okLegacy := CostOf(modelDSV4Flash, hit, miss, out, at)
	if !okNew || !okLegacy {
		t.Fatalf("both names must price: new ok=%v legacy ok=%v", okNew, okLegacy)
	}
	if nw.Total == lg.Total {
		t.Fatalf("flash and v4-flash priced the same (%.4f) — the repair added no real price", nw.Total)
	}
	// Exact cards: 0.02+1+4 = 5.02 vs 0.05+1.5+4.5 = 6.05.
	if !almost(nw.Total, 5.02, 1e-9) {
		t.Errorf("flash total = %.4f want 5.02", nw.Total)
	}
	if !almost(lg.Total, 6.05, 1e-9) {
		t.Errorf("legacy total = %.4f want 6.05", lg.Total)
	}
	// Per-dimension: the 2.5x cache-hit error is the one that mattered.
	if !almost(lg.Hit.Yuan/nw.Hit.Yuan, 2.5, 1e-9) {
		t.Errorf("legacy/new cache-hit ratio = %v want 2.5 (0.05/0.02)", lg.Hit.Yuan/nw.Hit.Yuan)
	}
	if !almost(lg.Miss.Yuan/nw.Miss.Yuan, 1.5, 1e-9) {
		t.Errorf("legacy/new input ratio = %v want 1.5 (1.5/1)", lg.Miss.Yuan/nw.Miss.Yuan)
	}
	// And after the cutover the legacy name must agree with the new name
	// (same model, same card) — a legacy-name session today is NOT cheaper.
	nw2, _ := CostOf(modelDSFlash, hit, miss, out, bj(2026, 9, 11, 22, 30))
	lg2, ok := CostOf(modelDSV4Flash, hit, miss, out, bj(2026, 9, 11, 22, 30))
	if !ok {
		t.Fatal("legacy name not priceable after the cutover")
	}
	if !almost(nw2.Total, lg2.Total, 1e-9) {
		t.Errorf("post-cutover the names must agree: flash %.4f vs v4-flash %.4f", nw2.Total, lg2.Total)
	}
}

// TestV4ProBillingReroutesAtCutoff: the official V4 Pro retirement — Pro
// rates until 2026-09-14 12:00 +08, V4.1 Flash rates from that instant (the
// card order matters: without the Until window the Pro tiers would keep
// matching and silently bill the retired model 6.75x too much).
func TestV4ProBillingReroutesAtCutoff(t *testing.T) {
	const hit, miss, out = 1_000_000, 1_000_000, 1_000_000
	cases := []struct {
		name   string
		at     time.Time
		want   Prices
		regime string
	}{
		{"Pro card, off-peak (last one before the cutoff)", bj(2026, 9, 14, 8, 59), dsV4ProIdle, regimeDSV4ProIdle},
		{"Pro card, peak", bj(2026, 9, 14, 10, 0), dsV4ProPeak, regimeDSV4ProPeak},
		{"routed at the cutoff", bj(2026, 9, 14, 12, 0), dsFlashIdle, regimeDSV4ProRouted},
		{"routed after", bj(2026, 9, 15, 22, 0), dsFlashIdle, regimeDSV4ProRouted},
		{"routed after, peak", bj(2026, 9, 15, 10, 0), dsFlashPeak, regimeDSV4ProRoutP},
	}
	for _, c := range cases {
		regime, price, _, ok := Rates(modelDSV4Pro, c.at)
		if !ok {
			t.Fatalf("%s: Rates(%s @ %v) not ok", c.name, modelDSV4Pro, c.at)
		}
		if regime != c.regime {
			t.Errorf("%s: regime = %q want %q", c.name, regime, c.regime)
		}
		if !almost(price.Input, c.want.Input, 1e-9) || !almost(price.CacheHit, c.want.CacheHit, 1e-9) ||
			!almost(price.Output, c.want.Output, 1e-9) {
			t.Errorf("%s: price = %+v want %+v", c.name, price, c.want)
		}
	}

	// Money: before the cutoff the Pro card costs 3.6x the Flash card on the
	// total (18.15 vs 5.02) and 7.5x on cache-hit money (0.15/0.02); from the
	// cutoff the session prices at Flash rates.
	before, _ := CostOf(modelDSV4Pro, hit, miss, out, bj(2026, 9, 14, 8, 59))
	after, _ := CostOf(modelDSV4Pro, hit, miss, out, bj(2026, 9, 14, 12, 0))
	if !almost(before.Total, 0.15+4.5+13.5, 1e-9) {
		t.Errorf("v4-pro before cutoff total = %.4f want 18.15", before.Total)
	}
	if !almost(after.Total, 0.02+1+4, 1e-9) {
		t.Errorf("v4-pro after cutoff total = %.4f want 5.02", after.Total)
	}
	if !almost(before.Hit.Yuan/after.Hit.Yuan, 7.5, 1e-9) {
		t.Errorf("v4-pro before/after cache-hit ratio = %v want 7.5 (0.15/0.02)", before.Hit.Yuan/after.Hit.Yuan)
	}
	// The routed tiers carry the Pro provenance (the change log + the page
	// whose footnote announced the reroute), never the Flash card's.
	if !strings.Contains(after.Ref, "changes") && !strings.Contains(after.Ref, "change log") {
		t.Errorf("v4-pro routed ref = %q — must cite the retirement change log", after.Ref)
	}
}

// TestUnpricedModelNamesStayUnpriced: names the official docs do NOT list must
// stay unpriceable — inventing a rate for a typo would silently mis-price
// money (the real session corpus contains such a typo).
func TestUnpricedModelNamesStayUnpriced(t *testing.T) {
	for _, model := range []string{"deepseek-4-flash", "deepseek-chat", "hy4-preview", "glm-4.6", ""} {
		if regime, _, _, ok := Rates(model, bj(2026, 9, 11, 22, 30)); ok {
			t.Errorf("Rates(%q) priced as %q — no official card exists for it", model, regime)
		}
	}
}
