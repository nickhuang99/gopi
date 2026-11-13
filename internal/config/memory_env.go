package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Ledger (knowledge base / task ledger) env names + file name — the SINGLE
// definition of the values that point gopi at a memory.db other than the
// profile default (<gopi dir>/memory.db).
//
// WHY THEY LIVE HERE (Rule 14 — shared values live in a small exported-constant
// package): these values are consumed by packages that cannot see each other's
// unexported constants — internal/tools resolves the ledger through them,
// internal/agent and cmd/agent sandbox their suites with them, and
// internal/testutil pins them for every suite. Before this file the env names
// were mirrored in each (measured 2026-09-15: internal/tools' unexported
// `memoryDBEnv` plus a re-stated copy in internal/agent/loop_test.go and
// cmd/agent/main_test.go — exactly the drift Rules 13/14 forbid), and the file
// name was private to internal/tools, so a suite pin could not name it.
// internal/config is the established home of the sibling names (GopiDirEnv,
// SettingsEnv) and is already imported by every consumer, so this adds no edge
// to the import graph.
const (
	// MemoryDBEnv overrides the ledger path — the shared knowledge base and
	// task ledger (~/.gopi/memory.db) or a per-profile / per-project one.
	//
	// A TEST that opens a ledger must name its own sandbox through this
	// variable: leaving it ambient is what wrote a fixture row into the
	// operator's real ledger (row t-stale-sibling, which then had the merger
	// reporting a phantom /tmp repo every pass — devdoc/reclaim-fossil-20260915.md).
	// The suite entry point now pins it (testutil.PinSuiteLedgerEnv).
	MemoryDBEnv = "GOPI_MEMORY_DB"

	// MemoryDBEnvLegacy is the pre-embedding spelling of the same override (it
	// used to be passed through to the hippo CLI as --db). Still honoured as a
	// fallback so existing invocations keep working — and still able to smuggle
	// the real ledger into a sandboxed suite, so a suite pin masks it too.
	MemoryDBEnvLegacy = "GOPI_HIPPO_DB"

	// MemoryDBFileName is the ledger file under the gopi data root: every gopi
	// instance accumulates distilled lessons and task rows into the same file,
	// so a later task can find what an earlier one learned (跨任务累积).
	MemoryDBFileName = "memory.db"

	// LedgerSettingsKey is the settings.json key of the ledger override
	// ("memory_db": "<path>"). It is a constant for the same reason the env
	// names are: the catalog row, the resolution below, the reporting texts and
	// the tests must name ONE spelling of the key they read (Rule 1b/13).
	LedgerSettingsKey = "memory_db"

	// LedgerFlagName is the CLI spelling of the same override (`--db PATH`),
	// bare (dashless) — the catalog's CLIFlag column convention.
	LedgerFlagName = "db"

	// LedgerSourceFlag / LedgerSourceDefault name the two channels of the
	// resolution whose label is not simply an env-var or settings-key NAME.
	// They are exported because `gopi config show` prints the channel that
	// decided the ledger: a bare literal there would be a second spelling of
	// the same fact (Rule 1b).
	LedgerSourceFlag    = "--db"
	LedgerSourceDefault = "built-in default"

	// LedgerNoFlag is the "this caller parsed no --db" value of ResolveLedger's
	// flag argument. It is EXPORTED and named (Rule 1b/14) because several
	// surfaces resolve the ledger without owning a flag — a bare "" at those call
	// sites reads like an oversight rather than like the no-flag channel, and a
	// future reader cannot tell the two apart.
	LedgerNoFlag = ""
)

// LedgerPath resolves the ledger (memory.db) this process would open, through
// the SAME ranked chain every other gopi knob uses (Rule 2 — one resolution,
// one reader per channel):
//
//	rank 1  CLI flag  --db PATH
//	rank 2  env       GOPI_MEMORY_DB, then its legacy alias GOPI_HIPPO_DB
//	rank 3  settings  settings.json "memory_db": "PATH"
//	rank 4  default   <gopi dir>/memory.db
//
// WHAT WAS WRONG (the defect this closes). Ranks 3 was documented in the
// catalog (settings.go's EnvVars row for GOPI_MEMORY_DB, CONFIG.md §2c) and the
// `--db`/`GOPI_MEMORY_DB` pair worked, but nothing read the settings KEY: an
// operator who declared "memory_db": "/srv/ledger.db" got the built-in default
// silently, on every surface that resolves a ledger (`gopi memory search`,
// `gopi config show`, `gopi config setup`). That is Rule 15's defect exactly — "the
// operator asked for something I did not understand" and "nobody asked" must
// not look alike, and here they looked alike for a documented channel.
//
// WHY IT LIVES IN internal/config (Rule 14): the resolver must serve
// internal/tools' memoryDBPath, cmd/agent's `config show` report and its
// identity section from ONE definition; before this the tools package resolved
// its own chain privately and cmd/agent re-derived the env half, which is how
// the two halves of one `config show` could name two different ledgers.
//
// An unresolvable home is reported, never guessed (Rule 15): the caller must
// treat the error as "not proven sandboxed", not as "no ledger".
func LedgerPath() string {
	path, _ := ResolveLedger(LedgerNoFlag)
	return path
}

// ResolveLedger is the ranked resolution above, reporting WHICH channel decided
// it. The source string is the channel's own name — the env variable, the
// settings KEY, LedgerSourceFlag (`--db`), or LedgerSourceDefault — so a
// reporter prints the fact it resolved rather than re-deriving the ranking
// (which is what let `config show` announce "built-in default" over a declared
// path).
//
// flagPath is the caller's parsed `--db` value, or "" when the caller has no
// flag of its own to pass (the ordinary case: most callers are not the CLI).
// It is passed IN rather than read from a global because the flag is parsed by
// a different layer (internal/config.FlagOverrides for the agent path,
// cmd/agent's own memoryFlagSet for `gopi memory …`) and both hand it to the
// same resolution — the flag is not a second reader of the file, it is rank 1
// stated by its owner (Rule 2).
func ResolveLedger(flagPath string) (path, source string) {
	if v := strings.TrimSpace(flagPath); v != "" {
		return v, LedgerSourceFlag
	}
	if v := strings.TrimSpace(os.Getenv(MemoryDBEnv)); v != "" {
		return v, MemoryDBEnv
	}
	if v := strings.TrimSpace(os.Getenv(MemoryDBEnvLegacy)); v != "" {
		return v, MemoryDBEnvLegacy
	}
	if v := ledgerPathFromSettings(); v != "" {
		return v, LedgerSettingsKey
	}
	gd, err := GopiDir()
	if err != nil || gd == "" {
		return "", LedgerSourceDefault
	}
	return filepath.Join(gd, MemoryDBFileName), LedgerSourceDefault
}

// ledgerPathFromSettings reads rank 3 — the settings.json key. It is a
// SEPARATE function only so the "no settings file" case is one decision rather
// than an inline nil-dance: an absent or unreadable file is the DEFAULT
// channel's business (rank 4 decides), and a broken file has already been
// reported by the settings loader the process ran earlier (the agent path exits
// on it; a report-only surface says so itself), so this must not print a second
// time.
func ledgerPathFromSettings() string {
	path := ResolveSettingsPath()
	if path == "" {
		return ""
	}
	cfg, err := FromSettingsFile(path)
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.MemoryDB)
}
