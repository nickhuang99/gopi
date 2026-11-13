package main

// cli_sandbox.go — the `gopi tool sandbox` verb: create, inspect and show the ONE
// standard agent sandbox.
//
// WHY A CLI FACE (Rule 3): the sandbox must be reachable from a shell script, a
// briefing, and a human at a terminal — the places where a sandbox is actually
// needed. The LLM tool surface is frozen, and the alternative that existed until
// now (`SB=$(mktemp -d)` + a hand-written GOPI_HOME) is precisely the
// anti-pattern the epic removes. A CLI verb is the only shape that fits both
// constraints, exactly as it was for `scratch-gc`.
//
// It is a THIN face: the shape and the idempotence are internal/sandbox, the
// reclaimer is internal/scratch.SweepSandboxes, and the configuration is
// config.ResolveSandboxRoot. Nothing about the sandbox is decided here — this
// file parses arguments, resolves the root through the ONE resolver, and prints
// the path, so a script can do `SB=$(gopi tool sandbox create --label build)` and get
// the same shape every other caller gets.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sandbox"
	"github.com/nick/gopi/internal/scratch"
)

// The verb's own constants (Rule 1b). Every spelling an operator can type or
// grep lives here once, so the parser, the completion table and the tests
// cannot drift from the usage text.
const (
	// SandboxCreateVerb is the sub-verb that materialises a sandbox — the ONE
	// initializer's CLI face.
	SandboxCreateVerb = "create"
	// SandboxRunVerb is the sub-verb that materialises a sandbox AND runs a
	// child inside it. It exists because a sandbox was only ever a PATH: a child
	// launched by absolute path inherited the CALLER's working directory (a
	// run's git worktree), and an orphan left standing there made the
	// finish-cleanup holder guard refuse that worktree forever. `run` binds the
	// allocation to the execution — one entry point, one cwd.
	SandboxRunVerb = "run"
	// SandboxSepToken ends the sandbox's own flags and begins the child's argv.
	// It is required (not optional): the sandbox's flags and the child's flags
	// share a namespace, so a caller must be able to say where one ends.
	SandboxSepToken = "--"
	// SandboxShowVerb prints where the sandbox root IS, and which channel
	// decided it. It is the question the anti-pattern answers wrongly ("where
	// should I put one?") — answered here from configuration.
	SandboxShowVerb = "show"
	// SandboxLabelFlag names the sandbox (`--label build` → sandbox-build-<rand>)
	// and is Create's idempotence key: the same label twice reuses one sandbox.
	SandboxLabelFlag = "--label"
	// SandboxTaskIDFlag records the ledger row in the ownership marker.
	SandboxTaskIDFlag = "--task-id"
	// SandboxDirFlag is the ROOT flag this verb accepts, so the documented
	// precedence (flag > env > settings > default) is reachable from the verb
	// itself. It is an ALIAS of config.SandboxFlagName (Rule 14) — the same
	// spelling the global parser, the catalog and the resolver's help use, and a
	// retyped literal here is exactly the drift that made `--sandbox-dir` work
	// for `config` and be an "unexpected argument" for this verb.
	SandboxDirFlag = "--" + config.SandboxFlagName
	// SandboxLedgerSeedFlag names the database COPIED to become the new
	// sandbox's ledger, so a caller never has to export an environment variable
	// to seed one. It is the FLAG FACE of the knob whose environment channel is
	// config.SandboxLedgerSeedEnvName, and it exists because of a measured
	// failure, not a symmetry (task 01a19574, 2026-11-01): the capability was
	// implemented, tested and documented — in the ENVIRONMENT only — so the
	// executor of a SIGPIPE experiment that needed a real, populated, WRITABLE
	// ledger never saw it and hand-built six `cp` directories holding 645 MiB
	// each, outside the sandbox channel and outside every reclaim leg. An env
	// var nobody prints is a capability nobody finds; a flag is on the page.
	//
	// It FEEDS Options.LedgerSeedPath and does not re-read the environment
	// (Rule 2): ledgerSeedPath() in internal/sandbox remains the ONE place the
	// precedence (Options > env) is decided.
	SandboxLedgerSeedFlag = "--ledger-seed"
)

