<!-- doc-class: historical -->
# Salvage Measurement: `salvage/version-identity-20260928` — intent SUPERSEDED by `ddbc3df1`

**Verdict: nothing to re-implement.** The archived branch was measured item by
item against current master (178aae9a = 6a5bc53a + 3 doc commits). Its intent is
not "already landed upstream" — it was **deliberately deleted** the day after the
branch forked, and the deletion was the point of the commit that made it
(`ddbc3df1 feat(version): the binary's identity is its own — internal, constant,
checkout-independent`, 2026-09-24). This document is the salvage record; **no
production code change accompanies it**, and none is warranted.

## 0. What was archived, and why it could not merge

| field | value |
|---|---|
| branch | `salvage/version-identity-20260928` |
| tip | `c039f519a476e998c8493cb7e7f134d7e9211371` |
| merge-base | `0ac3db6495f8e67a7c1bb9b1e6ce072f3822ce0e` (2026-09-23) |
| master at archive | `cb89fff6bc636427741bfdec0f22abb981a9edc6` |
| master measured | `178aae9a` (2026-09-29) |
| archive reason | non-ff salvage branch; substantive conflict / unrelated history with master |

The ref is deleted but the object is still resolvable, and the tip is **not**
reachable from master:

```bash
git cat-file -t c039f519a476e998c8493cb7e7f134d7e9211371   # commit
git merge-base --is-ancestor c039f519 master; echo $?      # 1
```

The rescue commit's own message states what it is worth:
*"Not reviewed, not built, not tested. Landed only so the bytes survive."*
It touched three paths — `cmd/agent/cli_version.go` (+85), `internal/version/version.go`
(+502), and `gopi-behind` (a new **26284210-byte ELF**), so Rule 23's read-only
inspection applies and Rule 35 forbids carrying the binary forward.

## 1. The intent, recovered from the diff (never applied)

The stale `.diff` attachment was read for intent only. It carries three
interlocking items, each stated in its own doc comments:

- **I1 — an honest fallback.** `gopi version` must not silently degrade to a bare
  revision: when the checkout cannot lend the binary a name, the command states
  *why* (`RefusalNoCheckout`, `…NoNameAtCheckout`, `…UnrelatedHistory`,
  `…NamePostdatesBuild`, `…NameNotAttachable`), and the fallback prints the same
  report **shape** as the borrowed path (`FallbackDescription`).
- **I2 — borrow on reachability, not on identity.** `Info.BorrowName` describes
  the **binary's** position (`describeTagAt(dir, embedded)`) instead of the
  checkout's HEAD, so a name survives the ordinary day (build, then a merge
  lands) while a binary from unrelated code is still refused. The comment names
  intent (b) vs intent (a) explicitly.
- **I3 — a `git describe` parse that cannot mis-split.** `tagIndex`/`splitDescribe`
  anchor on the final `-g<hex>` marker rather than a bare `-g`, because a minted
  tag *name* is free text and this repo's own minted names contain `-g` inside
  the name (`version/v20260923-1346-log-request-rawlog-resolver-raw-log-goes-through-efbbb87d`).

## 2. Measurement against current master

The measurement order decides the verdict. The branch's merge-base `0ac3db64`
(2026-09-23) is an **ancestor** of master (`git merge-base --is-ancestor 0ac3db64 master` → 0),
and the retirement `ddbc3df1` is dated **2026-09-24 — after the fork**. So master
did not independently restate this intent; it removed the machinery the intent
had been improving.

| intent item | the diff's shape | master today | deciding commit | status |
|---|---|---|---|---|
| **I1** refusal sentences + `FallbackDescription` | `Refusal*` constants in `internal/version`, reason rendered by `cmd/agent` | `gopi version` prints `internal/version.Version` (compiled in) plus metadata read from the **artifact**; there is no borrowing left, so there is no checkout-dependent degradation to narrate. The remaining absence it does state is the missing toolchain stamp (`versionStampSkippedCause` / `versionNoVCSStamp`) | `ddbc3df1`, then `e44723c0` | **RETIRED** — the mechanism it narrated was deleted, and the deletion is the argument |
| **I2** `BorrowName` + reachability ladder | binary-vs-checkout reachability decides whether a name may be lent | `internal/version/version.go`'s package doc names the deletion: *"the name-borrowing decision (a binary taking its name from the checkout it stands in), the `describe` parsers that fed both … They were deleted whole rather than kept behind a flag, because a dormant mint is the coupling this epic exists to remove"* | `ddbc3df1` | **DELIBERATELY RETIRED** |
| **I3** `tagIndex` / `splitDescribe` / `rangeCount` / `isAncestor` / `describeTagAt` | parse `git describe --long --dirty` output | **0 production hits** for every one of them; the only surviving mentions of the mint are two comments recording its removal (`cmd/agent/cli_tasks_rebuild.go:72`, `cmd/agent/merge_run.go:1889`) | `ddbc3df1` | **DELIBERATELY RETIRED** |
| **hygiene** no 26 MB ELF committed | the archived tip *does* commit `gopi-behind` | master has never tracked it | — | **CARRIED** — this rework commits no binary |

