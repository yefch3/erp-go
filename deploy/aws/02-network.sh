#!/bin/sh
# Two security groups, and the reason there are exactly two.
#
#   erp-app  挂在 EC2 上：允许全世界连 443，其余什么都不开。
#            没有 22——登录服务器走 SSM Session Manager（04 里配），
#            不存在的端口不需要防守。
#   erp-db   挂在 RDS 上：只允许「持有 erp-app 组的机器」连 5432。
#
# 第二条的写法是这份文件存在的意义：规则引用的是安全组而不是 IP 地址。
# 机器哪天重建、换了 IP，规则照样成立；反过来，任何不持有 erp-app 组的
# 东西——包括这个账号里以后新开的其他机器——天然连不上数据库。
# 这和 init-databases.sh 里 REVOKE PUBLIC 是同一个思路的两层：
# 网络层「谁能碰端口」，数据库层「碰到端口的人各自能进哪个库」。
set -eu
. "$(dirname "$0")/00-vars.sh"
require_aws

VPC_ID=$(aws ec2 describe-vpcs --filters Name=is-default,Values=true \
  --query 'Vpcs[0].VpcId' --output text)
[ "$VPC_ID" != "None" ] || { echo "没有默认 VPC（新账号不该出现这情况）" >&2; exit 1; }

get_or_create_sg() {
  name=$1; desc=$2
  sg=$(aws ec2 describe-security-groups \
    --filters Name=vpc-id,Values="$VPC_ID" Name=group-name,Values="$name" \
    --query 'SecurityGroups[0].GroupId' --output text)
  if [ "$sg" = "None" ]; then
    sg=$(aws ec2 create-security-group --vpc-id "$VPC_ID" \
      --group-name "$name" --description "$desc" \
      --tag-specifications "ResourceType=security-group,Tags=[{$TAGS}]" \
      --query GroupId --output text)
    echo "创建 $name = $sg" >&2
  else
    echo "$name 已存在 = $sg" >&2
  fi
  echo "$sg"
}

APP_SG=$(get_or_create_sg "$PROJECT-app" "ERP app host: 443 only, shell via SSM")
DB_SG=$(get_or_create_sg "$PROJECT-db" "ERP RDS: 5432 from the app SG only")

# authorize-security-group-ingress 对已存在的规则报错，|| true 让脚本可重跑。
aws ec2 authorize-security-group-ingress --group-id "$APP_SG" \
  --protocol tcp --port 443 --cidr 0.0.0.0/0 2>/dev/null \
  && echo "erp-app: 443 开放" || echo "erp-app: 443 已在"

aws ec2 authorize-security-group-ingress --group-id "$DB_SG" \
  --protocol tcp --port 5432 --source-group "$APP_SG" 2>/dev/null \
  && echo "erp-db: 5432 只对 erp-app 开放" || echo "erp-db: 规则已在"

echo
echo "02-network 完成。APP_SG=$APP_SG DB_SG=$DB_SG"
