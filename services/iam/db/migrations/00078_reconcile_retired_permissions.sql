-- +goose Up
-- 00070 retired partial quality release, but editing/replaying an old
-- migration does not change databases that had already recorded it as
-- applied. Reconcile those existing installations explicitly.
DELETE FROM role_permissions rp
USING permissions p
WHERE rp.permission_id = p.id
  AND p.code = 'quality:release:decide';

DELETE FROM permissions WHERE code = 'quality:release:decide';

-- +goose Down
-- The feature remains retired, so rollback does not expose it again.
SELECT 1;
