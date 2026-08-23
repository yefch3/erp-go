#!/bin/sh
# CI guard for the iam seeding convention: a grant into role_permissions must
# follow the role's own tenant (SELECT r.tenant_id, ...), never a hardcoded
# literal (SELECT 1, ...). The roles table holds one SUPER_ADMIN per tenant —
# a literal tenant id hands every tenant's grant to tenant 1, and every
# tenant bootstrapped before the migration silently misses the permission.
#
# The convention starts at 00033: earlier migrations wrote the literal too,
# but they predate any second tenant and are left as history. This script
# exists because the mistake came back four times after the convention was
# set (00036/00038/00039/00040) — review alone did not stop it.
set -eu

fail=0
for f in services/iam/db/migrations/*.sql; do
  base=$(basename "$f")
  num=${base%%_*}
  # 10# forces decimal: 00008 would otherwise read as octal.
  [ "$((10#$num))" -ge 33 ] || continue
  awk -v file="$base" '
    /INSERT[[:space:]]+INTO[[:space:]]+role_permissions/ { pending = 1; next }
    pending && /^[[:space:]]*SELECT[[:space:]]+[0-9]/ {
      printf "LITERAL tenant_id in grant: %s: %s\n", file, $0
      bad = 1
    }
    pending { pending = 0 }
    END { exit bad }
  ' "$f" || fail=1
done

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "Grants into role_permissions must read SELECT r.tenant_id, r.id, p.id —"
  echo "the roles table is per tenant, a literal tenant id grants the wrong tenant."
  exit 1
fi
echo "iam seed check passed"
