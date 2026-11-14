<!-- doc-class: normative -->

# Installing gopi

**Read this page before you run anything.** It takes about five minutes, and it is the
difference between "it works" and "it failed once and I gave up".

---

## 0. What you need, exactly

gopi is **built from source** today. There is no `curl … | sh`, no prebuilt binary, and
no Docker image. Four things are required and nothing else:

| | requirement | why |
|---|---|---|
| **OS** | **Ubuntu 22.04 LTS, x86-64** | the only platform this code has been run on. See §6 for the honest list of what has never been tested. |
| **Go** | **1.24.0 or newer** | `go.mod` declares `go 1.24.0`. An older toolchain refuses to build; there is no fallback. |
| **git** | any recent version | only to clone. |
| **An LLM API key** | DeepSeek | see §3. Without one, gopi **starts** and its non-model commands work, but nothing can talk to a model. |

Nothing else. No Node, no Python, no C compiler, no CGO, no package manager step.
The `require` block of `go.mod` is the complete dependency list, and it is all Go modules
compiled into one static binary.

### Check your Go version first

```bash
go version          # must print go1.24.0 or higher
```

If it prints something older, install the toolchain from <https://go.dev/dl/>. Do not try
to work around it — a `go` line in `go.mod` newer than your toolchain is a hard build error.

---

## 1. Build it

```bash
git clone https://github.com/nickhuang99/gopi.git
cd gopi
make build          # → ./bin/gopi
./bin/gopi --version
```

You should see the version, the Go version it was built with, the platform, and the
**revision** — the git commit the binary came from. That last line is how you prove the
binary is the code you just cloned:

```bash
./bin/gopi --version | grep revision
git rev-parse HEAD
```

The two must match. If they do not, you are running a stale binary (this happens when a
build fails and an older `bin/gopi` is still sitting there).

### Put it on your PATH (optional)

```bash
make install                       # → $(go env GOPATH)/bin/gopi
make install PREFIX=/usr/local/bin # or somewhere already on PATH
```

---

## 2. First run, with no key yet

gopi is designed to start and answer for itself even before you have a key:

```bash
./bin/gopi config setup     # idempotent: fills in what is missing, changes nothing that exists
./bin/gopi config show      # what is actually in force, and WHICH channel decided each value
```

`gopi config show` is the single most useful command on this page. It does not print the
file's contents — it prints the **parsed result**, and beside each value it names the
channel that won (`--flag`, an env var, `settings.json`, or a built-in default). When
something behaves differently from what you wrote in the file, run this first.

---

## 3. Give it an API key (bring your own key)

**gopi ships no credentials, and it never will.** Every key is yours.

### DeepSeek — the only model family this release is tested against

```bash
mkdir -p ~/.gopi
cp settings.example.json ~/.gopi/settings.json
$EDITOR ~/.gopi/settings.json     # fill in "api_key"
```

Or skip the file and use an environment variable:

```bash
export ANTHROPIC_AUTH_TOKEN='sk-your-own-deepseek-key'
export ANTHROPIC_BASE_URL='https://api.deepseek.com/anthropic'
```

The three knobs that matter:

| setting.json key | env var | default |
|---|---|---|
| `api_key` | `ANTHROPIC_AUTH_TOKEN` (fallback `DEEPSEEK_API_KEY`) | *(none — required for any model call)* |
| `base_url` | `ANTHROPIC_BASE_URL` | `https://api.deepseek.com` |
| `model` | `ANTHROPIC_MODEL` | `deepseek-v4-pro` |

Get a key at <https://platform.deepseek.com/>. **gopi is a bring-your-own-key tool: an
account is yours, the spend is yours, and nothing is proxied through us.**

`settings.example.json` in this repo is a **template with placeholder text**. It contains
no real key. Copy it, then replace the placeholder.

**Precedence: flag > environment variable > `settings.json` > built-in default.** All of
it, for every knob, is in [`CONFIG.md`](CONFIG.md); `gopi --help-env` prints the same
catalog from the binary.

### Voice input — a second, separate account (optional)

Speech-to-text is **not** DeepSeek. It runs against **Volcengine (ByteDance) SAUC**, which
is a separate service with a separate account, a separate app id and a separate access
token — billed separately. If you do not want voice input, **leave it out entirely**; gopi
then reports the voice path as unconfigured instead of failing at startup.

```jsonc
"audio_engine": "sauc",
"audio_engine_param": {
  "sauc": {
    "endpoint": "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async",
    "resource_id": "volc.seedasr.sauc.duration"
  },
  "streaming": {
    "app_id": "your-volcengine-app-id",
    "token":  "your-volcengine-access-token"
  }
}
```

---

## 4. Make it work

```bash
gopi chat                    # interactive
gopi chat --once -p "列出这个目录里最大的三个文件"    # one shot, no REPL
gopi task submit "把 X 改成 Y"   # queue a unit of work instead of chatting
gopi task list                # what is queued
```

If `gopi chat` starts but every turn fails, the cause is almost always one of:

| symptom | cause | fix |
|---|---|---|
| `401` / auth error on the first turn | key missing, wrong, or expired | `gopi config show | grep api_key` — it prints `<set: N chars>` or `(empty)`, never the key |
| connection error / timeout | `base_url` wrong, or no route to api.deepseek.com | `curl -sS $ANTHROPIC_BASE_URL/models` by hand |
| `model not found` | `model` names something your account does not have | leave `model` out and take the default |

