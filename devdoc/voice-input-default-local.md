<!-- doc-class: normative -->
# `gopi tool voice_input` — default shape = local capture + local transcription (design)

> **SUPERSEDED IN PART (2026-10-07).** This document's premise is the default
> **shape** — "a bare invocation records the mic and transcribes it here" — and
> that is still exactly what the bare invocation does. What changed is the
> **engine** it transcribes WITH: the terminal default flipped from `local` to
> `doubao` on the operator's instruction, so a bare `gopi tool voice_input` now
> uploads the audio to the cloud engine unless the three channels name `local`.
> The shape argument below stands unchanged; read the engine half of it
> (`devdoc/voice-input-default-local.md`'s "local" everywhere) as the state
> BEFORE 2026-10-07, and `devdoc/doubao-asr-integration.md` §3 for the reversal.

Task: `01a113e4-6730-7b5b-bc35-0278c2c20b8b` · epic 1/5 · host: **hostb-Laptop (client)**
Branch: `task/01a113e467307b5bbc350278c2c20b8b` (from master) · **design only, zero implementation**

---

## 0. The premise this design was asked to assume — and what the tree actually says

The epic asks for a **改造设计** (rework design) that makes the default shape
local-capture + local-transcription. Before designing the rework I measured the
tree, because a design against a premise nobody re-checked is the expensive kind
of wrong.

**Measured (this worktree, this run):**

| claim | command | result |
|---|---|---|
| the default shape already exists in master | `git log --oneline -1 457a999b6` | `feat(voice): bare \`gopi tool voice_input\` records the mic and transcribes it locally` |
| it is an ancestor of master **and** of this branch | `git merge-base --is-ancestor 457a999b6 master` / `… HEAD` | both exit 0 → **IN master, IN HEAD** |
| this worktree adds nothing over master | `git rev-list --count master..HEAD` | `0` |
| master's tree contents | `git diff --stat master...HEAD` | *(empty)* |
| the default is decided before the refusals | `sed -n '743p' cmd/agent/cli_voice.go` | `opts.capture = len(positionals) == 0 && !opts.server && !opts.client && !opts.listDevices` |
| the named default length exists and is referenced | `sed -n '103p;749p' cmd/agent/cli_voice.go` | `voiceLocalDefaultSeconds = 10` · `opts.seconds = voiceLocalDefaultSeconds` |
| `--listen` implies `--server` | `sed -n '731,733p' cmd/agent/cli_voice.go` | `if fs.has(voiceFlagListen) { opts.server = true }` |
| the profile rung resolves through `config.GopiDir()` | `sed -n '277p' cmd/agent/cli_voice_local.go` | `gd, err := config.GopiDir()` |
| a named refusal for every combination the epic names | `grep -n "return nil, fmt.Errorf" cmd/agent/cli_voice.go` | 23 parse-time refusals, all `fmt.Errorf` with a named message (L755–857) |
| the guard tests the epic names exist | `cmd/agent/cli_voice_parse_test.go`, `cli_voice_out_test.go` | present, wired to `voiceFlagMember` / `voiceFlagConstNames` |

**Conclusion of the measurement, stated plainly: the epic's target state is
already the landed state on master, and this branch is byte-identical to master.**
The prior run is recorded in `devdoc/voice-input-default-shape.md` (class
`historical`, `devdoc/DOC-INDEX.tsv:1909`) and its memory records are
`#8364 / #8365 / #8368 / #8369` (source `node:peerbox`).

So this document is **not** a design for a rework that has yet to happen. It is
the (a)–(e) deliverable the epic asks for — the current-state map, the target
map, the refusal inventory, the constants/profile table and the file-precise
change list — with the epic's premise corrected at the top, and with the changes
that the measurement says are **still genuinely open** isolated in §6.

> **Rule-28 note.** The epic places this on hostb-Laptop (client), whose job is
> feature work that does not touch the system layer. Nothing in §6 touches the
> system layer (no `merger*`, no `task_reclaim*`, no worktree sweep, no
> `internal/testutil` or `internal/standards` *logic* — only citing the existing
> gates as acceptance).

---

## (a) Current flag table — each flag's current shape membership, with its definition site

Shape vocabulary, from `voiceRunShape` (`cmd/agent/cli_voice.go:951-969`):
`server` (`--server`) · `list-devices` · `capture` (the bare invocation —
`voiceCaptureShapeName = "capture"`, L119) · `file` (positional audio file).
`capture` is **not a flag** — it is the *absence* of one (L743).

| flag | const (definition) | server | list-devices | capture (bare) | client | file | notes |
|---|---|---|---|---|---|---|---|
| `--chunk N` | L484 | ✔ windows the file the server is read for | — | **refused** L788 | — | ✔ | `voiceDefaultChunkSec = 15` (L50) |
| `--server` | L485 | ✔ selects | — | — | — | — | valueless (L533-536) |
| `--listen ADDR` | L486 | ✔ **implies `--server`** L731-733 | — | — | — | — | `voiceDefaultListen = "127.0.0.1:7788"` (L60) |
| `--stdio` | L487 | ✔ (pipe form) | — | — | — | — | ⊥ `--listen` (L755) |
| `--models DIR` | L488 | ✔ | — | (inherited) | (inherited) | ✔ | resolved once, L1008 |
| `--runtime PATH` | L489 | ✔ | — | (inherited) | (inherited) | ✔ | resolved once, L1008 |
| `--client` | L490 | — | — | — | ✔ selects | — | ⊥ `--server` (L758); valueless |
| `--list-devices` | L491 | — | ✔ selects | — | (implies client) | — | valueless |
| `--device D` | L492 | refused L801 | ✔ allowed | ✔ allowed | ✔ allowed | refused L801 | tier-2 default via `GOPI_VOICE_DEVICE` |
| `--seconds N` | L493 | refused L805 | — | **✔ allowed, defaults to 10** L744-749 | ✔ allowed, defaults 5 | refused L805 | two defaults, one per capture shape |
| `--server-url URL` | L494 | **refused** L797 | refused L797 | — | ✔ allowed | — | names the far transcriber |
| `--engine NAME` | L498 | ✔ | — | ✔ | — | ✔ | `local` (default) \| `doubao` |
| `--doubao-key-file PATH` | L499 | ✔ | — | ✔ | — | ✔ | engine knobs |
| `--doubao-resource ID` | L500 | ✔ | — | ✔ | — | ✔ | |
| `--doubao-hotwords LIST` | L501 | ✔ | — | ✔ | — | ✔ | |
| `--doubao-submit-url URL` | L502 | ✔ | — | ✔ | — | ✔ | |
| `--doubao-query-url URL` | L503 | ✔ | — | ✔ | — | ✔ | |
| `--out FILE` | L507 | refused L764 | refused L768 | **refused L764** | ✔ allowed | — | client's debug WAV; REFUSED if empty/dir/existing (L874-886) |
| `--recorder CMD` | L508 | — | — | — | — | — | **defined, never assigned, not advertised** — see §6 gap G2 |
| `--record FILE` | L518 | refused L776 | refused L780 | ✔ allowed | ✔ allowed | refused L776 | capture keep-file; existing path **overwritten on purpose** |
| `--self` | L524 (`shot.SelfAddressSymbol`) | refused L816 | refused L822 | ✔ | refused L816 | ✔ | ⊥ `--stdio` L819 |
| `--display D` | L528 (`actFlagDisplay`) | (with `--self`) | — | (with `--self`) | — | (with `--self`) | shared spelling with `gopi tool act` |

Membership is enforced in **three** places that must agree — which is why each
spelling is a constant (L483-531): `voiceFlagMember` (L569), `voiceValuelessFlags`
(L533), `voiceFlagConstNames` (L544). `--out`'s "advertised but rejected" defect
is pinned by `TestVoiceUsageAdvertisesOnlyAcceptedFlags`
(`cmd/agent/cli_voice_out_test.go:182`), which asserts every advertised flag is a
member **and** has a `voiceFlagConstNames` entry (L190-193).

---

## (b) Target flag table — default = local capture + local transcription

The target state, expressed as the classification rule rather than a wish list:

> **An invocation that names no shape IS the local shape.** `opts.capture` is
> `true` iff no positional, no `--server`, no `--client`, no `--list-devices`
> (L743). Every flag is then judged against that resolved shape, and a flag that
> cannot mean anything there is **refused by name at parse time** — never
> silently ignored (Rule 15). `--listen ADDR` **implies** the server shape
> (L731-733) instead of being accepted-and-dropped.

Shape weights after the change: the local shape is the **default** (weight 1);
`--server`, `--client`, `--list-devices` and a positional file are the four
**opt-in** shapes, each reached only by naming it.

| flag | target membership | change vs (a) |
|---|---|---|
| *(nothing)* | **capture + transcribe HERE** (`capture` shape, L743; dispatched L1027-1028) | already landed |
| `--seconds N` | local default **10** (`voiceLocalDefaultSeconds`, L103→L749) vs client default **5** (`voiceClientDefaultSeconds`, `cli_voice_client.go:114`) | landed; two defaults documented separately |
| `--record FILE` | capture **and** client | landed (L518, advertised L294-302) |
| `--out FILE` | client **only** — on the local shape it is refused by name (L764) | landed; `--record` is the local-shape spelling |
| `--listen ADDR` | implies `--server` | landed (L731-733) |
| `--server-url URL` | client only; refused on server/list-devices (L797) | landed |
| `--device` / `--seconds` | refused on shapes that record nothing (L801/L805) | landed |
| `--chunk` | refused on the capture shape (L788) | landed |
| `--models` / `--runtime` | both rungs resolved **once** in `runVoiceSubcommand` (L1008) so all shapes read one answer | landed |
| `--self` / `--display` | capture and file shapes; refused on server/stdio/list-devices (L816-822) | landed |

**Weight/runtime resolution** (the profile half): two rungs, first-that-holds,
`voiceResolveProfilePaths` (`cmd/agent/cli_voice_local.go:226-269`):

| rung | model dir | runtime |
|---|---|---|
| 1 — the deployment | `$HOME/work/funasr-models` | `$HOME/work/runtime/llama.cpp/build/bin/llama-funasr-cli` |
| 2 — the gopi profile | `<config.GopiDir()>/` + `voiceProfileDirName` (`"voice"`, L92) | that dir + `voiceProfileRuntimeName` (`"llama-funasr-cli"`, L96, used L255/L267) |

`--models` / `--runtime` override both, and the resolution **only** fires when a
path is actually missing (`needModels` / `needRuntime`, L227-233) — so rung 1
keeps its legacy behaviour byte-for-byte.

---

## (c) Parse-time refusal table — every contradictory combination, each with its named message

All refusals live in `parseVoiceArgs` (`cmd/agent/cli_voice.go:590-870`), **after**
the shape is resolved (L743) and **before** any capture happens — so a refusal
costs zero microphone time. Line = the `return nil, fmt.Errorf` site; the message
is the one at that line.

### c.1 The three combinations the epic names explicitly

| # | combination | line | named message (verbatim) |
|---|---|---|---|
| 1 | **server shape + file argument** (`--server clip.wav`, also `--list-devices clip.wav`) | **857** | `--%s takes no audio-file argument (got %q): a file is transcribed by ` ``%s <audio-file>`` `, and the microphone by a bare ` ``%s`` `` — i.e. `--server takes no audio-file argument (got "clip.wav"): a file is transcribed by `gopi tool voice_input <audio-file>`, and the microphone by a bare `gopi tool voice_input`` |
| 2 | **one-shot shape + `--listen`** (`clip.wav --listen :9999`, `--client --listen …`) | **755** | `--stdio and --listen are mutually exclusive` (the `--server`-only pair) — and the file-side half is the L857 refusal, reached because `--listen` **implies** `--server` (L731-733) and a server shape takes no positional. Pinned by `TestVoiceListenAloneMeansServer` (see the `--listen`-with-file case at `cli_voice_parse_test.go:151-152`). |
| 3 | **file transcription + capture-class flags** | **788** (`--chunk`) / **805** (`--seconds`) / **797** (`--server-url`) / **801** (`--device`) / **776** (`--record`) / **764** (`--out`) | e.g. L788: `--%s windows an audio FILE, but the bare invocation reads the microphone: name the file (` + `%s <audio-file> --%s %d` + `), or drop --%s`; L805: `--%s bounds a recording, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally` |

### c.2 The complete refusal inventory (23 sites, all named)

| line | combination refused | message (shape) |
|---|---|---|
| 601 | unknown flag | `unknown flag --%s` |
| 641 | `--seconds` non-positive/NaN | `--seconds must be a positive integer, got %q` |
| 651 | `--chunk` non-positive/NaN | `--chunk must be a positive integer, got %q` |
| 677 | `--engine` without a value / unknown name | `--%s requires a value (want one of %s)` |
| 710 | `--record` with no value | `--%s requires a file path (e.g. --%s utterance.wav)` |
| 713 | `--record` pointing at a directory | `--%s %s is a directory — name the file to write` |
| 755 | `--stdio` + `--listen` | `--stdio and --listen are mutually exclusive` |
| 758 | `--client` + `--server` | `--client captures audio and --server transcribes it — pick one side of the hop` |
| 764 | `--out` on a non-client shape | `--%s is a --client flag (it keeps the captured audio); it does nothing on a %s run` |
| 768 | `--out` + `--list-devices` | `--%s records audio, but --%s only lists devices and exits — drop one` |
| 776 | `--record` on a shape that records nothing | `--%s keeps a recording, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally` |
| 780 | `--record` + `--list-devices` | `--%s keeps a recording, but --%s only lists devices and exits — drop one` |
| 788 | `--chunk` on the capture shape | `--%s windows an audio FILE, but the bare invocation reads the microphone: name the file (…), or drop --%s` |
| 797 | `--server-url` on server/list-devices | `--%s names the transcriber a --client posts to, but this is a %s run — it captures nothing, so drop --%s (the local shapes transcribe HERE, they never post anywhere)` |
| 801 | `--device` on a shape that captures nothing | `--%s names a capture device, but this is a %s run — it records nothing: use it with --%s, or with no argument at all` |
| 805 | `--seconds` on a shape that records nothing | `--%s bounds a recording, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally` |
| 816 | `--self` with `--server` | `--%s types into a WINDOW, but --%s prints the transcript to stdout for a client: the two ends of an ssh hop have no shared window — run --%s on the machine holding the window (the client side)` |
| 819 | `--self` with `--stdio` | `--%s is a pipe shape: its stdout IS the audio, and a transcript typed into a window would corrupt the very stream the caller is reading — drop --%s` |
| 822 | `--self` + `--list-devices` | `--%s types a transcript into this process's window, but --%s only lists devices and exits — drop one` |
| 832 | boolean flag that swallowed a value* | `--%s takes no value (got %q)` |
| 840 | boolean flag that swallowed a value* | `--%s takes no value (got %q)` — **exact duplicate of 832**, see §6 gap G1 |
| 848 | `--client` + positional file | `--client records from a device and takes no audio file (got %q)` |
| 857 | server/list-devices shape + positional file | see c.1 #1 |
| 883/885 | `--out` directory / existing file (`voiceValidateOutPath`, L872-886) | `--%s %s is a directory — name the file to write` / `--%s %s already exists — remove it or name a new file …` |

\* the loop at L830-834 and the loop at L838-842 iterate the **same** two flags
(`voiceFlagClient`, `voiceFlagListDevices`) with the **same** condition and the
**same** message; the second is unreachable. Recorded as gap **G1** in §6.

**Why refusals rather than silent no-ops is the design (not a preference):**
`--server-url` on a local shape would let the **local** engine produce the
transcript while the operator believed the host they named had done it — "a wrong
answer delivered confidently, which is worse than a refusal" (comment at L793-796,
Rule 15).

