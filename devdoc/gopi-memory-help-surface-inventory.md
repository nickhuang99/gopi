<!-- doc-class: normative -->
# `gopi memory` help-surface inventory (G3 of the help-surface hardening)

> **Retired mechanism (2026-11-05), PARTLY REVERSED (2026-11-06).** Two verbs this inventory
> counted were DELETED with the cross-node sync family: `memory memsync` and `memory sync`.
> `memsync` stays deleted — its spelling still dispatches to a removal sentence and exits 1.
> **`memory sync` came back** as a MANUAL door (one invocation = one session, exit; no
> `--every`, no store trigger, no resident tick), so it is advertised again, it has a row in
> the table below (row 14), and the advertised verb count is 14. Its basic test is
> `cli_memory_sync_test.go` — the three direction shapes, the `--push`+`--pull` refusal, the
> undeclared-peer refusal that names this node's whole whitelist, and the page/parser agreement
> — because the decisions it makes are CLI-level rather than tool-level. The METHOD this page
> applies (derive the advertised set from the help, then require every verb to be either
> covered by a test or listed here with a reason) is untouched and still runs as a gate.

**Task**: G3 (epic brief: help-surface hardening, the `gopi memory`
knowledge-base group).
**Branch**: `feat/g3-memory-help-surface`.
**Question this document answers**: for every `gopi memory` verb/flag the LIVE
help advertises, does a BASIC test exist that INVOKES it the way the help says
to invoke it (happy path + first refusal) — never `-h`?

**Why a test and not a reading.** The motivating defect of the family (G1,
`gopi task sync --push-only --task-id T`) was implemented, compiled,
unit-tested, reported complete, and never typed (PROJECT.md Rule 31). A suite that
enters through a seam the author chose cannot see that class. So "covered" below
means **the help's own argv was driven through the real binary** — the same
spelling an operator types.

## The surface, as the help advertises it

Twelve subcommands under the `memory` group word, plus `gopi brew` (the one
memory-family verb that lives outside the group word, named beside it in the
briefing). `memory sync` and `memory backup` each carry a SECOND mode
(`--serve-assets` / `--serve`) that is the peer side of their own leg.

**Amended by the epic verb-restructure (2026-10-05).** Two verbs that used to be
TOP-LEVEL are now advertised here as faces of the knowledge dimension, and both
keep their flat spelling working (a demotion moves the door, never the command):

* `memory brew` — the producing half (distill lessons out of session logs);
  alias `gopi brew`.
* `memory memsync` — the transfer half by FILE (a reviewable JSON patch you move
  by hand); alias `gopi memsync`. It is NOT `memory sync`: that spelling is the
  LIVE bidirectional ssh session, a different command, and each page names the
  other as its sibling.

## Re-derivation (the quoted command)

The surface list is DERIVED, never hand-typed. Two independent derivations,
which the build requires to agree:

```bash
# 1. the operator-facing derivation (this document's table)
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ scripts/g3-memory-help-surface.sh ./bin/gopi
search	db,intent,limit	3
store	answer,db,keywords,question,round-index,session-id,source,topic	8
get	db	1
list	db,limit	2
update	answer,confidence,db,keywords,question,source,tier,topic	8
delete	db	1
feedback	db,list,memory-id,reason,recompute,suggest-cold,task,unused,useful,useless	10
converge-keywords	db,dry-run	2
backfill-tokens	db,dry-run	2
backup	db,dir,name,no-ship,peer,serve	6
provenance	backfill,counts,db,harvest-latent,latent,latent-calibrate,latent-candidates,latent-crosscheck,reclassify,shape	10
brew	dir,dry-run,json,limit,session	5
capture-stats	json,root	2

# 2. the same list, re-derived in the test package from the SAME usage text
#    (cmd/agent/cli_memory.go memoryUsage), which is what the binary prints:
$ go test -count=1 -run TestMemoryHelpSurfaceMatchesInventory -v ./cmd/agent/
=== RUN   TestMemoryHelpSurfaceMatchesInventory
--- PASS: TestMemoryHelpSurfaceMatchesInventory (0.00s)
```

