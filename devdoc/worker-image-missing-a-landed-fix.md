<!-- doc-class: normative -->
# A fix is on master and the live worker still acts like it is not — how to prove which side is wrong

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**The question this page answers.** A bug report says "the resident workers still do
X", and `git log origin/master` says X was fixed hours ago. One of two things is
true, and they need opposite remedies:

| what is true | remedy | who |
|---|---|---|
| the fix is NOT on the integration branch | write the fix | a dispatched row |
| the fix IS on the branch, and the **running image** predates it | restart the process | **the operator** — Rule 42: the signal may only be sent by whoever created and holds the process |

Getting these backwards costs a full run in each direction: a row dispatched to
"re-fix" something already fixed, or a live defect kept in service because a
`git grep` on the working tree came back green. Deciding it needs ONE reading, and
the obvious substitutes for that reading are all wrong in at least one direction.

## The criterion: `git grep <symbol> <rev>`, by REVISION, never by path or string

```bash
cd /home/user/work/gopi
git grep -c 'nothingToDeliver' 84beb309ce3c -- '*.go'    # the RUNNING image's rev
git grep -c 'nothingToDeliver' HEAD -- '*.go'            # the tree that will be built
git grep -q 'errNothingToDeliver' 84beb309ce3c -- '*.go'; echo $?
```

Measured 2026-10-29 (row `01a18377-0235-7214-95f9-e1268b5dba36`):

```
nothingToDeliver in 84beb309: 0 file(s)
nothingToDeliver in HEAD:     6 file(s)
errNothingToDeliver in 84beb309 exit=1
errNothingToDeliver in HEAD     exit=0
```

The **running image's revision** is readable for a live pid whose file has been
replaced on disk — the kernel keeps the old inode addressable, and the Go
toolchain stamps every build with its VCS revision:

```bash
for p in 1033859 1035646 1038465 3633443; do
  printf 'pid=%s rev=%s\n' "$p" \
    "$(go version -m /proc/$p/exe 2>/dev/null | awk '/mod\tgithub.com\/the operator\/gopi/{print $3}')"
done
# pid=1033859 rev=v0.0.0-20261028060848-84beb309ce3c
# pid=1035646 rev=v0.0.0-20261028060848-84beb309ce3c
# pid=1038465 rev=v0.0.0-20261028060848-84beb309ce3c
# pid=3633443 rev=v0.0.0-20261028170646-9b028a7ffeee
```

So the pair of facts is `rev` (from `/proc/<pid>/exe`) + `git grep` at that rev.
`(deleted)` is **not** a verdict — it only says the path was rewritten, and a
worker started one minute after the last `make build` carries it while running
the newest code.

## The two substitutes that answer the wrong question

Both were tried on the same day, on the same image, and each failed in a
different direction.

### ① The `(deleted)` marker alone — a FALSE POSITIVE on "this process is stale"

```
$ readlink /proc/3633443/exe
/home/user/work/gopi/bin/gopi (deleted)          # started 01:07:40, image = 9b028a7f
$ git merge-base --is-ancestor 20c15007c 9b028a7ffeee; echo $?
0                                                # it IS an ancestor: the fix is in there
```

`go build -o bin/gopi` writes a new inode and renames it over the path, so the
NEXT build deletes the image every running process is holding. A `(deleted)`
marker therefore means "a build happened after this process started" — nothing
about which code it is.

### ② `strings /proc/<pid>/exe | grep <text>` — wrong in BOTH directions

**False negative (CJK).** The Go linker stores UTF-8 string literals, but GNU
`strings` defaults to `--encoding=7-bit` and emits only runs of printable ASCII,
so a Chinese-only sentence reads as 0 hits on an image that certainly has it:

```bash
$ strings /proc/1035646/exe | grep -c '交付没有内容可发布'      # 84beb309
0          # FALSE NEGATIVE — the sentence is in that image
$ git grep -c '交付没有内容可发布' 84beb309ce3c -- cmd/agent/devworkspace_channel.go
84beb309ce3c:cmd/agent/devworkspace_channel.go:1      # the very next reading: 1 hit, in the SOURCE
```

**The zero is the ENCODING, not the content** — and this is the measurement that
proves it, not a reading of `strings(1)`'s documentation:

