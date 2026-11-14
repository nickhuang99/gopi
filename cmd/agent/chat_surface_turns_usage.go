package main

// chat_surface_turns_usage.go — the `gopi chat -h` section for the chat_surface
// tool's self-driven turn budget, kept beside the geometry section rather than
// inline in the usage string, and BUILT from the knob's own names (the flag, the
// env var, the settings key, the numbers) instead of typing them again
// (Rule 1b/13/14).
//
// WHY IT IS ON THIS PAGE. The bound is printed on the operator's console as
// `self-driven turn 1/20`, and until this section existed that notice was the
// ONLY place the number appeared in front of a human: the knob was configurable
// but undiscoverable, which is the half-finished shape the memory-prefetch row
// was added to fix for its own knob.

import (
	"strconv"

	"github.com/nickhuang99/gopi/internal/config"
)

// chatSurfaceTurnsUsage is that section. It states the number, what a PERIOD is
// (the operator's own line is the only reset), and that max_turns can only
// tighten a period — never lengthen it.
var chatSurfaceTurnsUsage = `--` + config.ChatSurfaceTurnsFlagName + ` N
                   how many self-driven turns the chat_surface tool may add
                   to ONE period (default ` + strconv.Itoa(config.ChatSurfaceTurnsDefault) + `, accepted
                   ` + strconv.Itoa(config.ChatSurfaceTurnsFloor) + `..` + strconv.Itoa(config.ChatSurfaceTurnsCeiling) + `). A period is the stretch between two lines a
                   HUMAN wrote: every line you submit opens a fresh budget,
                   and the model can never renew it itself. The tool's own
                   max_turns argument clamps INTO this range, so it can
                   shorten a period but never lengthen one. Channels,
                   ordinary precedence: this flag >
                   ` + config.ChatSurfaceTurnsEnv + ` > settings
                   "` + config.ChatSurfaceTurnsSettingsKey + `" > the default; an unusable value
                   is reported and the next channel decides.

`
