-- +goose Up

-- 把历史的发票扫描件搬进对账页的凭证表。
--
-- **不搬就是把它们锁在库里。** 那些 key 存在 supplier_invoices.attachment_key，
-- 唯一的读出口是 GetSupplierInvoice / ListSupplierInvoices，唯一的界面是供应商
-- 发票页——00038 那一版把页面和两条读路由一起下线了，却只 CREATE TABLE、没有
-- 回填。结果是：PDF 还在桶里，但系统里再没有任何角色、任何地址能拿到它的
-- 下载链接。而「留凭证」正是那次改造声称要保住的能力。
--
-- **归属靠发票行**：一张发票的行各自带 po_id，所以一张发票的扫描件会挂到它
-- 涉及的每一张采购单上（DISTINCT 去重）。这是有意的——那张纸对这几张单都是
-- 证据，不是只对其中一张。
--
-- object_key 原样搬，不改前缀：它指向桶里已经存在的对象，改了就指空。
-- 新上传走 po-recon/ 前缀，两种前缀在这张表里并存，读的时候都只是拿去签名。
-- （写入那头的前缀校验只管新上传，见 app/objectkey.go。）
INSERT INTO purchase_order_recon_files
  (tenant_id, po_id, object_key, file_name, note, uploaded_by_id, uploaded_by_name, created_at)
SELECT DISTINCT ON (il.po_id, si.attachment_key)
       si.tenant_id,
       il.po_id,
       si.attachment_key,
       -- key 的形状是 前缀/{16位hex}-{原文件名}，第一个 '-' 之后就是文件名。
       -- 拆不出来就退回整个 key，宁可名字难看也不要空白。
       coalesce(nullif(substring(si.attachment_key from '[^/]*$'), ''), si.attachment_key),
       '发票 ' || si.invoice_no,
       0,
       '（历史发票扫描件）',
       si.created_at
  FROM supplier_invoices si
  JOIN supplier_invoice_lines il
    ON il.tenant_id = si.tenant_id AND il.invoice_id = si.id AND il.po_id IS NOT NULL
 WHERE si.attachment_key <> ''
   AND si.status <> 'VOID';

-- +goose Down
-- 只删这次搬进来的那些（靠上传人那句话认）。员工自己传的一律不动。
DELETE FROM purchase_order_recon_files WHERE uploaded_by_name = '（历史发票扫描件）';
