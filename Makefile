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

.PHONY: certs-ca
certs-ca:
	@mkdir -p certs
	openssl genrsa -out certs/ca.key 4096
	openssl req -new -x509 -key certs/ca.key -sha256 -subj "/CN=localhost" -days 365 -out certs/ca.crt

.PHONY: certs-server
certs-server:
	@mkdir -p certs
	openssl genrsa -out certs/server.key 4096
	openssl req -new -key certs/server.key -subj "/CN=localhost" -out certs/server.csr
	echo "subjectAltName = DNS:localhost,IP:127.0.0.1" > certs/san.ext
	openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/server.crt -days 365 -sha256 -extfile certs/san.ext

.PHONY: certs-client
certs-client:
	@mkdir -p certs
	openssl genrsa -out certs/client.key 4096
	openssl req -new -key certs/client.key -subj "/CN=client" -out certs/client.csr
	openssl x509 -req -in certs/client.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/client.crt -days 365 -sha256

.PHONY: certs
certs: certs-ca certs-server certs-client
