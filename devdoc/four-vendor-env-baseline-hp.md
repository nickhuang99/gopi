<!-- doc-class: historical -->
# 四家对照环境的 HP 基线快照（第 1 轮，2026-09-30 实测）

配套设计文档：`briefings/brief-four-vendor-comparison-task-set.md`（任务表 + 反作弊约束）。
本文件只记**环境事实**：逐字版本、逐字 argv、依赖的 env 变量名、会话落点、坑。
原始输出全部落盘在 `~/.gopi/sessions/01a0eeb4-c023-77c9-92e9-9a4b51de9acd/{env,evidence}/`。

## 1. 逐字 `--version`（host: `hostb-Laptop`，2026-09-30T03:51+08:00）

```
$ claude --version
2.1.204 (Claude Code)

$ ~/bin/codex --version
codex-cli 0.0.0

$ $HOME/.npm-global/bin/dsh --version      # 官方 npm 包 @deepseek-ai/dsh@0.2.0-rc.2
0.2.0-rc.2

$ gopi --version
v0.1.0
go: go1.24.5
platform: linux/amd64
revision: 69610d98d6c9412525812bfc50b112f3d5244eb9
built: 2026-09-29T19:49:12Z
```

运行时底座（三家 node 系竞品的依赖）：`node v22.23.2` / `npm 10.9.8`；
codex 的构建链：`cargo 1.98.0` / `rustc 1.98.0`。
逐字原文：`env/versions.txt`（= `evidence/e1-versions.txt`）。

## 2. 四家怎么来的（HP 上的路径，以及为什么是这个路径）

| 家 | HP 路径 | 来历 |
|---|---|---|
| gopi | `gopi` → `/home/user/work/GoPi/bin/gopi`（`/usr/local/bin/gopi` 符号链接） | HP 本地构建件，revision 见上 |
| claude | `/home/user/.local/bin/claude` | 既有安装（256MB 单文件） |
| codex | `/home/user/bin/codex`（1.2 GB） | **从 peerbox 只读拷现成 release 构建件**：`scp peerbox:~/work/codex/codex-rs/target/release/codex ~/bin/codex`（18.7s）。HP 有源码 `~/work/codex/codex-rs` 但无 `target/release`；选拷贝而非 `cargo build` 是为了省掉 10–30 分钟首次构建 |
| dsh | `$HOME/.npm-global/bin/dsh` | **npm 官方包**：`npm install -g @deepseek-ai/dsh@0.2.0-rc.2`（541 包 / 54s）。**不是** scp peerbox 的源码树：dsh 是 TS monorepo（peerbox 上 `~/work/dsh-gopi` 含 node_modules 共 1.7 GB），单文件搬运跑不起来；官方包零构建且可复现 |

⚠️ **版本漂移已实测**：peerbox 上 `dsh --version` → `0.1.0-rc.8`，HP 上 npm 装到的是
`0.2.0-rc.2`。跨机器的 dsh 对照**必须记录版本**，否则不是同一家产品。

## 3. 依赖的 env 变量名（**只有名字，值一律不落盘**）

| 家 | 变量名 | 来源/备注 |
|---|---|---|
| gopi | `DEEPSEEK_API_KEY` / `DEEPSEEK_BASE_URL` / `DEEPSEEK_MODEL`（或 `ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_BASE_URL` / `ANTHROPIC_MODEL`）；数据根 `GOPI_HOME` | HP 用 `~/.gopi/settings.json`（= `~/.gopi/.ds_hp_competition_settings.json` 的符号链接） |
| claude | `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_MODEL` | 实际生效值由 `~/.claude/settings.json` 与进程环境共同决定 |
| codex | `DEEPSEEK_API_KEY` | `~/.codex/config.toml` 的 `model_providers.deepseek.env_key = "DEEPSEEK_API_KEY"` —— **只写变量名**；HP 上这份 config 是本次新写的，`grep -c 'sk-'` = 0 |
| dsh | `DEEPSEEK_API_KEY` / `DEEPSEEK_BASE_URL` / `DSH_MODEL`；数据根 `DSH_HOME`（默认 `~/.dsh`） | 该家在 HP 上 `DSH_HOME` 未设，用默认根，实测可跑 |

本机 shell 中可见的相关变量名清单（无值）：`env/e5-env-names-argv.txt`。

## 4. 逐字 argv 配方（已实测跑通，非 `-h`）

```bash
# 统一落盘变量
export SD=~/.gopi/sessions/<task-id>; mkdir -p $SD/env $SD/evidence

# A. gopi —— 一次运行 = 一个任务
gopi --prompt "Reply with exactly this token and nothing else: HP-COMPETITION-GOPI-OK"
gopi log --last                       # 会话头（Session: <uuid> / Started: <ts> / Workdir:）
gopi stats <session-id>               # rounds / tool 计数 / tokens / cache 命中
gopi code-search <symbol> [--root DIR]

# B. claude —— 非交互
claude -p "…" --output-format text    # json: --output-format json
# 会话控制：--session-id <uuid> / --resume <sid> / -c ；调试：--debug-file <path>
# 隔离：-w, --worktree <name>

# C. codex —— 非交互
~/bin/codex exec -s read-only --skip-git-repo-check -C "$PWD" "…"
# 写文件：-s workspace-write ；事件流：--json ；续跑：codex exec resume --last
# 会话浏览：codex agents ；版本：codex --version

# D. dsh —— 官方 headless profile
export PATH=$HOME/.npm-global/bin:$PATH
dsh headless "…"                      # 一次任务，打印结果退出
dsh --profile headless --dump-config  # 组合后的 profile 配置树
dsh --profile headless --resume <session>
```

## 5. 会话/状态的落点（自省面的物理位置）

| 家 | 落点 |
|---|---|
| gopi | `~/.gopi/sessions/<sid>/`（结构化 JSONL）＋ `~/.gopi/raw/`（逐字 HTTP）＋ `memory.db`（台账/知识同库） |
| claude | `~/.claude/projects/<cwd-slug>/<uuid>.jsonl`（cwd 决定 slug） |
| codex | 会话由 `codex agents` / `codex exec resume --last` 列出（本地 rollout 存储） |
| dsh | `~/.dsh/sessions/<cwd-slug>/`（含 `session_projcache` 存储） |

逐字目录清单：`evidence/e6-session-stores.txt`。

## 6. 已实测的两个坑（下一单直接省下这段）

1. **dsh 的 `dsh --version` 没有 `-h` 之外的坑，但 `--dump-default-config` 需要 `--profile`**：
   `dsh --dump-default-config` → `error: --profile <name> is required`（exit 1），
   必须 `dsh --profile headless --dump-default-config`。
2. **codex 在 HP 上原本没有 provider 配置**（`~/.codex/` 只有 `tmp/`，无 `config.toml`）。
   已按"只写变量名"的方式补上：`model` / `model_provider` / `base_url` / `wire_api = "responses"`
   / `env_key = "DEEPSEEK_API_KEY"`。缺这段则 `codex exec` 直接挂（记忆里的旧教训：拿到二进制
   不等于能跑）。

## 7. 隔离机制的实测（硬边界，未被触碰）

```
sync_peers = []
sync_to    = null
task_sync  = false
memory_peers = ["peerbox"]        # 只同步知识，不影响交付
git remote peerbox push = DISABLED-no-push     # 按设计
$ git push peerbox HEAD:refs/heads/should-not-exist
  → fatal: Please make sure you have the correct access rights and the repository exists.
```

逐字：`evidence/e4-isolation.txt`。**本轮所有产出留在 HP，未回流 peerbox。**
