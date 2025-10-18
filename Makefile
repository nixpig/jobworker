.PHONY: test
test:
	go test -race -timeout 10s ./...

.PHONY: testv
testv:
	go test -v -race -timeout 10s ./...

.PHONY: tidy
tidy: 
	go fmt ./...
	go mod tidy -v

.PHONY: audit
audit:
	go mod verify
	go vet ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: coverage
coverage:
	go test -buildvcs -coverprofile=coverage.out ./... \
		&& go tool cover -html=coverage.out

.PHONY: clean
clean:
	rm -rf *.out tmp

.PHONY: proto
proto:
	protoc --go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/v1/job.proto

.PHONY: build-server
build-server:
	go build -o ./tmp/bin/jobserver -v ./cmd/jobserver

.PHONY: run-server
run-server: build-server
	./tmp/bin/jobserver

