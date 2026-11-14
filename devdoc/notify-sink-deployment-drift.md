<!-- doc-class: normative -->

# The deployed notify sink is the repo's bytes, or it says why

> Measured 2026-10-24 on `hosta` (task `01a16942`). Three hosts, one
> `notify.cmd` line, three different realities — and nothing in the system
> noticed any of them.

## The incident, as measured

`settings.json` `notify.cmd` on all three machines reads the same thing:

```
bash /home/user/.gopi/notify/notify.sh
```

The file that line execs is deployed by hand (`scripts/notify/deploy.sh`), and
the three profiles disagreed:

| host | deployed `~/.gopi/notify/notify.sh` | repo `scripts/notify/notify.sh` | verdict |
|---|---|---|---|
| hosta | 23449 B, 2026-10-13 05:04 | 55815 B (HEAD `adb7dc0f`) | **stale** — 9 functions missing |
| peerbox | 5931 B, 2026-10-09 23:52 | 55815 B (its own master) | **stale** — 13 commits behind |
| hp | *does not exist* | 55143 B (its own master) | **absent** — exit 127 |

The nine functions the local copy lacked were not decoration:
`refuse_checkout_home`, `peer_health_write`, `deliver_batch`,
`deadline_expired`, `peer_drain_batches`, `sweep_stale_spools`,
`cursor_age_sec`, `seconds_left`, `positive_int_or_default`. Each had been
landed, reviewed and merged in the repository; **none of them was running on the
machine they were written for.** The peer-pull path was the visible half: the
deployed copy wrote `peer-<alias>-last-seen.txt` and nothing else, while the repo
copy additionally writes `peer-<alias>-health.txt` — the record that says
`state=ok|stalled|unreachable` — and bounds each run with a deadline.

`hp` is the extreme case and the reason an absent file is not a "skip": bash
exits `127` before the script body, so the sink's own self-reporting machinery
(`sink_ready`, `NO CHAT RECEIVED THIS EVENT`) cannot run at all. Every lifecycle
event on that machine was silently undelivered.

## Why nothing caught it

`deploy.sh` is a **one-shot an operator runs by hand** — deliberately so
(Rule 35: no cron, no poller, no watcher). Its own header says what it does not
own: *"this script does not own the profile"*. The consequence is that "the
deploy was never re-run" is indistinguishable, from inside the system, from "the
deploy succeeded and nothing has changed". That is Rule 15's silence, and the
repo already refuses it one artifact family over: `TestWebFetchContract_
DeployedScriptSync` compares `~/.gopi/plugins/websearch/scripts/*.py` against the
repo copy and fails loudly. The notify sink had no such check.

## The decision: a suite test, not a new mechanism

**Do it, and make it the same shape the repo already has.** `testsuite/notify_
sink_deployed_sync_test.go` compares the deployed sink against
`scripts/notify/notify.sh` and fails with the drift, both sizes, and the one
command that repairs it.

Why this and not something else, by the Rule 35 four questions:

1. **What breaks without it.** The table above. Nine merged behaviours not
   running; one host losing every event at exit 127. This is not hypothetical —
   it is the state three machines were in on the morning it was measured.
2. **Why it cannot be fixed in-process.** It *is* being fixed in-process: the
   check lives in the suite every change passes through. Nothing new is started,
   scheduled or watched. The failure is a fact about a moment, and the repair
   mechanism (`deploy.sh`) already exists and is idempotent.
3. **Who owns its lifecycle.** Nobody new. `go test ./testsuite/` already runs;
   the test adds no file, no state, no process, and nothing to reclaim.
4. **What the next person sees.** A red test naming both paths, both sizes, the
   sentence "the behaviours landed in the repo are NOT running on this machine",
   and `repair: bash scripts/notify/deploy.sh`. Refuse-by-default and explain
   itself.

The three rejected alternatives, recorded so they are not re-proposed:

- **a cron line / resident poller** — Rule 35's named anti-pattern, and worse
  here: it would *auto-repair* the drift, hiding a deploy nobody ran, which is
  the same "it covers the problem up" shape as the retired HP autostart.
- **a new `gopi` verb** — a knob for a fact the suite already observes on every
  run; and a verb an operator must remember to type has exactly the failure mode
  `deploy.sh` already has.
- **making `deploy.sh` run itself** — the file declares it does not own the
  profile. A machine-level decision stays a machine-level decision.

## The predicate is a pure function, and it is driven

`notifySinkDrift(notifyCmd, deployedPath string, deployed, repo []byte)` returns
one of five verdicts, and the fixture arm drives **all five** so no branch can go
unmeasured on a host that happens to be in a different state:

| verdict | when | what the test does |
|---|---|---|
| `unconfigured` | `notify.cmd` empty | skip, stating the settings path |
| `not-ours` | the command names a different sink | skip, quoting the command |
| `absent` | ours, and the file is not there | **fail** — the exit-127 shape |
| `stale` | both exist, bytes differ | **fail** — names both sizes |
| `current` | byte-identical | pass (logged, not silent) |

Two orderings are load-bearing. **Ownership is decided before existence**: a host
that points `notify.cmd` at its own script must not be accused of a missing file
it never wanted. And **one trailing newline is stale**: a byte compare that
tolerates whitespace would pass the exact drift it was built for.

The real-corpus arm resolves the profile from the **OS user record**
(`os.UserHomeDir()` + `config.ProfileDirName`), never from `GOPI_HOME` /
`GOPI_SETTINGS` — the suite's own hermeticity pin (`internal/testutil/suiteenv.go`)
points those at a scratch path that deliberately does not exist, so reading them
would make the test skip on every run: a green that measured nothing.

## Red proof

Both failure arms were driven with a fixture `HOME` and the real test binary:

```
# STALE — this machine's own pre-deploy bytes (23449 B)
HOME=<fixture> go test ./testsuite/ -run 'TestNotifySinkDeployedSync$' -count=1
  verdict=stale repo_bytes=55815 deployed_bytes=23449     --- FAIL

# ABSENT — HP's measured shape
HOME=<fixture> go test ./testsuite/ -run 'TestNotifySinkDeployedSync$' -count=1
  verdict=absent deployed_bytes=0
  "every lifecycle event is lost before the script body runs"   --- FAIL

# CURRENT — the real profile, after the reinstall
go test ./testsuite/ -run 'TestNotifySink' -count=1
  verdict=current repo_bytes=55815 deployed_bytes=55815    --- PASS
```

A trap worth recording: overriding `HOME` for a `go test` invocation also moves
the default `GOCACHE`, which makes the run die with `[setup failed]` before any
test executes. The arms above pin `GOCACHE` / `GOPATH` / `GOMODCACHE` explicitly.

## What this test does NOT do

It writes nothing, copies nothing, deploys nothing. A checker that repaired its
own finding would be a second deploy path beside `deploy.sh`, and the two would
drift exactly as the sink did. The verdict is a sentence; the repair is the
operator's command.

It also does not cover the **other** deployed artifact family that is currently
drifting on this host — `~/.gopi/plugins/websearch/scripts/bing_search.py` (17770
B, 2026-09-12) against `plugins/websearch/scripts/bing_search.py` (18494 B,
379 lines). That drift is real and it is why `go test ./testsuite/` is red here
today; it is filed as its own row rather than folded into this one, because the
check for it already exists and the defect is the deployed copy, not a missing
gate.
