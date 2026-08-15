# AWS 建设脚本

四个脚本，按序号跑一遍，得到一套完整的生产环境骨架：一台应用服务器、一个托管数据库、一个文件桶、以及把它们正确隔开的网络规则。**每个脚本都可以重复执行**——已存在的资源会被跳过，不会重复创建。

这份目录和 [docs/DEPLOY.md](../../docs/DEPLOY.md) 的分工：这里管**云上有什么**（AWS 资源），DEPLOY.md 管**机器上怎么配**（端口、环境变量、密钥轮换、上线清单）。搬迁那天两份都要在手边。

## 心智模型：界面用来看，脚本用来改

AWS 控制台和这些脚本调用的是同一套 API，改的是同一份状态——脚本建的东西，刷新控制台立刻可见，账单、监控照常。反过来请**不要在控制台里手点改动**：点过的东西没有记录，机器毁掉重建的那天没人记得当时点过什么。脚本在 git 里，重放一遍就是一套一模一样的环境，这本身就是这套架构容灾能力的一半。

## 前置条件

1. AWS 账号已注册，root 已开 MFA，每人一个 IAM 用户（AdministratorAccess + MFA）
2. 本机装好 aws CLI，`aws configure` 配好自己的 Access Key，区域 `us-west-2`
3. 想好一个全球唯一的桶名

## 执行

```bash
cd deploy/aws
export ERP_BUCKET=erp-files-你的后缀   # 桶名全球唯一，必须自己起

sh 01-s3.sh        # 文件桶 + 只能碰这个桶的应用密钥
sh 02-network.sh   # 两个安全组：443 对外，5432 只对应用机
sh 03-rds.sh       # PostgreSQL（含 10 分钟等待）
sh 04-ec2.sh       # 应用服务器 + 固定公网 IP
```

| 脚本 | 建了什么 | 月成本（约） |
|---|---|---|
| 01 | S3 桶（私有、带版本、图片缓存 180 天过期）；IAM 用户 `erp-app`，权限只有这一个桶 | 按量，试点期 < $5 |
| 02 | 安全组 `erp-app`（443）和 `erp-db`（5432 仅来自 erp-app 组） | 0 |
| 03 | RDS PostgreSQL 16，`db.t4g.medium`，7 天自动备份 + 任意时间点恢复，删除保护开启 | ~$60 |
| 04 | EC2 `t3.xlarge`（Ubuntu 24.04 + Docker），SSM 登录（**没有 22 端口**），弹性 IP | ~$125 |

## secrets/ 目录

脚本生成的密钥落在 `deploy/aws/secrets/`（已 gitignore）：

```
s3-app-key.txt            应用的 S3 密钥 → 服务器 .env 的 MINIO_ACCESS_KEY / MINIO_SECRET_KEY
rds-master-password.txt   数据库管理密码 → 只用于 init-databases.sh 和救急
```

纪律和邮箱授权码一样：**不进 git，不进聊天，不进截图。** 传给队友走密码管理器或当面。

## 登录服务器

没有 SSH，用 SSM（脚本 04 的输出里有现成命令）：

```bash
aws ssm start-session --target <实例ID>
```

好处不是省事，是**攻击面**：22 端口从未打开，也就无所谓扫描、爆破、fail2ban。上线清单里 `nmap` 那条应该只看到 443。

## 跑完之后（搬迁日的顺序）

1. 在 RDS 上执行 `init-databases.sh`（03 的输出里有现成命令），然后 `make migrate` 指向 RDS
2. 按 DEPLOY.md 第三节配 `.env`——**每个密钥重新生成**，第四节的库密码一并换掉
3. 数据搬迁：本地 Postgres `pg_dump` → RDS；MinIO 对象 → S3（`mc mirror` 或 `aws s3 sync`）
4. 服务器上装 nginx + Let's Encrypt，按 DEPLOY.md 第二节配转发
5. GitHub Actions 构建镜像推 GHCR，服务器 `docker compose pull && up -d`
6. DEPLOY.md 第六节逐条打勾（nmap、audit-mail、恢复演练……），然后切 DNS

## 故障与恢复（这套架构的自愈边界）

- 服务崩溃：Docker 自动重启，秒级
- 机器硬件故障：AWS 自动在新硬件上拉起，分钟级，磁盘和 IP 都跟着走
- 机器彻底没了：重跑 `04-ec2.sh` → 搬迁日第 4、5 步 → DNS 已指向弹性 IP，接管即可。**机器上没有任何不可再生的东西**（数据在 RDS，文件在 S3，Kafka 从 outbox 表重放，Redis 自愈）
- 误删数据：RDS 恢复到任意时间点（5 分钟粒度，7 天窗口）；S3 有版本控制
- 数据库实例删除：有删除保护挡着，删之前必须先显式关掉它

## 拆除（危险，按逆序）

没有一键拆除，是故意的。逐条执行且每条都要想一下：

```bash
aws ec2 terminate-instances --instance-ids <实例ID>
aws ec2 release-address --allocation-id <弹性IP的AllocationId>
aws rds modify-db-instance --db-instance-identifier erp-pg --no-deletion-protection
aws rds delete-db-instance --db-instance-identifier erp-pg --final-db-snapshot-identifier erp-pg-final
aws s3 rb s3://$ERP_BUCKET --force        # 桶里是合同和客户来信，删之前想三遍
```
