# 自动部署管道（GitHub Actions）

这份文档是**蓝图**：管道分三期落地，第一期今天就能开工，后两期等 AWS 机器建好。
它假设 [DEPLOY.md](./DEPLOY.md) 的上云清单（只开 443、SSM 登录、秘密只住在服务器上）已经成立——
管道不改变那些决定，只是把"合并之后发生的事"自动化。

---

## 一、原则先行

**镜像是唯一制品，服务器不构建。**
今天服务器上跑 `docker compose up --build`，意味着生产机器要装 Go、拉依赖、吃 CPU 编译——
而且两次构建可能产出不同的二进制。改成：GitHub Actions 构建一次，推到 GHCR，
服务器只做 `pull`。同一个 SHA 永远对应同一组字节，回滚变成"pull 旧标签"。

**部署走 SSM，不开 SSH。**
机器上没有 22 端口（这是 DEPLOY.md 定下的），部署命令通过
`aws ssm send-command` 送进去。GitHub Actions 拿到的 AWS 身份来自
**OIDC 联合**——GitHub 向 AWS 出示"我是 yefch3/erp-go 仓库 main 分支的一次运行"，
AWS 换发 15 分钟的临时凭据。**仓库的 Secrets 里不存任何 AWS 长期密钥**，
所以也没有可以被偷走的东西。

**秘密不进管道。**
`.env` 继续只住在服务器上（见 DEPLOY.md 第三节）。workflow 文件里出现的只有
角色 ARN、区域、实例 ID 这类"知道了也没用"的标识符。

**迁移与代码同一节奏，且向后兼容一个版本。**
部署顺序是"先迁库、后换容器"，中间有几秒新库配旧代码——所以任何迁移都不许
在同一个版本里做破坏性变更（删列、改类型分两个版本走：先加后删）。
这条规矩从今天开始就算数，不等管道上线。

---

## 二、全景图

```
PR 合并进 main
   │
   ▼
[build.yml]  make ci 全绿（现有闸门，不动）
   │
   ▼
构建 11 个服务镜像 + 1 个前端镜像
   │        （deploy/Dockerfile --build-arg SERVICE=xxx，buildx 层缓存）
   ▼
推送 GHCR：ghcr.io/yefch3/erp-go/<name>:<git-sha> 和 :latest
   │
   ▼
[deploy.yml]  GitHub OIDC → AssumeRole → aws ssm send-command
   │
   ▼
服务器上 /opt/erp/deploy.sh <git-sha>：
   git fetch && checkout <sha>     ← compose 文件和迁移脚本跟着同一个 SHA 走
   docker compose pull             ← 只拉镜像，不构建
   goose 迁移（全部服务）
   docker compose up -d
   健康检查                        ← 失败 → 自动回滚到 last-good
   echo <sha> > /opt/erp/last-good
```

代码从 GitHub 走，秘密在服务器上原地不动，两条路互不相交——
这正是「.env 不上传，怎么配置」那个问题的管道版答案。

---

## 三、制品：GHCR 镜像

| 镜像 | 来源 | 大小量级 |
|---|---|---|
| `ghcr.io/yefch3/erp-go/<服务名>` ×11 | `deploy/Dockerfile`（现成的，构建参数选服务） | distroless，每个 ~20MB |
| `ghcr.io/yefch3/erp-go/frontend` | `nginx:alpine` + `npm run build` 的 dist（新增一个小 Dockerfile） | ~50MB |

标签策略：每次构建打**两个**标签——`<git-sha>`（不可变，部署和回滚都用它）
和 `latest`（只是方便人眼看"现在最新是哪个"，部署脚本永远不用它）。

前端容器绑 `127.0.0.1:8081`，host nginx（443 + certbot）把 `/` 转给它、
`/api` 转给网关 8080。所有对外只有 443 的约定不变。

生产用一个 compose 覆盖文件 `deploy/docker-compose.prod.yml`：
内容只有 12 段 `image: ghcr.io/...:${ERP_SHA}`，叠在现有 services 文件上，
把 `build:` 替换掉。开发机不受影响，照旧 `--build`。

---

## 四、凭据都放在哪（一张表说清）

