#!/bin/sh
set -eu
for svc in iam masterdata product fx approval export procurement inventory mail shipping reporting audit; do
  [ -d "/src/services/$svc/db/migrations" ] || continue
  echo "D1 migrate $svc"
  goose -dir "/src/services/$svc/db/migrations" postgres "postgres://erp_${svc}:erp_${svc}_pw@postgres:5432/erp_${svc}?sslmode=disable" up
 done