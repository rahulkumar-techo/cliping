#!/usr/bin/env bash
set -e

echo "=== ClipCraft AI - VPS Provisioning & Setup Script ==="
echo "Target Machine Profile: 4 Cores, 8 GB RAM, 32 GB Disk"

# 1. Install System Dependencies
if command -v apt-get >/dev/null 2>&1; then
    echo "[1/4] Installing system packages (FFmpeg, Python, SQLite)..."
    sudo apt-get update -y
    sudo apt-get install -y ffmpeg sqlite3 python3 python3-pip python3-venv curl
fi

# 2. Setup Python Virtual Environment
echo "[2/4] Setting up Python virtual environment for worker..."
cd "$(dirname "$0")/.."
if [ ! -d "apps/worker/venv" ]; then
    python3 -m venv apps/worker/venv
fi

source apps/worker/venv/bin/activate
pip install --upgrade pip
pip install -r apps/worker/requirements.txt

# 3. Create Storage and Database Directories
echo "[3/4] Creating local storage directories..."
mkdir -p data storage/jobs

# 4. Build Go API Server
echo "[4/4] Building Go API server..."
cd apps/api
if command -v go >/dev/null 2>&1; then
    go build -o ../../api_server ./cmd/main.go
    echo "Go binary compiled to ./api_server"
else
    echo "Warning: Go is not installed on host. Run via Docker Compose or install Go 1.22+."
fi

echo "=== Setup complete! Configure .env and run: make run ==="
