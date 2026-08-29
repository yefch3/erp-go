-- +goose Up

-- 那份对账单本身。
--
-- 银行流水这本账现在的定位是**纯记录**：员工手工登记一条，把银行给的
-- statement（PDF）传上来，这一条就是那笔钱的凭证。核销不再从这里出发，
-- 所以这一列不参与任何判断——它只回答「这条记录背后那张纸在哪」。
--
-- 一行一份，用列不用表：一条流水对应的就是一份对账单，再传一次是**替换**
-- 而不是追加，和 supplier_invoices.attachment_key 同一个道理、同一套代码。
--
-- 注意不要复用 source_file：那一列存的是 CSV 导入时的文件名字符串（手工
-- 登记时写空串），不是对象存储里的 key，两者长得像但不是一回事。
ALTER TABLE bank_transactions
    ADD COLUMN attachment_key VARCHAR(500) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE bank_transactions DROP COLUMN attachment_key;
