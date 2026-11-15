<!-- doc-class: normative -->
# 无麦克风的官方 `/voice` 入口 — 一条真 pty 命令端到端取证（交付项 B）

Task `01a121fc-eebc-7391-8785-db2d149f931e`（epic voice-audio 的交付项 B）。目的是把已有的
`--voice-audio FILE` / `GOPI_CHAT_VOICE_AUDIO` 通道**接通成官方无麦克风测试入口**，并用它
取证：文件源取音 → 切窗 → 引擎转写 → 落字到输入行，同一素材跑两次结果一致。

## Completed

- **真 pty 端到端实跑（Rule 31，两次）**。`gopi chat --voice-audio <11.2s 16k mono wav>` 在
  真 pty（220×40，python openpty + TIOCSWINSZ 驱动）上跑完整 `/voice` 流程：
  `[gopi] VOICE MODE ON — the file … (16000 Hz mono signed 16-bit little-endian — no capture
  device is opened)` → `[voice] 开饭时间早上九点至下午五点。` → **`you> 开饭时间早上九点至下午五点。`**
  → 第二个窗口 `[voice] no text in the last 6s window … still listening` →
  `[gopi] voice mode OFF (the capture ended) — nothing is left to record; the text already on the
  line is kept.` 两次运行的这三行**逐字相同**，落字行相同。
- **素材**：`voicectl-11s.wav`（16000 Hz / mono / pcm_s16le / 11.232 s），由继承的 5.6 s 中文录音
  经 ffmpeg concat 合成（本机无 zh 语音合成可用：无 espeak/espeak-ng，`ffmpeg flite` 只有英文
  voice，浏览器与网络按 brief 不可用）。ffprobe 校验几何见 `## Verification`。
- **fixture 路径正确性预检**：`tool voice_input <wav> --engine local` → `开放时间早上九点至下午五点。`
  `([voice] probe.wav in 1.2s (chunk=15s)`），先证明素材与本地引擎在 host-b 上可用，再进 pty。
- **CI 回归（新增，红/绿可验）**：`cmd/agent/chat_voice_audio_smoke_test.go`
  - `TestChatVoiceAudioFileRunIsRepeatableWithoutADevice` — 同一文件跑两次生产 chat recorder，
    断言**窗口序列逐字段相同**、**交给引擎的字节逐窗口相同**、消耗字节数相同、ledger 完整铺满文件。
  - `TestChatVoiceAudioFileRunNeverNamesADevice` — 进入行必须写明文件捕获与几何，recorder 携带的
    device 必须是 `file` 这个**陈述**而不是解析出来的设备。
  - **红证实测**：把文件源的投递改成「整段一个 chunk」→ 该用例 FAIL
    （`the fixture produced 1 window(s) — the case cannot compare a sequence it never cut`），
    还原后 PASS。
- **入口文档（本 run session 证据目录，不入仓库）**：`artifacts/RUNME-no-mic.md`。

## Not Done

- **「文件运行绝不枚举设备」没有做成受控实验。** 本机（host-b）有真声卡
  （`arecord -l`：card 1 ALC256），所以「无设备主机也能进模式」这条路**无法在本机制造受控反证**。
  代码事实是 `chat_voice_recorder_staged.go` 的 `Select` 在 `voiceListDevices` 之前就分流到
  `selectFileSource`，但**这是读码结论，不是本次取证的实验结果** —— 已按此措辞写进 CI 用例注释与
  本报告，没有冒充足证。同因：`media`/pulse 回环不在本 brief 的允许范围（且属 Rule 35 的新依赖），
  未使用。
- **本包更宽的 smoke（`make test-budget PKGS='./cmd/agent'` 与 `go test ./cmd/agent/ -run 'Voice'`）被预算切断**：
  两条各跑 8–10 分钟仍未出结果（`cmd/agent` 全量编译 + 该包套件很重），在 75% 预算点被本 run 主动 kill。
  因此**本报告不声称「整包绿」**，只声称上面那一条 `-run 'TestChatVoiceAudioFileRun'`（10.8s，ok）
  与 `go build ./...` / `go vet ./cmd/agent/` 通过。整包 smoke 留给 merge/verify 阶段跑。
- **未修 `--voice-audio` 的接线缺口**：本次未发现接线缺口（通道完整可用），故源码 diff 仅新增测试。
- **一处未根因化的异常观察（见下）**，只记录、不入库、只提交为独立任务。

## 未根因化的异常观察（诚实记录，不冒充结论）

本次取证中，**同一个 wav 的同一份内容**，在同一个 pty 驱动下：

- 用**全写路径** `/home/user/.gopi/sessions/01a121fc-eebc-7391-8785-db2d149f931e/artifacts/probe.wav`
  时，chat 进程读得到（`MODE ON`，转写正常）；
