// Package pricing resolves per-token prices (元 / 1M tokens) for the models
// gopi talks to, so `gopi session stats --cost` can turn provider-reported usage into
// real per-session money.
//
// The table is DATE/TIME-AWARE, because the two providers' prices move on
// different clocks (see devdoc/glm-5.3-pricing.md, triple-verified
// 2026-08-30, and devdoc/cost-game-ds-glm-real-session-analysis-20260908.md):
//
//   - GLM-5.3-flash: a "5折限时两周" window runs to 2026-09-09 24:00 (+08:00).
//     Inside it the billable price is 0.4/0.115/1.4 (input/cache-hit/output);
//     from 2026-09-10 the ORIGINAL price 0.8/0.23/2.8 applies. No peak hours.
//   - DeepSeek: one card shape, three model names, two date cutovers. The
//     CURRENT card is DeepSeek-V4.1-Flash (model name deepseek-flash,
//     released 2026-09-10 04:00 UTC = 12:00 +08): 0.02/1/4 off-peak and
//     0.04/2/8 at peak (cache-hit/input/output 元 per 1M). Peak hours are
//     Beijing Mon-Fri 09-12 and 14-18, off-peak = half.
//     The retired name deepseek-v4-flash billed the OLD card
//     1.5/0.05/4.5 before that instant and is from it served by V4.1-Flash
//     at the Flash price. The legacy deepseek-v4-pro billed
//     0.15/4.5/13.5 until 2026-09-14 12:00 +08 and is routed to V4.1-Flash
//     at Flash prices after. Prices/provenance: refDSFlash, refDSPro.
//
// Prices are declared as data (PROJECT.md Rule 1 — no magic numbers inlined at
// call sites): a single declarative table below is the ONE source of truth,
// optionally overridable from settings.json ("pricing" section) for a user
// whose effective rate differs (enterprise contract / a newer model).
// Every resolved rate carries a Reference naming the source doc + the regime
// that was in force, so a money number is always traceable (epic hard rule:
// "价格数字标注来源/日期——钱的事要可追溯").
package pricing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// beijingOffsetSec is +08:00 in seconds — the offset both providers' civil
// clocks run on (Rule 1a: the offset is named once, not spelled inline).
const beijingOffsetSec = 8 * 60 * 60

// Provider identifies the model family a session's model string belongs to.
type Provider string

const (
	// DeepSeek is the deepseek-v4-* family (deepseek-* prefix).
	DeepSeek Provider = "deepseek"
	// GLM is the glm-*-flash family (glm-* prefix).
	GLM Provider = "glm"
	// Unknown marks a model string no price table covers (no cost is computed).
	Unknown Provider = ""
)

// Unit is 元 per one million tokens (the CNY billing unit of both providers).
const Unit = 1_000_000.0

// Prices is one rate card: 元 per 1M tokens for the three billable dims.
type Prices struct {
	// Input is the price of a cache MISS (new/uncached input tokens) — the
	// provider bills each miss-prompt token at the input rate. DeepSeek and
	// GLM both auto-cache; there is no separate cache-creation fee (DS) / it
	// is currently free (GLM "cache storage 限时免费").
	Input float64 `json:"input"`
	// CacheHit is the price of a cached input token (much cheaper than
	// Input — 0.05 vs 1.5 on DS, 0.115 vs 0.4 on GLM 5折).
	CacheHit float64 `json:"cache_hit"`
	// Output is the price of a completion (output) token.
	Output float64 `json:"output"`
}

