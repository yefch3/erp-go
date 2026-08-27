# 给生产库开存储加密

> **这件事已经做完了：2026-08-27 22:18–22:30 UTC，生产库现在是加密的。**
> 下面留着的是「怎么做」和「做的时候会踩什么」，将来换 KMS 密钥、跨区域搬库、
> 或者别的环境要开加密时照着走。做完那次实际踩到的三个坑已经改进正文里了。
>
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

## 现状（2026-08-27 切换后）

| | |
|---|---|
| 实例 | `erp-pg`，PostgreSQL 16.13，db.t4g.medium，gp3 50 GB |
| 端点 | `erp-pg.cr2eiumeyg65.us-west-2.rds.amazonaws.com`（**切换前后一字不差**） |
| 可用区 | us-west-2c（单 AZ） |
| **存储加密** | **是**，KMS `alias/aws/rds`（AWS 托管密钥，不收月费） |
| 备份保留 | 7 天，窗口 11:11–11:41 UTC |
| 删除保护 | 开 |
| 安全组 / 子网组 / 参数组 | `sg-0142f00cd13120fc6` / `default` / `default.postgres16` |
| 旧实例 | `erp-pg-old`（未加密），已停机、未删除，留作退路 |

**端点里的 `cr2eiumeyg65` 是账号加区域的固定后缀。** 所以只要新实例最终改名叫
`erp-pg`，端点字符串完全一样——`deploy/.env` 里的连接串一个字都不用动。这是
整套做法能成立的关键。

## 停机窗口

第 8 到第 11 步之间数据库是连不上的：旧实例已经改名让开，新实例还没顶上。
恢复实例本身（第 5–6 步）不影响现网，可以提前做完再挑时间切。

**2026-08-27 实测：22:18 停服务 → 22:30 全部起来并验证通过，共约 12 分钟。**
其中改名各花约 60–90 秒（两次合计 3 分钟），剩下的时间是我用错 compose 文件、
容器进重启循环、诊断并重起——也就是说照着改好的文档走，窗口在 6 分钟上下。

## 切换前先确认会不会丢数据

快照拍完到切换完成之间写进去的东西**全部会丢**。切之前把这个问题问清楚：

```bash
# 在生产机上跑：各表最后一次写入是什么时候
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript --parameters 'commands=["cd /opt/erp/repo && set -a && . ./deploy/.env && set +a && docker run --rm -i -e P=\"$IAM_DB_DSN\" postgres:16-alpine sh -c '\''psql -qtA \"$P\" -c \"select count(*), max(created_at) from employees\"'\''"]'
```

最后一次写入若早于快照时间，这条风险就是零（2026-08-27 那次正是如此：最后
一次写是前一天）。若不是，那就把第 0 步的停服务挪到拍快照**之前**——代价是
停机窗口从 6 分钟变成 25 分钟左右。

## 步骤

前提：`AWS_PROFILE=erp`，区域 `us-west-2`。每一步都等上一步真的完成再走。

> **停服务和起服务必须用两个 compose 文件。** 生产的连接串在
> `docker-compose.prod.yml` 里；`docker-compose.services.yml` 单独用带的是**本地
> 开发的默认值**，指向一个叫 `postgres` 的主机。2026-08-27 那次切换我在这里
> 栽了：`up -d` 拿本地默认值把十一个容器全重建了一遍，全部进入重启循环，日志
> 里刷的是 `lookup postgres ... server misbehaving`。停机因此从 6 分钟拖到 12
> 分钟。下面每条命令都已经带上了两个文件和 `ERP_SHA`，照抄即可。

### 0. 停掉写入（推荐）

让快照是「静止」的，避免切换后丢掉几分钟的写入。

```bash
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["cd /opt/erp/repo && ERP_SHA=$(cat /opt/erp/last-good) docker compose -f deploy/docker-compose.services.yml -f deploy/docker-compose.prod.yml stop"]'
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

**恢复出来的实例删除保护是关的**，一定要补。备份保留 2026-08-27 那次实测是
跟着快照继承下来的 7 天（不是我原先写的 0），但这条不要赌——下面那句 modify
无论如何都把两样一起设上，然后照着验证命令逐项对。「加密开了、备份没了」比
不加密更糟。

```bash
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg-enc \
  --backup-retention-period 7 \
  --preferred-backup-window 11:11-11:41 \
  --preferred-maintenance-window sun:07:00-sun:07:30 \
  --deletion-protection \
  --apply-immediately
```

验证（这三项必须对上再往下走）。**字段名不能写中文**——`--query` 是 JMESPath，
非 ASCII 的键名它直接报 `Unknown token`，我第一次就是这么写的：

```bash
aws rds describe-db-instances --region us-west-2 --db-instance-identifier erp-pg-enc \
  --query 'DBInstances[0].[StorageEncrypted,BackupRetentionPeriod,DeletionProtection]' --output text
