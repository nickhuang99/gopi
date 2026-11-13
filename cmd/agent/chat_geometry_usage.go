package main

// chat_geometry_usage.go — the `gopi chat -h` section for the ideal-geometry
// knob, kept beside the knob's own surface code (chat_geometry.go) rather than
// inline in the usage string.
//
// WHY IT IS A NAMED PART. The usage text is a constant that the help page and
// the refusal message both print, and the section below is BUILT from the knob's
// own names — the flag, the env var, the settings key, the built-in default —
// instead of typing them again (Rule 1b/13/14). A page that spelled the flag
// while the parser read a different one would be a page advertising a command
// line the verb refuses, which is exactly what the sibling dump flags' contract
// test guards against.

import "github.com/nick/gopi/internal/config"

// chatGeometryUsage is that section. It states the three facts a reader needs:
// the size, that a wider terminal draws the canvas at that size rather than
// stretching, and that a narrower one is TOLD what it lost (Rule 15).
const chatGeometryUsage = `--` + config.ChatIdealGeometryFlagName + ` COLS` + config.ChatIdealGeometryDimSep + `ROWS
                   the size the chat CHROME is designed for (default
                   ` + config.ChatIdealGeometryDefault + `; the knob's flag) — the box with its tips
                   panel, the editing line and status row, the info bar's
                   aligned columns. At least this wide the canvas is drawn at
                   EXACTLY this width (never stretched); a NARROWER terminal
                   keeps its own width and is TOLD which piece of chrome it
                   cannot host — never a silent downgrade. Rows are never
                   imposed: the status row is anchored to the screen's LAST row.
                   Channels, ordinary precedence: this flag >
                   ` + config.ChatIdealGeometryEnv + ` > settings ` + config.ChatIdealGeometrySettingsKey + `; the joined
                   and pre-verb forms are accepted. A value that is not
                   COLS` + config.ChatIdealGeometryDimSep + `ROWS is reported and the NEXT channel decides —
                   a typo never makes chat unusable. man gopi-chat.

`
