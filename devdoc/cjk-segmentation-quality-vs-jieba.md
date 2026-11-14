<!-- doc-class: normative -->
# gse vs Python jieba — the integrated segmenter measured on real corpus

**Task**: 01a113a2 (post-integration QUALITY verification).
**Branch**: `task/01a113a226027aaca49bdc2c138a213e`.
**Predecessor**: `devdoc/jieba-token-real-segmentation.md` — task 01a113a1 built the
integration (commit `485881ab4`: `internal/cjkseg` wrapping `go-ego/gse`
`LoadDictEmbed("zh_s")`, feeding the `jieba_token` column). **This document does not
redo that work.** It answers one question: *is the integrated segmenter good enough,
measured against Python jieba on real corpus rows?*

Every number below names the instrument that produced it (Rule 38); the
reproduction script is `run-all.sh` (§9) and the corpus is a fixed file (§2).

---

## 1. Verdict, up front

**GO — conditional.** Two conditions are named in §7, and one literal reading of the
acceptance criterion FAILS. Both readings are given so the operator can choose which
one binds:

| reading of "token F1 ≥ 0.95" | measured | verdict |
|---|---|---|
| token F1, case-sensitive (strict) | **0.8873** | **FAILS** |
| token F1, case-folded | **0.9577** | passes |
| token F1, case-folded + punctuation-normalized | **0.9893** | passes |

The gap between the first two rows is **entirely ASCII case** — a deliberate gse
policy (`ToLower = true`, §5), not a segmentation error: **not one line in the
corpus contains a lexical divergence** (§4: `char_diff = 0`). The gap between the
second and third is punctuation *attachment* (a colon glued to the closing bracket),
which no search index tokenizes meaningfully.

On the question the criterion is actually trying to ask — *does gse degrade the
CJK word boundaries jieba gets right?* — the answer is measured and it is **no**:
gse destroyed **1** dictionary-word boundary where jieba destroyed **74** (§6).

---

## 2. Corpus — the fixed input

| item | value |
|---|---|
| source | `~/.gopi/memory.db`, table `distilled_qa` — the operator's live ledger |
| opened | `file:<path>?mode=ro&_pragma=query_only(true)` (Rule 32) — **never written** |
| table size | **8401** rows |
| eligibility | `length(question) >= 12` AND `length(answer) >= 24` (MySQL `length()` = bytes) |
| sample | **systematic**: every 35th eligible row by `id`, capped at 240 rows |
| unit compared | the **first line** of each of `topic` / `question` / `answer` → **720 lines** |
| fixed corpus file | `.../artifacts/corpus.jsonl` (591,648 B) |

Why systematic and not `LIMIT 240`: `id` is chronological, so a `LIMIT` samples one
era of the ledger. Why first-line: answers are multi-paragraph, and comparing whole
documents measures the two segmenters' handling of *length*, not of word boundaries.

**Category precedence** is `digit > mixed > cjk > latin > other`, computed by one
function both producers run (`catLine` in `cmd/agent/main.go`, `cat_line` in `jieba_seg.py`), so
the two sides can never disagree about which bucket a line is in. The precedence is
why the `cjk` bucket is small (19 lines): any line carrying a digit is classified
before CJK is considered.

---

## 3. Token-level F1 and line agreement

Micro-averaged P/R/F1 per line, then averaged over the lines of each scope. `exact_*`
is the **sentence-level exact agreement rate** — the fraction of lines whose token
sequence matches with no tolerance at all. Instrument: `artifacts/segeval/score.py`
over `gse-out.jsonl` and `jieba-out.jsonl`.

| scope | lines | strict F1 | fold F1 | nopunct F1 | exact strict | exact fold | exact nopunct | tok gse | tok jieba |
|---|---|---|---|---|---|---|---|---|---|
| **ALL** | 720 | 0.8873 | **0.9577** | **0.9893** | 0.1319 | 0.4986 | 0.8472 | 67.1 | 70.4 |
| mixed (中英混排) | 144 | 0.9082 | 0.9479 | 0.9719 | 0.2569 | 0.4861 | 0.6806 | 27.8 | 28.8 |
| digit (含数字) | 285 | 0.8686 | 0.9389 | 0.9888 | 0.0842 | 0.2632 | 0.7860 | 126.1 | 133.3 |
| latin (纯 ASCII) | 272 | 0.8912 | 0.9830 | **1.0000** | 0.0809 | 0.7426 | **1.0000** | 30.0 | 30.5 |
| cjk (纯中文) | 19 | 0.9513 | 0.9513 | 0.9763 | 0.6316 | 0.6316 | 0.8421 | 12.2 | 12.5 |

