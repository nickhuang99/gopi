#!/usr/bin/env python3
"""Emit the Stage-0 RESIDUAL shard: per-row verdicts for the 68 rows that only
Stage 0's class-level partition covered.

Why this file exists: Stage 0's CSV carries no `verdict` column (it is the class
partition, not a disposition), so the 68 rows that no later stage claimed had
their verdicts only in a session scratch map. Stage 6 promotes them into a
committed shard, so the final 319-row table is reproducible from committed
artifacts alone (rule R8) — and so the 29 real-content rows carry the git fact
that decided them rather than a bare verdict.

Split of the 68 (Stage 0 verify §A1/§A2 + §B):
  * 29 real-content  -> individually git-checked (EVIDENCE below)
  * 39 void by shape -> rule R4 (no work identity: chat backfill / CLI probe /
                        deliberately-failing fixture)

  python3 devdoc/ledger-treasure-hunt-20260917-rows-stage0-residual.py            # emit
  python3 devdoc/ledger-treasure-hunt-20260917-rows-stage0-residual.py --check    # verify
"""
import argparse
import csv
import io
import sys

SRC = "devdoc/ledger-treasure-hunt-20260917-rows-stage0.csv"
OUT = "devdoc/ledger-treasure-hunt-20260917-rows-stage0-residual.csv"
COVERED = "devdoc/ledger-treasure-hunt-20260918-rows-final.csv"

