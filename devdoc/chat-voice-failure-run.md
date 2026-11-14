<!-- doc-class: historical -->
# Task Report: `/voice` — 一串失败才关掉模式（deliverable D）

task `01a121fc-eef5-7f3e-8ff3-14a3bd9afc4c` · 分支 `task/01a121fceef57f3e8ff314a3bd9afc4c`

## Completed

**缺陷**：`gopi chat` 的 `/voice` 模式在**任意一窗**引擎错误上调用 `m.fail(err)` →
打印 `chatVoiceErrorNote` 并立刻 `leave(chatVoiceLeftByFailed)`。两个 loop（`loop`
与 `liveLoop`）各写了一遍这个判断，所以一次上传被拒 / 一次引擎调用失败，就关掉
整个模式 —— 而 `/voice` 面对的失败大多是**瞬态**的（云端一次 upload 被拒、本地引擎
漏一次窗口）。

**规则**（`cmd/agent/chat_voice.go`）：

- `voiceChatMaxConsecutiveFailures = 3` —— 命名常量（Rule 1），注释里写明「为什么是
  一串失败而不是一次」和「为什么是 3」。
- `voiceChatFailuresGiveUp(consecutive int) bool` —— **唯一的谓词**，两个 loop 都问它，
  所以窗口臂与 live 臂不可能对「什么时候算致命」给出两个答案。
- `chatVoiceState.failStreak int` —— 计数放在**共享状态**里而不是 loop 局部变量：模式
  有两条循环，「这一串失败有多长」是关于会话的事实，不是关于某种循环形状的。
- `(*voiceChatMode).roundFailed(err) bool` —— 唯一的计数点与判定点：`false` = 已上报、
  继续听；`true` = 该走了（内部才调 `m.fail`）。
- `(*voiceChatMode).roundSucceeded()` —— 清零。**「成功」包含空转写/静音**（静音是引擎
  在回答），因此安静的房间不会把失败数往上加。`enter()` 里也清零，否则上一个会话的失败
  会计进这一次，操作员的第一个坏窗口就成了第三次。
- `chatVoiceRoundRetryNote = "[gopi] voice round FAILED (%d of %d) — %v. Still listening;
  %d more failed window(s) in a row turns the mode off.\n"` —— 复用的是既有 `notice`
  通道（`m.notice` → `VoiceNotice`）与既有 `chatVoiceErrorNote` 的排面，未改架构。
- **不重试「重试也不会变」的失败**：`errVoiceLiveUnsupported`（recorder 根本没有 live
  路径）与「编辑器 insert 路拒绝了转录」仍走原来的 `m.fail(err)` 立即退出 —— 计数只用于
  一个窗口的失败。
- `errVoiceStreamClosed` 哨兵语义**未动**：它仍是「非失败」出口（capture ended）。

## Not Done

- **N 没有做成可配置项**（未加 settings 键 / env）。briefing 只要求「N 为命名常量」；
  加一个配置面就是多一个 Rule 35 意义上的持久依赖，而本机策略也要求不要在 hosta 上
  自己发明策略面。如果后续需要按机器调（云端慢链路想要更宽的容忍度），那是一条独立的
  任务，应当走 `audio_engine_param` 那套既有嵌套块，而不是在 `chat_voice.go` 里再加一个
  `os.Getenv`。
- **live 臂没有真跑**（`GOPI_AUDIO_ENGINE=streaming` 需要 vendor 凭据且会真计费）。
  真跑取的是 `recording` 臂 + 不可达端点；live 臂的同一规则由两个单元测试
  （`chat_voice_failure_run_live_test.go`）钉住，不是靠「它和窗口臂共用一个谓词」这句话
  成立的 —— 但那两个测试仍**不是**真跑的替代，见 Rule 31 的诚实半边。

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK；`CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → OK（两份都跑了，后者是 Ran: 用的那个二进制）。
- **Tests**: 分支范围（本 diff 触及的面）**绿**：

  ```
  go test -count=1 -timeout 900s -run \
    'TestChatVoice|TestVoiceLive|TestVoiceModule|TestVoiceStaged|TestVoiceWindow|TestVoiceUndeclared|TestVoiceSelfTyping|TestVoiceBoundary|TestVoiceCapture' \
    ./cmd/agent/
  ok  github.com/nick/gopi/cmd/agent  173.463s        (RC=0)
  ```

  **诚实半边（Rule 5/31）**：`make test-budget PKGS='./cmd/agent'` 在本机 **exit 1**，
  26 个用例红 —— 但**这些红都不是本王带来的**，是在 BASE（`926a71342`，本分支的合并基点）
  上就已经红的：

  ```
  # git checkout 926a71342  → 同样三条红
  ctags: executable file not found in $PATH     (hosta 没装 ctags)
  rg:    executable file not found in $PATH     (hosta 没装 ripgrep)
  PPID=3100, want 1                             (daemon 重父化是宿主性质)
  ```

  三类环境原因 + 一类 suite 的 `/dev/shm` scratch home，逐条记在
  `artifacts/PREEXISTING.md`（含命令与输出）。**分支范围绿 + 基线同样红**，是这两个事实
  合起来才构成「不是我引入的」，不是任何一句话。
- **Ran**（Rule 31：改了的面真跑一次，逐字的调用）:

```bash
# 端点：录音引擎的根 URL 指向 discard 端口 127.0.0.1:9（没人监听），所以每一窗都在网络层失败
export GOPI_AUDIO_ENGINE=recording
export GOPI_AUDIO_API_BASE_URL=http://127.0.0.1:9
python3 devdoc/evidence/sgr-grid/termgrid.py \
  /home/user/.gopi/sessions/01a121fc-eef5-7f3e-8ff3-14a3bd9afc4c/artifacts/voice-failengine-wrapper.sh \
  200 40 /home/user/.gopi/sessions/01a121fc-eef5-7f3e-8ff3-14a3bd9afc4c/artifacts/failengine.script
