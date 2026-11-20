.PHONY: all dev test lint build run clean docker-up docker-down worker api

# Default target
all: build

# Development Mode (starts pre-compiled API server instantly)
dev:
	@echo "Starting Go API in development mode..."
	cd apps/api && .\api_server.exe

# Run Go & Python Unit Tests
test:
	@echo "Running Go unit tests..."
	cd apps/api && go test -v ./...
	@echo "Running Python worker tests..."
	cd apps/worker/src && python test_pipeline.py && python test_user_video.py

# Linting
lint:
	@echo "Checking Go formatting..."
	cd apps/api && go vet ./...
	@echo "Checking Python syntax..."
	python -m py_compile apps/worker/src/*.py

# Build Native Binary
build:
	@echo "Compiling Go binary..."
	cd apps/api && go build -o ../../api_server ./cmd/main.go
	@echo "Build successful: ./api_server"

# Run Production Server
run: build
	./api_server

# Build and run API server only
api:
	cd apps/api && go run ./cmd/main.go

# Test worker standalone
worker:
	@echo "Worker entrypoint: apps/worker/src/main.py"
	python apps/worker/src/main.py --help

# Clean temporary files & builds
clean:
	@echo "Cleaning binaries and temporary storage..."
	rm -f api_server apps/api/api_server apps/api/api_server.exe
	python scripts/cleanup.py --ttl-hours 0

# Docker Compose Commands
docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