| 凭据 | 放哪 | 用途 | 备注 |
|---|---|---|---|
| `GITHUB_TOKEN` | GitHub 自动注入 | Actions 推镜像到 GHCR | 每次运行自动生成自动过期，无需管理 |
| OIDC → IAM 角色 | AWS IAM（信任策略锁定本仓库 main 分支） | Actions 调 SSM | **无长期密钥可泄露**；角色权限只有 `ssm:SendCommand` + 读命令结果，锁定到那一台实例 |
| GHCR 拉取 PAT | 服务器 root 的 docker 凭据里（建机时 `docker login ghcr.io` 一次） | 服务器拉私有镜像 | fine-grained PAT，只勾 Packages read；到期日历上记一笔 |
| `.env` 全套 | 只在服务器 `/opt/erp/deploy/.env` | 服务运行 | 管道全程不碰它 |

仓库 Settings → Secrets 里最终只有两三个**标识符**（角色 ARN、区域、实例 ID）。
它们其实不算秘密，放 Secrets 只是习惯上的整洁。

---

## 五、服务器端：/opt/erp/deploy.sh

建机时（`deploy/aws/04-ec2.sh` 之后）放好一次，之后只被 SSM 调用：

```bash
#!/usr/bin/env bash
set -euo pipefail
SHA="$1"
cd /opt/erp/repo

git fetch origin && git checkout --quiet "$SHA"

export ERP_SHA="$SHA"
COMPOSE="docker compose -f deploy/docker-compose.services.yml -f deploy/docker-compose.prod.yml"

$COMPOSE pull --quiet
make migrate                       # goose，向后兼容一个版本（见第一节）
$COMPOSE up -d --remove-orphans

# 健康检查：给 30 秒起身，然后问网关
sleep 30
if ! curl -fsS --max-time 5 http://127.0.0.1:8080/api/healthz > /dev/null; then
    echo "健康检查失败，回滚到 $(cat /opt/erp/last-good)"
    git checkout --quiet "$(cat /opt/erp/last-good)"
    ERP_SHA="$(cat /opt/erp/last-good)" $COMPOSE up -d --remove-orphans
    exit 1
fi

echo "$SHA" > /opt/erp/last-good
```

几个刻意的选择：

- **服务器保留一个仓库克隆**（`/opt/erp/repo`），不是为了构建，是为了让
  compose 文件和迁移脚本跟镜像**同一个 SHA**。"配置从 git 走、二进制从
  registry 走"，两者用同一个提交对齐。仓库里没有秘密，克隆不违反纪律。
- **回滚是自动的**：健康检查不过，脚本自己退回 last-good 并把失败传回
  Actions（SSM 会带回退出码，workflow 红灯）。手动回滚 = 用旧 SHA 手动触发
  一次 deploy workflow，不需要任何新机制。
- **迁移失败会中止整个部署**（`set -e`），旧容器原样运行——这就是要求迁移
  向后兼容的原因：迁移成功但容器起不来时，旧代码还能跑在新库上。

---

## 六、两个 workflow 的骨架

`.github/workflows/build.yml`（第一期就建）：

```yaml
name: build
on:
  push:
    branches: [main]
permissions:
  contents: read
  packages: write
jobs:
  images:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: 11 个服务 + 前端
        run: |
          for s in iam masterdata fx product approval export \
                   inventory procurement shipping mail gateway; do
            docker buildx build --push \
              --build-arg SERVICE=$s \
              --cache-from type=gha --cache-to type=gha,mode=max \
              -f deploy/Dockerfile \
              -t ghcr.io/yefch3/erp-go/$s:${{ github.sha }} \
              -t ghcr.io/yefch3/erp-go/$s:latest .
          done
          docker buildx build --push \
            --cache-from type=gha --cache-to type=gha,mode=max \
            -f deploy/frontend.Dockerfile \
            -t ghcr.io/yefch3/erp-go/frontend:${{ github.sha }} \
            -t ghcr.io/yefch3/erp-go/frontend:latest .
```

（名单就是 `deploy/docker-compose.services.yml` 里的 11 个 `build:` 服务；
`pixel-proxy` 用现成的 nginx 官方镜像，不需要我们构建。落地时再核对一遍。）

`.github/workflows/deploy.yml`（第二期起）：