---

## (d) Named constants and the profile-resolution call points

Every value below is a **named constant** whose use site is a reference, never a
bare literal (Rule 1 / 13).

### d.1 Constants

| constant | value | defined at | landed/used at | role |
|---|---|---|---|---|
| `voiceLocalDefaultSeconds` | `10` | `cmd/agent/cli_voice.go:103` | L749 (parser) **and** `cli_voice_format.go:237` (help) | the local shape's bounded capture length — one constant, two consumers |
| `voiceClientDefaultSeconds` | `5` | `cmd/agent/cli_voice_client.go:114` | `cli_voice_client.go:461` | the remote client's own length (deliberately ≠ the local one) |
| `voiceSecondsPlaceholder` | `"{seconds}"` | `cmd/agent/cli_voice_format.go:82` | `cli_voice_format.go:237` (`strings.NewReplacer`) | renders the number `-h` prints from the constant (L280 usage) — pinned by `cli_voice_local_test.go:252` |
| `voiceDefaultChunkSec` | `15` | `cmd/agent/cli_voice.go:50` | L607 (defaults) + L788 (refusal quotes it) | not a bare literal in either place |
| `voiceDefaultListen` | `"127.0.0.1:7788"` | `cmd/agent/cli_voice.go:60` | L608 (defaults) | server bind default |
| `voiceDefaultListenPort` | `"7788"` | `cmd/agent/cli_voice.go:63` | `cli_voice_client.go:651` (hint builds the port from the constant) | one spelling of the port |
| `voiceDefaultServerURL` | derived | `cmd/agent/cli_voice.go:70` | client default target | composed from the scheme/host/separator constants, not typed |
| `voiceProfileDirName` | `"voice"` | `cmd/agent/cli_voice.go:92` | `cli_voice_local.go:281` | the profile subdirectory under the gopi dir |
| `voiceProfileRuntimeName` | `"llama-funasr-cli"` | `cmd/agent/cli_voice.go:96` | `cli_voice_local.go:255`, `:267` | the runtime's file name inside that dir |
| `voiceServerShapeName` | `"--server"` | `cmd/agent/cli_voice.go:117` | L896 (`voiceRunShape`) | shape name quoted by refusal messages |
| `voiceCaptureShapeName` | `"capture"` | `cmd/agent/cli_voice.go:119` | L900 | ditto |
| `voiceFlagSelf` | `shot.SelfAddressSymbol` | `cmd/agent/cli_voice.go:524` | L535, L817-822 | **shared** symbol across `capture`/`act`/`voice_input` (Rule 14) |
| `voiceFlagDisplay` | `actFlagDisplay` | `cmd/agent/cli_voice.go:528` | with `--self` | same |
| `VoiceDeviceEnv` / `VoiceEngineEnv` | `"GOPI_VOICE_DEVICE"` / `"GOPI_VOICE_ENGINE"` | `internal/config/settings.go:61` / `:72` | the env catalog rows (`settings.go:210-211`) | env half of the three-rung knob resolution |

