package config

import (
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// The temp-workspace knob's resolution (tmpdir epic, parameterised 2026-09-16).
//
// THE KNOB IS ORDINARY: it has ONE entry in the EnvVars catalog (env name,
// settings key, CLI flag) so `--tmpdir`, `GOPI_TMPDIR` and the settings `tmpdir`
// key are one declaration rather than three bespoke reads, and its value is
// resolved here through a SINGLE function that applies the project's standing
// precedence — CLI > env > settings > default (settings only FILL an unset
// variable; a flag always wins). The channels are ranked in
// ResolveTmpDirConfigured's doc comment, in one place.
//
// WHY THE RESOLUTION IS NOT A LINE INSIDE ApplyEnv'S ENV-WRITING LOOP: ApplyEnv
// pushes cfg fields INTO the process environment so the existing per-file
// readers see them. This knob's OUTPUT is exactly that kind of variable (TMPDIR,
// which every child inherits), but its INPUT is a DIFFERENT variable
// (GOPI_TMPDIR, plus the ambient TMPDIR a caller may have exported) — one name
// cannot be both the input and the output of a fill-if-unset loop without the
// caller's value reading back as gopi's own. So the resolution is a pure
// function of the channels, and the publisher (cmd/agent) writes the resulting
// value. The PRECEDENCE RULE is ApplyEnv's — spelled once, here — not a second,
// divergent policy.
//
// TmpDirFromEnv is this knob's ONE environment reader (Rule 2).
func TmpDirFromEnv() string {
	return strings.TrimSpace(os.Getenv(TmpDirEnvName))
}

// TmpDirFromCallerEnv is the ambient override's ONE reader (Rule 2): the TMPDIR
// a CALLER exported before starting gopi. It is deliberately a different
// variable from GOPI_TMPDIR — gopi WRITES TMPDIR for its children, so a knob
// named TMPDIR would be both input and output.
func TmpDirFromCallerEnv() string {
	return strings.TrimSpace(os.Getenv(scratch.TmpDirEnvName))
}

// The labels that say WHICH channel supplied the effective temp workspace. They
// are constants (Rule 1b) because the operator-facing report and the tests both
// match on them, and a nickname typed twice drifts.
const (
	TmpDirSourceFlag     = "from --" + TmpDirFlagName
	TmpDirSourceEnv      = "from " + TmpDirEnvName
	TmpDirSourceAmbient  = "from the caller's " + scratch.TmpDirEnvName
	TmpDirSourceSettings = "from the settings key " + scratch.TmpDirSettingsKey
)

// ResolveTmpDirConfigured applies the knob's precedence and returns the value a
// configuration channel produced, plus the label of the channel that won. An
// empty value means NO channel configured one — the caller then publishes the
// built-in default (scratch.DefaultTmpDir).
//
// Precedence, highest first:
//
//  1. --tmpdir — the operator typed it for THIS invocation, so it wins over
//     every environment channel. (This is the same CLI > env order ApplyEnv
//     enforces for every other knob; the flag is also hoisted into
//     GOPI_TMPDIR before the subcommand chain runs, so a verb that never reaches
//     the agent path sees it too.)
//  2. a caller-exported TMPDIR — the AMBIENT override. It outranks the settings
//     file AND GOPI_TMPDIR because it is the variable a child actually reads:
//     whoever started gopi sees TMPDIR in their own shell and expects it to be
//     in force, and silently publishing something else would contradict what
//     they observe. (Nested gopi converges for the same reason: the variable a
//     parent published resolves to itself in the child.)
//  3. GOPI_TMPDIR — gopi's own spelling of the knob (the catalog entry's env
//     channel): still a deliberate configuration, but less specific than the
//     variable that decides the effect.
//  4. the settings `tmpdir` key — the declarative layer and the weakest of the
//     four, because it is the one nobody has to restate per invocation (a file
//     on disk must not silently redirect a harness or a developer who pinned the
//     variable for one run).
func ResolveTmpDirConfigured(settingsCfg, flagCfg *Config) (value, source string) {
	if flagCfg != nil && flagCfg.TmpDir != "" {
		return flagCfg.TmpDir, TmpDirSourceFlag
	}
	if v := TmpDirFromCallerEnv(); v != "" {
		return v, TmpDirSourceAmbient
	}
	if v := TmpDirFromEnv(); v != "" {
		return v, TmpDirSourceEnv
	}
	if settingsCfg != nil && settingsCfg.TmpDir != "" {
		return settingsCfg.TmpDir, TmpDirSourceSettings
	}
	return "", ""
}
