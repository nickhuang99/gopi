<!-- doc-class: historical -->
# devdoc: the declared-topology wording has ONE definition — and how the "[3/6] drift" task turned out to be already landed

**Audience:** a future agent who is handed a "gate-vs-production drift" dispatch
(this one, or its sibling items from the same epic).

## What the reference shape is

`internal/tools/peer_topology.go` owns the declared-topology vocabulary, in ONE place
(Rule 14):

- `DeclaredPeerGate` — the shared clause `sync topology is declared, never discovered`.
- `undeclaredPeerError(peer, whitelistName, key, env, clause)` — the ONE sentence
  renderer.
- `declaredPeerError` / `assetDeclaredPeerError` — the two channels (`sync_peers` /
  `memory_peers`).
- `PeerDeclared(peer, cliPeers)` / `RequireDeclaredPeer(peer, cliPeers)` — the gate;
  `cliPeers` is the highest-precedence source for a run-scoped `--peers` list.

Every peer-taking face calls `RequireDeclaredPeer` and adds only its own phase prefix
(`memsync push: `, `harvest: `, `diff: `). The gate's literal is guarded by a Rule 13
mirror gate: `TestDeclaredTopologyWordingHasOneDefinition`
(`peer_topology_test.go`) scans every non-test `*.go` in `internal/tools/` with
`not in the [^"]{0,24}whitelist` and fails any other file that spells it.

## The drift, and its repair (history worth keeping)

The rule was implemented per face, and the copies drifted into **three** spellings
(full / nothing / shorter — see `peer_topology.go`'s header). `70a4b2b9`
(2026-09-22) introduced the ONE gate. `6c2b6f5c` then re-typed the sentence inside
`SyncTaskPushOnlyPeers` **and** checked the (empty) peer list before the whitelist —
so on a node with no `sync_peers`, `--peers <undeclared>` was misdiagnosed as *"no
sync_peers declared"* (the wrong diagnosis, plus a second source of truth for the
wording the gate forbids). `4f2255ac` (2026-09-25 11:40) repaired exactly that: the
block delegates to `RequireDeclaredPeer(p, nil)` and asks it **first**.

`4f2255ac` also repaired five other reds in the same sweep; check it before believing
a red-test claim measured on a mid-day master — that day's provenance/dedup commits
turned several gates red at once, and a stale briefing can name a red that a later
commit re-greened.

## The trap a "fix-it-again" run falls into

The tempting tightening — have `SyncTaskPushOnlyPeers` stop calling the gate per peer
and delegate to a single face — **must not** be done naively. That face validates the
DECLARED whitelist only; the run-scoped `--peers` list (`cliPeers`) is a
higher-precedence source, so replacing `RequireDeclaredPeer(p, nil)` with
`RequireDeclaredPeer(p, peers)` (or with a declared-only resolver) would refuse an
alias the operator legitimately passed for that one run — a regression, not a fix.
The shipped shape (per-peer `RequireDeclaredPeer(p, nil)`, *before* the empty-list
refusal) is the correct one.

## Checklist when a "gate-vs-production drift" task arrives

1. Run the named gate at HEAD **first** — the dispatch may be a duplicate of work
   already merged (Rule 26 allows the duplicate; measurement closes it).
2. If green: run the gate's own regex repo-wide and count the occurrences against the
   definition file; if green files exist only at the definition site, there is nothing
   to delete.
3. Red-proof the instrument anyway (inject the literal, watch it fail, remove it) so
   "the scan is green" cannot be confused with "the scan is inert".
4. Run the real surface (Rule 31) and quote the message a caller actually sees;
   compare it byte-for-byte with the canonical sentence — that IS the
   user-visible-wording guarantee the task asks for.
5. Only then: report, and state plainly that no code change was available to make.
