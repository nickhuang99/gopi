<!-- doc-class: historical -->
# The SAUC credential moves into gopi's own settings document

2026-10-14 · task `01a13626ef01787b82b2271e9ac74782` · branch
`task/01a13626ef01787b82b2271e9ac74782`

## The asymmetry this closed

The v3 SAUC engine (`--engine sauc`, `gopi tool voice_input`, `gopi chat`'s
`/voice`) takes the vendor's **legacy console pair** — `X-Api-App-Key` +
`X-Api-Access-Key`, the `APPID` / `TOKEN` lines of a `KEY=VALUE` document — and
until today that document could only be the VENDOR'S OWN file,
`/home/user/.doubao/v2_voice.env`.

That is the defect, and it is not cosmetic: `audio_engine_param.sauc` modelled
`endpoint` and `resource_id` and nothing else, and `ResolveAudioSauc` read no
environment variable at all — its comment said the credential was "deliberately
absent ... never stored inline in a document gopi reads for everything else". A
gopi deployment that was correct everywhere else silently depended on a
directory belonging to another program, whose contents gopi neither owns nor
backs up.

The `streaming` block had already closed exactly this asymmetry for **the same
vendor pair** (`audio_engine_param.streaming.app_id` / `.token`,
`$GOPI_AUDIO_APP_ID` / `$GOPI_AUDIO_TOKEN`). So this is not a new surface: it is
the second half of a decision gopi had already made, applied to the block that
was left behind.

## What was decided, and why each way

**The keys are `app_id` and `token` — the streaming block's vocabulary.** The
vendor issues ONE application id and ONE access token per account; a second
spelling for the same two values would be a second name an operator has to
discover. It also makes the redaction automatic: `gopi config show` redacts by
LEAF name (`config.IsAudioCredentialParamName`), so the pair inside the `sauc`
block renders as `<set: N chars>` with no new redaction rule — measured in this
run:

```
  audio_engine_param.sauc.app_id        <set: 10 chars>
  audio_engine_param.sauc.token         <set: 32 chars>
```

**The environment names are the streaming block's too** — `$GOPI_AUDIO_APP_ID` /
`$GOPI_AUDIO_TOKEN`, with the pre-rename `$GOPI_DOUBAO_APP_ID` /
`$GOPI_DOUBAO_TOKEN` at the SAME rung (the existing `overEnv` policy, applied).
Not two new names: one vendor, one account, one pair of variables, and the rung
order already published.

