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
# 记下进场时的上一个好版本。回滚要回到它，清理要保住它——必须在
# last-good 被本次部署覆盖之前取值。
PREV_GOOD=$(cat "$LAST_GOOD" 2>/dev/null || true)
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

# ---------------------------------------------------------------- 前门漂移检查
#
# 宿主机 nginx 的「我们那一半」源头在仓库（deploy/host-nginx.conf），落点在
# /etc/nginx/snippets/erp-app.conf。这里只**比对**，不覆盖。
#
# 为什么不自动同步：nginx 是所有人的前门。部署出问题目前最多是容器起不来——上面
# 那段有健康检查和自动回滚兜着。把 nginx reload 拉进这条路，一个配置错误就能让
# 整站的 TLS 失效，而回滚逻辑管不到它。而且自动覆盖会**静默抹掉线上的紧急手改**：
# 半夜有人改配置救火，第二天一次合并就给还原了，还没人知道。
#
# 所以这里只负责让漂移变得看得见。看见之后怎么办是人的判断——多数时候是把服务器
# 上那份 diff 出来，决定它该回到仓库还是该被覆盖。
#
# 不 exit 1：配置漂移不是「这次部署失败了」，业务代码换血已经成功。把它变成部署
# 失败会让人学会忽略它，那就白做了。
NGINX_SNIPPET=/etc/nginx/snippets/erp-app.conf
if [ -f "$NGINX_SNIPPET" ]; then
  if ! cmp -s "$REPO/deploy/host-nginx.conf" "$NGINX_SNIPPET"; then
    echo "警告：前门 nginx 配置与仓库不一致" >&2
    echo "  仓库：$REPO/deploy/host-nginx.conf" >&2
    echo "  线上：$NGINX_SNIPPET" >&2
    echo "  看差异：diff $REPO/deploy/host-nginx.conf $NGINX_SNIPPET" >&2
    echo "  应用仓库那份：cp 上面两个路径，然后 nginx -t && systemctl reload nginx" >&2
  fi
else
  # 机器重建后最可能的状态：snippet 还没放上去，443 那个 server 块里的 include
  # 指向一个不存在的文件——那样 nginx 根本起不来，站点是全黑的。宁可在这里吵。
  echo "警告：$NGINX_SNIPPET 不存在。前门 nginx 可能还没按 docs/DEPLOY.md 配好" >&2
fi

# ---------------------------------------------------------------- 镜像清理
# 只在部署成功后运行，只保留两个版本：本次（在跑）和上一个好版本（回滚
# 用）。不清的后果已经量过：每次部署留下 13 个镜像，三周攒了 331 个、
# 12.5GB——盘不是今天满，但它只会朝一个方向走。
#
# 失败路径刻意不清理：一次失败的部署本来就该把现场留给人看。
# docker rmi 不加 -f：正在被容器使用的镜像删不掉，而那正是要保住的。
docker images --format '{{.Repository}}:{{.Tag}}' \
  | grep '^ghcr.io/yefch3/erp-go/' \
  | grep -vE ":($SHA|${PREV_GOOD:-none})\$" \
  | xargs -r docker rmi > /dev/null 2>&1 || true
# 悬空层（被新标签顶掉的旧层）一并回收。
docker image prune -f > /dev/null 2>&1 || true
echo "镜像清理完成，保留 $SHA 与 ${PREV_GOOD:-'(无上一版)'}"
docker ps --filter status=restarting --format '{{.Names}}' | while read -r c; do
  [ -z "$c" ] || echo "注意：$c 正在重启" >&2
done