```yaml
name: deploy
on:
  workflow_dispatch:          # 第二期：手动点按钮，可指定 SHA（回滚也走这里）
    inputs:
      sha:
        description: 要部署的提交（默认最新 main）
        required: false
  # 第三期把下面两行解开注释，合并即部署：
  # workflow_run:
  #   workflows: [build], types: [completed], branches: [main]
permissions:
  id-token: write             # OIDC 的钥匙孔
  contents: read
concurrency: production       # 同一时间只允许一个部署在跑
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production   # 在仓库设置里可以给它加"需要批准"
    steps:
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.AWS_DEPLOY_ROLE_ARN }}
          aws-region: us-west-2
      - name: SSM 执行部署脚本
        run: |
          SHA="${{ inputs.sha || github.sha }}"
          CMD_ID=$(aws ssm send-command \
            --instance-ids "${{ secrets.EC2_INSTANCE_ID }}" \
            --document-name AWS-RunShellScript \
            --parameters commands="/opt/erp/deploy.sh $SHA" \
            --query Command.CommandId --output text)
          aws ssm wait command-executed --command-id "$CMD_ID" \
            --instance-id "${{ secrets.EC2_INSTANCE_ID }}"
          aws ssm get-command-invocation --command-id "$CMD_ID" \
            --instance-id "${{ secrets.EC2_INSTANCE_ID }}" \
            --query '{status:Status,out:StandardOutputContent,err:StandardErrorContent}'
```

---

## 七、分期落地

**第一期（现在就能做，不需要 AWS）**
- [ ] 网关加 `GET /api/healthz`（无鉴权，只回 200 和版本号——部署脚本和以后的监控都靠它）
- [ ] 新增 `deploy/frontend.Dockerfile` 和 `deploy/docker-compose.prod.yml`
- [ ] 建 `build.yml`，验证 12 个镜像能出现在 GHCR 页面上
- [ ] 一次本机演练：`ERP_SHA=<sha> docker compose -f ... -f prod.yml pull && up -d` 拉 GHCR 镜像跑起来

**第二期（EC2 建好之后）**
- [ ] AWS 里建 OIDC 身份提供商 + 部署角色（信任策略锁 `repo:yefch3/erp-go:ref:refs/heads/main`）
- [ ] 建机脚本补上：克隆仓库到 `/opt/erp/repo`、放 `deploy.sh`、`docker login ghcr.io`
- [ ] 建 `deploy.yml`，手动触发部署一次，验证含回滚路径（故意部署一个坏 SHA 看它退回来）

**第三期（试点稳定后）**
- [ ] `deploy.yml` 解开自动触发，合并 main 即部署
- [ ] GitHub 环境 `production` 上加保护规则（可选：需要一次人工批准）

试点期建议停在第二期一段时间：**构建自动、部署手动**。300 人公司的 ERP，
"部署"应该是有人清醒地按下的按钮；等回滚路径被真实使用过几次、信任建立了，
再把按钮交给合并动作。

---

## 八、成本与配额

- **Actions 分钟数**：私有仓库免费档每月 2000 分钟。一次全量构建（有 buildx
  缓存时）约 8–15 分钟，只在合并 main 时跑——每月几十次合并用不到一半额度。
  缓存失效的冷构建会到 25 分钟左右，偶发，可接受。
- **GHCR 存储**：distroless 镜像很小，12 个镜像 × 保留最近若干 SHA ≈ 1–2GB。
  免费档 500MB 会不够，**需要留意**：要么定期清老标签（GitHub 有现成的
  retention action，保留最近 10 个 SHA 即可），要么升 Pro。清理写进第一期落地项里。
- **AWS 侧零新增成本**：SSM、OIDC 都不收费。

---

## 九、这个方案明确不做的事

- **不做蓝绿/金丝雀**：一台机器的试点，停机窗口是几秒钟的 `up -d`，
  为它引入双环境不值得。等有第二台机器再谈。
- **不做 Kubernetes**：11 个容器一台机器，compose 够用且人人看得懂。
  将来若扩展到需要它，路线已备好：[K8S-PATH.md](./K8S-PATH.md)
  （触发条件、逐项搬家对照表、.env → ConfigMap/Secret 的去向）。
- **不把 `.env` 搬进 GitHub Secrets**：秘密的家在服务器上（将来在 SSM
  Parameter Store），管道只该知道"去哪台机器执行"，不该知道业务秘密。
