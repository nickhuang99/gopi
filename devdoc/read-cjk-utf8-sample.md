<!-- doc-class: historical -->
# Task Report: read 工具二分检测样本在 rune 中间截断导致误报 binary（CJK 假阳性）

Branch: `feat/read-cjk-utf8-sample`
Scope: `internal/tools/read.go` (checkBinary) + `internal/tools/read_test.go`

## 问题（症状 → 机理 → 根因，5-why）

1. **症状**：一个 100% 合法 UTF-8 的中文 Markdown（14365 B）经 read 工具被拒：
   `read: … is a binary file（检测到无效 UTF-8）— 用 bash('file …') 或 bash('xxd …') 检查，不要 read`。
   被逼去用 `bash sed/head` 这种违反路由规则（Rule 3）的绕路。
2. **机理**：`checkBinary` 只对**前 8192 字节的原始样本**做 `utf8.Valid`。`maxBinarySampleBytes = 8*1024 = 8192` 是**字节偏移，不是 rune 边界**。
3. **根因**：CJK 常用字是 3 字节 rune。`8192 % 3 == 2`，所以任何内容跨过 8KB 且以 3 字节 rune 为主的文件，样本**必然在某个 rune 的中间被切断**——样本尾部是一个不完整 rune（例如 `e5 b7` = 工 的前两字节），`utf8.Valid` 返回 false → 误判为 binary。这是一个**窗口切割伪影**，与文件内容无关。
   受影响比例 ≈ 1/3（长度 mod 3 的分布），且是系统性偏差（中文文档几乎全部命中）。
4. **为什么以前没发现**：纯 ASCII 文件、以及恰好落在 rune 边界的那 ~2/3 窗口都不触发；样本窗口恰好超出文件长度时也不触发。

## 复现（真实产物）

`devdoc/deepseek-v41-flash-pricing-table-repair-20260911.md`（14365 B，`len % 3 == 1`）：

```
bytes 8188..8198 = e6 89 8b | e5 b7 a5 | e5 8f 8d | e6
                    手         工(被切成 e5 b7)   反        下一个字的前导字节
```

- `bytes[:8192]` 解码失败：`'utf-8' codec can't decode byte 0xe5 in position 8191: unexpected end of data`
- 整个文件 `bytes` 解码成功：**全文合法 UTF-8**（这是关键：拒绝一个完全合法的文件）。
- 用修复后的 read 工具读该文件：成功，且与源文件**逐字节一致**（`TestRead_SampleCutMidRuneNotRejected/real_report_file_reads` 断言 `got == string(full)`）。

## 修复（Rule 4 外科手术式，不扩大样本、不放宽 NUL）

`internal/tools/read.go`：

- **新增** `trimIncompleteTrailingRune(sample []byte) []byte`：仅当样本尾部是**一个多字节 rune 的合法前缀**时才删掉这段尾巴（最多 `utf8.UTFMax-1` 字节），返回剩余部分。内部非法字节一律原样保留。
  - 快路径：`utf8.Valid(sample)` 直接返回（零开销覆盖纯 ASCII 与落在边界的窗口）。
  - 只尝试 `back = 1..utf8.UTFMax-1` 个候选；第一个「前缀合法 且 掉尾是不完整 rune」的候选即答案。
- **新增** `isIncompleteRunePrefix(tail []byte) bool`：把 `tail` 用合法续字节（`utf8ContinuationByte = 0x80`）补齐到 `utf8.UTFMax` 后解码；**只有当解码成功且 `size > len(tail)`** 才说明 tail 是一个被切短的 rune。
  - 因此**裸 0xFF、孤立续字节 0x80、完整 rune、空串全部返回 false** → 照旧拒绝。这是「不能为了让检测闭嘴而修字节」的分界线。
- `checkBinary` 改为 `utf8.Valid(trimIncompleteTrailingRune(sample))`。
  - **NUL 规则完全不变**：仍在**原始样本**上 `bytes.IndexByte(sample, 0)`，先于任何裁剪。
