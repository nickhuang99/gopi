<!-- doc-class: historical -->
# The Go 1.24 toolchain floor: a parser error that was not a parse error

**Trigger:** the operator reported, on Ubuntu jammy:

```
/home/user/work/GoPi/go.mod:3: invalid go version '1.24.0': must match format 1.23
```

**Host:** `hosta` (Ubuntu 22.04.5 LTS, amd64). **Convention note:** the
2026-09-20 doc `devdoc/go-toolchain-env-both-nodes-20260920.md` describes
"both nodes" (peerbox + HP); this is a **third** host, and it had neither fix.

---

## 0. The one-line answer

**`go.mod` was not malformed, and rewriting it cannot fix anything.** The
message is Go 1.16's *parser* rejecting a `go` directive it cannot represent.
The host's only toolchain was `gccgo 1.16.5`; the module and its dependencies
require Go ≥ 1.24. The fix is a **real Go ≥ 1.24 toolchain installed under
`$HOME`** — no root, no package manager — plus two environment corrections that
would otherwise each have been a *second* silent failure.

| # | defect | fix | root needed |
|---|---|---|---|
| 1 | only `gccgo 1.16.5` on the host | Go 1.24.13 under `~/workspace/toolchains` | **no** |
| 2 | `proxy.golang.org` unreachable (Go's default) | `GOPROXY` mirror chain in `~/.config/go/env` | **no** |
| 3 | stock non-login `PATH` puts `/usr/bin` before `~/.local/bin` | prepend in **both** `.bashrc` and `.profile` | **no** |
| 4 | a `bare sh -c` still finds gccgo | *cannot* be fixed user-locally — instructions only | yes (optional) |

All four are carried by `scripts/setup-go-toolchain.sh` (idempotent).

---

## 1. Why "fix go.mod" is the wrong reading — the actual mechanism

Go's `go` directive historically had the form `1.NN`. Go 1.21 moved to a
three-part `1.NN.P` (to distinguish the point releases that carry
security fixes), while **the parser stayed two-part in older releases**. Go
1.16.5 therefore emits exactly:

```
invalid go version '1.24.0': must match format 1.23
```

The text is a *format* complaint, which is why the reported symptom reads like
a typo. It is not: **the value is well-formed and current.** Four spellings
were considered and three are dead ends:

| spelling | verdict |
|---|---|
| `go 1.24.0` (current, as written) | **correct** — unparseable only by the old toolchain |
| `go 1.24` | parses under neither: Go 1.24 wants the patch word for a `1.2x` floor |
| `go 1.23` | the trap — see below |
| no directive | loses the floor entirely; Go assumes an ancient language version |

**The `go 1.23` trap.** Lowering to `1.23` is the *attractive* fix: it is the
only value the old parser accepts, and it appears to make the message go away.
It cannot work here, because the module's **own direct dependencies declare
`go 1.24.0`**:

```
github.com/charmbracelet/bubbletea@v1.3.10  -> go 1.24.0
modernc.org/sqlite@v1.44.0                  -> go 1.24.0
```

A directive is a floor, not a ceiling: the *effective* language version is the
maximum over the module and everything it requires. Lowering the main module's
line changes nothing about what building the deps needs, so the build still
fails — one layer further from the symptom. **This is the whole reason the task
is a toolchain task and not a go.mod task.**

---

## 2. The three environment gaps, each measured

### 2.1 No sufficient toolchain (and apt cannot supply one)

```
$ go version
go version go1.16.5 gccgo (Ubuntu 11.4.0-1ubuntu1~22.04.3) 11.4.0 linux/amd64
$ ls -la /usr/bin/go
/usr/bin/go -> go-11                      # package: gccgo-go
$ apt-cache policy golang-go
  Candidate: 2:1.18~0ubuntu2              # jammy's best; Go 1.24 does not exist there
```

`/usr/local/go`, any snap, `~/sdk` and `~/.local/go` were all absent. The
official tarball is the only route to ≥ 1.24 on jammy, it is self-contained, and
it installs into `$HOME` — so **the constraint that admits no root also admits
no package manager**, and that turned out to be fine.

### 2.2 `GOPROXY` pointed at an unreachable host — and was never configured here

```
$ cat ~/.config/go/env
cat: /home/user/.config/go/env: No such file or directory
```

No per-machine Go env existed at all, so `go` silently used its built-in
default. Measured from this host:

| endpoint | result |
|---|---|
| `https://proxy.golang.org/.../@v/list` | **timeout** (this is the default in use) |
| `https://goproxy.cn/.../@v/list` | `200`, ~0.3 s |
| `https://dl.google.com/go/...tar.gz` | `206`, ~3.5 MB/s |

That asymmetry is why the toolchain download succeeded while module fetches
failed: **different hosts, one blocked.** `devdoc/go-toolchain-env-both-nodes-20260920.md`
had already diagnosed and fixed this — for peerbox and HP. This host never got it.

**Ordering matters when fixing these two**: set `GOPROXY` *before* trusting any
`go` command, or the first module fetch fails for a reason unrelated to the
toolchain and looks like a second bug.

### 2.3 PATH: the same machine has two different answers

This is the subtle one, and it is why a "works for me" fix would have shipped
broken. Ubuntu's defaults disagree with each other:

| shell shape | reads | `~/.local/bin` position |
|---|---|---|
| login (`bash -l`, ssh) | `~/.profile` | **prepended** (line 2) → go1.24 wins |
| interactive non-login | `~/.bashrc` | **appended** (line 11) |
| bare `sh -c` | neither | absent entirely → gccgo |

The stock `.bashrc` **rebuilds** `PATH` from scratch on line 118, then appends
`~/.local/bin` at line 119. `~/.profile` *prepends* it. So the answer to
`go version` genuinely depended on how the shell was started:

```
$ bash -l -c 'command -v go'   →  (after fix) /home/user/.local/bin/go
$ env -i PATH=<stock> sh -c 'command -v go'  →  /usr/bin/go   # STILL gccgo
```

**Why this matters beyond the operator's own terminal:** gopi's post-merge
rebuild runs the declared build command through `sh -c`
(`internal/tools/rebuild.go`, `runBuild`) and **inherits the caller's
environment**. A gopi started from a non-login or non-shell context would have
kept compiling master with gccgo — reproducing the original failure *after* a
merge, which is precisely the failure mode Rule 12 exists to prevent (the
machine running old code against new master). Prepending `~/.local/bin` in
*both* rc files closes login and interactive; the bare-`sh` case is the one left
open, and it is documented rather than papered over.

---

## 3. The one gap that is genuinely root-only — stated, not hidden

`/usr/bin` precedes anything reachable from `$HOME` in the stock non-login
`PATH`, and only root may insert a directory ahead of `/usr/bin`. Two honest
options, in preference order:

1. **Give the launcher its PATH** (no root) — the correct fix for a cron line or
   a unit: `Environment="PATH=/home/user/.local/bin:/usr/local/bin:/usr/bin:/bin"`,
   or `systemctl --user edit <unit>`. This is a *configuration* of the thing that
   launches gopi, so it stays inside gopi's own process tree.
2. **Register the toolchain system-wide** (root, optional) — `update-alternatives`
   rather than overwriting `/usr/bin/go`, because it is reversible:

   ```bash
   sudo tar -C /usr/local -xzf ~/workspace/toolchains/go1.24.13.linux-amd64.tar.gz
   sudo update-alternatives --install /usr/local/bin/go go /usr/local/go/bin/go 100
   env -i PATH="$STOCK_PATH" sh -c 'go version'          # verify with a BARE sh
   sudo update-alternatives --remove go /usr/local/go/bin/go   # roll back
   ```

No `sudo` was run to produce this fix. A privilege is not a missing ingredient —
it is a different decision, belonging to the operator, about a system-level path.

---

## 4. What was changed, exactly

| artifact | change |
|---|---|
| `~/workspace/toolchains/go1.24.13/` | the toolchain (official tarball, under `$HOME`) |
| `~/.local/bin/{go,gofmt}` | **symlinks** to it — a symlink, not a copy, so the two cannot drift |
| `~/.config/go/env` | `GOPROXY=https://goproxy.cn,https://goproxy.io,direct`, `GOTOOLCHAIN=local` |
| `~/.bashrc`, `~/.profile` | one marked, deletable `PATH` prepend each |
| `scripts/setup-go-toolchain.sh` | new: reproduces all of it, idempotently, root-free |
| `internal/standards/go_toolchain_floor_gate_test.go` | new regression gate (below) |
| `go.mod` / `go.sum` | **untouched** — no change was needed |

**`GOTOOLCHAIN=local` is load-bearing, not decoration.** Without it Go 1.21+
may try to *download* a different toolchain to satisfy the directive — the very
network path that is blocked here. Pinning it also makes the build's toolchain
the one that was provisioned, instead of whatever the resolver picks.

**Two knobs were set and then deliberately reverted** (the value of measuring
rather than copying another machine's config):

- `GOFLAGS=-mod=mod` — `devdoc/go-toolchain-env-both-nodes-20260920.md` §3 names
  this as a **known per-machine divergence** whose failure mode is a `go.mod`
  rewritten on one host and rejected on the other. Setting it here would have
  created exactly the conflict that doc warns about. Left unset (Go's default
  `-mod=readonly`), and the build succeeds either way.
- `GOSUMDB=off` — briefly set while the mirror was doubted, then removed once
  checksum verification through `goproxy.cn` was **measured working**
  (`go get github.com/google/go-cmp@v0.7.0` → `go: added …`, no sumdb error).
  Disabling verification is a security downgrade; it is not a fix for a
  reachability problem that does not exist.

---

## 5. The regression guard, and why it is a *direction* check

`internal/standards/go_toolchain_floor_gate_test.go` (Gate 7) asserts the two
halves of the floor agree: `go.mod` must not declare less than its dependencies
require, and the toolchain running the test must satisfy `go.mod`.

**Red-proved, not asserted.** Raising the floor constant to `1.25.0` fails the
first half; a version probe over the three real versions checks the direction:

```
go1.16.5   vs go1.24.0 -> FAIL (too old)      ← the reported host
go1.23.0   vs go1.24.0 -> FAIL (too old)      ← the "lower the directive" trap
go1.24.13  vs go1.24.0 -> PASS
```

**A trap documented in the code because it was nearly walked into:** the obvious
`version.Lang(runtime.Version())` returns only `major.minor`, and
`version.Compare("go1.24", "go1.24.0")` is **-1** — so lang-ing the running
version makes a *correct* toolchain fail the gate. The gate compares the raw
runtime version against the declared floor, both full, and says so in a comment
so the next reader does not "simplify" it back into the bug.

The gate is offline and deterministic by design: a toolchain-floor check that
needed the network or a warm module cache would be skipped exactly when it
matters most.

---

## 6. Reproduce / verify

```bash
# the defect, on a jammy host with only gccgo (do NOT run this on the fixed host)
go build ./...          # go.mod:3: invalid go version '1.24.0': must match format 1.23

# is the toolchain sufficient? (always ask the toolchain that will build)
go env GOVERSION

# can the proxy be reached at all?
curl -s -o /dev/null -w 'proxy=%{http_code} t=%{time_total}s\n' \
  https://goproxy.cn/github.com/google/uuid/@v/list

# decisive end-to-end: fetch a module that is NOT cached, then build
cd "$(mktemp -d)" && go mod init probe && go get github.com/google/go-cmp@v0.7.0

# the provisioning script (idempotent; safe to re-run)
bash scripts/setup-go-toolchain.sh

# the three shell shapes, with an ISOLATED env — the only honest test
bash -lc 'command -v go; go env GOVERSION'
env -i HOME="$HOME" PATH=/usr/local/bin:/usr/bin:/bin sh -c 'go version'   # still gccgo; documented
```

**Portable rule:** a toolchain mismatch does not announce itself as a toolchain
mismatch. When a *syntax/format* complaint names a correct-looking value, check
the version of the tool doing the parsing before editing the value. And when a
fix works in your shell but not in a process, compare the **PATH each context
actually gets** — "it works for me" and "it works for a `sh -c` spawned by gopi"
are different claims, and only one of them is a build.
