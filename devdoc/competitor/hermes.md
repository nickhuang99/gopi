<!-- doc-class: historical -->

# Hermes Agent（NousResearch/hermes-agent@84a0301）

<!-- meta -->
repo: NousResearch/hermes-agent
sha: 84a0301415edd6d1385207ec96e4d7f2b6c1fcd9
guide: docs/hermes.md（awesome-deepseek-agent，33 行，抓取 http=200）
url_in_guide: https://github.com/NousResearch/hermes-agent
pinned_sha_verified_at: 2026-11-06T00:14Z（`git ls-remote --symref` → `refs/heads/main 84a0301415edd6d1385207ec96e4d7f2b6c1fcd9`）
license: MIT（`LICENSE:1`；`package.json:33` `"license": "MIT"`；`pyproject.toml:17` `license = "MIT"`）→ 开源
语言/形态: Python（`pyproject.toml` `[project] name = "hermes-agent"`，`requires-python = ">=3.11,<3.15"`）；三个 console script `hermes` / `hermes-agent` / `hermes-acp`（`pyproject.toml:586-589`）；另有 npm 包装（`package.json:2`）
证据通道: E1 + E2；E3 不产出（框架 §1 + Rule 27/35），上限 `partial`
证据原件: `artifacts/stage2/`（`cite-hermes.txt`、`p2-hermes.txt`、`e2-hermes.txt`、`guides/guide-hermes.md`、`e1-probes.txt`）

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | guide `:16` `curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh \| bash`；`:19` "The only prerequisite is Git"；`pyproject.toml:586-589` 三个 console script | 一行安装器 + 三条入口（`hermes`、`hermes-agent`、`hermes-acp`，后者是 ACP 协议入口）；安装器在 raw.githubusercontent（本机直连超时，经 `digital` 跳板取到 200） |
| D02 | partial | E1+E2 | guide `:29` "When prompted for the model provider, select **DeepSeek**"；`:30` "Enter your DeepSeek API Key"；`pyproject.toml:592` 注释 "Extras that only an explicit selection installs (setup, sync_venv)" | 有 `hermes setup` 向导，且安装本身带 `setup` extra——向导是一等公民 |
| D03 | partial | E1+E2 | guide `:29`（provider 选择列表，多 provider）；`hermes@84a0301:providers/__init__.py:202` `sig = config_cache_version(home / "config.yaml")` | 多 provider 适配 + `config.yaml` 承载；guide 步骤里是"选一个 provider"而非填 base_url |
| D04 | partial | E2 | `providers/__init__.py:202`（`config.yaml` 的 mtime 签名缓存）；`cli-config.yaml.example`（22 KB 示例配置，含逐项注释）；`.gitignore:83` `cli-config.yaml` | 配置有示例文件 + 版本签名缓存（改了会自动重载）；打印生效值未见专门 verb（`D04-show-cmd` 首命中是 LSP 路径工具，同音陷阱已排除） |
| D05 | partial | E1+E2 | `hermes@84a0301:agent/legacy_cli.py`（`hermes-agent` 入口）；`agent/vault_backends/base.py:83` "Run a manager CLI whose non-interactive contract reads the secret from a named env var" | 有非交互命令行；但本维"一次性 headless 执行 + 结构化输出"的专门开关未取到（`D05-headless` 命中都是 vault/后台进程的非交互**约定**，不是 agent 的 headless 模式）→ 停在 partial 下限 |
| D06 | partial | E2 | `hermes@84a0301:hermes_state_messages.py:1452` `def resolve_resume_session_id(self, session_id: str) -> str:`；`agent/image_replay.py:5` "every API-server request, ``--resume``, a gateway restart" | 有 `--resume` + 会话 id 解析；会话存放在 SQLite 式 state 层（`hermes_state_*.py` 一族），不是 jsonl 文件 |
| D07 | partial | E1+E2 | guide `:5` "it creates skills from experience"；`agent/learning_graph.py:94` `for skill_md in root.rglob("SKILL.md")`；`agent/coding_context.py:280` `def _enabled_mcp_servers(config)`；`agent/acp_client.py:491` `{"cwd": self._acp_cwd, "mcpServers": []}` | 扩展面三类：SKILL.md 技能、MCP client、**ACP 协议**（`hermes-acp`）——ACP 是 8 家里独有的接入形态 |
| D08 | partial | E2 | `hermes@84a0301:agent/codex_runtime.py:631` "explicit approval bypass (approvals.mode: off, /yolo, --yolo, HERMES_YOLO_MODE) hands policy to codex's sandbox"；`:632` `auto_approve_requests = False` | 有 `approvals.mode` 枚举 + 逃生阀（`/yolo`、`--yolo`、环境变量），且**把策略交接给下游 sandbox**；默认 `False` |
| D09 | partial | E2 | `hermes@84a0301:cli-config.yaml.example:498` `docker_run_as_host_user: true`；`hermes_constants_scratch_stuck.py:4` "backend without ``docker_run_as_host_user``, a test or review lane's ``docker run -v``" | 一次性私有环境是 **docker 容器级**（可配是否以宿主用户跑）；与 bubblewrap/seatbelt 是不同层次的隔离 |
| D10 | partial | E2 | `hermes@84a0301:agent/chat_completion_helpers.py:371` `_IMAGE_PART_TYPES = frozenset({"image_url", "input_image", "image"})`；`providers/base.py:197` "content (multipart with image_url parts)" | 三种图像 part 类型都识别（跨 provider 归一）；guide 未单列该维 |
| D11 | none | E1+E2 | E1 `D11` 探针 0 命中；E2 唯一命中 `.gitignore:28` `.notebooklm-playwright/`、`:77` `playwright-report/` —— **忽略规则**，不是实现 | 无 capture→act 闭环；playwright 只出现在被忽略的产物目录名里 |
| D12 | partial | E2 | `hermes@84a0301:agent/image_replay.py:5` "every API-server request"；`optional-skills/web-development/scrollcraft/scripts/serve.mjs:30` `http.createServer((req, res) => {` | 有 API server（镜像回放按请求重建，说明服务面存在）；但取样窗口里的 `createServer` 落在**可选技能**里，不是核心实现 → 停在 partial |
| D13 | partial | E2 | `hermes@84a0301:hermes_state_mutation_retirement.py:1` "Durable deletion fences outside prunable transcript rows."；`:87` "the transcript copies a turn result carries" | 痕迹在关系库里（可剪枝的 transcript 行 + 不可删的 deletion fence）——**有明确的保留/删除契约**，比 jsonl 更结构化 |
| D14 | partial | E2 | `agent/side_question_request.py:220` `"api_calls", "input_tokens", "cache_read_tokens", "cache_write_tokens", "output_tokens"`；`agent/context_breakdown.py:125` `real = getattr(compressor, "last_real_prompt_tokens", None)` | 五种计量（含缓存读写）+ 上下文分解（`context_breakdown` 显式取**真实** prompt token 而非估算） |
| D15 | partial | E2 | `hermes@84a0301:agent/lsp/manager.py:49`（LSP 路径配置）；`agent/lsp/install.py:3` "Installs go to a Hermes-owned staging dir, `<HERMES_HOME>/lsp/bin/`" | **有 LSP 管理器 + 自管安装目录**（`<HERMES_HOME>/lsp/bin/`）——符号级通道是真实现的，不只是依赖；全文检索未见专门实现点 |
| D16 | partial | E1+E2 | guide `:5` "It includes a built-in learning loop: it creates skills from experience, improves them during use, persists knowledge"；`agent/learning_graph.py:4` "skills (agent-created or used) plus ``MEMORY.md`` / ``USER.md`` chunks"；`agent/context_breakdown.py:93` `store = getattr(agent, "_memory_store", None)` | **本组最强的一档 D16**：学习图（技能使用次数/类别）+ MEMORY.md/USER.md 分块 + `_memory_store`；它是"自我改进"叙事的实现基础 |
| D17 | partial | E2 | `hermes@84a0301:hermes_state_search.py:1389` "substring indexes exclude (role='tool', cron/subagent sources)"；`hermes_state_messages.py:1773` "subagents (Nous Portal usage tagging)" | 有子 agent + **cron 调度源** + 用量归属（Portal tagging）；队列归调度器而非进程内 |

## 许可证与开闭源结论

MIT（`LICENSE:1` + `package.json:33` + `pyproject.toml:17` 三处一致）。开源；Python 主实现 + npm 包装层，无可疑闭源子目录。

**主机归属（Rule 40）**：这台机器（peerbox/本机）上有 `/home/user/work/hermes-agent` 工作树，但按 Rule 40 hermes-agent 的开发与运行归 **HP**，peerbox 只是 git 上游。本文件只做只读取证，未在该树里写入任何东西。

## 计分（框架 §5.2 / A3）

`full=2 / partial=1 / none=0`，`unknown` 不计。实测计数：`full=0 partial=16 none=1 unknown=0`，`n=17`。
**Hermes 得分 = 16 × 1 = 16**。`none` 的唯一一维是 D11（无 capture→act 闭环；playwright 只出现在被忽略的产物目录名里）。