Totals: **14 verbs, 68 advertised verb-flag pairs** (re-derived by `scripts/g3-memory-help-surface.sh`; Rule 38 — the generator, not a hand count). The gate compares the
derived set against both the pinned expectation and the covered set, so a verb
added to the help with no row and no test fails the build.

TWO SCANNER SHAPES this derivation had to get right, both worth recording
because each silently under-reported on the first pass:

* **one verb, several synopsis heads.** `provenance` prints two heads and
  `feedback` four (their mode flags do not fit on one line), so the table row is
  emitted only when the VERB changes — a scanner that emits per head reports
  `feedback` four times with a third of its flags each.
* **the description block is not a terminator.** A verb whose heads are
  separated by prose must keep accumulating; a scanner that ends the synopsis at
  the first non-head line loses the second head entirely.

## Inventory

`| # | verb | advertised flags | covered | how / why not |`

Numbered rows are read back by the build
(`TestMemoryHelpSurfaceInventoryTableMatchesHelp`), so the table cannot drift
from the help it describes.

| # | verb | advertised flags | covered | test / reason |
|---|------|------------------|---------|----------------|
| 1 | `search` | `--db --intent --limit` | yes | `TestMemoryVerbSearchRunsAsAdvertised` — store-then-search on the real binary; first refusal = missing `<query>` (exit 1), second = `--limit 0`. |
| 2 | `store` | `--answer --db --keywords --question --source --topic` | yes | `TestMemoryVerbStoreRunsAsAdvertised` — the help's minimal argv plus every optional flag; refusals = a missing required parameter and a stray positional. |
| 3 | `get` | `--db` | yes | `TestMemoryVerbGetRunsAsAdvertised` — renders topic/question/answer; first refusal = missing id, second = an absent id exits 1 (never a blank success). |
| 4 | `list` | `--db --limit` | yes | `TestMemoryVerbListRunsAsAdvertised` — bare form (default N) + `--limit 1`; refusals = `--limit 0` and a stray positional. |
| 5 | `update` | `--answer --confidence --db --keywords --question --source --tier --topic` | yes | `TestMemoryVerbUpdateRunsAsAdvertised` — `--tier cold` with the demotion READ BACK; refusals = no field given, `--confidence 2`. |
| 6 | `delete` | `--db` | yes | `TestMemoryVerbDeleteRunsAsAdvertised` — the removal is verified by re-reading the id; refusals = a second delete, missing id. |
| 7 | `feedback` | `--db --list --memory-id --reason --recompute --suggest-cold --task --unused --useful --useless` | yes | `TestMemoryVerbFeedbackRunsAsAdvertised` — all three ratings plus all three modes in one case; refusals = no rating, two ratings. |
| 8 | `backup` | `--db --dir --name --no-ship --peer --serve` | yes | `TestMemoryVerbBackupRunsAsAdvertised` — `--no-ship` (snapshot + verification reported), `--name`, and `--serve` driven with a real envelope whose receipt is parsed and whose artifact is checked on disk; refusals = the mistyped `--no-shipp`, `--name ../escape`, an undeclared `--peer` (which must still report the local artifact as good), and a short/altered transfer. **The live ssh ship leg (`LedgerBackupShip` over real ssh) is NOT covered — same reason; its seam tests live in `internal/tools/ledger_backup_test.go`.** |
| 9 | `brew` | `--dir --dry-run --json --limit --session` | yes | `TestMemoryGroupedBrewAndMemsyncRunAsAdvertised` — the grouped spelling runs the pipeline against an empty session dir (`--dry-run`), its own page is printed by `memory brew -h`, the first refusal (`--limit abc`) still names the domain, AND the flat `gopi brew` still runs (the alias half of the demotion). |
| 10 | `provenance` | `--backfill --counts --db --harvest-latent --latent --latent-calibrate --latent-candidates --latent-crosscheck --reclassify --shape` | yes | `TestMemoryVerbProvenanceRunsAsAdvertised` — every mode including the bare default (asserted EQUAL to `--shape`); the refusals are the interesting half: `--latent-crosscheck` before `--harvest-latent`, two modes at once, a stray positional. |
| 11 | `converge-keywords` | `--db --dry-run` | yes | `TestMemoryVerbConvergeKeywordsRunsAsAdvertised` — a ledger seeded with the pre-7139b28 duplicate-tail shape driven through the real binary: `--dry-run` reports the before histogram and the scan/change split and writes nothing, the real run collapses a two-block answer to ONE block keeping the LAST (the block that equals `search_kw`), the body is byte-identical afterwards, and a SECOND run changes 0 (idempotent). First refusal = a stray positional. |
| 12 | `backfill-tokens` | `--db --dry-run` | yes | `TestMemoryVerbBackfillTokensRunsAsAdvertised` — a legacy ledger (a bigram-era `jieba_token`) driven through the real binary: `--dry-run` reports the scan/change split and does not write, the real run re-derives the token and the FTS index answers at the segmenter's granularity, and a SECOND run re-derives 0 (idempotent). First refusal = a stray positional. |
| 13 | `capture-stats` | `--json --root` | yes | `TestMemoryVerbCaptureStatsRunsAsAdvertised` — the read-only survey of the auto-memory RESPONSE gate driven over a corpus the test wrote: three bodies (one markdown table, one acknowledgement, one long shapeless prose) counted arm by arm, `--json` parsed back as the same numbers, and the note gate's measured 1.31% quoted on the surface. Refusals: a corpus with no `session.jsonl` (named), an unknown flag (named), and — with no `--root` — a profile holding no sessions, which is what proves the DEFAULT root is the profile's session directory rather than the process' cwd. |
| 14 | `sync` | `--db --dry-run --peer --push --pull --serve-assets --timeout` | yes | `cli_memory_sync_test.go` — the three direction shapes resolved from the same flag table the parser uses (`--push` → outward, `--pull` → inward, neither → bidirectional), the refusal of `--push`+`--pull` together, the undeclared-peer refusal that names this node's whole whitelist (and the DIFFERENT sentence when the whitelist is empty), and page/parser agreement (every admitted flag documented, the retired `--every` NOT advertised, the MANUAL contract stated). **The live ssh leg is not covered here** — a real session was measured in two sandbox profiles with a shim standing in for ssh (two-way convergence 1/1, then idempotent `imported=0 existing=1`); see `devdoc/memory-sync-manual-door-restored.md`. |

