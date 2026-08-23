#!/bin/sh
# 一次性回填（A1 收尾）：把 owner_id=0 的存量合同需求补上合同负责人。
#
# 属主随 ContractEffective 事件进来，但事件带属主是 2026-08-22 之后的事；
# 之前拆出的需求 owner_id=0，按围栏规则只有「全部」范围能看见。本脚本
# 从 export 的合同表取 (contract_id, 负责人)，更新 procurement 的存量行。
# 幂等：只碰 source='CONTRACT' 且 owner_id=0 的行，跑几遍结果一样。
#
# 用法（两个库各给一个 DSN；机器上没有裸 psql 就用 PSQL 指到容器里的）：
#   PSQL='docker exec -i erp-go-infra-postgres-1 psql' \
#   EXPORT_DSN='postgres://erp_export:...@localhost:5432/erp_export?sslmode=disable' \
#   PROCUREMENT_DSN='postgres://erp_procurement:...@localhost:5432/erp_procurement?sslmode=disable' \
#   sh scripts/backfill-requirement-owners.sh
#
# 生产在 erp-app 主机执行，DSN 从容器环境取（照 docs/DEPLOY.md 的查库
# 套路），PSQL 指向 postgres:16-alpine 一次性容器即可。数据全程走管道，
# 不落盘到容器里。
set -eu

: "${EXPORT_DSN:?set EXPORT_DSN}"
: "${PROCUREMENT_DSN:?set PROCUREMENT_DSN}"
PSQL="${PSQL:-psql}"

pairs=$(mktemp)
trap 'rm -f "$pairs"' EXIT

# 合同负责人是 contracts 表上的快照列，转手会更新它——取当前值即可。
$PSQL "$EXPORT_DSN" -Atc \
  "COPY (SELECT tenant_id, id, sales_employee_id, sales_employee
           FROM contracts WHERE sales_employee_id <> 0)
   TO STDOUT WITH (FORMAT csv)" > "$pairs"

echo "contracts with an owner: $(wc -l < "$pairs" | tr -d ' ')"

{
  echo "CREATE TEMP TABLE _owners (tenant_id BIGINT, contract_id BIGINT, owner_id BIGINT, owner_name TEXT);"
  echo "COPY _owners FROM STDIN WITH (FORMAT csv);"
  cat "$pairs"
  echo "\\."
  cat <<'SQL'
UPDATE purchase_requirements r
   SET owner_id = o.owner_id, owner_name = o.owner_name, updated_at = now()
  FROM _owners o
 WHERE r.tenant_id = o.tenant_id AND r.contract_id = o.contract_id
   AND r.source = 'CONTRACT' AND r.owner_id = 0;
SQL
} | $PSQL "$PROCUREMENT_DSN"
