.PHONY: all build server desktop sandbox dev db-up db-down migrate test clean

all: build

# Build targets
build: sandbox server desktop

server:
	go build -o bin/sfarm-server ./cmd/server

sandbox:
	cd sandbox-core && cargo build --release
	mkdir -p bin
	cp sandbox-core/target/release/sfarm-sandbox bin/sfarm-sandbox

desktop: sandbox
	cd web && npm install && npm run build
	mkdir -p cmd/desktop/dist
	cp -r web/dist/* cmd/desktop/dist/
	go build -o bin/sfarm-desktop ./cmd/desktop

# Development
dev: db-up
	go run ./cmd/server

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

migrate:
	go run ./cmd/server  # migrations run on startup

# Dependencies
deps:
	go mod tidy
	cd web && npm install
	cd sandbox-core && cargo fetch

# Generate protobuf
proto:
	protoc --go_out=. --go-grpc_out=. internal/proto/farm/farm.proto

# Testing
test:
	go test ./... -v

# Linting
lint:
	golangci-lint run ./...

# Clean
clean:
	rm -rf bin/
	rm -rf sandbox-core/target/
	rm -rf web/dist/
	rm -rf cmd/desktop/dist/

# Install tools
tools:
	go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
