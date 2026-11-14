<!-- doc-class: historical -->

# nanobot（HKUDS/nanobot@6a4bb4d）

<!-- meta -->
repo: HKUDS/nanobot
sha: 6a4bb4d270fd3bd7a4c8965503f62308b5d3b5ca
guide: docs/nanobot.md（awesome-deepseek-agent，72 行，抓取 http=200）
url_in_guide: https://github.com/HKUDS/nanobot
pinned_sha_verified_at: 2026-11-06T00:14Z
license: MIT（`LICENSE:1` "MIT License"；`pyproject.toml:7` `license = {text = "MIT"}`）→ 开源
语言/形态: Python（`pyproject.toml:2` `name = "nanobot-ai"`、`:3` `version = "0.4.0"`、`requires-python = ">=3.11"`、`:20` 分类 "Development Status :: 3 - Alpha"）；两个 console script `nanobot` / `nanobot-desktop-tui`（`pyproject.toml:122-124`）；另有 Bun TUI（`tui/`）
证据通道: E1 + E2；E3 不产出（框架 §1 + Rule 27/35），上限 `partial`
证据原件: `artifacts/stage2/`（`cite-nanobot.txt`、`p2-nanobot.txt`、`e2-nanobot.txt`、`guides/guide-nanobot.md`、`e1-probes.txt`）

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | guide `:13` `uv tool install nanobot-ai`；`:31` `nanobot --version`；`pyproject.toml:122-124` 两个入口 | 单一安装路径（`uv tool`），Node/Bun 不参与主装；Windows 需手工加 `%USERPROFILE%\.local\bin` 到 PATH（guide `:16-19`） |
| D02 | partial | E1+E2 | guide `:36` `nanobot onboard`（初始化配置文件）；`nanobot@6a4bb4d:nanobot/agent/tools/self.py:130` `"exec_config shows shell sandbox settings and binds. Shell binds do not grant "` | 有 `onboard` 初始化命令，但**密钥要手工编辑 config.json 填进去**（guide `:59`），向导不代填 |
| D03 | partial | E1+E2 | guide `:59` `"apiKey": "<your DeepSeek API Key>"`、`:60` `"apiBase": "https://api.deepseek.com/v1"` | provider + apiBase 都是显式配置键；多 provider 适配由 `nanobot/providers/` 一族承担（D14 证据同源） |
| D04 | partial | E1+E2 | guide `:44-45` 逐 OS 给出配置路径；`:42` "initialize the nanobot configuration file"；`entrypoint.sh:35` `config="$dir/config.json"`；`entrypoint.sh:38` "initializing $config from render-config.json" | 配置单一载体 `~/.nanobot/config.json`，容器场景由 entrypoint 从模板渲染；`exec_config` 工具可**读回**部分生效值 |
| D05 | partial | E1+E2 | guide `:70` `nanobot agent`（非交互入口）；`nanobot@6a4bb4d:nanobot/providers/xai_grok_provider.py:314` `"x-grok-client-mode": "headless"` | 有专门 verb（`nanobot agent` vs `nanobot-desktop-tui`）；但"结构化输出格式"未见（`D05-headless` 命中是 provider 请求头，不是输出契约） |
| D06 | partial | E2 | `nanobot@6a4bb4d:nanobot/channels/mochat/runtime.py:558` `self.logger.warning("listSessions failed: {}", e)` | 有会话列举（`listSessions`，在 mochat 通道适配层暴露）；`--resume` 类 CLI flag 未取到 → 只到 partial |
| D07 | partial | E2 | `nanobot@6a4bb4d:nanobot/agent/tools/mcp.py:959` `async def connect_mcp_servers(`、`:960` `mcp_servers: dict[str, MCPServerConfig]`；`nanobot/agent/skills.py:43` "Skills are markdown files (SKILL.md) that teach the agent how to use" | MCP client + SKILL.md 技能，两类齐；技能可禁用（`disabled_skills` 参数，`skills.py:47`） |
| D08 | partial | E2 | `nanobot@6a4bb4d:nanobot/agent/tools/self.py:130`（`exec_config` 展示 sandbox 设置与 binds）；`nanobot/skills/tmux/SKILL.md:81-82` `codex --yolo 'Fix bug X'`（**技能文档里**对他方工具的调用示例） | 有沙箱策略可读回（`exec_config`）；**agent 自身的命令审批枚举未取到**，`--yolo` 命中是他方 CLI 的示例 → 停在 partial 下限 |
| D09 | partial | E2 | `nanobot@6a4bb4d:nanobot/agent/tools/sandbox.py:3` "To add a backend, match the callable signature of _bwrap and _seatbelt"；`:133` `def _seatbelt_ancestors(*paths: Path) -> list[str]:` | **两个内核级后端**（bubblewrap + seatbelt），且自述"加后端就照这个签名"——扩展点是设计好的 |
| D10 | partial | E2 | `nanobot@6a4bb4d:nanobot/providers/base.py:1016` "Replace image_url blocks with text placeholder. Returns None if no images found."；`:1025` `if block is not None and block.get("type") == "image_url":` | 有图像 part 处理，且**带文本降级**（provider 不支持图像时换成占位符）；降级逻辑在 base 层，跨 provider 统一 |
| D11 | none | E1+E2 | E1 `D11` 探针 0 命中；E2 唯一命中 `.gitignore:89` `.playwright-mcp/`、`:92` `.playwright-cli/` —— **忽略规则**，不是实现 | 无 capture→act 闭环（与 hermes 同一形态：playwright 只作为被忽略的产物目录名出现） |
| D12 | partial | E2 | `nanobot@6a4bb4d:nanobot/cli/_macos_dock.py:234` `app.run()` | 有 daemon/服务面（macOS dock 启动器）；对外 HTTP API 未见（`D12` 首命中是桌面 app 的 run，不是 `http.Listen`）→ partial 下限 |
| D13 | partial | E2 | `nanobot@6a4bb4d:nanobot/providers/routing.py:112` `if spec and spec.is_transcription_only:`；`nanobot/utils/subagent_channel_display.py:1` "Strip internal subagent inject scaffolding for human-facing channel surfaces" | 通道级产物是"人类可见渲染"与"内部注入"分离的（有专门 strip 层）；按轮次读回的 CLI verb 未见 |
| D14 | partial | E2 | `nanobot@6a4bb4d:nanobot/providers/openai_codex_provider.py:58` `from nanobot.utils.helpers import estimate_prompt_tokens`、`:76` `"input": usage.input_tokens - cached_tokens` | **两者都有**：既用服务端 usage（减缓存），也有本地 `estimate_prompt_tokens`；`/cost` 类命令未见 |
| D15 | partial | E2 | `nanobot@6a4bb4d:nanobot/agent/tools/rg.py:32` "Search file contents and discover files with ripgrep." | 全文检索是一等工具（`tools/rg.py` 包装 ripgrep）；**无符号级定位**（`D15-lsp` 在源码 0 命中，`tui/bun.lock` 命中是依赖锁文件） |
| D16 | partial | E2 | `nanobot@6a4bb4d:nanobot/utils/helpers.py:899` `_write(tpl / "memory" / "MEMORY.md", workspace / "memory" / "MEMORY.md")`；`:918` "memory/MEMORY.md" | 有 workspace 级 `memory/MEMORY.md` 模板（脚手架会生成）；跨会话检索层未见 |
| D17 | partial | E2 | `nanobot@6a4bb4d:nanobot/utils/subagent_channel_display.py:1` "Strip internal subagent inject scaffolding for human-facing channel surfaces."；`nanobot/agent/subagent_announce.md` | 有子 agent 且**有专门的公告协议文件**（`subagent_announce.md`）+ 面向通道的剥离层；跨会话队列未见 |

## 许可证与开闭源结论

MIT（`LICENSE:1` + `pyproject.toml:7`）。开源；Python 主包 + `tui/` 下的 Bun 前端（`tui/bun.lock` 锁定版本）。项目自评 Alpha（`pyproject.toml:20`），本文件不做成熟度推断。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=16 none=1 unknown=0`，`n=17`。
**nanobot 得分 = 16 × 1 = 16**。`none` 的唯一一维是 D11（无 capture→act 闭环；playwright 只出现在被忽略的产物目录名里）。