### Advertised outside the table, and covered

| surface | advertised where | covered by |
|---------|------------------|-----------|
| `gopi brew [--dry-run --limit --session --dir --json]` | the sibling page named in the briefing (`brewUsage`); the group's closing paragraph points at the memory family's own verbs | `TestBrewRunsAsAdvertised` — every advertised flag, with a session directory that holds nothing (so no LLM call is made); refusals = a stray positional, `--limit abc`; a bad `--session` is reported BY NAME. |
| the seven legacy flat verbs (`memory_search`, `memory_store`, `memory_get`, `memory_update`, `memory_delete`, `memory_feedback`, `memory_feedback_list`) | the group help's closing paragraph, by name | `TestMemoryLegacyFlatVerbsStillRunAsAdvertised` — each one runs, prints the advertised deprecation note pointing at the grouped form, and still does the work. |
| `gopi memory` (group, no subcommand) | `gopi memory -h` / the group page | `TestCLIContractSubcommandHelp` (pre-existing, `cli_contracts_test.go`) pins the bare-group page; this file's `TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand` pins its CONTENT against the dispatch set. |

## Findings (help/code disagreements found by this pass)

1. **The group page omitted `gopi memory backup` entirely.** `memorySubcommandNames`
   dispatches it, the completion set offers it, and the unknown-subcommand error
   line prints it — but a reader of `gopi memory -h` never learned it existed.
   The sub-verb has a full page (`gopi memory backup -h`), so the group page was
   the only undiscoverable link. Fixed in this branch: the group page now
   carries the backup synopsis (and names its flags), and
   `TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand` fails if a
   dispatched subcommand loses its synopsis again.