Evidence (`git grep -c` on master, production Go, `*.go` minus tests):

```
tagIndex 0   splitDescribe 0   rangeCount 0   describeTagAt 0
BorrowName 0 FallbackDescription 0 localDirtySuffix 0 ReportLocalKey 0 headRef 0
git check-ignore -v gopi-behind   # rc=1 (see §3)
```

Two facts make a "re-implementation" a non-answer rather than a task:

1. **It would restore deleted coupling.** `ddbc3df1`'s own message calls the mint
   and the borrowing *"deliberate BREAKING"*: gopi merges any repository and runs
   in directories that are not its own, so an identity read out of cwd is not an
   identity — the same binary answered `name: no version name borrowed — the
   working directory is not a git checkout` from `/tmp` and a `checkout: … at
   3300b6fb0513` line from a scratch repo. Master's gate pins the contrary:
   `cmd/agent/version_identity_test.go:TestVersionDoesNotBorrowANameTheCheckoutCarries`
   tags a fixture with a `version/*` tag and requires the answer to be
   **byte-identical**. Re-implementing I1–I3 would have to break that test.
2. **The diff is not self-contained.** It consumes the sibling commits' API
   (`gopiversion.Describe`, `MatchesRevision`, the `Refusal*` family) whose call
   sites the retirement removed — it does not compile against the untouched
   master tree. It also carries a Rule 14 mirror of its own: the same refusal
   sentence is defined **twice** (`cli_version.go`'s `refusalNoCheckout` and
   `internal/version.RefusalNoCheckout`), which is exactly the drift the diff's
   prose says it avoids.

## 3. One genuine gap found — filed, not patched

`gopi-behind` is not ignored by master's `.gitignore` (`/gopi` and `/agent` are
anchored file names; the directory rule `bin/` does not cover the repo root):

```bash
git check-ignore -v gopi-behind   # rc=1 — not ignored
```

The artifact is still **produced** in the repo root by a tracked recipe —
`tasks/01a0cea0-rule31-two-states.sh:35` runs
`CGO_ENABLED=0 go build -o gopi-behind ./cmd/agent/` and then `./gopi-behind version`
(lines 41-42) — so the archived branch's mistake (committing a 26 MB binary) is
reachable by an ordinary `git add` in any task worktree, and Rule 30 reads it as
`??` litter at close-out.

Filed as its own work item — **task `01a0e97a-e3e2-7017-977a-d2984b6221cb`**
(tags `bug,hygiene,gitignore`) — rather than patched here: a `.gitignore` rule
plus a red-proved gate is a reviewable change to a hygiene surface, and this
branch's deliverable is a measurement record. Rule 35 applies to the fix as
written: ignore-rules and a test, nothing resident, nothing new to own.

## 4. What this branch delivers

- This measurement record and **its `devdoc/DOC-INDEX.tsv` row, in the same
  commit** (Rule 36).
- No production code change. The acceptance for the rework — build, vet, merge
  probe, manifest union — is met by a branch whose content is the honest
  negative; re-implementing I1–I3 would be a regression against `ddbc3df1` and
  against `TestVersionDoesNotBorrowANameTheCheckoutCarries`.

## 5. Not done (honest)

- **The intent was not re-implemented** — verdict superseded, not skipped. §2
  names, per item, the commit that decided it.
- **The 26 MB ELF was neither committed nor pruned**: it remains reachable only
  from the archived tip `c039f519`, which is not an ancestor of master.
- **`gopi-behind`'s missing ignore rule is dispatched, not fixed** (§3), so a
  reader who lands this branch should expect that separate row.
- Measured at master `178aae9a`; a later master tip may move the retirement, in
  which case re-run §2's greps rather than trusting this table.
