# 一次代码变更的完整旅程

从你本机 `git push` 到试点公司的浏览器看见变化，中间发生了什么、谁推给谁、
谁去拉谁、每一步凭什么被允许、以及怎么知道它成功了。

本文写的是**已经在跑的事实**（2026-08-17 首次上云当天实测），而不是设想。
[DEPLOY-PIPELINE.md](./DEPLOY-PIPELINE.md) 是方案与分期，本文是逐步骤实录。

一句话骨架：

```
你 push → GitHub 跑 CI（闸门）→ 合并 main → GitHub 构建镜像并推 GHCR（货架）
→ 服务器从 GHCR 拉镜像（不构建）→ 换容器 → 问 /api/healthz → 浏览器看见
```

**最重要的一句：没有任何东西被"推"到服务器上。** 服务器永远是主动"拉"的一方
（GitHub 拉代码、GHCR 拉镜像、Parameter Store 拉秘密），所以它可以一个入站
端口都不为运维开放。

---

## 第 0 步：你 push 到分支（还没有任何部署）

`git push` 只把提交送到 GitHub。GitHub 上有两个 workflow 在等：

| workflow | 触发条件 | 干什么 |
|---|---|---|
| `ci.yml` | **任何** push / PR | 起 Postgres+Redis，跑 `make ci`（生成物一致性、11 服务测试、lint）——**闸门** |
| `build.yml` | **只有** push 到 `main` | 构建 12 个镜像推 GHCR——**货架** |

这就是"推分支只体检、合 main 才出货"的分界。

**怎么触发的**：GitHub 收到 push 后读仓库里 `.github/workflows/*.yml` 的
`on:` 段自己决定，没有任何外部调度器。

**验证**：PR 页面的绿勾；命令行 `gh pr checks <编号>`。本仓库的纪律是
CI 结果作为闸门——**红了不合并**，本地也用同一份 `make ci` 提前跑。

## 第 1 步：合并进 main → 出货

合并动作本身就是一次对 `main` 的 push，`build.yml` 因此启动（GitHub 托管的
一台 Ubuntu 临时机）：

1. `actions/checkout` 拉本仓库代码到那台临时机
2. `docker/login-action` 登录 `ghcr.io`——用的是 GitHub **每次运行自动注入
   的 `GITHUB_TOKEN`**（一次性、自动过期，仓库里不存任何长期密钥）
3. 循环 11 个 Go 服务：`docker buildx build --push -f deploy/Dockerfile
   --build-arg SERVICE=<名字>`，每个打**两个**标签
   - `ghcr.io/yefch3/erp-go/<服务>:<git-sha>` ← 不可变，部署与回滚都用它
   - `:latest` ← 只给人眼看，部署脚本从不使用
4. 前端另一份 Dockerfile：node 构建 dist → 烤进 nginx 镜像
5. `retention` 任务清理，每个包只留最近 10 个版本（GHCR 私有存储有限）

**谁推给谁**：GitHub Actions 的临时机 **push** 镜像到 GHCR。服务器此刻毫不知情。

**验证**：Actions 页面该次运行全绿；仓库首页右侧 Packages 出现 12 个包，
每个包的版本列表里有这次的 SHA。

## 第 2 步：服务器拉取并换血（今天=手动，第二期=自动）

**今天**由我通过 SSM 发一条命令；**第二期**由 `deploy.yml` 用同一条通道发
同一条命令（见 DEPLOY-PIPELINE.md 六）。命令内容就是这几行：

```sh
cd /opt/erp/repo
git fetch origin && git checkout <git-sha>     # 配置与迁移脚本对齐到同一个提交
export ERP_SHA=<git-sha>
docker compose -f deploy/docker-compose.services.yml \
               -f deploy/docker-compose.prod.yml pull    # 只拉，不构建
docker compose ... up -d --remove-orphans                # 换容器
curl -fsS http://127.0.0.1:8080/api/healthz              # 自证
```

几件事在这里同时成立，每件都是刻意的：

- **服务器上有一份仓库克隆**（`/opt/erp/repo`），但**不用来构建**——只为让
  compose 文件和 goose 迁移脚本与镜像**来自同一个提交**。二进制从 registry
  来，配置从 git 来，靠同一个 SHA 对齐。
