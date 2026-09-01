#!/bin/sh
# CI guard: 生产 compose 里的「必填变量」清单不能悄悄变长。
#
# 生产覆盖层用 ${VAR:?...} 标必填。少一个的后果不是某个服务起不来——是
# **compose 在插值阶段就失败**，早于拉镜像、早于迁库，于是整条部署停在第一
# 步，一行都没换。
#
# 而那个失败在外面看不见：healthz 照常回上一个好版本，页面一切正常。
# 2026-09-01 就是这样连着四次部署失败没人发现，直到有人去翻 Actions——
# 期间合进 main 的四个 PR 一个都没上线，两个迁移也没跑。
#
# 所以：新增一个必填变量必须同时改这份清单。改清单的那一刻，正是应该问
# 「生产的 deploy/.env 里有这个吗」的那一刻。
#
# 什么该进这份清单：**部署那台机器才知道的值**——数据库连接串、对象存储的
# 密钥、镜像 SHA。什么不该：compose 网络里的服务名（kafka:9092 这种），
# 那些在基础的 services.yml 里写死就行，标成必填只会让部署在一个本来不该
# 存在的地方失败。
set -eu

# 两个文件都要扫。生产部署跑的是这两个叠在一起，所以基础文件里的必填变量
# （INTERNAL_SIGNING_KEY 就是一个）挡的是同一件事——只盯覆盖层会漏掉它们。
FILES="deploy/docker-compose.services.yml deploy/docker-compose.prod.yml"

# 清单按字母序。加一行之前先回答：生产的 .env 里有它吗？
EXPECTED=$(cat <<'EOF'
APPROVAL_DB_DSN
ERP_SHA
EXPORT_DB_DSN
FX_DB_DSN
IAM_DB_DSN
INVENTORY_DB_DSN
INTERNAL_SIGNING_KEY
MAIL_DB_DSN
MASTERDATA_DB_DSN
MINIO_ACCESS_KEY
MINIO_BUCKET
MINIO_ENDPOINT
MINIO_PUBLIC_ENDPOINT
MINIO_SECRET_KEY
PROCUREMENT_DB_DSN
PRODUCT_DB_DSN
SHIPPING_DB_DSN
EOF
)

# 注释里也会出现 ${VAR:?} 这样的写法（文件头就有一处解释用法的），所以先把
# 整行是注释的那些去掉，只看真正生效的行。
ACTUAL=$(cat $FILES \
         | grep -v '^[[:space:]]*#' \
         | grep -oE '\$\{[A-Z_]+:\?' \
         | sed 's/^\${//; s/:?$//' \
         | sort -u)

if [ "$ACTUAL" != "$(echo "$EXPECTED" | sort -u)" ]; then
  echo "生产 compose 的必填变量清单和 $0 里记的对不上。"
  echo
  echo "只在 compose 里（新增的）："
  echo "$ACTUAL" | comm -23 - "$(echo "$EXPECTED" | sort -u > /tmp/_expected.$$; echo /tmp/_expected.$$)" | sed 's/^/  + /'
  echo "只在清单里（删掉的）："
  echo "$ACTUAL" | comm -13 - /tmp/_expected.$$ | sed 's/^/  - /'
  rm -f /tmp/_expected.$$
  echo
  echo "新增了一个必填变量的话，先确认**生产的 deploy/.env 里已经有它**，"
  echo "再把它加进这个脚本的 EXPECTED。顺序反了的话，下一次部署会停在"
  echo "compose 插值那一步，而 healthz 照常回旧版本——没人会发现。"
  echo
  echo "如果它其实是个 compose 网络里的服务名（kafka:9092 这种），"
  echo "那它根本不该是必填：在 deploy/docker-compose.services.yml 里写死。"
  exit 1
fi

echo "compose 必填变量：$(echo "$ACTUAL" | wc -l | tr -d ' ') 个，和清单一致"
