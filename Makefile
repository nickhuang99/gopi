# GoPi build configuration
# Static linking is required: CGO_ENABLED=0 produces a zero-dependency binary.

.PHONY: all build test verify fmt vet clean

all: build

# ONE BINARY (Rule 35). `cmd/agent/` is the only main package in the tree: the
# two programs that used to be built beside it — ledger-window and
# text2img-verify — are `gopi ledger-window` and `gopi text2img-verify` since
# 2026-10-02 (two verb files under cmd/agent). `bin/gopi` is therefore not the
# DEFAULT artifact but the ONLY one; the sentence "one binary, a settings file,
# and nothing else" now has no exception to explain. A third program,
# `gopi-replay`, went the other way: it was abolished on 2026-10-03 — replay is
# `gopi chat --replay <session>` now, a FLAG on the chat verb (memory #7653: the
# comparison with log/raw/stats was about STATUS, never about SHAPE), so there
# is no replay target here either.
BIN_DIR := bin
# BIN_PATH is the build ARTIFACT's path — the repo-PRIVATE name for it. It must
# not be called GOPI_BIN: that name is internal/testutil's published CALLER pin
# (testutil.BinaryPinEnv, the first arm of resolveGopiBin), a name whose whole
# contract is "the caller names the binary under test". An unconditional
# assignment to it (make takes its own value over the inherited one, and keeps
# the variable's export bit) silently handed every recipe's child the RELATIVE
# `bin/gopi` instead of the caller's pin; `go test` gives each package its own
# directory as the CWD, the relative path does not resolve, and resolveGopiBin's
# fatal arm — deliberately no silent substitution — turned the caller's pin into
# a red suite. Measured: `GOPI_BIN=$PWD/bin/gopi make -p | grep '^GOPI_BIN'` →
# `GOPI_BIN := bin/gopi`, while the same query with `-f /dev/null` shows the
# caller's absolute path. The gate that keeps the two names apart:
# internal/standards/makefile_caller_env_gate_test.go; the full account is
# devdoc/makefile-caller-pin-clobber.md.
BIN_PATH := $(BIN_DIR)/gopi

build:
	CGO_ENABLED=0 go build -o $(BIN_PATH) ./cmd/agent/

# The per-package test BUDGET lives in exactly one place: the test_budget
# constant at the top of scripts/test-budget.sh (Rule 1a/2). Go's default (10m)
# is a wall-clock cap that a loaded host trips before the suite finishes, which
# is how `internal/tools` came to report a 600s FAIL that read like a broken
# test — see devdoc/test-budget-single-source.md. The script also stamps wall
# time + load average and reports a budget trip AS a timeout (exit 3), never as
# a bare FAIL.
#
# THIS EXPORT SHIPS NO TEST SUITE, AND THIS TARGET SAYS SO INSTEAD OF PRINTING
# A GREEN. The internal repository carries ~2500 test files against a live
# ledger, a real device scratch tree and host-bound fixtures; none of that
# survives a portable export, so `test` here runs the two checks that ARE
# meaningful on a tree of product code only: the compiler over every package,
# and `go vet`. It is named `test` because that is the button a reader presses —
# a button that silently ran nothing would be the worst answer of all.
#
# `verify` is the alias with the honest name; `make test` and `make verify` are
# the same two lines.
test verify:
	CGO_ENABLED=0 go build ./...
	go vet ./...

# THE FOUR SUITE TARGETS ARE GONE WITH THE SUITE (test-all / test-heavy /
# test-budget / flake-catch). Each of them dispatched into
# scripts/test-budget.sh, which needs packages that actually carry _test.go
# files; on this tree it would scan, find none and stamp `outcome=pass` over
# zero arms — a green that measured nothing. They are DELETED rather than
# redefined, because a target that exists is a promise that it does something.

fmt:
	gofmt -l -w .

vet:
	go vet ./...

# THE REPLAY TARGET IS GONE (2026-10-03), and there is deliberately nothing in
# its place. `scripts/replay/replay.sh` was a SHELL pipeline that drove a SECOND
# RENDERER (`internal/replay`, the self-drawn band/editor/status) and then
# photographed it. Both halves were abolished: replay is
# `gopi chat --replay <session>` — the REAL chat surface re-running a recording —
# and the film pipeline that wraps it (Xvfb + a real terminal + GIF assembly) is
# being ported into Go. A `make replay` recipe here would be a SECOND entry point
# to the same run, with its own defaults for "where artifacts land" and "which
# terminal" — the drift Rule 14 exists to prevent. The ONE route is the flag.

clean:
	rm -f $(BIN_PATH)