- **它凭什么能 git clone**：机器上生成的一对 SSH 密钥，公钥作为
  **只读 deploy key** 挂在仓库上（GitHub 会为此给你发一封周知邮件）。
- **它凭什么能 docker pull**：一个只勾了 `read:packages` 的
  fine-grained PAT，在机器上 `docker login ghcr.io` 一次，凭据存在
  root 的 docker 配置里。转运这把令牌用的是加密参数通道，用后即销毁。
- **秘密从哪来**：`/opt/erp/repo/deploy/.env`（600 权限，只此一份，
  永不进 git）。三把机器钥匙在机器上现场 `openssl rand` 生成；你的
  OpenAI / Google 钥匙经 SSM Parameter Store（SecureString）转运一次，
  用后参数即删。`docker compose` 自动读同目录的 `.env`。
- **prod 覆盖文件的作用**：把基础 compose 里写死的开发值全部换成
  `${VAR:?}`——RDS 的 DSN（各服务各自的新口令）、S3 端点与密钥、
  `TRUST_PROXY_HEADERS=1`、网关 8080 收回环回。缺任何一个变量，
  compose 在**执行前**就大声失败，而不是运行时才崩。

**验证（分层，从里到外）**：

| 层 | 怎么问 | 通过的样子 |
|---|---|---|
| 部署确实换血了 | `curl /api/healthz` | `{"ok":true,"version":"<你部署的那个 SHA>"}` ——版本号是自证，不是猜 |
| 容器都活着 | `docker ps` | 15 个 Up，`--filter status=restarting` 计数为 0 |
| 单个服务为何不活 | `docker logs <容器>` | 首次点火就是这样抓到两个真 bug（见下） |
| 外部真能用 | 从外网 `curl http://<公网IP>/` 与 `/api/healthz` | 200 |
| 业务真能用 | 浏览器走登录 / 收发信 / Excel 转换 | 冒烟清单 |

## 第 3 步：浏览器到容器的路径

```
员工浏览器 → 公网 :80(将来 443) → 主机 nginx
                                    ├─ /api/*  → 127.0.0.1:8080  网关容器
                                    └─ /*      → 127.0.0.1:8082  前端 nginx 容器
网关 → docker 内网 → 11 个服务容器（iam:9001 ...）
服务 → RDS(私网 5432) / S3(https) / Redis / Kafka(容器内)
```

所有业务容器都绑在 `127.0.0.1`，**唯一对公网开的门是主机 nginx**。
`/api/events`（SSE 实时推送）在 nginx 里单独关掉了缓冲，否则事件会被
攒在 nginx 手里不走。

## 失败时会发生什么

- **CI 红**：不合并，不出货。零影响。
- **构建失败**：货架上没有这个 SHA 的镜像，服务器仍跑旧版本。零影响。
- **`compose config` 缺变量**：拉取前就失败，旧容器继续跑。
- **健康检查不过**：第二期的 `deploy.sh` 会自动 `git checkout` 回
  `/opt/erp/last-good` 并重新 `up -d`，然后以非零码退出让 workflow 变红。
- **回滚**：用旧 SHA 再跑一次同样的部署命令。镜像不可变，所以
  "回到上周三那版"就是拉上周三那个 SHA。

## 首次上云当天实测抓到的两个真 bug（这一节是为了说明验证不是仪式）

首次点火后 15 个容器里 11 个健康、**4 个重启循环**——全是存文件的服务。
`docker logs` 给出两句不同的死因，指向两个不同的 bug：

1. `blobstore: bucket check: 301 Moved Permanently`（mail/product/export）
   ——blobstore 支持指定区域，但四个服务谁都没把 `MINIO_REGION` 接进来；
   空区域按 us-east-1 签名，us-west-2 的桶对每个请求都答 301。本地开发
   永远不会暴露：MinIO 根本不看区域。
2. `dial tcp: lookup minio` （shipping）——覆盖文件第一版漏了 shipping 的
   `MINIO_*` 整组，它还在找开发环境的 MinIO 容器。

两者都在 PR #129 修掉。**这就是"分层验证"的产出**：如果只看
`curl /api/healthz`（网关本身是健康的，它不碰对象存储），这两个 bug 会
一直潜伏到某位员工上传第一个附件的那一刻。
