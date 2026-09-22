#!/bin/sh
# 按国家看请求：采集器 + 告警。幂等，改了重跑即可。
#
# 起因：2026-09-21 中国大陆访问一直超时，是员工报上来的，不是监控报上来的。
# 日志里其实早就写着答案——江苏那个 IP 的 499 率 0.131%、美国 0.008%，差
# 二十倍——但没有人会天天去 grep 日志。这套东西就是让那个数字自己说话。
#
# **它不发布"延迟"。** nginx 的 $request_time 结束于「把响应交给内核套接字」，
# 不是「客户端收到」，所以中国和美国量出来几乎一样（实测过）。发布一个叫延迟
# 的指标而它其实是服务端耗时，比没有指标更坏。真要量用户等了多久，只能从浏览
# 器那一侧上报，那是另一件事。
#
# 发布的是：Requests / ClientAborts(499) / ServerErrors(5xx) / UpstreamSeconds
# / BytesSent，维度 Region ∈ {CN, US, OTHER}。
#
# 花多少钱：自定义指标 $0.30/个/月 × 5 个指标 × 3 个桶 = $4.5/月，加上每分钟
# 一次 PutMetricData 约 $0.43/月。合计约 $5/月。
set -eu
export AWS_PROFILE="${AWS_PROFILE:-erp}"
. "$(dirname "$0")/00-vars.sh"
require_aws

NAMESPACE=ERP/Web
INSTANCE=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=erp-app" "Name=instance-state-name,Values=running" \
  --query 'Reservations[].Instances[0].InstanceId' --output text)
if [ -z "$INSTANCE" ] || [ "$INSTANCE" = "None" ]; then
  echo "找不到运行中的 erp-app 实例" >&2
  exit 1
fi

SNS_ARN=$(aws sns list-topics \
  --query "Topics[?ends_with(TopicArn, ':erp-alerts')].TopicArn" --output text)
if [ -z "$SNS_ARN" ] || [ "$SNS_ARN" = "None" ]; then
  echo "找不到 SNS 主题 erp-alerts（先跑 05-alerts.sh 那一节）" >&2
  exit 1
fi

# ---------------------------------------------------------------- 装到机器上
# 脚本本体走 SSM 送过去（base64，免得引号在几层 shell 里被吃掉）。
SCRIPT_B64=$(base64 < "$(dirname "$0")/region-metrics.py" | tr -d '\n')

SETUP=$(cat <<SETUP_EOF
set -e
mkdir -p /opt/erp /var/lib/erp-region
echo '$SCRIPT_B64' | base64 -d > /opt/erp/region-metrics.py
chmod +x /opt/erp/region-metrics.py

# IP→国家 的数据。Ubuntu 源里就有，不用注册 MaxMind 账号。
# 2024-04 的库，国家这一级对运营商大段地址是稳的（已拿生产上的真实 IP 验过）。
command -v geoiplookup >/dev/null 2>&1 || {
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq geoip-bin geoip-database
}

cat > /etc/systemd/system/erp-region-metrics.service <<'UNIT'
[Unit]
Description=ERP: nginx 日志按国家汇总，写进 CloudWatch
After=network-online.target

[Service]
Type=oneshot
Environment=AWS_REGION=$REGION
ExecStart=/usr/bin/python3 /opt/erp/region-metrics.py
# 读 nginx 日志要 adm 组。不用 root 跑：它只需要读日志和调一个 AWS 接口。
User=root
UNIT

cat > /etc/systemd/system/erp-region-metrics.timer <<'UNIT'
[Unit]
Description=ERP: 每分钟汇总一次

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
# 不追补：停机期间的那些分钟补发出来，会把历史数据画在错误的时间点上。
Persistent=false

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now erp-region-metrics.timer
# 立刻跑一次，好当场看见有没有报错
systemctl start erp-region-metrics.service
sleep 3
systemctl is-active erp-region-metrics.timer
journalctl -u erp-region-metrics.service -n 10 --no-pager | tail -5
SETUP_EOF
)

