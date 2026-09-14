#!/bin/sh
set -eu
# Only run on the D1 internal network; this database is a disposable copy of
# D1 test fixtures, never a production or original checkout database.
[ "${D1_CONTAINER_NETWORK:-}" = erp-d1-20260906_isolated ]
dsn='postgres://erp_procurement:erp_procurement_pw@postgres:5432/erp_d1_upgrade?sslmode=disable'
goose -dir /src/services/procurement/db/migrations postgres "$dsn" down-to 58
goose -dir /src/services/procurement/db/migrations postgres "$dsn" up
