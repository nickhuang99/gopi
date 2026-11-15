<!-- doc-class: normative -->
# Go toolchain environment: both compute nodes must be identical

**Author:** PA (session 2026-09-20)
**Status:** applied and verified on both nodes
**Trigger:** the operator asked *"HP 使用go的.cn对吗？为什么peer1不也这样子？"*

---

## 0. The one-line answer

**Neither node had `GOPROXY` set.** Both were silently using Go's built-in
default `https://proxy.golang.org,direct`, and `proxy.golang.org` is
**unreachable from this network**. The fix is a per-machine `go env -w`, and
both nodes now carry the same value.

| node | GOPROXY (now) | verified |
|---|---|---|
| peer1 | `https://goproxy.cn,https://goproxy.io,direct` | fetched `github.com/google/go-cmp v0.7.0` (a module not in cache) — `go mod tidy` + `go build` rc=0 |
| HP | `https://goproxy.cn,https://goproxy.io,direct` | `go list -m -versions github.com/muesli/termenv` returns the full version list, byte-identical to peer1's |

Files: `~/.config/go/env` on each node. Backups: `~/.config/go/env.bak-20260920`
on both. `GOPROXY` is **per-machine**; the two nodes were independent and
neither had ever been configured.

---

## 1. Measurements (why the default fails here)

Reachability from peer1, measured, not assumed:

| endpoint | result |
|---|---|
| `https://goproxy.cn/…/@v/list` | **http=200, 0.297 s** |
| `https://proxy.golang.org/…/@v/list` | **timeout** (Go's default — the one in use) |
| `https://goproxy.io/…/@v/list` | http=200, 0.761 s |
| `https://mirrors.aliyun.com/goproxy/…/@v/list` | http=200, 2.760 s |

`goproxy.cn` is ~10× faster than the aliyun mirror and is the stable first
choice; `goproxy.io` is the second hop; `direct` is the last resort (plain
HTTPS to the module host).

**Note the earlier misreading this corrects:** the believed story was "HP has
`.cn` and peer1 does not". HP did **not** have it either — HP was succeeding via
the `direct` fallback (slow, GitHub-dependent, worked by luck). Nothing was
"chosen" here; the setting had simply never been made on either side.

---

## 2. Why this matters beyond Go (the reason it is written down)

This is a **distributed-development conflict source**, and it is the cheapest
kind to remove:

> An environment difference masquerades as "we must collaborate across
> machines" — and cross-machine collaboration is exactly where merge conflicts
> come from.

Concrete instance from the same session: Bubble Tea appeared to be unbuildable
on peer1 ("peer1 cannot fetch charmbracelet"), which motivated a cross-machine
copy of 10 Go modules from HP's `$GOMODCACHE`. The copy was not wasted — it
established a genuinely useful `GOPROXY=off` offline-build capability — **but the
root cause was one missing environment line.** Checking reachability before
moving files would have reduced a cross-machine operation to a one-line config.

**Rule:** before concluding "this machine cannot do X, we must borrow from the
other machine", verify the machine cannot do X. Environment gaps are cheaper to
close than cross-machine work is to coordinate.

---

## 3. Remaining known inconsistency (documented, NOT changed)

HP's `~/.config/go/env` additionally carries:

```
GOFLAGS=-mod=mod
```

peer1 has **no** `GOFLAGS` (i.e. Go's default `-mod=readonly`).

**What it does:** with `-mod=mod`, `go build` / `go test` will silently **rewrite
`go.mod` / `go.sum`** instead of failing with "go.mod needs updating".
**The failure mode it can create:** editing on HP rewrites `go.mod`; editing the
same tree on peer1 then errors; `git status` shows a modified `go.mod` — a
conflict that appears **only** because the two machines differ.

**Why it exists (inferred, not confirmed):** the HP branch
`feat/gopi-cli-bubbletea` pins indirect deps to versions "present in the local
module cache so the build works fully offline" — writing those pins requires
`-mod=mod`. That constraint no longer applies now that `goproxy.cn` is
reachable.

**Why it was NOT removed:** it may be load-bearing for work in flight on HP.
Measured state at the time of writing: HP's working tree is **clean** for
`go.mod`/`go.sum`, so it is not currently causing damage. Left as an observed
item for the operator to decide; do not change it as a side effect of other work.

---

## 4. Reproduce / verify

```bash
# what each node actually resolves to
go env GOPROXY GOFLAGS
cat ~/.config/go/env

# reachability (from the node in question)
curl -s -o /dev/null -w 'http=%{http_code} time=%{time_total}s\n' \
  https://goproxy.cn/github.com/google/uuid/@v/list

# decisive: can it fetch a module that is NOT in the local cache?
cd "$(mktemp -d)" && go mod init probe \
  && go get github.com/google/go-cmp@latest \
  && echo FETCH-OK
```

---

## 5. Portable rule

**Any tool in the build path must have identical configuration on every node
that builds this repository.** When a node cannot do something its peer can,
compare `env` before concluding the machine is incapable — and when a node's
environment differs, fix the environment rather than routing work around it.
