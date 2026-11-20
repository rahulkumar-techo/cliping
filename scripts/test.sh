#!/usr/bin/env bash
set -e

echo "=== Running Go Unit Tests ==="
cd "$(dirname "$0")/../apps/api"
go test -v ./...

echo "=== Running Python Worker Unit Tests ==="
cd ../worker/src
python3 test_pipeline.py || python test_pipeline.py

echo "=== All Tests Passed Successfully! ==="
