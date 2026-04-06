.PHONY: all build server desktop dev db-up db-down migrate test clean

all: build

# Build targets
build: server desktop

server:
	go build -o bin/sfarm-server ./cmd/server

desktop:
	cd desktop && wails build -platform linux/amd64

# Development
dev: db-up
	go run ./cmd/server

db-up:
	docker-compose up -d postgres

db-down:
	docker-compose down

migrate:
	go run ./cmd/server  # migrations run on startup

# Dependencies
deps:
	go mod tidy
	cd web && npm install
	cd desktop/frontend && npm install
	cd shared-ui && npm install

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
	rm -rf desktop/build/bin/

# Docker
docker-build:
	docker build -f Dockerfile.server -t sfarm-server .

# Install tools
tools:
	go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
