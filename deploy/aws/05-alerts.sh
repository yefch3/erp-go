#!/bin/sh
# 应用层告警：日志 → 指标 → 告警。全部幂等，改了重跑即可。
#
# 日志怎么到 CloudWatch 的：deploy/docker-compose.prod.yml 里每个服务的日志
# 驱动是 awslogs，写进日志组 /erp/app，一个服务一条流。服务本来就打 JSON
# 日志（level / service / msg / event…），所以这里不解析文本，直接按字段过滤。
#
# 两类告警：
#   1. 每个 Go 服务一条：5 分钟内出现任何一条 level=ERROR 就响。兜底。
#   2. 邮件转 Excel 那条链路的几个具体失败点，按日志里固定的 event 字段响。
#      字段名定在 services/mail/internal/app/excel_jobs.go；那边有测试钉着
#      这个文件和代码两边一致，少一个、多一个都红。
#
# 告警线是「≥1 次」，不是「多于平时」：这些事一次都不该发生，发生一次就该有
# 人看。没数据当正常（notBreaching）：过滤器给了 defaultValue=0，日志组里有
# 日志但没匹配上就是 0；完全没日志则是服务全停了——那归机器那几条告警管。
#
# 看日志：aws logs tail /erp/app --follow --filter-pattern '{ $.service = "mail" }'
set -eu
export AWS_PROFILE="${AWS_PROFILE:-erp}"
. "$(dirname "$0")/00-vars.sh"
require_aws

LOG_GROUP=/erp/app
NAMESPACE=ERP/App
RETENTION_DAYS=30

SNS_ARN=$(aws sns list-topics \
  --query "Topics[?ends_with(TopicArn, ':erp-alerts')].TopicArn" --output text)
if [ -z "$SNS_ARN" ] || [ "$SNS_ARN" = "None" ]; then
  echo "找不到 SNS 主题 erp-alerts（先按 docs/MONITORING.md 第二层建好它）" >&2
  exit 1
fi

# ---------------------------------------------------------------- 日志组
if aws logs describe-log-groups --log-group-name-prefix "$LOG_GROUP" \
    --query "logGroups[?logGroupName=='$LOG_GROUP'].logGroupName" --output text \
    | grep -qx "$LOG_GROUP"; then
  echo "日志组 $LOG_GROUP 已存在"
else
  aws logs create-log-group --log-group-name "$LOG_GROUP" --tags "Project=$PROJECT"
  echo "日志组 $LOG_GROUP 已创建"
fi
# 留 30 天：排障够用，也不会越积越多。
aws logs put-retention-policy --log-group-name "$LOG_GROUP" --retention-in-days "$RETENTION_DAYS"

# ---------------------------------------------------------------- 过滤器 + 告警
# filter <过滤器名> <pattern> <指标名>
filter() {
  aws logs put-metric-filter --log-group-name "$LOG_GROUP" --filter-name "$1" \
    --filter-pattern "$2" \
    --metric-transformations "metricName=$3,metricNamespace=$NAMESPACE,metricValue=1,defaultValue=0"
}
# alarm <告警名> <指标名> <说明>
alarm() {
  aws cloudwatch put-metric-alarm --alarm-name "$1" \
    --namespace "$NAMESPACE" --metric-name "$2" --statistic Sum \
    --period 300 --evaluation-periods 1 \
    --threshold 0 --comparison-operator GreaterThanThreshold \
    --treat-missing-data notBreaching \
    --alarm-actions "$SNS_ARN" --alarm-description "$3" --tags "$TAGS"
}

# 每个 Go 服务一条兜底。frontend（nginx）和 docs（OnlyOffice）不打我们的
# JSON 日志，不在此列。
for svc in iam masterdata fx product approval export inventory procurement shipping mail gateway; do
  filter "erp-$svc-errors" "{ \$.service = \"$svc\" && \$.level = \"ERROR\" }" "Errors_$svc"
  alarm "erp-$svc-errors" "Errors_$svc" \
    "$svc 服务 5 分钟内打了 ERROR 日志。看日志组 /erp/app 里 $svc 那条流"
done

# 邮件转 Excel 的具体失败点。event 字段名见 services/mail/internal/app/excel_jobs.go。
excel() { # excel <event> <说明>
  filter "erp-mail-$1" "{ \$.service = \"mail\" && \$.event = \"$1\" }" "$1"
  alarm "erp-mail-$1" "$1" "$2"
}
excel excel_storage_unavailable "转换结果传不上对象存储，任务已标失败。对象存储此刻是坏的——附件、原件也在受影响"
excel excel_storage_unreachable "领转换任务时对象存储不通，任务留着等下一次。同上"
excel excel_row_write_failed    "转换结果传上去了、库里那一行没写成。下一次领走时会从对象存储恢复；连着出现说明数据库有问题"
excel excel_result_unreachable  "预览或下载时从对象存储取不到转换结果"
excel excel_sweep_remove_failed "清理器删不掉过期的转换结果，那一行会一直留着重试"
# 模型厂那头。这两条单独告，是因为「智能转换失败」本身什么都没说：key 失效和
# 一份读不懂的表格在兜底告警里是同一封信。
excel excel_model_account "模型厂拒了 OPENAI_API_KEY：key 失效或余额用完。全公司的「生成 Excel」此刻都转不成——去 OpenAI 后台看账单和 key"
excel excel_model_busy    "模型厂限流或出错，自动重试几次仍不行，这次转换失败了。偶尔一次可以不管；连着响说明用量碰到了账号的每分钟上限，或者对方在出故障"
# 恢复成功只记数、不告警：它是好消息（模型没重跑）。但次数多起来说明库那
# 一步经常写不成，那时该去看数据库。
filter "erp-mail-excel_result_recovered" \
  '{ $.service = "mail" && $.event = "excel_result_recovered" }' excel_result_recovered

echo "完成。现在的告警："
aws cloudwatch describe-alarms --alarm-name-prefix erp- \
  --query 'MetricAlarms[].[AlarmName,StateValue]' --output text
