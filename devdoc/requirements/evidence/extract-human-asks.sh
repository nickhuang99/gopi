#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# E1 extraction — the operator's own asks.
#
# Emits devdoc/requirements/evidence/evidence-human-asks.md: EVERY row of the
# gopi task ledger whose source='human', verbatim. Machine-generated, so the
# row count in the document is a fact read back from the database, never a
# number typed by an agent.
#
# Usage:  bash devdoc/requirements/evidence/extract-human-asks.sh [OUT_PATH]
# Env:    GOPI_LEDGER_DB  ledger path (default /home/user/.gopi/memory.db)
#
# Dependency preflight (Rule 10.7): sqlite3 (>= 3.9, for printf) and bash.
# Nothing else — no python, no awk, no jq. The ledger is opened READ-ONLY
# (file:...?mode=ro); this script never writes to it.
#
# Rule 22: writes only to OUT_PATH inside the repo; no temp files anywhere.
# Determinism: no wall-clock timestamp is emitted. The snapshot boundary is
# max(created_at) over the extracted rows, so re-running on an unchanged
# ledger reproduces the file BYTE-FOR-BYTE. Rows appended after the snapshot
# are the only legitimate difference (see the document's preamble).
# ---------------------------------------------------------------------------
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DB="${GOPI_LEDGER_DB:-$HOME/.gopi/memory.db}"
OUT="${1:-$HERE/evidence-human-asks.md}"
DSN="file:${DB}?mode=ro"

[ -r "$DB" ] || { echo "ledger not readable: $DB" >&2; exit 1; }

sql() { sqlite3 -noheader "$DSN" "$1"; }

WHERE="source='human'"
COUNT="$(sql "select count(*) from tasks where $WHERE;")"
SNAP="$(sql "select coalesce(strftime('%Y-%m-%dT%H:%M:%SZ',max(created_at),'unixepoch'),'(none)') from tasks where $WHERE;")"
SNAP_EPOCH="$(sql "select coalesce(max(created_at),0) from tasks where $WHERE;")"
FIRST="$(sql "select coalesce(strftime('%Y-%m-%dT%H:%M:%SZ',min(created_at),'unixepoch'),'(none)') from tasks where $WHERE;")"

