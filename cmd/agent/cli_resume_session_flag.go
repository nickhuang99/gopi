package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/sessionid"
)

// sessionResumeFlag is the NO-VERB spelling of a resume: `gopi --session ID
// ["<prompt>"]`.
//
// WHY A FLAG AND NOT A VERB (Rule 3 — no new verb, no new tool): the resume
// itself already has exactly one implementation (`resumeSession`, the core
// extracted from runResume). This face adds only a second ADDRESS for it, on
// the bare-prompt invocation the operator was already typing — the same shape
// as `gopi "<prompt>"`, one flag away.
//
// The namespace is FREE and that is checked, not assumed: config.FlagOverrides
// builds its arity table (config.flagArity) from the env catalog, which carries
// no `session` row, and BootstrapEnvFromArgs hoists only the knobs that need a
// pre-chain hoist (settings/home/tmpdir/output-language/…), none of which is
// `session`. So this face adds no knob, no env var and no dispatch predicate —
// which is exactly why it costs no gate in cli_contracts_test.go /
// cli_verb_census_test.go.
const sessionResumeFlag = "--session"

// sessionResumeProgName is the ONE spelling this face puts in its own
// diagnostics (Rule 1b: one spelling per face, one grep — the same shape
// resumeProgName uses for `gopi session resume`).
const sessionResumeProgName = "gopi " + sessionResumeFlag

// sessionResumeConfigExitCode is the status of an unusable CONFIGURATION line on
// this face — the same POSIX usage class main() exits with for the identical
// error on the run path (one class, so a caller scripting either face reads the
// same number). It is a named constant because an exit status is a contract,
// not a literal (Rule 1).
const sessionResumeConfigExitCode = 1

// sessionResumeUsageHead is the page's FIRST line — the synopsis — named on its
// own because it is the one line a caller can state without copying the page's
// prose: a test asking "did the help query answer?" compares against THIS, and
// the page below OPENS with it, so the two cannot drift apart (Rule 14: one
// definition, referenced, never mirrored; Rule 1: no re-typed usage text).
const sessionResumeUsageHead = `Usage: gopi ` + sessionResumeFlag + ` <session-id|raw-log> [--task <id>] ["<prompt>"]`

// sessionResumeUsage is the `gopi --session -h` page. Every line stays inside
// the help page width (the same rule every other page obeys).
const sessionResumeUsage = sessionResumeUsageHead + `

Continue an interrupted session from its raw HTTP log checkpoint. This is the
no-verb spelling of ` + "`gopi session resume <session-id>`" + ` — one
implementation, two addresses.

<session-id|raw-log>  what to continue: a session UUID (e.g. from
                    'gopi session log --last'), a task id, or the path of a raw
                    log. A UUID's shape is checked BEFORE anything is resolved:
                    a uuid whose dashes are wrong is refused by name, with the
                    canonical spelling it meant (never joined into a second
                    directory).

--task <id>         optional: the ledger row this resume CONTINUES. The resumed
                    session adopts it and its close-out chain runs on it, exactly
                    as the verb face's --task does. Omitted, the target's own row
                    is linked when the ledger resolves one, and a resume that
                    links no row says so.

"<prompt>"          optional: your next instruction. It becomes the request the
                    resumed session acts on. Omitted, the model is told to
                    continue from where it left off without repeating steps
                    already completed.

<session-id|raw-log> and --task may be written in either order; anything else
beginning with '-' is refused by name, never swallowed into the prompt.

Examples:
  gopi ` + sessionResumeFlag + ` 01a096c9-6f7d-7e43-8e46-9ecca58e9cd0
  gopi ` + sessionResumeFlag + ` 01a096c9-6f7d-7e43-8e46-9ecca58e9cd0 "run tests"

Configuration flags — the same knobs, resolved the same way:
  gopi --settings FILE ` + sessionResumeFlag + ` <id> "<prompt>"
  gopi --home DIR ` + sessionResumeFlag + ` <id> --tmpdir DIR

This face takes the configuration knobs the bare-prompt run takes, before OR
after the id, and CONSUMES them (--flag > env > settings.json > default): the
data root and the temp workspace they name apply to the resumed run and to
every child it spawns. The accepted names are the catalog's, the ONE source
` + "`gopi --help-env`" + ` renders from — so a name it does not carry is
refused BY NAME, never silently dropped, and so is a single-dash option.
`

