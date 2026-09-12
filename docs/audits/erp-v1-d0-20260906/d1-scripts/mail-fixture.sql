INSERT INTO email_inbound(tenant_id,account_id,owner_id,imap_uid,from_email,from_name,to_email,subject,body_text,has_attachments)
VALUES(1,1,2,987654,'client@d1.example.test','D1 Mail Contact','s1@d1.example.test','D1 source mail','Steel Q235 3 MT',true)
ON CONFLICT(tenant_id,account_id,folder,imap_uid) DO NOTHING;
INSERT INTO email_inbound_attachments(tenant_id,inbound_id,file_name,content_type,file_size,file_key)
SELECT 1,id,'D1-source.txt','text/plain',20,'inquiry/1/16/76b8ec3fc51f183681e3bb3623358bff' FROM email_inbound WHERE tenant_id=1 AND imap_uid=987654 AND NOT EXISTS(SELECT 1 FROM email_inbound_attachments a WHERE a.inbound_id=email_inbound.id);
SELECT id FROM email_inbound WHERE tenant_id=1 AND imap_uid=987654;