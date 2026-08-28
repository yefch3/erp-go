# 非 Excel 复杂询盘测试集

这组样例专门验证邮件转询盘在非结构化输入上的稳定性。所有公司、联系人和订单均为虚构。

## 文件

- `01_forwarded_email_body.txt`：正文转发链，包含过期数量和最终更正。
- `02_revision_with_markup.html`：HTML 修订稿，包含删除线、共享条件和跨段继承。
- `03_mobile_chat_inquiry.jpg`：手机聊天截图，规格分散在多条消息中并有口径纠正。
- `04_scanned_multipage_rfq.pdf`：三页纯扫描 PDF，续页、共享条件、后页更正和错误总计并存。
- `05_complete_mail_bundle.eml`：完整 MIME 邮件，正文加三个非 Excel 附件。
- `expected.json`：机器可读的期望行数、关键字段和必须处理的歧义。

## 验收原则

1. 明细行数必须等于 `expected_rows`，TOTAL、旧版本和删除线内容不能生成额外明细。
2. 后发且明确标记为 FINAL/correction 的值覆盖旧值，但旧值应在备注中留下可审计说明。
3. `6M`、`12 m` 等长度在数值长度列中应转换为 `6000`、`12000`；随机长度保留范围含义。
4. PCS 与 MT 不互相换算；汇总数量冲突不能靠模型猜测修正。
5. PDF 每页都必须识别，第三页的更正必须回写到第一页/第二页对应行。

运行 `generate_non_excel_cases.py` 可重复生成二进制图片、PDF 和 EML。生成结果固定使用随机种子 20260828。

## 真实模型回归

测试默认跳过，只有显式提供真实模型配置时才消耗 token：

```bash
ERP_LIVE_OPENAI=1 go test ./internal/adapter/openai \
  -run TestLiveNonExcelInquiryFixtures -count=1 -v
```

2026-08-28 基线结果：TXT 8/8、HTML 7/7、扫描 PDF 13/13；聊天截图被图片审计拒绝。拒绝原因是审计把普通共享备注也要求逐行完全复制，并非已经确认漏行。该样例故意保留，用于推动审计规则区分“必须逐行继承的业务字段”和“只需保留一次的说明”。