# 列依次是：加密 / 备份天数 / 删除保护 —— 期望 True 7 True
```

### 8–11. 改名顶替（**停机窗口从这里开始**）

删除保护挡的是删除，不挡改名，所以不用先关掉它。

**不要用 `aws rds wait ... --db-instance-identifier <新名字>` 等改名。** 改名下发
之后有一段时间，实例在 API 里还报**旧**标识符、状态 `renaming`，新名字查不到——
waiter 会当场 `DBInstanceNotFound` 退出，看起来像失败了其实没有。用轮询等：

```bash
wait_named() {  # 等到某个名字出现且 available
  for i in $(seq 1 40); do
    aws rds describe-db-instances --region us-west-2 \
      --query 'DBInstances[].[DBInstanceIdentifier,DBInstanceStatus]' --output text \
      | grep -q "^$1	available" && return 0
    sleep 15
  done
  echo "等 $1 超时" >&2; return 1
}

# 旧的让开
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg \
  --new-db-instance-identifier erp-pg-old --apply-immediately
wait_named erp-pg-old

# 新的顶上（端点从此和原来完全一样）
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg-enc \
  --new-db-instance-identifier erp-pg --apply-immediately
wait_named erp-pg
```

### 12. 起服务

```bash
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["cd /opt/erp/repo && ERP_SHA=$(cat /opt/erp/last-good) docker compose -f deploy/docker-compose.services.yml -f deploy/docker-compose.prod.yml up -d --remove-orphans"]'
```

起完先看容器状态再看健康检查：**全部 `Up` 才算起来了**。看到 `Restarting`
就是上面那个 compose 文件的坑，日志里会写 `lookup postgres`。

```bash
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["sleep 45; docker ps --format \"{{.Names}}\t{{.Status}}\"; curl -fsS --max-time 8 http://127.0.0.1:8080/api/healthz"]'
```

### 13. 验证

```bash
sh scripts/rds-encrypt-status.sh
```

它会打出：加密是否开启、备份保留天数、端点、以及各服务的迁移版本——迁移
版本能证明连上的确实是那份数据，而不是一个空库。

**2026-08-27 那次的基线**（切换前取，切换后逐项复核，一字不差）：

```
IAM 46   MASTERDATA 18   EXPORT 17   PROCUREMENT 29   INVENTORY 8
MAIL 41  SHIPPING 10     PRODUCT 2   APPROVAL 6      FX 2
员工 5   客户 2          供应商 3
```

**基线要在切换当天现取**，不要用这份——迁移版本每次上线都可能变。

对不上不要犹豫，直接照下面回退——一个数字对不上就是恢复错了快照，
往下走只会越走越难收。

最后两条，都要跑：

```bash
# 容器全是 Up，且健康检查回 ok
aws ssm send-command --region us-west-2 --instance-ids i-069173001683d7d9b \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["docker ps --format \"{{.Names}}\t{{.Status}}\"; curl -fsS http://127.0.0.1:8080/api/healthz"]'

# 从公网也能通
curl -s --max-time 15 https://mercova.ip-ddns.com/api/healthz
```

再从浏览器打开一次系统，确认能登录、能看到员工和角色。

## 出了问题怎么回退

**第 11 步之前**：什么都没动到现网。把 `erp-pg-enc` 删掉重来即可。

**第 11 步之后**：旧实例 `erp-pg-old` **原封不动地还在**（只是改了个名）。
回退就是把名字换回去：

**旧实例已经停机的话，先把它开起来**（停机是收尾那一步做的，约 3–5 分钟）：

```bash
aws rds start-db-instance --region us-west-2 --db-instance-identifier erp-pg-old
wait_named erp-pg-old        # wait_named 见第 8–11 步
```

然后把名字换回去（同样用 wait_named，不要用 `aws rds wait`）：

```bash
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg --new-db-instance-identifier erp-pg-enc --apply-immediately
wait_named erp-pg-enc
aws rds modify-db-instance --region us-west-2 \
  --db-instance-identifier erp-pg-old --new-db-instance-identifier erp-pg --apply-immediately
wait_named erp-pg
```

## 收尾

**旧实例先停、别急着删。** 观察几天，确认没有任何东西还在找它：

```bash
aws rds stop-db-instance --region us-west-2 --db-instance-identifier erp-pg-old
```

停着的实例仍然收存储费，但比「删早了才发现漏了什么」便宜得多。确认无碍之后
再关删除保护并删除，同时清掉这次产生的手工快照。

**注意 RDS 停机最多 7 天，到期会自己开机。** 所以「停着不管」不是长久状态：
要么在 7 天内决定删掉，要么记着到期再停一次。

### 2026-08-27 那次留下的东西（待清理）

| | 状态 | 什么时候能删 |
|---|---|---|
| 实例 `erp-pg-old` | 已停机，删除保护开着 | 观察几天没问题后：先关删除保护再删 |
| 快照 `erp-pg-pre-encrypt` / `-enc` | 第一次试切时留下的，已过时 | 随时 |
| 快照 `erp-pg-preenc-20260827-1450` / `-enc` | 这次真正用的 | 删掉 `erp-pg-old` 之后 |

删实例和删快照这两条命令**故意没进放行名单**，跑的时候会问一次——这是有意的。

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