- **明确拒绝** `bytes.ToValidUTF8` 式「修复」：它会重写/丢弃**内部**非法字节，等于把检测废掉（在注释中写明理由，防未来重蹈）。
- **未新增工具、未改工具描述、未改路由** → Rule 11 不适用。
- Rule 1/13：无魔法数字，复用 `maxBinarySampleBytes`；新增两个具名常量 `utf8ContinuationByte`、`cjkRuneRepeatCount`（测试）等。

### 为什么这个裁剪是安全的

删掉的字节必须同时满足两个条件：(i) 其**前面**的所有字节是合法 UTF-8；(ii) 这串尾巴本身是一个多字节 rune 的**真前缀**。任何**内部**非法字节都在 (i) 处失败，任何裸 `0xFF`/孤立续字节都在 (ii) 处失败。所以「合法 UTF-8 文件被窗口切了一刀」与「真的含非法字节的文件」被干净地分开了。

## 回归测试（Rule 10.5，最小复现 + 红证）

`internal/tools/read_test.go`：`TestRead_SampleCutMidRuneNotRejected`（含 6 组子用例）+ `TestTrimIncompleteTrailingRune`（9 个表驱动 case）。

| 子用例 | 断言 |
|---|---|
| `cjk_repeat_cut_mid_rune` | `strings.Repeat("中", 3000)` + `\n`，长度远超样本；**先断言样本截断处确实在 rune 中间**（否则 fixture 无效），再断言 Read 成功且返回全文 |
| `real_artifact_bytes_cut_mid_rune` | 用真实产物的**字节序列**（8188.. 起 `e6 89 8b e5 b7 a5 e5 8f 8d e6`）在**同一偏移 8188** 位置铺出来，样本恰好在 8192 切进 工 的中间 —— 逐字节复现，而非「差不多的中文文件」 |
| `real_report_file_reads` | 直接读真实 devdoc 文件，断言**逐字节一致**（文件不在 checkout 里则 skip） |
| `interior_invalid_byte_rejected` | 样本窗口**中部**的 `0xFF` 仍被拒（裁剪不会波及内部） |
| `bare_invalid_byte_at_cut_rejected` | 切点上的裸 `0xFF` 与孤立续字节 `0x80` **仍被拒**（不是「所有尾部问题都放过」） |
| `nul_rejected` | NUL 在样本中部 / **最后一个被采样字节**都仍被拒，且错误信息里含 `NUL`（NUL 规则未被裁剪影响） |

红证（red-proof，在本 worktree 内实测）：把 `checkBinary` 临时改回 `utf8.Valid(sample)`（加 `// RED-PROOF` 注释标记），
`go test -run TestRead_SampleCutMidRuneNotRejected` **FAIL**，三个子用例分别报出误报错误：
`cjk_repeat_cut_mid_rune`、`real_artifact_bytes_cut_mid_rune`、`real_report_file_reads` 全部命中
`is a binary file（检测到无效 UTF-8）`。恢复修复后同一命令 PASS。→ 该测试确实钉住了这个 bug，而不是「怎么写都过」。

## Verification

```
go build ./...                        : OK
go test ./internal/tools/             : PASS (见下方原始输出)
go test -run TestRead_SampleCutMidRuneNotRejected (pre-fix) : FAIL ×3 子用例（红证）
go test -run TestRead_SampleCutMidRuneNotRejected (post-fix): PASS
go test -run 'TestRead_InvalidUTF8Rejected|TestRead_BinaryFileRejected|TestRead_BinaryFileRejectedWithLimit|TestRead_NULAfterSampleWindow|TestRead_NormalTextStillPasses' : PASS（既有拒绝语义未回归）
gofmt -l internal/tools/read.go internal/tools/read_test.go : 无输出
go test ./internal/tools/                   : ok (EXIT=0，全包 173s；见下方 commit 后的完整复跑日志)
Commit: b13851efe7a8cf4f3b5cd3b3627313cd7ce222bd (feat/read-cjk-utf8-sample, worktree ~/workspace/gopi-read-cjk)
```

