#!/bin/sh
# CI guard for the multi-tenancy convention: every business table in every
# migration must carry tenant_id. Infrastructure tables that are legitimately
# tenant-free are listed in EXEMPT.
set -eu

# Single line: BSD awk rejects -v values containing newlines.
# tenants is the registry of tenants: a tenant_id on it would name itself.
EXEMPT="outbox_events processed_events goose_db_version schema_migrations fx_rates fx_rate_latest fx_manual_overrides fx_anomalies uoms permissions tenants platform_operators"

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

# Old migrations used DEFAULT 1 before the system became multi-tenant. Every
# service now has a require_explicit_tenant migration which removes those
# defaults from an upgraded database. From that marker onward, allowing any
# tenant_id default would recreate the same silent cross-tenant attribution.
for dir in services/*/db/migrations; do
  [ -d "$dir" ] || continue
  marker=$(find "$dir" -maxdepth 1 -name '*_require_explicit_tenant.sql' | sort | tail -n 1)
  if [ -z "$marker" ]; then
    echo "MISSING explicit-tenant migration: $dir"
    fail=1
    continue
  fi
  if ! grep -Eq 'CHECK[[:space:]]*\([[:space:]]*tenant_id[[:space:]]*>[[:space:]]*0[[:space:]]*\)' "$marker"; then
    echo "MISSING positive tenant_id constraint in: $marker"
    fail=1
  fi
  enforce=0
  for f in $(find "$dir" -maxdepth 1 -name '*.sql' | sort); do
    if [ "$f" = "$marker" ]; then
      enforce=1
      continue
    fi
    [ "$enforce" -eq 1 ] || continue
    if awk 'tolower($0) ~ /tenant_id/ && tolower($0) ~ /default/ { found=1 } END { exit(found ? 0 : 1) }' "$f"; then
      echo "FORBIDDEN tenant_id default after explicit-tenant migration: $f"
      fail=1
    fi
  done
done

for f in pkg/outbox/outbox.go pkg/deadletter/deadletter.go; do
  if awk 'tolower($0) ~ /tenant_id/ && tolower($0) ~ /default[[:space:]]+1/ { found=1 } END { exit(found ? 0 : 1) }' "$f"; then
    echo "FORBIDDEN tenant_id default in canonical DDL: $f"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "tenant_id must always be supplied explicitly; it has no default tenant."
  exit 1
fi
echo "tenant_id check passed"