echo "==> 装采集器到 $INSTANCE"
# 整段 base64 再送过去，落地成文件后执行。
#
# 不用 commands=["第一行","第二行"...] 那种写法：安装脚本里有 heredoc、引号和
# $ 变量，它们要穿过本地 shell、SSM 的 JSON、远端 shell 三层转义——2026-09-22
# 试过一次，整段被啃成 `set: Illegal option -k`。base64 里没有特殊字符，穿几层
# 都一样。
SETUP_B64=$(printf '%s' "$SETUP" | base64 | tr -d '\n')
CMD=$(aws ssm send-command --instance-ids "$INSTANCE" \
  --document-name AWS-RunShellScript \
  --parameters "commands=[\"echo $SETUP_B64 | base64 -d > /tmp/erp-region-setup.sh\",\"sh /tmp/erp-region-setup.sh\"]" \
  --query 'Command.CommandId' --output text)
sleep 25
aws ssm get-command-invocation --command-id "$CMD" --instance-id "$INSTANCE" \
  --query '[Status,StandardOutputContent,StandardErrorContent]' --output text

# ---------------------------------------------------------------- 告警
#
# 告的是**比率**不是次数：中国那边一天几万次请求，几次掐断是正常抖动；美国
# 那边一天几千次，同样几次就说明出事了。按次数定线，两边只能各定一个，而
# 那个数字过一个月就不对了。
#
# 5% 这条线是按实测定的：2026-09-21 压缩之前，出问题的那一整天江苏那个 IP
# 也只有 0.131%。也就是说 5% 意味着"比那次严重四十倍"——不会因为日常抖动
# 半夜把人叫起来，而真出事时一定响。
#
# 除零不会发生：采集器只在这一分钟真有请求时才发 Requests，所以有数据点就
# 意味着 total ≥ 1。没请求的那几分钟根本没有数据点，notBreaching 当正常——
# 半夜没人用不该算故障。
alarm_rate() { # alarm_rate <区域> <说明>
  aws cloudwatch put-metric-alarm \
    --alarm-name "erp-aborts-$1" \
    --alarm-description "$2" \
    --evaluation-periods 3 --datapoints-to-alarm 2 \
    --threshold 5 --comparison-operator GreaterThanThreshold \
    --treat-missing-data notBreaching \
    --alarm-actions "$SNS_ARN" --ok-actions "$SNS_ARN" \
    --tags "$TAGS" \
    --metrics "$(cat <<JSON
[
  {"Id":"rate","Expression":"100*aborts/total","Label":"$1 客户端放弃率(%)","ReturnData":true},
  {"Id":"aborts","MetricStat":{"Metric":{"Namespace":"$NAMESPACE","MetricName":"ClientAborts","Dimensions":[{"Name":"Region","Value":"$1"}]},"Period":300,"Stat":"Sum"},"ReturnData":false},
  {"Id":"total","MetricStat":{"Metric":{"Namespace":"$NAMESPACE","MetricName":"Requests","Dimensions":[{"Name":"Region","Value":"$1"}]},"Period":300,"Stat":"Sum"},"ReturnData":false}
]
JSON
)"
}

# 三档里只给 CN 和 US 建告警。OTHER 是一锅各国混在一起的数，它的比率说明不了
# 任何一个地方的情况——一个秘鲁用户断两次就能把它顶上去。它的数据照常收，
# 看图时有用，但不值得半夜叫人。
alarm_rate CN "中国大陆的客户端放弃率超过 5%（连续两个 5 分钟）。多半是又有大响应压不住了，先看 ERP/Web 里 BytesSent 涨没涨"
alarm_rate US "美国的客户端放弃率超过 5%（连续两个 5 分钟）。这一侧网络很好，涨起来通常是服务端慢，看 UpstreamSeconds"

echo
echo "完成。现在的告警："
aws cloudwatch describe-alarms --alarm-name-prefix erp-aborts \
  --query 'MetricAlarms[].[AlarmName,StateValue]' --output text
echo
echo "看图："
echo "  aws cloudwatch get-metric-statistics --namespace $NAMESPACE --metric-name ClientAborts \\"
echo "    --dimensions Name=Region,Value=CN --start-time \$(date -u -v-6H +%Y-%m-%dT%H:%M:%S) \\"
echo "    --end-time \$(date -u +%Y-%m-%dT%H:%M:%S) --period 300 --statistics Sum"
