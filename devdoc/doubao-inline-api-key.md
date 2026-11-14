<!-- doc-class: historical -->
# The doubao key may live inline in settings.json — `doubao_api_key`

**Task:** `01a116ab-8654-76ad-b74a-670e3195463a` · branch `task/01a116ab865476adb74a670e3195463a`

## What was wrong

`gopi tool voice_input --engine doubao` accepted its Volcengine credential from
exactly one place: a **file**, `~/.doubao/apikey.txt` by default. Every other
knob the engine needs — `resource_id`, `hotwords` — was a normal settings value,
and the operator's credential for the *sibling* model on the same endpoint
(`api_key`, the DeepSeek key `vision_analyze` reuses) already lived in
`~/.gopi/settings.json`. So the one secret with the worst ergonomics was the one
that had to be maintained in a vendor's own directory.

The request was narrow: let the key be **inline in settings.json**, alongside
`vision_model` / `vision_base_url` / `api_key`, reusing the existing layering —
without removing any of the three existing knobs and without a security
downgrade.

## The finding that shaped the implementation

Two measurements, taken on this host before any code was written:

```
$ ls -l ~/.gopi/settings.json
lrwxrwxrwx 1 nick nick 23 Sep 10 12:22 /home/user/.gopi/settings.json -> .ds_flash_settings.json
$ stat -L -c '%a %n' ~/.gopi/settings.json
664 /home/user/.gopi/settings.json
$ ls -l ~/.doubao/apikey.txt
-rw-rw-r-- 1 nick nick 37 Sep 13 15:00 /home/user/.doubao/apikey.txt
```

1. **The live settings document is world-readable (0664) and it already carries
   `api_key`.** Adding a second credential to it without touching the mode would
   have been a *silent security downgrade dressed as a configuration
   improvement* — the legacy `apikey.txt` is 0664 too, so the move would have
   bought ergonomics and no protection at all.
2. **It is a symlink.** `chmod` on the link path does not reach the file the
   bytes live in, so a naive guard would have "protected" the link and left the
   secret open. This is the same constraint the write path already knows about
   (`settings_write_guard.go`: POSIX `rename(2)` replaces the *link node*).

Both facts turned the feature into two halves: the *rung* (precedence) and the
*document* (a credential file must be private, and the report of a permission
change must not quote the secret).

## The precedence, spelled once

```
--doubao-key-file  >  $GOPI_DOUBAO_KEY_FILE  >  settings "doubao_key_file"
                   >  $GOPI_DOUBAO_API_KEY    >  settings "doubao_api_key"
                   >  ~/.doubao/apikey.txt (legacy fallback)
```

One resolver: `cmd/agent/cli_voice_engine.go`'s `voiceDoubaoKeySource` (Rule 2 —
exactly one place reads the knobs). The request assembly, the pre-flight
dependency check and every operator-facing message ask *it* rather than
re-reading the environment, so "a key is available" cannot mean one thing to the
check and another to the request.

