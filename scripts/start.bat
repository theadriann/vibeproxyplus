@echo off
REM start.bat - Start both proxies on Windows

cd /d "%~dp0\.."

REM Check for binaries
if not exist bin\thinking-proxy.exe (
    echo Building thinking-proxy...
    go build -o bin\thinking-proxy.exe .\cmd\thinking-proxy
)

if not exist bin\cli-proxy-api.exe (
    echo Error: bin\cli-proxy-api.exe not found
    echo Download from https://github.com/router-for-me/CLIProxyAPI/releases
    exit /b 1
)

echo Starting CLIProxyAPI on :8318...
start /b bin\cli-proxy-api.exe -config config\cliproxy.yaml

timeout /t 1 /nobreak >nul

echo Starting ThinkingProxy on :8317...
echo Press Ctrl+C to stop
bin\thinking-proxy.exe