---

## 5. A disposable, fully local sandbox (no global state touched)

This is how you test gopi without letting it write anywhere near your real profile.
**It needs no code changes — ONE environment variable does it:**

```bash
SB="$HOME/gopi-sandbox"       # NOT /tmp — see the note below
mkdir -p "$SB/.gopi"

# A private profile: sessions, the ledger, logs, scratch AND the settings
# document all land under $SB. This one variable is sufficient.
export GOPI_HOME="$SB"
# Optional, and only when you want the settings file somewhere OTHER than
# $GOPI_HOME/.gopi/settings.json (an explicit value wins over that default):
# export GOPI_SETTINGS="$SB/.gopi/settings.json"
```

**Do not put the sandbox on `/tmp`** — on most installs `/tmp` is a RAM-backed filesystem
sized a fraction of the disk, and gopi's ledger plus its build cache grow into hundreds of
megabytes. `$HOME` is the safe default. Any path works as long as it is a real directory on
a partition with room; gopi checks the free space and says so rather than filling it.

**`GOPI_HOME` alone IS enough — measured on a fresh clone, three ways.** Set both if you
want to be explicit, but do not expect `GOPI_HOME` to leave the global settings in force:

```bash
$ cd <fresh clone>
$ env -u GOPI_HOME -u GOPI_SETTINGS ./bin/gopi config show | grep 'settings file'
  settings file          /home/you/.gopi/settings.json  (1231 bytes)          ← global
$ GOPI_HOME=/tmp/sb ./bin/gopi config show | grep 'settings file'
  settings file          /tmp/sb/.gopi/settings.json  (absent — built-in defaults in force)
$ GOPI_HOME=/tmp/sb GOPI_SETTINGS=/tmp/sb/.gopi/settings.json ./bin/gopi config show | grep 'settings file'
  settings file          /tmp/sb/.gopi/settings.json  (3 bytes)
```

That middle line is the case where `$SB/.gopi/settings.json` does not exist yet — gopi says
`(absent — built-in defaults in force)` rather than falling back to your global file. Once
the file is there (the third command, or after `gopi config setup` created one) the same
command reports its byte count instead. **Neither line ever names `~/.gopi/settings.json`.**

`~/.gopi/settings.json` (a symlink on the author's host) is **not** read when `GOPI_HOME`
is set: `config.ResolveSettingsPath` falls through to `DefaultSettingsPath()`, which is
`config.GopiDir() + "/settings.json"`, and `GopiDir()` returns `$GOPI_HOME/.gopi` before
it ever consults `os.UserHomeDir()`. An earlier draft of this page claimed otherwise and
was wrong; the three commands above are the correction.

Two more facts worth knowing before you build a test harness on this:

- **The caller's `TMPDIR` beats the `tmpdir` key** in your settings file. gopi prints which
  channel decided it (`from the caller's TMPDIR`), so read that line rather than assuming.
- **Nothing is deleted for you.** The sandbox is an ordinary directory; `rm -rf` it when you
  are done, or use `gopi tool sandbox create` which produces the same shape at a
  configured, reclaimable location (`gopi tool sandbox show` prints where).

To confirm the isolation actually holds — **this is the measurement, not a description of
it** (run on a fresh clone 2026-11-15; the real ledger was 691642368 bytes before AND after,
mtime 02:58:48 both times):

```bash
ls -l ~/.gopi/memory.db              # BEFORE: 691642368 bytes, mtime 02:58:48
GOPI_HOME="$SB" gopi task list       # exit 0, prints "No tasks found."
ls -l "$SB/.gopi/memory.db"          # CREATED: 131072 bytes (a fresh, empty ledger)
ls -l ~/.gopi/memory.db              # AFTER:  691642368 bytes, mtime 02:58:48 — UNCHANGED
```

---

## 6. What has NOT been tested (read this before filing a bug)

Honesty is cheaper than a retraction later:

- **No macOS, no Windows, no ARM, no other Linux distribution.** Only Ubuntu 22.04 x86-64
  has actually been run. The code is pure Go and should build elsewhere; "should" is not
  "has been".
- **No Go version other than 1.24.x** has been exercised.
- **One LLM provider family**: DeepSeek's Anthropic-compatible endpoint. gopi speaks the
  Anthropic wire protocol, so another compatible endpoint *may* work — but only DeepSeek has
  been run, and any other choice is untested.
- **No hosted install path.** You build from source or you do not run it.

If you hit a failure on an untested platform, that is a genuine gap and worth an issue —
please include the output of `gopi --version` and `gopi config show`.

---

## 7. Where to go next

| | |
|---|---|
| What gopi is, and why it is shaped this way | [`README.md`](README.md) |
| Every CLI verb, by name | [`COMMANDS.md`](COMMANDS.md) |
| Every configuration key, its default and who reads it | [`CONFIG.md`](CONFIG.md) |
| The engineering rules the code obeys | [`PROJECT.md`](PROJECT.md) |
| The measurements behind design decisions | [`devdoc/`](devdoc/) |
