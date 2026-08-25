#!/bin/sh
# CI guard for the mistake this codebase has now made six times: seeding
# reference data for tenant 1 and calling it seeded.
#
#   INSERT INTO number_rules (tenant_id, ...) VALUES (1, 'QUOTATION', ...)
#
# That row is not "the default numbering rule". It is *the first company's*
# numbering rule. Every company opened afterwards has none, and finds out in
# whatever way that particular table fails:
#
#   number_rules          发邮件报「该单据类型未配置编码规则」   (#222)
#   approval_definitions  提交合同报「该单据类型未配置审批流」   (#223)
#   option_items          每一个下拉框都是空的，而且不报错
#   uoms                  一个产品都建不出来，界面上还没有新增入口
#
# The pattern that works is lazy seeding on the READ side: when the table
# answers "nothing" for a company that has never been seeded, fill in the
# defaults and answer again. It is immune to how the company was opened,
# which matters because there are now three ways to open one and there will
# be a fourth. See services/masterdata/internal/app/numbering.go for the
# reference implementation, and note what it deliberately does NOT do: it
# never refills a table somebody emptied on purpose.
#
# This is a guard, not a judge. A migration that genuinely means tenant 1 —
# a one-off correction to one company's data, say — declares it and the
# refusal becomes a decision somebody made on the record:
#
#   -- tenant-seed: 只修 tenant 1 自己的历史数据，不是给所有公司的默认值
#
# Migrations at or below the numbers below predate the guard and are left as
# history, exactly like scripts/check-iam-seeds.sh does. Raising a number
# here is not the way to pass the check.
set -eu

cutoff_for() {
  case "$1" in
    approval)    echo 6  ;;
    export)      echo 14 ;;
    fx)          echo 2  ;;
    iam)         echo 44 ;;
    inventory)   echo 6  ;;
    mail)        echo 40 ;;
    masterdata)  echo 18 ;;
    procurement) echo 24 ;;
    product)     echo 2  ;;
    shipping)    echo 10 ;;
    *)           echo 0  ;;
  esac
}

fail=0
for f in $(find services -path '*/db/migrations/*.sql' 2>/dev/null | sort); do
  svc=$(echo "$f" | cut -d/ -f2)
  base=$(basename "$f")
  num=$(echo "$base" | sed -n 's/^0*\([0-9][0-9]*\)_.*/\1/p')
  [ -n "$num" ] || continue
  # Leading zeros read as octal inside $(( )), which is why they are stripped
  # above; 10# would be simpler but CI runs this as dash, which lacks it.
  [ "$num" -gt "$(cutoff_for "$svc")" ] || continue

  # Declared? Then somebody has already reasoned about it out loud.
  if grep -qiE '^--[[:space:]]*tenant-seed:' "$f"; then
    continue
  fi

  # Only the Up section. A Down section deleting tenant 1's seed rows is
  # correct — it is undoing exactly what the Up section inserted.
  #
  # Narrow on purpose: an INSERT that NAMES tenant_id as its first column,
  # then supplies a literal 1 for it. Anything looser starts flagging
  # "VALUES (1," rows whose first column is a sort order or a level, and a
  # check that cries wolf gets an exemption added without being read.
  hits=$(awk '
    /^--[[:space:]]*\+goose[[:space:]]+Up/   { on = 1; next }
    /^--[[:space:]]*\+goose[[:space:]]+Down/ { on = 0 }
    !on { next }
    /INSERT[[:space:]]+INTO[[:space:]]+[a-zA-Z_]+[[:space:]]*\([[:space:]]*tenant_id[[:space:]]*,/ {
      pending = 1; start = NR; stmt = $0
    }
    pending && NR > start { stmt = $0 }
    pending && stmt ~ /VALUES[[:space:]]*\([[:space:]]*1[[:space:]]*,/ {
      printf "%d: %s\n", NR, $0; pending = 0; next
    }
    pending && stmt ~ /^[[:space:]]*\([[:space:]]*1[[:space:]]*,/ {
      printf "%d: %s\n", NR, $0; pending = 0; next
    }
    pending && stmt ~ /SELECT[[:space:]]+1[[:space:]]*,/ {
      printf "%d: %s\n", NR, $0; pending = 0; next
    }
    pending && stmt ~ /;[[:space:]]*$/ { pending = 0 }
  ' "$f")
  [ -n "$hits" ] || continue

  echo "只给第一家公司播种：$f"
  printf '%s\n' "$hits" | sed 's/^/    /'
  fail=1
done

if [ "$fail" -ne 0 ]; then
  cat <<'MSG'

上面这些 INSERT 把 tenant_id 写成了字面量 1。那不是「默认数据」，那是
第一家公司的数据——之后开的每一家公司都没有，而且多半是在用户点下某个
按钮时才发现的。这个错误在这个仓库里已经犯过六次。

两种正确做法，二选一：
  1. 把默认值搬到读的那一侧懒播种：查不到且这家公司从没被播过种时，补上
     默认值再答一次。参考 services/masterdata/internal/app/numbering.go。
     注意它刻意不做的那件事——被人特意清空的表不会被重新灌满。
  2. 确实只针对第一家公司（比如修某一家自己的历史数据），在迁移文件里
     写一行声明：
     -- tenant-seed: 理由，谁在什么时候确认的
MSG
  exit 1
fi

echo "tenant seed check passed"
