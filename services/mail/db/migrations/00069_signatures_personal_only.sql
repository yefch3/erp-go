-- +goose Up

-- 签名只属于写它的人（2026-09-18 定的）。
--
-- 原来有两层：「公司统一」（owner_type = 'TENANT'，owner_id = 0，谁都能选、
-- 谁都能改）和「仅自己」。老板的口径是签名是各人自己的数据，别人看不到，
-- 于是「公司的」这一层整个拿掉：清单、改、删、发信时取签名，从这个版本起
-- 一律只认 owner_id = 本人。
--
-- 这一层里已有的行 owner_id = 0，没有人可以接手，只能删。指着它们的草稿
-- 改回「不带签名」，免得发的时候撞上一句「签名不存在」。改到这天生产库里
-- 这一层是空的（0 行，草稿和已发的信也没有指向它们的），所以这两句在生产
-- 上是零行；写在这里是给别的库和以后重放用的。
UPDATE email_drafts SET signature_id = 0
WHERE signature_id IN (SELECT id FROM email_signatures WHERE owner_type = 'TENANT');
DELETE FROM email_signatures WHERE owner_type = 'TENANT';

-- owner_type 这一列从这个版本起代码不再读也不再写（插入走默认值
-- 'EMPLOYEE'）。列本身留到下一个版本再删：部署是先迁库、后换容器，换之前
-- 还在跑的旧代码仍然 SELECT 它，见 scripts/check-migration-safety.sh。

-- +goose Down

-- 删掉的行回不来；列没动，没什么可退的。