// Tier is one named rate card plus the regime window it applies in.
type Tier struct {
	Provider Provider `json:"provider"`
	// Model is the concrete model string the tier prices. "" = provider-wide
	// fallback (matches any model of the provider that no concrete tier
	// prices) — the escape hatch an override table uses for a new model.
	Model string `json:"model,omitempty"`
	// Name is the human regime label ("GLM-5.3 五折" / "GLM-5.3 原价" /
	// "DS 平价" / "DS 高峰") — surfaced in the stats COST box so the reader
	// sees WHY this price.
	Name  string `json:"name"`
	Price Prices `json:"price"`

	// Until, when non-zero, bounds the tier's eligibility: it applies only
	// to moments BEFORE Until (Beijing civil time). The GLM 五折 tier sets
	// Until = 2026-09-10 00:00 +08:00; the GLM 原价 tier leaves it zero
	// (always on after the discount tier stops matching). Declared as data —
	// the same table a date-aware test walks without hardcoding.
	Until *time.Time `json:"until,omitempty"`
	// Peak marks a DeepSeek peak-hour tier (applies only inside the Beijing
	// 09-12 / 14-18 windows).
	Peak bool `json:"peak,omitempty"`
	// Ref is the tier's own price provenance. Empty = fall back to the
	// provider-level reference (refDS / refGLM*) — set it when a tier's
	// numbers come from a DIFFERENT source than its provider's default (the
	// V4.1-Flash card is a new official page, not the old billing export),
	// so a money number always traces to the page it was read from.
	Ref string `json:"ref,omitempty"`
}

// Reference strings — every money number's provenance (epic hard rule).
const (
	refGLM5 = "devdoc/glm-5.3-pricing.md (bigmodel.cn SPA截图+vision 三重互证 2026-08-30: 五折 0.4/0.115/1.4 至 9/9)"
	refGLM  = "devdoc/glm-5.3-pricing.md (划线原价 0.8/0.23/2.8, 9/9 五折结束后生效)"
	refDS   = "devdoc/cost-game-ds-glm-real-session-analysis-20260908.md (deepseek_usage_amount.txt 账单 price 字段)"
	// refDSFlash is the CURRENT DeepSeek card's provenance: the official
	// pricing page (CNY column), fetched at the timestamp below. The offline
	// archive ~/docs/deepseek-api/quick_start/pricing.md is the same page's
	// snapshot (archived 2026-09-11).
	refDSFlash = "https://api-docs.deepseek.com/zh-cn/quick_start/pricing/ (官方定价页 zh-cn 元栏, fetched 2026-09-11T08:39:59+08:00; 离线快照 ~/docs/deepseek-api/quick_start/pricing.md: deepseek-flash 0.02/1/4 空闲, 0.04/2/8 高峰; 生效 2026-09-10 04:00 UTC = 12:00 +08)"
	// refDSPro is the LEGACY V4 Pro card's provenance (same page, the
	// deepseek-v4-pro column — in force until 2026-09-14 12:00 +08).
	refDSPro = "https://api-docs.deepseek.com/zh-cn/quick_start/pricing/ (deepseek-v4-pro 栏: 0.15/4.5/13.5 空闲, 0.30/9/27 高峰; fetched 2026-09-11T08:39:59+08:00) + change log 2026-09-10 (V4 Pro 9/14 12:00 +08 起路由到 V4.1 Flash 按 Flash 价计费)"
)

// Model-name constants — the wire names a session's "model" field carries
// (PROJECT.md Rule 1b: a name compared or persisted never appears as a bare
// literal; Rule 13: the table below REFERENCES these, never re-types them).
const (
	// modelDSFlash is the current DeepSeek model name (DeepSeek-V4.1-Flash,
	// released 2026-09-10) — the name gopi actually runs.
	modelDSFlash = "deepseek-flash"
	// modelDSV4Flash is the retired V4-Flash name. It is still ACCEPTED and
	// from 2026-09-10 12:00 +08 is served by V4.1-Flash at the Flash price;
	// before that instant it billed at the old V4-Flash price. Kept in the
	// table so historical sessions price at what they were actually billed.
	modelDSV4Flash = "deepseek-v4-flash"
	// modelDSV4Pro is the legacy flagship name: billed at the Pro price until
	// 2026-09-14 12:00 +08, then routed to V4.1-Flash at the Flash price.
	modelDSV4Pro = "deepseek-v4-pro"
)

