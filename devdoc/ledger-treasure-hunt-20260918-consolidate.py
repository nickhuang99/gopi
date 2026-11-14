#!/usr/bin/env python3
"""Stage 6 consolidation — emit the per-row verdict table for the 319-row worklist.

Re-runnable and deterministic: it NEVER queries the live ledger (rule R8). Its only
inputs are the FROZEN worklist and the six committed shards, so it reproduces
devdoc/ledger-treasure-hunt-20260918-rows-final.csv byte for byte.

  python3 devdoc/ledger-treasure-hunt-20260918-consolidate.py            # emit + report
  python3 devdoc/ledger-treasure-hunt-20260918-consolidate.py --check    # verify only

Shard precedence is pinned BEFORE classifying (rule R5) — otherwise the 24 rows
carrying more than one stage verdict would shift totals between runs.
"""
import argparse
import collections
import csv
import hashlib
import sys

D = "devdoc/ledger-treasure-hunt-20260917-"
MASTER = D + "rows-master.csv"
MASTER_MD5 = "ff312577ccb7eb0406b520cce31b15cb"
OUT = "devdoc/ledger-treasure-hunt-20260918-rows-final.csv"

# Pinned BEFORE classifying (rule R5): first shard that has a row wins.
# s0 (the auto-prefixed residual) is last — it is the class-level catch-all.
PRECEDENCE = ["stage5", "stage4", "stage3", "stage2", "stage1", "stage0"]
SHARDS = {
    "stage5": (D + "rows-stage5-resolved.csv", "task_id"),
    "stage4": (D + "rows-stage4-hp.csv", "task_id"),
    "stage3": (D + "rows-stage3-cancelled.csv", "task_id"),
    "stage2": (D + "stage2-verdicts.csv", "task_id"),
    "stage1": (D + "rows-stage1-merge-lane.csv", "task_id"),
    "stage0": (D + "rows-stage0-residual.csv", "task_id"),
}

# Stage 0's own class partition (partition B, mutually exclusive, remainder explicit).
# Stage 1's 83-row shard is the ORACLE SUPERSET (the epic's Phase-0 branch census):
# only its 17 ledger rows may enter the table; the other 66 are not ledger rows.
CLASS_ORDER = [
    "wrapper:t-*",
    "sync-yielded",
    "merge-lane",
    "auto-prefixed",
    "curated-failed",
    "curated-cancelled",
    "HP-origin",
]
VERDICT_ORDER = [
    "ALREADY-DONE",
    "NO-VALUE",
    "SUPERSEDED",
    "STILL-NEEDED",
    "PARTIALLY-DONE",
]

# The two rows where the shards DISAGREE on the verdict. Precedence alone would
# pick one silently; these are decided on EVIDENCE and pinned here, so the
# resolution is visible in the artifact instead of implied by the shard order.
# (task_id -> (winning shard, verdict, why))
CONFLICTS = {
    # Row died 20:02:29; the doc it was told to review was FIRST created by
    # 448a3ea1 at 20:56:46 — 54 min AFTER the row ended, so it cannot be the
    # row's output. The deliverable was a 300-500 word reply and the session
    # was killed by a 429 at iteration 11 (no report ever produced).
    "t-1788551878": ("s2", "NO-VALUE",
                     "evidence: subject doc authored 54min after the row ended; "
                     "deliverable was a reply never produced"),
    # Stage 2 greened it from the cli_voice sites; stage 5 re-ran the gate.
    # Both facts are true: the cited sites closed, the gate is still RED
    # (re-measured this stage). The row is about the GATE -> still needed.
    "01a091fb-1e7d-7293-a493-22d3748d2f59": ("s5", "STILL-NEEDED",
                                             "evidence: TestStyleGate FAIL re-measured at master"),
}

# Every STILL-NEEDED / PARTIALLY-DONE row must carry a submitted successor (§6).
# Two rows share one task: the screenshot paradigm is ONE mechanism filed from two
# rows (pa-q-spa-screenshot-paradigm + pa-q-cny-screenshot-shot).
RECOVERY = {
    "pa-q-spa-screenshot-paradigm": "01a0b4af-78da-7d9b-b9df-4dc5f317f52e",
    "pa-q-cny-screenshot-shot": "01a0b4af-78da-7d9b-b9df-4dc5f317f52e",
    "t-1788018876": "01a0b4af-ff33-7f09-a89b-997505f40ed5",
    "t-1788041891": "01a0b4ba-173e-70a6-9f21-72721927fe4f",
    "t-1788065158": "01a0b4af-93cf-788e-a916-ebdc122a8220",
    "t-1788468254": "01a0b4d6-5131-7a22-bfc1-58d41e289558",
    "t-1788897004": "01a0b4af-b718-7766-a6b6-295b2d7aecbd",
    "01a085d2-b051-70af-82f5-cb5f3b28c238": "01a0b4ba-a43e-78c8-9b96-9ac0eeac5959",
    "01a091fb-1e7d-7293-a493-22d3748d2f59": "01a0b4dc-771d-7f1a-a3de-332123331788",
    "01a0a4c4-c26a-726f-a32f-72b171194741": "01a0b4a7-b0b2-7c35-aeef-41db957ae921",
    "01a0a92d-77a0-7eb0-90a6-8c5f63051177": "01a0b4a7-b427-7e42-897f-3ed84da2c8a4",
}


