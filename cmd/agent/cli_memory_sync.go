package main

// gopi memory sync — the MANUAL cross-node knowledge-sync entry point.
//
//	gopi memory sync --peer peer1            one bidirectional session
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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/tools"
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
  --dry-run     report the size of the outbound patch and stop. It contacts
                the peer ONCE (a read-only identity probe) so it can report
                the INCREMENT — without that question it could only ever
                report the full store, which is the number it exists to
                explain. No patch is sent and nothing is written.
  --timeout N   absolute bound on the ssh leg, in SECONDS. Default 900.
                Also settable as ` + tools.MemorySyncTimeoutEnv + ` (the flag wins).
  --db PATH     knowledge base override (default: ~/.gopi/memory.db).

  Incremental transfer: before exporting, the initiator asks the peer which
  row IDENTITIES it already holds (--dump-asset-identities) and sends only
  the difference. The filter is by identity, NOT by time: a peer stamps
  distilled_at with its own import instant and writes rows of its own, so its
  newest timestamp sits ABOVE rows it does not have — filtering by it was
  measured to drop 72 of 94 genuinely-missing rows. An identity the peer does
  not hold can never be filtered out, so unique-only semantics are preserved.
  A peer that cannot answer (older gopi, ssh failure) DEGRADES the run to a
  full export and says so in one line — never a silent full transfer.

  --serve-assets is the PEER side of the leg — an initiator ssh's it and
  speaks the protocol over stdin/stdout. Never run by a human; it prints
  nothing on stdout but this node's reply export.

  --dump-asset-identities is the PEER side of the INCREMENT — an initiator
  ssh's it and reads this node's question-identity digest on stdout, one
  entry per line. Never run by a human: it imports nothing, writes nothing,
  and opens no leg of its own.

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
	// memorySyncProbeFlagKey is the PEER-SIDE identity probe (the increment's
	// read-only leg). It prints this node's identity digest on stdout and
	// imports nothing, so an initiator can size its patch to the difference.
	memorySyncProbeFlagKey = tools.AssetPeerIdentityProbeFlag
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
		memorySyncServeAssetsFlagKe, memorySyncProbeFlagKey,
	} {
		fs.expect(name)
	}
	for _, name := range []string{
		memorySyncDryRunFlagKey, memorySyncPushFlagKey,
		memorySyncPullFlagKey, memorySyncServeAssetsFlagKe, memorySyncProbeFlagKey,
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
	if fs.has(memorySyncProbeFlagKey) {
		runMemoryAssetsIdentityProbe(dbPath)
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

// memoryServeAssetsOrphanPollInterval is how often the peer side re-reads its
// own parent. 2s is the granularity at which an abandoned import notices it
// was abandoned — small enough to be a handful of rows of wasted work, large
// enough to be free (one getppid per tick).
const memoryServeAssetsOrphanPollInterval = 2 * time.Second

// memoryServeAssetsContext returns the cancellation channel the peer-side
// import runs under, and the func that releases it.
//
// WHY IT EXISTS — measured, not anticipated (row 01a1df33, 2026-11-15): an
// initiator's leg was killed at 120s; on the peer, `--serve-assets` kept
// running 12m33s / 8m24s CPU with PPid=1, holding the ledger's WAL WRITE lock
// and producing ZERO rows. Two facts made that possible, and this closes both:
//
//  1. `io.ReadAll(os.Stdin)` has already returned by the time the import
//     starts (the whole patch arrives up front), so nothing watches the
//     connection during the expensive part — reading stdin again would see EOF
//     immediately, not death.
//  2. The import loop never consulted the context it was given.
//
// So the death signal has to come from somewhere that still exists at that
// moment:
//   - a terminating SIGNAL on THIS process (sshd's channel close normally
//     delivers SIGHUP) — the ordinary path;
//   - and the ORPHAN condition: the parent that would have signalled us is
//     already GONE, so no signal will ever arrive. That is exactly the measured
//     shape (PPid=1), and it is why the poll below is not redundant.
//
// An orphan is noticed by OUR OWN pid's parent changing. Nothing is signalled,
// nothing else is touched (Rule 42): the process stops ITSELF.
func memoryServeAssetsContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signalrec.GracefulStopSignals()...)
	startParent := os.Getppid()
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(memoryServeAssetsOrphanPollInterval)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case sig := <-sigCh:
				fmt.Fprintf(os.Stderr, "%s: --%s: %v — stopping the import at the next batch boundary\n",
					memorySyncProg, memorySyncServeAssetsFlagKe, sig)
				cancel()
				return
			case <-tick.C:
				// A parent that disappears cannot signal us; the initiator's
				// ssh leg is what died, and its death is reparenting.
				if startParent != 1 && os.Getppid() != startParent {
					fmt.Fprintf(os.Stderr, "%s: --%s: the initiator is gone (parent %d → %d, reparented) — stopping the import\n",
						memorySyncProg, memorySyncServeAssetsFlagKe, startParent, os.Getppid())
					cancel()
					return
				}
			}
		}
	}()
	stop := func() {
		close(done)
		signal.Stop(sigCh)
		cancel()
	}
	return ctx, stop
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
	// From here the connection is no longer the liveness signal (stdin is at
	// EOF by construction) — the ctx is (see memoryServeAssetsContext).
	ctx, stop := memoryServeAssetsContext()
	defer stop()
	out, err := tools.MemoryAssetsServeRemoteCtx(ctx, body, dbPath)
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
	patch, exportStats, report, err := tools.MemoryAssetsDryRun(peer, nil, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", memorySyncProg, err)
		os.Exit(1)
	}
	full := len(patch.Rows) + skippedIdentical(exportStats)
	fmt.Printf("dry-run %s→%s: push would carry %d row(s) of %d local row(s) (local non-node rows; anti-reflow excludes node:* imports)\n",
		syncAssetsSelfName(), peer, len(patch.Rows), full)
	// The increment's two facts: how many identities the peer reported, and how
	// many rows that skipped. A degraded run prints its reason on stderr — the
	// numbers above are then the FULL store, which the line makes explicit.
	fmt.Printf("dry-run increment: peer_identities=%d skipped_already_known=%d\n",
		report.PeerIdentities, skippedIdentical(exportStats))
	if report.IncrementDegraded {
		fmt.Fprintf(os.Stderr, "%s\n", tools.AssetIdentitiesDegradedLine(peer, errors.New(report.IncrementReason)))
	}
}

