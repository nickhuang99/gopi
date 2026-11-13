package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/scratch"
)

// The agent SANDBOX knob and the shape that lives inside it.
//
// THE PROBLEM THIS FILE EXISTS FOR (measured 2026-09-22, census in
// devdoc/agent-sandbox-unify.md): an agent that needs an isolated gopi profile
// under which to run something (a worker round, an acceptance script, a
// subprocess test) INVENTED the sandbox on the spot — `SB=$(mktemp -d)` /
// `SB=~/workspace/sbx-$$` / a per-script `$SB` variable — exported GOPI_HOME
// into it and built the contents ad hoc. Every one of those is a different
// shape, none is reclaimed, and the invented path is a second answer to a
// question that already has one.
//
// So the sandbox is an ORDINARY KNOB, exactly like `tmpdir`: one catalog entry
// (env name, settings key, CLI flag), one resolver, and a built-in default. The
// initializer (internal/sandbox) materialises ONE standard shape inside the
// resolved root; the reclaimer (internal/scratch + `gopi tool scratch-gc sandboxes`)
// bounds it on the EXISTING tick.
//
// WHY THE NAME IS "sandbox" AND NOT ":memory:"-style FICTION: the sandbox is a
// real, private GOPI_HOME-shaped directory. What makes it safe is not that it is
// unusual but that it is STANDARD — the same four entries, always — so its
// contents can be reasoned about without reading the script that made it.

// SandboxEnvName is the knob's environment channel — gopi's own spelling, the
// counterpart of GOPI_TMPDIR (Rule 14: one definition; the catalog row, the
// resolver and the docs all read THIS name).
const SandboxEnvName = "GOPI_SANDBOX_DIR"

// SandboxSettingsKey is the settings.json key carrying the sandbox root
// ("sandbox_dir": "/home/me/workspace/gopi-sandbox").
const SandboxSettingsKey = "sandbox_dir"

// SandboxFlagName is the CLI spelling of the same knob, named here so the
// catalog row, the parser's case and the help text cannot drift (Rule 1b).
const SandboxFlagName = "sandbox-dir"

// SandboxLedgerSeedEnvName is the SECOND sandbox knob's environment channel:
// the path of a database COPIED to become a new sandbox's ledger. It has no
// settings key and no CLI flag (the seed is a per-invocation input the test
// scaffold hands `gopi tool sandbox create`), so the catalog row renders an em dash
// in its flag column.
//
// WHY THE DEFINITION LIVES HERE AND NOT IN internal/sandbox (Rule 14). The
// seed's only READER is internal/sandbox (ledgerSeedPath →
// os.Getenv(config.SandboxLedgerSeedEnvName)), and internal/sandbox already
// imports internal/config for ProfileDirName / MemoryDBFileName — so a
// definition on THAT side is one config could never import back (a cycle). The
// catalogue row below is the second consumer, and the catalogue lives here;
// config is therefore the one package both sides can reach, and internal/sandbox
// spells its exported GopiSandboxLedgerSeedEnvName as an ALIAS of this name.
// The alternative — a bare "GOPI_SANDBOX_LEDGER_SEED" typed into settings.go —
// is Rule 1b's string-literal defect, and it is how the two spellings drift
// apart.
const SandboxLedgerSeedEnvName = "GOPI_SANDBOX_LEDGER_SEED"

// SandboxDefaultLeaf is the LEAF name of the built-in default sandbox root:
// <home>/workspace/gopi-sandbox — an ordinary directory under the workspace,
// the same choice DefaultTmpDir makes and for the same reason (Rule 22's
// "prefer the explicit layer": the workspace is the operator's declared working
// space, and a default must be the obvious user-facing one).
//
// NOT under the swept tmpdir (gopi-test-scratch): the scratch home is swept by
// age with no notion of ownership, so a live sandbox placed there could be
// collected out from under its owner. The sandbox tree carries its own
// ownership marker precisely so it can live in the ordinary workspace, and its
// reclaimer (scratch.SweepSandboxes) reads that marker before it removes
// anything.
//
// An ALIAS of internal/scratch's definition (Rule 14): the leaf is used on both
// sides of the config/scratch boundary, and a mirrored copy is the drift this
// repository counts as a defect.
const SandboxDefaultLeaf = scratch.SandboxDefaultLeaf

