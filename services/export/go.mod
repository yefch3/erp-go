module github.com/sgao19/erp-go/services/export

go 1.25.0

require (
	github.com/jackc/pgx/v5 v5.7.2
	github.com/sgao19/erp-go/gen v0.0.0-00010101000000-000000000000
	github.com/sgao19/erp-go/pkg v0.0.0
	github.com/shopspring/decimal v1.4.0
	google.golang.org/grpc v1.82.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/sgao19/erp-go/gen => ../../gen
	github.com/sgao19/erp-go/pkg => ../../pkg
)
