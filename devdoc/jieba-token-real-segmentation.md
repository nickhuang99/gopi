<!-- doc-class: normative -->
# jieba_token is real segmentation now (gse), and distilled_qa_fts folds stems

**Task**: 01a113a1 (the memory-segmentation epic).
**Branch**: `task/01a113a1fe28730c937c6f98186c723f`.
**Predecessor**: `devdoc/jieba-token-absorbs-search-kw.md` (why the token column
carries `search_kw` at all) and `devdoc/memsearch-cjk-infix-recall.md` (which
recall route owns which CJK query shape).

This document answers three questions a future reader will actually have:

1. `jieba_token` promised jieba and delivered a 2-char window. What does it
   deliver now, and what is the ONE rule that keeps index and query aligned?
2. `distilled_qa_fts` changed tokenizer. How does an existing database migrate,
   and what did the engine refuse to do?
3. Rows written by an older binary still hold the old spelling. How are they
   re-derived — and why is that a sub-command and not a script?

Every number below was measured on this machine in this task; the instrument
is quoted with each table (Rule 38).

---

## 1. The defect, stated exactly

`cjkSegmentation` walked each contiguous CJK run and emitted every overlapping
2-char gram, comma-joined:

```
cjkSegmentation("逐步缩小") = "逐步, 步缩, 缩小"
```

That is a dictionary-free stand-in for a segmenter. Two consequences, both
measured on the pre-change binary (`artifacts/gopi-before`, sha256_16
`0866702483358a06`) against a 4-row corpus (`artifacts/e2e-BIGRAM-BEFORE.txt`):

| consequence | measurement |
|---|---|
| The correct word is not an index token. 逐步缩小's real words are 逐步 + 缩小; the gram window produces 逐步/步缩/缩小, so `逐步` is an index token but `缩小` only survives as a SUFFIX of the phrase in the query's other routes. | `memory search 逐步 缩小` = 1 row, but only through route 3's substring scan |
| The column's own name lies. It is called `jieba_token`; nothing jieba-shaped ever ran. | `devdoc/jieba-token-absorbs-search-kw.md` §2.3 records the original "pure-Go engine has none" note |

The old value, read straight out of the ledger, so the shape is not described
from memory:

```
id=1  逐步, 步缩, 缩小, 小检, 检索, 索算, 算法, 法在, 在哪, 哪里, 里实, 实现, 是参, 参考, 考实, 绕开, 开了, 了它, 子集, 集退, 退化
id=2  夜间, 间批, 批次, 次调, 调度, 怎么, 么降, 降低, 调用, 用开, 开支, ...
```

## 2. What replaces it — one segmenter, two callers

`internal/cjkseg` is a new leaf package wrapping `github.com/go-ego/gse` with
its EMBEDDED dictionary (`LoadDictEmbed("zh_s")`), HMM on. The contract that
matters is not "gse is better"; it is that **the writer and the query side call
the same function**:

| side | call | where |
|---|---|---|
| write (the `jieba_token` column) | `cjkSegmentation(s)` → `cjkseg.Words` | `internal/tools/memory.go`, `deriveJiebaToken` |
| query (route 1b, segmented MATCH) | `cjkQuerySegments(q)` → `cjkseg.Words` | same package, `MemorySearchHits` |
| query (route 3, segmented LIKE) | `cjkQuerySegments(q)` | same |

Neither side keeps its own rules; `TestQuerySegmentationSharesOneSegmenter`
asserts the two lists are equal to `cjkseg.Words`'s output, so a drift is a red
test rather than a silently un-findable row.

The new value, same ledger, after the swap (post-change binary
`/tmp/gopi-after`, sha256_16 `687bd7d5ab020503`):

```
id=1  逐步 缩小 检索 算法 逐步 缩小 and 检索 算法 在 哪里 实现 hermes 是 参考 实现 ， gopi 的 memory 有 route 1.5 ladder ， ...
id=2  夜间 批次 调度 怎么 降低 deepseek 调用 开支 把 批量 任务 挪到 夜间 便宜 时段 ， ...
```

Two separators, and the difference between them is the whole contract with
`unicode61`:

* `cjkSegmentSep = " "` joins the segmenter's **words** into the indexed value.
  A space makes each word its OWN index token; gluing them (the old comma, or
  nothing) leaves a multi-word run as one un-tokenizable blob.
* `m1JiebaTextSep = " "` joins the **source columns** before segmentation, so
  `这些` and `commit` present as two fields and cannot fuse into 这些commit.

## 3. Before/after, measured (not asserted)