*(target: `artifacts/score.json`; raw token streams: `artifacts/gse-out.jsonl`,
`artifacts/jieba-out.jsonl`)*

Three things this table says that a single headline number would hide:

1. **`latin` is a total agreement** once punctuation is ignored (F1 = 1.0000, exact
   = 1.0000). The two sides tokenize ASCII identically; every `latin` disagreement is
   case or punctuation.
2. **`digit` is the weakest scope on the raw metric** (fold F1 0.9389) and the
   *strongest* once punctuation is normalized (0.9888). A line full of digits,
   slashes, `%`, `──` and `()` disagreement is an attachment question, not a word
   question.
3. **`mixed` is the weakest scope on the normalized metric** (0.9719) — the genuinely
   hardest class, and the one the acceptance criterion names. It is still above 0.95.

Sentence-level exact agreement is the harshest reading and is reported because it is
the number a skeptic should look at: **13.2%** of lines match token-for-token with no
tolerance, and **84.7%** match once case and punctuation are normalized.

### 3a. Sentence-level exact agreement, by normalized class

| normalization | ALL | note |
|---|---|---|
| none | 0.1319 | every ASCII letter's case and every punctuation attachment must match |
| case-folded | 0.4986 | half the corpus is case-only from jieba |
| case + punctuation | 0.8472 | the operative reading: how often the two agree on WORDS |

---

## 4. Why the lines disagree — classified, not guessed

Each mismatching line is classed by mechanism, so no class can quietly absorb
another. Instrument: `score.py` (`classify`), which uses a content projection that
strips punctuation **and numbers** — because `87%）！` vs `87% ）！` and `1m` vs `1M`
are attachment/format questions, and lumping them with a lexical difference would
overstate the divergence. `②` is Unicode category `No`, not punctuation, so a
punctuation-only test alone would have mis-filed it as a lexical difference.

| class | lines | mechanism |
|---|---|---|
| `case_only` | **264** | identical once ASCII case is folded — gse's `ToLower` policy |
| `punct_only` | **260** | identical content words; punctuation/number **attachment** differs |
| `boundary` | **101** | identical character stream; the word **cuts** differ |
| `char_diff` | **0** | a real lexical divergence — a rune one side emitted and the other lost |

**`char_diff = 0` is the headline.** Across 720 real lines the two segmenters never
disagreed about *which characters are there* — 625 lines disagree and every one of
them is case, attachment, or boundary. There is **no line where one side invented or
dropped lexical content.**

Total mismatching lines: **625 / 720** (86.8%). That number looks alarming and is
mostly meaningless: 264 of those 625 differ *only* by ASCII case, and 260 more *only*
by punctuation attachment.

---

## 5. Case differences — the 264 lines, counted separately

The briefing names this class explicitly (`and` vs `AND`), so it is measured on its own.

| measurement | value |
|---|---|
| lines where some token differs from its counterpart **only** by ASCII case | **568 / 720 (78.9%)** |
| lines differing by case *alone* (whole line) | **264 / 720 (36.7%)** |
| case-only token pairs (multiset) | **3531** |

Top pairs (gse → jieba), by frequency:

| pair | n | | pair | n |
|---|---|---|---|---|
| `i` → `I` | 144 | | `the` → `The` | 32 |
| `a` → `A` | 96 | | `cli` → `CLI` | 31 |
| `how` → `How` | 66 | | `llm` → `LLM` | 24 |
| `not` → `NOT` | 63 | | `one` → `ONE` | 22 |
| `rule` → `Rule` | 54 | | `x` → `X` | 20 |
| `gopi` → `GOPI` | 53 | | `head` → `HEAD` | 35 |
| `gopi` → `GoPi` | 51 | | `n` → `N` | 35 |

**Root cause, read from the dependency's source, not inferred:**
`github.com/go-ego/gse@v1.1.0/dict_util.go:36` declares

```go
var ( ToLower = true )   // ToLower set alpha to lowercase
```

