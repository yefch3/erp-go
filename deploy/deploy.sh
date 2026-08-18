#!/bin/sh
# 生产部署的全部动作，一份。GitHub Actions 通过 SSM 调它，人也可以直接跑：
#
#   sh /opt/erp/repo/deploy/deploy.sh <git-sha>
#
# 服务器不构建任何东西：镜像在 CI 里构建好推到 GHCR，这里只 pull。代码克隆
# 存在的唯一理由是让 compose 文件和迁移脚本与镜像来自同一个提交。
#
# 部署顺序是「先迁库、后换容器」，而健康检查失败时能回滚的是容器、不是库
# ——这就是 scripts/check-migration-safety.sh 存在的原因：迁移必须让上一个
# 版本的代码仍然跑得起来，否则回滚回到的是一个它读不懂的数据库。
set -eu

SHA="${1:?用法: deploy.sh <git-sha>}"
REPO=/opt/erp/repo
LAST_GOOD=/opt/erp/last-good
LOCK=/opt/erp/deploy.lock

# 两次部署撞在一起会让 compose 互相拆台。flock 拿不到就直接退出而不是排队：
# 排队意味着旧版本可能后到、覆盖新版本。
exec 9>"$LOCK"
if ! flock -n 9; then
  echo "另一个部署正在进行，本次退出（这是刻意的：排队会让旧版本后到）" >&2
  exit 75
fi

cd "$REPO"
git fetch --quiet origin
git checkout --quiet "$SHA"
echo "checked out $(git log --oneline -1)"

export ERP_SHA="$SHA"
COMPOSE="docker compose -f deploy/docker-compose.services.yml -f deploy/docker-compose.prod.yml"

# 先拉镜像再迁库：拉取失败（网络、标签不存在）不该留下一个已经迁过的库。
$COMPOSE pull --quiet

# 迁移镜像与业务镜像同 SHA、同仓库，迁移脚本烤在里面——「跑的迁移」和
# 「跑的代码」物理同源。DSN 从同一份 .env 取，与各服务拿到的是同一组值。
# 失败即中止：容器不更换，旧版本原样继续跑。
docker pull --quiet "ghcr.io/yefch3/erp-go/migrate:$SHA" > /dev/null
if ! docker run --rm --env-file "$REPO/deploy/.env" "ghcr.io/yefch3/erp-go/migrate:$SHA"; then
  echo "迁移失败——容器未更换，仍在跑 $(cat "$LAST_GOOD" 2>/dev/null || echo '未知版本')" >&2
  exit 1
fi

$COMPOSE up -d --remove-orphans

# 起身时间。给的是「够慢的服务也起得来」的余量，不是精确值。
sleep 30

health() {
  curl -fsS --max-time 5 http://127.0.0.1:8080/api/healthz 2>/dev/null || return 1
}

if ! health; then
  PREV=$(cat "$LAST_GOOD" 2>/dev/null || true)
  echo "健康检查未通过" >&2
  if [ -z "$PREV" ] || [ "$PREV" = "$SHA" ]; then
    echo "没有可回滚的上一个好版本，容器保持现状等待人工处理" >&2
    exit 1
  fi
  echo "回滚到 $PREV" >&2
  git checkout --quiet "$PREV"
  ERP_SHA="$PREV" $COMPOSE up -d --remove-orphans
  sleep 20
  if health; then
    echo "已回滚到 $PREV，服务恢复" >&2
  else
    echo "回滚后仍不健康，需要人工介入" >&2
  fi
  exit 1
fi

# 版本号自证：healthz 报的是容器里的 ERP_SHA，与本次部署一致才算换血成功。
RUNNING=$(health | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
if [ "$RUNNING" != "$SHA" ]; then
  echo "健康检查通过，但报告的版本是 $RUNNING，不是 $SHA" >&2
  exit 1
fi

echo "$SHA" > "$LAST_GOOD"
echo "deployed $SHA"
docker ps --filter status=restarting --format '{{.Names}}' | while read -r c; do
  [ -z "$c" ] || echo "注意：$c 正在重启" >&2
done