// SandboxMarkerPrefix is the collection marker in a sandbox DIRECTORY NAME:
// `sandbox-<what>-<rand>`. internal/scratch's reclaimer removes a direct child
// of the sandbox root only when its name carries THIS prefix, so the root may
// hold anything else the operator put there — an unrelated directory is never a
// candidate (the same arm-scoped discipline as §1C of gc.go's Collectable).
//
// It is an ALIAS of the one definition in internal/scratch (Rule 14): the
// sweeper matches the prefix and this package names it, and internal/scratch
// cannot import internal/config (config imports scratch — a reverse import is a
// cycle), so the definition lives there and both consumers reference it.
const SandboxMarkerPrefix = scratch.SandboxMarkerPrefix

// The standard shape's entry names — the ONE list (Rule 14). internal/sandbox
// materialises exactly these, the reclaimer documents exactly these, and
// PROJECT.md's rule names exactly these. A deviation is a contract change and must
// move this comment with it.
const (
	// SandboxSettingsFileName is the profile's settings DOCUMENT: a read-only
	// COPY (0444) of the real settings file, materialised by settings_doc.go.
	// It is a copy, NOT a symlink — the pre-2026-09-27 shape linked it "read-only
	// by contract", and that contract was unenforceable (the bits of a symlink
	// are the TARGET's) while a shell redirect follows the link: a fixture
	// `printf '{…}' > "$SB/.gopi/settings.json"` truncated the operator's LIVE
	// settings document. The copy answers the same reads and refuses the write;
	// a sandbox run that needs its own knob declares it in its OWN settings file
	// (GOPI_SETTINGS), never as a write into the profile.
	//
	// The name is `settings.json` rather than `.flash_settings.json` on purpose:
	// resolution must go through the SAME channel production uses
	// (GOPI_SETTINGS → the resolved settings path), so the copy is installed at
	// whatever basename the resolved settings path carries and the sandbox names
	// no basename at all.
	SandboxSettingsFileName = "settings.json"

	// SandboxSessionsDirName / SandboxRawDirName / SandboxLogsDirName are the
	// sandbox-PRIVATE, empty, writable directories INSIDE the profile root:
	// this run's session records, raw HTTP logs and tool/job logs
	// (<sandbox>/.gopi/sessions|raw|logs — the same layout every gopi profile
	// has, which is why they are materialised where GopiDir resolves them
	// rather than one directory above; the join itself is config.ProfilePathIn,
	// and the profile leaf is ProfileDirName). The directories are private
	// rather than linked to the operator's because they are WRITE targets —
	// pointing them at the real profile would be the leak the sandbox exists to
	// prevent.
	SandboxSessionsDirName = SessionsDirName // the real profile's own directory name, one definition (Rule 14)
	SandboxRawDirName      = "raw"
	SandboxLogsDirName     = "logs"
)

// SandboxOwnerFileName is the ownership marker INSIDE each sandbox: task id,
// pid, process start ticks and created-at, written by the initializer.
//
// It is the reason a sandbox can be reclaimed WITHOUT remembering anything: the
// marker travels with the directory, so a reclaimer that finds the directory
// knows who made it and which process incarnation owned it. `git`-style
// liveness (a pid plus its kernel start ticks) is what makes the verdict safe —
// a recycled pid cannot pass as the original owner (memory #2633).
//
// An ALIAS of internal/scratch's definition (Rule 14), for the same cycle
// reason as SandboxMarkerPrefix — and the ONLY thing that may identify a
// directory as a sandbox (gopihome.go's near-miss check): asking for the
// marker the initializer wrote beats guessing from a path shape (Rule 34).
const SandboxOwnerFileName = scratch.SandboxOwnerFileName