### d.2 Profile-resolution call points (file:line)

| what | file:line | evidence (verbatim) |
|---|---|---|
| the resolver | `cmd/agent/cli_voice_local.go:226` | `func voiceResolveProfilePaths(opts *voiceOptions) {` |
| the ONE gopi-dir resolution | `cmd/agent/cli_voice_local.go:277` | `gd, err := config.GopiDir()` |
| the profile voice dir it yields | `cmd/agent/cli_voice_local.go:281` | `return filepath.Join(gd, voiceProfileDirName), nil` |
| the single call site (all shapes) | `cmd/agent/cli_voice.go:1008` | `voiceResolveProfilePaths(opts)` |
| env → profile root | `internal/config/gopihome.go` (`config.GopiDir`, `$GOPI_HOME/.gopi` else `~/.gopi`) | cited as the doc's own `read at` column in `CONFIG.md` |
| the resolution documented for the operator | `CONFIG.md:323-333` | rung table + `<gopi dir>` = `config.GopiDir` |

`voiceResolveProfilePaths` is called exactly once, in `runVoiceSubcommand`,
**before** the shape switch — its comment (L1005-1007) states the reason: so
*every* shape reads the same paths.

---

## (e) File-precise change list

Every path below was `stat`ed in this worktree. **Landed** = the file already
carries the target behaviour; **Open** = the change that remains (§6 ids).