// sandboxUsageFingerprint is the usage text's first line — the string the CLI
// contract suite asserts, so a reworded usage cannot leave a stale fingerprint
// behind in a test.
const sandboxUsageFingerprint = "gopi tool sandbox — the ONE standard agent sandbox (configured, private, reclaimable)"

// sandboxUsage is the verb's help. It names the anti-pattern explicitly: the
// usage text is where an agent looks before inventing something.
const sandboxUsage = sandboxUsageFingerprint + `

Usage:
  gopi tool sandbox create [--label NAME] [--task-id ID]  # materialise one; prints its path
  gopi tool sandbox run [create flags] -- CMD ARGS...   # materialise one AND run CMD inside it
  gopi tool sandbox show [--sandbox-dir PATH]           # print the resolved root and its source
  gopi tool sandbox -h                                    # this help (zero side-effects)

What a sandbox IS: a private gopi profile directory of the standard shape —
ONE shape, and the ledger is the file a run WRITES FIRST, so it is never a link:

  .gopi/                                       (the PROFILE root — GOPI_HOME points here)
  │   settings.json      a READ-ONLY COPY (0444) of the operator's real document
  │   plugins -> the REAL plugin tree          (symlink, READ-ONLY by contract)
  │   memory.db         the sandbox's OWN ledger (REAL file, 0600, WRITABLE)
  │   sessions/ raw/ logs/                     (private, empty, writable)
  .sandbox-owner.json                          (task id + pid + start ticks)

THE SETTINGS DOCUMENT IS A COPY, NOT A LINK — a measured ruling, not a
preference (2026-09-27). A link is followed by a shell REDIRECT, so a fixture
command of the form  printf '{...}' > "$SB/.gopi/settings.json"  truncated the
OPERATOR'S live settings.json through the link and destroyed the api_key and the
repos table 'gopi task merge' keys on. The copy answers the same reads (the sandbox
runs the operator's real configuration) and a write to it is refused by the
kernel instead of reaching the live file. Declare a sandbox run's own knobs in
its OWN settings file (` + config.SettingsEnv + `), never as a write into the
profile.

ONE LINK REMAINS, FOR A DIFFERENT REASON: the PLUGIN TREE, because a sandbox
must run the operator's REAL tools and a copy drifts the moment a script is
deployed. It is linked at the level the profile resolves
(<GOPI_HOME>/.gopi/plugins), which is where the web-search scripts are looked
up, so ` + config.WebSearchScriptsDirEnvName + ` and GOPI_HOME=$SB gopi web search
resolve through the SAME tree this side links. A host with no plugin tree is
still a valid host: the create line says so
(plugins=<path>(absent-not-linked)) instead of leaving the gap to be
discovered by a failing search.

THE LEDGER IS THE FILE A RUN WRITES FIRST, so it is a real, private, writable
file at the path the ledger resolver computes for this profile (GOPI_HOME=this
sandbox → .gopi/memory.db) — never a symlink: a link to the shared read-only
sample would either refuse every write or be silently REPLACED by a writer's
rename, after which the sandbox reads a different file than it pointed at. When
a seed is named ($GOPI_SANDBOX_LEDGER_SEED) it is COPIED, by value: the sample
keeps its 0444 seal and the sandbox keeps a file it may write. The measurements
behind all three shapes, in full: devdoc/sandbox-page-background.md.

WHY .gopi IS THERE: GOPI_HOME is a HOME, not a data root — every reader
(gopi session log, gopi session raw, gopi session stats, the ledger) resolves <GOPI_HOME>/.gopi/...,
so the sandbox materialises the profile at that exact path and nothing has to
be created by hand before a run.

EXECUTION IS PART OF THE SHAPE — a sandbox is a place to RUN something, not just
a path to point at: gopi tool sandbox run [create flags] -- CMD runs CMD with its
working directory set to the sandbox (never to the caller's, and gopi itself
never chdir's), with GOPI_HOME/GOPI_SETTINGS pointing into the sandbox, and with
CMD's exit status propagated. A child launched by absolute path instead inherits
the CALLER's cwd — inside a task run that is the run's worktree, and an orphan
left standing there makes the worktree permanently unreclaimable.

The root is CONFIGURATION, exactly like tmpdir:
  --sandbox-dir PATH  >  $` + config.SandboxEnvName + `  >  settings.json "` + config.SandboxSettingsKey + `"
  default: <home>/workspace/` + config.SandboxDefaultLeaf + `

DO NOT invent a sandbox (SB=$(mktemp -d), a hand-built GOPI_HOME): that path
has no standard shape and no reclaimer — deleted by nobody, understood by
nobody. Sandboxes under the configured root are reclaimed AUTOMATICALLY once
their owner is gone and they have aged out, by ` + CliVerbScratchGC + ` sandboxes
and by the scratch tick. See GOPI.md, the sandbox rule.
(CORRECTED 2026-10-30, task 01a18ef7: this also named "the resident
worker/merger tick", and no resident process reaches that tick any more.)

Flags:
  ` + SandboxLabelFlag + ` NAME   label the sandbox (` + sandbox.SandboxNamePrefix + `NAME-<rand>); the same label from the
                same process reuses its own sandbox, never another run's
  ` + SandboxTaskIDFlag + ` ID   record a task id in the ownership marker
  ` + SandboxDirFlag + ` PATH  the sandbox root for THIS invocation (highest precedence)
  ` + SandboxLedgerSeedFlag + ` PATH the database COPIED (by value) to become this sandbox's ledger —
                the flag face of $` + config.SandboxLedgerSeedEnvName + `; see the recipe below
  ` + SandboxSepToken + `          (` + SandboxRunVerb + ` only, REQUIRED) end the sandbox's own flags; everything
                after it is the child's argv, executed verbatim
  -h|--help   this help

NEED A REAL, POPULATED, WRITABLE LEDGER FOR AN EXPERIMENT? Seed one:

  ` + SandboxLedgerSeedFlag + ` <a real memory.db> ` + SandboxSepToken + ` CMD
  e.g. gopi tool sandbox run ` + SandboxLedgerSeedFlag + ` <home>/` + config.ProfileDirName + `/` + config.MemoryDBFileName + ` ` + SandboxSepToken + ` CMD

Why this flag exists, and why the obvious alternatives are wrong. An experiment
that exercises ledger corruption, SIGPIPE mid-write or a migration needs a ledger
that is REAL (rows in it), POPULATED (so a query returns something) and WRITABLE
(the experiment writes). The two test scaffolds answer NEITHER need:

  testutil.NewTestDBPath   → a fresh database, but EMPTY: a query returns nothing,
                             so "did my experiment lose a row?" has no baseline
  testutil.Sample/
  testutil.SampleDBPath    → REAL and POPULATED, but opened READ-ONLY (mode=ro,
                             file mode 0444): the first write fails

This flag is the third shape and the only one that answers all three: the seed is
COPIED by value into <sandbox>/` + config.ProfileDirName + `/` + config.MemoryDBFileName + `, the copy is a real 0600 file
the experiment may write, the seed itself keeps its own mode and seal, and the
sandbox it lands in carries ` + config.SandboxOwnerFileName + ` and is swept by ` + CliVerbScratchGC + `.

The alternative an agent reaches for instead — cp <a real memory.db> into a
hand-built directory, then export GOPI_HOME at it — is the UNRECLAIMABLE
anti-pattern this verb removes: no ` + config.SandboxOwnerFileName + `, no standard shape, not
inside the sandbox root, matched by NO sweep arm, so nothing ever collects it and
nothing can explain what it was (measured 2026-11-01: six such directories,
645 MiB each, ~3.8 GB, all still on disk). Do not build one; seed a sandbox.

A seed that is missing, is not a SQLite database, or lives INSIDE the sandbox
refuses the create, loudly and by name — the self-referential shape (a sandbox
configured by its own contents) is not a sandbox.

Env:
  ` + config.SandboxEnvName + `            the sandbox root (see above)
  ` + sandbox.GopiSandboxLedgerSeedEnvName + ` the same seed, through the environment (the flag wins; the
                precedence is decided in ONE place). Unset and no flag means the
                ledger starts EMPTY and its own first open initializes it

NEAR MISS: a ` + config.GopiDirEnv + ` whose LEAF is ` + config.ProfileDirName + ` IS REFUSED. This sandbox's profile is
<SB>/` + config.ProfileDirName + `, so handing THAT to ` + config.GopiDirEnv + ` resolves to <SB>/` + config.ProfileDirName + `/` + config.ProfileDirName + ` —
a SECOND, nested profile with its own ledger and ` + config.SessionsDirName + `/ and no
` + config.SandboxOwnerFileName + `, which no reclaimer models (the sweep looks at a sandbox
ROOT's children) while the sandbox's own ledger and ` + config.SessionsDirName + ` stay empty.
The test is the LEAF, not the parent: ` + config.GopiDirEnv + `=<home>/` + config.ProfileDirName + ` for an ordinary home is the
same near miss (measured 2026-10-13: a second ledger holding 2 rows against the real 1376), so
the refusal carries the correction ` + config.GopiDirEnv + `=<parent> instead.
Say ` + config.GopiDirEnv + `=$SB — that value ALREADY IS the profile.
` + config.SandboxNestedProfileOptOutEnv + `=1 turns the refusal into a warning, and says so out loud.

Exit status:
  0   the sandbox exists (created or reused); for ` + SandboxRunVerb + `, the child exited 0
  1   the sandbox could not be materialised — reason on stderr
  2   a usage error (a missing ` + SandboxSepToken + `, an unknown flag, an empty child argv)
  128+N  ` + SandboxRunVerb + ` only: the child's own exit status, propagated verbatim
      (128+N when the child was killed by signal N)
`

