# 历史物流合同补号

适用于已上传签署合同、但物流合同编号为空的历史记录。先应用物流迁移 00030 和采购迁移 00086。

配置 `SHIPPING_BACKFILL_DSN` 和 `PROCUREMENT_BACKFILL_DSN`，均指向同一环境对应服务数据库。不要把连接凭据写入命令、日志或报告。

默认只预览，租户与本地审计报告路径必须显式指定：

```sh
go run ./services/shipping/cmd/backfill-contract-numbers -tenant 1 -report ./preview.json
```

核对预览后，在同一命令追加 `-apply` 写入；可用 `-handoff ID` 限定独立测试记录。

工具只更新物流表的 `forwarder_contract_no`、`forwarder_contract_no_generated`，以及已有物流应付记录的 `business_document_no`、`business_document_no_generated`。不调用合同上传、核验、付款接口，不创建应付记录，不更新附件、金额、核验信息、付款信息、流程状态或更新时间。已有实际合同号不覆盖；发现财务编号冲突时整批拒绝提交。

报告保存完整的前后记录与付款分配快照，并检查除以上字段外完全一致。报告包含业务资料，应保存在受控本地目录。

两服务数据库分别提交，先物流、后财务。如果财务提交失败，应使用相同范围重新运行：工具会沿用物流端已生成的编号，仅补齐财务，不再生成第二个编号。再次运行已一致的数据时不做写入。