a **package-level global defaulting to true**, applied by `toLow`
(`segmenter.go:260`) and consumed in the DAG paths (`dag.go:89,227,278,319`).
`gopi` never sets it, so gse lowercases every ASCII run before matching its
dictionary — **that is why `GoPi`, `CLI` and `HEAD` all come back lowercase.**

**Assessment.** For the search index this is *correct and desirable*: FTS5's
`unicode61` is already case-insensitive, so lowercasing changes no match, and it makes
the index uniform. It is **not** a fidelity loss for retrieval. It *is* a fidelity loss
for anything that reads the column for display — and it is the sole cause of the
strict-vs-folded gap in §1.

---

## 6. Who is actually right — cut direction

Token F1 says the sides disagree. It cannot say which one is *right*, and for a CJK
index that is the whole question: an index token that is half a word is a word the
index can never answer. Instrument: `artifacts/segeval/cut_direction.py`.

**One authority for both directions** — jieba's own dictionary, read from
`jieba/dict.txt`: **348,974 CJK words**. Using each side's own dictionary would let
the weaker dictionary excuse its own bad cuts. A window of 3 adjacent tokens bounds
both tests, so a word the two sides merely *order* differently can never be counted.

| measurement | gse | jieba |
|---|---|---|
| dictionary word emitted as **one token** ("safe boundary") | **4327** | 4239 |
| dictionary word **split** across ≥2 adjacent tokens | **1** | **74** |
| dictionary word swallowed inside a longer token | 73 | 107 |
| lines containing a split | **1** | **50** |

**Split ratio jieba/gse = 74.00** — gse preserves 74 dictionary-word boundaries for
every one it destroys. `whole_gse` (4327) also exceeds `whole_jieba` (4239).

### 6a. The minimal sample that isolates the gse failure mode (Rule 10)

The one gse split in the corpus is `解决方案`, which `zh_s` cuts `解决`/`方案`.
A hand-built minimal probe (`artifacts/probe-corpus.jsonl`; both sides in
`artifacts/probe-gse.jsonl` and `artifacts/probe-jieba.jsonl`):

```
            text          gse                     jieba
**  不依赖 python       不依/赖/python            不/依赖/python
**  先验证 hash 存在性    先验/证/hash/存在/性        先/验证/hash/存在/性
**  不调用 dispatcher   不调/用/dispatcher        不/调用/dispatcher
**  时内网域名解析        时/内网/域名解析            时内网/域名解析
**  解决方案             解决/方案                 解决方案
   不依赖               不/依赖                   不/依赖        ← correct alone
   已修正               已/修正                   已/修正        ← correct alone
   先验证               先/验证                   先/验证        ← correct alone
```

The first three rows are the same defect in three costumes: **a CJK run is parsed
as `不依`+`赖` when an ASCII word follows it, and parses correctly when it does not.**
`不依赖` alone is `不/依赖` on *both* sides. So gse's character handling near a CJK↔ASCII
boundary is the mechanism, and it is **not** a dictionary gap for these words.

The last two rows are dictionary gaps: `解决方案` and `三节点` are absent from
`zh_s` (gse `解决/方案`, `三节/点`; jieba keeps both whole).

### 6b. Honest limits of this measurement

* The authority is jieba's dictionary, so **a word only gse's dictionary knows is not
  credited** — `whole_gse` 4327 vs `whole_jieba` 4239 is measured against the *other*
  side's word list, which is the conservative direction for gse.
* `glued` (gse 73, jieba 107) is a coarser-granularity signal, not a boundary
  destruction; the two are reported separately and **never summed**.
* The count is over first lines only (§2).

---

## 7. GO / NO-GO

### The criterion, applied literally

| criterion as written | measured | verdict |
|---|---|---|
| token F1 ≥ 0.95 | strict 0.8873 / folded 0.9577 | **FAILS strict, passes folded** |
| 技术术语/中英混排 not worse than jieba | mixed fold 0.9479 (below 0.95); direction test: gse 1 split vs jieba 74 | **fails the F1 bar, passes the direction test** |
| `CGO_ENABLED=0` build passes | **passes**, exit 0, both trees (§8) | **GO** |
| binary-size risk judged | **+31.52 MiB** (+106.6%) (§8a) | below the +50 MiB flag line, still a material risk |

