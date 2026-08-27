# 给生产库开存储加密

> RDS 的存储加密**不能原地打开**。唯一的路是：拍快照 → 复制成加密快照 →
> 从加密快照恢复出新实例 → 改名顶替。所以它必须挑一个能停几分钟的时候做，
> 而**越早做越便宜**：现在生产是零业务数据，切错了大不了重来；等装了客户
> 名单、合同和银行流水之后再做，同样的步骤就变成一件要排期、要通知、要人
> 盯着的事。
>
> 这份文档是**给人照着敲的**，不是脚本。中间有两步是不可逆的改名，值得
> 一步一看，而不是让一个脚本连着跑完。

## 为什么值得做

数据库里有客户名单、供应商报价、合同金额、银行流水和收款记录。存储不加密
意味着：底层磁盘、快照、以及任何一份快照的复制品，都是明文。AWS 的加密是
透明的——开了之后应用一个字不用改，性能差别在噪声范围内——**代价只有这
一次切换**。

## 现状（2026-08-27 查）

| | |
|---|---|
| 实例 | `erp-pg`，PostgreSQL 16.13，db.t4g.medium，gp3 50 GB |
| 端点 | `erp-pg.cr2eiumeyg65.us-west-2.rds.amazonaws.com` |
| 可用区 | us-west-2c（单 AZ） |
| **存储加密** | **否** ← 这次要改的 |
| 备份保留 | 7 天，窗口 11:11–11:41 UTC |
| 删除保护 | 开 |
| 安全组 / 子网组 / 参数组 | `sg-0142f00cd13120fc6` / `default` / `default.postgres16` |

**端点里的 `cr2eiumeyg65` 是账号加区域的固定后缀。** 所以只要新实例最终改名叫
`erp-pg`，端点字符串完全一样——`deploy/.env` 里的连接串一个字都不用动。这是
整套做法能成立的关键。

## 停机窗口

第 8 到第 11 步之间数据库是连不上的：旧实例已经改名让开，新实例还没顶上。
实测改名各需 1–3 分钟，**总窗口约 5 分钟**。恢复实例本身（第 5–6 步）不影响
现网，可以提前做完再挑时间切。

## 步骤

前提：`AWS_PROFILE=erp`，区域 `us-west-2`。每一步都等上一步真的完成再走。

### 0. 停掉写入（推荐）

让快照是「静止」的，避免切换后丢掉几分钟的写入。

```bash
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["cd /opt/erp/repo && docker compose -f deploy/docker-compose.services.yml stop"]'
```

### 1–2. 拍一个新快照并等它好

**不要用旧快照。** 拍完到切换之间的所有写入都会丢，所以这一步要挨着切换做。

```bash
STAMP=$(date -u +%Y%m%d-%H%M)
aws rds create-db-snapshot --region us-west-2 \
  --db-instance-identifier erp-pg \
  --db-snapshot-identifier erp-pg-preenc-$STAMP

aws rds wait db-snapshot-available --region us-west-2 \
  --db-snapshot-identifier erp-pg-preenc-$STAMP
```

### 3–4. 复制成加密快照

`alias/aws/rds` 是 AWS 托管的默认密钥，不用自己管轮换。要用自己的 KMS 密钥就
把它换掉。

```bash
aws rds copy-db-snapshot --region us-west-2 \
  --source-db-snapshot-identifier erp-pg-preenc-$STAMP \
  --target-db-snapshot-identifier erp-pg-preenc-$STAMP-enc \
  --kms-key-id alias/aws/rds

aws rds wait db-snapshot-available --region us-west-2 \
  --db-snapshot-identifier erp-pg-preenc-$STAMP-enc
```

### 5–6. 从加密快照恢复出新实例（约 10 分钟）

参数照抄现状那张表。**主账号密码跟着快照走**，所以恢复出来的实例用的还是
`deploy/.env` 里那套凭据。