// Regime labels (the "why this price" string the stats COST box prints).
// The pre-2026-09-10 12:00 legacy-name tiers keep their ORIGINAL labels: for
// a session dated in that window this card IS the true regime, and the
// gopilog goldens pin them (a table edit must not churn them). The
// post-cutover tiers carry a NEW label — same model name, different money —
// so a reader can never mistake one regime for the other.
const (
	regimeDSFlashIdle   = "DS flash 空闲"
	regimeDSFlashPeak   = "DS flash 高峰×2"
	regimeDSV4FlashOld  = "DS v4-flash 平价"
	regimeDSV4FlashOldP = "DS v4-flash 高峰×2"
	regimeDSV4FlashNew  = "DS v4-flash 现价(=flash)"
	regimeDSV4FlashNewP = "DS v4-flash 现价高峰×2"
	regimeDSV4ProIdle   = "DS v4-pro 空闲"
	regimeDSV4ProPeak   = "DS v4-pro 高峰×2"
	regimeDSV4ProRouted = "DS v4-pro→flash(9/14 12:00后)"
	regimeDSV4ProRoutP  = "DS v4-pro→flash 高峰×2"
)

// beijing is the fixed +08:00 zone both providers price in (session JSONL
// timestamps carry their own offset; the regime clocks are Beijing civil time).
// A fixed zone rather than time.LoadLocation: the package stays deterministic
// on a host whose tzdata is missing, and only Beijing civil time enters a price.
var beijing = time.FixedZone("CST", beijingOffsetSec)

// glmDiscountEnd is 2026-09-10 00:00 +08:00 — the instant the "五折限时两周"
// window (through 2026-09-09 24:00) ends and the original price resumes.
var glmDiscountEnd = time.Date(2026, time.September, 10, 0, 0, 0, 0, beijing)

// glmUntil is a *time.Time copy for the declarative tier below.
func glmUntil() *time.Time {
	t := glmDiscountEnd
	return &t
}

// dsPriceCutover is 2026-09-10 12:00 +08:00 (= 04:00 UTC, the instant the
// V4.1-Flash card took effect per the 2026-09-10 change log). Before it the
// DeepSeek names billed at the OLD V4-Flash card; from it the retired
// deepseek-v4-flash name is served by V4.1-Flash at the Flash price.
var dsPriceCutover = time.Date(2026, time.September, 10, 12, 0, 0, 0, beijing)

// dsProRetire is 2026-09-14 12:00 +08:00 (= 04:00 UTC) — from this instant
// every deepseek-v4-pro request is routed to V4.1-Flash and billed at the
// V4.1 Flash price (until a future V4.1-Pro launches).
var dsProRetire = time.Date(2026, time.September, 14, 12, 0, 0, 0, beijing)

// until returns a *time.Time copy of an instant for the declarative table
// (same reason as glmUntil: the table is data, the instants are shared).
func until(t time.Time) *time.Time { return &t }

// The DeepSeek rate cards, named once (Rule 13) and referenced by every tier
// below — the same card is billed under several model names / regimes, so a
// re-typed literal would be a drift point.
var (
	// dsFlashIdle/Peak: DeepSeek-V4.1-Flash (元 per 1M tokens).
	dsFlashIdle = Prices{Input: 1, CacheHit: 0.02, Output: 4}
	dsFlashPeak = Prices{Input: 2, CacheHit: 0.04, Output: 8}
	// dsV4FlashOldIdle/Peak: the retired V4-Flash card (in force before
	// 2026-09-10 12:00 +08; 账单-verified, see refDS).
	dsV4FlashOldIdle = Prices{Input: 1.5, CacheHit: 0.05, Output: 4.5}
	dsV4FlashOldPeak = Prices{Input: 3.0, CacheHit: 0.10, Output: 9.0}
	// dsV4ProIdle/Peak: the legacy V4 Pro card (until 2026-09-14 12:00 +08).
	dsV4ProIdle = Prices{Input: 4.5, CacheHit: 0.15, Output: 13.5}
	dsV4ProPeak = Prices{Input: 9.0, CacheHit: 0.30, Output: 27.0}
)