# The 29 real-content rows: verdict + the git/master fact that decided it.
# Each evidence cell names a resolvable ref and what it showed (rule R1: a merge
# proof is a resolvable ref plus a zero count, never a status field).
EVIDENCE = {
    "459a4263-862d-423c-a2aa-ec6ebf13c94c": ("ALREADY-DONE",
        "master:internal/tools/read_symlink_test.go + read.go carry the symlink refusal; landed 7f0427eb 2026-08-29 'fix(tools): symlink-aware read/write/edit — refuse link paths by default, follow_symlink=true resolves target'"),
    "7d0507eb-cbf5-4714-b513-b74dcc9d7c1a": ("ALREADY-DONE",
        "same landing as the row above — 7f0427eb is the fix for this payload's own /tmp/symlink-discussion.txt brief"),
    "b4b6b43c-cb43-46e8-87ea-b30eb95ebf8b": ("ALREADY-DONE",
        "master:devdoc/assets/bm-pricing-20260830.png + devdoc/assets/bm-pricing-cny-20260830.txt — the captured price tables are committed"),
    "8ef3003b-3db5-4a40-b596-872ed4a5dbcd": ("ALREADY-DONE",
        "master:devdoc/memroadmap-m3.md (12eca488) — the M3 implementation doc is on master"),
    "f14ae1bd-aa6c-456b-afb4-c2780ef57469": ("ALREADY-DONE",
        "master:devdoc/memroadmap-m3.md; the M3 acceptance this row tracked is the doc on master, branch feat/memroadmap-m3 no longer exists (merged)"),
    "3644f54d-9c9d-48ed-be0d-8bf1954291f1": ("ALREADY-DONE",
        "master:devdoc/chat-table-design.md + merge 0e2d727f \"Merge branch 'feat/chat-table'\"; the chat table and remote dispatch both landed"),
    "764751dd-42c8-46ed-b085-c91bd9b44614": ("SUPERSEDED",
        "the research question (cr-sqlite/litestream/rqlite for 2-node sync) was answered by the implemented mechanism: master:devdoc/memsync-bidirectional.md + devdoc/bidir2-fix.md"),
    "e6e82b59-0106-4a4c-a5d8-3b39fb369893": ("ALREADY-DONE",
        "master:internal/tools/memory_sync_assets.go — HP branch/asset harvest back to peerbox is implemented; feat/chat-table merged 0e2d727f"),
    "6c723b40-e2f3-4ad5-a0dc-6b38c953e162": ("ALREADY-DONE",
        "master:cmd/agent/cli_shot.go + internal/shot/ (cdp.go, chrome.go, x11.go) — the gopi shot subcommand exists as specified"),
    "aab1b66a-4017-49d6-bc86-cc7d185a0305": ("ALREADY-DONE",
        "master:internal/tools/memory_sync_assets.go + memory_sync_bidir2_e2e_test.go — bidirectional HP/peerbox asset sync with no-drop semantics"),
    "af1bb7d5-abe4-432b-96f6-ff07f2701311": ("ALREADY-DONE",
        "master:internal/agent/loop.go carries the 1214 stream-shape guard this row asked for"),
    "fcaff097-b868-4990-a227-5972a8dcac76": ("ALREADY-DONE",
        "master:internal/tools/task_adoption.go + task_merged_cleanup.go — the three-stage branch adoption chain (discover→merge→adopt) is implemented"),
    "fc652652-647a-4c59-a98c-80fdf84d7652": ("ALREADY-DONE",
        "master:cmd/agent/cli_tasks.go:1997 documents the standard chain 'client pushes → worktree remove → branch -d', i.e. the cleanup code the row said did not exist"),
    "59d49895-6021-436b-9b92-b213dc6d546e": ("ALREADY-DONE",
        "master:internal/tools/memory_sync_claim.go + cmd/agent/cli_tasks_sync.go implement claim-is-ownership broadcast"),
    "137026e9-44a6-4ea3-afe3-0a2465d0fe8a": ("ALREADY-DONE",
        "master:internal/tools/memory_sync_claim.go — the M4.18 rejection semantics (local row must not stay pending) landed"),
    "f8f351f8-ff77-4556-a3b4-c14c1e685b9e": ("ALREADY-DONE",
        "master:devdoc/code-review-2026-08-28.md and the coverage-slice review reports (7d44de48 'docs(cov-agent-cr)') are the full-review deliverable"),
    "6aa98260-8f28-47b7-9c76-d5ac82251a1a": ("ALREADY-DONE",
        "master:internal/tools/memory_sync_assets.go — 'first PULL all new records, then PUSH' is the implemented two-phase sync"),
    "cbe57cd6-c251-4fe1-8c25-7259e988e121": ("ALREADY-DONE",
        "master carries the bug056 fix: 52e4a39d / 659b1f42 'bug056 contract — conflict report rides task_events, not immutable summary'"),
    "01a0882c-954d-7368-8b42-9ae0caa4c4e9": ("ALREADY-DONE",
        "the resolver's own branch landed: master 5452a3e0 \"resolve merge feat/job-family-t3 into master\""),
    "01a0898a-88f8-7e4b-bea9-5736e8a2b2c0": ("ALREADY-DONE",
        "master 35423b4e \"Merge branch 'feat/hippocampus-host-binding'\" — the conflict this resolver was spawned for is resolved on master"),
    "01a089a1-7e3d-7169-81dc-d1851f2e0942": ("ALREADY-DONE",
        "same branch as the row above (feat/hippocampus-host, second resolver run) — also landed via 35423b4e"),
    "01a089a7-26cb-744f-8e74-a9180f4e5503": ("ALREADY-DONE",
        "master d75818e8 \"Merge branch 'feat/safe-merge-guard-answer'\" — the resolver's outcome is the merge commit"),
    "01a0a044-166a-75ed-90f6-a546f024d345": ("ALREADY-DONE",
        "the verification it performed is the artifact: master:tasks/01a09d07-no-privilege-daemon-report.md exists; verdict is one inference deep from its target's landing (stated, not silently upgraded)"),
    "01a0a1c0-c422-7757-ae01-80286a9579f5": ("ALREADY-DONE",
        "master:devdoc/stale-merge-audit-20260916.md (6197f1d1) — the stale-branch audit it asked for is committed"),
    "t-1788513902": ("NO-VALUE",
        "shape rule R4: payload is 'Reply with exactly: GOPI-DO-SMOKE-OK' — a harness smoke probe, no work identity"),
    "01a08c34-52b7-7a12-83d4-a45957d988a7": ("NO-VALUE",
        "shape rule R4 + Rule 22: the deliverable was a /tmp screenshot path (/tmp/llm-shot.png) on a swept host — nothing to check, nothing to recover"),
    "01a08e7f-ccaa-7e1c-a2e0-d35ada3c18fd": ("NO-VALUE",
        "shape rule R4: writes cost-probe.txt in a scratch dir as a cost probe; no product artifact"),
    "01a08e9b-4573-7026-a6fc-d08e59f0553e": ("NO-VALUE",
        "shape rule R4: same cost-probe as the row above, second run"),
    "01a09990-9282-7b17-8c82-e45bcb3fa0b4": ("NO-VALUE",
        "shape rule R4: a Rule-11 probe that runs one bash command (git worktree add /dev/shm/...) and reports output — the probe is the deliverable, and a /dev/shm worktree is refused by policy"),
}