// isSandboxSubcommand reports whether argv begins with the sandbox verb.
func isSandboxSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbSandbox {
		runSandboxSubcommand(args[1:])
		return true
	}
	return false
}

// runSandboxSubcommand dispatches the verb's sub-verbs. It returns nothing: the
// dispatch chain expects the verb to handle its own exit.
func runSandboxSubcommand(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, sandboxUsage)
		os.Exit(sandboxExitUsage)
	}
	switch args[0] {
	case "-h", "--help":
		fmt.Print(sandboxUsage)
		os.Exit(0)
	case SandboxShowVerb:
		runSandboxShow(args[1:])
	case SandboxCreateVerb:
		runSandboxCreate(args[1:])
	case SandboxRunVerb:
		runSandboxRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "gopi %s: unknown sub-verb %q (want %s, %s, %s or -h)\n",
			CliVerbSandbox, args[0], SandboxCreateVerb, SandboxRunVerb, SandboxShowVerb)
		os.Exit(sandboxExitUsage)
	}
}

// sandboxExitUsage is the exit status for a usage error, distinct from a failed
// materialisation so a script can tell "you typed it wrong" from "it did not
// work".
const (
	sandboxExitUsage = 2
	sandboxExitError = 1
)

// sandboxResolveRoot resolves the configured root through the ONE resolver. The
// settings read is done HERE (the CLI layer owns it — internal/sandbox must not
// read the settings file behind the caller's back) and its failure is reported
// rather than swallowed: a settings file that cannot be parsed means the
// `sandbox_dir` key may be one we did not see, so continuing with a default
// would place the sandbox somewhere the operator did not configure.
func sandboxResolveRoot() (root, source string) {
	var cfg *config.Config
	if path := config.ResolveSettingsPath(); path != "" {
		if loaded, err := config.FromSettingsFile(path); err == nil {
			cfg = loaded
		} else if config.SandboxRootFromEnv() == "" {
			fmt.Fprintf(os.Stderr, "gopi %s: warning: could not read %s (%v) — the settings key %q is not in force\n",
				CliVerbSandbox, path, err, config.SandboxSettingsKey)
		}
	}
	// The FLAG channel is not read here: it arrives as the nil argument below,
	// and sandboxResolveRootWithFlag is the caller that supplies it. This
	// function is the settings+env+default arm of the ONE resolution.
	return config.ResolveSandboxRoot(cfg, nil)
}

