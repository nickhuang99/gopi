<!-- doc-class: normative -->
# 记忆闭环九步的 demo 产物：配方与三个量出来的坑


**这是什么。** `docs/archive/demo/loop/`（论证文档 + 4 组配图）与它的两个生成器
（`scripts/demo-memory-loop.sh`、`scripts/demo-memory-loop-figs.sh` + `scripts/demo-loop-prepad.py`）的
设计说明：形状为什么是这样、哪些坑是**量出来**的、怎么重跑。

---

## 1. 形状

```
bash scripts/demo-memory-loop.sh <evidence_dir>          # 十九步真跑，逐字落盘
bash scripts/demo-memory-loop-figs.sh <loop-dir> docs/archive/demo/loop <render-evidence-dir>   # 渲染配图
```

- **真跑驱动器**（`demo-memory-loop.sh`）：`gopi sandbox create` 拿私有 profile（Rule 34）→
  `GOPI_HOME=$SB` 跑十九步 `gopi memory` / `gopi brew` → 每步落
  `01-input.txt`（cwd + `GOPI_HOME` + argv）、`02-output.txt`（stdout+stderr 合并，**末行 exit=N**），
  另有一份合订 `transcript.txt`。全程 `-h` 不算一步（Rule 31）。
- **配图生成器**（`demo-memory-loop-figs.sh`）：把若干步的 verbatim 输出拼成一份 doc，
  交 `scripts/materials/render.py`（**复用**，不重写）量几何、画框、自证像素。

**证据全部落在 run 的证据目录**（`/home/user/.gopi/sessions/<task-id>/loop-<stamp>/`），
仓库里只留 `docs/archive/demo/loop/` 的成品与两个脚本 —— 工作树干净（Rule 30）。

## 2. 三个量出来的坑（都会让整条图静默/报错不产出）

1. **标签带压住证据：render.py 自己的"腾空"在多标注时会把后面的行号顶偏。**
   render.py 先算好所有 needle 的行号、再逐条插空行 —— 插进去的空行让后面的 needle
   指向了前一处的标签带，像素自证 `label_covered_ink` 报 `190` 像素压住证据并拒产。
   修正：**在文本进 render.py 之前**就按同一份 `wrap()` 折行、按行号**升序**插空行并
   **同步累计偏移**（`scripts/demo-loop-prepad.py`），render.py 的腾空逻辑随即变成空操作。
2. **needle 里不能有冒号。** render.py 用单冒号切 `needle:label`，`A: 实测纠正` 与
   `brew: 二进制路径` 都被切成一个词 + 一段乱标签（后者直接 FATAL）。
   生成器里统一写 `needle::label` 并在还原时**断言 needle 不含冒号**。
3. **needle 必须是"证据段里真实存在"的子串**（`02-output.txt` 分隔行之后）。
   证据段分隔行的判据是 `l.lstrip().startswith("─")` 且**取第一次出现**——
   图尾那句"完整 verbatim 见 02-output.txt"含同样字样，取最后一次会把地板推到图外。

**图不是"看起来对"**：每张 `04-annotated.png` 在写出前过 render.py 的三项像素自证
（框内墨迹 > 0、标签带压住证据 = 0、annotate 改动像素 > 0），任一不过 exit 1 不产图；
量出来的数字（`pitch=30.0`、`ink_under_mark`、`changed_pixels`）留在 render JSON 里。

## 3. 台账隔离是怎么被证明的

**不是文件哈希前后相等** —— 宿主 chat 会话自己会写操作者台账，哈希会动，
那是别的写入者的。本 run 的判据是**按探针记号归属**：

```sql
select count(*) from distilled_qa where instr(topic||question||answer,'<探针记号>')>0;
```

（用 `instr` 而非 `grep`：本 run 的会话记录里就写着这些记号，`grep` 会自匹配 ——
那是审计记录，不是知识库写入。这条在本仓 `docs/archive/demo/materials/010-brew-dry-run/00-entry.md` 已经踩过一次。）

三个探针记号在操作者台账里 0 命中、在沙盒台账里各 1 命中，逐字证据见证据目录的
`ledger-evidence.txt` / `evidence-ledger-isolation.txt`。

## 4. 两个真跑的用法要点

- **`brew` 扫的是 `<GOPI_HOME>/.gopi/sessions/*.jsonl`**（`internal/tools/memory_brew_run.go`
  经 `config.GopiDir()` 解析）。沙盒的 `sessions/` 是空的 ⇒ 直接跑只会得到
  `sessions=0 chunks=0 ... stored=0`，**那不是"brew 没工作"，是"没有输入"**。
  所以驱动器先把一份**真会话记录**（439,866 字节）放进沙盒 `sessions/demo/`，
  再用 `--dir` 指向它：这才让 `--dry-run` 与真跑变成可比的两次运行。
- **`--dry-run` 与真跑的唯一可读差别是收尾行的 `stored`**（判定计数两轮相同）。
  输出里**没有 dry-run 自己的字段**，所以"没写因为 dry-run"与"没写因为全被去重"
  在那一行里不可区分 —— 本 run 用**台账行数**（`distilled_qa` / `procedural`）补了这个判据。
  已在 `docs/archive/demo/loop/CLOSED-LOOP.md` 的"缺口与勘误"一节如实写出。

## 5. 一条诚实记录：错记忆的样本取自真实知识库

第 2 节演示的"错记忆"不是编的：它取自知识库 `#6893` 的真实断言
（"投票后 conf 保持 0.85 不变，变的是 eff_conf"），而本轮真跑把它推翻
（`conf` 0.85→0.90）。演示"系统能发现自己错了"必须用**真的曾经被相信的错**，
否则演示的是"我写了一句废话然后删掉它"。

## 6. 复现

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
bash scripts/demo-memory-loop.sh /home/user/.gopi/sessions/<task-id>
bash scripts/demo-memory-loop-figs.sh <上面打印的 EVIDENCE 目录> docs/archive/demo/loop <证据目录>/figs
```
