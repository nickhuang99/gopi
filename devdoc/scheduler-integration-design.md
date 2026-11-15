<!-- doc-class: roadmap -->
# GoPi ↔ Scheduler Integration — Design

> 源自: ~/work/claude/the operator/gopi_scheduler_integration_design_talk.txt Part 2
> 日期: 2026-08-02
> 状态: 设计阶段。GoPi 和 Scheduler 各自独立开发，集成在未来阶段。
>       不污染 Scheduler 项目的核心调度逻辑。

## 目标

GoPi 当前是同步 ReAct loop —— 发起一个 SSH 命令后阻塞等待，无法处理：
- 远端慢任务（编译、部署、长脚本）
- 分步增量决策（走一步看一步，非提前规划全部步骤）
- 跨会话任务（进程重启后继续未完成工作）
- 上下文溢出（单轮处理不完大文件或复杂任务）

## 核心架构

```
GoPi (LLM推理)                     Scheduler (时序管理，独立包)
     │                                      │
     │  RegisterTask(callback, trigger)     │
     ├─────────────────────────────────────→│
     │                                      │ Tick 扫描队列
     │  本轮推理结束，让出 CPU               │
     │                                      │
     │            ... 时间流逝 ...           │
     │                                      │
     │          回调触发 (满足唤醒条件)      │
     │←─────────────────────────────────────┤
     │                                      │
     │  新一轮推理，结合历史结果继续          │
     │                                      │
```

### 单向依赖（关键原则）

- Scheduler **完全不依赖** GoPi —— 不导入 LLM、工具、推理代码
- GoPi 依赖 Scheduler —— 调用其公开接口注册任务、绑定回调
- 替换 Scheduler 策略不影响 GoPi 业务代码

## 运行时形态

### Phase 1: 同进程内置（Go 单二进制目标）

```
GoPi binary
  ├── agent/       LLM 推理 + 5 工具
  ├── scheduler/   独立包（心跳、队列、持久化、回调触发器）
  └── cmd/         入口：启动 Scheduler 协程 + GoPi 主循环
```

- `scheduler/` 包：纯时序逻辑，零 LLM 依赖
- `agent/` 包：可 import `scheduler/`，调用公开接口
- 通过 Go channel 传递事件（非阻塞）

### Phase 2 (未来): 独立调度服务

只有多 Agent 集群协作时才需要，本地自治场景不需要。

## Scheduler 包接口

```go
// scheduler/scheduler.go

type Task struct {
    ID          string
    Status      TaskStatus  // READY | RUNNING | BLOCKED | DONE | FAILED | TIMEOUT
    Priority    int
    MaxRuntime  time.Duration
    Callback    func(*Task)  // GoPi 注册的回调函数
    Trigger     Trigger      // 何时唤醒
    CreatedAt   time.Time
    RetryCount  int
    ParentID    string       // 父任务（分步拆解时）
}

type Trigger struct {
    AtTime      *time.Time      // 指定时刻唤醒
    AfterDur    *time.Duration  // 延迟 N 秒后
    OnFile      *string         // 文件出现时
    OnTaskDone  *string         // 前置任务完成时
}

func NewScheduler(cfg Config) *Scheduler
func (s *Scheduler) RegisterTask(task *Task) error
func (s *Scheduler) CancelTask(id string) error
func (s *Scheduler) GetPendingTasks() []*Task
func (s *Scheduler) Start()    // 启动后台心跳协程
func (s *Scheduler) Stop()     // 优雅停机
```

## 工作流示例：远程编译

```
1. GoPi 判断：需要编译远端 C 项目（预计 ~5min）
2. GoPi 生成编译脚本 → remote("host", script)
3. 无法立即返回 → GoPi 调用:
     sched.RegisterTask(&Task{
       ID:       "compile-abc",
       Trigger:  Trigger{AfterDur: &600*time.Second},  // 最多等 10min
       Callback: resumeCompile,   // 完成后唤醒入口
     })
4. 本轮 GoPi 推理结束

5. Scheduler 心跳 Tick:
   - 发现 "compile-abc" 到时间了 → Poll 远端脚本是否完成
   - 未完成 → 继续等
   - 已完成 → 触发回调 resumeCompile(task)

6. GoPi 被唤醒，处理编译结果，继续推理
```

## 上下文持久化

任务快照序列化到 `~/.gopi/tasks/<task-id>.json`：

```json
{
  "task_id": "compile-abc",
  "status": "BLOCKED",
  "conversation_snapshot": ["msg1", "msg2", ...],  // 上下文中断点
  "checkpoint": "step=2, remote script submitted",
  "parent_id": "main-task-001"
}
```

GoPi 重启后，Scheduler 加载所有未完成任务，按 Tick 依次唤醒。

## 与 `remote` 工具的天然适配

```
remote ("host", compile_script)
  → 提交后立即 yield
  → 注册 Scheduler 回调
  → Tick 轮询结果
  → 唤醒 GoPi 解析输出
```

不占用单轮上下文。远端慢任务从"阻塞"变成"异步等待"。

## 开发阶段

```
Phase 0 (当前): GoPi 5 工具 + 同步 ReAct loop        独立开发
Phase 1:        scheduler/ 包内置于 GoPi                独立开发
Phase 2:        GoPi import scheduler/，回调集成        集成
Phase 3:        任务持久化 + 跨会话恢复                 集成
Phase 4:        与外部 scheduler 项目对接                未来
```

**关键规则：两个项目独立开发，不互相污染设计。**

## 参考

- `~/work/scheduler/DESIGN.md` — 外部 Scheduler 项目设计（独立）
- `~/work/claude/the operator/gopi_scheduler_integration_design_talk.txt` — 原始讨论
- GoPi 5 工具: read, write, edit, bash (local), remote (cross-machine)
