-- +goose Up
-- 虚拟文件夹单独一档。Gmail 的「重要」「已加星标」「全部邮件」不是真的文件夹，
-- 是贴在信上的标签：同一封收件箱的信同时出现在它们里面。00058 把它们归成
-- SYSTEM，而下一步要同步 SYSTEM 文件夹的内容——照这样同步下去，Gmail 的每封
-- 信会被存三四遍（email_inbound 的唯一键是 (租户, 信箱, 文件夹, UID)，换个
-- 文件夹就是新的一行）。
--
-- VIRTUAL：内容是别处的信的映射，不同步、不显示。靠 LIST 的属性认：
-- \All \Flagged \Important，以及 \Noselect（只是层级容器，选都选不进去）。
--
-- 这里先把已经登记的改过来，不等下一次列文件夹：同步循环读的是库里的角色，
-- 谁都没打开过的信箱也会被它扫到。
UPDATE mail_folders SET role = 'VIRTUAL'
WHERE role = 'SYSTEM' AND (host_name = '[Gmail]' OR host_name LIKE '[Gmail]/%');

-- +goose Down
UPDATE mail_folders SET role = 'SYSTEM' WHERE role = 'VIRTUAL';
