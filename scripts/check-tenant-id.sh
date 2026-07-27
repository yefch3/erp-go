#!/bin/sh
# CI guard for the multi-tenancy convention: every business table in every
# migration must carry tenant_id. Infrastructure tables that are legitimately
# tenant-free are listed in EXEMPT.
set -eu

# Single line: BSD awk rejects -v values containing newlines.
EXEMPT="outbox_events processed_events goose_db_version schema_migrations fx_rates fx_rate_latest fx_manual_overrides fx_anomalies uoms permissions"

fail=0
for f in $(find services -path '*/db/migrations/*.sql' 2>/dev/null | sort); do
  # Each CREATE TABLE block: from the CREATE line to the closing ");".
  awk -v file="$f" -v exempt="$EXEMPT" '
    BEGIN {
      n = split(exempt, arr, /[ \n]+/)
      for (i = 1; i <= n; i++) ex[arr[i]] = 1
    }
    /^[[:space:]]*CREATE[[:space:]]+TABLE/ {
      line = $0
      sub(/.*CREATE[[:space:]]+TABLE[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?/, "", line)
      sub(/[[:space:]("].*/, "", line)
      tbl = line
      intable = 1
      has = 0
      next
    }
    intable && /tenant_id/ { has = 1 }
    intable && /^[[:space:]]*\)/ {
      if (!has && !(tbl in ex)) {
        printf "MISSING tenant_id: %s (%s)\n", tbl, file
        bad = 1
      }
      intable = 0
    }
    END { exit bad }
  ' "$f" || fail=1
done

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "Every business table needs tenant_id (see docs/ARCHITECTURE.md 多租户约定)."
  echo "Genuinely tenant-free infrastructure tables go in EXEMPT in this script."
  exit 1
fi
echo "tenant_id check passed"
