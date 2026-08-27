#!/bin/sh
# F2 第二步动手之前，先看清两张表里到底有什么。**只读，一个字都不改。**
#
# 背景：`bank_transactions` 这张表在两个库里各有一份——
#
#   erp_export      收款对账用的，只能手工登记，对到出口合同
#   erp_procurement 银行流水用的，只能导 CSV，对到供应商付款单
#
# 要把它们并成一张，动手之前必须先回答三个问题，而这三个都只有真实数据能
# 回答：
#
#   1. 各有多少行？（几十行和几万行是两种做法）
#   2. 出口那些行上挂着多少条核销记录？——它们有外键指着行的 id，
#      **行一旦搬家，id 变了，外键就断了**。这是整件事最硬的一处。
#   3. 同一笔银行流水是不是已经在两张表里各有一份？两张表的流水号去重是
#      各管各的，所以完全可能。**这种冲突不许自动合并**，得人来看。
#
# 用法（本地）：
#     sh scripts/f2-bank-merge-report.sh
#
# 用法（生产，在能连到 RDS 的机器上）：
#     EXPORT_DSN='postgres://...:.../erp_export' \
#     PROCUREMENT_DSN='postgres://...:.../erp_procurement' \
#     sh scripts/f2-bank-merge-report.sh
set -eu

PG_PORT="${PG_PORT:-5433}"
EXPORT_DSN="${EXPORT_DSN:-postgres://erp_export:erp_export_pw@localhost:${PG_PORT}/erp_export?sslmode=disable}"
PROCUREMENT_DSN="${PROCUREMENT_DSN:-postgres://erp_procurement:erp_procurement_pw@localhost:${PG_PORT}/erp_procurement?sslmode=disable}"

# psql 不在就退回容器里的那个，和 audit-mail.sh 一样的路子。
if command -v psql >/dev/null 2>&1; then
  q_export() { psql -qtA -F'|' "$EXPORT_DSN" -c "$1"; }
  q_proc() { psql -qtA -F'|' "$PROCUREMENT_DSN" -c "$1"; }
else
  if ! command -v docker >/dev/null 2>&1; then
    echo "需要 psql 或 docker" >&2
    exit 2
  fi
  q_export() { docker exec -i erp-go-infra-postgres-1 psql -qtA -F'|' -U erp_export -d erp_export -c "$1"; }
  q_proc() { docker exec -i erp-go-infra-postgres-1 psql -qtA -F'|' -U erp_procurement -d erp_procurement -c "$1"; }
fi

echo "==================== F2 银行流水合并：动手前的体检 ===================="
echo "只读报告，不改任何数据。"
echo ""

echo "---- 1. 两张表各有多少行 ----"
echo ""
printf "出口（收款对账）  "
q_export "SELECT count(*)||' 行，涉及 '||count(DISTINCT tenant_id)||' 家公司' FROM bank_transactions"
printf "采购（银行流水）  "
q_proc "SELECT count(*)||' 行，涉及 '||count(DISTINCT tenant_id)||' 家公司' FROM bank_transactions"
echo ""

echo "---- 2. 出口那些行上挂着多少条核销记录 ----"
echo ""
echo "这些记录有外键指着行的 id。行一旦搬到另一个库，id 会变，外键就断了。"
echo "所以真正的难点不是搬行，是搬完之后这些记录还得指对地方。"
echo ""
printf "核销记录        "
q_export "SELECT count(*)||' 条（其中冲销 '||count(*) FILTER (WHERE reversal_of IS NOT NULL)||' 条）' FROM receipt_allocations"
printf "被核销过的行    "
q_export "SELECT count(DISTINCT transaction_id)||' 行' FROM receipt_allocations"
echo ""

echo "---- 3. 同一笔流水是不是两张表里各有一份 ----"
echo ""
echo "两张表的「流水号不重复」是各管各的，所以同一笔完全可能进过两次："
echo "财务导了 CSV（进采购），又在收款对账手工登记了一遍（进出口）。"
echo "**这种冲突不许自动合并**，得人来看是不是同一笔。"
echo ""
tmp_e=$(mktemp) && tmp_p=$(mktemp)
trap 'rm -f "$tmp_e" "$tmp_p"' EXIT
q_export "SELECT tenant_id||'|'||bank_ref FROM bank_transactions ORDER BY 1" > "$tmp_e"
q_proc "SELECT tenant_id||'|'||bank_ref FROM bank_transactions ORDER BY 1" > "$tmp_p"
same_ref=$(comm -12 "$tmp_e" "$tmp_p" | wc -l | tr -d ' ')
echo "按「公司 + 银行流水号」完全相同的：$same_ref 条"
if [ "$same_ref" != "0" ]; then
  echo ""
  echo "  前 20 条（公司|流水号）："
  comm -12 "$tmp_e" "$tmp_p" | head -20 | sed 's/^/    /'
  echo ""
  echo "  ⚠️  这些必须逐条人工确认。同号不一定同一笔——两家银行的流水号"
  echo "     可能撞车；而真是同一笔的话，合并时留哪一份、核销记录跟谁走，"
  echo "     都是判断，不是规则。"
fi
echo ""

echo "---- 4. 出口那些行现在的归类分布 ----"
echo ""
echo "并表时这一列要映射成新的「归属」。映射规则："
echo "  已核销 / 待处理 的进账      → CUSTOMER（客户收款）"
echo "  与应收无关 · 供应商退款      → SUPPLIER（归采购那条线，这是个升级："
echo "                                原来它是死路，现在能对到付款单上）"
echo "  与应收无关 · 出口退税        → TAX_REFUND"
echo "  与应收无关 · 其余            → OTHER + 二级分类原样带过去"
echo ""
q_export "SELECT '  '||rpad(d, 14)||rpad(t, 18)||n||' 行' FROM (
            SELECT disposition AS d,
                   coalesce(nullif(irrelevant_type, ''), '—') AS t,
                   count(*) AS n
              FROM bank_transactions
             GROUP BY disposition, irrelevant_type
          ) x ORDER BY d, t"
echo ""

echo "---- 5. 收款账户 ----"
echo ""
echo "出口那些行指着 bank_accounts。采购那张表没有这个概念，并表要把它带过去。"
printf "收款账户        "
q_export "SELECT count(*)||' 个' FROM bank_accounts"
echo ""

echo "==================== 报告完 ===================="
echo ""
echo "怎么读这份报告："
echo ""
echo "  · 第 1 项两边都是 0 或很小  → 并表几乎没有风险，直接做"
echo "  · 第 2 项不是 0             → 外键要重新指，这是主要工作量"
echo "  · 第 3 项不是 0             → **先停下来人工核对**，别让脚本猜"