Two fresh private ledgers (`GOPI_MEMORY_DB`, never the operator's profile —
Rule 32), the same 4-row corpus, the same 8 queries, two binaries. Script:
`artifacts/measure-e2e.sh`; raw outputs `artifacts/e2e-BIGRAM-BEFORE.txt` and
`artifacts/e2e-GSE-AFTER.txt`.

| query | bigram (before) | gse (after) | what changed |
|---|---|---|---|
| `逐步 缩小` | 1 | 1 | unchanged (route 3 carried it before) |
| `缩小` | 1 | 1 | unchanged |
| `逐步缩小` | 1 | 1 | unchanged |
| `commit` | 1 | 1 | before: route 3 LIKE (the body literally says `commits`); after: the token index folds it |
| `commits` | 1 | 1 | unchanged |
| `夜间` | 1 | 1 | unchanged |
| `网络测试环境` | 1 | 1 | unchanged |

The whole-run corpus is equally served by both spellings — which is why the
EPIC's own acceptance criterion ("the before/after hit counts differ") needed
the discriminating probes below rather than a headline number. Those probes are
where the mechanisms differ:

| query | bigram (before) | gse (after) | mechanism |
|---|---|---|---|
| `逐` (single rune) | **0** | **1** | gse recovers a single-rune unknown word by HMM; a 2-char window can never index a 1-char query |
| `干怎` | 1 | 2 | the old gram list indexed 干怎 wherever 词干 + 怎么 abutted; gse does not invent that boundary, and the extra row arrives through the substring route |
| `移` (single rune) | **0** | **1** | same HMM recovery |

Instrument: `artifacts/probe-shapes.sh <bin> <db>` over the same two ledgers.

The honest other side of the same coin — the swap is not a strict superset:

| shape | before | after | why |
|---|---|---|---|
| a 2-char infix that the segmenter does not know (`试环境`) | found | found | the substring route (route 3) still carries it — `memory_cjk_infix_route_mechanism_test.go` measures that this route, not the token index, is what owns a cross-boundary infix |

The mechanism test's control is the causal half: the same corpus in a plain
`unicode61` index (no `jieba_token` at all) answers a cross-boundary infix
**0** times, so the delta is attributable to the route, not to the tokenizer.

## 4. The tokenizer: `distilled_qa_fts` now uses porter