// sandboxResolveRootWithFlag resolves the root for a sub-verb that DID accept
// `--sandbox-dir`. The flag value arrives through the ONE resolver (Rule 2) —
// the verb's parser is the only thing that reads argv, and the precedence
// (flag > env > settings > default) stays spelled in ONE place.
func sandboxResolveRootWithFlag(flagDir string) (root, source string) {
	if strings.TrimSpace(flagDir) == "" {
		return sandboxResolveRoot()
	}
	return config.ResolveSandboxRoot(nil, &config.Config{SandboxDir: flagDir})
}

// runSandboxShow prints the resolved root and the channel that decided it. An
// operator asking "where does a sandbox go?" gets the configuration's answer,
// not this file's opinion.
//
// It accepts the ROOT flag the whole verb advertises (CONFIG.md §1c: the
// `gopi tool sandbox` verb accepts `--sandbox-dir` itself and routes it through
// config.ResolveSandboxRootConfigured) and answers -h with the usage page — the
// two asymmetries this sub-verb used to carry: it refused a flag its own verb's
// page lists, and it swallowed the help flag as a positional so `show -h`
// printed a status line. The repair is the flag TAKING EFFECT, never the help
// being narrowed to match the refusal. The precedence (flag > env > settings >
// default) stays spelled in the ONE resolver (Rule 2) — this function only
// hands it the flag's value.
func runSandboxShow(args []string) {
	opts, dirFlag, rest := sandboxParseFlags(args, SandboxShowVerb)
	// `show` materialises nothing, so it owns no sandbox to LABEL and writes no
	// ownership marker: accepting these would be a flag parsed and ignored, and
	// a caller would believe a sandbox had been named. Refused by name instead
	// (Rule 15), with the accepted vocabulary in the message.
	if opts.Label != "" {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %s is not accepted by %s — it labels a sandbox, and %s materialises none (want %s)\n",
			CliVerbSandbox, SandboxShowVerb, SandboxLabelFlag, SandboxShowVerb, SandboxShowVerb,
			sandboxVerbWants(SandboxShowVerb))
		os.Exit(sandboxExitUsage)
	}
	if opts.TaskID != "" {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %s is not accepted by %s — it records a task id in an ownership marker, and %s materialises none (want %s)\n",
			CliVerbSandbox, SandboxShowVerb, SandboxTaskIDFlag, SandboxShowVerb, SandboxShowVerb,
			sandboxVerbWants(SandboxShowVerb))
		os.Exit(sandboxExitUsage)
	}
	if opts.LedgerSeedPath != "" {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %s is not accepted by %s — it seeds a sandbox's LEDGER, and %s materialises none (want %s)\n",
			CliVerbSandbox, SandboxShowVerb, SandboxLedgerSeedFlag, SandboxShowVerb, SandboxShowVerb,
			sandboxVerbWants(SandboxShowVerb))
		os.Exit(sandboxExitUsage)
	}
	if len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "gopi %s %s: unexpected argument %q (want %s)\n",
			CliVerbSandbox, SandboxShowVerb, rest[0], sandboxVerbWants(SandboxShowVerb))
		os.Exit(sandboxExitUsage)
	}
	root, source := sandboxResolveRootWithFlag(dirFlag)
	fmt.Printf("sandbox root=%s source=%s prefix=%s stale=%s\n",
		root, source, sandbox.SandboxNamePrefix, scratch.SandboxStaleAge)
}

