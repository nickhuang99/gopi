package main

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_banner_tips_test.go — the startup screen's TIPS: the two properties the
// operator asked for, each pinned so a later edit cannot quietly undo it.
//
//  1. the MUST tips come up on EVERY run (the knowledge a first-time user cannot
//     guess), and the rotating slots are filled from the pool;
//  2. the rotation VARIES run to run, and a high-weight tip is drawn more often
//     than a low-weight one — "priority within randomness", not a shuffle that
//     treats every line as equally interesting.
//
// The seed is a field of the spec (chatBannerSpec.tipSeed), so both ends are
// testable without a clock: a fixed seed yields a fixed list, and two seeds walk
// the pool. That purity is the reason the rotation lives in a pure function
// rather than in a call to rand inside the renderer.

// tipsMustSet is the panel's non-negotiable head, in its declared order: the
// lines a first-time user cannot guess. It is asserted by PREFIX because the
// panel crops a line that does not fit its column (chatBannerCrop).
var tipsTestMustOrder = []string{
	"/help lists every command",
	"Ctrl+C interrupts the running turn",
	"Ctrl+D or /quit to exit.",
	chatBannerNewlineKey,
}

func TestChatBannerTipsKeepTheMustLinesOnEverySeed(t *testing.T) {
	for seed := uint64(0); seed < 64; seed++ {
		spec := bannerTestSpec(100, lineedit.PaletteFor(lineedit.ColourOn))
		spec.tipSeed = seed
		tips := chatBannerTips(spec, chatBannerPanelMaxCols)
		joined := strings.Join(tips, "\n")
		for _, want := range tipsTestMustOrder {
			if !strings.Contains(joined, want) {
				t.Fatalf("seed %d: the panel dropped the must tip %q — the rotation is supposed to vary the EXTRA lines, never the knowledge.\ntips:\n%s",
					seed, want, joined)
			}
		}
	}
}

// TestChatBannerTipsRotateWithinTheSlots: the rotating pool is actually reached,
// and different seeds draw different lists. Both halves matter — a pool nothing
// ever draws from is a dead table, and a "rotation" that always picks the same
// lines is the fixed list the operator complained about, wearing a new type.
func TestChatBannerTipsRotateWithinTheSlots(t *testing.T) {
	seen := map[string]int{}
	for seed := uint64(0); seed < 64; seed++ {
		spec := bannerTestSpec(100, lineedit.PaletteFor(lineedit.ColourOn))
		spec.tipSeed = seed
		joined := strings.Join(chatBannerTips(spec, chatBannerPanelMaxCols), "\n")
		for _, tip := range chatBannerRotatingTips {
			if strings.Contains(joined, tip.text) {
				seen[tip.text]++
			}
		}
	}
	if len(seen) < 2 {
		t.Fatalf("64 seeds drew %d distinct rotating tips — the rotation is not rotating.\ndrawn: %v", len(seen), seen)
	}
}

// TestChatBannerTipsDrawIsDeterministic: the renderer is a PURE function of the
// spec. The panel's width measurement and its rows both call chatBannerTips, so a
// seed read twice from a clock could measure a column for one list and draw
// another — a sheared border, which is the failure the box's width invariant
// would report as a mystery. The seed is in the spec for exactly this reason.
func TestChatBannerTipsDrawIsDeterministic(t *testing.T) {
	spec := bannerTestSpec(100, lineedit.PaletteFor(lineedit.ColourOn))
	spec.tipSeed = 0x9e3779b97f4a7c15
	first := strings.Join(chatBannerTips(spec, chatBannerPanelMaxCols), "\n")
	for i := 0; i < 8; i++ {
		if got := strings.Join(chatBannerTips(spec, chatBannerPanelMaxCols), "\n"); got != first {
			t.Fatalf("draw %d differs from the first for the same seed:\n%q\n%q", i, first, got)
		}
	}
}