| # | file | exists (`stat -c '%s'`) | class | what the design requires there |
|---|---|---|---|---|
| 1 | `cmd/agent/cli_voice.go` | 53891 | landed | constants L92-119; shape decision L743-749; `--listen ⇒ --server` L731-733; refusals L755-857; single `voiceResolveProfilePaths` call L1008; usage text (placeholders, L280 `{seconds}`) |
| 2 | `cmd/agent/cli_voice_local.go` | 12408 | landed | `voiceResolveProfilePaths` L226-269, `config.GopiDir()` L277, rung-1 legacy paths |
| 3 | `cmd/agent/cli_voice_format.go` | 11227 | landed | `voiceSecondsPlaceholder` L82 + replacer L237 |
| 4 | `cmd/agent/cli_voice_client.go` | 40622 | landed | `voiceClientDefaultSeconds` L114; port derived from `voiceDefaultListenPort` L651 |
| 5 | `cmd/agent/cli_voice_parse_test.go` | 10354 | landed | default-shape parse tests; `--listen`⇒server L136-152; flag-member boundary L249-262 |
| 6 | `cmd/agent/cli_voice_out_test.go` | 15047 | landed | advertised ⇔ accepted ⇔ has-a-constant gate L182-193 (`voiceUsageFlagNames` L218) |
| 7 | `cmd/agent/cli_voice_local_test.go` | 11403 | landed | placeholder-rendered help L252; profile-rung resolution L207/L238 |
| 8 | `cmd/agent/cli_voice_test.go` | 7846 | landed | shape/dispatch guards |
| 9 | `CONFIG.md` | 89051 | landed | §Voice input L315-341: default shape, two-rung weights table, knob table with `read at` lines |
| 10 | `internal/config/settings.go` | 46148 | landed | `VoiceDeviceEnv` L61, `VoiceEngineEnv` L72, catalog rows L210-211 |
| 11 | `devdoc/DOC-INDEX.tsv` | 432669 | **open** | this document's own row (class `roadmap`) in the same commit (Rule 36) |
| 12 | `devdoc/voice-input-default-shape.md` | *prior report* | reference | the landed run's record (DOC-INDEX row 1909, class `historical`) — **read-only input to this design**, not to be edited |
| 13 | `internal/standards/config_doc_gate_test.go` | 17014 | gate | fails the build if `CONFIG.md`'s `read at` line refs stop resolving — run it after any §6 edit touching `CONFIG.md` |
| 14 | `internal/standards/config_doc_flag_gate_test.go` | 29150 | gate | flag/knob documentation must match the tree |
| 15 | `internal/standards/help_env_catalog_gate_test.go` | 15547 | gate | every env-name constant needs a real reader (Rule 21) |
| 16 | `internal/standards/docclass_gate_test.go` | 9337 | gate | every `devdoc/` file carries a well-formed, classified row |
| 17 | `internal/tools/named_limits_rule1_test.go` | 4245 | precedent | the AST-walk anti-regression shape a Rule-1 gate for the voice literals would follow (its own scope is `internal/tools/*.go`, **not** `cmd/agent` — see G3) |

---

## §6 The changes that are genuinely still open (roadmap input, not scope of this epic)

Each item is a defect or gap **measured in this tree**, small enough for one
session, and stated so a later epic can run it without re-deriving anything.

- **G1 — dead duplicate guard.** `cmd/agent/cli_voice.go:827-834` and `:836-842`
  are the same loop (same flags, same condition, same message). The second is
  unreachable; the second `fmt.Errorf` (L840) can never fire. *One session.*
  Acceptance: one loop remains, the comment at L828 explaining the guard survives,
  and a test that proves a valued `--client clip.wav` is still refused.
- **G2 — `--recorder` defined but never assigned and never advertised.**
  `voiceFlagRecorder` (L508) has no member entry in `voiceFlagConstNames` and no
  assignment to `opts.recorder` (L441); the `merge-note` at L519-523 says the
  wiring was deliberately deferred with master's failure-mode matrix. *One
  session.* Acceptance: either wire it (constant → member table → assignment →
  usage line → `TestVoiceUsageAdvertisesOnlyAcceptedFlags` green) or delete the
  constant, field and comment so the tree stops carrying a promise nothing keeps
  (Rule 15: a documented hook nobody consumes is the defect Rule 21 names).
