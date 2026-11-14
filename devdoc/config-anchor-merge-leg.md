<!-- doc-class: historical -->
> **RETIRED — this page is HISTORY, not machinery.** `internal/tools/merge_config_anchor_gate.go`
> and `internal/tools/merge_config_anchor_gate_test.go` were deleted whole in commit
> `5a35f6104` (*refactor(docclass): delete the document-pipeline corpus package (slice 3/5)*),
> together with the `internal/docclass` package they belonged to. **Do not run a command this
> page shows**: the files it names do not exist, and `go test ./internal/tools/ -run
> 'TestMergeConfigAnchorGate'` matched NOTHING for months after the deletion — it printed
> `[no tests to run]` and exited 0, a vacuous green.
>
> The LIVE check **was** the census generator's own delta leg plus the arms restored under the
> same documented names (they drove the generator, not a merge-time lane). **BOTH ARE GONE
> (2026-11-05)**: `78bdf05a` (operator ruling) deleted the whole documentation-citation family —
> `internal/standards/testdata/config-anchor-census.go`, the `.tsv` it wrote,
> `citation-census.go`, `citation-selector-census.go` and the gates that drove them — and
> `b8cad761` deleted `internal/tools/merge_config_anchor_gate_test.go` with the last census arm.
> `go run` on the generator prints `stat … no such file or directory`; `-run
> TestMergeConfigAnchorGate` prints `[no tests to run]` and exits 0, an EMPTY GREEN that is not
> a pass. **Nothing judges CONFIG.md's anchors today** — the anchor rules are prose authors
> obey, and a reader must not run either command this page used to give.
>
> The merge-time SIGNAL this page describes (a rotted anchor found in a merge TARGET, reported
> as one bug row) is **gone**: nothing in the tree reads CONFIG.md's anchors at merge time any
> more. `internal/configanchor/anchor.go` says so in its own package doc.

# The CONFIG-ANCHOR merge leg — a SIGNAL, not a stricter predicate