### 端到端验证（真实二进制 × 真实产物，非单测）

用本 worktree 的修复代码构建二进制 `CGO_ENABLED=0 go build -o /tmp/gopi-readfix ./cmd/agent/`，
以**真实 read 工具**（禁止 bash）读被误报的文件：

```
echo "Use the read tool to read the file devdoc/deepseek-v41-flash-pricing-table-repair-20260911.md
      and report its first heading line verbatim. Do not use bash." | GOPI_DISABLE_TASK_LEDGER=1 /tmp/gopi-readfix
```

结果：read 工具**成功**读到文件，返回首行
``# Devdoc: DeepSeek pricing table repair — `deepseek-flash` (V4.1-Flash) 定价修复``
（session 日志中无 `binary file（检测到无效 UTF-8）`，且 LLM 未被迫走 bash/sed 绕路）。
修复前该文件在 read 工具下必然被拒（见上文红证）。

原始输出摘录：

```
--- PASS: TestRead_SampleCutMidRuneNotRejected/cjk_repeat_cut_mid_rune (0.00s)
--- PASS: TestRead_SampleCutMidRuneNotRejected/real_artifact_bytes_cut_mid_rune (0.00s)
--- PASS: TestRead_SampleCutMidRuneNotRejected/real_report_file_reads (0.00s)
--- PASS: TestRead_SampleCutMidRuneNotRejected/interior_invalid_byte_rejected (0.00s)
--- PASS: TestRead_SampleCutMidRuneNotRejected/bare_invalid_byte_at_cut_rejected (0.00s)
--- PASS: TestRead_SampleCutMidRuneNotRejected/nul_rejected (0.00s)
--- PASS: TestTrimIncompleteTrailingRune (0.00s)

--- FAIL: TestRead_SampleCutMidRuneNotRejected/cjk_repeat_cut_mid_rune   ← 红证（pre-fix）
        read_test.go:674: valid-UTF-8 file cut mid-rune must not be rejected, got: read: … is a binary file（检测到无效 UTF-8）…
--- FAIL: TestRead_SampleCutMidRuneNotRejected/real_artifact_bytes_cut_mid_rune
--- FAIL: TestRead_SampleCutMidRuneNotRejected/real_report_file_reads
```

## Not Done / 有意不做

- **没有扩大样本窗口**、**没有放宽 NUL 规则**（任务约束）：NUL 检查仍在原始样本上、仍是硬拒绝。
- **没有修 `bytes.ToValidUTF8` 式修复**：会掩盖内部非法字节，等于废掉检测。
- **没有改错误提示文案**：`binary file（检测到无效 UTF-8）` 与其 bash('file')/xxd 指引保持不变（既有测试断言它）。
- 其他读取路径的 rune 边界问题（如 `maxFullReadBytes` 截断）**不在本次范围**：那里已有独立的 rune-boundary 处理与测试（`TestRead_SingleHugeLineRuneBoundary`、`TestRead_HugeLineFragmentMidRune`）。

## 给未来 agent 的提示（devdoc 的窗口）

- 「二进制检测」这一类逻辑，**任何固定字节窗口都必须按 rune 对齐或容忍尾部半个 rune**；`utf8.Valid(window)` 直接用是错的。
- 判断「尾部是半个 rune」而不是「文件坏了」的判据是：**补齐后能解码，且解码长度大于尾巴长度**。裸非法字节永远过不了这一关。
- 红证的写法：把修复点临时 sed 回旧实现（留一个 `// RED-PROOF` 标记）→ 跑新测试 → 必须 FAIL → 还原。这是「测试真的钉住了 bug」的唯一廉价证据。
