@echo off
rem ===========================================================================
rem  start-multi.bat - Launch rozclub-go Multi-Account Launcher
rem ===========================================================================
cd /d "%~dp0"

where pwsh >nul 2>&1
if not errorlevel 1 (
    pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-multi.ps1"
    exit /b %errorlevel%
)

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-multi.ps1"
exit /b %errorlevel%
