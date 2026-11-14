<!-- doc-class: historical -->
# The task-event sink, deployable: one command, per-host parameters, visible failure

Task `01a131f345ff798a8b540b2c9810ddeb`. Scope: **`scripts/notify/` only** — no Go,
no `internal/`, no new gopi knob, no resident/timed/self-scheduling dependency
(Rule 35). The sink is still exec'd only by the already-existing `notify.cmd` path.

## 1. What the problem actually was

The peer-pull sink is a ~52 KB shell script. Bringing up the SECOND machine meant
copying it into `~/.gopi/notify/` and hand-editing the copy, because three of its
values were literals *inside* the file:

```
SINK_DIR="${PROFILE}/${SINK_DIR_NAME}"   # where the per-peer cursors live
readonly PEER_LIMIT=2                    # how many peers one run may read
readonly PEER_SSH_TIMEOUT=10 …           # the staleness bound existed nowhere at all
```

Two consequences, both structural rather than cosmetic:

* every host carried its own drifting fork of a 52 KB file, so "what is running on
  hosta" and "what is running on peerbox" could not be compared by bytes;
* a broken pull was **invisible**. `notify.cmd` runs with the payload on stdin and
  its output captured (`internal/tools/tasknotify.go: taskNotifyCmd`); on the
  success path the parent records `cmd ok (stdout: )` and DROPS the text. So every
  `notify.sh: peer … cannot read its event stream` line went nowhere: a pull dead
  for a week printed exactly what a pull with nothing to say printed. That is the
  Rule 15 failure by name — the sink *was* saying something, to nobody.

## 2. The design

### 2a. Parameters are read from the ENVIRONMENT, once, at one site each

```
GOPI_NOTIFY_STATE_DIR          state (cursors + health records). default: <profile>/notify
GOPI_NOTIFY_CURSOR_STALE_SEC   when a cursor counts as stalled.  default: 21600 (6h)
GOPI_NOTIFY_PEER_LIMIT         peers read per run.               default: the sink's PEER_LIMIT
```

**These are not gopi configuration knobs** and that is deliberate, not an
oversight: nothing in Go reads them, so `CONFIG.md` is unchanged and the
config-doc gate is untouched. They belong to the operator's shell line. An unset
variable is the documented default, so a copy of the file nobody deployed behaves
byte-for-byte as it did before this change — the two existing rigs
(`notify_peer_resume_test.sh`, `notify_slot_contract_test.sh`) still pass with no
edits, which is the evidence for that claim.

The peers themselves were deliberately NOT parameterised: the one whitelist is
already `settings.json`'s `sync_peers` (`SYNC_PEERS_KEY`), and a second source of
truth for "who do I pull from" is precisely the drift this change removes.

A non-numeric or zero value is **reported and replaced by the default**
(`positive_int_or_default`), never silently obeyed and never silently ignored: a
typo in a bound must not disable the bound.

### 2b. The failure record — a file, because stderr is swallowed

One file per peer, `${STATE_DIR}/peer-<alias>-health.txt`, two lines:

```
state=unreachable peer=peerbox cursor=2026-10-16T09:00:00+08:00 reason=ssh exit 255: Could not resolve hostname peerbox
first_seen=2026-10-17T00:12:03Z last_checked=2026-10-17T00:12:03Z(last_checked_epoch=1760…)
```

The five states:

| state | meaning |
|---|---|
| `ok` | the peer was reached and its cursor is fresh |
| `stalled` | the peer was reached but the cursor has not moved past the threshold — the failure that looks like silence |
| `unreachable` | the ssh leg failed (exit status in the reason) |
| `not-read` | the run never got to this peer (over the per-run limit, or the run budget was spent) |
| `refused-cursor` | the cursor failed the whitelist, so the peer was skipped |

Three properties make this a record and not a log:

1. **Dedupe is the first line.** Line 1 carries only what a reader compares — state,
   peer, the cursor it judged, the reason — and it is rewritten ONLY when that key
   changes. A peer broken for a week leaves ONE record saying so.
2. **The volatile part of the reason is normalised.** `ssh` quotes the address and
   the port in its own wording, and a port/millisecond detail that varied per run
   would change the key and defeat the dedupe: the reason is `ssh exit <status>:
   <text with "ssh: " and decimal details dropped>`. The exit status is the stable
   classifier (255 connect/auth, 3 no readable anchor, 124 our own `timeout`).
3. **Line 2 refreshes at most hourly** (`PEER_HEALTH_REFRESH_SEC`) so a reader can
   tell "still broken now" from "broken once, months ago" without the file being
   rewritten on every task event. The write is `write sibling + mv` — a reader sees
   the old record or the new one, never half of either.

One stderr line rides along, on the transition only, so a live operator sees it too.

### 2c. `deploy.sh` — the one-shot, idempotent install

```
bash scripts/notify/deploy.sh --peer peerbox [--profile DIR] [--state-dir DIR]
                              [--cursor-stale SEC] [--peer-limit N]
                              [--source PATH] [--dry-run] [--print-cmd]
```

