#!/bin/bash
set -e
if [ ! -f "apps/api/api_server" ]; then
    echo "Building server executable first..."
    cd apps/api
    go build -o api_server ./cmd/main.go
    cd ../..
fi

echo "Starting ClipCraft AI on http://localhost:8080 ..."
./apps/api/api_server
