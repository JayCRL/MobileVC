@echo off
setlocal enabledelayedexpansion

set "PORT=8080"
set "AUTH_TOKEN=test"

echo Checking port %PORT%...
for /f "tokens=5" %%P in ('netstat -ano ^| findstr /R /C:":%PORT% .*LISTENING"') do (
  if not defined KILLED_%%P (
    set "KILLED_%%P=1"
    echo Killing PID %%P on port %PORT%...
    taskkill /PID %%P /F >nul 2>&1
  )
)

echo Starting server on port %PORT%...
set "PORT=%PORT%"
set "AUTH_TOKEN=%AUTH_TOKEN%"
go run ./cmd/server