**Task**: `01a13999-4647-7153-8fb4-8b4d3b40146e` (branch `task/01a13999464771538fb48b4d3b40146e`).
**Files**: `internal/configanchor/anchor.go`, `internal/tools/merge_config_anchor_gate.go`,
`internal/tools/merge_config_anchor_gate_test.go`, `internal/tools/merge_target_doc_gate.go` (one field + the leg's call).

## The gap, stated exactly

CONFIG.md publishes hand-written `path.go:NNN` anchors. **No merge-time lane reads CONFIG.md**:

| lane | what it reads |
|---|---|
| post-pass build (**deleted 2026-11-01** with the retired top-level `gopi merge` verb — `internal/tools/merge_pass_verify.go`, no lane runs it today) | `go build` / `go vet` — a document is data, a compiler has no opinion |
| ⓪e published-anchor lane | the gendoc of record (COMMANDS.md) |
| target-scoped devdoc lane (`merge_target_doc_gate.go`) | the `internal/docclass` corpus (`tasks/` + `devdoc/`) — zero mentions of CONFIG.md |

The only readers of CONFIG.md's anchors are the six `TestConfigDoc*` gates, and they
refuse exactly five classes: unresolvable anchor, blank line, bracket-only line, a
name-carrying reference missing its symbol, a call site. An anchor that rots onto an
**ordinary content line** — `} else {`, a mid-block `//` continuation — is **A4(b)** of
`internal/standards/anchor_semantics.go`: declared **OUT OF SCOPE**, because a legitimate
anchor and a drifted one can hold that shape and no predicate short of a semantic model
tells them apart (measured: a naive "not a comment continuation" rule reds ~58
*legitimate* anchors). So the tree moves under the number and every gate stays green —
by design, not by oversight. **What was missing was never a stricter predicate; it was a
signal.**

## What the leg asks instead

It does not judge prose and does not guess a "correct" line. It compares the target tree
against the **frozen record** — `internal/standards/testdata/config-anchor-census.tsv`,
landed as re-derivable data by the sibling item, carrying per anchor the line it landed on
and an excerpt of that line. Two mechanical questions:

- **ROT** — the recorded target line no longer holds the recorded text (compared through
  the ONE normalisation, `configanchor.Excerpt`, so a whitespace artefact is not a finding).
  When the file still carries the recorded text, the finding names **where it now sits**
  (the actionable half: "wrong" with no place to move the citation to is not actionable).
- **GONE** — the anchor the table records is no longer published in the tree's CONFIG.md
  **and** its recorded text is nowhere in its file either.

A row whose file is absent, or whose recorded line is past the end of the file, is neither:
it is counted **unresolved** and reported as a count, never as a finding (Rule 15 —
"found nothing" must not look like "never scanned"). The PASS line names the population.

## The third outcome — STALE RECORD — and why it is not a finding

A record can be stale rather than the tree drifted: a citation **hand-re-pointed**
(`:706` → `:711`) leaves the table recording an anchor nobody publishes while the citation
itself is *correct*. Reporting that as drift is exactly the false positive the epic
forbids, and it is **measured, not hypothetical**: at the time this leg was written the
shipped table recorded three `internal/config/settings.go` anchors a later fix had moved by
one line each. So a vanished anchor **whose recorded text still sits somewhere in its file**
is counted STALE RECORD — a statement about the *record's age*, whose repair is to
re-derive the census (the generator's own command), which is not a bug.

The discriminant is therefore not "the anchor vanished" but **"the text is nowhere"**. That
is what keeps the report honest: the shape the epic is about is *a number that stayed put
while the text moved under it*, and only that shape is reportable.

## Why it rides the target-scoped lane (Rules 3/35)

`merge_target_doc_gate.go` already materialises the target's tree into scratch **once per
run** and already owns the notify + `bug`-row channels, deduped by defect identity, for
exactly this class of finding (a defect of the TARGET, never reported to the author of an
incoming branch). The leg is handed that same tree: it reads no git, resolves no scratch,
creates no worktree, starts no tick, and adds no tool, script or hook. It is a **second
question on an existing channel** — which is why it costs the machine and the operator
nothing that was not already being paid.

Its verdict is folded into the sibling's verdict struct (`Cfg`) rather than returned
separately, and it is carried out on **every** return path — including the paths where the
devdoc question abstains or is clean — so the two questions cannot mask each other: a
caller that only saw the devdoc half would report the tick silent while a citation was
measured wrong (Rule 15).

## And it never refuses

`configAnchorVerdict.Refused` exists for the caller's symmetry with its sibling and is
**always false**, proved by a test that presents a real drift and asserts it. A document
citation one line off is not a reason to hold a healthy branch: the finding is a `bug` row
(a queue item a later run picks up) and a log line — the repair channel the epic asked for.

## The dedup identity is DEFECT-scoped

`mergeConfigAnchorBugSummaryFmt` = gate name + ROT count + GONE count + document name —
never a revision hash. So the same drift measured on several landings (or on every tick of
the merge sweep) is **one** row; a repair that changes the population is a *different*
defect and earns its own row; a re-measurement of the SAME state is the same one. The
summary is cut to `taskSummaryMaxRunes` with the SAME value used for the probe and the
submission (a summary longer than the ledger keeps is truncated on write, so an unbounded
probe would never match and every landing would file another row).

## Where the shared vocabulary lives (Rules 13/14)

`internal/configanchor` is a small leaf package (constants + reference parsing, no project
imports). The census **generator** now references it for the table path, document name,
generator path, read floor, excerpt width, class names, the anchor pattern and the
separators — so the leg and the generator cannot measure two populations. Re-deriving the
table after the refactor produced byte-identical output (minus the `measured-at` revision,
which moved with the tree).

The table's `measured-at` line moved from `84e9fe1efb18` to `04e595cec546` (the current
master tip) and the class counts re-derived with it — that is the header doing its job
(Rule 38: a census figure names the commit it was measured at).

## The tests (`merge_config_anchor_gate_test.go`)

| arm | what it pins |
|---|---|
| `_RealTreeIsClean` | GREEN on the shipped tree: measured (not abstained), 0 findings, `Scanned >= ReadFloor`, never refused, **no** bug row |
| `_ReportsTheOffendingPathAndLine` | the CONTROL on the materialised copy **before** the poison (so "always reports" cannot pass), then drift injected: the offending `cmd/agent/chat_dump.go:186` is named as ROT, exactly ONE bug row, and a **second** run files none while still measuring |
| `_ARePointedCitationIsAStaleRecord` | the false-positive wall: a hand-re-pointed citation is counted STALE, never a finding, never a row |
| `_AbstainsWithoutATree` | no materialised tree is an abstention, not a pass |

**A minimal fixture is impossible by construction, and that is a pinned property**: the leg
compares only when the table records at least `configanchor.ReadFloor` rows, so a
hand-written two-row fixture makes it abstain. Every red arm therefore carries the real
table, the real document, and materialises a real revision (`git archive` of `HEAD^tree`,
the same materialisation the merge lanes use) with the drift injected **into that copy** —
the repository's own working tree is never a scratch pad (Rule 30).

## The real run (Rule 31) — RED, GREEN, dedup, restore

`bin/gopi` was rebuilt from this branch and `gopi merge` was driven against a **fresh clone**
whose `master` carried the drift, with the census table and CONFIG.md shipped from this
branch (the exact state a landing would see: the CITATION moved, the TABLE frozen).

| direction | observed |
|---|---|
| RED | `CONFIG-ANCHOR gate RED: 1 ROT, 0 GONE: cmd/agent/chat_dump.go:186: recorded "v := strings.TrimSpace(os.Getenv(chatDumpEnv))" … the recorded text is now at line 187 (class ROT)` + `filed bug row: CONFIG-ANCHOR gate: the merge TARGET carries 1 rotted and 0 gone CONFIG.md anchor(s)` + `the landing is NOT held` |
| RED (2nd run, same state) | `the drift was ALREADY a bug row — announced once, not again`; ledger count still **1** |
| GREEN | `PASS for master (the target's CONFIG.md citations still point at the text the census table records: 194 row(s) scanned, 0 ROT, 0 GONE, 0 stale record row(s), 0 unresolved)` and **no** row filed |
| RESTORE | the drift was removed from the clone; the file's blob hash equals `git rev-parse HEAD:cmd/agent/chat_dump.go`; `git diff HEAD -- <path>` **empty**; the clone's `git status --porcelain` empty |

Two pitfalls fixed on the way, recorded because they cost time twice:
**`GOPI_HOME` is a HOME**, not the profile root (`<GOPI_HOME>/.gopi/sessions`, `<GOPI_HOME>/.gopi/memory.db`) —
passing the profile root itself is refused by name; and a fresh clone needs
`user.email`/`user.name` or the merge aborts before the legs run.

## Evidence

`$GOPI_TASK_REPORT_PATH`'s directory — `evidence/rule31.sh` plus `run{1,2,3}-*.{out,err}`
and `ledger-run*.txt`. Nothing in the worktree (Rule 30).
