#!/usr/bin/env bash
# Latent provenance harvest — re-runnable operator script (Rule 16 follow-up).
#
# WHAT IT DOES: records the DEFENSIBLE origin of knowledge rows whose free-text
# `sources` records none, into three ADDITIVE columns
# (source_type_latent / provenance / provenance_evidence). It never touches
# `sources`, `source_type`, topic, question, answer or search_kw — proven by a
# fingerprint of those columns taken before and after.
#
# WHY IT IS SAFE TO RE-RUN: the write is anchored on the latent columns being
# unset, so a second pass writes 0 rows. This script RUNS IT TWICE and fails
# loudly if the second pass writes anything — the idempotency claim is asserted,
# not assumed.
#
# WHY IT HARVESTS SO FEW ROWS: only a row whose TOPIC declares a file-shaped
# path that its OWN question/answer repeats is harvested (measured: 90 of 502
# empty-sources rows; 100% of those paths exist on disk, versus 65% of the
# RECORDED paths). A row that merely MENTIONS a path is left unlabelled with an
# explicit 'none' — inventing an origin is the failure this work prevents.
#
# USAGE:
#   scripts/latent-provenance-harvest.sh [--db PATH] [--bin PATH] [--dry-run]
#
#   --db PATH    knowledge base to operate on   (default: $HOME/.gopi/memory.db)
#   --bin PATH   gopi binary                    (default: ./bin/gopi, then $PATH)
#   --dry-run    report the candidate population only; write nothing

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants (Rule 1: no magic numbers / bare string keywords).
# ---------------------------------------------------------------------------
readonly DEFAULT_DB="${HOME}/.gopi/memory.db"
readonly DEFAULT_BIN_PRIMARY="./bin/gopi"
readonly BACKUP_SUFFIX=".bak-latent-provenance"
readonly PROV_SUBCOMMAND="provenance"
readonly MODE_CANDIDATES="--latent-candidates"
readonly MODE_HARVEST="--harvest-latent"
readonly MODE_CROSSCHECK="--latent-crosscheck"
readonly MODE_CALIBRATE="--latent-calibrate"
readonly MODE_COUNTS="--counts"
# The three protected-provenance columns + the text columns the harvest must
# never move. Fingerprinted before/after; a difference aborts the run.
readonly PROTECTED_EXPR="id||'|'||hex(topic)||hex(question)||hex(answer)||coalesce(sources,'')||coalesce(search_kw,'')||coalesce(source_type,'')"

db="${DEFAULT_DB}"
bin="${DEFAULT_BIN_PRIMARY}"
dry_run=0

while [ $# -gt 0 ]; do
	case "$1" in
	--db) db="$2"; shift 2 ;;
	--bin) bin="$2"; shift 2 ;;
	--dry-run) dry_run=1; shift ;;
	-h | --help)
		sed -n '2,25p' "$0"
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 2
		;;
	esac
done

if [ ! -x "${bin}" ]; then
	bin="$(command -v gopi || true)"
fi
if [ -z "${bin}" ] || [ ! -x "${bin}" ]; then
	echo "gopi binary not found — build it (CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/) or pass --bin" >&2
	exit 2
fi
if [ ! -f "${db}" ]; then
	echo "knowledge base not found: ${db}" >&2
	exit 2
fi

command -v sqlite3 >/dev/null || {
	echo "sqlite3 is required for the fingerprints" >&2
	exit 2
}

# fingerprint = one hash over every protected byte, for every row.
fingerprint() {
	sqlite3 -readonly "${db}" "select ${PROTECTED_EXPR} from distilled_qa" | sha256sum | cut -d' ' -f1
}

echo "== knowledge base: ${db}"
echo "== binary:        ${bin}"

# ---------------------------------------------------------------------------
# 0. Measure first — never write before the population is on the record.
# ---------------------------------------------------------------------------
echo
echo "== calibration (the rule's justification: harvested paths vs RECORDED paths)"
"${bin}" memory "${PROV_SUBCOMMAND}" "${MODE_CALIBRATE}" --db "${db}"

echo
echo "== candidate population (empty sources + a declared file-shaped heading)"
"${bin}" memory "${PROV_SUBCOMMAND}" "${MODE_CANDIDATES}" --db "${db}"

if [ "${dry_run}" -eq 1 ]; then
	echo
	echo "== dry run: nothing written"
	exit 0
fi

# ---------------------------------------------------------------------------
# 1. Back up BEFORE any write. The DB is production — agents read it live.
# ---------------------------------------------------------------------------
readonly_stamp="$(date +%Y%m%d-%H%M%S)"
backup="${db}${BACKUP_SUFFIX}-${readonly_stamp}"
cp --preserve=mode,timestamps "${db}" "${backup}"
echo
echo "== backup: ${backup}"
echo "   sha256 ${db}       $(sha256sum "${db}" | cut -d' ' -f1)"
echo "   sha256 ${backup}   $(sha256sum "${backup}" | cut -d' ' -f1)"

before="$(fingerprint)"
echo "== protected-column fingerprint before: ${before}"

# ---------------------------------------------------------------------------
# 2. Harvest, then harvest AGAIN — the second pass must write 0 rows.
# ---------------------------------------------------------------------------
echo
echo "== pass 1"
first="$("${bin}" memory "${PROV_SUBCOMMAND}" "${MODE_HARVEST}" --db "${db}")"
echo "${first}"

echo
echo "== pass 2 (idempotency proof: 'rows written this pass=0')"
second="$("${bin}" memory "${PROV_SUBCOMMAND}" "${MODE_HARVEST}" --db "${db}")"
echo "${second}"

if ! grep -q "rows written this pass=0" <<<"${second}"; then
	echo
	echo "IDEMPOTENCY FAILED — the second pass wrote rows. Restoring ${db} from ${backup}" >&2
	cp "${backup}" "${db}"
	exit 1
fi

# ---------------------------------------------------------------------------
# 3. Additive proof + the honest split.
# ---------------------------------------------------------------------------
after="$(fingerprint)"
echo
echo "== protected-column fingerprint after:  ${after}"
if [ "${before}" != "${after}" ]; then
	echo "ADDITIVITY FAILED — a protected column changed. Restoring ${db} from ${backup}" >&2
	cp "${backup}" "${db}"
	exit 1
fi
echo "   unchanged ✔ (sources / source_type / topic / question / answer / search_kw)"

echo
echo "== verdict distribution"
sqlite3 -readonly "${db}" \
	"select provenance, case when source_type_latent='' then '(none)' else source_type_latent end, count(*)
	   from distilled_qa group by 1,2 order by 3 desc;"

echo
echo "== the rows whose origin is now recoverable (manual cross-check sample)"
"${bin}" memory "${PROV_SUBCOMMAND}" "${MODE_CROSSCHECK}" --db "${db}"

echo
echo "== persisted source_type counts (the recorded-origin view, unchanged)"
"${bin}" memory "${PROV_SUBCOMMAND}" "${MODE_COUNTS}" --db "${db}"

echo
echo "== done. backup kept at ${backup}"