# install — the ONE named command that takes a reader from a checkout to a
# `gopi` on their PATH (README's "安装" section, the gap a 17-dimension
# competitor comparison ranked first by ROI). It is the same artifact `build`
# produces, only placed where the shell will find it: there is no second build
# path, no wrapper script, no download. PREFIX is the ONE knob, and the default
# is `go env GOPATH`/bin because that directory is already on PATH for anyone
# who builds Go projects — so the common case needs no argument at all.
#
# `go env GOPATH` rather than $GOPATH: the variable is usually UNSET and the
# default lives in the tool's own config, so reading the variable would install
# to `/bin`, which a non-root user cannot write (Rule 15: fail loudly at a path
# the user did not choose is worse than one they did).
PREFIX ?= $(shell go env GOPATH)/bin

.PHONY: install
install: build
	@mkdir -p $(PREFIX)
	install -m 0755 $(BIN_PATH) $(PREFIX)/gopi
	@echo "installed $(PREFIX)/gopi"
	@case ":$$PATH:" in \
	  *":$(PREFIX):"*) ;; \
	  *) echo "warning: $(PREFIX) is not on PATH — add it, or re-run: make install PREFIX=/somewhere/on/path" >&2 ;; \
	esac

# --- man pages: markdown SINGLE SOURCE -> roff (.1) ---------------------------------
#
# ONE source, ONE command. man/*.md is the source a human edits; the generated
# regions inside it are rewritten from the `-h` pages the built binary prints,
# so the man page and the help text are tied to the SAME artifact rather than to
# two hand-kept copies (epic "man page 方案 A", operator decision 2026-10-11).
# The pipeline is `markdown --pandoc--> roff`. `man` is the read half of it; the
# RENDER half needs `pandoc`, which is an OPTIONAL dependency of this target —
# the render leg is guarded by `command -v` and SKIPS LOUDLY, naming what it
# skipped and pointing at `make man-check`, when the renderer is missing
# (Rule 15: a missing renderer must not read as "the whole man pipeline is
# broken"). Measured 2026-10-14 (host-b): `make man` used to die with exit 127 at
# the render leg — `/bin/sh: 1: pandoc: not found` — after the source step above
# it had already succeeded, printed no hint at all, and this comment asserted the
# opposite: that pandoc was "already on the host". Only the .1 artifacts under
# bin/ (untracked, regenerable — Rule 39) go missing when pandoc is absent; the
# man/*.md sources are regenerated either way. The recipe adds no dependency of
# its own (Rule 35: no new script, service, state file or daemon; `manifest`
# step 1 of the epic is the FOURTH answer to "who owns its lifecycle" — the
# recipe is invoked, it does not schedule itself).
#
# The rendered .1 files land beside the built binary under bin/ and are NOT
# tracked: a clone rebuilds them with this one target, exactly as it rebuilds
# the binary (GOPI.md Rule 39 — a regenerable artifact's recipe is the
# deliverable, not the artifact). The gate that refuses drift between the source
# and the help pages is cmd/agent/man_source_gate_test.go; the generator that
# rewrites the generated regions is internal/standards/testdata/mancensus.go
# (Rule 38: the figure/product names the program that re-derives it).
MAN_SRC_DIR := man
MAN_OUT_DIR := bin/man/man1
MAN_PAGE_NAMES := gopi gopi-chat gopi-config gopi-memory gopi-merge gopi-notify \
	gopi-screen gopi-session gopi-task gopi-tool gopi-ui_state gopi-version \
	gopi-web gopi-worker

.PHONY: man man-check

man: build
	@mkdir -p $(MAN_OUT_DIR)
	go run internal/standards/testdata/mancensus.go . $(BIN_PATH)
	@set -e; if command -v pandoc >/dev/null 2>&1; then \
	  for p in $(MAN_PAGE_NAMES); do \
	    pandoc -s -t man $(MAN_SRC_DIR)/$$p.md -o $(MAN_OUT_DIR)/$$p.1; \
	  done; \
	  echo "man: rendered $(MAN_OUT_DIR)/ for: $(MAN_PAGE_NAMES)"; \
	  echo "man: read them with  MANPATH=$(CURDIR)/bin/man man gopi"; \
	else \
	  echo "man: SKIPPED the render leg - no pandoc on this host, so no .1 pages were written to $(MAN_OUT_DIR)/."; \
	  echo "man: what DID happen: the source step above regenerated $(MAN_SRC_DIR)/*.md from the built binary's -h pages."; \
	  echo "man: pandoc is an OPTIONAL dependency of this target, not a broken pipeline; the render leg is the only part it feeds."; \
	  echo "man: verify the sources you just regenerated with  make man-check"; \
	  echo "man: install pandoc (e.g. apt-get install pandoc) and re-run make man to get the .1 pages."; \
	fi

# man-check proves the sources are NOT stale against the built binary — the same
# predicate the Go gate asserts, available to a shell (exit 1 = regenerate).
man-check:
	go run internal/standards/testdata/mancensus.go . $(BIN_PATH) --check
