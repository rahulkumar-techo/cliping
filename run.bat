@echo off
echo ===================================================
echo Starting ClipCraft AI...
echo ===================================================
if not exist "apps\api\api_server.exe" (
    echo Building server executable first...
    cd apps\api
    go build -o api_server.exe .\cmd\main.go
    cd ..\..
)

start "" http://localhost:8080
apps\api\api_server.exe
