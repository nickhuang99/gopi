<!-- doc-class: historical -->
# Task Report: 语音捕获窗口的 grace 空等与固定切口（task 01a11853）

## Completed

### 1. grace 重构：窗口起点 = 首样本，而不是 spawn（deliverable a）

**真因（5-why）**：不是「grace 被无条件加上」这一句描述那么简单 —— 是**双重单位换算**。
`stopVoiceRecorder` 里的表达式是 `time.Duration(voiceCaptureCloseGraceSec) * time.Second`，
而 `voiceCaptureCloseGraceSec` **本身已经是 time.Duration**，于是该值读作 **5e8 秒**：

- 等待永不到期 ⇒ 关闭信号永不发出；
- 每次录制都跑到调用方的 `context.WithTimeout(seconds+voiceCaptureGraceSec)` 截止；
- 结果 `--seconds N` 恒等于 `N + 10`。实测：`--seconds 6` = 16.03 s、`--seconds 3` = 13.03 s、
  `--seconds 2` = 12.03 s（peerbox 2026-10-08）。

**修法**：窗口一满就发停止信号 —— 带 `-d` 的 arecord 此时已自行退出，不带 duration 的
parecord（`voiceRecorderCommand` 里没有时长参数）**就是靠这个信号结束的**；等它自行退出是纯空等。
`voiceCaptureStreaming` 改为流式读 stdout，**窗口时钟从首样本开始**，
`voiceCaptureGraceSec` 保留为「够到首样本」的预算（慢设备保护原样保留，只是不再每轮都付费）。
`voiceCaptureContextSensitiveError` 把「窗口满→关管道→记录器非零退出」识别为正常路径。

### 2. 真因 2：PulseAudio 默认 2 s fragment（同属窗口起点问题）

strace 实测（真声卡 + 合成 rig）：parecord 默认 fragment 约 2 s，首字节在 spawn 后
**0.08–2.32 s** 才出现，且**按 2 s 成簇投递**（每 ~2 s 一个 11876 B 簇）；一个在簇中间关闭的
窗口，其语句尾部的簇还没被交出来。`voiceRecorderCommand` 现加
`--latency-msec=50`（具名常量 `voiceParecordLatencyMs`），同一测量读到 **0.049–0.121 s**。

### 3. 最小回归测试（deliverable c，随 commit 落地）

- `TestVoiceCaptureWindowCostsTheWindowNotTheGrace`
- `TestVoiceCaptureTreatsAWindowStopAsANormalExit`
- `TestVoiceRecorderAsksForASmallTransferFragment`

真跑证据（本 run session 目录 `/home/user/.gopi/sessions/01a11853-e21b-7c51-b6e1-de7e978aacae/fix/`）：
`recprobe.py` / `firstbyte2.py`（首样本延迟）、`fwprobe.py` / `gwprobe.py`（分簇投递）、
`head*.wav` `tail*.wav` `ov*.wav` `sil*.wav`（切片夹具）、`testbudget.log`。

## 验收对照（epic Acceptance）

| # | 要求 | 结果 |
|---|---|---|
| 1 | `--seconds 6` recording→captured ≤ 6.5 s（现 16.0 s） | **6.05 s ✅**（原 16.03 s） |
| 2 | 三轮连续 TOTAL_WALL ≤ 30 s（现 79.99 s） | **26.67 s ✅**（原 79.99 s） |
| 3 | 12.455 s 连续语音窗口模式逐字不丢 | **未完成 ⚠️ 见下** |
| 4 | `go test` 绿 + 既有语音测试未打红 | 测试已随 commit 落地；`make test-budget PKGS=./cmd/agent` 本 run 内未跑完（见下） |

## Not Done（诚实）

- **验收 #3 未闭环**。窗口模式三轮拼接实测为
  「今天我们用手机做一个语音输入的延迟实验。识别是否已经开通授权，是否欢迎双向。首起源」，
  而整段转写为「今天我们用手机做一个语音输入的延迟实验测试，我声音完全流逝，识别是否已经开通授权。」
  —— 相对 6 s 硬切（前段 22 字 + 后段全空）已是数量级改善，**但轮间仍丢「测试，我声音完全流逝，」**。
  本 run 已定位机制（每轮重新 spawn 记录器 + 轮间转写耗时期间麦克风无人读取），
  **尚未实施**「VAD/能量提前收口或带重叠收尾」这一半修复（deliverable b 的窗口边界重构）。
- **验收 #4 未闭环**：`make test-budget PKGS=./cmd/agent` 在本 run 截止时才启动，
  后台 job 未跑完，本报告不声称其绿灯。
- 相应地，本 commit **不含** deliverable (b) 与 (d) 的完整闭环。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin-gopi-fix5 ./cmd/agent/` → OK（session 目录下 5 个二进制快照）
- Tests: 最小回归测试 3 个已随 commit `f28ecd4db` 落地；
  `make test-budget PKGS=./cmd/agent` —— **本 run 内未观察到结果，不声称通过**
- Ran: `./bin-gopi-fix5 tool voice_input --seconds 6 --engine local`（真声卡，配合 recprobe.py 观测 recording→captured）
- Result: recording→captured **6.05 s**；三轮连续窗口 **TOTAL_WALL 26.67 s**；首样本延迟 0.049–0.121 s（原 0.08–2.32 s，2 s 成簇）
- Commit: `f28ecd4db`

## Evidence (self-filed)

- `--seconds 6` = 16.03 s（基线 `bin-gopi-baseline`）→ 6.05 s（`bin-gopi-fix5`）：由 recprobe.py 打印的 wall 与 recorder 字节数观测。
- 首样本延迟 0.08–2.32 s、2 s 成簇 → 0.049–0.121 s：由 firstbyte2.py / fwprobe.py / gwprobe.py 的 `/proc/<pid>/io` wchar 采样观测。
- 未闭合项（#3 轮间丢失、#4 未跑完）如实记录，未作任何绿灯声明。

## 交付位置与恢复

- 分支：`task/01a11853e21b7c51b6e1de7e978aacae`（worktree `/home/user/workspace/gopi-01a11853e21b7c51b6e1de7e978aacae`）
- 恢复：`git show task/01a11853e21b7c51b6e1de7e978aacae:devdoc/voice-capture-window-cost-20261008.md`
- 提交已在主仓库（worktree 共享 refs，无需 push）。
