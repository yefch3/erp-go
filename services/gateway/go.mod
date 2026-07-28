module github.com/sgao19/erp-go/services/gateway

go 1.25.0

require (
	github.com/go-chi/chi/v5 v5.1.0
	github.com/sgao19/erp-go/gen v0.0.0
	github.com/sgao19/erp-go/pkg v0.0.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/sgao19/erp-go/gen => ../../gen
	github.com/sgao19/erp-go/pkg => ../../pkg
)
