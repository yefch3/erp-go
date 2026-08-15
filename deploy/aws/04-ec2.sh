#!/bin/sh
# The one machine, and what was decided by NOT putting things on it.
#
# Nothing irreplaceable lives here. Business data is in RDS, files are in
# S3, Kafka replays from the outbox tables, Redis rebuilds itself. So the
# recovery story for "the machine is gone" is: run this script again, point
# DNS at the new address, done. That property is worth more than any amount
# of in-place hardening, and every choice below protects it:
#
#   - no SSH port. Shell access goes through SSM Session Manager (the IAM
#     role below is what enables it): nothing listens, so nothing is
#     scanned, brute-forced or locked out. `make nmap` day should see 443
#     and silence.
#   - IMDSv2 required: the instance's own credential endpoint cannot be
#     reached by the SSRF one-liner that works on v1.
#   - Ubuntu LTS resolved through AWS's public SSM parameter, so a rebuild
#     next year gets next year's patched image, not a stale AMI id pinned
#     in this file.
#
# The elastic IP is the machine's one piece of persistent identity - DNS
# points at it, and a rebuilt machine takes it over.
set -eu
. "$(dirname "$0")/00-vars.sh"
require_aws

ROLE_NAME="$PROJECT-ec2"
INSTANCE_NAME="$PROJECT-app"

APP_SG=$(aws ec2 describe-security-groups \
  --filters Name=group-name,Values="$PROJECT-app" \
  --query 'SecurityGroups[0].GroupId' --output text)
[ "$APP_SG" != "None" ] || { echo "先跑 02-network.sh" >&2; exit 1; }

# ---------------------------------------------------------------- IAM role
# The role grants exactly one thing: being manageable through SSM. The app
# itself does not use instance credentials — S3 access rides the scoped
# static key from 01-s3.sh, which keeps "what the app can do" independent
# of "what the machine can do".
if ! aws iam get-role --role-name "$ROLE_NAME" >/dev/null 2>&1; then
  aws iam create-role --role-name "$ROLE_NAME" \
    --assume-role-policy-document '{
      "Version": "2012-10-17",
      "Statement": [{ "Effect": "Allow",
        "Principal": { "Service": "ec2.amazonaws.com" },
        "Action": "sts:AssumeRole" }]
    }' --tags $TAGS >/dev/null
  aws iam attach-role-policy --role-name "$ROLE_NAME" \
    --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
  aws iam create-instance-profile --instance-profile-name "$ROLE_NAME" >/dev/null
  aws iam add-role-to-instance-profile \
    --instance-profile-name "$ROLE_NAME" --role-name "$ROLE_NAME"
  echo "IAM 角色 $ROLE_NAME 已建（等 10 秒让它可用）"
  sleep 10
else
  echo "IAM 角色 $ROLE_NAME 已存在"
fi

# ---------------------------------------------------------------- instance
EXISTING=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=$INSTANCE_NAME" \
            "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)
if [ "$EXISTING" != "None" ]; then
  echo "实例 $INSTANCE_NAME 已存在（$EXISTING），跳过创建"
  INSTANCE_ID=$EXISTING
else
  AMI=$(aws ssm get-parameter \
    --name /aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id \
    --query 'Parameter.Value' --output text)

  # Docker and compose from Ubuntu's own repo: pinned by the distro,
  # patched by unattended-upgrades, no curl-pipe-sh. systemd-timesyncd is
  # on by default, which the 2-minute inter-service signature window
  # quietly depends on (docs/DEPLOY.md 三).
  cat > /tmp/erp-user-data.sh <<'EOF'
#!/bin/sh
set -eu
apt-get update
apt-get install -y docker.io docker-compose-v2
usermod -aG docker ubuntu
systemctl enable --now docker
EOF

  INSTANCE_ID=$(aws ec2 run-instances \
    --image-id "$AMI" \
    --instance-type "$EC2_TYPE" \
    --security-group-ids "$APP_SG" \
    --iam-instance-profile "Name=$ROLE_NAME" \
    --metadata-options HttpTokens=required \
    --block-device-mappings "DeviceName=/dev/sda1,Ebs={VolumeSize=$ROOT_VOL_GB,VolumeType=gp3,DeleteOnTermination=true}" \
    --user-data file:///tmp/erp-user-data.sh \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$INSTANCE_NAME},{$TAGS}]" \
    --count 1 \
    --query 'Instances[0].InstanceId' --output text)
  rm -f /tmp/erp-user-data.sh
  echo "实例创建中：$INSTANCE_ID"
fi

aws ec2 wait instance-running --instance-ids "$INSTANCE_ID"

# ---------------------------------------------------------------- elastic IP
ALLOC=$(aws ec2 describe-addresses \
  --filters "Name=tag:Name,Values=$INSTANCE_NAME" \
  --query 'Addresses[0].AllocationId' --output text)
if [ "$ALLOC" = "None" ]; then
  ALLOC=$(aws ec2 allocate-address \
    --tag-specifications "ResourceType=elastic-ip,Tags=[{Key=Name,Value=$INSTANCE_NAME},{$TAGS}]" \
    --query AllocationId --output text)
fi
aws ec2 associate-address --allocation-id "$ALLOC" --instance-id "$INSTANCE_ID" >/dev/null
PUBLIC_IP=$(aws ec2 describe-addresses --allocation-ids "$ALLOC" \
  --query 'Addresses[0].PublicIp' --output text)

echo
echo "04-ec2 完成。"
echo "  实例:  $INSTANCE_ID"
echo "  公网:  $PUBLIC_IP   ← DNS 的 A 记录指到这里"
echo
echo "登录（不走 SSH，不开 22）："
echo "  aws ssm start-session --target $INSTANCE_ID"
echo
echo "从本机连 RDS 调试（同一条通道做端口转发）："
echo "  aws ssm start-session --target $INSTANCE_ID \\"
echo "    --document-name AWS-StartPortForwardingSessionToRemoteHost \\"
echo "    --parameters host=<RDS endpoint>,portNumber=5432,localPortNumber=5433"
