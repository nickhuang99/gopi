package main

// The CONFIGURATION RESOLUTION of the chat surface.
//
// THE DEFECT THIS FILE CLOSES. Every run-shaped face resolved the configuration
// — `resolveRunConfig`, cmd/agent/run_config_resolution.go, the ONE place the
// precedence lives — EXCEPT the chat surface: `runChatWithInput` went straight
// from its argv walk to the agent, so config.ApplyEnv was never called on that
// path and NOT ONE settings.json knob reached a chat process's environment.
// The measured consequence is the one that matters most: internal/agent's
// request assembler reads GOPI_REASONING_EFFORT / GOPI_THINKING / GOPI_THINKING_BUDGET
// from the ENVIRONMENT only, so a chat run with `"reasoning_effort": "high"`
// and `"thinking_budget": 16384` in settings.json sent every request with
// thinking=disabled and no effort at all, while a bare-prompt run on the SAME
// machine sent the configured values. Two faces of one binary disagreed about
// the operator's own configuration, and the quieter one was the surface the
// operator sits in front of all day.
//
// WHY IT IS A CALL AND NOT A COPY OF THE STEPS. resolveRunConfig carries the
// five steps and their order (flag parse, --home export, settings read,
// ApplyEnv, temp-workspace publication), and Rule 2 makes that order a
// contract: a second, chat-shaped copy would be the second precedence that
// drifts on the first edit. So this file adds NO step — it adds the argv
// REDUCTION a verb face needs (catalogConfigArgs, run_config_args.go) and the
// chat surface's own VOICE for a failure, and then calls the one function.

import (
	"fmt"
	"os"
)

// resolveChatRunConfig resolves and publishes the configuration for a chat run.
//
// It is called from `runChatWithInput` AFTER the argv walk (so a usage query, a
// refusal and `--film` have already returned — a page must not change the
// process environment) and BEFORE any agent is built (so the agent, its system
// prompt and every request it assembles see the resolved values).
//
// The argv handed in is the chat tail, REDUCED to the catalog's own flags by
// catalogConfigArgs: chat's own switches (`--tools`, `--replay`, …) are not
// flags of the run path, and FlagOverrides would refuse them as unknown. The
// flags the two faces genuinely share (`--ambiguous-width`,
// `--chat-ideal-geometry`, `--chat-surface-turns`, `--film-terminal`,
// `--prompt`) are published twice — by chat's own parser and here — and that is
// idempotent BY CONSTRUCTION rather than by luck: both write the SAME env name
// through the SAME publisher (config.ApplyEnvAmbiguousWidth /
// config.PublishChatIdealGeometryFlag / … are the writers ApplyEnv and the walk
// both go through), so the second write is the same value.
//
// A FAILURE IS THIS FACE'S SENTENCE, NOT THE RUN PATH'S. reportRunConfigError
// prints the top-level usage page, which documents a command line the operator
// did not type; the resume face already made this split (it prints
// `gopi --session: …` plus reportConfigErrorDetail's fix hint), and chat does
// the same — the same error, in the voice of the verb that raised it (Rule 15:
// a refusal names its own cause and its own fix).
func resolveChatRunConfig(args []string) int {
	flagArgs, _ := catalogConfigArgs(args)
	if _, _, err := resolveRunConfig(flagArgs); err != nil {
		fmt.Fprintf(os.Stderr, "%s%v\n", chatErrorPrefix, err)
		reportConfigErrorDetail(err)
		return chatExitFailed
	}
	return chatExitOK
}
