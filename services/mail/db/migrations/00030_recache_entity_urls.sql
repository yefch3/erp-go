-- +goose Up

-- Send the messages whose picture addresses were entity-encoded back through
-- the caching pass.
--
-- The first version of the fetcher asked for the address exactly as the HTML
-- spelled it, and an HTML attribute encodes "&" as "&amp;". So every picture
-- with a query string was requested with a parameter called "amp;d" instead of
-- "d". A plain picture has no query string and was fine; a tracking pixel is
-- nothing but query string, which made this wrong in precisely the place the
-- whole feature exists for.
--
-- Clearing the stamp is all that is needed: images_cached_at IS NULL is the
-- queue, and the pass now takes the newest first, so these fill back in from
-- the most recent. Rows already stored are left alone — InsertInboundImage is
-- ON CONFLICT DO NOTHING, and a picture that was fetched successfully under
-- the mangled address is still the right picture at the right address for
-- matching purposes.
--
-- Deliberately not "clear every stamp and start again". That would re-fetch
-- 21,000 pictures to fix 323 messages' worth, which is a lot of somebody
-- else's bandwidth for no gain.
UPDATE email_inbound
SET images_cached_at = NULL
WHERE images_cached_at IS NOT NULL
  AND body_html ~ 'src="https?://[^"]*&(amp|#38|#x26);';

-- +goose Down
-- Nothing to undo: the stamp is a cache marker, and the pass rebuilds it.
SELECT 1;
