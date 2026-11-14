<!-- doc-class: historical -->
# G6 voice_input cases: the engine is the CASE's, not the host's
Task: `01a11e05-3525-781c-bed3-53940b76e972` · branch
`task/01a11e053525781cbed353940b76e972` · measured on hostb-Laptop, 2026-10-09.

## The defect

`cmd/agent`'s G6 help-surface table (`cli_tools_maintenance_help_surface_cases_test.go`)
carried four voice_input cases that were **red on any host whose default engine is
the cloud one and which holds no key file**:

```
--- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input
--- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input_--seconds_5
--- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input_clip.ogg
--- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input_clip.pcm
```

Every one of them printed the same thing instead of what it asserted:

```
gopi tool voice_input: doubao engine needs an API key: /home/user/.doubao/apikey.txt …
```

### Root cause (5-why), corrected

The briefing's hypothesis was a **near-miss marker**: `wantAny` accepted
`"doubao engine:"` (with the colon, `cli_voice_engine.go:554/:556`) while the
first cloud state is `voiceDoubaoKeyHint` (`cli_voice_engine.go:90`, no colon).
That is true, and it is *not* the whole defect — **adding that marker would
have turned the gate into decoration, and the measured state showed why.**

The four cases assert states of the **LOCAL** engine (the device policy's
refusal; the decoder's decode-set hint). They never named an engine: the probe
inherited the host's. `config.VoiceEngineDefault` is `doubao`, so on this host —
and on any host that never set a channel — the run resolves to the cloud engine
and stops at its first state, and the local states are **unreachable**. Widening
`wantAny` would have accepted that, i.e. accepted "the form was refused for a
reason that has nothing to do with what this case tests".

The same class has a second, sharper half. `transcribeFileDoubao` **uploads the
audio**. A host that holds a key file would not have been red at all — it would
have POSTed the fixture's bytes to the vendor's API on every run of a unit test.
A test may not do that.

So the fix is not a marker: the case names its engine.

## The fix

`g6VoiceCaptureEnv` — the env function the whole family already shares — now
returns:

```go
"PATH=" + <sandbox stub dir> + ":" + os.Getenv("PATH"),
"HOME=" + home,
config.VoiceEngineEnv + "=" + config.VoiceEngineLocal,
```

- **`GOPI_VOICE_ENGINE=local`** is the rung the resolver reads between the flag
  (`--engine`) and the settings file, and it is the spelling the binary's own
  refusal suggests to the operator (`voiceBackToLocalHint`). The cases do not
  spell the string: they take `config.VoiceEngineEnv` / `config.VoiceEngineLocal`
  (Rule 14).
- **`HOME=<sandbox>`** is the second half of the same argument. HOME decides the
  legacy deployment rung (`$HOME/work/funasr-models`, `$HOME/work/runtime/…`) and
  the doubao key file (`$HOME/.doubao/apikey.txt`). With the operator's real HOME
  the family silently *read the operator's own installation* — the device refusal
  these cases assert is reachable on this host only because
  `/home/user/work/funasr-models` happens to be complete. Pinned, the family is
  hermetic.

Pinning the engine exposes what the cases were previously getting for free: a
local run needs a **deployment**. With nothing planted, the first state of a
local run is `runtime not found` plus the 15-line install guidance (measured),
which is not the device policy either. So the cases that need an engine plant
one:

- `g6SeedVoiceEngineDeployment` — the runtime on rung 1
  (`$GOPI_HOME/.gopi/voice/llama-funasr-cli`) and both weight files beside it.
  The weights are **empty on purpose**: the walk tests presence
  (`voiceModelsComplete` → `voiceFilePresent`) and the stub never opens them.
- `g6SeedVoiceLocalStubs` = capture stubs + deployment (the two bare-capture
  cases).
- `g6SeedVoiceEngineFile(name, body)` = the fixture bytes + deployment (the two
  file cases).

Wiring, per case: `.ogg` / `.pcm` gained `env: g6VoiceCaptureEnv` (they had
none); all four kept their `wantAny`, which is still the honest assertion — with
the engine named, the local states it accepts are the ones reached. The fixture
bodies moved into named constants (`g6VoiceOggFixtureName`/`Body`,
`g6VoicePCMFixtureName`/`Body`, Rule 1) because the argv builds its path from
the same name the setup writes.

`doubao engine:` stays in both `wantAny` lists: it is a *legitimate* verdict for
those two forms where a run really did select the cloud channel — the list is a
statement about the states the form can produce, and pinning the case's own env
does not make another run's verdict false.

## Regression guard

`TestG6VoiceCasesPinTheirEngine` drives the real binary over ONE fixture set,
twice:

| arm | env | asserted |
|---|---|---|
| 1 | `HOME` + stub PATH, no engine named | exit 1 and `voiceDoubaoKeyHint` — the cloud default answers first |
| 2 | `g6VoiceCaptureEnv` | exit 1 and `g6VoiceDeviceRefusedMarker` — the planted deployment carries the run to the device policy |

Arm 1 is what makes the guard non-vacuous: without it, "the marker appeared"
would also pass on a host that pinned the engine globally. Removing either the
engine pin or the deployment reddens arm 2.

## Verification

- Build: `CGO_ENABLED=0 go build -o <session>/probe/gopi ./cmd/agent/` — OK
- Tests: `make test-budget PKGS='./cmd/agent ./internal/standards'` — see
  `final-budget.txt` in the session directory. Branch-scoped red/green for the
  family, measured:

  ```
  # BEFORE (base tree, this host, no ~/.doubao/apikey.txt)
  go test ./cmd/agent -run 'TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input' -v
  --- FAIL: …/gopi_tool_voice_input
  --- FAIL: …/gopi_tool_voice_input_--seconds_5
  --- FAIL: …/gopi_tool_voice_input_clip.ogg
  --- FAIL: …/gopi_tool_voice_input_clip.pcm
  # full transcript: <session>/before-forms-are-typed-red.txt

  # AFTER
  go test ./cmd/agent -run 'TestG6VoiceCasesPinTheirEngine|TestToolsMaintenanceHelpSurface' -count=1 -v
  --- PASS: TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation (0.00s)
  --- PASS: TestToolsMaintenanceHelpSurfacePagesComeFromTheirOwnConstant (2.18s)
  --- PASS: TestToolsMaintenanceHelpSurfaceAdvertisedFlagsAreAdmitted (0.00s)
  --- PASS: TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason (0.00s)
  --- PASS: TestToolsMaintenanceHelpSurfaceFormsAreTyped (34.39s)
  --- PASS: TestG6VoiceCasesPinTheirEngine (0.03s)
  ok  	github.com/nick/gopi/cmd/agent	37.553s
  ```

- Ran (Rule 31 — the surface itself, verbatim): the real binary over a private
  sandbox, the same argv the cases use, with the deployment planted:

  ```
  env -i PATH=<session>/probe/bin:/usr/bin:/bin HOME=<session>/probe/home \
      GOPI_HOME=<session>/sb GOPI_VOICE_ENGINE=local \
      <session>/probe/gopi tool voice_input --seconds 5
  ```

- Result (what was actually observed):
  - with the engine named and the deployment planted → exit 1, last line
    `gopi tool voice_input: device alsa:plughw:CARD=G6Fixture,DEV=0 is not
    reachable: arecord failed on … run \`gopi tool voice_input --list-devices\` …`
    — the state both bare-capture cases assert;
  - `tool voice_input <session>/sb/clip.pcm` → exit 1,
    `… engine failed: exit status 1; hint: .pcm is not in the engine's decode set
    (wav/mp3/flac) … cannot read raw PCM — the SAME bytes work on --server --stdio
    …` — the state the file cases assert;
  - the same run with **no** engine named → exit 1,
    `gopi tool voice_input: doubao engine needs an API key: …` — the RED state,
    reproduced by hand outside the test harness;
  - with the engine named and **no** deployment → exit 1,
    `runtime not found: <sb>/.gopi/voice/llama-funasr-cli` — the second state the
    naive fix walks into, which is why the cases plant a deployment.

- Commit: `pending` (filled in at commit time)

## Evidence (self-filed)

- `git rev-parse --abbrev-ref HEAD` → `task/01a11e053525781cbed353940b76e972`.
- `CGO_ENABLED=0 go build -o <session>/probe/gopi ./cmd/agent/` → exit 0.
- `<session>/probe/gopi tool doccheck` → `doccheck: the devdoc corpus is
  consistent — every file under devdoc/ has a well-formed row` (the new devdoc's
  DOC-INDEX row was written by the write hook, in the same change).
- `gofmt -l cmd/agent/cli_tools_maintenance_help_surface_cases_test.go` → empty.
- `go vet ./cmd/agent/` → no output.
- Red-then-green transition: the four subtests are listed `--- FAIL` in
  `before-forms-are-typed-red.txt` and `--- PASS` in the run above.

## One pre-existing red in the same budget run (not this branch's)

`make test-budget PKGS='./cmd/agent ./internal/standards'` ends
`outcome=fail` on `./internal/standards`, and it is not this change:

```
--- FAIL: TestStandardGates/TestGate5AccessorPrefixRedProof (0.65s)
    Rule 20 (no get/is/handle prefix on an accessor): NEW violation at internal/doubaostream/session.go:398  [isClosing]
```

The verdict is **master's own, on both sides of the peer boundary**, and the
two proofs were re-measured after this branch's delivery-point sync (it merged
`peerbox/master` at `f5ffc27e2`, this task's true upstream — the local `master`
ref was 21 commits behind it):

