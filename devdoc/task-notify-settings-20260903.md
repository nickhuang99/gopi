<!-- doc-class: historical -->
# Task Report: notify 配置落盘 settings.json（2026-09-02 stale 投递事故修复）

## Completed

### 事故根因（三言）
8/30 操作员在某 shell 导出 `GOPI_TASK_NOTIFY_CMD`；serve daemon 由另一个
上下文（Hermes 后台 / wrapper）spawn，从未继承该 env → task-events.jsonl
8/30 12:01 停更，而 serve 持续打印 `[stale] reported N`（通知投递到
虚空）。**Env 不是 daemon 的持久配置层**——spawn 上下文一变就丢。

### 修复：三层约定（CLI flag > env > settings.json；settings.json 为持久层）
1. **settings.json "notify" section**（internal/config/config.go）：
   ```json
   "notify": {
     "webhook": "",
     "cmd": "",
     "events": "claimed,in_progress,completed,failed",   // 空 = 代码默认
     "pattern": "",
     "async": false
   }
   ```
   遵循 config.go 已声明的拓扑约定 + FormatEnvTable 约定（settings.go
   EnvVars 目录每行绑定 settings key `notify.webhook/cmd/events/pattern/
   async`，`gopi -h` / `--help-env` 同步展示）。Rule 2：每个 knob 只在一
   处读。

2. **单解析器 `config.ResolveNotify()`**：per-knob 合并 env
   （GOPI_TASK_NOTIFY_*，SET 即覆盖——即使 set 成 "" 也显式禁用该 knob）
   > settings "notify" > 零值（禁用）。settings 读错（缺文件/坏 JSON）
   fail-soft 到 env/default 视图——配置失误静默禁用通知，绝不卡死投递
   路径。env 只用于一次性运行覆盖。

3. **tools 全部通知路径汇聚于 `loadTaskNotifyConfig()`**（唯一 loader，
   改调 config.ResolveNotify）——TaskUpdate 迁移、TaskNext CAS claim、
   stale 扫描（taskheartbeat.go）、terminal-push-failed、legacy-conflict、
   ledger-conflict-human-task、NotifySyntheticEvent 全部自动获得 settings
   层，**零额外接线**（serve 每次触发热读取，改 settings 无需重启）。

4. **帮助文档**：settings.go EnvVars 目录 + tasknotify.go 包注释更新
   （三层约定 + settings 示例）。`[stale] reported N via GOPI_TASK_NOTIFY`
   误导性打印改为 `via task-notify (env/settings)`（投递源已不限于 env，
   cli_resume.go / cli_tasks.go 四处）。

### 本机 settings.json 更新（durable 落盘）
`~/.gopi/.ds_settings.json`（symlink 目标）新增：
```json
"notify": {"cmd": "cat >> /home/user/.gopi/notify/task-events.jsonl",
           "events": "claimed,in_progress,completed,failed,stale", "async": false}
```
events 含 stale（事故事件）。`~/.gopi/notify/` 目录已建。

### serve 启动配方（devdoc 交付）
```bash
# 持久配置只写一次 settings.json（见上）；serve 重启不再需要 notify env。
# 一次性覆盖（某次运行临时改目标）才用 env：
GOPI_TASK_NOTIFY_CMD='cat >> /tmp/oneoff.log' gopi task serve ...
# env 优先级 > settings：serve 继承的环境变量会赢；干净 spawn（无 notify
# env）自动落到 settings 配置。
```

## Not Done
- CLI flag 层无 notify flag（现状无 `--notify-*` flag；三层约定里的 CLI
  flag 是"where one exists"——本功能无 flag，直接 env > settings）。
- t-1788382218（zombie grace disposal）与 t-1788362947（memory_peers
  whitelist）未触碰；notify settings 与 memory_peers 同属 settings schema
  区但互不冲突（memory_peers 在 merge/suite-hang 分支上，本分支从
  master 切出，未含该改动——无重叠文件冲突面：config.go 的 Notify 段与
  memory_peers 段是不同 struct 字段）。

## Verification
- Build: `go build ./...` OK
- Tests（internal/config 全量 + internal/tools notify/zombie/stale 定向）：
  - config: 7 新测试全 PASS（ResolveNotify settings-only / env-wins /
    env-empty-overrides / no-config-disabled / missing-file-fails-soft /
    envBoolValue / FromSettingsFile notify section）+ 全量 config 套件 PASS
  - tools: 4 验收测试全 PASS + notify/zombie/stale 全族 PASS
    （TestTaskNotifySettingsCmdDelivers / EnvWinsOverSettings /
    NeitherConfiguresDisabled / TestStaleEventLandsWithSettingsOnlyConfig）
  - internal/tools 全量套件后台跑（-timeout 1800s，长套件）
- Live E2E（本机 settings-only，notify env 全 unset）：
  `task submit → task update in_progress → task update completed`，
  task-events.jsonl 收到 2 条（in_progress + completed）——acceptance 1 实证。
- Env-vs-settings 优先级：单元 + 集成双覆盖。
- 无任何 notify 配置 = 零投递零错误（backward compat，既有测试守护）。

## 测试卫生（重要）
- **TestMain 守护**（internal/tools + cmd/agent）：套件启动即 unset 五个
  GOPI_TASK_NOTIFY_* env——env 是事故指责的易变通道，dev shell 导出的
  notify env 一旦泄漏进套件，每个走状态机的测试都会打到 ambient 目标。
  unset 后每个测试只能走显式配置（t.Setenv env 路径 或 GOPI_SETTINGS
  沙箱文件 settings 路径）。
- settings 路径测试必须 GOPI_SETTINGS 指向沙箱文件（memory #1311 教训：
  ambient GOPI_SETTINGS → live settings.json 会污染"无配置应禁用"断言）。
- 坑：t.Setenv(env,"") 使 LookupEnv 判 PRESENT → env 覆盖生效（显式禁用
  settings 值）——要"真 unset"得 os.Unsetenv + cleanup 还原
  （config/notify_resolve_test.go clearNotifyEnv）。
- 坑：TaskNext 跳过无 payload 行（ledger entry 不可 dispatch），断言
  claimed 事件的测试必须带 Payload submit。

## Recovery
- 分支：feat/task-notify-settings（worktree ~/workspace/gopi-notify-settings）
- `git show feat/task-notify-settings:devdoc/task-notify-settings-20260903.md`