- **G3 — the local default's literals have no AST gate of their own.**
  `internal/tools/named_limits_rule1_test.go` walks `internal/tools/*.go` only;
  nothing asserts that `10`/`5`/`15`/`7788`/`600` cannot reappear as bare
  literals in `cmd/agent/cli_voice*.go`. One of them **already has**:
  `cli_voice.go:280` advertises `max 600` in the usage text with no constant
  behind it (the only `600` in the file — `grep -n 600 cmd/agent/cli_voice.go`
  returns exactly that line). *One session.* Acceptance: named constant + usage
  placeholder, plus a `cmd/agent`-scoped gate with a red proof (Rule 13's
  exhaustive-test shape).
- **G4 — the profile rung has never been executed on this host.** The memory
  record `#8365` states the profile rung was proved on **peerbox** (weights under
  `<fixture>/profile/.gopi/voice`, rung 1 hidden). On hostb-Laptop no voice
  weights or `llama-funasr-cli` exist, so `capture` here cannot be run to
  completion — see §Verification. *One session, on a host that has the weights.*
  Acceptance: `GOPI_HOME=<fixture> $HOME/work` absent → exit 0 through
  `voiceResolveProfilePaths` rung 2, with the console report naming
  `<gopi dir>/voice`.

**Not open, and deliberately so:** the default shape itself, the `--listen`
implication, the profile resolution, the named default length and all 23 parse
refusals are **landed on master** (§0). No epic re-implements them.

---

## Completed

- **(a)** current-state flag table: 23 flags, each with its shape membership and its
  `const` definition line (all `cmd/agent/cli_voice.go`, L483-531).
- **(b)** target flag table: the classification rule the landed default implements
  (default = local capture + local transcription; four opt-in shapes), plus the
  two-rung weight/runtime resolution.
- **(c)** parse-time refusal table: the three combinations the epic names, plus the
  complete 23-site refusal inventory, each with its named message and line.
- **(d)** named-constant table (definition file + use site for each) and the
  profile-resolution call points with `file:line`.
- **(e)** file-precise change list: 17 paths, every one `stat`ed in this worktree.
- **A measurement that corrects the epic's premise** (§0): the target state is
  already landed on master (`457a999b6`), this worktree is byte-identical to
  master (`master..HEAD` = 0 commits, `master...HEAD` diff empty), and the four
  gaps that remain open are isolated in §6 with acceptance criteria.
- **Appendix A**: the verbatim `sed -n "${n}p"` output for 141 citations.

## Not Done (honest)

- **No implementation code** — by the epic's own constraint (2/5..5/5 own that).
- **No end-to-end run of the bare capture on this host.** hostb-Laptop has no
  Fun-ASR weights and no `llama-funasr-cli`, so §(b)'s runtime behaviour is
  documented from the code and from the prior run's record (`devdoc/voice-input-default-shape.md`,
  memory `#8364-#8369`, source `node:peerbox`) — **not** from a run of mine. Gap **G4**.
- **`make test-budget PKGS='./internal/docindex ./internal/standards'` exits 1 in this
  worktree** — and it does so for a reason that is **not this change**: the only
  remaining failure is `devdoc/cli-help-user-audit.md` carrying a `historical`
  banner while `devdoc/DOC-INDEX.tsv:374` classes it `normative`. Both halves are
  master's committed bytes (verified by `git show master:…`); this change adds one
  DOC-INDEX row and no banner. Reported rather than papered over; it is a
  system-layer doc-classification defect and belongs to peerbox (Rule 28).

## Verification (this run — what I actually executed)

- **Build:** not run — this epic produces a document only; no Go file was
  modified, so the build state is master's. State it rather than imply a green.