# The 39 void-by-shape rows, by the Stage 0 verify section that named them.
CHAT_BACKFILL = [
    "663d74a7-e813-4495-9cfe-1c137f76cbd6", "5666322d-4354-4662-bc01-23218e79ead8",
    "87663cb5-7ab7-46f5-99c5-24f934ba2201", "6874f922-ed2c-4e67-9b07-b782718f0daf",
    "b1fddd7e-5006-4522-a44e-62f02373e198", "85a3c295-d83e-4546-adac-ac15baa39f3a",
    "7a48b3c1-20ad-4030-8866-3644d289a9b2", "aa1b288d-f720-42d3-af23-8fb60a50e496",
    "28f3d941-6d6a-4e98-b8bc-4840dffed6fe", "58256cf1-24fd-4549-a149-e73e71a26544",
    "1f944cf2-3f71-4ee8-b2ee-6897f9229613", "28cdf1cd-ac2e-4492-83f6-387f1c908fc2",
    "871c3ccb-1fe9-4682-9aa5-14b29d605cad", "702ebd2a-7f04-4e2e-ac19-19dc37aa1259",
    "5f8ebe4d-e3ee-4ddb-a8b5-b02611f65c08", "14617361-f8b5-4b40-8cd4-399e528230be",
    "5c986760-97bb-43d8-a544-cfad5a52f483", "b1476151-e657-4ab4-982f-138efc4a84a1",
    "f92a3523-4e66-46b1-8ca7-a494fd4d1967",
]
CLI_PROBE = [
    "82a5d2ff-e245-4577-a16b-66f292871af1", "ee8c365b-6e3a-4850-a327-64227e844380",
    "78d40ad1-2150-4b00-9a78-a7bd31933583", "01a09573-328f-756d-b895-0292fc7cf60e",
    "01a097c6-3125-7d36-b28f-cb4cf698ced1", "01a0996c-fb44-73d2-85b2-87cb5f1707e9",
    "01a09a53-b747-76c5-9b2e-427911801592", "01a09f6d-7753-7321-ba33-8b32b09213e7",
    "01a0a0b7-b774-7909-846a-89f18ad4c13b", "01a0a1af-adae-7430-a018-80e28f4e3bf7",
]
FIXTURE = [
    "6fa09727-3c2b-4c97-97c9-c7e6bed29b7a", "d268ed28-3493-491a-ad61-2a227f256706",
    "813e4557-cc36-4b4b-8dec-77a40025caac", "0a34e9a9-3771-4130-8e99-113c2ddfbc56",
    "946d7023-c3e3-45df-b127-a02dd51aa0df", "830509e2-35d4-4c41-a7a8-9c67fbd93961",
    "45170459-8f50-4b23-b810-0825104f04a5", "13b13d0a-52a7-4780-a1df-619a044130a4",
    "907082b2-0525-4e78-8681-f43804c3d2e5", "f74719f0-6782-4516-9ac0-68cfa6b492f8",
]
SHAPE_RULE = "shape rule R4 (no work identity) — "


def shape_rows():
    for t in CHAT_BACKFILL:
        yield t, "NO-VALUE", SHAPE_RULE + "interactive chat turn backfilled as a ledger row (`— chat: backfilled`)"
    for t in CLI_PROBE:
        yield t, "NO-VALUE", SHAPE_RULE + "<=25-char CLI probe"
    for t in FIXTURE:
        yield t, "NO-VALUE", SHAPE_RULE + "deliberately-failing test fixture (`FAILED — recoverable: gopi resume …`)"


def build():
    """Every row the later stages did NOT claim, in worklist order."""
    covered = set()
    if _exists(COVERED):
        with open(COVERED, newline="") as fh:
            covered = {r["task_id"] for r in csv.DictReader(fh) if r["shard"] != "s0"}
    if not covered:
        # --check before the final table exists: use the shards themselves.
        covered = set()
        for p in ("ledger-treasure-hunt-20260917-rows-stage5-resolved.csv",
                  "ledger-treasure-hunt-20260917-rows-stage4-hp.csv",
                  "ledger-treasure-hunt-20260917-rows-stage3-cancelled.csv",
                  "ledger-treasure-hunt-20260917-stage2-verdicts.csv",
                  "ledger-treasure-hunt-20260917-rows-stage1-merge-lane.csv"):
            with open("devdoc/" + p, newline="") as fh:
                covered |= {r["task_id"] for r in csv.DictReader(fh)}

    rows = []
    with open(SRC, newline="") as fh:
        for r in csv.DictReader(fh):
            tid = r["task_id"]
            if tid in covered:
                continue
            if tid in EVIDENCE:
                v, ev = EVIDENCE[tid]
            else:
                v, ev = None, None
            rows.append((tid, v, ev))
    # rows the shape lists own
    shape = {t: (v, e) for t, v, e in shape_rows()}
    out = []
    for tid, v, ev in rows:
        if v is None:
            if tid not in shape:
                sys.exit("FATAL: residual row %s has neither git evidence nor a shape "
                         "rule — name it before the shard can be emitted" % tid)
            v, ev = shape[tid]
        out.append({"task_id": tid, "verdict": v, "evidence_cmd": "", "evidence_result": ev})
    return out


def _exists(p):
    import os
    return os.path.exists(p)


def to_csv(rows):
    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=["task_id", "verdict", "evidence_cmd", "evidence_result"],
                       lineterminator="\n")
    w.writeheader()
    w.writerows(rows)
    return buf.getvalue()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true")
    a = ap.parse_args()
    rows = build()
    if len(rows) != 68:
        sys.exit("FATAL: residual shard has %d rows, expected 68" % len(rows))
    body = to_csv(rows)
    if a.check:
        if open(OUT).read() != body:
            sys.exit("FATAL: %s is stale — re-run without --check" % OUT)
        print("%s matches (%d rows)" % (OUT, len(rows)))
        return
    open(OUT, "w").write(body)
    import collections
    print("wrote %s (%d rows) %s" % (OUT, len(rows), dict(collections.Counter(r["verdict"] for r in rows))))


if __name__ == "__main__":
    main()