2. **The group page named neither peer-side mode.** `--serve-assets`
   (`memory sync`) and `--serve` (`memory backup`) — the two flags an operator
   must never type by hand, and the two that a stale peer binary fails on — were
   advertized only on pages the group page did not point at for them. Both are
   now spelled on the group page beside their command, with the "ssh'd by an
   initiator, never run by a human" contract in the same breath.
3. **`gopi brew -h` after another flag prints the run, not the page.** Measured
   before any change: `gopi brew --json -h` runs the pipeline and prints brew
   stats instead of the help page, while every other verb in the family answers
   `-h` wherever it appears (`runMemorySubcommand` and `runMemorySync` scan for
   `-h`; `runMemoryBackup` does too). This is a help/code disagreement, so per
   the brief it is recorded rather than papered over: it is **not** fixed here
   (the fix is a one-line move of the `-h` scan ahead of the flag parse, but it
   changes a verb's exit code for that argv and deserves its own review), and it
   is filed as a `bug` task. The test above therefore asserts `brew -h` only in
   the leading position, which is the position the help page itself uses.

   **FIXED 2026-11-05** (task `01a1ac90-530d`): `runBrewSubcommand` now scans
   the WHOLE argv for `-h`/`--help`, ahead of the flag parse, so `brew -h` after
   another flag prints the page like every sibling verb. The same change gave
   this verb a STRICT flag table (`newBrewFlagSet`), which is the other half of
   the same defect: `gopi memory brew --sinc 2099-01-01` used to be ACCEPTED and
   followed by a real sweep. The group page's synopsis for this verb advertised
   `[--since DURATION] [--verbose]` — two flags borrowed from `memory sync` /
   `task sync` that brew has never parsed — and is now built from `brewSynopsis`
   (const concatenation), so the group page, the verb page and the parser share
   ONE vocabulary. `TestBrewFlagSurface*` (`cli_brew_flag_surface_test.go`)
   pins all three surfaces; the finding-3 assertion in the e2e file now asserts
   the FIXED behaviour.

## Not covered, and why (honest residual)

| surface | why not | what would be needed |
|---------|---------|----------------------|
| the LIVE ssh leg of `gopi memory sync --peer P` (a real peer pair) | **covered only through a shim.** The verb and its decisions are covered by `cli_memory_sync_test.go`, and a real two-profile session was measured by replacing `ssh` with a harness that runs the remote command string locally (`/home/user/.gopi/sessions/01a1b2a2-4cf0-70b0-906f-c77f861fde69/evidence/fakessh`) — every other layer (argv shape, stdin/stdout protocol, the peer binary's dispatcher, the merge) ran for real. What is NOT covered here is bytes over a real network to a host running this build; the fleet's binaries predate the restoration. | rebuild on `peerbox`/`hp`, then one `gopi memory sync --peer peerbox` from this node and a `pull{imported=N}` in its report |
| `gopi memory backup` live ssh ship | same | same |
| `gopi brew` producing a real lesson | needs an LLM credential and real session JSONLs; a probe that fabricates them would be testing the fixture | an off-path run against real session logs with a credential |
| `gopi memory feedback --suggest-cold` with a candidate present | requires ≥3 `useless` votes and 0 `useful` on one row; the probe asserts the empty answer BY NAME (which is the same code path's other branch) | a richer fixture (3 useless votes on one row) — cheap if ever needed |
