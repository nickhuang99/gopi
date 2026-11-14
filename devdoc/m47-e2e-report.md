<!-- doc-class: historical -->
# Task Report: M4.7 增补实测 — HP 真机端到端 report_to/repo 参数传递（t-1788046668 增补）

## Completed
- **链路审读**：dispatchToNode/dispatchRemoteCommand（cmd/agent/dispatch.go）→ 远端 `gopi task run <id> --report-to <alias> --repo <target>`（cli_tasks.go runTaskRun）→ writebackOutcome：①本地 writeback（远端自账）→ ②`ssh <report-to> gopi task update <id> --status X` → ③`git push <repo> HEAD`。ingest（taskingest.go）为本地已有行的反伪造镜像（404/409/400 已在代码确认）。
- **E2E 全链路真机执行**（peerbox → HP → peerbox，task t-1788065578，elapsed 1m40s）：
  - peerbox 造 pending 单（payload=三步 fixture briefing），HP 端预置孪生行（#142 缺口的 fixture 变通）
> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

  - 派发命令 = `cd ~/work/GoPi && nohup gopi task run t-1788065578 --report-to peerbox --repo nick@10.0.0.6:/home/user/work/GoPi &`（镜像 dispatchRemoteCommand 输出 + 显式 cd）
  - 中心镜像 dispatchToNode 的 claimed→in_progress 翻转
- **三断言全过**：
  1. peerbox 台账终态：t-1788065578 status=completed（--report-to → `ssh peerbox gopi task update` 正确落行）
  2. feat 分支到达 peerbox 主仓：feat/m47-e2e-hp @ 39118b9（--repo → `git push nick@10.0.0.6:/home/user/work/GoPi HEAD`）
  3. 内容完整性：`git show feat/m47-e2e-hp:e2e/m47-report-e2e.txt` 逐字等于 fixture 单行
  - 附加：master 停在 f1aff7e 未被污染；HP 端自账 writeback 正常（/tmp/m47-e2e.log 有 "reported completed → peerbox" + "pushed feat/m47-e2e-hp"）
- **拓扑确认**：settings.json compute_nodes 已是 `{name:hp, max_concurrent:2, report_to:peerbox, repo:nick@10.0.0.6:/home/user/work/GoPi}` 四字段完整描述；HP 二进制含 --report-to（master f1aff7e 同基线）。

## Not Done（发现的真实缺口，已另开单）
- **#142（已存在）**：dispatch→remote 纯 ssh 派发不带远端本地行 → 远端 TaskStart/TaskPayload 必失败。本次以"HP 端孪生 fixture 行"变通验证，正式修复待 #142。
- **[bug] dispatch 命令缺 cd（本次新发现，已提交 bug 单）**：wrapper 的 `git push HEAD` 在远端 HOME CWD 执行。HP 的 HOME 恰好是 gitfile 链接 ~/work/GoPi/.git 才碰对分支；HOME 非链接的节点会推错分支或静默跳过 push。修复方向：dispatchRemoteCommand 前缀 `cd ~/work/GoPi`。
- 分支合并：feat/m47-e2e-hp 留在主仓待验收，按规则不自 merge。
- HP 端遗留：~/work/GoPi 存在早前派发参数误生成的 untracked 垃圾目录（peerbox:/ 等，非本次产生）；孪生行已终态，无需清理。

## Verification
- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-e2e ./cmd/agent/` OK
- Tests: `go test ./cmd/agent/ -run 'TestDispatch|TestWriteback|TestComputeNodes|TestResolve' -count=1` → ok
- E2E: 三断言全过（上述），回程 ssh 双向可达预检通过（HP→裸 IP peerbox push-path-ok）
- Commit: 无代码改动（本任务=验证任务）；产物分支 feat/m47-e2e-hp @ 39118b9（recovery: `git show feat/m47-e2e-hp:e2e/m47-report-e2e.txt`）

## 实测配方（给未来 agent）
1. HP 的 gopi **无 version 子命令**——`gopi version` 会落到 agent prompt 路径白跑 session；探测能力用 `gopi task run --help | grep report-to`。
2. 所有 ssh 远端 gopi 命令必须 `</dev/null` + `timeout`（无 stdin 时挂起）。
3. E2E fixture = 中心+远端**同 id 孪生行**（task submit --task-id 两侧各一次），绕过 #142。
4. 回程 push 依赖 remote CWD —— HP HOME gitfile 巧合过关；见 bug 单。