```
# the branch's own tree, before the merge commit f2114e26e
git diff --stat <base> 917ac7520 -- internal/standards   → empty
# the peer boundary itself: the merge-base, and the peer's tip
git diff --stat f5ffc27e2 <branch> -- internal/standards internal/shot \
    internal/doubaostream/session.go                      → empty (identical)
# the peer's own tree, untouched by this branch
git worktree add --detach <scratch>/peer-master-proof peerbox/master   # f5ffc27e2
cd <scratch>/peer-master-proof && go test ./internal/standards/ -run 'TestStandardGates$' -count=1
--- FAIL: TestStandardGates/TestGate5AccessorPrefixRedProof
    Rule 20 … NEW violation (checklist 1.4) at internal/doubaostream/session.go:398  [isClosing …]
```

`git blame internal/doubaostream/session.go:398` → commit `823305a17`
(`feat(voice): streaming dictation …`), and the same line stands at `398` in the
peer's tip — i.e. it arrived from the upstream, not from this branch. So the
verdict is master's, reproducible without this branch (Rule 26: the honest gap
is reported, not cleaned up by a sibling). The renamed accessor
(`isClosing` → `closed`) or a one-line baseline entry is a master-side fix.

**The acceptance command was re-run after that merge**, so the claim below is
about the branch as it will be pushed (the pre-merge run reached the same
verdict; the merge only brought in `cmd/agent` and `devdoc/` changes).

## Not done (honest)

- The **`--self` family** (three cases) was audited and left alone: it runs the
  local capture arm (`pactl`/`arecord` stubs → device policy) and **passed on
  this host before the pin existed** — `--client` is outside `needsEngine`, so no
  credential check stands in front of the device policy. It now carries the pin
  through the shared env helper, which is deliberately more than it needs: the
  pin is a property of the helper, and splitting the helper into "pinned" and
  "unpinned" variants would reintroduce exactly the host-dependence this task
  removes. Their note says so.
- **Nothing in production code changed.** The states the cases observe are the
  binary's; only the cases' environment was made explicit. A host-level default
  engine remains a host-level fact — the fix is that a *test* stops depending on
  it.
- The `--list-devices` cases share `g6VoiceCaptureEnv` and never reach a
  deployment (they exit 0 after printing the table). Pinning HOME+engine there is
  harmless and was verified green; no separate helper was introduced.
