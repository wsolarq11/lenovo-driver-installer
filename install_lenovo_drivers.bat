@echo off
setlocal EnableExtensions
cd /d "%~dp0"

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0install_lenovo_drivers.ps1" %*
exit /b %errorlevel%