What is written, and why each step is safe to repeat:

* **`<profile>/notify/notify.sh`** — copied **byte-for-byte** from the repo (or
  `--source`), through a sibling and `mv`, mode 0755. A file already identical is
  left alone, mtime included. This is what makes machine 2 and 3 cost the same as
  machine 1: *the deployed artifact is the repo's artifact*, so an upgrade is
  `git pull` + re-run, and a comparison across hosts is `cmp`.
* **`settings.json` `sync_peers`** — each `--peer` appended once, never duplicated.
* **`settings.json` `notify.cmd`** — filled in when EMPTY, left alone when it
  already equals the wanted line, and **never overwritten when it names something
  else**: the wanted line is printed instead (`cmd CONFLICT (left alone)` +
  `cmd needed (…put this in settings.json yourself)`). The profile is the
  operator's; this command does not own it.
* The whole settings edit is ONE python3 pass that writes only `if changed`, so an
  idempotent run does not even bump the mtime of somebody else's settings file.

**Validation before any write**: a peer alias against `^[A-Za-z0-9_][A-Za-z0-9._@-]*$`,
a directory against "absolute, and no newline/quote/backtick/`$`/`;`/`&`/`|`/space/…".
A refused value stops the run by name with nothing half-written. This matters
because the same directory string ends up inside `notify.cmd`, which the parent
executes as `sh -c`.

**A settings file that cannot be written is reported, not skipped**: a SYMLINK is
refused by name (a sandbox's settings is a link to the real one, and POSIX
`rename(2)` would replace the link node and silently detach the profile — Rule 34),
the sink is still installed, the exact line to add by hand is printed, and the exit
status is non-zero because the deployment is incomplete.

## 3. Two findings from the real run (Rule 41 evidence)

**3a. `GOPI_HOME` leaks a worktree into a child.** Measured: running the installer
from inside this task's own run, `$GOPI_HOME` was this task's **git worktree**, so
`<worktree>/.gopi/notify/` was created and `gopi notify` was handed a throwaway
profile whose slot registry is empty — the delivery would be lost with no error
anywhere, and the worktree left dirty (Rule 30). The sink now refuses such a
profile **by name** and writes nothing into it (`-e $GOPI_HOME/.git ||
-e $PROFILE/.git`). The predicate is the `.git` entry and NOT a permission test:
the operator's own profile was measured at mode **775** on this host, so a
`0700`-style check would have refused the very profile the sink exists to serve.

**3b. The documented "sandbox profile" shape is not what the sandbox carries.**
`gopi tool sandbox create` yields `<sb>/.gopi/settings.json` as a **regular file
mode 444** (not the documented read-only *symlink* to the real settings, which is
what `~/.gopi/settings.json` itself is). Filed per Rule 41; not fixed here (it is
sandbox machinery, `internal/`, and out of this task's scope).

## 4. Verification

Rigs (all three green; run them with `bash scripts/notify/<name>`):

* `notify_peer_resume_test.sh` — unchanged, still 4/4 (the batch/cursor contract).
* `notify_slot_contract_test.sh` — unchanged, still 5/5 (the slot/delivery contract).
* `notify_peer_health_test.sh` — **new**, 21 assertions: a failing peer leaves ONE
  record carrying alias + instant + reason; a repeat does not touch it, does not
  reprint, and does not grow it; a fresh cursor reads `ok`, an idle one `stalled`;
  the state directory is a parameter (and the cursor follows it); the payload is
  still anchored; a worktree `GOPI_HOME` is refused by name.
* `notify_deploy_test.sh` — **new**, 43 assertions: install is byte-identical and
  executable, a second run writes nothing (bytes AND mtime), a repeated `--peer`
  lands once, a foreign `notify.cmd` is never overwritten, `--print-cmd`/`--dry-run`
  write nothing, nine refusal shapes leave the profile untouched, an unwritable
  (symlinked) settings file is reported with the line to add, and the installed
  `notify.cmd` **runs** (`sh -c`, payload on stdin) against an isolated profile.

Real invocation evidence (sandbox profile, not `-h`): see
`$GOPI_TASK_REPORT_PATH` §Verification and the session's
`artifacts/deploy-evidence.txt`.

## 5. What this change deliberately does NOT do

* **No `CONFIG.md` row.** The three names are not Go-read; the config-doc gate
  requires every `GOPI_*` name in that document to exist as a production Go
  literal, and adding one would make the document claim a knob nothing in gopi
  consumes.
* **No scheduler, no watcher, no reaper.** Nothing ticks. The health record is
  written by the pull that already runs (and is not running when nothing notifies).
* **The direction is untouched.** The sink still only *pulls* (`ssh … <peer> sh -s`),
  one alias at a time from `sync_peers`, with the cursor whitelisted before it
  reaches the far side's parser. Nothing here lets a peer open a session on this
  host.