// runSandboxCreate parses the flags and materialises the sandbox through the ONE
// initializer.
func runSandboxCreate(args []string) {
	opts, dirFlag, rest := sandboxParseFlags(args, SandboxCreateVerb)
	if len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "gopi %s %s: unexpected argument %q — %s takes no child argv (use %s %s to run one inside the sandbox)\n",
			CliVerbSandbox, SandboxCreateVerb, rest[0], SandboxCreateVerb, SandboxRunVerb, SandboxSepToken)
		os.Exit(sandboxExitUsage)
	}
	sb := sandboxMaterialise(opts, dirFlag, SandboxCreateVerb)
	reportMaterialised(sb)
	fmt.Println(sb.Path)
}

// runSandboxRun is the ALLOCATION+EXECUTION entry point: it resolves the root
// through the ONE resolver, materialises the sandbox through the ONE initializer,
// and executes the child with its working directory set to that sandbox — without
// gopi ever chdir'ing. The child's exit status is gopi's.
func runSandboxRun(args []string) {
	opts, dirFlag, child := sandboxParseFlags(args, SandboxRunVerb)
	if len(child) == 0 {
		fmt.Fprintf(os.Stderr, "gopi %s %s: nothing to run — expected: gopi %s %s [create flags] %s CMD ARGS...\n",
			CliVerbSandbox, SandboxRunVerb, CliVerbSandbox, SandboxRunVerb, SandboxSepToken)
		os.Exit(sandboxExitUsage)
	}
	sb := sandboxMaterialise(opts, dirFlag, SandboxRunVerb)
	// The path goes to stdout as the LAST line before the child takes over, so
	// the caller can still capture it — and the child's own output follows it.
	reportMaterialised(sb)
	fmt.Println(sb.Path)

	// The child's environment is a sandbox PROFILE by default: GOPI_HOME and
	// GOPI_SETTINGS point into the sandbox, so a gopi-aware child reads the
	// sandbox's profile (its settings copy and its ledger) and writes its
	// sessions/raw/logs there.
	env := sandbox.ProfileEnv(os.Environ(), sb.Path)
	err := sandbox.RunIn(sandbox.RunSpec{Dir: sb.Path, Argv: child, Env: env})
	if err == nil {
		return
	}
	var exitErr *sandbox.ExitError
	if errors.As(err, &exitErr) {
		// The child RAN and failed: its status is the launcher's status — a
		// script wrapping the sandbox must see the child's own code, not a
		// generic failure. Nothing else is printed on this line: the child has
		// already said why in its own output.
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbSandbox, SandboxRunVerb, err)
		os.Exit(exitErr.ExitCode)
	}
	// The child could not be started (or waited on) — there is no status to
	// propagate, and inventing one would be a lie (Rule 15).
	fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbSandbox, SandboxRunVerb, err)
	os.Exit(sandboxExitError)
}

