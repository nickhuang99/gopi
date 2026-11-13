package main

// The CONFIGURATION RESOLUTION of the three long-running SERVICE faces:
// `gopi web`, `gopi cli` and `gopi ui`.
//
// THE DEFECT THIS FILE CLOSES. It is the chat face's defect (6fd8b134,
// chat_config_resolution.go) on three more surfaces, and it has the SAME cause,
// not a similar one: `main()` calls resolveRunConfig AFTER the subcommand chain
// (cmd/agent/main.go, the line below the dispatch predicates), and every one of
// those faces is dispatched from INSIDE that chain and exits from there. So
// resolveRunConfig was never reached on any of these paths, config.ApplyEnv
// never ran, and NOT ONE settings.json knob reached the process environment.
//
// WHY THAT IS THE MOST EXPENSIVE PLACE FOR IT TO HAPPEN. internal/agent's
// request assembler reads GOPI_REASONING_EFFORT / GOPI_THINKING /
// GOPI_THINKING_BUDGET / GOPI_EFFORT_VALUES from the ENVIRONMENT only, and its
// documented default is thinking=DISABLED (loop.go: the escape hatch is
// settings "thinking": "enabled" → ApplyEnv → the variable). A machine whose
// settings.json says `"reasoning_effort": "high"` therefore sent every `gopi
// web` / `gopi cli` / `gopi ui` request with thinking disabled and no effort at
// all, while a bare-prompt run of the same binary sent the configured values.
// The three surfaces are the ones an operator SITS IN FRONT OF, so the quieter
// behaviour was the one they saw: measured 2026-11-12 on this host, an agent
// dispatch run and a `gopi chat` run of the same binary disagreed about the
// operator's own configuration, and chat's half had already been fixed.
//
// WHY IT IS A CALL AND NOT A COPY OF THE STEPS. resolveRunConfig carries the
// five steps and their order (flag parse, --home export, settings read,
// ApplyEnv, temp-workspace publication) and Rule 2 makes that order a
// contract: a second, service-shaped copy would be a second precedence that
// drifts on the first edit. resolveServiceRunConfig below adds NO step of its
// own — it adds the argv REDUCTION a verb face needs (catalogConfigArgs,
// run_config_args.go — the ONE reduction, shared with the chat face) and the
// face's own VOICE for a failure, and then calls the one function.
//
// WHY IT RUNS ONCE, AT START-UP. These are SERVERS: `webServer.newWebAgent` is
// a factory that a resumed conversation, a retry and (in the ui carrier) a
// second engine can all call again. Hanging the resolution on the factory would
// re-read the settings file and re-publish the environment on every agent
// construction — a per-turn configuration read whose second run could disagree
// with the first. The resolution belongs to the START of the service, before
// the first agent exists and after every refusal has already returned: a usage
// page, an unknown flag, a session address that names nothing and a missing
// text browser must all still change NOTHING in the process environment.

import (
	"fmt"
	"os"
)

const (
	// serviceConfigExitOK is "the configuration resolved; carry on with the
	// service". Named rather than written as 0 at the three call sites, so the
	// three faces cannot spell their own success differently (Rule 1).
	serviceConfigExitOK = 0
	// serviceConfigExitFailed is this package's ordinary "the run cannot
	// proceed" status — the same 1 every other start-up refusal on these three
	// faces returns (a bad flag line, a port that will not bind, no browser).
	serviceConfigExitFailed = 1
)

// resolveServiceRunConfig resolves and publishes the configuration for a
// long-running service face, speaking in that face's voice on failure.
//
// facePrefix is the verb's own prefix ("gopi web: ") — the ONE source for each
// spelling is chat_policy_flag.go's chatFacePrefix* block, reused here rather
// than re-typed (Rule 13/14). args is the face's argv TAIL (the words after the
// verb), handed in unmodified: the reduction decides which of them the
// resolution may see, and it does so through the catalog's own exported
// predicates (run_config_args.go's comment carries that argument).
//
// A FAILURE IS THIS FACE'S SENTENCE, NOT THE RUN PATH'S. reportRunConfigError
// prints the top-level usage page, which documents a command line the operator
// did not type; the chat face made this split (chat_config_resolution.go) and
// the resume face before it. reportConfigErrorDetail is the half that is the
// same for every face (a malformed settings file is a named hard exit with its
// path and its fix), so the two pieces compose without either being copied.
func resolveServiceRunConfig(facePrefix string, args []string) int {
	flagArgs, _ := catalogConfigArgs(args)
	if _, _, err := resolveRunConfig(flagArgs); err != nil {
		fmt.Fprintf(os.Stderr, "%s%v\n", facePrefix, err)
		reportConfigErrorDetail(err)
		return serviceConfigExitFailed
	}
	return serviceConfigExitOK
}

// resolveWebRunConfig is `gopi web`'s entry point into the resolution above.
// Called from runWeb AFTER the argv walk, the usage return and the session
// seed's refusal (so none of them can mutate the environment), and BEFORE the
// profile lock, the listener and the agent — which is what makes the operator's
// settings.json reach the request assembler.
func resolveWebRunConfig(args []string) int {
	return resolveServiceRunConfig(chatFacePrefixWeb, args)
}

// resolveTermRunConfig is `gopi cli`'s entry point. Called from runTermChat
// after the usage return and the missing-browser refusal, before the listener
// and the agent.
func resolveTermRunConfig(args []string) int {
	return resolveServiceRunConfig(chatFacePrefixCli, args)
}

// resolveUIRunConfig is `gopi ui`'s entry point. Called from runUIChat after the
// usage return and before uiStartServer — i.e. before the in-process engine
// exists, so the engine, its agent and every request the TUI causes see the
// resolved values.
func resolveUIRunConfig(args []string) int {
	return resolveServiceRunConfig(chatFacePrefixUI, args)
}
