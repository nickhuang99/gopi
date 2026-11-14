<!-- doc-class: historical -->
# Task Report: web_search 增加 github 搜索方法

## Completed

- 新增插件脚本 `~/.gopi/plugins/websearch/scripts/github_search.py`（同时同步进仓
  库 `plugins/websearch/scripts/github_search.py`），与其他方法同风格。
  - 用法：`python3 github_search.py "<query>" [--count N] [--type repositories|code|issues]`
  - 走 search-bot 跳板（`ssh remotebox 'curl ...'`，复用 google/bing 的 SSH 模式，base64 打包远程脚本防注入）。
  - 主路径：GitHub REST API `api.github.com/search/{repositories,code,issues}`。
    - `repositories` / `issues`：免认证，实测返回结果。
    - `code`：免认证返回 401 Requires authentication → 需 `GITHUB_TOKEN`/`GH_TOKEN`
      env（脚本已支持读该 env 走 Bearer auth）；无 token 时诚实降级 `unavailable`。
  - 兜底：API 限速/失败 → GitHub 网页搜索 `github.com/search?q=..&type=..`
    服务端渲染 JSON（`<script data-target="react-app.embeddedData">` 内
    `payload.blackbirdSearchRoute`）解析。code 网页搜索需登录 → 返回诚实 sign-in 门槛提示。
  - 统一契约：`{"status":"ok|empty|unavailable","method":"github","results":[{title,url,snippet}],"count":N}`
    snippet 强制 ≤200（含省略号，Rule 1 无魔法数字：SNIPPET_CHARS=200）。
- 接入 gopi web_search：
  - `internal/tools/websearch.go`：`webSearchScripts` 加 `"github"→"github_search.py"`；
    新增 `webSearchMethodArgs`（github 固定传 `--type repositories`，仓库搜索是免认证
    最高价值默认）；`runWebSearchScript` 附加 method 专属参数；`unknown method` 提示、
    `WebSearch` 文档更新。
  - `internal/agent/loop.go`：web_search 工具描述 methods 加
    `github (repos/issues via search-bot; code needs a token)`；`method` enum 加 `github`。
  - `internal/tools/websearch_test.go`：新增 `TestWebSearchGitHubTypeArg`（断言
    github 方法把 `--type repositories` 与 query/count 传进脚本）。
- 更新 `.gopi` 插件 skill 文档（方法表、路由规则、用法示例）。

## Not Done

- code 类型带 token 的端到端验证未做（本机无 GITHUB_TOKEN/GH_TOKEN）。代码路径已实现
  （读 env → 远程 curl 附 Bearer header），但未实测。无 token 的降级路径已真实验证
  （返回诚实 `unavailable` + 说明）。
- github 未纳入多方法 `merged` 默认集（bing/google/arxiv 并发融合）。github 是
  单方法检索，保留用户显式选择。

## Verification

- 脚本真实测试（search-bot 实时）：
  - `github_search.py "modernc sqlite" --type repositories --count 3` → `ok`, 3 条，
    top=`cznic/sqlite · Go`，`source: github api`，snippet ≤200。
  - `github_search.py "sqlite bug" --type issues --count 3` → `ok`, 3 条，
    top=`[krille-chan/fluffychat] SQLite bug`。
  - `github_search.py "modernc sqlite" --type code --count 2`（无 token）→
    `unavailable`, `code search requires authentication (401): no GITHUB_TOKEN and
    the web UI needs sign-in ...`（诚实降级）。
  - 网页兜底验证：`web_search()` 对 repositories/issues 返回真实结果；空结果查询 →
    `empty`。
- gopi 集成真实测试：用本任务构建的二进制跑短任务
  「search github for the modernc.org/sqlite repository…USE web_search method github」：
  - 工具定义含 `github` 方法 + enum；agent 实际调用了多次 `method:github`，
    日志确认 `source: github api` 出现 5 次，agent 正确拿到 `cznic/sqlite`(348 stars)
    作为目标仓库。
- Build: `go build ./...` OK。
- Tests: `go test ./internal/tools/` 全量 PASS（含新增 `TestWebSearchGitHubTypeArg`）。
- Commit（GoPi 主仓 worktree）: `feat/github-search` 分支…
- Commit（.gopi 插件仓）: `7b30aec feat(websearch): github_search.py — …`
