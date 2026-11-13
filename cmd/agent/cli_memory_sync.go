package main

// gopi memory sync — the MANUAL cross-node knowledge-sync entry point.
//
//	gopi memory sync --peer zbook            one bidirectional session
//	gopi memory sync --peer hp --push        one-way OUTWARD
//	gopi memory sync --peer hp --pull        one-way INWARD
//	gopi memory sync --peer hp --dry-run     size the outbound patch, no ssh
//	gopi memory sync --serve-assets          the PEER side (ssh'd by an
//	                                         initiator): stdin = its export,
//	                                         stdout = this node's reply.
//
// WHAT CAME BACK, AND WHY IT IS ONLY THIS. The verb and its `--peer` /
// `--push` / `--pull` directions were deleted with the whole cross-node sync
// CLI family (5677cc6e, 2026-11-05), which took the TASK-sync family with it.
// The tools layer underneath was never removed — SyncExport, WriteSyncPatch,
// SyncImport, MemoryAssetsPeerSessionMode and the memory_peers whitelist all
// stayed — so restoring the manual door is a matter of wiring argv to them
// (Rule 2: nothing here re-implements a leg).
//
// WHAT DID NOT COME BACK, and must not be re-added by a later reader:
//   - the `--every <dur>` periodic loop (it dispatched a resident round per
//     cadence on an operator's terminal; the verb is MANUAL now — one
//     invocation, one session, exit);
//   - `memory_sync_on_store` (a store must never spawn a peer session);
//   - the resident worker/merger tick that drove a round (it held a worker
//     round for 315-534s, devdoc/memory-sync-tick-retired.md).
// Those three were separate decisions with their own evidence, and every one
// of them is still retired. See devdoc/memory-sync-manual-door-restored.md.

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// memorySyncUsage is the help page for `gopi memory sync` (Rule: every
// subcommand documents itself, and the page and the parser are the same
// decision — the flags below are the flags runMemorySync admits).
const memorySyncUsage = `GoPi memory sync — MANUAL cross-node knowledge sync (unique-only).

Moves the distilled knowledge rows (the memory store/search assets) between
THIS node and one declared peer, so both sides end up with the superset.
Identity is the exact QUESTION: a row one side lacks is copied over, a row
present on BOTH sides is never touched (the divergence zone is deliberately
out of scope — no newer-wins, no merge). Insert-only and idempotent: a
re-run after convergence imports ~0.

Usage:
  gopi memory sync --peer <alias> [--db PATH] [--dry-run]
                   [--push | --pull] [--timeout SECONDS]
  gopi memory sync --serve-assets [--db PATH]

  --peer ALIAS  the peer (an ssh alias) to run ONE session with. It must be
                declared in settings.json "memory_peers" / GOPI_MEMORY_PEERS
                — a key decoupled from the task ledger's "sync_peers", so
                single-node task mode keeps knowledge sync available. An
                undeclared peer is REFUSED, and the refusal lists the peers
                this node declares.
  --push        one-way OUTWARD: send our unique rows, apply nothing back.
  --pull        one-way INWARD: apply the peer's rows here and send it
                nothing (the outbound patch is EMPTY, so the peer's ledger is
                not written by this run).
  (neither)     BIDIRECTIONAL: push our unique rows and apply the peer's
                reply — one invocation converges both stores.
  --push and --pull are mutually exclusive; naming both is refused rather
  than silently resolved to one of them.
  --dry-run     report the size of the outbound patch and stop (no ssh; the
                whitelist check still runs).
  --timeout N   absolute bound on the ssh leg, in SECONDS. Default 900.
                Also settable as ` + tools.MemorySyncTimeoutEnv + ` (the flag wins).
  --db PATH     knowledge base override (default: ~/.gopi/memory.db).

  --serve-assets is the PEER side of the leg — an initiator ssh's it and
  speaks the protocol over stdin/stdout. Never run by a human; it prints
  nothing on stdout but this node's reply export.

Exit status:
  0  the session succeeded (including full convergence: 0 imported)
  1  any failure — an undeclared or unreachable peer, a refused row, a
     failed import. The error names its own cause; it is never silent.

This verb is MANUAL BY DESIGN: no timer, no daemon, no loop, and no store
triggers it. One invocation runs one session and exits.`