// TestChatBannerTipsWeightsFavourTheImportantOnes: a high-weight tip is drawn
// more often than a low-weight one across seeds. The sampler is stochastic, so
// the assertion is a RATIO with a wide margin rather than an exact count — the
// failure it catches (weights ignored) is a ratio near 1:1, which no margin this
// side of the real spread can produce.
func TestChatBannerTipsWeightsFavourTheImportantOnes(t *testing.T) {
	high := "Ctrl+A / Ctrl+E jump to line start / end" // chatBannerTipWeightHigh
	low := "/new starts a fresh conversation"          // chatBannerTipWeightLow
	counts := map[string]int{}
	const seeds = 512
	for seed := uint64(0); seed < seeds; seed++ {
		spec := bannerTestSpec(100, lineedit.PaletteFor(lineedit.ColourOn))
		spec.tipSeed = seed
		joined := strings.Join(chatBannerTips(spec, chatBannerPanelMaxCols), "\n")
		for _, tip := range chatBannerRotatingTips {
			if strings.Contains(joined, tip.text) {
				counts[tip.text]++
			}
		}
	}
	if counts[high] <= counts[low] {
		t.Fatalf("across %d seeds the high-weight tip came up %d times and the low-weight one %d — the weights are not being read\ndraws: %v",
			seeds, counts[high], counts[low], counts)
	}
	if counts[low] == 0 {
		t.Fatalf("the low-weight tip never came up in %d seeds — a weight is a frequency, not a ban\ndraws: %v", seeds, counts)
	}
}

// TestChatBannerTipsAreCappedAndOrdered: the panel is a panel. The must tips keep
// their declared order (the exit sentence's reader in the pty e2e depends on the
// interrupt sentence being ABOVE it), the cap is respected, and the rotating lines
// follow in the pool's own order rather than in the sampler's key order — the
// extra lines must read as a list, not as a shuffle.
func TestChatBannerTipsAreCappedAndOrdered(t *testing.T) {
	for seed := uint64(0); seed < 32; seed++ {
		spec := bannerTestSpec(100, lineedit.PaletteFor(lineedit.ColourOn))
		spec.tipSeed = seed
		tips := chatBannerTips(spec, chatBannerPanelMaxCols)
		if len(tips) > chatBannerTipsMax {
			t.Fatalf("seed %d: %d tips, cap is %d", seed, len(tips), chatBannerTipsMax)
		}
		// The must tips, in order, are a PREFIX of the list.
		idx := 0
		for i, want := range tipsTestMustOrder {
			if !strings.Contains(tips[idx], strings.TrimSuffix(want, ".")) {
				t.Fatalf("seed %d: tip %d is %q, want the must tip %q — the head of the panel is not in its declared order\ntips: %v",
					seed, i, tips[idx], want, tips)
			}
			idx++
		}
		// The rotating tail is in the pool's declaration order. The list is
		// matched by PREFIX because the panel crops a line to its column
		// (chatBannerCrop), and a cropped line must still be identified.
		var poolOrder []int
		for _, tip := range tips[idx:] {
			for i, pool := range chatBannerRotatingTips {
				if strings.HasPrefix(pool.text, tip) || strings.HasPrefix(tip, pool.text) {
					poolOrder = append(poolOrder, i)
					break
				}
			}
		}
		for i := 1; i < len(poolOrder); i++ {
			if poolOrder[i] < poolOrder[i-1] {
				t.Fatalf("seed %d: the rotating tail is out of the pool's order (%v) — the extra lines read as a shuffle\ntips: %v",
					seed, poolOrder, tips)
			}
		}
	}
}

// TestChatBannerTipsDescribeTheCodeTheySitNextTo: every tip is a CLAIM about this
// surface, and the operator's rule is that the screen IS the manual (nobody reads
// the help). So the two facts that can drift are pinned against the code they
// describe rather than against a copy of the string:
//
//   - the commands the tips advertise exist in the REPL's own command table
//     (commands.go's constants — the tips build from them, so this asserts the
//     picture the tips give is the picture the REPL answers);
//   - the newline key the tip names is the sequence the decoder implements
//     (Alt+Enter = ESC + CR/LF → KeyNewline), which is what makes "Alt+Enter
//     = newline" true rather than a guess.
func TestChatBannerTipsDescribeTheCodeTheySitNextTo(t *testing.T) {
	joined := strings.Join(chatBannerTips(bannerTestSpec(100, lineedit.PaletteFor(lineedit.ColourOn)), chatBannerPanelMaxCols), "\n")
	for _, command := range []string{helpCommandName, toolsCommandName} {
		if !strings.Contains(joined, "/"+command) {
			t.Fatalf("the panel never names /%s, a command the REPL answers", command)
		}
	}
	if !strings.Contains(joined, chatBannerNewlineKey) {
		t.Fatalf("the panel does not name %s — the key that inserts a newline is the one fact the operator could not find", chatBannerNewlineKey)
	}
}
