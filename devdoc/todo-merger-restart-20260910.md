<!-- doc-class: historical -->
# 待办：merger 重启（应用新代码）

## 发现（2026-09-10 12:20）
merger 进程 PID **2209903** 从 **2026-09-03 21:13** 起跑（7 天未重启）——
**运行中进程不享受新代码**。证据：
- 它 12:19 创建的 resolve 行用 **legacy task_id 格式 `t-1789013949`**
  （<unix 秒>）——而 task_id 已在 2026-09-10 改为 **uuid v7**（963b8eb）
- 说明它跑的是旧二进制（9/3 版本）

## 影响
merger 缺少这些新特性（都是 9/3 后开发）：
1. uuid v7 task_id 生成（DB 清理 Phase 1）
2. 自带 resolver 逻辑的更新（9/5 merger-self-resolve 之后的演进）
3. 其他 merge 清理/rebuild 契约更新（REBUILD after merge 等）
4. sessions 目录布局（S1 之后的目录形态）

**功能上仍在工作**（它在正常解冲突/merge）——但产出旧格式登记行。

## 处置
等**自然窗口**（无 child 在跑时）重启：
```bash
kill 2209903  # 注意：同进程组会连带杀 child！先确认无 child
cd ~/work/GoPi && setsid ./bin/gopi task merger --every 5m &
```
**bootstrap 铁律**：merger 不能自重启（自杀式——重启中无人管队列），
所以由 PA 手动重启。

**重启前检查**（kill serve/merger 连带杀同进程组 child 的教训）：
- 确认无 resolve child 在跑（ps 查 2209903 的子进程）
- 或者接受连带杀（僵尸清理机制兜底）

## 当前状态（12:20）
- 4 个 in_progress：env-leak 修复 / 收敛门 bug 单 / **feat/hippocampus-host 冲突解决**
  （t-1789013949 + 01a0898a——merger 创建的 resolve 行）
- 冲突解决中 → 完成后是重启窗口