**The asymmetry is a decision, not an accident**: a key **FILE** named on an
explicit channel outranks the inline value. A stored path is a deliberate
statement about *where* the credential lives ("on this host's encrypted volume,
not in a file gopi reads for everything else"); overwriting it with whatever
happens to sit in `settings.json` would be gopi second-guessing the operator.
The inline key is what fills the gap when nobody named a file.

## Deliberate design choices worth recording

**No CLI flag for the key — a refusal, not an omission.** A credential on a
command line lands in the process table, readable by every user on the host.

**`GOPI_DOUBAO_API_KEY` is read but never republished.** This is the one place
this change departs from the package's own convention. Every other settings knob
is pushed into gopi's environment by `config.ApplyEnv` so its children inherit
it — and that environment is inherited by the bash tool, by every child process,
by the task executor. Republishing the credential would therefore create a
*second, wider* copy of it than the 0600 file it lives in. So the env name is
read when an operator exports it themselves (a catalog row the code ignores
would be the silent failure Rule 15 bans) and the catalog's own help sentence
says the value is not republished — one const,
`DoubaoAPIKeyNotPublishedClaim`, referenced by the catalog row and asserted by
a test, so the rendered help and the doc cannot drift into two different
promises about a secret.

**The credential key list lives in exactly one place.**
`config.CredentialSettingsKeys` (Rule 14) — because a second, hand-typed list of
"the secret keys" is how a key silently stops being redacted. `gopi config show`
asks it; `IsCredentialSettingsKey` decides. The list deliberately does **not**
pattern-match on `*_key`: `doubao_key_file` ends in `_key` and holds a *path*,
and a rule that redacted a path would teach readers that the list is noise.

**A document with no secret is left completely alone.** The permission guard fires
only when a credential *value* is actually present. Reading the operator's
profile is not a licence to reformat it, and a guard that chmodded on every run
would be switched off by the first person it inconvenienced. An empty or absent
`doubao_api_key` therefore means "unset" — the same reading every other optional
string knob takes (Rule 15a) — and `"doubao_api_key": ""` must not shadow the
file rungs a working deployment relies on.

## Two real defects the tests found (not written by hand — measured)

Both were found by driving the real renderer, and both are fixed at the shared
predicate rather than at the call site:

1. `gopi config show` rendered an **empty** credential as `<set: 2 chars>` — the
   raw JSON cell `""` is two bytes, so an unset key looked exactly like a set
   one. The presence question now has one owner (`credentialValuePresent`), which
   the guard and the renderer both ask.
2. The rendered length **counted the JSON quotes**: a 42-character key reported
   as `44 chars`. An operator uses that number to check the right value is in
   place, so the length is now the *decoded* string's.

## Files

| file | what changed |
|---|---|
| `internal/config/settings.go` | `DoubaoAPIKeySettingsKey`, `DoubaoAPIKeyEnv`, `DoubaoAPIKeyNotPublishedClaim`, `DoubaoAPIKeyPlaceholder`; two catalog rows; the stale "the key VALUE is never a settings field" comment corrected |
| `internal/config/config.go` | `Config.DoubaoAPIKey` with explicit Rule 15a semantics |
| `internal/config/credential_settings.go` | NEW — the credential key set, present-detection, symlink-aware mode reading, tightening, the note, `RedactedCredentialShape` |
| `internal/config/env.go` | *(unchanged on purpose)* — the inline key is **not** in `ApplyEnv`; the reason is the const's doc |
| `cmd/agent/cli_voice_engine.go` | `doubaoKeySource` (the one resolver), `load`/`missing`/`hint`, the guard call, `voiceReportKeyMode` |
| `cmd/agent/cli_config.go` | `config show` redacts every key in `CredentialSettingsKeys`, through `internal/config`'s shape helper |
| `cmd/agent/cli_voice.go` | the two usage texts state the new rung and the file rung's precedence |
| `CONFIG.md` §2c | the `doubao_api_key` row, the sibling row's missing `--doubao-key-file` face corrected, and a prose block stating the channel/flag/permission rules |
| `internal/standards/config_doc_flag_gate_test.go` | `configDocFlagRowCount` 62 → 63 with the audit-trail entry |

## Tests

- `internal/config/credential_settings_test.go` — the key set and its near-miss,
  the modelled-key guard, empty/absent/null values keeping the document valid,
  present-detection including its "I could not tell" arm, tightening that
  narrows only and reports, and **the symlink arm** (the target is what gets
  0600, and the link is still a link).
- `internal/config/credential_catalog_test.go` — the catalog row exists, backs the
  settings key, states the not-republished claim, and has no flag face.
- `cmd/agent/cli_voice_doubao_key_test.go` — inline key used; inline key satisfies
  the pre-flight check; an explicit file outranks it on all three channels; the
  env channel of the inline value is honoured and outranks settings; an empty
  inline value falls back to the file; the mode is tightened and announced; a
  credential-free document is untouched; **the sentinel never appears in any
  message the change can produce**; the winning rung is named.
- `cmd/agent/cli_config_credential_redact_test.go` — `gopi config show` over a
  document with *both* credential keys: no value survives, both keys are named,
  the shape and length are reported, an ordinary key is *not* redacted, and an
  unset credential is distinguishable from a redacted one.

## What this change does NOT do

- It does not migrate the operator's live `~/.gopi/settings.json`. The file is
  the operator's; the read path tightens its mode and says so, and putting the
  key *into* it is one edit they make (or a `GOPI_SETTINGS`-pointed run makes for
  them). Doing it for them from a task run would be an unannounced write to the
  profile they use every day.
- It does not add a `--doubao-api-key` flag, a keyring, a new file format or any
  other persistent artifact (Rule 35: zero new dependencies — one settings key,
  resolved by code that already runs).