### Verdict

**GO — conditional**, on these two conditions:

1. **The criterion must be read case-folded.** `0.9577 ≥ 0.95` holds once ASCII case
   is folded, and §5 shows the entire gap is gse's `ToLower` policy — which the FTS5
   index it feeds is insensitive to anyway. **If the operator insists on the literal
   case-sensitive reading, the verdict is NO-GO** and §7a's fallbacks apply.
2. **The +31.52 MiB binary growth is accepted knowingly.** It is below the briefing's
   ≥+50 MiB flag line, but it is a 106.6% growth and it is the single largest cost of
   this integration.

Supporting the GO: `char_diff = 0` (§4), the direction test (§6), `latin` F1 = 1.0000
under normalization (§3), and the mixed-class normalized F1 of 0.9719 — above the bar
on the operative reading.

### Two findings that are NOT about quality, and should be filed separately

**(a) `memory store` and `backfill-tokens` disagree about `jieba_token`.** Measured on
a private sandbox ledger (`gopi tool sandbox`, Rule 34):

```
memory store --keywords "zzuniquetok9,placeholder"
BEFORE token: … 检查 keywords : zzuniquetok9 , placeholder zzuniquetok9 , placeholder
                          ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ twice — once from the answer tail, once from search_kw
memory backfill-tokens          → 1 row scanned, 1 re-derived
AFTER  token: … 检查 keywords : zzuniquetok9 , placeholder
                          ^^^^^^^^^^^^^^^^^^^^^^ once
```

On **8 freshly stored rows**, `memory backfill-tokens --dry-run` immediately reported
**8 would be re-derived**, and a real run then reported 0 (idempotent). On the
operator's own 8401-row ledger the same dry run reports the pre-existing population
too, so the two cohorts are indistinguishable from the counter alone.

**Recall impact is masked**, and this is measured rather than assumed: a
search_kw-only term (`zzuniquetok9`, absent from topic/question/answer) was found both
**before** and **after** the backfill — route 3's `LIKE` substring scan carries it
(§7 of the predecessor devdoc). So this is **an index-column inconsistency, not a
measured recall loss**, and it is reported as a defect to fix rather than a NO-GO.

**(b) `-tags ne` is a trap, and must never be used to reclaim the 31.5 MiB.** Under
`-tags ne` gse's `//go:embed` directives are dropped
(`dict_no_embed.go`: `//go:build ne`), and the build succeeds — but the segmenter
**silently degrades to a dictionary-less HMM pass**:

```
plain 不依赖 python   → 不依/赖/python        -tags ne → 不依赖/python   (better here)
plain 已修正          → 已/修正               -tags ne → 已修/正         (worse)
plain 三节点          → 三/节点               -tags ne → 三节/点         (worse)
plain 不调用          → 不/调用               -tags ne → 不调/用         (worse)
```

11 of 51 probe lines change, and `cjkseg.DictName()` still reports `zh_s` — **the
degradation is invisible from the code**. Saving 32.36 MiB this way would trade a real
dictionary for a coin flip. (Evidence: `artifacts/probe-gse-ne.jsonl` vs
`artifacts/probe-gse.jsonl`.)

### 7a. Fallback routes (required if the strict reading binds)

In the order I would take them, with what each costs:

1. **Fix the criterion's definition, not the code.** Add case-folding to the
   comparison and record the strict number beside it. Cost: zero code. This is the
   route §6 supports — the direction test says the segmenter is fine.
2. **Pre-segment with Python jieba and write the column.** The pattern hermes-agent
   already uses. Cost: **a Python runtime and a jieba install on every machine that
   runs gopi** — it breaks the one-binary property (Rule 35) and cannot be the
   default; it is the right answer only if a deployment already has Python.
3. **Roll back to 2-char grams.** Cost: re-introduces the defect the predecessor task
   fixed (no dictionary, no HMM, a single-rune query can never match). The
   predecessor's measurement stands: single-rune recall goes 1 → 0.
4. **A different pure-Go CJK library** (`sukitss/thai-nlp-go/cjk`, `huichen/sego`).
   Cost: a re-run of the predecessor's selection work (dependency tree, size,
   licence) plus this entire measurement, against a library the survey already ranked
   below gse. Not justified by anything measured here.

