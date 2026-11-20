@echo off
echo ===================================================
echo [1/3] Setting up Python virtual environment...
echo ===================================================
if not exist "apps\worker\venv" (
    python -m venv apps\worker\venv
)

echo.
echo ===================================================
echo [2/3] Installing Python dependencies (like npm i)...
echo ===================================================
call apps\worker\venv\Scripts\activate.bat
python -m pip install --upgrade pip
pip install -r apps\worker\requirements.txt

echo.
echo ===================================================
echo [3/3] Building Go API server...
echo ===================================================
cd apps\api
go mod download
go build -o api_server.exe .\cmd\main.go
cd ..\..

echo.
echo ===================================================
echo [SUCCESS] All packages installed successfully!
echo Run '.\run.bat' to start the server.
echo ===================================================
pause
