# 恢复演练：备份到底能不能恢复

> 「备份存在」和「能恢复出一个可用的库」是两件事。这份文档记录一次**真正做过**
> 的演练，以及灾难时照着走的步骤。数据没了是这套系统唯一不可逆的风险——代码写
> 错能回滚，部署挂了 deploy.sh 会拒绝换容器，唯独数据没有第二次机会。

## 2026-08-23 首次演练结果

**结论：恢复可用。全流程 9 分 39 秒。**

| 项目 | 实测值 |
|---|---|
| 恢复方式 | 从自动快照 `rds:erp-pg-2026-08-22-11-24` 新建实例 |
| 发起 → 可连接 | **9 分 39 秒**（00:38:39 → 00:48:18 UTC） |
| 恢复出的数据库 | 12 个（approval/audit/export/fx/iam/inventory/mail/masterdata/procurement/product/reporting/shipping）全在 |
| 数据完整性 | 快照时刻之前的数据逐项一致（员工 2/2、角色列表完全相同、export 表结构 16/16） |
| 时点准确性 | 恢复库停在 `procurement goose 16 @ 08-22 10:32`——快照 11:24 之前的最后一个迁移，**精确**    |

**关于「差异」**：恢复库比生产少 2 张表、迁移版本差 5 个（proc 16 vs 21、iam 39 vs 41），
少了快照之后新增的客户/供应商数据。这**不是缺陷，是正确行为**——快照就该停在拍摄
时刻。检验完整性要拿「快照时刻之前创建的数据」比，那一项是完全一致的。

**当前备份配置**（`erp-pg`，us-west-2c）：

- PostgreSQL 16.13，db.t4g.medium，50 GB
- 自动备份保留 **7 天**，备份窗口 11:11–11:41 UTC
- PITR 可恢复到**约 1 分钟前**（演练时 `LatestRestorableTime` 距当下 4 分钟）
- 单 AZ、存储**未加密**

## 灾难时怎么做

### 1. 先决定恢复到哪个时点

```bash
# 有哪些快照
aws rds describe-db-snapshots --profile erp --db-instance-identifier erp-pg \
  --query 'reverse(sort_by(DBSnapshots,&SnapshotCreateTime))[:7].{id:DBSnapshotIdentifier,created:SnapshotCreateTime}' --output table
# PITR 能恢复到多新
aws rds describe-db-instances --profile erp --db-instance-identifier erp-pg \
  --query 'DBInstances[0].LatestRestorableTime' --output text
```

**误删数据**用 PITR 恢复到删除前一刻（`restore-db-instance-to-point-in-time --restore-time`）；
**实例损坏**用最新快照。两种都是**新建实例**，原实例不受影响——这一点很重要，恢复
过程不会让现状更糟。

### 2. 恢复到临时实例

```bash
aws rds restore-db-instance-from-db-snapshot --profile erp \
  --db-instance-identifier erp-pg-restore \
  --db-snapshot-identifier <快照 id> \
  --db-instance-class db.t4g.medium \
  --db-subnet-group-name default \
  --vpc-security-group-ids sg-0142f00cd13120fc6 \
  --no-publicly-accessible --no-multi-az
```

安全组必须用生产同一个（`sg-0142f00cd13120fc6`），否则 EC2 连不上。约 10 分钟后
`DBInstanceStatus` 变 `available`；状态走到 `backing-up` 时其实**已经可以连接**了。

### 3. 从 EC2 验证（RDS 不公开，只能在 VPC 内访问）

DSN 从容器环境里取，把主机名换成恢复实例的端点——密码不出现在命令行里：

```sh
DRILL_HOST=<恢复实例端点>
DSN=$(docker inspect erp-go-services-procurement-1 --format '{{range .Config.Env}}{{println .}}{{end}}' | grep '^DB_DSN=' | cut -d= -f2-)
RESTORE_DSN=$(printf '%s' "$DSN" | sed -E "s|@[^:/]+:|@${DRILL_HOST}:|")
docker run --rm postgres:16-alpine psql "$RESTORE_DSN" -tAc "SELECT count(*) FROM purchase_orders"
```

把脚本写成文件再用 `--cli-input-json` 发 SSM，不要把整段 shell 塞进命令行——
转义会把变量吃掉（演练时踩过两次）。

**该查什么**：关键表行数、`goose_db_version` 的最大版本、几张表的结构。别只看
「能连上」——能连上的空库也能连上。

### 4. 切换（真出事时）

确认数据无误后，把 `deploy/.env` 与 `write-env.sh` 里的 `DB_DSN` 主机名指向恢复
实例，重启服务。**两处都要改**，只改一处会在下次 write-env 时被覆盖回去。

如果恢复库的迁移版本落后于当前代码（快照之后发过版），先跑一次 `make migrate`
追平——迁移是幂等的，追平后才能让新代码接管。

### 5. 演练完删掉临时实例

```bash
aws rds delete-db-instance --profile erp --db-instance-identifier erp-pg-restore \
  --skip-final-snapshot --delete-automated-backups
```

db.t4g.medium 一天约 $2，忘了删就是白烧钱。

## 已知短板（值得改，不紧急）

1. **单 AZ**：可用区故障要靠恢复（约 10 分钟 + 切换时间），Multi-AZ 能做到分钟级
   自动切换，代价是费用翻倍。以当前业务量，10 分钟的 RTO 可以接受。
2. **存储未加密**：`StorageEncrypted: false`。加密只能在创建时开启，改要走
   「快照 → 加密副本 → 恢复」，需要一次停机窗口。
3. **保留 7 天**：一个月前的误删无法追回。如果业务需要更长，改
   `--backup-retention-period` 即可（最长 35 天），或定期打手工快照。
4. **恢复演练未纳入日程**：建议**每季度一次**，并在每次重大 schema 变更后补一次。

## 下次演练检查表

- [ ] 从最新快照恢复，记录实际耗时
- [ ] 12 个数据库全在
- [ ] 快照时刻之前的数据与生产一致（至少查 employees/roles/contracts/purchase_orders）
- [ ] `goose_db_version` 版本合理（停在快照时刻，不是空表）
- [ ] 用当前代码跑一次 `make migrate` 能追平
- [ ] 删除临时实例，确认账单不再计费