def load_shard(path):
    """task_id -> verdict, plus every id the shard mentions (id set is the extras
    check: ids outside the frozen worklist are reported, never silently dropped)."""
    verdicts, ids = {}, set()
    with open(path, newline="") as fh:
        for row in csv.DictReader(fh):
            tid = (row.get("task_id") or "").strip()
            if not tid:
                continue
            ids.add(tid)
            verdicts.setdefault(tid, (row.get("verdict") or "").strip())
    return verdicts, ids


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true",
                    help="verify the committed CSV matches; write nothing")
    args = ap.parse_args()

    raw = open(MASTER, "rb").read()
    digest = hashlib.md5(raw).hexdigest()
    if digest != MASTER_MD5:
        sys.exit("FATAL: worklist md5 %s != pinned %s — the worklist of record moved; "
                 "re-freeze it before consolidating (rule R8)" % (digest, MASTER_MD5))

    master = list(csv.DictReader(raw.decode().splitlines()))
    if len(master) != 319:
        sys.exit("FATAL: worklist has %d rows, expected 319" % len(master))

    shard_verdict, shard_ids = {}, {}
    for name in PRECEDENCE:
        path, _ = SHARDS[name]
        shard_verdict[name], shard_ids[name] = load_shard(path)

    # Stage 1 carries 66 oracle ids that are NOT ledger rows — keep only the
    # intersection with the frozen worklist (report the rest, never emit them).
    master_ids = {r["task_id"] for r in master}
    for name in PRECEDENCE:
        shard_ids[name] = set(shard_ids[name])

    rows, conflicts = [], []
    for r in master:
        tid = r["task_id"]
        hits = [(n, shard_verdict[n][tid]) for n in PRECEDENCE if tid in shard_verdict[n]]
        verdict, shard = (hits[0][1], "s" + hits[0][0][-1]) if hits else ("UNCLASSIFIED", "none")
        if tid in CONFLICTS:
            winshard, winverdict, _ = CONFLICTS[tid]
            verdict, shard = winverdict, winshard
        if len(hits) > 1 and len({v for _, v in hits}) > 1:
            conflicts.append((tid, hits))
        rows.append({
            "task_id": tid,
            "status": r.get("status", ""),
            "class": r.get("class", ""),
            "resolved_tag": r.get("resolved", "0"),
            "shard": shard,
            "verdict": verdict,
            "recovery_task": RECOVERY.get(tid, ""),
        })

    # --- accounting -----------------------------------------------------------
    uncovered = [r["task_id"] for r in rows if r["shard"] == "none"]
    outside = sorted(set().union(*shard_ids.values()) - master_ids)
    vc = collections.Counter(r["verdict"] for r in rows)
    by_status = collections.Counter(r["status"] for r in rows)
    by_class = collections.defaultdict(collections.Counter)
    by_shard = collections.defaultdict(collections.Counter)
    for r in rows:
        by_class[r["class"]][r["verdict"]] += 1
        by_shard[r["shard"]][r["verdict"]] += 1

    print("frozen worklist md5 ok: %s (%d rows)" % (digest, len(master)))
    print("verdicts: %s  sum=%d" % (dict(vc), sum(vc.values())))
    print("status:   failed=%d cancelled=%d" % (by_status["failed"], by_status["cancelled"]))
    print("uncovered=%d  outside-worklist ids=%d" % (len(uncovered), len(outside)))
    if outside:
        extra = sorted(outside)[:3]
        print("  (expected: stage 1's oracle superset — e.g. %s …)" % ", ".join(extra))
    print("\nclass x verdict:")
    for c in CLASS_ORDER:
        print("  %-18s %3d %s" % (c, sum(by_class[c].values()), dict(by_class[c])))
    print("\nshard x verdict:")
    for s in ["s" + n[-1] for n in PRECEDENCE]:
        print("  %-4s %3d %s" % (s, sum(by_shard[s].values()), dict(by_shard[s])))
    if conflicts:
        print("\nverdict conflicts (resolved on evidence, not by precedence):")
        for tid, hits in conflicts:
            print("  %s  %s" % (tid, hits))

    # --- identity assertions (fail loud, never a silent wrong total) ----------
    errs = []
    if uncovered:
        errs.append("uncovered rows: %s" % uncovered)
    if sum(vc.values()) != len(master):
        errs.append("verdict counts %d != %d rows" % (sum(vc.values()), len(master)))
    for r in rows:
        if r["verdict"] in ("STILL-NEEDED", "PARTIALLY-DONE") and not r["recovery_task"]:
            errs.append("live row %s has no submitted successor" % r["task_id"])
    if errs:
        sys.exit("FATAL: " + "; ".join(errs))

    if args.check:
        have = open(OUT).read()
        want = to_csv(rows)
        if have != want:
            sys.exit("FATAL: %s is stale — re-run without --check" % OUT)
        print("\n%s matches the shards (byte-identical)" % OUT)
        return
    open(OUT, "w").write(to_csv(rows))
    print("\nwrote %s (%d rows)" % (OUT, len(rows)))


def to_csv(rows):
    import io
    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=["task_id", "status", "class", "resolved_tag",
                                        "shard", "verdict", "recovery_task"],
                       lineterminator="\n")
    w.writeheader()
    w.writerows(rows)
    return buf.getvalue()


if __name__ == "__main__":
    main()
