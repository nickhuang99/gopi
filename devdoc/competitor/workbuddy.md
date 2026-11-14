<!-- doc-class: historical -->
# WorkBuddy / CodeBuddy（腾讯，claude-code 同族 CLI；源码托管在 cnb.cool）

<!-- meta -->
repo: codebuddy-code（托管在 `https://cnb.cool/codebuddy/codebuddy-code`，**不在 GitHub**：`https://api.github.com/repos/tencent/codebuddy-code` 等路径未取证到公开镜像；因此本文件无 GitHub sha 可用）
sha: 无可钉住的 sha（cnb.cool 的 trees API 探测失败：`https://cnb.cool/codebuddy/codebuddy-code/-/git/trees/main?recursive=1` 返回的是 HTML 404 页面，`<title>Page not found · CNB</title>`，实测 2026-11-06T00:15:00Z。可钉住的只有默认分支 `main` 上的 raw 文件**当次取回的内容**）
pin_substitute: raw 正文取回时间戳 2026-11-06T00:15:23Z（该行逐字抄在下方「证据重放」节 (1)），正文 145 行（全文抄在节 (2)，不需要外部路径即可复核）——**这是「内容快照」而不是「提交锚」，下游引用时须复述这一点**
guide: workbuddy.md（dsa-docs 只读快照，106 行）
url_in_guide: https://www.codebuddy.ai/ 、 https://copilot.tencent.com/
distribution: `npm install -g @tencent-ai/codebuddy-code`，registry 元数据 `version=2.180.0`、`bin=['cbc','codebuddy','cbc-prewarm','codebuddy-code','codebuddy-lowmem']`、`license="SEE LICENSE IN README.md"`（`https://registry.npmjs.org/@tencent-ai/codebuddy-code/latest`，实测 2026-11-06T00:17:00Z）
license: `codebuddy-LICENSE.txt`（raw 取回，18 行）首行 `Copyright Copyright 2023 Tencent Cloud`、第 3 行 `Permission is hereby granted, free of charge, to any person obtaining a copy` —— MIT 正文；registry 元数据的 `license` 字段却是 `SEE LICENSE IN README.md`，**两处声明不一致**（记录原样，不下结论）
open_source: 许可证正文为 MIT 形态（见上）；源码托管于 cnb.cool，非 GitHub
official_docs_fetched_at: 2026-11-06T00:07:48Z（`https://www.codebuddy.ai/docs`、`/docs/ide/getting-started`、`https://copilot.tencent.com/`），**三者均为 JS 渲染的空壳**：转文本后各 **1 行**，无正文可取（转文本输出逐字抄在下方「证据重放」节 (4)，那 1 行是什么也一并抄出）。可用的官方语料只有 cnb.cool 的 raw README + dsa-docs guide
channels: E1 = (a) raw README（145 行，`main` 分支当次内容）+ (b) dsa-docs/workbuddy.md（106 行）。E2 = 同一份 raw README 的源码级证据（视作该项目的自述文档，仍属 E1，**不冒充 E2**）。E3 不可用（对方不运行，Rule 27/35）→ 上限 partial，且**多数维落在 unknown**
evidence_inline: 见下方「证据重放」节——命令原文与原始输出已搬进本文件正文，读者不需要打开任何外部路径即可复核（采集于 2026-11-06；本轮重放于 2026-11-07）
probe_source: devdoc/competitor-analysis-framework.md §3（17 维探针正则，唯一真源）

## 证据重放（自足：命令原文 + 原始输出）

本节是上方 `pin_substitute`、`official_docs_fetched_at`、`channels`、`census` 与下方各 D 维的**唯一生成过程**。本文件没有可钉的 sha，所以证据链的起点就是「某个时刻取回的那份内容」——这一节把那内容本身抄出来。

### (1) 取回时间戳 —— 逐行原样

取回时每个 URL 追加一行 `时间戳 URL`：

```bash
printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$url" >> official-fetch-log.txt
```

与本文相关的两行（原样，来自同一次采集的日志）：

```
2026-11-06T00:15:23Z https://cnb.cool/codebuddy/codebuddy-code/-/git/raw/main/README.md
2026-11-06T00:15:28Z https://cnb.cool/codebuddy/codebuddy-code/-/git/raw/main/LICENSE.txt
```

⇒ `pin_substitute` 的 `2026-11-06T00:15:23Z` 就是第一行的时间戳，它锚定的 URL 是 `main` 分支的 `README.md`。

