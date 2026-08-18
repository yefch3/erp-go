#!/bin/sh
# 把每个服务的迁移跑到最新，DSN 从环境变量取（IAM_DB_DSN、MAIL_DB_DSN……），
# 与 docker-compose.prod.yml 注入给各服务的是同一组值。
#
# 一个失败即整体失败：部署脚本据此中止，不去更换容器。
set -eu

failed=0
for dir in /migrations/*; do
  [ -d "$dir" ] || continue
  svc=$(basename "$dir")
  var="$(echo "$svc" | tr '[:lower:]' '[:upper:]')_DB_DSN"
  eval "dsn=\${$var:-}"
  if [ -z "$dsn" ]; then
    # 没有 DSN 的服务要么没有库，要么部署配置漏了它——两者都值得看见，
    # 但都不该让部署失败：漏配的那个服务自己会起不来并说明原因。
    echo "跳过 $svc（未设置 $var）"
    continue
  fi
  echo "==> $svc"
  if ! goose -dir "$dir" postgres "$dsn" up; then
    echo "迁移失败：$svc" >&2
    failed=1
    break
  fi
done

exit "$failed"