```bash
$ strings /proc/1035646/exe | grep -c '交付没有内容可发布'; echo $?
0
1                       # ← grep found nothing
$ strings -e S /proc/1035646/exe | grep -c '交付没有内容可发布'; echo $?
1
0                       # ← the same bytes, read as 8-bit: FOUND
```

`-e S` is what the default `--encoding=7-bit` withholds. So the defect is not
"the linker dropped the string"; it is that a probe silently changed the
question. An image that reads 0 for a UTF-8 literal and 1 under `-e S` is not
stale — it is being asked in the wrong alphabet, and the operator reading the
zero has no way to tell that from a real absence.

**False positive (prefix collision).** A short ASCII phrase matches a DIFFERENT
sentence that happens to contain it. The probe looked for the nothing-to-deliver
path and found one hit on the old image — which was this, a message from a
different file entirely:

```
84beb309:internal/devworkspace/devworkspace.go:417:
    sb.WriteString("clone channel: nothing to deliver from this workspace")
```

HEAD's real exit is spelled elsewhere and composed differently:

```
HEAD:cmd/agent/devworkspace_channel.go:266  var errNothingToDeliver = errors.New("clone channel: nothing to deliver")
HEAD:cmd/agent/devworkspace_channel.go:302  reason := fmt.Sprintf("clone channel: nothing to deliver — %s (after %d delivery attempt(s), "+
HEAD:cmd/agent/devworkspace_channel.go:303      "the workspace %s is RECLAIMED: it held no commit past %s)",
HEAD:cmd/agent/devworkspace_channel.go:304      blk.Report(), s.rounds, s.dir, devworkspace.DefaultBranch)
```

Two images, two spellings, and `strings` counts 1 for the old and 4 for the new —
numbers that look like an answer and are an artefact of where `\0` happens to
fall. **A symbol in the SOURCE at the image's own revision is the evidence; a
byte-pattern in a binary is a proxy metric (Rule 15).**

## The inverse question — did THIS MESSAGE come from the stale process?

The same `git grep <rev>` reading answers it, in the opposite direction: **take a
sentence whose words are NEW and ask which revision has them.** A sentence that
exists only at `HEAD` cannot have been composed by a process running `84beb309`,
however the process table looks.

```
$ git grep -c 'do not chase the network' 84beb309ce3c -- '*.go'
0            # exit=1 — the OLD revision does not have this sentence at all
$ git grep -c 'do not chase the network' HEAD -- '*.go'
HEAD:internal/devworkspace/delivery_anchor.go:1
```

That single line decides the case that prompted this page. A delivery verdict was
relayed as "the stale workers are still printing the misdiagnosis" — but the
verdict quoted `This is NOT a refused pull and NOT a conflict — do not chase the
network` (`internal/devworkspace/delivery_anchor.go:76`), a sentence the old
revision has never contained. **The message was the FIXED arm doing its job**, and
its conclusion — "this ground carries no commit past master" — was literally true
at the moment it was read: `git reflog` held exactly two entries, the clone and
the checkout, so the ground had never committed anything.

Two lessons, and they point the same way:

1. **A verdict is not a defect just because a stale worker exists on the same
   host.** Ask which revision could have composed the words, before concluding
   which process wrote them.
2. **"Nothing to deliver" and "delivery failed" are different claims**, and only
   the second one is a failure. A ground with no commit past master has nothing to
   publish — the honest response is to produce work or to say so, not to
   `--allow-empty` a commit into existence so the machinery has something to move.
   An empty commit manufactured to satisfy a check is a lie with a green tick.

## What the two shapes actually look like, for a reader who must tell them apart

The defect that produced this page: the nothing-to-deliver fork exists **only in
the tree**, and the running image has the older `if` that routes every blocker —
including "this ground carries no commit" — into the failed-delivery exit.

```
84beb309:cmd/agent/devworkspace_channel.go:210-212
	if blocker != nil && !last.Pushed {
		return s.exhausted(log, last, blocker)
	}

HEAD:cmd/agent/devworkspace_channel.go:226-231
	if blocker != nil && !last.Pushed {
		if blocker.Kind == devworkspace.BlockerKindEmpty {
			return s.nothingToDeliver(log, last, blocker)
		}
		return s.exhausted(log, last, blocker)
	}
```

The old image DOES carry the empty-**briefing** branch (line 329 of that rev), so
a `strings` probe for the briefing sentence finds it and the reader concludes the
image is current. The briefing and the terminal verdict are two different arms;
one being present says nothing about the other.

