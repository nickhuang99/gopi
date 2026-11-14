package main

// chat_ambiguous_width_usage.go — the `gopi chat -h` section for the A-class
// width knob, kept beside the geometry section for the same reason that one is
// not inline in the usage string: the text is BUILT from the knob's own names
// (the flag, the two accepted values, the env var, the settings key) instead of
// typing them again (Rule 1b/13/14).
//
// WHY IT EXISTS AT ALL. The usage line above has advertised `ambiguous-width`
// since the knob landed, and this page had no row for it — so the flag was
// documented in the synopsis and explained nowhere. The closed-vocabulary walk
// measured the hole once the word joined the parser's list: "--ambiguous-width
// parses but is not documented in chatUsage". A reader who saw the synopsis had
// no way to learn what the two words mean, which is the half the synopsis
// cannot carry.

import "github.com/nickhuang99/gopi/internal/config"

// chatAmbiguousWidthUsage is that section. It states the ONE question the knob
// answers (how many cells an East-Asian AMBIGUOUS rune occupies), the two named
// values, and that the locale decides when nobody says anything.
const chatAmbiguousWidthUsage = `--` + config.AmbiguousWidthFlagName + ` ` + config.AmbiguousWidthWide + `|` + config.AmbiguousWidthNarrow + `
                   how many cells an East-Asian AMBIGUOUS rune (box
                   drawing, arrows, some punctuation) occupies on the
                   terminal this chat draws into: ` + config.AmbiguousWidthWide + ` = two
                   cells (the CJK-terminal reading), ` + config.AmbiguousWidthNarrow + ` = one
                   (the Western reading, gopi's historical
                   behaviour). It changes cell arithmetic only, never the
                   text. Declaring neither word leaves the LOCALE in
                   charge — LC_ALL, then LC_CTYPE, then LANG — and a
                   locale that says nothing means ` + config.AmbiguousWidthNarrow + `. The
                   flag is PUBLISHED into its env channel
                   (` + config.AmbiguousWidthEnv + `, the same one settings
                   "` + config.AmbiguousWidthSettingsKey + `" feeds), so all three channels meet
                   in ONE reader. An unusable value is reported and the
                   next channel decides — a typo never makes chat
                   unusable.

`