// sandboxVerbWants renders the vocabulary a sub-verb ACCEPTS, for a usage
// error message. It is per-verb on purpose: `show` reads the root and nothing
// else, so a message telling its caller to try `--label` would hand them a
// second refusal. ONE renderer, so the parser's own refusal and the
// post-parse refusals below cannot advertise two different vocabularies.
func sandboxVerbWants(verb string) string {
	if verb == SandboxShowVerb {
		return fmt.Sprintf("%s PATH, %s or %s", SandboxDirFlag, HelpFlag, HelpFlagLong)
	}
	return fmt.Sprintf("%s NAME, %s ID, %s PATH, %s PATH, %s or %s",
		SandboxLabelFlag, SandboxTaskIDFlag, SandboxDirFlag, SandboxLedgerSeedFlag, SandboxSepToken, HelpFlag)
}

// sandboxParseFlags reads the sandbox's OWN flags and returns what follows them.
// Everything after `--` is the child's argv, taken verbatim and never inspected:
// the child's flags are not this verb's business, and a caller that wants to pass
// `--label` to the CHILD must be able to.
func sandboxParseFlags(args []string, verb string) (opts sandbox.Options, dirFlag string, rest []string) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == SandboxSepToken:
			return opts, dirFlag, args[i+1:]
		case args[i] == "-h" || args[i] == "--help":
			fmt.Print(sandboxUsage)
			os.Exit(0)
		case args[i] == SandboxDirFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s %s: %s requires a value\n", CliVerbSandbox, verb, args[i])
				os.Exit(sandboxExitUsage)
			}
			dirFlag = args[i+1]
			i++
		case args[i] == SandboxLabelFlag || args[i] == SandboxTaskIDFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s %s: %s requires a value\n", CliVerbSandbox, verb, args[i])
				os.Exit(sandboxExitUsage)
			}
			if args[i] == SandboxLabelFlag {
				opts.Label = args[i+1]
			} else {
				opts.TaskID = args[i+1]
			}
			i++
		case args[i] == SandboxLedgerSeedFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s %s: %s requires a value\n", CliVerbSandbox, verb, args[i])
				os.Exit(sandboxExitUsage)
			}
			// The flag FEEDS the options; it does not become a second reader of
			// the environment (Rule 2) — ledgerSeedPath() keeps that precedence.
			opts.LedgerSeedPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], SandboxDirFlag+"="):
			dirFlag = strings.TrimPrefix(args[i], SandboxDirFlag+"=")
		case strings.HasPrefix(args[i], SandboxLabelFlag+"="):
			opts.Label = strings.TrimPrefix(args[i], SandboxLabelFlag+"=")
		case strings.HasPrefix(args[i], SandboxTaskIDFlag+"="):
			opts.TaskID = strings.TrimPrefix(args[i], SandboxTaskIDFlag+"=")
		case strings.HasPrefix(args[i], SandboxLedgerSeedFlag+"="):
			opts.LedgerSeedPath = strings.TrimPrefix(args[i], SandboxLedgerSeedFlag+"=")
		default:
			fmt.Fprintf(os.Stderr, "gopi %s %s: unexpected argument %q (want %s)\n",
				CliVerbSandbox, verb, args[i], sandboxVerbWants(verb))
			os.Exit(sandboxExitUsage)
		}
	}
	return opts, dirFlag, nil
}

