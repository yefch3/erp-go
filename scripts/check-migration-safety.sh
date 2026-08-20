#!/bin/sh
# CI guard for the one rule automatic deployment rests on: a migration must
# leave the previous version of the code still able to run.
#
# The deploy order is "migrate, then swap containers", and the health-check
# rollback restores CONTAINERS, not SCHEMA. So a migration that drops what the
# old binary still reads turns a failed deploy into an outage that rolling
# back cannot fix — the old code comes back to a database it no longer
# understands. Deployment being automatic does not create that risk, but it
# does make it arrive faster and with nobody watching.
#
# The rule this enforces: destructive changes go out in TWO releases.
#   release 1: add the new column, write both, read either
#   release 2 (after release 1 is deployed): drop the old one
#
# This is a guard, not a judge. A migration that genuinely needs a
# destructive statement declares it with a marker line, which turns the
# refusal into a decision somebody made on the record:
#
#   -- migration-safety: DROP COLUMN suppliers.legacy_code —
#   --   added-and-unread since 00031 (2026-08-05), no code references it
#
# Only checks migrations added since the last release marker would be nicer,
# but git history is not available in every context this runs in, so it reads
# every file and relies on the marker for the ones already reasoned about.
set -eu

# Statements that can break a running previous version. Kept deliberately
# short: a long list of maybe-dangerous patterns trains people to add the
# marker without reading, which costs the check its meaning.
#
# DROP COLUMN / DROP TABLE  — old code selecting it gets an error
# ALTER COLUMN ... TYPE     — old code may fail to scan the new type
# SET NOT NULL              — old code inserting without it starts failing
# RENAME                    — the old name is simply gone
PATTERN='DROP[[:space:]]+COLUMN|DROP[[:space:]]+TABLE|ALTER[[:space:]]+COLUMN[[:space:]]+[a-zA-Z_"]+[[:space:]]+TYPE|SET[[:space:]]+NOT[[:space:]]+NULL|RENAME[[:space:]]+(COLUMN|TO)'

fail=0
for f in $(find services -path '*/db/migrations/*.sql' 2>/dev/null | sort); do
  # Only the Up section: the Down section is expected to be destructive —
  # undoing is its entire job, and it never runs as part of a deploy.
  up=$(awk '/^--[[:space:]]*\+goose[[:space:]]+Up/{on=1; next} /^--[[:space:]]*\+goose[[:space:]]+Down/{on=0} on' "$f")
  hits=$(printf '%s\n' "$up" | grep -inE "$PATTERN" || true)
  [ -n "$hits" ] || continue

  # Declared? Then it has been reasoned about and said so out loud.
  if grep -qiE '^--[[:space:]]*migration-safety:' "$f"; then
    continue
  fi
  echo "UNDECLARED destructive statement: $f"
  printf '%s\n' "$hits" | sed 's/^/    /'
  fail=1
done

if [ "$fail" -ne 0 ]; then
  cat <<'MSG'

部署顺序是「先迁库、后换容器」，健康检查失败时回滚的是容器、不是库。
所以上面这些语句会让「回滚」救不回来：旧代码回到了它读不懂的库。

两种正确做法，二选一：
  1. 拆成两个版本发布——先加新列（双写、读任一），下一个版本再删旧列。
  2. 确实安全（例如那一列从加上起就没有代码读过），在迁移文件里写一行声明：
     -- migration-safety: DROP COLUMN xxx —— 理由，谁在什么时候确认的

见 docs/DEPLOY-PIPELINE.md 一（迁移与代码同一节奏）。
MSG
  exit 1
fi
# 第二道：同一个服务里不能有两个相同编号的迁移。
#
# 两个人并行开发时必然发生：各自看到最大号是 35，各自建了 36。goose 会在
# 跑到那个服务时 panic，而那已经是 CI 跑了几分钟之后的事，报错还是一句
# "duplicate version 36 detected" 夹在一堆 goroutine 栈里。
#
# 放在这里是为了让本地 `make ci` 就能拦住 —— 改个文件名的事，不该等推上去
# 才发现。
dup_found=0
for dir in services/*/db/migrations; do
  [ -d "$dir" ] || continue
  dups=$(ls "$dir" 2>/dev/null | sed -n 's/^\([0-9][0-9]*\)_.*/\1/p' | sort | uniq -d)
  [ -z "$dups" ] && continue
  dup_found=1
  for n in $dups; do
    echo "迁移编号重复：$dir 里有多个 $n" >&2
    ls "$dir" | grep "^$n" | sed 's/^/    /' >&2
  done
done
if [ "$dup_found" -ne 0 ]; then
  cat >&2 <<'DUP'

同一个服务里两个迁移用了同一个编号，goose 会直接 panic，整个服务的迁移
一条都不会跑。

通常是并行开发撞的：两个人各自看到最大号是 N，各自建了 N+1。把后合并的那个
改成下一个未被占用的编号即可 —— 迁移是按编号排序执行的，改名不影响已经
应用过的记录（goose 记的是编号，而那个编号本来就还没被应用）。
DUP
  exit 1
fi

echo "migration safety check passed"