Rollback (route 3) is the *worst* of the four on the evidence: it is the only route
that makes measured recall worse.

---

## 8. Build and size (CGO_ENABLED=0)

Instrument: `artifacts/segeval/size-build.sh`, log `artifacts/size-build.log`.
Baseline = the master tip **immediately before** the gse merge, extracted with
`git archive` into a `mktemp -d` scratch (Rule 22/29 — never a second worktree). Its
identity as the pre-gse tree is **verified, not assumed**: its `go.mod` contains
**0** matches for `go-ego/gse`.

```
baseline commit: 4d9ac2acf   gse requires in baseline go.mod: 0
scratch=/home/user/workspace/_tmp/tmp.U9mDeMLJ2q
scratch_device=2052  root_device=2051        ← scratch is OFF the / partition (Rule 22)
go version go1.24.5 linux/amd64
```

| build | bytes | MiB |
|---|---|---|
| baseline `4d9ac2acf` | 30,994,856 | **29.56** |
| HEAD (gse integrated) | 64,046,075 | **61.08** |
| **delta** | **+33,051,219** | **+31.52** |

Stripped reference (`-ldflags="-s -w"`, same two trees): 21,561,528 → 54,517,944 =
**+31.43 MiB**. Stripping removes ~6 MB from each tree but **does not touch the
delta** — as expected, since the delta is embedded dictionary DATA, not code.

`CGO_ENABLED=0 go build ./cmd/agent/` — **exit 0 on both trees** (this is the repo's
declared build, `Makefile:22` and the `repos` table in `CONFIG.md`).

**Risk judgement.** +31.52 MiB is **below** the briefing's ≥+50 MiB must-flag line, so
it is not flagged by that rule. It is still a **106.6% growth** (29.56 → 61.08 MiB),
and it is 100% the embedded `zh_s` dictionary. The mitigation is deliberately NOT
`-tags ne` (§7b): `gse` has **no serialization API**, so the expanded trie cannot be
cached beside the binary either. The honest statement is: the cost is paid once per
machine, buys a zero-dependency segmenter, and is not reducible without losing the
dictionary.

*Instrument note (honesty):* the baseline binary's `sha256_16` differed between the
two runs (`daf06d04705a5b06`, then `bdb7ac1838e5751a`) because an untrimmed build
embeds the scratch path; the **sizes were byte-identical** across both runs
(30,994,856 / 64,046,075), which is the measurement that matters. HEAD's hash was
stable (`2414391adf154289`) because it always builds from the same worktree.

---

## 9. Throughput — CPU time, not wall clock

Instrument: `segeval bench` (Go, in-process, `getrusage(RUSAGE_SELF)`) and
`jieba_seg.py --bench` (`time.process_time`). Both: **one warm call first**, whose
cost is reported as the dictionary load, then **5 full passes** over the same 720
lines / 184,376 runes. Rule 27: no load is generated and nothing is saturated — the
measurement is the same corpus the quality section uses, run normally.

Measured runs (loop CPU ms, 5 passes):

| side | runs | median |
|---|---|---|
| gse | 5205.8, 4972.2, 5311.2, 5159.1, 5020.6 (+6868.6 in the replay run) | **5159.1** |
| jieba | 2929.9, 1915.3, 2054.7, 2810.9, 2305.7 (+3327.5 in the replay run) | **2305.7** |

| metric | gse | jieba | ratio |
|---|---|---|---|
| loop CPU, 5 passes (median) | 5159.1 ms | 2305.7 ms | **2.24×** |
| throughput derived from the median CPU | **≈178,700 runes/s** | **≈399,800 runes/s** | 0.45× |
| dictionary load (first call) | 1.29–1.40 s wall / 1.33–1.37 s CPU | 0.78–1.14 s CPU | ~1.4× |
| peak RSS (one-shot runs) | 192,972 KB (~188 MiB) | 90,268 KB (~88 MiB) | 2.14× |

**jieba is ~2.2× faster in CPU time on this corpus, and gse costs a one-off ~1.3 s
dictionary load plus ~188 MiB resident.**

