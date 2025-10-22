.PHONY: test
test:
	sudo -E go test -race -timeout 10s ./...

.PHONY: testv
testv:
	sudo -E go test -v -race -timeout 10s ./...

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
	sudo -E go test -buildvcs -coverprofile=coverage.out ./... \
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

.PHONY: build-cli
build-cli:
	go build -o ./tmp/bin/jobctl -v ./cmd/jobctl

.PHONY: run-cli
run-cli: build-cli
	./tmp/bin/jobctl

.PHONY: build-server
build-server:
	go build -o ./tmp/bin/jobserver -v ./cmd/jobserver

.PHONY: run-server
run-server: build-server
	sudo ./tmp/bin/jobserver

.PHONY: build
build: build-server build-cli

.PHONY: certs-clean
certs-clean:
	@rm -rf certs

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
	echo "subjectAltName = DNS:localhost,IP:127.0.0.1" > certs/server.ext
	echo "keyUsage = critical,digitalSignature,keyEncipherment" >> certs/server.ext
	echo "extendedKeyUsage = serverAuth" >> certs/server.ext
	openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/server.crt -days 365 -sha256 -extfile certs/server.ext

.PHONY: certs-client-operator
certs-client-operator:
	@mkdir -p certs
	openssl genrsa -out certs/client-operator.key 4096
	openssl req -new -key certs/client-operator.key -subj "/CN=alice/OU=operator" -out certs/client-operator.csr
	echo "keyUsage = critical,digitalSignature" > certs/client-operator.ext
	echo "extendedKeyUsage = clientAuth" >> certs/client-operator.ext
	openssl x509 -req -in certs/client-operator.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/client-operator.crt -days 365 -sha256 -extfile certs/client-operator.ext

.PHONY: certs-client-viewer
certs-client-viewer:
	@mkdir -p certs
	openssl genrsa -out certs/client-viewer.key 4096
	openssl req -new -key certs/client-viewer.key -subj "/CN=bob/OU=viewer" -out certs/client-viewer.csr
	echo "keyUsage = critical,digitalSignature" > certs/client-viewer.ext
	echo "extendedKeyUsage = clientAuth" >> certs/client-viewer.ext
	openssl x509 -req -in certs/client-viewer.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/client-viewer.crt -days 365 -sha256 -extfile certs/client-viewer.ext

.PHONY: certs
certs: certs-ca certs-server certs-client-operator certs-client-viewer
