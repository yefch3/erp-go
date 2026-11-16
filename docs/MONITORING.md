# 监控（2026-08-24 已配置）

没有监控的服务器，第一个报障的永远是用户。但一台机器的试点不配
Prometheus + Grafana 全家桶——那是给机群准备的帝国。这套是
"单机试点"尺寸的：三层、成本近零。

告警线是照着**真实水位**画的，不是拍脑袋——配置当天各项实测值见文末。

---

## 第一层：门口的哨兵（员工视角，最重要）

UptimeRobot 免费版（或同类外部拨测），每 5 分钟从公网访问：

```
https://<域名>/api/healthz        （域名前：http://100.22.230.55/api/healthz）
```

- 这正是网关 `/api/healthz` 端点的第二个用途（第一个是部署脚本的换血确认）
- 挂了 → 邮件告警
- 它回答的是终极问题：**员工现在能不能用**。服务器内部坏到什么程度都
  骗不了外部探针——这就是黑盒拨测排第一的原因。

## 第二层：AWS 自带仪表（CloudWatch 告警，零安装）

EC2 和 RDS 天生向 CloudWatch 汇报，只需画告警线（前 10 条免费，
告警走 SNS → 邮件）：

| # | 告警名 | 告警线 | 为什么是它 |
|---|---|---|---|
| 1 | `erp-ec2-status-check` | ≥1，立即 | 机器死亡的第一信号 |
| 2 | `erp-ec2-cpu` | >80% 持续 15 分钟 | 容量预警，不是事故 |
| 3 | `erp-rds-free-storage` | <10 GB | 满盘的数据库 = 全系统趴下。50 GB 的 20%，且**没开自动扩容** |
| 4 | `erp-rds-cpu` | >80% 持续 15 分钟 | 慢查询/索引缺失的外在症状 |
| 5 | `erp-rds-connections` | **>150** | 见下方「配置时改掉的两条」 |

### 配置时改掉的两条（2026-08-24 实配）

**连接数的线从 85 改成了 150。** 原稿写「连接池总和 88、库上限 100」，两个数都不对：

- 参数组是 `default.postgres16`，`max_connections` 用的是 RDS 默认公式
  `LEAST({DBInstanceClassMemory/9531392},5000)`——对 db.t4g.medium 算出来
  **约 450**，不是 100。
- 实测 24 小时，连接数只有 **11–21 条**。连接池是按需开的，不会预先占满，
  所以「11 × 8 = 88」是理论上限而非常态。

按 85 画线会在正常忙碌时天天误报。150 的含义是：**高于连接池理论上限 88，
远低于真实天花板**——越过它就是真在泄漏，而不是「今天有点忙」。

**账单告警建不成，已删除。** 这个账户是 AWS 组织 `555126922367` 下的成员账户，
合并账单后 **`AWS/Billing` 指标只在主账户发布**——实测本账户该命名空间 0 条指标。
加权限也没用，是结构性的。

一度建出来的告警显示 OK，但那是因为「无数据视为正常」，它永远不会变红。
**一个不会变红的绿灯比没有灯更坏**，所以删掉而不是留着。真要超支告警，
得请主账户那边针对本成员账户建预算。

查花费本身是可以的：`aws ce get-cost-and-usage` 在补了 `ce:*` 权限后能用。

## 第三层：机器内部的两个盲区（装一个官方小探头）

CloudWatch 默认看不见**磁盘占用**和**内存**——恰恰是单机最常见的两种
慢性死法（docker 镜像越积越多、内存缓涨到 OOM）。装官方 CloudWatch
Agent（经 SSM 一条命令分发），补两条线：

| # | 告警名 | 告警线 |
|---|---|---|
| 7 | `erp-disk-used` | >80% |
| 8 | `erp-mem-used` | >85% |

两条都用**只带 InstanceId 的聚合维度**，不绑 `device`：设备名（`nvme0n1p1`）
换机器就会变，绑死它等于给未来埋一个「告警还在、但指向一个不存在的盘」的雷。

探头是 `systemctl enabled` 的，机器重启后自己起来，不需要手动管。配置在
`/opt/aws/amazon-cloudwatch-agent/etc/erp.json`。

配套习惯：deploy.sh 里已有 `--remove-orphans`；镜像清理靠
`docker image prune` 进 cron（每周），磁盘线就很难被镜像堆到。