The per-run **ratio is the only stable quantity, and it is not as tight as the median
suggests**: across the six measurement runs it spans **1.78× to 2.77×** (median
2.24×), because gse's CPU time moved 4611–6869 ms while jieba's moved 1798–3328 ms.
Absolute rates therefore should not be quoted to three digits — the plan-able figure is
**jieba ≈2.2× gse's CPU, range 1.8–2.8×**. For scale: at the median rate a single
200-rune memory row costs gse ≈**1.12 ms**. That is a per-write cost on a path that
already writes SQLite, and it is not a latency budget anyone has stated.

---

## 10. The ≥20 mismatching samples, adjudicated line by line

26 lines, drawn shortest-first from each class so the mechanism stays legible.
`G` = gse (Go, `internal/cjkseg`), `J` = Python jieba exact mode,
`T` = the source text. Full machine-readable set: `artifacts/samples.json`.

**Boundary class (14) — the ones that matter for an index.**

| # | T | G | J | better | why |
|---|---|---|---|---|---|
| 1 | 双向兼容的测试改法 | 测试 **改法** | 测试 **改 法** | **gse** | 改法 is a word; jieba cut it |
| 2 | 对比报告已写入三节点 | 三节 点 | 三 **节点** | **jieba** | 节点 is the word; `zh_s` lacks it |
| 3 | 全景对比矩阵+三派系 | 三派 系 | 三 **派系** | **jieba** | same shape, 派系 |
| 4 | …装配日志为什么会读到相反结论 | 会 **读到** | **会读** 到 | **gse** | 会读 is not a word |
| 5 | 物理学的解决方案是增加细分测度 | 解决 方案 | **解决方案** | **jieba** | `zh_s` lacks 解决方案 |
| 6 | 方向已修正——CLI 接口任务已派 | 已修 正 | 已 **修正** | **jieba** | gse merged 已+修 wrongly |
| 7 | (same text, answer field) | 已修 正 | 已 **修正** | **jieba** | same defect, second occurrence |
| 8 | gopi 重放灰阶诊断 | **重放** | 重 放 | **gse** | 重放 is a word |
| 9 | red 不可复现时的取证口径 | 不可 复 现时 | 不可 **复现** 时 | **jieba** | gse cut 复现 and glued 现时 |
| 10 | …本机策略段 | **本机** 策略 | 本 机 策略 | **gse** | 本机 is a word |
| 11 | …控制面必须按 pid 观测而非按锁文件 | **而非** | 而 非 | **gse** | 而非 is a word; jieba cut it |
| 12 | （DA1/DSR 有界探测） | **有界 探测** | 有 界 探测 | **gse** | both are words |
| 13 | 怎么避免自建一个和仓库已有门禁重复的 gate | **自建** | 自 建 | **gse** | 自建 is a word |
| 14 | 修法是归一化而非改产品 | **而非 改 产品** | 而 **非改** 产品 | **gse** | jieba glued 非改 |

