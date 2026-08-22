#!/bin/sh
# One database + one owner account per service: cross-database access is
# impossible by construction, which is what enforces data ownership.
set -e

for svc in iam masterdata product fx approval export procurement inventory mail shipping reporting audit; do
  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres <<SQL
    CREATE DATABASE erp_${svc};
    CREATE USER erp_${svc} WITH PASSWORD 'erp_${svc}_pw';
    ALTER DATABASE erp_${svc} OWNER TO erp_${svc};
    -- PostgreSQL grants CONNECT to PUBLIC on every new database by default,
    -- which would let any service account reach any other service's data.
    -- Revoking it is what makes "one database per service" an enforced
    -- boundary instead of a naming convention.
    REVOKE CONNECT ON DATABASE erp_${svc} FROM PUBLIC;
SQL
done

# 迁移生命周期测试会执行 DownTo(0)，必须使用独立数据库，避免与同时运行的
# 船期业务集成测试互相拆表。放在这里而不是 CI 的 workflow 里，本地 compose
# 首次启动也会建出同一个库，make ci 在本地才跑得动这个测试。
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres <<SQL
  CREATE DATABASE erp_shipping_migrations OWNER erp_shipping;
  REVOKE CONNECT ON DATABASE erp_shipping_migrations FROM PUBLIC;
  CREATE DATABASE erp_procurement_migrations OWNER erp_procurement;
  REVOKE CONNECT ON DATABASE erp_procurement_migrations FROM PUBLIC;
  CREATE DATABASE erp_masterdata_migrations OWNER erp_masterdata;
  REVOKE CONNECT ON DATABASE erp_masterdata_migrations FROM PUBLIC;
SQL

# Dashboards and reports read through a separate account with SELECT only:
# "报表只能读取正式业务数据" enforced at the database layer.
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d erp_reporting <<SQL
  CREATE USER erp_reporting_ro WITH PASSWORD 'erp_reporting_ro_pw';
  GRANT CONNECT ON DATABASE erp_reporting TO erp_reporting_ro;
  GRANT USAGE ON SCHEMA public TO erp_reporting_ro;
  ALTER DEFAULT PRIVILEGES FOR ROLE erp_reporting IN SCHEMA public
      GRANT SELECT ON TABLES TO erp_reporting_ro;
SQL