## 明确不做（以及做的触发条件）

| 不做 | 触发条件（到了再做） |
|---|---|
| Prometheus + Grafana | **变成分布式系统时**——即触发 [K8S-PATH.md](./K8S-PATH.md) 第一节任意两条的那天，监控帝国随集群一起上 |
| 日志中心（Loki/ELK） | 同上，或排障时"到底在哪台机"成为日常问题时。单机阶段 CloudWatch Logs 就是日志中心（第四层） |
| APM 链路追踪 | 服务间调用排障成为瓶颈时；目前 trace_id 已贯穿日志，够用 |

## 执行清单

**2026-08-24 执行。** 实配下来约一小时，比预估的半小时长——多出来的时间全花在
「文档写的和实际不符」上，见上方两处订正。

- [x] SNS 主题 `erp-alerts`（us-west-2）+ 邮件订阅，已确认
- [x] CloudWatch 告警 5 条（上表 1–5）——原计划 6 条，账单那条建不成，已删
- [x] SSM 分发 CloudWatch Agent + 告警 2 条（上表 7–8）
- [x] 面板 `erp-prod`：四张曲线 + 磁盘内存 + 告警总览，每张图都画了告警线
- [x] 2026-09-15：应用日志进 CloudWatch（`/erp/app`）+ 告警 16 条（第四层），`deploy/aws/05-alerts.sh`
- [ ] UptimeRobot 注册 + healthz 拨测（需要账号，操作人：fangchen）
- [ ] audit-mail 进 cron（每日）+ docker image prune 进 cron（每周）
- [ ] 一周后回看各指标真实水位，校准告警线

面板：
`https://us-west-2.console.aws.amazon.com/cloudwatch/home?region=us-west-2#dashboards/dashboard/erp-prod`

### 配置当天的真实水位（画线的依据）

| 指标 | 实测 | 告警线 |
|---|---|---|
| 机器 CPU | 8.7% | 80% |
| 机器磁盘 | 8%（7.3 GB / 96 GB） | 80% |
| 机器内存 | ~9%（1.4 GB / 15.7 GB） | 85% |
| 数据库 CPU | 3.8% | 80% |
| 数据库连接 | 11–21 | 150 |
| 数据库剩余磁盘 | 49.4 GB | 10 GB |

**每一项都离告警线很远**，这既是好消息也是提醒：现在的告警线是照着一台几乎
空转的机器画的。300 人真上来之后要回来重画——那时的「正常」和现在不是一回事。

顺带记一笔当时的规格与花费：EC2 `t3.xlarge`（4 核 16 GB）+ RDS
`db.t4g.medium` + 100 GB EBS，8 月实际消耗 46 美元（8/1–8/25）。按现在的
负载，机器规格明显偏大——但等 300 人上来再看，现在动没意义。

## 第四层：应用日志（2026-09-15 配置）

上面七条看的全是**机器**。应用自己报的错——业务失败、异常日志——之前没有任何
自动化，办法是人登上机器去 `docker logs` 里翻。这不是理论缺口：2026-08-24
发现邮件服务每两分钟打一簇 `i/o timeout`，持续了很久没人察觉——**日志里一直
有，但没人看**（修复见 `services/mail/internal/adapter/mailfetch/pool.go` 的
`idleWindow`）。

现在的做法，不加新组件：

1. **日志直接送 CloudWatch。** `deploy/docker-compose.prod.yml` 里每个服务的
   日志驱动是 `awslogs`，写进日志组 **`/erp/app`**，一个服务一条流，留 30 天。
   机器的 IAM 角色本来就有 `CloudWatchAgentServerPolicy`，够用。驱动设成
   `non-blocking`：CloudWatch 卡住时丢日志，不拖住服务；机器上 `docker logs`
   照常能用（Docker 20.10 起本地留副本）。
2. **从日志里数出指标。** 服务打的本来就是 JSON（`level`、`service`、`msg`、
   `event`），CloudWatch 的指标过滤器直接按字段匹配，不解析文本。
3. **告警**，都发到 `erp-alerts`，和上面七条同一个邮箱。

### ERROR 这一档的含义（2026-09-15 当天就改了一次）

告警的规则是「5 分钟内出现任何一条 `level=ERROR`」，所以**这一档必须只表示
「有东西坏了」**，不能是「有什么事没成」。

