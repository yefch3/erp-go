-- +goose Up
-- Multi-tenant invariant: omitting tenant_id must fail instead of silently
-- assigning the row to the first company. This scans the service's own
-- schema so it also covers tables created by older migrations.
-- +goose StatementBegin
DO $$
DECLARE
    col RECORD;
    constraint_name TEXT;
BEGIN
    FOR col IN
        SELECT table_schema, table_name
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND column_name = 'tenant_id'
    LOOP
        EXECUTE format(
            'ALTER TABLE %I.%I ALTER COLUMN tenant_id DROP DEFAULT',
            col.table_schema,
            col.table_name
        );

        constraint_name := left(col.table_name, 45)
            || '_tenant_positive_' || substr(md5(col.table_schema || '.' || col.table_name), 1, 8);
        IF NOT EXISTS (
            SELECT 1
            FROM pg_constraint
            WHERE conrelid = format('%I.%I', col.table_schema, col.table_name)::regclass
              AND conname = constraint_name
        ) THEN
            EXECUTE format(
                'ALTER TABLE %I.%I ADD CONSTRAINT %I CHECK (tenant_id > 0)',
                col.table_schema,
                col.table_name,
                constraint_name
            );
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Intentionally empty: restoring DEFAULT 1 or allowing tenant 0 would
-- reintroduce cross-tenant data attribution when a caller forgets the tenant.