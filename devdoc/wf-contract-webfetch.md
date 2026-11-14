<!-- doc-class: historical -->
# Task Report: web_fetch 契约测试套件（pa-q-p3-wf-contract 的 testsuite 部分）

**Branch:** `feat/wf-contract`（worktree `~/workspace/gopi-wf-contract`，与 `~/work/GoPi` 共享对象库）
**Date:** 2026-08-29
**Scope:** task #53 的三条验收——①testsuite 并入 web_fetch 契约测试（mock subprocess 零网络）②geo direct 路由断言 ③单行 body 回归用例（example.com 复现）。

---

## Completed

1. **部署端脚本回填 repo（根因修复，commit `5d625d6`）**
   仓库里的 `plugins/websearch/scripts/web_fetch.py` 是旧版：缺 geo-direct 路由
   （`_pick_route`/`_direct_fetch` + SSRF 硬门）和单行 body 修复（`lines[2]` →
   `find+slice`）。线上 `~/.gopi` 已是修复版但从未回写 repo——脚本首次进入版本控制。
2. **`testsuite/webfetch_contract_driver.py`（mock subprocess 零网络）**
   `importlib` 加载**真实** web_fetch.py，monkeypatch `subprocess.run` 为罐头字节流：
   无 ssh、无 curl、无 DNS。任何越界 subprocess 调用会 TypeError 爆炸（零网络 tripwire）。
3. **`testsuite/webfetch_contract_test.go`（4 个 Go 契约测试，14 用例）**
   - `TestWebFetchContract_GeoRoute`：13 探针跑真实 `_pick_route`——DIRECT_DOMAINS
     及子域→direct；其余→ssh；localhost/127/10/192.168/172.16-31/[::1] 全部 SSRF 门拦
     截回 ssh；`evil-bigmodel.cn` 后缀不得过匹配。脚本侧加 `--cov-mark-route` 测试钩子
     （只读路由，不触发 fetch）。
   - `TestWebFetchContract_FetchStatusContract`：10 罐头用例——ok/error(HTTP500)/
     timeout(rc=28)/unavailable(000、无 marker、binary)、尾部截断 marker、清洗契约
     （script/style/nav 剥除、实体反转义）、direct 路由 content-type 标记、direct 失败
     **不静默回退 ssh**（geo 正确性契约）。
   - `TestWebFetchContract_SingleLineBodyRegression`：epic 点名的 example.com 复现——
     压缩成单行的 HTML body 被 `lines[2]` 静默丢成空 content；契约 = chars>0 且正文
     完整（canary `single-line-body-regression-7f3a`）。
   - `TestWebFetchContract_DeployedScriptSync`：`~/.gopi` 部署版与 repo 版漂移即 fail——
     就是本 epic 发现的漂移，此后不再靠生产事故重新发现。
4. `~/.gopi` 部署端已同步为 repo 版（含 `--cov-mark-route` 钩子），同步守卫转绿。
5. `.gitignore` 增加 `__pycache__/`（commit `e27250a`）。

## Not Done（诚实声明）

- **同 epic 的 P1/P1.5（emission contract 单源化 + 退出解耦）不在此报告范围**：另一 gopi
  会话正在同一 worktree 做该部分（未提交的 `internal/agent/loop.go` 修改 +
  `devdoc/wf-contract.md`），本会话未触碰，报告独立命名为 `wf-contract-webfetch.md`。
- `_ssh_fetch` 的 ssh 握手失败路径（`rc!=0`）未单列罐头（与 no-marker 同映射到
  unavailable，由同一断言覆盖）。
- `WEB_FETCH_DIRECT_DOMAINS` 环境变量覆盖行为未测（Go 侧 `t.Setenv` 固定为默认值保证
  路由确定性；env 注入属部署面，非契约面）。

## Verification

- Build: `go build ./...` → OK
- Tests: `go test ./testsuite/` → ok（含 14 个 web_fetch 契约用例全 PASS，见上文 -v 输出）
- 零网络验证：driver 全部 10 个 fetch 用例 elapsed_ms=0、无任何 ssh/curl 真实调用
  （tripwire 机制保证越界即炸）
- Commits:
  - `5d625d6` fix(webfetch): sync deployed web_fetch.py — geo direct routing + single-line body marker fix
  - `7ac4530` test(webfetch): contract suite — mock-subprocess zero-network, geo route, single-line body regression
  - `e27250a` chore(gitignore): __pycache__ from testsuite python drivers

## Recovery

```
git -C ~/work/GoPi show feat/wf-contract:testsuite/webfetch_contract_test.go
git -C ~/work/GoPi show feat/wf-contract:testsuite/webfetch_contract_driver.py
git -C ~/work/GoPi log --oneline feat/wf-contract -3
```
（worktree 与主库共享 refs，commit 已在主库分支可见。）