上线第一天就证明了原来不是这样：**响了六次，一次真故障都没有**——两次是一个
界面 bug、两次是模型读图没对上（校验拦住了，重试就好）、一次是我们自己在部署。
日志里同一天还有一批「激活链接已经用过」「没有对账权限」「附件类型不支持」，
全被记成 ERROR。

**每次都响的告警，看的人很快就不看了——那等于没有告警。** 所以分档改成：

| 记什么档 | 哪些 |
|---|---|
| **WARN**（查得到，不告警） | 业务拒绝（带 `biz_code`：额度、权限、类型不支持、链接用过了）；调用被取消（客户端关了页面，或者**进程正在换版本**——部署必然产生） |
| **ERROR**（告警） | 没有业务码的内部失败、下游连不上（`Unavailable`）、超时（`DeadlineExceeded`） |

判断在 `pkg/grpcx/interceptor.go` 的 `failureLevel`，网关那半在
`writeGRPCError`；两边都有测试钉着，包括「换版本时那句
`the client connection is closing` 不算错」。

| 告警 | 什么时候响 |
|---|---|
| `erp-<服务>-errors`，11 条（每个 Go 服务一条） | 5 分钟内出现任何一条 `level=ERROR`（含义见上） |
| `erp-mail-excel_storage_unavailable` | 转换结果传不上对象存储，任务已标失败 |
| `erp-mail-excel_storage_unreachable` | 领转换任务时对象存储不通 |
| `erp-mail-excel_row_write_failed` | 结果传上去了、库里那一行没写成（会自动恢复，但连着出现说明数据库有问题） |
| `erp-mail-excel_result_unreachable` | 预览或下载时从对象存储取不到结果 |
| `erp-mail-excel_sweep_remove_failed` | 清理器删不掉过期的结果 |
| `erp-mail-excel_model_account` | 模型厂拒了 `OPENAI_API_KEY`：key 失效或余额用完，全公司都转不成——去 OpenAI 后台看账单和 key |
| `erp-mail-excel_model_busy` | 限流或对方出错，自动重试三次仍不行（偶尔一次可不管；连着响说明碰到了账号的每分钟上限） |

后七条按日志里**固定的 `event` 字段**匹配，不按那句话的文字——文字随便改，
`event` 不能改，改了告警就静默失效。字段名定在
`services/mail/internal/app/excel_jobs.go`，`TestAlertScriptKnowsEveryExcelEvent`
钉着代码和脚本两边一致。「恢复成功」（`excel_result_recovered`）只记数不告警：
它是好消息，模型没重跑。

告警线全是「≥1 次」：这些事一次都不该发生。没数据当正常——完全没日志是服务
全停了，那归机器那几条管。全部定义在 `deploy/aws/05-alerts.sh`，幂等，改了重跑。

看日志不用登机器了：

```bash
aws logs tail /erp/app --follow --filter-pattern '{ $.service = "mail" }' --profile erp --region us-west-2
```

要给别的失败点加告警：代码里那条日志加一个固定的 `event` 字段，脚本里加一行
`excel <event> "<说明>"`（或者照 `filter`/`alarm` 两个函数写），重跑脚本。

**还没做的**：`make audit-mail`（邮件数据完整性审计，✗ 数量 >0 时告警）早就写好
了，只是从来没被定时跑过——**能发现问题的东西存在，但没有人或机器去按它。**

## 另一块：依赖容器停了，七条告警一条都不响

2026-08-27 查出来的。Redis 和 Kafka 跑在这台机器的 Docker 里（数据库是 RDS，
不在此列）。它们停掉之后：

- CloudWatch 看的是 CPU / 内存 / 磁盘 / RDS ——**容器少了两个不体现在任何一条上**
- `/api/healthz` 只回答「网关进程活着」，它有意不探测下游（部署门禁需要这样）
- 员工也感觉不到：限流、登录失败节流、幂等三处在连不上 Redis 时都**选择放行**
  （宁可不拦，也不要因为 Redis 故障让全公司登不上），只写一条 WARN

合起来就是：**Redis 停了，系统看上去完全正常，只是暴力破解防护已经没了。**
Kafka 停了则是领域事件在生产者那头一直重试、消费者那头什么都收不到。

