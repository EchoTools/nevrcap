default: fmt vet lint test

fmt:
    gofmt -w . && goimports -w .

vet:
    go vet ./...

lint:
    golangci-lint run ./...

test:
    go test -race -count=1 ./...

test-leak:
    GOEXPERIMENT=goroutineleakprofile go test -race -count=1 ./...

bench:
    go test -bench=. -benchmem -benchtime=3s ./...

fuzz target="FuzzParseFrameLine" duration="30s":
    go test -fuzz={{target}} -fuzztime={{duration}} ./pkg/codec/

verify file:
    go run ./cmd/tapedeck verify {{file}}

stats file:
    go run ./cmd/tapedeck stats {{file}}

build:
    go build -o tapedeck ./cmd/tapedeck/

build-static:
    CGO_ENABLED=0 go build -ldflags="-s -w" -o tapedeck ./cmd/tapedeck/

modernize:
    go fix ./... && go mod tidy

audit:
    govulncheck ./...

ci: fmt vet lint test-leak audit