```bash
aws rds restore-db-instance-from-db-snapshot --region us-west-2 \
  --db-instance-identifier erp-pg-enc \
  --db-snapshot-identifier erp-pg-preenc-$STAMP-enc \
  --db-instance-class db.t4g.medium \
  --db-subnet-group-name default \
  --vpc-security-group-ids sg-0142f00cd13120fc6 \
  --db-parameter-group-name default.postgres16 \
  --no-multi-az --no-publicly-accessible --storage-type gp3

aws rds wait db-instance-available --region us-west-2 \
  --db-instance-identifier erp-pg-enc
```

### 7. 把备份和删除保护补回来

**恢复出来的实例备份保留是 0**（RDS 的默认），不补的话就是「加密开了、备份
没了」——比不加密更糟。

```bash
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg-enc \
  --backup-retention-period 7 \
  --preferred-backup-window 11:11-11:41 \
  --preferred-maintenance-window sun:07:00-sun:07:30 \
  --deletion-protection \
  --apply-immediately
```

验证（这三项必须对上再往下走）：

```bash
aws rds describe-db-instances --region us-west-2 --db-instance-identifier erp-pg-enc \
  --query 'DBInstances[0].{加密:StorageEncrypted,备份天数:BackupRetentionPeriod,删除保护:DeletionProtection}'
```

### 8–11. 改名顶替（**停机窗口从这里开始**）

删除保护挡的是删除，不挡改名，所以不用先关掉它。

```bash
# 旧的让开
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg \
  --new-db-instance-identifier erp-pg-old --apply-immediately
aws rds wait db-instance-available --region us-west-2 --db-instance-identifier erp-pg-old

# 新的顶上（端点从此和原来完全一样）
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg-enc \
  --new-db-instance-identifier erp-pg --apply-immediately
aws rds wait db-instance-available --region us-west-2 --db-instance-identifier erp-pg
```

### 12. 起服务

```bash
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["cd /opt/erp/repo && docker compose -f deploy/docker-compose.services.yml up -d"]'
```

### 13. 验证

```bash
sh scripts/rds-encrypt-status.sh
```

它会打出：加密是否开启、备份保留天数、端点、以及各服务的迁移版本——迁移
版本能证明连上的确实是那份数据，而不是一个空库。

**切换前的基线（2026-08-27 18:5x UTC 取）**，切完拿这个对：

```
IAM 46   MASTERDATA 18   EXPORT 17   PROCUREMENT 29   INVENTORY 8
MAIL 41  SHIPPING 10     PRODUCT 2   APPROVAL 6      FX 2
```

对不上不要犹豫，直接照下面回退——一个数字对不上就是恢复错了快照，
往下走只会越走越难收。

再从浏览器打开一次系统，确认能登录、能看到员工和角色。

## 出了问题怎么回退

**第 11 步之前**：什么都没动到现网。把 `erp-pg-enc` 删掉重来即可。

**第 11 步之后**：旧实例 `erp-pg-old` **原封不动地还在**（只是改了个名）。
回退就是把名字换回去：

```bash
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg --new-db-instance-identifier erp-pg-enc --apply-immediately
aws rds wait db-instance-available --region us-west-2 --db-instance-identifier erp-pg-enc
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg-old --new-db-instance-identifier erp-pg --apply-immediately
```

## 收尾

**旧实例先停、别急着删。** 观察几天，确认没有任何东西还在找它：

```bash
aws rds stop-db-instance --region us-west-2 --db-instance-identifier erp-pg-old
```

停着的实例仍然收存储费，但比「删早了才发现漏了什么」便宜得多。确认无碍之后
再关删除保护并删除，同时清掉这次产生的两个手工快照。

## 顺带一提：单 AZ

「单 AZ」是说这个数据库只住在一个机房。那个机房出问题就得靠备份恢复——按
2026-08-23 的演练是约 10 分钟，且会丢最后几分钟的数据。**多 AZ** 是在另一个
机房放一个实时同步的备库，故障时自动切换，几十秒恢复；代价是数据库月费翻倍。

它和加密不一样：**多 AZ 随时可以在线打开**，不用重建实例。所以不必挤在这次
做——等有真实业务、能算清停机一小时值多少钱的时候再决定。

```bash
# 将来想开的时候，一条命令，不停机
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg --multi-az --apply-immediately
```
