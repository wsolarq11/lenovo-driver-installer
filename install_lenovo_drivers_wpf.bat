@echo off
setlocal EnableExtensions
cd /d "%~dp0"

powershell.exe -STA -NoProfile -ExecutionPolicy Bypass -File "%~dp0lenovo_driver_wpf.ps1" %*
exit /b %errorlevel%
