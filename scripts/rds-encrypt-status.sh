#!/bin/sh
# 生产库加密切换到哪一步了。**只读，一个字都不改。**
#
# 配 docs/RDS-ENCRYPT.md 用：切换前看基线，切换后看结果，回退时确认旧实例
# 还在不在。
#
# 最后那段迁移版本是特意查的——「加密开了」和「连上的是那份数据」是两件事。
# 一个恢复错快照的空库同样会报「加密：是」，只有迁移版本对得上才说明数据在。
set -eu

REGION="${AWS_REGION:-us-west-2}"
PROFILE_ARG=""
[ -n "${AWS_PROFILE:-}" ] && PROFILE_ARG="--profile $AWS_PROFILE"

# shellcheck disable=SC2086
aws() { command aws $PROFILE_ARG --region "$REGION" "$@"; }

echo "==================== 生产库加密状态 ===================="
echo ""
echo "---- 实例 ----"
aws rds describe-db-instances \
  --query 'DBInstances[?starts_with(DBInstanceIdentifier, `erp-pg`)].[DBInstanceIdentifier,DBInstanceStatus,StorageEncrypted,BackupRetentionPeriod,DeletionProtection,MultiAZ,Endpoint.Address]' \
  --output table 2>&1 || echo "（查不到实例）"
echo "列依次是：名字 / 状态 / 加密 / 备份保留天数 / 删除保护 / 多AZ / 端点"
echo ""
echo "  · 切换完成的样子：erp-pg 加密=True、备份=7、删除保护=True"
echo "  · 还有 erp-pg-old 是正常的——旧实例先停不删，留几天做退路"
echo ""

echo "---- 手工快照（切换用的，收尾时要清）----"
aws rds describe-db-snapshots --snapshot-type manual \
  --query 'DBSnapshots[?starts_with(DBSnapshotIdentifier, `erp-pg`)].[DBSnapshotIdentifier,Status,Encrypted,SnapshotCreateTime]' \
  --output table 2>&1 || echo "（查不到快照）"
echo ""

echo "---- 自动备份还在不在（最近三个）----"
aws rds describe-db-snapshots --snapshot-type automated \
  --query 'reverse(sort_by(DBSnapshots,&SnapshotCreateTime))[:3].[DBSnapshotIdentifier,SnapshotCreateTime,Status]' \
  --output text 2>&1 | head -3 || echo "（查不到）"
echo ""

echo "---- 连上的是不是那份数据（各服务的迁移版本）----"
echo ""
echo "在生产机上跑（本机连不到 RDS）："
echo ""
cat <<'REMOTE'
    cd /opt/erp/repo && set -a && . ./deploy/.env && set +a
    for v in IAM MASTERDATA EXPORT PROCUREMENT INVENTORY MAIL SHIPPING; do
      eval "dsn=\$${v}_DB_DSN"; [ -z "$dsn" ] && continue
      printf '%-14s' "$v"
      docker run --rm -i -e PGCONN="$dsn" postgres:16-alpine \
        sh -c 'psql -qtA "$PGCONN" -c "SELECT max(version_id) FROM goose_db_version"'
    done
REMOTE
echo ""
echo "切换前后这组数字必须**完全一样**。对不上就说明恢复的不是那份数据，"
echo "照 docs/RDS-ENCRYPT.md 的「出了问题怎么回退」把名字换回去。"
echo ""
echo "==================== 完 ===================="