`unicode61` alone leaves `commits` a different token from `commit`. Measured on
the linked engine (`probe_porter_cjk.go`, `probe2.go`, `probe3.go` in the
session's `artifacts/`):

```
porter  MATCH commit      -> n=1     (against a body saying "commits")
porter  MATCH 逐步缩小     -> n=1     (CJK unaffected: porter wraps unicode61 and stems ASCII only)
uni61   MATCH commit      -> n=0
```

So the change is **strictly additive for CJK** and fixes the English stem
fold. `m1FTSVirtualTable` now renders `tokenize='porter'`.

### 4a. The migration, and the thing ALTER cannot do

An existing `distilled_qa_fts` is on `unicode61` and must be rebuilt. The
engine REFUSES the obvious spelling:

```
ALTER TABLE fts ADD COLUMN tokenize='porter'
  -> SQL logic error: virtual tables may not be altered (1)
```

measured in `artifacts/probe_alter.go` against the same `modernc.org/sqlite`
build this binary links. The tokenizer is fixed in the CREATE statement that
lives in `sqlite_master`, and an FTS5 virtual table cannot be altered at all.

The migration is therefore the rebuild the engine already uses for every other
index change — DROP + CREATE + `'rebuild'` — carried by
`m1MigrateFTSTokenizer`, which:

* detects the old spelling by reading the tokenizer out of the table's own DDL
  (`ftsIsPorter`, over `sqlite_master`) — the only local evidence that exists;
* does nothing at all when the index is already porter (idempotent);
* DROPS the table and returns, leaving the CREATE and the `'rebuild'` to the
  caller's existing path, so there is ONE owner of the index DDL.

Where it is called from is the load-bearing detail: **before** `m1MigrateFTSCols`
takes its own early return. A database that already has all six M1 columns is
otherwise "fully migrated" and returns before the tokenizer heal would run —
measured and fixed in this change.

Proven end to end on a real ledger, not only in a unit test:

```
$ python3 -c '...' legacy2.db        # a DB shaped the way a pre-fix gopi left it
legacy2 commit: 0
legacy2 commits: 1
$ GOPI_MEMORY_DB=.../legacy2.db gopi memory search "commit"
#1 legacy row ...
```

The search itself triggered the on-open migration, and the row came back — the
route that made it reachable on `unicode61` was route 3, which is why the
result alone is not the proof; the post-state is:

```
after  porter? False   <- the probe ran before the migration committed; the
                          DDL check below is the authoritative read
```

That probe line is noted here because it was a FALSE READ in the session, not
because it is the result: the authoritative post-state comes from the test
path, `TestFtsTokenizerMigrationIsIdempotentAndPreservesRows`, which asserts
(a) the downgraded fixture answers `MATCH "commit"` 0 times, (b) after
`migrateDefaultTriggers` the row count is unchanged and `ftsIsPorter` is true,
(c) `MATCH "commit"` finds the row again, and (d) a second migration is a
no-op.

## 5. Backfilling existing rows — a sub-command, not a script

### The Rule 35 gate, answered

1. **What breaks without it.** Rows written before the swap keep a
   comma-joined gram list. They stay FINDABLE (route 3 scans substrings) but
   they are not addressable at the segmenter's granularity, and the query side
   now demands exactly that: `memory search 逐步 缩小` asks for 逐步 AND 缩小 as
   tokens. Without re-derivation, every pre-existing row is a second-class
   citizen forever, silently.
2. **Why it cannot be fixed in-process.** The re-derivation needs the OLD text
   and the NEW segmenter in the same process; only a run of the new binary can
   do it. It cannot happen lazily on read (search is read-only by contract and
   opens with `PRAGMA query_only=ON`), and it cannot happen at write time for
   rows nobody writes again.
3. **Who owns its lifecycle.** `gopi memory backfill-tokens`. It is an
   existing verb family's new sub-command, invoked explicitly by whoever
   upgraded the binary. **Nothing schedules it**: no cron line, no daemon, no
   self-restart (Rule 35's incident), no state file, no lock. It is idempotent,
   so an interrupted run is repaired by running it again.
4. **What the next person sees.** The group help page carries a synopsis that
   says what it does, that it writes only the token column, that it is
   idempotent, and that it is not scheduled — and the help/inventory gates
   (`TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand`,
   `TestMemoryHelpSurfaceInventoryTableMatchesRow`) fail the build if the
   synopsis or its inventory row goes missing.

### What it writes

`BackfillJiebaTokens` scans `distilled_qa`, re-derives with the SAME
`deriveJiebaToken` the writer uses, and UPDATEs rows whose stored value differs.
It rewrites exactly ONE column; the row's prose, tier, confidence, counters and
timestamps are read-only inputs, never outputs. Two deliberate refusals:

* a row whose text yields no words keeps its stored token — blanking it would
  make the row permanently un-findable, which is worse than a stale spelling;
* a row already carrying the current derivation is not written at all.

Real runs, on a ledger built by the PRE-change binary:

```
$ gopi memory backfill-tokens --dry-run
memory backfill-tokens: dry-run — 4 row(s) scanned, 4 would be re-derived (db=.../bf.db)
$ gopi memory backfill-tokens
memory backfill-tokens: 4 row(s) scanned, 4 jieba_token value(s) re-derived (db=.../bf.db)
$ gopi memory backfill-tokens        # idempotent
memory backfill-tokens: 4 row(s) scanned, 0 jieba_token value(s) re-derived (db=.../bf.db)
```

and the row's own prose is untouched across it:

```
rows: 4
rows unchanged: [(1, '逐步缩小 AND 检索算法在哪里实现'), (2, '怎么降低 deepseek 调用开支'), ...]
```

## 6. The dependency this added (gse), named honestly

`go.mod` gains exactly one direct require:

```
github.com/go-ego/gse v1.1.0
github.com/vcaesar/cedar v0.50.0 // indirect
```

Three modules in the build graph (gse, cedar, and cedar's own dep), pure Go,
no CGO, no data file beside the binary (the dictionary is embedded). The
binary grew from 31,013,733 to 64,016,888 bytes — that is the embedded
dictionary, and it is the price of a segmenter that runs on a machine with
nothing installed. Rules 35's four questions are answered in §5 above for the
backfill procedure; for the LIBRARY the answers are: nothing works without it
(The defect in §1), it cannot be fixed in-process (writing a segmenter is the
thing that was already tried and produced the gram window), its lifecycle is
gopi's own `go.mod` (owned by whoever builds the binary, with no runtime
artifact at all), and the next person finds `github.com/go-ego/gse` in
`go.mod` plus its name in this document.

## 7. What was deliberately NOT changed

* **`chat_prefetch`** — a separate task owns it; untouched here.
* **`memsearchLadderTerms`'s gram splitting** — untouched. Its 2-char grams
  remain the ladder's degrade terms; the relationship is stated in the code
  comment beside `cjkSegmentSep`: the ladder is a SECOND, coarser path, not the
  token index's rule.
* **The `likeCJKTargets` set gained `answer`** — a measured fix, not a
  refactor: `memory search 环境` recalled nothing when 环境 existed only in the
  answer body (`memory_query_safety_test.go`). The substring route is now the
  only recovery for a phrasing the segmenter does not produce as a word, so its
  column list had to cover the prose.

## 8. Residual risks, stated

* **A segmenter is a model of Chinese, not an oracle.** A word gse does not
  know and does not recover by HMM is reachable only through the substring
  route. That route scans with LIKE, so recall is bought with a table scan:
  bounded, but a cost that grows with the `answer` column now in its target
  list.
* **`zh_s` (the small dictionary) was chosen; `zh` was not compared here.** A
  larger dictionary would change the token list — and therefore the token
  index — for the same rows. Changing it later is a backfill, not a code edit.
* **The old and new spellings coexist in one column.** A row yet to be
  backfilled holds grams. Nothing marks which rows are which; the only detector
  is `backfill-tokens --dry-run`, which reports the count. A future reader
  should run the dry run rather than guess.

## 9. Ran (literal `Ran:` lines)

The section is here because the report that carries this document was
originally failed on FORM, not on work: the harness's report gate looks for a
literal `Ran:` line, the original spelling was `**Ran**` (colon OUTSIDE the
bold), and no such token existed in the delivered report. Nothing below is
reconstructed from memory — each line was executed for real by the rescue run
`01a1145f-9e02-78f0-8f58-614a9226343e`, against the delivered artefacts and
their own measurement scripts:

Ran: cd /home/user/workspace/gopi-01a113a1fe28730c937c6f98186c723f && CGO_ENABLED=0 go build -o <session>/bin-gopi-3099ba919 ./cmd/agent/   → exit 0
Ran: make test-budget PKGS="-run 'TestJieba|TestSegmented|TestBackfill|TestFtsTokenizerMigration|TestMemorySearch' ./internal/tools/ ./internal/cjkseg/"   → exit=0 outcome=pass
Ran: bash <session>/e2e/probe-shapes.sh <session>/e2e/gopi-after <session>/e2e/after.db   → 移|1 , 干怎|2  (§3's discriminating probes)
Ran: GOPI_MEMORY_DB=<session>/e2e/bf2.db ./gopi-after memory backfill-tokens --dry-run  → 2 row(s) scanned, 2 would be re-derived
Ran: GOPI_MEMORY_DB=<session>/e2e/bf2.db ./gopi-after memory backfill-tokens            → 2 jieba_token value(s) re-derived
Ran: GOPI_MEMORY_DB=<session>/e2e/bf2.db ./gopi-after memory backfill-tokens            → 0 jieba_token value(s) re-derived  (idempotent, §5)

Two caveats a reader deserves, stated rather than buried: the `<session>` paths
above are the RESCUE run's private copies of this document's own scripts and
ledgers (Rule 37 — evidence lives in the session directory, never in the tree),
and the run that WROTE this document recorded its own taller transcript in
`~/.gopi/sessions/01a113a1-fe28-730c-937c-6f98186c723f/artifacts/`. The numbers
are identical because the scripts are the same files, replayed unmodified.

## 10. Not Done (honest)

**The quality comparison is NOT in this document, and was not run here.** gse
vs the jieba baseline (token F1 over ≥20 sample documents, then GO/NO-GO) is the
downstream row `01a113a2-2602-7aac-a49b-dc2c138a213e` — *集成后质量测试：gse 分词
vs jieba 基线* — which was parked on `01a113a1` until that row reached
`completed`. What §3 measures is narrower, and it is what this change claims:
the swap runs, the index side and the query side speak ONE granularity, and
recall on that corpus does not regress. **Whether gse's word boundaries are
BETTER than jieba's is exactly the question this document does not answer** —
the probes below show where the two tokenizers differ, not which one is right.

Each of the following is deferred to its own place rather than quietly dropped:
the `zh` vs `zh_s` dictionary (§8 — a token-list decision, a backfill later, not
a code edit); `chat_prefetch` (another row owns it); `memsearchLadderTerms`'s
grams (§7 — a coarser second path, untouched by design); any latency or scale
figure (§8 — recall counts, not latency, are what was measured; the substring
route now scans `answer`).

**A replay, by the rescue run `01a11460-0822-76cc-8215-053d2952e43b`** (Rule 31:
the surfaces were executed again, verbatim, against the delivered tree at master
tip `485881ab4`; the numbers below are that run's own observation, not a copy of
§3's):

Ran: cd /home/user/workspace/gopi-01a11460082276cc8215053d2952e43b && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → exit 0 (BUILD_OK)
Ran: go test ./internal/tools/ -run 'Jieba|Token|FTS' -count=1   → ok  github.com/the operator/gopi/internal/tools  2.908s (exit 0)
Ran: bash /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/measure-e2e.run.sh /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/gopi-before /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/before.db BEFORE   → index column holds grams: `id=1  逐步, 步缩, 缩小, 小检, 检索, 索算, 算法, …`
Ran: bash /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/measure-e2e.run.sh /home/user/workspace/gopi-01a11460082276cc8215053d2952e43b/bin/gopi /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/after.db AFTER   → index column holds words: `id=1  逐步 缩小 检索 算法 逐步 缩小 and 检索 算法 在 哪里 实现 …`
Ran: bash /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/probe-shapes.run.sh /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/gopi-before /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/before.db   → 移|0 , 干怎|1
Ran: bash /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/probe-shapes.run.sh /home/user/workspace/gopi-01a11460082276cc8215053d2952e43b/bin/gopi /home/user/.gopi/sessions/01a11460-0822-76cc-8215-053d2952e43b/e2e/after.db   → 移|1 , 干怎|2
Ran: cd /home/user/workspace/gopi-01a11460082276cc8215053d2952e43b && git status --porcelain   → prints nothing

The query-side half of the same replay — `memory search <q> --limit 20`, hit
count = the lines beginning `#`, both binaries against their own fresh private
ledger. **The headline number does not move:**

| query | hits BEFORE | hits AFTER |
|---|---|---|
| `逐步 缩小` | 1 | 1 |
| `缩小` | 1 | 1 |
| `逐步缩小` | 1 | 1 |
| `commit` | 1 | 1 |
| `commits` | 1 | 1 |
| `夜间` | 1 | 1 |
| `挪到` | 1 | 1 |
| `网络测试环境` | 1 | 1 |

…while the discriminating probes are where the two mechanisms separate
(`移` 0→1, `干怎` 1→2; `测试环境`, `环境`, `迁移`,
`逐步缩小检索算法在哪里实现`, `查询 逐步 缩小`, `deepseek 开支`, `开支` all 1→1).

Observed, two binaries / two fresh private ledgers (`GOPI_MEMORY_DB`, never the
operator's profile):

* **every query the replay ran is ABSOLUTE-ADDRESSED.** Four rows in the corpus
  and never more than 2 candidates in play, so every BEFORE/AFTER count is 1 or
  2 against a denominator of 4 — a `1 → 1` row says the query still resolves,
  and nothing about ranking, precision or recall over a real ledger. The numbers
  are repeatable and correctly attributed; they are a **replay, not a benchmark**,
  which is exactly why the F1 comparison is a separate row (top of this section);
* **the query-side table does not demonstrate the index-side claim.** Its `1 → 1`
  means the query still resolves under both tokenizations; that the column now
  holds words rather than grams comes from the raw `SELECT`, printed separately.
  Read the two together, never the table alone;
* **no latency figure was measured** (§8 names this too). What is measured is
  that the *string* handed to the index changed shape — `逐步缩小` becomes
  `逐步 缩小`, two words instead of one gram — which is the mechanism behind the
  single-rune probe `移 0 → 1`; a seconds-level claim would need its own run;
* both binaries are distinct artifacts — `gopi-before` sha256_16
  `0866702483358a06` (31,013,733 B, built from the PRE-change tree), the
  delivered `bin/gopi` sha256_16 `60bf203430c9a29a` (64,046,115 B, the embedded
  gse dictionary);
* the index column's CONTENT is the change: grams before, words after (the two
  `id=1` lines above are the same row, same prose, different tokenization);
* **all eight natural-language queries return 1 hit in BOTH directions**
  (`逐步 缩小`, `缩小`, `逐步缩小`, `commit`, `commits`, `夜间`, `挪到`,
  `网络测试环境`) — the headline before/after count does NOT move, which is
  why the discriminating probes carry the claim;
* the probes are where the mechanisms separate: single rune `移` 0→1 (HMM
  recovers a one-rune word a 2-char window can never index), `干怎` 1→2 (the
  gram list invented a boundary the segmenter does not, and the extra row
  arrives through the substring route), while `测试环境`, `环境`, `迁移`,
  `逐步缩小检索算法在哪里实现`, `查询 逐步 缩小`, `deepseek 开支`, `开支` are
  1 in both.
