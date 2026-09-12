# D1 测试复现

这些脚本仅面向 `erp-d1-20260906` 隔离环境。入口 `acceptance.ps1` 会先执行 `scripts/d1-environment.ps1 -Action Check`，验证独立分支、网络、卷、端口、DSN和构建目录。测试账号及密码均为本次隔离夹具，无真实邮箱凭据。

在本次 worktree 根目录用 PowerShell 7 执行：

```powershell
./scripts/d1-environment.ps1 -Action Check
./docs/audits/erp-v1-d0-20260906/d1-scripts/acceptance.ps1
./docs/audits/erp-v1-d0-20260906/d1-scripts/extended.ps1
./docs/audits/erp-v1-d0-20260906/d1-scripts/protection-acceptance.ps1
./docs/audits/erp-v1-d0-20260906/d1-scripts/mail-acceptance.ps1
./docs/audits/erp-v1-d0-20260906/d1-scripts/failure-acceptance.ps1
./docs/audits/erp-v1-d0-20260906/d1-scripts/large-attachment-acceptance.ps1
```

`extended.ps1` 生成本轮126产品夹具，并更新此目录 `ui-inquiry.json`。脚本再次运行会创建新的测试询盘，不会复用或清理原业务数据。`protection-acceptance.ps1` 的失败注入限定新建测试case，`finally` 删除测试触发器；`failure-acceptance.ps1` 仅停启 D1 export 容器。另一租户夹具由 `other-tenant.sql` 初始化，只能在D1 IAM库执行。

Go隔离集成测试：在根目录将对应包编译为 Linux 二进制（`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c`），再在 `erp-d1-20260906_isolated` 网络运行。挂载本worktree为 `/src:readonly`。

- procurement：`D1_CONTAINER_NETWORK=erp-d1-20260906_isolated`，`D1_PROCUREMENT_TEST_DSN=postgres://erp_procurement:erp_procurement_pw@postgres:5432/erp_procurement?sslmode=disable`；运行 `-test.run=^TestD1 -test.v=true`。
- export：同网络标识，`D1_EXPORT_TEST_DSN=postgres://erp_export:erp_export_pw@postgres:5432/erp_export?sslmode=disable`；运行 `-test.run=^TestD1WithdrawalFence$ -test.v=true`。
- 旧功能局部回归另设置对应 `PROCUREMENT_TEST_DSN` 或 `EXPORT_TEST_DSN` 为上述D1库；不能使用默认开发库地址。
- IAM预置角色对照测试使用一次性库 `erp_d1_iam_migrations`，不是运行中的IAM库。

未提供D1 DSN的普通Go测试会跳过D1专项测试；**跳过不计验收通过**。本次日志中的专项测试均提供DSN并实际执行。

`migrate.sh` 只供D1内部网络的迁移工具容器使用。`upgrade.sh` 只针对 `erp_d1_upgrade`：该库来自D1夹具副本，降至58再升61验证历史快照。禁止将这些地址替换为现有业务数据库。