**Score: gse better in 8, jieba better in 6.** The jieba wins are 4 dictionary gaps
(2, 3, 5, and 6/7's 修正) and one boundary error (9); the gse wins are all cases where
jieba split a word jieba's own dictionary contains. This is consistent with §6, which
counts the same asymmetry corpus-wide (74 vs 1).

**Punctuation-attachment class (6) — no search consequence.**

| # | T | G | J | judgment |
|---|---|---|---|---|
| 15 | 做法（已落地可复用）： | `）：` | `） ：` | tie for search; **gse** more faithful (the source has them adjacent) |
| 16 | 判据链（按顺序，一步都不要跳）： | `）：` | `） ：` | same |
| 17 | gopi 失败了——而且… | `——` | `— —` | tie for search; **gse** keeps the em-dash pair as one token |
| 18 | 做法（producer 删除类改动的可复用配方）： | `）：` | `） ：` | same as 15 |
| 19 | 【拓扑三键（CONFIG.md 是权威，改前必读）】 | `）】` | `） 】` | same |
| 20 | …才算"两边意图都保留"，验收标准 | `"，` | `" ，` | same |

None of these changes a single searchable token — punctuation is stripped by the
`unicode61` tokenizer downstream, and neither side's punctuation is queried.

**Case-only class (6) — the `ToLower` policy, visible.**

| # | T | G | J | judgment |
|---|---|---|---|---|
| 21 | 5W1H 测度框架 | `5w1h` | `5W1H` | tie for search (index is case-insensitive); **jieba** better for display |
| 22 | VPN 连接故障排查 | `vpn` | `VPN` | same |
| 23 | VPN 连不上/频繁掉线怎么办？ | `vpn` | `VPN` | same |
| 24 | It works! The tool: | `it … the` | `It … The` | same; jieba preserves sentence capital |
| 25 | OPC/loop-engineering | `opc` | `OPC` | same; acronym lost by gse |
| 26 | ## What You MUST NOT Do | `## what you must not do` | `## What You MUST NOT Do` | same; `MUST NOT` is a modal idiom, lost by gse |

**Aggregate judgment on the case class:** for the **search index** it is a tie — the
index cannot tell `vpn` from `VPN`. For any **human or LLM reading the column**, jieba
is better: gse destroys acronyms (`VPN`, `CLI`, `LLM`, `HEAD`) and emphasis
(`MUST NOT`). Since nothing in gopi reads `jieba_token` for display (it exists to be
indexed), this is a latent cost, not a current defect — recorded so a future reader who
*does* want to display it knows to set `gse.ToLower = false` at load time (one
package-level assignment in `cjkseg.load`).

---

## 11. Not Done (honest)

* **`zh_s` vs `zh` was not compared.** `cjkseg` hardcodes `zh_s`; the larger dictionary
  is a different measurement and a different binary size, and the predecessor's survey
  already chose `zh_s`.
* **Only the first line of each field is compared** (§2). Multi-paragraph behaviour,
  especially paragraph tails, is unmeasured.
* **The direction test uses jieba's dictionary as the single authority** (§6b), so
  gse's own dictionary advantages are not credited.
* **No recall/hit-rate A/B on the operator's real 8401-row ledger.** The end-to-end
  evidence is a private sandbox ledger with 8 stored rows plus the unique-token probe
  (§7a); running a recall A/B against the operator's live memory would need a
  controlled query set this task did not define.
* **The `store`/`backfill` divergence (finding (a)) was found, verified and NOT
  fixed** — out of scope for a verification task, and the briefing says not to change
  code. It is dispatchable as its own task.
* **Throughput is one corpus on one machine**, with no long runs and no warm-vs-cold
  JIT/GC characterisation (Rule 27 forbids manufacturing load; the honest limit is
  that the ratio is stable across 6 runs and the absolute rate is not).

---

## 12. Ran (literal `Ran:` lines)

Each line below was executed for real against the delivered artifacts and the corpus;
`run-all.sh` replays all of them end to end.

Ran: `bash <session>/artifacts/segeval/run-all.sh` → `DONE — all artifacts under <session>/artifacts`; `git status --porcelain` printed nothing
Ran: `<session>/artifacts/bin/segeval dump-corpus ~/.gopi/memory.db <session>/artifacts/corpus.jsonl` → `distilled_qa total=8401 sampled=240 stride=35`
Ran: `<session>/artifacts/bin/segeval segment <session>/artifacts/corpus.jsonl <session>/artifacts/gse-out.jsonl` → `cjkseg dictionary: zh_s`, `segment: 720 line(s)`
Ran: `~/work/hermes-agent/.venv/bin/python3 <session>/artifacts/segeval/jieba_seg.py <session>/artifacts/corpus.jsonl <session>/artifacts/jieba-out.jsonl` → `jieba 0.42.1 ... lines=720`
Ran: `python3 <session>/artifacts/segeval/score.py <session>/artifacts/gse-out.jsonl <session>/artifacts/jieba-out.jsonl <session>/artifacts/score.json` → `lines 720 mismatch_lines 625 diff_kinds {'case_only': 264, 'punct_only': 260, 'boundary': 101}`
Ran: `~/work/hermes-agent/.venv/bin/python3 <session>/artifacts/segeval/cut_direction.py <session>/artifacts/gse-out.jsonl <session>/artifacts/jieba-out.jsonl <session>/artifacts/cut-direction.json` → `gse_split 1 / jieba_split 74`, `split ratio jieba/gse = 74.00`
Ran: `<session>/artifacts/bin/segeval bench <session>/artifacts/corpus.jsonl 5` → `loop_cpu_ms=5159.1` (median of 6 runs; program-reported `runes_per_cpu_sec` ranged 134217–199918)
Ran: `~/work/hermes-agent/.venv/bin/python3 <session>/artifacts/segeval/jieba_seg.py --bench <session>/artifacts/corpus.jsonl 5` → `loop_cpu_ms=2305.7` (median of 6 runs; program-reported `runes_per_sec` ranged 277050–512656)
Ran: `bash <session>/artifacts/segeval/size-build.sh 4d9ac2acf` → `baseline_MB=29.56 current_MB=61.08 delta_MB=31.52`
Ran: `CGO_ENABLED=0 go build -o <session>/artifacts/bin/gopi-current ./cmd/agent/` → exit 0
Ran: `CGO_ENABLED=0 go build -tags ne -o <session>/artifacts/bin/segeval-ne ./cmd/segevaltmp/` → exit 0, then `<session>/artifacts/bin/segeval-ne segment <session>/artifacts/probe-corpus.jsonl <session>/artifacts/probe-gse-ne.jsonl` → 11/51 lines differ from the shipping build
Ran: `SB=$(bin/gopi tool sandbox create --label segprobe2 --task-id 01a113a2-...)` then `GOPI_HOME=$SB ./bin/gopi memory store --topic 探针A --question ... --answer ... --keywords "zzuniquetok9,placeholder"` → `Memory stored (id=1).`
Ran: `GOPI_HOME=$SB ./bin/gopi memory backfill-tokens` → `1 row(s) scanned, 1 jieba_token value(s) re-derived`
Ran: `GOPI_HOME=$SB ./bin/gopi memory backfill-tokens --dry-run` → `1 row(s) scanned, 0 would be re-derived` (idempotent)
Ran: `GOPI_HOME=$SB ./bin/gopi memory search "zzuniquetok9" --limit 3` → `#1 令牌探针 tier=warm conf=0.85 ... tokens=1`, both before and after the backfill
Ran: `./bin/gopi memory store` ×8 in a sandbox + `memory backfill-tokens --dry-run` → `8 row(s) scanned, 8 would be re-derived` (finding (a))

---

## 13. Reproduction

```bash
# everything, end to end, no arguments:
bash ~/.gopi/sessions/01a113a2-2602-7aac-a49b-dc2c138a213e/artifacts/segeval/run-all.sh
```

Artifacts (Rule 37 — all in the session directory, **none in the git tree**):

| path (under `~/.gopi/sessions/01a113a2-2602-7aac-a49b-dc2c138a213e/artifacts/`) | what |
|---|---|
| `segeval/run-all.sh` | the driver: every number in this document |
| `segeval/main.go` | the Go helper (dump-corpus / segment / bench) |
| `segeval/jieba_seg.py` | the jieba side (segment + bench) |
| `segeval/score.py` | token F1, line agreement, diff classes, case tally |
| `segeval/cut_direction.py` | who destroys a dictionary word boundary |
| `segeval/size-build.sh` | baseline-vs-HEAD `CGO_ENABLED=0` builds and sizes |
| `corpus.jsonl` | **the fixed corpus** (240 rows → 720 lines) |
| `gse-out.jsonl`, `jieba-out.jsonl` | both token streams |
| `score.json`, `cut-direction.json`, `samples.json` | the measurements and the 26 adjudicated samples |
| `probe-corpus.jsonl`, `probe-gse.jsonl`, `probe-jieba.jsonl`, `probe-gse-ne.jsonl` | the minimal sample (§6a) and the `-tags ne` probe (§7b) |
| `size-build.log` | the size run's raw log |
| `bin/gopi-pre`, `bin/gopi-current` | baseline and HEAD binaries, `CGO_ENABLED=0` |

---

## 14. Provenance

Task `01a113a2`, branch `task/01a113a226027aaca49bdc2c138a213e`, worktree
`/home/user/workspace/gopi-01a113a226027aaca49bdc2c138a213e`, measured on peer1
2026-10-07 with go1.24.5 linux/amd64, Python 3.11.14 + jieba 0.42.1 (from
`~/work/hermes-agent/.venv` — the system Python was not touched, Rule 35), and
`modernc.org/sqlite v1.44.0`. Corpus read from `~/.gopi/memory.db`, which was opened
read-only (`mode=ro&_pragma=query_only(true)`) and never written.

*Classification note for a reader auditing `DOC-INDEX.tsv`:* this document is a
measurement report on a shipped integration. Where the collector's classifier and this
prose disagree, §1 and §7 are the verdict and the rest is evidence for it.