// skippedIdentical reads the increment's counter, tolerating a nil stats
// (SyncExport always returns one, but a caller reading a nil must not panic).
func skippedIdentical(s *tools.SyncExportStats) int {
	if s == nil {
		return 0
	}
	return s.SkippedIdentical
}

// runMemoryAssetsIdentityProbe is the PEER side of the increment: it prints
// this node's identity digest on stdout (one entry per line) and imports
// nothing. It never reads stdin and never opens a leg of its own — an
// initiator ssh's it, exactly like --serve-assets.
//
// Nothing else goes to stdout (stdout IS the protocol); a refusal is on stderr
// with a non-zero exit, so the initiator reads a failure rather than an empty
// digest that would mean "the peer holds nothing".
func runMemoryAssetsIdentityProbe(dbPath string) {
	digest, count, err := tools.LocalAssetIdentityDigest(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: %v\n", memorySyncProg, memorySyncProbeFlagKey, err)
		os.Exit(1)
	}
	if _, err := io.WriteString(os.Stdout, digest); err != nil {
		fmt.Fprintf(os.Stderr, "%s: --%s: write digest: %v\n", memorySyncProg, memorySyncProbeFlagKey, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%s: --%s: %d identit(ies)\n", memorySyncProg, memorySyncProbeFlagKey, count)
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
	// The increment's own line, printed FIRST and unconditionally: whether the
	// session shipped the difference or the whole store is the fact an operator
	// reads this command for, and a degraded run must never look incremental.
	if s.IncrementDegraded {
		fmt.Fprintf(os.Stderr, "%s\n", tools.AssetIdentitiesDegradedLine(s.Peer, errors.New(s.IncrementReason)))
	}
	if s.PeerIdentities > 0 || s.SkippedIdentical > 0 {
		fmt.Printf("sync %s: increment peer_identities=%d exported=%d skipped_already_known=%d\n",
			s.Peer, s.PeerIdentities, s.ExportedRows, s.SkippedIdentical)
	}
	pull := s.PullStats
	if pull == nil {
		fmt.Printf("sync %s: mode=%s pushed=%d pull{skipped=%d} (%.1fs, bound=%.0fs)\n",
			s.Peer, s.Mode, s.PushedRows, s.PullSkippedRows, s.DurationSec, s.TimeoutSec)
		// A --push leg does not apply the reply locally, so the PEER's import
		// tally never reaches the pull branch below — and a peer that SKIPPED
		// our rows because another import of this node already held its lease
		// would otherwise print as a clean run. Report it here or it is lost.
		printPeerLeaseSkip(s.Peer, s.PeerImport)
		return
	}
	// rows=committed/total and batches/failed name the BATCHED apply: the peer
	// ledger is write-locked per batch, so these are also the numbers that say
	// how far a failed run got. body_differing names the deduped rows whose
	// ANSWER was not stored (the exact-key fold kept the existing row).
	fmt.Printf("sync %s: mode=%s pushed=%d pull{imported=%d existing=%d deduped=%d body_differing=%d refused=%d skipped_by_lease=%d rows=%d/%d batches=%d failed=%d} (%.1fs, bound=%.0fs)\n",
		s.Peer, s.Mode, s.PushedRows, pull.Imported, pull.Existing, pull.Deduped, pull.BodyDiffering, pull.Refused,
		pull.SkippedByLease, pull.RowsCommitted, pull.TotalRows, pull.Batches, pull.BatchesFailed, s.DurationSec, s.TimeoutSec)
	if pull.SkippedByLease > 0 {
		// The corpus was NOT imported because another process holds this
		// (peer, database) import lease. Say it on the report line's own face —
		// a zero-row pull that stays silent is indistinguishable from a clean
		// sync, and that is exactly the confusion this lease exists to end.
		fmt.Printf("sync %s: skipped %d row(s) — this batch is already being imported by %s\n",
			s.Peer, pull.SkippedByLease, pull.LeaseOwner)
	}
	for _, e := range pull.Errors {
		fmt.Fprintf(os.Stderr, "  %s\n", e)
	}
}

// printPeerLeaseSkip reports a PEER-side lease skip on the initiator's own
// console. It is one sentence and one number — the point is that the initiator
// can tell "the peer imported nothing because it had nothing to do" apart from
// "the peer imported nothing because somebody else was already importing it",
// which is the whole reason the lease exists (Rule 15: no silent zero).
func printPeerLeaseSkip(peer string, tally *tools.SyncImportTally) {
	if tally == nil || tally.SkippedByLease == 0 {
		return
	}
	fmt.Printf("sync %s: peer skipped %d row(s) — this batch is already being imported by %s\n",
		peer, tally.SkippedByLease, tally.LeaseOwner)
}
