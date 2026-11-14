<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# evidence 新鲜度：为什么 manifest 的 evidence 单元可以静默漂移，以及收口方式

<!--
本次记录（task 01a12cfd-e0b0-7c88-ba42-48b30134938c，tree 1508e3821，2026-10-11）。
它记的是**那一次测量**，所以是 historical：里面的数字是快照，不是对当前 master 的断言。
-->

## 现象（实测）

在 tree `1508e3821` 的干净工作区里，用仓库自己的生成器整库重派生：

```
python3 tools/docclass/classify.py .
git diff --numstat -- devdoc/DOC-INDEX.tsv     # → 662  662
```

同一棵树上改写 **662 行**（2282 行里），而三个"一致性"面在漂移前后的输出**完全相同**：

```
python3 tools/docclass/classify.py . --check   # 0 gap(s)
python3 tools/docclass/banner.py . --check     # 0 markdown docs whose banner disagrees
gopi tool doccheck                             # corpus consistent
```

原因是它们回答的是**另外三个问题**：覆盖（每个文件有行）、形状（四列/类/理由长度）、
类别（banner ↔ manifest）。**没有一个问"证据是否还新鲜"。**

## 662 行的构成（生成器与 commit 见上）

| 维度 | 值 |
|---|---|
| 总改动 | 662 |
| class 变化 | 182（`historical → normative` 163、`normative → historical` 19） |
| 仅 evidence 变化 | 480 |
| evidence-only 按 tier 对 | `anchor` 182、`sym` 87、`path` 83、空→空 77、`cite` 20、跨 tier 31 |
| 改动行在重派生 manifest 的 class | normative 566 / historical 95 / index-meta 1 |

其中 **77 行是"空 → 空"**：class 与 tier 都没变，变的是 reason 里的
`standing N / record N` 计数 —— 也就是说这 77 行连证据单元都没碰，只是计数重算。

## 关键取舍：为什么"新鲜度 = 与生成器逐字节一致"不能作为门禁

这是本任务唯一真正的设计决定，结论是**刻意不做**，理由有三条，且都可实测：

1. **生成器的 evidence 是偏好，不是声明。** 同一个 doc 往往有多个候选 cite/anchor，
   生成器按自己的启发式挑一个。实测：`devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md`
   的 cite 从 `tasks/01a0c621-docclass-historical-banner-report.md:20` 变成
   `briefings/brief-worker-resume-owns-the-ground.md:28` —— **两条都解析成功**，
   换的只是"引用哪一个"。把这种变化判成红，等于把生成器的偏好升格成规范。
2. **它的代价是语料级重写。** 要让"逐字节一致"成立，必须整库重派生（662 行 / 无人审），
   而本仓库既有的纪律恰好相反：生成物**只在被要求时**做 scoped 重派生
   （`classify.py --only <doc>`），整库形式在 memory #1991 里被记作"corpus-wide
   re-classification nobody asked for"。
3. **判据的语义已经有了唯一 owner。** `internal/docclass.Evidence.Check` 定义了什么算
   漂移：`anchor` 认**记录的文本**（行号只是加速器）、`cite` 认 basename 仍被引用、
   `path` 认存在、`sym` 认仍被声明。新的门禁**消费**这套语义，不另立一套
   （Rule 14），于是"漂移"的定义与 merge 缝完全同一个刻度。

## 收口方式（已交付）

```
internal/standards/doc_evidence_gate.go        # 门禁：实树 + python-free + 秒级
internal/standards/doc_evidence_gate_test.go   # red-proof（最小 fixture）+ 实树用例
```

- 对**真实 manifest**的每条 normative 行跑 `ParseEvidence` + `Check`，把"记录的证据
  不再解析"从 **merge 落地时点**提前到**引入它的那次 commit 的测试周期**。
- 不依赖 python：生成器是 provenance 工具（构建里没有 python），门禁判据若依赖它，
  在没装 python 的机器上就会"因为没跑而绿"（Rule 15）。
- 既有漂移用 **hold-out**（`docEvidenceHeldOut`，path→reason）**点名**，并配 stale 检查：
  漂移一旦消失，hold-out 必须删除否则报错 —— 它是"记在册的待修项"，不是 permit。

## 既有漂移（2026-10-11 时点的 2 条）

| doc | manifest 行 | 记录文本 | 实际情况 |
|---|---|---|---|
| `devdoc/plain-prompt-first-user-message-scope.md` | 1317 | `// did not start the message at all.` | `cmd/agent/cli_runpath_boundary_rp1_test.go`（第 246 行）那一行被后续 commit 改写，记录的文本在该文件里一行都不存在 |
| `devdoc/session-record-read-path-double-filter-gate.md` | 1616 | `stamp := a.turnStampLine()` | `internal/agent/clock.go`（第 237 行）同理 |

同一个判定在 master 上以 `cmd/agent` 的 merge-union 地板用例持续报红（`the check reports
2 finding(s) against the repo's OWN tree`），这正是"漂移只在 merge 时点被问到、且不阻断
落地"的后果。清理它们需要一次 scoped 重派生 + 复核，本任务只把判定提前，不动语料。

## 收口（2026-10-12，task 01a12d1b-7599-7aa8-8585-6a2d20ef7fbe）

上面那两条漂移**已在门禁落地之前被清掉**：`8a0d75d9b`（*fix(docclass): re-derive the
two drifted DOC-INDEX evidence anchors so the floor case passes on the real tree*，
task 01a12d12）做了 scoped 重派生，落进 master 的时间**早于**本门禁。

本门禁自己的提交是 `8c5f4812e`（06:27:16），把它合入 peerbox master 的是 merge
`f82777a9f`（06:43:25）—— 也就是说，它**落地的那一刻就自带两条陈旧 hold-out**，
于是它自带的 ratchet 立刻把自己判红：

```
--- FAIL: TestDocEvidenceGateOnTheRealManifest
    docEvidenceHeldOut names devdoc/plain-prompt-first-user-message-scope.md, but no drift fires there any more
    docEvidenceHeldOut names devdoc/session-record-read-path-double-filter-gate.md, but no drift fires there any more
    devdoc/DOC-INDEX.tsv: 728 normative row(s) resolved against the tree, 0 drift finding(s)
    (0 held out: [...]), 2 hold-out(s) stale
```

**处理方式就是 ratchet 自己说明的那一步**：删掉那两条 entry，`docEvidenceHeldOut`
清空。这不是"改门禁让它变绿"—— 恰恰相反，清空之后**没有任何 hold-out 能替漂移顶包**：
每一条漂移都直接落进 `docEvidenceReport.New` 并让真 manifest 用例失败（红证见
`tasks/01a12d1b-report.md`：合成单条假引用 → `1 drift finding(s) (0 held out)`）。

**这次暴露出来的那一类教训**：hold-out 表与它为之存在的漂移之间有一条**时间窗** ——
持有方先修上游、门禁后落地，门禁出生即陈旧。凡是"点名记录既有缺陷"的表都带这个窗口
（`docanchor-baseline.tsv`、`repo-hygiene-baseline.tsv` 同理），所以"落地时跑一次自己
的门禁"是必需的收口动作，而不是可选的礼貌。
