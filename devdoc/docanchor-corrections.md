<!-- doc-class: normative -->
# Correction: the permitted-COUNT is not the docanchor stability test

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Feature: `internal/agent` docanchor gate (`TestDocAnchorsResolveInNormativeDocs`) —
which number to quote when proving a merge did not break it.

## The trap

The failure line ends with three counts:

    ... 246 pre-existing drift entries permitted, 8 new, 7 stale permits

The first is `len(permitted)` — the size of the baseline TSV
(`internal/agent/testdata/docanchor-baseline.tsv`) — **not** a measurement of
breakage. A branch that ships legitimate new permits (the sanctioned route for
anchors its own diff moves) increments it *without any defect*, so quoting
`246 → 249` as "the merge changed the gate outcome" is a wrong inference: every
one of those 3 is `feat/merge-every`'s own entry, and the reason text welded to
each line says so verbatim — "feat/merge-every shifted `merge_run.go` … recorded,
not rewritten" (`git diff ec15febf db560467 --
internal/agent/testdata/docanchor-baseline.tsv` → 3 added, 0 removed).

## The two measurements that do answer it

1. **The stale set (the ratchet).** The gate errors on every permit that matched
   nothing: `stale baseline entry %q …`. A live permit is *used* and thus silent;
   a *stale* one means the site was fixed or left the normative class — the
   direction that indicates real drift. Extract and compare the sets:

       go test ./internal/agent/ -run DocAnchor -count=1 -v 2>&1 \
         | grep -E 'stale baseline entry' | sed 's/^ *//' | sort > /tmp/stale.txt

   `diff` of the pre-merge and post-merge files being empty is the proof.
   Measured this run: 7 lines each, identical byte-for-byte, while the permitted
   count went 246 → 249.

2. **Equality of the *new* count.** `new` is the number of un-permitted drift
   sites. Equal pre/post (8 vs 8) bounds the merge's contribution; it does NOT
   prove zero, because a merge can shift pre-existing drift k → k+1 while
   contributing nothing (the drift is un-keyed, so its count is all you get).
   Do not over-attest from it.

## Recipe

    git worktree add /home/user/workspace/<tag> <pre-merge-tip> --detach          # baseline
    cd <baseline> && go test ./internal/agent/ -run DocAnchor -count=1 -v 2>&1 \
      | grep 'stale baseline entry' | sed 's/^ *//' | sort > /tmp/pre-stale.txt
    cd <master>   && go test ./internal/agent/ -run DocAnchor -count=1 -v 2>&1 \
      | grep 'stale baseline entry' | sed 's/^ *//' | sort > /tmp/post-stale.txt
    diff /tmp/pre-stale.txt /tmp/post-stale.txt   # empty = the merge did not make the gate worse
    git worktree remove /home/user/workspace/<tag>                                # scratch cleanup — always