// deepseekPeakWindows are the Beijing-time peak windows (inclusive start,
// exclusive end) when DS input/output double — DeepSeek's 高峰时段, billed at
// 2x on every dimension. This is the PROVIDER'S rate card, not a gopi policy
// (devdoc/glm-5.3-pricing.md;
// https://api-docs.deepseek.com/zh-cn/quick_start/pricing/), and it is the ONE
// definition in the tree: the priced regime below resolves every model tier
// through it (Rule 14).
//
// HISTORY — WHY THIS PAIR LIVES HERE NOW: it used to be owned by an
// internal/peakhours package shared with the peak-hour START GUARD, a launcher
// that refused to start a gopi invocation inside a window. The guard was
// removed outright (devdoc/peak-guard-removal.md) and the package with it: a
// rate window that cannot refuse anything belongs to the rate table. Deleting
// the guard removed a POLICY, never the fact — `gopi session stats --cost` still prices
// a session run inside these windows at 2x, and TestPeakDoublesMoney pins it.
var deepseekPeakWindows = []peakWindow{{Start: 9, End: 12}, {Start: 14, End: 18}}

// peakWindow is one daily interval in Beijing civil time. Start is inclusive,
// End is exclusive and both are whole hours — the provider's peak hours are
// whole hours, which is exactly what keeps inPeakWindow exact (no
// minute-of-day subtlety exists to round).
type peakWindow struct {
	Start int
	End   int
}

// Default returns the built-in rate table — the single declarative source
// of truth for the models gopi knows how to price. Date/time awareness is
// expressed as windows here and evaluated in resolve (which tier applies
// depends on the moment being priced, so a session's cost never hardcodes
// "today's" promotion).
func Default() []Tier {
	return []Tier{
		{
			Provider: GLM, Model: "glm-5.3-flash",
			Name:  "GLM-5.3 五折",
			Price: Prices{Input: 0.4, CacheHit: 0.115, Output: 1.4},
			Until: glmUntil(),
		},
		{
			Provider: GLM, Model: "glm-5.3-flash",
			Name:  "GLM-5.3 原价",
			Price: Prices{Input: 0.8, CacheHit: 0.23, Output: 2.8},
		},
		// --- DeepSeek ---
		// deepseek-v4-flash BEFORE the 2026-09-10 12:00 +08 card change: what
		// historical sessions were actually billed (账单-verified old card).
		{
			Provider: DeepSeek, Model: modelDSV4Flash,
			Name:  regimeDSV4FlashOld,
			Price: dsV4FlashOldIdle,
			Until: until(dsPriceCutover),
		},
		{
			Provider: DeepSeek, Model: modelDSV4Flash,
			Name:  regimeDSV4FlashOldP,
			Price: dsV4FlashOldPeak,
			Peak:  true,
			Until: until(dsPriceCutover),
		},
		// deepseek-v4-flash FROM the card change: the retired V4-Flash model
		// is served by V4.1-Flash at the Flash price (official footnote (1)),
		// so a post-cutover session under the legacy name prices at Flash
		// rates — the old card would over-bill it 2.5x on cache hits.
		{
			Provider: DeepSeek, Model: modelDSV4Flash,
			Name:  regimeDSV4FlashNew,
			Price: dsFlashIdle,
			Ref:   refDSFlash,
		},
		{
			Provider: DeepSeek, Model: modelDSV4Flash,
			Name:  regimeDSV4FlashNewP,
			Price: dsFlashPeak,
			Peak:  true,
			Ref:   refDSFlash,
		},
		// deepseek-flash = DeepSeek-V4.1-Flash, the only model in production.
		{
			Provider: DeepSeek, Model: modelDSFlash,
			Name:  regimeDSFlashIdle,
			Price: dsFlashIdle,
			Ref:   refDSFlash,
		},
		{
			Provider: DeepSeek, Model: modelDSFlash,
			Name:  regimeDSFlashPeak,
			Price: dsFlashPeak,
			Peak:  true,
			Ref:   refDSFlash,
		},
		// deepseek-v4-pro: at the Pro card until 2026-09-14 12:00 +08 …
		{
			Provider: DeepSeek, Model: modelDSV4Pro,
			Name:  regimeDSV4ProIdle,
			Price: dsV4ProIdle,
			Ref:   refDSPro,
			Until: until(dsProRetire),
		},
		{
			Provider: DeepSeek, Model: modelDSV4Pro,
			Name:  regimeDSV4ProPeak,
			Price: dsV4ProPeak,
			Peak:  true,
			Ref:   refDSPro,
			Until: until(dsProRetire),
		},
		// … and routed to V4.1-Flash at Flash rates from that instant on.
		{
			Provider: DeepSeek, Model: modelDSV4Pro,
			Name:  regimeDSV4ProRouted,
			Price: dsFlashIdle,
			Ref:   refDSPro,
		},
		{
			Provider: DeepSeek, Model: modelDSV4Pro,
			Name:  regimeDSV4ProRoutP,
			Price: dsFlashPeak,
			Peak:  true,
			Ref:   refDSPro,
		},
	}
}

