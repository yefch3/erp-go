#!/bin/sh
# 登进生产服务器，不用记实例 ID。
#
#   sh deploy/aws/ssh.sh              # 交互式终端（SSM 会话，不走 22 端口）
#   sh deploy/aws/ssh.sh 'df -h /'    # 带参数则只执行这一条命令并打印结果
#
# 实例按 Name 标签现查，所以机器重建、换 ID 之后这个脚本照用。
# 前置（各一次）：
#   aws configure sso                          # profile 名用 erp
#   brew install --cask session-manager-plugin # 交互式会话需要
# 令牌过期时：aws sso login --profile erp
set -eu

# 不带 --profile 跑 aws 时走的是 default 档案——那不是 ERP 的身份，得到的
# 是一句 Forbidden。这里替健忘的手补上；已显式设置过的环境不动。
export AWS_PROFILE="${AWS_PROFILE:-erp}"

. "$(dirname "$0")/00-vars.sh"
require_aws

INSTANCE_ID=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=$PROJECT-app" \
            "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)

if [ "$INSTANCE_ID" = "None" ] || [ -z "$INSTANCE_ID" ]; then
  echo "没有找到运行中的 $PROJECT-app 实例（区域 $REGION）" >&2
  exit 1
fi

if [ $# -eq 0 ]; then
  exec aws ssm start-session --target "$INSTANCE_ID"
fi

# 非交互路径：发一条命令，等结果，原样打印。与自动部署走同一条通道，
# 所以它能通本身就是部署通道的日常体检。
CMD_ID=$(aws ssm send-command \
  --instance-ids "$INSTANCE_ID" \
  --document-name AWS-RunShellScript \
  --parameters commands="$*" \
  --query Command.CommandId --output text)
aws ssm wait command-executed --command-id "$CMD_ID" --instance-id "$INSTANCE_ID" || true
aws ssm get-command-invocation --command-id "$CMD_ID" --instance-id "$INSTANCE_ID" \
  --query '{status:Status,stdout:StandardOutputContent,stderr:StandardErrorContent}' --output json