// memorySyncProg is this verb's spelling, used by every message the file
// prints (Rule 1b: one definition, referenced at every site).
const memorySyncProg = "gopi memory " + tools.MemorySyncVerb

// The flag KEYS this verb admits (the flag parser keys on the name WITHOUT
// its dashes) — Rule 1: a value compared/stored at more than one site is a
// named constant, never a literal typed at each site.
const (
	memorySyncPeerFlagKey       = "peer"
	memorySyncDBFlagKey         = "db"
	memorySyncDryRunFlagKey     = "dry-run"
	memorySyncTimeoutFlagKey    = "timeout"
	memorySyncServeAssetsFlagKe = tools.MemorySyncServeAssetsFlag
	// The two DIRECTION keys are the tools layer's own mode spellings
	// (Rule 14: the flag word, the whitelist-independent mode value and the
	// usage text are one decision — AssetSyncModePush is what `--push`
	// selects, so the flag cannot drift from the mode it names).
	memorySyncPushFlagKey = tools.AssetSyncModePush
	memorySyncPullFlagKey = tools.AssetSyncModePull
)

// The DASHED spellings, derived from the keys (Rule 13: the two halves of one
// capability cannot drift apart) — used where a MESSAGE names a flag.
const (
	memorySyncPushFlagWord = argvFlagPrefix + memorySyncPushFlagKey
	memorySyncPullFlagWord = argvFlagPrefix + memorySyncPullFlagKey
	memorySyncPeerFlagWord = argvFlagPrefix + memorySyncPeerFlagKey
)

// runMemorySync parses this verb's flags and runs the session (or the peer's
// serve handler). It owns its own -h/--help so the group page is never
// printed in place of this one.
func runMemorySync(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(memorySyncUsage)
			os.Exit(0)
		}
	}
	fs := newMemoryFlagSet()
	// STRICT admission: the flags the page documents are exactly the flags
	// this verb accepts, so `--pus` is refused instead of silently running a
	// BIDIRECTIONAL session — this verb's whole point is that the operator
	// picks a direction.
	for _, name := range []string{
		memorySyncPeerFlagKey, memorySyncDBFlagKey, memorySyncDryRunFlagKey,
		memorySyncPushFlagKey, memorySyncPullFlagKey, memorySyncTimeoutFlagKey,
		memorySyncServeAssetsFlagKe,
	} {
		fs.expect(name)
	}
	for _, name := range []string{
		memorySyncDryRunFlagKey, memorySyncPushFlagKey,
		memorySyncPullFlagKey, memorySyncServeAssetsFlagKe,
	} {
		fs.boolean(name)
	}
	positional, err := fs.parse(args)
	if err != nil {
		memorySyncDie("%v", err)
	}
	if len(positional) > 0 {
		memorySyncDie("unexpected argument %q", positional[0])
	}
	dbPath := fs.get(memorySyncDBFlagKey)
	if err := tools.SetMemoryDBPath(dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: %v\n", memorySyncProg, memorySyncDBFlagKey, err)
		os.Exit(1)
	}
	if fs.has(memorySyncServeAssetsFlagKe) {
		runMemoryAssetsServe(dbPath)
		return
	}
	peer := strings.TrimSpace(fs.get(memorySyncPeerFlagKey))
	if peer == "" {
		memorySyncDie("--%s <alias> is required (or --%s for the peer side)",
			memorySyncPeerFlagKey, memorySyncServeAssetsFlagKe)
	}
	// The whitelist gate, BEFORE any leg exists, and with the LEGAL VALUES
	// spelled out: the tools layer refuses an undeclared peer on its own (it
	// is the enforcement point, `RequireDeclaredAssetPeer`), but a refusal
	// that names what is wrong and not what would have worked sends the
	// operator to the settings file blind (Rule 15).
	if err := memorySyncRequireDeclaredPeer(peer); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", memorySyncProg, err)
		os.Exit(1)
	}
	// EVERY flag is VALIDATED before any branch runs, and that order is
	// deliberate: `--dry-run` returns early, so a validation placed after it
	// silently SKIPS the flags a dry run does not itself use. Measured
	// 2026-11-06 on this very branch: `--dry-run --timeout 0` exited 0 and
	// printed a normal report, while the same value without `--dry-run` exited
	// 1 — one flag set, two verdicts, and the lenient one is the run that looks
	// like a rehearsal. (Rule 15: a malformed value is an error, and a
	// rehearsal that hides a refusal is worse than no rehearsal.)
	mode, err := memorySyncDirection(fs)
	if err != nil {
		memorySyncDie("%v", err)
	}
	if err := applyMemorySyncTimeout(fs); err != nil {
		memorySyncDie("%v", err)
	}
	if fs.has(memorySyncDryRunFlagKey) {
		runMemoryAssetsDryRun(peer, dbPath)
		return
	}
	stats, err := tools.MemoryAssetsPeerSessionMode(peer, nil, dbPath, mode)
	// A mid-corpus import failure returns BOTH a partial tally and the error:
	// report what landed, then fail loudly (Rule 15 — an operator must be able
	// to tell a 0-row failure from a 99%-applied one).
	if stats != nil {
		printMemoryAssetSession(stats)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", memorySyncProg, err)
		os.Exit(1)
	}
}

