<!-- doc-class: historical -->
# The Gate9 git-env red (78 vs 74, stale `:1365`) does not exist at HEAD — it was fixed upstream, before this ground was cut

Task `01a14226-0d2a-7851-a051-7eae41a7d6ae`, 2026-10-16. This is a run's record, not a
standing claim about master: it reports a measurement taken at one revision.

## The reported red, and what this ground actually carries

The briefing reported, at `master 4fd389a`:

- `TestGate9GitEnvRealTreeScansTestFiles` FAIL — **78 hits vs 74 baseline entries**;
- a **stale baseline row** `cmd/agent/cli_tasks_help_surface_basic_test.go:1365` matching no hit.

Both halves are real **at `4fd389a`** and **neither exists at this ground's HEAD**.

## Determination (revision-anchored)

`refs/remotes/gitPushcheck/master` and this ground's HEAD are the same commit;
`refs/heads/master` (the local branch) is an *older* ref, not a competing tip:

| ref | sha |
|---|---|
| HEAD (this ground) | `3db1fd211e30ef0f64c3bbc19cc7ca90d1069f40` |
| `refs/remotes/gitPushcheck/master` | `3db1fd211e30ef0f64c3bbc19cc7ca90d1069f40` (same) |
| `refs/heads/master` | `01c1674121dc089ada8e1d0292a5f2a1cb90a3e5` (older; its commit **is** an ancestor of HEAD) |

`4fd389a` (`4fd389ae60abc963caf8e0677fb89543aa8c0bd2`, 2026-10-16 08:12:18 +0800) **is an
ancestor of HEAD**, so the reported measurement is a genuine earlier reading of the same
history — it was not falsified, it was **already repaired** by commits that landed after it.

| measurement | at `4fd389a` | at HEAD `3db1fd2` |
|---|---|---|
| gate test name | `TestGate9GitEnvRealTreeScansTestFiles` (`git_env_gate_test.go:109`) | `TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly` (`:128`) |
| git-env baseline entries | **74** | **72** |
| `cli_tasks_help_surface_basic_test.go:1365` | live entry at `baseline.go:964` | deleted; the path survives only in two **comments** (`baseline.go:967`, `:971`) that document the rot |
| hits / baselined / escaped / new | 78 / 74 / — | **73 / 72 / 1 / 0** |

The commits that did it, in `4fd389a..HEAD` touching `internal/standards/`:

- `b5cb0628c fix(standards): repair the 4 fixture git-env leaks and ratchet the Gate9
  baseline down` — **half (1)**: the 4 unbaselined hits, repaired rather than permitted;
- `de5b3315c fix(standards): clear Gate6 and Gate9 by deleting a dead env constant and
  repairing the git-env locale control` — **half (2)**: the stale `:1365` permit, deleted;
- `c8348de04 test(standards/gate9): rename the coverage arm to state its two assertions,
  and point red proofs at the fatal arm` — the rename the briefing already suspected.

**Direction of the ratchet at HEAD is down, not up**: 74 → 72 entries. Nothing was added.

## Why the gate is green, and where the 73rd hit lives

`go test ./internal/standards/ -count=1 -v -run TestStandardGates` at HEAD:

```
Rule 13/15 (a fixture's git child carries no inherited repository routing):
73 total hits, 72 baselined, 1 escaped, 0 new (unannotated), 72 baseline entries
```

73 = 72 baselined **+ 1 escaped**. The escape is the reviewed inline marker at
`internal/docclass/anchor_number_fixture_isolation_test.go:198` (`// git-env-ok: this arm is
the WITNESS`), whose subject *is* the inherited environment. `classifyAll`'s own arithmetic
check (`baselined+escaped+unannot == countHits(hits)`) passes, and `enforce()` — reached by
`TestStandardGates`, whose `TestGate9GitEnvRedProof` subtest **PASS**es — raises no new
violation and no stale entry.

`TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly` **PASS**es with its own row
`1918 test files scanned, 73 hit(s), 72 baseline entries` and no `stale baseline entry` error.

## Independent corroboration

An earlier run on a **different, older ground** (`HEAD = 8dd15ee11`, task
`01a141d2-8913-7b17-9b57-636f227d655e`) measured the same two facts and wrote them to
`devdoc/gate9-stale-permit-already-fixed.md`: no live `:1365` permit, coverage arm green
(`73 hit(s), 72 baseline entries`). This run re-measured independently at `3db1fd2` and
agrees — the readings differ only in the scanned file count (1915 → 1918), which tracks
the tree growing between the two grounds. Two independent runs at two revisions, same
verdict.

## What this run changed

**Nothing in the gate or the baseline.** `git diff --stat 3db1fd2..HEAD --
internal/standards/baseline.go` is empty (baseline.go untouched) and `internal/standards/`
as a whole is untouched — there was nothing to repair without breaking the ratchet, and
adding a permit or weakening the assertion is forbidden. The deliverable is the
determination plus this document.

## Not claimed

- **Two other gates in `internal/standards/` are red at HEAD, and neither is the git-env
  gate** (measured with a package-wide `go test ./internal/standards/ -count=1`, exit 1):
  `TestCommandsDocGeneratedBlockMatchesSource` (`COMMANDS.md` verb index disagrees with the
  source — `cmd/agent/cli_notify.go:573` vs `:605`) and `TestDocClassBannerMatchesManifest`
  (`devdoc/removal-audit-shell-census.md` carries the `historical` banner while
  `DOC-INDEX.tsv` classes it `normative`). They are **outside this epic's scope** and are
  reported as observed, not diagnosed. Their existence is why the package-wide test exits 1
  while the git-env gate itself exits 0.
- No claim is made about the briefing's "78 hits" figure being a miscount: it is consistent
  with 74 entries + 4 repaired sites at `4fd389a`, and this run did not re-run the scan at
  that revision (Rule 29 — no second checkout; the reading is quoted, not re-measured).

## Evidence

Raw command output: `~/.gopi/sessions/01a14226-0d2a-7851-a051-7eae41a7d6ae/evidence/`
(`gate9-determination.txt`, `gate9-standardgates-HEAD.txt`).