已经做的是把「让它别停」这一半补上——`deploy/docker-compose.infra.yml` 里
那两个容器原来是 `restart: no`（十三个业务容器都是 `unless-stopped`），现在
统一了。**没有做的是「停了要有人知道」**：那需要一个会探测下游的就绪探针，
或者一条数容器的 cron。排在这一节上面那条日志 cron 的同一批里——两件事
共用同一个「每天有人去按一下」的机制。

在此之前，人工查一眼：

```bash
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["docker ps --format \"{{.Names}}\t{{.Status}}\" | sort"]'
```

应该看到 **15 个容器全是 Up**：十三个 `erp-go-services-*`，加上
`erp-go-infra-redis-1` 和 `erp-go-infra-kafka-1`。

## 第五层：按国家看请求（2026-09-22 配置）

### 为什么加

2026-09-21 中国大陆访问一直超时，**是员工报上来的，不是监控报上来的**。日志里其实早就
写着答案——江苏那个 IP 的 499 率 0.131%、美国 0.008%，差二十倍——但没有人会天天去
grep 日志。这一层就是让那个数字自己说话。

### 先说清楚它量不到什么

**没有任何一个指标叫"延迟"，这是故意的。**

nginx 的 `$request_time` 结束于「把响应交给内核套接字」，不是「客户端收到」。2026-09-22
实测：中国的 IP 和美国的 IP，`rt` 和 `urt` 几乎完全相等——**过太平洋那几百毫秒一点都没
被记进去**。发布一个叫"延迟"而其实是服务端耗时的指标，比没有指标更坏：它会在真出事的
时候显示一切正常。

真要量用户等了多久，只能从浏览器那一侧上报。那是另一件事，还没做。

### 收什么

命名空间 `ERP/Web`，维度 `Region ∈ {CN, US, OTHER}`：

| 指标 | 是什么 |
|---|---|
| `Requests` | 请求数 |
| **`ClientAborts`** | **499：客户端等不及自己断开——这才是"慢到不能用"的真实信号** |
| `ServerErrors` | 5xx |
| `UpstreamSeconds` | 服务端自己算了多久（和用户在哪儿无关，但能看出接口本身慢没慢） |
| `BytesSent` | 发出去多少字节（压缩要是回归了，会在这儿先露头） |

只分三档。每多一个国家就多五个自定义指标（$0.30/个/月），而秘鲁来的那几十次请求不值
一块钱。真有第二个国家的人天天用，那时再单列。

### 怎么收的

`deploy/aws/region-metrics.py` 装在 `/opt/erp/`，systemd timer 每分钟跑一次：读 nginx
访问日志新增的那一段（记字节偏移，认得出日志轮转），按 IP 查国家（`geoiplookup`，
Ubuntu 源里的 GeoIP 库，不用注册 MaxMind），汇总后 `put-metric-data`。

装/改：`sh deploy/aws/06-region-metrics.sh`（幂等）。

**第一次跑直接跳到日志末尾，不补历史。** 装上当天踩过：从 0 读起会把当天二十多万行
全算进"这一分钟"，CloudWatch 上是一根三万五千请求的假尖峰。

### 告警

`erp-aborts-CN` / `erp-aborts-US`：**客户端放弃率**连续两个 5 分钟超过 5% 就发到
`erp-alerts`。

告比率不是次数：中国那边一天几万次请求，几次掐断是正常抖动；美国那边一天几千次，同样
几次就说明出事了。按次数定线，两边只能各定一个，而那个数字过一个月就不对了。

**5% 是按实测定的**：2026-09-21 压缩之前，出问题的那一整天江苏那个 IP 也只有 0.131%。
5% 意味着"比那次严重四十倍"——不会因为日常抖动半夜把人叫起来，而真出事时一定响。

OTHER 不建告警：它是一锅各国混在一起的数，一个秘鲁用户断两次就能把比率顶上去。数据
照常收，看图时有用。

### 花多少钱

自定义指标 $0.30/个/月 × 5 个指标 × 3 个桶 = $4.5，加每分钟一次 PutMetricData 约
$0.43。**合计约 $5/月。**

### 看图

```bash
aws cloudwatch get-metric-statistics --namespace ERP/Web --metric-name ClientAborts \
  --dimensions Name=Region,Value=CN --period 300 --statistics Sum \
  --start-time $(date -u -v-6H +%Y-%m-%dT%H:%M:%S) --end-time $(date -u +%Y-%m-%dT%H:%M:%S)
```