### (2) raw README 全文（145 行，原样）

**这是本文件最主要的证据，所以整份抄出。** 它是 `main` 分支在 2026-11-06T00:15:23Z 的内容快照，不是某个提交的内容：

````markdown
# CodeBuddy Code

<div align="center">
  <img src="codebuddy-cat.png" alt="CodeBuddy Code" width="200" height="200">
  
  <h3>腾讯云官方智能编码工具</h3>
  <p>CodeBuddy Code 是一个智能编程助手，深度集成到您的开发环境中，精准理解您的项目结构，通过自然语言交互帮助您高效编程——自动化重复任务、深度解析代码逻辑、优化开发工作流程。支持终端、IDE 无缝使用，让编程更智能、更高效。</p>

  ![](https://img.shields.io/badge/Node.js-18%2B-brightgreen?style=flat-square) [![npm]](https://www.npmjs.com/package/@tencent-ai/codebuddy-code)

[npm]: https://img.shields.io/npm/v/@tencent-ai/codebuddy-code.svg?style=flat-square


</div>

---

## 🚀 快速开始

### 安装

```bash
# 全局安装
npm install -g @tencent-ai/codebuddy-code
```

### 使用

在任意项目目录下运行：

```bash
codebuddy
```

首次使用需要进行身份认证，按照提示完成登录即可。

## ✨ 主要功能

### 🤖 智能编程助手
- **自然语言交互**：用中文或英文描述需求，AI 自动理解并执行
- **代码生成与优化**：根据描述生成高质量代码，自动优化现有代码
- **智能调试**：快速定位和修复代码问题

### 📁 项目理解
- **深度代码分析**：理解项目结构、依赖关系和业务逻辑
- **上下文感知**：基于项目历史和当前状态提供精准建议
- **多语言支持**：支持 JavaScript、TypeScript、Python、Java、Go 等主流语言

### 🛠️ 开发工具集成
- **终端集成**：直接在命令行中使用，无需切换工具
- **IDE 支持**：支持 VS Code、WebStorm 等主流编辑器
- **Git 工作流**：智能处理版本控制、分支管理、代码审查

### 🔧 任务自动化
- **代码重构**：自动重构代码结构，提升代码质量
- **测试生成**：自动生成单元测试和集成测试
- **文档生成**：自动生成 API 文档和代码注释

## 🎯 使用场景

### 1. 日常开发
```bash
codebuddy "帮我优化这个函数的性能"
codebuddy "为这个组件添加错误处理"
codebuddy "生成这个 API 的单元测试"
```

### 2. 代码审查
```bash
codebuddy "检查这次提交的代码质量"
codebuddy "分析潜在的安全问题"
codebuddy "建议代码改进方案"
```

### 3. 学习与探索
```bash
codebuddy "解释这个算法的实现原理"
codebuddy "这个设计模式在项目中如何使用"
codebuddy "推荐适合的第三方库"
```

## 📖 详细文档

### 命令参考

#### 基本用法
```bash
codebuddy [选项] [命令] [提示词]
```

#### 主要选项

| 选项 | 描述 | 示例 |
|------|------|------|
| `codebuddy` | 启动交互式会话（默认） | `codebuddy` |
| `codebuddy [提示词]` | 执行单次任务 | `codebuddy "修复这个 bug"` |
| `-p, --print` | 打印响应并退出（非交互式） | `codebuddy -p "解释这个函数"` |
| `-c, --continue` | 继续最近的对话 | `codebuddy -c` |
| `-r, --resume [会话ID]` | 恢复指定对话 | `codebuddy -r` |
| `--model <模型>` | 指定使用的模型 | `codebuddy --model sonnet` |
| `--ide` | 启动时自动连接 IDE | `codebuddy --ide` |
| `-d, --debug` | 启用调试模式 | `codebuddy -d` |
| `-h, --help` | 显示帮助信息 | `codebuddy -h` |
| `-V, --version` | 显示版本信息 | `codebuddy -V` |

#### 子命令

| 命令 | 描述 | 示例 |
|------|------|------|
| `config` | 管理配置 | `codebuddy config set -g theme dark` |
| `mcp` | 配置和管理 MCP 服务器 | `codebuddy mcp` |
| `doctor` | 检查系统健康状态 | `codebuddy doctor` |
| `update` | 检查并安装更新 | `codebuddy update` |
| `install` | 安装原生构建版本 | `codebuddy install stable` |

注意：部分子命令可能尚未支持。

## 🔒 隐私与安全

### 数据收集
我们仅收集以下数据用于改进产品：
- 使用统计（命令使用频率、功能使用情况）
- 错误日志（用于问题诊断和修复）

### 隐私保护
- **本地优先**：代码分析和处理优先在本地进行
- **数据加密**：所有网络传输数据均经过加密


## 🤝 社区与支持

### 反馈问题
- 在 [Issues 页面](https://cnb.cool/codebuddy/codebuddy-code/-/issues) 中提交详细问题报告
- 通过官方渠道联系技术支持（codebuddy@tencent.com）


## 📝 更新日志

查看 [CHANGELOG.md](CHANGELOG.md) 了解详细的版本更新记录。

---

<div align="center">
  <p>由 <a href="https://cloud.tencent.com/">腾讯云</a> 团队开发维护</p>
  <p>© 2025 Tencent Cloud. All rights reserved.</p>
</div>
````

**与原件的一致性 —— 用 `diff` 说，不用形容词说。** 把上面这段摘出来，与采集日落盘的 `codebuddy-README-raw.md` 比对：

```bash
python3 -c 'import re; d=open("devdoc/competitor/workbuddy.md").read(); m=re.search(r"````markdown\n(.*?)\n````", d, re.S); open("/tmp/inlined-readme.md","w").write(m.group(1)+"\n")'
diff /tmp/inlined-readme.md raw/codebuddy-README-raw.md
wc -c /tmp/inlined-readme.md raw/codebuddy-README-raw.md
```

输出（原样）：

```
146c146
< </div>
---
> </div>
\ No newline at end of file
4632 /tmp/inlined-readme.md
4631 raw/codebuddy-README-raw.md
9263 total
```

⇒ **唯一差异是末尾的换行**：原文件末行 `</div>` 没有结尾换行（`\ No newline at end of file`），上面这段因代码围栏排版补了一个 `\n`，所以 4632 字节对 4631 字节。**除这一个字节外，全文逐字相同**（`diff` 只报 `146c146` 一处）。

规模与行号定位（原地可跑）：

```bash
wc -l < raw/codebuddy-README-raw.md
grep -n 'npm install -g @tencent-ai/codebuddy-code' raw/codebuddy-README-raw.md
grep -n '\-p, --print' raw/codebuddy-README-raw.md
```

输出（原样，三行）：

```
145
24:npm install -g @tencent-ai/codebuddy-code
97:| `-p, --print` | 打印响应并退出（非交互式） | `codebuddy -p "解释这个函数"` |
```

⇒ 「145 行」= 第一条；D01 引用的安装命令在**第 24 行**；下文引用 `-p, --print` 与 `-r, --resume` 时，行号 97、99 就出自这条 `grep -n` 的同一口径。

### (3) `cnb-tree.json` —— 这不是 JSON，是 404 页面

首部 `sha:` 行说 trees API 探测失败、返回 HTML 404 页面。实测：

```bash
head -c 60 cnb-tree.json
wc -c < cnb-tree.json
wc -l < cnb-tree.json
grep -o '<title data-next-head="">[^<]*' cnb-tree.json
```

输出（原样）：

```
<!DOCTYPE html><html lang="zh"><head><meta charSet="utf-8" d
131887
74
<title data-next-head="">Page not found · CNB
```

⇒ 文件名叫 `cnb-tree.json`，内容是 131,887 字节的 HTML，标题就是 `Page not found · CNB`。**这就是「无可钉住的 sha」的直接依据**：trees API 在这个仓库上返回的是站点 404 页，不是树清单。下游若把该文件当 JSON 解析会失败 —— 那是预期的，它本来就不是 JSON。

### (4) 三个官方站点 —— 「JS 渲染的空壳，各 1 行」

```bash
for f in raw/cb-a2a26384.txt raw/cb-c1d1e2a8.txt raw/codebuddy-docs.txt; do printf '%s: ' "$f"; cat "$f"; done
wc -l raw/cb-a2a26384.txt raw/cb-c1d1e2a8.txt raw/codebuddy-docs.txt
```

输出（原样）：

```
raw/cb-a2a26384.txt: 腾讯云代码助手 CodeBuddy - AI 时代的智能编程伙伴
raw/cb-c1d1e2a8.txt: Tencent Cloud Code Assistant CodeBuddy – AI Code Editor
raw/codebuddy-docs.txt: Tencent Cloud Code Assistant CodeBuddy – AI Code Editor
  1 raw/cb-a2a26384.txt
  1 raw/cb-c1d1e2a8.txt
  1 raw/codebuddy-docs.txt
  3 total
```

⇒ 转文本后每个文件**只有 1 行**，且那一行只是 `<title>`：`https://copilot.tencent.com/` 给中文标题，`https://www.codebuddy.ai/docs` 与 `/docs/ide/getting-started` 给同一个英文标题。**正文一个字都没有**——这是 JS 渲染空壳的实测形状，所以 `official_docs_fetched_at` 那三个 URL 在本文件里不构成任何 D 维证据。

（同批还有一份 `raw/cb-0a000668.txt`，128 行，是 `https://cnb.cool/codebuddy/codebuddy-code` 的页面导航文本，不含产品能力正文，故未用作证据。）

### (5) 语料规模与逐维命中数 —— 251 行、各 D 维「N 处命中」

```bash
# $GUIDE = dsa-docs/workbuddy.md 的本地副本（只读快照，不在本仓库内）
echo $(( $(wc -l < raw/codebuddy-README-raw.md) + $(wc -l < "$GUIDE") ))
while IFS=$'\t' read -r dim re; do printf '%s\t%s\n' "$dim" "$(cat raw/codebuddy-README-raw.md "$GUIDE" | grep -icE -e "$re")"; done < probe-regex.tsv
```

输出（原样）：

```
251
```

17 行逐维计数（与文末 `census` 行逐行 `diff` 结果为空）：

```
D01	2
D02	12
D03	13
D04	0
D05	1
D06	2
D07	1
D08	0
D09	0
D10	3
D11	0
D12	1
D13	1
D14	0
D15	0
D16	0
D17	0
```

⇒ 「251 行」= 145 行 README + 106 行 guide，与 `census` 行的声明一致。D04/D08/D09/D11/D14/D15/D16/D17 八维计数为 **0**，这正是本文「多数维落在 unknown」的算术来源。

### (6) E1(b) 的 guide 不在本仓库内

`dsa-docs/workbuddy.md` 是只读快照。本文件对它的每一处引用都把**该行的原文整句抄在备注里**，所以复核那一半同样不需要打开快照；它的 106 行由 `wc -l` 得到（见节 (5) 的 251 行算式）。
census: `e1-census.txt` —— 语料 = 145 行 README + 106 行 guide，共 251 行；逐维命中数见该文件

| 维度 | 等级 | 证据等级 | 证据（file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1 | `workbuddy/raw/codebuddy-README-raw.md:24` | 原文第 24 行 `npm install -g @tencent-ai/codebuddy-code`（第 20 行段标题 `### 安装`、第 23 行注释 `# 全局安装`）。**guide 侧零命中**：dsa-docs/workbuddy.md 第 9 行只写 `Install and sign in to WorkBuddy/CodeBuddy.`，不提命令——两条 E1 通道不一致，以 README 为准并记录差异 |
| D02 | partial | E1 | `dsa-docs/workbuddy.md:9`、`:11`、`:30` | 原文第 9 行 `- Install and sign in to WorkBuddy/CodeBuddy.`（首跑即登录，不需先写配置）；第 11 行 `- Get your API Key from the [DeepSeek Platform](https://platform.deepseek.com/api_keys).`；第 30 行 `setx DEEPSEEK_API_KEY "<your DeepSeek API Key>"` |
| D03 | partial | E1 | `dsa-docs/workbuddy.md:42`、`:57` | 原文第 42 行 `"url": "https://api.deepseek.com/v1/chat/completions",`、第 57 行（第二个模型条目同值）。自定义端点写在 `models.json` 的 `url` 字段里；README 第 100 行另有 `\| `--model <模型>` \| 指定使用的模型 \| `codebuddy --model sonnet` \|` 选项 |
| D04 | partial | E1 | `dsa-docs/workbuddy.md:17`、`:24`、`workbuddy/raw/codebuddy-README-raw.md:110` | 原文第 18 行 `C:\Users\<your-username>\.codebuddy\models.json`、第 24 行 `<your-project>\.codebuddy\models.json`；README 第 110 行 `\\| \`config\` \\| 管理配置 \\| \`codebuddy config set -g theme dark\` \\|`。有载体 + 有 `config` 子命令；"打印生效值"未取证 |
| D05 | partial | E1 | `workbuddy/raw/codebuddy-README-raw.md:97` | 原文 `\\| \`-p, --print\` \\| 打印响应并退出（非交互式） \\| \`codebuddy -p "解释这个函数"\` \\|` —— 具名非交互开关 + 用法示例 |
| D06 | partial | E1 | `workbuddy/raw/codebuddy-README-raw.md:98`、`:99` | 原文第 98 行 `\\| \`-c, --continue\` \\| 继续最近的对话 \\| \`codebuddy -c\` \\|`；第 99 行 `\\| \`-r, --resume [会话ID]\` \\| 恢复指定对话 \\| \`codebuddy -r\` \\|`。**「列/查」无证据**（只有续），故 partial 而非更高 |
| D07 | partial | E1 | `workbuddy/raw/codebuddy-README-raw.md:111` | 原文 `\\| \`mcp\` \\| 配置和管理 MCP 服务器 \\| \`codebuddy mcp\` \\|`。扩展点只有 MCP 一个，且同页第 116 行声明 `注意：部分子命令可能尚未支持。` —— 该子命令是否已实现**未见取证**，按原样记录 |
| D08 | unknown | — | — | 语料无此话题：251 行语料 `grep -ciE 'approv\\|permission\\|allowlist\\|allow_list\\|--yolo\\|dangerously\\|ask before'` = **0**（`e1-census.txt` D08 = 0）。官方文档站三页均为 JS 空壳，无第三通道可查 → unknown（不是 none：该话题的语料**不存在**，不是"看过且没有"） |
| D09 | unknown | — | — | 语料无此话题：`grep -ciE 'sandbox\\|isolat\\|container\\|docker\\|worktree'` = **0**（`e1-census.txt` D09 = 0）；官方文档站空壳 → unknown |
| D10 | partial | E1 | `dsa-docs/workbuddy.md:47`、`:62` | 原文第 47 行 `"supportsImages": false,`、第 62 行 `"supportsImages": false` —— 该配置里图像支持是**关闭的**；README 第 4 行只有 `<img src="codebuddy-cat.png" alt="CodeBuddy Code" width="200" height="200">` 这类徽标图，不是输入通道。**图像输入能力在该产品自有配置面上被显式置 false**，故记 partial（有明确入口参数 `supportsImages`，取值 false） |
| D11 | none | E1 | — | 看过、没有：251 行语料 `grep -ciE 'browser\\|playwright\\|puppeteer\\|chrome\\|selenium\\|click\\|mouse'` = **0**（`e1-census.txt` D11 = 0）；`screenshot` 亦 0。E2 结构性不可用（无树清单可枚举） |
| D12 | none | E1 | — | 看过、没有：最初 1 处命中经 `grep -v` 复核为第 145 行 `© 2025 Tencent Cloud. All rights reserved.` 里的 `reserved` 子串，不是服务面。251 行语料内 `serve\\|--port\\|localhost:\\|API server` 的真实命中 = 0（`e1-census.txt` D12 = 1 即该误命中，已逐条查看） |
| D13 | partial | E1 | `workbuddy/raw/codebuddy-README-raw.md:102` | 原文 `\\| \`-d, --debug\` \\| 启用调试模式 \\| \`codebuddy -d\` \\|` —— 有调试开关；**结构化 jsonl/DB 写入点与按轮次读回的命令均无证据**，故 partial 且只靠一个开关 |
| D14 | unknown | — | — | 语料无此话题：`grep -ciE 'token (count\\|usage\\|limit)\\|--usage\\|/cost\\|cost\\|credits\\|billing'` = **0**（`e1-census.txt` D14 = 0）；官方文档站空壳 → unknown |
| D15 | unknown | — | — | 语料无此话题：`grep -ciE 'LSP\\|language server\\|symbol\\|codebase index\\|indexing\\|grep'` = **0**（`e1-census.txt` D15 = 0）→ unknown |
| D16 | unknown | — | — | 语料无此话题：`grep -ciE 'memory\\|memories\\|knowledge\\|learn\\|CLAUDE\.md\\|AGENTS\.md\\|rules file'` = **0**（`e1-census.txt` D16 = 0）→ unknown |
| D17 | unknown | — | — | 语料无此话题：`grep -ciE 'sub-?agent\\|multi-?agent\\|Task tool\\|delegat\\|/-?agent\\|worker\\|queue'` = **0**（`e1-census.txt` D17 = 0）→ unknown |