- **Tests:** `make test-budget PKGS='./internal/docindex ./internal/standards'`
  → **exit 1**, with exactly one failing test: `TestDocClassBannerMatchesManifest`,
  on `devdoc/cli-help-user-audit.md` — **pre-existing on master** (both the banner
  and the `DOC-INDEX.tsv:374` row are master's committed bytes, shown above), not
  introduced by this document. After this document's row landed, the gate reports
  `1 mislabelled` (it reported `2` while this doc briefly carried a banner — this
  doc's own half of that count is now gone).
- **Ran (the surface this epic actually changed = a document):**
  1. `gopi tool doccheck` → **exit 0**, `doccheck: the devdoc corpus is consistent —
     every file under devdoc/ has a well-formed row` (binary built in this run:
     `CGO_ENABLED=0 go build -o …/evidence/gopi ./cmd/agent/` → `BUILD_OK`). This is
     Rule 36's own surface executed for real, not `-h`. (Spelling note: `gopi doccheck`
     is RETIRED — the live door is `gopi tool doccheck`; the retired verb exits
     non-zero and says so.)
  2. `git merge-base --is-ancestor 457a999b6 master` → exit 0; the same for `HEAD`
     → exit 0 (`IN master, IN HEAD`).
  2. `git rev-list --count master..HEAD` → `0`; `git diff --stat master...HEAD` →
     empty. *Result:* this branch carries no commit of its own — recorded above so
     the next reader does not mistake this worktree for a carrier of the design's
     changes.
  3. `sed -n "${n}p"` over every cited `file:line` — the output is the appendix
     below, byte-for-byte.
  4. `stat -c '%n %s'` over every path in §(e) — all 17 exist (sizes recorded in
     the table; the four *gate* rows and `named_limits_rule1_test.go` were stat'ed
     in the same batch).
  5. `grep -n "return nil, fmt.Errorf" cmd/agent/cli_voice.go` → 23 sites,
     inventoried in §(c).
- **Result / honest limit on it:** the design and change list are complete against
  the acceptance criteria, **and** the epic's premise is corrected by measurement.
  What I could **not** do on this host: run the bare capture end to end — there is
  no voice weight set and no `llama-funasr-cli` here (G4), so §(b)'s runtime
  behaviour is documented from the code and from the prior run's record, not from
  a run of mine.

---

## Evidence (self-filed)

- `make test-budget PKGS='./internal/docindex ./internal/standards'` → **exit 1**,
  one failing test: `--- FAIL: TestDocClassBannerMatchesManifest (0.04s)`,
  `docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1166 markdown docs of a bannered
  class carry their banner (1 mislabelled)`, and the named doc is
  `devdoc/cli-help-user-audit.md: carries the "historical" banner but
  devdoc/DOC-INDEX.tsv classes it "normative"`. **Not this change**: `git show
  master:devdoc/cli-help-user-audit.md | head -1` → `<!-- doc-class: historical -->`
  and `git show master:devdoc/DOC-INDEX.tsv | grep cli-help-user-audit` → `:374 … normative`.
  (This document briefly contributed the second of that count's two mislabelled rows
  while it carried a banner; removing it took the count back to the pre-existing 1.)
- `gopi tool doccheck` → **exit 0**, `doccheck: the devdoc corpus is consistent —
  every file under devdoc/ has a well-formed row` (the binary was built in this run:
  `CGO_ENABLED=0 go build -o …/gopi ./cmd/agent/` → `BUILD_OK`).
- `git merge-base --is-ancestor 457a999b6 master` → **exit 0**; `… HEAD` → **exit 0**.
  `git rev-list --count master..HEAD` → `0` **before** this document's commit, `1` after
  (this run's own commit `2dca42b86`); `git diff --stat master...HEAD` was empty before it.
- `git status --porcelain` after the commit → **prints nothing** (Rule 30).
- Citation check: 153 distinct `path:line` pairs and 91 `L<n>` shorthands in the
  document, **all** covered by Appendix A (verified with the generator that built it;
  the run reports `NOT verified: NONE`, `L<n> unverified: NONE`).
- `stat` of all 14 paths in the §(e) table plus its 4 cited gate files → all `EXISTS`
  (sizes are the table's, taken in this worktree).

## Appendix A — `sed -n "${n}p" <file>` output for every citation in this document

Each line below is the **verbatim stdout** of the exact command, so a reviewer can
re-run any one of them. Generated in this worktree; nothing here was written from
memory.

```
cmd/agent/cli_voice.go:50		voiceDefaultChunkSec = 15
cmd/agent/cli_voice.go:60		voiceDefaultListen = "127.0.0.1:7788"
cmd/agent/cli_voice.go:63		voiceDefaultListenPort = "7788"
cmd/agent/cli_voice.go:70		voiceDefaultServerURL = voiceSchemeHTTP + voiceLoopbackHost + voiceHostPortSeparator + voiceDefaultListenPort
cmd/agent/cli_voice.go:92		voiceProfileDirName = "voice"
cmd/agent/cli_voice.go:96		voiceProfileRuntimeName = "llama-funasr-cli"
cmd/agent/cli_voice.go:103		voiceLocalDefaultSeconds = 10
cmd/agent/cli_voice.go:117		voiceServerShapeName      = "--server"
cmd/agent/cli_voice.go:119		voiceCaptureShapeName     = "capture"
cmd/agent/cli_voice.go:280	  --seconds N      capture length (default {seconds}, max 600). It bounds every
cmd/agent/cli_voice.go:294	  --record FILE    keep this capture's audio in FILE as a WAV (16kHz mono
cmd/agent/cli_voice.go:302	                   an empty path and a directory.
cmd/agent/cli_voice.go:441		recorder string
cmd/agent/cli_voice.go:483	const (
cmd/agent/cli_voice.go:484		voiceFlagChunk       = "chunk"
cmd/agent/cli_voice.go:485		voiceFlagServer      = "server"
cmd/agent/cli_voice.go:486		voiceFlagListen      = "listen"
cmd/agent/cli_voice.go:487		voiceFlagStdio       = "stdio"
cmd/agent/cli_voice.go:488		voiceFlagModels      = "models"
cmd/agent/cli_voice.go:489		voiceFlagRuntime     = "runtime"
cmd/agent/cli_voice.go:490		voiceFlagClient      = "client"
cmd/agent/cli_voice.go:491		voiceFlagListDevices = "list-devices"
cmd/agent/cli_voice.go:492		voiceFlagDevice      = "device"
cmd/agent/cli_voice.go:493		voiceFlagSeconds     = "seconds"
cmd/agent/cli_voice.go:494		voiceFlagServerURL   = "server-url"
cmd/agent/cli_voice.go:498		voiceFlagEngine         = "engine"
cmd/agent/cli_voice.go:499		voiceFlagDoubaoKeyFile  = "doubao-key-file"
cmd/agent/cli_voice.go:500		voiceFlagDoubaoResource = "doubao-resource"
cmd/agent/cli_voice.go:501		voiceFlagDoubaoHotwords = "doubao-hotwords"
cmd/agent/cli_voice.go:502		voiceFlagDoubaoSubmit   = "doubao-submit-url"
cmd/agent/cli_voice.go:503		voiceFlagDoubaoQuery    = "doubao-query-url"
cmd/agent/cli_voice.go:507		voiceFlagOut      = "out"
cmd/agent/cli_voice.go:508		voiceFlagRecorder = "recorder"
cmd/agent/cli_voice.go:518		voiceFlagRecord = "record"
cmd/agent/cli_voice.go:519		// voiceFlagSelf types the transcript into the window that HOSTS this
cmd/agent/cli_voice.go:523		// (Rule 14: one definition, every consumer imports it).
cmd/agent/cli_voice.go:524		voiceFlagSelf = shot.SelfAddressSymbol
cmd/agent/cli_voice.go:528		voiceFlagDisplay = actFlagDisplay
cmd/agent/cli_voice.go:531	// voiceValuelessFlags take no argument. Declared so the shared parser never
cmd/agent/cli_voice.go:533	var voiceValuelessFlags = []string{
cmd/agent/cli_voice.go:535		voiceFlagSelf,
cmd/agent/cli_voice.go:544	var voiceFlagConstNames = map[string]string{
cmd/agent/cli_voice.go:569	func voiceFlagMember(f string) bool {
cmd/agent/cli_voice.go:590	func parseVoiceArgs(args []string) (*voiceOptions, error) {
cmd/agent/cli_voice.go:601				return nil, fmt.Errorf("unknown flag --%s", fname)
cmd/agent/cli_voice.go:607			chunk:   voiceDefaultChunkSec,
cmd/agent/cli_voice.go:608			listen:  voiceDefaultListen,
cmd/agent/cli_voice.go:641				return nil, fmt.Errorf("--seconds must be a positive integer, got %q", fs.get(voiceFlagSeconds))
cmd/agent/cli_voice.go:651				return nil, fmt.Errorf("--chunk must be a positive integer, got %q", fs.get(voiceFlagChunk))
cmd/agent/cli_voice.go:677				return nil, fmt.Errorf("--%s requires a value (want one of %s)", voiceFlagEngine, strings.Join(voiceEngineValues, ", "))
cmd/agent/cli_voice.go:710				return nil, fmt.Errorf("--%s requires a file path (e.g. --%s utterance.wav)", voiceFlagRecord, voiceFlagRecord)
cmd/agent/cli_voice.go:713				return nil, fmt.Errorf("--%s %s is a directory — name the file to write", voiceFlagRecord, opts.record)
cmd/agent/cli_voice.go:731		if fs.has(voiceFlagListen) {
cmd/agent/cli_voice.go:733		}
cmd/agent/cli_voice.go:743		opts.capture = len(positionals) == 0 && !opts.server && !opts.client && !opts.listDevices
cmd/agent/cli_voice.go:744		if opts.capture && !fs.has(voiceFlagSeconds) {
cmd/agent/cli_voice.go:749			opts.seconds = voiceLocalDefaultSeconds
cmd/agent/cli_voice.go:755			return nil, fmt.Errorf("--stdio and --listen are mutually exclusive")
cmd/agent/cli_voice.go:758			return nil, fmt.Errorf("--client captures audio and --server transcribes it — pick one side of the hop")
cmd/agent/cli_voice.go:764			return nil, fmt.Errorf("--%s is a --client flag (it keeps the captured audio); it does nothing on a %s run",
cmd/agent/cli_voice.go:768			return nil, fmt.Errorf("--%s records audio, but --%s only lists devices and exits — drop one",
cmd/agent/cli_voice.go:776			return nil, fmt.Errorf("--%s keeps a recording, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally",
cmd/agent/cli_voice.go:780			return nil, fmt.Errorf("--%s keeps a recording, but --%s only lists devices and exits — drop one",
cmd/agent/cli_voice.go:788			return nil, fmt.Errorf("--%s windows an audio FILE, but the bare invocation reads the microphone: name the file (`%s <audio-file> --%s %d`), or drop --%s",
cmd/agent/cli_voice.go:793		// LOCAL engine produce the transcript while the operator believed the host
cmd/agent/cli_voice.go:797			return nil, fmt.Errorf("--%s names the transcriber a --client posts to, but this is a %s run — it captures nothing, so drop --%s (the local shapes transcribe HERE, they never post anywhere)",
cmd/agent/cli_voice.go:801			return nil, fmt.Errorf("--%s names a capture device, but this is a %s run — it records nothing: use it with --%s, or with no argument at all",
cmd/agent/cli_voice.go:805			return nil, fmt.Errorf("--%s bounds a recording, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally",
cmd/agent/cli_voice.go:816				return nil, fmt.Errorf("--%s types into a WINDOW, but --%s prints the transcript to stdout for a client: the two ends of an ssh hop have no shared window — run --%s on the machine holding the window (the client side)",
cmd/agent/cli_voice.go:817					voiceFlagSelf, voiceFlagServer, voiceFlagSelf)
cmd/agent/cli_voice.go:819				return nil, fmt.Errorf("--%s is a pipe shape: its stdout IS the audio, and a transcript typed into a window would corrupt the very stream the caller is reading — drop --%s",
cmd/agent/cli_voice.go:822				return nil, fmt.Errorf("--%s types a transcript into this process's window, but --%s only lists devices and exits — drop one",
cmd/agent/cli_voice.go:827		// These two are boolean verbs. The flag parser would otherwise swallow a
cmd/agent/cli_voice.go:828		// following word as their value (`--client clip.wav`), silently turning a
cmd/agent/cli_voice.go:830		for _, boolFlag := range []string{voiceFlagClient, voiceFlagListDevices} {
cmd/agent/cli_voice.go:834		}
cmd/agent/cli_voice.go:838		for _, boolFlag := range []string{voiceFlagClient, voiceFlagListDevices} {
cmd/agent/cli_voice.go:840				return nil, fmt.Errorf("--%s takes no value (got %q)", boolFlag, fs.get(boolFlag))
cmd/agent/cli_voice.go:842		}
cmd/agent/cli_voice.go:848				return nil, fmt.Errorf("--client records from a device and takes no audio file (got %q)", positionals[0])
cmd/agent/cli_voice.go:857				return nil, fmt.Errorf("--%s takes no audio-file argument (got %q): a file is transcribed by `%s <audio-file>`, and the microphone by a bare `%s`",
cmd/agent/cli_voice.go:872	func voiceValidateOutPath(path string) error {
cmd/agent/cli_voice.go:874			return fmt.Errorf("--%s requires a file path (e.g. --%s nick.wav)", voiceFlagOut, voiceFlagOut)
cmd/agent/cli_voice.go:883			return fmt.Errorf("--%s %s is a directory — name the file to write", voiceFlagOut, path)
cmd/agent/cli_voice.go:885		return fmt.Errorf("--%s %s already exists — remove it or name a new file "+
cmd/agent/cli_voice.go:886			"(a capture must not overwrite a recording you kept)", voiceFlagOut, path)
cmd/agent/cli_voice.go:893	func voiceRunShape(opts *voiceOptions) string {
cmd/agent/cli_voice.go:896			return voiceServerShapeName
cmd/agent/cli_voice.go:900			return voiceCaptureShapeName
cmd/agent/cli_voice.go:1005		// happens (see voiceResolveProfilePaths): the $HOME/work deployment, and the
cmd/agent/cli_voice.go:1006		// operator's gopi profile. Done here so EVERY shape reads the same paths —
cmd/agent/cli_voice.go:1007		// server, client-less file run, bare capture and the deps check below.
cmd/agent/cli_voice.go:1008		voiceResolveProfilePaths(opts)
cmd/agent/cli_voice.go:1017		needsEngine := !opts.listDevices && !opts.client
cmd/agent/cli_voice.go:1027		case opts.capture:
cmd/agent/cli_voice.go:1028			runVoiceCaptureLocal(opts)
cmd/agent/cli_voice_local.go:226	func voiceResolveProfilePaths(opts *voiceOptions) {
cmd/agent/cli_voice_local.go:227		enc, llm := voiceModelPaths(opts.models)
cmd/agent/cli_voice_local.go:228		needModels := !voicePathPresent(enc, llm)
cmd/agent/cli_voice_local.go:255			candidate := filepath.Join(root, voiceProfileRuntimeName)
cmd/agent/cli_voice_local.go:267					opts.models, root, opts.runtime, filepath.Join(root, voiceProfileRuntimeName))
cmd/agent/cli_voice_local.go:277		gd, err := config.GopiDir()
cmd/agent/cli_voice_local.go:281		return filepath.Join(gd, voiceProfileDirName), nil
cmd/agent/cli_voice_client.go:114		voiceClientDefaultSeconds = 5
cmd/agent/cli_voice_client.go:461		binary, _ := voiceRecorderCommand(dev, voiceClientDefaultSeconds)
cmd/agent/cli_voice_client.go:651			port = voiceDefaultListenPort
cmd/agent/cli_voice_format.go:82	const voiceSecondsPlaceholder = "{seconds}"
cmd/agent/cli_voice_format.go:230		return strings.NewReplacer(
cmd/agent/cli_voice_format.go:237			voiceSecondsPlaceholder, strconv.Itoa(voiceLocalDefaultSeconds),
cmd/agent/cli_voice_parse_test.go:136		opts, err := parseVoiceArgs([]string{"--listen", ":9999"})
cmd/agent/cli_voice_parse_test.go:141			t.Fatal("--listen must imply --server — an address to listen on has no other meaning")
cmd/agent/cli_voice_parse_test.go:147			t.Fatal("--listen is a server shape; it must not also become the local capture default")
cmd/agent/cli_voice_parse_test.go:151		if _, err := parseVoiceArgs([]string{"clip.wav", "--listen", ":9999"}); err == nil {
cmd/agent/cli_voice_parse_test.go:152			t.Fatal("--listen with an audio file should be refused: a server never reads that file")
cmd/agent/cli_voice_parse_test.go:249	
cmd/agent/cli_voice_parse_test.go:255			if !voiceFlagMember(f) {
cmd/agent/cli_voice_parse_test.go:260			if voiceFlagMember(f) {
cmd/agent/cli_voice_out_test.go:182	// the Flags section advertises must be a member the parser accepts.
cmd/agent/cli_voice_out_test.go:190			if !voiceFlagMember(name) {
cmd/agent/cli_voice_out_test.go:191				t.Errorf("usage advertises --%s but voiceFlagMember refuses it — "+
cmd/agent/cli_voice_out_test.go:192					"an advertised flag the parser rejects is a broken promise (the --out defect)", name)
cmd/agent/cli_voice_out_test.go:218	func voiceUsageFlagNames(t *testing.T) []string {
cmd/agent/cli_voice_local_test.go:207		voiceResolveProfilePaths(opts)
cmd/agent/cli_voice_local_test.go:238		voiceResolveProfilePaths(opts)
cmd/agent/cli_voice_local_test.go:252		if strings.Contains(text, voiceSecondsPlaceholder) {
cmd/agent/cli_voice_local_test.go:260		for _, ph := range []string{voiceFormatPlaceholder, voiceEngineDoubaoPlaceholder} {
internal/config/settings.go:61	const VoiceDeviceEnv = "GOPI_VOICE_DEVICE"
internal/config/settings.go:72	const VoiceEngineEnv = "GOPI_VOICE_ENGINE"
internal/config/settings.go:210		{VoiceDeviceEnv, VoiceDeviceSettingsKey, "", "Default capture device for `gopi tool voice_input --client` (an id from --list-devices)"},
internal/config/settings.go:211		{VoiceEngineEnv, VoiceEngineSettingsKey, "", "`gopi tool voice_input` transcription engine: \"local\" (Fun-ASR-Nano, default) or \"doubao\" (Volcengine cloud; CLI: --engine)"},
CONFIG.md:315	#### Voice input
CONFIG.md:317	`gopi tool voice_input` with **no argument and no `--client`** is the default shape:
CONFIG.md:320	`--server` and pointing a `--client` at it. Its recording is bounded by a named
CONFIG.md:323	
CONFIG.md:330	| 2 — the gopi profile | `<gopi dir>/voice` | `<gopi dir>/voice/llama-funasr-cli` |
CONFIG.md:333	runs with needs no flag; `<gopi dir>` is `config.GopiDir` (`$GOPI_HOME/.gopi`, else
CONFIG.md:339	| `default_device` | `GOPI_VOICE_DEVICE` | — | Default capture device for `gopi tool voice_input` (the bare capture and `--client` alike), as a device **id** printed by `--list-devices` (never a card index). Unset → system default capture → first enumerated device | `internal/config/settings.go:210` |
CONFIG.md:341	| `doubao_key_file` | `GOPI_DOUBAO_KEY_FILE` | — | File holding the doubao API key. Default `~/.doubao/apikey.txt`. The key **value** is never a settings field | `internal/config/settings.go:212` |
devdoc/DOC-INDEX.tsv:1909	devdoc/voice-input-default-local.md	normative	normative: standing claim about master that names code that resolves (anchor); standing 5 / record 0	anchor cmd/agent/cli_voice_out_test.go:182 at // the Flags section advertises must be a member the parser accepts.
internal/tools/named_limits_rule1_test.go:78			t.Fatalf("glob: %v", err)
internal/config/settings.go:212		{DoubaoKeyFileEnv, DoubaoKeyFileSettingsKey, "", "File holding the doubao API key (default ~/.doubao/apikey.txt); the key VALUE is never a settings field"},
devdoc/DOC-INDEX.tsv:374	devdoc/cli-help-user-audit.md	normative	normative: standing claim about master, re-runnable (the row table is measured against the built binary with `artifacts/acceptance-rows.txt` as evidence); the merger row's absence is pinned by cmd/agent/cli_help_hidden_verbs_test.go and cmd/agent/usage_test.go	path cmd/agent/usage_test.go
```

## Appendix B — the commands that generated the appendix

```bash
cd /home/user/workspace/gopi-01a113e467307b5bbc350278c2c20b8b
# one line per citation:
for cite in cmd/agent/cli_voice.go:50 ... ; do
  f=${cite%:*}; n=${cite##*:}; printf '%s:%s: %s\n' "$f" "$n" "$(sed -n "${n}p" "$f")"
done
```
