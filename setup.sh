#!/bin/bash
set -e
echo "==================================================="
echo "[1/3] Setting up Python virtual environment..."
echo "==================================================="
if [ ! -d "apps/worker/venv" ]; then
    python3 -m venv apps/worker/venv
fi

echo ""
echo "==================================================="
echo "[2/3] Installing Python dependencies (like npm i)..."
echo "==================================================="
source apps/worker/venv/bin/activate
pip install --upgrade pip
pip install -r apps/worker/requirements.txt

echo ""
echo "==================================================="
echo "[3/3] Building Go API server..."
echo "==================================================="
cd apps/api
go mod download
go build -o api_server ./cmd/main.go
cd ../..

echo ""
echo "==================================================="
echo "[SUCCESS] All packages installed successfully!"
echo "Run './run.sh' to start the server."
echo "==================================================="