// sandboxMaterialise is the ONE path from options to a materialised sandbox: the
// configured root through config.ResolveSandboxRoot, then the ONE initializer. A
// failure exits with the error status and the reason on stderr.
func sandboxMaterialise(opts sandbox.Options, dirFlag, verb string) *sandbox.Sandbox {
	root, source := sandboxResolveRootWithFlag(dirFlag)
	sb, err := sandbox.Create(root, source, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbSandbox, verb, err)
		os.Exit(sandboxExitError)
	}
	return sb
}

// reportMaterialised prints the human-readable creation line to STDERR, keeping
// stdout to the path alone so `SB=$(gopi tool sandbox … | tail -1)` stays honest.
func reportMaterialised(sb *sandbox.Sandbox) {
	fmt.Fprintf(os.Stderr, "sandbox %s created=%t reused=%t root=%s source=%s ledger=%s owner=%s%s%s%s\n",
		sb.Path, sb.Created, sb.Reused, sb.Root, sb.RootSource, sb.LedgerPath,
		ownerField(sb.Owner.TaskID), ownerPID(sb.Owner.PID), settingsField(sb), pluginsField(sb))
}

// pluginsField renders the plugin-tree outcome for the report line, so an
// incomplete sandbox is AUDIBLE at creation time: a host with no plugin tree
// produces `plugins=<source> (absent — not linked)`, naming the directory the
// initializer looked for, instead of letting `gopi web search` discover the gap
// much later (Rule 15: an absent source is reported, never silently skipped).
func pluginsField(sb *sandbox.Sandbox) string {
	if sb.PluginsLink == "" {
		return " plugins=" + sb.PluginsSource + "(absent-not-linked)"
	}
	return " plugins=" + sb.PluginsLink
}

// settingsField renders the settings-document outcome, the same way pluginsField
// renders the tree: a host whose settings document does not exist gets a sandbox
// WITHOUT a profile settings entry, and that is said out loud
// (`settings=<source>(absent-not-linked)`) rather than left to be discovered by
// the first reader that falls back to a default. A materialised copy prints
// nothing — the ordinary case stays a short line.
func settingsField(sb *sandbox.Sandbox) string {
	if sb.SettingsDocPath != "" || sb.SettingsDocSource == "" {
		return ""
	}
	return " settings=" + sb.SettingsDocSource + "(absent-not-linked)"
}

// ownerField / ownerPID render the marker's identifying fields for the log
// line; both return "" for an empty value so the line stays parseable.
func ownerField(taskID string) string {
	if taskID == "" {
		return ""
	}
	return "task=" + taskID + " "
}

func ownerPID(pid int) string {
	if pid <= 0 {
		return ""
	}
	return "pid=" + fmt.Sprint(pid)
}
