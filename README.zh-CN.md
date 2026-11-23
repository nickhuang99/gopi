<!-- doc-class: normative -->

<div align="center">

# gopi

**一个 agent harness —— AI 编程 agent 跑在上面的那一层，而且它自己会记账。**

*Harness*（智能体运行框架）这个词很多人不认识，所以放在第一句。市面上大多数
"AI 应用"**不是** harness：它们是跑在 harness 之上的应用。gopi 是 harness 本体 ——
一个完整的编程 agent（对话、文件工具、记忆、可续跑的会话），**外加**只有 harness
才需要、应用不需要的那套机制：工作账本、派发、每个 worker 一个独立 clone、自合并交付。

你的编程 agent 在上下文被压缩的那一刻就忘了这件事。
gopi 把它记成**账本里的一行**，交给一个在独立 clone 里跑的 worker，
做完之后把经验留下来 —— 全部装在一个零运行时依赖的静态 Go 二进制里。

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)](https://go.dev/dl/)
[![No CGO](https://img.shields.io/badge/cgo-disabled-success.svg)](#为什么是单二进制)

[English](README.md) · 简体中文 · [安装](INSTALL.md) · [命令](COMMANDS.md) · [配置](CONFIG.md)

</div>

![四帧：构建它、读账本、交一个任务、让 worker 在独立 clone 里跑完](docs/quickstart.gif)

*上面每一帧都是一个真实命令的真实输出 —— 由 `gopi tool text2img` 渲染成 PNG、
由 `gopi tool gif` 拼成 GIF。两个都是这个二进制自己。整条链没有 ffmpeg、没有
ImageMagick（[复现配方](docs/quickstart.md)）。*

---

## 30 秒上手

```bash
git clone https://github.com/nickhuang99/gopi.git && cd gopi
make build
./bin/gopi --version
```

然后：

```bash
./bin/gopi config setup      # 幂等：缺什么补什么，已有的一个字都不动
./bin/gopi chat              # 开始对话
```

构建需要 **Go 1.24+**，跟模型说话需要**一个 API key**。
别的都不要 —— 不要 Node、不要 Python、不要 Docker、不要 `node_modules`。

---

## 它和别的 agent 哪里不一样

大多数 agent harness 回答的是「怎么跟一个会改文件的模型聊天」。
gopi 回答的是另一个问题：**一个人怎么在没有雇员的情况下运营一家软件公司。**
问题不一样，核心对象就不一样。

### 1. 工作单位是账本里的一行，不是一段对话

主流 agent 里，你说完、上下文压缩、这件事就不留痕迹了。在 gopi 里，一件工作是一**行**：
有 id、有状态机、有认领人、有交付分支、有一个几个月后还能翻出来读的会话目录。

```bash
$ gopi task submit "把 config 加载路径从相对改成绝对" --tags refactor
Task queued: 01a201ec-3cbc-7af2-ace5-e554fbb102dc (status=pending, source=agent, tags=refactor)
```

这一行能挺过崩溃、重启和 `rm -rf ~/.gopi`。事后可以问它任何事：

```bash
gopi task show 01a201ec-3cbc-7af2-ace5-e554fbb102dc   # 状态、认领人、父行、payload 修订史、交付证据
gopi task list --status pending                        # 什么能被认领，以及某行为什么被挡住
```

**失败不是终点。** `gopi worker --resume <id>` 在同一行、同一目录里接着跑，checkpoint 追加；
`gopi task clone <id>` 给一个干净房间，从头重做。该用哪个由 `gopi task status <id>` 的**退出码**
决定，不由你对当时情况的记忆决定。

### 2. worker 在自己的 clone 里跑，自己合并

整个系统存在的意义是：**持有上下文的那个不是你。**
`gopi worker` 认领一行、把仓库 clone 到私有目录、干活、构建、拉集成分支、
**自己解决冲突**、推送。然后这一行带着证据。

```bash
$ gopi worker 01a201ec-3cbc-7af2-ace5-e554fbb102dc
# ...在隔离环境里跑完...
clone-channel: attempt 1/M: push accepted
```

交不出去的运行会点名说清楚（`clone channel: delivery did not land — …`），**不会沉默**。

### 3. 记忆是一个子系统，不是外挂一个向量库

三层，全部可查、可搜、可投票：

| 层 | 装什么 | 怎么问 |
|---|---|---|
| 蒸馏 | 一行一条可复用经验：主题 / 问题 / 答案 / 关键词 | `gopi memory search "为什么 build 会红"` |
| 流程 | 可复用工作流，按步骤排列 | `gopi memory search "<任务>"` |
| 会话索引 | 过去每一次运行的每一轮，各一条可搜笔记 | `gopi session notes "<query>"` |

检索是**自动的** —— 相关的经验在你问之前就被预取进提示词。
反馈会推动置信度，所以差的沉下去、好的浮上来。

### 4. 它能看屏幕、也能操作屏幕

这不是外挂一个截图工具：是手写的 X11 协议客户端，纯 Go，零 CGO。
捕获 → 读 → 操作 → 验证，每一步都有像素哈希证明它真的变了。

```bash
gopi tool capture --self                    # gopi 自己所在的那个终端，按 pid 解析，不会抓错窗口
gopi tool vision_analyze --image shot.png   # 这张屏幕说了什么？
gopi tool act move 640 400                  # 操作真实桌面
```

---

## gopi 与其他主流 harness

| | gopi | Crush | Claude Code | Pi | Codex CLI |
|---|---|---|---|---|---|
| **运行时依赖** | 无（静态二进制） | Go 二进制 | Node.js 18+ | Node.js 22.19+ | 见其 README |
| **工作单位** | 账本行，跨会话存活 | 会话 | 会话 | 会话 | 会话 |
| **上下文压缩后工作还在吗** | 在 —— 它就是一条数据库记录 | 不在 | 不在 | 不在 | 不在 |
| **无人值守派发** | `gopi worker` 认领并跑完 | README 未提及 | README 未提及 | print/JSON 模式 | README 未提及 |
| **每个任务的隔离** | 独立 git clone，自动回收 | README 未提及 | README 未提及 | README 未提及 | sandbox |
| **长期记忆** | 三层 + FTS + 反馈 + 自动预取 | README 未提及 | README 未提及 | README 未提及 | README 未提及 |
| **桌面控制** | 纯 Go X11，无依赖 | README 未提及 | README 未提及 | README 未提及 | README 未提及 |
| **多模型** | DeepSeek（自带 key），Anthropic/OpenAI 兼容经 proxy | 多家 | Anthropic | 多家 | OpenAI |

<sub>「README 未提及」就是这个意思：本表写作时，那个字符串没有出现在该项目自己的 README 里。
它**不是**「对方没有这个能力」的断言 —— 其中大多数通过插件能做到更多。请把每一行读成
「各家选择宣传什么」，绝不要当成功能审计。数字与引文均取自各项目自己的 README，
依赖本表之前请自行复核。</sub>

**gopi 弱在哪里，诚实写出来：** 没有 MCP、只有 Linux 发布包（macOS 能交叉编译但没真跑过）、
开箱只有一家模型、没有 npm/brew 包。见[§ 边界](#边界)。

---

## 安装

挑适合你的一种。

### 下载预编译二进制（不需要工具链）

每个 release 都带一个可直接运行的二进制，由 GitHub Actions 构建：

**[→ 最新 release](https://github.com/nickhuang99/gopi/releases/latest)**

| 平台 | 文件 |
|---|---|
| Linux x86-64 | `gopi_<version>_linux_amd64` |
| Linux ARM64 | `gopi_<version>_linux_arm64` |

```bash
VER=v0.5.2
curl -fLO "https://github.com/nickhuang99/gopi/releases/download/${VER}/gopi_${VER}_linux_amd64"
curl -fLO "https://github.com/nickhuang99/gopi/releases/download/${VER}/gopi_${VER}_linux_amd64.sha256"
sha256sum -c "gopi_${VER}_linux_amd64.sha256"
chmod +x "gopi_${VER}_linux_amd64"
./gopi_${VER}_linux_amd64 --version
```

然后放进 `PATH`：

```bash
mv gopi_${VER}_linux_amd64 ~/.local/bin/gopi
```

**发布包只带 Linux，但 macOS 现在能交叉编译了。** darwin 的平台拆分已落地（2026-11-23）：
darwin 的 `syscall.Fsid` 字段叫 `Val`（linux 叫 `X__val`）、`Statfs_t.Type`/`Bsize` 是 `uint32`、
`Stat_t` 的时间戳叫 `Ctimespec`/`Mtimespec`、`SysProcAttr` **没有** `Pdeathsig` 字段 ——
每一处现在都在各自的平台文件后面。实测于该版本：`GOOS=darwin GOARCH=arm64` 产出一个可用的
`Mach-O 64-bit arm64 executable`（72 MB），linux 仍然照常构建。

**Windows 构建不了**，而且短期内也不会 —— 缺口不是语法。`syscall.RLIMIT_AS` / `Rlimit` /
`Getrlimit`（rlimit）、`O_NOFOLLOW`、以及一批 statfs 调用点在 Windows 上没有对应物，
每一处都需要真机制而不是改个字段名。发布流只发布实测能构建的两个目标，而不是三个里有一个永远红。
见[§ 边界](#边界)。

**darwin 是「编译通过」，不是「跑过」** —— 手上没有 macOS 机器，所以请当作「它能构建」而不是
「它能用」。已知三处降级，每处都在自己的调用点上写明：`Pdeathsig` 不存在（被 SIGKILL 的父进程
留下的 filmed 子进程不会被内核回收）、pty 文本抓取不可用（要 `TIOCPTYGNAME` 加
`grantpt`/`unlockpt`，属 libc，而本二进制是 `CGO_ENABLED=0`）、`cgroupscope` 依赖的
`/proc/self/cgroup` 在 darwin 没有对应物。

### 先装 Go —— 克隆之前请读这一段

gopi 需要 **Go 1.24.0 或更新**，这是硬性要求，不是偏好。`go.mod` 声明 `go 1.24.0`，
而且**四个直接依赖在各自的 `go.mod` 里声明了同样的下界** —— `modernc.org/sqlite v1.44.0`、
`golang.org/x/sys v0.37.0`、`github.com/charmbracelet/bubbletea v1.3.10`、
`github.com/go-ego/gse v1.1.0`。旧工具链会直接拒绝构建，没有绕过的路。

```bash
go version          # 必须打印 go1.24.0 或更高
```

**不要用发行版的包管理器装。** Ubuntu 22.04 的候选版本是 `2:1.18~0ubuntu2` —— 低了六个
版本 —— 所以 `sudo apt install golang-go` 装出来的工具链根本编不动这棵树。
（实测：`apt-cache policy golang-go`。）请装上游工具链：

**Ubuntu / 任意 x86-64 Linux**

```bash
curl -fLO https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin     # 同时把这行写进 ~/.bashrc 或 ~/.profile
go version                              # go1.24.0 linux/amd64
```

ARM64 把两条命令里的 `linux-amd64` 换成 `linux-arm64`。

**macOS** —— Homebrew（`brew install go`）或 <https://go.dev/dl/> 的官方 `.pkg`，两者都是
当前版本。用 `go version` 确认。

**Windows** —— <https://go.dev/dl/> 的官方 MSI 安装包。在新终端里用 `go version` 确认。

> 如果你的 `go version` 偏旧、以为 Go 会自己升级：它不会。`GOTOOLCHAIN=local` 是很多环境里的
> 默认值，而 `go.mod` 里比工具链更新的 `go` 行是硬构建错误，不是自动升级的触发条件。

### 从源码

```bash
go version                         # 必须 1.24.0 或更新 —— 硬性要求
git clone https://github.com/nickhuang99/gopi.git
cd gopi
make build                         # → ./bin/gopi
./bin/gopi --version
```

放进 `PATH`：

```bash
make install                        # → $(go env GOPATH)/bin/gopi
make install PREFIX=/usr/local/bin  # 或者一个已经在 PATH 里的地方
```

### 自带 key

gopi **不附带任何凭据**。它围绕 **DeepSeek** 构建；`settings.example.json` 里的模型名是占位符，
换成你自己从 <https://platform.deepseek.com/> 拿的 key。

```bash
cp settings.example.json ~/.gopi/settings.json
$EDITOR ~/.gopi/settings.json       # 替换 "PUT-YOUR-OWN-DEEPSEEK-KEY-HERE"
```

没有 key gopi 照样能启动，所有不调用模型的命令照样能用 —— 它会告诉你缺哪个 key，
而不是静默失败。

### 语音输入（可选，而且它**不是** DeepSeek）

听写是一套**独立子系统，有自己的账号、自己的账单**。两个引擎，都不是 LLM 供应商：

| 引擎 | 磁盘上的权重 | 音频是否离开本机 | 你需要什么 |
|---|---|---|---|
| `--engine doubao`（默认） | **0 B** —— 什么都不用下载 | **是**，会上传 | 火山引擎（字节）SAUC 账号：app id + access token |
| `--engine local` | **磁盘 1.7 GB**（每次运行加载 910 MB） | **否** | 那些权重文件，外加一个约 6 MB 的运行时，得自己编译 |

**云端引擎。** 语音转文字走的是火山引擎 SAUC（豆包大模型 ASR），**不是 DeepSeek**。
不同厂商、不同控制台、不同发票。`settings.example.json` 里注释掉的 `_voice_optional`
段落写了形状 —— endpoint `wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async`，
resource id `vol.seedasr.sauc.duration`。**不填那两个 key 什么都不会坏**：调用语音路径时
它报「未配置」，其余命令一律不受影响。

**本地引擎。** Fun-ASR-Nano（SAN-M 编码器 + Qwen3-0.6B），通过官方 FunASR 的
**llama.cpp** 运行时跑在你自己的 CPU 上 —— 磁盘上 1.7 GB 权重（每次运行加载 910 MB）
永不出磁盘，音频永不出本机。
同一段音频、同一个打分器，实测：

```
Fun-ASR-Nano（本地）   CER 1.58%   0.32x 实时
whisper-small          CER 5.14%   0.95x 实时
whisper-medium         CER 2.77%   3.4x  实时
```

精度比 whisper-medium 还好，算力只要它的十分之一。代价是诚实的，而且**是部署代价，
不是质量代价**：你得自己弄到权重（磁盘 1.7 GB）、自己编译一个 C++ 运行时。gopi 既不分发、
也不替你下载。

gopi 按顺序找本地部署：

```
<gopi 配置目录>/voice          即 ~/.gopi/voice   ← 优先
$HOME/work/funasr-models                        ← 旧位置，只读
```

两处都没有时它**不加载任何东西**，而是点名缺哪个文件、告诉你该放进哪个目录，
并告诉你怎样完全不需要它（`--engine doubao`）。接近 2 GB 的权重加一个外来的运行时，
**没装是正常的**，gopi 就是这么对待它的 —— 而不是当成装坏了。

**如果你根本不需要听写，这一整节可以跳过。** gopi 里没有别的东西碰它。
细节见 `devdoc/doubao-audio-endpoint-credential-config.md` 与
`devdoc/audio-engine-config-surface.md`。

### 验证安装

```bash
gopi config setup     # 幂等准备
gopi config show      # 实际生效的配置，以及每一个值是被哪个通道决定的
gopi chat             # 开始对话
```

`gopi config show` 打印的是**解析后的结果**，不是文件内容，而且每个值旁边写着哪个通道赢了
（命令行 flag / 环境变量 / settings.json / 默认值）。当行为和你在文件里写的对不上时，
读那一页。

**平台：Ubuntu 22.04 LTS、x86-64、Go 1.24.x** —— 唯一跑过的组合。
macOS、Windows、ARM、其他发行版都没测过；诚实清单在 [`INSTALL.md` §6](INSTALL.md)。

---

## 它能做什么

下面每一条命令都**真的能跑**，没有一个是示意。

### 对话与执行

| 我想 | 命令 |
|---|---|
| 跟它说话 | `gopi chat` |
| 一次干完一件小事，不进循环 | `gopi chat --once -p "把这个函数的错误分支补上"` |
| 派一件活，稍后收结果 | `gopi task submit "<这件事>"` |
| 看某件活现在走到哪一步 | `gopi task show <id>` |
| 找没人认领的活 | `gopi task list --status pending` |
| 无人值守地消费队列 | `gopi worker` |

### 记忆与检索

| 我想 | 命令 |
|---|---|
| 让它记住一条可复用经验 | `gopi memory store --topic "..." --question "..." --answer "..."` |
| 问它以前是否碰到过 | `gopi memory search "为什么 build 会红"` |
| 找某个符号的定义 | `gopi tool code-search <Symbol>` |
| 全文搜索代码树 | `gopi tool keyword_search "<pattern>"` |
| 查某一行是谁写的、为什么 | 会话内 `git_blame` 工具；命令行 `git blame` |

### 看与操作

| 我想 | 命令 |
|---|---|
| 捕获当前桌面 | `gopi tool capture --screen --out s.png` |
| 捕获它自己所在的那个终端 | `gopi tool capture --self` |
| 操作桌面 | `gopi tool act move 100 100` |
| 读出图片里是什么 | `gopi tool vision_analyze --image p.png` |
| 把文本渲染成图片（支持中日韩） | `gopi tool text2img --src notes.txt` |
| 给网页截图（无头，不碰真实屏幕） | `gopi tool shot --url https://example.com` |

### 配置与自省

| 我想 | 命令 |
|---|---|
| 看**实际生效**的配置 | `gopi config show` |
| 列出所有环境变量 | `gopi config show --help-env` |
| 把一台新机器准备好 | `gopi config setup` |
| 查任意动词的用法 | `gopi <verb> -h` |

### 隔离与清理

| 我想 | 命令 |
|---|---|
| 在私有 profile 里跑一次，不污染主账本 | `gopi tool sandbox run --label probe -- <cmd>` |
| 看临时目录里哪些可以回收 | `gopi tool scratch-gc --dry-run` |
| 清理已完成任务的工作目录 | `gopi task sweep-worktrees` |

### 服务面

| 我想 | 命令 |
|---|---|
| 本地 Web UI | `gopi web --addr 127.0.0.1:8080` |
| 给另一个会话投递消息 | `gopi notify --session <id> --message "..."` |

---

## 边界

gopi **不是**「什么都能」。下面这些是刻意的取舍，不是待办事项：

- **没有 MCP。** 有一个插件树，但没有 Model Context Protocol 客户端。加 MCP 意味着
  一个新协议加一个常驻连接，这与「零依赖、单二进制」相冲突。**是在等裁决，不是在等实现。**
- **不提供在线安装脚本。** 没有 `curl … | sh`。一个从网络上下来、用你的权限执行的脚本，
  是一个不该轻易引入的依赖。
- **它不会替你在别人的机器上装软件。** 取证、探测、验证都允许；
  「装一个第三方 agent 好让某个测试能跑一次」是明确禁止的。
- **不承诺在设备压力下的延迟。** gopi 不为测量什么而制造负载，所以对争用下的性能不作任何声称。
- **平台收敛到两个，但只有一个真跑过。** 唯一**跑过**的组合是 Linux x86-64。Linux ARM64 会构建并发布
  —— 已实测能编译、能带上版本戳，但**没有**实测能执行，因为 CI runner 是 amd64 且没配模拟器。
  macOS 自 2026-11-23 起**能交叉编译**（darwin 的平台拆分：`Fsid.Val` vs `X__val`、`Statfs_t.Type`
  的符号性、`Ctimespec`/`Mtimespec`、缺失的 `Pdeathsig`），实测产出可用的 arm64 Mach-O ——
  但**从未在 macOS 上运行过一次**，所以它是「能构建」而非「能用」。
  Windows 仍然**编译不过**，且缺口不在语法：rlimit、`O_NOFOLLOW`、一批 statfs 调用点在
  Windows 上没有对应物，需要真机制。
- **这个公开导出仓不带测试套件。** `make test` 跑的是 `go build ./...` 加 `go vet ./...` ——
  在纯产品代码上唯一有意义的两项检查。`_test.go` 与 `internal/standards/` 留在了私有树里。
  **这些文档里引用的任何 `go test` 命令，点名的测试都不在本仓库中：它会打印
  `no tests to run` 并退出 0。那是空转的绿灯，不是通过的检查。** 请用跑二进制的方式验证行为。

---

## 文档

| 你想知道 | 去哪看 |
|---|---|
| **在一台新机器上装起来，以及到底需要什么** | [`INSTALL.md`](INSTALL.md) |
| **每一个 CLI 动词的完整用法** | [`COMMANDS.md`](COMMANDS.md) —— 421 行，按动词排列 |
| **每一个配置键的默认值、优先级、以及读它的那行代码** | [`CONFIG.md`](CONFIG.md) —— 1649 行 |
| **某个子系统的正式说明** | [`man/`](man/) —— 14 页 |
| **代码为什么长这样** | [`PROJECT.md`](PROJECT.md) —— 37 条工程约束 |
| **设计决策背后的实测** | [`devdoc/`](devdoc/) —— 每份文档第 1 行声明自己的 doc-class |
| **改了什么** | [`CHANGELOG.md`](CHANGELOG.md) |

> 上表中的数字来自 `wc -l <file>` 与 `ls man/*.md | wc -l`。请自行重跑；
> 这个导出仓不带任何能发现数字过期的门。

---

## 状态

`v0.5.2` —— 第一个公开版本。在活跃开发中：接口会变，但每一次变化都记在 `CHANGELOG.md`。

```bash
$ ./bin/gopi --version
v0.5.2
go: go1.24.0
platform: linux/amd64
revision: <你构建时那个提交>
```

`revision` 在构建时写死，所以它指的是**你的**二进制来自哪个提交 —— 这就是它不写在这份文档里的原因。

## 许可证

MIT —— 见 [`LICENSE`](LICENSE)。