// SandboxRootSources are the labels that say WHICH channel supplied the
// effective sandbox root. Constants (Rule 1b): the operator-facing report and
// the tests both match on them, and a nickname typed twice drifts.
const (
	SandboxRootSourceFlag     = "from --" + SandboxFlagName
	SandboxRootSourceEnv      = "from " + SandboxEnvName
	SandboxRootSourceSettings = "from the settings key " + SandboxSettingsKey
	SandboxRootSourceDefault  = "from the built-in default"
)

// SandboxRootFromEnv is this knob's ONE environment reader (Rule 2).
func SandboxRootFromEnv() string {
	return strings.TrimSpace(os.Getenv(SandboxEnvName))
}

// SandboxLedgerSeedValue is the LEDGER SEED knob's ONE environment reader
// (Rule 2) — and it lives here, in the package that defines the name, rather
// than beside its caller in internal/sandbox.
//
// WHY THE READER MOVED (Rule 21, gate 6's verdict). The first cut of the
// catalog row left the read in internal/sandbox while the DECLARATION lived
// here: `internal/standards`'s env-reader gate then reported exactly what that
// gate exists for — "env constant SandboxLedgerSeedEnvName has no environment
// reader — a hook with no consumer reads as a working control surface". The
// gate is right and the exemption is not the remedy: a name declared in one
// package and read in another, through an aliasing chain, is a hook whose
// consumer the tree can only see indirectly. Declaring AND reading it here
// makes the definition self-contained, and internal/sandbox consumes this
// function exactly as it consumes config.MemoryDBFileName — one name, one
// reader, no alias to follow.
func SandboxLedgerSeedValue() string {
	return strings.TrimSpace(os.Getenv(SandboxLedgerSeedEnvName))
}

// SandboxRootDefault is the built-in root — <home>/workspace/gopi-sandbox. It
// is a function rather than a constant because the home is itself resolved
// (GOPI_HOME / the OS user record), and it returns a CANONICAL spelling so a
// workspace reached through a symlink does not make a child report a different
// path than its parent resolved.
func SandboxRootDefault() string {
	return filepath.Join(scratch.WorkspaceUnderHome(), SandboxDefaultLeaf)
}

// ResolveSandboxRootConfigured applies the knob's precedence and returns the
// value a configuration channel produced, plus the label of the winning
// channel. An empty value means NO channel configured one — the caller then
// uses SandboxRootDefault.
//
// Precedence, highest first (the project's standing CLI > env > settings rule,
// the same ranking ResolveTmpDirConfigured applies — one policy, not two):
//
//  1. --sandbox-dir — typed for THIS invocation.
//  2. GOPI_SANDBOX_DIR — the deployed configuration.
//  3. the settings `sandbox_dir` key — the declarative layer, weakest because
//     it is the one nobody has to restate per invocation.
//
// There is deliberately NO ambient override (no sandbox counterpart of the
// caller-exported TMPDIR): TMPDIR is ambient because children read it, while
// nothing outside gopi reads a sandbox root — so a second, ambient channel
// would be a knob with no reader.
func ResolveSandboxRootConfigured(settingsCfg, flagCfg *Config) (value, source string) {
	if flagCfg != nil && flagCfg.SandboxDir != "" {
		return flagCfg.SandboxDir, SandboxRootSourceFlag
	}
	if v := SandboxRootFromEnv(); v != "" {
		return v, SandboxRootSourceEnv
	}
	if settingsCfg != nil && settingsCfg.SandboxDir != "" {
		return settingsCfg.SandboxDir, SandboxRootSourceSettings
	}
	return "", ""
}

// ResolveSandboxRoot is THE resolver callers use: the configured channels, or
// the built-in default when none is set. It returns the root AND the label of
// the channel that decided it, so every surface that reports the sandbox (the
// initializer's output, `gopi tool sandbox path`, the reclaimer's log line) names
// where the value came from instead of presenting a default as a discovery.
//
// One function, so the CLI, the initializer and the reclaimer can never
// disagree about where the sandbox lives (Rule 2/14).
func ResolveSandboxRoot(settingsCfg, flagCfg *Config) (root, source string) {
	if v, src := ResolveSandboxRootConfigured(settingsCfg, flagCfg); v != "" {
		return v, src
	}
	return SandboxRootDefault(), SandboxRootSourceDefault
}
