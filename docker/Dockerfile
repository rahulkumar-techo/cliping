# Multi-stage build for personal low-resource VPS
# Stage 1: Build Go API server
FROM golang:1.22-bookworm AS builder
WORKDIR /app
COPY apps/api/go.mod apps/api/go.sum* ./apps/api/
WORKDIR /app/apps/api
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/api_server ./cmd/main.go

# Stage 2: Runtime image with Python 3.12 and FFmpeg
FROM python:3.12-slim-bookworm

# Install FFmpeg, sqlite3, curl, ca-certificates, and nodejs (for yt-dlp JavaScript extraction)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    sqlite3 \
    curl \
    ca-certificates \
    nodejs \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install lightweight CPU-only PyTorch (saves ~3.5 GB of heavy NVIDIA CUDA bloat)
RUN pip install --no-cache-dir torch torchvision --index-url https://download.pytorch.org/whl/cpu

# Copy Python worker requirements and install
COPY apps/worker/requirements.txt ./apps/worker/
RUN pip install --no-cache-dir -r apps/worker/requirements.txt

# Copy Go binary from builder
COPY --from=builder /app/api_server ./api_server

# Copy application files
COPY apps/web ./apps/web
COPY apps/worker ./apps/worker
COPY packages ./packages

# Create persistent storage directories
RUN mkdir -p data storage/jobs

ENV APP_ENV=production
ENV APP_PORT=8080
ENV DATABASE_PATH=/app/data/app.db
ENV STORAGE_PATH=/app/storage/jobs
ENV WORKER_SCRIPT=/app/apps/worker/src/main.py
ENV PYTHON_PATH=python3

EXPOSE 8080

CMD ["./api_server"]
