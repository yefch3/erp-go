#!/bin/sh
# The database. This is the one component bought managed, and the reason is
# a single sentence: everything else failing costs downtime, the database
# failing costs the customer's data, and there is no second copy of an ERP.
#
# What "managed" is buying here, concretely:
#   - automated daily backups + transaction logs → restore to any point in
#     the last 7 days with ~5-minute granularity (RPO ≈ 5 min)
#   - a hardware failure is AWS's problem to swap out, not an evening of ours
#   - deletion protection: a fat-fingered delete in console or script bounces
#
# Deliberately NOT bought (pilot trade-offs, each reversible later):
#   - Multi-AZ (~2x cost). Failover automation matters when downtime is
#     measured in money; for a 300-person pilot it is measured in patience.
#   - Encryption keys of our own (default AWS-managed encryption is on).
#
# The per-service ownership model (deploy/init-databases.sh) runs unchanged
# on RDS: the master user created here plays the role the postgres superuser
# plays locally — it creates the 12 databases and 12 owners, then the
# services never see it again. Its password is generated below, lands in
# ./secrets/, and is needed for exactly two things: init-databases.sh and
# emergencies.
set -eu
. "$(dirname "$0")/00-vars.sh"
require_aws

DB_ID="$PROJECT-pg"
MASTER_USER=erp_admin

DB_SG=$(aws ec2 describe-security-groups \
  --filters Name=group-name,Values="$PROJECT-db" \
  --query 'SecurityGroups[0].GroupId' --output text)
[ "$DB_SG" != "None" ] || { echo "先跑 02-network.sh" >&2; exit 1; }

if aws rds describe-db-instances --db-instance-identifier "$DB_ID" >/dev/null 2>&1; then
  echo "RDS 实例 $DB_ID 已存在，跳过创建"
else
  # hex 而不是 base64：RDS 禁止密码里出现 / @ " 和空格，hex 永远不含。
  MASTER_PASSWORD=$(openssl rand -hex 24)
  umask 077
  printf '%s\n' "$MASTER_PASSWORD" > "$SECRETS_DIR/rds-master-password.txt"
  echo "master 密码已写入 $SECRETS_DIR/rds-master-password.txt（用户名 $MASTER_USER）"

  aws rds create-db-instance \
    --db-instance-identifier "$DB_ID" \
    --engine postgres \
    --engine-version "$RDS_ENGINE_VERSION" \
    --db-instance-class "$RDS_CLASS" \
    --allocated-storage "$RDS_STORAGE_GB" \
    --storage-type gp3 \
    --master-username "$MASTER_USER" \
    --master-user-password "$MASTER_PASSWORD" \
    --vpc-security-group-ids "$DB_SG" \
    --backup-retention-period 7 \
    --no-publicly-accessible \
    --deletion-protection \
    --auto-minor-version-upgrade \
    --tags $TAGS >/dev/null
  echo "RDS 实例创建中——大约要 10 分钟，下面会等它就绪。"
fi

aws rds wait db-instance-available --db-instance-identifier "$DB_ID"

ENDPOINT=$(aws rds describe-db-instances --db-instance-identifier "$DB_ID" \
  --query 'DBInstances[0].Endpoint.Address' --output text)

echo
echo "03-rds 完成。"
echo "  endpoint: $ENDPOINT"
echo
echo "它不对公网开放，也不该开放。搬迁那天从 EC2 上（或经 SSM 端口转发）执行："
echo "  POSTGRES_USER=$MASTER_USER PGPASSWORD=\$(cat secrets/rds-master-password.txt) \\"
echo "  PGHOST=$ENDPOINT PGPORT=5432 sh deploy/init-databases.sh"
