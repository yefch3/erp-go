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
  # Pre-convention migrations are left as history. Pattern, not arithmetic:
  # the zero padding reads as octal in $(( )), and 10# is a bashism — CI
  # runs this script as dash, which has neither.
  case "$num" in
    0000[0-9]|0001[0-9]|0002[0-9]|0003[0-2]) continue ;;
  esac
  awk -v file="$base" '
    /INSERT[[:space:]]+INTO[[:space:]]+role_permissions/ { pending = 1; next }
    pending && /^[[:space:]]*SELECT[[:space:]]+[0-9]/ {
      printf "LITERAL tenant_id in grant: %s: %s\n", file, $0
      bad = 1
    }
    pending { pending = 0 }
    END { exit bad }
  ' "$f" || fail=1

  # 第二道：列清单里干脆没写 tenant_id。上面那道只认字面量 `SELECT 1`，
  # 而 00055 写的是 `INSERT INTO role_permissions (role_id, permission_id)`
  # ——没有字面量，列也没给，数据库按默认值填成 1。三家公司的销售因此少了
  # 两条权限（2026-09-19 查出来的）。00081 之后库里有复合外键兜底，这里
  # 是让人在提交前就看见，而不是在 CI 跑迁移时撞外键。
  # 00055 就是那次事故本身，已由 00081 改回；留作历史，不再报。
  [ "$num" = "00055" ] && continue
  awk -v file="$base" '
    /INSERT[[:space:]]+INTO[[:space:]]+(role_permissions|employee_roles|role_data_scopes)[[:space:]]*\(/ {
      cols = $0
      # 列清单可能跨行：一直读到右括号为止。
      while (cols !~ /\)/ && (getline line) > 0) cols = cols line
      if (cols !~ /tenant_id/) {
        printf "MISSING tenant_id in column list: %s: %s\n", file, $0
        bad = 1
      }
    }
    END { exit bad }
  ' "$f" || fail=1
done

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "Grants into role_permissions must read SELECT r.tenant_id, r.id, p.id —"
  echo "the roles table is per tenant, a literal tenant id grants the wrong tenant."
  echo "And the column list must name tenant_id: left out, it defaults to 1 and"
  echo "every other tenant's row is filed under the first company (see 00055/00081)."
  exit 1
fi
echo "iam seed check passed"