# wrapper.sh 内部就是被改动的那条面：
#   exec .../bin/gopi chat --tools --voice-audio .../failengine-60s.wav
# （termgrid 的 BIN 是它自己 execvp 的 argv[0]，它负责拼 `chat --tools`，所以
#   `--voice-audio` 由 wrapper 注入；见下）
```

- **Result**（真 pty 网格逐字，`artifacts/t25.grid.txt`，t≈25s 时整串已经跑完）：

```
[gopi] VOICE MODE ON — the file .../failengine-60s.wav (16000 Hz mono signed 16-bit little-endian
— no capture device is opened); transcribing with the recording engine every 6s; ...

[gopi] voice round FAILED (1 of 3) — doubao engine: doubaoasr: submit request refused: HTTP 0
status="" reqid=c21ade83-...: doubaoasr: http: Post "http://127.0.0.1:9/submit": dial tcp
127.0.0.1:9: connect: connection refused. Still listening; 2 more failed window(s) in a row turns
the mode off.
[gopi] voice round FAILED (2 of 3) — ... Still listening; 1 more failed window(s) in a row turns
the mode off.
[gopi] voice mode FAILED — ... The mode is OFF; the text on the line is kept.
[gopi] voice mode OFF (a failed round) — the recorder is stopping; ...
```

**观察到的实际行为，逐条回答 briefing 问的三个问题**：

1. **第一次失败后模式还在吗？** 在。第 1 次失败只出一行 `(1 of 3) … Still listening`，
   第 2 个窗口照常被录制并送到引擎（证据：第 2 行 `(2 of 3)`，且该行的 reqid 与第 1 行不同
   ⇒ 是**另一次** submit，不是同一行的重复打印）。
2. **连续几次后退出？** 第 **3** 次（`voiceChatMaxConsecutiveFailures`）。退出走的是既有
   通道：`chatVoiceErrorNote` + `chatVoiceLeftNote`，理由字段仍是 `a failed round`。
3. **和修复前差在哪？** 修复前第 1 行就会是 `voice mode FAILED` + 退出（该形状的红色由
   下面的 NEUTER 红证证明，不是靠这句话）。

**运行环境的两个坑（写下来给下一个人）**：`GOPI_AUDIO_API_BASE_URL` 必须是**根**
URL（submit 与 query 两个 URL 由它派生，`--doubao-submit-url` 只在子命令位置被解析，
`chat` 里没有）；文件音源按实时吐（100 ms 一个 chunk），所以 **60 s 的音频才够跑完 3 个
6 s 窗**（前两窗约 14 s 走完，第 3 次失败在约 22 s），t≈25 s 与 t≈70 s 两张网格内容相同
⇒ 模式确实在退出后不再产生行。

- **Red proof**（一次性 NEUTER，Rule 10.4 / 任务验收的「修复前 FAIL」）：
  把 `roundFailed` 里的判定临时改成 `if true { // NEUTER: the pre-fix one-strike shape`，
  两个新测试文件加既有两个受影响用例一起跑 →
  `--- FAIL: TestVoiceLiveLoop_OneFailedUtteranceDoesNotEndTheMode`、
  `TestVoiceLiveLoop_TheRunOfFailuresEndsTheMode`、`TestChatVoice_OneFailedWindowDoesNotEndTheMode`、
  `TestChatVoice_TheRunOfFailuresEndsTheMode`、`TestChatVoice_ASuccessClearsTheRunOfFailures`、
  `TestChatVoice_ARunOfFailuresLeavesTheModeOffAndKeepsTheLine` —— **6/6 红**；
  恢复后 `grep -c NEUTER cmd/agent/chat_voice.go` = **0**，同样 6 个用例绿。
  （原日志 `/tmp/neuter_red.log`，已留档到 session artifacts。）

- **Commit**: `f10f9a0f4` feat(voice): a RUN of failed windows ends /voice — one failed window is reported and retried

## Evidence (self-filed)

| 断言 | 观察 |
|---|---|
| `go build ./...` 通过 | `CGO_ENABLED=0 go build ./...` → 无输出，exit 0 |
| 新用例修复前 FAIL | NEUTER 版 `go test`（job-1 日志）：6 个用例全部 `--- FAIL`，`FAIL github.com/nick/gopi/cmd/agent 62.014s` |
| NEUTER 已还原 | `grep -c "NEUTER" cmd/agent/chat_voice.go` → `0`；`grep -n "voiceChatFailuresGiveUp(run)"` → `1605: if voiceChatFailuresGiveUp(run) {` |
| 新用例修复后 PASS | `go test -count=1 -run '…6 个用例…' ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 9.736s` |
| 真跑 | 上文的 `Ran:` 命令行 + `artifacts/t25.grid.txt` / `t70.grid.txt` 两张 pty 网格；第一次失败后 1 次仍活、第 3 次退出 |
| `git status --porcelain` 为空 | 提交后 `git status --porcelain` → 无输出；`git log --oneline -3` → `3dafd9395 docs(voice): the failure-run report states the branch-scoped green and the base's own red` / `3eaa631f6` / `f10f9a0f4 feat(voice): a RUN of failed windows ends /voice — one failed window is reported and retried` |
