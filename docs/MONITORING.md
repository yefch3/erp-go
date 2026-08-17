# 监控计划（上线次日执行）

没有监控的服务器，第一个报障的永远是用户。但一台机器的试点不配
Prometheus + Grafana 全家桶——那是给机群准备的帝国。这份计划是
"单机试点"尺寸的：三层、约半小时配完、成本近零。

**执行时机：迁移日 +1。** 不在迁移日当天，因为当天的目标是"跑起来"，
而告警线要照着真实水位画——空跑一天的 CPU 曲线才是画线的依据。

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

| # | 指标 | 告警线 | 为什么是它 |
|---|---|---|---|
| 1 | EC2 StatusCheckFailed | ≥1，立即 | 机器死亡的第一信号 |
| 2 | EC2 CPUUtilization | >80% 持续 15 分钟 | 容量预警，不是事故 |
| 3 | RDS FreeStorageSpace | <20% | 满盘的数据库 = 全系统趴下 |
| 4 | RDS CPUUtilization | >80% 持续 15 分钟 | 慢查询/索引缺失的外在症状 |
| 5 | RDS DatabaseConnections | >85 | 连接池总和 88、库上限 100（DEPLOY.md 五之二算过的账）——这条线是那笔账的哨兵 |
| 6 | Billing EstimatedCharges | 月预估 >$250 | 云上最容易失控的不是服务，是钱。预算 ~$200/月，$250 是"有东西不对"的线 |

## 第三层：机器内部的两个盲区（装一个官方小探头）

CloudWatch 默认看不见**磁盘占用**和**内存**——恰恰是单机最常见的两种
慢性死法（docker 镜像越积越多、内存缓涨到 OOM）。装官方 CloudWatch
Agent（经 SSM 一条命令分发），补两条线：

| # | 指标 | 告警线 |
|---|---|---|
| 7 | disk_used_percent (/) | >80% |
| 8 | mem_used_percent | >85% |

配套习惯：deploy.sh 里已有 `--remove-orphans`；镜像清理靠
`docker image prune` 进 cron（每周），磁盘线就很难被镜像堆到。

## 应用层（已有的，接上而不是新建）

- `make audit-mail`：邮件数据完整性审计，上线后进 cron 每日跑，
  ✗ 数量 >0 时邮件告警（上线清单已有此项，这里只是归档到监控视角）
- 业务错误看 `docker logs`——单机阶段够用，不建日志中心

## 明确不做（以及做的触发条件）

| 不做 | 触发条件（到了再做） |
|---|---|
| Prometheus + Grafana | **变成分布式系统时**——即触发 [K8S-PATH.md](./K8S-PATH.md) 第一节任意两条的那天，监控帝国随集群一起上 |
| 日志中心（Loki/ELK） | 同上，或排障时"到底在哪台机"成为日常问题时 |
| APM 链路追踪 | 服务间调用排障成为瓶颈时；目前 trace_id 已贯穿日志，够用 |

## 执行清单（迁移日 +1，约半小时）

- [ ] SNS 主题 + 邮件订阅（告警的收件通道）
- [ ] CloudWatch 告警 6 条（上表 1–6；Billing 告警在 us-east-1 建，这是 AWS 的规矩）
- [ ] SSM 分发 CloudWatch Agent + 告警 2 条（上表 7–8）
- [ ] UptimeRobot 注册 + healthz 拨测（这一项需要账号，操作人：fangchen）
- [ ] audit-mail 进 cron（每日）+ docker image prune 进 cron（每周）
- [ ] 一周后回看各指标真实水位，校准告警线