// table holds the active tiers (built-in defaults or an override).
type table struct {
	tiers []Tier
}

// defaultTable is the process-wide table; SetOverride / SetDefault swap its
// tiers (stats --cost resolves prices through this one seam).
var defaultTable = &table{tiers: Default()}

// SetDefault restores the built-in table (after an override; also the test
// reset seam).
func SetDefault() { defaultTable.tiers = Default() }

// SetOverride installs a settings.json "pricing" table in place of the
// defaults. A nil/empty slice restores the defaults. The override is the
// operator's effective-rate escape hatch — it REPLACES, it does not merge
// (a pricing table is a full statement of what the user pays).
func SetOverride(tiers []Tier) {
	if len(tiers) == 0 {
		SetDefault()
		return
	}
	defaultTable.tiers = tiers
}

// LoadSettingsOverride reads the optional "pricing" section of a gopi
// settings.json and installs it as the active table (the settings-level
// escape hatch for an operator whose effective rate differs — enterprise
// contract, a newer model revision, a reseller). The section is a JSON array
// of Tier objects with the same shape the built-in table uses, so a user can
// copy Default()'s shape and change the numbers:
//
//	"pricing": [
//	  {"provider":"glm","model":"glm-5.3-flash","name":"my GLM rate",
//	   "price":{"input":0.4,"cache_hit":0.115,"output":1.4},
//	   "until":"2026-09-10T00:00:00+08:00"},
//	  …
//	]
//
// A missing file or a missing "pricing" key is NOT an error (defaults stay);
// a malformed section is (a wrong override must be loud — silently ignoring
// a user's numbers would mis-price their money). SettingsPath is resolved by
// the caller (config.ResolveSettingsPath), keeping pricing dependency-free.
func LoadSettingsOverride(settingsPath string) error {
	if settingsPath == "" {
		return nil
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("pricing: read settings %s: %w", settingsPath, err)
	}
	// Probe cheaply before the decode (settings files carry api_key — parse
	// only the pricing key, never echo the file).
	if !bytes.Contains(data, []byte(`"pricing"`)) {
		return nil
	}
	var doc struct {
		Pricing json.RawMessage `json:"pricing"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("pricing: parse settings %s: %w", settingsPath, err)
	}
	if len(doc.Pricing) == 0 || string(doc.Pricing) == "null" {
		return nil
	}
	var tiers []Tier
	if err := json.Unmarshal(doc.Pricing, &tiers); err != nil {
		return fmt.Errorf("pricing: settings \"pricing\" section is malformed: %w", err)
	}
	SetOverride(tiers)
	return nil
}

// Rates resolves the rate card for a model string at a moment. ok is false
// when no tier matches (unknown provider/model — the session is NOT priced
// and stats says so rather than invent a number).
func Rates(model string, at time.Time) (regime string, price Prices, ref string, ok bool) {
	return defaultTable.resolve(model, at)
}

// resolve walks the tier list and returns the matching tier's price. Window
// logic is data-driven: a tier with Until set matches only before Until.
// Provider/regime clock is Beijing civil time (both price tables move on
// +08:00).
//
// DeepSeek peak handling: among the tiers that match the model and its Until
// window, a tier whose Peak state equals the current moment's peak state is
// preferred (the built-in table declares both a flat and a Peak tier, so the
// window picks correctly); when the matching pool has only one Peak state
// (an operator's flat-only override), that tier applies at ALL hours — a
// single declared price means "this is what I pay", not "flat until peak
// silently drops me".
func (t *table) resolve(model string, at time.Time) (string, Prices, string, bool) {
	prov := ProviderOfModel(model)
	if prov == Unknown {
		return "", Prices{}, "", false
	}
	bt := at.In(beijing)
	peak := inPeakWindow(bt)

	// Pool = tiers matching provider + model + Until window.
	var pool []*Tier
	for i := range t.tiers {
		tr := &t.tiers[i]
		if tr.Provider != prov {
			continue
		}
		if tr.Model != "" && !strings.HasPrefix(model, tr.Model) {
			continue
		}
		if tr.Until != nil && !bt.Before(*tr.Until) {
			continue
		}
		pool = append(pool, tr)
	}
	if len(pool) == 0 {
		return "", Prices{}, "", false
	}

	// Prefer the tier whose Peak state matches the moment; fall back to the
	// whole pool (a flat-only override prices every hour). Concrete-model
	// tiers beat provider-wide fallbacks.
	match := pick(pool, func(tr *Tier) bool { return tr.Peak == peak })
	if match == nil {
		match = pick(pool, func(*Tier) bool { return true })
	}
	if match == nil {
		return "", Prices{}, "", false
	}
	// Provenance: the tier's own Ref wins — its numbers may come from a
	// different page than the provider default (the V4.1-Flash card is a new
	// official pricing page, not the old billing export) — else the
	// provider-level reference, whose GLM variant is date-windowed.
	ref := match.Ref
	if ref == "" {
		ref = refDS
		if prov == GLM {
			if bt.Before(glmDiscountEnd) {
				ref = refGLM5
			} else {
				ref = refGLM
			}
		}
	}
	return match.Name, match.Price, ref, true
}

// pick returns the best tier satisfying want: concrete-model over
// provider-wide, else the first-declared.
func pick(pool []*Tier, want func(*Tier) bool) *Tier {
	var best *Tier
	for i := range pool {
		tr := pool[i]
		if !want(tr) {
			continue
		}
		if best == nil || moreSpecific(tr, best) {
			best = tr
		}
	}
	return best
}

// moreSpecific prefers an exact-model tier over a provider-wide fallback
// (and keeps first-declared order for two ties).
func moreSpecific(a, b *Tier) bool {
	if a.Model == "" && b.Model != "" {
		return false
	}
	if a.Model != "" && b.Model == "" {
		return true
	}
	return false
}

// inPeakWindow reports whether a Beijing-civil moment falls in a DS peak
// window (inclusive start, exclusive end). Only the hour enters the decision —
// see peakWindow.
func inPeakWindow(bt time.Time) bool {
	h := bt.Hour()
	for _, w := range deepseekPeakWindows {
		if h >= w.Start && h < w.End {
			return true
		}
	}
	return false
}

// Beijing returns the fixed +08:00 zone the price tables move on (exported
// so callers can render a session's local regime moment).
func Beijing() *time.Location { return beijing }

// ProviderOfModel infers the provider from the model string prefix
// (deepseek-* / glm-*). Anything else (hy4-preview, test-model, a local
// model, empty) is Unknown → not priced. Same family rule the stats COST
// box uses to decide whether a session CAN be priced at all.
func ProviderOfModel(model string) Provider {
	switch {
	case strings.HasPrefix(model, "deepseek-"), strings.HasPrefix(model, "deepseek_"):
		return DeepSeek
	case strings.HasPrefix(model, "glm-"):
		return GLM
	}
	return Unknown
}

// Cost is one billable dimension's money + its token volume + its share.
type Cost struct {
	Yuan   float64 `json:"yuan"`
	Tokens int64   `json:"tokens"`
	Pct    float64 `json:"pct"` // share of total money, 0-100 (0 when total is 0)
}

// Result is a session's full cost breakdown under one resolved rate card.
type Result struct {
	// Model / Provider / Regime identify what was priced and why.
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"` // deepseek | glm
	Regime   string `json:"regime,omitempty"`   // "GLM-5.3 五折" | "DS v4-flash 高峰×2" …

	// Hit / Miss / Output are the three billable dims (money + tokens).
	Hit    Cost `json:"cache_hit"`
	Miss   Cost `json:"input_miss"`
	Output Cost `json:"output"`

	// Total is Hit+Miss+Output money (元).
	Total float64 `json:"total_yuan"`
	// Price is the resolved rate card behind these numbers — kept so a
	// per-round breakdown (--bill) can re-price each round's tokens under the
	// SAME regime without re-resolving.
	Price Prices `json:"price"`
	// Amplification is hit money / miss money — the cache n² effect made
	// visible in MONEY (token-count n² can tell a different story when hit
	// and miss prices differ; on DS a 22x token amplification is only a
	// ~0.7x money amplification because hit tokens are 30x cheaper).
	Amplification float64 `json:"cache_amplification,omitempty"`

	// Ref is the price-table provenance (epic hard rule: traceable money).
	Ref string `json:"price_ref,omitempty"`
	// PricedAt is the moment the rate was resolved (session start).
	PricedAt time.Time `json:"priced_at"`
	// Currency is always CNY — declared so the JSON is self-describing.
	Currency string `json:"currency"`
}

// CostOf computes the money a session's token volumes cost at `at` under the
// given model. hit/miss/out are the provider-reported token counts (the same
// numbers SessionStats aggregates). ok=false → the model is not priceable
// (unknown provider or no matching tier); the caller says so instead of
// guessing. PricedAt is the moment the rate resolves — date/time-aware: a
// GLM session started 2026-09-08 prices at 五折 even when viewed on
// 2026-09-20, and a DS session started 10:30 Beijing prices at peak.
func CostOf(model string, hit, miss, out int64, at time.Time) (*Result, bool) {
	regime, price, ref, ok := Rates(model, at)
	if !ok {
		return nil, false
	}
	res := &Result{
		Model:    model,
		Provider: string(ProviderOfModel(model)),
		Regime:   regime,
		PricedAt: at,
		Currency: "CNY",
		Ref:      ref,
		Price:    price,
	}
	res.Hit = Cost{Yuan: yuan(price.CacheHit, hit), Tokens: hit}
	res.Miss = Cost{Yuan: yuan(price.Input, miss), Tokens: miss}
	res.Output = Cost{Yuan: yuan(price.Output, out), Tokens: out}
	res.Total = round6(res.Hit.Yuan + res.Miss.Yuan + res.Output.Yuan)
	if res.Total > 0 {
		res.Hit.Pct = res.Hit.Yuan / res.Total * 100
		res.Miss.Pct = res.Miss.Yuan / res.Total * 100
		res.Output.Pct = res.Output.Yuan / res.Total * 100
	}
	if res.Miss.Yuan > 0 {
		res.Amplification = res.Hit.Yuan / res.Miss.Yuan
	}
	return res, true
}

// yuan converts tokens at a per-1M price into 元 (rounded to 6 decimals —
// the smallest CNY coin is 1/100 but a single round's fractional cent must
// not round to zero before the sum).
func yuan(perM float64, tokens int64) float64 {
	return round6(perM * float64(tokens) / Unit)
}

// round6 rounds to 6 decimals (money arithmetic noise guard).
func round6(f float64) float64 {
	if f < 0 {
		return -round6(-f)
	}
	return float64(int64(f*1e6+0.5)) / 1e6
}
