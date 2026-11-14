<!-- doc-class: historical -->
# Devdoc: re-homing the legacy flat raw-log directory

**Branch:** `fix/rawlog-test-home-leak` · **Commits:** `0094ec2e` (+ the sweep's
evidence-strength rework) · **Script:** `scripts/rehome-flat-raw-logs.sh` ·
**Test:** `internal/testutil/rehome_flat_raw_logs_test.go`

## The problem this closes

`~/.gopi/raw/` is the RETIRED flat raw-log area (commit 2464f9f3 fixed the
writing side: a bare session's raw log and JSONL now share one directory). What
was left behind is dirty: **1432 files / 27 GB** of `gopi-raw-<stamp>.log` that
an operator cannot attribute to a task, session or process.

The epic's rule is *"if you cannot figure out which process/task it belongs to,
delete it — it is useless to anyone"*. The operative words are **figure out**:
deletion must be justified by evidence, so the sweep accepts a session record
that NAMES the file and rejects everything else.

## Two strengths of evidence — and why the first attempt was wrong

The first version treated "any mention of the name in the sessions tree" as a
reference (equal weight). Measured on the live tree, that rule over-attributes
badly, because **the most common mention is an `ls` of the raw directory pasted
into a transcript** — one such listing names EVERY file in the directory:

```
$ ls -la ~/.gopi/raw/
-rw-r--r-- … 4673410 Aug  6 14:30 gopi-raw-20260806-142047.log
…
```

The result was 794 files "attributable" and 501 of them "AMBIGUOUS (2..7
references)" — a bucket that says nothing about ownership, only that the
directory was listed several times.

The sweep now separates the two:

| Evidence | Shape | Meaning |
| --- | --- | --- |
| **LOCATED** | the record quotes `<flat dir>/<name>` (session_start's `rawLogPath`, a resume note) | ownership |
| **WEAK** | the bare `<name>` appears with no location | a mention (an `ls`, a report listing) |

Measured with that split: **228 names carry location evidence** where **794**
were merely mentioned — the 566-file gap is exactly the `ls` noise.

## Four buckets; only ONE may ever be deleted

| Bucket | Rule | Action |
| --- | --- | --- |
| located, exactly 1 owner | ownership, unambiguous | **moved** into `sessions/<id>/` |
| located, >1 owner | contested | left in place (a wrong owner is worse than a stale file: it puts a stranger's evidence inside a task dir) |
| weak mention only | someone *can* name it | **left in place** — the operator's judgement, printed |
| invisible (no mention anywhere) | `"no one can figure out"` | reported; **deleted only under `--delete`** |

Dry-run is the default; `--delete` removes only the invisible bucket, after
printing its count and byte total.

## Cost of the scan (measured, and why the shape matters)

The sessions tree holds **14.6 GB of `*.raw.log`** (one file alone is 890 MB)
against **563 MB of records** (1733 `*.jsonl` + 55k metadata `*.json`). Two
consequences the script now encodes:

1. **Raw logs are never scanned.** A raw log quoting its own name is a
   self-reference that proves nothing, and including `*.log` in the search
   turned a scan into a many-minute read of tens of GB. The reference pass is
   `rg -l -g '*.jsonl' -g '*.json' -g '*.txt'`.
2. **No backtracking regex over a single-line JSON file.** The first
   implementation extracted names with an `awk` `match()` loop over a
   `/path*gopi-raw-…/` pattern — on a 700 KB single-line session record under
   host load 21 that ran for minutes without finishing. The extraction is now
   linear `grep -oE` (two fixed passes: one path-anchored, one bare-name), with
   a `grep -qF` pre-filter so the path-anchored pass only touches records that
   actually contain the flat dir string.

## Rule adherence

- **Rule 1b/13** — the legacy name shape is declared once (`NAME_RE`) and every
  mention of the wire shape comes from it.
- **Rule 15** — nothing is silently dropped: every bucket is printed with its
  count and byte total, `mv`/`rm` failures are reported (`MOVE FAILED`,
  `DELETE FAILED`), and the dry run says in words that nothing was removed.
- **Rule 10.2 (minimal sample)** — the whole contract is pinned by a synthetic
  four-bucket fixture (`internal/testutil/rehome_flat_raw_logs_test.go`), driven
  through the script's `GOPI_SESSIONS_ROOT` / `GOPI_FLAT_RAW_DIR` seams, so the
  rules are tested WITHOUT touching the live profile.

## Moving a raw log breaks its RECORDED path — fixed here

`session_start` records `rawLogPath` as an **absolute** path pointing into the
flat directory. Re-homing a log therefore dangles that record: the file is
sitting in the session directory, but `gopi raw`, `gopi stats` and
`gopi task show` resolve the recorded path, find nothing, and report
*"no raw logs found"* — a moved file that the tooling can no longer see is a
regression, not a cleanup.

`gopilog.SiblingRawLog(sessionPath, recordedName)` resolves the basename
**beside the session record** (and on the recorded path itself, if it still
exists), and is wired into all three resolvers:

- `gopilog.ResolveRawLogForSession` (`gopi raw`, `gopi stats`)
- `cmd/agent/cli_resume.go` (the resume resolver)
- `internal/tools.taskRawLogPathFromSession` (`gopi task show`)

Pinned by `TestResolveRawLogForSessionFollowsARehomedRawLog`, with a negative
probe: a *different* session directory holding a file of the same basename is
never offered — the name resolves relative to its OWN session only. Red-proof
captured then restored.

**Live E2E (real binary):** after re-homing a sandbox session's log, the
pre-fix binary exits 1 with *"no raw logs found"* while the fixed binary prints
the log (rc=0); `gopi stats` reports the moved file as the source.

## Live dry-run result (the operator's decision input)

Full sweep of the live flat directory — **1432 files / 27 GB, 1013 s**:

| Bucket | Files | Bytes | Disposition |
| --- | --- | --- | --- |
| located, exactly 1 owner | 417 | 9.08 GB | safe to move into that session dir |
| located, >1 owner (contested) | 288 | 15.45 GB | left — ownership is genuinely ambiguous |
| weak mention only | 135 | 2.02 GB | left — an operator can still name it |
| invisible (no mention anywhere) | 586 | 1.40 GB | deletable under `--delete` |

**Nothing was moved or deleted.** The destructive step is the operator's call —
it is irreversible, and the epic's rule leaves the "invisible" judgement to a
human.

## How to run

```bash
scripts/rehome-flat-raw-logs.sh              # report only (default)
scripts/rehome-flat-raw-logs.sh --move       # move the located bucket
scripts/rehome-flat-raw-logs.sh --delete     # move + delete the invisible bucket
```

Test seams (keep the tests off the live profile): `GOPI_SESSIONS_ROOT`,
`GOPI_FLAT_RAW_DIR`.

## Not done (deliberate)

- The sweep has **not been executed with `--delete`** on the live profile. The
  measured dry-run plan is recorded in the task report; the destructive step is
  the operator's call (it is a one-off, irreversible deletion of up to ~1.4 GB
  of unnameable logs).
- Contest/weak buckets are reported, never resolved.
