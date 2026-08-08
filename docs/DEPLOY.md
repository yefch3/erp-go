# 上云清单

这份清单存在的理由：**部署那天不会有人重新想一遍这些事。** 到时候执行的是 `docker compose up`，用的是仓库里那份配置——所以每一条「上线前记得改」如果不写下来，就一定会漏。

---

## 一、只开一个端口

```
443    网页（https）
```

**其余一个都不要开。**

仓库里所有内部端口已经绑在 `127.0.0.1` 上了（11 个服务 + Postgres + Redis + Kafka + MinIO），云上**保持原样即可，不需要再改**。`127.0.0.1` 在云主机上和在笔记本上是同一个意思：只有这台机器自己能连。

三件容易搞错的事：

**① 不要指望防火墙。** Docker 发布端口时会自己往 iptables 里写规则，**位置在 ufw 的规则之前**。你配好 ufw、以为 5432 关着，实际它照常应答。绑定地址是真正起作用的那道控制，防火墙是第二道。

**② 内部服务不验证身份。** 它们只信网关递过来的那句「这位是 X 公司的 Y 员工」——这个分工是对的，前提是外面碰不到它们。**任何能连上 9001 的人，不用密码就是所有租户的管理员。**

**③ 云主机开机就在被扫。** 5432、6379、9000 都在自动扫描器的必扫列表里。这不是有人针对你，是背景噪音，但结果一样。

要从云上连数据库调试时，走 SSH 隧道或者 `docker exec`，不要为此打开端口：

```bash
ssh -L 5433:127.0.0.1:5433 你的服务器     # 之后本机 psql -h 127.0.0.1 -p 5433
```

---

## 二、网关放在反向代理后面

`8080` 是唯一从外面能连的端口，它应该只对本机开放，由 nginx 占着 443 转进来。

改 `deploy/docker-compose.services.yml` 里 gateway 那段：

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

nginx 那边必须**覆写**而不是追加转发头，否则下面那个开关会被伪造：

```nginx
proxy_set_header X-Forwarded-For $remote_addr;   # 覆写，不是 $proxy_add_x_forwarded_for
proxy_set_header Host $host;
proxy_pass http://127.0.0.1:8080;
```

---

## 三、必须设的环境变量

放在 `deploy/.env`（不进代码仓库）。

| 变量 | 为什么 |
|---|---|
| `JWT_SECRET` | **最要紧的一条。** 默认值是 `dev-secret-change-in-production`，知道这串字符的人可以伪造任何人的登录态——不需要密码，不经过登录页，所有限速都碰不到他。换成随机值：`openssl rand -base64 48` |
| `TRUST_PROXY_HEADERS=1` | 只有在上面那个 nginx 配置到位之后才开。不开的话，登录限速会把**全世界当成同一个来源**（都来自 nginx），20 次错密码锁住所有人；乱开的话，转发头可以伪造，等于没有限速 |
| `ADMIN_EMAIL` / `COMPANY_MAIL_DOMAINS` | 第一个租户和它的域名 |
| `ADMIN_INITIAL_PASSWORD` | 默认值是 `admin123`，**低于系统要求所有人达到的门槛**。启动日志里会有一行警告 |
| `MAIL_CRED_KEY` | 员工邮箱凭据的加密密钥。**没有它服务起不来**，这是有意的 |
| `GOOGLE_OAUTH_CLIENT_SECRET` | 用 Google 登录邮箱时需要 |
| `FRONTEND_BASE_URL` | 激活链接里的域名，必须是外面能打开的那个 |
| `MAIL_PUBLIC_BASE_URL` | 发出去的邮件里图片的地址，同上 |

**`COMPANY_MAIL_DOMAINS` 要把 `gmail.com` 去掉。** 它在开发配置里是为了用 Gmail 的 `+别名` 造测试员工；留在生产等于任何一个 Gmail 地址都能路由到你的公司。

---

## 四、还留在代码仓库里的明文口令

这些目前写死在 `deploy/docker-compose.*.yml` 里，上线前要换成 `.env` 变量：

```
10 处  DB_DSN 里的 erp_<服务>_pw       每个服务一个库密码
 4 处  MINIO_SECRET_KEY: erp_dev_password
```

它们在内网里，但和「上面所有端口都绑了本机」是同一道防线——**只有一道**。

---

## 五、上线前逐条确认

- [ ] `nmap` 扫一遍自己的公网 IP，**除了 443 什么都不该开**
- [ ] `JWT_SECRET` 已换成随机值（会把所有人登出一次，选个不忙的时候）
- [ ] nginx 覆写了 `X-Forwarded-For`，并且 `TRUST_PROXY_HEADERS=1`
- [ ] `COMPANY_MAIL_DOMAINS` 里没有公共邮箱域名
- [ ] 管理员密码不是 `admin123`（看启动日志有没有那行警告）
- [ ] 库密码和 MinIO 密钥已经不是仓库里那几个
- [ ] 数据库有备份，而且**恢复流程真的走过一遍**