{
cat <<HEADER
# E1 — the operator's own asks (every \`source=human\` ledger row)

**Evidence class E1.** A row the operator wrote into the gopi task ledger is the
closest thing this system has to a *user-need record*: it is a human stating
what they want built, fixed or changed, in their own words. This file is the
frozen, greppable copy of that class.

- **Source**: \`/home/user/.gopi/memory.db\`, table \`tasks\`, \`where source='human'\` (read-only)
- **Row count in this snapshot: ${COUNT}** — machine-read from the ledger; this number must equal a
  fresh \`sqlite3 /home/user/.gopi/memory.db "select count(*) from tasks where source='human'"\`
- **Count handle (use this, not a visual count)**: \`grep -c '^<!--row: ' devdoc/requirements/evidence/evidence-human-asks.md\`
- **Snapshot boundary**: \`max(created_at)\` = ${SNAP} (epoch ${SNAP_EPOCH}); first row = ${FIRST}
- **Extraction**: \`bash devdoc/requirements/evidence/extract-human-asks.sh\` (script committed beside this file)
- **Determinism**: no wall-clock time is written, so a re-run over an unchanged ledger reproduces this
  file byte-for-byte. The ONE thing a re-run may differ in: rows created after the snapshot boundary,
  plus — if such rows arrive — the \`Row count\`/\`Snapshot boundary\` lines above and the
  \`Class fingerprint\` counts below. Nothing else shifts.

**How to cite a row** (module authors): \`E1 task <task_id>\` — e.g. \`E1 task t-1788039140\` — and quote the
summary line. The task id is unique and stable; it is also the key the rest of the ledger
(\`task_events\`, tags, parent_task_id) joins on.

**Honesty note, read before using this as a requirement corpus**: this class is *not* a clean set of
user stories. It contains harness bookkeeping written into the same table (see the Class fingerprint:
rows whose summary begins \`gopi run\`), duplicate submissions of one ask, and rows whose *reason* for
being cancelled or failed was never recorded. The fingerprint section quantifies each of those, and the
companion \`evidence-index.md\` carries the full "what is NOT recorded" accounting.

---

## Class fingerprint (machine-derived)

| aspect | rows |
|---|---|
HEADER

sql "select printf('| \`total source=human rows\` | %d |', count(*)) from tasks where $WHERE;"
sql "select printf('| \`summary begins \"gopi run\" (harness bookkeeping, not an ask)\` | %d |', count(*)) from tasks where $WHERE and summary like 'gopi run%';"
sql "select printf('| \`payload/spec text present\` | %d |', count(*)) from tasks where $WHERE and trim(payload)<>'';"
sql "select printf('| \`payload/spec text ABSENT (summary only)\` | %d |', count(*)) from tasks where $WHERE and trim(payload)='';"
sql "select printf('| \`summary under 40 chars AND no payload (thinnest asks)\` | %d |', count(*)) from tasks where $WHERE and length(trim(summary))<40 and trim(payload)='';"
sql "select printf('| \`tags present\` | %d |', count(*)) from tasks where $WHERE and trim(tags)<>'';"
sql "select printf('| \`parent_task_id set (follow-up of another ask)\` | %d |', count(*)) from tasks where $WHERE and trim(parent_task_id)<>'';"
sql "select printf('| \`rows sharing a summary text with another row (duplicate submissions)\` | %d |', coalesce(sum(c),0)) from (select count(*) c from tasks where $WHERE group by summary having count(*)>1);"
sql "select printf('| \`cancelled WITH empty reason (why it was dropped: lost)\` | %d |', count(*)) from tasks where $WHERE and status='cancelled' and trim(reason)='';"
sql "select printf('| \`failed WITH empty reason (why it broke: lost)\` | %d |', count(*)) from tasks where $WHERE and status='failed' and trim(reason)='';"

cat <<'MID'

### Status distribution

| status | rows |
|---|---|
MID

sql "select printf('| \`%s\` | %d |', status, count(*)) from tasks where $WHERE group by status order by count(*) desc;"

cat <<'MID2'

---

## The rows

Format per row — the marker line is the authoritative count handle and the row
boundary (text inside a summary or payload may itself look like markdown, so
never count headings):

    <!--row: <task_id>-->
    ### <task_id>
    - created: <ISO-8601 UTC> | epoch: <created_at as stored>
    - status: <status> | tags: <tags> | parent: <parent_task_id>
    - summary:   <- the operative line
    <summary verbatim>
    - spec/payload:   <- the full spec when the operator wrote one
    <payload verbatim, or "(none recorded)">

Ordering: \`created_at\`, then \`task_id\` (deterministic; no locale-dependent sort).

MID2

sql "select printf('%s<!--row: %s-->%s### %s%s- created: %s | epoch: %s%s- status: %s | tags: %s | parent: %s%s- summary:%s%s%s%s- spec/payload:%s%s%s%s',
  char(10), task_id, char(10),
  task_id, char(10),
  strftime('%Y-%m-%dT%H:%M:%SZ', created_at, 'unixepoch'), printf('%s', created_at), char(10),
  status, tags, parent_task_id, char(10),
  char(10), summary, char(10), char(10),
  char(10), case when trim(payload)='' then '(none recorded)' else payload end, char(10), char(10))
 from tasks where $WHERE order by created_at, task_id;"

cat <<'FOOTER'

---

## End of E1

Rows in this file: **the count printed in the header at generation time** — verify with
`grep -c '^<!--row: ' devdoc/requirements/evidence/evidence-human-asks.md`.
Regenerate with `bash devdoc/requirements/evidence/extract-human-asks.sh`.
FOOTER
} > "$OUT"

echo "wrote $OUT"
echo "rows in ledger : $COUNT"
echo "rows in file   : $(grep -c '^<!--row: ' "$OUT")"
FOOTER2=""
