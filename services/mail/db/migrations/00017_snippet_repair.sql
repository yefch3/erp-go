-- +goose Up

-- Snippets that are markup instead of a sentence.
--
-- The list shows the snippet beside the subject, so a mail whose text/plain
-- part contains HTML — plenty of bulk senders do this — put
-- "<!doctype html><html xmlns=..." where the first line should be, and a mail
-- whose stylesheet came first showed the stylesheet. The generator now routes
-- anything that looks like markup through the HTML path; this repairs what is
-- already stored, from the bodies we kept.
--
-- Same shape as 00011's received_at repair: only rows that are actually wrong
-- are touched, so re-running costs nothing and correct snippets are left
-- exactly as they are.
UPDATE email_inbound
SET snippet = left(
    btrim(
        regexp_replace(
            -- Entities we can name. The rest are dropped rather than shown
            -- raw: "&#847;" in a subject line is invisible junk, not text.
            replace(replace(replace(replace(
                regexp_replace(
                    regexp_replace(
                        -- Invisible elements first, contents and all — a
                        -- <style> body stripped of only its tags is worse
                        -- than leaving it alone.
                        regexp_replace(
                            coalesce(nullif(body_text, ''), body_html),
                            '<(script|style|head)[^>]*>.*?</\1[^>]*>', ' ', 'gis'),
                        '<[^>]*>', ' ', 'g'),
                    '&(nbsp|#160|zwnj|zwj|#8203|#847);', ' ', 'gi'),
                '&amp;', '&'), '&lt;', '<'), '&gt;', '>'), '&quot;', '"'),
            '\s+', ' ', 'g')),
    280)
-- \y, not \b: in Postgres's regex flavour \b is a literal backspace, so the
-- word-boundary spelling every other language uses here silently matches
-- nothing at all.
WHERE snippet ~ '<\s*(!doctype|html|head|body|table|div|style|meta|script|p|br|span|a)\y'
   OR snippet ~ '&(nbsp|amp|lt|gt|quot|#[0-9]+);';

-- +goose Down
-- Nothing to undo: the old value was the same text with markup in it, and
-- keeping a copy of that to restore would be keeping rubbish on purpose.
SELECT 1;