- 用**短写 session-id 路径** `/home/user/.gopi/sessions/01a121fceebc73918785db2d149f931e/artifacts/probe.wav`
  （即账本里 task-id 的拼写）时，同一个文件、`ls -l` 可见、python `os.open` 可见、wrapper 脚本里
  `[ -r ]` 也可见，但 chat 进程报 `open …: no such file or directory`；`strace -f -e trace=openat`
  实测到该 `openat(...) = -1 ENOENT`，而 `chdir` 到的是 `ptyrun1`。

**复现率**：短写路径 FAIL 约 8 次全中；全写路径 OK 约 8 次全中；同一 shell + 同一 pty 驱动 + 逐字节
相同的 wrapper（仅路径拼写不同，`cmp` 已确认只差这一段）。**这不是诊断**：没在本会话预算内根因化，
因此**不写任何机制断言**，只作为现象与证据留档，并已提交独立任务跟进。
官方命令形因此统一使用**全写路径**，不受影响。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK。
- Tests: `GOPI_AUDIO_ENGINE=local go test ./cmd/agent/ -run 'TestChatVoiceAudioFileRun' -count=1`
  → `ok github.com/the operator/gopi/cmd/agent 10.819s`（红证另见 `## Evidence`）。
- Ran: 真 pty（python `openpty` + `TIOCSWINSZ` 220×40，argv 逐参传递，不经 shell 重组）：
  `PTY_ARGV="$G/bin/gopi chat --voice-audio $SB/artifacts/voicectl-11s.wav" GOPI_VOICE_ENGINE=local
  GOPI_AUDIO_ENGINE=local python3 ptydrv2.py $G 220 40 s5.txt official-N.raw`
  （脚本：`sleep 2` → `type /voice` → `submit`（间隔 0.9 s，避开粘贴检测吞 Enter）→ `wait 70` → `quit`）
- Result: 两次运行各出现 `VOICE MODE ON … no capture device is opened`、`[voice] 开饭时间早上九点至下午五点。`
  以及**`you> 开饭时间早上九点至下午五点。`（落字到输入行，验收）**，收尾
  `voice mode OFF (the capture ended)`；两次的关键行逐字一致。Raw pty 字节：
  `ptyrun1/official-1.raw`（2060→20253 B 区间）、`ptyrun1/official-2.raw`；关键行抽录
  `ptyrun1/OFFICIAL-PTY-RUNS.txt`。**全程无麦克风参与**：素材是文件，`--voice-audio` 走
  `voiceFileSource`；本机确实有声卡，但该通道一次也没打开它（分流的代码事实见 `## Not Done`）。
- Commit: 见本文末（报告与测试同一次提交）。

## Files

- `cmd/agent/chat_voice_audio_smoke_test.go`（新增，CI 回归：file source 可重复 / 入口行陈述）
- `devdoc/chat-voice-no-mic.md`（本报告）
- 证据（本 run session 目录 `~/.gopi/sessions/01a121fc-eebc-7391-8785-db2d149f931e/artifacts/`，不入仓库）：
  `voicectl-11s.wav`、`probe.wav`、`probe-local-zh.txt`、`RUNME-no-mic.md`、
  `ptyrun1/official-{1,2}.raw`、`ptyrun1/OFFICIAL-PTY-RUNS.txt`、`ptyrun1/ptydrv2.py`（pty 驱动）

## Evidence (self-filed)

- 素材几何：`ffprobe -v error -show_streams -select_streams a voicectl-11s.wav`
  → `codec_name=pcm_s16le sample_rate=16000 channels=1 bits_per_sample=16 duration=11.232000`。
- 本地引擎可用：`GOPI_VOICE_ENGINE=local ./bin/gopi tool voice_input probe.wav --engine local`
  → `开放时间早上九点至下午五点。` / `[voice] probe.wav in 1.2s (chunk=15s)` / `EXIT=0`。
- CI 绿：`go test ./cmd/agent/ -run 'TestChatVoiceAudioFileRun' -count=1` → `ok … 10.819s`。
- CI 红：把 `voiceFileSource.pump` 的步长改成整段（`off += len(s.pcm)`）后同一命令 →
  `--- FAIL: TestChatVoiceAudioFileRunIsRepeatableWithoutADevice … the fixture produced 1 window(s)`；
  还原后复跑 → `ok … 10.919s`。
- strace：`strace -f -e trace=openat,chdir` 中
  `openat(AT_FDCWD, "/home/user/.gopi/sessions/01a121fceebc73918785db2d149f931e/artifacts/probe.wav", O_RDONLY|O_CLOEXEC) = -1 ENOENT`，
  同时 `chdir("…/artifacts/ptyrun1") = 0`（短写路径异常的唯一实测证据，见 `## Not Done`）。
- 本机环境（用于「无麦克风」措辞的诚实边界）：`arecord -l` → `card 1: PCH … ALC256 Analog`，
  `pactl list short sources` → 有 `alsa_input.…analog-stereo`。
