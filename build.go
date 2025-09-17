package nevrcap

//go:generate protoc -I./proto -I. --go_out=./gen/go --go_opt=paths=source_relative rtapi/telemetry_v1.proto
//go:generate protoc -I./proto -I. --go_out=./gen/go --go_opt=paths=source_relative apigame/http_v1.proto