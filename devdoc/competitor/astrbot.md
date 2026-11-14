<!-- doc-class: historical -->

# AstrBot（AstrBotDevs/AstrBot@86c24e8）

<!-- meta -->
repo: AstrBotDevs/AstrBot
sha: 86c24e846c0de78830f6a0bc402049edb0f0c671
default_branch: `master`（`git ls-remote --symref` 实测）
guide: docs/astrbot.md（awesome-deepseek-agent，63 行，抓取 http=200）
url_in_guide: https://github.com/AstrBotDevs/AstrBot
pinned_sha_verified_at: 2026-11-06T00:14Z
license: **AGPL-3.0**（`LICENSE:1-2` "GNU AFFERO GENERAL PUBLIC LICENSE / Version 3, 19 November 2007"）→ 开源，但**传染性许可**
语言/形态: Python（`pyproject.toml:2` `name = "AstrBot"`、`:3` `version = "4.29.4"`、`:4` "Easy-to-use multi-platform LLM chatbot and development framework"）；`astrbot/` 核心 + `dashboard/`（Vue WebUI）+ `openspec/` + `changelogs/`
证据通道: E1 + E2；E3 不产出（框架 §1 + Rule 27/35），上限 `partial`
证据原件: `artifacts/stage2/`（`cite-astrbot.txt`、`e2-astrbot.txt`、`guides/guide-astrbot.md`、`e1-probes.txt`）
取样提示: 本仓 `changelogs/` 有 30+ 个版本文件、README 有多语种副本，粗探针会被 changelog 淹没（D07 粗探针 10751 命中）。因此 E2 用 `astrbot/` 限定 + 排除 `changelogs/dashboard/docs/tests` 的定向探针（`ap.sh`），下表每格都来自定向命中。

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | guide `:14` `curl -LsSf https://docs.astrbot.app/install.sh \| bash`、`:20` Windows `iwr -useb https://docs.astrbot.app/install.ps1 | iex`、`:26-27` `astrbot init` → `astrbot run`；`README.md:74` `uv tool install astrbot --python 3.12`；`:34-42` Docker 路线（`git clone --depth 1` + `sudo docker compose up -d`） | **三条安装路线**（脚本 / uv / Docker），并区分"初始化一次"与"启动"两个 verb；本组安装面最宽的一家 |
| D02 | partial | E1+E2 | guide `:26` `astrbot init # Run this only once to initialize the environment`；`:47-55` 全部配置在 **Web UI** 里点（Providers → + Add → 选 DeepSeek → 粘 key → Save） | 首跑不是终端向导而是 **Web 配置面板**（`http://localhost:6185`）；D02 的实现点落在 dashboard，属"有"但形态独特 |
| D03 | partial | E1+E2 | guide `:53` "select `DeepSeek`, paste your API Key into the `API Key` field"；`astrbot/core/config/default.py:1` 自述配置在 `data/cmd_config.json` 或管理面板可视化修改 | provider 是配置面板里的一等实体（Providers 页可加多个）；base_url 可按 provider 配（`changelogs/v3.5.9.md:8` 记载过 `api_base_url` 的修复） |
| D04 | partial | E2 | `AstrBot@86c24e8:astrbot/core/config/default.py:1` "如需修改配置，请在 `data/cmd_config.json` 中修改或者在管理面板中可视化修改"；`astrbot/core/utils/migra_helper.py:346` `default_path = Path(get_astrbot_data_path()) / "cmd_config.json"` | 配置单一载体 `data/cmd_config.json` + 迁移助手（`migra_helper.py` 管版本升级）；**两种修改入口**（手改 / 面板）且作者明示二者等价 |
| D05 | partial | E2 | `AstrBot@86c24e8:astrbot/core/agent/mcp_client.py:85` `_JS_INLINE_CODE_FLAGS = frozenset({"-e", "--eval", "-p", "--print"})`；`astrbot/core/computer/process_sandbox/windows_setup.py:134` "noninteractive hosts can disable elevation and inspect the failure" | 非交互场景被认真对待（沙箱安装可在非交互主机跑）；但**agent 自身的一次性 headless + 结构化输出**未取到（`--print` 命中是他方 JS 运行时参数）→ partial 下限 |
| D06 | partial | E2 | `astrbot@86c24e8:changelogs/v4.18.0.md:19` "Added AstrBot HTTP API with API Key support for chat, session listing, config listing, file upload, and IM message sending"；`openspec/openapi-v1.yaml:1284`（session 元数据分页） | **会话列举是公开 HTTP API 的一部分**（`openspec/openapi-v1.yaml` 有完整契约）；`--resume` 类 CLI flag 未见 → partial |
| D07 | partial | E2 | `astrbot@86c24e8:astrbot/core/agent/mcp_client.py:412` `self._mcp_server_config: dict \| None = None`；`astrbot/core/astr_agent_tool_exec.py:15` `from astrbot.core.agent.mcp_client import MCPTool`；guide `:5` "extended with skills, plugins, and MCPs" | **三类扩展点**：插件（1000+ 插件市场）、MCP、Skills；guide 与代码一致 |
| D08 | partial | E2 | `AstrBot@86c24e8:astrbot/builtin_stars/builtin_commands/main.py:38` `@filter.permission_type(filter.PermissionType.ADMIN)`；`:45` `@filter.permission_type(filter.PermissionType.SHARED_GROUP_ADMIN)` | **有枚举化的权限级别装饰器**（`PermissionType.ADMIN`、`PermissionType.SHARED_GROUP_ADMIN`，枚举全集用 `ssh digital 'curl -sSL https://raw.githubusercontent.com/AstrBotDevs/AstrBot/86c24e846c0de78830f6a0bc402049edb0f0c671/astrbot/core/pipeline/filter/permission.py' | grep -n 'class PermissionType' -A12` 取回），挂在命令上——这是"命令级策略"而非"每次工具调用审批"，形态与 gopi 的 allow/ask/deny 不同但确实是策略层 |
| D09 | partial | E2 | `astrbot@86c24e8:astrbot/core/computer/process_sandbox/windows.py:311` `class WindowsSandboxProcess:`；`astrbot/core/tools/computer_tools/util.py:162` `elif backend == "seatbelt":`；`README.md:51` "Isolated environment [Agent Sandbox]" | **多后端沙箱**（Windows JobObject / seatbelt / 其他），按平台选 backend；README 把它作为正式特性列出 |
| D10 | partial | E2 | `astrbot@86c24e8:astrbot/builtin_stars/astrbot/main.py:250` `image_urls = []`、`:254` `image_urls.append(await comp.convert_to_file_path())` | 有图像输入（平台消息里的图片转成文件路径再进模型）；guide 未单列 |
| D11 | partial | E2 | `AstrBot@86c24e8:astrbot/core/astr_agent_tool_exec.py:230` `screenshot_tool = tool_mgr.get_builtin_tool(CuaScreenshotTool)`、`:235` `screenshot_tool.name: screenshot_tool` | **有 CUA（computer-use agent）截图工具，且作为内置工具注册给 agent** —— 本组 8 家里 D11 证据最直接的一家（`CuaScreenshotTool` 是具名类，不是文档提及） |
| D12 | partial | E2 | `AstrBot@86c24e8:astrbot/core/platform/webhook_server.py:7` `from fastapi import FastAPI, Request`；help 面 `http://localhost:6185`（guide `:49`）+ `openspec/openapi-v1.yaml` 公开契约 | 有 FastAPI 服务面（webhook 接入平台事件）+ WebUI（6185）+ 版本化 OpenAPI；**服务面是本组最强的一档**，但 webhook 用于平台接入而非 agent 控制面，按"对外服务面"判 partial |
| D13 | partial | E2 | `AstrBot@86c24e8:astrbot/core/utils/trace.py:85` `self.trace_id = format_trace_id(root_context.trace_id)`；`astrbot/core/utils/trace_store.py:376` "A process restart cannot resume spans from a previous process." | **有 OpenTelemetry 级 trace**（trace_id/span_id + 独立的 trace_store），且明写跨进程重启的边界。这是本组痕迹面最强的一家（结构化到 span 级，远超 jsonl） |
| D14 | partial | E2 | `AstrBot@86c24e8:astrbot/builtin_stars/builtin_commands/commands/conversation.py:247` `total_tokens = total_input_other + total_input_cached + total_output`、`:251` `f"Total:          {total_tokens:,}\n"` | 有**面向用户的用量报告命令**（区分非缓存输入 / 缓存输入 / 输出三项再求和）——与 gopi 的 `/cost` 同族，本组唯一一家有该命令的 |
| D15 | partial | E2 | `astrbot@86c24e8:astrbot/core/tools/computer_tools/fs.py:686` `description: str = "Search and read file contents using ripgrep."`；`pyproject.toml:70` `"python-ripgrep==0.0.8 ; python_version < '3.14'"` | 全文检索是一等工具且**依赖里带了 python-ripgrep**（不是靠宿主有 rg）；无符号级定位 |
| D16 | partial | E2 | `AstrBot@86c24e8:astrbot/core/utils/astrbot_path.py:115` `def get_astrbot_knowledge_base_path() -> str:`、`:117` 返回 `data/knowledge_base`；`main.py:78` `os.makedirs(get_astrbot_knowledge_base_path(), exist_ok=True)` | **有知识库目录且启动时创建**（`data/knowledge_base`）——guide `:5` 也列了"База знаний/知识库"；与"记忆"是不同东西，本维按知识库通道判 partial |
| D17 | partial | E2 | `AstrBot@86c24e8:astrbot/core/astr_agent_tool_exec.py:352` "Use per-subagent provider override if configured; otherwise fall back"、`:408` "the subagent asynchronously. When the subagent finishes, a"；`changelogs/v4.21.0.md:88` "Passed `tool_call_timeout` to subagent handsoff/cron/background tasks" | 子 agent 有**provider 覆盖 + 异步完成回调 + cron/后台任务**，`tool_call_timeout` 一路透传——调度面（cron）+ 子 agent 组合在本组最重 |

## 许可证与开闭源结论

**AGPL-3.0**（`LICENSE:1-2`）。开源，但**网络使用即触发开源义务**：把它作为对外服务部署，修改版必须同样以 AGPL 提供。这是本组 8 家里唯一的强传染性许可（其余为 MIT），且与 Langcli 的"无许可"是两个极端。任何"借用其代码"的决定都必须先过这一条——这是本文件里唯一带风险结论的格。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=17 none=0 unknown=0`，`n=17`。
**AstrBot 得分 = 17 × 1 = 17**（本组 17 维无一是 `none` 的两家之一）。
**但许可这条压在分数之上**：AGPL-3.0 使"网络部署即触发开源义务"，所以它与 OpenClaw 同分而可用性判据完全不同——**得分不构成借用建议**。