// scanSessionResumeFlag finds the no-verb resume address in raw argv and
// returns the session id it carries plus the argv tail AFTER it (the prompt,
// and anything else the caller typed).
//
// found=false means this is not that face at all (any other invocation): the
// caller then leaves argv untouched for the ordinary chain and agent path.
//
// The id is taken from `--session VALUE` or `--session=VALUE`. A HELP QUERY IS
// NOT A VALUE: `gopi --session -h` asks what the flag does, so "-h" is left in
// the tail for the help branch instead of being taken as the session id (that
// would answer a usage question with a shape refusal).
func scanSessionResumeFlag(args []string) (id string, tail []string, found bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == sessionResumeFlag:
			tail = append(tail, args[:i]...)
			if i+1 < len(args) && args[i+1] != HelpFlag && args[i+1] != HelpFlagLong {
				id = args[i+1]
				tail = append(tail, args[i+2:]...)
			} else if i+1 < len(args) {
				tail = append(tail, args[i+1])
				tail = append(tail, args[i+2:]...)
			}
			return id, tail, true
		case strings.HasPrefix(a, sessionResumeFlag+"="):
			tail = append(tail, args[:i]...)
			tail = append(tail, args[i+1:]...)
			return strings.TrimPrefix(a, sessionResumeFlag+"="), tail, true
		}
	}
	return "", nil, false
}

// sessionResumePrompt splits the face's argv tail into the inline prompt (the
// remaining POSITIONAL words, joined) and the args the CONFIG RESOLUTION must
// see (the catalog configuration knobs, with their operands intact).
//
// THE TWO FACES MUST AGREE ON THE KNOBS (epic resume-surface convergence). The
// bare-prompt run `gopi "<prompt>" --settings X` accepts the catalog flags and
// this face is the same shape, so refusing them here made the two shapes
// disagree on configuration alone. The accepted set is NOT written here: each
// word is asked about through config.CatalogFlagArity, which reads the SAME
// arity table FlagOverrides parses with (built from the EnvVars catalog). So a
// knob added to the catalog is accepted by this face the same day, and there is
// no second list to forget (Rules 1/13/14).
//
// Everything else that begins with `-` is still REFUSED BY NAME (Rule 15): an
// option the catalog does not carry is named back with the alternative, never
// silently dropped into the prompt or ignored.
//
// configArgs is what runConfigResolution consumes — the tail's flag words in
// the order typed, each value-taking flag followed by its operand. Those words
// are ALSO in no way part of the prompt: the prompt is what the operator typed
// as instruction, and `--home /tmp/x` is not instruction.
//
// A flag BEFORE the id (or after it) is the same flag: the scanner hands over
// the whole tail, so position does not decide anything. `--name=VALUE` is
// consumed as one word and `--name VALUE` as two, exactly as FlagOverrides
// splits them (config.CatalogFlagValueTaken is that same split).
func sessionResumePrompt(tail []string) (prompt string, configArgs []string, taskID string, badFlag string) {
	var words []string
	for i := 0; i < len(tail); i++ {
		a := tail[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			words = append(words, a)
			continue
		}
		// A help word is not configuration: the caller answered it before this
		// point and it must not reach the flag parser (which would refuse it as
		// unknown, turning a usage question into an error).
		if a == HelpFlag || a == HelpFlagLong {
			continue
		}
		// THE ROW LINKAGE IS THIS FACE'S OWN (Rule 15b: a face never refuses a
		// word it advertises). `--task <id>` is in this face's usage page and
		// in the ONE resume renderer (runcmd.ResumeArgv) that a spawned child is
		// built from, so it is consumed HERE instead of being asked about in
		// the CATALOG arity table — where, measured 2026-10-18, it was refused
		// by name, which made every spawned recovery child exit 1 before its
		// first instruction and left the crashed row unclosed.
		//
		// The OPERAND rule is the verb face's own (parseResumeArgs): `--task=ID`
		// inline, or `--task ID`; a missing or empty value is that face's
		// refusal, reached by handing the bare flag on with no operand.
		if name, val, inline := splitTaskLinkageFlag(a, tail[i+1:]); name {
			if !inline {
				if val == "" {
					return "", nil, "", runcmd.FlagTask
				}
				i++
			}
			if strings.TrimSpace(val) == "" {
				return "", nil, "", runcmd.FlagTask
			}
			taskID = strings.TrimSpace(val)
			continue
		}
		needsValue, known := config.CatalogFlagArity(a)
		if !known {
			return "", nil, taskID, a
		}
		configArgs = append(configArgs, a)
		if inline, ok := config.CatalogFlagValueTaken(a); ok {
			// `--name=value` carries its operand; a value-taking flag with an
			// EMPTY operand is the parser's own refusal to make, not ours —
			// but an empty one is almost certainly a typo, so let the parser
			// answer it (`--name= requires a value` would be a guess here).
			_ = inline
			continue
		}
		if i+1 >= len(tail) {
			continue // dangling flag: the parser names the missing value
		}
		next := tail[i+1]
		if needsValue {
			if strings.HasPrefix(next, "-") && next != "-" {
				// The operand is another flag: the parser's
				// "flag --name requires a value" is the sentence that names
				// this, and it needs to SEE both words to say so.
				continue
			}
			configArgs = append(configArgs, next)
			i++
			continue
		}
		// Boolean knob: the parser consumes a following token only when it is a
		// boolean spelling, and the prompt builder must consume the SAME token
		// or `--debug false "run tests"` would leave `false` in the prompt.
		if config.IsCatalogBoolValue(next) {
			configArgs = append(configArgs, next)
			i++
		}
	}
	return strings.TrimSpace(strings.Join(words, " ")), configArgs, taskID, ""
}