## The operative boundary (Rule 42), stated so it is not read as an oversight

The remedy for shape ② is a **restart**, and an agent must not perform it: those
processes were not created by the run, are not held by it, and a signal addressed
to a pid the caller does not own is a signal aimed at whatever that number names
by the time it is delivered. What the run CAN do is drain-and-hand-over — the
request file the operator's `gopi worker --stop` writes is consumed at the claim
boundary:

```
~/.gopi/worker/TERM_<pid>          # request: pid, argv, signal, ts, target
~/.gopi/worker/ACK_<pid>.<req>.consumed   # receipt: mode=drain, exit=exit at the next round boundary
```

**A receipt is not a death.** Measured on the incident row: eight ACK files were
written between 01:24:13 and 01:24:15

```
$ ls -l --time-style=+%H:%M:%S ~/.gopi/worker/ACK_*.consumed | wc -l
8
$ ls -l --time-style=+%H:%M:%S ~/.gopi/worker/ACK_*.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:14 ACK_1033859.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:14 ACK_1034814.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:13 ACK_1035646.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:15 ACK_1037353.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:15 ACK_1038465.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:15 ACK_1040879.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:15 ACK_1042251.3929899.consumed
-rw-r--r-- 1 the operator the operator 185 01:24:14 ACK_3633443.3929899.consumed
```

Drain is "finish this round, then leave", so the population keeps shrinking
while the receipts sit there unchanged (the receipt list above is the complete
`ls` output — nothing is elided from it). Two readings of the SAME receipt set:

| probe time | pids from those eight still answering `/proc` |
|---|---|
| 01:38:33 | `1033859`, `1035646`, `1038465` — all still `84beb309ce3c` |
| 01:39:41 | `1035646`, `1038465` — `1033859` exited between the two probes |

Both tables are the same 8 ACK files; only `/proc` moved. Judge liveness with
`/proc/<pid>`, never with the ACK file and never with a process name.

## The self-check that already exists, and what it does not cover

`internal/tools/image_selfcheck.go` (adopted by `gopi worker`'s claim loop and by
the merge loop — then `gopi merge --every`, removed 2026-11-01 with the top-level
verb; `gopi worker`'s claim loop is the surviving adopter) answers *"is THIS
process still the artifact at its own
path?"* — see `devdoc/merge-image-selfcheck.md`. It is the mechanism that makes a
replaced image loud **from inside the process**.

It has one shape it cannot cover, and it is exactly the shape above: a resident
started BEFORE the check existed has no check to run. Nothing inside such a
process will ever print the alarm, so the reading has to be taken from outside —
`/proc/<pid>/exe`'s revision plus a source-level `git grep` — which is what this
page prescribes. Adopting the check does not retire this procedure; it narrows
the population it is needed for.

## Verification of the claims on this page

```
$ cd /home/user/work/gopi && git rev-parse HEAD origin/master
3a4c052565d5c1146c419529d98a20e27790e7d1          # the tree the greps ran in
3a4c052565d5c1146c419529d98a20e27790e7d1
$ git merge-base --is-ancestor c63f60ab95ee35c4ffa0e73af15161e08480d868 origin/master; echo $?
0                                                  # the rev these greps were first
                                                   # taken at is still reachable from master
$ for c in 20c15007c 45eb68ece 116de3444; do git merge-base --is-ancestor $c origin/master; echo "$c exit=$?"; done
20c15007c exit=0
45eb68ece exit=0
116de3444 exit=0
$ git merge-base --is-ancestor 20c15007c 84beb309ce3c; echo $?
1                                                  # the running image does NOT have it
```

The counts this page quotes are revision-stable, so re-deriving them later still
answers the same thing — measured again on `origin/master = 3a4c052`:

```
$ git grep -c 'nothingToDeliver' 84beb309ce3c -- '*.go' | wc -l ; git grep -c 'nothingToDeliver' HEAD -- '*.go' | wc -l
0
6
$ git grep -q 'errNothingToDeliver' 84beb309ce3c -- '*.go'; echo $?; git grep -q 'errNothingToDeliver' HEAD -- '*.go'; echo $?
1
0
```

All three fixes are on the integration branch; the image the cohort is running
predates them. Code: nothing to change. Processes: the operator's call.
