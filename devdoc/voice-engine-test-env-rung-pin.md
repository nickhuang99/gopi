<!-- doc-class: historical -->
# A test that asserted the SETTINGS rung while a live gopi session fed it the env rung

**Task**: `01a1220ee06f7da9b57ab46ec0f5ca52` · branch `task/01a1220ee06f7da9b57ab46ec0f5ca52`
**File changed**: `cmd/agent/cli_voice_engine_test.go` (test only — the resolver is untouched)

## The defect, in one line

`TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings`'s FIRST assertion is titled
"settings alone → doubao", but the two rungs ABOVE settings were not pinned — so
anything that exports `GOPI_AUDIO_ENGINE` (or the pre-rename `GOPI_VOICE_ENGINE`)
turns that assertion red for a reason it does not assert.

## Why that is not a hypothetical: gopi publishes the rung itself

`voiceResolveEngine` (`cmd/agent/cli_voice_engine.go:172`) walks

```
opts.engine (flag)  →  GOPI_AUDIO_ENGINE  →  GOPI_VOICE_ENGINE (pre-rename, SAME rung)  →  settings  →  default
```

and `internal/config/env.go:190` publishes this host's own settings into the
environment of every child a gopi process starts:

```go
setStr(AudioEngineEnv, cfg.AudioEngine)      // internal/config/env.go:190
```

So a test run **inside a gopi session or a task run** (exactly how this one was
found) inherits `GOPI_AUDIO_ENGINE=<this host's engine>` and the "settings alone"
line reads the CALLER's engine. Measured on this worktree before the fix — the
same file, `streaming` exported:

```
$ GOPI_AUDIO_ENGINE=streaming go test ./cmd/agent/ \
      -run '^TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings$' -count=1
--- FAIL: TestVoiceEnginePrecedenceFlagBeatsSettings (0.00s)
    cli_voice_engine_test.go:139: settings rung: engine = "streaming", want doubao
FAIL	github.com/nick/gopi/cmd/agent	0.656s
```

and with the pre-rename name instead (this assertion is a test, so the legacy
rung has to be pinned too, or the "settings alone" line is still not alone):

```
$ env -u GOPI_AUDIO_ENGINE GOPI_VOICE_ENGINE=local go test ./cmd/agent/ \
      -run '^TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings$' -count=1
    cli_voice_engine_test.go:139: settings rung: engine = "local", want doubao
```

## The fix (test side only — the precedence itself is correct)

`t.Setenv(config.AudioEngineEnv, "")` and
`t.Setenv(config.AudioEngineLegacyEnv, "")` immediately before the first
assertion, with the reason in the comment. The constants come from
`internal/config/settings.go` (`AudioEngineEnv = "GOPI_AUDIO_ENGINE"`,
`AudioEngineLegacyEnv = "GOPI_VOICE_ENGINE"`), never a typed literal (Rule 1/13);
`t.Setenv` not `os.Setenv` (Rule 17). Later lines still set the env rung
explicitly, which is what the test is FOR.

The same file already used this shape two tests below
(`TestAudioEngineDefaultsToDoubao` pins `config.AudioEngineEnv` to empty) — this
change makes the precedence test agree with it.

## Why not fix it in the resolver

The resolver's order is the documented one (Rule 2: one precedence chain, stated
once) and the env rung is deliberately ABOVE settings — an explicit export must
beat a stored default. The defect is in the test's premise, not the chain.

## Verification

`go build ./cmd/agent/` clean; the named test run in four environments (A unset,
B `GOPI_AUDIO_ENGINE=streaming`, C `GOPI_VOICE_ENGINE=local`, D both) — all `ok`.
Rule 31 run lines and Rule 5's report live in the task report.
