-- +goose Up
-- Repair migration. 00036/00038/00039/00040 granted their new permissions
-- with a hardcoded tenant_id = 1: any SUPER_ADMIN belonging to another
-- tenant — one bootstrapped before those migrations ran — never received
-- them. The files are fixed for fresh databases; this re-runs the grants
-- the right way for databases where the broken versions already ran.
-- Idempotent by ON CONFLICT, and a no-op where nothing was missing.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN (
    'procurement:invoice:read', 'procurement:invoice:write',
    'procurement:payment:read', 'procurement:payment:write',
    'procurement:recon:read', 'procurement:order:close')
ON CONFLICT DO NOTHING;

-- +goose Down
-- A repair grant cannot be told apart from a legitimate one after the fact,
-- so an honest Down takes nothing back.
SELECT true;