// splitTaskLinkageFlag recognizes the row linkage in either spelling this face
// advertises (`--task ID` / `--task=ID`) and returns its operand plus whether it
// was inline.
//
// The SPELLING is not typed here: it is runcmd.FlagTask, the ONE constant the
// resume renderer composes a child's argv from (Rule 1/14) — a second literal
// is how the renderer and the face would drift apart again.
//
// The operand of a bare `--task` is the NEXT word, and it is taken only when
// that word is not itself a flag — the same rule the verb face's
// parseResumeArgs applies (`--task --db` is a missing operand, not an id). An
// empty val therefore means the operand is missing, and the caller refuses by
// naming the flag (Rule 15).
func splitTaskLinkageFlag(a string, rest []string) (ok bool, val string, inline bool) {
	if a == runcmd.FlagTask {
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			return true, rest[0], false
		}
		return true, "", false
	}
	if strings.HasPrefix(a, runcmd.FlagTask+"=") {
		return true, strings.TrimPrefix(a, runcmd.FlagTask+"="), true
	}
	return false, "", false
}

// sessionResumeFaceArmed reports whether argv carries the no-verb resume
// address. It is the ONE question the top-level help scan asks before it
// answers a `-h`: `-h` next to this flag is the FLAG's query, not gopi's
// (mirroring the subcommand chain, which is likewise checked before that scan).
func sessionResumeFaceArmed(args []string) bool {
	_, _, found := scanSessionResumeFlag(args)
	return found
}

// runSessionResumeFlag is the no-verb `--session` face. handled=false means
// argv does not carry the flag and the caller continues with the ordinary
// dispatch chain; otherwise the returned code is this process's exit status.
//
// ORDER IS THE CONTRACT (and it is what the three real-run checks observe):
//
//  1. the help query, before any resolution — `-h` must never resolve a log;
//  2. the id's PRESENCE, then its SHAPE via internal/sessionid — a
//     non-canonical spelling is refused by the package's own sentence, with
//     the canonical id it meant, and never reaches the filesystem (that is
//     the whole reason the package exists: joining such an id mints a SECOND
//     session container beside the row's own);
//  3. the resume itself, through the ONE core — resolveResumeLogPath locates
//     the raw log (session_start timestamp pairing, re-homed sibling, etc.),
//     and every error it reports is this face's error.
func runSessionResumeFlag(args []string) (handled bool, code int) {
	id, tail, found := scanSessionResumeFlag(args)
	if !found {
		return false, 0
	}
	for _, a := range tail {
		if a == HelpFlag || a == HelpFlagLong {
			fmt.Print(sessionResumeUsage)
			return true, 0
		}
	}
	if strings.TrimSpace(id) == "" {
		fmt.Fprintf(os.Stderr, "%s: %s requires a session id\n\n%s", sessionResumeProgName, sessionResumeFlag, sessionResumeUsage)
		return true, 1
	}
	if !sessionid.IsAdmissible(id) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", sessionResumeProgName, sessionid.Refuse(id))
		return true, 1
	}
	prompt, configArgs, taskID, badFlag := sessionResumePrompt(tail)
	if badFlag != "" {
		if badFlag == runcmd.FlagTask {
			// The row linkage is THIS face's word, so its operand's refusal is
			// this face's sentence too — the same sentence the verb face's
			// parseResumeArgs gives, so a caller reading either face learns the
			// same requirement.
			fmt.Fprintf(os.Stderr, "%s: %s requires a non-empty row id\n", sessionResumeProgName, runcmd.FlagTask)
			return true, 1
		}
		fmt.Fprintf(os.Stderr, "%s: unknown option %q — this face takes a session id, an optional prompt, and the CATALOG's configuration flags (the names `gopi --help-env` lists a flag for, plus --home/--settings/--tmpdir); use %q when the resume must be linked to a row\n",
			sessionResumeProgName, badFlag, resumeProgName)
		return true, 1
	}
	// THE CONFIGURATION IS RESOLVED BEFORE THE RESUME, by the SAME function the
	// bare-prompt run calls (cmd/agent/run_config_resolution.go): the two faces
	// are one shape, so a `--settings` / `--home` / `--tmpdir` here means what it
	// means there — including for the settings.json read, the provider
	// credentials in it, and every child the resumed run spawns. Refusing the
	// knobs (what this face did until now) was the asymmetry the epic repairs.
	if _, _, cerr := resolveRunConfig(configArgs); cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", sessionResumeProgName, cerr)
		reportConfigErrorDetail(cerr)
		fmt.Fprintf(os.Stderr, "%s: the configuration flags this face accepts are `gopi %s -h`\n", sessionResumeProgName, sessionResumeFlag)
		return true, sessionResumeConfigExitCode
	}
	return true, resumeSession(sessionResumeProgName, strings.TrimSpace(id), "", taskID, prompt)
}
