#!/bin/sh
# The bucket that replaces MinIO, plus the one identity allowed to touch it.
#
# What lives in here is the part of the system that has no second copy
# anywhere: contract scans, customers' original mail (MIME), attachments.
# Everything in this script follows from that:
#   - public access blocked four ways (a public bucket = every contract and
#     every customer letter on the open internet)
#   - versioning on (an overwrite or delete can be taken back)
#   - the app's credentials can reach THIS bucket and nothing else in the
#     account — a leaked app key must not become an account key
#
# Lifecycle: only mail/inbound-img/ expires. Those are cached copies of
# remote images from received mail — regenerable, and growing forever if
# nobody says otherwise. mail/inbound/ (originals, attachments) has no
# expiry: nothing in there can be re-created.
set -eu
. "$(dirname "$0")/00-vars.sh"
require_aws
require_bucket_name

if aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null; then
  echo "桶 $BUCKET 已存在，跳过创建"
else
  aws s3api create-bucket --bucket "$BUCKET" \
    --create-bucket-configuration LocationConstraint="$REGION"
  echo "桶 $BUCKET 已创建"
fi

aws s3api put-public-access-block --bucket "$BUCKET" \
  --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true

aws s3api put-bucket-versioning --bucket "$BUCKET" \
  --versioning-configuration Status=Enabled

aws s3api put-bucket-lifecycle-configuration --bucket "$BUCKET" \
  --lifecycle-configuration '{
    "Rules": [
      {
        "ID": "expire-cached-remote-images",
        "Filter": { "Prefix": "mail/inbound-img/" },
        "Status": "Enabled",
        "Expiration": { "Days": 180 }
      },
      {
        "ID": "drop-old-versions",
        "Filter": { "Prefix": "" },
        "Status": "Enabled",
        "NoncurrentVersionExpiration": { "NoncurrentDays": 30 }
      },
      {
        "ID": "abort-stuck-uploads",
        "Filter": { "Prefix": "" },
        "Status": "Enabled",
        "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 7 }
      }
    ]
  }'

# ---------------------------------------------------------------------- cors
# Without this, every upload in the product is broken in production and works
# perfectly in development.
#
# Files go browser-to-bucket through a presigned URL, so the gateway never
# carries a 20 MB attachment. That makes the request cross-origin — the page
# came from APP_ORIGIN, the PUT goes to s3.amazonaws.com — and a cross-origin
# PUT is not a request a browser will simply send. It asks first, with an
# OPTIONS preflight, and S3 answers 403 to every preflight until the bucket
# names the origin. The upload then fails without ever being attempted.
#
# It hid for as long as it did because MinIO, which stands in for S3 locally,
# answers preflights permissively. Nothing in development can catch this; only
# the real bucket can.
#
# GET is here as well as PUT: the reader fetches an attachment's bytes to hand
# to the spreadsheet preview, and that fetch is cross-origin too.
#
# AllowedHeaders is "*" on purpose. The credential is the signature in the
# URL; a header allowlist protects nothing here and turns every new request
# header into another production-only failure.
aws s3api put-bucket-cors --bucket "$BUCKET" \
  --cors-configuration "{
    \"CORSRules\": [
      {
        \"AllowedOrigins\": [\"$APP_ORIGIN\"],
        \"AllowedMethods\": [\"GET\", \"PUT\", \"HEAD\"],
        \"AllowedHeaders\": [\"*\"],
        \"ExposeHeaders\": [\"ETag\"],
        \"MaxAgeSeconds\": 3000
      }
    ]
  }"
echo "CORS 已设置，允许来源：$APP_ORIGIN"

# ---------------------------------------------------------------- app user
# A dedicated IAM user whose entire world is this bucket. The services speak
# the S3 API through static keys (pkg/blobstore), so this is the identity
# they will hold. ListBucket needs the bucket ARN, object ops need /*.
POLICY_NAME="$PROJECT-app-bucket"
USER_NAME="$PROJECT-app"

cat > /tmp/erp-bucket-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::$BUCKET" },
    { "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::$BUCKET/*" }
  ]
}
EOF

if aws iam get-user --user-name "$USER_NAME" >/dev/null 2>&1; then
  echo "IAM 用户 $USER_NAME 已存在，跳过创建"
else
  aws iam create-user --user-name "$USER_NAME" --tags $TAGS
fi
aws iam put-user-policy --user-name "$USER_NAME" \
  --policy-name "$POLICY_NAME" --policy-document file:///tmp/erp-bucket-policy.json
rm -f /tmp/erp-bucket-policy.json

# One key pair, generated once. If a key already exists we refuse to mint
# another rather than accumulating live keys nobody remembers.
if [ "$(aws iam list-access-keys --user-name "$USER_NAME" \
        --query 'length(AccessKeyMetadata)' --output text)" != "0" ]; then
  echo "IAM 用户 $USER_NAME 已有 access key，跳过生成（在 $SECRETS_DIR 里找，或先删旧的）"
else
  aws iam create-access-key --user-name "$USER_NAME" \
    --query 'AccessKey.{id:AccessKeyId,secret:SecretAccessKey}' --output text \
    > "$SECRETS_DIR/s3-app-key.txt"
  chmod 600 "$SECRETS_DIR/s3-app-key.txt"
  echo "应用的 S3 密钥已写入 $SECRETS_DIR/s3-app-key.txt"
  echo "它只进服务器上的 deploy/.env（MINIO_ACCESS_KEY / MINIO_SECRET_KEY），不进 git，不进聊天。"
fi

echo
echo "01-s3 完成。上线时 .env 里对应的值："
echo "  MINIO_ENDPOINT=s3.$REGION.amazonaws.com"
echo "  MINIO_PUBLIC_ENDPOINT=s3.$REGION.amazonaws.com"
echo "  MINIO_BUCKET=$BUCKET"
echo "  MINIO_USE_SSL=1"
echo "  MINIO_REGION=$REGION"
