package main

// chat_prefetch_usage.go — the `gopi chat -h` section for the typing-time memory
// prefetch knob, kept beside the knob's own surface code (chat_prefetch.go)
// rather than inline in the usage string. It is the sibling of
// chat_geometry_usage.go and exists for the same two reasons: the usage text is
// a constant the help page prints, and the section is BUILT from the knob's own
// names — the flag, the invocation example, the env var, the settings key, the
// record file name — instead of typing them again (Rule 1b/13/14). A page that
// spelled the flag while the hoist read a different one would advertise a command
// line gopi does not accept.
//
// WHY THIS SECTION EXISTS AT ALL (measured on the built binary at this task's
// base commit 6c17d819, before it did).
// The knob shipped complete — three channels, a resolver, an executor, records,
// tests, a CONFIG.md §2c row — and `gopi chat -h` did not mention it:
//
//	$ gopi chat -h 2>&1 | grep -i prefetch ; echo $?
//	1
//
// The verb this feature exists FOR was the one surface that could not tell a
// reader it existed.
//
// WHY IT IS PROSE AND NOT A `--flag  description` DEFINITION ROW. This knob's
// flag is a TOP-LEVEL one, and this surface's parser REFUSES it after the verb
// (`gopi chat --memory-prefetch` exits 2 naming it as an unknown flag). A
// definition row here would advertise a command line `gopi chat` rejects, so the
// page states the position constraint instead — the definition row lives on the
// front door (`gopi -h`), where the flag is accepted, and the derivation
// (scripts/g5-chat-web-help-surface.sh) reads this block as prose, not as one of
// this verb's flags.
//
// The section head and every continuation line stay within config.HelpWrapCol so
// the block reads correctly in an 80-column terminal, and no line is a flag
// definition at two-space indent (the shape the derivation would count).

import "github.com/nick/gopi/internal/config"

const chatPrefetchUsage = `                   Typing-time MEMORY PREFETCH — written BEFORE the verb,
                   as ` + "`gopi " + config.MemoryPrefetchInvocation + "`" + `, because the flag
                   is ` + config.MemoryPrefetchVerbPositionClaim + `:
                   this surface's own parser refuses it after the verb
                   (exit 2) rather than ignoring it silently or reading it
                   as the seed positional.
                   While you type, a READ-ONLY fetch of the distilled
                   knowledge base runs in the background and its hits ride
                   into the next turn's prompt; nothing is written to the
                   memory DB, and each run appends one
                   ` + config.MemoryPrefetchRecordName + ` line per event to the
                   session directory. Written bare it means ON,
                   ` + "`--" + config.MemoryPrefetchFlagName + "=off`" + ` turns it off
                   again, and precedence is flag=off >
                   ` + config.MemoryPrefetchEnv + ` > settings
                   ` + config.MemoryPrefetchSettingsKey + `. OFF means OFF: nothing is
                   started, no query is issued, and the prompt is
                   byte-identical to the pre-knob tree.
                   Records, and the full CONFIG.md §2c row: man gopi-chat.

`
