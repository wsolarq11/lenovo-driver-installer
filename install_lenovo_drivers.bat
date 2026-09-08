@echo off
setlocal EnableExtensions
cd /d "%~dp0"

if not exist "bin\lenovo-driver.exe" (
    set "GO_EXE=go"
    where go >nul 2>nul
    if errorlevel 1 set "GO_EXE=%USERPROFILE%\.local\go\bin\go.exe"
    if not exist "%GO_EXE%" (
        echo Go toolchain not found. Install Go 1.27 or build bin\lenovo-driver.exe.
        exit /b 1
    )
    echo Building Go engine...
    "%GO_EXE%" build -o "bin\lenovo-driver.exe" ".\cmd\lenovo-driver"
    if errorlevel 1 exit /b %errorlevel%
)

"bin\lenovo-driver.exe" %*
exit /b %errorlevel%