// memorySyncDie reports a usage error with the page and exits 1 (the shape
// every refusal here uses: the cause, then the flags that would have worked).
func memorySyncDie(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s\n\n%s", memorySyncProg, fmt.Sprintf(format, args...), memorySyncUsage)
	os.Exit(1)
}

// memorySyncRequireDeclaredPeer is the CLI's own whitelist gate: the phrase
// the tools layer refuses with is authoritative, and this one adds the half a
// refusal needs to be actionable — the peers this node DOES declare. A
// resolution ERROR is carried through verbatim (an unreadable topology must
// never be rendered as "no peers declared": that is a different fact).
func memorySyncRequireDeclaredPeer(peer string) error {
	peers, err := config.ResolveMemoryPeers()
	if err != nil {
		return fmt.Errorf("reading %s: %w", config.MemoryPeersSettingsKey, err)
	}
	for _, p := range peers {
		if p == peer {
			return nil
		}
	}
	if len(peers) == 0 {
		return fmt.Errorf("peer %q is not declared, and this node declares NO memory peers at all — set settings.json %q (or %s) to the ssh alias(es) this node may sync knowledge with",
			peer, config.MemoryPeersSettingsKey, config.MemoryPeersEnv)
	}
	return fmt.Errorf("peer %q is not declared — %q is this node's whole whitelist (%q, or %s for one run); topology is declared, never discovered",
		peer, strings.Join(peers, "|"), config.MemoryPeersSettingsKey, config.MemoryPeersEnv)
}

// memorySyncDirection resolves --push / --pull into the asset channel's
// direction mode. Declaring BOTH is refused (Rule 15 — an ambiguous request
// is an error, not a silent preference for one of them).
func memorySyncDirection(fs *memoryFlagSet) (string, error) {
	push, pull := fs.has(memorySyncPushFlagKey), fs.has(memorySyncPullFlagKey)
	if push && pull {
		return "", fmt.Errorf("%s and %s are mutually exclusive (omit both for one bidirectional session)",
			memorySyncPushFlagWord, memorySyncPullFlagWord)
	}
	if push {
		return tools.AssetSyncModePush, nil
	}
	if pull {
		return tools.AssetSyncModePull, nil
	}
	return tools.AssetSyncModeBoth, nil
}