**The transport knobs got no env channel, on purpose.** `endpoint` and
`resource_id` are neither secret nor per-invocation; the surface already has
seven environment channels and a URL is set per host in the document. A new
name has to be discovered before it can be used, for a value the operator
already stores (Rule 35's "what breaks without it" — nothing did).

**The precedence, and the one non-obvious rung:**

```
a FILE named on an explicit channel
      --doubao-key-file  >  $GOPI_AUDIO_KEY_FILE ($GOPI_DOUBAO_KEY_FILE)
                         >  settings "audio_engine_param.recording.key_file"
   >  the COMPLETE inline pair
      settings "audio_engine_param.sauc.app_id" + ".token"
      (or $GOPI_AUDIO_APP_ID + $GOPI_AUDIO_TOKEN over it)
   >  the LEGACY vendored file  ~/.doubao/v2_voice.env  — ANNOUNCED
```

A named **file** outranks the inline pair because a path is the operator's own
statement about where the credential lives — the v2 arm settled the same
question the same way (`voiceDoubaoKeySource`). What the new middle rung buys is
the case that previously had no answer: a host where the credential is in gopi's
document and the vendor's directory does not exist at all.

**The legacy file is a FALLBACK, not a refusal, and the fallback is announced.**
It is the rung that keeps every pre-existing deployment working; a silent
fallback would be the Rule 15 defect, so a run that ends on it prints the path
that answered AND the settings path that supersedes it.

**A half-stated pair is refused BY NAME, and it is refused even when another
rung could have answered.** The two halves are ONE credential: a stated
`app_id` mixed with a file's `TOKEN` authenticates as *neither* application, and
the run would fail with a message about the wrong thing. The refusal names the
half that is set, the half that is not, and both channels that fix it — and the
pre-flight check returns the same sentence as the resolver (one text, so the two
surfaces cannot drift).

**A named file that cannot be READ does not refuse the run while a complete
inline pair is configured** — the operator configured both, and the substitution
is announced. Same shape as the v2 arm's `fellThroughNote`.

## The resolution lives in ONE place

`cmd/agent/cli_voice_sauc.go`'s `saucCredentialSource` is the single resolution
point; `resolve()` is the single reader. The pre-flight check, the privacy
banner, the `channel →` start line, the chat `/voice` row and the request
assembly all go through it, so no two surfaces can describe different routes.
Inside it, the settings/env rungs come from `config.ResolveAudioSauc` — the
package's ONE reader — rather than re-reading the same variables (Rule 2).

Every message carries an **origin** — a path, `settings
audio_engine_param.sauc.app_id/token`, or `$GOPI_AUDIO_APP_ID/$GOPI_AUDIO_TOKEN`
— and never a value.

## Two defects this run found in its own first cut

Both were caught by RUNNING the surface (Rule 31), not by reading it:

1. **The `inlineFrom` label rendered `os.Getenv(name)` — the credential VALUE
   itself** — into the very message that exists to avoid printing it. It now
   renders the variable's NAME (`"$" + name`). The unit tests would not have
   caught it; the first real run against a private profile did.
2. **The half-stated refusal named the WRONG half** ("`token` is set while
   `app_id` is not" when it was `app_id` that was set). The missing/stated pair
   is now derived from which side is empty, and the negative arm in
   `~/.gopi/sessions/01a13626-…/evidence/arm3-half.out` pins the corrected text.

A third was caught by the doc gate: the settings-permission sentence *inside*
the fallback note read "…in gopi's own settings (settings
audio_engine_param.sauc.app_id/token.app_id / .token)", because the label
helpers interleaved. The note now takes a pre-rendered label.

## Measured — the five arms

All five ran the real binary (`bin/gopi`, `CGO_ENABLED=0`), with a private
`HOME`/`GOPI_SETTINGS`/`GOPI_HOME` profile, against
`/home/user/workspace/voice-evidence-tmp/probe.wav` (16 kHz mono PCM16, 5.6 s),
and every one reached the vendor. Transcripts are verbatim from the arms' own
output files under `~/.gopi/sessions/01a13626-…/evidence/`.

| # | profile | credential source | exit | observed |
|---|---|---|---|---|
| 1 | `profile` — inline pair in settings, **no vendor directory at all** | `settings audio_engine_param.sauc.app_id/token` | 0 | `开放时间早上9点至下午5点。` (14 chars, logid …89) |
| 2 | `profile-legacy` — no inline pair, a 0644 vendor file | the file's path, **plus the fallback announcement** and the mode note | 0 | `开放。时间。早上9点至下午5点。` (16 chars) |
| 3 | `profile-half` — `app_id` set, `token` not, a COMPLETE legacy file present | refused before any audio moved | 1 | `the sauc credential pair is half-stated: …app_id is set while …token is not` |
| 4 | `profile-none` — nothing anywhere | the path it looked at + the way back to `--engine local` | 1 | `the sauc engine needs a credential (…/.doubao/v2_voice.env): … no such file or directory` |
| 5 | `profile-none` + `$GOPI_AUDIO_APP_ID` / `$GOPI_AUDIO_TOKEN` | `$GOPI_AUDIO_APP_ID/$GOPI_AUDIO_TOKEN` | 0 | `开放。时间。早上9点至下午5点。` |

Arm 1 is the epic's own case: **a host with no `~/.doubao/v2_voice.env`
transcribes**, and the banner names the settings path instead of a file. Arm 5
proves the environment rung — and its label, which is what caught defect 1.

`grep -c -E '<APPID>|<TOKEN>'` over all five stdout+stderr files: **0 hits in
every file**.

## Files

- `internal/config/audio_engine_param.go` — `AudioSaucParams` gains
  `AppID`/`Token`; the accepted-knob table gains the two leaves;
  `ResolveAudioSauc` applies the env rung through the existing `overEnv`.
- `internal/config/settings.go` — the catalog descriptions of
  `$GOPI_AUDIO_APP_ID` / `$GOPI_AUDIO_TOKEN` now name both blocks.
- `cmd/agent/cli_voice_sauc.go` — `saucCredentialSource` (the ONE resolution),
  `halfStated`, `fallbackNote`, `substitutedNote`, `filePathForMode`,
  `voiceSaucCredentialLabel`, `voiceReportSaucCredentialMode`.
- `cmd/agent/cli_voice.go` — the `-h` knob paragraph for the pair.
- `cmd/agent/cli_voice_stream.go` — the chat row now says `credential %s`
  (an origin, not necessarily a file).
- `cmd/agent/chat_voice.go` — the live arm announces the legacy fallback too.
- `CONFIG.md` — two §2c rows + the precedence note and the announcement text.
- `internal/standards/config_doc_flag_gate_test.go` — the §2c row inventory
  73 → 75, with the reason.
- Tests: `cli_voice_sauc_test.go` (four new arms) and
  `internal/config/audio_engine_param_test.go` (env rung, accepted-knob table,
  credential-leaf redaction).
