#!/bin/sh
# Checks stored mail against the invariants in audit-mail.sql and says which
# ones the data violates.
#
# Exists because silent corruption does not report itself. The two parsing
# bugs found on 2026-08-11/12 both succeeded quietly - no error, no log line,
# nothing to alert on - and were only visible to someone who happened to open
# an affected message. At a few thousand messages a 0.3% fault rate is
# invisible; at 300 people it is a steady trickle of "the system lost my mail"
# with nothing to point at.
#
# Exits non-zero only on BLOCK findings, so it is safe to wire into CI or a
# cron without a sender's odd-but-legal mail failing the build.
set -eu

PG_PORT="${PG_PORT:-5433}"
DSN="${MAIL_DSN:-postgres://erp_mail:erp_mail_pw@localhost:${PG_PORT}/erp_mail?sslmode=disable}"
SQL="$(dirname "$0")/audit-mail.sql"

if ! command -v psql >/dev/null 2>&1; then
  # The database runs in compose; a local psql is not a requirement.
  if command -v docker >/dev/null 2>&1; then
    run() { docker exec -i erp-go-infra-postgres-1 psql -q -U erp_mail -d erp_mail -f - ; }
  else
    echo "audit-mail: 需要 psql 或 docker" >&2
    exit 2
  fi
else
  run() { psql -q "$DSN" -f - ; }
fi

out="$(run < "$SQL")" || { echo "audit-mail: 查询失败" >&2; exit 2; }

total=0
blocking=0
printf '%s\n' "邮件数据体检"
printf '%s\n' "────────────────────────────────────────────────────────────"

# severity|name|count|why
#
# Padded in awk rather than by printf: a CJK character is three bytes and two
# columns, so %-28s lines nothing up in a report that is mostly Chinese.
printf '%s\n' "$out" | LC_ALL=C awk -F'|' '
  # Display columns, computed from UTF-8 byte structure because awk here
  # counts bytes: a CJK character is three bytes and two columns wide, and its
  # two continuation bytes are what tell them apart from ASCII. Dropping them
  # from the byte count and halving gives columns = bytes - continuations/2.
  function width(s,   b, c, t) {
    b = length(s)
    t = s
    c = gsub(/[\200-\277]/, "", t)
    return b - c / 2
  }
  function pad(s, target,   p) {
    p = target - width(s)
    while (p-- > 0) s = s " "
    return s
  }
  NF > 1 {
    mark = ($3 + 0 == 0) ? "✓" : (($1 == "BLOCK") ? "✗" : "·")
    if ($3 + 0 == 0) printf "  %s  %s %s\n", mark, pad($2, 32), $3
    else             printf "  %s  %s %-6s %s\n", mark, pad($2, 32), $3, $4
  }'

# The loop above runs in a subshell, so the counts are recomputed here rather
# than carried out of it.
blocking="$(printf '%s\n' "$out" | awk -F'|' '$1=="BLOCK" && $3+0>0 {c++} END {print c+0}')"
noted="$(printf '%s\n' "$out" | awk -F'|' '$1=="NOTE" && $3+0>0 {c++} END {print c+0}')"
total="$(printf '%s\n' "$out" | awk -F'|' 'NF>1 {c++} END {print c+0}')"

printf '%s\n' "────────────────────────────────────────────────────────────"
if [ "$blocking" -gt 0 ]; then
  printf '%s\n' "$total 项检查：${blocking} 项发现问题（✗），${noted} 项值得一看（·）"
  printf '%s\n' ""
  printf '%s\n' "✗ 是我们自己的代码造成的：发件人无论怎么发都不该触发这些。"
  exit 1
fi
printf '%s\n' "$total 项检查全部通过（${noted} 项值得一看，不阻塞）"