// applyMemorySyncTimeout exports --timeout (seconds) into the ONE channel the
// tools layer reads (Rule 2: the CLI and the env knob share a single reader,
// so the tools layer keeps its existing signature). Absent flag → nothing is
// exported, so an operator's own exported value still decides.
func applyMemorySyncTimeout(fs *memoryFlagSet) error {
	if !fs.has(memorySyncTimeoutFlagKey) {
		return nil
	}
	raw := strings.TrimSpace(fs.get(memorySyncTimeoutFlagKey))
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return fmt.Errorf("--%s %q is not a positive number of seconds", memorySyncTimeoutFlagKey, raw)
	}
	if err := os.Setenv(config.MemorySyncTimeoutEnv, strconv.Itoa(secs)); err != nil {
		return fmt.Errorf("--%s: %w", memorySyncTimeoutFlagKey, err)
	}
	return nil
}

// runMemoryAssetsServe is the peer-side handler (ssh'd by an initiator):
// stdin = the initiator's export; stdout = our reply export. Nothing else on
// stdout (stdin/stdout IS the protocol); a failure is reported on stderr and
// exits non-zero, so the initiator reads it as a failed leg rather than as an
// empty (and therefore "peer had nothing") reply.
func runMemoryAssetsServe(dbPath string) {
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: read stdin: %v\n", memorySyncProg, memorySyncServeAssetsFlagKe, err)
		os.Exit(1)
	}
	out, err := tools.MemoryAssetsServeRemote(body, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: %v\n", memorySyncProg, memorySyncServeAssetsFlagKe, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: write reply: %v\n", memorySyncProg, memorySyncServeAssetsFlagKe, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write([]byte("\n")); err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: write reply: %v\n", memorySyncProg, memorySyncServeAssetsFlagKe, err)
		os.Exit(1)
	}
}

// runMemoryAssetsDryRun reports the local push size without the ssh leg. The
// whitelist check still runs inside tools.MemoryAssetsDryRun: a report-only
// mode is not a licence to size a transfer toward an undeclared host.
func runMemoryAssetsDryRun(peer, dbPath string) {
	patch, err := tools.MemoryAssetsDryRun(peer, nil, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", memorySyncProg, err)
		os.Exit(1)
	}
	fmt.Printf("dry-run %s→%s: push would carry %d row(s) (local non-node rows; anti-reflow excludes node:* imports)\n",
		syncAssetsSelfName(), peer, len(patch.Rows))
}

// syncAssetsSelfName returns this node's display name (GOPI_SYNC_NODE override
// or hostname) for the dry-run header.
func syncAssetsSelfName() string {
	if n := strings.TrimSpace(os.Getenv("GOPI_SYNC_NODE")); n != "" {
		return n
	}
	host, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	return host
}

// printMemoryAssetSession renders one session's outcome. A one-way --push run
// has no local pull tally: it reports the rows NOT applied instead of a zero
// tally that would read as "nothing came back" (Rule 15).
func printMemoryAssetSession(s *tools.MemoryAssetSyncStats) {
	pull := s.PullStats
	if pull == nil {
		fmt.Printf("sync %s: mode=%s pushed=%d pull{skipped=%d} (%.1fs, bound=%.0fs)\n",
			s.Peer, s.Mode, s.PushedRows, s.PullSkippedRows, s.DurationSec, s.TimeoutSec)
		return
	}
	// rows=committed/total and batches/failed name the BATCHED apply: the peer
	// ledger is write-locked per batch, so these are also the numbers that say
	// how far a failed run got. body_differing names the deduped rows whose
	// ANSWER was not stored (the exact-key fold kept the existing row).
	fmt.Printf("sync %s: mode=%s pushed=%d pull{imported=%d existing=%d deduped=%d body_differing=%d refused=%d rows=%d/%d batches=%d failed=%d} (%.1fs, bound=%.0fs)\n",
		s.Peer, s.Mode, s.PushedRows, pull.Imported, pull.Existing, pull.Deduped, pull.BodyDiffering, pull.Refused,
		pull.RowsCommitted, pull.TotalRows, pull.Batches, pull.BatchesFailed, s.DurationSec, s.TimeoutSec)
	for _, e := range pull.Errors {
		fmt.Fprintf(os.Stderr, "  %s\n", e)
	}
}
